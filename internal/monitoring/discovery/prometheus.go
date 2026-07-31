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
	Cluster         string // "akspriv-faturamento-hlg-admin"
	Name            string // "faturamento"
	Environment     string // "hlg", "dev", "prod"
	URL             string // "https://prometheus-faturamento-hlg.viavarejo.com.br/"
	Available       bool   // Endpoint acessível?
	RequiresGCPAuth bool   // true quando URL é o Google Cloud Managed Service for Prometheus (GMP) — requisições precisam de "Authorization: Bearer <token>" (ver GCPAuthTransport)
}

// DiscoverEndpoint descobre e valida o endpoint Prometheus para um cluster
func DiscoverEndpoint(cluster string) (*PrometheusEndpoint, error) {
	name, env := parseClusterName(cluster)
	url, requiresGCPAuth := resolvePrometheusSource(cluster, name, env)

	endpoint := &PrometheusEndpoint{
		Cluster:         cluster,
		Name:            name,
		Environment:     env,
		URL:             url,
		Available:       false,
		RequiresGCPAuth: requiresGCPAuth,
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

// resolvePrometheusSource decide qual URL usar e se ela exige autenticação GCP, em ordem de
// prioridade:
//  1. Override manual (getPrometheusURLOverride, campo "prometheusUrl" nos *-clusters-config.json)
//     — nunca exige auth GCP, é uma URL arbitrária configurada por quem operou o arquivo.
//  2. GMP automático (buildGMPURL) — só se aplica a contexts GKE (gke_<project>_<region>_<cluster>)
//     sem override; URL determinística a partir do Project ID embutido no context, sempre exige
//     auth GCP.
//  3. Padrão de sempre (buildPrometheusURL) — hostname viavarejo.com.br, sem auth.
//
// Nenhuma instalação existente tem override configurado, então para todo cluster AKS/EKS já
// funcionando hoje o resultado desta função é idêntico ao buildPrometheusURL de antes — GMP só
// entra em jogo para contexts que começam com "gke_", que antes desta fase sempre falhavam.
func resolvePrometheusSource(cluster, nome, ambiente string) (url string, requiresGCPAuth bool) {
	if override := getPrometheusURLOverride(cluster); override != "" {
		return override, false
	}
	if gmpURL := buildGMPURL(cluster); gmpURL != "" {
		return gmpURL, true
	}
	return buildPrometheusURL(nome, ambiente), false
}

// validateEndpoint valida se o endpoint Prometheus está acessível
func validateEndpoint(endpoint *PrometheusEndpoint) error {
	var transport http.RoundTripper = &http.Transport{}

	// /api/v1/status/config é um endpoint de introspecção de servidor Prometheus real — o GMP
	// (Google Cloud Managed Service for Prometheus, usado por endpoints RequiresGCPAuth) não o
	// implementa, só a API de query. Usa uma query barata (`up`) como equivalente.
	statusPath := "api/v1/status/config"
	if endpoint.RequiresGCPAuth {
		statusPath = "api/v1/query?query=up"
		transport = GCPAuthTransport(transport)
	} else {
		// Certificado auto-assinado — só se aplica ao Prometheus self-hosted (viavarejo.com.br).
		// GMP tem certificado válido do Google; nunca pular verificação nesse caso.
		transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	statusURL := endpoint.URL + statusPath

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

// GetPrometheusURL retorna a URL completa do Prometheus para um cluster. Não indica se a URL
// exige autenticação GCP — usar RequiresGCPAuth(cluster) separadamente quando isso importar
// (chamadores que só têm a URL crua em mãos, ex: internal/finops, internal/monitoring/alerts).
func GetPrometheusURL(cluster string) string {
	name, env := parseClusterName(cluster)
	url, _ := resolvePrometheusSource(cluster, name, env)
	return url
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
