package finops

import (
	"context"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// EnrichWorkloadsLiveMetrics popula CPUCurrentMillis/MemCurrentMi de cada workload a partir de
// uma chamada direta à metrics.k8s.io (metrics-server) — "current" de verdade, um snapshot do
// instante do scan, diferente do histórico via Prometheus/Dynatrace (CPUAvgMillis/CPUP95Millis,
// que são agregações de uma janela de dias). Best-effort: silenciosamente não altera nada se o
// metrics-server não estiver disponível no cluster (mesmo padrão de graceful degradation de
// internal/kubernetes/node_methods.go — nunca bloqueia o resto do relatório).
func EnrichWorkloadsLiveMetrics(ctx context.Context, metricsClient metricsclientset.Interface, workloads []FinOpsWorkload, podToWorkload map[string]string) {
	if metricsClient == nil || len(podToWorkload) == 0 {
		return
	}

	podMetricsList, err := metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Debug().Err(err).Msg("FinOps: metrics-server indisponível — uso 'current' (live) de workloads ficará vazio")
		return
	}

	type usage struct{ cpuMillis, memMi float64 }
	byWorkload := make(map[string]*usage)

	for _, pm := range podMetricsList.Items {
		key, ok := podToWorkload[pm.Namespace+"/"+pm.Name]
		if !ok {
			continue
		}
		u, ok := byWorkload[key]
		if !ok {
			u = &usage{}
			byWorkload[key] = u
		}
		for _, c := range pm.Containers {
			if cpu, ok := c.Usage[corev1.ResourceCPU]; ok {
				u.cpuMillis += float64(cpu.MilliValue())
			}
			if mem, ok := c.Usage[corev1.ResourceMemory]; ok {
				u.memMi += float64(mem.Value()) / (1024 * 1024)
			}
		}
	}

	for i := range workloads {
		key := workloads[i].Namespace + "/" + workloads[i].Workload
		if u, ok := byWorkload[key]; ok {
			workloads[i].CPUCurrentMillis = round2(u.cpuMillis)
			workloads[i].MemCurrentMi = round2(u.memMi)
		}
	}
}

// ComputeNodeUsage calcula, por node único (não por workload), o uso "current" (live, via
// metrics-server) e "top" (pico histórico via Prometheus, best-effort) de CPU/Mem — usado pra
// correlacionar cada workload (via FinOpsWorkload.NodeName) com a saúde real do node onde roda.
// nodeNames: lista de nodes únicos a computar (tipicamente os NodeName distintos dos workloads
// do relatório). nodeToPool: mesmo mapa já resolvido em collectWorkloads, só pra exibição.
// promEnricher pode ser nil (sem Prometheus configurado) — nesse caso CPUTopPct/MemTopPct ficam
// zerados, mas CPUCurrentPct/MemCurrentPct (via metrics-server) continuam funcionando.
func ComputeNodeUsage(
	ctx context.Context,
	client kubernetes.Interface,
	metricsClient metricsclientset.Interface,
	nodeNames []string,
	nodeToPool map[string]string,
	promEnricher *PrometheusEnricher,
) []NodeUsage {
	result := make([]NodeUsage, 0, len(nodeNames))
	if len(nodeNames) == 0 {
		return result
	}

	// Capacidade real por node — já vem do K8s, sem precisar de PromQL/kube-state-metrics pra isso.
	capCPU := make(map[string]float64)
	capMem := make(map[string]float64)
	if nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		for _, n := range nodeList.Items {
			if cpu, ok := n.Status.Capacity[corev1.ResourceCPU]; ok {
				capCPU[n.Name] = float64(cpu.MilliValue())
			}
			if mem, ok := n.Status.Capacity[corev1.ResourceMemory]; ok {
				capMem[n.Name] = float64(mem.Value()) / (1024 * 1024)
			}
		}
	} else {
		log.Warn().Err(err).Msg("FinOps: falha ao listar nodes para NodeUsage — capacidade ficará zerada")
	}

	// Live "current" via metrics-server — uma única chamada cobre todos os nodes do cluster.
	liveCPU := make(map[string]float64)
	liveMem := make(map[string]float64)
	metricsAvailable := true
	metricsErr := ""
	if metricsClient == nil {
		metricsAvailable = false
		metricsErr = "metrics-server não configurado para este cluster"
	} else {
		nodeMetricsList, err := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
		if err != nil {
			metricsAvailable = false
			metricsErr = err.Error()
		} else {
			for _, nm := range nodeMetricsList.Items {
				if cpu, ok := nm.Usage[corev1.ResourceCPU]; ok {
					liveCPU[nm.Name] = float64(cpu.MilliValue())
				}
				if mem, ok := nm.Usage[corev1.ResourceMemory]; ok {
					liveMem[nm.Name] = float64(mem.Value()) / (1024 * 1024)
				}
			}
		}
	}

	// Pico histórico (top) via Prometheus — best-effort, correlacionado por substring no label
	// "instance" do node-exporter (ver PrometheusEnricher.nodeTopUsage).
	var topCPU, topMem map[string]float64
	if promEnricher != nil {
		topCPU, topMem = promEnricher.nodeTopUsage(ctx, nodeNames)
	}

	for _, name := range nodeNames {
		if name == "" {
			continue
		}
		nu := NodeUsage{
			NodeName:         name,
			NodePool:         nodeToPool[name],
			CPUCapMillis:     round2(capCPU[name]),
			MemCapMi:         round2(capMem[name]),
			MetricsAvailable: metricsAvailable,
			MetricsError:     metricsErr,
		}
		if cap := capCPU[name]; cap > 0 {
			nu.CPUCurrentPct = round2(liveCPU[name] / cap * 100)
			if v, ok := topCPU[name]; ok {
				nu.CPUTopPct = round2(v / cap * 100)
			}
		}
		if cap := capMem[name]; cap > 0 {
			nu.MemCurrentPct = round2(liveMem[name] / cap * 100)
			if v, ok := topMem[name]; ok {
				nu.MemTopPct = round2(v / cap * 100)
			}
		}
		result = append(result, nu)
	}
	return result
}
