package healthcheck

import (
	"context"
	"fmt"

	"k8s-hpa-manager/internal/elasticsearch"
)

// ElasticsearchTargetSource (HEALTHCHECK-TRIAGE-MODE-PLAN.md Fase 3) — conta erros de log por
// namespace numa janela curta (últimos 15min por padrão) e sinaliza qualquer namespace com pelo
// menos 1 erro. Contorna o mesmo bloqueio de Grail/Platform que deixa os logs do Dynatrace
// inacessíveis pra praticamente todo mundo (ver seção "Dynatrace" do CLAUDE.md,
// `/api/v2/logs/search` exige OAuth2 client credentials) — acesso direto ao Elasticsearch via
// Basic Auth, sem essa exigência.
type ElasticsearchTargetSource struct {
	url          string
	username     string
	password     string
	indexPattern string
	// ignoredNamespaces (Fase 4 — TriageIgnoreManager): namespaces que nunca devem entrar no
	// escopo por essa fonte, mesmo com erro real no período. Diferente de Dynatrace/Prometheus
	// (que suprimem por nome de alerta/problem), aqui o "sinal" já É o namespace — não existe um
	// nome de alerta/regra separado pra suprimir.
	ignoredNamespaces map[string]struct{}
}

// NewElasticsearchTargetSource cria a fonte de triagem Elasticsearch.
func NewElasticsearchTargetSource(url, username, password, indexPattern string, ignoredNamespaces map[string]struct{}) *ElasticsearchTargetSource {
	return &ElasticsearchTargetSource{
		url:               url,
		username:          username,
		password:          password,
		indexPattern:      indexPattern,
		ignoredNamespaces: ignoredNamespaces,
	}
}

func (s *ElasticsearchTargetSource) Name() string {
	return "Elasticsearch"
}

func (s *ElasticsearchTargetSource) Resolve(ctx context.Context, cluster string) TargetSourceResult {
	if s.url == "" || s.username == "" || s.password == "" {
		return TargetSourceResult{Available: false}
	}

	client := elasticsearch.NewClient(s.url, s.username, s.password, s.indexPattern)
	counts, err := client.NamespaceErrorCounts(ctx, cluster, elasticsearch.DefaultTimeWindow)
	if err != nil {
		return TargetSourceResult{Available: false, Err: err}
	}

	namespaces := make([]string, 0, len(counts))
	reasons := make(map[string][]string, len(counts))
	for ns, count := range counts {
		if ns == "" || count == 0 {
			continue
		}
		if _, ignored := s.ignoredNamespaces[ns]; ignored {
			continue
		}
		namespaces = append(namespaces, ns)
		reasons[ns] = []string{fmt.Sprintf("Elasticsearch: %d erro(s) em %s", count, elasticsearch.DefaultTimeWindow)}
	}

	// Available=true mesmo quando namespaces vem vazio — a chamada funcionou, só não achou
	// erro nenhum na janela (bom sinal, ver TargetSourceResult).
	return TargetSourceResult{Available: true, Namespaces: namespaces, Reasons: reasons}
}
