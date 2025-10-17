package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// NamespaceHandler gerencia requisições relacionadas a namespaces
type NamespaceHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewNamespaceHandler cria um novo handler de namespaces
func NewNamespaceHandler(km *config.KubeConfigManager) *NamespaceHandler {
	return &NamespaceHandler{kubeManager: km}
}

// List retorna todos os namespaces de um cluster
func (h *NamespaceHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	showSystem := c.DefaultQuery("showSystem", "false") == "true"

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	// Obter client do cluster (reutilizar código existente)
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client for cluster %s: %v", cluster, err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(client, cluster)

	// Listar namespaces (reutilizar código existente)
	namespaces, err := kubeClient.ListNamespaces(c.Request.Context(), showSystem)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": fmt.Sprintf("Failed to list namespaces: %v", err),
			},
		})
		return
	}

	// Formatar resposta
	response := make([]gin.H, len(namespaces))
	for i, ns := range namespaces {
		response[i] = gin.H{
			"name":     ns.Name,
			"cluster":  ns.Cluster,
			"hpaCount": ns.HPACount,
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    response,
		"count":   len(response),
	})
}
