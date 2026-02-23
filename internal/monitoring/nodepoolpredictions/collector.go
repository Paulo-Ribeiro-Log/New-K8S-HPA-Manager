package nodepoolpredictions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/prometheus"

	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePoolCollector coleta métricas do Kubernetes, Prometheus e Azure
// para análise preditiva de um node pool específico.
type NodePoolCollector struct {
	promClient *prometheus.Client
	kubeClient *kubernetes.Client
	queries    *NodePoolQueries
}

// NewNodePoolCollector cria um novo collector para análise preditiva de node pool.
// promClient pode ser nil — nesse caso há degradação graceful para Metrics Server + K8s API.
func NewNodePoolCollector(promClient *prometheus.Client, kubeClient *kubernetes.Client) *NodePoolCollector {
	return &NodePoolCollector{
		promClient: promClient,
		kubeClient: kubeClient,
		queries:    NewNodePoolQueries(),
	}
}

// azureNodePoolConfig contém dados da Azure AKS API sobre o node pool
type azureNodePoolConfig struct {
	VMSize            string
	CurrentNodes      int
	MinNodes          int
	MaxNodes          int
	AutoscalerEnabled bool
}

// azureNodePoolShowResponse representa a resposta de `az aks nodepool show`
type azureNodePoolShowResponse struct {
	Name              string `json:"name"`
	VmSize            string `json:"vmSize"`
	Count             int    `json:"count"`
	MinCount          *int   `json:"minCount"`
	MaxCount          *int   `json:"maxCount"`
	EnableAutoScaling bool   `json:"enableAutoScaling"`
}

// ==============================================================================
// Coleta Principal
// ==============================================================================

// Collect executa a coleta completa de métricas para um node pool.
// Aplica degradação graceful quando Prometheus ou Azure API estiverem indisponíveis.
func (c *NodePoolCollector) Collect(ctx context.Context, req NodePoolPredictionRequest) (*NodePoolMetrics, error) {
	azureCluster := req.AzureCluster
	if azureCluster == "" {
		azureCluster = strings.TrimSuffix(req.Cluster, "-admin")
	}
	metrics := &NodePoolMetrics{
		NodePoolName: req.NodePoolName,
		Cluster:      req.Cluster,
		AzureCluster: azureCluster,
	}

	// ------------------------------------------------------------------
	// 2.2 – Resolver nodes do pool via K8s API
	// ------------------------------------------------------------------
	nodeNames, nodeInstances, resourceGroup, subscription, err := c.resolvePoolNodes(ctx, req.NodePoolName)
	if err != nil {
		return nil, fmt.Errorf("falha ao resolver nodes do pool %q: %w", req.NodePoolName, err)
	}
	if len(nodeNames) == 0 {
		return nil, fmt.Errorf("nenhum node encontrado no pool %q", req.NodePoolName)
	}

	instanceRegex := BuildInstanceRegex(nodeInstances)
	nameRegex := BuildNodeNameRegex(nodeNames)

	log.Info().
		Str("nodepool", req.NodePoolName).
		Int("nodes", len(nodeNames)).
		Msg("Nodes do pool resolvidos com sucesso")

	// ------------------------------------------------------------------
	// 2.3 – Dados Azure AKS (min/max, VM SKU, autoscaler)
	// Prioridade: ResourceGroup/Subscription do clusters-config.json (passado pelo handler)
	// Fallback: extração do providerID (dá o grupo MC_* de infra — ERRADO para az aks nodepool)
	// ------------------------------------------------------------------
	azRG := req.ResourceGroup
	azSub := req.Subscription
	azClusterName := req.AzureCluster
	if azRG == "" {
		azRG = resourceGroup // MC_* — vai falhar, mas tentamos mesmo assim
		log.Warn().
			Str("mc_resource_group", azRG).
			Msg("ResourceGroup não enriquecido pelo handler — usando MC_* do providerID (pode falhar)")
	}
	if azSub == "" {
		azSub = subscription
	}
	if azClusterName == "" {
		azClusterName = strings.TrimSuffix(req.Cluster, "-admin")
	}

	azConfig, azErr := c.collectAzureNodePoolConfig(req.NodePoolName, azClusterName, azRG, azSub)
	if azErr != nil {
		log.Warn().Err(azErr).Msg("Azure API indisponível — usando fallback K8s para node count")
		metrics.DataSources.AzureAPIAvailable = false
		metrics.CurrentNodes = len(nodeNames)
	} else {
		metrics.DataSources.AzureAPIAvailable = true
		metrics.VMSize = azConfig.VMSize
		metrics.CurrentNodes = azConfig.CurrentNodes
		metrics.MinNodes = azConfig.MinNodes
		metrics.MaxNodes = azConfig.MaxNodes
		metrics.AutoscalerEnabled = azConfig.AutoscalerEnabled

		log.Info().
			Str("vm_size", azConfig.VMSize).
			Int("current", azConfig.CurrentNodes).
			Int("min", azConfig.MinNodes).
			Int("max", azConfig.MaxNodes).
			Bool("autoscaler", azConfig.AutoscalerEnabled).
			Msg("Config Azure AKS coletada")
	}

	// ------------------------------------------------------------------
	// Verificar disponibilidade do Prometheus (2.9 – degradação graceful)
	// ------------------------------------------------------------------
	prometheusOK := c.promClient != nil && c.checkPrometheusAvailable(ctx)
	metrics.DataSources.PrometheusAvailable = prometheusOK
	metrics.DataSources.NodeExporterAvailable = prometheusOK && len(nodeInstances) > 0

	if !prometheusOK {
		log.Warn().Msg("Prometheus indisponível — análise restrita ao estado atual via K8s API")
		metrics.DataSources.LimitationNote = "Prometheus indisponível: histórico de tendências e conntrack não disponíveis"
	}

	// ------------------------------------------------------------------
	// 2.4 – Snapshot atual de cada node
	// ------------------------------------------------------------------
	snapshots, snapshotErr := c.collectCurrentSnapshot(ctx, nodeNames, instanceRegex, nameRegex, prometheusOK)
	if snapshotErr != nil {
		log.Warn().Err(snapshotErr).Msg("Snapshot dos nodes parcialmente coletado")
	}
	metrics.NodesSnapshot = snapshots
	metrics.DataSources.MetricsServerAvailable = len(snapshots) > 0

	// ------------------------------------------------------------------
	// 2.5 – Histórico D-3, D-7, D-14 via Prometheus offset
	// ------------------------------------------------------------------
	if prometheusOK {
		cpuTrend, memTrend, podsTrend, trendErr := c.collectTrends(ctx, instanceRegex, nameRegex, len(nodeNames))
		if trendErr != nil {
			log.Warn().Err(trendErr).Msg("Tendências históricas parcialmente coletadas")
		}
		metrics.CPUTrendPerNode = cpuTrend
		metrics.MemTrendPerNode = memTrend
		metrics.PodsTrendPerNode = podsTrend
		metrics.DataSources.HistoryDepthDays = 14
	} else {
		metrics.DataSources.HistoryDepthDays = 0
		// D-0 sintético: sem Prometheus não há histórico, mas ao menos
		// populamos o ponto atual para que calculateTrends() produza
		// CPUTrend/MemTrend com valor real (em vez de zero-strings).
		if len(snapshots) > 0 {
			now := time.Now()
			nodeCount := len(snapshots)
			var cpuSum, memSum, podsSum float64
			for _, s := range snapshots {
				cpuSum += s.CPUUsagePercent
				memSum += s.MemUsagePercent
				podsSum += float64(s.PodCount)
			}
			avgCPU := cpuSum / float64(nodeCount)
			avgMem := memSum / float64(nodeCount)
			avgPods := podsSum / float64(nodeCount)

			metrics.CPUTrendPerNode = []TrendSnapshot{{DaysAgo: 0, Timestamp: now, ValuePerNode: avgCPU, NodeCountAtTime: nodeCount, Unit: "%"}}
			metrics.MemTrendPerNode = []TrendSnapshot{{DaysAgo: 0, Timestamp: now, ValuePerNode: avgMem, NodeCountAtTime: nodeCount, Unit: "%"}}
			metrics.PodsTrendPerNode = []TrendSnapshot{{DaysAgo: 0, Timestamp: now, ValuePerNode: avgPods, NodeCountAtTime: nodeCount, Unit: "pods"}}

			log.Info().
				Float64("avg_cpu_pct", avgCPU).
				Float64("avg_mem_pct", avgMem).
				Float64("avg_pods", avgPods).
				Int("node_count", nodeCount).
				Msg("D-0 sintetico criado para tendencias (Prometheus indisponivel)")
		}
	}

	// ------------------------------------------------------------------
	// 2.6 – conntrack: pool-filtered (primário) + cluster-wide (contexto)
	// ------------------------------------------------------------------
	if prometheusOK && instanceRegex != "" {
		poolAnalysis, ctErr := c.collectConntrackPool(ctx, instanceRegex)
		if ctErr != nil {
			log.Warn().Err(ctErr).Msg("conntrack do pool indisponível")
			poolAnalysis.HasSufficientData = false
		}
		metrics.ConntrackPool = poolAnalysis
		metrics.ConntrackPerNode = poolAnalysis.Nodes

		clusterCtx, clCtErr := c.collectConntrackCluster(ctx)
		if clCtErr != nil {
			log.Warn().Err(clCtErr).Msg("conntrack cluster-wide indisponível")
		}
		metrics.ConntrackCluster = clusterCtx
	}

	// ------------------------------------------------------------------
	// 2.7 – Eventos do Cluster Autoscaler via K8s Events API
	// ------------------------------------------------------------------
	events, evErr := c.collectAutoscalerEvents(ctx, req.NodePoolName)
	if evErr != nil {
		log.Warn().Err(evErr).Msg("Eventos do autoscaler indisponíveis")
	}
	metrics.AutoscalerEvents = events

	// ------------------------------------------------------------------
	// 2.8 – BinPacking analysis (fragmentação do pool)
	// ------------------------------------------------------------------
	metrics.BinPacking = c.calculateBinPacking(snapshots)

	// Nodes com pressão ativa (derivado do snapshot)
	metrics.NodesWithPressure = c.extractNodePressures(snapshots)

	return metrics, nil
}

