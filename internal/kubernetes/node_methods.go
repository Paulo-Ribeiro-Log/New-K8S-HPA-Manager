package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"k8s-hpa-manager/internal/models"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ==================================================================
// Node Management Methods
// ==================================================================

// GetNodesWithMetrics retorna lista de nodes de um node pool com métricas de uso
func (c *Client) GetNodesWithMetrics(ctx context.Context, nodePoolName string) ([]models.NodeInfo, error) {
	// 1. Obter lista de nodes do node pool (reutiliza GetNodesInNodePool)
	nodeNames, err := c.GetNodesInNodePool(ctx, nodePoolName)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes for node pool %s: %w", nodePoolName, err)
	}

	// 2. Buscar detalhes de cada node em paralelo
	nodeInfos := make([]models.NodeInfo, 0, len(nodeNames))
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(nodeNames))

	for _, nodeName := range nodeNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			nodeInfo, err := c.getNodeInfo(ctx, name, nodePoolName)
			if err != nil {
				errChan <- fmt.Errorf("failed to get info for node %s: %w", name, err)
				return
			}

			mu.Lock()
			nodeInfos = append(nodeInfos, nodeInfo)
			mu.Unlock()
		}(nodeName)
	}

	wg.Wait()
	close(errChan)

	// Coletar erros (se houver)
	if len(errChan) > 0 {
		return nil, <-errChan
	}

	// 3. Ordenar por nome
	sort.Slice(nodeInfos, func(i, j int) bool {
		return nodeInfos[i].Name < nodeInfos[j].Name
	})

	return nodeInfos, nil
}

