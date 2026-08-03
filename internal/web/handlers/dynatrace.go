package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	dtclient "k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/healthcheck"
	"k8s-hpa-manager/internal/sanitizer"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// DynatraceHandler gerencia integração com Dynatrace para análise de problemas
type DynatraceHandler struct {
	tokensStore   *storage.UserTokensStore
	historyStore  *storage.AIHistoryStore
	aiHandler     *AIDiagnosticsHandler
	sanitizer     *sanitizer.Sanitizer
	nodePoolStore *storage.NodePoolRegistryStore
	orchestrator  *healthcheck.Orchestrator
	depRegistry   *storage.DependencyRegistry
}

// NewDynatraceHandler cria o handler
func NewDynatraceHandler(
	tokensStore *storage.UserTokensStore,
	historyStore *storage.AIHistoryStore,
	aiHandler *AIDiagnosticsHandler,
	nodePoolStore *storage.NodePoolRegistryStore,
) *DynatraceHandler {
	return &DynatraceHandler{
		tokensStore:   tokensStore,
		historyStore:  historyStore,
		aiHandler:     aiHandler,
		sanitizer:     sanitizer.New(),
		nodePoolStore: nodePoolStore,
	}
}

// SetInvestigateStores injeta orchestrator e dependency registry para investigação profunda.
// Chamado pelo server após criar os componentes de health check (que dependem de setup posterior).
func (h *DynatraceHandler) SetInvestigateStores(orchestrator *healthcheck.Orchestrator, depRegistry *storage.DependencyRegistry) {
	h.orchestrator = orchestrator
	h.depRegistry = depRegistry
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
//
// Identidade via InjectUserEmail() (JWT/RBAC) — não mais um "ai_email" digitado manualmente
// pelo cliente. Ver DYNATRACE-PROFILE-MIGRATION-PLAN.md: o campo digitado era um resquício de
// quando o app suportava rodar sem JWT; hoje o JWT está sempre ativo, então a identidade real do
// usuário logado já está sempre disponível.
func (h *DynatraceHandler) GetConfig(c *gin.Context) {
	userEmail := c.GetString("user_email")

	var dtURL, tagFilter string
	hasToken := false

	if userEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(userEmail)
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

// ─── POST /api/v1/dynatrace/config ────────────────────────────────────────────

// SaveConfig salva URL/token/tag filter do Dynatrace para o usuário logado (identidade via
// InjectUserEmail(), nunca um e-mail enviado pelo cliente). Faz merge com os tokens já
// existentes — igual ao handler de SaveTokens em ai_tokens.go — para não sobrescrever/apagar as
// chaves dos provedores de IA salvas na mesma linha (user_ai_tokens é uma tabela só, com colunas
// dynatrace_* ao lado das colunas de cada provedor).
func (h *DynatraceHandler) SaveConfig(c *gin.Context) {
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não identificado"})
		return
	}

	var req struct {
		DynatraceURL       string `json:"dynatrace_url"`
		DynatraceToken     string `json:"dynatrace_token"`
		DynatraceTagFilter string `json:"dynatrace_tag_filter"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if h.tokensStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tokens store não configurado"})
		return
	}

	existingTokens, err := h.tokensStore.GetTokens(userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get existing tokens"})
		return
	}
	if existingTokens == nil {
		existingTokens = &storage.UserTokens{UserEmail: userEmail}
	}

	// URL e token só sobrescrevem se vierem não-vazios (permite salvar só a tag filter sem
	// precisar reenviar o token a cada save); tag filter sempre sobrescreve, mesmo vazio, pra
	// permitir limpar o filtro — mesma semântica de ai_tokens.go:286-298.
	if req.DynatraceURL != "" {
		existingTokens.DynatraceURL = req.DynatraceURL
	}
	if req.DynatraceToken != "" {
		existingTokens.DynatraceToken = req.DynatraceToken
	}
	existingTokens.DynatraceTagFilter = req.DynatraceTagFilter

	if existingTokens.PreferredProvider == "" {
		existingTokens.PreferredProvider = "ollama"
	}

	if err := h.tokensStore.SaveTokens(userEmail, existingTokens); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save dynatrace config"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url":   existingTokens.DynatraceURL,
		"has_token":  existingTokens.DynatraceToken != "",
		"enabled":    existingTokens.DynatraceURL != "" && existingTokens.DynatraceToken != "",
		"tag_filter": existingTokens.DynatraceTagFilter,
	})
}

// ─── POST /api/v1/dynatrace/test ──────────────────────────────────────────────

// TestConnection testa conectividade com a API Dynatrace do usuário logado (mesma identidade de
// GetConfig/SaveConfig — via InjectUserEmail()).
func (h *DynatraceHandler) TestConnection(c *gin.Context) {
	userEmail := c.GetString("user_email")

	client, err := h.clientForUser(userEmail)
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

// ─── GET /api/v1/dynatrace/management-zones ───────────────────────────────────

// GetManagementZones retorna as management zones (alert profiles) disponíveis no ambiente DT.
func (h *DynatraceHandler) GetManagementZones(c *gin.Context) {
	aiEmail := c.Query("ai_email")

	client, err := h.clientForUser(aiEmail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"zones": []interface{}{}})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	zones, err := client.GetManagementZones(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Dynatrace: falha ao buscar management zones")
		c.JSON(http.StatusOK, gin.H{"zones": []interface{}{}, "error": err.Error()})
		return
	}

	log.Info().Int("count", len(zones)).Msg("Dynatrace: management zones carregadas")
	c.JSON(http.StatusOK, gin.H{"zones": zones})
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

	// Filtro por management zone ou tag (prefixo "tag:valor")
	tagFilter := c.Query("filter")
	// status: "OPEN" (padrão), "CLOSED" ou "ALL"
	statusFilter := c.Query("status")
	// Intervalo de tempo para busca histórica.
	// Aceita: valores relativos ("now-7d", "now-24h"), ISO 8601 completo, ou datas "YYYY-MM-DD"
	// (que são convertidas para ISO 8601 completo pois a API DT rejeita datas sem hora).
	fromTime := normalizeDTTime(c.Query("from"), false)
	toTime := normalizeDTTime(c.Query("to"), true)

	result, err := client.GetOpenProblems(ctx, tagFilter, statusFilter, fromTime, toTime)
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

// ─── POST /api/v1/dynatrace/problems/:problemId/investigate ───────────────────

// InvestigationReport é o resultado estruturado de uma investigação profunda.
type InvestigationReport struct {
	ProblemID    string `json:"problem_id"`
	ProblemTitle string `json:"problem_title"`

	// Entidades identificadas no alerta DT
	RootCauseEntityName string `json:"root_cause_entity_name,omitempty"`
	RootCauseEntityType string `json:"root_cause_entity_type,omitempty"`

	// Cluster identificado via Node Pool Registry (de entidades HOST)
	IdentifiedCluster  string `json:"identified_cluster,omitempty"`
	IdentifiedNodePool string `json:"identified_node_pool,omitempty"`

	// Namespace e workload identificados (via K8s tags do PROCESS_GROUP/rootCause)
	IdentifiedNamespace string `json:"identified_namespace,omitempty"`
	IdentifiedWorkload  string `json:"identified_workload,omitempty"`

	// Métricas DT coletadas para o problem (serviços, hosts, workloads K8s)
	DTMetrics *dtclient.ProblemMetricsResponse `json:"dt_metrics,omitempty"`

	// Health check K8s direcionado (nil se cluster/namespace não identificados)
	HealthCheckResult *healthcheck.HealthCheckResult `json:"health_check_result,omitempty"`
	HealthCheckError  string                         `json:"health_check_error,omitempty"`

	// Dependências do workload (nil se não encontrado)
	Dependencies []storage.DependencyRecord `json:"dependencies,omitempty"`

	// Análise AI combinando DT + K8s + dependências
	AIAnalysis string `json:"ai_analysis,omitempty"`
	AIError    string `json:"ai_error,omitempty"`

	InvestigatedAt time.Time `json:"investigated_at"`
}

// reDTHostEntity extrai o nome do node pool de entity names do padrão "aks-<pool>-XXXXXXXX-vmssXXXXX"
var reDTHostEntity = regexp.MustCompile(`^aks-([a-z0-9]+)-[0-9a-f]+-vmss`)

// InvestigateProblem executa investigação profunda de um problem DT.
// Usa Node Pool Registry, Health Check Orchestrator e Dependency Registry para
// produzir análise causa→efeito→correção contextualizada no ambiente K8s.
func (h *DynatraceHandler) InvestigateProblem(c *gin.Context) {
	problemID := c.Param("problemId")

	var req struct {
		AIEmail string `json:"ai_email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AIEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ai_email é obrigatório"})
		return
	}

	dtClient, err := h.clientForUser(req.AIEmail)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Timeout alto: busca DT + health check K8s em série
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()

	// 1. Buscar problem e enriquecer todas as entidades
	problem, err := dtClient.GetProblem(ctx, problemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao buscar problem: " + err.Error()})
		return
	}
	problem.AffectedEntities = dtClient.EnrichEntitiesWithK8s(ctx, problem.AffectedEntities)
	problem.ImpactedEntities = dtClient.EnrichEntitiesWithK8s(ctx, problem.ImpactedEntities)
	if problem.RootCauseEntity != nil {
		enriched := dtClient.EnrichStub(ctx, *problem.RootCauseEntity)
		problem.RootCauseEntity = &enriched
	}

	// 2a. Coletar métricas DT (em paralelo, sem bloquear o fluxo principal)
	// Reutiliza a mesma coleta rica da aba "Métricas": P50/P90/P95/P99, error_rate, throughput,
	// DB calls, pods_ready_pct, pod_restarts, cpu_throttle, memory
	dtMetrics := dtClient.GetEntityMetricsForProblem(ctx, problem)

	report := &InvestigationReport{
		ProblemID:      problemID,
		ProblemTitle:   problem.Title,
		DTMetrics:      dtMetrics,
		InvestigatedAt: time.Now(),
	}

	// 2. Extrair namespace/workload da rootCauseEntity (PROCESS_GROUP com K8s tags injetadas pelo OneAgent)
	if rc := problem.RootCauseEntity; rc != nil {
		report.RootCauseEntityName = rc.Name
		report.RootCauseEntityType = rc.EntityID.Type
		if rc.K8sNamespace != "" {
			report.IdentifiedNamespace = rc.K8sNamespace
		}
		if rc.K8sWorkload != "" {
			report.IdentifiedWorkload = rc.K8sWorkload
		}
		// Fallback para Labels (OneAgent pode expor via labels em vez de campos diretos)
		if rc.Labels != nil {
			if report.IdentifiedNamespace == "" {
				report.IdentifiedNamespace = rc.Labels.Namespace
			}
			if report.IdentifiedWorkload == "" {
				report.IdentifiedWorkload = rc.Labels.AppName
			}
		}
	}

	// 3. Identificar cluster via HOST entities → Node Pool Registry
	if h.nodePoolStore != nil {
		allEntities := append(problem.AffectedEntities, problem.ImpactedEntities...)
		for _, e := range allEntities {
			if e.EntityID.Type != "HOST" {
				continue
			}
			// Formato DT: "aks-<nodepool>-XXXXXXXX-vmssXXXXX"
			m := reDTHostEntity.FindStringSubmatch(strings.ToLower(e.Name))
			if len(m) < 2 {
				continue
			}
			poolName := m[1]
			entries, lookupErr := h.nodePoolStore.LookupByNodePool(poolName)
			if lookupErr != nil || len(entries) == 0 {
				log.Debug().Str("pool", poolName).Msg("InvestigateProblem: pool não encontrado no registry")
				continue
			}
			report.IdentifiedCluster = entries[0].Cluster
			report.IdentifiedNodePool = entries[0].NodePool
			log.Info().
				Str("host_entity", e.Name).
				Str("pool", poolName).
				Str("cluster", report.IdentifiedCluster).
				Msg("InvestigateProblem: cluster identificado via Node Pool Registry")
			break
		}
	}

	// Determinar o ambiente alvo do problema UMA VEZ — usado em 3b e 3c.
	// Regra: se nenhum marcador não-prd (hlg/sit/stg…) aparece no problema → prd por padrão.
	problemEnvHint := extractEnvHint(problem)

	// 3b. Fallback por keyword: se cluster ainda não identificado, buscar no Node Pool Registry.
	//     Prioriza clusters compatíveis com o ambiente do problema: se envHint="prd", escolhe
	//     o cluster prd dentre os resultados — nunca retorna hlg quando o problema é de prd.
	if report.IdentifiedCluster == "" && h.nodePoolStore != nil {
		kwTexts := make([]string, 0, len(problem.ManagementZones)+1)
		for _, mz := range problem.ManagementZones {
			kwTexts = append(kwTexts, mz.Name)
		}
		if problem.RootCauseEntity != nil {
			kwTexts = append(kwTexts, problem.RootCauseEntity.Name)
		}
		keywords := extractKeywords(kwTexts...)
		for _, kw := range keywords {
			entries, kwErr := h.nodePoolStore.LookupByKeyword(kw)
			if kwErr != nil || len(entries) == 0 {
				continue
			}
			// Escolher entry com cluster compatível com o ambiente do problema
			chosen := pickEntryByEnv(entries, problemEnvHint)
			report.IdentifiedCluster = chosen.Cluster
			report.IdentifiedNodePool = chosen.NodePool
			log.Info().
				Str("keyword", kw).
				Str("cluster", report.IdentifiedCluster).
				Str("nodepool", report.IdentifiedNodePool).
				Str("env_hint", problemEnvHint).
				Msg("InvestigateProblem: cluster identificado via keyword no Node Pool Registry")
			break
		}
	}

	// 3c. Fallback por keyword para namespace: usa o ambiente do PROBLEMA (não do cluster)
	//     para filtrar namespaces — garante que prd nunca analisa namespaces hlg/sit e vice-versa.
	if report.IdentifiedCluster != "" && report.IdentifiedNamespace == "" && h.orchestrator != nil {
		kwTexts := make([]string, 0, len(problem.ManagementZones)+1)
		for _, mz := range problem.ManagementZones {
			kwTexts = append(kwTexts, mz.Name)
		}
		if problem.RootCauseEntity != nil {
			kwTexts = append(kwTexts, problem.RootCauseEntity.Name)
		}
		keywords := extractKeywords(kwTexts...)
		if ns := h.orchestrator.FindNamespaceByKeywords(ctx, report.IdentifiedCluster, keywords, problemEnvHint); ns != "" {
			report.IdentifiedNamespace = ns
			log.Info().
				Str("cluster", report.IdentifiedCluster).
				Str("namespace", ns).
				Str("env_hint", problemEnvHint).
				Msg("InvestigateProblem: namespace identificado via keyword em K8s")
		}
	}

	// 4. Health Check K8s direcionado (apenas se cluster identificado)
	if report.IdentifiedCluster != "" && h.orchestrator != nil {
		hcReq := healthcheck.HealthCheckRequest{
			Clusters:         []string{report.IdentifiedCluster},
			CheckDeployments: true,
			CheckEvents:      true,
			CheckHPAs:        true,
			Timeout:          60,
			ApplyFilters:     true,
		}
		if report.IdentifiedNamespace != "" {
			hcReq.Namespaces = []string{report.IdentifiedNamespace}
		}

		sessionID := "dt-investigate-" + uuid.New().String()[:8]
		hcCtx, hcCancel := context.WithTimeout(ctx, 90*time.Second)
		defer hcCancel()

		results, hcErr := h.orchestrator.ExecuteHealthCheck(hcCtx, sessionID, hcReq)
		if hcErr != nil {
			report.HealthCheckError = hcErr.Error()
			log.Warn().Err(hcErr).Str("cluster", report.IdentifiedCluster).Msg("InvestigateProblem: health check falhou")
		} else if len(results) > 0 {
			report.HealthCheckResult = results[0]
		}
	}

	// 5. Buscar dependências do workload
	if h.depRegistry != nil && report.IdentifiedNamespace != "" {
		deps, depErr := h.depRegistry.GetAll(report.IdentifiedCluster, report.IdentifiedNamespace, "")
		if depErr == nil {
			report.Dependencies = deps
		}
	}

	// 6. Análise AI se provider disponível
	if h.aiHandler != nil {
		provider, provErr := h.aiHandler.GetProviderForUser(req.AIEmail)
		if provErr != nil {
			report.AIError = provErr.Error()
		} else {
			prompt := buildInvestigationPrompt(problem, report)
			prompt = h.sanitizer.SanitizeText(prompt)
			aiCtx, aiCancel := context.WithTimeout(ctx, 90*time.Second)
			defer aiCancel()
			analysis, aiErr := provider.Analyze(aiCtx, prompt)
			if aiErr != nil {
				report.AIError = aiErr.Error()
			} else {
				report.AIAnalysis = analysis
			}
		}
	}

	log.Info().
		Str("problem_id", problemID).
		Str("cluster", report.IdentifiedCluster).
		Str("namespace", report.IdentifiedNamespace).
		Str("workload", report.IdentifiedWorkload).
		Msg("InvestigateProblem: investigação concluída")

	c.JSON(http.StatusOK, report)
}

