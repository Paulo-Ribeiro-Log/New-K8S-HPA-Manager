package report

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s-hpa-manager/internal/dynatrace"
)

// Cache do Dynatrace para o Tempo real, só em memória e entre requisições. O Tempo real repete a mesma consulta a
// cada 1 a 5 minutos; sem cache cada repetição refaz tudo. Duas coisas são guardadas, separadas:
//
//  1. O mapa aplicação -> serviços de entrada (ResolveEntryServices). Custa pelo menos duas chamadas por aplicação
//     (process group e lista de serviços) e quase nunca muda, então vale por resolveTTL.
//  2. As séries por serviço (QueryServiceSeries): na repetição busca só o trecho NOVO e junta ao que já tem.
//
// A regra que protege contra valor errado em silêncio: o resultado devolvido para uma janela é idêntico ao de uma
// busca completa dessa janela (testado contra uma "verdade" que muda nos últimos pontos). Para isso:
//   - a busca incremental SOBREPÕE os últimos pontos (o Dynatrace revisa os recentes e o último intervalo chega
//     incompleto): tudo a partir de (fim do cache - sobreposição) é substituído pelo que veio;
//   - de tempos em tempos (fullRefreshEvery) a série é refeita por inteiro, para corrigir revisões antigas;
//   - qualquer dúvida (janela que cresce para trás, buracos, resolução desconhecida) cai em busca completa.
//
// Nada aqui é compartilhado entre usuários: a chave leva o usuário e o cluster.

const (
	resolveTTL        = 10 * time.Minute
	seriesIdleTTL     = 30 * time.Minute
	fullRefreshEvery  = 30 * time.Minute
	minOverlap        = 5 * time.Minute
	maxResolveEntries = 4000
	maxSeriesPoints   = 3_000_000 // ~48 MB (16 bytes por ponto)
	sweepEvery        = time.Minute
)

// CacheStats diz o quanto o cache ajudou numa requisição (a tela mostra, para o operador ver que funciona).
type CacheStats struct {
	ResolveHits       int `json:"resolve_hits"`       // aplicações cujo mapa de serviços veio do cache
	ResolveMisses     int `json:"resolve_misses"`     // aplicações resolvidas no Dynatrace
	SeriesFull        int `json:"series_full"`        // séries buscadas por inteiro
	SeriesIncremental int `json:"series_incremental"` // séries em que só o trecho novo foi buscado
	SeriesCached      int `json:"series_cached"`      // séries servidas sem nenhuma busca
}

type resolveEntry struct {
	svcs  []dynatrace.ServiceRef
	notes []string
	at    time.Time
}

// seriesEntry é imutável depois de guardado: toda atualização cria outro e troca o ponteiro, então quem leu um
// snapshot nunca vê a série mudar por baixo.
type seriesEntry struct {
	times  []int64
	vals   []float64
	from   int64 // início (ms) da janela que a última busca cobriu
	to     int64 // fim (ms) da janela que a última busca cobriu
	fullAt time.Time
	// at é quando a ponta da série foi buscada pela última vez. Se foi perto do fim da janela (busca "ao vivo"), os
	// últimos pontos podem estar incompletos ou ser revisados e a próxima busca os refaz; se foi bem depois (janela do
	// passado, como o dia anterior da projeção), eles já são definitivos e nada precisa ser rebuscado.
	at   time.Time
	used time.Time
}

// DTCache é o cache compartilhado entre as requisições do Tempo real. O zero value não serve: use NewDTCache.
type DTCache struct {
	mu          sync.Mutex
	resolve     map[string]resolveEntry
	series      map[string]*seriesEntry
	points      int
	lastSweep   time.Time
	maxPoints   int
	now         func() time.Time
	resolveTTL  time.Duration
	fullRefresh time.Duration
}

func NewDTCache() *DTCache {
	return &DTCache{resolve: map[string]resolveEntry{}, series: map[string]*seriesEntry{}, now: time.Now, resolveTTL: resolveTTL, fullRefresh: fullRefreshEvery, maxPoints: maxSeriesPoints}
}

// Wrap devolve o cliente do Dynatrace com cache. prefix identifica quem consulta (usuário e cluster): o cache não
// atravessa usuários, porque o que cada um enxerga depende do token dele.
func (c *DTCache) Wrap(prefix string, api DynatraceAPI) *CachedDynatrace {
	return &CachedDynatrace{c: c, api: api, prefix: prefix}
}

// CachedDynatrace satisfaz DynatraceAPI (e o EntityURL opcional do cliente real).
type CachedDynatrace struct {
	c      *DTCache
	api    DynatraceAPI
	prefix string

	resolveHits, resolveMisses, full, incremental, cached atomic.Int64
}

