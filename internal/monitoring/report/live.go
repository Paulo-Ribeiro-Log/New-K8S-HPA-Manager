package report

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"k8s-hpa-manager/internal/dynatrace"
)

// Tempo real: as MESMAS métricas do relatório (tráfego, erros e latência do Dynatrace), mas como série
// no tempo e numa janela que termina "agora". É consultado a cada 1 a 5 minutos pela tela; por isso
// faz poucas chamadas: resolve os serviços de entrada de cada aplicação (cache do cliente) e pede cada
// métrica UMA vez por cluster, com os serviços de todas as aplicações juntos.

// LiveMaxItems limita as aplicações de uma consulta (o custo é por cluster, não por aplicação, mas a
// resolução dos process groups e o tamanho do seletor crescem com o número).
const LiveMaxItems = 100

const (
	// LiveDefaultPoints é o padrão de pontos por série; LiveMinPoints e LiveMaxPoints limitam o pedido.
	LiveDefaultPoints = 120
	LiveMinPoints     = 12
	LiveMaxPoints     = 600
)

// LiveRequest é o pedido: aplicações, janela (já em instantes absolutos) e percentis a consultar.
type LiveRequest struct {
	Items       []Item
	Window      Window
	Percentiles []int
	// MaxPoints limita os pontos por série na resposta (0 = LiveDefaultPoints). Janelas longas viram
	// poucas dezenas de barras: o cartão não desenha mais que isso e a resposta fica pequena.
	MaxPoints int
	// Resources liga as réplicas, restarts, CPU e memória (Prometheus). Os percentis continuam do Dynatrace.
	Resources bool
}

// LiveDeps são as fontes: o Dynatrace (tráfego, erros e latência) e, opcional, o Prometheus (consumo).
type LiveDeps struct {
	Dynatrace  func(cluster string) (DynatraceAPI, error)
	Prometheus func(cluster string) (RangeQuerier, error)
}

// LiveSeries é a série de uma aplicação. Todas as listas têm o mesmo tamanho de Times; nulo = sem dado
// naquele instante (nunca zero, que é um valor real).
type LiveSeries struct {
	Item
	Error     string     `json:"error,omitempty"`
	Times     []int64    `json:"times"` // início de cada intervalo, epoch em ms
	RPS       []*float64 `json:"rps"`
	Err4xxRPS []*float64 `json:"err_4xx_rps"`
	Err5xxRPS []*float64 `json:"err_5xx_rps"`
	// Totais no período (contagem inteira, somada antes de reduzir os pontos). Nulo = sem dado.
	Err4xxTotal *float64       `json:"err_4xx_total"`
	Err5xxTotal *float64       `json:"err_5xx_total"`
	P50Ms       []*float64     `json:"p50_ms,omitempty"`
	P95Ms       []*float64     `json:"p95_ms,omitempty"`
	P99Ms       []*float64     `json:"p99_ms,omitempty"`
	Resources   *LiveResources `json:"resources,omitempty"`
	Services    []string       `json:"services,omitempty"`
	Resolution  string         `json:"resolution,omitempty"`
	Notes       []string       `json:"notes,omitempty"`
}

// LiveResult é a resposta de uma consulta.
type LiveResult struct {
	Window      Window       `json:"window"`
	Resolution  string       `json:"resolution"`   // resolução do Dynatrace
	StepSeconds int64        `json:"step_seconds"` // intervalo de cada ponto da resposta (resolução × agrupamento)
	Percentiles []int        `json:"percentiles"`
	Warnings    []string     `json:"warnings,omitempty"`
	Series      []LiveSeries `json:"series"`
	GeneratedAt time.Time    `json:"generated_at"`
}

// byTime indexa uma série por instante.
func (s Series) byTime() map[int64]float64 {
	m := make(map[int64]float64, len(s.Times))
	for i, t := range s.Times {
		if i < len(s.Values) && !math.IsNaN(s.Values[i]) {
			m[t] = s.Values[i]
		}
	}
	return m
}

func nullable(v float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &v
}

