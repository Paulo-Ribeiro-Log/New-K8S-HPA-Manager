package finops

import (
	"math"
	"testing"
	"time"
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

func TestAccrueShowback(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.Local) }
	snaps := []ShowbackSnapshot{
		{Day: day(5), Rows: []ShowbackRow{{Cluster: "c", Namespace: "pay", TotalBRL: 3650, ComputeBRL: 3650}}},
		{Day: day(10), Rows: []ShowbackRow{{Cluster: "c", Namespace: "pay", TotalBRL: 7300, ComputeBRL: 7300}, {Cluster: "c", Namespace: "novo", TotalBRL: 365}}},
	}
	rows, days := AccrueShowback(snaps, day(1), day(14))
	// Dias 5–9 a 3650/mês (120/dia) + dias 10–14 a 7300 (240/dia); 1–4 sem foto.
	if days != 10 {
		t.Errorf("dias cobertos = %d, want 10", days)
	}
	by := map[string]ShowbackRow{}
	for _, r := range rows {
		by[r.Namespace] = r
	}
	if math.Abs(by["pay"].TotalBRL-1800) > 0.01 || math.Abs(by["novo"].TotalBRL-60) > 0.01 {
		t.Errorf("acumulado: %+v", by)
	}
	if rows, days := AccrueShowback(nil, day(1), day(2)); rows != nil || days != 0 {
		t.Error("sem foto não acumula nada")
	}
}
