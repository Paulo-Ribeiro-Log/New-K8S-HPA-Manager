package report

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Contexto de node do relatório: em que node pool a aplicação rodou, quão cheios estavam os nodes que a
// hospedaram e se ela sofreu throttling de CPU. É CONTEXTO para interpretar latência (um P99 alto pode ser
// do node ou do throttling), não um veredito: não gera selo. Somente leitura no Prometheus.
//
// Achados da sondagem real (cluster AKS, node-exporter + kube-state-metrics):
//   - o node-exporter informa o hostname com a caixa do Azure ("...vmss00000S") e o Kubernetes usa minúscula
//     ("...vmss00000s"): ligar pelo NOME perde os nodes com letra no sufixo. A ligação é pelo IP: `instance`
//     do node-exporter (sem a porta) contra o `host_ip` do pod de node-exporter de cada node;
//   - o kube-state-metrics pode estar duplicado: o lado direito das junções passa por `max by (...)`.

// PrometheusRetention é a retenção do Prometheus informada pelo operador (~14 dias): CPU, memória e o
// contexto de node não existem para janelas mais antigas.
const PrometheusRetention = 14 * 24 * time.Hour

// maxNodes limita o regex de nodes de uma consulta.
const maxNodes = 400

// poolLabelKeys: rótulos do kube_node_labels que dizem o node pool, em ordem de preferência (AKS, EKS,
// GKE, Karpenter). Só existem se o kube-state-metrics exporta rótulos de node.
var poolLabelKeys = []string{
	"label_agentpool", "label_kubernetes_azure_com_agentpool", "label_eks_amazonaws_com_nodegroup",
	"label_cloud_google_com_gke_nodepool", "label_karpenter_sh_nodepool",
}
var instanceTypeKeys = []string{"label_node_kubernetes_io_instance_type", "label_beta_kubernetes_io_instance_type"}

// PoolInfo é um node pool usado pela aplicação.
type PoolInfo struct {
	Name         string `json:"name"`
	Nodes        int    `json:"nodes"`
	InstanceType string `json:"instance_type,omitempty"`
}

// NodeUsage resume o uso (%) dos nodes que hospedaram a aplicação.
type NodeUsage struct {
	NodesWithData int      `json:"nodes_with_data"`
	WorstPct      *float64 `json:"worst_pct"` // maior pico, na janela, entre os nodes (o pior node)
	AvgPct        *float64 `json:"avg_pct"`   // média, entre os nodes, da média de cada node na janela
}

// NodeContext é o contexto de node de uma aplicação. Ponteiro nulo = sem dado.
type NodeContext struct {
	Source    string     `json:"source"`
	Nodes     int        `json:"nodes"` // nodes que hospedaram pods da aplicação na janela
	Pools     []PoolInfo `json:"pools"`
	PoolLabel string     `json:"pool_label,omitempty"` // rótulo de onde veio o pool
	CPU       *NodeUsage `json:"cpu"`
	Mem       *NodeUsage `json:"mem"`
	Throttle  *Pct       `json:"throttle_pct"` // % de períodos de CPU limitados (máx/mín/méd) da aplicação
	Step      string     `json:"step"`
	Notes     []string   `json:"notes,omitempty"`
}

// ── consultas (funções puras, testáveis sem rede) ──────────────────────────────────────────────────────

