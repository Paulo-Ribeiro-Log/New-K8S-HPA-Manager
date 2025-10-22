package tui

import (
	"context"
	"fmt"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// handleClusterSelectionKeys - Navegação na seleção de clusters
func (a *App) handleClusterSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
			a.adjustClusterScrollToKeepItemVisible()
			a.updateClusterStatsInStatusPanel() // Atualizar cluster selecionado
		}
	case "down", "j":
		if a.model.SelectedIndex < len(a.model.Clusters)-1 {
			a.model.SelectedIndex++
			a.adjustClusterScrollToKeepItemVisible()
			a.updateClusterStatsInStatusPanel() // Atualizar cluster selecionado
		}
	case "enter":
		if a.model.SelectedIndex < len(a.model.Clusters) && len(a.model.Clusters) > 0 {
			//Memorizar posição atual antes da seleção
			a.model.MemorizeCurrentPosition("enter")
			//Selecionar cluster
			cluster := &a.model.Clusters[a.model.SelectedIndex]
			a.model.SelectedCluster = cluster

			//Atualizar nome da aba com o cluster selecionado
			a.updateTabName()

			//Limpar seleções anteriores
			a.model.SelectedNamespaces = make([]models.Namespace, 0)
			a.model.SelectedHPAs = make([]models.HPA, 0)
			a.model.SelectedIndex = 0
			a.model.ActivePanel = models.PanelNamespaces
			a.model.LoadedSessionName = "" // Limpar nome da sessão carregada

			//Transição para seleção de namespaces
			a.model.State = models.StateNamespaceSelection

			//Configurar cluster (contexto kubectl + Azure subscription) e carregar namespaces
			return a, tea.Batch(tea.ClearScreen, a.setupClusterAndLoadNamespaces())
		}
	case "ctrl+l":
		//Carregar sessão - ir para seleção de pastas primeiro
		a.model.State = models.StateSessionFolderSelection
		a.model.SelectedFolderIdx = 0
		a.model.SavingToFolder = false
		return a, a.loadSessionFolders()
	case "f5", "r":
		//Recarregar clusters
		a.model.Loading = true
		a.model.SelectedIndex = 0
		return a, tea.Batch(tea.ClearScreen, a.discoverClusters())
		// F7 agora é tratado globalmente em app.go (adiciona cluster na seleção, gerencia recursos em outros estados)
	}
	return a, nil
}

// handleSessionSelectionKeys - Navegação na seleção de sessões
func (a *App) handleSessionSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Se estamos renomeando sessão, usar as funções auxiliares de edição
	if a.model.RenamingSession {
		switch msg.String() {
		case "enter":
			//Confirmar renome
			if a.model.NewSessionName != "" && a.model.NewSessionName != a.model.RenamingSessionName {
				oldName := a.model.RenamingSessionName
				newName := a.model.NewSessionName
				currentFolder := a.model.CurrentFolder
				a.model.RenamingSession = false
				a.model.RenamingSessionName = ""
				a.model.NewSessionName = ""
				return a, a.renameSessionInFolder(oldName, newName, currentFolder)
			}
			//Se nomes iguais ou nome vazio, cancelar
			a.model.RenamingSession = false
			a.model.RenamingSessionName = ""
			a.model.NewSessionName = ""
		case "ctrl+c", "esc":
			//Cancelar renome
			a.model.RenamingSession = false
			a.model.RenamingSessionName = ""
			a.model.NewSessionName = ""
		default:
			//Usar função auxiliar para processar edição de texto
			var continueEditing bool
			a.model.NewSessionName, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.NewSessionName, nil, nil)

			if continueEditing {
				//Validar posição do cursor
				a.validateCursorPosition(a.model.NewSessionName)
			}
		}
		return a, nil
	}

	// Se estamos confirmando deleção, processar confirmação
	if a.model.ConfirmingDeletion {
		switch msg.String() {
		case "y", "Y":
			//Confirmar deleção
			sessionName := a.model.DeletingSessionName
			currentFolder := a.model.CurrentFolder
			a.model.ConfirmingDeletion = false
			a.model.DeletingSessionName = ""
			return a, a.deleteSessionFromFolder(sessionName, currentFolder)
		case "n", "N", "esc":
			//Cancelar deleção
			a.model.ConfirmingDeletion = false
			a.model.DeletingSessionName = ""
		}
		return a, nil
	}

	switch msg.String() {
	case "up", "k":
		if a.model.SelectedSessionIdx > 0 {
			a.model.SelectedSessionIdx--
		}
	case "down", "j":
		if a.model.SelectedSessionIdx < len(a.model.LoadedSessions)-1 {
			a.model.SelectedSessionIdx++
		}
	case "enter":
		if a.model.SelectedSessionIdx < len(a.model.LoadedSessions) && len(a.model.LoadedSessions) > 0 {
			//Salvar estado antes da seleção
			a.saveCurrentPanelState()

			// Memorizar a posição da sessão selecionada nesta pasta
			if a.model.CurrentFolder != "" {
				a.model.FolderSessionMemory[a.model.CurrentFolder] = a.model.SelectedSessionIdx
			}

			//Carregar a sessão selecionada e restaurar estado
			session := a.model.LoadedSessions[a.model.SelectedSessionIdx]
			return a, a.loadSessionState(&session)
		}
	case "ctrl+r":
		//Iniciar confirmação de deleção da sessão
		if a.model.SelectedSessionIdx < len(a.model.LoadedSessions) && len(a.model.LoadedSessions) > 0 {
			session := a.model.LoadedSessions[a.model.SelectedSessionIdx]
			a.model.ConfirmingDeletion = true
			a.model.DeletingSessionName = session.Name
		}
	case "ctrl+n", "f2":
		//Iniciar renome da sessão
		if a.model.SelectedSessionIdx < len(a.model.LoadedSessions) && len(a.model.LoadedSessions) > 0 {
			session := a.model.LoadedSessions[a.model.SelectedSessionIdx]
			a.model.RenamingSession = true
			a.model.RenamingSessionName = session.Name
			a.model.NewSessionName = session.Name              // Começar com o nome atual
			a.model.CursorPosition = len([]rune(session.Name)) // Cursor no final
		}
	}
	return a, nil
}

// handleSessionFolderSelectionKeys - Navegação na seleção de pastas de sessão
func (a *App) handleSessionFolderSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.model.SelectedFolderIdx > 0 {
			a.model.SelectedFolderIdx--
		}
	case "down", "j":
		if a.model.SelectedFolderIdx < len(a.model.SessionFolders)-1 {
			a.model.SelectedFolderIdx++
		}
	case "enter":
		if a.model.SelectedFolderIdx < len(a.model.SessionFolders) && len(a.model.SessionFolders) > 0 {
			//Salvar estado antes da seleção
			a.saveCurrentPanelState()

			// Memorizar a pasta selecionada
			a.model.LastSelectedFolderIdx = a.model.SelectedFolderIdx

			//Se estamos salvando, usar a pasta selecionada para salvar
			if a.model.SavingToFolder {
				selectedFolder := a.model.SessionFolders[a.model.SelectedFolderIdx]
				a.model.CurrentFolder = selectedFolder
				a.model.SavingToFolder = false
				a.model.EnteringSessionName = true
				a.model.SessionName = ""

				// Determinar o estado correto baseado no tipo de sessão sendo salva
				if len(a.model.SelectedNodePools) > 0 && len(a.model.SelectedHPAs) == 0 {
					// Sessão apenas de node pools
					a.model.State = models.StateNodeSelection
				} else if len(a.model.SelectedHPAs) > 0 && len(a.model.SelectedNodePools) == 0 {
					// Sessão apenas de HPAs
					a.model.State = models.StateHPASelection
				} else if len(a.model.SelectedHPAs) > 0 && len(a.model.SelectedNodePools) > 0 {
					// Sessão mista
					a.model.State = models.StateMixedSession
				} else {
					// Fallback para HPA selection
					a.model.State = models.StateHPASelection
				}
				return a, nil
			} else {
				//Carregando sessões da pasta selecionada
				selectedFolder := a.model.SessionFolders[a.model.SelectedFolderIdx]
				a.model.CurrentFolder = selectedFolder
				a.model.State = models.StateSessionSelection

				// Restaurar última posição de sessão nesta pasta (se existir)
				if lastIdx, exists := a.model.FolderSessionMemory[selectedFolder]; exists {
					a.model.SelectedSessionIdx = lastIdx
				} else {
					a.model.SelectedSessionIdx = 0
				}

				return a, a.loadSessionsFromFolder(selectedFolder)
			}
		}
	case "esc":
		// Tentar restaurar posição anterior primeiro
		if a.model.RestorePreviousPosition() {
			// Se conseguiu restaurar, limpar estados temporários
			a.model.CurrentFolder = ""
			a.model.SavingToFolder = false
			return a, tea.ClearScreen
		}

		// Se não há posição memorizada, voltar para clusters (fallback)
		a.model.State = models.StateClusterSelection
		a.model.SelectedIndex = 0
		a.model.CurrentFolder = ""
		a.model.SavingToFolder = false
		return a, tea.ClearScreen
	}
	return a, nil
}

