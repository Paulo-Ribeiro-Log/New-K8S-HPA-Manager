package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderClusterResourceDiscovery renderiza a tela de descoberta de recursos
func (a *App) renderClusterResourceDiscovery() string {
	if a.model.Loading {
		var mode string
		if a.model.PrometheusStackMode {
			mode = "Prometheus Stack"
		} else {
			mode = "Todos os Recursos"
		}
		
		content := fmt.Sprintf(
			"🔍 Descobrindo recursos do cluster...\n\n"+
			"Cluster: %s\n"+
			"Modo: %s\n\n"+
			"⏳ Analisando deployments, statefulsets e daemonsets...",
			a.model.SelectedCluster.Name,
			mode,
		)
		
		return renderPanelWithTitle(content, fmt.Sprintf("Recursos do Cluster: %s", a.model.SelectedCluster.Name), 60, 18, primaryColor)
	}
	return ""
}

// renderClusterResourceSelection renderiza a tela de seleção de recursos
func (a *App) renderClusterResourceSelection() string {
	clusterName := a.model.SelectedCluster.Name
	sessionInfo := a.renderSessionHeader()

	// Título da tela
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00ADD8")).
		MarginBottom(1)
	screenTitle := titleStyle.Render("🔧 RECURSOS DO CLUSTER")

	// Header
	var content strings.Builder
	content.WriteString(fmt.Sprintf("🎯 Cluster: %s\n", clusterName))
	content.WriteString(fmt.Sprintf("📊 Total: %d | Selecionados: %d\n\n", len(a.model.ClusterResources), len(a.model.SelectedResources)))
	
	// Lista de recursos (aplicar filtro)
	for i, resource := range a.model.ClusterResources {
		// Pular recursos que não passam no filtro
		if !a.shouldShowResource(&resource) {
			continue
		}

		selection := "◯"
		if resource.Selected {
			selection = "●"
		}

		modified := ""
		if resource.Modified {
			modified = " ⚡"
		}

		// Criar linha principal
		mainLine := fmt.Sprintf("  %s %s %s%s",
			selection, resource.Type.String(), resource.Name, modified)

		// Aplicar estilo de seleção se for o item selecionado
		if i == a.model.SelectedIndex {
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content.WriteString(selectedStyle.Render(mainLine) + "\n")
		} else {
			content.WriteString(mainLine + "\n")
		}
		
		// Usar campos Display* se disponíveis (com métricas), senão usar Current* (sem métricas)
		cpuReq := resource.CurrentCPURequest
		memReq := resource.CurrentMemoryRequest
		if resource.DisplayCPURequest != "" {
			cpuReq = resource.DisplayCPURequest
		}
		if resource.DisplayMemoryRequest != "" {
			memReq = resource.DisplayMemoryRequest
		}

		content.WriteString(fmt.Sprintf("     CPU: %s/%s | MEM: %s/%s | Rep: %d\n",
			cpuReq, resource.CurrentCPULimit,
			memReq, resource.CurrentMemoryLimit, resource.Replicas))
		
		if resource.Modified {
			if resource.TargetCPURequest != "" && resource.TargetCPURequest != resource.CurrentCPURequest {
				content.WriteString(fmt.Sprintf("     → CPU Req: %s\n", resource.TargetCPURequest))
			}
			if resource.TargetCPULimit != "" && resource.TargetCPULimit != resource.CurrentCPULimit {
				content.WriteString(fmt.Sprintf("     → CPU Lim: %s\n", resource.TargetCPULimit))
			}
			if resource.TargetMemoryRequest != "" && resource.TargetMemoryRequest != resource.CurrentMemoryRequest {
				content.WriteString(fmt.Sprintf("     → MEM Req: %s\n", resource.TargetMemoryRequest))
			}
			if resource.TargetMemoryLimit != "" && resource.TargetMemoryLimit != resource.CurrentMemoryLimit {
				content.WriteString(fmt.Sprintf("     → MEM Lim: %s\n", resource.TargetMemoryLimit))
			}
		}
		content.WriteString("\n")
	}
	
	if len(a.model.ClusterResources) == 0 {
		content.WriteString("Nenhum recurso encontrado\n")
	}
	
	// Controles
	content.WriteString("\nControles:\n")
	content.WriteString("↑↓ Navegar • SPACE Selecionar • ENTER Editar\n")
	content.WriteString("Ctrl+A Selecionar todos • Ctrl+D Aplicar • Ctrl+U Aplicar todos • ESC Voltar\n")
	content.WriteString("Abas: Alt+1-9/0 Mudar • Ctrl+T Nova • Ctrl+W Fechar\n")
	
	// Título
	title := "Recursos do Cluster"

	panel := renderPanelWithTitle(content.String(), title, 60, 18, primaryColor)

	return sessionInfo + screenTitle + "\n" + panel
}

