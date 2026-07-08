package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	kubeConfigMgr    *config.KubeConfigManager // Para pegar k8s clients
	kubeManager      *kubernetes.KubeManager   // Para criar AI analyzers
	analyzer         *ai.Analyzer              // Analyzer padrão (fallback)
	tokensStore      *storage.UserTokensStore
	predictionsStore *storage.PredictionsStore // Store para histórico de análises
	defaultConfig    *ai.Config                // Config padrão (flags do servidor)
}

// NewPredictionsHandler cria novo handler
func NewPredictionsHandler(
	kubeConfigMgr *config.KubeConfigManager,
	kubeManager *kubernetes.KubeManager,
	analyzer *ai.Analyzer,
	tokensStore *storage.UserTokensStore,
	predictionsStore *storage.PredictionsStore,
	defaultConfig *ai.Config,
) *PredictionsHandler {
	return &PredictionsHandler{
		kubeConfigMgr:    kubeConfigMgr,
		kubeManager:      kubeManager,
		analyzer:         analyzer,
		tokensStore:      tokensStore,
		predictionsStore: predictionsStore,
		defaultConfig:    defaultConfig,
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

	// 🔒 SECURITY: Log qual provider será usado ANTES de executar
	providerName := aiProvider.GetName()
	modelName := aiProvider.GetModel()

	log.Warn().
		Str("user_email", userEmail).
		Str("provider", providerName).
		Str("model", modelName).
		Str("deployment", fmt.Sprintf("%s/%s/%s", req.Cluster, req.Namespace, req.Deployment)).
		Msg("🔒 SECURITY: Predictive analysis will use the following AI provider")

	// 🚨 WARNING CRÍTICO para providers pagos
	if providerName == "claude" || providerName == "openai" || providerName == "copilot" {
		log.Warn().
			Str("provider", providerName).
			Str("model", modelName).
			Msg("WARNING: Using PAID AI provider for predictions - charges may apply!")
	}

	// 5. Executar análise
	result, err := analyzer.Analyze(ctx, predictions.PredictionRequest{
		Cluster:    req.Cluster,
		Namespace:  req.Namespace,
		Deployment: req.Deployment,
		UserEmail:  userEmail,
	})

	if err != nil {
		log.Error().Err(err).Msg("Prediction analysis failed")

		// Registrar erro globalmente para exibir no painel AI Provider Status
		providerName := aiProvider.GetName()
		modelName := aiProvider.GetModel()
		RecordGlobalAIError(userEmail, providerName, modelName, err)

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Analysis failed: " + err.Error(),
		})
		return
	}

	// 6. Salvar resultado no banco de dados para histórico
	if h.predictionsStore != nil {
		// Obter provider e model usado
		providerName := aiProvider.GetName()
		modelName := aiProvider.GetModel()

		// Limpar erro global pois análise foi bem-sucedida
		RecordGlobalAIError(userEmail, providerName, modelName, nil)

		err := h.predictionsStore.SavePrediction(
			result.RequestID,
			result.Cluster,
			result.Namespace,
			result.Deployment,
			float64(result.HealthScore.Overall),
			result.ExecutiveSummary.RiskLevel,
			result.ExecutiveSummary,
			result.Predictions,
			result.Recommendations,
			result.RawMetrics,
			providerName,
			modelName,
			result.DurationMs,
			userEmail,
			result.AnalyzedAt,
		)

		if err != nil {
			// Log erro mas não falha a requisição
			log.Error().Err(err).Msg("Failed to save prediction to database")
		} else {
			log.Info().
				Str("cluster", req.Cluster).
				Str("namespace", req.Namespace).
				Str("deployment", req.Deployment).
				Msg("Prediction saved to history")
		}
	}

	// 7. Retornar resultado
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
		// Gerar PDF básico (texto simples)
		pdfContent := h.generatePDFReport(&result)
		c.Header("Content-Disposition", "attachment; filename=prediction-report.pdf")
		c.Header("Content-Type", "application/pdf")
		c.Data(http.StatusOK, "application/pdf", pdfContent)

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
			Err(err).
			Msg("Using default analyzer (no user preferences found)")
		return h.analyzer
	}

	log.Debug().
		Str("user_email", userEmail).
		Str("preferred_provider", tokens.PreferredProvider).
		Bool("has_gemini_key", tokens.GeminiAPIKey != "").
		Bool("has_claude_key", tokens.ClaudeAPIKey != "").
		Bool("has_openai_key", tokens.OpenAIAPIKey != "").
		Str("gemini_model", tokens.GeminiModel).
		Msg("Retrieved user tokens from database")

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
				config.GeminiModel = "gemini-2.5-flash"
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
			Str("gemini_api_key_length", fmt.Sprintf("%d", len(config.GeminiAPIKey))).
			Str("gemini_model", config.GeminiModel).
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
	var report strings.Builder

	// Header
	report.WriteString("# ANALISE PREDITIVA: " + result.Deployment + "\n\n")
	report.WriteString("**Cluster**: " + result.Cluster + "  \n")
	report.WriteString("**Namespace**: " + result.Namespace + "  \n")
	report.WriteString("**Gerado em**: " + result.AnalyzedAt.Format("02/01/2006 15:04:05 MST") + "  \n")
	report.WriteString(fmt.Sprintf("**Health Score**: %d/100", result.HealthScore.Overall))

	// Status baseado no score
	if result.HealthScore.Overall >= 75 {
		report.WriteString(" - SAUDAVEL\n\n")
	} else if result.HealthScore.Overall >= 50 {
		report.WriteString(" - ATENCAO\n\n")
	} else {
		report.WriteString(" - CRITICO\n\n")
	}

	report.WriteString("---\n\n")

	// Executive Summary
	report.WriteString("## SUMARIO EXECUTIVO\n\n")
	report.WriteString(result.ExecutiveSummary.CurrentState + "\n\n")
	report.WriteString("**Nivel de Risco**: " + strings.ToUpper(result.ExecutiveSummary.RiskLevel) + "  \n")
	if result.ExecutiveSummary.ActionRequired {
		report.WriteString("**Acao Necessaria**: SIM\n\n")
	} else {
		report.WriteString("**Acao Necessaria**: NAO\n\n")
	}

	report.WriteString("### Principais Descobertas\n\n")
	for _, finding := range result.ExecutiveSummary.KeyFindings {
		report.WriteString("- " + finding + "\n")
	}
	report.WriteString("\n")

	report.WriteString("### Impacto no Negócio\n\n")
	report.WriteString(result.ExecutiveSummary.BusinessImpact + "\n\n")

	report.WriteString("---\n\n")

	// Dados Analisados - Nova seção explicativa
	report.WriteString("## DADOS ANALISADOS\n\n")
	report.WriteString("Esta análise foi baseada nas seguintes métricas e observações do deployment:\n\n")

	report.WriteString("### Métricas de Réplicas\n")
	report.WriteString(fmt.Sprintf("- Réplicas Desejadas: %d\n", result.RawMetrics.DesiredReplicas))
	report.WriteString(fmt.Sprintf("- Réplicas Disponíveis: %d\n", result.RawMetrics.AvailableReplicas))
	report.WriteString(fmt.Sprintf("- Réplicas Prontas: %d\n", result.RawMetrics.ReadyReplicas))
	disponibilidade := float64(result.RawMetrics.AvailableReplicas) / float64(result.RawMetrics.DesiredReplicas) * 100
	report.WriteString(fmt.Sprintf("- Taxa de Disponibilidade: %.1f%%\n\n", disponibilidade))

	report.WriteString("### Consumo de Recursos\n")
	report.WriteString(fmt.Sprintf("- CPU Média: %.2f cores (P95: %.2f cores)\n", result.RawMetrics.Current.CPUUsageAvg, result.RawMetrics.Current.CPUUsageP95))
	report.WriteString(fmt.Sprintf("- Memória Média: %.2f GB (P95: %.2f GB)\n",
		result.RawMetrics.Current.MemoryUsageAvg/(1024*1024*1024),
		result.RawMetrics.Current.MemoryUsageP95/(1024*1024*1024)))
	report.WriteString(fmt.Sprintf("- Tendência CPU (7 dias): %.1f%%\n", result.RawMetrics.Trends.CPUChange7d))
	report.WriteString(fmt.Sprintf("- Tendência Memória (7 dias): %.1f%%\n\n", result.RawMetrics.Trends.MemoryChange7d))

	report.WriteString("### Capacidade do Cluster\n")
	report.WriteString(fmt.Sprintf("- CPU Total Disponível: %.2f cores (Utilização: %.1f%%)\n",
		result.RawMetrics.NodeMetrics.TotalCapacity.CPUTotal,
		result.RawMetrics.NodeMetrics.TotalCapacity.CPUUtilization))
	report.WriteString(fmt.Sprintf("- Memória Total: %.2f GB (Utilização: %.1f%%)\n",
		result.RawMetrics.NodeMetrics.TotalCapacity.MemTotal,
		result.RawMetrics.NodeMetrics.TotalCapacity.MemUtilization))
	report.WriteString(fmt.Sprintf("- Nodes: %d/%d em uso\n\n",
		result.RawMetrics.NodeMetrics.NodesUsed,
		result.RawMetrics.NodeMetrics.TotalNodesInCluster))

	// VM Sizing
	if result.RawMetrics.NodeMetrics.VMSizing.PredominantInstanceType != "" {
		report.WriteString("### Tipo de VM/Instance\n")
		report.WriteString(fmt.Sprintf("- Tipo Predominante: %s\n", result.RawMetrics.NodeMetrics.VMSizing.PredominantInstanceType))
		if result.RawMetrics.NodeMetrics.VMSizing.CPUPerVM > 0 {
			report.WriteString(fmt.Sprintf("- CPU por VM: %d cores\n", result.RawMetrics.NodeMetrics.VMSizing.CPUPerVM))
		}
		if result.RawMetrics.NodeMetrics.VMSizing.MemoryPerVM > 0 {
			report.WriteString(fmt.Sprintf("- Memória por VM: %d GB\n", result.RawMetrics.NodeMetrics.VMSizing.MemoryPerVM))
		}
		if result.RawMetrics.NodeMetrics.VMSizing.MaxPodsPerNode > 0 {
			report.WriteString(fmt.Sprintf("- Máximo de Pods por Node: %d\n", result.RawMetrics.NodeMetrics.VMSizing.MaxPodsPerNode))
		}
		if result.RawMetrics.NodeMetrics.VMSizing.RecommendedInstanceType != "" {
			report.WriteString(fmt.Sprintf("- Tipo Recomendado: %s\n", result.RawMetrics.NodeMetrics.VMSizing.RecommendedInstanceType))
			if result.RawMetrics.NodeMetrics.VMSizing.RecommendationReason != "" {
				report.WriteString(fmt.Sprintf("- Razão: %s\n", result.RawMetrics.NodeMetrics.VMSizing.RecommendationReason))
			}
		}
		report.WriteString("\n")
	}

	// Aplicações Concorrentes
	if len(result.RawMetrics.CompetingApps) > 0 {
		report.WriteString("### Aplicações Concorrentes nas Mesmas VMs\n\n")
		report.WriteString("**IMPORTANTE**: As VMs/Nodes não dispõem de recursos totais para este deployment.\n")
		report.WriteString("Os recursos são compartilhados e há concorrência com outras aplicações:\n\n")

		// Ordenar por impacto
		highImpact := []predictions.CompetingApp{}
		mediumImpact := []predictions.CompetingApp{}
		lowImpact := []predictions.CompetingApp{}

		for _, app := range result.RawMetrics.CompetingApps {
			switch app.ImpactLevel {
			case "high":
				highImpact = append(highImpact, app)
			case "medium":
				mediumImpact = append(mediumImpact, app)
			default:
				lowImpact = append(lowImpact, app)
			}
		}

		if len(highImpact) > 0 {
			report.WriteString("**Impacto Alto:**\n\n")
			for _, app := range highImpact {
				report.WriteString(fmt.Sprintf("- **%s** (%s) - %d réplicas\n", app.Name, app.Namespace, app.Replicas))
				report.WriteString(fmt.Sprintf("  - CPU: %.2f cores\n", app.CPUUsage))
				report.WriteString(fmt.Sprintf("  - Memória: %.2f GB\n", app.MemoryUsage))
			}
			report.WriteString("\n")
		}

		if len(mediumImpact) > 0 {
			report.WriteString("**Impacto Médio:**\n\n")
			for _, app := range mediumImpact {
				report.WriteString(fmt.Sprintf("- **%s** (%s) - %d réplicas\n", app.Name, app.Namespace, app.Replicas))
				report.WriteString(fmt.Sprintf("  - CPU: %.2f cores | Memória: %.2f GB\n", app.CPUUsage, app.MemoryUsage))
			}
			report.WriteString("\n")
		}

		if len(lowImpact) > 0 {
			report.WriteString("**Impacto Baixo:**\n\n")
			for _, app := range lowImpact {
				report.WriteString(fmt.Sprintf("- %s (%s) - %d réplicas: CPU %.2f cores | Mem %.2f GB\n", app.Name, app.Namespace, app.Replicas, app.CPUUsage, app.MemoryUsage))
			}
			report.WriteString("\n")
		}

		// Totais
		totalCPU := 0.0
		totalMem := 0.0
		for _, app := range result.RawMetrics.CompetingApps {
			totalCPU += app.CPUUsage
			totalMem += app.MemoryUsage
		}
		report.WriteString(fmt.Sprintf("**Total Consumido por Aplicações Concorrentes**: %.2f cores CPU | %.2f GB Memória\n\n", totalCPU, totalMem))
	}

	// Análise de Capacidade para Crescimento
	growth := result.RawMetrics.CapacityForecast.GrowthAnalysis
	report.WriteString("### ANÁLISE DE CAPACIDADE PARA CRESCIMENTO HORIZONTAL\n\n")

	// Node Pool Info
	report.WriteString("**Configuração do Node Pool:**\n")
	report.WriteString(fmt.Sprintf("- Nodes Mínimos: %d\n", result.RawMetrics.NodeMetrics.VMSizing.MinNodes))
	report.WriteString(fmt.Sprintf("- Nodes Máximos: %d\n", result.RawMetrics.NodeMetrics.VMSizing.MaxNodes))
	report.WriteString(fmt.Sprintf("- Nodes Atuais: %d\n\n", result.RawMetrics.NodeMetrics.VMSizing.CurrentNodes))

	// Aplicação em Análise
	report.WriteString("**Aplicação em Análise:**\n")
	report.WriteString(fmt.Sprintf("- Réplicas Atuais: %d\n", growth.TargetApp.Replicas))
	report.WriteString(fmt.Sprintf("- CPU Total: %.2f cores (%.3f cores/réplica)\n",
		growth.TargetApp.Usage.CPUCores,
		growth.TargetApp.Usage.CPUCores/float64(growth.TargetApp.Replicas)))
	report.WriteString(fmt.Sprintf("- Memória Total: %.2f GB (%.2f GB/réplica)\n\n",
		growth.TargetApp.Usage.MemoryGB,
		growth.TargetApp.Usage.MemoryGB/float64(growth.TargetApp.Replicas)))

	// Aplicações Concorrentes - Resumo com Réplicas
	if len(growth.CompetingApps) > 0 {
		report.WriteString("**Aplicações Concorrentes (com Réplicas):**\n\n")
		report.WriteString("| Aplicação | Namespace | Réplicas | CPU Total | Memory Total | CPU/Réplica | Mem/Réplica |\n")
		report.WriteString("|-----------|-----------|----------|-----------|--------------|-------------|-------------|\n")

		for _, app := range growth.CompetingApps {
			cpuPerReplica := app.Usage.CPUCores
			memPerReplica := app.Usage.MemoryGB
			if app.Replicas > 0 {
				cpuPerReplica = app.Usage.CPUCores / float64(app.Replicas)
				memPerReplica = app.Usage.MemoryGB / float64(app.Replicas)
			}
			report.WriteString(fmt.Sprintf("| %s | %s | %d | %.2f cores | %.2f GB | %.3f cores | %.2f GB |\n",
				app.Name, app.Namespace, app.Replicas,
				app.Usage.CPUCores, app.Usage.MemoryGB,
				cpuPerReplica, memPerReplica))
		}
		report.WriteString("\n")
		report.WriteString(fmt.Sprintf("**Total Concorrentes**: %.2f cores CPU | %.2f GB Memória\n\n",
			growth.TotalCompetingUsage.CPUCores, growth.TotalCompetingUsage.MemoryGB))
	}

	// Capacidade Total
	report.WriteString("**Capacidade do Cluster:**\n\n")
	report.WriteString("| Cenário | Nodes | CPU Total | Memória Total |\n")
	report.WriteString("|---------|-------|-----------|---------------|\n")
	report.WriteString(fmt.Sprintf("| Atual | %d | %.2f cores | %.2f GB |\n",
		growth.CurrentCapacity.Nodes, growth.CurrentCapacity.Resources.CPUCores, growth.CurrentCapacity.Resources.MemoryGB))
	report.WriteString(fmt.Sprintf("| Máximo (se escalar) | %d | %.2f cores | %.2f GB |\n\n",
		growth.MaxCapacity.Nodes, growth.MaxCapacity.Resources.CPUCores, growth.MaxCapacity.Resources.MemoryGB))

	// Disponível para Crescimento
	report.WriteString("**Capacidade Disponível para Crescimento:**\n")
	report.WriteString(fmt.Sprintf("- CPU Disponível: %.2f cores\n", growth.AvailableForGrowth.CPUCores))
	report.WriteString(fmt.Sprintf("- Memória Disponível: %.2f GB\n", growth.AvailableForGrowth.MemoryGB))
	report.WriteString(fmt.Sprintf("- Recurso Gargalo: **%s**\n\n", growth.BottleneckResource))

	// Cenários de Crescimento
	report.WriteString("**Cenários de Escalabilidade:**\n\n")
	report.WriteString("| Cenário | Máximo de Réplicas |\n")
	report.WriteString("|---------|--------------------|\n")
	report.WriteString(fmt.Sprintf("| Nodes Atuais (%d) | **%d réplicas** |\n",
		growth.CurrentCapacity.Nodes, growth.MaxReplicasCurrentNodes))
	report.WriteString(fmt.Sprintf("| Escalando para Max Nodes (%d) | **%d réplicas** |\n",
		growth.MaxCapacity.Nodes, growth.MaxReplicasWithMaxNodes))
	if growth.ReplicasIfRemoveCompeting > growth.MaxReplicasCurrentNodes {
		report.WriteString(fmt.Sprintf("| Se remover aplicações concorrentes | **%d réplicas** |\n",
			growth.ReplicasIfRemoveCompeting))
	}
	report.WriteString("\n")

	// Recomendação Final
	report.WriteString("**RECOMENDAÇÃO:**\n")
	report.WriteString(fmt.Sprintf("- %s\n", growth.GrowthRecommendation))
	report.WriteString(fmt.Sprintf("- Máximo Recomendado: **%d réplicas**\n\n", growth.RecommendedMaxReplicas))

	report.WriteString("---\n\n")

	// Análise de Custos
	if result.CostAnalysis != nil {
		cost := result.CostAnalysis
		report.WriteString("## ANÁLISE DE CUSTOS\n\n")
		report.WriteString(fmt.Sprintf("**Cotação USD/BRL**: R$ %.2f (referência: %s)\n\n", cost.ExchangeRate, cost.ExchangeRateDate))

		report.WriteString("### Custo Mensal Atual\n\n")
		report.WriteString("| Recurso | USD | BRL |\n")
		report.WriteString("|---------|-----|-----|\n")
		report.WriteString(fmt.Sprintf("| CPU | $ %.2f | R$ %.2f |\n", cost.CostBreakdown.CPUCostUSD, cost.CostBreakdown.CPUCostBRL))
		report.WriteString(fmt.Sprintf("| Memória | $ %.2f | R$ %.2f |\n", cost.CostBreakdown.MemoryCostUSD, cost.CostBreakdown.MemoryCostBRL))
		report.WriteString(fmt.Sprintf("| **Total** | **$ %.2f** | **R$ %.2f** |\n\n", cost.CurrentMonthlyCostUSD, cost.CurrentMonthlyCostBRL))
		report.WriteString(fmt.Sprintf("**Custo por réplica**: $ %.2f / R$ %.2f\n\n", cost.CostPerReplicaUSD, cost.CostPerReplicaBRL))

		if cost.SavingsPercent > 0 {
			report.WriteString("### Potencial de Otimização\n\n")
			report.WriteString(fmt.Sprintf("- Custo otimizado: $ %.2f / R$ %.2f por mês\n", cost.RecommendedCostUSD, cost.RecommendedCostBRL))
			report.WriteString(fmt.Sprintf("- Economia mensal: **$ %.2f / R$ %.2f** (%.1f%%)\n", cost.MonthlySavingsUSD, cost.MonthlySavingsBRL, cost.SavingsPercent))
			report.WriteString(fmt.Sprintf("- Economia anual: **$ %.2f / R$ %.2f**\n\n", cost.AnnualSavingsUSD, cost.AnnualSavingsBRL))
		}

		if len(cost.Recommendations) > 0 {
			report.WriteString("### Recomendações de Custo\n\n")
			report.WriteString("| Ação | Antes (USD) | Depois (USD) | Economia (USD) | Economia (BRL) | Impacto |\n")
			report.WriteString("|------|-------------|--------------|----------------|----------------|---------|\n")
			for _, rec := range cost.Recommendations {
				report.WriteString(fmt.Sprintf("| %s | $ %.2f | $ %.2f | $ %.2f | R$ %.2f | %s |\n",
					rec.Title, rec.CostBeforeUSD, rec.CostAfterUSD, rec.SavingsUSD, rec.SavingsBRL, rec.Impact))
			}
			report.WriteString("\n")
		}

		report.WriteString("---\n\n")
	}

	// Health Score Breakdown
	report.WriteString("## HEALTH SCORE DETALHADO\n\n")
	report.WriteString("O Health Score é calculado com base em 4 dimensões principais:\n\n")
	report.WriteString(fmt.Sprintf("**Overall**: %d/100 (%s)\n\n", result.HealthScore.Overall, result.HealthScore.Category))

	report.WriteString("### Breakdown por Dimensão\n\n")
	report.WriteString("| Metrica | Score | Interpretação |\n")
	report.WriteString("|---------|-------|---------------|\n")

	availInterpretation := "Excelente"
	if result.HealthScore.Breakdown.Availability < 75 {
		availInterpretation = "Precisa atenção"
	} else if result.HealthScore.Breakdown.Availability < 90 {
		availInterpretation = "Bom, mas melhorável"
	}
	report.WriteString(fmt.Sprintf("| Availability | %d/100 | %s |\n", result.HealthScore.Breakdown.Availability, availInterpretation))

	perfInterpretation := "Excelente"
	if result.HealthScore.Breakdown.Performance < 75 {
		perfInterpretation = "Precisa atenção"
	} else if result.HealthScore.Breakdown.Performance < 90 {
		perfInterpretation = "Bom, mas melhorável"
	}
	report.WriteString(fmt.Sprintf("| Performance | %d/100 | %s |\n", result.HealthScore.Breakdown.Performance, perfInterpretation))

	stabInterpretation := "Excelente"
	if result.HealthScore.Breakdown.Stability < 75 {
		stabInterpretation = "Precisa atenção"
	} else if result.HealthScore.Breakdown.Stability < 90 {
		stabInterpretation = "Bom, mas melhorável"
	}
	report.WriteString(fmt.Sprintf("| Stability | %d/100 | %s |\n", result.HealthScore.Breakdown.Stability, stabInterpretation))

	effInterpretation := "Excelente"
	if result.HealthScore.Breakdown.Efficiency < 75 {
		effInterpretation = "Precisa atenção"
	} else if result.HealthScore.Breakdown.Efficiency < 90 {
		effInterpretation = "Bom, mas melhorável"
	}
	report.WriteString(fmt.Sprintf("| Efficiency | %d/100 | %s |\n\n", result.HealthScore.Breakdown.Efficiency, effInterpretation))

	report.WriteString("**Como interpretamos estes scores:**\n\n")
	report.WriteString("- **Availability**: Mede a taxa de réplicas disponíveis vs. desejadas e histórico de downtime\n")
	report.WriteString("- **Performance**: Avalia utilização de CPU/memória, latência e capacidade de resposta\n")
	report.WriteString("- **Stability**: Considera restarts, crashloops, erros de health checks e variação de réplicas\n")
	report.WriteString("- **Efficiency**: Analisa otimização de recursos, desperdício e relação requests/limits\n\n")

	report.WriteString("---\n\n")

	// Predictions
	report.WriteString("## PREVISOES\n\n")
	report.WriteString("Com base nos dados coletados e padrões identificados, a IA prevê os seguintes eventos.\n")
	report.WriteString("Cada previsão inclui o nível de severidade, probabilidade de ocorrência e os indicadores que levaram à conclusão.\n\n")

	if len(result.Predictions.ShortTerm) > 0 {
		report.WriteString("### Curto Prazo (proximas 4 horas)\n\n")
		for i, pred := range result.Predictions.ShortTerm {
			report.WriteString(fmt.Sprintf("%d. [%s] **%s** (Probabilidade: %.0f%%)\n", i+1, strings.ToUpper(pred.Severity), pred.Event, pred.Probability*100))
			report.WriteString("   - **Timeframe**: " + pred.Timeframe + "\n")
			report.WriteString("   - **Impacto**: " + pred.Impact + "\n")
			if len(pred.Indicators) > 0 {
				report.WriteString("   - **Indicadores**:\n")
				for _, ind := range pred.Indicators {
					report.WriteString("     - " + ind + "\n")
				}
			}
			report.WriteString("\n")
		}
	}

	if len(result.Predictions.MediumTerm) > 0 {
		report.WriteString("### Medio Prazo (proximas 24 horas)\n\n")
		for i, pred := range result.Predictions.MediumTerm {
			report.WriteString(fmt.Sprintf("%d. [%s] **%s** (Probabilidade: %.0f%%)\n", i+1, strings.ToUpper(pred.Severity), pred.Event, pred.Probability*100))
			report.WriteString("   - **Timeframe**: " + pred.Timeframe + "\n")
			report.WriteString("   - **Impacto**: " + pred.Impact + "\n\n")
		}
	}

	if len(result.Predictions.LongTerm) > 0 {
		report.WriteString("### Longo Prazo (proximos 7 dias)\n\n")
		for i, pred := range result.Predictions.LongTerm {
			report.WriteString(fmt.Sprintf("%d. [%s] **%s** (Probabilidade: %.0f%%)\n", i+1, strings.ToUpper(pred.Severity), pred.Event, pred.Probability*100))
			report.WriteString("   - **Timeframe**: " + pred.Timeframe + "\n")
			report.WriteString("   - **Impacto**: " + pred.Impact + "\n\n")
		}
	}

	report.WriteString("---\n\n")

	// Root Cause Analysis
	if len(result.RootCauseAnalysis.IdentifiedCauses) > 0 {
		report.WriteString("## ANALISE DE CAUSA RAIZ\n\n")
		report.WriteString("A análise identificou as seguintes causas com base em evidências concretas das métricas coletadas.\n")
		report.WriteString("O nível de certeza indica a confiança da IA na identificação da causa.\n\n")
		report.WriteString("**Fator Primario Identificado**: " + result.RootCauseAnalysis.PrimaryFactor + "\n\n")

		for i, cause := range result.RootCauseAnalysis.IdentifiedCauses {
			report.WriteString(fmt.Sprintf("### Causa %d: %s (Certeza: %.0f%%)\n\n", i+1, cause.Cause, cause.Certainty*100))
			report.WriteString("**Categoria**: " + cause.Category + "\n\n")
			report.WriteString("**Evidências**:\n")
			for _, evidence := range cause.Evidence {
				report.WriteString("- " + evidence + "\n")
			}
			report.WriteString("\n**Remediação**: " + cause.Remediation + "\n\n")
		}

		if len(result.RootCauseAnalysis.ContributingFactors) > 0 {
			report.WriteString("**Fatores Contribuintes**:\n")
			for _, factor := range result.RootCauseAnalysis.ContributingFactors {
				report.WriteString("- " + factor + "\n")
			}
			report.WriteString("\n")
		}

		report.WriteString("---\n\n")
	}

	// Impact Analysis
	report.WriteString("## ANALISE DE IMPACTO\n\n")
	report.WriteString("Esta seção compara dois cenários para ajudar na tomada de decisão:\n\n")

	report.WriteString("### Cenário 1: Se Nenhuma Acao For Tomada\n\n")
	report.WriteString("**Análise do que acontecerá se o estado atual for mantido:**\n\n")
	report.WriteString("**Impacto nos Usuários**: " + result.ImpactAnalysis.IfNoAction.UserImpact + "\n\n")
	report.WriteString("**Impacto na Infraestrutura**: " + result.ImpactAnalysis.IfNoAction.InfrastructureImpact + "\n\n")
	report.WriteString("**Timeline**: " + result.ImpactAnalysis.IfNoAction.TimelineDescription + "\n\n")
	if len(result.ImpactAnalysis.IfNoAction.Risks) > 0 {
		report.WriteString("**Riscos**:\n")
		for _, risk := range result.ImpactAnalysis.IfNoAction.Risks {
			report.WriteString("- " + risk + "\n")
		}
		report.WriteString("\n")
	}

	report.WriteString("### Cenário 2: Se Otimizacoes Forem Aplicadas\n\n")
	report.WriteString("**Análise dos benefícios esperados ao implementar as recomendações:**\n\n")
	report.WriteString("**Impacto nos Usuários**: " + result.ImpactAnalysis.IfOptimizationsApplied.UserImpact + "\n\n")
	report.WriteString("**Impacto na Infraestrutura**: " + result.ImpactAnalysis.IfOptimizationsApplied.InfrastructureImpact + "\n\n")
	report.WriteString("**Timeline**: " + result.ImpactAnalysis.IfOptimizationsApplied.TimelineDescription + "\n\n")
	if len(result.ImpactAnalysis.IfOptimizationsApplied.Benefits) > 0 {
		report.WriteString("**Benefícios**:\n")
		for _, benefit := range result.ImpactAnalysis.IfOptimizationsApplied.Benefits {
			report.WriteString("- " + benefit + "\n")
		}
		report.WriteString("\n")
	}

	report.WriteString("**Prioridade de Ação**: " + strings.ToUpper(result.ImpactAnalysis.RecommendedActionPriority) + "  \n")
	report.WriteString("**Timeline para Ação**: " + result.ImpactAnalysis.TimelineToAction + "\n\n")

	report.WriteString("---\n\n")

	// Recommendations
	if len(result.Recommendations) > 0 {
		report.WriteString("## RECOMENDACOES PRIORITARIAS\n\n")
		report.WriteString("As recomendações abaixo são ordenadas por prioridade e foram geradas considerando:\n")
		report.WriteString("- O estado atual do deployment e suas métricas\n")
		report.WriteString("- Os problemas identificados na análise de causa raiz\n")
		report.WriteString("- O impacto esperado vs. complexidade de implementação\n")
		report.WriteString("- As previsões de eventos futuros\n")
		report.WriteString("- **Oportunidades de economia de custos e otimização de recursos**\n\n")

		// Identificar recomendações de custo
		hasCostOptimization := false
		for _, rec := range result.Recommendations {
			if rec.Category == "cost-optimization" || rec.Category == "downsizing" {
				hasCostOptimization = true
				break
			}
		}

		if hasCostOptimization {
			report.WriteString("### [ALERTA] OPORTUNIDADE DE ECONOMIA DE CUSTOS IDENTIFICADA\n\n")
			report.WriteString("**IMPORTANTE**: Há recursos sobreprovisionados que podem ser reduzidos sem impacto negativo.\n")
			report.WriteString("A otimização destes recursos pode resultar em economia significativa de custos.\n\n")
		}

		for _, rec := range result.Recommendations {
			report.WriteString(fmt.Sprintf("### %d. %s\n\n", rec.Priority, rec.Title))

			// Destacar economia de custos
			if rec.Category == "cost-optimization" || rec.Category == "downsizing" {
				report.WriteString("**[ECONOMIA DE CUSTOS]**\n\n")
			}

			report.WriteString("**Categoria**: " + rec.Category + "\n\n")
			report.WriteString("**Por que esta recomendação?**\n")
			report.WriteString(rec.Description + "\n\n")

			report.WriteString("**Impacto Esperado**: " + rec.ExpectedImpact + "\n\n")

			if len(rec.Actions) > 0 {
				report.WriteString("**Ações**:\n")
				for i, action := range rec.Actions {
					report.WriteString(fmt.Sprintf("%d. %s\n", i+1, action))
				}
				report.WriteString("\n")
			}

			report.WriteString("**Estimativa de Implementacao**:\n")
			report.WriteString("- Tempo: " + rec.ImplementationEstimate.TimeRequired + "\n")
			report.WriteString("- Complexidade: " + rec.ImplementationEstimate.Complexity + "\n")
			report.WriteString("- Risco: " + rec.ImplementationEstimate.RiskLevel + "\n")
			if rec.ImplementationEstimate.RequiresDowntime {
				report.WriteString("- Requer Downtime: SIM\n")
			} else {
				report.WriteString("- Requer Downtime: NAO\n")
			}
			if rec.ImplementationEstimate.ResourceEfficiencyGain > 0 {
				report.WriteString(fmt.Sprintf("- Ganho de Eficiencia: %.1f%%\n", rec.ImplementationEstimate.ResourceEfficiencyGain))
			}
			report.WriteString("\n")
		}
	}

	report.WriteString("---\n\n")

	// Metrics Summary
	report.WriteString("## RESUMO DE METRICAS\n\n")
	report.WriteString("### Réplicas\n")
	report.WriteString(fmt.Sprintf("- Desejadas: %d\n", result.RawMetrics.DesiredReplicas))
	report.WriteString(fmt.Sprintf("- Disponíveis: %d\n", result.RawMetrics.AvailableReplicas))
	report.WriteString(fmt.Sprintf("- Prontas: %d\n\n", result.RawMetrics.ReadyReplicas))

	report.WriteString("### CPU\n")
	report.WriteString(fmt.Sprintf("- Uso Atual Médio: %.2f cores\n", result.RawMetrics.Current.CPUUsageAvg))
	report.WriteString(fmt.Sprintf("- Uso P95: %.2f cores\n", result.RawMetrics.Current.CPUUsageP95))
	report.WriteString(fmt.Sprintf("- Tendência (7d): %.1f%%\n\n", result.RawMetrics.Trends.CPUChange7d))

	report.WriteString("### Memória\n")
	report.WriteString(fmt.Sprintf("- Uso Atual Médio: %.2f GB\n", result.RawMetrics.Current.MemoryUsageAvg/(1024*1024*1024)))
	report.WriteString(fmt.Sprintf("- Uso P95: %.2f GB\n", result.RawMetrics.Current.MemoryUsageP95/(1024*1024*1024)))
	report.WriteString(fmt.Sprintf("- Tendência (7d): %.1f%%\n\n", result.RawMetrics.Trends.MemoryChange7d))

	report.WriteString("### Capacidade do Cluster\n")
	report.WriteString(fmt.Sprintf("- CPU Total: %.2f cores (%.1f%% utilizado)\n",
		result.RawMetrics.NodeMetrics.TotalCapacity.CPUTotal,
		result.RawMetrics.NodeMetrics.TotalCapacity.CPUUtilization))
	report.WriteString(fmt.Sprintf("- Memória Total: %.2f GB (%.1f%% utilizado)\n",
		result.RawMetrics.NodeMetrics.TotalCapacity.MemTotal,
		result.RawMetrics.NodeMetrics.TotalCapacity.MemUtilization))
	report.WriteString(fmt.Sprintf("- Nodes utilizados: %d/%d\n\n",
		result.RawMetrics.NodeMetrics.NodesUsed,
		result.RawMetrics.NodeMetrics.TotalNodesInCluster))

	report.WriteString("---\n\n")
	report.WriteString(fmt.Sprintf("*Relatório gerado por K8s HPA Manager em %s*\n",
		result.AnalyzedAt.Format("02/01/2006 15:04:05")))

	return report.String()
}

