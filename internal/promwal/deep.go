package promwal

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	tempPodImage     = "busybox:1.36"
	deepReadyWait    = 2 * time.Minute
	pollEvery        = 2 * time.Second
	deepPodLabelKey  = "app"
	deepPodLabelVal  = "prometheus-wal-analyze"
	cleanPodLabelVal = "prometheus-wal-cleanup"
)

var maxTimeRe = regexp.MustCompile(`"maxTime":\s*(\d+)`)

func parseQuantity(s string) int64 {
	if s == "" {
		return 0
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.Value()
}

// stsSpec é o que o pod temporário herda do StatefulSet do Prometheus para conseguir montar o mesmo
// volume e ler/escrever os mesmos arquivos (subPath, permissões, nó/pool).
type stsSpec struct {
	SubPath      string
	Tolerations  []corev1.Toleration
	NodeSelector map[string]string
	SecurityCtx  *corev1.PodSecurityContext
}

func (c *Collector) statefulSetSpec(ctx context.Context, ns, crName string) stsSpec {
	sts, err := c.Client.AppsV1().StatefulSets(ns).Get(ctx, "prometheus-"+crName, metav1.GetOptions{})
	if err != nil {
		return stsSpec{}
	}
	tpl := sts.Spec.Template.Spec
	sp := stsSpec{Tolerations: tpl.Tolerations, NodeSelector: tpl.NodeSelector, SecurityCtx: tpl.SecurityContext}
	if len(sts.Spec.VolumeClaimTemplates) == 0 {
		return sp
	}
	vct := sts.Spec.VolumeClaimTemplates[0].Name
	for _, ct := range tpl.Containers {
		if ct.Name != promContainer {
			continue
		}
		for _, vm := range ct.VolumeMounts {
			if vm.Name == vct {
				sp.SubPath = vm.SubPath
			}
		}
	}
	return sp
}

// tempPod monta o PVC em /prometheus. nodeName vazio = o scheduler escolhe (usa nodeSelector/tolerations do
// StatefulSet); preenchido = fixa no nó (análise com o Prometheus ainda ligado: RWO só monta no mesmo nó).
func tempPod(namePrefix, label, pvc, nodeName string, sp stsSpec, readOnly bool) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-%04x", namePrefix, rand.Intn(0x10000)),
			Labels: map[string]string{deepPodLabelKey: label},
		},
		Spec: corev1.PodSpec{
			NodeName:        nodeName,
			NodeSelector:    sp.NodeSelector,
			RestartPolicy:   corev1.RestartPolicyNever,
			Tolerations:     sp.Tolerations,
			SecurityContext: sp.SecurityCtx,
			Containers: []corev1.Container{{
				Name: "sh", Image: tempPodImage, Command: []string{"sleep", "3600"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/prometheus", SubPath: sp.SubPath, ReadOnly: readOnly}},
			}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc, ReadOnly: readOnly},
			}}},
		},
	}
}

// CollectDeep monta o PVC de cada pod com problema em um pod temporário READ-ONLY (fixado no nó do pod
// do Prometheus, o que evita falha de RWO em nó diferente e dispensa desligar o Prometheus) e lê blocos
// e tamanhos. O pod temporário é sempre removido. Falha em um PVC não impede os demais.
func (c *Collector) CollectDeep(ctx context.Context, in *Input) []error {
	sp := c.statefulSetSpec(ctx, in.Namespace, in.Name)
	var errs []error
	for _, p := range in.Pods {
		if p.Ready {
			continue
		}
		d, err := c.deepOne(ctx, in.Namespace, in.Cluster, p, sp)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name, err))
			continue
		}
		in.Deep = append(in.Deep, d)
	}
	return errs
}

