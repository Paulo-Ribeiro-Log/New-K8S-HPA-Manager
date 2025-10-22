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
