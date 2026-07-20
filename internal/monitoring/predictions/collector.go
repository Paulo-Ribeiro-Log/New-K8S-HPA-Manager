package predictions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"io"

	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/sanitizer"

	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// 1.5. Coletar configuração do HPA (se existir)
	if err := c.collectHPAConfiguration(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Falha ao coletar configuração do HPA (deployment pode não ter HPA)")
	}

	// 2. Coletar métricas temporais (current, 7d ago, 30d ago)
	if err := c.collectTemporalMetrics(ctx, req, metrics); err != nil {
		return nil, fmt.Errorf("falha ao coletar métricas temporais: %w", err)
	}

	// 3. Calcular tendências
	c.calculateTrends(metrics)

	// 3.5. Analisar thresholds do HPA (se existir) - chamado DEPOIS de collectTemporalMetrics
	c.analyzeHPAThresholds(metrics)

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

	// 7. Coletar logs dos pods (antes de sanitizar)
	if err := c.collectPodLogs(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Falha ao coletar logs dos pods")
	}

	// 8. Coletar métricas adicionais (RPS consolidado, OOMKill, Uptime)
	if err := c.collectAdditionalMetrics(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Falha ao coletar métricas adicionais")
	}

	// 8.5. Detectar padrões sazonais (Fase 3)
	if err := c.collectSeasonalPatterns(ctx, req, metrics); err != nil {
		log.Warn().Err(err).Msg("Falha ao coletar padrões sazonais")
	}

	// 8.7. Analisar conntrack do cluster e nodes
	if err := c.collectConntrackAnalysis(ctx, metrics); err != nil {
		log.Warn().Err(err).Msg("Falha ao coletar análise conntrack (node_exporter pode estar indisponível)")
	}

	// 9. Sanitizar dados sensíveis (inclui logs coletados)
	c.sanitizeMetrics(metrics)

	log.Info().
		Str("deployment", req.Deployment).
		Msg("Coleta de métricas concluída")

	return metrics, nil
}

// collectK8sDeploymentInfo coleta informações do deployment via API do Kubernetes
func (c *MetricsCollector) collectK8sDeploymentInfo(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Buscar deployment completo via clientset para obter metadata completa
	clientset := c.kubeClient.GetClientset()
	deployment, err := clientset.AppsV1().Deployments(req.Namespace).Get(ctx, req.Deployment, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("falha ao obter deployment %s/%s: %w", req.Namespace, req.Deployment, err)
	}

	// Extrair informações básicas
	metrics.DesiredReplicas = *deployment.Spec.Replicas
	metrics.CurrentReplicas = deployment.Status.Replicas
	metrics.AvailableReplicas = deployment.Status.AvailableReplicas
	metrics.ReadyReplicas = deployment.Status.ReadyReplicas

	// ✅ NOVO: Extrair contexto temporal para análise preditiva verdadeira
	creationTime := deployment.CreationTimestamp.Time
	now := time.Now()
	ageInDays := int(now.Sub(creationTime).Hours() / 24)

	metrics.CreationTimestamp = creationTime
	metrics.AgeInDays = ageInDays
	metrics.IsNew = ageInDays < 7                  // < 7 dias = deployment novo
	metrics.HasSufficientHistory = ageInDays >= 14 // >= 14 dias = histórico confiável

	// ✅ NOVO: Buscar predecessores (deployments similares que foram substituídos)
	predecessorInfo := c.findPredecessorDeployments(ctx, req.Cluster, req.Namespace, req.Deployment, creationTime)
	metrics.PredecessorInfo = predecessorInfo

	// Resources (extrair do primeiro container)
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := deployment.Spec.Template.Spec.Containers[0]
		metrics.Resources = ResourceRequests{
			CPURequest:    container.Resources.Requests.Cpu().String(),
			CPULimit:      container.Resources.Limits.Cpu().String(),
			MemoryRequest: container.Resources.Requests.Memory().String(),
			MemoryLimit:   container.Resources.Limits.Memory().String(),
		}
	}

	log.Info().
		Str("deployment", req.Deployment).
		Int("age_days", ageInDays).
		Bool("is_new", metrics.IsNew).
		Bool("has_history", metrics.HasSufficientHistory).
		Str("predecessor", predecessorInfo).
		Msg("Contexto temporal do deployment coletado")

	return nil
}

// findPredecessorDeployments busca deployments predecessores que foram substituídos
// Retorna string descritiva sobre o predecessor encontrado (se houver)
func (c *MetricsCollector) findPredecessorDeployments(ctx context.Context, cluster, namespace, deploymentName string, currentCreationTime time.Time) string {
	clientset := c.kubeClient.GetClientset()

	// Buscar todos os deployments do namespace
	allDeployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Debug().Err(err).Msg("Não foi possível buscar predecessores")
		return "Nenhum predecessor identificado (erro ao buscar)"
	}

	// Estratégias para encontrar predecessores:
	// 1. Mesmo nome base mas com sufixo diferente (ex: api-v1 → api-v2)
	// 2. Labels indicando versão anterior (app.kubernetes.io/version)
	// 3. Annotations de rollout history

	var predecessors []string
	baseName := extractBaseName(deploymentName)

	for _, deploy := range allDeployments.Items {
		// Ignorar o próprio deployment
		if deploy.Name == deploymentName {
			continue
		}

		// Verificar se é predecessor (criado antes E nome similar)
		deployBase := extractBaseName(deploy.Name)
		createdBefore := deploy.CreationTimestamp.Time.Before(currentCreationTime)

		if createdBefore && deployBase == baseName {
			ageDiff := int(currentCreationTime.Sub(deploy.CreationTimestamp.Time).Hours() / 24)
			predecessors = append(predecessors, fmt.Sprintf("%s (criado %d dias antes)", deploy.Name, ageDiff))
		}
	}

	if len(predecessors) == 0 {
		return "Nenhum predecessor identificado - primeira implementação ou deployment único"
	}

	if len(predecessors) == 1 {
		return fmt.Sprintf("Predecessor encontrado: %s", predecessors[0])
	}

	return fmt.Sprintf("Múltiplos predecessores: %s", strings.Join(predecessors, ", "))
}

