package handlers

import (
	"context"
	"net/url"
	"strings"

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
	P95Ms         float64 `json:"p95_ms,omitempty"`
	P99Ms         float64 `json:"p99_ms,omitempty"`
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
	serviceName := guessServiceNameFromURL(targetURL)
	if serviceName == "" {
		return LatencyHistoricalContext{}
	}

	if h.dtTokenStore != nil {
		if dtURL, dtToken, ok := h.dtTokenStore.GetDynatraceConfig(); ok {
			if dtClient, err := dynatrace.NewClient(dtURL, dtToken); err == nil {
				if wl, err := dtClient.GetSingleWorkloadLatency(ctx, namespace, serviceName, dtLatencyWindowDays); err == nil && wl != nil {
					return LatencyHistoricalContext{P95Ms: wl.P95Ms, P99Ms: wl.P99Ms, MetricsSource: "dynatrace"}
				}
			}
		}
	}

	promURL := discovery.GetPrometheusURL(cluster)
	promC, err := promclient.NewClient(cluster, promURL)
	if err != nil {
		return LatencyHistoricalContext{}
	}
	p95, err95 := promC.GetP95Latency(ctx, namespace, serviceName)
	p99, err99 := promC.GetP99Latency(ctx, namespace, serviceName)
	if err95 != nil && err99 != nil {
		return LatencyHistoricalContext{}
	}
	return LatencyHistoricalContext{P95Ms: p95, P99Ms: p99, MetricsSource: "prometheus"}
}

// guessServiceNameFromURL extrai um candidato a nome de Service K8s a partir do host da URL —
// heurística: primeiro label DNS antes do primeiro ponto (ex: "meu-service.ns.svc.cluster.local"
// → "meu-service"). Não valida contra o cluster real — é só um palpite pro contexto histórico
// complementar, nunca afeta o teste ativo em si (que usa a URL exata, sem heurística nenhuma).
func guessServiceNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := u.Hostname()
	if idx := strings.Index(host, "."); idx > 0 {
		return host[:idx]
	}
	return host
}
