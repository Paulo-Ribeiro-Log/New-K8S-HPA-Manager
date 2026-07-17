package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/healthcheck"
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/web/sse"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type cancelEntry struct {
	cancel          context.CancelFunc
	clusterSessions map[string]string
}

// HealthCheckHandler gerencia requisições de health checking
type HealthCheckHandler struct {
	kubeManager  *config.KubeConfigManager
	orchestrator *healthcheck.Orchestrator
	tracker      *sse.ProgressTracker
	tokensStore  *storage.UserTokensStore
	aiHandler    *AIDiagnosticsHandler // para análise AI de itens correlacionados (opcional)

	// ✅ Map para armazenar funções de cancelamento de cada sessão
	cancelFuncs map[string]*cancelEntry
	cancelMutex sync.RWMutex
}

// NewHealthCheckHandler cria um novo handler de health checking
func NewHealthCheckHandler(km *config.KubeConfigManager, orch *healthcheck.Orchestrator, tracker *sse.ProgressTracker, tokensStore *storage.UserTokensStore, aiHandler *AIDiagnosticsHandler) *HealthCheckHandler {
	return &HealthCheckHandler{
		kubeManager:  km,
		orchestrator: orch,
		tracker:      tracker,
		tokensStore:  tokensStore,
		aiHandler:    aiHandler,
		cancelFuncs:  make(map[string]*cancelEntry), // ✅ Inicializar map
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

	// Popular credenciais Dynatrace do perfil do usuário (se check_dynatrace ou check_oneagent_signals solicitado)
	if (req.CheckDynatrace || req.CheckOneAgentSignals) && req.AIEmail != "" && h.tokensStore != nil {
		if tokens, err := h.tokensStore.GetTokens(req.AIEmail); err == nil && tokens != nil {
			req.DynatraceURL = tokens.DynatraceURL
			req.DynatraceToken = tokens.DynatraceToken
			req.DynatraceTagFilter = tokens.DynatraceTagFilter
		}
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
	sessionsCopy := make(map[string]string, len(clusterSessions))
	for cluster, session := range clusterSessions {
		sessionsCopy[cluster] = session
	}

	h.cancelMutex.Lock()
	h.cancelFuncs[baseSessionID] = &cancelEntry{
		cancel:          cancel,
		clusterSessions: sessionsCopy,
	}
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
	h.cancelMutex.Lock()
	entry, exists := h.cancelFuncs[sessionID]
	if exists {
		delete(h.cancelFuncs, sessionID)
	}
	h.cancelMutex.Unlock()

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
	entry.cancel()

	// Publicar evento de cancelamento via SSE para cada cluster
	for cluster, clusterSessionID := range entry.clusterSessions {
		h.tracker.SendToClient(clusterSessionID, sse.ProgressEvent{
			ID:        clusterSessionID,
			Type:      "error",
			Phase:     "cancelled",
			Message:   "Health check cancelado pelo usuário",
			Progress:  0,
			Status:    "critical",
			Details:   fmt.Sprintf("cluster:%s", cluster),
			Timestamp: time.Now(),
		})
	}

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

	// ✅ REPLAY BUFFER: Enviar eventos que ocorreram antes do cliente conectar
	replayEvents := h.tracker.GetReplayEvents(sessionID)
	if len(replayEvents) > 0 {
		log.Info().
			Str("session_id", sessionID).
			Int("replay_count", len(replayEvents)).
			Msg("[SSE] Enviando eventos do replay buffer")

		for _, event := range replayEvents {
			c.SSEvent("progress", event)
			c.Writer.Flush()

			// Se já tinha evento de conclusão no buffer, não precisa aguardar mais
			if event.Type == "complete" || event.Type == "error" {
				log.Info().Str("session_id", sessionID).Msg("[SSE] Replay buffer continha evento final - fechando stream")
				return
			}
		}
	}

	// Stream eventos em tempo real
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

// ProgressMultiplexed retorna stream SSE multiplexado de TODOS os clusters
// GET /api/v1/healthcheck/progress-multiplex?session={baseId}&clusters=cluster1,cluster2,cluster3
// Uma única conexão EventSource recebe eventos de todos os clusters
func (h *HealthCheckHandler) ProgressMultiplexed(c *gin.Context) {
	baseSessionID := c.Query("session")
	clustersParam := c.Query("clusters")

	if baseSessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"message": "base session ID é obrigatório"},
		})
		return
	}

	if clustersParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"message": "lista de clusters é obrigatória"},
		})
		return
	}

	// Parse clusters
	clusters := strings.Split(clustersParam, ",")
	if len(clusters) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"message": "lista de clusters vazia"},
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

