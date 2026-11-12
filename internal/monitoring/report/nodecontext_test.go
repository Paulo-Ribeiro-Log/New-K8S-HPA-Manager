package report

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

type nodeCall struct {
	query string
	step  time.Duration
}

// nodeFake responde às consultas do contexto de node por tipo e registra o que foi pedido.
type nodeFake struct {
	mu     sync.Mutex
	calls  []nodeCall
	series map[string][]fakeSeries // "nodes", "labels", "cpu", "mem", "throttle"
	fail   map[string]bool
}

type fakeSeries struct {
	metric map[string]string
	values []string
}

func (f *nodeFake) kind(q string) string {
	switch {
	case strings.Contains(q, "count by (node) (kube_pod_info"):
		return "nodes"
	case strings.HasPrefix(q, "kube_node_labels"):
		return "labels"
	case strings.Contains(q, "node_cpu_seconds_total"):
		return "cpu"
	case strings.Contains(q, "node_memory_MemAvailable_bytes"):
		return "mem"
	case strings.Contains(q, "container_cpu_cfs_throttled_periods_total"):
		return "throttle"
	}
	return "?"
}

func (f *nodeFake) QueryRange(_ context.Context, query string, _, _ time.Time, step time.Duration) (*promclient.QueryRangeResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, nodeCall{query, step})
	f.mu.Unlock()
	k := f.kind(query)
	if f.fail[k] {
		return nil, errors.New("boom " + k)
	}
	r := &promclient.QueryRangeResult{Status: "success"}
	for _, s := range f.series[k] {
		var vals [][]interface{}
		for i, v := range s.values {
			vals = append(vals, []interface{}{float64(i), v})
		}
		r.Data.Result = append(r.Data.Result, struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		}{Metric: s.metric, Values: vals})
	}
	return r, nil
}

func (f *nodeFake) queryOf(kind string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if f.kind(c.query) == kind {
			return c.query, true
		}
	}
	return "", false
}

func (f *nodeFake) stepOf(kind string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if f.kind(c.query) == kind {
			return c.step
		}
	}
	return 0
}

func okNode() *nodeFake {
	return &nodeFake{series: map[string][]fakeSeries{
		"nodes": {
			{map[string]string{"node": "aks-p-vmss000001"}, []string{"3", "3"}},
			{map[string]string{"node": "aks-p-vmss00000f"}, []string{"2", "NaN"}},
			{map[string]string{"node": "aks-p-vmss000002"}, []string{"NaN"}}, // sem ponto válido: o node não conta
		},
		"labels": {
			{map[string]string{"node": "aks-p-vmss000001", "label_agentpool": "calculofret2", "label_node_kubernetes_io_instance_type": "Standard_D4s_v5", "instance": "10.0.0.1:8080"}, []string{"1"}},
			{map[string]string{"node": "aks-p-vmss000001", "label_agentpool": "calculofret2", "label_node_kubernetes_io_instance_type": "Standard_D4s_v5", "instance": "10.0.0.2:8080"}, []string{"1"}}, // kube-state-metrics duplicado
			{map[string]string{"node": "aks-p-vmss00000f", "label_agentpool": "calculofret2", "label_node_kubernetes_io_instance_type": "Standard_D4s_v5"}, []string{"1"}},
		},
		"cpu": {
			{map[string]string{"node": "aks-p-vmss000001"}, []string{"10", "30", "20"}},
			{map[string]string{"node": "aks-p-vmss00000f"}, []string{"70.5", "50"}},
		},
		"mem": {
			{map[string]string{"node": "aks-p-vmss000001"}, []string{"40", "42"}},
			{map[string]string{"node": "aks-p-vmss00000f"}, []string{"35"}},
		},
		"throttle": {{map[string]string{}, []string{"0", "0.5", "NaN", "0.1"}}},
	}}
}

func nodeWindow() (Window, Plan) {
	w := Window{Start: now.Add(-6 * time.Hour), End: now}
	p, _ := PlanFor(w, now, nil)
	return w, p
}