// pickEntryByEnv escolhe o NodePoolRegistryEntry cujo cluster mais combina com o envHint.
// Prioridade: cluster com token de ambiente igual ao envHint → qualquer outro.
// Evita escolher cluster hlg quando o problema é de prd e vice-versa.
func pickEntryByEnv(entries []storage.NodePoolRegistryEntry, envHint string) storage.NodePoolRegistryEntry {
	if len(entries) == 1 || envHint == "" {
		return entries[0]
	}
	for _, e := range entries {
		if extractEnvFromText(e.Cluster) == envHint {
			return e
		}
	}
	return entries[0] // fallback: primeiro disponível
}

// extractEnvFromText extrai o marcador de ambiente de um texto qualquer (ex: nome de cluster)
// por comparação de token exato. Ex: "akspriv-oferta-hlg-admin" → "hlg".
// Retorna "" se nenhum marcador reconhecido for encontrado como token.
func extractEnvFromText(text string) string {
	allEnvs := []string{"prd", "prod", "hlg", "sit", "stg", "hml", "uat", "dev"}
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' '
	})
	for _, tok := range tokens {
		for _, env := range allEnvs {
			if tok == env {
				return env
			}
		}
	}
	return ""
}

// extractEnvHint detecta o ambiente alvo a partir do conteúdo do problema DT.
// Varre management zones, nome da rootCause e título em busca de marcadores não-prd
// como tokens completos (separados por espaço, hífen ou underscore) — evita falsos
// positivos de palavras que contêm "sit"/"stg" como substring (ex: "transition", "staging-area").
// Se nenhum marcador não-prd for encontrado, retorna "prd" por padrão.
func extractEnvHint(problem *dtclient.Problem) string {
	nonPrd := []string{"hlg", "sit", "stg", "hml", "uat", "dev"}
	candidates := make([]string, 0)
	for _, mz := range problem.ManagementZones {
		candidates = append(candidates, mz.Name)
	}
	if problem.RootCauseEntity != nil {
		candidates = append(candidates, problem.RootCauseEntity.Name)
	}
	candidates = append(candidates, problem.Title)

	for _, text := range candidates {
		tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return r == ' ' || r == '-' || r == '_' || r == '/' || r == '.'
		})
		for _, tok := range tokens {
			for _, marker := range nonPrd {
				if tok == marker {
					return marker
				}
			}
		}
	}
	return "prd" // padrão: assumir produção
}

