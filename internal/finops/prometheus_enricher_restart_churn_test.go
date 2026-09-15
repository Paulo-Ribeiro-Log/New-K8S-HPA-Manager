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

// TestEnrichWorkloads_P95AndAvgQueriesAggregateByPod cobre um bug real CRÍTICO, achado ao vivo
// (relatado pelo usuário com números impossíveis — "Mem: 519%" num pool de 1 node): as queries
// de P95/avg (CPU e Mem) nunca tinham o wrapper `sum by (namespace, pod) (...)` que as queries de
// MAX/pico já usam (ver queryPodMetricPeakValue) — sem ele, o seletor casa com uma série POR
// CGROUP ("id", que muda a cada restart/recriação do container: cAdvisor/kubelet expõem um novo
// cgroup id por instância física). Um pod que reiniciou N vezes na janela histórica faz
// quantile_over_time/avg_over_time calcular o valor INDEPENDENTEMENTE pra CADA série (uma por
// cgroup id), e o pós-processamento em Go soma todas elas — Nx o valor real. Confirmado ao vivo
// contra um Prometheus real: um pod com 24 reinícios em 30d tinha quantile_over_time SEM o
// wrapper devolvendo 24 valores distintos (somando ~31,5GB); COM `sum by (namespace, pod)`
// aplicado antes do quantile_over_time, a mesma query devolveu 1 única série coerente (~1,5GB).
//
// Este teste confirma que as 4 queries (cpu_p95, cpu_avg, mem_p95, mem_avg) geradas por
// EnrichWorkloads sempre incluem `sum by (namespace, pod)` envolvendo a métrica bruta.
func TestEnrichWorkloads_P95AndAvgQueriesAggregateByPod(t *testing.T) {
	var mu sync.Mutex
	queriesByLabel := map[string]string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		selector := r.Form.Get("query")
		mu.Lock()
		switch {
		case strings.Contains(selector, "quantile_over_time") && strings.Contains(selector, "container_cpu_usage_seconds_total"):
			queriesByLabel["cpu_p95"] = selector
		case strings.Contains(selector, "avg_over_time") && strings.Contains(selector, "container_cpu_usage_seconds_total"):
			queriesByLabel["cpu_avg"] = selector
		case strings.Contains(selector, "quantile_over_time") && strings.Contains(selector, "container_memory_working_set_bytes"):
			queriesByLabel["mem_p95"] = selector
		case strings.Contains(selector, "avg_over_time") && strings.Contains(selector, "container_memory_working_set_bytes"):
			queriesByLabel["mem_avg"] = selector
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"namespace": "ns1", "pod": "pod1"},
						"value":  []interface{}{1700000000, "100"},
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
	e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})

	workloads := []FinOpsWorkload{{Namespace: "ns1", Workload: "wl1", CPURequestMillis: 1000, MemRequestMi: 1000}}
	e.EnrichWorkloads(context.Background(), workloads)

	for _, label := range []string{"cpu_p95", "cpu_avg", "mem_p95", "mem_avg"} {
		mu.Lock()
		q, ok := queriesByLabel[label]
		mu.Unlock()
		if !ok {
			t.Errorf("query %q nunca foi disparada", label)
			continue
		}
		if !strings.Contains(q, "sum by (namespace, pod)") {
			t.Errorf("query %q não tem o wrapper 'sum by (namespace, pod)' — regressão do bug de restart churn.\nquery: %s", label, q)
		}
	}
}
