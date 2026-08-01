package finops

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/monitoring/discovery"
)

// HPADayPoint representa o estado de um HPA em um dia específico
type HPADayPoint struct {
	Date        string  `json:"date"`         // "2026-02-24"
	MaxReplicas float64 `json:"max_replicas"` // pico do dia
	AvgReplicas float64 `json:"avg_replicas"` // média do dia
	MinReplicas float64 `json:"min_replicas"` // mínimo do dia
}

// HPATimeline contém a série temporal de um HPA no período
type HPATimeline struct {
	Namespace string        `json:"namespace"`
	Workload  string        `json:"workload"` // nome do HPA (= nome do Deployment)
	HPAMin    int           `json:"hpa_min"`  // minReplicas configurado
	HPAMax    int           `json:"hpa_max"`  // maxReplicas configurado
	Series    []HPADayPoint `json:"series"`
}

// NodeDayPoint representa o número de nodes em um dia
type NodeDayPoint struct {
	Date      string `json:"date"`
	NodeCount int    `json:"node_count"` // máximo de nodes no dia
}

// CPUDayPoint representa uso e request de CPU do cluster em um dia (aggregate de todos os containers)
type CPUDayPoint struct {
	Date      string  `json:"date"`
	UsedMilli float64 `json:"used_millis"` // millicores usados (avg do dia)
	ReqMilli  float64 `json:"req_millis"`  // millicores solicitados (avg do dia)
}

// MemDayPoint representa uso e request de memória do cluster em um dia
type MemDayPoint struct {
	Date   string  `json:"date"`
	UsedMi float64 `json:"used_mi"` // MiB em uso (avg do dia)
	ReqMi  float64 `json:"req_mi"`  // MiB solicitados (avg do dia)
}

// TimelineReport é a resposta completa da análise temporal
type TimelineReport struct {
	Cluster   string         `json:"cluster"`
	StartDate string         `json:"start_date"`
	EndDate   string         `json:"end_date"`
	HPAs      []HPATimeline  `json:"hpas"`
	Nodes     []NodeDayPoint `json:"nodes"`
	CPU       []CPUDayPoint  `json:"cpu"`
	Mem       []MemDayPoint  `json:"mem"`
}

