package collectors

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

// DiagnosticContext representa o contexto completo para análise AI
type DiagnosticContext struct {
	// ResourceType tipo do recurso (Pod, Deployment, HPA, Node)
	ResourceType string `json:"resource_type"`

	// Cluster nome do cluster
	Cluster string `json:"cluster"`

	// Namespace namespace do recurso
	Namespace string `json:"namespace"`

	// ResourceName nome do recurso
	ResourceName string `json:"resource_name"`

	// CollectedAt timestamp da coleta
	CollectedAt time.Time `json:"collected_at"`

	// Pod contexto específico de Pod (se aplicável)
	Pod *PodContext `json:"pod,omitempty"`

	// Deployment contexto específico de Deployment (se aplicável)
	Deployment *DeploymentContext `json:"deployment,omitempty"`

	// HPA contexto específico de HPA (se aplicável)
	HPA *HPAContext `json:"hpa,omitempty"`

	// Node contexto específico de Node (se aplicável)
	Node *NodeContext `json:"node,omitempty"`

	// Events eventos Kubernetes relacionados
	Events []corev1.Event `json:"events,omitempty"`

	// PrometheusAlerts alertas do Prometheus relacionados
	PrometheusAlerts []PrometheusAlert `json:"prometheus_alerts,omitempty"`

	// DescribeOutput output do kubectl describe
	DescribeOutput string `json:"describe_output,omitempty"`
}

// PodContext contexto específico de um Pod
type PodContext struct {
	// Manifest manifesto completo do Pod
	Manifest *corev1.Pod `json:"manifest"`

	// Logs logs dos containers (últimas 500 linhas)
	Logs map[string]string `json:"logs,omitempty"` // container_name → logs

	// PreviousLogs logs de containers anteriores (se CrashLoopBackOff)
	PreviousLogs map[string]string `json:"previous_logs,omitempty"`

	// RelatedDeployment deployment relacionado (se existir)
	RelatedDeployment string `json:"related_deployment,omitempty"`

	// RelatedConfigMaps configmaps referenciados
	RelatedConfigMaps []string `json:"related_configmaps,omitempty"`

	// RelatedSecrets secrets referenciados
	RelatedSecrets []string `json:"related_secrets,omitempty"`

	// NodeInfo informações do node onde o pod está rodando
	NodeInfo *NodeSummary `json:"node_info,omitempty"`
}

// DeploymentContext contexto específico de um Deployment
type DeploymentContext struct {
	// ManifestYAML manifesto completo do Deployment (YAML)
	ManifestYAML string `json:"manifest_yaml"`

	// ReplicaSets replica sets associados
	ReplicaSets []string `json:"replica_sets,omitempty"`

	// Pods pods gerenciados pelo deployment
	Pods []PodSummary `json:"pods,omitempty"`

	// RolloutStatus status do rollout
	RolloutStatus string `json:"rollout_status,omitempty"`

	// RolloutHistory histórico de rollouts
	RolloutHistory []RolloutRevision `json:"rollout_history,omitempty"`
}

// HPAContext contexto específico de um HPA
type HPAContext struct {
	// Manifest manifesto completo do HPA
	Manifest *autoscalingv2.HorizontalPodAutoscaler `json:"manifest"`

	// CurrentMetrics métricas atuais
	CurrentMetrics map[string]interface{} `json:"current_metrics,omitempty"`

	// ScalingHistory histórico de scaling (últimas 20 operações)
	ScalingHistory []ScalingEvent `json:"scaling_history,omitempty"`

	// TargetDeployment deployment alvo
	TargetDeployment string `json:"target_deployment,omitempty"`

	// IsMaxedOut se HPA está no limite máximo de réplicas
	IsMaxedOut bool `json:"is_maxed_out"`
}

// NodeContext contexto específico de um Node
type NodeContext struct {
	// ManifestYAML manifesto completo do Node (YAML)
	ManifestYAML string `json:"manifest_yaml"`

	// Conditions condições do node
	Conditions []corev1.NodeCondition `json:"conditions"`

	// Taints taints aplicados
	Taints []corev1.Taint `json:"taints,omitempty"`

	// Allocatable recursos alocáveis
	Allocatable corev1.ResourceList `json:"allocatable"`

	// Capacity capacidade total
	Capacity corev1.ResourceList `json:"capacity"`

	// PodsRunning pods rodando no node
	PodsRunning []PodSummary `json:"pods_running,omitempty"`

	// ResourceUsage uso atual de recursos
	ResourceUsage *ResourceUsage `json:"resource_usage,omitempty"`
}

// PrometheusAlert representa um alerta do Prometheus
type PrometheusAlert struct {
	Name        string            `json:"name"`
	Severity    string            `json:"severity"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels,omitempty"`
	StartsAt    time.Time         `json:"starts_at"`
}

// NodeSummary sumário de informações de um node
type NodeSummary struct {
	Name       string              `json:"name"`
	Ready      bool                `json:"ready"`
	Conditions []corev1.NodeCondition `json:"conditions,omitempty"`
}

// PodSummary sumário de informações de um pod
type PodSummary struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Phase             string            `json:"phase"`
	RestartCount      int32             `json:"restart_count"`
	ContainerStatuses []ContainerStatus `json:"container_statuses,omitempty"`
}

// ContainerStatus status de um container
type ContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restart_count"`
	State        string `json:"state"` // Running, Waiting, Terminated
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}

// RolloutRevision revisão de um rollout
type RolloutRevision struct {
	Revision  int64  `json:"revision"`
	CreatedAt string `json:"created_at"`
	Image     string `json:"image,omitempty"`
}

// ScalingEvent evento de scaling de um HPA
type ScalingEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	OldReplicas   int32     `json:"old_replicas"`
	NewReplicas   int32     `json:"new_replicas"`
	Reason        string    `json:"reason,omitempty"`
	CurrentMetric string    `json:"current_metric,omitempty"`
}

// ResourceUsage uso de recursos de um node
type ResourceUsage struct {
	CPUUsageMillis    int64   `json:"cpu_usage_millis"`
	CPUUsagePercent   float64 `json:"cpu_usage_percent"`
	MemoryUsageBytes  int64   `json:"memory_usage_bytes"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
}

// ContextRequest requisição para coletar contexto de diagnóstico
type ContextRequest struct {
	ResourceType string
	Cluster      string
	Namespace    string
	ResourceName string

	// IncludeLogs se verdadeiro, coleta logs (pode ser lento)
	IncludeLogs bool

	// IncludeMetrics se verdadeiro, coleta métricas do Prometheus
	IncludeMetrics bool

	// IncludeDescribe se verdadeiro, executa kubectl describe
	IncludeDescribe bool

	// LogTailLines número de linhas de log para coletar (padrão: 500)
	LogTailLines int64
}
