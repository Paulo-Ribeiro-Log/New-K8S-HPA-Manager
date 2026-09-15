package finops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestWorkloadHistory_EmptyPodNamesReturnsEmptyNotError confirma que WorkloadHistory nunca chama
// o Prometheus quando não há pods atuais do workload (ex: escalado a 0) — retorna slices vazios,
// não erro, deixando o handler decidir a mensagem "available:false" pro frontend.
func TestWorkloadHistory_EmptyPodNamesReturnsEmptyNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("não deveria disparar nenhuma requisição HTTP com podNames vazio")
	}))
	defer srv.Close()

	e, err := NewPrometheusEnricher(srv.URL, 30, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}

	cpu, mem, err := e.WorkloadHistory(context.Background(), nil, "ns1", 30)
	if err != nil {
		t.Fatalf("esperava nil, veio erro: %v", err)
	}
	if cpu != nil || mem != nil {
		t.Fatalf("esperava slices nil, veio cpu=%v mem=%v", cpu, mem)
	}
}

// TestWorkloadHistory_QueriesIncludeSumByWrapper cobre o mesmo bug real de restart-churn já
// corrigido nas queries agregadas de P95/avg (ver prometheus_enricher_restart_churn_test.go) —
// as 2 queries de série temporal (CPU/Mem) de WorkloadHistory também precisam do wrapper
// `sum by (namespace, pod)` antes de qualquer rate()/valor bruto, senão um pod que reiniciou N
// vezes na janela vira N séries sobrepostas no gráfico em vez de uma linha coerente.
func TestWorkloadHistory_QueriesIncludeSumByWrapper(t *testing.T) {
	var mu sync.Mutex
	var capturedQueries []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		capturedQueries = append(capturedQueries, r.Form.Get("query"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]interface{}{"resultType": "matrix", "result": []interface{}{}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e, err := NewPrometheusEnricher(srv.URL, 30, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}

	if _, _, err := e.WorkloadHistory(context.Background(), []string{"pod1", "pod2"}, "ns1", 30); err != nil {
		t.Fatalf("WorkloadHistory falhou: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(capturedQueries) != 2 {
		t.Fatalf("esperava 2 queries (cpu+mem), vieram %d: %v", len(capturedQueries), capturedQueries)
	}
	for _, q := range capturedQueries {
		if !strings.Contains(q, "sum by (namespace, pod)") {
			t.Errorf("query não tem o wrapper 'sum by (namespace, pod)' — regressão do bug de restart churn.\nquery: %s", q)
		}
		if !strings.Contains(q, `pod=~"^(pod1|pod2)$"`) {
			t.Errorf("query não restringe aos pods atuais do workload (esperado regex ancorado ^(pod1|pod2)$).\nquery: %s", q)
		}
	}
}

// TestWorkloadHistory_ReducesMultiplePodsByMaxPerTimestamp confirma que, com mais de um pod
// (réplica > 1), o ponto de cada timestamp vira o MÁXIMO entre os pods — mesma convenção "pior
// caso entre pods" já usada por aggregatePodToWorkload(useMax=true) pro cálculo de P95, mantendo
// o gráfico semanticamente consistente com o resto do modal.
func TestWorkloadHistory_ReducesMultiplePodsByMaxPerTimestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"namespace": "ns1", "pod": "pod1"},
						"values": [][]interface{}{
							{1700000000, "100"},
							{1700003600, "900"},
						},
					},
					{
						"metric": map[string]string{"namespace": "ns1", "pod": "pod2"},
						"values": [][]interface{}{
							{1700000000, "500"},
							{1700003600, "200"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e, err := NewPrometheusEnricher(srv.URL, 30, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}

	cpu, _, err := e.WorkloadHistory(context.Background(), []string{"pod1", "pod2"}, "ns1", 30)
	if err != nil {
		t.Fatalf("WorkloadHistory falhou: %v", err)
	}
	if len(cpu) != 2 {
		t.Fatalf("esperava 2 pontos (um por timestamp), vieram %d: %+v", len(cpu), cpu)
	}

	byUnix := make(map[int64]float64, len(cpu))
	for _, p := range cpu {
		byUnix[p.Timestamp.Unix()] = p.Value
	}
	if v := byUnix[1700000000]; v != 500 {
		t.Errorf("timestamp 1700000000: esperava max(100,500)=500, veio %v", v)
	}
	if v := byUnix[1700003600]; v != 900 {
		t.Errorf("timestamp 1700003600: esperava max(900,200)=900, veio %v", v)
	}
}
