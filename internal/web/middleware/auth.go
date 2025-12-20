package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware valida o token Bearer no header Authorization
func AuthMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "UNAUTHORIZED",
					"message": "No authorization header provided",
				},
			})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_AUTH_FORMAT",
					"message": "Authorization header must be 'Bearer <token>'",
				},
			})
			c.Abort()
			return
		}

		if parts[1] != token {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_TOKEN",
					"message": "Invalid authentication token",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// WebSocketAuthMiddleware valida o token via query parameter ou header
// Usado para WebSocket que não pode enviar headers customizados facilmente
func WebSocketAuthMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Tentar pegar token do query parameter primeiro (para WebSocket)
		queryToken := c.Query("token")
		if queryToken != "" {
			if queryToken != token {
				c.JSON(401, gin.H{
					"success": false,
					"error": gin.H{
						"code":    "INVALID_TOKEN",
						"message": "Invalid authentication token",
					},
				})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// Fallback para header Authorization (caso cliente consiga enviar)
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "UNAUTHORIZED",
					"message": "No authentication provided (token query param or Authorization header required)",
				},
			})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_AUTH_FORMAT",
					"message": "Authorization header must be 'Bearer <token>'",
				},
			})
			c.Abort()
			return
		}

		if parts[1] != token {
			c.JSON(401, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_TOKEN",
					"message": "Invalid authentication token",
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
