package finops

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func wls(namespaces ...string) []FinOpsWorkload {
	var out []FinOpsWorkload
	for _, ns := range namespaces {
		out = append(out, FinOpsWorkload{Namespace: ns, Workload: "app"})
	}
	return out
}

func TestPlanNamespaceShards(t *testing.T) {
	// Poucos namespaces: 1 fatia (não vale repartir), mas já restrita aos analisados.
	if got := planNamespaceShards(wls("a", "b", "c"), 4); len(got) != 1 || got[0] != "a|b|c" {
		t.Errorf("3 namespaces: %v", got)
	}
	// Sem namespace válido: sem restrição (mantém a query inteira, nunca uma query vazia).
	if got := planNamespaceShards(wls("Inválido!", "com.ponto", ""), 4); len(got) != 1 || got[0] != "" {
		t.Errorf("sem válidos: %v", got)
	}
	if got := planNamespaceShards(nil, 4); len(got) != 1 || got[0] != "" {
		t.Errorf("sem workloads: %v", got)
	}

	// 8 namespaces → 4 fatias, cobrindo cada namespace EXATAMENTE uma vez.
	in := wls("a", "b", "c", "d", "e", "f", "g", "h")
	got := planNamespaceShards(in, 4)
	if len(got) != 4 {
		t.Fatalf("esperava 4 fatias: %v", got)
	}
	seen := map[string]int{}
	for _, sh := range got {
		for _, ns := range strings.Split(sh, "|") {
			seen[ns]++
		}
	}
	if len(seen) != 8 {
		t.Errorf("faltou namespace: %v", seen)
	}
	for ns, n := range seen {
		if n != 1 {
			t.Errorf("namespace %s em %d fatias (deveria ser 1)", ns, n)
		}
	}
	// Determinístico.
	if again := planNamespaceShards(in, 4); strings.Join(again, ",") != strings.Join(got, ",") {
		t.Errorf("não determinístico: %v vs %v", got, again)
	}
}

func TestPlanNamespaceShards_BalancesByWorkloadCount(t *testing.T) {
	// "grande" tem 10 workloads; os outros 5 namespaces têm 1 cada — o grande fica sozinho numa
	// fatia e os pequenos se juntam nas outras, em vez de o grande dividir fatia com muitos.
	var in []FinOpsWorkload
	for i := 0; i < 10; i++ {
		in = append(in, FinOpsWorkload{Namespace: "grande", Workload: fmt.Sprintf("w%d", i)})
	}
	in = append(in, wls("p1", "p2", "p3", "p4", "p5")...)
	got := planNamespaceShards(in, 3)
	for _, sh := range got {
		if strings.Contains(sh, "grande") && sh != "grande" {
			t.Errorf("namespace grande deveria ficar sozinho, fatia = %q (todas: %v)", sh, got)
		}
	}
}

func TestNsMatcherAndShardLabel(t *testing.T) {
	if nsMatcher("") != "" {
		t.Error("fatia vazia não restringe")
	}
	if got := nsMatcher("a|b"); got != `,namespace=~"a|b"` {
		t.Errorf("matcher: %q", got)
	}
	if shardLabel("cpu_p95", 0, 1) != "cpu_p95" || shardLabel("cpu_p95", 1, 4) != "cpu_p95[2/4]" {
		t.Error("rótulo da fatia")
	}
}

func TestShardedRun_MergesAndSurvivesPartialFailure(t *testing.T) {
	e := &PrometheusEnricher{}
	shards := []string{"a", "b", "c"}
	got := shardedRun(e, shards, "m", func(matcher, label string) map[string]float64 {
		if strings.Contains(matcher, `"b"`) {
			return nil // a fatia "b" falhou
		}
		return map[string]float64{matcher: 1}
	})
	if len(got) != 2 {
		t.Errorf("as fatias que deram certo devem ser preservadas: %v", got)
	}
	// Todas falharam → nil (semântica "a coleta falhou" preservada).
	if all := shardedRun(e, shards, "m", func(string, string) map[string]float64 { return nil }); all != nil {
		t.Errorf("todas falharam: esperava nil, veio %v", all)
	}
	// Sucesso VAZIO ≠ falha: mapa não-nil.
	if empty := shardedRun(e, shards, "m", func(string, string) map[string]float64 { return map[string]float64{} }); empty == nil {
		t.Error("sucesso com resultado vazio não pode virar nil")
	}
}