// removeAccents substitui caracteres acentuados comuns do português/espanhol por equivalentes ASCII.
// Necessário porque K8s exige nomes ASCII e management zones DT podem ter Unicode.
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
	"Á", "a", "À", "a", "Â", "a", "Ã", "a",
	"É", "e", "È", "e", "Ê", "e",
	"Í", "i", "Ì", "i",
	"Ó", "o", "Ò", "o", "Ô", "o", "Õ", "o",
	"Ú", "u", "Ù", "u",
	"Ç", "c", "Ñ", "n",
)

// extractKeywords extrai palavras-chave de textos livres para busca fuzzy em node pools e namespaces.
// Normaliza acentos para ASCII, separa por separadores e dígitos, filtra tokens ≥4 chars.
// Adiciona stems removendo 's' final (plurais). Ex: "Cálculo de Fretes" → ["calculo","fretes","frete"]
func extractKeywords(texts ...string) []string {
	seen := map[string]bool{}
	var result []string

	add := func(token string) {
		token = accentReplacer.Replace(strings.ToLower(strings.TrimSpace(token)))
		if len(token) >= 4 && !seen[token] {
			seen[token] = true
			result = append(result, token)
		}
	}

	for _, text := range texts {
		tokens := strings.FieldsFunc(text, func(r rune) bool {
			return r == ' ' || r == '-' || r == '_' || r == '/' || r == ':' || (r >= '0' && r <= '9')
		})
		for _, tok := range tokens {
			add(tok)
			// Stem: remover 's' final (plural) se a raiz ainda tiver ≥4 chars
			lower := accentReplacer.Replace(strings.ToLower(tok))
			if strings.HasSuffix(lower, "s") && len(lower) > 4 {
				stem := lower[:len(lower)-1]
				if !seen[stem] {
					seen[stem] = true
					result = append(result, stem)
				}
			}
		}
	}
	return result
}