// handleNamespaceSelectionKeys - Navegação na seleção de namespaces
func (a *App) handleNamespaceSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		// Memorizar posição atual antes de alternar entre painéis
		a.model.MemorizeCurrentPosition("tab")

		//Alternar entre painéis
		if a.model.ActivePanel == models.PanelNamespaces {
			a.model.ActivePanel = models.PanelSelectedNamespaces
			a.model.SelectedIndex = a.model.CurrentNamespaceIdx
		} else {
			a.model.ActivePanel = models.PanelNamespaces
			a.model.CurrentNamespaceIdx = a.model.SelectedIndex
			a.model.SelectedIndex = 0
		}

	case "up", "k":
		if a.model.ActivePanel == models.PanelNamespaces {
			if a.model.SelectedIndex > 0 {
				a.model.SelectedIndex--
				a.adjustNamespaceScrollToKeepItemVisible()
			}
		} else if a.model.ActivePanel == models.PanelSelectedNamespaces {
			if a.model.CurrentNamespaceIdx > 0 {
				a.model.CurrentNamespaceIdx--
			}
		}

	case "down", "j":
		if a.model.ActivePanel == models.PanelNamespaces {
			if a.model.SelectedIndex < len(a.model.Namespaces)-1 {
				a.model.SelectedIndex++
				a.adjustNamespaceScrollToKeepItemVisible()
			}
		} else if a.model.ActivePanel == models.PanelSelectedNamespaces {
			if a.model.CurrentNamespaceIdx < len(a.model.SelectedNamespaces)-1 {
				a.model.CurrentNamespaceIdx++
			}
		}

	case " ":
		//Selecionar/deselecionar namespace
		if a.model.ActivePanel == models.PanelNamespaces && a.model.SelectedIndex < len(a.model.Namespaces) && len(a.model.Namespaces) > 0 {
			ns := &a.model.Namespaces[a.model.SelectedIndex]

			if ns.Selected {
				//Remover da lista de selecionados
				ns.Selected = false
				for i, selected := range a.model.SelectedNamespaces {
					if selected.Name == ns.Name && selected.Cluster == ns.Cluster {
						a.model.SelectedNamespaces = append(a.model.SelectedNamespaces[:i], a.model.SelectedNamespaces[i+1:]...)
						break
					}
				}
			} else {
				//Adicionar à lista de selecionados
				ns.Selected = true
				a.model.SelectedNamespaces = append(a.model.SelectedNamespaces, *ns)
			}
		}

	case "ctrl+r":
		//Remover namespace da lista de selecionados
		if a.model.ActivePanel == models.PanelSelectedNamespaces && a.model.CurrentNamespaceIdx < len(a.model.SelectedNamespaces) && len(a.model.SelectedNamespaces) > 0 {
			selectedNamespace := a.model.SelectedNamespaces[a.model.CurrentNamespaceIdx]

			//Marcar como não selecionado na lista principal
			for i := range a.model.Namespaces {
				if a.model.Namespaces[i].Name == selectedNamespace.Name &&
					a.model.Namespaces[i].Cluster == selectedNamespace.Cluster {
					a.model.Namespaces[i].Selected = false
					break
				}
			}

			//Remover da lista de selecionados
			a.model.SelectedNamespaces = append(a.model.SelectedNamespaces[:a.model.CurrentNamespaceIdx], a.model.SelectedNamespaces[a.model.CurrentNamespaceIdx+1:]...)

			//Ajustar índice se necessário
			if a.model.CurrentNamespaceIdx >= len(a.model.SelectedNamespaces) && len(a.model.SelectedNamespaces) > 0 {
				a.model.CurrentNamespaceIdx = len(a.model.SelectedNamespaces) - 1
			}
		}

	case "enter":
		//Continuar para seleção de HPAs se há namespaces selecionados
		if len(a.model.SelectedNamespaces) > 0 {
			//Salvar estado antes da navegação
			a.saveCurrentPanelState()
			if a.model.ActivePanel == models.PanelSelectedNamespaces {
				//Carregar HPAs do namespace atual
				a.model.State = models.StateHPASelection
				a.model.ActivePanel = models.PanelHPAs
				a.model.SelectedIndex = 0
				return a, a.loadHPAs()
			} else {
				//Mover para o primeiro namespace selecionado
				a.model.State = models.StateHPASelection
				a.model.ActivePanel = models.PanelHPAs
				a.model.SelectedIndex = 0
				a.model.CurrentNamespaceIdx = 0
				return a, a.loadHPAs()
			}
		}
	case "s":
		//Alternar exibição de namespaces de sistema
		a.model.ShowSystemNamespaces = !a.model.ShowSystemNamespaces
		//Limpar seleções anteriores quando mudar filtro
		a.model.SelectedNamespaces = make([]models.Namespace, 0)
		for i := range a.model.Namespaces {
			a.model.Namespaces[i].Selected = false
		}
		//Recarregar namespaces com novo filtro
		return a, a.loadNamespaces()

	case "ctrl+n":
		//Gerenciar node pools - o contexto do cluster já está ativo
		if a.model.SelectedCluster != nil {
			//Limpar seleções anteriores de node pools
			a.model.NodePools = make([]models.NodePool, 0)
			a.model.SelectedNodePools = make([]models.NodePool, 0)
			a.model.SelectedIndex = 0
			a.model.ActivePanel = models.PanelNodePools

			//Transição para gerenciamento de node pools
			a.model.State = models.StateNodeSelection

			//Carregar node pools do cluster
			return a, a.loadNodePools()
		}

	case "ctrl+m":
		//Criar sessão mista (HPAs + Node Pools) - precisa ter cluster selecionado
		if a.model.SelectedCluster != nil {
			//Limpar estados anteriores
			a.model.SelectedNamespaces = make([]models.Namespace, 0)
			a.model.SelectedHPAs = make([]models.HPA, 0)
			a.model.NodePools = make([]models.NodePool, 0)
			a.model.SelectedNodePools = make([]models.NodePool, 0)
			a.model.SelectedIndex = 0
			a.model.ActivePanel = models.PanelNamespaces

			//Transição para modo de sessão mista
			a.model.State = models.StateMixedSession

			//Inicializar nova sessão
			a.model.CurrentSession = &models.Session{
				Name:            "",
				Changes:         make([]models.HPAChange, 0),
				NodePoolChanges: make([]models.NodePoolChange, 0),
			}

			//Carregar namespaces para começar
			return a, a.loadNamespaces()
		}
	}
	return a, nil
}

