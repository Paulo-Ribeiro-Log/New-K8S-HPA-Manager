package web

import (
	// "context"  // TODO: Remover após migração completa para V2
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/healthcheck"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/notifications"
	"k8s-hpa-manager/internal/rbac"
	"k8s-hpa-manager/internal/storage"
	"k8s-hpa-manager/internal/updater"

	// TODO: Remover após migração completa para V2
	// "k8s-hpa-manager/internal/monitoring/analyzer"
	// "k8s-hpa-manager/internal/monitoring/engine"
	enginev2 "k8s-hpa-manager/internal/monitoring/engine"
	// "k8s-hpa-manager/internal/monitoring/models"
	helmservice "k8s-hpa-manager/internal/helm"
	"k8s-hpa-manager/internal/monitoring/scanner"
	helmclient "k8s-hpa-manager/internal/pkg/helm"
	"k8s-hpa-manager/internal/web/handlers"
	"k8s-hpa-manager/internal/web/middleware"
)

//go:embed all:static
var staticFiles embed.FS

// Server representa o servidor HTTP
type Server struct {
	router         *gin.Engine
	kubeManager    *config.KubeConfigManager
	port           int
	token          string
	disableADAuth  bool // Flag para desabilitar verificação RBAC (emergências)
	lastHeartbeat  time.Time
	heartbeatMutex sync.RWMutex
	shutdownTimer  *time.Timer
	timerMutex     sync.Mutex // Protege operações no timer
	logBuffer      *handlers.LogBuffer
	historyTracker *history.HistoryTracker

	// TODO: Remover após migração completa para V2
	// monitoringEngine *engine.ScanEngine
	// snapshotChan     chan *models.HPASnapshot
	// anomalyChan      chan analyzer.Anomaly
	// stressResultChan chan *models.StressTestMetrics
	// monitoringCtx    context.Context
	// monitoringCancel context.CancelFunc

	// Monitoring Engine V2 (sem port-forwards)
	monitoringEngineV2 *enginev2.MonitoringEngineV2

	// Notification Manager (Windows Toast via PowerShell)
	notificationManager *notifications.NotificationManager

	// AI Diagnostics Handler (pode ser nil se AI estiver desabilitado)
	aiHandler *handlers.AIDiagnosticsHandler

	// AI Tokens Handler (gerencia tokens AI dos usuários)
	aiTokensHandler *handlers.AITokensHandler

	// AI Tokens Store (compartilhado com Dynatrace handler)
	aiTokensStore *storage.UserTokensStore

	// AI History Store (compartilhado com Dynatrace handler)
	aiHistoryStore *storage.AIHistoryStore

	// KubeManager wrapper para AI (pode ser nil se AI estiver desabilitado)
	kubeManagerWrapper *kubernetes.KubeManager

	// AWX Integration Handler (pode ser nil se AWX não estiver configurado)
	awxHandler *handlers.AWXHandler

	// Node Pool Registry (catálogo de node pools para correlação Dynatrace)
	nodepoolRegistryHandler *handlers.NodePoolRegistryHandler
	npRegistryStore         *storage.NodePoolRegistryStore

	// FinOps Timeline Store (snapshots históricos de HPA para comparação)
	finopsTimelineStore *storage.FinOpsTimelineStore
}

// NewServer cria uma nova instância do servidor web
func NewServer(kubeconfig string, port int, debug bool, disableADAuth bool, aiProvider, ollamaURL, ollamaModel, claudeAPIKey, claudeModel, geminiAuthMode, geminiProject, geminiLocation string) (*Server, error) {
	// Reutilizar gerenciador de kube existente
	kubeManager, err := config.NewKubeConfigManager(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube manager: %w", err)
	}

	// Token de autenticação (opcional para POC)
	token := os.Getenv("K8S_HPA_WEB_TOKEN")
	if token == "" {
		token = "poc-token-123" // Token padrão para POC
		fmt.Println("⚠️  Usando token padrão para POC: poc-token-123")
		fmt.Println("💡 Para produção, defina K8S_HPA_WEB_TOKEN")
	}

	// Setup Gin
	if !debug {
		gin.SetMode(gin.ReleaseMode)
	}
	// gin.New() ao invés de gin.Default() para controle manual dos middlewares
	router := gin.New()
	// UseRawPath=true: Gin usa r.URL.RawPath para routing, preservando %2F nos path params.
	// Necessário porque ARNs EKS contêm "/" (ex: arn:aws:eks:REGION:ACCT:cluster/NAME)
	// que o encodeURIComponent do JS codifica como %2F, mas o net/http decodifica antes de rotear.
	router.UseRawPath = true
	router.UnescapePathValues = true

	// Criar buffer de logs (mantém últimos 1000 logs em memória)
	logBuffer := handlers.NewLogBuffer(1000)

	// Criar history tracker
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	baseDir := filepath.Join(homeDir, ".k8s-hpa-manager")
	historyTracker, err := history.NewHistoryTracker(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create history tracker: %w", err)
	}

	// Criar notification manager (Windows Toast via PowerShell/WSL2)
	notificationManager := notifications.NewNotificationManager()
	fmt.Println("📢 Notification Manager inicializado")
	if notificationManager.IsEnabled() {
		fmt.Println("   ✅ Notificações Windows habilitadas (via PowerShell)")
	} else {
		fmt.Println("   ⚠️  Notificações desabilitadas (não está em WSL2)")
	}

	// Aviso se autenticação AD estiver desabilitada
	if disableADAuth {
		fmt.Println("\n⚠️  ⚠️  ⚠️  AVISO DE SEGURANÇA ⚠️  ⚠️  ⚠️")
		fmt.Println("   Verificação RBAC (Azure AD) DESABILITADA")
		fmt.Println("   Todos os usuários terão acesso TOTAL ao sistema")
		fmt.Println("   Use apenas para debugging ou emergências")
		fmt.Println("   ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️")
	}

	// Configurar historyTracker no kubeManager para audit logging de rollouts
	kubeManager.SetHistoryTracker(historyTracker)

	// TODO: Remover após migração completa para V2
	// snapshotChan := make(chan *models.HPASnapshot, 100)
	// anomalyChan := make(chan analyzer.Anomaly, 100)
	// stressResultChan := make(chan *models.StressTestMetrics, 10)
	// monitoringCtx, monitoringCancel := context.WithCancel(context.Background())
	// scanConfig := &scanner.ScanConfig{...}
	// monitoringEngine := engine.New(scanConfig, snapshotChan, anomalyChan, stressResultChan)

	// Criar Monitoring Engine V2 (sem port-forwards, acesso direto ao Prometheus)
	monitoringEngineV2 := enginev2.NewMonitoringEngineV2()
	fmt.Println("✅ Monitoring Engine V2 criado (sem port-forwards - acesso direto ao Prometheus)")

	// Inicializar AI Diagnostics System
	fmt.Println("🤖 Inicializando AI Diagnostics System...")

	// 1. Criar SQLite client para histórico AI (em ~/.k8s-hpa-manager/)
	// Reutilizar baseDir que já foi criado anteriormente
	aiDBPath := filepath.Join(baseDir, "ai_diagnostics.db")
	sqliteClient, err := storage.NewSQLiteClient(aiDBPath)
	if err != nil {
		fmt.Printf("⚠️  Falha ao criar SQLite client para AI: %v\n", err)
		fmt.Println("   AI Diagnostics desabilitado")
		sqliteClient = nil
	}

	var aiHistoryStore *storage.AIHistoryStore
	var aiHandler *handlers.AIDiagnosticsHandler
	var aiTokensStore *storage.UserTokensStore
	var aiTokensHandler *handlers.AITokensHandler
	var localSettingsStore *storage.LocalSettingsStore
	var kubeManagerWrapper *kubernetes.KubeManager
	var aiConfig *ai.Config

	if sqliteClient != nil {
		aiHistoryStore = storage.NewAIHistoryStore(sqliteClient)
		aiTokensStore = storage.NewUserTokensStore(sqliteClient)
		localSettingsStore = storage.NewLocalSettingsStore(sqliteClient)

		// Criar tabelas
		if err := aiTokensStore.CreateTable(); err != nil {
			fmt.Printf("⚠️  Falha ao criar tabela de tokens: %v\n", err)
		}
		if err := localSettingsStore.CreateTable(); err != nil {
			fmt.Printf("⚠️  Falha ao criar tabela local_settings: %v\n", err)
		}

		// Criar AI Tokens Handler com LocalSettingsStore para persistência
		aiTokensHandler = handlers.NewAITokensHandler(aiTokensStore, localSettingsStore)
		fmt.Println("✅ AI Tokens Handler criado com persistência local")

		// 2. Criar AI config
		aiConfig = &ai.Config{
			Provider:             aiProvider,
			OllamaBaseURL:        ollamaURL,
			OllamaModel:          ollamaModel,
			ClaudeAPIKey:         claudeAPIKey,
			ClaudeModel:          claudeModel,
			GeminiAuthMode:       geminiAuthMode,
			GeminiVertexProject:  geminiProject,
			GeminiVertexLocation: geminiLocation,
			Timeout:              300, // 5 minutos para análises preditivas complexas
		}

		// 3. Criar KubeManager wrapper
		kubeManagerWrapper = kubernetes.NewKubeManager(
			kubeManager.GetClient,
			nil, // kubectl describe será implementado depois
		)

		// 4. Criar AI provider padrão (opcional — pode falhar se credenciais não estão disponíveis)
		var defaultAnalyzer *ai.Analyzer
		aiProviderInstance, err := ai.NewProvider(aiConfig)
		if err != nil {
			fmt.Printf("⚠️  Falha ao criar AI provider padrão: %v\n", err)
			fmt.Println("   ℹ️  AI Diagnostics funcionará via tokens de usuário (AI Settings)")
		} else {
			// 5. Criar AI Analyzer padrão (fallback quando usuário não configurou tokens)
			defaultAnalyzer = ai.NewAnalyzer(aiProviderInstance, kubeManagerWrapper, aiHistoryStore)
			fmt.Printf("✅ AI Diagnostics habilitado (Provider padrão: %s)\n", aiProvider)
			if aiProvider == "gemini" {
				if geminiAuthMode == "vertex" {
					fmt.Printf("   ✅ Modo Vertex AI (SSO/ADC via gcloud) — projeto: %s, região: %s\n", geminiProject, geminiLocation)
				} else if os.Getenv("GEMINI_API_KEY") != "" {
					fmt.Println("   ✅ GEMINI_API_KEY detectado (env var)")
				} else {
					fmt.Println("   ⚠️  GEMINI_API_KEY não encontrado (pode falhar na análise)")
				}
			}
		}

		// 6. Criar handler (sempre criado — suporta tokens por usuário mesmo sem provider padrão)
		aiHandler = handlers.NewAIDiagnosticsHandler(
			defaultAnalyzer,
			aiHistoryStore,
			aiTokensStore,
			kubeManagerWrapper,
			aiConfig,
		)
		fmt.Println("   ℹ️  Usuários podem configurar seus próprios modelos em Settings → AI")
	}

	// AWX Integration (URL configurada via UI — perfil do usuário)
	awxHandler := handlers.NewAWXHandler(baseDir)
	{
		fmt.Println("ℹ️  AWX Integration: URL e credenciais configuradas via perfil do usuário")
	}

	// Node Pool Registry (catálogo para correlação Dynatrace aks-<pool>-vmss*)
	var nodepoolRegistryHandler *handlers.NodePoolRegistryHandler
	var npRegistryStore *storage.NodePoolRegistryStore
	npRegistryDBPath := filepath.Join(baseDir, "nodepool_registry.db")
	if store, err := storage.NewNodePoolRegistryStore(npRegistryDBPath); err != nil {
		fmt.Printf("⚠️  NodePool Registry: falha ao criar store: %v\n", err)
	} else {
		npRegistryStore = store
		nodepoolRegistryHandler = handlers.NewNodePoolRegistryHandler(kubeManager, npRegistryStore)
		fmt.Println("✅ NodePool Registry inicializado (correlação Dynatrace aks-<pool>-vmss*)")
	}

	// FinOps Timeline Store (snapshots históricos HPA)
	var finopsTimelineStore *storage.FinOpsTimelineStore
	finopsTimelineDBPath := filepath.Join(baseDir, "finops_timeline.db")
	if store, err := storage.NewFinOpsTimelineStore(finopsTimelineDBPath); err != nil {
		fmt.Printf("⚠️  FinOps Timeline Store: falha ao criar store: %v\n", err)
	} else {
		finopsTimelineStore = store
		fmt.Println("✅ FinOps Timeline Store inicializado (snapshots históricos HPA)")
	}

	server := &Server{
		router:              router,
		kubeManager:         kubeManager,
		port:                port,
		token:               token,
		disableADAuth:       disableADAuth,
		lastHeartbeat:       time.Now(),
		logBuffer:           logBuffer,
		historyTracker:      historyTracker,
		notificationManager: notificationManager,
		// TODO: Remover após migração completa para V2
		// monitoringEngine:   monitoringEngine,
		// snapshotChan:       snapshotChan,
		// anomalyChan:        anomalyChan,
		// stressResultChan:   stressResultChan,
		// monitoringCtx:      monitoringCtx,
		// monitoringCancel:   monitoringCancel,
		monitoringEngineV2: monitoringEngineV2,
		aiHandler:          aiHandler,          // Pode ser nil se AI estiver desabilitado
		aiTokensHandler:    aiTokensHandler,    // Gerencia tokens AI dos usuários
		aiTokensStore:      aiTokensStore,      // Compartilhado com Dynatrace handler
		aiHistoryStore:     aiHistoryStore,     // Compartilhado com Dynatrace handler
		kubeManagerWrapper: kubeManagerWrapper, // Para predictions RBAC
		awxHandler:              awxHandler,              // AWX Integration (certificados TLS)
		nodepoolRegistryHandler: nodepoolRegistryHandler, // Catálogo de node pools Dynatrace
		npRegistryStore:         npRegistryStore,         // Usado pelo healthcheck orchestrator
		finopsTimelineStore:     finopsTimelineStore,     // Snapshots históricos HPA para comparação
	}

	server.setupMiddleware()
	server.setupRoutes()
	server.setupStatic()
	server.startInactivityMonitor()

	// ❌ REMOVIDO: Persistência de targets em arquivo
	// Source of truth: localStorage do frontend (reconciliação)

	return server, nil
}

