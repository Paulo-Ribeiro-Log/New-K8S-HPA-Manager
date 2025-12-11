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

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/notifications"
	"k8s-hpa-manager/internal/rbac"

	// TODO: Remover após migração completa para V2
	// "k8s-hpa-manager/internal/monitoring/analyzer"
	// "k8s-hpa-manager/internal/monitoring/engine"
	enginev2 "k8s-hpa-manager/internal/monitoring/engine"
	// "k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/scanner"
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
}

// NewServer cria uma nova instância do servidor web
func NewServer(kubeconfig string, port int, debug bool, disableADAuth bool) (*Server, error) {
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
		fmt.Println("   ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️  ⚠️\n")
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
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
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
	// Health check (sem auth)
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
		s.timerMutex.Lock()
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
		s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)
		s.timerMutex.Unlock()

		// Log para debugging
		fmt.Printf("💓 Heartbeat recebido: %s | Próximo shutdown em: %s\n",
			now.Format("15:04:05"),
			now.Add(20*time.Minute).Format("15:04:05"))

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

	// API v1 (com auth)
	api := s.router.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(s.token))

	// RBAC - Inicializar manager e middleware
	rbacManager := rbac.NewRBACManager(s.disableADAuth)
	rbacMiddleware := middleware.NewRBACMiddleware(rbacManager)

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
	api.GET("/nodepools/disk-metrics", nodePoolHandler.GetNodePoolDiskMetrics) // NOVO: Métricas de disco
	api.GET("/nodepools/storage-overview", nodePoolHandler.GetStorageOverview) // NOVO: Visão geral de storage
	api.GET("/nodepools/sequence/progress", nodePoolHandler.SequenceProgress)  // NOVO: SSE progress tracking

	// Node Pools - Write Operations (SRE-only)
	api.PUT("/nodepools/:cluster/:resource_group/:name", rbacMiddleware.RequireSREGroup(), nodePoolHandler.Update)
	api.POST("/nodepools/apply-sequential", rbacMiddleware.RequireSREGroup(), nodePoolHandler.ApplySequential)
	api.POST("/nodepools/sequence/execute", rbacMiddleware.RequireSREGroup(), nodePoolHandler.ExecuteSequence) // NOVO: Cordon/Drain sequencing

	// SSE Progress Streaming (sem auth para permitir conexão EventSource)
	s.router.GET("/api/v1/nodepools/progress/:operationId", handlers.HandleProgressStream)
	s.router.GET("/api/v1/nodepools/progress/:operationId/status", handlers.HandleProgressStatus)

	// CronJobs
	cronJobHandler := handlers.NewCronJobHandler(s.kubeManager, s.historyTracker)
	api.GET("/cronjobs", cronJobHandler.List)

	// CronJobs - Write Operations (SRE-only)
	api.PUT("/cronjobs/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), cronJobHandler.Update)

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
		configMaps.PUT("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), configMapHandler.Apply)
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
	}

	// Pods/Containers
	podHandler := handlers.NewPodHandler(s.kubeManager, s.historyTracker)
	pods := api.Group("/pods")
	{
		pods.GET("", podHandler.List)
		pods.GET("/:cluster/:namespace/:name", podHandler.Get)
		pods.GET("/:cluster/:namespace/:name/describe", podHandler.Describe)
		pods.GET("/:cluster/:namespace/:name/logs", podHandler.GetLogs)

		// Pods - Write Operations (SRE-only)
		pods.DELETE("/:cluster/:namespace/:name", rbacMiddleware.RequireSREGroup(), podHandler.Delete)
		pods.POST("/:cluster/:namespace/:name/restart", rbacMiddleware.RequireSREGroup(), podHandler.Restart)
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
}

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

	// VPN Status Check (sem auth para polling leve)
	s.router.GET("/api/v1/vpn/status", handlers.CheckVPNConnection)

	// Service Mesh (Istio/Kiali Integration)
	serviceMeshHandler := handlers.NewServiceMeshHandler(s.kubeManager)
	serviceMesh := api.Group("/servicemesh")
	{
		serviceMesh.GET("/graph", serviceMeshHandler.GetServiceGraph)       // GET /api/v1/servicemesh/graph?cluster=X&namespace=Y&duration=60s
		serviceMesh.GET("/namespaces", serviceMeshHandler.GetNamespaces)    // GET /api/v1/servicemesh/namespaces?cluster=X
		serviceMesh.GET("/metrics", serviceMeshHandler.GetMetrics)          // GET /api/v1/servicemesh/metrics?cluster=X&namespace=Y
	}

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
	// O primeiro heartbeat do frontend vai resetar para 20 minutos
	s.timerMutex.Lock()
	s.shutdownTimer = time.AfterFunc(30*time.Minute, s.autoShutdown)
	s.timerMutex.Unlock()

	fmt.Println("⏰ Monitor de inatividade ativado:")
	fmt.Println("   - Frontend deve enviar heartbeat a cada 5 minutos")
	fmt.Println("   - Servidor desligará após 20 minutos sem heartbeat")
	fmt.Println("   - Timer inicial: 30 minutos (aguardando primeiro heartbeat)")
}

// autoShutdown desliga o servidor automaticamente por inatividade
func (s *Server) autoShutdown() {
	s.heartbeatMutex.RLock()
	lastHeartbeat := s.lastHeartbeat
	s.heartbeatMutex.RUnlock()

	timeSinceLastHeartbeat := time.Since(lastHeartbeat)

	// IMPORTANTE: Verificar se realmente passaram 20 minutos
	// (proteção contra race conditions ou timers duplicados)
	if timeSinceLastHeartbeat < 20*time.Minute {
		fmt.Printf("⚠️  Timer de shutdown disparou prematuramente (apenas %.1f minutos)\n", timeSinceLastHeartbeat.Minutes())
		fmt.Println("✅ Heartbeat ainda ativo, shutdown cancelado")
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
