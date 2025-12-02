package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

var (
	kubeconfig   string
	debug        bool
	demo         bool
	checkUpdates bool
	autoUpdate   bool
)

var rootCmd = &cobra.Command{
	Use:   "new-k8s-hpa",
	Short: "Kubernetes HPA and Azure AKS Node Pool Manager - Web Interface",
	Long: `A web-based interface for managing Kubernetes Horizontal Pod Autoscalers (HPAs) and Azure AKS Node Pools.

Features:
- Modern React/TypeScript web interface
- Real-time monitoring with Prometheus integration
- HPAs and Node Pools management
- ConfigMaps and Secrets editor
- CronJobs management
- Session system for saving/loading configurations
- Alerts and notifications system

Usage:
  new-k8s-hpa web              Start web server (default: port 8080)
  new-k8s-hpa web -f           Start in foreground mode
  new-k8s-hpa version          Check version and updates
  new-k8s-hpa autodiscover     Auto-discover clusters

Documentation:
  https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TUI mode has been removed - redirect to web
		fmt.Println("❌ TUI mode has been removed in this version")
		fmt.Println("")
		fmt.Println("✅ Please use the web interface instead:")
		fmt.Println("   new-k8s-hpa web              # Start web server")
		fmt.Println("   new-k8s-hpa web -f           # Start in foreground")
		fmt.Println("")
		fmt.Println("📖 Documentation: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager")
		fmt.Println("")
		return fmt.Errorf("TUI mode removed - use 'new-k8s-hpa web' instead")
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// validateAzureAuth valida se Azure AD está autenticado antes de carregar kubeconfig
// Previne panic quando kubeconfig tem credenciais Azure expiradas/corrompidas
func validateAzureAuth() error {
	// Criar contexto com timeout de 5 segundos
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verificar se Azure CLI está instalado
	checkCmd := exec.CommandContext(ctx, "az", "version", "--only-show-errors")
	checkCmd.Stdout = nil
	checkCmd.Stderr = nil
	if err := checkCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Println("⚠️  Timeout ao verificar Azure CLI (ignorando)")
			return nil
		}
		fmt.Println("⚠️  Azure CLI não encontrado (ignorando - necessário apenas para node pools)")
		return nil
	}

	// Verificar se está autenticado
	accountCmd := exec.CommandContext(ctx, "az", "account", "show", "--only-show-errors")
	accountCmd.Stdout = nil
	accountCmd.Stderr = nil
	if err := accountCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Println("⚠️  Timeout ao verificar autenticação Azure (ignorando)")
			return nil
		}
		fmt.Println("⚠️  Azure AD não está autenticado")
		return performAzureLogin()
	}

	// Verificar se token está válido
	tokenCmd := exec.CommandContext(ctx, "az", "account", "get-access-token", "--only-show-errors")
	tokenCmd.Stdout = nil
	tokenCmd.Stderr = nil
	if err := tokenCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Println("⚠️  Timeout ao verificar token Azure (ignorando)")
			return nil
		}
		fmt.Println("⚠️  Token Azure AD expirado")
		return performAzureLogin()
	}

	fmt.Println("✅ Azure AD autenticado")
	return nil
}

// performAzureLogin executa o login no Azure AD
func performAzureLogin() error {
	fmt.Println("\n🔐 Iniciando login no Azure AD...")
	fmt.Println("📌 Uma janela do navegador será aberta para autenticação.")

	// Login simples sem forçar tenant/subscription específico
	// Isso permite que o Azure use as subscriptions disponíveis para o usuário
	loginCmd := exec.Command("az", "login", "--only-show-errors")
	loginCmd.Stdin = os.Stdin

	// Redirecionar stdout/stderr para /dev/null para silenciar output
	loginCmd.Stdout = nil
	loginCmd.Stderr = nil

	err := loginCmd.Run()
	if err != nil {
		// Em caso de erro, rodar novamente com output visível para debug
		fmt.Println("\n⚠️  Erro no login. Tentando novamente com output detalhado...")
		retryCmd := exec.Command("az", "login")
		retryCmd.Stdout = os.Stdout
		retryCmd.Stderr = os.Stderr
		retryCmd.Stdin = os.Stdin
		if retryErr := retryCmd.Run(); retryErr != nil {
			return fmt.Errorf("❌ falha no login Azure: %w", retryErr)
		}
	}

	fmt.Println("\n✅ Login Azure AD concluído com sucesso!")
	return nil
}


func init() {
	// Define flags
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "",
		"Path to kubeconfig file (default: $HOME/.kube/config)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false,
		"Enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&demo, "demo", false,
		"Run in demo mode (show implementation status)")
	rootCmd.PersistentFlags().BoolVar(&checkUpdates, "check-updates", true,
		"Check for updates on startup (default: true)")
	rootCmd.PersistentFlags().BoolVar(&autoUpdate, "auto-update", false,
		"Run auto-update script (supports --yes, --dry-run, --check, --force)")

	// Set default kubeconfig path
	if home, exists := os.LookupEnv("HOME"); exists && kubeconfig == "" {
		kubeconfig = fmt.Sprintf("%s/.kube/config", home)
	}
}
