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
