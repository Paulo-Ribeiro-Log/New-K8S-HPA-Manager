package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PredictionsStore armazena histórico de análises preditivas
type PredictionsStore struct {
	client *SQLiteClient
}

// NewPredictionsStore cria um novo PredictionsStore
func NewPredictionsStore(client *SQLiteClient) *PredictionsStore {
	return &PredictionsStore{
		client: client,
	}
}

// SavePrediction salva uma análise preditiva (aceita dados estruturados)
func (s *PredictionsStore) SavePrediction(
	requestID, cluster, namespace, deployment string,
	healthScore float64,
	riskLevel string,
	executiveSummary, predictions, recommendations, rawMetrics interface{},
	provider, model string,
	durationMs int64,
	userEmail string,
	analyzedAt time.Time,
) error {
	// Serializar estruturas complexas para JSON
	executiveSummaryJSON, _ := json.Marshal(executiveSummary)
	predictionsJSON, _ := json.Marshal(predictions)
	recommendationsJSON, _ := json.Marshal(recommendations)
	rawMetricsJSON, _ := json.Marshal(rawMetrics)

	query := `
INSERT INTO predictions_history (
    id, cluster, namespace, deployment,
    health_score, risk_level, executive_summary, predictions,
    recommendations, raw_metrics, provider, model,
    duration_ms, user_email, analyzed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

	_, err := s.client.Exec(query,
		requestID,
		cluster,
		namespace,
		deployment,
		healthScore,
		riskLevel,
		string(executiveSummaryJSON),
		string(predictionsJSON),
		string(recommendationsJSON),
		string(rawMetricsJSON),
		provider,
		model,
		durationMs,
		userEmail,
		analyzedAt.Format(time.RFC3339),
	)

	return err
}

// GetByID obtém uma análise pelo ID
func (s *PredictionsStore) GetByID(id string) (*PredictionRecord, error) {
	query := `
SELECT id, cluster, namespace, deployment,
       health_score, risk_level, executive_summary, predictions,
       recommendations, raw_metrics, provider, model,
       duration_ms, user_email, analyzed_at, created_at
FROM predictions_history
WHERE id = ?
`

	row := s.client.QueryRow(query, id)

	record := &PredictionRecord{}
	err := row.Scan(
		&record.ID,
		&record.Cluster,
		&record.Namespace,
		&record.Deployment,
		&record.HealthScore,
		&record.RiskLevel,
		&record.ExecutiveSummary,
		&record.Predictions,
		&record.Recommendations,
		&record.RawMetrics,
		&record.Provider,
		&record.Model,
		&record.DurationMs,
		&record.UserEmail,
		&record.AnalyzedAt,
		&record.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("record not found")
	}

	if err != nil {
		return nil, err
	}

	return record, nil
}

// Query busca análises com filtros
func (s *PredictionsStore) Query(filters *PredictionQueryFilters) ([]*PredictionRecord, error) {
	query := `
SELECT id, cluster, namespace, deployment,
       health_score, risk_level, executive_summary, predictions,
       recommendations, raw_metrics, provider, model,
       duration_ms, user_email, analyzed_at, created_at
FROM predictions_history
WHERE 1=1
`
	args := []interface{}{}

	if filters.Cluster != "" {
		query += " AND cluster = ?"
		args = append(args, filters.Cluster)
	}

	if filters.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, filters.Namespace)
	}

	if filters.Deployment != "" {
		query += " AND deployment = ?"
		args = append(args, filters.Deployment)
	}

	if filters.RiskLevel != "" {
		query += " AND risk_level = ?"
		args = append(args, filters.RiskLevel)
	}

	if filters.UserEmail != "" {
		query += " AND user_email = ?"
		args = append(args, filters.UserEmail)
	}

	if filters.StartDate != nil {
		query += " AND analyzed_at >= ?"
		args = append(args, filters.StartDate)
	}

	if filters.EndDate != nil {
		query += " AND analyzed_at <= ?"
		args = append(args, filters.EndDate)
	}

	// Ordenar por data (mais recente primeiro)
	query += " ORDER BY analyzed_at DESC"

	// Paginação
	if filters.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filters.Limit)
	}

	if filters.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filters.Offset)
	}

	rows, err := s.client.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []*PredictionRecord{}
	for rows.Next() {
		record := &PredictionRecord{}
		err := rows.Scan(
			&record.ID,
			&record.Cluster,
			&record.Namespace,
			&record.Deployment,
			&record.HealthScore,
			&record.RiskLevel,
			&record.ExecutiveSummary,
			&record.Predictions,
			&record.Recommendations,
			&record.RawMetrics,
			&record.Provider,
			&record.Model,
			&record.DurationMs,
			&record.UserEmail,
			&record.AnalyzedAt,
			&record.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, nil
}

// GetLatestForDeployment obtém a análise mais recente de um deployment
func (s *PredictionsStore) GetLatestForDeployment(cluster, namespace, deployment string) (*PredictionRecord, error) {
	query := `
SELECT id, cluster, namespace, deployment,
       health_score, risk_level, executive_summary, predictions,
       recommendations, raw_metrics, provider, model,
       duration_ms, user_email, analyzed_at, created_at
FROM predictions_history
WHERE cluster = ? AND namespace = ? AND deployment = ?
ORDER BY analyzed_at DESC
LIMIT 1
`

	row := s.client.QueryRow(query, cluster, namespace, deployment)

	record := &PredictionRecord{}
	err := row.Scan(
		&record.ID,
		&record.Cluster,
		&record.Namespace,
		&record.Deployment,
		&record.HealthScore,
		&record.RiskLevel,
		&record.ExecutiveSummary,
		&record.Predictions,
		&record.Recommendations,
		&record.RawMetrics,
		&record.Provider,
		&record.Model,
		&record.DurationMs,
		&record.UserEmail,
		&record.AnalyzedAt,
		&record.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Nenhum registro encontrado
	}

	if err != nil {
		return nil, err
	}

	return record, nil
}

// Count conta o total de registros com os filtros
func (s *PredictionsStore) Count(filters *PredictionQueryFilters) (int, error) {
	query := "SELECT COUNT(*) FROM predictions_history WHERE 1=1"
	args := []interface{}{}

	if filters.Cluster != "" {
		query += " AND cluster = ?"
		args = append(args, filters.Cluster)
	}

	if filters.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, filters.Namespace)
	}

	if filters.Deployment != "" {
		query += " AND deployment = ?"
		args = append(args, filters.Deployment)
	}

	if filters.RiskLevel != "" {
		query += " AND risk_level = ?"
		args = append(args, filters.RiskLevel)
	}

	if filters.UserEmail != "" {
		query += " AND user_email = ?"
		args = append(args, filters.UserEmail)
	}

	if filters.StartDate != nil {
		query += " AND analyzed_at >= ?"
		args = append(args, filters.StartDate)
	}

	if filters.EndDate != nil {
		query += " AND analyzed_at <= ?"
		args = append(args, filters.EndDate)
	}

	var count int
	err := s.client.QueryRow(query, args...).Scan(&count)
	return count, err
}

// DeleteOlderThan remove análises mais antigas que a data especificada
func (s *PredictionsStore) DeleteOlderThan(before time.Time) (int64, error) {
	query := "DELETE FROM predictions_history WHERE analyzed_at < ?"
	result, err := s.client.Exec(query, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetStatistics retorna estatísticas das análises
func (s *PredictionsStore) GetStatistics(cluster string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total de análises
	var total int
	query := "SELECT COUNT(*) FROM predictions_history"
	args := []interface{}{}

	if cluster != "" {
		query += " WHERE cluster = ?"
		args = append(args, cluster)
	}

	err := s.client.QueryRow(query, args...).Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total"] = total

	// Análises por nível de risco
	riskQuery := `
		SELECT risk_level, COUNT(*) as count
		FROM predictions_history
	`
	if cluster != "" {
		riskQuery += " WHERE cluster = ?"
	}
	riskQuery += " GROUP BY risk_level"

	rows, err := s.client.Query(riskQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	riskCounts := make(map[string]int)
	for rows.Next() {
		var level string
		var count int
		if err := rows.Scan(&level, &count); err != nil {
			return nil, err
		}
		riskCounts[level] = count
	}
	stats["by_risk_level"] = riskCounts

	// Health score médio
	var avgScore sql.NullFloat64
	scoreQuery := "SELECT AVG(health_score) FROM predictions_history"
	if cluster != "" {
		scoreQuery += " WHERE cluster = ?"
	}
	err = s.client.QueryRow(scoreQuery, args...).Scan(&avgScore)
	if err == nil && avgScore.Valid {
		stats["avg_health_score"] = avgScore.Float64
	}

	return stats, nil
}
