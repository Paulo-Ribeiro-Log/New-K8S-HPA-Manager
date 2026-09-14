package dynatrace

import (
	"context"
	"fmt"
	"net/url"
	"sync"
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
	CPUMaxMillicores float64 // pico observado — espelha MemMaxBytes, usado pro "top" de CPU na UI
	MemAvgBytes      float64
	MemP95Bytes      float64
	MemMaxBytes      float64 // pico observado — usado só pra Mem Limit recomendado (ver finops.recommendedLimits)
}

// GetAllWorkloadMetrics consulta CPU e memória de todos os workloads monitorados pelo DT
// no período dado, retornando um mapa "namespace/workload" → WorkloadMetrics.
// Usa splitBy para fazer apenas 6 queries (avg+P95+max × cpu+mem) em vez de N queries por
// workload — as 6 são disparadas em PARALELO (bug real corrigido: eram sequenciais, cada uma
// pagando o RTT completo do DT — historicamente lento contra o metrics/query real desta empresa
// — antes da próxima nem começar; mesma classe de problema já corrigida no lado Prometheus desta
// mesma investigação de lentidão, ver internal/finops/prometheus_enricher.go — só que nunca
// tinha sido olhada aqui). Retorna mapa vazio (não erro) quando DT não tem dados para o cluster.
func (c *Client) GetAllWorkloadMetrics(ctx context.Context, windowDays int) (map[string]WorkloadMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, dtQueryTimeout)
	defer cancel()

	from := fmt.Sprintf("now-%dd", windowDays)
	result := make(map[string]WorkloadMetrics)

	var (
		cpuAvg, cpuP95, memAvg, memP95, memMax, cpuMax map[string]float64
		cpuAvgErr, cpuP95Err, memAvgErr, memP95Err     error
	)
	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	run(func() { cpuAvg, cpuAvgErr = c.queryWorkloadBatch(ctx, metricCPUMillicores, "avg", from) })
	run(func() { cpuP95, cpuP95Err = c.queryWorkloadBatch(ctx, metricCPUMillicores, "percentile(95)", from) })
	run(func() { memAvg, memAvgErr = c.queryWorkloadBatch(ctx, metricMemBytes, "avg", from) })
	run(func() { memP95, memP95Err = c.queryWorkloadBatch(ctx, metricMemBytes, "percentile(95)", from) })
	// Pico real de memória (mesmo papel do max_over_time do Prometheus) — usado só pra Mem Limit
	// recomendado, nunca pro request. Best-effort: se a query falhar, memMax fica vazio e a
	// recomendação de Mem Limit cai pra P95×margem sem o benefício extra do pico real (ver
	// finops.recommendedLimits) — não derruba o enriquecimento inteiro por causa disso.
	run(func() {
		var err error
		memMax, err = c.queryWorkloadBatch(ctx, metricMemBytes, "max", from)
		if err != nil {
			memMax = map[string]float64{}
		}
	})
	// Pico real de CPU (espelha memMax acima) — best-effort, mesma tolerância a falha.
	run(func() {
		var err error
		cpuMax, err = c.queryWorkloadBatch(ctx, metricCPUMillicores, "max", from)
		if err != nil {
			cpuMax = map[string]float64{}
		}
	})
	wg.Wait()

	if cpuAvgErr != nil {
		return nil, fmt.Errorf("DT finops cpu avg: %w", cpuAvgErr)
	}
	if cpuP95Err != nil {
		return nil, fmt.Errorf("DT finops cpu p95: %w", cpuP95Err)
	}
	if memAvgErr != nil {
		return nil, fmt.Errorf("DT finops mem avg: %w", memAvgErr)
	}
	if memP95Err != nil {
		return nil, fmt.Errorf("DT finops mem p95: %w", memP95Err)
	}

	// Merge nos mapas usando chave "namespace/workload"
	keys := make(map[string]struct{})
	for k := range cpuAvg {
		keys[k] = struct{}{}
	}
	for k := range cpuP95 {
		keys[k] = struct{}{}
	}
	for k := range memAvg {
		keys[k] = struct{}{}
	}
	for k := range memP95 {
		keys[k] = struct{}{}
	}

	for k := range keys {
		result[k] = WorkloadMetrics{
			CPUAvgMillicores: cpuAvg[k],
			CPUP95Millicores: cpuP95[k],
			CPUMaxMillicores: cpuMax[k],
			MemAvgBytes:      memAvg[k],
			MemP95Bytes:      memP95[k],
			MemMaxBytes:      memMax[k],
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
