package predictions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/sanitizer"

	"github.com/prometheus/common/model"
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
		Msg("Iniciando coleta de métricas")

	metrics := &DeploymentMetrics{
		Deployment: req.Deployment,
		Namespace:  req.Namespace,
		Cluster:    req.Cluster,
	}

	// 1. Coletar informações do deployment do Kubernetes
	if err := c.collectK8sDeploymentInfo(ctx, req, metrics); err != nil {
		return nil, fmt.Errorf("falha ao coletar informações do deployment K8s: %w", err)
	}

	// 2. Coletar métricas temporais (current, 7d ago, 30d ago)
	if err := c.collectTemporalMetrics(ctx, req, metrics); err != nil {
		return nil, fmt.Errorf("falha ao coletar métricas temporais: %w", err)
	}

	// 3. Calcular tendências
	c.calculateTrends(metrics)

	// 4. Coletar métricas de nodes
	if err := c.collectNodeMetrics(ctx, req, metrics); err != nil {
		return nil, fmt.Errorf("falha ao coletar métricas de nodes: %w", err)
	}

	// 5. Coletar aplicações concorrentes
	if err := c.collectCompetingApps(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Falha ao coletar aplicações concorrentes")
	}

	// 6. Calcular previsão de capacidade
	c.calculateCapacityForecast(metrics)

	// 7. Sanitizar dados sensíveis
	c.sanitizeMetrics(metrics)

	log.Info().
		Str("deployment", req.Deployment).
		Msg("Coleta de métricas concluída")

	return metrics, nil
}

// collectK8sDeploymentInfo coleta informações do deployment via API do Kubernetes
func (c *MetricsCollector) collectK8sDeploymentInfo(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Listar deployments do namespace específico (usando busca pelo nome)
	deployments, err := c.kubeClient.ListDeployments(ctx, []string{req.Namespace}, req.Deployment, false)
	if err != nil {
		return fmt.Errorf("falha ao obter deployment da API K8s: %w", err)
	}

	// Buscar o deployment específico na lista
	var foundDeploy *models.DeploymentSummary
	for i := range deployments {
		if deployments[i].Name == req.Deployment && deployments[i].Namespace == req.Namespace {
			foundDeploy = &deployments[i]
			break
		}
	}

	if foundDeploy == nil {
		return fmt.Errorf("deployment %s/%s não encontrado", req.Namespace, req.Deployment)
	}

	// Extrair informações do deployment
	metrics.DesiredReplicas = int32(foundDeploy.Replicas)
	metrics.CurrentReplicas = int32(foundDeploy.Replicas)
	metrics.AvailableReplicas = int32(foundDeploy.AvailableReplicas)
	metrics.ReadyReplicas = int32(foundDeploy.ReadyReplicas)

	// Resources não estão disponíveis no DeploymentSummary, usar valores padrão
	// Seria necessário buscar o deployment completo ou parsear o YAML
	metrics.Resources = ResourceRequests{
		CPURequest:    "",
		CPULimit:      "",
		MemoryRequest: "",
		MemoryLimit:   "",
	}
	log.Debug().
		Int("desired", int(metrics.DesiredReplicas)).
		Int("current", int(metrics.CurrentReplicas)).
		Int("available", int(metrics.AvailableReplicas)).
		Msg("Informações do deployment K8s coletadas")

	return nil
}

// collectTemporalMetrics coleta métricas em diferentes pontos temporais
func (c *MetricsCollector) collectTemporalMetrics(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Coletar métricas atuais
	current, err := c.collectSnapshot(ctx, req, 0)
	if err != nil {
		return fmt.Errorf("falha ao coletar métricas atuais: %w", err)
	}
	metrics.Current = *current

	// Coletar métricas de 3 dias atrás (deployments muito novos)
	day3Ago, err := c.collectSnapshot(ctx, req, 3*24*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao coletar métricas de 3 dias atrás: %w", err)
	}
	metrics.Day3Ago = *day3Ago

	// Coletar métricas de 7 dias atrás
	day7Ago, err := c.collectSnapshot(ctx, req, 7*24*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao coletar métricas de 7 dias atrás: %w", err)
	}
	metrics.Day7Ago = *day7Ago

	// Coletar métricas de 10 dias atrás
	day10Ago, err := c.collectSnapshot(ctx, req, 10*24*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao coletar métricas de 10 dias atrás: %w", err)
	}
	metrics.Day10Ago = *day10Ago

	// Coletar métricas de 14 dias atrás (dentro da retenção padrão do Prometheus de 15 dias)
	day14Ago, err := c.collectSnapshot(ctx, req, 14*24*time.Hour)
	if err != nil {
		return fmt.Errorf("falha ao coletar métricas de 14 dias atrás: %w", err)
	}
	metrics.Day14Ago = *day14Ago

	return nil
}

