package finops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakePrometheusServer serve /api/v1/query (instant) e /api/v1/query_range (range) com
// comportamento configurável por endpoint — usado pra reproduzir, sem depender de um Prometheus
// real, o cenário exato do bug relatado: a query instant (barata, fonte do VALOR do pico)
// funciona normalmente, mas a QueryRange (pesada — uma série por pod/node, resolução completa)
// falha (erro ou resultado vazio) por causa de alta cardinalidade/tamanho de resposta.
type fakePrometheusServer struct {
	queryFail       bool // faz /api/v1/query (instant) retornar erro
	queryRangeFail  bool // faz /api/v1/query_range (range) retornar erro
	queryRangeEmpty bool // faz /api/v1/query_range retornar sucesso com result vazio
}

func (f *fakePrometheusServer) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/query":
		if f.queryFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"error","error":"simulated failure"}`))
			return
		}
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"namespace": "ns1", "pod": "pod1", "instance": "10.0.0.1:9100"},
						"value":  []interface{}{1700000000, "777"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	case "/api/v1/query_range":
		if f.queryRangeFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"error","error":"query processing would load too many samples into memory"}`))
			return
		}
		if f.queryRangeEmpty {
			resp := map[string]interface{}{
				"status": "success",
				"data":   map[string]interface{}{"resultType": "matrix", "result": []interface{}{}},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"namespace": "ns1", "pod": "pod1", "instance": "10.0.0.1:9100"},
						"values": [][]interface{}{
							{1700000000, "500"},
							{1700003600, "999"},
							{1700007200, "300"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFakeEnricher(t *testing.T, f *fakePrometheusServer) *PrometheusEnricher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	e, err := NewPrometheusEnricher(srv.URL, 3, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}
	return e
}

// TestQueryPodMetricPeakValue_ParsesInstantVector confirma que a query instant/subquery (fonte
// confiável do VALOR do pico) parseia corretamente uma resposta real do Prometheus.
func TestQueryPodMetricPeakValue_ParsesInstantVector(t *testing.T) {
	e := newFakeEnricher(t, &fakePrometheusServer{})
	m := e.queryPodMetricPeakValue(t.Context(), `whatever`, "test")
	if len(m) != 1 {
		t.Fatalf("esperava 1 entrada, veio %d", len(m))
	}
	if m["ns1/pod1"] != 777 {
		t.Fatalf("esperava valor 777, veio %v", m["ns1/pod1"])
	}
}

// TestQueryPodMetricRangeMax_ParsesMatrixWithTimestamp confirma que a QueryRange (best-effort,
// fonte do timestamp) parseia corretamente valor E o instante exato do pico.
func TestQueryPodMetricRangeMax_ParsesMatrixWithTimestamp(t *testing.T) {
	e := newFakeEnricher(t, &fakePrometheusServer{})
	m := e.queryPodMetricRangeMax(t.Context(), `whatever`, "test")
	if len(m) != 1 {
		t.Fatalf("esperava 1 entrada, veio %d", len(m))
	}
	p := m["ns1/pod1"]
	if p.Value != 999 {
		t.Fatalf("esperava valor 999 (o maior dos 3 pontos), veio %v", p.Value)
	}
	if p.At.Unix() != 1700003600 {
		t.Fatalf("esperava timestamp 1700003600, veio %v", p.At.Unix())
	}
}

// TestEnrichWorkloads_PeakSurvivesRangeQueryFailure é a regressão do bug real relatado pelo
// usuário ("os valores de pico em todas as analizes que fiz sempre retorna zeradas"): antes desta
// correção, CPUMaxMillis/MemMaxMi vinham SÓ da QueryRange pesada (uma série por pod, resolução
// completa) — quando essa query falhava (erro ou resultado vazio, o caso mais provável em
// clusters com muitos pods, por estourar algum limite do Prometheus), o "top" inteiro zerava
// silenciosamente, mesmo com o P95/avg (queries muito mais baratas) funcionando normalmente.
// Confirma que, com a QueryRange falhando/vazia, o valor do pico AINDA vem (da query instant
// barata), só o timestamp que fica ausente.
func TestEnrichWorkloads_PeakSurvivesRangeQueryFailure(t *testing.T) {
	for _, tc := range []struct {
		name            string
		queryRangeFail  bool
		queryRangeEmpty bool
	}{
		{name: "range query com erro", queryRangeFail: true},
		{name: "range query vazia (sem erro, sem série)", queryRangeEmpty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newFakeEnricher(t, &fakePrometheusServer{queryRangeFail: tc.queryRangeFail, queryRangeEmpty: tc.queryRangeEmpty})
			e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})

			workloads := []FinOpsWorkload{{Namespace: "ns1", Workload: "wl1", CPURequestMillis: 100, MemRequestMi: 100}}
			e.EnrichWorkloads(t.Context(), workloads)

			wl := workloads[0]
			if wl.CPUMaxMillis != 777 {
				t.Fatalf("CPUMaxMillis deveria vir da query instant (777) mesmo com a QueryRange falhando, veio %v", wl.CPUMaxMillis)
			}
			if wl.MemMaxMi != 777 {
				t.Fatalf("MemMaxMi deveria vir da query instant (777) mesmo com a QueryRange falhando, veio %v", wl.MemMaxMi)
			}
			if wl.CPUMaxAt != nil {
				t.Fatalf("CPUMaxAt deveria ficar nil (QueryRange falhou/vazia, sem timestamp confiável), veio %v", wl.CPUMaxAt)
			}
			if wl.MemMaxAt != nil {
				t.Fatalf("MemMaxAt deveria ficar nil, veio %v", wl.MemMaxAt)
			}
		})
	}
}

