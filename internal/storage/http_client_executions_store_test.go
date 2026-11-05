package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// ── segredos em repouso ──────────────────────────────────────────────────────────────────────

const (
	segToken  = "SEGREDO-TOKEN-HEADER-123"
	segQuery  = "SEGREDO-QUERY-456"
	segSenha  = "SEGREDO-SENHA-789"
	segCorpo  = "SEGREDO-CORPO-000"
	reqSecret = `{"method":"POST","url":"https://api.corp/v1/x?api_key=` + segQuery + `","headers":[{"key":"Authorization","value":"Bearer ` + segToken + `"}],"body":"{\"senha\":\"` + segCorpo + `\"}"}`
)

// bancoBruto devolve o conteúdo de TODOS os arquivos do banco (db, -wal, -shm), como um leitor
// externo do disco veria.
func bancoBruto(t *testing.T, dbPath string) string {
	t.Helper()
	var all []byte
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if b, err := os.ReadFile(p); err == nil {
			all = append(all, b...)
		}
	}
	return string(all)
}

func TestHTTPClientExecutionStore_NaoGravaSegredoEmTextoPuro(t *testing.T) {
	store, dbPath := newTestHTTPClientExecutionStore(t)

	e := &HTTPClientExecution{
		UserEmail: "ana@corp", Request: reqSecret, Method: "POST",
		URL:   "https://user:" + segSenha + "@api.corp/v1/x?api_key=" + segQuery,
		Error: `Post "https://api.corp/v1/x?api_key=` + segQuery + `": dial tcp: connection refused`,
	}
	if err := store.Add(e); err != nil {
		t.Fatal(err)
	}

	raw := bancoBruto(t, dbPath)
	for _, seg := range []string{segToken, segQuery, segSenha, segCorpo} {
		if strings.Contains(raw, seg) {
			t.Errorf("o arquivo do banco contém %q em texto puro", seg)
		}
	}
}

// O "re-executar" depende da requisição inteira: a leitura devolve exatamente o que foi gravado.
func TestHTTPClientExecutionStore_ListDevolveRequisicaoCompleta(t *testing.T) {
	store, _ := newTestHTTPClientExecutionStore(t)
	e := &HTTPClientExecution{UserEmail: "ana@corp", Request: reqSecret, Method: "POST",
		URL: "https://api.corp/v1/x?api_key=" + segQuery}
	if err := store.Add(e); err != nil {
		t.Fatal(err)
	}

	list, err := store.List("ana@corp", 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %d itens, err=%v", len(list), err)
	}
	if list[0].Request != reqSecret {
		t.Errorf("Request alterado:\n got  %s\n want %s", list[0].Request, reqSecret)
	}
	if strings.Contains(list[0].URL, segQuery) || list[0].URL != "https://api.corp/v1/x?…" {
		t.Errorf("URL exibida = %q, quer a URL sem query", list[0].URL)
	}
}

// Linhas gravadas por versões anteriores (JSON em texto puro, URL com token) são protegidas ao abrir
// o banco, e continuam legíveis na lista.
func TestHTTPClientExecutionStore_MigraLinhasAntigas(t *testing.T) {
	store, dbPath := newTestHTTPClientExecutionStore(t)
	if _, err := store.db.Exec(
		`INSERT INTO http_client_executions (user_email, created_at, request, method, url, error) VALUES (?,?,?,?,?,?)`,
		"ana@corp", time.Now().UTC(), reqSecret, "POST",
		"https://api.corp/v1/x?api_key="+segQuery,
		`Get "https://api.corp/v1/x?api_key=`+segQuery+`": timeout`,
	); err != nil {
		t.Fatal(err)
	}
	store.Close()

	reaberto, err := NewHTTPClientExecutionStore(dbPath) // a migração roda na abertura
	if err != nil {
		t.Fatal(err)
	}
	defer reaberto.Close()
	if _, err := reaberto.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}

	raw := bancoBruto(t, dbPath)
	for _, seg := range []string{segToken, segQuery, segCorpo} {
		if strings.Contains(raw, seg) {
			t.Errorf("linha antiga continua com %q em texto puro", seg)
		}
	}
	list, err := reaberto.List("ana@corp", 0)
	if err != nil || len(list) != 1 || list[0].Request != reqSecret {
		t.Fatalf("a linha migrada deveria continuar legível: %v %+v", err, list)
	}
	if strings.Contains(list[0].Error, segQuery) {
		t.Errorf("erro ainda traz a query: %q", list[0].Error)
	}
}

func TestHTTPClientExecutionStore_Idempotente(t *testing.T) {
	store, dbPath := newTestHTTPClientExecutionStore(t)
	if err := store.Add(&HTTPClientExecution{UserEmail: "ana@corp", Request: reqSecret, Method: "POST", URL: "https://a/b"}); err != nil {
		t.Fatal(err)
	}
	var antes string
	_ = store.db.QueryRow(`SELECT request FROM http_client_executions`).Scan(&antes)
	store.Close()

	for i := 0; i < 2; i++ { // reabrir não pode recifrar o que já está cifrado
		s2, err := NewHTTPClientExecutionStore(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		var depois string
		_ = s2.db.QueryRow(`SELECT request FROM http_client_executions`).Scan(&depois)
		if depois != antes {
			t.Errorf("reabertura %d alterou a linha já cifrada", i+1)
		}
		s2.Close()
	}
}

// Se a chave sumir (ex.: .secret trocado), a linha não decifra: sai da lista com request vazio (o
// handler a omite) em vez de derrubar a listagem inteira.
func TestHTTPClientExecutionStore_SemChaveNaoQuebraALista(t *testing.T) {
	store, dbPath := newTestHTTPClientExecutionStore(t)
	if err := store.Add(&HTTPClientExecution{UserEmail: "ana@corp", Request: reqSecret, Method: "GET", URL: "https://a/b"}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := os.Remove(filepath.Join(filepath.Dir(dbPath), ".secret")); err != nil {
		t.Fatal(err)
	}

	s2, err := NewHTTPClientExecutionStore(dbPath) // gera uma chave nova
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	list, err := s2.List("ana@corp", 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %d itens, err=%v", len(list), err)
	}
	if list[0].Request != "" {
		t.Errorf("sem a chave certa o request deveria vir vazio, veio %q", list[0].Request)
	}
}

func TestRedactURL(t *testing.T) {
	casos := map[string]string{
		"https://api.corp/v1/x":                       "https://api.corp/v1/x",
		"https://api.corp/v1/x?token=abc":             "https://api.corp/v1/x?…",
		"https://u:p@api.corp:8443/v1?a=1#frag":       "https://api.corp:8443/v1?…",
		"http://svc.ns.svc.cluster.local:8080/health": "http://svc.ns.svc.cluster.local:8080/health",
		"não é uma url":                               "não é uma url",
		"":                                            "",
		`Get "https://h/p?k=SEGREDO": dial tcp: refused`: `Get "https://h/p?…": dial tcp: refused`,
	}
	for in, want := range casos {
		got := redactURL(in)
		if strings.Contains(in, `"`) { // texto livre passa por redactURLsInText
			got = redactURLsInText(in)
		}
		if got != want {
			t.Errorf("redact(%q) = %q, quer %q", in, got, want)
		}
	}
}
