package incidentkb

import (
	"fmt"
	"strings"
)

// ExportMarkdown gera o Markdown de UM incidente pronto pra colar no editor do
// Confluence (que converte Markdown automaticamente ao colar). Diferente do
// formato interno de armazenamento (toMarkdown, com front-matter YAML), aqui
// os metadados viram uma tabela — o front-matter apareceria como texto cru
// (e um "---" solto é lido como linha horizontal, não como front-matter).
func ExportMarkdown(inc *Incident) []byte {
	var b strings.Builder
	writeIncidentSection(&b, inc, 1)
	return []byte(b.String())
}

// ExportBundleMarkdown concatena vários incidentes em um único documento —
// útil pra popular de uma vez uma página do Confluence com a base inteira
// (ou um recorte filtrado dela).
func ExportBundleMarkdown(incidents []*Incident) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Base de Conhecimento de Incidentes (%d registros)\n\n", len(incidents))

	for i, inc := range incidents {
		writeIncidentSection(&b, inc, 2)
		if i < len(incidents)-1 {
			b.WriteString("\n---\n\n")
		}
	}

	return []byte(b.String())
}

// writeIncidentSection escreve um incidente a partir de headingLevel (1 = "#",
// 2 = "##", ...), com as subseções (metadados, sintoma, causa raiz, resolução)
// sempre um nível abaixo do título.
func writeIncidentSection(b *strings.Builder, inc *Incident, headingLevel int) {
	h := strings.Repeat("#", headingLevel)
	sub := strings.Repeat("#", headingLevel+1)

	fmt.Fprintf(b, "%s %s / %s / %s\n\n", h, inc.Cluster, inc.Namespace, inc.ResourceName)

	b.WriteString("| Campo | Valor |\n")
	b.WriteString("|---|---|\n")
	fmt.Fprintf(b, "| Data | %s |\n", inc.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(b, "| Cluster | %s |\n", inc.Cluster)
	fmt.Fprintf(b, "| Namespace | %s |\n", inc.Namespace)
	fmt.Fprintf(b, "| Recurso | %s / %s |\n", inc.ResourceType, inc.ResourceName)
	fmt.Fprintf(b, "| Severidade | %s |\n", inc.Severity)
	fmt.Fprintf(b, "| Tags | %s |\n", strings.Join(inc.Tags, ", "))
	fmt.Fprintf(b, "| Autor | %s |\n", inc.Author)
	b.WriteString("\n")

	fmt.Fprintf(b, "%s Sintoma\n\n%s\n\n", sub, strings.TrimSpace(inc.Symptom))
	fmt.Fprintf(b, "%s Causa raiz\n\n%s\n\n", sub, strings.TrimSpace(inc.RootCause))
	fmt.Fprintf(b, "%s Resolução\n\n%s\n\n", sub, strings.TrimSpace(inc.Resolution))
}
