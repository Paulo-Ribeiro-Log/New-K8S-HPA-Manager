package report

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s-hpa-manager/internal/dynatrace"
)

func ser(t []int64, v ...float64) Series { return Series{Times: t, Values: v} }

func TestBuildLiveSomaEPercentilPorFatia(t *testing.T) {
	ts := []int64{0, 60000, 120000}
	svcs := []ServiceTraffic{
		{ID: "A", Name: "A", Requests: ser(ts, 600, 1200, 600), Err4xx: ser(ts, 60, 0, nan), Err5xx: ser(ts, 0, 6, 0),
			P50Ms: ser(ts, 10, 20, 30), P95Ms: ser(ts, 100, 200, 300), P99Ms: ser(ts, 500, 900, 700)},
		// serviço com fatia < 1% (3 de ~3000): não pode ditar a latência
		{ID: "B", Name: "B", Requests: ser(ts, 1, 1, 1), Err4xx: ser(ts, nan, nan, nan), Err5xx: ser(ts, nan, nan, nan),
			P50Ms: ser(ts, 9999, 9999, 9999), P95Ms: ser(ts, 9999, 9999, 9999), P99Ms: ser(ts, 9999, 9999, 9999)},
	}
	now := time.UnixMilli(10 * 60000)
	got := BuildLive(svcs, time.Minute, DefaultP99Floor, now)
	if len(got.Times) != 3 {
		t.Fatalf("times = %v", got.Times)
	}
	if v := *got.RPS[1]; v != (1200+1)/60.0 {
		t.Errorf("RPS[1] = %v, esperado soma/60", v)
	}
	if got.Err4xxTotal == nil || *got.Err4xxTotal != 60 || got.Err5xxTotal == nil || *got.Err5xxTotal != 6 {
		t.Errorf("totais 4xx/5xx = %v / %v, esperado 60 / 6 (contagem inteira do período)", got.Err4xxTotal, got.Err5xxTotal)
	}
	if v := *got.Err5xxRPS[1]; v != 0.1 {
		t.Errorf("5xx/s = %v", v)
	}
	// 4xx do instante 2: A tem NaN e B também → sem dado (nunca zero)
	if got.Err4xxRPS[2] != nil {
		t.Errorf("4xx sem dado virou %v, esperado nulo", *got.Err4xxRPS[2])
	}
	if got.Err4xxRPS[0] == nil || *got.Err4xxRPS[0] != 1 {
		t.Errorf("4xx[0] = %v", got.Err4xxRPS[0])
	}
	if v := *got.P99Ms[1]; v != 900 {
		t.Errorf("P99[1] = %v: o serviço com <1%% do tráfego ditou a latência", v)
	}
	if v := *got.P50Ms[2]; v != 30 {
		t.Errorf("P50[2] = %v", v)
	}
}

func TestBuildLiveDescartaIntervaloIncompleto(t *testing.T) {
	ts := []int64{0, 60000, 120000}
	svcs := []ServiceTraffic{{ID: "A", Name: "A", Requests: ser(ts, 60, 60, 60), Err4xx: ser(ts, 0, 0, 0), Err5xx: ser(ts, 0, 0, 0)}}
	// agora = 2m30s: o intervalo que começa em 2m ainda não terminou (termina em 3m)
	got := BuildLive(svcs, time.Minute, DefaultP99Floor, time.UnixMilli(150000))
	if len(got.Times) != 2 || got.Times[1] != 60000 {
		t.Fatalf("times = %v, esperado só os 2 intervalos completos", got.Times)
	}
	// exatamente no fim do intervalo, ele conta
	got = BuildLive(svcs, time.Minute, DefaultP99Floor, time.UnixMilli(180000))
	if len(got.Times) != 3 {
		t.Fatalf("times = %v", got.Times)
	}
}

