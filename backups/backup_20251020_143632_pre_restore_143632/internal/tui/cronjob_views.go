package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderCronJobSelection renderiza a tela de seleção de CronJobs
func (a *App) renderCronJobSelection() string {
	if a.model.Loading {
		content := fmt.Sprintf(
			"🔍 Carregando CronJobs...\n\n"+
			"Cluster: %s\n\n"+
			"⏳ Analisando namespaces e CronJobs...",
			a.model.SelectedCluster.Name,
		)
		return a.getTabBar() + renderPanelWithTitle(content, fmt.Sprintf("CronJobs do Cluster: %s", a.model.SelectedCluster.Name), 60, 18, primaryColor)
	}

	// Header com cluster, sessão e contexto
	contextBox := renderContextBox(a.model.SelectedCluster, a.model.LoadedSessionName, "Gerenciamento de CronJobs")

	sessionInfo := contextBox + a.renderSessionHeader()

	// Preparar linhas para scroll responsivo
	allLines := a.buildCronJobLines()
	cronJobPanel := a.renderResponsiveCronJobPanel(allLines)

	// Painel de status
	statusPanel := a.renderStatusInfoPanel()
	statusSpacing := a.calculateCronJobPanelSpacing()

	return a.getTabBar() + sessionInfo + cronJobPanel + statusSpacing + statusPanel
}

// renderCronJobEditing renderiza a tela de edição de CronJob
func (a *App) renderCronJobEditing() string {
	if a.model.EditingCronJob == nil {
		return a.getTabBar() + "⚠️ Nenhum CronJob selecionado para edição"
	}

	cronJob := a.model.EditingCronJob
	sessionInfo := a.renderSessionHeader()

	var content strings.Builder
	content.WriteString(fmt.Sprintf("📝 Editando: %s\n", cronJob.Name))
	content.WriteString(fmt.Sprintf("📍 Namespace: %s\n", cronJob.Namespace))
	content.WriteString(fmt.Sprintf("📅 Schedule: %s\n\n", cronJob.ScheduleDesc))

	// Campo editável - Status (Suspenso/Ativo)
	currentStatus := "Ativo"
	if cronJob.Suspend != nil && *cronJob.Suspend {
		currentStatus = "Suspenso"
	}

	// Criar linha do status
	statusLine := fmt.Sprintf("  Status: [%s]", currentStatus)

	// Aplicar estilo de seleção se for o item selecionado
	if a.model.SelectedIndex == 0 {
		selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
		content.WriteString(selectedStyle.Render(statusLine) + "\n")
	} else {
		content.WriteString(statusLine + "\n")
	}
	content.WriteString("\nControles:\n")
	content.WriteString("Space: Alternar Status • Ctrl+S: Salvar • ESC: Voltar\n")
	content.WriteString("Abas: Alt+1-9/0 Mudar • Ctrl+T Nova • Ctrl+W Fechar\n")

	// Informações adicionais
	content.WriteString("\n--- Informações do CronJob ---\n")
	if cronJob.LastScheduleTime != nil {
		content.WriteString(fmt.Sprintf("Última execução: %s\n", cronJob.LastScheduleTime.Format("02/01/2006 15:04:05")))
		content.WriteString(fmt.Sprintf("Status da última execução: %s\n", cronJob.LastRunStatus))
	}
	content.WriteString(fmt.Sprintf("Jobs ativos: %d\n", cronJob.ActiveJobs))

	if cronJob.JobTemplate != "" {
		content.WriteString(fmt.Sprintf("Template: %s\n", cronJob.JobTemplate))
	}

	editingPanel := renderPanelWithTitle(content.String(), fmt.Sprintf("Editando CronJob: %s", cronJob.Name), 60, 18, primaryColor)

	// Painel de status
	statusPanel := a.renderStatusInfoPanel()
	statusSpacing := a.calculateCronJobEditingPanelSpacing()

	return a.getTabBar() + sessionInfo + editingPanel + statusSpacing + statusPanel
}

