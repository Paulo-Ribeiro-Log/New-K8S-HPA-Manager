package report

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func p(v float64) *float64 { return &v }

func fixedNow() time.Time { return now }

func TestFlagsPeloExemploDoUsuario(t *testing.T) {
	th := DefaultThresholds
	casos := []struct {
		nome              string
		p99ms, e4, e5     float64
		p99Fl, e4Fl, e5Fl string
	}{
		{"frete-hub: 1.769s, 78883 4xx, 0 5xx", 1769, 78883, 0, "red", "warn", ""},
		{"vv-estoque: 0.241s, 56750 4xx", 241, 56750, 0, "", "warn", ""},
		{"estoque-gestao: 0.026s sem erros", 26, 0, 0, "", "", ""},
		{"mp-inventario: 0.750s, 36 4xx", 750, 36, 0, "", "warn", ""},
		{"entrega-envias: 4.534s, 95905 4xx, 1 5xx", 4534, 95905, 1, "red", "warn", "red"},
	}
	for _, c := range casos {
		f := th.flags(&Traffic{P99PeakMs: p(c.p99ms), Err4xx: p(c.e4), Err5xx: p(c.e5)})
		if f.P99 != c.p99Fl || f.Err4 != c.e4Fl || f.Err5 != c.e5Fl {
			t.Errorf("%s: %+v", c.nome, f)
		}
	}
	if f := th.flags(&Traffic{}); f != (Flags{}) { // sem dado não ganha selo
		t.Errorf("sem dado: %+v", f)
	}
	if f := th.flags(nil); f != (Flags{}) {
		t.Errorf("sem tráfego: %+v", f)
	}
}