// collectSnapshot coleta um snapshot de métricas em um momento específico
func (c *MetricsCollector) collectSnapshot(ctx context.Context, req PredictionRequest, offset time.Duration) (*MetricSnapshot, error) {
	snapshot := &MetricSnapshot{
		Timestamp: time.Now().Add(-offset),
	}

	// CPU (métricas críticas - devem ter sucesso)
	cpuAvg, err := c.queryScalar(ctx, c.queries.GetCPUUsageQuery(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar CPU avg: %w", err)
	}
	snapshot.CPUUsageAvg = cpuAvg

	cpuP95, err := c.queryScalar(ctx, c.queries.GetCPUUsageP95Query(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar CPU P95: %w", err)
	}
	snapshot.CPUUsageP95 = cpuP95

	// Memory (métricas críticas - devem ter sucesso)
	memAvg, err := c.queryScalar(ctx, c.queries.GetMemoryUsageQuery(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Memory avg: %w", err)
	}
	snapshot.MemoryUsageAvg = memAvg

	memP95, err := c.queryScalar(ctx, c.queries.GetMemoryUsageP95Query(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Memory P95: %w", err)
	}
	snapshot.MemoryUsageP95 = memP95

	// Network
	netRx, err := c.queryScalar(ctx, c.queries.GetNetworkRxQuery(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Network RX: %w", err)
	}
	snapshot.NetworkRxAvg = netRx

	netTx, err := c.queryScalar(ctx, c.queries.GetNetworkTxQuery(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Network TX: %w", err)
	}
	snapshot.NetworkTxAvg = netTx

	// Restarts
	restarts, err := c.queryScalar(ctx, c.queries.GetRestartCountQuery(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Restart count: %w", err)
	}
	snapshot.RestartCount = int(restarts)

	// Error rate
	errorRate, err := c.queryScalar(ctx, c.queries.GetErrorRateQuery(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Error rate: %w", err)
	}
	snapshot.ErrorRate = errorRate

	// Latency
	latP50, err := c.queryScalar(ctx, c.queries.GetLatencyP50Query(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Latency P50: %w", err)
	}
	latP95, err := c.queryScalar(ctx, c.queries.GetLatencyP95Query(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Latency P95: %w", err)
	}
	latP99, err := c.queryScalar(ctx, c.queries.GetLatencyP99Query(req.Namespace, req.Deployment, offset))
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar Latency P99: %w", err)
	}

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
	totalCPU, err := c.queryScalar(ctx, c.queries.GetClusterTotalCPUQuery())
	if err != nil {
		return fmt.Errorf("falha ao coletar CPU total do cluster: %w", err)
	}
	totalMem, err := c.queryScalar(ctx, c.queries.GetClusterTotalMemoryQuery())
	if err != nil {
		return fmt.Errorf("falha ao coletar memória total do cluster: %w", err)
	}
	allocatedCPU, err := c.queryScalar(ctx, c.queries.GetClusterAllocatedCPUQuery())
	if err != nil {
		return fmt.Errorf("falha ao coletar CPU alocada do cluster: %w", err)
	}
	allocatedMem, err := c.queryScalar(ctx, c.queries.GetClusterAllocatedMemoryQuery())
	if err != nil {
		return fmt.Errorf("falha ao coletar memória alocada do cluster: %w", err)
	}

	nodeMetrics.TotalCapacity = ClusterCapacity{
		CPUTotal:       totalCPU,
		CPUAllocated:   allocatedCPU,
		CPUUtilization: (allocatedCPU / totalCPU) * 100,
		MemTotal:       totalMem / (1024 * 1024 * 1024), // Convert to GB
		MemAllocated:   allocatedMem / (1024 * 1024 * 1024),
		MemUtilization: (allocatedMem / totalMem) * 100,
	}

	// Análise de bin-packing (baseada em utilização real)
	currentEfficiency := calculateBinPackingEfficiency(allocatedCPU, totalCPU, allocatedMem, totalMem)
	fragLevel := "baixa"
	if currentEfficiency > 70 {
		fragLevel = "alta"
	} else if currentEfficiency > 50 {
		fragLevel = "média"
	}

	nodeMetrics.BinPackingAnalysis = BinPackingAnalysis{
		CurrentEfficiency:   currentEfficiency,
		FragmentationLevel:  fragLevel,
		OptimizedEfficiency: currentEfficiency + 10, // Potencial de otimização
		RebalancingNeeded:   currentEfficiency < 40 || currentEfficiency > 85,
	}

	// Coletar distribuição de pods por node para este deployment
	if err := c.collectNodeDistribution(ctx, req, &nodeMetrics); err != nil {
		log.Warn().Err(err).Msg("Failed to collect node distribution")
	}

	// VM sizing info - extrair do node real onde os pods estão rodando
	predominantType := "unknown"
	cpuPerVM := 0
	memPerVM := 0
	maxPods := 110 // Padrão K8s

	// Coletar min/max/current nodes do cluster
	minNodes := 1
	maxNodes := 10 // Padrão conservador
	currentNodes := nodeMetrics.TotalNodesInCluster

	// Tentar buscar do kube-state-metrics ou labels
	// Azure AKS: kube_node_labels com agentpool label
	if minMaxInfo := c.getNodePoolMinMax(ctx); minMaxInfo != nil {
		minNodes = minMaxInfo.Min
		maxNodes = minMaxInfo.Max
	}

	// Se temos nodes com pods do deployment, pegar dados do primeiro node real
	if len(nodeMetrics.NodeDistribution) > 0 {
		// Pegar o primeiro node com pods
		for _, nodeInfo := range nodeMetrics.NodeDistribution {
			if nodeInfo.InstanceType != "" && nodeInfo.InstanceType != "unknown" {
				predominantType = nodeInfo.InstanceType
				// Extrair CPU/Memory do node real
				cpuCap := 0.0
				fmt.Sscanf(nodeInfo.CPUCapacity, "%f", &cpuCap)
				cpuPerVM = int(cpuCap)

				memCap := 0.0
				fmt.Sscanf(nodeInfo.MemCapacity, "%fGi", &memCap)
				memPerVM = int(memCap)
				break
			}
		}
	}

	// Fallback: calcular média se não conseguimos dados reais
	if cpuPerVM == 0 || memPerVM == 0 {
		estimatedNodes := len(nodeMetrics.NodeDistribution)
		if estimatedNodes == 0 {
			estimatedNodes = int(totalCPU / 4) // Fallback: assumir ~4 CPUs por node
			if estimatedNodes == 0 {
				estimatedNodes = 1
			}
		}
		cpuPerVM = int(totalCPU / float64(estimatedNodes))
		memPerVM = int((totalMem / (1024 * 1024 * 1024)) / float64(estimatedNodes))
		predominantType = determineInstanceType(float64(cpuPerVM), float64(memPerVM))
	}

	nodeMetrics.VMSizing = VMSizingInfo{
		PredominantInstanceType: predominantType,
		CPUPerVM:                cpuPerVM,
		MemoryPerVM:             memPerVM,
		MaxPodsPerNode:          maxPods,
		MinNodes:                minNodes,
		MaxNodes:                maxNodes,
		CurrentNodes:            currentNodes,
	}

	nodeMetrics.NodesUsed = len(nodeMetrics.NodeDistribution)
	// Buscar total de nodes do cluster
	totalNodesQuery := `count(kube_node_info)`
	totalNodesResult, err := c.queryScalar(ctx, totalNodesQuery)
	if err == nil && totalNodesResult > 0 {
		nodeMetrics.TotalNodesInCluster = int(totalNodesResult)
	} else {
		nodeMetrics.TotalNodesInCluster = nodeMetrics.NodesUsed
	}

	metrics.NodeMetrics = nodeMetrics
	return nil
}

// collectCompetingApps coleta aplicações que competem por recursos
func (c *MetricsCollector) collectCompetingApps(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Query top 5 apps consumindo CPU no mesmo namespace (excluindo o deployment atual)
	query := fmt.Sprintf(
		`topk(5, sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod!~"%s-.*"}[5m])) by (pod))`,
		req.Namespace, req.Deployment,
	)

	result, err := c.promClient.Query(ctx, query)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to query competing apps, continuing without")
		metrics.CompetingApps = []CompetingApp{}
		return nil
	}

	competingApps := []CompetingApp{}

	// Processar resultados
	if vec, ok := result.(model.Vector); ok {
		for _, sample := range vec {
			podName := string(sample.Metric["pod"])
			cpuUsage := float64(sample.Value)

			// Buscar memória deste pod
			memQuery := fmt.Sprintf(
				`avg(container_memory_working_set_bytes{namespace="%s",pod="%s"})`,
				req.Namespace, podName,
			)
			memResult, memErr := c.promClient.Query(ctx, memQuery)
			memUsage := 0.0
			if memErr == nil {
				if memVec, ok := memResult.(model.Vector); ok && len(memVec) > 0 {
					memUsage = float64(memVec[0].Value) / (1024 * 1024 * 1024) // Convert to GB
				}
			}

			// Determinar nível de impacto
			impactLevel := "low"
			if cpuUsage > 2.0 {
				impactLevel = "high"
			} else if cpuUsage > 1.0 {
				impactLevel = "medium"
			}

			// Extrair deployment name do pod e buscar réplicas
			appName := extractAppNameFromPod(podName)
			replicas := c.getDeploymentReplicas(ctx, req.Namespace, appName)

			competingApps = append(competingApps, CompetingApp{
				Name:        appName,
				Namespace:   req.Namespace,
				Replicas:    replicas,
				CPUUsage:    cpuUsage,
				MemoryUsage: memUsage,
				ImpactLevel: impactLevel,
			})
		}
	}

	metrics.CompetingApps = competingApps
	log.Debug().Int("count", len(competingApps)).Msg("Competing apps collected")

	return nil
}

// collectNodeDistribution coleta distribuição de pods e recursos por node
func (c *MetricsCollector) collectNodeDistribution(ctx context.Context, req PredictionRequest, nodeMetrics *NodeMetrics) error {
	// Query para listar nodes onde os pods do deployment estão
	query := fmt.Sprintf(
		`count(kube_pod_info{namespace="%s",pod=~"%s-.*"}) by (node)`,
		req.Namespace, req.Deployment,
	)

	result, err := c.promClient.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query node distribution: %w", err)
	}

	// Processar resultado
	if vec, ok := result.(model.Vector); ok {
		for _, sample := range vec {
			nodeName := string(sample.Metric["node"])
			podCount := int(sample.Value)

			// Buscar capacidade do node
			cpuCapQuery := fmt.Sprintf(`kube_node_status_capacity{node="%s",resource="cpu"}`, nodeName)
			cpuCap, _ := c.queryScalar(ctx, cpuCapQuery)

			memCapQuery := fmt.Sprintf(`kube_node_status_capacity{node="%s",resource="memory"}`, nodeName)
			memCap, _ := c.queryScalar(ctx, memCapQuery)

			// Buscar CPU/Memory alocável
			cpuAllocQuery := fmt.Sprintf(`kube_node_status_allocatable{node="%s",resource="cpu"}`, nodeName)
			cpuAlloc, _ := c.queryScalar(ctx, cpuAllocQuery)

			memAllocQuery := fmt.Sprintf(`kube_node_status_allocatable{node="%s",resource="memory"}`, nodeName)
			memAlloc, _ := c.queryScalar(ctx, memAllocQuery)

			// Buscar uso atual de CPU do node
			cpuUsageQuery := fmt.Sprintf(
				`sum(rate(node_cpu_seconds_total{instance=~".*%s.*",mode!="idle"}[5m])) / %f * 100`,
				nodeName, cpuCap,
			)
			cpuUsage, _ := c.queryScalar(ctx, cpuUsageQuery)

			// Buscar uso atual de memória do node
			memUsageQuery := fmt.Sprintf(
				`(kube_node_status_capacity{node="%s",resource="memory"} - node_memory_MemAvailable_bytes{instance=~".*%s.*"}) / kube_node_status_capacity{node="%s",resource="memory"} * 100`,
				nodeName, nodeName, nodeName,
			)
			memUsage, _ := c.queryScalar(ctx, memUsageQuery)

			// Calcular quantas réplicas cabem (estimativa baseada em uso médio por réplica)
			canFitReplicas := 0
			if podCount > 0 && cpuAlloc > 0 {
				cpuAvailable := cpuAlloc * (100 - cpuUsage) / 100
				cpuPerPod := cpuUsage * cpuCap / 100 / float64(podCount)
				if cpuPerPod > 0 {
					canFitReplicas = int(cpuAvailable / cpuPerPod)
				}
			}

			// Sanitizar nome do node
			sanitizedNodeName := fmt.Sprintf("node-%d", len(nodeMetrics.NodeDistribution)+1)

			// Buscar o tipo de instância real dos labels do Kubernetes
			instanceType := ""

			// Query para pegar os labels do node via kube_node_labels
			labelsQuery := fmt.Sprintf(`kube_node_labels{node="%s"}`, nodeName)
			if result, err := c.promClient.Query(ctx, labelsQuery); err == nil {
				if vec, ok := result.(model.Vector); ok && len(vec) > 0 {
					// Tentar vários labels possíveis
					labels := vec[0].Metric

					// Azure usa: node.kubernetes.io/instance-type ou beta.kubernetes.io/instance-type
					// AWS usa: node.kubernetes.io/instance-type
					// GCP usa: cloud.google.com/gke-nodepool ou node.kubernetes.io/instance-type

					if val, ok := labels["label_node_kubernetes_io_instance_type"]; ok && val != "" {
						instanceType = string(val)
					} else if val, ok := labels["label_beta_kubernetes_io_instance_type"]; ok && val != "" {
						instanceType = string(val)
					} else if val, ok := labels["label_agentpool"]; ok && val != "" {
						// Azure específico: label agentpool identifica o node pool
						instanceType = string(val)
					}
				}
			}

			// Se não encontrou via labels, tenta inferir pelo tamanho
			if instanceType == "" || instanceType == "unknown" {
				instanceType = determineInstanceType(cpuCap, memCap/(1024*1024*1024))
			}

			nodeMetrics.NodeDistribution[sanitizedNodeName] = NodeInfo{
				NodeName:       sanitizedNodeName,
				PodCount:       podCount,
				CPUAvailable:   fmt.Sprintf("%.2f", cpuAlloc),
				CPUCapacity:    fmt.Sprintf("%.2f", cpuCap),
				CPUUsage:       cpuUsage,
				MemAvailable:   fmt.Sprintf("%.2fGi", memAlloc/(1024*1024*1024)),
				MemCapacity:    fmt.Sprintf("%.2fGi", memCap/(1024*1024*1024)),
				MemUsage:       memUsage,
				CanFitReplicas: canFitReplicas,
				InstanceType:   instanceType,
			}
		}
	}

	return nil
}

// calculateTrends calcula tendências baseado nas métricas temporais
func (c *MetricsCollector) calculateTrends(metrics *DeploymentMetrics) {
	trends := TrendAnalysis{}

	// CPU trends - comparar múltiplos períodos
	cpuChange3d := calculatePercentChange(metrics.Day3Ago.CPUUsageAvg, metrics.Current.CPUUsageAvg)
	cpuChange7d := calculatePercentChange(metrics.Day7Ago.CPUUsageAvg, metrics.Current.CPUUsageAvg)
	cpuChange10d := calculatePercentChange(metrics.Day10Ago.CPUUsageAvg, metrics.Current.CPUUsageAvg)
	cpuChange14d := calculatePercentChange(metrics.Day14Ago.CPUUsageAvg, metrics.Current.CPUUsageAvg)

	trends.CPUChange3d = cpuChange3d
	trends.CPUChange7d = cpuChange7d
	trends.CPUChange10d = cpuChange10d
	trends.CPUChange14d = cpuChange14d
	trends.CPUTrend = determineTrend(cpuChange7d) // Usar 7d como referência principal

	// Memory trends
	memChange7d := calculatePercentChange(metrics.Day7Ago.MemoryUsageAvg, metrics.Current.MemoryUsageAvg)
	memChange14d := calculatePercentChange(metrics.Day14Ago.MemoryUsageAvg, metrics.Current.MemoryUsageAvg)
	trends.MemoryChange7d = memChange7d
	trends.MemoryChange14d = memChange14d
	trends.MemoryTrend = determineTrend(memChange7d)

	// Error rate trends
	errorChange7d := calculatePercentChange(metrics.Day7Ago.ErrorRate, metrics.Current.ErrorRate)
	trends.ErrorRateChange7d = errorChange7d
	trends.ErrorRateTrend = determineTrend(errorChange7d)

	// Latency trends
	latencyChange7d := calculatePercentChange(metrics.Day7Ago.Latency.P95, metrics.Current.Latency.P95)
	trends.LatencyChange7d = latencyChange7d
	trends.LatencyTrend = determineTrend(latencyChange7d)

	metrics.Trends = trends
}

// calculateCapacityForecast calcula previsão de capacidade
func (c *MetricsCollector) calculateCapacityForecast(metrics *DeploymentMetrics) {
	// Calcular utilização atual de CPU e memória
	cpuUtil := metrics.NodeMetrics.TotalCapacity.CPUUtilization
	memUtil := metrics.NodeMetrics.TotalCapacity.MemUtilization

	// Determinar fator limitante
	limitingFactor := "Disponibilidade de CPU nos nodes"
	if memUtil > cpuUtil {
		limitingFactor = "Disponibilidade de memória nos nodes"
	}

	// Estimar capacidade disponível para réplicas adicionais
	cpuAvailable := metrics.NodeMetrics.TotalCapacity.CPUTotal - metrics.NodeMetrics.TotalCapacity.CPUAllocated
	memAvailable := metrics.NodeMetrics.TotalCapacity.MemTotal - metrics.NodeMetrics.TotalCapacity.MemAllocated

	// Estimar quantas réplicas adicionais cabem (baseado no uso atual por réplica)
	currentReplicas := float64(metrics.CurrentReplicas)
	if currentReplicas == 0 {
		currentReplicas = 1
	}
	cpuPerReplica := metrics.Current.CPUUsageAvg / currentReplicas
	memPerReplica := metrics.Current.MemoryUsageAvg / currentReplicas

	maxReplicasByCPU := int(cpuAvailable / cpuPerReplica)
	maxReplicasByMem := int(memAvailable / memPerReplica)
	maxAdditionalReplicas := maxReplicasByCPU
	if maxReplicasByMem < maxAdditionalReplicas {
		maxAdditionalReplicas = maxReplicasByMem
	}
	if maxAdditionalReplicas < 0 {
		maxAdditionalReplicas = 0
	}

	// Calcular quando atingirá limites (baseado na tendência de CPU)
	daysUntil80 := 30 // Padrão conservador
	daysUntil100 := 60

	if metrics.Trends.CPUChange7d > 0 {
		// Projetar quando atingirá 80% e 100% baseado na taxa de crescimento
		currentUtil := cpuUtil
		growthRate := metrics.Trends.CPUChange7d / 7 // % por dia
		if growthRate > 0 {
			daysUntil80 = int((80 - currentUtil) / growthRate)
			daysUntil100 = int((100 - currentUtil) / growthRate)
			if daysUntil80 < 1 {
				daysUntil80 = 1
			}
			if daysUntil100 < daysUntil80 {
				daysUntil100 = daysUntil80 + 3
			}
		}
	}

	// Determinar se pode escalar
	canScale := maxAdditionalReplicas > 0

	// Estimar nodes saturados e disponíveis
	estimatedNodes := int(metrics.NodeMetrics.TotalCapacity.CPUTotal / 4)
	if estimatedNodes == 0 {
		estimatedNodes = 1
	}
	saturatedNodeName := fmt.Sprintf("node-saturado (%.0f%% CPU)", cpuUtil)
	availableNodeName := fmt.Sprintf("node-disponível (%.0f%% CPU)", 100-cpuUtil)
	replicasPerNode := int(metrics.NodeMetrics.TotalCapacity.CPUTotal / cpuPerReplica / float64(estimatedNodes))
	if replicasPerNode == 0 {
		replicasPerNode = 1
	}

	// Calcular se novos nodes são necessários
	newNodesNeeded := 0
	newNodesReason := ""
	if cpuUtil > 85 || memUtil > 85 {
		newNodesNeeded = int((cpuUtil - 70) / 30) // Rough estimate
		if newNodesNeeded < 1 {
			newNodesNeeded = 1
		}
		newNodesReason = fmt.Sprintf("Cluster com utilização alta (CPU: %.1f%%, Mem: %.1f%%), recomenda-se adicionar capacidade", cpuUtil, memUtil)
	} else if !canScale {
		newNodesNeeded = 1
		newNodesReason = "Sem capacidade disponível para escalar réplicas adicionais"
	}

	forecast := CapacityForecast{
		CanScale:              canScale,
		MaxAdditionalReplicas: maxAdditionalReplicas,
		LimitingFactor:        limitingFactor,
		NodeAnalysis: NodeAnalysisDetail{
			MostSaturatedNode:    saturatedNodeName,
			BestCandidateNode:    availableNodeName,
			TotalCapacityPerNode: replicasPerNode,
		},
		ScalingTimeline: ScalingTimeline{
			Reach80PercentDate:    time.Now().Add(time.Duration(daysUntil80) * 24 * time.Hour),
			Reach100PercentDate:   time.Now().Add(time.Duration(daysUntil100) * 24 * time.Hour),
			DaysUntil80Percent:    daysUntil80,
			DaysUntil100Percent:   daysUntil100,
			RecommendedActionDate: time.Now().Add(time.Duration(daysUntil80-1) * 24 * time.Hour),
		},
		NewNodesNeeded: newNodesNeeded,
		NewNodesReason: newNodesReason,

		// Adicionar análise detalhada de crescimento
		GrowthAnalysis: c.calculateGrowthAnalysis(metrics),
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
	// Executar query no Prometheus
	result, err := c.promClient.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("falha na query Prometheus: %w", err)
	}

	// Verificar se retornou resultado
	if result == nil {
		return 0, fmt.Errorf("query Prometheus retornou resultado nil")
	}

	// Extrair valor escalar do resultado
	switch v := result.(type) {
	case *model.Scalar:
		return float64(v.Value), nil
	case model.Vector:
		if len(v) > 0 {
			return float64(v[0].Value), nil
		}
		// Sem dados = retorna 0 (normal para métricas sem dados)
		return 0, nil
	case *model.String:
		// Tentar converter string para float
		if val, err := strconv.ParseFloat(string(v.Value), 64); err == nil {
			return val, nil
		}
	}

	return 0, fmt.Errorf("não foi possível extrair valor escalar do resultado da query")
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
	if totalCPU == 0 || totalMem == 0 {
		return 0
	}
	cpuUtil := (allocCPU / totalCPU) * 100
	memUtil := (allocMem / totalMem) * 100
	// Média ponderada
	return (cpuUtil + memUtil) / 2
}

func determineInstanceType(cpu, mem float64) string {
	// Determinar tipo de instância baseado em CPU e memória
	if cpu <= 2 && mem <= 4 {
		return "t3.small"
	} else if cpu <= 2 && mem <= 8 {
		return "t3.medium"
	} else if cpu <= 4 && mem <= 16 {
		return "t3.large"
	} else if cpu <= 8 && mem <= 32 {
		return "t3.xlarge"
	} else {
		return "t3.2xlarge"
	}
}

func extractAppNameFromPod(podName string) string {
	// Extrair nome da aplicação do pod (remove sufixo hash)
	// Ex: "nginx-deployment-6d4cf56db6-abc12" -> "nginx-deployment"
	parts := strings.Split(podName, "-")
	if len(parts) >= 3 {
		// Remove últimos 2 segmentos (replicaset hash + pod hash)
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return podName
}

// getDeploymentReplicas busca o número de réplicas de um deployment
func (c *MetricsCollector) getDeploymentReplicas(ctx context.Context, namespace, deploymentName string) int {
	query := fmt.Sprintf(
		`kube_deployment_status_replicas{namespace="%s",deployment="%s"}`,
		namespace, deploymentName,
	)

	result, err := c.queryScalar(ctx, query)
	if err != nil {
		// Fallback: tentar contar pods
		podQuery := fmt.Sprintf(
			`count(kube_pod_info{namespace="%s",pod=~"%s-.*"})`,
			namespace, deploymentName,
		)
		if podResult, podErr := c.queryScalar(ctx, podQuery); podErr == nil {
			return int(podResult)
		}
		return 1 // Fallback: assumir 1 réplica
	}

	return int(result)
}

// NodePoolMinMax informações de min/max nodes
type NodePoolMinMax struct {
	Min int
	Max int
}

// getNodePoolMinMax tenta descobrir min/max nodes do node pool
func (c *MetricsCollector) getNodePoolMinMax(ctx context.Context) *NodePoolMinMax {
	// Tentar buscar do Azure AKS através de annotations/labels
	// kube_node_labels com agentpool annotations

	// Por enquanto, retornar valores conservadores
	// TODO: Integrar com Azure API ou buscar de annotations específicas
	return &NodePoolMinMax{
		Min: 1,
		Max: 10, // Padrão conservador
	}
}

// calculateGrowthAnalysis calcula análise detalhada de capacidade para crescimento
func (c *MetricsCollector) calculateGrowthAnalysis(metrics *DeploymentMetrics) GrowthCapacityAnalysis {
	// 1. Aplicação em análise
	targetApp := ApplicationCapacity{
		Name:      metrics.Deployment,
		Namespace: metrics.Namespace,
		Replicas:  int(metrics.CurrentReplicas),
		Usage: ResourceUsage{
			CPUCores: metrics.Current.CPUUsageAvg,
			MemoryGB: metrics.Current.MemoryUsageAvg / (1024 * 1024 * 1024),
		},
	}

	// 2. Aplicações concorrentes
	competingApps := make([]ApplicationCapacity, 0, len(metrics.CompetingApps))
	totalCompetingCPU := 0.0
	totalCompetingMem := 0.0

	for _, comp := range metrics.CompetingApps {
		competingApps = append(competingApps, ApplicationCapacity{
			Name:      comp.Name,
			Namespace: comp.Namespace,
			Replicas:  comp.Replicas,
			Usage: ResourceUsage{
				CPUCores: comp.CPUUsage,
				MemoryGB: comp.MemoryUsage,
			},
		})
		totalCompetingCPU += comp.CPUUsage
		totalCompetingMem += comp.MemoryUsage
	}

	// 3. Capacidade atual e máxima
	currentCapacity := CapacityInfo{
		Nodes: metrics.NodeMetrics.VMSizing.CurrentNodes,
		Resources: ResourceUsage{
			CPUCores: metrics.NodeMetrics.TotalCapacity.CPUTotal,
			MemoryGB: metrics.NodeMetrics.TotalCapacity.MemTotal,
		},
	}

	maxCapacity := CapacityInfo{
		Nodes: metrics.NodeMetrics.VMSizing.MaxNodes,
		Resources: ResourceUsage{
			CPUCores: float64(metrics.NodeMetrics.VMSizing.MaxNodes * metrics.NodeMetrics.VMSizing.CPUPerVM),
			MemoryGB: float64(metrics.NodeMetrics.VMSizing.MaxNodes * metrics.NodeMetrics.VMSizing.MemoryPerVM),
		},
	}

	// 4. Capacidade disponível para crescimento
	availableCPU := currentCapacity.Resources.CPUCores - metrics.NodeMetrics.TotalCapacity.CPUAllocated
	availableMem := currentCapacity.Resources.MemoryGB - metrics.NodeMetrics.TotalCapacity.MemAllocated

	availableForGrowth := ResourceUsage{
		CPUCores: availableCPU,
		MemoryGB: availableMem,
	}

	// 5. Calcular máximo de réplicas
	cpuPerReplica := targetApp.Usage.CPUCores / float64(targetApp.Replicas)
	memPerReplica := targetApp.Usage.MemoryGB / float64(targetApp.Replicas)
	if targetApp.Replicas == 0 {
		cpuPerReplica = 0.5 // Padrão conservador
		memPerReplica = 0.5
	}

	maxReplicasByCPU := int(availableCPU / cpuPerReplica)
	maxReplicasByMem := int(availableMem / memPerReplica)
	maxReplicasCurrentNodes := targetApp.Replicas + minInt(maxReplicasByCPU, maxReplicasByMem)

	// Máximo com max nodes
	maxCPU := maxCapacity.Resources.CPUCores - metrics.NodeMetrics.TotalCapacity.CPUAllocated + availableCPU
	maxMem := maxCapacity.Resources.MemoryGB - metrics.NodeMetrics.TotalCapacity.MemAllocated + availableMem
	maxReplicasWithMaxNodes := int(min(maxCPU/cpuPerReplica, maxMem/memPerReplica))

	// Réplicas se remover competidores
	replicasIfRemoveCompeting := int(min(
		(availableCPU+totalCompetingCPU)/cpuPerReplica,
		(availableMem+totalCompetingMem)/memPerReplica,
	))

	// 6. Recomendação
	bottleneckResource := "cpu"
	if memPerReplica/availableMem > cpuPerReplica/availableCPU {
		bottleneckResource = "memory"
	}

	recommendedMax := maxReplicasCurrentNodes
	recommendation := fmt.Sprintf("Pode escalar até %d réplicas nos nodes atuais", maxReplicasCurrentNodes)

	if maxReplicasCurrentNodes < targetApp.Replicas*2 {
		recommendation = fmt.Sprintf("Capacidade limitada: apenas %d réplicas adicionais. Considere escalar nodes para %d (max: %d réplicas)",
			maxReplicasCurrentNodes-targetApp.Replicas,
			metrics.NodeMetrics.VMSizing.MaxNodes,
			maxReplicasWithMaxNodes)
		recommendedMax = maxReplicasWithMaxNodes
	}

	return GrowthCapacityAnalysis{
		TargetApp:                 targetApp,
		CompetingApps:             competingApps,
		TotalCompetingUsage:       ResourceUsage{CPUCores: totalCompetingCPU, MemoryGB: totalCompetingMem},
		CurrentCapacity:           currentCapacity,
		MaxCapacity:               maxCapacity,
		AvailableForGrowth:        availableForGrowth,
		MaxReplicasCurrentNodes:   maxReplicasCurrentNodes,
		MaxReplicasWithMaxNodes:   maxReplicasWithMaxNodes,
		ReplicasIfRemoveCompeting: replicasIfRemoveCompeting,
		RecommendedMaxReplicas:    recommendedMax,
		GrowthRecommendation:      recommendation,
		BottleneckResource:        bottleneckResource,
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