// Stats soma o que este cliente fez desde que foi criado.
func (w *CachedDynatrace) Stats() CacheStats {
	return CacheStats{
		ResolveHits: int(w.resolveHits.Load()), ResolveMisses: int(w.resolveMisses.Load()),
		SeriesFull: int(w.full.Load()), SeriesIncremental: int(w.incremental.Load()), SeriesCached: int(w.cached.Load()),
	}
}

// Add soma duas contagens (uma requisição pode usar um cliente por cluster).
func (s CacheStats) Add(o CacheStats) CacheStats {
	return CacheStats{
		ResolveHits: s.ResolveHits + o.ResolveHits, ResolveMisses: s.ResolveMisses + o.ResolveMisses,
		SeriesFull: s.SeriesFull + o.SeriesFull, SeriesIncremental: s.SeriesIncremental + o.SeriesIncremental, SeriesCached: s.SeriesCached + o.SeriesCached,
	}
}

// Empty diz que nada passou pelo cache.
func (s CacheStats) Empty() bool { return s == CacheStats{} }

// EntityURL repassa o link da entidade, quando o cliente de baixo sabe montá-lo.
func (w *CachedDynatrace) EntityURL(id string) string {
	if u, ok := w.api.(entityURLer); ok {
		return u.EntityURL(id)
	}
	return ""
}

func cloneRefs(in []dynatrace.ServiceRef) []dynatrace.ServiceRef {
	return append([]dynatrace.ServiceRef(nil), in...)
}
func cloneStrings(in []string) []string { return append([]string(nil), in...) }

// ResolveEntryServices devolve o mapa do cache enquanto ele é recente. Erro nunca é guardado.
func (w *CachedDynatrace) ResolveEntryServices(ctx context.Context, cluster, namespace, workload string) ([]dynatrace.ServiceRef, []string, error) {
	key := w.prefix + "|" + strings.ToLower(cluster) + "|" + strings.ToLower(namespace) + "|" + strings.ToLower(workload)
	c := w.c
	c.mu.Lock()
	e, ok := c.resolve[key]
	fresh := ok && c.now().Sub(e.at) < c.resolveTTL
	c.mu.Unlock()
	if fresh {
		w.resolveHits.Add(1)
		return cloneRefs(e.svcs), cloneStrings(e.notes), nil
	}
	svcs, notes, err := w.api.ResolveEntryServices(ctx, cluster, namespace, workload)
	if err != nil {
		return nil, nil, err
	}
	w.resolveMisses.Add(1)
	c.mu.Lock()
	if len(c.resolve) >= maxResolveEntries {
		c.resolve = map[string]resolveEntry{} // cheio: recomeça (são poucas centenas de aplicações por usuário)
	}
	c.resolve[key] = resolveEntry{svcs: cloneRefs(svcs), notes: cloneStrings(notes), at: c.now()}
	c.mu.Unlock()
	return svcs, notes, nil
}

// parseResolution lê "1m", "5m", "1h", "1d". Resolução que não se entende desliga o cache para aquela chamada.
func parseResolution(s string) (time.Duration, bool) {
	if len(s) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, false
	}
	switch s[len(s)-1] {
	case 'm':
		return time.Duration(n) * time.Minute, true
	case 'h':
		return time.Duration(n) * time.Hour, true
	case 'd':
		return time.Duration(n) * 24 * time.Hour, true
	}
	return 0, false
}

func seriesKey(prefix, selector, id, resolution string, scale float64) string {
	return prefix + "|" + selector + "|" + id + "|" + resolution + "|" + strconv.FormatFloat(scale, 'g', -1, 64)
}

