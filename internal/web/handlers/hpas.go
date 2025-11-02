package handlers

import (
	"fmt"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"

	"github.com/gin-gonic/gin"
)

// HPAHandler gerencia requisições relacionadas a HPAs
type HPAHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewHPAHandler cria um novo handler de HPAs
func NewHPAHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *HPAHandler {
	return &HPAHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// List retorna todos os HPAs de um namespace ou de todos os namespaces
func (h *HPAHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace") // Opcional
	showSystemStr := c.Query("showSystem") // Opcional: "true" para mostrar namespaces de sistema

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

	// Parse showSystem parameter (default: false)
	showSystem := false
	if showSystemStr == "true" {
		showSystem = true
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

	var allHPAs []models.HPA

	// Se namespace não especificado, listar de TODOS os namespaces
	if namespace == "" {
		// Primeiro listar todos os namespaces
		namespaces, err := kubeClient.ListNamespaces(c.Request.Context(), showSystem)
		if err != nil {
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "LIST_ERROR",
					"message": fmt.Sprintf("Failed to list namespaces: %v", err),
				},
			})
			return
		}

		// Listar HPAs de cada namespace
		for _, ns := range namespaces {
			hpas, err := kubeClient.ListHPAs(c.Request.Context(), ns.Name)
			if err != nil {
				// Ignorar erros de namespaces individuais, continuar
				continue
			}
			allHPAs = append(allHPAs, hpas...)
		}
	} else {
		// Listar HPAs de um namespace específico
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
		allHPAs = hpas
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    allHPAs,
		"count":   len(allHPAs),
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

	// Timestamp de início para medir duração
	startTime := time.Now()

	var hpa models.HPA
	if err := c.ShouldBindJSON(&hpa); err != nil {
		fmt.Printf("❌ Error parsing JSON: %v\n", err)
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	fmt.Printf("📝 Received HPA update: %+v\n", hpa)

	// Validações básicas (permitir minReplicas = 0 para scale-to-zero)
	if hpa.MinReplicas != nil && *hpa.MinReplicas < 0 {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_VALUE",
				"message": "minReplicas must be >= 0",
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

	// Capturar estado ANTES da alteração (para history)
	beforeHPA, err := kubeClient.GetHPA(c.Request.Context(), namespace, name)
	var beforeState map[string]interface{}
	if err == nil {
		beforeState = map[string]interface{}{
			"min_replicas":    beforeHPA.MinReplicas,
			"max_replicas":    beforeHPA.MaxReplicas,
			"target_cpu":      beforeHPA.TargetCPU,
			"target_memory":   beforeHPA.TargetMemory,
			"cpu_request":     beforeHPA.TargetCPURequest,     // Use Target* (configuração do deployment)
			"memory_request":  beforeHPA.TargetMemoryRequest,  // Use Target* (configuração do deployment)
			"cpu_limit":       beforeHPA.TargetCPULimit,       // Use Target* (configuração do deployment)
			"memory_limit":    beforeHPA.TargetMemoryLimit,    // Use Target* (configuração do deployment)
		}
	}

	// Aplicar mudanças (reutilizar código existente)
	hpa.Name = name
	hpa.Namespace = namespace
	hpa.Cluster = cluster

	if err := kubeClient.UpdateHPA(c.Request.Context(), hpa); err != nil {
		// Log falha no history
		if h.historyTracker != nil && beforeState != nil {
			duration := time.Since(startTime).Milliseconds()
			h.historyTracker.Log(history.HistoryEntry{
				Action:   history.ActionUpdateHPA,
				Resource: fmt.Sprintf("%s/%s", namespace, name),
				Cluster:  cluster,
				Before:   beforeState,
				After:    nil,
				Status:   history.StatusFailed,
				ErrorMsg: err.Error(),
				Duration: duration,
			})
		}

		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "UPDATE_ERROR",
				"message": fmt.Sprintf("Failed to update HPA: %v", err),
			},
		})
		return
	}

	updatedHPA, err := kubeClient.GetHPA(c.Request.Context(), namespace, name)
	if err != nil {
		fmt.Printf("[HPAHandler.Update] ⚠️ Failed to fetch updated HPA: %v\n", err)
		c.JSON(200, gin.H{
			"success": true,
			"message": fmt.Sprintf("HPA %s/%s updated successfully", namespace, name),
		})
		return
	}

	// Capturar estado DEPOIS da alteração
	afterState := map[string]interface{}{
		"min_replicas":   updatedHPA.MinReplicas,
		"max_replicas":   updatedHPA.MaxReplicas,
		"target_cpu":     updatedHPA.TargetCPU,
		"target_memory":  updatedHPA.TargetMemory,
		"cpu_request":    updatedHPA.TargetCPURequest,     // Use Target* (configuração do deployment)
		"memory_request": updatedHPA.TargetMemoryRequest,  // Use Target* (configuração do deployment)
		"cpu_limit":      updatedHPA.TargetCPULimit,       // Use Target* (configuração do deployment)
		"memory_limit":   updatedHPA.TargetMemoryLimit,    // Use Target* (configuração do deployment)
	}

	// Log sucesso no history
	if h.historyTracker != nil {
		duration := time.Since(startTime).Milliseconds()
		h.historyTracker.Log(history.HistoryEntry{
			Action:   history.ActionUpdateHPA,
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Before:   beforeState,
			After:    afterState,
			Status:   history.StatusSuccess,
			Duration: duration,
		})
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("HPA %s/%s updated successfully", namespace, name),
		"data":    updatedHPA,
	})
}
