package healthcheck

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
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

	CREATE TABLE IF NOT EXISTS health_check_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		cluster TEXT NOT NULL,
		event_type TEXT NOT NULL,
		phase TEXT NOT NULL,
		message TEXT NOT NULL,
		progress INTEGER NOT NULL,
		status TEXT NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_cluster ON health_check_results(cluster);
	CREATE INDEX IF NOT EXISTS idx_namespace ON health_check_results(namespace);
	CREATE INDEX IF NOT EXISTS idx_started_at ON health_check_results(started_at DESC);
	CREATE INDEX IF NOT EXISTS idx_overall_status ON health_check_results(overall_status);

	CREATE INDEX IF NOT EXISTS idx_events_session ON health_check_events(session_id);
	CREATE INDEX IF NOT EXISTS idx_events_cluster ON health_check_events(cluster);
	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON health_check_events(timestamp DESC);
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

// Delete deleta um resultado específico
func (s *HealthCheckStorage) Delete(ctx context.Context, resultID string) error {
	query := "DELETE FROM health_check_results WHERE id = ?"

	result, err := s.db.ExecContext(ctx, query, resultID)
	if err != nil {
		return fmt.Errorf("failed to delete result: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("health check result not found: %s", resultID)
	}

	log.Info().Str("result_id", resultID).Msg("Deleted health check result")

	return nil
}

// GetStats retorna estatísticas agregadas de health checks
func (s *HealthCheckStorage) GetStats(ctx context.Context, cluster, daysStr string) (map[string]interface{}, error) {
	days := 7 // Default 7 dias
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	since := time.Now().AddDate(0, 0, -days)

	query := `
		SELECT
			COUNT(*) as total_runs,
			SUM(total_checks) as total_checks,
			SUM(healthy_count) as total_healthy,
			SUM(warning_count) as total_warnings,
			SUM(critical_count) as total_critical,
			AVG(duration_ms) as avg_duration_ms,
			COUNT(CASE WHEN overall_status = 'healthy' THEN 1 END) as healthy_runs,
			COUNT(CASE WHEN overall_status = 'warning' THEN 1 END) as warning_runs,
			COUNT(CASE WHEN overall_status = 'critical' THEN 1 END) as critical_runs
		FROM health_check_results
		WHERE started_at >= ?
	`

	args := []interface{}{since}

	if cluster != "" {
		query += " AND cluster = ?"
		args = append(args, cluster)
	}

	var stats struct {
		TotalRuns      int
		TotalChecks    int
		TotalHealthy   int
		TotalWarnings  int
		TotalCritical  int
		AvgDurationMs  float64
		HealthyRuns    int
		WarningRuns    int
		CriticalRuns   int
	}

	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&stats.TotalRuns,
		&stats.TotalChecks,
		&stats.TotalHealthy,
		&stats.TotalWarnings,
		&stats.TotalCritical,
		&stats.AvgDurationMs,
		&stats.HealthyRuns,
		&stats.WarningRuns,
		&stats.CriticalRuns,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}

	return map[string]interface{}{
		"total_runs":       stats.TotalRuns,
		"total_checks":     stats.TotalChecks,
		"total_healthy":    stats.TotalHealthy,
		"total_warnings":   stats.TotalWarnings,
		"total_critical":   stats.TotalCritical,
		"avg_duration_ms":  stats.AvgDurationMs,
		"healthy_runs":     stats.HealthyRuns,
		"warning_runs":     stats.WarningRuns,
		"critical_runs":    stats.CriticalRuns,
		"days":             days,
		"since":            since,
	}, nil
}

// ProgressEvent representa um evento de progresso
type ProgressEvent struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Cluster   string    `json:"cluster"`
	Type      string    `json:"type"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	Progress  int       `json:"progress"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// SaveEvent salva um evento de progresso
func (s *HealthCheckStorage) SaveEvent(ctx context.Context, event *ProgressEvent) error {
	query := `
	INSERT INTO health_check_events (
		session_id, cluster, event_type, phase, message, progress, status, timestamp
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query,
		event.SessionID,
		event.Cluster,
		event.Type,
		event.Phase,
		event.Message,
		event.Progress,
		event.Status,
		event.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to insert progress event: %w", err)
	}

	id, _ := result.LastInsertId()
	event.ID = id

	return nil
}

// GetEvents retorna eventos de progresso de um cluster (por sessionID)
func (s *HealthCheckStorage) GetEvents(ctx context.Context, sessionID string) ([]*ProgressEvent, error) {
	query := `
	SELECT id, session_id, cluster, event_type, phase, message, progress, status, timestamp
	FROM health_check_events
	WHERE session_id = ?
	ORDER BY timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	events := []*ProgressEvent{}

	for rows.Next() {
		var event ProgressEvent

		err := rows.Scan(
			&event.ID,
			&event.SessionID,
			&event.Cluster,
			&event.Type,
			&event.Phase,
			&event.Message,
			&event.Progress,
			&event.Status,
			&event.Timestamp,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan event row: %w", err)
		}

		events = append(events, &event)
	}

	return events, nil
}

