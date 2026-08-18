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

// SpinnakerRolloutRecord é o último resultado CONFIRMADO ao vivo (DetectRollback com Matched=true)
// pra um deployment específico — persistido pra sobreviver à janela de busca curta do próprio
// Gate do Spinnaker (achado real: `GET .../executions/search` devolve só as execuções dos
// últimos ~28 dias, independente do parâmetro "limit" pedido — ver internal/web/handlers/spinnaker.go).
// Sem essa persistência, um deployment que não é redeployado há mais de ~28 dias perde o dado de
// CHG/status pra sempre assim que a janela do Gate "rola" pra frente, mesmo esse dado tendo sido
// visto e confirmado numa consulta anterior.
type SpinnakerRolloutRecord struct {
	Cluster               string    `json:"cluster"`
	Namespace             string    `json:"namespace"`
	DeploymentName        string    `json:"deployment_name"`
	IsRollback            bool      `json:"is_rollback"`
	RollbackType          string    `json:"rollback_type,omitempty"`
	LastCHGApplied        string    `json:"last_chg_applied,omitempty"`
	PipelineExecutedAt    int64     `json:"pipeline_executed_at,omitempty"`
	ExecutionStatus       string    `json:"execution_status,omitempty"`
	RollbackStartedAt     int64     `json:"rollback_started_at,omitempty"`
	RollbackEndedAt       int64     `json:"rollback_ended_at,omitempty"`
	FailedCHG             string    `json:"failed_chg,omitempty"`
	RollbackPipelineName  string    `json:"rollback_pipeline_name,omitempty"`
	SpinnakerExecutionID  string    `json:"spinnaker_execution_id,omitempty"`
	SpinnakerExecutionURL string    `json:"spinnaker_execution_url,omitempty"`
	UpdatedAt             time.Time `json:"updated_at"` // última vez que isso foi confirmado AO VIVO no Gate
}

// SpinnakerHistoryStore persiste o último rollout status confirmado por deployment, atualizado
// a cada scan (RolloutStatusBatch) que encontra um match real — nunca perde dado só porque a
// janela de busca do Gate rolou pra frente.
type SpinnakerHistoryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

const spinnakerHistorySchema = `
CREATE TABLE IF NOT EXISTS spinnaker_rollout_status (
    cluster                  TEXT     NOT NULL,
    namespace                TEXT     NOT NULL,
    deployment_name          TEXT     NOT NULL,
    is_rollback              INTEGER  NOT NULL,
    rollback_type            TEXT     NOT NULL DEFAULT '',
    last_chg_applied         TEXT     NOT NULL DEFAULT '',
    pipeline_executed_at     INTEGER  NOT NULL DEFAULT 0,
    execution_status         TEXT     NOT NULL DEFAULT '',
    rollback_started_at      INTEGER  NOT NULL DEFAULT 0,
    rollback_ended_at        INTEGER  NOT NULL DEFAULT 0,
    failed_chg                TEXT    NOT NULL DEFAULT '',
    rollback_pipeline_name   TEXT     NOT NULL DEFAULT '',
    spinnaker_execution_id   TEXT     NOT NULL DEFAULT '',
    spinnaker_execution_url  TEXT     NOT NULL DEFAULT '',
    updated_at               DATETIME NOT NULL,
    PRIMARY KEY (cluster, namespace, deployment_name)
);
`

func NewSpinnakerHistoryStore(dbPath string) (*SpinnakerHistoryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("criar diretório spinnaker history: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir spinnaker_history.db: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping spinnaker_history.db: %w", err)
	}
	if _, err := db.Exec(spinnakerHistorySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("criar schema spinnaker_history: %w", err)
	}
	return &SpinnakerHistoryStore{db: db}, nil
}

func (s *SpinnakerHistoryStore) Close() error {
	return s.db.Close()
}

