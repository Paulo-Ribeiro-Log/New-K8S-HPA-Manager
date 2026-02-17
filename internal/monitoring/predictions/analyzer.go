package predictions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/aierrors"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/prometheus"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Analyzer é o orquestrador principal da análise preditiva
type Analyzer struct {
	collector    *MetricsCollector
	aiProvider   ai.Provider
	kubeClient   *kubernetes.Client
	costAnalyzer *CostAnalyzer
}

// NewAnalyzer cria novo Analyzer
func NewAnalyzer(
	promClient *prometheus.Client,
	aiProvider ai.Provider,
	kubeClient *kubernetes.Client,
) *Analyzer {
	return &Analyzer{
		collector:    NewMetricsCollector(promClient, kubeClient),
		aiProvider:   aiProvider,
		kubeClient:   kubeClient,
		costAnalyzer: NewCostAnalyzer(),
	}
}

// Analyze executa análise preditiva completa
func (a *Analyzer) Analyze(ctx context.Context, req PredictionRequest) (*PredictionResult, error) {
	startTime := time.Now()
	requestID := uuid.New().String()

	log.Info().
		Str("request_id", requestID).
		Str("cluster", req.Cluster).
		Str("namespace", req.Namespace).
		Str("deployment", req.Deployment).
		Msg("Starting predictive analysis")

	result := &PredictionResult{
		RequestID:  requestID,
		Cluster:    req.Cluster,
		Namespace:  req.Namespace,
		Deployment: req.Deployment,
		AnalyzedAt: time.Now(),
	}

	// 1. Coletar métricas
	metrics, err := a.collector.CollectMetrics(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to collect metrics: %w", err)
	}
	result.RawMetrics = *metrics

	// 2. Calcular Health Score (baseado em métricas)
	result.HealthScore = a.calculateHealthScore(metrics)

	// 3. Enviar para IA para análise aprofundada
	aiAnalysis, err := a.performAIAnalysis(ctx, req, metrics)
	if err != nil {
		log.Warn().Err(err).Msg("AI analysis failed, using fallback")
		
		// Registrar erro globalmente para exibir no painel AI Provider Status
		if a.aiProvider != nil {
			aierrors.RecordGlobalAIError(req.UserEmail, a.aiProvider.GetName(), a.aiProvider.GetModel(), err)
			// Incrementar contador de chamadas (falha)
			aierrors.IncrementAICall(req.UserEmail, false)
		}
		
		// Usar análise de fallback (baseada em regras)
		aiAnalysis = a.fallbackAnalysis(metrics)
	} else {
		// Limpar erro em caso de sucesso
		if a.aiProvider != nil {
			aierrors.RecordGlobalAIError(req.UserEmail, a.aiProvider.GetName(), a.aiProvider.GetModel(), nil)
			// Incrementar contador de chamadas (sucesso)
			aierrors.IncrementAICall(req.UserEmail, true)
		}
	}

	// 4. Extrair análises da resposta da IA
	result.Predictions = aiAnalysis.Predictions
	result.RootCauseAnalysis = aiAnalysis.RootCause
	result.ImpactAnalysis = aiAnalysis.Impact
	result.ExecutiveSummary = aiAnalysis.ExecutiveSummary
	result.Recommendations = aiAnalysis.Recommendations

	// 4.1. Enriquecer predictions com timestamps calculados (baseado no timestamp das métricas atuais)
	a.enrichPredictionsWithTimestamps(&result.Predictions, metrics.Current.Timestamp)

	// 4.2. Enriquecer predictions com confidence percent
	a.enrichPredictionsWithConfidence(&result.Predictions, metrics)

	// 4.3. Calcular Action Summary (resumo para decisão rápida)
	result.ActionSummary = a.calculateActionSummary(result, metrics)

	// 4.4. Calcular Cost Analysis
	result.CostAnalysis = a.costAnalyzer.Calculate(metrics)

	// 5. Calcular duração
	result.DurationMs = time.Since(startTime).Milliseconds()

	log.Info().
		Str("request_id", requestID).
		Int64("duration_ms", result.DurationMs).
		Int("health_score", result.HealthScore.Overall).
		Msg("Predictive analysis completed")

	return result, nil
}

