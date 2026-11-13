package report

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s-hpa-manager/internal/dynatrace"
)

// dtWorld é um "Dynatrace" simulado com uma verdade conhecida: o valor de cada ponto vem de uma função do
// instante e da versão do mundo. Os últimos pontos (menos de 3 intervalos atrás) MUDAM a cada versão, como o
// Dynatrace revisa os dados recentes; os antigos não. Assim dá para comparar, a cada passo, o que o cache devolve
// com o que uma busca completa devolveria.
type dtWorld struct {
	mu      sync.Mutex
	nowMs   int64
	version int64
	oldRev  map[int64]float64 // revisões forçadas de pontos antigos (para o teste da busca completa periódica)

	calls   []dtCall
	resolve int
	failErr error
	noData  map[string]bool
}

type dtCall struct {
	selector string
	ids      []string
	from, to int64
	points   int
}

func (w *dtWorld) value(id string, t, resMs int64) float64 {
	v := float64((t/resMs)%97) + float64(len(id))
	if r, ok := w.oldRev[t]; ok {
		v = r
	}
	if t > w.nowMs-3*resMs {
		v += float64(w.version) * 1000
	}
	return v
}

// answer é o que o Dynatrace responde para a janela: um ponto por intervalo, do intervalo que contém `from` até
// o que contém `to` (o último, ainda incompleto).
func (w *dtWorld) answer(ids []string, from, to int64, res string, scale float64) map[string]dynatrace.NullableSeries {
	d, _ := parseResolution(res)
	resMs := d.Milliseconds()
	out := map[string]dynatrace.NullableSeries{}
	if resMs <= 0 { // resolução que o Dynatrace real recusaria: sem dado
		return out
	}
	for _, id := range ids {
		if w.noData[id] {
			continue
		}
		s := dynatrace.NullableSeries{}
		for t := (from / resMs) * resMs; t <= to; t += resMs {
			s.Times = append(s.Times, t)
			s.Values = append(s.Values, w.value(id, t, resMs)*scale)
		}
		out[id] = s
	}
	return out
}

func (w *dtWorld) QueryServiceSeries(_ context.Context, selector string, ids []string, from, to time.Time, res string, scale float64) (map[string]dynatrace.NullableSeries, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failErr != nil {
		return nil, w.failErr
	}
	out := w.answer(ids, from.UnixMilli(), to.UnixMilli(), res, scale)
	n := 0
	for _, s := range out {
		n += len(s.Times)
	}
	w.calls = append(w.calls, dtCall{selector, append([]string(nil), ids...), from.UnixMilli(), to.UnixMilli(), n})
	return out, nil
}

func (w *dtWorld) ResolveEntryServices(_ context.Context, cluster, namespace, workload string) ([]dynatrace.ServiceRef, []string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.resolve++
	if w.failErr != nil {
		return nil, nil, w.failErr
	}
	return []dynatrace.ServiceRef{{ID: "SERVICE-" + workload, Name: workload, Type: "WEB_SERVICE"}}, []string{"nota " + workload}, nil
}

func (w *dtWorld) EntityURL(id string) string { return "https://dt.example/ui/entity/" + id }

// truth é a resposta completa da janela no estado atual do mundo, sem cache.
func (w *dtWorld) truth(ids []string, from, to int64, res string, scale float64) map[string]dynatrace.NullableSeries {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.answer(ids, from, to, res, scale)
}

func (w *dtWorld) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}
func (w *dtWorld) last() dtCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls[len(w.calls)-1]
}

func sameSeries(t *testing.T, got, want map[string]dynatrace.NullableSeries, msg string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d séries, esperado %d", msg, len(got), len(want))
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Fatalf("%s: falta a série %s", msg, id)
		}
		if len(g.Times) != len(w.Times) || len(g.Values) != len(w.Values) {
			t.Fatalf("%s: %s tem %d pontos, esperado %d", msg, id, len(g.Times), len(w.Times))
		}
		for i := range w.Times {
			if g.Times[i] != w.Times[i] || !(g.Values[i] == w.Values[i] || (math.IsNaN(g.Values[i]) && math.IsNaN(w.Values[i]))) {
				t.Fatalf("%s: %s ponto %d = (%d, %v), esperado (%d, %v)", msg, id, i, g.Times[i], g.Values[i], w.Times[i], w.Values[i])
			}
		}
	}
}

