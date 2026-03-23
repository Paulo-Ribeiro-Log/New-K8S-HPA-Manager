package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	dtclient "k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/sanitizer"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// DynatraceHandler gerencia integração com Dynatrace para análise de problemas
type DynatraceHandler struct {
	tokensStore  *storage.UserTokensStore
	historyStore *storage.AIHistoryStore
	aiHandler    *AIDiagnosticsHandler
	sanitizer    *sanitizer.Sanitizer
}

// NewDynatraceHandler cria o handler
func NewDynatraceHandler(tokensStore *storage.UserTokensStore, historyStore *storage.AIHistoryStore, aiHandler *AIDiagnosticsHandler) *DynatraceHandler {
	return &DynatraceHandler{
		tokensStore:  tokensStore,
		historyStore: historyStore,
		aiHandler:    aiHandler,
		sanitizer:    sanitizer.New(),
	}
}

// clientForUser cria um cliente Dynatrace para o usuário.
// Usa tokens salvos; fallback para env vars DT_API_URL e DT_API_TOKEN (service account futuro).
func (h *DynatraceHandler) clientForUser(aiEmail string) (*dtclient.Client, error) {
	var dtURL, dtToken string

	if aiEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(aiEmail)
		if err == nil && tokens != nil {
			dtURL = tokens.DynatraceURL
			dtToken = tokens.DynatraceToken
		}
	}

	return dtclient.NewClient(dtURL, dtToken)
}

// ─── GET /api/v1/dynatrace/config ─────────────────────────────────────────────

// GetConfig retorna configuração atual sem expor o token
func (h *DynatraceHandler) GetConfig(c *gin.Context) {
	aiEmail := c.Query("ai_email")

	var dtURL, tagFilter string
	hasToken := false

	if aiEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(aiEmail)
		if err == nil && tokens != nil {
			dtURL = tokens.DynatraceURL
			hasToken = tokens.DynatraceToken != ""
			tagFilter = tokens.DynatraceTagFilter
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url":   dtURL,
		"has_token":  hasToken,
		"enabled":    dtURL != "" && hasToken,
		"tag_filter": tagFilter,
	})
}

// ─── POST /api/v1/dynatrace/test ──────────────────────────────────────────────

