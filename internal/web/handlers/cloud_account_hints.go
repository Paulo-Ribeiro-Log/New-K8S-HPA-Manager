package handlers

import (
	"fmt"
	"net/http"

	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
)

// CloudAccountHintsHandler gerencia o lembrete pessoal de qual e-mail usar ao
// autenticar via Device Auth Grant em cada provider (GCP/AWS).
type CloudAccountHintsHandler struct {
	userTokensStore *storage.UserTokensStore
}

// NewCloudAccountHintsHandler cria o handler.
func NewCloudAccountHintsHandler(store *storage.UserTokensStore) *CloudAccountHintsHandler {
	return &CloudAccountHintsHandler{userTokensStore: store}
}

func cloudAccountHintsEmail(c *gin.Context) string {
	email, _ := c.Get("user_email")
	s := fmt.Sprintf("%v", email)
	if s == "" || s == "<nil>" {
		return "default"
	}
	return s
}

// Get — GET /api/v1/user/cloud-account-hints
func (h *CloudAccountHintsHandler) Get(c *gin.Context) {
	if h.userTokensStore == nil {
		c.JSON(http.StatusOK, storage.CloudAccountHints{})
		return
	}
	hints, err := h.userTokensStore.GetCloudAccountHints(cloudAccountHintsEmail(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, hints)
}

// Save — POST /api/v1/user/cloud-account-hints
func (h *CloudAccountHintsHandler) Save(c *gin.Context) {
	var hints storage.CloudAccountHints
	if err := c.ShouldBindJSON(&hints); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "json inválido"})
		return
	}
	if h.userTokensStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage não disponível"})
		return
	}
	if err := h.userTokensStore.SaveCloudAccountHints(cloudAccountHintsEmail(c), hints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
