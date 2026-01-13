package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
)

type ResourceQuotaHandler struct {
	kubeManager *config.KubeConfigManager
}

func NewResourceQuotaHandler(km *config.KubeConfigManager) *ResourceQuotaHandler {
	return &ResourceQuotaHandler{kubeManager: km}
}

type ResourceQuotaSummary struct {
	Cluster   string          `json:"cluster"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Hard      []ResourceLimit `json:"hard"`
}

type ResourceLimit struct {
	Resource string  `json:"resource"`
	Hard     string  `json:"hard"`
	Used     string  `json:"used"`
	Percent  float64 `json:"percent,omitempty"`
}

func (h *ResourceQuotaHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespaces := parseNamespaces(c.Query("namespaces"))

	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	var quotas []ResourceQuotaSummary

	for _, ns := range namespaces {
		quotaList, err := clientset.CoreV1().ResourceQuotas(ns).List(c.Request.Context(), metav1.ListOptions{})
		if err != nil {
			continue
		}

		for _, quota := range quotaList.Items {
			quotas = append(quotas, buildResourceQuotaSummary(cluster, ns, quota))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"quotas": quotas,
			"count":  len(quotas),
		},
	})
}

func buildResourceQuotaSummary(cluster, namespace string, quota corev1.ResourceQuota) ResourceQuotaSummary {
	// Construir limites com percentual de uso
	var hard []ResourceLimit

	for resourceName, hardQuantity := range quota.Status.Hard {
		usedQuantity := quota.Status.Used[resourceName]

		limit := ResourceLimit{
			Resource: string(resourceName),
			Hard:     hardQuantity.String(),
			Used:     usedQuantity.String(),
		}

		// Calcular percentual se possível
		if hardValue := hardQuantity.Value(); hardValue > 0 {
			usedValue := usedQuantity.Value()
			limit.Percent = float64(usedValue) / float64(hardValue) * 100
		}

		hard = append(hard, limit)
	}

	return ResourceQuotaSummary{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      quota.Name,
		Hard:      hard,
	}
}
