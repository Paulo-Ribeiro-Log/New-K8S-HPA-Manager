package dynatrace

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

const (
	metricCPUMillicores = "builtin:container.cpu.usageMilliCores"
	metricMemBytes      = "builtin:container.memory.workingSet"
	dtQueryTimeout      = 30 * time.Second
)

// WorkloadMetrics agrega as métricas de CPU e memória de um workload K8s retornadas pelo DT.
type WorkloadMetrics struct {
	CPUAvgMillicores float64
	CPUP95Millicores float64
	MemAvgBytes      float64
	MemP95Bytes      float64
}

// GetAllWorkloadMetrics consulta CPU e memória de todos os workloads monitorados pelo DT
// no período dado, retornando um mapa "namespace/workload" → WorkloadMetrics.
// Usa splitBy para fazer apenas 4 queries (avg+P95 × cpu+mem) em vez de N queries por workload.
// Retorna mapa vazio (não erro) quando DT não tem dados para o cluster.
func (c *Client) GetAllWorkloadMetrics(ctx context.Context, windowDays int) (map[string]WorkloadMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, dtQueryTimeout)
	defer cancel()

	from := fmt.Sprintf("now-%dd", windowDays)
	result := make(map[string]WorkloadMetrics)

	cpuAvg, err := c.queryWorkloadBatch(ctx, metricCPUMillicores, "avg", from)
	if err != nil {
		return nil, fmt.Errorf("DT finops cpu avg: %w", err)
	}
	cpuP95, err := c.queryWorkloadBatch(ctx, metricCPUMillicores, "percentile(95)", from)
	if err != nil {
		return nil, fmt.Errorf("DT finops cpu p95: %w", err)
	}
	memAvg, err := c.queryWorkloadBatch(ctx, metricMemBytes, "avg", from)
	if err != nil {
		return nil, fmt.Errorf("DT finops mem avg: %w", err)
	}
	memP95, err := c.queryWorkloadBatch(ctx, metricMemBytes, "percentile(95)", from)
	if err != nil {
		return nil, fmt.Errorf("DT finops mem p95: %w", err)
	}

	// Merge nos 4 mapas usando chave "namespace/workload"
	keys := make(map[string]struct{})
	for k := range cpuAvg { keys[k] = struct{}{} }
	for k := range cpuP95 { keys[k] = struct{}{} }
	for k := range memAvg { keys[k] = struct{}{} }
	for k := range memP95 { keys[k] = struct{}{} }

	for k := range keys {
		result[k] = WorkloadMetrics{
			CPUAvgMillicores: cpuAvg[k],
			CPUP95Millicores: cpuP95[k],
			MemAvgBytes:      memAvg[k],
			MemP95Bytes:      memP95[k],
		}
	}
	return result, nil
}

// queryWorkloadBatch executa uma query com splitBy de namespace e workload.
// aggregation: "avg", "percentile(95)", etc.
// Retorna mapa "namespace/workload" → valor agregado no período.
func (c *Client) queryWorkloadBatch(ctx context.Context, metric, aggregation, from string) (map[string]float64, error) {
	// resolution=inf agrega todo o período em um único ponto por série
	metricSelector := fmt.Sprintf(
		`%s:%s:splitBy("k8s.namespace.name","k8s.workload.name"):last`,
		metric, aggregation,
	)

	params := url.Values{
		"metricSelector": {metricSelector},
		"from":           {from},
		"to":             {"now"},
		"resolution":     {"inf"},
	}

	var response struct {
		Result []struct {
			Data []struct {
				DimensionMap map[string]string `json:"dimensionMap"`
				Values       []float64         `json:"values"`
			} `json:"data"`
		} `json:"result"`
	}

	if err := c.get(ctx, "metrics/query", params, &response); err != nil {
		return nil, err
	}

	out := make(map[string]float64)
	if len(response.Result) == 0 {
		return out, nil
	}

	for _, series := range response.Result[0].Data {
		ns := series.DimensionMap["k8s.namespace.name"]
		wl := series.DimensionMap["k8s.workload.name"]
		if ns == "" || wl == "" || len(series.Values) == 0 {
			continue
		}
		// Pegar o primeiro valor não-zero (resolution=inf devolve 1 ponto)
		for _, v := range series.Values {
			if v > 0 {
				out[ns+"/"+wl] = v
				break
			}
		}
	}
	return out, nil
}
