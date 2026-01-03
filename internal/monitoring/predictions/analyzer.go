package predictions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/prometheus"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Analyzer é o orquestrador principal da análise preditiva
type Analyzer struct {
	collector  *MetricsCollector
	aiProvider ai.Provider
	kubeClient *kubernetes.Client
}

// NewAnalyzer cria novo Analyzer
func NewAnalyzer(
	promClient *prometheus.Client,
	aiProvider ai.Provider,
	kubeClient *kubernetes.Client,
) *Analyzer {
	return &Analyzer{
		collector:  NewMetricsCollector(promClient, kubeClient),
		aiProvider: aiProvider,
		kubeClient: kubeClient,
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
		// Usar análise de fallback (baseada em regras)
		aiAnalysis = a.fallbackAnalysis(metrics)
	}

	// 4. Extrair análises da resposta da IA
	result.Predictions = aiAnalysis.Predictions
	result.RootCauseAnalysis = aiAnalysis.RootCause
	result.ImpactAnalysis = aiAnalysis.Impact
	result.ExecutiveSummary = aiAnalysis.ExecutiveSummary
	result.Recommendations = aiAnalysis.Recommendations

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
	restartDelta := metrics.Current.RestartCount - metrics.Week7Ago.RestartCount
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
	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")

	return fmt.Sprintf(`Você é um especialista em análise preditiva de deployments Kubernetes.

Analise as métricas abaixo e forneça uma análise preditiva completa em formato JSON.

# MÉTRICAS COLETADAS:
%s

# RESPOSTA ESPERADA (JSON):

Retorne um JSON seguindo esta estrutura:

{
  "predictions": {
    "short_term": [
      {
        "timeframe": "4h",
        "timestamp": "2026-01-02T18:00:00Z",
        "event": "CPU usage will reach 85%% of capacity",
        "probability": 0.75,
        "severity": "medium",
        "impact": "Response times may increase by 20-30%%",
        "indicators": ["CPU trend increasing 15%% last 7 days", "Memory pressure detected"]
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
- Seja específico com números e percentuais
- Base as previsões nas tendências observadas
- Considere o contexto de nodes e capacidade do cluster
- Priorize ações de maior impacto
- Use probabilidades realistas (0.0 a 1.0)
- Retorne APENAS o JSON, sem texto adicional`, string(metricsJSON))
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
