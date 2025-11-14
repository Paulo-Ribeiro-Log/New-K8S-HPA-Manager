package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/web/validators"
)

// NodePoolHandler gerencia requisições relacionadas a Node Pools
type NodePoolHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewNodePoolHandler cria um novo handler de Node Pools
func NewNodePoolHandler(km *config.KubeConfigManager) *NodePoolHandler {
	return &NodePoolHandler{kubeManager: km}
}

// NodePoolUpdateRequest representa o payload de atualização de um node pool
type NodePoolUpdateRequest struct {
	NodeCount          *int32 `json:"node_count"`
	MinNodeCount       *int32 `json:"min_node_count"`
	MaxNodeCount       *int32 `json:"max_node_count"`
	AutoscalingEnabled *bool  `json:"autoscaling_enabled"`
}

// List retorna todos os node pools de um cluster
func (h *NodePoolHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")

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

	// Buscar configuração do cluster no clusters-config.json
	clusterConfig, err := findClusterInConfig(cluster)
	if err != nil {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLUSTER_NOT_FOUND",
				"message": fmt.Sprintf("Cluster not found in clusters-config.json: %v", err),
			},
		})
		return
	}

	// Validar Azure AD (faz login automático se necessário, igual ao TUI)
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
	cmd := exec.Command("az", "account", "set", "--subscription", clusterConfig.Subscription)
	if err := cmd.Run(); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_SUBSCRIPTION_ERROR",
				"message": fmt.Sprintf("Failed to set subscription: %v", err),
			},
		})
		return
	}

	// Normalizar nome do cluster (remover -admin se existir)
	clusterNameForAzure := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")

	// Listar node pools via Azure CLI
	nodePools, err := loadNodePoolsFromAzure(clusterNameForAzure, clusterConfig.ResourceGroup)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_CLI_ERROR",
				"message": fmt.Sprintf("Failed to load node pools: %v", err),
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    nodePools,
		"count":   len(nodePools),
	})
}

// Update atualiza um node pool específico via Azure CLI
func (h *NodePoolHandler) Update(c *gin.Context) {
	cluster := c.Param("cluster")
	resourceGroup := c.Param("resource_group")
	nodePoolName := c.Param("name")

	if cluster == "" || resourceGroup == "" || nodePoolName == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameters 'cluster', 'resource_group', and 'name' are required",
			},
		})
		return
	}

	// Parse do body
	var req NodePoolUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	// Buscar configuração do cluster no clusters-config.json
	clusterConfig, err := findClusterInConfig(cluster)
	if err != nil {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLUSTER_NOT_FOUND",
				"message": fmt.Sprintf("Cluster not found in clusters-config.json: %v", err),
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
	cmd := exec.Command("az", "account", "set", "--subscription", clusterConfig.Subscription)
	if err := cmd.Run(); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_SUBSCRIPTION_ERROR",
				"message": fmt.Sprintf("Failed to set subscription: %v", err),
			},
		})
		return
	}

	// Normalizar nome do cluster
	clusterNameForAzure := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")

	// Converter request para NodePoolOperation
	op := NodePoolOperation{
		Name:               nodePoolName,
		AutoscalingEnabled: req.AutoscalingEnabled != nil && *req.AutoscalingEnabled,
		NodeCount:          0,
		MinNodeCount:       0,
		MaxNodeCount:       0,
	}

	if req.NodeCount != nil {
		op.NodeCount = *req.NodeCount
	}
	if req.MinNodeCount != nil {
		op.MinNodeCount = *req.MinNodeCount
	}
	if req.MaxNodeCount != nil {
		op.MaxNodeCount = *req.MaxNodeCount
	}

	// Aplicar mudanças via Azure CLI (reutiliza função de sequential)
	if err := applyNodePoolChanges(clusterNameForAzure, resourceGroup, op); err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_OPERATION_FAILED",
				"message": fmt.Sprintf("Failed to update node pool: %v", err),
			},
		})
		return
	}

	// Recarregar node pools para retornar o estado atualizado
	nodePools, err := loadNodePoolsFromAzure(clusterNameForAzure, clusterConfig.ResourceGroup)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "RELOAD_FAILED",
				"message": fmt.Sprintf("Node pool updated but failed to reload: %v", err),
			},
		})
		return
	}

	// Encontrar o node pool atualizado
	var updatedPool *models.NodePool
	for i := range nodePools {
		if nodePools[i].Name == nodePoolName {
			updatedPool = &nodePools[i]
			break
		}
	}

	if updatedPool == nil {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": "Node pool not found after update",
			},
		})
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    updatedPool,
		"message": fmt.Sprintf("Node pool '%s' updated successfully", nodePoolName),
	})
}

// --- Funções auxiliares ---

