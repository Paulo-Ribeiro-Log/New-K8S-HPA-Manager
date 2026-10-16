package finops

import "k8s-hpa-manager/internal/storage"

// exemptReplicas é o número de réplicas comparado com o limite da isenção: o maior entre as
// réplicas atuais (HPA ou pods) e o pico visto no histórico — um HPA que escalou acima do limite
// na janela não está "parado em 1" e volta a ser analisado.
func exemptReplicas(w *FinOpsWorkload) int {
	n := w.HPACurrent
	if n <= 0 {
		n = w.Pods
	}
	if w.HPAMaxObserved > n {
		n = w.HPAMaxObserved
	}
	return n
}

// ApplyExemptions marca os workloads isentos do relatório e, nos que estão dentro do limite de
// réplicas, suprime a análise (Verdict "ok", WasteBRL 0) e tira os números deles do Summary.
// Roda na leitura (GetReport/GetLastReport) sobre o relatório recém-desserializado — o cache
// guarda o relatório sem isenção, então criar ou remover uma isenção não exige novo scan.
// exemptions: chave "namespace/workload" (FinOpsExemptionsStore.GetAllAsMap).
func ApplyExemptions(r *FinOpsReport, exemptions map[string]*storage.FinOpsExemption) {
	if r == nil || len(exemptions) == 0 {
		return
	}
	s := &r.Summary
	for i := range r.Workloads {
		w := &r.Workloads[i]
		ex, ok := exemptions[w.Namespace+"/"+w.Workload]
		if !ok || w.IsExempt {
			continue
		}
		w.IsExempt = true
		w.ExemptReason = ex.Reason
		w.ExemptThreshold = ex.MaxReplicasThreshold
		w.ExemptReplicas = exemptReplicas(w)
		if w.ExemptReplicas > ex.MaxReplicasThreshold {
			continue // passou do limite: a isenção não vale, o workload segue analisado
		}
		w.ExemptActive = true
		w.ExemptOriginalVerdict = w.Verdict
		w.ExemptOriginalWasteBRL = w.WasteBRL

		switch w.Verdict {
		case "superprovisioned":
			s.SuperprovisionedCount--
		case "oom_risk":
			s.OOMRiskCount--
		case "no_request":
			s.NoRequestCount--
		case "hpa_removable":
			s.HPARemovableCount--
		case "fixed_high_cost":
			s.FixedHighCostCount--
		case "sem_dados":
			s.NoDataCount--
		}
		if w.WasteBRL > 0 {
			s.PotentialSavingsBRL = round2(max(0, s.PotentialSavingsBRL-w.WasteBRL))
		}
		if w.HPACostMinBRL < w.HPACostCurrentBRL {
			s.HPASavingsIfMinBRL = round2(max(0, s.HPASavingsIfMinBRL-(w.HPACostCurrentBRL-w.HPACostMinBRL)))
		}
		w.Verdict = "ok"
		w.WasteBRL = 0
	}
}