// DeleteEvents deleta eventos de um sessionID específico
func (s *HealthCheckStorage) DeleteEvents(ctx context.Context, sessionID string) error {
	query := "DELETE FROM health_check_events WHERE session_id = ?"

	result, err := s.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete events: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Info().
		Str("session_id", sessionID).
		Int64("rows_deleted", rowsAffected).
		Msg("Deleted progress events")

	return nil
}

// Close fecha a conexão com o banco
func (s *HealthCheckStorage) Close() error {
	return s.db.Close()
}

// ========== Métricas para Dashboard ==========

// ClusterMetrics representa métricas agregadas de um cluster
type ClusterMetrics struct {
	Cluster        string    `json:"cluster"`
	LastStatus     string    `json:"last_status"`
	LastRunAt      time.Time `json:"last_run_at"`
	TotalRuns      int       `json:"total_runs"`
	HealthyRuns    int       `json:"healthy_runs"`
	WarningRuns    int       `json:"warning_runs"`
	CriticalRuns   int       `json:"critical_runs"`
	AvgDurationMs  float64   `json:"avg_duration_ms"`
	TotalHealthy   int       `json:"total_healthy"`
	TotalWarnings  int       `json:"total_warnings"`
	TotalCritical  int       `json:"total_critical"`
}

// DurationDataPoint representa um ponto de dados de duração ao longo do tempo
type DurationDataPoint struct {
	Timestamp  string `json:"timestamp"` // RFC3339 format string para compatibilidade SQLite
	Cluster    string `json:"cluster"`
	DurationMs int64  `json:"duration_ms"`
	Status     string `json:"status"`
}