// generatePDFReport gera PDF simples com análise (formato básico sem biblioteca externa)
func (h *PredictionsHandler) generatePDFReport(result *predictions.PredictionResult) []byte {
	// PDF header básico (simplified PDF format)
	// Para produção, usar biblioteca como github.com/jung-kurt/gofpdf
	pdfContent := "%PDF-1.4\n"
	pdfContent += "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	pdfContent += "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n"
	pdfContent += "3 0 obj\n<< /Type /Page /Parent 2 0 R /Resources 4 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>\nendobj\n"
	pdfContent += "4 0 obj\n<< /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Courier >> >> >>\nendobj\n"

	// Conteúdo simplificado
	streamContent := fmt.Sprintf("BT\n/F1 12 Tf\n50 750 Td\n15 TL\n")

	// Adicionar título
	streamContent += fmt.Sprintf("(%s - Analise Preditiva) Tj\nT*\nT*\n", escapeForPDF(result.Deployment))
	streamContent += fmt.Sprintf("(Cluster: %s | Namespace: %s) Tj\nT*\n", escapeForPDF(result.Cluster), escapeForPDF(result.Namespace))
	streamContent += fmt.Sprintf("(Data: %s) Tj\nT*\nT*\n", result.AnalyzedAt.Format("02/01/2006 15:04:05"))

	// Health Score
	streamContent += fmt.Sprintf("(Health Score: %d/100 - %s) Tj\nT*\nT*\n",
		result.HealthScore.Overall,
		result.HealthScore.Category)

	// Executive Summary
	streamContent += "(Resumo Executivo:) Tj\nT*\n"
	streamContent += fmt.Sprintf("(Risk Level: %s) Tj\nT*\n", result.ExecutiveSummary.RiskLevel)
	// Limitar texto para caber no PDF
	summary := result.ExecutiveSummary.CurrentState
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	streamContent += fmt.Sprintf("(%s) Tj\nT*\nT*\n", escapeForPDF(summary))

	// Previsões - ShortTerm
	streamContent += "(Previsoes Curto Prazo:) Tj\nT*\n"
	for i, pred := range result.Predictions.ShortTerm {
		if i >= 3 { // Limitar a 3 previsões
			break
		}
		streamContent += fmt.Sprintf("(- %s: %s) Tj\nT*\n", pred.Timeframe, escapeForPDF(pred.Event))
	}
	streamContent += "T*\n"

	// Recomendações
	streamContent += "(Recomendacoes:) Tj\nT*\n"
	for i, rec := range result.Recommendations {
		if i >= 3 { // Limitar a 3 recomendações
			break
		}
		streamContent += fmt.Sprintf("(- %s) Tj\nT*\n", escapeForPDF(rec.Title))
	}

	streamContent += "ET\n"

	// Stream object
	pdfContent += fmt.Sprintf("5 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(streamContent), streamContent)

	// Xref table
	pdfContent += "xref\n0 6\n"
	pdfContent += "0000000000 65535 f \n"
	pdfContent += "0000000009 00000 n \n"
	pdfContent += "0000000058 00000 n \n"
	pdfContent += "0000000115 00000 n \n"
	pdfContent += "0000000229 00000 n \n"
	pdfContent += "0000000329 00000 n \n"

	// Trailer
	pdfContent += "trailer\n<< /Size 6 /Root 1 0 R >>\n"
	pdfContent += "startxref\n"
	pdfContent += fmt.Sprintf("%d\n", len(pdfContent)-200) // Aproximação
	pdfContent += "%%EOF\n"

	return []byte(pdfContent)
}

// escapeForPDF escapa caracteres especiais para PDF
func escapeForPDF(s string) string {
	// Remover caracteres que podem quebrar PDF
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	// Limitar tamanho da linha
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
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

// GetHistory retorna histórico de análises com filtros
// @Summary Lista histórico de análises preditivas
// @Tags Predictions
// @Param cluster query string false "Cluster name"
// @Param namespace query string false "Namespace"
// @Param deployment query string false "Deployment name"
// @Param risk_level query string false "Risk level (critical, high, medium, low)"
// @Param start_date query string false "Start date (RFC3339)"
// @Param end_date query string false "End date (RFC3339)"
// @Param limit query int false "Limit results" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {array} storage.PredictionRecord
// @Router /api/predictions/history [get]
func (h *PredictionsHandler) GetHistory(c *gin.Context) {
	if h.predictionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Predictions history not available",
		})
		return
	}

	// Obter user email do contexto (RBAC)
	userEmail := c.GetString("user_email")

	// Construir filtros a partir dos query params
	filters := storage.PredictionQueryFilters{
		Cluster:    c.Query("cluster"),
		Namespace:  c.Query("namespace"),
		Deployment: c.Query("deployment"),
		RiskLevel:  c.Query("risk_level"),
		UserEmail:  userEmail, // Filtrar por usuário logado
	}

	// Parse datas se fornecidas
	if startDate := c.Query("start_date"); startDate != "" {
		if t, err := parseTime(startDate); err == nil {
			filters.StartDate = &t
		}
	}
	if endDate := c.Query("end_date"); endDate != "" {
		if t, err := parseTime(endDate); err == nil {
			filters.EndDate = &t
		}
	}

	// Parse pagination
	if limit := c.Query("limit"); limit != "" {
		if l, err := parseInt(limit); err == nil && l > 0 {
			filters.Limit = l
		}
	} else {
		filters.Limit = 50 // Default
	}

	if offset := c.Query("offset"); offset != "" {
		if o, err := parseInt(offset); err == nil && o >= 0 {
			filters.Offset = o
		}
	}

	// Buscar histórico
	records, err := h.predictionsStore.Query(&filters)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query predictions history")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve history",
		})
		return
	}

	log.Error().
		Int("records_count", len(records)).
		Msg("=== PREDICTIONS HISTORY: Records retrieved from database ===")

	// Contar total para pagination
	total, err := h.predictionsStore.Count(&filters)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to count total records")
		total = len(records) // Fallback
	}

	// Converter records para formato adequado para frontend (parse JSON strings)
	frontendRecords := make([]map[string]interface{}, len(records))
	for i, rec := range records {
		log.Error().
			Str("id", rec.ID).
			Str("deployment", rec.Deployment).
			Int("predictions_len", len(rec.Predictions)).
			Int("recommendations_len", len(rec.Recommendations)).
			Str("predictions_preview", rec.Predictions[:min(50, len(rec.Predictions))]).
			Msg("=== PREDICTIONS HISTORY: Converting record ===")
		frontendRecords[i] = h.convertRecordForFrontend(rec)
	}

	log.Error().
		Int("frontend_records_count", len(frontendRecords)).
		Msg("=== PREDICTIONS HISTORY: Conversion complete ===")

	c.JSON(http.StatusOK, gin.H{
		"records": frontendRecords,
		"total":   total,
		"limit":   filters.Limit,
		"offset":  filters.Offset,
	})
}

