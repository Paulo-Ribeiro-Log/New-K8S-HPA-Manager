package handlers

import (
	"context"
	"net/url"
	"strings"

	"k8s-hpa-manager/internal/monitoring/latencylookup"
)

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

// fetchHistoricalLatencyContext busca o histórico DT/Prometheus do mesmo alvo do teste ativo —
// wrapper fino sobre latencylookup.Fetch (mesma lógica reaproveitada pela correlação de breach de
// latência no Health Check, ver LATENCY-METRICS-PLAN.md Fase 7) que só adiciona a heurística de
// nome de Service a partir da URL, específica desse fluxo (o Health Check já tem o nome real do
// workload, não precisa adivinhar).
func (h *LatencyTestHandler) fetchHistoricalLatencyContext(ctx context.Context, cluster, namespace, targetURL string) LatencyHistoricalContext {
	serviceName := guessServiceNameFromURL(targetURL)
	if serviceName == "" {
		return LatencyHistoricalContext{}
	}

	var dtURL, dtToken string
	if h.dtTokenStore != nil {
		dtURL, dtToken, _ = h.dtTokenStore.GetDynatraceConfig()
	}

	result := latencylookup.Fetch(ctx, dtURL, dtToken, cluster, namespace, serviceName)
	return LatencyHistoricalContext{P95Ms: result.P95Ms, P99Ms: result.P99Ms, MetricsSource: result.Source}
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
