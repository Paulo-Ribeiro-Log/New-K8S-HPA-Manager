package dynatrace

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"
)

// ─── Tipos de resposta ────────────────────────────────────────────────────────

// MetricPoint ponto temporal com epoch ms e valor float.
type MetricPoint struct {
	T int64   `json:"t"` // epoch ms
	V float64 `json:"v"`
}

// MetricSeriesData série de pontos com metadados de exibição.
type MetricSeriesData struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Unit   string        `json:"unit"`
	Points []MetricPoint `json:"points"`
}

// EntityMetricsData métricas coletadas para uma entidade de um problem.
type EntityMetricsData struct {
	EntityID    string             `json:"entityId"`
	EntityName  string             `json:"entityName"`
	EntityType  string             `json:"entityType"`
	IsRootCause bool               `json:"isRootCause"`
	Metrics     []MetricSeriesData `json:"metrics"`
}

// ProblemMetricsResponse resposta do endpoint GET /problems/:id/metrics.
type ProblemMetricsResponse struct {
	ProblemID    string              `json:"problemId"`
	Title        string              `json:"title"`
	TimeFrom     time.Time           `json:"timeFrom"`
	TimeTo       time.Time           `json:"timeTo"`
	ProblemStart time.Time           `json:"problemStart"`
	Entities     []EntityMetricsData `json:"entities"`
}

// ─── Definições de métricas por tipo de entidade ──────────────────────────────

type metricDef struct {
	Key      string  // chave interna única
	Selector string  // Dynatrace metric selector
	Label    string  // rótulo de exibição
	Unit     string  // unidade
	Scale    float64 // fator de conversão (0 = sem conversão)
}

// SERVICE — latência (P50/P90/P95/P99), erros, throughput, DB, externos
var serviceMetricDefs = []metricDef{
	{"response_p50", "builtin:service.response.time:percentile(50)", "Latência P50", "ms", 0.001},
	{"response_p90", "builtin:service.response.time:percentile(90)", "Latência P90", "ms", 0.001},
	{"response_p95", "builtin:service.response.time:percentile(95)", "Latência P95", "ms", 0.001},
	{"response_p99", "builtin:service.response.time:percentile(99)", "Latência P99", "ms", 0.001},
	{"error_rate", "builtin:service.errors.total.rate", "Taxa de Erro", "%", 1},
	{"error_count", "builtin:service.errors.total.count", "Erros", "req", 1},
	{"throughput", "builtin:service.requestCount.total", "Throughput", "req/min", 1},
	{"ext_calls", "builtin:service.nonDbChildCallCount", "Calls Externos", "calls/min", 1},
	{"db_calls", "builtin:service.dbChildCallCount", "Calls DB", "calls/min", 1},
	{"db_latency_p95", "builtin:service.dbChildCallTime:percentile(95)", "Latência DB P95", "ms", 0.001},
}

// CLOUD_APPLICATION / CLOUD_APPLICATION_INSTANCE — K8s pods + containers
var k8sWorkloadMetricDefs = []metricDef{
	{"pods_running", "builtin:kubernetes.workload.pods.running", "Pods Rodando", "pods", 1},
	{"pods_ready_pct", "builtin:kubernetes.workload.pods.readyFraction", "Pods Prontos", "%", 100},
	{"pod_restarts", "builtin:kubernetes.workload.pods.restarts", "Restarts", "count", 1},
	{"cpu_milli", "builtin:containers.cpu.usageMilliCores", "CPU Uso", "mCPU", 1},
	{"cpu_throttle", "builtin:containers.cpu.throttlingTime", "CPU Throttling", "ms", 1},
	{"memory_mb", "builtin:containers.memory.workingSet", "Memória", "MB", 1.0 / (1024 * 1024)},
}

// KUBERNETES_NODE — nó K8s
var k8sNodeMetricDefs = []metricDef{
	{"node_cpu", "builtin:kubernetes.node.cpu.usage", "CPU Nó", "%", 1},
	{"node_pods", "builtin:kubernetes.node.pods.running", "Pods no Nó", "pods", 1},
	{"cpu_milli", "builtin:containers.cpu.usageMilliCores", "CPU Containers", "mCPU", 1},
	{"memory_mb", "builtin:containers.memory.workingSet", "Memória", "MB", 1.0 / (1024 * 1024)},
}

