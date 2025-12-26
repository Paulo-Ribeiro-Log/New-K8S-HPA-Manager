package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ConfigChecker valida ConfigMaps e Secrets
type ConfigChecker struct{}

// NewConfigChecker cria um novo config checker
func NewConfigChecker() *ConfigChecker {
	return &ConfigChecker{}
}

// CheckAll valida todos os ConfigMaps e Secrets necessários
func (c *ConfigChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string) []ConfigHealth {
	results := []ConfigHealth{}

	for _, ns := range namespaces {
		// Validar ConfigMaps
		configMaps, err := client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list configmaps")
			continue
		}

		for _, cm := range configMaps.Items {
			health := c.validateConfigMap(ns, cm.Name, cm.Data)
			results = append(results, health)
		}

		// Validar Secrets
		secrets, err := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list secrets")
			continue
		}

		for _, secret := range secrets.Items {
			// Converter []byte para string
			data := make(map[string]string)
			for k, v := range secret.Data {
				data[k] = string(v)
			}
			health := c.validateSecret(ns, secret.Name, data)
			results = append(results, health)
		}
	}

	return results
}

// validateConfigMap valida um ConfigMap específico
func (c *ConfigChecker) validateConfigMap(namespace, name string, data map[string]string) ConfigHealth {
	health := ConfigHealth{
		Name:         name,
		Namespace:    namespace,
		ResourceType: ResourceConfigMap,
		Exists:       true,
		CheckedAt:    time.Now(),
		Suggestions:  []string{},
	}

	// Verificar se está vazio
	if len(data) == 0 {
		health.Status = StatusWarning
		health.Message = "ConfigMap vazio (sem chaves)"
		health.Suggestions = append(health.Suggestions, "ConfigMap não contém dados úteis")
		health.Suggestions = append(health.Suggestions, "Considerar deletar se não estiver em uso")
		return health
	}

	// Validar valores
	invalidValues := []string{}
	hasRequiredKeys := true

	for key, value := range data {
		// Verificar valores vazios
		if strings.TrimSpace(value) == "" {
			invalidValues = append(invalidValues, fmt.Sprintf("%s (valor vazio)", key))
		}

		// Validar connection strings (se parecer ser uma)
		if c.looksLikeConnectionString(key, value) {
			if !c.isValidConnectionString(value) {
				invalidValues = append(invalidValues, fmt.Sprintf("%s (connection string inválida)", key))
			}
		}
	}

	health.HasRequiredKeys = hasRequiredKeys
	health.InvalidValues = invalidValues

	// Determinar status
	if len(invalidValues) > 0 {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("ConfigMap com %d valores inválidos ou vazios", len(invalidValues))
		health.Suggestions = append(health.Suggestions, "Verificar valores vazios ou inválidos")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe configmap %s -n %s", name, namespace))
	} else {
		health.Status = StatusHealthy
		health.Message = "ConfigMap validado com sucesso"
	}

	return health
}

