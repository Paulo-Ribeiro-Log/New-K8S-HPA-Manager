package finops

import (
	"math"
	"testing"
)

func TestOwnerFromLabels(t *testing.T) {
	if o, l := ownerFromLabels(map[string]string{"Team": "frete", "app.kubernetes.io/part-of": "oferta"}); o != "frete" || l != "team" {
		t.Errorf("team tem preferência sobre part-of, sem diferenciar caixa: %q %q", o, l)
	}
	if o, _ := ownerFromLabels(map[string]string{"kubernetes.io/metadata.name": "x"}); o != "" {
		t.Errorf("label desconhecido não é dono: %q", o)
	}
}

func TestBuildShowback(t *testing.T) {
	r := &FinOpsReport{
		Cluster: "c",
		NodePools: []FinOpsPool{
			{Name: "res", MonthlyCostBRL: 4000, Coverage: &PoolCoverage{Reservation: 1, EffectiveCost: 1000, Currency: "BRL", WindowDays: 30}},
			{Name: "od", MonthlyCostBRL: 1000},
		},
		Namespaces: []FinOpsNamespace{{Namespace: "pay", Owner: "time-pay", OwnerLabel: "team"}},
		Workloads: []FinOpsWorkload{
			{Namespace: "pay", NodePool: "res", CostShareBRL: 2000, WasteBRL: 3000},
			{Namespace: "pay", NodePool: "od", CostShareBRL: 500, WasteBRL: 100},
			{Namespace: "frete", NodePool: "od", CostShareBRL: 300},
		},
		Storage: StorageSummary{OSDiskCostBRL: 50, ByNamespace: map[string]float64{"frete": 20}},
	}
	rows := BuildShowback(r)
	by := map[string]ShowbackRow{}
	for _, x := range rows {
		by[x.Namespace] = x
	}
	f := (1000 * (730.0 / 24) / 30) / 4000 // ≈ 0,2535
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.02 }
	pay := by["pay"]
	if pay.Owner != "time-pay" || pay.Workloads != 2 || !near(pay.ComputeBRL, 2000*f+500) || !near(pay.WasteBRL, 2000*f+100) {
		t.Errorf("pay: %+v", pay)
	}
	if fr := by["frete"]; fr.StorageBRL != 20 || fr.TotalBRL != 320 {
		t.Errorf("frete: %+v", fr)
	}
	// compute efetivo (4000f + 1000) + disco do SO 50 − alocado (2000f + 800).
	if u := by[UnallocatedNamespace]; !near(u.ComputeBRL, 2000*f+250) {
		t.Errorf("não alocado: %+v", u)
	}
	total := 0.0
	for _, x := range rows {
		total += x.TotalBRL
	}
	if !near(total, 4000*f+1000+50+20) {
		t.Errorf("a soma do showback deve fechar com o custo do cluster: %v", total)
	}
}
