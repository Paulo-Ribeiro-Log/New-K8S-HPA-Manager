package report

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

// RangeQuerier é o que o relatório precisa do Prometheus (o cliente real satisfaz esta interface).
type RangeQuerier interface {
	QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*promclient.QueryRangeResult, error)
}

// rangeResult é o resultado de uma consulta de intervalo do Prometheus.
type rangeResult = promclient.QueryRangeResult

// queryRange executa uma consulta de intervalo na janela.
func queryRange(ctx context.Context, q RangeQuerier, query string, w Window, step time.Duration) (*rangeResult, error) {
	return q.QueryRange(ctx, query, w.Start, w.End, step)
}

// Pct são estatísticas de uma série de percentual na janela.
type Pct struct {
	Max float64 `json:"max"`
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
}

// Usage é o consumo de uma aplicação: CPU e memória em % do request e em % do limit. Ponteiro nulo =
// sem dado (ex.: container sem limit definido não gera série de % do limit).
type Usage struct {
	Source string   `json:"source"` // "prometheus"
	CPUReq *Pct     `json:"cpu_req_pct"`
	CPULim *Pct     `json:"cpu_lim_pct"`
	MemReq *Pct     `json:"mem_req_pct"`
	MemLim *Pct     `json:"mem_lim_pct"`
	Step   string   `json:"step"`
	Notes  []string `json:"notes,omitempty"`
}

var k8sNameStrict = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// podSelector casa só os pods do Deployment ("<deploy>-<hash do ReplicaSet>-<sufixo>"). O seletor
// "<deploy>-.*" usado em outras telas casaria também Deployments de nome parecido (frete-hub e
// frete-hub-regionalizado).
func podSelector(workload string) string {
	return fmt.Sprintf(`pod=~"%s-[a-z0-9]{5,10}-[a-z0-9]{5}"`, strings.ReplaceAll(workload, ".", `\\.`))
}

// usageQueries monta as 4 consultas (média, entre os containers dos pods, de uso/request e uso/limit).
// rateWindow é a janela do rate() de CPU. Função pura (testável sem rede).
//
// Os DOIS lados da divisão passam por `max by (pod,container)`: o kube-state-metrics pode estar
// duplicado (duas instâncias raspadas, achado num cluster real: "found duplicate series for the match
// group ... on the right hand-side" -> HTTP 422) e o cAdvisor também pode repetir série. Com o max,
// cada (pod,container) vira uma série só e a divisão é um-para-um.
func usageQueries(namespace, workload string, rateWindow time.Duration) map[string]string {
	sel := fmt.Sprintf(`namespace="%s",%s`, namespace, podSelector(workload))
	cpu := fmt.Sprintf(`max by (pod,container) (rate(container_cpu_usage_seconds_total{%s,container!="",container!="POD"}[%s]))`, sel, promDur(rateWindow))
	mem := fmt.Sprintf(`max by (pod,container) (container_memory_working_set_bytes{%s,container!="",container!="POD"})`, sel)
	ratio := func(usage, metric, resource string) string {
		return fmt.Sprintf(`avg(%s / on(pod,container) max by (pod,container) (%s{%s,resource="%s"})) * 100`, usage, metric, sel, resource)
	}
	return map[string]string{
		"cpu_req": ratio(cpu, "kube_pod_container_resource_requests", "cpu"),
		"cpu_lim": ratio(cpu, "kube_pod_container_resource_limits", "cpu"),
		"mem_req": ratio(mem, "kube_pod_container_resource_requests", "memory"),
		"mem_lim": ratio(mem, "kube_pod_container_resource_limits", "memory"),
	}
}

func promDur(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// pctStats lê a 1ª série do resultado e calcula máx/mín/média dos pontos válidos.
func pctStats(r *promclient.QueryRangeResult) *Pct {
	if r == nil || len(r.Data.Result) == 0 {
		return nil
	}
	var p Pct
	n := 0
	for _, pt := range r.Data.Result[0].Values {
		if len(pt) < 2 {
			continue
		}
		s, ok := pt[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if n == 0 || v > p.Max {
			p.Max = v
		}
		if n == 0 || v < p.Min {
			p.Min = v
		}
		p.Avg += v
		n++
	}
	if n == 0 {
		return nil
	}
	p.Avg /= float64(n)
	return &p
}

// CollectUsage executa as 4 consultas em paralelo. Falha isolada deixa só aquele campo sem dado
// (e anota o motivo); erro só se TODAS falharem.
func CollectUsage(ctx context.Context, q RangeQuerier, namespace, workload string, w Window, plan Plan) (*Usage, error) {
	if !k8sNameStrict.MatchString(namespace) || !k8sNameStrict.MatchString(workload) {
		return nil, fmt.Errorf("namespace ou deployment com nome inválido")
	}
	rateWindow := 5 * time.Minute
	if plan.PromStep > rateWindow {
		rateWindow = plan.PromStep
	}
	queries := usageQueries(namespace, workload, rateWindow)

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		out    = &Usage{Source: "prometheus", Step: durLabel(plan.PromStep)}
		failed int
	)
	for key, query := range queries {
		wg.Add(1)
		go func(key, query string) {
			defer wg.Done()
			res, err := q.QueryRange(ctx, query, w.Start, w.End, plan.PromStep)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				out.Notes = append(out.Notes, fmt.Sprintf("%s: %v", key, err))
				return
			}
			st := pctStats(res)
			switch key {
			case "cpu_req":
				out.CPUReq = st
			case "cpu_lim":
				out.CPULim = st
			case "mem_req":
				out.MemReq = st
			case "mem_lim":
				out.MemLim = st
			}
		}(key, query)
	}
	wg.Wait()
	if failed == len(queries) {
		return nil, fmt.Errorf("todas as consultas de consumo falharam: %s", strings.Join(out.Notes, "; "))
	}
	if out.CPUReq == nil && out.MemReq == nil && failed == 0 {
		out.Notes = append(out.Notes, "sem dado de consumo no Prometheus para esta janela (retenção ou pods com outro nome)")
	}
	return out, nil
}
