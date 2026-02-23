package nodepoolpredictions

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/aierrors"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/prometheus"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// NodePoolAnalyzer orquestra a análise preditiva de node pool
type NodePoolAnalyzer struct {
	collector    *NodePoolCollector
	aiProvider   ai.Provider
	costAnalyzer *NodePoolCostAnalyzer
}

// NewNodePoolAnalyzer cria novo analyzer.
// promClient pode ser nil (degradação graceful).
func NewNodePoolAnalyzer(
	promClient *prometheus.Client,
	aiProvider ai.Provider,
	kubeClient *kubernetes.Client,
) *NodePoolAnalyzer {
	return &NodePoolAnalyzer{
		collector:    NewNodePoolCollector(promClient, kubeClient),
		aiProvider:   aiProvider,
		costAnalyzer: NewNodePoolCostAnalyzer(),
	}
}

// nodePoolAIAnalysisResult é a estrutura esperada da resposta JSON da IA
type nodePoolAIAnalysisResult struct {
	Predictions      NodePoolPredictionsAnalysis `json:"predictions"`
	RootCause        NodePoolRootCauseAnalysis   `json:"root_cause"`
	ExecutiveSummary NodePoolExecutiveSummary    `json:"executive_summary"`
	Recommendations  []NodePoolRecommendation    `json:"recommendations"`
}

// ==============================================================================
// Ponto de entrada principal
// ==============================================================================

// Analyze executa a análise preditiva completa de um node pool.
func (a *NodePoolAnalyzer) Analyze(ctx context.Context, req NodePoolPredictionRequest) (*NodePoolPredictionResult, error) {
	startTime := time.Now()
	requestID := uuid.New().String()

	log.Info().
		Str("request_id", requestID).
		Str("cluster", req.Cluster).
		Str("nodepool", req.NodePoolName).
		Msg("Iniciando análise preditiva de node pool")

	result := &NodePoolPredictionResult{
		RequestID:    requestID,
		Cluster:      req.Cluster,
		NodePoolName: req.NodePoolName,
		AnalyzedAt:   time.Now(),
	}

	// 1. Coletar métricas
	metrics, err := a.collector.Collect(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("falha ao coletar métricas do pool: %w", err)
	}
	result.RawMetrics = *metrics

	// 2. Calcular trends
	trends := a.calculateTrends(metrics)
	result.Trends = trends

	// 3. Calcular Health Score (determinístico, baseado em métricas)
	result.HealthScore = a.calculateHealthScore(metrics, trends)

	// 4. Análise IA
	aiAnalysis, aiErr := a.performAIAnalysis(ctx, req, metrics, trends)
	if aiErr != nil {
		log.Warn().Err(aiErr).Msg("Análise IA falhou — usando fallback baseado em regras")
		if a.aiProvider != nil {
			aierrors.RecordGlobalAIError(req.UserEmail, a.aiProvider.GetName(), a.aiProvider.GetModel(), aiErr)
			aierrors.IncrementAICall(req.UserEmail, false)
		}
		aiAnalysis = a.fallbackAnalysis(metrics, trends, result.HealthScore)
	} else {
		if a.aiProvider != nil {
			aierrors.RecordGlobalAIError(req.UserEmail, a.aiProvider.GetName(), a.aiProvider.GetModel(), nil)
			aierrors.IncrementAICall(req.UserEmail, true)
		}
	}

	// 5. Transferir resultados da IA
	result.Predictions = aiAnalysis.Predictions
	result.RootCauseAnalysis = aiAnalysis.RootCause
	result.ExecutiveSummary = aiAnalysis.ExecutiveSummary
	result.Recommendations = aiAnalysis.Recommendations

	// 6. Enriquecer predictions com timestamps calculados
	a.enrichPredictionsWithTimestamps(&result.Predictions, time.Now())

	// 7. Enriquecer predictions com confidence percent
	a.enrichPredictionsWithConfidence(&result.Predictions, metrics, trends)

	// 8. Calcular ActionSummary
	result.ActionSummary = a.calculateActionSummary(result, metrics, trends)

	// 9. Análise de custo (baseada no VM SKU real — mais precisa que deployment)
	result.CostAnalysis = a.costAnalyzer.Calculate(metrics)

	// 9.5 Timeline de saturação determinística (regressão linear D-0/D-3/D-7/D-14)
	result.SaturationTimeline = a.calculateSaturationTimeline(metrics, trends)

	// 10. Duração total
	result.DurationMs = time.Since(startTime).Milliseconds()

	log.Info().
		Str("request_id", requestID).
		Int64("duration_ms", result.DurationMs).
		Int("health_score", result.HealthScore.Overall).
		Str("category", result.HealthScore.Category).
		Msg("Análise preditiva de node pool concluída")

	return result, nil
}

// ==============================================================================
// 3.2 – Health Score (pesos específicos de node pool)
// ==============================================================================

