package healthcheck

import "time"

// ResourceType tipos de recursos Kubernetes suportados
type ResourceType string

const (
	ResourceDeployment ResourceType = "Deployment"
	ResourceService    ResourceType = "Service"
	ResourceConfigMap  ResourceType = "ConfigMap"
	ResourceSecret     ResourceType = "Secret"
)

// ServiceType tipos de serviços externos suportados
type ServiceType string

const (
	ServiceMongoDB   ServiceType = "MongoDB"
	ServiceRedis     ServiceType = "Redis"
	ServicePostgres  ServiceType = "PostgreSQL"
	ServiceKafka     ServiceType = "Kafka"
	ServiceEventHub  ServiceType = "EventHub"
	ServiceRabbitMQ  ServiceType = "RabbitMQ"
	ServiceHTTP      ServiceType = "HTTP"
)

// HealthStatus status de saúde de um recurso
type HealthStatus string

const (
	StatusHealthy  HealthStatus = "healthy"
	StatusWarning  HealthStatus = "warning"
	StatusCritical HealthStatus = "critical"
	StatusUnknown  HealthStatus = "unknown"
)

// HealthCheckRequest requisição de health check
type HealthCheckRequest struct {
	// Modo 1: Filtro por ambiente (prod, hlg, all)
	Environment string `json:"environment,omitempty"` // "prod", "hlg", "all" - se preenchido, ignora Clusters

	// Modo 2: Seleção manual de clusters (checkboxes individuais)
	Clusters []string `json:"clusters,omitempty"` // Se preenchido, ignora Environment

	Namespaces []string `json:"namespaces"` // Vazio = todos

	// Opções de verificação
	CheckDeployments bool `json:"check_deployments"`
	CheckServices    bool `json:"check_services"`
	CheckConfigs     bool `json:"check_configs"`

	// Timeout por check (segundos)
	Timeout int `json:"timeout"` // Padrão: 10s

	// Paralelismo (apenas para múltiplos clusters)
	// Se Clusters > 1: mínimo 2 workers, máximo = NumCPU ou total de clusters
	MaxParallel int `json:"max_parallel"` // Padrão: min(NumCPU, len(Clusters))

	// Aplicar filtros de falsos positivos (padrão: true)
	ApplyFilters bool `json:"apply_filters"` // Filtrar ConfigMaps vazios conhecidos, secrets de sistema, etc
}

// HealthCheckResult resultado de health check
type HealthCheckResult struct {
	ID         string    `json:"id"`          // UUID
	Cluster    string    `json:"cluster"`
	Namespace  string    `json:"namespace"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Duration   int64     `json:"duration_ms"` // Milissegundos

	// Resultados
	DeploymentResults []DeploymentHealth `json:"deployment_results"`
	ServiceResults    []ServiceHealth    `json:"service_results"`
	ConfigResults     []ConfigHealth     `json:"config_results"`

	// Resumo
	TotalChecks   int          `json:"total_checks"`
	HealthyCount  int          `json:"healthy_count"`
	WarningCount  int          `json:"warning_count"`
	CriticalCount int          `json:"critical_count"`
	OverallStatus HealthStatus `json:"overall_status"`
}

// DeploymentHealth saúde de um Deployment
type DeploymentHealth struct {
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
	Status    HealthStatus `json:"status"`

	// Detalhes
	ReplicasReady   int32 `json:"replicas_ready"`
	ReplicasDesired int32 `json:"replicas_desired"`
	ContainersCrash int32 `json:"containers_crash"`
	ImagePullErrors int32 `json:"image_pull_errors"`

	// Probes (Liveness/Readiness)
	HasLivenessProbe       bool  `json:"has_liveness_probe"`
	HasReadinessProbe      bool  `json:"has_readiness_probe"`
	LivenessProbeFailures  int32 `json:"liveness_probe_failures"`
	ReadinessProbeFailures int32 `json:"readiness_probe_failures"`

	// Recursos
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`    // 0-100
	MemoryUsagePercent float64 `json:"memory_usage_percent"` // 0-100

	// Mensagem
	Message     string    `json:"message"`
	Suggestions []string  `json:"suggestions"`
	CheckedAt   time.Time `json:"checked_at"`
}

// ServiceHealth saúde de um serviço externo
type ServiceHealth struct {
	Name        string       `json:"name"`
	Namespace   string       `json:"namespace"`
	ServiceType ServiceType  `json:"service_type"`
	Status      HealthStatus `json:"status"`

	// Conectividade
	Reachable       bool   `json:"reachable"`
	LatencyMs       int64  `json:"latency_ms"`
	ConnectionError string `json:"connection_error,omitempty"`

	// Detalhes específicos
	Details map[string]interface{} `json:"details,omitempty"`

	// Fonte (ConfigMap/Secret)
	ConfigSource string `json:"config_source"` // "configmap:my-config" ou "secret:my-secret"

	// Mensagem
	Message     string    `json:"message"`
	Suggestions []string  `json:"suggestions"`
	CheckedAt   time.Time `json:"checked_at"`
}

// ConfigHealth saúde de ConfigMap/Secret
type ConfigHealth struct {
	Name         string       `json:"name"`
	Namespace    string       `json:"namespace"`
	ResourceType ResourceType `json:"resource_type"` // ConfigMap ou Secret
	Status       HealthStatus `json:"status"`

	// Validações
	Exists          bool     `json:"exists"`
	HasRequiredKeys bool     `json:"has_required_keys"`
	MissingKeys     []string `json:"missing_keys,omitempty"`
	InvalidValues   []string `json:"invalid_values,omitempty"`

	// Mensagem
	Message     string    `json:"message"`
	Suggestions []string  `json:"suggestions"`
	CheckedAt   time.Time `json:"checked_at"`
}

// HealthCheckProgress progresso de health check (SSE)
type HealthCheckProgress struct {
	SessionID string       `json:"session_id"`
	Cluster   string       `json:"cluster"`  // Identifica qual cluster está sendo processado
	Phase     string       `json:"phase"`    // "deployments", "services", "configs"
	Message   string       `json:"message"`
	Progress  int          `json:"progress"` // 0-100
	Status    HealthStatus `json:"status"`
	Timestamp time.Time    `json:"timestamp"`
}
