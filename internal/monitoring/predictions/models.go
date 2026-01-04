package predictions

import "time"

// PredictionRequest representa uma solicitação de análise preditiva
type PredictionRequest struct {
	Cluster    string `json:"cluster" binding:"required"`
	Namespace  string `json:"namespace" binding:"required"`
	Deployment string `json:"deployment" binding:"required"`
	UserEmail  string `json:"user_email,omitempty"`
}

// PredictionResult é o resultado completo da análise
type PredictionResult struct {
	// Metadados
	RequestID  string    `json:"request_id"`
	Cluster    string    `json:"cluster"`
	Namespace  string    `json:"namespace"`
	Deployment string    `json:"deployment"`
	AnalyzedAt time.Time `json:"analyzed_at"`
	DurationMs int64     `json:"duration_ms"`

	// Dados coletados
	RawMetrics DeploymentMetrics `json:"raw_metrics"`

	// Análises
	HealthScore       HealthScore         `json:"health_score"`
	Predictions       PredictionsAnalysis `json:"predictions"`
	RootCauseAnalysis RootCauseAnalysis   `json:"root_cause_analysis"`
	ImpactAnalysis    ImpactAnalysis      `json:"impact_analysis"`
	ExecutiveSummary  ExecutiveSummary    `json:"executive_summary"`
	Recommendations   []Recommendation    `json:"recommendations"`
}

// DeploymentMetrics contém todas as métricas coletadas
type DeploymentMetrics struct {
	// Identificação
	Deployment string `json:"deployment"`
	Namespace  string `json:"namespace"`
	Cluster    string `json:"cluster"`

	// Configuração
	DesiredReplicas   int32            `json:"desired_replicas"`
	CurrentReplicas   int32            `json:"current_replicas"`
	AvailableReplicas int32            `json:"available_replicas"`
	ReadyReplicas     int32            `json:"ready_replicas"`
	Resources         ResourceRequests `json:"resources"`

	// Métricas temporais (current, 3d, 7d, 10d, 14d ago)
	Current  MetricSnapshot `json:"current"`
	Day3Ago  MetricSnapshot `json:"day_3_ago"`
	Day7Ago  MetricSnapshot `json:"day_7_ago"`
	Day10Ago MetricSnapshot `json:"day_10_ago"`
	Day14Ago MetricSnapshot `json:"day_14_ago"`

	// Tendências calculadas
	Trends TrendAnalysis `json:"trends"`

	// Contexto de infraestrutura
	NodeMetrics      NodeMetrics      `json:"node_metrics"`
	CompetingApps    []CompetingApp   `json:"competing_apps"`
	CapacityForecast CapacityForecast `json:"capacity_forecast"`
}

// ResourceRequests define requests e limits do deployment
type ResourceRequests struct {
	CPURequest    string `json:"cpu_request"`
	CPULimit      string `json:"cpu_limit"`
	MemoryRequest string `json:"memory_request"`
	MemoryLimit   string `json:"memory_limit"`
}

// MetricSnapshot é uma foto das métricas em um momento
type MetricSnapshot struct {
	Timestamp      time.Time      `json:"timestamp"`
	CPUUsageAvg    float64        `json:"cpu_usage_avg"` // cores
	CPUUsageP95    float64        `json:"cpu_usage_p95"`
	MemoryUsageAvg float64        `json:"memory_usage_avg"` // bytes
	MemoryUsageP95 float64        `json:"memory_usage_p95"`
	NetworkRxAvg   float64        `json:"network_rx_avg"` // bytes/s
	NetworkTxAvg   float64        `json:"network_tx_avg"`
	RestartCount   int            `json:"restart_count"`
	ErrorRate      float64        `json:"error_rate"` // %
	Latency        LatencyMetrics `json:"latency"`
}