// setupMiddleware configura os middlewares do servidor
func (s *Server) setupMiddleware() {
	// CORS - permitir todas as origens para POC
	s.router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-GitHub-Email"}, // ✅ Header customizado para token GitHub
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Custom logging middleware que captura logs no buffer
	s.router.Use(s.loggingMiddleware())

	// Logging padrão do Gin (console)
	s.router.Use(gin.Logger())

	// Recovery
	s.router.Use(gin.Recovery())
}

// loggingMiddleware captura logs de todas as requisições HTTP
func (s *Server) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Timestamp de início
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		// Processar requisição
		c.Next()

		// Calcular latência
		latency := time.Since(start)
		statusCode := c.Writer.Status()

		// Criar entrada de log
		logEntry := fmt.Sprintf("[%s] %s %s | Status: %d | Latency: %v",
			start.Format("2006/01/02 15:04:05"),
			method,
			path,
			statusCode,
			latency,
		)

		// Adicionar ao buffer (skip health checks para não encher o log)
		if path != "/health" && path != "/heartbeat" {
			s.logBuffer.Add(logEntry)
		}
	}
}

// setupRoutes configura as rotas da API
func (s *Server) setupRoutes() {
	// Obter baseDir para caminhos de bancos SQLite (necessário para predictions e health checks)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("❌ Erro ao obter diretório home: %v\n", err)
		return
	}
	baseDir := filepath.Join(homeDir, ".k8s-hpa-manager")

	// Health check (sem auth)
	// OAuth2 Google callback — usa a porta do próprio app (funciona no WSL2 onde portas aleatórias não são forwardadas)
	if s.aiTokensHandler != nil {
		s.router.GET("/oauth/google/callback", s.aiTokensHandler.GoogleOAuthCallback)
	}

	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"version": "1.0.0-poc",
			"mode":    "web",
		})
	})

	// Heartbeat endpoint (sem auth) - frontend envia a cada 5 minutos
	s.router.POST("/heartbeat", func(c *gin.Context) {
		now := time.Now()

		s.heartbeatMutex.Lock()
		s.lastHeartbeat = now
		s.heartbeatMutex.Unlock()

		// Resetar timer de shutdown (thread-safe)
		// Usar 25 minutos (margem de 5 minutos sobre os 20 configurados)
		// Isso garante que heartbeats atrasados não causem shutdown prematuro
		s.timerMutex.Lock()
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
		s.shutdownTimer = time.AfterFunc(25*time.Minute, s.autoShutdown)
		s.timerMutex.Unlock()

		// Log para debugging
		fmt.Printf("💓 Heartbeat recebido: %s | Próximo shutdown em: %s\n",
			now.Format("15:04:05"),
			now.Add(25*time.Minute).Format("15:04:05"))

		c.JSON(200, gin.H{
			"status":         "alive",
			"last_heartbeat": s.lastHeartbeat,
		})
	})

	// Shutdown endpoint (com auth)
	s.router.POST("/shutdown", middleware.AuthMiddleware(s.token), func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Servidor será desligado em 1 segundo...",
		})

		// Aguardar resposta ser enviada e então encerrar
		go func() {
			fmt.Println("\n🛑 Shutdown solicitado via API...")
			fmt.Println("✅ Servidor encerrado")
			os.Exit(0)
		}()
	})

	// Version (sem auth - informação pública)
	versionHandler := handlers.NewVersionHandler()
	s.router.GET("/api/v1/version", versionHandler.GetVersion)
	s.router.POST("/api/v1/version/update", versionHandler.SelfUpdate)

	// API v1 (com auth)
	api := s.router.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(s.token))

	// RBAC - Inicializar manager e middleware
	rbacManager := rbac.NewRBACManager(s.disableADAuth)
	rbacMiddleware := middleware.NewRBACMiddleware(rbacManager)

	// WebSocket endpoints (com auth via query param + RBAC SRE-only)
	// Usa WebSocketAuthMiddleware para aceitar token via query parameter
	podExecHandler := handlers.NewPodExecHandler(s.kubeManager)

	wsShell := s.router.Group("/api/v1/pods/:cluster/:namespace/:name")
	wsShell.Use(middleware.WebSocketAuthMiddleware(s.token))
	wsShell.Use(rbacMiddleware.RequireSREGroup())
	{
		wsShell.GET("/shell", podExecHandler.HandleShell)
		wsShell.GET("/debug", podExecHandler.HandleDebug)
	}

	// RBAC - Endpoints públicos de permissões (apenas GET, sem proteção extra)
	api.GET("/permissions", rbacMiddleware.GetUserPermissions())
	api.POST("/permissions/refresh", rbacMiddleware.RefreshPermissions())

	// Clusters
	clusterHandler := handlers.NewClusterHandler(s.kubeManager)
	api.GET("/clusters", clusterHandler.List)
	api.GET("/clusters/:name/test", clusterHandler.Test)
	api.GET("/clusters/:name/config", clusterHandler.GetClusterConfig)
	api.POST("/clusters/:name/context", clusterHandler.SwitchToClusterContext)
	api.POST("/clusters/switch-context", clusterHandler.SwitchContext)
	api.GET("/clusters/info", clusterHandler.GetClusterInfo)

	// Auto-descoberta de clusters (SSE + Sync)
	autoDiscoverHandler := handlers.NewAutoDiscoverHandler(s.kubeManager)
	api.POST("/clusters/autodiscover", autoDiscoverHandler.HandleAutoDiscover)          // SSE com progress em tempo real
	api.POST("/clusters/autodiscover-sync", autoDiscoverHandler.HandleAutoDiscoverSync) // Síncrono sem SSE

	// Azure
	azureHandler := handlers.NewAzureHandler()
	api.POST("/azure/subscription", azureHandler.SetSubscription)

	// AWS SSO Auth
	awsAuthHandler := handlers.NewAWSAuthHandler(s.kubeManager)
	awsGroup := api.Group("/aws")
	awsGroup.GET("/auth/status", awsAuthHandler.CheckStatus)
	awsGroup.POST("/auth/login", awsAuthHandler.StartLogin)
	awsGroup.GET("/auth/poll", awsAuthHandler.PollLogin)
	awsGroup.GET("/config", awsAuthHandler.ListConfigs)
	awsGroup.GET("/config/:profile", awsAuthHandler.GetConfig)
	awsGroup.POST("/config", awsAuthHandler.SaveConfig)
	awsGroup.DELETE("/config/:profile", awsAuthHandler.DeleteConfig)

	// Namespaces
	namespaceHandler := handlers.NewNamespaceHandler(s.kubeManager, s.historyTracker)
	api.GET("/namespaces", namespaceHandler.List)
	api.GET("/namespaces/:cluster/:name", namespaceHandler.Get)
	api.GET("/namespaces/:cluster/:name/describe", namespaceHandler.Describe)
	api.GET("/namespaces/:cluster/metrics", namespaceHandler.GetMetrics) // NOVO: Métricas agregadas por namespace

	// Namespaces - Write Operations (SRE-only)
	api.POST("/namespaces/:cluster", rbacMiddleware.RequireSREGroup(), namespaceHandler.Create)
	api.PUT("/namespaces/:cluster/:name", rbacMiddleware.RequireSREGroup(), namespaceHandler.Apply)
	api.DELETE("/namespaces/:cluster/:name", rbacMiddleware.RequireSREGroup(), namespaceHandler.Delete)

	// HPAs
	hpaHandler := handlers.NewHPAHandler(s.kubeManager, s.historyTracker)
	api.GET("/hpas", hpaHandler.List)
	api.GET("/hpas/:cluster/:namespace/:name", hpaHandler.Get)

	// HPAs - Write Operations (SRE-only)
	api.PUT("/hpas/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), hpaHandler.Update)

	// Node Pools
	nodePoolHandler := handlers.NewNodePoolHandler(s.kubeManager, s.historyTracker)
	api.GET("/nodepools", nodePoolHandler.List)
	api.GET("/nodepools/disk-metrics", nodePoolHandler.GetNodePoolDiskMetrics) // Métricas de disco
	api.GET("/nodepools/storage-overview", nodePoolHandler.GetStorageOverview) // Visão geral de storage
	api.GET("/nodepools/sequence/progress", nodePoolHandler.SequenceProgress)  // SSE progress tracking
	api.GET("/nodepools/conntrack", nodePoolHandler.GetConntrackStats)                 // Conntrack stats por node
	api.GET("/nodepools/conntrack/history", nodePoolHandler.GetConntrackNodeHistory) // Histórico via Prometheus

	// Node Pools - Write Operations (SRE-only)
	api.PUT("/nodepools/:cluster/:resource_group/:name", rbacMiddleware.RequireSREGroup(), nodePoolHandler.Update)
	api.POST("/nodepools/:cluster/:resource_group/:name/abort", rbacMiddleware.RequireSREGroup(), nodePoolHandler.Abort)
	api.POST("/nodepools/apply-sequential", rbacMiddleware.RequireSREGroup(), nodePoolHandler.ApplySequential)
	api.POST("/nodepools/sequence/execute", rbacMiddleware.RequireSREGroup(), nodePoolHandler.ExecuteSequence) // NOVO: Cordon/Drain sequencing

	// Node Pools - Node Details
	api.GET("/nodes/:cluster/:nodepool", nodePoolHandler.ListNodesInNodePool)                  // Lista nodes do node pool
	api.GET("/nodes/:cluster/:nodepool/azure-info", nodePoolHandler.GetNodePoolAzureInfo)      // Info Azure async (tags, subscription)
	api.GET("/nodes/:cluster/:nodepool/:node", nodePoolHandler.GetNodeDetails)                 // Detalhes de um node específico

	// Node Pool Registry (catálogo para correlação Dynatrace aks-<pool>-vmss*)
	if s.nodepoolRegistryHandler != nil {
		api.GET("/nodepools/registry", s.nodepoolRegistryHandler.List)
		api.GET("/nodepools/registry/lookup", s.nodepoolRegistryHandler.Lookup)
		api.POST("/nodepools/registry/scan", rbacMiddleware.RequireSREGroup(), s.nodepoolRegistryHandler.Scan)
	}

	// FinOps — análise de custo real de clusters AKS (Azure Pricing API + alocação por workload)
	finOpsHandler := handlers.NewFinOpsHandler(s.kubeManager, s.npRegistryStore, s.finopsTimelineStore, s.aiHandler)
	api.GET("/finops/report", finOpsHandler.GetReport)
	api.GET("/finops/pricing", finOpsHandler.GetPricing)
	api.POST("/finops/pricing/refresh", finOpsHandler.RefreshPricing)
	api.GET("/finops/exchange-rate", finOpsHandler.GetExchangeRate)
	api.POST("/finops/analyze", finOpsHandler.AnalyzeReport)
	api.GET("/finops/timeline", finOpsHandler.GetTimeline)
	api.GET("/finops/timeline/compare", finOpsHandler.GetTimelineCompare)
	api.GET("/finops/timeline/compare-snapshot", finOpsHandler.CompareWithSnapshot)
	api.GET("/finops/timeline/compare-saved", finOpsHandler.CompareSnapshots)
	api.GET("/finops/timeline/saved", finOpsHandler.GetSavedTimelines)
	api.GET("/finops/vm-alternatives", finOpsHandler.GetVMAlternatives)
	api.POST("/finops/storage/refresh", finOpsHandler.RefreshDiskPricing)

	// SSE Progress Streaming (sem auth para permitir conexão EventSource)
	s.router.GET("/api/v1/nodepools/progress/:operationId", handlers.HandleProgressStream)
	s.router.GET("/api/v1/nodepools/progress/:operationId/status", handlers.HandleProgressStatus)

	// CronJobs
	cronJobHandler := handlers.NewCronJobHandler(s.kubeManager, s.historyTracker)
	api.GET("/cronjobs", cronJobHandler.List)
	api.GET("/cronjobs/:cluster/:namespace/:name", cronJobHandler.Get)
	api.GET("/cronjobs/:cluster/:namespace/:name/describe", cronJobHandler.Describe)
	api.POST("/cronjobs/diff", cronJobHandler.Diff)
	api.POST("/cronjobs/validate", cronJobHandler.Validate)

	// CronJobs - Write Operations (SRE-only)
	api.PUT("/cronjobs/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), cronJobHandler.Update)
	api.PUT("/cronjobs/:cluster/:namespace/:name/yaml", rbacMiddleware.RequireSREGroup(), cronJobHandler.Apply)
	api.POST("/cronjobs/:cluster/:namespace/:name/trigger", rbacMiddleware.RequireSREGroup(), cronJobHandler.Trigger)

	// Prometheus Stack
	prometheusHandler := handlers.NewPrometheusHandler(s.kubeManager, s.historyTracker)
	api.GET("/prometheus", prometheusHandler.List)

	// Prometheus Stack - Write Operations (SRE-only)
	api.PUT("/prometheus/:cluster/:namespace/:type/:name", rbacMiddleware.RequireSREGroup(), prometheusHandler.Update)
	api.POST("/prometheus/:cluster/:namespace/:type/:name/rollout", rbacMiddleware.RequireSREGroup(), prometheusHandler.Rollout)

	// ConfigMaps
	configMapHandler := handlers.NewConfigMapHandler(s.kubeManager, s.historyTracker)
	configMaps := api.Group("/configmaps")
	{
		configMaps.GET("", configMapHandler.List)
		configMaps.GET("/:cluster/:namespace/:name", configMapHandler.Get)
		configMaps.GET("/:cluster/:namespace/:name/describe", configMapHandler.Describe)
		configMaps.POST("/diff", configMapHandler.Diff)
		configMaps.POST("/validate", configMapHandler.Validate)

		// ConfigMaps - Write Operations (SRE-only)
		configMaps.POST("/:cluster/:namespace", rbacMiddleware.RequireSREGroup(), configMapHandler.Create)
		configMaps.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), configMapHandler.Apply)
		configMaps.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), configMapHandler.Delete)
	}

	// Ingress
	ingressHandler := handlers.NewIngressHandler(s.kubeManager, s.historyTracker)
	ingresses := api.Group("/ingresses")
	{
		ingresses.GET("", ingressHandler.List)
		ingresses.GET("/:cluster/:namespace/:name", ingressHandler.Get)
		ingresses.GET("/:cluster/:namespace/:name/describe", ingressHandler.Describe)
		ingresses.POST("/diff", ingressHandler.Diff)
		ingresses.POST("/validate", ingressHandler.Validate)

		// Ingress - Write Operations (SRE-only)
		ingresses.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), ingressHandler.Apply)
		ingresses.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), ingressHandler.Delete)
	}

	// Deployments
	deploymentHandler := handlers.NewDeploymentHandler(s.kubeManager, s.historyTracker)
	deployments := api.Group("/deployments")
	{
		deployments.GET("", deploymentHandler.List)
		deployments.GET("/:cluster/:namespace/:name", deploymentHandler.Get)
		deployments.GET("/:cluster/:namespace/:name/describe", deploymentHandler.Describe)
		deployments.POST("/diff", deploymentHandler.Diff)
		deployments.POST("/validate", deploymentHandler.Validate)

		// Deployments - Write Operations (SRE-only)
		deployments.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), deploymentHandler.Apply)
		deployments.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), deploymentHandler.Delete)
		deployments.POST("/:cluster/:namespace/:name/restart", rbacMiddleware.RequireSREGroup(), deploymentHandler.RolloutRestart)
		deployments.POST("/:cluster/:namespace/:name/scale", rbacMiddleware.RequireSREGroup(), deploymentHandler.Scale)
		// Deployments - Batch Operations (SRE-only)
		deployments.POST("/:cluster/batch/delete", rbacMiddleware.RequireSREGroup(), deploymentHandler.BatchDelete)
		deployments.POST("/:cluster/batch/restart", rbacMiddleware.RequireSREGroup(), deploymentHandler.BatchRestart)
	}

	// StatefulSets
	statefulSetHandler := handlers.NewStatefulSetHandler(s.kubeManager, s.historyTracker)
	statefulsets := api.Group("/statefulsets")
	{
		statefulsets.GET("", statefulSetHandler.List)
		statefulsets.GET("/:cluster/:namespace/:name", statefulSetHandler.Get)
		statefulsets.GET("/:cluster/:namespace/:name/describe", statefulSetHandler.Describe)
		statefulsets.POST("/diff", statefulSetHandler.Diff)
		statefulsets.POST("/validate", statefulSetHandler.Validate)

		// StatefulSets - Write Operations (SRE-only)
		statefulsets.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), statefulSetHandler.Apply)
		statefulsets.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), statefulSetHandler.Delete)
		statefulsets.POST("/:cluster/:namespace/:name/restart", rbacMiddleware.RequireSREGroup(), statefulSetHandler.RolloutRestart)
		statefulsets.POST("/:cluster/:namespace/:name/scale", rbacMiddleware.RequireSREGroup(), statefulSetHandler.Scale)
	}

	// DaemonSets
	daemonSetHandler := handlers.NewDaemonSetHandler(s.kubeManager, s.historyTracker)
	daemonsets := api.Group("/daemonsets")
	{
		daemonsets.GET("", daemonSetHandler.List)
		daemonsets.GET("/:cluster/:namespace/:name", daemonSetHandler.Get)
		daemonsets.GET("/:cluster/:namespace/:name/describe", daemonSetHandler.Describe)
		daemonsets.POST("/diff", daemonSetHandler.Diff)
		daemonsets.POST("/validate", daemonSetHandler.Validate)

		// DaemonSets - Write Operations (SRE-only)
		daemonsets.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), daemonSetHandler.Apply)
		daemonsets.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), daemonSetHandler.Delete)
		daemonsets.POST("/:cluster/:namespace/:name/restart", rbacMiddleware.RequireSREGroup(), daemonSetHandler.RolloutRestart)
	}

	// Pods/Containers
	podHandler := handlers.NewPodHandler(s.kubeManager, s.historyTracker)
	pods := api.Group("/pods")
	{
		pods.GET("", podHandler.List)
		pods.GET("/metrics", podHandler.GetBatchMetrics)
		pods.GET("/:cluster/:namespace/:name", podHandler.Get)
		pods.GET("/:cluster/:namespace/:name/describe", podHandler.Describe)
		pods.GET("/:cluster/:namespace/:name/logs", podHandler.GetLogs)
		pods.GET("/:cluster/:namespace/:name/metrics", podHandler.GetMetrics)
		pods.GET("/:cluster/:namespace/:name/download", podHandler.DownloadFromPod)
		pods.POST("/:cluster/:namespace/:name/download/batch", podHandler.DownloadMultipleFromPod)
		pods.GET("/:cluster/:namespace/:name/browse", podHandler.BrowseFiles)

		// Pods - Write Operations (SRE-only)
		pods.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), podHandler.Apply)
		pods.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), podHandler.Delete)
		pods.POST("/:cluster/:namespace/:name/restart", rbacMiddleware.RequireSREGroup(), podHandler.Restart)
		pods.POST("/:cluster/:namespace/:name/kill", rbacMiddleware.RequireSREGroup(), podHandler.Kill)
		pods.POST("/:cluster/:namespace/debug", rbacMiddleware.RequireSREGroup(), podHandler.CreateDebugPod)

		// Batch Operations (SRE-only)
		pods.POST("/:cluster/batch/delete", rbacMiddleware.RequireSREGroup(), podHandler.BatchDelete)
		pods.POST("/:cluster/batch/kill", rbacMiddleware.RequireSREGroup(), podHandler.BatchKill)
		pods.POST("/:cluster/batch/restart", rbacMiddleware.RequireSREGroup(), podHandler.BatchRestart)
	}

	// Pods Summary
	pods.GET("/:cluster/:namespace/summary", podHandler.GetSummary)

	// Events
	eventHandler := handlers.NewEventHandler(s.kubeManager)
	events := api.Group("/events")
	{
		events.GET("", eventHandler.List)
	}

	// Resource Quotas
	quotaHandler := handlers.NewResourceQuotaHandler(s.kubeManager)
	quotas := api.Group("/resource-quotas")
	{
		quotas.GET("", quotaHandler.List)
	}

	// Network Policies
	policyHandler := handlers.NewNetworkPolicyHandler(s.kubeManager)
	policies := api.Group("/network-policies")
	{
		policies.GET("", policyHandler.List)
	}

	// Services
	serviceHandler := handlers.NewServiceHandlerWithHistory(s.kubeManager, s.historyTracker)
	services := api.Group("/services")
	{
		services.GET("", serviceHandler.List)
		services.GET("/:cluster/:namespace/:name", serviceHandler.Get)
		services.GET("/:cluster/:namespace/:name/describe", serviceHandler.Describe)
		services.POST("/diff", serviceHandler.Diff)
		services.POST("/validate", serviceHandler.Validate)

		// Services - Write Operations (SRE-only)
		services.POST("/:cluster/:namespace", rbacMiddleware.RequireSREGroup(), serviceHandler.Create)
		services.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), serviceHandler.Apply)
		services.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), serviceHandler.Delete)
	}

	// VPAs (Vertical Pod Autoscalers)
	vpaHandler := handlers.NewVPAHandler(s.kubeManager)
	vpas := api.Group("/vpas")
	{
		vpas.GET("", vpaHandler.List)
		vpas.GET("/:cluster/:namespace/:name", vpaHandler.Get)
		vpas.GET("/:cluster/:namespace/:name/describe", vpaHandler.Describe)
		vpas.POST("/diff", vpaHandler.Diff)
		vpas.POST("/validate", vpaHandler.Validate)

		// VPAs - Write Operations (SRE-only)
		vpas.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), vpaHandler.Apply)
		vpas.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), vpaHandler.Delete)
	}

	// Command Runner — execução de comandos em lote em múltiplos clusters/namespaces
	commandRunnerHandler := handlers.NewCommandRunnerHandler(s.kubeManager, handlers.GetProgressTracker(), s.historyTracker, s.aiHandler)
	// SSE stream: usa WebSocketAuthMiddleware para aceitar token via query param
	s.router.GET("/api/v1/command-runner/stream/:sessionId",
		middleware.WebSocketAuthMiddleware(s.token),
		commandRunnerHandler.Stream)
	cmdRunner := api.Group("/command-runner")
	{
		cmdRunner.POST("/execute", rbacMiddleware.RequireSREGroup(), commandRunnerHandler.Execute)
		cmdRunner.POST("/generate", commandRunnerHandler.GenerateCommand)                             // AI: sem RBAC extra
		cmdRunner.DELETE("/session/:sessionId", commandRunnerHandler.Cancel) // Forçar parada — sem RBAC extra (quem executa pode parar)
	}

	// Resource Explorer — navegador universal de recursos K8s (built-in + CRDs)
	explorerHandler := handlers.NewExplorerHandler(s.kubeManager)
	explorer := api.Group("/explorer")
	{
		explorer.GET("/api-resources", explorerHandler.ListResources)
		explorer.GET("/items", explorerHandler.ListByKind)
		explorer.GET("/:cluster/:namespace/:resource/:name", explorerHandler.GetYAML)
		explorer.GET("/:cluster/:namespace/:resource/:name/describe", explorerHandler.Describe)
		explorer.POST("/diff", explorerHandler.Diff)
		explorer.POST("/validate", explorerHandler.Validate)

		// Explorer - Write Operations (SRE-only)
		explorer.PUT("/:cluster/:namespace/:resource/:name", rbacMiddleware.RequireSREGroup(), explorerHandler.Apply)
		explorer.DELETE("/:cluster/:namespace/:resource/:name", rbacMiddleware.RequireSREGroup(), explorerHandler.Delete)
	}

	// Dynatrace Integration — análise de problems com AI
	dtHandler := handlers.NewDynatraceHandler(s.aiTokensStore, s.aiHistoryStore, s.aiHandler, s.npRegistryStore)
	dt := api.Group("/dynatrace")
	{
		dt.GET("/config", dtHandler.GetConfig)
		dt.POST("/test", dtHandler.TestConnection)
		dt.GET("/management-zones", dtHandler.GetManagementZones)
		dt.GET("/problems", dtHandler.ListProblems)
		dt.GET("/problems/:problemId", dtHandler.GetProblem)
		dt.POST("/problems/:problemId/analyze", dtHandler.AnalyzeProblem)
		dt.POST("/problems/:problemId/investigate", dtHandler.InvestigateProblem)
		dt.GET("/problems/:problemId/metrics", dtHandler.GetProblemMetrics)
		dt.GET("/problems/:problemId/context", dtHandler.GetProblemContext)
		dt.GET("/history", dtHandler.GetHistory)
	}

	// Helm
	helmLogger := zerolog.New(os.Stdout).With().Timestamp().Str("component", "helm-cli").Logger()
	helmOptions := []helmclient.Option{helmclient.WithLogger(helmLogger)}
	if resolved, err := helmclient.ResolveBinary(""); err == nil {
		helmOptions = append(helmOptions, helmclient.WithBinary(resolved))
	} else {
		fmt.Printf("⚠️  Helm binary not found in PATH (continuing with default name): %v\n", err)
	}
	helmCLI := helmclient.NewCLIClient(helmOptions...)
	helmService := helmservice.NewService(s.kubeManager, helmCLI)
	helmHandler := handlers.NewHelmHandler(helmService)
	helmRoutes := api.Group("/helm")
	{
		helmRoutes.GET("/releases", helmHandler.List)
		helmRoutes.GET("/releases/:release", helmHandler.Get)
		helmRoutes.GET("/releases/:release/history", helmHandler.History)
		helmRoutes.GET("/releases/:release/revisions/:revision/values", helmHandler.GetRevisionValues)
		helmRoutes.POST("/releases", rbacMiddleware.RequireSREGroup(), helmHandler.Install)
		helmRoutes.PUT("/releases/:release", rbacMiddleware.RequireSREGroup(), helmHandler.Upgrade)
		helmRoutes.POST("/releases/:release/rollback", rbacMiddleware.RequireSREGroup(), helmHandler.Rollback)
		helmRoutes.DELETE("/releases/:release", rbacMiddleware.RequireSREGroup(), helmHandler.Uninstall)
		helmRoutes.GET("/operations/:operationId/stream", helmHandler.StreamOperation)
	}

	// Nexus
	nexusHandler, err := handlers.NewNexusHandler()
	if err != nil {
		fmt.Printf("⚠️  Failed to initialize Nexus handler: %v\n", err)
	} else {
		nexusRoutes := api.Group("/nexus")
		{
			nexusRoutes.GET("/status", nexusHandler.CheckStatus)
			nexusRoutes.POST("/test", nexusHandler.TestConnection)
			nexusRoutes.GET("/config", nexusHandler.LoadConfig)
			nexusRoutes.POST("/config", nexusHandler.SaveConfig)
			nexusRoutes.DELETE("/config", nexusHandler.DeleteConfig)
			nexusRoutes.POST("/values/download", nexusHandler.DownloadValues)
			nexusRoutes.POST("/values/compare", nexusHandler.CompareValues)
			nexusRoutes.GET("/browse", nexusHandler.BrowseRepository)
		}
	}

	// Secrets
	secretHandler := handlers.NewSecretHandler(s.kubeManager, s.historyTracker)
	secrets := api.Group("/secrets")
	{
		secrets.GET("", secretHandler.List)
		secrets.GET("/:cluster/:namespace/:name", secretHandler.Get)
		secrets.GET("/:cluster/:namespace/:name/describe", secretHandler.Describe)
		secrets.POST("/diff", secretHandler.Diff)
		secrets.POST("/validate", secretHandler.Validate)

		// Secrets - Write Operations (SRE-only)
		secrets.POST("/:cluster/:namespace", rbacMiddleware.RequireSREGroup(), secretHandler.Create)
		secrets.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), secretHandler.Apply)
		secrets.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), secretHandler.Delete)
	}

	// Certificates TLS
	certificatesHandler := handlers.NewCertificatesHandler(s.kubeManager)
	certGroup := api.Group("/certificates")
	{
		certGroup.POST("/scan", certificatesHandler.Scan)
		certGroup.GET("/:cluster/:namespace/:name", certificatesHandler.GetDetails)
		certGroup.GET("/report", certificatesHandler.Report)
		// Write Operations (SRE-only)
		certGroup.POST("/copy", rbacMiddleware.RequireSREGroup(), certificatesHandler.Copy)
		certGroup.POST("/upload", rbacMiddleware.RequireSREGroup(), certificatesHandler.Upload)
	}

	// AWX Integration (gerenciamento de certificados TLS via Ansible AWX/Tower)
	awxRoutes := api.Group("/awx")
	{
		awxRoutes.GET("/status",                 s.awxHandler.Status)
		awxRoutes.GET("/certificates",           s.awxHandler.ListCerts)
		awxRoutes.GET("/cluster-info",           s.awxHandler.GetClusterInfo)
		awxRoutes.GET("/templates/:id/survey",   s.awxHandler.GetTemplateSurvey)
		awxRoutes.POST("/jobs/launch",           rbacMiddleware.RequireSREGroup(), s.awxHandler.LaunchJob)
		// Credenciais por usuário (usuário/senha do SSO)
		awxRoutes.GET("/credentials/status",   s.awxHandler.GetCredentialsStatus)
		awxRoutes.POST("/credentials",         s.awxHandler.SaveCredentials)
		awxRoutes.DELETE("/credentials",       s.awxHandler.DeleteCredentials)
	}
	// SSE de logs do job: EventSource não suporta headers customizados, sem auth
	s.router.GET("/api/v1/awx/jobs/:id/stream", s.awxHandler.StreamJobLogs)
	fmt.Println("✅ AWX Integration routes registradas")

	// Validation (VPN + Azure CLI)
	validationHandler := handlers.NewValidationHandler()
	api.GET("/validate", validationHandler.Validate)

	// Alerts (Prometheus Alertmanager Integration)
	alertsHandler := handlers.NewAlertsHandler()
	alertsGroup := api.Group("/alerts")
	{
		alertsGroup.GET("", alertsHandler.GetAlerts)                             // GET /api/v1/alerts?cluster=X
		alertsGroup.GET("/summary", alertsHandler.GetAlertsSummary)              // GET /api/v1/alerts/summary?cluster=X
		alertsGroup.GET("/hpa", alertsHandler.GetHPAAlerts)                      // GET /api/v1/alerts/hpa?cluster=X
		alertsGroup.GET("/hpa/namespace", alertsHandler.GetHPAAlertsByNamespace) // GET /api/v1/alerts/hpa/namespace?cluster=X&namespace=Y
		alertsGroup.GET("/nodepool", alertsHandler.GetNodePoolAlerts)            // GET /api/v1/alerts/nodepool?cluster=X
	}

	// VPN Status Check — testa cluster específico via ?cluster=<name>
	vpnHandler := handlers.NewVPNHandler(s.kubeManager)
	s.router.GET("/api/v1/vpn/status", vpnHandler.CheckStatus)

	// Service Mesh (Istio/Kiali Integration) - SEM AUTH (operações de leitura públicas)
	serviceMeshHandler := handlers.NewServiceMeshHandler(s.kubeManager)
	s.router.GET("/api/v1/servicemesh/graph", serviceMeshHandler.GetServiceGraph)    // GET /api/v1/servicemesh/graph?cluster=X&namespace=Y&duration=60s
	s.router.GET("/api/v1/servicemesh/namespaces", serviceMeshHandler.GetNamespaces) // GET /api/v1/servicemesh/namespaces?cluster=X
	s.router.GET("/api/v1/servicemesh/metrics", serviceMeshHandler.GetMetrics)       // GET /api/v1/servicemesh/metrics?cluster=X&namespace=Y

	// GitHub Tokens Store (tokens individuais por usuário)
	fmt.Println("🔑 Inicializando GitHub Tokens Store...")
	var githubTokenStore *storage.GitHubTokenStore
	githubTokenStore, err = storage.NewGitHubTokenStore()
	if err != nil {
		fmt.Printf("⚠️  Falha ao inicializar GitHub Tokens Store: %v\n", err)
		fmt.Println("   Tokens individuais não estarão disponíveis (fallback para GITHUB_TOKEN)")
		githubTokenStore = nil
	} else {
		fmt.Println("✅ GitHub Tokens Store inicializado (AES-256-GCM encryption)")
	}

	// GitHub Releases Compare - SEM AUTH (operações de leitura públicas)
	// Registry pode ser nil (graceful degradation se base não estiver disponível)
	fmt.Println("🐙 Inicializando GitHub Releases Handler...")
	var githubRegistry *storage.DeploymentRegistry
	githubRegistry, err = storage.NewDeploymentRegistry()
	if err != nil {
		fmt.Printf("⚠️  Falha ao inicializar Deployment Registry para GitHub: %v\n", err)
		fmt.Println("   GitHub Releases continuará funcionando com funcionalidade limitada")
		githubRegistry = nil // Continua sem registry (graceful degradation)
	} else {
		fmt.Println("✅ Deployment Registry inicializado (base de conhecimento para GitHub)")
	}

	// Criar logger para GitHub handler
	githubLogger := zerolog.New(os.Stdout).With().Timestamp().Str("component", "github-releases").Logger()
	githubHandler := handlers.NewGitHubReleasesHandler(githubRegistry, githubTokenStore, s.kubeManager, &githubLogger)

	// Rotas que precisam de token individual do usuário (usar middleware InjectUserEmail)
	api.GET("/github/repos", rbacMiddleware.InjectUserEmail(), githubHandler.GetRepos)
	api.GET("/github/user/repos", rbacMiddleware.InjectUserEmail(), githubHandler.ListUserRepos)
	api.GET("/github/repos/:owner/:repo/releases", rbacMiddleware.InjectUserEmail(), githubHandler.GetReleases)
	api.GET("/github/repos/:owner/:repo/compare/:basehead", rbacMiddleware.InjectUserEmail(), githubHandler.CompareReleases)
	api.GET("/github/deployments/search", rbacMiddleware.InjectUserEmail(), githubHandler.SearchDeployments)
	api.GET("/github/deployments/production", rbacMiddleware.InjectUserEmail(), githubHandler.GetProductionDeployment)
	api.GET("/github/deployments/all-versions", rbacMiddleware.InjectUserEmail(), githubHandler.GetAllVersions)
	api.GET("/github/deployments/registry", rbacMiddleware.InjectUserEmail(), githubHandler.GetDeploymentsRegistry)
	api.GET("/github/compare", rbacMiddleware.InjectUserEmail(), githubHandler.CompareReleasesWithRegistry)
	api.POST("/github/deployments/scan", rbacMiddleware.InjectUserEmail(), githubHandler.ScanDeployments)
	fmt.Println("✅ GitHub Releases routes registradas (com autenticação de usuário)")

	// GitHub Tokens Management (gerenciamento de tokens individuais)
	if githubTokenStore != nil {
		githubTokensHandler := handlers.NewGitHubTokensHandler(githubTokenStore, &githubLogger)
		// Rotas requerem injeção de user_email via RBAC middleware
		api.GET("/github/token/status", rbacMiddleware.InjectUserEmail(), githubTokensHandler.GetTokenStatus)
		api.POST("/github/token", rbacMiddleware.InjectUserEmail(), githubTokensHandler.SaveToken)
		api.DELETE("/github/token", rbacMiddleware.InjectUserEmail(), githubTokensHandler.DeleteToken)
		fmt.Println("✅ GitHub Tokens routes registradas")
	}

	// ServiceNow Integration (importação de dados de CHG)
	// Tenta scraping via HTTP, se falhar sugere usar aba "Texto Manual"
	// Também suporta Playwright para extração via browser com Azure AD SSO
	serviceNowHandler := handlers.NewServiceNowHandler(&githubLogger)
	servicenow := api.Group("/servicenow")
	{
		servicenow.POST("/import", serviceNowHandler.ImportFromURL)
		servicenow.POST("/parse", serviceNowHandler.ImportFromDescription)
		servicenow.GET("/extract-sysid", serviceNowHandler.ExtractSysID)
		// Playwright (browser automation com Azure AD SSO)
		servicenow.POST("/extract-playwright", serviceNowHandler.ExtractWithPlaywright)
		servicenow.GET("/playwright-status", serviceNowHandler.GetPlaywrightStatus)
		// Gerenciamento de sessão do Playwright
		servicenow.GET("/session-status", serviceNowHandler.GetSessionStatus)
		servicenow.DELETE("/session", serviceNowHandler.ClearSession)
		servicenow.POST("/session/test", serviceNowHandler.TestSession)
		// Configuração de browser (modo Windows via CDP)
		servicenow.GET("/browser-config", serviceNowHandler.GetBrowserConfig)
		servicenow.POST("/browser-config", serviceNowHandler.SetBrowserConfig)
	}
	fmt.Println("✅ ServiceNow Integration routes registradas (HTTP + Playwright + Session)")

	// SRE Approval Integration (aprovação de deployments)
	sreApprovalHandler := handlers.NewSREApprovalHandler(&githubLogger)
	sreApproval := api.Group("/sre-approval")
	{
		sreApproval.GET("/info", sreApprovalHandler.GetApprovalInfo)
		sreApproval.POST("/approve", rbacMiddleware.RequireSREGroup(), sreApprovalHandler.Approve)
		sreApproval.GET("/extract-id", sreApprovalHandler.ExtractApprovalID)
		sreApproval.GET("/current-user", sreApprovalHandler.GetCurrentUser)
	}
	fmt.Println("✅ SRE Approval Integration routes registradas")

	// Sessions
	sessionHandler := handlers.NewSessionsHandler()
	api.GET("/sessions", sessionHandler.ListAllSessions)
	api.GET("/sessions/folders", sessionHandler.ListSessionFolders)
	api.GET("/sessions/folders/:folder", sessionHandler.ListSessionsInFolder)
	api.GET("/sessions/:name", sessionHandler.GetSession)
	api.POST("/sessions", sessionHandler.SaveSession)
	api.PUT("/sessions/:name", sessionHandler.UpdateSession)
	api.DELETE("/sessions/:name", sessionHandler.DeleteSession)
	api.PUT("/sessions/:name/rename", sessionHandler.RenameSession)
	api.GET("/sessions/templates", sessionHandler.GetSessionTemplates)

	// Logs
	logsHandler := handlers.NewLogsHandler(s.logBuffer)
	api.GET("/logs", logsHandler.GetLogs)
	api.DELETE("/logs", logsHandler.ClearLogs)

	// Monitoring V2 (NOVO - sem port-forwards, acesso direto ao Prometheus)
	monitoringHandlerV2 := handlers.NewMonitoringHandlerV2(s.monitoringEngineV2, s.kubeManager)

	// Rotas V1 (compatibilidade - redireciona para V2)
	monitoring := api.Group("/monitoring")
	{
		monitoring.GET("/status", monitoringHandlerV2.GetStatus)
		monitoring.POST("/start", monitoringHandlerV2.Start)
		monitoring.POST("/stop", monitoringHandlerV2.Stop)
		monitoring.POST("/hpa", monitoringHandlerV2.AddHPA)                                            // Adicionar HPA individual
		monitoring.GET("/metrics/:cluster/:namespace/:hpaName", monitoringHandlerV2.GetMetrics)        // Métricas históricas
		monitoring.GET("/current/:cluster/:namespace/:hpaName", monitoringHandlerV2.GetCurrentMetrics) // Snapshot atual
		monitoring.DELETE("/cache/:cluster/:namespace/:hpaName", monitoringHandlerV2.ClearCache)       // Limpar cache

		// Rotas que o frontend chama mas não existem em V2 - retornar 200 com placeholder
		monitoring.POST("/sync", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok", "message": "V2 não requer sync (cache on-demand)"})
		})
		monitoring.GET("/health/:cluster/:namespace/:hpaName", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "healthy", "message": "V2 usa cache on-demand"})
		})
	}

	// Rotas V2 (diretas)
	monitoringV2 := api.Group("/monitoring/v2")
	{
		monitoringV2.GET("/metrics/:cluster/:namespace/:hpaName", monitoringHandlerV2.GetMetrics)        // Métricas históricas
		monitoringV2.GET("/current/:cluster/:namespace/:hpaName", monitoringHandlerV2.GetCurrentMetrics) // Snapshot atual
		monitoringV2.GET("/status", monitoringHandlerV2.GetStatus)                                       // Status da engine
		monitoringV2.POST("/start", monitoringHandlerV2.Start)                                           // Iniciar engine
		monitoringV2.POST("/stop", monitoringHandlerV2.Stop)                                             // Parar engine
		monitoringV2.POST("/hpa", monitoringHandlerV2.AddHPA)                                            // Adicionar HPA (apenas cache)
		monitoringV2.DELETE("/cache/:cluster/:namespace/:hpaName", monitoringHandlerV2.ClearCache)       // Limpar cache
	}

	// History
	historyHandler := handlers.NewHistoryHandler(s.historyTracker)
	api.GET("/history", historyHandler.GetHistory)
	api.GET("/history/stats", historyHandler.GetHistoryStats)
	api.GET("/history/cordon-drain", historyHandler.GetCordonDrainHistory) // Endpoint específico para Cordon/Drain
	api.GET("/history/:id", historyHandler.GetHistoryEntry)
	api.DELETE("/history", historyHandler.ClearHistory)

	// Notifications - KISS implementation (in-app para ambientes corporativos)
	notificationHandler := handlers.NewNotificationHandler(s.notificationManager)
	api.POST("/notifications/test", notificationHandler.TestNotification)
	api.GET("/notifications/status", notificationHandler.GetStatus)
	api.POST("/notifications/toggle", notificationHandler.ToggleNotifications)
	api.POST("/notifications/alert", notificationHandler.NotifyAlert)

	// In-App Notifications (funciona sem restrições corporativas)
	api.GET("/notifications/in-app", notificationHandler.GetInAppNotifications)
	api.GET("/notifications/in-app/unread", notificationHandler.GetUnreadNotifications)
	api.PUT("/notifications/in-app/:id/read", notificationHandler.MarkNotificationAsRead)
	api.PUT("/notifications/in-app/read-all", notificationHandler.MarkAllNotificationsAsRead)
	api.DELETE("/notifications/in-app", notificationHandler.ClearAllNotifications)

	// AI Diagnostics (se habilitado)
	if s.aiHandler != nil {
		// Rotas públicas (GET)
		api.GET("/ai/status", s.aiHandler.GetProviderStatus)
		api.GET("/ai/history", s.aiHandler.GetHistory)
		api.GET("/ai/history/:id", s.aiHandler.GetAnalysisByID)
		api.GET("/ai/stats", s.aiHandler.GetStats)

		// Rotas de exportação de relatórios (PDF, Markdown, CSV)
		api.GET("/ai/report/:id/pdf", s.aiHandler.GetReportPDF)
		api.GET("/ai/report/:id/markdown", s.aiHandler.GetReportMarkdown)
		api.GET("/ai/report/:id/csv", s.aiHandler.GetReportCSV)

		// Rotas de escrita (POST, DELETE) - podem adicionar RBAC depois se necessário
		api.POST("/ai/analyze", s.aiHandler.Analyze)
		api.DELETE("/ai/history/:id", s.aiHandler.DeleteAnalysis)

		fmt.Println("✅ AI Diagnostics routes registradas")
	}

	// AI Tokens (gerenciamento de tokens por usuário)
	if s.aiTokensHandler != nil {
		s.aiTokensHandler.RegisterRoutes(api, rbacMiddleware)
		fmt.Println("✅ AI Tokens routes registradas")
	}

	// Predictive Analysis (análise preditiva de deployments)
	// Reutilizar analyzer, tokensStore e config do AI Diagnostics
	fmt.Println("🔮 Inicializando Predictions Store...")
	predictionsDBPath := filepath.Join(baseDir, "predictions.db")
	predictionsDB, err := storage.NewSQLiteClient(predictionsDBPath)
	if err != nil {
		fmt.Printf("⚠️  Erro ao criar Predictions DB: %v\n", err)
	} else {
		fmt.Println("✅ Predictions DB criado com sucesso")
	}

	var predictionsHandler *handlers.PredictionsHandler
	if s.aiHandler != nil && s.kubeManagerWrapper != nil {
		// Se AI está habilitado, predictions pode usar os mesmos recursos
		predictionsStore := storage.NewPredictionsStore(predictionsDB)
		predictionsHandler = handlers.NewPredictionsHandler(
			s.kubeManager,                  // KubeConfigManager para pegar clients
			s.kubeManagerWrapper,           // KubeManager para criar AI analyzers
			s.aiHandler.GetAnalyzer(),      // Compartilhar analyzer
			s.aiHandler.GetTokensStore(),   // Compartilhar tokensStore
			predictionsStore,               // Store para histórico
			s.aiHandler.GetDefaultConfig(), // Compartilhar config
		)
	} else {
		// Se AI desabilitado, criar handler básico (sem AI)
		predictionsHandler = handlers.NewPredictionsHandler(s.kubeManager, nil, nil, nil, nil, nil)
		fmt.Println("⚠️  Predictions sem AI (AI Diagnostics desabilitado)")
	}

	// Middleware de debug para predictions
	api.POST("/predictions/analyze", rbacMiddleware.InjectUserEmail(), func(c *gin.Context) {
		fmt.Println("🔍 [MIDDLEWARE] POST /predictions/analyze - Request chegou!")
		fmt.Printf("   Headers: %+v\n", c.Request.Header)
		fmt.Printf("   ContentType: %s\n", c.ContentType())
		userEmail := c.GetString("user_email")
		fmt.Printf("   User Email (from RBAC): %s\n", userEmail)
		c.Next()
		fmt.Printf("   Response Status: %d\n", c.Writer.Status())
	}, predictionsHandler.AnalyzeDeployment)

	api.POST("/predictions/export", rbacMiddleware.InjectUserEmail(), predictionsHandler.ExportReport)
	api.GET("/predictions/health", rbacMiddleware.InjectUserEmail(), predictionsHandler.GetHealthScore)

	// Rotas de histórico de predictions
	api.GET("/predictions/history", rbacMiddleware.InjectUserEmail(), predictionsHandler.GetHistory)
	api.GET("/predictions/history/:id", rbacMiddleware.InjectUserEmail(), predictionsHandler.GetHistoryByID)
	api.GET("/predictions/history/latest", rbacMiddleware.InjectUserEmail(), predictionsHandler.GetLatestForDeployment)
	api.GET("/predictions/statistics", rbacMiddleware.InjectUserEmail(), predictionsHandler.GetStatistics)

	fmt.Println("✅ Predictions routes registradas")

	// NodePool Predictive Analysis
	fmt.Println("🔮 Inicializando NodePool Predictions Store...")
	nodepoolPredictionsDBPath := filepath.Join(baseDir, "nodepool_predictions.db")
	nodepoolPredictionsStore, npStoreErr := storage.NewNodePoolPredictionsStore(nodepoolPredictionsDBPath)
	if npStoreErr != nil {
		fmt.Printf("⚠️  Erro ao criar NodePool Predictions Store: %v\n", npStoreErr)
	} else {
		fmt.Println("✅ NodePool Predictions Store criado com sucesso")
	}

	var nodepoolPredictionsHandler *handlers.NodePoolPredictionsHandler
	if s.aiHandler != nil && s.kubeManagerWrapper != nil && nodepoolPredictionsStore != nil {
		nodepoolPredictionsHandler = handlers.NewNodePoolPredictionsHandler(
			s.kubeManager,
			s.kubeManagerWrapper,
			s.aiHandler.GetAnalyzer(),
			s.aiHandler.GetTokensStore(),
			nodepoolPredictionsStore,
			s.aiHandler.GetDefaultConfig(),
		)
	} else {
		nodepoolPredictionsHandler = handlers.NewNodePoolPredictionsHandler(s.kubeManager, nil, nil, nil, nodepoolPredictionsStore, nil)
		fmt.Println("⚠️  NodePool Predictions sem AI (AI Diagnostics desabilitado)")
	}

	api.POST("/nodepoolpredictions/analyze", rbacMiddleware.InjectUserEmail(), nodepoolPredictionsHandler.AnalyzeNodePool)
	api.GET("/nodepoolpredictions/history", rbacMiddleware.InjectUserEmail(), nodepoolPredictionsHandler.GetHistory)
	api.GET("/nodepoolpredictions/report/:id/markdown", rbacMiddleware.InjectUserEmail(), nodepoolPredictionsHandler.GetMarkdownReport)

	fmt.Println("✅ NodePool Predictions routes registradas")

	// Health Checking System
	fmt.Println("🏥 Inicializando Health Checking System...")
	healthCheckDBPath := filepath.Join(baseDir, "health_checks.db")
	filtersConfigPath := filepath.Join(baseDir, "health_check_filters.json") // ✅ Config de filtros
	progressTracker := handlers.GetProgressTracker()                         // Reutilizar ProgressTracker global

	healthCheckOrchestrator, err := healthcheck.NewOrchestrator(s.kubeManager, progressTracker, healthCheckDBPath, filtersConfigPath)
	if err != nil {
		fmt.Printf("⚠️  Falha ao criar Health Check Orchestrator: %v\n", err)
	} else {
		// Criar handler
		healthCheckHandler := handlers.NewHealthCheckHandler(s.kubeManager, healthCheckOrchestrator, progressTracker, s.aiTokensStore, s.aiHandler)

		// System Health endpoints (padrão Kubernetes) - sem auth
		systemHealthHandler := handlers.NewSystemHealthHandler(s.kubeManager, healthCheckOrchestrator, updater.Version)
		s.router.GET("/healthz", systemHealthHandler.Health)
		s.router.GET("/healthz/live", systemHealthHandler.Live)
		s.router.GET("/healthz/ready", systemHealthHandler.Ready)

		// Prometheus Metrics endpoint - sem auth (padrão Prometheus)
		metricsHandler := handlers.NewMetricsHandler()
		s.router.GET("/metrics", metricsHandler.Metrics)

		// Rotas de health checking
		healthCheckGroup := api.Group("/healthcheck")
		{
			// Rotas públicas (GET)
			healthCheckGroup.GET("/history", healthCheckHandler.History)
			healthCheckGroup.GET("/stats", healthCheckHandler.Stats)
			healthCheckGroup.GET("/dashboard", healthCheckHandler.DashboardMetrics) // 🆕 Métricas para dashboard visual
			healthCheckGroup.GET("/events/:sessionId", healthCheckHandler.GetEvents) // 🆕 Buscar eventos persistidos
			healthCheckGroup.GET("/:id", healthCheckHandler.Get)

			// Rotas de escrita (POST, DELETE) - SRE only
			healthCheckGroup.POST("/run", rbacMiddleware.RequireSREGroup(), healthCheckHandler.Run)
			healthCheckGroup.POST("/correlated/analyze", rbacMiddleware.RequireSREGroup(), healthCheckHandler.AnalyzeCorrelated)
			healthCheckGroup.POST("/correlated/analyze-batch", rbacMiddleware.RequireSREGroup(), healthCheckHandler.AnalyzeCorrelatedBatch)
			healthCheckGroup.POST("/oneagent/analyze", rbacMiddleware.RequireSREGroup(), healthCheckHandler.AnalyzeOneAgentSignal)
			healthCheckGroup.DELETE("/cancel/:sessionId", rbacMiddleware.RequireSREGroup(), healthCheckHandler.Cancel) // ✅ Cancelar health check
			healthCheckGroup.DELETE("/:id", rbacMiddleware.RequireSREGroup(), healthCheckHandler.Delete)
		}

		// SSE stream - requer token via query param (EventSource não suporta headers)
		sseGroup := s.router.Group("/api/v1/healthcheck")
		sseGroup.Use(middleware.WebSocketAuthMiddleware(s.token))
		{
			sseGroup.GET("/progress", healthCheckHandler.Progress)                      // Original: 1 conexão por cluster
			sseGroup.GET("/progress-multiplex", healthCheckHandler.ProgressMultiplexed) // 🆕 Multiplexado: 1 conexão para TODOS os clusters
		}

		fmt.Println("✅ Health Checking routes registradas")

		// ✅ Rotas de Filtros (Health Check Filters Management)
		filtersHandler := handlers.NewFiltersHandler(healthCheckOrchestrator)
		filtersGroup := api.Group("/filters")
		{
			// Rotas públicas (GET)
			filtersGroup.GET("", filtersHandler.ListRules)                // Listar regras
			filtersGroup.GET("/categories", filtersHandler.GetCategories) // Listar categorias

			// Rotas de escrita (POST, DELETE) - SRE only
			filtersGroup.POST("", rbacMiddleware.RequireSREGroup(), filtersHandler.AddRule)          // Adicionar regra
			filtersGroup.DELETE("/:id", rbacMiddleware.RequireSREGroup(), filtersHandler.RemoveRule) // Remover regra
		}

		fmt.Println("✅ Health Check Filters routes registradas")

		// ✅ Rotas de Dependency Scanner com SQLite Registry
		dependencyScanner := healthcheck.NewDependencyScanner(s.kubeManager)
		dependencyRegistry, err := storage.NewDependencyRegistry()
		if err != nil {
			fmt.Printf("⚠️  Erro ao criar dependency registry: %v\n", err)
		} else {
			fmt.Println("✅ Dependency Registry (SQLite) inicializado")
		}

		// Injetar stores no orchestrator para enriquecimento dos OneAgent Signals
		healthCheckOrchestrator.SetStores(s.npRegistryStore, dependencyRegistry)
		// Injetar orchestrator e dependency registry no DT handler para investigação profunda
		dtHandler.SetInvestigateStores(healthCheckOrchestrator, dependencyRegistry)
		dependenciesHandler := handlers.NewDependenciesHandler(dependencyScanner, dependencyRegistry)
		dependenciesGroup := api.Group("/dependencies")
		{
			// Rotas públicas (GET) - Consultam SQLite
			dependenciesGroup.GET("/search", dependenciesHandler.Search)                        // Busca reversa no SQLite
			dependenciesGroup.GET("/registry", dependenciesHandler.GetRegistry)                 // Todas dependências do SQLite
			dependenciesGroup.GET("/stats", dependenciesHandler.GetStats)                       // Estatísticas do registry
			dependenciesGroup.GET("/clusters", dependenciesHandler.GetClusters)                 // Lista clusters únicos do SQLite
			dependenciesGroup.GET("/scan/history", dependenciesHandler.GetScanHistory)          // Histórico de scans
			dependenciesGroup.GET("/export", dependenciesHandler.Export)                        // Exportar CSV/JSON/Markdown do SQLite
			dependenciesGroup.GET("/service/:serviceName", dependenciesHandler.GetServiceUsage) // Uso de serviço específico

			// Rotas de escrita (POST) - Escaneiam K8s e persistem no SQLite
			dependenciesGroup.POST("/scan", dependenciesHandler.Scan)                        // Scan múltiplos clusters (sem RBAC: leitura K8s)
			dependenciesGroup.POST("/scan/:cluster", dependenciesHandler.ScanCluster)          // Scan cluster único (sem RBAC: auto-scan)
			dependenciesGroup.POST("/cache/clear", rbacMiddleware.RequireSREGroup(), dependenciesHandler.ClearCache)      // Limpar cache em memória
		}

		fmt.Println("✅ Dependency Scanner routes registradas (com SQLite)")
	}
}

