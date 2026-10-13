package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/history"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// checkForbidden escreve uma resposta 403 se err for um erro de permissão do K8s RBAC.
// Retorna true quando tratou o erro — o chamador deve fazer return imediatamente.
// Inserir como PRIMEIRO check em blocos de erro de operações de escrita K8s.
func checkForbidden(c *gin.Context, err error) bool {
	if apierrors.IsForbidden(err) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "K8S_FORBIDDEN",
				"message": "Permissão negada pelo K8s RBAC. Verifique suas permissões neste namespace.",
			},
		})
		return true
	}
	return false
}

// logOperation grava uma operação no Change History: autor vem do JWT da requisição, status e
// mensagem de erro derivam de err, duração conta desde start. tracker nil não grava nada.
func logOperation(c *gin.Context, tracker *history.HistoryTracker, action, resource, cluster string, before, after map[string]interface{}, start time.Time, err error) {
	if tracker == nil {
		return
	}
	status, msg := history.StatusSuccess, ""
	if err != nil {
		status, msg = history.StatusFailed, err.Error()
	}
	_ = tracker.Log(c, history.HistoryEntry{
		Action:   action,
		Resource: resource,
		Cluster:  cluster,
		Before:   before,
		After:    after,
		Status:   status,
		ErrorMsg: msg,
		Duration: time.Since(start).Milliseconds(),
	})
}

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