// clock é o relógio do cache nos testes (andamos junto com o mundo simulado).
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newCache(c *clock) *DTCache {
	dc := NewDTCache()
	dc.now = c.now
	return dc
}

const sel = "builtin:service.requestCount.total:splitBy(\"dt.entity.service\"):sum"

var T0 = time.Date(2026, 11, 13, 12, 0, 7, 0, time.UTC)

// O teste que importa: a cada passo de uma janela de 60 min que desliza de 1 em 1 min, com os últimos pontos sendo
// revisados, o cache devolve EXATAMENTE o que uma busca completa devolveria, gastando bem menos pontos.
func TestJanelaDeslizanteIgualABuscaCompleta(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{}
	dc := newCache(clk)
	api := dc.Wrap("u|c1", w)
	ids := []string{"A", "BB", "CCC"}
	var fetchedAfterFirst, fullPoints int
	for i := 0; i < 45; i++ {
		now := T0.Add(time.Duration(i) * time.Minute)
		clk.t, w.nowMs, w.version = now, now.UnixMilli(), int64(i)
		from, to := now.Add(-60*time.Minute), now
		got, err := api.QueryServiceSeries(context.Background(), sel, ids, from, to, "1m", 1)
		if err != nil {
			t.Fatal(err)
		}
		sameSeries(t, got, w.truth(ids, from.UnixMilli(), to.UnixMilli(), "1m", 1), "passo "+time.Duration(i).String())
		if i == 0 {
			fullPoints = w.last().points
		} else {
			// a busca incremental nunca volta mais que a sobreposição; a cada 30 min o cache refaz a janela inteira
			// de propósito (corrige revisões antigas), e nesse passo a busca começa no início da janela
			c := w.last()
			if i == 30 {
				if c.from != now.Add(-60*time.Minute).UnixMilli() {
					t.Fatalf("passo 30: esperada a busca completa periódica, a busca começou em %d", c.from)
				}
				continue
			}
			if c.from < now.Add(-time.Duration(minOverlap)-2*time.Minute).UnixMilli() {
				t.Fatalf("passo %d: busca começou em %d, mais atrás que a sobreposição", i, c.from)
			}
			fetchedAfterFirst += c.points
		}
	}
	// 43 passos incrementais: cada um busca ~7 pontos por série em vez de 61
	if fetchedAfterFirst >= 43*fullPoints/4 {
		t.Errorf("incremental buscou %d pontos em 43 passos (uma busca completa tem %d): não economizou", fetchedAfterFirst, fullPoints)
	}
	st := api.Stats()
	if st.SeriesFull != 6 || st.SeriesIncremental != 3*43 { // 3 séries: busca completa no passo 0 e de novo no 30
		t.Errorf("contagem: %+v, esperado 6 completas e %d incrementais", st, 3*43)
	}
}

