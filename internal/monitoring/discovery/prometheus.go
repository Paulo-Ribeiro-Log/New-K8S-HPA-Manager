package discovery

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// PrometheusEndpoint representa um endpoint Prometheus descoberto
type PrometheusEndpoint struct {
	Cluster     string // "akspriv-faturamento-hlg-admin"
	Name        string // "faturamento"
	Environment string // "hlg", "dev", "prod"
	URL         string // "https://prometheus-faturamento-hlg.viavarejo.com.br/"
	Available   bool   // Endpoint acessível?
}

// DiscoverEndpoint descobre e valida o endpoint Prometheus para um cluster
func DiscoverEndpoint(cluster string) (*PrometheusEndpoint, error) {
	name, env := parseClusterName(cluster)

	endpoint := &PrometheusEndpoint{
		Cluster:     cluster,
		Name:        name,
		Environment: env,
		URL:         resolvePrometheusURL(cluster, name, env),
		Available:   false,
	}

	// Validar se endpoint está acessível
	if err := validateEndpoint(endpoint); err != nil {
		log.Warn().
			Str("cluster", cluster).
			Str("url", endpoint.URL).
			Err(err).
			Msg("Endpoint Prometheus não disponível")
		return endpoint, err
	}

	endpoint.Available = true
	log.Info().
		Str("cluster", cluster).
		Str("url", endpoint.URL).
		Msg("Endpoint Prometheus descoberto com sucesso")

	return endpoint, nil
}

// parseClusterName extrai nome e ambiente do cluster
// Entrada: "akspriv-faturamento-hlg-admin"
// Saída: nome="akspriv-faturamento", ambiente="hlg"
// (mantém "akspriv-" no nome pois o Prometheus usa esse padrão no Ingress)
func parseClusterName(cluster string) (nome, ambiente string) {
	// Remove apenas sufixo "-admin"
	clean := strings.TrimSuffix(cluster, "-admin")

	// Split por "-" e pega última parte como ambiente
	parts := strings.Split(clean, "-")
	if len(parts) == 0 {
		return cluster, "unknown"
	}

	ambiente = parts[len(parts)-1] // "hlg", "dev", "prd"

	// Nome é tudo exceto o ambiente (mantém "akspriv-")
	if len(parts) > 1 {
		nome = strings.Join(parts[:len(parts)-1], "-")
	} else {
		nome = cluster
	}

	return nome, ambiente
}

// buildPrometheusURL constrói a URL do Prometheus a partir do nome e ambiente
// Pattern: https://prometheus-<nome>-<ambiente>.viavarejo.com.br/
// Usa o ambiente EXATAMENTE como vem do cluster (prd, hlg, dev)
func buildPrometheusURL(nome, ambiente string) string {
	return fmt.Sprintf("https://prometheus-%s-%s.viavarejo.com.br/", nome, ambiente)
}

// resolvePrometheusURL decide qual URL usar: um override manual (getPrometheusURLOverride, campo
// "prometheusUrl" em clusters-config.json/eks-clusters-config.json/gke-clusters-config.json) tem
// prioridade quando configurado; caso contrário cai no padrão automático de sempre
// (buildPrometheusURL). Nenhuma instalação existente tem esse campo preenchido, então para todo
// cluster AKS/EKS já funcionando hoje o resultado é idêntico ao anterior — o override só muda o
// comportamento de clusters que nunca resolveram corretamente por esse padrão (GKE, e EKS/AKS fora
// da convenção de hostname viavarejo.com.br).
func resolvePrometheusURL(cluster, nome, ambiente string) string {
	if override := getPrometheusURLOverride(cluster); override != "" {
		return override
	}
	return buildPrometheusURL(nome, ambiente)
}

// validateEndpoint valida se o endpoint Prometheus está acessível
func validateEndpoint(endpoint *PrometheusEndpoint) error {
	// Cliente HTTP com SSL auto-assinado permitido
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Certificado auto-assinado
			},
		},
	}

	// Testar endpoint /api/v1/status/config
	statusURL := endpoint.URL + "api/v1/status/config"

	req, err := http.NewRequest("GET", statusURL, nil)
	if err != nil {
		return fmt.Errorf("erro ao criar request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("erro ao conectar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code inválido: %d", resp.StatusCode)
	}

	log.Debug().
		Str("url", statusURL).
		Int("status", resp.StatusCode).
		Msg("Endpoint Prometheus validado")

	return nil
}

// GetPrometheusURL retorna a URL completa do Prometheus para um cluster
func GetPrometheusURL(cluster string) string {
	name, env := parseClusterName(cluster)
	return resolvePrometheusURL(cluster, name, env)
}

// IsEndpointAvailable verifica rapidamente se o endpoint está disponível
func IsEndpointAvailable(url string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	resp, err := client.Get(url + "api/v1/status/config")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
