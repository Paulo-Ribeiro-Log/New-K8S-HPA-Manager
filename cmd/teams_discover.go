package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s-hpa-manager/internal/teams"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var teamsDiscoverCmd = &cobra.Command{
	Use:   "teams-discover",
	Short: "Descobre as APIs internas do Microsoft Teams interceptando requisições de rede",
	Long: `Lança o Chromium com a sessão existente (mesma do ServiceNow), navega para
teams.microsoft.com e intercepta todas as chamadas de rede relevantes via CDP.

O resultado é salvo em internal/teams/testdata/ e impresso no terminal.
Use para mapear as URLs reais da API interna do Teams antes de implementar
a extração de mensagens do MR.ViaBot.

Pré-requisitos:
  - Sessão Azure AD válida em ~/.k8s-hpa-manager/rod-session/
    (fazer login via: ./build/new-k8s-hpa web → Menu Perfil → Sessão ServiceNow → Fazer Login)

Durante a sessão:
  - Navegue até o chat do MR.ViaBot no Teams
  - As requisições serão capturadas automaticamente
  - Aguarde o timeout ou pressione Ctrl+C para finalizar`,

	Example: `  # Descoberta com 3 minutos de janela (padrão)
  ./build/new-k8s-hpa teams-discover

  # Janela de 5 minutos para navegar com calma
  ./build/new-k8s-hpa teams-discover --timeout 5m

  # Salvar em diretório customizado
  ./build/new-k8s-hpa teams-discover --output /tmp/teams-discovery`,

	RunE: func(cmd *cobra.Command, args []string) error {
		timeoutStr, _ := cmd.Flags().GetString("timeout")
		outputDir, _ := cmd.Flags().GetString("output")

		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("timeout inválido %q: use formato como 3m, 5m, 300s", timeoutStr)
		}

		// Diretório de sessão (mesmo do ServiceNow)
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		sessionDir := filepath.Join(home, ".k8s-hpa-manager", "rod-session")

		// Diretório de saída padrão
		if outputDir == "" {
			outputDir = "internal/teams/testdata"
		}

		// Verificar se sessão existe
		if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
			return fmt.Errorf(
				"sessão não encontrada em %s\n"+
					"Execute primeiro: ./build/new-k8s-hpa web → Menu Perfil → Sessão ServiceNow → Fazer Login",
				sessionDir,
			)
		}

		logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).
			With().Timestamp().Logger()

		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════════════════╗")
		fmt.Println("║       Teams Discovery — Fase 0 do Plano              ║")
		fmt.Println("╠══════════════════════════════════════════════════════╣")
		fmt.Printf("║  Sessão:  %-43s║\n", sessionDir)
		fmt.Printf("║  Saída:   %-43s║\n", outputDir)
		fmt.Printf("║  Timeout: %-43s║\n", timeout.String())
		fmt.Println("╠══════════════════════════════════════════════════════╣")
		fmt.Println("║  Durante a sessão:                                   ║")
		fmt.Println("║  1. O Chrome abrirá com teams.microsoft.com          ║")
		fmt.Println("║  2. Navegue até o chat do MR.ViaBot                  ║")
		fmt.Println("║  3. Aguarde o timeout — as APIs serão capturadas     ║")
		fmt.Println("╚══════════════════════════════════════════════════════╝")
		fmt.Println()

		result, err := teams.RunDiscovery(sessionDir, outputDir, &logger, timeout)
		if err != nil {
			return fmt.Errorf("erro na descoberta: %v", err)
		}

		fmt.Println()
		fmt.Printf("✅ Descoberta concluída — %d auth, %d chat, %d outras APIs capturadas\n",
			len(result.AuthRequests), len(result.ChatRequests), len(result.OtherAPIs))
		if result.SkypeToken != "" {
			fmt.Println("✅ X-Skypetoken encontrado e salvo!")
		} else {
			fmt.Println("⚠️  X-Skypetoken não encontrado — inspecione auth-requests.json")
		}
		fmt.Printf("📁 Arquivos salvos em: %s\n", outputDir)

		return nil
	},
}

func init() {
	teamsDiscoverCmd.Flags().String("timeout", "3m", "Tempo de janela para captura de requisições (ex: 3m, 5m, 300s)")
	teamsDiscoverCmd.Flags().String("output", "", "Diretório de saída (padrão: internal/teams/testdata)")
	rootCmd.AddCommand(teamsDiscoverCmd)
}