// handleHPASelectionKeys - Navegação na seleção de HPAs
func (a *App) handleHPASelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Se estamos digitando nome da sessão, usar as funções auxiliares de edição
	if a.model.EnteringSessionName {
		//Definir callbacks para salvar e cancelar
		onSave := func(value string) {
			if value != "" {
				a.debugLog("💾 Saving session '%s' to folder '%s'", value, a.model.CurrentFolder)
				a.model.EnteringSessionName = false
				//Criar uma sessão com os HPAs selecionados
				session := &models.Session{
					Name: value,
				}
				a.debugLog("📊 Selected HPAs count: %d", len(a.model.SelectedHPAs))
				// Debug: Show state of each selected HPA
				for i, hpa := range a.model.SelectedHPAs {
					a.debugLog("  HPA[%d]: %s/%s (Modified: %t)", i, hpa.Namespace, hpa.Name, hpa.Modified)
				}
				// Executar comando de salvamento
				cmd := a.saveSession(session)
				if cmd != nil {
					a.debugLog("✅ Session save command created")
					// Executar o comando diretamente para capturar o resultado
					result := cmd()
					if result != nil {
						a.debugLog("📝 Save result: %+v", result)
					}
				} else {
					a.debugLog("❌ Session save command is nil")
				}
				a.model.SessionName = ""
			}
		}

		onCancel := func() {
			a.model.EnteringSessionName = false
			a.model.SessionName = ""
		}

		//Usar função auxiliar para processar edição
		var continueEditing bool
		a.model.SessionName, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.SessionName, onSave, onCancel)

		if continueEditing {
			//Validar posição do cursor
			a.validateCursorPosition(a.model.SessionName)
		}
		return a, nil
	}

	// Navegação padrão
	switch msg.String() {
	case "ctrl+s":
		//Salvar sessão de HPAs - sempre permitir salvar (mesmo sem modificações, para rollback)
		if len(a.model.SelectedHPAs) > 0 {
			a.model.State = models.StateSessionFolderSelection
			a.model.SelectedFolderIdx = 0
			a.model.SavingToFolder = true
			return a, a.loadSessionFolders()
		}
	// Silencioso se não houver HPAs - não faz nada
	case "ctrl+u":
		//Aplicar todos os HPAs selecionados (independente de modificação)
		if len(a.model.SelectedHPAs) > 0 {
			// Mostrar modal de confirmação
			a.model.ShowConfirmModal = true
			a.model.ConfirmModalMessage = "Aplicar alterações em TODOS os HPAs selecionados"
			a.model.ConfirmModalCallback = "apply_batch_hpa"
			a.model.ConfirmModalItemCount = len(a.model.SelectedHPAs)
			return a, nil
		}
	case "ctrl+l":
		//Carregar sessão
		a.model.State = models.StateSessionFolderSelection
		a.model.SelectedIndex = 0
		//Carregar pastas de sessão
		//Definir pastas de sessão disponíveis
		a.model.SessionFolders = []string{"HPA-Upscale", "HPA-Downscale", "Node-Upscale", "Node-Downscale", "Rollback"}
		a.model.SelectedFolderIdx = 0
		a.model.CurrentFolder = ""
		return a, tea.ClearScreen
	case "esc":
		//Voltar para seleção de namespaces
		a.model.State = models.StateNamespaceSelection
		a.model.SelectedIndex = 0
		return a, tea.ClearScreen
	case "f4":
		//Sair da aplicação
		return a, tea.Quit
	case "shift+up":
		// Scroll up baseado no painel ativo - prioriza painel de status se focado
		// TODO: Implementar IsFocused e ScrollUp no StatusContainer
		// if a.model.StatusContainer.IsFocused() {
		//	a.model.StatusContainer.ScrollUp()
		if false { // Temporário
		} else if a.model.ActivePanel == models.PanelSelectedHPAs {
			if a.model.HPASelectedScrollOffset > 0 {
				a.model.HPASelectedScrollOffset--
				a.debugLog("⬆️ Manual scroll UP - HPASelectedScrollOffset: %d", a.model.HPASelectedScrollOffset)
			}
		} else if a.model.ActivePanel == models.PanelSelectedNodePools {
			if a.model.NodePoolSelectedScrollOffset > 0 {
				a.model.NodePoolSelectedScrollOffset--
			}
		}
		return a, nil
	case "shift+down":
		// Scroll down baseado no painel ativo - prioriza painel de status se focado
		// TODO: Implementar IsFocused e ScrollDown no StatusContainer
		// if a.model.StatusContainer.IsFocused() {
		//	a.model.StatusContainer.ScrollDown()
		if false { // Temporário
		} else if a.model.ActivePanel == models.PanelSelectedHPAs {
			a.model.HPASelectedScrollOffset++
			a.debugLog("⬇️ Manual scroll DOWN - HPASelectedScrollOffset: %d", a.model.HPASelectedScrollOffset)
		} else if a.model.ActivePanel == models.PanelSelectedNodePools {
			a.model.NodePoolSelectedScrollOffset++
		}
		return a, nil
	case "?":
		//Mostrar ajuda
		a.model.PreviousState = a.model.State
		a.model.SaveHelpSnapshot() // Salvar snapshot completo do estado
		a.model.State = models.StateHelp
		a.model.HelpScrollOffset = 0
		return a, tea.ClearScreen

	case " ":
		//Selecionar/deselecionar HPA
		if a.model.ActivePanel == models.PanelHPAs && a.model.SelectedIndex < len(a.model.HPAs) && len(a.model.HPAs) > 0 {
			hpa := &a.model.HPAs[a.model.SelectedIndex]

			if hpa.Selected {
				//Remover da lista de selecionados
				hpa.Selected = false
				for i, selected := range a.model.SelectedHPAs {
					if selected.Name == hpa.Name && selected.Namespace == hpa.Namespace && selected.Cluster == hpa.Cluster {
						a.model.SelectedHPAs = append(a.model.SelectedHPAs[:i], a.model.SelectedHPAs[i+1:]...)
						break
					}
				}
			} else {
				//Adicionar à lista de selecionados
				hpa.Selected = true
				a.model.SelectedHPAs = append(a.model.SelectedHPAs, *hpa)
			}
		}

	case "up", "k":
		a.debugLog("🔼 UP key pressed - ActivePanel: %d (PanelHPAs=%d, PanelSelectedHPAs=%d), SelectedIndex: %d", a.model.ActivePanel, models.PanelHPAs, models.PanelSelectedHPAs, a.model.SelectedIndex)
		if a.model.ActivePanel == models.PanelHPAs {
			if a.model.SelectedIndex > 0 {
				a.model.SelectedIndex--
				a.debugLog("🔼 Navigated up in PanelHPAs to index %d", a.model.SelectedIndex)
				a.adjustHPAListScrollToKeepItemVisible()
			}
		} else if a.model.ActivePanel == models.PanelSelectedHPAs {
			if a.model.SelectedIndex > 0 {
				a.model.SelectedIndex--
				a.debugLog("🔼 Navigated up in PanelSelectedHPAs to index %d", a.model.SelectedIndex)
				// Forçar auto-scroll para manter item visível
				a.adjustHPASelectedScrollToKeepItemVisible()
			}
		}

	case "down", "j":
		a.debugLog("🔽 DOWN key pressed - ActivePanel: %d, SelectedIndex: %d, HPAs: %d, SelectedHPAs: %d", a.model.ActivePanel, a.model.SelectedIndex, len(a.model.HPAs), len(a.model.SelectedHPAs))
		if a.model.ActivePanel == models.PanelHPAs {
			if a.model.SelectedIndex < len(a.model.HPAs)-1 {
				a.model.SelectedIndex++
				a.debugLog("🔽 Navigated down in PanelHPAs to index %d", a.model.SelectedIndex)
				a.adjustHPAListScrollToKeepItemVisible()
			}
		} else if a.model.ActivePanel == models.PanelSelectedHPAs {
			if a.model.SelectedIndex < len(a.model.SelectedHPAs)-1 {
				a.model.SelectedIndex++
				a.debugLog("🔽 Navigated down in PanelSelectedHPAs to index %d", a.model.SelectedIndex)
				// Forçar auto-scroll para manter item visível
				a.adjustHPASelectedScrollToKeepItemVisible()
			}
		}

	case "enter":
		//Editar HPA selecionado
		if a.model.ActivePanel == models.PanelSelectedHPAs && a.model.SelectedIndex < len(a.model.SelectedHPAs) && len(a.model.SelectedHPAs) > 0 {
			//Salvar estado antes da edição
			a.saveCurrentPanelState()
			hpa := &a.model.SelectedHPAs[a.model.SelectedIndex]
			a.model.EditingHPA = hpa
			a.model.State = models.StateHPAEditing
			a.model.ActiveField = "min_replicas"
			a.model.ActivePanel = models.PanelHPAMain

			//Inicializar campos do formulário
			a.model.FormFields = make(map[string]string)

			return a, tea.ClearScreen
		}

	case "ctrl+d":
		//Aplicar HPA individual selecionado (independente de modificação)
		if a.model.ActivePanel == models.PanelSelectedHPAs && a.model.SelectedIndex < len(a.model.SelectedHPAs) && len(a.model.SelectedHPAs) > 0 {
			hpa := a.model.SelectedHPAs[a.model.SelectedIndex]
			// Mostrar modal de confirmação
			a.model.ShowConfirmModal = true
			a.model.ConfirmModalMessage = fmt.Sprintf("Aplicar alterações do HPA:\n%s/%s", hpa.Namespace, hpa.Name)
			a.model.ConfirmModalCallback = "apply_individual_hpa"
			a.model.ConfirmModalItemCount = 1
			return a, nil
		}

	case "ctrl+r":
		//Remover HPA da lista de selecionados
		if a.model.ActivePanel == models.PanelSelectedHPAs && a.model.SelectedIndex < len(a.model.SelectedHPAs) && len(a.model.SelectedHPAs) > 0 {
			selectedHPA := a.model.SelectedHPAs[a.model.SelectedIndex]

			//Marcar como não selecionado na lista principal
			for i := range a.model.HPAs {
				if a.model.HPAs[i].Name == selectedHPA.Name &&
					a.model.HPAs[i].Namespace == selectedHPA.Namespace &&
					a.model.HPAs[i].Cluster == selectedHPA.Cluster {
					a.model.HPAs[i].Selected = false
					break
				}
			}

			//Remover da lista de selecionados
			a.model.SelectedHPAs = append(a.model.SelectedHPAs[:a.model.SelectedIndex], a.model.SelectedHPAs[a.model.SelectedIndex+1:]...)

			//Ajustar índice se necessário
			if a.model.SelectedIndex >= len(a.model.SelectedHPAs) && len(a.model.SelectedHPAs) > 0 {
				a.model.SelectedIndex = len(a.model.SelectedHPAs) - 1
			}
		}

	case "tab":
		// Salvar estado antes de alternar entre painéis
		a.saveStateOnTabSwitch()

		// Salvar estado antes de alternar entre painéis
		a.saveStateOnTabSwitch()

		//Alternar entre painéis de HPAs
		a.debugLog("🔄 TAB pressed - Before: ActivePanel=%d, SelectedIndex=%d", a.model.ActivePanel, a.model.SelectedIndex)
		if a.model.ActivePanel == models.PanelHPAs {
			a.model.ActivePanel = models.PanelSelectedHPAs
			a.model.SelectedIndex = 0
			a.debugLog("🔄 Switched to PanelSelectedHPAs, SelectedIndex=0")
		} else {
			a.model.ActivePanel = models.PanelHPAs
			a.model.SelectedIndex = 0
			a.debugLog("🔄 Switched to PanelHPAs, SelectedIndex=0")
		}
	}
	return a, nil
}

