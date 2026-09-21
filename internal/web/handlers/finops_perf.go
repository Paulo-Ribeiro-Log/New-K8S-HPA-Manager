package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/storage"
)

// Medição de desempenho de CPU por node pool (aba Rightsizing). Cria UM pod efêmero por node
// amostrado, fixado naquele node, roda finops.PerfBenchScript (~15 s) e remove o pod. O resultado
// alimenta o comparativo "SKU atual × alternativa" das sugestões de tier — ver finops/vm_perf.go.

const (
	perfBenchDefaultNamespace = "default"
	perfBenchDefaultPerPool   = 2
	perfBenchMaxPerPool       = 4
	perfBenchMaxNodes         = 24 // teto por chamada: cada node = 1 pod + ~15 s de CPU
	perfBenchConcurrency      = 4
	perfBenchPodDeadlineSec   = 240
	perfBenchNodeTimeout      = 4 * time.Minute // cobre o pull da imagem num node frio
	perfBenchOverallTimeout   = 9 * time.Minute
	perfBenchPodContainer     = "bench"
)

type perfBenchRequest struct {
	Cluster      string   `json:"cluster"`
	Namespace    string   `json:"namespace"`
	NodePools    []string `json:"node_pools"`
	NodesPerPool int      `json:"nodes_per_pool"`
}

type perfBenchNodeResult struct {
	NodeName      string  `json:"node_name"`
	NodePool      string  `json:"node_pool"`
	SKU           string  `json:"sku"`
	CPUModel      string  `json:"cpu_model,omitempty"`
	VCPUs         int     `json:"vcpus,omitempty"`
	PyScore       float64 `json:"py_score,omitempty"`
	PySpread      float64 `json:"py_spread,omitempty"`
	RSASignPerSec float64 `json:"rsa_sign_per_sec,omitempty"`
	CPUFeatures   string  `json:"cpu_features,omitempty"`
	Noisy         bool    `json:"noisy,omitempty"`
	Error         string  `json:"error,omitempty"`
}

func nodeInstanceType(n corev1.Node) string {
	if v := n.Labels["node.kubernetes.io/instance-type"]; v != "" {
		return v
	}
	return n.Labels["beta.kubernetes.io/instance-type"]
}

// nodeHasUnschedulableTaint: node marcado como fora de uso por taint (ex: o remediator do AKS,
// "remediator.kubernetes.azure.com/unschedulable", quando o node está em reparo). Um node assim é um
// péssimo alvo de medição — está com problema — e o pod poderia nem agendar.
func nodeHasUnschedulableTaint(n corev1.Node) bool {
	for _, t := range n.Spec.Taints {
		if strings.HasSuffix(t.Key, "/unschedulable") {
			return true
		}
	}
	return false
}