// DashboardMetrics retorna métricas para o dashboard visual de health checking
// GET /api/v1/healthcheck/dashboard?days=7
func (h *HealthCheckHandler) DashboardMetrics(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "7")
	days := 7
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
		days = d
	}

	log.Info().
		Int("days", days).
		Msg("Fetching health check dashboard metrics")

	metrics, err := h.orchestrator.GetDashboardMetrics(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao buscar métricas do dashboard",
				"details": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    metrics,
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
	if !req.CheckDeployments && !req.CheckServices && !req.CheckConfigs && !req.CheckEvents && !req.CheckHPAs {
		return fmt.Errorf("pelo menos um tipo de check deve estar habilitado")
	}

	return nil
}

// AnalyzeCorrelated realiza análise AI de um item correlacionado K8s ↔ Dynatrace.
// Recebe o contexto completo no body (K8s issues + DT problems) e retorna o diagnóstico.
// POST /api/v1/healthcheck/correlated/analyze
func (h *HealthCheckHandler) AnalyzeCorrelated(c *gin.Context) {
	if h.aiHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI não configurado neste servidor"})
		return
	}

	var req struct {
		AIEmail string                           `json:"ai_email"`
		Item    healthcheck.CorrelatedHealthItem `json:"item"`
		Nodes   []healthcheck.NodeHealth         `json:"nodes,omitempty"` // contexto de capacidade do cluster (opcional — Fase 2)
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AIEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ai_email e item são obrigatórios"})
		return
	}

	provider, err := h.aiHandler.GetProviderForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	prompt := buildCorrelatedItemPrompt(req.Item, req.Nodes)

	analysis, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha na análise AI: " + err.Error()})
		return
	}

	log.Info().
		Str("ai_email", req.AIEmail).
		Str("workload", req.Item.WorkloadName).
		Str("namespace", req.Item.Namespace).
		Str("cluster", req.Item.Cluster).
		Bool("correlated", req.Item.Correlated).
		Msg("[HealthCheck] Análise AI de item correlacionado concluída")

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"analysis":    analysis,
		"analyzed_at": time.Now(),
	})
}

// buildCorrelatedItemPrompt monta prompt combinado com contexto K8s + Dynatrace para AI
// severityEmoji retorna o emoji de severidade usado nos relatórios de IA do Health Check —
// mesma convenção visual de relatórios de incidente (Davis AI e afins): 🔴 crítico, 🟠 alto,
// 🟡 médio, 🟢 baixo/informativo.
func severityEmoji(sev healthcheck.Severity) string {
	switch sev {
	case healthcheck.SeverityCritical:
		return "🔴"
	case healthcheck.SeverityHigh:
		return "🟠"
	case healthcheck.SeverityMedium:
		return "🟡"
	default:
		return "🟢"
	}
}

// daysOpen formata há quantos dias um problem Dynatrace está aberto, a partir de StartTime.
func daysOpen(start time.Time) string {
	if start.IsZero() {
		return "data de início desconhecida"
	}
	days := int(time.Since(start).Hours() / 24)
	if days <= 0 {
		return "aberto hoje"
	}
	if days == 1 {
		return "1 dia em aberto"
	}
	return fmt.Sprintf("%d dias em aberto (desde %s)", days, start.Format("02/01/2006"))
}

// chronicityLine formata a linha de crônico/agudo de um issue K8s a partir de Chronicity — vazio
// quando ainda não há histórico suficiente (evento visto pela primeira vez nesta execução).
func chronicityLine(issue healthcheck.CorrelatedK8sIssue) string {
	if issue.Chronicity == nil {
		return ""
	}
	if issue.Chronicity.IsChronic {
		return fmt.Sprintf("  - ⚠️ **Problema crônico**: %d ocorrência(s) acumulada(s) desde %s — não é um evento pontual, é recorrente.\n",
			issue.Chronicity.CumulativeCount, issue.Chronicity.FirstSeenEver.Format("02/01/2006"))
	}
	return fmt.Sprintf("  - Agudo: %d ocorrência(s) desde %s (dentro da janela de retenção do apiserver).\n",
		issue.Chronicity.CumulativeCount, issue.Chronicity.FirstSeenEver.Format("02/01/2006"))
}

