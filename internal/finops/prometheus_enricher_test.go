package finops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	// emptyQuerySubstrings faz /api/v1/query retornar sucesso com result VAZIO (não erro) quando
	// o parâmetro "query" contém qualquer uma destas substrings — usado pra simular UMA query
	// PromQL específica (ex: só a de P95, "quantile_over_time") não achando nada, enquanto as
	// demais queries instant (ex: avg, "avg_over_time") no mesmo enriquecimento continuam
	// retornando dado normal. Sem isso, todo /api/v1/query desta fake sempre devolve a mesma
	// série fixa, então não dá pra reproduzir "uma métrica falhou, a outra não" no nível de query
	// individual (só no nível de endpoint inteiro, via queryFail).
	emptyQuerySubstrings []string
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
		_ = r.ParseForm()
		queryParam := r.Form.Get("query")
		for _, sub := range f.emptyQuerySubstrings {
			if strings.Contains(queryParam, sub) {
				resp := map[string]interface{}{
					"status": "success",
					"data":   map[string]interface{}{"resultType": "vector", "result": []interface{}{}},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
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

// TestEnrichWorkloads_FallsBackToAvgWhenP95QueryReturnsNothing cobre o bug real corrigido: cada
// campo (P95/avg de CPU/Mem) vem de uma query PromQL INDEPENDENTE — é plausível a query de P95
// (quantile_over_time, mais cara) não retornar nada pra um workload enquanto a de avg (mais
// barata) retorna normalmente. Antes desta correção, `hasUsage` só olhava P95 — um workload nesse
// estado nunca era enriquecido (CPUAvgMillis/Verdict/CPURecommendedMillis todos ficavam vazios),
// mesmo tendo uso real mensurável via avg. Mesmo padrão de fallback já usado no enricher
// Dynatrace, replicado aqui pela primeira vez.
func TestEnrichWorkloads_FallsBackToAvgWhenP95QueryReturnsNothing(t *testing.T) {
	e := newFakeEnricher(t, &fakePrometheusServer{
		emptyQuerySubstrings: []string{"quantile_over_time"}, // derruba só as 2 queries de P95 (cpu+mem)
	})
	e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})

	workloads := []FinOpsWorkload{{Namespace: "ns1", Workload: "wl1", CPURequestMillis: 1000, MemRequestMi: 1000}}
	e.EnrichWorkloads(t.Context(), workloads)

	wl := workloads[0]
	if wl.CPUP95Millis != 0 {
		t.Fatalf("CPUP95Millis deveria continuar honestamente 0 (query não achou nada), veio %v — nunca deveria ser populado com avg disfarçado", wl.CPUP95Millis)
	}
	if wl.CPUAvgMillis != 777 {
		t.Fatalf("esperava CPUAvgMillis=777 (query de avg funcionou normalmente), veio %v — workload não foi enriquecido", wl.CPUAvgMillis)
	}
	if wl.CPURecommendedMillis != round2(777*SafetyMargin) {
		t.Fatalf("esperava CPURecommendedMillis calculado a partir do avg (fallback, já que P95=0), veio %v", wl.CPURecommendedMillis)
	}
	if wl.MemRecommendedMi != round2(777*SafetyMargin) {
		t.Fatalf("esperava MemRecommendedMi calculado a partir do avg (fallback), veio %v", wl.MemRecommendedMi)
	}
	if wl.Verdict == "" {
		t.Fatalf("esperava um Verdict calculado (workload tem uso real via avg) — ficou vazio, sinal de que hasUsage não considerou o avg")
	}
	if wl.MetricsSource != "prometheus" {
		t.Fatalf("esperava MetricsSource=prometheus, veio %q", wl.MetricsSource)
	}
}

// TestEnrichWorkloadsPartial_DoesNotOverwriteDynatraceEnrichedWorkloads cobre um bug real
// corrigido no mesmo lote: EnrichWorkloadsPartial chamava EnrichWorkloads sobre o slice INTEIRO,
// sem filtrar — como EnrichWorkloads sobrescreve incondicionalmente qualquer workload com dado
// Prometheus disponível, isso sobrescrevia silenciosamente valores já preenchidos pelo Dynatrace
// sempre que o MESMO workload também tinha métrica Prometheus disponível (violando a prioridade
// documentada "DT primário, Prometheus só cobre o resto"). O pod deste workload é justamente o
// único que a fakePrometheusServer tem dado pra devolver — se a correlação corresse mesmo assim,
// o teste pegaria a sobrescrita.
func TestEnrichWorkloadsPartial_DoesNotOverwriteDynatraceEnrichedWorkloads(t *testing.T) {
	e := newFakeEnricher(t, &fakePrometheusServer{})
	e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl-dt"})

	workloads := []FinOpsWorkload{
		{
			Namespace: "ns1", Workload: "wl-dt",
			CPURequestMillis: 1000, MemRequestMi: 1000,
			CPUAvgMillis: 999, CPUP95Millis: 999, Verdict: "ok", MetricsSource: "dynatrace",
		},
	}
	dtEnriched := map[string]bool{"ns1/wl-dt": true}

	e.EnrichWorkloadsPartial(t.Context(), workloads, dtEnriched)

	wl := workloads[0]
	if wl.MetricsSource != "dynatrace" {
		t.Fatalf("MetricsSource deveria continuar 'dynatrace' (workload já coberto pelo DT), veio %q", wl.MetricsSource)
	}
	if wl.CPUAvgMillis != 999 {
		t.Fatalf("CPUAvgMillis deveria continuar 999 (valor do DT) mesmo com dado Prometheus disponível pro mesmo pod, veio %v — sinal de que Prometheus sobrescreveu o DT", wl.CPUAvgMillis)
	}
}

// TestEnrichWorkloadsPartial_EnrichesWorkloadsNotCoveredByDynatrace confirma o caminho feliz:
// workloads FORA de dtEnriched continuam sendo enriquecidos normalmente via Prometheus.
func TestEnrichWorkloadsPartial_EnrichesWorkloadsNotCoveredByDynatrace(t *testing.T) {
	e := newFakeEnricher(t, &fakePrometheusServer{})
	e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl-prom-only"})

	workloads := []FinOpsWorkload{
		{Namespace: "ns1", Workload: "wl-prom-only", CPURequestMillis: 1000, MemRequestMi: 1000},
	}
	e.EnrichWorkloadsPartial(t.Context(), workloads, map[string]bool{})

	wl := workloads[0]
	if wl.MetricsSource != "prometheus" {
		t.Fatalf("esperava MetricsSource=prometheus, veio %q", wl.MetricsSource)
	}
	if wl.CPUAvgMillis != 777 {
		t.Fatalf("esperava CPUAvgMillis=777 (dado real da fake), veio %v", wl.CPUAvgMillis)
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

// TestMergeNodeTopPeaks cobre a regressão real relatada pelo usuário: "pico 0%" em TODOS os
// nodes de um cluster real (17/17), apesar de "agora" (metrics-server, nunca passa por aqui)
// mostrar uso real. Causa: o label "instance" do node-exporter nesse cluster não contém o nome
// literal do node K8s (ex: é um IP:porta do scrape target) — a correlação por substring
// (mergeInstancePeak) nunca bate, silenciosamente, pra NENHUM node. A correlação por "nodename"
// (join com node_uname_info, IGUALDADE exata) é a estratégia preferida justamente por não
// depender desse formato.
func TestMergeNodeTopPeaks(t *testing.T) {
	t.Run("nodename encontra quando instance (substring) não acha nada — bug real corrigido", func(t *testing.T) {
		// "instance" é um IP:porta que nunca contém o nome do node — cenário real relatado.
		valueByInstance := map[string]float64{"10.244.3.5:9100": 999}
		valueByNodename := map[string]float64{"aks-calculofrete-22930315-vmss000055": 25}

		out := mergeNodeTopPeaks([]string{"aks-calculofrete-22930315-vmss000055"},
			valueByNodename, nil, valueByInstance, nil)

		p, ok := out["aks-calculofrete-22930315-vmss000055"]
		if !ok {
			t.Fatalf("esperava o node no resultado via correlação por nodename")
		}
		if p.Value != 25 {
			t.Fatalf("esperava valor 25 (da correlação por nodename, não 999 do fallback), veio %v", p.Value)
		}
	})

	t.Run("cai pro fallback (instance) quando nodename não tem dado pro node", func(t *testing.T) {
		valueByInstance := map[string]float64{"node-a:9100": 42}
		out := mergeNodeTopPeaks([]string{"node-a"}, nil, nil, valueByInstance, nil)
		p, ok := out["node-a"]
		if !ok {
			t.Fatalf("esperava node-a via fallback de instance")
		}
		if p.Value != 42 {
			t.Fatalf("esperava valor 42, veio %v", p.Value)
		}
	})

	t.Run("nodename sobrescreve instance quando os dois têm dado (prioridade da correlação mais confiável)", func(t *testing.T) {
		valueByInstance := map[string]float64{"node-a:9100": 111}
		valueByNodename := map[string]float64{"node-a": 222}
		out := mergeNodeTopPeaks([]string{"node-a"}, valueByNodename, nil, valueByInstance, nil)
		if out["node-a"].Value != 222 {
			t.Fatalf("esperava 222 (nodename tem prioridade), veio %v", out["node-a"].Value)
		}
	})

	t.Run("timestamp de nodename também sobrescreve o de instance", func(t *testing.T) {
		valueByNodename := map[string]float64{"node-a": 42}
		rangeByNodename := map[string]promPeakSample{"node-a": {Value: 42, At: time.Unix(1800000000, 0)}}
		valueByInstance := map[string]float64{"node-a:9100": 42}
		rangeByInstance := map[string]promPeakSample{"node-a:9100": {Value: 42, At: time.Unix(1700000000, 0)}}

		out := mergeNodeTopPeaks([]string{"node-a"}, valueByNodename, rangeByNodename, valueByInstance, rangeByInstance)
		if out["node-a"].At.Unix() != 1800000000 {
			t.Fatalf("esperava timestamp da correlação por nodename (1800000000), veio %v", out["node-a"].At.Unix())
		}
	})

	t.Run("nenhuma estratégia encontra dado", func(t *testing.T) {
		out := mergeNodeTopPeaks([]string{"node-z"}, nil, nil, nil, nil)
		if _, ok := out["node-z"]; ok {
			t.Fatalf("node-z não deveria aparecer no resultado")
		}
	})
}

// TestQueryNodenameMetric_ParsesNodenameLabel confirma que as duas queries por "nodename" (join
// com node_uname_info) parseiam corretamente uma resposta real do Prometheus, keyed pelo label
// certo — nunca "instance" (que é o que a correlação antiga usava e o que motivou este ajuste).
func TestQueryNodenameMetric_ParsesNodenameLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/query":
			resp := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"resultType": "vector",
					"result": []map[string]interface{}{
						{
							"metric": map[string]string{"nodename": "aks-calculofrete-22930315-vmss000055"},
							"value":  []interface{}{1700000000, "321"},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/query_range":
			resp := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"resultType": "matrix",
					"result": []map[string]interface{}{
						{
							"metric": map[string]string{"nodename": "aks-calculofrete-22930315-vmss000055"},
							"values": [][]interface{}{{1700000000, "321"}},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	e, err := NewPrometheusEnricher(srv.URL, 3, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}

	valueMap := e.queryNodenameMetricPeakValue(t.Context(), `whatever`, "test")
	if valueMap["aks-calculofrete-22930315-vmss000055"] != 321 {
		t.Fatalf("esperava 321 keyed por nodename, veio %+v", valueMap)
	}

	rangeMap := e.queryNodenameMetricRangeMax(t.Context(), `whatever`, "test")
	p, ok := rangeMap["aks-calculofrete-22930315-vmss000055"]
	if !ok || p.Value != 321 {
		t.Fatalf("esperava 321 keyed por nodename, veio %+v", rangeMap)
	}
}

// countingNodeQueryServer conta quantas vezes cada "família" de query (nodename vs. instance) foi
// disparada — usado pra provar o short-circuit de nodeMetricTopUsage (bug real corrigido: a
// versão anterior sempre rodava as duas estratégias incondicionalmente, dobrando os round-trips
// ao Prometheus e deixando o scan "horrores" mais lento, a ponto de derrubar VPN/túnel instável).
type countingNodeQueryServer struct {
	nodenameQueries int
	instanceQueries int
	nodenameEmpty   bool // se true, toda query "by nodename" volta vazia (simula ausência de node_uname_info)
}

func queryParam(r *http.Request) string {
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		return r.PostFormValue("query")
	}
	return r.URL.Query().Get("query")
}

func (s *countingNodeQueryServer) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := queryParam(r)
	isNodename := strings.Contains(q, "nodename")

	var resultMetric map[string]string
	if isNodename {
		s.nodenameQueries++
		if s.nodenameEmpty {
			resultMetric = nil
		} else {
			resultMetric = map[string]string{"nodename": "node-a"}
		}
	} else {
		s.instanceQueries++
		// "instance" precisa conter o nome do node como substring — mesmo critério de correlação
		// do fallback (mergeInstancePeak); usar um IP aqui faria o fallback também "falhar",
		// mascarando o que este teste quer provar (contagem de queries, não a correlação em si —
		// essa já é coberta por TestMergeNodeTopPeaks).
		resultMetric = map[string]string{"instance": "node-a:9100"}
	}

	switch r.URL.Path {
	case "/api/v1/query":
		result := []map[string]interface{}{}
		if resultMetric != nil {
			result = append(result, map[string]interface{}{"metric": resultMetric, "value": []interface{}{1700000000, "50"}})
		}
		resp := map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "vector", "result": result}}
		_ = json.NewEncoder(w).Encode(resp)
	case "/api/v1/query_range":
		result := []map[string]interface{}{}
		if resultMetric != nil {
			result = append(result, map[string]interface{}{"metric": resultMetric, "values": [][]interface{}{{1700000000, "50"}}})
		}
		resp := map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": result}}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// TestNodeMetricTopUsage_ShortCircuitsWhenNodenameCoversEverything confirma que, quando a
// correlação por nodename já cobre 100% dos nodes pedidos, a estratégia de fallback (substring de
// "instance") NUNCA é disparada — 2 queries no total (valor + timestamp), não 4.
func TestNodeMetricTopUsage_ShortCircuitsWhenNodenameCoversEverything(t *testing.T) {
	s := &countingNodeQueryServer{}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	e, err := NewPrometheusEnricher(srv.URL, 3, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}

	out := e.nodeMetricTopUsage(t.Context(), []string{"node-a"}, nodeCPUTopQueries(3), 1, "cpu")
	if out["node-a"].Value != 50 {
		t.Fatalf("esperava valor 50, veio %+v", out["node-a"])
	}
	if s.nodenameQueries != 2 {
		t.Fatalf("esperava exatamente 2 queries por nodename (valor+timestamp), veio %d", s.nodenameQueries)
	}
	if s.instanceQueries != 0 {
		t.Fatalf("fallback por instance NUNCA deveria ter sido chamado quando nodename já cobriu tudo, veio %d chamada(s)", s.instanceQueries)
	}
}

// TestNodeMetricTopUsage_FallsBackAndSkipsUnnecessaryRangeQueries confirma que, quando a
// correlação por nodename não encontra NADA (node_uname_info ausente), o fallback por instance é
// disparado — mas a QueryRange (timestamp) de nodename NUNCA roda nesse caso (não há valor
// nenhum pra anexar timestamp), evitando a 4ª query desnecessária.
func TestNodeMetricTopUsage_FallsBackAndSkipsUnnecessaryRangeQueries(t *testing.T) {
	s := &countingNodeQueryServer{nodenameEmpty: true}
	srv := httptest.NewServer(http.HandlerFunc(s.handler))
	defer srv.Close()
	e, err := NewPrometheusEnricher(srv.URL, 3, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}

	out := e.nodeMetricTopUsage(t.Context(), []string{"node-a"}, nodeCPUTopQueries(3), 1, "cpu")
	if out["node-a"].Value != 50 {
		t.Fatalf("esperava valor 50 (via fallback), veio %+v", out["node-a"])
	}
	if s.nodenameQueries != 1 {
		t.Fatalf("esperava exatamente 1 query por nodename (só o valor — a de timestamp deve ser pulada, sem dado pra anexar), veio %d", s.nodenameQueries)
	}
	if s.instanceQueries != 2 {
		t.Fatalf("esperava exatamente 2 queries de fallback por instance (valor+timestamp), veio %d", s.instanceQueries)
	}
}

// TestEnrichWorkloads_RunsQueriesConcurrently é a regressão real do relato "demora horrores...
// coisas de minutos": as 12 queries de EnrichWorkloads eram disparadas sequencialmente, uma atrás
// da outra — cada uma pagando o RTT completa antes da próxima nem começar. Este teste usa um
// servidor fake que atrasa CADA request em latencyPerQuery e mede o tempo total: se as 12 queries
// ainda rodassem em série, o teste levaria pelo menos 12×latencyPerQuery; rodando em paralelo,
// leva pouco mais que 1×latencyPerQuery (todas competem pela mesma janela de tempo).
func TestEnrichWorkloads_RunsQueriesConcurrently(t *testing.T) {
	const latencyPerQuery = 80 * time.Millisecond
	const expectedQueries = 12 // 4 container + 2 valor de pico + 2 range de pico + 4 HPA

	var mu sync.Mutex
	seen := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen++
		mu.Unlock()
		time.Sleep(latencyPerQuery)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/query":
			resp := map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "vector", "result": []interface{}{}}}
			_ = json.NewEncoder(w).Encode(resp)
		case "/api/v1/query_range":
			resp := map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": []interface{}{}}}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	e, err := NewPrometheusEnricher(srv.URL, 3, false)
	if err != nil {
		t.Fatalf("NewPrometheusEnricher: %v", err)
	}
	e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})

	start := time.Now()
	e.EnrichWorkloads(t.Context(), []FinOpsWorkload{{Namespace: "ns1", Workload: "wl1"}})
	elapsed := time.Since(start)

	mu.Lock()
	total := seen
	mu.Unlock()
	if total != expectedQueries {
		t.Fatalf("esperava %d requests ao fake server, veio %d — contagem de queries mudou? atualize expectedQueries", expectedQueries, total)
	}

	// Sequencial custaria >= 12×80ms = 960ms; em paralelo, pouco mais que 80ms. Limiar
	// generoso (metade do tempo sequencial) pra não ficar flaky em CI mais lento.
	sequentialWorstCase := time.Duration(expectedQueries) * latencyPerQuery
	if elapsed >= sequentialWorstCase/2 {
		t.Fatalf("EnrichWorkloads levou %v pra %d queries de %v cada — parece sequencial, não paralelo (esperava bem menos que %v)",
			elapsed, expectedQueries, latencyPerQuery, sequentialWorstCase/2)
	}
}