// calculateHealthScore calcula score de saúde baseado em métricas
func (a *Analyzer) calculateHealthScore(metrics *DeploymentMetrics) HealthScore {
	score := ScoreBreakdown{}

	// Availability (baseado em réplicas)
	if metrics.AvailableReplicas == metrics.DesiredReplicas && metrics.ReadyReplicas == metrics.DesiredReplicas {
		score.Availability = 100
	} else if metrics.AvailableReplicas > 0 {
		ratio := float64(metrics.AvailableReplicas) / float64(metrics.DesiredReplicas)
		score.Availability = int(ratio * 100)
	} else {
		score.Availability = 0
	}

	// Performance (baseado em CPU/Memory usage vs limits)
	cpuHealthy := metrics.Current.CPUUsageP95 < metrics.Current.CPUUsageAvg*1.5
	memHealthy := metrics.Current.MemoryUsageP95 < metrics.Current.MemoryUsageAvg*1.3
	if cpuHealthy && memHealthy {
		score.Performance = 100
	} else if cpuHealthy || memHealthy {
		score.Performance = 70
	} else {
		score.Performance = 40
	}

	// Stability (baseado em restarts e error rate)
	restartDelta := metrics.Current.RestartCount - metrics.Day7Ago.RestartCount
	if restartDelta == 0 && metrics.Current.ErrorRate < 1.0 {
		score.Stability = 100
	} else if restartDelta < 3 && metrics.Current.ErrorRate < 5.0 {
		score.Stability = 80
	} else if restartDelta < 10 {
		score.Stability = 50
	} else {
		score.Stability = 20
	}

	// Efficiency (baseado em tendências de recursos)
	cpuTrendGood := metrics.Trends.CPUTrend == TrendStable || metrics.Trends.CPUTrend == TrendDown
	memTrendGood := metrics.Trends.MemoryTrend == TrendStable || metrics.Trends.MemoryTrend == TrendDown
	if cpuTrendGood && memTrendGood {
		score.Efficiency = 100
	} else if cpuTrendGood || memTrendGood {
		score.Efficiency = 70
	} else {
		score.Efficiency = 40
	}

	// Overall (média ponderada)
	overall := (score.Availability*40 + score.Performance*25 + score.Stability*20 + score.Efficiency*15) / 100

	category := "healthy"
	if overall < 50 {
		category = "critical"
	} else if overall < 75 {
		category = "warning"
	}

	return HealthScore{
		Overall:     overall,
		Category:    category,
		Breakdown:   score,
		LastUpdated: time.Now(),
	}
}

// performAIAnalysis envia dados para IA e processa resposta
func (a *Analyzer) performAIAnalysis(ctx context.Context, req PredictionRequest, metrics *DeploymentMetrics) (*AIAnalysisResult, error) {
	// Construir prompt estruturado
	prompt := a.buildAIPrompt(metrics)

	// Chamar IA
	response, err := a.aiProvider.Analyze(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI completion failed: %w", err)
	}

	// Parsear resposta JSON da IA
	var aiResult AIAnalysisResult
	if err := json.Unmarshal([]byte(response), &aiResult); err != nil {
		// Se falhar parsing, tentar extrair JSON do response
		jsonStr := extractJSON(response)
		if err := json.Unmarshal([]byte(jsonStr), &aiResult); err != nil {
			return nil, fmt.Errorf("failed to parse AI response: %w", err)
		}
	}

	return &aiResult, nil
}

