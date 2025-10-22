package tui

import (
	"fmt"
	"strings"

	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/tui/layout"

	"github.com/charmbracelet/lipgloss"
)

// renderPanelWithTitle cria um painel com título integrado na borda superior
func renderPanelWithTitle(content, title string, minWidth, height int, borderColor lipgloss.Color) string {
	// Remove estilos do conteúdo para calcular largura real
	lines := strings.Split(content, "\n")
	maxLineLength := 0
	
	// Calcula a largura real de cada linha (sem estilos)
	for _, line := range lines {
		// Remove códigos de cor/estilo para calcular largura real
		cleanLine := strings.ReplaceAll(line, "\x1b", "")
		realLength := len([]rune(cleanLine))
		if realLength > maxLineLength {
			maxLineLength = realLength
		}
	}
	
	// Garante largura mínima
	titleLength := len([]rune(title))
	neededWidth := titleLength + 6 // título + espaços + padding
	if maxLineLength+4 > neededWidth {
		neededWidth = maxLineLength + 4
	}
	if minWidth > neededWidth {
		neededWidth = minWidth
	}
	
	// Calcula padding para o título
	titlePadding := (neededWidth - titleLength - 6) / 2
	if titlePadding < 0 {
		titlePadding = 0
	}
	
	// Constrói bordas
	topBorder := "╭" + strings.Repeat("─", titlePadding+1) + " " + title + " " + strings.Repeat("─", neededWidth-titleLength-titlePadding-5) + "╮"
	bottomBorder := "╰" + strings.Repeat("─", neededWidth-2) + "╯"
	
	// Constrói conteúdo
	var contentLines []string
	contentWidth := neededWidth - 4 // espaço interno
	
	for _, line := range lines {
		// Adiciona padding à direita para completar a largura
		paddedLine := "│ " + line
		// Calcula quantos espaços são necessários
		lineDisplayWidth := lipgloss.Width(line)
		spacesNeeded := contentWidth - lineDisplayWidth
		if spacesNeeded > 0 {
			paddedLine += strings.Repeat(" ", spacesNeeded)
		}
		paddedLine += " │"
		contentLines = append(contentLines, paddedLine)
	}
	
	// Preenche altura se necessário
	emptyLine := "│" + strings.Repeat(" ", neededWidth-2) + "│"
	for len(contentLines) < height-2 {
		contentLines = append(contentLines, emptyLine)
	}
	
	// Aplica cor apenas nos caracteres de borda
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	
	// Aplica cor na borda superior
	styledTopBorder := borderStyle.Render(topBorder)
	
	// Aplica cor nas linhas de conteúdo (apenas nos caracteres │)
	var styledContentLines []string
	for _, line := range contentLines {
		if len(line) > 0 {
			// Converte para runes para lidar corretamente com caracteres Unicode
			runes := []rune(line)
			if len(runes) > 0 {
				// Pega o primeiro caractere (│), aplica cor, depois o meio sem cor, depois o último (│) com cor
				firstChar := borderStyle.Render(string(runes[0]))
				lastChar := borderStyle.Render(string(runes[len(runes)-1]))
				middle := ""
				if len(runes) > 2 {
					middle = string(runes[1 : len(runes)-1])
				}
				styledLine := firstChar + middle + lastChar
				styledContentLines = append(styledContentLines, styledLine)
			} else {
				styledContentLines = append(styledContentLines, line)
			}
		} else {
			styledContentLines = append(styledContentLines, line)
		}
	}
	
	// Aplica cor na borda inferior
	styledBottomBorder := borderStyle.Render(bottomBorder)
	
	// Junta tudo
	return styledTopBorder + "\n" + strings.Join(styledContentLines, "\n") + "\n" + styledBottomBorder
}

// renderStatusPanelWithTitle cria um painel de status com largura específica e cálculo corrigido
func renderStatusPanelWithTitle(content, title string, minWidth, height int, borderColor lipgloss.Color) string {
	// 🔒 CONTAINER AUTO-CONTIDO COM DIMENSÕES FIXAS 140x15 🔒
	// Mantém dimensões consistentes conforme definição original
	const containerWidth = 140
	const containerHeight = 15

	// Parse do conteúdo
	lines := strings.Split(content, "\n")

	// 🏗️ BORDAS RESPONSIVAS - baseadas nos parâmetros da tela
	titleLength := len([]rune(title))
	titlePadding := (containerWidth - titleLength - 6) / 2
	if titlePadding < 0 {
		titlePadding = 1
	}

	leftDashes := titlePadding
	rightDashes := containerWidth - titleLength - titlePadding - 6
	if rightDashes < 1 {
		rightDashes = 1
	}

	topBorder := "╭" + strings.Repeat("─", leftDashes) + " " + title + " " + strings.Repeat("─", rightDashes) + "╮"
	bottomBorder := "╰" + strings.Repeat("─", containerWidth-2) + "╯"

	// 📦 CONTEÚDO RESPONSIVO - baseado nos parâmetros da tela
	var contentLines []string
	internalWidth := containerWidth - 4 // caracteres úteis (container - 4 para bordas e espaços)

	// 🔄 QUEBRA DE LINHAS INTELIGENTE - expande todas as linhas de entrada
	var expandedLines []string
	for _, line := range lines {
		if line == "" {
			expandedLines = append(expandedLines, "")
			continue
		}

		// Quebra linhas longas em múltiplas linhas
		lineDisplayWidth := lipgloss.Width(line)
		if lineDisplayWidth <= internalWidth {
			expandedLines = append(expandedLines, line)
		} else {
			// Quebra a linha em pedaços que cabem na largura
			runes := []rune(line)
			start := 0

			for start < len(runes) {
				end := start + internalWidth
				if end > len(runes) {
					end = len(runes)
				}

				// Tenta quebrar em um espaço para evitar cortar palavras
				if end < len(runes) {
					// Procura o último espaço antes do limite
					for i := end - 1; i > start && i > end-20; i-- {
						if runes[i] == ' ' {
							end = i
							break
						}
					}
				}

				chunk := string(runes[start:end])
				expandedLines = append(expandedLines, chunk)
				start = end

				// Pula espaços no início da próxima linha
				for start < len(runes) && runes[start] == ' ' {
					start++
				}
			}
		}
	}

	// Preenche conforme altura definida por cada tela
	for i := 0; i < containerHeight-2; i++ { // linhas internas baseadas na altura da tela
		var line string
		if i < len(expandedLines) {
			line = expandedLines[i]
		}

		// Calcula espaços necessários para completar a largura
		lineDisplayWidth := lipgloss.Width(line)
		spacesNeeded := internalWidth - lineDisplayWidth
		if spacesNeeded < 0 {
			spacesNeeded = 0
		}

		// Linha perfeitamente formatada com largura exata
		paddedLine := "│ " + line + strings.Repeat(" ", spacesNeeded) + " │"
		contentLines = append(contentLines, paddedLine)
	}

	// 🎨 Aplica cor apenas nos caracteres de borda
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Borda superior com cor
	styledTopBorder := borderStyle.Render(topBorder)

	// Linhas de conteúdo com bordas coloridas
	var styledContentLines []string
	for _, line := range contentLines {
		if len(line) > 0 {
			runes := []rune(line)
			if len(runes) >= 2 {
				// Primeiro e último caractere (│) com cor, meio sem cor
				firstChar := borderStyle.Render(string(runes[0]))
				lastChar := borderStyle.Render(string(runes[len(runes)-1]))
				middle := ""
				if len(runes) > 2 {
					middle = string(runes[1 : len(runes)-1])
				}
				styledLine := firstChar + middle + lastChar
				styledContentLines = append(styledContentLines, styledLine)
			} else {
				styledContentLines = append(styledContentLines, borderStyle.Render(line))
			}
		} else {
			styledContentLines = append(styledContentLines, line)
		}
	}

	// Borda inferior com cor
	styledBottomBorder := borderStyle.Render(bottomBorder)

	// 🚀 Resultado: Container auto-contido responsivo às necessidades de cada tela
	return styledTopBorder + "\n" + strings.Join(styledContentLines, "\n") + "\n" + styledBottomBorder
}

// Cores e estilos simplificados
var (
	primaryColor = lipgloss.Color("#007ACC")
	successColor = lipgloss.Color("#28A745")
	warningColor = lipgloss.Color("#FFC107")
	errorColor   = lipgloss.Color("#DC3545")
	mutedColor   = lipgloss.Color("#6C757D")
	textColor    = lipgloss.Color("#FFFFFF")

	// Estilos base
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			MarginBottom(1)

	listStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1).
			MarginRight(2)

	// Estilos específicos para cada tipo de painel
	clusterPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1).
			Width(60)

	namespacePanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1).
			Width(50).
			Height(25)

	selectedNamespacePanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(successColor).
			Padding(1).
			Width(50).
			Height(25)

	hpaPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1).
			Width(70).
			Height(25)

	selectedHpaPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(successColor).
			Padding(1).
			Width(70).
			Height(25)

	sessionPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1).
			Width(80)

	selectedItemStyle = lipgloss.NewStyle().
				Background(primaryColor).
				Foreground(textColor).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(textColor)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(successColor).
			Background(lipgloss.Color("#1a1a1a")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			MarginTop(1)
)

// renderSessionHeader renderiza o header com informações da sessão carregada
func (a *App) renderSessionHeader() string {
	if a.model.LoadedSessionName == "" {
		return ""
	}
	
	sessionInfo := fmt.Sprintf("📚 Sessão: %s", a.model.LoadedSessionName)
	
	// Aplicar estilo ao header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("6")).  // Cyan
		Background(lipgloss.Color("0")).  // Preto
		Bold(true).
		Padding(0, 1).
		Margin(0, 0, 1, 0)
	
	return headerStyle.Render(sessionInfo) + "\n"
}

// renderClusterSelection - Tela de seleção de clusters
func (a *App) renderClusterSelection() string {
	// Criar gerenciador de layout
	layoutMgr := layout.NewLayoutManager()

	// Preparar conteúdo dos painéis
	clusterContent := a.buildClusterListContent()

	// Criar painéis
	clusterPanel := layout.NewResponsivePanel("Clusters Kubernetes", clusterContent, layout.PrimaryColor, layoutMgr)
	statusPanel := a.renderStatusInfoPanel()

	// Header e ajuda
	sessionInfo := layout.TitleStyle.Render("🏗️  Seleção de Cluster") + "\n\n"
	helpText := layout.HelpStyle.Render("↑↓ Navegar • ENTER Selecionar • Ctrl+L Carregar • F5/R Recarregar • ? Ajuda • F4 Sair")

	// Construir layout de coluna única
	return layout.NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(clusterPanel.Render(), clusterPanel.GetActualHeight()).
		BuildSingleColumn(statusPanel)
}

// renderSessionSelection - Tela de seleção de sessões
func (a *App) renderSessionSelection() string {
	var content strings.Builder

	// Título com pasta atual
	title := "📚 Carregar Sessão"
	if a.model.CurrentFolder != "" {
		title = fmt.Sprintf("📚 Sessões - Pasta: %s", a.model.CurrentFolder)
	}
	content.WriteString(titleStyle.Render(title) + "\n\n")

	// Lista de sessões
	if len(a.model.LoadedSessions) == 0 {
		content.WriteString("Nenhuma sessão salva encontrada.\n")
		content.WriteString(helpStyle.Render("ESC Voltar"))
		return content.String()
	}

	sessionList := make([]string, len(a.model.LoadedSessions))
	for i, session := range a.model.LoadedSessions {
		createdAt := session.CreatedAt.Format("02/01/2006 15:04")
		
		// Detectar tipo de sessão e contar mudanças
		var sessionType string
		var changesCount int
		if len(session.NodePoolChanges) > 0 {
			sessionType = "🔧 Node Pools"
			changesCount = len(session.NodePoolChanges)
		} else {
			sessionType = "📊 HPAs"
			changesCount = len(session.Changes)
		}
		
		sessionInfo := fmt.Sprintf("%s\n   %s • %d mudanças • %s", 
			session.Name, sessionType, changesCount, createdAt)

		if i == a.model.SelectedSessionIdx {
			sessionList[i] = selectedItemStyle.Render(sessionInfo)
		} else {
			sessionList[i] = normalItemStyle.Render(sessionInfo)
		}
	}

	// Painel com título integrado na borda
	panelContent := strings.Join(sessionList, "\n\n")
	customPanel := renderPanelWithTitle(panelContent, "Sessões Salvas", 70, 18, primaryColor)
	content.WriteString(customPanel)

	// Interface de renome de sessão
	if a.model.RenamingSession {
		content.WriteString("\n\n")
		renameMsg := fmt.Sprintf("✏️ Renomear sessão '%s':", a.model.RenamingSessionName)
		renameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")) // Azul
		content.WriteString(renameStyle.Render(renameMsg))
		content.WriteString("\n")

		// Campo de entrada com cursor
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(0, 1).
			Width(40)

		inputText := a.model.NewSessionName
		if len(inputText) == 0 {
			inputText = " " // Para manter a altura da caixa
		}

		// Inserir cursor visual na posição correta
		displayText := a.insertCursorInText(inputText, a.model.CursorPosition)

		content.WriteString(inputStyle.Render(displayText))
		content.WriteString("\n")
		content.WriteString(helpStyle.Render("ENTER Confirmar • ESC Cancelar"))
	} else if a.model.ConfirmingDeletion {
		// Mensagem de confirmação de deleção
		content.WriteString("\n\n")
		confirmMsg := fmt.Sprintf("❌ Confirma a deleção da sessão '%s'?", a.model.DeletingSessionName)
		warningStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")) // Vermelho
		content.WriteString(warningStyle.Render(confirmMsg))
		content.WriteString("\n")
		content.WriteString(helpStyle.Render("Y Confirmar • N/ESC Cancelar"))
	} else {
		// Ajuda normal
		content.WriteString("\n")
		content.WriteString(helpStyle.Render("↑↓ Navegar • ENTER Aplicar • Ctrl+N/F2 Renomear • Ctrl+R Deletar • ESC Voltar"))
	}

	return content.String()
}

