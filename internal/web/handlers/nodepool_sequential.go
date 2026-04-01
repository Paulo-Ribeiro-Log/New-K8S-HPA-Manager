package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// NodePoolSequentialRequest representa a requisição de execução sequencial
type NodePoolSequentialRequest struct {
	Cluster   string              `json:"cluster" binding:"required"`
	NodePools []NodePoolOperation `json:"node_pools" binding:"required,min=1,max=2"`
}

// NodePoolOperation representa uma operação em um node pool
type NodePoolOperation struct {
	Name               string `json:"name" binding:"required"`
	AutoscalingEnabled bool   `json:"autoscaling_enabled"`
	NodeCount          int32  `json:"node_count"`
	MinNodeCount       int32  `json:"min_node_count"`
	MaxNodeCount       int32  `json:"max_node_count"`
	Order              int    `json:"order"` // 1 ou 2 (*1, *2)
}

// ApplySequential aplica alterações em node pools de forma sequencial
func (h *NodePoolHandler) ApplySequential(c *gin.Context) {
	var req NodePoolSequentialRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request: %v", err),
			},
		})
		return
	}

	// Validar que temos 1 ou 2 node pools
	if len(req.NodePools) == 0 || len(req.NodePools) > 2 {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_NODE_POOL_COUNT",
				"message": "Sequential execution requires 1 or 2 node pools",
			},
		})
		return
	}

	if _, err := findClusterInConfig(req.Cluster); err != nil {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLUSTER_NOT_FOUND",
				"message": fmt.Sprintf("Cluster not found: %v", err),
			},
		})
		return
	}

	provider := h.kubeManager.GetNodeGroupProvider(req.Cluster)
	if err := provider.ValidateAuth(c.Request.Context()); err != nil {
		c.JSON(401, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLOUD_AUTH_FAILED",
				"message": fmt.Sprintf("Cloud authentication failed: %v", err),
			},
		})
		return
	}

	results := make([]gin.H, 0)
	for i, poolOp := range req.NodePools {
		stepNum := i + 1
		result := gin.H{
			"step":      stepNum,
			"pool_name": poolOp.Name,
			"order":     poolOp.Order,
		}

		fmt.Printf("\n🔄 [Step %d/%d] Aplicando node pool '%s' (*%d)...\n", stepNum, len(req.NodePools), poolOp.Name, poolOp.Order)

		if err := applyOpViaProvider(context.Background(), provider, req.Cluster, poolOp); err != nil {
			result["success"] = false
			result["error"] = err.Error()
			result["message"] = fmt.Sprintf("❌ Falha ao aplicar node pool '%s': %v", poolOp.Name, err)
			fmt.Printf("❌ [Step %d/%d] Erro: %v\n", stepNum, len(req.NodePools), err)
			results = append(results, result)
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "SEQUENTIAL_EXECUTION_FAILED",
					"message": fmt.Sprintf("Sequential execution failed at step %d", stepNum),
				},
				"results": results,
			})
			return
		}

		result["success"] = true
		result["message"] = fmt.Sprintf("✅ Node pool '%s' (*%d) aplicado com sucesso", poolOp.Name, poolOp.Order)
		results = append(results, result)
		fmt.Printf("✅ [Step %d/%d] Node pool '%s' aplicado com sucesso\n", stepNum, len(req.NodePools), poolOp.Name)

		if len(req.NodePools) > 1 && i < len(req.NodePools)-1 {
			waitTime := 10 * time.Second
			fmt.Printf("⏳ Aguardando %v antes de aplicar próximo node pool (*%d)...\n", waitTime, req.NodePools[i+1].Order)
			time.Sleep(waitTime)
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("✅ Execução sequencial completa! %d node pool(s) aplicado(s)", len(req.NodePools)),
		"results": results,
	})
}
