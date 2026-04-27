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

	if !result.Success {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   result.Error,
		})
		return
	}

	// Parser Go (parser.go) tem regexes mais completos; parser Rod (rod_extractor.go parseDescription) é fallback
	extractedData := h.client.ImportFromDescription(result.Description)
	if extractedData == nil {
		extractedData = &servicenow.ExtractedData{}
	}
	// Se o parser Go não encontrou aplicação, usar resultado do Rod
	if extractedData.Application == "" && result.Extracted != nil {
		extractedData = result.Extracted
	}

	h.logger.Info().
		Str("url", req.URL).
		Str("change_number", result.ChangeNumber).
		Str("short_description", result.ShortDescription).
		Str("application", extractedData.Application).
		Str("version", extractedData.Version).
		Int("description_len", len(result.Description)).
		Bool("has_extracted", result.Extracted != nil).
		Str("extracted_app", func() string {
			if result.Extracted != nil {
				return result.Extracted.Application
			}
			return ""
		}()).
		Msg("[ServiceNow] ExtractWithPlaywright resultado")

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"change_number":     result.ChangeNumber,
		"short_description": result.ShortDescription,
		"description":       result.Description,
		"state":             result.State,
		"extracted_data":    extractedData,
	})
}

// ParseBatch extrai dados de múltiplas CHGs em sequência, reutilizando o mesmo fluxo do ExtractWithPlaywright.
// Evita N requisições HTTP simultâneas que fariam o Rod serializar e estourar o timeout do cliente.
// POST /api/v1/servicenow/parse-batch
func (h *ServiceNowHandler) ParseBatch(c *gin.Context) {
	var req struct {
		Items []struct {
			CHG         string `json:"chg"`
			URL         string `json:"url"`
			ApprovalURL string `json:"approval_url"`
		} `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Campo 'items' é obrigatório",
		})
		return
	}

	type batchResult struct {
		CHG              string                    `json:"chg"`
		Success          bool                      `json:"success"`
		ChangeNumber     string                    `json:"change_number,omitempty"`
		ShortDescription string                    `json:"short_description,omitempty"`
		Description      string                    `json:"description,omitempty"`
		State            string                    `json:"state,omitempty"`
		ExtractedData    *servicenow.ExtractedData  `json:"extracted_data,omitempty"`
		Error            string                    `json:"error,omitempty"`
	}

	results := make([]batchResult, 0, len(req.Items))

	for _, item := range req.Items {
		h.logger.Info().Str("chg", item.CHG).Msg("[ServiceNow] ParseBatch: extraindo")

		if item.URL == "" {
			results = append(results, batchResult{CHG: item.CHG, Success: false, Error: "URL ausente"})
			continue
		}

		result, err := h.rod.Extract(c.Request.Context(), item.URL)
		if err != nil {
			h.logger.Error().Err(err).Str("chg", item.CHG).Msg("[ServiceNow] ParseBatch: erro")
			results = append(results, batchResult{CHG: item.CHG, Success: false, Error: err.Error()})
			continue
		}
		if !result.Success {
			results = append(results, batchResult{CHG: item.CHG, Success: false, Error: result.Error})
			continue
		}

		extractedData := h.client.ImportFromDescription(result.Description)
		if extractedData == nil {
			extractedData = &servicenow.ExtractedData{}
		}
		if extractedData.Application == "" && result.Extracted != nil {
			extractedData = result.Extracted
		}

		results = append(results, batchResult{
			CHG:              item.CHG,
			Success:          true,
			ChangeNumber:     result.ChangeNumber,
			ShortDescription: result.ShortDescription,
			Description:      result.Description,
			State:            result.State,
			ExtractedData:    extractedData,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "results": results})
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

// GetBrowserConfig retorna o estado do ambiente de browser para autenticação ServiceNow
// GET /api/v1/servicenow/browser-config
func (h *ServiceNowHandler) GetBrowserConfig(c *gin.Context) {
	cfg := servicenow.LoadBrowserConfig()
	identifier := cfg.SSOLoginIdentifier
	if identifier == "" {
		identifier = "email"
	}
	c.JSON(http.StatusOK, gin.H{
		"is_wsl":               servicenow.IsWSL(),
		"has_display":          servicenow.HasGraphicalDisplay(),
		"xvfb_installed":       servicenow.IsXvfbInstalled(),
		"xvfb_hint":            servicenow.XvfbInstallHint(),
		"browser_mode":         "chromium-local",
		"active_mode":          "Chromium local (headless/Xvfb)",
		"sso_login_identifier": identifier,
	})
}

// SetBrowserConfig salva configurações de browser (incluindo sso_login_identifier).
// PUT /api/v1/servicenow/browser-config
func (h *ServiceNowHandler) SetBrowserConfig(c *gin.Context) {
	var req struct {
		SSOLoginIdentifier string `json:"sso_login_identifier"` // "email" ou "matricula"
	}
	// Ignorar erro — campos são todos opcionais
	c.ShouldBindJSON(&req) //nolint:errcheck

	if req.SSOLoginIdentifier != "" {
		cfg := servicenow.LoadBrowserConfig()
		if req.SSOLoginIdentifier == "email" || req.SSOLoginIdentifier == "matricula" {
			cfg.SSOLoginIdentifier = req.SSOLoginIdentifier
			servicenow.SaveBrowserConfig(cfg) //nolint:errcheck
		}
	}

	cfg := servicenow.LoadBrowserConfig()
	identifier := cfg.SSOLoginIdentifier
	if identifier == "" {
		identifier = "email"
	}
	c.JSON(http.StatusOK, gin.H{
		"success":              true,
		"active_mode":          "Chromium local (headless/Xvfb)",
		"browser_mode":         "chromium-local",
		"sso_login_identifier": identifier,
	})
}
