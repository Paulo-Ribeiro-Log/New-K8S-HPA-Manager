package handlers

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// buildKialiURL constrói a URL externa do Kiali baseado no nome do cluster
// Pattern: https://kiali-<nome>-<ambiente>.grupocasasbahia.com.br/kiali/
// Exemplo: akspriv-abastecimento-hlg-admin → https://kiali-akspriv-abastecimento-hlg.grupocasasbahia.com.br/kiali/
func buildKialiURL(cluster string) string {
	// Remove sufixo "-admin"
	clean := strings.TrimSuffix(cluster, "-admin")

	// Pattern da URL do Kiali (Ingress externo com base path /kiali/)
	return fmt.Sprintf("https://kiali-%s.grupocasasbahia.com.br/kiali/", clean)
}

// validateKialiURL verifica se a URL do Kiali está acessível
func validateKialiURL(url string) bool {
	// Cliente HTTP com SSL auto-assinado permitido
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Certificado auto-assinado
			},
		},
	}

	// Testar endpoint /api/status (health check do Kiali)
	statusURL := url + "api/status"

	resp, err := client.Get(statusURL)
	if err != nil {
		fmt.Printf("[ServiceMesh] Erro ao validar Kiali URL %s: %v\n", url, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[ServiceMesh] Kiali URL retornou status %d: %s\n", resp.StatusCode, url)
		return false
	}

	fmt.Printf("[ServiceMesh] ✅ Kiali URL validada: %s\n", url)
	return true
}

// getKialiURL retorna a URL do Kiali para o cluster (auto-discovery)
func getKialiURL(cluster string) (string, error) {
	url := buildKialiURL(cluster)

	// Validar se URL está acessível
	if !validateKialiURL(url) {
		return "", fmt.Errorf("kiali não acessível em: %s", url)
	}

	return url, nil
}
