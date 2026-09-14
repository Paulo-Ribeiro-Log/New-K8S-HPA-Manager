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
