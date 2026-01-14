package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/ai/reports"
	"k8s-hpa-manager/internal/aierrors"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// RecordGlobalAIError é um wrapper para aierrors.RecordGlobalAIError
func RecordGlobalAIError(userEmail, provider, model string, err error) {
	if err == nil {
		aierrors.RecordGlobalAIError(userEmail, provider, model, nil)
		return
	}
	
	errMsg := err.Error()
	// Truncar se muito longo
	if len(errMsg) > 300 {
		errMsg = errMsg[:300] + "..."
	}
	
	aierrors.RecordGlobalAIError(userEmail, provider, model, err)
}

// GetGlobalAIError é um wrapper para aierrors.GetGlobalAIError
func GetGlobalAIError(userEmail string) *aierrors.ErrorRecord {
	if userEmail == "" {
		userEmail = "default"
	}
	return aierrors.GetGlobalAIError(userEmail)
}

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

	// 🔒 SECURITY: Log qual provider será usado ANTES de executar
	provider := analyzer.GetProvider()
	if provider != nil {
		providerName := provider.GetName()
		modelName := provider.GetModel()

		log.Warn().
			Str("user_email", userEmail).
			Str("provider", providerName).
			Str("model", modelName).
			Str("resource", fmt.Sprintf("%s/%s/%s", req.Cluster, req.Namespace, req.ResourceName)).
			Msg("🔒 SECURITY: AI analysis will use the following provider")

		// 🚨 WARNING CRÍTICO para providers pagos
		if providerName == "claude" || providerName == "openai" {
			log.Warn().
				Str("provider", providerName).
				Str("model", modelName).
				Msg("⚠️ WARNING: Using PAID AI provider - charges may apply!")
		}
	}

	// Executar análise (com timeout do Gin context)
	result, err := analyzer.Analyze(c.Request.Context(), &req)
	if err != nil {
		log.Error().Err(err).Msg("AI analysis failed")
		
		// Registrar erro globalmente para exibir no painel
		provider := analyzer.GetProvider()
		if provider != nil {
			RecordGlobalAIError(userEmail, provider.GetName(), provider.GetModel(), err)
		}
		
		c.JSON(http.StatusInternalServerError, gin.H{"error": "analysis failed: " + err.Error()})
		return
	}

	// Limpar erro global pois análise foi bem-sucedida
	provider = analyzer.GetProvider()
	if provider != nil {
		RecordGlobalAIError(userEmail, provider.GetName(), provider.GetModel(), nil)
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

	// 🔒 SECURITY: Garantir que APENAS o provider configurado seja usado
	// NUNCA usar API keys de outros providers, mesmo se estiverem salvas no banco

	switch tokens.PreferredProvider {
	case "gemini":
		if tokens.GeminiAPIKey != "" {
			config.GeminiAPIKey = tokens.GeminiAPIKey
			if tokens.GeminiModel != "" {
				config.GeminiModel = tokens.GeminiModel
			} else {
				config.GeminiModel = "gemini-2.5-flash"
			}

			// 🚨 SECURITY: Log uso de Gemini (gratuito com quota)
			log.Warn().
				Str("user_email", userEmail).
				Str("provider", "gemini").
				Str("model", config.GeminiModel).
				Msg("⚠️ Using Gemini API - quota-limited free tier")
		} else {
			// Sem API key, usar analyzer padrão
			log.Debug().
				Str("user_email", userEmail).
				Msg("Gemini selected but no API key - using default analyzer")
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

			// 🚨 CRITICAL: Log uso de Claude (PAGO!)
			log.Warn().
				Str("user_email", userEmail).
				Str("provider", "claude").
				Str("model", config.ClaudeModel).
				Msg("🚨 CRITICAL: Using Claude API - PAID SERVICE - charges will apply!")
		} else {
			log.Debug().
				Str("user_email", userEmail).
				Msg("Claude selected but no API key - using default analyzer")
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

			// 🚨 CRITICAL: Log uso de OpenAI (PAGO!)
			log.Warn().
				Str("user_email", userEmail).
				Str("provider", "openai").
				Str("model", config.OpenAIModel).
				Msg("🚨 CRITICAL: Using OpenAI API - PAID SERVICE - charges will apply!")
		} else {
			log.Debug().
				Str("user_email", userEmail).
				Msg("OpenAI selected but no API key - using default analyzer")
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

			// 🚨 CRITICAL: Log uso de Copilot (PAGO - Azure OpenAI!)
			log.Warn().
				Str("user_email", userEmail).
				Str("provider", "copilot").
				Str("deployment", config.CopilotDeployment).
				Msg("🚨 CRITICAL: Using Azure OpenAI (Copilot) - PAID SERVICE - charges will apply!")
		} else {
			log.Debug().
				Str("user_email", userEmail).
				Msg("Copilot selected but missing API key or endpoint - using default analyzer")
			return h.analyzer
		}

	case "ollama":
		// Ollama é local e gratuito - sem preocupação com custos
		config.OllamaBaseURL = h.defaultConfig.OllamaBaseURL
		if tokens.OllamaModel != "" {
			config.OllamaModel = tokens.OllamaModel
		} else {
			config.OllamaModel = "llama3.2:3b"
		}

		log.Info().
			Str("user_email", userEmail).
			Str("provider", "ollama").
			Str("model", config.OllamaModel).
			Msg("✅ Using Ollama (local) - FREE, no API costs")

	default:
		// Provider desconhecido, usar padrão
		log.Warn().
			Str("user_email", userEmail).
			Str("provider", tokens.PreferredProvider).
			Msg("Unknown provider - falling back to default analyzer")
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

	// Obter status do provider
	status := analyzer.GetProviderStatus(c.Request.Context())
	
	// Verificar se existe erro global recente para este usuário
	if errorRecord := GetGlobalAIError(userEmail); errorRecord != nil {
		// Se não tiver erro no analyzer, usar erro global
		if status.Error == "" {
			duration := time.Since(errorRecord.Timestamp)
			status.Error = formatErrorWithTime(errorRecord.Error, duration)
			status.Available = false
		}
	}
	
	// Adicionar estatísticas de chamadas
	stats := aierrors.GetAICallStats(userEmail)
	status.TotalCalls = stats.TotalCalls
	status.SuccessfulCalls = stats.SuccessfulCalls
	status.FailedCalls = stats.FailedCalls
	
	c.JSON(http.StatusOK, status)
}

// formatErrorWithTime formata erro com timestamp
func formatErrorWithTime(errMsg string, duration time.Duration) string {
	if duration < time.Minute {
		return errMsg + " (agora)"
	}
	if duration < time.Hour {
		minutes := int(duration.Minutes())
		return errMsg + " (há " + strconv.Itoa(minutes) + "min)"
	}
	hours := int(duration.Hours())
	return errMsg + " (há " + strconv.Itoa(hours) + "h)"
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

// GetReportPDF exporta análise como PDF
// GET /api/v1/ai/report/:id/pdf
func (h *AIDiagnosticsHandler) GetReportPDF(c *gin.Context) {
	id := c.Param("id")

	// Buscar análise no banco
	record, err := h.historyStore.GetByID(id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to get analysis for PDF export")
		c.JSON(http.StatusNotFound, gin.H{"error": "analysis not found"})
		return
	}

	// Converter HistoryRecord para AnalysisResult
	result, err := historyRecordToAnalysisResult(record)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to convert record to AnalysisResult")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to convert analysis"})
		return
	}

	// Gerar PDF
	pdfBytes, err := reports.GeneratePDF(result)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to generate PDF")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PDF"})
		return
	}

	// Filename: diagnostico_{resourceName}_{date}.pdf
	filename := fmt.Sprintf("diagnostico_%s_%s.pdf",
		result.ResourceName,
		result.AnalyzedAt.Format("2006-01-02_15-04-05"),
	)

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Data(http.StatusOK, "application/pdf", pdfBytes)

	log.Info().Str("id", id).Str("filename", filename).Msg("PDF report generated")
}

