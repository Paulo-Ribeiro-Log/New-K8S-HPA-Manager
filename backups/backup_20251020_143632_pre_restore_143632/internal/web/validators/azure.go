package validators

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ValidateAzureAuth valida Azure CLI e faz login se necessário (igual ao TUI)
func ValidateAzureAuth() error {
	// Criar contexto com timeout de 5 segundos
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Verificar se Azure CLI está instalado
	checkCmd := exec.CommandContext(ctx, "az", "version", "--only-show-errors")
	checkCmd.Stdout = nil
	checkCmd.Stderr = nil
	if err := checkCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil // Timeout - ignorar
		}
		return fmt.Errorf("Azure CLI not installed")
	}

	// 2. Verificar se está autenticado
	accountCmd := exec.CommandContext(ctx, "az", "account", "show", "--only-show-errors")
	accountCmd.Stdout = nil
	accountCmd.Stderr = nil
	if err := accountCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil // Timeout - ignorar
		}
		// Não autenticado - fazer login
		return performAzureLogin()
	}

	// 3. Verificar se token está válido
	tokenCmd := exec.CommandContext(ctx, "az", "account", "get-access-token", "--only-show-errors")
	tokenCmd.Stdout = nil
	tokenCmd.Stderr = nil
	if err := tokenCmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil // Timeout - ignorar
		}
		// Token expirado - fazer login
		return performAzureLogin()
	}

	return nil
}

// performAzureLogin executa az login (igual ao TUI em cmd/root.go)
func performAzureLogin() error {
	fmt.Println("\n🔐 Iniciando login no Azure AD...")
	fmt.Println("📌 Uma janela do navegador será aberta para autenticação.")

	// Login simples sem forçar tenant/subscription (igual ao TUI)
	loginCmd := exec.Command("az", "login", "--only-show-errors")
	loginCmd.Stdin = os.Stdin

	// Silenciar output inicial
	loginCmd.Stdout = nil
	loginCmd.Stderr = nil

	err := loginCmd.Run()
	if err != nil {
		// Retry com output visível para debug
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

// ValidateVPNConnectivity verifica conectividade com Kubernetes (requer VPN)
func ValidateVPNConnectivity() error {
	// Buscar clusters para testar
	var prdContext, hlgContext string

	// Obter lista de contextos do kubeconfig
	cmd := exec.Command("kubectl", "config", "get-contexts", "-o", "name")
	output, err := cmd.Output()
	if err == nil {
		contexts := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, context := range contexts {
			if strings.Contains(context, "-prd") && prdContext == "" {
				prdContext = context
			}
			if strings.Contains(context, "-hlg") && hlgContext == "" {
				hlgContext = context
			}
		}
	}

	// Tentar contexto atual primeiro
	if err := testKubernetesConnectivity(""); err == nil {
		return nil
	}

	// Tentar produção
	if prdContext != "" {
		if err := testKubernetesConnectivity(prdContext); err == nil {
			return nil
		}
	}

	// Tentar homologação
	if hlgContext != "" {
		if err := testKubernetesConnectivity(hlgContext); err == nil {
			return nil
		}
	}

	return fmt.Errorf("VPN disconnected: no Kubernetes clusters accessible")
}

// testKubernetesConnectivity testa conectividade com um contexto específico
func testKubernetesConnectivity(kubeContext string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if kubeContext != "" {
		cmd = exec.CommandContext(ctx, "kubectl", "cluster-info", "--context", kubeContext, "--request-timeout=5s")
	} else {
		cmd = exec.CommandContext(ctx, "kubectl", "cluster-info", "--request-timeout=5s")
	}

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Se kubectl conseguiu responder (mesmo com erro de auth), VPN está OK
	if err == nil || strings.Contains(outputStr, "running at") || strings.Contains(outputStr, "Kubernetes") {
		return nil
	}

	return fmt.Errorf("kubectl failed: %w", err)
}