func TestBuildLiveSemPercentilNaoCriaSerie(t *testing.T) {
	ts := []int64{0}
	svcs := []ServiceTraffic{{ID: "A", Name: "A", Requests: ser(ts, 60), Err4xx: ser(ts, 0), Err5xx: ser(ts, 0),
		P50Ms: emptySeries(), P95Ms: emptySeries(), P99Ms: emptySeries()}}
	got := BuildLive(svcs, time.Minute, DefaultP99Floor, time.UnixMilli(600000))
	if got.P50Ms != nil || got.P95Ms != nil || got.P99Ms != nil {
		t.Errorf("percentis sem dado devem ficar ausentes: %v %v %v", got.P50Ms, got.P95Ms, got.P99Ms)
	}
}

// ── CollectLive ─────────────────────────────────────────────────────────────────────

type liveFake struct {
	mu       sync.Mutex
	selCalls map[string]int
	noPG     map[string]bool // workload -> sem process group
	failReq  bool
	idsSeen  [][]string
}

func (f *liveFake) ResolveEntryServices(_ context.Context, _, ns, wl string) ([]dynatrace.ServiceRef, []string, error) {
	if f.noPG[wl] {
		return nil, nil, dynatrace.ErrNoProcessGroup
	}
	return []dynatrace.ServiceRef{{ID: "S-" + wl, Name: wl + "Ctl", Type: "WEB_SERVICE"}}, nil, nil
}

func (f *liveFake) QueryServiceSeries(_ context.Context, sel string, ids []string, from, to time.Time, _ string, _ float64) (map[string]dynatrace.NullableSeries, error) {
	f.mu.Lock()
	f.selCalls[sel]++
	f.idsSeen = append(f.idsSeen, ids)
	f.mu.Unlock()
	if f.failReq && strings.Contains(sel, "requestCount") {
		return nil, errors.New("boom")
	}
	out := map[string]dynatrace.NullableSeries{}
	for _, id := range ids {
		out[id] = dynatrace.NullableSeries{Times: []int64{from.UnixMilli()}, Values: []float64{60}}
	}
	return out, nil
}

func liveReq(items ...Item) LiveRequest {
	now := time.Now()
	return LiveRequest{Items: items, Window: Window{Start: now.Add(-time.Hour), End: now}, Percentiles: []int{50, 99}}
}

func TestCollectLiveUmaChamadaPorMetricaPorCluster(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	var items []Item
	for _, w := range []string{"a", "b", "c", "d", "e"} {
		items = append(items, Item{Cluster: "c1", Namespace: "ns", Workload: w})
	}
	res, err := CollectLive(context.Background(), liveReq(items...), LiveDeps{Dynatrace: dt}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range f.selCalls {
		total += n
		if n != 1 {
			t.Errorf("métrica consultada %d vezes, esperado 1 por cluster", n)
		}
	}
	if total != 5 { // req, 4xx, 5xx, P50, P99
		t.Errorf("%d chamadas, esperado 5 (percentil 95 não foi pedido): %v", total, f.selCalls)
	}
	for _, ids := range f.idsSeen {
		if len(ids) != 5 {
			t.Errorf("a chamada levou %d serviços, esperado os 5 juntos", len(ids))
		}
	}
	for i, s := range res.Series {
		if s.Error != "" || s.Workload != items[i].Workload {
			t.Errorf("série %d: %+v", i, s)
		}
	}
}

func TestCollectLiveFalhaDeUmaNaoDerrubaAsOutras(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}, noPG: map[string]bool{"b": true}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	res, err := CollectLive(context.Background(), liveReq(Item{"c1", "ns", "a"}, Item{"c1", "ns", "b"}), LiveDeps{Dynatrace: dt}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Series[0].Error != "" || res.Series[1].Error == "" {
		t.Errorf("erros: %q / %q", res.Series[0].Error, res.Series[1].Error)
	}
	if res.Series[1].Workload != "b" {
		t.Error("a aplicação com erro perdeu a identificação")
	}
}

func TestCollectLiveFalhaDeRequisicoesMarcaOCluster(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}, failReq: true}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	res, _ := CollectLive(context.Background(), liveReq(Item{"c1", "ns", "a"}), LiveDeps{Dynatrace: dt}, time.Now())
	if !strings.Contains(res.Series[0].Error, "requisições") {
		t.Errorf("erro = %q", res.Series[0].Error)
	}
}

