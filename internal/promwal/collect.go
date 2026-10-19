package promwal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AnnotationOriginalReplicas é gravada no CRD antes de desligar o Prometheus: se a limpeza for
// interrompida (processo morto, queda de rede), ela diz quantas réplicas religar.
const AnnotationOriginalReplicas = "hpa-manager/wal-cleanup-original-replicas"

const (
	logTailLines   = 200
	maxLogBytes    = 64 << 10
	maxEvents      = 6
	promContainer  = "prometheus"
	defaultCRLabel = "operator.prometheus.io/name"
)

// Runner executa kubectl contra um cluster (substituível nos testes). args não incluem --context/-n.
type Runner func(ctx context.Context, cluster, namespace string, args ...string) ([]byte, error)

// KubectlRunner é o Runner real: sempre com timeout do ctx.
func KubectlRunner(ctx context.Context, cluster, namespace string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", kubectlArgs(cluster, namespace, args)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// kubectlArgs monta os argumentos: --context sempre; -n só se namespace != "" (vazio = listagens com
// --all-namespaces, que não aceitam -n).
func kubectlArgs(cluster, namespace string, args []string) []string {
	full := []string{"--context", cluster}
	if namespace != "" {
		full = append(full, "-n", namespace)
	}
	return append(full, args...)
}

// Collector lê o estado do Prometheus no cluster. Não escreve nada.
type Collector struct {
	Client kubernetes.Interface
	Run    Runner
}

// prometheusCR é o recorte do CRD que interessa (o projeto não usa dynamic client).
type prometheusCR struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Replicas  *int32 `json:"replicas"`
		Resources struct {
			Limits map[string]string `json:"limits"`
		} `json:"resources"`
		Thanos  json.RawMessage `json:"thanos"`
		Storage struct {
			VolumeClaimTemplate struct {
				Spec struct {
					Resources struct {
						Requests map[string]string `json:"requests"`
					} `json:"resources"`
				} `json:"spec"`
			} `json:"volumeClaimTemplate"`
		} `json:"storage"`
	} `json:"spec"`
}

// ResolveName devolve o nome do recurso Prometheus: o informado ou, se vazio, o único do namespace.
func (c *Collector) ResolveName(ctx context.Context, cluster, namespace, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	out, err := c.Run(ctx, cluster, namespace, "get", "prometheus", "-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
	if err != nil {
		return "", err
	}
	names := strings.Fields(string(out))
	if len(names) != 1 {
		return "", fmt.Errorf("há %d recursos Prometheus em %s (%s): informe qual", len(names), namespace, strings.Join(names, ", "))
	}
	return names[0], nil
}

func (c *Collector) readCR(ctx context.Context, cluster, namespace, name string) (prometheusCR, error) {
	var cr prometheusCR
	out, err := c.Run(ctx, cluster, namespace, "get", "prometheus", name, "-o", "json")
	if err != nil {
		return cr, fmt.Errorf("lendo o recurso Prometheus %s/%s: %w", namespace, name, err)
	}
	if err := json.Unmarshal(out, &cr); err != nil {
		return cr, fmt.Errorf("resposta inválida do recurso Prometheus: %w", err)
	}
	return cr, nil
}

// parseReplicas lê o valor da anotação; ilegível vale 1 (religar com uma réplica é o mínimo seguro).
func parseReplicas(v string) int32 {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 1
	}
	return int32(n)
}

