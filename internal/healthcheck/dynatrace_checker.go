package healthcheck

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	dtclient "k8s-hpa-manager/internal/dynatrace"
)

// DynatraceChecker verifica problems abertos no Dynatrace e correlaciona com o cluster K8s
type DynatraceChecker struct{}

// NewDynatraceChecker cria o checker
func NewDynatraceChecker() *DynatraceChecker {
	return &DynatraceChecker{}
}

// normalizeClusterName remove o sufixo "-admin" e normaliza o nome do cluster para comparação.
// O kubeconfig AKS usa "<cluster>-admin" mas o Dynatrace DTLabels.HostGroup não tem esse sufixo.
func normalizeClusterName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, "-admin")
	return name
}

// matchesCluster verifica se uma entidade enriquecida pertence ao cluster alvo.
// Compara (em ordem de confiança):
//  1. K8sCluster (tag kubernetes.cluster.name)
//  2. DTLabels.HostGroup (tag dt.host_group.id)
//  3. DisplayName — DT nomeia process groups como "Tech - cluster - proc"
//     ex: "Envoy - akspriv-busca-prd - envoy vv-categoria..." → contém "akspriv-busca-prd"
func matchesCluster(clusterNorm string, e dtclient.EntityStub) bool {
	if strings.EqualFold(normalizeClusterName(e.K8sCluster), clusterNorm) {
		return true
	}
	if e.Labels != nil && strings.EqualFold(normalizeClusterName(e.Labels.HostGroup), clusterNorm) {
		return true
	}
	// Fallback: cluster name aparece em qualquer parte do display name
	if clusterNorm != "" && strings.Contains(strings.ToLower(e.DisplayName), clusterNorm) {
		return true
	}
	if clusterNorm != "" && strings.Contains(strings.ToLower(e.Name), clusterNorm) {
		return true
	}
	return false
}

