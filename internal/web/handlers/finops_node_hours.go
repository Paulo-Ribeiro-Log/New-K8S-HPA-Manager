package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/monitoring/discovery"
)

// GetNodeHours godoc
// GET /api/v1/finops/node-hours?cluster=X — nodes por pool em cada hora da semana (últimos 7 dias,
// Prometheus). Base do card "Fora do horário". Sem Prometheus volta available=false com o motivo.
func (h *FinOpsHandler) GetNodeHours(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	url := discovery.GetPrometheusURL(cluster)
	if url == "" {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "Prometheus do cluster não encontrado"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	nh, err := finops.QueryNodeHours(ctx, url, 7, discovery.RequiresGCPAuth(cluster))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": err.Error()})
		return
	}
	if len(nh.Pools) == 0 {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "kube_node_info sem nodes no padrão aks-<pool>-…-vmss (só AKS)"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": true, "node_hours": nh})
}