// buildCronJobLines - Constrói todas as linhas de CronJobs para renderização responsiva
func (a *App) buildCronJobLines() []string {
	if len(a.model.CronJobs) == 0 {
		return []string{"Nenhum CronJob encontrado"}
	}

	var allLines []string
	clusterName := a.model.SelectedCluster.Name

	// Header
	allLines = append(allLines, fmt.Sprintf("📅 Cluster: %s", clusterName))
	allLines = append(allLines, fmt.Sprintf("📊 Total: %d | Selecionados: %d", len(a.model.CronJobs), len(a.model.SelectedCronJobs)))
	allLines = append(allLines, "")
	allLines = append(allLines, "Controles: [Space] Selecionar [Enter] Editar [ESC] Voltar")
	allLines = append(allLines, "Abas: Alt+1-9/0 Mudar • Ctrl+T Nova • Ctrl+W Fechar")
	allLines = append(allLines, "")

	// Lista de CronJobs
	for i, cronJob := range a.model.CronJobs {
		selection := "◯"
		if cronJob.Selected {
			selection = "●"
		}

		// Status
		status := "🟢"
		if cronJob.Suspend != nil && *cronJob.Suspend {
			status = "🔴 SUSPENSO"
		} else if cronJob.LastRunStatus == "Failed" {
			status = "🟡 FALHOU"
		} else if cronJob.ActiveJobs > 0 {
			status = "🔵 EXECUTANDO"
		}

		// Criar linha principal
		mainLine := fmt.Sprintf("  %s %s %s/%s", selection, status, cronJob.Namespace, cronJob.Name)

		// Aplicar estilo de seleção se for o item selecionado
		if i == a.model.SelectedIndex {
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			allLines = append(allLines, selectedStyle.Render(mainLine))
		} else {
			allLines = append(allLines, mainLine)
		}

		// Schedule e informações
		allLines = append(allLines, fmt.Sprintf("     %s", cronJob.ScheduleDesc))

		// Última execução
		lastExec := "Nunca executado"
		if cronJob.LastScheduleTime != nil {
			lastExec = fmt.Sprintf("Última: %s (%s)",
				cronJob.LastScheduleTime.Format("02/01 15:04"),
				cronJob.LastRunStatus)
		}
		allLines = append(allLines, fmt.Sprintf("     %s", lastExec))

		// Job Template
		if cronJob.JobTemplate != "" {
			allLines = append(allLines, fmt.Sprintf("     Função: %s", cronJob.JobTemplate))
		}

		allLines = append(allLines, "")
	}

	return allLines
}

// renderResponsiveCronJobPanel - Painel de CronJobs responsivo com scroll
func (a *App) renderResponsiveCronJobPanel(allLines []string) string {
	// Calcular largura responsiva baseada no conteúdo (maior linha)
	contentWidth := 0
	for _, line := range allLines {
		// Remover códigos de cor/estilo para calcular largura real
		cleanLine := lipgloss.NewStyle().UnsetBackground().UnsetForeground().Render(line)
		lineWidth := len([]rune(cleanLine))
		if lineWidth > contentWidth {
			contentWidth = lineWidth
		}
	}

	// Adicionar margem para bordas e espaçamento interno
	maxWidth := contentWidth + 6 // +6 para bordas e padding
	if maxWidth < 35 {
		maxWidth = 35 // Largura mínima
	}
	if maxWidth > 120 {
		maxWidth = 120 // Largura máxima para não ficar muito largo
	}

	// Altura responsiva baseada no conteúdo, até 35 linhas máximo
	totalLines := len(allLines)
	maxHeight := 35 // Limite máximo

	// Calcular altura dinâmica
	var availableHeight int
	if totalLines <= maxHeight-2 { // -2 para bordas
		// Tudo cabe - usar altura baseada no conteúdo
		availableHeight = totalLines + 2 // +2 para bordas
		if availableHeight < 5 {
			availableHeight = 5 // Altura mínima
		}
	} else {
		// Não cabe tudo - usar altura máxima e ativar scroll
		availableHeight = maxHeight
	}

	// Calcular quantas linhas podemos mostrar (descontando bordas)
	visibleLines := availableHeight - 2

	// Aplicar scroll com foco automático no item selecionado
	var displayLines []string
	var scrollInfo string

	if totalLines > visibleLines {
		// Scroll necessário - calcular posição do item selecionado
		selectedItemLine := a.calculateSelectedCronJobLinePosition(allLines)

		// Ajustar scroll para manter item selecionado visível
		a.adjustScrollToKeepCronJobVisible(selectedItemLine, visibleLines, totalLines)

		// Pegar apenas as linhas visíveis
		start := a.model.CronJobScrollOffset
		end := start + visibleLines
		displayLines = allLines[start:end]
		scrollInfo = fmt.Sprintf(" [%d-%d/%d]", start+1, end, totalLines)
	} else {
		// Tudo cabe
		displayLines = allLines
		a.model.CronJobScrollOffset = 0
	}

	// Juntar linhas para exibição
	content := strings.Join(displayLines, "\n")

	// Título com informação de scroll
	title := "CronJobs Disponíveis"
	if scrollInfo != "" {
		title += scrollInfo
	}

	return renderPanelWithTitle(content, title, maxWidth, availableHeight, primaryColor)
}

