package kubernetes

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// GatewayListenerCert descreve um listener TLS de um Gateway (Gateway API) junto com os Secrets
// que ele referencia — usado por internal/certificates/gateway_hosts.go para resolver hostnames
// públicos que servem um Secret TLS específico (handshake TLS direto, Fase 4).
type GatewayListenerCert struct {
	GatewayNamespace string
	GatewayName      string
	ListenerName     string
	Hostname         string   // "" = sem restrição de hostname (aceita qualquer)
	CertificateRefs  []string // nomes de Secret no mesmo namespace do Gateway
}

// HTTPRouteBinding descreve o vínculo de uma HTTPRoute com um Gateway/listener + os hostnames que
// ela expõe.
type HTTPRouteBinding struct {
	RouteNamespace  string
	ParentNamespace string // default = RouteNamespace quando parentRefs[].namespace vier vazio
	ParentName      string
	SectionName     string // "" = qualquer listener do Gateway
	Hostnames       []string
}

type gatewayListJSON struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			Listeners []struct {
				Name     string `json:"name"`
				Hostname string `json:"hostname"`
				TLS      struct {
					CertificateRefs []struct {
						Kind string `json:"kind"`
						Name string `json:"name"`
					} `json:"certificateRefs"`
				} `json:"tls"`
			} `json:"listeners"`
		} `json:"spec"`
	} `json:"items"`
}

type httpRouteListJSON struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			ParentRefs []struct {
				Namespace   string `json:"namespace"`
				Name        string `json:"name"`
				SectionName string `json:"sectionName"`
			} `json:"parentRefs"`
			Hostnames []string `json:"hostnames"`
		} `json:"spec"`
	} `json:"items"`
}

// ListGatewayListenerCerts roda `kubectl get gateways.gateway.networking.k8s.io -o json
// --all-namespaces` e extrai spec.listeners[].{hostname, tls.certificateRefs[]}. CRD ausente ou
// erro de kubectl retorna (nil, err) — o chamador (internal/certificates.resolveGatewayHosts)
// trata isso como "não dá pra checar por esse caminho", nunca como falha fatal — mesmo espírito de
// GatewayHandler.List (campo not_installed).
func ListGatewayListenerCerts(cluster string, authArgs []string) ([]GatewayListenerCert, error) {
	selector := buildResourceSelector("gateways", "gateway.networking.k8s.io")
	if authArgs == nil {
		authArgs = []string{"--context", cluster}
	}
	args := append([]string{"get", selector}, authArgs...)
	args = append(args, "-o", "json", "--all-namespaces")

	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl get %s failed: %w - %s", selector, err, string(out))
	}

	var parsed gatewayListJSON
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse gateway list: %w", err)
	}

	var result []GatewayListenerCert
	for _, item := range parsed.Items {
		for _, l := range item.Spec.Listeners {
			glc := GatewayListenerCert{
				GatewayNamespace: item.Metadata.Namespace,
				GatewayName:      item.Metadata.Name,
				ListenerName:     l.Name,
				Hostname:         l.Hostname,
			}
			for _, ref := range l.TLS.CertificateRefs {
				// kind vazio ou "Secret" — outros kinds (ex: ConfigMap) não se aplicam a esta
				// checagem de propagação de certificado.
				if ref.Kind == "" || ref.Kind == "Secret" {
					glc.CertificateRefs = append(glc.CertificateRefs, ref.Name)
				}
			}
			if len(glc.CertificateRefs) > 0 {
				result = append(result, glc)
			}
		}
	}
	return result, nil
}

// ListHTTPRouteBindings roda o mesmo padrão para httproutes.gateway.networking.k8s.io, extraindo
// spec.parentRefs[]/spec.hostnames[]. Mesmo contrato de erro de ListGatewayListenerCerts.
func ListHTTPRouteBindings(cluster string, authArgs []string) ([]HTTPRouteBinding, error) {
	selector := buildResourceSelector("httproutes", "gateway.networking.k8s.io")
	if authArgs == nil {
		authArgs = []string{"--context", cluster}
	}
	args := append([]string{"get", selector}, authArgs...)
	args = append(args, "-o", "json", "--all-namespaces")

	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl get %s failed: %w - %s", selector, err, string(out))
	}

	var parsed httpRouteListJSON
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse httproute list: %w", err)
	}

	var result []HTTPRouteBinding
	for _, item := range parsed.Items {
		for _, ref := range item.Spec.ParentRefs {
			parentNS := ref.Namespace
			if parentNS == "" {
				// Regra da Gateway API: parentRefs sem namespace explícito refere-se ao mesmo
				// namespace da HTTPRoute.
				parentNS = item.Metadata.Namespace
			}
			result = append(result, HTTPRouteBinding{
				RouteNamespace:  item.Metadata.Namespace,
				ParentNamespace: parentNS,
				ParentName:      ref.Name,
				SectionName:     ref.SectionName,
				Hostnames:       item.Spec.Hostnames,
			})
		}
	}
	return result, nil
}
