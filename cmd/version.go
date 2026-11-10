package cmd

import (
	"fmt"
	"strings"

	"k8s-hpa-manager/internal/buildinfo"
	"k8s-hpa-manager/internal/updater"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Exibir versão da aplicação",
	Long:  `Exibe a versão atual do k8s-hpa-manager e verifica se há updates disponíveis.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("k8s-hpa-manager versão %s\n", updater.Version)
		bi := buildinfo.Get()
		if bi.CommitShort != "" {
			dirty := ""
			if bi.Dirty {
				dirty = " (com mudanças sem commit)"
			}
			fmt.Printf("build: commit %s%s", bi.CommitShort, dirty)
			if bi.CommitTime != "" {
				fmt.Printf(" de %s", bi.CommitTime)
			}
			fmt.Println()
		}
		if bi.BuiltAt != "" {
			fmt.Printf("binário gerado em %s\n", bi.BuiltAt)
		}
		fmt.Println()

		// Se versão é "dev", não verificar updates
		if updater.Version == "dev" {
			fmt.Println("ℹ️  Versão de desenvolvimento - verificação de updates desabilitada")
			return nil
		}

		// Verificar updates
		fmt.Println("🔍 Verificando updates...")
		info, err := updater.CheckForUpdates()
		if err != nil {
			// Erro ao verificar - não falhar, apenas informar
			fmt.Printf("⚠️  Não foi possível verificar updates: %v\n", err)
			return nil
		}

		if info.Available {
			fmt.Printf("🆕 Nova versão disponível: %s → %s\n", info.CurrentVersion, info.LatestVersion)
			fmt.Printf("📦 Download: %s\n", info.ReleaseURL)

			// Exibir release notes se disponíveis (primeiras 5 linhas)
			if info.ReleaseNotes != "" {
				lines := strings.Split(info.ReleaseNotes, "\n")
				maxLines := 5
				if len(lines) < maxLines {
					maxLines = len(lines)
				}

				fmt.Printf("\n📝 Release Notes (preview):\n")
				for i := 0; i < maxLines; i++ {
					fmt.Printf("   %s\n", lines[i])
				}
				if len(lines) > maxLines {
					fmt.Printf("   ... (ver mais em %s)\n", info.ReleaseURL)
				}
			}
		} else {
			fmt.Println("✅ Você está usando a versão mais recente!")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