// SeverityCount representa contagem de issues por severidade
type SeverityCount struct {
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

// DashboardMetrics representa todas as métricas para o dashboard
type DashboardMetrics struct {
	// Métricas por cluster
	ClusterMetrics []ClusterMetrics `json:"cluster_metrics"`

	// Histórico de duração (últimas 50 execuções)
	DurationHistory []DurationDataPoint `json:"duration_history"`

	// Contagem de issues por severidade (agregado)
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`

	// Resumo geral
	Summary struct {
		TotalClusters   int     `json:"total_clusters"`
		TotalRuns       int     `json:"total_runs"`
		HealthRate      float64 `json:"health_rate"`
		AvgDurationMs   float64 `json:"avg_duration_ms"`
		LastRunAt       *time.Time `json:"last_run_at"`
	} `json:"summary"`
}

// GetDashboardMetrics retorna métricas para o dashboard de health checking
func (s *HealthCheckStorage) GetDashboardMetrics(ctx context.Context, days int) (*DashboardMetrics, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)

	metrics := &DashboardMetrics{
		ClusterMetrics:    []ClusterMetrics{},
		DurationHistory:   []DurationDataPoint{},
		SeverityBreakdown: []SeverityCount{},
	}

	// 1. Métricas por cluster
	clusterQuery := `
		SELECT
			cluster,
			MAX(started_at) as last_run_at,
			COUNT(*) as total_runs,
			COUNT(CASE WHEN overall_status = 'healthy' THEN 1 END) as healthy_runs,
			COUNT(CASE WHEN overall_status = 'warning' THEN 1 END) as warning_runs,
			COUNT(CASE WHEN overall_status = 'critical' THEN 1 END) as critical_runs,
			AVG(duration_ms) as avg_duration_ms,
			SUM(healthy_count) as total_healthy,
			SUM(warning_count) as total_warnings,
			SUM(critical_count) as total_critical
		FROM health_check_results
		WHERE started_at >= ?
		GROUP BY cluster
		ORDER BY last_run_at DESC
	`

	rows, err := s.db.QueryContext(ctx, clusterQuery, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster metrics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cm ClusterMetrics
		var lastRunAtStr sql.NullString
		var avgDuration sql.NullFloat64

		err := rows.Scan(
			&cm.Cluster,
			&lastRunAtStr,
			&cm.TotalRuns,
			&cm.HealthyRuns,
			&cm.WarningRuns,
			&cm.CriticalRuns,
			&avgDuration,
			&cm.TotalHealthy,
			&cm.TotalWarnings,
			&cm.TotalCritical,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cluster row: %w", err)
		}

		// Parse timestamp string para time.Time
		if lastRunAtStr.Valid && lastRunAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339, lastRunAtStr.String); err == nil {
				cm.LastRunAt = t
			} else if t, err := time.Parse("2006-01-02 15:04:05", lastRunAtStr.String); err == nil {
				cm.LastRunAt = t
			} else if t, err := time.Parse("2006-01-02T15:04:05Z", lastRunAtStr.String); err == nil {
				cm.LastRunAt = t
			}
		}
		if avgDuration.Valid {
			cm.AvgDurationMs = avgDuration.Float64
		}

		metrics.ClusterMetrics = append(metrics.ClusterMetrics, cm)
	}

	// Buscar último status de cada cluster
	for i := range metrics.ClusterMetrics {
		statusQuery := `
			SELECT overall_status FROM health_check_results
			WHERE cluster = ?
			ORDER BY started_at DESC
			LIMIT 1
		`
		var status string
		err := s.db.QueryRowContext(ctx, statusQuery, metrics.ClusterMetrics[i].Cluster).Scan(&status)
		if err == nil {
			metrics.ClusterMetrics[i].LastStatus = status
		}
	}

	// 2. Histórico de duração (últimas 50 execuções)
	durationQuery := `
		SELECT started_at, cluster, duration_ms, overall_status
		FROM health_check_results
		WHERE started_at >= ?
		ORDER BY started_at DESC
		LIMIT 50
	`

	durationRows, err := s.db.QueryContext(ctx, durationQuery, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query duration history: %w", err)
	}
	defer durationRows.Close()

	for durationRows.Next() {
		var dp DurationDataPoint
		err := durationRows.Scan(&dp.Timestamp, &dp.Cluster, &dp.DurationMs, &dp.Status)
		if err != nil {
			continue
		}
		metrics.DurationHistory = append(metrics.DurationHistory, dp)
	}

	// Inverter para ordem cronológica (mais antigo primeiro)
	for i, j := 0, len(metrics.DurationHistory)-1; i < j; i, j = i+1, j-1 {
		metrics.DurationHistory[i], metrics.DurationHistory[j] = metrics.DurationHistory[j], metrics.DurationHistory[i]
	}

	// 3. Breakdown de severidade (agregado de todos os clusters)
	severityQuery := `
		SELECT
			SUM(healthy_count) as healthy,
			SUM(warning_count) as warning,
			SUM(critical_count) as critical
		FROM health_check_results
		WHERE started_at >= ?
	`

	var healthy, warning, critical int
	err = s.db.QueryRowContext(ctx, severityQuery, since).Scan(&healthy, &warning, &critical)
	if err == nil {
		metrics.SeverityBreakdown = []SeverityCount{
			{Severity: "healthy", Count: healthy},
			{Severity: "warning", Count: warning},
			{Severity: "critical", Count: critical},
		}
	}

	// 4. Resumo geral
	summaryQuery := `
		SELECT
			COUNT(DISTINCT cluster) as total_clusters,
			COUNT(*) as total_runs,
			AVG(duration_ms) as avg_duration_ms,
			MAX(started_at) as last_run_at,
			COUNT(CASE WHEN overall_status = 'healthy' THEN 1 END) as healthy_runs
		FROM health_check_results
		WHERE started_at >= ?
	`

	var totalClusters, totalRuns, healthyRuns int
	var avgDuration sql.NullFloat64
	var lastRunAtStr sql.NullString

	err = s.db.QueryRowContext(ctx, summaryQuery, since).Scan(
		&totalClusters,
		&totalRuns,
		&avgDuration,
		&lastRunAtStr,
		&healthyRuns,
	)
	if err == nil {
		metrics.Summary.TotalClusters = totalClusters
		metrics.Summary.TotalRuns = totalRuns
		if avgDuration.Valid {
			metrics.Summary.AvgDurationMs = avgDuration.Float64
		}
		// Parse timestamp string para time.Time
		if lastRunAtStr.Valid && lastRunAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339, lastRunAtStr.String); err == nil {
				metrics.Summary.LastRunAt = &t
			} else if t, err := time.Parse("2006-01-02 15:04:05", lastRunAtStr.String); err == nil {
				metrics.Summary.LastRunAt = &t
			} else if t, err := time.Parse("2006-01-02T15:04:05Z", lastRunAtStr.String); err == nil {
				metrics.Summary.LastRunAt = &t
			}
		}
		if totalRuns > 0 {
			metrics.Summary.HealthRate = float64(healthyRuns) / float64(totalRuns) * 100
		}
	}

	return metrics, nil
}