// extractBaseName extrai nome base removendo sufixos de versão comuns
// Ex: "api-v2" → "api", "backend-prod" → "backend", "service-1.2" → "service"
func extractBaseName(name string) string {
	// Padrões comuns de sufixo: -v1, -v2, -prod, -stg, -1.0, -2024
	patterns := []string{
		`-v\d+$`,       // -v1, -v2, etc
		`-\d+\.\d+$`,   // -1.0, -2.3, etc
		`-\d{4}$`,      // -2024, -2025, etc
		`-prod$`,       // -prod
		`-stg$`,        // -stg
		`-staging$`,    // -staging
		`-production$`, // -production
		`-canary$`,     // -canary
		`-blue$`,       // -blue
		`-green$`,      // -green
	}

	baseName := name
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(baseName) {
			baseName = re.ReplaceAllString(baseName, "")
			break // Remove apenas o primeiro match
		}
	}

	return baseName
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

	// RPS (opcional — 0 se http_requests_total não instrumentado)
	if rps, rpsErr := c.queryScalar(ctx, c.queries.GetRPSQuery(req.Namespace, req.Deployment)); rpsErr == nil {
		snapshot.RPS = rps
	}

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

	// ✅ Buscar total de nodes do cluster (para contexto geral)
	totalNodesQuery := `count(kube_node_info)`
	totalNodesResult, err := c.queryScalar(ctx, totalNodesQuery)
	if err == nil && totalNodesResult > 0 {
		nodeMetrics.TotalNodesInCluster = int(totalNodesResult)
	}

	// VM sizing info - extrair do node real onde os pods estão rodando
	predominantType := "unknown"
	cpuPerVM := 0
	memPerVM := 0
	maxPods := 110 // Padrão K8s

	// Coletar min/max nodes do node pool
	minNodes := 1
	maxNodes := 10 // Padrão conservador

	// Tentar buscar do kube-state-metrics ou labels
	// Azure AKS: kube_node_labels com agentpool label
	if minMaxInfo := c.getNodePoolMinMax(ctx); minMaxInfo != nil {
		minNodes = minMaxInfo.Min
		maxNodes = minMaxInfo.Max
	}

	// ✅ NOVA LÓGICA: Usar mapeamento de VM specs do Azure (fonte confiável)
	if len(nodeMetrics.NodeDistribution) > 0 {
		// Pegar VM size do primeiro node com pods
		for _, nodeInfo := range nodeMetrics.NodeDistribution {
			if nodeInfo.InstanceType != "" && nodeInfo.InstanceType != "unknown" {
				predominantType = nodeInfo.InstanceType
				break
			}
		}
	}

	// Buscar specs reais da VM no mapeamento do Azure
	if vmSpec := GetVMSpecs(predominantType); vmSpec != nil {
		cpuPerVM = vmSpec.VCPUs
		memPerVM = vmSpec.MemoryGiB
		log.Info().
			Str("vm_size", predominantType).
			Int("vcpus", cpuPerVM).
			Int("memory_gib", memPerVM).
			Msg("VM specs obtidas do mapeamento Azure")
	} else {
		// Fallback 1: Buscar via Azure CLI (fonte oficial)
		log.Warn().
			Str("vm_size", predominantType).
			Msg("VM specs não encontradas no mapeamento, consultando Azure CLI")

		if specs := c.getVMSpecsFromAzureCLI(ctx, req.Cluster, predominantType); specs != nil {
			cpuPerVM = specs.VCPUs
			memPerVM = specs.MemoryGiB
			log.Info().
				Str("vm_size", predominantType).
				Int("vcpus", cpuPerVM).
				Int("memory_gib", memPerVM).
				Msg("VM specs obtidas via Azure CLI")
		} else {
			// Fallback 2: Tentar extrair do Prometheus (menos confiável)
			log.Warn().Msg("Azure CLI falhou, tentando extrair do Prometheus")

			if len(nodeMetrics.NodeDistribution) > 0 {
				for _, nodeInfo := range nodeMetrics.NodeDistribution {
					if nodeInfo.InstanceType != "" {
						cpuCap := 0.0
						fmt.Sscanf(nodeInfo.CPUCapacity, "%f", &cpuCap)
						cpuPerVM = int(cpuCap)

						memCap := 0.0
						fmt.Sscanf(nodeInfo.MemCapacity, "%fGi", &memCap)
						memPerVM = int(memCap)

						if cpuPerVM > 0 && memPerVM > 0 {
							log.Info().
								Int("vcpus", cpuPerVM).
								Int("memory_gib", memPerVM).
								Msg("VM specs extraídas do Prometheus")
							break
						}
					}
				}
			}

			// ❌ ÚLTIMO RECURSO: Retornar ERRO ao invés de valor mockado
			if cpuPerVM == 0 || memPerVM == 0 {
				log.Error().
					Str("vm_size", predominantType).
					Msg("ERRO CRÍTICO: Não foi possível determinar specs da VM. Análise preditiva será imprecisa!")
				// Deixar zerado para sinalizar erro - NÃO mockar valores incorretos
				cpuPerVM = 0
				memPerVM = 0
			}
		}
	}

	// ✅ NodesUsed = nodes onde ESTA aplicação está rodando (não o total do cluster)
	nodeMetrics.NodesUsed = len(nodeMetrics.NodeDistribution)

	// Se NodeDistribution está vazio, tentar via K8s API como fallback
	if nodeMetrics.NodesUsed == 0 {
		log.Warn().
			Str("deployment", req.Deployment).
			Str("namespace", req.Namespace).
			Msg("NodeDistribution is empty, trying K8s API fallback")

		if err := c.collectNodeDistributionViaK8sAPI(ctx, req, &nodeMetrics); err != nil {
			log.Error().Err(err).Msg("K8s API fallback also failed")
		}
		nodeMetrics.NodesUsed = len(nodeMetrics.NodeDistribution)
	}

	nodeMetrics.VMSizing = VMSizingInfo{
		PredominantInstanceType: predominantType,
		CPUPerVM:                cpuPerVM,
		MemoryPerVM:             memPerVM,
		MaxPodsPerNode:          maxPods,
		MinNodes:                minNodes,
		MaxNodes:                maxNodes,
		CurrentNodes:            nodeMetrics.NodesUsed, // ✅ USA NodesUsed, não TotalNodesInCluster
	}

	println("\n========== 🔍 DEBUG [Final Values] ==========")
	println("NodesUsed (where app runs):", nodeMetrics.NodesUsed)
	println("TotalNodesInCluster:", nodeMetrics.TotalNodesInCluster)
	println("CurrentNodes (VMSizing):", nodeMetrics.VMSizing.CurrentNodes)
	println("deployment:", req.Deployment)
	println("NodeDistribution:", nodeMetrics.NodeDistribution)
	println("=================================================\n")

	metrics.NodeMetrics = nodeMetrics
	return nil
}

// collectHPAConfiguration busca e analisa configuração do HPA para o deployment
func (c *MetricsCollector) collectHPAConfiguration(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	// Buscar HPA do deployment via Kubernetes API
	clientset := c.kubeClient.GetClientset()
	hpaList, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(req.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("falha ao listar HPAs: %w", err)
	}

	// Procurar HPA que referencia este deployment
	var targetHPA *autoscalingv2.HorizontalPodAutoscaler
	for i := range hpaList.Items {
		hpa := &hpaList.Items[i]
		if hpa.Spec.ScaleTargetRef.Kind == "Deployment" && hpa.Spec.ScaleTargetRef.Name == req.Deployment {
			targetHPA = hpa
			break
		}
	}

	// Se não encontrou HPA, marcar como não existente
	if targetHPA == nil {
		metrics.HPAConfig = &HPAConfiguration{
			Exists: false,
		}
		log.Debug().
			Str("deployment", req.Deployment).
			Msg("Deployment não possui HPA configurado")
		return nil
	}

	// HPA existe - extrair configuração
	hpaConfig := &HPAConfiguration{
		Exists:      true,
		MinReplicas: *targetHPA.Spec.MinReplicas,
		MaxReplicas: targetHPA.Spec.MaxReplicas,
	}

	// Extrair targets de CPU e Memory
	for _, metric := range targetHPA.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType {
			if metric.Resource.Name == "cpu" && metric.Resource.Target.AverageUtilization != nil {
				hpaConfig.TargetCPUPercent = metric.Resource.Target.AverageUtilization
			}
			if metric.Resource.Name == "memory" && metric.Resource.Target.AverageUtilization != nil {
				hpaConfig.TargetMemoryPercent = metric.Resource.Target.AverageUtilization
			}
		}
	}

	// Calcular uso atual vs request (em percentual)
	// Nota: metrics.Current será populado depois, então vamos recalcular isso no método analyzeHPAThresholds()

	metrics.HPAConfig = hpaConfig

	log.Info().
		Str("deployment", req.Deployment).
		Int("min_replicas", int(hpaConfig.MinReplicas)).
		Int("max_replicas", int(hpaConfig.MaxReplicas)).
		Interface("target_cpu", hpaConfig.TargetCPUPercent).
		Interface("target_memory", hpaConfig.TargetMemoryPercent).
		Msg("Configuração do HPA coletada")

	return nil
}

// analyzeHPAThresholds analisa proximidade aos thresholds do HPA e faz previsões
// Este método deve ser chamado DEPOIS de collectTemporalMetrics (para ter metrics.Current)
func (c *MetricsCollector) analyzeHPAThresholds(metrics *DeploymentMetrics) {
	if metrics.HPAConfig == nil || !metrics.HPAConfig.Exists {
		return
	}

	// Buscar CPU/Memory request do deployment para calcular percentuais
	cpuRequest := c.parseResourceQuantity(metrics.Resources.CPURequest)    // em cores
	memRequest := c.parseResourceQuantity(metrics.Resources.MemoryRequest) // em bytes

	if cpuRequest == 0 || memRequest == 0 {
		log.Warn().Msg("CPU/Memory request não disponíveis, usando fallback")
		// Fallback: assumir uso vs capacity total
		cpuRequest = metrics.Current.CPUUsageAvg
		memRequest = metrics.Current.MemoryUsageAvg
	}

	// Calcular percentual de uso atual vs request
	metrics.HPAConfig.CurrentCPUPercent = (metrics.Current.CPUUsageAvg / cpuRequest) * 100
	metrics.HPAConfig.CurrentMemoryPercent = (metrics.Current.MemoryUsageAvg / (memRequest / (1024 * 1024 * 1024))) * 100

	// Calcular proximidade ao threshold (quanto falta para atingir)
	if metrics.HPAConfig.TargetCPUPercent != nil {
		targetCPU := float64(*metrics.HPAConfig.TargetCPUPercent)
		proximityCPU := ((targetCPU - metrics.HPAConfig.CurrentCPUPercent) / targetCPU) * 100
		if proximityCPU < 0 {
			proximityCPU = 0 // Já ultrapassou o threshold
		}
		metrics.HPAConfig.CPUProximityToThreshold = proximityCPU
	}

	if metrics.HPAConfig.TargetMemoryPercent != nil {
		targetMem := float64(*metrics.HPAConfig.TargetMemoryPercent)
		proximityMem := ((targetMem - metrics.HPAConfig.CurrentMemoryPercent) / targetMem) * 100
		if proximityMem < 0 {
			proximityMem = 0 // Já ultrapassou o threshold
		}
		metrics.HPAConfig.MemoryProximityToThreshold = proximityMem
	}

	// Prever quando vai atingir o threshold (baseado em tendência)
	c.predictThresholdReach(metrics)

	// Recomendar novo threshold baseado em uso histórico
	c.recommendHPAThresholds(metrics)

	log.Info().
		Float64("current_cpu_percent", metrics.HPAConfig.CurrentCPUPercent).
		Float64("current_memory_percent", metrics.HPAConfig.CurrentMemoryPercent).
		Float64("cpu_proximity", metrics.HPAConfig.CPUProximityToThreshold).
		Float64("memory_proximity", metrics.HPAConfig.MemoryProximityToThreshold).
		Msg("Análise de thresholds do HPA concluída")
}

