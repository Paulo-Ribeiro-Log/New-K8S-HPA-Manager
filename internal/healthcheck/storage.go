package healthcheck

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// HealthCheckStorage armazena resultados de health checks
type HealthCheckStorage struct {
	db *sql.DB
}

// NewHealthCheckStorage cria um novo storage
func NewHealthCheckStorage(dbPath string) (*HealthCheckStorage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	storage := &HealthCheckStorage{db: db}

	// Criar tabela se não existir
	if err := storage.createTable(); err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	return storage, nil
}

// createTable cria tabela de health checks
func (s *HealthCheckStorage) createTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS health_check_results (
		id TEXT PRIMARY KEY,
		cluster TEXT NOT NULL,
		namespace TEXT NOT NULL,
		started_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NOT NULL,
		duration_ms INTEGER NOT NULL,

		-- Resumo
		total_checks INTEGER NOT NULL,
		healthy_count INTEGER NOT NULL,
		warning_count INTEGER NOT NULL,
		critical_count INTEGER NOT NULL,
		overall_status TEXT NOT NULL,

		-- JSON blobs com resultados detalhados
		deployment_results TEXT,
		service_results TEXT,
		config_results TEXT,

		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_cluster ON health_check_results(cluster);
	CREATE INDEX IF NOT EXISTS idx_namespace ON health_check_results(namespace);
	CREATE INDEX IF NOT EXISTS idx_started_at ON health_check_results(started_at DESC);
	CREATE INDEX IF NOT EXISTS idx_overall_status ON health_check_results(overall_status);
	`

	_, err := s.db.Exec(query)
	return err
}

// Save salva resultado de health check
func (s *HealthCheckStorage) Save(ctx context.Context, result *HealthCheckResult) error {
	// Serializar arrays para JSON
	deploymentJSON, err := json.Marshal(result.DeploymentResults)
	if err != nil {
		return fmt.Errorf("failed to marshal deployment results: %w", err)
	}

	serviceJSON, err := json.Marshal(result.ServiceResults)
	if err != nil {
		return fmt.Errorf("failed to marshal service results: %w", err)
	}

	configJSON, err := json.Marshal(result.ConfigResults)
	if err != nil {
		return fmt.Errorf("failed to marshal config results: %w", err)
	}

	query := `
	INSERT INTO health_check_results (
		id, cluster, namespace, started_at, finished_at, duration_ms,
		total_checks, healthy_count, warning_count, critical_count, overall_status,
		deployment_results, service_results, config_results
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		result.ID,
		result.Cluster,
		result.Namespace,
		result.StartedAt,
		result.FinishedAt,
		result.Duration,
		result.TotalChecks,
		result.HealthyCount,
		result.WarningCount,
		result.CriticalCount,
		result.OverallStatus,
		string(deploymentJSON),
		string(serviceJSON),
		string(configJSON),
	)

	if err != nil {
		return fmt.Errorf("failed to insert health check result: %w", err)
	}

	log.Info().
		Str("id", result.ID).
		Str("cluster", result.Cluster).
		Int("total_checks", result.TotalChecks).
		Msg("Health check result saved to database")

	return nil
}

// Get retorna resultado específico por ID
func (s *HealthCheckStorage) Get(ctx context.Context, id string) (*HealthCheckResult, error) {
	query := `
	SELECT id, cluster, namespace, started_at, finished_at, duration_ms,
		   total_checks, healthy_count, warning_count, critical_count, overall_status,
		   deployment_results, service_results, config_results
	FROM health_check_results
	WHERE id = ?
	`

	var result HealthCheckResult
	var deploymentJSON, serviceJSON, configJSON string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&result.ID,
		&result.Cluster,
		&result.Namespace,
		&result.StartedAt,
		&result.FinishedAt,
		&result.Duration,
		&result.TotalChecks,
		&result.HealthyCount,
		&result.WarningCount,
		&result.CriticalCount,
		&result.OverallStatus,
		&deploymentJSON,
		&serviceJSON,
		&configJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("health check result not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query health check result: %w", err)
	}

	// Deserializar JSON
	if err := json.Unmarshal([]byte(deploymentJSON), &result.DeploymentResults); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deployment results: %w", err)
	}
	if err := json.Unmarshal([]byte(serviceJSON), &result.ServiceResults); err != nil {
		return nil, fmt.Errorf("failed to unmarshal service results: %w", err)
	}
	if err := json.Unmarshal([]byte(configJSON), &result.ConfigResults); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config results: %w", err)
	}

	return &result, nil
}

// GetHistory retorna histórico de health checks
func (s *HealthCheckStorage) GetHistory(ctx context.Context, cluster, namespace string, limit int) ([]*HealthCheckResult, error) {
	query := `
	SELECT id, cluster, namespace, started_at, finished_at, duration_ms,
		   total_checks, healthy_count, warning_count, critical_count, overall_status,
		   deployment_results, service_results, config_results
	FROM health_check_results
	WHERE 1=1
	`

	args := []interface{}{}

	if cluster != "" {
		query += " AND cluster = ?"
		args = append(args, cluster)
	}

	if namespace != "" {
		query += " AND namespace = ?"
		args = append(args, namespace)
	}

	query += " ORDER BY started_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query history: %w", err)
	}
	defer rows.Close()

	results := []*HealthCheckResult{}

	for rows.Next() {
		var result HealthCheckResult
		var deploymentJSON, serviceJSON, configJSON string

		err := rows.Scan(
			&result.ID,
			&result.Cluster,
			&result.Namespace,
			&result.StartedAt,
			&result.FinishedAt,
			&result.Duration,
			&result.TotalChecks,
			&result.HealthyCount,
			&result.WarningCount,
			&result.CriticalCount,
			&result.OverallStatus,
			&deploymentJSON,
			&serviceJSON,
			&configJSON,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Deserializar JSON
		if err := json.Unmarshal([]byte(deploymentJSON), &result.DeploymentResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal deployment results: %w", err)
		}
		if err := json.Unmarshal([]byte(serviceJSON), &result.ServiceResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal service results: %w", err)
		}
		if err := json.Unmarshal([]byte(configJSON), &result.ConfigResults); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config results: %w", err)
		}

		results = append(results, &result)
	}

	return results, nil
}

// DeleteOlderThan remove health checks mais antigos que a data especificada
func (s *HealthCheckStorage) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	query := "DELETE FROM health_check_results WHERE started_at < ?"

	result, err := s.db.ExecContext(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old health checks: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()

	log.Info().
		Int64("rows_deleted", rowsAffected).
		Time("before", before).
		Msg("Deleted old health check results")

	return rowsAffected, nil
}

// Close fecha a conexão com o banco
func (s *HealthCheckStorage) Close() error {
	return s.db.Close()
}
