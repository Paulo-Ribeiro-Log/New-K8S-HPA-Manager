package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// AIDiagnosticsHandler handler para diagnósticos AI
type AIDiagnosticsHandler struct {
	analyzer     *ai.Analyzer
	historyStore *storage.AIHistoryStore
}

// NewAIDiagnosticsHandler cria um novo AIDiagnosticsHandler
func NewAIDiagnosticsHandler(
	analyzer *ai.Analyzer,
	historyStore *storage.AIHistoryStore,
) *AIDiagnosticsHandler {
	return &AIDiagnosticsHandler{
		analyzer:     analyzer,
		historyStore: historyStore,
	}
}

// analyzeRequest estrutura de requisição para análise
type analyzeRequest struct {
	ResourceType    string `json:"resource_type" binding:"required"`
	Cluster         string `json:"cluster" binding:"required"`
	Namespace       string `json:"namespace" binding:"required"`
	ResourceName    string `json:"resource_name" binding:"required"`
	IncludeLogs     bool   `json:"include_logs"`
	IncludeMetrics  bool   `json:"include_metrics"`
	IncludeDescribe bool   `json:"include_describe"`
}

// Analyze executa análise AI de um recurso
// POST /api/v1/ai/analyze
func (h *AIDiagnosticsHandler) Analyze(c *gin.Context) {
	var req analyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	log.Info().
		Str("resource_type", req.ResourceType).
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("resource_name", req.ResourceName).
		Msg("Starting AI analysis")

	// Obter user email do contexto RBAC (se disponível)
	userEmail := ""
	if email, exists := c.Get("user_email"); exists {
		if emailStr, ok := email.(string); ok {
			userEmail = emailStr
		}
	}

	// Criar request de análise
	analysisReq := &ai.AnalysisRequest{
		ResourceType:    req.ResourceType,
		Cluster:         req.Cluster,
		Namespace:       req.Namespace,
		ResourceName:    req.ResourceName,
		IncludeLogs:     req.IncludeLogs,
		IncludeMetrics:  req.IncludeMetrics,
		IncludeDescribe: req.IncludeDescribe,
		UserEmail:       userEmail,
	}

	// Executar análise (com timeout do Gin context)
	result, err := h.analyzer.Analyze(c.Request.Context(), analysisReq)
	if err != nil {
		log.Error().Err(err).Msg("AI analysis failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "analysis failed: " + err.Error()})
		return
	}

	log.Info().
		Str("analysis_id", result.ID).
		Float64("response_time", result.ResponseTime).
		Msg("AI analysis completed")

	c.JSON(http.StatusOK, result)
}

// GetHistory lista histórico de análises
// GET /api/v1/ai/history
func (h *AIDiagnosticsHandler) GetHistory(c *gin.Context) {
	// Parse filtros da query string
	filters := &storage.QueryFilters{
		Cluster:      c.Query("cluster"),
		Namespace:    c.Query("namespace"),
		ResourceType: c.Query("resource_type"),
		ResourceName: c.Query("resource_name"),
		Provider:     c.Query("provider"),
		UserEmail:    c.Query("user_email"),
		Limit:        50, // Padrão: 50 registros
	}

	// Parse limit e offset
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filters.Limit = limit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filters.Offset = offset
		}
	}

	// Parse datas
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if startDate, err := time.Parse(time.RFC3339, startDateStr); err == nil {
			filters.StartDate = &startDate
		}
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if endDate, err := time.Parse(time.RFC3339, endDateStr); err == nil {
			filters.EndDate = &endDate
		}
	}

	// Buscar histórico
	records, err := h.historyStore.Query(filters)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query history"})
		return
	}

	// Contar total (para paginação)
	total, _ := h.historyStore.Count(filters)

	c.JSON(http.StatusOK, gin.H{
		"records": records,
		"total":   total,
		"limit":   filters.Limit,
		"offset":  filters.Offset,
	})
}

// GetAnalysisByID obtém uma análise específica por ID
// GET /api/v1/ai/history/:id
func (h *AIDiagnosticsHandler) GetAnalysisByID(c *gin.Context) {
	id := c.Param("id")

	record, err := h.historyStore.GetByID(id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to get analysis")
		c.JSON(http.StatusNotFound, gin.H{"error": "analysis not found"})
		return
	}

	// Parse suggestions de JSON string para objeto
	var suggestions []ai.Suggestion
	if record.Suggestions != "" {
		if err := json.Unmarshal([]byte(record.Suggestions), &suggestions); err != nil {
			log.Warn().Err(err).Msg("Failed to parse suggestions")
		}
	}

	// Construir resposta
	result := &ai.AnalysisResult{
		ID:           record.ID,
		ResourceType: record.ResourceType,
		Cluster:      record.Cluster,
		Namespace:    record.Namespace,
		ResourceName: record.ResourceName,
		Provider:     record.Provider,
		Model:        record.Model,
		Analysis:     record.Analysis,
		Suggestions:  suggestions,
		TokensUsed:   record.TokensUsed,
		ResponseTime: record.ResponseTime,
		AnalyzedAt:   record.AnalyzedAt,
	}

	c.JSON(http.StatusOK, result)
}

// GetProviderStatus obtém status do provider AI
// GET /api/v1/ai/status
func (h *AIDiagnosticsHandler) GetProviderStatus(c *gin.Context) {
	status := h.analyzer.GetProviderStatus(c.Request.Context())
	c.JSON(http.StatusOK, status)
}

// GetStats obtém estatísticas do histórico
// GET /api/v1/ai/stats
func (h *AIDiagnosticsHandler) GetStats(c *gin.Context) {
	stats, err := h.historyStore.GetStats()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get stats")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get stats"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// DeleteAnalysis deleta uma análise do histórico
// DELETE /api/v1/ai/history/:id
func (h *AIDiagnosticsHandler) DeleteAnalysis(c *gin.Context) {
	id := c.Param("id")

	if err := h.historyStore.Delete(id); err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to delete analysis")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete analysis"})
		return
	}

	log.Info().Str("id", id).Msg("Analysis deleted")
	c.JSON(http.StatusOK, gin.H{"message": "analysis deleted"})
}

// RegisterRoutes registra rotas do handler
func (h *AIDiagnosticsHandler) RegisterRoutes(r *gin.RouterGroup, kubeManager *kubernetes.KubeManager) {
	// Rotas públicas (GET)
	r.GET("/ai/status", h.GetProviderStatus)
	r.GET("/ai/history", h.GetHistory)
	r.GET("/ai/history/:id", h.GetAnalysisByID)
	r.GET("/ai/stats", h.GetStats)

	// Rotas protegidas (POST, DELETE) - apenas leitura por enquanto
	r.POST("/ai/analyze", h.Analyze)
	r.DELETE("/ai/history/:id", h.DeleteAnalysis)
}
