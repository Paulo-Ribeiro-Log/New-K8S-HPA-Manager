package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"

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

	// Gerar ID da sessão
	sessionID := uuid.New().String()

	// Executar em goroutine (long-running operation)
	go func() {
		ctx := context.Background()

		log.Info().
			Str("session_id", sessionID).
			Str("environment", string(req.Environment)).
			Int("clusters", len(req.Clusters)).
			Msg("Starting health check")

		results, err := h.orchestrator.ExecuteHealthCheck(ctx, req)
		if err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("Health check failed")
			return
		}

		log.Info().
			Str("session_id", sessionID).
			Int("total_results", len(results)).
			Msg("Health check completed")
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"success":    true,
		"session_id": sessionID,
		"message":    "Health check iniciado",
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

	log.Info().Str("session_id", sessionID).Msg("Client connected to progress stream")

	// Criar cliente SSE para esta sessão
	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(sessionID)

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Stream events
	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-client.Channel:
			if !ok {
				return false
			}

			// Marshal event to JSON
			c.SSEvent("progress", event)

			// Fechar stream se tipo for "complete" ou "error"
			if event.Type == "complete" || event.Type == "error" {
				return false
			}

			return true

		case <-c.Request.Context().Done():
			log.Info().Str("session_id", sessionID).Msg("Client disconnected from progress stream")
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
