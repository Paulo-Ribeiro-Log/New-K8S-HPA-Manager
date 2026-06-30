package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SNATHistoryRecord representa um snapshot do orçamento SNAT de um cluster
type SNATHistoryRecord struct {
	Cluster                string    `json:"cluster"`
	TotalNodeCount         int       `json:"total_node_count"`
	UsagePercent           float64   `json:"usage_percent"`
	NodesUntilLimit        int       `json:"nodes_until_limit"`
	AllocatedOutboundPorts int       `json:"allocated_outbound_ports"`
	OutboundIPCount        int       `json:"outbound_ip_count"`
	RecordedAt             time.Time `json:"recorded_at"`
}

// SNATProjection resultado de cálculo de projeção de crescimento
type SNATProjection struct {
	Records        []SNATHistoryRecord `json:"records"`
	GrowthPerDay   float64             `json:"growth_per_day"`   // nós/dia (pode ser negativo)
	DaysUntilLimit int                 `json:"days_until_limit"` // -1 = indeterminado
	EstimatedDate  *time.Time          `json:"estimated_date"`   // nil = indeterminado
	Confidence     string              `json:"confidence"`       // "high" | "medium" | "low" | "none"
	DataPoints     int                 `json:"data_points"`
}

// SNATHistoryStore armazena snapshots históricos de orçamento SNAT por cluster
type SNATHistoryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const snatHistorySchema = `
CREATE TABLE IF NOT EXISTS snat_history (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster                  TEXT    NOT NULL,
    total_node_count         INTEGER NOT NULL,
    usage_percent            REAL    NOT NULL,
    nodes_until_limit        INTEGER NOT NULL,
    allocated_outbound_ports INTEGER NOT NULL,
    outbound_ip_count        INTEGER NOT NULL,
    recorded_at              DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_snat_cluster_time ON snat_history(cluster, recorded_at DESC);
`

func NewSNATHistoryStore(dbPath string) (*SNATHistoryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório snat_history: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir snat_history.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping snat_history.db: %w", err)
	}
	if _, err := db.Exec(snatHistorySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema snat_history: %w", err)
	}
	return &SNATHistoryStore{db: db}, nil
}

// Save insere um snapshot (no máximo 1 por hora por cluster para evitar duplicação)
func (s *SNATHistoryStore) Save(r SNATHistoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	cutoff := r.RecordedAt.Add(-1 * time.Hour)
	_ = s.db.QueryRow(
		`SELECT COUNT(*) FROM snat_history WHERE cluster=? AND recorded_at > ?`,
		r.Cluster, cutoff,
	).Scan(&count)
	if count > 0 {
		return nil
	}

	_, err := s.db.Exec(
		`INSERT INTO snat_history
		 (cluster, total_node_count, usage_percent, nodes_until_limit, allocated_outbound_ports, outbound_ip_count, recorded_at)
		 VALUES (?,?,?,?,?,?,?)`,
		r.Cluster, r.TotalNodeCount, r.UsagePercent, r.NodesUntilLimit,
		r.AllocatedOutboundPorts, r.OutboundIPCount, r.RecordedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserir snat_history: %w", err)
	}

	// Manter apenas 90 dias de histórico
	_, _ = s.db.Exec(
		`DELETE FROM snat_history WHERE cluster=? AND recorded_at < ?`,
		r.Cluster, r.RecordedAt.Add(-90*24*time.Hour).UTC(),
	)
	return nil
}

// GetRecent retorna os registros dos últimos N dias de um cluster em ordem cronológica
func (s *SNATHistoryStore) GetRecent(cluster string, days int) ([]SNATHistoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	rows, err := s.db.Query(
		`SELECT cluster, total_node_count, usage_percent, nodes_until_limit,
		        allocated_outbound_ports, outbound_ip_count, recorded_at
		 FROM snat_history
		 WHERE cluster=? AND recorded_at > ?
		 ORDER BY recorded_at ASC`,
		cluster, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SNATHistoryRecord
	for rows.Next() {
		var r SNATHistoryRecord
		var recAt string
		if err := rows.Scan(
			&r.Cluster, &r.TotalNodeCount, &r.UsagePercent, &r.NodesUntilLimit,
			&r.AllocatedOutboundPorts, &r.OutboundIPCount, &recAt,
		); err != nil {
			continue
		}
		// SQLite pode retornar datetime em dois formatos
		if t, err := time.Parse("2006-01-02T15:04:05Z", recAt); err == nil {
			r.RecordedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", recAt); err == nil {
			r.RecordedAt = t
		} else {
			r.RecordedAt, _ = time.Parse(time.RFC3339, recAt)
		}
		records = append(records, r)
	}
	return records, nil
}

// GetLatest retorna o registro mais recente de um cluster, ou nil se não houver histórico
func (s *SNATHistoryStore) GetLatest(cluster string) (*SNATHistoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var r SNATHistoryRecord
	var recAt string
	err := s.db.QueryRow(`
		SELECT cluster, total_node_count, usage_percent, nodes_until_limit,
		       allocated_outbound_ports, outbound_ip_count, recorded_at
		FROM snat_history WHERE cluster=? ORDER BY recorded_at DESC LIMIT 1`,
		cluster,
	).Scan(&r.Cluster, &r.TotalNodeCount, &r.UsagePercent, &r.NodesUntilLimit,
		&r.AllocatedOutboundPorts, &r.OutboundIPCount, &recAt)
	if err != nil {
		return nil, err // inclui sql.ErrNoRows
	}
	if t, e := time.Parse("2006-01-02T15:04:05Z", recAt); e == nil {
		r.RecordedAt = t
	} else if t, e := time.Parse("2006-01-02 15:04:05", recAt); e == nil {
		r.RecordedAt = t
	} else {
		r.RecordedAt, _ = time.Parse(time.RFC3339, recAt)
	}
	return &r, nil
}

// ComputeSNATProjection calcula taxa de crescimento linear e estima quando o limite será atingido
func ComputeSNATProjection(records []SNATHistoryRecord, nodesUntilLimit int) SNATProjection {
	proj := SNATProjection{
		Records:        records,
		DataPoints:     len(records),
		DaysUntilLimit: -1,
		Confidence:     "none",
	}
	if len(records) < 2 {
		return proj
	}

	// Regressão linear: x = dias desde o primeiro ponto, y = total_node_count
	first := records[0].RecordedAt
	n := float64(len(records))
	var sumX, sumY, sumXY, sumX2 float64
	for _, r := range records {
		x := r.RecordedAt.Sub(first).Hours() / 24.0
		y := float64(r.TotalNodeCount)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return proj
	}
	slope := (n*sumXY - sumX*sumY) / denom // nós por dia

	proj.GrowthPerDay = slope

	spanDays := records[len(records)-1].RecordedAt.Sub(first).Hours() / 24.0

	switch {
	case len(records) >= 14 && spanDays >= 7:
		proj.Confidence = "high"
	case len(records) >= 5 && spanDays >= 2:
		proj.Confidence = "medium"
	default:
		proj.Confidence = "low"
	}

	if slope <= 0 {
		// sem crescimento — sem estimativa de data
		return proj
	}

	daysLeft := float64(nodesUntilLimit) / slope
	proj.DaysUntilLimit = int(daysLeft)
	estimated := time.Now().Add(time.Duration(daysLeft * float64(24*time.Hour)))
	proj.EstimatedDate = &estimated

	return proj
}