// QueryServiceSeries busca só o que falta. O resultado para a janela pedida é o mesmo de uma busca completa.
func (w *CachedDynatrace) QueryServiceSeries(ctx context.Context, selector string, ids []string, from, to time.Time, resolution string, scale float64) (map[string]dynatrace.NullableSeries, error) {
	resD, ok := parseResolution(resolution)
	if !ok || !to.After(from) || len(ids) == 0 {
		return w.api.QueryServiceSeries(ctx, selector, ids, from, to, resolution, scale)
	}
	c := w.c
	fromMs, toMs, resMs := from.UnixMilli(), to.UnixMilli(), resD.Milliseconds()
	overlap := minOverlap
	if 3*resD > overlap {
		overlap = 3 * resD
	}
	overlapMs := overlap.Milliseconds()
	now := c.now()

	// 1) o que há no cache para cada série, e de onde buscar
	type job struct {
		id   string
		e    *seriesEntry // nulo = busca completa
		need int64        // início da busca
	}
	c.mu.Lock()
	jobs := make([]job, 0, len(ids))
	for _, id := range ids {
		e := c.series[seriesKey(w.prefix, selector, id, resolution, scale)]
		usable := e != nil && e.from <= fromMs && e.to >= fromMs && now.Sub(e.fullAt) < c.fullRefresh
		if !usable {
			jobs = append(jobs, job{id: id, need: fromMs})
			continue
		}
		need := e.to
		if e.at.UnixMilli()-e.to < overlapMs { // ponta buscada ao vivo: refaz os últimos pontos
			need = e.to - overlapMs
		}
		if need < fromMs {
			need = fromMs
		}
		jobs = append(jobs, job{id: id, e: e, need: need})
	}
	c.mu.Unlock()

	// 2) uma chamada por (início da busca); sem nada novo a buscar, serve do cache
	groups := map[int64][]string{}
	for _, j := range jobs {
		if j.need < toMs {
			groups[j.need] = append(groups[j.need], j.id)
		}
	}
	fetched := map[int64]map[string]dynatrace.NullableSeries{}
	needs := make([]int64, 0, len(groups))
	for n := range groups {
		needs = append(needs, n)
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i] < needs[j] })
	for _, n := range needs {
		m, err := w.api.QueryServiceSeries(ctx, selector, groups[n], time.UnixMilli(n), to, resolution, scale)
		if err != nil {
			return nil, err
		}
		fetched[n] = m
	}

	// 3) junta, guarda e monta a resposta
	out := make(map[string]dynatrace.NullableSeries, len(ids))
	newEntries := map[string]*seriesEntry{}
	for _, j := range jobs {
		key := seriesKey(w.prefix, selector, j.id, resolution, scale)
		var fresh dynatrace.NullableSeries
		hasFetch := j.need < toMs
		if hasFetch {
			fresh = fetched[j.need][j.id]
		}
		var times []int64
		var vals []float64
		fullAt, tail := now, now
		switch {
		case j.e == nil: // busca completa: guarda e devolve exatamente o que o Dynatrace deu
			times, vals = fresh.Times, fresh.Values
			w.full.Add(1)
		case !hasFetch: // nada novo: tudo do cache
			times, vals, fullAt, tail = j.e.times, j.e.vals, j.e.fullAt, j.e.at
			w.cached.Add(1)
		default: // incremental: o cache até o ponto de corte + o que veio (que vale a partir dali)
			cut := j.need
			if len(fresh.Times) > 0 && fresh.Times[0] < cut {
				cut = fresh.Times[0]
			}
			k := sort.Search(len(j.e.times), func(i int) bool { return j.e.times[i] >= cut })
			times = append(append(make([]int64, 0, k+len(fresh.Times)), j.e.times[:k]...), fresh.Times...)
			vals = append(append(make([]float64, 0, k+len(fresh.Values)), j.e.vals[:k]...), fresh.Values...)
			if j.need <= fromMs {
				fullAt = now // refez a janela toda: conta como busca completa
			} else {
				fullAt = j.e.fullAt
			}
			w.incremental.Add(1)
		}
		// o que ficou inteiramente antes da janela não serve mais
		start := 0
		for start < len(times) && times[start]+resMs <= fromMs {
			start++
		}
		times, vals = times[start:], vals[start:]
		// pedido que termina antes do fim do que já temos não encolhe a cobertura guardada
		cover := toMs
		if !hasFetch && j.e != nil && j.e.to > cover {
			cover = j.e.to
		}
		newEntries[key] = &seriesEntry{times: times, vals: vals, from: fromMs, to: cover, fullAt: fullAt, at: tail, used: now}

		end := len(times)
		for end > 0 && times[end-1] > toMs {
			end--
		}
		if end > 0 {
			out[j.id] = dynatrace.NullableSeries{Times: append([]int64(nil), times[:end]...), Values: append([]float64(nil), vals[:end]...)}
		}
	}
	c.store(newEntries, now)
	return out, nil
}

// store guarda as entradas, tira as paradas há muito tempo e mantém o total de pontos dentro do limite
// (descartando as menos usadas).
func (c *DTCache) store(entries map[string]*seriesEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range entries {
		if old, ok := c.series[k]; ok {
			c.points -= len(old.times)
		}
		c.series[k] = e
		c.points += len(e.times)
	}
	if now.Sub(c.lastSweep) >= sweepEvery {
		c.lastSweep = now
		for k, e := range c.series {
			if now.Sub(e.used) > seriesIdleTTL {
				c.points -= len(e.times)
				delete(c.series, k)
			}
		}
	}
	if c.points <= c.maxPoints {
		return
	}
	type kv struct {
		k string
		e *seriesEntry
	}
	all := make([]kv, 0, len(c.series))
	for k, e := range c.series {
		all = append(all, kv{k, e})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].e.used.Before(all[j].e.used) })
	for _, x := range all {
		if c.points <= c.maxPoints*9/10 {
			break
		}
		if _, fresh := entries[x.k]; fresh {
			continue // o que acabou de ser usado fica
		}
		c.points -= len(x.e.times)
		delete(c.series, x.k)
	}
}
