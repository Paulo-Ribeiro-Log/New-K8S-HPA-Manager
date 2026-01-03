package handlers

import (
	"net/http"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/monitoring/predictions"
	"k8s-hpa-manager/internal/monitoring/prometheus"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// PredictionsHandler gerencia análises preditivas
type PredictionsHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewPredictionsHandler cria novo handler
func NewPredictionsHandler(kubeManager *config.KubeConfigManager) *PredictionsHandler {
	return &PredictionsHandler{
		kubeManager: kubeManager,
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
	var req AnalyzeDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

	// 2. Obter provider AI do usuário
	aiProvider := h.getAIProviderForUser(userEmail)
	if aiProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "AI provider not configured. Please configure your AI tokens first.",
		})
		return
	}

	// 3. Obter KubeManager wrapper
	kubeClient, err := h.kubeManager.GetK8sClient(req.Cluster)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get Kubernetes client")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to connect to cluster: " + req.Cluster,
		})
		return
	}

	// 4. Criar analyzer
	analyzer := predictions.NewAnalyzer(promClient, aiProvider, kubeClient)

	// 5. Executar análise
	ctx := c.Request.Context()
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
	// Obter configuração de Prometheus do cluster
	// Por ora, assumir endpoint padrão
	endpoint := "http://prometheus-server.monitoring.svc.cluster.local:9090"

	// TODO: Buscar endpoint real da configuração do cluster

	return prometheus.NewClient(cluster, endpoint)
}

// getAIProviderForUser obtém provider AI configurado pelo usuário
func (h *PredictionsHandler) getAIProviderForUser(userEmail string) ai.Provider {
	// Reutilizar lógica do AIDiagnosticsHandler
	// Por ora, retornar provider padrão

	// TODO: Buscar tokens configurados pelo usuário e instanciar provider correto

	return nil
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

	kubeClient, err := h.kubeManager.GetK8sClient(cluster)
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
