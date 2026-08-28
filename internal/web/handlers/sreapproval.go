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

	// Bug real corrigido: o e-mail do aprovador é quem está de fato autenticado NESTA sessão web
	// (via SSO/JWT) — nunca a identidade do Azure CLI rodando no processo do servidor (que pode
	// divergir, ex: outra conta `az login` ativa por conta de uma operação de AKS/Node Pools no
	// meio da sessão). O frontend já manda o e-mail obtido de `GET /current-user` (corrigido pelo
	// mesmo motivo), mas aqui é reforçado como fonte de verdade caso o request chegue sem esse
	// campo (ex: cliente HTTP direto) — só cai pro Azure CLI dentro de `client.Approve` como
	// último recurso, mesmo padrão de `history.GetCurrentUserInfo`.
	if req.ApproverEmail == "" {
		req.ApproverEmail = c.GetString("user_email")
	}

	h.logger.Info().
		Str("url", req.ApprovalURL).
		Str("approver_email", req.ApproverEmail).
		Msg("Executando aprovação SRE")

	err := h.client.Approve(c.Request.Context(), req.ApprovalURL, req.ApproverEmail)
	if err != nil {
		if finErr, ok := err.(*sreapproval.ErrAlreadyFinalized); ok {
			c.JSON(http.StatusOK, sreapproval.ApproveActionResponse{
				Success:          true,
				AlreadyFinalized: true,
				ApproverEmail:    finErr.ApproverEmail,
				Message:          "Esta solicitação já foi finalizada",
			})
			return
		}
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

// GetCurrentUser obtém o e-mail do usuário autenticado NESTA sessão web (o aprovador de fato) —
// GET /api/v1/sre-approval/current-user
//
// Bug real corrigido: sempre chamava `az account show` no processo do servidor, ignorando por
// completo a identidade real de quem está logado na aplicação (via SSO/JWT) — a rota nunca tinha
// `InjectUserEmail()` aplicada (única exceção entre rotas comparáveis deste arquivo), então
// `user_email` nunca existia no contexto. Corrigido priorizando `c.GetString("user_email")`
// (claims do JWT, populado pelo middleware agora aplicado em server.go) — o Azure CLI só entra
// como fallback de último recurso (modo sem JWT/token estático), mesmo padrão já usado em
// `history.GetCurrentUserInfo`.
func (h *SREApprovalHandler) GetCurrentUser(c *gin.Context) {
	if email := c.GetString("user_email"); email != "" {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"email":   email,
		})
		return
	}

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
