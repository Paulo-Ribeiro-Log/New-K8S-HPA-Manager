package dynatrace

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"
)

// metricCPUMillicores/metricMemBytes — 2ª rodada do mesmo bug: a 1ª correção trocou os IDs pra
// "builtin:containers.cpu.usageMilliCores"/"builtin:containers.memory.residentSetBytes" (confirmado
// existente, ver histórico abaixo) — mas o 404 virou 400: "The dimension key `k8s.namespace.name`
// has been referenced, but the metric has no such key" (confirmado ao vivo, `GET /metrics/<id>`
// contra o tenant real nyr48864 — dimensionDefinitions desse metric ID só tem
// "dt.entity.container_group_instance" e "Container", NUNCA k8s.namespace.name/k8s.workload.name;
// entityType primário é CONTAINER_GROUP_INSTANCE, um container físico, não o workload agregado).
// Trocado pra família "builtin:kubernetes.workload.*" — confirmado ao vivo, mesmo tenant, que
// TEM k8s.namespace.name/k8s.workload.name como dimensionDefinitions reais (STRING, não ENTITY) e
// que uma query real com esse splitBy retorna dado de verdade pra dezenas de workloads reais da
// frota (ex: "falcon-system/falcon-platform-falcon-sensor" com CPU avg ~25000 mCPU plausível).
// Achado adicional no mesmo teste ao vivo: NENHUM dos 44 metricId "builtin:kubernetes.*" deste
// tenant suporta agregação percentile — só "auto"/"avg"/"max"/"min" (erro real reproduzido:
// "Illegal transform operator: Found unsupported aggregation :percentile(95). Supported at this
// point: avg, max, min") — restrição do PRÓPRIO metric (aggregationTypes no descriptor), não um
// bug de query; por isso a família de queries de P95 foi removida (ver GetAllWorkloadMetrics
// abaixo) — pedir percentile(95) aqui SEMPRE falharia, não é uma falha transiente.
const (
	metricCPUMillicores = "builtin:kubernetes.workload.cpu_usage"
	metricMemBytes      = "builtin:kubernetes.workload.memory_working_set"
	dtQueryTimeout      = 30 * time.Second
)

// WorkloadMetrics agrega as métricas de CPU e memória de um workload K8s retornadas pelo DT.
// CPUP95Millicores/MemP95Bytes ficam sempre 0 — família "builtin:kubernetes.workload.*" não
// suporta agregação percentile neste tenant (ver comentário de metricCPUMillicores acima), então
// nunca fingimos ter um P95 real aqui (o campo existe só pra manter o mesmo shape usado pelo
// enriquecimento Prometheus, que ESSE sim consegue P95 de verdade via PromQL quantile_over_time).
type WorkloadMetrics struct {
	CPUAvgMillicores float64
	CPUP95Millicores float64 // sempre 0 pra DT — ver comentário acima
	CPUMaxMillicores float64 // pico observado — espelha MemMaxBytes, usado pro "top" de CPU na UI
	MemAvgBytes      float64
	MemP95Bytes      float64 // sempre 0 pra DT — ver comentário acima
	MemMaxBytes      float64 // pico observado — usado só pra Mem Limit recomendado (ver finops.recommendedLimits)
}

// GetAllWorkloadMetrics consulta CPU e memória de todos os workloads monitorados pelo DT
// no período dado, retornando um mapa "namespace/workload" → WorkloadMetrics.
// Usa splitBy para fazer apenas 4 queries (avg+max × cpu+mem) em vez de N queries por
// workload — as 4 são disparadas em PARALELO (bug real corrigido: eram sequenciais, cada uma
// pagando o RTT completo do DT — historicamente lento contra o metrics/query real desta empresa
// — antes da próxima nem começar; mesma classe de problema já corrigida no lado Prometheus desta
// mesma investigação de lentidão, ver internal/finops/prometheus_enricher.go — só que nunca
// tinha sido olhada aqui). Retorna mapa vazio (não erro) quando DT não tem dados para o cluster.
//
// Sem query de percentile(95) — removida de propósito (2ª rodada do bug real corrigido acima):
// pedir percentile pra "builtin:kubernetes.workload.*" sempre falha (400 "unsupported
// aggregation"), não é uma falha transiente digna de retry/best-effort, é uma restrição
// permanente do metric neste tenant. Só avg/max: chamar `finops.recommendedLimits`/o cálculo de
// CPURecommendedMillis com CPUAvgMillicores como base quando P95 não está disponível é
// responsabilidade do chamador (dynatrace_enricher.go), não deste client.
func (c *Client) GetAllWorkloadMetrics(ctx context.Context, windowDays int) (map[string]WorkloadMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, dtQueryTimeout)
	defer cancel()

	from := fmt.Sprintf("now-%dd", windowDays)
	result := make(map[string]WorkloadMetrics)

	var (
		cpuAvg, memAvg, memMax, cpuMax map[string]float64
		cpuAvgErr, memAvgErr           error
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
	run(func() { memAvg, memAvgErr = c.queryWorkloadBatch(ctx, metricMemBytes, "avg", from) })
	// Pico real de memória (mesmo papel do max_over_time do Prometheus) — usado pra Mem Limit
	// recomendado (ver finops.recommendedLimits) E como substituto de P95 quando este não existe.
	// Best-effort: se a query falhar, memMax fica vazio (não derruba o enriquecimento).
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
	if memAvgErr != nil {
		return nil, fmt.Errorf("DT finops mem avg: %w", memAvgErr)
	}

	// Merge nos mapas usando chave "namespace/workload"
	keys := make(map[string]struct{})
	for k := range cpuAvg {
		keys[k] = struct{}{}
	}
	for k := range memAvg {
		keys[k] = struct{}{}
	}

	for k := range keys {
		result[k] = WorkloadMetrics{
			CPUAvgMillicores: cpuAvg[k],
			CPUMaxMillicores: cpuMax[k],
			MemAvgBytes:      memAvg[k],
			MemMaxBytes:      memMax[k],
		}
	}
	return result, nil
}

// queryWorkloadBatch executa uma query com splitBy de namespace e workload.
// aggregation: "avg", "max", etc. (percentile não é suportado por este metric, ver comentário
// de metricCPUMillicores acima).
// Retorna mapa "namespace/workload" → valor agregado no período.
func (c *Client) queryWorkloadBatch(ctx context.Context, metric, aggregation, from string) (map[string]float64, error) {
	// resolution=inf agrega todo o período em um único ponto por série — bug real corrigido:
	// o ":last" no final (sobrando de quando isso tentava extrair "o último ponto" de uma série
	// multi-ponto) é ILEGAL combinado com resolution=inf ("Illegal transform operator: Usage of
	// last operator is only supported for resolutions other than `Inf`", confirmado ao vivo) —
	// redundante mesmo: com resolution=inf já existe só 1 ponto por série, não há "último" a
	// extrair.
	metricSelector := fmt.Sprintf(
		`%s:%s:splitBy("k8s.namespace.name","k8s.workload.name")`,
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
