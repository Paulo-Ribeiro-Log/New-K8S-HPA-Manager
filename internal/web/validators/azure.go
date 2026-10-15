package validators

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s-hpa-manager/internal/azlogin"
)

// ValidateAzureAuth confere se o login do Azure CLI ainda vale (azlogin.Check, com cache de 60 s).
// Nunca roda `az login`: numa requisição web ninguém está no terminal do servidor para concluir o
// login, e a requisição ficava presa esperando. Login expirado → azlogin.ErrLoginRequired.
// Estado indeterminado (timeout/rede) não bloqueia: a operação real roda e, se falhar por login,
// o erro dela é classificado (azlogin.Classify).
func ValidateAzureAuth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	switch st := azlogin.Check(ctx, false); st.State {
	case azlogin.StateNotInstalled:
		return fmt.Errorf("Azure CLI não instalado")
	case azlogin.StateLoginRequired:
		return fmt.Errorf("%w\n%s", azlogin.ErrLoginRequired, st.Detail)
	}
	return nil
}

// ValidateVPNConnectivity verifica conectividade com Kubernetes (requer VPN)
// Testa QUALQUER contexto disponível (não apenas prd/hlg)
func ValidateVPNConnectivity() error {
	// Obter lista de TODOS os contextos disponíveis
	cmd := exec.Command("kubectl", "config", "get-contexts", "-o", "name")
	output, err := cmd.Output()
	if err != nil {
		// Se kubectl não funciona, considerar VPN OK (não bloquear)
		return nil
	}

	contexts := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(contexts) == 0 {
		// Sem contextos configurados - não bloquear
		return nil
	}

	// Tentar contexto atual primeiro (mais rápido)
	if err := testKubernetesConnectivity(""); err == nil {
		return nil
	}

	// Tentar todos os outros contextos disponíveis
	// Limitar a 10 contextos para evitar demora excessiva
	maxTests := 10
	if len(contexts) < maxTests {
		maxTests = len(contexts)
	}

	for i := 0; i < maxTests; i++ {
		context := strings.TrimSpace(contexts[i])
		if context == "" {
			continue
		}
		if err := testKubernetesConnectivity(context); err == nil {
			return nil
		}
	}

	return fmt.Errorf("VPN disconnected: no Kubernetes clusters accessible")
}

// testKubernetesConnectivity testa conectividade com um contexto específico
func testKubernetesConnectivity(kubeContext string) error {
	// Timeout AUMENTADO para 10 segundos (rede lenta/VPN)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if kubeContext != "" {
		cmd = exec.CommandContext(ctx, "kubectl", "cluster-info", "--context", kubeContext, "--request-timeout=8s")
	} else {
		cmd = exec.CommandContext(ctx, "kubectl", "cluster-info", "--request-timeout=8s")
	}

	output, err := cmd.CombinedOutput()
	outputStr := strings.ToLower(string(output))

	// Múltiplos indicadores de sucesso (VPN conectada)
	successIndicators := []string{
		"running at",
		"kubernetes",
		"control plane",
		"is running",
		"master",
	}

	for _, indicator := range successIndicators {
		if strings.Contains(outputStr, indicator) {
			return nil
		}
	}

	// Se erro == nil, kubectl respondeu com sucesso
	if err == nil {
		return nil
	}

	return fmt.Errorf("kubectl failed: %w", err)
}