// handleHPAEditingKeys - Navegação na edição de HPA
func (a *App) handleHPAEditingKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tratar Space para toggle dos rollouts primeiro, antes de outras verificações
	if msg.String() == " " && !a.model.EditingField {
		switch a.model.ActiveField {
		case "rollout":
			a.model.EditingHPA.PerformRollout = !a.model.EditingHPA.PerformRollout
			a.model.EditingHPA.Modified = true
		case "daemonset_rollout":
			a.model.EditingHPA.PerformDaemonSetRollout = !a.model.EditingHPA.PerformDaemonSetRollout
			a.model.EditingHPA.Modified = true
		case "statefulset_rollout":
			a.model.EditingHPA.PerformStatefulSetRollout = !a.model.EditingHPA.PerformStatefulSetRollout
			a.model.EditingHPA.Modified = true
		}
		//Atualizar também na lista de HPAs selecionados
		for i := range a.model.SelectedHPAs {
			if a.model.SelectedHPAs[i].Name == a.model.EditingHPA.Name &&
				a.model.SelectedHPAs[i].Namespace == a.model.EditingHPA.Namespace &&
				a.model.SelectedHPAs[i].Cluster == a.model.EditingHPA.Cluster {
				a.model.SelectedHPAs[i] = *a.model.EditingHPA
				break
			}
		}
		return a, nil
	}

	// Se estamos editando um campo específico, usar as funções auxiliares de edição
	if a.model.EditingField {
		//Definir callbacks para salvar e cancelar
		onSave := func(value string) {
			if err := a.applyFieldValue(a.model.ActiveField, value); err == nil {
				a.model.EditingHPA.Modified = true
				//Atualizar também na lista de HPAs selecionados
				for i := range a.model.SelectedHPAs {
					if a.model.SelectedHPAs[i].Name == a.model.EditingHPA.Name &&
						a.model.SelectedHPAs[i].Namespace == a.model.EditingHPA.Namespace &&
						a.model.SelectedHPAs[i].Cluster == a.model.EditingHPA.Cluster {
						a.model.SelectedHPAs[i] = *a.model.EditingHPA
						break
					}
				}
			}
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.CursorPosition = 0
		}

		onCancel := func() {
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.CursorPosition = 0
		}

		//Usar função auxiliar para processar edição
		var continueEditing bool
		a.model.EditingValue, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.EditingValue, onSave, onCancel)

		if continueEditing {
			//Validar posição do cursor
			a.validateCursorPosition(a.model.EditingValue)
		}
		return a, nil
	}

	// Campos do painel principal (HPA)
	mainFields := []string{"min_replicas", "max_replicas", "target_cpu", "target_memory", "rollout", "daemonset_rollout", "statefulset_rollout"}

	// Campos do painel de recursos
	resourceFields := []string{"deployment_cpu_request", "deployment_cpu_limit", "deployment_memory_request", "deployment_memory_limit"}

	switch msg.String() {
	case "up", "k":
		if a.model.ActivePanel == models.PanelHPAMain {
			a.navigateMainPanelUp(mainFields)
		} else if a.model.ActivePanel == models.PanelHPAResources {
			a.navigateResourcePanelUp(resourceFields)
		}
	case "down", "j":
		if a.model.ActivePanel == models.PanelHPAMain {
			a.navigateMainPanelDown(mainFields)
		} else if a.model.ActivePanel == models.PanelHPAResources {
			a.navigateResourcePanelDown(resourceFields)
		}
	case "tab":
		// Salvar estado antes de alternar entre painéis
		a.saveStateOnTabSwitch()

		//Alternar entre painéis
		if a.model.ActivePanel == models.PanelHPAMain {
			a.model.ActivePanel = models.PanelHPAResources
			a.model.ActiveField = resourceFields[0] // Primeiro campo do painel de recursos
		} else {
			a.model.ActivePanel = models.PanelHPAMain
			a.model.ActiveField = mainFields[0] // Primeiro campo do painel principal
		}
	case "enter":
		//Iniciar edição do campo atual (exceto rollouts que usam Space)
		if a.model.EditingHPA != nil && a.model.ActiveField != "rollout" &&
			a.model.ActiveField != "daemonset_rollout" && a.model.ActiveField != "statefulset_rollout" {
			a.model.EditingField = true
			//Definir valor inicial baseado no campo atual
			a.model.EditingValue = a.getCurrentFieldValue(a.model.ActiveField)
			a.model.CursorPosition = len(a.model.EditingValue) // Cursor no final
		}
	case "ctrl+s":
		//Salvar mudanças e voltar
		if a.model.EditingHPA != nil {
			a.model.EditingHPA.Modified = true
			a.model.State = models.StateHPASelection
			a.model.ActivePanel = models.PanelSelectedHPAs
			a.model.EditingHPA = nil
		}
	}
	return a, nil
}

