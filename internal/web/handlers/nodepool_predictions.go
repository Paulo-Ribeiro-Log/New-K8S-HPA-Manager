package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/monitoring/discovery"
	np "k8s-hpa-manager/internal/monitoring/nodepoolpredictions"
	"k8s-hpa-manager/internal/monitoring/prometheus"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// calculateNodePoolDelta compara o resultado atual com a análise anterior do mesmo pool.
// Retorna nil se não houver análise anterior ou se ocorrer erro ao desserializar.
func calculateNodePoolDelta(curr *np.NodePoolPredictionResult, store *storage.NodePoolPredictionsStore) *np.NodePoolAnalysisDelta {
	if store == nil {
		return nil
	}

	// Busca as 2 análises mais recentes (a salva agora + a anterior)
	records, err := store.List(&storage.NodePoolPredictionQueryFilters{
		Cluster:      curr.Cluster,
		NodePoolName: curr.NodePoolName,
		Limit:        2,
	})
	if err != nil || len(records) < 2 {
		return nil // primeira análise ou erro — sem delta
	}

	// records[0] = atual (recém salva), records[1] = anterior
	prevRecord := records[1]
	var prev np.NodePoolPredictionResult
	if unmarshalErr := json.Unmarshal([]byte(prevRecord.FullResult), &prev); unmarshalErr != nil {
		log.Warn().Err(unmarshalErr).Str("id", prevRecord.ID).Msg("Falha ao desserializar análise anterior para delta")
		return nil
	}

	daysSince := curr.AnalyzedAt.Sub(prev.AnalyzedAt).Hours() / 24.0

	delta := &np.NodePoolAnalysisDelta{
		PreviousID:         prevRecord.ID,
		PreviousAnalyzedAt: prev.AnalyzedAt,
		DaysSince:          daysSince,
		HealthScoreDelta:   curr.HealthScore.Overall - prev.HealthScore.Overall,
	}

	// Helper: pega value_per_node do snapshot D-0 de um TrendSnapshot slice
	currentVal := func(snapshots []np.TrendSnapshot) (float64, bool) {
		for _, s := range snapshots {
			if s.DaysAgo == 0 {
				return s.ValuePerNode, true
			}
		}
		return 0, false
	}

	// CPU
	if cCPU, ok1 := currentVal(curr.RawMetrics.CPUTrendPerNode); ok1 {
		if pCPU, ok2 := currentVal(prev.RawMetrics.CPUTrendPerNode); ok2 {
			delta.CPUDeltaPP = cCPU - pCPU
		}
	}
	// Memória
	if cMem, ok1 := currentVal(curr.RawMetrics.MemTrendPerNode); ok1 {
		if pMem, ok2 := currentVal(prev.RawMetrics.MemTrendPerNode); ok2 {
			delta.MemDeltaPP = cMem - pMem
		}
	}
	// Pods
	if cPods, ok1 := currentVal(curr.RawMetrics.PodsTrendPerNode); ok1 {
		if pPods, ok2 := currentVal(prev.RawMetrics.PodsTrendPerNode); ok2 {
			delta.PodsDelta = cPods - pPods
		}
	}
	// Conntrack
	if curr.RawMetrics.ConntrackPool.HasSufficientData && prev.RawMetrics.ConntrackPool.HasSufficientData {
		delta.ConntrackDeltaPP = curr.RawMetrics.ConntrackPool.MaxUsage - prev.RawMetrics.ConntrackPool.MaxUsage
	}
	// BinPacking (eficiência CPU — aumento = menos fragmentação = melhor)
	delta.BinPackingDeltaPP = curr.RawMetrics.BinPacking.CPUEfficiency - prev.RawMetrics.BinPacking.CPUEfficiency

	// Listas improving / degrading
	// Para recursos: delta < 0 = menos pressão = melhora
	// Para HealthScore e BinPacking: delta > 0 = melhora
	type metricCheck struct {
		name    string
		delta   float64
		higher  bool // true = aumento é bom (HealthScore, BinPacking)
		minDiff float64
	}
	checks := []metricCheck{
		{"health_score", float64(delta.HealthScoreDelta), true, 2},
		{"cpu", delta.CPUDeltaPP, false, 1},
		{"mem", delta.MemDeltaPP, false, 1},
		{"pods", delta.PodsDelta, false, 0.5},
		{"conntrack", delta.ConntrackDeltaPP, false, 0.5},
		{"bin_packing", delta.BinPackingDeltaPP, true, 2},
	}
	for _, c := range checks {
		if c.delta > c.minDiff {
			if c.higher {
				delta.Improving = append(delta.Improving, c.name)
			} else {
				delta.Degrading = append(delta.Degrading, c.name)
			}
		} else if c.delta < -c.minDiff {
			if c.higher {
				delta.Degrading = append(delta.Degrading, c.name)
			} else {
				delta.Improving = append(delta.Improving, c.name)
			}
		}
	}

	// Resumo legível
	daysLabel := fmt.Sprintf("%.0fd", daysSince)
	if daysSince < 1 {
		daysLabel = fmt.Sprintf("%.0fh", daysSince*24)
	}
	parts := []string{}
	if delta.CPUDeltaPP != 0 {
		parts = append(parts, fmt.Sprintf("CPU %+.1fpp", delta.CPUDeltaPP))
	}
	if delta.MemDeltaPP != 0 {
		parts = append(parts, fmt.Sprintf("Mem %+.1fpp", delta.MemDeltaPP))
	}
	if delta.ConntrackDeltaPP != 0 && curr.RawMetrics.ConntrackPool.HasSufficientData {
		parts = append(parts, fmt.Sprintf("conntrack %+.1fpp", delta.ConntrackDeltaPP))
	}
	if delta.HealthScoreDelta != 0 {
		parts = append(parts, fmt.Sprintf("health %+d", delta.HealthScoreDelta))
	}
	if len(parts) > 0 {
		delta.Summary = fmt.Sprintf("Desde análise anterior (há %s): %s", daysLabel, strings.Join(parts, ", "))
	} else {
		delta.Summary = fmt.Sprintf("Sem variação significativa desde análise anterior (há %s)", daysLabel)
	}

	return delta
}

