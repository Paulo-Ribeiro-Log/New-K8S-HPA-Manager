package finops

import (
	"math"
	"testing"
)

func TestSummarizeCluster(t *testing.T) {
	r := &FinOpsReport{
		Cluster: "akspriv-oferta-prd-admin",
		NodePools: []FinOpsPool{
			// 30 dias de custo efetivo = 1000 → 1000 × (730/24)/30 ≈ 1013,89/mês, contra 4000 de tabela.
			{Name: "calculofrete", NodeCount: 10, MonthlyCostBRL: 4000, Coverage: &PoolCoverage{Reservation: 1, EffectiveCost: 1000, Currency: "BRL", WindowDays: 30}},
			{Name: "frete", NodeCount: 2, MonthlyCostBRL: 1000},
		},
		Workloads: []FinOpsWorkload{
			{Workload: "a", NodePool: "calculofrete", WasteBRL: 400, CostShareBRL: 800, Verdict: "superprovisioned", MetricsSource: "prometheus"},
			{Workload: "b", NodePool: "frete", WasteBRL: 900, CostShareBRL: 100, Verdict: "oom_risk", MetricsSource: "prometheus"},
			{Workload: "c", NodePool: "frete", Verdict: "ok", ExemptActive: true},
			{Workload: "d", NodePool: "frete", Verdict: "no_request"},
		},
		Storage: StorageSummary{OSDiskCostBRL: 50, TotalMonthlyCostBRL: 30, OrphanedCostBRL: 10},
	}
	o := SummarizeCluster(r)
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.02 }
	if !near(o.ComputeBRL, 2013.89) || o.ComputeTableBRL != 5000 || o.CoveredPools != 1 {
		t.Errorf("compute efetivo/tabela: %+v", o)
	}
	if !near(o.TotalBRL, 2093.89) || o.StorageBRL != 80 {
		t.Errorf("total = compute efetivo + disco do SO + PVCs: %+v", o)
	}
	// 400 × (1013,89/4000) ≈ 101,39 sob reserva + 100 sob demanda (900 limitado ao custo do workload).
	if !near(o.WasteBRL, 201.39) || !near(o.WasteCommittedBRL, 101.39) {
		t.Errorf("desperdício no custo efetivo, separando o que está sob reserva: %+v", o)
	}
	if o.Nodes != 12 || o.OOMRisk != 1 || o.NoRequest != 1 || o.Superprovisioned != 1 || o.Exempt != 1 || o.MetricsCoveragePct != 50 {
		t.Errorf("contadores: %+v", o)
	}
}

func TestIdleBRL(t *testing.T) {
	// 4 nodes de 4 vCPU / 8 GiB alocáveis; CPU 50% requisitada, memória 75% em uso → ocupação 75%.
	r := &FinOpsReport{
		NodePools: []FinOpsPool{
			{Name: "app", NodeCount: 4, MonthlyCostBRL: 1000, AllocatableNodes: 4, AllocatableCPUMillicores: 16000, AllocatableMemoryMi: 32768},
			{Name: "vazio", NodeCount: 2, MonthlyCostBRL: 500, VMCPUCores: 4, VMMemoryGB: 8},
		},
		Workloads: []FinOpsWorkload{
			{NodePool: "app", Pods: 4, CPURequestMillis: 2000, MemRequestMi: 4096, MemAvgMi: 6144},
		},
	}
	if occ := PoolOccupancy(r); occ["app"] != 0.75 {
		t.Errorf("ocupação = %v, want 0,75 (memória em uso)", occ["app"])
	}
	// Pool sem workload atribuído fica fora (mesma regra da tela).
	if got := IdleBRL(r); got != 250 {
		t.Errorf("ociosa = %v, want 250", got)
	}
}