// ==============================================================================
// 2.2 – Resolver nodes do pool
// ==============================================================================

// resolvePoolNodes retorna nomes dos nodes, instâncias do node_exporter (IP:9100),
// resource group e subscription extraídos do providerID dos nodes.
func (c *NodePoolCollector) resolvePoolNodes(ctx context.Context, nodePoolName string) (
	nodeNames []string,
	nodeInstances []string,
	resourceGroup string,
	subscription string,
	err error,
) {
	clientset := c.kubeClient.GetClientset()

	// Tentar primeiro com label "agentpool", fallback para "kubernetes.azure.com/agentpool"
	labelSelectors := []string{
		fmt.Sprintf("agentpool=%s", nodePoolName),
		fmt.Sprintf("kubernetes.azure.com/agentpool=%s", nodePoolName),
	}

	var nodes *corev1.NodeList
	for _, selector := range labelSelectors {
		nodes, err = clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err == nil && len(nodes.Items) > 0 {
			break
		}
	}

	if err != nil {
		return nil, nil, "", "", fmt.Errorf("falha ao listar nodes do pool %s: %w", nodePoolName, err)
	}
	if nodes == nil || len(nodes.Items) == 0 {
		return nil, nil, "", "", fmt.Errorf("nenhum node encontrado com label agentpool=%s", nodePoolName)
	}

	nodeNames = make([]string, 0, len(nodes.Items))
	nodeInstances = make([]string, 0, len(nodes.Items))

	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)

		// Extrair IP interno para construir instância do node_exporter (<IP>:9100)
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP && addr.Address != "" {
				nodeInstances = append(nodeInstances, fmt.Sprintf("%s:9100", addr.Address))
				break
			}
		}

		// Extrair resource group e subscription do providerID (extrair uma vez)
		if resourceGroup == "" && node.Spec.ProviderID != "" {
			resourceGroup, subscription = extractAzureInfoFromProviderID(node.Spec.ProviderID)
		}
	}

	log.Debug().
		Strs("nodes", nodeNames).
		Str("resource_group", resourceGroup).
		Msg("Pool nodes resolvidos")

	return nodeNames, nodeInstances, resourceGroup, subscription, nil
}

