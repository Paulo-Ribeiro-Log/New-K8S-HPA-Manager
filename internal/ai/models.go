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

	// 🆕 Estrutura nova (4 seções)
	ExecutiveSummary  ExecutiveSummary           `json:"executive_summary,omitempty"`
	RootCauseAnalysis RootCauseAnalysis          `json:"root_cause_analysis,omitempty"`
	ImpactAssessment  ImpactAssessment           `json:"impact_assessment,omitempty"`
	Recommendations   []ActionableRecommendation `json:"recommendations,omitempty"`

	// Campos legados (manter compatibilidade)
	Analysis    string       `json:"analysis,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`

	// Métricas (NOVO)
	CurrentMetrics *ResourceMetrics `json:"current_metrics,omitempty"`

	// Metadados do Recurso (NOVO - FASE 3.5 - Cabeçalho Profissional)
	ResourceMetadata *ResourceMetadata `json:"resource_metadata,omitempty"`

	// Provider provider AI usado (gemini, ollama, claude)
	Provider string `json:"provider"`

	// Model modelo AI usado
	Model string `json:"model,omitempty"`

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
	ResourceType string `json:"resourceType" binding:"required"`
	Cluster      string `json:"cluster" binding:"required"`
	Namespace    string `json:"namespace" binding:"required"`
	ResourceName string `json:"resourceName" binding:"required"`

	// IncludeLogs se verdadeiro, inclui logs na análise
	IncludeLogs bool `json:"includeLogs"`

	// IncludeMetrics se verdadeiro, inclui métricas Prometheus
	IncludeMetrics bool `json:"includeMetrics"`

	// IncludeDescribe se verdadeiro, inclui kubectl describe
	IncludeDescribe bool `json:"includeDescribe"`

	// AIEmail email usado para buscar configurações AI (independente do Azure AD)
	// Se vazio, usa provider padrão do servidor
	AIEmail string `json:"aiEmail,omitempty"`

	// UserEmail email do usuário que solicitou a análise (DEPRECATED - usar AIEmail)
	UserEmail string `json:"userEmail,omitempty"`
}

// ProviderStatus representa o status de um provider AI
type ProviderStatus struct {
	Provider        string `json:"provider"`
	Available       bool   `json:"available"`
	Model           string `json:"model,omitempty"`
	Error           string `json:"error,omitempty"`
	TotalCalls      int64  `json:"total_calls"`
	SuccessfulCalls int64  `json:"successful_calls"`
	FailedCalls     int64  `json:"failed_calls"`
}

// NOVOS STRUCTS - Análise Estruturada

// ExecutiveSummary - Resumo executivo
type ExecutiveSummary struct {
	Severity      string `json:"severity"`        // critical, high, medium, low
	Status        string `json:"status"`          // unhealthy, degraded, healthy
	QuickSummary  string `json:"quick_summary"`   // 1-2 frases
	TimeToResolve string `json:"time_to_resolve"` // ex: "15 minutes"
}

// RootCauseAnalysis - Análise de causa raiz
type RootCauseAnalysis struct {
	Symptom        string   `json:"symptom"`
	ProbableCauses []string `json:"probable_causes"` // Multi-hipótese
	Evidence       []string `json:"evidence"`        // Trechos de logs
	Confidence     string   `json:"confidence"`      // high, medium, low
}

// ImpactAssessment - Avaliação de impacto
type ImpactAssessment struct {
	Severity         string `json:"severity"`
	AffectedUsers    string `json:"affected_users"`
	DowntimeEstimate string `json:"downtime_estimate"`
	SLABreach        bool   `json:"sla_breach"`
	BusinessImpact   string `json:"business_impact"`
}

// ActionableRecommendation - Ação priorizada
type ActionableRecommendation struct {
	Priority     int      `json:"priority"`      // 1-5 (1 = mais crítico)
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Commands     []string `json:"commands"`      // kubectl/az
	TimeEstimate string   `json:"time_estimate"`
	RiskLevel    string   `json:"risk_level"`
	ImpactLevel  string   `json:"impact_level"`
}

// ResourceMetrics - Métricas Prometheus
type ResourceMetrics struct {
	CPUUsage        float64 `json:"cpu_usage"`
	CPURequest      float64 `json:"cpu_request"`
	CPULimit        float64 `json:"cpu_limit"`
	MemoryUsage     float64 `json:"memory_usage"`
	MemoryRequest   float64 `json:"memory_request"`
	MemoryLimit     float64 `json:"memory_limit"`
	RestartCount    int32   `json:"restart_count"`
	ReadyReplicas   int32   `json:"ready_replicas"`
	DesiredReplicas int32   `json:"desired_replicas"`
}

// ResourceMetadata - Metadados do recurso para cabeçalho profissional (FASE 3.5)
type ResourceMetadata struct {
	// Identificação
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Cluster   string `json:"cluster"`
	Type      string `json:"type"` // Pod, Deployment, HPA, Node

	// Versão da aplicação (extraída de labels/annotations)
	Version string `json:"version,omitempty"` // app.kubernetes.io/version ou version label

	// Status e Saúde
	Status       string `json:"status"`        // Running, Pending, Failed, CrashLoopBackOff, etc
	Phase        string `json:"phase"`         // Para Pods: Running, Pending, Succeeded, Failed, Unknown
	Age          string `json:"age"`           // Formato legível: "2d5h", "30m", "45s"
	RestartCount int32  `json:"restart_count"` // Total de restarts

	// Node (para Pods)
	NodeName string `json:"node_name,omitempty"`

	// Imagens dos containers (para Pods/Deployments)
	ContainerImages []string `json:"container_images,omitempty"`

	// Labels importantes
	Labels map[string]string `json:"labels,omitempty"`

	// Recursos (CPU/Memory)
	Resources ResourceSummary `json:"resources"`

	// Réplicas (para Deployments/HPAs)
	Replicas *ReplicaInfo `json:"replicas,omitempty"`

	// Timestamp da coleta
	CollectedAt time.Time `json:"collected_at"`
}

// ResourceSummary - Resumo de recursos (CPU/Memory)
type ResourceSummary struct {
	CPU    ResourceDetail `json:"cpu"`
	Memory ResourceDetail `json:"memory"`
}

// ResourceDetail - Detalhes de um recurso (CPU ou Memory)
type ResourceDetail struct {
	Current string `json:"current"` // Uso atual (formato legível: "250m", "512Mi")
	Request string `json:"request"` // Request configurado
	Limit   string `json:"limit"`   // Limit configurado
}

// ReplicaInfo - Informações de réplicas
type ReplicaInfo struct {
	Current int32 `json:"current"` // Réplicas atuais
	Ready   int32 `json:"ready"`   // Réplicas prontas
	Desired int32 `json:"desired"` // Réplicas desejadas
}
