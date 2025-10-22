package handlers

import (
	"log"
	"net/http"

	"k8s-hpa-manager/internal/config"

	"github.com/gin-gonic/gin"
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

// SwitchContext muda o contexto ativo do Kubernetes e Azure CLI para o cluster especificado
func (h *ClusterHandler) SwitchContext(c *gin.Context) {
	var request struct {
		Context string `json:"context" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Context é obrigatório",
		})
		return
	}

	log.Printf("[ClusterHandler] Switching context to: %s", request.Context)

	// Trocar contexto do Kubernetes
	if err := h.kubeManager.SwitchContext(request.Context); err != nil {
		log.Printf("[ClusterHandler] Error switching Kubernetes context: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Falha ao trocar contexto do Kubernetes: " + err.Error(),
		})
		return
	}

	// Trocar contexto do Azure CLI (se aplicável)
	if err := h.kubeManager.SwitchAzureContext(request.Context); err != nil {
		log.Printf("[ClusterHandler] Warning: Could not switch Azure context: %v", err)
		// Não falhar aqui, pois nem todos os clusters são Azure
	}

	log.Printf("[ClusterHandler] Context switched successfully to: %s", request.Context)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"context": request.Context,
			"message": "Contexto alterado com sucesso",
		},
	})
}

// GetClusterInfo retorna informações detalhadas sobre o cluster atual
func (h *ClusterHandler) GetClusterInfo(c *gin.Context) {
	clusterName := c.Query("cluster")
	if clusterName == "" {
		// Se não especificado, usar o contexto atual
		clusterName = h.kubeManager.GetCurrentContext()
	}

	// Obter informações básicas do cluster
	clusterInfo, err := h.kubeManager.GetClusterInfo(clusterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "Falha ao obter informações do cluster: " + err.Error(),
		})
		return
	}

	// Obter versão do Kubernetes
	kubernetesVersion, err := h.kubeManager.GetKubernetesVersion(clusterName)
	if err != nil {
		log.Printf("[ClusterHandler] Warning: Could not get Kubernetes version: %v", err)
		kubernetesVersion = "Unknown"
	}

	// Obter métricas do cluster
	metrics, err := h.kubeManager.GetClusterMetrics(clusterName)
	if err != nil {
		log.Printf("[ClusterHandler] Warning: Could not get cluster metrics: %v", err)
		metrics = &config.ClusterMetrics{
			CPUUsagePercent:    0.0,
			MemoryUsagePercent: 0.0,
			NodeCount:          0,
			PodCount:           0,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"cluster":            clusterInfo.Name,
			"context":            clusterInfo.Context,
			"server":             clusterInfo.Server,
			"namespace":          clusterInfo.Namespace,
			"kubernetesVersion":  kubernetesVersion,
			"cpuUsagePercent":    metrics.CPUUsagePercent,
			"memoryUsagePercent": metrics.MemoryUsagePercent,
			"nodeCount":          metrics.NodeCount,
			"podCount":           metrics.PodCount,
		},
	})
}
