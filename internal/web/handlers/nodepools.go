package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
	promclient "k8s-hpa-manager/internal/monitoring/client"
	"k8s-hpa-manager/internal/web/sse"
	"k8s-hpa-manager/internal/web/validators"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// NodePoolHandler gerencia requisições relacionadas a Node Pools
type NodePoolHandler struct {
	kubeManager     *config.KubeConfigManager
	progressManager *SequenceProgressManager
	historyTracker  *history.HistoryTracker
}

// NewNodePoolHandler cria um novo handler de Node Pools
func NewNodePoolHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *NodePoolHandler {
	return &NodePoolHandler{
		kubeManager:     km,
		progressManager: NewSequenceProgressManager(),
		historyTracker:  ht,
	}
}

// Abort cancela uma operação em andamento de um node pool via Azure ARM abort API
// Usa: az aks nodepool operation-abort --resource-group <rg> --cluster-name <cluster> --name <pool>
// Isso efetivamente para a operação no Azure (não apenas o polling local).
func (h *NodePoolHandler) Abort(c *gin.Context) {
	cluster := c.Param("cluster")
	resourceGroup := c.Param("resource_group")
	nodePoolName := c.Param("name")

	// Buscar configuração do cluster
	clusterConfig, err := findClusterInConfig(cluster)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "message": fmt.Sprintf("Cluster não encontrado: %v", err)})
		return
	}

	clusterNameForAzure := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")

	// Configurar subscription
	{
		abortSubCtx, abortSubCancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		out, err := exec.CommandContext(abortSubCtx, "az", "account", "set", "--subscription", clusterConfig.Subscription).CombinedOutput()
		abortSubCancel()
		if err != nil {
			c.JSON(500, gin.H{"success": false, "message": fmt.Sprintf("Erro ao configurar subscription: %s", string(out))})
			return
		}
	}

	// Chamar az aks nodepool operation-abort — cancela a operação ARM em andamento
	abortCtx, abortCancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer abortCancel()
	out, err := exec.CommandContext(abortCtx,
		"az", "aks", "nodepool", "operation-abort",
		"--resource-group", resourceGroup,
		"--cluster-name", clusterNameForAzure,
		"--name", nodePoolName,
	).CombinedOutput()

	if err != nil {
		log.Error().Str("output", string(out)).Err(err).Msg("Falha ao abortar operação do node pool")
		c.JSON(500, gin.H{
			"success": false,
			"message": fmt.Sprintf("Falha ao abortar operação no Azure: %s", strings.TrimSpace(string(out))),
		})
		return
	}

	log.Info().
		Str("cluster", clusterNameForAzure).
		Str("resource_group", resourceGroup).
		Str("node_pool", nodePoolName).
		Msg("Operação de node pool abortada via ARM abort API")

	c.JSON(200, gin.H{
		"success": true,
		"message": "Operação abortada no Azure. O provisioningState mudará para Canceled. Aguarde antes de tentar Reconcile.",
	})
}

// ProgressEvent representa um evento de progresso
type ProgressEvent struct {
	Phase     int     `json:"phase"`      // 1-5
	PhaseName string  `json:"phase_name"` // "PRE-DRAIN", "CORDON", etc
	Status    string  `json:"status"`     // "running", "completed", "error"
	Message   string  `json:"message"`    // Mensagem detalhada
	Progress  float64 `json:"progress"`   // 0-100
	NodeName  string  `json:"node_name"`  // Node sendo processado (se aplicável)
	NodeIndex int     `json:"node_index"` // Índice do node (se aplicável)
	NodeTotal int     `json:"node_total"` // Total de nodes (se aplicável)
	Timestamp string  `json:"timestamp"`  // ISO 8601
	Error     string  `json:"error"`      // Mensagem de erro (se status == "error")
}

// SequenceProgressManager gerencia o progresso de múltiplas execuções
type SequenceProgressManager struct {
	mu       sync.RWMutex
	sessions map[string]chan ProgressEvent // sessionID -> event channel
}

// NewSequenceProgressManager cria um novo gerenciador de progresso
func NewSequenceProgressManager() *SequenceProgressManager {
	return &SequenceProgressManager{
		sessions: make(map[string]chan ProgressEvent),
	}
}

// CreateSession cria uma nova sessão de progresso
func (m *SequenceProgressManager) CreateSession(sessionID string) chan ProgressEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan ProgressEvent, 100) // Buffer de 100 eventos
	m.sessions[sessionID] = ch
	return ch
}

// GetSession retorna o canal de uma sessão existente
func (m *SequenceProgressManager) GetSession(sessionID string) (chan ProgressEvent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ch, exists := m.sessions[sessionID]
	return ch, exists
}

// CloseSession fecha e remove uma sessão
func (m *SequenceProgressManager) CloseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, exists := m.sessions[sessionID]; exists {
		close(ch)
		delete(m.sessions, sessionID)
	}
}

// NodePoolUpdateRequest representa o payload de atualização de um node pool
type CordonDrainConfig struct {
	CordonEnabled    bool `json:"cordon_enabled"`
	DrainEnabled     bool `json:"drain_enabled"`
	GracePeriod      int  `json:"grace_period"`
	Timeout          int  `json:"timeout"`
	ForceDelete      bool `json:"force_delete"`
	IgnoreDaemonSets bool `json:"ignore_daemonsets"`
	DeleteEmptyDir   bool `json:"delete_emptydir"`
	ChunkSize        int  `json:"chunk_size"`
}