// resourceVerdictLine formata a linha de uso real vs. request configurado de um issue de
// Deployment — ResourceVerdict só vem preenchido quando o histórico via Prometheus rodou
// (CheckResourceHistory), mas CPUUsagePercent/MemoryUsagePercent (snapshot ao vivo do
// metrics-server) podem estar disponíveis mesmo sem isso.
func resourceVerdictLine(issue healthcheck.CorrelatedK8sIssue) string {
	if issue.ResourceVerdict == "" || issue.ResourceVerdict == "ok" {
		return ""
	}
	usage := fmt.Sprintf("CPU %.0f%% / memória %.0f%% do request (snapshot ao vivo)", issue.CPUUsagePercent, issue.MemoryUsagePercent)
	switch issue.ResourceVerdict {
	case "oom_risk":
		return fmt.Sprintf("  - ⚠️ **Uso real de recursos**: P95 histórico próximo ou acima do request configurado (%s) — risco de OOM/throttling, requests provavelmente subdimensionados.\n", usage)
	case "superprovisioned":
		return fmt.Sprintf("  - **Uso real de recursos**: P95 histórico bem abaixo do request configurado (%s) — requests provavelmente superdimensionados.\n", usage)
	default:
		return ""
	}
}

// writeK8sIssue escreve um issue K8s no formato exigido do prompt: citação verbatim da mensagem
// bruta do evento/recurso, e as linhas de crônico/agudo e uso real de recursos quando disponíveis.
func writeK8sIssue(sb *strings.Builder, issue healthcheck.CorrelatedK8sIssue) {
	sb.WriteString(fmt.Sprintf("- **%s** `%s` [%s]\n", issue.ResourceKind, issue.ResourceName, issue.Severity))
	sb.WriteString(fmt.Sprintf("  - Mensagem bruta (cite literalmente no relatório): `%s`\n", issue.Message))
	if issue.Count > 0 {
		sb.WriteString(fmt.Sprintf("  - Ocorrências nesta execução: %d (desde %s)\n", issue.Count, issue.FirstTimestamp.Format("02/01/2006 15:04")))
	}
	if line := chronicityLine(issue); line != "" {
		sb.WriteString(line)
	}
	if line := resourceVerdictLine(issue); line != "" {
		sb.WriteString(line)
	}
}

// writeDTProblem escreve um problem Dynatrace no formato exigido do prompt: ID + dias em aberto,
// evidências Davis AI e métricas (incluindo CPU/memória de node/pod quando disponíveis).
func writeDTProblem(sb *strings.Builder, p healthcheck.DynatraceHealth) {
	sb.WriteString(fmt.Sprintf("- **%s** [%s/%s] — %s\n", p.DisplayID, p.DTSeverity, p.ImpactLevel, daysOpen(p.StartTime)))
	sb.WriteString(fmt.Sprintf("  - %s\n", p.Title))
	if len(p.Evidence) > 0 {
		sb.WriteString("  - Davis AI Evidence:\n")
		for _, ev := range p.Evidence {
			sb.WriteString(fmt.Sprintf("    - %s\n", ev))
		}
	}
	if len(p.MetricsSummary) > 0 {
		sb.WriteString("  - Métricas:\n")
		for k, v := range p.MetricsSummary {
			sb.WriteString(fmt.Sprintf("    - %s: %.2f\n", k, v))
		}
	}
	if len(p.RecentEvents) > 0 {
		sb.WriteString("  - Eventos recentes:\n")
		for _, ev := range p.RecentEvents {
			sb.WriteString(fmt.Sprintf("    - %s\n", ev))
		}
	}
}

