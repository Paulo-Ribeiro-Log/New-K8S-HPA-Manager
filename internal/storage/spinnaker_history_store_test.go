package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestSpinnakerHistoryStore(t *testing.T) *SpinnakerHistoryStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "spinnaker-history-test.db")
	store, err := NewSpinnakerHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewSpinnakerHistoryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestSpinnakerHistoryStore_UpsertAndGet cobre o caso de uso principal: persistir um match real
// e recuperá-lo depois — a razão de existir deste store (sobreviver à janela curta de busca do
// Gate, ver comentário em SpinnakerRolloutRecord).
func TestSpinnakerHistoryStore_UpsertAndGet(t *testing.T) {
	store := newTestSpinnakerHistoryStore(t)

	rec := SpinnakerRolloutRecord{
		Cluster:               "akspriv-logreversa-prd-admin",
		Namespace:             "dat-prd",
		DeploymentName:        "dat-documento-vendas-api",
		IsRollback:            false,
		LastCHGApplied:        "CHG0478175",
		LastCHGAppliedURL:     "https://viavarejo.service-now.com/change_request.do?sys_id=abc",
		PipelineExecutedAt:    1785229410605,
		ExecutionStatus:       "SUCCEEDED",
		SpinnakerExecutionID:  "01KYKZE041AJCRWFQFGSC17R5C",
		SpinnakerExecutionURL: "https://spinnaker-prd.viavarejo.com.br/#/projects/x",
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(rec.Cluster, rec.Namespace, rec.DeploymentName)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastCHGApplied != "CHG0478175" {
		t.Errorf("LastCHGApplied = %q, want CHG0478175", got.LastCHGApplied)
	}
	if got.LastCHGAppliedURL != rec.LastCHGAppliedURL {
		t.Errorf("LastCHGAppliedURL = %q, want %q", got.LastCHGAppliedURL, rec.LastCHGAppliedURL)
	}
	if got.ExecutionStatus != "SUCCEEDED" {
		t.Errorf("ExecutionStatus = %q, want SUCCEEDED", got.ExecutionStatus)
	}
	if got.IsRollback {
		t.Error("IsRollback = true, want false")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt não foi preenchido")
	}
}

// TestSpinnakerHistoryStore_UpsertOverwritesPreviousValue confirma "a cada novo scan esses dados
// devem ser atualizados" — um segundo Upsert pro mesmo deployment substitui o valor antigo, não
// acumula histórico (é sempre "o último confirmado", não um diário como Notes).
func TestSpinnakerHistoryStore_UpsertOverwritesPreviousValue(t *testing.T) {
	store := newTestSpinnakerHistoryStore(t)

	base := SpinnakerRolloutRecord{Cluster: "c", Namespace: "ns", DeploymentName: "app"}

	first := base
	first.LastCHGApplied = "CHG0000001"
	first.ExecutionStatus = "SUCCEEDED"
	if err := store.Upsert(first); err != nil {
		t.Fatalf("primeiro Upsert: %v", err)
	}

	second := base
	second.LastCHGApplied = "CHG0000002"
	second.ExecutionStatus = "SUCCEEDED"
	second.IsRollback = true
	second.RollbackType = "implicit"
	if err := store.Upsert(second); err != nil {
		t.Fatalf("segundo Upsert: %v", err)
	}

	got, err := store.Get("c", "ns", "app")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastCHGApplied != "CHG0000002" {
		t.Errorf("LastCHGApplied = %q, want CHG0000002 (deveria ter sido sobrescrito pelo scan mais recente)", got.LastCHGApplied)
	}
	if !got.IsRollback {
		t.Error("IsRollback = false, want true (valor do 2º scan)")
	}
}

func TestSpinnakerHistoryStore_Get_NotFound(t *testing.T) {
	store := newTestSpinnakerHistoryStore(t)

	_, err := store.Get("c", "ns", "nunca-visto")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Get de deployment inexistente = %v, want sql.ErrNoRows", err)
	}
}

// TestSpinnakerHistoryStore_GetAll_ScopedByClusterAndNamespace cobre a query usada como fallback
// em lote — cluster+namespace corretos, e não vaza registros de outro cluster/namespace.
func TestSpinnakerHistoryStore_GetAll_ScopedByClusterAndNamespace(t *testing.T) {
	store := newTestSpinnakerHistoryStore(t)

	recs := []SpinnakerRolloutRecord{
		{Cluster: "c1", Namespace: "ns1", DeploymentName: "app-a", LastCHGApplied: "CHG1"},
		{Cluster: "c1", Namespace: "ns1", DeploymentName: "app-b", LastCHGApplied: "CHG2"},
		{Cluster: "c1", Namespace: "ns2", DeploymentName: "app-c", LastCHGApplied: "CHG3"},
		{Cluster: "c2", Namespace: "ns1", DeploymentName: "app-d", LastCHGApplied: "CHG4"},
	}
	for _, r := range recs {
		if err := store.Upsert(r); err != nil {
			t.Fatalf("Upsert %s: %v", r.DeploymentName, err)
		}
	}

	got, err := store.GetAll("c1", "ns1")
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetAll(c1, ns1) retornou %d registros, want 2 (%v)", len(got), got)
	}
	if _, ok := got["app-a/ns1"]; !ok {
		t.Error("esperava app-a/ns1 no resultado")
	}
	if _, ok := got["app-b/ns1"]; !ok {
		t.Error("esperava app-b/ns1 no resultado")
	}

	// namespace vazio = todos os namespaces do cluster
	gotAllNS, err := store.GetAll("c1", "")
	if err != nil {
		t.Fatalf("GetAll(c1, \"\"): %v", err)
	}
	if len(gotAllNS) != 3 {
		t.Fatalf("GetAll(c1, \"\") retornou %d registros, want 3", len(gotAllNS))
	}
}

// Garante que UpdatedAt sobrevive ao round-trip via SQLite (DATETIME) sem perder precisão a
// ponto de ficar zero/inválido — usado pelo frontend pra rotular "dado de X atrás".
func TestSpinnakerHistoryStore_UpdatedAtSurvivesRoundTrip(t *testing.T) {
	store := newTestSpinnakerHistoryStore(t)
	before := time.Now().UTC()

	if err := store.Upsert(SpinnakerRolloutRecord{Cluster: "c", Namespace: "ns", DeploymentName: "app"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get("c", "ns", "app")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UpdatedAt.Before(before.Add(-time.Second)) {
		t.Errorf("UpdatedAt = %v, esperava próximo de %v", got.UpdatedAt, before)
	}
}
