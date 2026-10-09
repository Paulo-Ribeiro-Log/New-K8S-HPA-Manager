package finops

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestAKSReservedCPUMillis(t *testing.T) {
	cases := map[int]float64{1: 60, 2: 100, 4: 140, 8: 180, 16: 260, 32: 420, 64: 740}
	for vcpu, want := range cases {
		if got := aksReservedCPUMillis(vcpu); got != want {
			t.Errorf("aksReservedCPUMillis(%d) = %v; quer %v", vcpu, got, want)
		}
	}
}

// Validação contra o pool real calculofrete (F4s_v2): 3860m e ~5,8 GiB alocáveis observados.
func TestEstimateAllocatableBateComF4sV2Real(t *testing.T) {
	spec, ok := DeepSpecFor("Standard_F4s_v2")
	if !ok {
		t.Fatal("F4s_v2 deveria estar na tabela")
	}
	cpu, mem := EstimateAllocatable(spec, 110, 0.984)
	if cpu != 3860 {
		t.Errorf("CPU alocável = %v; quer 3860", cpu)
	}
	if math.Abs(mem-5817) > 150 {
		t.Errorf("memória alocável = %.0f Mi; quer ~5817 Mi (±150)", mem)
	}
}

func TestDeepSpecFor(t *testing.T) {
	s, ok := DeepSpecFor("standard_f4as_v6")
	if !ok || s.VCPU != 4 || s.MemGB != 16 || s.SMT {
		t.Errorf("F4as_v6 = %+v, ok=%v; quer 4 vCPU / 16 GB sem SMT", s, ok)
	}
	if s.VMSize != "Standard_F4as_v6" {
		t.Errorf("nome normalizado = %q", s.VMSize)
	}
	if s, ok := DeepSpecFor("Standard_D4s_v4"); !ok || s.MemGB != 16 || !s.SMT {
		t.Errorf("D4s_v4 = %+v, ok=%v", s, ok)
	}
	if _, ok := DeepSpecFor("Standard_X99_v9"); ok {
		t.Error("SKU desconhecida não deveria ser encontrada")
	}
}

func TestDeepCandidateSKUs(t *testing.T) {
	c := DeepCandidateSKUs("Standard_F4s_v2", 4)
	names := map[string]DeepSKUSpec{}
	for _, s := range c {
		names[s.VMSize] = s
	}
	for _, want := range []string{"Standard_D4s_v5", "Standard_D4as_v5", "Standard_F4as_v6", "Standard_D4s_v4", "Standard_D8as_v5"} {
		if _, ok := names[want]; !ok {
			t.Errorf("faltou candidata %s", want)
		}
	}
	if _, ok := names["Standard_F4s_v2"]; ok {
		t.Error("a SKU atual não deve aparecer entre as candidatas")
	}
	for _, s := range DeepCandidateSKUs("Standard_D16s_v5", 16) {
		if s.VCPU > 16 {
			t.Errorf("não deveria sugerir %s (dobro acima de 16 vCPU)", s.VMSize)
		}
	}
}

