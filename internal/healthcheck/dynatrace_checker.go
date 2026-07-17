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

	dtResult, err := client.GetOpenProblems(checkCtx, tagFilter, "OPEN", "", "")
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
		// Descartar problems de entidades puramente web/RUM (APPLICATION, BROWSER, SYNTHETIC_TEST, etc.)
		// que não têm relação com infraestrutura K8s/serviços.
		if isWebOnlyProblem(p.AffectedEntities) {
			log.Debug().Str("problemId", p.ProblemID).Str("title", p.Title).
				Msg("[DynatraceChecker] Problem descartado: apenas entidades web/RUM (APPLICATION/BROWSER/SYNTHETIC)")
			continue
		}

		// Enriquecer entidades com tags K8s + DTLabels (squad, journey, version, GitHub, etc.)
		enriched := client.EnrichEntitiesWithK8s(checkCtx, p.AffectedEntities)

		// Filtrar por cluster com 3 níveis de confiança:
		//  1. Entidade com K8sCluster/HostGroup/DisplayName matching → inclui
		//  2. Entidade com K8sCluster/HostGroup de outro cluster → exclui
		//  3. Sem info de cluster nas entidades → fallback: Management Zone deve conter cluster name
		if clusterNorm != "" {
			hasThisCluster := false
			hasOtherCluster := false
			for _, e := range enriched {
				if matchesCluster(clusterNorm, e) {
					hasThisCluster = true
					break
				}
				eCluster := normalizeClusterName(e.K8sCluster)
				hg := ""
				if e.Labels != nil {
					hg = normalizeClusterName(e.Labels.HostGroup)
				}
				if (eCluster != "" && eCluster != clusterNorm) || (hg != "" && hg != clusterNorm) {
					hasOtherCluster = true
				}
			}

			if !hasThisCluster {
				if hasOtherCluster {
					// Entidades explicitamente de outro cluster — descartar
					log.Debug().Str("problemId", p.ProblemID).Str("title", p.Title).
						Msg("[DynatraceChecker] Problem descartado: entidades pertencem a outro cluster")
					continue
				}
				// Sem info de cluster nas entidades — usar Management Zones como fallback
				mzMatch := false
				for _, mz := range p.ManagementZones {
					if strings.Contains(strings.ToLower(mz.Name), clusterNorm) {
						mzMatch = true
						break
					}
				}
				if !mzMatch {
					log.Debug().Str("problemId", p.ProblemID).Str("title", p.Title).
						Strs("mzNames", func() []string {
							names := make([]string, len(p.ManagementZones))
							for i, mz := range p.ManagementZones {
								names[i] = mz.Name
							}
							return names
						}()).
						Msg("[DynatraceChecker] Problem descartado: sem match por entidade nem por management zone")
					continue
				}
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

	// Métricas para AVAILABILITY, ERROR e PERFORMANCE (severidades onde CPU/memória de
	// node/pod tende a explicar a causa raiz — ex: pressão de recursos gerando degradação)
	metricsCtx, metricsCancel := context.WithTimeout(ctx, time.Duration(metricsTimeoutSec)*time.Second)
	defer metricsCancel()

	var mwg sync.WaitGroup
	for _, idx := range indices {
		sev := results[idx].DTSeverity
		if sev != "AVAILABILITY" && sev != "ERROR" && sev != "PERFORMANCE" {
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
					default:
						// Demais chaves (cpu_milli, memory_mb, node_cpu, node_pods, pod_restarts,
						// host_cpu, host_mem_avail etc. — ver metricsForEntityType em
						// internal/dynatrace/metrics.go) eram descartadas aqui antes; agora ficam
						// disponíveis pro prompt de IA sob a própria chave da métrica.
						if maxVal > summary[series.Key] {
							summary[series.Key] = maxVal
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
// workloads: lista com nome + namespace dos workloads K8s problemáticos.
// existingIDs: conjunto de problem IDs já presentes em DynatraceResults (para deduplicação).
// timeoutSec: orçamento de tempo para toda a busca reversa.
func (c *DynatraceChecker) SearchProblemsForWorkloads(
	ctx context.Context,
	dtURL, dtToken string,
	workloads []TroubledWorkloadInfo,
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

	// Mapa nome → namespace para fallback de correlação quando entidade DT não tem tags K8s
	// (ex: APPLICATION "TMS Embarcador" encontrada pela busca de "tms-embarcador")
	workloadNSMap := make(map[string]string, len(workloads))
	names := make([]string, len(workloads))
	for i, tw := range workloads {
		names[i] = tw.Name
		workloadNSMap[strings.ToLower(tw.Name)] = tw.Namespace
	}

	// 1. Buscar entidades DT pelo nome dos workloads (múltiplas estratégias: contains, tokens AND)
	entities := client.SearchEntitiesByName(searchCtx, names)
	if len(entities) == 0 {
		log.Debug().Strs("workloads", names).Msg("[DynatraceChecker.Reverse] Nenhuma entidade DT encontrada para os workloads K8s")
		return nil
	}

	log.Info().
		Int("entities", len(entities)).
		Strs("workloads", names).
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

			// Fallback: entidade sem tags K8s (ex: APPLICATION "TMS Embarcador").
			// Verifica se o display name da entidade contém tokens de algum workload pesquisado,
			// e usa esse workload + namespace original como referência de correlação.
			if len(workloadSet) == 0 {
				for _, e := range entry.enriched {
					displayLower := strings.ToLower(strings.ReplaceAll(e.DisplayName, " ", "-"))
					for wName, wNS := range workloadNSMap {
						// match se display name contém o nome do workload ou vice-versa
						if strings.Contains(displayLower, wName) || strings.Contains(wName, displayLower) {
							workloadSet[wNS+"/"+wName] = struct{}{}
							nsSet[wNS] = struct{}{}
							log.Debug().
								Str("entity", e.DisplayName).
								Str("workload", wName).
								Str("namespace", wNS).
								Msg("[DynatraceChecker.Reverse] Correlação por fallback de nome (sem tags K8s)")
							break
						}
					}
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

// buildDTSuggestions gera sugestões de navegação no HPA Manager baseadas no tipo de problem.
// Cada sugestão indica qual aba/seção acessar e qual ação executar — sem comandos kubectl.
func buildDTSuggestions(dtSeverity string, workloads []string) []string {
	suggestions := []string{}

	// Extrair namespace e workload do primeiro item para sugestões contextuais
	firstNS, firstWL := "", ""
	if len(workloads) > 0 {
		parts := strings.SplitN(workloads[0], "/", 2)
		if len(parts) == 2 {
			firstNS, firstWL = parts[0], parts[1]
		} else {
			firstWL = workloads[0]
		}
	}

	// Formata referência contextual se namespace conhecido
	ref := func(section string) string {
		if firstNS != "" && firstWL != "" {
			return fmt.Sprintf("Aba %s → namespace %q → workload %q", section, firstNS, firstWL)
		} else if firstWL != "" {
			return fmt.Sprintf("Aba %s → workload %q", section, firstWL)
		}
		return fmt.Sprintf("Aba %s", section)
	}

	switch dtSeverity {
	case "AVAILABILITY":
		suggestions = append(suggestions, ref("Deployments")+" → verificar pods em CrashLoop ou Pending")
		suggestions = append(suggestions, ref("HPA")+" → verificar réplicas e escalar se necessário")
	case "ERROR":
		suggestions = append(suggestions, ref("Deployments")+" → verificar logs e considerar rollback")
		suggestions = append(suggestions, ref("Health Check")+" → executar verificação detalhada de eventos recentes")
	case "PERFORMANCE":
		suggestions = append(suggestions, ref("HPA")+" → aumentar maxReplicas para absorver carga")
		suggestions = append(suggestions, ref("Resource Explorer")+" → revisar CPU/memory limits do container")
	case "RESOURCE_CONTENTION":
		suggestions = append(suggestions, ref("Resource Explorer")+" → aumentar CPU/memory requests e limits")
		suggestions = append(suggestions, ref("HPA")+" → verificar se está no limite de réplicas configurado")
	default:
		suggestions = append(suggestions, ref("Health Check")+" → executar verificação detalhada")
	}

	suggestions = append(suggestions, "Aba Dynatrace → 'Analisar com IA' para diagnóstico completo com métricas")

	return suggestions
}

// webOnlyEntityTypes são tipos de entidade Dynatrace que representam experiência web/RUM/sintético,
// sem relação com infraestrutura K8s ou serviços de backend.
var webOnlyEntityTypes = map[string]struct{}{
	"APPLICATION":        {},
	"BROWSER":            {},
	"SYNTHETIC_TEST":     {},
	"SYNTHETIC_MONITOR":  {},
	"HTTP_CHECK":         {},
	"MOBILE_APPLICATION": {},
	"WEB_APPLICATION":    {},
}

// isWebOnlyProblem retorna true se TODAS as entidades afetadas são tipos web/RUM/sintético.
// Esses problems não têm relação com K8s e devem ser ignorados no Health Check.
func isWebOnlyProblem(entities []dtclient.EntityStub) bool {
	if len(entities) == 0 {
		return false
	}
	for _, e := range entities {
		if _, isWeb := webOnlyEntityTypes[strings.ToUpper(e.EntityID.Type)]; !isWeb {
			return false // tem pelo menos uma entidade não-web → não é web-only
		}
	}
	return true
}
