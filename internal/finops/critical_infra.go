package finops

import "strings"

// criticalInfraWorkloadPatterns é a lista curada de substrings de nome de workload que
// tipicamente indicam infraestrutura de cluster (não app de negócio) — mesma classe de
// heurística por nome já usada em isCompanyManagedDeployment (internal/kubernetes/client.go),
// motivada pelo incidente real que originou a Fase 1 desta auditoria: o pool "ingress"
// (nginx-ingress-controller/velero/istio-ingressgateway) recebeu sugestão de downsize sem
// nenhum aviso de que carrega componentes de infra burst-sensitive, não apps comuns.
//
// Substring, não prefixo exato — nomes reais de workload variam de convenção (ex:
// "ingress-nginx-controller", "nginx-ingress-controller", "ingress-nginx") — e case-insensitive.
// Deliberadamente SEM "ingress"/"vault"/"cost" soltos (falso-positivo alto contra apps de
// negócio nomeadas com esses termos, ex: um serviço "cost-report-api") — só termos específicos o
// bastante pra baixo risco de casar nome de app real.
var criticalInfraWorkloadPatterns = []string{
	"ingress-controller", "ingress-nginx", "istio", "envoy", "velero",
	"cert-manager", "external-dns", "coredns", "kube-dns", "kube-proxy",
	"calico", "cilium", "cni-node", "vpc-cni", "aws-node", "kyverno",
	"prometheus", "grafana", "alertmanager", "cluster-autoscaler",
	"metrics-server", "csi-", "konnectivity", "node-local-dns",
	"external-secrets", "linkerd",
}

// MatchCriticalInfraWorkloads devolve, dentre names, os que batem em qualquer padrão de
// criticalInfraWorkloadPatterns (substring, case-insensitive) — sem duplicatas, ordem
// preservada. É um PALPITE heurístico, nunca uma verdade absoluta (mesmo princípio de
// TrustedByPublicCA/isCompanyManagedDeployment nesta app) — serve só pra sinalizar "reveja com
// cuidado", nunca pra bloquear a sugestão em si.
func MatchCriticalInfraWorkloads(names []string) []string {
	seen := make(map[string]bool, len(names))
	matched := make([]string, 0)
	for _, n := range names {
		if seen[n] {
			continue
		}
		lower := strings.ToLower(n)
		for _, pat := range criticalInfraWorkloadPatterns {
			if strings.Contains(lower, pat) {
				seen[n] = true
				matched = append(matched, n)
				break
			}
		}
	}
	return matched
}