// CheckAll busca problems OPEN no Dynatrace filtrados pelo cluster e pela tag do analista.
// tagFilter (opcional): filtra por management zone ou tag — ex: "SRE-LOGISTICA".
// O filtro de cluster é sempre aplicado (mesmo sem tagFilter) usando normalizeClusterName.
func (c *DynatraceChecker) CheckAll(ctx context.Context, dtURL, dtToken, tagFilter, cluster string, timeoutSec int) []DynatraceHealth {
	results := []DynatraceHealth{}

	log.Info().
		Str("cluster", cluster).
		Str("dtURL", dtURL).
		Bool("hasToken", dtToken != "").
		Str("tagFilter", tagFilter).
		Int("timeoutSec", timeoutSec).
		Msg("[DynatraceChecker] Iniciando CheckAll")

	client, err := dtclient.NewClient(dtURL, dtToken)
	if err != nil {
		log.Error().Err(err).Msg("[DynatraceChecker] Falha ao criar client")
		return results
	}

	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	dtResult, err := client.GetOpenProblems(checkCtx, tagFilter)
	if err != nil {
		log.Error().Err(err).Str("tagFilter", tagFilter).Msg("[DynatraceChecker] Falha ao buscar problems")
		return results
	}

	log.Info().
		Int("totalProblems", dtResult.TotalCount).
		Int("returnedProblems", len(dtResult.Problems)).
		Str("tagFilter", tagFilter).
		Msg("[DynatraceChecker] Problems retornados pela API")

	clusterNorm := normalizeClusterName(cluster)
	log.Info().Str("clusterNorm", clusterNorm).Msg("[DynatraceChecker] Cluster normalizado para matching")
	matchedProblems := make([]dtclient.Problem, 0)

	toSlice := func(m map[string]struct{}) []string {
		s := make([]string, 0, len(m))
		for k := range m {
			if k != "" {
				s = append(s, k)
			}
		}
		return s
	}

	for _, p := range dtResult.Problems {
		// Enriquecer entidades com tags K8s + DTLabels (squad, journey, version, GitHub, etc.)
		enriched := client.EnrichEntitiesWithK8s(checkCtx, p.AffectedEntities)

		// Filtrar por cluster — normaliza ambos os lados para remover sufixo "-admin"
		// Lógica: descartar SOMENTE se alguma entidade tem K8sCluster/HostGroup explicitamente
		// diferente deste cluster. Entidades sem info de cluster são incluídas (não podemos verificar).
		if clusterNorm != "" {
			hasThisCluster := false
			hasOtherCluster := false
			for _, e := range enriched {
				if matchesCluster(clusterNorm, e) {
					hasThisCluster = true
					break
				}
				// Detectar se pertence explicitamente a outro cluster
				eCluster := normalizeClusterName(e.K8sCluster)
				hg := ""
				if e.Labels != nil {
					hg = normalizeClusterName(e.Labels.HostGroup)
				}
				if (eCluster != "" && eCluster != clusterNorm) || (hg != "" && hg != clusterNorm) {
					hasOtherCluster = true
				}
			}
			if !hasThisCluster && hasOtherCluster {
				// Todas as entidades com info de cluster apontam para outro cluster
				entityInfo := make([]string, 0, len(enriched))
				for _, e := range enriched {
					hostGroup := ""
					if e.Labels != nil {
						hostGroup = e.Labels.HostGroup
					}
					entityInfo = append(entityInfo, fmt.Sprintf("%s(k8s=%q,hg=%q)", e.BestName(), e.K8sCluster, hostGroup))
				}
				log.Debug().
					Str("problemId", p.ProblemID).
					Str("title", p.Title).
					Str("clusterNorm", clusterNorm).
					Strs("entities", entityInfo).
					Msg("[DynatraceChecker] Problem descartado: entidades pertencem a outro cluster")
				continue
			}
		}

		// Coletar workloads + metadata rico via DTLabels
		workloadSet := make(map[string]struct{})
		nsSet := make(map[string]struct{})
		squadSet := make(map[string]struct{})
		journeySet := make(map[string]struct{})
		envSet := make(map[string]struct{})
		repoSet := make(map[string]struct{})
		appVersions := make(map[string]string)
		affectedNames := make([]string, 0, len(enriched))

		for _, e := range enriched {
			if e.DisplayName != "" {
				affectedNames = append(affectedNames, e.DisplayName)
			}
			ns := e.K8sNamespace
			workload := e.K8sWorkload
			if e.Labels != nil {
				if ns == "" {
					ns = e.Labels.Namespace
				}
				if workload == "" {
					workload = e.Labels.AppName
				}
				if workload != "" && e.Labels.AppVersion != "" {
					appVersions[workload] = e.Labels.AppVersion
				}
				if e.Labels.ComponentSquad != "" {
					squadSet[e.Labels.ComponentSquad] = struct{}{}
				}
				if e.Labels.ComponentJourney != "" {
					journeySet[e.Labels.ComponentJourney] = struct{}{}
				}
				if e.Labels.AppEnvironment != "" {
					envSet[e.Labels.AppEnvironment] = struct{}{}
				}
				if e.Labels.GitHubRepoID != "" {
					repoSet[e.Labels.GitHubRepoID] = struct{}{}
				}
			}
			if workload != "" {
				workloadSet[ns+"/"+workload] = struct{}{}
				nsSet[ns] = struct{}{}
			}
		}

		workloads := toSlice(workloadSet)
		namespaces := toSlice(nsSet)
		severity, status := mapDTSeverity(p.SeverityLevel)

		// Fase 4.2: Escalada — problem em ambiente prd com impacto ENVIRONMENT → Crítico
		if p.ImpactLevel == "ENVIRONMENT" {
			for env := range envSet {
				if strings.EqualFold(env, "prd") {
					severity = SeverityCritical
					status = StatusCritical
					break
				}
			}
		}

		results = append(results, DynatraceHealth{
			ProblemID:        p.ProblemID,
			DisplayID:        p.DisplayID,
			Title:            p.Title,
			DTSeverity:       p.SeverityLevel,
			ImpactLevel:      p.ImpactLevel,
			Status:           status,
			Severity:         severity,
			StartTime:        p.StartTime,
			K8sNamespaces:    namespaces,
			K8sWorkloads:     workloads,
			AffectedEntities: affectedNames,
			Message:          fmt.Sprintf("[%s] %s — impacto: %s", p.SeverityLevel, p.Title, p.ImpactLevel),
			Suggestions:      buildDTSuggestions(p.SeverityLevel, workloads),
			CheckedAt:        time.Now(),
			AppVersions:      appVersions,
			GitHubRepos:      toSlice(repoSet),
			Squads:           toSlice(squadSet),
			Journeys:         toSlice(journeySet),
			Environments:     toSlice(envSet),
		})
		// Guardar problem original para enriquecimento na fase 2
		matchedProblems = append(matchedProblems, p)
	}

	log.Info().
		Int("matchedProblems", len(results)).
		Str("cluster", cluster).
		Msg("[DynatraceChecker] Problems correlacionados com o cluster")

	// Fase 2: enriquecer com Davis AI context + métricas (Top 5 por severidade, sem bloquear o HC)
	results = enrichWithContext(checkCtx, client, results, matchedProblems)

	return results
}

