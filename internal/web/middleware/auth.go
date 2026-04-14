package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/auth"
)

// JWTAuthMiddleware valida o token Bearer em modo dual:
//   - Se jwtManager.IsConfigured(): exige JWT assinado; injeta claims no contexto via "jwt_claims"
//   - Caso contrário: valida token estático (backward compat com K8S_HPA_WEB_TOKEN)
func JWTAuthMiddleware(jwtManager *auth.JWTManager, staticToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "UNAUTHORIZED", "message": "No authorization header provided"}})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "INVALID_AUTH_FORMAT", "message": "Authorization header must be 'Bearer <token>'"}})
			c.Abort()
			return
		}

		tokenStr := parts[1]

		if jwtManager != nil && jwtManager.IsConfigured() {
			claims, err := jwtManager.Validate(tokenStr)
			if err != nil {
				code := "INVALID_TOKEN"
				if err.Error() == "token expirado" {
					code = "TOKEN_EXPIRED"
				}
				c.JSON(401, gin.H{"success": false, "error": gin.H{"code": code, "message": err.Error()}})
				c.Abort()
				return
			}
			c.Set("jwt_claims", claims)
			c.Next()
			return
		}

		// Fallback: token estático
		if tokenStr != staticToken {
			c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "INVALID_TOKEN", "message": "Invalid authentication token"}})
			c.Abort()
			return
		}
		c.Next()
	}
}

// WebSocketJWTAuthMiddleware valida autenticação para WebSocket (aceita token via query param).
// Mesmo dual-mode do JWTAuthMiddleware.
func WebSocketJWTAuthMiddleware(jwtManager *auth.JWTManager, staticToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := c.Query("token")
		if tokenStr == "" {
			authHeader := c.GetHeader("Authorization")
			if authHeader == "" {
				c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "UNAUTHORIZED", "message": "No authentication provided"}})
				c.Abort()
				return
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		}

		if tokenStr == "" {
			c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "UNAUTHORIZED", "message": "Token ausente"}})
			c.Abort()
			return
		}

		if jwtManager != nil && jwtManager.IsConfigured() {
			claims, err := jwtManager.Validate(tokenStr)
			if err != nil {
				c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "INVALID_TOKEN", "message": err.Error()}})
				c.Abort()
				return
			}
			c.Set("jwt_claims", claims)
			c.Next()
			return
		}

		// Fallback: token estático
		if tokenStr != staticToken {
			c.JSON(401, gin.H{"success": false, "error": gin.H{"code": "INVALID_TOKEN", "message": "Invalid authentication token"}})
			c.Abort()
			return
		}
		c.Next()
	}
}

