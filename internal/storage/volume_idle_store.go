package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s-hpa-manager/internal/dbfile"

	_ "github.com/mattn/go-sqlite3"
)

// VolumeIdleStore guarda desde quando cada volume foi visto ocioso (PVC sem pod, PV sem PVC). O
// Kubernetes não registra isso; a ferramenta só sabe desde a primeira leitura em que o volume
// apareceu ocioso, e esquece o volume assim que ele volta a ser usado ou some.
type VolumeIdleStore struct {
	db *sql.DB
	mu sync.Mutex
}

const volumeIdleSchema = `
CREATE TABLE IF NOT EXISTS volume_idle (
    cluster    TEXT     NOT NULL,
    vol_key    TEXT     NOT NULL,
    first_seen DATETIME NOT NULL,
    last_seen  DATETIME NOT NULL,
    PRIMARY KEY (cluster, vol_key)
);`

func NewVolumeIdleStore(dbPath string) (*VolumeIdleStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório volume_idle: %w", err)
	}
	dbfile.Restrict(dbPath)
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir volume_idle.db: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping volume_idle.db: %w", err)
	}
	if _, err := db.Exec(volumeIdleSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema volume_idle: %w", err)
	}
	return &VolumeIdleStore{db: db}, nil
}

// Sync recebe os volumes ociosos AGORA no cluster: os já conhecidos mantêm first_seen, os novos
// entram com now e os que não estão na lista (voltaram a ser usados ou foram apagados) saem.
func (s *VolumeIdleStore) Sync(cluster string, idleKeys []string, now time.Time) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	now = now.UTC()
	for _, k := range idleKeys {
		if _, err := tx.Exec(
			`INSERT INTO volume_idle (cluster, vol_key, first_seen, last_seen) VALUES (?,?,?,?)
			 ON CONFLICT(cluster, vol_key) DO UPDATE SET last_seen = excluded.last_seen`,
			cluster, k, now, now,
		); err != nil {
			return nil, fmt.Errorf("gravar volume_idle: %w", err)
		}
	}
	// o que não foi visto nesta leitura deixou de estar ocioso
	if _, err := tx.Exec(`DELETE FROM volume_idle WHERE cluster = ? AND last_seen < ?`, cluster, now); err != nil {
		return nil, fmt.Errorf("limpar volume_idle: %w", err)
	}
	rows, err := tx.Query(`SELECT vol_key, first_seen FROM volume_idle WHERE cluster = ?`, cluster)
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for rows.Next() {
		var k string
		var t time.Time
		if err := rows.Scan(&k, &t); err != nil {
			rows.Close()
			return nil, err
		}
		out[k] = t
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return out, tx.Commit()
}