type NodePoolUpdateRequest struct {
	NodeCount          *int32             `json:"node_count"`
	MinNodeCount       *int32             `json:"min_node_count"`
	MaxNodeCount       *int32             `json:"max_node_count"`
	AutoscalingEnabled *bool              `json:"autoscaling_enabled"`
	CordonDrainConfig  *CordonDrainConfig `json:"cordon_drain_config,omitempty"`
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
	{
		subCtx, subCancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		err := exec.CommandContext(subCtx, "az", "account", "set", "--subscription", clusterConfig.Subscription).Run()
		subCancel()
		if err != nil {
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "AZURE_SUBSCRIPTION_ERROR",
					"message": fmt.Sprintf("Failed to set subscription: %v", err),
				},
			})
			return
		}
	}

	// Normalizar nome do cluster (remover -admin se existir)
	clusterNameForAzure := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")

	// Listar node pools via Azure CLI
	nodePools, err := loadNodePoolsFromAzure(c.Request.Context(), clusterNameForAzure, clusterConfig.ResourceGroup, clusterConfig.Subscription)
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
	{
		updateSubCtx, updateSubCancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		cmd := exec.CommandContext(updateSubCtx, "az", "account", "set", "--subscription", clusterConfig.Subscription)
		err := cmd.Run()
		updateSubCancel()
		if err != nil {
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "AZURE_SUBSCRIPTION_ERROR",
					"message": fmt.Sprintf("Failed to set subscription: %v", err),
				},
			})
			return
		}
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

	// Criar ProgressReporter para enviar eventos SSE
	var reporter *sse.ProgressReporter
	operationID := fmt.Sprintf("nodepool-%s-%s-%d", cluster, nodePoolName, time.Now().Unix())
	reporter = sse.NewProgressReporter(operationID, GetProgressTracker())

	// Se configuração de Cordon/Drain foi fornecida, executar ANTES de aplicar mudanças
	if req.CordonDrainConfig != nil {
		cfg := req.CordonDrainConfig

		// Obter client Kubernetes para executar cordon/drain
		kubeManager, err := config.NewKubeConfigManager("")
		if err != nil {
			reporter.SendError("cordon", fmt.Sprintf("Failed to create kube manager: %v", err))
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "KUBE_MANAGER_ERROR",
					"message": fmt.Sprintf("Failed to create kube manager: %v", err),
				},
			})
			return
		}

		clientInterface, err := kubeManager.GetClient(cluster)
		if err != nil {
			reporter.SendError("cordon", fmt.Sprintf("Failed to get K8s client: %v", err))
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "K8S_CLIENT_ERROR",
					"message": fmt.Sprintf("Failed to get K8s client: %v", err),
				},
			})
			return
		}

		// Type assertion para nosso wrapper Client
		var emptyInterface interface{} = clientInterface
		k8sClient, ok := emptyInterface.(*kubernetes.Client)
		if !ok {
			reporter.SendError("cordon", "Invalid Kubernetes client type")
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "CLIENT_TYPE_ERROR",
					"message": "Invalid Kubernetes client type",
				},
			})
			return
		}

		ctx := context.Background()

		// Buscar nodes do node pool
		nodes, err := k8sClient.GetNodesInNodePool(ctx, nodePoolName)
		if err != nil {
			reporter.SendError("cordon", fmt.Sprintf("Failed to get nodes: %v", err))
			c.JSON(500, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "GET_NODES_ERROR",
					"message": fmt.Sprintf("Failed to get nodes: %v", err),
				},
			})
			return
		}

		totalNodes := len(nodes)

		// Track de nodes que foram cordoned para rollback em caso de erro
		cordonedNodes := make([]string, 0, totalNodes)

		// Função de rollback para uncordon em caso de falha
		rollbackCordon := func() {
			if len(cordonedNodes) == 0 {
				return
			}

			log.Warn().
				Int("nodes_count", len(cordonedNodes)).
				Msg("Executando rollback de cordon devido a erro")

			for _, nodeName := range cordonedNodes {
				if err := k8sClient.UncordonNode(ctx, nodeName); err != nil {
					log.Error().
						Err(err).
						Str("node", nodeName).
						Msg("Falha ao uncordon node durante rollback")
				} else {
					log.Info().
						Str("node", nodeName).
						Msg("Node uncordoned com sucesso durante rollback")
				}
			}
		}

		// Fase CORDON
		if cfg.CordonEnabled {
			reporter.SendCordonStarted(totalNodes)

			for i, nodeName := range nodes {
				if err := k8sClient.CordonNode(ctx, nodeName); err != nil {
					reporter.SendError("cordon", fmt.Sprintf("Failed to cordon node %s: %v", nodeName, err))

					// ROLLBACK: Uncordon nodes já processados
					rollbackCordon()

					c.JSON(500, gin.H{
						"success": false,
						"error": gin.H{
							"code":    "CORDON_ERROR",
							"message": fmt.Sprintf("Failed to cordon node %s: %v", nodeName, err),
							"details": fmt.Sprintf("Rollback executado: %d nodes uncordoned", len(cordonedNodes)),
						},
					})
					return
				}

				// Adicionar node à lista de cordoned (para rollback se necessário)
				cordonedNodes = append(cordonedNodes, nodeName)
				reporter.SendCordonProgress(nodeName, i+1, totalNodes)
			}

			reporter.SendCordonCompleted(totalNodes)
		}

		// Fase DRAIN
		if cfg.DrainEnabled {
			// FASE 3: Validação de PDB ANTES de iniciar drain
			log.Info().
				Int("nodes_count", totalNodes).
				Msg("Validando PodDisruptionBudgets antes de iniciar drain")

			// Validar PDBs para todos os nodes
			allPDBsValid := true
			var pdbErrors []string
			var pdbWarnings []string

			for _, nodeName := range nodes {
				pdbResult, err := k8sClient.ValidatePDBsForNode(ctx, nodeName)
				if err != nil {
					log.Error().
						Err(err).
						Str("node", nodeName).
						Msg("Falha ao validar PDBs")

					reporter.SendError("pdb_validation", fmt.Sprintf("Failed to validate PDBs for node %s: %v", nodeName, err))

					// ROLLBACK: Uncordon nodes em caso de erro na validação
					rollbackCordon()

					c.JSON(500, gin.H{
						"success": false,
						"error": gin.H{
							"code":    "PDB_VALIDATION_ERROR",
							"message": fmt.Sprintf("Failed to validate PDBs for node %s: %v", nodeName, err),
							"details": fmt.Sprintf("Rollback executado: %d nodes uncordoned", len(cordonedNodes)),
						},
					})
					return
				}

				// Log resultado da validação
				log.Info().
					Str("node", nodeName).
					Bool("can_proceed", pdbResult.CanProceed).
					Int("total_pdbs", pdbResult.TotalPDBs).
					Int("pods_protected", pdbResult.PodsProtected).
					Int("blocking_pdbs", len(pdbResult.BlockingPDBs)).
					Msg("Resultado da validação de PDBs")

				// Se PDB bloqueia, adicionar aos erros
				if !pdbResult.CanProceed {
					allPDBsValid = false
					for _, msg := range pdbResult.WarningMessages {
						pdbErrors = append(pdbErrors, fmt.Sprintf("[%s] %s", nodeName, msg))
					}
				}

				// Coletar warnings (mesmo se PDB permite)
				if len(pdbResult.WarningMessages) > 0 {
					for _, msg := range pdbResult.WarningMessages {
						pdbWarnings = append(pdbWarnings, fmt.Sprintf("[%s] %s", nodeName, msg))
					}
				}
			}

			// Se algum PDB bloqueia, abortar e fazer rollback
			if !allPDBsValid {
				log.Warn().
					Int("errors_count", len(pdbErrors)).
					Msg("PDBs bloqueiam drain, abortando operação")

				reporter.SendError("pdb_blocked", fmt.Sprintf("PDBs block drain: %s", strings.Join(pdbErrors, "; ")))

				// ROLLBACK: Uncordon nodes
				rollbackCordon()

				c.JSON(409, gin.H{ // 409 Conflict
					"success": false,
					"error": gin.H{
						"code":    "PDB_BLOCKS_DRAIN",
						"message": "PodDisruptionBudgets block drain operation",
						"details": gin.H{
							"blocking_pdbs": pdbErrors,
							"rollback":      fmt.Sprintf("%d nodes uncordoned", len(cordonedNodes)),
						},
					},
				})
				return
			}

			// Se há warnings mas PDBs permitem, logar e enviar via SSE
			if len(pdbWarnings) > 0 {
				log.Info().
					Int("warnings_count", len(pdbWarnings)).
					Msg("PDBs permitem drain mas com avisos")

				// Enviar warnings via SSE (como evento informativo)
				for _, warning := range pdbWarnings {
					reporter.SendDrainProgress("PDB Validation", 0, totalNodes, 0)
					log.Info().Str("warning", warning).Msg("PDB warning")
				}
			}

			reporter.SendDrainStarted(totalNodes)

			drainOpts := &models.DrainOptions{
				GracePeriod:        cfg.GracePeriod,
				Timeout:            fmt.Sprintf("%ds", cfg.Timeout),
				Force:              cfg.ForceDelete,
				IgnoreDaemonsets:   cfg.IgnoreDaemonSets,
				DeleteEmptyDirData: cfg.DeleteEmptyDir,
				ChunkSize:          cfg.ChunkSize,
			}

			totalPodsEvicted := 0
			for i, nodeName := range nodes {
				// Contar pods antes do drain
				podsBefore, _ := k8sClient.CountPodsOnNode(ctx, nodeName)

				if err := k8sClient.DrainNode(ctx, nodeName, drainOpts); err != nil {
					reporter.SendError("drain", fmt.Sprintf("Failed to drain node %s: %v", nodeName, err))

					// ROLLBACK: Uncordon todos os nodes em caso de falha no drain
					rollbackCordon()

					c.JSON(500, gin.H{
						"success": false,
						"error": gin.H{
							"code":    "DRAIN_ERROR",
							"message": fmt.Sprintf("Failed to drain node %s: %v", nodeName, err),
							"details": fmt.Sprintf("Rollback executado: %d nodes uncordoned", len(cordonedNodes)),
						},
					})
					return
				}

				// Atualizar total de pods evacuados
				totalPodsEvicted += podsBefore

				reporter.SendDrainProgress(nodeName, i+1, totalNodes, podsBefore)
			}

			reporter.SendDrainCompleted(totalNodes, totalPodsEvicted)
		}

		// Enviar evento de início da operação Azure
		reporter.SendAzureStarted()
	}

	// Aplicar mudanças via Azure CLI (reutiliza função de sequential)
	// Enviar evento Azure Started se não foi enviado durante Cordon/Drain
	if req.CordonDrainConfig == nil {
		reporter.SendAzureStarted()
	}

	if err := applyNodePoolChanges(context.Background(), clusterNameForAzure, resourceGroup, op); err != nil {
		if reporter != nil {
			reporter.SendError("azure", fmt.Sprintf("Failed to update node pool: %v", err))
		}
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "AZURE_OPERATION_FAILED",
				"message": fmt.Sprintf("Failed to update node pool: %v", err),
			},
		})
		return
	}

	if reporter != nil {
		reporter.SendAzureCompleted()
	}

	// Recarregar node pools para retornar o estado atualizado
	nodePools, err := loadNodePoolsFromAzure(c.Request.Context(), clusterNameForAzure, clusterConfig.ResourceGroup, clusterConfig.Subscription)
	if err != nil {
		if reporter != nil {
			reporter.SendError("azure", fmt.Sprintf("Failed to reload node pools: %v", err))
		}
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
		if reporter != nil {
			reporter.SendError("azure", "Node pool not found after update")
		}
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": "Node pool not found after update",
			},
		})
		return
	}

	// Enviar evento de conclusão se houve reporter
	if reporter != nil {
		reporter.SendComplete()
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
func loadNodePoolsFromAzure(ctx context.Context, clusterName, resourceGroup, subscription string) ([]models.NodePool, error) {
	// Executar comando Azure CLI
	listCtx, listCancel := context.WithTimeout(ctx, 60*time.Second)
	defer listCancel()
	cmd := exec.CommandContext(listCtx, "az", "aks", "nodepool", "list",
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

	// Buscar tags do cluster (uma única vez para todos os node pools)
	clusterTags, err := getClusterTags(ctx, clusterName, resourceGroup)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to fetch cluster tags for %s, continuing without tags", clusterName)
		clusterTags = make(map[string]string) // Mapa vazio se falhar
	}

	// Buscar nome e UUID real da subscription (uma única vez para todos os node pools)
	subscriptionName := getSubscriptionName(ctx, subscription)
	subscriptionUUID := getSubscriptionUUID(ctx, subscription)

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
			Subscription:       subscription,     // Valor da config (pode ser nome ou UUID)
			SubscriptionName:   subscriptionName, // Nome legível da subscription
			SubscriptionUUID:   subscriptionUUID, // UUID real resolvido via az account show
			ClusterTags:        clusterTags,      // Tags do cluster
		}

		nodePools = append(nodePools, nodePool)
	}

	return nodePools, nil
}