// GetReportMarkdown exporta análise como Markdown
// GET /api/v1/ai/report/:id/markdown
func (h *AIDiagnosticsHandler) GetReportMarkdown(c *gin.Context) {
	id := c.Param("id")

	// Buscar análise no banco
	record, err := h.historyStore.GetByID(id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to get analysis for Markdown export")
		c.JSON(http.StatusNotFound, gin.H{"error": "analysis not found"})
		return
	}

	// Converter HistoryRecord para AnalysisResult
	result, err := historyRecordToAnalysisResult(record)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to convert record to AnalysisResult")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to convert analysis"})
		return
	}

	// Gerar Markdown
	markdownBytes, err := reports.GenerateMarkdown(result)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to generate Markdown")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate Markdown"})
		return
	}

	// Filename: diagnostico_{resourceName}_{date}.md
	filename := fmt.Sprintf("diagnostico_%s_%s.md",
		result.ResourceName,
		result.AnalyzedAt.Format("2006-01-02_15-04-05"),
	)

	c.Header("Content-Type", "text/markdown")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Data(http.StatusOK, "text/markdown", markdownBytes)

	log.Info().Str("id", id).Str("filename", filename).Msg("Markdown report generated")
}

// GetReportCSV exporta análise como CSV
// GET /api/v1/ai/report/:id/csv
func (h *AIDiagnosticsHandler) GetReportCSV(c *gin.Context) {
	id := c.Param("id")

	// Buscar análise no banco
	record, err := h.historyStore.GetByID(id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to get analysis for CSV export")
		c.JSON(http.StatusNotFound, gin.H{"error": "analysis not found"})
		return
	}

	// Converter HistoryRecord para AnalysisResult
	result, err := historyRecordToAnalysisResult(record)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to convert record to AnalysisResult")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to convert analysis"})
		return
	}

	// Gerar CSV
	csvBytes, err := reports.GenerateCSV(result)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to generate CSV")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate CSV"})
		return
	}

	// Filename: diagnostico_{resourceName}_{date}.csv
	filename := fmt.Sprintf("diagnostico_%s_%s.csv",
		result.ResourceName,
		result.AnalyzedAt.Format("2006-01-02_15-04-05"),
	)

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Data(http.StatusOK, "text/csv", csvBytes)

	log.Info().Str("id", id).Str("filename", filename).Msg("CSV report generated")
}

