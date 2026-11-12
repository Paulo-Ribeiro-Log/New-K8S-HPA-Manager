package report

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Item é uma aplicação do relatório: um Deployment num cluster e namespace.
type Item struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
}

// Thresholds definem os selos. Os padrões vêm do relatório de referência do usuário: P99 abaixo de
// 0,75 s sem selo, 1,769 s com 🔴 (limite em 1 s); qualquer 4xx é ⚠️; qualquer 5xx é 🔴.
type Thresholds struct {
	P99RedMs float64 `json:"p99_red_ms"` // P99 de pico acima disto: vermelho
	Err4xx   float64 `json:"err_4xx"`    // 4xx acima disto: aviso
	Err5xx   float64 `json:"err_5xx"`    // 5xx acima disto: vermelho
}

// DefaultThresholds: ver Thresholds.
var DefaultThresholds = Thresholds{P99RedMs: 1000, Err4xx: 0, Err5xx: 0}

// Flags são os selos de uma linha: "" (sem selo), "warn" ou "red".
type Flags struct {
	P99  string `json:"p99,omitempty"`
	Err4 string `json:"err_4xx,omitempty"`
	Err5 string `json:"err_5xx,omitempty"`
}

func (t Thresholds) flags(tr *Traffic) Flags {
	var f Flags
	if tr == nil {
		return f
	}
	if tr.P99PeakMs != nil && *tr.P99PeakMs > t.P99RedMs {
		f.P99 = "red"
	}
	if tr.Err4xx != nil && *tr.Err4xx > t.Err4xx {
		f.Err4 = "warn"
	}
	if tr.Err5xx != nil && *tr.Err5xx > t.Err5xx {
		f.Err5 = "red"
	}
	return f
}

// Row é uma linha do relatório. Cada fonte falha sozinha: Errors diz o que faltou e o resto aparece.
type Row struct {
	Item
	Traffic *Traffic     `json:"traffic"`
	Usage   *Usage       `json:"usage"`
	Node    *NodeContext `json:"node,omitempty"`
	Extras  *Extras      `json:"extras,omitempty"`
	Flags   Flags        `json:"flags"`
	Errors  []string     `json:"errors,omitempty"`
}

