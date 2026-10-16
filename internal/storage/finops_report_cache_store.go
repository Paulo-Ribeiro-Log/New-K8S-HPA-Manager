package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FinOpsReportCacheStore persiste o ÚLTIMO relatório FinOps (o mesmo JSON já devolvido por
// GET /finops/report) por cluster — existe só pra reabrir a aba FinOps não vir vazia. Bug real
// corrigido, relatado pelo usuário: "sempre que chamamos a aba finops, ela vem vazia só com as
// seleções de cluster e os botões... ajuste para que venha com a exibição do último scan" — antes,
// o relatório vivia só no cache em memória do React Query do navegador (staleTime: Infinity, mas
// sem persistência nenhuma além disso), então qualquer reload de página ou expiração do cache do
// componente exigia clicar "Analisar" de novo pra ver qualquer coisa. Nunca recalcula nada — é um
// cache puro, sobrescrito a cada "Analisar" bem-sucedido; GET /finops/report continua 100% ao
// vivo (Dynatrace/Prometheus/K8s), isso só serve pra restaurar a ÚLTIMA tela vista sem exigir um
// novo clique manual. Mesmo princípio já usado pelo Node Pool Registry e pelo FinOps Rightsizing
// Store ("análise fica salva — reabrir a aba não escaneia de novo sozinho"), só que aqui como um
// blob JSON opaco em vez de tabelas normalizadas — o schema do FinOpsReport já muda com
// frequência conforme a aba evolui, e não há necessidade de consultar campos individuais aqui
// (o consumidor é sempre "me devolva o relatório inteiro de novo", nunca uma agregação).
type FinOpsReportCacheStore struct {
	db *sql.DB
}

const finOpsReportCacheSchema = `
CREATE TABLE IF NOT EXISTS finops_report_cache (
    cluster      TEXT PRIMARY KEY,
    report_json  TEXT NOT NULL,
    generated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS finops_showback_month (
    cluster   TEXT NOT NULL,
    month     TEXT NOT NULL,
    rows_json TEXT NOT NULL,
    report_at DATETIME NOT NULL,
    PRIMARY KEY (cluster, month)
);
`

// NewFinOpsReportCacheStore abre (ou cria) o banco SQLite do cache de último relatório FinOps.
func NewFinOpsReportCacheStore(dbPath string) (*FinOpsReportCacheStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório finops-report-cache: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco finops-report-cache: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping banco finops-report-cache: %w", err)
	}
	if _, err := db.Exec(finOpsReportCacheSchema); err != nil {
		return nil, fmt.Errorf("criar schema finops-report-cache: %w", err)
	}
	if err := purgeFinOpsDataIfStale(db, "finops_report_cache"); err != nil {
		return nil, fmt.Errorf("migrar semântica finops-report-cache: %w", err)
	}
	return &FinOpsReportCacheStore{db: db}, nil
}

// Save sobrescreve (upsert) o relatório cacheado de um cluster — sempre reflete o último
// "Analisar" bem-sucedido, nunca um histórico.
func (s *FinOpsReportCacheStore) Save(cluster string, reportJSON []byte, generatedAt time.Time) error {
	_, err := s.db.Exec(`
INSERT INTO finops_report_cache (cluster, report_json, generated_at) VALUES (?, ?, ?)
ON CONFLICT(cluster) DO UPDATE SET report_json = excluded.report_json, generated_at = excluded.generated_at
`, cluster, string(reportJSON), generatedAt)
	return err
}

// Get retorna o relatório cacheado de um cluster, se houver. found=false (nunca erro) quando o
// cluster ainda não foi analisado nenhuma vez.
func (s *FinOpsReportCacheStore) Get(cluster string) (reportJSON []byte, generatedAt time.Time, found bool, err error) {
	var raw string
	row := s.db.QueryRow(`SELECT report_json, generated_at FROM finops_report_cache WHERE cluster = ?`, cluster)
	if scanErr := row.Scan(&raw, &generatedAt); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return nil, time.Time{}, false, nil
		}
		return nil, time.Time{}, false, scanErr
	}
	return []byte(raw), generatedAt, true, nil
}

// CachedReport é uma linha do cache: o último relatório de um cluster.
type CachedReport struct {
	Cluster     string
	ReportJSON  []byte
	GeneratedAt time.Time
}

// ListAll devolve o último relatório de todos os clusters (visão multi-cluster do FinOps).
func (s *FinOpsReportCacheStore) ListAll() ([]CachedReport, error) {
	rows, err := s.db.Query(`SELECT cluster, report_json, generated_at FROM finops_report_cache ORDER BY cluster`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CachedReport
	for rows.Next() {
		var r CachedReport
		var raw string
		if err := rows.Scan(&r.Cluster, &raw, &r.GeneratedAt); err != nil {
			return nil, err
		}
		r.ReportJSON = []byte(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ShowbackMonth é o showback de um cluster num mês (YYYY-MM): a foto do último scan do mês.
type ShowbackMonth struct {
	Cluster  string
	Month    string
	RowsJSON []byte
	ReportAt time.Time
}

// SaveShowback grava (sobrescreve) o showback do cluster no mês.
func (s *FinOpsReportCacheStore) SaveShowback(cluster, month string, rowsJSON []byte, reportAt time.Time) error {
	_, err := s.db.Exec(`
INSERT INTO finops_showback_month (cluster, month, rows_json, report_at) VALUES (?, ?, ?, ?)
ON CONFLICT(cluster, month) DO UPDATE SET rows_json = excluded.rows_json, report_at = excluded.report_at
WHERE excluded.report_at >= finops_showback_month.report_at`, cluster, month, string(rowsJSON), reportAt)
	return err
}

// ListShowback devolve o showback de todos os clusters no mês.
func (s *FinOpsReportCacheStore) ListShowback(month string) ([]ShowbackMonth, error) {
	rows, err := s.db.Query(`SELECT cluster, month, rows_json, report_at FROM finops_showback_month WHERE month = ? ORDER BY cluster`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShowbackMonth
	for rows.Next() {
		var m ShowbackMonth
		var raw string
		if err := rows.Scan(&m.Cluster, &m.Month, &raw, &m.ReportAt); err != nil {
			return nil, err
		}
		m.RowsJSON = []byte(raw)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ShowbackMonths lista os meses com showback gravado, do mais recente para o mais antigo.
func (s *FinOpsReportCacheStore) ShowbackMonths() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT month FROM finops_showback_month ORDER BY month DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