// mapDTSeverity converte severidade Dynatrace para o sistema de saúde do HC
func mapDTSeverity(dtSeverity string) (Severity, HealthStatus) {
	switch dtSeverity {
	case "AVAILABILITY":
		return SeverityCritical, StatusCritical
	case "ERROR":
		return SeverityHigh, StatusCritical
	case "PERFORMANCE":
		return SeverityMedium, StatusWarning
	case "RESOURCE_CONTENTION":
		return SeverityMedium, StatusWarning
	default: // CUSTOM_ALERT e outros
		return SeverityLow, StatusWarning
	}
}

// severityOrder retorna a prioridade numérica da severidade DT (menor = mais crítico).
func severityOrder(dtSeverity string) int {
	switch dtSeverity {
	case "AVAILABILITY":
		return 0
	case "ERROR":
		return 1
	case "PERFORMANCE":
		return 2
	case "RESOURCE_CONTENTION":
		return 3
	default:
		return 4
	}
}

// enrichWithContext enriquece os Top 5 problems com Davis AI context (evidências + eventos)
// e os críticos (AVAILABILITY/ERROR) com métricas. Roda em paralelo com timeout próprio.
func enrichWithContext(ctx context.Context, client *dtclient.Client, results []DynatraceHealth, problems []dtclient.Problem) []DynatraceHealth {
	if len(results) == 0 {
		return results
	}

	// Ordenar índices por severidade para processar os mais críticos primeiro
	indices := make([]int, len(results))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(a, b int) bool {
		return severityOrder(results[indices[a]].DTSeverity) < severityOrder(results[indices[b]].DTSeverity)
	})

	const maxContext = 5
	const ctxTimeoutSec = 15
	const metricsTimeoutSec = 10

	ctxTimeout, ctxCancel := context.WithTimeout(ctx, time.Duration(ctxTimeoutSec)*time.Second)
	defer ctxCancel()

	var wg sync.WaitGroup

	for rank, idx := range indices {
		if rank >= maxContext {
			break
		}
		wg.Add(1)
		go func(resultIdx int, p dtclient.Problem) {
			defer wg.Done()

			pctx := client.GetProblemContext(ctxTimeout, &p)
			if pctx == nil {
				return
			}

			evidence := make([]string, 0, len(pctx.Evidence))
			for _, ev := range pctx.Evidence {
				text := ev.DisplayName
				if ev.RootCause {
					text = "[Root Cause] " + text
				}
				evidence = append(evidence, text)
			}

			recentEvents := make([]string, 0, 3)
			for i, ev := range pctx.Events {
				if i >= 3 {
					break
				}
				recentEvents = append(recentEvents, ev.EventType+": "+ev.Title)
			}

			results[resultIdx].Evidence = evidence
			results[resultIdx].RecentEvents = recentEvents
			results[resultIdx].ContextFetched = true
		}(idx, problems[idx])
	}

	wg.Wait()

	// Métricas apenas para AVAILABILITY e ERROR (os mais críticos)
	metricsCtx, metricsCancel := context.WithTimeout(ctx, time.Duration(metricsTimeoutSec)*time.Second)
	defer metricsCancel()

	var mwg sync.WaitGroup
	for _, idx := range indices {
		sev := results[idx].DTSeverity
		if sev != "AVAILABILITY" && sev != "ERROR" {
			continue
		}
		mwg.Add(1)
		go func(resultIdx int, p dtclient.Problem) {
			defer mwg.Done()

			resp := client.GetEntityMetricsForProblem(metricsCtx, &p)
			if resp == nil || len(resp.Entities) == 0 {
				return
			}

			summary := make(map[string]float64)
			for _, entity := range resp.Entities {
				for _, series := range entity.Metrics {
					var maxVal float64
					for _, pt := range series.Points {
						if pt.V > maxVal {
							maxVal = pt.V
						}
					}
					if maxVal == 0 {
						continue
					}
					switch series.Key {
					case "error_rate":
						if maxVal > summary["error_rate"] {
							summary["error_rate"] = maxVal
						}
					case "response_p90":
						if maxVal > summary["response_p90_ms"] {
							summary["response_p90_ms"] = maxVal
						}
					case "response_p99":
						if maxVal > summary["response_p99_ms"] {
							summary["response_p99_ms"] = maxVal
						}
					case "throughput":
						if maxVal > summary["throughput_rpm"] {
							summary["throughput_rpm"] = maxVal
						}
					}
				}
			}
			if len(summary) > 0 {
				results[resultIdx].MetricsSummary = summary
			}
		}(idx, problems[idx])
	}

	mwg.Wait()
	return results
}

