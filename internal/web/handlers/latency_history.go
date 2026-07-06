package handlers

import (
	"context"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/dynatrace"
	"k8s-hpa-manager/internal/monitoring/discovery"
	promclient "k8s-hpa-manager/internal/monitoring/prometheus"
)

// dtLatencyWindowDays é a janela de latência histórica consultada no DT — 7 dias dá contexto
// razoável sem ser tão largo quanto as janelas de tendência de custo do FinOps.
const dtLatencyWindowDays = 7

// LatencyHistoricalContext é o contexto complementar (best-effort, nunca bloqueia nem falha o
// teste ativo) mostrado ao lado do resultado — latência histórica já observada nesse alvo, via
// Dynatrace (primário) ou Prometheus (fallback), mesmo padrão MetricsSource já validado no FinOps.
// MetricsSource vazio = nenhuma fonte teve dado (não é erro — pode ser workload sem OneAgent/sem
// métricas HTTP expostas, ou a heurística de nome de Service abaixo ter errado o alvo).
type LatencyHistoricalContext struct {
	// Sem omitempty de propósito: um zero aqui é um valor real (latência de 0ms), não "campo
	// ausente" — com omitempty, Go remove o campo do JSON quando o valor é o zero-value, e o
	// frontend (que já trata null/undefined como "sem dado") passaria a exibir "sem dado" pra um
	// resultado que na verdade existe. Bug real: aconteceu quando MetricsSource vinha preenchido
	// mas P95Ms/P99Ms sumiam do JSON por terem ficado em 0 num teste anterior.
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	MetricsSource string  `json:"metrics_source"` // "dynatrace" | "prometheus" | ""
}

// fetchHistoricalLatencyContext busca o histórico DT/Prometheus do mesmo alvo do teste ativo.
//
// Ressalvas não validadas contra ambiente real (documentadas em LATENCY-METRICS-PLAN.md Fase 5,
// verificar antes de confiar cegamente no resultado em produção):
//   - `builtin:service.response.time` (DT) vive em entidades SERVICE; `GetWorkloadLatency`
//     reaproveita o mesmo `splitBy` por k8s.namespace.name/k8s.workload.name que já funciona pra
//     CPU/mem (entidades CLOUD_APPLICATION, ver finops_metrics.go) — não confirmamos se SERVICE
//     expõe essas dimensões da mesma forma.
//   - O nome do Service K8s usado nas duas fontes é adivinhado a partir do host da URL
//     (heurística simples), não resolvido de verdade contra o cluster.
//
// Em ambos os casos, se a suposição estiver errada o resultado é só "sem dado" (MetricsSource
// vazio) — nunca um valor errado sendo exibido como se fosse confiável.
func (h *LatencyTestHandler) fetchHistoricalLatencyContext(ctx context.Context, cluster, namespace, targetURL string) LatencyHistoricalContext {
	logger := log.With().Str("component", "latency-historical-context").Logger()

	serviceName := guessServiceNameFromURL(targetURL)
	if serviceName == "" {
		logger.Debug().Str("target_url", targetURL).Msg("não foi possível extrair nome de serviço da URL")
		return LatencyHistoricalContext{}
	}
	logger.Debug().Str("service", serviceName).Str("namespace", namespace).Str("cluster", cluster).Msg("iniciando busca de contexto histórico")

	if h.dtTokenStore != nil {
		if dtURL, dtToken, ok := h.dtTokenStore.GetDynatraceConfig(); ok {
			if dtClient, err := dynatrace.NewClient(dtURL, dtToken); err == nil {
				wl, err := dtClient.GetSingleWorkloadLatency(ctx, namespace, serviceName, dtLatencyWindowDays)
				if err != nil {
					logger.Debug().Err(err).Msg("DT: falha ao consultar latência")
				} else if wl == nil {
					logger.Debug().Msg("DT: sem dado pra esse workload")
				} else {
					logger.Debug().Float64("p95", wl.P95Ms).Float64("p99", wl.P99Ms).Msg("DT: dado encontrado")
					return LatencyHistoricalContext{P95Ms: wl.P95Ms, P99Ms: wl.P99Ms, MetricsSource: "dynatrace"}
				}
			} else {
				logger.Debug().Err(err).Msg("DT: falha ao criar client")
			}
		} else {
			logger.Debug().Msg("DT: não configurado (sem token)")
		}
	} else {
		logger.Debug().Msg("DT: dtTokenStore é nil")
	}

	promURL := discovery.GetPrometheusURL(cluster)
	logger.Debug().Str("prometheus_url", promURL).Msg("tentando Prometheus")
	promC, err := promclient.NewClient(cluster, promURL)
	if err != nil {
		logger.Debug().Err(err).Msg("Prometheus: falha ao criar client")
		return LatencyHistoricalContext{}
	}
	// O client usa "lazy connection" (ver client.go) — Query()/QueryRange() recusam rodar
	//("client not connected") até TestConnection() marcar connected=true explicitamente. Sem
	// isso aqui, TODA consulta falhava silenciosamente mesmo com o Prometheus acessível.
	if err := promC.TestConnection(ctx); err != nil {
		logger.Debug().Err(err).Msg("Prometheus: falha no teste de conexão")
		return LatencyHistoricalContext{}
	}
	p95, err95 := promC.GetP95Latency(ctx, namespace, serviceName)
	p99, err99 := promC.GetP99Latency(ctx, namespace, serviceName)
	logger.Debug().
		Float64("p95", p95).AnErr("err95", err95).
		Float64("p99", p99).AnErr("err99", err99).
		Msg("Prometheus: resultado da consulta")
	if err95 != nil && err99 != nil {
		return LatencyHistoricalContext{}
	}
	return LatencyHistoricalContext{P95Ms: p95, P99Ms: p99, MetricsSource: "prometheus"}
}

// guessServiceNameFromURL extrai um candidato a nome de Service K8s a partir do host da URL —
// heurística: primeiro label DNS antes do primeiro ponto (ex: "meu-service.ns.svc.cluster.local"
// → "meu-service"). Não valida contra o cluster real — é só um palpite pro contexto histórico
// complementar, nunca afeta o teste ativo em si (que usa a URL exata, sem heurística nenhuma).
//
// Aceita host sem esquema (ex: "meu-host.com", sem "http://" na frente) — bug real encontrado
// testando no navegador: `url.Parse` sem esquema trata a string inteira como Path, não Host, e
// `u.Hostname()` volta vazio. Nesse caso o campo "URL alvo" foi digitado como host cru; prefixamos
// "http://" só pra conseguir extrair o host, o teste ativo em si não usa esse valor corrigido.
func guessServiceNameFromURL(rawURL string) string {
	parseTarget := rawURL
	if !strings.Contains(parseTarget, "://") {
		parseTarget = "http://" + parseTarget
	}
	u, err := url.Parse(parseTarget)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := u.Hostname()
	if idx := strings.Index(host, "."); idx > 0 {
		return host[:idx]
	}
	return host
}