// NodePoolPredictionsHandler gerencia análises preditivas de node pools
type NodePoolPredictionsHandler struct {
	kubeManager   *config.KubeConfigManager
	kubeWrapper   *kubernetes.KubeManager
	analyzer      *ai.Analyzer // usado em getAnalyzerForUser
	tokensStore   *storage.UserTokensStore
	store         *storage.NodePoolPredictionsStore
	defaultConfig *ai.Config
	snatStore     *storage.SNATHistoryStore
}

// NewNodePoolPredictionsHandler cria novo handler
func NewNodePoolPredictionsHandler(
	kubeManager *config.KubeConfigManager,
	kubeWrapper *kubernetes.KubeManager,
	analyzer *ai.Analyzer,
	tokensStore *storage.UserTokensStore,
	store *storage.NodePoolPredictionsStore,
	defaultConfig *ai.Config,
	snatStore *storage.SNATHistoryStore,
) *NodePoolPredictionsHandler {
	return &NodePoolPredictionsHandler{
		kubeManager:   kubeManager,
		kubeWrapper:   kubeWrapper,
		analyzer:      analyzer,
		tokensStore:   tokensStore,
		store:         store,
		defaultConfig: defaultConfig,
		snatStore:     snatStore,
	}
}

// AnalyzeNodePool executa análise preditiva completa de um node pool.
// POST /api/v1/nodepoolpredictions/analyze
func (h *NodePoolPredictionsHandler) AnalyzeNodePool(c *gin.Context) {
	var req np.NodePoolPredictionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Request inválido: " + err.Error()})
		return
	}

	userEmail := c.GetString("user_email")
	req.UserEmail = userEmail

	log.Info().
		Str("cluster", req.Cluster).
		Str("nodepool", req.NodePoolName).
		Str("user", userEmail).
		Msg("Análise preditiva de node pool solicitada")

	ctx := c.Request.Context()

	// 1. Prometheus (opcional — análise funciona sem ele, com dados reduzidos)
	var promClient *prometheus.Client
	if pc, err := h.getNodePoolPrometheusClient(req.Cluster); err != nil {
		log.Warn().Err(err).Msg("Prometheus indisponível para análise de node pool — continuando sem métricas históricas")
	} else if err := pc.TestConnection(ctx); err != nil {
		log.Warn().Err(err).Msg("Prometheus não responde — continuando sem métricas históricas")
	} else {
		promClient = pc
	}

	// 2. Provider IA do usuário
	userAnalyzer := h.getNodePoolAnalyzerForUser(userEmail)
	if userAnalyzer == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Provider de IA não configurado. Configure seus tokens de IA primeiro.",
		})
		return
	}

	aiProvider := userAnalyzer.GetProvider()
	if aiProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Provider de IA não inicializado corretamente. Verifique suas configurações.",
		})
		return
	}

	// 3. Kubernetes client para o cluster
	kubeClient, err := h.kubeManager.GetK8sClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Falha ao conectar ao cluster: " + req.Cluster,
		})
		return
	}

	providerName := aiProvider.GetName()
	modelName := aiProvider.GetModel()

	log.Info().
		Str("user", userEmail).
		Str("provider", providerName).
		Str("model", modelName).
		Str("nodepool", fmt.Sprintf("%s/%s", req.Cluster, req.NodePoolName)).
		Bool("prometheus_available", promClient != nil).
		Msg("Executando análise preditiva de node pool")

	// 4. Enriquecer request com dados do clusters-config.json
	// O collector usa o resource group real do cluster AKS (não o MC_* de infra extraído do providerID)
	if clusterCfg, cfgErr := findClusterInConfig(req.Cluster); cfgErr == nil {
		req.ResourceGroup = clusterCfg.ResourceGroup
		req.Subscription = clusterCfg.Subscription
		req.AzureCluster = strings.TrimSuffix(clusterCfg.ClusterName, "-admin")
		log.Info().
			Str("cluster", req.Cluster).
			Str("resource_group", req.ResourceGroup).
			Str("subscription", req.Subscription).
			Str("azure_cluster", req.AzureCluster).
			Msg("Cluster config enriquecido do clusters-config.json")
	} else {
		log.Warn().Err(cfgErr).Str("cluster", req.Cluster).
			Msg("Cluster não encontrado no clusters-config.json — Azure min/max pode não ser coletado")
	}

	// 4b. Injetar contexto SNAT do histórico (se disponível)
	if h.snatStore != nil {
		if latest, err := h.snatStore.GetLatest(req.Cluster); err == nil && latest != nil && latest.AllocatedOutboundPorts > 0 {
			proj := storage.ComputeSNATProjection(nil, latest.NodesUntilLimit) // projeção sem histórico — só para MaxNodes
			// Obter histórico completo para projeção real
			if records, rErr := h.snatStore.GetRecent(req.Cluster, 30); rErr == nil && len(records) > 0 {
				proj = storage.ComputeSNATProjection(records, latest.NodesUntilLimit)
			}

			status := "ok"
			if latest.UsagePercent >= 95 {
				status = "critical"
			} else if latest.UsagePercent >= 80 {
				status = "warning"
			}

			totalAvailable := latest.OutboundIPCount * 64000
			maxNodes := 0
			if latest.AllocatedOutboundPorts > 0 {
				maxNodes = totalAvailable / latest.AllocatedOutboundPorts
			}
			ipsNeeded := 0
			if latest.AllocatedOutboundPorts > 0 && latest.TotalNodeCount > 0 {
				ipsNeeded = (latest.TotalNodeCount*latest.AllocatedOutboundPorts + 64000 - 1) / 64000
			}

			snatCtx := &np.SNATContextData{
				AllocatedOutboundPorts:   latest.AllocatedOutboundPorts,
				OutboundIPCount:          latest.OutboundIPCount,
				TotalNodeCount:           latest.TotalNodeCount,
				UsagePercent:             latest.UsagePercent,
				NodesUntilLimit:          latest.NodesUntilLimit,
				MaxNodesAllowed:          maxNodes,
				IPsNeededForCurrentNodes: ipsNeeded,
				Status:                   status,
				RecordedAt:               latest.RecordedAt,
			}
			if proj.GrowthPerDay > 0 {
				snatCtx.GrowthPerDay = proj.GrowthPerDay
				snatCtx.DaysUntilSNATLimit = proj.DaysUntilLimit
			}
			req.SNATContext = snatCtx

			log.Info().
				Str("cluster", req.Cluster).
				Float64("snat_usage_pct", latest.UsagePercent).
				Int("nodes_until_limit", latest.NodesUntilLimit).
				Str("snat_status", status).
				Msg("Contexto SNAT injetado na análise preditiva")
		}
	}

	// 5. Executar análise preditiva
	analyzer := np.NewNodePoolAnalyzer(promClient, aiProvider, kubeClient)
	result, err := analyzer.Analyze(ctx, req)
	if err != nil {
		log.Error().Err(err).
			Str("cluster", req.Cluster).
			Str("nodepool", req.NodePoolName).
			Msg("Análise preditiva de node pool falhou")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Análise falhou: " + err.Error(),
		})
		return
	}

	// 5. Persistir no banco de dados
	if h.store != nil {
		riskLevel := result.ExecutiveSummary.RiskLevel
		if riskLevel == "" {
			riskLevel = result.ActionSummary.Status
		}

		if saveErr := h.store.Save(
			result.RequestID,
			result.Cluster,
			result.NodePoolName,
			result.HealthScore.Overall,
			riskLevel,
			result.ActionSummary,
			result.CostAnalysis,
			result,
			providerName,
			modelName,
			result.DurationMs,
			userEmail,
			result.AnalyzedAt,
		); saveErr != nil {
			log.Warn().Err(saveErr).Str("id", result.RequestID).
				Msg("Falha ao salvar análise de node pool no banco — resultado retornado mesmo assim")
		} else {
			log.Info().Str("id", result.RequestID).
				Str("nodepool", result.NodePoolName).
				Msg("Análise de node pool salva no banco")
		}
	}

	// 6. Calcular delta em relação à análise anterior (enriquece o resultado antes de retornar)
	result.Delta = calculateNodePoolDelta(result, h.store)
	if result.Delta != nil {
		log.Info().
			Str("previous_id", result.Delta.PreviousID).
			Float64("days_since", result.Delta.DaysSince).
			Str("summary", result.Delta.Summary).
			Msg("Delta calculado em relação à análise anterior")
	}

	c.JSON(http.StatusOK, result)
}

