package report

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Extras do relatório (opt-in, só Prometheus): réplicas, teto do HPA, restarts e rede da aplicação na
// janela, os mesmos itens do tempo real. Throttling de CPU continua no contexto de node.
// Tráfego, erros e latência seguem vindo só do Dynatrace.

// Extras é o resumo dessas métricas numa janela. Ponteiro nulo = sem dado (nunca zero).
type Extras struct {
	Source   string   `json:"source"`
	Replicas *Pct     `json:"replicas"`         // réplicas disponíveis (máx/mín/méd)
	Desired  *Pct     `json:"replicas_desired"` // réplicas desejadas
	HPAMin   *float64 `json:"hpa_min"`
	HPAMax   *float64 `json:"hpa_max"`
	Restarts *float64 `json:"restarts"` // total de restarts dos containers na janela
	NetRx    *Pct     `json:"net_rx_bps"`
	NetTx    *Pct     `json:"net_tx_bps"`
	NetErr   *Pct     `json:"net_err_ps"` // pacotes descartados + erros por segundo
	Step     string   `json:"step"`
	Notes    []string `json:"notes,omitempty"`
}

// extrasQueries monta as consultas de UMA aplicação. `restarts` soma os incrementos de cada passo
// (janela do increase = passo), então o total da janela é a soma dos pontos. Função pura.
func extrasQueries(namespace, workload string, step time.Duration) map[string]string {
	dep := fmt.Sprintf(`namespace="%s",deployment="%s"`, namespace, workload)
	// HPA do Deployment: pelo alvo do HPA e, onde faltar, pelo nome igual ao do Deployment
	hpa := func(metric string) string {
		info := fmt.Sprintf(`kube_horizontalpodautoscaler_info{namespace="%s",scaletargetref_kind="Deployment",scaletargetref_name="%s"}`, namespace, workload)
		return fmt.Sprintf(`max(%s{namespace="%s"} * on (namespace,horizontalpodautoscaler) %s) or max(%s{namespace="%s",horizontalpodautoscaler="%s"})`, metric, namespace, info, metric, namespace, workload)
	}
	net := netQueries(namespace, workload)
	pods := fmt.Sprintf(`namespace="%s",%s`, namespace, podSelector(workload))
	return map[string]string{
		"replicas": fmt.Sprintf(`max(kube_deployment_status_replicas_available{%s})`, dep),
		"desired":  fmt.Sprintf(`max(kube_deployment_spec_replicas{%s})`, dep),
		"hpa_max":  hpa("kube_horizontalpodautoscaler_spec_max_replicas"),
		"hpa_min":  hpa("kube_horizontalpodautoscaler_spec_min_replicas"),
		"restarts": fmt.Sprintf(`sum(max by (pod,container) (increase(kube_pod_container_status_restarts_total{%s}[%s])))`, pods, promDur(step)),
		"net_rx":   net["rx"],
		"net_tx":   net["tx"],
		"net_err":  net["err"],
	}
}

// sumOfSeries soma os pontos válidos da primeira série (total de um contador por passo).
func sumOfSeries(r *rangeResult) *float64 {
	if r == nil || len(r.Data.Result) == 0 {
		return nil
	}
	vs := floatsOf(r.Data.Result[0].Values)
	if len(vs) == 0 {
		return nil
	}
	var s float64
	for _, v := range vs {
		s += v
	}
	s = math.Round(s)
	return &s
}

// maxOfSeries devolve o maior valor da primeira série (limites do HPA: valem o maior da janela).
func maxOfSeries(r *rangeResult) *float64 {
	st := pctStats(r)
	if st == nil {
		return nil
	}
	v := st.Max
	return &v
}

// CollectExtras roda as consultas em paralelo. Falha isolada deixa só aquele campo sem dado e anota o
// motivo; erro só se TODAS falharem.
func CollectExtras(ctx context.Context, q RangeQuerier, namespace, workload string, w Window, plan Plan) (*Extras, error) {
	if !k8sNameStrict.MatchString(namespace) || !k8sNameStrict.MatchString(workload) {
		return nil, fmt.Errorf("namespace ou deployment com nome inválido")
	}
	queries := extrasQueries(namespace, workload, plan.PromStep)
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		out    = &Extras{Source: "prometheus", Step: durLabel(plan.PromStep)}
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
			switch key {
			case "replicas":
				out.Replicas = pctStats(res)
			case "desired":
				out.Desired = pctStats(res)
			case "hpa_max":
				out.HPAMax = maxOfSeries(res)
			case "hpa_min":
				out.HPAMin = maxOfSeries(res)
			case "restarts":
				out.Restarts = sumOfSeries(res)
			case "net_rx":
				out.NetRx = pctStats(res)
			case "net_tx":
				out.NetTx = pctStats(res)
			case "net_err":
				out.NetErr = pctStats(res)
			}
		}(key, query)
	}
	wg.Wait()
	sort.Strings(out.Notes)
	if failed == len(queries) {
		return nil, fmt.Errorf("todas as consultas falharam: %s", strings.Join(out.Notes, "; "))
	}
	if out.Replicas == nil && out.NetRx == nil && failed == 0 {
		out.Notes = append(out.Notes, "sem dado no Prometheus para esta janela (retenção ou outro nome de Deployment)")
	}
	return out, nil
}
