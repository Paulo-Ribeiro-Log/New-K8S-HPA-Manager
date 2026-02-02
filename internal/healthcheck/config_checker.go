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

// Whitelist de ConfigMaps/Secrets conhecidos que podem estar vazios sem ser problema
var knownEmptyConfigMapsSecretsWhitelist = map[string]map[string]bool{
	// ConfigMaps conhecidos que podem estar vazios
	"configmap": {
		// Ingress NGINX
		"ingress-nginx-external/ingress-controller-leader":          true,
		"ingress-nginx-internal/ingress-controller-leader":          true,
		"ingress-nginx/ingress-controller-leader":                   true,
		"ingress-nginx-external/ingress-nginx-controller":           true,
		"ingress-nginx-internal/ingress-nginx-controller":           true,

		// Cert-manager
		"cert-manager/cert-manager-cainjector-leader-election":      true,
		"cert-manager/cert-manager-controller":                      true,

		// Kube-system
		"kube-system/extension-apiserver-authentication":            true,
		"kube-system/kube-proxy":                                    true,
		"kube-system/kubeadm-config":                                true,
		"kube-system/cluster-info":                                  true,

		// Istio
		"istio-system/istio-leader":                                 true,
		"istio-system/istio-ca-root-cert":                           true,

		// ArgoCD
		"argocd/argocd-cm":                                          true,
		"argocd/argocd-rbac-cm":                                     true,
	},

	// Secrets conhecidos que podem estar vazios ou ter dados mínimos
	"secret": {
		// Service Account Tokens (gerados automaticamente pelo K8s)
		// Pattern: default-token-*, system:serviceaccount:*
		// Estes serão tratados por regex no método isKnownEmptyResource
	},
}

// isKnownEmptyResource verifica se um recurso vazio está na whitelist
func (c *ConfigChecker) isKnownEmptyResource(namespace, name string, resourceType ResourceType) bool {
	key := fmt.Sprintf("%s/%s", namespace, name)

	// Verificar whitelist explícita
	if resourceType == ResourceConfigMap {
		if knownEmptyConfigMapsSecretsWhitelist["configmap"][key] {
			return true
		}
	} else if resourceType == ResourceSecret {
		// Service account tokens (pattern: default-token-*, *-token-*)
		if strings.HasSuffix(name, "-token") || strings.Contains(name, "-token-") {
			return true
		}
		// Service account secrets (pattern: *-sa-token, *-serviceaccount-*)
		if strings.HasSuffix(name, "-sa-token") || strings.Contains(name, "-serviceaccount-") {
			return true
		}
		// Docker registry secrets (gerados automaticamente)
		if strings.HasPrefix(name, "default-dockercfg-") {
			return true
		}

		if knownEmptyConfigMapsSecretsWhitelist["secret"][key] {
			return true
		}
	}

	return false
}

// CheckAll valida todos os ConfigMaps e Secrets necessários
func (c *ConfigChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, applyFilters bool, progressCallback ProgressCallback) []ConfigHealth {
	results := []ConfigHealth{}

	// Criar contexto com timeout se especificado
	workCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		workCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Primeiro, contar total de configs para calcular progresso
	totalConfigs := 0
	for _, ns := range namespaces {
		configMaps, err := client.CoreV1().ConfigMaps(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list configmaps")
			continue
		}
		totalConfigs += len(configMaps.Items)

		secrets, err := client.CoreV1().Secrets(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list secrets")
			continue
		}
		totalConfigs += len(secrets.Items)
	}

	currentConfig := 0

	for _, ns := range namespaces {
		// Validar ConfigMaps
		configMaps, err := client.CoreV1().ConfigMaps(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list configmaps")
			continue
		}

		for _, cm := range configMaps.Items {
			currentConfig++

			// Publicar evento: validando ConfigMap
			if progressCallback != nil {
				progressCallback(ns, cm.Name,
					fmt.Sprintf("Validando ConfigMap %s/%s...", ns, cm.Name),
					StatusHealthy, currentConfig, totalConfigs)
			}

			health := c.validateConfigMap(ns, cm.Name, cm.Data, applyFilters)

			// ✅ Se filtros ativos e recurso vazio está na whitelist, pular
			if applyFilters && health.Status == StatusWarning &&
			   c.isKnownEmptyResource(ns, cm.Name, ResourceConfigMap) {
				log.Debug().
					Str("namespace", ns).
					Str("configmap", cm.Name).
					Msg("Skipping known empty ConfigMap (whitelist)")
				continue
			}

			results = append(results, health)

			// Publicar resultado
			if progressCallback != nil {
				summary := c.getHealthSummary(health)
				progressCallback(ns, cm.Name,
					fmt.Sprintf("ConfigMap %s/%s: %s", ns, cm.Name, summary), health.Status, currentConfig, totalConfigs)
			}
		}

		// Validar Secrets
		secrets, err := client.CoreV1().Secrets(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list secrets")
			continue
		}

		for _, secret := range secrets.Items {
			currentConfig++

			// Publicar evento: validando Secret
			if progressCallback != nil {
				progressCallback(ns, secret.Name,
					fmt.Sprintf("Validando Secret %s/%s...", ns, secret.Name),
					StatusHealthy, currentConfig, totalConfigs)
			}

			// Converter []byte para string
			data := make(map[string]string)
			for k, v := range secret.Data {
				data[k] = string(v)
			}
			health := c.validateSecret(ns, secret.Name, data, applyFilters)

			// ✅ Se filtros ativos e recurso vazio está na whitelist, pular
			if applyFilters && health.Status == StatusWarning &&
			   c.isKnownEmptyResource(ns, secret.Name, ResourceSecret) {
				log.Debug().
					Str("namespace", ns).
					Str("secret", secret.Name).
					Msg("Skipping known empty Secret (whitelist)")
				continue
			}

			results = append(results, health)

			// Publicar resultado
			if progressCallback != nil {
				summary := c.getHealthSummary(health)
				progressCallback(ns, secret.Name,
					fmt.Sprintf("Secret %s/%s: %s", ns, secret.Name, summary), health.Status, currentConfig, totalConfigs)
			}
		}
	}

	return results
}