// TestConnection testa conectividade com a API Dynatrace
func (h *DynatraceHandler) TestConnection(c *gin.Context) {
	var req struct {
		AIEmail string `json:"ai_email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	client, err := h.clientForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	latency, err := client.TestConnection(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"latency_ms": latency,
		"base_url":   client.BaseURL(),
	})
}

// ─── GET /api/v1/dynatrace/problems ───────────────────────────────────────────

// ListProblems retorna problems OPEN com correlação K8s extraída do OneAgent
func (h *DynatraceHandler) ListProblems(c *gin.Context) {
	aiEmail := c.Query("ai_email")

	client, err := h.clientForUser(aiEmail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"problems":          []interface{}{},
			"dt_not_configured": true,
			"message":           err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// Filtro apenas quando o usuário passa explicitamente via query param na UI
	tagFilter := c.Query("filter")
	// status: "OPEN" (padrão), "CLOSED" ou "ALL"
	statusFilter := c.Query("status")

	result, err := client.GetOpenProblems(ctx, tagFilter, statusFilter)
	if err != nil {
		log.Error().Err(err).Str("ai_email", aiEmail).Msg("Dynatrace: falha ao buscar problems")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	summaries := make([]dtclient.ProblemSummary, 0, len(result.Problems))
	for _, p := range result.Problems {
		// Enriquecer affected + impacted com displayName e correlação K8s
		enrichedAffected := client.EnrichEntitiesWithK8s(ctx, p.AffectedEntities)
		enrichedImpacted := client.EnrichEntitiesWithK8s(ctx, p.ImpactedEntities)

		// Enriquecer rootCauseEntity separadamente
		var rootCause *dtclient.EntityStub
		if p.RootCauseEntity != nil {
			enriched := client.EnrichStub(ctx, *p.RootCauseEntity)
			rootCause = &enriched
		}

		// Deduplica correlações K8s únicas (de affected + impacted)
		// Inclui AppName, AppVersion, GitHubRepoID para correlação com deployments e GitHub releases
		k8sMap := make(map[string]dtclient.K8sCorrelation)
		for _, e := range append(enrichedAffected, enrichedImpacted...) {
			workload := e.K8sWorkload
			ns := e.K8sNamespace
			// Fallback: usar Labels quando K8s básico não extraiu
			if workload == "" && e.Labels != nil && e.Labels.AppName != "" {
				workload = e.Labels.AppName
			}
			if ns == "" && e.Labels != nil && e.Labels.Namespace != "" {
				ns = e.Labels.Namespace
			}
			if workload == "" && ns == "" {
				continue
			}
			key := e.K8sCluster + "/" + ns + "/" + workload
			corr := dtclient.K8sCorrelation{
				Cluster:   e.K8sCluster,
				Namespace: ns,
				Workload:  workload,
			}
			if e.Labels != nil {
				corr.AppName = e.Labels.AppName
				corr.AppVersion = e.Labels.AppVersion
				corr.GitHubRepoID = e.Labels.GitHubRepoID
				corr.Environment = e.Labels.AppEnvironment
			}
			k8sMap[key] = corr
		}
		k8sWorkloads := make([]dtclient.K8sCorrelation, 0, len(k8sMap))
		for _, v := range k8sMap {
			k8sWorkloads = append(k8sWorkloads, v)
		}

		summaries = append(summaries, dtclient.ProblemSummary{
			ProblemID:        p.ProblemID,
			DisplayID:        p.DisplayID,
			Title:            p.Title,
			Status:           p.Status,
			SeverityLevel:    p.SeverityLevel,
			ImpactLevel:      p.ImpactLevel,
			StartTime:        p.StartTime,
			EndTime:          p.EndTime,
			AffectedEntities: enrichedAffected,
			ImpactedEntities: enrichedImpacted,
			RootCauseEntity:  rootCause,
			ManagementZones:  p.ManagementZones,
			K8sWorkloads:     k8sWorkloads,
		})
	}

	log.Info().
		Str("ai_email", aiEmail).
		Int("returned", len(summaries)).
		Int("dt_total", result.TotalCount).
		Str("filter", tagFilter).
		Msg("Dynatrace: problems carregados")

	// UI usa apps.dynatrace.com; API usa live.dynatrace.com
	uiBaseURL := strings.ReplaceAll(client.BaseURL(), ".live.dynatrace.com", ".apps.dynatrace.com")

	c.JSON(http.StatusOK, gin.H{
		"problems":    summaries,
		"total":       len(summaries),
		"dt_total":    result.TotalCount,
		"fetched_at":  time.Now(),
		"ui_base_url": uiBaseURL,
	})
}

// ─── GET /api/v1/dynatrace/problems/:problemId ────────────────────────────────

// GetProblem retorna detalhes de um problem específico
func (h *DynatraceHandler) GetProblem(c *gin.Context) {
	problemID := c.Param("problemId")
	aiEmail := c.Query("ai_email")

	client, err := h.clientForUser(aiEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	problem, err := client.GetProblem(ctx, problemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	problem.AffectedEntities = client.EnrichEntitiesWithK8s(ctx, problem.AffectedEntities)
	c.JSON(http.StatusOK, problem)
}

// ─── POST /api/v1/dynatrace/problems/:problemId/analyze ───────────────────────

// AnalyzeProblem monta contexto completo (Dynatrace + K8s) e envia para AI
func (h *DynatraceHandler) AnalyzeProblem(c *gin.Context) {
	problemID := c.Param("problemId")

	var req struct {
		AIEmail string `json:"ai_email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AIEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ai_email é obrigatório"})
		return
	}
	if h.aiHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI não configurado neste servidor"})
		return
	}

	dtClient, err := h.clientForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	// 1. Buscar detalhes do problem
	problem, err := dtClient.GetProblem(ctx, problemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar problem: " + err.Error()})
		return
	}

	// 2. Enriquecer entidades com correlação K8s
	problem.AffectedEntities = dtClient.EnrichEntitiesWithK8s(ctx, problem.AffectedEntities)

	// 3. Usar GetEntityMetricsForProblem — mesma coleta rica da aba "Métricas"
	//    (P50/P90/P95/P99, error_rate, throughput, DB calls, pods, CPU throttling…)
	metricsResponse := dtClient.GetEntityMetricsForProblem(ctx, problem)

	// 4. Coletar eventos das entidades afetadas (até 5 entidades, 10 eventos cada)
	fromTime := problem.StartTime.Add(-10 * time.Minute)
	toTime := problem.StartTime.Add(2 * time.Hour)
	if problem.EndTime != nil {
		toTime = problem.EndTime.Add(15 * time.Minute)
	}
	entityEvents := make(map[string][]dtclient.Event)
	evLimit := 5
	if len(problem.AffectedEntities) < evLimit {
		evLimit = len(problem.AffectedEntities)
	}
	for i := 0; i < evLimit; i++ {
		id := problem.AffectedEntities[i].EntityID.ID
		if evs, err := dtClient.GetEntityEvents(ctx, id, fromTime, toTime); err == nil {
			entityEvents[id] = evs
		}
	}

	// 5. Gerar itens de ação determinísticos por threshold de métricas
	actionItems := generateActionItems(problem, metricsResponse)

	// 6. Montar e sanitizar prompt
	prompt := buildDynatracePrompt(problem, metricsResponse, entityEvents)
	prompt = h.sanitizer.SanitizeText(prompt)

	// 5. Obter provider AI e analisar
	provider, err := h.aiHandler.GetProviderForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	analysis, err := provider.Analyze(ctx, prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha na análise AI: " + err.Error()})
		return
	}

	log.Info().
		Str("ai_email", req.AIEmail).
		Str("problem_id", problemID).
		Str("title", problem.Title).
		Msg("Dynatrace: análise AI concluída")

	analyzedAt := time.Now()

	// Salvar análise no histórico (mesmo store do AI Diagnostics)
	if h.historyStore != nil {
		mzNames := make([]string, 0, len(problem.ManagementZones))
		for _, mz := range problem.ManagementZones {
			mzNames = append(mzNames, mz.Name)
		}
		metaJSON, _ := json.Marshal(map[string]interface{}{
			"display_id":       problem.DisplayID,
			"severity":         problem.SeverityLevel,
			"impact_level":     problem.ImpactLevel,
			"management_zones": mzNames,
			"start_time":       problem.StartTime,
		})
		record := &storage.HistoryRecord{
			ID:           uuid.New().String(),
			ResourceType: "dynatrace-problem",
			Cluster:      "dynatrace",
			Namespace:    strings.Join(mzNames, ","),
			ResourceName: problem.DisplayID + " — " + problem.Title,
			Provider:     "dynatrace-ai",
			Analysis:     analysis,
			Suggestions:  "[]",
			AnalyzedAt:   analyzedAt,
			UserEmail:    req.AIEmail,
		}
		record.ResourceMetadata.Valid = true
		record.ResourceMetadata.String = string(metaJSON)
		if err := h.historyStore.Save(record); err != nil {
			log.Warn().Err(err).Str("problem_id", problemID).Msg("Dynatrace: falha ao salvar análise no histórico")
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"problem_id":   problemID,
		"title":        problem.Title,
		"severity":     problem.SeverityLevel,
		"analysis":     analysis,
		"action_items": actionItems,
		"analyzed_at":  analyzedAt,
	})
}

