package finops

import (
	"math"
	"strings"
	"time"
)

// Visão multi-cluster do FinOps (FINOPS-REVISAO-PLAN.md, Fase 3): resumo de cada cluster a partir
// do último relatório gravado, sem novo scan. Mesmas regras de custo da tela de um cluster
// (components/finops/capacity.ts): pools com cobertura entram pelo custo efetivo, e o desperdício
// em pool sob reserva/Savings Plan/Spot não é economia certa.

// coverageShare: a partir de quanto do custo do pool um compromisso (reserva/SP/Spot) muda a
// conta — mesmo COVERAGE_SHARE do frontend e de coverage.go.
const coverageShare = 0.2

// ClusterOverview é uma linha do ranking multi-cluster.
type ClusterOverview struct {
	Cluster     string    `json:"cluster"`
	Journey     string    `json:"journey,omitempty"`
	Environment string    `json:"environment,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`

	ComputeTableBRL float64 `json:"compute_table_brl"` // compute em preço de tabela
	ComputeBRL      float64 `json:"compute_brl"`       // compute efetivo nos pools com cobertura, tabela nos demais
	StorageBRL      float64 `json:"storage_brl"`       // disco do SO + PVCs (tabela)
	TotalBRL        float64 `json:"total_brl"`         // ComputeBRL + StorageBRL
	CoveredPools    int     `json:"covered_pools"`

	WasteBRL           float64 `json:"waste_brl"`           // request acima do uso, no custo efetivo
	WasteCommittedBRL  float64 `json:"waste_committed_brl"` // parte em pools sob reserva/SP/Spot (só vira dinheiro liberando nodes)
	OrphanStorageBRL   float64 `json:"orphan_storage_brl"`  // PVCs sem pod
	IdleBRL            float64 `json:"idle_brl"`            // capacidade ociosa dos nodes (efetivo) — idle.go
	Workloads          int     `json:"workloads"`
	Nodes              int     `json:"nodes"`
	Pools              int     `json:"pools"`
	OOMRisk            int     `json:"oom_risk"`
	NoRequest          int     `json:"no_request"`
	HPARemovable       int     `json:"hpa_removable"`
	Superprovisioned   int     `json:"superprovisioned"`
	Exempt             int     `json:"exempt"`
	MetricsCoveragePct int     `json:"metrics_coverage_pct"` // workloads com métrica de uso
}

// effectiveMonthlyBRL: custo efetivo mensal do pool pela cobertura (0 = sem número em reais).
func effectiveMonthlyBRL(c *PoolCoverage) float64 {
	if c == nil || c.WindowDays <= 0 || c.EffectiveCost <= 0 {
		return 0
	}
	if c.Currency != "" && !strings.EqualFold(c.Currency, "BRL") {
		return 0
	}
	return c.EffectiveCost * (HoursPerMonth / 24.0) / float64(c.WindowDays)
}

func isCommitted(c *PoolCoverage) bool {
	return c != nil && (c.Reservation >= coverageShare || c.SavingsPlan >= coverageShare || c.Spot >= coverageShare)
}

// SummarizeCluster monta a linha do ranking. O relatório já deve ter a cobertura e as isenções
// aplicadas (AttachPoolCoverage/ApplyExemptions), como na leitura de um cluster.
func SummarizeCluster(r *FinOpsReport) ClusterOverview {
	o := ClusterOverview{Cluster: r.Cluster, GeneratedAt: r.GeneratedAt, Pools: len(r.NodePools), Workloads: len(r.Workloads)}
	factor := map[string]float64{}
	committed := map[string]bool{}
	for _, p := range r.NodePools {
		o.Nodes += p.NodeCount
		o.ComputeTableBRL += p.MonthlyCostBRL
		f := 1.0
		if eff := effectiveMonthlyBRL(p.Coverage); eff > 0 && p.MonthlyCostBRL > 0 {
			f = eff / p.MonthlyCostBRL
			o.CoveredPools++
		}
		factor[p.Name] = f
		committed[p.Name] = isCommitted(p.Coverage)
		o.ComputeBRL += p.MonthlyCostBRL * f
	}
	if len(r.NodePools) == 0 {
		o.ComputeTableBRL = r.Summary.TotalMonthlyCostBRL
		o.ComputeBRL = r.Summary.TotalMonthlyCostBRL
	}
	osBRL, pvcTotal, _ := diskCosts(r)
	o.StorageBRL = osBRL + pvcTotal
	o.OrphanStorageBRL = r.Storage.OrphanedCostBRL
	o.TotalBRL = o.ComputeBRL + o.StorageBRL

	enriched := 0
	for _, w := range r.Workloads {
		f, ok := factor[w.NodePool]
		if !ok {
			f = 1
		}
		// Teto igual ao da tela de um cluster: o desperdício de um workload não passa do custo dele.
		waste := math.Min(w.WasteBRL, w.CostShareBRL) * f
		if w.CostShareBRL <= 0 {
			waste = w.WasteBRL * f
		}
		o.WasteBRL += waste
		if committed[w.NodePool] {
			o.WasteCommittedBRL += waste
		}
		if w.MetricsSource != "" {
			enriched++
		}
		if w.ExemptActive {
			o.Exempt++
		}
		switch w.Verdict {
		case "oom_risk":
			o.OOMRisk++
		case "no_request":
			o.NoRequest++
		case "hpa_removable":
			o.HPARemovable++
		case "superprovisioned":
			o.Superprovisioned++
		}
	}
	o.IdleBRL = IdleBRL(r)
	// Mesmo teto da tela: o desperdício cabe no compute que a ociosa não ocupa.
	if room := o.ComputeBRL - o.IdleBRL; o.WasteBRL > room && o.WasteBRL > 0 && room >= 0 {
		scale := room / o.WasteBRL
		o.WasteBRL *= scale
		o.WasteCommittedBRL *= scale
	}
	if o.WasteBRL > o.ComputeBRL && o.WasteBRL > 0 {
		scale := o.ComputeBRL / o.WasteBRL
		o.WasteBRL *= scale
		o.WasteCommittedBRL *= scale
	}
	if len(r.Workloads) > 0 {
		o.MetricsCoveragePct = enriched * 100 / len(r.Workloads)
	}
	for _, v := range []*float64{&o.ComputeTableBRL, &o.ComputeBRL, &o.StorageBRL, &o.TotalBRL, &o.WasteBRL, &o.WasteCommittedBRL, &o.OrphanStorageBRL} {
		*v = round2(*v)
	}
	return o
}
