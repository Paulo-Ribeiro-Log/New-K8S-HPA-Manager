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

// LatencyTestRecord é um resultado de teste de latência sob demanda persistido — fonte
// estruturada e consultável usada pelo grafo de topologia (Fase 6.4), complementar ao
// HistoryTracker genérico (que continua sendo o audit trail em JSON não-estruturado, ver
// LatencyTestHandler.logHistory).
type LatencyTestRecord struct {
	Cluster       string    `json:"cluster"`
	Namespace     string    `json:"namespace"`
	Target        string    `json:"target"`
	Protocol      string    `json:"protocol"`
	P95Ms         float64   `json:"p95_ms"`
	P99Ms         float64   `json:"p99_ms"`
	ErrorCount    int       `json:"error_count"`
	TotalRequests int       `json:"total_requests"`
	TestedAt      time.Time `json:"tested_at"`
	TestedBy      string    `json:"tested_by"`
}

// LatencyTestHistoryStore persiste resultados de testes de latência sob demanda.
type LatencyTestHistoryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const latencyTestHistorySchema = `
CREATE TABLE IF NOT EXISTS latency_test_history (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster        TEXT    NOT NULL,
    namespace      TEXT    NOT NULL,
    target         TEXT    NOT NULL,
    protocol       TEXT    NOT NULL,
    p95_ms         REAL    NOT NULL,
    p99_ms         REAL    NOT NULL,
    error_count    INTEGER NOT NULL,
    total_requests INTEGER NOT NULL,
    tested_at      DATETIME NOT NULL,
    tested_by      TEXT
);

CREATE INDEX IF NOT EXISTS idx_latency_test_cluster_target ON latency_test_history(cluster, target, tested_at DESC);
`

// NewLatencyTestHistoryStore abre (ou cria) o banco em dbPath — mesmo padrão de
// NewSNATHistoryStore (WAL mode, timeout de lock curto, poucas conexões — é uma tabela de baixo
// volume, um registro por teste manual, não um coletor de alta frequência).
func NewLatencyTestHistoryStore(dbPath string) (*LatencyTestHistoryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório latency_test_history: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir latency_test_history.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping latency_test_history.db: %w", err)
	}
	if _, err := db.Exec(latencyTestHistorySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema latency_test_history: %w", err)
	}
	return &LatencyTestHistoryStore{db: db}, nil
}

// Save insere um registro e limpa entradas com mais de 30 dias (retenção mais curta que o SNAT,
// que usa 90 — este dado é diagnóstico/efêmero, não histórico de capacidade). Ao contrário do
// SNAT, NUNCA deduplica por janela de tempo: cada teste aqui é uma ação explícita do usuário
// (botão "Executar Teste"), não um snapshot periódico automático — todo teste deve virar um
// registro.
func (s *LatencyTestHistoryStore) Save(r LatencyTestRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO latency_test_history
		 (cluster, namespace, target, protocol, p95_ms, p99_ms, error_count, total_requests, tested_at, tested_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.Cluster, r.Namespace, r.Target, r.Protocol, r.P95Ms, r.P99Ms,
		r.ErrorCount, r.TotalRequests, r.TestedAt.UTC(), r.TestedBy,
	)
	if err != nil {
		return fmt.Errorf("inserir latency_test_history: %w", err)
	}

	_, _ = s.db.Exec(
		`DELETE FROM latency_test_history WHERE tested_at < ?`,
		time.Now().Add(-30*24*time.Hour).UTC(),
	)
	return nil
}

// GetRecent retorna os `limit` registros mais recentes de TODOS os clusters/alvos — usado pelo
// endpoint de topologia (Fase 6.4) pra montar nós/arestas do grafo. Sem filtro de cluster porque
// o grafo é justamente a visão agregada de toda a frota testada até agora.
func (s *LatencyTestHistoryStore) GetRecent(limit int) ([]LatencyTestRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 200
	}

	rows, err := s.db.Query(
		`SELECT cluster, namespace, target, protocol, p95_ms, p99_ms, error_count, total_requests, tested_at, tested_by
		 FROM latency_test_history
		 ORDER BY tested_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LatencyTestRecord
	for rows.Next() {
		var r LatencyTestRecord
		var testedAt string
		var testedBy sql.NullString
		if err := rows.Scan(
			&r.Cluster, &r.Namespace, &r.Target, &r.Protocol, &r.P95Ms, &r.P99Ms,
			&r.ErrorCount, &r.TotalRequests, &testedAt, &testedBy,
		); err != nil {
			continue
		}
		r.TestedBy = testedBy.String
		// SQLite pode retornar datetime em formatos diferentes (mesmo tratamento de
		// snat_history_store.go)
		if t, err := time.Parse("2006-01-02T15:04:05Z", testedAt); err == nil {
			r.TestedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", testedAt); err == nil {
			r.TestedAt = t
		} else {
			r.TestedAt, _ = time.Parse(time.RFC3339, testedAt)
		}
		records = append(records, r)
	}
	return records, nil
}