// ─── GET /api/v1/dynatrace/problems/:problemId/metrics ───────────────────────

// GetProblemMetrics coleta métricas de performance de todas as entidades do problem.
// Janela: 30min antes do início até agora (ou fim + 15min se já resolvido).
func (h *DynatraceHandler) GetProblemMetrics(c *gin.Context) {
	problemID := c.Param("problemId")
	aiEmail := c.Query("ai_email")

	client, err := h.clientForUser(aiEmail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "not_configured": true})
		return
	}

	// Timeout generoso: múltiplas entidades × múltiplas métricas em paralelo
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	problem, err := client.GetProblem(ctx, problemID)
	if err != nil {
		log.Error().Err(err).Str("problem_id", problemID).Msg("Dynatrace: falha ao buscar problem para métricas")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Enriquecer entidades com displayName e correlação K8s
	problem.AffectedEntities = client.EnrichEntitiesWithK8s(ctx, problem.AffectedEntities)
	problem.ImpactedEntities = client.EnrichEntitiesWithK8s(ctx, problem.ImpactedEntities)
	if problem.RootCauseEntity != nil {
		enriched := client.EnrichStub(ctx, *problem.RootCauseEntity)
		problem.RootCauseEntity = &enriched
	}

	response := client.GetEntityMetricsForProblem(ctx, problem)

	log.Info().
		Str("problem_id", problemID).
		Int("entities", len(response.Entities)).
		Msg("Dynatrace: métricas coletadas")

	c.JSON(http.StatusOK, response)
}

