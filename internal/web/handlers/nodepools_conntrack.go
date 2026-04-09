package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ConntrackNodeStats estatísticas de conntrack de um nó
type ConntrackNodeStats struct {
	NodeName    string  `json:"node_name"`
	Count       int64   `json:"count"`
	Max         int64   `json:"max"`
	Buckets     int64   `json:"buckets"`
	UsagePct    float64 `json:"usage_pct"`
	Status      string  `json:"status"` // ok / warning / critical / error
	ProbeMethod string  `json:"probe_method"`
	Error       string  `json:"error,omitempty"`
}

// ConntrackResponse resposta do endpoint de conntrack
type ConntrackResponse struct {
	NodePool  string               `json:"node_pool"`
	Cluster   string               `json:"cluster"`
	Nodes     []ConntrackNodeStats `json:"nodes"`
	FetchedAt time.Time            `json:"fetched_at"`
}

// GetConntrackStats retorna estatísticas de conntrack para todos os nós de um node pool.
// GET /api/v1/nodepools/conntrack?cluster=X&nodepool=Y
func (h *NodePoolHandler) GetConntrackStats(c *gin.Context) {
	cluster := c.Query("cluster")
	nodepool := c.Query("nodepool")
	if cluster == "" || nodepool == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros cluster e nodepool são obrigatórios"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao conectar ao cluster: %v", err)})
		return
	}

	restConfig, err := h.kubeManager.GetRestConfig(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao obter restConfig: %v", err)})
		return
	}

	nodes, err := resolveNodePoolNodes(ctx, clientset, nodepool)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(nodes) == 0 {
		c.JSON(http.StatusOK, ConntrackResponse{
			NodePool: nodepool, Cluster: cluster,
			Nodes: []ConntrackNodeStats{}, FetchedAt: time.Now(),
		})
		return
	}

	// Coletar stats em paralelo
	results := make([]ConntrackNodeStats, len(nodes))
	var wg sync.WaitGroup
	for i, nodeName := range nodes {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = probeConntrack(ctx, clientset, restConfig, name)
		}(i, nodeName)
	}
	wg.Wait()

	c.JSON(http.StatusOK, ConntrackResponse{
		NodePool: nodepool, Cluster: cluster,
		Nodes: results, FetchedAt: time.Now(),
	})
}

// resolveNodePoolNodes retorna os nomes dos nós pertencentes ao node pool
func resolveNodePoolNodes(ctx context.Context, clientset kubernetes.Interface, nodepool string) ([]string, error) {
	selectors := []string{
		fmt.Sprintf("kubernetes.azure.com/agentpool=%s", nodepool),
		fmt.Sprintf("agentpool=%s", nodepool),
	}

	for _, sel := range selectors {
		list, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: sel})
		if err == nil && len(list.Items) > 0 {
			names := make([]string, len(list.Items))
			for i, n := range list.Items {
				names[i] = n.Name
			}
			return names, nil
		}
	}

	// Fallback: filtrar por substring no nome do nó
	all, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao listar nós: %v", err)
	}
	var names []string
	for _, n := range all.Items {
		if strings.Contains(strings.ToLower(n.Name), strings.ToLower(nodepool)) {
			names = append(names, n.Name)
		}
	}
	return names, nil
}

// probeConntrack lê as métricas de conntrack de um nó via exec em pod com hostNetwork:true.
// Lê /proc/sys/net/netfilter/nf_conntrack_* — qualquer pod hostNetwork tem acesso
// ao conntrack do host sem necessidade de container privilegiado.
func probeConntrack(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config, nodeName string) ConntrackNodeStats {
	stats := ConntrackNodeStats{NodeName: nodeName, Status: "error"}

	podName, containerName, err := findHostNetworkPod(ctx, clientset, nodeName)
	if err != nil {
		stats.Error = fmt.Sprintf("nenhum pod hostNetwork encontrado no nó: %v", err)
		return stats
	}
	stats.ProbeMethod = fmt.Sprintf("exec:%s", podName)

	// Ler os três arquivos em um único exec
	cmd := []string{
		"sh", "-c",
		"printf '%s\\n%s\\n%s\\n' " +
			"$(cat /proc/sys/net/netfilter/nf_conntrack_count 2>/dev/null || echo -1) " +
			"$(cat /proc/sys/net/netfilter/nf_conntrack_max 2>/dev/null || echo -1) " +
			"$(cat /proc/sys/net/netfilter/nf_conntrack_buckets 2>/dev/null || echo -1)",
	}

	output, execErr := execCmdInPod(ctx, clientset, restConfig, "kube-system", podName, containerName, cmd)
	if execErr != nil {
		stats.Error = fmt.Sprintf("exec falhou (%s): %v", podName, execErr)
		return stats
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		stats.Error = fmt.Sprintf("resposta inesperada: %q", output)
		return stats
	}

	stats.Count = parseInt64(lines[0])
	stats.Max = parseInt64(lines[1])
	if len(lines) >= 3 {
		stats.Buckets = parseInt64(lines[2])
	}

	if stats.Max > 0 {
		stats.UsagePct = float64(stats.Count) / float64(stats.Max) * 100
	}
	switch {
	case stats.UsagePct >= 90:
		stats.Status = "critical"
	case stats.UsagePct >= 70:
		stats.Status = "warning"
	default:
		stats.Status = "ok"
	}
	return stats
}

// findHostNetworkPod localiza um pod com hostNetwork:true no nó alvo.
// Prioriza kube-proxy; fallback para qualquer pod hostNetwork em kube-system.
func findHostNetworkPod(ctx context.Context, clientset kubernetes.Interface, nodeName string) (podName, containerName string, err error) {
	// Candidatos ordenados por preferência
	labelCandidates := []string{
		"k8s-app=kube-proxy",
		"component=kube-proxy",
		"k8s-app=azure-npm",
		"app=azure-cni-networkmonitor",
	}

	for _, label := range labelCandidates {
		pods, listErr := clientset.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
			LabelSelector: label,
			FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", nodeName),
		})
		if listErr != nil || len(pods.Items) == 0 {
			continue
		}
		p := pods.Items[0]
		if p.Spec.HostNetwork && len(p.Spec.Containers) > 0 {
			return p.Name, p.Spec.Containers[0].Name, nil
		}
	}

	// Fallback genérico: qualquer pod hostNetwork Running no nó
	all, listErr := clientset.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", nodeName),
	})
	if listErr != nil {
		return "", "", fmt.Errorf("erro ao listar pods: %v", listErr)
	}
	for _, p := range all.Items {
		if p.Spec.HostNetwork && len(p.Spec.Containers) > 0 {
			return p.Name, p.Spec.Containers[0].Name, nil
		}
	}
	return "", "", fmt.Errorf("nenhum pod hostNetwork Running encontrado em kube-system no nó %s", nodeName)
}

// execCmdInPod executa um comando em um pod via SPDY e retorna o stdout
func execCmdInPod(ctx context.Context, clientset kubernetes.Interface, restConfig *rest.Config,
	namespace, podName, containerName string, cmd []string) (string, error) {

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("SPDY executor: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("stream: %v (stderr: %s)", err, stderr.String())
	}
	return stdout.String(), nil
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
