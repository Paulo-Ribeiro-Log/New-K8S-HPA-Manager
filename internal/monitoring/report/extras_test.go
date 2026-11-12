package report

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

// seriesFake responde cada consulta (pelo trecho) com uma série de valores dados.
type seriesFake struct {
	mu      sync.Mutex
	queries []string
	data    map[string][]string
	failAll bool
}

func (f *seriesFake) QueryRange(_ context.Context, q string, _, _ time.Time, _ time.Duration) (*promclient.QueryRangeResult, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	if f.failAll {
		return nil, context.DeadlineExceeded
	}
	r := &promclient.QueryRangeResult{Status: "success"}
	for frag, vs := range f.data {
		if !strings.Contains(q, frag) {
			continue
		}
		var vals [][]interface{}
		for i, v := range vs {
			vals = append(vals, []interface{}{float64(i + 1), v})
		}
		r.Data.Result = append(r.Data.Result, struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		}{Metric: map[string]string{}, Values: vals})
	}
	return r, nil
}

func extrasFake() *seriesFake {
	return &seriesFake{data: map[string][]string{
		"kube_deployment_status_replicas_available":       {"10", "30", "20"},
		"kube_deployment_spec_replicas":                   {"10", "40", "40"},
		"kube_horizontalpodautoscaler_spec_max_replicas":  {"40", "40", "40"},
		"kube_horizontalpodautoscaler_spec_min_replicas":  {"10", "10", "10"},
		"kube_pod_container_status_restarts_total":        {"0", "2", "1"},
		"container_network_receive_bytes_total":           {"1000", "3000"},
		"container_network_transmit_bytes_total":          {"500", "700"},
		"container_network_receive_packets_dropped_total": {"0", "2"},
	}}
}

func TestExtrasResumoDaJanela(t *testing.T) {
	w := Window{Start: time.Now().Add(-time.Hour), End: time.Now()}
	x, err := CollectExtras(context.Background(), extrasFake(), "calc", "frete-hub", w, Plan{PromStep: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if x.Replicas == nil || x.Replicas.Max != 30 || x.Replicas.Min != 10 || x.Replicas.Avg != 20 {
		t.Errorf("réplicas: %+v", x.Replicas)
	}
	if x.Desired == nil || x.Desired.Max != 40 {
		t.Errorf("desejadas: %+v", x.Desired)
	}
	if x.HPAMax == nil || *x.HPAMax != 40 || x.HPAMin == nil || *x.HPAMin != 10 {
		t.Errorf("HPA: %v %v", x.HPAMin, x.HPAMax)
	}
	if x.Restarts == nil || *x.Restarts != 3 {
		t.Errorf("restarts = soma dos incrementos (0+2+1): %v", x.Restarts)
	}
	if x.NetRx == nil || x.NetRx.Max != 3000 || x.NetRx.Avg != 2000 || x.NetTx == nil || x.NetTx.Max != 700 || x.NetErr == nil || x.NetErr.Max != 2 {
		t.Errorf("rede: rx=%+v tx=%+v err=%+v", x.NetRx, x.NetTx, x.NetErr)
	}
}

func TestExtrasQueriesSaoDaAplicacaoENaoSomamSerieDuplicada(t *testing.T) {
	f := extrasFake()
	if _, err := CollectExtras(context.Background(), f, "calc", "frete-hub", Window{Start: time.Now().Add(-time.Hour), End: time.Now()}, Plan{PromStep: 5 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 8 {
		t.Errorf("%d consultas, esperado 8", len(f.queries))
	}
	for _, q := range f.queries {
		if !strings.Contains(q, `namespace="calc"`) {
			t.Errorf("toda consulta é do namespace da aplicação: %s", q)
		}
		if strings.Contains(q, "restarts_total") && (!strings.Contains(q, "[5m]") || !strings.Contains(q, "max by (pod,container)")) {
			t.Errorf("restarts: janela do increase = passo e sem série duplicada: %s", q)
		}
	}
}

func TestExtrasSemDadoFicaNuloEFalhaTotalDaErro(t *testing.T) {
	w := Window{Start: time.Now().Add(-time.Hour), End: time.Now()}
	x, err := CollectExtras(context.Background(), &seriesFake{data: map[string][]string{}}, "calc", "x", w, Plan{PromStep: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if x.Replicas != nil || x.HPAMax != nil || x.Restarts != nil || x.NetRx != nil {
		t.Errorf("sem dado tem de ficar nulo, nunca zero: %+v", x)
	}
	if len(x.Notes) == 0 {
		t.Error("deveria explicar a falta de dado")
	}
	if _, err := CollectExtras(context.Background(), &seriesFake{failAll: true}, "calc", "x", w, Plan{PromStep: time.Minute}); err == nil {
		t.Error("todas as consultas falhando deve dar erro")
	}
	if _, err := CollectExtras(context.Background(), extrasFake(), "calc", "a;b", w, Plan{PromStep: time.Minute}); err == nil {
		t.Error("nome inválido deveria falhar")
	}
}

func TestExtrasSoRodaQuandoOPedidoLiga(t *testing.T) {
	called := 0
	var mu sync.Mutex
	c := &Collector{Deps: Deps{Extras: func(context.Context, Item, Window, Plan) (*Extras, error) {
		mu.Lock()
		called++
		mu.Unlock()
		return &Extras{Source: "prometheus"}, nil
	}}}
	now := time.Now()
	req := Request{Items: []Item{{"c", "n", "w"}}, Window: Window{Start: now.Add(-time.Hour), End: now}}
	res, err := c.Run(context.Background(), req, nil)
	if err != nil || called != 0 || res.Extras || res.Rows[0].Extras != nil {
		t.Fatalf("desligado: err=%v chamadas=%d extras=%v", err, called, res.Rows[0].Extras)
	}
	req.Extras = true
	res, err = c.Run(context.Background(), req, nil)
	if err != nil || called != 1 || !res.Extras || res.Rows[0].Extras == nil {
		t.Fatalf("ligado: err=%v chamadas=%d", err, called)
	}
}
