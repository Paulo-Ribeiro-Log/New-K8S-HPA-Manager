package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/auth"
	"k8s-hpa-manager/internal/rbac"
)

// jwtClaimsFromCtx extrai JWTClaims do contexto Gin, se presentes.
// Retorna nil quando o sistema opera em modo de token estático.
func jwtClaimsFromCtx(c *gin.Context) *auth.JWTClaims {
	val, exists := c.Get("jwt_claims")
	if !exists {
		return nil
	}
	claims, _ := val.(*auth.JWTClaims)
	return claims
}

// RBACMiddleware armazena o gerenciador RBAC
type RBACMiddleware struct {
	rbacManager *rbac.RBACManager
}

// NewRBACMiddleware cria um novo middleware RBAC
func NewRBACMiddleware(rbacManager *rbac.RBACManager) *RBACMiddleware {
	return &RBACMiddleware{
		rbacManager: rbacManager,
	}
}

// InjectUserEmail middleware que injeta user_email no contexto.
// Em modo JWT: lê email diretamente dos claims (zero chamada ao Azure CLI).
// Em modo token estático: obtém via `az account show`.
func (m *RBACMiddleware) InjectUserEmail() gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims := jwtClaimsFromCtx(c); claims != nil {
			c.Set("user_email", claims.Email)
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		email, err := m.rbacManager.GetCurrentUserEmail(ctx)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "user not authenticated",
			})
			c.Abort()
			return
		}

		c.Set("user_email", email)
		c.Next()
	}
}

// RequireSREGroup middleware que exige que usuário seja do grupo VV_CLOUD_SRE.
// Em modo JWT: lê isSRE diretamente dos claims (zero chamada ao Azure CLI).
// Em modo token estático: verifica via `az ad user get-member-groups`.
func (m *RBACMiddleware) RequireSREGroup() gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims := jwtClaimsFromCtx(c); claims != nil {
			if !claims.IsSRE {
				c.JSON(http.StatusForbidden, gin.H{
					"error":   "Access denied",
					"message": "This operation requires SRE group membership (VV_CLOUD_SRE)",
				})
				c.Abort()
				return
			}
			c.Set("isSRE", true)
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		isSRE, err := m.rbacManager.CheckCurrentUserIsSRE(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to verify user permissions",
				"details": err.Error(),
			})
			c.Abort()
			return
		}

		if !isSRE {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Access denied",
				"message": "This operation requires SRE group membership (VV_CLOUD_SRE)",
			})
			c.Abort()
			return
		}

		c.Set("isSRE", true)
		c.Next()
	}
}

// GetUserPermissions endpoint público para frontend verificar permissões.
// Em modo JWT: retorna dados dos claims sem chamar Azure CLI.
// Em modo token estático: comportamento original via az CLI.
func (m *RBACMiddleware) GetUserPermissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims := jwtClaimsFromCtx(c); claims != nil {
			c.JSON(http.StatusOK, gin.H{
				"email":  claims.Email,
				"isSRE":  claims.IsSRE,
				"groups": []interface{}{},
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		perms, err := m.rbacManager.GetCurrentUserPermissions(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to get user permissions",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"email":  perms.Email,
			"isSRE":  perms.IsSRE,
			"groups": perms.Groups,
		})
	}
}

// RefreshPermissions endpoint para forçar atualização do cache
func (m *RBACMiddleware) RefreshPermissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		email, err := m.rbacManager.GetCurrentUserEmail(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to get current user",
			})
			return
		}

		// Invalidar cache
		m.rbacManager.InvalidateUserCache(email)

		// Buscar permissões atualizadas
		perms, err := m.rbacManager.GetUserPermissions(ctx, email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to refresh permissions",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Permissions refreshed successfully",
			"email":   perms.Email,
			"isSRE":   perms.IsSRE,
			"groups":  perms.Groups,
		})
	}
}

// OptionalSRECheck middleware que adiciona flag isSRE ao contexto sem bloquear.
// Em modo JWT: lê isSRE dos claims diretamente.
// Em modo token estático: verifica via az CLI (5s timeout), assume false se falhar.
func (m *RBACMiddleware) OptionalSRECheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims := jwtClaimsFromCtx(c); claims != nil {
			c.Set("isSRE", claims.IsSRE)
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		isSRE, err := m.rbacManager.CheckCurrentUserIsSRE(ctx)
		if err == nil {
			c.Set("isSRE", isSRE)
		} else {
			c.Set("isSRE", false)
		}

		c.Next()
	}
}
