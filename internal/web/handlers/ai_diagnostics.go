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
	analyzer      *ai.Analyzer // Analyzer padrão (fallback)
	historyStore  *storage.AIHistoryStore
	tokensStore   *storage.UserTokensStore
	kubeManager   *kubernetes.KubeManager
	defaultConfig *ai.Config // Config padrão (flags do servidor)
}

// NewAIDiagnosticsHandler cria um novo AIDiagnosticsHandler
func NewAIDiagnosticsHandler(
	analyzer *ai.Analyzer,
	historyStore *storage.AIHistoryStore,
	tokensStore *storage.UserTokensStore,
	kubeManager *kubernetes.KubeManager,
	defaultConfig *ai.Config,
) *AIDiagnosticsHandler {
	return &AIDiagnosticsHandler{
		analyzer:      analyzer,
		historyStore:  historyStore,
		tokensStore:   tokensStore,
		kubeManager:   kubeManager,
		defaultConfig: defaultConfig,
	}
}

// Analyze executa análise AI de um recurso
// POST /api/v1/ai/analyze
func (h *AIDiagnosticsHandler) Analyze(c *gin.Context) {
	var req ai.AnalysisRequest
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
	var userEmail string
	if email, exists := c.Get("user_email"); exists {
		if emailStr, ok := email.(string); ok {
			userEmail = emailStr
			req.UserEmail = emailStr
		}
	}

	// Buscar analyzer apropriado (preferências do usuário ou padrão)
	analyzer := h.getAnalyzerForUser(userEmail)

	// Executar análise (com timeout do Gin context)
	result, err := analyzer.Analyze(c.Request.Context(), &req)
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

// getAnalyzerForUser retorna analyzer personalizado baseado nas preferências do usuário
func (h *AIDiagnosticsHandler) getAnalyzerForUser(userEmail string) *ai.Analyzer {
	// Se não tiver user email ou tokensStore, usar analyzer padrão
	if userEmail == "" || h.tokensStore == nil {
		return h.analyzer
	}

	// Buscar tokens/preferências do usuário
	tokens, err := h.tokensStore.GetTokens(userEmail)
	if err != nil || tokens == nil {
		// Se erro ou usuário não tem preferências, usar padrão
		log.Debug().
			Str("user_email", userEmail).
			Msg("Using default analyzer (no user preferences found)")
		return h.analyzer
	}

	// Criar config personalizado baseado nas preferências do usuário
	config := &ai.Config{
		Provider: tokens.PreferredProvider,
		Timeout:  h.defaultConfig.Timeout,
	}

	// Configurar provider específico com modelo selecionado
	switch tokens.PreferredProvider {
	case "gemini":
		if tokens.GeminiAPIKey != "" {
			config.GeminiAPIKey = tokens.GeminiAPIKey
			// Usar modelo selecionado ou padrão
			if tokens.GeminiModel != "" {
				config.GeminiModel = tokens.GeminiModel
			} else {
				config.GeminiModel = "gemini-2.0-flash-exp"
			}
		} else {
			// Sem API key, usar padrão
			return h.analyzer
		}
	case "claude":
		if tokens.ClaudeAPIKey != "" {
			config.ClaudeAPIKey = tokens.ClaudeAPIKey
			if tokens.ClaudeModel != "" {
				config.ClaudeModel = tokens.ClaudeModel
			} else {
				config.ClaudeModel = "claude-3-5-sonnet-20241022"
			}
		} else {
			return h.analyzer
		}
	case "openai":
		if tokens.OpenAIAPIKey != "" {
			config.OpenAIAPIKey = tokens.OpenAIAPIKey
			if tokens.OpenAIModel != "" {
				config.OpenAIModel = tokens.OpenAIModel
			} else {
				config.OpenAIModel = "gpt-4o-mini"
			}
		} else {
			return h.analyzer
		}
	case "copilot":
		if tokens.CopilotAPIKey != "" && tokens.CopilotEndpoint != "" {
			config.CopilotAPIKey = tokens.CopilotAPIKey
			config.CopilotEndpoint = tokens.CopilotEndpoint
			if tokens.CopilotDeployment != "" {
				config.CopilotDeployment = tokens.CopilotDeployment
			} else {
				config.CopilotDeployment = "gpt-4o"
			}
			config.CopilotAPIVersion = "2024-02-15-preview"
		} else {
			return h.analyzer
		}
	case "ollama":
		// Ollama não precisa de API key
		config.OllamaBaseURL = h.defaultConfig.OllamaBaseURL
		if tokens.OllamaModel != "" {
			config.OllamaModel = tokens.OllamaModel
		} else {
			config.OllamaModel = "llama3.2:3b"
		}
	default:
		// Provider desconhecido, usar padrão
		return h.analyzer
	}

	// Criar provider personalizado
	provider, err := ai.NewProvider(config)
	if err != nil {
		log.Warn().
			Err(err).
			Str("user_email", userEmail).
			Str("provider", tokens.PreferredProvider).
			Msg("Failed to create user-specific provider, using default")
		return h.analyzer
	}

	// Criar analyzer temporário para esta requisição
	userAnalyzer := ai.NewAnalyzer(provider, h.kubeManager, h.historyStore)

	log.Info().
		Str("user_email", userEmail).
		Str("provider", tokens.PreferredProvider).
		Str("model", h.getModelFromConfig(config)).
		Msg("Using user-specific AI analyzer")

	return userAnalyzer
}

// getModelFromConfig extrai o nome do modelo da config
func (h *AIDiagnosticsHandler) getModelFromConfig(config *ai.Config) string {
	switch config.Provider {
	case "gemini":
		return config.GeminiModel
	case "claude":
		return config.ClaudeModel
	case "openai":
		return config.OpenAIModel
	case "ollama":
		return config.OllamaModel
	case "copilot":
		return config.CopilotDeployment
	default:
		return "unknown"
	}
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
	// Obter user email do contexto RBAC (se disponível)
	var userEmail string
	if email, exists := c.Get("user_email"); exists {
		if emailStr, ok := email.(string); ok {
			userEmail = emailStr
		}
	}

	// Buscar analyzer apropriado (preferências do usuário ou padrão do servidor)
	analyzer := h.getAnalyzerForUser(userEmail)

	// Obter status do provider (com chamada de teste para detectar erros)
	status := analyzer.GetProviderStatus(c.Request.Context())
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

// Getters para compartilhar recursos com outros handlers (como PredictionsHandler)
func (h *AIDiagnosticsHandler) GetAnalyzer() *ai.Analyzer {
	return h.analyzer
}

func (h *AIDiagnosticsHandler) GetTokensStore() *storage.UserTokensStore {
	return h.tokensStore
}

func (h *AIDiagnosticsHandler) GetDefaultConfig() *ai.Config {
	return h.defaultConfig
}
