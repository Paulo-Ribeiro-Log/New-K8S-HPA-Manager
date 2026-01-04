package handlers

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/discovery"
	"k8s-hpa-manager/internal/monitoring/predictions"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// PredictionsHandler gerencia análises preditivas
type PredictionsHandler struct {
	kubeConfigMgr *config.KubeConfigManager // Para pegar k8s clients
	kubeManager   *kubernetes.KubeManager   // Para criar AI analyzers
	analyzer      *ai.Analyzer              // Analyzer padrão (fallback)
	tokensStore   *storage.UserTokensStore
	defaultConfig *ai.Config // Config padrão (flags do servidor)
}

// NewPredictionsHandler cria novo handler
func NewPredictionsHandler(
	kubeConfigMgr *config.KubeConfigManager,
	kubeManager *kubernetes.KubeManager,
	analyzer *ai.Analyzer,
	tokensStore *storage.UserTokensStore,
	defaultConfig *ai.Config,
) *PredictionsHandler {
	return &PredictionsHandler{
		kubeConfigMgr: kubeConfigMgr,
		kubeManager:   kubeManager,
		analyzer:      analyzer,
		tokensStore:   tokensStore,
		defaultConfig: defaultConfig,
	}
}

// AnalyzeDeploymentRequest request para análise
type AnalyzeDeploymentRequest struct {
	Cluster    string `json:"cluster" binding:"required"`
	Namespace  string `json:"namespace" binding:"required"`
	Deployment string `json:"deployment" binding:"required"`
}

// AnalyzeDeployment executa análise preditiva de um deployment
// @Summary Analisa deployment com IA e Prometheus
// @Description Coleta métricas históricas, analisa tendências e prevê problemas
// @Tags Predictions
// @Accept json
// @Produce json
// @Param request body AnalyzeDeploymentRequest true "Request"
// @Success 200 {object} predictions.PredictionResult
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/predictions/analyze [post]
func (h *PredictionsHandler) AnalyzeDeployment(c *gin.Context) {
	// Log ANTES de tudo para confirmar que chegou até aqui
	log.Info().Msg("========== PREDICTIONS HANDLER CALLED ==========")

	// Log headers e content type
	log.Info().
		Str("content_type", c.ContentType()).
		Str("method", c.Request.Method).
		Msg("Request details")

	// Ler body manualmente para debug
	bodyBytes, _ := c.GetRawData()
	log.Info().Str("raw_body", string(bodyBytes)).Msg("Raw request body")

	// Restaurar body para ShouldBindJSON poder ler
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req AnalyzeDeploymentRequest

	// Log do body recebido para debug
	log.Info().Msg("About to bind JSON request")

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().Err(err).Str("error_detail", err.Error()).Msg("FAILED TO BIND JSON REQUEST")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	log.Info().
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("deployment", req.Deployment).
		Msg("Request parsed successfully")

	// Obter user email do contexto (RBAC)
	userEmail := c.GetString("user_email")

	log.Info().
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("deployment", req.Deployment).
		Str("user", userEmail).
		Msg("Prediction analysis requested")

	// 1. Obter cliente Prometheus para o cluster
	promClient, err := h.getPrometheusClient(req.Cluster)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get Prometheus client")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to connect to Prometheus for cluster: " + req.Cluster,
		})
		return
	}

	// 1.1. Testar conexão com Prometheus (lazy connection)
	ctx := c.Request.Context()
	if err := promClient.TestConnection(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to connect to Prometheus")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to connect to Prometheus: " + err.Error(),
		})
		return
	}

	// 2. Obter analyzer AI do usuário (com provider configurado)
	userAnalyzer := h.getAnalyzerForUser(userEmail)
	if userAnalyzer == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI provider not configured. Please configure your AI tokens first.",
		})
		return
	}

	// 3. Obter KubeManager wrapper
	kubeClient, err := h.kubeConfigMgr.GetK8sClient(req.Cluster)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get Kubernetes client")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to connect to cluster: " + req.Cluster,
		})
		return
	}

	// 4. Criar predictions analyzer com provider do usuário
	aiProvider := userAnalyzer.GetProvider()
	if aiProvider == nil {
		log.Error().Msg("AI provider is nil")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "AI provider not properly configured. Please check your AI settings.",
		})
		return
	}
	analyzer := predictions.NewAnalyzer(promClient, aiProvider, kubeClient)

	// 5. Executar análise
	result, err := analyzer.Analyze(ctx, predictions.PredictionRequest{
		Cluster:    req.Cluster,
		Namespace:  req.Namespace,
		Deployment: req.Deployment,
		UserEmail:  userEmail,
	})

	if err != nil {
		log.Error().Err(err).Msg("Prediction analysis failed")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Analysis failed: " + err.Error(),
		})
		return
	}

	// 5. Retornar resultado
	c.JSON(http.StatusOK, result)
}

