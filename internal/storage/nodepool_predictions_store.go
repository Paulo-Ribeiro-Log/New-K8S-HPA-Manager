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

// NodePoolPredictionRecord representa um registro de análise preditiva de node pool
type NodePoolPredictionRecord struct {
	ID            string    `json:"id"`
	Cluster       string    `json:"cluster"`
	NodePoolName  string    `json:"nodepool_name"`
	HealthScore   int       `json:"health_score"`
	RiskLevel     string    `json:"risk_level"`
	ActionSummary string    `json:"action_summary"` // JSON serializado
	CostAnalysis  string    `json:"cost_analysis"`  // JSON serializado (nullable)
	FullResult    string    `json:"full_result"`    // JSON completo do NodePoolPredictionResult
	Provider      string    `json:"provider"`
	Model         string    `json:"model,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	UserEmail     string    `json:"user_email,omitempty"`
	AnalyzedAt    time.Time `json:"analyzed_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// NodePoolPredictionQueryFilters filtros para consultas de histórico
type NodePoolPredictionQueryFilters struct {
	Cluster      string
	NodePoolName string
	UserEmail    string
	StartDate    *time.Time
	EndDate      *time.Time
	Limit        int
	Offset       int
}

// NodePoolPredictionsStore armazena histórico de análises preditivas de node pools
// Usa banco SQLite dedicado (nodepool_predictions.db)
type NodePoolPredictionsStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const nodePoolPredictionsSchema = `
CREATE TABLE IF NOT EXISTS nodepool_predictions (
    id            TEXT PRIMARY KEY,
    cluster       TEXT NOT NULL,
    nodepool_name TEXT NOT NULL,
    health_score  INTEGER NOT NULL,
    risk_level    TEXT NOT NULL,
    action_summary TEXT NOT NULL,
    cost_analysis  TEXT,
    full_result    TEXT NOT NULL,
    provider       TEXT NOT NULL,
    model          TEXT,
    duration_ms    INTEGER NOT NULL,
    user_email     TEXT,
    analyzed_at    DATETIME NOT NULL,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_np_cluster_pool ON nodepool_predictions(cluster, nodepool_name);
CREATE INDEX IF NOT EXISTS idx_np_analyzed_at  ON nodepool_predictions(analyzed_at DESC);
CREATE INDEX IF NOT EXISTS idx_np_user_email   ON nodepool_predictions(user_email);
CREATE INDEX IF NOT EXISTS idx_np_risk_level   ON nodepool_predictions(risk_level);
`

// NewNodePoolPredictionsStore cria o store com seu próprio banco SQLite.
// dbPath ex: "./build/nodepool_predictions.db"
func NewNodePoolPredictionsStore(dbPath string) (*NodePoolPredictionsStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório do banco nodepool_predictions: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco nodepool_predictions: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping banco nodepool_predictions: %w", err)
	}

	if _, err := db.Exec(nodePoolPredictionsSchema); err != nil {
		return nil, fmt.Errorf("criar schema nodepool_predictions: %w", err)
	}

	return &NodePoolPredictionsStore{db: db}, nil
}

