package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"k8s-hpa-manager/internal/dbfile"

	_ "github.com/mattn/go-sqlite3"
)

// VolumeScanStore guarda quais clusters entram na varredura periódica de volumes e o último
// resultado de cada um (JSON opaco: quem escreve é o handler).
type VolumeScanStore struct {
	db *sql.DB
	mu sync.Mutex
}

// VolumeScanRow é o último resultado gravado de um cluster.
type VolumeScanRow struct {
	ScannedAt time.Time
	Payload   string
}

const volumeScanSchema = `
CREATE TABLE IF NOT EXISTS volume_scan_watch (
    cluster TEXT PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS volume_scan_result (
    cluster    TEXT PRIMARY KEY,
    scanned_at DATETIME NOT NULL,
    payload    TEXT     NOT NULL
);`

func NewVolumeScanStore(dbPath string) (*VolumeScanStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório volume_scan: %w", err)
	}
	dbfile.Restrict(dbPath)
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir volume_scan.db: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping volume_scan.db: %w", err)
	}
	if _, err := db.Exec(volumeScanSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema volume_scan: %w", err)
	}
	return &VolumeScanStore{db: db}, nil
}

// SetWatched troca a lista de clusters varridos; resultados de quem saiu da lista são apagados.
func (s *VolumeScanStore) SetWatched(clusters []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM volume_scan_watch`); err != nil {
		return err
	}
	for _, c := range clusters {
		if c == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO volume_scan_watch (cluster) VALUES (?)`, c); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM volume_scan_result WHERE cluster NOT IN (SELECT cluster FROM volume_scan_watch)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *VolumeScanStore) Watched() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT cluster FROM volume_scan_watch`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func (s *VolumeScanStore) SaveResult(cluster string, at time.Time, payload string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO volume_scan_result (cluster, scanned_at, payload) VALUES (?,?,?)
		 ON CONFLICT(cluster) DO UPDATE SET scanned_at = excluded.scanned_at, payload = excluded.payload`,
		cluster, at.UTC(), payload)
	return err
}

func (s *VolumeScanStore) Results() (map[string]VolumeScanRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT cluster, scanned_at, payload FROM volume_scan_result`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]VolumeScanRow{}
	for rows.Next() {
		var c string
		var r VolumeScanRow
		if err := rows.Scan(&c, &r.ScannedAt, &r.Payload); err != nil {
			return nil, err
		}
		out[c] = r
	}
	return out, rows.Err()
}
