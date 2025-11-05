package handlers

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/monitoring/analyzer"
	"k8s-hpa-manager/internal/monitoring/engine"
	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/scanner"
)

// MonitoringHandler gerencia endpoints de monitoramento
type MonitoringHandler struct {
	engine       *engine.ScanEngine
	anomalyChan  chan analyzer.Anomaly
	snapshotChan chan *models.HPASnapshot

	// Cache de anomalias em memória (até implementar storage)
	anomalies []analyzer.Anomaly
}

// NewMonitoringHandler cria novo handler de monitoramento
func NewMonitoringHandler(eng *engine.ScanEngine, anomalyChan chan analyzer.Anomaly, snapshotChan chan *models.HPASnapshot) *MonitoringHandler {
	h := &MonitoringHandler{
		engine:       eng,
		anomalyChan:  anomalyChan,
		snapshotChan: snapshotChan,
		anomalies:    make([]analyzer.Anomaly, 0),
	}

	// Goroutine para coletar anomalias do canal
	go h.collectAnomalies()

	return h
}

// collectAnomalies coleta anomalias do canal em background
func (h *MonitoringHandler) collectAnomalies() {
	for anomaly := range h.anomalyChan {
		// Adicionar anomalia ao cache (limitar a 1000 últimas)
		h.anomalies = append(h.anomalies, anomaly)
		if len(h.anomalies) > 1000 {
			h.anomalies = h.anomalies[len(h.anomalies)-1000:]
		}
	}
}

// GetMetrics retorna métricas históricas de um HPA
// GET /api/v1/monitoring/metrics/:cluster/:namespace/:hpaName?duration=5m
func (h *MonitoringHandler) GetMetrics(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	hpaName := c.Param("hpaName")
	duration := c.DefaultQuery("duration", "5m")

	// Remove sufixo -admin do cluster para buscar no cache
	// O cache usa o nome sem -admin, mas o frontend envia com -admin
	cacheCluster := strings.TrimSuffix(cluster, "-admin")

	// Parse duration
	dur, err := time.ParseDuration(duration)
	if err != nil {
		c.JSON(400, gin.H{
			"error": "Invalid duration format. Use formats like: 5m, 1h, 24h",
		})
		return
	}

	// Buscar time series data do cache
	cache := h.engine.GetCache()
	if cache == nil {
		c.JSON(500, gin.H{
			"error": "Cache not available",
		})
		return
	}

	tsData := cache.Get(cacheCluster, namespace, hpaName)
	if tsData == nil {
		c.JSON(200, gin.H{
			"cluster":   cluster,
			"namespace": namespace,
			"hpa_name":  hpaName,
			"duration":  duration,
			"snapshots": []gin.H{},
			"count":     0,
			"message":   "No data available for this HPA. Start monitoring to collect metrics.",
		})
		return
	}

	// Filtrar snapshots pelos últimos X minutos
	since := time.Now().Add(-dur)
	apiSnapshots := make([]gin.H, 0)

	tsData.RLock()
	for _, snap := range tsData.Snapshots {
		if snap.Timestamp.After(since) {
			apiSnapshots = append(apiSnapshots, gin.H{
				"cluster":           snap.Cluster,
				"namespace":         snap.Namespace,
				"hpa_name":          snap.Name,
				"timestamp":         snap.Timestamp.Format(time.RFC3339),
				"cpu_current":       snap.CPUCurrent,
				"cpu_target":        snap.CPUTarget,
				"memory_current":    snap.MemoryCurrent,
				"memory_target":     snap.MemoryTarget,
				"replicas_current":  snap.CurrentReplicas,
				"replicas_desired":  snap.DesiredReplicas,
				"replicas_min":      snap.MinReplicas,
				"replicas_max":      snap.MaxReplicas,
			})
		}
	}
	tsData.RUnlock()

	c.JSON(200, gin.H{
		"cluster":   cluster,
		"namespace": namespace,
		"hpa_name":  hpaName,
		"duration":  duration,
		"snapshots": apiSnapshots,
		"count":     len(apiSnapshots),
	})
}