// extractAzureInfoFromProviderID extrai resource group e subscription do ProviderID do node.
// Formato: azure:///subscriptions/<sub>/resourceGroups/<rg>/providers/...
func extractAzureInfoFromProviderID(providerID string) (resourceGroup, subscription string) {
	path := strings.TrimPrefix(providerID, "azure:///")
	path = strings.TrimPrefix(path, "azure://")

	subRe := regexp.MustCompile(`(?i)subscriptions/([^/]+)`)
	rgRe := regexp.MustCompile(`(?i)resourceGroups/([^/]+)`)

	if m := subRe.FindStringSubmatch(path); len(m) > 1 {
		subscription = m[1]
	}
	if m := rgRe.FindStringSubmatch(path); len(m) > 1 {
		resourceGroup = m[1]
	}
	return
}

// ==============================================================================
// 2.3 – Dados Azure AKS
// ==============================================================================

// collectAzureNodePoolConfig busca configuração do node pool via Azure CLI.
// collectAzureNodePoolConfig obtém min/max/autoscaler do cluster AKS via Azure CLI.
// clusterName deve ser o nome real do cluster AKS (sem sufixo -admin).
// resourceGroup deve ser o resource group onde o cluster AKS está definido (não o MC_* de infra).
func (c *NodePoolCollector) collectAzureNodePoolConfig(nodePoolName, clusterName, resourceGroup, subscription string) (*azureNodePoolConfig, error) {
	// Garantir que o nome do cluster não tem -admin (segurança extra)
	azClusterName := strings.TrimSuffix(clusterName, "-admin")

	args := []string{
		"aks", "nodepool", "show",
		"--name", nodePoolName,
		"--cluster-name", azClusterName,
		"--output", "json",
	}
	if resourceGroup != "" {
		args = append(args, "--resource-group", resourceGroup)
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	log.Debug().
		Str("nodepool", nodePoolName).
		Str("cluster", azClusterName).
		Str("resource_group", resourceGroup).
		Str("subscription", subscription).
		Msg("Chamando az aks nodepool show")

	cmd := exec.Command("az", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		log.Error().
			Str("nodepool", nodePoolName).
			Str("cluster", azClusterName).
			Str("resource_group", resourceGroup).
			Str("subscription", subscription).
			Str("stderr", stderrStr).
			Err(err).
			Msg("az aks nodepool show falhou")
		return nil, fmt.Errorf("az aks nodepool show falhou: %w — stderr: %s", err, stderrStr)
	}

	var pool azureNodePoolShowResponse
	if err := json.Unmarshal(output, &pool); err != nil {
		return nil, fmt.Errorf("falha ao parsear resposta az aks nodepool show: %w", err)
	}

	cfg := &azureNodePoolConfig{
		VMSize:            pool.VmSize,
		CurrentNodes:      pool.Count,
		AutoscalerEnabled: pool.EnableAutoScaling,
	}
	if pool.MinCount != nil {
		cfg.MinNodes = *pool.MinCount
	}
	if pool.MaxCount != nil {
		cfg.MaxNodes = *pool.MaxCount
	}

	return cfg, nil
}

// ==============================================================================
// Prometheus: verificação de disponibilidade
// ==============================================================================

func (c *NodePoolCollector) checkPrometheusAvailable(ctx context.Context) bool {
	if c.promClient == nil {
		return false
	}
	result, err := c.promClient.Query(ctx, "up")
	return err == nil && result != nil
}

// ==============================================================================
// 2.4 – Snapshot atual de cada node
// ==============================================================================

// collectCurrentSnapshot coleta o estado atual de cada node em paralelo.
func (c *NodePoolCollector) collectCurrentSnapshot(
	ctx context.Context,
	nodeNames []string,
	instanceRegex, nameRegex string,
	prometheusOK bool,
) ([]NodePoolNodeSnapshot, error) {
	// Pré-carregar métricas do Prometheus (evita N queries individuais)
	var cpuByInstance map[string]float64
	var memByInstance map[string]float64
	var diskByInstance map[string]float64
	var pidByInstance map[string]float64
	var conntrackPctByInstance map[string]float64
	var podCountByNode map[string]int

	if prometheusOK && instanceRegex != "" {
		cpuByInstance = c.queryVectorByInstance(ctx, c.queries.GetNodeCPUUsageQuery(instanceRegex, 0))
		memByInstance = c.queryVectorByInstance(ctx, c.queries.GetNodeMemUsagePercentQuery(instanceRegex, 0))
		diskByInstance = c.queryVectorByInstance(ctx, c.queries.GetNodeDiskUsagePercentQuery(instanceRegex))
		pidByInstance = c.queryVectorByInstance(ctx, c.queries.GetNodePIDCountQuery(instanceRegex))

		// conntrack %: entries / limit * 100
		entriesMap := c.queryVectorByInstance(ctx, c.queries.GetConntrackEntriesQuery(instanceRegex))
		limitsMap := c.queryVectorByInstance(ctx, c.queries.GetConntrackLimitQuery(instanceRegex))
		conntrackPctByInstance = make(map[string]float64, len(entriesMap))
		for inst, entries := range entriesMap {
			limit, ok := limitsMap[inst]
			if !ok || limit == 0 {
				limit = 131072 // padrão Linux
			}
			conntrackPctByInstance[inst] = entries / limit * 100.0
		}
	}

	if prometheusOK && nameRegex != "" {
		podCountByNode = c.queryVectorByNode(ctx, c.queries.GetPodCountPerNodeQuery(nameRegex))
	}

	// Coletar snapshots em paralelo
	snapshots := make([]NodePoolNodeSnapshot, 0, len(nodeNames))
	var mu sync.Mutex
	var wg sync.WaitGroup
	clientset := c.kubeClient.GetClientset()

	for _, nodeName := range nodeNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			node, nodeErr := clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
			if nodeErr != nil {
				log.Warn().Err(nodeErr).Str("node", name).Msg("Falha ao obter node")
				return
			}

			snap := buildNodeSnapshotFromK8s(node)

			// Métricas do Prometheus (quando disponível)
			inst := snap.PrometheusInstance
			if v, ok := cpuByInstance[inst]; ok {
				// CPU query retorna valor 0-1 (fração), converter para %
				snap.CPUUsagePercent = clamp(v*100.0, 0, 100)
			}
			if v, ok := memByInstance[inst]; ok {
				// Mem query retorna 0-1 (fração), converter para %
				snap.MemUsagePercent = clamp(v*100.0, 0, 100)
			}
			if v, ok := diskByInstance[inst]; ok {
				// Disk query retorna 0-1 (fração), converter para %
				snap.DiskUsagePercent = clamp(v*100.0, 0, 100)
			}
			if v, ok := pidByInstance[inst]; ok {
				snap.PIDCount = int(v)
			}
			if v, ok := conntrackPctByInstance[inst]; ok {
				snap.ConntrackPercent = clamp(v, 0, 100)
			}

			// Fallback: Metrics Server quando Prometheus indisponível ou retornou 0 para CPU/Mem
			if snap.CPUUsagePercent == 0 || snap.MemUsagePercent == 0 {
				if cpuMillis, memBytes, msErr := c.kubeClient.GetNodeRawMetrics(ctx, name); msErr == nil {
					if snap.CPUUsagePercent == 0 && snap.CPUCapacityCores > 0 {
						allocMillis := snap.CPUCapacityCores * 1000.0
						snap.CPUUsagePercent = clamp(float64(cpuMillis)/allocMillis*100.0, 0, 100)
					}
					if snap.MemUsagePercent == 0 && snap.MemCapacityGB > 0 {
						allocBytes := snap.MemCapacityGB * 1024.0 * 1024.0 * 1024.0
						snap.MemUsagePercent = clamp(float64(memBytes)/allocBytes*100.0, 0, 100)
					}
					log.Debug().
						Str("node", name).
						Float64("cpu_pct", snap.CPUUsagePercent).
						Float64("mem_pct", snap.MemUsagePercent).
						Msg("Métricas do node obtidas via Metrics Server (fallback)")
				} else {
					log.Warn().Str("node", name).Err(msErr).Msg("Metrics Server indisponível — métricas de uso serão 0%")
				}
			}

			// Sempre listar pods rodando no node para:
			// 1. Pod count (fallback se Prometheus indisponível)
			// 2. Calcular resource requests (allocation analysis — SEMPRE necessário)
			pods, podErr := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", node.Name),
			})

			// Pod count: Prometheus tem precedência (mais preciso), K8s como fallback
			if podCount, ok := podCountByNode[node.Name]; ok {
				snap.PodCount = podCount
			} else if podErr == nil {
				snap.PodCount = len(pods.Items)
			}

			// Resource requests: somar CPU e memória solicitados por todos os containers
			if podErr == nil {
				var cpuReqCores, memReqGB float64
				for _, pod := range pods.Items {
					for _, container := range pod.Spec.Containers {
						if req := container.Resources.Requests; req != nil {
							if cpuQty, ok := req[corev1.ResourceCPU]; ok {
								cpuReqCores += float64(cpuQty.MilliValue()) / 1000.0
							}
							if memQty, ok := req[corev1.ResourceMemory]; ok {
								memReqGB += float64(memQty.Value()) / (1024.0 * 1024.0 * 1024.0)
							}
						}
					}
				}
				snap.CPURequestedCores = cpuReqCores
				snap.MemRequestedGB = memReqGB
				// Percentual em relação ao alocável (pode ultrapassar 100% em over-commit)
				if snap.CPUCapacityCores > 0 {
					snap.CPURequestedPercent = clamp(cpuReqCores/snap.CPUCapacityCores*100.0, 0, 300)
				}
				if snap.MemCapacityGB > 0 {
					snap.MemRequestedPercent = clamp(memReqGB/snap.MemCapacityGB*100.0, 0, 300)
				}
				log.Debug().
					Str("node", name).
					Float64("cpu_req_cores", cpuReqCores).
					Float64("mem_req_gb", memReqGB).
					Float64("cpu_req_pct", snap.CPURequestedPercent).
					Float64("mem_req_pct", snap.MemRequestedPercent).
					Msg("Resource requests coletados via K8s API")
			} else {
				log.Warn().Str("node", name).Err(podErr).Msg("Falha ao listar pods para resource requests")
			}

			// Pod density %
			if snap.PodCapacity > 0 {
				snap.PodDensityPercent = float64(snap.PodCount) / float64(snap.PodCapacity) * 100.0
			}

			mu.Lock()
			snapshots = append(snapshots, snap)
			mu.Unlock()
		}(nodeName)
	}

	wg.Wait()

	// Ordenar por nome para saída determinística
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].NodeName < snapshots[j].NodeName
	})

	return snapshots, nil
}

