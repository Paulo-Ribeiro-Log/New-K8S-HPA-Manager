package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	snatPortsPerIP = 64000 // Azure: portas SNAT disponíveis por IP público
)

// SNATNodePoolInfo representa a contribuição de um node pool ao orçamento SNAT
type SNATNodePoolInfo struct {
	Name          string `json:"name"`
	NodeCount     int    `json:"node_count"`
	RequiredPorts int    `json:"required_ports"`
}

// SNATProfile resultado do cálculo do orçamento de portas SNAT
type SNATProfile struct {
	Cluster                string             `json:"cluster"`
	AllocatedOutboundPorts int                `json:"allocated_outbound_ports"`
	OutboundIPCount        int                `json:"outbound_ip_count"`
	MaxPortsPerIP          int                `json:"max_ports_per_ip"`
	TotalNodeCount         int                `json:"total_node_count"`
	TotalAvailablePorts    int                `json:"total_available_ports"`
	TotalRequiredPorts     int                `json:"total_required_ports"`
	PortDeficit            int                `json:"port_deficit"` // positivo = falta portas
	UsagePercent           float64            `json:"usage_percent"`
	MaxNodesAllowed        int                `json:"max_nodes_allowed"`
	NodesUntilLimit        int                `json:"nodes_until_limit"` // quantos nós ainda cabem
	IPsNeededForCurrentNodes int              `json:"ips_needed_for_current_nodes"`
	Status                 string             `json:"status"` // ok / warning / critical
	NodePools              []SNATNodePoolInfo `json:"node_pools"`
	FetchedAt              time.Time          `json:"fetched_at"`
	Error                  string             `json:"error,omitempty"`
}