// GetAnomalies retorna anomalias detectadas
// GET /api/v1/monitoring/anomalies?cluster=X&severity=critical
func (h *MonitoringHandler) GetAnomalies(c *gin.Context) {
	cluster := c.Query("cluster")
	severityParam := c.DefaultQuery("severity", "all")

	// Filtrar anomalias
	filtered := make([]gin.H, 0)
	for _, anomaly := range h.anomalies {
		// Filtro por cluster
		if cluster != "" && anomaly.Cluster != cluster {
			continue
		}

		// Filtro por severidade
		if severityParam != "all" {
			severityStr := severityToString(anomaly.Severity)
			if severityStr != severityParam {
				continue
			}
		}

		// Converter para formato API
		filtered = append(filtered, gin.H{
			"id":               generateAnomalyID(anomaly),
			"cluster":          anomaly.Cluster,
			"namespace":        anomaly.Namespace,
			"hpa_name":         anomaly.HPAName,
			"type":             string(anomaly.Type),
			"severity":         severityToString(anomaly.Severity),
			"detected_at":      anomaly.Timestamp.Format(time.RFC3339),
			"duration_seconds": 0, // Não temos duração na estrutura atual
			"message":          anomaly.Message,
			"details":          gin.H{"description": anomaly.Description},
			"resolved":         false, // Não temos flag de resolved
			"resolved_at":      nil,
		})
	}

	c.JSON(200, gin.H{
		"cluster":   cluster,
		"severity":  severityParam,
		"anomalies": filtered,
		"count":     len(filtered),
	})
}

// GetHealth retorna status de saúde de um HPA
// GET /api/v1/monitoring/health/:cluster/:namespace/:hpaName
func (h *MonitoringHandler) GetHealth(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	hpaName := c.Param("hpaName")

	// Remove sufixo -admin do cluster para buscar anomalias
	cacheCluster := strings.TrimSuffix(cluster, "-admin")

	// Buscar anomalias recentes deste HPA (últimas 24h)
	recentAnomalies := make([]gin.H, 0)
	cutoff := time.Now().Add(-24 * time.Hour)

	criticalCount := 0
	highCount := 0
	mediumCount := 0

	for _, anomaly := range h.anomalies {
		if anomaly.Cluster != cacheCluster || anomaly.Namespace != namespace || anomaly.HPAName != hpaName {
			continue
		}
		if anomaly.Timestamp.Before(cutoff) {
			continue
		}

		// Contar por severidade
		switch anomaly.Severity {
		case models.SeverityCritical:
			criticalCount++
		case models.SeverityWarning:
			highCount++
		case models.SeverityInfo:
			mediumCount++
		}

		recentAnomalies = append(recentAnomalies, gin.H{
			"id":               generateAnomalyID(anomaly),
			"type":             string(anomaly.Type),
			"severity":         severityToString(anomaly.Severity),
			"detected_at":      anomaly.Timestamp.Format(time.RFC3339),
			"duration_seconds": 0,
			"message":          anomaly.Message,
			"resolved":         false,
		})
	}

	// Calcular health status
	status := "healthy"
	score := 100
	recommendations := make([]string, 0)

	if criticalCount > 0 {
		status = "critical"
		score = 0
		recommendations = append(recommendations, "Anomalias críticas detectadas - ação imediata necessária")
	} else if highCount > 2 {
		status = "critical"
		score = 20
		recommendations = append(recommendations, "Múltiplas anomalias de alta severidade")
	} else if highCount > 0 {
		status = "warning"
		score = 50
		recommendations = append(recommendations, "Anomalias de alta severidade detectadas")
	} else if mediumCount > 5 {
		status = "warning"
		score = 60
		recommendations = append(recommendations, "Muitas anomalias de média severidade")
	} else if mediumCount > 0 {
		score = 80
	}

	c.JSON(200, gin.H{
		"cluster":         cluster,
		"namespace":       namespace,
		"hpa_name":        hpaName,
		"status":          status,
		"score":           score,
		"anomalies":       recentAnomalies,
		"recommendations": recommendations,
	})
}

// GetStatus retorna status do monitoring engine
// GET /api/v1/monitoring/status
func (h *MonitoringHandler) GetStatus(c *gin.Context) {
	running := h.engine.IsRunning()
	paused := h.engine.IsPaused()

	// Buscar estatísticas do cache
	cache := h.engine.GetCache()
	var totalSnapshots int
	var lastScan *time.Time

	if cache != nil {
		stats := cache.Stats()
		totalSnapshots = stats.TotalSnapshots

		// Buscar último snapshot timestamp
		allData := cache.GetAll()
		for _, tsData := range allData {
			tsData.RLock()
			if len(tsData.Snapshots) > 0 {
				lastSnapshot := tsData.Snapshots[len(tsData.Snapshots)-1]
				if lastScan == nil || lastSnapshot.Timestamp.After(*lastScan) {
					lastScan = &lastSnapshot.Timestamp
				}
			}
			tsData.RUnlock()
		}
	}

	status := "stopped"
	if running {
		if paused {
			status = "paused"
		} else {
			status = "running"
		}
	}

	// Extrair clusters únicos
	clustersMap := make(map[string]bool)
	if cache != nil {
		allData := cache.GetAll()
		for _, tsData := range allData {
			parts := strings.Split(tsData.HPAKey, "/")
			if len(parts) >= 3 {
				clustersMap[parts[0]] = true
			}
		}
	}

	c.JSON(200, gin.H{
		"running":     running,
		"status":      status,
		"mode":        "individual",
		"interval":    "1m",
		"clusters":    len(clustersMap),
		"last_scan":   formatTime(lastScan),
		"total_scans": totalSnapshots,
	})
}