// Upsert grava (ou atualiza) o último rollout status confirmado ao vivo pra um deployment.
// Chamado só quando DetectRollback retorna Matched=true — nunca grava "não encontrado", que não
// tem informação nenhuma pra preservar.
func (s *SpinnakerHistoryStore) Upsert(rec SpinnakerRolloutRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	isRollback := 0
	if rec.IsRollback {
		isRollback = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO spinnaker_rollout_status (
			cluster, namespace, deployment_name, is_rollback, rollback_type, last_chg_applied,
			pipeline_executed_at, execution_status, rollback_started_at, rollback_ended_at,
			failed_chg, rollback_pipeline_name, spinnaker_execution_id, spinnaker_execution_url, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(cluster, namespace, deployment_name) DO UPDATE SET
			is_rollback=excluded.is_rollback,
			rollback_type=excluded.rollback_type,
			last_chg_applied=excluded.last_chg_applied,
			pipeline_executed_at=excluded.pipeline_executed_at,
			execution_status=excluded.execution_status,
			rollback_started_at=excluded.rollback_started_at,
			rollback_ended_at=excluded.rollback_ended_at,
			failed_chg=excluded.failed_chg,
			rollback_pipeline_name=excluded.rollback_pipeline_name,
			spinnaker_execution_id=excluded.spinnaker_execution_id,
			spinnaker_execution_url=excluded.spinnaker_execution_url,
			updated_at=excluded.updated_at`,
		rec.Cluster, rec.Namespace, rec.DeploymentName, isRollback, rec.RollbackType, rec.LastCHGApplied,
		rec.PipelineExecutedAt, rec.ExecutionStatus, rec.RollbackStartedAt, rec.RollbackEndedAt,
		rec.FailedCHG, rec.RollbackPipelineName, rec.SpinnakerExecutionID, rec.SpinnakerExecutionURL, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert spinnaker rollout status: %w", err)
	}
	return nil
}

// Get retorna o último status persistido de um deployment específico, ou (nil, sql.ErrNoRows)
// se nunca foi visto. Usado como fallback quando a busca ao vivo no Gate não encontra nada
// dentro da janela atual.
func (s *SpinnakerHistoryStore) Get(cluster, namespace, deploymentName string) (*SpinnakerRolloutRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rec SpinnakerRolloutRecord
	var isRollback int
	err := s.db.QueryRow(
		`SELECT cluster, namespace, deployment_name, is_rollback, rollback_type, last_chg_applied,
			pipeline_executed_at, execution_status, rollback_started_at, rollback_ended_at,
			failed_chg, rollback_pipeline_name, spinnaker_execution_id, spinnaker_execution_url, updated_at
		 FROM spinnaker_rollout_status WHERE cluster=? AND namespace=? AND deployment_name=?`,
		cluster, namespace, deploymentName,
	).Scan(
		&rec.Cluster, &rec.Namespace, &rec.DeploymentName, &isRollback, &rec.RollbackType, &rec.LastCHGApplied,
		&rec.PipelineExecutedAt, &rec.ExecutionStatus, &rec.RollbackStartedAt, &rec.RollbackEndedAt,
		&rec.FailedCHG, &rec.RollbackPipelineName, &rec.SpinnakerExecutionID, &rec.SpinnakerExecutionURL, &rec.UpdatedAt,
	)
	if err != nil {
		return nil, err // inclui sql.ErrNoRows
	}
	rec.IsRollback = isRollback != 0
	return &rec, nil
}

// GetAll retorna o último status persistido de todos os deployments de um cluster (namespace
// vazio = todos os namespaces) — usado como fallback quando a busca ao vivo no Gate não
// encontrou nada dentro da janela atual, mas já vimos esse deployment antes.
func (s *SpinnakerHistoryStore) GetAll(cluster, namespace string) (map[string]SpinnakerRolloutRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT cluster, namespace, deployment_name, is_rollback, rollback_type, last_chg_applied,
		pipeline_executed_at, execution_status, rollback_started_at, rollback_ended_at,
		failed_chg, rollback_pipeline_name, spinnaker_execution_id, spinnaker_execution_url, updated_at
		FROM spinnaker_rollout_status WHERE cluster=?`
	args := []interface{}{cluster}
	if namespace != "" {
		query += " AND namespace=?"
		args = append(args, namespace)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listar spinnaker rollout status: %w", err)
	}
	defer rows.Close()

	result := map[string]SpinnakerRolloutRecord{}
	for rows.Next() {
		var rec SpinnakerRolloutRecord
		var isRollback int
		if err := rows.Scan(
			&rec.Cluster, &rec.Namespace, &rec.DeploymentName, &isRollback, &rec.RollbackType, &rec.LastCHGApplied,
			&rec.PipelineExecutedAt, &rec.ExecutionStatus, &rec.RollbackStartedAt, &rec.RollbackEndedAt,
			&rec.FailedCHG, &rec.RollbackPipelineName, &rec.SpinnakerExecutionID, &rec.SpinnakerExecutionURL, &rec.UpdatedAt,
		); err != nil {
			continue
		}
		rec.IsRollback = isRollback != 0
		result[rec.DeploymentName+"/"+rec.Namespace] = rec
	}
	return result, nil
}