// nodeUnderPressure: disco/memória/PIDs no limite. A imagem de medição (~500 MB) é baixada em cada
// node novo — num node com pouco disco isso piora o problema (visto: pod "Evicted ... DiskPressure").
func nodeUnderPressure(n corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		switch c.Type {
		case corev1.NodeDiskPressure, corev1.NodeMemoryPressure, corev1.NodePIDPressure:
			if c.Status == corev1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

func nodeIsReady(n corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// pickBenchNodes escolhe até perPool nodes por pool, espalhados pela lista (não os N primeiros —
// nodes de um mesmo pool podem cair em hosts/processadores diferentes, e amostrar pontas opostas
// da lista ordenada aumenta a chance de enxergar essa variação). Só Linux, Ready, não cordonados e sem taint de "unschedulable".
// Sem filtro de pools, ignora o pool de sistema do AKS (não é alvo de rightsizing de VM).
func pickBenchNodes(nodes []corev1.Node, pools []string, perPool int) []corev1.Node {
	want := map[string]bool{}
	for _, p := range pools {
		want[p] = true
	}
	byPool := map[string][]corev1.Node{}
	for _, n := range nodes {
		if n.Labels["kubernetes.io/os"] == "windows" || n.Spec.Unschedulable || nodeHasUnschedulableTaint(n) || nodeUnderPressure(n) || !nodeIsReady(n) {
			continue
		}
		pool := nodePoolLabel(n.Labels)
		if len(want) > 0 {
			if !want[pool] {
				continue
			}
		} else if n.Labels["kubernetes.azure.com/mode"] == "system" {
			continue
		}
		byPool[pool] = append(byPool[pool], n)
	}
	names := make([]string, 0, len(byPool))
	for p := range byPool {
		names = append(names, p)
	}
	sort.Strings(names)

	var out []corev1.Node
	for _, p := range names {
		list := byPool[p]
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		k := perPool
		if k > len(list) {
			k = len(list)
		}
		for i := 0; i < k; i++ {
			out = append(out, list[i*len(list)/k])
		}
	}
	return out
}

func buildPerfBenchPod(namespace string, node corev1.Node) *corev1.Pod {
	deadline := int64(perfBenchPodDeadlineSec)
	noToken, noPriv, nonRoot, uid := false, false, true, int64(65534)
	host := node.Labels["kubernetes.io/hostname"]
	if host == "" {
		host = node.Name
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("perf-bench-%d", rand.Int63()),
			Namespace: namespace,
			// Sem sidecar do Istio: com RestartPolicy=Never o sidecar nunca sai e o pod nunca "completa".
			Labels:      map[string]string{"app": "perf-bench", "created-by": "k8s-hpa-manager", "sidecar.istio.io/inject": "false"},
			Annotations: map[string]string{"sidecar.istio.io/inject": "false"},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: &deadline,
			// Nada disso é necessário pra medir CPU; sem ele o webhook de segurança do cluster (ex:
			// Falcon KAC) acusa capabilities, root e token de service account num pod descartável.
			AutomountServiceAccountToken: &noToken,
			SecurityContext:              &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			NodeSelector:                 map[string]string{"kubernetes.io/hostname": host},
			// Pools com taint (ingress, infra, spot) precisam ser tolerados pra o pod cair lá.
			Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			Containers: []corev1.Container{{
				Name:    perfBenchPodContainer,
				Image:   latencyTestPodImage,
				Command: []string{"sh", "-c", finops.PerfBenchScript},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &noPriv,
					RunAsNonRoot:             &nonRoot,
					RunAsUser:                &uid,
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					// Request de CPU garante um share justo no node (sem request o pod ficaria com o
					// menor peso e mediria a contenção, não o processador). Sem LIMIT de CPU: um limit
					// estrangularia justamente a medição.
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
			}},
		},
	}
}

// runNodePerfBench cria o pod no node, espera o container terminar, lê o log e remove o pod.
// Espera pelo estado do CONTAINER (não pela fase do pod) — robusto a qualquer sidecar injetado.
func runNodePerfBench(ctx context.Context, cs kubernetes.Interface, namespace string, node corev1.Node) (finops.PerfBenchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, perfBenchNodeTimeout)
	defer cancel()

	pod := buildPerfBenchPod(namespace, node)
	if _, err := cs.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return finops.PerfBenchResult{}, fmt.Errorf("criar pod: %w", err)
	}
	defer func() {
		delCtx, dcancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dcancel()
		grace := int64(0)
		_ = cs.CoreV1().Pods(namespace).Delete(delCtx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
	}()

	lastState := "Pending"
	pullFailures := 0
	for {
		p, err := cs.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return finops.PerfBenchResult{}, fmt.Errorf("consultar pod: %w", err)
		}
		lastState = string(p.Status.Phase)
		for _, cst := range p.Status.ContainerStatuses {
			if cst.Name != perfBenchPodContainer {
				continue
			}
			if w := cst.State.Waiting; w != nil {
				lastState = w.Reason
				if w.Reason == "ImagePullBackOff" || w.Reason == "ErrImagePull" || w.Reason == "InvalidImageName" {
					if pullFailures++; pullFailures >= 3 {
						return finops.PerfBenchResult{}, fmt.Errorf("não conseguiu baixar a imagem %s (%s)", latencyTestPodImage, w.Reason)
					}
				}
			}
			if t := cst.State.Terminated; t != nil {
				raw, lerr := cs.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: perfBenchPodContainer}).Do(ctx).Raw()
				if lerr != nil {
					return finops.PerfBenchResult{}, fmt.Errorf("ler log do benchmark: %w", lerr)
				}
				res, perr := finops.ParsePerfBenchOutput(string(raw))
				if perr != nil {
					return res, fmt.Errorf("%w (exit %d)", perr, t.ExitCode)
				}
				return res, nil
			}
		}
		if p.Status.Phase == corev1.PodFailed {
			return finops.PerfBenchResult{}, fmt.Errorf("pod falhou: %s %s", p.Status.Reason, p.Status.Message)
		}
		for _, cond := range p.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Message != "" {
				lastState = fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
			}
		}
		select {
		case <-ctx.Done():
			return finops.PerfBenchResult{}, fmt.Errorf("tempo esgotado esperando o benchmark (estado: %s)", lastState)
		case <-time.After(2 * time.Second):
		}
	}
}