// setupStatic configura servir arquivos estáticos
func (s *Server) setupStatic() {
	// Criar filesystem com prefixo "static/"
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		s.router.GET("/", func(c *gin.Context) {
			c.String(500, "Failed to load static files")
		})
		return
	}

	// Servir diretório assets (JS, CSS)
	assetsFS, err := fs.Sub(staticFS, "assets")
	if err != nil {
		s.router.GET("/", func(c *gin.Context) {
			c.String(500, "Failed to load assets")
		})
		return
	}
	s.router.StaticFS("/assets", http.FS(assetsFS))

	// Servir arquivos individuais na raiz
	s.router.StaticFileFS("/favicon.ico", "favicon.ico", http.FS(staticFS))
	s.router.StaticFileFS("/robots.txt", "robots.txt", http.FS(staticFS))
	s.router.StaticFileFS("/placeholder.svg", "placeholder.svg", http.FS(staticFS))

	// Rota raiz serve index.html (sem cache)
	s.router.GET("/", func(c *gin.Context) {
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			c.String(404, "Frontend not found - run 'make web-build' first")
			return
		}
		// Headers para prevenir cache
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Data(200, "text/html; charset=utf-8", data)
	})

	// SPA fallback - todas as rotas não-API servem index.html
	s.router.NoRoute(func(c *gin.Context) {
		// Não interceptar requisições de assets
		if len(c.Request.URL.Path) >= 7 && c.Request.URL.Path[:7] == "/assets" {
			c.String(404, "Asset not found")
			return
		}
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.String(404, "API endpoint not found")
			return
		}

		// SPA fallback para outras rotas (sem cache)
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			c.String(404, "Not found")
			return
		}
		// Headers para prevenir cache
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Data(200, "text/html; charset=utf-8", data)
	})
}

