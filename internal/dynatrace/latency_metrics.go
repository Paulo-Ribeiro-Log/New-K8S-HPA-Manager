package dynatrace

import (
	"context"
	"fmt"
	"net/url"
)

// metricResponseTime é o metric selector base de latência de SERVICE entities — valores brutos
// vêm em microssegundos (mesma unidade usada em serviceMetricDefs, metrics.go), por isso o
// fator de conversão responseTimeUsToMs abaixo.
const (
	metricResponseTime = "builtin:service.response.time"
	responseTimeUsToMs = 0.001
)

// WorkloadLatency agrega P95/P99 de latência (ms) de um workload K8s via Dynatrace.
type WorkloadLatency struct {
	P95Ms float64
	P99Ms float64
}

// findServiceEntityID busca a entidade SERVICE cujo nome contém `namePattern`, devolvendo a
// primeira correspondência (heurística, mesmo espírito de guessServiceNameFromURL no handler —
// nunca resolve o Service K8s de verdade, só um palpite razoável pro contexto histórico).
//
// Por que por nome e não por k8s.namespace.name/k8s.workload.name: confirmado contra a API real
// (GET /api/v2/metrics/builtin:service.response.time) que a ÚNICA dimensão dessa métrica é
// `dt.entity.service` (tipo ENTITY) — SERVICE entities não carregam as dimensões k8s nativamente
// (diferente de CLOUD_APPLICATION, usada em GetAllWorkloadMetrics pra CPU/mem). A abordagem antiga
// de splitBy("k8s.namespace.name","k8s.workload.name") sempre falhava com 400 nessa métrica.
func (c *Client) findServiceEntityID(ctx context.Context, namePattern string) (string, error) {
	params := url.Values{
		"entitySelector": {fmt.Sprintf(`type(SERVICE),entityName.contains("%s")`, namePattern)},
		"pageSize":       {"1"},
	}
	var response struct {
		Entities []struct {
			EntityID string `json:"entityId"`
		} `json:"entities"`
	}
	if err := c.get(ctx, "entities", params, &response); err != nil {
		return "", err
	}
	if len(response.Entities) == 0 {
		return "", nil
	}
	return response.Entities[0].EntityID, nil
}

// queryServicePercentile consulta um percentil de builtin:service.response.time (µs) filtrado
// pra UMA entidade SERVICE específica via entitySelector — sem splitBy, já que só há 1 entidade.
func (c *Client) queryServicePercentile(ctx context.Context, entityID, aggregation, from string) (float64, error) {
	params := url.Values{
		"metricSelector": {fmt.Sprintf(`%s:%s`, metricResponseTime, aggregation)},
		"entitySelector": {fmt.Sprintf(`type(SERVICE),entityId(%s)`, entityID)},
		"from":           {from},
		"to":             {"now"},
		"resolution":     {"inf"},
	}
	var response struct {
		Result []struct {
			Data []struct {
				Values []float64 `json:"values"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := c.get(ctx, "metrics/query", params, &response); err != nil {
		return 0, err
	}
	if len(response.Result) == 0 || len(response.Result[0].Data) == 0 {
		return 0, fmt.Errorf("sem série pra entidade %s", entityID)
	}
	for _, v := range response.Result[0].Data[0].Values {
		if v > 0 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("valor zero/NaN pra entidade %s no período", entityID)
}

// GetSingleWorkloadLatency busca a latência histórica (P95/P99, ms) de um Service K8s via
// Dynatrace, pro contexto complementar do teste de latência sob demanda.
//
// `namespace` não é usado pra filtrar (a métrica não expõe k8s.namespace.name, ver
// findServiceEntityID) — só o nome do serviço entra na busca por entidade. Se houver mais de uma
// entidade SERVICE com nome parecido (comum: variações PF/CB/EX por squad, ou README instances),
// fica com a primeira que a API devolver — é best-effort, o teste ativo em si nunca depende disso.
//
// Retorna (nil, nil) — não erro — quando: não existe entidade SERVICE com esse nome (workload sem
// OneAgent, ou fora do escopo desse tenant DT), ou existe mas sem dado no período. Isso é comum:
// confirmado contra o ambiente real que o Dynatrace configurado neste projeto não monitora todos
// os clusters/namespaces (zero entidades CLOUD_APPLICATION nos testados) — não é bug, é a
// realidade da cobertura de monitoramento desse tenant.
func (c *Client) GetSingleWorkloadLatency(ctx context.Context, namespace, workload string, windowDays int) (*WorkloadLatency, error) {
	ctx, cancel := context.WithTimeout(ctx, dtQueryTimeout)
	defer cancel()

	entityID, err := c.findServiceEntityID(ctx, workload)
	if err != nil {
		return nil, fmt.Errorf("DT: busca de entidade SERVICE: %w", err)
	}
	if entityID == "" {
		return nil, nil
	}

	from := fmt.Sprintf("now-%dd", windowDays)
	p95, err95 := c.queryServicePercentile(ctx, entityID, "percentile(95)", from)
	p99, err99 := c.queryServicePercentile(ctx, entityID, "percentile(99)", from)
	if err95 != nil && err99 != nil {
		return nil, nil
	}
	return &WorkloadLatency{
		P95Ms: p95 * responseTimeUsToMs,
		P99Ms: p99 * responseTimeUsToMs,
	}, nil
}
