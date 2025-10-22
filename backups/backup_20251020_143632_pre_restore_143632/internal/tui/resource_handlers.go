package tui

import (
	"fmt"
	"strconv"
	"strings"
	"k8s-hpa-manager/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

// handleClusterResourceSelectionKeys - Navegação na seleção de recursos do cluster
func (a *App) handleClusterResourceSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
		}
	case "down", "j":
		if a.model.SelectedIndex < len(a.model.ClusterResources)-1 {
			a.model.SelectedIndex++
		}
	case " ":
		// Toggle seleção do recurso atual
		if a.model.SelectedIndex < len(a.model.ClusterResources) {
			resource := &a.model.ClusterResources[a.model.SelectedIndex]
			resource.Selected = !resource.Selected
			
			// Atualizar lista de recursos selecionados
			a.updateSelectedResources()
		}
	case "enter":
		// Editar recurso selecionado
		if len(a.model.ClusterResources) == 0 {
			a.model.Error = "Nenhum recurso encontrado para editar"
			return a, nil
		}
		if a.model.SelectedIndex >= len(a.model.ClusterResources) {
			a.model.Error = fmt.Sprintf("Índice inválido: %d >= %d", a.model.SelectedIndex, len(a.model.ClusterResources))
			return a, nil
		}

		//Salvar estado antes da edição
		a.saveCurrentPanelState()
		resource := &a.model.ClusterResources[a.model.SelectedIndex]
		a.model.EditingResource = resource
		a.model.State = models.StateClusterResourceEditing
		a.model.Error = "" // Limpar qualquer erro anterior
		a.model.SuccessMsg = "" // Limpar mensagens de sucesso
		a.initResourceEditingForm()
	case "/":
		// TODO: Implementar busca
		a.model.Error = "Busca não implementada ainda"
	case "ctrl+a":
		// Selecionar todos os recursos filtrados
		for i := range a.model.ClusterResources {
			if a.shouldShowResource(&a.model.ClusterResources[i]) {
				a.model.ClusterResources[i].Selected = true
			}
		}
		a.updateSelectedResources()
	case "ctrl+d":
		// Aplicar mudanças no recurso individual
		if a.model.SelectedIndex < len(a.model.ClusterResources) {
			resource := &a.model.ClusterResources[a.model.SelectedIndex]
			if resource.Modified {
				return a, a.applyResourceChange(resource)
			}
		}
	case "ctrl+u":
		// Aplicar todas as mudanças
		return a, a.applyAllResourceChanges()
	case "ctrl+s":
		// Salvar sessão
		return a.saveResourceSession()
	}
	return a, nil
}

// handleClusterResourceEditingKeys - Navegação na edição de recursos
func (a *App) handleClusterResourceEditingKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.model.EditingResource == nil {
		a.model.State = models.StateClusterResourceSelection
		return a, nil
	}
	
	// Se estamos editando um campo específico, usar as funções auxiliares de edição
	if a.model.EditingField {
		// Definir callbacks para salvar e cancelar
		onSave := func(value string) {
			a.setCurrentEditingValue(value)
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.EditingResource.Modified = true
		}

		onCancel := func() {
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.CursorPosition = 0
		}

		// Usar função auxiliar para processar edição
		var continueEditing bool
		a.model.EditingValue, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.EditingValue, onSave, onCancel)

		if continueEditing {
			// Validar posição do cursor
			a.validateCursorPosition(a.model.EditingValue)
		}
		return a, nil
	}

	// Navegação normal entre campos
	switch msg.String() {
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
		}
	case "down", "j":
		maxFields := 6 // CPU Req, CPU Lim, Mem Req, Mem Lim, Replicas, Storage
		if a.model.SelectedIndex < maxFields-1 {
			a.model.SelectedIndex++
		}
	case "tab":
		// Salvar estado antes de trocar campo
		a.saveStateOnTabSwitch()

		// Próximo campo
		maxFields := 6
		a.model.SelectedIndex = (a.model.SelectedIndex + 1) % maxFields
	case "enter":
		// Entrar no modo de edição
		//Salvar estado antes da edição
		a.saveCurrentPanelState()
		a.model.EditingField = true
		a.model.EditingValue = a.getCurrentEditingValue()
		a.model.CursorPosition = len([]rune(a.model.EditingValue)) // Cursor no final
	case "ctrl+s":
		// Salvar e voltar
		a.applyResourceEditChanges()
		a.model.State = models.StateClusterResourceSelection
	case "esc":
		// Voltar sem salvar
		a.model.State = models.StateClusterResourceSelection
	}
	
	return a, nil
}

