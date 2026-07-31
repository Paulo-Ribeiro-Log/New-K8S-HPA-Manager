package certificates

import (
	"fmt"
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
	// concreto) como candidato — ver hostsForListener.
	bindings, _ := kubeclient.ListHTTPRouteBindings(cluster, authArgs)

	var hosts []string
	for _, listener := range matched {
		hosts = append(hosts, hostsForListener(listener, bindings)...)
	}

	return dedupStrings(hosts)
}

// hostsForListener resolve os hostnames servidos por UM listener já identificado, cruzando com as
// HTTPRoutes que o referenciam — compartilhado entre resolveGatewayHosts (path on-demand, Fase 7,
// um listener por chamada) e Scanner.crossRefGateways (path do scan em lote, Fase 8, todos os
// listeners do cluster de uma vez, mesmas bindings reaproveitadas).
func hostsForListener(listener kubeclient.GatewayListenerCert, bindings []kubeclient.HTTPRouteBinding) []string {
	var hosts []string
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
	return hosts
}

// crossRefGateways cruza certificados com Gateway API (Gateway+HTTPRoute), análogo a
// crossRefIngresses (scanner.go) mas pra Gateway API — roda 1x por cluster dentro de scanCluster
// (não por secret, ao contrário do path on-demand resolveGatewayHosts da Fase 7, que multiplicaria
// subprocessos kubectl por certificado). Popula certs[i].UsedByGateways e retorna o mapa de
// host -> owners usado por detectHostConflicts (config_issues.go), pra ser combinado com os
// owners de Ingress antes de rodar a detecção de conflito (B). CRD Gateway API ausente/erro de
// kubectl → retorna nil (caso normal em cluster que ainda só usa Ingress, não um erro).
func (s *Scanner) crossRefGateways(cluster string, certs []CertificateInfo) map[string][]hostOwner {
	authArgs, cleanup, err := s.kubeManager.KubectlAuthArgs(cluster)
	if err != nil {
		return nil
	}
	defer cleanup()

	listeners, err := kubeclient.ListGatewayListenerCerts(cluster, authArgs)
	if err != nil {
		return nil
	}
	bindings, _ := kubeclient.ListHTTPRouteBindings(cluster, authArgs) // best-effort

	gatewayMap := make(map[string][]GatewayRef)
	hostOwners := make(map[string][]hostOwner)

	for _, l := range listeners {
		hosts := hostsForListener(l, bindings)
		for _, secretName := range l.CertificateRefs {
			key := fmt.Sprintf("%s/%s", l.GatewayNamespace, secretName)
			gatewayMap[key] = append(gatewayMap[key], GatewayRef{
				Name:      l.GatewayName,
				Namespace: l.GatewayNamespace,
				Hosts:     hosts,
			})

			for _, h := range hosts {
				hostOwners[h] = append(hostOwners[h], hostOwner{
					Kind:       "gateway",
					Namespace:  l.GatewayNamespace,
					Name:       l.GatewayName,
					SecretName: secretName,
				})
			}
		}
	}

	for i := range certs {
		key := fmt.Sprintf("%s/%s", certs[i].Namespace, certs[i].SecretName)
		if refs, ok := gatewayMap[key]; ok {
			certs[i].UsedByGateways = refs
		}
	}

	return hostOwners
}

// hostnameMatchesListener implementa o casamento de hostname da Gateway API: listenerHost pode ser
// "" (sem restrição, aceita qualquer hostname de rota) ou um wildcard de único nível
// ("*.example.com") — matching por sufixo, sem necessidade de lib de glob. Hostname concreto exige
// igualdade exata. Delega para matchesWildcardHost (hostmatch.go), compartilhada com
// certSANCoversHost (config_issues.go).
func hostnameMatchesListener(listenerHost, routeHost string) bool {
	return matchesWildcardHost(listenerHost, routeHost)
}