// predictThresholdReach prevê quando o threshold será atingido baseado em tendências
func (c *MetricsCollector) predictThresholdReach(metrics *DeploymentMetrics) {
	if metrics.HPAConfig.TargetCPUPercent == nil {
		return
	}

	targetCPU := float64(*metrics.HPAConfig.TargetCPUPercent)
	currentCPU := metrics.HPAConfig.CurrentCPUPercent

	// Se já ultrapassou, marcar como 0 horas
	if currentCPU >= targetCPU {
		hours := 0
		metrics.HPAConfig.WillTriggerScaleInHours = &hours
		return
	}

	// Calcular taxa de crescimento (mudança % por dia)
	growthRatePerDay := metrics.Trends.CPUChange7d / 7.0 // % por dia

	if growthRatePerDay <= 0 {
		// Tendência estável ou decrescente - não vai atingir
		return
	}

	// Calcular quantos % faltam para atingir o threshold
	percentToGo := targetCPU - currentCPU

	// Calcular quantos dias até atingir (percentToGo / growthRatePerDay)
	daysToReach := percentToGo / growthRatePerDay
	hoursToReach := int(daysToReach * 24)

	if hoursToReach > 0 && hoursToReach < 24*30 { // Só avisar se for dentro de 30 dias
		metrics.HPAConfig.WillTriggerScaleInHours = &hoursToReach
		log.Info().
			Int("hours_to_reach_threshold", hoursToReach).
			Float64("growth_rate_per_day", growthRatePerDay).
			Msg("HPA threshold será atingido em breve")
	}
}

// recommendHPAThresholds recomenda ajustes nos thresholds baseado em uso histórico
func (c *MetricsCollector) recommendHPAThresholds(metrics *DeploymentMetrics) {
	// Recomendar CPU threshold baseado em P95 histórico
	if metrics.HPAConfig.TargetCPUPercent != nil {
		// Se P95 está sempre abaixo do threshold, pode aumentar o threshold
		p95CPU := metrics.Current.CPUUsageP95
		cpuRequest := c.parseResourceQuantity(metrics.Resources.CPURequest)

		if cpuRequest > 0 {
			p95Percent := (p95CPU / cpuRequest) * 100

			// Se P95 está 20% abaixo do threshold, recomendar threshold maior
			currentThreshold := float64(*metrics.HPAConfig.TargetCPUPercent)
			if p95Percent < currentThreshold*0.8 {
				recommendedThreshold := int32(p95Percent * 1.2) // 20% acima do P95
				metrics.HPAConfig.RecommendedCPUThreshold = &recommendedThreshold
			}

			// Se P95 está acima do threshold, recomendar threshold menor
			if p95Percent > currentThreshold {
				recommendedThreshold := int32(p95Percent * 0.9) // 10% abaixo do P95
				metrics.HPAConfig.RecommendedCPUThreshold = &recommendedThreshold
			}
		}
	}

	// Lógica similar para Memory
	if metrics.HPAConfig.TargetMemoryPercent != nil {
		p95Mem := metrics.Current.MemoryUsageP95
		memRequest := c.parseResourceQuantity(metrics.Resources.MemoryRequest)

		if memRequest > 0 {
			p95Percent := (p95Mem / (memRequest / (1024 * 1024 * 1024))) * 100

			currentThreshold := float64(*metrics.HPAConfig.TargetMemoryPercent)
			if p95Percent < currentThreshold*0.8 {
				recommendedThreshold := int32(p95Percent * 1.2)
				metrics.HPAConfig.RecommendedMemoryThreshold = &recommendedThreshold
			}

			if p95Percent > currentThreshold {
				recommendedThreshold := int32(p95Percent * 0.9)
				metrics.HPAConfig.RecommendedMemoryThreshold = &recommendedThreshold
			}
		}
	}
}