// HOST — infraestrutura de host
var hostMetricDefs = []metricDef{
	{"host_cpu", "builtin:host.cpu.usage", "CPU", "%", 1},
	{"host_mem_avail", "builtin:host.mem.availableGBytes", "Memória Disponível", "GB", 1},
	{"net_in", "builtin:host.net.nic.trafficIn", "Rede Entrada", "KB/s", 1.0 / 1024},
	{"net_out", "builtin:host.net.nic.trafficOut", "Rede Saída", "KB/s", 1.0 / 1024},
}

// PROCESS_GROUP / PROCESS_GROUP_INSTANCE — processos
var processMetricDefs = []metricDef{
	{"proc_cpu", "builtin:tech.generic.cpu.usage", "CPU", "%", 1},
	{"proc_mem", "builtin:tech.generic.mem.usage", "Memória", "MB", 1.0 / (1024 * 1024)},
	{"tcp_sockets", "builtin:process.network.tcpSocketOpenCount", "Sockets TCP Abertos", "count", 1},
}

// APPLICATION — aplicação (browser/mobile)
var applicationMetricDefs = []metricDef{
	{"app_response", "builtin:apps.web.action.duration.load:percentile(95)", "Duração Ação P95", "ms", 0.001},
	{"app_errors", "builtin:apps.web.actionCount.errors", "Erros de Ação", "count", 1},
	{"app_sessions", "builtin:apps.web.sessionCount", "Sessões", "sessions", 1},
}

func metricsForEntityType(entityType string) []metricDef {
	switch entityType {
	case "SERVICE":
		return serviceMetricDefs
	case "CLOUD_APPLICATION", "CLOUD_APPLICATION_INSTANCE":
		return k8sWorkloadMetricDefs
	case "KUBERNETES_NODE":
		return k8sNodeMetricDefs
	case "HOST":
		return hostMetricDefs
	case "PROCESS_GROUP", "PROCESS_GROUP_INSTANCE":
		return processMetricDefs
	case "APPLICATION":
		return applicationMetricDefs
	default:
		// Fallback: tenta service metrics (mais comum)
		return serviceMetricDefs
	}
}

// ─── Parsers com suporte a null ───────────────────────────────────────────────

type metricDataRaw struct {
	MetricID string `json:"metricId"`
	Data     []struct {
		Timestamps []int64    `json:"timestamps"`
		Values     []*float64 `json:"values"` // ponteiro para detectar null
	} `json:"data"`
}

// autoResolution escolhe resolução temporal adequada para a janela.
func autoResolution(from, to time.Time) string {
	window := to.Sub(from)
	switch {
	case window <= 2*time.Hour:
		return "1m"
	case window <= 6*time.Hour:
		return "5m"
	case window <= 24*time.Hour:
		return "10m"
	default:
		return "30m"
	}
}

// ─── Métodos do Client ────────────────────────────────────────────────────────

// fetchMetricPoints busca uma métrica e converte para MetricPoints (descarta nulls).
func (c *Client) fetchMetricPoints(ctx context.Context, entityID string, def metricDef, from, to, resolution string) []MetricPoint {
	params := url.Values{
		"metricSelector": {def.Selector},
		"entitySelector": {fmt.Sprintf(`entityId("%s")`, entityID)},
		"from":           {from},
		"to":             {to},
		"resolution":     {resolution},
	}

	var result struct {
		Result []metricDataRaw `json:"result"`
	}
	if err := c.get(ctx, "metrics/query", params, &result); err != nil {
		return nil
	}
	if len(result.Result) == 0 || len(result.Result[0].Data) == 0 {
		return nil
	}

	series := result.Result[0].Data[0]
	scale := def.Scale
	if scale == 0 {
		scale = 1
	}

	points := make([]MetricPoint, 0)
	for i, ts := range series.Timestamps {
		if i >= len(series.Values) || series.Values[i] == nil {
			continue
		}
		points = append(points, MetricPoint{T: ts, V: *series.Values[i] * scale})
	}
	return points
}