// handlePrometheusStackKeys - Navegação específica do Prometheus
func (a *App) handlePrometheusStackKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
		}
	case "down", "j":
		if a.model.SelectedIndex < len(a.model.ClusterResources)-1 {
			a.model.SelectedIndex++
		}
	case " ":
		// Toggle seleção do recurso atual
		if a.model.SelectedIndex < len(a.model.ClusterResources) {
			resource := &a.model.ClusterResources[a.model.SelectedIndex]
			resource.Selected = !resource.Selected
			a.updateSelectedResources()
		}
	case "enter":
		// Editar recurso selecionado
		if len(a.model.ClusterResources) == 0 {
			a.model.Error = "Nenhum recurso encontrado para editar"
			return a, nil
		}
		if a.model.SelectedIndex >= len(a.model.ClusterResources) {
			a.model.Error = fmt.Sprintf("Índice inválido: %d >= %d", a.model.SelectedIndex, len(a.model.ClusterResources))
			return a, nil
		}

		//Salvar estado antes da edição
		a.saveCurrentPanelState()
		resource := &a.model.ClusterResources[a.model.SelectedIndex]
		a.model.EditingResource = resource
		a.model.State = models.StateClusterResourceEditing
		a.model.Error = "" // Limpar qualquer erro anterior
		a.model.SuccessMsg = "" // Limpar mensagens de sucesso
		a.initResourceEditingForm()
	case "ctrl+d":
		// Aplicar mudanças individuais
		if a.model.SelectedIndex < len(a.model.ClusterResources) {
			resource := &a.model.ClusterResources[a.model.SelectedIndex]
			if resource.Modified {
				return a, a.applyResourceChange(resource)
			}
		}
	case "ctrl+u":
		// Aplicar todo o stack Prometheus
		return a, a.applyPrometheusStack()
	case "ctrl+s":
		// Salvar configuração do stack
		return a.savePrometheusSession()
	}
	return a, nil
}

// updateSelectedResources atualiza a lista de recursos selecionados
func (a *App) updateSelectedResources() {
	selected := make([]models.ClusterResource, 0)
	for _, resource := range a.model.ClusterResources {
		if resource.Selected {
			selected = append(selected, resource)
		}
	}
	a.model.SelectedResources = selected
}

// shouldShowResource determina se um recurso deve ser exibido baseado nos filtros
func (a *App) shouldShowResource(resource *models.ClusterResource) bool {
	// Filtro por tipo
	if a.model.ResourceFilter != models.ResourceMonitoring && resource.Type != a.model.ResourceFilter {
		return false
	}
	
	// Se está em modo Prometheus, mostrar apenas recursos relacionados
	if a.model.PrometheusStackMode {
		prometheusComponents := []string{"prometheus", "grafana", "alertmanager", "node-exporter"}
		resourceName := strings.ToLower(resource.Name)
		isPrometheus := false
		for _, component := range prometheusComponents {
			if strings.Contains(resourceName, component) {
				isPrometheus = true
				break
			}
		}
		if !isPrometheus && resource.Namespace != "monitoring" {
			return false
		}
	}
	
	return true
}

// initResourceEditingForm inicializa o formulário de edição de recurso
func (a *App) initResourceEditingForm() {
	if a.model.EditingResource == nil {
		return
	}
	
	a.model.SelectedIndex = 0
	a.model.EditingField = false
	a.model.EditingValue = ""
	
	// Configurar valores iniciais se não foram definidos
	if a.model.EditingResource.TargetCPURequest == "" {
		a.model.EditingResource.TargetCPURequest = a.model.EditingResource.CurrentCPURequest
	}
	if a.model.EditingResource.TargetCPULimit == "" {
		a.model.EditingResource.TargetCPULimit = a.model.EditingResource.CurrentCPULimit
	}
	if a.model.EditingResource.TargetMemoryRequest == "" {
		a.model.EditingResource.TargetMemoryRequest = a.model.EditingResource.CurrentMemoryRequest
	}
	if a.model.EditingResource.TargetMemoryLimit == "" {
		a.model.EditingResource.TargetMemoryLimit = a.model.EditingResource.CurrentMemoryLimit
	}
}

