package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/azlogin"
)

// AzureLoginStatus — GET /api/v1/azure/login-status[?force=1]. Estado do login do Azure CLI do
// servidor (valid | login_required | unknown | not_installed), com cache de 60 s no azlogin.
// Consultado pelo aviso de login expirado do frontend (AzureLoginBanner).
func AzureLoginStatus(c *gin.Context) {
	c.JSON(http.StatusOK, azlogin.Check(c.Request.Context(), c.Query("force") == "1"))
}

// cloudAuthErrorCode: AZ_LOGIN_REQUIRED quando o erro é login do Azure expirado (o frontend
// mostra o aviso com o comando), senão o CLOUD_AUTH_FAILED genérico.
func cloudAuthErrorCode(err error) string {
	if errors.Is(err, azlogin.ErrLoginRequired) {
		return azlogin.Code
	}
	return "CLOUD_AUTH_FAILED"
}
