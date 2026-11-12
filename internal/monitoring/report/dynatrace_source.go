package report

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"k8s-hpa-manager/internal/dynatrace"
)

// DynatraceAPI é o que o relatório usa do cliente do Dynatrace (*dynatrace.Client satisfaz).
type DynatraceAPI interface {
	ResolveEntryServices(ctx context.Context, cluster, namespace, workload string) ([]dynatrace.ServiceRef, []string, error)
	QueryServiceSeries(ctx context.Context, selector string, serviceIDs []string, from, to time.Time, resolution string, scale float64) (map[string]dynatrace.NullableSeries, error)
}

const (
	selRequests = `builtin:service.requestCount.total:splitBy("dt.entity.service"):sum`
	sel4xx      = `builtin:service.errors.fourxx.count:splitBy("dt.entity.service"):sum`
	sel5xx      = `builtin:service.errors.fivexx.count:splitBy("dt.entity.service"):sum`
	// média do tempo de resposta (µs; a tela converte para ms). Agregada entre serviços PONDERADA pelas requisições.
	selAvg = `builtin:service.response.time:splitBy("dt.entity.service"):avg`
	selP50 = `builtin:service.response.time:splitBy("dt.entity.service"):percentile(50)`
	selP95 = `builtin:service.response.time:splitBy("dt.entity.service"):percentile(95)`
	selP99 = `builtin:service.response.time:splitBy("dt.entity.service"):percentile(99)`
)

func toSeries(n dynatrace.NullableSeries) Series { return Series{Times: n.Times, Values: n.Values} }

func emptySeries() Series { return Series{Values: []float64{math.NaN()}, Times: []int64{0}} }

// DynatraceTraffic coleta o tráfego de um Deployment: resolve os serviços de entrada pelo process
// group e lê requisições, 4xx, 5xx (na resolução do Plan) e P50, P95 e P99 (resolução de no mínimo 5 min).
func DynatraceTraffic(api DynatraceAPI, floor float64, percentiles []int) func(ctx context.Context, it Item, w Window, plan Plan) (*Traffic, error) {
	return func(ctx context.Context, it Item, w Window, plan Plan) (*Traffic, error) {
		svcs, notes, err := api.ResolveEntryServices(ctx, it.Cluster, it.Namespace, it.Workload)
		if err != nil {
			if errors.Is(err, dynatrace.ErrNoProcessGroup) {
				return nil, fmt.Errorf("Dynatrace: %w (%s/%s)", err, it.Namespace, it.Workload)
			}
			return nil, fmt.Errorf("Dynatrace: %w", err)
		}
		if len(svcs) == 0 {
			return nil, fmt.Errorf("Dynatrace: o process group de %s/%s não tem serviço de entrada (web/RPC)", it.Namespace, it.Workload)
		}
		ids := make([]string, len(svcs))
		for i, s := range svcs {
			ids[i] = s.ID
		}

		type job struct {
			sel, res string
			scale    float64
		}
		jobs := map[string]job{
			"req": {selRequests, plan.DTResolution, 1},
			"4xx": {sel4xx, plan.DTResolution, 1},
			"5xx": {sel5xx, plan.DTResolution, 1},
			// a média é barata e usa a mesma resolução das requisições (os pesos têm de cair nos mesmos instantes)
			"avg": {selAvg, plan.DTResolution, 0.001},
		}
		// só os percentis escolhidos (microssegundos -> ms; resolução de no mínimo 5 min)
		for _, p := range percentiles {
			switch p {
			case 50:
				jobs["p50"] = job{selP50, plan.P99Resolution, 0.001}
			case 95:
				jobs["p95"] = job{selP95, plan.P99Resolution, 0.001}
			case 99:
				jobs["p99"] = job{selP99, plan.P99Resolution, 0.001}
			}
		}
		var (
			mu   sync.Mutex
			wg   sync.WaitGroup
			got  = map[string]map[string]dynatrace.NullableSeries{}
			errs = map[string]error{}
		)
		for key, j := range jobs {
			wg.Add(1)
			go func(key string, j job) {
				defer wg.Done()
				m, err := api.QueryServiceSeries(ctx, j.sel, ids, w.Start, w.End, j.res, j.scale)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs[key] = err
					return
				}
				got[key] = m
			}(key, j)
		}
		wg.Wait()
		// sem requisições não há relatório de tráfego; falha só de erros/P99 vira nota
		if e := errs["req"]; e != nil {
			return nil, fmt.Errorf("Dynatrace: requisições: %w", e)
		}

		pick := func(key, id string) Series {
			if s, ok := got[key][id]; ok {
				return toSeries(s)
			}
			return emptySeries()
		}
		var st []ServiceTraffic
		for _, s := range svcs {
			st = append(st, ServiceTraffic{ID: s.ID, Name: s.Name, Type: s.Type,
				Requests: pick("req", s.ID), Err4xx: pick("4xx", s.ID), Err5xx: pick("5xx", s.ID),
				AvgMs: pick("avg", s.ID), P50Ms: pick("p50", s.ID), P95Ms: pick("p95", s.ID), P99Ms: pick("p99", s.ID)})
		}
		tr := AggregateTraffic(st, plan.DTRes.Seconds(), floor)
		tr.PeakApprox = plan.PeakApprox
		tr.Resolution = plan.DTResolution
		tr.Notes = append(tr.Notes, notes...)
		tr.Notes = append(tr.Notes, plan.Notes...)
		if len(percentiles) > 0 && (tr.P50PeakMs != nil || tr.P95PeakMs != nil || tr.P99PeakMs != nil) {
			tr.Notes = append(tr.Notes, fmt.Sprintf("percentis de pico (P%s) consideram só serviços com ≥ %.0f%% das requisições (resolução %s)", joinInts(percentiles, "/P"), floor*100, plan.P99Resolution))
		}
		for key, e := range errs {
			tr.Notes = append(tr.Notes, fmt.Sprintf("%s indisponível: %v", key, e))
		}
		return &tr, nil
	}
}

func joinInts(in []int, sep string) string {
	out := ""
	for i, v := range in {
		if i > 0 {
			out += sep
		}
		out += fmt.Sprint(v)
	}
	return out
}
