package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"k8s-hpa-manager/internal/displayenv"
)

var (
	diagnoseDisplayJSON bool
	diagnoseDisplayOpen string
)

var diagnoseDisplayCmd = &cobra.Command{
	Use:   "diagnose-display",
	Short: "Testa se esta máquina tem modo gráfico (WSLg) e se o navegador do Windows pode ser usado",
	Long: `Verifica se há um display que atende (WSLg no WSL 2, X11 ou Wayland no Linux) e, no WSL sem
modo gráfico, se o navegador padrão do Windows pode abrir os links da ferramenta.

Mostra o que funciona neste ambiente: abrir links (a interface, OAuth, login do az), o robô do Teams
e o login do ServiceNow. Só leitura; a única ação visível é a de --open.

Exemplos:
  new-k8s-hpa diagnose-display
  new-k8s-hpa diagnose-display --json
  new-k8s-hpa diagnose-display --open http://localhost:8080   # abre de verdade, para conferir

K8S_HPA_BROWSER=windows|linux força onde os links abrem (padrão: o Linux com display, o Windows sem).`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		r := displayenv.Detect(ctx, displayenv.DefaultProbe())

		if diagnoseDisplayJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(r)
		} else {
			printDisplayReport(r)
		}

		if diagnoseDisplayOpen != "" {
			via, err := displayenv.NewOpener().OpenURL(ctx, diagnoseDisplayOpen)
			if err != nil {
				fmt.Printf("\n✗ Não abriu: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("\n✓ Pedido de abertura enviado por: %s\n  Confira se %s apareceu no navegador.\n", via, diagnoseDisplayOpen)
		}
	},
}

func yesNo(b bool) string {
	if b {
		return "sim"
	}
	return "não"
}

func printDisplayReport(r displayenv.Report) {
	fmt.Println("=== Ambiente gráfico e navegador ===")
	fmt.Println()
	fmt.Printf("Sistema:            %s", r.OS)
	if r.IsWSL {
		fmt.Printf(" (WSL %d, distro %s)", r.WSLVersion, r.Distro)
	}
	fmt.Println()
	if r.IsWSL {
		fmt.Printf("Interop com Windows: %s\n", yesNo(r.Interop))
		fmt.Printf("WSLg instalado:      %s (/mnt/wslg)\n", yesNo(r.WSLg.Present))
	}
	fmt.Printf("Display que atende:  %s", yesNo(r.GraphicalDisplay))
	if r.WSLg.DisplayEnv != "" {
		fmt.Printf("  [DISPLAY=%s]", r.WSLg.DisplayEnv)
	}
	if r.WSLg.WaylandEnv != "" {
		fmt.Printf("  [WAYLAND_DISPLAY=%s]", r.WSLg.WaylandEnv)
	}
	fmt.Println()
	fmt.Printf("Chrome/Chromium:     %s\n", orDash(r.ChromePath))
	fmt.Printf("Xvfb (invisível):    %s   Docker: %s   xdg-open: %s\n", yesNo(r.Xvfb), yesNo(r.Docker), yesNo(r.XdgOpen))
	if r.IsWSL && r.Interop {
		fmt.Printf("Navegador do Windows: %s (abre por: %v)\n", orDash(r.WindowsDefaultBrowser), r.WindowsOpeners)
	}
	fmt.Println()
	fmt.Printf("Modo: %s\n  %s\n\n", r.Mode, r.Summary)
	fmt.Println("O que funciona:")
	for _, c := range r.Capabilities {
		mark := "✗"
		if c.OK {
			mark = "✓"
		}
		fmt.Printf("  %s %s", mark, c.Label)
		if c.Via != "" {
			fmt.Printf(" — %s", c.Via)
		}
		fmt.Println()
		if c.Detail != "" {
			fmt.Printf("      %s\n", c.Detail)
		}
	}
	if len(r.Notes) > 0 {
		fmt.Println("\nObservações:")
		for _, n := range r.Notes {
			fmt.Printf("  • %s\n", n)
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func init() {
	diagnoseDisplayCmd.Flags().BoolVar(&diagnoseDisplayJSON, "json", false, "saída em JSON")
	diagnoseDisplayCmd.Flags().StringVar(&diagnoseDisplayOpen, "open", "", "abre esta URL (http/https) pelo caminho que a ferramenta usaria, para conferir")
	rootCmd.AddCommand(diagnoseDisplayCmd)
}
