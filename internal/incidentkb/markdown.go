package incidentkb

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const frontmatterDelim = "---"

// toMarkdown serializa um Incident como front-matter YAML + corpo em seções,
// no mesmo espírito de knowledge base em Markdown usado por outras ferramentas
// do time (runbooks, MCP-SRE-Canais) — legível por humanos e diffável no git.
func toMarkdown(inc *Incident) ([]byte, error) {
	fm, err := yaml.Marshal(inc)
	if err != nil {
		return nil, fmt.Errorf("erro ao serializar front-matter: %w", err)
	}

	var b strings.Builder
	b.WriteString(frontmatterDelim + "\n")
	b.Write(fm)
	b.WriteString(frontmatterDelim + "\n\n")
	fmt.Fprintf(&b, "# %s / %s / %s\n\n", inc.Cluster, inc.Namespace, inc.ResourceName)
	b.WriteString("## Sintoma\n\n")
	b.WriteString(strings.TrimSpace(inc.Symptom) + "\n\n")
	b.WriteString("## Causa raiz\n\n")
	b.WriteString(strings.TrimSpace(inc.RootCause) + "\n\n")
	b.WriteString("## Resolução\n\n")
	b.WriteString(strings.TrimSpace(inc.Resolution) + "\n")

	return []byte(b.String()), nil
}

// fromMarkdown faz o caminho inverso de toMarkdown.
func fromMarkdown(data []byte) (*Incident, error) {
	content := string(data)
	parts := strings.SplitN(content, frontmatterDelim, 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("arquivo sem front-matter válido")
	}

	inc := &Incident{}
	if err := yaml.Unmarshal([]byte(parts[1]), inc); err != nil {
		return nil, fmt.Errorf("erro ao ler front-matter: %w", err)
	}

	body := parts[2]
	inc.Symptom = extractSection(body, "## Sintoma")
	inc.RootCause = extractSection(body, "## Causa raiz")
	inc.Resolution = extractSection(body, "## Resolução")

	return inc, nil
}

// extractSection retorna o texto entre um cabeçalho "## X" e o próximo "## "
// (ou o fim do documento), já sem espaços nas pontas.
func extractSection(body, header string) string {
	idx := strings.Index(body, header)
	if idx == -1 {
		return ""
	}
	rest := body[idx+len(header):]
	if next := strings.Index(rest, "\n## "); next != -1 {
		rest = rest[:next]
	}
	return strings.TrimSpace(rest)
}