func TestCollectNodeContextAgrega(t *testing.T) {
	f := okNode()
	w, plan := nodeWindow()
	nc, err := CollectNodeContext(context.Background(), f, "calculo-de-fretes-prd", "frete-hub", w, plan)
	if err != nil {
		t.Fatal(err)
	}
	// só os nodes com ponto válido contam (o terceiro só tinha NaN)
	if nc.Nodes != 2 {
		t.Errorf("nodes que hospedaram a aplicação: %d", nc.Nodes)
	}
	// pool: um só, dedupe do kube-state-metrics duplicado (2 séries do mesmo node = 1 node)
	if len(nc.Pools) != 1 || nc.Pools[0] != (PoolInfo{Name: "calculofret2", Nodes: 2, InstanceType: "Standard_D4s_v5"}) || nc.PoolLabel != "label_agentpool" {
		t.Errorf("pool: %+v (rótulo %q)", nc.Pools, nc.PoolLabel)
	}
	// CPU: pior pico entre os nodes = 70.5; média das médias = (20 + 60.25)/2
	if nc.CPU == nil || nc.CPU.NodesWithData != 2 || *nc.CPU.WorstPct != 70.5 || math.Abs(*nc.CPU.AvgPct-40.125) > 1e-9 {
		t.Errorf("CPU dos nodes: %+v", nc.CPU)
	}
	if nc.Mem == nil || *nc.Mem.WorstPct != 42 || math.Abs(*nc.Mem.AvgPct-38) > 1e-9 {
		t.Errorf("memória dos nodes: %+v", nc.Mem)
	}
	// throttling: NaN ignorado
	if nc.Throttle == nil || nc.Throttle.Max != 0.5 || nc.Throttle.Min != 0 || math.Abs(nc.Throttle.Avg-0.2) > 1e-9 {
		t.Errorf("throttling: %+v", nc.Throttle)
	}
	if len(nc.Notes) != 0 {
		t.Errorf("sem notas esperadas: %v", nc.Notes)
	}
}

func TestCollectNodeContextConsultas(t *testing.T) {
	f := okNode()
	w, plan := nodeWindow()
	if _, err := CollectNodeContext(context.Background(), f, "calculo-de-fretes-prd", "frete-hub", w, plan); err != nil {
		t.Fatal(err)
	}
	cpu, _ := f.queryOf("cpu")
	// ligação pelo IP (e não pelo nome, que muda de caixa), com o lado direito deduplicado
	for _, trecho := range []string{`"host_ip"`, `on(host_ip) group_left(node)`, `max by (host_ip, node)`, `node=~"aks-p-vmss000001|aks-p-vmss00000f"`, `.*node-exporter.*`} {
		if !strings.Contains(cpu, trecho) {
			t.Errorf("a consulta de CPU deveria conter %s: %s", trecho, cpu)
		}
	}
	if strings.Contains(cpu, "nodename") || strings.Contains(cpu, "node_uname_info") {
		t.Errorf("não pode ligar pelo nome do node: %s", cpu)
	}
	nodes, _ := f.queryOf("nodes")
	if !strings.Contains(nodes, `pod=~"frete-hub-[a-z0-9]{5,10}-[a-z0-9]{5}"`) {
		t.Errorf("seletor de pod estrito: %s", nodes)
	}
	// rótulos: só interessam os rótulos, não a série (passo = duração da janela)
	if got := f.stepOf("labels"); got != w.Duration() {
		t.Errorf("passo dos rótulos %s, esperava a duração da janela %s", got, w.Duration())
	}
	if got := f.stepOf("cpu"); got != plan.PromStep {
		t.Errorf("passo da CPU %s, esperava %s", got, plan.PromStep)
	}
}