// SearchProblemsForWorkloads busca problems DT para workloads K8s que não apareceram
// no CheckAll (tags OneAgent ausentes ou incorretas). É a direção reversa: K8s → DT.
// existingIDs: conjunto de problem IDs já presentes em DynatraceResults (para deduplicação).
// timeoutSec: orçamento de tempo para toda a busca reversa.
func (c *DynatraceChecker) SearchProblemsForWorkloads(
	ctx context.Context,
	dtURL, dtToken string,
	workloads []string, // nomes de workloads K8s sem namespace ("payment-service")
	cluster string,
	existingIDs map[string]struct{},
	timeoutSec int,
) []DynatraceHealth {
	if len(workloads) == 0 || dtURL == "" {
		return nil
	}

	client, err := dtclient.NewClient(dtURL, dtToken)
	if err != nil {
		log.Error().Err(err).Msg("[DynatraceChecker.Reverse] Falha ao criar client")
		return nil
	}

	searchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// 1. Buscar entidades DT pelo nome dos workloads
	entities := client.SearchEntitiesByName(searchCtx, workloads)
	if len(entities) == 0 {
		log.Debug().Strs("workloads", workloads).Msg("[DynatraceChecker.Reverse] Nenhuma entidade DT encontrada para os workloads K8s")
		return nil
	}

	log.Info().
		Int("entities", len(entities)).
		Strs("workloads", workloads).
		Msg("[DynatraceChecker.Reverse] Entidades DT encontradas para workloads K8s")

	// 2. Buscar problems para cada entidade (paralelo, limite de 5 entidades)
	clusterNorm := normalizeClusterName(cluster)
	maxEntities := 5
	if len(entities) > maxEntities {
		entities = entities[:maxEntities]
	}

	type problemEntry struct {
		p       dtclient.Problem
		enriched []dtclient.EntityStub
	}
	type result struct {
		entries []problemEntry
	}
	results := make([]result, len(entities))
	var wg sync.WaitGroup

	for i, entity := range entities {
		wg.Add(1)
		go func(idx int, e dtclient.EntityStub) {
			defer wg.Done()

			problems, err := client.GetOpenProblemsForEntity(searchCtx, e.EntityID.ID)
			if err != nil {
				log.Debug().Err(err).Str("entityID", e.EntityID.ID).Msg("[DynatraceChecker.Reverse] Erro ao buscar problems")
				return
			}

			entries := make([]problemEntry, 0, len(problems))
			for _, p := range problems {
				if _, skip := existingIDs[p.ProblemID]; skip {
					continue
				}
				enriched := client.EnrichEntitiesWithK8s(searchCtx, p.AffectedEntities)
				entries = append(entries, problemEntry{p: p, enriched: enriched})
			}
			results[idx] = result{entries: entries}
		}(i, entity)
	}
	wg.Wait()

	// 3. Montar DynatraceHealth para problems novos
	toSlice := func(m map[string]struct{}) []string {
		s := make([]string, 0, len(m))
		for k := range m {
			if k != "" {
				s = append(s, k)
			}
		}
		return s
	}

	var newResults []DynatraceHealth
	seen := make(map[string]struct{})

	for _, r := range results {
		for _, entry := range r.entries {
			p := entry.p
			if _, dup := seen[p.ProblemID]; dup {
				continue
			}
			seen[p.ProblemID] = struct{}{}

			workloadSet := make(map[string]struct{})
			nsSet := make(map[string]struct{})
			affectedNames := make([]string, 0, len(entry.enriched))

			for _, e := range entry.enriched {
				if e.DisplayName != "" {
					affectedNames = append(affectedNames, e.DisplayName)
				}
				ns := e.K8sNamespace
				wl := e.K8sWorkload
				if e.Labels != nil {
					if ns == "" {
						ns = e.Labels.Namespace
					}
					if wl == "" {
						wl = e.Labels.AppName
					}
				}
				if wl != "" {
					workloadSet[ns+"/"+wl] = struct{}{}
					nsSet[ns] = struct{}{}
				}
			}

			// Validação de cluster: se a entidade tiver cluster identificado, confirmar
			if clusterNorm != "" {
				hasCluster := false
				for _, e := range entry.enriched {
					if matchesCluster(clusterNorm, e) {
						hasCluster = true
						break
					}
				}
				// Entidade sem K8s tags: aceitar mesmo assim (é o propósito da busca reversa)
				if !hasCluster && len(entry.enriched) > 0 && entry.enriched[0].K8sCluster != "" {
					log.Debug().Str("problemId", p.ProblemID).Msg("[DynatraceChecker.Reverse] Problem descartado — cluster não confere")
					continue
				}
			}

			severity, status := mapDTSeverity(p.SeverityLevel)
			newResults = append(newResults, DynatraceHealth{
				ProblemID:        p.ProblemID,
				DisplayID:        p.DisplayID,
				Title:            p.Title,
				DTSeverity:       p.SeverityLevel,
				ImpactLevel:      p.ImpactLevel,
				Status:           status,
				Severity:         severity,
				StartTime:        p.StartTime,
				K8sNamespaces:    toSlice(nsSet),
				K8sWorkloads:     toSlice(workloadSet),
				AffectedEntities: affectedNames,
				Message:          fmt.Sprintf("[Busca reversa] [%s] %s — impacto: %s", p.SeverityLevel, p.Title, p.ImpactLevel),
				Suggestions:      buildDTSuggestions(p.SeverityLevel, toSlice(workloadSet)),
				CheckedAt:        time.Now(),
			})
		}
	}

	if len(newResults) > 0 {
		log.Info().
			Int("new_problems", len(newResults)).
			Str("cluster", cluster).
			Msg("[DynatraceChecker.Reverse] Problems adicionais encontrados via busca reversa K8s → DT")
	}
	return newResults
}