// getNodeInfo obtém informações detalhadas de um node individual (helper privado)
func (c *Client) getNodeInfo(ctx context.Context, nodeName, nodePoolName string) (models.NodeInfo, error) {
	// Buscar node do Kubernetes API
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return models.NodeInfo{}, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	log.Info().
		Str("nodeName", nodeName).
		Str("nodePoolName", nodePoolName).
		Int("labelsCount", len(node.Labels)).
		Int("taintsCount", len(node.Spec.Taints)).
		Msg("🔍 [getNodeInfo] Node data collected")

	// Debug: Log unique labels/taints to verify no sharing
	if len(node.Labels) > 0 {
		log.Debug().
			Str("nodeName", nodeName).
			Interface("sampleLabels", extractSampleLabels(node.Labels, 3)).
			Msg("📋 [getNodeInfo] Sample labels for node")
	}
	if len(node.Spec.Taints) > 0 {
		log.Debug().
			Str("nodeName", nodeName).
			Interface("sampleTaints", extractSampleTaints(node.Spec.Taints, 2)).
			Msg("🔶 [getNodeInfo] Sample taints for node")
	}

	// Buscar informações do cluster a partir do config local (sem Azure CLI)
	// Tags são buscadas assincronamente via endpoint /azure-info
	clusterInfo := c.getClusterInfoFast()

	// Create deep copy of cluster tags to avoid sharing
	clusterTagsCopy := make(map[string]string)
	for k, v := range clusterInfo.ClusterTags {
		clusterTagsCopy[k] = v
	}

	// Create deep copy of node labels with detailed logging
	nodeLabels := make(map[string]string)
	for k, v := range node.Labels {
		nodeLabels[k] = v
	}

	// Create deep copy of node annotations
	nodeAnnotations := make(map[string]string)
	for k, v := range node.Annotations {
		nodeAnnotations[k] = v
	}

	// Create deep copy of taints
	nodeTaints := make([]corev1.Taint, len(node.Spec.Taints))
	copy(nodeTaints, node.Spec.Taints)

	log.Debug().
		Str("nodeName", nodeName).
		Interface("nodeLabels", nodeLabels).
		Interface("clusterTags", clusterTagsCopy).
		Int("nodeLabelsCount", len(nodeLabels)).
		Int("clusterTagsCount", len(clusterTagsCopy)).
		Int("nodeTaintsCount", len(nodeTaints)).
		Msg("🔍 [getNodeInfo] Deep copies created - data should be isolated per node")

	nodeInfo := models.NodeInfo{
		Name:              node.Name,
		NodePoolName:      nodePoolName,
		ClusterName:       c.cluster,
		ResourceGroup:     clusterInfo.ResourceGroup,
		Subscription:      clusterInfo.Subscription,
		SubscriptionName:  clusterInfo.SubscriptionName,
		ClusterTags:       clusterTagsCopy,
		KubernetesVersion: node.Status.NodeInfo.KubeletVersion,
		ProviderID:        node.Spec.ProviderID,
		Hostname:          node.Name, // Usar nome do node como hostname
		CreatedAt:         node.CreationTimestamp.Time,
		Age:               formatAge(node.CreationTimestamp.Time),
		Unschedulable:     node.Spec.Unschedulable,
		Labels:            nodeLabels,
		Annotations:       nodeAnnotations,
		Taints:            nodeTaints,
	}

	// Extrair IPs
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP:
			nodeInfo.InternalIP = addr.Address
		case corev1.NodeExternalIP:
			nodeInfo.ExternalIP = addr.Address
		}
	}

	// Capacity e Allocatable
	if cpu, ok := node.Status.Capacity[corev1.ResourceCPU]; ok {
		nodeInfo.CPUCapacity = cpu.String()
	}
	if memory, ok := node.Status.Capacity[corev1.ResourceMemory]; ok {
		nodeInfo.MemoryCapacity = formatMemory(memory.Value())
	}
	if pods, ok := node.Status.Capacity[corev1.ResourcePods]; ok {
		nodeInfo.PodsCapacity = int(pods.Value())
	}

	if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
		nodeInfo.CPUAllocatable = cpu.String()
	}
	if memory, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
		nodeInfo.MemoryAllocatable = formatMemory(memory.Value())
	}
	if pods, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
		nodeInfo.PodsAllocatable = int(pods.Value())
	}

	// Conditions
	nodeInfo.Conditions = make([]models.NodeCondition, len(node.Status.Conditions))
	for i, cond := range node.Status.Conditions {
		nodeInfo.Conditions[i] = models.NodeCondition{
			Type:               string(cond.Type),
			Status:             string(cond.Status),
			LastTransitionTime: cond.LastTransitionTime.Time,
			Reason:             cond.Reason,
			Message:            cond.Message,
		}
	}

	// Determinar status geral do node
	nodeInfo.Status = c.determineNodeStatus(node)

	// Buscar métricas (CPU, Memory usage)
	if err := c.enrichNodeWithMetrics(ctx, &nodeInfo, nodeName); err != nil {
		// Não falhar se métricas não disponíveis - apenas logar warning
		log.Warn().Err(err).Str("node", nodeName).Msg("Failed to get node metrics")
	}

	// Contar pods no node
	if err := c.enrichNodeWithPodCount(ctx, &nodeInfo, nodeName); err != nil {
		log.Warn().Err(err).Str("node", nodeName).Msg("Failed to count pods on node")
	}

	return nodeInfo, nil
}

// determineNodeStatus determina status geral do node baseado nas conditions
func (c *Client) determineNodeStatus(node *corev1.Node) string {
	if node.Spec.Unschedulable {
		return "SchedulingDisabled" // Cordoned
	}

	// Verificar condition "Ready"
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			if cond.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return "NotReady"
		}
	}

	return "Unknown"
}

