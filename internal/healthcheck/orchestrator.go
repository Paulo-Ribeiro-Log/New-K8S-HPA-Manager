package healthcheck

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/web/sse"
)

// Orchestrator coordena todos os health checks
type Orchestrator struct {
	kubeManager       *config.KubeConfigManager
	deploymentChecker *DeploymentChecker
	serviceChecker    *ServiceChecker
	configChecker     *ConfigChecker
	storage           *HealthCheckStorage
	progressTracker   *sse.ProgressTracker
}

// NewOrchestrator cria um novo orchestrator
func NewOrchestrator(kubeManager *config.KubeConfigManager, progressTracker *sse.ProgressTracker, dbPath string) (*Orchestrator, error) {
	storage, err := NewHealthCheckStorage(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	return &Orchestrator{
		kubeManager:       kubeManager,
		deploymentChecker: NewDeploymentChecker(),
		serviceChecker:    NewServiceChecker(),
		configChecker:     NewConfigChecker(),
		storage:           storage,
		progressTracker:   progressTracker,
	}, nil
}

// ExecuteHealthCheck executa health check em um ou mais clusters
func (o *Orchestrator) ExecuteHealthCheck(ctx context.Context, sessionID string, req HealthCheckRequest) ([]*HealthCheckResult, error) {
	// Resolver clusters baseado em Environment ou Clusters
	clusters, err := o.resolveClusters(req)
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("session_id", sessionID).
		Str("environment", req.Environment).
		Strs("clusters", clusters).
		Int("cluster_count", len(clusters)).
		Msg("Starting health check")

	// Validação
	if len(clusters) == 0 {
		return nil, fmt.Errorf("no clusters specified or found for environment")
	}

	// Determinar paralelismo
	numWorkers := calculateWorkers(len(clusters), req.MaxParallel)

	log.Info().
		Int("num_workers", numWorkers).
		Int("total_clusters", len(clusters)).
		Msg("Worker pool configuration")

	// Caso especial: 1 cluster apenas (sem pool)
	if len(clusters) == 1 {
		result, err := o.executeClusterCheck(ctx, sessionID, clusters[0], req)
		if err != nil {
			return nil, err
		}
		return []*HealthCheckResult{result}, nil
	}

	// Múltiplos clusters: worker pool
	return o.executeMultiClusterCheck(ctx, sessionID, clusters, req, numWorkers)
}

// executeClusterCheck executa health check em um único cluster
func (o *Orchestrator) executeClusterCheck(ctx context.Context, sessionID, cluster string, req HealthCheckRequest) (*HealthCheckResult, error) {
	result := &HealthCheckResult{
		ID:        sessionID,
		Cluster:   cluster,
		StartedAt: time.Now(),
	}

	// Obter cliente Kubernetes
	client, err := o.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client for %s: %w", cluster, err)
	}

	// Determinar namespaces
	namespaces := req.Namespaces
	if len(namespaces) == 0 {
		namespaces, err = getAllNamespaces(ctx, client)
		if err != nil {
			return nil, fmt.Errorf("failed to get namespaces for %s: %w", cluster, err)
		}
	}

	// Executar checks em paralelo (dentro do cluster)
	var wg sync.WaitGroup
	var mu sync.Mutex

	totalPhases := 0
	if req.CheckDeployments {
		totalPhases++
	}
	if req.CheckServices {
		totalPhases++
	}
	if req.CheckConfigs {
		totalPhases++
	}
	currentPhase := 0

	// Check Deployments
	if req.CheckDeployments {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "deployments", "Verificando deployments...", currentPhase*100/totalPhases, StatusHealthy)

			deploymentResults := o.deploymentChecker.CheckAll(ctx, client, namespaces, req.Timeout)

			mu.Lock()
			result.DeploymentResults = deploymentResults
			mu.Unlock()

			currentPhase++
		}()
	}

	// Check Services
	if req.CheckServices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "services", "Testando conectividade de serviços...", currentPhase*100/totalPhases, StatusHealthy)

			serviceResults := o.serviceChecker.CheckAll(ctx, client, namespaces, req.Timeout)

			mu.Lock()
			result.ServiceResults = serviceResults
			mu.Unlock()

			currentPhase++
		}()
	}

	// Check Configs
	if req.CheckConfigs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "configs", "Validando ConfigMaps e Secrets...", currentPhase*100/totalPhases, StatusHealthy)

			configResults := o.configChecker.CheckAll(ctx, client, namespaces)

			mu.Lock()
			result.ConfigResults = configResults
			mu.Unlock()

			currentPhase++
		}()
	}

	// Aguardar conclusão
	wg.Wait()

	// Calcular resumo
	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	o.calculateSummary(result)

	// Publicar conclusão
	o.publishProgress(sessionID, cluster, "complete", "Health check concluído", 100, result.OverallStatus)

	// Salvar no histórico
	if err := o.storage.Save(ctx, result); err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("Failed to save health check result")
	}

	log.Info().
		Str("session_id", sessionID).
		Str("cluster", cluster).
		Int("total_checks", result.TotalChecks).
		Int("healthy", result.HealthyCount).
		Int("warning", result.WarningCount).
		Int("critical", result.CriticalCount).
		Msg("Health check completed")

	return result, nil
}