// handleNodePoolSelectionKeys - Navegação na seleção de node pools
func (a *App) handleNodePoolSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Se estamos digitando nome da sessão, usar as funções auxiliares de edição
	if a.model.EnteringSessionName {
		//Definir callbacks para salvar e cancelar
		onSave := func(value string) {
			if value != "" {
				a.debugLog("💾 Saving node pool session '%s' to folder '%s'", value, a.model.CurrentFolder)
				a.model.EnteringSessionName = false
				//Criar uma sessão com os node pools selecionados
				session := &models.Session{
					Name: value,
				}
				a.debugLog("📊 Selected NodePools count: %d", len(a.model.SelectedNodePools))
				// Debug: Show state of each selected node pool
				for i, pool := range a.model.SelectedNodePools {
					a.debugLog("  NodePool[%d]: %s (Modified: %t)", i, pool.Name, pool.Modified)
				}
				// Executar comando de salvamento usando a função unificada
				cmd := a.saveSession(session)
				if cmd != nil {
					a.debugLog("✅ Node pool session save command created")
					// Executar o comando diretamente para capturar o resultado
					result := cmd()
					if result != nil {
						a.debugLog("📝 Save result: %+v", result)
					}
				} else {
					a.debugLog("❌ Node pool session save command is nil")
				}
				a.model.SessionName = ""
			}
		}

		onCancel := func() {
			a.model.EnteringSessionName = false
			a.model.SessionName = ""
		}

		//Usar função auxiliar para processar edição
		var continueEditing bool
		a.model.SessionName, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.SessionName, onSave, onCancel)

		if continueEditing {
			//Validar posição do cursor
			a.validateCursorPosition(a.model.SessionName)
		}
		return a, nil
	}

	switch msg.String() {
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
			// Auto-scroll para Node Pools selecionados
			if a.model.ActivePanel == models.PanelSelectedNodePools {
				a.adjustNodePoolSelectedScrollToKeepItemVisible()
			}
		}
	case "down", "j":
		maxIndex := 0
		if a.model.ActivePanel == models.PanelNodePools {
			maxIndex = len(a.model.NodePools) - 1
		} else if a.model.ActivePanel == models.PanelSelectedNodePools {
			maxIndex = len(a.model.SelectedNodePools) - 1
		}
		if a.model.SelectedIndex < maxIndex {
			a.model.SelectedIndex++
			// Auto-scroll para Node Pools selecionados
			if a.model.ActivePanel == models.PanelSelectedNodePools {
				a.adjustNodePoolSelectedScrollToKeepItemVisible()
			}
		}
	case " ":
		//Selecionar/deselecionar node pool
		if a.model.ActivePanel == models.PanelNodePools && a.model.SelectedIndex < len(a.model.NodePools) && len(a.model.NodePools) > 0 {
			pool := &a.model.NodePools[a.model.SelectedIndex]
			pool.Selected = !pool.Selected

			if pool.Selected {
				//Adicionar à lista de selecionados
				a.model.SelectedNodePools = append(a.model.SelectedNodePools, *pool)
			} else {
				//Remover da lista de selecionados
				for i, selectedPool := range a.model.SelectedNodePools {
					if selectedPool.Name == pool.Name {
						a.model.SelectedNodePools = append(a.model.SelectedNodePools[:i], a.model.SelectedNodePools[i+1:]...)
						break
					}
				}
			}
		}
	case "tab":
		// Salvar estado antes de alternar entre painéis
		a.saveStateOnTabSwitch()

		//Alternar entre painéis
		if a.model.ActivePanel == models.PanelNodePools {
			a.model.ActivePanel = models.PanelSelectedNodePools
			a.model.SelectedIndex = 0
		} else {
			a.model.ActivePanel = models.PanelNodePools
			a.model.SelectedIndex = 0
		}
	case "ctrl+r":
		//Remover node pool da lista de selecionados
		if a.model.ActivePanel == models.PanelSelectedNodePools && a.model.SelectedIndex < len(a.model.SelectedNodePools) && len(a.model.SelectedNodePools) > 0 {
			selectedPool := a.model.SelectedNodePools[a.model.SelectedIndex]

			//Marcar como não selecionado na lista principal
			for i := range a.model.NodePools {
				if a.model.NodePools[i].Name == selectedPool.Name {
					a.model.NodePools[i].Selected = false
					break
				}
			}

			//Remover da lista de selecionados
			a.model.SelectedNodePools = append(a.model.SelectedNodePools[:a.model.SelectedIndex], a.model.SelectedNodePools[a.model.SelectedIndex+1:]...)

			//Ajustar índice se necessário
			if a.model.SelectedIndex >= len(a.model.SelectedNodePools) && len(a.model.SelectedNodePools) > 0 {
				a.model.SelectedIndex = len(a.model.SelectedNodePools) - 1
			}
		}
	case "ctrl+d":
		//Aplicar mudanças dos node pools modificados
		// Verificar se há execução sequencial marcada
		var firstPool, secondPool *models.NodePool
		for i := range a.model.SelectedNodePools {
			pool := &a.model.SelectedNodePools[i]
			if pool.SequenceOrder == 1 {
				firstPool = pool
			} else if pool.SequenceOrder == 2 {
				secondPool = pool
			}
		}

		// Contar node pools modificados
		var modifiedNodePools []models.NodePool
		for _, pool := range a.model.SelectedNodePools {
			if pool.Modified {
				modifiedNodePools = append(modifiedNodePools, pool)
			}
		}

		if len(modifiedNodePools) == 0 && firstPool == nil && secondPool == nil {
			// Nada para aplicar
			return a, nil
		}

		// Mostrar modal de confirmação
		var message string
		itemCount := 0

		if firstPool != nil && secondPool != nil {
			message = fmt.Sprintf("Executar sequencialmente:\n*1 %s → *2 %s", firstPool.Name, secondPool.Name)
			itemCount = 2
		} else if len(modifiedNodePools) > 0 {
			message = "Aplicar alterações nos Node Pools modificados"
			itemCount = len(modifiedNodePools)
		}

		a.model.ShowConfirmModal = true
		a.model.ConfirmModalMessage = message
		a.model.ConfirmModalCallback = "apply_nodepools"
		a.model.ConfirmModalItemCount = itemCount
		return a, nil
	case "ctrl+u":
		//Aplicar todas as mudanças dos node pools modificados (mesmo que ctrl+d para node pools)
		// Verificar se há execução sequencial marcada
		var firstPool, secondPool *models.NodePool
		for i := range a.model.SelectedNodePools {
			pool := &a.model.SelectedNodePools[i]
			if pool.SequenceOrder == 1 {
				firstPool = pool
			} else if pool.SequenceOrder == 2 {
				secondPool = pool
			}
		}

		// Contar node pools modificados
		var modifiedNodePools []models.NodePool
		for _, pool := range a.model.SelectedNodePools {
			if pool.Modified {
				modifiedNodePools = append(modifiedNodePools, pool)
			}
		}

		if len(modifiedNodePools) == 0 && firstPool == nil && secondPool == nil {
			// Nada para aplicar
			return a, nil
		}

		// Mostrar modal de confirmação (mesma lógica do Ctrl+D)
		var message string
		itemCount := 0

		if firstPool != nil && secondPool != nil {
			message = fmt.Sprintf("Executar sequencialmente:\n*1 %s → *2 %s", firstPool.Name, secondPool.Name)
			itemCount = 2
		} else if len(modifiedNodePools) > 0 {
			message = "Aplicar alterações nos Node Pools modificados"
			itemCount = len(modifiedNodePools)
		}

		a.model.ShowConfirmModal = true
		a.model.ConfirmModalMessage = message
		a.model.ConfirmModalCallback = "apply_nodepools"
		a.model.ConfirmModalItemCount = itemCount
		return a, nil
	case "f12":
		// Marcar/desmarcar node pool para execução sequencial (stress test)
		if a.model.ActivePanel == models.PanelSelectedNodePools && a.model.SelectedIndex < len(a.model.SelectedNodePools) && len(a.model.SelectedNodePools) > 0 {
			a.toggleNodePoolSequenceMarking(a.model.SelectedIndex)
			return a, nil
		}
		return a, nil
	case "enter":
		//Editar node pool selecionado
		if a.model.ActivePanel == models.PanelSelectedNodePools && a.model.SelectedIndex < len(a.model.SelectedNodePools) && len(a.model.SelectedNodePools) > 0 {
			pool := a.model.SelectedNodePools[a.model.SelectedIndex]
			a.model.EditingNodePool = &pool
			a.model.State = models.StateNodeEditing
			a.model.ActiveField = "autoscaling_enabled"
			a.model.EditingField = false

			//Inicializar campos do formulário com valores atuais
			if a.model.FormFields == nil {
				a.model.FormFields = make(map[string]string)
			}
			//Não sobrescrever os valores - deixar vazios para usar os padrões
			//Os valores padrão são puxados diretamente do pool na renderização
		}
	case "ctrl+s":
		//Salvar sessão de node pools - sempre permitir salvar (mesmo sem modificações)
		if len(a.model.SelectedNodePools) > 0 {
			a.model.State = models.StateSessionFolderSelection
			a.model.SelectedFolderIdx = 0
			a.model.SavingToFolder = true
			return a, a.loadSessionFolders()
		}
		// Silencioso se não houver Node Pools - não faz nada
	case "shift+up":
		// TODO: Scroll up no painel de status apenas se focado
		// if a.model.StatusContainer.IsFocused() {
		//	a.model.StatusContainer.ScrollUp()
		if false { // Temporário

		}
		return a, nil
	case "shift+down":
		// TODO: Scroll down no painel de status apenas se focado
		// if a.model.StatusContainer.IsFocused() {
		//	a.model.StatusContainer.ScrollDown()
		if false { // Temporário

		}
		return a, nil
	case "esc":
		//Voltar para seleção de clusters
		a.model.State = models.StateClusterSelection
		a.model.SelectedIndex = 0
		return a, tea.ClearScreen
	}
	return a, nil
}

