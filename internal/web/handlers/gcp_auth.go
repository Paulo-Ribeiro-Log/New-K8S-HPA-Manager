package handlers

import (
	"net/http"

	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"

	"github.com/gin-gonic/gin"
)

// GCPAuthHandler gerencia autenticação GCP via Device Auth Grant.
type GCPAuthHandler struct {
	auth *gcpprovider.GCPAuthManager
}

func NewGCPAuthHandler() *GCPAuthHandler {
	return &GCPAuthHandler{auth: gcpprovider.NewGCPAuthManager()}
}

// CheckStatus verifica se o gcloud / ADC estão autenticados.
// GET /api/v1/gcp/auth/status
func (h *GCPAuthHandler) CheckStatus(c *gin.Context) {
	status := h.auth.CheckStatus(c.Request.Context())
	c.JSON(http.StatusOK, status)
}

// StartLogin inicia o Device Authorization Grant.
// POST /api/v1/gcp/auth/login
func (h *GCPAuthHandler) StartLogin(c *gin.Context) {
	result, _, err := h.auth.StartLogin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "falha ao iniciar autenticação GCP",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id":   result.SessionID,
		"user_code":    result.UserCode,
		"verify_url":   result.VerifyURL,
		"expires_at":   result.ExpiresAt,
		"interval_sec": result.IntervalSec,
		"message":      "Acesse a URL e insira o código para autenticar",
	})
}

// PollLogin verifica se o login foi concluído.
// GET /api/v1/gcp/auth/poll?session_id=xxx
func (h *GCPAuthHandler) PollLogin(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id é obrigatório"})
		return
	}

	done, success := h.auth.PollStatus(sessionID)
	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"done":       done,
		"success":    success,
	})
}
