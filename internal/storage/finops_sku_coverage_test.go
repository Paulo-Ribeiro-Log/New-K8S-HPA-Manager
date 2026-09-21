package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSKUPricingCoverage_SnapshotPorSubscriptionEFrescor(t *testing.T) {
	st, err := NewFinOpsRightsizingStore(filepath.Join(t.TempDir(), "rs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-time.Hour)
	if !st.SKUPricingCoverageFetchedAt("subA").IsZero() {
		t.Error("subscription nunca consultada → hora zero")
	}
	a := []SKUPricingCoverage{
		{SKU: "Standard_F4s_v2", Model: "reservation", Cost: 52186, Currency: "BRL", WindowDays: 30},
		{SKU: "Standard_D8s_v5", Model: "savingsplan", Cost: 8973, Currency: "BRL", WindowDays: 30},
		{SKU: "Standard_antigo", Model: "reservation", Cost: 1, WindowDays: 30},
	}
	if err := st.ReplaceSKUPricingCoverage("subA", a, t0); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceSKUPricingCoverage("subB", []SKUPricingCoverage{{SKU: "Standard_D4s_v4", Model: "reservation", Cost: 31926}}, t0); err != nil {
		t.Fatal(err)
	}
	// Nova consulta de subA: o SKU "antigo" sumiu e não pode ficar preso.
	t1 := time.Now()
	if err := st.ReplaceSKUPricingCoverage("subA", a[:2], t1); err != nil {
		t.Fatal(err)
	}
	all, err := st.ListSKUPricingCoverage()
	if err != nil || len(all) != 3 {
		t.Fatalf("esperado 2 (subA) + 1 (subB) = 3: %+v err=%v", all, err)
	}
	if got := st.SKUPricingCoverageFetchedAt("subA"); got.Before(t1.Add(-time.Second)) {
		t.Errorf("hora da última consulta de subA não atualizou: %v", got)
	}
	// Subscription consultada SEM nenhum SKU coberto continua "consultada" (o TTL não repete a chamada).
	if err := st.ReplaceSKUPricingCoverage("subVazia", nil, t1); err != nil {
		t.Fatal(err)
	}
	if st.SKUPricingCoverageFetchedAt("subVazia").IsZero() {
		t.Error("consulta sem resultado precisa registrar a hora (senão repete a cada scan)")
	}
}
