package handlers

import (
	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/history"
)

// GetUserInfoForHistory obtém informações do usuário para auditoria
// Wrapper para simplificar uso em todos os handlers
func GetUserInfoForHistory(c *gin.Context) history.UserInfo {
	return history.GetCurrentUserInfo(c)
}

// CreateHistoryEntry cria uma entrada de histórico com user info automático
func CreateHistoryEntry(c *gin.Context, action, resource, cluster, status string, before, after map[string]interface{}, duration int64, errMsg string) history.HistoryEntry {
	userInfo := GetUserInfoForHistory(c)

	return history.HistoryEntry{
		UserEmail: userInfo.Email,
		UserName:  userInfo.Name,
		Action:    action,
		Resource:  resource,
		Cluster:   cluster,
		Status:    status,
		Before:    before,
		After:     after,
		Duration:  duration,
		ErrorMsg:  errMsg,
	}
}
