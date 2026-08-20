package healthcheck

import (
	"context"
	"fmt"
	"sort"
)

// TargetSource resolve, uma vez por cluster, quais namespaces têm algo sinalizado por uma fonte
// externa de monitoramento (Dynatrace, Prometheus/Alertmanager, e futuramente Zabbix — ver
// HEALTHCHECK-TRIAGE-MODE-PLAN.md). Implementações nunca devem ser fatais: uma fonte
// indisponível/mal configurada retorna Available=false, nunca um erro que aborte o Health Check.
type TargetSource interface {
	Name() string
	Resolve(ctx context.Context, cluster string) TargetSourceResult
}

// TargetSourceResult é o resultado de UMA fonte para um cluster.
//
// Available=false é deliberadamente distinto de Namespaces vazio — o primeiro significa "sem dado
// desta fonte" (não configurada, ou a chamada falhou), o segundo significa "a fonte respondeu e
// não achou nenhum problema" (resultado bom, não ausência de dado). resolveTriageTargets depende
// dessa distinção pra decidir entre "escopo reduzido" e "cair pra Varredura Completa".
type TargetSourceResult struct {
	Available  bool
	Namespaces []string
	Reasons    map[string][]string // namespace → motivos (ex: "Dynatrace: problem X (ERROR)")
	Err        error               // só informativo/log — não usado pra decisão, ver Available
}

// TriageResolution é o agregado de rodar todas as TargetSource configuradas para um cluster.
type TriageResolution struct {
	AnyAvailable bool
	Namespaces   []string // união ordenada dos namespaces sinalizados pelas fontes disponíveis
	Reasons      map[string][]string
	Sources      []TriageSourceStatus
}

// resolveTriageTargets executa cada fonte (sequencialmente — o custo de cada uma já é uma
// chamada de rede síncrona única por cluster, mesmo padrão de custo do ResourceEnricher/
// SpinnakerEnricher; paralelizar aqui não é necessário na Fase 1 com só 2 fontes) e agrega o
// resultado. Nunca retorna erro — cada fonte já trata a própria falha via Available=false.
func resolveTriageTargets(ctx context.Context, cluster string, sources []TargetSource) TriageResolution {
	nsSet := make(map[string]struct{})
	// reasonCounts: namespace → texto do motivo → quantas vezes apareceu. Achado real via
	// validação ao vivo (2026-08-20): Prometheus dispara UM alerta por objeto afetado (um
	// KubePodNotReady por pod, um KubeHpaMaxedOut por HPA) — todos com o mesmo alertname/severity,
	// então viravam o mesmo texto de motivo repetido dezenas de vezes na UI (ex: "KubePodNotReady
	// (warning)" 9x seguidas para o mesmo namespace). Agregado aqui, no ponto único de merge entre
	// fontes, em vez de em cada TargetSource — cobre automaticamente qualquer fonte futura
	// (Zabbix/Elasticsearch) sem precisar duplicar a lógica de dedup em cada uma.
	reasonCounts := make(map[string]map[string]int)
	statuses := make([]TriageSourceStatus, 0, len(sources))
	anyAvailable := false

	for _, src := range sources {
		res := src.Resolve(ctx, cluster)

		status := TriageSourceStatus{
			Name:       src.Name(),
			Available:  res.Available,
			Namespaces: res.Namespaces,
		}
		if res.Err != nil {
			status.Error = res.Err.Error()
		}
		statuses = append(statuses, status)

		if !res.Available {
			continue
		}
		anyAvailable = true

		for _, ns := range res.Namespaces {
			nsSet[ns] = struct{}{}
		}
		for ns, rs := range res.Reasons {
			if reasonCounts[ns] == nil {
				reasonCounts[ns] = make(map[string]int)
			}
			for _, r := range rs {
				reasonCounts[ns][r]++
			}
		}
	}

	namespaces := make([]string, 0, len(nsSet))
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	// Achata reasonCounts em Reasons final — cada motivo único uma vez só, com "(×N)" quando
	// repetiu (N>1). Ordenado por texto pra saída determinística (testes, UI estável entre runs).
	reasons := make(map[string][]string, len(reasonCounts))
	for ns, counts := range reasonCounts {
		list := make([]string, 0, len(counts))
		for text, count := range counts {
			if count > 1 {
				text = fmt.Sprintf("%s (×%d)", text, count)
			}
			list = append(list, text)
		}
		sort.Strings(list)
		reasons[ns] = list
	}

	return TriageResolution{
		AnyAvailable: anyAvailable,
		Namespaces:   namespaces,
		Reasons:      reasons,
		Sources:      statuses,
	}
}

// intersectOrUse aplica um filtro manual do usuário (req.Namespaces) por cima do escopo resolvido
// pela triagem — só os namespaces presentes nos dois. Sem filtro manual, usa o escopo resolvido
// inteiro. Preserva a ordem de `resolved` (já vem ordenada por resolveTriageTargets).
func intersectOrUse(userFilter, resolved []string) []string {
	if len(userFilter) == 0 {
		return resolved
	}
	resolvedSet := make(map[string]struct{}, len(resolved))
	for _, ns := range resolved {
		resolvedSet[ns] = struct{}{}
	}
	out := make([]string, 0, len(userFilter))
	for _, ns := range userFilter {
		if _, ok := resolvedSet[ns]; ok {
			out = append(out, ns)
		}
	}
	return out
}