// buildNodeSnapshotFromK8s extrai dados estáticos (sem Prometheus) de um node K8s.
func buildNodeSnapshotFromK8s(node *corev1.Node) NodePoolNodeSnapshot {
	snap := NodePoolNodeSnapshot{
		NodeName:        node.Name,
		IsUnschedulable: node.Spec.Unschedulable,
		Status:          nodeReadyStatus(node),
		Age:             formatAge(node.CreationTimestamp.Time),
	}

	// IP interno para lookup Prometheus
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP && addr.Address != "" {
			snap.PrometheusInstance = fmt.Sprintf("%s:9100", addr.Address)
			break
		}
	}

	// Capacidade de CPU (cores)
	if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
		snap.CPUCapacityCores = float64(cpu.MilliValue()) / 1000.0
	}

	// Capacidade de Memória (GB)
	if mem, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
		snap.MemCapacityGB = float64(mem.Value()) / (1024 * 1024 * 1024)
	}

	// Pod capacity
	if pods, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
		snap.PodCapacity = int(pods.Value())
	}

	// Condições ativas (pressões)
	for _, cond := range node.Status.Conditions {
		if cond.Status == corev1.ConditionTrue && cond.Type != corev1.NodeReady {
			snap.ActiveConditions = append(snap.ActiveConditions, string(cond.Type))
		}
	}

	return snap
}

