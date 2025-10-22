package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"k8s-hpa-manager/internal/web"

	"github.com/spf13/cobra"
)

var (
	webPort   int
	noBrowser bool
)

// openBrowser opens the default browser at the given URL
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start k8s-hpa-manager in web mode",
	Long: `Start k8s-hpa-manager as a web server with HTTP API and browser interface.

This is a POC (Proof of Concept) version of the web interface.

Example usage:
  # Start with default settings
  k8s-hpa-manager web

  # Start on custom port
  k8s-hpa-manager web --port 8080

  # With custom token
  K8S_HPA_WEB_TOKEN=my-secret-token k8s-hpa-manager web

  # With debug logging
  k8s-hpa-manager web --debug

Authentication:
  Set K8S_HPA_WEB_TOKEN environment variable to define your access token.
  If not set, a default token 'poc-token-123' will be used.

  All API requests must include the header:
    Authorization: Bearer <your-token>

API Endpoints:
  GET  /health                          - Health check (no auth)
  GET  /api/v1/clusters                 - List clusters
  GET  /api/v1/clusters/:name/test      - Test cluster connection
  GET  /api/v1/namespaces?cluster=X     - List namespaces
  GET  /api/v1/hpas?cluster=X&namespace=Y - List HPAs
  PUT  /api/v1/hpas/:cluster/:ns/:name  - Update HPA
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Criar servidor web
		server, err := web.NewServer(kubeconfig, webPort, debug)
		if err != nil {
			return fmt.Errorf("failed to create web server: %w", err)
		}

		// Abrir browser automaticamente (após um pequeno delay para garantir que o servidor iniciou)
		if !noBrowser {
			go func() {
				time.Sleep(1 * time.Second)
				url := fmt.Sprintf("http://localhost:%d", webPort)
				if err := openBrowser(url); err != nil {
					fmt.Printf("Could not open browser automatically: %v\n", err)
					fmt.Printf("Please open manually: %s\n", url)
				} else {
					fmt.Printf("Opening browser at %s\n", url)
				}
			}()
		}

		// Iniciar servidor
		return server.Start()
	},
}

func init() {
	// Adicionar comando ao root
	rootCmd.AddCommand(webCmd)

	// Flags específicas do web
	webCmd.Flags().IntVar(&webPort, "port", 8080, "Port for web server")
	webCmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically")
}