// A revisão de um ponto ANTIGO (fora da sobreposição) só é corrigida na busca completa periódica.
func TestBuscaCompletaPeriodicaCorrigeRevisaoAntiga(t *testing.T) {
	w := &dtWorld{oldRev: map[int64]float64{}}
	clk := &clock{}
	dc := newCache(clk)
	dc.fullRefresh = 10 * time.Minute
	api := dc.Wrap("u|c1", w)
	ids := []string{"A"}
	step := func(i int) (map[string]dynatrace.NullableSeries, map[string]dynatrace.NullableSeries) {
		now := T0.Add(time.Duration(i) * time.Minute)
		clk.t, w.nowMs = now, now.UnixMilli()
		from, to := now.Add(-60*time.Minute), now
		got, err := api.QueryServiceSeries(context.Background(), sel, ids, from, to, "1m", 1)
		if err != nil {
			t.Fatal(err)
		}
		return got, w.truth(ids, from.UnixMilli(), to.UnixMilli(), "1m", 1)
	}
	got, want := step(0)
	sameSeries(t, got, want, "passo 0")
	// o Dynatrace revisa um ponto de 30 min atrás
	old := (T0.UnixMilli() / 60000 * 60000) - 30*60000
	w.oldRev[old] = 424242
	got, want = step(1)
	if got["A"].Values[indexOf(got["A"].Times, old)] == 424242 {
		t.Fatal("a revisão antiga apareceu antes da hora: a busca incremental não deveria ter olhado tão atrás")
	}
	_ = want
	// passado o intervalo de busca completa, a série é refeita e a revisão entra
	got, want = step(12)
	sameSeries(t, got, want, "depois da busca completa periódica")
	if v := got["A"].Values[indexOf(got["A"].Times, old)]; v != 424242 {
		t.Errorf("revisão antiga = %v, esperado 424242 depois da busca completa", v)
	}
}

func indexOf(ts []int64, t int64) int {
	for i, x := range ts {
		if x == t {
			return i
		}
	}
	return -1
}

func TestJanelaQueCresceParaTrasOuTemBuracoFazBuscaCompleta(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	api := newCache(clk).Wrap("u|c1", w)
	ids := []string{"A"}
	q := func(from, to time.Time) map[string]dynatrace.NullableSeries {
		got, err := api.QueryServiceSeries(context.Background(), sel, ids, from, to, "1m", 1)
		if err != nil {
			t.Fatal(err)
		}
		sameSeries(t, got, w.truth(ids, from.UnixMilli(), to.UnixMilli(), "1m", 1), "resultado")
		return got
	}
	q(T0.Add(-15*time.Minute), T0)
	// cresce para trás (15 min -> 60 min): o cache não tem o começo, então busca tudo
	clk.t = T0.Add(time.Minute)
	w.nowMs = clk.t.UnixMilli()
	q(clk.t.Add(-60*time.Minute), clk.t)
	if c := w.last(); c.from != clk.t.Add(-60*time.Minute).UnixMilli() {
		t.Errorf("janela maior: a busca começou em %d, esperado o início da janela inteira", c.from)
	}
	// longa pausa (o cache termina antes do começo da nova janela): busca completa
	clk.t = T0.Add(3 * time.Hour)
	w.nowMs = clk.t.UnixMilli()
	q(clk.t.Add(-30*time.Minute), clk.t)
	if c := w.last(); c.from != clk.t.Add(-30*time.Minute).UnixMilli() {
		t.Errorf("buraco: a busca começou em %d, esperado o início da janela inteira", c.from)
	}
	if st := api.Stats(); st.SeriesFull != 3 || st.SeriesIncremental != 0 {
		t.Errorf("contagem: %+v, esperado 3 completas", st)
	}
}

func TestSerieNovaNaMesmaChamadaBuscaCompletaEAsOutrasIncremental(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	api := newCache(clk).Wrap("u|c1", w)
	call := func(now time.Time, ids []string) {
		clk.t, w.nowMs = now, now.UnixMilli()
		got, err := api.QueryServiceSeries(context.Background(), sel, ids, now.Add(-30*time.Minute), now, "1m", 1)
		if err != nil {
			t.Fatal(err)
		}
		sameSeries(t, got, w.truth(ids, now.Add(-30*time.Minute).UnixMilli(), now.UnixMilli(), "1m", 1), "resultado")
	}
	call(T0, []string{"A"})
	before := w.callCount()
	call(T0.Add(time.Minute), []string{"A", "B"})
	if n := w.callCount() - before; n != 2 {
		t.Fatalf("%d chamadas, esperado 2 (B por inteiro e A só o trecho novo)", n)
	}
	st := api.Stats()
	if st.SeriesFull != 2 || st.SeriesIncremental != 1 {
		t.Errorf("contagem: %+v", st)
	}
}