// enrichNodeWithMetrics adiciona métricas de uso ao NodeInfo
func (c *Client) enrichNodeWithMetrics(ctx context.Context, nodeInfo *models.NodeInfo, nodeName string) error {
	// Tentar obter do Metrics Server primeiro
	if c.metricsClient == nil {
		log.Warn().Str("node", nodeName).Msg("Metrics client is nil - skipping metrics collection")
		nodeInfo.MetricsError = "cliente de métricas não inicializado"
		return nil
	}

	nodeMetrics, err := c.metricsClient.MetricsV1beta1().NodeMetricses().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		log.Warn().Err(err).Str("node", nodeName).Msg("Failed to get node metrics from Metrics Server")
		// Bug real corrigido: antes esse erro era só logado no servidor e descartado — o
		// frontend via CPUUsed/MemoryUsed vazios (zero-value) e mostrava "N/A" sem explicar o
		// motivo, indistinguível de "ainda não tentou". Comum em clusters EKS, que não vêm com
		// metrics-server por padrão (diferente de AKS) — expõe o erro real pro usuário decidir
		// se é preciso instalar o metrics-server, mesmo padrão já usado na aba Pods.
		nodeInfo.MetricsError = fmt.Sprintf("metrics-server indisponível: %v", err)
		return nil
	}

	nodeInfo.MetricsAvailable = true
	log.Info().Str("node", nodeName).Msg("Successfully fetched node metrics from Metrics Server")

	// CPU
	if cpu, ok := nodeMetrics.Usage[corev1.ResourceCPU]; ok {
		nodeInfo.CPUUsed = fmt.Sprintf("%dm", cpu.MilliValue())

		// Calcular % (usado / allocatable * 100)
		if allocatable := parseQuantityMilliCPU(nodeInfo.CPUAllocatable); allocatable > 0 {
			used := float64(cpu.MilliValue())
			nodeInfo.CPUUsagePercent = (used / allocatable) * 100
			log.Info().Str("node", nodeName).Float64("cpu_percent", nodeInfo.CPUUsagePercent).Msg("CPU metrics calculated")
		}
	}

	// Memory
	if memory, ok := nodeMetrics.Usage[corev1.ResourceMemory]; ok {
		nodeInfo.MemoryUsed = formatMemory(memory.Value())

		if allocatable := parseMemoryBytes(nodeInfo.MemoryAllocatable); allocatable > 0 {
			used := float64(memory.Value())
			nodeInfo.MemoryUsagePercent = (used / allocatable) * 100
			log.Info().Str("node", nodeName).Float64("memory_percent", nodeInfo.MemoryUsagePercent).Msg("Memory metrics calculated")
		}
	}

	// Disk usage - não disponível diretamente no Metrics Server
	// Placeholder: 0% (seria necessário Prometheus node_exporter)
	nodeInfo.DiskUsagePercent = 0

	return nil
}

// GetNodeRawMetrics retorna uso bruto de CPU (milicores) e memória (bytes) via Metrics Server.
// Retorna (0, 0, error) se o Metrics Server não estiver disponível.
func (c *Client) GetNodeRawMetrics(ctx context.Context, nodeName string) (cpuMillis int64, memBytes int64, err error) {
	if c.metricsClient == nil {
		return 0, 0, fmt.Errorf("metrics client not initialized")
	}

	nodeMetrics, err := c.metricsClient.MetricsV1beta1().NodeMetricses().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("metrics server unavailable for node %s: %w", nodeName, err)
	}

	if cpu, ok := nodeMetrics.Usage[corev1.ResourceCPU]; ok {
		cpuMillis = cpu.MilliValue()
	}
	if mem, ok := nodeMetrics.Usage[corev1.ResourceMemory]; ok {
		memBytes = mem.Value()
	}

	return cpuMillis, memBytes, nil
}

// enrichNodeWithPodCount conta pods rodando no node
func (c *Client) enrichNodeWithPodCount(ctx context.Context, nodeInfo *models.NodeInfo, nodeName string) error {
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return err
	}

	nodeInfo.PodsTotal = len(pods.Items)
	nodeInfo.PodsRunning = 0

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			nodeInfo.PodsRunning++
		}
	}

	return nil
}

