package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
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

	// ✅ Map para armazenar funções de cancelamento de cada sessão
	cancelFuncs map[string]context.CancelFunc
	cancelMutex sync.RWMutex
}

// NewHealthCheckHandler cria um novo handler de health checking
func NewHealthCheckHandler(km *config.KubeConfigManager, orch *healthcheck.Orchestrator, tracker *sse.ProgressTracker) *HealthCheckHandler {
	return &HealthCheckHandler{
		kubeManager:  km,
		orchestrator: orch,
		tracker:      tracker,
		cancelFuncs:  make(map[string]context.CancelFunc), // ✅ Inicializar map
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

	// ✅ Criar contexto cancelável
	ctx, cancel := context.WithCancel(context.Background())

	// ✅ Armazenar função cancel (para permitir cancelamento via API)
	h.cancelMutex.Lock()
	h.cancelFuncs[baseSessionID] = cancel
	h.cancelMutex.Unlock()

	// Executar em goroutine (long-running operation)
	go func() {
		// ✅ Cleanup: remover cancel function ao finalizar
		defer func() {
			h.cancelMutex.Lock()
			delete(h.cancelFuncs, baseSessionID)
			h.cancelMutex.Unlock()
		}()

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
		"success":          true,
		"session_id":       baseSessionID,
		"cluster_sessions": clusterSessions, // Map: cluster -> sessionID
		"message":          "Health check iniciado",
	})
}