// getMetricsBatch busca todas as métricas de uma entidade em goroutines paralelas.
// Retorna apenas as séries que tiverem dados.
func (c *Client) getMetricsBatch(ctx context.Context, entityID string, defs []metricDef, from, to, resolution string) []MetricSeriesData {
	type chanResult struct {
		idx    int
		series MetricSeriesData
	}

	ch := make(chan chanResult, len(defs))
	var wg sync.WaitGroup

	for i, def := range defs {
		wg.Add(1)
		go func(idx int, d metricDef) {
			defer wg.Done()
			points := c.fetchMetricPoints(ctx, entityID, d, from, to, resolution)
			ch <- chanResult{idx: idx, series: MetricSeriesData{
				Key:    d.Key,
				Label:  d.Label,
				Unit:   d.Unit,
				Points: points,
			}}
		}(i, def)
	}

	go func() { wg.Wait(); close(ch) }()

	ordered := make([]MetricSeriesData, len(defs))
	for r := range ch {
		ordered[r.idx] = r.series
	}

	out := make([]MetricSeriesData, 0)
	for _, s := range ordered {
		if len(s.Points) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// GetEntityMetricsForProblem coleta métricas de todas as entidades de um problem em paralelo.
// Janela: 30min antes do início até agora (ou fim + 15min se o problem já foi resolvido).
func (c *Client) GetEntityMetricsForProblem(ctx context.Context, problem *Problem) *ProblemMetricsResponse {
	from := problem.StartTime.Add(-30 * time.Minute)
	to := time.Now()
	if problem.EndTime != nil {
		if candidate := problem.EndTime.Add(15 * time.Minute); candidate.Before(to) {
			to = candidate
		}
	}

	fromStr := from.UTC().Format(time.RFC3339)
	toStr := to.UTC().Format(time.RFC3339)
	res := autoResolution(from, to)

	// Coletar entidades únicas: rootCause → affected → impacted
	type entityEntry struct {
		ID          string
		Name        string
		Type        string
		IsRootCause bool
	}

	seen := make(map[string]bool)
	var entries []entityEntry

	addStub := func(stub *EntityStub, isRoot bool) {
		if stub == nil || seen[stub.EntityID.ID] {
			return
		}
		seen[stub.EntityID.ID] = true
		entries = append(entries, entityEntry{
			ID:          stub.EntityID.ID,
			Name:        stub.BestName(),
			Type:        stub.EntityID.Type,
			IsRootCause: isRoot,
		})
	}

	addStub(problem.RootCauseEntity, true)
	for i := range problem.AffectedEntities {
		addStub(&problem.AffectedEntities[i], false)
	}
	for i := range problem.ImpactedEntities {
		addStub(&problem.ImpactedEntities[i], false)
	}

	// Buscar métricas de todas as entidades em paralelo
	type entityResult struct {
		idx  int
		data EntityMetricsData
	}

	ch := make(chan entityResult, len(entries))
	for i, entry := range entries {
		go func(idx int, e entityEntry) {
			defs := metricsForEntityType(e.Type)
			metrics := c.getMetricsBatch(ctx, e.ID, defs, fromStr, toStr, res)
			ch <- entityResult{idx: idx, data: EntityMetricsData{
				EntityID:    e.ID,
				EntityName:  e.Name,
				EntityType:  e.Type,
				IsRootCause: e.IsRootCause,
				Metrics:     metrics,
			}}
		}(i, entry)
	}

	ordered := make([]EntityMetricsData, len(entries))
	for range entries {
		r := <-ch
		ordered[r.idx] = r.data
	}

	return &ProblemMetricsResponse{
		ProblemID:    problem.ProblemID,
		Title:        problem.Title,
		TimeFrom:     from,
		TimeTo:       to,
		ProblemStart: problem.StartTime,
		Entities:     ordered,
	}
}