// parseResourceQuantity converte string de recurso K8s para float64
// Ex: "500m" -> 0.5 (cores), "1Gi" -> 1073741824 (bytes)
func (c *MetricsCollector) parseResourceQuantity(qty string) float64 {
	if qty == "" {
		return 0
	}

	// CPU (millicores ou cores)
	if strings.HasSuffix(qty, "m") {
		val, _ := strconv.ParseFloat(strings.TrimSuffix(qty, "m"), 64)
		return val / 1000.0 // Converter millicores para cores
	}

	// Memory (Ki, Mi, Gi, etc)
	multipliers := map[string]float64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
	}

	for suffix, multiplier := range multipliers {
		if strings.HasSuffix(qty, suffix) {
			val, _ := strconv.ParseFloat(strings.TrimSuffix(qty, suffix), 64)
			return val * multiplier
		}
	}

	// Número puro (cores de CPU ou bytes de memória)
	val, _ := strconv.ParseFloat(qty, 64)
	return val
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
	// Query para listar NODES DISTINTOS onde os pods do deployment estão
	// Tentamos múltiplas fontes em ordem de preferência

	// 1. Tentar kube_pod_info (mais confiável, vem do kube-state-metrics)
	query := fmt.Sprintf(
		`count(kube_pod_info{namespace="%s",pod=~"%s-.*",created_by_kind="ReplicaSet"}) by (node)`,
		req.Namespace, req.Deployment,
	)

	log.Debug().
		Str("namespace", req.Namespace).
		Str("deployment", req.Deployment).
		Str("query", query).
		Msg("Querying node distribution (attempt 1: kube_pod_info)")

	result, err := c.promClient.Query(ctx, query)

	// Se falhou, tentar sem o filtro created_by_kind
	if err != nil || (result != nil && len(result.(model.Vector)) == 0) {
		log.Warn().Msg("kube_pod_info query failed or returned empty, trying without created_by_kind filter")
		query = fmt.Sprintf(
			`count(kube_pod_info{namespace="%s",pod=~"%s-.*"}) by (node)`,
			req.Namespace, req.Deployment,
		)
		result, err = c.promClient.Query(ctx, query)
	}

	// Se ainda falhou, tentar com métricas de container (agrupando por node)
	if err != nil || (result != nil && len(result.(model.Vector)) == 0) {
		log.Warn().Msg("kube_pod_info not available, trying container metrics grouped by node")
		query = fmt.Sprintf(
			`count(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="POD",container!=""}[5m])) by (node)`,
			req.Namespace, req.Deployment,
		)
		result, err = c.promClient.Query(ctx, query)
	}

	// Se todas queries Prometheus falharam, usar API Kubernetes diretamente
	if err != nil || (result != nil && len(result.(model.Vector)) == 0) {
		log.Warn().Msg("All Prometheus queries failed, falling back to Kubernetes API")
		return c.collectNodeDistributionViaK8sAPI(ctx, req, nodeMetrics)
	}

	// Processar resultado
	if vec, ok := result.(model.Vector); ok {
		log.Info().
			Int("nodes_found", len(vec)).
			Str("deployment", req.Deployment).
			Msg("Node distribution result")

		if len(vec) == 0 {
			log.Error().
				Str("namespace", req.Namespace).
				Str("deployment", req.Deployment).
				Msg("CRITICAL: No nodes found for running deployment - metrics may be unavailable")
		}

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

	// Log final do resultado
	log.Info().
		Int("nodes_used", len(nodeMetrics.NodeDistribution)).
		Str("deployment", req.Deployment).
		Msg("Node distribution collection completed")

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

// calculateCapacityForecast calcula previsão de capacidade baseada no DEPLOYMENT
// REFATORADO 06/02/2026: Usa métricas do deployment (CPU/Mem vs request/limit)
// ao invés de utilização cluster-wide que misturava escopos
func (c *MetricsCollector) calculateCapacityForecast(metrics *DeploymentMetrics) {
	// ========================================
	// MÉTRICAS DO DEPLOYMENT (não cluster-wide)
	// ========================================
	cpuRequest := c.parseResourceQuantity(metrics.Resources.CPURequest)    // cores por réplica
	memRequest := c.parseResourceQuantity(metrics.Resources.MemoryRequest) // bytes por réplica

	currentReplicas := float64(metrics.CurrentReplicas)
	if currentReplicas == 0 {
		currentReplicas = 1
	}

	// Uso real por réplica
	cpuPerReplica := metrics.Current.CPUUsageAvg / currentReplicas
	memPerReplica := metrics.Current.MemoryUsageAvg / currentReplicas

	// Utilização do deployment: uso real vs request (por réplica)
	cpuUtilPercent := 0.0
	memUtilPercent := 0.0
	if cpuRequest > 0 {
		cpuUtilPercent = (cpuPerReplica / cpuRequest) * 100
	}
	if memRequest > 0 {
		memUtilPercent = (memPerReplica / memRequest) * 100
	}

	log.Info().
		Float64("cpu_per_replica_cores", cpuPerReplica).
		Float64("cpu_request_cores", cpuRequest).
		Float64("cpu_util_percent", cpuUtilPercent).
		Float64("mem_per_replica_bytes", memPerReplica).
		Float64("mem_request_bytes", memRequest).
		Float64("mem_util_percent", memUtilPercent).
		Str("deployment", metrics.Deployment).
		Msg("Capacity forecast - deployment utilization")

	// ========================================
	// FATOR LIMITANTE (baseado no deployment)
	// ========================================
	limitingFactor := fmt.Sprintf("CPU do deployment (%.0f%% do request por réplica)", cpuUtilPercent)
	if memUtilPercent > cpuUtilPercent {
		limitingFactor = fmt.Sprintf("Memória do deployment (%.0f%% do request por réplica)", memUtilPercent)
	}

	// Se tem HPA, considerar proximidade ao max replicas
	hpaMaxReplicas := int32(0)
	hpaTargetCPU := 80.0 // default se não tem HPA
	if metrics.HPAConfig != nil && metrics.HPAConfig.Exists {
		hpaMaxReplicas = metrics.HPAConfig.MaxReplicas
		if metrics.HPAConfig.TargetCPUPercent != nil {
			hpaTargetCPU = float64(*metrics.HPAConfig.TargetCPUPercent)
		}

		replicaUtilPercent := (currentReplicas / float64(hpaMaxReplicas)) * 100
		if replicaUtilPercent > 80 {
			limitingFactor = fmt.Sprintf("HPA max replicas (%d) - atualmente em %d (%.0f%%)",
				hpaMaxReplicas, int(currentReplicas), replicaUtilPercent)
		}
	}

	// ========================================
	// MAX RÉPLICAS ADICIONAIS
	// ========================================
	growthAnalysis := c.calculateGrowthAnalysis(metrics)

	maxAdditionalReplicas := growthAnalysis.MaxReplicasCurrentNodes - int(currentReplicas)
	if maxAdditionalReplicas < 0 {
		maxAdditionalReplicas = 0
	}

	// HPA pode limitar antes dos nodes
	if hpaMaxReplicas > 0 {
		hpaAdditional := int(hpaMaxReplicas) - int(currentReplicas)
		if hpaAdditional < 0 {
			hpaAdditional = 0
		}
		if hpaAdditional < maxAdditionalReplicas {
			maxAdditionalReplicas = hpaAdditional
		}
	}

	canScale := maxAdditionalReplicas > 0

	// ========================================
	// TIMELINE BASEADA NO DEPLOYMENT
	// ========================================
	// Projetar quando o uso de CPU por réplica atingirá thresholds
	// 80% = threshold típico do HPA (ou o target real se configurado)
	// 100% = réplica saturada (uso = request)
	baseTimestamp := metrics.Current.Timestamp

	threshold80 := hpaTargetCPU // Usar target do HPA como referência de 80%
	threshold100 := 100.0       // 100% do request = saturação

	daysUntil80 := 365 // Muito longe (tendência estável ou decrescente)
	daysUntil100 := 365

	cpuGrowthPerDay := metrics.Trends.CPUChange7d / 7.0 // % mudança por dia

	if cpuGrowthPerDay > 0 && cpuUtilPercent > 0 {
		// Dias até atingir threshold do HPA (CPU por réplica)
		if cpuUtilPercent < threshold80 {
			daysUntil80 = int((threshold80 - cpuUtilPercent) / cpuGrowthPerDay)
		} else {
			daysUntil80 = 0 // Já ultrapassou
		}

		// Dias até saturação da réplica (100% do request)
		if cpuUtilPercent < threshold100 {
			daysUntil100 = int((threshold100 - cpuUtilPercent) / cpuGrowthPerDay)
		} else {
			daysUntil100 = 0 // Já saturado
		}
	}

	// Limitar a range razoável
	if daysUntil80 > 365 {
		daysUntil80 = 365
	}
	if daysUntil100 > 365 {
		daysUntil100 = 365
	}
	if daysUntil80 < 0 {
		daysUntil80 = 0
	}
	if daysUntil100 < 0 {
		daysUntil100 = 0
	}
	if daysUntil100 < daysUntil80 {
		daysUntil100 = daysUntil80 + 1
	}

	// ========================================
	// NODE ANALYSIS (baseada nos nodes do deployment)
	// ========================================
	saturatedNodeName := "N/A"
	availableNodeName := "N/A"
	replicasPerNode := 0

	if len(metrics.NodeMetrics.NodeDistribution) > 0 {
		maxUsage := 0.0
		minUsage := 100.0
		totalPods := 0

		for name, nodeInfo := range metrics.NodeMetrics.NodeDistribution {
			totalPods += nodeInfo.PodCount
			if nodeInfo.CPUUsage > maxUsage {
				maxUsage = nodeInfo.CPUUsage
				saturatedNodeName = fmt.Sprintf("%s (%.0f%% CPU)", name, nodeInfo.CPUUsage)
			}
			if nodeInfo.CPUUsage < minUsage {
				minUsage = nodeInfo.CPUUsage
				availableNodeName = fmt.Sprintf("%s (%.0f%% CPU)", name, nodeInfo.CPUUsage)
			}
		}

		nodeCount := len(metrics.NodeMetrics.NodeDistribution)
		if nodeCount > 0 {
			replicasPerNode = totalPods / nodeCount
			if replicasPerNode == 0 {
				replicasPerNode = 1
			}
		}
	}

	// ========================================
	// NOVOS NODES NECESSÁRIOS (baseado no deployment)
	// ========================================
	newNodesNeeded := 0
	newNodesReason := ""

	if !canScale && hpaMaxReplicas > 0 && int(currentReplicas) >= int(hpaMaxReplicas) {
		// Deployment já está no max do HPA - precisa ajustar HPA, não nodes
		newNodesReason = fmt.Sprintf("Deployment atingiu HPA max (%d réplicas). Considerar aumentar maxReplicas do HPA.", hpaMaxReplicas)
	} else if !canScale {
		// Sem capacidade para escalar - precisa de novos nodes
		newNodesNeeded = 1
		if cpuPerReplica > 0 {
			// Calcular quantos nodes precisa para dobrar réplicas
			cpuPerVM := float64(metrics.NodeMetrics.VMSizing.CPUPerVM)
			if cpuPerVM > 0 {
				replicasPerNewNode := int(cpuPerVM * 0.85 / cpuPerReplica) // 85% utilizável
				if replicasPerNewNode > 0 {
					additionalNeeded := int(currentReplicas) // Dobrar capacidade
					newNodesNeeded = (additionalNeeded + replicasPerNewNode - 1) / replicasPerNewNode
				}
			}
		}
		newNodesReason = fmt.Sprintf("Deployment %s sem capacidade para escalar nos nodes atuais. Necessário adicionar %d node(s).",
			metrics.Deployment, newNodesNeeded)
	} else if daysUntil80 < 7 && daysUntil80 > 0 {
		// CPU do deployment atingirá threshold do HPA em menos de 7 dias
		newNodesReason = fmt.Sprintf("Deployment atingirá threshold de scaling (%.0f%%) em ~%d dias. Monitorar de perto.",
			threshold80, daysUntil80)
	}

	log.Info().
		Bool("can_scale", canScale).
		Int("max_additional", maxAdditionalReplicas).
		Int("days_until_80", daysUntil80).
		Int("days_until_100", daysUntil100).
		Float64("cpu_util_percent", cpuUtilPercent).
		Float64("hpa_target_cpu", hpaTargetCPU).
		Str("limiting_factor", limitingFactor).
		Str("deployment", metrics.Deployment).
		Msg("Capacity forecast completed (deployment-scoped)")

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
			Reach80PercentDate:    baseTimestamp.Add(time.Duration(daysUntil80) * 24 * time.Hour),
			Reach100PercentDate:   baseTimestamp.Add(time.Duration(daysUntil100) * 24 * time.Hour),
			DaysUntil80Percent:    daysUntil80,
			DaysUntil100Percent:   daysUntil100,
			RecommendedActionDate: baseTimestamp.Add(time.Duration(daysUntil80-1) * 24 * time.Hour),
		},
		NewNodesNeeded: newNodesNeeded,
		NewNodesReason: newNodesReason,

		// Análise detalhada de crescimento (já era correta - per-node)
		GrowthAnalysis: growthAnalysis,
	}

	metrics.CapacityForecast = forecast
}

