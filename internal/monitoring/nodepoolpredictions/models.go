package nodepoolpredictions

import "time"

// ---------------------------------------------------------------------------
// Request / Result
// ---------------------------------------------------------------------------

// NodePoolPredictionRequest representa uma solicitação de análise preditiva de node pool
type NodePoolPredictionRequest struct {
	Cluster      string `json:"cluster" binding:"required"`
	NodePoolName string `json:"nodepool_name" binding:"required"`
	UserEmail    string `json:"user_email,omitempty"`

	// Campos internos — preenchidos pelo handler a partir do clusters-config.json.
	// Não são obrigatórios no JSON do frontend; o handler injeta antes de repassar ao collector.
	ResourceGroup string `json:"resource_group,omitempty"` // RG real do cluster AKS (não o MC_* de infra)
	Subscription  string `json:"subscription,omitempty"`   // Subscription ID do cluster AKS
	AzureCluster  string `json:"azure_cluster,omitempty"`  // Nome do cluster sem sufixo -admin
}

// NodePoolPredictionResult é o resultado completo da análise
type NodePoolPredictionResult struct {
	// Metadados
	RequestID    string    `json:"request_id"`
	Cluster      string    `json:"cluster"`
	NodePoolName string    `json:"nodepool_name"`
	AnalyzedAt   time.Time `json:"analyzed_at"`
	DurationMs   int64     `json:"duration_ms"`

	// Dados coletados
	RawMetrics NodePoolMetrics `json:"raw_metrics"`

	// Resumo de ação rápida (topo do modal)
	ActionSummary NodePoolActionSummary `json:"action_summary"`

	// Análises
	HealthScore       NodePoolHealthScore         `json:"health_score"`
	Trends            NodePoolTrends              `json:"trends"`
	Predictions       NodePoolPredictionsAnalysis `json:"predictions"`
	RootCauseAnalysis NodePoolRootCauseAnalysis   `json:"root_cause_analysis"`
	ExecutiveSummary  NodePoolExecutiveSummary    `json:"executive_summary"`
	Recommendations   []NodePoolRecommendation    `json:"recommendations"`

	// Análise de custo (baseada em VM SKU real — mais precisa que deployment)
	CostAnalysis *NodePoolCostAnalysis `json:"cost_analysis,omitempty"`

	// Timeline de saturação determinística (calculada via regressão linear sobre D-0/D-3/D-7/D-14)
	SaturationTimeline PoolSaturationTimeline `json:"saturation_timeline"`

	// Delta em relação à análise anterior do mesmo pool (nil se for a primeira análise)
	Delta *NodePoolAnalysisDelta `json:"delta,omitempty"`
}

// NodePoolAnalysisDelta compara a análise atual com a anterior do mesmo pool.
// Todos os campos de delta são: positivo = métrica aumentou, negativo = diminuiu.
// "IsGood" depende da métrica: para recursos (CPU/Mem/etc), aumento = risco maior.
type NodePoolAnalysisDelta struct {
	// Referência à análise anterior
	PreviousID         string    `json:"previous_id"`
	PreviousAnalyzedAt time.Time `json:"previous_analyzed_at"`
	DaysSince          float64   `json:"days_since"` // horas / 24

	// Deltas de métricas (em pontos percentuais, exceto HealthScore em pontos absolutos)
	HealthScoreDelta   int     `json:"health_score_delta"`    // positivo = melhorou
	CPUDeltaPP         float64 `json:"cpu_delta_pp"`          // positivo = mais pressão (pior)
	MemDeltaPP         float64 `json:"mem_delta_pp"`          // positivo = mais pressão (pior)
	PodsDelta          float64 `json:"pods_delta"`            // positivo = mais pods/node
	ConntrackDeltaPP   float64 `json:"conntrack_delta_pp"`    // positivo = mais pressão (pior)
	BinPackingDeltaPP  float64 `json:"bin_packing_delta_pp"`  // positivo = menos desperdício (melhor)

	// Listas de métricas que melhoraram ou pioraram (para exibição rápida)
	Improving []string `json:"improving"` // ex: ["cpu", "mem"]
	Degrading []string `json:"degrading"` // ex: ["conntrack", "pods"]

	// Resumo legível
	Summary string `json:"summary"` // ex: "Desde análise anterior (há 2d): conntrack +15.3pp, CPU -8.2pp"
}

