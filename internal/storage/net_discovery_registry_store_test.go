package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestNetDiscoveryRegistryStore(t *testing.T) *NetDiscoveryRegistryStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "net-discovery-registry-test.db")
	store, err := NewNetDiscoveryRegistryStore(dbPath)
	if err != nil {
		t.Fatalf("NewNetDiscoveryRegistryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNetDiscoveryRegistryStore_GetMissReturnsErrNoRows(t *testing.T) {
	store := newTestNetDiscoveryRegistryStore(t)
	_, err := store.Get("10.0.0.1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("esperava sql.ErrNoRows, got %v", err)
	}
}

func TestNetDiscoveryRegistryStore_UpsertThenGet(t *testing.T) {
	store := newTestNetDiscoveryRegistryStore(t)
	now := time.Now().Truncate(time.Second)

	if err := store.Upsert(NetDiscoveryIPCacheEntry{
		IP: "10.0.0.5", Kind: "pod", Name: "Deployment/checkout-api",
		Namespace: "prod", Cluster: "meu-cluster", CachedAt: now,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get("10.0.0.5")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != "pod" || got.Name != "Deployment/checkout-api" || got.Namespace != "prod" || got.Cluster != "meu-cluster" {
		t.Errorf("entrada não bateu: %+v", got)
	}
	if !got.CachedAt.Equal(now) {
		t.Errorf("CachedAt = %v, want %v", got.CachedAt, now)
	}
}

// TestNetDiscoveryRegistryStore_UpsertOverwritesExisting confirma o "cache-on-read" de verdade —
// uma nova descoberta do MESMO IP (ex: node reciclado, agora com outro kind — nunca deveria
// acontecer na prática, mas o contrato de ON CONFLICT DO UPDATE deve sempre refletir o dado mais
// recente, não empilhar linhas duplicadas nem preservar o valor antigo).
func TestNetDiscoveryRegistryStore_UpsertOverwritesExisting(t *testing.T) {
	store := newTestNetDiscoveryRegistryStore(t)
	old := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	fresh := time.Now().Truncate(time.Second)

	if err := store.Upsert(NetDiscoveryIPCacheEntry{
		IP: "10.0.0.9", Kind: "node", Name: "node-antigo", Cluster: "cluster-a", CachedAt: old,
	}); err != nil {
		t.Fatalf("primeiro Upsert: %v", err)
	}
	if err := store.Upsert(NetDiscoveryIPCacheEntry{
		IP: "10.0.0.9", Kind: "service", Name: "svc-novo", Namespace: "ns", Cluster: "cluster-b", CachedAt: fresh,
	}); err != nil {
		t.Fatalf("segundo Upsert: %v", err)
	}

	got, err := store.Get("10.0.0.9")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != "service" || got.Name != "svc-novo" || got.Namespace != "ns" || got.Cluster != "cluster-b" {
		t.Errorf("esperava o dado mais recente, got %+v", got)
	}
	if !got.CachedAt.Equal(fresh) {
		t.Errorf("CachedAt = %v, want o timestamp fresco (%v), não o antigo", got.CachedAt, fresh)
	}
}

// TestNetDiscoveryRegistryStore_EmptyNamespaceRoundTrips — entradas de Kind=node nunca têm
// Namespace (só service/pod têm); confirma que o NULL da coluna vira string vazia de volta, sem
// erro de scan (ver sql.NullString em Get).
func TestNetDiscoveryRegistryStore_EmptyNamespaceRoundTrips(t *testing.T) {
	store := newTestNetDiscoveryRegistryStore(t)
	if err := store.Upsert(NetDiscoveryIPCacheEntry{
		IP: "10.0.0.20", Kind: "node", Name: "meu-node", Cluster: "meu-cluster", CachedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get("10.0.0.20")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Namespace != "" {
		t.Errorf("Namespace = %q, want vazio pra um node", got.Namespace)
	}
}