// buildNodeUtilizationSection monta a tabela de utilização por nó (pods ativos / capacidade máxima
// / % de utilização) — contexto de capacidade do CLUSTER, não específico de um workload, no mesmo
// formato do relatório de incidente que motivou essa estrutura de prompt. Vazio quando não há dados
// de nó (CheckNodes=false na request do Health Check, ou nenhum node retornado).
func buildNodeUtilizationSection(nodes []healthcheck.NodeHealth) string {
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Utilização dos Nós\n\n")
	sb.WriteString("| Nó | Pods Ativos | Capacidade Máx | Utilização |\n")
	sb.WriteString("|---|---|---|---|\n")
	for _, n := range nodes {
		sb.WriteString(fmt.Sprintf("| %s | %d | %d | %.1f%% |\n", n.Name, n.Allocated.Pods, n.Allocatable.Pods, n.PodUtilization))
	}
	sb.WriteString("\nSe a utilização de pods for baixa mas os nós tiverem alta utilização de CPU/memória, " +
		"suspeite de requests/limits mal configurados nos workloads (consumindo mais do que declarado) — " +
		"não de excesso de pods por nó.\n\n")
	return sb.String()
}

// promptOutputInstructions é a instrução final compartilhada pelos dois prompt builders,
// exigindo a estrutura de relatório de incidente madura: emoji de severidade, citação verbatim,
// link causal explícito pro sintoma do usuário, e ações divididas em 3 baldes de urgência.
const promptOutputInstructions = `Estruture a resposta como um relatório de incidente, seguindo EXATAMENTE este formato:

1. Para cada problema relevante, use um cabeçalho "EMOJI Problema — título curto", onde EMOJI é
   🔴 (crítico), 🟠 (alto), 🟡 (médio) ou 🟢 (baixo/informativo) conforme a severidade.
2. Logo abaixo do cabeçalho, cite a mensagem bruta do evento/problem entre crases (verbatim, sem
   parafrasear) e explique em linguagem simples o que ela significa tecnicamente.
3. Diga explicitamente qual sintoma o usuário final provavelmente percebe por causa desse problema
   (ex: "isso causa lentidão/erro/reset percebido pelo usuário") — não deixe essa conexão implícita.
4. Quando o item tiver marcação de crônico, deixe claro que não é um problema pontual: cite a
   contagem acumulada e a data de primeira ocorrência.
5. Ao final, uma seção "## Ações Recomendadas" dividida em exatamente três subtítulos, cada um com
   ações concretas (comandos kubectl quando aplicável):
   - "### Imediato (hoje)"
   - "### Curto prazo (esta semana)"
   - "### Monitoramento contínuo"

Não invente números ou IDs de problem que não estejam nos dados fornecidos acima.`

func buildCorrelatedItemPrompt(item healthcheck.CorrelatedHealthItem, nodes []healthcheck.NodeHealth) string {
	var sb strings.Builder

	sb.WriteString("Você é um especialista em Kubernetes e observabilidade, escrevendo um relatório de incidente para um time de SRE. Analise o item de saúde correlacionado entre K8s e Dynatrace abaixo.\n\n")
	sb.WriteString(fmt.Sprintf("**Workload:** %s\n", item.WorkloadName))
	sb.WriteString(fmt.Sprintf("**Namespace:** %s\n", item.Namespace))
	sb.WriteString(fmt.Sprintf("**Cluster:** %s\n", item.Cluster))
	sb.WriteString(fmt.Sprintf("**Severidade Final:** %s %s\n", severityEmoji(item.FinalSeverity), item.FinalSeverity))
	if item.Correlated {
		sb.WriteString("**Correlação:** Confirmada — K8s e Dynatrace detectaram problemas no mesmo workload\n")
	}
	sb.WriteString("\n")

	if len(item.K8sIssues) > 0 {
		sb.WriteString("## Sintomas Kubernetes\n")
		for _, issue := range item.K8sIssues {
			writeK8sIssue(&sb, issue)
		}
		sb.WriteString("\n")
	}

	if len(item.DTProblems) > 0 {
		sb.WriteString("## Problems Dynatrace\n")
		for _, p := range item.DTProblems {
			writeDTProblem(&sb, p)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(buildNodeUtilizationSection(nodes))
	sb.WriteString(promptOutputInstructions)

	return sb.String()
}

// AnalyzeCorrelatedBatch realiza análise AI consolidada de múltiplos itens correlacionados K8s ↔ Dynatrace.
// Útil para obter um diagnóstico geral do ambiente em vez de item por item.
// POST /api/v1/healthcheck/correlated/analyze-batch
func (h *HealthCheckHandler) AnalyzeCorrelatedBatch(c *gin.Context) {
	if h.aiHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI não configurado neste servidor"})
		return
	}

	var req struct {
		AIEmail string                             `json:"ai_email"`
		Items   []healthcheck.CorrelatedHealthItem `json:"items"`
		Nodes   []healthcheck.NodeHealth           `json:"nodes,omitempty"` // contexto de capacidade do cluster (opcional — Fase 2)
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AIEmail == "" || len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ai_email e items são obrigatórios"})
		return
	}
	// Limita a 20 itens para não explodir o contexto do modelo
	if len(req.Items) > 20 {
		req.Items = req.Items[:20]
	}

	provider, err := h.aiHandler.GetProviderForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	prompt := buildBatchCorrelatedPrompt(req.Items, req.Nodes)

	analysis, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha na análise AI: " + err.Error()})
		return
	}

	log.Info().
		Str("ai_email", req.AIEmail).
		Int("item_count", len(req.Items)).
		Msg("[HealthCheck] Análise AI batch de itens correlacionados concluída")

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"analysis":    analysis,
		"item_count":  len(req.Items),
		"analyzed_at": time.Now(),
	})
}

