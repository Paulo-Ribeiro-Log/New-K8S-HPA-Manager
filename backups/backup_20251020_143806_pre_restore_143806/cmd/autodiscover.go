package cmd

import (
	"fmt"
	"os"

	"k8s-hpa-manager/internal/config"

	"github.com/spf13/cobra"
)

var autodiscoverCmd = &cobra.Command{
	Use:   "autodiscover",
	Short: "Auto-descobre resource groups e subscriptions de todos os clusters",
	Long: `Descobre automaticamente resource groups e subscriptions para todos os clusters
no kubeconfig e salva em ~/.k8s-hpa-manager/clusters-config.json.

Esta funcionalidade:
1. Lê todos os clusters 'akspriv-*' do kubeconfig
2. Extrai o resource group do campo 'user' (formato: clusterAdmin_{RG}_{CLUSTER})
3. Descobre a subscription via Azure CLI
4. Salva ou atualiza clusters-config.json

Útil para:
- Configuração inicial de múltiplos clusters (26, 70+ clusters)
- Atualizar configurações após adicionar novos clusters ao kubeconfig
- Regenerar clusters-config.json após mudanças de subscriptions`,
	Example: `  # Descobrir todos os clusters automaticamente
  k8s-hpa-manager autodiscover

  # Com kubeconfig customizado
  k8s-hpa-manager --kubeconfig /path/to/config autodiscover`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 K8s HPA Manager - Auto-Descoberta de Clusters")
		fmt.Println("=" + "================================================")
		fmt.Println()

		// Validar Azure CLI
		fmt.Println("🔍 Validando Azure CLI...")
		if err := validateAzureAuth(); err != nil {
			return fmt.Errorf("Azure CLI não está disponível ou não autenticado: %w\nExecute: az login", err)
		}
		fmt.Println()

		// Criar KubeConfigManager
		kubeconfigPath := kubeconfig
		if kubeconfigPath == "" {
			if home, exists := os.LookupEnv("HOME"); exists {
				kubeconfigPath = fmt.Sprintf("%s/.kube/config", home)
			} else {
				return fmt.Errorf("não foi possível determinar o caminho do kubeconfig")
			}
		}

		fmt.Printf("📁 Kubeconfig: %s\n\n", kubeconfigPath)

		manager, err := config.NewKubeConfigManager(kubeconfigPath)
		if err != nil {
			return fmt.Errorf("erro ao carregar kubeconfig: %w", err)
		}

		// Auto-descobrir todos os clusters
		configs, errors := manager.AutoDiscoverAllClusters()

		// Salvar configurações
		if len(configs) > 0 {
			fmt.Println()
			if err := manager.SaveClusterConfigs(configs); err != nil {
				return fmt.Errorf("erro ao salvar configurações: %w", err)
			}
		}

		// Mostrar erros se houver
		if len(errors) > 0 {
			fmt.Println("\n⚠️  Erros encontrados:")
			for _, err := range errors {
				fmt.Printf("  • %v\n", err)
			}
		}

		// Resumo final
		fmt.Println("\n✅ Auto-descoberta concluída!")
		if len(configs) > 0 {
			fmt.Printf("✅ %d clusters configurados com sucesso\n", len(configs))
		}
		if len(errors) > 0 {
			fmt.Printf("⚠️  %d clusters com erros\n", len(errors))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(autodiscoverCmd)
}
