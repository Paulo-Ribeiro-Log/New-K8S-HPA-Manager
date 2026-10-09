package finops

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/common/model"
)

// deepThrottleMaxWindowDays limita a janela do P95 de throttling (a subquery é pesada).
const deepThrottleMaxWindowDays = 7

// deepThrottleQueries monta as queries de throttling (P95 na janela e atual) restritas aos namespaces.
// Throttling = períodos do CFS em que o container foi congelado ÷ períodos totais, em %.
func deepThrottleQueries(namespaces []string, windowDays int) (p95, current string) {
	quoted := make([]string, len(namespaces))
	for i, ns := range namespaces {
		quoted[i] = regexp.QuoteMeta(ns)
	}
	sel := fmt.Sprintf(`container!="",container!="POD",namespace=~"%s"`, strings.Join(quoted, "|"))
	ratio := fmt.Sprintf(`(sum by (namespace, pod) (rate(container_cpu_cfs_throttled_periods_total{%s}[5m])) / clamp_min(sum by (namespace, pod) (rate(container_cpu_cfs_periods_total{%s}[5m])), 1e-9))`, sel, sel)
	return fmt.Sprintf(`quantile_over_time(0.95, %s[%dd:5m]) * 100`, ratio, windowDays), ratio + " * 100"
}

// CPUThrottlingByPod devolve o throttling de CPU por pod ("namespace/pod") dos namespaces informados:
// P95 na janela (até 7 dias) e o valor atual. Erro só quando as duas consultas falham.
func (e *PrometheusEnricher) CPUThrottlingByPod(ctx context.Context, namespaces []string, windowDays int) (map[string]DeepThrottle, int, error) {
	if len(namespaces) == 0 {
		return map[string]DeepThrottle{}, 0, nil
	}
	if windowDays <= 0 || windowDays > deepThrottleMaxWindowDays {
		windowDays = deepThrottleMaxWindowDays
	}
	qP95, qCur := deepThrottleQueries(namespaces, windowDays)

	run := func(q string) (map[string]float64, error) {
		qctx, cancel := context.WithTimeout(ctx, promHeavyQueryTimeout())
		defer cancel()
		res, _, err := e.api.Query(qctx, q, time.Now())
		if err != nil {
			return nil, err
		}
		vec, ok := res.(model.Vector)
		if !ok {
			return map[string]float64{}, nil
		}
		m := make(map[string]float64, len(vec))
		for _, smp := range vec {
			ns, pod := string(smp.Metric["namespace"]), string(smp.Metric["pod"])
			if ns != "" && pod != "" {
				m[ns+"/"+pod] = float64(smp.Value)
			}
		}
		return m, nil
	}

	p95, errP95 := run(qP95)
	cur, errCur := run(qCur)
	if errP95 != nil && errCur != nil {
		return nil, windowDays, fmt.Errorf("throttling: %w", errP95)
	}
	out := make(map[string]DeepThrottle, len(p95)+len(cur))
	for k, v := range p95 {
		t := out[k]
		t.P95Pct = v
		out[k] = t
	}
	for k, v := range cur {
		t := out[k]
		t.CurrentPct = v
		out[k] = t
	}
	if errP95 != nil {
		windowDays = 0 // só o valor atual
	}
	return out, windowDays, nil
}
