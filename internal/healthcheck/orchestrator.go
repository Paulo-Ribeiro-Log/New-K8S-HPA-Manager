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
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/web/sse"
)

// Orchestrator coordena todos os health checks
type Orchestrator struct {
	kubeManager        *config.KubeConfigManager
	deploymentChecker  *DeploymentChecker
	serviceChecker     *ServiceChecker
	configChecker      *ConfigChecker
	eventChecker       *EventChecker              // ✅ Verificador de eventos K8s
	storage            *HealthCheckStorage
	progressTracker    *sse.ProgressTracker
	filterManager      *FilterManager         // ✅ Gerenciador de filtros
	deploymentRegistry *storage.DeploymentRegistry // ✅ Base de conhecimento de deployments
}

// NewOrchestrator cria um novo orchestrator
func NewOrchestrator(kubeManager *config.KubeConfigManager, progressTracker *sse.ProgressTracker, dbPath string, filtersConfigPath string) (*Orchestrator, error) {
	hcStorage, err := NewHealthCheckStorage(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// ✅ Inicializar FilterManager
	filterManager, err := NewFilterManager(filtersConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize filter manager: %w", err)
	}

	// ✅ Inicializar DeploymentRegistry (base de conhecimento)
	deploymentRegistry, err := storage.NewDeploymentRegistry()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize deployment registry; continuing without it")
		deploymentRegistry = nil // Continua sem registry (não é crítico)
	}

	return &Orchestrator{
		kubeManager:        kubeManager,
		deploymentChecker:  NewDeploymentChecker(),
		serviceChecker:     NewServiceChecker(),
		configChecker:      NewConfigChecker(),
		eventChecker:       NewEventChecker(),
		storage:            hcStorage,
		progressTracker:    progressTracker,
		filterManager:      filterManager,
		deploymentRegistry: deploymentRegistry,
	}, nil
}

