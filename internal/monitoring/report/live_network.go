package report

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NetworkSeries é a rede de UMA aplicação ao longo do tempo (gráfico do modal): bytes/s recebidos e
// enviados, e descartes + erros de pacote por segundo, somando os pods do Deployment. Nulo = sem dado.
type NetworkSeries struct {
	Item        Item       `json:"item"`
	StepSeconds int64      `json:"step_seconds"`
	Times       []int64    `json:"times"` // epoch ms
	RxBps       []*float64 `json:"rx_bps"`
	TxBps       []*float64 `json:"tx_bps"`
	ErrPs       []*float64 `json:"err_ps"`
	Notes       []string   `json:"notes,omitempty"`
}

const (
	netMaxWindow = 15 * 24 * time.Hour // um pouco acima da retenção de 14 dias do Prometheus
	netMaxPoints = 150
)

// netQueries monta as três consultas da aplicação. O pod é casado pela mesma regra do dono do pod
// ("<deploy>-<hash>-<sufixo>"), com o `\` dobrado para caber na string do PromQL.
func netQueries(ns, workload string) map[string]string {
	podRe := strings.ReplaceAll(regexp.QuoteMeta(workload)+`-[a-z0-9]{5,10}-[a-z0-9]{5}`, `\`, `\\`)
	sel := fmt.Sprintf(`namespace="%s",pod=~"%s"`, strings.ReplaceAll(ns, `\`, `\\`), podRe)
	sum := func(inner string) string { return fmt.Sprintf(`sum (max by (pod,interface) (%s))`, inner) }
	return map[string]string{
		"rx":  sum(fmt.Sprintf(`rate(container_network_receive_bytes_total{%s}[2m])`, sel)),
		"tx":  sum(fmt.Sprintf(`rate(container_network_transmit_bytes_total{%s}[2m])`, sel)),
		"err": sum(fmt.Sprintf(`rate(container_network_receive_packets_dropped_total{%[1]s}[2m]) + rate(container_network_transmit_packets_dropped_total{%[1]s}[2m]) + rate(container_network_receive_errors_total{%[1]s}[2m]) + rate(container_network_transmit_errors_total{%[1]s}[2m])`, sel)),
	}
}

// netStep escolhe o passo: no máximo netMaxPoints pontos e nunca menos de 1 minuto.
func netStep(from, to time.Time) time.Duration {
	step := to.Sub(from) / netMaxPoints
	if step < time.Minute {
		return time.Minute
	}
	return step.Truncate(time.Minute)
}

// CollectNetworkSeries consulta o Prometheus do cluster da aplicação. Falha numa das três consultas
// vira nota e deixa só aquela série sem dado; erro só quando as três falham.
func CollectNetworkSeries(ctx context.Context, q RangeQuerier, it Item, from, to time.Time) (*NetworkSeries, error) {
	if !k8sNameStrict.MatchString(it.Namespace) || !k8sNameStrict.MatchString(it.Workload) {
		return nil, fmt.Errorf("namespace ou deployment com nome inválido")
	}
	if !to.After(from) || to.Sub(from) > netMaxWindow {
		return nil, fmt.Errorf("janela inválida: use de 1 minuto a 15 dias")
	}
	step := netStep(from, to)
	qs := netQueries(it.Namespace, it.Workload)

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		got    = map[string]map[int64]float64{}
		notes  []string
		failed int
	)
	for key, query := range qs {
		wg.Add(1)
		go func(key, query string) {
			defer wg.Done()
			res, err := q.QueryRange(ctx, query, from, to, step)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				notes = append(notes, fmt.Sprintf("rede %s: %v", key, err))
				return
			}
			m := map[int64]float64{}
			for _, r := range res.Data.Result {
				for _, v := range r.Values {
					if len(v) < 2 {
						continue
					}
					ts, ok := v[0].(float64)
					str, ok2 := v[1].(string)
					if !ok || !ok2 {
						continue
					}
					f, err := strconv.ParseFloat(str, 64)
					if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
						continue
					}
					m[int64(ts)*1000] = f
				}
			}
			got[key] = m
		}(key, query)
	}
	wg.Wait()
	if failed == len(qs) {
		sort.Strings(notes)
		return nil, fmt.Errorf("as consultas de rede falharam: %s", strings.Join(notes, "; "))
	}
	sort.Strings(notes)

	seen := map[int64]bool{}
	for _, m := range got {
		for t := range m {
			seen[t] = true
		}
	}
	out := &NetworkSeries{Item: it, StepSeconds: int64(step / time.Second), Times: []int64{}, RxBps: []*float64{}, TxBps: []*float64{}, ErrPs: []*float64{}, Notes: notes}
	for t := range seen {
		out.Times = append(out.Times, t)
	}
	sort.Slice(out.Times, func(i, j int) bool { return out.Times[i] < out.Times[j] })
	at := func(key string, t int64) *float64 {
		if v, ok := got[key][t]; ok {
			return &v
		}
		return nil
	}
	for _, t := range out.Times {
		out.RxBps = append(out.RxBps, at("rx", t))
		out.TxBps = append(out.TxBps, at("tx", t))
		out.ErrPs = append(out.ErrPs, at("err", t))
	}
	return out, nil
}