// GetNodeDetails retorna detalhes completos de um node incluindo pods e eventos
func (c *Client) GetNodeDetails(ctx context.Context, nodeName, nodePoolName string) (*models.NodeDetailsResponse, error) {
	// 1. Obter informações do node
	nodeInfo, err := c.getNodeInfo(ctx, nodeName, nodePoolName)
	if err != nil {
		return nil, err
	}

	// 2. Buscar pods rodando no node
	pods, err := c.getPodsOnNode(ctx, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods on node %s: %w", nodeName, err)
	}

	// 3. Buscar eventos recentes do node
	events, err := c.getNodeEvents(ctx, nodeName)
	if err != nil {
		// Não falhar - eventos são opcionais
		log.Warn().Err(err).Str("node", nodeName).Msg("Failed to get node events")
	}

	// 4. Executar kubectl describe (opcional)
	kubectlDescribe := ""
	if describe, err := c.executeKubectlDescribeNode(nodeName); err == nil {
		kubectlDescribe = describe
	}

	return &models.NodeDetailsResponse{
		Node:            nodeInfo,
		Pods:            pods,
		Events:          events,
		KubectlDescribe: kubectlDescribe,
	}, nil
}

// getPodsOnNode retorna lista de pods rodando em um node
func (c *Client) getPodsOnNode(ctx context.Context, nodeName string) ([]models.PodOnNode, error) {
	podsList, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return nil, err
	}

	// Buscar métricas de pods via Metrics Server (graceful degradation se indisponível)
	type podKey struct{ name, ns string }
	podCPUUsage := map[podKey]resource.Quantity{}
	podMemUsage := map[podKey]resource.Quantity{}

	if c.metricsClient != nil {
		podMetricsList, mErr := c.metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
		if mErr == nil {
			for _, pm := range podMetricsList.Items {
				var totalCPU, totalMem resource.Quantity
				for _, c := range pm.Containers {
					if v, ok := c.Usage[corev1.ResourceCPU]; ok {
						totalCPU.Add(v)
					}
					if v, ok := c.Usage[corev1.ResourceMemory]; ok {
						totalMem.Add(v)
					}
				}
				k := podKey{pm.Name, pm.Namespace}
				podCPUUsage[k] = totalCPU
				podMemUsage[k] = totalMem
			}
		}
	}

	pods := make([]models.PodOnNode, 0, len(podsList.Items))
	for _, pod := range podsList.Items {
		podInfo := models.PodOnNode{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Phase:     string(pod.Status.Phase),
		}

		// Agregar recursos de todos os containers
		var cpuReq, cpuLim, memReq, memLim resource.Quantity
		var restarts int32

		for _, container := range pod.Spec.Containers {
			if req, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				cpuReq.Add(req)
			}
			if lim, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
				cpuLim.Add(lim)
			}
			if req, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				memReq.Add(req)
			}
			if lim, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
				memLim.Add(lim)
			}
		}

		// Contar restarts
		for _, status := range pod.Status.ContainerStatuses {
			restarts += status.RestartCount
		}

		podInfo.CPURequest = cpuReq.String()
		podInfo.CPULimit = cpuLim.String()
		podInfo.MemoryRequest = formatMemory(memReq.Value())
		podInfo.MemoryLimit = formatMemory(memLim.Value())
		podInfo.RestartCount = int(restarts)

		// Popular métricas de uso atual se disponíveis
		k := podKey{pod.Name, pod.Namespace}
		if cpuUsed, ok := podCPUUsage[k]; ok {
			podInfo.CPUUsage = fmt.Sprintf("%dm", cpuUsed.MilliValue())
			// % em relação ao limit; se sem limit, usa request
			ref := cpuLim
			if ref.IsZero() {
				ref = cpuReq
			}
			if !ref.IsZero() {
				podInfo.CPUUsagePct = float64(cpuUsed.MilliValue()) / float64(ref.MilliValue()) * 100
			}
		}
		if memUsed, ok := podMemUsage[k]; ok {
			podInfo.MemoryUsage = formatMemory(memUsed.Value())
			ref := memLim
			if ref.IsZero() {
				ref = memReq
			}
			if !ref.IsZero() {
				podInfo.MemoryUsagePct = float64(memUsed.Value()) / float64(ref.Value()) * 100
			}
		}

		pods = append(pods, podInfo)
	}

	return pods, nil
}