// buildBatchCorrelatedPrompt monta prompt consolidado com todos os itens correlacionados para análise em batch
func buildBatchCorrelatedPrompt(items []healthcheck.CorrelatedHealthItem, nodes []healthcheck.NodeHealth) string {
	var sb strings.Builder

	// Contadores para o sumário
	correlated, critical, high := 0, 0, 0
	clusters := make(map[string]struct{})
	for _, it := range items {
		if it.Correlated {
			correlated++
		}
		if it.FinalSeverity == healthcheck.SeverityCritical {
			critical++
		} else if it.FinalSeverity == healthcheck.SeverityHigh {
			high++
		}
		clusters[it.Cluster] = struct{}{}
	}

	// Cabeçalho no mesmo espírito do exemplo de relatório de incidente que motivou esta estrutura
	// (cluster/período/namespace/nós) — aqui adaptado ao que o batch realmente tem disponível.
	clusterList := make([]string, 0, len(clusters))
	for c := range clusters {
		clusterList = append(clusterList, c)
	}
	sb.WriteString("Você é um especialista em Kubernetes e observabilidade, escrevendo um relatório de incidente consolidado para um time de SRE. Analise o estado geral de saúde do ambiente abaixo.\n\n")
	sb.WriteString(fmt.Sprintf("**Cluster(s):** %s\n", strings.Join(clusterList, ", ")))
	sb.WriteString(fmt.Sprintf("**Resumo:** %d workloads com problemas | %d correlacionados K8s+DT | %d críticos | %d alto\n\n",
		len(items), correlated, critical, high))

	// Itens agrupados por severidade (críticos primeiro, já vêm ordenados do correlator)
	sb.WriteString("## Workloads Afetados\n\n")
	for i, it := range items {
		sb.WriteString(fmt.Sprintf("### %d. %s %s/%s [%s] — Severidade: %s\n",
			i+1, severityEmoji(it.FinalSeverity), it.Namespace, it.WorkloadName, it.Cluster, it.FinalSeverity))
		if it.Correlated {
			sb.WriteString("*(Correlacionado: K8s e Dynatrace confirmam problema no mesmo workload)*\n")
		}

		if len(it.K8sIssues) > 0 {
			sb.WriteString("**K8s:**\n")
			for _, issue := range it.K8sIssues {
				writeK8sIssue(&sb, issue)
			}
		}

		if len(it.DTProblems) > 0 {
			sb.WriteString("**Dynatrace:**\n")
			for _, p := range it.DTProblems {
				writeDTProblem(&sb, p)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(buildNodeUtilizationSection(nodes))

	sb.WriteString("## Forneça\n")
	sb.WriteString("1. **Panorama geral** — qual é o estado de saúde do ambiente?\n")
	sb.WriteString("2. **Padrões identificados** — há causa raiz comum entre múltiplos workloads (ex: um nginx ingress ou um node pool afetando várias apps)?\n")
	sb.WriteString("3. **Tabela-resumo** — uma tabela markdown com colunas Severidade | Problema | Aplicação Afetada | Status, cobrindo os itens mais relevantes.\n\n")
	sb.WriteString(promptOutputInstructions)

	return sb.String()
}

// AnalyzeOneAgentSignal realiza análise AI de um sinal OneAgent individual.
// POST /api/v1/healthcheck/oneagent/analyze
func (h *HealthCheckHandler) AnalyzeOneAgentSignal(c *gin.Context) {
	if h.aiHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI não configurado neste servidor"})
		return
	}

	var req struct {
		AIEmail string                     `json:"ai_email"`
		Signal  healthcheck.OneAgentSignal `json:"signal"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	provider, err := h.aiHandler.GetProviderForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	prompt := buildOneAgentSignalPrompt(req.Signal)
	analysis, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro na análise AI: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis})
}

// buildOneAgentSignalPrompt monta prompt para análise de um sinal OneAgent individual
func buildOneAgentSignalPrompt(s healthcheck.OneAgentSignal) string {
	var sb strings.Builder

	sb.WriteString("Você é um especialista em Kubernetes e observabilidade Dynatrace. Analise o sinal de risco abaixo e forneça diagnóstico e recomendações.\n\n")
	sb.WriteString(fmt.Sprintf("**Workload:** %s/%s (cluster: %s)\n", s.Namespace, s.WorkloadName, s.Cluster))
	sb.WriteString(fmt.Sprintf("**Entidade DT:** %s (%s)\n", s.EntityID, s.EntityType))
	sb.WriteString(fmt.Sprintf("**Nível de risco:** %s\n", s.RiskLevel))

	if len(s.RiskReasons) > 0 {
		sb.WriteString("**Razões:**\n")
		for _, r := range s.RiskReasons {
			sb.WriteString(fmt.Sprintf("  - %s\n", r))
		}
	}

	sb.WriteString("\n**Métricas (último 60min — valor máximo):**\n")
	if s.ErrorRate > 0 {
		sb.WriteString(fmt.Sprintf("  - Taxa de erro: %.1f%%\n", s.ErrorRate))
	}
	if s.ResponseP90Ms > 0 {
		sb.WriteString(fmt.Sprintf("  - Latência P90: %.0fms\n", s.ResponseP90Ms))
	}
	if s.PodRestarts > 0 {
		sb.WriteString(fmt.Sprintf("  - Pod restarts: %.0f\n", s.PodRestarts))
	}
	if s.CPUThrottlePct > 0 {
		sb.WriteString(fmt.Sprintf("  - CPU throttle: %.1f%%\n", s.CPUThrottlePct))
	}
	if s.PodsReadyPct > 0 {
		sb.WriteString(fmt.Sprintf("  - Pods prontos: %.1f%%\n", s.PodsReadyPct))
	}

	if s.HasDTProblem {
		sb.WriteString("\n⚠️ Este workload também possui um Problem ativo no Dynatrace.\n")
	}

	if len(s.DependedBy) > 0 {
		sb.WriteString(fmt.Sprintf("\n**Dependido por:** %s\n", strings.Join(s.DependedBy, ", ")))
	}
	if len(s.DependsOn) > 0 {
		sb.WriteString(fmt.Sprintf("**Depende de:** %s\n", strings.Join(s.DependsOn, ", ")))
	}

	if len(s.ClusterPools) > 0 {
		sb.WriteString("\n**Node Pools do cluster:**\n")
		for _, p := range s.ClusterPools {
			sb.WriteString(fmt.Sprintf("  - %s (%s, mode=%s, nodes=%d)\n", p.NodePool, p.VMSize, p.Mode, p.NodeCount))
		}
	}

	sb.WriteString("\n## Forneça\n")
	sb.WriteString("1. **Diagnóstico** — qual é a causa provável do risco identificado?\n")
	sb.WriteString("2. **Impacto** — qual é o risco de este workload afetar usuários ou outros serviços?\n")
	sb.WriteString("3. **Ações recomendadas** — passos concretos para mitigar o risco (kubectl ou configuração)\n")
	sb.WriteString("4. **Monitoramento** — o que observar para confirmar melhora ou deterioração?\n")

	return sb.String()
}