// GetHistory retorna histórico de análises de node pool com filtros.
// GET /api/v1/nodepoolpredictions/history?cluster=&nodepool=&user_email=&start_date=&end_date=
func (h *NodePoolPredictionsHandler) GetHistory(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusOK, gin.H{"records": []interface{}{}, "total": 0})
		return
	}

	filters := &storage.NodePoolPredictionQueryFilters{
		Cluster:      c.Query("cluster"),
		NodePoolName: c.Query("nodepool"),
		UserEmail:    c.Query("user_email"),
		Limit:        50,
	}

	if start := c.Query("start_date"); start != "" {
		if t, err := time.Parse("2006-01-02", start); err == nil {
			filters.StartDate = &t
		}
	}
	if end := c.Query("end_date"); end != "" {
		if t, err := time.Parse("2006-01-02", end); err == nil {
			endDay := t.Add(24 * time.Hour)
			filters.EndDate = &endDay
		}
	}

	records, err := h.store.List(filters)
	if err != nil {
		log.Error().Err(err).Msg("Falha ao listar histórico de node pool")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"records": records,
		"total":   len(records),
	})
}

// GetMarkdownReport gera e retorna relatório Markdown de uma análise específica.
// GET /api/v1/nodepoolpredictions/report/:id/markdown
func (h *NodePoolPredictionsHandler) GetMarkdownReport(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Store não disponível"})
		return
	}

	id := c.Param("id")
	record, err := h.store.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Análise não encontrada: " + id})
		return
	}

	// Tentar deserializar o resultado completo para gerar relatório rico
	var result np.NodePoolPredictionResult
	if jsonErr := json.Unmarshal([]byte(record.FullResult), &result); jsonErr != nil {
		// Fallback: gerar relatório básico do record
		md := npBasicMarkdownFromRecord(record)
		h.sendMarkdownFile(c, record.NodePoolName, record.AnalyzedAt, md)
		return
	}

	md := generateNodePoolMarkdownReport(&result)
	h.sendMarkdownFile(c, result.NodePoolName, result.AnalyzedAt, md)
}