// getNodeEvents retorna eventos recentes do node (últimos 20)
func (c *Client) getNodeEvents(ctx context.Context, nodeName string) ([]models.NodeEvent, error) {
	eventsList, err := c.clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Node", nodeName),
	})
	if err != nil {
		return nil, err
	}

	// Ordenar por timestamp (mais recente primeiro)
	sort.Slice(eventsList.Items, func(i, j int) bool {
		return eventsList.Items[i].LastTimestamp.Time.After(eventsList.Items[j].LastTimestamp.Time)
	})

	// Limitar a 20 eventos mais recentes
	maxEvents := 20
	if len(eventsList.Items) > maxEvents {
		eventsList.Items = eventsList.Items[:maxEvents]
	}

	events := make([]models.NodeEvent, 0, len(eventsList.Items))
	for _, event := range eventsList.Items {
		events = append(events, models.NodeEvent{
			Type:            event.Type,
			Reason:          event.Reason,
			Message:         event.Message,
			Count:           event.Count,
			FirstTimestamp:  event.FirstTimestamp.Time,
			LastTimestamp:   event.LastTimestamp.Time,
			SourceComponent: event.Source.Component,
			SourceHost:      event.Source.Host,
		})
	}

	return events, nil
}

// executeKubectlDescribeNode executa kubectl describe node
func (c *Client) executeKubectlDescribeNode(nodeName string) (string, error) {
	// Contexts K8s têm sufixo -admin; tentar com sufixo primeiro
	context := c.cluster
	if !strings.HasSuffix(context, "-admin") {
		context = context + "-admin"
	}
	cmd := exec.Command("kubectl", "describe", "node", nodeName, "--context", context)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback sem sufixo
		cmd2 := exec.Command("kubectl", "describe", "node", nodeName, "--context", c.cluster)
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return "", fmt.Errorf("kubectl describe failed: %w\nOutput: %s", err, string(output))
		}
		return string(output2), nil
	}
	return string(output), nil
}

// ==================================================================
// Helper Functions
// ==================================================================

// formatMemory formata bytes em unidades legíveis (Gi, Mi, Ki)
func formatMemory(bytes int64) string {
	const (
		Ki = 1024
		Mi = 1024 * Ki
		Gi = 1024 * Mi
	)

	if bytes >= Gi {
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(Gi))
	}
	if bytes >= Mi {
		return fmt.Sprintf("%.0fMi", float64(bytes)/float64(Mi))
	}
	return fmt.Sprintf("%.0fKi", float64(bytes)/float64(Ki))
}

// parseQuantityMilliCPU converte string CPU quantity para float64 millicores
func parseQuantityMilliCPU(s string) float64 {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return float64(q.MilliValue())
}

// parseMemoryBytes converte string memory quantity para float64 bytes
func parseMemoryBytes(s string) float64 {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return float64(q.Value())
}

// ClusterInfo armazena informações do cluster
type ClusterInfo struct {
	ResourceGroup    string
	Subscription     string
	SubscriptionName string
	ClusterTags      map[string]string
}

// getClusterInfo busca informações do cluster (resource group, subscription, tags)
func (c *Client) getClusterInfo() ClusterInfo {
	info := ClusterInfo{
		ClusterTags: make(map[string]string),
	}

	// Buscar cluster config do clusters-config.json
	clusterConfig, err := findClusterConfig(c.cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", c.cluster).Msg("Failed to find cluster config")
		return info
	}

	info.ResourceGroup = clusterConfig.ResourceGroup
	info.Subscription = clusterConfig.Subscription

	// Buscar nome da subscription
	info.SubscriptionName = getSubscriptionNameForNode(clusterConfig.Subscription)

	// Buscar tags do cluster
	clusterName := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")
	tags, err := getClusterTagsForNode(clusterName, clusterConfig.ResourceGroup)
	if err != nil {
		log.Warn().Err(err).Str("cluster", c.cluster).Msg("Failed to get cluster tags")
	} else {
		info.ClusterTags = tags
	}

	return info
}

