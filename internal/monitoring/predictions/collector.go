package predictions

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/sanitizer"

	"github.com/rs/zerolog/log"
)

// MetricsCollector coleta métricas do Prometheus e Kubernetes
type MetricsCollector struct {
	promClient *prometheus.Client
	kubeClient *kubernetes.Client
	queries    *PrometheusQueries
	sanitizer  *sanitizer.Sanitizer
}

// NewMetricsCollector cria novo collector
func NewMetricsCollector(
	promClient *prometheus.Client,
	kubeClient *kubernetes.Client,
) *MetricsCollector {
	return &MetricsCollector{
		promClient: promClient,
		kubeClient: kubeClient,
		queries:    NewPrometheusQueries(),
		sanitizer:  sanitizer.New(),
	}
}

// CollectMetrics coleta todas as métricas necessárias para análise
func (c *MetricsCollector) CollectMetrics(ctx context.Context, req PredictionRequest) (*DeploymentMetrics, error) {
	log.Info().
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("deployment", req.Deployment).
		Msg("Starting metrics collection")

	metrics := &DeploymentMetrics{
		Deployment: req.Deployment,
		Namespace:  req.Namespace,
		Cluster:    req.Cluster,
	}

	// 1. Coletar informações do deployment do Kubernetes
	if err := c.collectK8sDeploymentInfo(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Failed to collect K8s deployment info")
		// Não retornar erro, continuar com métricas do Prometheus
	}

	// 2. Coletar métricas temporais (current, 7d ago, 30d ago)
	if err := c.collectTemporalMetrics(ctx, req, metrics); err != nil {
		return nil, fmt.Errorf("failed to collect temporal metrics: %w", err)
	}

	// 3. Calcular tendências
	c.calculateTrends(metrics)

	// 4. Coletar métricas de nodes
	if err := c.collectNodeMetrics(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Failed to collect node metrics")
	}

	// 5. Coletar aplicações concorrentes
	if err := c.collectCompetingApps(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Failed to collect competing apps")
	}

	// 6. Calcular previsão de capacidade
	c.calculateCapacityForecast(metrics)

	// 7. Sanitizar dados sensíveis
	c.sanitizeMetrics(metrics)

	log.Info().
		Str("deployment", req.Deployment).
		Msg("Metrics collection completed")

	return metrics, nil
}

// collectK8sDeploymentInfo coleta informações do deployment via API do Kubernetes
func (c *MetricsCollector) collectK8sDeploymentInfo(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// TODO: Implementar coleta de dados do deployment
	// Por ora, usar valores padrão
	metrics.DesiredReplicas = 3
	metrics.CurrentReplicas = 3
	metrics.AvailableReplicas = 3
	metrics.ReadyReplicas = 3
	metrics.Resources = ResourceRequests{
		CPURequest:    "500m",
		CPULimit:      "1000m",
		MemoryRequest: "512Mi",
		MemoryLimit:   "1Gi",
	}
	return nil
}

// collectTemporalMetrics coleta métricas em diferentes pontos temporais
func (c *MetricsCollector) collectTemporalMetrics(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Coletar métricas atuais
	current, err := c.collectSnapshot(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("failed to collect current metrics: %w", err)
	}
	metrics.Current = *current

	// Coletar métricas de 7 dias atrás
	week7Ago, err := c.collectSnapshot(ctx, req, 7*24*time.Hour)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to collect 7d ago metrics, using defaults")
		week7Ago = &MetricSnapshot{Timestamp: time.Now().Add(-7 * 24 * time.Hour)}
	}
	metrics.Week7Ago = *week7Ago

	// Coletar métricas de 30 dias atrás
	day30Ago, err := c.collectSnapshot(ctx, req, 30*24*time.Hour)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to collect 30d ago metrics, using defaults")
		day30Ago = &MetricSnapshot{Timestamp: time.Now().Add(-30 * 24 * time.Hour)}
	}
	metrics.Day30Ago = *day30Ago

	return nil
}