// ---------------------------------------------------------------------------
// Métricas coletadas
// ---------------------------------------------------------------------------

// NodePoolMetrics contém todos os dados coletados do node pool
type NodePoolMetrics struct {
	// Identificação
	NodePoolName string `json:"nodepool_name"`
	Cluster      string `json:"cluster"`       // Nome do contexto kubeconfig (pode ter -admin)
	AzureCluster string `json:"azure_cluster"` // Nome real do cluster AKS no Azure (sem -admin)
	VMSize       string `json:"vm_size"`

	// Configuração do pool (via Azure API)
	CurrentNodes int  `json:"current_nodes"`
	MinNodes     int  `json:"min_nodes"`
	MaxNodes     int  `json:"max_nodes"`
	AutoscalerEnabled bool `json:"autoscaler_enabled"`

	// Estado atual de cada node do pool
	NodesSnapshot []NodePoolNodeSnapshot `json:"nodes_snapshot"`

	// Trends normalizadas por node (D-0, D-3, D-7, D-14)
	// Usando médias por node para comparabilidade quando pool escala/descala
	CPUTrendPerNode    []TrendSnapshot `json:"cpu_trend_per_node"`
	MemTrendPerNode    []TrendSnapshot `json:"mem_trend_per_node"`
	PodsTrendPerNode   []TrendSnapshot `json:"pods_trend_per_node"`
	NetworkTrendPerNode []TrendSnapshot `json:"network_trend_per_node"`

	// conntrack — primário: filtrado ao pool; secundário: cluster-wide
	ConntrackPerNode []ConntrackNodeInfo    `json:"conntrack_per_node"`
	ConntrackPool    ConntrackPoolAnalysis  `json:"conntrack_pool"`
	ConntrackCluster ConntrackClusterContext `json:"conntrack_cluster"`

	// Eventos do Cluster Autoscaler (últimos 7 dias)
	AutoscalerEvents []AutoscalerEvent `json:"autoscaler_events"`

	// Nodes com pressões ativas (conditions)
	NodesWithPressure []NodePressureInfo `json:"nodes_with_pressure"`

	// Análise de fragmentação (bin-packing)
	BinPacking BinPackingAnalysis `json:"bin_packing"`

	// Correlação com HPAs do pool (quais HPAs têm pods neste pool)
	HPACorrelation []HPAPoolCorrelation `json:"hpa_correlation,omitempty"`

	// Crescimento do disco efêmero por node (silent killer)
	DiskGrowth DiskGrowthAnalysis `json:"disk_growth"`

	// Capacidade de crescimento
	CapacityForecast NodePoolCapacityForecast `json:"capacity_forecast"`

	// Fonte de dados (para indicar limitações no frontend)
	DataSources DataSourceInfo `json:"data_sources"`
}