// ─── GET /api/v1/dynatrace/problems/:problemId/context ───────────────────────

// GetProblemContext coleta evidências Davis AI, eventos, topologia e traces distribuídos.
func (h *DynatraceHandler) GetProblemContext(c *gin.Context) {
	problemID := c.Param("problemId")
	aiEmail := c.Query("ai_email")

	client, err := h.clientForUser(aiEmail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "not_configured": true})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	problem, err := client.GetProblem(ctx, problemID)
	if err != nil {
		log.Error().Err(err).Str("problem_id", problemID).Msg("Dynatrace: falha ao buscar problem para contexto")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Enriquecer entidades para nomes legíveis
	problem.AffectedEntities = client.EnrichEntitiesWithK8s(ctx, problem.AffectedEntities)
	if problem.RootCauseEntity != nil {
		enriched := client.EnrichStub(ctx, *problem.RootCauseEntity)
		problem.RootCauseEntity = &enriched
	}

	ctx2, cancel2 := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel2()

	pctx := client.GetProblemContext(ctx2, problem)

	log.Info().
		Str("problem_id", problemID).
		Int("evidence", len(pctx.Evidence)).
		Int("events", len(pctx.Events)).
		Int("topology", len(pctx.Topology)).
		Int("traces", len(pctx.Traces)).
		Msg("Dynatrace: contexto coletado")

	c.JSON(http.StatusOK, pctx)
}

// ─── GET /api/v1/dynatrace/history ────────────────────────────────────────────

// GetHistory retorna histórico de análises Dynatrace do usuário
func (h *DynatraceHandler) GetHistory(c *gin.Context) {
	aiEmail := c.Query("ai_email")
	if h.historyStore == nil {
		c.JSON(http.StatusOK, gin.H{"records": []interface{}{}, "total": 0})
		return
	}

	records, err := h.historyStore.Query(&storage.QueryFilters{
		ResourceType: "dynatrace-problem",
		UserEmail:    aiEmail,
		Limit:        50,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"records": records,
		"total":   len(records),
	})
}