// ==============================================================================
// 2.5 – Histórico D-3, D-7, D-14
// ==============================================================================

// collectTrends coleta snapshots históricos normalizados por node count.
// IMPORTANTE: usa média por node (ValuePerNode), não soma total, para garantir
// comparabilidade entre períodos com node counts diferentes (pool escalou/descalou).
func (c *NodePoolCollector) collectTrends(
	ctx context.Context,
	instanceRegex, nameRegex string,
	currentNodeCount int,
) (cpuTrend, memTrend, podsTrend []TrendSnapshot, err error) {
	now := time.Now()

	// Snapshot atual (D-0)
	cpuNow := c.avgByInstance(ctx, c.queries.GetNodeCPUUsageQuery(instanceRegex, 0)) * 100.0
	memNow := c.avgByInstance(ctx, c.queries.GetNodeMemUsagePercentQuery(instanceRegex, 0)) * 100.0
	podsNow := c.avgByNodeFloat(ctx, c.queries.GetPodCountPerNodeQuery(nameRegex))

	cpuTrend = append(cpuTrend, TrendSnapshot{DaysAgo: 0, Timestamp: now, ValuePerNode: cpuNow, NodeCountAtTime: currentNodeCount, Unit: "%"})
	memTrend = append(memTrend, TrendSnapshot{DaysAgo: 0, Timestamp: now, ValuePerNode: memNow, NodeCountAtTime: currentNodeCount, Unit: "%"})
	podsTrend = append(podsTrend, TrendSnapshot{DaysAgo: 0, Timestamp: now, ValuePerNode: podsNow, NodeCountAtTime: currentNodeCount, Unit: "pods"})

	// Snapshots históricos
	offsets := DayOffsets()
	dayMapping := []struct {
		label   string
		daysAgo int
	}{
		{"D-3", 3},
		{"D-7", 7},
		{"D-14", 14},
	}

	for _, day := range dayMapping {
		offset, ok := offsets[day.label]
		if !ok {
			continue
		}
		ts := now.Add(-offset)

		cpuAvg := c.avgByInstance(ctx, c.queries.GetNodeCPUUsageQuery(instanceRegex, offset)) * 100.0
		memAvg := c.avgByInstance(ctx, c.queries.GetNodeMemUsagePercentQuery(instanceRegex, offset)) * 100.0
		podsAvg := c.avgByNodeFloat(ctx, c.queries.GetPodCountPerNodeWithOffsetQuery(nameRegex, offset))

		// Estimar node count no período histórico (count de instâncias com dados)
		nodeCountAtTime := c.estimateNodeCountAtTime(ctx, instanceRegex, offset)
		if nodeCountAtTime == 0 {
			nodeCountAtTime = currentNodeCount
		}

		cpuTrend = append(cpuTrend, TrendSnapshot{DaysAgo: day.daysAgo, Timestamp: ts, ValuePerNode: cpuAvg, NodeCountAtTime: nodeCountAtTime, Unit: "%"})
		memTrend = append(memTrend, TrendSnapshot{DaysAgo: day.daysAgo, Timestamp: ts, ValuePerNode: memAvg, NodeCountAtTime: nodeCountAtTime, Unit: "%"})
		podsTrend = append(podsTrend, TrendSnapshot{DaysAgo: day.daysAgo, Timestamp: ts, ValuePerNode: podsAvg, NodeCountAtTime: nodeCountAtTime, Unit: "pods"})
	}

	log.Info().
		Int("snapshots", len(cpuTrend)).
		Msg("Tendências históricas coletadas")

	return cpuTrend, memTrend, podsTrend, nil
}

// estimateNodeCountAtTime conta instâncias que retornaram dados no período histórico.
func (c *NodePoolCollector) estimateNodeCountAtTime(ctx context.Context, instanceRegex string, offset time.Duration) int {
	byInst := c.queryVectorByInstance(ctx, c.queries.GetNodeCPUUsageQuery(instanceRegex, offset))
	return len(byInst)
}

// ==============================================================================
// 2.6 – conntrack
// ==============================================================================

