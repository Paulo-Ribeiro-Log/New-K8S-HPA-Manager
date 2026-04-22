package teams

import (
	"regexp"
	"strings"
	"time"
)

var (
	reCHG         = regexp.MustCompile(`(?i)CHG\d{5,}`)
	reApprovalURL = regexp.MustCompile(`https?://devstartcd\.via\.com\.br/sre-approval/form/[a-f0-9-]+`)
	// Captura "Nome e versão da aplicação: [Grupo] app-name - 1.2.3"
	reNomeVersao  = regexp.MustCompile(`(?i)Nome e vers[aã]o da aplica[cç][aã]o:\s*(.+)`)
	// Captura padrão "[Grupo] app-name - versão" diretamente
	reAppVersion  = regexp.MustCompile(`^\[.+?\]\s+\S+\s+-\s+[\w.\-]+$`)
)

// ParseDOMMessages analisa o array plano de strings extraído do DOM do Teams.
// O padrão esperado por mensagem: [preview, descrição/corpo, CHG, URL].
// O parser extrai CHG, URL e busca a descrição nos 3 elementos anteriores ao CHG.
func ParseDOMMessages(messages []string) []ApprovalItem {
	now := time.Now()
	seen := map[string]bool{}
	var items []ApprovalItem

	for i := 0; i < len(messages); i++ {
		chg := reCHG.FindString(messages[i])
		if chg == "" {
			continue
		}
		chg = strings.ToUpper(chg)
		if seen[chg] {
			continue
		}
		// Procurar URL nos próximos 3 elementos
		approvalURL := ""
		for j := i + 1; j < len(messages) && j <= i+3; j++ {
			if u := reApprovalURL.FindString(messages[j]); u != "" {
				approvalURL = u
				break
			}
		}
		if approvalURL == "" {
			continue
		}
		// Procurar descrição em janela ao redor do CHG: [i-3, i+3], excluindo URL
		description := ""
		start := i - 3
		if start < 0 {
			start = 0
		}
		end := i + 3
		if end >= len(messages) {
			end = len(messages) - 1
		}
		for k := start; k <= end; k++ {
			text := strings.TrimSpace(messages[k])
			// Ignorar o próprio CHG e URLs
			if reApprovalURL.MatchString(text) || reCHG.MatchString(text) && len(text) < 20 {
				continue
			}
			// Linha "Nome e versão da aplicação: ..."
			if m := reNomeVersao.FindStringSubmatch(text); len(m) > 1 {
				description = strings.TrimSpace(m[1])
				break
			}
			// Formato direto "[Grupo] app-name - versão"
			if reAppVersion.MatchString(text) {
				description = text
				break
			}
			// Texto multi-linha: varrer linha a linha
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if m2 := reNomeVersao.FindStringSubmatch(line); len(m2) > 1 {
					description = strings.TrimSpace(m2[1])
					break
				}
			}
			if description != "" {
				break
			}
		}
		seen[chg] = true
		snURL := "https://viavarejo.service-now.com/nav_to.do?uri=change_request.do%3Fnumber%3D" + chg
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