// getHealthSummary gera resumo compacto do health check
func (c *ConfigChecker) getHealthSummary(health ConfigHealth) string {
	if health.Status == StatusHealthy {
		return "OK"
	}
	// Para warning/critical, usar mensagem completa
	return health.Message
}

// validateConfigMap valida um ConfigMap específico
func (c *ConfigChecker) validateConfigMap(namespace, name string, data map[string]string, applyFilters bool) ConfigHealth {
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
		health.Message = fmt.Sprintf("CONFIGMAP VAZIO: %s/%s nao contem nenhuma chave. Se estiver em uso, aplicacao pode falhar ao ler configuracoes.", namespace, name)
		health.Suggestions = append(health.Suggestions, "Verificar se ConfigMap deveria conter dados de configuracao")
		health.Suggestions = append(health.Suggestions, "Se nao estiver em uso, considerar deletar para manter cluster limpo")
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
		health.Message = fmt.Sprintf("CONFIGMAP COM PROBLEMAS: %s/%s tem %d valor(es) invalido(s) ou vazio(s): %s. Aplicacao pode nao funcionar corretamente.", namespace, name, len(invalidValues), strings.Join(invalidValues, ", "))
		health.Suggestions = append(health.Suggestions, "Corrigir valores vazios ou invalidos no ConfigMap")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe configmap %s -n %s", name, namespace))
	} else {
		health.Status = StatusHealthy
		health.Message = fmt.Sprintf("OK: ConfigMap %s/%s validado com sucesso", namespace, name)
	}

	return health
}

// validateSecret valida um Secret específico
func (c *ConfigChecker) validateSecret(namespace, name string, data map[string]string, applyFilters bool) ConfigHealth {
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
		health.Message = fmt.Sprintf("SECRET VAZIO: %s/%s nao contem nenhuma chave. Se estiver em uso, aplicacao pode falhar ao ler credenciais.", namespace, name)
		health.Suggestions = append(health.Suggestions, "Verificar se Secret deveria conter credenciais ou certificados")
		health.Suggestions = append(health.Suggestions, "Se nao estiver em uso, considerar deletar para manter cluster limpo")
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
		health.Message = fmt.Sprintf("SECRET COM PROBLEMAS: %s/%s tem %d valor(es) invalido(s) ou vazio(s): %s. Credenciais podem estar incorretas.", namespace, name, len(invalidValues), strings.Join(invalidValues, ", "))
		health.Suggestions = append(health.Suggestions, "Corrigir valores vazios ou credenciais invalidas no Secret")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl get secret %s -n %s -o yaml", name, namespace))
		health.Suggestions = append(health.Suggestions, "ATENCAO: Secrets contem credenciais sensiveis - nao expor em logs")
	} else {
		health.Status = StatusHealthy
		health.Message = fmt.Sprintf("OK: Secret %s/%s validado com sucesso", namespace, name)
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