// fixtureCalculofrete reproduz o perfil do pool calculofrete em escala menor (10 nodes F4s_v2): CPU
// reservada alta e pouco usada, memória quase cheia, DaemonSets pesados.
func fixtureCalculofrete(withMetrics, withHistory bool) DeepAnalysisInput {
	spec, _ := DeepSpecFor("Standard_F4s_v2")
	in := DeepAnalysisInput{
		Cluster: "akspriv-oferta-prd", Pool: "calculofrete", VMSize: spec.VMSize, Region: "brazilsouth",
		CurrentSpec: spec, Candidates: DeepCandidateSKUs(spec.VMSize, spec.VCPU),
		Prices: map[string]DeepPrice{
			"standard_f4s_v2":  {USDHour: 0.169, Source: "api"},
			"standard_d4s_v5":  {USDHour: 0.192, Source: "api"},
			"standard_f4as_v6": {USDHour: 0.21, Source: "api"},
		},
		ExchangeRate: 5.0, MetricsLive: withMetrics,
		Now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
	}
	type ds struct {
		ns, name       string
		cpuReq, memReq float64
		cpuUse, memUse float64
		missingMemLim  bool
	}
	daemonsets := []ds{
		{"dynatrace", "dynakube-oneagent", 100, 0, 28, 517, true},
		{"falcon-system", "falcon-sensor", 50, 300, 73, 586, false},
		{"logging", "fluentd", 100, 300, 133, 160, false},
		{"kube-system", "kube-proxy", 100, 0, 2, 59, true},
		{"calico-system", "calico-node", 0, 0, 18, 156, true},
		{"kube-system", "csi-azurefile-node", 40, 80, 13, 131, false},
	}
	type app struct {
		name           string
		perNode        int
		cpuReq, memReq float64
		cpuLim, memLim float64
		cpuUse, memUse float64
		p95CPU, p95Mem float64
	}
	apps := []app{
		{"frete-hub", 2, 800, 950, 1000, 1500, 35, 752, 59, 790},
		{"frete-b2c", 2, 500, 620, 900, 1100, 59, 534, 127, 589},
		{"frete-aggregate-sku-mktp", 1, 500, 477, 2000, 2861, 38, 694, 44, 780},
		{"frete-envvias", 1, 150, 800, 1200, 1500, 17, 564, 23, 575},
	}
	in.History = map[string]DeepHistory{}
	for i := 0; i < 10; i++ {
		node := fmt.Sprintf("aks-calculofrete-vmss%02d", i)
		n := DeepNode{
			Name: node, Zone: fmt.Sprintf("brazilsouth-%d", i%3+1),
			CPUCapMillis: 4000, MemCapMi: 8063, CPUAllocMillis: 3860, MemAllocMi: 5817, PodsAlloc: 110,
		}
		var nodeCPU, nodeMem float64
		for _, d := range daemonsets {
			in.Pods = append(in.Pods, DeepPod{
				Namespace: d.ns, Name: fmt.Sprintf("%s-%d", d.name, i), Node: node, OwnerKind: "DaemonSet", Workload: d.name,
				CPUReqMillis: d.cpuReq, MemReqMi: d.memReq, MissingMemLimit: d.missingMemLim,
				CPUUsageMillis: d.cpuUse, MemUsageMi: d.memUse, HasUsage: withMetrics,
			})
			nodeCPU += d.cpuUse
			nodeMem += d.memUse
		}
		for _, a := range apps {
			for k := 0; k < a.perNode; k++ {
				in.Pods = append(in.Pods, DeepPod{
					Namespace: "calculo-de-fretes-prd", Name: fmt.Sprintf("%s-abc-%d%d", a.name, i, k), Node: node,
					OwnerKind: "Deployment", Workload: a.name,
					CPUReqMillis: a.cpuReq, MemReqMi: a.memReq, CPULimMillis: a.cpuLim, MemLimMi: a.memLim,
					CPUUsageMillis: a.cpuUse, MemUsageMi: a.memUse, HasUsage: withMetrics,
				})
				nodeCPU += a.cpuUse
				nodeMem += a.memUse
			}
		}
		if withMetrics {
			n.CPUUsageMillis, n.MemUsageMi, n.HasUsage = nodeCPU+250, nodeMem+250, true
		}
		in.Nodes = append(in.Nodes, n)
	}
	if withHistory {
		for _, a := range apps {
			in.History["calculo-de-fretes-prd/"+a.name] = DeepHistory{CPUP95Millis: a.p95CPU, MemP95Mi: a.p95Mem, MemPeakMi: a.p95Mem * 1.05, Source: "prometheus"}
		}
		gen := in.Now.Add(-2 * time.Hour)
		in.HistoryGeneratedAt, in.HistoryWindowDays = &gen, 30
	}
	return in
}

func findingCodes(fs []DeepFinding) map[string]DeepFinding {
	m := map[string]DeepFinding{}
	for _, f := range fs {
		m[f.Code] = f
	}
	return m
}

