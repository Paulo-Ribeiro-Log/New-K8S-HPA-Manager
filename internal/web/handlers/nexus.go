package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/browser"
	"k8s-hpa-manager/internal/pkg/nexus"
)

// ─── Nexus — login unificado com o Perfil SSO ────────────────────────────────────────────────
//
// Bug real corrigido: o login do Nexus (Menu de Perfil → Nexus) exigia um usuário/senha PRÓPRIOS,
// digitados e salvos à parte — mas o Nexus desta empresa é autenticado via SSO corporativo, a
// MESMA identidade (email OU matrícula, conforme o Perfil SSO já configurado — ver
// browser.SSOLoginIdentifier) já usada por ServiceNow/Teams/Spinnaker. Manter uma senha separada
// e desatualizada é exatamente a causa do 401 relatado — nada garante que a senha Nexus salva
// meses atrás ainda bata com a senha corporativa atual, enquanto o Perfil SSO é o lugar único que
// o usuário já mantém atualizado. Corrigido: username/senha NUNCA mais são pedidos nem
// persistidos pra este handler — sempre resolvidos em tempo real a partir do Perfil SSO
// (`browser.LoadSSOCredentials`, `~/.k8s-hpa-manager/sso_profile.json`, mesmo arquivo/mecanismo já
// usado pela automação de browser). `nexus.Config` mantém os campos Username/Password na struct
// (compatibilidade de schema com configs antigas em disco, nunca mais escritos por este handler)
// só BaseURL/Repository/TempDir/URLPattern continuam sendo configuração de fato do usuário.

// NexusHandler gerencia requisições relacionadas ao Nexus
type NexusHandler struct {
	configManager nexus.ConfigManager
	baseDir       string // ~/.k8s-hpa-manager — mesmo diretório do Perfil SSO (sso_profile.json)
}

// NewNexusHandler cria uma nova instância do NexusHandler. baseDir é ~/.k8s-hpa-manager (mesmo
// diretório do Perfil SSO usado por ServiceNow/Teams/Spinnaker).
func NewNexusHandler(baseDir string) (*NexusHandler, error) {
	configManager, err := nexus.NewFileConfigManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create config manager: %w", err)
	}

	return &NexusHandler{
		configManager: configManager,
		baseDir:       baseDir,
	}, nil
}

// resolveSSOCredentials obtém username+senha do Perfil SSO corporativo (mesma fonte usada por
// ServiceNow/Teams/Spinnaker) — nunca de um campo próprio do Nexus.
//
// Achado real, confirmado ao vivo contra o Nexus real desta empresa (Basic Auth em
// /service/rest/v1/status): o login do Nexus exige a MATRÍCULA, não o email — email devolve 401
// mesmo com a senha certa, matrícula devolve 200. Por isso este handler NUNCA usa
// browser.SSOLoginIdentifier() (o identificador global "email"/"matricula" configurável em
// Browser Config, que rege o preenchimento automático do formulário Azure AD/SAML usado por
// ServiceNow/Teams — um mecanismo de login completamente diferente, sem relação com o formato que
// o Nexus REST API exige) — é sempre "matricula" aqui, incondicional. Erro claro e acionável
// quando o Perfil SSO ainda não foi configurado OU não tem matrícula preenchida, em vez de deixar
// a chamada HTTP falhar com um 401 sem contexto nenhum sobre o que fazer.
func (h *NexusHandler) resolveSSOCredentials() (username, password string, err error) {
	username, password, ok := browser.LoadSSOCredentials(h.baseDir, "matricula")
	if !ok {
		return "", "", fmt.Errorf("perfil SSO não configurado (ou sem matrícula preenchida) — o login do Nexus exige a matrícula; configure em Menu de Perfil → Perfil SSO antes de usar o Nexus")
	}
	return username, password, nil
}

