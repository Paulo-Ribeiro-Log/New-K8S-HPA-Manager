package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"k8s-hpa-manager/internal/logs"
)

// renderLogViewer renderiza a interface de visualização de logs
func (a *App) renderLogViewer() string {
	logManager := logs.GetInstance()
	logPath := logManager.GetLogPath()

	// Estilos
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("87"))

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Bold(true)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(1)

	// Construir header
	var header strings.Builder
	header.WriteString(titleStyle.Render("📄 Visualizador de Logs"))
	header.WriteString("\n")
	header.WriteString(headerStyle.Render("Arquivo: "))
	header.WriteString(pathStyle.Render(logPath))
	header.WriteString("\n")

	// Mensagem de status
	if a.model.LogViewerMessage != "" {
		header.WriteString(messageStyle.Render(a.model.LogViewerMessage))
		header.WriteString("\n")
	}

	// Se está carregando
	if a.model.LogViewerLoading {
		header.WriteString("\n🔄 Carregando logs...\n")
		return header.String()
	}

	// Conteúdo dos logs
	var content strings.Builder
	content.WriteString(header.String())
	content.WriteString("\n")

	// Borda superior
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	content.WriteString(borderStyle.Render(strings.Repeat("─", a.width-4)))
	content.WriteString("\n")

	// Calcular linhas visíveis
	headerLines := strings.Count(header.String(), "\n") + 3 // +3 para borda e ajuda
	availableHeight := a.height - headerLines - 5 // Reservar espaço para footer
	if availableHeight < 10 {
		availableHeight = 10
	}

	// Renderizar logs com scroll
	totalLogs := len(a.model.LogViewerLogs)
	if totalLogs == 0 {
		content.WriteString("\n📭 Nenhum log encontrado\n\n")
	} else {
		// Calcular janela de visualização
		startLine := a.model.LogViewerScrollPos
		endLine := startLine + availableHeight
		if endLine > totalLogs {
			endLine = totalLogs
		}
		if startLine >= totalLogs {
			startLine = totalLogs - availableHeight
			if startLine < 0 {
				startLine = 0
			}
		}

		// Renderizar linhas visíveis
		for i := startLine; i < endLine && i < totalLogs; i++ {
			logLine := a.model.LogViewerLogs[i]

			// Colorir com base no nível do log
			lineStyle := lipgloss.NewStyle()
			if strings.Contains(logLine, "[ERROR]") {
				lineStyle = lineStyle.Foreground(lipgloss.Color("196")) // Vermelho
			} else if strings.Contains(logLine, "[WARNING]") {
				lineStyle = lineStyle.Foreground(lipgloss.Color("214")) // Laranja
			} else if strings.Contains(logLine, "[SUCCESS]") {
				lineStyle = lineStyle.Foreground(lipgloss.Color("46")) // Verde
			} else if strings.Contains(logLine, "[DEBUG]") {
				lineStyle = lineStyle.Foreground(lipgloss.Color("240")) // Cinza
			} else {
				lineStyle = lineStyle.Foreground(lipgloss.Color("255")) // Branco
			}

			// Truncar linha se muito longa
			maxLineWidth := a.width - 4
			if len(logLine) > maxLineWidth {
				logLine = logLine[:maxLineWidth-3] + "..."
			}

			content.WriteString(lineStyle.Render(logLine))
			content.WriteString("\n")
		}

		// Indicador de scroll
		if totalLogs > availableHeight {
			scrollInfo := fmt.Sprintf(" [Linhas %d-%d de %d] ", startLine+1, endLine, totalLogs)
			scrollStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Italic(true)
			content.WriteString("\n")
			content.WriteString(scrollStyle.Render(scrollInfo))
		}
	}

	// Borda inferior
	content.WriteString("\n")
	content.WriteString(borderStyle.Render(strings.Repeat("─", a.width-4)))
	content.WriteString("\n")

	// Footer com ajuda
	help := helpStyle.Render(
		"↑↓/k j: Scroll | PgUp/PgDn: Página | Home/End: Início/Fim | " +
		"C: Copiar | L: Limpar | R/F5: Recarregar | ESC: Voltar")
	content.WriteString(help)

	return content.String()
}
