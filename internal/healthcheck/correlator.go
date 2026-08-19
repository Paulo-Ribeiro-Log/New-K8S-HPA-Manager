package healthcheck

import (
	"context"
	"sort"
	"strings"
)

// workloadKey é a chave normalizada para lookup: (namespace lowercase, workload-name lowercase sem porta)
type workloadKey struct {
	namespace string
	name      string
}

// newWorkloadKey cria uma chave normalizada para matching bidirecional K8s ↔ Dynatrace.
// Remove sufixo de porta (ex: ":8080") e normaliza para lowercase.
func newWorkloadKey(namespace, name string) workloadKey {
	n := strings.ToLower(strings.TrimSpace(name))
	if idx := strings.LastIndex(n, ":"); idx > 0 {
		n = n[:idx]
	}
	return workloadKey{
		namespace: strings.ToLower(strings.TrimSpace(namespace)),
		name:      n,
	}
}

// statusToSeverity converte HealthStatus para Severity (para recursos K8s sem campo Severity próprio)
func statusToSeverity(s HealthStatus) Severity {
	switch s {
	case StatusCritical:
		return SeverityCritical
	case StatusWarning:
		return SeverityMedium
	default:
		return SeverityInfo
	}
}

// maxSeverityFromIssues retorna a maior severidade entre os issues K8s de um workload
func maxSeverityFromIssues(issues []CorrelatedK8sIssue) Severity {
	max := SeverityInfo
	for _, i := range issues {
		if i.Severity.Weight() > max.Weight() {
			max = i.Severity
		}
	}
	return max
}

// maxSeverityFromDT retorna a maior severidade entre os DynatraceHealth de um workload
func maxSeverityFromDT(problems []DynatraceHealth) Severity {
	max := SeverityInfo
	for _, p := range problems {
		if p.Severity.Weight() > max.Weight() {
			max = p.Severity
		}
	}
	return max
}