// findClusterInConfig encontra configuração do cluster no arquivo
func findClusterInConfig(clusterContext string) (*models.ClusterConfig, error) {
	clusters, err := loadClusterConfig()
	if err != nil {
		return nil, err
	}

	// Remover -admin do contexto se existir (kubeconfig contexts têm -admin, config file não)
	clusterNameWithoutAdmin := strings.TrimSuffix(clusterContext, "-admin")

	// Tentar encontrar por context ou cluster name
	for _, cluster := range clusters {
		// Remover -admin do cluster name também para comparação
		configClusterName := strings.TrimSuffix(cluster.ClusterName, "-admin")

		// Comparar sem o sufixo -admin
		if configClusterName == clusterNameWithoutAdmin {
			return &cluster, nil
		}

		// Também comparar exatamente como está
		if cluster.ClusterName == clusterContext {
			return &cluster, nil
		}
	}

	return nil, fmt.Errorf("cluster '%s' not found in clusters-config.json", clusterContext)
}

// loadClusterConfig carrega a configuração de clusters do arquivo
func loadClusterConfig() ([]models.ClusterConfig, error) {
	// 1. Procurar primeiro no diretório padrão ~/.k8s-hpa-manager/
	homeConfigPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json")
	configPath := homeConfigPath

	// Se existir no diretório padrão, usar ele
	if _, err := os.Stat(homeConfigPath); err == nil {
		// Arquivo encontrado no diretório padrão
	} else {
		// 2. Fallback: procurar no diretório do executável
		execPath, execErr := os.Executable()
		if execErr == nil {
			execDir := filepath.Dir(execPath)
			execConfigPath := filepath.Join(execDir, "clusters-config.json")

			if _, err := os.Stat(execConfigPath); err == nil {
				configPath = execConfigPath
			} else {
				// 3. Último fallback: diretório de trabalho atual
				wd, _ := os.Getwd()
				wdConfigPath := filepath.Join(wd, "clusters-config.json")

				if _, err := os.Stat(wdConfigPath); err == nil {
					configPath = wdConfigPath
				} else {
					return nil, fmt.Errorf("clusters-config.json not found. Run 'k8s-hpa-manager autodiscover' to generate it")
				}
			}
		}
	}

	// Ler o arquivo
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read clusters-config.json: %w", err)
	}

	// Parse do JSON
	var clusters []models.ClusterConfig
	if err := json.Unmarshal(data, &clusters); err != nil {
		return nil, fmt.Errorf("failed to parse clusters-config.json: %w", err)
	}

	return clusters, nil
}

// loadNodePoolsFromAzure carrega node pools via Azure CLI
func loadNodePoolsFromAzure(clusterName, resourceGroup string) ([]models.NodePool, error) {
	// Executar comando Azure CLI
	cmd := exec.Command("az", "aks", "nodepool", "list",
		"--resource-group", resourceGroup,
		"--cluster-name", clusterName,
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		// Capturar stderr para melhor debugging
		if exitError, ok := err.(*exec.ExitError); ok {
			stderr := string(exitError.Stderr)

			// Detectar erros de autenticação Azure AD
			if strings.Contains(stderr, "AADSTS") ||
				strings.Contains(stderr, "expired") ||
				strings.Contains(stderr, "authentication") ||
				strings.Contains(stderr, "az login") {
				return nil, fmt.Errorf("Azure CLI not authenticated. Please run on server: az login")
			}

			return nil, fmt.Errorf("az command failed: %s", stderr)
		}
		return nil, fmt.Errorf("failed to execute az command: %w", err)
	}

	// Parse do JSON
	var azureNodePools []AzureNodePool
	if err := json.Unmarshal(output, &azureNodePools); err != nil {
		return nil, fmt.Errorf("failed to parse Azure CLI output: %w", err)
	}

	// Converter para nosso modelo
	var nodePools []models.NodePool
	for _, azPool := range azureNodePools {
		// Converter pointers para valores diretos
		var minCount, maxCount int32
		if azPool.MinCount != nil {
			minCount = *azPool.MinCount
		}
		if azPool.MaxCount != nil {
			maxCount = *azPool.MaxCount
		}

		nodePool := models.NodePool{
			Name:               azPool.Name,
			VMSize:             azPool.VmSize,
			NodeCount:          azPool.Count,
			MinNodeCount:       minCount,
			MaxNodeCount:       maxCount,
			AutoscalingEnabled: azPool.EnableAutoScaling,
			Status:             azPool.ProvisioningState,
			IsSystemPool:       azPool.Mode == "System",
			ClusterName:        clusterName,
			ResourceGroup:      resourceGroup,
		}

		nodePools = append(nodePools, nodePool)
	}

	return nodePools, nil
}