// renderPrometheusStackManagement renderiza a interface específica do Prometheus
func (a *App) renderPrometheusStackManagement() string {
	// Header com cluster, sessão e contexto
	contextBox := renderContextBox(a.model.SelectedCluster, a.model.LoadedSessionName, "Prometheus Stack - Recursos de Monitoramento")

	sessionInfo := contextBox + a.renderSessionHeader()

	// Título da tela
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00ADD8")).
		MarginBottom(1)
	screenTitle := titleStyle.Render("🔧 RECURSOS DO CLUSTER")

	// Calcular altura dinâmica do painel
	terminalHeight := a.height
	headerLines := 6 // Session info + título + header do painel
	statusPanelHeight := 15
	footerLines := 2
	availableHeight := terminalHeight - headerLines - statusPanelHeight - footerLines
	if availableHeight < 10 {
		availableHeight = 10
	}
	maxPanelHeight := availableHeight

	// Construir todas as linhas primeiro
	var allLines []string

	// Header
	allLines = append(allLines, fmt.Sprintf("📊 Monitoring Stack: %s", a.model.SelectedCluster.Name))
	allLines = append(allLines, fmt.Sprintf("🎯 Componentes: %d | Selecionados: %d", len(a.model.ClusterResources), len(a.model.SelectedResources)))
	allLines = append(allLines, "")

	// Contar recursos visíveis
	visibleCount := 0
	for _, resource := range a.model.ClusterResources {
		if a.shouldShowResource(&resource) {
			visibleCount++
		}
	}

	// Lista de recursos Prometheus
	lineIndexToResourceIndex := make(map[int]int) // Mapear linha para índice do recurso
	currentLine := len(allLines)

	for i, resource := range a.model.ClusterResources {
		if !a.shouldShowResource(&resource) {
			continue
		}

		lineIndexToResourceIndex[currentLine] = i

		selection := "◯"
		if resource.Selected {
			selection = "●"
		}

		modified := ""
		if resource.Modified {
			modified = " ⚡"
		}

		mainLine := fmt.Sprintf("  %s 📊 %s%s", selection, resource.Name, modified)
		allLines = append(allLines, mainLine)
		currentLine++

		// Usar campos Display* se disponíveis
		cpuReq := resource.CurrentCPURequest
		memReq := resource.CurrentMemoryRequest
		if resource.DisplayCPURequest != "" {
			cpuReq = resource.DisplayCPURequest
		}
		if resource.DisplayMemoryRequest != "" {
			memReq = resource.DisplayMemoryRequest
		}

		allLines = append(allLines, fmt.Sprintf("      CPU: %s/%s | MEM: %s/%s",
			cpuReq, resource.CurrentCPULimit, memReq, resource.CurrentMemoryLimit))
		currentLine++

		if resource.Modified {
			if resource.TargetCPURequest != "" && resource.TargetCPURequest != resource.CurrentCPURequest {
				allLines = append(allLines, fmt.Sprintf("      → CPU Req: %s", resource.TargetCPURequest))
				currentLine++
			}
			if resource.TargetCPULimit != "" && resource.TargetCPULimit != resource.CurrentCPULimit {
				allLines = append(allLines, fmt.Sprintf("      → CPU Lim: %s", resource.TargetCPULimit))
				currentLine++
			}
			if resource.TargetMemoryRequest != "" && resource.TargetMemoryRequest != resource.CurrentMemoryRequest {
				allLines = append(allLines, fmt.Sprintf("      → MEM Req: %s", resource.TargetMemoryRequest))
				currentLine++
			}
			if resource.TargetMemoryLimit != "" && resource.TargetMemoryLimit != resource.CurrentMemoryLimit {
				allLines = append(allLines, fmt.Sprintf("      → MEM Lim: %s", resource.TargetMemoryLimit))
				currentLine++
			}
		}
		allLines = append(allLines, "")
		currentLine++
	}

	if len(a.model.ClusterResources) == 0 {
		allLines = append(allLines, "Nenhum componente Prometheus encontrado")
	}

	// Controles
	allLines = append(allLines, "")
	allLines = append(allLines, "Controles:")
	allLines = append(allLines, "↑↓ Navegar • SPACE Selecionar • ENTER Editar")
	allLines = append(allLines, "Ctrl+D Aplicar • Ctrl+U Aplicar Stack • ESC Voltar")
	allLines = append(allLines, "Abas: Alt+1-9/0 Mudar • Ctrl+T Nova • Ctrl+W Fechar")

	// Ajustar scroll para manter item selecionado visível
	a.adjustPrometheusStackScrollToKeepItemVisible(lineIndexToResourceIndex, len(allLines), maxPanelHeight)

	// Aplicar scroll e construir conteúdo visível
	var content strings.Builder
	start := a.model.PrometheusStackScrollOffset
	end := start + maxPanelHeight
	if end > len(allLines) {
		end = len(allLines)
	}

	for lineIdx := start; lineIdx < end; lineIdx++ {
		line := allLines[lineIdx]

		// Aplicar estilo de seleção se for a linha do item selecionado
		if resourceIdx, exists := lineIndexToResourceIndex[lineIdx]; exists && resourceIdx == a.model.SelectedIndex {
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content.WriteString(selectedStyle.Render(line) + "\n")
		} else {
			content.WriteString(line + "\n")
		}
	}

	// Indicador de scroll se necessário
	scrollIndicator := ""
	if len(allLines) > maxPanelHeight {
		scrollIndicator = fmt.Sprintf(" [%d-%d/%d]", start+1, end, len(allLines))
	}

	title := "Prometheus Stack Management" + scrollIndicator
	panel := renderPanelWithTitle(content.String(), title, 60, maxPanelHeight, primaryColor)

	return a.getTabBar() + sessionInfo + screenTitle + "\n" + panel
}