// LatencyMetrics contém métricas de latência
type LatencyMetrics struct {
	P50 float64 `json:"p50"` // ms
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// TrendAnalysis contém análise de tendências
type TrendAnalysis struct {
	CPUTrend       TrendDirection `json:"cpu_trend"`
	MemoryTrend    TrendDirection `json:"memory_trend"`
	ErrorRateTrend TrendDirection `json:"error_rate_trend"`
	LatencyTrend   TrendDirection `json:"latency_trend"`

	// Mudanças percentuais
	CPUChange3d       float64 `json:"cpu_change_3d_percent"`
	CPUChange7d       float64 `json:"cpu_change_7d_percent"`
	CPUChange10d      float64 `json:"cpu_change_10d_percent"`
	CPUChange14d      float64 `json:"cpu_change_14d_percent"`
	MemoryChange7d    float64 `json:"memory_change_7d_percent"`
	MemoryChange14d   float64 `json:"memory_change_14d_percent"`
	ErrorRateChange7d float64 `json:"error_rate_change_7d_percent"`
	LatencyChange7d   float64 `json:"latency_change_7d_percent"`
}

// TrendDirection indica direção da tendência
type TrendDirection string

const (
	TrendUp       TrendDirection = "increasing"
	TrendDown     TrendDirection = "decreasing"
	TrendStable   TrendDirection = "stable"
	TrendVolatile TrendDirection = "volatile"
)

// NodeMetrics contém informações dos nodes
type NodeMetrics struct {
	NodesUsed           int                 `json:"nodes_used"`
	TotalNodesInCluster int                 `json:"total_nodes_in_cluster"`
	NodeDistribution    map[string]NodeInfo `json:"node_distribution"`
	TotalCapacity       ClusterCapacity     `json:"total_capacity"`
	VMSizing            VMSizingInfo        `json:"vm_sizing"`
	BinPackingAnalysis  BinPackingAnalysis  `json:"bin_packing_analysis"`
}

// NodeInfo contém informações de um node específico
type NodeInfo struct {
	NodeName       string  `json:"node_name"`
	PodCount       int     `json:"pod_count"`
	CPUAvailable   string  `json:"cpu_available"`
	CPUCapacity    string  `json:"cpu_capacity"`
	CPUUsage       float64 `json:"cpu_usage_percent"`
	MemAvailable   string  `json:"mem_available"`
	MemCapacity    string  `json:"mem_capacity"`
	MemUsage       float64 `json:"mem_usage_percent"`
	CanFitReplicas int     `json:"can_fit_replicas"`
	InstanceType   string  `json:"instance_type,omitempty"`
}

// ClusterCapacity representa capacidade total do cluster
type ClusterCapacity struct {
	CPUTotal       float64 `json:"cpu_total_cores"`
	CPUAllocated   float64 `json:"cpu_allocated_cores"`
	CPUUtilization float64 `json:"cpu_utilization_percent"`
	MemTotal       float64 `json:"mem_total_gb"`
	MemAllocated   float64 `json:"mem_allocated_gb"`
	MemUtilization float64 `json:"mem_utilization_percent"`
}

// VMSizingInfo contém informações sobre sizing de VMs
type VMSizingInfo struct {
	PredominantInstanceType string `json:"predominant_instance_type"`
	CPUPerVM                int    `json:"cpu_per_vm_cores"`
	MemoryPerVM             int    `json:"memory_per_vm_gb"`
	MaxPodsPerNode          int    `json:"max_pods_per_node"`

	// Node Pool Scaling Info
	MinNodes     int `json:"min_nodes"`
	MaxNodes     int `json:"max_nodes"`
	CurrentNodes int `json:"current_nodes"`

	RecommendedInstanceType string `json:"recommended_instance_type,omitempty"`
	RecommendationReason    string `json:"recommendation_reason,omitempty"`
}

// BinPackingAnalysis contém análise de empacotamento
type BinPackingAnalysis struct {
	CurrentEfficiency   float64 `json:"current_efficiency_percent"`
	WastedResources     string  `json:"wasted_resources"`
	FragmentationLevel  string  `json:"fragmentation_level"` // low, medium, high
	OptimizedEfficiency float64 `json:"optimized_efficiency_percent"`
	RebalancingNeeded   bool    `json:"rebalancing_needed"`
}

// CompetingApp representa aplicações competindo por recursos
type CompetingApp struct {
	Name        string  `json:"name"`
	Namespace   string  `json:"namespace"`
	Replicas    int     `json:"replicas"`
	CPUUsage    float64 `json:"cpu_usage_cores"`
	MemoryUsage float64 `json:"memory_usage_gb"`
	ImpactLevel string  `json:"impact_level"` // low, medium, high
}

// CapacityForecast previsão de capacidade
type CapacityForecast struct {
	CanScale              bool               `json:"can_scale"`
	MaxAdditionalReplicas int                `json:"max_additional_replicas"`
	LimitingFactor        string             `json:"limiting_factor"`
	NodeAnalysis          NodeAnalysisDetail `json:"node_analysis"`
	ScalingTimeline       ScalingTimeline    `json:"scaling_timeline"`
	NewNodesNeeded        int                `json:"new_nodes_needed"`
	NewNodesReason        string             `json:"new_nodes_reason,omitempty"`

	// Análise de Capacidade Detalhada
	GrowthAnalysis GrowthCapacityAnalysis `json:"growth_analysis"`
}

// NodeAnalysisDetail análise detalhada por node
type NodeAnalysisDetail struct {
	MostSaturatedNode    string `json:"most_saturated_node"`
	BestCandidateNode    string `json:"best_candidate_node"`
	TotalCapacityPerNode int    `json:"total_capacity_per_node"`
}

// ScalingTimeline timeline de saturação
type ScalingTimeline struct {
	Reach80PercentDate    time.Time `json:"reach_80_percent_date"`
	Reach100PercentDate   time.Time `json:"reach_100_percent_date"`
	DaysUntil80Percent    int       `json:"days_until_80_percent"`
	DaysUntil100Percent   int       `json:"days_until_100_percent"`
	RecommendedActionDate time.Time `json:"recommended_action_date"`
}

// HealthScore pontuação de saúde do deployment
type HealthScore struct {
	Overall     int            `json:"overall"`  // 0-100
	Category    string         `json:"category"` // healthy, warning, critical
	Breakdown   ScoreBreakdown `json:"breakdown"`
	LastUpdated time.Time      `json:"last_updated"`
}

// ScoreBreakdown detalhamento da pontuação
type ScoreBreakdown struct {
	Availability int `json:"availability"` // 0-100
	Performance  int `json:"performance"`
	Stability    int `json:"stability"`
	Efficiency   int `json:"efficiency"`
}

// PredictionsAnalysis contém previsões temporais
type PredictionsAnalysis struct {
	ShortTerm  []Prediction `json:"short_term"`  // próximas 4h
	MediumTerm []Prediction `json:"medium_term"` // próximas 24h
	LongTerm   []Prediction `json:"long_term"`   // próximos 7 dias
}

// Prediction representa uma previsão individual
type Prediction struct {
	Timeframe   string    `json:"timeframe"` // "4h", "24h", "7d"
	Timestamp   time.Time `json:"timestamp"`
	Event       string    `json:"event"`       // descrição do evento previsto
	Probability float64   `json:"probability"` // 0.0 a 1.0
	Severity    string    `json:"severity"`    // low, medium, high, critical
	Impact      string    `json:"impact"`      // descrição do impacto
	Indicators  []string  `json:"indicators"`  // sinais que levam a esta previsão
}

// RootCauseAnalysis análise de causa raiz
type RootCauseAnalysis struct {
	IdentifiedCauses    []RootCause `json:"identified_causes"`
	PrimaryFactor       string      `json:"primary_factor"`
	ContributingFactors []string    `json:"contributing_factors"`
}

// RootCause representa uma causa identificada
type RootCause struct {
	Cause       string   `json:"cause"`
	Evidence    []string `json:"evidence"`
	Certainty   float64  `json:"certainty"`   // 0.0 a 1.0
	Category    string   `json:"category"`    // resource, config, external, code
	Remediation string   `json:"remediation"` // ação corretiva sugerida
}

// ImpactAnalysis análise de impacto
type ImpactAnalysis struct {
	IfNoAction                ImpactScenario `json:"if_no_action"`
	IfOptimizationsApplied    ImpactScenario `json:"if_optimizations_applied"`
	RecommendedActionPriority string         `json:"recommended_action_priority"` // immediate, urgent, moderate, low
	TimelineToAction          string         `json:"timeline_to_action"`
}

// ImpactScenario cenário de impacto
type ImpactScenario struct {
	UserImpact           string   `json:"user_impact"`
	InfrastructureImpact string   `json:"infrastructure_impact"`
	TimelineDescription  string   `json:"timeline_description"`
	Risks                []string `json:"risks"`
	Benefits             []string `json:"benefits,omitempty"`
}

// ExecutiveSummary resumo executivo não-técnico
type ExecutiveSummary struct {
	CurrentState    string   `json:"current_state"`
	KeyFindings     []string `json:"key_findings"`
	RiskLevel       string   `json:"risk_level"` // low, medium, high, critical
	ActionRequired  bool     `json:"action_required"`
	ExpectedOutcome string   `json:"expected_outcome"`
	BusinessImpact  string   `json:"business_impact"`
}

// Recommendation recomendação de ação
type Recommendation struct {
	Priority               int                    `json:"priority"` // 1 (mais alta) a 5
	Title                  string                 `json:"title"`
	Description            string                 `json:"description"`
	Category               string                 `json:"category"` // scaling, resources, config, monitoring
	Actions                []string               `json:"actions"`  // passos específicos
	ExpectedImpact         string                 `json:"expected_impact"`
	ImplementationEstimate ImplementationEstimate `json:"implementation_estimate"`
}

// ImplementationEstimate estimativa de implementação
type ImplementationEstimate struct {
	TimeRequired           string  `json:"time_required"` // "5 minutes", "1 hour", "1 day"
	Complexity             string  `json:"complexity"`    // low, medium, high
	RiskLevel              string  `json:"risk_level"`    // low, medium, high
	RequiresDowntime       bool    `json:"requires_downtime"`
	ResourceEfficiencyGain float64 `json:"resource_efficiency_gain_percent"`
}

// GrowthCapacityAnalysis análise detalhada de capacidade para crescimento
type GrowthCapacityAnalysis struct {
	// Aplicação em Análise
	TargetApp ApplicationCapacity `json:"target_app"`

	// Aplicações Concorrentes
	CompetingApps       []ApplicationCapacity `json:"competing_apps"`
	TotalCompetingUsage ResourceUsage         `json:"total_competing_usage"`

	// Capacidade do Cluster
	CurrentCapacity CapacityInfo `json:"current_capacity"`
	MaxCapacity     CapacityInfo `json:"max_capacity"` // Se escalar até max nodes

	// Análise de Crescimento
	AvailableForGrowth        ResourceUsage `json:"available_for_growth"`
	MaxReplicasCurrentNodes   int           `json:"max_replicas_current_nodes"`
	MaxReplicasWithMaxNodes   int           `json:"max_replicas_with_max_nodes"`
	ReplicasIfRemoveCompeting int           `json:"replicas_if_remove_competing"`

	// Recomendações
	RecommendedMaxReplicas int    `json:"recommended_max_replicas"`
	GrowthRecommendation   string `json:"growth_recommendation"`
	BottleneckResource     string `json:"bottleneck_resource"` // cpu, memory, nodes
}

// ApplicationCapacity representa capacidade de uma aplicação
type ApplicationCapacity struct {
	Name      string        `json:"name"`
	Namespace string        `json:"namespace"`
	Replicas  int           `json:"replicas"`
	Usage     ResourceUsage `json:"usage"`
}

// ResourceUsage representa uso de recursos
type ResourceUsage struct {
	CPUCores float64 `json:"cpu_cores"`
	MemoryGB float64 `json:"memory_gb"`
}

// CapacityInfo representa capacidade total ou disponível
type CapacityInfo struct {
	Nodes     int           `json:"nodes"`
	Resources ResourceUsage `json:"resources"`
}