// renderSessionFolderSelection - Tela de seleção de pastas de sessão
func (a *App) renderSessionFolderSelection() string {
	var content strings.Builder

	// Título baseado no contexto
	if a.model.SavingToFolder {
		content.WriteString(titleStyle.Render("📁 Escolher Pasta para Salvar") + "\n\n")
	} else {
		content.WriteString(titleStyle.Render("📁 Escolher Pasta de Sessão") + "\n\n")
	}

	// Lista de pastas
	if len(a.model.SessionFolders) == 0 {
		content.WriteString("Carregando pastas...\n")
		if a.model.SavingToFolder {
			content.WriteString(helpStyle.Render("ESC Voltar ao salvamento"))
		} else {
			content.WriteString(helpStyle.Render("ESC Voltar"))
		}
		return content.String()
	}

	folderList := make([]string, len(a.model.SessionFolders))
	for i, folder := range a.model.SessionFolders {
		// Descrição baseada na pasta
		var description string
		switch folder {
		case "HPA-Upscale":
			description = "📈 Sessões para aumentar recursos HPA"
		case "HPA-Downscale":
			description = "📉 Sessões para diminuir recursos HPA"
		case "Node-Upscale":
			description = "⬆️  Sessões para aumentar node pools"
		case "Node-Downscale":
			description = "⬇️  Sessões para diminuir node pools"
		default:
			description = "📂 Sessões gerais"
		}

		folderInfo := fmt.Sprintf("%s\n   %s", folder, description)

		if i == a.model.SelectedFolderIdx {
			folderList[i] = selectedItemStyle.Render(folderInfo)
		} else {
			folderList[i] = normalItemStyle.Render(folderInfo)
		}
	}

	// Painel com título integrado na borda
	panelContent := strings.Join(folderList, "\n\n")
	customPanel := renderPanelWithTitle(panelContent, "Pastas de Sessão", 70, 18, primaryColor)
	content.WriteString(customPanel)

	// Ajuda baseada no contexto
	content.WriteString("\n")
	if a.model.SavingToFolder {
		content.WriteString(helpStyle.Render("↑↓ Navegar • ENTER Selecionar pasta • ESC Cancelar"))
	} else {
		content.WriteString(helpStyle.Render("↑↓ Navegar • ENTER Abrir pasta • ESC Voltar"))
	}

	return content.String()
}

// renderNamespaceSelection - Tela de seleção de namespaces
func (a *App) renderNamespaceSelection() string {
	// Criar gerenciador de layout
	layoutMgr := layout.NewLayoutManager()

	// Preparar conteúdo dos painéis
	leftContent := a.buildNamespaceListContent()
	rightContent := a.buildSelectedNamespacesContent()

	// Criar painéis responsivos
	leftPanel := layout.NewResponsivePanel("Namespaces Disponíveis", leftContent, layout.PrimaryColor, layoutMgr)
	rightPanel := layout.NewResponsivePanel("Namespaces Selecionados", rightContent, layout.SuccessColor, layoutMgr)
	statusPanel := a.renderStatusInfoPanel()

	// Header e ajuda
	var sessionInfo string
	if a.model.SelectedCluster != nil {
		sessionInfo = layout.TitleStyle.Render(fmt.Sprintf("📁 Namespaces - Cluster: %s", a.model.SelectedCluster.Name)) + "\n\n"
	}

	systemStatus := "ocultos"
	if a.model.ShowSystemNamespaces {
		systemStatus = "exibidos"
	}
	helpText := layout.HelpStyle.Render(fmt.Sprintf("↑↓ Navegar • SPACE Selecionar • TAB Alternar painel • Ctrl+R Remover • Ctrl+N Node pools • F8 Recursos • F9 CronJobs\nS Toggle sistema (%s) • ENTER Continuar • ? Ajuda • ESC Voltar", systemStatus))

	// Construir layout
	return layout.NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(leftPanel.Render(), leftPanel.GetActualHeight()).
		AddPanel(rightPanel.Render(), rightPanel.GetActualHeight()).
		BuildTwoColumn(statusPanel)
}

// renderNamespaceList - Lista de namespaces disponíveis
func (a *App) renderNamespaceList() string {
	if len(a.model.Namespaces) == 0 {
		return renderPanelWithTitle("Carregando...", "Namespaces Disponíveis", 70, 18, primaryColor)
	}

	var items []string
	for i, ns := range a.model.Namespaces {
		marker := "  "
		if ns.Selected {
			marker = "✓ "
		}

		hpaInfo := ""
		hpaIndicator := ""
		if ns.HPACount > 0 {
			hpaInfo = fmt.Sprintf(" (%d HPAs)", ns.HPACount)
			hpaIndicator = "🎯"
		} else if ns.HPACount == 0 {
			hpaInfo = " (sem HPAs)"
			hpaIndicator = "❌"
		} else {
			// -1 ou não carregado ainda
			hpaInfo = " (carregando...)"
			hpaIndicator = "⏳"
		}

		item := fmt.Sprintf("%s%s %s%s", marker, hpaIndicator, ns.Name, hpaInfo)

		if i == a.model.SelectedIndex && a.model.ActivePanel == models.PanelNamespaces {
			items = append(items, selectedItemStyle.Render(item))
		} else {
			items = append(items, normalItemStyle.Render(item))
		}
	}

	content := strings.Join(items, "\n")
	return renderPanelWithTitle(content, "Namespaces Disponíveis", 70, 18, primaryColor)
}

// renderSelectedNamespacesList - Lista de namespaces selecionados
func (a *App) renderSelectedNamespacesList() string {
	if len(a.model.SelectedNamespaces) == 0 {
		return renderPanelWithTitle("Nenhum namespace selecionado", "Namespaces Selecionados", 70, 18, successColor)
	}

	var items []string
	for i, ns := range a.model.SelectedNamespaces {
		hpaInfo := ""
		if ns.HPACount > 0 {
			hpaInfo = fmt.Sprintf(" (%d HPAs)", ns.HPACount)
		}

		item := fmt.Sprintf("📁 %s%s", ns.Name, hpaInfo)

		if i == a.model.CurrentNamespaceIdx && a.model.ActivePanel == models.PanelSelectedNamespaces {
			items = append(items, selectedItemStyle.Render(item))
		} else {
			items = append(items, normalItemStyle.Render(item))
		}
	}

	content := strings.Join(items, "\n")
	return renderPanelWithTitle(content, "Namespaces Selecionados", 70, 18, successColor)
}

// renderHPASelection - Tela de seleção de HPAs usando novo sistema de layout
func (a *App) renderHPASelection() string {
	// Se estamos digitando nome da sessão, mostrar prompt (não migrado)
	if a.model.EnteringSessionName {
		var content strings.Builder
		content.WriteString(titleStyle.Render("💾 Salvando Sessão") + "\n\n")
		content.WriteString("Digite o nome da sessão:\n")
		displayName := a.insertCursorInText(a.model.SessionName, a.model.CursorPosition)
		content.WriteString(selectedItemStyle.Render(displayName) + "\n\n")
		content.WriteString(helpStyle.Render("ENTER Salvar • ESC Cancelar"))
		return content.String()
	}

	// Criar gerenciador de layout
	layoutMgr := layout.NewLayoutManager()

	// Preparar dados dos painéis
	leftContent := a.buildHPAListContent()

	// Criar painel esquerdo
	leftPanel := layout.NewResponsivePanel("HPAs Disponíveis", leftContent, layout.PrimaryColor, layoutMgr)

	// Criar painel direito com SCROLL (usa nossa função custom)
	rightPanel := a.renderSelectedHPAsList()
	statusPanel := a.renderStatusInfoPanel()

	// Session info e help
	sessionInfo := a.renderSessionHeader()

	currentNs := ""
	if a.model.CurrentNamespaceIdx < len(a.model.SelectedNamespaces) {
		currentNs = a.model.SelectedNamespaces[a.model.CurrentNamespaceIdx].Name
	}
	sessionInfo += titleStyle.Render(fmt.Sprintf("🎯 HPAs - Namespace: %s", currentNs)) + "\n\n"

	var help string
	if a.model.ActivePanel == models.PanelHPAs {
		help = "↑↓ Navegar • SPACE Selecionar • TAB Painel direito • ? Ajuda • ESC Voltar"
		if len(a.model.SelectedHPAs) > 0 {
			help = "↑↓ Navegar • SPACE Selecionar • TAB Painel direito • Ctrl+S Salvar sessão • ? Ajuda • ESC Voltar"
		}
	} else {
		help = "↑↓ Navegar • ENTER Editar • Ctrl+R Remover • TAB Painel esquerdo • ? Ajuda • ESC Voltar"
		if len(a.model.SelectedHPAs) > 0 {
			help = "↑↓ Navegar • ENTER Editar • Ctrl+R Remover • TAB Painel esquerdo\nCtrl+D Aplicar individual • Ctrl+U Aplicar todos • ? Ajuda • ESC Voltar"
		}
	}
	helpText := helpStyle.Render(help)

	// Construir layout usando LayoutBuilder
	return layout.NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(leftPanel.Render(), leftPanel.GetActualHeight()).
		AddPanel(rightPanel, 35). // rightPanel já é string renderizada com scroll, altura fixa 35
		BuildTwoColumn(statusPanel)
}

// buildHPAListContent constrói o conteúdo do painel esquerdo (HPAs Disponíveis)
func (a *App) buildHPAListContent() []string {
	var content []string

	if len(a.model.HPAs) == 0 {
		content = append(content, "Nenhum HPA encontrado")
		return content
	}

	// Header
	if a.model.CurrentNamespaceIdx < len(a.model.SelectedNamespaces) {
		namespace := a.model.SelectedNamespaces[a.model.CurrentNamespaceIdx].Name
		content = append(content, fmt.Sprintf("📁 Namespace: %s", namespace))
		content = append(content, fmt.Sprintf("📊 Total: %d HPAs", len(a.model.HPAs)))
		content = append(content, "")
	}

	// Lista de HPAs
	for i, hpa := range a.model.HPAs {
		selection := "◯"
		if hpa.Selected {
			selection = "●"
		}

		status := ""
		if hpa.Modified {
			status = "✨"
		}

		// Criar linha do HPA
		hpaLine := fmt.Sprintf("  %s 🎯 %s%s", selection, hpa.Name, status)

		// Aplicar estilo de seleção se for o item selecionado
		if i == a.model.SelectedIndex && a.model.ActivePanel == models.PanelHPAs {
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content = append(content, selectedStyle.Render(hpaLine))
		} else {
			content = append(content, hpaLine)
		}
		content = append(content, fmt.Sprintf("     Min: %d | Max: %d | Current: %d", getIntValue(hpa.MinReplicas), hpa.MaxReplicas, hpa.CurrentReplicas))
	}

	return content
}

// buildSelectedHPAsContent constrói o conteúdo do painel direito (HPAs Selecionados)
func (a *App) buildSelectedHPAsContent() []string {
	var content []string

	if len(a.model.SelectedHPAs) == 0 {
		content = append(content, "Nenhum HPA selecionado")
		return content
	}

	// Agrupar HPAs por namespace
	namespaceGroups := make(map[string][]models.HPA)
	var namespaceOrder []string

	for _, hpa := range a.model.SelectedHPAs {
		if _, exists := namespaceGroups[hpa.Namespace]; !exists {
			namespaceOrder = append(namespaceOrder, hpa.Namespace)
			namespaceGroups[hpa.Namespace] = make([]models.HPA, 0)
		}
		namespaceGroups[hpa.Namespace] = append(namespaceGroups[hpa.Namespace], hpa)
	}

	currentIndex := 0
	for _, namespace := range namespaceOrder {
		hpas := namespaceGroups[namespace]

		// Separador de namespace
		content = append(content, "")
		content = append(content, fmt.Sprintf("───────── %s ─────────", namespace))

		// HPAs do namespace
		for _, hpa := range hpas {
			// Indicador de aplicação
			appliedIndicator := ""
			if hpa.AppliedCount > 0 {
				if hpa.AppliedCount == 1 {
					appliedIndicator = " ●"
				} else {
					appliedIndicator = fmt.Sprintf(" ●%d", hpa.AppliedCount)
				}
			}

			// Formatação do HPA
			minRep := fmt.Sprintf("%d", getIntValue(hpa.MinReplicas))
			if hpa.Modified {
				minRep += "✨"
			}

			// Criar linhas do HPA
			hpaMainLine := fmt.Sprintf("  🎯 %s", hpa.Name)
			hpaDetailLine := fmt.Sprintf("     Min:%s Max:%d Curr:%d%s", minRep, hpa.MaxReplicas, hpa.CurrentReplicas, appliedIndicator)

			// Aplicar estilo de seleção se for o item selecionado
			if currentIndex == a.model.SelectedIndex && a.model.ActivePanel == models.PanelSelectedHPAs {
				selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
				content = append(content, selectedStyle.Render(hpaMainLine))
			} else {
				content = append(content, hpaMainLine)
			}
			content = append(content, hpaDetailLine)

			// Status detalhado de rollouts
			var rolloutLines []string

			// Deployment rollout
			deployStatus := "❌"
			if hpa.PerformRollout {
				deployStatus = "✅"
			}
			rolloutLines = append(rolloutLines, fmt.Sprintf("Deployment:%s", deployStatus))

			// DaemonSet rollout
			daemonStatus := "❌"
			if hpa.PerformDaemonSetRollout {
				daemonStatus = "✅"
			}
			rolloutLines = append(rolloutLines, fmt.Sprintf("DaemonSet:%s", daemonStatus))

			// StatefulSet rollout
			statefulStatus := "❌"
			if hpa.PerformStatefulSetRollout {
				statefulStatus = "✅"
			}
			rolloutLines = append(rolloutLines, fmt.Sprintf("StatefulSet:%s", statefulStatus))

			// Combinar tudo em uma linha
			content = append(content, fmt.Sprintf("     Rollout: %s", strings.Join(rolloutLines, " ")))

			currentIndex++
		}
	}

	return content
}

