package handlers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/web/validators"
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

	// Buscar configuração do cluster
	clusterConfig, err := findClusterInConfig(req.Cluster)
	if err != nil {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLUSTER_NOT_FOUND",
				"message": fmt.Sprintf("Cluster not found: %v", err),
			},
		})
		return
	}

	// Validar Azure AD
	if err := validators.ValidateAzureAuth(); err != nil {
		c.JSON(401, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_AUTH_FAILED",
				"message": fmt.Sprintf("Azure authentication failed: %v", err),
			},
		})
		return
	}

	// Configurar subscription
	if err := setAzureSubscription(clusterConfig.Subscription); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_SUBSCRIPTION_ERROR",
				"message": fmt.Sprintf("Failed to set subscription: %v", err),
			},
		})
		return
	}

	// Executar operações sequencialmente
	results := make([]gin.H, 0)
	clusterNameForAzure := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")

	for i, poolOp := range req.NodePools {
		stepNum := i + 1
		result := gin.H{
			"step":      stepNum,
			"pool_name": poolOp.Name,
			"order":     poolOp.Order,
		}

		// Log início da operação
		fmt.Printf("\n🔄 [Step %d/%d] Aplicando node pool '%s' (*%d)...\n", stepNum, len(req.NodePools), poolOp.Name, poolOp.Order)

		// Aplicar alterações no node pool
		err := applyNodePoolChanges(
			context.Background(),
			clusterNameForAzure,
			clusterConfig.ResourceGroup,
			poolOp,
		)

		if err != nil {
			result["success"] = false
			result["error"] = err.Error()
			result["message"] = fmt.Sprintf("❌ Falha ao aplicar node pool '%s': %v", poolOp.Name, err)

			fmt.Printf("❌ [Step %d/%d] Erro: %v\n", stepNum, len(req.NodePools), err)

			// Se falhar, parar execução sequencial
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

		// Se temos mais de 1 pool e não é o último, aguardar antes de continuar
		if len(req.NodePools) > 1 && i < len(req.NodePools)-1 {
			waitTime := 10 * time.Second
			fmt.Printf("⏳ Aguardando %v antes de aplicar próximo node pool (*%d)...\n", waitTime, req.NodePools[i+1].Order)
			time.Sleep(waitTime)
		}
	}

	// Sucesso total
	c.JSON(200, gin.H{
		"success": true,
		"message": fmt.Sprintf("✅ Execução sequencial completa! %d node pool(s) aplicado(s)", len(req.NodePools)),
		"results": results,
	})
}

// applyNodePoolChanges aplica alterações em um node pool via Azure CLI
func applyNodePoolChanges(ctx context.Context, clusterName, resourceGroup string, op NodePoolOperation) error {
	// Construir comandos baseado nas mudanças
	commands := make([][]string, 0)

	// Cenário 1: Desabilitar autoscaling e fazer scale (ex: *1 - scale down para 0)
	if !op.AutoscalingEnabled {
		// Comando 1: Desabilitar autoscaling
		commands = append(commands, []string{
			"az", "aks", "nodepool", "update",
			"--resource-group", resourceGroup,
			"--cluster-name", clusterName,
			"--name", op.Name,
			"--disable-cluster-autoscaler",
		})

		// Comando 2: Fazer scale para node count especificado
		commands = append(commands, []string{
			"az", "aks", "nodepool", "scale",
			"--resource-group", resourceGroup,
			"--cluster-name", clusterName,
			"--name", op.Name,
			"--node-count", fmt.Sprintf("%d", op.NodeCount),
		})
	} else {
		// Cenário 2: Atualizar autoscaling com min/max
		// Tenta --update-cluster-autoscaler primeiro (autoscaling já habilitado)
		// Fallback para --enable-cluster-autoscaler (primeira habilitação)
		minStr := fmt.Sprintf("%d", op.MinNodeCount)
		maxStr := fmt.Sprintf("%d", op.MaxNodeCount)

		updateArgs := []string{
			"az", "aks", "nodepool", "update",
			"--resource-group", resourceGroup,
			"--cluster-name", clusterName,
			"--name", op.Name,
			"--update-cluster-autoscaler",
			"--min-count", minStr,
			"--max-count", maxStr,
		}
		fmt.Printf("   🔧 Tentando --update-cluster-autoscaler: %s\n", strings.Join(updateArgs, " "))
		outputUpdate, errUpdate := exec.CommandContext(ctx, updateArgs[0], updateArgs[1:]...).CombinedOutput()
		if errUpdate != nil {
			fmt.Printf("   ⚠️  --update-cluster-autoscaler falhou: %s\nOutput: %s\n   Tentando --enable-cluster-autoscaler...\n", errUpdate, strings.TrimSpace(string(outputUpdate)))
			enableArgs := []string{
				"az", "aks", "nodepool", "update",
				"--resource-group", resourceGroup,
				"--cluster-name", clusterName,
				"--name", op.Name,
				"--enable-cluster-autoscaler",
				"--min-count", minStr,
				"--max-count", maxStr,
			}
			fmt.Printf("   🔧 Tentando --enable-cluster-autoscaler: %s\n", strings.Join(enableArgs, " "))
			outputEnable, errEnable := exec.CommandContext(ctx, enableArgs[0], enableArgs[1:]...).CombinedOutput()
			if errEnable != nil {
				return fmt.Errorf("falha ao atualizar autoscaling.\n--update-cluster-autoscaler: %s (output: %s)\n--enable-cluster-autoscaler: %s (output: %s)",
					errUpdate, strings.TrimSpace(string(outputUpdate)), errEnable, strings.TrimSpace(string(outputEnable)))
			}
			fmt.Printf("   ✅ --enable-cluster-autoscaler executado com sucesso\n")
		} else {
			fmt.Printf("   ✅ --update-cluster-autoscaler executado com sucesso\n")
		}
		return nil
	}

	// Executar comandos sequencialmente (cenário sem autoscaling)
	for cmdIdx, cmdArgs := range commands {
		fmt.Printf("   🔧 Executando comando %d/%d: %s\n", cmdIdx+1, len(commands), strings.Join(cmdArgs, " "))

		cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			return fmt.Errorf("comando falhou: %s - output: %s", err, string(output))
		}

		fmt.Printf("   ✅ Comando %d/%d executado com sucesso\n", cmdIdx+1, len(commands))

		// Pequeno delay entre comandos
		if cmdIdx < len(commands)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return nil
}

// setAzureSubscription configura a subscription do Azure
func setAzureSubscription(subscription string) error {
	cmd := exec.Command("az", "account", "set", "--subscription", subscription)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set subscription: %w", err)
	}
	return nil
}