// renderClusterResourceEditing renderiza a tela de edição de recursos
func (a *App) renderClusterResourceEditing() string {
	if a.model.EditingResource == nil {
		return "⚠️ Nenhum recurso selecionado para edição"
	}
	
	resource := a.model.EditingResource
	sessionInfo := a.renderSessionHeader()
	
	var content strings.Builder
	content.WriteString(fmt.Sprintf("📝 Editando: %s\n", resource.Name))
	content.WriteString(fmt.Sprintf("📍 Namespace: %s\n", resource.Namespace))
	content.WriteString(fmt.Sprintf("🔧 Tipo: %s\n\n", resource.WorkloadType))
	
	// Campos editáveis - separados em requests e limits
	fields := []string{"CPU Request", "CPU Limit", "Memory Request", "Memory Limit", "Replicas", "Storage"}
	
	for i, fieldName := range fields {
		
		var value string
		switch i {
		case 0: // CPU Request
			value = resource.TargetCPURequest
			if value == "" {
				value = resource.CurrentCPURequest
			}
		case 1: // CPU Limit
			value = resource.TargetCPULimit
			if value == "" {
				value = resource.CurrentCPULimit
			}
		case 2: // Memory Request
			value = resource.TargetMemoryRequest
			if value == "" {
				value = resource.CurrentMemoryRequest
			}
		case 3: // Memory Limit
			value = resource.TargetMemoryLimit
			if value == "" {
				value = resource.CurrentMemoryLimit
			}
		case 4: // Replicas
			if resource.TargetReplicas != nil {
				value = fmt.Sprintf("%d", *resource.TargetReplicas)
			} else {
				value = fmt.Sprintf("%d", resource.Replicas)
			}
		case 5: // Storage
			value = resource.StorageSize
			if value == "" {
				value = "N/A"
			}
		}
		
		var fieldLine string
		if a.model.EditingField && i == a.model.SelectedIndex {
			// Mostrar cursor na posição correta
			displayValue := a.insertCursorInText(a.model.EditingValue, a.model.CursorPosition)
			fieldLine = fmt.Sprintf("  %s: %s", fieldName, displayValue)
		} else {
			fieldLine = fmt.Sprintf("  %s: %s", fieldName, value)
		}

		// Aplicar estilo de seleção se for o item selecionado
		if i == a.model.SelectedIndex {
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content.WriteString(selectedStyle.Render(fieldLine) + "\n")
		} else {
			content.WriteString(fieldLine + "\n")
		}
	}
	
	content.WriteString("\nControles:\n")
	if a.model.EditingField {
		content.WriteString("←→ Mover cursor • Digite para inserir • ENTER Salvar • Ctrl+C Cancelar\n")
		content.WriteString("Backspace Apagar antes • Delete Apagar atual • Home/End Início/Fim\n")
	} else {
		content.WriteString("↑↓ Navegar • ENTER Editar campo • Ctrl+S Salvar tudo • ESC Voltar\n")
	}
	content.WriteString("Abas: Alt+1-9/0 Mudar • Ctrl+T Nova • Ctrl+W Fechar\n")
	
	panel := renderPanelWithTitle(content.String(), fmt.Sprintf("Editando %s", resource.Name), 60, 18, primaryColor)
	
	return sessionInfo + panel
}

