package tui

import (
	"fmt"
	"k8s-hpa-manager/internal/logs"

	tea "github.com/charmbracelet/bubbletea"
)

// logLoadedMsg é enviada quando os logs foram carregados
type logLoadedMsg struct {
	logs []string
	err  error
}

// logClearedMsg é enviada quando os logs foram limpos
type logClearedMsg struct {
	success bool
	err     error
}

// logCopiedMsg é enviada quando os logs foram copiados
type logCopiedMsg struct {
	success bool
	err     error
}

// loadLogs carrega os logs do arquivo
func (a *App) loadLogs() tea.Cmd {
	return func() tea.Msg {
		logManager := logs.GetInstance()
		logs, err := logManager.ReadLogs()
		return logLoadedMsg{
			logs: logs,
			err:  err,
		}
	}
}

// clearLogs limpa o arquivo de logs
func (a *App) clearLogs() tea.Cmd {
	return func() tea.Msg {
		logManager := logs.GetInstance()
		err := logManager.ClearLogs()
		return logClearedMsg{
			success: err == nil,
			err:     err,
		}
	}
}

// copyLogs copia os logs para a área de transferência
func (a *App) copyLogs() tea.Cmd {
	return func() tea.Msg {
		logManager := logs.GetInstance()
		err := logManager.CopyLogsToClipboard()
		return logCopiedMsg{
			success: err == nil,
			err:     err,
		}
	}
}

// handleLogViewerKeys - Navegação no visualizador de logs
func (a *App) handleLogViewerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Voltar ao estado anterior
		a.model.State = a.model.PreviousState
		a.model.LogViewerLogs = nil
		a.model.LogViewerScrollPos = 0
		a.model.LogViewerMessage = ""
		return a, tea.ClearScreen

	case "up", "k":
		// Scroll up
		if a.model.LogViewerScrollPos > 0 {
			a.model.LogViewerScrollPos--
		}

	case "down", "j":
		// Scroll down
		maxScroll := len(a.model.LogViewerLogs) - 20 // 20 linhas visíveis
		if maxScroll < 0 {
			maxScroll = 0
		}
		if a.model.LogViewerScrollPos < maxScroll {
			a.model.LogViewerScrollPos++
		}

	case "pgup":
		// Page up (10 linhas)
		a.model.LogViewerScrollPos -= 10
		if a.model.LogViewerScrollPos < 0 {
			a.model.LogViewerScrollPos = 0
		}

	case "pgdown":
		// Page down (10 linhas)
		maxScroll := len(a.model.LogViewerLogs) - 20
		if maxScroll < 0 {
			maxScroll = 0
		}
		a.model.LogViewerScrollPos += 10
		if a.model.LogViewerScrollPos > maxScroll {
			a.model.LogViewerScrollPos = maxScroll
		}

	case "home":
		// Ir para o início
		a.model.LogViewerScrollPos = 0

	case "end":
		// Ir para o fim
		maxScroll := len(a.model.LogViewerLogs) - 20
		if maxScroll < 0 {
			maxScroll = 0
		}
		a.model.LogViewerScrollPos = maxScroll

	case "c", "C":
		// Copiar logs para área de transferência
		a.model.LogViewerMessage = "📋 Copiando logs..."
		return a, a.copyLogs()

	case "l", "L":
		// Limpar logs
		a.model.LogViewerMessage = "🗑️ Limpando logs..."
		return a, a.clearLogs()

	case "r", "R", "f5":
		// Recarregar logs
		a.model.LogViewerLoading = true
		a.model.LogViewerMessage = "🔄 Recarregando logs..."
		return a, a.loadLogs()
	}

	return a, nil
}

// handleLogViewerMessages - Processar mensagens do visualizador de logs
func (a *App) handleLogViewerMessages(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case logLoadedMsg:
		a.model.LogViewerLoading = false
		if msg.err != nil {
			a.model.LogViewerMessage = fmt.Sprintf("❌ Erro ao carregar logs: %v", msg.err)
			a.model.LogViewerLogs = []string{}
		} else {
			a.model.LogViewerLogs = msg.logs
			a.model.LogViewerMessage = fmt.Sprintf("✅ %d linhas carregadas", len(msg.logs))
			// Ir para o final dos logs
			maxScroll := len(msg.logs) - 20
			if maxScroll < 0 {
				maxScroll = 0
			}
			a.model.LogViewerScrollPos = maxScroll
		}
		return a, nil

	case logClearedMsg:
		if msg.success {
			a.model.LogViewerMessage = "✅ Logs limpos com sucesso"
			a.model.LogViewerLogs = []string{}
			a.model.LogViewerScrollPos = 0
			// Recarregar para mostrar mensagem de limpeza
			return a, a.loadLogs()
		} else {
			a.model.LogViewerMessage = fmt.Sprintf("❌ Erro ao limpar logs: %v", msg.err)
		}
		return a, nil

	case logCopiedMsg:
		if msg.success {
			a.model.LogViewerMessage = "✅ Logs copiados para /tmp/k8s-hpa-manager-logs.txt"
		} else {
			a.model.LogViewerMessage = fmt.Sprintf("📋 %v", msg.err)
		}
		return a, nil
	}

	return a, nil
}
