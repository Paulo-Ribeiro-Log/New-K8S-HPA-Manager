package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/servicenow"
)

// ServiceNowHandler lida com importação de dados do ServiceNow
type ServiceNowHandler struct {
	client *servicenow.Client
	rod    *servicenow.RodExtractor // Usa Rod (Go nativo) - sem dependências externas
	logger *zerolog.Logger
}

// NewServiceNowHandler cria novo handler
func NewServiceNowHandler(logger *zerolog.Logger) *ServiceNowHandler {
	client := servicenow.NewClient("https://viavarejo.service-now.com", logger)
	rod := servicenow.NewRodExtractor(logger) // Rod baixa o browser automaticamente
	return &ServiceNowHandler{
		client: client,
		rod:    rod,
		logger: logger,
	}
}

// ImportFromURL tenta importar dados de uma CHG a partir da URL via HTTP scraping
// Se não conseguir (página requer autenticação), sugere usar a aba manual
// POST /api/v1/servicenow/import
func (h *ServiceNowHandler) ImportFromURL(c *gin.Context) {
	var req servicenow.ImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "URL é obrigatória",
		})
		return
	}

	h.logger.Info().
		Str("url", req.URL).
		Msg("Tentando importar dados do ServiceNow via HTTP")

	response, err := h.client.ImportFromURL(c.Request.Context(), req.URL)
	if err != nil {
		h.logger.Error().Err(err).Msg("Erro ao importar do ServiceNow")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// ImportFromDescription importa dados diretamente do texto do description
// Esta é a opção mais confiável - usuário cola o texto do "Motivo da mudança"
// POST /api/v1/servicenow/parse
func (h *ServiceNowHandler) ImportFromDescription(c *gin.Context) {
	var req struct {
		Description string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Campo 'description' é obrigatório",
		})
		return
	}

	h.logger.Info().
		Int("description_length", len(req.Description)).
		Msg("Parseando description do ServiceNow")

	extractedData := h.client.ImportFromDescription(req.Description)

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"extracted_data": extractedData,
	})
}

// ExtractSysID extrai o sys_id de uma URL do ServiceNow
// GET /api/v1/servicenow/extract-sysid?url=...
func (h *ServiceNowHandler) ExtractSysID(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parâmetro 'url' é obrigatório",
		})
		return
	}

	sysID, err := servicenow.ExtractSysIDFromURL(url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"sys_id":  sysID,
	})
}