// BuildLive junta os serviços de entrada de uma aplicação numa série por instante:
//   - RPS e erros/s: soma dos serviços (pontos de entrada disjuntos) dividida pelo intervalo;
//   - P50/P95/P99: o maior valor entre os serviços com fatia >= floor das requisições da janela (mesma
//     regra do pico do relatório: um endpoint com 4 requisições não pode ditar a latência).
//
// O último intervalo só entra se já terminou (início + resolução <= now): o Dynatrace entrega o bucket
// corrente incompleto, e ele apareceria como uma queda falsa de tráfego no fim do gráfico.
func BuildLive(svcs []ServiceTraffic, res time.Duration, floor float64, now time.Time) LiveSeries {
	out := LiveSeries{Times: []int64{}}
	var reqSeries []Series
	for _, s := range svcs {
		out.Services = append(out.Services, s.Name)
		reqSeries = append(reqSeries, s.Requests)
	}
	sort.Strings(out.Services)

	times, _ := sumByTime(reqSeries)
	cut := now.UnixMilli() - res.Milliseconds()
	for _, t := range times {
		if t <= cut {
			out.Times = append(out.Times, t)
		}
	}
	n := len(out.Times)
	resSec := res.Seconds()

	// rate devolve a taxa por segundo de cada intervalo e o total (contagem) do período
	rate := func(pick func(ServiceTraffic) Series) ([]*float64, *float64) {
		acc := map[int64]float64{}
		for _, s := range svcs {
			for t, v := range pick(s).byTime() {
				acc[t] += v
			}
		}
		r := make([]*float64, n)
		sum, any := 0.0, false
		for i, t := range out.Times {
			v, ok := acc[t]
			if ok {
				sum += v
				any = true
			}
			if resSec > 0 {
				v /= resSec
			}
			r[i] = nullable(v, ok)
		}
		return r, nullable(math.Round(sum), any)
	}
	out.RPS, _ = rate(func(s ServiceTraffic) Series { return s.Requests })
	out.Err4xxRPS, out.Err4xxTotal = rate(func(s ServiceTraffic) Series { return s.Err4xx })
	out.Err5xxRPS, out.Err5xxTotal = rate(func(s ServiceTraffic) Series { return s.Err5xx })

	// serviços elegíveis aos percentis: fatia das requisições da janela
	total := 0.0
	share := map[string]float64{}
	for _, s := range svcs {
		v, _ := s.Requests.sum()
		share[s.ID] = v
		total += v
	}
	pct := func(pick func(ServiceTraffic) Series) []*float64 {
		type ser struct{ m map[int64]float64 }
		var eligible []ser
		for _, s := range svcs {
			if total > 0 && share[s.ID]/total >= floor {
				eligible = append(eligible, ser{pick(s).byTime()})
			}
		}
		any := false
		r := make([]*float64, n)
		for i, t := range out.Times {
			best, ok := 0.0, false
			for _, e := range eligible {
				if v, has := e.m[t]; has && (!ok || v > best) {
					best, ok = v, true
				}
			}
			if ok {
				r[i] = &best
				any = true
			}
		}
		if !any {
			return nil
		}
		return r
	}
	out.P50Ms = pct(func(s ServiceTraffic) Series { return s.P50Ms })
	out.P95Ms = pct(func(s ServiceTraffic) Series { return s.P95Ms })
	out.P99Ms = pct(func(s ServiceTraffic) Series { return s.P99Ms })
	return out
}

