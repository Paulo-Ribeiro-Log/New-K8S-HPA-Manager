package certificates

import "strings"

// matchesWildcardHost reporta se pattern (um hostname concreto ou um wildcard de único nível, ex:
// "*.example.com") cobre candidate. Compartilhado por dois chamadores com direções semânticas
// opostas: resolveGatewayHosts/hostnameMatchesListener (gateway_hosts.go) — o listener do Gateway
// API pode ser wildcard, o hostname da HTTPRoute é concreto — e certSANCoversHost
// (config_issues.go) — o SAN do certificado pode ser wildcard, o host do Ingress/Gateway é
// concreto. Mesmo algoritmo de matching em ambos os casos.
//
// Normaliza case dos dois lados antes de comparar: hosts de recursos K8s já vêm forçados
// minúsculo pelo apimachinery, mas SANs de certificado NÃO são garantidamente lowercase (depende
// de como o CSR foi gerado) — sem essa normalização, uma diferença de caixa geraria falso-positivo
// de "SAN não cobre host" sem nenhum erro de configuração real.
func matchesWildcardHost(pattern, candidate string) bool {
	pattern = strings.ToLower(pattern)
	candidate = strings.ToLower(candidate)

	if pattern == "" {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(candidate, suffix) && candidate != suffix[1:]
	}
	return pattern == candidate
}
