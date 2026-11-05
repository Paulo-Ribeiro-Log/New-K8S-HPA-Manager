package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// Bancos com credenciais precisam ficar legíveis só pelo dono, também
// quando já existia com permissão aberta (instalações anteriores) e nos arquivos -wal/-shm.
func TestRestrictSQLitePermissions(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "dependency-registry.db")

	restrictSQLitePermissions(db) // arquivo novo
	assertMode(t, db, 0600)

	for _, p := range []string{db, db + "-wal", db + "-shm"} { // instalação antiga, aberta
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0644); err != nil {
			t.Fatal(err)
		}
	}
	restrictSQLitePermissions(db)
	for _, p := range []string{db, db + "-wal", db + "-shm"} {
		assertMode(t, p, 0600)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Errorf("%s: permissão %o, quer %o", filepath.Base(path), got, want)
	}
}

// O banco de tokens de IA e do Dynatrace (ai_diagnostics.db) passa por NewSQLiteClient: tem que
// nascer 0600, e um arquivo antigo aberto (0644) é corrigido ao abrir.
func TestNewSQLiteClientRestringePermissao(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "ai_diagnostics.db")
	if err := os.WriteFile(db, nil, 0644); err != nil { // instalação antiga
		t.Fatal(err)
	}
	if err := os.Chmod(db, 0644); err != nil {
		t.Fatal(err)
	}

	client, err := NewSQLiteClient(db)
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	defer client.Close()

	assertMode(t, db, 0600)
	if _, err := os.Stat(db + "-wal"); err == nil {
		assertMode(t, db+"-wal", 0600)
	}
}
