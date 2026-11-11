package report

import (
	"math"
	"testing"
)

func serie(vals ...float64) Series {
	s := Series{Values: vals}
	for i := range vals {
		s.Times = append(s.Times, int64(i)*60000)
	}
	return s
}

var nan = math.NaN()

// Os totais por serviço vêm de uma medição real (frete-hub, janela 10/11 10:22 → 11/11 02:00): a
// série abaixo reproduz as proporções reais (FreteController concentra o tráfego).
func fretehub() []ServiceTraffic {
	return []ServiceTraffic{
		{Name: "FreteController", Type: "WEB_SERVICE",
			Requests: serie(60000, 120000, 15960), Err4xx: serie(6000, 12000, 1590), Err5xx: serie(0, 40, 9),
			P50Ms: serie(20, 45, 18), P95Ms: serie(200, 820, 150), P99Ms: serie(300, 1495.46, 200)},
		{Name: "CotacaoController", Type: "WEB_SERVICE",
			Requests: serie(1000, 2000, 1160), Err4xx: serie(50, 100, 69), Err5xx: serie(0, 0, 0),
			P50Ms: serie(10, 30, 9), P95Ms: serie(80, 150, 70), P99Ms: serie(100, 211.96, 90)},
		{Name: "frete-hub-*", Type: "WEB_REQUEST_SERVICE",
			Requests: serie(800, 1000, 556), Err4xx: serie(799, 1000, 555), Err5xx: serie(0, 0, 0),
			P50Ms: serie(2, 3, 1), P95Ms: serie(15, 25, 8), P99Ms: serie(20, 30.4, 10)},
		{Name: "OpenApiWebMvcResource", Type: "WEB_SERVICE",
			Requests: serie(1, nan, nan), Err4xx: serie(0, nan, nan), Err5xx: serie(0, nan, nan),
			P50Ms: serie(2000, nan, nan), P95Ms: serie(2900, nan, nan), P99Ms: serie(3038.1, nan, nan)}, // 1 requisição: percentis de 3 s são ruído
	}
}

func TestAggregateTrafficSomaPontosDeEntrada(t *testing.T) {
	tr := AggregateTraffic(fretehub(), 60, DefaultP99Floor)
	if tr.Requests == nil || *tr.Requests != 60000+120000+15960+1000+2000+1160+800+1000+556+1 {
		t.Errorf("requisições: %v", tr.Requests)
	}
	if *tr.Err4xx != 6000+12000+1590+50+100+69+799+1000+555 || *tr.Err5xx != 49 {
		t.Errorf("erros: 4xx=%v 5xx=%v", *tr.Err4xx, *tr.Err5xx)
	}
	if len(tr.Services) != 4 || tr.Source != "dynatrace" {
		t.Errorf("serviços/fonte: %+v", tr.Services)
	}
}

func TestAggregateTrafficPicoDeRPSEDaSerieSomada(t *testing.T) {
	// picos que NÃO coincidem: a soma dos picos individuais (600+600 = 20 RPS) estaria errada; o pico
	// da série somada é 10 RPS
	a := ServiceTraffic{Name: "a", Requests: Series{Times: []int64{0, 60000}, Values: []float64{600, 0}}}
	b := ServiceTraffic{Name: "b", Requests: Series{Times: []int64{0, 60000}, Values: []float64{0, 600}}}
	tr := AggregateTraffic([]ServiceTraffic{a, b}, 60, DefaultP99Floor)
	if tr.MaxRPS == nil || *tr.MaxRPS != 10 { // 600/60 = 10; a soma de picos (1200/60=20) estaria errada
		t.Errorf("pico da série somada deveria ser 10 RPS, veio %v", tr.MaxRPS)
	}
	// real: pico no 2º minuto = 120000+2000+1000 = 123000/60 = 2050
	tr = AggregateTraffic(fretehub(), 60, DefaultP99Floor)
	if math.Abs(*tr.MaxRPS-123000.0/60) > 1e-9 {
		t.Errorf("max RPS real: %v", *tr.MaxRPS)
	}
}