// executeMultiClusterCheck executa health check em múltiplos clusters com worker pool
func (o *Orchestrator) executeMultiClusterCheck(ctx context.Context, sessionID string, clusters []string, req HealthCheckRequest, numWorkers int) ([]*HealthCheckResult, error) {
	clusterChan := make(chan string, len(clusters))
	resultsChan := make(chan *HealthCheckResult, len(clusters))
	errorsChan := make(chan error, len(clusters))

	// Enfileirar clusters
	for _, cluster := range clusters {
		clusterChan <- cluster
	}
	close(clusterChan)

	// Iniciar workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for cluster := range clusterChan {
				log.Info().
					Int("worker_id", workerID).
					Str("cluster", cluster).
					Msg("Worker processing cluster")

				result, err := o.executeClusterCheck(ctx, sessionID, cluster, req)
				if err != nil {
					log.Error().
						Err(err).
						Int("worker_id", workerID).
						Str("cluster", cluster).
						Msg("Worker failed to process cluster")
					errorsChan <- fmt.Errorf("cluster %s: %w", cluster, err)
					continue
				}

				resultsChan <- result
			}
		}(i)
	}

	// Aguardar todos os workers
	wg.Wait()
	close(resultsChan)
	close(errorsChan)

	// Coletar resultados
	results := make([]*HealthCheckResult, 0, len(clusters))
	for result := range resultsChan {
		results = append(results, result)
	}

	// Verificar erros
	var errs []error
	for err := range errorsChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		log.Warn().
			Int("failed_clusters", len(errs)).
			Int("successful_clusters", len(results)).
			Msg("Some clusters failed health check")
	}

	return results, nil
}

// calculateSummary calcula resumo do health check
func (o *Orchestrator) calculateSummary(result *HealthCheckResult) {
	statusCount := map[HealthStatus]int{
		StatusHealthy:  0,
		StatusWarning:  0,
		StatusCritical: 0,
	}

	// Contar deployments
	for _, d := range result.DeploymentResults {
		statusCount[d.Status]++
	}

	// Contar services
	for _, s := range result.ServiceResults {
		statusCount[s.Status]++
	}

	// Contar configs
	for _, c := range result.ConfigResults {
		statusCount[c.Status]++
	}

	result.TotalChecks = len(result.DeploymentResults) + len(result.ServiceResults) + len(result.ConfigResults)
	result.HealthyCount = statusCount[StatusHealthy]
	result.WarningCount = statusCount[StatusWarning]
	result.CriticalCount = statusCount[StatusCritical]

	// Determinar status geral
	if result.CriticalCount > 0 {
		result.OverallStatus = StatusCritical
	} else if result.WarningCount > 0 {
		result.OverallStatus = StatusWarning
	} else {
		result.OverallStatus = StatusHealthy
	}
}

// publishProgress publica progresso via SSE
func (o *Orchestrator) publishProgress(sessionID, cluster, phase, message string, progress int, status HealthStatus) {
	if o.progressTracker == nil {
		return
	}

	// Converter para ProgressEvent do SSE
	event := sse.ProgressEvent{
		ID:        sessionID,
		Type:      phase,
		Phase:     string(status),
		Message:   fmt.Sprintf("[%s] %s", cluster, message),
		Progress:  float64(progress) / 100.0,
		Details:   cluster,
		Timestamp: time.Now(),
	}

	// Publicar para a sessão específica (não broadcast)
	o.progressTracker.SendToClient(sessionID, event)
}

// GetHistory retorna histórico de health checks
func (o *Orchestrator) GetHistory(ctx context.Context, cluster, namespace string) ([]*HealthCheckResult, error) {
	return o.storage.GetHistory(ctx, cluster, namespace, 50) // Últimos 50
}