func (h *NodePoolPredictionsHandler) sendMarkdownFile(c *gin.Context, nodepool string, analyzedAt time.Time, md string) {
	filename := fmt.Sprintf("nodepool_%s_%s.md",
		strings.ReplaceAll(nodepool, "/", "_"),
		analyzedAt.Format("2006-01-02_15-04"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "text/markdown", []byte(md))
}

// ==============================================================================
// Geração de Relatório Markdown
// ==============================================================================

func generateNodePoolMarkdownReport(r *np.NodePoolPredictionResult) string {
	var b strings.Builder

	// ── Cabeçalho ────────────────────────────────────────────────────────────
	b.WriteString("# ANALISE PREDITIVA: NODE POOL " + r.NodePoolName + "\n\n")
	b.WriteString("**Cluster**: " + r.Cluster + "  \n")
	b.WriteString("**Node Pool**: " + r.NodePoolName + "  \n")
	b.WriteString("**VM Size**: " + r.RawMetrics.VMSize + "  \n")
	b.WriteString("**Gerado em**: " + r.AnalyzedAt.Format("02/01/2006 15:04:05") + "  \n")
	b.WriteString(fmt.Sprintf("**Duracao**: %dms  \n", r.DurationMs))
	category := npHealthCategory(r.HealthScore.Overall)
	b.WriteString(fmt.Sprintf("\n**Health Score**: %d/100 — %s\n\n", r.HealthScore.Overall, category))
	b.WriteString("---\n\n")

	// ── Sumário Executivo (topo) ───────────────────────────────────────────
	if r.ExecutiveSummary.CurrentState != "" {
		b.WriteString("## SUMARIO EXECUTIVO\n\n")
		b.WriteString(r.ExecutiveSummary.CurrentState + "\n\n")
		b.WriteString("**Nivel de risco**: " + r.ExecutiveSummary.RiskLevel + "  \n")
		b.WriteString(fmt.Sprintf("**Acao necessaria**: %s\n\n",
			boolStr(r.ExecutiveSummary.ActionRequired, "SIM", "NAO")))
		if len(r.ExecutiveSummary.KeyFindings) > 0 {
			b.WriteString("**Principais achados**:\n\n")
			for _, f := range r.ExecutiveSummary.KeyFindings {
				b.WriteString("- " + f + "\n")
			}
			b.WriteString("\n")
		}
	}

	// ── Resumo de Ação ────────────────────────────────────────────────────
	b.WriteString("## RESUMO DE ACAO\n\n")
	b.WriteString("**Status**: " + r.ActionSummary.Status + "  \n")
	b.WriteString("**Mensagem**: " + r.ActionSummary.StatusMessage + "  \n")
	if r.ActionSummary.HoursToCritical != nil && *r.ActionSummary.HoursToCritical > 0 {
		b.WriteString(fmt.Sprintf("**Horas ate critico**: %d  \n", *r.ActionSummary.HoursToCritical))
		b.WriteString("**Recurso critico**: " + r.ActionSummary.CriticalMetric + "  \n")
		b.WriteString("**Motivo**: " + r.ActionSummary.CriticalReason + "  \n")
	}
	if r.ActionSummary.TopAction != "" {
		b.WriteString("\n**Acao prioritaria**: " + r.ActionSummary.TopAction + "  \n")
		if r.ActionSummary.TopActionCommand != "" {
			b.WriteString("```\n" + r.ActionSummary.TopActionCommand + "\n```\n")
		}
	}
	b.WriteString("\n")

	// ── Health Score Breakdown ────────────────────────────────────────────
	b.WriteString("## HEALTH SCORE BREAKDOWN\n\n")
	hb := r.HealthScore.Breakdown
	b.WriteString("| Componente | Peso | Score |\n")
	b.WriteString("|------------|------|-------|\n")
	b.WriteString(fmt.Sprintf("| Disponibilidade dos Nodes | 25%% | %d |\n", hb.NodeAvailability))
	b.WriteString(fmt.Sprintf("| Headroom de Recursos | 30%% | %d |\n", hb.ResourceHeadroom))
	b.WriteString(fmt.Sprintf("| Densidade de Pods | 20%% | %d |\n", hb.PodDensity))
	b.WriteString(fmt.Sprintf("| Seguranca conntrack | 15%% | %d |\n", hb.ConntrackSafety))
	b.WriteString(fmt.Sprintf("| Saude do Autoscaler | 10%% | %d |\n", hb.AutoscalerHealth))
	b.WriteString(fmt.Sprintf("| **TOTAL** | 100%% | **%d** |\n", r.HealthScore.Overall))
	b.WriteString("\n")

	// ── Estado Atual do Pool ──────────────────────────────────────────────
	b.WriteString("## ESTADO ATUAL DO POOL\n\n")
	b.WriteString(fmt.Sprintf("- Nodes atuais: %d (min: %d, max: %d)\n",
		r.RawMetrics.CurrentNodes, r.RawMetrics.MinNodes, r.RawMetrics.MaxNodes))
	b.WriteString(fmt.Sprintf("- Autoscaler: %s\n", boolStr(r.RawMetrics.AutoscalerEnabled, "Habilitado", "Desabilitado")))
	b.WriteString(fmt.Sprintf("- Nodes com pressao: %d\n", len(r.RawMetrics.NodesWithPressure)))
	b.WriteString("\n")

	// Nodes snapshot (todos)
	if len(r.RawMetrics.NodesSnapshot) > 0 {
		b.WriteString("### Estado por Node\n\n")
		b.WriteString("| Node | CPU% | Mem% | Pods | conntrack% | Disk% | Status |\n")
		b.WriteString("|------|------|------|------|-----------|-------|--------|\n")
		for _, n := range r.RawMetrics.NodesSnapshot {
			b.WriteString(fmt.Sprintf("| %s | %.1f | %.1f | %d | %.1f | %.1f | %s |\n",
				n.NodeName, n.CPUUsagePercent, n.MemUsagePercent,
				n.PodCount, n.ConntrackPercent, n.DiskUsagePercent, n.Status))
		}
		b.WriteString("\n")
	}

	// ── Análise conntrack ─────────────────────────────────────────────────
	if r.RawMetrics.ConntrackPool.HasSufficientData {
		b.WriteString("## ANALISE CONNTRACK\n\n")
		cp := r.RawMetrics.ConntrackPool
		b.WriteString(fmt.Sprintf("- Uso medio do pool: %.1f%%\n", cp.AvgUsage))
		b.WriteString(fmt.Sprintf("- Node mais saturado: %s (%.1f%%)\n", cp.HighestNode, cp.MaxUsage))
		b.WriteString(fmt.Sprintf("- Nodes em warning (>70%%): %d\n", cp.NodesWarning))
		b.WriteString(fmt.Sprintf("- Nodes criticos (>85%%): %d\n", cp.NodesCritical))
		if cp.AvgGrowthRatePerH > 0 {
			b.WriteString(fmt.Sprintf("- Taxa de crescimento: %.0f entries/hora\n", cp.AvgGrowthRatePerH))
		}
		b.WriteString("\n")

		// Tabela por node
		if len(r.RawMetrics.ConntrackPerNode) > 0 {
			b.WriteString("### conntrack por Node\n\n")
			b.WriteString("| Node | Entries | Limit | Uso% | Status |\n")
			b.WriteString("|------|---------|-------|------|--------|\n")
			for _, cn := range r.RawMetrics.ConntrackPerNode {
				b.WriteString(fmt.Sprintf("| %s | %d | %d | %.1f | %s |\n",
					cn.NodeName, cn.CurrentEntries, cn.MaxEntries,
					cn.UsagePercent, cn.Status))
			}
			b.WriteString("\n")
		}
	}

	// ── Tendências ────────────────────────────────────────────────────────
	b.WriteString("## TENDENCIAS (por node, normalizadas)\n\n")
	b.WriteString("| Metrica | D-0 | D-3 | D-7 | D-14 | Tendencia |\n")
	b.WriteString("|---------|-----|-----|-----|------|----------|\n")
	cpuSnapshots := r.RawMetrics.CPUTrendPerNode
	if len(cpuSnapshots) > 0 {
		d0 := npTrendVal(cpuSnapshots, 0)
		d3 := npTrendVal(cpuSnapshots, 3)
		d7 := npTrendVal(cpuSnapshots, 7)
		d14 := npTrendVal(cpuSnapshots, 14)
		b.WriteString(fmt.Sprintf("| CPU%% | %.1f | %.1f | %.1f | %.1f | %s |\n",
			d0, d3, d7, d14, string(r.Trends.CPUTrend)))
	}
	memSnapshots := r.RawMetrics.MemTrendPerNode
	if len(memSnapshots) > 0 {
		d0 := npTrendVal(memSnapshots, 0)
		d3 := npTrendVal(memSnapshots, 3)
		d7 := npTrendVal(memSnapshots, 7)
		d14 := npTrendVal(memSnapshots, 14)
		b.WriteString(fmt.Sprintf("| Mem%% | %.1f | %.1f | %.1f | %.1f | %s |\n",
			d0, d3, d7, d14, string(r.Trends.MemTrend)))
	}
	podSnapshots := r.RawMetrics.PodsTrendPerNode
	if len(podSnapshots) > 0 {
		d0 := npTrendVal(podSnapshots, 0)
		d3 := npTrendVal(podSnapshots, 3)
		d7 := npTrendVal(podSnapshots, 7)
		d14 := npTrendVal(podSnapshots, 14)
		b.WriteString(fmt.Sprintf("| Pods/node | %.1f | %.1f | %.1f | %.1f | %s |\n",
			d0, d3, d7, d14, string(r.Trends.PodsTrend)))
	}
	b.WriteString("\n")

	// ── Timeline de Saturação ────────────────────────────────────────────
	tl := r.SaturationTimeline
	if len(tl.Forecasts) > 0 {
		b.WriteString("## TIMELINE DE SATURACAO\n\n")
		if tl.Summary != "" {
			b.WriteString("> " + tl.Summary + "\n\n")
		}
		b.WriteString("| Metrica | Valor Atual | Threshold | Crescimento/Dia | Dias | Data Estimada | Urgencia | Confianca |\n")
		b.WriteString("|---------|------------|-----------|----------------|------|--------------|----------|----------|\n")
		for _, f := range tl.Forecasts {
			daysStr := "—"
			dateStr := "—"
			if f.DaysUntilSaturation != nil {
				daysStr = fmt.Sprintf("%.0f", *f.DaysUntilSaturation)
				if f.EstimatedDate != nil {
					dateStr = f.EstimatedDate.Format("02/01/2006")
				}
			}
			nodeInfo := ""
			if f.AffectedNode != "" {
				nodeInfo = " (" + f.AffectedNode + ")"
			}
			b.WriteString(fmt.Sprintf("| %s%s | %.1f%% | %.0f%% | %.2f%%/dia | %s | %s | %s | %s |\n",
				f.Metric, nodeInfo,
				f.CurrentValue, f.Threshold,
				f.DailyGrowthRate,
				daysStr, dateStr,
				f.UrgencyBadge, f.Confidence))
		}
		b.WriteString("\n")
	}

	// ── Histórico do Autoscaler ───────────────────────────────────────────
	if len(r.RawMetrics.AutoscalerEvents) > 0 {
		b.WriteString("## HISTORICO DO AUTOSCALER\n\n")
		b.WriteString("| Data/Hora | Tipo | Delta | Motivo |\n")
		b.WriteString("|-----------|------|-------|--------|\n")
		events := r.RawMetrics.AutoscalerEvents
		if len(events) > 10 {
			events = events[:10]
		}
		for _, e := range events {
			delta := fmt.Sprintf("%+d", e.NodesDelta)
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				e.Timestamp.Format("02/01 15:04"), e.Type, delta, e.Reason))
		}
		b.WriteString("\n")
	}

	// ── Bin Packing ───────────────────────────────────────────────────────
	bp := r.RawMetrics.BinPacking
	b.WriteString("## ANALISE DE BIN PACKING\n\n")
	b.WriteString(fmt.Sprintf("- Eficiencia CPU: %.1f%%\n", bp.CPUEfficiency))
	b.WriteString(fmt.Sprintf("- Eficiencia Memoria: %.1f%%\n", bp.MemEfficiency))
	b.WriteString(fmt.Sprintf("- Nivel de Fragmentacao: %s\n", bp.FragmentationLevel))
	b.WriteString(fmt.Sprintf("- Candidatos a scale-in: %d nodes\n", bp.ScaleInCandidates))
	b.WriteString(fmt.Sprintf("- Scale-in seguro: %s\n", boolStr(bp.ScaleInSafe, "Sim", "Nao")))
	if bp.WastedResources != "" {
		b.WriteString(fmt.Sprintf("- Recursos desperdicados: %s\n", bp.WastedResources))
	}
	b.WriteString("\n")

	// ── HPAs do Pool ──────────────────────────────────────────────────────
	if len(r.RawMetrics.HPACorrelation) > 0 {
		b.WriteString("## HPAs COM PODS NESTE POOL\n\n")
		atMax := 0
		for _, hpa := range r.RawMetrics.HPACorrelation {
			if hpa.AtMax {
				atMax++
			}
		}
		b.WriteString(fmt.Sprintf("Total de HPAs correlacionados: %d (%d no limite maximo)\n\n",
			len(r.RawMetrics.HPACorrelation), atMax))
		b.WriteString(fmt.Sprintf("| HPA | Namespace | Target | Replicas | Max | Em Limite | Pods no Pool |\n"))
		b.WriteString(fmt.Sprintf("|-----|-----------|--------|----------|-----|-----------|-------------|\n"))
		for _, hpa := range r.RawMetrics.HPACorrelation {
			limiteStr := "Nao"
			if hpa.AtMax {
				limiteStr = "*** SIM ***"
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %d | %s | %d/%d |\n",
				hpa.HPAName, hpa.Namespace, hpa.TargetName,
				hpa.CurrentReplicas, hpa.MaxReplicas,
				limiteStr, hpa.PodsOnPool, hpa.TotalPods))
		}
		b.WriteString("\n")
	}

	// ── Crescimento de Disco Efêmero ──────────────────────────────────────
	if dg := r.RawMetrics.DiskGrowth; dg.HasData {
		b.WriteString("## CRESCIMENTO DE DISCO EFEMERO\n\n")
		if dg.MaxGrowthPctDay > 0 {
			b.WriteString(fmt.Sprintf("- Node mais critico: **%s**\n", dg.FastestNode))
			b.WriteString(fmt.Sprintf("- Taxa de crescimento: %.3f%%/dia\n", dg.MaxGrowthPctDay))
			b.WriteString(fmt.Sprintf("- Uso atual (max): %.1f%%\n", dg.MaxUsagePct))
			if dg.MinDaysUntilFull > 0 {
				b.WriteString(fmt.Sprintf("- **Dias ate cheio (estimativa): %.0f dias**\n", dg.MinDaysUntilFull))
			}
			if dg.NodesWarning > 0 || dg.NodesCritical > 0 {
				b.WriteString(fmt.Sprintf("- Nodes em alerta (>70%%): %d | Nodes criticos (>85%%): %d\n",
					dg.NodesWarning, dg.NodesCritical))
			}
		} else {
			b.WriteString("- Sem crescimento detectado (disco estavel)\n")
		}
		b.WriteString("\n")
	}

	// ── Análise de Custo ──────────────────────────────────────────────────
	if r.CostAnalysis != nil {
		ca := r.CostAnalysis
		b.WriteString("## ANALISE DE CUSTO\n\n")
		b.WriteString(fmt.Sprintf("- VM Size: %s (%d vCPUs, %d GB RAM)\n",
			ca.VMSize, ca.VMCPUCores, ca.VMMemoryGB))
		b.WriteString(fmt.Sprintf("- Custo por node: USD %.2f/hora | USD %.2f/mes\n",
			ca.CostPerNodePerHour, ca.CostPerNodeMonthly))
		b.WriteString(fmt.Sprintf("- **Custo atual (%d nodes): USD %.2f/mes | BRL %.2f/mes**\n",
			r.RawMetrics.CurrentNodes, ca.CurrentMonthlyCostUSD, ca.CurrentMonthlyCostBRL))
		b.WriteString(fmt.Sprintf("- Custo maximo (%d nodes): USD %.2f/mes\n",
			r.RawMetrics.MaxNodes, ca.MaxMonthlyCostUSD))
		if ca.IdleWastePercent > 0 {
			b.WriteString(fmt.Sprintf("- Desperdicio (nodes ociosos): %.0f%% | USD %.2f/mes\n",
				ca.IdleWastePercent, ca.WasteMonthlyCostUSD))
		}
		if ca.MonthlySavingsUSD > 0 {
			b.WriteString(fmt.Sprintf("- Nodes recomendados (P95): %d\n", ca.RecommendedNodes))
			b.WriteString(fmt.Sprintf("- **Economia potencial: USD %.2f/mes | BRL %.2f/mes (%.1f%%)**\n",
				ca.MonthlySavingsUSD, ca.MonthlySavingsBRL, ca.SavingsPercent))
			b.WriteString(fmt.Sprintf("- Economia anual: USD %.2f | BRL %.2f\n",
				ca.AnnualSavingsUSD, ca.AnnualSavingsBRL))
		}
		if len(ca.Recommendations) > 0 {
			b.WriteString("\n### Recomendacoes de Custo\n\n")
			for i, rec := range ca.Recommendations {
				b.WriteString(fmt.Sprintf("%d. **%s**  \n", i+1, rec.Title))
				b.WriteString("   " + rec.Description + "  \n")
				b.WriteString(fmt.Sprintf("   Economia: USD %.2f/mes | BRL %.2f/mes  \n",
					rec.SavingsUSD, rec.SavingsBRL))
				b.WriteString(fmt.Sprintf("   Impacto: %s | Acao: `%s`\n\n", rec.Impact, rec.Action))
			}
		}
		if len(ca.SKUAlternatives) > 0 {
			b.WriteString("\n### VMs Alternativas (baseado em consumo historico P95)\n\n")
			b.WriteString("| VM SKU | vCPUs | RAM (GB) | Custo/Node/Mes | Pool/Mes (atual N nodes) | Economia | Justificativa |\n")
			b.WriteString("|--------|-------|----------|----------------|--------------------------|----------|---------------|\n")
			for _, alt := range ca.SKUAlternatives {
				economiaStr := fmt.Sprintf("USD %.2f (%.1f%%)", alt.SavingsUSD, alt.SavingsPercent)
				if alt.SavingsUSD < 0 {
					economiaStr = fmt.Sprintf("+USD %.2f (%.1f%% mais caro)", -alt.SavingsUSD, -alt.SavingsPercent)
				}
				b.WriteString(fmt.Sprintf("| %s | %d | %d | USD %.2f | USD %.2f | %s | %s |\n",
					alt.VMSize, alt.VMCPUCores, alt.VMMemoryGB,
					alt.CostPerNodeMonthly, alt.PoolMonthlyCostUSD,
					economiaStr, alt.Rationale))
			}
			b.WriteString("\n")
		}
	}

	// ── Previsões IA ──────────────────────────────────────────────────────
	b.WriteString("## PREVISOES (IA)\n\n")
	allPreds := []struct {
		label string
		preds []np.NodePoolPrediction
	}{
		{"Curto Prazo", r.Predictions.ShortTerm},
		{"Medio Prazo", r.Predictions.MediumTerm},
		{"Longo Prazo", r.Predictions.LongTerm},
	}
	for _, pg := range allPreds {
		if len(pg.preds) == 0 {
			continue
		}
		p := pg.preds[0]
		b.WriteString(fmt.Sprintf("### %s (%s)\n\n", pg.label, p.Timeframe))
		b.WriteString(p.Event + "\n")
		b.WriteString(fmt.Sprintf("Confianca: %.0f%%  |  Severidade: %s\n\n",
			p.ConfidencePercent, p.Severity))
	}

	// ── Recomendações Gerais ──────────────────────────────────────────────
	if len(r.Recommendations) > 0 {
		b.WriteString("## RECOMENDACOES\n\n")
		for i, rec := range r.Recommendations {
			b.WriteString(fmt.Sprintf("%d. **[%s]** %s  \n", i+1, strings.ToUpper(rec.Category), rec.Title))
			b.WriteString("   " + rec.Description + "\n")
			if len(rec.Actions) > 0 {
				b.WriteString("   **Acoes**:\n")
				for _, a := range rec.Actions {
					b.WriteString("   - `" + a + "`\n")
				}
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*Relatorio gerado em %s | Analise ID: %s*\n",
		r.AnalyzedAt.Format("02/01/2006 15:04:05"), r.RequestID))

	return b.String()
}