// RunPerfBenchmark godoc
// POST /api/v1/finops/perf-benchmark
//
// Mede o desempenho de CPU single-thread de nodes de cada pool e grava o resultado. Cria pods
// efêmeros (250m de CPU pedidos, removidos ao final) — por isso exige o grupo SRE, como os demais
// POSTs de escrita do FinOps. Síncrono (até ~9 min); o frontend mostra progresso indeterminado.
func (h *FinOpsHandler) RunPerfBenchmark(c *gin.Context) {
	var req perfBenchRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Cluster) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: 'cluster' é obrigatório"})
		return
	}
	if h.rightsizingStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de rightsizing indisponível (medições não podem ser gravadas)"})
		return
	}
	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = perfBenchDefaultNamespace
	}
	perPool := req.NodesPerPool
	if perPool <= 0 {
		perPool = perfBenchDefaultPerPool
	}
	if perPool > perfBenchMaxPerPool {
		perPool = perfBenchMaxPerPool
	}

	k8sClient, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao conectar ao cluster: " + err.Error()}) //nolint:gosec
		return
	}
	cs := k8sClient

	ctx, cancel := context.WithTimeout(c.Request.Context(), perfBenchOverallTimeout)
	defer cancel()

	nodeList, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Falha ao listar nodes: " + err.Error()})
		return
	}
	picked := pickBenchNodes(nodeList.Items, req.NodePools, perPool)
	if len(picked) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "nenhum node elegível (Linux, Ready, não cordonado) nos pools pedidos"})
		return
	}
	if len(picked) > perfBenchMaxNodes {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(
			"%d nodes selecionados excede o teto de %d por execução — escolha menos pools ou reduza nodes_per_pool", len(picked), perfBenchMaxNodes)})
		return
	}

	log.Info().Str("cluster", req.Cluster).Str("user", c.GetString("user_email")).Int("nodes", len(picked)).
		Str("namespace", namespace).Msg("FinOps/Perf: iniciando medição de desempenho de CPU")

	results := make([]perfBenchNodeResult, len(picked))
	var wg sync.WaitGroup
	sem := make(chan struct{}, perfBenchConcurrency)
	now := time.Now()
	for i, n := range picked {
		wg.Add(1)
		go func(i int, n corev1.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := perfBenchNodeResult{NodeName: n.Name, NodePool: nodePoolLabel(n.Labels), SKU: nodeInstanceType(n)}
			res, berr := runNodePerfBench(ctx, cs, namespace, n)
			if berr != nil {
				r.Error = berr.Error()
				log.Warn().Err(berr).Str("cluster", req.Cluster).Str("node", n.Name).Msg("FinOps/Perf: medição falhou")
			} else {
				r.CPUModel, r.VCPUs = res.CPUModel, res.VCPUs
				r.PyScore, r.PySpread, r.RSASignPerSec = res.PyBest, res.PySpread, res.RSASignPerSec
				r.CPUFeatures = strings.Join(res.Features, " ")
				r.Noisy = res.PySpread > finops.PerfNoisySpread
			}
			results[i] = r
		}(i, n)
	}
	wg.Wait()

	var toSave []storage.NodePerfBenchmark
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		toSave = append(toSave, storage.NodePerfBenchmark{
			Cluster: req.Cluster, NodeName: r.NodeName, NodePool: r.NodePool, SKU: r.SKU, CPUModel: r.CPUModel,
			VCPUs: r.VCPUs, PyScore: r.PyScore, PySpread: r.PySpread, RSASignPerSec: r.RSASignPerSec, CPUFeatures: r.CPUFeatures,
			Samples: 5, MeasuredAt: now,
		})
	}
	if len(toSave) > 0 {
		if err := h.rightsizingStore.SavePerfBenchmarks(toSave); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "medições feitas, mas falhou ao gravar: " + err.Error(), "results": results})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"cluster": req.Cluster, "measured": len(toSave), "failed": len(results) - len(toSave), "results": results})
}