// lbProfileResponse estrutura parcial do az aks show para o LB profile
type lbProfileResponse struct {
	NetworkProfile struct {
		LoadBalancerProfile struct {
			AllocatedOutboundPorts int `json:"allocatedOutboundPorts"`
			ManagedOutboundIPs     struct {
				Count int `json:"count"`
			} `json:"managedOutboundIPs"`
			OutboundIPs struct {
				PublicIPs []struct {
					ID string `json:"id"`
				} `json:"publicIPs"`
			} `json:"outboundIPs"`
			OutboundIPPrefixes struct {
				PublicIPPrefixes []interface{} `json:"publicIPPrefixes"`
			} `json:"outboundIPPrefixes"`
		} `json:"loadBalancerProfile"`
	} `json:"networkProfile"`
	AgentPoolProfiles []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"agentPoolProfiles"`
}

// GetSNATProfile calcula o orçamento de portas SNAT do cluster AKS.
// GET /api/v1/nodepools/snat?cluster=<context>
func (h *NodePoolHandler) GetSNATProfile(c *gin.Context) {
	clusterCtx := c.Query("cluster")
	if clusterCtx == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro cluster é obrigatório"})
		return
	}

	cfg, err := findClusterInConfig(clusterCtx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("cluster não encontrado na config: %v", err)})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Consulta az aks show para obter LB profile + node pools
	args := []string{
		"aks", "show",
		"--name", strings.TrimSuffix(cfg.ClusterName, "-admin"),
		"--resource-group", cfg.ResourceGroup,
		"--subscription", cfg.Subscription,
		"--query", "{networkProfile: networkProfile, agentPoolProfiles: agentPoolProfiles}",
		"-o", "json",
	}
	cmd := exec.CommandContext(ctx, "az", args...)
	out, err := cmd.Output()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("az aks show falhou: %v", err)})
		return
	}

	var azResp lbProfileResponse
	if err := json.Unmarshal(out, &azResp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao parsear resposta: %v", err)})
		return
	}

	lbProfile := azResp.NetworkProfile.LoadBalancerProfile
	allocatedPorts := lbProfile.AllocatedOutboundPorts
	if allocatedPorts == 0 {
		allocatedPorts = 0 // Azure default = auto (calculado pelo Azure, não por nós)
	}

	// Contar IPs: managedOutboundIPs tem precedência; fallback para outboundIPs.publicIPs
	ipCount := lbProfile.ManagedOutboundIPs.Count
	if ipCount == 0 {
		ipCount = len(lbProfile.OutboundIPs.PublicIPs)
	}
	if ipCount == 0 {
		ipCount = 1 // mínimo default Azure
	}

	// Agregar node pools
	var pools []SNATNodePoolInfo
	totalNodes := 0
	for _, ap := range azResp.AgentPoolProfiles {
		required := ap.Count * allocatedPorts
		pools = append(pools, SNATNodePoolInfo{
			Name:          ap.Name,
			NodeCount:     ap.Count,
			RequiredPorts: required,
		})
		totalNodes += ap.Count
	}

	totalAvailable := ipCount * snatPortsPerIP
	totalRequired := totalNodes * allocatedPorts
	deficit := totalRequired - totalAvailable
	usagePct := 0.0
	if totalAvailable > 0 {
		usagePct = float64(totalRequired) / float64(totalAvailable) * 100
	}

	maxNodes := 0
	nodesUntilLimit := 0
	if allocatedPorts > 0 {
		maxNodes = totalAvailable / allocatedPorts
		nodesUntilLimit = maxNodes - totalNodes
		if nodesUntilLimit < 0 {
			nodesUntilLimit = 0
		}
	}

	ipsNeeded := 0
	if allocatedPorts > 0 && totalNodes > 0 {
		ipsNeeded = (totalNodes*allocatedPorts + snatPortsPerIP - 1) / snatPortsPerIP
	}

	status := "ok"
	if usagePct >= 100 {
		status = "critical"
	} else if usagePct >= 85 {
		status = "warning"
	}

	// allocatedPorts=0 significa "auto" pelo Azure — não calculamos deficit
	errorMsg := ""
	if allocatedPorts == 0 {
		errorMsg = "allocatedOutboundPorts=0 (gerenciado automaticamente pelo Azure — sem cálculo de déficit)"
		status = "ok"
	}

	profile := SNATProfile{
		Cluster:                  clusterCtx,
		AllocatedOutboundPorts:   allocatedPorts,
		OutboundIPCount:          ipCount,
		MaxPortsPerIP:            snatPortsPerIP,
		TotalNodeCount:           totalNodes,
		TotalAvailablePorts:      totalAvailable,
		TotalRequiredPorts:       totalRequired,
		PortDeficit:              deficit,
		UsagePercent:             usagePct,
		MaxNodesAllowed:          maxNodes,
		NodesUntilLimit:          nodesUntilLimit,
		IPsNeededForCurrentNodes: ipsNeeded,
		Status:                   status,
		NodePools:                pools,
		FetchedAt:                time.Now(),
		Error:                    errorMsg,
	}

	// Salvar snapshot no histórico (assíncrono, erros ignorados)
	if h.snatStore != nil && allocatedPorts > 0 {
		go func() {
			if err := h.snatStore.Save(storage.SNATHistoryRecord{
				Cluster:                clusterCtx,
				TotalNodeCount:         totalNodes,
				UsagePercent:           usagePct,
				NodesUntilLimit:        nodesUntilLimit,
				AllocatedOutboundPorts: allocatedPorts,
				OutboundIPCount:        ipCount,
				RecordedAt:             profile.FetchedAt,
			}); err != nil {
				log.Warn().Err(err).Str("cluster", clusterCtx).Msg("snat_history: falha ao salvar snapshot")
			}
		}()
	}

	c.JSON(http.StatusOK, profile)
}

// GetSNATProjection retorna o histórico e a projeção de crescimento de nós vs. limite SNAT.
// GET /api/v1/nodepools/snat/projection?cluster=<context>
func (h *NodePoolHandler) GetSNATProjection(c *gin.Context) {
	clusterCtx := c.Query("cluster")
	if clusterCtx == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro cluster é obrigatório"})
		return
	}
	if h.snatStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "histórico SNAT não disponível"})
		return
	}

	records, err := h.snatStore.GetRecent(clusterCtx, 30)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao ler histórico: %v", err)})
		return
	}

	// nodesUntilLimit atual: precisamos do último snapshot
	nodesUntilLimit := 0
	if len(records) > 0 {
		nodesUntilLimit = records[len(records)-1].NodesUntilLimit
	}

	proj := storage.ComputeSNATProjection(records, nodesUntilLimit)
	c.JSON(http.StatusOK, proj)
}

// ─── Breakdown por nó ─────────────────────────────────────────────────────────

// SNATNodeStat estatísticas de uso SNAT estimado para um nó individual
type SNATNodeStat struct {
	Name              string  `json:"name"`
	Pool              string  `json:"pool"`
	InternalIP        string  `json:"internal_ip"`
	ConntrackEntries  int64   `json:"conntrack_entries"`
	ConntrackMax      int64   `json:"conntrack_max"`
	AllocatedPorts    int     `json:"allocated_ports"`
	SNATUsagePct      float64 `json:"snat_usage_pct"`       // conntrack/allocated (estimativa)
	ConntrackUsagePct float64 `json:"conntrack_usage_pct"`  // conntrack/max
	Status            string  `json:"status"`               // ok/warning/critical/unknown
}

// SNATNodesResponse resposta do endpoint de breakdown por nó
type SNATNodesResponse struct {
	Cluster             string         `json:"cluster"`
	AllocatedPorts      int            `json:"allocated_outbound_ports"`
	Nodes               []SNATNodeStat `json:"nodes"`
	PrometheusAvailable bool           `json:"prometheus_available"`
	FetchedAt           time.Time      `json:"fetched_at"`
}

// GetSNATNodes retorna breakdown de uso SNAT estimado por nó via Prometheus (conntrack como proxy).
// GET /api/v1/nodepools/snat/nodes?cluster=<context>
func (h *NodePoolHandler) GetSNATNodes(c *gin.Context) {
	clusterCtx := c.Query("cluster")
	if clusterCtx == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro cluster é obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp := SNATNodesResponse{
		Cluster:   clusterCtx,
		FetchedAt: time.Now(),
	}

	// 1. Obter allocatedOutboundPorts do histórico SQLite (evita az aks show)
	if h.snatStore != nil {
		if latest, err := h.snatStore.GetLatest(clusterCtx); err == nil && latest != nil {
			resp.AllocatedPorts = latest.AllocatedOutboundPorts
		}
	}

	// 2. Listar nós K8s (nome, pool, IP interno)
	clientset, err := h.kubeManager.GetClient(clusterCtx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao conectar ao cluster: %v", err)})
		return
	}

	nodeList, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("erro ao listar nós: %v", err)})
		return
	}

	// Mapa IP → node stat base
	ipToStat := map[string]*SNATNodeStat{}
	for _, n := range nodeList.Items {
		pool := n.Labels["kubernetes.azure.com/agentpool"]
		if pool == "" {
			pool = n.Labels["agentpool"]
		}
		var ip string
		for _, addr := range n.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				ip = addr.Address
				break
			}
		}
		stat := &SNATNodeStat{
			Name:           n.Name,
			Pool:           pool,
			InternalIP:     ip,
			AllocatedPorts: resp.AllocatedPorts,
			Status:         "unknown",
		}
		if ip != "" {
			ipToStat[ip] = stat
		}
		resp.Nodes = append(resp.Nodes, *stat)
	}

	// 3. Consultar Prometheus (instant query — todos os nós de uma vez)
	prom, promErr := h.getPromClient(clusterCtx)
	if promErr != nil {
		resp.PrometheusAvailable = false
		c.JSON(http.StatusOK, resp)
		return
	}
	resp.PrometheusAvailable = true

	entriesResult, err := prom.Query(ctx, "node_nf_conntrack_entries")
	limitResult, _ := prom.Query(ctx, "node_nf_conntrack_entries_limit")

	// Mapear IP → entries/limit
	entriesByIP := map[string]float64{}
	limitByIP := map[string]float64{}
	if err == nil {
		for _, series := range entriesResult.Data.Result {
			ip := strings.Split(series.Metric["instance"], ":")[0]
			if v, ok := parseSNATInstantValue(series.Value); ok {
				entriesByIP[ip] = v
			}
		}
	}
	if limitResult != nil {
		for _, series := range limitResult.Data.Result {
			ip := strings.Split(series.Metric["instance"], ":")[0]
			if v, ok := parseSNATInstantValue(series.Value); ok {
				limitByIP[ip] = v
			}
		}
	}

	// 4. Montar resposta final com conntrack + SNAT estimate
	resp.Nodes = nil
	for _, n := range nodeList.Items {
		pool := n.Labels["kubernetes.azure.com/agentpool"]
		if pool == "" {
			pool = n.Labels["agentpool"]
		}
		var ip string
		for _, addr := range n.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				ip = addr.Address
				break
			}
		}

		stat := SNATNodeStat{
			Name:           n.Name,
			Pool:           pool,
			InternalIP:     ip,
			AllocatedPorts: resp.AllocatedPorts,
			Status:         "ok",
		}
		if ip == "" {
			stat.Status = "unknown"
		}

		if entries, ok := entriesByIP[ip]; ok {
			stat.ConntrackEntries = int64(entries)
			if lim, ok := limitByIP[ip]; ok && lim > 0 {
				stat.ConntrackMax = int64(lim)
				stat.ConntrackUsagePct = entries / lim * 100
			}
			if resp.AllocatedPorts > 0 {
				pct := entries / float64(resp.AllocatedPorts) * 100
				if pct > 100 {
					pct = 100
				}
				stat.SNATUsagePct = pct
			}
		} else if ip != "" {
			stat.Status = "unknown" // sem dados de conntrack
		}

		if resp.AllocatedPorts > 0 && stat.ConntrackEntries > 0 {
			switch {
			case stat.SNATUsagePct >= 90:
				stat.Status = "critical"
			case stat.SNATUsagePct >= 70:
				stat.Status = "warning"
			default:
				stat.Status = "ok"
			}
		}

		resp.Nodes = append(resp.Nodes, stat)
	}

	// Ordenar por SNATUsagePct decrescente (nós mais críticos primeiro)
	sort.Slice(resp.Nodes, func(i, j int) bool {
		return resp.Nodes[i].SNATUsagePct > resp.Nodes[j].SNATUsagePct
	})

	c.JSON(http.StatusOK, resp)
}

// parseSNATInstantValue extrai o valor float64 de um par [timestamp, "value"] de query instantânea
func parseSNATInstantValue(val []interface{}) (float64, bool) {
	if len(val) < 2 {
		return 0, false
	}
	s, ok := val[1].(string)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}
