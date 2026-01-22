package sreapproval

import (
	"fmt"
	"regexp"
	"strings"
)

// Padrão para extrair UUID da URL do SRE Approval
// URL formato: https://devstartcd.via.com.br/sre-approval/form/842b99dd-a40e-45c9-b8b8-9e45d0c887d0
var approvalURLPattern = regexp.MustCompile(`/sre-approval/form/([a-f0-9-]{36})`)

// Padrão para extrair dados do título
// Formato: [Squad] aplicacao - versao
var titlePattern = regexp.MustCompile(`^\[([^\]]+)\]\s*(.+?)\s*-\s*([^\s]+)$`)

// ExtractApprovalID extrai o UUID da URL do SRE Approval
func ExtractApprovalID(url string) (string, error) {
	match := approvalURLPattern.FindStringSubmatch(url)
	if len(match) < 2 {
		return "", fmt.Errorf("ID de aprovação não encontrado na URL: %s", url)
	}
	return match[1], nil
}

// ParseTitle extrai squad, aplicação e versão do título
// Input: "[Retira Rápido] gcb-order-details-api - 1.1.0-3"
// Output: squad="Retira Rápido", app="gcb-order-details-api", version="1.1.0-3"
func ParseTitle(title string) (squad, application, version string) {
	match := titlePattern.FindStringSubmatch(strings.TrimSpace(title))
	if len(match) >= 4 {
		squad = strings.TrimSpace(match[1])
		application = strings.TrimSpace(match[2])
		version = strings.TrimSpace(match[3])
	}
	return
}

// ExtractBaseURL extrai a URL base do SRE Approval
// Input: https://devstartcd.via.com.br/sre-approval/form/842b99dd-...
// Output: https://devstartcd.via.com.br
func ExtractBaseURL(url string) string {
	if idx := strings.Index(url, "/sre-approval"); idx != -1 {
		return url[:idx]
	}
	// Fallback: extrair scheme + host
	parts := strings.SplitN(url, "/", 4)
	if len(parts) >= 3 {
		return parts[0] + "//" + parts[2]
	}
	return url
}

// BuildApprovalURL constrói a URL do formulário de aprovação
func BuildApprovalURL(baseURL, approvalID string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return fmt.Sprintf("%s/sre-approval/form/%s", baseURL, approvalID)
}

// BuildAPIURL constrói a URL da API de aprovação
func BuildAPIURL(baseURL, approvalID string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return fmt.Sprintf("%s/sre-approval/api/%s", baseURL, approvalID)
}
