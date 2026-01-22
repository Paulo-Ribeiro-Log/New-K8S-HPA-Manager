package servicenow

import (
	"net/url"
	"regexp"
	"strings"
)

// Padrões de extração do template da esteira de CD
var extractionPatterns = map[string]*regexp.Regexp{
	// * Aplicação(ões): tms-sync-1p-order-management-acl.
	"application": regexp.MustCompile(`\*\s*Aplicação\(ões\):\s*(.+?)\.`),

	// * Versão: 0.0.6-2. (captura versão semver com pontos e hífens)
	"version": regexp.MustCompile(`\*\s*Versão:\s*([\d]+\.[\d]+\.[\d]+(?:-[\d]+)?)`),

	// * Repositório: github.com/viavarejo-internal/tms-sync-1p-order-management-acl.git.
	"repository": regexp.MustCompile(`\*\s*Repositório:\s*github\.com/viavarejo-internal/(.+?)\.git`),

	// * Squad(s): Planejamento.
	"squad": regexp.MustCompile(`\*\s*Squad\(s\):\s*(.+?)\.`),

	// * Branch no GitHub: release/0.0.6. (captura branch com pontos)
	"branch": regexp.MustCompile(`\*\s*Branch no GitHub:\s*([^\.\n]+(?:\.[^\.\n]+)*)`),

	// * Produto: tms-sync-1p-order-management-acl.
	"product": regexp.MustCompile(`\*\s*Produto:\s*(.+?)\.`),

	// * Projeto: tms-sync-1p-order-management-acl.
	"project": regexp.MustCompile(`\*\s*Projeto:\s*(.+?)\.`),

	// * Titulo da release no XL-Release: [Planejamento] tms-sync-1p-order-management-acl - 0.0.6-2.
	"xlrelease_title": regexp.MustCompile(`\*\s*Titulo da release no XL-Release:\s*(.+?)\.`),

	// * Link da release no XL-Release: http://release.viavarejo.com.br/#/releases/...
	"xlrelease_url": regexp.MustCompile(`\*\s*Link da release no XL-Release:\s*(https?://[^\s]+)`),

	// * Severidade: 1
	"severity": regexp.MustCompile(`\*\s*Severidade:\s*(\d+)`),

	// Issues do Jira (múltiplas): PDE-3215, ABC-1234, etc
	"jira_issues": regexp.MustCompile(`([A-Z]+-\d+)`),
}

// ExtractFromDescription extrai dados estruturados do campo description
func ExtractFromDescription(description string) *ExtractedData {
	data := &ExtractedData{
		JiraIssues: []string{},
		Confidence: 0.0,
	}

	fieldsFound := 0
	totalFields := 4 // Campos principais: application, version, repository, xlrelease_url

	// Aplicação
	if match := extractionPatterns["application"].FindStringSubmatch(description); len(match) > 1 {
		data.Application = strings.TrimSpace(match[1])
		fieldsFound++
	}

	// Versão
	if match := extractionPatterns["version"].FindStringSubmatch(description); len(match) > 1 {
		data.Version = strings.TrimSpace(match[1])
		fieldsFound++
	}

	// Repositório GitHub
	if match := extractionPatterns["repository"].FindStringSubmatch(description); len(match) > 1 {
		data.GitHubRepo = strings.TrimSpace(match[1])
		fieldsFound++
	}

	// Link XL Release
	if match := extractionPatterns["xlrelease_url"].FindStringSubmatch(description); len(match) > 1 {
		data.XLReleaseURL = strings.TrimSpace(match[1])
		fieldsFound++
	}

	// Campos adicionais (não afetam confiança)
	if match := extractionPatterns["squad"].FindStringSubmatch(description); len(match) > 1 {
		data.Squad = strings.TrimSpace(match[1])
	}

	if match := extractionPatterns["branch"].FindStringSubmatch(description); len(match) > 1 {
		data.Branch = strings.TrimSpace(match[1])
	}

	if match := extractionPatterns["product"].FindStringSubmatch(description); len(match) > 1 {
		data.Product = strings.TrimSpace(match[1])
	}

	if match := extractionPatterns["project"].FindStringSubmatch(description); len(match) > 1 {
		data.Project = strings.TrimSpace(match[1])
	}

	if match := extractionPatterns["xlrelease_title"].FindStringSubmatch(description); len(match) > 1 {
		data.XLReleaseTitle = strings.TrimSpace(match[1])
	}

	if match := extractionPatterns["severity"].FindStringSubmatch(description); len(match) > 1 {
		data.Severity = strings.TrimSpace(match[1])
	}

	// Jira Issues (múltiplas)
	jiraMatches := extractionPatterns["jira_issues"].FindAllStringSubmatch(description, -1)
	seen := make(map[string]bool)
	for _, match := range jiraMatches {
		if len(match) > 0 && !seen[match[1]] {
			data.JiraIssues = append(data.JiraIssues, match[1])
			seen[match[1]] = true
		}
	}

	// Calcular confiança baseado nos campos principais encontrados
	if totalFields > 0 {
		data.Confidence = float64(fieldsFound) / float64(totalFields)
	}

	return data
}

// ExtractSysIDFromURL extrai o sys_id da URL do ServiceNow
// URL formato: https://viavarejo.service-now.com/change_request.do?sys_id=c3b53c259722ba50ec54f3b6f053af33
func ExtractSysIDFromURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	sysID := parsedURL.Query().Get("sys_id")
	if sysID == "" {
		return "", &ParseError{Message: "sys_id não encontrado na URL"}
	}

	return sysID, nil
}

// ParseError representa um erro de parsing
type ParseError struct {
	Message string
}

func (e *ParseError) Error() string {
	return e.Message
}
