package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FinOpsAction é uma recomendação do FinOps que alguém mandou aplicar (Staging, patch de values ou
// kubectl), com a foto do workload naquele momento (Baseline*) e o alvo (Target*; 0 = não muda).
// O acompanhamento compara a foto com o relatório seguinte (finops.EvaluateAction): se foi
// aplicada e quanto economizou de fato. FINOPS-REVISAO-PLAN.md, Fase 3.
type FinOpsAction struct {
	ID        int64     `json:"id"`
	Cluster   string    `json:"cluster"`
	Namespace string    `json:"namespace"`
	Workload  string    `json:"workload"`
	Channel   string    `json:"channel"` // staging | values | kubectl
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	BaselineReportAt  time.Time `json:"baseline_report_at"`
	BaselinePods      int       `json:"baseline_pods"`
	BaselineHPAMin    int       `json:"baseline_hpa_min"`
	BaselineHPAMax    int       `json:"baseline_hpa_max"`
	BaselineCPUMillis float64   `json:"baseline_cpu_millis"`
	BaselineMemMi     float64   `json:"baseline_mem_mi"`
	BaselineCostBRL   float64   `json:"baseline_cost_brl"`
	BaselineWasteBRL  float64   `json:"baseline_waste_brl"`
	ExpectedSavingBRL float64   `json:"expected_saving_brl"`
	TargetHPAMin      int       `json:"target_hpa_min,omitempty"`
	TargetHPAMax      int       `json:"target_hpa_max,omitempty"`
	TargetCPUMillis   float64   `json:"target_cpu_millis,omitempty"`
	TargetMemMi       float64   `json:"target_mem_mi,omitempty"`
}

// FinOpsActionsStore grava as ações em ~/.k8s-hpa-manager/finops_actions.db.
type FinOpsActionsStore struct {
	db *sql.DB
}

const finOpsActionsSchema = `
CREATE TABLE IF NOT EXISTS finops_actions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster             TEXT NOT NULL,
    namespace           TEXT NOT NULL,
    workload            TEXT NOT NULL,
    channel             TEXT NOT NULL,
    created_by          TEXT NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL,
    baseline_report_at  DATETIME NOT NULL,
    baseline_pods       INTEGER NOT NULL DEFAULT 0,
    baseline_hpa_min    INTEGER NOT NULL DEFAULT 0,
    baseline_hpa_max    INTEGER NOT NULL DEFAULT 0,
    baseline_cpu_millis REAL NOT NULL DEFAULT 0,
    baseline_mem_mi     REAL NOT NULL DEFAULT 0,
    baseline_cost_brl   REAL NOT NULL DEFAULT 0,
    baseline_waste_brl  REAL NOT NULL DEFAULT 0,
    expected_saving_brl REAL NOT NULL DEFAULT 0,
    target_hpa_min      INTEGER NOT NULL DEFAULT 0,
    target_hpa_max      INTEGER NOT NULL DEFAULT 0,
    target_cpu_millis   REAL NOT NULL DEFAULT 0,
    target_mem_mi       REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_fa_cluster ON finops_actions(cluster);
`

// NewFinOpsActionsStore abre (ou cria) o banco das ações.
func NewFinOpsActionsStore(dbPath string) (*FinOpsActionsStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório finops_actions: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco finops_actions: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping banco finops_actions: %w", err)
	}
	if _, err := db.Exec(finOpsActionsSchema); err != nil {
		return nil, fmt.Errorf("criar schema finops_actions: %w", err)
	}
	return &FinOpsActionsStore{db: db}, nil
}

// Insert grava a ação e devolve o id.
func (s *FinOpsActionsStore) Insert(a FinOpsAction) (int64, error) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	res, err := s.db.Exec(`
INSERT INTO finops_actions (cluster, namespace, workload, channel, created_by, created_at, baseline_report_at,
    baseline_pods, baseline_hpa_min, baseline_hpa_max, baseline_cpu_millis, baseline_mem_mi, baseline_cost_brl,
    baseline_waste_brl, expected_saving_brl, target_hpa_min, target_hpa_max, target_cpu_millis, target_mem_mi)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Cluster, a.Namespace, a.Workload, a.Channel, a.CreatedBy, a.CreatedAt, a.BaselineReportAt,
		a.BaselinePods, a.BaselineHPAMin, a.BaselineHPAMax, a.BaselineCPUMillis, a.BaselineMemMi, a.BaselineCostBRL,
		a.BaselineWasteBRL, a.ExpectedSavingBRL, a.TargetHPAMin, a.TargetHPAMax, a.TargetCPUMillis, a.TargetMemMi)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListByCluster devolve as ações do cluster, a mais recente primeiro.
func (s *FinOpsActionsStore) ListByCluster(cluster string) ([]FinOpsAction, error) {
	rows, err := s.db.Query(`
SELECT id, cluster, namespace, workload, channel, created_by, created_at, baseline_report_at,
    baseline_pods, baseline_hpa_min, baseline_hpa_max, baseline_cpu_millis, baseline_mem_mi, baseline_cost_brl,
    baseline_waste_brl, expected_saving_brl, target_hpa_min, target_hpa_max, target_cpu_millis, target_mem_mi
FROM finops_actions WHERE cluster = ? ORDER BY created_at DESC, id DESC`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FinOpsAction{}
	for rows.Next() {
		var a FinOpsAction
		if err := rows.Scan(&a.ID, &a.Cluster, &a.Namespace, &a.Workload, &a.Channel, &a.CreatedBy, &a.CreatedAt,
			&a.BaselineReportAt, &a.BaselinePods, &a.BaselineHPAMin, &a.BaselineHPAMax, &a.BaselineCPUMillis,
			&a.BaselineMemMi, &a.BaselineCostBRL, &a.BaselineWasteBRL, &a.ExpectedSavingBRL, &a.TargetHPAMin,
			&a.TargetHPAMax, &a.TargetCPUMillis, &a.TargetMemMi); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Delete remove a ação do cluster. found=false quando não existia.
func (s *FinOpsActionsStore) Delete(cluster string, id int64) (found bool, err error) {
	res, err := s.db.Exec(`DELETE FROM finops_actions WHERE cluster = ? AND id = ?`, cluster, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FindRecent devolve o id de uma ação igual (mesmo workload, canal e alvo) criada depois de since;
// 0 quando não há. Evita duplicar o acompanhamento quando o mesmo kubectl é copiado de novo.
func (s *FinOpsActionsStore) FindRecent(a FinOpsAction, since time.Time) (int64, error) {
	var id int64
	err := s.db.QueryRow(`
SELECT id FROM finops_actions WHERE cluster = ? AND namespace = ? AND workload = ? AND channel = ?
  AND target_hpa_min = ? AND target_hpa_max = ? AND target_cpu_millis = ? AND target_mem_mi = ? AND created_at >= ?
ORDER BY id DESC LIMIT 1`, a.Cluster, a.Namespace, a.Workload, a.Channel, a.TargetHPAMin, a.TargetHPAMax,
		a.TargetCPUMillis, a.TargetMemMi, since).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}