func TestShardedRun_RespectsConcurrencyLimit(t *testing.T) {
	e := &PrometheusEnricher{heavySem: make(chan struct{}, 2)}
	var cur, peak int32
	shardedRun(e, []string{"a", "b", "c", "d", "e", "f"}, "m", func(string, string) map[string]float64 {
		n := atomic.AddInt32(&cur, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		return map[string]float64{}
	})
	if peak > 2 {
		t.Errorf("no máximo 2 simultâneas, pico = %d", peak)
	}
	if peak < 2 {
		t.Errorf("deveria paralelizar até o limite (2), pico = %d", peak)
	}
}

// Prometheus falso que se comporta como o real diante do filtro de namespace: devolve UM pod por
// namespace presente no matcher da query — assim dá pra provar que cada fatia só pede o seu pedaço
// e que a união das fatias cobre tudo, sem lacuna nem sobreposição.
type shardFake struct {
	mu      sync.Mutex
	queries []string // só as queries de container (as com matcher de namespace)
}

var nsMatcherRe = regexp.MustCompile(`namespace=~"([^"]+)"`)

func (f *shardFake) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = r.ParseForm()
	q := r.Form.Get("query")
	if strings.Contains(q, "kube_horizontalpodautoscaler") {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "vector", "result": []interface{}{}}})
		return
	}
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()

	var nss []string
	if m := nsMatcherRe.FindStringSubmatch(q); m != nil {
		nss = strings.Split(m[1], "|")
	}
	if r.URL.Path == "/api/v1/query_range" {
		var res []map[string]interface{}
		for _, ns := range nss {
			res = append(res, map[string]interface{}{
				"metric": map[string]string{"namespace": ns, "pod": "pod-" + ns},
				"values": [][]interface{}{{1700000000, "100"}, {1700003600, "200"}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": res}})
		return
	}
	var res []map[string]interface{}
	for _, ns := range nss {
		res = append(res, map[string]interface{}{
			"metric": map[string]string{"namespace": ns, "pod": "pod-" + ns},
			"value":  []interface{}{1700000000, "150"},
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "vector", "result": res}})
}

func TestEnrichWorkloads_ShardsHeavyQueriesByNamespace(t *testing.T) {
	f := &shardFake{}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	e, err := NewPrometheusEnricher(srv.URL, 30, false)
	if err != nil {
		t.Fatal(err)
	}

	namespaces := []string{"ns-a", "ns-b", "ns-c", "ns-d", "ns-e", "ns-f"}
	workloads := wls(namespaces...)
	podMap := map[string]string{}
	for _, ns := range namespaces {
		podMap[ns+"/pod-"+ns] = ns + "/app"
	}
	e.podToWorkload = podMap

	e.EnrichWorkloads(t.Context(), workloads)

	// 8 tipos de query pesada; cada um deve ter sido pedido em fatias que juntas cobrem os 6
	// namespaces EXATAMENTE uma vez.
	perKind := map[string][]string{}
	for _, q := range f.queries {
		m := nsMatcherRe.FindStringSubmatch(q)
		if m == nil {
			t.Fatalf("query pesada SEM restrição de namespace (varreria o cluster inteiro): %s", q)
		}
		kind := nsMatcherRe.ReplaceAllString(q, "")
		perKind[kind] = append(perKind[kind], strings.Split(m[1], "|")...)
	}
	if len(perKind) != 8 {
		t.Errorf("esperava 8 tipos de query pesada, vieram %d", len(perKind))
	}
	for kind, got := range perKind {
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(namespaces, ",") {
			t.Errorf("cobertura errada (lacuna ou sobreposição) pra %.60s…: %v", kind, got)
		}
	}
	if n := len(f.queries); n <= 8 {
		t.Errorf("com 6 namespaces deveria haver mais de 8 queries (fatias), houve %d", n)
	}

	// O resultado juntado alimenta TODOS os workloads.
	for _, wl := range workloads {
		if wl.CPUP95Millis != 150 {
			t.Errorf("workload %s sem dado de uso depois de juntar as fatias: %+v", wl.Namespace, wl)
		}
	}
	if len(e.CollectionErrors()) != 0 {
		t.Errorf("sem erros esperados: %v", e.CollectionErrors())
	}
}

func TestParseHeavyTimeout(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"5m", 5 * time.Minute, true}, {"90s", 90 * time.Second, true}, {"30s", 30 * time.Second, true},
		{"10s", 0, false}, {"2h", 0, false}, {"lixo", 0, false}, {"", 0, false},
	}
	for _, c := range cases {
		if d, ok := parseHeavyTimeout(c.in); ok != c.ok || d != c.want {
			t.Errorf("parseHeavyTimeout(%q) = %v,%v; quer %v,%v", c.in, d, ok, c.want, c.ok)
		}
	}
	if promHeavyTimeoutDefault < 2*time.Minute {
		t.Error("o padrão precisa ficar acima do --query.timeout padrão do Prometheus (2 min)")
	}
}