func nodeQueries(namespace, workload, nodesRe string, rateWindow time.Duration) map[string]string {
	pods := fmt.Sprintf(`namespace="%s",%s`, namespace, podSelector(workload))
	// pod de node-exporter de cada node: dá o par (host_ip, node), sem depender da caixa do nome
	mapping := fmt.Sprintf(`max by (host_ip, node) (kube_pod_info{pod=~".*node-exporter.*",node=~"%s"})`, nodesRe)
	ipLabel := func(expr string) string {
		return fmt.Sprintf(`label_replace(%s, "host_ip", "$1", "instance", "([^:]+):.*")`, expr)
	}
	rw := promDur(rateWindow)
	return map[string]string{
		"nodes":  fmt.Sprintf(`count by (node) (kube_pod_info{%s})`, pods),
		"labels": fmt.Sprintf(`kube_node_labels{node=~"%s"}`, nodesRe),
		"cpu": fmt.Sprintf(`100 * (1 - avg by (node) (%s * on(host_ip) group_left(node) %s))`,
			ipLabel(fmt.Sprintf(`rate(node_cpu_seconds_total{mode="idle"}[%s])`, rw)), mapping),
		"mem": fmt.Sprintf(`100 * (1 - max by (node) (%s * on(host_ip) group_left(node) %s))`,
			ipLabel(`node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes`), mapping),
		"throttle": fmt.Sprintf(`sum(rate(container_cpu_cfs_throttled_periods_total{%s,container!=""}[%s])) / sum(rate(container_cpu_cfs_periods_total{%s,container!=""}[%s])) * 100`, pods, rw, pods, rw),
	}
}