// perfSKUSummary é uma linha do quadro "desempenho medido por SKU" (frota inteira).
type perfSKUSummary struct {
	SKU        string   `json:"sku"`
	Score      float64  `json:"score"`
	Nodes      int      `json:"nodes"`
	CPUModels  []string `json:"cpu_models"`
	Noisy      bool     `json:"noisy,omitempty"`
	RSAScore   float64  `json:"rsa_score,omitempty"` // RSA-2048 assinaturas/s (mediana) — proxy de criptografia/TLS
	Features   []string `json:"features,omitempty"`  // extensões de CPU garantidas em todos os nodes medidos
	RelToBest  float64  `json:"rel_to_best"`         // 1,00 = o SKU mais rápido medido
	Clusters   int      `json:"clusters"`
	MeasuredAt string   `json:"measured_at,omitempty"`
}

// GetPerfBenchmarks godoc
// GET /api/v1/finops/perf-benchmark?cluster=X
//
// Devolve as medições do cluster (por node) e o quadro por SKU da frota inteira — o quadro é o que
// permite comparar SKUs medidos em clusters diferentes.
func (h *FinOpsHandler) GetPerfBenchmarks(c *gin.Context) {
	if h.rightsizingStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de rightsizing indisponível"})
		return
	}
	cluster := c.Query("cluster")
	all, err := h.rightsizingStore.ListPerfBenchmarks("")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	nodes := []storage.NodePerfBenchmark{}
	clustersBySKU := map[string]map[string]bool{}
	latest := map[string]time.Time{}
	for _, r := range all {
		if cluster != "" && r.Cluster == cluster {
			nodes = append(nodes, r)
		}
		k := strings.ToLower(r.SKU)
		if clustersBySKU[k] == nil {
			clustersBySKU[k] = map[string]bool{}
		}
		clustersBySKU[k][r.Cluster] = true
		if r.MeasuredAt.After(latest[k]) {
			latest[k] = r.MeasuredAt
		}
	}

	bySKU := finops.BuildSKUPerf(all)
	summary := make([]perfSKUSummary, 0, len(bySKU))
	best := 0.0
	for _, p := range bySKU {
		if !p.Noisy && p.Score > best {
			best = p.Score
		}
	}
	if best == 0 { // só há medições ruidosas: compara contra a melhor delas
		for _, p := range bySKU {
			if p.Score > best {
				best = p.Score
			}
		}
	}
	// Nome de exibição do SKU: a grafia mais frequente entre as medições (o registro mistura caixas).
	display := map[string]string{}
	for _, r := range all {
		if display[strings.ToLower(r.SKU)] == "" {
			display[strings.ToLower(r.SKU)] = r.SKU
		}
	}
	for k, p := range bySKU {
		row := perfSKUSummary{SKU: display[k], Score: p.Score, Nodes: p.Nodes, CPUModels: p.CPUModels, Noisy: p.Noisy, RSAScore: p.RSAScore, Features: p.Features, Clusters: len(clustersBySKU[k])}
		if best > 0 {
			row.RelToBest = p.Score / best
		}
		if t := latest[k]; !t.IsZero() {
			row.MeasuredAt = t.Format(time.RFC3339)
		}
		summary = append(summary, row)
	}
	sort.Slice(summary, func(i, j int) bool { return summary[i].Score > summary[j].Score })

	c.JSON(http.StatusOK, gin.H{"cluster": cluster, "nodes": nodes, "sku_summary": summary})
}

// perfSet carrega as medições agregadas (frota + por pool). nil-safe: sem store ou sem medições
// devolve um conjunto vazio, e a resposta de rightsizing simplesmente não traz comparativo.
func (h *FinOpsHandler) perfSet() finops.PerfSet {
	if h.rightsizingStore == nil {
		return finops.PerfSet{}
	}
	recs, err := h.rightsizingStore.ListPerfBenchmarks("")
	if err != nil {
		log.Warn().Err(err).Msg("FinOps/Perf: falha ao ler medições — comparativo de desempenho omitido")
		return finops.PerfSet{}
	}
	return finops.BuildPerfSet(recs)
}
