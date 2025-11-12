package handlers

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/monitoring/analyzer"
	"k8s-hpa-manager/internal/monitoring/engine"
	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/scanner"
	"k8s-hpa-manager/internal/monitoring/storage"
)

// MonitoringHandler gerencia endpoints de monitoramento
type MonitoringHandler struct {
	engine       *engine.ScanEngine
	persistence  *storage.Persistence // FASE 4: Query SQLite direto
	anomalyChan  chan analyzer.Anomaly
	snapshotChan chan *models.HPASnapshot

	// Cache de anomalias em memória (até implementar storage)
	anomalies []analyzer.Anomaly
}

// NewMonitoringHandler cria novo handler de monitoramento
func NewMonitoringHandler(eng *engine.ScanEngine, persistence *storage.Persistence, anomalyChan chan analyzer.Anomaly, snapshotChan chan *models.HPASnapshot) *MonitoringHandler {
	h := &MonitoringHandler{
		engine:       eng,
		persistence:  persistence, // FASE 4: Referência ao SQLite
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

// GetMetrics retorna métricas históricas de um HPA (FASE 4: Query SQLite direto)
// GET /api/v1/monitoring/metrics/:cluster/:namespace/:hpaName?duration=5m
func (h *MonitoringHandler) GetMetrics(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	hpaName := c.Param("hpaName")
	duration := c.DefaultQuery("duration", "1h") // Default 1h (mais útil que 5m)

	// Remove sufixo -admin do cluster para buscar no SQLite
	// O frontend envia "akspriv-prod-admin", mas o SQLite salva "akspriv-prod"
	normalizedCluster := strings.TrimSuffix(cluster, "-admin")

	// Parse duration
	dur, err := time.ParseDuration(duration)
	if err != nil {
		c.JSON(400, gin.H{
			"error": "Invalid duration format. Use formats like: 5m, 1h, 24h",
		})
		return
	}

	// FASE 4: Query SQLite direto (SEM cache em memória)
	if h.persistence == nil {
		c.JSON(500, gin.H{
			"error": "Persistence not available",
		})
		return
	}

	// Calcular janela de tempo
	since := time.Now().Add(-dur)

	// Buscar snapshots do SQLite (dados atuais)
	snapshots, err := h.persistence.LoadSnapshots(normalizedCluster, namespace, hpaName, since)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", normalizedCluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Msg("Erro ao buscar snapshots do SQLite")

		c.JSON(500, gin.H{
			"error": "Failed to load snapshots from database",
		})
		return
	}

	// Buscar dados de ontem (comparison D-1)
	snapshotsYesterday, err := h.persistence.LoadSnapshotsYesterday(normalizedCluster, namespace, hpaName, dur)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", normalizedCluster).
			Str("namespace", namespace).
			Str("hpa", hpaName).
			Msg("Falha ao buscar snapshots de ontem (continuando sem comparação)")
		// Não é erro fatal, continua sem dados de ontem
		snapshotsYesterday = []models.HPASnapshot{}
	}

	// Converter para formato API
	apiSnapshots := make([]gin.H, 0, len(snapshots))
	for _, snap := range snapshots {
		apiSnapshots = append(apiSnapshots, gin.H{
			"cluster":          snap.Cluster,
			"namespace":        snap.Namespace,
			"hpa_name":         snap.Name,
			"timestamp":        snap.Timestamp.Format(time.RFC3339),
			"cpu_current":      snap.CPUCurrent,
			"cpu_target":       snap.CPUTarget,
			"memory_current":   snap.MemoryCurrent,
			"memory_target":    snap.MemoryTarget,
			"replicas_current": snap.CurrentReplicas,
			"replicas_desired": snap.DesiredReplicas,
			"replicas_min":     snap.MinReplicas,
			"replicas_max":     snap.MaxReplicas,
			// Recursos do Deployment (K8s API) - NOVO
			"cpu_request":    snap.CPURequest,
			"cpu_limit":      snap.CPULimit,
			"memory_request": snap.MemoryRequest,
			"memory_limit":   snap.MemoryLimit,
			// Extended metrics (Prometheus)
			"request_rate":     snap.RequestRate,
			"error_rate":       snap.ErrorRate,
			"p95_latency":      snap.P95Latency,
			"p99_latency":      snap.P99Latency,
			"network_rx_bytes": snap.NetworkRxBytes,
			"network_tx_bytes": snap.NetworkTxBytes,
		})
	}

	// Converter dados de ontem para formato API
	apiSnapshotsYesterday := make([]gin.H, 0, len(snapshotsYesterday))
	for _, snap := range snapshotsYesterday {
		apiSnapshotsYesterday = append(apiSnapshotsYesterday, gin.H{
			"cluster":          snap.Cluster,
			"namespace":        snap.Namespace,
			"hpa_name":         snap.Name,
			"timestamp":        snap.Timestamp.Format(time.RFC3339),
			"cpu_current":      snap.CPUCurrent,
			"cpu_target":       snap.CPUTarget,
			"memory_current":   snap.MemoryCurrent,
			"memory_target":    snap.MemoryTarget,
			"replicas_current": snap.CurrentReplicas,
			"replicas_desired": snap.DesiredReplicas,
			"replicas_min":     snap.MinReplicas,
			"replicas_max":     snap.MaxReplicas,
			// Recursos do Deployment (K8s API) - NOVO
			"cpu_request":    snap.CPURequest,
			"cpu_limit":      snap.CPULimit,
			"memory_request": snap.MemoryRequest,
			"memory_limit":   snap.MemoryLimit,
			// Extended metrics (Prometheus)
			"request_rate":     snap.RequestRate,
			"error_rate":       snap.ErrorRate,
			"p95_latency":      snap.P95Latency,
			"p99_latency":      snap.P99Latency,
			"network_rx_bytes": snap.NetworkRxBytes,
			"network_tx_bytes": snap.NetworkTxBytes,
		})
	}

	// Log para debug
	log.Debug().
		Str("cluster", normalizedCluster).
		Str("namespace", namespace).
		Str("hpa", hpaName).
		Dur("duration", dur).
		Int("count", len(snapshots)).
		Int("count_yesterday", len(snapshotsYesterday)).
		Msg("Métricas retornadas do SQLite (com comparação D-1)")

	c.JSON(200, gin.H{
		"cluster":             cluster,
		"namespace":           namespace,
		"hpa_name":            hpaName,
		"duration":            duration,
		"snapshots":           apiSnapshots,
		"snapshots_yesterday": apiSnapshotsYesterday,
		"count":               len(apiSnapshots),
		"count_yesterday":     len(apiSnapshotsYesterday),
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

	// Buscar mapeamento de portas do PriorityCollector
	portMapping := make(map[string]int)
	priorityCollector := h.engine.GetPriorityCollector()
	if priorityCollector != nil {
		portMapping = priorityCollector.GetPortMapping()
	}

	c.JSON(200, gin.H{
		"running":     running,
		"status":      status,
		"mode":        "individual",
		"interval":    "1m",
		"clusters":    len(clustersMap),
		"last_scan":   formatTime(lastScan),
		"total_scans": totalSnapshots,
		"port_info":   portMapping, // cluster -> porta
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
	// Buscar HPAs prioritários do PriorityCollector
	priorityCollector := h.engine.GetPriorityCollector()
	if priorityCollector == nil {
		c.JSON(200, gin.H{
			"targets": []gin.H{},
			"count":   0,
		})
		return
	}

	priorityHPAs := priorityCollector.GetPriorityHPAs()

	// Agrupar HPAs por cluster
	clusterMap := make(map[string]map[string][]string) // cluster -> namespace -> []hpa
	for _, hpa := range priorityHPAs {
		if _, exists := clusterMap[hpa.Cluster]; !exists {
			clusterMap[hpa.Cluster] = make(map[string][]string)
		}
		if _, exists := clusterMap[hpa.Cluster][hpa.Namespace]; !exists {
			clusterMap[hpa.Cluster][hpa.Namespace] = []string{}
		}
		clusterMap[hpa.Cluster][hpa.Namespace] = append(clusterMap[hpa.Cluster][hpa.Namespace], hpa.Name)
	}

	// Converter para formato de resposta
	apiTargets := make([]gin.H, 0)
	for cluster, namespaces := range clusterMap {
		var nsList []string
		var hpaList []string
		for ns, hpas := range namespaces {
			nsList = append(nsList, ns)
			hpaList = append(hpaList, hpas...)
		}

		apiTargets = append(apiTargets, gin.H{
			"cluster":    cluster,
			"namespaces": nsList,
			"hpas":       hpaList,
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

	// IMPORTANTE: Remove sufixo -admin do cluster para o scanner/portforward
	// O frontend envia "akspriv-prod-admin", mas o scanner precisa de "akspriv-prod"
	clusterName := strings.TrimSuffix(req.Cluster, "-admin")

	log.Info().
		Str("cluster_received", req.Cluster).
		Str("cluster_normalized", clusterName).
		Str("namespace", req.Namespace).
		Str("hpa", req.HPA).
		Msg("Adicionando HPA ao monitoramento")

	// Adiciona HPA ao PriorityCollector (com port-forward dedicado)
	priorityCollector := h.engine.GetPriorityCollector()
	if priorityCollector == nil {
		c.JSON(500, gin.H{
			"status":  "error",
			"message": "PriorityCollector not available",
		})
		return
	}

	if err := priorityCollector.AddPriorityHPA(clusterName, req.Namespace, req.HPA); err != nil {
		log.Error().
			Err(err).
			Str("cluster", clusterName).
			Str("namespace", req.Namespace).
			Str("hpa", req.HPA).
			Msg("Falha ao adicionar HPA ao PriorityCollector")

		c.JSON(500, gin.H{
			"status":  "error",
			"message": "Failed to add HPA to priority monitoring",
			"error":   err.Error(),
		})
		return
	}

	log.Info().
		Str("cluster", clusterName).
		Str("namespace", req.Namespace).
		Str("hpa", req.HPA).
		Msg("✅ HPA adicionado ao monitoramento prioritário")

	c.JSON(200, gin.H{
		"status":  "success",
		"message": "HPA added to priority monitoring successfully",
		"target": gin.H{
			"cluster":   clusterName,
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

// SyncMonitoredHPAs sincroniza lista completa de HPAs monitorados (reconciliação)
// POST /api/v1/monitoring/sync
func (h *MonitoringHandler) SyncMonitoredHPAs(c *gin.Context) {
	var req struct {
		HPAs []struct {
			Cluster   string `json:"cluster"`
			Namespace string `json:"namespace"`
			HPA       string `json:"hpa"`
		} `json:"hpas"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	log.Info().
		Int("hpas_count", len(req.HPAs)).
		Msg("🔄 Iniciando reconciliação de HPAs monitorados")

	// Construir mapa de HPAs desejados (frontend)
	// Normalizar cluster name removendo sufixo -admin para comparação consistente
	desiredHPAs := make(map[string]bool)
	for _, hpa := range req.HPAs {
		// Normalizar cluster name (remover -admin se presente)
		clusterName := strings.TrimSuffix(hpa.Cluster, "-admin")
		key := clusterName + "/" + hpa.Namespace + "/" + hpa.HPA
		desiredHPAs[key] = true
	}

	// Obter lista atual de targets do engine
	currentTargets := h.engine.GetTargets()

	// Construir mapa de HPAs atuais (backend)
	currentHPAs := make(map[string]bool)
	for _, target := range currentTargets {
		for _, ns := range target.Namespaces {
			for _, hpaName := range target.HPAs {
				key := target.Cluster + "/" + ns + "/" + hpaName
				currentHPAs[key] = true
			}
		}
	}

	// Calcular diferenças
	added := 0
	removed := 0

	// Adicionar HPAs que estão no frontend mas não no backend
	for _, hpa := range req.HPAs {
		// Normalizar cluster name (já normalizado no mapa desiredHPAs)
		clusterName := strings.TrimSuffix(hpa.Cluster, "-admin")
		key := clusterName + "/" + hpa.Namespace + "/" + hpa.HPA
		if !currentHPAs[key] {

			log.Info().
				Str("cluster", clusterName).
				Str("namespace", hpa.Namespace).
				Str("hpa", hpa.HPA).
				Msg("➕ Adicionando HPA ao monitoramento (reconciliação)")

			// CORREÇÃO: Adiciona diretamente ao PriorityCollector
			// Cada HPA monitorado DEVE ter port-forward dedicado + baseline
			if err := h.engine.GetPriorityCollector().AddPriorityHPA(clusterName, hpa.Namespace, hpa.HPA); err != nil {
				log.Error().
					Err(err).
					Str("cluster", clusterName).
					Str("namespace", hpa.Namespace).
					Str("hpa", hpa.HPA).
					Msg("❌ Erro ao adicionar HPA prioritário (reconciliação)")
			} else {
				added++
			}
		}
	}

	// Remover HPAs que estão no backend mas não no frontend
	for key := range currentHPAs {
		if !desiredHPAs[key] {
			parts := strings.Split(key, "/")
			if len(parts) == 3 {
				cluster := parts[0]

				log.Info().
					Str("cluster", cluster).
					Str("key", key).
					Msg("➖ Removendo HPA do monitoramento (reconciliação)")

				// Remove todo o cluster se não há mais HPAs
				// TODO: Implementar remoção granular de HPA individual
				h.engine.RemoveTarget(cluster)
				removed++
			}
		}
	}

	total := len(desiredHPAs)

	log.Info().
		Int("added", added).
		Int("removed", removed).
		Int("total", total).
		Msg("✅ Reconciliação concluída")

	c.JSON(200, gin.H{
		"status":  "success",
		"added":   added,
		"removed": removed,
		"total":   total,
	})
}
