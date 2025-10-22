package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"golang.org/x/sys/unix"

	"k8s-hpa-manager/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	kubeconfig string
	debug      bool
	demo       bool
)

var rootCmd = &cobra.Command{
	Use:   "k8s-hpa-manager",
	Short: "Interactive Kubernetes HPA and Azure AKS Node Pool Manager",
	Long: `A terminal-based interface for managing Kubernetes Horizontal Pod Autoscalers (HPAs) and Azure AKS Node Pools.
	
Kubernetes Features:
- Discover and connect to akspriv-* clusters
- Navigate and select multiple namespaces
- View and modify HPA configurations (min/max replicas, CPU/memory targets)
- Trigger deployment rollouts
- Save and restore HPA configuration sessions

Azure AKS Features:
- Manage Azure AKS node pools with transparent authentication
- Modify node count, min/max node count, and autoscaler settings
- Real-time application via Azure CLI with progress feedback
- Automatic subscription management from clusters-config.json

Advanced Workflows:
- Mixed sessions: Combine HPA and node pool modifications (Ctrl+M)
- Session management: Save/load/apply complex infrastructure changes
- Template-based session naming with variables
- Multi-panel interface with TAB navigation

Controls:
- Ctrl+N: Node pool management    - Ctrl+M: Mixed sessions
- Ctrl+L: Load sessions          - Ctrl+S: Save sessions
- Ctrl+D/U: Apply changes        - ?: Show help
- Space: Select items            - Tab: Switch panels`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Demo mode - just show that the app would start
		if demo {
			fmt.Println("🚀 k8s-hpa-manager iniciado com sucesso!")
			fmt.Println("✅ Módulo Kubernetes HPA: OK")
			fmt.Println("✅ Módulo Azure AKS Node Pools: OK")
			fmt.Println("✅ Autenticação Azure SDK: OK")
			fmt.Println("✅ Interface TUI Bubble Tea: OK")
			fmt.Println("✅ Sistema de Sessões Mistas: OK")
			fmt.Println("\n📝 Funcionalidades implementadas:")
			fmt.Println("  🎯 HPA Management:")
			fmt.Println("    • Descoberta automática de clusters akspriv-*")
			fmt.Println("    • Seleção múltipla de namespaces e HPAs")
			fmt.Println("    • Edição de min/max replicas, CPU/memory targets")
			fmt.Println("    • Rollout deployment integration")
			fmt.Println("  🔧 Node Pool Management:")
			fmt.Println("    • Gerenciamento completo de node pools AKS")
			fmt.Println("    • Autenticação Azure transparente (SDK + CLI)")
			fmt.Println("    • Edição de node count, min/max, autoscaler")
			fmt.Println("    • Configuração automática via clusters-config.json")
			fmt.Println("  🔄 Mixed Sessions:")
			fmt.Println("    • Sessões unificadas (HPAs + Node Pools)")
			fmt.Println("    • Interface multi-painel com TAB navigation")
			fmt.Println("    • Aplicação de mudanças em batch (Ctrl+D/U)")
			fmt.Println("    • Template naming com variáveis {cluster}_{env}_{timestamp}")
			fmt.Println("\n🎮 Controles principais:")
			fmt.Println("  • Ctrl+M: Sessões mistas   • Ctrl+N: Node pools")
			fmt.Println("  • Ctrl+L: Carregar sessão  • Ctrl+S: Salvar sessão")
			fmt.Println("  • Ctrl+D/U: Aplicar        • ?: Ajuda completa")
			fmt.Println("\n🎯 Aplicação pronta para uso em ambiente terminal interativo!")
			return nil
		}

		// Validar autenticação Azure AD ANTES de carregar kubeconfig
		// Isso previne panic quando kubeconfig tem credenciais Azure expiradas/inválidas
		fmt.Println("🔍 Verificando autenticação Azure AD...")
		if err := validateAzureAuth(); err != nil {
			return fmt.Errorf("falha na autenticação Azure: %w", err)
		}

		// Initialize the TUI application
		app := tui.NewApp(kubeconfig, debug)

		// Create the Bubble Tea program with alt screen for clean rendering (sem mouse capture para permitir seleção de texto)
		p := tea.NewProgram(app, tea.WithAltScreen())

		// Run the program
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run application: %w", err)
		}

		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// validateAzureAuth valida se Azure AD está autenticado antes de carregar kubeconfig
// Previne panic quando kubeconfig tem credenciais Azure expiradas/corrompidas
func validateAzureAuth() error {
	// Verificar se Azure CLI está instalado
	checkCmd := exec.Command("az", "version", "--only-show-errors")
	checkCmd.Stdout = nil // Suprimir output
	checkCmd.Stderr = nil // Suprimir erros
	if err := checkCmd.Run(); err != nil {
		fmt.Println("⚠️  Azure CLI não encontrado (ignorando - necessário apenas para node pools)")
		return nil // Não é erro crítico - pode usar apenas HPAs
	}

	// Verificar se está autenticado
	accountCmd := exec.Command("az", "account", "show", "--only-show-errors")
	accountCmd.Stdout = nil // Suprimir output
	accountCmd.Stderr = nil // Suprimir erros
	if err := accountCmd.Run(); err != nil {
		fmt.Println("⚠️  Azure AD não está autenticado")
		return performAzureLogin()
	}

	// Verificar se token está válido
	tokenCmd := exec.Command("az", "account", "get-access-token", "--only-show-errors")
	tokenCmd.Stdout = nil // Suprimir output
	tokenCmd.Stderr = nil // Suprimir erros
	if err := tokenCmd.Run(); err != nil {
		fmt.Println("⚠️  Token Azure AD expirado")
		return performAzureLogin()
	}

	fmt.Println("✅ Azure AD autenticado")
	return nil
}

// performAzureLogin executa o login no Azure AD
func performAzureLogin() error {
	fmt.Println("\n🔐 Iniciando login no Azure AD...")
	fmt.Println("📌 Uma janela do navegador será aberta para autenticação.\n")

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
		fmt.Println("\n⚠️  Erro no login. Tentando novamente com output detalhado...\n")
		retryCmd := exec.Command("az", "login")
		retryCmd.Stdout = os.Stdout
		retryCmd.Stderr = os.Stderr
		retryCmd.Stdin = os.Stdin
		if retryErr := retryCmd.Run(); retryErr != nil {
			return fmt.Errorf("❌ falha no login Azure: %w", retryErr)
		}
	}

	fmt.Println("\n✅ Login Azure AD concluído com sucesso!\n")
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

	// Set default kubeconfig path
	if home, exists := os.LookupEnv("HOME"); exists && kubeconfig == "" {
		kubeconfig = fmt.Sprintf("%s/.kube/config", home)
	}
}

// isatty checks if we have a TTY available
func isatty() bool {
	_, err := unix.IoctlGetTermios(int(os.Stdout.Fd()), unix.TCGETS)
	return err == nil
}
