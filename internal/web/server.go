package web

import (
	"context"
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
	"k8s-hpa-manager/internal/monitoring/analyzer"
	"k8s-hpa-manager/internal/monitoring/engine"
	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/scanner"
	"k8s-hpa-manager/internal/web/handlers"
	"k8s-hpa-manager/internal/web/middleware"
)

//go:embed all:static
var staticFiles embed.FS

// Server representa o servidor HTTP
type Server struct {
	router          *gin.Engine
	kubeManager     *config.KubeConfigManager
	port            int
	token           string
	lastHeartbeat   time.Time
	heartbeatMutex  sync.RWMutex
	shutdownTimer   *time.Timer
	timerMutex      sync.Mutex // Protege operações no timer
	logBuffer       *handlers.LogBuffer
	historyTracker  *history.HistoryTracker

	// Monitoring engine (NOVO)
	monitoringEngine *engine.ScanEngine
	snapshotChan     chan *models.HPASnapshot
	anomalyChan      chan analyzer.Anomaly
	stressResultChan chan *models.StressTestMetrics
	monitoringCtx    context.Context
	monitoringCancel context.CancelFunc
}

// NewServer cria uma nova instância do servidor web
func NewServer(kubeconfig string, port int, debug bool) (*Server, error) {
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

	// Criar canais para monitoring engine
	snapshotChan := make(chan *models.HPASnapshot, 100)
	anomalyChan := make(chan analyzer.Anomaly, 100)
	stressResultChan := make(chan *models.StressTestMetrics, 10)

	// Criar contexto para monitoring
	monitoringCtx, monitoringCancel := context.WithCancel(context.Background())

	// Carregar targets salvos (se existirem)
	targetsFile := filepath.Join(baseDir, "monitoring-targets.json")
	savedTargets, err := loadTargetsFromFile(targetsFile)
	if err != nil {
		fmt.Printf("⚠️  Não foi possível carregar targets salvos: %v\n", err)
		savedTargets = []scanner.ScanTarget{}
	} else if len(savedTargets) > 0 {
		fmt.Printf("📂 %d target(s) restaurado(s) do arquivo\n", len(savedTargets))
	}

	// Configuração do monitoring engine com targets restaurados
	scanConfig := &scanner.ScanConfig{
		Mode:        scanner.ScanModeIndividual,
		Targets:     savedTargets, // Restaura targets salvos
		Interval:    1 * time.Minute,
		Duration:    0,
		AutoStart:   len(savedTargets) > 0, // Inicia automaticamente se houver targets salvos
		Name:        "Web Monitoring",
		Description: "Monitoring engine para interface web",
		CreatedAt:   time.Now(),
	}

	// Criar monitoring engine
	monitoringEngine := engine.New(scanConfig, snapshotChan, anomalyChan, stressResultChan)

	// Iniciar engine automaticamente se houver targets
	if len(savedTargets) > 0 {
		fmt.Printf("🚀 Iniciando monitoring engine com %d cluster(s)...\n", len(savedTargets))
		go func() {
			time.Sleep(2 * time.Second) // Aguarda servidor estabilizar
			if err := monitoringEngine.Start(); err != nil {
				fmt.Printf("❌ Erro ao iniciar monitoring engine: %v\n", err)
			} else {
				fmt.Printf("✅ Monitoring engine iniciado automaticamente\n")
			}
		}()
	}

	server := &Server{
		router:           router,
		kubeManager:      kubeManager,
		port:             port,
		token:            token,
		lastHeartbeat:    time.Now(),
		logBuffer:        logBuffer,
		historyTracker:   historyTracker,
		monitoringEngine: monitoringEngine,
		snapshotChan:     snapshotChan,
		anomalyChan:      anomalyChan,
		stressResultChan: stressResultChan,
		monitoringCtx:    monitoringCtx,
		monitoringCancel: monitoringCancel,
	}

	server.setupMiddleware()
	server.setupRoutes()
	server.setupStatic()
	server.startInactivityMonitor()

	// Iniciar persistência de targets em background
	go server.startTargetsPersistence(targetsFile, 30*time.Second)

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

	// API v1 (com auth)
	api := s.router.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(s.token))

	// Clusters
	clusterHandler := handlers.NewClusterHandler(s.kubeManager)
	api.GET("/clusters", clusterHandler.List)
	api.GET("/clusters/:name/test", clusterHandler.Test)
	api.GET("/clusters/:name/config", clusterHandler.GetClusterConfig)
	api.POST("/clusters/:name/context", clusterHandler.SwitchToClusterContext)
	api.POST("/clusters/switch-context", clusterHandler.SwitchContext)
	api.GET("/clusters/info", clusterHandler.GetClusterInfo)

	// Azure
	azureHandler := handlers.NewAzureHandler()
	api.POST("/azure/subscription", azureHandler.SetSubscription)

	// Namespaces
	namespaceHandler := handlers.NewNamespaceHandler(s.kubeManager)
	api.GET("/namespaces", namespaceHandler.List)

	// HPAs
	hpaHandler := handlers.NewHPAHandler(s.kubeManager, s.historyTracker)
	api.GET("/hpas", hpaHandler.List)
	api.GET("/hpas/:cluster/:namespace/:name", hpaHandler.Get)
	api.PUT("/hpas/:cluster/:namespace/:name", hpaHandler.Update)

	// Node Pools
	nodePoolHandler := handlers.NewNodePoolHandler(s.kubeManager)
	api.GET("/nodepools", nodePoolHandler.List)
	api.PUT("/nodepools/:cluster/:resource_group/:name", nodePoolHandler.Update)
	api.POST("/nodepools/apply-sequential", nodePoolHandler.ApplySequential)

	// CronJobs
	cronJobHandler := handlers.NewCronJobHandler(s.kubeManager)
	api.GET("/cronjobs", cronJobHandler.List)
	api.PUT("/cronjobs/:cluster/:namespace/:name", cronJobHandler.Update)

	// Prometheus Stack
	prometheusHandler := handlers.NewPrometheusHandler(s.kubeManager)
	api.GET("/prometheus", prometheusHandler.List)
	api.PUT("/prometheus/:cluster/:namespace/:type/:name", prometheusHandler.Update)
	api.POST("/prometheus/:cluster/:namespace/:type/:name/rollout", prometheusHandler.Rollout)

	// Validation (VPN + Azure CLI)
	validationHandler := handlers.NewValidationHandler()
	api.GET("/validate", validationHandler.Validate)

	// VPN Status Check (sem auth para polling leve)
	s.router.GET("/api/v1/vpn/status", handlers.CheckVPNConnection)

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

	// Monitoring (NOVO - FASE 4: Passa persistence para query SQLite direto)
	persistence := s.monitoringEngine.GetPersistence()
	monitoringHandler := handlers.NewMonitoringHandler(s.monitoringEngine, persistence, s.anomalyChan, s.snapshotChan)
	monitoring := api.Group("/monitoring")
	{
		monitoring.GET("/metrics/:cluster/:namespace/:hpaName", monitoringHandler.GetMetrics)
		monitoring.GET("/anomalies", monitoringHandler.GetAnomalies)
		monitoring.GET("/health/:cluster/:namespace/:hpaName", monitoringHandler.GetHealth)
		monitoring.GET("/status", monitoringHandler.GetStatus)
		monitoring.POST("/start", monitoringHandler.Start)
		monitoring.POST("/stop", monitoringHandler.Stop)

		// Target management (NOVO)
		monitoring.GET("/targets", monitoringHandler.GetTargets)
		monitoring.POST("/targets", monitoringHandler.AddTarget)
		monitoring.POST("/hpa", monitoringHandler.AddHPA) // Adicionar HPA individual
		monitoring.DELETE("/targets/:cluster", monitoringHandler.RemoveTarget)
	}

	// History
	historyHandler := handlers.NewHistoryHandler(s.historyTracker)
	api.GET("/history", historyHandler.GetHistory)
	api.GET("/history/stats", historyHandler.GetHistoryStats)
	api.GET("/history/:id", historyHandler.GetHistoryEntry)
	api.DELETE("/history", historyHandler.ClearHistory)
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

	// 2. Cancelar contexto de monitoring
	if s.monitoringCancel != nil {
		s.monitoringCancel()
		fmt.Println("✓ Contexto de monitoring cancelado")
	}

	// 3. Parar monitoring engine (fecha port-forwards e SQLite)
	if s.monitoringEngine != nil {
		fmt.Println("⏳ Parando monitoring engine...")
		if err := s.monitoringEngine.Stop(); err != nil {
			fmt.Printf("⚠️  Erro ao parar engine: %v\n", err)
		} else {
			fmt.Println("✓ Monitoring engine parado")
		}
	}

	// 4. Salvar targets uma última vez
	homeDir, _ := os.UserHomeDir()
	baseDir := filepath.Join(homeDir, ".k8s-hpa-manager")
	targetsFile := filepath.Join(baseDir, "monitoring-targets.json")
	if s.monitoringEngine != nil {
		targets := s.monitoringEngine.GetTargets()
		if err := saveTargetsToFile(targetsFile, targets); err != nil {
			fmt.Printf("⚠️  Erro ao salvar targets: %v\n", err)
		} else {
			fmt.Println("✓ Targets salvos")
		}
	}

	// 5. Fechar canais
	close(s.snapshotChan)
	close(s.anomalyChan)
	close(s.stressResultChan)
	fmt.Println("✓ Canais fechados")

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

// startTargetsPersistence persiste targets periodicamente em arquivo
func (s *Server) startTargetsPersistence(filename string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Buscar targets atuais do engine
			targets := s.monitoringEngine.GetTargets()

			// Salvar em arquivo
			if err := saveTargetsToFile(filename, targets); err != nil {
				fmt.Printf("⚠️  Erro ao salvar targets: %v\n", err)
			}

		case <-s.monitoringCtx.Done():
			// Servidor sendo encerrado, salvar uma última vez
			targets := s.monitoringEngine.GetTargets()
			saveTargetsToFile(filename, targets)
			return
		}
	}
}