// buildStatusContent constrói o conteúdo do painel de status usando o StatusContainer
func (a *App) buildStatusContent() string {
	statusContainer := a.model.StatusContainer

	// Migrar mensagens de erro e sucesso para o StatusContainer (apenas uma vez)
	if a.model.Error != "" {
		statusContainer.AddError("system", a.model.Error)
		a.model.Error = "" // Limpar após adicionar ao container
	}
	if a.model.SuccessMsg != "" {
		statusContainer.AddSuccess("system", a.model.SuccessMsg)
		a.model.SuccessMsg = "" // Limpar após adicionar ao container
	}

	// Renderizar o container usando o novo sistema
	return statusContainer.Render()
}

// renderHPAList - Lista de HPAs disponíveis
func (a *App) renderHPAList() string {
	if len(a.model.HPAs) == 0 {
		return renderPanelWithTitle("Carregando HPAs...", "HPAs Disponíveis", 70, 18, primaryColor)
	}

	var items []string
	for i, hpa := range a.model.HPAs {
		marker := "  "
		if hpa.Selected {
			marker = "✓ "
		}

		status := ""
		if hpa.Modified {
			status = " ✨"
		}

		minRep := "?"
		if hpa.MinReplicas != nil {
			minRep = fmt.Sprintf("%d", *hpa.MinReplicas)
		}

		item := fmt.Sprintf("%s%s (Min:%s Max:%d Curr:%d)%s",
			marker, hpa.Name, minRep, hpa.MaxReplicas, hpa.CurrentReplicas, status)

		if i == a.model.SelectedIndex && a.model.ActivePanel == models.PanelHPAs {
			items = append(items, selectedItemStyle.Render(item))
		} else {
			items = append(items, normalItemStyle.Render(item))
		}
	}

	content := strings.Join(items, "\n")
	return renderPanelWithTitle(content, "HPAs Disponíveis", 70, 18, primaryColor)
}

// renderSelectedHPAsList - Lista de HPAs selecionados agrupados por namespace com scroll responsivo
func (a *App) renderSelectedHPAsList() string {
	if len(a.model.SelectedHPAs) == 0 {
		// Usar função responsiva mesmo quando vazio
		emptyLines := []string{"Nenhum HPA selecionado"}
		return a.renderResponsiveHPASelectedPanel(emptyLines)
	}

	// Agrupar HPAs por namespace
	namespaceGroups := make(map[string][]models.HPA)
	var namespaceOrder []string

	for _, hpa := range a.model.SelectedHPAs {
		if _, exists := namespaceGroups[hpa.Namespace]; !exists {
			namespaceOrder = append(namespaceOrder, hpa.Namespace)
			namespaceGroups[hpa.Namespace] = make([]models.HPA, 0)
		}
		namespaceGroups[hpa.Namespace] = append(namespaceGroups[hpa.Namespace], hpa)
	}

	var allLines []string
	currentIndex := 0

	a.debugLog("🎨 RENDERIZANDO HPAs Selecionados: Total=%d HPAs, %d namespaces", len(a.model.SelectedHPAs), len(namespaceOrder))

	// Renderizar cada grupo de namespace
	for nsIdx, namespace := range namespaceOrder {
		hpas := namespaceGroups[namespace]

		a.debugLog("🎨 Namespace[%d]=%s: %d HPAs, allLines=%d", nsIdx, namespace, len(hpas), len(allLines))

		// Cabeçalho do namespace
		separator := fmt.Sprintf("───────── %s ─────────", namespace)
		allLines = append(allLines, "")  // Linha em branco antes do separador
		allLines = append(allLines, lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(separator))

		a.debugLog("🎨   Adicionou cabeçalho (2 linhas), allLines=%d", len(allLines))

		// HPAs do namespace
		for hpaIdx, hpa := range hpas {
			a.debugLog("🎨   HPA[%d]=%s, currentIndex=%d, allLines=%d", hpaIdx, hpa.Name, currentIndex, len(allLines))
			status := ""
			if hpa.Modified {
				status = " ✨"
			}

			minRep := "?"
			if hpa.MinReplicas != nil {
				minRep = fmt.Sprintf("%d", *hpa.MinReplicas)
			}

			// Indicador de aplicação
			appliedIndicator := ""
			if hpa.AppliedCount > 0 {
				if hpa.AppliedCount == 1 {
					appliedIndicator = " ●"
				} else {
					appliedIndicator = fmt.Sprintf(" ●%d", hpa.AppliedCount)
				}
			}

			// Status detalhado de rollouts
			var rolloutLines []string

			// Deployment rollout
			deployStatus := "❌"
			if hpa.PerformRollout {
				deployStatus = "✅"
			}
			rolloutLines = append(rolloutLines, fmt.Sprintf("Deployment:%s", deployStatus))

			// DaemonSet rollout
			daemonStatus := "❌"
			if hpa.PerformDaemonSetRollout {
				daemonStatus = "✅"
			}
			rolloutLines = append(rolloutLines, fmt.Sprintf("DaemonSet:%s", daemonStatus))

			// StatefulSet rollout
			statefulStatus := "❌"
			if hpa.PerformStatefulSetRollout {
				statefulStatus = "✅"
			}
			rolloutLines = append(rolloutLines, fmt.Sprintf("StatefulSet:%s", statefulStatus))

			// Verificar se este HPA está selecionado
			isSelected := currentIndex == a.model.SelectedIndex && a.model.ActivePanel == models.PanelSelectedHPAs

			// Formatação simplificada sem mostrar namespace (já está no cabeçalho)
			lines := []string{
				fmt.Sprintf("🎯 %s", hpa.Name),
				fmt.Sprintf("   Min:%s Max:%d Curr:%d%s%s", minRep, hpa.MaxReplicas, hpa.CurrentReplicas, status, appliedIndicator),
				fmt.Sprintf("   Rollout: %s", strings.Join(rolloutLines, " ")),
			}

			for lineIdx, line := range lines {
				if isSelected {
					a.debugLog("🎨     Linha[%d] SELECIONADA: allLines=%d", lineIdx, len(allLines))
					allLines = append(allLines, selectedItemStyle.Render(line))
				} else {
					allLines = append(allLines, normalItemStyle.Render(line))
				}
			}

			a.debugLog("🎨   Finalizou HPA (3 linhas), allLines=%d, currentIndex=%d", len(allLines), currentIndex)
			currentIndex++
		}
	}

	a.debugLog("🎨 TOTAL RENDERIZADO: allLines=%d, currentIndex=%d", len(allLines), currentIndex)

	// Usar painel responsivo com scroll
	return a.renderResponsiveHPASelectedPanel(allLines)
}

// renderStatusInfoPanel - Painel unificado usando StatusContainer
func (a *App) renderStatusInfoPanel() string {
	// Usar o StatusContainer com dimensões fixas 140x15
	return a.model.StatusContainer.Render()
}


// renderResponsiveHPASelectedPanel - Painel de HPAs Selecionados responsivo com scroll
func (a *App) renderResponsiveHPASelectedPanel(allLines []string) string {
	// DIMENSÕES BASE: 70 colunas x 18 linhas (mínimo)
	const minWidth = 70
	const minHeight = 18

	// ALTURA MÁXIMA: 35 linhas para scroll
	const maxHeight = 35

	totalLines := len(allLines)

	// Calcular largura responsiva baseada no conteúdo (mínimo 70)
	maxWidth := minWidth
	for _, line := range allLines {
		lineLen := len([]rune(line))
		if lineLen > maxWidth {
			maxWidth = lineLen
		}
	}

	// Calcular altura responsiva baseada no conteúdo (mínimo 18, máximo 35)
	availableHeight := totalLines + 2 // +2 para bordas
	if availableHeight < minHeight {
		availableHeight = minHeight
	}
	if availableHeight > maxHeight {
		availableHeight = maxHeight
	}

	// Calcular quantas linhas podemos mostrar (descontando bordas)
	visibleLines := availableHeight - 2 // 35 - 2 = 33 linhas visíveis

	a.debugLog("🖼️ renderResponsiveHPASelectedPanel: totalLines=%d, availableHeight=%d, visibleLines=%d, offset=%d",
		totalLines, availableHeight, visibleLines, a.model.HPASelectedScrollOffset)

	// Aplicar scroll com foco automático no item selecionado
	var displayLines []string
	var scrollInfo string

	if totalLines > visibleLines {
		// Scroll necessário - calcular posição do namespace e HPA selecionado
		namespaceStart, namespaceEnd, selectedHPALine := a.calculateSelectedHPALinePosition(allLines)

		a.debugLog("🖼️ ANTES scroll: namespace %d--%d, selectedHPA=%d, offset atual=%d",
			namespaceStart, namespaceEnd, selectedHPALine, a.model.HPASelectedScrollOffset)

		// Ajustar scroll para exibir todo o namespace
		a.adjustScrollToKeepItemVisible(namespaceStart, namespaceEnd, selectedHPALine, visibleLines, totalLines)

		a.debugLog("🖼️ DEPOIS scroll: offset=%d", a.model.HPASelectedScrollOffset)

		// Pegar apenas as linhas visíveis
		start := a.model.HPASelectedScrollOffset
		end := start + visibleLines
		if end > totalLines {
			end = totalLines
		}
		displayLines = allLines[start:end]
		scrollInfo = fmt.Sprintf(" [%d-%d/%d]", start+1, end, totalLines)

		a.debugLog("🖼️ EXIBINDO linhas: %d--%d de %d", start, end, totalLines)
	} else {
		// Tudo cabe
		displayLines = allLines
		a.model.HPASelectedScrollOffset = 0
		a.debugLog("🖼️ TODO conteúdo cabe, sem scroll necessário")
	}

	// Juntar linhas para exibição
	content := strings.Join(displayLines, "\n")

	// Título com informação de scroll
	title := "HPAs Selecionados"
	if scrollInfo != "" {
		title += scrollInfo
	}

	return renderPanelWithTitle(content, title, maxWidth, availableHeight, successColor)
}

// calculateLeftPanelSpacing - Calcula espaçamento dinâmico para o painel esquerdo (HPAs Disponíveis)
func (a *App) calculateLeftPanelSpacing() string {
	// Referência: painel HPAs com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// Calcular quantas linhas o painel HPAs disponíveis tem
	currentHPALines := a.calculateCurrentHPAListLines()

	// Calcular diferença
	difference := currentHPALines - referenceLines

	a.debugLog("🔍 LEFT SPACING: currentLines=%d, reference=%d, difference=%d", currentHPALines, referenceLines, difference)

	// Calcular espaçamento necessário
	var spacing strings.Builder

	if difference > 0 {
		// Painel é maior que a referência - subtrair espaço
		// Começar com espaçamento base mínimo
		spacing.WriteString("\n") // Pelo menos uma linha
	} else {
		// Painel é menor que a referência - adicionar espaço
		extraLines := -difference + 1 // +1 para o espaçamento base
		for i := 0; i < extraLines; i++ {
			spacing.WriteString("\n")
		}
	}

	return spacing.String()
}

// calculateRightPanelSpacing - Calcula espaçamento dinâmico para o painel direito (HPAs Selecionados)
func (a *App) calculateRightPanelSpacing() string {
	// Referência: painel HPAs com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// Calcular quantas linhas o painel HPAs selecionados tem
	currentSelectedHPALines := a.calculateCurrentSelectedHPALines()

	// Calcular diferença
	difference := currentSelectedHPALines - referenceLines

	a.debugLog("🔍 RIGHT SPACING: currentLines=%d, reference=%d, difference=%d", currentSelectedHPALines, referenceLines, difference)


	// Calcular espaçamento necessário
	var spacing strings.Builder

	if difference > 0 {
		// Painel é maior que a referência - subtrair espaço
		// Começar com espaçamento base mínimo
		spacing.WriteString("\n") // Pelo menos uma linha
	} else {
		// Painel é menor que a referência - adicionar espaço
		extraLines := -difference + 1 // +1 para o espaçamento base
		for i := 0; i < extraLines; i++ {
			spacing.WriteString("\n")
		}
	}

	return spacing.String()
}

// calculateNodePoolLeftPanelSpacing - Calcula espaçamento dinâmico para o painel esquerdo (Node Pools Disponíveis)
func (a *App) calculateNodePoolLeftPanelSpacing() string {
	// Referência: painel Node Pools com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// Calcular quantas linhas o painel Node Pools disponíveis tem
	currentNodePoolLines := a.calculateCurrentNodePoolListLines()

	// Calcular diferença
	difference := currentNodePoolLines - referenceLines

	// Calcular espaçamento necessário
	var spacing strings.Builder

	if difference > 0 {
		// Painel é maior que a referência - subtrair espaço
		// Começar com espaçamento base mínimo
		spacing.WriteString("\n") // Pelo menos uma linha
	} else {
		// Painel é menor que a referência - adicionar espaço
		extraLines := -difference + 1 // +1 para o espaçamento base
		for i := 0; i < extraLines; i++ {
			spacing.WriteString("\n")
		}
	}

	return spacing.String()
}

// calculateNodePoolRightPanelSpacing - Calcula espaçamento dinâmico para o painel direito (Node Pools Selecionados)
func (a *App) calculateNodePoolRightPanelSpacing() string {
	// Referência: painel Node Pools com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// Calcular quantas linhas o painel Node Pools selecionados tem
	currentSelectedNodePoolLines := a.calculateCurrentSelectedNodePoolLines()

	// Calcular diferença
	difference := currentSelectedNodePoolLines - referenceLines

	// Calcular espaçamento necessário
	var spacing strings.Builder

	if difference > 0 {
		// Painel é maior que a referência - subtrair espaço
		// Começar com espaçamento base mínimo
		spacing.WriteString("\n") // Pelo menos uma linha
	} else {
		// Painel é menor que a referência - adicionar espaço
		extraLines := -difference + 1 // +1 para o espaçamento base
		for i := 0; i < extraLines; i++ {
			spacing.WriteString("\n")
		}
	}

	return spacing.String()
}