// Save persiste uma análise de node pool.
// actionSummary e costAnalysis podem ser qualquer estrutura serializável.
// fullResult deve ser o NodePoolPredictionResult completo.
func (s *NodePoolPredictionsStore) Save(
	id, cluster, nodePoolName string,
	healthScore int,
	riskLevel string,
	actionSummary, costAnalysis, fullResult interface{},
	provider, model string,
	durationMs int64,
	userEmail string,
	analyzedAt time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	actionJSON, _ := json.Marshal(actionSummary)
	costJSON, _ := json.Marshal(costAnalysis)
	fullJSON, _ := json.Marshal(fullResult)

	_, err := s.db.Exec(`
INSERT INTO nodepool_predictions
    (id, cluster, nodepool_name, health_score, risk_level, action_summary, cost_analysis, full_result,
     provider, model, duration_ms, user_email, analyzed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, cluster, nodePoolName, healthScore, riskLevel,
		string(actionJSON), string(costJSON), string(fullJSON),
		provider, model, durationMs, userEmail,
		analyzedAt.Format(time.RFC3339),
	)

	return err
}

// GetByID retorna a análise pelo ID.
func (s *NodePoolPredictionsStore) GetByID(id string) (*NodePoolPredictionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const q = `
SELECT id, cluster, nodepool_name, health_score, risk_level,
       action_summary, cost_analysis, full_result,
       provider, model, duration_ms, user_email, analyzed_at, created_at
FROM nodepool_predictions
WHERE id = ?`

	row := s.db.QueryRow(q, id)

	r := &NodePoolPredictionRecord{}
	var analyzedAt, createdAt string
	err := row.Scan(
		&r.ID, &r.Cluster, &r.NodePoolName, &r.HealthScore, &r.RiskLevel,
		&r.ActionSummary, &r.CostAnalysis, &r.FullResult,
		&r.Provider, &r.Model, &r.DurationMs, &r.UserEmail,
		&analyzedAt, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("análise não encontrada: %s", id)
	}
	if err != nil {
		return nil, err
	}

	r.AnalyzedAt, _ = time.Parse(time.RFC3339, analyzedAt)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return r, nil
}

// List retorna análises com filtros opcionais, ordenadas por data (mais recente primeiro).
func (s *NodePoolPredictionsStore) List(f *NodePoolPredictionQueryFilters) ([]*NodePoolPredictionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := `
SELECT id, cluster, nodepool_name, health_score, risk_level,
       action_summary, cost_analysis, full_result,
       provider, model, duration_ms, user_email, analyzed_at, created_at
FROM nodepool_predictions
WHERE 1=1`

	var args []interface{}

	if f.Cluster != "" {
		q += " AND cluster = ?"
		args = append(args, f.Cluster)
	}
	if f.NodePoolName != "" {
		q += " AND nodepool_name = ?"
		args = append(args, f.NodePoolName)
	}
	if f.UserEmail != "" {
		q += " AND user_email = ?"
		args = append(args, f.UserEmail)
	}
	if f.StartDate != nil {
		q += " AND analyzed_at >= ?"
		args = append(args, f.StartDate.Format(time.RFC3339))
	}
	if f.EndDate != nil {
		q += " AND analyzed_at <= ?"
		args = append(args, f.EndDate.Format(time.RFC3339))
	}

	q += " ORDER BY analyzed_at DESC"

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q += " LIMIT ?"
	args = append(args, limit)

	if f.Offset > 0 {
		q += " OFFSET ?"
		args = append(args, f.Offset)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*NodePoolPredictionRecord
	for rows.Next() {
		r := &NodePoolPredictionRecord{}
		var analyzedAt, createdAt string
		if err := rows.Scan(
			&r.ID, &r.Cluster, &r.NodePoolName, &r.HealthScore, &r.RiskLevel,
			&r.ActionSummary, &r.CostAnalysis, &r.FullResult,
			&r.Provider, &r.Model, &r.DurationMs, &r.UserEmail,
			&analyzedAt, &createdAt,
		); err != nil {
			return nil, err
		}
		r.AnalyzedAt, _ = time.Parse(time.RFC3339, analyzedAt)
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		records = append(records, r)
	}

	if records == nil {
		records = []*NodePoolPredictionRecord{}
	}

	return records, rows.Err()
}

// Count retorna o total de análises com os filtros aplicados.
func (s *NodePoolPredictionsStore) Count(f *NodePoolPredictionQueryFilters) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := "SELECT COUNT(*) FROM nodepool_predictions WHERE 1=1"
	var args []interface{}

	if f.Cluster != "" {
		q += " AND cluster = ?"
		args = append(args, f.Cluster)
	}
	if f.NodePoolName != "" {
		q += " AND nodepool_name = ?"
		args = append(args, f.NodePoolName)
	}
	if f.UserEmail != "" {
		q += " AND user_email = ?"
		args = append(args, f.UserEmail)
	}

	var count int
	return count, s.db.QueryRow(q, args...).Scan(&count)
}

// Close fecha a conexão com o banco.
func (s *NodePoolPredictionsStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