// handleNodePoolEditingKeys - Navegação na edição de node pools
func (a *App) handleNodePoolEditingKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Se estamos editando um campo específico, usar as funções auxiliares de edição
	if a.model.EditingField {
		//Definir callbacks para salvar e cancelar
		onSave := func(value string) {
			if err := a.applyNodePoolFieldValue(a.model.ActiveField, value); err == nil {
				a.model.EditingNodePool.Modified = true
				//Atualizar também na lista de node pools selecionados
				for i := range a.model.SelectedNodePools {
					if a.model.SelectedNodePools[i].Name == a.model.EditingNodePool.Name {
						a.model.SelectedNodePools[i] = *a.model.EditingNodePool
						break
					}
				}
			}
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.CursorPosition = 0
		}

		onCancel := func() {
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.CursorPosition = 0
		}

		//Usar função auxiliar para processar edição
		var continueEditing bool
		a.model.EditingValue, a.model.CursorPosition, continueEditing = a.handleTextEditingKeys(msg, a.model.EditingValue, onSave, onCancel)

		if continueEditing {
			//Validar posição do cursor
			a.validateCursorPosition(a.model.EditingValue)
		}
		return a, nil
	}

	// Navegação normal
	switch msg.String() {
	case "up", "k":
		a.moveToPrevNodeField()
	case "down", "j":
		a.moveToNextNodeField()
	case "tab":
		// Salvar estado antes de alternar entre painéis
		a.saveStateOnTabSwitch()

		a.moveToNextNodeField()
	case "enter":
		// Para autoscaling, fazer toggle direto. Para outros campos, entrar em modo de edição
		if a.model.ActiveField == "autoscaling_enabled" {
			// Toggle do autoscaling
			a.model.EditingNodePool.AutoscalingEnabled = !a.model.EditingNodePool.AutoscalingEnabled
			a.model.EditingNodePool.Modified = true

			// Atualizar na lista de selecionados
			for i := range a.model.SelectedNodePools {
				if a.model.SelectedNodePools[i].Name == a.model.EditingNodePool.Name {
					a.model.SelectedNodePools[i] = *a.model.EditingNodePool
					break
				}
			}

			// Se desabilitou autoscaling, definir node count como 0
			if !a.model.EditingNodePool.AutoscalingEnabled {
				a.model.EditingNodePool.NodeCount = 0
			}
		} else {
			// Iniciar edição de campo numérico
			a.model.EditingField = true
			// Definir valor inicial baseado no campo atual
			a.model.EditingValue = a.getCurrentNodePoolFieldValue(a.model.ActiveField)
			a.model.CursorPosition = len([]rune(a.model.EditingValue)) // Cursor no final
		}
	case "esc":
		//Voltar para seleção de node pools (sem salvar mudanças)
		a.model.State = models.StateNodeSelection
		a.model.ActivePanel = models.PanelSelectedNodePools
		a.model.EditingNodePool = nil
		return a, tea.ClearScreen
	case "ctrl+s":
		//Salvar mudanças e voltar
		if a.model.EditingNodePool != nil {
			a.debugLog("💾 Saving changes for node pool %s\n", a.model.EditingNodePool.Name)
			a.model.EditingNodePool.Modified = true

			//Atualizar também na lista de node pools selecionados
			for i := range a.model.SelectedNodePools {
				if a.model.SelectedNodePools[i].Name == a.model.EditingNodePool.Name {
					a.debugLog("📝 Updating selected node pool %s to Modified=true\n", a.model.EditingNodePool.Name)
					a.model.SelectedNodePools[i] = *a.model.EditingNodePool
					break
				}
			}

			a.model.State = models.StateNodeSelection
			a.model.ActivePanel = models.PanelSelectedNodePools
			a.model.EditingNodePool = nil
		}
	}
	return a, nil
}

// moveToNextNodeField - Mover para próximo campo na edição de node pool
func (a *App) moveToNextNodeField() {
	// Construir lista de campos baseada no modo de autoscaling
	var fields []string

	// Autoscaling sempre é o primeiro campo
	fields = append(fields, "autoscaling_enabled")

	if a.model.EditingNodePool != nil && a.model.EditingNodePool.AutoscalingEnabled {
		// Modo autoscaling: min, max, current
		fields = append(fields, "min_nodes", "max_nodes", "node_count")
	} else {
		// Modo manual: apenas node count
		fields = append(fields, "node_count")
	}

	for i, field := range fields {
		if a.model.ActiveField == field {
			if i < len(fields)-1 {
				a.model.ActiveField = fields[i+1]
			} else {
				a.model.ActiveField = fields[0]
			}
			break
		}
	}
}

// moveToPrevNodeField - Mover para campo anterior na edição de node pool
func (a *App) moveToPrevNodeField() {
	// Construir lista de campos baseada no modo de autoscaling
	var fields []string

	// Autoscaling sempre é o primeiro campo
	fields = append(fields, "autoscaling_enabled")

	if a.model.EditingNodePool != nil && a.model.EditingNodePool.AutoscalingEnabled {
		// Modo autoscaling: min, max, current
		fields = append(fields, "min_nodes", "max_nodes", "node_count")
	} else {
		// Modo manual: apenas node count
		fields = append(fields, "node_count")
	}

	for i, field := range fields {
		if a.model.ActiveField == field {
			if i > 0 {
				a.model.ActiveField = fields[i-1]
			} else {
				a.model.ActiveField = fields[len(fields)-1]
			}
			break
		}
	}
}

// getCurrentNodePoolFieldValue retorna o valor atual do campo sendo editado
func (a *App) getCurrentNodePoolFieldValue(fieldName string) string {
	if a.model.EditingNodePool == nil {
		return ""
	}

	pool := a.model.EditingNodePool
	switch fieldName {
	case "node_count":
		return fmt.Sprintf("%d", pool.NodeCount)
	case "min_nodes":
		return fmt.Sprintf("%d", pool.MinNodeCount)
	case "max_nodes":
		return fmt.Sprintf("%d", pool.MaxNodeCount)
	default:
		return ""
	}
}