// NodePoolNodeSnapshot representa o estado atual de um node no pool
type NodePoolNodeSnapshot struct {
	NodeName string `json:"node_name"`

	// Recursos
	CPUUsagePercent   float64 `json:"cpu_usage_percent"`   // uso atual (0-100)
	CPUCapacityCores  float64 `json:"cpu_capacity_cores"`  // total de cores
	MemUsagePercent   float64 `json:"mem_usage_percent"`   // uso atual (0-100)
	MemCapacityGB     float64 `json:"mem_capacity_gb"`     // total em GB
	DiskUsagePercent  float64 `json:"disk_usage_percent"`  // uso de disco (0-100)

	// Pod density
	PodCount          int     `json:"pod_count"`           // pods atuais no node
	PodCapacity       int     `json:"pod_capacity"`        // max-pods-per-node configurado
	PodDensityPercent float64 `json:"pod_density_percent"` // PodCount/PodCapacity * 100

	// conntrack (requer node_exporter)
	ConntrackEntries  int64   `json:"conntrack_entries"`   // 0 se node_exporter indisponível
	ConntrackLimit    int64   `json:"conntrack_limit"`     // 0 se indisponível
	ConntrackPercent  float64 `json:"conntrack_percent"`   // 0 se indisponível

	// Disco efêmero — crescimento (requer node_exporter com [1h] de dados)
	DiskSizeGB          float64 `json:"disk_size_gb"`            // tamanho total do disco raiz (GB)
	DiskGrowthBytesPerSec float64 `json:"disk_growth_bytes_per_sec"` // bytes/s sendo consumidos (0 se estável/decrescendo)
	DiskGrowthPctPerDay   float64 `json:"disk_growth_pct_per_day"`   // crescimento em %/dia do disco raiz
	DiskDaysUntilFull     float64 `json:"disk_days_until_full"`      // 0 = sem dados, <0 = estável/decrescendo

	// Sistema
	PIDCount int `json:"pid_count"` // 0 se indisponível

	// Resource allocation (requests somados dos pods no node — sempre via K8s API)
	CPURequestedPercent float64 `json:"cpu_requested_percent"` // requests.cpu / alocável * 100 (pode ultrapassar 100)
	MemRequestedPercent float64 `json:"mem_requested_percent"` // requests.memory / alocável * 100 (pode ultrapassar 100)
	CPURequestedCores   float64 `json:"cpu_requested_cores"`   // soma de requests.cpu em cores
	MemRequestedGB      float64 `json:"mem_requested_gb"`      // soma de requests.memory em GB

	// Estado
	IsUnschedulable  bool     `json:"is_unschedulable"`   // node está cordoned
	ActiveConditions []string `json:"active_conditions"`  // ex: ["MemoryPressure", "DiskPressure"]
	Status           string   `json:"status"`             // "Ready", "NotReady", "Cordoned"
	Age              string   `json:"age"`                // "5d3h", "12h"

	// Identificação para Prometheus
	PrometheusInstance string `json:"prometheus_instance,omitempty"` // "<IP>:9100" para filtragem
}

// TrendSnapshot representa métricas em um ponto no tempo (normalizado por node)
type TrendSnapshot struct {
	// Offset em relação a now (0 = atual, 3 = D-3, 7 = D-7, 14 = D-14)
	DaysAgo int `json:"days_ago"`

	// Timestamp exato do snapshot
	Timestamp time.Time `json:"timestamp"`

	// Valor médio POR NODE (normalizado para comparabilidade entre períodos com node counts diferentes)
	ValuePerNode float64 `json:"value_per_node"`

	// Número de nodes no período (para contexto)
	NodeCountAtTime int `json:"node_count_at_time"`

	// Valor bruto (soma) — apenas para referência
	TotalValue float64 `json:"total_value"`

	// Unidade do valor (ex: "cores", "GB", "pods", "bytes/s")
	Unit string `json:"unit"`
}

// ---------------------------------------------------------------------------
// conntrack
// ---------------------------------------------------------------------------

// ConntrackNodeInfo contém métricas de conntrack de um node específico
// (filtrado ao node pool em análise)
type ConntrackNodeInfo struct {
	Instance       string  `json:"instance"`        // "<IP>:9100" do node_exporter
	NodeName       string  `json:"node_name"`       // nome do node no Kubernetes
	CurrentEntries int64   `json:"current_entries"` // entradas atuais na tabela
	MaxEntries     int64   `json:"max_entries"`     // limite configurado no kernel
	UsagePercent   float64 `json:"usage_percent"`   // (CurrentEntries/MaxEntries)*100
	Status         string  `json:"status"`          // "ok" (<70%), "warning" (70-85%), "critical" (>85%), "emergency" (>95%)
}

// ConntrackPoolAnalysis agrega conntrack dos nodes DO POOL (visão primária)
type ConntrackPoolAnalysis struct {
	Nodes         []ConntrackNodeInfo `json:"nodes"`           // dados por node do pool
	TotalEntries  int64               `json:"total_entries"`   // soma das entradas de todos os nodes do pool
	TotalLimit    int64               `json:"total_limit"`     // soma dos limites
	AvgUsage      float64             `json:"avg_usage"`       // média de uso (%)
	MaxUsage      float64             `json:"max_usage"`       // maior uso entre os nodes do pool
	HighestNode   string              `json:"highest_node"`    // node com maior uso
	NodesWarning  int                 `json:"nodes_warning"`   // nodes entre 70-85%
	NodesCritical int                 `json:"nodes_critical"`  // nodes acima de 85%

	// Taxa de crescimento média de conntrack por hora (entries/hora somado de todos os nodes do pool).
	// Calculado via rate(node_nf_conntrack_entries[10m]) * 60 * 60.
	// Útil para estimar quando o pool vai atingir o limite de conntrack.
	AvgGrowthRatePerH float64 `json:"avg_growth_rate_per_h"` // entradas/hora (taxa de crescimento do pool)

	HasSufficientData bool   `json:"has_sufficient_data"` // false se node_exporter indisponível
	MetricSource      string `json:"metric_source"`       // "node_exporter" ou "unavailable"
}

