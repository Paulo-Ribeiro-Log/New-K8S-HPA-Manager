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
	CheckEvents      bool `json:"check_events"` // Verificar eventos do Kubernetes (FailedScheduling, etc.)
	CheckHPAs        bool `json:"check_hpas"`   // Verificar HPAs (min=max, métricas, scaling)

	// Timeout geral (segundos) - usado como fallback se timeouts específicos não forem definidos
	Timeout int `json:"timeout"` // Padrão: 30s

	// Timeouts específicos por tipo de check (segundos)
	// Se 0, usa o valor de Timeout como fallback
	TimeoutDeployments int `json:"timeout_deployments,omitempty"` // Padrão: 60s (deployments podem ter muitos pods)
	TimeoutServices    int `json:"timeout_services,omitempty"`    // Padrão: 45s (testes de conectividade)
	TimeoutConfigs     int `json:"timeout_configs,omitempty"`     // Padrão: 30s (validação rápida)
	TimeoutEvents      int `json:"timeout_events,omitempty"`      // Padrão: 30s (consulta de eventos)
	TimeoutHPAs        int `json:"timeout_hpas,omitempty"`        // Padrão: 45s (validação de HPAs)

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
	EventResults      []EventHealth      `json:"event_results"` // Eventos K8s críticos (FailedScheduling, etc.)
	HPAResults        []HPAHealth        `json:"hpa_results"`   // HPAs com problemas de configuração

	// Resumo
	TotalChecks   int          `json:"total_checks"`
	HealthyCount  int          `json:"healthy_count"`
	WarningCount  int          `json:"warning_count"`
	CriticalCount int          `json:"critical_count"`
	OverallStatus HealthStatus `json:"overall_status"`
}

// ProbeIssue representa um problema na configuração de probes
type ProbeIssue struct {
	Container string `json:"container"`
	ProbeType string `json:"probe_type"` // "liveness", "readiness", "startup"
	Issue     string `json:"issue"`
	Severity  string `json:"severity"` // "warning", "critical"
}

// QoSClass representa a classe de QoS do Kubernetes
type QoSClass string

const (
	QoSGuaranteed QoSClass = "Guaranteed" // requests == limits para todos os containers
	QoSBurstable  QoSClass = "Burstable"  // pelo menos um container tem request ou limit
	QoSBestEffort QoSClass = "BestEffort" // nenhum container tem request ou limit
)

// ResourceIssue representa um problema na configuração de recursos
type ResourceIssue struct {
	Container    string `json:"container"`
	ResourceType string `json:"resource_type"` // "cpu", "memory"
	Issue        string `json:"issue"`
	Severity     string `json:"severity"` // "warning", "critical"
}

// ContainerResources representa os recursos configurados de um container
type ContainerResources struct {
	Name string `json:"name"`

	// Requests
	CPURequest    string `json:"cpu_request,omitempty"`    // ex: "100m", "0.5"
	MemoryRequest string `json:"memory_request,omitempty"` // ex: "128Mi", "1Gi"

	// Limits
	CPULimit    string `json:"cpu_limit,omitempty"`
	MemoryLimit string `json:"memory_limit,omitempty"`

	// Flags de configuração
	HasCPURequest    bool `json:"has_cpu_request"`
	HasMemoryRequest bool `json:"has_memory_request"`
	HasCPULimit      bool `json:"has_cpu_limit"`
	HasMemoryLimit   bool `json:"has_memory_limit"`
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

	// Probes (Liveness/Readiness/Startup)
	HasLivenessProbe       bool  `json:"has_liveness_probe"`
	HasReadinessProbe      bool  `json:"has_readiness_probe"`
	HasStartupProbe        bool  `json:"has_startup_probe"`
	LivenessProbeFailures  int32 `json:"liveness_probe_failures"`
	ReadinessProbeFailures int32 `json:"readiness_probe_failures"`

	// Problemas de configuração de probes
	ProbeIssues []ProbeIssue `json:"probe_issues,omitempty"`

	// Recursos - Uso atual
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`    // 0-100
	MemoryUsagePercent float64 `json:"memory_usage_percent"` // 0-100

	// Recursos - Configuração
	QoSClass           QoSClass             `json:"qos_class"`                     // Guaranteed, Burstable, BestEffort
	ContainerResources []ContainerResources `json:"container_resources,omitempty"` // Recursos por container
	ResourceIssues     []ResourceIssue      `json:"resource_issues,omitempty"`     // Problemas de configuração

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

// HPAScalingIssue representa um problema na configuração do HPA
type HPAScalingIssue struct {
	Type        string `json:"type"`        // "config", "metric", "scaling", "target"
	Description string `json:"description"`
	Severity    string `json:"severity"` // "warning", "critical"
}

// HPAMetricConfig configuração de métrica do HPA
type HPAMetricConfig struct {
	Type           string `json:"type"`                      // "Resource", "Pods", "Object", "External"
	Name           string `json:"name"`                      // "cpu", "memory", "custom-metric"
	TargetType     string `json:"target_type"`               // "Utilization", "Value", "AverageValue"
	TargetValue    string `json:"target_value"`              // "80%", "100m", "1000"
	CurrentValue   string `json:"current_value,omitempty"`   // Valor atual se disponível
	IsHealthy      bool   `json:"is_healthy"`                // Métrica funcionando corretamente
	ErrorMessage   string `json:"error_message,omitempty"`   // Mensagem de erro se métrica falhar
}

// HPAScalingEvent evento de scaling recente
type HPAScalingEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`       // "ScaledUp", "ScaledDown", "FailedScaling"
	OldReplicas int32     `json:"old_replicas"`
	NewReplicas int32     `json:"new_replicas"`
	Reason      string    `json:"reason"`
	Message     string    `json:"message"`
}