// calculateEditingPanelSpacing - Calcula espaçamento dinâmico para telas de edição
func (a *App) calculateEditingPanelSpacing() string {
	// Referência: painel com 20 linhas (18 linhas de conteúdo + 2 bordas)
	referenceLines := 20

	// Todos os painéis de edição agora têm altura 18 + 2 bordas = 20 linhas
	currentEditingLines := 20

	// Calcular diferença
	difference := currentEditingLines - referenceLines

	// Calcular espaçamento necessário
	var spacing strings.Builder

	if difference > 0 {
		// Painel é maior que a referência - subtrair espaço
		// Começar com espaçamento base mínimo
		spacing.WriteString("\n") // Pelo menos uma linha
	} else {
		// Painel é menor que a referência - adicionar espaço
		extraLines := -difference + 1 // +1 para o espaçamento base
		for i := 0; i < extraLines; i++ {
			spacing.WriteString("\n")
		}
	}

	return spacing.String()
}

// calculateHPAEditingPanelLines - Calcula altura dos painéis de edição de HPA
func (a *App) calculateHPAEditingPanelLines() int {
	// HPA tem dois painéis lado a lado, usar uma altura média/estimada
	// Painel principal: ~8 campos + bordas = 10 linhas
	// Painel recursos: ~6 campos + bordas = 8 linhas
	// Usar o maior dos dois
	mainPanelLines := 10
	resourcePanelLines := 8

	if mainPanelLines > resourcePanelLines {
		return mainPanelLines
	}
	return resourcePanelLines
}

// calculateNodePoolEditingPanelLines - Calcula altura do painel de edição de Node Pool
func (a *App) calculateNodePoolEditingPanelLines() int {
	// Node Pool tem um painel fixo de altura 20 (definido na linha 1839)
	return 20
}

// calculateCurrentHPAListLines - Calcula quantas linhas o painel HPAs Disponíveis atual tem
func (a *App) calculateCurrentHPAListLines() int {
	// Sistema fixo: sempre retorna 18 linhas para manter compatibilidade com espaçamento
	return 18
}

// calculateCurrentSelectedHPALines - Calcula quantas linhas o painel HPAs Selecionados atual tem
func (a *App) calculateCurrentSelectedHPALines() int {
	// Sistema fixo: sempre retorna 18 linhas para manter compatibilidade com espaçamento
	return 18
}

// calculateCurrentNodePoolListLines - Calcula quantas linhas o painel Node Pools Disponíveis atual tem
func (a *App) calculateCurrentNodePoolListLines() int {
	// Sistema fixo: sempre retorna 20 linhas para manter compatibilidade com espaçamento
	return 20
}

// calculateCurrentSelectedNodePoolLines - Calcula quantas linhas o painel Node Pools Selecionados atual tem
func (a *App) calculateCurrentSelectedNodePoolLines() int {
	// Sistema fixo: sempre retorna 20 linhas para manter compatibilidade com espaçamento
	return 20
}

// calculateSelectedHPALinePosition - Calcula em qual linha está o HPA selecionado
// Retorna: (startLine, endLine, selectedHPALine) - namespace completo + linha específica do HPA
func (a *App) calculateSelectedHPALinePosition(allLines []string) (int, int, int) {
	if a.model.ActivePanel != models.PanelSelectedHPAs {
		return 0, 0, 0 // Se não está no painel HPAs, não há item selecionado
	}

	selectedIndex := a.model.SelectedIndex
	if selectedIndex < 0 || selectedIndex >= len(a.model.SelectedHPAs) {
		return 0, 0, 0
	}

	a.debugLog("📐 Calculando posição para SelectedIndex=%d, Total HPAs=%d", selectedIndex, len(a.model.SelectedHPAs))

	// Calcular posição baseada na estrutura do painel
	currentLine := 0
	currentHPAIndex := 0

	// Agrupar HPAs por namespace (mesmo código da renderização)
	namespaceGroups := make(map[string][]models.HPA)
	var namespaceOrder []string

	for _, hpa := range a.model.SelectedHPAs {
		if _, exists := namespaceGroups[hpa.Namespace]; !exists {
			namespaceOrder = append(namespaceOrder, hpa.Namespace)
			namespaceGroups[hpa.Namespace] = make([]models.HPA, 0)
		}
		namespaceGroups[hpa.Namespace] = append(namespaceGroups[hpa.Namespace], hpa)
	}

	a.debugLog("📐 Grupos: %d namespaces", len(namespaceOrder))

	// Percorrer grupos para encontrar posição do item selecionado
	for nsIdx, namespace := range namespaceOrder {
		hpas := namespaceGroups[namespace]

		a.debugLog("📐 Namespace[%d]=%s: %d HPAs, currentLine=%d, currentHPAIndex=%d",
			nsIdx, namespace, len(hpas), currentLine, currentHPAIndex)

		// Linha inicial do namespace (cabeçalho)
		namespaceStartLine := currentLine

		// Adicionar linhas do cabeçalho do namespace
		currentLine += 2 // linha em branco + separador

		// Verificar cada HPA deste namespace
		selectedHPALine := 0
		foundInThisNamespace := false
		for hpaIdx := range hpas {
			a.debugLog("📐   HPA[%d] do namespace %s: currentHPAIndex=%d, selectedIndex=%d, currentLine=%d",
				hpaIdx, namespace, currentHPAIndex, selectedIndex, currentLine)

			if currentHPAIndex == selectedIndex {
				selectedHPALine = currentLine
				foundInThisNamespace = true
				a.debugLog("📐 ✅ ENCONTRADO! HPA em linha %d do namespace %s", selectedHPALine, namespace)
			}
			currentLine += 3 // 3 linhas por HPA
			currentHPAIndex++
		}

		// Se encontramos o HPA neste namespace, retornar o range completo do namespace
		if foundInThisNamespace {
			namespaceEndLine := currentLine - 1 // Última linha do último HPA do namespace
			a.debugLog("📐 🎯 NAMESPACE COMPLETO: start=%d, end=%d, selectedHPA=%d",
				namespaceStartLine, namespaceEndLine, selectedHPALine)
			return namespaceStartLine, namespaceEndLine, selectedHPALine
		}
	}

	a.debugLog("📐 ⚠️ NÃO ENCONTRADO! Retornando 0,0,0")
	return 0, 0, 0
}

// adjustScrollToKeepItemVisible - Ajusta scroll para EXIBIR TODO O NAMESPACE
func (a *App) adjustScrollToKeepItemVisible(namespaceStart, namespaceEnd, selectedHPALine, visibleLines, totalLines int) {
	// Limites do scroll
	maxOffset := totalLines - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}

	currentStart := a.model.HPASelectedScrollOffset
	currentEnd := currentStart + visibleLines - 1

	namespaceHeight := namespaceEnd - namespaceStart + 1

	a.debugLog("🔍 NAMESPACE - start:%d, end:%d, height:%d, selectedHPA:%d, window:%d--%d, visibleLines:%d",
		namespaceStart, namespaceEnd, namespaceHeight, selectedHPALine, currentStart, currentEnd, visibleLines)

	// Verificar se o namespace completo cabe na janela visível
	if namespaceHeight <= visibleLines {
		// Namespace completo cabe! Tentar centralizá-lo
		centerOffset := namespaceStart - (visibleLines-namespaceHeight)/2
		if centerOffset < 0 {
			centerOffset = 0
		}
		if centerOffset > maxOffset {
			centerOffset = maxOffset
		}
		a.model.HPASelectedScrollOffset = centerOffset
		a.debugLog("🎯 NAMESPACE COMPLETO CABE - centralizando em offset: %d", centerOffset)
	} else {
		// Namespace não cabe completamente - priorizar mostrar o HPA selecionado + contexto
		// Tentar posicionar o HPA selecionado no terço superior da tela
		targetPosition := visibleLines / 3
		idealOffset := selectedHPALine - targetPosition

		// Garantir que não cortamos o início do namespace desnecessariamente
		if idealOffset < namespaceStart {
			idealOffset = namespaceStart
		}

		// Garantir limites válidos
		if idealOffset < 0 {
			idealOffset = 0
		}
		if idealOffset > maxOffset {
			idealOffset = maxOffset
		}

		a.model.HPASelectedScrollOffset = idealOffset
		a.debugLog("🎯 NAMESPACE GRANDE - HPA no terço superior, offset: %d", idealOffset)
	}
}

// adjustHPASelectedScrollToKeepItemVisible - Ajusta scroll do painel HPAs selecionados para manter item visível
func (a *App) adjustHPASelectedScrollToKeepItemVisible() {
	if a.model.ActivePanel != models.PanelSelectedHPAs || len(a.model.SelectedHPAs) == 0 {
		return
	}

	// Simular a construção das linhas para calcular a posição
	allLines := a.buildSelectedHPAsContent()
	totalLines := len(allLines)

	// Altura visível do painel (altura máxima - bordas)
	visibleLines := layout.StandardPanelMaxHeight - layout.BorderLines

	// Calcular posição do namespace e HPA selecionado
	namespaceStart, namespaceEnd, selectedHPALine := a.calculateSelectedHPALinePosition(allLines)

	// Ajustar scroll para exibir todo o namespace
	a.adjustScrollToKeepItemVisible(namespaceStart, namespaceEnd, selectedHPALine, visibleLines, totalLines)
}

// adjustNodePoolSelectedScrollToKeepItemVisible - Ajusta scroll do painel Node Pools selecionados para manter item visível
func (a *App) adjustNodePoolSelectedScrollToKeepItemVisible() {
	if a.model.ActivePanel != models.PanelSelectedNodePools || len(a.model.SelectedNodePools) == 0 {
		return
	}

	// Simular a construção das linhas para calcular a posição
	allLines := a.buildSelectedNodePoolsContent()
	totalLines := len(allLines)

	// Altura visível do painel (altura máxima - bordas)
	visibleLines := layout.StandardPanelMaxHeight - layout.BorderLines

	// Calcular linha do item selecionado
	selectedItemLine := a.calculateSelectedNodePoolLinePosition(allLines)

	// Ajustar scroll usando a função genérica, mas para Node Pools
	a.adjustNodePoolScrollToKeepItemVisible(selectedItemLine, visibleLines, totalLines)
}


// adjustClusterScrollToKeepItemVisible - Auto-scroll para lista de clusters
func (a *App) adjustClusterScrollToKeepItemVisible() {
	// Para clusters simples, usar scroll direto baseado no SelectedIndex
	totalClusters := len(a.model.Clusters)
	if totalClusters <= 10 { // Se cabe na tela, não precisa de scroll
		return
	}

	visibleLines := 10 // Aproximadamente 10 clusters visíveis por vez
	selectedIndex := a.model.SelectedIndex

	// Simular scroll baseado no índice selecionado
	if selectedIndex >= visibleLines/2 && selectedIndex < totalClusters-visibleLines/2 {
		// Centralizar item selecionado
		a.model.ClusterScrollOffset = selectedIndex - visibleLines/2
	} else if selectedIndex >= totalClusters-visibleLines/2 {
		// No final da lista
		a.model.ClusterScrollOffset = totalClusters - visibleLines
	} else {
		// No início da lista
		a.model.ClusterScrollOffset = 0
	}

	// Garantir limites
	if a.model.ClusterScrollOffset < 0 {
		a.model.ClusterScrollOffset = 0
	}
}

// adjustNamespaceScrollToKeepItemVisible - Auto-scroll para lista de namespaces
func (a *App) adjustNamespaceScrollToKeepItemVisible() {
	totalNamespaces := len(a.model.Namespaces)
	if totalNamespaces <= 15 { // Se cabe na tela, não precisa de scroll
		return
	}

	visibleLines := 15 // Aproximadamente 15 namespaces visíveis
	selectedIndex := a.model.SelectedIndex

	// Centralizar item selecionado
	if selectedIndex >= visibleLines/2 && selectedIndex < totalNamespaces-visibleLines/2 {
		a.model.NamespaceScrollOffset = selectedIndex - visibleLines/2
	} else if selectedIndex >= totalNamespaces-visibleLines/2 {
		a.model.NamespaceScrollOffset = totalNamespaces - visibleLines
	} else {
		a.model.NamespaceScrollOffset = 0
	}

	// Garantir limites
	if a.model.NamespaceScrollOffset < 0 {
		a.model.NamespaceScrollOffset = 0
	}
}

// adjustHPAListScrollToKeepItemVisible - Auto-scroll para lista principal de HPAs
func (a *App) adjustHPAListScrollToKeepItemVisible() {
	totalHPAs := len(a.model.HPAs)
	if totalHPAs <= 10 { // Se cabe na tela, não precisa de scroll
		return
	}

	visibleLines := 10 // Aproximadamente 10 HPAs visíveis
	selectedIndex := a.model.SelectedIndex

	// Centralizar item selecionado
	if selectedIndex >= visibleLines/2 && selectedIndex < totalHPAs-visibleLines/2 {
		a.model.HPAListScrollOffset = selectedIndex - visibleLines/2
	} else if selectedIndex >= totalHPAs-visibleLines/2 {
		a.model.HPAListScrollOffset = totalHPAs - visibleLines
	} else {
		a.model.HPAListScrollOffset = 0
	}

	// Garantir limites
	if a.model.HPAListScrollOffset < 0 {
		a.model.HPAListScrollOffset = 0
	}
}

// buildNamespaceListContent - Constrói conteúdo para lista de namespaces
func (a *App) buildNamespaceListContent() []string {
	if len(a.model.Namespaces) == 0 {
		return []string{"Carregando..."}
	}

	var content []string
	for i, ns := range a.model.Namespaces {
		marker := "  "
		if ns.Selected {
			marker = "✓ "
		}

		hpaInfo := ""
		hpaIndicator := ""
		if ns.HPACount > 0 {
			hpaInfo = fmt.Sprintf(" (%d HPAs)", ns.HPACount)
			hpaIndicator = "🎯"
		} else if ns.HPACount == 0 {
			hpaInfo = " (sem HPAs)"
			hpaIndicator = "❌"
		} else {
			hpaInfo = " (carregando...)"
			hpaIndicator = "⏳"
		}

		itemText := fmt.Sprintf("%s%s %s%s", marker, hpaIndicator, ns.Name, hpaInfo)

		if i == a.model.SelectedIndex && a.model.ActivePanel == 0 {
			// Criar estilo simples sem padding para evitar problemas
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content = append(content, selectedStyle.Render(itemText))
		} else {
			content = append(content, itemText)
		}
	}

	return content
}