// ConntrackClusterContext agrega conntrack de todo o cluster (visão secundária/contexto)
type ConntrackClusterContext struct {
	TotalEntries  int64   `json:"total_entries"`  // soma de entradas de todos os nodes do cluster
	TotalLimit    int64   `json:"total_limit"`    // soma dos limites
	AvgUsage      float64 `json:"avg_usage"`      // média de uso (%)
	MaxUsage      float64 `json:"max_usage"`      // maior uso no cluster inteiro
	HighestNode   string  `json:"highest_node"`   // node com maior uso no cluster
	TotalNodes    int     `json:"total_nodes"`    // total de nodes no cluster
	NodesWarning  int     `json:"nodes_warning"`
	NodesCritical int     `json:"nodes_critical"`

	// Pool como % do cluster (para contexto)
	PoolSharePercent float64 `json:"pool_share_percent"` // quanto este pool representa do uso total

	HasSufficientData bool   `json:"has_sufficient_data"`
	MetricSource      string `json:"metric_source"`
}

// ---------------------------------------------------------------------------
// Autoscaler e Pressões
// ---------------------------------------------------------------------------

// AutoscalerEvent representa um evento do Cluster Autoscaler
type AutoscalerEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`    // "ScaleUp", "ScaleDown", "Failed"
	Reason    string    `json:"reason"`  // motivo (ex: "Unschedulable pods", "Underutilized nodes")
	Message   string    `json:"message"` // descrição completa
	NodesDelta int      `json:"nodes_delta"` // +N (scale up) ou -N (scale down)
	Source    string    `json:"source"`  // "k8s_events", "azure_activity_log"
}

// NodePressureInfo representa um node com condições de pressão ativas
type NodePressureInfo struct {
	NodeName   string   `json:"node_name"`
	Conditions []string `json:"conditions"` // ["MemoryPressure", "DiskPressure", etc.]
	Severity   string   `json:"severity"`   // "warning", "critical"
}

// ---------------------------------------------------------------------------
// Bin Packing (Fragmentação)
// ---------------------------------------------------------------------------

// BinPackingAnalysis avalia eficiência de empacotamento dos pods nos nodes
type BinPackingAnalysis struct {
	// Eficiência atual (% de recursos realmente usados vs capacidade total do pool)
	CPUEfficiency    float64 `json:"cpu_efficiency"`
	MemEfficiency    float64 `json:"mem_efficiency"`
	PodEfficiency    float64 `json:"pod_efficiency_percent"`

	// Nível de fragmentação
	FragmentationLevel string `json:"fragmentation_level"` // "low" (<20% desperdício), "medium", "high"

	// Nodes que poderiam ser removidos sem impacto (scale-in candidatos)
	ScaleInCandidates int    `json:"scale_in_candidates"` // nodes com < 30% uso
	ScaleInSafe       bool   `json:"scale_in_safe"`       // é seguro fazer scale-in agora?
	ScaleInReason     string `json:"scale_in_reason,omitempty"`

	// Rebalanceamento necessário?
	RebalancingNeeded bool   `json:"rebalancing_needed"`
	WastedResources   string `json:"wasted_resources"` // descrição legível (ex: "~4 cores, ~8GB RAM")
}

// DiskGrowthAnalysis resumo do crescimento de disco efêmero do pool.
// Foca no node mais crítico (que cheia mais rápido).
type DiskGrowthAnalysis struct {
	HasData         bool    `json:"has_data"`          // true quando node_exporter forneceu dados
	FastestNode     string  `json:"fastest_node"`      // node enchendo mais rápido
	MaxGrowthPctDay float64 `json:"max_growth_pct_day"` // maior taxa de crescimento (pp/dia)
	MaxUsagePct     float64 `json:"max_usage_pct"`      // maior % de uso atual entre os nodes
	MinDaysUntilFull float64 `json:"min_days_until_full"` // menor "dias até cheio" do pool (0 = N/A)
	NodesWarning    int     `json:"nodes_warning"`     // nodes com > 70% de uso
	NodesCritical   int     `json:"nodes_critical"`    // nodes com > 85% de uso
}

// HPAPoolCorrelation representa um HPA que possui pods rodando neste pool.
type HPAPoolCorrelation struct {
	HPAName         string `json:"hpa_name"`
	Namespace       string `json:"namespace"`
	TargetName      string `json:"target_name"`
	TargetKind      string `json:"target_kind"`
	CurrentReplicas int32  `json:"current_replicas"`
	MaxReplicas     int32  `json:"max_replicas"`
	DesiredReplicas int32  `json:"desired_replicas"`
	TargetCPUPct    int32  `json:"target_cpu_percent,omitempty"`
	// AtMax = true quando current == max (HPA não consegue escalar mais)
	AtMax      bool `json:"at_max"`
	PodsOnPool int  `json:"pods_on_pool"` // pods do deployment rodando neste pool
	TotalPods  int  `json:"total_pods"`   // total de pods do deployment
}

// ---------------------------------------------------------------------------
// Saturation Timeline — previsão determinística de datas de saturação
// ---------------------------------------------------------------------------

// SaturationForecast previsão de saturação para uma métrica específica.
// Calculado deterministicamente a partir dos snapshots D-0/D-3/D-7/D-14 via
// regressão linear — independente da IA.
type SaturationForecast struct {
	// Identificação
	Metric      string `json:"metric"`                 // "cpu", "memory", "conntrack", "pods"
	AffectedNode string `json:"affected_node,omitempty"` // node mais crítico (conntrack/pods)

	// Valores atuais
	CurrentValue float64 `json:"current_value"` // valor atual em %
	Threshold    float64 `json:"threshold"`     // limiar de saturação (ex: 85.0)

	// Tendência calculada
	DailyGrowthRate   float64 `json:"daily_growth_rate"`   // p.p./dia (positivo = crescendo)
	TrendAcceleration float64 `json:"trend_acceleration"`  // positivo = piora acelerando (p.p./dia comparado à semana anterior)

	// Projeção
	DaysUntilSaturation *float64   `json:"days_until_saturation,omitempty"` // nil se estável/decrescente
	EstimatedDate       *time.Time `json:"estimated_date,omitempty"`        // data absoluta calculada

	// Qualidade da estimativa
	Confidence   string `json:"confidence"`    // "high" (3+ pontos), "medium" (2 pontos), "low" (1 ponto)
	DataPoints   int    `json:"data_points"`   // quantos snapshots históricos disponíveis (1-4)
	UrgencyBadge string `json:"urgency_badge"` // "CRITICO" (<7d), "ATENCAO" (7-30d), "ESTAVEL" (>30d ou N/A)
}

// PoolSaturationTimeline agrupa previsões de saturação de todas as métricas do pool
type PoolSaturationTimeline struct {
	Forecasts    []SaturationForecast `json:"forecasts"`
	MostCritical *SaturationForecast  `json:"most_critical,omitempty"` // métrica que satura primeiro
	Summary      string               `json:"summary"`                 // ex: "conntrack satura em 12 dias (aks-node-03)"
}

// ---------------------------------------------------------------------------
// Previsão de Capacidade
// ---------------------------------------------------------------------------

// NodePoolCapacityForecast previsão de capacidade do pool
type NodePoolCapacityForecast struct {
	// Timeline de saturação baseada nas tendências atuais
	DaysUntilCPUSaturation  int `json:"days_until_cpu_saturation"`   // 0 = já saturado
	DaysUntilMemSaturation  int `json:"days_until_mem_saturation"`
	DaysUntilPodSaturation  int `json:"days_until_pod_saturation"`   // limite max-pods-per-node
	DaysUntilConntrackRisk  int `json:"days_until_conntrack_risk"`   // quando atinge 85%

	// Fator limitante (o que vai saturar primeiro)
	LimitingFactor string `json:"limiting_factor"` // "cpu", "memory", "pods", "conntrack", "none"

	// Aviso de lead time do autoscaler
	// Se DaysUntilXSaturation < AutoscalerLeadTimeMinutes/1440, é urgente!
	AutoscalerLeadTimeMinutes int  `json:"autoscaler_lead_time_minutes"` // estimativa (default 10 min)
	IsUrgent                  bool `json:"is_urgent"`                    // saturação em < 2x lead time

	// Headroom restante
	HeadroomCPUCores  float64 `json:"headroom_cpu_cores"`  // cores disponíveis antes de precisar escalar
	HeadroomMemGB     float64 `json:"headroom_mem_gb"`
	HeadroomPodsCount int     `json:"headroom_pods_count"` // pods adicionais antes do limite
}

// ---------------------------------------------------------------------------
// Health Score
// ---------------------------------------------------------------------------

// NodePoolHealthScore pontuação de saúde do node pool (0-100)
type NodePoolHealthScore struct {
	Overall     int                        `json:"overall"`   // 0-100
	Category    string                     `json:"category"`  // "healthy" (>79), "warning" (50-79), "critical" (<50)
	Breakdown   NodePoolHealthBreakdown     `json:"breakdown"`
	LastUpdated time.Time                  `json:"last_updated"`
}

// NodePoolHealthBreakdown detalhamento com pesos específicos para node pool
// Pesos: NodeAvailability 25% | ResourceHeadroom 30% | PodDensity 20% | ConntrackSafety 15% | AutoscalerHealth 10%
type NodePoolHealthBreakdown struct {
	NodeAvailability  int `json:"node_availability"`  // 0-100 — nodes Ready vs total
	ResourceHeadroom  int `json:"resource_headroom"`  // 0-100 — CPU/mem disponível até próximo scale
	PodDensity        int `json:"pod_density"`        // 0-100 — pod density no node mais cheio
	ConntrackSafety   int `json:"conntrack_safety"`   // 0-100 — conntrack no node mais saturado
	AutoscalerHealth  int `json:"autoscaler_health"`  // 0-100 — frequência e sucesso de scale events
}

// ---------------------------------------------------------------------------
// Action Summary
// ---------------------------------------------------------------------------

// NodePoolActionSummary resumo de ação rápida para decisão imediata
type NodePoolActionSummary struct {
	Status        string `json:"status"`         // "healthy", "attention", "critical"
	StatusColor   string `json:"status_color"`   // "green", "yellow", "red"
	StatusMessage string `json:"status_message"` // "Pool operacional" / "Requer atenção" / "Risco de saturação"

	// Urgência
	HoursToCritical *int   `json:"hours_to_critical,omitempty"` // nil se sem risco iminente
	CriticalMetric  string `json:"critical_metric,omitempty"`   // "cpu", "memory", "conntrack", "pods"
	CriticalReason  string `json:"critical_reason,omitempty"`   // "conntrack atingirá 85% em 6h"

	// Ações
	TotalActions     int    `json:"total_actions"`
	UrgentActions    int    `json:"urgent_actions"`
	TopAction        string `json:"top_action,omitempty"`
	TopActionCommand string `json:"top_action_command,omitempty"` // comando az/kubectl se aplicável

	// Próxima revisão sugerida
	NextReviewDays int `json:"next_review_days"`

	// Confiança geral da análise (0-100%)
	OverallConfidence float64 `json:"overall_confidence"`
}

// ---------------------------------------------------------------------------
// Predictions (IA)
// ---------------------------------------------------------------------------

// NodePoolPredictionsAnalysis contém previsões temporais
type NodePoolPredictionsAnalysis struct {
	ShortTerm  []NodePoolPrediction `json:"short_term"`  // próximas 4h
	MediumTerm []NodePoolPrediction `json:"medium_term"` // próximas 24h
	LongTerm   []NodePoolPrediction `json:"long_term"`   // próximos 7 dias
}

// NodePoolPrediction representa uma previsão individual
type NodePoolPrediction struct {
	Timeframe         string     `json:"timeframe"`                   // "4h", "24h", "7d"
	Timestamp         *time.Time `json:"timestamp,omitempty"`         // timestamp absoluto calculado no backend
	Event             string     `json:"event"`                       // descrição do evento previsto
	Probability       float64    `json:"probability"`                 // 0.0 a 1.0
	Severity          string     `json:"severity"`                    // "low", "medium", "high", "critical"
	Impact            string     `json:"impact"`                      // descrição do impacto
	Indicators        []string   `json:"indicators"`                  // sinais que sustentam esta previsão
	ConfidencePercent float64    `json:"confidence_percent"`          // 0-100%
}

// ---------------------------------------------------------------------------
// Root Cause Analysis
// ---------------------------------------------------------------------------

// NodePoolRootCauseAnalysis análise de causa raiz do node pool
type NodePoolRootCauseAnalysis struct {
	IdentifiedCauses    []NodePoolRootCause `json:"identified_causes"`
	PrimaryFactor       string              `json:"primary_factor"`
	ContributingFactors []string            `json:"contributing_factors"`
}

// NodePoolRootCause representa uma causa identificada
type NodePoolRootCause struct {
	Cause       string   `json:"cause"`
	Evidence    []string `json:"evidence"`
	Certainty   float64  `json:"certainty"`   // 0.0 a 1.0
	Category    string   `json:"category"`    // "capacity", "config", "workload", "conntrack", "autoscaler"
	Remediation string   `json:"remediation"` // ação corretiva
}

// ---------------------------------------------------------------------------
// Executive Summary e Recommendations
// ---------------------------------------------------------------------------

// NodePoolExecutiveSummary resumo executivo não-técnico
type NodePoolExecutiveSummary struct {
	CurrentState    string   `json:"current_state"`
	KeyFindings     []string `json:"key_findings"`
	RiskLevel       string   `json:"risk_level"`     // "low", "medium", "high", "critical"
	ActionRequired  bool     `json:"action_required"`
	ExpectedOutcome string   `json:"expected_outcome"`
	BusinessImpact  string   `json:"business_impact"`
}

// NodePoolRecommendation recomendação de ação
type NodePoolRecommendation struct {
	Priority    int      `json:"priority"` // 1 (mais alta) a 5
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"` // "scaling", "conntrack", "config", "cost", "monitoring"
	Actions     []string `json:"actions"`  // passos específicos (comandos az/kubectl quando aplicável)

	ExpectedImpact string `json:"expected_impact"`

	// Estimativas de implementação
	TimeRequired     string  `json:"time_required"`      // "5 minutes", "30 minutes", "1 day"
	Complexity       string  `json:"complexity"`         // "low", "medium", "high"
	RiskLevel        string  `json:"risk_level"`         // "low", "medium", "high"
	RequiresDowntime bool    `json:"requires_downtime"`
	EfficiencyGain   float64 `json:"efficiency_gain_percent"`
}

