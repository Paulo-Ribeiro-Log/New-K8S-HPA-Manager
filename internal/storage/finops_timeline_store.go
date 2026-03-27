package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FinOpsTimelineSnapshot representa um snapshot salvo de TimelineReport
type FinOpsTimelineSnapshot struct {
	ID        string    `json:"id"`
	Cluster   string    `json:"cluster"`
	StartDate string    `json:"start_date"`
	EndDate   string    `json:"end_date"`
	Days      int       `json:"days"`
	Data      any       `json:"data"` // TimelineReport serializado
	SavedAt   time.Time `json:"saved_at"`
}

// FinOpsTimelineStore persiste snapshots de timeline HPA por cluster
type FinOpsTimelineStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const finopsTimelineSchema = `
CREATE TABLE IF NOT EXISTS finops_timeline_snapshots (
    id         TEXT PRIMARY KEY,
    cluster    TEXT NOT NULL,
    start_date TEXT NOT NULL,
    end_date   TEXT NOT NULL,
    days       INTEGER NOT NULL,
    data_json  TEXT NOT NULL,
    saved_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fts_cluster     ON finops_timeline_snapshots(cluster);
CREATE INDEX IF NOT EXISTS idx_fts_cluster_end ON finops_timeline_snapshots(cluster, end_date DESC);
CREATE INDEX IF NOT EXISTS idx_fts_saved_at    ON finops_timeline_snapshots(saved_at DESC);
`

// NewFinOpsTimelineStore abre (ou cria) o banco SQLite de snapshots FinOps.
func NewFinOpsTimelineStore(dbPath string) (*FinOpsTimelineStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório finops_timeline: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco finops_timeline: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping finops_timeline: %w", err)
	}

	if _, err := db.Exec(finopsTimelineSchema); err != nil {
		return nil, fmt.Errorf("criar schema finops_timeline: %w", err)
	}

	s := &FinOpsTimelineStore{db: db}
	go s.cleanupLoop()
	return s, nil
}

// Save persiste um snapshot de TimelineReport. O campo data pode ser qualquer
// struct serializável para JSON (tipicamente finops.TimelineReport).
func (s *FinOpsTimelineStore) Save(cluster, startDate, endDate string, days int, data any) error {
	blob, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("serializar snapshot: %w", err)
	}

	id := fmt.Sprintf("%s_%s_%s_%dd", cluster, startDate, endDate, days)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Upsert: atualiza se já existe snapshot para o mesmo período
	_, err = s.db.Exec(`
		INSERT INTO finops_timeline_snapshots (id, cluster, start_date, end_date, days, data_json, saved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			data_json = excluded.data_json,
			saved_at  = excluded.saved_at
	`, id, cluster, startDate, endDate, days, string(blob), time.Now().UTC())
	return err
}

// GetLatest retorna o snapshot mais recente para o cluster com o mesmo número de dias.
// Retorna nil, nil se não houver nenhum.
func (s *FinOpsTimelineStore) GetLatest(cluster string, days int) (*FinOpsTimelineSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, cluster, start_date, end_date, days, data_json, saved_at
		FROM finops_timeline_snapshots
		WHERE cluster = ? AND days = ?
		ORDER BY saved_at DESC
		LIMIT 1
	`, cluster, days)

	return scanSnapshot(row)
}

// GetPreviousPeriod busca o snapshot mais recente cujo end_date seja anterior
// a beforeDate. Usado para comparar o período atual com o anterior.
func (s *FinOpsTimelineStore) GetPreviousPeriod(cluster string, days int, beforeDate string) (*FinOpsTimelineSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, cluster, start_date, end_date, days, data_json, saved_at
		FROM finops_timeline_snapshots
		WHERE cluster = ? AND days = ? AND end_date < ?
		ORDER BY end_date DESC
		LIMIT 1
	`, cluster, days, beforeDate)

	return scanSnapshot(row)
}

// ListSnapshots retorna todos os snapshots de um cluster ordenados por data decrescente.
func (s *FinOpsTimelineStore) ListSnapshots(cluster string) ([]FinOpsTimelineSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, cluster, start_date, end_date, days, saved_at
		FROM finops_timeline_snapshots
		WHERE cluster = ?
		ORDER BY saved_at DESC
	`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []FinOpsTimelineSnapshot
	for rows.Next() {
		var snap FinOpsTimelineSnapshot
		if err := rows.Scan(&snap.ID, &snap.Cluster, &snap.StartDate, &snap.EndDate, &snap.Days, &snap.SavedAt); err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	return snaps, rows.Err()
}

// GetByID busca um snapshot específico pelo ID.
// Retorna nil, nil se não encontrado.
func (s *FinOpsTimelineStore) GetByID(id string) (*FinOpsTimelineSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, cluster, start_date, end_date, days, data_json, saved_at
		FROM finops_timeline_snapshots
		WHERE id = ?
	`, id)

	return scanSnapshot(row)
}

// DeleteOlderThan remove snapshots mais antigos que a data informada.
func (s *FinOpsTimelineStore) DeleteOlderThan(before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM finops_timeline_snapshots WHERE saved_at < ?`, before.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// cleanupLoop remove snapshots com mais de 90 dias a cada 24 horas.
func (s *FinOpsTimelineStore) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := s.DeleteOlderThan(time.Now().AddDate(0, 0, -90)); err == nil && n > 0 {
			_ = n // cleanup silencioso
		}
	}
}

// scanSnapshot lê uma linha completa incluindo data_json.
func scanSnapshot(row *sql.Row) (*FinOpsTimelineSnapshot, error) {
	var snap FinOpsTimelineSnapshot
	var dataJSON string
	err := row.Scan(&snap.ID, &snap.Cluster, &snap.StartDate, &snap.EndDate, &snap.Days, &dataJSON, &snap.SavedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(dataJSON), &snap.Data); err != nil {
		return nil, fmt.Errorf("deserializar snapshot: %w", err)
	}
	return &snap, nil
}
