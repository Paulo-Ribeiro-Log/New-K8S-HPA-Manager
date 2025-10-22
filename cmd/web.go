package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"k8s-hpa-manager/internal/web"

	"github.com/spf13/cobra"
)

var (
	webPort    int
	noBrowser  bool
	foreground bool
)

// runInBackground executes the web server as a background process
func runInBackground() error {
	// Get current executable path
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get current executable path: %w", err)
	}

	// Prepare arguments
	args := []string{"web", "--foreground", "--port", fmt.Sprintf("%d", webPort)}

	if noBrowser {
		args = append(args, "--no-browser")
	}

	if debug {
		args = append(args, "--debug")
	}

	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}

	// Start process in background
	cmd := exec.Command(executable, args...)

	// Create log file for background process output
	logFile := filepath.Join(os.TempDir(), fmt.Sprintf("k8s-hpa-manager-web-%d.log", time.Now().Unix()))
	outFile, err := os.Create(logFile)
	if err != nil {
		fmt.Printf("⚠️  Could not create log file: %v\n", err)
		fmt.Println("   Starting without logging...")
	} else {
		cmd.Stdout = outFile
		cmd.Stderr = outFile
		defer outFile.Close()
	}

	// Detach from parent process
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start background process: %w", err)
	}

	// Save PID before releasing
	pid := cmd.Process.Pid

	// Release the process
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("failed to release background process: %w", err)
	}

	fmt.Printf("✅ k8s-hpa-manager web server started in background (PID: %d)\n", pid)
	fmt.Printf("🌐 Access at: http://localhost:%d\n", webPort)

	if outFile != nil {
		fmt.Printf("📋 Logs: %s\n", logFile)
	}

	if !noBrowser {
		fmt.Println("🔗 Opening browser...")
		time.Sleep(2 * time.Second)
		url := fmt.Sprintf("http://localhost:%d", webPort)
		if err := openBrowser(url); err != nil {
			fmt.Printf("⚠️  Could not open browser automatically: %v\n", err)
			fmt.Printf("   Please open manually: %s\n", url)
		}
	}

	fmt.Println("\n💡 To stop the server:")
	fmt.Printf("   ps aux | grep k8s-hpa-manager\n")
	fmt.Printf("   kill %d\n", pid)

	return nil
}

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

By default, the server runs in BACKGROUND mode. Use --foreground/-f to run in the current terminal.

Example usage:
  # Start in background (default)
  k8s-hpa-manager web

  # Start in foreground
  k8s-hpa-manager web --foreground
  k8s-hpa-manager web -f

  # Start on custom port in foreground
  k8s-hpa-manager web --port 8080 -f

  # Background with custom token
  K8S_HPA_WEB_TOKEN=my-secret-token k8s-hpa-manager web

  # Foreground with debug logging
  k8s-hpa-manager web --debug -f

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
		// Se não for foreground, executar em background
		if !foreground {
			return runInBackground()
		}

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
	webCmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run server in foreground (default: background)")
}
