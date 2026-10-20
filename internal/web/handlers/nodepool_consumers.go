package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"k8s-hpa-manager/internal/kubernetes"
)

// GetNodeConsumers devolve os pods do node com o consumo atual de CPU/memória (Metrics Server), para o modal de maiores
// consumidores da aba Node Pools. Só leitura.
//
// GET /api/v1/nodes/:cluster/:nodepool/:node/consumers
func (h *NodePoolHandler) GetNodeConsumers(c *gin.Context) {
	cluster := c.Param("cluster")
	nodeName := c.Param("node")
	if cluster == "" || nodeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{
			"code": "MISSING_PARAMETERS", "message": "Parameters 'cluster' and 'node' are required",
		}})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{
			"code": "K8S_CLIENT_ERROR", "message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
		}})
		return
	}
	k8sClient := kubernetes.NewClient(clientset, cluster)

	if mci, err := h.kubeManager.GetMetricsClient(cluster); err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Metrics client unavailable, consumers will have no usage")
	} else if mc, ok := mci.(*metricsclientset.Clientset); ok {
		k8sClient.SetMetricsClient(mc)
	}

	result, err := k8sClient.GetNodeConsumers(c.Request.Context(), nodeName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{
			"code": "GET_NODE_CONSUMERS_ERROR", "message": fmt.Sprintf("Failed to get node consumers: %v", err),
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