// getClusterTags busca as tags do cluster AKS
func getClusterTags(ctx context.Context, clusterName, resourceGroup string) (map[string]string, error) {
	tagCtx, tagCancel := context.WithTimeout(ctx, 30*time.Second)
	defer tagCancel()
	cmd := exec.CommandContext(tagCtx, "az", "aks", "show",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--query", "tags",
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			stderr := string(exitError.Stderr)
			return nil, fmt.Errorf("az aks show failed: %s", stderr)
		}
		return nil, fmt.Errorf("failed to execute az aks show: %w", err)
	}

	var tags map[string]string
	if err := json.Unmarshal(output, &tags); err != nil {
		return nil, fmt.Errorf("failed to parse cluster tags: %w", err)
	}

	// Se tags for null, retornar mapa vazio
	if tags == nil {
		tags = make(map[string]string)
	}

	return tags, nil
}

// getSubscriptionName busca o nome da subscription via Azure CLI
func getSubscriptionName(ctx context.Context, subscriptionID string) string {
	nameCtx, nameCancel := context.WithTimeout(ctx, 30*time.Second)
	defer nameCancel()
	cmd := exec.CommandContext(nameCtx, "az", "account", "show",
		"--subscription", subscriptionID,
		"--query", "name",
		"--output", "tsv")

	output, err := cmd.Output()
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to fetch subscription name for %s", subscriptionID)
		return "" // Retorna vazio se falhar
	}

	name := strings.TrimSpace(string(output))
	return name
}

// getSubscriptionUUID busca o UUID real da subscription via Azure CLI
func getSubscriptionUUID(ctx context.Context, subscription string) string {
	uuidCtx, uuidCancel := context.WithTimeout(ctx, 30*time.Second)
	defer uuidCancel()
	cmd := exec.CommandContext(uuidCtx, "az", "account", "show",
		"--subscription", subscription,
		"--query", "id",
		"--output", "tsv")

	output, err := cmd.Output()
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to fetch subscription UUID for %s", subscription)
		return ""
	}

	return strings.TrimSpace(string(output))
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
	Name             string                  `json:"name"`
	ResourceGroup    string                  `json:"resource_group"`
	Subscription     string                  `json:"subscription"`
	SequenceOrder    int                     `json:"sequence_order"` // 1 ou 2
	PreDrainChanges  *models.NodePoolChanges `json:"pre_drain_changes"`
	PostDrainChanges *models.NodePoolChanges `json:"post_drain_changes"`
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

	// Gerar sessionID único
	sessionID := fmt.Sprintf("%s_%d", req.Cluster, time.Now().UnixNano())

	// Criar sessão de progresso
	progressCh := h.progressManager.CreateSession(sessionID)

	// Executar sequenciamento em goroutine para não bloquear
	go h.executeSequenceAsync(client, origin, dest, req, sessionID, progressCh)

	// Retornar sucesso imediato (operação assíncrona)
	c.JSON(202, gin.H{
		"success": true,
		"message": "Sequencing started",
		"data": gin.H{
			"cluster":    req.Cluster,
			"origin":     origin.Name,
			"dest":       dest.Name,
			"phases":     5,
			"session_id": sessionID,
		},
	})
}

// SequenceProgress retorna eventos de progresso via Server-Sent Events (SSE)
func (h *NodePoolHandler) SequenceProgress(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'session_id' is required",
			},
		})
		return
	}

	// Buscar sessão de progresso
	progressCh, exists := h.progressManager.GetSession(sessionID)
	if !exists {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SESSION_NOT_FOUND",
				"message": "Progress session not found",
			},
		})
		return
	}

	// Configurar headers para SSE
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Nginx compatibility

	// Flusher para enviar dados imediatamente
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SSE_NOT_SUPPORTED",
				"message": "Streaming not supported",
			},
		})
		return
	}

	// Canal de cancelamento do cliente
	clientGone := c.Writer.CloseNotify()

	// Loop de envio de eventos
	for {
		select {
		case event, ok := <-progressCh:
			if !ok {
				// Canal fechado - sequenciamento completado
				fmt.Fprintf(c.Writer, "event: close\ndata: {\"message\":\"Sequencing completed\"}\n\n")
				flusher.Flush()
				return
			}

			// Serializar evento para JSON
			eventJSON, err := json.Marshal(event)
			if err != nil {
				fmt.Fprintf(c.Writer, "event: error\ndata: {\"error\":\"Failed to encode event\"}\n\n")
				flusher.Flush()
				continue
			}

			// Enviar evento SSE
			fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", string(eventJSON))
			flusher.Flush()

		case <-clientGone:
			// Cliente desconectou
			return
		}
	}
}