// pointsPerBucket é quantos pontos de resolução entram em cada ponto da resposta.
func pointsPerBucket(window, res time.Duration, maxPoints int) int {
	if res <= 0 || maxPoints <= 0 {
		return 1
	}
	n := int(window / res)
	if n <= maxPoints {
		return 1
	}
	return (n + maxPoints - 1) / maxPoints
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// shrink agrupa de k em k pontos: média para taxas (RPS e erros/s: a média é o que a taxa significa) ou
// máximo para latência (o que importa é o pior momento do grupo). Grupo sem nenhum valor fica nulo.
// Arredonda para encurtar o JSON (a tela mostra no máximo 3 casas).
func shrink(a []*float64, k int, useMax bool, places int) []*float64 {
	if a == nil {
		return nil
	}
	out := make([]*float64, 0, (len(a)+k-1)/k)
	for i := 0; i < len(a); i += k {
		end := i + k
		if end > len(a) {
			end = len(a)
		}
		sum, best, cnt := 0.0, 0.0, 0
		for _, p := range a[i:end] {
			if p == nil {
				continue
			}
			sum += *p
			if cnt == 0 || *p > best {
				best = *p
			}
			cnt++
		}
		if cnt == 0 {
			out = append(out, nil)
			continue
		}
		v := sum / float64(cnt)
		if useMax {
			v = best
		}
		v = round(v, places)
		out = append(out, &v)
	}
	return out
}

// Downsample reduz a série para um ponto a cada k intervalos (k <= 1 só arredonda). Cada ponto novo
// leva o início do grupo.
func Downsample(s LiveSeries, k int) LiveSeries {
	if k < 1 {
		k = 1
	}
	var times []int64
	for i := 0; i < len(s.Times); i += k {
		times = append(times, s.Times[i])
	}
	if times == nil {
		times = []int64{}
	}
	s.Times = times
	s.RPS = shrink(s.RPS, k, false, 3)
	s.Err4xxRPS = shrink(s.Err4xxRPS, k, false, 3)
	s.Err5xxRPS = shrink(s.Err5xxRPS, k, false, 3)
	s.P50Ms = shrink(s.P50Ms, k, true, 1)
	s.P95Ms = shrink(s.P95Ms, k, true, 1)
	s.P99Ms = shrink(s.P99Ms, k, true, 1)
	return s
}

// CollectLive roda a consulta. dt devolve o cliente do Dynatrace de um cluster. Falha de uma aplicação
// (ou de um cluster) vira o Error dela; as demais seguem.
func CollectLive(ctx context.Context, req LiveRequest, deps LiveDeps, now time.Time) (*LiveResult, error) {
	dt := deps.Dynatrace
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("nenhuma aplicação selecionada")
	}
	if len(req.Items) > LiveMaxItems {
		return nil, fmt.Errorf("aplicações demais (%d): o máximo no tempo real é %d", len(req.Items), LiveMaxItems)
	}
	for _, it := range req.Items {
		if it.Cluster == "" || it.Namespace == "" || it.Workload == "" {
			return nil, fmt.Errorf("aplicação incompleta: cluster, namespace e workload são obrigatórios")
		}
	}
	w, warns, err := req.Window.Normalize(now)
	if err != nil {
		return nil, err
	}
	plan, err := PlanFor(w, now, nil)
	if err != nil {
		return nil, err
	}
	pcts, err := NormalizePercentiles(req.Percentiles)
	if err != nil {
		return nil, err
	}
	maxPts := req.MaxPoints
	if maxPts == 0 {
		maxPts = LiveDefaultPoints
	}
	if maxPts < LiveMinPoints {
		maxPts = LiveMinPoints
	} else if maxPts > LiveMaxPoints {
		maxPts = LiveMaxPoints
	}
	bucket := pointsPerBucket(w.Duration(), plan.DTRes, maxPts)
	res := &LiveResult{Window: w, Resolution: plan.DTResolution, StepSeconds: int64(plan.DTRes.Seconds()) * int64(bucket), Percentiles: pcts, Warnings: append(warns, plan.Notes...), Series: make([]LiveSeries, len(req.Items)), GeneratedAt: now}

	type resolved struct {
		svcs  []dynatrace.ServiceRef
		notes []string
	}
	got := make([]resolved, len(req.Items))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, 6)
	)

	// consumo (Prometheus): um conjunto de consultas por cluster, em paralelo com o Dynatrace; entra na
	// série no fim, para a série do Dynatrace (que substitui o elemento inteiro) não apagá-lo
	resources := make([]*LiveResources, len(req.Items))
	resNotes := make([][]string, len(req.Items))
	if req.Resources && deps.Prometheus != nil {
		byC := map[string][]int{}
		for i, it := range req.Items {
			byC[it.Cluster] = append(byC[it.Cluster], i)
		}
		for cluster, idxs := range byC {
			wg.Add(1)
			go func(cluster string, idxs []int) {
				defer wg.Done()
				fail := func(msg string) {
					for _, i := range idxs {
						resNotes[i] = append(resNotes[i], msg)
					}
				}
				q, err := deps.Prometheus(cluster)
				if err != nil {
					fail("Prometheus: " + err.Error())
					return
				}
				its := make([]Item, len(idxs))
				for k, i := range idxs {
					its[k] = req.Items[i]
				}
				rs, notes, err := CollectLiveResources(ctx, q, its, now)
				if err != nil {
					fail("Prometheus: " + err.Error())
					return
				}
				for k, i := range idxs {
					resources[i] = rs[k]
					for _, n := range notes {
						resNotes[i] = append(resNotes[i], "Prometheus "+n)
					}
				}
			}(cluster, idxs)
		}
	}

	for i, it := range req.Items {
		res.Series[i].Item = it
		wg.Add(1)
		go func(i int, it Item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			api, err := dt(it.Cluster)
			if err != nil {
				res.Series[i].Error = "Dynatrace: " + err.Error()
				return
			}
			svcs, notes, err := api.ResolveEntryServices(ctx, it.Cluster, it.Namespace, it.Workload)
			switch {
			case err != nil:
				res.Series[i].Error = "Dynatrace: " + err.Error()
			case len(svcs) == 0:
				res.Series[i].Error = fmt.Sprintf("Dynatrace: o process group de %s/%s não tem serviço de entrada (web/RPC)", it.Namespace, it.Workload)
			default:
				got[i] = resolved{svcs, notes}
			}
		}(i, it)
	}
	wg.Wait()

	// por cluster: cada métrica numa chamada só, com os serviços de todas as aplicações
	byCluster := map[string][]int{}
	for i, it := range req.Items {
		if len(got[i].svcs) > 0 {
			byCluster[it.Cluster] = append(byCluster[it.Cluster], i)
		}
	}
	for cluster, idxs := range byCluster {
		wg.Add(1)
		go func(cluster string, idxs []int) {
			defer wg.Done()
			api, err := dt(cluster)
			if err != nil {
				for _, i := range idxs {
					res.Series[i].Error = "Dynatrace: " + err.Error()
				}
				return
			}
			seen := map[string]bool{}
			var ids []string
			for _, i := range idxs {
				for _, s := range got[i].svcs {
					if !seen[s.ID] {
						seen[s.ID] = true
						ids = append(ids, s.ID)
					}
				}
			}
			type job struct {
				sel   string
				scale float64
			}
			jobs := map[string]job{"req": {selRequests, 1}, "4xx": {sel4xx, 1}, "5xx": {sel5xx, 1}}
			for _, p := range pcts {
				switch p {
				case 50:
					jobs["p50"] = job{selP50, 0.001}
				case 95:
					jobs["p95"] = job{selP95, 0.001}
				case 99:
					jobs["p99"] = job{selP99, 0.001}
				}
			}
			var (
				mu   sync.Mutex
				data = map[string]map[string]dynatrace.NullableSeries{}
				errs = map[string]error{}
				jw   sync.WaitGroup
			)
			for key, j := range jobs {
				jw.Add(1)
				go func(key string, j job) {
					defer jw.Done()
					m, err := api.QueryServiceSeries(ctx, j.sel, ids, w.Start, w.End, plan.DTResolution, j.scale)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						errs[key] = err
					} else {
						data[key] = m
					}
				}(key, j)
			}
			jw.Wait()

			for _, i := range idxs {
				if e := errs["req"]; e != nil {
					res.Series[i].Error = "Dynatrace: requisições: " + e.Error()
					continue
				}
				pick := func(key, id string) Series {
					if s, ok := data[key][id]; ok {
						return toSeries(s)
					}
					return emptySeries()
				}
				var st []ServiceTraffic
				for _, s := range got[i].svcs {
					st = append(st, ServiceTraffic{ID: s.ID, Name: s.Name, Type: s.Type,
						Requests: pick("req", s.ID), Err4xx: pick("4xx", s.ID), Err5xx: pick("5xx", s.ID),
						P50Ms: pick("p50", s.ID), P95Ms: pick("p95", s.ID), P99Ms: pick("p99", s.ID)})
				}
				ls := Downsample(BuildLive(st, plan.DTRes, DefaultP99Floor, now), bucket)
				ls.Item, ls.Resolution = req.Items[i], plan.DTResolution
				ls.Notes = append(ls.Notes, got[i].notes...)
				for key, e := range errs {
					ls.Notes = append(ls.Notes, fmt.Sprintf("%s indisponível: %v", key, e))
				}
				sort.Strings(ls.Notes)
				res.Series[i] = ls
			}
		}(cluster, idxs)
	}
	wg.Wait()
	for i := range res.Series {
		res.Series[i].Resources = resources[i]
		res.Series[i].Notes = append(res.Series[i].Notes, resNotes[i]...)
	}
	return res, nil
}