// npBasicMarkdownFromRecord gera relatório mínimo a partir do record quando
// não é possível deserializar o FullResult.
func npBasicMarkdownFromRecord(r *storage.NodePoolPredictionRecord) string {
	var b strings.Builder
	b.WriteString("# ANALISE PREDITIVA: NODE POOL " + r.NodePoolName + "\n\n")
	b.WriteString("**Cluster**: " + r.Cluster + "  \n")
	b.WriteString("**Gerado em**: " + r.AnalyzedAt.Format("02/01/2006 15:04:05") + "  \n")
	b.WriteString(fmt.Sprintf("**Health Score**: %d/100 — %s\n\n", r.HealthScore, npHealthCategory(r.HealthScore)))
	b.WriteString("**Nivel de risco**: " + r.RiskLevel + "\n\n")
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*ID: %s*\n", r.ID))
	return b.String()
}

// ==============================================================================
// Helpers internos (reutilizam lógica do PredictionsHandler sem duplicar código)
// ==============================================================================

// getNodePoolPrometheusClient descobre e conecta ao Prometheus do cluster
func (h *NodePoolPredictionsHandler) getNodePoolPrometheusClient(cluster string) (*prometheus.Client, error) {
	endpoint, err := discovery.DiscoverEndpoint(cluster)
	if err != nil {
		return nil, err
	}
	return prometheus.NewClient(cluster, endpoint.URL, endpoint.RequiresGCPAuth)
}

