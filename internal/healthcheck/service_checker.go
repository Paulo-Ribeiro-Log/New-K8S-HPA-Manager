package healthcheck

import (
	"context"

	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
)

/* IMPORTS COMENTADOS - apenas necessários quando service checking estiver habilitado
import (
	"fmt"
	"regexp"
	"strings"
	"time"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s-hpa-manager/internal/healthcheck/analyzers"
)
*/

// ServiceChecker testa conectividade de serviços externos (DESABILITADO)
type ServiceChecker struct{}

// NewServiceChecker cria um novo service checker
func NewServiceChecker() *ServiceChecker {
	return &ServiceChecker{}
}

/* CÓDIGO COMENTADO - ServiceAnalyzer e analyzers
type ServiceAnalyzer interface {
	Check(ctx context.Context, connectionString string, timeout int) (bool, int64, error)
	GetDetails(ctx context.Context, connectionString string) (map[string]interface{}, error)
}

type ServiceCheckerOriginal struct {
	analyzers map[ServiceType]ServiceAnalyzer
}

func NewServiceCheckerOriginal() *ServiceCheckerOriginal {
	return &ServiceCheckerOriginal{
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
*/

// CheckAll verifica conectividade de todos os serviços externos
// ⚠️ DESABILITADO: Testes de conectividade são feitos do servidor web,
// que não tem acesso aos serviços internos do cluster (DNS, firewalls).
// Isso gera alarmes falsos (critical) em clusters saudáveis.
func (c *ServiceChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, progressCallback ProgressCallback) []ServiceHealth {
	// ✅ Retornar array vazio - service checking desabilitado
	log.Info().Msg("Service checking desabilitado - servidor web não tem acesso a serviços internos do cluster")
	return []ServiceHealth{}
}

/* CÓDIGO ORIGINAL - DESABILITADO
func (c *ServiceChecker) _CheckAllOriginal(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, progressCallback ProgressCallback) []ServiceHealth {
	results := []ServiceHealth{}

	// Primeiro, contar total de services para calcular progresso
	totalServices := 0
	servicesToCheck := []struct {
		namespace    string
		resourceName string
		key          string
		serviceType  ServiceType
		connStr      string
		configSource string
	}{}

	// Detectar todos os services primeiro
	for _, ns := range namespaces {
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

		// Coletar de ConfigMaps
		for _, cm := range configMaps.Items {
			for key, value := range cm.Data {
				serviceType, connStr := c._detectServiceType(value)
				if serviceType != "" {
					servicesToCheck = append(servicesToCheck, struct {
						namespace    string
						resourceName string
						key          string
						serviceType  ServiceType
						connStr      string
						configSource string
					}{
						namespace:    ns,
						resourceName: cm.Name,
						key:          key,
						serviceType:  serviceType,
						connStr:      connStr,
						configSource: fmt.Sprintf("configmap:%s/%s", cm.Name, key),
					})
					totalServices++
				}
			}
		}

		// Coletar de Secrets
		for _, secret := range secrets.Items {
			for key, value := range secret.Data {
				valueStr := string(value)
				serviceType, connStr := c._detectServiceType(valueStr)
				if serviceType != "" {
					servicesToCheck = append(servicesToCheck, struct {
						namespace    string
						resourceName string
						key          string
						serviceType  ServiceType
						connStr      string
						configSource string
					}{
						namespace:    ns,
						resourceName: secret.Name,
						key:          key,
						serviceType:  serviceType,
						connStr:      connStr,
						configSource: fmt.Sprintf("secret:%s/%s", secret.Name, key),
					})
					totalServices++
				}
			}
		}
	}

	// Agora verificar todos os services com progresso
	currentService := 0
	for _, svc := range servicesToCheck {
		currentService++

		// Publicar evento: testando conectividade
		if progressCallback != nil {
			progressCallback(svc.namespace, svc.resourceName,
				fmt.Sprintf("Testando %s conectividade via %s...", svc.serviceType, svc.configSource),
				StatusHealthy, currentService, totalServices)
		}

		health := c._Check(ctx, svc.namespace, svc.resourceName, svc.key, svc.serviceType, svc.connStr, timeout)
		health.ConfigSource = svc.configSource
		results = append(results, health)

		// Publicar resultado
		if progressCallback != nil {
			summary := c._getHealthSummary(health)
			progressCallback(svc.namespace, svc.resourceName,
				fmt.Sprintf("%s: %s", svc.configSource, summary), health.Status, currentService, totalServices)
		}
	}

	return results
}

// _getHealthSummary gera resumo compacto do health check
func (c *ServiceChecker) _getHealthSummary(health ServiceHealth) string {
	if health.Status == StatusHealthy {
		return fmt.Sprintf("Conectado (%dms)", health.LatencyMs)
	}
	if health.Status == StatusWarning {
		return fmt.Sprintf("Conectado mas lento (%dms)", health.LatencyMs)
	}
	// Para critical, usar mensagem completa
	return health.Message
}

// _detectServiceType detecta tipo de serviço por padrões de connection string
func (c *ServiceChecker) _detectServiceType(value string) (ServiceType, string) {
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

// _Check verifica conectividade de um serviço específico
func (c *ServiceChecker) _Check(ctx context.Context, namespace, resourceName, key string, serviceType ServiceType, connStr string, timeout int) ServiceHealth {
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
*/