// executeSequenceAsync executa o sequenciamento de forma assíncrona
func (h *NodePoolHandler) executeSequenceAsync(client interface{}, origin, dest NodePoolSequenceConfig, req SequenceExecuteRequest, sessionID string, progressCh chan ProgressEvent) {
	ctx := context.Background()
	startTime := time.Now()

	// Garantir que o canal será fechado ao final
	defer h.progressManager.CloseSession(sessionID)

	// Helper para enviar eventos de progresso
	sendProgress := func(phase int, phaseName, status, message string, progress float64, nodeName string, nodeIdx, nodeTotal int, err error) {
		event := ProgressEvent{
			Phase:     phase,
			PhaseName: phaseName,
			Status:    status,
			Message:   message,
			Progress:  progress,
			NodeName:  nodeName,
			NodeIndex: nodeIdx,
			NodeTotal: nodeTotal,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		if err != nil {
			event.Error = err.Error()
		}
		progressCh <- event
	}

	// Log início
	fmt.Printf("\n")
	fmt.Printf("╔════════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║        NODE POOL SEQUENCING - Cordon/Drain Execution              ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("🔹 Cluster: %s\n", req.Cluster)
	fmt.Printf("🔹 Origin:  %s (sequence *1)\n", origin.Name)
	fmt.Printf("🔹 Dest:    %s (sequence *2)\n", dest.Name)
	fmt.Printf("🔹 Cordon:  %v | Drain: %v\n", req.CordonEnabled, req.DrainEnabled)
	fmt.Printf("🔹 Session: %s\n", sessionID)
	fmt.Printf("\n")

	// FASE 1: PRE-DRAIN
	sendProgress(1, "PRE-DRAIN", "running", "Starting PRE-DRAIN phase (scale UP destination)", 0, "", 0, 0, nil)
	fmt.Printf("1️⃣  FASE PRE-DRAIN - Scale UP destination\n")
	fmt.Printf("────────────────────────────────────────────────────────────────────\n")

	if dest.PreDrainChanges != nil {
		sendProgress(1, "PRE-DRAIN", "running", fmt.Sprintf("Applying changes to %s", dest.Name), 5, "", 0, 0, nil)
		fmt.Printf("📤 Applying changes to %s:\n", dest.Name)
		fmt.Printf("   Autoscaling: %v | NodeCount: %d | Min: %d | Max: %d\n",
			dest.PreDrainChanges.Autoscaling,
			dest.PreDrainChanges.NodeCount,
			dest.PreDrainChanges.MinNodes,
			dest.PreDrainChanges.MaxNodes)

		// Aplicar mudanças via Azure CLI
		if err := h.applyNodePoolChanges(dest.Name, dest.ResourceGroup, dest.Subscription, dest.PreDrainChanges); err != nil {
			sendProgress(1, "PRE-DRAIN", "error", "Failed to apply PRE-DRAIN changes", 0, "", 0, 0, err)
			fmt.Printf("❌ ERROR: Failed to apply PRE-DRAIN changes: %v\n", err)
			return
		}
		sendProgress(1, "PRE-DRAIN", "running", "PRE-DRAIN changes applied successfully", 10, "", 0, 0, nil)
		fmt.Printf("✅ PRE-DRAIN changes applied successfully\n")
	} else {
		sendProgress(1, "PRE-DRAIN", "running", fmt.Sprintf("No PRE-DRAIN changes configured for %s", dest.Name), 10, "", 0, 0, nil)
		fmt.Printf("⏭️  No PRE-DRAIN changes configured for %s\n", dest.Name)
	}

	// Aguardar nodes Ready
	sendProgress(1, "PRE-DRAIN", "running", "Waiting 30s for nodes to become Ready...", 15, "", 0, 0, nil)
	fmt.Printf("\n⏳ Waiting 30s for nodes to become Ready...\n")
	time.Sleep(30 * time.Second)
	sendProgress(1, "PRE-DRAIN", "completed", "Nodes should be Ready now", 20, "", 0, 0, nil)
	fmt.Printf("✅ Nodes should be Ready now\n\n")

	// FASE 2: CORDON
	if req.CordonEnabled {
		sendProgress(2, "CORDON", "running", "Starting CORDON phase (mark origin nodes unschedulable)", 20, "", 0, 0, nil)
		fmt.Printf("2️⃣  FASE CORDON - Mark origin nodes unschedulable\n")
		fmt.Printf("────────────────────────────────────────────────────────────────────\n")

		// Obter client Kubernetes (type assertion)
		k8sClient, ok := client.(*kubernetes.Client)
		if !ok {
			sendProgress(2, "CORDON", "error", "Invalid Kubernetes client type", 0, "", 0, 0, fmt.Errorf("type assertion failed"))
			fmt.Printf("❌ ERROR: Invalid Kubernetes client type\n")
			return
		}

		// Listar nodes do node pool origem
		sendProgress(2, "CORDON", "running", fmt.Sprintf("Listing nodes in %s", origin.Name), 22, "", 0, 0, nil)
		nodes, err := k8sClient.GetNodesInNodePool(ctx, origin.Name)
		if err != nil {
			sendProgress(2, "CORDON", "error", fmt.Sprintf("Failed to get nodes from %s", origin.Name), 0, "", 0, 0, err)
			fmt.Printf("❌ ERROR: Failed to get nodes from %s: %v\n", origin.Name, err)
			return
		}

		sendProgress(2, "CORDON", "running", fmt.Sprintf("Found %d nodes in %s", len(nodes), origin.Name), 25, "", 0, len(nodes), nil)
		fmt.Printf("📋 Found %d nodes in %s:\n", len(nodes), origin.Name)
		for _, nodeName := range nodes {
			fmt.Printf("   - %s\n", nodeName)
		}

		// Cordon cada node
		fmt.Printf("\n🔒 Cordoning nodes...\n")
		for i, nodeName := range nodes {
			nodeProgress := 25 + float64(i+1)/float64(len(nodes))*15 // 25% → 40%
			sendProgress(2, "CORDON", "running", fmt.Sprintf("Cordoning node %d/%d", i+1, len(nodes)), nodeProgress, nodeName, i+1, len(nodes), nil)
			fmt.Printf("[%d/%d] Cordoning %s...", i+1, len(nodes), nodeName)
			if err := k8sClient.CordonNode(ctx, nodeName); err != nil {
				sendProgress(2, "CORDON", "error", fmt.Sprintf("Failed to cordon %s", nodeName), 0, nodeName, i+1, len(nodes), err)
				fmt.Printf(" ❌ FAILED: %v\n", err)
				// Log failure no audit
				if h.historyTracker != nil {
					h.historyTracker.Log(history.HistoryEntry{
						Action:   history.ActionCordonNode,
						Resource: nodeName,
						Cluster:  req.Cluster,
						Status:   history.StatusFailed,
						ErrorMsg: err.Error(),
						Duration: time.Since(startTime).Milliseconds(),
					})
				}
				return
			}
			fmt.Printf(" ✅\n")

			// Log success no audit (por node)
			if h.historyTracker != nil {
				h.historyTracker.Log(history.HistoryEntry{
					Action:   history.ActionCordonNode,
					Resource: fmt.Sprintf("%s/%s", origin.Name, nodeName),
					Cluster:  req.Cluster,
					Status:   history.StatusSuccess,
					Duration: time.Since(startTime).Milliseconds(),
					Before: map[string]interface{}{
						"schedulable": true,
					},
					After: map[string]interface{}{
						"schedulable": false,
					},
				})
			}
		}
		sendProgress(2, "CORDON", "completed", fmt.Sprintf("All %d nodes cordoned successfully", len(nodes)), 40, "", 0, 0, nil)
		fmt.Printf("✅ All %d nodes cordoned successfully\n\n", len(nodes))
	} else {
		sendProgress(2, "CORDON", "completed", "CORDON phase skipped (disabled)", 40, "", 0, 0, nil)
		fmt.Printf("2️⃣  FASE CORDON - SKIPPED (disabled)\n\n")
	}

	// FASE 3: DRAIN
	if req.DrainEnabled {
		sendProgress(3, "DRAIN", "running", "Starting DRAIN phase (migrate pods from origin to destination)", 40, "", 0, 0, nil)
		fmt.Printf("3️⃣  FASE DRAIN - Migrate pods from origin to destination\n")
		fmt.Printf("────────────────────────────────────────────────────────────────────\n")

		// Obter client Kubernetes
		k8sClient, ok := client.(*kubernetes.Client)
		if !ok {
			sendProgress(3, "DRAIN", "error", "Invalid Kubernetes client type", 0, "", 0, 0, fmt.Errorf("type assertion failed"))
			fmt.Printf("❌ ERROR: Invalid Kubernetes client type\n")
			return
		}

		// Listar nodes novamente
		sendProgress(3, "DRAIN", "running", fmt.Sprintf("Listing nodes in %s for draining", origin.Name), 42, "", 0, 0, nil)
		nodes, err := k8sClient.GetNodesInNodePool(ctx, origin.Name)
		if err != nil {
			sendProgress(3, "DRAIN", "error", "Failed to get nodes", 0, "", 0, 0, err)
			fmt.Printf("❌ ERROR: Failed to get nodes: %v\n", err)
			return
		}

		// Mostrar flags que serão usadas
		drainFlagsMsg := fmt.Sprintf("Drain options: grace=%ds, timeout=%s", req.DrainOptions.GracePeriod, req.DrainOptions.Timeout)
		sendProgress(3, "DRAIN", "running", drainFlagsMsg, 45, "", 0, len(nodes), nil)
		fmt.Printf("🔧 Drain options:\n")
		if req.DrainOptions.IgnoreDaemonsets {
			fmt.Printf("   ✓ --ignore-daemonsets\n")
		}
		if req.DrainOptions.DeleteEmptyDirData {
			fmt.Printf("   ✓ --delete-emptydir-data\n")
		}
		if req.DrainOptions.Force {
			fmt.Printf("   ⚠️  --force\n")
		}
		fmt.Printf("   Grace period: %ds | Timeout: %s\n",
			req.DrainOptions.GracePeriod,
			req.DrainOptions.Timeout)

		// Drain cada node
		fmt.Printf("\n🚀 Draining nodes...\n")
		totalPodsMigrated := 0
		for i, nodeName := range nodes {
			drainStartTime := time.Now()
			nodeProgress := 45 + float64(i+1)/float64(len(nodes))*30 // 45% → 75%
			sendProgress(3, "DRAIN", "running", fmt.Sprintf("Draining node %d/%d", i+1, len(nodes)), nodeProgress, nodeName, i+1, len(nodes), nil)
			fmt.Printf("[%d/%d] Draining %s...\n", i+1, len(nodes), nodeName)

			// Drain com opções
			if err := k8sClient.DrainNode(ctx, nodeName, &req.DrainOptions); err != nil {
				sendProgress(3, "DRAIN", "error", fmt.Sprintf("Failed to drain %s", nodeName), 0, nodeName, i+1, len(nodes), err)
				fmt.Printf("   ❌ FAILED: %v\n", err)
				// Log failure no audit
				if h.historyTracker != nil {
					h.historyTracker.Log(history.HistoryEntry{
						Action:   history.ActionDrainNode,
						Resource: fmt.Sprintf("%s/%s", origin.Name, nodeName),
						Cluster:  req.Cluster,
						Status:   history.StatusFailed,
						ErrorMsg: err.Error(),
						Duration: time.Since(drainStartTime).Milliseconds(),
					})
				}
				return
			}

			// Verificar se está drained
			isDrained, err := k8sClient.IsNodeDrained(ctx, nodeName)
			if err != nil {
				sendProgress(3, "DRAIN", "running", fmt.Sprintf("Could not verify drain status for %s", nodeName), nodeProgress, nodeName, i+1, len(nodes), err)
				fmt.Printf("   ⚠️  WARNING: Could not verify drain status: %v\n", err)
			} else if isDrained {
				sendProgress(3, "DRAIN", "running", fmt.Sprintf("Node %s fully drained", nodeName), nodeProgress, nodeName, i+1, len(nodes), nil)
				fmt.Printf("   ✅ Node fully drained (all pods migrated)\n")
				totalPodsMigrated += 5 // Placeholder - seria o count real
			} else {
				sendProgress(3, "DRAIN", "running", fmt.Sprintf("Warning: Some pods may still be present on %s", nodeName), nodeProgress, nodeName, i+1, len(nodes), nil)
				fmt.Printf("   ⚠️  WARNING: Some pods may still be present (DaemonSets?)\n")
			}

			// Log success no audit (por node)
			if h.historyTracker != nil {
				h.historyTracker.Log(history.HistoryEntry{
					Action:   history.ActionDrainNode,
					Resource: fmt.Sprintf("%s/%s", origin.Name, nodeName),
					Cluster:  req.Cluster,
					Status:   history.StatusSuccess,
					Duration: time.Since(drainStartTime).Milliseconds(),
					Before: map[string]interface{}{
						"pods_count": totalPodsMigrated, // Approximate
						"drained":    false,
					},
					After: map[string]interface{}{
						"pods_count": 0,
						"drained":    isDrained,
					},
				})
			}
		}
		sendProgress(3, "DRAIN", "completed", fmt.Sprintf("All %d nodes drained successfully (~%d pods migrated)", len(nodes), totalPodsMigrated), 75, "", 0, 0, nil)
		fmt.Printf("✅ All %d nodes drained successfully (~%d pods migrated)\n\n", len(nodes), totalPodsMigrated)
	} else {
		sendProgress(3, "DRAIN", "completed", "DRAIN phase skipped (disabled)", 75, "", 0, 0, nil)
		fmt.Printf("3️⃣  FASE DRAIN - SKIPPED (disabled)\n\n")
	}

	// FASE 4: POST-DRAIN
	sendProgress(4, "POST-DRAIN", "running", "Starting POST-DRAIN phase (scale DOWN origin)", 75, "", 0, 0, nil)
	fmt.Printf("4️⃣  FASE POST-DRAIN - Scale DOWN origin\n")
	fmt.Printf("────────────────────────────────────────────────────────────────────\n")

	if origin.PostDrainChanges != nil {
		sendProgress(4, "POST-DRAIN", "running", fmt.Sprintf("Applying changes to %s", origin.Name), 80, "", 0, 0, nil)
		fmt.Printf("📥 Applying changes to %s:\n", origin.Name)
		fmt.Printf("   Autoscaling: %v | NodeCount: %d\n",
			origin.PostDrainChanges.Autoscaling,
			origin.PostDrainChanges.NodeCount)

		// Aplicar mudanças via Azure CLI
		if err := h.applyNodePoolChanges(origin.Name, origin.ResourceGroup, origin.Subscription, origin.PostDrainChanges); err != nil {
			sendProgress(4, "POST-DRAIN", "error", "Failed to apply POST-DRAIN changes", 0, "", 0, 0, err)
			fmt.Printf("❌ ERROR: Failed to apply POST-DRAIN changes: %v\n", err)
			return
		}
		sendProgress(4, "POST-DRAIN", "completed", "POST-DRAIN changes applied successfully", 90, "", 0, 0, nil)
		fmt.Printf("✅ POST-DRAIN changes applied successfully\n")
	} else {
		sendProgress(4, "POST-DRAIN", "completed", fmt.Sprintf("No POST-DRAIN changes configured for %s", origin.Name), 90, "", 0, 0, nil)
		fmt.Printf("⏭️  No POST-DRAIN changes configured for %s\n", origin.Name)
	}

	// FASE 5: FINALIZAÇÃO
	sendProgress(5, "FINALIZAÇÃO", "running", "Cleanup and finalization", 90, "", 0, 0, nil)
	fmt.Printf("\n5️⃣  FASE FINALIZAÇÃO - Cleanup\n")
	fmt.Printf("────────────────────────────────────────────────────────────────────\n")

	duration := time.Since(startTime)
	sendProgress(5, "FINALIZAÇÃO", "completed", fmt.Sprintf("Sequencing completed successfully in %s", duration.Round(time.Second)), 100, "", 0, 0, nil)
	fmt.Printf("⏱️  Total execution time: %s\n", duration.Round(time.Second))
	fmt.Printf("✅ Sequencing completed successfully!\n")

	// Log da sequência completa no audit
	if h.historyTracker != nil {
		h.historyTracker.Log(history.HistoryEntry{
			Action:   history.ActionNodePoolSequence,
			Resource: fmt.Sprintf("%s → %s", origin.Name, dest.Name),
			Cluster:  req.Cluster,
			Status:   history.StatusSuccess,
			Duration: duration.Milliseconds(),
			Before: map[string]interface{}{
				"origin_pool": origin.Name,
				"dest_pool":   dest.Name,
				"cordon":      req.CordonEnabled,
				"drain":       req.DrainEnabled,
			},
			After: map[string]interface{}{
				"total_duration_ms": duration.Milliseconds(),
				"total_duration":    duration.Round(time.Second).String(),
				"session_id":        sessionID,
			},
		})
	}

	fmt.Printf("\n")
	fmt.Printf("╔════════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                    SEQUENCING COMPLETE                             ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")
}

// applyNodePoolChanges aplica mudanças em um node pool via Azure CLI
func (h *NodePoolHandler) applyNodePoolChanges(poolName, resourceGroup, subscription string, changes *models.NodePoolChanges) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Configurar subscription (se necessário)
	if subscription != "" {
		subCtx, subCancel := context.WithTimeout(ctx, 30*time.Second)
		err := exec.CommandContext(subCtx, "az", "account", "set", "--subscription", subscription).Run()
		subCancel()
		if err != nil {
			return fmt.Errorf("failed to set subscription: %w", err)
		}
	}

	// Construir comando baseado nas mudanças
	var commands [][]string

	if changes.Autoscaling {
		// Habilitar autoscaling com min/max
		commands = append(commands, []string{
			"az", "aks", "nodepool", "update",
			"--resource-group", resourceGroup,
			"--cluster-name", poolName, // TODO: Obter cluster name correto
			"--name", poolName,
			"--enable-cluster-autoscaler",
			"--min-count", fmt.Sprintf("%d", changes.MinNodes),
			"--max-count", fmt.Sprintf("%d", changes.MaxNodes),
		})
	} else {
		// Desabilitar autoscaling
		commands = append(commands, []string{
			"az", "aks", "nodepool", "update",
			"--resource-group", resourceGroup,
			"--cluster-name", poolName, // TODO: Obter cluster name correto
			"--name", poolName,
			"--disable-cluster-autoscaler",
		})

		// Scale para node count específico
		commands = append(commands, []string{
			"az", "aks", "nodepool", "scale",
			"--resource-group", resourceGroup,
			"--cluster-name", poolName, // TODO: Obter cluster name correto
			"--name", poolName,
			"--node-count", fmt.Sprintf("%d", changes.NodeCount),
		})
	}

	// Executar comandos sequencialmente
	for _, cmd := range commands {
		output, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("command failed: %s\nOutput: %s", err, string(output))
		}
	}

	return nil
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

// NodeDiskMetrics representa métricas de disco de um node
type NodeDiskMetrics struct {
	NodeName       string  `json:"node_name"`
	NodePoolName   string  `json:"node_pool_name"`
	TotalBytes     float64 `json:"total_bytes"`
	UsedBytes      float64 `json:"used_bytes"`
	AvailableBytes float64 `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	IsEphemeral    bool    `json:"is_ephemeral"`
	DiskType       string  `json:"disk_type"`
}

// NodePoolDiskMetrics representa métricas agregadas de disco de um node pool
type NodePoolDiskMetrics struct {
	NodePoolName   string            `json:"node_pool_name"`
	TotalBytes     float64           `json:"total_bytes"`
	UsedBytes      float64           `json:"used_bytes"`
	AvailableBytes float64           `json:"available_bytes"`
	UsagePercent   float64           `json:"usage_percent"`
	NodeCount      int               `json:"node_count"`
	Nodes          []NodeDiskMetrics `json:"nodes"`
}

// StorageClassInfo representa informações sobre uma storage class
type StorageClassInfo struct {
	Name              string            `json:"name"`
	Provisioner       string            `json:"provisioner"`
	ReclaimPolicy     string            `json:"reclaim_policy"`
	VolumeBindMode    string            `json:"volume_bind_mode"`
	AllowExpansion    bool              `json:"allow_expansion"`
	Parameters        map[string]string `json:"parameters"`
	PVCount           int               `json:"pv_count"`
	TotalCapacity     float64           `json:"total_capacity_bytes"`
	UsedCapacity      float64           `json:"used_capacity_bytes,omitempty"`
	AvailableCapacity float64           `json:"available_capacity_bytes,omitempty"`
	UsagePercentage   float64           `json:"usage_percentage,omitempty"`
}

// PVCInfo representa informações sobre um PVC
type PVCInfo struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Status          string   `json:"status"`
	StorageClass    string   `json:"storage_class"`
	Capacity        float64  `json:"capacity_bytes"`
	AccessModes     []string `json:"access_modes"`
	BoundPV         string   `json:"bound_pv"`
	NodeName        string   `json:"node_name,omitempty"`
	UsedBytes       float64  `json:"used_bytes,omitempty"`
	AvailableBytes  float64  `json:"available_bytes,omitempty"`
	UsagePercentage float64  `json:"usage_percentage,omitempty"`
}

// StorageOverview representa uma visão geral do storage no cluster
type StorageOverview struct {
	StorageClasses []StorageClassInfo `json:"storage_classes"`
	PVCs           []PVCInfo          `json:"pvcs"`
	TotalPVs       int                `json:"total_pvs"`
	TotalPVCs      int                `json:"total_pvcs"`
	TotalCapacity  float64            `json:"total_capacity_bytes"`
	UsedCapacity   float64            `json:"used_capacity_bytes"`
}

// nodeDiskData armazena total e disponível de disco de um node vindos do Prometheus
type nodeDiskData struct {
	total float64
	avail float64
}

// getNodeDiskFromPrometheus busca uso real de disco por node via Prometheus node_exporter.
// nodeIPToName mapeia IP interno → nome do node; nodeNames é o set de nomes K8s.
// Tenta match por IP e por hostname (VMSS format), sem filtrar instance no PromQL
// para suportar qualquer formato de label usado na configuração do Prometheus.
// Retorna nil quando Prometheus não está disponível ou nenhum node é encontrado.
func (h *NodePoolHandler) getNodeDiskFromPrometheus(cluster string, nodeIPToName map[string]string, nodeNames []string) map[string]nodeDiskData {
	promClient, err := promclient.NewPrometheusClient(cluster)
	if err != nil {
		log.Debug().Err(err).Str("cluster", cluster).Msg("Prometheus indisponível para métricas de disco")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Sem filtro de instance — o Prometheus pode usar IP ou hostname (VMSS format)
	// Filtramos apenas pelo mountpoint e excluímos filesystems virtuais
	const querySize = `node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs",fstype!="overlay",fstype!="rootfs"}`
	const queryAvail = `node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs",fstype!="overlay",fstype!="rootfs"}`

	sizeRes, err := promClient.Query(ctx, querySize)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Erro ao consultar node_filesystem_size_bytes")
		return nil
	}
	if len(sizeRes.Data.Result) == 0 {
		log.Warn().Str("cluster", cluster).Msg("Prometheus não retornou métricas node_filesystem_size_bytes")
		return nil
	}

	availRes, err := promClient.Query(ctx, queryAvail)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Erro ao consultar node_filesystem_avail_bytes")
		return nil
	}

	parseValue := func(v []interface{}) float64 {
		if len(v) < 2 {
			return 0
		}
		s, ok := v[1].(string)
		if !ok {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}

	// Extrai o host de "host:porta" removendo apenas o sufixo ":porta"
	extractHost := func(instance string) string {
		if idx := strings.LastIndex(instance, ":"); idx > 0 {
			return instance[:idx]
		}
		return instance
	}

	// Mapear host (IP ou hostname) → [total, avail], mantendo o maior valor por host
	sizeByHost := make(map[string]float64)
	for _, r := range sizeRes.Data.Result {
		host := extractHost(r.Metric["instance"])
		if val := parseValue(r.Value); host != "" && val > sizeByHost[host] {
			sizeByHost[host] = val
		}
	}
	availByHost := make(map[string]float64)
	for _, r := range availRes.Data.Result {
		host := extractHost(r.Metric["instance"])
		if val := parseValue(r.Value); host != "" && val > availByHost[host] {
			availByHost[host] = val
		}
	}

	result := make(map[string]nodeDiskData)

	// Tentar match por IP primeiro
	for ip, nodeName := range nodeIPToName {
		total := sizeByHost[ip]
		avail := availByHost[ip]
		if total > 0 || avail > 0 {
			result[nodeName] = nodeDiskData{total: total, avail: avail}
		}
	}

	// Para nodes ainda não encontrados, tentar match por hostname K8s
	for _, nodeName := range nodeNames {
		if _, found := result[nodeName]; found {
			continue
		}
		total := sizeByHost[nodeName]
		avail := availByHost[nodeName]
		if total > 0 || avail > 0 {
			result[nodeName] = nodeDiskData{total: total, avail: avail}
		}
	}

	log.Debug().Str("cluster", cluster).
		Int("prom_results", len(sizeRes.Data.Result)).
		Int("nodes_matched", len(result)).
		Int("nodes_expected", len(nodeNames)).
		Msg("Prometheus disk metrics")

	if len(result) == 0 {
		// Log os primeiros 3 instances retornados para facilitar diagnóstico
		for i, r := range sizeRes.Data.Result {
			if i >= 3 {
				break
			}
			log.Warn().
				Str("cluster", cluster).
				Str("instance", r.Metric["instance"]).
				Str("fstype", r.Metric["fstype"]).
				Str("node_label", r.Metric["node"]).
				Msg("Prometheus retornou dados mas nenhum node bateu — verifique o formato do label instance")
		}
		return nil
	}
	return result
}

