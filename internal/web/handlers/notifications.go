package handlers

import (
	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/notifications"
)

// NotificationHandler gerencia requisições de notificações
type NotificationHandler struct {
	notificationManager *notifications.NotificationManager
}

// NewNotificationHandler cria um novo handler de notificações
func NewNotificationHandler(nm *notifications.NotificationManager) *NotificationHandler {
	return &NotificationHandler{
		notificationManager: nm,
	}
}

// TestNotification envia uma notificação de teste
func (h *NotificationHandler) TestNotification(c *gin.Context) {
	if err := h.notificationManager.TestNotification(); err != nil {
		c.JSON(500, gin.H{
			"error":   "Failed to send test notification",
			"details": err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"message": "Test notification sent successfully",
		"enabled": h.notificationManager.IsEnabled(),
	})
}

// GetStatus retorna o status das notificações
func (h *NotificationHandler) GetStatus(c *gin.Context) {
	c.JSON(200, gin.H{
		"enabled":    h.notificationManager.IsEnabled(),
		"cacheSize":  h.notificationManager.GetAlertCacheSize(),
		"platform":   "windows",
		"mechanism":  "PowerShell Toast Notifications",
	})
}

// ToggleNotifications habilita/desabilita notificações
func (h *NotificationHandler) ToggleNotifications(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	h.notificationManager.EnableNotifications(req.Enabled)

	c.JSON(200, gin.H{
		"message": "Notification settings updated",
		"enabled": req.Enabled,
	})
}

// NotifyAlert envia notificação de alerta manualmente
func (h *NotificationHandler) NotifyAlert(c *gin.Context) {
	var req struct {
		AlertName string `json:"alertName"`
		Severity  string `json:"severity"`
		Cluster   string `json:"cluster"`
		Namespace string `json:"namespace"`
		HPAName   string `json:"hpaName"`
		Message   string `json:"message"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	err := h.notificationManager.NotifyAlert(
		req.AlertName,
		req.Severity,
		req.Cluster,
		req.Namespace,
		req.HPAName,
		req.Message,
	)

	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Failed to send alert notification",
			"details": err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"message": "Alert notification sent",
	})
}