// buildAIPrompt constrói prompt estruturado para IA
func (a *Analyzer) buildAIPrompt(metrics *DeploymentMetrics) string {
	// Remover pod_logs do JSON de métricas para evitar duplicação (logs têm seção própria)
	metricsWithoutLogs := *metrics
	metricsWithoutLogs.PodLogs = nil
	metricsJSON, _ := json.MarshalIndent(metricsWithoutLogs, "", "  ")

	// Construir contexto temporal para análise preditiva verdadeira
	temporalContext := a.buildTemporalContext(metrics)

	// Construir seção de logs dos pods (se existir)
	podLogsSection := a.buildPodLogsSection(metrics)

	// Construir seção de sazonalidade (se dados disponíveis)
	seasonalSection := a.buildSeasonalSection(metrics)

	return fmt.Sprintf(`Você é um especialista em análise preditiva de deployments Kubernetes.

Analise as métricas abaixo e forneça uma análise preditiva completa em formato JSON.

**IMPORTANTE: Toda a análise DEVE ser escrita em PORTUGUÊS BRASILEIRO (PT-BR). Todos os textos, descrições, recomendações e mensagens devem estar em português.**

%s
%s
%s
# MÉTRICAS COLETADAS:
%s

# RESPOSTA ESPERADA (JSON):

Retorne um JSON seguindo esta estrutura:

{
  "predictions": {
    "short_term": [
      {
        "timeframe": "4h",
        "event": "Uso de CPU atingirá 85%% da capacidade",
        "probability": 0.75,
        "severity": "medium",
        "impact": "Tempos de resposta podem aumentar em 20-30%%",
        "indicators": ["Tendência de CPU aumentando 15%% nos últimos 7 dias", "Pressão de memória detectada"]
      }
    ],
    "medium_term": [],
    "long_term": []
  },
  "root_cause": {
    "identified_causes": [
      {
        "cause": "Insufficient CPU allocation for current load",
        "evidence": ["CPU usage at 80%% of limits", "P95 latency increased 40%% in 7 days"],
        "certainty": 0.85,
        "category": "resource",
        "remediation": "Increase CPU limits to 2 cores or add 2 replicas"
      }
    ],
    "primary_factor": "Resource constraints",
    "contributing_factors": ["Growing traffic", "Inefficient queries"]
  },
  "impact": {
    "if_no_action": {
      "user_impact": "Users will experience 500-1000ms latency, 5%% error rate",
      "infrastructure_impact": "Pods will OOMKill, cascading failures possible",
      "timeline_description": "Critical in 3-7 days",
      "risks": ["Service degradation", "Potential outages"]
    },
    "if_optimizations_applied": {
      "user_impact": "Latency reduced to <200ms, error rate <0.5%%",
      "infrastructure_impact": "Stable resource usage at 60-70%% capacity",
      "timeline_description": "Improvements in 1-2 hours after deployment",
      "benefits": ["Better performance", "Higher reliability"]
    },
    "recommended_action_priority": "urgent",
    "timeline_to_action": "Within 48 hours"
  },
  "executive_summary": {
    "current_state": "Deployment is under resource pressure with increasing CPU usage",
    "key_findings": [
      "CPU usage increased 15%% in 7 days",
      "Latency degraded 40%%",
      "Capacity will be exhausted in 3-7 days"
    ],
    "risk_level": "high",
    "action_required": true,
    "expected_outcome": "With optimizations, performance improves by 50%% and stability increases",
    "business_impact": "Without action, user experience degrades, potential revenue impact"
  },
  "recommendations": [
    {
      "priority": 1,
      "title": "Increase replica count",
      "description": "Add 2 more replicas to distribute load",
      "category": "scaling",
      "actions": ["Update deployment spec", "Apply with kubectl"],
      "expected_impact": "Reduces CPU per pod by 40%%",
      "implementation_estimate": {
        "time_required": "5 minutes",
        "complexity": "low",
        "risk_level": "low",
        "requires_downtime": false,
        "resource_efficiency_gain_percent": 35.0
      }
    }
  ]
}

IMPORTANTE:
- **ESCREVA TUDO EM PORTUGUÊS BRASILEIRO (PT-BR)**
- **NAO USE EMOJIS OU ICONES** - apenas texto puro (sem símbolos Unicode como ⚠️, ✅, ❌, 💰, 🚀, etc)
- Seja específico com números e percentuais
- Base as previsões nas tendências observadas
- Considere o contexto de nodes e capacidade do cluster
- Priorize ações de maior impacto
- Use probabilidades realistas (0.0 a 1.0)
- Use terminologia técnica em português (ex: "réplicas" ao invés de "replicas", "uso de CPU" ao invés de "CPU usage")
- Retorne APENAS o JSON, sem texto adicional
- Todos os campos de texto devem estar em português brasileiro
- **NÃO INCLUA o campo "timestamp" nas previsões** - apenas "timeframe" (ex: "4h", "24h", "7d")

## ANÁLISE DE ECONOMIA DE CUSTOS (DOWNSIZING):

**ATENÇÃO ESPECIAL**: Analise se há DESPERDÍCIO DE RECURSOS e oportunidades de REDUÇÃO DE CUSTOS:

1. **SOBREPROVISIONAMENTO**: Se o uso de CPU está consistentemente abaixo de 30%% dos requests/limits ou memória abaixo de 40%%, há sobreprovisionamento
2. **CUSTOS DESNECESSÁRIOS**: Recursos alocados mas não utilizados geram custos sem benefício
3. **DOWNSIZING**: Quando identificar sobreprovisionamento:
   - Recomende redução de CPU/memória requests e limits
   - Calcule economia estimada em percentual (resource_efficiency_gain_percent)
   - Explique impacto positivo na redução de custos
   - Prioridade ALTA se economia > 30%%
4. **RIGHTSIZING**: Ajuste recursos para o uso real + margem de segurança (20-30%%)
5. **ECONOMIA ESTIMADA**: 
   - Se CPU usage < 30%% do limit: "Economia de até X%% nos custos de computação"
   - Se memória usage < 40%% do limit: "Economia de até Y%% nos custos de memória"
   - Considere custo de VMs compartilhadas (competing_apps)

**CATEGORIAS DE RECOMENDAÇÕES**:
- "cost-optimization" ou "downsizing": Para redução de recursos e custos
- "scaling": Para aumento de réplicas ou recursos
- "performance": Para otimizações de performance
- "reliability": Para melhorias de estabilidade

**EXEMPLO DE RECOMENDAÇÃO DE DOWNSIZING**:
{
  "priority": 1,
  "title": "Reduzir alocação de CPU - Sobreprovisionamento detectado",
  "description": "O deployment está usando apenas 15%% da CPU alocada (0.3 de 2 cores). Recursos ociosos geram custos desnecessários de aproximadamente R$ XXX/mês por réplica.",
  "category": "cost-optimization",
  "actions": [
    "Reduzir CPU requests de 2 cores para 0.5 cores",
    "Reduzir CPU limits de 4 cores para 1 core",
    "Monitorar por 48h após ajuste"
  ],
  "expected_impact": "Economia de 75%% nos custos de CPU sem impacto em performance. Redução de ~R$ XXX/mês no custo total.",
  "implementation_estimate": {
    "time_required": "10 minutos",
    "complexity": "low",
    "risk_level": "low",
    "requires_downtime": false,
    "resource_efficiency_gain_percent": 75.0
  }
}`, temporalContext, podLogsSection, seasonalSection, string(metricsJSON))
}