// collectConntrackPool coleta análise de conntrack filtrada aos nodes do pool (primário).
func (c *NodePoolCollector) collectConntrackPool(ctx context.Context, instanceRegex string) (ConntrackPoolAnalysis, error) {
	entriesResult, err := c.promClient.Query(ctx, c.queries.GetConntrackEntriesQuery(instanceRegex))
	if err != nil {
		return ConntrackPoolAnalysis{HasSufficientData: false}, fmt.Errorf("conntrack entries: %w", err)
	}

	entriesVec, ok := entriesResult.(model.Vector)
	if !ok || len(entriesVec) == 0 {
		return ConntrackPoolAnalysis{HasSufficientData: false}, nil
	}

	// Limite por node
	limitResult, _ := c.promClient.Query(ctx, c.queries.GetConntrackLimitQuery(instanceRegex))
	limitMap := make(map[string]int64)
	if limitVec, ok := limitResult.(model.Vector); ok {
		for _, s := range limitVec {
			limitMap[string(s.Metric["instance"])] = int64(s.Value)
		}
	}

	// Mapa IP → nome do node via kube_node_info
	ipToName := c.buildIPToNodeNameMap(ctx)
	extractIP := func(inst string) string {
		for i := len(inst) - 1; i >= 0; i-- {
			if inst[i] == ':' {
				return inst[:i]
			}
		}
		return inst
	}

	var nodeInfos []ConntrackNodeInfo
	var poolTotal, poolMaxTotal int64
	var nodesWarning, nodesCritical int
	var highestNode string
	var maxUsage float64

	for _, s := range entriesVec {
		inst := string(s.Metric["instance"])
		current := int64(s.Value)
		limit := limitMap[inst]
		if limit == 0 {
			limit = 131072 // padrão Linux
		}

		pct := float64(current) / float64(limit) * 100.0
		status := ConntrackStatusFromPercent(pct)

		switch status {
		case "warning":
			nodesWarning++
		case "critical", "emergency":
			nodesCritical++
		}

		nodeName := ipToName[extractIP(inst)]
		if nodeName == "" {
			nodeName = extractIP(inst)
		}

		nodeInfos = append(nodeInfos, ConntrackNodeInfo{
			NodeName:       nodeName,
			Instance:       inst,
			CurrentEntries: current,
			MaxEntries:     limit,
			UsagePercent:   pct,
			Status:         status,
		})

		poolTotal += current
		poolMaxTotal += limit

		if pct > maxUsage {
			maxUsage = pct
			highestNode = nodeName
		}
	}

	avgUsage := 0.0
	if poolMaxTotal > 0 {
		avgUsage = float64(poolTotal) / float64(poolMaxTotal) * 100.0
	}

	// Taxa de crescimento de conntrack por hora (entries/hora, somado de todos os nodes do pool).
	// rate()[10m] retorna entries/segundo → *60 = entries/minuto → *60 = entries/hora.
	var avgGrowthRatePerH float64
	growthResult, _ := c.promClient.Query(ctx, c.queries.GetConntrackGrowthRateQuery(instanceRegex))
	if growthVec, ok := growthResult.(model.Vector); ok && len(growthVec) > 0 {
		var sumRatePerMin float64
		for _, s := range growthVec {
			sumRatePerMin += float64(s.Value)
		}
		avgGrowthRatePerH = sumRatePerMin * 60.0 // entries/minuto → entries/hora
	}

	analysis := ConntrackPoolAnalysis{
		Nodes:             nodeInfos,
		TotalEntries:      poolTotal,
		TotalLimit:        poolMaxTotal,
		AvgUsage:          avgUsage,
		MaxUsage:          maxUsage,
		HighestNode:       highestNode,
		NodesWarning:      nodesWarning,
		NodesCritical:     nodesCritical,
		AvgGrowthRatePerH: avgGrowthRatePerH,
		HasSufficientData: true,
		MetricSource:      "node_exporter",
	}

	log.Info().
		Float64("avg_usage_pct", avgUsage).
		Float64("max_usage_pct", maxUsage).
		Str("highest_node", highestNode).
		Int("nodes_warning", nodesWarning).
		Int("nodes_critical", nodesCritical).
		Msg("conntrack do pool coletado")

	return analysis, nil
}

// collectConntrackCluster coleta contexto de conntrack cluster-wide (sem filtro de pool).
func (c *NodePoolCollector) collectConntrackCluster(ctx context.Context) (ConntrackClusterContext, error) {
	entriesResult, err := c.promClient.Query(ctx, c.queries.GetConntrackEntriesClusterQuery())
	if err != nil {
		return ConntrackClusterContext{}, fmt.Errorf("conntrack cluster entries: %w", err)
	}

	entriesVec, ok := entriesResult.(model.Vector)
	if !ok || len(entriesVec) == 0 {
		return ConntrackClusterContext{HasSufficientData: false}, nil
	}

	limitResult, _ := c.promClient.Query(ctx, c.queries.GetConntrackLimitClusterQuery())
	limitMap := make(map[string]int64)
	if limitVec, ok := limitResult.(model.Vector); ok {
		for _, s := range limitVec {
			limitMap[string(s.Metric["instance"])] = int64(s.Value)
		}
	}

	var clusterTotal, clusterMax int64
	nodesAbove70, nodesAbove85 := 0, 0
	var maxUsage float64

	for _, s := range entriesVec {
		inst := string(s.Metric["instance"])
		current := int64(s.Value)
		limit := limitMap[inst]
		if limit == 0 {
			limit = 131072
		}
		pct := float64(current) / float64(limit) * 100.0
		clusterTotal += current
		clusterMax += limit
		if pct > 70 {
			nodesAbove70++
		}
		if pct > 85 {
			nodesAbove85++
		}
		if pct > maxUsage {
			maxUsage = pct
		}
	}

	avgUsage := 0.0
	if clusterMax > 0 {
		avgUsage = float64(clusterTotal) / float64(clusterMax) * 100.0
	}

	return ConntrackClusterContext{
		TotalEntries:      clusterTotal,
		TotalLimit:        clusterMax,
		AvgUsage:          avgUsage,
		MaxUsage:          maxUsage,
		TotalNodes:        len(entriesVec),
		NodesWarning:      nodesAbove70,
		NodesCritical:     nodesAbove85,
		HasSufficientData: true,
		MetricSource:      "node_exporter",
	}, nil
}

// buildIPToNodeNameMap constrói mapa IP → nome do node via kube_node_info.
func (c *NodePoolCollector) buildIPToNodeNameMap(ctx context.Context) map[string]string {
	result, err := c.promClient.Query(ctx, `kube_node_info`)
	if err != nil {
		return make(map[string]string)
	}
	m := make(map[string]string)
	if vec, ok := result.(model.Vector); ok {
		for _, s := range vec {
			nodeName := string(s.Metric["node"])
			internalIP := string(s.Metric["internal_ip"])
			if nodeName != "" && internalIP != "" {
				m[internalIP] = nodeName
			}
		}
	}
	return m
}

// ==============================================================================
// 2.7 – Eventos do Cluster Autoscaler
// ==============================================================================

