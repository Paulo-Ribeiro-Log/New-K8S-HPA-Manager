package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/monitoring/alerts"
)

// AlertsHandler gerencia endpoints de alertas
type AlertsHandler struct{}

// NewAlertsHandler cria um novo handler de alertas
func NewAlertsHandler() *AlertsHandler {
	return &AlertsHandler{}
}

// GetAlerts retorna todos os alertas de um cluster
func (h *AlertsHandler) GetAlerts(c *gin.Context) {
	clusterName := c.Query("cluster")
	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetro 'cluster' é obrigatório"})
		return
	}

	// Descobrir URL do Prometheus
	promURL, err := getPrometheusURL(clusterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao descobrir Prometheus: %v", err)})
		return
	}

	// Criar cliente de alertas
	client := alerts.NewClient(promURL)

	// Buscar alertas
	alertsList, err := client.GetAlerts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao buscar alertas: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": clusterName,
		"alerts":  alertsList,
		"count":   len(alertsList),
	})
}

// GetAlertsSummary retorna resumo de alertas por severidade
func (h *AlertsHandler) GetAlertsSummary(c *gin.Context) {
	clusterName := c.Query("cluster")
	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetro 'cluster' é obrigatório"})
		return
	}

	promURL, err := getPrometheusURL(clusterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao descobrir Prometheus: %v", err)})
		return
	}

	client := alerts.NewClient(promURL)
	summary, err := client.GetAlertSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao buscar resumo: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": clusterName,
		"summary": summary,
	})
}

// GetHPAAlerts retorna alertas relacionados a HPAs
func (h *AlertsHandler) GetHPAAlerts(c *gin.Context) {
	clusterName := c.Query("cluster")
	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetro 'cluster' é obrigatório"})
		return
	}

	promURL, err := getPrometheusURL(clusterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao descobrir Prometheus: %v", err)})
		return
	}

	client := alerts.NewClient(promURL)
	hpaAlerts, err := client.GetHPAAlerts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao buscar alertas HPA: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": clusterName,
		"alerts":  hpaAlerts,
		"count":   len(hpaAlerts),
	})
}

// GetNodePoolAlerts retorna alertas relacionados a Node Pools
func (h *AlertsHandler) GetNodePoolAlerts(c *gin.Context) {
	clusterName := c.Query("cluster")
	if clusterName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetro 'cluster' é obrigatório"})
		return
	}

	promURL, err := getPrometheusURL(clusterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao descobrir Prometheus: %v", err)})
		return
	}

	client := alerts.NewClient(promURL)
	nodeAlerts, err := client.GetNodePoolAlerts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao buscar alertas Node Pool: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": clusterName,
		"alerts":  nodeAlerts,
		"count":   len(nodeAlerts),
	})
}

// GetHPAAlertsByNamespace retorna alertas de HPA filtrados por namespace
func (h *AlertsHandler) GetHPAAlertsByNamespace(c *gin.Context) {
	clusterName := c.Query("cluster")
	namespace := c.Query("namespace")

	if clusterName == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Parâmetros 'cluster' e 'namespace' são obrigatórios"})
		return
	}

	promURL, err := getPrometheusURL(clusterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao descobrir Prometheus: %v", err)})
		return
	}

	client := alerts.NewClient(promURL)
	hpaAlerts, err := client.GetHPAAlerts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao buscar alertas: %v", err)})
		return
	}

	// Filtrar por namespace
	var filtered []alerts.HPAAlert
	for _, alert := range hpaAlerts {
		if alert.Namespace == namespace {
			filtered = append(filtered, alert)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster":   clusterName,
		"namespace": namespace,
		"alerts":    filtered,
		"count":     len(filtered),
	})
}

// getPrometheusURL descobre a URL do Prometheus para o cluster
func getPrometheusURL(clusterName string) (string, error) {
	// Validar formato do cluster
	// Formato esperado: akspriv-{nome}-{ambiente}
	parts := strings.Split(clusterName, "-")
	if len(parts) < 3 {
		return "", fmt.Errorf("cluster name inválido: %s (esperado: akspriv-{nome}-{ambiente})", clusterName)
	}

	// Construir URL do Prometheus
	// Formato: prometheus-akspriv-{nome}-{ambiente}.viavarejo.com.br
	promURL := fmt.Sprintf("https://prometheus-%s.viavarejo.com.br", clusterName)
	return promURL, nil
}