// Collect lê CRD, pods, PVCs, eventos e logs. Falhas de log/evento não derrubam a coleta.
func (c *Collector) Collect(ctx context.Context, cluster, namespace, name string) (Input, error) {
	in := Input{Cluster: cluster, Namespace: namespace, Name: name}

	cr, err := c.readCR(ctx, cluster, namespace, name)
	if err != nil {
		return in, err
	}
	in.Replicas = 1
	if cr.Spec.Replicas != nil {
		in.Replicas = *cr.Spec.Replicas
	}
	in.MemoryLimitBytes = parseQuantity(cr.Spec.Resources.Limits["memory"])
	in.PVCSizeBytes = parseQuantity(cr.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests["storage"])
	in.ThanosSidecar = len(cr.Spec.Thanos) > 0 && string(cr.Spec.Thanos) != "null"
	in.HelmManaged = cr.Metadata.Labels["app.kubernetes.io/managed-by"] == "Helm" || cr.Metadata.Annotations["meta.helm.sh/release-name"] != ""
	in.ArgoManaged = cr.Metadata.Labels["argocd.argoproj.io/instance"] != "" || cr.Metadata.Annotations["argocd.argoproj.io/tracking-id"] != ""

	in.CleanupCount = parseCount(cr.Metadata.Annotations[AnnotationCleanupCount])
	in.CleanupDates = parseDates(cr.Metadata.Annotations[AnnotationCleanupHistory])
	if v := cr.Metadata.Annotations[AnnotationOriginalReplicas]; v != "" {
		in.PendingRestore = true
		in.PendingRestoreReplicas = parseReplicas(v)
	}

	pods, err := c.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: defaultCRLabel + "=" + name})
	if err != nil {
		return in, fmt.Errorf("listando pods do Prometheus: %w", err)
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
	for i := range pods.Items {
		pod := &pods.Items[i]
		pi := podInfo(pod)
		if m := promMemoryLimit(pod); m > 0 {
			in.MemoryLimitBytes = m // o limit realmente aplicado ao pod vale mais que o do CRD
		}
		if !pi.Ready {
			pi.LogTail = c.logTail(ctx, namespace, pod.Name, pi.Restarts > 0)
			in.Events = append(in.Events, c.warningEvents(ctx, namespace, pod.Name)...)
		}
		in.Pods = append(in.Pods, pi)
	}
	for _, p := range in.Pods {
		if p.Node == "" && p.SchedulingMessage != "" {
			// Há pod não agendado: lê os nodes para dizer, um a um, por que ele não cabe.
			nodes, err := c.Client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				in.NodesError = "sem permissão ou falha ao listar os nodes: " + err.Error()
			} else {
				in.Nodes = nodes.Items
			}
			break
		}
	}
	if len(in.Events) > maxEvents {
		in.Events = in.Events[:maxEvents]
	}
	return in, nil
}

func podInfo(pod *corev1.Pod) PodInfo {
	pi := PodInfo{Name: pod.Name, Node: pod.Spec.NodeName, Phase: string(pod.Status.Phase)}
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			pi.PVC = v.PersistentVolumeClaim.ClaimName
			break
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != promContainer {
			continue
		}
		pi.Ready = cs.Ready
		pi.Restarts = cs.RestartCount
		if cs.State.Waiting != nil {
			pi.WaitingReason = cs.State.Waiting.Reason
		}
		if t := cs.LastTerminationState.Terminated; t != nil {
			pi.LastTermReason, pi.LastExitCode = t.Reason, t.ExitCode
		} else if t := cs.State.Terminated; t != nil {
			pi.LastTermReason, pi.LastExitCode = t.Reason, t.ExitCode
		}
	}
	pi.NodeSelector, pi.Tolerations = pod.Spec.NodeSelector, pod.Spec.Tolerations
	if aff := pod.Spec.Affinity; aff != nil && aff.NodeAffinity != nil {
		pi.NodeAffinity = aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
			pi.SchedulingMessage = cond.Message
		}
	}
	if pi.WaitingReason == "" && pod.Status.Phase == corev1.PodPending {
		pi.WaitingReason = "Pending"
	}
	return pi
}

func promMemoryLimit(pod *corev1.Pod) int64 {
	for _, ct := range pod.Spec.Containers {
		if ct.Name == promContainer {
			return ct.Resources.Limits.Memory().Value()
		}
	}
	return 0
}

// logTail lê o log do container anterior (o que morreu) e, sem ele, o do atual.
func (c *Collector) logTail(ctx context.Context, namespace, pod string, previous bool) string {
	tail := int64(logTailLines)
	for _, prev := range []bool{previous, false} {
		stream, err := c.Client.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
			Container: promContainer, Previous: prev, TailLines: &tail,
		}).Stream(ctx)
		if err != nil {
			if !prev {
				return ""
			}
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(stream, maxLogBytes))
		stream.Close()
		return string(data)
	}
	return ""
}

func (c *Collector) warningEvents(ctx context.Context, namespace, pod string) []string {
	list, err := c.Client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod + ",type=Warning",
	})
	if err != nil {
		return nil
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].LastTimestamp.After(list.Items[j].LastTimestamp.Time) })
	var out []string
	for _, e := range list.Items {
		msg := e.Message
		if len(msg) > 240 {
			msg = msg[:240] + "…"
		}
		out = append(out, fmt.Sprintf("%s: %s %s", pod, e.Reason, msg))
		if len(out) == 3 {
			break
		}
	}
	return out
}

// now é substituível nos testes.
var now = time.Now
