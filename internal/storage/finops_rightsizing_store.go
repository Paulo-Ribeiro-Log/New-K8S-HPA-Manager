package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// WorkloadRecommendation é o snapshot persistido de request/limit atual + recomendado de um
// workload — gerado por um scan explícito (POST /finops/rightsizing/scan), não recalculado a
// cada leitura (ver internal/web/handlers/finops_rightsizing.go).
type WorkloadRecommendation struct {
	Cluster          string  `json:"cluster"`
	Namespace        string  `json:"namespace"`
	Workload         string  `json:"workload"`
	NodePool         string  `json:"node_pool,omitempty"`
	Pods             int     `json:"pods"`
	CPURequestMillis float64 `json:"cpu_request_millis"`
	MemRequestMi     float64 `json:"mem_request_mi"`
	CPULimitMillis   float64 `json:"cpu_limit_millis,omitempty"`
	MemLimitMi       float64 `json:"mem_limit_mi,omitempty"`

	// Uso real observado (P95, mesma fonte que embasou a recomendação) — persistido pra a UI
	// poder mostrar "atual" no gauge, distinto do "recomendado" (P95×margem). Sem isso não há
	// como saber o quanto o workload realmente usa, só request/limit configurado x recomendado.
	CPUP95Millis float64 `json:"cpu_p95_millis,omitempty"`
	MemP95Mi     float64 `json:"mem_p95_mi,omitempty"`
	// Pico histórico (top) — max_over_time (Prometheus) ou "max" (Dynatrace). Distinto de P95:
	// mostra o pior spike já observado, não só o percentil.
	CPUMaxMillis float64 `json:"cpu_max_millis,omitempty"`
	MemMaxMi     float64 `json:"mem_max_mi,omitempty"`
	// Uso "current" (live, via metrics-server) — snapshot do instante do scan, distinto de
	// CPUP95Millis/MemP95Mi (agregação histórica de uma janela de dias). Vazio quando o
	// metrics-server não está disponível no cluster (ex: EKS sem instalar).
	CPUCurrentMillis float64 `json:"cpu_current_millis,omitempty"`
	MemCurrentMi     float64 `json:"mem_current_mi,omitempty"`
	// NodeName é o node onde a maioria dos pods deste workload roda — correlaciona com a tabela
	// node_usage (ver NodeUsage/ReplaceNodeUsage) pra saber "current"/"top" do node em si.
	NodeName string `json:"node_name,omitempty"`

	CPURecommendedMillis      float64 `json:"cpu_recommended_millis,omitempty"`
	MemRecommendedMi          float64 `json:"mem_recommended_mi,omitempty"`
	CPULimitRecommendedMillis float64 `json:"cpu_limit_recommended_millis,omitempty"`
	MemLimitRecommendedMi     float64 `json:"mem_limit_recommended_mi,omitempty"`

	Verdict       string    `json:"verdict"`
	WasteBRL      float64   `json:"waste_brl,omitempty"`
	MetricsSource string    `json:"metrics_source,omitempty"`
	WindowDays    int       `json:"window_days"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// NodeUsage é o snapshot persistido de uso "current" (live, metrics-server) e "top" (pico
// histórico, Prometheus) de um node — mesmo shape de finops.NodeUsage, mas o pacote storage não
// pode importar finops (ciclo, ver NodePoolTierSuggestion acima), então os campos são duplicados
// aqui e o chamador (finops_rightsizing.go) converte na hora de persistir/ler.
type NodeUsage struct {
	Cluster          string    `json:"cluster"`
	NodeName         string    `json:"node_name"`
	NodePool         string    `json:"node_pool,omitempty"`
	CPUCapMillis     float64   `json:"cpu_cap_millis,omitempty"`
	MemCapMi         float64   `json:"mem_cap_mi,omitempty"`
	CPUCurrentPct    float64   `json:"cpu_current_pct,omitempty"`
	MemCurrentPct    float64   `json:"mem_current_pct,omitempty"`
	CPUTopPct        float64   `json:"cpu_top_pct,omitempty"`
	MemTopPct        float64   `json:"mem_top_pct,omitempty"`
	MetricsAvailable bool      `json:"metrics_available"`
	MetricsError     string    `json:"metrics_error,omitempty"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// NodePoolTierSuggestion é o snapshot persistido de sugestão de troca de tier de VM de um pool.
// AlternativesJSON guarda a lista serializada (mesmo shape de finops.VMAlternative) — o pacote
// storage não pode importar finops (finops já importa storage, ver calculator.go:BuildReport),
// então quem monta/lê esse JSON é sempre o chamador (internal/web/handlers/finops_rightsizing.go).
type NodePoolTierSuggestion struct {
	Cluster          string    `json:"cluster"`
	NodePool         string    `json:"node_pool"`
	CurrentSKU       string    `json:"current_sku"`
	CPUUtilPct       float64   `json:"cpu_util_pct"`
	MemUtilPct       float64   `json:"mem_util_pct"`
	WorkloadCount    int       `json:"workload_count"` // nº de workloads que embasaram o cálculo de util%
	AlternativesJSON string    `json:"-"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// FinOpsRightsizingStore persiste as análises de rightsizing (request/limit por workload + tier
// de VM por node pool) — existe pra não re-escanear (Prometheus/Dynatrace/cloud pricing API) toda
// vez que alguém só quer OLHAR uma análise de um cluster já analisado antes. Mesmo padrão de
// nodepool_registry_store.go (SQLite WAL, upsert por chave natural).
type FinOpsRightsizingStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const finopsRightsizingSchema = `
CREATE TABLE IF NOT EXISTS workload_recommendations (
    cluster                        TEXT NOT NULL,
    namespace                      TEXT NOT NULL,
    workload                       TEXT NOT NULL,
    node_pool                      TEXT,
    pods                           INTEGER NOT NULL DEFAULT 0,
    cpu_request_millis             REAL NOT NULL DEFAULT 0,
    mem_request_mi                 REAL NOT NULL DEFAULT 0,
    cpu_limit_millis               REAL NOT NULL DEFAULT 0,
    mem_limit_mi                   REAL NOT NULL DEFAULT 0,
    cpu_p95_millis                 REAL NOT NULL DEFAULT 0,
    mem_p95_mi                     REAL NOT NULL DEFAULT 0,
    cpu_recommended_millis         REAL NOT NULL DEFAULT 0,
    mem_recommended_mi             REAL NOT NULL DEFAULT 0,
    cpu_limit_recommended_millis   REAL NOT NULL DEFAULT 0,
    mem_limit_recommended_mi       REAL NOT NULL DEFAULT 0,
    verdict                        TEXT,
    waste_brl                      REAL NOT NULL DEFAULT 0,
    metrics_source                 TEXT,
    window_days                    INTEGER NOT NULL DEFAULT 0,
    generated_at                   DATETIME NOT NULL,
    PRIMARY KEY (cluster, namespace, workload)
);
CREATE INDEX IF NOT EXISTS idx_wl_reco_cluster ON workload_recommendations(cluster);

CREATE TABLE IF NOT EXISTS nodepool_tier_suggestions (
    cluster           TEXT NOT NULL,
    node_pool         TEXT NOT NULL,
    current_sku       TEXT,
    cpu_util_pct      REAL NOT NULL DEFAULT 0,
    mem_util_pct      REAL NOT NULL DEFAULT 0,
    workload_count    INTEGER NOT NULL DEFAULT 0,
    alternatives_json TEXT,
    generated_at      DATETIME NOT NULL,
    PRIMARY KEY (cluster, node_pool)
);
CREATE INDEX IF NOT EXISTS idx_np_tier_cluster ON nodepool_tier_suggestions(cluster);

CREATE TABLE IF NOT EXISTS node_usage (
    cluster            TEXT NOT NULL,
    node_name          TEXT NOT NULL,
    node_pool          TEXT,
    cpu_cap_millis     REAL NOT NULL DEFAULT 0,
    mem_cap_mi         REAL NOT NULL DEFAULT 0,
    cpu_current_pct    REAL NOT NULL DEFAULT 0,
    mem_current_pct    REAL NOT NULL DEFAULT 0,
    cpu_top_pct        REAL NOT NULL DEFAULT 0,
    mem_top_pct        REAL NOT NULL DEFAULT 0,
    metrics_available  INTEGER NOT NULL DEFAULT 0,
    metrics_error      TEXT,
    generated_at       DATETIME NOT NULL,
    PRIMARY KEY (cluster, node_name)
);
CREATE INDEX IF NOT EXISTS idx_node_usage_cluster ON node_usage(cluster);
`

// finopsRightsizingMigrations adiciona colunas novas a bancos já existentes (criados antes desta
// mudança) — CREATE TABLE IF NOT EXISTS acima nunca altera uma tabela já existente. Cada ALTER
// TABLE é tentado isoladamente e o erro "duplicate column" é ignorado (idempotente entre
// reinicializações), mesmo padrão já usado no Node Pool Registry (disk_size_gb/disk_type).
var finopsRightsizingMigrations = []string{
	`ALTER TABLE workload_recommendations ADD COLUMN node_name TEXT`,
	`ALTER TABLE workload_recommendations ADD COLUMN cpu_max_millis REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE workload_recommendations ADD COLUMN mem_max_mi REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE workload_recommendations ADD COLUMN cpu_current_millis REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE workload_recommendations ADD COLUMN mem_current_mi REAL NOT NULL DEFAULT 0`,
}

// NewFinOpsRightsizingStore abre (ou cria) o banco SQLite de análises de rightsizing.
func NewFinOpsRightsizingStore(dbPath string) (*FinOpsRightsizingStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório finops-rightsizing: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco finops-rightsizing: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping banco finops-rightsizing: %w", err)
	}
	if _, err := db.Exec(finopsRightsizingSchema); err != nil {
		return nil, fmt.Errorf("criar schema finops-rightsizing: %w", err)
	}
	for _, migration := range finopsRightsizingMigrations {
		if _, err := db.Exec(migration); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrar schema finops-rightsizing: %w", err)
		}
	}
	return &FinOpsRightsizingStore{db: db}, nil
}

// ReplaceWorkloadRecommendations substitui TODAS as recomendações de workload de um cluster
// pelo conjunto novo (delete+insert numa transação) — um scan é sempre um snapshot completo do
// cluster naquele instante, não um merge incremental (um workload que sumiu do cluster desde o
// último scan não deve continuar aparecendo como recomendação "presa" indefinidamente).
func (s *FinOpsRightsizingStore) ReplaceWorkloadRecommendations(cluster string, recs []WorkloadRecommendation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM workload_recommendations WHERE cluster = ?`, cluster); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
INSERT INTO workload_recommendations (
    cluster, namespace, workload, node_pool, node_name, pods,
    cpu_request_millis, mem_request_mi, cpu_limit_millis, mem_limit_mi,
    cpu_p95_millis, mem_p95_mi, cpu_max_millis, mem_max_mi,
    cpu_current_millis, mem_current_mi,
    cpu_recommended_millis, mem_recommended_mi, cpu_limit_recommended_millis, mem_limit_recommended_mi,
    verdict, waste_brl, metrics_source, window_days, generated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range recs {
		if _, err := stmt.Exec(
			cluster, r.Namespace, r.Workload, r.NodePool, r.NodeName, r.Pods,
			r.CPURequestMillis, r.MemRequestMi, r.CPULimitMillis, r.MemLimitMi,
			r.CPUP95Millis, r.MemP95Mi, r.CPUMaxMillis, r.MemMaxMi,
			r.CPUCurrentMillis, r.MemCurrentMi,
			r.CPURecommendedMillis, r.MemRecommendedMi, r.CPULimitRecommendedMillis, r.MemLimitRecommendedMi,
			r.Verdict, r.WasteBRL, r.MetricsSource, r.WindowDays, r.GeneratedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetWorkloadRecommendations retorna as recomendações de workload persistidas para um cluster.
func (s *FinOpsRightsizingStore) GetWorkloadRecommendations(cluster string) ([]WorkloadRecommendation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
SELECT cluster, namespace, workload, node_pool, node_name, pods,
       cpu_request_millis, mem_request_mi, cpu_limit_millis, mem_limit_mi,
       cpu_p95_millis, mem_p95_mi, cpu_max_millis, mem_max_mi,
       cpu_current_millis, mem_current_mi,
       cpu_recommended_millis, mem_recommended_mi, cpu_limit_recommended_millis, mem_limit_recommended_mi,
       verdict, waste_brl, metrics_source, window_days, generated_at
FROM workload_recommendations WHERE cluster = ? ORDER BY waste_brl DESC`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recs := make([]WorkloadRecommendation, 0)
	for rows.Next() {
		var r WorkloadRecommendation
		var nodePool, nodeName, verdict, metricsSource sql.NullString
		if err := rows.Scan(
			&r.Cluster, &r.Namespace, &r.Workload, &nodePool, &nodeName, &r.Pods,
			&r.CPURequestMillis, &r.MemRequestMi, &r.CPULimitMillis, &r.MemLimitMi,
			&r.CPUP95Millis, &r.MemP95Mi, &r.CPUMaxMillis, &r.MemMaxMi,
			&r.CPUCurrentMillis, &r.MemCurrentMi,
			&r.CPURecommendedMillis, &r.MemRecommendedMi, &r.CPULimitRecommendedMillis, &r.MemLimitRecommendedMi,
			&verdict, &r.WasteBRL, &metricsSource, &r.WindowDays, &r.GeneratedAt,
		); err != nil {
			return nil, err
		}
		r.NodePool = nodePool.String
		r.NodeName = nodeName.String
		r.Verdict = verdict.String
		r.MetricsSource = metricsSource.String
		recs = append(recs, r)
	}
	return recs, rows.Err()
}

// ReplaceNodePoolTierSuggestions substitui todas as sugestões de tier de um cluster (mesmo
// racional de snapshot completo de ReplaceWorkloadRecommendations).
func (s *FinOpsRightsizingStore) ReplaceNodePoolTierSuggestions(cluster string, suggestions []NodePoolTierSuggestion) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM nodepool_tier_suggestions WHERE cluster = ?`, cluster); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
INSERT INTO nodepool_tier_suggestions (
    cluster, node_pool, current_sku, cpu_util_pct, mem_util_pct, workload_count, alternatives_json, generated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sug := range suggestions {
		if _, err := stmt.Exec(
			cluster, sug.NodePool, sug.CurrentSKU, sug.CPUUtilPct, sug.MemUtilPct,
			sug.WorkloadCount, sug.AlternativesJSON, sug.GeneratedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetNodePoolTierSuggestions retorna as sugestões de tier persistidas para um cluster.
func (s *FinOpsRightsizingStore) GetNodePoolTierSuggestions(cluster string) ([]NodePoolTierSuggestion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
SELECT cluster, node_pool, current_sku, cpu_util_pct, mem_util_pct, workload_count, alternatives_json, generated_at
FROM nodepool_tier_suggestions WHERE cluster = ? ORDER BY node_pool`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]NodePoolTierSuggestion, 0)
	for rows.Next() {
		var sug NodePoolTierSuggestion
		var currentSKU, altJSON sql.NullString
		if err := rows.Scan(
			&sug.Cluster, &sug.NodePool, &currentSKU, &sug.CPUUtilPct, &sug.MemUtilPct,
			&sug.WorkloadCount, &altJSON, &sug.GeneratedAt,
		); err != nil {
			return nil, err
		}
		sug.CurrentSKU = currentSKU.String
		sug.AlternativesJSON = altJSON.String
		result = append(result, sug)
	}
	return result, rows.Err()
}

// ReplaceNodeUsage substitui todo o uso current/top de node de um cluster (mesmo racional de
// snapshot completo das duas funções Replace* acima — um node que sumiu do cluster não deve
// continuar "preso" indefinidamente na análise).
func (s *FinOpsRightsizingStore) ReplaceNodeUsage(cluster string, usage []NodeUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM node_usage WHERE cluster = ?`, cluster); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
INSERT INTO node_usage (
    cluster, node_name, node_pool, cpu_cap_millis, mem_cap_mi,
    cpu_current_pct, mem_current_pct, cpu_top_pct, mem_top_pct,
    metrics_available, metrics_error, generated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, u := range usage {
		if _, err := stmt.Exec(
			cluster, u.NodeName, u.NodePool, u.CPUCapMillis, u.MemCapMi,
			u.CPUCurrentPct, u.MemCurrentPct, u.CPUTopPct, u.MemTopPct,
			u.MetricsAvailable, u.MetricsError, u.GeneratedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetNodeUsage retorna o uso current/top de node persistido para um cluster.
func (s *FinOpsRightsizingStore) GetNodeUsage(cluster string) ([]NodeUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
SELECT cluster, node_name, node_pool, cpu_cap_millis, mem_cap_mi,
       cpu_current_pct, mem_current_pct, cpu_top_pct, mem_top_pct,
       metrics_available, metrics_error, generated_at
FROM node_usage WHERE cluster = ? ORDER BY node_name`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]NodeUsage, 0)
	for rows.Next() {
		var u NodeUsage
		var nodePool, metricsError sql.NullString
		if err := rows.Scan(
			&u.Cluster, &u.NodeName, &nodePool, &u.CPUCapMillis, &u.MemCapMi,
			&u.CPUCurrentPct, &u.MemCurrentPct, &u.CPUTopPct, &u.MemTopPct,
			&u.MetricsAvailable, &metricsError, &u.GeneratedAt,
		); err != nil {
			return nil, err
		}
		u.NodePool = nodePool.String
		u.MetricsError = metricsError.String
		result = append(result, u)
	}
	return result, rows.Err()
}

// LastScannedAt retorna o timestamp da análise mais recente de um cluster (max entre as 2
// tabelas) e se já houve algum scan. Usado pra exibir "Última análise: Xh atrás" / "Nunca
// analisado" sem precisar carregar todas as recomendações só pra saber a data.
func (s *FinOpsRightsizingStore) LastScannedAt(cluster string) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Duas queries diretas (não MAX() nem subquery/UNION) de propósito: confirmado isolando o
	// bug antes desta correção — o driver mattn/go-sqlite3 perde o hint de tipo DATETIME quando
	// a coluna passa por uma função agregada (MAX()), devolvendo TEXT cru e fazendo sql.NullTime
	// falhar ao escanear. Um SELECT direto da coluna (com ORDER BY + LIMIT 1 pra achar o maior)
	// preserva o hint de tipo normalmente — mesmo padrão de scan já usado no resto do pacote.
	wl := s.maxGeneratedAt(`SELECT generated_at FROM workload_recommendations WHERE cluster = ? ORDER BY generated_at DESC LIMIT 1`, cluster)
	np := s.maxGeneratedAt(`SELECT generated_at FROM nodepool_tier_suggestions WHERE cluster = ? ORDER BY generated_at DESC LIMIT 1`, cluster)

	switch {
	case wl.Valid && np.Valid:
		if wl.Time.After(np.Time) {
			return wl.Time, true
		}
		return np.Time, true
	case wl.Valid:
		return wl.Time, true
	case np.Valid:
		return np.Time, true
	default:
		return time.Time{}, false
	}
}

func (s *FinOpsRightsizingStore) maxGeneratedAt(query, cluster string) sql.NullTime {
	var t sql.NullTime
	if err := s.db.QueryRow(query, cluster).Scan(&t); err != nil {
		return sql.NullTime{}
	}
	return t
}