// GetHistoryByID retorna uma análise específica por ID
// @Summary Retorna análise preditiva por ID
// @Tags Predictions
// @Param id path int true "Record ID"
// @Success 200 {object} storage.PredictionRecord
// @Router /api/predictions/history/:id [get]
func (h *PredictionsHandler) GetHistoryByID(c *gin.Context) {
	if h.predictionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Predictions history not available",
		})
		return
	}

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ID is required",
		})
		return
	}

	record, err := h.predictionsStore.GetByID(id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("Failed to get prediction by ID")
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Record not found",
		})
		return
	}

	c.JSON(http.StatusOK, record)
}

// GetLatestForDeployment retorna a análise mais recente para um deployment
// @Summary Retorna última análise de um deployment
// @Tags Predictions
// @Param cluster query string true "Cluster name"
// @Param namespace query string true "Namespace"
// @Param deployment query string true "Deployment name"
// @Success 200 {object} storage.PredictionRecord
// @Router /api/predictions/history/latest [get]
func (h *PredictionsHandler) GetLatestForDeployment(c *gin.Context) {
	if h.predictionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Predictions history not available",
		})
		return
	}

	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	deployment := c.Query("deployment")

	if cluster == "" || namespace == "" || deployment == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "cluster, namespace, and deployment are required",
		})
		return
	}

	record, err := h.predictionsStore.GetLatestForDeployment(cluster, namespace, deployment)
	if err != nil {
		log.Error().Err(err).
			Str("cluster", cluster).
			Str("namespace", namespace).
			Str("deployment", deployment).
			Msg("Failed to get latest prediction")
		c.JSON(http.StatusNotFound, gin.H{
			"error": "No records found for this deployment",
		})
		return
	}

	c.JSON(http.StatusOK, record)
}

