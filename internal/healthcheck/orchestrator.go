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
	"k8s-hpa-manager/internal/metrics"
	"k8s-hpa-manager/internal/monitoring/discovery"
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/web/sse"
)

// Orchestrator coordena todos os health checks
type Orchestrator struct {
	kubeManager        *config.KubeConfigManager
	deploymentChecker  *DeploymentChecker
	serviceChecker     *ServiceChecker
	configChecker      *ConfigChecker
	eventChecker       *EventChecker           // ✅ Verificador de eventos K8s
	hpaChecker         *HPAChecker             // ✅ Verificador de HPAs
	pvChecker          *PVChecker              // ✅ Verificador de PVCs
	nodeChecker        *NodeChecker            // ✅ Verificador de capacidade/utilização dos nós
	dynatraceChecker   *DynatraceChecker       // ✅ Verificador de problems Dynatrace
	oneAgentChecker    *OneAgentSignalsChecker // ✅ Varredura OneAgent por threshold
	nodePoolStore      *storage.NodePoolRegistryStore
	depRegistry        *storage.DependencyRegistry
	storage            *HealthCheckStorage
	progressTracker    *sse.ProgressTracker
	filterManager      *FilterManager              // ✅ Gerenciador de filtros
	deploymentRegistry *storage.DeploymentRegistry // ✅ Base de conhecimento de deployments
	baseDir            string                      // ~/.k8s-hpa-manager — usado pelo SpinnakerEnricher (spinnaker_config.json)
}

// NewOrchestrator cria um novo orchestrator
func NewOrchestrator(kubeManager *config.KubeConfigManager, progressTracker *sse.ProgressTracker, dbPath string, filtersConfigPath string, baseDir string) (*Orchestrator, error) {
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
		hpaChecker:         NewHPAChecker(),
		pvChecker:          NewPVChecker(),
		nodeChecker:        NewNodeChecker(),
		dynatraceChecker:   NewDynatraceChecker(),
		oneAgentChecker:    NewOneAgentSignalsChecker(),
		storage:            hcStorage,
		progressTracker:    progressTracker,
		filterManager:      filterManager,
		deploymentRegistry: deploymentRegistry,
		baseDir:            baseDir,
	}, nil
}

// SetStores injeta os stores de node pool e dependências para enriquecimento dos OneAgent Signals.
// Chamado pelo server após criar o orchestrator.
func (o *Orchestrator) SetStores(nodePoolStore *storage.NodePoolRegistryStore, depRegistry *storage.DependencyRegistry) {
	o.nodePoolStore = nodePoolStore
	o.depRegistry = depRegistry
}

// nonPrdEnvMarkers são marcadores de ambiente não-produtivo reconhecidos em nomes de namespace.
var nonPrdEnvMarkers = []string{"hlg", "sit", "stg", "hml", "uat", "dev"}