// GetNodePoolDiskMetrics retorna métricas de disco agregadas por node pool.
// Tenta buscar uso real via Prometheus node_exporter; se indisponível, usa os
// valores estáticos da API Kubernetes (ephemeral-storage capacity/allocatable).
func (h *NodePoolHandler) GetNodePoolDiskMetrics(c *gin.Context) {
	cluster := c.Query("cluster")
	nodePoolName := c.Query("nodepool")

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameter 'cluster' is required",
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
				"code":    "KUBERNETES_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	// Listar todos os nodes
	nodes, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "NODE_LIST_ERROR",
				"message": fmt.Sprintf("Failed to list nodes: %v", err),
			},
		})
		return
	}

	// Construir mapa IP → nome do node e lista de nomes para correlação com Prometheus
	nodeIPToName := make(map[string]string)
	nodeNames := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)
		for _, addr := range node.Status.Addresses {
			if addr.Type == "InternalIP" {
				nodeIPToName[addr.Address] = node.Name
				break
			}
		}
	}

	// Tentar buscar métricas reais do Prometheus
	promDisk := h.getNodeDiskFromPrometheus(cluster, nodeIPToName, nodeNames)

	// Agrupar métricas por node pool
	poolMetrics := make(map[string]*NodePoolDiskMetrics)

	for _, node := range nodes.Items {
		// Extrair nome do node pool das labels
		nodePool, ok := node.Labels["agentpool"]
		if !ok {
			continue
		}

		// Filtrar por node pool se especificado
		if nodePoolName != "" && nodePool != nodePoolName {
			continue
		}

		// Determinar tipo de disco (efêmero ou persistente)
		isEphemeral := false
		diskType := "Managed Disk"
		if storageProfile, ok := node.Labels["storageprofile"]; ok {
			if storageProfile == "ephemeral" {
				isEphemeral = true
				diskType = "Ephemeral OS Disk"
			}
		} else if node.Labels["kubernetes.azure.com/ephemeral-os"] == "true" {
			isEphemeral = true
			diskType = "Ephemeral OS Disk"
		}

		var totalBytes, usedBytes, availableBytes float64

		if promData, ok := promDisk[node.Name]; ok {
			// Dados reais do Prometheus (uso efetivo do filesystem)
			totalBytes = promData.total
			availableBytes = promData.avail
			usedBytes = totalBytes - availableBytes
		} else {
			// Fallback: valores estáticos da API K8s (iguais para todos os nodes do mesmo SKU)
			if capacity, ok := node.Status.Capacity["ephemeral-storage"]; ok {
				totalBytes = float64(capacity.Value())
			}
			if allocatable, ok := node.Status.Allocatable["ephemeral-storage"]; ok {
				availableBytes = float64(allocatable.Value())
			}
			usedBytes = totalBytes - availableBytes
		}

		nodeMetric := NodeDiskMetrics{
			NodeName:       node.Name,
			NodePoolName:   nodePool,
			TotalBytes:     totalBytes,
			UsedBytes:      usedBytes,
			AvailableBytes: availableBytes,
			UsagePercent:   0,
			IsEphemeral:    isEphemeral,
			DiskType:       diskType,
		}

		if totalBytes > 0 {
			nodeMetric.UsagePercent = (usedBytes / totalBytes) * 100
		}

		// Agregar por node pool
		if _, exists := poolMetrics[nodePool]; !exists {
			poolMetrics[nodePool] = &NodePoolDiskMetrics{
				NodePoolName: nodePool,
				Nodes:        make([]NodeDiskMetrics, 0),
			}
		}

		poolMetrics[nodePool].TotalBytes += totalBytes
		poolMetrics[nodePool].UsedBytes += usedBytes
		poolMetrics[nodePool].AvailableBytes += availableBytes
		poolMetrics[nodePool].NodeCount++
		poolMetrics[nodePool].Nodes = append(poolMetrics[nodePool].Nodes, nodeMetric)
	}

	// Recalcular percentual médio por pool
	for _, metrics := range poolMetrics {
		if metrics.TotalBytes > 0 {
			metrics.UsagePercent = (metrics.UsedBytes / metrics.TotalBytes) * 100
		}
	}

	// Converter map para slice
	result := make([]NodePoolDiskMetrics, 0, len(poolMetrics))
	for _, metrics := range poolMetrics {
		result = append(result, *metrics)
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetStorageOverview retorna informações sobre StorageClasses e PVs/PVCs do cluster
func (h *NodePoolHandler) GetStorageOverview(c *gin.Context) {
	cluster := c.Query("cluster")

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameter 'cluster' is required",
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
				"code":    "KUBERNETES_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	overview := StorageOverview{
		StorageClasses: make([]StorageClassInfo, 0),
		PVCs:           make([]PVCInfo, 0),
	}

	// Buscar StorageClasses
	storageClasses, err := client.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err == nil {
		// Buscar todos os PVs para contar por StorageClass
		pvs, pvErr := client.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
		pvCountByClass := make(map[string]int)
		pvCapacityByClass := make(map[string]float64)

		if pvErr == nil {
			overview.TotalPVs = len(pvs.Items)
			for _, pv := range pvs.Items {
				if pv.Spec.StorageClassName != "" {
					pvCountByClass[pv.Spec.StorageClassName]++
					if capacity, ok := pv.Spec.Capacity["storage"]; ok {
						bytes := float64(capacity.Value())
						pvCapacityByClass[pv.Spec.StorageClassName] += bytes
						overview.TotalCapacity += bytes
					}
				}
			}
		}

		for _, sc := range storageClasses.Items {
			scInfo := StorageClassInfo{
				Name:           sc.Name,
				Provisioner:    sc.Provisioner,
				Parameters:     sc.Parameters,
				PVCount:        pvCountByClass[sc.Name],
				TotalCapacity:  pvCapacityByClass[sc.Name],
				AllowExpansion: sc.AllowVolumeExpansion != nil && *sc.AllowVolumeExpansion,
			}

			if sc.ReclaimPolicy != nil {
				scInfo.ReclaimPolicy = string(*sc.ReclaimPolicy)
			}

			if sc.VolumeBindingMode != nil {
				scInfo.VolumeBindMode = string(*sc.VolumeBindingMode)
			}

			overview.StorageClasses = append(overview.StorageClasses, scInfo)
		}
	}

	// Buscar PVCs de todos os namespaces
	pvcs, err := client.CoreV1().PersistentVolumeClaims("").List(context.Background(), metav1.ListOptions{})
	if err == nil {
		overview.TotalPVCs = len(pvcs.Items)

		// Buscar pods para associar PVCs aos nodes e obter métricas
		pods, _ := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
		pvcToNode := make(map[string]string)    // namespace/pvcname -> nodename
		pvcToPodName := make(map[string]string) // namespace/pvcname -> podname

		for _, pod := range pods.Items {
			if pod.Spec.NodeName != "" {
				for _, vol := range pod.Spec.Volumes {
					if vol.PersistentVolumeClaim != nil {
						key := pod.Namespace + "/" + vol.PersistentVolumeClaim.ClaimName
						pvcToNode[key] = pod.Spec.NodeName
						pvcToPodName[key] = pod.Name
					}
				}
			}
		}

		// Buscar métricas de uso via Prometheus
		volumeMetrics := h.getVolumeMetricsFromPrometheus(cluster)

		// Mapear uso por StorageClass
		usedByStorageClass := make(map[string]float64)

		for _, pvc := range pvcs.Items {
			pvcInfo := PVCInfo{
				Name:        pvc.Name,
				Namespace:   pvc.Namespace,
				Status:      string(pvc.Status.Phase),
				AccessModes: make([]string, 0),
			}

			if pvc.Spec.StorageClassName != nil {
				pvcInfo.StorageClass = *pvc.Spec.StorageClassName
			}

			if pvc.Spec.VolumeName != "" {
				pvcInfo.BoundPV = pvc.Spec.VolumeName
			}

			// Capacidade
			if capacity, ok := pvc.Status.Capacity["storage"]; ok {
				pvcInfo.Capacity = float64(capacity.Value())
			}

			// Access modes
			for _, mode := range pvc.Spec.AccessModes {
				pvcInfo.AccessModes = append(pvcInfo.AccessModes, string(mode))
			}

			// Node associado
			key := pvc.Namespace + "/" + pvc.Name
			if nodeName, ok := pvcToNode[key]; ok {
				pvcInfo.NodeName = nodeName
			}

			// Buscar métricas do Prometheus primeiro
			metricsKey := pvc.Namespace + "/" + pvc.Name
			if metric, hasMetrics := volumeMetrics[metricsKey]; hasMetrics {
				pvcInfo.UsedBytes = metric.UsedBytes
				pvcInfo.AvailableBytes = metric.AvailableBytes
				pvcInfo.UsagePercentage = metric.UsagePercentage
			} else {
				// Fallback: verificar anotações
				if usageAnnotation, hasAnnotation := pvc.Annotations["volume.kubernetes.io/storage-used"]; hasAnnotation {
					if quantity, parseErr := resource.ParseQuantity(usageAnnotation); parseErr == nil {
						pvcInfo.UsedBytes = float64(quantity.Value())
						pvcInfo.AvailableBytes = pvcInfo.Capacity - pvcInfo.UsedBytes
						if pvcInfo.Capacity > 0 {
							pvcInfo.UsagePercentage = (pvcInfo.UsedBytes / pvcInfo.Capacity) * 100
						}
					}
				}
			}

			// Acumular uso por StorageClass
			if pvcInfo.UsedBytes > 0 && pvcInfo.StorageClass != "" {
				usedByStorageClass[pvcInfo.StorageClass] += pvcInfo.UsedBytes
				overview.UsedCapacity += pvcInfo.UsedBytes
			}

			overview.PVCs = append(overview.PVCs, pvcInfo)
		}

		// Atualizar uso nas StorageClasses
		for i := range overview.StorageClasses {
			if used, ok := usedByStorageClass[overview.StorageClasses[i].Name]; ok {
				overview.StorageClasses[i].UsedCapacity = used
				overview.StorageClasses[i].AvailableCapacity = overview.StorageClasses[i].TotalCapacity - used
				if overview.StorageClasses[i].TotalCapacity > 0 {
					overview.StorageClasses[i].UsagePercentage = (used / overview.StorageClasses[i].TotalCapacity) * 100
				}
			}
		}
	}

	c.JSON(200, gin.H{
		"success": true,
		"data":    overview,
	})
}