// calculateHealthScore calcula o health score do pool com pesos específicos:
// NodeAvailability 25% | ResourceHeadroom 30% | PodDensity 20% | ConntrackSafety 15% | AutoscalerHealth 10%
func (a *NodePoolAnalyzer) calculateHealthScore(metrics *NodePoolMetrics, trends NodePoolTrends) NodePoolHealthScore {
	breakdown := NodePoolHealthBreakdown{}

	// --- Node Availability (25%): nodes Ready vs total ---
	total := len(metrics.NodesSnapshot)
	if total == 0 {
		// Sem snapshots → usar CurrentNodes como base
		total = metrics.CurrentNodes
	}
	readyNodes := 0
	for _, s := range metrics.NodesSnapshot {
		if s.Status == "Ready" && !s.IsUnschedulable {
			readyNodes++
		}
	}
	if total > 0 {
		availRatio := float64(readyNodes) / float64(total)
		breakdown.NodeAvailability = clampInt(int(availRatio*100), 0, 100)
	} else {
		breakdown.NodeAvailability = 50 // desconhecido
	}

	// --- Resource Headroom (30%): quão longe estamos da saturação de CPU/mem ---
	maxCPU := 0.0
	maxMem := 0.0
	for _, s := range metrics.NodesSnapshot {
		if s.CPUUsagePercent > maxCPU {
			maxCPU = s.CPUUsagePercent
		}
		if s.MemUsagePercent > maxMem {
			maxMem = s.MemUsagePercent
		}
	}
	// Pior caso entre CPU e memória
	worstUtil := math.Max(maxCPU, maxMem)
	switch {
	case worstUtil < 60:
		breakdown.ResourceHeadroom = 100
	case worstUtil < 75:
		breakdown.ResourceHeadroom = 80
	case worstUtil < 85:
		breakdown.ResourceHeadroom = 55
	case worstUtil < 95:
		breakdown.ResourceHeadroom = 25
	default:
		breakdown.ResourceHeadroom = 5
	}

	// --- Pod Density (20%): % capacidade de pods no node mais cheio ---
	maxPodDensity := 0.0
	for _, s := range metrics.NodesSnapshot {
		if s.PodDensityPercent > maxPodDensity {
			maxPodDensity = s.PodDensityPercent
		}
	}
	switch {
	case maxPodDensity < 50:
		breakdown.PodDensity = 100
	case maxPodDensity < 70:
		breakdown.PodDensity = 80
	case maxPodDensity < 85:
		breakdown.PodDensity = 50
	default:
		breakdown.PodDensity = 20
	}

	// --- ConntrackSafety (15%): % conntrack no node mais saturado do pool ---
	conntrackScore := 100
	if metrics.ConntrackPool.HasSufficientData {
		maxCT := metrics.ConntrackPool.MaxUsage
		switch {
		case maxCT < 50:
			conntrackScore = 100
		case maxCT < 70:
			conntrackScore = 80
		case maxCT < 85:
			conntrackScore = 45
		case maxCT < 95:
			conntrackScore = 15
		default:
			conntrackScore = 0
		}
	}
	breakdown.ConntrackSafety = conntrackScore

	// --- AutoscalerHealth (10%): eventos de scaling (sucesso vs falha) ---
	failedEvents := 0
	totalEvents := len(metrics.AutoscalerEvents)
	for _, ev := range metrics.AutoscalerEvents {
		if strings.Contains(ev.Type, "failed") {
			failedEvents++
		}
	}
	autoscalerScore := 100
	if totalEvents > 0 {
		failRatio := float64(failedEvents) / float64(totalEvents)
		switch {
		case failRatio == 0:
			autoscalerScore = 100
		case failRatio < 0.25:
			autoscalerScore = 75
		case failRatio < 0.5:
			autoscalerScore = 50
		default:
			autoscalerScore = 20
		}
	}
	breakdown.AutoscalerHealth = autoscalerScore

	// --- Overall (média ponderada) ---
	overall := (breakdown.NodeAvailability*25 +
		breakdown.ResourceHeadroom*30 +
		breakdown.PodDensity*20 +
		breakdown.ConntrackSafety*15 +
		breakdown.AutoscalerHealth*10) / 100

	category := "healthy"
	if overall < 50 {
		category = "critical"
	} else if overall < 75 {
		category = "warning"
	}

	log.Debug().
		Int("overall", overall).
		Str("category", category).
		Int("node_availability", breakdown.NodeAvailability).
		Int("resource_headroom", breakdown.ResourceHeadroom).
		Int("pod_density", breakdown.PodDensity).
		Int("conntrack_safety", breakdown.ConntrackSafety).
		Int("autoscaler_health", breakdown.AutoscalerHealth).
		Msg("Health Score calculado")

	return NodePoolHealthScore{
		Overall:     overall,
		Category:    category,
		Breakdown:   breakdown,
		LastUpdated: time.Now(),
	}
}

// ==============================================================================
// 3.4 – Calcular tendências do pool
// ==============================================================================

// calculateTrends deriva as tendências do pool a partir dos TrendSnapshots coletados.
func (a *NodePoolAnalyzer) calculateTrends(metrics *NodePoolMetrics) NodePoolTrends {
	trends := NodePoolTrends{}

	// Helper: extrai valor D-0 e D-N de uma slice de TrendSnapshot
	getValue := func(snapshots []TrendSnapshot, daysAgo int) (float64, bool) {
		for _, s := range snapshots {
			if s.DaysAgo == daysAgo {
				return s.ValuePerNode, true
			}
		}
		return 0, false
	}

	// Helper: calcular variação percentual entre dois valores
	changePct := func(current, past float64) float64 {
		if past == 0 {
			return 0
		}
		return (current - past) / past * 100.0
	}

	// Helper: classificar direção da tendência
	classifyTrend := func(change7d, change14d float64) TrendDirection {
		// Usa média dos dois períodos para suavizar ruído
		avgChange := (change7d + change14d) / 2.0
		switch {
		case avgChange > 10:
			return TrendUp
		case avgChange < -10:
			return TrendDown
		case math.Abs(avgChange) <= 5:
			return TrendStable
		default:
			// Mudanças moderadas ou variáveis
			if change7d*change14d < 0 {
				return TrendVolatile // sinais opostos = volátil
			}
			return TrendStable
		}
	}

	// CPU
	cpu0, ok0 := getValue(metrics.CPUTrendPerNode, 0)
	cpu3, ok3 := getValue(metrics.CPUTrendPerNode, 3)
	cpu7, ok7 := getValue(metrics.CPUTrendPerNode, 7)
	cpu14, ok14 := getValue(metrics.CPUTrendPerNode, 14)

	if ok0 && ok3 {
		trends.CPUChange3d = changePct(cpu0, cpu3)
	}
	if ok0 && ok7 {
		trends.CPUChange7d = changePct(cpu0, cpu7)
	}
	if ok0 && ok14 {
		trends.CPUChange14d = changePct(cpu0, cpu14)
	}
	if ok7 || ok14 {
		trends.CPUTrend = classifyTrend(trends.CPUChange7d, trends.CPUChange14d)
	}

	// Memória
	mem0, _ := getValue(metrics.MemTrendPerNode, 0)
	mem7, ok7m := getValue(metrics.MemTrendPerNode, 7)
	mem14, ok14m := getValue(metrics.MemTrendPerNode, 14)

	if ok7m {
		trends.MemChange7d = changePct(mem0, mem7)
	}
	if ok14m {
		trends.MemChange14d = changePct(mem0, mem14)
	}
	if ok7m || ok14m {
		trends.MemTrend = classifyTrend(trends.MemChange7d, trends.MemChange14d)
	}

	// Pods
	pods0, _ := getValue(metrics.PodsTrendPerNode, 0)
	pods7, ok7p := getValue(metrics.PodsTrendPerNode, 7)

	if ok7p {
		trends.PodsChange7d = changePct(pods0, pods7)
		trends.PodsTrend = classifyTrend(trends.PodsChange7d, trends.PodsChange7d)
	}

	// ConntrackTrend: usa MaxUsage do pool atual + growth rate calculado pelo collector
	if metrics.ConntrackPool.HasSufficientData && metrics.ConntrackPool.AvgGrowthRatePerH > 0 {
		// Se crescimento positivo e já em uso significativo → increasing
		if metrics.ConntrackPool.MaxUsage > 40 {
			trends.ConntrackTrend = TrendUp
			// Estimativa de mudança 7d: growthRate * 24h * 7 / totalLimit * 100
			if metrics.ConntrackPool.TotalLimit > 0 {
				growthIn7d := metrics.ConntrackPool.AvgGrowthRatePerH * 24 * 7
				trends.ConntrackChange7d = growthIn7d / float64(metrics.ConntrackPool.TotalLimit) * 100.0
			}
		} else {
			trends.ConntrackTrend = TrendStable
		}
	} else if metrics.ConntrackPool.HasSufficientData {
		// Sem growth rate disponível — classifica apenas por MaxUsage
		ct := metrics.ConntrackPool.MaxUsage
		if ct >= 85 {
			trends.ConntrackTrend = TrendUp
		} else if ct >= 70 {
			trends.ConntrackTrend = TrendUp
		} else {
			trends.ConntrackTrend = TrendStable
		}
	} else {
		trends.ConntrackTrend = TrendStable
	}

	log.Debug().
		Str("cpu_trend", string(trends.CPUTrend)).
		Float64("cpu_change_7d", trends.CPUChange7d).
		Str("mem_trend", string(trends.MemTrend)).
		Float64("mem_change_7d", trends.MemChange7d).
		Str("conntrack_trend", string(trends.ConntrackTrend)).
		Msg("Tendências do pool calculadas")

	return trends
}