// nsTokens divide o nome do namespace em tokens pelo separador hífen.
// Ex: "calculo-de-fretes-prd" → ["calculo", "de", "fretes", "prd"]
func nsTokens(nsName string) []string {
	return strings.FieldsFunc(strings.ToLower(nsName), func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
}

// namespaceEnvToken retorna o marcador de ambiente presente nos tokens do namespace.
// "calculo-de-fretes-hlg" → "hlg", "myapp-prd" → "prd", "kube-system" → ""
func namespaceEnvToken(nsName string) string {
	allEnvs := append([]string{"prd", "prod"}, nonPrdEnvMarkers...)
	for _, tok := range nsTokens(nsName) {
		for _, env := range allEnvs {
			if tok == env {
				return env
			}
		}
	}
	return ""
}

// FindNamespaceByKeywords lista namespaces do cluster e retorna o melhor match por pontuação.
// envHint é o marcador de ambiente esperado (ex: "prd", "hlg", "sit") derivado do cluster.
// Regras de filtragem por token exato:
//   - envHint prd/prod → rejeita namespaces com marcadores não-prd (hlg, sit, stg…)
//   - envHint não-prd (hlg, sit…) → rejeita namespaces com marcadores prd/prod
//   - Bônus de +5 no score quando o env do namespace bate exatamente com o envHint
//     (desempata "calculo-de-fretes-hlg" sobre "calculo-de-fretes-sit" quando cluster é hlg)
func (o *Orchestrator) FindNamespaceByKeywords(ctx context.Context, cluster string, keywords []string, envHint string) string {
	if len(keywords) == 0 {
		return ""
	}
	client, err := o.kubeManager.GetClient(cluster)
	if err != nil {
		log.Debug().Err(err).Str("cluster", cluster).Msg("FindNamespaceByKeywords: falha ao obter client K8s")
		return ""
	}
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Debug().Err(err).Str("cluster", cluster).Msg("FindNamespaceByKeywords: falha ao listar namespaces")
		return ""
	}

	isPrdHint := envHint == "prd" || envHint == "prod"

	bestNS := ""
	bestScore := 0
	for _, ns := range nsList.Items {
		nsEnv := namespaceEnvToken(ns.Name)

		// Filtrar por compatibilidade de ambiente
		if envHint != "" {
			if isPrdHint {
				// Procurando prd: rejeitar namespaces não-prd
				for _, m := range nonPrdEnvMarkers {
					if nsEnv == m {
						goto nextNS
					}
				}
			} else {
				// Procurando não-prd: rejeitar namespaces de produção
				if nsEnv == "prd" || nsEnv == "prod" {
					continue
				}
			}
		}

		{
			nsName := strings.ToLower(ns.Name)
			score := 0
			for _, kw := range keywords {
				if strings.Contains(nsName, kw) {
					score++
				}
			}
			if score == 0 {
				continue
			}
			// Bônus: env do namespace bate exatamente com o hint
			if nsEnv == envHint {
				score += 5
			}
			if score > bestScore {
				bestScore = score
				bestNS = ns.Name
			}
		}
	nextNS:
	}

	if bestNS != "" {
		log.Info().
			Str("cluster", cluster).
			Str("namespace", bestNS).
			Int("score", bestScore).
			Str("env_hint", envHint).
			Msg("FindNamespaceByKeywords: namespace identificado por keyword")
	}
	return bestNS
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

// buildTriageSources monta as fontes de triagem disponíveis nesta instância — Dynatrace e
// Prometheus na Fase 1 (Zabbix entra como 3ª fonte na Fase 3, condicional a
// ZABBIX-INTEGRATION-PLAN.md, ver HEALTHCHECK-TRIAGE-MODE-PLAN.md seção 3). Credenciais Dynatrace
// vêm da mesma request (preenchidas pelo handler a partir dos tokens do usuário, igual ao check
// tradicional) — a fonte de triagem não depende de req.CheckDynatrace estar marcado, só das
// credenciais estarem presentes (DynatraceTargetSource.Resolve trata URL/token vazios sozinho).
func (o *Orchestrator) buildTriageSources(req HealthCheckRequest) []TargetSource {
	return []TargetSource{
		NewDynatraceTargetSource(o.dynatraceChecker, req.DynatraceURL, req.DynatraceToken, req.DynatraceTagFilter, req.GetTimeoutDynatrace()),
		NewPrometheusAlertsTargetSource(),
	}
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
		DynatraceResults:  []DynatraceHealth{},
		NodeResults:       []NodeHealth{},
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

	// Modo Triagem (HEALTHCHECK-TRIAGE-MODE-PLAN.md, Fase 1): resolve o escopo de namespaces via
	// fontes externas (Dynatrace/Prometheus) ANTES de decidir "todos os namespaces" abaixo.
	// triageDecided=true significa que o escopo já é autoritativo — mesmo quando vier vazio (bom
	// sinal: nenhuma fonte disponível sinalizou problema) — e não deve cair no fallback de
	// getAllNamespaces logo abaixo, que existe só pra "usuário não filtrou nada".
	triageDecided := false
	if req.TriageMode {
		o.publishProgress(sessionID, cluster, "init", "Modo Triagem: resolvendo escopo via Dynatrace/Prometheus...", 0, StatusHealthy)
		resolution := resolveTriageTargets(ctx, cluster, o.buildTriageSources(req))
		summary := &TriageSummary{Enabled: true, Sources: resolution.Sources, Reasons: resolution.Reasons}
		if resolution.AnyAvailable {
			triageDecided = true
			namespaces = intersectOrUse(req.Namespaces, resolution.Namespaces)
			summary.Namespaces = namespaces
			if len(namespaces) == 0 {
				o.publishProgress(sessionID, cluster, "init", "Modo Triagem: nenhuma fonte sinalizou problema — cluster aparenta saudável, varredura reduzida", 3, StatusHealthy)
			} else {
				o.publishProgress(sessionID, cluster, "init", fmt.Sprintf("Modo Triagem: %d namespace(s) sinalizado(s): %v", len(namespaces), namespaces), 3, StatusHealthy)
			}
		} else {
			summary.FellBackToFull = true
			summary.FallbackReason = "nenhuma fonte de triagem disponível para este cluster"
			o.publishProgress(sessionID, cluster, "init", "Modo Triagem: nenhuma fonte disponível — caindo para Varredura Completa", 3, StatusWarning)
		}
		result.TriageSummary = summary
	}

	if len(namespaces) == 0 && !triageDecided {
		o.publishProgress(sessionID, cluster, "init", "Buscando todos os namespaces do cluster...", 0, StatusHealthy)
		namespaces, err = getAllNamespaces(ctx, client)
		if err != nil {
			// Publicar erro via SSE antes de retornar
			o.publishProgress(sessionID, cluster, "error", fmt.Sprintf("Falha ao obter namespaces: %v", err), 0, StatusCritical)
			return nil, fmt.Errorf("failed to get namespaces for %s: %w", cluster, err)
		}
		o.publishProgress(sessionID, cluster, "init", fmt.Sprintf("%d namespace(s) encontrado(s): %v", len(namespaces), namespaces), 5, StatusHealthy)
	} else if triageDecided && len(namespaces) == 0 {
		o.publishProgress(sessionID, cluster, "init", "Nenhum namespace no escopo da triagem — pulando varredura detalhada", 5, StatusHealthy)
	} else {
		o.publishProgress(sessionID, cluster, "init", fmt.Sprintf("Verificando %d namespace(s): %v", len(namespaces), namespaces), 5, StatusHealthy)
	}

	// Comparação de uso real (P95 histórico) vs. request configurado dos deployments, via
	// Prometheus — opcional, condicionada a CheckResourceHistory + endpoint alcançável pro
	// cluster (mesmo padrão de auto-descoberta usado no FinOps). Resolvido uma única vez aqui
	// (não dentro do goroutine de deployments) porque a checagem de alcançabilidade já é uma
	// chamada de rede síncrona — evita recriar o client por deployment.
	var resourceEnricher *ResourceEnricher
	if req.CheckDeployments && req.CheckResourceHistory {
		promURL := discovery.GetPrometheusURL(cluster)
		if promURL != "" && discovery.IsEndpointAvailable(promURL) {
			enricher, err := NewResourceEnricher(promURL, 48*time.Hour)
			if err != nil {
				log.Warn().Err(err).Str("cluster", cluster).Str("prometheus_url", promURL).
					Msg("[HealthCheck] Falha ao criar ResourceEnricher — seguindo sem comparação de uso real")
			} else {
				resourceEnricher = enricher
			}
		} else {
			log.Debug().Str("cluster", cluster).Str("prometheus_url", promURL).
				Msg("[HealthCheck] Prometheus não alcançável — comparação de uso real desabilitada para este cluster")
		}
	}

	// Rollback recente no Spinnaker como sinal extra de risco (seção 9 item 7 do
	// SPINNAKER-INTEGRATION-PLAN.md) — opcional, condicionada a CheckSpinnakerRollback + projeto
	// Spinnaker configurado no perfil do usuário + cluster com ambiente Spinnaker reconhecido
	// (sufixo -hlg/-prd). Resolvido uma única vez por cluster (login + busca de execuções de
	// todas as applications do projeto, ~7-10s), mesmo padrão de custo do resourceEnricher acima
	// — nunca por deployment.
	var spinnakerEnricher *SpinnakerEnricher
	if req.CheckDeployments && req.CheckSpinnakerRollback {
		enricher, err := NewSpinnakerEnricher(ctx, o.baseDir, cluster)
		if err != nil {
			log.Debug().Err(err).Str("cluster", cluster).
				Msg("[HealthCheck] Spinnaker indisponível — sinal de rollback recente desabilitado para este cluster")
		} else {
			spinnakerEnricher = enricher
			log.Debug().Str("cluster", cluster).Int("applications", len(enricher.executionsByApp)).
				Msg("[HealthCheck] SpinnakerEnricher pronto — sinal de rollback recente habilitado para este cluster")
		}
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
	if req.CheckHPAs {
		enabledChecks++
	}
	if req.CheckPVCs {
		enabledChecks++
	}
	if req.CheckNodes {
		enabledChecks++
	}
	if req.CheckDynatrace && req.DynatraceURL != "" {
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

			deploymentResults := o.deploymentChecker.CheckAll(ctx, client, metricsClient, namespaces, req.GetTimeoutDeployments(), cluster, o.deploymentRegistry, resourceEnricher, spinnakerEnricher, deploymentCallback)

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

	// Check HPAs
	if req.CheckHPAs {
		hpasStart := currentRangeStart
		hpasEnd := currentRangeStart + rangePerCheck
		currentRangeStart = hpasEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "hpas", fmt.Sprintf("Iniciando verificação de HPAs em %d namespace(s)...", len(namespaces)), hpasStart, StatusHealthy)

			// Callback para publicar progresso de cada HPA
			hpaCallback := func(namespace, name, message string, status HealthStatus, current, total int) {
				// Calcular progresso proporcional dentro da faixa alocada para HPAs
				hpaProgress := hpasStart + int(float64(current)/float64(total)*float64(rangePerCheck))
				o.publishProgress(sessionID, cluster, "hpas", message, hpaProgress, status)
			}

			hpaResults := o.hpaChecker.CheckAll(ctx, client, namespaces, req.GetTimeoutHPAs(), req.ApplyFilters, hpaCallback)

			mu.Lock()
			result.HPAResults = hpaResults
			mu.Unlock()

			// Publicar resumo de HPAs
			criticalHPAs := 0
			warningHPAs := 0
			for _, h := range hpaResults {
				if h.Status == StatusCritical {
					criticalHPAs++
				} else if h.Status == StatusWarning {
					warningHPAs++
				}
			}

			if criticalHPAs > 0 {
				o.publishProgress(sessionID, cluster, "hpas", fmt.Sprintf("%d HPA(s) com problemas criticos encontrado(s)", criticalHPAs), hpasEnd, StatusCritical)
			} else if warningHPAs > 0 {
				o.publishProgress(sessionID, cluster, "hpas", fmt.Sprintf("%d HPA(s) com avisos encontrado(s)", warningHPAs), hpasEnd, StatusWarning)
			} else {
				o.publishProgress(sessionID, cluster, "hpas", fmt.Sprintf("%d HPA(s) verificado(s) - todos saudaveis", len(hpaResults)), hpasEnd, StatusHealthy)
			}
		}()
	}

	// Check PVCs
	if req.CheckPVCs {
		pvcsStart := currentRangeStart
		pvcsEnd := currentRangeStart + rangePerCheck
		currentRangeStart = pvcsEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "pvcs", fmt.Sprintf("Iniciando verificação de PVCs em %d namespace(s)...", len(namespaces)), pvcsStart, StatusHealthy)

			// Callback para publicar progresso de cada PVC
			pvcCallback := func(namespace, name, message string, status HealthStatus, current, total int) {
				// Calcular progresso proporcional dentro da faixa alocada para PVCs
				pvcProgress := pvcsStart + int(float64(current)/float64(total)*float64(rangePerCheck))
				o.publishProgress(sessionID, cluster, "pvcs", message, pvcProgress, status)
			}

			pvcResults := o.pvChecker.CheckAll(ctx, client, namespaces, req.GetTimeoutPVCs(), req.ApplyFilters, pvcCallback)

			mu.Lock()
			result.PVCResults = pvcResults
			mu.Unlock()

			// Publicar resumo de PVCs
			criticalPVCs := 0
			warningPVCs := 0
			for _, p := range pvcResults {
				if p.Status == StatusCritical {
					criticalPVCs++
				} else if p.Status == StatusWarning {
					warningPVCs++
				}
			}

			if criticalPVCs > 0 {
				o.publishProgress(sessionID, cluster, "pvcs", fmt.Sprintf("%d PVC(s) com problemas criticos encontrado(s)", criticalPVCs), pvcsEnd, StatusCritical)
			} else if warningPVCs > 0 {
				o.publishProgress(sessionID, cluster, "pvcs", fmt.Sprintf("%d PVC(s) com avisos encontrado(s)", warningPVCs), pvcsEnd, StatusWarning)
			} else {
				o.publishProgress(sessionID, cluster, "pvcs", fmt.Sprintf("%d PVC(s) verificado(s) - todos saudaveis", len(pvcResults)), pvcsEnd, StatusHealthy)
			}
		}()
	}

	// Check Nodes (capacidade/utilização — recurso cluster-scoped, não depende de namespaces)
	if req.CheckNodes {
		nodesStart := currentRangeStart
		nodesEnd := currentRangeStart + rangePerCheck
		currentRangeStart = nodesEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "nodes", "Verificando capacidade e utilização dos nós...", nodesStart, StatusHealthy)

			nodeResults := o.nodeChecker.CheckAll(ctx, client, req.GetTimeoutNodes())

			mu.Lock()
			result.NodeResults = nodeResults
			mu.Unlock()

			criticalNodes := GetNodeCriticalCount(nodeResults)
			warningNodes := GetNodeWarningCount(nodeResults)
			if criticalNodes > 0 {
				o.publishProgress(sessionID, cluster, "nodes", fmt.Sprintf("%d nó(s) com problemas críticos de capacidade", criticalNodes), nodesEnd, StatusCritical)
			} else if warningNodes > 0 {
				o.publishProgress(sessionID, cluster, "nodes", fmt.Sprintf("%d nó(s) com avisos de capacidade", warningNodes), nodesEnd, StatusWarning)
			} else {
				o.publishProgress(sessionID, cluster, "nodes", fmt.Sprintf("%d nó(s) verificado(s) - todos saudáveis", len(nodeResults)), nodesEnd, StatusHealthy)
			}
		}()
	}

	// Check Dynatrace Problems
	if req.CheckDynatrace && req.DynatraceURL != "" {
		dtStart := currentRangeStart
		dtEnd := currentRangeStart + rangePerCheck
		currentRangeStart = dtEnd

		wg.Add(1)
		go func() {
			defer wg.Done()
			o.publishProgress(sessionID, cluster, "dynatrace", "Buscando problems abertos no Dynatrace...", dtStart, StatusHealthy)

			dtResults, _ := o.dynatraceChecker.CheckAll(ctx, req.DynatraceURL, req.DynatraceToken, req.DynatraceTagFilter, cluster, req.GetTimeoutDynatrace())

			mu.Lock()
			result.DynatraceResults = dtResults
			mu.Unlock()

			critical := 0
			for _, d := range dtResults {
				if d.Status == StatusCritical {
					critical++
				}
			}
			if critical > 0 {
				o.publishProgress(sessionID, cluster, "dynatrace", fmt.Sprintf("%d problem(s) Dynatrace crítico(s) correlacionado(s) com este cluster", critical), dtEnd, StatusCritical)
			} else if len(dtResults) > 0 {
				o.publishProgress(sessionID, cluster, "dynatrace", fmt.Sprintf("%d problem(s) Dynatrace encontrado(s)", len(dtResults)), dtEnd, StatusWarning)
			} else {
				o.publishProgress(sessionID, cluster, "dynatrace", "Nenhum problem Dynatrace correlacionado com este cluster", dtEnd, StatusHealthy)
			}
		}()
	}

	// Aguardar conclusão
	wg.Wait()

	// Calcular resumo
	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	o.calculateSummary(result)

	// Busca reversa K8s → DT: para workloads K8s com problema que não apareceram nos resultados DT
	// (tags OneAgent ausentes/incorretas). Timeout: 10s para não atrasar o HC.
	if req.CheckDynatrace && req.DynatraceURL != "" {
		// Usa extractTroubledWorkloadInfos para ter namespace + nome (necessário para correlação)
		troubledInfos := extractTroubledWorkloadInfos(result)
		existingDTWorkloads := extractExistingDTWorkloads(result.DynatraceResults)
		// Filtra apenas workloads que NÃO têm match DT ainda
		var unmatched []TroubledWorkloadInfo
		for _, tw := range troubledInfos {
			key := strings.ToLower(tw.Namespace) + "/" + strings.ToLower(tw.Name)
			if _, ok := existingDTWorkloads[key]; !ok {
				unmatched = append(unmatched, tw)
			}
		}
		if len(unmatched) > 0 {
			names := make([]string, len(unmatched))
			for i, tw := range unmatched {
				names[i] = tw.Name
			}
			log.Info().
				Strs("unmatched_workloads", names).
				Str("cluster", cluster).
				Msg("[Orchestrator] Iniciando busca reversa DT para workloads K8s sem match")

			existingIDs := make(map[string]struct{}, len(result.DynatraceResults))
			for _, d := range result.DynatraceResults {
				existingIDs[d.ProblemID] = struct{}{}
			}

			extra := o.dynatraceChecker.SearchProblemsForWorkloads(
				ctx, req.DynatraceURL, req.DynatraceToken,
				unmatched, cluster, existingIDs, 10,
			)
			if len(extra) > 0 {
				result.DynatraceResults = append(result.DynatraceResults, extra...)
			}
		}
	}

	// OneAgent Signals: scan híbrido (Fase 1: K8s troubled workloads → DT; Fase 2: varredura geral)
	if req.CheckOneAgentSignals && req.DynatraceURL != "" {
		o.publishProgress(sessionID, cluster, "oneagent", "Coletando sinais OneAgent (K8s + DT)...", 88, StatusHealthy)

		// Construir mapa de workloads já cobertos por DT problems (para marcar HasDTProblem)
		existingDTWorkloads := make(map[string]struct{}, len(result.DynatraceResults))
		for _, d := range result.DynatraceResults {
			for _, w := range d.K8sWorkloads {
				existingDTWorkloads[strings.ToLower(w)] = struct{}{}
			}
		}

		// Fase 1: workloads K8s com problema sem match DT (candidates para enriquecimento)
		allTroubled := extractTroubledWorkloadInfos(result)
		var p1Workloads []TroubledWorkloadInfo
		for _, tw := range allTroubled {
			key := strings.ToLower(tw.Namespace) + "/" + strings.ToLower(tw.Name)
			if _, covered := existingDTWorkloads[key]; !covered {
				p1Workloads = append(p1Workloads, tw)
			}
		}

		signals := o.oneAgentChecker.CheckAll(
			ctx,
			req.DynatraceURL, req.DynatraceToken,
			cluster,
			existingDTWorkloads,
			p1Workloads,
			o.nodePoolStore,
			o.depRegistry,
			req.OneAgentThresholds,
			req.GetTimeoutOneAgent(),
		)
		if signals == nil {
			signals = []OneAgentSignal{} // array vazio (não null) para o frontend distinguir "não executado" de "sem dados"
		}
		result.OneAgentSignals = signals

		atRisk := 0
		for _, s := range signals {
			if s.RiskLevel != SeverityInfo {
				atRisk++
			}
		}
		if atRisk > 0 {
			o.publishProgress(sessionID, cluster, "oneagent",
				fmt.Sprintf("%d workload(s) com sinais de risco (sem problem DT ativo)", atRisk), 90, StatusWarning)
		} else {
			o.publishProgress(sessionID, cluster, "oneagent",
				fmt.Sprintf("%d entidades varredas — sem sinais de risco por threshold", len(signals)), 90, StatusHealthy)
		}
	}

	// Correlacionar K8s ↔ Dynatrace (apenas quando DT está habilitado e retornou dados)
	if req.CheckDynatrace {
		result.CorrelatedItems = Correlate(ctx, o.storage, result)
		if len(result.CorrelatedItems) > 0 {
			correlated := 0
			for _, ci := range result.CorrelatedItems {
				if ci.Correlated {
					correlated++
				}
			}
			log.Info().
				Str("cluster", cluster).
				Int("total_correlated_items", len(result.CorrelatedItems)).
				Int("bidirectional_matches", correlated).
				Msg("[Correlator] K8s ↔ Dynatrace correlação concluída")

			// Fase 7 (LATENCY-METRICS-PLAN.md): cruzar breach de latência (Prometheus/DT) com os
			// DT Problems já encontrados acima — escala severidade quando os dois batem. Só roda
			// pros workloads com DTProblems não-vazio (custo proporcional a "quantos problems DT
			// ativos", não ao total de workloads do cluster).
			if req.DynatraceURL != "" {
				result.CorrelatedItems = EnrichWithLatencyBreach(ctx, result.CorrelatedItems, cluster, req.DynatraceURL, req.DynatraceToken)
				breaches := 0
				for _, ci := range result.CorrelatedItems {
					if ci.LatencyBreach {
						breaches++
					}
				}
				if breaches > 0 {
					log.Info().
						Str("cluster", cluster).
						Int("latency_breaches", breaches).
						Msg("[Correlator] Breach de latência escalado (P95 acima do threshold + DT Problem ativo)")
				}
			}
		}
	}

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

		// Publicar primeiros HPAs críticos (se ainda tiver espaço)
		for _, h := range result.HPAResults {
			if h.Status == StatusCritical && criticalShown < maxCritical {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Crítico: HPA %s/%s - %s", h.Namespace, h.Name, h.Message), 97, StatusCritical)
				criticalShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar primeiros PVCs críticos (se ainda tiver espaço)
		for _, p := range result.PVCResults {
			if p.Status == StatusCritical && criticalShown < maxCritical {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Crítico: PVC %s/%s - %s", p.Namespace, p.Name, p.Message), 97, StatusCritical)
				criticalShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar problems Dynatrace críticos (se ainda tiver espaço)
		for _, d := range result.DynatraceResults {
			if d.Status == StatusCritical && criticalShown < maxCritical {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Crítico: Dynatrace %s - %s", d.DisplayID, d.Title), 97, StatusCritical)
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

		// Publicar primeiros HPAs warning
		for _, h := range result.HPAResults {
			if h.Status == StatusWarning && warningShown < maxWarning {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Aviso: HPA %s/%s - %s", h.Namespace, h.Name, h.Message), 98, StatusWarning)
				warningShown++
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Publicar primeiros PVCs warning
		for _, p := range result.PVCResults {
			if p.Status == StatusWarning && warningShown < maxWarning {
				o.publishProgress(sessionID, cluster, "summary",
					fmt.Sprintf("Aviso: PVC %s/%s - %s", p.Namespace, p.Name, p.Message), 98, StatusWarning)
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

	// ======== Registrar métricas Prometheus ========
	promMetrics := metrics.GetMetrics()
	durationSeconds := float64(result.Duration) / 1000.0 // converter ms para segundos

	// Registrar health check completo (duração, status, contador)
	promMetrics.RecordHealthCheck(cluster, string(result.OverallStatus), durationSeconds)

	// Registrar contagem de recursos por status
	promMetrics.RecordDeployments(cluster, countByStatus(result.DeploymentResults, StatusHealthy),
		countByStatus(result.DeploymentResults, StatusWarning), countByStatus(result.DeploymentResults, StatusCritical))
	promMetrics.RecordServices(cluster, countByStatus(result.ServiceResults, StatusHealthy),
		countByStatus(result.ServiceResults, StatusWarning), countByStatus(result.ServiceResults, StatusCritical))
	promMetrics.RecordHPAs(cluster, countByStatus(result.HPAResults, StatusHealthy),
		countByStatus(result.HPAResults, StatusWarning), countByStatus(result.HPAResults, StatusCritical))
	promMetrics.RecordPVCs(cluster, countByStatus(result.PVCResults, StatusHealthy),
		countByStatus(result.PVCResults, StatusWarning), countByStatus(result.PVCResults, StatusCritical))

	// Registrar eventos por severidade
	promMetrics.RecordEvents(cluster, result.WarningCount, result.CriticalCount)

	log.Debug().
		Str("cluster", cluster).
		Float64("duration_seconds", durationSeconds).
		Str("status", string(result.OverallStatus)).
		Msg("Prometheus metrics recorded for health check")
	// ================================================

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

	// Contar HPAs
	for _, h := range result.HPAResults {
		statusCount[h.Status]++
	}

	// Contar PVCs
	for _, p := range result.PVCResults {
		statusCount[p.Status]++
	}

	// Contar Dynatrace problems
	for _, d := range result.DynatraceResults {
		statusCount[d.Status]++
	}

	// Contar nodes
	for _, n := range result.NodeResults {
		statusCount[n.Status]++
	}

	result.TotalChecks = len(result.DeploymentResults) + len(result.ServiceResults) + len(result.ConfigResults) + len(result.EventResults) + len(result.HPAResults) + len(result.PVCResults) + len(result.DynatraceResults) + len(result.NodeResults)
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

// GetDashboardMetrics retorna métricas para o dashboard de health checking
func (o *Orchestrator) GetDashboardMetrics(ctx context.Context, days int) (*DashboardMetrics, error) {
	return o.storage.GetDashboardMetrics(ctx, days)
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

// =============== Helper functions para métricas Prometheus ===============

// countByStatus conta resultados de um slice genérico pelo status
// Aceita qualquer slice que tenha campo Status do tipo HealthStatus
func countByStatus[T any](results []T, status HealthStatus) int {
	count := 0
	for _, r := range results {
		// Usar reflexão/type assertion para acessar Status
		switch v := any(r).(type) {
		case DeploymentHealth:
			if v.Status == status {
				count++
			}
		case ServiceHealth:
			if v.Status == status {
				count++
			}
		case HPAHealth:
			if v.Status == status {
				count++
			}
		case PVCHealth:
			if v.Status == status {
				count++
			}
		}
	}
	return count
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

// extractTroubledWorkloadInfos retorna workloads K8s com problema incluindo namespace e resumo de issues.
// Usado como entrada para a Fase 1 do scan OneAgent.
func extractTroubledWorkloadInfos(result *HealthCheckResult) []TroubledWorkloadInfo {
	type key struct{ ns, name string }
	seenIdx := make(map[key]int)
	var infos []TroubledWorkloadInfo

	add := func(ns, name, issue string, sev Severity) {
		ns = strings.TrimSpace(ns)
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		k := key{strings.ToLower(ns), strings.ToLower(name)}
		if idx, ok := seenIdx[k]; ok {
			infos[idx].Issues = append(infos[idx].Issues, issue)
			if sev.Weight() > infos[idx].Severity.Weight() {
				infos[idx].Severity = sev
			}
		} else {
			seenIdx[k] = len(infos)
			infos = append(infos, TroubledWorkloadInfo{
				Name:      name,
				Namespace: ns,
				Issues:    []string{issue},
				Severity:  sev,
			})
		}
	}

	for _, d := range result.DeploymentResults {
		if d.Status != StatusHealthy {
			sev := SeverityMedium
			if d.Status == StatusCritical {
				sev = SeverityCritical
			}
			add(d.Namespace, d.Name, d.Message, sev)
		}
	}
	for _, h := range result.HPAResults {
		if h.Status != StatusHealthy && h.TargetName != "" {
			sev := SeverityMedium
			if h.Status == StatusCritical {
				sev = SeverityCritical
			}
			add(h.Namespace, h.TargetName, h.Message, sev)
		}
	}
	for _, e := range result.EventResults {
		if e.Status != StatusHealthy && e.InvolvedKind == "Deployment" {
			add(e.Namespace, e.InvolvedName, e.Message, e.Severity)
		}
	}
	return infos
}

// extractTroubledWorkloads retorna os nomes (sem namespace) dos workloads K8s com problema.
// Usado para direcionar a busca reversa DT.
func extractTroubledWorkloads(result *HealthCheckResult) []string {
	seen := make(map[string]struct{})
	var names []string

	add := func(name string) {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" {
			if _, ok := seen[n]; !ok {
				seen[n] = struct{}{}
				names = append(names, name)
			}
		}
	}

	for _, d := range result.DeploymentResults {
		if d.Status != StatusHealthy {
			add(d.Name)
		}
	}
	for _, h := range result.HPAResults {
		if h.Status != StatusHealthy && h.TargetName != "" {
			add(h.TargetName)
		}
	}
	for _, e := range result.EventResults {
		if e.Status != StatusHealthy && e.InvolvedKind == "Deployment" {
			add(e.InvolvedName)
		}
	}
	return names
}

// extractExistingDTWorkloads retorna um set com os nomes de workloads já cobertos pelos DynatraceResults.
// Permite filtrar workloads que já têm match DT para não fazer busca reversa desnecessária.
func extractExistingDTWorkloads(dtResults []DynatraceHealth) map[string]struct{} {
	set := make(map[string]struct{})
	for _, d := range dtResults {
		for _, w := range d.K8sWorkloads {
			// formato "namespace/workload" — extrair só o nome
			parts := strings.SplitN(w, "/", 2)
			if len(parts) == 2 {
				set[strings.ToLower(parts[1])] = struct{}{}
			}
		}
	}
	return set
}