// AzureNodePool representa a estrutura retornada pela Azure CLI
type AzureNodePool struct {
	Name              string `json:"name"`
	VmSize            string `json:"vmSize"`
	Count             int32  `json:"count"`
	MinCount          *int32 `json:"minCount"`          // Pointer pois pode ser null
	MaxCount          *int32 `json:"maxCount"`          // Pointer pois pode ser null
	EnableAutoScaling bool   `json:"enableAutoScaling"` // Campo correto do Azure
	Mode              string `json:"mode"`              // "System" ou "User"
	ProvisioningState string `json:"provisioningState"`
}

// SequenceExecuteRequest representa a requisição para executar sequenciamento com cordon/drain
type SequenceExecuteRequest struct {
	Cluster       string                   `json:"cluster"`
	NodePools     []NodePoolSequenceConfig `json:"node_pools"`
	CordonEnabled bool                     `json:"cordon_enabled"`
	DrainEnabled  bool                     `json:"drain_enabled"`
	DrainOptions  models.DrainOptions      `json:"drain_options"`
}

// NodePoolSequenceConfig representa um node pool no sequenciamento
type NodePoolSequenceConfig struct {
	Name             string                   `json:"name"`
	ResourceGroup    string                   `json:"resource_group"`
	Subscription     string                   `json:"subscription"`
	SequenceOrder    int                      `json:"sequence_order"`    // 1 ou 2
	PreDrainChanges  *models.NodePoolChanges  `json:"pre_drain_changes"`
	PostDrainChanges *models.NodePoolChanges  `json:"post_drain_changes"`
}

// ExecuteSequence executa o sequenciamento de node pools com cordon/drain
func (h *NodePoolHandler) ExecuteSequence(c *gin.Context) {
	var req SequenceExecuteRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "Invalid request body",
				"details": err.Error(),
			},
		})
		return
	}

	// Validar que temos exatamente 2 node pools
	if len(req.NodePools) != 2 {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_NODE_POOLS",
				"message": "Sequencing requires exactly 2 node pools",
			},
		})
		return
	}

	// Validar DrainOptions
	if req.DrainEnabled {
		// Validar usando função do kubernetes client
		// (importar kubernetes package se necessário)
		if err := validateDrainOptions(&req.DrainOptions); err != nil {
			c.JSON(400, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "INVALID_DRAIN_OPTIONS",
					"message": err.Error(),
				},
			})
			return
		}
	}

	// Drain requer Cordon
	if req.DrainEnabled && !req.CordonEnabled {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DRAIN_REQUIRES_CORDON",
				"message": "Drain enabled requires Cordon to be enabled",
			},
		})
		return
	}

	// Ordenar node pools por sequence_order
	origin := req.NodePools[0]
	dest := req.NodePools[1]
	if origin.SequenceOrder == 2 {
		origin, dest = dest, origin
	}

	// Obter cliente Kubernetes
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KUBERNETES_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	// Executar sequenciamento em goroutine para não bloquear
	// TODO: Implementar progress tracking via WebSocket ou polling
	go h.executeSequenceAsync(client, origin, dest, req)

	// Retornar sucesso imediato (operação assíncrona)
	c.JSON(202, gin.H{
		"success": true,
		"message": "Sequencing started",
		"data": gin.H{
			"cluster": req.Cluster,
			"origin":  origin.Name,
			"dest":    dest.Name,
			"phases":  5,
		},
	})
}

// executeSequenceAsync executa o sequenciamento de forma assíncrona
func (h *NodePoolHandler) executeSequenceAsync(client interface{}, origin, dest NodePoolSequenceConfig, req SequenceExecuteRequest) {
	// TODO: Implementar lógica completa de sequenciamento
	// Este é um placeholder - a implementação real virá na integração completa

	// FASE 1: PRE-DRAIN
	// - Aplicar dest.PreDrainChanges
	// - Aguardar 30s para nodes Ready

	// FASE 2: CORDON
	// - GetNodesInNodePool(origin.Name)
	// - CordonNode() para cada node

	// FASE 3: DRAIN
	// - DrainNode() para cada node com req.DrainOptions

	// FASE 4: POST-DRAIN
	// - Aplicar origin.PostDrainChanges

	// FASE 5: FINALIZAÇÃO
	// - Logs e cleanup
}

// validateDrainOptions valida as opções de drain (placeholder)
func validateDrainOptions(opts *models.DrainOptions) error {
	// TODO: Chamar kubernetes.ValidateDrainOptions quando integrar
	if opts.GracePeriod < 0 {
		return fmt.Errorf("grace period must be >= 0")
	}
	if opts.ChunkSize < 1 {
		return fmt.Errorf("chunk size must be >= 1")
	}
	return nil
}