// collectConntrackAnalysis coleta métricas de connection tracking (nf_conntrack) por node
func (c *MetricsCollector) collectConntrackAnalysis(ctx context.Context, metrics *DeploymentMetrics) error {
	// Query entradas atuais por node
	entriesResult, err := c.promClient.Query(ctx, c.queries.GetConntrackEntriesQuery())
	if err != nil {
		metrics.ConntrackAnalysis = ConntrackAnalysis{
			HasSufficientData: false,
			MetricSource:      "unavailable",
		}
		return fmt.Errorf("falha ao consultar conntrack entries: %w", err)
	}

	entriesVec, ok := entriesResult.(model.Vector)
	if !ok || len(entriesVec) == 0 {
		metrics.ConntrackAnalysis = ConntrackAnalysis{
			HasSufficientData: false,
			MetricSource:      "unavailable",
		}
		log.Debug().Msg("conntrack: node_exporter não disponível ou sem dados")
		return nil
	}

	// Query limites por node
	limitResult, err := c.promClient.Query(ctx, c.queries.GetConntrackLimitQuery())
	if err != nil {
		metrics.ConntrackAnalysis = ConntrackAnalysis{
			HasSufficientData: false,
			MetricSource:      "unavailable",
		}
		return fmt.Errorf("falha ao consultar conntrack limit: %w", err)
	}

	// Construir mapa instance → limite
	limitMap := make(map[string]int64)
	if limitVec, ok := limitResult.(model.Vector); ok {
		for _, s := range limitVec {
			instance := string(s.Metric["instance"])
			limitMap[instance] = int64(s.Value)
		}
	}

	// Construir mapa IP → nome do node via kube_node_info
	// kube_node_info tem labels "node" (nome) e "internal_ip" (IP)
	ipToNodeName := make(map[string]string)
	if nodeInfoResult, nodeInfoErr := c.promClient.Query(ctx, c.queries.GetNodeInfoQuery()); nodeInfoErr == nil {
		if nodeInfoVec, ok := nodeInfoResult.(model.Vector); ok {
			for _, s := range nodeInfoVec {
				nodeName := string(s.Metric["node"])
				internalIP := string(s.Metric["internal_ip"])
				if nodeName != "" && internalIP != "" {
					ipToNodeName[internalIP] = nodeName
				}
			}
		}
	}

	// extractIP remove a porta de "IP:porta" → "IP"
	extractIP := func(instance string) string {
		for i := len(instance) - 1; i >= 0; i-- {
			if instance[i] == ':' {
				return instance[:i]
			}
		}
		return instance
	}

	// Construir análise por node
	var nodes []ConntrackNodeInfo
	var clusterTotal, clusterMax int64
	var nodesWarning, nodesCritical int
	var highestNode string
	var highestUsage float64

	for _, s := range entriesVec {
		instance := string(s.Metric["instance"])
		current := int64(s.Value)
		maxEntries, hasLimit := limitMap[instance]
		if !hasLimit || maxEntries == 0 {
			// Sem node_nf_conntrack_entries_limit real para este instance — não fabricar
			// percentual com um valor chutado (nf_conntrack_max real varia muito por node,
			// comum ser 4x+ maior que qualquer chute fixo, o que gerava alertas de IA de
			// "conntrack crítico" falsos). Pular o node em vez de reportar dado errado.
			log.Warn().Str("instance", instance).Msg("conntrack: limite não encontrado para este node — ignorando (sem fabricar percentual)")
			continue
		}

		usagePct := float64(current) / float64(maxEntries) * 100.0

		status := "ok"
		if usagePct >= 85 {
			status = "critical"
			nodesCritical++
		} else if usagePct >= 70 {
			status = "warning"
			nodesWarning++
		}

		// Resolver nome do node: kube_node_info > IP extraído > instance completo
		nodeIP := extractIP(instance)
		nodeName := ipToNodeName[nodeIP]
		if nodeName == "" {
			nodeName = nodeIP // fallback: só o IP sem porta
		}

		nodes = append(nodes, ConntrackNodeInfo{
			Instance:       instance,
			NodeName:       nodeName,
			CurrentEntries: current,
			MaxEntries:     maxEntries,
			UsagePercent:   usagePct,
			Status:         status,
		})

		clusterTotal += current
		clusterMax += maxEntries

		if usagePct > highestUsage {
			highestUsage = usagePct
			highestNode = nodeName
		}
	}

	clusterUsage := 0.0
	if clusterMax > 0 {
		clusterUsage = float64(clusterTotal) / float64(clusterMax) * 100.0
	}

	metrics.ConntrackAnalysis = ConntrackAnalysis{
		Nodes:             nodes,
		ClusterTotal:      clusterTotal,
		ClusterMax:        clusterMax,
		ClusterUsage:      clusterUsage,
		NodesWarning:      nodesWarning,
		NodesCritical:     nodesCritical,
		HighestNode:       highestNode,
		HighestUsage:      highestUsage,
		HasSufficientData: true,
		MetricSource:      "node_exporter",
	}

	log.Info().
		Int("total_nodes", len(nodes)).
		Float64("cluster_usage_pct", clusterUsage).
		Int("nodes_warning", nodesWarning).
		Int("nodes_critical", nodesCritical).
		Msg("Análise conntrack coletada")

	return nil
}

// queryMatrix executa range query Prometheus e retorna Matrix (série temporal)
func (c *MetricsCollector) queryMatrix(ctx context.Context, query string, start, end time.Time, step time.Duration) (model.Matrix, error) {
	result, err := c.promClient.QueryRange(ctx, query, start, end, step)
	if err != nil {
		return nil, fmt.Errorf("range query falhou: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("range query retornou nil")
	}
	matrix, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("esperado Matrix, recebido %T", result)
	}
	return matrix, nil
}

// collectSeasonalPatterns detecta padrões horários e semanais de CPU nos últimos 7 dias (Fase 3)
func (c *MetricsCollector) collectSeasonalPatterns(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour)

	query := c.queries.GetCPUUsageQuery(req.Namespace, req.Deployment, 0)
	matrix, err := c.queryMatrix(ctx, query, start, end, time.Hour)
	if err != nil || len(matrix) == 0 {
		log.Warn().Err(err).Msg("Dados insuficientes para análise sazonal")
		metrics.SeasonalPatterns.HasSufficientData = false
		return nil
	}

	// Extrair todos os pontos (timestamp + valor)
	type point struct {
		t time.Time
		v float64
	}
	var points []point
	for _, stream := range matrix {
		for _, pair := range stream.Values {
			points = append(points, point{
				t: pair.Timestamp.Time(),
				v: float64(pair.Value),
			})
		}
	}

	if len(points) < 24 {
		metrics.SeasonalPatterns.HasSufficientData = false
		return nil
	}

	// --- Padrão horário (0-23h) ---
	var hourSum [24]float64
	var hourCount [24]int
	for _, p := range points {
		h := p.t.Hour()
		hourSum[h] += p.v
		hourCount[h]++
	}

	var hourlyAvg [24]float64
	totalAvg := 0.0
	validHours := 0
	maxHourVal := 0.0
	peakHour := 0
	for h := 0; h < 24; h++ {
		if hourCount[h] > 0 {
			hourlyAvg[h] = hourSum[h] / float64(hourCount[h])
			totalAvg += hourlyAvg[h]
			validHours++
		}
		if hourlyAvg[h] > maxHourVal {
			maxHourVal = hourlyAvg[h]
			peakHour = h
		}
	}
	if validHours > 0 {
		totalAvg /= float64(validHours)
	}

	var peakHours, lowHours []int
	peakMultiplier := 1.0
	for h := 0; h < 24; h++ {
		if totalAvg > 0 {
			ratio := hourlyAvg[h] / totalAvg
			if ratio > 1.20 {
				peakHours = append(peakHours, h)
			} else if ratio < 0.80 {
				lowHours = append(lowHours, h)
			}
		}
	}
	if totalAvg > 0 && maxHourVal > 0 {
		peakMultiplier = maxHourVal / totalAvg
	}

	// --- Padrão semanal (0=Dom, 1=Seg, ..., 6=Sáb) ---
	var daySum [7]float64
	var dayCount [7]int
	for _, p := range points {
		d := int(p.t.Weekday())
		daySum[d] += p.v
		dayCount[d]++
	}

	var dailyAvg [7]float64
	weekdayTotal, weekendTotal := 0.0, 0.0
	weekdayCount, weekendCount := 0, 0
	for d := 0; d < 7; d++ {
		if dayCount[d] > 0 {
			dailyAvg[d] = daySum[d] / float64(dayCount[d])
			if d == 0 || d == 6 { // Domingo=0, Sábado=6
				weekendTotal += dailyAvg[d]
				weekendCount++
			} else {
				weekdayTotal += dailyAvg[d]
				weekdayCount++
			}
		}
	}

	dayNames := []string{"domingo", "segunda", "terca", "quarta", "quinta", "sexta", "sabado"}
	weekAvgPerDay := (weekdayTotal + weekendTotal) / 7.0
	var highDays, lowDays []string
	for d := 0; d < 7; d++ {
		if weekAvgPerDay > 0 {
			ratio := dailyAvg[d] / weekAvgPerDay
			if ratio > 1.10 {
				highDays = append(highDays, dayNames[d])
			} else if ratio < 0.90 {
				lowDays = append(lowDays, dayNames[d])
			}
		}
	}

	weekendReduction := 0.0
	if weekdayCount > 0 && weekendCount > 0 {
		wkdAvg := weekdayTotal / float64(weekdayCount)
		wkndAvg := weekendTotal / float64(weekendCount)
		if wkdAvg > 0 {
			weekendReduction = (wkdAvg - wkndAvg) / wkdAvg * 100
		}
	}

	// --- Detectar se tendência atual é sazonal (3.3) ---
	currentHour := time.Now().Hour()
	isSeasonalPeak := false
	for _, h := range peakHours {
		if h == currentHour {
			isSeasonalPeak = true
			break
		}
	}

	seasonalAdjustedTrend := string(metrics.Trends.CPUTrend)
	if isSeasonalPeak && metrics.Trends.CPUTrend == TrendUp {
		seasonalAdjustedTrend = "stable_seasonal_peak"
	}

	log.Debug().
		Str("deployment", req.Deployment).
		Int("data_points", len(points)).
		Ints("peak_hours", peakHours).
		Float64("peak_multiplier", peakMultiplier).
		Float64("weekend_reduction", weekendReduction).
		Bool("is_seasonal_peak", isSeasonalPeak).
		Msg("Padrões sazonais detectados")

	metrics.SeasonalPatterns = SeasonalPatterns{
		Hourly: HourlyPattern{
			AvgByHour:      hourlyAvg,
			PeakHours:      peakHours,
			LowHours:       lowHours,
			PeakHour:       peakHour,
			PeakMultiplier: peakMultiplier,
		},
		Weekly: WeeklyPattern{
			AvgByDay:         dailyAvg,
			HighDays:         highDays,
			LowDays:          lowDays,
			WeekendReduction: weekendReduction,
		},
		DataPoints:            len(points),
		HasSufficientData:     len(points) >= 100,
		IsTrendSeasonal:       isSeasonalPeak,
		SeasonalAdjustedTrend: seasonalAdjustedTrend,
	}

	return nil
}

