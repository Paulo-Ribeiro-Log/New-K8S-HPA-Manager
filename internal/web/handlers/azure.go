package handlers

import (
	"log"
	"net/http"
	"os/exec"

	"github.com/gin-gonic/gin"
)

// AzureHandler gerencia operações do Azure CLI
type AzureHandler struct{}

// NewAzureHandler cria um novo handler do Azure
func NewAzureHandler() *AzureHandler {
	return &AzureHandler{}
}

// SetSubscription define a subscription ativa do Azure CLI
func (h *AzureHandler) SetSubscription(c *gin.Context) {
	var request struct {
		Subscription string `json:"subscription" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Subscription is required",
		})
		return
	}

	log.Printf("[AzureHandler] Setting Azure subscription: %s", request.Subscription)

	// Executar: az account set --subscription <name/id>
	cmd := exec.Command("az", "account", "set", "--subscription", request.Subscription)
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Printf("[AzureHandler] Error setting subscription: %v, output: %s", err, string(output))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Failed to set Azure subscription: " + err.Error(),
			"output":  string(output),
		})
		return
	}

	log.Printf("[AzureHandler] Successfully set subscription to: %s", request.Subscription)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"subscription": request.Subscription,
			"message":      "Azure subscription set successfully",
		},
	})
}
