package dbfile

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func modo(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return fi.Mode().Perm()
}

func TestRestrictCriaEcorrige(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "novo.db")
	Restrict(db) // arquivo novo
	if got := modo(t, db); got != 0600 {
		t.Errorf("novo: %o, quer 600", got)
	}
	for _, p := range []string{db, db + "-wal", db + "-shm"} { // instalação antiga, aberta
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0644); err != nil {
			t.Fatal(err)
		}
	}
	Restrict(db)
	for _, p := range []string{db, db + "-wal", db + "-shm"} {
		if got := modo(t, p); got != 0600 {
			t.Errorf("%s: %o, quer 600", filepath.Base(p), got)
		}
	}
}

// Banco em memória ou URI "file:" não pode virar um arquivo no diretório atual.
func TestRestrictIgnoraMemoria(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	for _, p := range []string{":memory:", "file::memory:?cache=shared", "file:x.db?mode=memory", ""} {
		Restrict(p)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("criou arquivos: %v", entries)
	}
}

// O ponto central: com 0600 o dono continua lendo e gravando, também em WAL.
func TestRestrictDonoLeEGrava(t *testing.T) {
	db := filepath.Join(t.TempDir(), "rw.db")
	Restrict(db)
	conn, err := sql.Open("sqlite3", db+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatalf("criar tabela: %v", err)
	}
	if _, err := conn.Exec("INSERT INTO t (v) VALUES ('ok')"); err != nil {
		t.Fatalf("gravar: %v", err)
	}
	var v string
	if err := conn.QueryRow("SELECT v FROM t").Scan(&v); err != nil || v != "ok" {
		t.Fatalf("ler: %q %v", v, err)
	}
	Restrict(db) // com a conexão aberta e o -wal existente
	if _, err := conn.Exec("INSERT INTO t (v) VALUES ('depois')"); err != nil {
		t.Fatalf("gravar depois do chmod: %v", err)
	}
	for _, p := range []string{db, db + "-wal", db + "-shm"} {
		if _, err := os.Stat(p); err == nil && modo(t, p) != 0600 {
			t.Errorf("%s: %o, quer 600", filepath.Base(p), modo(t, p))
		}
	}
}

func TestPrivateDirCriaECorrige(t *testing.T) {
	base := t.TempDir()

	novo := filepath.Join(base, "a", "b") // não existe: cria 0700 (inclusive os pais)
	PrivateDir(novo)
	if got := modo(t, novo); got != 0700 {
		t.Errorf("novo: %o, quer 700", got)
	}

	antigo := filepath.Join(base, "antigo") // instalação antiga, aberta
	if err := os.Mkdir(antigo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(antigo, 0755); err != nil {
		t.Fatal(err)
	}
	PrivateDir(antigo)
	if got := modo(t, antigo); got != 0700 {
		t.Errorf("antigo: %o, quer 700", got)
	}
}

// O dono continua listando, criando e lendo arquivos dentro da pasta 0700 (inclusive o que já
// existia antes do chmod).
func TestPrivateDirDonoContinuaUsando(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessao")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	pre := filepath.Join(dir, "Cookies")
	if err := os.WriteFile(pre, []byte("antes"), 0600); err != nil {
		t.Fatal(err)
	}

	PrivateDir(dir)

	if b, err := os.ReadFile(pre); err != nil || string(b) != "antes" {
		t.Fatalf("ler arquivo antigo: %q %v", b, err)
	}
	novo := filepath.Join(dir, "novo.txt")
	if err := os.WriteFile(novo, []byte("depois"), 0600); err != nil {
		t.Fatalf("gravar: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("listar: %d entradas, err=%v", len(entries), err)
	}
	if err := os.RemoveAll(dir); err != nil { // e recriar, como o extrator faz a cada extração
		t.Fatal(err)
	}
	PrivateDir(dir)
	if got := modo(t, dir); got != 0700 {
		t.Errorf("recriada: %o, quer 700", got)
	}
}

func TestPrivateDirVazioNaoFazNada(t *testing.T) {
	PrivateDir("") // não pode criar/alterar nada nem entrar em pânico
}
