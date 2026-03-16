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

// CheckAll busca problems OPEN no Dynatrace filtrados pelo cluster e pela tag do analista.
// tagFilter (opcional): filtra apenas problems com essa tag — ex: "SRE-LOGISTICA".
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

	for _, p := range dtResult.Problems {
		// Enriquecer entidades com tags K8s do OneAgent
		enriched := client.EnrichEntitiesWithK8s(checkCtx, p.AffectedEntities)

		// Filtrar por cluster (quando cluster está especificado)
		if cluster != "" {
			hasThisCluster := false
			for _, e := range enriched {
				if strings.EqualFold(e.K8sCluster, cluster) {
					hasThisCluster = true
					break
				}
			}
			if !hasThisCluster {
				continue
			}
		}

		// Coletar K8s workloads únicos deste problem
		workloadSet := make(map[string]struct{})
		nsSet := make(map[string]struct{})
		affectedNames := make([]string, 0, len(enriched))
		for _, e := range enriched {
			affectedNames = append(affectedNames, e.DisplayName)
			if e.K8sWorkload != "" {
				workloadSet[e.K8sNamespace+"/"+e.K8sWorkload] = struct{}{}
				nsSet[e.K8sNamespace] = struct{}{}
			}
		}

		workloads := make([]string, 0, len(workloadSet))
		for w := range workloadSet {
			workloads = append(workloads, w)
		}
		namespaces := make([]string, 0, len(nsSet))
		for ns := range nsSet {
			namespaces = append(namespaces, ns)
		}

		severity, status := mapDTSeverity(p.SeverityLevel)

		msg := fmt.Sprintf("[%s] %s — impacto: %s", p.SeverityLevel, p.Title, p.ImpactLevel)
		suggestions := buildDTSuggestions(p.SeverityLevel, workloads)

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
			Message:          msg,
			Suggestions:      suggestions,
			CheckedAt:        time.Now(),
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
