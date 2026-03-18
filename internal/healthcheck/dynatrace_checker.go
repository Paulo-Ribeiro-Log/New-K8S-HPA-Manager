package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

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
// Compara K8sCluster e DTLabels.HostGroup, ambos normalizados (sem "-admin").
func matchesCluster(clusterNorm string, e dtclient.EntityStub) bool {
	if strings.EqualFold(normalizeClusterName(e.K8sCluster), clusterNorm) {
		return true
	}
	if e.Labels != nil && strings.EqualFold(normalizeClusterName(e.Labels.HostGroup), clusterNorm) {
		return true
	}
	return false
}

// CheckAll busca problems OPEN no Dynatrace filtrados pelo cluster e pela tag do analista.
// tagFilter (opcional): filtra por management zone ou tag — ex: "SRE-LOGISTICA".
// O filtro de cluster é sempre aplicado (mesmo sem tagFilter) usando normalizeClusterName.
func (c *DynatraceChecker) CheckAll(ctx context.Context, dtURL, dtToken, tagFilter, cluster string, timeoutSec int) []DynatraceHealth {
	results := []DynatraceHealth{}

	client, err := dtclient.NewClient(dtURL, dtToken)
	if err != nil {
		return results
	}

	checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	dtResult, err := client.GetOpenProblems(checkCtx, tagFilter)
	if err != nil {
		return results
	}

	clusterNorm := normalizeClusterName(cluster)

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
		if clusterNorm != "" {
			hasThisCluster := false
			for _, e := range enriched {
				if matchesCluster(clusterNorm, e) {
					hasThisCluster = true
					break
				}
			}
			if !hasThisCluster {
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
	}

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