// buildInvestigationPrompt monta prompt contextualizado para análise AI de investigação profunda.
// Inclui: métricas DT (serviços/hosts/workloads K8s), Health Check K8s com recursos de container,
// e dependências externas — para análise causa→efeito→correção sem lacunas de dados.
func buildInvestigationPrompt(problem *dtclient.Problem, report *InvestigationReport) string {
	var sb strings.Builder

	sb.WriteString("Você é um SRE sênior especialista em Kubernetes e observabilidade.\n")
	sb.WriteString("Analise este incidente com base EXCLUSIVAMENTE nos dados abaixo.\n")
	sb.WriteString("Cite valores numéricos reais. Se uma métrica não constar, diga explicitamente 'Dados Faltantes'.\n")
	sb.WriteString("NÃO sugira comandos kubectl — use apenas ações do HPA Manager:\n")
	sb.WriteString("(escalar HPA, reiniciar/rollback deployment, ajustar requests/limits, ajustar ConfigMap)\n\n")

	// ── Cabeçalho do incidente ────────────────────────────────────────────────
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("INCIDENTE DYNATRACE\n")
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("ID: %s | Severidade: %s | Impacto: %s | Status: %s\n",
		problem.DisplayID, problem.SeverityLevel, problem.ImpactLevel, problem.Status))
	sb.WriteString(fmt.Sprintf("Título: %s\n", problem.Title))
	sb.WriteString(fmt.Sprintf("Início: %s", problem.StartTime.Format("2006-01-02 15:04:05 UTC")))
	if problem.EndTime != nil {
		dur := problem.EndTime.Sub(problem.StartTime).Round(time.Second)
		sb.WriteString(fmt.Sprintf(" → Fim: %s (duração: %s)", problem.EndTime.Format("15:04:05 UTC"), dur))
	} else {
		sb.WriteString(" → AINDA ABERTO")
	}
	sb.WriteString("\n")
	if len(problem.ManagementZones) > 0 {
		zones := make([]string, 0, len(problem.ManagementZones))
		for _, mz := range problem.ManagementZones {
			zones = append(zones, mz.Name)
		}
		sb.WriteString(fmt.Sprintf("Squads/Zones: %s\n", strings.Join(zones, ", ")))
	}
	if report.RootCauseEntityName != "" {
		sb.WriteString(fmt.Sprintf("Causa Raiz (Davis): %s [%s]\n", report.RootCauseEntityName, report.RootCauseEntityType))
	}
	if report.IdentifiedCluster != "" {
		sb.WriteString(fmt.Sprintf("Cluster K8s: %s (node pool: %s)\n", report.IdentifiedCluster, report.IdentifiedNodePool))
	}
	if report.IdentifiedNamespace != "" {
		sb.WriteString(fmt.Sprintf("Namespace: %s  Workload: %s\n", report.IdentifiedNamespace, report.IdentifiedWorkload))
	}

	// ── Métricas Dynatrace (serviço/host/K8s workload) ────────────────────────
	sb.WriteString("\n═══════════════════════════════════════\n")
	sb.WriteString("MÉTRICAS DYNATRACE\n")
	sb.WriteString("═══════════════════════════════════════\n")

	if report.DTMetrics != nil && len(report.DTMetrics.Entities) > 0 {
		hasMetricData := false
		for _, ed := range report.DTMetrics.Entities {
			if len(ed.Metrics) == 0 {
				continue
			}
			hasMetricData = true
			rootMark := ""
			if ed.IsRootCause {
				rootMark = " ⭐CAUSA RAIZ"
			}
			sb.WriteString(fmt.Sprintf("\n▸ %s [%s]%s\n", ed.EntityName, ed.EntityType, rootMark))
			for _, m := range ed.Metrics {
				mn, avg, mx, last, ok := pointStats(m.Points)
				if !ok {
					continue
				}
				sev := severityLabel(m.Key, mx)
				sb.WriteString(fmt.Sprintf("  %-30s min=%.2f avg=%.2f max=%.2f último=%.2f %s %s\n",
					m.Label+":", mn, avg, mx, last, m.Unit, sev))
			}
		}
		if !hasMetricData {
			sb.WriteString("⚠️ Métricas DT sem dados nesta janela (entidades sem instrumentação ou problema já resolvido).\n")
		}
	} else {
		sb.WriteString("⚠️ Métricas DT não disponíveis.\n")
	}

	// ── Health Check K8s ──────────────────────────────────────────────────────
	sb.WriteString("\n═══════════════════════════════════════\n")
	sb.WriteString("HEALTH CHECK K8S\n")
	sb.WriteString("═══════════════════════════════════════\n")

	if report.HealthCheckError != "" {
		sb.WriteString(fmt.Sprintf("Falhou: %s\n", report.HealthCheckError))
	} else if report.IdentifiedCluster == "" {
		sb.WriteString("Cluster não identificado — execute 'Escanear Clusters' no Node Pool Registry.\n")
	} else if report.HealthCheckResult == nil {
		sb.WriteString("Health Check não executado.\n")
	} else {
		hc := report.HealthCheckResult
		sb.WriteString(fmt.Sprintf("Cluster: %s | Status geral: %s | Verificações: %d | Críticos: %d | Altos: %d\n",
			hc.Cluster, hc.OverallStatus, hc.TotalChecks, hc.SeverityCounts.Critical, hc.SeverityCounts.High))

		// Deployments — com métricas de resource e crashes
		if len(hc.DeploymentResults) > 0 {
			// Prioriza o workload identificado; mostra todos
			sb.WriteString("\nDeployments:\n")
			for _, d := range hc.DeploymentResults {
				focus := ""
				if strings.EqualFold(d.Name, report.IdentifiedWorkload) {
					focus = " ← WORKLOAD IDENTIFICADO"
				}
				sb.WriteString(fmt.Sprintf("  %s/%s [%s]%s\n", d.Namespace, d.Name, d.Status, focus))
				sb.WriteString(fmt.Sprintf("    Pods: %d/%d prontos | Crashes: %d | ImagePullErrors: %d\n",
					d.ReplicasReady, d.ReplicasDesired, d.ContainersCrash, d.ImagePullErrors))
				if d.CPUUsagePercent > 0 || d.MemoryUsagePercent > 0 {
					sb.WriteString(fmt.Sprintf("    CPU uso: %.1f%% | Memória uso: %.1f%% | QoS: %s\n",
						d.CPUUsagePercent, d.MemoryUsagePercent, d.QoSClass))
				}
				// Recursos dos containers
				for _, cr := range d.ContainerResources {
					cpuReq := cr.CPURequest
					if cpuReq == "" {
						cpuReq = "não definido"
					}
					cpuLim := cr.CPULimit
					if cpuLim == "" {
						cpuLim = "não definido"
					}
					memReq := cr.MemoryRequest
					if memReq == "" {
						memReq = "não definido"
					}
					memLim := cr.MemoryLimit
					if memLim == "" {
						memLim = "não definido"
					}
					sb.WriteString(fmt.Sprintf("    Container %s: cpu req=%s lim=%s | mem req=%s lim=%s\n",
						cr.Name, cpuReq, cpuLim, memReq, memLim))
				}
				for _, ri := range d.ResourceIssues {
					sb.WriteString(fmt.Sprintf("    ⚠️ [%s] container=%s %s=%s\n",
						string(ri.Severity), ri.Container, ri.ResourceType, ri.Issue))
				}
				if d.Message != "" && d.Status != "healthy" {
					sb.WriteString(fmt.Sprintf("    Mensagem: %s\n", d.Message))
				}
			}
		}

		// HPAs
		if len(hc.HPAResults) > 0 {
			sb.WriteString("\nHPAs:\n")
			for _, h := range hc.HPAResults {
				focus := ""
				if strings.EqualFold(h.Name, report.IdentifiedWorkload) {
					focus = " ← WORKLOAD IDENTIFICADO"
				}
				sb.WriteString(fmt.Sprintf("  %s/%s [%s]%s replicas: atual=%d desejado=%d min=%d max=%d\n",
					h.Namespace, h.Name, h.Status, focus,
					h.CurrentReplicas, h.DesiredReplicas, h.MinReplicas, h.MaxReplicas))
			}
		}

		// Eventos K8s
		if len(hc.EventResults) > 0 {
			sb.WriteString(fmt.Sprintf("\nEventos K8s críticos (%d):\n", len(hc.EventResults)))
			for i, e := range hc.EventResults {
				if i >= 10 {
					sb.WriteString(fmt.Sprintf("  ... e mais %d eventos\n", len(hc.EventResults)-10))
					break
				}
				sb.WriteString(fmt.Sprintf("  [%s] %s/%s — %s: %s\n",
					string(e.Severity), e.Namespace, e.Name, e.Reason, e.Message))
			}
		}
	}

	// ── Dependências externas ─────────────────────────────────────────────────
	if len(report.Dependencies) > 0 {
		sb.WriteString("\n═══════════════════════════════════════\n")
		sb.WriteString("DEPENDÊNCIAS EXTERNAS DO WORKLOAD\n")
		sb.WriteString("═══════════════════════════════════════\n")
		seen := map[string]bool{}
		for _, dep := range report.Dependencies {
			key := dep.ServiceType + ":" + dep.ServiceName
			if seen[key] {
				continue
			}
			seen[key] = true
			topic := ""
			if dep.TopicName != "" {
				topic = " (tópico: " + dep.TopicName + ")"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s%s\n", dep.ServiceType, dep.ServiceName, topic))
		}
	}

	// ── Solicitação de análise — 5 seções compatíveis com o parser do frontend ──
	sb.WriteString("\n═══════════════════════════════════════\n")
	sb.WriteString("ANÁLISE SOLICITADA\n")
	sb.WriteString("═══════════════════════════════════════\n")
	sb.WriteString("REGRAS: Cite valores numéricos reais. Se dados faltarem, diga explicitamente.\n")
	sb.WriteString("NÃO sugira comandos kubectl. Use ações do HPA Manager.\n")
	sb.WriteString("Estruture a resposta EXATAMENTE nestas 5 seções (use os títulos abaixo literalmente):\n\n")
	sb.WriteString("1. ORIGEM\n")
	sb.WriteString("   Qual métrica corrobora a causa raiz? Cite valores (ex: 'CPU max=96.52%', 'P95=8.4s').\n")
	sb.WriteString("   Classifique: Sobrecarga (throughput), Degradação (latência), Falha (error_rate), Recurso (CPU/memória).\n\n")
	sb.WriteString("2. PROPAGAÇÃO\n")
	sb.WriteString("   Como a falha se propagou entre serviços/pods? Duração? Impacto nos usuários?\n\n")
	sb.WriteString("3. ANÁLISE K8S\n")
	sb.WriteString("   Estado atual: pods prontos, HPA configurado, container resources, eventos críticos.\n")
	sb.WriteString("   O que os dados K8s confirmam ou contradizem sobre a causa raiz DT?\n\n")
	sb.WriteString("4. DEPENDÊNCIAS EXTERNAS\n")
	sb.WriteString("   Quais dependências (Kafka, DB, APIs externas) foram afetadas ou podem ter contribuído?\n")
	sb.WriteString("   Se não houver dados de dependências, diga explicitamente.\n\n")
	sb.WriteString("5. AÇÕES CORRETIVAS\n")
	sb.WriteString("   a) Mitigação imediata: Escalar HPA — cite namespace, workload, novos min/maxReplicas sugeridos\n")
	sb.WriteString("   b) Correção definitiva: Ajustar requests/limits CPU ou memória (cite container e valores)\n")
	sb.WriteString("   c) Prevenção: O que configurar no HPA Manager para evitar recorrência\n")

	return sb.String()
}

// normalizeDTTime converte o valor de tempo recebido para o formato aceito pela API DT.
// Valores relativos ("now-7d", "now-24h") passam sem alteração.
// Datas no formato "YYYY-MM-DD" são convertidas para ISO 8601 com hora.
// endOfDay=true arredonda para o fim do dia (23:59:59Z), usado no parâmetro "to".
func normalizeDTTime(v string, endOfDay bool) string {
	if v == "" {
		return ""
	}
	// Valores relativos DT passam direto
	if strings.HasPrefix(v, "now") {
		return v
	}
	// Tenta parsear como "YYYY-MM-DD"
	if t, err := time.Parse("2006-01-02", v); err == nil {
		if endOfDay {
			t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		}
		return t.UTC().Format(time.RFC3339)
	}
	// Qualquer outro formato (ISO 8601 já completo) passa sem alteração
	return v
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
