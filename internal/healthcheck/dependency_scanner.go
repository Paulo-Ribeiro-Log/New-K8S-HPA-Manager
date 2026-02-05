// Package healthcheck fornece scanner de dependências externas
package healthcheck

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
)

// DependencyType representa o tipo de dependência externa
type DependencyType string

const (
	DependencyRDS      DependencyType = "rds"
	DependencyKafka    DependencyType = "kafka"
	DependencyEventHub DependencyType = "eventhub"
	DependencyRedis    DependencyType = "redis"
	DependencyMongo    DependencyType = "mongodb"
	DependencySQL      DependencyType = "sql"
	DependencyStorage  DependencyType = "storage"
	DependencyAPI      DependencyType = "api"
	DependencyOther    DependencyType = "other"
)

// DependencyPattern define padrões para detectar dependências
type DependencyPattern struct {
	Type    DependencyType
	Pattern *regexp.Regexp
	Name    string
}

// ExternalDependency representa uma dependência externa encontrada
type ExternalDependency struct {
	// Identificação do serviço externo
	ServiceName string         `json:"service_name"` // ex: rdsh-regional01.dc.nova
	ServiceType DependencyType `json:"service_type"` // ex: rds

	// Onde foi encontrado
	Cluster    string `json:"cluster"`
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`

	// Fonte da descoberta
	SourceType string `json:"source_type"` // configmap, secret, env
	SourceName string `json:"source_name"` // nome do configmap/secret
	SourceKey  string `json:"source_key"`  // chave onde foi encontrado

	// Metadados
	FoundAt time.Time `json:"found_at"`
}

// DependencyScanResult representa o resultado de um scan
type DependencyScanResult struct {
	Cluster      string               `json:"cluster"`
	Namespaces   []string             `json:"namespaces"`
	Dependencies []ExternalDependency `json:"dependencies"`
	ScannedAt    time.Time            `json:"scanned_at"`
	Duration     int64                `json:"duration_ms"`

	// Estatísticas
	Stats DependencyStats `json:"stats"`
}

// DependencyStats contém estatísticas do scan
type DependencyStats struct {
	TotalDependencies  int            `json:"total_dependencies"`
	UniqueServices     int            `json:"unique_services"`
	ByType             map[string]int `json:"by_type"`
	DeploymentsScanned int            `json:"deployments_scanned"`
	ConfigMapsScanned  int            `json:"configmaps_scanned"`
	SecretsScanned     int            `json:"secrets_scanned"`
}

// DependencyScanner escaneia dependências externas em clusters K8s
type DependencyScanner struct {
	kubeManager *config.KubeConfigManager
	patterns    []DependencyPattern
	mu          sync.RWMutex

	// Cache de resultados
	cache     map[string]*DependencyScanResult
	cacheTTL  time.Duration
	cacheTime map[string]time.Time
}

// NewDependencyScanner cria um novo scanner de dependências
func NewDependencyScanner(kubeManager *config.KubeConfigManager) *DependencyScanner {
	scanner := &DependencyScanner{
		kubeManager: kubeManager,
		cache:       make(map[string]*DependencyScanResult),
		cacheTime:   make(map[string]time.Time),
		cacheTTL:    5 * time.Minute,
	}

	// Inicializar padrões de detecção
	scanner.patterns = []DependencyPattern{
		// RDS (PostgreSQL, MySQL, SQL Server) - inclui rds*, rdsh-*, rdsp-*
		{Type: DependencyRDS, Pattern: regexp.MustCompile(`(?i)\brds[a-z]*-[a-z0-9\-\.]+`), Name: "RDS Database"},
		{Type: DependencyRDS, Pattern: regexp.MustCompile(`(?i)\bpsg[a-z]*-[a-z0-9\-\.]+`), Name: "PostgreSQL"},
		{Type: DependencyRDS, Pattern: regexp.MustCompile(`(?i)\bpostgres[a-z]*-[a-z0-9\-\.]+`), Name: "PostgreSQL"},
		{Type: DependencyRDS, Pattern: regexp.MustCompile(`(?i)\bmysql[a-z]*-[a-z0-9\-\.]+`), Name: "MySQL"},

		// Kafka - inclui kfk*, kafka*
		{Type: DependencyKafka, Pattern: regexp.MustCompile(`(?i)\bkfk[a-z]*-[a-z0-9\-\.]+`), Name: "Kafka"},
		{Type: DependencyKafka, Pattern: regexp.MustCompile(`(?i)\bkafka[a-z]*-[a-z0-9\-\.]+`), Name: "Kafka"},

		// Event Hub - inclui evh*, eventhub*
		{Type: DependencyEventHub, Pattern: regexp.MustCompile(`(?i)\bevh[a-z]*-[a-z0-9\-\.]+`), Name: "Event Hub"},
		{Type: DependencyEventHub, Pattern: regexp.MustCompile(`(?i)\beventhub[a-z]*-[a-z0-9\-\.]+`), Name: "Event Hub"},
		{Type: DependencyEventHub, Pattern: regexp.MustCompile(`(?i)\.servicebus\.windows\.net`), Name: "Azure Service Bus"},

		// Redis - inclui redis*, rds* (redis), cache*
		{Type: DependencyRedis, Pattern: regexp.MustCompile(`(?i)\bredis[a-z]*-[a-z0-9\-\.]+`), Name: "Redis"},
		{Type: DependencyRedis, Pattern: regexp.MustCompile(`(?i)\bcache[a-z]*-[a-z0-9\-\.]+`), Name: "Cache"},
		{Type: DependencyRedis, Pattern: regexp.MustCompile(`(?i)\.redis\.cache\.windows\.net`), Name: "Azure Redis"},

		// MongoDB - inclui mdb*, mongo*
		{Type: DependencyMongo, Pattern: regexp.MustCompile(`(?i)\bmdb[a-z]*-[a-z0-9\-\.]+`), Name: "MongoDB"},
		{Type: DependencyMongo, Pattern: regexp.MustCompile(`(?i)\bmongo[a-z]*-[a-z0-9\-\.]+`), Name: "MongoDB"},
		{Type: DependencyMongo, Pattern: regexp.MustCompile(`(?i)\.mongo\.cosmos\.azure\.com`), Name: "Azure Cosmos MongoDB"},

		// SQL Server - inclui sql*, mssql*
		{Type: DependencySQL, Pattern: regexp.MustCompile(`(?i)\bsql[a-z]*-[a-z0-9\-\.]+`), Name: "SQL Server"},
		{Type: DependencySQL, Pattern: regexp.MustCompile(`(?i)\bmssql[a-z]*-[a-z0-9\-\.]+`), Name: "SQL Server"},
		{Type: DependencySQL, Pattern: regexp.MustCompile(`(?i)\.database\.windows\.net`), Name: "Azure SQL"},

		// Storage - Azure Blob, File, AWS S3
		{Type: DependencyStorage, Pattern: regexp.MustCompile(`(?i)\.blob\.core\.windows\.net`), Name: "Azure Blob Storage"},
		{Type: DependencyStorage, Pattern: regexp.MustCompile(`(?i)\.file\.core\.windows\.net`), Name: "Azure File Storage"},
		{Type: DependencyStorage, Pattern: regexp.MustCompile(`(?i)\bs3\.amazonaws\.com`), Name: "AWS S3"},
		{Type: DependencyStorage, Pattern: regexp.MustCompile(`(?i)\bstg[a-z]*-[a-z0-9\-\.]+`), Name: "Storage"},

		// APIs genéricas
		{Type: DependencyAPI, Pattern: regexp.MustCompile(`(?i)\bapi[a-z]*-[a-z0-9\-\.]+\.(dc|corp|internal)`), Name: "Internal API"},
	}

	return scanner
}

// Scan executa um scan de dependências em um cluster
func (s *DependencyScanner) Scan(ctx context.Context, cluster string, namespaces []string) (*DependencyScanResult, error) {
	startTime := time.Now()

	log.Info().
		Str("cluster", cluster).
		Strs("namespaces", namespaces).
		Msg("Starting dependency scan")

	// Verificar cache
	cacheKey := cluster + ":" + strings.Join(namespaces, ",")
	s.mu.RLock()
	if cached, ok := s.cache[cacheKey]; ok {
		if time.Since(s.cacheTime[cacheKey]) < s.cacheTTL {
			s.mu.RUnlock()
			log.Debug().Str("cluster", cluster).Msg("Returning cached dependency scan result")
			return cached, nil
		}
	}
	s.mu.RUnlock()

	// Obter cliente K8s
	client, err := s.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, err
	}

	result := &DependencyScanResult{
		Cluster:      cluster,
		Namespaces:   namespaces,
		Dependencies: []ExternalDependency{},
		ScannedAt:    startTime,
		Stats: DependencyStats{
			ByType: make(map[string]int),
		},
	}

	// Se nenhum namespace especificado, buscar todos
	if len(namespaces) == 0 {
		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, ns := range nsList.Items {
			// Ignorar namespaces de sistema
			if isSystemNamespace(ns.Name) {
				continue
			}
			namespaces = append(namespaces, ns.Name)
		}
		result.Namespaces = namespaces
	}

	// Mapa para rastrear serviços únicos
	uniqueServices := make(map[string]bool)

	// Escanear cada namespace
	for _, ns := range namespaces {
		// 1. Escanear ConfigMaps
		configMaps, err := client.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("Failed to list ConfigMaps")
			continue
		}
		result.Stats.ConfigMapsScanned += len(configMaps.Items)

		for _, cm := range configMaps.Items {
			deps := s.scanConfigMapOrSecret(cluster, ns, cm.Name, "configmap", cm.Data)
			for _, dep := range deps {
				result.Dependencies = append(result.Dependencies, dep)
				uniqueServices[dep.ServiceName] = true
				result.Stats.ByType[string(dep.ServiceType)]++
			}
		}

		// 2. Escanear Secrets (apenas nomes, não valores decodificados)
		secrets, err := client.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("Failed to list Secrets")
			continue
		}
		result.Stats.SecretsScanned += len(secrets.Items)

		for _, secret := range secrets.Items {
			// Ignorar secrets de sistema
			if strings.HasPrefix(secret.Name, "default-token-") ||
				strings.HasPrefix(secret.Name, "sh.helm.release") ||
				secret.Type == corev1.SecretTypeServiceAccountToken {
				continue
			}

			// Converter []byte para string para análise
			stringData := make(map[string]string)
			for k, v := range secret.Data {
				stringData[k] = string(v)
			}

			deps := s.scanConfigMapOrSecret(cluster, ns, secret.Name, "secret", stringData)
			for _, dep := range deps {
				result.Dependencies = append(result.Dependencies, dep)
				uniqueServices[dep.ServiceName] = true
				result.Stats.ByType[string(dep.ServiceType)]++
			}
		}

		// 3. Escanear Deployments (env vars)
		deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("Failed to list Deployments")
			continue
		}
		result.Stats.DeploymentsScanned += len(deployments.Items)

		for _, deploy := range deployments.Items {
			deps := s.scanDeploymentEnvVars(cluster, ns, deploy.Name, deploy.Spec.Template.Spec.Containers)
			for _, dep := range deps {
				result.Dependencies = append(result.Dependencies, dep)
				uniqueServices[dep.ServiceName] = true
				result.Stats.ByType[string(dep.ServiceType)]++
			}
		}
	}

	// Calcular estatísticas finais
	result.Stats.TotalDependencies = len(result.Dependencies)
	result.Stats.UniqueServices = len(uniqueServices)
	result.Duration = time.Since(startTime).Milliseconds()

	// Armazenar em cache
	s.mu.Lock()
	s.cache[cacheKey] = result
	s.cacheTime[cacheKey] = time.Now()
	s.mu.Unlock()

	log.Info().
		Str("cluster", cluster).
		Int("total_dependencies", result.Stats.TotalDependencies).
		Int("unique_services", result.Stats.UniqueServices).
		Int64("duration_ms", result.Duration).
		Msg("Dependency scan completed")

	return result, nil
}

// scanConfigMapOrSecret analisa um ConfigMap ou Secret em busca de dependências
func (s *DependencyScanner) scanConfigMapOrSecret(cluster, namespace, name, sourceType string, data map[string]string) []ExternalDependency {
	var deps []ExternalDependency
	foundServices := make(map[string]bool) // Evitar duplicatas no mesmo recurso

	for key, value := range data {
		for _, pattern := range s.patterns {
			matches := pattern.Pattern.FindAllString(value, -1)
			for _, match := range matches {
				// Normalizar o match (lowercase, trim)
				serviceName := strings.ToLower(strings.TrimSpace(match))

				// Evitar duplicatas
				dedupKey := serviceName + ":" + name
				if foundServices[dedupKey] {
					continue
				}
				foundServices[dedupKey] = true

				deps = append(deps, ExternalDependency{
					ServiceName: serviceName,
					ServiceType: pattern.Type,
					Cluster:     cluster,
					Namespace:   namespace,
					SourceType:  sourceType,
					SourceName:  name,
					SourceKey:   key,
					FoundAt:     time.Now(),
				})
			}
		}
	}

	return deps
}

// scanDeploymentEnvVars analisa variáveis de ambiente de um Deployment
func (s *DependencyScanner) scanDeploymentEnvVars(cluster, namespace, deploymentName string, containers []corev1.Container) []ExternalDependency {
	var deps []ExternalDependency
	foundServices := make(map[string]bool)

	for _, container := range containers {
		for _, env := range container.Env {
			// Ignorar variáveis sem valor direto (referências a secrets/configmaps já foram escaneadas)
			if env.Value == "" {
				continue
			}

			for _, pattern := range s.patterns {
				matches := pattern.Pattern.FindAllString(env.Value, -1)
				for _, match := range matches {
					serviceName := strings.ToLower(strings.TrimSpace(match))

					dedupKey := serviceName + ":" + deploymentName
					if foundServices[dedupKey] {
						continue
					}
					foundServices[dedupKey] = true

					deps = append(deps, ExternalDependency{
						ServiceName: serviceName,
						ServiceType: pattern.Type,
						Cluster:     cluster,
						Namespace:   namespace,
						Deployment:  deploymentName,
						SourceType:  "env",
						SourceName:  container.Name,
						SourceKey:   env.Name,
						FoundAt:     time.Now(),
					})
				}
			}
		}
	}

	return deps
}

// Search busca dependências por nome de serviço (busca reversa)
func (s *DependencyScanner) Search(ctx context.Context, clusters []string, query string) ([]ExternalDependency, error) {
	var results []ExternalDependency
	query = strings.ToLower(query)

	for _, cluster := range clusters {
		// Primeiro fazer scan (usa cache se disponível)
		scanResult, err := s.Scan(ctx, cluster, nil)
		if err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("Failed to scan cluster for search")
			continue
		}

		// Filtrar por query
		for _, dep := range scanResult.Dependencies {
			if strings.Contains(strings.ToLower(dep.ServiceName), query) {
				results = append(results, dep)
			}
		}
	}

	return results, nil
}

// GetAllDependencies retorna todas as dependências de todos os clusters (para export)
func (s *DependencyScanner) GetAllDependencies(ctx context.Context, clusters []string) ([]ExternalDependency, error) {
	var allDeps []ExternalDependency

	for _, cluster := range clusters {
		scanResult, err := s.Scan(ctx, cluster, nil)
		if err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("Failed to scan cluster")
			continue
		}

		allDeps = append(allDeps, scanResult.Dependencies...)
	}

	return allDeps, nil
}

// GetServiceUsage retorna todos os lugares onde um serviço específico é usado
func (s *DependencyScanner) GetServiceUsage(ctx context.Context, clusters []string, serviceName string) ([]ExternalDependency, error) {
	var results []ExternalDependency
	serviceName = strings.ToLower(serviceName)

	for _, cluster := range clusters {
		scanResult, err := s.Scan(ctx, cluster, nil)
		if err != nil {
			continue
		}

		for _, dep := range scanResult.Dependencies {
			if strings.ToLower(dep.ServiceName) == serviceName {
				results = append(results, dep)
			}
		}
	}

	return results, nil
}

// ClearCache limpa o cache de resultados
func (s *DependencyScanner) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]*DependencyScanResult)
	s.cacheTime = make(map[string]time.Time)
}

// isSystemNamespace verifica se é um namespace de sistema
func isSystemNamespace(ns string) bool {
	systemNamespaces := []string{
		"kube-system", "kube-public", "kube-node-lease",
		"istio-system", "istio-operator",
		"cert-manager", "ingress-nginx",
		"monitoring", "prometheus",
		"gatekeeper-system", "azure-arc",
		"calico-system", "tigera-operator",
	}

	for _, sysNs := range systemNamespaces {
		if ns == sysNs {
			return true
		}
	}

	return false
}
