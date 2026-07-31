package certificates

import (
	"strings"

	"k8s-hpa-manager/internal/config"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// resolveGatewayHosts consulta Gateway API (Gateway + HTTPRoute) via kubectl e retorna os
// hostnames através dos quais namespace/secretName é servido. CRD ausente, kubectl indisponível,
// ou nenhum listener referenciando o Secret → retorna nil (caso normal em clusters que ainda só
// usam Ingress, não um erro) — usado pelo handshake TLS (tls_dial_enrich.go) como a segunda fonte
// de hosts candidatos (a outra é Ingress, ver ingress_hosts.go).
func resolveGatewayHosts(km *config.KubeConfigManager, cluster, namespace, secretName string) []string {
	authArgs, cleanup, err := km.KubectlAuthArgs(cluster)
	if err != nil {
		return nil
	}
	defer cleanup()

	listeners, err := kubeclient.ListGatewayListenerCerts(cluster, authArgs)
	if err != nil {
		// CRD Gateway API não instalada neste cluster, ou erro de kubectl — caso normal, não
		// bloqueia o restante da checagem (o chamador ainda tenta Ingress).
		return nil
	}

	var matched []kubeclient.GatewayListenerCert
	for _, l := range listeners {
		if l.GatewayNamespace != namespace {
			continue
		}
		for _, ref := range l.CertificateRefs {
			if ref == secretName {
				matched = append(matched, l)
				break
			}
		}
	}
	if len(matched) == 0 {
		return nil
	}

	// HTTPRoute é best-effort: se falhar, ainda aproveitamos o hostname do listener (quando
	// concreto) como candidato — ver passo abaixo.
	bindings, _ := kubeclient.ListHTTPRouteBindings(cluster, authArgs)

	var hosts []string
	for _, listener := range matched {
		routeMatchedListener := false
		for _, b := range bindings {
			if b.ParentNamespace != listener.GatewayNamespace || b.ParentName != listener.GatewayName {
				continue
			}
			if b.SectionName != "" && b.SectionName != listener.ListenerName {
				continue
			}
			for _, routeHost := range b.Hostnames {
				if hostnameMatchesListener(listener.Hostname, routeHost) {
					hosts = append(hosts, routeHost)
					routeMatchedListener = true
				}
			}
		}
		// Sem nenhuma HTTPRoute casando e o listener já tem um hostname concreto (não vazio, não
		// wildcard) — o LB termina TLS ali independente de existir HTTPRoute apontando pra ele.
		if !routeMatchedListener && listener.Hostname != "" && !strings.HasPrefix(listener.Hostname, "*.") {
			hosts = append(hosts, listener.Hostname)
		}
	}

	return dedupStrings(hosts)
}

// hostnameMatchesListener implementa o casamento de hostname da Gateway API: listenerHost pode ser
// "" (sem restrição, aceita qualquer hostname de rota) ou um wildcard de único nível
// ("*.example.com") — matching por sufixo, sem necessidade de lib de glob. Hostname concreto exige
// igualdade exata.
func hostnameMatchesListener(listenerHost, routeHost string) bool {
	if listenerHost == "" {
		return true
	}
	if strings.HasPrefix(listenerHost, "*.") {
		suffix := listenerHost[1:] // ".example.com"
		return strings.HasSuffix(routeHost, suffix) && routeHost != suffix[1:]
	}
	return listenerHost == routeHost
}