func TestBuildPoolDeepAnalysisPerfilCalculofrete(t *testing.T) {
	a := BuildPoolDeepAnalysis(fixtureCalculofrete(true, true))

	if a.Overview.Nodes != 10 || a.Overview.VCPU != 4 || a.Overview.MemGB != 8 || len(a.Overview.Zones) != 3 {
		t.Errorf("overview inesperado: %+v", a.Overview)
	}
	if want := round2(0.169 * HoursPerMonth * 10 * 5.0); a.Overview.MonthlyCostBRL != want {
		t.Errorf("custo mensal = %v; quer %v", a.Overview.MonthlyCostBRL, want)
	}

	al := a.Allocation
	if al.SchedulingBound != "cpu" || al.RealBottleneck != "memory" || !al.Mismatch {
		t.Errorf("diagnóstico: bound=%s bottleneck=%s mismatch=%v; quer cpu/memory/true", al.SchedulingBound, al.RealBottleneck, al.Mismatch)
	}
	if al.CPU.RequestPct < 80 || al.CPU.UsageLivePct > 25 {
		t.Errorf("CPU: request %.0f%% / uso %.0f%%; quer request alto e uso baixo", al.CPU.RequestPct, al.CPU.UsageLivePct)
	}
	if al.NodesMemAbove90 != 10 {
		t.Errorf("nodes com memória ≥ 90%% = %d; quer 10", al.NodesMemAbove90)
	}

	ds := a.DaemonSets
	if len(ds.Items) != 6 || math.Abs(ds.PerNodeMemUsageMi-1609) > 1 || ds.PerNodePods != 6 {
		t.Errorf("DaemonSets: %d itens, %.0f Mi/node, %.1f pods/node; quer 6, 1609, 6", len(ds.Items), ds.PerNodeMemUsageMi, ds.PerNodePods)
	}
	if ds.Items[0].Name != "falcon-sensor" {
		t.Errorf("DaemonSet de maior memória = %s; quer falcon-sensor", ds.Items[0].Name)
	}
	var dyna DeepDaemonSetRow
	for _, d := range ds.Items {
		if d.Name == "dynakube-oneagent" {
			dyna = d
		}
	}
	if !containsStr(dyna.Flags, "no_mem_request") || !containsStr(dyna.Flags, "no_mem_limit") {
		t.Errorf("oneagent deveria ter no_mem_request e no_mem_limit: %v", dyna.Flags)
	}

	if len(a.Workloads) != 4 || a.Workloads[0].Workload != "frete-hub" {
		t.Fatalf("workloads: %+v", a.Workloads)
	}
	hub := a.Workloads[0]
	if hub.UsageBasis != "p95" || hub.PodsOnPool != 20 || !containsStr(hub.Flags, "cpu_over_requested") {
		t.Errorf("frete-hub: basis=%s pods=%d flags=%v", hub.UsageBasis, hub.PodsOnPool, hub.Flags)
	}
	if hub.CPURecMillis != 80 { // P95 59m × 1,2 = 70,8 → múltiplo de 10 acima = 80m
		t.Errorf("CPU recomendado do frete-hub = %v; quer 80", hub.CPURecMillis)
	}
	var agg DeepWorkloadRow
	for _, w := range a.Workloads {
		if w.Workload == "frete-aggregate-sku-mktp" {
			agg = w
		}
	}
	if !containsStr(agg.Flags, "mem_under_requested") || !containsStr(agg.Flags, "high_mem_limit_ratio") {
		t.Errorf("aggregate-sku-mktp deveria ter mem_under_requested e high_mem_limit_ratio: %v", agg.Flags)
	}

	codes := findingCodes(a.Findings)
	for _, c := range []string{"allocation_mismatch", "nodes_memory_pressure", "memory_overcommit", "daemonset_overhead", "daemonset_no_mem_request", "memory_under_requested", "cpu_over_requested"} {
		if _, ok := codes[c]; !ok {
			t.Errorf("faltou o achado %q", c)
		}
	}
	if a.Findings[0].Severity != "critical" {
		t.Errorf("primeiro achado deveria ser crítico: %+v", a.Findings[0])
	}

	sims := map[string]DeepSimulation{}
	for _, s := range a.Simulation {
		sims[s.VMSize] = s
	}
	cur := sims["Standard_F4s_v2"]
	if !cur.IsCurrent || cur.AllocEstimated || cur.AllocCPUMillis != 3860 {
		t.Errorf("simulação da SKU atual deveria usar o alocável medido: %+v", cur)
	}
	d4 := sims["Standard_D4s_v5"]
	if !d4.AllocEstimated || !d4.Feasible || d4.NodesRecommended >= cur.NodesCurrentReq || d4.LimitingRecommended == "cpu" {
		t.Errorf("D4s_v5 deveria precisar de menos nodes, limitado por memória ou mínimo: %+v (atual com requests de hoje: %d)", d4, cur.NodesCurrentReq)
	}
	if d4.NodesRecommended < deepMinNodes {
		t.Errorf("nunca menos que %d nodes: %d", deepMinNodes, d4.NodesRecommended)
	}
	if fas := sims["Standard_F4as_v6"]; !strings.Contains(strings.Join(fas.Notes, " "), "sem SMT") {
		t.Errorf("F4as_v6 deveria trazer a nota de SMT: %v", fas.Notes)
	}
	if sims["Standard_E4as_v5"].PriceUSDHour != 0 || len(sims["Standard_E4as_v5"].Notes) == 0 {
		t.Errorf("SKU sem preço deveria ter preço 0 e nota: %+v", sims["Standard_E4as_v5"])
	}
	if a.Simulation[0].PriceUSDHour == 0 {
		t.Error("SKUs com preço devem vir antes das sem preço")
	}
}