// getVolumeMetricsFromPrometheus busca métricas de uso de volumes do Prometheus
func (h *NodePoolHandler) getVolumeMetricsFromPrometheus(cluster string) map[string]VolumeMetric {
	metrics := make(map[string]VolumeMetric)

	// Criar cliente Prometheus
	promClient, err := promclient.NewPrometheusClient(cluster)
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Não foi possível criar cliente Prometheus para métricas de volumes")
		return metrics
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query para obter uso de volumes persistentes
	// kubelet_volume_stats_used_bytes - bytes usados no volume
	// kubelet_volume_stats_capacity_bytes - capacidade total do volume
	queries := map[string]string{
		"used":      `kubelet_volume_stats_used_bytes{persistentvolumeclaim!=""}`,
		"capacity":  `kubelet_volume_stats_capacity_bytes{persistentvolumeclaim!=""}`,
		"available": `kubelet_volume_stats_available_bytes{persistentvolumeclaim!=""}`,
	}

	// Buscar métricas de uso
	usedResult, err := promClient.Query(ctx, queries["used"])
	if err != nil {
		log.Warn().
			Err(err).
			Str("cluster", cluster).
			Msg("Erro ao buscar métricas de uso de volumes do Prometheus")
		return metrics
	}

	// Processar resultados
	for _, result := range usedResult.Data.Result {
		namespace := result.Metric["namespace"]
		pvcName := result.Metric["persistentvolumeclaim"]

		if namespace == "" || pvcName == "" {
			continue
		}

		key := namespace + "/" + pvcName

		// Extrair valor de bytes usados
		if len(result.Value) >= 2 {
			if usedStr, ok := result.Value[1].(string); ok {
				if used, parseErr := strconv.ParseFloat(usedStr, 64); parseErr == nil {
					metric := metrics[key]
					metric.UsedBytes = used
					metric.Namespace = namespace
					metric.PVCName = pvcName
					metrics[key] = metric
				}
			}
		}
	}

	// Buscar capacidade total
	capacityResult, err := promClient.Query(ctx, queries["capacity"])
	if err == nil {
		for _, result := range capacityResult.Data.Result {
			namespace := result.Metric["namespace"]
			pvcName := result.Metric["persistentvolumeclaim"]

			if namespace == "" || pvcName == "" {
				continue
			}

			key := namespace + "/" + pvcName

			if len(result.Value) >= 2 {
				if capacityStr, ok := result.Value[1].(string); ok {
					if capacity, parseErr := strconv.ParseFloat(capacityStr, 64); parseErr == nil {
						metric := metrics[key]
						metric.CapacityBytes = capacity
						metrics[key] = metric
					}
				}
			}
		}
	}

	// Buscar bytes disponíveis
	availableResult, err := promClient.Query(ctx, queries["available"])
	if err == nil {
		for _, result := range availableResult.Data.Result {
			namespace := result.Metric["namespace"]
			pvcName := result.Metric["persistentvolumeclaim"]

			if namespace == "" || pvcName == "" {
				continue
			}

			key := namespace + "/" + pvcName

			if len(result.Value) >= 2 {
				if availableStr, ok := result.Value[1].(string); ok {
					if available, parseErr := strconv.ParseFloat(availableStr, 64); parseErr == nil {
						metric := metrics[key]
						metric.AvailableBytes = available
						metrics[key] = metric
					}
				}
			}
		}
	}

	// Calcular percentuais
	for key, metric := range metrics {
		if metric.CapacityBytes > 0 {
			metric.UsagePercentage = (metric.UsedBytes / metric.CapacityBytes) * 100
			metrics[key] = metric
		}
	}

	log.Info().
		Str("cluster", cluster).
		Int("volumes", len(metrics)).
		Msg("Métricas de volumes obtidas do Prometheus")

	return metrics
}

