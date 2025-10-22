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
		
		return renderPanelWithTitle(content, fmt.Sprintf("Recursos do Cluster: %s", a.model.SelectedCluster.Name), 70, 18, primaryColor)
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

	// Header com filtros
	var content strings.Builder
	content.WriteString(fmt.Sprintf("🎯 Cluster: %s\n", clusterName))
	
	// Mostrar filtro ativo
	filterText := "Todos os recursos"
	if a.model.ResourceFilter >= 0 && int(a.model.ResourceFilter) < 7 {
		filterText = fmt.Sprintf("Filtro ativo: %s", a.model.ResourceFilter.String())
	}
	content.WriteString(fmt.Sprintf("📊 %s | Total: %d | Selecionados: %d\n", filterText, len(a.model.ClusterResources), len(a.model.SelectedResources)))
	
	// Barra de filtros rápidos
	content.WriteString("Filtros: [1]📊 [2]🌐 [3]🔒 [4]📦 [5]🌐 [6]📝 [7]⚙️\n\n")
	
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
		
		content.WriteString(fmt.Sprintf("     CPU: %s/%s | MEM: %s/%s | Rep: %d\n",
			resource.CurrentCPURequest, resource.CurrentCPULimit, 
			resource.CurrentMemoryRequest, resource.CurrentMemoryLimit, resource.Replicas))
		
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
	content.WriteString("↑↓ Navegar • SPACE Selecionar • ENTER Editar • 1-7 Filtros\n")
	content.WriteString("Ctrl+A Selecionar filtrados • Ctrl+D Aplicar • Ctrl+U Aplicar todos • ESC Voltar\n")
	
	// Título dinâmico baseado no filtro
	title := "Recursos do Cluster"
	if a.model.ResourceFilter >= 0 && int(a.model.ResourceFilter) < 7 {
		title = fmt.Sprintf("Recursos do Cluster (%s)", a.model.ResourceFilter.String())
	}

	panel := renderPanelWithTitle(content.String(), title, 70, 18, primaryColor)

	return sessionInfo + screenTitle + "\n" + panel
}

// renderPrometheusStackManagement renderiza a interface específica do Prometheus
func (a *App) renderPrometheusStackManagement() string {
	clusterName := a.model.SelectedCluster.Name
	sessionInfo := a.renderSessionHeader()

	// Título da tela
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00ADD8")).
		MarginBottom(1)
	screenTitle := titleStyle.Render("🔧 RECURSOS DO CLUSTER")

	var content strings.Builder
	content.WriteString(fmt.Sprintf("📊 Monitoring Stack: %s\n", clusterName))
	content.WriteString(fmt.Sprintf("🎯 Componentes: %d | Selecionados: %d\n", len(a.model.ClusterResources), len(a.model.SelectedResources)))
	
	// Filtros e presets
	content.WriteString("Filtros: [1]📊 [2]🌐 [3]🔒 [4]📦 [5]🌐 [6]📝 [7]⚙️\n")
	content.WriteString("📊 Presets: [P]Dev [M]Prod [L]Scale\n\n")
	
	// Lista de recursos Prometheus (aplicar filtro)
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
		mainLine := fmt.Sprintf("  %s 📊 %s%s", selection, resource.Name, modified)

		// Aplicar estilo de seleção se for o item selecionado
		if i == a.model.SelectedIndex {
			selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("#00ADD8")).Foreground(lipgloss.Color("#FFFFFF"))
			content.WriteString(selectedStyle.Render(mainLine) + "\n")
		} else {
			content.WriteString(mainLine + "\n")
		}
		
		content.WriteString(fmt.Sprintf("      CPU: %s/%s | MEM: %s/%s\n",
			resource.CurrentCPURequest, resource.CurrentCPULimit,
			resource.CurrentMemoryRequest, resource.CurrentMemoryLimit))
		
		if resource.Modified {
			if resource.TargetCPURequest != "" && resource.TargetCPURequest != resource.CurrentCPURequest {
				content.WriteString(fmt.Sprintf("      → CPU Req: %s\n", resource.TargetCPURequest))
			}
			if resource.TargetCPULimit != "" && resource.TargetCPULimit != resource.CurrentCPULimit {
				content.WriteString(fmt.Sprintf("      → CPU Lim: %s\n", resource.TargetCPULimit))
			}
			if resource.TargetMemoryRequest != "" && resource.TargetMemoryRequest != resource.CurrentMemoryRequest {
				content.WriteString(fmt.Sprintf("      → MEM Req: %s\n", resource.TargetMemoryRequest))
			}
			if resource.TargetMemoryLimit != "" && resource.TargetMemoryLimit != resource.CurrentMemoryLimit {
				content.WriteString(fmt.Sprintf("      → MEM Lim: %s\n", resource.TargetMemoryLimit))
			}
		}
		content.WriteString("\n")
	}
	
	if len(a.model.ClusterResources) == 0 {
		content.WriteString("Nenhum componente Prometheus encontrado\n")
	}
	
	// Controles específicos
	content.WriteString("\nControles Prometheus:\n")
	content.WriteString("↑↓ Navegar • SPACE Selecionar • ENTER Editar • 1-7 Filtros\n")
	content.WriteString("P/M/L Presets • Ctrl+D Aplicar • Ctrl+U Aplicar Stack • ESC Voltar\n")

	panel := renderPanelWithTitle(content.String(), "Prometheus Stack Management", 70, 18, primaryColor)

	return sessionInfo + screenTitle + "\n" + panel
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
			fieldLine = fmt.Sprintf("  %s: [%s]", fieldName, displayValue)
		} else {
			fieldLine = fmt.Sprintf("  %s: [%s]", fieldName, value)
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
	
	panel := renderPanelWithTitle(content.String(), fmt.Sprintf("Editando %s", resource.Name), 70, 18, primaryColor)
	
	return sessionInfo + panel
}