// getClusterInfoFast lê apenas o clusters-config.json (sem chamar Azure CLI).
// Use em operações que precisam de RG/SubscriptionID mas não precisam esperar az aks show.
func (c *Client) getClusterInfoFast() ClusterInfo {
	info := ClusterInfo{
		ClusterTags: make(map[string]string),
	}
	clusterConfig, err := findClusterConfig(c.cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", c.cluster).Msg("Failed to find cluster config (fast)")
		return info
	}
	info.ResourceGroup = clusterConfig.ResourceGroup
	info.Subscription = clusterConfig.Subscription
	return info
}

// GetClusterAzureInfo retorna informações do cluster incluindo tags e nome da subscription.
// Faz chamadas Azure CLI (az aks show + az account show) — use em endpoint async dedicado.
func (c *Client) GetClusterAzureInfo() ClusterInfo {
	return c.getClusterInfo()
}

// findClusterConfig busca cluster no clusters-config.json
func findClusterConfig(clusterName string) (*models.ClusterConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".k8s-hpa-manager", "clusters-config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read clusters-config.json: %w", err)
	}

	var configs []models.ClusterConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("failed to parse clusters-config.json: %w", err)
	}

	// Normalizar nome do cluster (remover -admin)
	normalizedName := strings.TrimSuffix(clusterName, "-admin")

	for _, cfg := range configs {
		clusterCfgName := strings.TrimSuffix(cfg.ClusterName, "-admin")
		if clusterCfgName == normalizedName {
			return &cfg, nil
		}
	}

	return nil, fmt.Errorf("cluster '%s' not found in clusters-config.json", clusterName)
}

// getSubscriptionNameForNode busca nome da subscription via Azure CLI
func getSubscriptionNameForNode(subscriptionID string) string {
	cmd := exec.Command("az", "account", "show",
		"--subscription", subscriptionID,
		"--query", "name",
		"--output", "tsv")

	output, err := cmd.Output()
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to fetch subscription name for %s", subscriptionID)
		return ""
	}

	return strings.TrimSpace(string(output))
}

// getClusterTagsForNode busca tags do cluster via Azure CLI
func getClusterTagsForNode(clusterName, resourceGroup string) (map[string]string, error) {
	cmd := exec.Command("az", "aks", "show",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--query", "tags",
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			stderr := string(exitError.Stderr)
			return nil, fmt.Errorf("az aks show failed: %s", stderr)
		}
		return nil, fmt.Errorf("failed to execute az aks show: %w", err)
	}

	var tags map[string]string
	if err := json.Unmarshal(output, &tags); err != nil {
		return nil, fmt.Errorf("failed to parse cluster tags: %w", err)
	}

	if tags == nil {
		tags = make(map[string]string)
	}

	return tags, nil
}

// extractSampleLabels extracts a sample of labels for debugging (not for production)
func extractSampleLabels(labels map[string]string, maxSamples int) map[string]string {
	if len(labels) == 0 {
		return nil
	}

	sample := make(map[string]string)
	count := 0
	for k, v := range labels {
		if count >= maxSamples {
			break
		}
		sample[k] = v
		count++
	}
	return sample
}

// extractSampleTaints extracts a sample of taints for debugging (not for production)
func extractSampleTaints(taints []corev1.Taint, maxSamples int) []string {
	if len(taints) == 0 {
		return nil
	}

	sample := make([]string, 0, maxSamples)
	for i, taint := range taints {
		if i >= maxSamples {
			break
		}
		sample = append(sample, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
	}
	return sample
}