// buildSelectedNamespacesContent - Constrói conteúdo para namespaces selecionados
func (a *App) buildSelectedNamespacesContent() []string {
	selectedNamespaces := make([]*models.Namespace, 0)
	for _, ns := range a.model.Namespaces {
		if ns.Selected {
			selectedNamespaces = append(selectedNamespaces, &ns)
		}
	}

	if len(selectedNamespaces) == 0 {
		return []string{
			"Nenhum namespace selecionado",
			"",
			"Use SPACE para selecionar namespaces",
			"na lista à esquerda.",
		}
	}

	var content []string
	for i, ns := range selectedNamespaces {
		hpaInfo := fmt.Sprintf("📊 %d HPAs", ns.HPACount)
		if ns.HPACount == 0 {
			hpaInfo = "❌ Sem HPAs"
		} else if ns.HPACount < 0 {
			hpaInfo = "⏳ Carregando..."
		}

		itemText := fmt.Sprintf("🎯 %s\n   %s", ns.Name, hpaInfo)

		if i == a.model.CurrentNamespaceIdx && a.model.ActivePanel == 1 {
			// Criar estilo simples sem padding para evitar problemas
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content = append(content, selectedStyle.Render(itemText))
		} else {
			content = append(content, itemText)
		}

		if i < len(selectedNamespaces)-1 {
			content = append(content, "") // Linha em branco entre itens
		}
	}

	return content
}

// buildNamespaceStatusContent - Constrói conteúdo para painel de status
func (a *App) buildNamespaceStatusContent() string {
	var status strings.Builder

	// Informações gerais
	totalNamespaces := len(a.model.Namespaces)
	selectedCount := 0
	totalHPAs := 0

	for _, ns := range a.model.Namespaces {
		if ns.Selected {
			selectedCount++
			if ns.HPACount > 0 {
				totalHPAs += ns.HPACount
			}
		}
	}

	status.WriteString(fmt.Sprintf("📊 Total: %d namespaces | Selecionados: %d\n", totalNamespaces, selectedCount))
	status.WriteString(fmt.Sprintf("🎯 HPAs encontrados: %d\n\n", totalHPAs))

	// Status do sistema
	systemStatus := "Sistema namespaces: ocultos"
	if a.model.ShowSystemNamespaces {
		systemStatus = "Sistema namespaces: exibidos"
	}
	status.WriteString(fmt.Sprintf("⚙️  %s\n", systemStatus))

	// Informações da sessão
	if a.model.LoadedSessionName != "" {
		status.WriteString(fmt.Sprintf("💾 Sessão: %s\n", a.model.LoadedSessionName))
	}

	return status.String()
}

// buildClusterListContent - Constrói conteúdo para lista de clusters
func (a *App) buildClusterListContent() []string {
	if len(a.model.Clusters) == 0 {
		return []string{"Carregando clusters..."}
	}

	var content []string
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")) // Cinza escuro
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))

	var currentSection string // HLG, PRD, ou OUTROS

	for i, cluster := range a.model.Clusters {
		nameLower := strings.ToLower(cluster.Name)
		var section string

		// Determinar seção
		if strings.Contains(nameLower, "-hlg-") {
			section = "HLG"
		} else if strings.Contains(nameLower, "-prd-") {
			section = "PRD"
		} else {
			section = "OUTROS"
		}

		// Adicionar separador se mudou de seção
		if section != currentSection {
			if currentSection != "" {
				content = append(content, "") // Linha vazia entre seções
			}
			content = append(content, separatorStyle.Render("──────────────────── "+section+" ────────────────────"))
			currentSection = section
		}

		// Adicionar cluster
		status := "❌"
		if cluster.Status == models.StatusConnected {
			status = "✅"
		} else if cluster.Status == models.StatusUnknown {
			status = "⏳"
		}

		item := fmt.Sprintf("%s %s", status, cluster.Name)
		if i == a.model.SelectedIndex {
			content = append(content, selectedStyle.Render(item))
		} else {
			content = append(content, item)
		}
	}

	return content
}

// buildClusterStatusContent - Constrói conteúdo para painel de status do cluster
func (a *App) buildClusterStatusContent() string {
	var status strings.Builder

	// Informações gerais
	totalClusters := len(a.model.Clusters)
	connectedCount := 0
	disconnectedCount := 0
	unknownCount := 0

	for _, cluster := range a.model.Clusters {
		switch cluster.Status {
		case models.StatusConnected:
			connectedCount++
		case models.StatusError, models.StatusTimeout:
			disconnectedCount++
		case models.StatusUnknown:
			unknownCount++
		}
	}

	status.WriteString(fmt.Sprintf("🏗️  Total: %d clusters\n", totalClusters))
	status.WriteString(fmt.Sprintf("✅ Conectados: %d\n", connectedCount))
	status.WriteString(fmt.Sprintf("❌ Desconectados: %d\n", disconnectedCount))
	status.WriteString(fmt.Sprintf("⏳ Verificando: %d\n\n", unknownCount))

	// Cluster selecionado
	if len(a.model.Clusters) > 0 && a.model.SelectedIndex < len(a.model.Clusters) {
		selectedCluster := a.model.Clusters[a.model.SelectedIndex]
		status.WriteString(fmt.Sprintf("🎯 Selecionado: %s\n", selectedCluster.Name))

		statusText := "❌ Desconectado"
		if selectedCluster.Status == models.StatusConnected {
			statusText = "✅ Conectado"
		} else if selectedCluster.Status == models.StatusUnknown {
			statusText = "⏳ Verificando"
		} else if selectedCluster.Status == models.StatusError {
			statusText = "❌ Erro"
		} else if selectedCluster.Status == models.StatusTimeout {
			statusText = "⏱️ Timeout"
		}
		status.WriteString(fmt.Sprintf("📡 Status: %s\n", statusText))
	}

	// Informações da sessão
	if a.model.LoadedSessionName != "" {
		status.WriteString(fmt.Sprintf("💾 Sessão: %s", a.model.LoadedSessionName))
	}

	return status.String()
}

// buildNodePoolListContent - Constrói conteúdo para lista de node pools
func (a *App) buildNodePoolListContent() []string {
	if len(a.model.NodePools) == 0 {
		return []string{"Carregando node pools..."}
	}

	var content []string
	for i, pool := range a.model.NodePools {
		marker := "  "
		if pool.Selected {
			marker = "✓ "
		}

		// Status baseado no estado
		status := "🟢"
		if pool.Modified {
			status = "🟡"
		}

		// Truncar nome do pool se muito longo (máximo 45 chars para caber em 70)
		poolName := pool.Name
		if len(poolName) > 45 {
			poolName = poolName[:42] + "..."
		}

		item := fmt.Sprintf("%s%s %s", marker, status, poolName)

		// Adicionar informações do node pool (formato compacto)
		if pool.AutoscalingEnabled {
			item += fmt.Sprintf("\n   Auto: %d-%d (atual:%d)",
				pool.MinNodeCount, pool.MaxNodeCount, pool.NodeCount)
		} else {
			item += fmt.Sprintf("\n   Manual: %d nodes", pool.NodeCount)
		}

		if i == a.model.SelectedIndex && a.model.ActivePanel == models.PanelNodePools {
			// Criar estilo simples sem padding para evitar problemas
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content = append(content, selectedStyle.Render(item))
		} else {
			content = append(content, item)
		}

		if i < len(a.model.NodePools)-1 {
			content = append(content, "") // Linha em branco entre itens
		}
	}

	return content
}

// buildSelectedNodePoolsContent - Constrói conteúdo para node pools selecionados
func (a *App) buildSelectedNodePoolsContent() []string {
	if len(a.model.SelectedNodePools) == 0 {
		return []string{
			"Nenhum node pool selecionado",
			"",
			"Use SPACE para selecionar node pools",
			"na lista à esquerda.",
		}
	}

	var content []string
	for i, pool := range a.model.SelectedNodePools {
		status := "🟢"
		if pool.Modified {
			status = "🟡✨"
		}

		// Indicador de marcação sequencial
		sequenceIndicator := ""
		if pool.SequenceOrder > 0 {
			sequenceIndicator = fmt.Sprintf(" *%d", pool.SequenceOrder)
		}

		item := fmt.Sprintf("%s %s%s", status, pool.Name, sequenceIndicator)

		// Adicionar detalhes das modificações
		if pool.AutoscalingEnabled {
			item += fmt.Sprintf("\n   Auto-scaling: %d-%d nodes (atual: %d)",
				pool.MinNodeCount, pool.MaxNodeCount, pool.NodeCount)
		} else {
			item += fmt.Sprintf("\n   Manual: %d nodes", pool.NodeCount)
		}

		if pool.Modified {
			item += "\n   🔧 Modificado"
		}

		if i == a.model.SelectedIndex && a.model.ActivePanel == models.PanelSelectedNodePools {
			// Criar estilo simples sem padding para evitar problemas
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content = append(content, selectedStyle.Render(item))
		} else {
			content = append(content, item)
		}

		if i < len(a.model.SelectedNodePools)-1 {
			content = append(content, "") // Linha em branco entre itens
		}
	}

	return content
}

// buildNodePoolStatusContent - Constrói conteúdo para painel de status dos node pools
func (a *App) buildNodePoolStatusContent() string {
	var status strings.Builder

	// Informações gerais
	totalPools := len(a.model.NodePools)
	selectedCount := len(a.model.SelectedNodePools)
	modifiedCount := 0

	for _, pool := range a.model.SelectedNodePools {
		if pool.Modified {
			modifiedCount++
		}
	}

	status.WriteString(fmt.Sprintf("🖥️  Total: %d node pools | Selecionados: %d\n", totalPools, selectedCount))
	status.WriteString(fmt.Sprintf("🔧 Modificados: %d\n\n", modifiedCount))

	// Informações do cluster
	if a.model.SelectedCluster != nil {
		status.WriteString(fmt.Sprintf("🏗️  Cluster: %s\n", a.model.SelectedCluster.Name))
	}

	// Progress bars se houver
	rolloutContent := a.renderRolloutProgressContent()
	nodePoolContent := a.renderNodePoolProgressContent()

	if rolloutContent != "" {
		status.WriteString("\n📊 Rollout Progress:\n")
		status.WriteString(rolloutContent)
	}

	if nodePoolContent != "" {
		status.WriteString("\n🖥️  Node Pool Progress:\n")
		status.WriteString(nodePoolContent)
	}

	// Informações da sessão
	if a.model.LoadedSessionName != "" {
		status.WriteString(fmt.Sprintf("\n💾 Sessão: %s", a.model.LoadedSessionName))
	}

	return status.String()
}

// renderRolloutProgressContent - Agora gerenciado pelo StatusPanel
func (a *App) renderRolloutProgressContent() string {
	return "" // StatusPanel gerencia progress bars automaticamente
	// Código antigo removido - StatusPanel agora gerencia tudo
	/*
	var content strings.Builder
	barWidth := 50

	for i, progress := range a.model.RolloutProgress {
		// Status icon
		var statusIcon string
		var statusColor lipgloss.Color

		switch progress.Status {
		case models.RolloutStatusPending:
			statusIcon = "⏳"
			statusColor = lipgloss.Color("3") // Yellow
		case models.RolloutStatusRunning:
			statusIcon = "🔄"
			statusColor = lipgloss.Color("6") // Cyan
		case models.RolloutStatusCompleted:
			statusIcon = "✅"
			statusColor = lipgloss.Color("2") // Green
		case models.RolloutStatusFailed:
			statusIcon = "❌"
			statusColor = lipgloss.Color("1") // Red
		case models.RolloutStatusCancelled:
			statusIcon = "⚠️"
			statusColor = lipgloss.Color("8") // Gray
		default:
			statusIcon = "❓"
			statusColor = lipgloss.Color("7") // White
		}

		// Progress bar characters (estilo Rich - mais fino e elegante)
		filled := int(math.Round(float64(progress.Progress) * float64(barWidth) / 100.0))

		var bar strings.Builder

		// Filled portion - usando caracteres mais finos
		if filled > 0 {
			bar.WriteString(lipgloss.NewStyle().
				Foreground(statusColor).
				Render(strings.Repeat("━", filled)))
		}

		// Empty portion - usando caracteres mais sutis
		remaining := barWidth - filled
		if remaining > 0 {
			bar.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Render(strings.Repeat("╌", remaining)))
		}

		// Percentage text
		percentText := fmt.Sprintf(" %3d%%", progress.Progress)
		percentStyle := lipgloss.NewStyle().
			Foreground(textColor).
			Bold(true)

		// Elapsed time
		elapsed := time.Since(progress.StartTime)
		var timeText string
		if progress.EndTime != nil {
			totalTime := progress.EndTime.Sub(progress.StartTime)
			timeText = fmt.Sprintf(" (%s)", totalTime.Round(time.Second))
		} else {
			timeText = fmt.Sprintf(" (%s)", elapsed.Round(time.Second))
		}

		// Task description
		taskDesc := fmt.Sprintf("%s %s/%s [%s]",
			statusIcon,
			progress.Namespace,
			progress.HPAName,
			progress.RolloutType)

		taskStyle := lipgloss.NewStyle().
			Foreground(textColor).
			Width(30)

		// Message
		messageText := progress.Message
		if messageText == "" {
			messageText = progress.Status.String()
		}

		messageStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Italic(true)

		// Build the line
		line := fmt.Sprintf("%s %s%s%s %s",
			taskStyle.Render(taskDesc),
			bar.String(),
			percentStyle.Render(percentText),
			timeText,
			messageStyle.Render(messageText))

		content.WriteString(line)

	*/
}