// calculateSelectedCronJobLinePosition - Calcula a posição da linha do CronJob selecionado
func (a *App) calculateSelectedCronJobLinePosition(allLines []string) int {
	// O header tem 5 linhas (cluster, total, linha vazia, controles, linha vazia)
	headerLines := 5

	// Cada CronJob tem geralmente 5 linhas (linha principal + schedule + última execução + função + linha vazia)
	if len(a.model.CronJobs) == 0 {
		return 0
	}

	selectedItemLine := headerLines + (a.model.SelectedIndex * 5) // 5 linhas por CronJob

	// Garantir que não exceda o número de linhas disponíveis
	if selectedItemLine >= len(allLines) {
		selectedItemLine = len(allLines) - 1
	}

	return selectedItemLine
}

// adjustScrollToKeepCronJobVisible - Ajusta o scroll para manter o CronJob selecionado visível
func (a *App) adjustScrollToKeepCronJobVisible(selectedItemLine, visibleLines, totalLines int) {
	// Se o item selecionado está antes da janela visível
	if selectedItemLine < a.model.CronJobScrollOffset {
		a.model.CronJobScrollOffset = selectedItemLine
	}

	// Se o item selecionado está depois da janela visível
	if selectedItemLine >= a.model.CronJobScrollOffset+visibleLines {
		a.model.CronJobScrollOffset = selectedItemLine - visibleLines + 1
	}

	// Garantir que o offset não seja negativo
	if a.model.CronJobScrollOffset < 0 {
		a.model.CronJobScrollOffset = 0
	}

	// Garantir que o offset não exceda o limite
	maxOffset := totalLines - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if a.model.CronJobScrollOffset > maxOffset {
		a.model.CronJobScrollOffset = maxOffset
	}
}

// calculateCronJobPanelSpacing - Calcula espaçamento dinâmico para o painel de CronJobs
func (a *App) calculateCronJobPanelSpacing() string {
	// Referência: painel com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// Calcular quantas linhas o painel CronJobs atual tem
	currentCronJobLines := a.calculateCurrentCronJobLines()

	// Calcular diferença
	difference := currentCronJobLines - referenceLines

	// Se o painel atual for maior que a referência, não adicionar espaçamento extra
	if difference >= 0 {
		return "\n" // Quebra de linha mínima entre painéis
	}

	// Adicionar linhas em branco para compensar a diferença
	spacingLines := -difference
	spacing := strings.Repeat("\n", spacingLines)

	return "\n" + spacing
}

// calculateCurrentCronJobLines - Calcula quantas linhas o painel CronJobs atual tem
func (a *App) calculateCurrentCronJobLines() int {
	if len(a.model.CronJobs) == 0 {
		return 5 // Altura mínima quando vazio
	}

	allLines := a.buildCronJobLines()
	totalLines := len(allLines)
	maxHeight := 35 // Limite máximo

	// Se tudo cabe
	if totalLines <= maxHeight-2 { // -2 para bordas
		availableHeight := totalLines + 2 // +2 para bordas
		if availableHeight < 5 {
			availableHeight = 5 // Altura mínima
		}
		return availableHeight
	} else {
		// Usar altura máxima quando há scroll
		return maxHeight
	}
}

// calculateCronJobEditingPanelSpacing - Calcula espaçamento dinâmico para a tela de edição de CronJob
func (a *App) calculateCronJobEditingPanelSpacing() string {
	// Referência: painel com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// O painel de edição de CronJob tem altura fixa de 18 + 2 bordas = 20 linhas
	currentEditingLines := 18 + 2 // altura definida na linha 80

	// Calcular diferença
	difference := currentEditingLines - referenceLines

	// Calcular espaçamento necessário
	var spacing strings.Builder

	if difference > 0 {
		// Painel é maior que a referência - espaçamento mínimo
		spacing.WriteString("\n")
	} else {
		// Painel é menor que a referência - adicionar espaço
		extraLines := -difference + 1 // +1 para o espaçamento base
		for i := 0; i < extraLines; i++ {
			spacing.WriteString("\n")
		}
	}

	return spacing.String()
}