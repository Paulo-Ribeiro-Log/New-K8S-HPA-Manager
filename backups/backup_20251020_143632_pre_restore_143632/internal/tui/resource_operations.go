package tui

import (
	"fmt"
	"strings"
	"time"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/kubernetes"

	tea "github.com/charmbracelet/bubbletea"
)

// applyResourceChange aplica mudanças em um recurso individual
func (a *App) applyResourceChange(resource *models.ClusterResource) tea.Cmd {
	return func() tea.Msg {
		clusterName := a.model.SelectedCluster.Name
		
		// Obter o client do Kubernetes para este cluster
		clientset, err := a.kubeManager.GetClient(clusterName)
		if err != nil {
			return resourceChangeAppliedMsg{
				resource: resource,
				err:      fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err),
			}
		}
		
		client := kubernetes.NewClient(clientset, clusterName)
		
		// Aplicar mudanças
		err = client.ApplyResourceChanges(resource)
		if err != nil {
			return resourceChangeAppliedMsg{
				resource: resource,
				err:      fmt.Errorf("failed to apply resource changes: %w", err),
			}
		}
		
		// Marcar como aplicado
		resource.Modified = false
		resource.LastUpdated = time.Now()
		
		return resourceChangeAppliedMsg{
			resource: resource,
			err:      nil,
		}
	}
}

// applyAllResourceChanges aplica mudanças em todos os recursos modificados
func (a *App) applyAllResourceChanges() tea.Cmd {
	return func() tea.Msg {
		clusterName := a.model.SelectedCluster.Name
		successCount := 0
		
		// Obter o client do Kubernetes para este cluster
		clientset, err := a.kubeManager.GetClient(clusterName)
		if err != nil {
			return resourcesBatchAppliedMsg{
				successCount: 0,
				err:         fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err),
			}
		}
		
		client := kubernetes.NewClient(clientset, clusterName)
		
		// Aplicar mudanças em todos os recursos modificados
		for i := range a.model.ClusterResources {
			resource := &a.model.ClusterResources[i]
			if resource.Modified {
				err := client.ApplyResourceChanges(resource)
				if err == nil {
					resource.Modified = false
					resource.LastUpdated = time.Now()
					successCount++
				}
			}
		}
		
		return resourcesBatchAppliedMsg{
			successCount: successCount,
			err:         nil,
		}
	}
}

// applyPrometheusStack aplica mudanças em todo o stack Prometheus
func (a *App) applyPrometheusStack() tea.Cmd {
	return func() tea.Msg {
		clusterName := a.model.SelectedCluster.Name
		successCount := 0
		
		// Obter o client do Kubernetes para este cluster
		clientset, err := a.kubeManager.GetClient(clusterName)
		if err != nil {
			return prometheusStackAppliedMsg{
				successCount: 0,
				err:         fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err),
			}
		}
		
		client := kubernetes.NewClient(clientset, clusterName)
		
		// Aplicar mudanças apenas nos recursos Prometheus selecionados
		for i := range a.model.SelectedResources {
			resource := &a.model.SelectedResources[i]
			if resource.Modified {
				err := client.ApplyResourceChanges(resource)
				if err == nil {
					resource.Modified = false
					resource.LastUpdated = time.Now()
					successCount++
					
					// Atualizar também na lista principal
					for j := range a.model.ClusterResources {
						if a.model.ClusterResources[j].Name == resource.Name &&
							a.model.ClusterResources[j].Namespace == resource.Namespace {
							a.model.ClusterResources[j] = *resource
							break
						}
					}
				}
			}
		}
		
		return prometheusStackAppliedMsg{
			successCount: successCount,
			err:         nil,
		}
	}
}

// saveResourceSession salva sessão de recursos
func (a *App) saveResourceSession() (tea.Model, tea.Cmd) {
	// Por enquanto, usar o sistema de sessão existente
	// TODO: Implementar salvamento específico de recursos
	a.model.Error = "Salvamento de sessão de recursos não implementado ainda"
	return a, nil
}

// savePrometheusSession salva sessão específica do Prometheus
func (a *App) savePrometheusSession() (tea.Model, tea.Cmd) {
	// Por enquanto, usar o sistema de sessão existente
	// TODO: Implementar salvamento específico do Prometheus
	a.model.Error = "Salvamento de sessão Prometheus não implementado ainda"
	return a, nil
}

