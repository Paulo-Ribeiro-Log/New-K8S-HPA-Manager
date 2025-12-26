package healthcheck

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/healthcheck/analyzers"
)

// ServiceAnalyzer interface para analyzers de serviços externos
type ServiceAnalyzer interface {
	Check(ctx context.Context, connectionString string, timeout int) (bool, int64, error)
	GetDetails(ctx context.Context, connectionString string) (map[string]interface{}, error)
}

// ServiceChecker testa conectividade de serviços externos
type ServiceChecker struct {
	analyzers map[ServiceType]ServiceAnalyzer
}

// NewServiceChecker cria um novo service checker
func NewServiceChecker() *ServiceChecker {
	return &ServiceChecker{
		analyzers: map[ServiceType]ServiceAnalyzer{
			ServiceMongoDB:  &analyzers.MongoDBAnalyzer{},
			ServiceRedis:    &analyzers.RedisAnalyzer{},
			ServicePostgres: &analyzers.PostgresAnalyzer{},
			ServiceKafka:    &analyzers.KafkaAnalyzer{},
			ServiceEventHub: &analyzers.EventHubAnalyzer{},
			ServiceHTTP:     &analyzers.HTTPAnalyzer{},
		},
	}
}

// CheckAll verifica conectividade de todos os serviços externos
func (c *ServiceChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int) []ServiceHealth {
	results := []ServiceHealth{}

	for _, ns := range namespaces {
		// Buscar ConfigMaps e Secrets com connection strings
		configMaps, err := client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list configmaps")
			continue
		}

		secrets, err := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list secrets")
			continue
		}

		// Extrair connection strings de ConfigMaps
		for _, cm := range configMaps.Items {
			for key, value := range cm.Data {
				serviceType, connStr := c.detectServiceType(value)
				if serviceType != "" {
					health := c.Check(ctx, ns, cm.Name, key, serviceType, connStr, timeout)
					health.ConfigSource = fmt.Sprintf("configmap:%s/%s", cm.Name, key)
					results = append(results, health)
				}
			}
		}

		// Extrair connection strings de Secrets
		for _, secret := range secrets.Items {
			for key, value := range secret.Data {
				valueStr := string(value)
				serviceType, connStr := c.detectServiceType(valueStr)
				if serviceType != "" {
					health := c.Check(ctx, ns, secret.Name, key, serviceType, connStr, timeout)
					health.ConfigSource = fmt.Sprintf("secret:%s/%s", secret.Name, key)
					results = append(results, health)
				}
			}
		}
	}

	return results
}

// detectServiceType detecta tipo de serviço por padrões de connection string
func (c *ServiceChecker) detectServiceType(value string) (ServiceType, string) {
	// MongoDB: mongodb://... ou mongodb+srv://...
	if strings.HasPrefix(value, "mongodb://") || strings.HasPrefix(value, "mongodb+srv://") {
		return ServiceMongoDB, value
	}

	// Redis: redis://...
	if strings.HasPrefix(value, "redis://") {
		return ServiceRedis, value
	}

	// PostgreSQL: postgresql://... ou postgres://...
	if strings.HasPrefix(value, "postgresql://") || strings.HasPrefix(value, "postgres://") {
		return ServicePostgres, value
	}

	// Kafka: broker:port (formato simples) - apenas se parece com host:port e não é outro protocolo
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9\-\.]+:\d+$`, value); matched {
		return ServiceKafka, value
	}

	// EventHub: Endpoint=sb://...
	if strings.Contains(value, "Endpoint=sb://") {
		return ServiceEventHub, value
	}

	// HTTP: http://... ou https://...
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return ServiceHTTP, value
	}

	return "", ""
}

// Check verifica conectividade de um serviço específico
func (c *ServiceChecker) Check(ctx context.Context, namespace, resourceName, key string, serviceType ServiceType, connStr string, timeout int) ServiceHealth {
	health := ServiceHealth{
		Name:        fmt.Sprintf("%s/%s", resourceName, key),
		Namespace:   namespace,
		ServiceType: serviceType,
		CheckedAt:   time.Now(),
		Suggestions: []string{},
	}

	analyzer, exists := c.analyzers[serviceType]
	if !exists {
		health.Status = StatusUnknown
		health.Message = fmt.Sprintf("Analyzer não implementado para %s", serviceType)
		return health
	}

	// Executar check com timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	reachable, latency, err := analyzer.Check(timeoutCtx, connStr, timeout)

	health.Reachable = reachable
	health.LatencyMs = latency

	if err != nil {
		health.Status = StatusCritical
		health.ConnectionError = err.Error()
		health.Message = fmt.Sprintf("Falha ao conectar: %v", err)
		health.Suggestions = append(health.Suggestions, "Verificar se o serviço está acessível")
		health.Suggestions = append(health.Suggestions, "Validar connection string no ConfigMap/Secret")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("Testar conectividade: kubectl exec -n %s <pod> -- curl/nc/telnet", namespace))
		return health
	}

	// Obter detalhes adicionais (não crítico se falhar)
	details, err := analyzer.GetDetails(timeoutCtx, connStr)
	if err == nil {
		health.Details = details
	}

	// Determinar status baseado em latência
	if latency > 1000 {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Conectado, mas latência alta (%dms)", latency)
		health.Suggestions = append(health.Suggestions, "Investigar performance da rede")
		health.Suggestions = append(health.Suggestions, "Verificar se serviço está sobrecarregado")
	} else {
		health.Status = StatusHealthy
		health.Message = fmt.Sprintf("Conectado com sucesso (latência: %dms)", latency)
	}

	return health
}
