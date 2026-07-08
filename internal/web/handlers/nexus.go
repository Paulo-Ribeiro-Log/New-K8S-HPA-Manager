package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/pkg/nexus"
)

// NexusHandler gerencia requisições relacionadas ao Nexus
type NexusHandler struct {
	configManager nexus.ConfigManager
	client        nexus.Client
}

// NewNexusHandler cria uma nova instância do NexusHandler
func NewNexusHandler() (*NexusHandler, error) {
	configManager, err := nexus.NewFileConfigManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	return &NexusHandler{
		configManager: configManager,
	}, nil
}

// getClient obtém o cliente Nexus com a configuração atual
func (h *NexusHandler) getClient() (nexus.Client, error) {
	if h.client != nil {
		return h.client, nil
	}

	config, err := h.configManager.Load()
	if err != nil {
		return nil, fmt.Errorf("no configuration found. Please configure Nexus connection first")
	}

	h.client = nexus.NewHTTPClient(*config)
	return h.client, nil
}

// refreshClient força a recriação do cliente com nova configuração
func (h *NexusHandler) refreshClient() {
	h.client = nil
}

// TestConnection testa a conexão com o Nexus
// POST /api/v1/nexus/test
func (h *NexusHandler) TestConnection(c *gin.Context) {
	var config nexus.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Valida campos obrigatórios
	if config.BaseURL == "" || config.Repository == "" || config.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "BaseURL, Repository and Username are required",
		})
		return
	}

	// Se senha veio vazia, usar a existente
	if config.Password == "" {
		existingConfig, err := h.configManager.Load()
		if err != nil || existingConfig.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Password is required",
			})
			return
		}
		config.Password = existingConfig.Password
	}

	// Cria cliente temporário para teste
	client := nexus.NewHTTPClient(config)

	response, err := client.TestConnection()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Test failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// SaveConfig salva a configuração do Nexus
// POST /api/v1/nexus/config
func (h *NexusHandler) SaveConfig(c *gin.Context) {
	var config nexus.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Valida campos obrigatórios (exceto senha que pode vir do config existente)
	if config.BaseURL == "" || config.Repository == "" || config.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "BaseURL, Repository and Username are required",
		})
		return
	}

	// Se senha veio vazia, manter a existente
	if config.Password == "" {
		existingConfig, err := h.configManager.Load()
		if err != nil || existingConfig.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Password is required",
			})
			return
		}
		config.Password = existingConfig.Password
	}

	// Define diretório temporário padrão se não fornecido
	if config.TempDir == "" {
		config.TempDir = "/tmp/k8s-hpa-nexus"
	}

	// Salva configuração
	if err := h.configManager.Save(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to save configuration: %v", err),
		})
		return
	}

	// Atualiza cliente
	h.refreshClient()

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration saved successfully",
	})
}

// LoadConfig carrega a configuração do Nexus
// GET /api/v1/nexus/config
func (h *NexusHandler) LoadConfig(c *gin.Context) {
	if !h.configManager.Exists() {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "No configuration found",
		})
		return
	}

	config, err := h.configManager.Load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to load configuration: %v", err),
		})
		return
	}

	// Remove senha da resposta por segurança
	config.Password = ""

	c.JSON(http.StatusOK, config)
}

// DeleteConfig remove a configuração do Nexus
// DELETE /api/v1/nexus/config
func (h *NexusHandler) DeleteConfig(c *gin.Context) {
	if err := h.configManager.Delete(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to delete configuration: %v", err),
		})
		return
	}

	h.refreshClient()

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration deleted successfully",
	})
}

// DownloadValues baixa um arquivo de values
// POST /api/v1/nexus/values/download
func (h *NexusHandler) DownloadValues(c *gin.Context) {
	var req nexus.ValuesFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	fmt.Printf("[NexusHandler] Download request: release=%s, version=%s, env=%s, type=%s\n",
		req.Release, req.Version, req.Environment, req.Type)

	client, err := h.getClient()
	if err != nil {
		fmt.Printf("[NexusHandler] Error getting client: %v\n", err)
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": err.Error(),
		})
		return
	}

	response, err := client.DownloadValues(req)
	if err != nil {
		fmt.Printf("[NexusHandler] Download error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Download failed: %v", err),
		})
		return
	}

	// Se houver erro na resposta, retorna como bad request
	if response.Error != "" {
		fmt.Printf("[NexusHandler] Response contains error: %s\n", response.Error)
		c.JSON(http.StatusBadRequest, response)
		return
	}

	fmt.Printf("[NexusHandler] Download successful: %d bytes\n", response.Size)
	c.JSON(http.StatusOK, response)
}

// CompareValues compara dois arquivos de values
// POST /api/v1/nexus/values/compare
func (h *NexusHandler) CompareValues(c *gin.Context) {
	var req nexus.CompareValuesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	fmt.Printf("[NexusHandler] Compare request:\n")
	fmt.Printf("  File1: %s/%s/%s/%s (repo=%q)\n", req.File1.Release, req.File1.Version, req.File1.Environment, req.File1.Type, req.File1.Repository)
	fmt.Printf("  File2: %s/%s/%s/%s (repo=%q)\n", req.File2.Release, req.File2.Version, req.File2.Environment, req.File2.Type, req.File2.Repository)

	client, err := h.getClient()
	if err != nil {
		fmt.Printf("[NexusHandler] Error getting client: %v\n", err)
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Baixa ambos os arquivos
	responses, err := client.DownloadMultipleValues([]nexus.ValuesFileRequest{req.File1, req.File2})
	if err != nil {
		fmt.Printf("[NexusHandler] Compare error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Download failed: %v", err),
		})
		return
	}

	response := nexus.CompareValuesResponse{
		File1: responses[0],
		File2: responses[1],
	}

	// Verifica se houve erro em algum dos downloads
	if response.File1.Error != "" || response.File2.Error != "" {
		response.Error = "One or more files failed to download"
		fmt.Printf("[NexusHandler] File1 error: %s\n", response.File1.Error)
		fmt.Printf("[NexusHandler] File2 error: %s\n", response.File2.Error)
	} else {
		fmt.Printf("[NexusHandler] Compare successful: File1=%d bytes, File2=%d bytes\n",
			response.File1.Size, response.File2.Size)
	}

	c.JSON(http.StatusOK, response)
}

// BrowseRepository navega o repositório Nexus e lista diretórios
// GET /api/v1/nexus/browse?path=&q=comercial
func (h *NexusHandler) BrowseRepository(c *gin.Context) {
	path := c.DefaultQuery("path", "")
	query := c.DefaultQuery("q", "")

	client, err := h.getClient()
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": err.Error(),
		})
		return
	}

	response, err := client.BrowseRepository(path, query)
	if err != nil {
		fmt.Printf("[NexusHandler] Browse error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Browse failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// CheckStatus verifica se o Nexus está configurado
// GET /api/v1/nexus/status
func (h *NexusHandler) CheckStatus(c *gin.Context) {
	configured := h.configManager.Exists()

	status := gin.H{
		"configured": configured,
	}

	if configured {
		config, err := h.configManager.Load()
		if err == nil {
			status["baseUrl"] = config.BaseURL
			status["repository"] = config.Repository
			status["username"] = config.Username
			status["tempDir"] = config.TempDir
			status["urlPattern"] = config.URLPattern
		}
	}

	c.JSON(http.StatusOK, status)
}