// Correlate correlaciona os resultados K8s com os problems Dynatrace de um HealthCheckResult.
// Retorna CorrelatedHealthItem para:
//   - workloads com issues nos dois lados (K8s + DT) — Correlated=true
//   - workloads com issues apenas no K8s — Correlated=false, DTProblems vazio
//   - workloads com problems DT sem issue K8s correspondente — Correlated=false, K8sIssues vazio
//
// Só é chamado quando DynatraceResults está preenchido (check_dynatrace=true).
//
// store é usado para consultar o histórico crônico/agudo de cada evento (ver EventChronicity) —
// pode ser nil (ex: testes), caso em que a classificação simplesmente não é preenchida.
func Correlate(ctx context.Context, store *HealthCheckStorage, result *HealthCheckResult) []CorrelatedHealthItem {
	if result == nil || len(result.DynatraceResults) == 0 {
		return nil
	}

	// 1. Coletar workloads K8s com problemas ────────────────────────────────────
	k8sIssuesMap := make(map[workloadKey][]CorrelatedK8sIssue)
	// originalName preserva o nome com caixa original para exibição
	originalNameMap := make(map[workloadKey]string)

	addK8sIssue := func(namespace, workloadName string, issue CorrelatedK8sIssue) {
		k := newWorkloadKey(namespace, workloadName)
		k8sIssuesMap[k] = append(k8sIssuesMap[k], issue)
		if _, ok := originalNameMap[k]; !ok {
			originalNameMap[k] = workloadName
		}
	}

	// Deployments com problemas
	for _, d := range result.DeploymentResults {
		if d.Status == StatusHealthy {
			continue
		}
		addK8sIssue(d.Namespace, d.Name, CorrelatedK8sIssue{
			ResourceKind:            "Deployment",
			ResourceName:            d.Name,
			Status:                  d.Status,
			Message:                 d.Message,
			Severity:                statusToSeverity(d.Status),
			Suggestions:             d.Suggestions,
			ResourceVerdict:         d.ResourceVerdict,
			CPUUsagePercent:         d.CPUUsagePercent,
			MemoryUsagePercent:      d.MemoryUsagePercent,
			SpinnakerRecentRollback: d.SpinnakerRecentRollback,
			SpinnakerRollbackCHG:    d.SpinnakerRollbackCHG,
			SpinnakerRollbackAt:     d.SpinnakerRollbackAt,
		})
	}

	// HPAs com problemas (correlaciona pelo TargetName — o deployment/StatefulSet sendo escalado)
	for _, h := range result.HPAResults {
		if h.Status == StatusHealthy {
			continue
		}
		// Usar pior severidade dos Issues do HPA, ou fallback por Status
		sev := statusToSeverity(h.Status)
		for _, issue := range h.Issues {
			if issue.Severity.Weight() > sev.Weight() {
				sev = issue.Severity
			}
		}
		workload := h.TargetName
		if workload == "" {
			workload = h.Name
		}
		addK8sIssue(h.Namespace, workload, CorrelatedK8sIssue{
			ResourceKind: "HPA",
			ResourceName: h.Name,
			Status:       h.Status,
			Message:      h.Message,
			Severity:     sev,
			Suggestions:  h.Suggestions,
		})
	}

	// Eventos K8s críticos (apenas Deployment — pods são efêmeros e difíceis de correlacionar)
	for _, e := range result.EventResults {
		if e.Status == StatusHealthy || e.InvolvedKind != "Deployment" {
			continue
		}
		issue := CorrelatedK8sIssue{
			ResourceKind:   "Event",
			ResourceName:   e.InvolvedName,
			Status:         e.Status,
			Message:        e.Message,
			Severity:       e.Severity,
			Suggestions:    e.Suggestions,
			Count:          e.Count,
			FirstTimestamp: e.FirstTimestamp,
		}
		if store != nil {
			if chronicity, err := store.GetEventChronicity(ctx, result.Cluster, e.Namespace, e.InvolvedKind, e.InvolvedName, e.Reason); err == nil {
				issue.Chronicity = chronicity
			}
		}
		addK8sIssue(e.Namespace, e.InvolvedName, issue)
	}

	// 2. Coletar DT problems por workload ───────────────────────────────────────
	dtByWorkload := make(map[workloadKey][]DynatraceHealth)
	dtOriginalNameMap := make(map[workloadKey]string)

	for _, d := range result.DynatraceResults {
		for _, w := range d.K8sWorkloads {
			// formato: "namespace/workload"
			parts := strings.SplitN(w, "/", 2)
			if len(parts) != 2 {
				continue
			}
			k := newWorkloadKey(parts[0], parts[1])
			dtByWorkload[k] = append(dtByWorkload[k], d)
			if _, ok := dtOriginalNameMap[k]; !ok {
				dtOriginalNameMap[k] = parts[1]
			}
		}
	}

	// 3. Merge: union de todas as chaves ────────────────────────────────────────
	allKeys := make(map[workloadKey]struct{})
	for k := range k8sIssuesMap {
		allKeys[k] = struct{}{}
	}
	for k := range dtByWorkload {
		allKeys[k] = struct{}{}
	}

	items := make([]CorrelatedHealthItem, 0, len(allKeys))
	for k := range allKeys {
		k8sIssues := k8sIssuesMap[k]
		dtProblems := dtByWorkload[k]

		k8sSev := maxSeverityFromIssues(k8sIssues)
		dtSev := maxSeverityFromDT(dtProblems)

		// FinalSeverity: pior dos dois lados
		finalSev := k8sSev
		if dtSev.Weight() > finalSev.Weight() {
			finalSev = dtSev
		}
		// Escalada: se ambos os lados confirmam problema com severidade >= High → Critical
		if len(k8sIssues) > 0 && len(dtProblems) > 0 &&
			k8sSev.Weight() >= SeverityHigh.Weight() &&
			dtSev.Weight() >= SeverityHigh.Weight() {
			finalSev = SeverityCritical
		}

		// Resolver nome original para exibição
		displayName := k.name
		if n, ok := originalNameMap[k]; ok {
			displayName = n
		} else if n, ok := dtOriginalNameMap[k]; ok {
			displayName = n
		}

		items = append(items, CorrelatedHealthItem{
			WorkloadName:  displayName,
			Namespace:     k.namespace,
			Cluster:       result.Cluster,
			K8sIssues:     k8sIssues,
			K8sSeverity:   k8sSev,
			DTProblems:    dtProblems,
			DTSeverity:    dtSev,
			FinalSeverity: finalSev,
			Correlated:    len(k8sIssues) > 0 && len(dtProblems) > 0,
		})
	}

	// Ordenar: correlacionados primeiro, depois por FinalSeverity descendente
	sort.Slice(items, func(i, j int) bool {
		if items[i].Correlated != items[j].Correlated {
			return items[i].Correlated // correlated first
		}
		return items[i].FinalSeverity.Weight() > items[j].FinalSeverity.Weight()
	})

	return items
}