func TestRunItensEmParaleloComFalhasIsoladas(t *testing.T) {
	var mu sync.Mutex
	var progress []int
	deps := Deps{
		Traffic: func(_ context.Context, it Item, _ Window, _ Plan) (*Traffic, error) {
			switch it.Workload {
			case "sem-dt":
				return nil, errors.New("Dynatrace: nenhum process group")
			case "lento":
				time.Sleep(20 * time.Millisecond)
			}
			return &Traffic{Source: "dynatrace", Requests: p(100), P99PeakMs: p(2000), Err4xx: p(3), Err5xx: p(0)}, nil
		},
		Usage: func(_ context.Context, it Item, _ Window, _ Plan) (*Usage, error) {
			if it.Workload == "sem-prom" {
				return nil, errors.New("todas as consultas falharam")
			}
			return &Usage{Source: "prometheus", CPUReq: &Pct{Max: 9, Min: 1, Avg: 5}}, nil
		},
	}
	c := &Collector{Deps: deps, Concurrency: 2, Now: fixedNow}
	req := Request{
		Items: []Item{
			{"c", "ns", "ok"}, {"c", "ns", "sem-dt"}, {"c", "ns", "sem-prom"}, {"c", "ns", "lento"},
		},
		Window: Window{Start: now.Add(-6 * time.Hour), End: now.Add(-time.Hour)},
	}
	res, err := c.Run(context.Background(), req, func(d, total int) {
		mu.Lock()
		progress = append(progress, d)
		mu.Unlock()
		if total != 4 {
			t.Errorf("total do progresso: %d", total)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// a ordem das linhas é a do pedido, mesmo rodando em paralelo
	for i, w := range []string{"ok", "sem-dt", "sem-prom", "lento"} {
		if res.Rows[i].Workload != w {
			t.Fatalf("ordem: linha %d é %s, esperava %s", i, res.Rows[i].Workload, w)
		}
	}
	if r := res.Rows[0]; r.Traffic == nil || r.Usage == nil || len(r.Errors) != 0 || r.Flags.P99 != "red" || r.Flags.Err4 != "warn" {
		t.Errorf("linha ok: %+v", r)
	}
	if r := res.Rows[1]; r.Traffic != nil || r.Usage == nil || len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "Dynatrace") {
		t.Errorf("sem Dynatrace: o consumo deve aparecer mesmo assim: %+v", r)
	}
	if r := res.Rows[2]; r.Traffic == nil || r.Usage != nil || !strings.HasPrefix(r.Errors[0], "Prometheus:") {
		t.Errorf("sem Prometheus: %+v", r)
	}
	if len(progress) != 4 || progress[3] != 4 {
		t.Errorf("progresso: %v", progress)
	}
	if res.Plan.DTResolution != "1m" || res.Window.Duration() != 5*time.Hour {
		t.Errorf("janela/plano: %+v %+v", res.Window, res.Plan)
	}
}

func TestRunValidaPedido(t *testing.T) {
	c := &Collector{Now: fixedNow}
	w := Window{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour)}
	casos := map[string]Request{
		"sem itens":        {Window: w},
		"item incompleto":  {Items: []Item{{"c", "", "app"}}, Window: w},
		"janela inválida":  {Items: []Item{{"c", "ns", "app"}}, Window: Window{Start: now, End: now.Add(-time.Hour)}},
		"além da retenção": {Items: []Item{{"c", "ns", "app"}}, Window: Window{Start: now.Add(-500 * 24 * time.Hour), End: now.Add(-499 * 24 * time.Hour)}},
	}
	for nome, r := range casos {
		if _, err := c.Run(context.Background(), r, nil); err == nil {
			t.Errorf("%s: esperava erro", nome)
		}
	}
	many := Request{Window: w}
	for i := 0; i <= MaxItems; i++ {
		many.Items = append(many.Items, Item{"c", "ns", "app"})
	}
	if _, err := c.Run(context.Background(), many, nil); err == nil {
		t.Errorf("itens demais: esperava erro")
	}
}

func TestRunCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Collector{Concurrency: 1, Now: fixedNow, Deps: Deps{Traffic: func(ctx context.Context, _ Item, _ Window, _ Plan) (*Traffic, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	res, err := c.Run(ctx, Request{Items: []Item{{"c", "ns", "a"}, {"c", "ns", "b"}}, Window: Window{Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour)}}, nil)
	if err != nil || len(res.Rows) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	for _, r := range res.Rows {
		if len(r.Errors) == 0 {
			t.Errorf("cancelado deve registrar erro: %+v", r)
		}
	}
}

func TestPrepareWindowAvisaRetencaoDoPrometheus(t *testing.T) {
	c := &Collector{Now: fixedNow}
	dia := 24 * time.Hour
	aviso := func(start, end time.Duration) []string {
		_, warns, _, err := c.PrepareWindow(Window{Start: now.Add(-start), End: now.Add(-end)})
		if err != nil {
			t.Fatalf("janela %v..%v: %v", start, end, err)
		}
		return warns
	}
	// dentro dos 14 dias: sem aviso
	if w := aviso(6*time.Hour, time.Hour); len(w) != 0 {
		t.Errorf("janela recente: %v", w)
	}
	if w := aviso(13*dia, 12*dia); len(w) != 0 {
		t.Errorf("13 dias atrás ainda tem dado: %v", w)
	}
	// começa antes e termina depois do limite: parte dos dados existe
	w := aviso(20*dia, time.Hour)
	if len(w) != 1 || !strings.Contains(w[0], "só têm dado a partir de") || !strings.Contains(w[0], "14 dias") {
		t.Errorf("janela que atravessa o limite: %v", w)
	}
	// toda antes do limite: nada de CPU, memória e node, mas o tráfego continua
	w = aviso(30*dia, 29*dia)
	if len(w) != 1 || !strings.Contains(w[0], "mais antiga que a retenção do Prometheus") || !strings.Contains(w[0], "o tráfego (Dynatrace) continua") {
		t.Errorf("janela antiga: %v", w)
	}
	// o aviso não impede a coleta (só informa): a janela de 30 dias atrás é válida
	if _, _, _, err := c.PrepareWindow(Window{Start: now.Add(-30 * dia), End: now.Add(-29 * dia)}); err != nil {
		t.Errorf("janela antiga continua válida: %v", err)
	}
}

func TestRunContextoDeNodeSoQuandoPedido(t *testing.T) {
	var mu sync.Mutex
	nodeCalls := 0
	deps := Deps{
		Node: func(_ context.Context, it Item, _ Window, _ Plan) (*NodeContext, error) {
			mu.Lock()
			nodeCalls++
			mu.Unlock()
			if it.Workload == "sem-node" {
				return nil, errors.New("falha de node")
			}
			return &NodeContext{Source: "prometheus", Nodes: 3}, nil
		},
		Usage: func(_ context.Context, _ Item, _ Window, _ Plan) (*Usage, error) {
			return &Usage{Source: "prometheus"}, nil
		},
	}
	c := &Collector{Deps: deps, Now: fixedNow}
	req := Request{
		Items:  []Item{{"c", "ns", "ok"}, {"c", "ns", "sem-node"}},
		Window: Window{Start: now.Add(-6 * time.Hour), End: now.Add(-time.Hour)},
	}

	// sem pedir: nenhuma consulta de node e o resultado diz isso
	res, err := c.Run(context.Background(), req, nil)
	if err != nil || nodeCalls != 0 || res.NodeContext || res.Rows[0].Node != nil {
		t.Fatalf("sem node_context: chamadas=%d resultado=%+v err=%v", nodeCalls, res, err)
	}

	// pedindo: roda por aplicação; falha de uma não derruba a linha nem as outras
	req.NodeContext = true
	res, err = c.Run(context.Background(), req, nil)
	if err != nil || nodeCalls != 2 || !res.NodeContext {
		t.Fatalf("com node_context: chamadas=%d err=%v", nodeCalls, err)
	}
	if r := res.Rows[0]; r.Node == nil || r.Node.Nodes != 3 || r.Usage == nil || len(r.Errors) != 0 {
		t.Errorf("linha ok: %+v", r)
	}
	if r := res.Rows[1]; r.Node != nil || r.Usage == nil || len(r.Errors) != 1 || !strings.HasPrefix(r.Errors[0], "Prometheus (node):") {
		t.Errorf("node falhou, o resto fica: %+v", r)
	}
}
