package handlers

import (
	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
)

// ClusterHandler gerencia requisições relacionadas a clusters
type ClusterHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewClusterHandler cria um novo handler de clusters
func NewClusterHandler(km *config.KubeConfigManager) *ClusterHandler {
	return &ClusterHandler{kubeManager: km}
}

// List retorna todos os clusters descobertos
func (h *ClusterHandler) List(c *gin.Context) {
	// Descobrir clusters (reutilizar código existente)
	clusters := h.kubeManager.DiscoverClusters()

	// Formatar resposta
	response := make([]gin.H, len(clusters))
	for i, cluster := range clusters {
		response[i] = gin.H{
			"name":    cluster.Name,
			"context": cluster.Context,
			"status":  cluster.Status.String(),
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    response,
		"count":   len(response),
	})
}

// Test testa a conexão com um cluster específico
func (h *ClusterHandler) Test(c *gin.Context) {
	clusterName := c.Param("name")

	// Testar conexão (reutilizar código existente)
	status := h.kubeManager.TestClusterConnection(c.Request.Context(), clusterName)

	c.JSON(200, gin.H{
		"success": true,
		"data": gin.H{
			"cluster": clusterName,
			"status":  status.String(),
		},
	})
}