// collectAutoscalerEvents coleta eventos do Cluster Autoscaler para o pool específico.
func (c *NodePoolCollector) collectAutoscalerEvents(ctx context.Context, nodePoolName string) ([]AutoscalerEvent, error) {
	clientset := c.kubeClient.GetClientset()

	// Buscar eventos com source cluster-autoscaler (namespace kube-system é o padrão)
	events, err := clientset.CoreV1().Events("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		// Tentar cluster-wide
		events, err = clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("falha ao buscar eventos: %w", err)
		}
	}

	var autoscalerEvents []AutoscalerEvent
	cutoff := time.Now().Add(-7 * 24 * time.Hour)

	for _, ev := range events.Items {
		// Filtrar por source component
		if ev.Source.Component != "cluster-autoscaler" {
			continue
		}

		// Filtrar por relevância de reason
		if !isAutoscalerEvent(ev.Reason) {
			continue
		}

		// Filtrar por tempo (últimos 7 dias)
		eventTime := ev.LastTimestamp.Time
		if eventTime.IsZero() {
			eventTime = ev.CreationTimestamp.Time
		}
		if eventTime.Before(cutoff) {
			continue
		}

		// Filtrar por node pool quando possível identificar na mensagem
		objName := ev.InvolvedObject.Name
		if nodePoolName != "" &&
			!strings.Contains(strings.ToLower(objName), strings.ToLower(nodePoolName)) &&
			!strings.Contains(strings.ToLower(ev.Message), strings.ToLower(nodePoolName)) {
			continue
		}

		delta := 0
		eventType := classifyAutoscalerEvent(ev.Reason)
		if strings.Contains(eventType, "up") {
			delta = int(ev.Count)
		} else if strings.Contains(eventType, "down") {
			delta = -int(ev.Count)
		}

		autoscalerEvents = append(autoscalerEvents, AutoscalerEvent{
			Timestamp:  eventTime,
			Type:       eventType,
			Reason:     ev.Reason,
			Message:    ev.Message,
			NodesDelta: delta,
			Source:     "k8s_events",
		})
	}

	// Ordenar mais recentes primeiro
	sort.Slice(autoscalerEvents, func(i, j int) bool {
		return autoscalerEvents[i].Timestamp.After(autoscalerEvents[j].Timestamp)
	})

	// Limitar a 20 eventos mais recentes
	if len(autoscalerEvents) > 20 {
		autoscalerEvents = autoscalerEvents[:20]
	}

	log.Info().
		Int("events", len(autoscalerEvents)).
		Str("nodepool", nodePoolName).
		Msg("Eventos do Cluster Autoscaler coletados")

	return autoscalerEvents, nil
}

func isAutoscalerEvent(reason string) bool {
	relevant := []string{
		"TriggeredScaleUp", "ScaleUp", "ScaleDown",
		"ScaleDownEmpty", "NotTriggeringScaleUp",
		"FailedScaleDown", "FailedScaleUp",
	}
	for _, r := range relevant {
		if strings.EqualFold(reason, r) {
			return true
		}
	}
	return false
}

func classifyAutoscalerEvent(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "fail") && strings.Contains(lower, "up"):
		return "scale-up-failed"
	case strings.Contains(lower, "fail") && strings.Contains(lower, "down"):
		return "scale-down-failed"
	case strings.Contains(lower, "up"):
		return "scale-up"
	case strings.Contains(lower, "down"):
		return "scale-down"
	default:
		return "info"
	}
}

// ==============================================================================
// 2.8 – BinPacking Analysis (fragmentação)
// ==============================================================================

// calculateBinPacking analisa a eficiência de empacotamento do pool.
// Fragmentação alta = nodes com recursos ociosos que poderiam ser consolidados.
// Usa tanto o uso real (via Metrics Server) quanto os requests dos pods (via K8s API)
// para uma análise mais precisa — nodes com requests altos mas uso baixo não são
// candidatos reais a scale-in.
func (c *NodePoolCollector) calculateBinPacking(snapshots []NodePoolNodeSnapshot) BinPackingAnalysis {
	if len(snapshots) == 0 {
		return BinPackingAnalysis{}
	}

	schedulable := 0
	var cpuSum, memSum, podSum float64
	var cpuReqSum, memReqSum float64
	hasAllocationData := false
	overProvisionedNodes := 0 // requests >> uso real (gap > 30pp)

	for _, s := range snapshots {
		if s.IsUnschedulable {
			continue
		}
		schedulable++
		cpuSum += s.CPUUsagePercent
		memSum += s.MemUsagePercent
		if s.PodCapacity > 0 {
			podSum += s.PodDensityPercent
		}
		// Dados de allocation (requests)
		if s.CPURequestedPercent > 0 || s.MemRequestedPercent > 0 {
			hasAllocationData = true
			cpuReqSum += s.CPURequestedPercent
			memReqSum += s.MemRequestedPercent
			// Over-provisioning: requests muito maiores que uso real
			if s.CPURequestedPercent-s.CPUUsagePercent > 30 {
				overProvisionedNodes++
			}
		}
	}

	if schedulable == 0 {
		return BinPackingAnalysis{}
	}

	cpuEff := cpuSum / float64(schedulable)    // uso real médio
	memEff := memSum / float64(schedulable)
	podEff := podSum / float64(schedulable)

	// Eficiência de allocation (requests vs capacidade) — disponível quando K8s API acessível
	cpuAllocEff := 0.0
	memAllocEff := 0.0
	if hasAllocationData {
		cpuAllocEff = cpuReqSum / float64(schedulable)
		memAllocEff = memReqSum / float64(schedulable)
	}

	// Fragmentação: considera o maior entre uso real e requests alocados.
	// Um node com 15% CPU usado mas 85% requestado está "cheio" para o scheduler.
	effectiveCPU := math.Max(cpuEff, cpuAllocEff)
	effectiveMem := math.Max(memEff, memAllocEff)
	avgIdlePct := 100 - (effectiveCPU+effectiveMem)/2.0
	fragLevel := "low"
	if avgIdlePct > 50 {
		fragLevel = "high"
	} else if avgIdlePct > 30 {
		fragLevel = "medium"
	}

	// Scale-in candidatos: uso real < 30% E requests < 40% (genuinamente vazio)
	// Nodes com requests altos mas uso baixo NÃO são candidatos seguros
	scaleInCandidates := 0
	for _, s := range snapshots {
		if s.IsUnschedulable {
			continue
		}
		cpuReqOk := !hasAllocationData || s.CPURequestedPercent < 40
		memReqOk := !hasAllocationData || s.MemRequestedPercent < 40
		if s.CPUUsagePercent < 30 && s.MemUsagePercent < 30 && cpuReqOk && memReqOk {
			scaleInCandidates++
		}
	}
	if scaleInCandidates >= schedulable {
		scaleInCandidates = schedulable - 1 // manter pelo menos 1
	}

	scaleInSafe := scaleInCandidates > 0 && avgIdlePct > 40
	scaleInReason := ""
	if scaleInSafe {
		scaleInReason = fmt.Sprintf("%d node(s) com uso real e requests abaixo de 30-40%%", scaleInCandidates)
	}

	rebalancingNeeded := fragLevel == "high" && effectiveCPU < 60

	// Estimativa de recursos desperdiçados (apenas nodes genuinamente vazios)
	wastedCPU := 0.0
	wastedMemGB := 0.0
	for _, s := range snapshots {
		if s.IsUnschedulable {
			continue
		}
		cpuReqOk := !hasAllocationData || s.CPURequestedPercent < 40
		memReqOk := !hasAllocationData || s.MemRequestedPercent < 40
		if s.CPUUsagePercent < 30 && s.MemUsagePercent < 30 && cpuReqOk && memReqOk {
			wastedCPU += s.CPUCapacityCores * (1 - s.CPUUsagePercent/100)
			wastedMemGB += s.MemCapacityGB * (1 - s.MemUsagePercent/100)
		}
	}
	wastedResources := ""
	if wastedCPU > 0 || wastedMemGB > 0 {
		wastedResources = fmt.Sprintf("~%.1f cores, ~%.1fGB RAM subutilizados", wastedCPU, wastedMemGB)
	}
	// Over-provisioning: diferença entre requests e uso real
	if hasAllocationData && overProvisionedNodes > 0 {
		note := fmt.Sprintf(
			"; %d node(s) com requests >> uso real (>30pp de gap) — pods super-provisionados limitam capacidade disponivel",
			overProvisionedNodes,
		)
		wastedResources += note
	}

	return BinPackingAnalysis{
		CPUEfficiency:      cpuEff,
		MemEfficiency:      memEff,
		PodEfficiency:      podEff,
		FragmentationLevel: fragLevel,
		ScaleInCandidates:  scaleInCandidates,
		ScaleInSafe:        scaleInSafe,
		ScaleInReason:      scaleInReason,
		RebalancingNeeded:  rebalancingNeeded,
		WastedResources:    wastedResources,
	}
}