// buildDTSuggestions gera sugestões baseadas no tipo de problem
func buildDTSuggestions(dtSeverity string, workloads []string) []string {
	suggestions := []string{}

	if len(workloads) > 0 {
		suggestions = append(suggestions, fmt.Sprintf("Verifique os workloads K8s afetados: %s", strings.Join(workloads, ", ")))
	}

	switch dtSeverity {
	case "AVAILABILITY":
		suggestions = append(suggestions, "Verifique o status dos pods: kubectl get pods -n <namespace>")
		suggestions = append(suggestions, "Verifique os logs dos containers com falha: kubectl logs -n <namespace> <pod>")
	case "ERROR":
		suggestions = append(suggestions, "Analise os logs de erro: kubectl logs -n <namespace> <pod> --tail=100")
		suggestions = append(suggestions, "Verifique eventos recentes: kubectl get events -n <namespace> --sort-by='.lastTimestamp'")
	case "PERFORMANCE":
		suggestions = append(suggestions, "Verifique uso de recursos: kubectl top pods -n <namespace>")
		suggestions = append(suggestions, "Considere ajustar limits/requests ou escalar o HPA")
	case "RESOURCE_CONTENTION":
		suggestions = append(suggestions, "Verifique uso de CPU/memória: kubectl top pods -n <namespace>")
		suggestions = append(suggestions, "Verifique se o HPA está no limite máximo de réplicas")
	}

	suggestions = append(suggestions, "Use 'Analisar com AI' na aba Dynatrace para diagnóstico completo")

	return suggestions
}
