package handlers

import (
	"net/http"
	"time"

	awsprovider "k8s-hpa-manager/internal/cloudprovider/aws"
	"k8s-hpa-manager/internal/config"

	"github.com/gin-gonic/gin"
)

// AWSAuthHandler gerencia autenticação AWS SSO via API.
type AWSAuthHandler struct {
	sso         *awsprovider.AWSSSOManager
	kubeManager *config.KubeConfigManager
}

// NewAWSAuthHandler cria o handler.
func NewAWSAuthHandler(kubeManager *config.KubeConfigManager) *AWSAuthHandler {
	return &AWSAuthHandler{
		sso:         awsprovider.NewAWSSSOManager(),
		kubeManager: kubeManager,
	}
}

// CheckStatus verifica se o token AWS está válido para um perfil.
// GET /api/v1/aws/auth/status?profile=asaplog
func (h *AWSAuthHandler) CheckStatus(c *gin.Context) {
	profile := c.Query("profile")
	if profile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile é obrigatório"})
		return
	}

	valid := h.sso.IsTokenValid(c.Request.Context(), profile)

	// Verificar se há sessão de login em andamento.
	session := h.sso.GetSession(profile)
	loginInProgress := session != nil

	c.JSON(http.StatusOK, gin.H{
		"profile":          profile,
		"valid":            valid,
		"login_in_progress": loginInProgress,
	})
}

// StartLogin inicia o fluxo de login SSO e retorna a URL de autorização.
// POST /api/v1/aws/auth/login
// Body: {"profile": "asaplog"}
func (h *AWSAuthHandler) StartLogin(c *gin.Context) {
	var req struct {
		Profile string `json:"profile" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile é obrigatório"})
		return
	}

	// Se token já válido, não precisa logar.
	if h.sso.IsTokenValid(c.Request.Context(), req.Profile) {
		c.JSON(http.StatusOK, gin.H{
			"profile": req.Profile,
			"already_valid": true,
		})
		return
	}

	session, err := h.sso.StartLogin(c.Request.Context(), req.Profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "falha ao iniciar login SSO",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"profile":   req.Profile,
		"url":       session.URL,
		"user_code": session.UserCode,
		"message":   "Abra a URL no navegador e insira o código para autenticar",
	})
}

// PollLogin verifica se o login SSO foi concluído.
// GET /api/v1/aws/auth/poll?profile=asaplog
func (h *AWSAuthHandler) PollLogin(c *gin.Context) {
	profile := c.Query("profile")
	if profile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile é obrigatório"})
		return
	}

	session := h.sso.GetSession(profile)

	// Sem sessão ativa — verificar se token ficou válido.
	if session == nil {
		valid := h.sso.IsTokenValid(c.Request.Context(), profile)
		if valid {
			h.kubeManager.EvictClientsByAWSProfile(profile)
		}
		c.JSON(http.StatusOK, gin.H{
			"profile":  profile,
			"done":     true,
			"success":  valid,
		})
		return
	}

	// Sessão ativa — verificar se já terminou (non-blocking).
	select {
	case err := <-session.Done:
		success := err == nil
		if success {
			h.kubeManager.EvictClientsByAWSProfile(profile)
		}
		c.JSON(http.StatusOK, gin.H{
			"profile": profile,
			"done":    true,
			"success": success,
		})
	default:
		// Ainda aguardando o usuário autenticar.
		c.JSON(http.StatusOK, gin.H{
			"profile":    profile,
			"done":       false,
			"url":        session.URL,
			"user_code":  session.UserCode,
			"started_at": session.StartedAt.Format(time.RFC3339),
		})
	}
}

// ListConfigs retorna todos os perfis AWS configurados.
// GET /api/v1/aws/config
func (h *AWSAuthHandler) ListConfigs(c *gin.Context) {
	cfg, err := h.sso.LoadConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"profiles": cfg.Profiles})
}

// GetConfig retorna a config de um perfil específico.
// GET /api/v1/aws/config/:profile
func (h *AWSAuthHandler) GetConfig(c *gin.Context) {
	profile := c.Param("profile")

	profileCfg, err := h.sso.LoadProfileConfig(profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if profileCfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "perfil não configurado"})
		return
	}

	c.JSON(http.StatusOK, profileCfg)
}

// SaveConfig cria ou atualiza a config de um perfil.
// POST /api/v1/aws/config
// Body: {"profile": "asaplog", "sso": {"start_url": "...", "region": "...", "account_id": "...", "role_name": "..."}, "region": "us-east-1"}
func (h *AWSAuthHandler) SaveConfig(c *gin.Context) {
	var req struct {
		Profile string                       `json:"profile" binding:"required"`
		SSO     awsprovider.AWSSSOConfig     `json:"sso"     binding:"required"`
		Region  string                       `json:"region"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	profileCfg := &awsprovider.AWSProfileConfig{
		SSO:    req.SSO,
		Region: req.Region,
	}

	if err := h.sso.SaveProfileConfig(req.Profile, profileCfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"profile": req.Profile,
		"message": "Configuração AWS SSO salva com sucesso",
	})
}

// DeleteConfig remove a config de um perfil.
// DELETE /api/v1/aws/config/:profile
func (h *AWSAuthHandler) DeleteConfig(c *gin.Context) {
	profile := c.Param("profile")

	if err := h.sso.DeleteProfileConfig(profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"profile": profile,
		"message": "Configuração removida",
	})
}
