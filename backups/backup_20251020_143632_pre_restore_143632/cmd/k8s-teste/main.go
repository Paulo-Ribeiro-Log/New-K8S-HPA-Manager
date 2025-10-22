package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	kubeconfig string
	debug      bool
)

var testCmd = &cobra.Command{
	Use:   "k8s-teste",
	Short: "K8s HPA Manager - Teste de Layout Unificado",
	Long: `Versão de teste do K8s HPA Manager com layout unificado.

Esta versão implementa:
- Container único 200x50 com moldura
- Header dinâmico baseado na função atual
- Layout limpo usando Bubble Tea + Lipgloss
- Navegação básica para testes visuais

Esta é uma versão APENAS para testes visuais do novo layout.
A lógica completa permanece no aplicativo principal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 K8s HPA Manager - Layout de Teste")
		fmt.Println("📐 Container: 200x50 com moldura unificada")
		fmt.Println("🎨 Header dinâmico baseado no estado atual")
		fmt.Println("⚠️  Versão APENAS para teste visual")

		// Initialize the simple demo
		app := NewSimpleDemo()

		// Create Bubble Tea program (sem mouse capture)
		p := tea.NewProgram(app, tea.WithAltScreen())

		// Run the program
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run test application: %w", err)
		}

		fmt.Println("\n👋 Teste do layout concluído!")
		return nil
	},
}

func main() {
	// Set default kubeconfig path
	if home, exists := os.LookupEnv("HOME"); exists && kubeconfig == "" {
		kubeconfig = fmt.Sprintf("%s/.kube/config", home)
	}

	// Define flags
	testCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", kubeconfig,
		"Path to kubeconfig file")
	testCmd.PersistentFlags().BoolVar(&debug, "debug", false,
		"Enable debug logging to /tmp/k8s-teste-debug.log")

	if err := testCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}