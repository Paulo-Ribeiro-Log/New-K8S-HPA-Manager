// Package latencylookup extrai a busca de latência histórica (Dynatrace → Prometheus, mesmo
// padrão MetricsSource do FinOps) usada tanto pelo contexto complementar do Teste de Latência sob
// Demanda (Fase 5) quanto pela correlação de breach de latência no Health Check (Fase 7) — ver
// LATENCY-METRICS-PLAN.md. Extraído pra pacote próprio pra não duplicar a lógica DT→Prometheus
// entre `internal/web/handlers` e `internal/healthcheck` (que não deve depender de handlers).
package latencylookup

import (
	"context"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/monitoring/discovery"
	promclient "k8s-hpa-manager/internal/monitoring/prometheus"
)

// DefaultWindowDays é a janela de latência histórica consultada no DT — 7 dias dá contexto
// razoável sem ser tão largo quanto as janelas de tendência de custo do FinOps.
const DefaultWindowDays = 7

// Result é o resultado da busca — best-effort, nunca erro pro chamador (ver Fetch).
// MetricsSource vazio (campo Source) = nenhuma fonte teve dado, não é erro.
type Result struct {
	// Sem omitempty de propósito: um zero aqui é um valor real (latência de 0ms), não "campo
	// ausente" — bug real encontrado na Fase 5 (ver CLAUDE.md), corrigido nos dois chamadores.
	P95Ms  float64
	P99Ms  float64
	Source string // "dynatrace" | "prometheus" | ""
}

// Fetch busca a latência histórica (P95/P99, ms) de um Service K8s — tenta Dynatrace primeiro
// (se dtURL/dtToken não vazios), Prometheus como fallback. Nunca retorna erro: falha em qualquer
// etapa (client, conexão, sem dado no período) resulta em Result{} (Source vazio).
//
// Ressalvas confirmadas contra ambiente real (ver LATENCY-METRICS-PLAN.md Fase 5/7):
//   - Dynatrace: busca a entidade SERVICE por nome (`workload`), não resolve k8s.namespace.name de
//     verdade (essa dimensão não existe na métrica builtin:service.response.time) — se houver mais
//     de uma entidade com nome parecido, fica com a primeira que a API devolver.
//   - O Dynatrace configurado num projeto pode simplesmente não monitorar o cluster/namespace
//     testado (sem OneAgent) — isso é esperado, tratado como "sem dado", nunca erro visível.
func Fetch(ctx context.Context, dtURL, dtToken, cluster, namespace, workload string) Result {
	logger := log.With().Str("component", "latencylookup").Logger()

	if dtURL != "" && dtToken != "" {
		if dtClient, err := dynatrace.NewClient(dtURL, dtToken); err == nil {
			wl, err := dtClient.GetSingleWorkloadLatency(ctx, namespace, workload, DefaultWindowDays)
			if err != nil {
				logger.Debug().Err(err).Str("workload", workload).Msg("DT: falha ao consultar latência")
			} else if wl == nil {
				logger.Debug().Str("workload", workload).Msg("DT: sem dado pra esse workload")
			} else {
				logger.Debug().Str("workload", workload).Float64("p95", wl.P95Ms).Float64("p99", wl.P99Ms).Msg("DT: dado encontrado")
				return Result{P95Ms: wl.P95Ms, P99Ms: wl.P99Ms, Source: "dynatrace"}
			}
		} else {
			logger.Debug().Err(err).Msg("DT: falha ao criar client")
		}
	}

	promURL := discovery.GetPrometheusURL(cluster)
	promC, err := promclient.NewClient(cluster, promURL)
	if err != nil {
		logger.Debug().Err(err).Msg("Prometheus: falha ao criar client")
		return Result{}
	}
	// O client usa "lazy connection" (ver internal/monitoring/prometheus/client.go) —
	// Query()/QueryRange() recusam rodar ("client not connected") até TestConnection() marcar
	// connected=true explicitamente. Sem isso aqui, TODA consulta falha silenciosamente mesmo com
	// o Prometheus acessível — bug real encontrado na Fase 5.
	if err := promC.TestConnection(ctx); err != nil {
		logger.Debug().Err(err).Msg("Prometheus: falha no teste de conexão")
		return Result{}
	}
	p95, err95 := promC.GetP95Latency(ctx, namespace, workload)
	p99, err99 := promC.GetP99Latency(ctx, namespace, workload)
	logger.Debug().
		Str("workload", workload).
		Float64("p95", p95).AnErr("err95", err95).
		Float64("p99", p99).AnErr("err99", err99).
		Msg("Prometheus: resultado da consulta")
	if err95 != nil && err99 != nil {
		return Result{}
	}
	return Result{P95Ms: p95, P99Ms: p99, Source: "prometheus"}
}