// fallbackAnalysis análise de fallback quando IA falha
func (a *Analyzer) fallbackAnalysis(metrics *DeploymentMetrics) *AIAnalysisResult {
	result := &AIAnalysisResult{
		Predictions: PredictionsAnalysis{
			ShortTerm:  []Prediction{},
			MediumTerm: []Prediction{},
			LongTerm:   []Prediction{},
		},
		RootCause: RootCauseAnalysis{
			IdentifiedCauses: []RootCause{
				{
					Cause:       "Automated analysis not available",
					Evidence:    []string{"AI provider unavailable"},
					Certainty:   0.5,
					Category:    "external",
					Remediation: "Review metrics manually",
				},
			},
			PrimaryFactor:       "Unknown",
			ContributingFactors: []string{},
		},
		Impact: ImpactAnalysis{
			IfNoAction: ImpactScenario{
				UserImpact:           "Unable to predict without AI analysis",
				InfrastructureImpact: "Monitor manually",
				TimelineDescription:  "Unknown",
				Risks:                []string{"Analysis incomplete"},
			},
			IfOptimizationsApplied: ImpactScenario{
				UserImpact:           "Benefits uncertain",
				InfrastructureImpact: "Requires manual assessment",
				TimelineDescription:  "Unknown",
				Benefits:             []string{},
			},
			RecommendedActionPriority: "moderate",
			TimelineToAction:          "Review within 24 hours",
		},
		ExecutiveSummary: ExecutiveSummary{
			CurrentState:    "Deployment metrics collected, AI analysis unavailable",
			KeyFindings:     []string{"Manual review recommended"},
			RiskLevel:       "unknown",
			ActionRequired:  false,
			ExpectedOutcome: "N/A",
			BusinessImpact:  "Unable to assess without complete analysis",
		},
		Recommendations: []Recommendation{
			{
				Priority:       3,
				Title:          "Review metrics manually",
				Description:    "AI analysis unavailable, review collected metrics",
				Category:       "monitoring",
				Actions:        []string{"Check Prometheus dashboards", "Review pod logs"},
				ExpectedImpact: "Identify issues manually",
				ImplementationEstimate: ImplementationEstimate{
					TimeRequired:           "30 minutes",
					Complexity:             "medium",
					RiskLevel:              "low",
					RequiresDowntime:       false,
					ResourceEfficiencyGain: 0,
				},
			},
		},
	}

	return result
}