// ExecuteHealthCheck executa health check em um ou mais clusters
func (o *Orchestrator) ExecuteHealthCheck(ctx context.Context, sessionID string, req HealthCheckRequest) ([]*HealthCheckResult, error) {
	// Resolver clusters baseado em Environment ou Clusters
	clusters, err := o.ResolveClusters(req)
	if err != nil {
		return nil, err
	}

	log.Info().
		Str("session_id", sessionID).
		Str("environment", req.Environment).
		Strs("clusters", clusters).
		Int("cluster_count", len(clusters)).
		Msg("Starting health check")

	// Log dos timeouts configurados
	log.Info().
		Int("timeout_deployments", req.GetTimeoutDeployments()).
		Int("timeout_services", req.GetTimeoutServices()).
		Int("timeout_configs", req.GetTimeoutConfigs()).
		Msg("Timeouts configuration")

	// ⏱️ Aguardar 500ms para garantir que cliente SSE conecte antes de publicar eventos
	// (Evita race condition: events publicados antes do cliente registrar no tracker)
	time.Sleep(500 * time.Millisecond)
	log.Debug().Str("session_id", sessionID).Msg("SSE client connection grace period completed")

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
		// ✅ Gerar sessionID com sufixo do cluster (mesmo padrão de multi-cluster)
		clusterSessionID := fmt.Sprintf("%s-%s", sessionID, clusters[0])
		result, err := o.executeClusterCheck(ctx, clusterSessionID, clusters[0], req)
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
	// Resetar circuit breaker de métricas para nova sessão/cluster
	o.deploymentChecker.ResetMetricsCircuitBreaker()

	result := &HealthCheckResult{
		ID:        sessionID,
		Cluster:   cluster,
		StartedAt: time.Now(),
		// ✅ Sempre inicializar arrays vazios (nunca null)
		DeploymentResults: []DeploymentHealth{},
		ServiceResults:    []ServiceHealth{},
		ConfigResults:     []ConfigHealth{},
		EventResults:      []EventHealth{},
	}

	// Obter cliente Kubernetes
	client, err := o.kubeManager.GetClient(cluster)
	if err != nil {
		// Publicar erro via SSE antes de retornar
		o.publishProgress(sessionID, cluster, "error", fmt.Sprintf("Falha ao conectar: %v", err), 0, StatusCritical)
		return nil, fmt.Errorf("failed to get kubernetes client for %s: %w", cluster, err)
	}

	metricsClient, err := o.kubeManager.GetMetricsClient(cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Falha ao inicializar metrics client; seguindo sem percentuais de uso")
	}

	// Determinar namespaces
	namespaces := req.Namespaces
	if len(namespaces) == 0 {
		o.publishProgress(sessionID, cluster, "init", "Buscando todos os namespaces do cluster...", 0, StatusHealthy)
		namespaces, err = getAllNamespaces(ctx, client)
		if err != nil {
			// Publicar erro via SSE antes de retornar
			o.publishProgress(sessionID, cluster, "error", fmt.Sprintf("Falha ao obter namespaces: %v", err), 0, StatusCritical)
			return nil, fmt.Errorf("failed to get namespaces for %s: %w", cluster, err)
		}
		o.publishProgress(sessionID, cluster, "init", fmt.Sprintf("%d namespace(s) encontrado(s): %v", len(namespaces), namespaces), 5, StatusHealthy)
	} else {
		o.publishProgress(sessionID, cluster, "init", fmt.Sprintf("Verificando %d namespace(s) especificado(s): %v", len(namespaces), namespaces), 5, StatusHealthy)
	}

	// Executar checks em paralelo (dentro do cluster)
	var wg sync.WaitGroup
	var mu sync.Mutex

	// ✅ Calcular faixas de progresso DINÂMICAS baseadas nos checks habilitados
	const (
		progressInit    = 5  // 0-5%: Inicialização
		progressSummary = 95 // 95-100%: Summary
		availableRange  = 90 // 5-95% disponível para dividir entre checks
	)

	// Contar quantos checks estão habilitados
	enabledChecks := 0
	if req.CheckDeployments {
		enabledChecks++
	}
	if req.CheckServices {
		enabledChecks++
	}
	if req.CheckConfigs {
		enabledChecks++
	}
	if req.CheckEvents {
		enabledChecks++
	}

	// Calcular quanto cada check vale (dividir 90% entre checks habilitados)
	rangePerCheck := availableRange / enabledChecks
	currentRangeStart := progressInit

	// Check Deployments
	if req.CheckDeployments {
		deploymentStart := currentRangeStart
		deploymentEnd := currentRangeStart + rangePerCheck
		currentRangeStart = deploymentEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "deployments", fmt.Sprintf("Iniciando verificação de Deployments em %d namespace(s)...", len(namespaces)), deploymentStart, StatusHealthy)

			// Callback para publicar progresso de cada deployment
			deploymentCallback := func(namespace, name, message string, status HealthStatus, current, total int) {
				// ✅ Aplicar filtro ANTES de publicar/salvar (evita eventos filtrados no banco)
				if req.ApplyFilters && o.filterManager != nil {
					if o.filterManager.ShouldFilter(ResourceDeployment, namespace, name, message) {
						return // ⛔ Não publicar nem salvar eventos filtrados
					}
				}

				// Calcular progresso proporcional dentro da faixa alocada para deployments
				// Exemplo: se deploymentStart=5 e deploymentEnd=35 (range=30):
				//   deployment 15/30 = 5 + (15/30 * 30) = 20%
				deploymentProgress := deploymentStart + int(float64(current)/float64(total)*float64(rangePerCheck))
				o.publishProgress(sessionID, cluster, "deployments", message, deploymentProgress, status)
			}

			deploymentResults := o.deploymentChecker.CheckAll(ctx, client, metricsClient, namespaces, req.GetTimeoutDeployments(), cluster, o.deploymentRegistry, deploymentCallback)

			// ✅ Aplicar filtros se habilitado
			if req.ApplyFilters && o.filterManager != nil {
				filteredResults := []DeploymentHealth{}
				for _, dep := range deploymentResults {
					if !o.filterManager.ShouldFilter(ResourceDeployment, dep.Namespace, dep.Name, dep.Message) {
						filteredResults = append(filteredResults, dep)
					}
				}
				deploymentResults = filteredResults
			}

			mu.Lock()
			result.DeploymentResults = deploymentResults
			mu.Unlock()

			o.publishProgress(sessionID, cluster, "deployments", fmt.Sprintf("%d deployment(s) verificado(s)", len(deploymentResults)), deploymentEnd, StatusHealthy)
		}()
	}

	// Check Services
	if req.CheckServices {
		servicesStart := currentRangeStart
		servicesEnd := currentRangeStart + rangePerCheck
		currentRangeStart = servicesEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "services", fmt.Sprintf("Iniciando testes de conectividade de serviços externos em %d namespace(s)...", len(namespaces)), servicesStart, StatusHealthy)

			// Callback para publicar progresso de cada service
			serviceCallback := func(namespace, name, message string, status HealthStatus, current, total int) {
				// ✅ Aplicar filtro ANTES de publicar/salvar (evita eventos filtrados no banco)
				if req.ApplyFilters && o.filterManager != nil {
					if o.filterManager.ShouldFilter(ResourceService, namespace, name, message) {
						return // ⛔ Não publicar nem salvar eventos filtrados
					}
				}

				// Calcular progresso proporcional dentro da faixa alocada para services
				serviceProgress := servicesStart + int(float64(current)/float64(total)*float64(rangePerCheck))
				o.publishProgress(sessionID, cluster, "services", message, serviceProgress, status)
			}

			serviceResults := o.serviceChecker.CheckAll(ctx, client, namespaces, req.GetTimeoutServices(), serviceCallback)

			mu.Lock()
			result.ServiceResults = serviceResults
			mu.Unlock()

			o.publishProgress(sessionID, cluster, "services", fmt.Sprintf("%d serviço(s) testado(s)", len(serviceResults)), servicesEnd, StatusHealthy)
		}()
	}

	// Check Configs
	if req.CheckConfigs {
		configsStart := currentRangeStart
		configsEnd := currentRangeStart + rangePerCheck
		currentRangeStart = configsEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "configs", fmt.Sprintf("Iniciando validação de ConfigMaps e Secrets em %d namespace(s)...", len(namespaces)), configsStart, StatusHealthy)

			// Callback para publicar progresso de cada config
			configCallback := func(namespace, name, message string, status HealthStatus, current, total int) {
				// ✅ Aplicar filtro ANTES de publicar/salvar (evita eventos filtrados no banco)
				if req.ApplyFilters && o.filterManager != nil {
					if o.filterManager.ShouldFilter(ResourceConfigMap, namespace, name, message) {
						return // ⛔ Não publicar nem salvar eventos filtrados
					}
				}

				// Calcular progresso proporcional dentro da faixa alocada para configs
				configProgress := configsStart + int(float64(current)/float64(total)*float64(rangePerCheck))
				o.publishProgress(sessionID, cluster, "configs", message, configProgress, status)
			}

			configResults := o.configChecker.CheckAll(ctx, client, namespaces, req.GetTimeoutConfigs(), false, configCallback) // ✅ Passar false - filtros são aplicados abaixo

			// ✅ Aplicar filtros se habilitado
			if req.ApplyFilters && o.filterManager != nil {
				filteredResults := []ConfigHealth{}
				for _, cfg := range configResults {
					if !o.filterManager.ShouldFilter(cfg.ResourceType, cfg.Namespace, cfg.Name, cfg.Message) {
						filteredResults = append(filteredResults, cfg)
					}
				}
				configResults = filteredResults
			}

			mu.Lock()
			result.ConfigResults = configResults
			mu.Unlock()

			o.publishProgress(sessionID, cluster, "configs", fmt.Sprintf("%d configuração(ões) validada(s)", len(configResults)), configsEnd, StatusHealthy)
		}()
	}

	// Check Events
	if req.CheckEvents {
		eventsStart := currentRangeStart
		eventsEnd := currentRangeStart + rangePerCheck
		currentRangeStart = eventsEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "events", fmt.Sprintf("Iniciando verificação de eventos K8s em %d namespace(s)...", len(namespaces)), eventsStart, StatusHealthy)

			// Callback para publicar progresso de cada namespace
			eventCallback := func(namespace, name, message string, status HealthStatus, current, total int) {
				// Calcular progresso proporcional dentro da faixa alocada para events
				eventProgress := eventsStart + int(float64(current)/float64(total)*float64(rangePerCheck))
				o.publishProgress(sessionID, cluster, "events", message, eventProgress, status)
			}

			eventResults := o.eventChecker.CheckAll(ctx, client, namespaces, req.GetTimeoutEvents(), eventCallback)

			mu.Lock()
			result.EventResults = eventResults
			mu.Unlock()

			// Publicar resumo de eventos
			criticalEvents := o.eventChecker.GetCriticalCount(eventResults)
			warningEvents := o.eventChecker.GetWarningCount(eventResults)
			if criticalEvents > 0 {
				o.publishProgress(sessionID, cluster, "events", fmt.Sprintf("%d evento(s) crítico(s) encontrado(s)", criticalEvents), eventsEnd, StatusCritical)
			} else if warningEvents > 0 {
				o.publishProgress(sessionID, cluster, "events", fmt.Sprintf("%d evento(s) de aviso encontrado(s)", warningEvents), eventsEnd, StatusWarning)
			} else {
				o.publishProgress(sessionID, cluster, "events", "Nenhum evento crítico encontrado", eventsEnd, StatusHealthy)
			}
		}()
	}

	// Aguardar conclusão
	wg.Wait()

	// Calcular resumo
	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	o.calculateSummary(result)

	// Publicar resumo detalhado
	o.publishProgress(sessionID, cluster, "summary", fmt.Sprintf("Total: %d checks realizados", result.TotalChecks), 95, result.OverallStatus)
	o.publishProgress(sessionID, cluster, "summary", fmt.Sprintf("Saudáveis: %d", result.HealthyCount), 96, StatusHealthy)

	// Publicar detalhes dos problemas CRÍTICOS (limite de 10 para não sobrecarregar UI)
	if result.CriticalCount > 0 {
		o.publishProgress(sessionID, cluster, "summary", fmt.Sprintf("Críticos: %d", result.CriticalCount), 97, StatusCritical)

		criticalShown := 0
		maxCritical := 10

		// Publicar primeiros 10 deployments críticos
		for _, d := range result.DeploymentResults {
			if d.Status == StatusCritical && criticalShown < maxCritical {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Crítico: Deployment %s/%s - %s", d.Namespace, d.Name, d.Message), 97, StatusCritical)
				criticalShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar primeiros serviços críticos (se ainda tiver espaço)
		for _, s := range result.ServiceResults {
			if s.Status == StatusCritical && criticalShown < maxCritical {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Crítico: Service %s/%s - %s", s.Namespace, s.Name, s.Message), 97, StatusCritical)
				criticalShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar primeiros configs críticos (se ainda tiver espaço)
		for _, c := range result.ConfigResults {
			if c.Status == StatusCritical && criticalShown < maxCritical {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Crítico: Config %s/%s - %s", c.Namespace, c.Name, c.Message), 97, StatusCritical)
				criticalShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Avisar se há mais problemas
		if result.CriticalCount > maxCritical {
			o.publishProgress(sessionID, cluster, "summary",
				fmt.Sprintf("... e mais %d problema(s) crítico(s). Veja abaixo nos resultados completos.", result.CriticalCount-criticalShown), 97, StatusCritical)
		}
	}

	// Publicar detalhes dos problemas de AVISO (limite de 10)
	if result.WarningCount > 0 {
		o.publishProgress(sessionID, cluster, "summary", fmt.Sprintf("Avisos: %d", result.WarningCount), 98, StatusWarning)

		warningShown := 0
		maxWarning := 10

		// Publicar primeiros 10 deployments warning
		for _, d := range result.DeploymentResults {
			if d.Status == StatusWarning && warningShown < maxWarning {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Aviso: Deployment %s/%s - %s", d.Namespace, d.Name, d.Message), 98, StatusWarning)
				warningShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar primeiros serviços warning
		for _, s := range result.ServiceResults {
			if s.Status == StatusWarning && warningShown < maxWarning {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Aviso: Service %s/%s - %s", s.Namespace, s.Name, s.Message), 98, StatusWarning)
				warningShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar primeiros configs warning
		for _, c := range result.ConfigResults {
			if c.Status == StatusWarning && warningShown < maxWarning {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Aviso: Config %s/%s - %s", c.Namespace, c.Name, c.Message), 98, StatusWarning)
				warningShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Avisar se há mais problemas
		if result.WarningCount > maxWarning {
			o.publishProgress(sessionID, cluster, "summary",
				fmt.Sprintf("... e mais %d aviso(s). Veja abaixo nos resultados completos.", result.WarningCount-warningShown), 98, StatusWarning)
		}
	}

	o.publishProgress(sessionID, cluster, "summary", fmt.Sprintf("Duração: %dms", result.Duration), 99, StatusHealthy)

	// Publicar conclusão
	o.publishProgress(sessionID, cluster, "complete", fmt.Sprintf("Concluído - Status: %s", result.OverallStatus), 100, result.OverallStatus)

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

	// Log do status do circuit breaker de métricas (observabilidade)
	if o.deploymentChecker.IsMetricsCircuitOpen() {
		log.Warn().
			Str("session_id", sessionID).
			Str("cluster", cluster).
			Int("fail_count", o.deploymentChecker.GetMetricsFailCount()).
			Msg("Circuit breaker de métricas estava ABERTO durante esta sessão - metrics-server pode estar indisponível")
	}

	return result, nil
}

// executeMultiClusterCheck executa health check em múltiplos clusters com worker pool
// Retorna um map de cluster -> sessionID gerado para cada cluster
func (o *Orchestrator) executeMultiClusterCheck(ctx context.Context, sessionID string, clusters []string, req HealthCheckRequest, numWorkers int) ([]*HealthCheckResult, error) {
	type clusterJob struct {
		cluster   string
		sessionID string
	}

	jobsChan := make(chan clusterJob, len(clusters))
	resultsChan := make(chan *HealthCheckResult, len(clusters))
	errorsChan := make(chan error, len(clusters))

	// Gerar sessionID único por cluster e enfileirar jobs
	for _, cluster := range clusters {
		clusterSessionID := fmt.Sprintf("%s-%s", sessionID, cluster) // sessionID-base + cluster name
		jobsChan <- clusterJob{cluster: cluster, sessionID: clusterSessionID}
	}
	close(jobsChan)

	// Iniciar workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for job := range jobsChan {
				log.Info().
					Int("worker_id", workerID).
					Str("cluster", job.cluster).
					Str("session_id", job.sessionID).
					Msg("Worker processing cluster")

				result, err := o.executeClusterCheck(ctx, job.sessionID, job.cluster, req)
				if err != nil {
					log.Error().
						Err(err).
						Int("worker_id", workerID).
						Str("cluster", job.cluster).
						Msg("Worker failed to process cluster")
					errorsChan <- fmt.Errorf("cluster %s: %w", job.cluster, err)
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

// GetClusterSessionMapping retorna mapeamento cluster -> sessionID para múltiplos clusters
func (o *Orchestrator) GetClusterSessionMapping(baseSessionID string, clusters []string) map[string]string {
	mapping := make(map[string]string)
	for _, cluster := range clusters {
		mapping[cluster] = fmt.Sprintf("%s-%s", baseSessionID, cluster)
	}
	return mapping
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

	// Contar events
	for _, e := range result.EventResults {
		statusCount[e.Status]++
	}

	result.TotalChecks = len(result.DeploymentResults) + len(result.ServiceResults) + len(result.ConfigResults) + len(result.EventResults)
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

// publishProgress publica progresso via SSE e salva no banco
func (o *Orchestrator) publishProgress(sessionID, cluster, phase, message string, progress int, status HealthStatus) {
	timestamp := time.Now()

	// ✅ Publicar via SSE
	if o.progressTracker != nil {
		// Converter para ProgressEvent do SSE
		event := sse.ProgressEvent{
			ID:        sessionID,
			Type:      phase,
			Phase:     "in_progress",  // Fase da operação (sempre "in_progress" para health checking)
			Status:    string(status), // ✅ CORRIGIDO: Status de saúde (healthy/warning/critical)
			Message:   fmt.Sprintf("[%s] %s", cluster, message),
			Progress:  float64(progress), // 0-100 direto (frontend espera 0-100)
			Details:   cluster,
			Timestamp: timestamp,
		}

		// Publicar para a sessão específica (não broadcast)
		o.progressTracker.SendToClient(sessionID, event)
	}

	// 🆕 Salvar evento no banco de dados (persistência)
	if o.storage != nil {
		ctx := context.Background()
		progressEvent := &ProgressEvent{
			SessionID: sessionID,
			Cluster:   cluster,
			Type:      phase,
			Phase:     "in_progress",
			Message:   message, // Mensagem sem prefixo [cluster] (já salvamos cluster separadamente)
			Progress:  progress,
			Status:    string(status),
			Timestamp: timestamp,
		}

		if err := o.storage.SaveEvent(ctx, progressEvent); err != nil {
			log.Error().Err(err).
				Str("session_id", sessionID).
				Str("cluster", cluster).
				Str("phase", phase).
				Msg("Failed to save progress event to database")
			// NÃO BLOQUEAR - apenas logar erro
		}
	}
}

// GetHistory retorna histórico de health checks
func (o *Orchestrator) GetHistory(ctx context.Context, cluster, namespace string) ([]*HealthCheckResult, error) {
	return o.storage.GetHistory(ctx, cluster, namespace, 50) // Últimos 50
}

// GetResult retorna resultado específico
func (o *Orchestrator) GetResult(ctx context.Context, id string) (*HealthCheckResult, error) {
	return o.storage.Get(ctx, id)
}

// DeleteResult deleta um resultado específico
func (o *Orchestrator) DeleteResult(ctx context.Context, id string) error {
	return o.storage.Delete(ctx, id)
}

// GetStats retorna estatísticas agregadas
func (o *Orchestrator) GetStats(ctx context.Context, cluster, days string) (map[string]interface{}, error) {
	return o.storage.GetStats(ctx, cluster, days)
}

// Storage retorna o storage para acesso direto (usado por handlers)
func (o *Orchestrator) Storage() *HealthCheckStorage {
	return o.storage
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

// ResolveClusters resolve lista de clusters baseado em Environment ou Clusters (método público)
func (o *Orchestrator) ResolveClusters(req HealthCheckRequest) ([]string, error) {
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

// GetFilterManager retorna o gerenciador de filtros
func (o *Orchestrator) GetFilterManager() *FilterManager {
	return o.filterManager
}