// Cancel cancela um health check em andamento
// DELETE /api/v1/healthcheck/cancel/:sessionId
func (h *HealthCheckHandler) Cancel(c *gin.Context) {
	sessionID := c.Param("sessionId")

	log.Info().Str("session_id", sessionID).Msg("Cancelling health check")

	// Buscar função de cancelamento
	h.cancelMutex.RLock()
	cancelFunc, exists := h.cancelFuncs[sessionID]
	h.cancelMutex.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Sessão não encontrada ou já finalizada",
			},
		})
		return
	}

	// Chamar cancel() - isso cancela o contexto e interrompe todas as goroutines
	cancelFunc()

	// Publicar evento de cancelamento via SSE
	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID:        sessionID,
		Type:      "error",
		Phase:     "cancelled",
		Message:   "Health check cancelado pelo usuário",
		Progress:  0,
		Status:    "critical",
		Timestamp: time.Now(),
	})

	log.Info().Str("session_id", sessionID).Msg("Health check cancelled successfully")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Health check cancelado com sucesso",
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
			if (event.Type == "complete" || event.Type == "error") {
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

// ProgressMultiplexed retorna stream SSE multiplexado de TODOS os clusters
// GET /api/v1/healthcheck/progress-multiplex?session={baseId}&clusters=cluster1,cluster2,cluster3
// Uma única conexão EventSource recebe eventos de todos os clusters
func (h *HealthCheckHandler) ProgressMultiplexed(c *gin.Context) {
	baseSessionID := c.Query("session")
	clustersParam := c.Query("clusters")

	if baseSessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{"message": "base session ID é obrigatório"},
		})
		return
	}

	if clustersParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{"message": "lista de clusters é obrigatória"},
		})
		return
	}

	// Parse clusters
	clusters := strings.Split(clustersParam, ",")
	if len(clusters) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{"message": "lista de clusters vazia"},
		})
		return
	}

	log.Info().
		Str("base_session_id", baseSessionID).
		Strs("clusters", clusters).
		Int("num_clusters", len(clusters)).
		Msg("[SSE Multiplex] Client connected to multiplexed progress stream")

	// Criar um client para CADA cluster session ID
	// Todos os eventos serão merged em um único stream
	clients := make([]*sse.Client, 0, len(clusters))
	for _, cluster := range clusters {
		clusterSessionID := fmt.Sprintf("%s-%s", baseSessionID, cluster)
		client := sse.NewClient(clusterSessionID)
		h.tracker.AddClient(client)
		clients = append(clients, client)
		log.Debug().
			Str("cluster", cluster).
			Str("cluster_session_id", clusterSessionID).
			Msg("[SSE Multiplex] Added client for cluster")
	}

	defer func() {
		for _, client := range clients {
			h.tracker.RemoveClient(client.ID)
		}
		log.Info().Str("base_session_id", baseSessionID).Msg("[SSE Multiplex] All clients removed from tracker")
	}()

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Track completions/errors to know when to close stream
	completedClusters := make(map[string]bool)

	// Stream events multiplexados
	c.Stream(func(w io.Writer) bool {
		// Use select para escutar TODOS os channels simultaneamente
		cases := make([]reflect.SelectCase, len(clients)+1)

		// Case 0: Context cancellation
		cases[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.Request.Context().Done())}

		// Cases 1+: Channels dos clients
		for i, client := range clients {
			cases[i+1] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(client.Channel)}
		}

		chosen, value, ok := reflect.Select(cases)

		// Case 0: Context cancelled
		if chosen == 0 {
			log.Info().Str("base_session_id", baseSessionID).Msg("[SSE Multiplex] Client disconnected")
			return false
		}

		// Cases 1+: Event from cluster client
		if !ok {
			// Channel fechado - um cluster terminou
			clientIdx := chosen - 1
			cluster := clusters[clientIdx]
			log.Warn().
				Str("base_session_id", baseSessionID).
				Str("cluster", cluster).
				Msg("[SSE Multiplex] Cluster channel closed")

			// Se todos os clusters completaram, fechar stream
			completedClusters[cluster] = true
			if len(completedClusters) == len(clusters) {
				log.Info().
					Str("base_session_id", baseSessionID).
					Msg("[SSE Multiplex] All clusters completed, closing stream")
				return false
			}
			return true
		}

		// Evento válido
		event := value.Interface().(sse.ProgressEvent)
		clientIdx := chosen - 1
		cluster := clusters[clientIdx]

		// ✅ IMPORTANTE: Adicionar identificador de cluster ao evento para frontend distribuir
		// Usar diretamente a variável 'cluster' ao invés de fazer split do ID
		event.Details = fmt.Sprintf("cluster:%s", cluster)

		log.Info().
			Str("base_session_id", baseSessionID).
			Str("cluster", cluster).
			Str("type", event.Type).
			Str("message", event.Message).
			Float64("progress", event.Progress).
			Msg("[SSE Multiplex] Sending event to client")

		// Send event
		c.SSEvent("progress", event)

		// Se cluster completou/falhou, marcar como completo
		if event.Type == "complete" || event.Type == "error" {
			completedClusters[cluster] = true
			log.Info().
				Str("cluster", cluster).
				Str("type", event.Type).
				Int("completed_count", len(completedClusters)).
				Int("total_clusters", len(clusters)).
				Msg("[SSE Multiplex] Cluster finished")

			// Se todos completaram, fechar stream
			if len(completedClusters) == len(clusters) {
				log.Info().
					Str("base_session_id", baseSessionID).
					Msg("[SSE Multiplex] All clusters completed, closing stream")
				return false
			}
		}

		return true
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

// GetEvents retorna eventos de progresso persistidos de um cluster
// GET /api/v1/healthcheck/events/:sessionId
func (h *HealthCheckHandler) GetEvents(c *gin.Context) {
	sessionID := c.Param("sessionId")

	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "session ID é obrigatório",
			},
		})
		return
	}

	log.Info().Str("session_id", sessionID).Msg("Fetching health check events")

	ctx := c.Request.Context()
	events, err := h.orchestrator.Storage().GetEvents(ctx, sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to get events")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Erro ao buscar eventos",
				"details": err.Error(),
			},
		})
		return
	}

	// Converter para formato frontend
	responseEvents := make([]gin.H, len(events))
	for i, e := range events {
		responseEvents[i] = gin.H{
			"id":        e.SessionID,
			"type":      e.Type,
			"phase":     e.Phase,
			"message":   e.Message,
			"progress":  e.Progress,
			"status":    e.Status,
			"timestamp": e.Timestamp.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"events":  responseEvents,
		"count":   len(responseEvents),
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