// TestPrometheusEnricher_CollectionErrors_DistinguishesRealFailureFromEmptyCoverage cobre o bug
// real corrigido: o banner de aviso do FinOps sempre assumia "provável falha de coleta
// transitória (VPN/rede/API indisponível no momento do scan)" mesmo quando as 12 queries do
// EnrichWorkloads tiveram sucesso (HTTP 200) e só não acharam nenhuma série pro cluster —
// CollectionErrors() é a fonte de verdade que agora distingue os dois casos (ver comentário de
// PrometheusEnricher.workloadQueryErrs).
func TestPrometheusEnricher_CollectionErrors_DistinguishesRealFailureFromEmptyCoverage(t *testing.T) {
	workloads := func() []FinOpsWorkload {
		return []FinOpsWorkload{{Namespace: "ns1", Workload: "wl1"}}
	}

	t.Run("falha real (HTTP 500 nas duas rotas) fica registrada", func(t *testing.T) {
		e := newFakeEnricher(t, &fakePrometheusServer{queryFail: true, queryRangeFail: true})
		e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})
		e.EnrichWorkloads(context.Background(), workloads())

		errs := e.CollectionErrors()
		if len(errs) == 0 {
			t.Fatal("esperava CollectionErrors() não-vazio após falha HTTP real em todas as queries, veio vazio")
		}
	})

	t.Run("sucesso com zero cobertura NÃO conta como falha", func(t *testing.T) {
		e := newFakeEnricher(t, &fakePrometheusServer{
			queryRangeEmpty: true,
			emptyQuerySubstrings: []string{
				"container_cpu_usage_seconds_total",
				"container_memory_working_set_bytes",
				"kube_horizontalpodautoscaler_status_current_replicas",
			},
		})
		e.SetPodMapping(map[string]string{"ns1/pod1": "ns1/wl1"})
		e.EnrichWorkloads(context.Background(), workloads())

		errs := e.CollectionErrors()
		if len(errs) != 0 {
			t.Fatalf("esperava CollectionErrors() vazio (todas as queries tiveram sucesso, só sem dado), veio %v", errs)
		}
	})
}
