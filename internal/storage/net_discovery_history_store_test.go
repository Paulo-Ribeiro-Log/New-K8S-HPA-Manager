package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestNetDiscoveryHistoryStore(t *testing.T) *NetDiscoveryHistoryStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "net-discovery-history-test.db")
	store, err := NewNetDiscoveryHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("NewNetDiscoveryHistoryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNetDiscoveryHistoryStore_GetRecentByTarget_NeverSeenReturnsEmptyNotNil(t *testing.T) {
	store := newTestNetDiscoveryHistoryStore(t)
	records, err := store.GetRecentByTarget("nunca-visto.example.com", 3)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if records == nil {
		t.Fatal("esperava slice vazio, não nil (evita null no JSON)")
	}
	if len(records) != 0 {
		t.Errorf("esperava 0 registros, veio %d", len(records))
	}
}

func TestNetDiscoveryHistoryStore_SaveThenGetRecentByTarget(t *testing.T) {
	store := newTestNetDiscoveryHistoryStore(t)
	now := time.Now().Truncate(time.Second)

	if err := store.Save(NetDiscoveryHistoryRecord{
		TargetInput: "Servidor.Exemplo.Com", // case misto — normalizeTarget deve tratar
		TargetIP:    "10.0.0.5",
		Mode:        "local",
		Reached:     true,
		HopsCount:   11,
		ResultJSON:  `{"target_ip":"10.0.0.5"}`,
		CreatedAt:   now,
		CreatedBy:   "user@example.com",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Busca pelo mesmo texto, mas em minúsculas — normalizeTarget precisa casar.
	records, err := store.GetRecentByTarget("servidor.exemplo.com", 3)
	if err != nil {
		t.Fatalf("GetRecentByTarget: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("esperava 1 registro, veio %d", len(records))
	}
	r := records[0]
	if r.TargetInput != "servidor.exemplo.com" {
		t.Errorf("TargetInput = %q, want normalizado", r.TargetInput)
	}
	if r.TargetIP != "10.0.0.5" || r.Mode != "local" || !r.Reached || r.HopsCount != 11 {
		t.Errorf("registro não bateu: %+v", r)
	}
	if r.ResultJSON != `{"target_ip":"10.0.0.5"}` {
		t.Errorf("ResultJSON = %q, want o JSON salvo intacto", r.ResultJSON)
	}
	if r.CreatedBy != "user@example.com" {
		t.Errorf("CreatedBy = %q, want user@example.com", r.CreatedBy)
	}
}

// TestNetDiscoveryHistoryStore_MatchesByResolvedIPToo cobre o caso do usuário alternar entre
// digitar o hostname e o IP já resolvido do mesmo host — a busca por QUALQUER um dos dois deve
// encontrar o registro salvo.
func TestNetDiscoveryHistoryStore_MatchesByResolvedIPToo(t *testing.T) {
	store := newTestNetDiscoveryHistoryStore(t)
	if err := store.Save(NetDiscoveryHistoryRecord{
		TargetInput: "meuservico.interno.com",
		TargetIP:    "192.168.50.10",
		Mode:        "pod",
		Reached:     false,
		HopsCount:   4,
		ResultJSON:  "{}",
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Buscando pelo IP resolvido (não o hostname original) — deve encontrar o mesmo registro.
	records, err := store.GetRecentByTarget("192.168.50.10", 3)
	if err != nil {
		t.Fatalf("GetRecentByTarget: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("esperava 1 registro buscando pelo IP, veio %d", len(records))
	}
	if records[0].TargetInput != "meuservico.interno.com" {
		t.Errorf("registro errado retornado: %+v", records[0])
	}
}

// TestNetDiscoveryHistoryStore_MatchesByResolvedIPCaseInsensitive — achado real de code review:
// target_ip era comparado sem normalização (SQLite '=' é case-sensitive), enquanto target_input já
// normalizava — um IPv6 digitado numa caixa diferente da persistida nunca batia. IPv6 é o caso que
// expõe isso (IPv4 não tem letras).
func TestNetDiscoveryHistoryStore_MatchesByResolvedIPCaseInsensitive(t *testing.T) {
	store := newTestNetDiscoveryHistoryStore(t)
	if err := store.Save(NetDiscoveryHistoryRecord{
		TargetInput: "meuservico.interno.com",
		TargetIP:    "2001:db8::1",
		Mode:        "pod",
		Reached:     false,
		HopsCount:   4,
		ResultJSON:  "{}",
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Buscando pelo mesmo IP, mas em CAIXA DIFERENTE da persistida.
	records, err := store.GetRecentByTarget("2001:DB8::1", 3)
	if err != nil {
		t.Fatalf("GetRecentByTarget: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("esperava 1 registro buscando pelo IPv6 em caixa diferente, veio %d", len(records))
	}
	if records[0].TargetInput != "meuservico.interno.com" {
		t.Errorf("registro errado retornado: %+v", records[0])
	}
}

// TestNetDiscoveryHistoryStore_OrdersNewestFirst confirma a ordem (mais recente primeiro) e o
// respeito ao `limit`.
func TestNetDiscoveryHistoryStore_OrdersNewestFirst(t *testing.T) {
	store := newTestNetDiscoveryHistoryStore(t)
	base := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 3; i++ {
		if err := store.Save(NetDiscoveryHistoryRecord{
			TargetInput: "alvo.exemplo.com",
			TargetIP:    "10.0.0.1",
			Mode:        "local",
			HopsCount:   i,
			ResultJSON:  "{}",
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("Save[%d]: %v", i, err)
		}
	}

	records, err := store.GetRecentByTarget("alvo.exemplo.com", 2)
	if err != nil {
		t.Fatalf("GetRecentByTarget: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("esperava 2 registros (limit), veio %d", len(records))
	}
	if records[0].HopsCount != 2 || records[1].HopsCount != 1 {
		t.Errorf("ordem errada — esperava o mais recente (HopsCount=2) primeiro, veio %+v", records)
	}
}

// TestNetDiscoveryHistoryStore_PrunesOldEntriesPerTarget cobre a retenção de 90 dias, escopada por
// alvo (mesmo padrão de snat_history_store.go) — uma entrada antiga do MESMO alvo é removida ao
// salvar uma nova; entradas de OUTRO alvo não são afetadas.
func TestNetDiscoveryHistoryStore_PrunesOldEntriesPerTarget(t *testing.T) {
	store := newTestNetDiscoveryHistoryStore(t)
	old := time.Now().Add(-100 * 24 * time.Hour) // > 90 dias

	if err := store.Save(NetDiscoveryHistoryRecord{
		TargetInput: "alvo-antigo.com", TargetIP: "10.0.0.1", Mode: "local", ResultJSON: "{}", CreatedAt: old,
	}); err != nil {
		t.Fatalf("Save antigo: %v", err)
	}
	if err := store.Save(NetDiscoveryHistoryRecord{
		TargetInput: "outro-alvo.com", TargetIP: "10.0.0.2", Mode: "local", ResultJSON: "{}", CreatedAt: old,
	}); err != nil {
		t.Fatalf("Save outro alvo: %v", err)
	}

	// Nova execução do PRIMEIRO alvo dispara a poda — só pra esse alvo.
	if err := store.Save(NetDiscoveryHistoryRecord{
		TargetInput: "alvo-antigo.com", TargetIP: "10.0.0.1", Mode: "local", ResultJSON: "{}", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save novo: %v", err)
	}

	records, err := store.GetRecentByTarget("alvo-antigo.com", 10)
	if err != nil {
		t.Fatalf("GetRecentByTarget: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("esperava só o registro novo (antigo podado), veio %d registros", len(records))
	}

	// O outro alvo, nunca re-salvo, mantém seu registro antigo intacto — a poda é escopada.
	outroRecords, err := store.GetRecentByTarget("outro-alvo.com", 10)
	if err != nil {
		t.Fatalf("GetRecentByTarget outro alvo: %v", err)
	}
	if len(outroRecords) != 1 {
		t.Errorf("registro antigo de OUTRO alvo não deveria ter sido podado, veio %d registros", len(outroRecords))
	}
}