// startInactivityMonitor inicia o monitoramento de inatividade
func (s *Server) startInactivityMonitor() {
	// Timer inicial de 30 minutos (mais tempo que o normal para dar tempo do primeiro heartbeat)
	// O primeiro heartbeat do frontend vai resetar para 25 minutos (margem de 5min)
	s.timerMutex.Lock()
	s.shutdownTimer = time.AfterFunc(30*time.Minute, s.autoShutdown)
	s.timerMutex.Unlock()

	fmt.Println("⏰ Monitor de inatividade ativado:")
	fmt.Println("   - Frontend deve enviar heartbeat a cada 5 minutos")
	fmt.Println("   - Servidor desligará após 25 minutos sem heartbeat (margem de segurança)")
	fmt.Println("   - Timer inicial: 30 minutos (aguardando primeiro heartbeat)")
}

// autoShutdown desliga o servidor automaticamente por inatividade
func (s *Server) autoShutdown() {
	s.heartbeatMutex.RLock()
	lastHeartbeat := s.lastHeartbeat
	s.heartbeatMutex.RUnlock()

	timeSinceLastHeartbeat := time.Since(lastHeartbeat)

	// IMPORTANTE: Verificar se realmente passaram pelo menos 20 minutos
	// Margem de segurança: timer está configurado para 25 minutos,
	// mas verificamos se passou o mínimo de 20 minutos antes de desligar
	// Isso protege contra race conditions, atrasos de rede, etc.
	if timeSinceLastHeartbeat < 20*time.Minute {
		fmt.Printf("⚠️  Timer de shutdown disparou prematuramente (apenas %.1f minutos)\n", timeSinceLastHeartbeat.Minutes())
		fmt.Println("✅ Heartbeat ainda ativo, shutdown cancelado")

		// Resetar timer para evitar disparo prematuro novamente
		s.timerMutex.Lock()
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
		// Esperar o tempo restante até completar os 20 minutos + margem
		remaining := (20 * time.Minute) - timeSinceLastHeartbeat + (5 * time.Minute)
		if remaining < 1*time.Minute {
			remaining = 1 * time.Minute // Mínimo de 1 minuto
		}
		s.shutdownTimer = time.AfterFunc(remaining, s.autoShutdown)
		s.timerMutex.Unlock()

		fmt.Printf("⏰ Timer resetado para mais %.1f minutos\n", remaining.Minutes())
		return
	}

	fmt.Println("\n╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║             AUTO-SHUTDOWN POR INATIVIDADE                 ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Printf("⏰ Último heartbeat: %s (há %.0f minutos)\n",
		lastHeartbeat.Format("15:04:05"),
		timeSinceLastHeartbeat.Minutes())
	fmt.Println("🛑 Nenhuma página web conectada por mais de 20 minutos")
	fmt.Println("✅ Servidor sendo encerrado...")

	os.Exit(0)
}

