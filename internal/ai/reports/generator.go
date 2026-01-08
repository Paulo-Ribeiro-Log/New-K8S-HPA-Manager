package reports

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"k8s-hpa-manager/internal/ai"

	"github.com/jung-kurt/gofpdf"
)

// utf8ToLatin1 converte string UTF-8 para Latin-1 (compatível com gofpdf)
func utf8ToLatin1(s string) string {
	// gofpdf não suporta UTF-8 nativamente com fontes built-in
	// Fazer conversão básica de caracteres acentuados
	replacements := map[string]string{
		"ç": "c", "Ç": "C",
		"ã": "a", "Ã": "A",
		"õ": "o", "Õ": "O",
		"á": "a", "Á": "A",
		"é": "e", "É": "E",
		"í": "i", "Í": "I",
		"ó": "o", "Ó": "O",
		"ú": "u", "Ú": "U",
		"â": "a", "Â": "A",
		"ê": "e", "Ê": "E",
		"ô": "o", "Ô": "O",
		"à": "a", "À": "A",
	}

	result := s
	for utf8Char, latin1Char := range replacements {
		result = strings.ReplaceAll(result, utf8Char, latin1Char)
	}
	return result
}

// GeneratePDF gera relatório PDF profissional (sem emojis)
func GeneratePDF(result *ai.AnalysisResult) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	// Usar fontes built-in do gofpdf ao invés de arquivos externos
	pdf.SetFont("Arial", "", 12)
	pdf.AddPage()

	// Header azul (padrão Health Check)
	pdf.SetFillColor(41, 128, 185) // RGB azul
	pdf.Rect(0, 0, 210, 50, "F")

	// Título principal
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 24)
	pdf.SetY(15)
	pdf.CellFormat(0, 10, "ANALISE DE DIAGNOSTICO - AI", "", 1, "C", false, 0, "")

	// Subtítulo
	pdf.SetFont("Arial", "", 12)
	pdf.SetY(30)
	subtitle := fmt.Sprintf("%s: %s/%s", result.ResourceType, result.Namespace, result.ResourceName)
	pdf.CellFormat(0, 8, subtitle, "", 1, "C", false, 0, "")

	// Data e hora
	pdf.SetFont("Arial", "I", 10)
	pdf.SetY(42)
	dateStr := result.AnalyzedAt.Format("02/01/2006 15:04:05")
	pdf.CellFormat(0, 6, "Analisado em: "+dateStr, "", 1, "C", false, 0, "")

	// Reset cor do texto
	pdf.SetTextColor(0, 0, 0)
	pdf.SetY(60)

	// SECAO 1: METADADOS DO RECURSO (Cabecalho Profissional)
	if result.ResourceMetadata != nil {
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(0, 10, "METADADOS DO RECURSO", "", 1, "L", true, 0, "")
		pdf.Ln(2)

		pdf.SetFont("Arial", "", 10)
		metadata := result.ResourceMetadata

		// Grid 2 colunas
		leftX := 10.0
		rightX := 110.0

		// Coluna esquerda
		pdf.SetX(leftX)
		pdf.Cell(40, 6, "Nome:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, metadata.Name)
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.SetX(leftX)
		pdf.Cell(40, 6, "Namespace:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, metadata.Namespace)
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.SetX(leftX)
		pdf.Cell(40, 6, "Cluster:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, metadata.Cluster)
		pdf.Ln(6)

		if metadata.Version != "" {
			pdf.SetFont("Arial", "", 10)
			pdf.SetX(leftX)
			pdf.Cell(40, 6, "Versao:")
			pdf.SetFont("Arial", "B", 10)
			pdf.Cell(0, 6, metadata.Version)
			pdf.Ln(6)
		}

		// Coluna direita (voltar no Y)
		currentY := pdf.GetY()
		pdf.SetY(currentY - 24) // Voltar 4 linhas

		pdf.SetFont("Arial", "", 10)
		pdf.SetX(rightX)
		pdf.Cell(40, 6, "Status:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, metadata.Status)
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.SetX(rightX)
		pdf.Cell(40, 6, "Age:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, metadata.Age)
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.SetX(rightX)
		pdf.Cell(40, 6, "Restart Count:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, fmt.Sprintf("%d", metadata.RestartCount))
		pdf.Ln(6)

		if metadata.NodeName != "" {
			pdf.SetFont("Arial", "", 10)
			pdf.SetX(rightX)
			pdf.Cell(40, 6, "Node:")
			pdf.SetFont("Arial", "B", 10)
			pdf.Cell(0, 6, metadata.NodeName)
			pdf.Ln(6)
		}

		pdf.SetY(currentY) // Restaurar posição Y

		// Recursos (CPU/Memory)
		pdf.Ln(4)
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(0, 6, "Recursos:")
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.SetX(leftX + 5)
		cpuLine := fmt.Sprintf("CPU: Current=%s, Request=%s, Limit=%s",
			metadata.Resources.CPU.Current,
			metadata.Resources.CPU.Request,
			metadata.Resources.CPU.Limit,
		)
		pdf.Cell(0, 6, cpuLine)
		pdf.Ln(6)

		pdf.SetX(leftX + 5)
		memLine := fmt.Sprintf("Memory: Current=%s, Request=%s, Limit=%s",
			metadata.Resources.Memory.Current,
			metadata.Resources.Memory.Request,
			metadata.Resources.Memory.Limit,
		)
		pdf.Cell(0, 6, memLine)
		pdf.Ln(8)
	}

	// SECAO 2: SUMARIO EXECUTIVO
	pdf.SetFont("Arial", "B", 14)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(0, 10, "SUMARIO EXECUTIVO", "", 1, "L", true, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Arial", "", 10)
	if result.ExecutiveSummary.QuickSummary != "" {
		// Converter UTF-8 corretamente
		quickSummary := utf8ToLatin1(result.ExecutiveSummary.QuickSummary)
		pdf.MultiCell(0, 6, quickSummary, "", "L", false)
		pdf.Ln(4)

		// Grid de métricas
		pdf.Cell(40, 6, "Severity:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(50, 6, strings.ToUpper(result.ExecutiveSummary.Severity))
		pdf.SetFont("Arial", "", 10)
		pdf.Cell(40, 6, "Status:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, strings.ToUpper(result.ExecutiveSummary.Status))
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.Cell(40, 6, "Tempo Estimado:")
		pdf.SetFont("Arial", "B", 10)
		timeToResolve := utf8ToLatin1(result.ExecutiveSummary.TimeToResolve)
		pdf.Cell(0, 6, timeToResolve)
		pdf.Ln(8)
	} else {
		// Fallback: usar Analysis legado (NUNCA DEVE CHEGAR AQUI)
		// Não exibir JSON cru - avisar que dados estruturados não disponíveis
		pdf.SetFont("Arial", "I", 10)
		pdf.Cell(0, 6, "Analise estruturada nao disponivel (formato legado)")
		pdf.Ln(8)
	}

	// SECAO 3: ANALISE DE CAUSA RAIZ
	if result.RootCauseAnalysis.Symptom != "" {
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(0, 10, "ANALISE DE CAUSA RAIZ", "", 1, "L", true, 0, "")
		pdf.Ln(2)

		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(0, 6, "Sintoma:")
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		pdf.MultiCell(0, 6, result.RootCauseAnalysis.Symptom, "", "L", false)
		pdf.Ln(4)

		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(0, 6, "Causas Provaveis:")
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		for i, cause := range result.RootCauseAnalysis.ProbableCauses {
			pdf.Cell(10, 6, fmt.Sprintf("%d.", i+1))
			pdf.MultiCell(0, 6, cause, "", "L", false)
		}
		pdf.Ln(4)

		if len(result.RootCauseAnalysis.Evidence) > 0 {
			pdf.SetFont("Arial", "B", 11)
			pdf.Cell(0, 6, "Evidencias:")
			pdf.Ln(6)
			pdf.SetFont("Arial", "I", 9)
			for _, evidence := range result.RootCauseAnalysis.Evidence {
				pdf.Cell(5, 5, "-")
				pdf.MultiCell(0, 5, evidence, "", "L", false)
			}
			pdf.Ln(4)
		}

		pdf.SetFont("Arial", "", 10)
		pdf.Cell(40, 6, "Confianca:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, strings.ToUpper(result.RootCauseAnalysis.Confidence))
		pdf.Ln(8)
	}

	// SECAO 4: IMPACTO E SEVERIDADE
	if result.ImpactAssessment.Severity != "" {
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(0, 10, "IMPACTO E SEVERIDADE", "", 1, "L", true, 0, "")
		pdf.Ln(2)

		pdf.SetFont("Arial", "", 10)
		pdf.Cell(40, 6, "Usuarios Afetados:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, result.ImpactAssessment.AffectedUsers)
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.Cell(40, 6, "Downtime Estimado:")
		pdf.SetFont("Arial", "B", 10)
		pdf.Cell(0, 6, result.ImpactAssessment.DowntimeEstimate)
		pdf.Ln(6)

		pdf.SetFont("Arial", "", 10)
		pdf.Cell(40, 6, "SLA Breach:")
		pdf.SetFont("Arial", "B", 10)
		slaText := "NAO"
		if result.ImpactAssessment.SLABreach {
			slaText = "SIM"
		}
		pdf.Cell(0, 6, slaText)
		pdf.Ln(6)

		if result.ImpactAssessment.BusinessImpact != "" {
			pdf.SetFont("Arial", "B", 11)
			pdf.Cell(0, 6, "Impacto no Negocio:")
			pdf.Ln(6)
			pdf.SetFont("Arial", "", 10)
			pdf.MultiCell(0, 6, result.ImpactAssessment.BusinessImpact, "", "L", false)
			pdf.Ln(8)
		}
	}

	// SECAO 5: RECOMENDACOES PRIORIZADAS
	if len(result.Recommendations) > 0 {
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(0, 10, "ACOES RECOMENDADAS", "", 1, "L", true, 0, "")
		pdf.Ln(2)

		for i, rec := range result.Recommendations {
			pdf.SetFont("Arial", "B", 12)
			pdf.SetFillColor(220, 220, 220)
			title := fmt.Sprintf("%d. %s (Prioridade: %d)", i+1, rec.Title, rec.Priority)
			pdf.CellFormat(0, 8, title, "", 1, "L", true, 0, "")
			pdf.Ln(2)

			pdf.SetFont("Arial", "", 10)
			pdf.MultiCell(0, 6, rec.Description, "", "L", false)
			pdf.Ln(2)

			if len(rec.Commands) > 0 {
				pdf.SetFont("Arial", "B", 10)
				pdf.Cell(0, 6, "Comandos:")
				pdf.Ln(6)
				pdf.SetFont("Courier", "", 9)
				pdf.SetFillColor(245, 245, 245)
				for _, cmd := range rec.Commands {
					pdf.CellFormat(0, 5, "  $ "+cmd, "", 1, "L", true, 0, "")
				}
				pdf.Ln(2)
			}

			pdf.SetFont("Arial", "", 9)
			metaLine := fmt.Sprintf("Tempo Estimado: %s  |  Risco: %s  |  Impacto: %s",
				rec.TimeEstimate, strings.ToUpper(rec.RiskLevel), strings.ToUpper(rec.ImpactLevel))
			pdf.Cell(0, 5, metaLine)
			pdf.Ln(8)
		}
	} else if len(result.Suggestions) > 0 {
		// Fallback: usar suggestions legadas
		pdf.SetFont("Arial", "B", 14)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(0, 10, "SUGESTOES", "", 1, "L", true, 0, "")
		pdf.Ln(2)

		for i, sug := range result.Suggestions {
			pdf.SetFont("Arial", "B", 11)
			title := fmt.Sprintf("%d. %s", i+1, sug.Description)
			pdf.MultiCell(0, 6, title, "", "L", false)

			if sug.Command != "" {
				pdf.SetFont("Courier", "", 9)
				pdf.SetFillColor(245, 245, 245)
				pdf.CellFormat(0, 5, "  $ "+sug.Command, "", 1, "L", true, 0, "")
			}

			pdf.SetFont("Arial", "I", 9)
			pdf.Cell(0, 5, fmt.Sprintf("Prioridade: %s", strings.ToUpper(sug.Priority)))
			pdf.Ln(8)
		}
	}

	// Footer
	pdf.SetY(-20)
	pdf.SetFont("Arial", "I", 8)
	pdf.SetTextColor(128, 128, 128)
	footer := fmt.Sprintf("Gerado por: %s (%s) | Tokens: %d | Tempo: %.2fs",
		result.Provider, result.Model, result.TokensUsed, result.ResponseTime)
	pdf.CellFormat(0, 6, footer, "", 0, "C", false, 0, "")

	var buf bytes.Buffer
	err := pdf.Output(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to generate PDF: %w", err)
	}

	return buf.Bytes(), nil
}

// GenerateMarkdown gera relatório Markdown formatado (sem emojis)
func GenerateMarkdown(result *ai.AnalysisResult) ([]byte, error) {
	var sb strings.Builder

	// Header
	sb.WriteString("# ANALISE DE DIAGNOSTICO - AI\n\n")
	sb.WriteString(fmt.Sprintf("**Recurso**: %s/%s/%s\n\n", result.ResourceType, result.Namespace, result.ResourceName))
	sb.WriteString(fmt.Sprintf("**Cluster**: %s\n\n", result.Cluster))
	sb.WriteString(fmt.Sprintf("**Analisado em**: %s\n\n", result.AnalyzedAt.Format("02/01/2006 15:04:05")))
	sb.WriteString("---\n\n")

	// SECAO 1: METADADOS DO RECURSO
	if result.ResourceMetadata != nil {
		sb.WriteString("## METADADOS DO RECURSO\n\n")

		sb.WriteString("| Campo | Valor |\n")
		sb.WriteString("|-------|-------|\n")
		sb.WriteString(fmt.Sprintf("| **Nome** | %s |\n", result.ResourceMetadata.Name))
		sb.WriteString(fmt.Sprintf("| **Namespace** | %s |\n", result.ResourceMetadata.Namespace))
		sb.WriteString(fmt.Sprintf("| **Cluster** | %s |\n", result.ResourceMetadata.Cluster))

		if result.ResourceMetadata.Version != "" {
			sb.WriteString(fmt.Sprintf("| **Versao** | %s |\n", result.ResourceMetadata.Version))
		}

		sb.WriteString(fmt.Sprintf("| **Status** | %s |\n", result.ResourceMetadata.Status))
		sb.WriteString(fmt.Sprintf("| **Age** | %s |\n", result.ResourceMetadata.Age))
		sb.WriteString(fmt.Sprintf("| **Restart Count** | %d |\n", result.ResourceMetadata.RestartCount))

		if result.ResourceMetadata.NodeName != "" {
			sb.WriteString(fmt.Sprintf("| **Node** | %s |\n", result.ResourceMetadata.NodeName))
		}

		sb.WriteString("\n### Recursos\n\n")
		sb.WriteString("| Tipo | Current | Request | Limit |\n")
		sb.WriteString("|------|---------|---------|-------|\n")
		sb.WriteString(fmt.Sprintf("| **CPU** | %s | %s | %s |\n",
			result.ResourceMetadata.Resources.CPU.Current,
			result.ResourceMetadata.Resources.CPU.Request,
			result.ResourceMetadata.Resources.CPU.Limit))
		sb.WriteString(fmt.Sprintf("| **Memory** | %s | %s | %s |\n\n",
			result.ResourceMetadata.Resources.Memory.Current,
			result.ResourceMetadata.Resources.Memory.Request,
			result.ResourceMetadata.Resources.Memory.Limit))
	}

	// SECAO 2: SUMARIO EXECUTIVO
	sb.WriteString("## SUMARIO EXECUTIVO\n\n")
	if result.ExecutiveSummary.QuickSummary != "" {
		sb.WriteString(result.ExecutiveSummary.QuickSummary + "\n\n")

		sb.WriteString("| Campo | Valor |\n")
		sb.WriteString("|-------|-------|\n")
		sb.WriteString(fmt.Sprintf("| **Severity** | %s |\n", strings.ToUpper(result.ExecutiveSummary.Severity)))
		sb.WriteString(fmt.Sprintf("| **Status** | %s |\n", strings.ToUpper(result.ExecutiveSummary.Status)))
		sb.WriteString(fmt.Sprintf("| **Tempo Estimado** | %s |\n\n", result.ExecutiveSummary.TimeToResolve))
	} else {
		sb.WriteString(result.Analysis + "\n\n")
	}

	// SECAO 3: ANALISE DE CAUSA RAIZ
	if result.RootCauseAnalysis.Symptom != "" {
		sb.WriteString("## ANALISE DE CAUSA RAIZ\n\n")

		sb.WriteString("### Sintoma\n\n")
		sb.WriteString(result.RootCauseAnalysis.Symptom + "\n\n")

		sb.WriteString("### Causas Provaveis\n\n")
		for i, cause := range result.RootCauseAnalysis.ProbableCauses {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, cause))
		}
		sb.WriteString("\n")

		if len(result.RootCauseAnalysis.Evidence) > 0 {
			sb.WriteString("### Evidencias\n\n")
			for _, evidence := range result.RootCauseAnalysis.Evidence {
				sb.WriteString(fmt.Sprintf("- %s\n", evidence))
			}
			sb.WriteString("\n")
		}

		sb.WriteString(fmt.Sprintf("**Confianca**: %s\n\n", strings.ToUpper(result.RootCauseAnalysis.Confidence)))
	}

	// SECAO 4: IMPACTO E SEVERIDADE
	if result.ImpactAssessment.Severity != "" {
		sb.WriteString("## IMPACTO E SEVERIDADE\n\n")

		sb.WriteString("| Campo | Valor |\n")
		sb.WriteString("|-------|-------|\n")
		sb.WriteString(fmt.Sprintf("| **Usuarios Afetados** | %s |\n", result.ImpactAssessment.AffectedUsers))
		sb.WriteString(fmt.Sprintf("| **Downtime Estimado** | %s |\n", result.ImpactAssessment.DowntimeEstimate))

		slaText := "NAO"
		if result.ImpactAssessment.SLABreach {
			slaText = "SIM"
		}
		sb.WriteString(fmt.Sprintf("| **SLA Breach** | %s |\n\n", slaText))

		if result.ImpactAssessment.BusinessImpact != "" {
			sb.WriteString("### Impacto no Negocio\n\n")
			sb.WriteString(result.ImpactAssessment.BusinessImpact + "\n\n")
		}
	}

	// SECAO 5: RECOMENDACOES PRIORIZADAS
	if len(result.Recommendations) > 0 {
		sb.WriteString("## ACOES RECOMENDADAS\n\n")

		for i, rec := range result.Recommendations {
			sb.WriteString(fmt.Sprintf("### %d. %s (Prioridade: %d)\n\n", i+1, rec.Title, rec.Priority))
			sb.WriteString(rec.Description + "\n\n")

			if len(rec.Commands) > 0 {
				sb.WriteString("**Comandos**:\n\n")
				sb.WriteString("```bash\n")
				for _, cmd := range rec.Commands {
					sb.WriteString(cmd + "\n")
				}
				sb.WriteString("```\n\n")
			}

			sb.WriteString(fmt.Sprintf("- **Tempo Estimado**: %s\n", rec.TimeEstimate))
			sb.WriteString(fmt.Sprintf("- **Risco**: %s\n", strings.ToUpper(rec.RiskLevel)))
			sb.WriteString(fmt.Sprintf("- **Impacto**: %s\n\n", strings.ToUpper(rec.ImpactLevel)))
		}
	} else if len(result.Suggestions) > 0 {
		sb.WriteString("## SUGESTOES\n\n")

		for i, sug := range result.Suggestions {
			sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, sug.Description))

			if sug.Command != "" {
				sb.WriteString("```bash\n")
				sb.WriteString(sug.Command + "\n")
				sb.WriteString("```\n\n")
			}

			sb.WriteString(fmt.Sprintf("**Prioridade**: %s\n\n", strings.ToUpper(sug.Priority)))
		}
	}

	// Footer
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("**Gerado por**: %s (%s) | **Tokens**: %d | **Tempo**: %.2fs\n",
		result.Provider, result.Model, result.TokensUsed, result.ResponseTime))

	return []byte(sb.String()), nil
}

// GenerateCSV gera relatório CSV para Excel
func GenerateCSV(result *ai.AnalysisResult) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Header geral
	writer.Write([]string{"ANALISE DE DIAGNOSTICO - AI"})
	writer.Write([]string{"Recurso", fmt.Sprintf("%s/%s/%s", result.ResourceType, result.Namespace, result.ResourceName)})
	writer.Write([]string{"Cluster", result.Cluster})
	writer.Write([]string{"Analisado em", result.AnalyzedAt.Format("02/01/2006 15:04:05")})
	writer.Write([]string{}) // Linha vazia

	// METADADOS DO RECURSO
	if result.ResourceMetadata != nil {
		writer.Write([]string{"METADADOS DO RECURSO"})
		writer.Write([]string{"Campo", "Valor"})
		writer.Write([]string{"Nome", result.ResourceMetadata.Name})
		writer.Write([]string{"Namespace", result.ResourceMetadata.Namespace})
		writer.Write([]string{"Cluster", result.ResourceMetadata.Cluster})

		if result.ResourceMetadata.Version != "" {
			writer.Write([]string{"Versao", result.ResourceMetadata.Version})
		}

		writer.Write([]string{"Status", result.ResourceMetadata.Status})
		writer.Write([]string{"Age", result.ResourceMetadata.Age})
		writer.Write([]string{"Restart Count", fmt.Sprintf("%d", result.ResourceMetadata.RestartCount)})

		if result.ResourceMetadata.NodeName != "" {
			writer.Write([]string{"Node", result.ResourceMetadata.NodeName})
		}

		writer.Write([]string{}) // Linha vazia
		writer.Write([]string{"Recursos"})
		writer.Write([]string{"Tipo", "Current", "Request", "Limit"})
		writer.Write([]string{"CPU",
			result.ResourceMetadata.Resources.CPU.Current,
			result.ResourceMetadata.Resources.CPU.Request,
			result.ResourceMetadata.Resources.CPU.Limit})
		writer.Write([]string{"Memory",
			result.ResourceMetadata.Resources.Memory.Current,
			result.ResourceMetadata.Resources.Memory.Request,
			result.ResourceMetadata.Resources.Memory.Limit})
		writer.Write([]string{}) // Linha vazia
	}

	// SUMARIO EXECUTIVO
	writer.Write([]string{"SUMARIO EXECUTIVO"})
	if result.ExecutiveSummary.QuickSummary != "" {
		writer.Write([]string{"Resumo", result.ExecutiveSummary.QuickSummary})
		writer.Write([]string{"Severity", strings.ToUpper(result.ExecutiveSummary.Severity)})
		writer.Write([]string{"Status", strings.ToUpper(result.ExecutiveSummary.Status)})
		writer.Write([]string{"Tempo Estimado", result.ExecutiveSummary.TimeToResolve})
	} else {
		writer.Write([]string{"Analise", result.Analysis})
	}
	writer.Write([]string{}) // Linha vazia

	// ANALISE DE CAUSA RAIZ
	if result.RootCauseAnalysis.Symptom != "" {
		writer.Write([]string{"ANALISE DE CAUSA RAIZ"})
		writer.Write([]string{"Sintoma", result.RootCauseAnalysis.Symptom})

		writer.Write([]string{"Causas Provaveis"})
		for i, cause := range result.RootCauseAnalysis.ProbableCauses {
			writer.Write([]string{fmt.Sprintf("Causa %d", i+1), cause})
		}

		if len(result.RootCauseAnalysis.Evidence) > 0 {
			writer.Write([]string{"Evidencias"})
			for i, evidence := range result.RootCauseAnalysis.Evidence {
				writer.Write([]string{fmt.Sprintf("Evidencia %d", i+1), evidence})
			}
		}

		writer.Write([]string{"Confianca", strings.ToUpper(result.RootCauseAnalysis.Confidence)})
		writer.Write([]string{}) // Linha vazia
	}

	// IMPACTO E SEVERIDADE
	if result.ImpactAssessment.Severity != "" {
		writer.Write([]string{"IMPACTO E SEVERIDADE"})
		writer.Write([]string{"Usuarios Afetados", result.ImpactAssessment.AffectedUsers})
		writer.Write([]string{"Downtime Estimado", result.ImpactAssessment.DowntimeEstimate})

		slaText := "NAO"
		if result.ImpactAssessment.SLABreach {
			slaText = "SIM"
		}
		writer.Write([]string{"SLA Breach", slaText})

		if result.ImpactAssessment.BusinessImpact != "" {
			writer.Write([]string{"Impacto no Negocio", result.ImpactAssessment.BusinessImpact})
		}
		writer.Write([]string{}) // Linha vazia
	}

	// RECOMENDACOES PRIORIZADAS
	if len(result.Recommendations) > 0 {
		writer.Write([]string{"ACOES RECOMENDADAS"})
		writer.Write([]string{"Prioridade", "Titulo", "Descricao", "Comandos", "Tempo", "Risco", "Impacto"})

		for _, rec := range result.Recommendations {
			commands := strings.Join(rec.Commands, "; ")
			writer.Write([]string{
				fmt.Sprintf("%d", rec.Priority),
				rec.Title,
				rec.Description,
				commands,
				rec.TimeEstimate,
				strings.ToUpper(rec.RiskLevel),
				strings.ToUpper(rec.ImpactLevel),
			})
		}
	} else if len(result.Suggestions) > 0 {
		writer.Write([]string{"SUGESTOES"})
		writer.Write([]string{"Tipo", "Descricao", "Comando", "Prioridade"})

		for _, sug := range result.Suggestions {
			writer.Write([]string{
				sug.Type,
				sug.Description,
				sug.Command,
				strings.ToUpper(sug.Priority),
			})
		}
	}

	writer.Write([]string{}) // Linha vazia

	// Footer
	writer.Write([]string{"Provider", result.Provider})
	writer.Write([]string{"Model", result.Model})
	writer.Write([]string{"Tokens Used", fmt.Sprintf("%d", result.TokensUsed)})
	writer.Write([]string{"Response Time", fmt.Sprintf("%.2fs", result.ResponseTime)})

	writer.Flush()

	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("failed to generate CSV: %w", err)
	}

	return buf.Bytes(), nil
}
