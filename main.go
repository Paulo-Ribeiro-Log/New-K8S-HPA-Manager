package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"k8s-hpa-manager/cmd"
)

func main() {
	// Interceptar --auto-update ANTES do Cobra para permitir passar argumentos ao script
	for i, arg := range os.Args {
		if arg == "--auto-update" {
			scriptPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "scripts", "auto-update.sh")

			// Verificar se o script existe
			if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "❌ Script auto-update.sh não encontrado em: %s\n", scriptPath)
				fmt.Fprintf(os.Stderr, "💡 Execute a instalação completa via install-from-github.sh\n")
				os.Exit(1)
			}

			// Passar todos os argumentos APÓS --auto-update para o script
			var scriptArgs []string
			if i+1 < len(os.Args) {
				scriptArgs = os.Args[i+1:]
			}

			// Executar o script com os argumentos
			updateCmd := exec.Command(scriptPath, scriptArgs...)
			updateCmd.Stdout = os.Stdout
			updateCmd.Stderr = os.Stderr
			updateCmd.Stdin = os.Stdin

			if err := updateCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Erro ao executar auto-update: %v\n", err)
				os.Exit(1)
			}

			os.Exit(0)
		}
	}

	// Executar Cobra normalmente
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
