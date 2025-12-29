package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/healthcheck"
	"k8s-hpa-manager/internal/web/sse"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// HealthCheckHandler gerencia requisições de health checking
type HealthCheckHandler struct {
	kubeManager  *config.KubeConfigManager
	orchestrator *healthcheck.Orchestrator
	tracker      *sse.ProgressTracker
}

// NewHealthCheckHandler cria um novo handler de health checking
func NewHealthCheckHandler(km *config.KubeConfigManager, orch *healthcheck.Orchestrator, tracker *sse.ProgressTracker) *HealthCheckHandler {
	return &HealthCheckHandler{
		kubeManager:  km,
		orchestrator: orch,
		tracker:      tracker,
	}
}

// Run executa um health check nos clusters especificados
// POST /api/v1/healthcheck/run
func (h *HealthCheckHandler) Run(c *gin.Context) {
	var req healthcheck.HealthCheckRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Dados inválidos",
				"details": err.Error(),
			},
		})
		return
	}

	// Validar request
	if err := h.validateRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": err.Error(),
			},
		})
		return
	}

	// Gerar ID da sessão base
	baseSessionID := uuid.New().String()

	// Resolver clusters para obter lista completa
	clusters, err := h.orchestrator.ResolveClusters(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": fmt.Sprintf("Failed to resolve clusters: %v", err),
			},
		})
		return
	}

	// Gerar mapeamento cluster -> sessionID
	clusterSessions := h.orchestrator.GetClusterSessionMapping(baseSessionID, clusters)

	// Executar em goroutine (long-running operation)
	go func() {
		ctx := context.Background()

		log.Info().
			Str("base_session_id", baseSessionID).
			Str("environment", string(req.Environment)).
			Int("clusters", len(clusters)).
			Msg("Starting health check")

		results, err := h.orchestrator.ExecuteHealthCheck(ctx, baseSessionID, req)
		if err != nil {
			log.Error().Err(err).Str("base_session_id", baseSessionID).Msg("Health check failed")

			// Publicar evento de erro via SSE para todos os clusters
			for _, sessionID := range clusterSessions {
				h.tracker.SendToClient(sessionID, sse.ProgressEvent{
					ID:        sessionID,
					Type:      "error",
					Phase:     "failed",
					Message:   fmt.Sprintf("Health check failed: %v", err),
					Progress:  0,
					Timestamp: time.Now(),
				})
			}
			return
		}

		log.Info().
			Str("base_session_id", baseSessionID).
			Int("total_results", len(results)).
			Msg("Health check completed")
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"success":         true,
		"session_id":      baseSessionID,
		"cluster_sessions": clusterSessions, // Map: cluster -> sessionID
		"message":         "Health check iniciado",
	})
}

// Progress retorna stream SSE de progresso do health check
// GET /api/v1/healthcheck/progress?session={id}
func (h *HealthCheckHandler) Progress(c *gin.Context) {
	sessionID := c.Query("session")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "session ID é obrigatório",
			},
		})
		return
	}

	log.Info().Str("session_id", sessionID).Msg("[SSE] Client connected to progress stream")

	// Criar cliente SSE para esta sessão
	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer func() {
		h.tracker.RemoveClient(sessionID)
		log.Info().Str("session_id", sessionID).Msg("[SSE] Client removed from tracker")
	}()

	log.Info().Str("session_id", sessionID).Msg("[SSE] Client added to tracker, waiting for events")

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Stream events
	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-client.Channel:
			if !ok {
				log.Warn().Str("session_id", sessionID).Msg("[SSE] Channel closed")
				return false
			}

			log.Info().
				Str("session_id", sessionID).
				Str("type", event.Type).
				Str("message", event.Message).
				Float64("progress", event.Progress).
				Msg("[SSE] Sending event to client")

			// Marshal event to JSON
			c.SSEvent("progress", event)

			// Fechar stream se tipo for "complete" ou "error"
			if event.Type == "complete" || event.Type == "error" {
				log.Info().Str("session_id", sessionID).Str("type", event.Type).Msg("[SSE] Closing stream (complete/error)")
				return false
			}

			return true

		case <-c.Request.Context().Done():
			log.Info().Str("session_id", sessionID).Msg("[SSE] Client disconnected from progress stream")
			return false
		}
	})
}

// History retorna histórico de health checks
// GET /api/v1/healthcheck/history?cluster=x&namespace=y
func (h *HealthCheckHandler) History(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")

	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Msg("Fetching health check history")

	results, err := h.orchestrator.GetHistory(c.Request.Context(), cluster, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao buscar histórico",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    results,
		"count":   len(results),
	})
}

// Get retorna um resultado específico de health check
// GET /api/v1/healthcheck/:id
func (h *HealthCheckHandler) Get(c *gin.Context) {
	resultID := c.Param("id")

	log.Info().Str("result_id", resultID).Msg("Fetching health check result")

	result, err := h.orchestrator.GetResult(c.Request.Context(), resultID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Resultado não encontrado",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// Delete deleta um resultado específico de health check
// DELETE /api/v1/healthcheck/:id
func (h *HealthCheckHandler) Delete(c *gin.Context) {
	resultID := c.Param("id")

	log.Info().Str("result_id", resultID).Msg("Deleting health check result")

	err := h.orchestrator.DeleteResult(c.Request.Context(), resultID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao deletar resultado",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Resultado deletado com sucesso",
	})
}

// Stats retorna estatísticas agregadas de health checks
// GET /api/v1/healthcheck/stats?cluster=x&days=7
func (h *HealthCheckHandler) Stats(c *gin.Context) {
	cluster := c.Query("cluster")
	days := c.DefaultQuery("days", "7")

	log.Info().
		Str("cluster", cluster).
		Str("days", days).
		Msg("Fetching health check stats")

	stats, err := h.orchestrator.GetStats(c.Request.Context(), cluster, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao buscar estatísticas",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

// validateRequest valida requisição de health check
func (h *HealthCheckHandler) validateRequest(req *healthcheck.HealthCheckRequest) error {
	// Validar ambiente OU lista de clusters
	if req.Environment == "" && len(req.Clusters) == 0 {
		return fmt.Errorf("deve especificar environment (prod/hlg/all) ou lista de clusters")
	}

	// Se ambiente especificado, validar valor
	if req.Environment != "" {
		validEnvs := map[string]bool{
			"prod": true,
			"hlg":  true,
			"all":  true,
		}
		if !validEnvs[req.Environment] {
			return fmt.Errorf("ambiente inválido: %s (válidos: prod, hlg, all)", req.Environment)
		}
	}

	// Validar timeout
	if req.Timeout <= 0 {
		req.Timeout = 10 // Default 10 segundos
	}
	if req.Timeout > 120 {
		return fmt.Errorf("timeout máximo é 120 segundos")
	}

	// Pelo menos um tipo de check deve estar habilitado
	if !req.CheckDeployments && !req.CheckServices && !req.CheckConfigs {
		return fmt.Errorf("pelo menos um tipo de check deve estar habilitado")
	}

	return nil
}
