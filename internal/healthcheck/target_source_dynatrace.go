package healthcheck

import (
	"context"
	"fmt"
)

// DynatraceTargetSource reaproveita DynatraceChecker.CheckAll (zero mudança de lógica — mesma
// checagem de problems OPEN já usada pelo check "K8s↔DT" tradicional) só pra extrair, a partir de
// DynatraceHealth.K8sNamespaces, quais namespaces têm algo sinalizado. Available=false quando
// URL/token não estão configurados para este cluster, ou quando a chamada real falhou (erro
// propagado por CheckAll) — nos dois casos a triagem não pode confiar neste sinal.
type DynatraceTargetSource struct {
	checker    *DynatraceChecker
	url        string
	token      string
	tagFilter  string
	timeoutSec int
}

// NewDynatraceTargetSource cria a fonte de triagem Dynatrace. checker é o mesmo *DynatraceChecker
// já usado pelo orchestrator (sem estado próprio, seguro reaproveitar).
func NewDynatraceTargetSource(checker *DynatraceChecker, url, token, tagFilter string, timeoutSec int) *DynatraceTargetSource {
	return &DynatraceTargetSource{checker: checker, url: url, token: token, tagFilter: tagFilter, timeoutSec: timeoutSec}
}

func (s *DynatraceTargetSource) Name() string {
	return "Dynatrace"
}

func (s *DynatraceTargetSource) Resolve(ctx context.Context, cluster string) TargetSourceResult {
	if s.url == "" || s.token == "" {
		return TargetSourceResult{Available: false}
	}

	problems, err := s.checker.CheckAll(ctx, s.url, s.token, s.tagFilter, cluster, s.timeoutSec)
	if err != nil {
		return TargetSourceResult{Available: false, Err: err}
	}

	nsSet := make(map[string]struct{})
	reasons := make(map[string][]string)
	for _, p := range problems {
		reason := fmt.Sprintf("Dynatrace: %s (%s)", p.Title, p.DTSeverity)
		for _, ns := range p.K8sNamespaces {
			if ns == "" {
				continue
			}
			nsSet[ns] = struct{}{}
			reasons[ns] = append(reasons[ns], reason)
		}
	}

	namespaces := make([]string, 0, len(nsSet))
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}

	// Available=true mesmo quando namespaces vem vazio — a chamada funcionou, só não achou
	// problem nenhum sinalizando um namespace (bom sinal, ver TargetSourceResult).
	return TargetSourceResult{Available: true, Namespaces: namespaces, Reasons: reasons}
}