// Start inicia o monitoring engine
// POST /api/v1/monitoring/start
func (h *MonitoringHandler) Start(c *gin.Context) {
	if h.engine.IsRunning() {
		c.JSON(200, gin.H{
			"status":  "already_running",
			"message": "Monitoring engine is already running",
		})
		return
	}

	err := h.engine.Start()
	if err != nil {
		c.JSON(500, gin.H{
			"status":  "error",
			"message": "Failed to start monitoring engine",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"status":  "started",
		"message": "Monitoring engine started successfully",
	})
}

// Stop para o monitoring engine
// POST /api/v1/monitoring/stop
func (h *MonitoringHandler) Stop(c *gin.Context) {
	if !h.engine.IsRunning() {
		c.JSON(200, gin.H{
			"status":  "already_stopped",
			"message": "Monitoring engine is not running",
		})
		return
	}

	err := h.engine.Stop()
	if err != nil {
		c.JSON(500, gin.H{
			"status":  "error",
			"message": "Failed to stop monitoring engine",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"status":  "stopped",
		"message": "Monitoring engine stopped successfully",
	})
}

// Helper functions

func formatTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

func severityToString(s models.AlertSeverity) string {
	switch s {
	case models.SeverityCritical:
		return "critical"
	case models.SeverityWarning:
		return "warning"
	case models.SeverityInfo:
		return "info"
	default:
		return "info"
	}
}

func generateAnomalyID(a analyzer.Anomaly) string {
	// Gerar ID simples baseado em timestamp e HPA
	return a.Cluster + "-" + a.Namespace + "-" + a.HPAName + "-" + a.Timestamp.Format("20060102150405")
}

// GetTargets retorna lista de targets sendo monitorados
// GET /api/v1/monitoring/targets
func (h *MonitoringHandler) GetTargets(c *gin.Context) {
	targets := h.engine.GetTargets()

	apiTargets := make([]gin.H, 0)
	for _, target := range targets {
		apiTargets = append(apiTargets, gin.H{
			"cluster":     target.Cluster,
			"namespaces":  target.Namespaces,
			"deployments": target.Deployments,
			"hpas":        target.HPAs,
		})
	}

	c.JSON(200, gin.H{
		"targets": apiTargets,
		"count":   len(apiTargets),
	})
}

// AddTarget adiciona cluster/namespace/HPAs para monitorar
// POST /api/v1/monitoring/targets
// Body: { "cluster": "...", "namespaces": [...], "hpas": [...] }
func (h *MonitoringHandler) AddTarget(c *gin.Context) {
	var req struct {
		Cluster     string   `json:"cluster" binding:"required"`
		Namespaces  []string `json:"namespaces"`
		Deployments []string `json:"deployments"`
		HPAs        []string `json:"hpas"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"status":  "error",
			"message": "Invalid request body",
			"error":   err.Error(),
		})
		return
	}

	target := scanner.ScanTarget{
		Cluster:     req.Cluster,
		Namespaces:  req.Namespaces,
		Deployments: req.Deployments,
		HPAs:        req.HPAs,
	}

	h.engine.AddTarget(target)

	c.JSON(200, gin.H{
		"status":  "success",
		"message": "Target added successfully",
		"target": gin.H{
			"cluster":     target.Cluster,
			"namespaces":  target.Namespaces,
			"deployments": target.Deployments,
			"hpas":        target.HPAs,
		},
	})
}

// AddHPA adiciona um HPA específico para monitoramento
// POST /api/v1/monitoring/hpa
// Body: { "cluster": "...", "namespace": "...", "hpa": "..." }
func (h *MonitoringHandler) AddHPA(c *gin.Context) {
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

	// Criar target com HPA específico
	target := scanner.ScanTarget{
		Cluster:    req.Cluster,
		Namespaces: []string{req.Namespace},
		HPAs:       []string{req.HPA},
	}

	h.engine.AddTarget(target)

	c.JSON(200, gin.H{
		"status":  "success",
		"message": "HPA added to monitoring successfully",
		"target": gin.H{
			"cluster":   target.Cluster,
			"namespace": req.Namespace,
			"hpa":       req.HPA,
		},
	})
}

// RemoveTarget remove cluster do monitoring
// DELETE /api/v1/monitoring/targets/:cluster
func (h *MonitoringHandler) RemoveTarget(c *gin.Context) {
	cluster := c.Param("cluster")

	if cluster == "" {
		c.JSON(400, gin.H{
			"status":  "error",
			"message": "Cluster parameter is required",
		})
		return
	}

	h.engine.RemoveTarget(cluster)

	c.JSON(200, gin.H{
		"status":  "success",
		"message": "Target removed successfully",
		"cluster": cluster,
	})
}
