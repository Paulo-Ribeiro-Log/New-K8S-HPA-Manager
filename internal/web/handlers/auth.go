package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/auth"
	"k8s-hpa-manager/internal/rbac"
)

// AuthHandler gerencia os endpoints de autenticação JWT.
type AuthHandler struct {
	jwtManager *auth.JWTManager
	rbacManager *rbac.RBACManager
	disableAD  bool
}

// NewAuthHandler cria um AuthHandler.
func NewAuthHandler(jwtManager *auth.JWTManager, rbacManager *rbac.RBACManager, disableAD bool) *AuthHandler {
	return &AuthHandler{
		jwtManager:  jwtManager,
		rbacManager: rbacManager,
		disableAD:   disableAD,
	}
}

// Login emite um JWT para o usuário autenticado via Azure CLI.
//
// POST /api/v1/auth/login
//
// Modos de operação:
//  - JWT não configurado (K8S_HPA_JWT_SECRET ausente): retorna 501
//  - Bypass AD (flag --ad): emite JWT com isSRE=true sem consultar Azure AD
//  - Normal: obtém email via `az account show`, verifica grupo VV_CLOUD_SRE,
//    emite JWT com os claims reais
func (h *AuthHandler) Login(c *gin.Context) {
	if !h.jwtManager.IsConfigured() {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "JWT não configurado no servidor. Defina K8S_HPA_JWT_SECRET (mín. 32 bytes) e reinicie.",
			"code":  "JWT_NOT_CONFIGURED",
		})
		return
	}

	var email, name string
	var isSRE bool

	if h.disableAD {
		// Modo emergência: bypass completo de autenticação AD
		email = "bypass@emergency.mode"
		name = "Emergency Bypass"
		isSRE = true
	} else {
		// Modo normal: consultar Azure CLI (lento, mas executado apenas uma vez)
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()

		perms, err := h.rbacManager.GetCurrentUserPermissions(ctx)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":  "Não foi possível identificar o usuário. Verifique se o Azure CLI está autenticado no servidor (`az login`).",
				"code":   "AZ_CLI_ERROR",
				"detail": err.Error(),
			})
			return
		}
		email = perms.Email
		name = email // display name não disponível via az CLI sem chamada Graph extra
		isSRE = perms.IsSRE
	}

	token, err := h.jwtManager.Generate(email, name, isSRE)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "Erro ao gerar token JWT.",
			"code":   "JWT_GENERATE_ERROR",
			"detail": err.Error(),
		})
		return
	}

	expiresAt := time.Now().Add(h.jwtManager.TTL())

	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"email":      email,
		"is_sre":     isSRE,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"ttl_hours":  int(h.jwtManager.TTL().Hours()),
	})
}

// Logout instrui o frontend a descartar o token. O backend é stateless — não há
// lista de tokens revogados. Para invalidação imediata em produção, usar TTL curto
// ou implementar lista de revogação (fase futura).
//
// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "Logout realizado. Descarte o token no cliente.",
	})
}

// RefreshToken valida o JWT atual e emite um novo com TTL renovado,
// mantendo os mesmos claims (email, isSRE). Útil para sessões longas sem
// re-autenticação via Azure CLI.
//
// POST /api/v1/auth/refresh
// Authorization: Bearer <jwt-atual>
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	if !h.jwtManager.IsConfigured() {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "JWT não configurado.",
			"code":  "JWT_NOT_CONFIGURED",
		})
		return
	}

	// Ler token do header Authorization
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) < 8 || authHeader[:7] != "Bearer " {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Header Authorization ausente ou malformado.",
			"code":  "MISSING_TOKEN",
		})
		return
	}
	tokenStr := authHeader[7:]

	// ValidateForRefresh aceita tokens expirados há até 24h — o analista não
	// precisa refazer az login durante o dia de trabalho.
	claims, err := h.jwtManager.ValidateForRefresh(tokenStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":  "Sessão expirada há mais de 24h. Faça login novamente.",
			"code":   "INVALID_TOKEN",
			"detail": err.Error(),
		})
		return
	}

	newToken, err := h.jwtManager.Generate(claims.Email, claims.Name, claims.IsSRE)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Erro ao renovar token.",
			"code":  "JWT_GENERATE_ERROR",
		})
		return
	}

	expiresAt := time.Now().Add(h.jwtManager.TTL())

	c.JSON(http.StatusOK, gin.H{
		"token":      newToken,
		"email":      claims.Email,
		"is_sre":     claims.IsSRE,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"ttl_hours":  int(h.jwtManager.TTL().Hours()),
	})
}
