package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler expõe métricas Prometheus
type MetricsHandler struct{}

// NewMetricsHandler cria um novo handler de métricas
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

// Metrics retorna as métricas no formato Prometheus
// GET /metrics
func (h *MetricsHandler) Metrics(c *gin.Context) {
	// Usar o handler padrão do Prometheus
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}
