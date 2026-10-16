package finops

import (
	"strings"

	"k8s-hpa-manager/internal/storage"
)

// AttachDiskCost anexa o custo efetivo mensal dos discos (Cost Management, gravado com a cobertura)
// ao relatório: disco do SO por pool e PVC pelo nome do disco (= PV, pvc-<uid>). Moeda ≠ BRL não
// entra (sem número em reais). Roda na leitura, como AttachPoolCoverage.
func AttachDiskCost(r *FinOpsReport, rows []storage.DiskCost) {
	if r == nil || len(rows) == 0 {
		return
	}
	os, disks := map[string]float64{}, map[string]float64{}
	for _, d := range rows {
		if d.WindowDays <= 0 || (d.Currency != "" && !strings.EqualFold(d.Currency, "BRL")) {
			continue
		}
		monthly := d.Cost * (HoursPerMonth / 24.0) / float64(d.WindowDays)
		if d.Kind == "os" {
			os[strings.ToLower(d.Name)] += monthly
		} else {
			disks[strings.ToLower(d.Name)] += monthly
		}
	}
	for i := range r.NodePools {
		r.NodePools[i].OSDiskEffectiveBRL = round2(os[strings.ToLower(r.NodePools[i].Name)])
	}
	for i := range r.PVCs {
		r.PVCs[i].EffectiveCostBRL = round2(disks[strings.ToLower(r.PVCs[i].BoundPV)])
	}
}

// diskCosts devolve o custo mensal do disco do SO (todos os pools) e dos PVCs por namespace, pelo
// custo efetivo quando gravado (AttachDiskCost) e pelo de tabela no resto — mesma regra do
// computeCostBreakdown do frontend.
func diskCosts(r *FinOpsReport) (osBRL, pvcTotal float64, pvcByNS map[string]float64) {
	effOS, effPVC := false, false
	for _, p := range r.NodePools {
		effOS = effOS || p.OSDiskEffectiveBRL > 0
	}
	for _, p := range r.PVCs {
		effPVC = effPVC || p.EffectiveCostBRL > 0
	}
	if effOS {
		for _, p := range r.NodePools {
			if p.OSDiskEffectiveBRL > 0 {
				osBRL += p.OSDiskEffectiveBRL
			} else {
				osBRL += p.OSDiskCostBRL
			}
		}
	} else {
		osBRL = r.Storage.OSDiskCostBRL
	}
	if !effPVC {
		return osBRL, r.Storage.TotalMonthlyCostBRL, r.Storage.ByNamespace
	}
	pvcByNS = map[string]float64{}
	for _, p := range r.PVCs {
		v := p.MonthlyCostBRL
		if p.EffectiveCostBRL > 0 {
			v = p.EffectiveCostBRL
		}
		pvcByNS[p.Namespace] += v
		pvcTotal += v
	}
	return osBRL, pvcTotal, pvcByNS
}
