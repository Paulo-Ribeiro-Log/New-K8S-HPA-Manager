package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestRightsizingStore(t *testing.T) *FinOpsRightsizingStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "finops-rightsizing.db")
	store, err := NewFinOpsRightsizingStore(dbPath)
	if err != nil {
		t.Fatalf("NewFinOpsRightsizingStore() error = %v", err)
	}
	return store
}

func TestFinOpsRightsizingStore_NeverScannedClusterReturnsFalse(t *testing.T) {
	store := newTestRightsizingStore(t)
	_, scanned := store.LastScannedAt("cluster-nunca-visto")
	if scanned {
		t.Error("esperava scanned=false para cluster nunca escaneado")
	}
}

func TestFinOpsRightsizingStore_WorkloadRecommendationsRoundTrip(t *testing.T) {
	store := newTestRightsizingStore(t)
	now := time.Now().Round(time.Second)

	recs := []WorkloadRecommendation{
		{
			Namespace: "ns1", Workload: "api", NodePool: "pool-a", Pods: 3,
			CPURequestMillis: 500, MemRequestMi: 512,
			CPULimitMillis: 1000, MemLimitMi: 1024,
			CPURecommendedMillis: 240, MemRecommendedMi: 300,
			CPULimitRecommendedMillis: 480, MemLimitRecommendedMi: 400,
			Verdict: "superprovisioned", WasteBRL: 42.5, MetricsSource: "prometheus",
			WindowDays: 30, GeneratedAt: now,
		},
		{
			Namespace: "ns2", Workload: "worker", NodePool: "pool-b", Pods: 1,
			CPURequestMillis: 100, MemRequestMi: 128,
			Verdict: "ok", GeneratedAt: now,
		},
	}

	if err := store.ReplaceWorkloadRecommendations("cluster-a", recs); err != nil {
		t.Fatalf("ReplaceWorkloadRecommendations() error = %v", err)
	}

	got, err := store.GetWorkloadRecommendations("cluster-a")
	if err != nil {
		t.Fatalf("GetWorkloadRecommendations() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}

	byWorkload := make(map[string]WorkloadRecommendation)
	for _, r := range got {
		byWorkload[r.Workload] = r
	}

	api, ok := byWorkload["api"]
	if !ok {
		t.Fatal("workload 'api' não encontrado")
	}
	if api.NodePool != "pool-a" || api.WasteBRL != 42.5 || api.MetricsSource != "prometheus" {
		t.Errorf("api recovery incorreto: %+v", api)
	}
	if api.CPULimitRecommendedMillis != 480 || api.MemLimitRecommendedMi != 400 {
		t.Errorf("api limits recomendados incorretos: cpu=%v mem=%v", api.CPULimitRecommendedMillis, api.MemLimitRecommendedMi)
	}

	lastScanned, scanned := store.LastScannedAt("cluster-a")
	if !scanned {
		t.Fatal("esperava scanned=true após ReplaceWorkloadRecommendations")
	}
	if !lastScanned.Equal(now) {
		t.Errorf("LastScannedAt = %v, want %v", lastScanned, now)
	}
}

func TestFinOpsRightsizingStore_ReplaceWorkloadRecommendationsIsFullSnapshot(t *testing.T) {
	store := newTestRightsizingStore(t)
	now := time.Now()

	// 1º scan: 2 workloads
	first := []WorkloadRecommendation{
		{Namespace: "ns1", Workload: "a", GeneratedAt: now},
		{Namespace: "ns1", Workload: "b", GeneratedAt: now},
	}
	if err := store.ReplaceWorkloadRecommendations("cluster-x", first); err != nil {
		t.Fatalf("1º Replace error = %v", err)
	}

	// 2º scan: só 1 workload (o "b" sumiu, ex: deployment deletado) — deve substituir por
	// completo, não fazer merge incremental (um workload que sumiu do cluster não deveria
	// continuar "preso" indefinidamente na análise persistida).
	second := []WorkloadRecommendation{
		{Namespace: "ns1", Workload: "a", GeneratedAt: now.Add(time.Hour)},
	}
	if err := store.ReplaceWorkloadRecommendations("cluster-x", second); err != nil {
		t.Fatalf("2º Replace error = %v", err)
	}

	got, err := store.GetWorkloadRecommendations("cluster-x")
	if err != nil {
		t.Fatalf("GetWorkloadRecommendations() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (workload 'b' deveria ter sido removido no 2º scan)", len(got))
	}
	if got[0].Workload != "a" {
		t.Errorf("Workload = %q, want %q", got[0].Workload, "a")
	}
}

func TestFinOpsRightsizingStore_NodePoolTierSuggestionsRoundTrip(t *testing.T) {
	store := newTestRightsizingStore(t)
	now := time.Now().Round(time.Second)

	suggestions := []NodePoolTierSuggestion{
		{
			NodePool: "pool-a", CurrentSKU: "Standard_D8s_v5",
			CPUUtilPct: 22.5, MemUtilPct: 61.2, WorkloadCount: 7,
			AlternativesJSON: `[{"vm_size":"Standard_D4s_v5","verdict":"cheaper"}]`,
			GeneratedAt:      now,
		},
	}
	if err := store.ReplaceNodePoolTierSuggestions("cluster-y", suggestions); err != nil {
		t.Fatalf("ReplaceNodePoolTierSuggestions() error = %v", err)
	}

	got, err := store.GetNodePoolTierSuggestions("cluster-y")
	if err != nil {
		t.Fatalf("GetNodePoolTierSuggestions() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].CurrentSKU != "Standard_D8s_v5" || got[0].WorkloadCount != 7 {
		t.Errorf("got[0] incorreto: %+v", got[0])
	}
	if got[0].AlternativesJSON == "" {
		t.Error("AlternativesJSON não deveria vir vazio")
	}
}

func TestFinOpsRightsizingStore_DifferentClustersAreIsolated(t *testing.T) {
	store := newTestRightsizingStore(t)
	now := time.Now()

	if err := store.ReplaceWorkloadRecommendations("cluster-1", []WorkloadRecommendation{
		{Namespace: "ns", Workload: "w1", GeneratedAt: now},
	}); err != nil {
		t.Fatalf("Replace cluster-1 error = %v", err)
	}

	// cluster-2 nunca foi escaneado — não deve "ver" nada do cluster-1.
	_, scanned := store.LastScannedAt("cluster-2")
	if scanned {
		t.Error("cluster-2 não deveria aparecer como escaneado (isolamento entre clusters)")
	}
	got, err := store.GetWorkloadRecommendations("cluster-2")
	if err != nil {
		t.Fatalf("GetWorkloadRecommendations(cluster-2) error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0 (cluster-2 isolado do cluster-1)", len(got))
	}
}