func TestCollectNodeContextSemPods(t *testing.T) {
	f := &nodeFake{series: map[string][]fakeSeries{}}
	w, plan := nodeWindow()
	nc, err := CollectNodeContext(context.Background(), f, "ns", "app", w, plan)
	if err != nil || nc == nil || nc.Nodes != 0 || len(nc.Notes) != 1 || !strings.Contains(nc.Notes[0], "14 dias") {
		t.Fatalf("sem pods: %+v %v", nc, err)
	}
	if len(f.calls) != 1 {
		t.Errorf("sem nodes não há o que consultar depois: %d chamadas", len(f.calls))
	}
}

func TestCollectNodeContextFalhas(t *testing.T) {
	w, plan := nodeWindow()
	// a consulta dos nodes é indispensável
	f := okNode()
	f.fail = map[string]bool{"nodes": true}
	if _, err := CollectNodeContext(context.Background(), f, "ns", "app", w, plan); err == nil {
		t.Errorf("sem a lista de nodes deveria dar erro")
	}
	// falha parcial: a memória some e fica uma nota; o resto aparece
	f = okNode()
	f.fail = map[string]bool{"mem": true}
	nc, err := CollectNodeContext(context.Background(), f, "ns", "app", w, plan)
	if err != nil || nc.Mem != nil || nc.CPU == nil || len(nc.Notes) != 1 || !strings.HasPrefix(nc.Notes[0], "mem:") {
		t.Errorf("falha parcial: %+v %v", nc, err)
	}
	// todas as consultas do contexto falham: erro
	f = okNode()
	f.fail = map[string]bool{"labels": true, "cpu": true, "mem": true, "throttle": true}
	if _, err := CollectNodeContext(context.Background(), f, "ns", "app", w, plan); err == nil {
		t.Errorf("todas falharam: esperava erro")
	}
	// nomes inválidos nunca viram consulta
	f = okNode()
	if _, err := CollectNodeContext(context.Background(), f, `ns"x`, "app", w, plan); err == nil || len(f.calls) != 0 {
		t.Errorf("nome inválido: %v (%d chamadas)", err, len(f.calls))
	}
}

func TestPoolsFromRotulosAlternativosESemRotulo(t *testing.T) {
	mk := func(m map[string]string) *rangeResult {
		r := &rangeResult{}
		r.Data.Result = append(r.Data.Result, struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		}{Metric: m})
		return r
	}
	// EKS: rótulo de node group
	pools, key := poolsFrom(mk(map[string]string{"node": "n1", "label_eks_amazonaws_com_nodegroup": "ng-web", "label_beta_kubernetes_io_instance_type": "m5.large"}))
	if len(pools) != 1 || pools[0].Name != "ng-web" || pools[0].InstanceType != "m5.large" || key != "label_eks_amazonaws_com_nodegroup" {
		t.Errorf("EKS: %+v %q", pools, key)
	}
	// o rótulo preferido vence o alternativo
	pools, key = poolsFrom(mk(map[string]string{"node": "n1", "label_agentpool": "a", "label_kubernetes_azure_com_agentpool": "b"}))
	if pools[0].Name != "a" || key != "label_agentpool" {
		t.Errorf("preferência: %+v %q", pools, key)
	}
	// sem rótulo de pool
	pools, key = poolsFrom(mk(map[string]string{"node": "n1"}))
	if len(pools) != 1 || pools[0].Name != "(sem rótulo de pool)" || key != "" {
		t.Errorf("sem rótulo: %+v %q", pools, key)
	}
	if p, _ := poolsFrom(nil); p != nil {
		t.Errorf("nil: %+v", p)
	}
}

func TestCoberturaParcialDoNodeExporterViraNota(t *testing.T) {
	f := okNode()
	f.series["cpu"] = f.series["cpu"][:1] // só 1 dos 2 nodes tem node-exporter
	w, plan := nodeWindow()
	nc, err := CollectNodeContext(context.Background(), f, "ns", "app", w, plan)
	if err != nil || nc.CPU.NodesWithData != 1 || len(nc.Notes) != 1 || !strings.Contains(nc.Notes[0], "1 dos 2 nodes") {
		t.Errorf("cobertura parcial deve aparecer: %+v %v", nc, err)
	}
}
