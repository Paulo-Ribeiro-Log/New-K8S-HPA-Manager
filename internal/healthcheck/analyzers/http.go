package analyzers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// HTTPAnalyzer testa conectividade de endpoints HTTP/HTTPS
type HTTPAnalyzer struct{}

// CAStatus contém informações sobre status dos certificados CA
type CAStatus struct {
	SystemPoolLoaded bool
	CustomCAPath     string
	CustomCALoaded   bool
	TotalCertsLoaded int
}

// getCAStatus verifica e retorna status dos certificados CA
func (a *HTTPAnalyzer) getCAStatus() CAStatus {
	status := CAStatus{}

	// Verificar system pool
	_, err := x509.SystemCertPool()
	status.SystemPoolLoaded = (err == nil)

	// Verificar custom CA
	customCAPath := os.Getenv("CUSTOM_CA_BUNDLE")
	if customCAPath == "" {
		// Tentar locais padrão
		possiblePaths := []string{
			"/etc/ssl/certs/ca-certificates.crt",           // Debian/Ubuntu
			"/etc/pki/tls/certs/ca-bundle.crt",             // RHEL/CentOS
			"/etc/ssl/ca-bundle.pem",                       // OpenSUSE
			"/usr/local/share/ca-certificates/ca-bundle.crt", // Manual install
			"./ca-certificates.crt",                        // Local (desenvolvimento)
		}

		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				customCAPath = path
				break
			}
		}
	}

	status.CustomCAPath = customCAPath

	if customCAPath != "" {
		caCert, err := os.ReadFile(customCAPath)
		if err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(caCert) {
				status.CustomCALoaded = true
				// Contar certificados aproximadamente (cada BEGIN CERTIFICATE)
				status.TotalCertsLoaded = strings.Count(string(caCert), "BEGIN CERTIFICATE")
			}
		}
	}

	return status
}

// getHTTPClient cria cliente HTTP com suporte a certificados CA customizados
func (a *HTTPAnalyzer) getHTTPClient(timeout int) *http.Client {
	// Começar com pool de CAs do sistema operacional
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		log.Warn().Err(err).Msg("⚠️  Failed to load system cert pool - this may cause TLS validation issues")
		rootCAs = x509.NewCertPool()
	}

	// Verificar se existe arquivo de CA customizado
	customCAPath := os.Getenv("CUSTOM_CA_BUNDLE")
	foundCustomCA := false

	if customCAPath == "" {
		// Tentar locais padrão
		possiblePaths := []string{
			"/etc/ssl/certs/ca-certificates.crt",           // Debian/Ubuntu
			"/etc/pki/tls/certs/ca-bundle.crt",             // RHEL/CentOS
			"/etc/ssl/ca-bundle.pem",                       // OpenSUSE
			"/usr/local/share/ca-certificates/ca-bundle.crt", // Manual install
			"./ca-certificates.crt",                        // Local (desenvolvimento)
		}

		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				customCAPath = path
				break
			}
		}
	}

	// Adicionar CAs customizados se encontrados
	if customCAPath != "" {
		caCert, err := os.ReadFile(customCAPath)
		if err != nil {
			log.Warn().
				Err(err).
				Str("path", customCAPath).
				Msg("⚠️  Custom CA bundle found but failed to read")
		} else {
			if ok := rootCAs.AppendCertsFromPEM(caCert); ok {
				certCount := strings.Count(string(caCert), "BEGIN CERTIFICATE")
				log.Info().
					Str("path", customCAPath).
					Int("certificates", certCount).
					Msg("✅ Loaded custom CA certificates for TLS validation")
				foundCustomCA = true
			} else {
				log.Warn().
					Str("path", customCAPath).
					Msg("⚠️  Custom CA bundle found but contains no valid PEM certificates")
			}
		}
	}

	// Se não encontrou custom CA, avisar
	if !foundCustomCA {
		log.Warn().Msg("⚠️  No custom CA certificates found - TLS validation may fail for internal company endpoints")
		log.Warn().Msg("💡 To fix: Set CUSTOM_CA_BUNDLE env var or place certificates in standard locations")
		log.Warn().Msg("📖 See: docs/guides/CUSTOM_CA_CERTIFICATES.md")
	}

	return &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: rootCAs,
			},
		},
	}
}

