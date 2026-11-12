package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/monitoring/report"
)

type liveNetworkRequest struct {
	Item  report.Item `json:"item"`
	Start time.Time   `json:"start"`
	End   time.Time   `json:"end"`
}

// LiveNetwork — POST /api/v1/monitoring/live/network
// Rede (bytes/s recebidos e enviados, descartes e erros) de UMA aplicação no tempo, do Prometheus do
// cluster dela, para o gráfico do modal. Somente leitura e sem estado.
func (h *MonitoringReportHandler) LiveNetwork(c *gin.Context) {
	var req liveNetworkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	if req.Item.Cluster == "" || req.Item.Namespace == "" || req.Item.Workload == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe cluster, namespace e deployment"})
		return
	}
	end := req.End
	if now := time.Now(); end.After(now) {
		end = now
	}
	q, err := h.newPrometheus(req.Item.Cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Prometheus: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	res, err := report.CollectNetworkSeries(ctx, q, req.Item, req.Start, end)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