// getClient monta o cliente Nexus com a configuração (BaseURL/Repository/etc) salva + as
// credenciais do Perfil SSO resolvidas NA HORA (nunca cacheadas por muito tempo — se o usuário
// trocar a senha no Perfil SSO, a próxima chamada já usa a senha nova, sem precisar de nenhum
// "refresh" manual).
func (h *NexusHandler) getClient() (nexus.Client, error) {
	config, err := h.configManager.Load()
	if err != nil {
		return nil, fmt.Errorf("Nexus não configurado — informe a URL base e o repositório em Menu de Perfil → Nexus")
	}

	username, password, err := h.resolveSSOCredentials()
	if err != nil {
		return nil, err
	}
	config.Username = username
	config.Password = password

	return nexus.NewHTTPClient(*config), nil
}

// nexusConfigRequest — payload de configuração do Nexus, SEM username/senha (ver comentário no
// topo do arquivo). Usado por TestConnection e SaveConfig.
type nexusConfigRequest struct {
	BaseURL    string `json:"baseUrl"`
	Repository string `json:"repository"`
	TempDir    string `json:"tempDir"`
	URLPattern string `json:"urlPattern"`
}

// TestConnection testa a conexão com o Nexus usando as credenciais do Perfil SSO
// POST /api/v1/nexus/test
func (h *NexusHandler) TestConnection(c *gin.Context) {
	var req nexusConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if req.BaseURL == "" || req.Repository == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "BaseURL and Repository are required",
		})
		return
	}

	username, password, err := h.resolveSSOCredentials()
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Cliente temporário só pra este teste — nunca persistido.
	client := nexus.NewHTTPClient(nexus.Config{
		BaseURL:    req.BaseURL,
		Repository: req.Repository,
		Username:   username,
		Password:   password,
	})

	response, err := client.TestConnection()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Test failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// SaveConfig salva a configuração do Nexus (BaseURL/Repository/TempDir/URLPattern — nunca
// username/senha, sempre resolvidos do Perfil SSO em tempo real)
// POST /api/v1/nexus/config
func (h *NexusHandler) SaveConfig(c *gin.Context) {
	var req nexusConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if req.BaseURL == "" || req.Repository == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "BaseURL and Repository are required",
		})
		return
	}

	if req.TempDir == "" {
		req.TempDir = "/tmp/k8s-hpa-nexus"
	}

	config := nexus.Config{
		BaseURL:    req.BaseURL,
		Repository: req.Repository,
		TempDir:    req.TempDir,
		URLPattern: req.URLPattern,
	}

	if err := h.configManager.Save(config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to save configuration: %v", err),
		})
		return
	}

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

	c.JSON(http.StatusOK, nexusConfigRequest{
		BaseURL:    config.BaseURL,
		Repository: config.Repository,
		TempDir:    config.TempDir,
		URLPattern: config.URLPattern,
	})
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
// GET /api/v1/nexus/browse?path=&q=comercial&repository=continuousdeploy-history
func (h *NexusHandler) BrowseRepository(c *gin.Context) {
	path := c.DefaultQuery("path", "")
	query := c.DefaultQuery("q", "")
	repository := c.DefaultQuery("repository", "")

	client, err := h.getClient()
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": err.Error(),
		})
		return
	}

	response, err := client.BrowseRepository(path, query, repository)
	if err != nil {
		fmt.Printf("[NexusHandler] Browse error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Browse failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// CheckStatus verifica se o Nexus está configurado — cobre os dois pré-requisitos separadamente
// (BaseURL/Repository salvos E Perfil SSO configurado), já que agora são duas coisas
// independentes: o usuário pode ter configurado só uma delas.
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
			status["tempDir"] = config.TempDir
			status["urlPattern"] = config.URLPattern
		}
	}

	// Sempre "matricula" aqui — ver comentário de resolveSSOCredentials.
	if username, _, ok := browser.LoadSSOCredentials(h.baseDir, "matricula"); ok {
		status["ssoConfigured"] = true
		status["ssoUsername"] = username
	} else {
		status["ssoConfigured"] = false
	}

	c.JSON(http.StatusOK, status)
}