// collectAdditionalMetrics coleta métricas de observabilidade complementares (Fase 5)
func (c *MetricsCollector) collectAdditionalMetrics(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	additional := AdditionalMetrics{}

	// RPS: copiado do snapshot current (já coletado em collectSnapshot)
	additional.RequestsPerSecond = metrics.Current.RPS

	// Uptime % 30d via subquery Prometheus
	uptime, err := c.queryScalar(ctx, c.queries.GetUptimeQuery(req.Namespace, req.Deployment))
	if err == nil && uptime > 0 {
		additional.UptimePercent30d = uptime
	} else {
		// Fallback: disponibilidade atual
		fallback, fErr := c.queryScalar(ctx, c.queries.GetCurrentAvailabilityQuery(req.Namespace, req.Deployment))
		if fErr == nil && fallback > 0 {
			additional.UptimePercent30d = fallback
		} else if metrics.DesiredReplicas > 0 {
			// Fallback final: ratio baseado em réplicas K8s
			additional.UptimePercent30d = float64(metrics.AvailableReplicas) / float64(metrics.DesiredReplicas) * 100
		}
	}

	// OOMKill events via K8s API (últimos 7 dias)
	oomCount, oomErr := c.countOOMKillEvents(ctx, req)
	if oomErr == nil {
		additional.OOMKillEvents7d = oomCount
	} else {
		log.Warn().Err(oomErr).Msg("Falha ao contar eventos OOMKill")
	}

	log.Debug().
		Str("deployment", req.Deployment).
		Float64("rps", additional.RequestsPerSecond).
		Float64("uptime_30d", additional.UptimePercent30d).
		Int("oom_events_7d", additional.OOMKillEvents7d).
		Msg("Metricas adicionais coletadas")

	metrics.AdditionalMetrics = additional
	return nil
}

// countOOMKillEvents conta eventos de OOMKill nos últimos 7 dias para o deployment
func (c *MetricsCollector) countOOMKillEvents(ctx context.Context, req PredictionRequest) (int, error) {
	clientset := c.kubeClient.GetClientset()
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour)

	events, err := clientset.CoreV1().Events(req.Namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "reason=OOMKilling",
	})
	if err != nil {
		return 0, fmt.Errorf("falha ao listar eventos OOMKilling: %w", err)
	}

	deploymentPrefix := req.Deployment + "-"
	count := 0
	for _, event := range events.Items {
		if !strings.HasPrefix(event.InvolvedObject.Name, deploymentPrefix) {
			continue
		}
		if event.LastTimestamp.Time.After(sevenDaysAgo) {
			count++
		}
	}

	return count, nil
}

