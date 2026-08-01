package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePoolRegistryHandler gerencia o catálogo de node pools para correlação Dynatrace
type NodePoolRegistryHandler struct {
	kubeManager *config.KubeConfigManager
	store       *storage.NodePoolRegistryStore
}

// NewNodePoolRegistryHandler cria o handler.
func NewNodePoolRegistryHandler(km *config.KubeConfigManager, store *storage.NodePoolRegistryStore) *NodePoolRegistryHandler {
	return &NodePoolRegistryHandler{kubeManager: km, store: store}
}

// List retorna todos os node pools do registry (GET /api/v1/nodepools/registry?cluster=X)
func (h *NodePoolRegistryHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	entries, err := h.store.GetAll(cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("falha ao consultar registry: %v", err)})
		return
	}
	c.JSON(200, gin.H{"entries": entries, "total": len(entries)})
}

// Scan varre todos os nodes de um cluster e atualiza o registry
// POST /api/v1/nodepools/registry/scan?cluster=X
// Se cluster="" varre todos os clusters conhecidos
func (h *NodePoolRegistryHandler) Scan(c *gin.Context) {
	clusterFilter := c.Query("cluster")

	clusters := h.kubeManager.ListContexts()
	if len(clusters) == 0 {
		c.JSON(500, gin.H{"error": "nenhum cluster configurado no kubeconfig"})
		return
	}

	type scanResult struct {
		Cluster  string `json:"cluster"`
		Scanned  int    `json:"scanned"`
		Inserted int    `json:"inserted"`
		Error    string `json:"error,omitempty"`
	}

	var results []scanResult
	totalInserted := 0

	for _, clusterName := range clusters {
		if clusterFilter != "" && clusterName != clusterFilter {
			continue
		}

		res := scanResult{Cluster: clusterName}

		client, err := h.kubeManager.GetClient(clusterName)
		if err != nil {
			res.Error = fmt.Sprintf("falha ao obter client: %v", err)
			log.Warn().Str("cluster", clusterName).Err(err).Msg("NodePoolRegistry: falha ao obter client")
			results = append(results, res)
			continue
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		cancel()
		if err != nil {
			res.Error = fmt.Sprintf("falha ao listar nodes: %v", err)
			log.Warn().Str("cluster", clusterName).Err(err).Msg("NodePoolRegistry: falha ao listar nodes")
			results = append(results, res)
			continue
		}

		res.Scanned = len(nodeList.Items)

		// Agregar por node pool
		type poolInfo struct {
			count      int
			vmSize     string
			osSku      string
			mode       string
			diskSizeGB int32
			diskType   string
		}
		pools := map[string]*poolInfo{}

		for _, node := range nodeList.Items {
			labels := node.Labels
			// nodePoolLabel (nodepools_snat.go) já cobre AKS/EKS/GKE — antes daqui só existia o
			// label AKS + fallback de nome de nó AKS, então nós GKE/EKS (sem
			// kubernetes.azure.com/agentpool) nunca eram reconhecidos e eram descartados em
			// silêncio (nenhum erro, só node_count=0 pools=0 pro cluster inteiro).
			poolName := nodePoolLabel(labels)
			if poolName == "default" {
				// nodePoolLabel não reconheceu nenhum label conhecido — tenta o fallback
				// histórico específico de AKS (nome do nó no padrão aks-<pool>-<digits>-vmss*)
				// antes de aceitar "default" como nome genérico, preservando o comportamento
				// anterior pra esse caso.
				if fromName := extractNodePoolFromName(node.Name); fromName != "" {
					poolName = fromName
				}
			}

			if _, ok := pools[poolName]; !ok {
				pools[poolName] = &poolInfo{
					vmSize: labels["node.kubernetes.io/instance-type"],
					osSku:  labels["kubernetes.azure.com/os-sku"],
					mode:   labels["kubernetes.azure.com/mode"],
				}
			}
			pools[poolName].count++
		}

		// GKE não expõe o tamanho/tipo do disco de boot via label de node nenhum (diferente da
		// Azure, que tem kubernetes.azure.com/os-disk-size-gb) — só a Container API do GKE sabe
		// disso (NodePool.config.diskSizeGb/diskType). Enriquece via NodeGroupProvider, mesmo
		// caminho de auth já usado por Node Pools/SNAT/predictions, sem custo extra pra AKS/EKS
		// (só chamado quando o context é GKE).
		if strings.HasPrefix(clusterName, "gke_") {
			if provider := h.kubeManager.GetNodeGroupProvider(clusterName); provider != nil {
				// ctx acima já foi cancelado logo após o List de nodes (cancel() na linha de cima)
				// — precisa de um context novo pra esta chamada.
				diskCtx, diskCancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
				gkePools, gkeErr := provider.ListNodeGroups(diskCtx, clusterName)
				diskCancel()
				if gkeErr != nil {
					log.Warn().Str("cluster", clusterName).Err(gkeErr).Msg("NodePoolRegistry: falha ao buscar disco via Container API (GKE)")
				}
				for _, gp := range gkePools {
					if info, ok := pools[gp.Name]; ok {
						info.diskSizeGB = gp.DiskSizeGB
						info.diskType = gp.DiskType
					}
				}
			}
		}

		// Limpar entries antigas do cluster antes de inserir novas
		if len(pools) > 0 {
			if err := h.store.DeleteByCluster(clusterName); err != nil {
				log.Warn().Str("cluster", clusterName).Err(err).Msg("NodePoolRegistry: falha ao deletar entries antigas")
			}
		}

		now := time.Now().UTC()
		for poolName, info := range pools {
			entry := storage.NodePoolRegistryEntry{
				Cluster:     clusterName,
				NodePool:    poolName,
				NodeCount:   info.count,
				VMSize:      info.vmSize,
				OSSku:       info.osSku,
				Mode:        strings.Title(strings.ToLower(info.mode)),
				DiskSizeGB:  int(info.diskSizeGB),
				DiskType:    info.diskType,
				LastScanned: now,
			}
			if err := h.store.Upsert(entry); err != nil {
				log.Warn().Str("cluster", clusterName).Str("pool", poolName).Err(err).Msg("NodePoolRegistry: falha ao upsert")
				continue
			}
			res.Inserted++
			totalInserted++
		}

		results = append(results, res)
	}

	c.JSON(200, gin.H{
		"results":        results,
		"total_inserted": totalInserted,
		"scanned_at":     time.Now().UTC(),
	})
}

// Lookup dado um nome de entidade Dynatrace (ex: "aks-nodepool1-12345678-vmss000000"),
// extrai o nome do node pool e retorna os clusters que o possuem.
// GET /api/v1/nodepools/registry/lookup?name=aks-nodepool1-12345678-vmss000000
func (h *NodePoolRegistryHandler) Lookup(c *gin.Context) {
	entityName := c.Query("name")
	if entityName == "" {
		c.JSON(400, gin.H{"error": "parâmetro 'name' obrigatório"})
		return
	}

	poolName := extractNodePoolFromName(entityName)
	if poolName == "" {
		c.JSON(200, gin.H{
			"entity_name": entityName,
			"nodepool":    "",
			"matches":     []interface{}{},
			"found":       false,
		})
		return
	}

	entries, err := h.store.LookupByNodePool(poolName)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("falha na consulta: %v", err)})
		return
	}

	c.JSON(200, gin.H{
		"entity_name": entityName,
		"nodepool":    poolName,
		"matches":     entries,
		"found":       len(entries) > 0,
	})
}

// extractNodePoolFromName extrai o nome do node pool de um nome de nó AKS.
// Padrão: aks-<nodepool>-<5-8 dígitos>-vmss<hexid>
// Ex: "aks-spotool-12345678-vmss000001" → "spotool"
var aksNodeNameRe = regexp.MustCompile(`^aks-(.+?)-\d{5,8}-vmss[0-9a-f]+`)

func extractNodePoolFromName(name string) string {
	lower := strings.ToLower(name)
	m := aksNodeNameRe.FindStringSubmatch(lower)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}
