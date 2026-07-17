package handlers

import (
	"fmt"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/client"
)

// NamespaceHandler gerencia requisições relacionadas a namespaces
type NamespaceHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewNamespaceHandler cria um novo handler de namespaces
func NewNamespaceHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *NamespaceHandler {
	return &NamespaceHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// List retorna todos os namespaces de um cluster
func (h *NamespaceHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	showSystem := c.DefaultQuery("showSystem", "false") == "true"

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	// Obter client do cluster (reutilizar código existente)
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client for cluster %s: %v", cluster, err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(client, cluster)

	// Listar namespaces (reutilizar código existente)
	namespaces, err := kubeClient.ListNamespaces(c.Request.Context(), showSystem)
	if err != nil {
		err = h.kubeManager.EnrichEKSError(err, cluster)
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": fmt.Sprintf("Failed to list namespaces: %v", err),
			},
		})
		return
	}

	// Formatar resposta
	response := make([]gin.H, len(namespaces))
	for i, ns := range namespaces {
		response[i] = gin.H{
			"name":     ns.Name,
			"cluster":  ns.Cluster,
			"hpaCount": ns.HPACount,
			"isSystem": ns.IsSystem,
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    response,
		"count":   len(response),
	})
}

// GetMetrics retorna métricas agregadas por namespace (Top N namespaces por CPU, Memory, Pods)
// GET /api/v1/namespaces/:cluster/metrics?limit=5&sort_by=cpu
func (h *NamespaceHandler) GetMetrics(c *gin.Context) {
	cluster := c.Param("cluster")
	limit := 5                                 // Top 5 por padrão
	sortBy := c.DefaultQuery("sort_by", "cpu") // cpu, memory, pods

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	log.Info().
		Str("cluster", cluster).
		Str("sort_by", sortBy).
		Int("limit", limit).
		Msg("Buscando métricas agregadas por namespace")

	// Criar cliente Prometheus para o cluster
	promClient, err := client.NewPrometheusClient(cluster)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", cluster).
			Msg("Erro ao criar cliente Prometheus")

		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "PROMETHEUS_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to create Prometheus client: %v", err),
			},
		})
		return
	}
	defer promClient.Close()

	// Buscar métricas de todos os namespaces
	metrics, err := promClient.GetNamespaceMetrics(c.Request.Context())
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", cluster).
			Msg("Erro ao buscar métricas de namespace")

		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "METRICS_ERROR",
				"message": fmt.Sprintf("Failed to fetch namespace metrics: %v", err),
			},
		})
		return
	}

	// Ordenar por CPU request (padrão)
	sortByCPU := func(metrics []client.NamespaceMetrics) []client.NamespaceMetrics {
		sort.Slice(metrics, func(i, j int) bool {
			return metrics[i].CPURequestMillis > metrics[j].CPURequestMillis
		})
		return metrics
	}

	sortByMemory := func(metrics []client.NamespaceMetrics) []client.NamespaceMetrics {
		sort.Slice(metrics, func(i, j int) bool {
			return metrics[i].MemoryRequestGB > metrics[j].MemoryRequestGB
		})
		return metrics
	}

	sortByPods := func(metrics []client.NamespaceMetrics) []client.NamespaceMetrics {
		sort.Slice(metrics, func(i, j int) bool {
			return metrics[i].PodCount > metrics[j].PodCount
		})
		return metrics
	}

	// Criar cópias para cada ordenação
	topCPU := make([]client.NamespaceMetrics, len(metrics))
	topMemory := make([]client.NamespaceMetrics, len(metrics))
	topPods := make([]client.NamespaceMetrics, len(metrics))

	copy(topCPU, metrics)
	copy(topMemory, metrics)
	copy(topPods, metrics)

	topCPU = sortByCPU(topCPU)
	topMemory = sortByMemory(topMemory)
	topPods = sortByPods(topPods)

	// Limitar ao Top N
	if len(topCPU) > limit {
		topCPU = topCPU[:limit]
	}
	if len(topMemory) > limit {
		topMemory = topMemory[:limit]
	}
	if len(topPods) > limit {
		topPods = topPods[:limit]
	}

	// Calcular "Others" (soma de todos os namespaces fora do Top N)
	calculateOthers := func(allMetrics []client.NamespaceMetrics, topN []client.NamespaceMetrics) client.NamespaceMetrics {
		topNamesMap := make(map[string]bool)
		for _, m := range topN {
			topNamesMap[m.Namespace] = true
		}

		others := client.NamespaceMetrics{
			Namespace: "Outros",
		}

		for _, m := range allMetrics {
			if !topNamesMap[m.Namespace] {
				others.CPURequestMillis += m.CPURequestMillis
				others.MemoryRequestGB += m.MemoryRequestGB
				others.PodCount += m.PodCount
				others.CPUPercentOfCluster += m.CPUPercentOfCluster
				others.MemoryPercentOfCluster += m.MemoryPercentOfCluster
				others.PodPercentOfCluster += m.PodPercentOfCluster
			}
		}

		return others
	}

	cpuOthers := calculateOthers(metrics, topCPU)
	memoryOthers := calculateOthers(metrics, topMemory)
	podsOthers := calculateOthers(metrics, topPods)

	log.Info().
		Int("total_namespaces", len(metrics)).
		Int("top_cpu_count", len(topCPU)).
		Int("top_memory_count", len(topMemory)).
		Int("top_pods_count", len(topPods)).
		Msg("Métricas de namespace processadas")

	// Retornar resposta com Top N + Others
	c.JSON(200, gin.H{
		"success": true,
		"data": gin.H{
			"top_cpu":          topCPU,
			"top_memory":       topMemory,
			"top_pods":         topPods,
			"cpu_others":       cpuOthers,
			"memory_others":    memoryOthers,
			"pods_others":      podsOthers,
			"total_namespaces": len(metrics),
		},
	})
}