// ExportReport exporta relatório em formato específico
// @Summary Exporta relatório de análise
// @Tags Predictions
// @Param format query string true "Format (pdf, markdown, json)"
// @Success 200 {file} file
// @Router /api/predictions/export [get]
func (h *PredictionsHandler) ExportReport(c *gin.Context) {
	format := c.Query("format") // pdf, markdown, json

	// Obter resultado da análise do body
	var result predictions.PredictionResult
	if err := c.ShouldBindJSON(&result); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch format {
	case "markdown":
		markdown := h.generateMarkdownReport(&result)
		c.Header("Content-Disposition", "attachment; filename=prediction-report.md")
		c.Data(http.StatusOK, "text/markdown", []byte(markdown))

	case "json":
		c.Header("Content-Disposition", "attachment; filename=prediction-report.json")
		c.JSON(http.StatusOK, result)

	case "pdf":
		// TODO: Implementar geração de PDF
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "PDF export not yet implemented",
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid format. Use: pdf, markdown, or json",
		})
	}
}

// getPrometheusClient obtém cliente Prometheus para cluster
func (h *PredictionsHandler) getPrometheusClient(cluster string) (*prometheus.Client, error) {
	// Descobrir endpoint Prometheus para o cluster
	endpoint, err := discovery.DiscoverEndpoint(cluster)
	if err != nil {
		return nil, err
	}

	// Criar cliente Prometheus com endpoint descoberto
	return prometheus.NewClient(cluster, endpoint.URL)
}

// getAnalyzerForUser obtém analyzer AI configurado pelo usuário
func (h *PredictionsHandler) getAnalyzerForUser(userEmail string) *ai.Analyzer {
	// Se não tiver user email ou tokensStore, usar analyzer padrão
	if userEmail == "" || h.tokensStore == nil || h.analyzer == nil {
		log.Debug().Msg("Using default analyzer (no user email or tokens store)")
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
			if tokens.GeminiModel != "" {
				config.GeminiModel = tokens.GeminiModel
			} else {
				config.GeminiModel = "gemini-2.0-flash-exp"
			}
		} else {
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

	// Criar analyzer com provider personalizado
	// ai.NewAnalyzer precisa de (provider, kubeManager, historyStore)
	// Mas no contexto de predictions, não temos historyStore aqui
	// Vamos retornar o analyzer padrão se não pudermos criar um novo
	analyzer := ai.NewAnalyzer(provider, h.kubeManager, nil)
	log.Info().
		Str("user_email", userEmail).
		Str("provider", tokens.PreferredProvider).
		Msg("User-specific analyzer created successfully")

	return analyzer
}

// generateMarkdownReport gera relatório em formato Markdown
func (h *PredictionsHandler) generateMarkdownReport(result *predictions.PredictionResult) string {
	report := "# Predictive Analysis Report\n\n"

	report += "## Summary\n"
	report += "- **Deployment**: " + result.Deployment + "\n"
	report += "- **Namespace**: " + result.Namespace + "\n"
	report += "- **Cluster**: " + result.Cluster + "\n"
	report += "- **Analyzed**: " + result.AnalyzedAt.Format(time.RFC3339) + "\n"
	report += "- **Health Score**: " + string(rune(result.HealthScore.Overall)) + "/100 (" + result.HealthScore.Category + ")\n\n"

	report += "## Executive Summary\n"
	report += "**Current State**: " + result.ExecutiveSummary.CurrentState + "\n\n"
	report += "**Risk Level**: " + result.ExecutiveSummary.RiskLevel + "\n\n"
	report += "**Key Findings**:\n"
	for _, finding := range result.ExecutiveSummary.KeyFindings {
		report += "- " + finding + "\n"
	}
	report += "\n"

	report += "## Predictions\n"
	report += "### Short Term (4 hours)\n"
	for _, pred := range result.Predictions.ShortTerm {
		report += "- **" + pred.Event + "** (probability: " + string(rune(int(pred.Probability*100))) + "%)\n"
		report += "  - Severity: " + pred.Severity + "\n"
		report += "  - Impact: " + pred.Impact + "\n"
	}
	report += "\n"

	report += "## Recommendations\n"
	for i, rec := range result.Recommendations {
		report += string(rune(i+1)) + ". **" + rec.Title + "** (Priority " + string(rune(rec.Priority)) + ")\n"
		report += "   - " + rec.Description + "\n"
		report += "   - Time: " + rec.ImplementationEstimate.TimeRequired + "\n"
		report += "   - Complexity: " + rec.ImplementationEstimate.Complexity + "\n\n"
	}

	return report
}

// GetHealthScore retorna apenas o health score
// @Summary Retorna health score do deployment
// @Tags Predictions
// @Success 200 {object} predictions.HealthScore
// @Router /api/predictions/health [get]
func (h *PredictionsHandler) GetHealthScore(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	deployment := c.Query("deployment")

	if cluster == "" || namespace == "" || deployment == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "cluster, namespace, and deployment are required",
		})
		return
	}

	// Executar análise rápida apenas para health score
	promClient, err := h.getPrometheusClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to Prometheus"})
		return
	}

	kubeClient, err := h.kubeConfigMgr.GetK8sClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to cluster"})
		return
	}

	collector := predictions.NewMetricsCollector(promClient, kubeClient)
	_, err = collector.CollectMetrics(c.Request.Context(), predictions.PredictionRequest{
		Cluster:    cluster,
		Namespace:  namespace,
		Deployment: deployment,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Por ora, retornar score básico (TODO: implementar cálculo real)
	c.JSON(http.StatusOK, gin.H{
		"health_score": 75,
		"category":     "warning",
		"message":      "Quick health check completed",
	})
}