// applyNodePoolFieldValue aplica o valor editado ao campo do node pool
func (a *App) applyNodePoolFieldValue(fieldName string, value string) error {
	if a.model.EditingNodePool == nil {
		return fmt.Errorf("no node pool being edited")
	}

	pool := a.model.EditingNodePool
	switch fieldName {
	case "node_count":
		val, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		pool.NodeCount = int32(val)
	case "min_nodes":
		val, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		pool.MinNodeCount = int32(val)
	case "max_nodes":
		val, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		pool.MaxNodeCount = int32(val)
	}

	return nil
}

// handleMixedSessionKeys - Navegação para sessões mistas (HPAs + Node Pools)
func (a *App) handleMixedSessionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		// Salvar estado antes de alternar entre painéis
		a.saveStateOnTabSwitch()

		//Alternar entre painéis: Namespaces/HPAs ↔ Node Pools
		switch a.model.ActivePanel {
		case models.PanelNamespaces, models.PanelHPAs:
			a.model.ActivePanel = models.PanelNodePools
			a.model.SelectedIndex = 0
			//Carregar node pools se ainda não carregados
			if len(a.model.NodePools) == 0 {
				return a, a.loadNodePools()
			}
		case models.PanelNodePools:
			a.model.ActivePanel = models.PanelNamespaces
			a.model.SelectedIndex = 0
		}
		return a, nil

	case "ctrl+s":
		//Salvar sessão mista - sempre permitir salvar (mesmo sem modificações, para rollback)
		if a.model.CurrentSession != nil {
			a.model.EnteringSessionName = true
			a.model.SessionName = ""
			return a, nil
		}
		// Silencioso se não houver sessão ativa - não faz nada

	case "ctrl+d", "ctrl+u":
		//Aplicar todas as mudanças da sessão mista
		if a.model.CurrentSession != nil {
			// Contar total de itens na sessão mista
			totalItems := len(a.model.SelectedHPAs) + len(a.model.SelectedNodePools)

			// Mostrar modal de confirmação
			a.model.ShowConfirmModal = true
			a.model.ConfirmModalMessage = fmt.Sprintf("Aplicar alterações da sessão mista:\n%d HPAs + %d Node Pools", len(a.model.SelectedHPAs), len(a.model.SelectedNodePools))
			a.model.ConfirmModalCallback = "apply_mixed_session"
			a.model.ConfirmModalItemCount = totalItems
			return a, nil
		}

	case "enter":
		//Editar item selecionado dependendo do painel ativo
		switch a.model.ActivePanel {
		case models.PanelHPAs:
			if a.model.SelectedIndex < len(a.model.SelectedHPAs) && len(a.model.SelectedHPAs) > 0 {
				a.model.EditingHPA = &a.model.SelectedHPAs[a.model.SelectedIndex]
				a.model.State = models.StateHPAEditing
				a.model.ActiveField = "min_replicas"
				a.model.ActivePanel = models.PanelHPAMain // Iniciar no painel principal
				return a, nil
			}
		case models.PanelNodePools:
			if a.model.SelectedIndex < len(a.model.SelectedNodePools) && len(a.model.SelectedNodePools) > 0 {
				a.model.EditingNodePool = &a.model.SelectedNodePools[a.model.SelectedIndex]
				a.model.State = models.StateNodeEditing
				a.model.ActiveField = "min_nodes"
				return a, nil
			}
		}

	case "space":
		// Memorizar posição atual antes da seleção
		a.model.MemorizeCurrentPosition("space")
		//Selecionar/desselecionar itens
		switch a.model.ActivePanel {
		case models.PanelNamespaces:
			if a.model.SelectedIndex < len(a.model.Namespaces) && len(a.model.Namespaces) > 0 {
				namespace := &a.model.Namespaces[a.model.SelectedIndex]
				namespace.Selected = !namespace.Selected

				if namespace.Selected {
					a.model.SelectedNamespaces = append(a.model.SelectedNamespaces, *namespace)
				} else {
					//Remover da lista de selecionados
					for i, selected := range a.model.SelectedNamespaces {
						if selected.Name == namespace.Name {
							a.model.SelectedNamespaces = append(a.model.SelectedNamespaces[:i], a.model.SelectedNamespaces[i+1:]...)
							break
						}
					}
				}
			}
		case models.PanelNodePools:
			if a.model.SelectedIndex < len(a.model.NodePools) && len(a.model.NodePools) > 0 {
				nodePool := &a.model.NodePools[a.model.SelectedIndex]
				nodePool.Selected = !nodePool.Selected

				if nodePool.Selected {
					a.model.SelectedNodePools = append(a.model.SelectedNodePools, *nodePool)
				} else {
					//Remover da lista de selecionados
					for i, selected := range a.model.SelectedNodePools {
						if selected.Name == nodePool.Name {
							a.model.SelectedNodePools = append(a.model.SelectedNodePools[:i], a.model.SelectedNodePools[i+1:]...)
							break
						}
					}
				}
			}
		}

	case "shift+up":
		// TODO: Scroll up no painel de status apenas se focado
		// if a.model.StatusContainer.IsFocused() {
		//	a.model.StatusContainer.ScrollUp()
		if false { // Temporário

		}
		return a, nil
	case "shift+down":
		// TODO: Scroll down no painel de status apenas se focado
		// if a.model.StatusContainer.IsFocused() {
		//	a.model.StatusContainer.ScrollDown()
		if false { // Temporário

		}
		return a, nil
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
		}
	case "down", "j":
		maxIndex := 0
		switch a.model.ActivePanel {
		case models.PanelNamespaces:
			maxIndex = len(a.model.Namespaces) - 1
		case models.PanelHPAs:
			maxIndex = len(a.model.SelectedHPAs) - 1
		case models.PanelNodePools:
			maxIndex = len(a.model.NodePools) - 1
		}
		if a.model.SelectedIndex < maxIndex {
			a.model.SelectedIndex++
		}
	}
	return a, nil
}

// enrichHPAWithDeploymentResources enriquece o HPA com informações do deployment
func (a *App) enrichHPAWithDeploymentResources(hpa *models.HPA) tea.Cmd {
	return func() tea.Msg {
		clusterName := hpa.Cluster

		//Obter o client do Kubernetes para este cluster
		clientset, err := a.kubeManager.GetClient(clusterName)
		if err != nil {
			return hpaDeploymentResourcesEnrichedMsg{
				hpa: hpa,
				err: fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err),
			}
		}

		client := kubernetes.NewClient(clientset, clusterName)
		ctx := context.Background()

		//Enriquecer o HPA com informações do deployment
		err = client.EnrichHPAWithDeploymentResources(ctx, hpa)
		if err != nil {
			return hpaDeploymentResourcesEnrichedMsg{
				hpa: hpa,
				err: fmt.Errorf("failed to enrich HPA with deployment resources: %w", err),
			}
		}

		return hpaDeploymentResourcesEnrichedMsg{
			hpa: hpa,
			err: nil,
		}
	}
}

// Mensagem para indicar que o HPA foi enriquecido com recursos do deployment
type hpaDeploymentResourcesEnrichedMsg struct {
	hpa *models.HPA
	err error
}

// navigateMainPanelUp - Navegar para cima no painel principal
func (a *App) navigateMainPanelUp(fields []string) {
	currentIdx := 0
	for i, field := range fields {
		if a.model.ActiveField == field {
			currentIdx = i
			break
		}
	}
	if currentIdx > 0 {
		a.model.ActiveField = fields[currentIdx-1]
	}
}

// navigateMainPanelDown - Navegar para baixo no painel principal
func (a *App) navigateMainPanelDown(fields []string) {
	currentIdx := 0
	for i, field := range fields {
		if a.model.ActiveField == field {
			currentIdx = i
			break
		}
	}
	if currentIdx < len(fields)-1 {
		a.model.ActiveField = fields[currentIdx+1]
	}
}

// navigateResourcePanelUp - Navegar para cima no painel de recursos
func (a *App) navigateResourcePanelUp(fields []string) {
	currentIdx := 0
	for i, field := range fields {
		if a.model.ActiveField == field {
			currentIdx = i
			break
		}
	}
	if currentIdx > 0 {
		a.model.ActiveField = fields[currentIdx-1]
	}
}

