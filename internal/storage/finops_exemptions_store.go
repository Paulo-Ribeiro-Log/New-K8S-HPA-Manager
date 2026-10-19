package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FinOpsExemption é a isenção de um workload na análise do FinOps (FINOPS-EXEMPTIONS-PLAN.md).
// Caso típico: HPA 1/1 em HLG, que é decisão de projeto e não desperdício. Só o limite de
// réplicas é gravado; se a isenção está valendo é calculado a cada leitura do relatório
// (finops.ApplyExemptions) — passou do limite, a isenção deixa de valer sozinha.
type FinOpsExemption struct {
	ID                   int64     `json:"id"`
	Cluster              string    `json:"cluster"`
	Namespace            string    `json:"namespace"`
	Workload             string    `json:"workload"`
	MaxReplicasThreshold int       `json:"max_replicas_threshold"`
	Reason               string    `json:"reason"`
	CreatedBy            string    `json:"created_by,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// FinOpsExemptionsStore grava as isenções em ~/.k8s-hpa-manager/finops_exemptions.db.
type FinOpsExemptionsStore struct {
	db *sql.DB
}

const finOpsExemptionsSchema = `
CREATE TABLE IF NOT EXISTS finops_exemptions (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster                TEXT    NOT NULL,
    namespace              TEXT    NOT NULL,
    workload               TEXT    NOT NULL,
    max_replicas_threshold INTEGER NOT NULL DEFAULT 1,
    reason                 TEXT    NOT NULL DEFAULT '',
    created_by             TEXT    NOT NULL DEFAULT '',
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (cluster, namespace, workload)
);
CREATE INDEX IF NOT EXISTS idx_fe_cluster ON finops_exemptions(cluster);
`

// NewFinOpsExemptionsStore abre (ou cria) o banco das isenções.
func NewFinOpsExemptionsStore(dbPath string) (*FinOpsExemptionsStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório finops_exemptions: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco finops_exemptions: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping banco finops_exemptions: %w", err)
	}
	if _, err := db.Exec(finOpsExemptionsSchema); err != nil {
		return nil, fmt.Errorf("criar schema finops_exemptions: %w", err)
	}
	return &FinOpsExemptionsStore{db: db}, nil
}

// Upsert cria ou atualiza a isenção do workload (created_by/created_at ficam os da criação).
func (s *FinOpsExemptionsStore) Upsert(e FinOpsExemption) error {
	now := time.Now()
	_, err := s.db.Exec(`
INSERT INTO finops_exemptions (cluster, namespace, workload, max_replicas_threshold, reason, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(cluster, namespace, workload) DO UPDATE SET
    max_replicas_threshold = excluded.max_replicas_threshold,
    reason = excluded.reason,
    updated_at = excluded.updated_at
`, e.Cluster, e.Namespace, e.Workload, e.MaxReplicasThreshold, e.Reason, e.CreatedBy, now, now)
	return err
}

// ListByCluster devolve as isenções do cluster, por namespace e workload.
func (s *FinOpsExemptionsStore) ListByCluster(cluster string) ([]FinOpsExemption, error) {
	rows, err := s.db.Query(`
SELECT id, cluster, namespace, workload, max_replicas_threshold, reason, created_by, created_at, updated_at
FROM finops_exemptions WHERE cluster = ? ORDER BY namespace, workload`, cluster)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FinOpsExemption{}
	for rows.Next() {
		var e FinOpsExemption
		if err := rows.Scan(&e.ID, &e.Cluster, &e.Namespace, &e.Workload, &e.MaxReplicasThreshold, &e.Reason,
			&e.CreatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetAllAsMap devolve as isenções do cluster com chave "namespace/workload".
func (s *FinOpsExemptionsStore) GetAllAsMap(cluster string) (map[string]*FinOpsExemption, error) {
	list, err := s.ListByCluster(cluster)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*FinOpsExemption, len(list))
	for i := range list {
		out[list[i].Namespace+"/"+list[i].Workload] = &list[i]
	}
	return out, nil
}

// Delete remove a isenção. found=false quando não existia.
func (s *FinOpsExemptionsStore) Delete(cluster, namespace, workload string) (found bool, err error) {
	res, err := s.db.Exec(`DELETE FROM finops_exemptions WHERE cluster = ? AND namespace = ? AND workload = ?`,
		cluster, namespace, workload)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
