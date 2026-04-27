package teams

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	reCHG = regexp.MustCompile(`(?i)CHG\d{5,}`)
	// Casa URL direta, MCAS proxy (.mcas.ms) e URL-encoded (%2F em vez de /)
	reApprovalURL = regexp.MustCompile(`https?://devstartcd\.via\.com\.br(?:\.mcas\.ms)?/sre-approval/form/[a-f0-9%-]+`)
	// Safe Links: extrai a URL original de dentro do wrapper safelinks
	reSafeLinks = regexp.MustCompile(`safelinks\.protection\.outlook\.com/\?url=([^&\s"']+)`)
	// Captura "Nome e versão da aplicação: [Grupo] app-name - 1.2.3"
	reNomeVersao = regexp.MustCompile(`(?i)Nome e vers[aã]o da aplica[cç][aã]o:\s*(.+)`)
	// Captura padrão "[Grupo] app-name - versão" diretamente
	reAppVersion = regexp.MustCompile(`^\[.+?\]\s+\S+\s+-\s+[\w.\-]+$`)
)

// findApprovalURL encontra a URL do devstartcd em um texto, tratando Safe Links e MCAS.
func findApprovalURL(text string) string {
	// Tentativa direta (URL já limpa ou MCAS)
	if u := reApprovalURL.FindString(text); u != "" {
		return strings.Replace(u, ".mcas.ms", "", 1)
	}
	// Safe Links como texto: extrair URL original do parâmetro ?url=
	if m := reSafeLinks.FindStringSubmatch(text); len(m) > 1 {
		decoded, err := url.QueryUnescape(m[1])
		if err == nil {
			if u := reApprovalURL.FindString(decoded); u != "" {
				return strings.Replace(u, ".mcas.ms", "", 1)
			}
		}
	}
	return ""
}

// ParseDOMMessages analisa o array de strings extraído do DOM do Teams.
// Suporta dois formatos:
//   - Mensagem completa: um único string contém CHG + URL de aprovação + "Nome e versão"
//   - Elementos separados: CHG, URL e descrição em strings distintos (formato antigo)
func ParseDOMMessages(messages []string) []ApprovalItem {
	now := time.Now()
	seen := map[string]bool{}
	var items []ApprovalItem

	for i := 0; i < len(messages); i++ {
		chg := strings.ToUpper(reCHG.FindString(messages[i]))
		if chg == "" {
			continue
		}
		if seen[chg] {
			continue
		}

		// Formato completo: CHG e URL de aprovação no mesmo string (container DOM inteiro)
		approvalURL := findApprovalURL(messages[i])
		if approvalURL == "" {
			// Formato separado: URL nos próximos 3 elementos
			for j := i + 1; j < len(messages) && j <= i+3; j++ {
				if u := findApprovalURL(messages[j]); u != "" {
					approvalURL = u
					break
				}
			}
		}
		if approvalURL == "" {
			continue
		}

		// Tentar extrair descrição do mesmo string (formato completo)
		description := extractDescription(messages[i])

		// Formato separado: buscar descrição em elementos vizinhos
		if description == "" {
			start := i - 3
			if start < 0 {
				start = 0
			}
			end := i + 3
			if end >= len(messages) {
				end = len(messages) - 1
			}
			for k := start; k <= end; k++ {
				if d := extractDescription(messages[k]); d != "" {
					description = d
					break
				}
			}
		}

		seen[chg] = true
		snURL := "https://viavarejo.service-now.com/change_request.do?sysparm_query=number=" + chg
		items = append(items, ApprovalItem{
			CHG:           chg,
			ServiceNowURL: snURL,
			ApprovalURL:   approvalURL,
			Description:   description,
			ExtractedAt:   now,
		})
	}
	return items
}

// extractDescription tenta extrair o nome e versão da aplicação de um texto.
// Trata tanto mensagens completas (multiline) quanto strings simples.
func extractDescription(text string) string {
	// Varrer linha a linha (mensagem completa do Teams)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if m := reNomeVersao.FindStringSubmatch(line); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	// Formato direto "[Grupo] app-name - versão" numa única linha
	text = strings.TrimSpace(text)
	if reAppVersion.MatchString(text) {
		return text
	}
	return ""
}
