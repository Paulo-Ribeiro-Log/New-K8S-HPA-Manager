package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestHTTPClientExecutionStore(t *testing.T) (*HTTPClientExecutionStore, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "http_client_executions.db")
	store, err := NewHTTPClientExecutionStore(dbPath)
	if err != nil {
		t.Fatalf("NewHTTPClientExecutionStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dbPath
}

func TestHTTPClientExecutionStore_AddListDelete(t *testing.T) {
	store, dbPath := newTestHTTPClientExecutionStore(t)

	if info, err := os.Stat(dbPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissão do banco = %v (err %v), quer 0600 — headers podem ter tokens", info.Mode().Perm(), err)
	}

	for i, u := range []string{"ana@corp", "bia@corp", "ana@corp"} {
		e := &HTTPClientExecution{UserEmail: u, Request: `{"method":"GET"}`, Method: "GET", URL: "https://x/" + string(rune('a'+i)), Status: 200}
		if err := store.Add(e); err != nil {
			t.Fatal(err)
		}
		if e.ID == 0 {
			t.Fatal("Add não preencheu o ID")
		}
	}

	ana, err := store.List("ana@corp", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ana) != 2 || ana[0].URL != "https://x/c" || ana[1].URL != "https://x/a" {
		t.Fatalf("lista da ana = %+v, quer [c, a] (mais recente primeiro, sem as da bia)", ana)
	}

	if ok, _ := store.Delete("bia@corp", ana[0].ID); ok {
		t.Error("bia removeu execução da ana")
	}
	if ok, err := store.Delete("ana@corp", ana[0].ID); !ok || err != nil {
		t.Fatalf("Delete = %v, %v", ok, err)
	}
	if ana, _ = store.List("ana@corp", 0); len(ana) != 1 {
		t.Fatalf("depois do delete: %d execuções, quer 1", len(ana))
	}
}

func TestHTTPClientExecutionStore_KeepLimit(t *testing.T) {
	store, _ := newTestHTTPClientExecutionStore(t)
	for i := 0; i < HTTPClientExecutionsKeep+5; i++ {
		if err := store.Add(&HTTPClientExecution{UserEmail: "ana@corp", Request: "{}", Method: "GET", URL: "https://x"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Add(&HTTPClientExecution{UserEmail: "bia@corp", Request: "{}", Method: "GET", URL: "https://x"}); err != nil {
		t.Fatal(err)
	}
	ana, _ := store.List("ana@corp", 1000)
	if len(ana) != HTTPClientExecutionsKeep {
		t.Errorf("ana tem %d execuções, quer %d", len(ana), HTTPClientExecutionsKeep)
	}
	if bia, _ := store.List("bia@corp", 0); len(bia) != 1 {
		t.Errorf("limite de um usuário apagou execução de outro: bia tem %d", len(bia))
	}
}