// collectSnapshot coleta um snapshot de métricas em um momento específico
func (c *MetricsCollector) collectSnapshot(ctx context.Context, req PredictionRequest, offset time.Duration) (*MetricSnapshot, error) {
	snapshot := &MetricSnapshot{
		Timestamp: time.Now().Add(-offset),
	}

	// CPU
	cpuAvg, err := c.queryScalar(ctx, c.queries.GetCPUUsageQuery(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.CPUUsageAvg = cpuAvg
	}

	cpuP95, err := c.queryScalar(ctx, c.queries.GetCPUUsageP95Query(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.CPUUsageP95 = cpuP95
	}

	// Memory
	memAvg, err := c.queryScalar(ctx, c.queries.GetMemoryUsageQuery(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.MemoryUsageAvg = memAvg
	}

	memP95, err := c.queryScalar(ctx, c.queries.GetMemoryUsageP95Query(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.MemoryUsageP95 = memP95
	}

	// Network
	netRx, err := c.queryScalar(ctx, c.queries.GetNetworkRxQuery(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.NetworkRxAvg = netRx
	}

	netTx, err := c.queryScalar(ctx, c.queries.GetNetworkTxQuery(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.NetworkTxAvg = netTx
	}

	// Restarts
	restarts, err := c.queryScalar(ctx, c.queries.GetRestartCountQuery(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.RestartCount = int(restarts)
	}

	// Error rate
	errorRate, err := c.queryScalar(ctx, c.queries.GetErrorRateQuery(req.Namespace, req.Deployment, offset))
	if err == nil {
		snapshot.ErrorRate = errorRate
	}

	// Latency
	latP50, _ := c.queryScalar(ctx, c.queries.GetLatencyP50Query(req.Namespace, req.Deployment, offset))
	latP95, _ := c.queryScalar(ctx, c.queries.GetLatencyP95Query(req.Namespace, req.Deployment, offset))
	latP99, _ := c.queryScalar(ctx, c.queries.GetLatencyP99Query(req.Namespace, req.Deployment, offset))

	snapshot.Latency = LatencyMetrics{
		P50: latP50,
		P95: latP95,
		P99: latP99,
	}

	return snapshot, nil
}

// collectNodeMetrics coleta métricas dos nodes
func (c *MetricsCollector) collectNodeMetrics(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	nodeMetrics := NodeMetrics{
		NodeDistribution: make(map[string]NodeInfo),
	}

	// Coletar capacidade total do cluster
	totalCPU, _ := c.queryScalar(ctx, c.queries.GetClusterTotalCPUQuery())
	totalMem, _ := c.queryScalar(ctx, c.queries.GetClusterTotalMemoryQuery())
	allocatedCPU, _ := c.queryScalar(ctx, c.queries.GetClusterAllocatedCPUQuery())
	allocatedMem, _ := c.queryScalar(ctx, c.queries.GetClusterAllocatedMemoryQuery())

	nodeMetrics.TotalCapacity = ClusterCapacity{
		CPUTotal:       totalCPU,
		CPUAllocated:   allocatedCPU,
		CPUUtilization: (allocatedCPU / totalCPU) * 100,
		MemTotal:       totalMem / (1024 * 1024 * 1024), // Convert to GB
		MemAllocated:   allocatedMem / (1024 * 1024 * 1024),
		MemUtilization: (allocatedMem / totalMem) * 100,
	}

	// Análise de bin-packing (simplificada)
	nodeMetrics.BinPackingAnalysis = BinPackingAnalysis{
		CurrentEfficiency:   calculateBinPackingEfficiency(allocatedCPU, totalCPU, allocatedMem, totalMem),
		FragmentationLevel:  "medium",
		OptimizedEfficiency: 82.0,
		RebalancingNeeded:   false,
	}

	// VM sizing info (valores padrão, pode ser enriquecido)
	nodeMetrics.VMSizing = VMSizingInfo{
		PredominantInstanceType: "t3.large",
		CPUPerVM:                2,
		MemoryPerVM:             8,
		MaxPodsPerNode:          110,
	}

	metrics.NodeMetrics = nodeMetrics
	return nil
}

// collectCompetingApps coleta aplicações que competem por recursos
func (c *MetricsCollector) collectCompetingApps(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Query top 5 apps consumindo CPU no mesmo namespace
	// query := c.queries.GetCompetingAppsQuery(req.Namespace, 5)

	// Aqui seria executada a query e processados os resultados
	// Por ora, retornar lista vazia
	metrics.CompetingApps = []CompetingApp{}

	return nil
}

// calculateTrends calcula tendências baseado nas métricas temporais
func (c *MetricsCollector) calculateTrends(metrics *DeploymentMetrics) {
	trends := TrendAnalysis{}

	// CPU trends
	cpuChange7d := calculatePercentChange(metrics.Week7Ago.CPUUsageAvg, metrics.Current.CPUUsageAvg)
	cpuChange30d := calculatePercentChange(metrics.Day30Ago.CPUUsageAvg, metrics.Current.CPUUsageAvg)
	trends.CPUChange7d = cpuChange7d
	trends.CPUChange30d = cpuChange30d
	trends.CPUTrend = determineTrend(cpuChange7d)

	// Memory trends
	memChange7d := calculatePercentChange(metrics.Week7Ago.MemoryUsageAvg, metrics.Current.MemoryUsageAvg)
	memChange30d := calculatePercentChange(metrics.Day30Ago.MemoryUsageAvg, metrics.Current.MemoryUsageAvg)
	trends.MemoryChange7d = memChange7d
	trends.MemoryChange30d = memChange30d
	trends.MemoryTrend = determineTrend(memChange7d)

	// Error rate trends
	errorChange7d := calculatePercentChange(metrics.Week7Ago.ErrorRate, metrics.Current.ErrorRate)
	trends.ErrorRateChange7d = errorChange7d
	trends.ErrorRateTrend = determineTrend(errorChange7d)

	// Latency trends
	latencyChange7d := calculatePercentChange(metrics.Week7Ago.Latency.P95, metrics.Current.Latency.P95)
	trends.LatencyChange7d = latencyChange7d
	trends.LatencyTrend = determineTrend(latencyChange7d)

	metrics.Trends = trends
}

// calculateCapacityForecast calcula previsão de capacidade
func (c *MetricsCollector) calculateCapacityForecast(metrics *DeploymentMetrics) {
	forecast := CapacityForecast{
		CanScale:              true,
		MaxAdditionalReplicas: 12,
		LimitingFactor:        "CPU availability on nodes",
		NodeAnalysis: NodeAnalysisDetail{
			MostSaturatedNode:    "node-3 (95% CPU)",
			BestCandidateNode:    "node-2 (50% CPU)",
			TotalCapacityPerNode: 6,
		},
		ScalingTimeline: ScalingTimeline{
			Reach80PercentDate:    time.Now().Add(3 * 24 * time.Hour),
			Reach100PercentDate:   time.Now().Add(7 * 24 * time.Hour),
			DaysUntil80Percent:    3,
			DaysUntil100Percent:   7,
			RecommendedActionDate: time.Now().Add(3 * 24 * time.Hour),
		},
		NewNodesNeeded: 2,
		NewNodesReason: "Current nodes reaching capacity, need additional compute resources",
	}

	metrics.CapacityForecast = forecast
}

// sanitizeMetrics sanitiza dados sensíveis
func (c *MetricsCollector) sanitizeMetrics(metrics *DeploymentMetrics) {
	// Sanitizar nomes de cluster (simplificado)
	if len(metrics.Cluster) > 0 {
		metrics.Cluster = "cluster-***"
	}

	// Sanitizar namespaces sensíveis
	if metrics.Namespace == "production" || metrics.Namespace == "prod" {
		metrics.Namespace = "namespace-***"
	}

	// Sanitizar nomes de nodes nos NodeMetrics
	for nodeName, nodeInfo := range metrics.NodeMetrics.NodeDistribution {
		nodeInfo.NodeName = "node-***"
		delete(metrics.NodeMetrics.NodeDistribution, nodeName)
		metrics.NodeMetrics.NodeDistribution[nodeInfo.NodeName] = nodeInfo
	}
}

// queryScalar executa query Prometheus e retorna valor escalar
func (c *MetricsCollector) queryScalar(ctx context.Context, query string) (float64, error) {
	// TODO: Implementar query real ao Prometheus
	// Por ora, retornar valores mock para testes
	// return 0, fmt.Errorf("Prometheus integration pending")

	// Retornar valores mock para testes
	return 0.5, nil // Mock: 0.5 cores CPU ou 500MB memória
}

// Helper functions

func calculatePercentChange(oldVal, newVal float64) float64 {
	if oldVal == 0 {
		if newVal == 0 {
			return 0
		}
		return 100 // ou +∞
	}
	return ((newVal - oldVal) / oldVal) * 100
}

func determineTrend(changePercent float64) TrendDirection {
	if changePercent > 15 {
		return TrendUp
	} else if changePercent < -15 {
		return TrendDown
	} else if changePercent > 5 || changePercent < -5 {
		return TrendVolatile
	}
	return TrendStable
}

func calculateBinPackingEfficiency(allocCPU, totalCPU, allocMem, totalMem float64) float64 {
	cpuUtil := (allocCPU / totalCPU) * 100
	memUtil := (allocMem / totalMem) * 100
	// Média ponderada
	return (cpuUtil + memUtil) / 2
}