// collectPodLogs coleta e sanitiza logs dos pods do deployment para análise pela IA
func (c *MetricsCollector) collectPodLogs(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) error {
	clientset := c.kubeClient.GetClientset()

	// Buscar deployment para obter o label selector
	deployment, err := clientset.AppsV1().Deployments(req.Namespace).Get(ctx, req.Deployment, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("falha ao obter deployment para logs: %w", err)
	}

	// Converter matchLabels em string de seletor
	if deployment.Spec.Selector == nil || len(deployment.Spec.Selector.MatchLabels) == 0 {
		return fmt.Errorf("deployment %s não possui selector definido", req.Deployment)
	}

	var selectorParts []string
	for k, v := range deployment.Spec.Selector.MatchLabels {
		selectorParts = append(selectorParts, fmt.Sprintf("%s=%s", k, v))
	}
	labelSelector := strings.Join(selectorParts, ",")

	// Listar pods do deployment
	pods, err := clientset.CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("falha ao listar pods de %s: %w", req.Deployment, err)
	}

	if len(pods.Items) == 0 {
		log.Debug().Str("deployment", req.Deployment).Msg("Nenhum pod encontrado para coleta de logs")
		return nil
	}

	// Limitar a no máximo 3 pods para não sobrecarregar a análise
	maxPods := 3
	if len(pods.Items) < maxPods {
		maxPods = len(pods.Items)
	}

	var tailLines int64 = 80

	for i := 0; i < maxPods; i++ {
		pod := pods.Items[i]

		// Ignorar pods Succeeded (jobs concluídos)
		if pod.Status.Phase == corev1.PodSucceeded {
			continue
		}

		for _, container := range pod.Spec.Containers {
			entry := PodLogEntry{
				PodName:       pod.Name,
				ContainerName: container.Name,
			}

			// Obter restart count do container
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == container.Name {
					entry.RestartCount = cs.RestartCount
					break
				}
			}

			// Coletar logs atuais (tail 80 linhas)
			logCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			logOpts := &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: &tailLines,
			}
			stream, err := clientset.CoreV1().Pods(req.Namespace).GetLogs(pod.Name, logOpts).Stream(logCtx)
			cancel()
			if err == nil {
				raw, readErr := io.ReadAll(io.LimitReader(stream, 512*1024))
				stream.Close()
				if readErr == nil && len(raw) > 0 {
					entry.LogLines = c.sanitizer.SanitizeText(string(raw))
				}
			}

			// Verificar se container está em estado de crash (CrashLoopBackOff/Error)
			inCrashState := false
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == container.Name {
					if cs.State.Waiting != nil {
						reason := cs.State.Waiting.Reason
						if reason == "CrashLoopBackOff" || reason == "Error" || reason == "OOMKilled" {
							inCrashState = true
						}
					}
					break
				}
			}

			// Coletar logs anteriores ao último restart se: houve restart OU está em crash
			if entry.RestartCount > 0 || inCrashState {
				prevCtx, cancelPrev := context.WithTimeout(ctx, 10*time.Second)
				prevOpts := &corev1.PodLogOptions{
					Container: container.Name,
					TailLines: &tailLines,
					Previous:  true,
				}
				prevStream, prevErr := clientset.CoreV1().Pods(req.Namespace).GetLogs(pod.Name, prevOpts).Stream(prevCtx)
				cancelPrev()
				if prevErr == nil {
					raw, readErr := io.ReadAll(io.LimitReader(prevStream, 256*1024))
					prevStream.Close()
					if readErr == nil && len(raw) > 0 {
						entry.PreviousLogs = c.sanitizer.SanitizeText(string(raw))
					}
				} else {
					log.Debug().Err(prevErr).
						Str("pod", pod.Name).
						Str("container", container.Name).
						Msg("Logs anteriores não disponíveis")
				}
			}

			// Adicionar entry sempre que o pod for relevante (com ou sem logs)
			// Restart count e crash state já são informações úteis para a IA
			if entry.LogLines != "" || entry.PreviousLogs != "" || entry.RestartCount > 0 || inCrashState {
				metrics.PodLogs = append(metrics.PodLogs, entry)
			}
		}
	}

	log.Info().
		Str("deployment", req.Deployment).
		Int("pods_coletados", maxPods).
		Int("entradas_logs", len(metrics.PodLogs)).
		Msg("Logs dos pods coletados e sanitizados")

	return nil
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
	// ========================================
	// CÁLCULO POR NODE (REFATORADO - 05/01/2026)
	// ========================================
	// Lógica correta:
	// 1. Calcular capacidade de 1 node (cpuPerVM, memPerVM)
	// 2. Aplicar margem de segurança de 15% (0.85 utilizável)
	// 3. Calcular uso per-replica (aplicação alvo + concorrentes)
	// 4. IMPORTANTE: Aplicações concorrentes escalam proporcionalmente ao número de nodes
	// 5. Capacidade por node = (nodeCapacity * 0.85) - competingApps
	// 6. Max réplicas = (capacidade por node / appPerReplica) * numberOfNodes

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

	// 3. Constantes
	const safetyMargin = 0.85 // 15% reservado para kubelet, kube-system, etc

	// 4. Specs de VM e nodes
	cpuPerVM := float64(metrics.NodeMetrics.VMSizing.CPUPerVM)
	memPerVM := float64(metrics.NodeMetrics.VMSizing.MemoryPerVM)
	currentNodes := metrics.NodeMetrics.VMSizing.CurrentNodes
	maxNodes := metrics.NodeMetrics.VMSizing.MaxNodes

	// Validação: Se não temos specs de VM, retornar análise vazia
	if cpuPerVM == 0 || memPerVM == 0 {
		log.Warn().
			Str("deployment", metrics.Deployment).
			Msg("VM specs não disponíveis - análise de crescimento será incompleta")

		return GrowthCapacityAnalysis{
			TargetApp:     targetApp,
			CompetingApps: competingApps,
			CurrentCapacity: CapacityInfo{
				Nodes:     currentNodes,
				Resources: ResourceUsage{CPUCores: 0, MemoryGB: 0},
			},
			MaxCapacity: CapacityInfo{
				Nodes:     maxNodes,
				Resources: ResourceUsage{CPUCores: 0, MemoryGB: 0},
			},
			GrowthRecommendation: "ERRO: Não foi possível determinar especificações da VM. Análise de crescimento indisponível.",
			BottleneckResource:   "unknown",
		}
	}

	// 5. Uso per-replica da aplicação alvo
	cpuPerReplica := 0.0
	memPerReplica := 0.0
	if targetApp.Replicas > 0 {
		cpuPerReplica = targetApp.Usage.CPUCores / float64(targetApp.Replicas)
		memPerReplica = targetApp.Usage.MemoryGB / float64(targetApp.Replicas)
	} else {
		// Fallback conservador: usar requests ou defaults
		cpuPerReplica = 0.5 // 500m
		memPerReplica = 0.5 // 512Mi
		log.Warn().
			Str("deployment", metrics.Deployment).
			Msg("Deployment sem réplicas - usando valores padrão conservadores")
	}

	// 6. Uso per-replica das aplicações concorrentes (POR NODE)
	competingCPUPerNode := 0.0
	competingMemPerNode := 0.0
	if currentNodes > 0 {
		// Aplicações concorrentes distribuídas pelos nodes atuais
		competingCPUPerNode = totalCompetingCPU / float64(currentNodes)
		competingMemPerNode = totalCompetingMem / float64(currentNodes)
	}

	log.Debug().
		Float64("cpu_per_vm", cpuPerVM).
		Float64("mem_per_vm", memPerVM).
		Float64("cpu_per_replica", cpuPerReplica).
		Float64("mem_per_replica", memPerReplica).
		Float64("competing_cpu_per_node", competingCPUPerNode).
		Float64("competing_mem_per_node", competingMemPerNode).
		Msg("Growth analysis - per-node calculations")

	// ========================================
	// CENÁRIO 1: NODES ATUAIS
	// ========================================
	// Capacidade utilizável por node (com margem de segurança)
	usableCPUPerNode := cpuPerVM * safetyMargin
	usableMemPerNode := memPerVM * safetyMargin

	// Capacidade disponível por node (descontando concorrentes)
	availableCPUPerNode := usableCPUPerNode - competingCPUPerNode
	availableMemPerNode := usableMemPerNode - competingMemPerNode

	// Max réplicas por node
	maxReplicasPerNodeByCPU := int(availableCPUPerNode / cpuPerReplica)
	maxReplicasPerNodeByMem := int(availableMemPerNode / memPerReplica)
	maxReplicasPerNode := minInt(maxReplicasPerNodeByCPU, maxReplicasPerNodeByMem)

	// Total de réplicas com nodes atuais
	maxReplicasCurrentNodes := maxReplicasPerNode * currentNodes
	if maxReplicasCurrentNodes < 0 {
		maxReplicasCurrentNodes = 0 // Não pode ser negativo
	}

	// ========================================
	// CENÁRIO 2: NODES MÁXIMOS
	// ========================================
	// IMPORTANTE: Aplicações concorrentes também escalam proporcionalmente!
	// Se tenho 3 réplicas de competing app em 1 node, terei ~30 em 10 nodes
	competingCPUPerNodeAtMax := 0.0
	competingMemPerNodeAtMax := 0.0
	if maxNodes > 0 && currentNodes > 0 {
		// Proporção de crescimento: maxNodes / currentNodes
		// Exemplo: 10 maxNodes / 1 currentNode = 10x mais réplicas de competing apps
		scaleFactor := float64(maxNodes) / float64(currentNodes)
		competingCPUPerNodeAtMax = competingCPUPerNode * scaleFactor / float64(maxNodes)
		competingMemPerNodeAtMax = competingMemPerNode * scaleFactor / float64(maxNodes)
	}

	// Capacidade disponível por node com max nodes
	availableCPUPerNodeAtMax := usableCPUPerNode - competingCPUPerNodeAtMax
	availableMemPerNodeAtMax := usableMemPerNode - competingMemPerNodeAtMax

	// Max réplicas por node com max nodes
	maxReplicasPerNodeByCPUAtMax := int(availableCPUPerNodeAtMax / cpuPerReplica)
	maxReplicasPerNodeByMemAtMax := int(availableMemPerNodeAtMax / memPerReplica)
	maxReplicasPerNodeAtMax := minInt(maxReplicasPerNodeByCPUAtMax, maxReplicasPerNodeByMemAtMax)

	// Total de réplicas com max nodes
	maxReplicasWithMaxNodes := maxReplicasPerNodeAtMax * maxNodes
	if maxReplicasWithMaxNodes < 0 {
		maxReplicasWithMaxNodes = 0
	}

	// ========================================
	// CENÁRIO 3: SEM APLICAÇÕES CONCORRENTES
	// ========================================
	// Capacidade total disponível por node (sem concorrentes)
	maxReplicasPerNodeNoConcurrentsByCPU := int(usableCPUPerNode / cpuPerReplica)
	maxReplicasPerNodeNoConcurrentsByMem := int(usableMemPerNode / memPerReplica)
	maxReplicasPerNodeNoConcurrents := minInt(maxReplicasPerNodeNoConcurrentsByCPU, maxReplicasPerNodeNoConcurrentsByMem)

	// Total de réplicas sem concorrentes (usando nodes atuais)
	replicasIfRemoveCompeting := maxReplicasPerNodeNoConcurrents * currentNodes
	if replicasIfRemoveCompeting < 0 {
		replicasIfRemoveCompeting = 0
	}

	// ========================================
	// IDENTIFICAR BOTTLENECK
	// ========================================
	bottleneckResource := "cpu"
	if maxReplicasPerNodeByMem < maxReplicasPerNodeByCPU {
		bottleneckResource = "memory"
	}

	// ========================================
	// RECOMENDAÇÃO
	// ========================================
	recommendedMax := maxReplicasCurrentNodes
	recommendation := ""

	if maxReplicasCurrentNodes == 0 {
		recommendation = "ALERTA: Capacidade esgotada! Aplicações concorrentes estão consumindo 100% dos recursos. Ação necessária: (1) Escalar nodes ou (2) Reduzir réplicas de outras aplicações."
	} else if maxReplicasCurrentNodes < targetApp.Replicas {
		recommendation = fmt.Sprintf("CRÍTICO: Capacidade atual (%d réplicas) é MENOR que réplicas existentes (%d). Cluster está sobrecarregado!",
			maxReplicasCurrentNodes, targetApp.Replicas)
	} else if maxReplicasCurrentNodes < targetApp.Replicas*2 {
		additionalReplicas := maxReplicasCurrentNodes - targetApp.Replicas
		recommendation = fmt.Sprintf("Capacidade limitada: apenas %d réplicas adicionais nos nodes atuais (%d total). "+
			"Para maior margem, escalar para %d nodes permite até %d réplicas.",
			additionalReplicas, maxReplicasCurrentNodes, maxNodes, maxReplicasWithMaxNodes)
		recommendedMax = maxReplicasWithMaxNodes
	} else {
		recommendation = fmt.Sprintf("Capacidade saudável: pode escalar até %d réplicas nos %d nodes atuais (%.1fx da carga atual). "+
			"Com %d nodes (máx), suporta até %d réplicas.",
			maxReplicasCurrentNodes, currentNodes,
			float64(maxReplicasCurrentNodes)/float64(targetApp.Replicas),
			maxNodes, maxReplicasWithMaxNodes)
		recommendedMax = maxReplicasWithMaxNodes
	}

	// ========================================
	// CAPACIDADES PARA EXIBIÇÃO
	// ========================================
	currentCapacity := CapacityInfo{
		Nodes: currentNodes,
		Resources: ResourceUsage{
			CPUCores: float64(currentNodes) * cpuPerVM,
			MemoryGB: float64(currentNodes) * memPerVM,
		},
	}

	maxCapacity := CapacityInfo{
		Nodes: maxNodes,
		Resources: ResourceUsage{
			CPUCores: float64(maxNodes) * cpuPerVM,
			MemoryGB: float64(maxNodes) * memPerVM,
		},
	}

	availableForGrowth := ResourceUsage{
		CPUCores: availableCPUPerNode * float64(currentNodes),
		MemoryGB: availableMemPerNode * float64(currentNodes),
	}

	log.Info().
		Int("max_replicas_current", maxReplicasCurrentNodes).
		Int("max_replicas_max_nodes", maxReplicasWithMaxNodes).
		Int("replicas_no_competing", replicasIfRemoveCompeting).
		Str("bottleneck", bottleneckResource).
		Msg("Growth analysis completed")

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