// ---------------------------------------------------------------------------
// Custo
// ---------------------------------------------------------------------------

// NodePoolCostAnalysis análise de custo baseada no VM SKU real
// Muito mais precisa que a análise de deployment porque conhecemos o VM exato
type NodePoolCostAnalysis struct {
	// Cotação USD→BRL (mesma lógica do deployment)
	ExchangeRate     float64 `json:"exchange_rate"`
	ExchangeRateDate string  `json:"exchange_rate_date"` // "2026-02-21"

	// VM SKU
	VMSize         string `json:"vm_size"`
	VMFamily       string `json:"vm_family"`
	VMCPUCores     int    `json:"vm_cpu_cores"`
	VMMemoryGB     int    `json:"vm_memory_gb"`

	// Custo por node (baseado em preço Azure pay-as-you-go)
	CostPerNodePerHour float64 `json:"cost_per_node_per_hour_usd"` // USD/hora por VM
	CostPerNodeMonthly float64 `json:"cost_per_node_monthly_usd"`  // = hourly * 730

	// Custo atual do pool (currentNodes VMs)
	CurrentMonthlyCostUSD float64              `json:"current_monthly_cost_usd"`
	CurrentMonthlyCostBRL float64              `json:"current_monthly_cost_brl"`
	CostBreakdown         NodePoolCostBreakdown `json:"cost_breakdown"`

	// Custo máximo possível (maxNodes VMs)
	MaxMonthlyCostUSD float64 `json:"max_monthly_cost_usd"`
	MaxMonthlyCostBRL float64 `json:"max_monthly_cost_brl"`

	// Desperdício (idle waste) — nodes com utilização < 30%
	IdleWastePercent  float64 `json:"idle_waste_percent"`
	WasteMonthlyCostUSD float64 `json:"waste_monthly_cost_usd"`
	WasteMonthlyCostBRL float64 `json:"waste_monthly_cost_brl"`

	// Otimização (nodes recomendados baseado em P95 + 20% margem)
	RecommendedNodes      int     `json:"recommended_nodes"`
	OptimizedMonthlyCostUSD float64 `json:"optimized_monthly_cost_usd"`
	OptimizedMonthlyCostBRL float64 `json:"optimized_monthly_cost_brl"`
	MonthlySavingsUSD     float64 `json:"monthly_savings_usd"`
	MonthlySavingsBRL     float64 `json:"monthly_savings_brl"`
	AnnualSavingsUSD      float64 `json:"annual_savings_usd"`
	AnnualSavingsBRL      float64 `json:"annual_savings_brl"`
	SavingsPercent        float64 `json:"savings_percent"`

	// Recomendações de custo
	Recommendations []NodePoolCostRecommendation `json:"recommendations,omitempty"`
}