// TestEnrichWorkloads_PeakHasTimestampWhenBothQueriesSucceed confirma o caminho feliz completo:
// valor da query barata + timestamp da QueryRange, combinados corretamente por workload.
func TestEnrichWorkloads_PeakHasTimestampWhenBothQueriesSucceed(t *testing.T) {
	e := newFakeEnricher(t, &fakePrometheusServer{})
	e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})

	workloads := []FinOpsWorkload{{Namespace: "ns1", Workload: "wl1", CPURequestMillis: 100, MemRequestMi: 100}}
	e.EnrichWorkloads(t.Context(), workloads)

	wl := workloads[0]
	if wl.CPUMaxMillis != 777 {
		t.Fatalf("esperava CPUMaxMillis=777 (da query instant), veio %v", wl.CPUMaxMillis)
	}
	if wl.CPUMaxAt == nil || wl.CPUMaxAt.Unix() != 1700003600 {
		t.Fatalf("esperava CPUMaxAt=1700003600 (da QueryRange), veio %v", wl.CPUMaxAt)
	}
}

// TestMergeInstancePeak cobre a mesma separação valor-confiável/timestamp-best-effort, agora pro
// caminho de node (nodeTopUsage) — sem valor confiável nenhum, o node simplesmente não aparece no
// resultado (nunca um zero fantasma); com valor mas sem timestamp, o campo At fica zero-value.
func TestMergeInstancePeak(t *testing.T) {
	// Instance label do node-exporter contém o nome literal do node (mesmo critério de correlação
	// já usado por nodeTopUsage/mergeInstancePeak — substring, não igualdade exata).
	valueByInstance := map[string]float64{"node-a:9100": 42}
	rangeByInstance := map[string]promPeakSample{"node-a:9100": {Value: 42, At: time.Unix(1700000000, 0)}}

	t.Run("com valor e timestamp", func(t *testing.T) {
		out := mergeInstancePeak([]string{"node-a"}, valueByInstance, rangeByInstance)
		p, ok := out["node-a"]
		if !ok {
			t.Fatalf("esperava node-a no resultado")
		}
		if p.Value != 42 || p.At.Unix() != 1700000000 {
			t.Fatalf("esperava {42, 1700000000}, veio %+v", p)
		}
	})

	t.Run("só valor, sem timestamp (QueryRange falhou)", func(t *testing.T) {
		out := mergeInstancePeak([]string{"node-a"}, valueByInstance, nil)
		p, ok := out["node-a"]
		if !ok {
			t.Fatalf("esperava node-a no resultado mesmo sem timestamp")
		}
		if p.Value != 42 {
			t.Fatalf("esperava valor 42, veio %v", p.Value)
		}
		if !p.At.IsZero() {
			t.Fatalf("esperava At zero-value, veio %v", p.At)
		}
	})

	t.Run("nenhum dado pro node", func(t *testing.T) {
		out := mergeInstancePeak([]string{"node-b"}, valueByInstance, rangeByInstance)
		if _, ok := out["node-b"]; ok {
			t.Fatalf("node-b não deveria aparecer no resultado (nenhuma série bateu)")
		}
	})
}
