package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/client"
	enginev2 "k8s-hpa-manager/internal/monitoring/engine"
)

// formatCPU converte CPU de cores (float string) para millicores (ex: "0.384" → "384m")
func formatCPU(cpuStr string) string {
	cpuFloat, err := strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		log.Warn().Str("value", cpuStr).Msg("Falha ao parsear CPU para formatação")
		return cpuStr // Retorna original se falhar
	}

	// Converter cores para millicores
	millicores := cpuFloat * 1000

	return fmt.Sprintf("%.0fm", millicores)
}

// formatMemory converte memória de bytes (string) para formato humanizado (Mi/Gi)
func formatMemory(memoryStr string) string {
	memoryFloat, err := strconv.ParseFloat(memoryStr, 64)
	if err != nil {
		log.Warn().Str("value", memoryStr).Msg("Falha ao parsear memória para formatação")
		return memoryStr // Retorna original se falhar
	}

	const (
		Ki = 1024
		Mi = Ki * 1024
		Gi = Mi * 1024
	)

	switch {
	case memoryFloat >= Gi:
		return fmt.Sprintf("%.0fGi", memoryFloat/Gi)
	case memoryFloat >= Mi:
		return fmt.Sprintf("%.0fMi", memoryFloat/Mi)
	case memoryFloat >= Ki:
		return fmt.Sprintf("%.0fKi", memoryFloat/Ki)
	default:
		return fmt.Sprintf("%.0f", memoryFloat)
	}
}

// MonitoringHandlerV2 usa a nova MonitoringEngineV2 (sem port-forwards)
type MonitoringHandlerV2 struct {
	engine      *enginev2.MonitoringEngineV2
	kubeManager *config.KubeConfigManager
}

// NewMonitoringHandlerV2 cria novo handler usando MonitoringEngineV2
func NewMonitoringHandlerV2(engine *enginev2.MonitoringEngineV2, kubeManager *config.KubeConfigManager) *MonitoringHandlerV2 {
	return &MonitoringHandlerV2{
		engine:      engine,
		kubeManager: kubeManager,
	}
}

