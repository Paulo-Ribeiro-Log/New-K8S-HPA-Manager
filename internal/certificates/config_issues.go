package certificates

import (
	"fmt"
	"strings"
)

// certSANCoversHost reporta se algum SAN do certificado cobre host (wildcard-aware, via
// matchesWildcardHost em hostmatch.go).
func certSANCoversHost(sans []string, host string) bool {
	for _, san := range sans {
		if matchesWildcardHost(san, host) {
			return true
		}
	}
	return false
}

// hostOwner identifica quem reivindica um host — Ingress ou Gateway — usado só internamente pra
// detecção de conflito (B), nunca persistido/serializado numa resposta.
type hostOwner struct {
	Kind         string // "ingress" | "gateway"
	Namespace    string
	Name         string
	SecretName   string
	IngressClass string // vazio para Gateway, ou Ingress sem classe definida
}

// detectHostConflicts recebe host -> owners que o reivindicam e retorna, por secretKey
// ("namespace/secretName") e depois por host, a mensagem de warning gerada — indexado por host
// (não uma lista solta) pra o chamador conseguir anexar o warning só ao HostIssue do host
// realmente conflitante, sem espalhar a mensagem pros outros hosts do mesmo Ingress/Gateway.
func detectHostConflicts(hostOwners map[string][]hostOwner) map[string]map[string]string {
	result := make(map[string]map[string]string)

	for host, owners := range hostOwners {
		for _, group := range groupConflictingOwners(owners) {
			distinctSecrets := make(map[string]bool)
			for _, o := range group {
				distinctSecrets[o.Namespace+"/"+o.SecretName] = true
			}
			if len(distinctSecrets) < 2 {
				continue
			}

			parts := make([]string, 0, len(group))
			for _, o := range group {
				kindLabel := "Ingress"
				if o.Kind == "gateway" {
					kindLabel = "Gateway"
				}
				parts = append(parts, fmt.Sprintf("%s %s/%s (secret %s)", kindLabel, o.Namespace, o.Name, o.SecretName))
			}
			msg := fmt.Sprintf(
				"Host %q reivindicado por múltiplos objetos com Secrets diferentes: %s — comportamento indeterminado, depende de qual o controller processa por último",
				host, strings.Join(parts, "; "),
			)

			for _, o := range group {
				key := o.Namespace + "/" + o.SecretName
				if result[key] == nil {
					result[key] = make(map[string]string)
				}
				result[key][host] = msg
			}
		}
	}

	return result
}

// groupConflictingOwners particiona os owners de UM host em grupos candidatos a conflito.
// Regra: Ingresses são agrupados por IngressClass (evita falso-positivo em clusters com múltiplos
// ingress-controllers coexistindo de propósito, ex: nginx-interno + nginx-externo); qualquer
// grupo que tenha ao menos um owner Gateway ignora a separação por classe — é justamente o
// cenário de migração incompleta (Ingress legado + Gateway novo disputando o mesmo host) que vale
// a pena flagar, sem uma "classe" em comum pra usar como critério de supressão.
func groupConflictingOwners(owners []hostOwner) [][]hostOwner {
	var hasGateway bool
	byClass := make(map[string][]hostOwner)
	for _, o := range owners {
		if o.Kind == "gateway" {
			hasGateway = true
		}
		byClass[o.IngressClass] = append(byClass[o.IngressClass], o)
	}

	if hasGateway {
		return [][]hostOwner{owners}
	}

	groups := make([][]hostOwner, 0, len(byClass))
	for _, g := range byClass {
		groups = append(groups, g)
	}
	return groups
}

// mergeHostOwners combina os mapas host -> owners de Ingress (crossRefIngresses) e Gateway API
// (crossRefGateways) num único mapa, pra detectHostConflicts enxergar conflitos entre os dois
// tipos de recurso (não só dentro de cada um).
func mergeHostOwners(maps ...map[string][]hostOwner) map[string][]hostOwner {
	merged := make(map[string][]hostOwner)
	for _, m := range maps {
		for host, owners := range m {
			merged[host] = append(merged[host], owners...)
		}
	}
	return merged
}

// backendTLSWarningMessage é a mensagem determinística e sempre-correta anexada a todo host de um
// Ingress com re-encryption (backend-protocol HTTPS/GRPCS ou ssl-passthrough) — não confirma nem
// nega falha de handshake (isso é o Diagnóstico Avançado, backend_tls_check.go), só avisa que a
// superfície de risco existe. Zero custo, zero chance de falso-negativo.
const backendTLSWarningMessage = "este Ingress usa TLS re-encryption/passthrough para o backend — " +
	"a ferramenta não confirma automaticamente o handshake com o pod sem diagnóstico avançado " +
	"(ver botão \"Diagnóstico Avançado\")"

// applyConfigIssues preenche HostIssues em cada IngressRef/GatewayRef de cada certificado —
// (A) SAN não cobre o host (Error), (B) conflito de host detectado via hostOwners (Warning), e o
// aviso determinístico de re-encryption (Warning, só para Ingress) — e deriva HasConfigIssues.
// Chamado por Scanner.scanCluster depois de crossRefIngresses/crossRefGateways já terem populado
// UsedByIngresses/UsedByGateways.
func applyConfigIssues(certs []CertificateInfo, hostOwners map[string][]hostOwner) {
	conflicts := detectHostConflicts(hostOwners)

	for i := range certs {
		secretKey := certs[i].Namespace + "/" + certs[i].SecretName
		conflictsForSecret := conflicts[secretKey] // pode ser nil, ok

		for j := range certs[i].UsedByIngresses {
			ref := &certs[i].UsedByIngresses[j]
			for _, host := range ref.Hosts {
				if !certSANCoversHost(certs[i].DNSNames, host) {
					ref.HostIssues = append(ref.HostIssues, HostIssue{
						Host: host, Severity: "error",
						Message: fmt.Sprintf("nenhum SAN do certificado cobre o host %q declarado por este Ingress — clientes podem ver erro de certificado (SNI) ou o fallback padrão do controller", host),
					})
				}
				if msg, ok := conflictsForSecret[host]; ok {
					ref.HostIssues = append(ref.HostIssues, HostIssue{Host: host, Severity: "warning", Message: msg})
				}
				if ref.BackendTLS {
					ref.HostIssues = append(ref.HostIssues, HostIssue{Host: host, Severity: "warning", Message: backendTLSWarningMessage})
				}
			}
			if len(ref.HostIssues) > 0 {
				certs[i].HasConfigIssues = true
			}
		}

		for j := range certs[i].UsedByGateways {
			ref := &certs[i].UsedByGateways[j]
			for _, host := range ref.Hosts {
				if !certSANCoversHost(certs[i].DNSNames, host) {
					ref.HostIssues = append(ref.HostIssues, HostIssue{
						Host: host, Severity: "error",
						Message: fmt.Sprintf("nenhum SAN do certificado cobre o host %q declarado por este Gateway — clientes podem ver erro de certificado (SNI)", host),
					})
				}
				if msg, ok := conflictsForSecret[host]; ok {
					ref.HostIssues = append(ref.HostIssues, HostIssue{Host: host, Severity: "warning", Message: msg})
				}
			}
			if len(ref.HostIssues) > 0 {
				certs[i].HasConfigIssues = true
			}
		}
	}
}