// floatsOf lê os valores válidos de uma série do Prometheus (descarta NaN/Inf/texto).
func floatsOf(values [][]interface{}) []float64 {
	var out []float64
	for _, pt := range values {
		if len(pt) < 2 {
			continue
		}
		s, ok := pt[1].(string)
		if !ok {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(s, "%g", &v); err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func maxAvg(vs []float64) (mx, avg float64) {
	mx = math.Inf(-1)
	for _, v := range vs {
		if v > mx {
			mx = v
		}
		avg += v
	}
	return mx, avg / float64(len(vs))
}

// usageAcrossNodes junta as séries (uma por node): pior pico e média das médias.
func usageAcrossNodes(r *rangeResult) *NodeUsage {
	if r == nil {
		return nil
	}
	n, worst, sum := 0, math.Inf(-1), 0.0
	for _, s := range r.Data.Result {
		vs := floatsOf(s.Values)
		if len(vs) == 0 {
			continue
		}
		mx, avg := maxAvg(vs)
		n++
		sum += avg
		if mx > worst {
			worst = mx
		}
	}
	if n == 0 {
		return nil
	}
	avg := sum / float64(n)
	return &NodeUsage{NodesWithData: n, WorstPct: &worst, AvgPct: &avg}
}

// throttleStats lê a série única de throttling (%).
func throttleStats(r *rangeResult) *Pct {
	if r == nil || len(r.Data.Result) == 0 {
		return nil
	}
	vs := floatsOf(r.Data.Result[0].Values)
	if len(vs) == 0 {
		return nil
	}
	mx, avg := maxAvg(vs)
	mn := math.Inf(1)
	for _, v := range vs {
		mn = math.Min(mn, v)
	}
	return &Pct{Max: mx, Min: mn, Avg: avg}
}

// poolsFrom agrupa os nodes por pool a partir do kube_node_labels. O conjunto de nodes de cada pool deduplica
// as séries repetidas do kube-state-metrics (mesmo node, instance diferente).
func poolsFrom(r *rangeResult) (pools []PoolInfo, usedKey string) {
	if r == nil {
		return nil, ""
	}
	type acc struct {
		nodes map[string]bool
		inst  string
	}
	byPool := map[string]*acc{}
	for _, s := range r.Data.Result {
		node := s.Metric["node"]
		if node == "" {
			continue
		}
		pool := ""
		for _, k := range poolLabelKeys {
			if v := s.Metric[k]; v != "" {
				pool = v
				if usedKey == "" {
					usedKey = k
				}
				break
			}
		}
		if pool == "" {
			pool = "(sem rótulo de pool)"
		}
		a := byPool[pool]
		if a == nil {
			a = &acc{nodes: map[string]bool{}}
			byPool[pool] = a
		}
		a.nodes[node] = true
		if a.inst == "" {
			for _, k := range instanceTypeKeys {
				if v := s.Metric[k]; v != "" {
					a.inst = v
					break
				}
			}
		}
	}
	for name, a := range byPool {
		pools = append(pools, PoolInfo{Name: name, Nodes: len(a.nodes), InstanceType: a.inst})
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools, usedKey
}

// ── coleta ─────────────────────────────────────────────────────────────────────────────────────────────

// CollectNodeContext coleta o contexto de node de um Deployment na janela. Primeiro acha os nodes que
// hospedaram pods dele; com eles, consulta rótulos (pool), CPU, memória e throttling em paralelo. Falha
// isolada vira nota; só erro se TODAS as consultas falharem. Sem pods na janela (ex.: janela fora da
// retenção de 14 dias) devolve um contexto vazio com a explicação, não erro.
func CollectNodeContext(ctx context.Context, q RangeQuerier, namespace, workload string, w Window, plan Plan) (*NodeContext, error) {
	if !k8sNameStrict.MatchString(namespace) || !k8sNameStrict.MatchString(workload) {
		return nil, fmt.Errorf("namespace ou deployment com nome inválido")
	}
	rateWindow := 5 * time.Minute
	if plan.PromStep > rateWindow {
		rateWindow = plan.PromStep
	}
	out := &NodeContext{Source: "prometheus", Step: durLabel(plan.PromStep)}

	// 1) nodes que hospedaram a aplicação
	first := nodeQueries(namespace, workload, "x", rateWindow)["nodes"]
	res, err := queryRange(ctx, q, first, w, plan.PromStep)
	if err != nil {
		return nil, fmt.Errorf("nodes da aplicação: %w", err)
	}
	var nodes []string
	seen := map[string]bool{}
	for _, s := range res.Data.Result {
		n := s.Metric["node"]
		if n != "" && !seen[n] && k8sNameStrict.MatchString(n) && len(floatsOf(s.Values)) > 0 {
			seen[n] = true
			nodes = append(nodes, n)
		}
	}
	sort.Strings(nodes)
	if len(nodes) == 0 {
		out.Notes = append(out.Notes, "nenhum pod da aplicação encontrado no Prometheus nesta janela (o Prometheus guarda cerca de 14 dias)")
		return out, nil
	}
	if len(nodes) > maxNodes {
		out.Notes = append(out.Notes, fmt.Sprintf("a aplicação passou por %d nodes; o contexto considera os %d primeiros", len(nodes), maxNodes))
		nodes = nodes[:maxNodes]
	}
	out.Nodes = len(nodes)
	re := strings.Join(nodes, "|")

	// 2) rótulos, CPU, memória e throttling em paralelo
	qs := nodeQueries(namespace, workload, re, rateWindow)
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		got    = map[string]*rangeResult{}
		failed int
	)
	for _, key := range []string{"labels", "cpu", "mem", "throttle"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			step := plan.PromStep
			if key == "labels" {
				step = w.Duration() // só interessam os rótulos, não a série: 2 pontos bastam
			}
			r, err := queryRange(ctx, q, qs[key], w, step)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				out.Notes = append(out.Notes, fmt.Sprintf("%s: %v", key, err))
				return
			}
			got[key] = r
		}(key)
	}
	wg.Wait()
	if failed == 4 {
		return nil, fmt.Errorf("todas as consultas de contexto de node falharam: %s", strings.Join(out.Notes, "; "))
	}

	out.Pools, out.PoolLabel = poolsFrom(got["labels"])
	if len(out.Pools) == 0 {
		out.Notes = append(out.Notes, "sem rótulo de node pool: o kube-state-metrics deste cluster pode não exportar rótulos de node")
	}
	out.CPU = usageAcrossNodes(got["cpu"])
	out.Mem = usageAcrossNodes(got["mem"])
	out.Throttle = throttleStats(got["throttle"])
	if out.CPU != nil && out.CPU.NodesWithData < out.Nodes {
		out.Notes = append(out.Notes, fmt.Sprintf("uso de CPU de %d dos %d nodes (os demais sem node-exporter no período)", out.CPU.NodesWithData, out.Nodes))
	}
	return out, nil
}
