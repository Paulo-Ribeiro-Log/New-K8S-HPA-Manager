package finops

import (
	"strings"

	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/storage"
)

// AttachDiskCost anexa o custo efetivo mensal dos discos (Cost Management, gravado com a cobertura)
// ao relatório: disco do SO por pool e PVC pelo nome do disco (= PV, pvc-<uid>). Moeda ≠ BRL não
// entra (sem número em reais). Roda na leitura, como AttachPoolCoverage.
func AttachDiskCost(r *FinOpsReport, rows []storage.DiskCost) {
	if r == nil || len(rows) == 0 {
		return
	}
	os, disks := effectiveDiskCosts(rows)
	for i := range r.NodePools {
		r.NodePools[i].OSDiskEffectiveBRL = round2(os[strings.ToLower(r.NodePools[i].Name)])
	}
	for i := range r.PVCs {
		r.PVCs[i].EffectiveCostBRL = round2(disks[strings.ToLower(r.PVCs[i].BoundPV)])
	}
}

// effectiveDiskCosts soma o custo mensal em reais por pool (disco do SO) e por nome de disco.
func effectiveDiskCosts(rows []storage.DiskCost) (os, disks map[string]float64) {
	os, disks = map[string]float64{}, map[string]float64{}
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
	return os, disks
}

// EffectiveDiskMonthlyBRL devolve o custo efetivo mensal (R$) dos discos avulsos por nome em
// minúsculas — PVCs do Azure: pvc-<uid>. Vazio quando não há custo gravado em BRL.
func EffectiveDiskMonthlyBRL(rows []storage.DiskCost) map[string]float64 {
	_, disks := effectiveDiskCosts(rows)
	return disks
}

// EstimateDiskMonthlyUSD estima o custo mensal de um disco por preço de API/tabela (on-demand,
// sem reserva). source: api | fallback | table | unpriced | unsupported (0 USD nos dois últimos).
func EstimateDiskMonthlyUSD(d models.UnattachedDisk, p DiskPriceFuncs) (usd float64, source string) {
	return priceUnattachedDisk(d, p)
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