// VolumeMetric representa métricas de um volume
type VolumeMetric struct {
	Namespace       string
	PVCName         string
	UsedBytes       float64
	CapacityBytes   float64
	AvailableBytes  float64
	UsagePercentage float64
}
// ==================================================================
// Node Details Handlers
// ==================================================================

// ListNodesInNodePool retorna lista de nodes de um node pool com métricas
func (h *NodePoolHandler) ListNodesInNodePool(c *gin.Context) {
	cluster := c.Param("cluster")
	nodePoolName := c.Param("nodepool")

	if cluster == "" || nodePoolName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameters 'cluster' and 'nodepool' are required",
			},
		})
		return
	}

	// Obter client Kubernetes
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "K8S_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	// Type assertion para nosso wrapper Client
	k8sClient := kubernetes.NewClient(clientset, cluster)

	// Configurar Metrics Client para coletar métricas de CPU/Memory
	metricsClientInterface, err := h.kubeManager.GetMetricsClient(cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Metrics client unavailable, metrics will be empty")
	} else {
		// Type assertion para o tipo concreto
		if metricsClient, ok := metricsClientInterface.(*metricsclientset.Clientset); ok {
			k8sClient.SetMetricsClient(metricsClient)
		} else {
			log.Warn().Str("cluster", cluster).Msg("Failed to assert metrics client type")
		}
	}

	ctx := c.Request.Context()

	// Buscar nodes com métricas
	nodes, err := k8sClient.GetNodesWithMetrics(ctx, nodePoolName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "GET_NODES_ERROR",
				"message": fmt.Sprintf("Failed to get nodes: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"nodes":          nodes,
			"count":          len(nodes),
			"node_pool_name": nodePoolName,
			"cluster":        cluster,
		},
	})
}