// Start inicia o servidor HTTP
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)

	// Iniciar Monitoring Engine V2
	if err := s.monitoringEngineV2.Start(); err != nil {
		fmt.Printf("⚠️  Aviso: Falha ao iniciar Monitoring Engine V2: %v\n", err)
		fmt.Println("   Continuando sem monitoramento...")
	} else {
		fmt.Println("✅ Monitoring Engine V2 iniciado com sucesso")
	}

	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║       k8s-hpa-manager - Web Interface (POC)              ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Printf("\n")
	fmt.Printf("🌐 Server URL:    http://localhost%s\n", addr)
	fmt.Printf("📍 API Endpoint:  http://localhost%s/api/v1\n", addr)
	fmt.Printf("🔐 Auth Token:    %s\n", s.token)
	fmt.Printf("❤️  Health Check: http://localhost%s/health\n", addr)
	fmt.Printf("💓 Heartbeat:     POST http://localhost%s/heartbeat\n", addr)
	fmt.Printf("\n")
	fmt.Println("📝 Exemplo de uso:")
	fmt.Printf("   curl -H 'Authorization: Bearer %s' http://localhost%s/api/v1/clusters\n", s.token, addr)
	fmt.Printf("\n")
	fmt.Println("🚀 Servidor iniciado! Pressione Ctrl+C para parar.")
	fmt.Printf("\n")

	return s.router.Run(addr)
}