// ==============================================================================
// 3.5 – Prompt IA
// ==============================================================================

// performAIAnalysis envia dados para a IA e parseia a resposta
func (a *NodePoolAnalyzer) performAIAnalysis(
	ctx context.Context,
	req NodePoolPredictionRequest,
	metrics *NodePoolMetrics,
	trends NodePoolTrends,
) (*nodePoolAIAnalysisResult, error) {
	if a.aiProvider == nil {
		return nil, fmt.Errorf("nenhum provider de IA configurado")
	}

	prompt := a.buildAIPrompt(metrics, trends)

	response, err := a.aiProvider.Analyze(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("falha na chamada IA: %w", err)
	}

	var result nodePoolAIAnalysisResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// Tentar extrair JSON de texto
		jsonStr := extractJSONFromText(response)
		if err2 := json.Unmarshal([]byte(jsonStr), &result); err2 != nil {
			return nil, fmt.Errorf("falha ao parsear resposta da IA: %w (raw: %s...)", err2, truncate(response, 200))
		}
	}

	return &result, nil
}

// buildAIPrompt constrói prompt estruturado e específico para node pool
func (a *NodePoolAnalyzer) buildAIPrompt(metrics *NodePoolMetrics, trends NodePoolTrends) string {
	var sb strings.Builder

	sb.WriteString("Você é um especialista sênior em infraestrutura Kubernetes e Azure AKS.\n")
	sb.WriteString("Analise o node pool abaixo e forneça análise preditiva completa em JSON.\n\n")
	sb.WriteString("**IMPORTANTE: Toda a análise DEVE estar em PORTUGUÊS BRASILEIRO (PT-BR). SEM EMOJIS.**\n\n")

	// --- Contexto do Pool ---
	azureCluster := metrics.AzureCluster
	if azureCluster == "" {
		azureCluster = strings.TrimSuffix(metrics.Cluster, "-admin")
	}
	sb.WriteString("# CONTEXTO DO NODE POOL\n\n")
	sb.WriteString(fmt.Sprintf("- Pool: %s\n", metrics.NodePoolName))
	sb.WriteString(fmt.Sprintf("- Cluster (Azure): %s\n", azureCluster))
	sb.WriteString(fmt.Sprintf("- VM SKU: %s\n", ifEmpty(metrics.VMSize, "desconhecido")))
	sb.WriteString(fmt.Sprintf("- Nodes atuais: %d | Min: %d | Max: %d\n",
		metrics.CurrentNodes, metrics.MinNodes, metrics.MaxNodes))
	sb.WriteString(fmt.Sprintf("- Autoscaler: %s\n", boolToSim(metrics.AutoscalerEnabled)))
	sb.WriteString(fmt.Sprintf("- Dados disponíveis: Prometheus=%s, NodeExporter=%s, Azure=%s\n\n",
		boolToSim(metrics.DataSources.PrometheusAvailable),
		boolToSim(metrics.DataSources.NodeExporterAvailable),
		boolToSim(metrics.DataSources.AzureAPIAvailable)))
	sb.WriteString(fmt.Sprintf("IMPORTANTE para comandos Azure CLI: use --cluster-name %s (SEM sufixo -admin)\n\n", azureCluster))

	// --- Top 3 nodes mais saturados ---
	sb.WriteString("# ESTADO ATUAL DOS NODES (top 3 mais saturados)\n\n")
	topNodes := topSaturatedNodes(metrics.NodesSnapshot, 3)
	if len(topNodes) == 0 {
		sb.WriteString("Dados de snapshot indisponíveis.\n\n")
	} else {
		for i, n := range topNodes {
			sb.WriteString(fmt.Sprintf("## Node %d: %s (%s)\n", i+1, n.NodeName, n.Status))
			sb.WriteString(fmt.Sprintf("- CPU: %.1f%% | Mem: %.1f%% | Disco: %.1f%%\n",
				n.CPUUsagePercent, n.MemUsagePercent, n.DiskUsagePercent))
			sb.WriteString(fmt.Sprintf("- Pods: %d/%d (%.1f%%)\n",
				n.PodCount, n.PodCapacity, n.PodDensityPercent))
			if n.ConntrackPercent > 0 {
				sb.WriteString(fmt.Sprintf("- conntrack: %.1f%%\n", n.ConntrackPercent))
			}
			if n.IsUnschedulable {
				sb.WriteString("- STATUS: CORDONED (unschedulable)\n")
			}
			if len(n.ActiveConditions) > 0 {
				sb.WriteString(fmt.Sprintf("- Pressões ativas: %s\n", strings.Join(n.ActiveConditions, ", ")))
			}
			sb.WriteString("\n")
		}
	}

	// --- Tendências ---
	sb.WriteString("# TENDENCIAS (normalizado por node — comparável mesmo com scaling)\n\n")
	sb.WriteString(fmt.Sprintf("- CPU: %s (D-3: %+.1f%%, D-7: %+.1f%%, D-14: %+.1f%%)\n",
		trends.CPUTrend, trends.CPUChange3d, trends.CPUChange7d, trends.CPUChange14d))
	sb.WriteString(fmt.Sprintf("- Memória: %s (D-7: %+.1f%%, D-14: %+.1f%%)\n",
		trends.MemTrend, trends.MemChange7d, trends.MemChange14d))
	sb.WriteString(fmt.Sprintf("- Pods/node: %s (D-7: %+.1f%%)\n",
		trends.PodsTrend, trends.PodsChange7d))
	if metrics.DataSources.NodeExporterAvailable {
		sb.WriteString(fmt.Sprintf("- conntrack: %s (D-7 estimado: %+.1f%%)\n",
			trends.ConntrackTrend, trends.ConntrackChange7d))
	}
	sb.WriteString("\n")

	// --- Análise conntrack ---
	if metrics.ConntrackPool.HasSufficientData {
		sb.WriteString("# ANALISE CONNTRACK (CRITICO — esgotamento descarta conexoes silenciosamente)\n\n")
		sb.WriteString(fmt.Sprintf("- Uso médio no pool: %.1f%%\n", metrics.ConntrackPool.AvgUsage))
		sb.WriteString(fmt.Sprintf("- Pior node: %s (%.1f%%)\n",
			metrics.ConntrackPool.HighestNode, metrics.ConntrackPool.MaxUsage))
		sb.WriteString(fmt.Sprintf("- Nodes em warning (>70%%): %d | Nodes críticos (>85%%): %d\n",
			metrics.ConntrackPool.NodesWarning, metrics.ConntrackPool.NodesCritical))

		if metrics.ConntrackPool.NodesCritical > 0 {
			sb.WriteString("\n[URGENTE] CONNTRACK EM NIVEL CRITICO. Incluir recomendação de alta prioridade:\n")
			sb.WriteString("- Comando: sysctl -w net.netfilter.nf_conntrack_max=<novo_valor>\n")
			sb.WriteString("- Ou ajustar via DaemonSet para persistência entre reboots\n")
		} else if metrics.ConntrackPool.NodesWarning > 0 {
			sb.WriteString("\n[ATENCAO] conntrack elevado — monitorar e planejar aumento preventivo\n")
		}
		sb.WriteString("\n")
	}

	// --- Bin packing ---
	bp := metrics.BinPacking
	if bp.CPUEfficiency > 0 || bp.MemEfficiency > 0 {
		sb.WriteString("# FRAGMENTACAO DO POOL (BIN PACKING)\n\n")
		sb.WriteString(fmt.Sprintf("- Eficiência CPU: %.1f%% | Mem: %.1f%% | Pods: %.1f%%\n",
			bp.CPUEfficiency, bp.MemEfficiency, bp.PodEfficiency))
		sb.WriteString(fmt.Sprintf("- Nível de fragmentação: %s\n", bp.FragmentationLevel))
		if bp.ScaleInCandidates > 0 {
			sb.WriteString(fmt.Sprintf("- Scale-in possível: %d node(s) com < 30%% utilização (%s)\n",
				bp.ScaleInCandidates, boolToSim(bp.ScaleInSafe)))
		}
		if bp.WastedResources != "" {
			sb.WriteString(fmt.Sprintf("- Recursos desperdiçados estimados: %s\n", bp.WastedResources))
		}
		sb.WriteString("\n")
	}

	// --- Autoscaler events ---
	if len(metrics.AutoscalerEvents) > 0 {
		sb.WriteString("# HISTORICO DO AUTOSCALER (últimos 7 dias)\n\n")
		shown := 5
		if len(metrics.AutoscalerEvents) < shown {
			shown = len(metrics.AutoscalerEvents)
		}
		for _, ev := range metrics.AutoscalerEvents[:shown] {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n",
				ev.Timestamp.Format("02/01 15:04"), ev.Type, truncate(ev.Message, 120)))
		}
		sb.WriteString("\n")
	}

	// --- Perguntas-chave para IA ---
	sb.WriteString("# PERGUNTAS-CHAVE PARA ANALISE\n\n")
	sb.WriteString("1. Quando o pool vai saturar dado o crescimento atual de CPU/mem?\n")
	sb.WriteString("2. conntrack vai esgotar antes da memória/CPU? Em que prazo?\n")
	sb.WriteString("3. O autoscaler consegue reagir antes da saturação (lead time ~10min para VMs Azure)?\n")
	sb.WriteString("4. Há fragmentação que permite scale-in seguro sem risco de saturação?\n")
	sb.WriteString("5. O VM SKU está adequado para o perfil de workload atual?\n\n")

	// --- Esquema de resposta esperado ---
	sb.WriteString("# RESPOSTA ESPERADA (JSON)\n\n")
	sb.WriteString(`Retorne APENAS o JSON abaixo, sem texto adicional:

{
  "predictions": {
    "short_term": [
      {
        "timeframe": "4h",
        "event": "descrição do evento previsto",
        "probability": 0.75,
        "severity": "medium",
        "impact": "impacto se ocorrer",
        "indicators": ["evidência 1", "evidência 2"]
      }
    ],
    "medium_term": [],
    "long_term": []
  },
  "root_cause": {
    "identified_causes": [
      {
        "cause": "causa identificada",
        "evidence": ["evidência"],
        "certainty": 0.85,
        "category": "capacity",
        "remediation": "ação corretiva"
      }
    ],
    "primary_factor": "fator principal",
    "contributing_factors": ["fator 1", "fator 2"]
  },
  "executive_summary": {
    "current_state": "estado atual do pool",
    "key_findings": ["achado 1", "achado 2"],
    "risk_level": "medium",
    "action_required": true,
    "expected_outcome": "resultado esperado das ações",
    "business_impact": "impacto para o negócio"
  },
  "recommendations": [
    {
      "priority": 1,
      "title": "título da recomendação",
      "description": "descrição detalhada",
      "category": "scaling",
      "actions": ["passo 1", "passo 2"],
      "expected_impact": "impacto esperado",
      "time_required": "10 minutos",
      "complexity": "low",
      "risk_level": "low",
      "requires_downtime": false,
      "efficiency_gain_percent": 0.0
    }
  ]
}

REGRAS:
- TUDO em Português Brasileiro
- SEM emojis, SEM ícones Unicode
- Severity: "low", "medium", "high", "critical"
- Category (root_cause): "capacity", "config", "workload", "conntrack", "autoscaler", "network"
- Category (recommendations): "scaling", "conntrack", "config", "cost", "monitoring", "reliability"
- Risk_level: "low", "medium", "high", "critical"
- Complexity: "low", "medium", "high"
- Considere o lead time de 10-15min para provisionar VMs AKS ao avaliar urgência
- Mencione comandos az ou kubectl específicos nas actions quando aplicável
- Retorne APENAS o JSON, sem markdown, sem texto adicional
`)

	return sb.String()
}