// pointStats calcula min/avg/max/último de uma série MetricPoint
func pointStats(points []dtclient.MetricPoint) (min, avg, max, last float64, ok bool) {
	valid := make([]float64, 0, len(points))
	for _, p := range points {
		if !math.IsNaN(p.V) && p.V >= 0 {
			valid = append(valid, p.V)
		}
	}
	if len(valid) == 0 {
		return 0, 0, 0, 0, false
	}
	min, max, sum := valid[0], valid[0], 0.0
	for _, v := range valid {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	return min, sum / float64(len(valid)), max, valid[len(valid)-1], true
}

// severity avalia o nível de alerta de uma métrica pelo valor máximo
func severityLabel(key string, maxVal float64) string {
	switch key {
	case "error_rate":
		if maxVal > 20 {
			return "🔴 CRÍTICO"
		} else if maxVal > 5 {
			return "🟡 ATENÇÃO"
		}
		return "🟢 normal"
	case "response_p90", "response_p95", "response_p99":
		if maxVal > 5000 {
			return "🔴 CRÍTICO"
		} else if maxVal > 1000 {
			return "🟡 ATENÇÃO"
		}
		return "🟢 normal"
	case "pod_restarts":
		if maxVal > 5 {
			return "🔴 CRÍTICO"
		} else if maxVal > 1 {
			return "🟡 ATENÇÃO"
		}
		return "🟢 normal"
	case "pods_ready_pct":
		if maxVal < 80 {
			return "🔴 CRÍTICO"
		} else if maxVal < 95 {
			return "🟡 ATENÇÃO"
		}
		return "🟢 normal"
	case "cpu_throttle":
		if maxVal > 500 {
			return "🔴 CRÍTICO"
		} else if maxVal > 100 {
			return "🟡 ATENÇÃO"
		}
		return "🟢 normal"
	}
	return ""
}

// ActionItem ação corretiva determinística gerada por threshold de métricas do Dynatrace.
// Exibida antes da análise de IA para guiar o usuário nas seções corretas do HPA Manager.
type ActionItem struct {
	Urgency    string `json:"urgency"`     // "IMEDIATA" | "ALTA" | "MONITORAR"
	AppSection string `json:"app_section"` // "HPA" | "Deployments" | "Resource Explorer" | "Health Check"
	Workload   string `json:"workload,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Cluster    string `json:"cluster,omitempty"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
}

// generateActionItems gera ações corretivas determinísticas baseadas em thresholds de métricas.
// Não usa IA — lê os valores reais das métricas e decide qual seção do HPA Manager o usuário deve acessar.
func generateActionItems(problem *dtclient.Problem, mr *dtclient.ProblemMetricsResponse) []ActionItem {
	items := []ActionItem{}
	seen := map[string]struct{}{}

	add := func(item ActionItem) {
		key := item.AppSection + "|" + item.Namespace + "/" + item.Workload + "|" + item.Urgency + "|" + item.Action
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			items = append(items, item)
		}
	}

	for _, ed := range mr.Entities {
		if len(ed.Metrics) == 0 {
			continue
		}
		// Busca contexto K8s da entidade nas stubs do problem
		var ns, workload, cluster string
		for _, stub := range problem.AffectedEntities {
			if stub.EntityID.ID == ed.EntityID {
				ns, workload, cluster = stub.K8sNamespace, stub.K8sWorkload, stub.K8sCluster
				if workload == "" && stub.Labels != nil {
					workload = stub.Labels.AppName
				}
				if ns == "" && stub.Labels != nil {
					ns = stub.Labels.Namespace
				}
				break
			}
		}

		for _, m := range ed.Metrics {
			mn, avg, mx, _, ok := pointStats(m.Points)
			if !ok {
				continue
			}

			switch m.Key {
			case "error_rate":
				if mx > 20 {
					add(ActionItem{
						Urgency: "IMEDIATA", AppSection: "Deployments",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Verificar crash loops e considerar rollback para versão anterior",
						Reason: fmt.Sprintf("Taxa de erro crítica: máx=%.1f%% avg=%.1f%%", mx, avg),
					})
				} else if mx > 5 {
					add(ActionItem{
						Urgency: "ALTA", AppSection: "Deployments",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Verificar logs dos pods para identificar causa dos erros",
						Reason: fmt.Sprintf("Taxa de erro elevada: máx=%.1f%% avg=%.1f%%", mx, avg),
					})
				}
			case "response_p95":
				if mx > 5000 {
					add(ActionItem{
						Urgency: "IMEDIATA", AppSection: "HPA",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Aumentar maxReplicas — latência P95 indica sobrecarga",
						Reason: fmt.Sprintf("Latência P95 crítica: máx=%.0fms avg=%.0fms", mx, avg),
					})
				} else if mx > 1000 {
					add(ActionItem{
						Urgency: "ALTA", AppSection: "HPA",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Revisar minReplicas do HPA — latência elevada",
						Reason: fmt.Sprintf("Latência P95 alta: máx=%.0fms avg=%.0fms", mx, avg),
					})
				}
			case "pod_restarts":
				if mx > 5 {
					add(ActionItem{
						Urgency: "IMEDIATA", AppSection: "Resource Explorer",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Aumentar memory limit do container — provável OOMKill",
						Reason: fmt.Sprintf("Pods reiniciando: máx=%.0f avg=%.1f restarts", mx, avg),
					})
				} else if mx > 1 {
					add(ActionItem{
						Urgency: "ALTA", AppSection: "Deployments",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Verificar logs de crash — possível OOMKill ou falha na inicialização",
						Reason: fmt.Sprintf("Pods com restarts: máx=%.0f avg=%.1f", mx, avg),
					})
				}
			case "pods_ready_pct":
				if mn < 80 {
					add(ActionItem{
						Urgency: "IMEDIATA", AppSection: "HPA",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Escalar HPA — pods insuficientes servindo tráfego",
						Reason: fmt.Sprintf("Mínimo de %.0f%% pods prontos (saudável: ≥95%%)", mn),
					})
				} else if mn < 95 {
					add(ActionItem{
						Urgency: "ALTA", AppSection: "Deployments",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Verificar saúde dos pods — alguns não respondem ao readiness probe",
						Reason: fmt.Sprintf("Mínimo de %.0f%% pods prontos (ideal: 100%%)", mn),
					})
				}
			case "cpu_throttle":
				if mx > 500 {
					add(ActionItem{
						Urgency: "IMEDIATA", AppSection: "Resource Explorer",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Aumentar CPU limit/request — throttling severo está causando lentidão",
						Reason: fmt.Sprintf("CPU throttling crítico: máx=%.0f%% avg=%.0f%%", mx, avg),
					})
				} else if mx > 100 {
					add(ActionItem{
						Urgency: "ALTA", AppSection: "Resource Explorer",
						Workload: workload, Namespace: ns, Cluster: cluster,
						Action: "Revisar CPU requests — container subdimensionado",
						Reason: fmt.Sprintf("CPU throttling elevado: máx=%.0f%% avg=%.0f%%", mx, avg),
					})
				}
			}
		}
	}

	// Fallback: sem métricas suficientes para diagnóstico específico
	if len(items) == 0 {
		urgency := "ALTA"
		if problem.SeverityLevel == "AVAILABILITY" {
			urgency = "IMEDIATA"
		}
		items = append(items, ActionItem{
			Urgency: urgency, AppSection: "Health Check",
			Action: "Executar Health Check detalhado para investigar causa da falha",
			Reason: fmt.Sprintf("Problem %s sem métricas suficientes para diagnóstico automático", problem.SeverityLevel),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		order := map[string]int{"IMEDIATA": 0, "ALTA": 1, "MONITORAR": 2}
		return order[items[i].Urgency] < order[items[j].Urgency]
	})

	return items
}

// buildDynatracePrompt monta prompt com métricas ricas e instrui IA a sugerir ações corretivas
func buildDynatracePrompt(problem *dtclient.Problem, mr *dtclient.ProblemMetricsResponse, entityEvents map[string][]dtclient.Event) string {
	var sb strings.Builder

	sb.WriteString("Você é um SRE sênior especialista em Kubernetes e observabilidade.\n")
	sb.WriteString("Analise este incidente Dynatrace com base EXCLUSIVAMENTE nos dados abaixo.\n")
	sb.WriteString("Seja técnico e direto. Cite valores numéricos reais das métricas.\n")
	sb.WriteString("Não sugira comandos kubectl — sugira ações que a ferramenta HPA Manager pode executar\n")
	sb.WriteString("(escalar HPA, reiniciar deployment, fazer rollback, ajustar limites de recursos).\n\n")

	// ── Cabeçalho ─────────────────────────────────────────────────────────────
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("INCIDENTE\n")
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("ID: %s | Severidade: %s | Impacto: %s\n", problem.DisplayID, problem.SeverityLevel, problem.ImpactLevel))
	sb.WriteString(fmt.Sprintf("Título: %s\n", problem.Title))
	sb.WriteString(fmt.Sprintf("Início: %s", problem.StartTime.Format("2006-01-02 15:04:05 UTC")))
	if problem.EndTime != nil {
		dur := problem.EndTime.Sub(problem.StartTime).Round(time.Second)
		sb.WriteString(fmt.Sprintf(" → Fim: %s (duração: %s)", problem.EndTime.Format("15:04:05 UTC"), dur))
	} else {
		sb.WriteString(" → 🔴 AINDA ABERTO")
	}
	sb.WriteString("\n")
	if len(problem.ManagementZones) > 0 {
		zones := make([]string, 0, len(problem.ManagementZones))
		for _, mz := range problem.ManagementZones {
			zones = append(zones, mz.Name)
		}
		sb.WriteString(fmt.Sprintf("Squads/Zones: %s\n", strings.Join(zones, ", ")))
	}
	if problem.RootCauseEntity != nil {
		rc := problem.RootCauseEntity
		name := rc.DisplayName
		if name == "" {
			name = rc.Name
		}
		sb.WriteString(fmt.Sprintf("Causa Raiz (Davis): %s [%s]\n", name, rc.EntityID.Type))
		if rc.K8sWorkload != "" {
			sb.WriteString(fmt.Sprintf("  → K8s: namespace=%s  workload=%s  cluster=%s\n",
				rc.K8sNamespace, rc.K8sWorkload, rc.K8sCluster))
		}
	}

	// ── Métricas por entidade (mesma coleta da aba Métricas) ──────────────────
	sb.WriteString("\n═══════════════════════════════════════\n")
	sb.WriteString("MÉTRICAS COLETADAS (aba Métricas do HPA Manager)\n")
	sb.WriteString("═══════════════════════════════════════\n")

	hasData := false
	for _, ed := range mr.Entities {
		if len(ed.Metrics) == 0 {
			continue
		}
		hasData = true
		prefix := ""
		if ed.IsRootCause {
			prefix = " ⭐ CAUSA RAIZ"
		}
		sb.WriteString(fmt.Sprintf("\n▸ %s [%s]%s\n", ed.EntityName, ed.EntityType, prefix))

		// Encontrar stub da entidade para K8s info
		for _, stub := range problem.AffectedEntities {
			if stub.EntityID.ID == ed.EntityID && stub.K8sWorkload != "" {
				sb.WriteString(fmt.Sprintf("  K8s: cluster=%s  namespace=%s  workload=%s\n",
					stub.K8sCluster, stub.K8sNamespace, stub.K8sWorkload))
				if stub.Labels != nil && stub.Labels.AppVersion != "" {
					sb.WriteString(fmt.Sprintf("  Versão: %s", stub.Labels.AppVersion))
					if stub.Labels.Stage != "" {
						sb.WriteString(fmt.Sprintf("  Stage: %s", stub.Labels.Stage))
					}
					sb.WriteString("\n")
				}
				break
			}
		}

		for _, m := range ed.Metrics {
			min, avg, max, last, ok := pointStats(m.Points)
			if !ok {
				continue
			}
			sev := severityLabel(m.Key, max)
			sb.WriteString(fmt.Sprintf("  %-28s min=%.2f avg=%.2f max=%.2f último=%.2f %s  [%s]\n",
				m.Label+":", min, avg, max, last, m.Unit, sev))
		}

		// Eventos desta entidade
		if evs, ok := entityEvents[ed.EntityID]; ok && len(evs) > 0 {
			sb.WriteString(fmt.Sprintf("  Eventos (%d):\n", len(evs)))
			for i, ev := range evs {
				if i >= 8 {
					break
				}
				sb.WriteString(fmt.Sprintf("    [%s] %s — %s\n",
					ev.StartTime.Format("15:04:05"), ev.EventType, ev.Title))
			}
		}
	}

	if !hasData {
		sb.WriteString("⚠️ Nenhuma métrica retornou dados na janela coletada.\n")
		sb.WriteString("Possíveis causas: entidades sem instrumentação de serviço, problema já resolvido,\n")
		sb.WriteString("ou agente DT não coletando nessa janela de tempo.\n")
	}

	// ── Instrução de análise ──────────────────────────────────────────────────
	sb.WriteString("\n═══════════════════════════════════════\n")
	sb.WriteString("ANÁLISE SOLICITADA\n")
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("REGRAS:\n")
	sb.WriteString("• Cite valores numéricos das métricas acima (ex: 'P95 de 8.4s', 'error_rate avg=34%')\n")
	sb.WriteString("• NÃO invente dados — se a métrica não constar acima, diga explicitamente\n")
	sb.WriteString("• As ações corretivas devem ser executáveis pela ferramenta HPA Manager:\n")
	sb.WriteString("  - Escalar HPA (ajustar minReplicas/maxReplicas)\n")
	sb.WriteString("  - Reiniciar Deployment (rollout restart)\n")
	sb.WriteString("  - Fazer rollback para versão anterior\n")
	sb.WriteString("  - Ajustar resource requests/limits do container\n")
	sb.WriteString("  - Verificar/ajustar ConfigMap de configuração\n")
	sb.WriteString("• NÃO sugira comandos kubectl para o usuário executar\n\n")

	sb.WriteString("1. ORIGEM: Qual métrica específica comprova a causa raiz? Cite min/avg/max.\n")
	sb.WriteString("   Classifique: falha de código (error_rate), gargalo (latência P90+), sobrecarga (throughput), infra K8s (pod_restarts, cpu_throttle).\n\n")

	sb.WriteString("2. PROPAGAÇÃO: Como a falha se cascateou entre os serviços?\n")
	sb.WriteString("   Use os timestamps dos eventos e os valores de latência/erro para traçar a sequência.\n\n")

	sb.WriteString("3. ANÁLISE K8S: O que as métricas de pods/containers revelam?\n")
	sb.WriteString("   Analise: restarts, pods_ready_pct, cpu_throttle, memória. Identifique se há pressão de recursos.\n\n")

	sb.WriteString("4. DEPENDÊNCIAS EXTERNAS: Analise as métricas de chamadas downstream.\n")
	sb.WriteString("   db_calls altos + db_latency alta → banco de dados. ext_calls altos → API externa. Cite os valores.\n\n")

	sb.WriteString("5. AÇÕES CORRETIVAS: Liste ações que a ferramenta HPA Manager pode executar agora.\n")
	sb.WriteString("   Para cada ação: qual workload, qual namespace, qual parâmetro alterar e por quê (baseado nos dados).\n")
	sb.WriteString("   Separe: mitigação imediata (ex: escalar HPA) vs correção definitiva (ex: ajustar limites de memória).\n")

	return sb.String()
}