// Check executa GET request no endpoint e valida status code
func (a *HTTPAnalyzer) Check(ctx context.Context, endpoint string, timeout int) (bool, int64, error) {
	start := time.Now()

	// Usar cliente com suporte a CA certificates customizados
	client := a.getHTTPClient(timeout)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Detectar se erro é de certificado TLS
		if strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "x509:") || strings.Contains(err.Error(), "certificate") {
			// Obter status dos certificados CA
			caStatus := a.getCAStatus()

			log.Error().
				Err(err).
				Str("endpoint", endpoint).
				Bool("system_pool_loaded", caStatus.SystemPoolLoaded).
				Str("custom_ca_path", caStatus.CustomCAPath).
				Bool("custom_ca_loaded", caStatus.CustomCALoaded).
				Int("total_certs", caStatus.TotalCertsLoaded).
				Msg("❌ TLS certificate validation failed")

			// Construir mensagem de erro detalhada
			errorMsg := fmt.Sprintf("TLS certificate validation failed: %v", err)

			if !caStatus.CustomCALoaded {
				errorMsg += "\n\n🔍 DIAGNÓSTICO AUTOMÁTICO:"
				errorMsg += "\n   ❌ Nenhum certificado CA customizado foi carregado"
				errorMsg += "\n   ⚠️  Isso é necessário para validar endpoints internos da empresa"
				errorMsg += "\n\n💡 SOLUÇÃO RECOMENDADA:"
				errorMsg += "\n   1. Obter certificado CA raiz da empresa (exportar do navegador ou solicitar ao time de infra)"
				errorMsg += "\n   2. Instalar usando um destes métodos:"
				errorMsg += "\n"
				errorMsg += "\n      Opção A - Variável de ambiente:"
				errorMsg += "\n      export CUSTOM_CA_BUNDLE=/caminho/para/empresa-ca.crt"
				errorMsg += "\n      ./build/new-k8s-hpa web"
				errorMsg += "\n"
				errorMsg += "\n      Opção B - Sistema (Debian/Ubuntu):"
				errorMsg += "\n      sudo cp empresa-ca.crt /usr/local/share/ca-certificates/"
				errorMsg += "\n      sudo update-ca-certificates"
				errorMsg += "\n"
				errorMsg += "\n      Opção C - Local (desenvolvimento):"
				errorMsg += "\n      cp empresa-ca.crt ./ca-certificates.crt"
				errorMsg += "\n"
				errorMsg += "\n📖 Documentação: docs/guides/CUSTOM_CA_CERTIFICATES.md"
			} else {
				errorMsg += fmt.Sprintf("\n\n🔍 DIAGNÓSTICO AUTOMÁTICO:")
				errorMsg += fmt.Sprintf("\n   ✅ Certificados CA carregados: %d certificados de %s", caStatus.TotalCertsLoaded, caStatus.CustomCAPath)
				errorMsg += "\n   ⚠️  Porém a validação falhou - possíveis causas:"
				errorMsg += "\n      - Certificado CA instalado não é o correto para este endpoint"
				errorMsg += "\n      - Certificado do servidor expirado ou inválido"
				errorMsg += "\n      - Problema de conectividade de rede"
			}

			return false, 0, fmt.Errorf("%s", errorMsg)
		}
		return false, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	// Status 4xx/5xx = endpoint reachable mas com erro
	if resp.StatusCode >= 400 {
		return false, latency, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return true, latency, nil
}

// GetDetails retorna informações adicionais do endpoint HTTP
func (a *HTTPAnalyzer) GetDetails(ctx context.Context, endpoint string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", endpoint, nil)
	if err != nil {
		return nil, err
	}

	// Usar cliente com suporte a CA certificates customizados
	client := a.getHTTPClient(5)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return map[string]interface{}{
		"status_code":    resp.StatusCode,
		"content_type":   resp.Header.Get("Content-Type"),
		"content_length": resp.ContentLength,
		"server":         resp.Header.Get("Server"),
	}, nil
}