// historyRecordToAnalysisResult converte HistoryRecord para AnalysisResult
func historyRecordToAnalysisResult(record *storage.HistoryRecord) (*ai.AnalysisResult, error) {
	result := &ai.AnalysisResult{
		ID:           record.ID,
		ResourceType: record.ResourceType,
		Cluster:      record.Cluster,
		Namespace:    record.Namespace,
		ResourceName: record.ResourceName,
		Provider:     record.Provider,
		Model:        record.Model,
		Analysis:     record.Analysis,
		TokensUsed:   record.TokensUsed,
		ResponseTime: record.ResponseTime,
		AnalyzedAt:   record.AnalyzedAt,
		Error:        "",
	}

	// Parse Suggestions (JSON array)
	if record.Suggestions != "" {
		var suggestions []ai.Suggestion
		if err := json.Unmarshal([]byte(record.Suggestions), &suggestions); err != nil {
			log.Warn().Err(err).Msg("Failed to parse suggestions, using empty array")
		} else {
			result.Suggestions = suggestions
		}
	}

	// Parse Prometheus Metrics (JSON object)
	if record.PrometheusMetrics.Valid && record.PrometheusMetrics.String != "" {
		var metrics ai.ResourceMetrics
		if err := json.Unmarshal([]byte(record.PrometheusMetrics.String), &metrics); err != nil {
			log.Warn().Err(err).Msg("Failed to parse Prometheus metrics")
		} else {
			result.CurrentMetrics = &metrics
		}
	}

	// Parse Resource Metadata (JSON object - FASE 3.5 - 08/01/2026)
	if record.ResourceMetadata.Valid && record.ResourceMetadata.String != "" {
		var metadata ai.ResourceMetadata
		if err := json.Unmarshal([]byte(record.ResourceMetadata.String), &metadata); err != nil {
			log.Warn().Err(err).Msg("Failed to parse Resource metadata")
		} else {
			result.ResourceMetadata = &metadata
		}
	}

	// Parse campos estruturados do JSON em Analysis
	// Tentar fazer parse do Analysis como JSON estruturado
	if record.Analysis != "" {
		// Tentar parse como estrutura completa
		var structuredAnalysis struct {
			ExecutiveSummary  ai.ExecutiveSummary           `json:"executive_summary"`
			RootCauseAnalysis ai.RootCauseAnalysis          `json:"root_cause_analysis"`
			ImpactAssessment  ai.ImpactAssessment           `json:"impact_assessment"`
			Recommendations   []ai.ActionableRecommendation `json:"recommendations"`
		}

		if err := json.Unmarshal([]byte(record.Analysis), &structuredAnalysis); err == nil {
			// Parse bem-sucedido - usar campos estruturados
			result.ExecutiveSummary = structuredAnalysis.ExecutiveSummary
			result.RootCauseAnalysis = structuredAnalysis.RootCauseAnalysis
			result.ImpactAssessment = structuredAnalysis.ImpactAssessment
			result.Recommendations = structuredAnalysis.Recommendations

			// Limpar Analysis para não duplicar informação
			result.Analysis = ""
		} else {
			// Parse falhou - manter Analysis como texto (formato legado)
			log.Debug().Err(err).Msg("Analysis is not structured JSON, keeping as legacy format")
		}
	}

	return result, nil
}

// RegisterRoutes registra rotas do handler
func (h *AIDiagnosticsHandler) RegisterRoutes(r *gin.RouterGroup, kubeManager *kubernetes.KubeManager) {
	// Rotas públicas (GET)
	r.GET("/ai/status", h.GetProviderStatus)
	r.GET("/ai/history", h.GetHistory)
	r.GET("/ai/history/:id", h.GetAnalysisByID)
	r.GET("/ai/stats", h.GetStats)

	// Rotas de exportação (GET)
	r.GET("/ai/report/:id/pdf", h.GetReportPDF)
	r.GET("/ai/report/:id/markdown", h.GetReportMarkdown)
	r.GET("/ai/report/:id/csv", h.GetReportCSV)

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
