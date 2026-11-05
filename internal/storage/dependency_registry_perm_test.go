package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// O banco guarda o conteúdo decodificado de Secrets: precisa ficar legível só pelo dono, também
// quando já existia com permissão aberta (instalações anteriores) e nos arquivos -wal/-shm.
func TestRestrictDependencyRegistryPermissions(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "dependency-registry.db")

	restrictDependencyRegistryPermissions(db) // arquivo novo
	assertMode(t, db, 0600)

	for _, p := range []string{db, db + "-wal", db + "-shm"} { // instalação antiga, aberta
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0644); err != nil {
			t.Fatal(err)
		}
	}
	restrictDependencyRegistryPermissions(db)
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
