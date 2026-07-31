package discovery

import (
	"context"
	"net/http"
)

// gcpTokenFunc, quando definido, fornece um Bearer token OAuth2 válido para autenticar
// requisições ao Google Cloud Monitoring API (usado pelo GMP — Prometheus-compatible endpoint dos
// clusters GKE).
//
// Não podemos importar internal/cloudprovider/gcp diretamente aqui: esse pacote depende
// transitivamente deste (internal/cloudprovider/gcp → internal/ai → internal/collectors →
// internal/monitoring/client → internal/monitoring/discovery) — importar na direção contrária
// fecharia um import cycle (confirmado experimentalmente).
//
// Quem liga os dois lados é internal/config.KubeConfigManager.DiscoverClusters, chamando
// SetGCPTokenFunc(gcpprovider.GetFreshGKEToken) sempre que há cluster GKE no kubeconfig — mesmo
// ponto onde o resto do bootstrap GKE (EnsureGKEAuthPlugin/LoadSavedGCPADC) já acontece.
// Idempotente: pode ser chamado várias vezes sem efeito colateral (DiscoverClusters roda a cada
// reload). Enquanto não for chamado (testes, ou nenhum cluster GKE no kubeconfig), fica nil e
// GCPAuthTransport simplesmente não injeta nenhum header — nunca panica.
var gcpTokenFunc func(ctx context.Context) string

// SetGCPTokenFunc registra a função usada para obter um access token OAuth2 do GCP.
func SetGCPTokenFunc(fn func(ctx context.Context) string) {
	gcpTokenFunc = fn
}

// GCPAuthTransport envolve `base` (ou http.DefaultTransport se nil) injetando
// "Authorization: Bearer <token>" em toda requisição feita através dele, usando gcpTokenFunc.
// Usado pelos clientes Prometheus quando PrometheusEndpoint.RequiresGCPAuth é true.
func GCPAuthTransport(base http.RoundTripper) http.RoundTripper {
	return &gcpAuthRoundTripper{base: base}
}

type gcpAuthRoundTripper struct {
	base http.RoundTripper
}

func (t *gcpAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if gcpTokenFunc == nil {
		return base.RoundTrip(req)
	}
	token := gcpTokenFunc(req.Context())
	if token == "" {
		return base.RoundTrip(req)
	}
	// Clona antes de mutar: nunca modificar o *http.Request original do chamador.
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+token)
	return base.RoundTrip(cloned)
}

// RequiresGCPAuth indica se a URL que GetPrometheusURL retornaria para este cluster é do Google
// Cloud Managed Service for Prometheus (GMP) — e portanto precisa de autenticação OAuth2 Bearer.
// Não faz nenhuma chamada de rede (mesmo custo de GetPrometheusURL) — para chamadores que só têm a
// URL em mãos (via GetPrometheusURL) e precisam decidir separadamente se autenticam o client.
func RequiresGCPAuth(cluster string) bool {
	if getPrometheusURLOverride(cluster) != "" {
		return false // override manual nunca é GMP
	}
	return buildGMPURL(cluster) != ""
}
