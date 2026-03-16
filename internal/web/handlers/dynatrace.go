package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

	var tagFilter string
	if aiEmail != "" && h.tokensStore != nil {
		if tokens, err2 := h.tokensStore.GetTokens(aiEmail); err2 == nil && tokens != nil {
			tagFilter = tokens.DynatraceTagFilter
		}
	}

	result, err := client.GetOpenProblems(ctx, tagFilter)
	if err != nil {
		log.Error().Err(err).Str("ai_email", aiEmail).Msg("Dynatrace: falha ao buscar problems")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	summaries := make([]dtclient.ProblemSummary, 0, len(result.Problems))
	for _, p := range result.Problems {
		// Enriquecer entidades com correlação K8s (tags do OneAgent)
		enriched := client.EnrichEntitiesWithK8s(ctx, p.AffectedEntities)

		// Deduplica correlações K8s únicas deste problem
		k8sMap := make(map[string]dtclient.K8sCorrelation)
		for _, e := range enriched {
			if e.K8sWorkload != "" {
				key := e.K8sCluster + "/" + e.K8sNamespace + "/" + e.K8sWorkload
				k8sMap[key] = dtclient.K8sCorrelation{
					Cluster:   e.K8sCluster,
					Namespace: e.K8sNamespace,
					Workload:  e.K8sWorkload,
				}
			}
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
			AffectedEntities: enriched,
			ImpactedEntities: p.ImpactedEntities,
			RootCauseEntity:  p.RootCauseEntity,
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

	c.JSON(http.StatusOK, gin.H{
		"problems":    summaries,
		"total":       len(summaries),
		"dt_total":    result.TotalCount, // total real no Dynatrace (pode ser > total se houver paginação)
		"fetched_at":  time.Now(),
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

	// 3. Coletar métricas e eventos das principais entidades (até 3)
	limit := 3
	if len(problem.AffectedEntities) < limit {
		limit = len(problem.AffectedEntities)
	}

	fromTime := problem.StartTime.Add(-10 * time.Minute)
	toTime := problem.StartTime.Add(2 * time.Hour)
	if problem.EndTime != nil {
		toTime = *problem.EndTime
	}

	entityContexts := make([]dtEntityCtx, 0, limit)
	for i := 0; i < limit; i++ {
		stub := problem.AffectedEntities[i]
		ec := dtEntityCtx{metrics: make(map[string]string)}

		if entity, err := dtClient.GetEntity(ctx, stub.EntityID.ID); err == nil {
			ec.entity = entity
		}
		if events, err := dtClient.GetEntityEvents(ctx, stub.EntityID.ID, fromTime, toTime); err == nil {
			ec.events = events
		}
		for _, selector := range []string{
			"builtin:service.errors.total.rate",
			"builtin:service.response.time",
		} {
			if md, err := dtClient.GetEntityMetrics(ctx, stub.EntityID.ID, selector, "now-1h", "now"); err == nil {
				if len(md.Data) > 0 && len(md.Data[0].Values) > 0 {
					vals := md.Data[0].Values
					ec.metrics[selector] = fmt.Sprintf("%.2f", vals[len(vals)-1])
				}
			}
		}
		entityContexts = append(entityContexts, ec)
	}

	// 4. Montar e sanitizar prompt
	prompt := buildDynatracePrompt(problem, entityContexts)
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
		"problem_id":  problemID,
		"title":       problem.Title,
		"severity":    problem.SeverityLevel,
		"analysis":    analysis,
		"analyzed_at": analyzedAt,
	})
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

// dtEntityCtx contexto coletado de uma entidade Dynatrace para análise AI
type dtEntityCtx struct {
	entity  *dtclient.Entity
	events  []dtclient.Event
	metrics map[string]string
}

// buildDynatracePrompt constrói o prompt completo com contexto Dynatrace + K8s
func buildDynatracePrompt(problem *dtclient.Problem, entityContexts []dtEntityCtx) string {
	var sb strings.Builder

	sb.WriteString("Você é um especialista em observabilidade e Kubernetes.\n")
	sb.WriteString("Analise o problema detectado pelo Dynatrace abaixo e responda em português.\n\n")

	sb.WriteString("=== PROBLEMA DYNATRACE ===\n")
	sb.WriteString(fmt.Sprintf("ID: %s (%s)\n", problem.DisplayID, problem.ProblemID))
	sb.WriteString(fmt.Sprintf("Título: %s\n", problem.Title))
	sb.WriteString(fmt.Sprintf("Severidade: %s | Impacto: %s\n", problem.SeverityLevel, problem.ImpactLevel))
	sb.WriteString(fmt.Sprintf("Início: %s\n", problem.StartTime.Format("2006-01-02 15:04:05 UTC")))
	if problem.EndTime != nil {
		sb.WriteString(fmt.Sprintf("Fim: %s\n", problem.EndTime.Format("2006-01-02 15:04:05 UTC")))
	} else {
		sb.WriteString("Status: ABERTO (ainda ativo)\n")
	}
	if problem.RootCauseEntity != nil {
		sb.WriteString(fmt.Sprintf("Causa raiz (Dynatrace): %s (%s)\n",
			problem.RootCauseEntity.DisplayName, problem.RootCauseEntity.EntityID.ID))
	}

	sb.WriteString("\n=== ENTIDADES AFETADAS ===\n")
	for _, e := range problem.AffectedEntities {
		sb.WriteString(fmt.Sprintf("- %s [%s]", e.DisplayName, e.EntityID.Type))
		if e.K8sWorkload != "" {
			sb.WriteString(fmt.Sprintf(" → K8s: cluster=%s ns=%s workload=%s",
				e.K8sCluster, e.K8sNamespace, e.K8sWorkload))
		}
		sb.WriteString("\n")
	}

	for _, ec := range entityContexts {
		if ec.entity == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n--- %s [%s] ---\n", ec.entity.DisplayName, ec.entity.Type))

		if len(ec.metrics) > 0 {
			sb.WriteString("Métricas (última hora):\n")
			for k, v := range ec.metrics {
				name := k
				if strings.Contains(k, "errors") {
					name = "error_rate"
				} else if strings.Contains(k, "response.time") {
					name = "response_time_us"
				}
				sb.WriteString(fmt.Sprintf("  %s: %s\n", name, v))
			}
		}

		if len(ec.events) > 0 {
			sb.WriteString(fmt.Sprintf("Eventos recentes (%d):\n", len(ec.events)))
			for i, ev := range ec.events {
				if i >= 5 {
					break
				}
				sb.WriteString(fmt.Sprintf("  [%s] %s — %s\n",
					ev.StartTime.Format("15:04"), ev.EventType, ev.Title))
			}
		}

		hasTags := false
		for _, tag := range ec.entity.Tags {
			if strings.HasPrefix(tag.Key, "kubernetes.") {
				if !hasTags {
					sb.WriteString("Tags K8s (OneAgent):\n")
					hasTags = true
				}
				sb.WriteString(fmt.Sprintf("  %s: %s\n", tag.Key, tag.Value))
			}
		}
	}

	sb.WriteString("\n=== ANÁLISE SOLICITADA ===\n")
	sb.WriteString("Responda de forma estruturada:\n\n")
	sb.WriteString("1. **ORIGEM**: Causa raiz provável. O problema está no K8s ou em dependência externa?\n")
	sb.WriteString("2. **PROPAGAÇÃO**: Como o problema se propagou entre os serviços?\n")
	sb.WriteString("3. **COMPONENTES K8S**: Quais workloads/namespaces investigar com kubectl?\n")
	sb.WriteString("4. **DEPENDÊNCIAS EXTERNAS**: O que está fora do K8s (banco, Kafka, API, rede)?\n")
	sb.WriteString("5. **PRÓXIMOS PASSOS**: Ações de investigação em ordem de prioridade.\n")

	return sb.String()
}
