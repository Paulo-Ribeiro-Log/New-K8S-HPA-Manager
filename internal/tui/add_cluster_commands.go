package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// ClusterConfig representa a configuração de um cluster no arquivo JSON
type ClusterConfig struct {
	Name          string `json:"clusterName"`
	ResourceGroup string `json:"resourceGroup"`
	Subscription  string `json:"subscription"`
}

// saveNewCluster salva o novo cluster no arquivo clusters-config.json
func (a *App) saveNewCluster() tea.Cmd {
	return func() tea.Msg {
		// Criar estrutura do novo cluster
		newCluster := ClusterConfig{
			Name:          a.model.AddClusterFormFields["clusterName"],
			ResourceGroup: a.model.AddClusterFormFields["resourceGroup"],
			Subscription:  a.model.AddClusterFormFields["subscription"],
		}

		// Carregar arquivo existente ou criar novo
		configPath := "clusters-config.json"
		var clusters []ClusterConfig

		// Tentar ler arquivo existente
		if data, err := os.ReadFile(configPath); err == nil {
			json.Unmarshal(data, &clusters)
		}

		// Verificar se cluster já existe
		for _, existing := range clusters {
			if existing.Name == newCluster.Name {
				return clusterSaveResultMsg{
					success: false,
					error:   fmt.Sprintf("Cluster '%s' já existe no arquivo de configuração", newCluster.Name),
				}
			}
		}

		// Adicionar novo cluster
		clusters = append(clusters, newCluster)

		// Salvar arquivo
		data, err := json.MarshalIndent(clusters, "", "  ")
		if err != nil {
			return clusterSaveResultMsg{
				success: false,
				error:   fmt.Sprintf("Erro ao serializar configuração: %v", err),
			}
		}

		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return clusterSaveResultMsg{
				success: false,
				error:   fmt.Sprintf("Erro ao salvar arquivo: %v", err),
			}
		}

		// Copiar para diretório home se não existir
		homeConfigDir := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager")
		homeConfigPath := filepath.Join(homeConfigDir, "clusters-config.json")

		// Criar diretório se não existir
		if err := os.MkdirAll(homeConfigDir, 0755); err != nil {
			return clusterSaveResultMsg{
				success: false,
				error:   fmt.Sprintf("Erro ao criar diretório ~/.k8s-hpa-manager: %v", err),
			}
		}

		// Copiar arquivo para diretório home
		if err := os.WriteFile(homeConfigPath, data, 0644); err != nil {
			return clusterSaveResultMsg{
				success: false,
				error:   fmt.Sprintf("Erro ao copiar para ~/.k8s-hpa-manager: %v", err),
			}
		}

		return clusterSaveResultMsg{
			success: true,
			cluster: newCluster,
		}
	}
}

// clusterSaveResultMsg mensagem de resultado do salvamento
type clusterSaveResultMsg struct {
	success bool
	cluster ClusterConfig
	error   string
}