// validateSecret valida um Secret específico
func (c *ConfigChecker) validateSecret(namespace, name string, data map[string]string) ConfigHealth {
	health := ConfigHealth{
		Name:         name,
		Namespace:    namespace,
		ResourceType: ResourceSecret,
		Exists:       true,
		CheckedAt:    time.Now(),
		Suggestions:  []string{},
	}

	// Verificar se está vazio
	if len(data) == 0 {
		health.Status = StatusWarning
		health.Message = "Secret vazio (sem chaves)"
		health.Suggestions = append(health.Suggestions, "Secret não contém dados úteis")
		health.Suggestions = append(health.Suggestions, "Considerar deletar se não estiver em uso")
		return health
	}

	// Validar valores (secrets geralmente contêm credenciais)
	invalidValues := []string{}
	hasRequiredKeys := true

	for key, value := range data {
		// Verificar valores vazios
		if strings.TrimSpace(value) == "" {
			invalidValues = append(invalidValues, fmt.Sprintf("%s (valor vazio)", key))
		}

		// Validar connection strings (se parecer ser uma)
		if c.looksLikeConnectionString(key, value) {
			if !c.isValidConnectionString(value) {
				invalidValues = append(invalidValues, fmt.Sprintf("%s (connection string inválida)", key))
			}
		}

		// Validar chaves comuns de autenticação
		if c.isAuthenticationKey(key) {
			if len(strings.TrimSpace(value)) < 8 {
				invalidValues = append(invalidValues, fmt.Sprintf("%s (credential muito curta, possível erro)", key))
			}
		}
	}

	health.HasRequiredKeys = hasRequiredKeys
	health.InvalidValues = invalidValues

	// Determinar status
	if len(invalidValues) > 0 {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Secret com %d valores inválidos ou vazios", len(invalidValues))
		health.Suggestions = append(health.Suggestions, "Verificar valores vazios ou credenciais inválidas")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe secret %s -n %s", name, namespace))
		health.Suggestions = append(health.Suggestions, "ATENÇÃO: Secrets podem conter credenciais sensíveis")
	} else {
		health.Status = StatusHealthy
		health.Message = "Secret validado com sucesso"
	}

	return health
}

// looksLikeConnectionString detecta se chave ou valor parecem ser connection string
func (c *ConfigChecker) looksLikeConnectionString(key, value string) bool {
	keyLower := strings.ToLower(key)
	valueLower := strings.ToLower(value)

	// Chaves comuns de connection string
	connectionKeywords := []string{
		"connection", "conn", "url", "uri", "dsn",
		"database_url", "db_url", "mongo_url", "redis_url",
	}

	for _, keyword := range connectionKeywords {
		if strings.Contains(keyLower, keyword) {
			return true
		}
	}

	// Valores que começam com padrões de connection string
	connectionPrefixes := []string{
		"mongodb://", "mongodb+srv://",
		"redis://", "rediss://",
		"postgresql://", "postgres://",
		"mysql://",
		"http://", "https://",
		"amqp://", "amqps://",
	}

	for _, prefix := range connectionPrefixes {
		if strings.HasPrefix(valueLower, prefix) {
			return true
		}
	}

	// EventHub
	if strings.Contains(valueLower, "endpoint=sb://") {
		return true
	}

	return false
}

// isValidConnectionString valida formato básico de connection string
func (c *ConfigChecker) isValidConnectionString(value string) bool {
	value = strings.TrimSpace(value)

	// Connection string muito curta
	if len(value) < 10 {
		return false
	}

	// Verificar se tem protocolo válido
	hasValidProtocol := strings.HasPrefix(value, "mongodb://") ||
		strings.HasPrefix(value, "mongodb+srv://") ||
		strings.HasPrefix(value, "redis://") ||
		strings.HasPrefix(value, "rediss://") ||
		strings.HasPrefix(value, "postgresql://") ||
		strings.HasPrefix(value, "postgres://") ||
		strings.HasPrefix(value, "mysql://") ||
		strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "amqp://") ||
		strings.HasPrefix(value, "amqps://") ||
		strings.Contains(value, "Endpoint=sb://")

	if !hasValidProtocol {
		return false
	}

	// Verificar se tem host/endpoint (básico - não precisa ser perfeito)
	// Connection string deve ter algo após o protocolo
	parts := strings.SplitN(value, "://", 2)
	if len(parts) == 2 {
		hostPart := parts[1]
		// Host não pode ser vazio
		if strings.TrimSpace(hostPart) == "" {
			return false
		}
	}

	return true
}

// isAuthenticationKey detecta se chave é de autenticação
func (c *ConfigChecker) isAuthenticationKey(key string) bool {
	keyLower := strings.ToLower(key)

	authKeywords := []string{
		"password", "passwd", "pwd",
		"secret", "token", "key",
		"api_key", "apikey",
		"auth", "credential",
	}

	for _, keyword := range authKeywords {
		if strings.Contains(keyLower, keyword) {
			return true
		}
	}

	return false
}