// ==============================================================================
// Fallback quando IA falha
// ==============================================================================

// fallbackAnalysis gera análise baseada em regras quando a IA está indisponível.
func (a *NodePoolAnalyzer) fallbackAnalysis(
	metrics *NodePoolMetrics,
	trends NodePoolTrends,
	healthScore NodePoolHealthScore,
) *nodePoolAIAnalysisResult {
	result := &nodePoolAIAnalysisResult{
		Predictions: NodePoolPredictionsAnalysis{
			ShortTerm:  []NodePoolPrediction{},
			MediumTerm: []NodePoolPrediction{},
			LongTerm:   []NodePoolPrediction{},
		},
		RootCause: NodePoolRootCauseAnalysis{
			IdentifiedCauses:    []NodePoolRootCause{},
			PrimaryFactor:       "Análise automática indisponível",
			ContributingFactors: []string{},
		},
		ExecutiveSummary: NodePoolExecutiveSummary{
			CurrentState:    fmt.Sprintf("Pool %s com %d nodes — health score: %d/100 (%s)", metrics.NodePoolName, metrics.CurrentNodes, healthScore.Overall, healthScore.Category),
			KeyFindings:     []string{},
			RiskLevel:       healthScore.Category,
			ActionRequired:  healthScore.Overall < 70,
			ExpectedOutcome: "Análise de IA indisponível — revisar métricas manualmente",
			BusinessImpact:  "Não foi possível calcular impacto sem análise de IA",
		},
		Recommendations: []NodePoolRecommendation{},
	}

	// Gerar previsões baseadas em tendências observadas
	if trends.CPUTrend == TrendUp && trends.CPUChange7d > 15 {
		prob := math.Min(0.9, trends.CPUChange7d/100.0)
		result.Predictions.MediumTerm = append(result.Predictions.MediumTerm, NodePoolPrediction{
			Timeframe:   "24h",
			Event:       fmt.Sprintf("CPU pode atingir nível crítico dado crescimento de %.1f%% em 7 dias", trends.CPUChange7d),
			Probability: prob,
			Severity:    "high",
			Impact:      "Pods podem ser throttled; autoscaler pode escalar o pool",
			Indicators:  []string{fmt.Sprintf("CPU crescendo %.1f%% por semana por node", trends.CPUChange7d)},
		})
	}

	if metrics.ConntrackPool.HasSufficientData && metrics.ConntrackPool.MaxUsage > 70 {
		sev := "medium"
		if metrics.ConntrackPool.MaxUsage > 85 {
			sev = "critical"
		}
		result.Predictions.ShortTerm = append(result.Predictions.ShortTerm, NodePoolPrediction{
			Timeframe:   "4h",
			Event:       fmt.Sprintf("conntrack no node %s pode atingir limite (atualmente %.1f%%)", metrics.ConntrackPool.HighestNode, metrics.ConntrackPool.MaxUsage),
			Probability: 0.7,
			Severity:    sev,
			Impact:      "Novas conexoes podem ser descartadas silenciosamente causando timeouts",
			Indicators:  []string{fmt.Sprintf("conntrack em %.1f%% do limite", metrics.ConntrackPool.MaxUsage)},
		})
	}

	// Gerar findings para o executive summary
	if trends.CPUTrend == TrendUp {
		result.ExecutiveSummary.KeyFindings = append(result.ExecutiveSummary.KeyFindings,
			fmt.Sprintf("CPU crescendo %.1f%% nos últimos 7 dias por node", trends.CPUChange7d))
	}
	if metrics.ConntrackPool.NodesCritical > 0 {
		result.ExecutiveSummary.KeyFindings = append(result.ExecutiveSummary.KeyFindings,
			fmt.Sprintf("%d node(s) com conntrack em nivel critico (>85%%)", metrics.ConntrackPool.NodesCritical))
	}
	if metrics.BinPacking.ScaleInCandidates > 0 {
		result.ExecutiveSummary.KeyFindings = append(result.ExecutiveSummary.KeyFindings,
			fmt.Sprintf("%d node(s) candidatos para scale-in (< 30%% utilização)", metrics.BinPacking.ScaleInCandidates))
	}

	// Recomendação básica
	if metrics.ConntrackPool.HasSufficientData && metrics.ConntrackPool.NodesCritical > 0 {
		result.Recommendations = append(result.Recommendations, NodePoolRecommendation{
			Priority:    1,
			Title:       "Aumentar limite de conntrack nos nodes críticos",
			Description: "Nodes com conntrack > 85% estão em risco de descartar conexoes silenciosamente",
			Category:    "conntrack",
			Actions: []string{
				"sysctl -w net.netfilter.nf_conntrack_max=524288",
				"Aplicar via DaemonSet para persistência entre reboots",
				"Monitorar evolução após ajuste",
			},
			ExpectedImpact:   "Eliminar risco de descarte de conexoes",
			TimeRequired:     "30 minutos",
			Complexity:       "medium",
			RiskLevel:        "low",
			RequiresDowntime: false,
		})
	}

	result.Recommendations = append(result.Recommendations, NodePoolRecommendation{
		Priority:         5,
		Title:            "Revisar métricas manualmente",
		Description:      "Análise de IA indisponível — verificar Prometheus e logs manualmente",
		Category:         "monitoring",
		Actions:          []string{"Verificar dashboards Prometheus", "Analisar logs do cluster autoscaler"},
		ExpectedImpact:   "Identificar problemas manualmente",
		TimeRequired:     "30 minutos",
		Complexity:       "low",
		RiskLevel:        "low",
		RequiresDowntime: false,
	})

	return result
}

