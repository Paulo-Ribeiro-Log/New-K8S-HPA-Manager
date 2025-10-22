package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/web/handlers"
	"k8s-hpa-manager/internal/web/middleware"
)

//go:embed static/*
var staticFiles embed.FS

// Server representa o servidor HTTP
type Server struct {
	router         *gin.Engine
	kubeManager    *config.KubeConfigManager
	port           int
	token          string
	lastHeartbeat  time.Time
	heartbeatMutex sync.RWMutex
	shutdownTimer  *time.Timer
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

	server := &Server{
		router:        router,
		kubeManager:   kubeManager,
		port:          port,
		token:         token,
		lastHeartbeat: time.Now(),
	}

	server.setupMiddleware()
	server.setupRoutes()
	server.setupStatic()
	server.startInactivityMonitor()

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

	// Logging
	s.router.Use(gin.Logger())

	// Recovery
	s.router.Use(gin.Recovery())
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
		s.heartbeatMutex.Lock()
		s.lastHeartbeat = time.Now()
		s.heartbeatMutex.Unlock()

		// Resetar timer de shutdown
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
		s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)

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
	hpaHandler := handlers.NewHPAHandler(s.kubeManager)
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

	// Validation (VPN + Azure CLI)
	validationHandler := handlers.NewValidationHandler()
	api.GET("/validate", validationHandler.Validate)

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
	// Timer inicial de 20 minutos
	s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)

	fmt.Println("⏰ Monitor de inatividade ativado:")
	fmt.Println("   - Frontend deve enviar heartbeat a cada 5 minutos")
	fmt.Println("   - Servidor desligará após 20 minutos sem heartbeat")
}

// autoShutdown desliga o servidor automaticamente por inatividade
func (s *Server) autoShutdown() {
	s.heartbeatMutex.RLock()
	lastHeartbeat := s.lastHeartbeat
	s.heartbeatMutex.RUnlock()

	timeSinceLastHeartbeat := time.Since(lastHeartbeat)

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