// getCurrentEditingValue retorna o valor atual do campo sendo editado
func (a *App) getCurrentEditingValue() string {
	if a.model.EditingResource == nil {
		return ""
	}
	
	switch a.model.SelectedIndex {
	case 0: // CPU Request
		if a.model.EditingResource.TargetCPURequest != "" {
			return a.model.EditingResource.TargetCPURequest
		}
		return a.model.EditingResource.CurrentCPURequest
	case 1: // CPU Limit
		if a.model.EditingResource.TargetCPULimit != "" {
			return a.model.EditingResource.TargetCPULimit
		}
		return a.model.EditingResource.CurrentCPULimit
	case 2: // Memory Request
		if a.model.EditingResource.TargetMemoryRequest != "" {
			return a.model.EditingResource.TargetMemoryRequest
		}
		return a.model.EditingResource.CurrentMemoryRequest
	case 3: // Memory Limit
		if a.model.EditingResource.TargetMemoryLimit != "" {
			return a.model.EditingResource.TargetMemoryLimit
		}
		return a.model.EditingResource.CurrentMemoryLimit
	case 4: // Replicas
		if a.model.EditingResource.TargetReplicas != nil {
			return fmt.Sprintf("%d", *a.model.EditingResource.TargetReplicas)
		}
		return fmt.Sprintf("%d", a.model.EditingResource.Replicas)
	case 5: // Storage (para StatefulSets)
		return a.model.EditingResource.StorageSize
	}
	return ""
}

// setCurrentEditingValue define o valor do campo sendo editado
func (a *App) setCurrentEditingValue(value string) {
	if a.model.EditingResource == nil {
		return
	}
	
	switch a.model.SelectedIndex {
	case 0: // CPU Request
		a.model.EditingResource.TargetCPURequest = value
	case 1: // CPU Limit
		a.model.EditingResource.TargetCPULimit = value
	case 2: // Memory Request
		a.model.EditingResource.TargetMemoryRequest = value
	case 3: // Memory Limit
		a.model.EditingResource.TargetMemoryLimit = value
	case 4: // Replicas
		if replicas, err := strconv.Atoi(value); err == nil {
			rep := int32(replicas)
			a.model.EditingResource.TargetReplicas = &rep
		}
	case 5: // Storage
		a.model.EditingResource.StorageSize = value
	}
}

// incrementResourceValue incrementa o valor do campo atual
func (a *App) incrementResourceValue() {
	if a.model.EditingResource == nil {
		return
	}
	
	switch a.model.SelectedIndex {
	case 0: // CPU Request
		a.model.EditingResource.TargetCPURequest = a.incrementCPUValue(a.model.EditingResource.TargetCPURequest)
	case 1: // CPU Limit
		a.model.EditingResource.TargetCPULimit = a.incrementCPUValue(a.model.EditingResource.TargetCPULimit)
	case 2: // Memory Request
		a.model.EditingResource.TargetMemoryRequest = a.incrementMemoryValue(a.model.EditingResource.TargetMemoryRequest)
	case 3: // Memory Limit
		a.model.EditingResource.TargetMemoryLimit = a.incrementMemoryValue(a.model.EditingResource.TargetMemoryLimit)
	case 4: // Replicas
		if a.model.EditingResource.TargetReplicas != nil {
			*a.model.EditingResource.TargetReplicas++
		} else {
			rep := a.model.EditingResource.Replicas + 1
			a.model.EditingResource.TargetReplicas = &rep
		}
	}
	a.model.EditingResource.Modified = true
}

// decrementResourceValue decrementa o valor do campo atual
func (a *App) decrementResourceValue() {
	if a.model.EditingResource == nil {
		return
	}
	
	switch a.model.SelectedIndex {
	case 0: // CPU Request
		a.model.EditingResource.TargetCPURequest = a.decrementCPUValue(a.model.EditingResource.TargetCPURequest)
	case 1: // CPU Limit
		a.model.EditingResource.TargetCPULimit = a.decrementCPUValue(a.model.EditingResource.TargetCPULimit)
	case 2: // Memory Request
		a.model.EditingResource.TargetMemoryRequest = a.decrementMemoryValue(a.model.EditingResource.TargetMemoryRequest)
	case 3: // Memory Limit
		a.model.EditingResource.TargetMemoryLimit = a.decrementMemoryValue(a.model.EditingResource.TargetMemoryLimit)
	case 4: // Replicas
		if a.model.EditingResource.TargetReplicas != nil && *a.model.EditingResource.TargetReplicas > 1 {
			*a.model.EditingResource.TargetReplicas--
		} else if a.model.EditingResource.Replicas > 1 {
			rep := a.model.EditingResource.Replicas - 1
			a.model.EditingResource.TargetReplicas = &rep
		}
	}
	a.model.EditingResource.Modified = true
}

