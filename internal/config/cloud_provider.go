package config

import (
	"regexp"
	"strings"
)

const (
	CloudProviderAKS     = "aks"
	CloudProviderEKS     = "eks"
	CloudProviderUnknown = "unknown"
)

// eksURLPattern detecta URLs de API server do AWS EKS.
// Exemplo: https://ABC123.gr7.us-east-1.eks.amazonaws.com
var eksURLPattern = regexp.MustCompile(`\.eks\.amazonaws\.com`)

// aksURLPattern detecta URLs de API server do Azure AKS.
// Exemplo: https://akspriv-abc.hcp.brazilsouth.azmk8s.io
var aksURLPattern = regexp.MustCompile(`\.azmk8s\.io`)

// eksRegionPattern extrai a região de uma URL EKS.
var eksRegionPattern = regexp.MustCompile(`\.([a-z]{2}-[a-z]+-\d)\.eks\.amazonaws\.com`)

// DetectCloudProvider detecta o cloud provider a partir da URL do API server do kubeconfig.
// Retorna "aks", "eks" ou "unknown".
func DetectCloudProvider(serverURL string) string {
	switch {
	case aksURLPattern.MatchString(serverURL):
		return CloudProviderAKS
	case eksURLPattern.MatchString(serverURL):
		return CloudProviderEKS
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