// Result é o relatório completo.
type Result struct {
	Window      Window    `json:"window"`
	Plan        Plan      `json:"plan"`
	Percentiles []int     `json:"percentiles"`  // os percentis coletados (50, 95 e/ou 99)
	NodeContext bool      `json:"node_context"` // o contexto de node foi coletado
	Extras      bool      `json:"extras"`       // réplicas, HPA, restarts e rede foram coletados
	Warnings    []string  `json:"warnings,omitempty"`
	Rows        []Row     `json:"rows"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Request é o pedido de um relatório.
type Request struct {
	Items      []Item
	Window     Window
	Thresholds *Thresholds
	// Percentiles de latência a coletar (50, 95 e/ou 99); vazio = só P99. O que não for escolhido não é
	// consultado nem aparece no relatório.
	Percentiles []int
	// NodeContext pede o contexto de node (pool, uso dos nodes, throttling); opt-in, custa consultas a mais.
	NodeContext bool
	// Extras pede réplicas, teto do HPA, restarts e rede (só Prometheus); opt-in, custa consultas a mais.
	Extras bool
}

// Deps são as fontes de dados (injetadas: nos testes, falsas).
type Deps struct {
	Traffic func(ctx context.Context, it Item, w Window, plan Plan) (*Traffic, error)
	Usage   func(ctx context.Context, it Item, w Window, plan Plan) (*Usage, error)
	// Node só roda quando o pedido liga o contexto de node.
	Node func(ctx context.Context, it Item, w Window, plan Plan) (*NodeContext, error)
	// Extras só roda quando o pedido liga os extras.
	Extras func(ctx context.Context, it Item, w Window, plan Plan) (*Extras, error)
}

// Collector roda o relatório.
type Collector struct {
	Deps        Deps
	Concurrency int           // itens ao mesmo tempo (padrão 4)
	ItemTimeout time.Duration // por item (padrão 90 s)
	Now         func() time.Time
}

// MaxItems limita o tamanho de um relatório.
const MaxItems = 200

func (r Request) validate() error {
	if len(r.Items) == 0 {
		return fmt.Errorf("escolha ao menos uma aplicação")
	}
	if len(r.Items) > MaxItems {
		return fmt.Errorf("aplicações demais (%d): o máximo é %d por relatório", len(r.Items), MaxItems)
	}
	for i, it := range r.Items {
		if strings.TrimSpace(it.Cluster) == "" || strings.TrimSpace(it.Namespace) == "" || strings.TrimSpace(it.Workload) == "" {
			return fmt.Errorf("a aplicação %d está incompleta: informe cluster, namespace e deployment", i+1)
		}
	}
	return nil
}

// Prepared é o pedido já validado: janela normalizada, resolução escolhida e limites dos selos.
type Prepared struct {
	Window      Window
	Warnings    []string
	Plan        Plan
	Thresholds  Thresholds
	Percentiles []int
	NodeContext bool
	Extras      bool
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// PrepareWindow valida só a janela e escolhe a resolução (a tela usa para mostrar o plano antes de
// coletar).
func (c *Collector) PrepareWindow(w Window) (Window, []string, Plan, error) {
	nw, warns, err := w.Normalize(c.now())
	if err != nil {
		return Window{}, nil, Plan{}, err
	}
	plan, err := PlanFor(nw, c.now(), nil)
	if err != nil {
		return Window{}, nil, Plan{}, err
	}
	// CPU, memória e contexto de node vêm do Prometheus, que guarda ~14 dias (o tráfego vem do Dynatrace,
	// com retenção maior): avisa antes de coletar, em vez de só descobrir "sem dado" depois.
	if limit := c.now().Add(-PrometheusRetention); nw.Start.Before(limit) {
		if nw.End.Before(limit) {
			warns = append(warns, fmt.Sprintf("a janela é mais antiga que a retenção do Prometheus (cerca de %d dias): CPU, memória e contexto de node ficam \"sem dado\"; o tráfego (Dynatrace) continua", int(PrometheusRetention.Hours()/24)))
		} else {
			warns = append(warns, fmt.Sprintf("o Prometheus guarda cerca de %d dias: CPU, memória e contexto de node só têm dado a partir de %s", int(PrometheusRetention.Hours()/24), limit.UTC().Format("02/01/2006 15:04 UTC")))
		}
	}
	return nw, warns, plan, nil
}

// Prepare valida o pedido (itens, janela, retenção) sem fazer nenhuma consulta.
func (c *Collector) Prepare(req Request) (Prepared, error) {
	if err := req.validate(); err != nil {
		return Prepared{}, err
	}
	w, warns, plan, err := c.PrepareWindow(req.Window)
	if err != nil {
		return Prepared{}, err
	}
	th := DefaultThresholds
	if req.Thresholds != nil {
		th = *req.Thresholds
	}
	pcts, err := NormalizePercentiles(req.Percentiles)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Window: w, Warnings: warns, Plan: plan, Thresholds: th, Percentiles: pcts, NodeContext: req.NodeContext, Extras: req.Extras}, nil
}

// Run coleta o relatório. onProgress (opcional) é chamado a cada item concluído. Itens com falha
// não derrubam o relatório; o contexto cancelado interrompe os que faltam.
func (c *Collector) Run(ctx context.Context, req Request, onProgress func(done, total int)) (*Result, error) {
	pre, err := c.Prepare(req)
	if err != nil {
		return nil, err
	}
	w, plan, th, warns := pre.Window, pre.Plan, pre.Thresholds, pre.Warnings
	conc := c.Concurrency
	if conc <= 0 {
		conc = 4
	}
	timeout := c.ItemTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}

	rows := make([]Row, len(req.Items))
	var (
		mu   sync.Mutex
		done int
		wg   sync.WaitGroup
		sem  = make(chan struct{}, conc)
	)
	for i, it := range req.Items {
		wg.Add(1)
		go func(i int, it Item) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				rows[i] = Row{Item: it, Errors: []string{"cancelado"}}
				return
			}
			rows[i] = c.collectItem(ctx, it, w, plan, th, timeout, pre.NodeContext, pre.Extras)
			mu.Lock()
			done++
			d := done
			mu.Unlock()
			if onProgress != nil {
				onProgress(d, len(req.Items))
			}
		}(i, it)
	}
	wg.Wait()
	return &Result{Window: w, Plan: plan, Warnings: warns, Rows: rows, Percentiles: pre.Percentiles, NodeContext: pre.NodeContext, Extras: pre.Extras, GeneratedAt: c.now().UTC()}, nil
}

func (c *Collector) collectItem(ctx context.Context, it Item, w Window, plan Plan, th Thresholds, timeout time.Duration, nodeOn, extrasOn bool) Row {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	row := Row{Item: it}
	var wg sync.WaitGroup
	var mu sync.Mutex
	addErr := func(e error) {
		mu.Lock()
		row.Errors = append(row.Errors, e.Error())
		mu.Unlock()
	}
	if c.Deps.Traffic != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tr, err := c.Deps.Traffic(ctx, it, w, plan); err != nil {
				addErr(err)
			} else {
				row.Traffic = tr
			}
		}()
	}
	if c.Deps.Usage != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if u, err := c.Deps.Usage(ctx, it, w, plan); err != nil {
				addErr(fmt.Errorf("Prometheus: %w", err))
			} else {
				row.Usage = u
			}
		}()
	}
	if nodeOn && c.Deps.Node != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if n, err := c.Deps.Node(ctx, it, w, plan); err != nil {
				addErr(fmt.Errorf("Prometheus (node): %w", err))
			} else {
				row.Node = n
			}
		}()
	}
	if extrasOn && c.Deps.Extras != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if x, err := c.Deps.Extras(ctx, it, w, plan); err != nil {
				addErr(fmt.Errorf("Prometheus (extras): %w", err))
			} else {
				row.Extras = x
			}
		}()
	}
	wg.Wait()
	row.Flags = th.flags(row.Traffic)
	return row
}
