package finops

import (
	"fmt"
	"strings"
	"testing"
)

func TestEncaixeNoNodeDaSKU(t *testing.T) {
	ds := DeepDaemonSetOverhead{PerNodeCPUReqMillis: 200, PerNodeMemReqMi: 500, PerNodePods: 2}
	s := DeepSimulation{Feasible: true, AllocCPUMillis: 4000, AllocMemMi: 16000, NodesRecommended: 2,
		FreeCPUMillis: 3800, FreeMemMi: 15500, FreePods: 108}
	inv := DeepInventory{AvgPodCPUMillis: 100, AvgPodMemMi: 200, BiggestPod: "ns/grande", BiggestPodCPU: 1000, BiggestPodMemMi: 2000}
	f := buildNodeFit(s, inv, ds, 110, deepDemand{cpuMillis: 4000, memMi: 8000, pods: 60})
	if f == nil {
		t.Fatal("deveria calcular o encaixe")
	}
	if f.PodsByCPU != 38 || f.PodsByMem != 77 || f.PodsByLimit != 108 || f.PodsFit != 38 || f.FitLimiting != "cpu" {
		t.Errorf("pods que cabem: %+v", f)
	}
	if f.BiggestFits != 3 || f.BiggestPctCPU != 25 || f.BiggestPctMem != 12.5 {
		t.Errorf("maior pod: %+v", f)
	}
	if f.DSCPUPct != 5 || f.OccCPUPct != 55 || f.OccMemPct != 28.13 || f.PodsPerNode != 30 {
		t.Errorf("ocupação: %+v", f)
	}
	if f.OccPodsPct != 29.09 {
		t.Errorf("ocupação de pods (limite 110/node): %v", f.OccPodsPct)
	}
}

func TestEncaixeLimitadoPorPodsQuandoOsPodsSaoPequenos(t *testing.T) {
	s := DeepSimulation{Feasible: true, AllocCPUMillis: 8000, AllocMemMi: 32000, NodesRecommended: 1, FreeCPUMillis: 8000, FreeMemMi: 32000, FreePods: 108}
	f := buildNodeFit(s, DeepInventory{AvgPodCPUMillis: 10, AvgPodMemMi: 10}, DeepDaemonSetOverhead{PerNodePods: 2}, 110, deepDemand{pods: 10})
	if f.PodsFit != 108 || f.FitLimiting != "pods" {
		t.Errorf("pods pequenos esbarram nos 110 por node: %+v", f)
	}
	if buildNodeFit(DeepSimulation{Feasible: false}, DeepInventory{}, DeepDaemonSetOverhead{}, 110, deepDemand{}) != nil {
		t.Errorf("SKU que não comporta não tem encaixe")
	}
}

func TestInventarioPorTipoEMaiorPod(t *testing.T) {
	in := DeepAnalysisInput{Nodes: []DeepNode{{CPUAllocMillis: 4000, MemAllocMi: 16000}}}
	ws := []DeepWorkloadRow{
		{Namespace: "a", Workload: "api", Kind: "Deployment", PodsOnPool: 4, CPUReqMillis: 100, MemReqMi: 200, CPURecMillis: 100, MemRecMi: 200},
		{Namespace: "a", Workload: "web", Kind: "Deployment", PodsOnPool: 2, CPUReqMillis: 1500, MemReqMi: 400, CPURecMillis: 1000, MemRecMi: 400},
		{Namespace: "b", Workload: "db", Kind: "StatefulSet", PodsOnPool: 3, CPUReqMillis: 500, MemReqMi: 8000, CPURecMillis: 500, MemRecMi: 8000},
	}
	inv := buildInventory(in, DeepDaemonSetOverhead{PerNodePods: 5, Items: make([]DeepDaemonSetRow, 3)}, ws, nil)
	if len(inv.Kinds) != 2 || inv.Kinds[0].Kind != "Deployment" || inv.Kinds[0].Workloads != 2 || inv.Kinds[0].Pods != 6 {
		t.Errorf("tipos: %+v", inv.Kinds)
	}
	if inv.DaemonSets != 3 || inv.TotalPods != 9+5 {
		t.Errorf("DaemonSets/total: %+v", inv)
	}
	if inv.BiggestPod != "b/db" {
		t.Errorf("o maior pod é o db (8000Mi de 16000Mi = 50%%): %q", inv.BiggestPod)
	}
}

func TestMarkdownMostraInventarioEEncaixe(t *testing.T) {
	a := PoolDeepAnalysis{
		SimulationInputs: DeepSimulationInputs{Inventory: DeepInventory{
			Kinds: []DeepKindCount{{Kind: "Deployment", Workloads: 12, Pods: 40}}, DaemonSets: 6, DSPodsPerNode: 6, TotalPods: 70, Nodes: 5, AvgPodsNode: 14,
			BiggestPod: "ns/api", BiggestPodCPU: 1000, BiggestPodMemMi: 2048}},
		Simulation: []DeepSimulation{{VMSize: "Standard_D8s_v5", IsCurrent: true, Fit: &DeepNodeFit{DSPods: 6, PodsFit: 38, PodsByCPU: 38, PodsByMem: 77, PodsByLimit: 104, BiggestFits: 3, OccCPUPct: 55}}},
	}
	var b strings.Builder
	writeInventoryAndFit(func(f string, x ...interface{}) { b.WriteString(fmt.Sprintf(f, x...)) }, a)
	out := b.String()
	for _, want := range []string{"12 Deployment (40 pods)", "6 DaemonSets", "70 pods em 5 nodes", "`ns/api`", "**38** (38 / 77 / 104)", "Standard_D8s_v5"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
}
