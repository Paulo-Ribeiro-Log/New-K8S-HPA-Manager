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
	ServiceMongoDB  ServiceType = "MongoDB"
	ServiceRedis    ServiceType = "Redis"
	ServicePostgres ServiceType = "PostgreSQL"
	ServiceKafka    ServiceType = "Kafka"
	ServiceEventHub ServiceType = "EventHub"
	ServiceRabbitMQ ServiceType = "RabbitMQ"
	ServiceHTTP     ServiceType = "HTTP"
)

// HealthStatus status de saúde de um recurso
type HealthStatus string

const (
	StatusHealthy  HealthStatus = "healthy"
	StatusWarning  HealthStatus = "warning"
	StatusCritical HealthStatus = "critical"
	StatusUnknown  HealthStatus = "unknown"
)

// Severity níveis de severidade para problemas detectados
type Severity string

const (
	SeverityCritical Severity = "critical" // Requer ação imediata (ex: pod crashloop, PVC lost)
	SeverityHigh     Severity = "high"     // Impacto significativo (ex: sem probes, HPA no limite)
	SeverityMedium   Severity = "medium"   // Deveria ser corrigido (ex: QoS BestEffort, warnings)
	SeverityLow      Severity = "low"      // Melhoria recomendada (ex: sem startupProbe)
	SeverityInfo     Severity = "info"     // Informativo apenas (ex: scaling events, estatísticas)
)

// SeverityWeight retorna o peso numérico da severidade para ordenação (maior = mais grave)
func (s Severity) Weight() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// IsMoreSevereThan compara se esta severidade é mais grave que outra
func (s Severity) IsMoreSevereThan(other Severity) bool {
	return s.Weight() > other.Weight()
}

// SeverityFromString converte string para Severity com fallback
func SeverityFromString(s string) Severity {
	switch s {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	case "info":
		return SeverityInfo
	case "warning": // backward compatibility
		return SeverityMedium
	default:
		return SeverityInfo
	}
}

// ToHealthStatus converte Severity para HealthStatus (para compatibilidade)
func (s Severity) ToHealthStatus() HealthStatus {
	switch s {
	case SeverityCritical, SeverityHigh:
		return StatusCritical
	case SeverityMedium:
		return StatusWarning
	default:
		return StatusHealthy
	}
}

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
	CheckEvents      bool `json:"check_events"`    // Verificar eventos do Kubernetes (FailedScheduling, etc.)
	CheckHPAs        bool `json:"check_hpas"`      // Verificar HPAs (min=max, métricas, scaling)
	CheckPVCs        bool `json:"check_pvcs"`      // Verificar PersistentVolumeClaims (status, storage class)
	CheckDynatrace   bool `json:"check_dynatrace"` // Verificar problems OPEN no Dynatrace

	// Credenciais Dynatrace (preenchidas pelo handler a partir dos tokens do usuário)
	AIEmail            string `json:"ai_email,omitempty"`
	DynatraceURL       string `json:"-"` // não exposto no JSON de request (preenchido internamente)
	DynatraceToken     string `json:"-"` // não exposto no JSON de request (preenchido internamente)
	DynatraceTagFilter string `json:"-"` // tag para filtrar problems (ex: "SRE-LOGISTICA")

	// Timeout geral (segundos) - usado como fallback se timeouts específicos não forem definidos
	Timeout int `json:"timeout"` // Padrão: 30s

	TimeoutDynatrace int `json:"timeout_dynatrace,omitempty"` // Padrão: 20s

	// Timeouts específicos por tipo de check (segundos)
	// Se 0, usa o valor de Timeout como fallback
	TimeoutDeployments int `json:"timeout_deployments,omitempty"` // Padrão: 60s (deployments podem ter muitos pods)
	TimeoutServices    int `json:"timeout_services,omitempty"`    // Padrão: 45s (testes de conectividade)
	TimeoutConfigs     int `json:"timeout_configs,omitempty"`     // Padrão: 30s (validação rápida)
	TimeoutEvents      int `json:"timeout_events,omitempty"`      // Padrão: 30s (consulta de eventos)
	TimeoutHPAs        int `json:"timeout_hpas,omitempty"`        // Padrão: 45s (validação de HPAs)
	TimeoutPVCs        int `json:"timeout_pvcs,omitempty"`        // Padrão: 30s (validação de PVCs)

	// Paralelismo (apenas para múltiplos clusters)
	// Se Clusters > 1: mínimo 2 workers, máximo = NumCPU ou total de clusters
	MaxParallel int `json:"max_parallel"` // Padrão: min(NumCPU, len(Clusters))

	// Aplicar filtros de falsos positivos (padrão: true)
	ApplyFilters bool `json:"apply_filters"` // Filtrar ConfigMaps vazios conhecidos, secrets de sistema, etc
}