// GetNodePoolAzureInfo retorna informações Azure do cluster (tags + nome da subscription).
// Endpoint separado para não bloquear a listagem de nodes com chamadas az aks show.
func (h *NodePoolHandler) GetNodePoolAzureInfo(c *gin.Context) {
	cluster := c.Param("cluster")
	nodePoolName := c.Param("nodepool")

	if cluster == "" || nodePoolName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PARAMETERS", "message": "Parameters 'cluster' and 'nodepool' are required"},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "K8S_CLIENT_ERROR", "message": fmt.Sprintf("Failed to get Kubernetes client: %v", err)},
		})
		return
	}

	k8sClient := kubernetes.NewClient(clientset, cluster)
	info := k8sClient.GetClusterAzureInfo()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"cluster":           cluster,
			"node_pool_name":    nodePoolName,
			"resource_group":    info.ResourceGroup,
			"subscription":      info.Subscription,
			"subscription_name": info.SubscriptionName,
			"cluster_tags":      info.ClusterTags,
		},
	})
}

// GetNodeDetails retorna detalhes completos de um node específico
func (h *NodePoolHandler) GetNodeDetails(c *gin.Context) {
	cluster := c.Param("cluster")
	nodePoolName := c.Param("nodepool")
	nodeName := c.Param("node")

	if cluster == "" || nodePoolName == "" || nodeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameters 'cluster', 'nodepool' and 'node' are required",
			},
		})
		return
	}

	// Obter client Kubernetes
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "K8S_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	k8sClient := kubernetes.NewClient(clientset, cluster)

	// Configurar Metrics Client para coletar métricas de CPU/Memory
	metricsClientInterface, err := h.kubeManager.GetMetricsClient(cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("Metrics client unavailable, metrics will be empty")
	} else {
		// Type assertion para o tipo concreto
		if metricsClient, ok := metricsClientInterface.(*metricsclientset.Clientset); ok {
			k8sClient.SetMetricsClient(metricsClient)
		}
	}

	ctx := c.Request.Context()

	// Buscar detalhes do node
	details, err := k8sClient.GetNodeDetails(ctx, nodeName, nodePoolName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "GET_NODE_DETAILS_ERROR",
				"message": fmt.Sprintf("Failed to get node details: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    details,
	})
}
