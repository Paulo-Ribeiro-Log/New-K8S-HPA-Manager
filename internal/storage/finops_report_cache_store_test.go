package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestFinOpsReportCacheStore(t *testing.T) *FinOpsReportCacheStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "finops-report-cache.db")
	store, err := NewFinOpsReportCacheStore(dbPath)
	if err != nil {
		t.Fatalf("NewFinOpsReportCacheStore() error = %v", err)
	}
	return store
}

func TestFinOpsReportCacheStore_NeverScannedClusterReturnsNotFound(t *testing.T) {
	store := newTestFinOpsReportCacheStore(t)
	_, _, found, err := store.Get("cluster-nunca-visto")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if found {
		t.Error("esperava found=false para cluster nunca escaneado")
	}
}

func TestFinOpsReportCacheStore_SaveAndGetRoundTrip(t *testing.T) {
	store := newTestFinOpsReportCacheStore(t)
	now := time.Now().Round(time.Second)
	reportJSON := []byte(`{"cluster":"cluster-a","summary":{"workloads_analyzed":3}}`)

	if err := store.Save("cluster-a", reportJSON, now); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, generatedAt, found, err := store.Get("cluster-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("esperava found=true depois de Save")
	}
	if string(got) != string(reportJSON) {
		t.Errorf("report_json = %s, quer %s", got, reportJSON)
	}
	if !generatedAt.Equal(now) {
		t.Errorf("generated_at = %v, quer %v", generatedAt, now)
	}
}

func TestFinOpsReportCacheStore_SaveOverwritesPreviousReport(t *testing.T) {
	store := newTestFinOpsReportCacheStore(t)
	t1 := time.Now().Add(-time.Hour).Round(time.Second)
	t2 := time.Now().Round(time.Second)

	if err := store.Save("cluster-a", []byte(`{"v":1}`), t1); err != nil {
		t.Fatalf("Save() 1 error = %v", err)
	}
	if err := store.Save("cluster-a", []byte(`{"v":2}`), t2); err != nil {
		t.Fatalf("Save() 2 error = %v", err)
	}

	got, generatedAt, found, err := store.Get("cluster-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !found {
		t.Fatal("esperava found=true")
	}
	if string(got) != `{"v":2}` {
		t.Errorf("esperava o relatório MAIS RECENTE (v:2), veio %s", got)
	}
	if !generatedAt.Equal(t2) {
		t.Errorf("generated_at = %v, quer %v (do save mais recente)", generatedAt, t2)
	}
}

func TestFinOpsReportCacheStore_DifferentClustersAreIndependent(t *testing.T) {
	store := newTestFinOpsReportCacheStore(t)
	now := time.Now().Round(time.Second)

	if err := store.Save("cluster-a", []byte(`{"cluster":"a"}`), now); err != nil {
		t.Fatalf("Save() cluster-a error = %v", err)
	}

	_, _, found, err := store.Get("cluster-b")
	if err != nil {
		t.Fatalf("Get() cluster-b error = %v", err)
	}
	if found {
		t.Error("cluster-b não deveria ter cache — só cluster-a foi salvo")
	}
}