// NodePoolCostBreakdown detalhamento do custo do pool
type NodePoolCostBreakdown struct {
	ComputeCostUSD float64 `json:"compute_cost_usd"` // custo de CPU
	MemoryCostUSD  float64 `json:"memory_cost_usd"`  // custo de memória
	// Nota: em VMs o custo não é separado por recurso — é o SKU inteiro
	// Este breakdown é uma estimativa proporcional para referência
}

// NodePoolCostRecommendation recomendação de economia
type NodePoolCostRecommendation struct {
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Action        string  `json:"action"` // "reduce_max_nodes", "downgrade_vm_sku", "enable_spot_nodes"
	CostBeforeUSD float64 `json:"cost_before_usd"`
	CostAfterUSD  float64 `json:"cost_after_usd"`
	SavingsUSD    float64 `json:"savings_usd"`
	SavingsBRL    float64 `json:"savings_brl"`
	Impact        string  `json:"impact"` // "low", "medium", "high"
}

// ---------------------------------------------------------------------------
// Meta / Data Sources
// ---------------------------------------------------------------------------

// DataSourceInfo indica quais fontes de dados estavam disponíveis
// (para que o frontend mostre alertas de limitação quando necessário)
type DataSourceInfo struct {
	PrometheusAvailable   bool   `json:"prometheus_available"`
	MetricsServerAvailable bool  `json:"metrics_server_available"`
	AzureAPIAvailable     bool   `json:"azure_api_available"`
	NodeExporterAvailable bool   `json:"node_exporter_available"` // necessário para conntrack
	HistoryDepthDays      int    `json:"history_depth_days"`      // quantos dias de histórico disponíveis (0-14)
	LimitationNote        string `json:"limitation_note,omitempty"` // aviso legível se há limitação
}