// ==============================================================================
// 3.3 – ActionSummary
// ==============================================================================

// calculateActionSummary gera resumo de ações para decisão rápida.
func (a *NodePoolAnalyzer) calculateActionSummary(
	result *NodePoolPredictionResult,
	metrics *NodePoolMetrics,
	trends NodePoolTrends,
) NodePoolActionSummary {
	summary := NodePoolActionSummary{
		NextReviewDays:    7,
		OverallConfidence: a.calculateOverallConfidence(metrics),
	}

	// Status baseado no health score
	switch {
	case result.HealthScore.Overall >= 75:
		summary.Status = "healthy"
		summary.StatusColor = "green"
		summary.StatusMessage = "Pool operacional"
		summary.NextReviewDays = 14
	case result.HealthScore.Overall >= 50:
		summary.Status = "attention"
		summary.StatusColor = "yellow"
		summary.StatusMessage = "Requer atenção"
		summary.NextReviewDays = 7
	default:
		summary.Status = "critical"
		summary.StatusColor = "red"
		summary.StatusMessage = "Risco de saturação"
		summary.NextReviewDays = 1
	}

	// Contar ações
	summary.TotalActions = len(result.Recommendations)
	for _, rec := range result.Recommendations {
		if rec.Priority <= 2 {
			summary.UrgentActions++
		}
	}

	// Ação principal (prioridade mais alta)
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

	// Tempo até crítico
	summary.HoursToCritical, summary.CriticalMetric, summary.CriticalReason =
		a.calculateHoursToCritical(metrics, trends)

	// Ajustar status se risco iminente
	if summary.HoursToCritical != nil && *summary.HoursToCritical < 24 {
		summary.Status = "critical"
		summary.StatusColor = "red"
		summary.StatusMessage = "Risco iminente"
		summary.NextReviewDays = 0
	} else if summary.HoursToCritical != nil && *summary.HoursToCritical < 72 {
		if summary.Status == "healthy" {
			summary.Status = "attention"
			summary.StatusColor = "yellow"
			summary.StatusMessage = "Atenção preventiva"
		}
		summary.NextReviewDays = 1
	}

	return summary
}

