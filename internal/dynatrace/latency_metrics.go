package dynatrace

import (
	"context"
	"fmt"
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

// GetWorkloadLatency consulta a latência (P95/P99) de todos os workloads monitorados pelo DT no
// período dado, retornando um mapa "namespace/workload" → WorkloadLatency. Reaproveita
// queryWorkloadBatch (mesmo helper de GetAllWorkloadMetrics em finops_metrics.go) — só 2 queries
// (P95 + P99), independente de quantos workloads existirem.
//
// Ressalva não validada contra ambiente real (ver LATENCY-METRICS-PLAN.md Fase 5):
// `builtin:service.response.time` vive em entidades SERVICE, enquanto GetAllWorkloadMetrics
// (CPU/mem) faz o mesmo splitBy em entidades CLOUD_APPLICATION — que carregam
// k8s.namespace.name/k8s.workload.name nativamente. Não confirmamos se SERVICE expõe essas
// dimensões da mesma forma. Se não expuser, esta função simplesmente não encontra dados (mapa
// vazio, sem erro) — degradação segura, nunca retorna valor errado.
func (c *Client) GetWorkloadLatency(ctx context.Context, windowDays int) (map[string]WorkloadLatency, error) {
	ctx, cancel := context.WithTimeout(ctx, dtQueryTimeout)
	defer cancel()

	from := fmt.Sprintf("now-%dd", windowDays)

	p95, err := c.queryWorkloadBatch(ctx, metricResponseTime, "percentile(95)", from)
	if err != nil {
		return nil, fmt.Errorf("DT latency p95: %w", err)
	}
	p99, err := c.queryWorkloadBatch(ctx, metricResponseTime, "percentile(99)", from)
	if err != nil {
		return nil, fmt.Errorf("DT latency p99: %w", err)
	}

	keys := make(map[string]struct{}, len(p95)+len(p99))
	for k := range p95 {
		keys[k] = struct{}{}
	}
	for k := range p99 {
		keys[k] = struct{}{}
	}

	result := make(map[string]WorkloadLatency, len(keys))
	for k := range keys {
		result[k] = WorkloadLatency{
			P95Ms: p95[k] * responseTimeUsToMs,
			P99Ms: p99[k] * responseTimeUsToMs,
		}
	}
	return result, nil
}

// GetSingleWorkloadLatency é um atalho de GetWorkloadLatency pra quando só interessa UM workload
// específico (ex: contexto do teste de latência sob demanda) — evita o chamador lidar com o mapa
// completo. Ainda faz as mesmas 2 queries de todo o cluster por baixo (não existe um jeito mais
// barato de pedir só 1 workload nessa API do DT); aceitável porque roda em paralelo e best-effort,
// nunca bloqueia o teste ativo. Retorna (nil, nil) quando o workload não tem dado — não é erro.
func (c *Client) GetSingleWorkloadLatency(ctx context.Context, namespace, workload string, windowDays int) (*WorkloadLatency, error) {
	all, err := c.GetWorkloadLatency(ctx, windowDays)
	if err != nil {
		return nil, err
	}
	wl, ok := all[namespace+"/"+workload]
	if !ok {
		return nil, nil
	}
	return &wl, nil
}
