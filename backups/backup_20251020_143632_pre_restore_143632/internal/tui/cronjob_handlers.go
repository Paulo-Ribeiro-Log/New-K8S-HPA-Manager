package tui

import (
	"context"
	"fmt"
	k8sClientSet "k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s-hpa-manager/internal/models"
	tea "github.com/charmbracelet/bubbletea"
)

// handleCronJobSelectionKeys - Navegação na seleção de CronJobs
func (a *App) handleCronJobSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "shift+up":
		// Scroll up no painel de CronJobs
		if a.model.CronJobScrollOffset > 0 {
			a.model.CronJobScrollOffset--
		}
		return a, nil
	case "shift+down":
		// Scroll down no painel de CronJobs
		a.model.CronJobScrollOffset++
		return a, nil
	case "up", "k":
		if a.model.SelectedIndex > 0 {
			a.model.SelectedIndex--
		}
	case "down", "j":
		if a.model.SelectedIndex < len(a.model.CronJobs)-1 {
			a.model.SelectedIndex++
		}
	case " ":
		// Selecionar/desselecionar CronJob
		if a.model.SelectedIndex < len(a.model.CronJobs) {
			cronJob := &a.model.CronJobs[a.model.SelectedIndex]
			cronJob.Selected = !cronJob.Selected

			// Atualizar lista de selecionados
			a.updateSelectedCronJobs()
		}
	case "enter":
		// Editar CronJob selecionado
		if a.model.SelectedIndex < len(a.model.CronJobs) {
			//Salvar estado antes da edição
			a.saveCurrentPanelState()
			cronJob := &a.model.CronJobs[a.model.SelectedIndex]
			a.model.EditingCronJob = cronJob
			a.model.State = models.StateCronJobEditing
			a.model.SelectedIndex = 0 // Reset para o campo de status
		}
	case "ctrl+d":
		// Aplicar mudanças nos CronJobs selecionados
		return a.applyCronJobChanges(false)
	case "ctrl+u":
		// Aplicar mudanças em todos os CronJobs selecionados
		return a.applyCronJobChanges(true)
	case "escape":
		// Delegar para handleEscape() que tem lógica de memória de estado
		return a.handleEscape()
	}
	return a, nil
}

// handleCronJobEditingKeys - Navegação na edição de CronJobs
func (a *App) handleCronJobEditingKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case " ":
		// Alternar status do CronJob (Ativo/Suspenso)
		if a.model.EditingCronJob != nil {
			cronJob := a.model.EditingCronJob
			if cronJob.Suspend == nil {
				suspended := false
				cronJob.Suspend = &suspended
			}
			*cronJob.Suspend = !*cronJob.Suspend
			cronJob.Modified = true
		}
	case "ctrl+s":
		// Salvar mudanças e voltar
		if a.model.EditingCronJob != nil && a.model.EditingCronJob.Modified {
			return a.applySingleCronJobChange(a.model.EditingCronJob)
		}
		a.model.State = models.StateCronJobSelection
		a.model.EditingCronJob = nil
	case "escape":
		// Voltar sem salvar
		a.model.State = models.StateCronJobSelection
		a.model.EditingCronJob = nil
	}
	return a, nil
}

// updateSelectedCronJobs atualiza a lista de CronJobs selecionados
func (a *App) updateSelectedCronJobs() {
	a.model.SelectedCronJobs = make([]models.CronJob, 0)
	for _, cronJob := range a.model.CronJobs {
		if cronJob.Selected {
			a.model.SelectedCronJobs = append(a.model.SelectedCronJobs, cronJob)
		}
	}
}

// applyCronJobChanges aplica mudanças nos CronJobs selecionados
func (a *App) applyCronJobChanges(applyAll bool) (tea.Model, tea.Cmd) {
	var cronJobsToApply []models.CronJob

	if applyAll {
		cronJobsToApply = a.model.SelectedCronJobs
	} else {
		// Aplicar apenas os modificados
		for _, cronJob := range a.model.SelectedCronJobs {
			if cronJob.Modified {
				cronJobsToApply = append(cronJobsToApply, cronJob)
			}
		}
	}

	if len(cronJobsToApply) == 0 {
		a.model.Error = "Nenhum CronJob modificado para aplicar"
		return a, nil
	}

	a.model.SuccessMsg = fmt.Sprintf("Aplicando mudanças em %d CronJob(s)...", len(cronJobsToApply))
	return a, a.applyCronJobsAsync(cronJobsToApply)
}

// applySingleCronJobChange aplica mudança em um único CronJob
func (a *App) applySingleCronJobChange(cronJob *models.CronJob) (tea.Model, tea.Cmd) {
	if !cronJob.Modified {
		a.model.Error = "CronJob não foi modificado"
		return a, nil
	}

	a.model.SuccessMsg = fmt.Sprintf("Aplicando mudança no CronJob %s...", cronJob.Name)
	return a, a.applyCronJobsAsync([]models.CronJob{*cronJob})
}

// applyCronJobsAsync aplica mudanças nos CronJobs de forma assíncrona
func (a *App) applyCronJobsAsync(cronJobs []models.CronJob) tea.Cmd {
	return func() tea.Msg {
		if a.model.SelectedCluster == nil {
			return cronJobUpdateMsg{err: fmt.Errorf("no cluster selected")}
		}

		clusterName := a.model.SelectedCluster.Name
		contextName := a.model.SelectedCluster.Context // Usar Context com sufixo -admin
		a.debugLog("🔧 Aplicando CronJobs no cluster: %s (context: %s)", clusterName, contextName)

		client, err := a.kubeManager.GetClient(contextName)
		if err != nil {
			return cronJobUpdateMsg{err: fmt.Errorf("failed to get kubernetes client: %w", err)}
		}

		successCount := 0
		for _, cronJob := range cronJobs {
			err := a.updateCronJobInKubernetes(client, &cronJob)
			if err != nil {
				a.debugLog("❌ Erro ao atualizar CronJob %s: %v", cronJob.Name, err)
				continue
			}
			successCount++
			a.debugLog("✅ CronJob %s atualizado com sucesso", cronJob.Name)
		}

		if successCount == 0 {
			return cronJobUpdateMsg{err: fmt.Errorf("failed to update any cronjobs")}
		}

		return cronJobUpdateMsg{err: nil}
	}
}

// updateCronJobInKubernetes atualiza um CronJob no Kubernetes
func (a *App) updateCronJobInKubernetes(client k8sClientSet.Interface, cronJob *models.CronJob) error {
	ctx := context.Background()

	// Buscar o CronJob atual
	currentCronJob, err := client.BatchV1().CronJobs(cronJob.Namespace).Get(ctx, cronJob.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get cronjob %s: %w", cronJob.Name, err)
	}

	// Atualizar apenas o campo Suspend
	currentCronJob.Spec.Suspend = cronJob.Suspend

	// Aplicar a atualização
	_, err = client.BatchV1().CronJobs(cronJob.Namespace).Update(ctx, currentCronJob, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update cronjob %s: %w", cronJob.Name, err)
	}

	// Marcar como não modificado após sucesso
	cronJob.Modified = false
	cronJob.OriginalSuspend = cronJob.Suspend

	return nil
}