// navigateResourcePanelDown - Navegar para baixo no painel de recursos
func (a *App) navigateResourcePanelDown(fields []string) {
	currentIdx := 0
	for i, field := range fields {
		if a.model.ActiveField == field {
			currentIdx = i
			break
		}
	}
	if currentIdx < len(fields)-1 {
		a.model.ActiveField = fields[currentIdx+1]
	}
}

// ==================== TAB MANAGEMENT HANDLERS ====================

// updateTabName atualiza o nome da aba ativa baseado no contexto atual
func (a *App) updateTabName() {
	if a.tabManager == nil {
		return
	}

	activeTab := a.tabManager.GetActiveTab()
	if activeTab == nil {
		return
	}

	// Construir nome da aba baseado no contexto
	var tabName string

	// Prioridade: Sessão > Cluster > Padrão
	if a.model.LoadedSessionName != "" {
		// Sessão carregada - mostrar nome da sessão
		tabName = a.model.LoadedSessionName

		// Se houver cluster também, adicionar entre parênteses
		if a.model.SelectedCluster != nil {
			tabName = fmt.Sprintf("%s (%s)", a.model.LoadedSessionName, a.model.SelectedCluster.Name)
		}
	} else if a.model.SelectedCluster != nil {
		// Cluster selecionado sem sessão
		tabName = a.model.SelectedCluster.Name
		activeTab.ClusterContext = a.model.SelectedCluster.Context
	} else {
		// Nenhum contexto - manter nome padrão ou criar novo
		if activeTab.Name == "" || activeTab.Name == "Principal" {
			tabName = fmt.Sprintf("Aba %d", a.tabManager.ActiveIdx+1)
		} else {
			return // Manter nome existente
		}
	}

	// Atualizar nome da aba
	activeTab.Name = tabName
	a.debugLog("📑 Aba renomeada para: %s", tabName)
}

// handleNewTab cria uma nova aba
func (a *App) handleNewTab() (tea.Model, tea.Cmd) {
	// Verificar se TabManager existe
	if a.tabManager == nil {
		a.tabManager = models.NewTabManager()
	}

	// Verificar se pode adicionar mais abas
	if !a.tabManager.CanAddTab() {
		a.model.StatusContainer.AddWarning("tabs", "⚠️ Máximo de 10 abas atingido!")
		return a, nil
	}

	// Criar novo modelo para a aba
	newModel := &models.AppModel{
		State:               models.StateClusterSelection,
		Loading:             false,
		SelectedIndex:       0,
		ActivePanel:         models.PanelNamespaces,
		SelectedNamespaces:  make([]models.Namespace, 0),
		SelectedHPAs:        make([]models.HPA, 0),
		CurrentNamespaceIdx: 0,
		FormFields:          make(map[string]string),
		StatusContainer:     a.model.StatusContainer, // Compartilhar status container
		StateMemory:         make(map[models.AppState]*models.PanelState),
		FolderSessionMemory: make(map[string]int),
	}

	// Nome da nova aba
	tabName := fmt.Sprintf("Nova Aba %d", a.tabManager.GetTabCount()+1)

	// Adicionar aba
	if a.tabManager.AddTab(tabName, "", newModel) {
		a.model.StatusContainer.AddSuccess("tabs", fmt.Sprintf("✅ Nova aba criada: %s", tabName))

		// A nova aba já é ativada automaticamente por AddTab
		// Atualizar modelo da app para a nova aba
		activeTab := a.tabManager.GetActiveTab()
		if activeTab != nil {
			a.model = activeTab.Model
		}

		return a, a.discoverClusters()
	}

	return a, nil
}

// handleCloseTab fecha a aba atual
func (a *App) handleCloseTab() (tea.Model, tea.Cmd) {
	// Verificar se TabManager existe
	if a.tabManager == nil || a.tabManager.GetTabCount() == 0 {
		return a, nil
	}

	// Não pode fechar a última aba
	if a.tabManager.GetTabCount() == 1 {
		a.model.StatusContainer.AddWarning("tabs", "⚠️ Não é possível fechar a última aba!")
		return a, nil
	}

	// Verificar se há modificações não salvas
	activeTab := a.tabManager.GetActiveTab()
	if activeTab != nil && activeTab.Modified {
		// TODO: Adicionar confirmação futura
		a.model.StatusContainer.AddWarning("tabs", "⚠️ Aba tem modificações não salvas!")
	}

	// Fechar aba atual
	currentIdx := a.tabManager.ActiveIdx
	if a.tabManager.CloseTab(currentIdx) {
		a.model.StatusContainer.AddInfo("tabs", fmt.Sprintf("🗑️ Aba %d fechada", currentIdx+1))

		// Atualizar modelo para a nova aba ativa
		activeTab := a.tabManager.GetActiveTab()
		if activeTab != nil {
			a.model = activeTab.Model
		}
	}

	return a, nil
}

// handleNavigateTab navega entre abas (próxima/anterior)
func (a *App) handleNavigateTab(direction string) (tea.Model, tea.Cmd) {
	// Verificar se TabManager existe
	if a.tabManager == nil || a.tabManager.GetTabCount() == 0 {
		return a, nil
	}

	// Salvar modelo atual na aba ativa antes de mudar
	currentTab := a.tabManager.GetActiveTab()
	if currentTab != nil {
		currentTab.Model = a.model
	}

	currentIdx := a.tabManager.ActiveIdx
	tabCount := a.tabManager.GetTabCount()
	var targetIdx int

	if direction == "next" {
		// Alt+Right: próxima aba (com wrap-around)
		targetIdx = (currentIdx + 1) % tabCount
	} else if direction == "prev" {
		// Alt+Left: aba anterior (com wrap-around)
		targetIdx = (currentIdx - 1 + tabCount) % tabCount
	} else {
		return a, nil
	}

	// Mudar para a aba calculada
	if a.tabManager.SwitchToTab(targetIdx) {
		// Atualizar modelo para a nova aba
		newTab := a.tabManager.GetActiveTab()
		if newTab != nil {
			a.model = newTab.Model
			directionIcon := "➡️"
			if direction == "prev" {
				directionIcon = "⬅️"
			}
			a.model.StatusContainer.AddInfo("tabs", fmt.Sprintf("%s Aba %d: %s", directionIcon, targetIdx+1, newTab.Name))
		}
	}

	return a, tea.ClearScreen
}

// handleSwitchTab muda para uma aba específica baseado na tecla pressionada
func (a *App) handleSwitchTab(key string) (tea.Model, tea.Cmd) {
	// Verificar se TabManager existe
	if a.tabManager == nil || a.tabManager.GetTabCount() == 0 {
		return a, nil
	}

	// Mapear tecla para índice (Alt+1 = 0, Alt+2 = 1, ..., Alt+0 = 9)
	tabIndexMap := map[string]int{
		"alt+1": 0, "alt+2": 1, "alt+3": 2, "alt+4": 3, "alt+5": 4,
		"alt+6": 5, "alt+7": 6, "alt+8": 7, "alt+9": 8, "alt+0": 9,
	}

	targetIdx, exists := tabIndexMap[key]
	if !exists {
		return a, nil
	}

	// Verificar se o índice existe
	if targetIdx >= a.tabManager.GetTabCount() {
		a.model.StatusContainer.AddWarning("tabs", fmt.Sprintf("⚠️ Aba %d não existe!", targetIdx+1))
		return a, nil
	}

	// Salvar modelo atual na aba ativa antes de mudar
	currentTab := a.tabManager.GetActiveTab()
	if currentTab != nil {
		currentTab.Model = a.model
	}

	// Mudar para a aba especificada
	if a.tabManager.SwitchToTab(targetIdx) {
		// Atualizar modelo para a nova aba
		newTab := a.tabManager.GetActiveTab()
		if newTab != nil {
			a.model = newTab.Model
			a.model.StatusContainer.AddInfo("tabs", fmt.Sprintf("📑 Mudou para aba %d: %s", targetIdx+1, newTab.Name))
		}
	}

	return a, nil
}
