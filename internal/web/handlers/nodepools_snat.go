package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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

	c.JSON(http.StatusOK, profile)
}