// GetStatistics retorna estatísticas agregadas das análises
// @Summary Retorna estatísticas de análises preditivas
// @Tags Predictions
// @Param cluster query string false "Cluster name"
// @Success 200 {object} map[string]interface{}
// @Router /api/predictions/statistics [get]
func (h *PredictionsHandler) GetStatistics(c *gin.Context) {
	if h.predictionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Predictions history not available",
		})
		return
	}

	cluster := c.Query("cluster")

	stats, err := h.predictionsStore.GetStatistics(cluster)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get statistics")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve statistics",
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// Helper functions
func parseTime(s string) (time.Time, error) {
	// Tentar vários formatos
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid time format: %s", s)
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// convertRecordForFrontend converte PredictionRecord com JSON strings para formato adequado
func (h *PredictionsHandler) convertRecordForFrontend(rec *storage.PredictionRecord) map[string]interface{} {
	result := map[string]interface{}{
		"id":           rec.ID,
		"cluster":      rec.Cluster,
		"namespace":    rec.Namespace,
		"deployment":   rec.Deployment,
		"health_score": rec.HealthScore,
		"risk_level":   rec.RiskLevel,
		"provider":     rec.Provider,
		"model":        rec.Model,
		"duration_ms":  rec.DurationMs,
		"user_email":   rec.UserEmail,
		"analyzed_at":  rec.AnalyzedAt,
		"created_at":   rec.CreatedAt,
	}

	// Log para debug
	log.Debug().
		Str("id", rec.ID).
		Str("deployment", rec.Deployment).
		Int("predictions_length", len(rec.Predictions)).
		Int("recommendations_length", len(rec.Recommendations)).
		Int("raw_metrics_length", len(rec.RawMetrics)).
		Msg("Converting record for frontend")

	// Parse executive_summary JSON string
	var executiveSummary interface{}
	if err := parseJSONField(rec.ExecutiveSummary, &executiveSummary); err != nil {
		log.Warn().Err(err).Str("raw", rec.ExecutiveSummary[:min(100, len(rec.ExecutiveSummary))]).Msg("Failed to parse executive_summary")
		result["executive_summary"] = map[string]interface{}{}
	} else {
		result["executive_summary"] = executiveSummary
	}

	// Parse predictions JSON string
	var predictions interface{}
	if err := parseJSONField(rec.Predictions, &predictions); err != nil {
		log.Warn().Err(err).Str("raw", rec.Predictions[:min(100, len(rec.Predictions))]).Msg("Failed to parse predictions")
		result["predictions"] = map[string]interface{}{
			"short_term":  []interface{}{},
			"medium_term": []interface{}{},
			"long_term":   []interface{}{},
		}
	} else {
		log.Debug().Interface("predictions", predictions).Msg("Predictions parsed successfully")
		result["predictions"] = predictions
	}

	// Parse recommendations JSON string
	var recommendations interface{}
	if err := parseJSONField(rec.Recommendations, &recommendations); err != nil {
		log.Warn().Err(err).Str("raw", rec.Recommendations[:min(100, len(rec.Recommendations))]).Msg("Failed to parse recommendations")
		result["recommendations"] = []interface{}{}
	} else {
		log.Debug().Interface("recommendations", recommendations).Msg("Recommendations parsed successfully")
		result["recommendations"] = recommendations
	}

	// Parse raw_metrics JSON string
	var rawMetrics interface{}
	if err := parseJSONField(rec.RawMetrics, &rawMetrics); err != nil {
		log.Warn().Err(err).Str("raw", rec.RawMetrics[:min(100, len(rec.RawMetrics))]).Msg("Failed to parse raw_metrics")
		result["raw_metrics"] = map[string]interface{}{}
	} else {
		result["raw_metrics"] = rawMetrics
	}

	return result
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseJSONField helper para fazer parse de um campo JSON string
func parseJSONField(jsonStr string, target interface{}) error {
	if jsonStr == "" {
		return fmt.Errorf("empty JSON string")
	}
	return json.Unmarshal([]byte(jsonStr), target)
}