// QueryTimeline busca série temporal de HPAs e nodes via Prometheus QueryRange.
// Usa step=2h para balancear granularidade vs volume de dados, agrega max/avg/min por dia.
//
// requiresGCPAuth: true quando prometheusURL é o Google Cloud Managed Service for Prometheus
// (GMP, ver internal/monitoring/discovery.RequiresGCPAuth) — mesmo tratamento de auth/TLS de
// NewPrometheusEnricher.
func QueryTimeline(ctx context.Context, prometheusURL string, start, end time.Time, requiresGCPAuth bool) (*TimelineReport, error) {
	transport := &http.Transport{}
	var roundTripper http.RoundTripper = transport
	if requiresGCPAuth {
		roundTripper = discovery.GCPAuthTransport(transport)
	} else {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	httpClient := &http.Client{
		Transport: roundTripper,
		Timeout:   120 * time.Second,
	}
	apiClient, err := api.NewClient(api.Config{
		Address:      prometheusURL,
		RoundTripper: httpClient.Transport,
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao criar cliente Prometheus: %w", err)
	}
	promAPI := v1.NewAPI(apiClient)

	// Step de 2h: 30 dias = 360 pontos por série (bom balanço granularidade/velocidade)
	r := v1.Range{
		Start: start,
		End:   end,
		Step:  2 * time.Hour,
	}

	// 1. HPA replicas ao longo do tempo
	hpaMatrix, err := queryMatrix(ctx, promAPI, `kube_horizontalpodautoscaler_status_current_replicas`, r)
	if err != nil {
		return nil, fmt.Errorf("falha ao consultar HPA timeline: %w", err)
	}

	// 2. Configuração atual de min/max (instant query para referência nos charts)
	hpaMinVec, _ := queryVector(ctx, promAPI, `kube_horizontalpodautoscaler_spec_min_replicas`)
	hpaMaxVec, _ := queryVector(ctx, promAPI, `kube_horizontalpodautoscaler_spec_max_replicas`)
	hpaMinMap := vecToMap(hpaMinVec)
	hpaMaxMap := vecToMap(hpaMaxVec)

	// 3. Número de nodes ao longo do tempo
	nodeMatrix, _ := queryMatrix(ctx, promAPI,
		`count(kube_node_status_condition{condition="Ready",status="true"})`, r)

	// 4. CPU usage (millicores) e requests — soma de todos os containers do cluster
	cpuUsedMatrix, _ := queryMatrix(ctx, promAPI,
		`sum(rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])) * 1000`, r)
	cpuReqMatrix, _ := queryMatrix(ctx, promAPI,
		`sum(kube_pod_container_resource_requests{resource="cpu",container!=""}) * 1000`, r)

	// 5. Memória (MiB) e requests — soma de todos os containers do cluster
	memUsedMatrix, _ := queryMatrix(ctx, promAPI,
		`sum(container_memory_working_set_bytes{container!="",container!="POD"}) / 1048576`, r)
	memReqMatrix, _ := queryMatrix(ctx, promAPI,
		`sum(kube_pod_container_resource_requests{resource="memory",container!=""}) / 1048576`, r)

	resp := &TimelineReport{
		StartDate: start.Format("2006-01-02"),
		EndDate:   end.Format("2006-01-02"),
	}

	// Agrega HPA: hourly → daily (max, avg, min por dia)
	type hpaKey struct{ ns, hpa string }
	type dayAgg struct {
		sum   float64
		count int
		max   float64
		min   float64
	}
	hpaDaily := make(map[hpaKey]map[string]*dayAgg)

	for _, stream := range hpaMatrix {
		ns := string(stream.Metric["namespace"])
		hpa := string(stream.Metric["horizontalpodautoscaler"])
		if ns == "" || hpa == "" {
			continue
		}
		k := hpaKey{ns, hpa}
		if hpaDaily[k] == nil {
			hpaDaily[k] = make(map[string]*dayAgg)
		}
		for _, pair := range stream.Values {
			date := pair.Timestamp.Time().Format("2006-01-02")
			val := float64(pair.Value)
			agg, exists := hpaDaily[k][date]
			if !exists {
				hpaDaily[k][date] = &dayAgg{sum: val, count: 1, max: val, min: val}
				continue
			}
			agg.sum += val
			agg.count++
			if val > agg.max {
				agg.max = val
			}
			if val < agg.min {
				agg.min = val
			}
		}
	}

	for k, daily := range hpaDaily {
		mapKey := k.ns + "/" + k.hpa
		var series []HPADayPoint
		for date, agg := range daily {
			avg := 0.0
			if agg.count > 0 {
				avg = round2(agg.sum / float64(agg.count))
			}
			series = append(series, HPADayPoint{
				Date:        date,
				MaxReplicas: agg.max,
				AvgReplicas: avg,
				MinReplicas: agg.min,
			})
		}
		sort.Slice(series, func(i, j int) bool { return series[i].Date < series[j].Date })

		resp.HPAs = append(resp.HPAs, HPATimeline{
			Namespace: k.ns,
			Workload:  k.hpa,
			HPAMin:    int(hpaMinMap[mapKey]),
			HPAMax:    int(hpaMaxMap[mapKey]),
			Series:    series,
		})
	}

	// Ordenar por namespace/workload
	sort.Slice(resp.HPAs, func(i, j int) bool {
		if resp.HPAs[i].Namespace != resp.HPAs[j].Namespace {
			return resp.HPAs[i].Namespace < resp.HPAs[j].Namespace
		}
		return resp.HPAs[i].Workload < resp.HPAs[j].Workload
	})

	// Agrega nodes: hourly → daily (máximo por dia)
	nodeDaily := make(map[string]int)
	for _, stream := range nodeMatrix {
		for _, pair := range stream.Values {
			date := pair.Timestamp.Time().Format("2006-01-02")
			val := int(pair.Value)
			if val > nodeDaily[date] {
				nodeDaily[date] = val
			}
		}
	}
	for date, count := range nodeDaily {
		resp.Nodes = append(resp.Nodes, NodeDayPoint{Date: date, NodeCount: count})
	}
	sort.Slice(resp.Nodes, func(i, j int) bool { return resp.Nodes[i].Date < resp.Nodes[j].Date })

	// Agrega CPU/Mem: step=2h → daily avg usando helper genérico
	resp.CPU = buildCPUSeries(cpuUsedMatrix, cpuReqMatrix)
	resp.Mem = buildMemSeries(memUsedMatrix, memReqMatrix)

	log.Info().
		Int("hpas", len(resp.HPAs)).
		Int("node_days", len(resp.Nodes)).
		Int("cpu_days", len(resp.CPU)).
		Int("mem_days", len(resp.Mem)).
		Str("start", resp.StartDate).
		Str("end", resp.EndDate).
		Msg("FinOps: timeline carregada")

	return resp, nil
}

// scalarDayAgg acumula valores de uma série escalar (sum(…)) para média diária
type scalarDayAgg struct {
	sum   float64
	count int
}

// aggregateScalar itera sobre uma matrix de série escalar e agrega por dia (avg)
func aggregateScalar(matrix model.Matrix) map[string]*scalarDayAgg {
	daily := make(map[string]*scalarDayAgg)
	for _, stream := range matrix {
		for _, pair := range stream.Values {
			date := pair.Timestamp.Time().Format("2006-01-02")
			val := float64(pair.Value)
			if daily[date] == nil {
				daily[date] = &scalarDayAgg{}
			}
			daily[date].sum += val
			daily[date].count++
		}
	}
	return daily
}

func buildCPUSeries(usedMatrix, reqMatrix model.Matrix) []CPUDayPoint {
	usedByDay := aggregateScalar(usedMatrix)
	reqByDay := aggregateScalar(reqMatrix)

	dates := make(map[string]struct{})
	for d := range usedByDay {
		dates[d] = struct{}{}
	}
	for d := range reqByDay {
		dates[d] = struct{}{}
	}

	var out []CPUDayPoint
	for date := range dates {
		p := CPUDayPoint{Date: date}
		if a := usedByDay[date]; a != nil && a.count > 0 {
			p.UsedMilli = round2(a.sum / float64(a.count))
		}
		if a := reqByDay[date]; a != nil && a.count > 0 {
			p.ReqMilli = round2(a.sum / float64(a.count))
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func buildMemSeries(usedMatrix, reqMatrix model.Matrix) []MemDayPoint {
	usedByDay := aggregateScalar(usedMatrix)
	reqByDay := aggregateScalar(reqMatrix)

	dates := make(map[string]struct{})
	for d := range usedByDay {
		dates[d] = struct{}{}
	}
	for d := range reqByDay {
		dates[d] = struct{}{}
	}

	var out []MemDayPoint
	for date := range dates {
		p := MemDayPoint{Date: date}
		if a := usedByDay[date]; a != nil && a.count > 0 {
			p.UsedMi = round2(a.sum / float64(a.count))
		}
		if a := reqByDay[date]; a != nil && a.count > 0 {
			p.ReqMi = round2(a.sum / float64(a.count))
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// queryMatrix executa QueryRange e retorna uma model.Matrix
func queryMatrix(ctx context.Context, promAPI v1.API, query string, r v1.Range) (model.Matrix, error) {
	qctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	result, _, err := promAPI.QueryRange(qctx, query, r)
	if err != nil {
		return nil, err
	}
	mat, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("tipo inesperado: %T", result)
	}
	return mat, nil
}

// queryVector executa uma query instant e retorna model.Vector
func queryVector(ctx context.Context, promAPI v1.API, query string) (model.Vector, error) {
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, _, err := promAPI.Query(qctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	vec, ok := result.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("tipo inesperado: %T", result)
	}
	return vec, nil
}

// vecToMap transforma um Vector HPA em map["ns/hpa"] → valor
func vecToMap(vec model.Vector) map[string]float64 {
	m := make(map[string]float64)
	for _, s := range vec {
		ns := string(s.Metric["namespace"])
		hpa := string(s.Metric["horizontalpodautoscaler"])
		if ns != "" && hpa != "" {
			m[ns+"/"+hpa] = float64(s.Value)
		}
	}
	return m
}