// HPAHealth saúde de um HorizontalPodAutoscaler
type HPAHealth struct {
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
	Status    HealthStatus `json:"status"`

	// Target Reference
	TargetKind string `json:"target_kind"` // "Deployment", "StatefulSet", etc
	TargetName string `json:"target_name"`
	TargetExists bool  `json:"target_exists"` // Target resource existe?

	// Configuração de Réplicas
	MinReplicas     int32 `json:"min_replicas"`
	MaxReplicas     int32 `json:"max_replicas"`
	CurrentReplicas int32 `json:"current_replicas"`
	DesiredReplicas int32 `json:"desired_replicas"`

	// Flags de Problemas
	IsMinEqualsMax     bool `json:"is_min_equals_max"`      // min == max (não escala)
	IsMaxTooLow        bool `json:"is_max_too_low"`         // max < 3 (pouca flexibilidade)
	IsAtMaxReplicas    bool `json:"is_at_max_replicas"`     // current == max (pode precisar escalar mais)
	IsAtMinReplicas    bool `json:"is_at_min_replicas"`     // current == min
	HasScalingDisabled bool `json:"has_scaling_disabled"`   // Annotations que desabilitam scaling

	// Métricas Configuradas
	Metrics       []HPAMetricConfig `json:"metrics"`
	MetricsCount  int               `json:"metrics_count"`
	MetricsErrors int               `json:"metrics_errors"` // Quantas métricas com erro

	// Comportamento de Scaling
	ScaleUpStabilization   int32 `json:"scale_up_stabilization_seconds,omitempty"`   // Período de estabilização para scale up
	ScaleDownStabilization int32 `json:"scale_down_stabilization_seconds,omitempty"` // Período de estabilização para scale down

	// Eventos Recentes de Scaling
	RecentScalingEvents []HPAScalingEvent `json:"recent_scaling_events,omitempty"`
	LastScaleTime       *time.Time        `json:"last_scale_time,omitempty"`

	// Problemas Detectados
	Issues []HPAScalingIssue `json:"issues,omitempty"`

	// Mensagem e Sugestões
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

// Constantes de timeout padrão
const (
	DefaultTimeoutGeneral     = 30 // segundos
	DefaultTimeoutDeployments = 60 // segundos (deployments podem ter muitos pods)
	DefaultTimeoutServices    = 45 // segundos (testes de conectividade)
	DefaultTimeoutConfigs     = 30 // segundos (validação rápida)
	DefaultTimeoutEvents      = 30 // segundos (consulta de eventos)
	DefaultTimeoutHPAs        = 45 // segundos (validação de HPAs + eventos)
)

// GetTimeoutDeployments retorna o timeout para deployments com fallback
func (r *HealthCheckRequest) GetTimeoutDeployments() int {
	if r.TimeoutDeployments > 0 {
		return r.TimeoutDeployments
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeoutDeployments
}

// GetTimeoutServices retorna o timeout para services com fallback
func (r *HealthCheckRequest) GetTimeoutServices() int {
	if r.TimeoutServices > 0 {
		return r.TimeoutServices
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeoutServices
}

// GetTimeoutConfigs retorna o timeout para configs com fallback
func (r *HealthCheckRequest) GetTimeoutConfigs() int {
	if r.TimeoutConfigs > 0 {
		return r.TimeoutConfigs
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeoutConfigs
}

// GetTimeoutEvents retorna o timeout para events com fallback
func (r *HealthCheckRequest) GetTimeoutEvents() int {
	if r.TimeoutEvents > 0 {
		return r.TimeoutEvents
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeoutEvents
}

// GetTimeoutHPAs retorna o timeout para HPAs com fallback
func (r *HealthCheckRequest) GetTimeoutHPAs() int {
	if r.TimeoutHPAs > 0 {
		return r.TimeoutHPAs
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeoutHPAs
}
