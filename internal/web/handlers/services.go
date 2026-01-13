package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
)

type ServiceHandler struct {
	kubeManager *config.KubeConfigManager
}

func NewServiceHandler(km *config.KubeConfigManager) *ServiceHandler {
	return &ServiceHandler{kubeManager: km}
}

type ServiceSummary struct {
	Cluster   string   `json:"cluster"`
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	ClusterIP string   `json:"clusterIP"`
	Ports     []string `json:"ports"`
}

func (h *ServiceHandler) List(c *gin.Context) {
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

	var services []ServiceSummary

	for _, ns := range namespaces {
		svcList, err := clientset.CoreV1().Services(ns).List(c.Request.Context(), metav1.ListOptions{})
		if err != nil {
			continue
		}

		for _, svc := range svcList.Items {
			services = append(services, buildServiceSummary(cluster, ns, svc))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"services": services,
			"count":    len(services),
		},
	})
}

func buildServiceSummary(cluster, namespace string, svc corev1.Service) ServiceSummary {
	// Extrair portas
	var ports []string
	for _, port := range svc.Spec.Ports {
		portStr := fmt.Sprintf("%d", port.Port)
		if port.TargetPort.IntVal > 0 {
			portStr += fmt.Sprintf(":%d", port.TargetPort.IntVal)
		} else if port.TargetPort.StrVal != "" {
			portStr += fmt.Sprintf(":%s", port.TargetPort.StrVal)
		}
		portStr += fmt.Sprintf("/%s", port.Protocol)
		ports = append(ports, portStr)
	}

	return ServiceSummary{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      svc.Name,
		Type:      string(svc.Spec.Type),
		ClusterIP: svc.Spec.ClusterIP,
		Ports:     ports,
	}
}
