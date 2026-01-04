package storage

import "time"

// HistoryRecord representa um registro de histórico de análise AI
type HistoryRecord struct {
	ID           string    `json:"id"`
	ResourceType string    `json:"resource_type"`
	Cluster      string    `json:"cluster"`
	Namespace    string    `json:"namespace"`
	ResourceName string    `json:"resource_name"`
	Provider     string    `json:"provider"` // "gemini" ou "ollama"
	Model        string    `json:"model,omitempty"`
	Analysis     string    `json:"analysis"`
	Suggestions  string    `json:"suggestions"` // JSON array
	TokensUsed   int       `json:"tokens_used,omitempty"`
	ResponseTime float64   `json:"response_time,omitempty"` // seconds
	AnalyzedAt   time.Time `json:"analyzed_at"`
	UserEmail    string    `json:"user_email,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// QueryFilters filtros para consultas de histórico
type QueryFilters struct {
	Cluster      string
	Namespace    string
	ResourceType string
	ResourceName string
	Provider     string
	UserEmail    string
	StartDate    *time.Time
	EndDate      *time.Time
	Limit        int
	Offset       int
}

// PredictionRecord representa um registro de análise preditiva
type PredictionRecord struct {
	ID               string    `json:"id"`
	Cluster          string    `json:"cluster"`
	Namespace        string    `json:"namespace"`
	Deployment       string    `json:"deployment"`
	HealthScore      int       `json:"health_score"`
	RiskLevel        string    `json:"risk_level"`
	ExecutiveSummary string    `json:"executive_summary"` // JSON
	Predictions      string    `json:"predictions"`       // JSON
	Recommendations  string    `json:"recommendations"`   // JSON
	RawMetrics       string    `json:"raw_metrics"`       // JSON completo
	Provider         string    `json:"provider"`
	Model            string    `json:"model,omitempty"`
	DurationMs       int64     `json:"duration_ms"`
	UserEmail        string    `json:"user_email,omitempty"`
	AnalyzedAt       time.Time `json:"analyzed_at"`
	CreatedAt        time.Time `json:"created_at"`
}

// PredictionQueryFilters filtros para consultas de predictions
type PredictionQueryFilters struct {
	Cluster    string
	Namespace  string
	Deployment string
	RiskLevel  string
	UserEmail  string
	StartDate  *time.Time
	EndDate    *time.Time
	Limit      int
	Offset     int
}
