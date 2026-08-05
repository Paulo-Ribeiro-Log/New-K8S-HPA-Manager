package handlers

import (
	"net/http"

	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"

	"github.com/gin-gonic/gin"
)

// GCPAuthHandler gerencia autenticação GCP rodando `gcloud auth login` como subprocesso — ver
// comentário completo em internal/cloudprovider/gcp/auth.go sobre por que não é Device
// Authorization Grant nem uma implementação própria de OAuth2 (nenhum dos dois funcionava de
// verdade / era mais complexo do que precisava ser).
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

// StartLogin roda `gcloud auth login` em background e retorna a URL de autenticação real do
// Google assim que o próprio gcloud a imprime — o frontend só precisa mostrar um link.
// POST /api/v1/gcp/auth/login
func (h *GCPAuthHandler) StartLogin(c *gin.Context) {
	// Testar se já está autenticado ANTES de iniciar um novo login — mesmo guard que o
	// AWSAuthHandler.StartLogin já tinha (IsTokenValid → already_valid). Sem isso, toda chamada a
	// StartLogin (inclusive as disparadas pelo listener reativo "gcp-sso-token-expired" em
	// client.ts) spawnaria um `gcloud auth login` novo mesmo com a sessão já válida.
	if h.auth.CheckStatus(c.Request.Context()).Authenticated {
		c.JSON(http.StatusOK, gin.H{"already_valid": true})
		return
	}

	result, err := h.auth.StartGcloudLogin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "falha ao iniciar autenticação GCP",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id":   result.SessionID,
		"verify_url":   result.VerifyURL,
		"expires_at":   result.ExpiresAt,
		"interval_sec": result.IntervalSec,
		"message":      "Clique no link para autenticar no navegador",
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

	done, success, errMsg := h.auth.PollGcloudLogin(sessionID)
	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"done":       done,
		"success":    success,
		"error":      errMsg,
	})
}