func TestCollectLiveClusterSemCredencial(t *testing.T) {
	dt := func(c string) (DynatraceAPI, error) { return nil, errors.New("sem token") }
	res, err := CollectLive(context.Background(), liveReq(Item{"c1", "ns", "a"}), LiveDeps{Dynatrace: dt}, time.Now())
	if err != nil || !strings.Contains(res.Series[0].Error, "sem token") {
		t.Errorf("err=%v série=%+v", err, res.Series[0])
	}
}

func TestCollectLivePedidoInvalido(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	now := time.Now()
	ok := Item{"c1", "ns", "a"}
	many := make([]Item, LiveMaxItems+1)
	for i := range many {
		many[i] = ok
	}
	casos := map[string]LiveRequest{
		"sem itens":        {Window: Window{Start: now.Add(-time.Hour), End: now}},
		"itens demais":     {Items: many, Window: Window{Start: now.Add(-time.Hour), End: now}},
		"item incompleto":  {Items: []Item{{Cluster: "c1"}}, Window: Window{Start: now.Add(-time.Hour), End: now}},
		"janela curta":     {Items: []Item{ok}, Window: Window{Start: now.Add(-time.Minute), End: now}},
		"percentil errado": {Items: []Item{ok}, Window: Window{Start: now.Add(-time.Hour), End: now}, Percentiles: []int{77}},
	}
	for nome, r := range casos {
		if _, err := CollectLive(context.Background(), r, LiveDeps{Dynatrace: dt}, now); err == nil {
			t.Errorf("%s: esperado erro", nome)
		}
	}
	if len(f.selCalls) != 0 {
		t.Error("pedido inválido não pode consultar o Dynatrace")
	}
}

// ── redução de pontos ───────────────────────────────────────────────────────────────

func fp(v float64) *float64 { return &v }

func TestDownsampleMediaParaTaxaEMaximoParaLatencia(t *testing.T) {
	s := LiveSeries{
		Times:     []int64{0, 1, 2, 3, 4},
		RPS:       []*float64{fp(10), fp(20), fp(30), fp(40), fp(50)},
		Err4xxRPS: []*float64{nil, nil, fp(1), nil, nil},
		Err5xxRPS: []*float64{nil, nil, nil, nil, nil},
		P99Ms:     []*float64{fp(100), fp(900), fp(300), nil, fp(50)},
	}
	got := Downsample(s, 2)
	if len(got.Times) != 3 || got.Times[1] != 2 {
		t.Fatalf("times = %v, esperado o início de cada grupo", got.Times)
	}
	if *got.RPS[0] != 15 || *got.RPS[1] != 35 || *got.RPS[2] != 50 {
		t.Errorf("RPS = %v %v %v, esperado a média de cada grupo", *got.RPS[0], *got.RPS[1], *got.RPS[2])
	}
	if *got.P99Ms[0] != 900 || *got.P99Ms[1] != 300 || *got.P99Ms[2] != 50 {
		t.Errorf("P99 = %v %v %v, esperado o máximo de cada grupo", *got.P99Ms[0], *got.P99Ms[1], *got.P99Ms[2])
	}
	if got.Err4xxRPS[0] != nil || got.Err4xxRPS[1] == nil || got.Err4xxRPS[2] != nil {
		t.Errorf("grupo sem dado tem de ficar nulo (e não zero): %v", got.Err4xxRPS)
	}
	if got.P50Ms != nil {
		t.Error("percentil não pedido não pode aparecer")
	}
	if len(s.RPS) != 5 || *s.RPS[0] != 10 {
		t.Error("a série original não pode mudar")
	}
}