// ---------------------------------------------------------------------------
// Trends direction (replicado do pacote predictions para autonomia)
// ---------------------------------------------------------------------------

// TrendDirection indica direção de uma tendência
type TrendDirection string

const (
	TrendUp       TrendDirection = "increasing"
	TrendDown     TrendDirection = "decreasing"
	TrendStable   TrendDirection = "stable"
	TrendVolatile TrendDirection = "volatile"
)

// NodePoolTrends agrega as tendências calculadas do pool
type NodePoolTrends struct {
	CPUTrend    TrendDirection `json:"cpu_trend"`
	MemTrend    TrendDirection `json:"mem_trend"`
	PodsTrend   TrendDirection `json:"pods_trend"`
	NetworkTrend TrendDirection `json:"network_trend"`

	// Mudanças percentuais por período (normalizadas por node)
	CPUChange3d    float64 `json:"cpu_change_3d_percent"`
	CPUChange7d    float64 `json:"cpu_change_7d_percent"`
	CPUChange14d   float64 `json:"cpu_change_14d_percent"`
	MemChange7d    float64 `json:"mem_change_7d_percent"`
	MemChange14d   float64 `json:"mem_change_14d_percent"`
	PodsChange7d   float64 `json:"pods_change_7d_percent"`

	// Conntrack trend (crítico — crescimento mais rápido que outros recursos?)
	ConntrackTrend    TrendDirection `json:"conntrack_trend"`
	ConntrackChange7d float64        `json:"conntrack_change_7d_percent"`
}
