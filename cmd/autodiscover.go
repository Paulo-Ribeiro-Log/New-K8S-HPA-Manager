package cmd

import (
	"fmt"
	"os"
	"sync"

	"k8s-hpa-manager/internal/config"

	"github.com/spf13/cobra"
)

var autodiscoverCmd = &cobra.Command{
	Use:   "autodiscover",
	Short: "Auto-descobre configurações de todos os clusters AKS, EKS e GKE",
	Long: `Descobre automaticamente configurações de clusters AKS (Azure), EKS (AWS) e GKE (GCP)
a partir do kubeconfig local, rodando os três em paralelo.

AKS: extrai resource group + subscription via Azure CLI
     → salva em ~/.k8s-hpa-manager/clusters-config.json

EKS: varre perfis AWS × regiões via AWS CLI
     → salva em ~/.k8s-hpa-manager/eks-clusters-config.json

GKE: parseia contexts gke_* do kubeconfig + gcloud container clusters list
     → salva em ~/.k8s-hpa-manager/gke-clusters-config.json

Útil para configuração inicial ou após adicionar novos clusters ao kubeconfig.`,
	Example: `  # Descobrir todos os clusters (AKS + EKS + GKE) automaticamente
  ./build/new-k8s-hpa autodiscover

  # Com kubeconfig customizado
  ./build/new-k8s-hpa --kubeconfig /path/to/config autodiscover`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 K8s HPA Manager - Auto-Descoberta de Clusters (AKS + EKS + GKE)")
		fmt.Println("====================================================================")
		fmt.Println()

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

		log := func(msg string) { fmt.Println(msg) }

		var (
			aksConfigs []config.ClusterConfig
			eksConfigs []config.EKSClusterConfig
			gkeConfigs []config.GKEClusterConfig
			aksErrors  []error
			eksErrors  []error
			gkeErrors  []error
			wg         sync.WaitGroup
			mu         sync.Mutex
		)

		wg.Add(3)

		go func() {
			defer wg.Done()
			cfgs, errs := manager.AutoDiscoverAllClusters(log)
			mu.Lock()
			aksConfigs, aksErrors = cfgs, errs
			mu.Unlock()
		}()

		go func() {
			defer wg.Done()
			cfgs, errs := manager.AutoDiscoverEKSClusters(log)
			mu.Lock()
			eksConfigs, eksErrors = cfgs, errs
			mu.Unlock()
		}()

		go func() {
			defer wg.Done()
			cfgs, errs := manager.AutoDiscoverGKEClusters(log)
			mu.Lock()
			gkeConfigs, gkeErrors = cfgs, errs
			mu.Unlock()
		}()

		wg.Wait()

		fmt.Println()

		if len(aksConfigs) > 0 {
			if err := manager.SaveClusterConfigs(aksConfigs, log); err != nil {
				fmt.Printf("❌ Erro ao salvar clusters AKS: %v\n", err)
			}
		}
		if len(eksConfigs) > 0 {
			if err := manager.SaveEKSClusterConfigs(eksConfigs, log); err != nil {
				fmt.Printf("❌ Erro ao salvar clusters EKS: %v\n", err)
			}
		}
		if len(gkeConfigs) > 0 {
			if err := manager.SaveGKEClusterConfigs(gkeConfigs, log); err != nil {
				fmt.Printf("❌ Erro ao salvar clusters GKE: %v\n", err)
			}
		}

		allErrors := append(append(aksErrors, eksErrors...), gkeErrors...)
		if len(allErrors) > 0 {
			fmt.Println("\n⚠️  Erros encontrados:")
			for _, e := range allErrors {
				fmt.Printf("  • %v\n", e)
			}
		}

		fmt.Println()
		fmt.Printf("✅ AKS: %d cluster(s) configurado(s)", len(aksConfigs))
		if len(aksErrors) > 0 {
			fmt.Printf(" | ⚠️  %d erro(s)", len(aksErrors))
		}
		fmt.Println()
		fmt.Printf("✅ EKS: %d cluster(s) configurado(s)", len(eksConfigs))
		if len(eksErrors) > 0 {
			fmt.Printf(" | ⚠️  %d erro(s)", len(eksErrors))
		}
		fmt.Println()
		fmt.Printf("✅ GKE: %d cluster(s) configurado(s)", len(gkeConfigs))
		if len(gkeErrors) > 0 {
			fmt.Printf(" | ⚠️  %d erro(s)", len(gkeErrors))
		}
		fmt.Println()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(autodiscoverCmd)
}
