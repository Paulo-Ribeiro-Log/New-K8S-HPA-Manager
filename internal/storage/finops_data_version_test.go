package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestPurgeFinOpsDataIfStale(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE a (x TEXT); CREATE TABLE b (x TEXT); INSERT INTO a VALUES ('old'); INSERT INTO b VALUES ('old')`); err != nil {
		t.Fatal(err)
	}
	count := func(table string) int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// user_version 0 (banco gravado com a semântica antiga): limpa tudo e sobe a versão.
	if err := purgeFinOpsDataIfStale(db, "a", "b"); err != nil {
		t.Fatal(err)
	}
	if count("a") != 0 || count("b") != 0 {
		t.Fatalf("dados antigos deveriam ter sido descartados: a=%d b=%d", count("a"), count("b"))
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != finopsDataVersion {
		t.Fatalf("user_version = %d (err %v), want %d", v, err, finopsDataVersion)
	}

	// Segunda chamada (já na versão atual): NÃO pode apagar dados novos.
	if _, err := db.Exec(`INSERT INTO a VALUES ('novo')`); err != nil {
		t.Fatal(err)
	}
	if err := purgeFinOpsDataIfStale(db, "a", "b"); err != nil {
		t.Fatal(err)
	}
	if count("a") != 1 {
		t.Errorf("dado gravado após a migração foi apagado indevidamente: a=%d", count("a"))
	}
}