// GetMetrics retorna métricas em tempo real de um HPA (via Prometheus)
// GET /api/v1/monitoring/v2/metrics/:cluster/:namespace/:hpaName?duration=1h
func (h *MonitoringHandlerV2) GetMetrics(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	hpaName := c.Param("hpaName")
	duration := c.DefaultQuery("duration", "1h")

	// Remove sufixo -admin do cluster (frontend envia com -admin, mas Prometheus não precisa)
	normalizedCluster := strings.TrimSuffix(cluster, "-admin")

	// Parse duration
	dur, err := time.ParseDuration(duration)
	if err != nil {
		c.JSON(400, gin.H{
			"error": "Invalid duration format. Use formats like: 5m, 1h, 24h",
		})
		return
	}

	log.Info().
		Str("cluster", normalizedCluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Dur("duration", dur).
		Msg("Buscando métricas via MonitoringEngineV2")

	// Buscar métricas históricas via Prometheus (range query)
	historicalData, err := h.engine.GetHPAHistoricalMetrics(normalizedCluster, namespace, hpaName, dur, 1*time.Minute)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", normalizedCluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Str("error_type", fmt.Sprintf("%T", err)).
			Msg("Erro ao buscar métricas históricas do Prometheus")

		c.JSON(500, gin.H{
			"error":   fmt.Sprintf("Failed to fetch metrics from Prometheus: %v", err),
			"cluster": normalizedCluster,
			"details": err.Error(),
		})
		return
	}

	log.Info().
		Str("cluster", normalizedCluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Int("data_points", len(historicalData)).
		Msg("Métricas históricas obtidas com sucesso")

	// Buscar targets do HPA (uma vez, válido para todos os snapshots)
	var cpuTarget, memoryTarget float64
	tmpSnapshot := &enginev2.HPAMetrics{}
	if err := h.enrichSnapshotWithTargets(c.Request.Context(), normalizedCluster, namespace, hpaName, tmpSnapshot); err != nil {
		log.Warn().Err(err).Msg("Falha ao buscar targets do HPA (continuando sem targets)")
	} else {
		cpuTarget = tmpSnapshot.CPUTarget
		memoryTarget = tmpSnapshot.MemoryTarget
	}

	// Converter para formato API (compatível com frontend)
	apiSnapshots := convertHistoricalToAPI(historicalData, cpuTarget, memoryTarget)

	log.Info().
		Str("cluster", normalizedCluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Int("count", len(apiSnapshots)).
		Msg("Métricas retornadas via Prometheus")

	c.JSON(200, gin.H{
		"cluster":             cluster,
		"namespace":           namespace,
		"hpa_name":            hpaName,
		"duration":            duration,
		"snapshots":           apiSnapshots,
		"snapshots_yesterday": []gin.H{}, // V2 não usa comparação D-1 (dados em tempo real)
		"count":               len(apiSnapshots),
		"count_yesterday":     0,
		"source":              "prometheus", // Indicador de fonte de dados
		"cpu_target":          cpuTarget,
		"memory_target":       memoryTarget,
	})
}

// GetStatus retorna status da MonitoringEngineV2
// GET /api/v1/monitoring/v2/status
func (h *MonitoringHandlerV2) GetStatus(c *gin.Context) {
	running := h.engine.IsRunning()
	cacheStats := h.engine.GetCacheStats()

	status := "stopped"
	if running {
		status = "running"
	}

	c.JSON(200, gin.H{
		"running":         running,
		"status":          status,
		"mode":            "direct_prometheus", // Nova arquitetura
		"cache_stats":     cacheStats,
		"port_forwarding": false, // V2 não usa port-forwards
	})
}

// Start inicia a MonitoringEngineV2
// POST /api/v1/monitoring/v2/start
func (h *MonitoringHandlerV2) Start(c *gin.Context) {
	if h.engine.IsRunning() {
		c.JSON(200, gin.H{
			"status":  "already_running",
			"message": "Monitoring engine V2 is already running",
		})
		return
	}

	err := h.engine.Start()
	if err != nil {
		c.JSON(500, gin.H{
			"status":  "error",
			"message": "Failed to start monitoring engine V2",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"status":  "started",
		"message": "Monitoring engine V2 started successfully",
	})
}

// Stop para a MonitoringEngineV2
// POST /api/v1/monitoring/v2/stop
func (h *MonitoringHandlerV2) Stop(c *gin.Context) {
	if !h.engine.IsRunning() {
		c.JSON(200, gin.H{
			"status":  "already_stopped",
			"message": "Monitoring engine V2 is not running",
		})
		return
	}

	err := h.engine.Stop()
	if err != nil {
		c.JSON(500, gin.H{
			"status":  "error",
			"message": "Failed to stop monitoring engine V2",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"status":  "stopped",
		"message": "Monitoring engine V2 stopped successfully",
	})
}

// AddHPA adiciona HPA para monitoramento (apenas cache, sem coleta ativa)
// POST /api/v1/monitoring/v2/hpa
// Body: { "cluster": "...", "namespace": "...", "hpa": "..." }
func (h *MonitoringHandlerV2) AddHPA(c *gin.Context) {
	var req struct {
		Cluster   string `json:"cluster" binding:"required"`
		Namespace string `json:"namespace" binding:"required"`
		HPA       string `json:"hpa" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"status":  "error",
			"message": "Invalid request body",
			"error":   err.Error(),
		})
		return
	}

	// Remove sufixo -admin
	clusterName := strings.TrimSuffix(req.Cluster, "-admin")

	log.Info().
		Str("cluster", clusterName).
		Str("namespace", req.Namespace).
		Str("hpa", req.HPA).
		Msg("HPA adicionado ao monitoramento V2 (cache on-demand)")

	// V2 não precisa adicionar targets - usa cache on-demand
	// Primeira chamada a GetHPAMetrics() vai buscar dados do Prometheus e cachear

	c.JSON(200, gin.H{
		"status":  "success",
		"message": "HPA added to monitoring V2 (cache on-demand)",
		"target": gin.H{
			"cluster":   clusterName,
			"namespace": req.Namespace,
			"hpa":       req.HPA,
		},
	})
}

// enrichSnapshotWithTargets busca os targets do HPA via Kubernetes API
func (h *MonitoringHandlerV2) enrichSnapshotWithTargets(ctx context.Context, cluster, namespace, hpaName string, snapshot *enginev2.HPAMetrics) error {
	// Normalizar nome do cluster (adicionar -admin se não tiver)
	clusterContext := cluster
	if !strings.HasSuffix(clusterContext, "-admin") {
		clusterContext = cluster + "-admin"
	}

	// Obter cliente Kubernetes (wrapper customizado)
	k8sWrapper, err := h.kubeManager.GetClient(clusterContext)
	if err != nil {
		return fmt.Errorf("erro ao obter cliente k8s: %w", err)
	}

	// Criar instância do Client (wrapper com métodos customizados)
	k8sClient := kubernetes.NewClient(k8sWrapper, clusterContext)

	// Buscar HPA
	hpa, err := k8sClient.GetHPA(ctx, namespace, hpaName)
	if err != nil {
		return fmt.Errorf("erro ao buscar HPA: %w", err)
	}

	// Extrair targets das métricas (hpa é models.HPA)
	if hpa.TargetCPU != nil {
		snapshot.CPUTarget = float64(*hpa.TargetCPU)
	}
	if hpa.TargetMemory != nil {
		snapshot.MemoryTarget = float64(*hpa.TargetMemory)
	}

	return nil
}

// GetCurrentMetrics retorna métricas atuais (snapshot instantâneo)
// GET /api/v1/monitoring/v2/current/:cluster/:namespace/:hpaName
func (h *MonitoringHandlerV2) GetCurrentMetrics(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	hpaName := c.Param("hpaName")

	// Remove sufixo -admin
	normalizedCluster := strings.TrimSuffix(cluster, "-admin")

	log.Info().
		Str("cluster", normalizedCluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Msg("Buscando snapshot atual via Prometheus")

	// Buscar métricas atuais (query instantâneo)
	snapshot, err := h.engine.GetHPAMetrics(normalizedCluster, namespace, hpaName)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", normalizedCluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Msg("Erro ao buscar snapshot atual do Prometheus")

		c.JSON(500, gin.H{
			"error": fmt.Sprintf("Failed to fetch current metrics: %v", err),
		})
		return
	}

	// Enriquecer com targets do HPA (busca via Kubernetes API)
	if err := h.enrichSnapshotWithTargets(c.Request.Context(), normalizedCluster, namespace, hpaName, snapshot); err != nil {
		log.Warn().Err(err).Msg("Falha ao buscar targets do HPA (continuando sem targets)")
	}

	// Converter para formato API
	apiSnapshot := gin.H{
		"cluster":          snapshot.Cluster,
		"namespace":        snapshot.Namespace,
		"hpa_name":         snapshot.Name,
		"timestamp":        snapshot.Timestamp.Format(time.RFC3339),
		"cpu_current":      snapshot.CPUCurrent,
		"cpu_target":       snapshot.CPUTarget,
		"cpu_request":      snapshot.CPURequest,
		"cpu_limit":        snapshot.CPULimit,
		"memory_current":   snapshot.MemoryCurrent,
		"memory_target":    snapshot.MemoryTarget,
		"memory_request":   snapshot.MemoryRequest,
		"memory_limit":     snapshot.MemoryLimit,
		"replicas_current": snapshot.CurrentReplicas,
		"replicas_desired": snapshot.DesiredReplicas,
		"replicas_min":     snapshot.MinReplicas,
		"replicas_max":     snapshot.MaxReplicas,
	}

	c.JSON(200, gin.H{
		"cluster":   cluster,
		"namespace": namespace,
		"hpa_name":  hpaName,
		"snapshot":  apiSnapshot,
		"source":    "prometheus",
	})
}

// ClearCache limpa o cache de um HPA específico
// DELETE /api/v1/monitoring/v2/cache/:cluster/:namespace/:hpaName
func (h *MonitoringHandlerV2) ClearCache(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	hpaName := c.Param("hpaName")

	// Remove sufixo -admin
	normalizedCluster := strings.TrimSuffix(cluster, "-admin")

	h.engine.ClearCacheForHPA(normalizedCluster, namespace, hpaName)

	log.Info().
		Str("cluster", normalizedCluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Msg("Cache do HPA limpo")

	c.JSON(200, gin.H{
		"status":  "success",
		"message": "Cache cleared successfully",
		"cluster": normalizedCluster,
	})
}

// Helper: Converter dados históricos para formato API
func convertHistoricalToAPI(historicalData map[string]interface{}, cpuTarget, memoryTarget float64) []gin.H {
	apiSnapshots := make([]gin.H, 0)

	// Extrair query results para cada métrica
	replicasData, _ := historicalData["replicas"].(*client.QueryRangeResult)
	desiredReplicasData, _ := historicalData["desired_replicas"].(*client.QueryRangeResult)
	minReplicasData, _ := historicalData["min_replicas"].(*client.QueryRangeResult)
	maxReplicasData, _ := historicalData["max_replicas"].(*client.QueryRangeResult)
	cpuData, _ := historicalData["cpu"].(*client.QueryRangeResult)
	memoryData, _ := historicalData["memory"].(*client.QueryRangeResult)
	cpuRequestData, _ := historicalData["cpu_request"].(*client.QueryRangeResult)
	cpuLimitData, _ := historicalData["cpu_limit"].(*client.QueryRangeResult)
	memoryRequestData, _ := historicalData["memory_request"].(*client.QueryRangeResult)
	memoryLimitData, _ := historicalData["memory_limit"].(*client.QueryRangeResult)

	// Log de debug para ver o que chegou
	log.Debug().
		Bool("has_replicas", replicasData != nil).
		Bool("has_cpu", cpuData != nil).
		Bool("has_memory", memoryData != nil).
		Msg("Dados históricos recebidos")

	if replicasData != nil {
		log.Debug().Int("replicas_count", len(replicasData.Data.Result)).Msg("Resultados de réplicas")
	}
	if cpuData != nil {
		log.Debug().Int("cpu_count", len(cpuData.Data.Result)).Msg("Resultados de CPU")
	}
	if memoryData != nil {
		log.Debug().Int("memory_count", len(memoryData.Data.Result)).Msg("Resultados de memória")
	}

	if replicasData == nil || len(replicasData.Data.Result) == 0 {
		log.Warn().Msg("Sem dados de réplicas no range query")
		return apiSnapshots
	}

	// Usar réplicas como base temporal (deve ter todos os timestamps)
	for _, valuePair := range replicasData.Data.Result[0].Values {
		timestamp := int64(valuePair[0].(float64))
		replicasStr := valuePair[1].(string)
		replicas := 0
		fmt.Sscanf(replicasStr, "%d", &replicas)

		// Converter timestamp Unix para ISO8601 string (compatível com JavaScript)
		timestampISO := time.Unix(timestamp, 0).Format(time.RFC3339)

		snapshot := gin.H{
			"timestamp":        timestampISO, // ISO8601 string (ex: "2025-11-18T18:01:28-03:00")
			"replicas":         replicas,
			"replicas_desired": 0,
			"replicas_min":     0,
			"replicas_max":     0,
			"cpu_current":      0.0,
			"cpu_target":       cpuTarget,
			"cpu_request":      "", // String vazia (será preenchida)
			"cpu_limit":        "", // String vazia (será preenchida)
			"memory_current":   0.0,
			"memory_target":    memoryTarget,
			"memory_request":   "", // String vazia (será preenchida)
			"memory_limit":     "", // String vazia (será preenchida)
		}

		// Buscar CPU correspondente (timestamp matching com tolerância de ±30s)
		cpuFound := false
		if cpuData != nil && len(cpuData.Data.Result) > 0 {
			for _, cpuPair := range cpuData.Data.Result[0].Values {
				cpuTimestamp := int64(cpuPair[0].(float64))
				if abs(cpuTimestamp-timestamp) <= 30 {
					cpuStr := cpuPair[1].(string)
					var cpuValue float64
					fmt.Sscanf(cpuStr, "%f", &cpuValue)
					snapshot["cpu_current"] = cpuValue
					cpuFound = true
					break
				}
			}
		}

		// Log se não encontrou CPU (apenas primeiro snapshot)
		if !cpuFound && len(apiSnapshots) == 0 {
			cpuResultsCount := 0
			if cpuData != nil {
				cpuResultsCount = len(cpuData.Data.Result)
			}
			log.Debug().
				Int64("timestamp", timestamp).
				Int("cpu_results", cpuResultsCount).
				Msg("CPU não encontrado para timestamp")
		}

		// Buscar Desired Replicas correspondente
		if desiredReplicasData != nil && len(desiredReplicasData.Data.Result) > 0 {
			for _, desiredPair := range desiredReplicasData.Data.Result[0].Values {
				desiredTimestamp := int64(desiredPair[0].(float64))
				if abs(desiredTimestamp-timestamp) <= 30 {
					desiredStr := desiredPair[1].(string)
					var desiredValue int
					fmt.Sscanf(desiredStr, "%d", &desiredValue)
					snapshot["replicas_desired"] = desiredValue
					break
				}
			}
		}

		// Buscar Min Replicas correspondente
		if minReplicasData != nil && len(minReplicasData.Data.Result) > 0 {
			for _, minPair := range minReplicasData.Data.Result[0].Values {
				minTimestamp := int64(minPair[0].(float64))
				if abs(minTimestamp-timestamp) <= 30 {
					minStr := minPair[1].(string)
					var minValue int
					fmt.Sscanf(minStr, "%d", &minValue)
					snapshot["replicas_min"] = minValue
					break
				}
			}
		}

		// Buscar Max Replicas correspondente
		if maxReplicasData != nil && len(maxReplicasData.Data.Result) > 0 {
			for _, maxPair := range maxReplicasData.Data.Result[0].Values {
				maxTimestamp := int64(maxPair[0].(float64))
				if abs(maxTimestamp-timestamp) <= 30 {
					maxStr := maxPair[1].(string)
					var maxValue int
					fmt.Sscanf(maxStr, "%d", &maxValue)
					snapshot["replicas_max"] = maxValue
					break
				}
			}
		}

		// Buscar Memory correspondente
		if memoryData != nil && len(memoryData.Data.Result) > 0 {
			for _, memPair := range memoryData.Data.Result[0].Values {
				memTimestamp := int64(memPair[0].(float64))
				if abs(memTimestamp-timestamp) <= 30 {
					memStr := memPair[1].(string)
					var memValue float64
					fmt.Sscanf(memStr, "%f", &memValue)
					snapshot["memory_current"] = memValue
					break
				}
			}
		}

		// Buscar CPU Request correspondente
		if cpuRequestData != nil && len(cpuRequestData.Data.Result) > 0 {
			for _, reqPair := range cpuRequestData.Data.Result[0].Values {
				reqTimestamp := int64(reqPair[0].(float64))
				if abs(reqTimestamp-timestamp) <= 30 {
					reqStr := reqPair[1].(string)
					snapshot["cpu_request"] = formatCPU(reqStr) // Formatar: "0.384" → "384m"
					break
				}
			}
		}

		// Buscar CPU Limit correspondente
		if cpuLimitData != nil && len(cpuLimitData.Data.Result) > 0 {
			for _, limPair := range cpuLimitData.Data.Result[0].Values {
				limTimestamp := int64(limPair[0].(float64))
				if abs(limTimestamp-timestamp) <= 30 {
					limStr := limPair[1].(string)
					snapshot["cpu_limit"] = formatCPU(limStr) // Formatar: "0.512" → "512m"
					break
				}
			}
		}

		// Buscar Memory Request correspondente
		if memoryRequestData != nil && len(memoryRequestData.Data.Result) > 0 {
			for _, reqPair := range memoryRequestData.Data.Result[0].Values {
				reqTimestamp := int64(reqPair[0].(float64))
				if abs(reqTimestamp-timestamp) <= 30 {
					reqStr := reqPair[1].(string)
					snapshot["memory_request"] = formatMemory(reqStr) // Formatar: "268435456" → "256Mi"
					break
				}
			}
		}

		// Buscar Memory Limit correspondente
		if memoryLimitData != nil && len(memoryLimitData.Data.Result) > 0 {
			for _, limPair := range memoryLimitData.Data.Result[0].Values {
				limTimestamp := int64(limPair[0].(float64))
				if abs(limTimestamp-timestamp) <= 30 {
					limStr := limPair[1].(string)
					snapshot["memory_limit"] = formatMemory(limStr) // Formatar: "402653184" → "384Mi"
					break
				}
			}
		}

		apiSnapshots = append(apiSnapshots, snapshot)
	}

	log.Debug().
		Int("metrics", len(historicalData)).
		Int("snapshots", len(apiSnapshots)).
		Msg("Conversão de métricas históricas concluída")

	return apiSnapshots
}

// abs retorna o valor absoluto de um inteiro
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