func TestDownsampleArredondaEK1PreservaOsPontos(t *testing.T) {
	s := LiveSeries{Times: []int64{0, 1}, RPS: []*float64{fp(1.23456789), fp(2)}, Err4xxRPS: []*float64{nil, nil}, Err5xxRPS: []*float64{nil, nil}, P99Ms: []*float64{fp(123.456), nil}}
	got := Downsample(s, 1)
	if len(got.Times) != 2 || *got.RPS[0] != 1.235 || *got.P99Ms[0] != 123.5 || got.P99Ms[1] != nil {
		t.Errorf("k=1: %v %v %v", got.Times, *got.RPS[0], *got.P99Ms[0])
	}
	if g := Downsample(LiveSeries{}, 3); g.Times == nil || len(g.Times) != 0 {
		t.Errorf("série vazia deve ter Times vazio, não nulo: %#v", g.Times)
	}
}

func TestPontosPorGrupo(t *testing.T) {
	cases := []struct {
		window, res time.Duration
		max, want   int
	}{
		{time.Hour, time.Minute, 120, 1},               // 60 pontos cabem
		{24 * time.Hour, time.Minute, 120, 12},         // 1440 / 120
		{7 * 24 * time.Hour, 5 * time.Minute, 120, 17}, // 2016 / 120 = 16,8 -> sobe para 17
		{2 * time.Hour, time.Minute, 120, 1},           // exatamente 120
		{121 * time.Minute, time.Minute, 120, 2},
	}
	for _, c := range cases {
		if got := pointsPerBucket(c.window, c.res, c.max); got != c.want {
			t.Errorf("%v/%v max %d: %d, esperado %d", c.window, c.res, c.max, got, c.want)
		}
	}
}

func TestCollectLiveLimitaOsPontosDaResposta(t *testing.T) {
	f := &liveFake{selCalls: map[string]int{}}
	dt := func(string) (DynatraceAPI, error) { return f, nil }
	now := time.Now()
	req := LiveRequest{Items: []Item{{"c1", "ns", "a"}}, Window: Window{Start: now.Add(-6 * time.Hour), End: now}, MaxPoints: 5} // 5 < mínimo: sobe para 12
	res, err := CollectLive(context.Background(), req, LiveDeps{Dynatrace: dt}, now)
	if err != nil {
		t.Fatal(err)
	}
	// 360 pontos de 1 min, máximo 12 -> grupos de 30 min
	if res.StepSeconds != 30*60 {
		t.Errorf("step = %ds, esperado 1800 (360 pontos em no máximo 12)", res.StepSeconds)
	}
	req.MaxPoints = 100000 // acima do teto: 600
	res, _ = CollectLive(context.Background(), req, LiveDeps{Dynatrace: dt}, now)
	if res.StepSeconds != 60 {
		t.Errorf("com teto 600 e 360 pontos o passo fica em 60s, veio %d", res.StepSeconds)
	}
}

func TestBuildLiveTotalDeErrosSoContaIntervalosCompletosESemDadoENulo(t *testing.T) {
	ts := []int64{0, 60000, 120000}
	svcs := []ServiceTraffic{{ID: "A", Name: "A", Requests: ser(ts, 60, 60, 60), Err4xx: ser(ts, 10, 20, 1000), Err5xx: ser(ts, nan, nan, nan)}}
	// agora = 2m30s: o intervalo de 2m ainda não terminou e não entra no total
	got := BuildLive(svcs, time.Minute, DefaultP99Floor, time.UnixMilli(150000))
	if got.Err4xxTotal == nil || *got.Err4xxTotal != 30 {
		t.Errorf("4xx total = %v, esperado 30 (sem o intervalo incompleto)", got.Err4xxTotal)
	}
	if got.Err5xxTotal != nil {
		t.Errorf("sem dado de 5xx o total tem de ser nulo, veio %v", *got.Err5xxTotal)
	}
}

func TestDownsampleMantemOTotalExato(t *testing.T) {
	s := LiveSeries{Times: []int64{0, 1, 2}, RPS: []*float64{fp(1), fp(1), fp(1)}, Err4xxRPS: []*float64{fp(1), fp(1), fp(1)}, Err5xxRPS: []*float64{nil, nil, nil}, Err4xxTotal: fp(19197)}
	if got := Downsample(s, 2); got.Err4xxTotal == nil || *got.Err4xxTotal != 19197 {
		t.Errorf("a redução de pontos não pode mexer no total: %v", got.Err4xxTotal)
	}
}