func (c *Collector) deepOne(ctx context.Context, ns, cluster string, p PodInfo, sp stsSpec) (DeepInfo, error) {
	d := DeepInfo{Pod: p.Name, PVC: p.PVC}
	if p.PVC == "" || p.Node == "" {
		return d, fmt.Errorf("pod sem PVC ou ainda sem nó (não foi agendado): não dá para montar o volume")
	}
	pod := tempPod("prometheus-wal-analyze", deepPodLabelVal, p.PVC, p.Node, sp, true)
	name, err := c.startTempPod(ctx, ns, pod, deepReadyWait)
	if err != nil {
		return d, err
	}
	defer c.removeTempPod(ns, name)

	v, err := c.inspectVolume(ctx, cluster, ns, name)
	if err != nil {
		return d, err
	}
	d.Blocks, d.LastBlockMaxTime, d.WALBytes, d.ChunksHeadBytes = v.Blocks, v.LastBlockMaxTime, v.WALBytes, v.ChunksHeadBytes
	return d, nil
}

// startTempPod cria o pod e espera ficar Ready. Em erro, o pod já criado é removido.
func (c *Collector) startTempPod(ctx context.Context, ns string, pod *corev1.Pod, wait time.Duration) (string, error) {
	if _, err := c.Client.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("criando pod temporário: %w", err)
	}
	if err := c.waitReady(ctx, ns, pod.Name, wait); err != nil {
		c.removeTempPod(ns, pod.Name)
		return "", err
	}
	return pod.Name, nil
}

// removeTempPod apaga o pod e espera sumir (libera o PVC). Usa ctx próprio: o da requisição pode já ter
// sido cancelado, e é justamente aí que a limpeza precisa acontecer.
func (c *Collector) removeTempPod(ns, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = c.Client.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{})
	_ = c.waitGone(ctx, ns, name)
}

// inspectVolume lê, dentro do pod temporário, os blocos compactados e o tamanho de wal/ e chunks_head/.
func (c *Collector) inspectVolume(ctx context.Context, cluster, ns, pod string) (DeepInfo, error) {
	var d DeepInfo
	out, err := c.Run(ctx, cluster, ns, "exec", pod, "--", "sh", "-c", "cat /prometheus/*/meta.json 2>/dev/null; true")
	if err != nil {
		return d, fmt.Errorf("lendo meta.json dos blocos: %w", err)
	}
	d.LastBlockMaxTime = lastMaxTime(string(out))
	if out, err := c.Run(ctx, cluster, ns, "exec", pod, "--", "sh", "-c", "ls -d /prometheus/01* 2>/dev/null | wc -l"); err == nil {
		d.Blocks, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	if out, err := c.Run(ctx, cluster, ns, "exec", pod, "--", "sh", "-c", "cd /prometheus && du -sk wal chunks_head 2>/dev/null; true"); err == nil {
		d.WALBytes, d.ChunksHeadBytes = parseDU(string(out))
	}
	return d, nil
}

func (c *Collector) waitReady(ctx context.Context, ns, name string, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		pod, err := c.Client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Ready {
					return nil
				}
				if w := cs.State.Waiting; w != nil && (w.Reason == "ErrImagePull" || w.Reason == "ImagePullBackOff") {
					return fmt.Errorf("o nó não conseguiu baixar %s (%s): sem acesso ao registry", tempPodImage, w.Reason)
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("pod temporário não ficou pronto em %s", wait)
		case <-time.After(pollEvery):
		}
	}
}

func (c *Collector) waitGone(ctx context.Context, ns, name string) error {
	for {
		if _, err := c.Client.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{}); err != nil {
			return nil // NotFound (ou erro de API: não há mais o que esperar)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// lastMaxTime devolve o maior maxTime (ms) entre os meta.json concatenados.
func lastMaxTime(s string) time.Time {
	var best int64
	for _, m := range maxTimeRe.FindAllStringSubmatch(s, -1) {
		if v, err := strconv.ParseInt(m[1], 10, 64); err == nil && v > best {
			best = v
		}
	}
	if best == 0 {
		return time.Time{}
	}
	return time.UnixMilli(best)
}

// parseDU lê a saída de `du -sk wal chunks_head` ("<kb>\t<dir>").
func parseDU(s string) (wal, chunks int64) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		kb, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		switch f[1] {
		case "wal":
			wal = kb * 1024
		case "chunks_head":
			chunks = kb * 1024
		}
	}
	return
}