// Uma janela inteiramente no passado (o dia anterior da projeção) já é definitiva: a segunda consulta não busca nada,
// e quando a janela avança só busca o trecho novo, sem sobreposição.
func TestJanelaDoPassadoNaoRebuscaASobreposicao(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	api := newCache(clk).Wrap("u|c1", w)
	ids := []string{"A"}
	from, to := T0.Add(-26*time.Hour), T0.Add(-24*time.Hour)
	q := func(from, to time.Time) {
		got, err := api.QueryServiceSeries(context.Background(), sel, ids, from, to, "1m", 1)
		if err != nil {
			t.Fatal(err)
		}
		sameSeries(t, got, w.truth(ids, from.UnixMilli(), to.UnixMilli(), "1m", 1), "resultado")
	}
	q(from, to)
	n := w.callCount()
	q(from, to)
	if w.callCount() != n {
		t.Errorf("a mesma janela do passado voltou a buscar (%d chamadas)", w.callCount()-n)
	}
	if st := api.Stats(); st.SeriesCached != 1 {
		t.Errorf("deveria contar 1 série servida do cache: %+v", st)
	}
	q(from.Add(5*time.Minute), to.Add(5*time.Minute)) // a janela avança 5 min
	if c := w.last(); c.from != to.UnixMilli() {
		t.Errorf("busca começou em %d, esperado %d (o fim do cache, sem sobreposição)", c.from, to.UnixMilli())
	}
}