// HealthCheckResult resultado de health check
type HealthCheckResult struct {
	ID         string    `json:"id"` // UUID
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
	PVCResults        []PVCHealth        `json:"pvc_results"`   // PVCs com problemas de storage

	// Dynatrace problems correlacionados (opcional)
	DynatraceResults []DynatraceHealth `json:"dynatrace_results"`

	// Resumo
	TotalChecks   int          `json:"total_checks"`
	HealthyCount  int          `json:"healthy_count"`
	WarningCount  int          `json:"warning_count"`  // Deprecated: use severity counts
	CriticalCount int          `json:"critical_count"` // Deprecated: use severity counts
	OverallStatus HealthStatus `json:"overall_status"`

	// Contadores por severidade (novo sistema)
	SeverityCounts SeverityCounts `json:"severity_counts"`
}

// DynatraceHealth problema Dynatrace correlacionado com recursos K8s do cluster
type DynatraceHealth struct {
	ProblemID        string       `json:"problem_id"`
	DisplayID        string       `json:"display_id"`
	Title            string       `json:"title"`
	DTSeverity       string       `json:"dt_severity"` // AVAILABILITY, ERROR, PERFORMANCE, etc.
	ImpactLevel      string       `json:"impact_level"`
	Status           HealthStatus `json:"status"`
	Severity         Severity     `json:"severity"`
	StartTime        time.Time    `json:"start_time"`
	K8sNamespaces    []string     `json:"k8s_namespaces,omitempty"`
	K8sWorkloads     []string     `json:"k8s_workloads,omitempty"` // "namespace/workload"
	AffectedEntities []string     `json:"affected_entities,omitempty"`
	Message          string       `json:"message"`
	Suggestions      []string     `json:"suggestions"`
	CheckedAt        time.Time    `json:"checked_at"`
	// Campos enriquecidos via DTLabels (OneAgent tags) — para correlação com GitHub releases e ownership
	AppVersions  map[string]string `json:"app_versions,omitempty"` // appName → version (ex: "vv-categoria-frontend" → "147-206-7-1")
	GitHubRepos  []string          `json:"github_repos,omitempty"` // IDs dos repos afetados (correlação com GitHub releases)
	Squads       []string          `json:"squads,omitempty"`       // times donos das apps afetadas
	Journeys     []string          `json:"journeys,omitempty"`     // jornadas/domínios
	Environments []string          `json:"environments,omitempty"` // prd/hlg/dev
	// Campos enriquecidos via GetProblemContext (Davis AI) — top 5 problems por severidade
	Evidence       []string           `json:"evidence,omitempty"`        // Evidências Davis AI, prefixadas com "[Root Cause]" quando aplicável
	RecentEvents   []string           `json:"recent_events,omitempty"`   // Top 3 eventos recentes: "TIPO: título"
	MetricsSummary map[string]float64 `json:"metrics_summary,omitempty"` // ex: {"error_rate": 12.5, "response_p90_ms": 2300}
	ContextFetched bool               `json:"context_fetched"`           // true se GetProblemContext foi chamado com sucesso
}

// SeverityCounts contadores por nível de severidade
type SeverityCounts struct {
	Critical int `json:"critical"` // Requer ação imediata
	High     int `json:"high"`     // Impacto significativo
	Medium   int `json:"medium"`   // Deveria ser corrigido
	Low      int `json:"low"`      // Melhoria recomendada
	Info     int `json:"info"`     // Informativo
}

// Total retorna o total de issues (excluindo info)
func (sc SeverityCounts) Total() int {
	return sc.Critical + sc.High + sc.Medium + sc.Low
}

// TotalWithInfo retorna o total de issues incluindo info
func (sc SeverityCounts) TotalWithInfo() int {
	return sc.Critical + sc.High + sc.Medium + sc.Low + sc.Info
}

// Increment incrementa o contador para a severidade especificada
func (sc *SeverityCounts) Increment(severity Severity) {
	switch severity {
	case SeverityCritical:
		sc.Critical++
	case SeverityHigh:
		sc.High++
	case SeverityMedium:
		sc.Medium++
	case SeverityLow:
		sc.Low++
	case SeverityInfo:
		sc.Info++
	}
}

// ProbeIssue representa um problema na configuração de probes
type ProbeIssue struct {
	Container string   `json:"container"`
	ProbeType string   `json:"probe_type"` // "liveness", "readiness", "startup"
	Issue     string   `json:"issue"`
	Severity  Severity `json:"severity"` // critical, high, medium, low, info
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
	Container    string   `json:"container"`
	ResourceType string   `json:"resource_type"` // "cpu", "memory"
	Issue        string   `json:"issue"`
	Severity     Severity `json:"severity"` // critical, high, medium, low, info
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
	Type        string   `json:"type"` // "config", "metric", "scaling", "target"
	Description string   `json:"description"`
	Severity    Severity `json:"severity"` // critical, high, medium, low, info
}

