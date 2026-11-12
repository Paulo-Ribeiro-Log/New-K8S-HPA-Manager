package report

import (
	"math"
	"sort"
)

// Series é uma série temporal; Values[i] == NaN significa "sem dado" naquele instante (nunca zero:
// zero é um valor real).
type Series struct {
	Times  []int64 // epoch em ms
	Values []float64
}

// ServiceTraffic são as séries de um serviço de ENTRADA do Dynatrace (WEB_REQUEST_SERVICE, WEB_SERVICE...).
type ServiceTraffic struct {
	ID, Name, Type string
	Requests       Series // contagem por intervalo (resolução do Plan)
	Err4xx         Series
	Err5xx         Series
	AvgMs          Series // média do tempo de resposta em ms (na resolução das requisições)
	P50Ms          Series // P50 em ms (já convertido de microssegundos)
	P95Ms          Series
	P99Ms          Series
}

// Traffic é o resultado agregado de uma aplicação. Ponteiro nulo = "sem dado".
type Traffic struct {
	Source   string   `json:"source"` // "dynatrace"
	Requests *float64 `json:"requests"`
	MaxRPS   *float64 `json:"max_rps"`
	// AvgMs é o tempo de resposta MÉDIO da janela, em ms: média das médias de cada serviço ponderada
	// pelas requisições de cada instante (não é pico nem média simples de pontos).
	AvgMs      *float64 `json:"avg_ms"`
	P50PeakMs  *float64 `json:"p50_peak_ms"`
	P95PeakMs  *float64 `json:"p95_peak_ms"`
	P99PeakMs  *float64 `json:"p99_peak_ms"`
	P99Service string   `json:"p99_service,omitempty"` // serviço que gerou o pico de P99
	Err4xx     *float64 `json:"err_4xx"`
	Err5xx     *float64 `json:"err_5xx"`
	Services   []string `json:"services"` // serviços de entrada usados
	PeakApprox bool     `json:"peak_approx"`
	Resolution string   `json:"resolution"`
	Notes      []string `json:"notes,omitempty"`
}

// DefaultP99Floor: só serviços com pelo menos 1% das requisições entram no pico de P99. Sem esse
// piso, um endpoint com 4 requisições (ex.: OpenApiWebMvcResource, P99 3,6 s) dominaria o pico.
const DefaultP99Floor = 0.01

func f(v float64) *float64 { return &v }

// sum soma os pontos válidos; ok=false quando não há nenhum.
func (s Series) sum() (float64, bool) {
	total, ok := 0.0, false
	for _, v := range s.Values {
		if !math.IsNaN(v) {
			total += v
			ok = true
		}
	}
	return total, ok
}

// max devolve o maior ponto válido; ok=false quando não há nenhum.
func (s Series) max() (float64, bool) {
	best, ok := math.Inf(-1), false
	for _, v := range s.Values {
		if !math.IsNaN(v) && v > best {
			best, ok = v, true
		}
	}
	return best, ok
}

// sumByTime soma séries instante a instante (ignora NaN); devolve os instantes em ordem.
func sumByTime(all []Series) (times []int64, sums []float64) {
	acc := map[int64]float64{}
	seen := map[int64]bool{}
	for _, s := range all {
		for i, t := range s.Times {
			if i >= len(s.Values) || math.IsNaN(s.Values[i]) {
				continue
			}
			acc[t] += s.Values[i]
			seen[t] = true
		}
	}
	for t := range seen {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	for _, t := range times {
		sums = append(sums, acc[t])
	}
	return
}

// weightedAvgMs é a média do tempo de resposta da janela: Σ(média × requisições) / Σ requisições, sobre os
// pares (serviço, instante) que têm os dois valores. Média simples dos pontos daria o mesmo peso a um
// minuto de madrugada e a um minuto de pico. Nulo quando não há par válido ou o peso é zero.
func weightedAvgMs(svcs []ServiceTraffic) *float64 {
	num, den := 0.0, 0.0
	for _, s := range svcs {
		req := s.Requests.byTime()
		for t, avg := range s.AvgMs.byTime() {
			if w, ok := req[t]; ok && w > 0 {
				num += avg * w
				den += w
			}
		}
	}
	if den <= 0 {
		return nil
	}
	return f(num / den)
}

// AggregateTraffic junta os serviços de entrada de uma aplicação:
//   - requisições, 4xx e 5xx: soma de todos os serviços (são pontos de entrada disjuntos);
//   - Max RPS: pico da série SOMADA dividido pelo intervalo (nunca soma de picos);
//   - P50, P95 e P99 de pico: máximo entre os serviços com fatia >= floor das requisições (o P99 mostra
//     qual serviço gerou o pico).
//
// resSec é a resolução do tráfego em segundos (converte contagem por intervalo em RPS).
func AggregateTraffic(svcs []ServiceTraffic, resSec float64, floor float64) Traffic {
	t := Traffic{Source: "dynatrace"}
	var reqSeries []Series
	for _, s := range svcs {
		t.Services = append(t.Services, s.Name)
		reqSeries = append(reqSeries, s.Requests)
	}
	sort.Strings(t.Services)

	sumOf := func(pick func(ServiceTraffic) Series) *float64 {
		total, any := 0.0, false
		for _, s := range svcs {
			if v, ok := pick(s).sum(); ok {
				total += v
				any = true
			}
		}
		if !any {
			return nil
		}
		return f(total)
	}
	t.Requests = sumOf(func(s ServiceTraffic) Series { return s.Requests })
	t.Err4xx = sumOf(func(s ServiceTraffic) Series { return s.Err4xx })
	t.Err5xx = sumOf(func(s ServiceTraffic) Series { return s.Err5xx })

	if _, sums := sumByTime(reqSeries); len(sums) > 0 && resSec > 0 {
		peak := 0.0
		for _, v := range sums {
			if v > peak {
				peak = v
			}
		}
		t.MaxRPS = f(peak / resSec)
	}

	t.AvgMs = weightedAvgMs(svcs)

	// P50, P95 e P99: pico da janela, só entre serviços com fatia relevante do tráfego
	if t.Requests != nil && *t.Requests > 0 {
		peak := func(pick func(ServiceTraffic) Series) (best float64, name string, found bool) {
			for _, s := range svcs {
				r, ok := s.Requests.sum()
				if !ok || r/(*t.Requests) < floor {
					continue
				}
				if m, ok := pick(s).max(); ok && (!found || m > best) {
					best, name, found = m, s.Name, true
				}
			}
			return
		}
		if v, _, ok := peak(func(s ServiceTraffic) Series { return s.P50Ms }); ok {
			t.P50PeakMs = f(v)
		}
		if v, _, ok := peak(func(s ServiceTraffic) Series { return s.P95Ms }); ok {
			t.P95PeakMs = f(v)
		}
		if v, name, ok := peak(func(s ServiceTraffic) Series { return s.P99Ms }); ok {
			t.P99PeakMs, t.P99Service = f(v), name
		}
	}
	return t
}
