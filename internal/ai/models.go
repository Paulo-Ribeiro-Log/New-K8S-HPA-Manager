package ai

import "time"

// AnalysisResult representa o resultado de uma análise AI
type AnalysisResult struct {
	// ID identificador único da análise
	ID string `json:"id"`

	// ResourceType tipo do recurso analisado
	ResourceType string `json:"resource_type"`

	// Cluster nome do cluster
	Cluster string `json:"cluster"`

	// Namespace namespace do recurso
	Namespace string `json:"namespace"`

	// ResourceName nome do recurso
	ResourceName string `json:"resource_name"`

	// Provider provider AI usado (gemini, ollama)
	Provider string `json:"provider"`

	// Model modelo AI usado
	Model string `json:"model,omitempty"`

	// Analysis análise textual gerada pela AI
	Analysis string `json:"analysis"`

	// Suggestions lista de sugestões extraídas da análise
	Suggestions []Suggestion `json:"suggestions,omitempty"`

	// TokensUsed número de tokens usados (se disponível)
	TokensUsed int `json:"tokens_used,omitempty"`

	// ResponseTime tempo de resposta em segundos
	ResponseTime float64 `json:"response_time,omitempty"`

	// AnalyzedAt timestamp da análise
	AnalyzedAt time.Time `json:"analyzed_at"`

	// Error erro se análise falhou
	Error string `json:"error,omitempty"`
}

// Suggestion representa uma sugestão de ação
type Suggestion struct {
	// Type tipo da sugestão (investigate, fix, scale, update, delete)
	Type string `json:"type"`

	// Description descrição da sugestão
	Description string `json:"description"`

	// Command comando kubectl (opcional)
	Command string `json:"command,omitempty"`

	// Priority prioridade (low, medium, high, critical)
	Priority string `json:"priority"`

	// ImpactLevel nível de impacto (low, medium, high)
	ImpactLevel string `json:"impact_level,omitempty"`
}

// AnalysisRequest requisição de análise AI
type AnalysisRequest struct {
	ResourceType string
	Cluster      string
	Namespace    string
	ResourceName string

	// IncludeLogs se verdadeiro, inclui logs na análise
	IncludeLogs bool

	// IncludeMetrics se verdadeiro, inclui métricas Prometheus
	IncludeMetrics bool

	// IncludeDescribe se verdadeiro, inclui kubectl describe
	IncludeDescribe bool

	// UserEmail email do usuário que solicitou a análise
	UserEmail string
}

// ProviderStatus representa o status de um provider AI
type ProviderStatus struct {
	Provider  string `json:"provider"`
	Available bool   `json:"available"`
	Model     string `json:"model,omitempty"`
	Error     string `json:"error,omitempty"`
}
