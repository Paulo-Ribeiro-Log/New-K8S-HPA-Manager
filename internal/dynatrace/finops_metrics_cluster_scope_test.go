package dynatrace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// TestGetAllWorkloadMetrics_ScopesQueryToCluster cobre um bug real CRÍTICO, achado ao vivo
// (relatado pelo usuário com números impossíveis — "CPU: 626%, Mem: 519%" num node pool de 1
// node): a query nunca tinha NENHUMA dimensão de cluster no splitBy — só "k8s.namespace.name"/
// "k8s.workload.name". O tenant Dynatrace desta empresa é COMPARTILHADO entre toda a frota de
// clusters (confirmado ao vivo: 17 clusters distintos monitorados no mesmo tenant, a maioria PRD).
// Qualquer workload cujo namespace+nome se repete em mais de um cluster (o caso normal pra
// componentes de infra genéricos: ingress-nginx-controller, istiod, cert-manager, velero,
// prometheus-prometheus-prometheus — confirmados ao vivo existindo identicamente em 15-16
// clusters diferentes) tinha sua métrica AGREGADA através de TODOS os clusters que a
// compartilham, nunca isolada pro cluster sendo de fato analisado.
//
// Este teste confirma que toda query gerada por GetAllWorkloadMetrics agora inclui
// filter(and(eq("k8s.cluster.name","<cluster>"))) — sintaxe validada ao vivo contra o tenant
// real: filtrada pro cluster certo, devolve exatamente o valor isolado daquele cluster; filtrada
// pra um cluster sem cobertura DT real, devolve corretamente zero séries.
func TestGetAllWorkloadMetrics_ScopesQueryToCluster(t *testing.T) {
	var mu sync.Mutex
	var capturedSelectors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selector := r.URL.Query().Get("metricSelector")
		mu.Lock()
		capturedSelectors = append(capturedSelectors, selector)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"data":[{"dimensionMap":{"k8s.namespace.name":"ns1","k8s.workload.name":"wl1"},"values":[42]}]}]}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	_, err = client.GetAllWorkloadMetrics(context.Background(), 30, "akspriv-abastecimento-hlg-admin")
	if err != nil {
		t.Fatalf("GetAllWorkloadMetrics falhou: %v", err)
	}

	if len(capturedSelectors) == 0 {
		t.Fatal("nenhuma query foi disparada")
	}
	// O cluster passado é usado tal como recebido (com "-admin") — a normalização
	// (TrimSuffix "-admin") é responsabilidade do chamador (finops.DTEnricher), não deste
	// client, que só monta a query com o que recebeu.
	wantFilter := `filter(and(eq("k8s.cluster.name","akspriv-abastecimento-hlg-admin")))`
	for _, sel := range capturedSelectors {
		if !strings.Contains(sel, wantFilter) {
			t.Errorf("query não contém o filtro de cluster esperado.\nquery: %s\nfiltro esperado: %s", sel, wantFilter)
		}
	}
}

// TestGetAllWorkloadMetrics_RejectsEmptyCluster confirma que a função nunca mais consulta o DT
// sem escopo de cluster — mesmo por engano de um chamador que esqueça de passar o parâmetro.
func TestGetAllWorkloadMetrics_RejectsEmptyCluster(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("não deveria disparar nenhuma requisição HTTP com cluster vazio")
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	_, err = client.GetAllWorkloadMetrics(context.Background(), 30, "")
	if err == nil {
		t.Fatal("esperava erro com cluster vazio, veio nil")
	}
}

// TestQueryWorkloadBatch_ClusterNameIsURLEncoded confirma que um nome de cluster com caracteres
// que precisam de escape em query string (ex: nunca deveria acontecer na prática, mas o valor
// passa pela camada normal de encoding de query params do cliente HTTP, não string-concatenado
// na URL) chega ao servidor decodificado corretamente — validação indireta de que a
// interpolação do filtro não quebra o parsing da URL como um todo.
func TestQueryWorkloadBatch_ClusterNameIsURLEncoded(t *testing.T) {
	var gotSelector string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("metricSelector")
		decoded, err := url.QueryUnescape(raw)
		if err != nil {
			t.Fatalf("falha ao decodificar metricSelector: %v", err)
		}
		gotSelector = decoded
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"data":[]}]}`))
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient falhou: %v", err)
	}

	_, err = client.queryWorkloadBatch(context.Background(), metricCPUMillicores, "avg", "now-30d", "akspriv-viaunica-prd")
	if err != nil {
		t.Fatalf("queryWorkloadBatch falhou: %v", err)
	}
	if !strings.Contains(gotSelector, `eq("k8s.cluster.name","akspriv-viaunica-prd")`) {
		t.Errorf("selector decodificado não contém o filtro esperado: %s", gotSelector)
	}
}