// ExtractWithPlaywright extrai dados de uma CHG usando Playwright (browser automation)
// Útil quando Azure AD SSO é necessário - abre browser real para o usuário fazer login
// POST /api/v1/servicenow/extract-playwright
func (h *ServiceNowHandler) ExtractWithPlaywright(c *gin.Context) {
	var req servicenow.ImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "URL é obrigatória",
		})
		return
	}

	h.logger.Info().
		Str("url", req.URL).
		Msg("[ServiceNow] Extraindo CHG via Playwright")

	result, err := h.rod.Extract(c.Request.Context(), req.URL)
	if err != nil {
		h.logger.Error().Err(err).Msg("[ServiceNow] Erro no Playwright")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Se temos description, reprocessar com o parser Go para consistência
	if result.Success && result.Description != "" {
		extractedData := h.client.ImportFromDescription(result.Description)
		c.JSON(http.StatusOK, gin.H{
			"success":           true,
			"change_number":     result.ChangeNumber,
			"short_description": result.ShortDescription,
			"description":       result.Description,
			"state":             result.State,
			"extracted_data":    extractedData,
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetPlaywrightStatus verifica o status da configuração do Playwright
// GET /api/v1/servicenow/playwright-status
func (h *ServiceNowHandler) GetPlaywrightStatus(c *gin.Context) {
	status := h.rod.GetStatus()
	c.JSON(http.StatusOK, status)
}

// GetSessionStatus retorna o status da sessão do Playwright (cookies/login Azure AD)
// GET /api/v1/servicenow/session-status
func (h *ServiceNowHandler) GetSessionStatus(c *gin.Context) {
	status := h.rod.GetSessionStatus()
	c.JSON(http.StatusOK, status)
}

// ClearSession limpa a sessão do Playwright, forçando novo login
// DELETE /api/v1/servicenow/session
func (h *ServiceNowHandler) ClearSession(c *gin.Context) {
	h.logger.Info().Msg("[ServiceNow] Limpando sessão do Playwright")

	err := h.rod.ClearSession()
	if err != nil {
		h.logger.Error().Err(err).Msg("[ServiceNow] Erro ao limpar sessão")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Sessão limpa com sucesso. Novo login será necessário na próxima extração.",
	})
}

// TestSession abre o browser para o usuário fazer login preventivamente
// POST /api/v1/servicenow/session/test
func (h *ServiceNowHandler) TestSession(c *gin.Context) {
	h.logger.Info().Msg("[ServiceNow] Iniciando teste de sessão")

	// Verificar status do Playwright primeiro
	playwrightStatus := h.rod.GetStatus()
	h.logger.Info().
		Interface("playwright_status", playwrightStatus).
		Msg("[ServiceNow] Status do Playwright antes do teste")

	// IMPORTANTE: Usar context.Background() para não ser cancelado pelo timeout do HTTP request
	// O usuário pode levar até 3 minutos para fazer login no Azure AD
	ctx := context.Background()

	status, err := h.rod.TestSession(ctx)
	if err != nil {
		h.logger.Error().
			Err(err).
			Interface("playwright_status", playwrightStatus).
			Msg("[ServiceNow] Erro ao iniciar teste de sessão")

		// Retornar erro detalhado para o frontend
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":           false,
			"error":             err.Error(),
			"playwright_status": playwrightStatus,
		})
		return
	}

	if status == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success":           false,
			"error":             "Não foi possível verificar o status da sessão",
			"playwright_status": playwrightStatus,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": status.Valid,
		"status":  status,
	})
}

// GetBrowserConfig retorna a configuração de browser para autenticação ServiceNow
// GET /api/v1/servicenow/browser-config
func (h *ServiceNowHandler) GetBrowserConfig(c *gin.Context) {
	cfg := servicenow.LoadBrowserConfig()
	c.JSON(http.StatusOK, gin.H{
		"force_windows_browser": cfg.ForceWindowsBrowser,
		"needs_windows_browser": servicenow.NeedsWindowsBrowser(),
		"is_wsl":                servicenow.IsWSL(),
		"has_display":           servicenow.HasGraphicalDisplay(),
	})
}

// SetBrowserConfig atualiza a configuração de browser para autenticação ServiceNow
// POST /api/v1/servicenow/browser-config
func (h *ServiceNowHandler) SetBrowserConfig(c *gin.Context) {
	var req struct {
		ForceWindowsBrowser bool `json:"force_windows_browser"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg := servicenow.BrowserConfig{
		ForceWindowsBrowser: req.ForceWindowsBrowser,
	}
	if err := servicenow.SaveBrowserConfig(cfg); err != nil {
		h.logger.Error().Err(err).Msg("[ServiceNow] Erro ao salvar configuração de browser")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	mode := "local (Chromium WSL/Linux)"
	if servicenow.NeedsWindowsBrowser() {
		mode = "Windows (Chrome/Edge via CDP)"
	}
	h.logger.Info().
		Bool("force_windows_browser", req.ForceWindowsBrowser).
		Str("active_mode", mode).
		Msg("[ServiceNow] Configuração de browser atualizada")

	c.JSON(http.StatusOK, gin.H{
		"success":               true,
		"force_windows_browser": cfg.ForceWindowsBrowser,
		"needs_windows_browser": servicenow.NeedsWindowsBrowser(),
		"active_mode":           mode,
	})
}
