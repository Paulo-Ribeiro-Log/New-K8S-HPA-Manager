package tui

import (
	"k8s-hpa-manager/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

// handleAddClusterKeys - Navegação no formulário de adicionar cluster
func (a *App) handleAddClusterKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		// Navegação para campo anterior
		a.navigateAddClusterField(-1)
	case "down", "j", "tab":
		// Navegação para próximo campo
		a.navigateAddClusterField(1)
	case "enter":
		// Tentar salvar o cluster
		if a.validateAddClusterForm() {
			return a, a.saveNewCluster()
		}
	case "ctrl+c", "esc":
		// Cancelar adição
		a.model.State = models.StateClusterSelection
		a.model.AddingCluster = false
		a.model.AddClusterFormFields = make(map[string]string)
		a.model.AddClusterActiveField = ""
		a.model.EditingField = false
		a.model.EditingValue = ""
		a.model.CursorPosition = 0
		return a, tea.ClearScreen
	default:
		// Edição de texto usando o sistema centralizado
		if a.model.EditingField {
			var continueEditing bool
			a.model.EditingValue, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.EditingValue, nil, nil)

			if continueEditing {
				// Atualizar o campo atual
				a.model.AddClusterFormFields[a.model.AddClusterActiveField] = a.model.EditingValue
				// Validar posição do cursor
				a.validateCursorPosition(a.model.EditingValue)
			} else {
				// Sair da edição
				a.model.EditingField = false
				a.model.EditingValue = ""
				a.model.CursorPosition = 0
			}
		} else {
			// Entrar em modo de edição
			a.model.EditingField = true
			a.model.EditingValue = a.model.AddClusterFormFields[a.model.AddClusterActiveField]
			a.model.CursorPosition = len(a.model.EditingValue)
		}
	}
	return a, nil
}

// navigateAddClusterField navega entre os campos do formulário de cluster
func (a *App) navigateAddClusterField(direction int) {
	for i, field := range a.model.AddClusterFieldOrder {
		if field == a.model.AddClusterActiveField {
			newIdx := i + direction
			if newIdx < 0 {
				newIdx = len(a.model.AddClusterFieldOrder) - 1
			} else if newIdx >= len(a.model.AddClusterFieldOrder) {
				newIdx = 0
			}
			a.model.AddClusterActiveField = a.model.AddClusterFieldOrder[newIdx]

			// Se estava editando, parar edição e atualizar campo
			if a.model.EditingField {
				a.model.AddClusterFormFields[a.model.AddClusterFieldOrder[i]] = a.model.EditingValue
				a.model.EditingField = false
				a.model.EditingValue = ""
				a.model.CursorPosition = 0
			}
			break
		}
	}
}

// validateAddClusterForm valida se todos os campos estão preenchidos
func (a *App) validateAddClusterForm() bool {
	for _, field := range a.model.AddClusterFieldOrder {
		if a.model.AddClusterFormFields[field] == "" {
			return false
		}
	}
	return true
}