// ==============================================================================
// Helpers de pressão de nodes
// ==============================================================================

func (c *NodePoolCollector) extractNodePressures(snapshots []NodePoolNodeSnapshot) []NodePressureInfo {
	var pressures []NodePressureInfo
	for _, s := range snapshots {
		if len(s.ActiveConditions) == 0 {
			continue
		}
		severity := "warning"
		for _, cond := range s.ActiveConditions {
			// MemoryPressure e DiskPressure são condições críticas
			if cond == "MemoryPressure" || cond == "DiskPressure" {
				severity = "critical"
				break
			}
		}
		pressures = append(pressures, NodePressureInfo{
			NodeName:   s.NodeName,
			Conditions: s.ActiveConditions,
			Severity:   severity,
		})
	}
	return pressures
}

// ==============================================================================
// Helpers de Prometheus
// ==============================================================================

// queryVectorByInstance executa query Prometheus e retorna mapa instance → valor.
func (c *NodePoolCollector) queryVectorByInstance(ctx context.Context, query string) map[string]float64 {
	if c.promClient == nil {
		return make(map[string]float64)
	}
	result, err := c.promClient.Query(ctx, query)
	if err != nil {
		log.Debug().Err(err).Str("query", query).Msg("Query Prometheus por instance falhou")
		return make(map[string]float64)
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return make(map[string]float64)
	}
	m := make(map[string]float64, len(vec))
	for _, s := range vec {
		if inst := string(s.Metric["instance"]); inst != "" {
			m[inst] = float64(s.Value)
		}
	}
	return m
}

// queryVectorByNode executa query Prometheus e retorna mapa node → valor (int).
func (c *NodePoolCollector) queryVectorByNode(ctx context.Context, query string) map[string]int {
	if c.promClient == nil {
		return make(map[string]int)
	}
	result, err := c.promClient.Query(ctx, query)
	if err != nil {
		return make(map[string]int)
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return make(map[string]int)
	}
	m := make(map[string]int, len(vec))
	for _, s := range vec {
		if node := string(s.Metric["node"]); node != "" {
			m[node] = int(s.Value)
		}
	}
	return m
}

// avgByInstance retorna a média dos valores de uma query por instance.
func (c *NodePoolCollector) avgByInstance(ctx context.Context, query string) float64 {
	byInst := c.queryVectorByInstance(ctx, query)
	if len(byInst) == 0 {
		return 0
	}
	var sum float64
	for _, v := range byInst {
		sum += v
	}
	return sum / float64(len(byInst))
}

// avgByNodeFloat retorna a média dos valores de uma query por node (float64).
func (c *NodePoolCollector) avgByNodeFloat(ctx context.Context, query string) float64 {
	byNode := c.queryVectorByNode(ctx, query)
	if len(byNode) == 0 {
		return 0
	}
	var sum float64
	for _, v := range byNode {
		sum += float64(v)
	}
	return sum / float64(len(byNode))
}

// ==============================================================================
// Utilidades
// ==============================================================================

// nodeReadyStatus retorna "Ready", "NotReady" ou "Unknown".
func nodeReadyStatus(node *corev1.Node) string {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			switch cond.Status {
			case corev1.ConditionTrue:
				return "Ready"
			case corev1.ConditionFalse:
				return "NotReady"
			default:
				return "Unknown"
			}
		}
	}
	return "Unknown"
}

// formatAge formata a idade de um recurso Kubernetes de forma legível.
func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		return "0s"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// clamp garante que v está dentro do intervalo [minVal, maxVal].
func clamp(v, minVal, maxVal float64) float64 {
	return math.Max(minVal, math.Min(maxVal, v))
}