func TestBuildPoolDeepAnalysisSemMetricasNemHistorico(t *testing.T) {
	a := BuildPoolDeepAnalysis(fixtureCalculofrete(false, false))
	if a.Allocation.RealBottleneck != "" || a.Allocation.CPU.HasLive {
		t.Errorf("sem uso, não há gargalo real: %+v", a.Allocation)
	}
	for _, w := range a.Workloads {
		if w.UsageBasis != "" || w.CPURecMillis != 0 {
			t.Errorf("%s sem uso não deveria ter recomendação: %+v", w.Workload, w)
		}
	}
	if len(a.Warnings) < 2 {
		t.Errorf("deveria avisar sobre metrics-server e histórico: %v", a.Warnings)
	}
	// Sem uso, o cenário recomendado = requests atuais.
	for _, s := range a.Simulation {
		if s.IsCurrent && s.NodesRecommended != s.NodesCurrentReq {
			t.Errorf("sem uso, recomendado deveria igualar o atual: %+v", s)
		}
	}
}

func TestNodesFor(t *testing.T) {
	ds := DeepDaemonSetOverhead{PerNodeCPUReqMillis: 500, PerNodeMemReqMi: 800, PerNodeMemUsageMi: 1800, PerNodePods: 10}
	// livre: 3360m / 4017Mi / 100 pods; headroom 0,8 → 2688m / 3213,6Mi por node.
	n, lim := nodesFor(deepDemand{cpuMillis: 26880, memMi: 1000, pods: 50}, 3860, 5817, 110, ds, 0.8, 3)
	if n != 10 || lim != "cpu" {
		t.Errorf("por CPU: %d/%s; quer 10/cpu", n, lim)
	}
	n, lim = nodesFor(deepDemand{cpuMillis: 100, memMi: 100, pods: 5}, 3860, 5817, 110, ds, 0.8, 3)
	if n != 3 || lim != "min_nodes" {
		t.Errorf("mínimo: %d/%s; quer 3/min_nodes", n, lim)
	}
	n, lim = nodesFor(deepDemand{cpuMillis: 100, memMi: 100, pods: 950}, 3860, 5817, 110, ds, 0.8, 3)
	if n != 10 || lim != "pods" {
		t.Errorf("por pods: %d/%s; quer 10/pods", n, lim)
	}
	if n, _ := nodesFor(deepDemand{cpuMillis: 1}, 400, 1000, 110, ds, 0.8, 3); n != 0 {
		t.Errorf("SKU que não comporta os DaemonSets deveria dar 0: %d", n)
	}
}

func TestDeepHistoryFromReport(t *testing.T) {
	r := &FinOpsReport{Workloads: []FinOpsWorkload{
		{Namespace: "a", Workload: "x", CPUP95Millis: 10, MemP95Mi: 20, CPUMaxMillis: 30, MemMaxMi: 40, MetricsSource: "prometheus"},
		{Namespace: "a", Workload: "sem-dados"},
	}}
	h := DeepHistoryFromReport(r)
	if len(h) != 1 || h["a/x"].CPUPeakMillis != 30 || h["a/x"].MemPeakMi != 40 {
		t.Errorf("histórico = %+v", h)
	}
	if DeepHistoryFromReport(nil) != nil {
		t.Error("nil deveria devolver nil")
	}
}
