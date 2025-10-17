package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"

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
	router      *gin.Engine
	kubeManager *config.KubeConfigManager
	port        int
	token       string
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
	router := gin.Default()

	server := &Server{
		router:      router,
		kubeManager: kubeManager,
		port:        port,
		token:       token,
	}

	server.setupMiddleware()
	server.setupRoutes()
	server.setupStatic()

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

	// Validation (VPN + Azure CLI)
	validationHandler := handlers.NewValidationHandler()
	api.GET("/validate", validationHandler.Validate)
}

// setupStatic configura servir arquivos estáticos
func (s *Server) setupStatic() {
	// Servir arquivos estáticos do embed
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Se não conseguir carregar do embed, criar handler vazio
		s.router.GET("/", func(c *gin.Context) {
			c.HTML(200, "index.html", nil)
		})
		return
	}

	s.router.StaticFS("/assets", http.FS(staticFS))

	// Rota raiz serve index.html
	s.router.GET("/", func(c *gin.Context) {
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			c.String(404, "Frontend not found - run 'make web-build' first")
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})

	// SPA fallback - todas as rotas não-API servem index.html
	s.router.NoRoute(func(c *gin.Context) {
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			c.String(404, "Not found")
			return
		}
		c.Data(200, "text/html; charset=utf-8", data)
	})
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
	fmt.Printf("\n")
	fmt.Println("📝 Exemplo de uso:")
	fmt.Printf("   curl -H 'Authorization: Bearer %s' http://localhost%s/api/v1/clusters\n", s.token, addr)
	fmt.Printf("\n")
	fmt.Println("🚀 Servidor iniciado! Pressione Ctrl+C para parar.")
	fmt.Printf("\n")

	return s.router.Run(addr)
}