// buildSeasonalSection constrói seção de contexto sazonal para o prompt da IA
func (a *Analyzer) buildSeasonalSection(metrics *DeploymentMetrics) string {
	sp := metrics.SeasonalPatterns
	if !sp.HasSufficientData {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# ANALISE DE SAZONALIDADE\n\n")

	// Alerta principal quando tendência de crescimento é na verdade sazonalidade
	if sp.IsTrendSeasonal {
		sb.WriteString("[ATENCAO - SAZONALIDADE DETECTADA]\n")
		sb.WriteString("O aumento de CPU detectado COINCIDE com o horario de pico tipico desta aplicacao.\n")
		sb.WriteString("IMPORTANTE: Antes de recomendar scaling permanente, verifique se o aumento e sazonalidade:\n")
		sb.WriteString("- Se o aumento ocorre somente nos horarios de pico e retorna ao normal depois, e SAZONALIDADE\n")
		sb.WriteString("- Sazonalidade NAO requer novos nos, apenas HPA bem configurado para absorver os picos\n")
		sb.WriteString("- Recomende HPA com minReplicas = piso do vale e maxReplicas = pico esperado\n\n")
	}

	// Padrão horário
	sb.WriteString("## Padrao Horario (media de CPU por hora do dia):\n")
	if len(sp.Hourly.PeakHours) > 0 {
		peakHoursStr := make([]string, len(sp.Hourly.PeakHours))
		for i, h := range sp.Hourly.PeakHours {
			peakHoursStr[i] = fmt.Sprintf("%02dh", h)
		}
		sb.WriteString(fmt.Sprintf("- Horas de pico (>120%% da media): %s\n", strings.Join(peakHoursStr, ", ")))
	}
	if len(sp.Hourly.LowHours) > 0 {
		lowHoursStr := make([]string, len(sp.Hourly.LowHours))
		for i, h := range sp.Hourly.LowHours {
			lowHoursStr[i] = fmt.Sprintf("%02dh", h)
		}
		sb.WriteString(fmt.Sprintf("- Horas de baixo uso (<80%% da media): %s\n", strings.Join(lowHoursStr, ", ")))
	}
	if sp.Hourly.PeakMultiplier > 0 {
		sb.WriteString(fmt.Sprintf("- Multiplicador de pico: %.1fx (pico e %.0f%% maior que o vale)\n",
			sp.Hourly.PeakMultiplier, (sp.Hourly.PeakMultiplier-1)*100))
	}

	// Padrão semanal
	sb.WriteString("\n## Padrao Semanal (media de CPU por dia da semana):\n")
	dayNames := []string{"Domingo", "Segunda", "Terca", "Quarta", "Quinta", "Sexta", "Sabado"}
	if len(sp.Weekly.HighDays) > 0 {
		sb.WriteString(fmt.Sprintf("- Dias de alto uso: %s\n", strings.Join(sp.Weekly.HighDays, ", ")))
	}
	if len(sp.Weekly.LowDays) > 0 {
		sb.WriteString(fmt.Sprintf("- Dias de baixo uso: %s\n", strings.Join(sp.Weekly.LowDays, ", ")))
	}
	if sp.Weekly.WeekendReduction > 0 {
		sb.WriteString(fmt.Sprintf("- Reducao de fim de semana: %.0f%% menos uso que dias uteis\n", sp.Weekly.WeekendReduction))
	}
	_ = dayNames // usado implicitamente pelos nomes já preenchidos nas structs

	// Tendência ajustada
	if sp.SeasonalAdjustedTrend != "" && sp.SeasonalAdjustedTrend != string(metrics.Trends.CPUTrend) {
		sb.WriteString(fmt.Sprintf("\n## Tendencia Ajustada pela Sazonalidade:\n"))
		sb.WriteString(fmt.Sprintf("- Tendencia bruta detectada: %s\n", metrics.Trends.CPUTrend))
		sb.WriteString(fmt.Sprintf("- Tendencia ajustada (considerando sazonalidade): %s\n", sp.SeasonalAdjustedTrend))
		sb.WriteString("- INTERPRETACAO: O crescimento aparente pode ser um pico sazonal esperado, nao crescimento real\n")
	}

	sb.WriteString("\n## Instrucoes para a IA:\n")
	sb.WriteString("1. Considere a sazonalidade ao fazer previsoes - picos esperados nao indicam problema\n")
	sb.WriteString("2. Se a tendencia e sazonal, recomende configuracao de HPA ao inves de scaling permanente\n")
	sb.WriteString("3. Mencione nos short_term predictions se um pico esta previsto para as proximas horas\n")
	sb.WriteString("4. Diferencie entre crescimento real (preocupante) e variacao sazonal (esperada)\n\n")

	return sb.String()
}

// buildTemporalContext constrói contexto temporal para análise preditiva verdadeira
func (a *Analyzer) buildTemporalContext(metrics *DeploymentMetrics) string {
	var context strings.Builder

	context.WriteString("# CONTEXTO TEMPORAL - ANALISE PREDITIVA VERDADEIRA\n\n")

	// Idade do deployment
	if metrics.IsNew {
		context.WriteString(fmt.Sprintf(`[ATENCAO] **DEPLOYMENT NOVO - HISTORICO LIMITADO**
- **Idade**: %d dias (criado em %s)
- **Status**: Deployment recente - menos de 7 dias de histórico
- **Impacto na Análise**:
  - Padrões de uso ainda não estabelecidos
  - Tendências podem não ser representativas
  - Previsões devem ser feitas com CAUTELA e menor confiança
  - Recomende monitoramento intensivo nas primeiras 2 semanas
  - Evite previsões de longo prazo (>7d) - dados insuficientes

`, metrics.AgeInDays, metrics.CreationTimestamp.Format("02/01/2006")))
	} else if !metrics.HasSufficientHistory {
		context.WriteString(fmt.Sprintf(`[ATENCAO] **DEPLOYMENT RECENTE - HISTORICO PARCIAL**
- **Idade**: %d dias (criado em %s)
- **Status**: Entre 7-14 dias - histórico em formação
- **Impacto na Análise**:
  - Alguns padrões começam a aparecer
  - Tendências devem ser validadas com cautela
  - Previsões de médio prazo (24h) são confiáveis
  - Previsões de longo prazo (7d) devem ser marcadas como "preliminares"

`, metrics.AgeInDays, metrics.CreationTimestamp.Format("02/01/2006")))
	} else {
		context.WriteString(fmt.Sprintf(`[OK] **DEPLOYMENT MADURO - HISTORICO CONFIAVEL**
- **Idade**: %d dias (criado em %s)
- **Status**: Mais de 14 dias de histórico - padrões estabelecidos
- **Impacto na Análise**:
  - Tendências são representativas do comportamento real
  - Previsões de curto, médio e longo prazo são confiáveis
  - Dados históricos permitem análise preditiva profunda
  - Sazonalidades podem ser identificadas

`, metrics.AgeInDays, metrics.CreationTimestamp.Format("02/01/2006")))
	}

	// Predecessores
	context.WriteString("## 🔄 Histórico de Deployments\n\n")
	if strings.Contains(metrics.PredecessorInfo, "Nenhum predecessor") {
		context.WriteString(fmt.Sprintf(`- **Predecessor**: %s
- **Interpretação**: Este é o primeiro deployment deste tipo ou não há histórico anterior detectado
- **Recomendação**: Compare com benchmarks da indústria ou deployments similares em outros namespaces

`, metrics.PredecessorInfo))
	} else {
		context.WriteString(fmt.Sprintf(`- **Predecessor**: %s
- **Interpretação**: Este deployment substitui uma versão anterior - possível migração/upgrade
- **Recomendação**:
  - Se houver degradação de performance vs predecessor, investigue regressões
  - Se houver melhoria, documente otimizações aplicadas
  - Considere que padrões de uso podem mudar após migração

`, metrics.PredecessorInfo))
	}

	context.WriteString("\n---\n\n")
	context.WriteString("**INSTRUÇÕES DE ANÁLISE BASEADAS NO CONTEXTO TEMPORAL:**\n\n")

	if metrics.IsNew {
		context.WriteString(`1. **Confiança das Previsões**: Marque todas as previsões como "baixa confiança" ou "preliminar"
2. **Recomendações Priorizadas**:
   - Prioridade 1: Configurar alertas de monitoramento intensivo
   - Prioridade 2: Estabelecer baselines de performance
   - Evite recomendações de rightsizing agressivo (aguardar 14 dias)
3. **Timeframes Sugeridos**: Foque em previsões de curto prazo (4h) e médio prazo (24h) apenas
4. **Mensagem de Aviso**: Inclua no executive summary que análise é preliminar devido à idade do deployment

`)
	} else if !metrics.HasSufficientHistory {
		context.WriteString(`1. **Confiança das Previsões**: Marque previsões curto/médio prazo como "moderada", longo prazo como "preliminar"
2. **Recomendações Priorizadas**: Validar tendências observadas com monitoramento contínuo
3. **Timeframes Sugeridos**: Curto prazo (4h) e médio prazo (24h) são confiáveis, longo prazo (7d) com ressalvas

`)
	} else {
		context.WriteString(`1. **Confiança das Previsões**: Alta confiança em todos os timeframes (4h, 24h, 7d)
2. **Recomendações Priorizadas**: Análise preditiva completa - rightsizing, scaling, otimizações
3. **Timeframes Sugeridos**: Utilize todos os timeframes disponíveis com confiança

`)
	}

	return context.String()
}

// AIAnalysisResult estrutura da resposta da IA
type AIAnalysisResult struct {
	Predictions      PredictionsAnalysis `json:"predictions"`
	RootCause        RootCauseAnalysis   `json:"root_cause"`
	Impact           ImpactAnalysis      `json:"impact"`
	ExecutiveSummary ExecutiveSummary    `json:"executive_summary"`
	Recommendations  []Recommendation    `json:"recommendations"`
}

// extractJSON tenta extrair JSON de uma resposta de texto
func extractJSON(text string) string {
	// Procurar por { ... } no texto
	start := -1
	braceCount := 0

	for i, ch := range text {
		if ch == '{' {
			if start == -1 {
				start = i
			}
			braceCount++
		} else if ch == '}' {
			braceCount--
			if braceCount == 0 && start != -1 {
				return text[start : i+1]
			}
		}
	}

	return text
}

// buildPodLogsSection constrói seção de logs dos pods para o prompt da IA
func (a *Analyzer) buildPodLogsSection(metrics *DeploymentMetrics) string {
	if len(metrics.PodLogs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# LOGS DOS PODS (coletados e sanitizados automaticamente):\n\n")
	sb.WriteString("IMPORTANTE: Analise os logs abaixo para identificar erros, exceções e causas raiz.\n")
	sb.WriteString("Estes logs foram coletados AGORA dos pods em execução. Use-os na análise de causa raiz.\n\n")

	for _, entry := range metrics.PodLogs {
		sb.WriteString(fmt.Sprintf("## Pod: %s | Container: %s | Restarts: %d\n",
			entry.PodName, entry.ContainerName, entry.RestartCount))

		if entry.LogLines != "" {
			sb.WriteString("### Logs atuais (últimas 80 linhas):\n```\n")
			sb.WriteString(entry.LogLines)
			sb.WriteString("\n```\n\n")
		}

		if entry.PreviousLogs != "" {
			sb.WriteString("### Logs ANTES do ultimo restart (crash anterior):\n```\n")
			sb.WriteString(entry.PreviousLogs)
			sb.WriteString("\n```\n\n")
		}
	}

	return sb.String()
}

// enrichPredictionsWithTimestamps adiciona timestamps às predictions baseado no timestamp atual das métricas
func (a *Analyzer) enrichPredictionsWithTimestamps(predictions *PredictionsAnalysis, baseTimestamp time.Time) {
	// Helper para calcular timestamp baseado em timeframe
	calculateTimestamp := func(timeframe string) *time.Time {
		var duration time.Duration

		switch timeframe {
		case "4h", "próximas 4 horas", "curto prazo":
			duration = 4 * time.Hour
		case "24h", "próximas 24 horas", "médio prazo", "medio prazo":
			duration = 24 * time.Hour
		case "7d", "próximos 7 dias", "longo prazo":
			duration = 7 * 24 * time.Hour
		default:
			// Tentar parsear tempo do formato "Xh" ou "Xd"
			if len(timeframe) > 0 {
				last := timeframe[len(timeframe)-1]
				if last == 'h' || last == 'H' {
					// Formato "4h"
					hours := 0
					fmt.Sscanf(timeframe, "%dh", &hours)
					duration = time.Duration(hours) * time.Hour
				} else if last == 'd' || last == 'D' {
					// Formato "7d"
					days := 0
					fmt.Sscanf(timeframe, "%dd", &days)
					duration = time.Duration(days) * 24 * time.Hour
				}
			}
		}

		if duration == 0 {
			// Fallback: retornar nil se não conseguiu parsear
			return nil
		}

		timestamp := baseTimestamp.Add(duration)
		return &timestamp
	}

	// Enriquecer short_term
	for i := range predictions.ShortTerm {
		predictions.ShortTerm[i].Timestamp = calculateTimestamp(predictions.ShortTerm[i].Timeframe)
	}

	// Enriquecer medium_term
	for i := range predictions.MediumTerm {
		predictions.MediumTerm[i].Timestamp = calculateTimestamp(predictions.MediumTerm[i].Timeframe)
	}

	// Enriquecer long_term
	for i := range predictions.LongTerm {
		predictions.LongTerm[i].Timestamp = calculateTimestamp(predictions.LongTerm[i].Timeframe)
	}
}

// enrichPredictionsWithConfidence calcula e adiciona confidence percent para cada prediction
func (a *Analyzer) enrichPredictionsWithConfidence(predictions *PredictionsAnalysis, metrics *DeploymentMetrics) {
	// Fatores que afetam a confiança:
	// 1. Quantidade de dados históricos (deployment novo = menos confiança)
	// 2. Variabilidade das métricas (alta variância = menos confiança)
	// 3. Probabilidade da previsão (probabilidade baixa = menos confiança)

	baseConfidence := 80.0

	// Reduz confiança se deployment é novo (<7 dias)
	if metrics.IsNew {
		baseConfidence -= 25.0
	} else if !metrics.HasSufficientHistory {
		baseConfidence -= 15.0
	}

	// Reduz confiança se métricas são muito voláteis
	if metrics.Trends.CPUTrend == TrendVolatile {
		baseConfidence -= 10.0
	}
	if metrics.Trends.MemoryTrend == TrendVolatile {
		baseConfidence -= 10.0
	}

	// Função helper para calcular confidence de uma prediction
	calculateConfidence := func(p *Prediction) float64 {
		conf := baseConfidence

		// Ajusta pela probabilidade da previsão
		// Previsões com probabilidade muito baixa (<0.3) ou muito alta (>0.9) são menos confiáveis
		if p.Probability < 0.3 {
			conf -= 15.0
		} else if p.Probability > 0.9 {
			conf -= 5.0 // Muito certeza também pode ser overconfidence
		}

		// Previsões de longo prazo são menos confiáveis
		if strings.Contains(p.Timeframe, "7d") || strings.Contains(p.Timeframe, "d") {
			conf -= 10.0
		}

		// Limitar entre 10% e 95%
		if conf < 10.0 {
			conf = 10.0
		}
		if conf > 95.0 {
			conf = 95.0
		}

		return conf
	}

	// Aplicar a todas as predictions
	for i := range predictions.ShortTerm {
		predictions.ShortTerm[i].ConfidencePercent = calculateConfidence(&predictions.ShortTerm[i])
	}
	for i := range predictions.MediumTerm {
		predictions.MediumTerm[i].ConfidencePercent = calculateConfidence(&predictions.MediumTerm[i])
	}
	for i := range predictions.LongTerm {
		predictions.LongTerm[i].ConfidencePercent = calculateConfidence(&predictions.LongTerm[i])
	}
}

// calculateActionSummary gera resumo de ações para decisão rápida
func (a *Analyzer) calculateActionSummary(result *PredictionResult, metrics *DeploymentMetrics) ActionSummary {
	summary := ActionSummary{
		NextReviewDays: 7, // Padrão: revisar em 7 dias
	}

	// 1. Determinar status baseado no health score
	healthScore := result.HealthScore.Overall
	switch {
	case healthScore >= 75:
		summary.Status = "healthy"
		summary.StatusColor = "green"
		summary.StatusMessage = "Operacional"
		summary.NextReviewDays = 14
	case healthScore >= 50:
		summary.Status = "attention"
		summary.StatusColor = "yellow"
		summary.StatusMessage = "Requer atenção"
		summary.NextReviewDays = 7
	default:
		summary.Status = "critical"
		summary.StatusColor = "red"
		summary.StatusMessage = "Crítico"
		summary.NextReviewDays = 1
	}

	// 2. Contar ações
	summary.TotalActions = len(result.Recommendations)
	for _, rec := range result.Recommendations {
		if rec.Priority <= 2 {
			summary.UrgentActions++
		}
	}

	// 3. Obter ação principal (maior prioridade)
	if len(result.Recommendations) > 0 {
		topRec := result.Recommendations[0]
		for _, rec := range result.Recommendations {
			if rec.Priority < topRec.Priority {
				topRec = rec
			}
		}
		summary.TopAction = topRec.Title
		if len(topRec.Actions) > 0 {
			summary.TopActionCommand = topRec.Actions[0]
		}
	}

	// 4. Calcular tempo até crítico (horas)
	summary.HoursToCritical, summary.CriticalMetric, summary.CriticalReason = a.calculateHoursToCritical(metrics, result)

	// Ajustar status se há risco iminente
	if summary.HoursToCritical != nil && *summary.HoursToCritical < 24 {
		summary.Status = "critical"
		summary.StatusColor = "red"
		summary.StatusMessage = "Risco iminente"
		summary.NextReviewDays = 0 // Ação imediata
	} else if summary.HoursToCritical != nil && *summary.HoursToCritical < 72 {
		if summary.Status == "healthy" {
			summary.Status = "attention"
			summary.StatusColor = "yellow"
			summary.StatusMessage = "Atenção preventiva"
		}
		summary.NextReviewDays = 1
	}

	// 5. Calcular confiança geral (média das predictions)
	var totalConfidence float64
	var count int
	for _, p := range result.Predictions.ShortTerm {
		totalConfidence += p.ConfidencePercent
		count++
	}
	for _, p := range result.Predictions.MediumTerm {
		totalConfidence += p.ConfidencePercent
		count++
	}
	for _, p := range result.Predictions.LongTerm {
		totalConfidence += p.ConfidencePercent
		count++
	}
	if count > 0 {
		summary.OverallConfidence = totalConfidence / float64(count)
	} else {
		// Se não há predictions, usar confiança baseada na qualidade dos dados
		if metrics.HasSufficientHistory {
			summary.OverallConfidence = 75.0
		} else if metrics.IsNew {
			summary.OverallConfidence = 40.0
		} else {
			summary.OverallConfidence = 60.0
		}
	}

	return summary
}

// calculateHoursToCritical calcula quantas horas até atingir estado crítico
func (a *Analyzer) calculateHoursToCritical(metrics *DeploymentMetrics, result *PredictionResult) (*int, string, string) {
	// Verificar CPU
	if metrics.Current.CPUUsageAvg > 0 {
		// Assumir que o usage atual representa uma fração do limit
		// Tendência de 7 dias para calcular projeção
		cpuChange7d := metrics.Trends.CPUChange7d
		if cpuChange7d > 0 {
			// CPU está aumentando
			// Estimar quando atingirá 90% (considerando tendência linear)
			// Se aumentou X% em 7 dias, quanto tempo para aumentar mais (90% - atual)%?
			currentUsage := metrics.Current.CPUUsageAvg
			if metrics.Day7Ago.CPUUsageAvg > 0 {
				dailyIncrease := (currentUsage - metrics.Day7Ago.CPUUsageAvg) / 7.0
				if dailyIncrease > 0 {
					// Estimando limite baseado no P95 atual (assumindo que é ~80% do limit)
					estimatedLimit := metrics.Current.CPUUsageP95 * 1.25
					remaining := estimatedLimit - currentUsage
					daysToReach := remaining / dailyIncrease
					if daysToReach > 0 && daysToReach < 30 {
						hours := int(daysToReach * 24)
						return &hours, "cpu", fmt.Sprintf("CPU atingirá limite em aproximadamente %d horas", hours)
					}
				}
			}
		}
	}

	// Verificar Memória
	memChange7d := metrics.Trends.MemoryChange7d
	if memChange7d > 5 { // Memória crescendo mais de 5% por semana
		currentMem := metrics.Current.MemoryUsageAvg
		if metrics.Day7Ago.MemoryUsageAvg > 0 && currentMem > 0 {
			dailyIncrease := (currentMem - metrics.Day7Ago.MemoryUsageAvg) / 7.0
			if dailyIncrease > 0 {
				estimatedLimit := metrics.Current.MemoryUsageP95 * 1.2
				remaining := estimatedLimit - currentMem
				daysToReach := remaining / dailyIncrease
				if daysToReach > 0 && daysToReach < 30 {
					hours := int(daysToReach * 24)
					return &hours, "memory", fmt.Sprintf("Memória atingirá limite em aproximadamente %d horas", hours)
				}
			}
		}
	}

	// Verificar se HPA está próximo do máximo
	if metrics.HPAConfig != nil && metrics.HPAConfig.Exists {
		if metrics.CurrentReplicas >= metrics.HPAConfig.MaxReplicas {
			hours := 0
			return &hours, "replicas", "HPA já está no máximo de réplicas"
		}
		// Verificar proximidade ao threshold de scaling
		if metrics.HPAConfig.CPUProximityToThreshold > 80 {
			hours := 4 // Estimativa conservadora
			return &hours, "hpa_threshold", fmt.Sprintf("CPU em %.0f%% do threshold de scaling", metrics.HPAConfig.CPUProximityToThreshold)
		}
	}

	// Verificar previsões críticas
	for _, p := range result.Predictions.ShortTerm {
		if p.Severity == "critical" && p.Probability > 0.7 {
			hours := 4
			return &hours, "prediction", p.Event
		}
	}

	return nil, "", ""
}
