package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/sreapproval"
)

// SREApprovalHandler lida com aprovações SRE
type SREApprovalHandler struct {
	client *sreapproval.Client
	logger *zerolog.Logger
}

// NewSREApprovalHandler cria novo handler
func NewSREApprovalHandler(logger *zerolog.Logger) *SREApprovalHandler {
	client := sreapproval.NewClient(logger)
	return &SREApprovalHandler{
		client: client,
		logger: logger,
	}
}

// GetApprovalInfo obtém informações de uma solicitação de aprovação
// GET /api/v1/sre-approval/info?url=...
func (h *SREApprovalHandler) GetApprovalInfo(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parâmetro 'url' é obrigatório",
		})
		return
	}

	h.logger.Info().
		Str("url", url).
		Msg("Buscando informações de aprovação SRE")

	info, err := h.client.GetApprovalInfo(c.Request.Context(), url)
	if err != nil {
		h.logger.Error().Err(err).Msg("Erro ao buscar informações de aprovação")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, sreapproval.ApprovalResponse{
		Success:      true,
		ApprovalInfo: info,
	})
}

// Approve executa a aprovação de uma solicitação
// POST /api/v1/sre-approval/approve
func (h *SREApprovalHandler) Approve(c *gin.Context) {
	var req sreapproval.ApproveActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Campo 'approval_url' é obrigatório",
		})
		return
	}

	h.logger.Info().
		Str("url", req.ApprovalURL).
		Str("approver_email", req.ApproverEmail).
		Msg("Executando aprovação SRE")

	err := h.client.Approve(c.Request.Context(), req.ApprovalURL, req.ApproverEmail)
	if err != nil {
		h.logger.Error().Err(err).Msg("Erro ao aprovar")
		c.JSON(http.StatusInternalServerError, sreapproval.ApproveActionResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, sreapproval.ApproveActionResponse{
		Success: true,
		Message: "Aprovação realizada com sucesso",
	})
}

// ExtractApprovalID extrai o ID de aprovação de uma URL
// GET /api/v1/sre-approval/extract-id?url=...
func (h *SREApprovalHandler) ExtractApprovalID(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parâmetro 'url' é obrigatório",
		})
		return
	}

	id, err := sreapproval.ExtractApprovalID(url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"approval_id": id,
	})
}

// GetCurrentUser obtém informações do usuário atual (email via Azure CLI)
// GET /api/v1/sre-approval/current-user
func (h *SREApprovalHandler) GetCurrentUser(c *gin.Context) {
	email, err := sreapproval.GetCurrentUserEmail(c.Request.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("Erro ao obter email do usuário")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Não foi possível obter email do usuário. Verifique se está logado no Azure CLI.",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"email":   email,
	})
}