// collectNodeDistributionViaK8sAPI usa a API do Kubernetes como fallback
// quando Prometheus não está disponível ou não retorna dados
func (c *MetricsCollector) collectNodeDistributionViaK8sAPI(ctx context.Context, req PredictionRequest, nodeMetrics *NodeMetrics) error {
	log.Info().
		Str("namespace", req.Namespace).
		Str("deployment", req.Deployment).
		Msg("Collecting node distribution via Kubernetes API")

	// Buscar pods do deployment via clientset
	pods, err := c.kubeClient.GetClientset().CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", req.Deployment),
	})

	if err != nil {
		log.Error().Err(err).Msg("Failed to list pods via K8s API")
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Agrupar pods por node
	nodePodsCount := make(map[string]int)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" && (pod.Status.Phase == "Running" || pod.Status.Phase == "Pending") {
			nodePodsCount[pod.Spec.NodeName]++
		}
	}

	log.Info().
		Int("nodes_found", len(nodePodsCount)).
		Int("total_pods", len(pods.Items)).
		Msg("Nodes found via K8s API")

	if len(nodePodsCount) == 0 {
		log.Error().Msg("No nodes found with running pods")
		return fmt.Errorf("no nodes found for deployment %s in namespace %s", req.Deployment, req.Namespace)
	}

	// Popular NodeDistribution
	nodeIndex := 1
	for nodeName, podCount := range nodePodsCount {
		sanitizedNodeName := fmt.Sprintf("node-%d", nodeIndex)

		// Criar entrada simplificada (sem métricas detalhadas de Prometheus)
		nodeMetrics.NodeDistribution[sanitizedNodeName] = NodeInfo{
			NodeName:       sanitizedNodeName,
			PodCount:       podCount,
			CPUAvailable:   "N/A",
			CPUCapacity:    "N/A",
			CPUUsage:       0,
			MemAvailable:   "N/A",
			MemCapacity:    "N/A",
			MemUsage:       0,
			CanFitReplicas: 0,
			InstanceType:   "unknown",
		}

		log.Debug().
			Str("node", nodeName).
			Str("sanitized", sanitizedNodeName).
			Int("pod_count", podCount).
			Msg("Node added to distribution")

		nodeIndex++
	}

	return nil
}

// getVMSpecsFromAzureCLI busca especificações de VM via Azure CLI
// Fallback para quando o mapeamento estático não contém o VM size
func (c *MetricsCollector) getVMSpecsFromAzureCLI(ctx context.Context, cluster string, vmSize string) *VMSpec {
	if vmSize == "" || vmSize == "unknown" {
		return nil
	}

	// 1. Carregar configuração do cluster do arquivo clusters-config.json
	clusterConfig, err := c.loadClusterConfig(cluster)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Não foi possível carregar configuração do cluster para buscar VM specs via Azure CLI")
		return nil
	}

	// 2. Configurar subscription (obrigatório para az aks show funcionar)
	if clusterConfig.Subscription == "" {
		log.Warn().
			Str("cluster", cluster).
			Msg("Subscription não encontrada na configuração do cluster - não é possível buscar VM specs via Azure CLI")
		return nil
	}

	cmd := exec.Command("az", "account", "set", "--subscription", clusterConfig.Subscription)
	if err := cmd.Run(); err != nil {
		log.Warn().
			Err(err).
			Str("subscription", clusterConfig.Subscription).
			Msg("Falha ao configurar subscription - não é possível buscar VM specs via Azure CLI")
		return nil
	}

	// 3. Obter location do cluster via az aks show
	// Remover sufixo -admin do nome do cluster para Azure CLI
	aksClusterName := strings.TrimSuffix(cluster, "-admin")

	locationCmd := exec.Command("az", "aks", "show",
		"--name", aksClusterName,
		"--resource-group", clusterConfig.ResourceGroup,
		"--query", "location",
		"--output", "tsv")

	locationOutput, err := locationCmd.Output()
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", aksClusterName).
			Str("resource_group", clusterConfig.ResourceGroup).
			Msg("Falha ao obter location do cluster via az aks show")
		return nil
	}

	location := strings.TrimSpace(string(locationOutput))
	if location == "" {
		log.Warn().
			Str("cluster", cluster).
			Msg("Location vazia retornada por az aks show")
		return nil
	}

	// 4. Executar az vm list-sizes com filtro por nome do VM size
	// Formato: az vm list-sizes --location <location> --query "[?name=='<vmSize>']" --output json
	query := fmt.Sprintf("[?name=='%s']", vmSize)
	vmSizesCmd := exec.Command("az", "vm", "list-sizes",
		"--location", location,
		"--query", query,
		"--output", "json")

	output, err := vmSizesCmd.Output()
	if err != nil {
		// Capturar stderr para melhor debugging
		if exitError, ok := err.(*exec.ExitError); ok {
			stderr := string(exitError.Stderr)
			log.Error().
				Err(err).
				Str("stderr", stderr).
				Str("vm_size", vmSize).
				Str("location", location).
				Msg("Azure CLI falhou ao buscar VM specs")
		} else {
			log.Error().
				Err(err).
				Str("vm_size", vmSize).
				Str("location", location).
				Msg("Falha ao executar comando az vm list-sizes")
		}
		return nil
	}

	// 5. Parse do JSON retornado
	// Azure CLI retorna array de objetos: [{"name": "...", "numberOfCores": X, "memoryInMB": Y, ...}]
	var azureVMs []struct {
		Name          string `json:"name"`
		NumberOfCores int    `json:"numberOfCores"`
		MemoryInMB    int    `json:"memoryInMB"`
	}

	if err := json.Unmarshal(output, &azureVMs); err != nil {
		log.Error().
			Err(err).
			Str("vm_size", vmSize).
			Str("output", string(output)).
			Msg("Falha ao fazer parse do JSON retornado pela Azure CLI")
		return nil
	}

	// 6. Validar que encontrou o VM size
	if len(azureVMs) == 0 {
		log.Warn().
			Str("vm_size", vmSize).
			Str("location", location).
			Msg("VM size não encontrado na Azure (location incompatível ou VM size inexistente)")
		return nil
	}

	// 7. Converter para VMSpec (memória de MB para GB)
	vm := azureVMs[0]
	vmSpec := &VMSpec{
		Size:      vm.Name,
		VCPUs:     vm.NumberOfCores,
		MemoryGiB: vm.MemoryInMB / 1024, // Converter MB para GB
		Family:    "",                   // Azure CLI não retorna family, deixar vazio
	}

	log.Info().
		Str("vm_size", vmSpec.Size).
		Int("vcpus", vmSpec.VCPUs).
		Int("memory_gib", vmSpec.MemoryGiB).
		Str("location", location).
		Msg("VM specs obtidas via Azure CLI com sucesso")

	return vmSpec
}

// loadClusterConfig carrega configuração do cluster do arquivo clusters-config.json
func (c *MetricsCollector) loadClusterConfig(cluster string) (*models.ClusterConfig, error) {
	// Remover sufixo -admin se existir (normalização)
	clusterName := strings.TrimSuffix(cluster, "-admin")

	// Caminho do arquivo de configuração
	homeConfigPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json")

	// Verificar se arquivo existe
	if _, err := os.Stat(homeConfigPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("clusters-config.json não encontrado em %s - execute 'autodiscover' primeiro", homeConfigPath)
	}

	// Ler arquivo
	data, err := os.ReadFile(homeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler clusters-config.json: %w", err)
	}

	// Parse do JSON
	var clusters []models.ClusterConfig
	if err := json.Unmarshal(data, &clusters); err != nil {
		return nil, fmt.Errorf("falha ao fazer parse de clusters-config.json: %w", err)
	}

	// Buscar cluster específico
	for _, cfg := range clusters {
		if cfg.ClusterName == clusterName {
			return &cfg, nil
		}
	}

	return nil, fmt.Errorf("cluster '%s' não encontrado em clusters-config.json", clusterName)
}
