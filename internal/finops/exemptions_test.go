package finops

import (
	"testing"

	"k8s-hpa-manager/internal/storage"
)

func TestApplyExemptions(t *testing.T) {
	r := &FinOpsReport{
		Workloads: []FinOpsWorkload{
			{Namespace: "pay", Workload: "api-hlg", Pods: 1, HPAMin: 1, HPAMax: 1, HPACurrent: 1, Verdict: "hpa_removable", WasteBRL: 120},
			{Namespace: "pay", Workload: "worker", Pods: 3, HPACurrent: 3, Verdict: "superprovisioned", WasteBRL: 300},
			{Namespace: "pay", Workload: "escalou", Pods: 1, HPACurrent: 1, HPAMaxObserved: 4, Verdict: "superprovisioned", WasteBRL: 50},
			{Namespace: "pay", Workload: "sem-isencao", Pods: 1, Verdict: "superprovisioned", WasteBRL: 80},
		},
		Summary: FinOpsSummary{PotentialSavingsBRL: 550, HPARemovableCount: 1, SuperprovisionedCount: 3},
	}
	ex := map[string]*storage.FinOpsExemption{
		"pay/api-hlg": {MaxReplicasThreshold: 1, Reason: "Ambiente HLG"},
		"pay/worker":  {MaxReplicasThreshold: 2, Reason: "Controle de custos"},
		"pay/escalou": {MaxReplicasThreshold: 1, Reason: "Ambiente HLG"},
	}
	ApplyExemptions(r, ex)

	a := r.Workloads[0]
	if !a.IsExempt || !a.ExemptActive || a.Verdict != "ok" || a.WasteBRL != 0 ||
		a.ExemptOriginalVerdict != "hpa_removable" || a.ExemptOriginalWasteBRL != 120 || a.ExemptReason != "Ambiente HLG" {
		t.Errorf("HPA 1/1 isento deveria estar suprimido: %+v", a)
	}
	w := r.Workloads[1]
	if !w.IsExempt || w.ExemptActive || w.Verdict != "superprovisioned" || w.WasteBRL != 300 || w.ExemptReplicas != 3 {
		t.Errorf("3 réplicas com limite 2: isenção não vale: %+v", w)
	}
	e := r.Workloads[2]
	if e.ExemptActive || e.ExemptReplicas != 4 {
		t.Errorf("pico de 4 réplicas no histórico deve derrubar a isenção: %+v", e)
	}
	if r.Workloads[3].IsExempt {
		t.Errorf("workload sem isenção não deve mudar")
	}
	if r.Summary.PotentialSavingsBRL != 430 || r.Summary.HPARemovableCount != 0 || r.Summary.SuperprovisionedCount != 3 {
		t.Errorf("summary sem o isento ativo: %+v", r.Summary)
	}

	// Aplicar de novo (ex: o mesmo ponteiro em duas leituras) não pode descontar duas vezes.
	ApplyExemptions(r, ex)
	if r.Summary.PotentialSavingsBRL != 430 {
		t.Errorf("reaplicar descontou de novo: %v", r.Summary.PotentialSavingsBRL)
	}
}