// Get retorna o manifesto YAML completo de um namespace específico
func (h *NamespaceHandler) Get(c *gin.Context) {
	cluster := c.Param("cluster")
	name := c.Param("name")

	if cluster == "" || name == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster and name are required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(clientset, cluster)
	manifest, err := kubeClient.GetNamespace(c.Request.Context(), name)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "GET_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    manifest,
	})
}

// Describe retorna a saída do kubectl describe para um namespace
func (h *NamespaceHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	name := c.Param("name")

	if cluster == "" || name == "" {
		c.JSON(400, gin.H{"error": "cluster and name are required"})
		return
	}

	// Executar kubectl describe
	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(cluster)
	if authErr != nil {
		c.JSON(500, gin.H{
			"error": fmt.Sprintf("Failed to resolve cluster authentication: %v", authErr),
		})
		return
	}
	defer cleanup()

	output, err := kubeclient.ExecuteKubectlDescribe(authArgs, "namespace", name, "")
	if err != nil {
		c.JSON(500, gin.H{
			"error": fmt.Sprintf("Failed to execute kubectl describe: %v", err),
		})
		return
	}

	c.JSON(200, gin.H{
		"cluster":  cluster,
		"name":     name,
		"describe": output,
	})
}

// Delete deleta um namespace
func (h *NamespaceHandler) Delete(c *gin.Context) {
	cluster := c.Param("cluster")
	name := c.Param("name")

	if cluster == "" || name == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster and name are required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(clientset, cluster)

	ctx := c.Request.Context()
	var before map[string]interface{}
	if manifest, err := kubeClient.GetNamespace(ctx, name); err == nil {
		before = map[string]interface{}{
			"name":            manifest.Name,
			"yaml":            manifest.YAML,
			"resourceVersion": manifest.Metadata.ResourceVersion,
		}
	}

	start := time.Now()
	err = kubeClient.DeleteNamespace(ctx, name)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "delete_namespace",
			Resource: name,
			Cluster:  cluster,
			Before:   before,
			After:    nil,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("Namespace %s deleted successfully", name),
	})
}

// Create cria um novo namespace
func (h *NamespaceHandler) Create(c *gin.Context) {
	cluster := c.Param("cluster")

	var req struct {
		Name           string            `json:"name" binding:"required"`
		IsSpotInstance bool              `json:"isSpotInstance"`
		Annotations    map[string]string `json:"annotations"`
		Labels         map[string]string `json:"labels"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "Name is required",
			},
		})
		return
	}

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster is required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(clientset, cluster)
	start := time.Now()
	err = kubeClient.CreateNamespace(c.Request.Context(), req.Name, req.IsSpotInstance, req.Annotations, req.Labels)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CREATE_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	if h.historyTracker != nil {
		after := map[string]interface{}{"name": req.Name}
		if req.IsSpotInstance {
			after["isSpotInstance"] = true
		}
		if len(req.Annotations) > 0 {
			after["annotations"] = req.Annotations
		}
		if len(req.Labels) > 0 {
			after["labels"] = req.Labels
		}
		entry := history.HistoryEntry{
			Action:   "create_namespace",
			Resource: req.Name,
			Cluster:  cluster,
			Before:   nil,
			After:    after,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("Namespace %s created successfully", req.Name),
	})
}

// Apply aplica um manifesto YAML de namespace (suporta dry-run)
func (h *NamespaceHandler) Apply(c *gin.Context) {
	cluster := c.Param("cluster")
	name := c.Param("name")

	var req struct {
		YAML         string `json:"yaml" binding:"required"`
		FieldManager string `json:"fieldManager"`
		DryRun       bool   `json:"dryRun"`
		Force        bool   `json:"force"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "YAML is required",
			},
		})
		return
	}

	if cluster == "" || name == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster and name are required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(clientset, cluster)

	ctx := c.Request.Context()
	var before map[string]interface{}
	if !req.DryRun {
		if manifest, err := kubeClient.GetNamespace(ctx, name); err == nil {
			before = map[string]interface{}{
				"yaml":            manifest.YAML,
				"resourceVersion": manifest.Metadata.ResourceVersion,
			}
		}
	}

	start := time.Now()
	// Aplicar namespace
	result, err := kubeClient.ApplyNamespace(ctx, req.YAML, req.FieldManager, name, req.DryRun, req.Force)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "APPLY_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		after := map[string]interface{}{
			"name":            result.Name,
			"resourceVersion": result.ResourceVersion,
			"labels":          result.Labels,
			"annotations":     result.Annotations,
		}
		entry := history.HistoryEntry{
			Action:   "apply_namespace",
			Resource: name,
			Cluster:  cluster,
			Before:   before,
			After:    after,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	message := fmt.Sprintf("Namespace %s applied successfully", name)
	if req.DryRun {
		message = fmt.Sprintf("Namespace %s validated successfully (dry-run)", name)
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": message,
	})
}
