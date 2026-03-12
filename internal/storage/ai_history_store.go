package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AIHistoryStore armazena histórico de análises AI
type AIHistoryStore struct {
	client *SQLiteClient
}

// NewAIHistoryStore cria um novo AIHistoryStore
func NewAIHistoryStore(client *SQLiteClient) *AIHistoryStore {
	s := &AIHistoryStore{client: client}
	go s.cleanupLoop()
	return s
}

// cleanupLoop remove registros antigos automaticamente (executa em background)
func (s *AIHistoryStore) cleanupLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	s.runCleanup()
	for range ticker.C {
		s.runCleanup()
	}
}

// runCleanup deleta análises AI com mais de 30 dias
func (s *AIHistoryStore) runCleanup() {
	cutoff := time.Now().AddDate(0, 0, -30)
	_, _ = s.DeleteOlderThan(cutoff)
}

// Save salva um registro de análise AI
func (s *AIHistoryStore) Save(record *HistoryRecord) error {
	query := `
INSERT INTO ai_analysis_history (
    id, resource_type, cluster, namespace, resource_name,
    provider, model, analysis, suggestions, prometheus_metrics, resource_metadata,
    tokens_used, response_time, analyzed_at, user_email
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

	// Converter sql.NullString para valores compatíveis com SQLite
	var prometheusMetrics interface{}
	if record.PrometheusMetrics.Valid {
		prometheusMetrics = record.PrometheusMetrics.String
	} else {
		prometheusMetrics = nil
	}

	var resourceMetadata interface{}
	if record.ResourceMetadata.Valid {
		resourceMetadata = record.ResourceMetadata.String
	} else {
		resourceMetadata = nil
	}

	_, err := s.client.Exec(query,
		record.ID,
		record.ResourceType,
		record.Cluster,
		record.Namespace,
		record.ResourceName,
		record.Provider,
		record.Model,
		record.Analysis,
		record.Suggestions,
		prometheusMetrics, // FASE 2.1
		resourceMetadata,  // FASE 3.5 - 08/01/2026
		record.TokensUsed,
		record.ResponseTime,
		record.AnalyzedAt,
		record.UserEmail,
	)

	return err
}

// GetByID obtém um registro pelo ID
func (s *AIHistoryStore) GetByID(id string) (*HistoryRecord, error) {
	query := `
SELECT id, resource_type, cluster, namespace, resource_name,
       provider, model, analysis, suggestions, prometheus_metrics, resource_metadata,
       tokens_used, response_time, analyzed_at, user_email, created_at
FROM ai_analysis_history
WHERE id = ?
`

	row := s.client.QueryRow(query, id)

	record := &HistoryRecord{}
	err := row.Scan(
		&record.ID,
		&record.ResourceType,
		&record.Cluster,
		&record.Namespace,
		&record.ResourceName,
		&record.Provider,
		&record.Model,
		&record.Analysis,
		&record.Suggestions,
		&record.PrometheusMetrics, // FASE 2.1
		&record.ResourceMetadata,  // FASE 3.5 - 08/01/2026
		&record.TokensUsed,
		&record.ResponseTime,
		&record.AnalyzedAt,
		&record.UserEmail,
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

// Query busca registros com filtros
func (s *AIHistoryStore) Query(filters *QueryFilters) ([]*HistoryRecord, error) {
	query := `
SELECT id, resource_type, cluster, namespace, resource_name,
       provider, model, analysis, suggestions, prometheus_metrics, resource_metadata,
       tokens_used, response_time, analyzed_at, user_email, created_at
FROM ai_analysis_history
WHERE 1=1
`
	args := []interface{}{}

	// Aplicar filtros
	if filters.Cluster != "" {
		query += " AND cluster = ?"
		args = append(args, filters.Cluster)
	}

	if filters.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, filters.Namespace)
	}

	if filters.ResourceType != "" {
		query += " AND resource_type = ?"
		args = append(args, filters.ResourceType)
	}

	if filters.ResourceName != "" {
		query += " AND resource_name = ?"
		args = append(args, filters.ResourceName)
	}

	if filters.Provider != "" {
		query += " AND provider = ?"
		args = append(args, filters.Provider)
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

	// Ordenar por data mais recente
	query += " ORDER BY analyzed_at DESC"

	// Paginação
	if filters.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filters.Limit)

		if filters.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filters.Offset)
		}
	}

	rows, err := s.client.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []*HistoryRecord{}
	for rows.Next() {
		record := &HistoryRecord{}
		err := rows.Scan(
			&record.ID,
			&record.ResourceType,
			&record.Cluster,
			&record.Namespace,
			&record.ResourceName,
			&record.Provider,
			&record.Model,
			&record.Analysis,
			&record.Suggestions,
			&record.PrometheusMetrics, // FASE 2.1
			&record.ResourceMetadata,  // FASE 3.5 - 08/01/2026
			&record.TokensUsed,
			&record.ResponseTime,
			&record.AnalyzedAt,
			&record.UserEmail,
			&record.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}

// Delete deleta um registro pelo ID
func (s *AIHistoryStore) Delete(id string) error {
	query := "DELETE FROM ai_analysis_history WHERE id = ?"
	_, err := s.client.Exec(query, id)
	return err
}

// DeleteOlderThan deleta registros mais antigos que uma data
func (s *AIHistoryStore) DeleteOlderThan(date time.Time) (int64, error) {
	query := "DELETE FROM ai_analysis_history WHERE analyzed_at < ?"
	result, err := s.client.Exec(query, date)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Count retorna contagem total de registros com filtros
func (s *AIHistoryStore) Count(filters *QueryFilters) (int64, error) {
	query := "SELECT COUNT(*) FROM ai_analysis_history WHERE 1=1"
	args := []interface{}{}

	// Aplicar mesmos filtros da Query()
	if filters.Cluster != "" {
		query += " AND cluster = ?"
		args = append(args, filters.Cluster)
	}

	if filters.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, filters.Namespace)
	}

	if filters.ResourceType != "" {
		query += " AND resource_type = ?"
		args = append(args, filters.ResourceType)
	}

	if filters.ResourceName != "" {
		query += " AND resource_name = ?"
		args = append(args, filters.ResourceName)
	}

	if filters.Provider != "" {
		query += " AND provider = ?"
		args = append(args, filters.Provider)
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

	var count int64
	err := s.client.QueryRow(query, args...).Scan(&count)
	return count, err
}

// GetStats retorna estatísticas do histórico
func (s *AIHistoryStore) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total de análises
	var totalAnalyses int64
	err := s.client.QueryRow("SELECT COUNT(*) FROM ai_analysis_history").Scan(&totalAnalyses)
	if err != nil {
		return nil, err
	}
	stats["total_analyses"] = totalAnalyses

	// Por provider
	providerQuery := `
SELECT provider, COUNT(*) as count
FROM ai_analysis_history
GROUP BY provider
`
	rows, err := s.client.Query(providerQuery)
	if err == nil {
		defer rows.Close()
		byProvider := make(map[string]int64)
		for rows.Next() {
			var provider string
			var count int64
			if err := rows.Scan(&provider, &count); err == nil {
				byProvider[provider] = count
			}
		}
		stats["by_provider"] = byProvider
	}

	// Por tipo de recurso
	typeQuery := `
SELECT resource_type, COUNT(*) as count
FROM ai_analysis_history
GROUP BY resource_type
`
	rows, err = s.client.Query(typeQuery)
	if err == nil {
		defer rows.Close()
		byType := make(map[string]int64)
		for rows.Next() {
			var resourceType string
			var count int64
			if err := rows.Scan(&resourceType, &count); err == nil {
				byType[resourceType] = count
			}
		}
		stats["by_resource_type"] = byType
	}

	// Tempo médio de resposta
	var avgResponseTime sql.NullFloat64
	err = s.client.QueryRow("SELECT AVG(response_time) FROM ai_analysis_history WHERE response_time IS NOT NULL").Scan(&avgResponseTime)
	if err == nil && avgResponseTime.Valid {
		stats["avg_response_time"] = avgResponseTime.Float64
	}

	// Total de tokens usados
	var totalTokensUsed sql.NullInt64
	err = s.client.QueryRow("SELECT SUM(tokens_used) FROM ai_analysis_history WHERE tokens_used IS NOT NULL").Scan(&totalTokensUsed)
	if err == nil && totalTokensUsed.Valid {
		stats["total_tokens_used"] = totalTokensUsed.Int64
	} else {
		stats["total_tokens_used"] = 0
	}

	// Última análise - usar string diretamente ao invés de NullTime
	var lastAnalysisAtStr sql.NullString
	err = s.client.QueryRow("SELECT MAX(analyzed_at) FROM ai_analysis_history").Scan(&lastAnalysisAtStr)
	if err == nil && lastAnalysisAtStr.Valid && lastAnalysisAtStr.String != "" {
		stats["last_analysis_at"] = lastAnalysisAtStr.String
	} else {
		stats["last_analysis_at"] = nil
	}

	return stats, nil
}

// Vacuum executa VACUUM no banco de dados (libera espaço)
func (s *AIHistoryStore) Vacuum() error {
	_, err := s.client.Exec("VACUUM")
	return err
}

// GetRecentByResource obtém análises recentes de um recurso específico
func (s *AIHistoryStore) GetRecentByResource(cluster, namespace, resourceName string, limit int) ([]*HistoryRecord, error) {
	query := `
SELECT id, resource_type, cluster, namespace, resource_name,
       provider, model, analysis, suggestions, prometheus_metrics, resource_metadata,
       tokens_used, response_time, analyzed_at, user_email, created_at
FROM ai_analysis_history
WHERE cluster = ? AND namespace = ? AND resource_name = ?
ORDER BY analyzed_at DESC
LIMIT ?
`

	rows, err := s.client.Query(query, cluster, namespace, resourceName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []*HistoryRecord{}
	for rows.Next() {
		record := &HistoryRecord{}
		err := rows.Scan(
			&record.ID,
			&record.ResourceType,
			&record.Cluster,
			&record.Namespace,
			&record.ResourceName,
			&record.Provider,
			&record.Model,
			&record.Analysis,
			&record.Suggestions,
			&record.PrometheusMetrics, // FASE 2.1
			&record.ResourceMetadata,  // FASE 3.5 - 08/01/2026
			&record.TokensUsed,
			&record.ResponseTime,
			&record.AnalyzedAt,
			&record.UserEmail,
			&record.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}

// Search busca texto em análises anteriores
func (s *AIHistoryStore) Search(searchTerm string, limit int) ([]*HistoryRecord, error) {
	query := `
SELECT id, resource_type, cluster, namespace, resource_name,
       provider, model, analysis, suggestions, prometheus_metrics, resource_metadata,
       tokens_used, response_time, analyzed_at, user_email, created_at
FROM ai_analysis_history
WHERE analysis LIKE ? OR suggestions LIKE ?
ORDER BY analyzed_at DESC
LIMIT ?
`

	searchPattern := "%" + strings.ToLower(searchTerm) + "%"
	rows, err := s.client.Query(query, searchPattern, searchPattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []*HistoryRecord{}
	for rows.Next() {
		record := &HistoryRecord{}
		err := rows.Scan(
			&record.ID,
			&record.ResourceType,
			&record.Cluster,
			&record.Namespace,
			&record.ResourceName,
			&record.Provider,
			&record.Model,
			&record.Analysis,
			&record.Suggestions,
			&record.PrometheusMetrics, // FASE 2.1
			&record.ResourceMetadata,  // FASE 3.5 - 08/01/2026
			&record.TokensUsed,
			&record.ResponseTime,
			&record.AnalyzedAt,
			&record.UserEmail,
			&record.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}