// Shutdown encerra gracefully o servidor e componentes
func (s *Server) Shutdown() error {
	fmt.Println("\n╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║              GRACEFUL SHUTDOWN INICIADO                   ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")

	// 1. Parar timer de auto-shutdown
	s.timerMutex.Lock()
	if s.shutdownTimer != nil {
		s.shutdownTimer.Stop()
		fmt.Println("✓ Timer de auto-shutdown parado")
	}
	s.timerMutex.Unlock()

	// 2. Parar Monitoring Engine V2
	if s.monitoringEngineV2 != nil && s.monitoringEngineV2.IsRunning() {
		if err := s.monitoringEngineV2.Stop(); err != nil {
			fmt.Printf("⚠️  Erro ao parar Monitoring Engine V2: %v\n", err)
		} else {
			fmt.Println("✓ Monitoring Engine V2 parado")
		}
	}

	// TODO: Remover após migração completa para V2
	// if s.monitoringCancel != nil { s.monitoringCancel() }
	// if s.monitoringEngine != nil { s.monitoringEngine.Stop() }
	// Salvar targets, fechar canais, etc...

	fmt.Println("\n✅ Shutdown concluído com sucesso!")
	return nil
}

// saveTargetsToFile salva targets em arquivo JSON
func saveTargetsToFile(filename string, targets []scanner.ScanTarget) error {
	data, err := json.Marshal(targets)
	if err != nil {
		return fmt.Errorf("failed to marshal targets: %w", err)
	}

	// Criar diretório se não existir
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// loadTargetsFromFile carrega targets de arquivo JSON
func loadTargetsFromFile(filename string) ([]scanner.ScanTarget, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []scanner.ScanTarget{}, nil // Arquivo não existe = sem targets
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var targets []scanner.ScanTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("failed to unmarshal targets: %w", err)
	}

	return targets, nil
}

// TODO: Remover após migração completa para V2
// func (s *Server) startTargetsPersistence(filename string, interval time.Duration) { ... }