// calculateHoursToCritical estima horas até saturação baseado nas tendências.
func (a *NodePoolAnalyzer) calculateHoursToCritical(metrics *NodePoolMetrics, trends NodePoolTrends) (*int, string, string) {
	type candidate struct {
		hours  int
		metric string
		reason string
	}
	var candidates []candidate

	// conntrack: mais urgente porque sem aviso visível
	if metrics.ConntrackPool.HasSufficientData {
		ct := metrics.ConntrackPool.MaxUsage
		if ct >= 85 {
			h := 0
			candidates = append(candidates, candidate{h, "conntrack",
				fmt.Sprintf("conntrack já em %.1f%% no node %s", ct, metrics.ConntrackPool.HighestNode)})
		} else if ct >= 70 && metrics.ConntrackPool.AvgGrowthRatePerH > 0 {
			remaining := 85 - ct
			// Quantas horas para atingir 85%? usa TotalLimit como capacidade total do pool
			if metrics.ConntrackPool.TotalLimit > 0 {
				totalCapacity := float64(metrics.ConntrackPool.TotalLimit)
				growthPerHour := metrics.ConntrackPool.AvgGrowthRatePerH
				pctGrowthPerHour := growthPerHour / totalCapacity * 100.0
				if pctGrowthPerHour > 0 {
					h := int(remaining / pctGrowthPerHour)
					if h < 168 { // < 1 semana → relevante
						candidates = append(candidates, candidate{h, "conntrack",
							fmt.Sprintf("conntrack atingirá 85%% em ~%dh (node: %s, atual: %.1f%%)", h, metrics.ConntrackPool.HighestNode, ct)})
					}
				}
			}
		} else if ct >= 70 {
			// Growth rate indisponível — estimativa conservadora: 48h
			candidates = append(candidates, candidate{48, "conntrack",
				fmt.Sprintf("conntrack em %.1f%% no node %s — taxa de crescimento não disponível", ct, metrics.ConntrackPool.HighestNode)})
		}
	}

	// CPU: baseado na tendência de crescimento
	if trends.CPUTrend == TrendUp && trends.CPUChange7d > 0 {
		// Node mais saturado
		maxCPU := 0.0
		for _, s := range metrics.NodesSnapshot {
			if s.CPUUsagePercent > maxCPU {
				maxCPU = s.CPUUsagePercent
			}
		}
		if maxCPU > 0 && maxCPU < 90 {
			// Crescimento médio por hora: change7d / (7*24)
			growthPerHour := trends.CPUChange7d / (7 * 24)
			if growthPerHour > 0 {
				remaining := 85.0 - maxCPU
				h := int(remaining / growthPerHour)
				if h > 0 && h < 168 {
					candidates = append(candidates, candidate{h, "cpu",
						fmt.Sprintf("CPU atingirá 85%% em ~%dh dado crescimento de %.1f%%/semana por node", h, trends.CPUChange7d)})
				}
			}
		}
	}

	// Pods: baseado na densidade máxima
	for _, s := range metrics.NodesSnapshot {
		if s.PodDensityPercent >= 85 {
			h := 0
			candidates = append(candidates, candidate{h, "pods",
				fmt.Sprintf("Node %s já atingiu %.1f%% da capacidade de pods", s.NodeName, s.PodDensityPercent)})
		}
	}

	if len(candidates) == 0 {
		return nil, "", ""
	}

	// Selecionar o mais urgente (menor tempo)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].hours < candidates[j].hours
	})

	h := candidates[0].hours
	return &h, candidates[0].metric, candidates[0].reason
}

// ==============================================================================
// 3.4 – Saturation Timeline (regressão linear determinística)
// ==============================================================================

// calculateSaturationTimeline projeta datas de saturação para CPU, memória,
// pods e conntrack usando regressão linear nos snapshots D-0/D-3/D-7/D-14.
func (a *NodePoolAnalyzer) calculateSaturationTimeline(metrics *NodePoolMetrics, trends NodePoolTrends) PoolSaturationTimeline {
	now := time.Now()
	var forecasts []SaturationForecast

	// 1. CPU (threshold 85%)
	if f := saturationForecastFromTrend("cpu", metrics.CPUTrendPerNode, maxCPUCurrent(metrics.NodesSnapshot), 85.0, now); f != nil {
		forecasts = append(forecasts, *f)
	}

	// 2. Memória (threshold 85%)
	if f := saturationForecastFromTrend("memory", metrics.MemTrendPerNode, maxMemCurrent(metrics.NodesSnapshot), 85.0, now); f != nil {
		forecasts = append(forecasts, *f)
	}

	// 3. Densidade de pods por node (threshold 85%)
	if f := saturationForecastFromTrend("pods", metrics.PodsTrendPerNode, maxPodDensityCurrent(metrics.NodesSnapshot), 85.0, now); f != nil {
		forecasts = append(forecasts, *f)
	}

	// 4. Conntrack (threshold 85%) — usa taxa de crescimento do Prometheus quando disponível
	if metrics.DataSources.NodeExporterAvailable && metrics.ConntrackPool.HasSufficientData {
		if f := saturationForecastConntrack(metrics, now); f != nil {
			forecasts = append(forecasts, *f)
		}
	}

	// Ordenar: métricas que saturam primeiro aparecem primeiro; nil (estável) vai para o final
	sort.Slice(forecasts, func(i, j int) bool {
		di, dj := forecasts[i].DaysUntilSaturation, forecasts[j].DaysUntilSaturation
		if di == nil && dj == nil {
			return false
		}
		if di == nil {
			return false
		}
		if dj == nil {
			return true
		}
		return *di < *dj
	})

	timeline := PoolSaturationTimeline{Forecasts: forecasts}

	// Métrica mais crítica = a que satura primeiro (com data concreta)
	for i := range forecasts {
		if forecasts[i].DaysUntilSaturation != nil {
			f := forecasts[i]
			timeline.MostCritical = &f
			break
		}
	}

	// Sumário legível
	if timeline.MostCritical != nil {
		mc := timeline.MostCritical
		days := *mc.DaysUntilSaturation
		dateStr := mc.EstimatedDate.Format("02/01/2006")
		if mc.AffectedNode != "" {
			timeline.Summary = fmt.Sprintf("%s satura em %.0f dias (%s) — %s", mc.Metric, days, dateStr, mc.AffectedNode)
		} else {
			timeline.Summary = fmt.Sprintf("%s satura em %.0f dias (%s)", mc.Metric, days, dateStr)
		}
	} else {
		timeline.Summary = "Sem saturacao projetada nos proximos 30 dias"
	}

	return timeline
}

