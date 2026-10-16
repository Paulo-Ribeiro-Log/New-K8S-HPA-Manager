package finops

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/storage"
)

func TestEvaluateAction(t *testing.T) {
	sent := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	a := storage.FinOpsAction{
		Cluster: "c", Namespace: "pay", Workload: "api", CreatedAt: sent,
		BaselineHPAMin: 10, BaselineCPUMillis: 1000, BaselineMemMi: 2048, BaselineCostBRL: 1000,
		TargetHPAMin: 4, TargetCPUMillis: 300, TargetMemMi: 1024,
	}
	report := func(at time.Time, w FinOpsWorkload) *FinOpsReport {
		w.Namespace, w.Workload = "pay", "api"
		return &FinOpsReport{GeneratedAt: at, Workloads: []FinOpsWorkload{w}}
	}
	later := sent.Add(48 * time.Hour)

	if ev := EvaluateAction(a, report(sent.Add(-time.Hour), FinOpsWorkload{})); ev.Status != ActionWaitingScan {
		t.Errorf("relatório anterior ao envio: %+v", ev)
	}
	if ev := EvaluateAction(a, &FinOpsReport{GeneratedAt: later}); ev.Status != ActionNotFound {
		t.Errorf("workload sumiu: %+v", ev)
	}
	ev := EvaluateAction(a, report(later, FinOpsWorkload{HPAMin: 10, CPURequestMillis: 1000, MemRequestMi: 2048, CostShareBRL: 1000}))
	if ev.Status != ActionPending || ev.Targets != 3 || ev.Applied != 0 {
		t.Errorf("nada mudou: %+v", ev)
	}
	// CPU arredondada para 400m (86% do caminho) conta; min ficou em 10.
	ev = EvaluateAction(a, report(later, FinOpsWorkload{HPAMin: 10, CPURequestMillis: 400, MemRequestMi: 1024, CostShareBRL: 700}))
	if ev.Status != ActionPartial || ev.Applied != 2 || ev.RealizedSavingBRL != 300 {
		t.Errorf("parcial: %+v", ev)
	}
	ev = EvaluateAction(a, report(later, FinOpsWorkload{HPAMin: 4, CPURequestMillis: 300, MemRequestMi: 1024, CostShareBRL: 280}))
	if ev.Status != ActionApplied || ev.RealizedSavingBRL != 720 {
		t.Errorf("aplicada: %+v", ev)
	}
	// Alvo de SUBIR request (risco de OOM): chegou, e a "economia" é negativa.
	up := storage.FinOpsAction{Namespace: "pay", Workload: "api", CreatedAt: sent, BaselineMemMi: 512, BaselineCostBRL: 100, TargetMemMi: 1024}
	ev = EvaluateAction(up, report(later, FinOpsWorkload{MemRequestMi: 1024, CostShareBRL: 150}))
	if ev.Status != ActionApplied || ev.RealizedSavingBRL != -50 {
		t.Errorf("subida de request: %+v", ev)
	}
}
