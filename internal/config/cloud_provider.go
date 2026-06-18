package config

import (
	"regexp"
	"strings"
)

const (
	CloudProviderAKS     = "aks"
	CloudProviderEKS     = "eks"
	CloudProviderGKE     = "gke"
	CloudProviderUnknown = "unknown"
)

// eksURLPattern detecta URLs de API server do AWS EKS.
// Exemplo: https://ABC123.gr7.us-east-1.eks.amazonaws.com
var eksURLPattern = regexp.MustCompile(`\.eks\.amazonaws\.com`)

// aksURLPattern detecta URLs de API server do Azure AKS.
// Exemplo: https://akspriv-abc.hcp.brazilsouth.azmk8s.io
var aksURLPattern = regexp.MustCompile(`\.azmk8s\.io`)

// gkeURLPattern detecta URLs de API server do GKE.
// Exemplo: https://HASH.REGION.container.googleapis.com
var gkeURLPattern = regexp.MustCompile(`\.container\.googleapis\.com`)

// eksRegionPattern extrai a região de uma URL EKS.
var eksRegionPattern = regexp.MustCompile(`\.([a-z]{2}-[a-z]+-\d)\.eks\.amazonaws\.com`)

// DetectCloudProvider detecta o cloud provider a partir da URL do API server e do nome do context.
// contextName (opcional) é o nome do context no kubeconfig — usado para detectar GKE via prefixo gke_,
// pois clusters GKE privados expõem apenas IPs como API server.
// Retorna "aks", "eks", "gke" ou "unknown".
func DetectCloudProvider(serverURL string, contextName ...string) string {
	// Context GKE começa com "gke_" — mais confiável que URL para clusters privados
	for _, ctx := range contextName {
		if strings.HasPrefix(ctx, "gke_") {
			return CloudProviderGKE
		}
	}
	switch {
	case aksURLPattern.MatchString(serverURL):
		return CloudProviderAKS
	case eksURLPattern.MatchString(serverURL):
		return CloudProviderEKS
	case gkeURLPattern.MatchString(serverURL):
		return CloudProviderGKE
	default:
		return CloudProviderUnknown
	}
}

// ExtractRegionFromEKSURL extrai a região AWS de uma URL EKS.
// Retorna string vazia se a URL não for EKS ou não contiver a região.
func ExtractRegionFromEKSURL(serverURL string) string {
	m := eksRegionPattern.FindStringSubmatch(serverURL)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
