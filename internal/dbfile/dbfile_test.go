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