// GetResult retorna resultado específico
func (o *Orchestrator) GetResult(ctx context.Context, id string) (*HealthCheckResult, error) {
	return o.storage.Get(ctx, id)
}

// getAllNamespaces obtém todos os namespaces do cluster
func getAllNamespaces(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	namespaceList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	namespaces := make([]string, 0, len(namespaceList.Items))
	for _, ns := range namespaceList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// calculateWorkers determina número de workers para o pool
func calculateWorkers(numClusters, maxParallel int) int {
	if numClusters == 1 {
		return 1
	}

	// Mínimo 2 workers para múltiplos clusters
	minWorkers := 2

	// Máximo baseado em CPUs disponíveis
	maxWorkers := runtime.NumCPU()

	// Se usuário especificou MaxParallel, usar como limite superior
	if maxParallel > 0 && maxParallel < maxWorkers {
		maxWorkers = maxParallel
	}

	// Número ideal: min(numClusters, maxWorkers)
	numWorkers := numClusters
	if numWorkers > maxWorkers {
		numWorkers = maxWorkers
	}

	// Garantir mínimo de 2
	if numWorkers < minWorkers {
		numWorkers = minWorkers
	}

	return numWorkers
}

// resolveClusters resolve lista de clusters baseado em Environment ou Clusters
func (o *Orchestrator) resolveClusters(req HealthCheckRequest) ([]string, error) {
	// Modo 1: Filtro por ambiente (prioritário)
	if req.Environment != "" {
		return o.filterClustersByEnvironment(req.Environment)
	}

	// Modo 2: Seleção manual
	if len(req.Clusters) > 0 {
		return req.Clusters, nil
	}

	return nil, fmt.Errorf("no clusters or environment specified")
}

// filterClustersByEnvironment filtra clusters disponíveis por ambiente
func (o *Orchestrator) filterClustersByEnvironment(environment string) ([]string, error) {
	// Obter todos os clusters do kubeconfig
	allClusters := o.kubeManager.ListContexts()

	if len(allClusters) == 0 {
		return nil, fmt.Errorf("no clusters available in kubeconfig")
	}

	// Filtrar por ambiente
	filtered := []string{}

	for _, cluster := range allClusters {
		clusterEnv := detectEnvironment(cluster)

		switch environment {
		case "all":
			filtered = append(filtered, cluster)
		case "prod":
			if clusterEnv == "prod" {
				filtered = append(filtered, cluster)
			}
		case "hlg":
			if clusterEnv == "hlg" {
				filtered = append(filtered, cluster)
			}
		default:
			return nil, fmt.Errorf("invalid environment: %s (must be 'prod', 'hlg', or 'all')", environment)
		}
	}

	log.Info().
		Str("environment", environment).
		Strs("filtered_clusters", filtered).
		Int("total_available", len(allClusters)).
		Msg("Filtered clusters by environment")

	return filtered, nil
}

// detectEnvironment detecta ambiente baseado no nome do cluster
func detectEnvironment(clusterName string) string {
	lowerName := strings.ToLower(clusterName)

	// Padrões de produção
	if strings.Contains(lowerName, "prod") || strings.Contains(lowerName, "prd") {
		return "prod"
	}

	// Padrões de homologação
	if strings.Contains(lowerName, "hlg") || strings.Contains(lowerName, "homolog") || strings.Contains(lowerName, "staging") || strings.Contains(lowerName, "stg") {
		return "hlg"
	}

	// Padrões de desenvolvimento (considera como hlg para segurança)
	if strings.Contains(lowerName, "dev") || strings.Contains(lowerName, "develop") {
		return "hlg"
	}

	// Se não detectar, assume produção (mais seguro mostrar tudo)
	return "prod"
}

// DeleteResult deleta um resultado específico de health check
func (o *Orchestrator) DeleteResult(ctx context.Context, resultID string) error {
	log.Info().Str("result_id", resultID).Msg("Deleting health check result")

	return o.storage.Delete(ctx, resultID)
}

// GetStats retorna estatísticas agregadas de health checks
func (o *Orchestrator) GetStats(ctx context.Context, cluster, daysStr string) (map[string]interface{}, error) {
	log.Info().
		Str("cluster", cluster).
		Str("days", daysStr).
		Msg("Fetching health check stats")

	return o.storage.GetStats(ctx, cluster, daysStr)
}