// getNodePoolAnalyzerForUser retorna o analyzer AI configurado pelo usuário,
// com fallback ao analyzer padrão do servidor.
func (h *NodePoolPredictionsHandler) getNodePoolAnalyzerForUser(userEmail string) *ai.Analyzer {
	if userEmail == "" || h.tokensStore == nil || h.analyzer == nil {
		return h.analyzer
	}

	tokens, err := h.tokensStore.GetTokens(userEmail)
	if err != nil || tokens == nil {
		return h.analyzer
	}

	cfg := &ai.Config{
		Provider: tokens.PreferredProvider,
		Timeout:  h.defaultConfig.Timeout,
	}

	switch tokens.PreferredProvider {
	case "gemini":
		if tokens.GeminiAPIKey == "" {
			return h.analyzer
		}
		cfg.GeminiAPIKey = tokens.GeminiAPIKey
		cfg.GeminiModel = tokens.GeminiModel
		if cfg.GeminiModel == "" {
			cfg.GeminiModel = "gemini-2.5-flash"
		}
	case "claude":
		if tokens.ClaudeAPIKey == "" {
			return h.analyzer
		}
		cfg.ClaudeAPIKey = tokens.ClaudeAPIKey
		cfg.ClaudeModel = tokens.ClaudeModel
		if cfg.ClaudeModel == "" {
			cfg.ClaudeModel = "claude-3-5-sonnet-20241022"
		}
	case "openai":
		if tokens.OpenAIAPIKey == "" {
			return h.analyzer
		}
		cfg.OpenAIAPIKey = tokens.OpenAIAPIKey
		cfg.OpenAIModel = tokens.OpenAIModel
		cfg.OpenAIBaseURL = tokens.OpenAIBaseURL
		if cfg.OpenAIModel == "" {
			cfg.OpenAIModel = "gpt-4o-mini"
		}
	case "ollama":
		cfg.OllamaBaseURL = h.defaultConfig.OllamaBaseURL
		cfg.OllamaModel = tokens.OllamaModel
		if cfg.OllamaModel == "" {
			cfg.OllamaModel = "llama3.2:3b"
		}
	default:
		return h.analyzer
	}

	provider, err := ai.NewProvider(cfg)
	if err != nil {
		log.Warn().Err(err).Str("user", userEmail).Msg("Falha ao criar provider personalizado — usando padrão")
		return h.analyzer
	}

	return ai.NewAnalyzer(provider, h.kubeWrapper, nil)
}

// ==============================================================================
// Funções utilitárias para formatação de relatório
// ==============================================================================

func npHealthCategory(score int) string {
	switch {
	case score >= 75:
		return "SAUDAVEL"
	case score >= 50:
		return "ATENCAO"
	default:
		return "CRITICO"
	}
}

func boolStr(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

func npTrendVal(snapshots []np.TrendSnapshot, daysAgo int) float64 {
	for _, s := range snapshots {
		if s.DaysAgo == daysAgo {
			return s.ValuePerNode
		}
	}
	return 0
}
