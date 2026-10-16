package finops

import "testing"

func TestParseCommitmentItems(t *testing.T) {
	items := []retailCommitmentItem{
		{Type: "Consumption", UnitOfMeasure: "1 Hour", RetailPrice: 0.2, ProductName: "Virtual Machines FSv2 Series",
			SavingsPlan: []struct {
				RetailPrice float64 `json:"retailPrice"`
				Term        string  `json:"term"`
			}{{RetailPrice: 0.15, Term: "1 Year"}, {RetailPrice: 0.11, Term: "3 Years"}}},
		{Type: "Consumption", UnitOfMeasure: "1 Hour", RetailPrice: 0.4, ProductName: "Virtual Machines FSv2 Series Windows"},
		{Type: "Consumption", UnitOfMeasure: "1 Hour", RetailPrice: 0.05, ProductName: "Virtual Machines FSv2 Series", MeterName: "F4s v2 Spot"},
		{Type: "Reservation", ReservationTerm: "1 Year", RetailPrice: 1200, ProductName: "Virtual Machines FSv2 Series"},
		{Type: "Reservation", ReservationTerm: "3 Years", RetailPrice: 2520, ProductName: "Virtual Machines FSv2 Series"},
	}
	p := parseCommitmentItems(items)
	if p.OnDemand != 0.2*HoursPerMonth || p.Reservation1Y != 100 || p.Reservation3Y != 70 ||
		p.SavingsPlan1Y != 0.15*HoursPerMonth || p.SavingsPlan3Y != 0.11*HoursPerMonth {
		t.Errorf("preços: %+v", p)
	}
}

func TestSuggestBaseReservations(t *testing.T) {
	prices := map[string]CommitmentPrices{"Standard_F4s_v2": {OnDemand: 146, Reservation1Y: 100, Reservation3Y: 70, SavingsPlan1Y: 120}}
	pools := []FinOpsPool{
		{Name: "calculofrete", VMSize: "Standard_F4s_v2", NodeCount: 51, Coverage: &PoolCoverage{Reservation: 0.4}},
		{Name: "frete", VMSize: "Standard_F4s_v2", NodeCount: 3},
		{Name: "monitoringspot", VMSize: "Standard_F4s_v2", NodeCount: 2},
		{Name: "zero", VMSize: "Standard_F4s_v2", NodeCount: 2},
		{Name: "coberto", VMSize: "Standard_F4s_v2", NodeCount: 4, Coverage: &PoolCoverage{Reservation: 1}},
	}
	scale := map[string]PoolScale{
		"calculofrete": {Min: 30, Autoscaling: true}, "frete": {}, "monitoringspot": {Min: 1, Autoscaling: true},
		"zero": {Min: 0, Autoscaling: true}, "coberto": {},
	}
	got := SuggestBaseReservations(pools, nil, scale, prices, 5)
	cf := got[0]
	// base 30 (min do autoscaler), coberto ≈ 0,4 × 51 = 20 → sugere 10 nodes.
	if cf.BaseNodes != 30 || cf.BaseSource != "autoscaler_min" || cf.CoveredNodes != 20 || cf.SuggestedNodes != 10 || cf.Skip != "" || len(cf.Options) != 3 {
		t.Fatalf("calculofrete: %+v", cf)
	}
	r1 := cf.Options[0]
	if r1.Kind != "reservation" || r1.Term != "1y" || r1.SavingMonth != (146-100)*5*10 || r1.CommitmentBRL != 100*5*10*12 {
		t.Errorf("reserva 1 ano: %+v", r1)
	}
	if fr := got[1]; fr.Autoscaling || fr.BaseNodes != 3 || fr.SuggestedNodes != 3 {
		t.Errorf("pool fixo: base = tamanho atual: %+v", fr)
	}
	if got[2].Skip == "" || got[3].Skip == "" || got[4].Skip == "" {
		t.Errorf("spot, min 0 e já coberto não têm sugestão: %+v %+v %+v", got[2], got[3], got[4])
	}
}

func TestNodesAtHPAMin(t *testing.T) {
	// 10 nodes de 4 vCPU / 8 GiB alocáveis.
	p := FinOpsPool{Name: "app", NodeCount: 10, AllocatableNodes: 10, AllocatableCPUMillicores: 40000, AllocatableMemoryMi: 81920}
	workloads := []FinOpsWorkload{
		// HPA 30 → min 6, 1 vCPU por pod: 6 vCPU no mínimo.
		{NodePool: "app", Pods: 30, HPAMin: 6, HPAMax: 40, HPACurrent: 30, CPURequestMillis: 1000, MemRequestMi: 512},
		// Sem HPA: 2 pods de 2 vCPU.
		{NodePool: "app", Pods: 2, CPURequestMillis: 2000, MemRequestMi: 1024},
		// DaemonSet: 500m por node sai da capacidade.
		{NodePool: "app", Pods: 10, Kind: "DaemonSet", CPURequestMillis: 500, MemRequestMi: 256},
		{NodePool: "outro", Pods: 50, CPURequestMillis: 4000},
	}
	// 10 vCPU ÷ ((4 − 0,5) × 0,85 = 2,975) = 3,36 → 4 nodes.
	if n := nodesAtHPAMin(p, workloads); n != 4 {
		t.Errorf("nodes com HPA no mínimo = %d, want 4", n)
	}
	sc := map[string]PoolScale{"app": {Min: 2, Autoscaling: true}}
	prices := map[string]CommitmentPrices{"": {OnDemand: 100, SavingsPlan1Y: 80}}
	s := SuggestBaseReservations([]FinOpsPool{p}, workloads, sc, prices, 1)[0]
	if s.BaseNodes != 4 || s.BaseSource != "hpa_min" || s.SuggestedNodes != 4 {
		t.Errorf("a base usa o HPA no mínimo quando passa do min do autoscaler: %+v", s)
	}
}