// saturationForecastFromTrend calcula regressão linear sobre snapshots D-0/D-3/D-7/D-14.
// x = DaysAgo (0=agora, 14=14 dias atrás), y = ValuePerNode.
// slope < 0 significa que o valor cresce ao longo do tempo (passado < presente).
func saturationForecastFromTrend(metric string, snapshots []TrendSnapshot, currentValue, threshold float64, now time.Time) *SaturationForecast {
	if currentValue <= 0 || threshold <= 0 {
		return nil
	}

	type pt struct{ x, y float64 }
	var pts []pt
	pts = append(pts, pt{0, currentValue}) // D-0 = agora

	for _, s := range snapshots {
		if s.DaysAgo > 0 && s.ValuePerNode > 0 {
			pts = append(pts, pt{float64(s.DaysAgo), s.ValuePerNode})
		}
	}
	if len(pts) < 2 {
		// Apenas 1 ponto: não há histórico suficiente para projeção
		return &SaturationForecast{
			Metric:       metric,
			CurrentValue: currentValue,
			Threshold:    threshold,
			Confidence:   "low",
			DataPoints:   1,
			UrgencyBadge: "ESTAVEL",
		}
	}

	// Regressão linear simples: y = a + b*x
	n := float64(len(pts))
	var sumX, sumY, sumXY, sumXX float64
	for _, p := range pts {
		sumX += p.x
		sumY += p.y
		sumXY += p.x * p.y
		sumXX += p.x * p.x
	}
	denom := n*sumXX - sumX*sumX
	var slope float64
	if math.Abs(denom) > 1e-9 {
		slope = (n*sumXY - sumX*sumY) / denom
	}
	// slope negativo: valor era menor no passado → crescendo → taxa de crescimento = -slope
	dailyGrowthRate := -slope

	// Aceleração: comparar crescimento D-0→D-7 com D-7→D-14
	var acceleration float64
	var d0, d7, d14 float64
	for _, p := range pts {
		switch int(p.x) {
		case 0:
			d0 = p.y
		case 7:
			d7 = p.y
		case 14:
			d14 = p.y
		}
	}
	if d7 > 0 && d0 > 0 {
		slopeRecent := (d0 - d7) / 7.0
		if d14 > 0 {
			slopeOlder := (d7 - d14) / 7.0
			acceleration = slopeRecent - slopeOlder // positivo = acelerando
		}
	}

	// Confiança baseada em número de snapshots históricos
	confidence := "low"
	switch {
	case len(pts) >= 3:
		confidence = "high"
	case len(pts) == 2:
		confidence = "medium"
	}

	forecast := &SaturationForecast{
		Metric:            metric,
		CurrentValue:      currentValue,
		Threshold:         threshold,
		DailyGrowthRate:   dailyGrowthRate,
		TrendAcceleration: acceleration,
		Confidence:        confidence,
		DataPoints:        len(pts),
	}

	// Projeção apenas se há crescimento e ainda não atingiu threshold
	if dailyGrowthRate > 0 && currentValue < threshold {
		days := (threshold - currentValue) / dailyGrowthRate
		forecast.DaysUntilSaturation = &days
		estimatedDate := now.Add(time.Duration(days*24) * time.Hour)
		forecast.EstimatedDate = &estimatedDate

		switch {
		case days <= 7:
			forecast.UrgencyBadge = "CRITICO"
		case days <= 30:
			forecast.UrgencyBadge = "ATENCAO"
		default:
			forecast.UrgencyBadge = "ESTAVEL"
		}
	} else if currentValue >= threshold {
		// Já saturado
		zero := 0.0
		forecast.DaysUntilSaturation = &zero
		t := now
		forecast.EstimatedDate = &t
		forecast.UrgencyBadge = "CRITICO"
	} else {
		forecast.UrgencyBadge = "ESTAVEL"
	}

	return forecast
}

// saturationForecastConntrack usa a taxa de crescimento do Prometheus (entries/hora)
// ao invés de regressão linear, pois conntrack já fornece taxa via rate().
func saturationForecastConntrack(metrics *NodePoolMetrics, now time.Time) *SaturationForecast {
	pool := metrics.ConntrackPool
	if !pool.HasSufficientData || pool.TotalLimit == 0 {
		return nil
	}

	threshold := 85.0
	currentPct := pool.MaxUsage

	forecast := &SaturationForecast{
		Metric:       "conntrack",
		AffectedNode: pool.HighestNode,
		CurrentValue: currentPct,
		Threshold:    threshold,
		DataPoints:   1,
		Confidence:   "medium",
	}

	if pool.AvgGrowthRatePerH > 0 {
		totalCap := float64(pool.TotalLimit)
		pctGrowthPerHour := pool.AvgGrowthRatePerH / totalCap * 100.0
		if pctGrowthPerHour > 0 {
			forecast.DailyGrowthRate = pctGrowthPerHour * 24.0
			if currentPct >= threshold {
				zero := 0.0
				forecast.DaysUntilSaturation = &zero
				t := now
				forecast.EstimatedDate = &t
				forecast.UrgencyBadge = "CRITICO"
			} else {
				remaining := threshold - currentPct
				days := remaining / forecast.DailyGrowthRate

				// Sanity check: se uso atual é baixo (<30%) mas a projeção é de saturação
				// em menos de 3 dias, é quase certo que é ruído de medição (ex: gauge
				// oscilando, janela de coleta muito curta, spike momentâneo de conexões).
				// Nesses casos, descartamos a projeção alarmante.
				if currentPct < 30.0 && days < 3.0 {
					forecast.DailyGrowthRate = 0
					forecast.UrgencyBadge = "ESTAVEL"
					return forecast
				}

				forecast.DaysUntilSaturation = &days
				estimatedDate := now.Add(time.Duration(days*24) * time.Hour)
				forecast.EstimatedDate = &estimatedDate
				switch {
				case days <= 7:
					forecast.UrgencyBadge = "CRITICO"
				case days <= 30:
					forecast.UrgencyBadge = "ATENCAO"
				default:
					forecast.UrgencyBadge = "ESTAVEL"
				}
			}
		} else {
			forecast.UrgencyBadge = "ESTAVEL"
		}
	} else {
		forecast.UrgencyBadge = "ESTAVEL"
	}

	return forecast
}