func TestCacheNaoCruzaUsuarioSeletorResolucaoNemEscala(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	dc := newCache(clk)
	ids := []string{"A"}
	from, to := T0.Add(-30*time.Minute), T0
	run := func(prefix, selector, res string, scale float64) map[string]dynatrace.NullableSeries {
		got, err := dc.Wrap(prefix, w).QueryServiceSeries(context.Background(), selector, ids, from, to, res, scale)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	run("u1|c1", sel, "1m", 1)
	n := w.callCount()
	for _, c := range []struct {
		name, prefix, sel, res string
		scale                  float64
	}{
		{"outro usuário", "u2|c1", sel, "1m", 1},
		{"outro cluster", "u1|c2", sel, "1m", 1},
		{"outra métrica", "u1|c1", "builtin:service.errors.fivexx.count", "1m", 1},
		{"outra resolução", "u1|c1", sel, "5m", 1},
		{"outra escala", "u1|c1", sel, "1m", 0.001},
	} {
		before := w.callCount()
		got := run(c.prefix, c.sel, c.res, c.scale)
		if w.callCount() != before+1 {
			t.Errorf("%s: deveria buscar por inteiro (não pode herdar o cache de outra chave)", c.name)
		}
		d, _ := parseResolution(c.res)
		_ = d
		sameSeries(t, got, w.truth(ids, from.UnixMilli(), to.UnixMilli(), c.res, c.scale), c.name)
	}
	_ = n
}

func TestResolucaoDesconhecidaOuJanelaInvalidaPassaDireto(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	api := newCache(clk).Wrap("u|c", w)
	for _, res := range []string{"", "x", "0m", "-1m", "1s"} {
		if _, err := api.QueryServiceSeries(context.Background(), sel, []string{"A"}, T0.Add(-time.Hour), T0, res, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := api.QueryServiceSeries(context.Background(), sel, []string{"A"}, T0, T0, "1m", 1); err != nil {
		t.Fatal(err)
	}
	if st := api.Stats(); !st.Empty() {
		t.Errorf("sem cache nenhuma contagem deveria subir: %+v", st)
	}
}

func TestErroDeBuscaPropagaENaoEstragaOCache(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	api := newCache(clk).Wrap("u|c", w)
	ids := []string{"A"}
	if _, err := api.QueryServiceSeries(context.Background(), sel, ids, T0.Add(-30*time.Minute), T0, "1m", 1); err != nil {
		t.Fatal(err)
	}
	clk.t = T0.Add(time.Minute)
	w.nowMs = clk.t.UnixMilli()
	w.failErr = errors.New("timeout")
	if _, err := api.QueryServiceSeries(context.Background(), sel, ids, clk.t.Add(-30*time.Minute), clk.t, "1m", 1); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("o erro do Dynatrace deveria subir, veio %v", err)
	}
	w.failErr = nil
	got, err := api.QueryServiceSeries(context.Background(), sel, ids, clk.t.Add(-30*time.Minute), clk.t, "1m", 1)
	if err != nil {
		t.Fatal(err)
	}
	sameSeries(t, got, w.truth(ids, clk.t.Add(-30*time.Minute).UnixMilli(), clk.t.UnixMilli(), "1m", 1), "depois do erro")
}

func TestSerieSemDadoNaoApareceNaRespostaComoNoDynatraceReal(t *testing.T) {
	w := &dtWorld{noData: map[string]bool{"VAZIO": true}}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	api := newCache(clk).Wrap("u|c", w)
	ids := []string{"A", "VAZIO"}
	for i := 0; i < 3; i++ {
		now := T0.Add(time.Duration(i) * time.Minute)
		clk.t, w.nowMs = now, now.UnixMilli()
		got, err := api.QueryServiceSeries(context.Background(), sel, ids, now.Add(-20*time.Minute), now, "1m", 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["VAZIO"]; ok {
			t.Fatal("série sem dado não pode aparecer na resposta")
		}
		sameSeries(t, got, w.truth(ids, now.Add(-20*time.Minute).UnixMilli(), now.UnixMilli(), "1m", 1), "resultado")
	}
}

func TestResolucaoEmCacheComValidadeEErroNaoGuardado(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	api := newCache(clk).Wrap("u|c1", w)
	ctx := context.Background()
	s1, n1, err := api.ResolveEntryServices(ctx, "c1", "Calc", "Frete-Hub")
	if err != nil || len(s1) != 1 || len(n1) != 1 {
		t.Fatalf("1ª chamada: %v %v %v", s1, n1, err)
	}
	if _, _, err := api.ResolveEntryServices(ctx, "C1", "calc", "frete-hub"); err != nil { // caixa não importa
		t.Fatal(err)
	}
	if w.resolve != 1 {
		t.Errorf("%d resoluções no Dynatrace, esperado 1 (a segunda veio do cache)", w.resolve)
	}
	// o que o chamador recebe é cópia: mexer nela não suja o cache
	s1[0].Name = "MEXIDO"
	n1[0] = "MEXIDO"
	s2, n2, _ := api.ResolveEntryServices(ctx, "c1", "calc", "frete-hub")
	if s2[0].Name == "MEXIDO" || n2[0] == "MEXIDO" {
		t.Error("o cache devolveu a mesma memória que o chamador alterou")
	}
	// passada a validade, resolve de novo
	clk.t = clk.t.Add(resolveTTL + time.Second)
	if _, _, err := api.ResolveEntryServices(ctx, "c1", "calc", "frete-hub"); err != nil {
		t.Fatal(err)
	}
	if w.resolve != 2 {
		t.Errorf("depois da validade: %d resoluções, esperado 2", w.resolve)
	}
	// erro não é guardado: a próxima chamada tenta de novo
	w.failErr = errors.New("sem process group")
	if _, _, err := api.ResolveEntryServices(ctx, "c1", "calc", "outra"); err == nil {
		t.Fatal("o erro deveria subir")
	}
	w.failErr = nil
	if _, _, err := api.ResolveEntryServices(ctx, "c1", "calc", "outra"); err != nil {
		t.Fatalf("depois do erro deveria resolver: %v", err)
	}
	if st := api.Stats(); st.ResolveHits != 2 || st.ResolveMisses != 3 {
		t.Errorf("contagem: %+v, esperado 2 acertos e 3 buscas", st)
	}
	// outro usuário não aproveita
	n := w.resolve
	other := newCache(clk).Wrap("u2|c1", w)
	if _, _, err := other.ResolveEntryServices(ctx, "c1", "calc", "frete-hub"); err != nil {
		t.Fatal(err)
	}
	if w.resolve != n+1 {
		t.Error("outro cache/usuário deveria resolver por conta própria")
	}
}

func TestLimiteDePontosDescartaOsMenosUsados(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	dc := newCache(clk)
	dc.maxPoints = 200
	api := dc.Wrap("u|c", w)
	for i, id := range []string{"A", "B", "C", "D", "E", "F"} {
		clk.t = T0.Add(time.Duration(i) * time.Second)
		if _, err := api.QueryServiceSeries(context.Background(), sel, []string{id}, T0.Add(-60*time.Minute), T0, "1m", 1); err != nil {
			t.Fatal(err)
		}
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.points > 200 {
		t.Errorf("%d pontos guardados, o limite é 200", dc.points)
	}
	sum := 0
	for _, e := range dc.series {
		sum += len(e.times)
	}
	if sum != dc.points {
		t.Errorf("contador de pontos (%d) fora de sincronia com o que está guardado (%d)", dc.points, sum)
	}
	if _, ok := dc.series[seriesKey("u|c", sel, "F", "1m", 1)]; !ok {
		t.Error("a série usada por último deveria ter ficado")
	}
}

func TestSeriesParadasHaMuitoTempoSaem(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	dc := newCache(clk)
	api := dc.Wrap("u|c", w)
	if _, err := api.QueryServiceSeries(context.Background(), sel, []string{"A"}, T0.Add(-30*time.Minute), T0, "1m", 1); err != nil {
		t.Fatal(err)
	}
	clk.t = T0.Add(seriesIdleTTL + 2*time.Minute)
	w.nowMs = clk.t.UnixMilli()
	if _, err := api.QueryServiceSeries(context.Background(), sel, []string{"B"}, clk.t.Add(-30*time.Minute), clk.t, "1m", 1); err != nil {
		t.Fatal(err)
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if _, ok := dc.series[seriesKey("u|c", sel, "A", "1m", 1)]; ok {
		t.Error("a série parada há mais que a validade deveria ter saído")
	}
	if dc.points != len(dc.series[seriesKey("u|c", sel, "B", "1m", 1)].times) {
		t.Errorf("contador de pontos errado depois da limpeza: %d", dc.points)
	}
}

func TestEntityURLEhRepassado(t *testing.T) {
	w := &dtWorld{}
	api := NewDTCache().Wrap("u|c", w)
	if got := api.EntityURL("SERVICE-1"); got != "https://dt.example/ui/entity/SERVICE-1" {
		t.Errorf("EntityURL = %q", got)
	}
	var _ DynatraceAPI = api
	var _ entityURLer = api
}

// Várias requisições ao mesmo tempo (duas telas abertas, ou o "Atualizar agora" junto do automático) não podem
// corromper o cache nem dar valor diferente da busca completa. Rode com -race.
func TestRequisicoesConcorrentes(t *testing.T) {
	w := &dtWorld{}
	clk := &clock{t: T0}
	w.nowMs = T0.UnixMilli()
	dc := newCache(clk)
	ids := []string{"A", "B"}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			api := dc.Wrap("u|c", w)
			for i := 0; i < 20; i++ {
				// o mundo e o relógio ficam parados: só a concorrência é testada
				got, err := api.QueryServiceSeries(context.Background(), sel, ids, T0.Add(-time.Duration(30+g%3)*time.Minute), T0, "1m", 1)
				if err != nil {
					t.Error(err)
					return
				}
				for _, id := range ids {
					if len(got[id].Times) == 0 || len(got[id].Times) != len(got[id].Values) {
						t.Errorf("resposta inconsistente para %s", id)
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
}