// applyPrometheusPreset aplica preset de configuração do Prometheus
func (a *App) applyPrometheusPreset(preset string) {
	// Definir presets de configuração
	presets := map[string]map[string]map[string]string{
		"small": {
			"prometheus-server": {
				"cpu":    "500m",
				"memory": "1Gi",
			},
			"grafana": {
				"cpu":    "200m",
				"memory": "512Mi",
			},
			"alertmanager": {
				"cpu":    "100m",
				"memory": "256Mi",
			},
		},
		"medium": {
			"prometheus-server": {
				"cpu":    "1000m",
				"memory": "4Gi",
			},
			"grafana": {
				"cpu":    "500m",
				"memory": "1Gi",
			},
			"alertmanager": {
				"cpu":    "200m",
				"memory": "512Mi",
			},
		},
		"large": {
			"prometheus-server": {
				"cpu":    "2000m",
				"memory": "8Gi",
			},
			"grafana": {
				"cpu":    "1000m",
				"memory": "2Gi",
			},
			"alertmanager": {
				"cpu":    "500m",
				"memory": "1Gi",
			},
		},
	}
	
	presetConfig, exists := presets[preset]
	if !exists {
		a.model.Error = fmt.Sprintf("Preset '%s' não encontrado", preset)
		return
	}
	
	// Aplicar preset nos recursos
	appliedCount := 0
	for i := range a.model.ClusterResources {
		resource := &a.model.ClusterResources[i]
		component := strings.ToLower(resource.Component)
		
		if config, hasConfig := presetConfig[component]; hasConfig {
			if cpu, hasCPU := config["cpu"]; hasCPU {
				resource.TargetCPURequest = cpu
				resource.TargetCPULimit = cpu // Set same value for both request and limit
			}
			if memory, hasMemory := config["memory"]; hasMemory {
				resource.TargetMemoryRequest = memory
				resource.TargetMemoryLimit = memory // Set same value for both request and limit
			}
			resource.Modified = true
			resource.Selected = true
			appliedCount++
		}
	}
	
	// Atualizar lista de selecionados
	a.updateSelectedResources()
	
	if appliedCount > 0 {
		a.model.ResourcePresetConfig = preset
		a.model.SuccessMsg = fmt.Sprintf("✅ Preset '%s' aplicado em %d recursos", preset, appliedCount)
	} else {
		a.model.Error = fmt.Sprintf("Nenhum recurso compatível encontrado para preset '%s'", preset)
	}
}

// Mensagens para as operações de recursos

// resourceChangeAppliedMsg resultado da aplicação de mudança em recurso individual
type resourceChangeAppliedMsg struct {
	resource *models.ClusterResource
	err      error
}

// resourcesBatchAppliedMsg resultado da aplicação em lote de recursos
type resourcesBatchAppliedMsg struct {
	successCount int
	err          error
}

// prometheusStackAppliedMsg resultado da aplicação do stack Prometheus
type prometheusStackAppliedMsg struct {
	successCount int
	err          error
}

// Handlers das mensagens de operações de recursos (adicionar ao Update do app.go)

func (a *App) handleResourceChangeApplied(msg resourceChangeAppliedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.model.Error = fmt.Sprintf("Failed to apply resource change: %v", msg.err)
		return a, nil
	}
	
	// Atualizar recurso na lista principal
	for i := range a.model.ClusterResources {
		if a.model.ClusterResources[i].Name == msg.resource.Name &&
			a.model.ClusterResources[i].Namespace == msg.resource.Namespace {
			a.model.ClusterResources[i] = *msg.resource
			break
		}
	}
	
	a.model.SuccessMsg = fmt.Sprintf("✅ Resource %s updated successfully", msg.resource.Name)
	return a, nil
}

func (a *App) handleResourcesBatchApplied(msg resourcesBatchAppliedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.model.Error = fmt.Sprintf("Failed to apply resource changes: %v", msg.err)
		return a, nil
	}
	
	a.model.SuccessMsg = fmt.Sprintf("✅ %d resources updated successfully", msg.successCount)
	return a, nil
}

func (a *App) handlePrometheusStackApplied(msg prometheusStackAppliedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.model.Error = fmt.Sprintf("Failed to apply Prometheus stack: %v", msg.err)
		return a, nil
	}
	
	a.model.SuccessMsg = fmt.Sprintf("✅ Prometheus stack updated: %d components", msg.successCount)
	return a, nil
}