// maxCPUCurrent retorna o maior CPU% entre os nodes snapshot.
func maxCPUCurrent(snapshots []NodePoolNodeSnapshot) float64 {
	max := 0.0
	for _, s := range snapshots {
		if s.CPUUsagePercent > max {
			max = s.CPUUsagePercent
		}
	}
	return max
}

// maxMemCurrent retorna o maior Mem% entre os nodes snapshot.
func maxMemCurrent(snapshots []NodePoolNodeSnapshot) float64 {
	max := 0.0
	for _, s := range snapshots {
		if s.MemUsagePercent > max {
			max = s.MemUsagePercent
		}
	}
	return max
}

// maxPodDensityCurrent retorna o maior PodDensity% entre os nodes snapshot.
func maxPodDensityCurrent(snapshots []NodePoolNodeSnapshot) float64 {
	max := 0.0
	for _, s := range snapshots {
		if s.PodDensityPercent > max {
			max = s.PodDensityPercent
		}
	}
	return max
}

// calculateOverallConfidence estima a confiança geral da análise.
func (a *NodePoolAnalyzer) calculateOverallConfidence(metrics *NodePoolMetrics) float64 {
	confidence := 70.0

	if metrics.DataSources.PrometheusAvailable {
		confidence += 15.0
	}
	if metrics.DataSources.NodeExporterAvailable {
		confidence += 10.0
	}
	if metrics.DataSources.AzureAPIAvailable {
		confidence += 5.0
	}
	if metrics.DataSources.HistoryDepthDays >= 14 {
		confidence += 10.0 // já somou
	} else if metrics.DataSources.HistoryDepthDays >= 7 {
		confidence += 5.0
	} else if metrics.DataSources.HistoryDepthDays == 0 {
		confidence -= 20.0
	}

	if confidence > 95 {
		confidence = 95
	}
	if confidence < 10 {
		confidence = 10
	}
	return confidence
}

// ==============================================================================
// 3.6 / 3.7 – Enriquecimento de predictions
// ==============================================================================

// enrichPredictionsWithTimestamps adiciona timestamps às predictions baseado no momento atual.
func (a *NodePoolAnalyzer) enrichPredictionsWithTimestamps(predictions *NodePoolPredictionsAnalysis, baseTime time.Time) {
	calc := func(timeframe string) *time.Time {
		var d time.Duration
		switch timeframe {
		case "4h", "próximas 4 horas", "curto prazo":
			d = 4 * time.Hour
		case "24h", "próximas 24 horas", "médio prazo", "medio prazo":
			d = 24 * time.Hour
		case "7d", "próximos 7 dias", "longo prazo":
			d = 7 * 24 * time.Hour
		default:
			// Parsear formato "Xh" ou "Xd"
			if len(timeframe) > 0 {
				last := timeframe[len(timeframe)-1]
				if last == 'h' || last == 'H' {
					var hours int
					fmt.Sscanf(timeframe, "%dh", &hours)
					d = time.Duration(hours) * time.Hour
				} else if last == 'd' || last == 'D' {
					var days int
					fmt.Sscanf(timeframe, "%dd", &days)
					d = time.Duration(days) * 24 * time.Hour
				}
			}
		}
		if d == 0 {
			return nil
		}
		ts := baseTime.Add(d)
		return &ts
	}

	for i := range predictions.ShortTerm {
		predictions.ShortTerm[i].Timestamp = calc(predictions.ShortTerm[i].Timeframe)
	}
	for i := range predictions.MediumTerm {
		predictions.MediumTerm[i].Timestamp = calc(predictions.MediumTerm[i].Timeframe)
	}
	for i := range predictions.LongTerm {
		predictions.LongTerm[i].Timestamp = calc(predictions.LongTerm[i].Timeframe)
	}
}

// enrichPredictionsWithConfidence calcula confiança por prediction.
func (a *NodePoolAnalyzer) enrichPredictionsWithConfidence(
	predictions *NodePoolPredictionsAnalysis,
	metrics *NodePoolMetrics,
	trends NodePoolTrends,
) {
	// Confiança base: depende de disponibilidade de dados
	base := 75.0
	if !metrics.DataSources.PrometheusAvailable {
		base -= 25.0
	}
	if metrics.DataSources.HistoryDepthDays < 7 {
		base -= 15.0
	}
	if trends.CPUTrend == TrendVolatile || trends.MemTrend == TrendVolatile {
		base -= 10.0
	}

	calc := func(p *NodePoolPrediction) float64 {
		conf := base
		if p.Probability < 0.3 {
			conf -= 15.0
		} else if p.Probability > 0.9 {
			conf -= 5.0
		}
		// Previsões de longo prazo têm menos confiança
		if strings.Contains(p.Timeframe, "7d") || strings.Contains(p.Timeframe, "d") {
			conf -= 10.0
		}
		return math.Max(10, math.Min(95, conf))
	}

	for i := range predictions.ShortTerm {
		predictions.ShortTerm[i].ConfidencePercent = calc(&predictions.ShortTerm[i])
	}
	for i := range predictions.MediumTerm {
		predictions.MediumTerm[i].ConfidencePercent = calc(&predictions.MediumTerm[i])
	}
	for i := range predictions.LongTerm {
		predictions.LongTerm[i].ConfidencePercent = calc(&predictions.LongTerm[i])
	}
}

// ==============================================================================
// Utilidades internas
// ==============================================================================

// topSaturatedNodes retorna os N nodes mais saturados (maior CPU+mem).
func topSaturatedNodes(snapshots []NodePoolNodeSnapshot, n int) []NodePoolNodeSnapshot {
	sorted := make([]NodePoolNodeSnapshot, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool {
		// Saturação = máximo entre CPU e memória
		satI := math.Max(sorted[i].CPUUsagePercent, sorted[i].MemUsagePercent)
		satJ := math.Max(sorted[j].CPUUsagePercent, sorted[j].MemUsagePercent)
		return satI > satJ
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// clampInt garante que v está no intervalo [min, max].
func clampInt(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

// boolToSim converte bool para "Sim"/"Não".
func boolToSim(b bool) string {
	if b {
		return "Sim"
	}
	return "Nao"
}

// ifEmpty retorna fallback se s estiver vazio.
func ifEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// truncate trunca string em n caracteres.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// extractJSONFromText tenta extrair bloco JSON de texto com conteúdo extra.
func extractJSONFromText(text string) string {
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
