package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

type sample struct {
	labels map[string]string
	value  string
}

// promFake responde cada consulta pelo trecho que a identifica.
type promFake struct {
	mu      sync.Mutex
	queries []string
	data    map[string][]sample // chave: trecho único da consulta
	fail    map[string]bool
	failAll bool
}

func (f *promFake) QueryRange(_ context.Context, q string, _, _ time.Time, _ time.Duration) (*promclient.QueryRangeResult, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	if f.failAll {
		return nil, errors.New("fora do ar")
	}
	r := &promclient.QueryRangeResult{Status: "success"}
	for frag, ss := range f.data {
		if !strings.Contains(q, frag) {
			continue
		}
		if f.fail[frag] {
			return nil, errors.New("422")
		}
		for _, s := range ss {
			r.Data.Result = append(r.Data.Result, struct {
				Metric map[string]string `json:"metric"`
				Values [][]interface{}   `json:"values"`
			}{Metric: s.labels, Values: [][]interface{}{{1.0, "999"}, {2.0, s.value}}})
		}
	}
	return r, nil
}

func pod(ns, name string) map[string]string { return map[string]string{"namespace": ns, "pod": name} }

func fakeCluster() *promFake {
	const hub1, hub2 = "frete-hub-7d9f8c6b5d-abcde", "frete-hub-7d9f8c6b5d-fghij"
	const outro = "frete-hub-regionalizado-6c8d7f5b4-zzzzz" // NÃO é do frete-hub
	const gib = 1024 * 1024 * 1024
	return &promFake{data: map[string][]sample{
		"kube_deployment_status_replicas_available":                                 {{map[string]string{"namespace": "calc", "deployment": "frete-hub"}, "70"}},
		"kube_deployment_spec_replicas":                                             {{map[string]string{"namespace": "calc", "deployment": "frete-hub"}, "72"}},
		"kube_pod_container_status_restarts_total":                                  {{pod("calc", hub1), "1.4"}, {pod("calc", hub2), "0.4"}, {pod("calc", outro), "50"}},
		"container_cpu_usage_seconds_total":                                         {{pod("calc", hub1), "0.4"}, {pod("calc", hub2), "0.4"}, {pod("calc", outro), "9"}},
		`kube_pod_container_resource_requests{namespace=~"calc",resource="cpu"}`:    {{pod("calc", hub1), "1"}, {pod("calc", hub2), "1"}},
		`kube_pod_container_resource_limits{namespace=~"calc",resource="cpu"}`:      {{pod("calc", hub1), "2"}, {pod("calc", hub2), "2"}},
		"container_memory_working_set_bytes":                                        {{pod("calc", hub1), fmt.Sprint(gib)}, {pod("calc", hub2), fmt.Sprint(gib)}, {pod("calc", outro), fmt.Sprint(8 * gib)}},
		`kube_pod_container_resource_requests{namespace=~"calc",resource="memory"}`: {{pod("calc", hub1), fmt.Sprint(2 * gib)}, {pod("calc", hub2), fmt.Sprint(2 * gib)}},
		`kube_pod_container_resource_limits{namespace=~"calc",resource="memory"}`:   {{pod("calc", hub1), fmt.Sprint(4 * gib)}, {pod("calc", hub2), fmt.Sprint(4 * gib)}},
	}}
}

func TestLiveResourcesSomaSoOsPodsDoDeployment(t *testing.T) {
	f := fakeCluster()
	rs, notes, err := CollectLiveResources(context.Background(), f, []Item{{"c1", "calc", "frete-hub"}}, time.Now())
	if err != nil || len(notes) != 0 {
		t.Fatalf("err=%v notes=%v", err, notes)
	}
	r := rs[0]
	chk := func(nome string, got *float64, want float64) {
		t.Helper()
		if got == nil || (*got-want) > 1e-9 || (want-*got) > 1e-9 {
			t.Errorf("%s = %v, esperado %v", nome, got, want)
		}
	}
	chk("replicas disponíveis", r.ReplicasAvailable, 70)
	chk("replicas desejadas", r.ReplicasDesired, 72)
	chk("restarts (soma 1,4+0,4 arredondada; o de outro deployment não entra)", r.Restarts15m, 2)
	chk("CPU cores", r.CPUCores, 0.8)
	chk("CPU % do request (0,8 / 2)", r.CPUReqPct, 40)
	chk("CPU % do limit (0,8 / 4)", r.CPULimPct, 20)
	chk("memória MB (2 GiB)", r.MemMB, 2048)
	chk("memória % do request (2/4)", r.MemReqPct, 50)
	chk("memória % do limit (2/8)", r.MemLimPct, 25)
	if r.Pods != 2 {
		t.Errorf("pods = %d", r.Pods)
	}
}