// HPAMetricConfig configuração de métrica do HPA
type HPAMetricConfig struct {
	Type         string `json:"type"`                    // "Resource", "Pods", "Object", "External"
	Name         string `json:"name"`                    // "cpu", "memory", "custom-metric"
	TargetType   string `json:"target_type"`             // "Utilization", "Value", "AverageValue"
	TargetValue  string `json:"target_value"`            // "80%", "100m", "1000"
	CurrentValue string `json:"current_value,omitempty"` // Valor atual se disponível
	IsHealthy    bool   `json:"is_healthy"`              // Métrica funcionando corretamente
	ErrorMessage string `json:"error_message,omitempty"` // Mensagem de erro se métrica falhar
}

// HPAScalingEvent evento de scaling recente
type HPAScalingEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"` // "ScaledUp", "ScaledDown", "FailedScaling"
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
	TargetKind   string `json:"target_kind"` // "Deployment", "StatefulSet", etc
	TargetName   string `json:"target_name"`
	TargetExists bool   `json:"target_exists"` // Target resource existe?

	// Configuração de Réplicas
	MinReplicas     int32 `json:"min_replicas"`
	MaxReplicas     int32 `json:"max_replicas"`
	CurrentReplicas int32 `json:"current_replicas"`
	DesiredReplicas int32 `json:"desired_replicas"`

	// Flags de Problemas
	IsMinEqualsMax     bool `json:"is_min_equals_max"`    // min == max (não escala)
	IsMaxTooLow        bool `json:"is_max_too_low"`       // max < 3 (pouca flexibilidade)
	IsAtMaxReplicas    bool `json:"is_at_max_replicas"`   // current == max (pode precisar escalar mais)
	IsAtMinReplicas    bool `json:"is_at_min_replicas"`   // current == min
	HasScalingDisabled bool `json:"has_scaling_disabled"` // Annotations que desabilitam scaling

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

// PVIssue representa um problema na configuração do PV/PVC
type PVIssue struct {
	Type        string   `json:"type"` // "status", "config", "volume"
	Description string   `json:"description"`
	Severity    Severity `json:"severity"` // critical, high, medium, low, info
}

// PVCHealth saúde de um PersistentVolumeClaim
type PVCHealth struct {
	Name      string       `json:"name"`
	Namespace string       `json:"namespace"`
	Status    HealthStatus `json:"status"`

	// Status do PVC
	Phase     string `json:"phase"`      // Bound, Pending, Lost
	IsBound   bool   `json:"is_bound"`   // Está vinculado a um PV?
	IsPending bool   `json:"is_pending"` // Está aguardando provisionamento?

	// Volume vinculado
	VolumeName   string `json:"volume_name,omitempty"`   // Nome do PV vinculado
	VolumeExists bool   `json:"volume_exists,omitempty"` // PV existe?

	// Storage Class
	StorageClassName   string `json:"storage_class_name,omitempty"`
	StorageClassExists bool   `json:"storage_class_exists,omitempty"`

	// Capacidade
	RequestedStorage string `json:"requested_storage,omitempty"` // Ex: "10Gi"
	ActualStorage    string `json:"actual_storage,omitempty"`    // Capacidade real do PV
	RequestedBytes   int64  `json:"requested_bytes,omitempty"`   // Bytes solicitados
	ActualBytes      int64  `json:"actual_bytes,omitempty"`      // Bytes reais

	// Utilização (se disponível)
	UsagePercent float64 `json:"usage_percent,omitempty"` // 0-100

	// Configuração
	AccessModes   []string `json:"access_modes,omitempty"`   // ReadWriteOnce, ReadOnlyMany, ReadWriteMany
	VolumeMode    string   `json:"volume_mode,omitempty"`    // Filesystem, Block
	ReclaimPolicy string   `json:"reclaim_policy,omitempty"` // Retain, Delete, Recycle

	// Problemas Detectados
	Issues []PVIssue `json:"issues,omitempty"`

	// Mensagem e Sugestões
	Message     string    `json:"message"`
	Suggestions []string  `json:"suggestions"`
	CheckedAt   time.Time `json:"checked_at"`
}

// HealthCheckProgress progresso de health check (SSE)
type HealthCheckProgress struct {
	SessionID string       `json:"session_id"`
	Cluster   string       `json:"cluster"` // Identifica qual cluster está sendo processado
	Phase     string       `json:"phase"`   // "deployments", "services", "configs"
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
	DefaultTimeoutPVCs        = 30 // segundos (validação de PVCs)
	DefaultTimeoutDynatrace   = 45 // segundos (inclui GetProblemContext Top5 + métricas críticas)
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

// GetTimeoutPVCs retorna o timeout para PVCs com fallback
func (r *HealthCheckRequest) GetTimeoutPVCs() int {
	if r.TimeoutPVCs > 0 {
		return r.TimeoutPVCs
	}
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeoutPVCs
}

// GetTimeoutDynatrace retorna o timeout para Dynatrace com fallback
func (r *HealthCheckRequest) GetTimeoutDynatrace() int {
	if r.TimeoutDynatrace > 0 {
		return r.TimeoutDynatrace
	}
	return DefaultTimeoutDynatrace
}
