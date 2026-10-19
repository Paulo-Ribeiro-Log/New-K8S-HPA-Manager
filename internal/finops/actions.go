package finops

import (
	"math"

	"k8s-hpa-manager/internal/storage"
)

// Acompanhamento das recomendações aplicadas (FINOPS-REVISAO-PLAN.md, Fase 3): a foto gravada no
// envio (storage.FinOpsAction) contra o relatório mais recente do cluster.

const (
	ActionWaitingScan = "aguardando_scan" // o relatório mais recente é anterior ao envio
	ActionNotFound    = "nao_encontrado"  // o workload sumiu do relatório (renomeado, removido)
	ActionPending     = "pendente"        // nada do alvo chegou ao cluster
	ActionPartial     = "parcial"         // parte do alvo chegou
	ActionApplied     = "aplicada"        // tudo chegou
)

// appliedProgress: fração do caminho foto → alvo a partir da qual a mudança conta como aplicada
// (o time pode ter arredondado: 120m em vez de 115m).
const appliedProgress = 0.8

// ActionEvaluation é o estado de uma ação no relatório mais recente.
type ActionEvaluation struct {
	Status          string  `json:"status"`
	ReportAt        string  `json:"report_at,omitempty"`
	CurrentPods     int     `json:"current_pods,omitempty"`
	CurrentHPAMin   int     `json:"current_hpa_min,omitempty"`
	CurrentHPAMax   int     `json:"current_hpa_max,omitempty"`
	CurrentCPU      float64 `json:"current_cpu_millis,omitempty"`
	CurrentMem      float64 `json:"current_mem_mi,omitempty"`
	CurrentCostBRL  float64 `json:"current_cost_brl,omitempty"`
	CurrentWasteBRL float64 `json:"current_waste_brl,omitempty"`
	// RealizedSavingBRL = custo da foto − custo agora (por mês, mesma base de preço do relatório).
	// Negativo = ficou mais caro (ex.: subiu request por risco de OOM, ou a carga cresceu).
	RealizedSavingBRL float64 `json:"realized_saving_brl"`
	Applied           int     `json:"applied"` // quantas dimensões do alvo chegaram
	Targets           int     `json:"targets"` // quantas dimensões o alvo tinha
}

// reached diz se current percorreu ao menos appliedProgress do caminho baseline → target.
func reached(baseline, current, target float64) bool {
	if target <= 0 {
		return false
	}
	if baseline == target {
		return math.Abs(current-target) <= target*0.05
	}
	return (baseline-current)/(baseline-target) >= appliedProgress
}

// EvaluateAction compara a ação com o relatório (já com cobertura e isenções, como na leitura).
func EvaluateAction(a storage.FinOpsAction, r *FinOpsReport) ActionEvaluation {
	if r == nil || !r.GeneratedAt.After(a.CreatedAt) {
		return ActionEvaluation{Status: ActionWaitingScan}
	}
	ev := ActionEvaluation{ReportAt: r.GeneratedAt.Format("2006-01-02T15:04:05Z07:00")}
	var w *FinOpsWorkload
	for i := range r.Workloads {
		if r.Workloads[i].Namespace == a.Namespace && r.Workloads[i].Workload == a.Workload {
			w = &r.Workloads[i]
			break
		}
	}
	if w == nil {
		ev.Status = ActionNotFound
		return ev
	}
	ev.CurrentPods, ev.CurrentHPAMin, ev.CurrentHPAMax = w.Pods, w.HPAMin, w.HPAMax
	ev.CurrentCPU, ev.CurrentMem = w.CPURequestMillis, w.MemRequestMi
	ev.CurrentCostBRL = round2(w.CostShareBRL)
	ev.CurrentWasteBRL = round2(w.WasteBRL)
	ev.RealizedSavingBRL = round2(a.BaselineCostBRL - w.CostShareBRL)

	check := func(target, baseline, current float64) {
		if target <= 0 {
			return
		}
		ev.Targets++
		if reached(baseline, current, target) {
			ev.Applied++
		}
	}
	check(float64(a.TargetHPAMin), float64(a.BaselineHPAMin), float64(w.HPAMin))
	check(float64(a.TargetHPAMax), float64(a.BaselineHPAMax), float64(w.HPAMax))
	check(a.TargetCPUMillis, a.BaselineCPUMillis, w.CPURequestMillis)
	check(a.TargetMemMi, a.BaselineMemMi, w.MemRequestMi)
	switch {
	case ev.Targets > 0 && ev.Applied == ev.Targets:
		ev.Status = ActionApplied
	case ev.Applied > 0:
		ev.Status = ActionPartial
	default:
		ev.Status = ActionPending
	}
	return ev
}
