package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/sreapproval"
)

// SREApprovalHandler lida com aprovações SRE
type SREApprovalHandler struct {
	client  *sreapproval.Client
	logger  *zerolog.Logger
	baseDir string
}

// NewSREApprovalHandler cria novo handler. `baseDir` (~/.k8s-hpa-manager) é usado pra ler o Perfil
// SSO corporativo (`ssoProfileEmail`, sso_profile.go) — ver comentário de `resolveApproverEmail`.
func NewSREApprovalHandler(logger *zerolog.Logger, baseDir string) *SREApprovalHandler {
	client := sreapproval.NewClient(logger)
	return &SREApprovalHandler{
		client:  client,
		logger:  logger,
		baseDir: baseDir,
	}
}

// resolveApproverEmail decide o e-mail do aprovador a submeter no devstartcd (ServiceNow).
//
// Bug real corrigido, relatado pelo usuário logo depois de uma correção anterior desta mesma
// rotina: "não é esse email que usamos para logar no service now. é para ser usado o email que
// cadastramos no perfil da aplicação. o mesmo que é usado para logar no service now". A correção
// anterior priorizava `c.GetString("user_email")` (claims do JWT — o e-mail de login da própria
// aplicação via Azure AD, ex: `4960023587.ca@via.com.br`, uma conta de nuvem secundária) — mas
// esse NÃO é o e-mail que o ServiceNow reconhece como o usuário: quem loga no ServiceNow via SSO
// usa as credenciais do "Perfil SSO corporativo centralizado" (`~/.k8s-hpa-manager/sso_profile.json`,
// `ssoProfileEmail` em sso_profile.go — o mesmo perfil já usado por `internal/browser/sso_autologin.go`
// pro auto-preenchimento do Azure AD ao abrir ServiceNow/Teams/Spinnaker), que pode ser (e no caso
// relatado É) um e-mail corporativo diferente (ex: `paulo.gribeiro@viaverjo.com.br`).
//
// Prioridade: (1) e-mail do Perfil SSO — a fonte correta, é literalmente o que loga no ServiceNow;
// (2) `user_email` do JWT — só como fallback quando o Perfil SSO não está configurado, melhor que
// nada; (3) Azure CLI do servidor (dentro de `client.Approve`) — último recurso, só em modo sem
// JWT.
func (h *SREApprovalHandler) resolveApproverEmail(c *gin.Context) string {
	if email := ssoProfileEmail(h.baseDir); email != "" {
		return email
	}
	return c.GetString("user_email")
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

	// Ver resolveApproverEmail — e-mail do Perfil SSO (o que de fato loga no ServiceNow), não o
	// e-mail de login da própria aplicação. O frontend já manda o e-mail obtido de
	// `GET /current-user` (mesma fonte), mas aqui é reforçado como fallback caso o request chegue
	// sem esse campo (ex: cliente HTTP direto) — só cai pro Azure CLI dentro de `client.Approve`
	// como último recurso.
	if req.ApproverEmail == "" {
		req.ApproverEmail = h.resolveApproverEmail(c)
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

// GetCurrentUser obtém o e-mail do aprovador — o mesmo e-mail usado pra logar no ServiceNow (ver
// resolveApproverEmail) — GET /api/v1/sre-approval/current-user
func (h *SREApprovalHandler) GetCurrentUser(c *gin.Context) {
	if email := h.resolveApproverEmail(c); email != "" {
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
