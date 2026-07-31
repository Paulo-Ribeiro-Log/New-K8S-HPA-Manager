package certificates

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// resolveIngressHosts retorna os hosts (Ingress.Spec.TLS[].Hosts) de Ingresses em `namespace` que
// referenciam secretName — usado pelo handshake TLS (tls_dial_enrich.go) como uma das duas fontes
// de hosts candidatos (a outra é Gateway API, ver gateway_hosts.go). Implementação independente de
// crossRefIngresses (scanner.go) — mesma ideia, mas não reaproveita o código existente de
// propósito: crossRefIngresses já roda dentro do fluxo de scan em lote (N clusters em paralelo) e
// não vale o risco de acoplar um caminho novo e pontual a esse código já testado em produção.
// Best-effort: qualquer erro ao listar retorna nil, nunca propaga erro (mesmo espírito do restante
// da Fase 4 — ausência de Ingress apontando pro Secret é o caso normal, não uma falha).
func resolveIngressHosts(ctx context.Context, clientset kubernetes.Interface, namespace, secretName string) []string {
	ingresses, err := clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var hosts []string
	for _, ing := range ingresses.Items {
		for _, tls := range ing.Spec.TLS {
			if tls.SecretName != secretName {
				continue
			}
			hosts = append(hosts, tls.Hosts...)
		}
	}
	return hosts
}