func TestLiveResourcesUmaChamadaPorMetricaPorCluster(t *testing.T) {
	f := fakeCluster()
	items := []Item{{"c1", "calc", "frete-hub"}, {"c1", "calc", "frete-b2c"}, {"c1", "outro-ns", "x"}}
	rs, _, err := CollectLiveResources(context.Background(), f, items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 9 {
		t.Errorf("%d consultas, esperado 9 (uma por métrica, não por aplicação)", len(f.queries))
	}
	for _, q := range f.queries {
		if !strings.Contains(q, `namespace=~"calc|outro-ns"`) {
			t.Errorf("a consulta deveria cobrir os dois namespaces: %s", q)
		}
		if strings.Contains(q, "sum by (namespace,pod)") && !strings.Contains(q, "max by (namespace,pod,container)") {
			t.Errorf("série duplicada do kube-state-metrics/cAdvisor dobraria a soma: %s", q)
		}
	}
	// sem pod nenhum: sem dado, nunca zero
	if rs[1].CPUCores != nil || rs[1].MemMB != nil || rs[1].Restarts15m != nil || rs[2].CPUCores != nil {
		t.Errorf("aplicação sem pods tem de ficar sem dado: %+v %+v", rs[1], rs[2])
	}
}

func TestLiveResourcesSemLimitNaoDaPercentualDoLimit(t *testing.T) {
	f := fakeCluster()
	delete(f.data, `kube_pod_container_resource_limits{namespace=~"calc",resource="cpu"}`)
	rs, _, _ := CollectLiveResources(context.Background(), f, []Item{{"c1", "calc", "frete-hub"}}, time.Now())
	if rs[0].CPULimPct != nil {
		t.Errorf("sem limit o percentual do limit tem de ser nulo, veio %v", *rs[0].CPULimPct)
	}
	if rs[0].CPUReqPct == nil || rs[0].CPUCores == nil {
		t.Error("o resto não pode sumir por falta do limit")
	}
}

func TestLiveResourcesFalhaIsoladaEFalhaTotal(t *testing.T) {
	f := fakeCluster()
	f.fail = map[string]bool{"kube_deployment_spec_replicas": true}
	rs, notes, err := CollectLiveResources(context.Background(), f, []Item{{"c1", "calc", "frete-hub"}}, time.Now())
	if err != nil || rs[0].ReplicasDesired != nil || rs[0].CPUCores == nil || len(notes) != 1 || !strings.Contains(notes[0], "replicas_desired") {
		t.Errorf("falha isolada: err=%v notes=%v r=%+v", err, notes, rs[0])
	}
	all := &promFake{failAll: true}
	if _, _, err := CollectLiveResources(context.Background(), all, []Item{{"c1", "calc", "frete-hub"}}, time.Now()); err == nil {
		t.Error("todas as consultas falharam: esperado erro")
	}
}

func TestLiveResourcesNomeInvalido(t *testing.T) {
	f := fakeCluster()
	if _, _, err := CollectLiveResources(context.Background(), f, []Item{{"c1", `calc"} or vector(1) #`, "x"}}, time.Now()); err == nil {
		t.Error("namespace com aspas tem de ser recusado (não pode entrar na consulta)")
	}
	if len(f.queries) != 0 {
		t.Error("nada pode ser consultado com nome inválido")
	}
}

func TestLiveResourcesNamespaceComPontoEscapado(t *testing.T) {
	f := &promFake{data: map[string][]sample{}}
	_, _, _ = CollectLiveResources(context.Background(), f, []Item{{"c1", "a.b", "x"}}, time.Now())
	if len(f.queries) == 0 || !strings.Contains(f.queries[0], `a\\.b`) {
		t.Errorf("ponto do namespace deve ser escapado na regex: %v", f.queries)
	}
}

// ── integração no CollectLive ────────────────────────────────────────────────────────

func TestCollectLiveComRecursosNaoMexeNosPercentis(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	p := fakeCluster()
	pr := func(string) (RangeQuerier, error) { return p, nil }
	req := liveReq(Item{"c1", "calc", "frete-hub"})
	req.Resources = true
	res, err := CollectLive(context.Background(), req, LiveDeps{Dynatrace: dt, Prometheus: pr}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := res.Series[0]
	if s.Resources == nil || s.Resources.ReplicasAvailable == nil || *s.Resources.ReplicasAvailable != 70 {
		t.Errorf("recursos: %+v", s.Resources)
	}
	// percentis e tráfego vêm só do Dynatrace: nenhuma consulta de latência foi ao Prometheus
	for _, q := range p.queries {
		if strings.Contains(q, "percentile") || strings.Contains(q, "histogram_quantile") || strings.Contains(q, "nginx_ingress") {
			t.Errorf("latência/tráfego não pode vir do Prometheus: %s", q)
		}
	}
	if f.selCalls[selP99] != 1 || f.selCalls[selP50] != 1 {
		t.Errorf("os percentis têm de ser consultados no Dynatrace: %v", f.selCalls)
	}
}

func TestCollectLiveSemRecursosNaoConsultaOPrometheus(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	p := fakeCluster()
	pr := func(string) (RangeQuerier, error) { return p, nil }
	res, _ := CollectLive(context.Background(), liveReq(Item{"c1", "calc", "frete-hub"}), LiveDeps{Dynatrace: dt, Prometheus: pr}, time.Now())
	if len(p.queries) != 0 || res.Series[0].Resources != nil {
		t.Errorf("sem o pedido de recursos o Prometheus não é consultado (%d consultas)", len(p.queries))
	}
}

func TestCollectLiveRecursosSobrevivemAFalhaDoDynatrace(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}, noPG: map[string]bool{"frete-hub": true}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	p := fakeCluster()
	pr := func(string) (RangeQuerier, error) { return p, nil }
	req := liveReq(Item{"c1", "calc", "frete-hub"})
	req.Resources = true
	res, _ := CollectLive(context.Background(), req, LiveDeps{Dynatrace: dt, Prometheus: pr}, time.Now())
	if res.Series[0].Error == "" || res.Series[0].Resources == nil {
		t.Errorf("o erro do Dynatrace não pode apagar os recursos: erro=%q recursos=%v", res.Series[0].Error, res.Series[0].Resources)
	}
}

func TestCollectLivePrometheusIndisponivelViraNota(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	pr := func(string) (RangeQuerier, error) { return nil, errors.New("sem rota") }
	req := liveReq(Item{"c1", "calc", "frete-hub"})
	req.Resources = true
	res, err := CollectLive(context.Background(), req, LiveDeps{Dynatrace: dt, Prometheus: pr}, time.Now())
	s := res.Series[0]
	if err != nil || s.Error != "" || s.Resources != nil || !strings.Contains(strings.Join(s.Notes, "|"), "sem rota") {
		t.Errorf("err=%v erro=%q recursos=%v notas=%v", err, s.Error, s.Resources, s.Notes)
	}
}

func TestLiveResourcesLimitZeradoNaoDividePorZero(t *testing.T) {
	f := fakeCluster()
	f.data[`kube_pod_container_resource_limits{namespace=~"calc",resource="cpu"}`] = []sample{{pod("calc", "frete-hub-7d9f8c6b5d-abcde"), "0"}}
	rs, _, _ := CollectLiveResources(context.Background(), f, []Item{{"c1", "calc", "frete-hub"}}, time.Now())
	if rs[0].CPULimPct != nil {
		t.Errorf("limit total 0 daria +Inf (e quebra o JSON): esperado nulo, veio %v", *rs[0].CPULimPct)
	}
}