// renderNodePoolProgressContent - Agora gerenciado pelo StatusPanel
func (a *App) renderNodePoolProgressContent() string {
	return "" // StatusPanel gerencia progress bars automaticamente
	// Código antigo removido
	/*
	var content strings.Builder
	barWidth := 40

	for i, progress := range a.model.NodePoolProgress {
		// Status icon
		var statusIcon string
		var statusColor lipgloss.Color

		switch progress.Status {
		case models.RolloutStatusPending:
			statusIcon = "⏳"
			statusColor = lipgloss.Color("3") // Yellow
		case models.RolloutStatusRunning:
			statusIcon = "⚙️"
			statusColor = lipgloss.Color("6") // Cyan
		case models.RolloutStatusCompleted:
			statusIcon = "✅"
			statusColor = lipgloss.Color("2") // Green
		case models.RolloutStatusFailed:
			statusIcon = "❌"
			statusColor = lipgloss.Color("1") // Red
		case models.RolloutStatusCancelled:
			statusIcon = "⚠️"
			statusColor = lipgloss.Color("8") // Gray
		default:
			statusIcon = "❓"
			statusColor = lipgloss.Color("7") // White
		}

		// Progress bar characters (estilo Rich - mais fino e elegante)
		filled := int(math.Round(float64(progress.Progress) * float64(barWidth) / 100.0))

		var bar strings.Builder

		// Filled portion - usando caracteres mais finos
		if filled > 0 {
			bar.WriteString(lipgloss.NewStyle().
				Foreground(statusColor).
				Render(strings.Repeat("━", filled)))
		}

		// Empty portion - usando caracteres mais sutis
		remaining := barWidth - filled
		if remaining > 0 {
			bar.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Render(strings.Repeat("╌", remaining)))
		}

		// Percentage text
		percentText := fmt.Sprintf(" %3d%%", progress.Progress)
		percentStyle := lipgloss.NewStyle().
			Foreground(textColor).
			Bold(true)

		// Elapsed time
		elapsed := time.Since(progress.StartTime)
		var timeText string
		if progress.EndTime != nil {
			totalTime := progress.EndTime.Sub(progress.StartTime)
			timeText = fmt.Sprintf(" (%s)", totalTime.Round(time.Second))
		} else {
			timeText = fmt.Sprintf(" (%s)", elapsed.Round(time.Second))
		}

		// Operation details - mostrar valores corretos baseados na operação
		var operationDetails string
		if progress.Operation == "manual" || progress.Operation == "scale" {
			// Para operações manuais ou de scale, mostrar mudança no node count
			operationDetails = fmt.Sprintf(" %d→%d nodes", progress.FromNodeCount, progress.ToNodeCount)
		} else if progress.Operation == "autoscale" && progress.FromNodeCount == progress.ToNodeCount {
			// Para autoscale sem mudança no node count, mostrar min→max
			operationDetails = fmt.Sprintf(" %d→%d nodes", progress.FromMinNodes, progress.ToMaxNodes)
		} else {
			// Para outras operações com mudança no node count
			operationDetails = fmt.Sprintf(" %d→%d nodes", progress.FromNodeCount, progress.ToNodeCount)
		}

		// Task description
		taskDesc := fmt.Sprintf("%s %s/%s [%s]%s",
			statusIcon,
			progress.ClusterName,
			progress.PoolName,
			progress.Operation,
			operationDetails)

		taskStyle := lipgloss.NewStyle().
			Foreground(textColor).
			Width(30)

		// Message
		messageText := progress.Message
		if messageText == "" {
			messageText = progress.Status.String()
		}

		messageStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Italic(true)

		// Build the line
		line := fmt.Sprintf("%s %s%s%s %s",
			taskStyle.Render(taskDesc),
			bar.String(),
			percentStyle.Render(percentText),
			timeText,
			messageStyle.Render(messageText))

		content.WriteString(line)

	*/
}

// renderHPAEditing - Tela de edição de HPA
func (a *App) renderHPAEditing() string {
	if a.model.EditingHPA == nil {
		return "Erro: Nenhum HPA sendo editado"
	}

	hpa := a.model.EditingHPA

	// Session header (if loaded)
	sessionHeader := a.renderSessionHeader()
	
	// Título
	title := titleStyle.Render(fmt.Sprintf("✏️  Editando HPA: %s", hpa.Name)) + "\n"
	
	// Painéis lado a lado
	leftPanel := a.renderHPAMainPanel()
	rightPanel := a.renderHPAResourcePanel()
	
	// Combinar painéis horizontalmente
	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Calcular espaçamento dinâmico para posição fixa do painel Status
	spacing := a.calculateEditingPanelSpacing()

	// Painel unificado para status e informações
	statusPanel := a.renderStatusInfoPanel()

	// Ajuda
	helpText := ""
	if a.model.EditingField {
		helpText = helpStyle.Render("Digite o valor • ENTER Confirmar • ESC Cancelar edição")
	} else {
		helpText = helpStyle.Render("↑↓ Navegar campos • ENTER Editar • SPACE Alternar rollout • TAB Alternar painel\nCtrl+S Salvar • ? Ajuda • ESC Voltar")
	}

	return sessionHeader + title + panels + spacing + statusPanel + "\n" + helpText
}


// Funções auxiliares
func getFormValue(formFields map[string]string, key, defaultVal string) string {
	if val, exists := formFields[key]; exists && val != "" {
		return val
	}
	return defaultVal
}

func getIntPtrString(val *int32) string {
	if val == nil {
		return "não definido"
	}
	return fmt.Sprintf("%d", *val)
}

// renderNodePoolSelection - Tela de seleção de node pools
func (a *App) renderNodePoolSelection() string {
	// Se estamos digitando nome da sessão, mostrar prompt
	if a.model.EnteringSessionName {
		var content strings.Builder
		content.WriteString(titleStyle.Render("💾 Salvando Sessão de Node Pools") + "\n\n")
		content.WriteString("Digite o nome da sessão:\n")
		displayName := a.insertCursorInText(a.model.SessionName, a.model.CursorPosition)
		content.WriteString(selectedItemStyle.Render(displayName) + "\n\n")
		content.WriteString(helpStyle.Render("ENTER Salvar • ESC Cancelar"))
		return content.String()
	}

	// Criar gerenciador de layout
	layoutMgr := layout.NewLayoutManager()

	// Preparar conteúdo dos painéis
	leftContent := a.buildNodePoolListContent()
	rightContent := a.buildSelectedNodePoolsContent()

	// Criar painéis responsivos
	leftPanel := layout.NewResponsivePanel("Node Pools Disponíveis", leftContent, layout.PrimaryColor, layoutMgr)
	rightPanel := layout.NewResponsivePanel("Node Pools Selecionados", rightContent, layout.SuccessColor, layoutMgr)
	statusPanel := a.renderStatusInfoPanel()

	// Header e ajuda
	sessionInfo := a.renderSessionHeader()
	clusterName := "Unknown"
	if a.model.SelectedCluster != nil {
		clusterName = a.model.SelectedCluster.Name
	}
	sessionInfo += layout.TitleStyle.Render(fmt.Sprintf("🖥️  Node Pools - Cluster: %s", clusterName)) + "\n\n"

	help := "↑↓ Navegar • SPACE Selecionar • TAB Alternar painel • Ctrl+R Remover\nENTER Editar Node Pool • ? Ajuda • ESC Voltar"
	if len(a.model.SelectedNodePools) > 0 {
		help = "↑↓ Navegar • SPACE Selecionar • TAB Alternar painel • Ctrl+R Remover\nCtrl+D Aplicar individual • Ctrl+U Aplicar todos • Ctrl+S Salvar sessão • ? Ajuda • ESC Voltar"
	}
	helpText := layout.HelpStyle.Render(help)

	// Construir layout
	return layout.NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(leftPanel.Render(), leftPanel.GetActualHeight()).
		AddPanel(rightPanel.Render(), rightPanel.GetActualHeight()).
		BuildTwoColumn(statusPanel)
}

// renderNodePoolList - DEPRECATED: Lista de node pools disponíveis (não utilizada)
// Esta função foi substituída pelo sistema de layout responsivo em renderNodePoolSelection()
/*
func (a *App) renderNodePoolList() string {
	if len(a.model.NodePools) == 0 {
		return renderPanelWithTitle("Carregando node pools...", "Node Pools Disponíveis", 70, 18, primaryColor)
	}

	var items []string
	for i, pool := range a.model.NodePools {
		marker := "  "
		if pool.Selected {
			marker = "✓ "
		}

		status := ""
		if pool.Modified {
			status = " ✨"
		}

		poolType := "Worker"
		if pool.IsSystemPool {
			poolType = "System"
		}

		item := fmt.Sprintf("%s%s (%s) VM:%s Count:%d/%d-%d%s",
			marker, pool.Name, poolType, pool.VMSize, pool.NodeCount, pool.MinNodeCount, pool.MaxNodeCount, status)

		if i == a.model.SelectedIndex && a.model.ActivePanel == models.PanelNodePools {
			items = append(items, selectedItemStyle.Render(item))
		} else {
			items = append(items, normalItemStyle.Render(item))
		}
	}

	content := strings.Join(items, "\n")
	return renderPanelWithTitle(content, "Node Pools Disponíveis", 70, 18, primaryColor)
}
*/

// renderSelectedNodePoolsList - Lista de node pools selecionados com scroll responsivo
func (a *App) renderSelectedNodePoolsList() string {
	if len(a.model.SelectedNodePools) == 0 {
		// Usar função responsiva mesmo quando vazio
		emptyLines := []string{"Nenhum node pool selecionado"}
		return a.renderResponsiveNodePoolSelectedPanel(emptyLines)
	}

	var allLines []string
	currentIndex := 0

	// Renderizar cada node pool
	for _, pool := range a.model.SelectedNodePools {
		status := ""
		if pool.Modified {
			status = " ✨"
		}

		poolType := "Worker"
		if pool.IsSystemPool {
			poolType = "System"
		}

		// Indicador de aplicação
		appliedIndicator := ""
		if pool.AppliedCount > 0 {
			if pool.AppliedCount == 1 {
				appliedIndicator = " ●"
			} else {
				appliedIndicator = fmt.Sprintf(" ●%d", pool.AppliedCount)
			}
		}

		// Verificar se este Node Pool está selecionado
		isSelected := currentIndex == a.model.SelectedIndex && a.model.ActivePanel == models.PanelSelectedNodePools

		lines := []string{
			fmt.Sprintf("🖥️  %s", pool.Name),
			fmt.Sprintf("   Tipo: %s | VM: %s", poolType, pool.VMSize),
			fmt.Sprintf("   Count:%d Min:%d Max:%d%s%s", pool.NodeCount, pool.MinNodeCount, pool.MaxNodeCount, status, appliedIndicator),
		}

		for _, line := range lines {
			if isSelected {
				allLines = append(allLines, selectedItemStyle.Render(line))
			} else {
				allLines = append(allLines, normalItemStyle.Render(line))
			}
		}
		currentIndex++
	}

	return a.renderResponsiveNodePoolSelectedPanel(allLines)
}

// renderResponsiveNodePoolSelectedPanel - Painel de Node Pools Selecionados responsivo com scroll
func (a *App) renderResponsiveNodePoolSelectedPanel(allLines []string) string {
	// Calcular largura responsiva baseada no conteúdo (maior linha) - IGUAL AOS HPAs
	contentWidth := 0
	for _, line := range allLines {
		// Remover códigos de cor/estilo para calcular largura real
		cleanLine := lipgloss.NewStyle().UnsetBackground().UnsetForeground().Render(line)
		lineWidth := len([]rune(cleanLine))
		if lineWidth > contentWidth {
			contentWidth = lineWidth
		}
	}

	// Adicionar margem para bordas e espaçamento interno - IGUAL AOS HPAs
	maxWidth := contentWidth + 6 // +6 para bordas e padding
	if maxWidth < 35 {
		maxWidth = 35 // Largura mínima
	}
	if maxWidth > 120 {
		maxWidth = 120 // Largura máxima para não ficar muito largo
	}

	// Altura responsiva baseada no conteúdo, até 35 linhas máximo - IGUAL AOS HPAs
	totalLines := len(allLines)
	maxHeight := 35 // Limite máximo IGUAL AOS HPAs

	// Calcular altura dinâmica - IGUAL AOS HPAs
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

	// Calcular quantas linhas podemos mostrar (descontando bordas) - IGUAL AOS HPAs
	visibleLines := availableHeight - 2

	// Aplicar scroll com foco automático no item selecionado - IGUAL AOS HPAs
	var displayLines []string
	var scrollInfo string

	if totalLines > visibleLines {
		// Scroll necessário - calcular posição do item selecionado
		selectedItemLine := a.calculateSelectedNodePoolLinePosition(allLines)

		// Ajustar scroll para manter item selecionado visível
		a.adjustNodePoolScrollToKeepItemVisible(selectedItemLine, visibleLines, totalLines)

		// Pegar apenas as linhas visíveis
		start := a.model.NodePoolSelectedScrollOffset
		end := start + visibleLines
		displayLines = allLines[start:end]
		scrollInfo = fmt.Sprintf(" [%d-%d/%d]", start+1, end, totalLines)
	} else {
		// Tudo cabe
		displayLines = allLines
		a.model.NodePoolSelectedScrollOffset = 0
	}

	// Juntar linhas para exibição
	content := strings.Join(displayLines, "\n")

	// Título com informação de scroll - MANTENDO NOME ESPECÍFICO DE NODE POOLS
	title := "Node Pools Selecionados"
	if scrollInfo != "" {
		title += scrollInfo
	}

	return renderPanelWithTitle(content, title, maxWidth, availableHeight, successColor)
}

// calculateSelectedNodePoolLinePosition - Calcula em qual linha está o Node Pool selecionado
func (a *App) calculateSelectedNodePoolLinePosition(allLines []string) int {
	if a.model.ActivePanel != models.PanelSelectedNodePools {
		return 0 // Se não está no painel Node Pools, não há item selecionado
	}

	selectedIndex := a.model.SelectedIndex
	if selectedIndex < 0 || selectedIndex >= len(a.model.SelectedNodePools) {
		return 0 // Índice inválido
	}

	// Cada node pool ocupa 3 linhas (nome + tipo/vm + count/min/max)
	// Como não há agrupamento por namespace, é direto
	linePosition := selectedIndex * 3

	// Garantir que não exceda o total de linhas
	if linePosition >= len(allLines) {
		return len(allLines) - 1
	}

	return linePosition
}