// incrementCPUValue incrementa valor de CPU de forma inteligente
func (a *App) incrementCPUValue(current string) string {
	// Lógica de incremento para valores de CPU (100m, 200m, 500m, 1000m, etc.)
	if strings.HasSuffix(current, "m") {
		if val, err := strconv.Atoi(strings.TrimSuffix(current, "m")); err == nil {
			if val < 100 {
				return "100m"
			} else if val < 500 {
				return fmt.Sprintf("%dm", val+100)
			} else if val < 1000 {
				return "1000m"
			} else {
				return fmt.Sprintf("%dm", val+500)
			}
		}
	}
	// Fallback
	return "500m"
}

// decrementCPUValue decrementa valor de CPU de forma inteligente
func (a *App) decrementCPUValue(current string) string {
	if strings.HasSuffix(current, "m") {
		if val, err := strconv.Atoi(strings.TrimSuffix(current, "m")); err == nil {
			if val > 500 {
				return fmt.Sprintf("%dm", val-500)
			} else if val > 100 {
				return fmt.Sprintf("%dm", val-100)
			} else if val > 50 {
				return "50m"
			}
		}
	}
	return current
}

// incrementMemoryValue incrementa valor de memória de forma inteligente
func (a *App) incrementMemoryValue(current string) string {
	// Lógica para incrementar memória (256Mi, 512Mi, 1Gi, 2Gi, etc.)
	if strings.HasSuffix(current, "Mi") {
		if val, err := strconv.Atoi(strings.TrimSuffix(current, "Mi")); err == nil {
			if val < 512 {
				return fmt.Sprintf("%dMi", val*2)
			} else if val < 1024 {
				return "1Gi"
			} else {
				return fmt.Sprintf("%dMi", val+512)
			}
		}
	} else if strings.HasSuffix(current, "Gi") {
		if val, err := strconv.Atoi(strings.TrimSuffix(current, "Gi")); err == nil {
			return fmt.Sprintf("%dGi", val*2)
		}
	}
	return "512Mi"
}

// decrementMemoryValue decrementa valor de memória de forma inteligente
func (a *App) decrementMemoryValue(current string) string {
	if strings.HasSuffix(current, "Mi") {
		if val, err := strconv.Atoi(strings.TrimSuffix(current, "Mi")); err == nil {
			if val > 512 {
				return fmt.Sprintf("%dMi", val-256)
			} else if val > 256 {
				return "256Mi"
			} else if val > 128 {
				return "128Mi"
			}
		}
	} else if strings.HasSuffix(current, "Gi") {
		if val, err := strconv.Atoi(strings.TrimSuffix(current, "Gi")); err == nil {
			if val > 1 {
				return fmt.Sprintf("%dGi", val/2)
			} else {
				return "512Mi"
			}
		}
	}
	return current
}

// applyResourceEditChanges aplica as mudanças feitas na edição
func (a *App) applyResourceEditChanges() {
	if a.model.EditingResource == nil {
		return
	}
	
	// Marcar como modificado se algum valor mudou
	if a.model.EditingResource.TargetCPURequest != a.model.EditingResource.CurrentCPURequest ||
		a.model.EditingResource.TargetCPULimit != a.model.EditingResource.CurrentCPULimit ||
		a.model.EditingResource.TargetMemoryRequest != a.model.EditingResource.CurrentMemoryRequest ||
		a.model.EditingResource.TargetMemoryLimit != a.model.EditingResource.CurrentMemoryLimit ||
		(a.model.EditingResource.TargetReplicas != nil && *a.model.EditingResource.TargetReplicas != a.model.EditingResource.Replicas) {
		a.model.EditingResource.Modified = true
	}
	
	// Atualizar na lista principal
	for i := range a.model.ClusterResources {
		if a.model.ClusterResources[i].Name == a.model.EditingResource.Name &&
			a.model.ClusterResources[i].Namespace == a.model.EditingResource.Namespace {
			a.model.ClusterResources[i] = *a.model.EditingResource
			break
		}
	}
}