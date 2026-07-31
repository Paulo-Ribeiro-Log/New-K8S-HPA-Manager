package certificates

import (
	"context"
	"fmt"
)

// ResolveHostsForSecret resolve os hostnames públicos que servem um Secret TLS específico,
// combinando Ingress (resolveIngressHosts) e Gateway API (resolveGatewayHosts). Usado pelo
// handshake TLS direto (tls_dial_enrich.go) como alternativa universal ao Prometheus/ingress-nginx
// — funciona independente de qual componente termina o TLS.
func (s *Scanner) ResolveHostsForSecret(ctx context.Context, cluster, namespace, secretName string) ([]string, error) {
	clientset, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter client para %s: %w", cluster, err)
	}

	hosts := resolveIngressHosts(ctx, clientset, namespace, secretName)
	hosts = append(hosts, resolveGatewayHosts(s.kubeManager, cluster, namespace, secretName)...)
	return dedupStrings(hosts), nil
}

// dedupStrings remove duplicatas preservando a ordem de primeira ocorrência.
func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