// adjustNodePoolScrollToKeepItemVisible - Ajusta scroll para manter item selecionado visível
func (a *App) adjustNodePoolScrollToKeepItemVisible(selectedItemLine, visibleLines, totalLines int) {
	// Se o item selecionado está acima da janela visível, mover para cima
	if selectedItemLine < a.model.NodePoolSelectedScrollOffset {
		a.model.NodePoolSelectedScrollOffset = selectedItemLine
	}

	// Se o item selecionado está abaixo da janela visível, mover para baixo
	if selectedItemLine >= a.model.NodePoolSelectedScrollOffset+visibleLines {
		a.model.NodePoolSelectedScrollOffset = selectedItemLine - visibleLines + 1
	}

	// Garantir que o offset não seja negativo
	if a.model.NodePoolSelectedScrollOffset < 0 {
		a.model.NodePoolSelectedScrollOffset = 0
	}

	// Garantir que o offset não exceda o limite máximo
	maxOffset := totalLines - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if a.model.NodePoolSelectedScrollOffset > maxOffset {
		a.model.NodePoolSelectedScrollOffset = maxOffset
	}
}

// renderNodePoolEditing - Tela de edição de node pool
func (a *App) renderNodePoolEditing() string {
	if a.model.EditingNodePool == nil {
		return "Erro: Nenhum node pool sendo editado"
	}

	pool := a.model.EditingNodePool

	// Criar gerenciador de layout
	layoutMgr := layout.NewLayoutManager()

	// Preparar conteúdo
	formContent := []string{a.renderNodePoolEditForm()}

	// Criar painéis
	formPanel := layout.NewResponsivePanel("Configurações do Node Pool", formContent, layout.PrimaryColor, layoutMgr)
	statusPanel := a.renderStatusInfoPanel()

	// Header e ajuda
	sessionInfo := a.renderSessionHeader()
	sessionInfo += layout.TitleStyle.Render(fmt.Sprintf("✏️  Editando Node Pool: %s", pool.Name)) + "\n\n"

	help := "↑↓ Navegar campos • ENTER Editar • Tab Próximo campo • Ctrl+S Salvar • ? Ajuda • ESC Voltar"
	if a.model.EditingField {
		help = "Digite o valor • ENTER Confirmar • ESC Cancelar edição"
	}
	helpText := layout.HelpStyle.Render(help)

	// Construir layout
	return layout.NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(formPanel.Render(), formPanel.GetActualHeight()).
		BuildSingleColumn(statusPanel)
}

// renderNodePoolEditForm - Formulário de edição do node pool
func (a *App) renderNodePoolEditForm() string {
	pool := a.model.EditingNodePool
	var content strings.Builder

	// Informações básicas
	content.WriteString(fmt.Sprintf("Cluster: %s\n", pool.ClusterName))
	content.WriteString(fmt.Sprintf("VM Size: %s\n", pool.VMSize))
	poolType := "Worker Pool"
	if pool.IsSystemPool {
		poolType = "System Pool"
	}
	content.WriteString(fmt.Sprintf("Type: %s\n\n", poolType))

	// Toggle de Autoscaling - sempre no topo
	autoscalingValue := "Habilitado"
	if !pool.AutoscalingEnabled {
		autoscalingValue = "Desabilitado"
	}

	autoscalingStyle := normalItemStyle
	if a.model.ActiveField == "autoscaling_enabled" {
		autoscalingStyle = selectedItemStyle
	}

	content.WriteString(autoscalingStyle.Render(fmt.Sprintf("Autoscaling: %s", autoscalingValue)))
	content.WriteString("\n")

	// Campos editáveis baseados no modo de autoscaling
	var fields []struct {
		name        string
		currentVal  string
		fieldKey    string
		description string
	}

	if pool.AutoscalingEnabled {
		// Modo autoscaling: mostrar min, max e current
		fields = []struct {
			name        string
			currentVal  string
			fieldKey    string
			description string
		}{
			{"Min Node Count", getFormValue(a.model.FormFields, "min_nodes", fmt.Sprintf("%d", pool.MinNodeCount)), "min_nodes", "Número mínimo de nodes"},
			{"Max Node Count", getFormValue(a.model.FormFields, "max_nodes", fmt.Sprintf("%d", pool.MaxNodeCount)), "max_nodes", "Número máximo de nodes"},
			{"Node Count", getFormValue(a.model.FormFields, "node_count", fmt.Sprintf("%d", pool.NodeCount)), "node_count", "Número atual de nodes"},
		}
	} else {
		// Modo manual: mostrar apenas node count
		fields = []struct {
			name        string
			currentVal  string
			fieldKey    string
			description string
		}{
			{"Node Count", getFormValue(a.model.FormFields, "node_count", fmt.Sprintf("%d", pool.NodeCount)), "node_count", "Número de nodes (manual)"},
		}
	}

	for _, field := range fields {
		style := normalItemStyle
		if a.model.ActiveField == field.fieldKey {
			style = selectedItemStyle
		}

		// Show editing value if currently editing this field
		displayValue := field.currentVal
		if a.model.EditingField && a.model.ActiveField == field.fieldKey {
			displayValue = a.insertCursorInText(a.model.EditingValue, a.model.CursorPosition)
		}

		// Renderizar campo normalmente
		content.WriteString(style.Render(fmt.Sprintf("%s: %s", field.name, displayValue)))

		// Mostrar ajuda específica para cada campo
		if a.model.ActiveField == field.fieldKey {
			if a.model.EditingField {
				content.WriteString("\n" + helpStyle.Render("  Digite o valor • ENTER Confirmar • ESC Cancelar"))
			} else {
				content.WriteString("\n" + helpStyle.Render(fmt.Sprintf("  %s • ENTER Editar", field.description)))
			}
		}

		content.WriteString("\n")
	}

	return content.String()
}

// renderHelp - Tela de ajuda com scroll
func (a *App) renderHelp() string {
	// Criar lista de todas as linhas de conteúdo
	var allLines []string
	
	// Estilos
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(successColor)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Width(16)
	descStyle := lipgloss.NewStyle().Foreground(textColor)
	
	// Header
	allLines = append(allLines, 
		lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Align(lipgloss.Center).Width(70).
		Render("📖 AJUDA - K8s HPA Manager"),
		"",
	)
	
	// Seções de ajuda
	sections := []struct {
		title string
		keys  [][]string
	}{
		{"🌐 NAVEGAÇÃO GLOBAL", [][]string{
			{"?", "Mostrar esta ajuda"},
			{"F4", "Sair da aplicação"},
			{"F9", "Gerenciar CronJobs do cluster"},
			{"ESC", "Voltar/Cancelar"},
			{"Ctrl+C", "Forçar saída"},
		}},
		{"🏗️  SELEÇÃO DE CLUSTERS", [][]string{
			{"↑↓ / k j", "Navegar pelos clusters"},
			{"ENTER", "Selecionar cluster"},
			{"Ctrl+L", "Carregar sessão salva"},
			{"F5 / R", "Recarregar lista de clusters"},
		}},
		{"📚 GERENCIAMENTO DE SESSÕES", [][]string{
			{"↑↓ / k j", "Navegar pelas sessões"},
			{"ENTER", "Carregar sessão selecionada"},
			{"Ctrl+S", "Salvar sessão (funciona MESMO SEM modificações)"},
			{"Ctrl+R", "Deletar sessão selecionada"},
			{"Ctrl+N / F2", "Renomear sessão (com cursor)"},
			{"💾 Rollback", "Carregar (Ctrl+L) → Salvar (Ctrl+S) sem modificar"},
			{"📚", "Sessão carregada será exibida no header"},
		}},
		{"📁 NAMESPACES", [][]string{
			{"↑↓ / k j", "Navegar pelos namespaces"},
			{"SPACE", "Selecionar/desselecionar"},
			{"TAB", "Alternar painéis"},
			{"Ctrl+R", "Remover selecionado"},
			{"Ctrl+N", "Gerenciar node pools"},
			{"Ctrl+M", "Sessão mista (HPAs + Node Pools)"},
			{"S", "Toggle sistema"},
			{"ENTER", "Continuar para HPAs"},
		}},
		{"🎯 HPAs", [][]string{
			{"↑↓ / k j", "Navegar pelos HPAs"},
			{"SPACE", "Selecionar/desselecionar"},
			{"TAB", "Alternar painéis"},
			{"Ctrl+R", "Remover selecionado"},
			{"ENTER", "Editar HPA (7 campos + 3 rollouts)"},
			{"Ctrl+D", "Aplicar individual (múltiplas vezes, mostra ●)"},
			{"Ctrl+U", "Aplicar todos selecionados (múltiplas vezes)"},
			{"Ctrl+S", "Salvar sessão (sem modificações = rollback)"},
			{"📋", "HPAs agrupados por namespace no painel de selecionados"},
			{"🔄", "Rollouts aparecem em tempo real no painel de status"},
		}},
		{"🖥️  NODE POOLS", [][]string{
			{"↑↓ / k j", "Navegar pelos node pools"},
			{"SPACE", "Selecionar/desselecionar"},
			{"TAB", "Alternar painéis"},
			{"Ctrl+R", "Remover selecionado"},
			{"ENTER", "Editar node pool"},
			{"Ctrl+D", "Aplicar individual"},
			{"Ctrl+U", "Aplicar todos"},
			{"Ctrl+S", "Salvar sessão (sem modificações = rollback)"},
			{"Ctrl+M", "Marcar para execução sequencial (máx 2)"},
			{"*1, *2", "Indicadores de ordem sequencial"},
			{"🔄", "Primeiro executa manualmente, segundo automaticamente"},
		}},
		{"📅 GERENCIAMENTO DE CRONJOBS", [][]string{
			{"F9", "Acessar CronJobs do cluster selecionado"},
			{"↑↓ / k j", "Navegar pelos CronJobs"},
			{"SPACE", "Selecionar/desselecionar CronJob"},
			{"ENTER", "Editar CronJob (habilitar/desabilitar)"},
			{"ESC", "Voltar para seleção de clusters"},
		}},
		{"🔄 EXECUÇÃO SEQUENCIAL NODE POOLS", [][]string{
			{"Ctrl+M", "Marcar até 2 node pools para execução sequencial"},
			{"*1", "Primeiro node pool (execução manual)"},
			{"*2", "Segundo node pool (execução automática após *1)"},
			{"Ctrl+D/U", "Executar *1 manualmente (stress test)"},
			{"⏳", "Sistema aguarda *1 completar"},
			{"🚀", "Sistema inicia *2 automaticamente"},
			{"💾", "Marcações salvas/restauradas em sessões"},
			{"📊", "Status: pending → executing → completed"},
		}},
		{"✏️  EDIÇÃO COM CURSOR", [][]string{
			{"↑↓ / k j", "Navegar campos"},
			{"TAB", "Próximo campo"},
			{"ENTER", "Editar campo"},
			{"SPACE", "Toggle Rollouts (Deployment/DaemonSet/StatefulSet) / Autoscaling"},
			{"📝", "3 tipos de rollout disponíveis em HPAs"},
			{"📝", "Autoscaling: campos aparecem/desaparecem dinamicamente"},
			{"Ctrl+S", "Salvar e voltar"},
			{"←→ / h l", "Mover cursor caractere por caractere"},
			{"Home / Ctrl+A", "Cursor para início"},
			{"End / Ctrl+E", "Cursor para final"},
			{"Backspace", "Apagar antes do cursor"},
			{"Delete", "Apagar na posição do cursor"},
			{"Ctrl+U", "Apagar até início da linha"},
			{"Ctrl+K", "Apagar até final da linha"},
			{"Ctrl+W", "Apagar palavra anterior"},
			{"Qualquer tecla", "Inserir na posição do cursor"},
		}},
		{"📊 PAINEL DE STATUS (Rich Python Style)", [][]string{
			{"Localização", "Logo abaixo dos painéis principais de HPAs"},
			{"Dimensões", "140 colunas x 15 linhas - fixas e centralizadas"},
			{"Bottom-Up", "Novos itens sempre aparecem na última linha"},
			{"Mensagens", "✅ Sucesso, ❌ Erros - limpeza automática (5s)"},
			{"Progress Bars", "━━━━━╌╌╌ Rollouts em tempo real (Rich Python style)"},
			{"Cores Dinâmicas", "🔴→🟠→🟡→🟢 Baseado no progresso (0→100%)"},
			{"Lifecycle", "Início ℹ️ → Progresso ━╌ → Fim ✅/❌"},
			{"Conclusão", "✅ Sucesso com nome do recurso"},
			{"Falha", "❌ Erro detalhado com motivo da falha"},
			{"Tipos", "🔄 Deployment, DaemonSet, StatefulSet simultâneos"},
			{"Auto-cleanup", "Progress bars removidas 3s após conclusão"},
			{"Scrollable", "Shift+Up/Down para navegar histórico completo"},
			{"Alinhamento", "Variation selectors (U+FE0F) removidos automaticamente"},
			{"Precisão", "Cada linha tem exatamente 140 colunas visuais"},
		}},
		{"📜 CONTROLES DE SCROLL", [][]string{
			{"Shift+Up/Down", "Scroll em painéis responsivos"},
			{"Mouse Wheel", "Scroll alternativo"},
			{"Painéis com scroll", "HPAs Selecionados, Node Pools Selecionados, Status"},
			{"Auto-scroll", "Item selecionado sempre permanece visível"},
			{"Indicadores", "[5-15/45] mostram posição atual/total"},
			{"Context-aware", "Funciona apenas no painel ativo"},
			{"Responsivo", "Largura baseada no conteúdo, altura máxima 35/7 linhas"},
		}},
		{"☁️  AZURE & AUTENTICAÇÃO", [][]string{
			{"Auto-login", "Login automático quando necessário"},
			{"Token refresh", "Renova tokens expirados automaticamente"},
			{"Subscription", "Troca automática entre subscriptions"},
			{"Retry", "Retry automático em falhas de auth"},
		}},
		{"🔒 SEGURANÇA NODE POOLS", [][]string{
			{"⚠️", "Scale Method removido para prevenir acidentes"},
			{"✅", "Apenas toggle Autoscaling Enable/Disable seguro"},
			{"🛡️", "Manual scaling: mostra apenas Node Count editável"},
			{"🔄", "Auto scaling: mostra Min/Max/Current node counts"},
			{"💾", "Valores originais sempre preservados para rollback"},
		}},
		{"🐛 CORREÇÕES IMPORTANTES", [][]string{
			{"MinReplicas", "HPAs não mostram mais endereços de memória"},
			{"Rollout completo", "Deployment, DaemonSet, StatefulSet todos exibidos"},
			{"Sessões HPA", "Rollouts DaemonSet/StatefulSet salvos e restaurados"},
			{"Resolução mínima", "Terminal limitado a 188x45 mínimo"},
			{"ESC navigation", "Funciona em todas as telas (CronJob, folders)"},
			{"Spacing", "CronJob editing tem espaçamento correto"},
			{"Session loading", "HPAs restauram estado completo"},
			{"Emoji alignment", "Variation selectors invisíveis (U+FE0F) removidos"},
			{"Status borders", "Container status 140 colunas exatas - alinhamento perfeito"},
		}},
		{"🎨 INDICADORES", [][]string{
			{"✅", "Online/Ativo"},
			{"❌", "Offline/Inativo"},
			{"⏳", "Carregando"},
			{"🎯", "Com HPAs"},
			{"✨", "Modificado"},
			{"📁", "Selecionado"},
			{"●", "Aplicado 1 vez (na sessão atual)"},
			{"●2", "Aplicado 2 vezes (contador de aplicações)"},
			{"█", "Cursor"},
			{"📚", "Sessão carregada (no header)"},
			{"[manual]", "Node Pool: desabilitando autoscaling + nodes fixos"},
			{"[autoscale]", "Node Pool: habilitando autoscaling + min/max"},
			{"[scale]", "Node Pool: alterando node count manualmente"},
		}},
		{"📐 NAVEGAÇÃO DESTA AJUDA", [][]string{
			{"↑↓ / k j", "Scroll linha por linha"},
			{"PgUp/PgDn", "Scroll página"},
			{"Home", "Ir ao início"},
			{"End", "Ir ao final"},
			{"Qualquer tecla", "Voltar"},
		}},
	}
	
	// Adicionar todas as seções
	for _, section := range sections {
		allLines = append(allLines, "", sectionStyle.Render(section.title))
		for _, key := range section.keys {
			line := keyStyle.Render(key[0]) + " " + descStyle.Render(key[1])
			allLines = append(allLines, line)
		}
	}
	
	// Calcular dimensões da tela (aproximadamente)
	terminalHeight := a.height - 4 // Reservar espaço para bordas
	if terminalHeight < 10 {
		terminalHeight = 20 // Fallback
	}
	
	// Aplicar scroll
	totalLines := len(allLines)
	startLine := a.model.HelpScrollOffset
	endLine := startLine + terminalHeight
	
	// Limitar scroll
	if startLine < 0 {
		startLine = 0
		a.model.HelpScrollOffset = 0
	}
	if startLine >= totalLines {
		startLine = totalLines - terminalHeight
		if startLine < 0 {
			startLine = 0
		}
		a.model.HelpScrollOffset = startLine
	}
	if endLine > totalLines {
		endLine = totalLines
	}
	
	// Obter linhas visíveis
	var visibleLines []string
	if endLine > startLine {
		visibleLines = allLines[startLine:endLine]
	}
	
	// Indicador de scroll
	scrollInfo := ""
	if totalLines > terminalHeight {
		scrollInfo = fmt.Sprintf(" [%d/%d]", startLine+1, totalLines)
	}
	
	// Conteúdo final
	content := strings.Join(visibleLines, "\n")
	
	// Container com scroll info
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1).
		Width(76)
	
	if scrollInfo != "" {
		title := lipgloss.NewStyle().
			Foreground(mutedColor).
			Align(lipgloss.Right).
			Width(70).
			Render(scrollInfo)
		content = title + "\n" + content
	}
	
	return containerStyle.Render(content)
}

