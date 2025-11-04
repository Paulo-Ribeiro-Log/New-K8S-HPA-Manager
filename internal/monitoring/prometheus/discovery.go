package prometheus

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"github.com/rs/zerolog/log"
)

// DiscoveryConfig configuração para auto-discovery
type DiscoveryConfig struct {
	Enabled         bool
	Namespaces      []string // namespaces onde procurar (default: monitoring)
	ServicePatterns []string // padrões de nome de serviço
}

// DefaultDiscoveryConfig retorna configuração padrão
func DefaultDiscoveryConfig() *DiscoveryConfig {
	return &DiscoveryConfig{
		Enabled: true,
		Namespaces: []string{
			"monitoring",
			"prometheus",
			"kube-prometheus",
		},
		ServicePatterns: []string{
			"prometheus",
			"prometheus-server",
			"prometheus-operated",
			"kube-prometheus-stack-prometheus",
			"prometheus-k8s",
		},
	}
}

// Simple discovery without K8s client dependency
// Note: More advanced discovery should be implemented in the monitor package
// to avoid import cycles

// VerifyPrometheusEndpoint verifica se um endpoint está funcional
func VerifyPrometheusEndpoint(ctx context.Context, cluster, endpoint string) error {
	client, err := NewClient(cluster, endpoint)
	if err != nil {
		return err
	}

	return client.TestConnection(ctx)
}

// GetPrometheusVersion obtém a versão do Prometheus
func GetPrometheusVersion(ctx context.Context, client *Client) (string, error) {
	buildInfo, err := client.api.Buildinfo(ctx)
	if err != nil {
		return "", err
	}

	version := buildInfo.Version
	if version == "" {
		return "unknown", nil
	}

	return version, nil
}

// GetPrometheusConfig obtém configuração do Prometheus
func GetPrometheusConfig(ctx context.Context, client *Client) (map[string]interface{}, error) {
	config, err := client.api.Config(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"yaml": config.YAML,
	}, nil
}

// GetPrometheusTargets obtém targets configurados no Prometheus
func GetPrometheusTargets(ctx context.Context, client *Client) (map[string]interface{}, error) {
	targets, err := client.api.Targets(ctx)
	if err != nil {
		return nil, err
	}

	activeCount := 0
	droppedCount := 0

	if targets.Active != nil {
		activeCount = len(targets.Active)
	}
	if targets.Dropped != nil {
		droppedCount = len(targets.Dropped)
	}

	return map[string]interface{}{
		"active_count":  activeCount,
		"dropped_count": droppedCount,
		"active":        targets.Active,
		"dropped":       targets.Dropped,
	}, nil
}

// CheckPrometheusHealth verifica saúde do Prometheus
func CheckPrometheusHealth(ctx context.Context, endpoint string) (*models.PrometheusHealth, error) {
	client, err := NewClient("health-check", endpoint)
	if err != nil {
		return nil, err
	}

	health := &models.PrometheusHealth{
		Endpoint:  endpoint,
		Timestamp: time.Now(),
	}

	// Testa conexão
	if err := client.TestConnection(ctx); err != nil {
		health.Healthy = false
		health.Error = err.Error()
		return health, nil
	}

	health.Healthy = true
	health.Connected = true

	// Obtém versão
	if version, err := GetPrometheusVersion(ctx, client); err == nil {
		health.Version = version
	}

	// Obtém targets
	if targets, err := GetPrometheusTargets(ctx, client); err == nil {
		health.ActiveTargets = targets["active_count"].(int)
		health.DroppedTargets = targets["dropped_count"].(int)
	}

	log.Info().
		Str("endpoint", endpoint).
		Bool("healthy", health.Healthy).
		Str("version", health.Version).
		Int("targets", health.ActiveTargets).
		Msg("Prometheus health check complete")

	return health, nil
}

// DiscoverAndConnect descobre e conecta ao Prometheus em um cluster
// Retorna o client, health info e erro
func DiscoverAndConnect(ctx context.Context, clientset interface{}, cluster, namespace string) (*Client, *models.PrometheusHealth, error) {
	// Por hora, vamos tentar endpoints conhecidos diretamente
	// TODO: Implementar discovery real via K8s API quando resolver import cycle

	knownServices := []string{
		"prometheus-k8s-prometheus-1",
		"prometheus-server",
		"prometheus-operated",
		"kube-prometheus-stack-prometheus",
		"prometheus-k8s",
		"prometheus",
	}

	for _, serviceName := range knownServices {
		// Tenta endpoint ClusterIP
		endpoint := fmt.Sprintf("http://%s.%s.svc:9090", serviceName, namespace)

		client, err := NewClient(cluster, endpoint)
		if err != nil {
			continue
		}

		// Testa conexão
		testCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		if err := client.TestConnection(testCtx); err != nil {
			continue
		}

		// Coleta health info
		health, err := CheckPrometheusHealth(ctx, endpoint)
		if err != nil || !health.Healthy {
			continue
		}

		log.Info().
			Str("cluster", cluster).
			Str("namespace", namespace).
			Str("service", serviceName).
			Str("endpoint", endpoint).
			Msg("✅ Prometheus discovered and connected")

		return client, health, nil
	}

	return nil, nil, fmt.Errorf("prometheus not found in namespace %s", namespace)
}
