package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPoolPricingCoverage_ReplaceEhSnapshotPorCluster(t *testing.T) {
	st, err := NewFinOpsRightsizingStore(filepath.Join(t.TempDir(), "rs.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a := []PoolPricingCoverage{
		{NodePool: "calculofrete", Model: "reservation", Cost: 15400, Currency: "BRL", WindowDays: 30, FetchedAt: now},
		{NodePool: "calculofrete", Model: "ondemand", Cost: 141, Currency: "BRL", WindowDays: 30, FetchedAt: now},
		{NodePool: "antigo", Model: "spot", Cost: 5, Currency: "BRL", WindowDays: 30, FetchedAt: now},
	}
	if err := st.ReplacePoolPricingCoverage("c1", a); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplacePoolPricingCoverage("c2", []PoolPricingCoverage{{NodePool: "outro", Model: "ondemand", Cost: 1, FetchedAt: now}}); err != nil {
		t.Fatal(err)
	}

	// Nova atualização de c1: o pool "antigo" sumiu do cluster e não pode ficar preso.
	if err := st.ReplacePoolPricingCoverage("c1", a[:2]); err != nil {
		t.Fatal(err)
	}
	c1, err := st.ListPoolPricingCoverage("c1")
	if err != nil || len(c1) != 2 {
		t.Fatalf("c1 deveria ter 2 linhas (pool antigo removido): %+v err=%v", c1, err)
	}
	if c1[0].Currency != "BRL" || c1[0].WindowDays != 30 {
		t.Errorf("moeda/janela não persistidas: %+v", c1[0])
	}
	if c2, _ := st.ListPoolPricingCoverage("c2"); len(c2) != 1 {
		t.Errorf("atualizar c1 não pode mexer em c2: %+v", c2)
	}
	if none, err := st.ListPoolPricingCoverage("nunca"); err != nil || none == nil || len(none) != 0 {
		t.Errorf("cluster nunca consultado → slice vazio não-nil: %v %v", none, err)
	}
}