// getDeploymentResourceValue retorna o valor do recurso do deployment (target ou current)
func getDeploymentResourceValue(targetValue, currentValue string) string {
	if targetValue != "" {
		return targetValue
	}
	if currentValue != "" {
		return currentValue
	}
	return ""
}

// renderHPAMainPanel - Painel principal de edição do HPA
func (a *App) renderHPAMainPanel() string {
	hpa := a.model.EditingHPA
	if hpa == nil {
		return ""
	}
	
	var content strings.Builder
	
	// Informações básicas
	content.WriteString(fmt.Sprintf("Namespace: %s\n", hpa.Namespace))
	content.WriteString(fmt.Sprintf("Cluster: %s\n\n", hpa.Cluster))
	
	// Campos principais do HPA
	fields := []struct {
		name        string
		currentVal  string
		fieldKey    string
		description string
	}{
		{"Min Replicas", getFormValue(a.model.FormFields, "min_replicas", getIntPtrString(hpa.MinReplicas)), "min_replicas", "Número mínimo de replicas"},
		{"Max Replicas", getFormValue(a.model.FormFields, "max_replicas", fmt.Sprintf("%d", hpa.MaxReplicas)), "max_replicas", "Número máximo de replicas"},
		{"Target CPU", getFormValue(a.model.FormFields, "target_cpu", getIntPtrString(hpa.TargetCPU)), "target_cpu", "Porcentagem de CPU alvo"},
		{"Target Memory", getFormValue(a.model.FormFields, "target_memory", getIntPtrString(hpa.TargetMemory)), "target_memory", "Porcentagem de memória alvo"},
		{"Rollout", fmt.Sprintf("%t", hpa.PerformRollout), "rollout", "Executar rollout após aplicar"},
		{"DaemonSet Rollout", fmt.Sprintf("%t", hpa.PerformDaemonSetRollout), "daemonset_rollout", "Executar rollout de DaemonSets"},
		{"StatefulSet Rollout", fmt.Sprintf("%t", hpa.PerformStatefulSetRollout), "statefulset_rollout", "Executar rollout de StatefulSets"},
	}
	
	for _, field := range fields {
		style := normalItemStyle
		if a.model.ActivePanel == models.PanelHPAMain && a.model.ActiveField == field.fieldKey {
			style = selectedItemStyle
		}
		
		// Show editing value if currently editing this field
		displayValue := field.currentVal
		if a.model.EditingField && a.model.ActiveField == field.fieldKey {
			displayValue = a.insertCursorInText(a.model.EditingValue, a.model.CursorPosition)
		}

		line := fmt.Sprintf("%s: %s", field.name, displayValue)
		content.WriteString(style.Render(line) + "\n")

		if a.model.ActivePanel == models.PanelHPAMain && a.model.ActiveField == field.fieldKey {
			if a.model.EditingField {
				content.WriteString(helpStyle.Render("  Digite o valor • ENTER Confirmar • ESC Cancelar") + "\n")
			} else {
				if field.fieldKey == "rollout" {
					content.WriteString(helpStyle.Render(fmt.Sprintf("  %s • SPACE Alternar", field.description)) + "\n")
				} else {
					content.WriteString(helpStyle.Render(fmt.Sprintf("  %s • ENTER Editar", field.description)) + "\n")
				}
			}
		}
	}
	
	// Usar renderPanelWithTitle padronizado (70,18)
	title := "Configurações HPA"
	color := primaryColor
	if a.model.ActivePanel == models.PanelHPAMain {
		color = lipgloss.Color("39") // Azul quando ativo
	}

	return renderPanelWithTitle(content.String(), title, 70, 18, color)
}

// renderHPAResourcePanel - Painel de recursos do deployment
func (a *App) renderHPAResourcePanel() string {
	hpa := a.model.EditingHPA
	if hpa == nil {
		return ""
	}
	
	var content strings.Builder
	
	// Título do painel
	content.WriteString(fmt.Sprintf("Deployment: %s\n\n", hpa.DeploymentName))
	
	// Campos de recursos do deployment
	fields := []struct {
		name        string
		currentVal  string
		fieldKey    string
		description string
	}{
		{"CPU Request", getFormValue(a.model.FormFields, "deployment_cpu_request", getDeploymentResourceValue(hpa.TargetCPURequest, hpa.CurrentCPURequest)), "deployment_cpu_request", "CPU request do deployment"},
		{"CPU Limit", getFormValue(a.model.FormFields, "deployment_cpu_limit", getDeploymentResourceValue(hpa.TargetCPULimit, hpa.CurrentCPULimit)), "deployment_cpu_limit", "CPU limit do deployment"},
		{"Memory Request", getFormValue(a.model.FormFields, "deployment_memory_request", getDeploymentResourceValue(hpa.TargetMemoryRequest, hpa.CurrentMemoryRequest)), "deployment_memory_request", "Memory request do deployment"},
		{"Memory Limit", getFormValue(a.model.FormFields, "deployment_memory_limit", getDeploymentResourceValue(hpa.TargetMemoryLimit, hpa.CurrentMemoryLimit)), "deployment_memory_limit", "Memory limit do deployment"},
	}
	
	for _, field := range fields {
		style := normalItemStyle
		if a.model.ActivePanel == models.PanelHPAResources && a.model.ActiveField == field.fieldKey {
			style = selectedItemStyle
		}
		
		// Show editing value if currently editing this field
		displayValue := field.currentVal
		if a.model.EditingField && a.model.ActiveField == field.fieldKey {
			displayValue = a.insertCursorInText(a.model.EditingValue, a.model.CursorPosition)
		}

		line := fmt.Sprintf("%s: %s", field.name, displayValue)
		content.WriteString(style.Render(line) + "\n")

		if a.model.ActivePanel == models.PanelHPAResources && a.model.ActiveField == field.fieldKey {
			if a.model.EditingField {
				content.WriteString(helpStyle.Render("  Digite o valor • ENTER Confirmar • ESC Cancelar") + "\n")
			} else {
				content.WriteString(helpStyle.Render(fmt.Sprintf("  %s • ENTER Editar", field.description)) + "\n")
			}
		}
	}
	
	// Indicador de modificação
	if hpa.ResourcesModified {
		mutedStyle := lipgloss.NewStyle().Foreground(mutedColor)
		content.WriteString("\n" + mutedStyle.Render("⚡ Recursos modificados"))
	}
	
	// Usar renderPanelWithTitle padronizado (70,18)
	title := "Recursos do Deployment"
	color := primaryColor
	if a.model.ActivePanel == models.PanelHPAResources {
		color = lipgloss.Color("39") // Azul quando ativo
	}

	return renderPanelWithTitle(content.String(), title, 70, 18, color)
}

// getIntValue safely dereferences an int32 pointer, returning 0 if nil
func getIntValue(val *int32) int32 {
	if val == nil {
		return 0
	}
	return *val
}

// renderAddCluster renderiza o formulário de adicionar novo cluster
func (a *App) renderAddCluster() string {
	var content strings.Builder

	// Header
	header := fmt.Sprintf("📊 %s - %s", "Adicionar Novo Cluster", "F7")
	content.WriteString(layout.TitleStyle.Render(header))
	content.WriteString("\n\n")

	// Instruções
	instructions := "Preencha os dados do novo cluster:"
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	content.WriteString(textStyle.Render(instructions))
	content.WriteString("\n\n")

	// Formulário
	formContent := a.renderAddClusterForm()
	content.WriteString(formContent)
	content.WriteString("\n\n")

	// Controles
	var controls []string
	controls = append(controls, "↑↓/k j: Navegar")
	controls = append(controls, "Enter: Salvar cluster")
	controls = append(controls, "ESC: Cancelar")
	controls = append(controls, "Tab: Próximo campo")

	controlsText := strings.Join(controls, " • ")
	content.WriteString(layout.HelpStyle.Render(controlsText))

	return content.String()
}

// renderAddClusterForm renderiza o formulário com os 3 campos
func (a *App) renderAddClusterForm() string {
	var content strings.Builder

	// Campos do formulário
	fields := []struct {
		key   string
		label string
		placeholder string
	}{
		{"clusterName", "Nome do Cluster", "Ex: akspriv-prod-central"},
		{"resourceGroup", "Resource Group", "Ex: rg-aks-prod"},
		{"subscription", "Subscription", "Ex: subscription-prod-123"},
	}

	for _, field := range fields {
		// Verificar se é o campo ativo
		isActive := a.model.AddClusterActiveField == field.key

		// Obter valor atual
		value := a.model.AddClusterFormFields[field.key]

		// Se está editando este campo, usar o valor sendo editado
		if isActive && a.model.EditingField {
			value = a.model.EditingValue
		}

		// Renderizar campo
		fieldContent := a.renderFormField(field.label, value, field.placeholder, isActive)
		content.WriteString(fieldContent)
		content.WriteString("\n")
	}

	// Status de validação
	if !a.validateAddClusterForm() {
		content.WriteString("\n")
		warning := "⚠️ Todos os campos são obrigatórios"
		errorStyle := lipgloss.NewStyle().Foreground(layout.ErrorColor)
		content.WriteString(errorStyle.Render(warning))
	}

	return content.String()
}

// renderFormField renderiza um campo individual do formulário
func (a *App) renderFormField(label, value, placeholder string, isActive bool) string {
	var content strings.Builder

	// Label
	labelStyle := layout.NormalItemStyle
	if isActive {
		labelStyle = layout.SelectedItemStyle
	}
	content.WriteString(labelStyle.Render(fmt.Sprintf("%s:", label)))
	content.WriteString("\n")

	// Input field
	inputContent := value
	if inputContent == "" {
		inputContent = placeholder
	}

	// Se está editando e é o campo ativo, mostrar cursor
	if isActive && a.model.EditingField {
		inputContent = a.insertCursorInText(a.model.EditingValue, a.model.CursorPosition)
	}

	// Estilo do input
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(layout.SecondaryColor).
		Padding(0, 1)

	if isActive {
		inputStyle = inputStyle.BorderForeground(layout.PrimaryColor)
	}

	// Box do input
	inputBox := inputStyle.Width(50).Render(inputContent)
	content.WriteString("  " + inputBox)

	return content.String()
}

