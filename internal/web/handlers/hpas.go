package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// HPAHandler gerencia requisições relacionadas a HPAs
type HPAHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewHPAHandler cria um novo handler de HPAs
func NewHPAHandler(km *config.KubeConfigManager) *HPAHandler {
	return &HPAHandler{kubeManager: km}
}

// List retorna todos os HPAs de um namespace
func (h *HPAHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")

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

	if namespace == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'namespace' is required",
			},
		})
		return
	}

	// Obter client do cluster
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(client, cluster)

	// Listar HPAs (reutilizar código existente)
	hpas, err := kubeClient.ListHPAs(c.Request.Context(), namespace)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": fmt.Sprintf("Failed to list HPAs: %v", err),
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    hpas,
		"count":   len(hpas),
	})
}

// Get retorna detalhes de um HPA específico
func (h *HPAHandler) Get(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	// Obter client
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(client, cluster)

	// Listar todos os HPAs e encontrar o específico
	hpas, err := kubeClient.ListHPAs(c.Request.Context(), namespace)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": fmt.Sprintf("Failed to list HPAs: %v", err),
			},
		})
		return
	}

	// Encontrar o HPA específico
	for _, hpa := range hpas {
		if hpa.Name == name {
			c.JSON(200, gin.H{
				"success": true,
				"data":    hpa,
			})
			return
		}
	}

	c.JSON(404, gin.H{
		"success": false,
		"error": gin.H{
			"code":    "NOT_FOUND",
			"message": fmt.Sprintf("HPA %s/%s not found", namespace, name),
		},
	})
}

// Update atualiza um HPA
func (h *HPAHandler) Update(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	var hpa models.HPA
	if err := c.ShouldBindJSON(&hpa); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	// Validações básicas
	if hpa.MinReplicas != nil && *hpa.MinReplicas < 1 {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_VALUE",
				"message": "minReplicas must be >= 1",
			},
		})
		return
	}

	if hpa.MaxReplicas < 1 {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_VALUE",
				"message": "maxReplicas must be >= 1",
			},
		})
		return
	}

	// Obter client
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	kubeClient := kubeclient.NewClient(client, cluster)

	// Aplicar mudanças (reutilizar código existente)
	hpa.Name = name
	hpa.Namespace = namespace
	hpa.Cluster = cluster

	if err := kubeClient.UpdateHPA(c.Request.Context(), hpa); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "UPDATE_ERROR",
				"message": fmt.Sprintf("Failed to update HPA: %v", err),
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("HPA %s/%s updated successfully", namespace, name),
	})
}