func TestAggregateTrafficP99IgnoraServicoComPoucoTrafego(t *testing.T) {
	tr := AggregateTraffic(fretehub(), 60, DefaultP99Floor)
	// sem o piso, o pico seria 3038 ms (OpenApiWebMvcResource, 1 requisição)
	if tr.P99PeakMs == nil || *tr.P99PeakMs != 1495.46 || tr.P99Service != "FreteController" {
		t.Errorf("P99 pico: %v por %q", tr.P99PeakMs, tr.P99Service)
	}
	// com piso zero, o ruído volta (prova de que o piso é o que protege)
	tr0 := AggregateTraffic(fretehub(), 60, 0)
	if *tr0.P99PeakMs != 3038.1 {
		t.Errorf("sem piso esperava 3038.1, veio %v", *tr0.P99PeakMs)
	}
}

func TestAggregateTrafficP50P95IgnoramServicoComPoucoTrafego(t *testing.T) {
	tr := AggregateTraffic(fretehub(), 60, DefaultP99Floor)
	// pico entre os serviços relevantes: FreteController manda em todos (45, 820, 1495.46)
	if tr.P50PeakMs == nil || *tr.P50PeakMs != 45 || tr.P95PeakMs == nil || *tr.P95PeakMs != 820 {
		t.Errorf("P50/P95 de pico: %v %v", tr.P50PeakMs, tr.P95PeakMs)
	}
	// sem o piso, o ruído do serviço de 1 requisição domina também P50 e P95
	tr0 := AggregateTraffic(fretehub(), 60, 0)
	if *tr0.P50PeakMs != 2000 || *tr0.P95PeakMs != 2900 {
		t.Errorf("sem piso: %v %v", *tr0.P50PeakMs, *tr0.P95PeakMs)
	}
	// a ordem dos percentis se mantém: P50 <= P95 <= P99
	if !(*tr.P50PeakMs <= *tr.P95PeakMs && *tr.P95PeakMs <= *tr.P99PeakMs) {
		t.Errorf("P50 <= P95 <= P99 violado: %v %v %v", *tr.P50PeakMs, *tr.P95PeakMs, *tr.P99PeakMs)
	}
}

func TestAggregateTrafficSemDadoNaoViraZero(t *testing.T) {
	vazio := ServiceTraffic{Name: "x", Requests: serie(nan, nan), Err4xx: serie(nan), Err5xx: serie(nan), P99Ms: serie(nan)}
	tr := AggregateTraffic([]ServiceTraffic{vazio}, 60, DefaultP99Floor)
	if tr.Requests != nil || tr.MaxRPS != nil || tr.P99PeakMs != nil || tr.P50PeakMs != nil || tr.P95PeakMs != nil || tr.Err4xx != nil || tr.Err5xx != nil {
		t.Errorf("sem dado deve ser nulo, nunca zero: %+v", tr)
	}
	// zero real é diferente de sem dado
	zero := ServiceTraffic{Name: "y", Requests: serie(0, 0), Err4xx: serie(0, 0), Err5xx: serie(0, 0), P99Ms: serie(0, 0)}
	tr = AggregateTraffic([]ServiceTraffic{zero}, 60, DefaultP99Floor)
	if tr.Requests == nil || *tr.Requests != 0 || tr.MaxRPS == nil || *tr.MaxRPS != 0 {
		t.Errorf("zero real deve aparecer como 0: %+v", tr)
	}
	if tr.P99PeakMs != nil || tr.P50PeakMs != nil || tr.P95PeakMs != nil {
		t.Errorf("sem tráfego não há percentil de pico: %v %v %v", tr.P50PeakMs, tr.P95PeakMs, tr.P99PeakMs)
	}
}
