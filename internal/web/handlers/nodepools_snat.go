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
	snatPortsPerIPAzure = 64000 // Azure LB: portas SNAT por IP público
	snatPortsPerIPGCP   = 64512 // Cloud NAT: faixa de portas 0-64511
	snatPortsPerIPAWS   = 55000 // AWS NAT Gateway: conexões simultâneas por destino único por EIP
)

// SNATNodePoolInfo representa a contribuição de um node pool ao orçamento SNAT
type SNATNodePoolInfo struct {
	Name          string `json:"name"`
	NodeCount     int    `json:"node_count"`
	RequiredPorts int    `json:"required_ports"`
}

// SNATProfile resultado do cálculo do orçamento de portas SNAT
type SNATProfile struct {
	Cluster                  string             `json:"cluster"`
	CloudProvider            string             `json:"cloud_provider"`            // "aks" | "eks" | "gke"
	AllocatedOutboundPorts   int                `json:"allocated_outbound_ports"`  // 0 = N/A (EKS)
	OutboundIPCount          int                `json:"outbound_ip_count"`
	MaxPortsPerIP            int                `json:"max_ports_per_ip"`
	TotalNodeCount           int                `json:"total_node_count"`
	TotalAvailablePorts      int                `json:"total_available_ports"`
	TotalRequiredPorts       int                `json:"total_required_ports"`
	PortDeficit              int                `json:"port_deficit"`
	UsagePercent             float64            `json:"usage_percent"`
	MaxNodesAllowed          int                `json:"max_nodes_allowed"`          // 0 = N/A
	NodesUntilLimit          int                `json:"nodes_until_limit"`
	IPsNeededForCurrentNodes int                `json:"ips_needed_for_current_nodes"`
	Status                   string             `json:"status"` // ok / warning / critical
	NodePools                []SNATNodePoolInfo `json:"node_pools"`
	FetchedAt                time.Time          `json:"fetched_at"`
	Error                    string             `json:"error,omitempty"`
}

// detectSNATProvider retorna "gke", "eks" ou "aks" com base no prefixo do context.
func detectSNATProvider(clusterCtx string) string {
	if strings.HasPrefix(clusterCtx, "gke_") {
		return "gke"
	}
	if strings.HasPrefix(clusterCtx, "arn:aws:eks:") {
		return "eks"
	}
	return "aks"
}

// ─── AKS ──────────────────────────────────────────────────────────────────────

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
		} `json:"loadBalancerProfile"`
	} `json:"networkProfile"`
	AgentPoolProfiles []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"agentPoolProfiles"`
}

func (h *NodePoolHandler) buildSNATProfileAKS(ctx context.Context, clusterCtx string, totalNodes int, pools []SNATNodePoolInfo) (SNATProfile, error) {
	cfg, err := findClusterInConfig(clusterCtx)
	if err != nil {
		return SNATProfile{}, fmt.Errorf("cluster não encontrado na config AKS: %w", err)
	}

	args := []string{
		"aks", "show",
		"--name", strings.TrimSuffix(cfg.ClusterName, "-admin"),
		"--resource-group", cfg.ResourceGroup,
		"--subscription", cfg.Subscription,
		"--query", "{networkProfile: networkProfile, agentPoolProfiles: agentPoolProfiles}",
		"-o", "json",
	}
	out, err := exec.CommandContext(ctx, "az", args...).Output()
	if err != nil {
		return SNATProfile{}, fmt.Errorf("az aks show falhou: %w", err)
	}

	var azResp lbProfileResponse
	if err := json.Unmarshal(out, &azResp); err != nil {
		return SNATProfile{}, fmt.Errorf("falha ao parsear resposta az: %w", err)
	}

	lbProfile := azResp.NetworkProfile.LoadBalancerProfile
	allocatedPorts := lbProfile.AllocatedOutboundPorts

	ipCount := lbProfile.ManagedOutboundIPs.Count
	if ipCount == 0 {
		ipCount = len(lbProfile.OutboundIPs.PublicIPs)
	}
	if ipCount == 0 {
		ipCount = 1
	}

	// Agregar node pools do az aks show (mais preciso que K8s API para pools em scaling)
	var aksPoolsFromAz []SNATNodePoolInfo
	aksTotal := 0
	for _, ap := range azResp.AgentPoolProfiles {
		required := ap.Count * allocatedPorts
		aksPoolsFromAz = append(aksPoolsFromAz, SNATNodePoolInfo{
			Name:          ap.Name,
			NodeCount:     ap.Count,
			RequiredPorts: required,
		})
		aksTotal += ap.Count
	}
	if len(aksPoolsFromAz) > 0 {
		pools = aksPoolsFromAz
		totalNodes = aksTotal
	}

	return buildSNATProfileFromValues(clusterCtx, "aks", allocatedPorts, ipCount, snatPortsPerIPAzure, totalNodes, pools,
		"allocatedOutboundPorts=0 (gerenciado automaticamente pelo Azure — sem cálculo de déficit)"), nil
}

// ─── GKE ──────────────────────────────────────────────────────────────────────

// gkeRouterNat resposta parcial de gcloud compute routers nats describe
type gkeRouterNat struct {
	Name                string   `json:"name"`
	MinPortsPerVm       int      `json:"minPortsPerVm"`
	NatIpAllocateOption string   `json:"natIpAllocateOption"` // MANUAL_ONLY | AUTO_ONLY
	NatIps              []string `json:"natIps"`              // só se MANUAL_ONLY
}

func (h *NodePoolHandler) buildSNATProfileGKE(ctx context.Context, clusterCtx string, totalNodes int, pools []SNATNodePoolInfo) (SNATProfile, error) {
	gkeCfg := h.kubeManager.GetGKEClusterConfig(clusterCtx)
	if gkeCfg == nil {
		return SNATProfile{}, fmt.Errorf("cluster GKE não encontrado na config: %s", clusterCtx)
	}

	// Região: strip de zona (us-central1-a → us-central1)
	region := gkeCfg.Region
	if parts := strings.Split(region, "-"); len(parts) == 3 {
		// Se última parte é letra única (a, b, c) é zona — remover
		if len(parts[2]) == 1 {
			region = strings.Join(parts[:2], "-")
		}
	}

	// 1. Listar routers na região
	listArgs := []string{
		"compute", "routers", "list",
		"--project", gkeCfg.ProjectID,
		"--regions", region,
		"--format", "json(name,nats)",
	}
	out, err := exec.CommandContext(ctx, "gcloud", listArgs...).Output()
	if err != nil {
		return SNATProfile{}, fmt.Errorf("gcloud compute routers list falhou: %w", err)
	}

	// Resposta: array de routers com campo nats[]
	var routers []struct {
		Name string `json:"name"`
		Nats []struct {
			Name                string   `json:"name"`
			MinPortsPerVm       int      `json:"minPortsPerVm"`
			NatIpAllocateOption string   `json:"natIpAllocateOption"`
			NatIps              []string `json:"natIps"`
		} `json:"nats"`
	}
	if err := json.Unmarshal(out, &routers); err != nil {
		return SNATProfile{}, fmt.Errorf("falha ao parsear routers GKE: %w", err)
	}

	// Agregar todos os Cloud NATs encontrados
	allocatedPorts := 0
	ipCount := 0
	autoAllocated := false

	for _, router := range routers {
		for _, nat := range router.Nats {
			if nat.MinPortsPerVm > allocatedPorts {
				allocatedPorts = nat.MinPortsPerVm // pega o mais restritivo
			}
			if nat.NatIpAllocateOption == "AUTO_ONLY" {
				autoAllocated = true
				ipCount++ // não conhecemos o número exato — contamos 1 por NAT como base
			} else {
				ipCount += len(nat.NatIps)
			}
		}
	}

	// Default do Cloud NAT quando minPortsPerVm não está configurado explicitamente
	if allocatedPorts == 0 {
		allocatedPorts = 64 // default GKE Cloud NAT
	}
	if ipCount == 0 {
		ipCount = 1
	}

	errMsg := ""
	if autoAllocated {
		errMsg = "Cloud NAT com IPs auto-alocados — contagem de IPs pode estar subestimada"
	}
	if len(routers) == 0 {
		errMsg = "nenhum Cloud NAT encontrado nesta região — cluster pode usar rota de saída diferente"
		return SNATProfile{
			Cluster:       clusterCtx,
			CloudProvider: "gke",
			TotalNodeCount: totalNodes,
			NodePools:     pools,
			Status:        "ok",
			FetchedAt:     time.Now(),
			Error:         errMsg,
		}, nil
	}

	return buildSNATProfileFromValues(clusterCtx, "gke", allocatedPorts, ipCount, snatPortsPerIPGCP, totalNodes, pools, errMsg), nil
}

// ─── EKS ──────────────────────────────────────────────────────────────────────

// natGatewayResponse resposta parcial do aws ec2 describe-nat-gateways
type natGatewayResponse struct {
	NatGateways []struct {
		NatGatewayId      string `json:"NatGatewayId"`
		NatGatewayAddresses []struct {
			PublicIp string `json:"PublicIp"`
		} `json:"NatGatewayAddresses"`
	} `json:"NatGateways"`
}

func (h *NodePoolHandler) buildSNATProfileEKS(ctx context.Context, clusterCtx string, totalNodes int, pools []SNATNodePoolInfo) (SNATProfile, error) {
	eksCfg := h.kubeManager.GetEKSClusterConfig(clusterCtx)
	if eksCfg == nil {
		return SNATProfile{}, fmt.Errorf("cluster EKS não encontrado na config: %s", clusterCtx)
	}

	// Obter VPC ID: usar o que está na config ou buscar via aws eks
	vpcID := eksCfg.VpcID
	if vpcID == "" {
		vpcArgs := []string{"eks", "describe-cluster", "--name", eksCfg.Name, "--region", eksCfg.AwsRegion, "--query", "cluster.resourcesVpcConfig.vpcId", "--output", "text"}
		if eksCfg.AwsProfile != "" {
			vpcArgs = append(vpcArgs, "--profile", eksCfg.AwsProfile)
		}
		out, err := exec.CommandContext(ctx, "aws", vpcArgs...).Output()
		if err != nil {
			return SNATProfile{}, fmt.Errorf("aws eks describe-cluster falhou: %w", err)
		}
		vpcID = strings.TrimSpace(string(out))
	}

	// Listar NAT Gateways disponíveis na VPC
	natArgs := []string{
		"ec2", "describe-nat-gateways",
		"--region", eksCfg.AwsRegion,
		"--filter", fmt.Sprintf("Name=vpc-id,Values=%s", vpcID), "Name=state,Values=available",
		"--output", "json",
	}
	if eksCfg.AwsProfile != "" {
		natArgs = append(natArgs, "--profile", eksCfg.AwsProfile)
	}
	out, err := exec.CommandContext(ctx, "aws", natArgs...).Output()
	if err != nil {
		return SNATProfile{}, fmt.Errorf("aws ec2 describe-nat-gateways falhou: %w", err)
	}

	var natResp natGatewayResponse
	if err := json.Unmarshal(out, &natResp); err != nil {
		return SNATProfile{}, fmt.Errorf("falha ao parsear NAT Gateways: %w", err)
	}

	// Contar EIPs totais em todos os NAT Gateways
	natGWCount := len(natResp.NatGateways)
	eipCount := 0
	for _, gw := range natResp.NatGateways {
		eipCount += len(gw.NatGatewayAddresses)
	}
	if eipCount == 0 && natGWCount > 0 {
		eipCount = natGWCount // fallback: 1 EIP por NAT GW
	}
	if eipCount == 0 {
		eipCount = 1
	}

	// EKS: sem "portas por nó" — modelo baseado em conexões simultâneas por NAT GW EIP/destino.
	// AllocatedOutboundPorts = 0 indica ao frontend que a fórmula AKS não se aplica.
	// Usamos snatPortsPerIPAWS (55000) como capacidade por EIP para dar uma referência de escala.
	totalAvailable := eipCount * snatPortsPerIPAWS

	errMsg := ""
	if natGWCount == 0 {
		errMsg = "nenhum NAT Gateway ativo na VPC — pods podem usar Internet Gateway direto (IP público por nó) ou outro mecanismo de saída"
	} else {
		errMsg = fmt.Sprintf("EKS: %d NAT Gateway(s) com %d EIP(s) — limite de 55k conexões simultâneas por EIP por destino único (modelo diferente de AKS)", natGWCount, eipCount)
	}

	profile := SNATProfile{
		Cluster:              clusterCtx,
		CloudProvider:        "eks",
		AllocatedOutboundPorts: 0, // N/A para EKS
		OutboundIPCount:      eipCount,
		MaxPortsPerIP:        snatPortsPerIPAWS,
		TotalNodeCount:       totalNodes,
		TotalAvailablePorts:  totalAvailable,
		TotalRequiredPorts:   0, // não calculável sem dados de tráfego
		PortDeficit:          0,
		UsagePercent:         0, // preenchido via conntrack se disponível
		MaxNodesAllowed:      0, // N/A: NAT GW não limita por nó
		NodesUntilLimit:      0,
		IPsNeededForCurrentNodes: 0,
		Status:               "ok",
		NodePools:            pools,
		FetchedAt:            time.Now(),
		Error:                errMsg,
	}
	return profile, nil
}

// ─── Formula compartilhada (AKS + GKE) ───────────────────────────────────────

// buildSNATProfileFromValues calcula SNATProfile para providers que usam a fórmula
// ports-per-node (AKS = Azure LB, GKE = Cloud NAT).
func buildSNATProfileFromValues(clusterCtx, provider string, allocatedPorts, ipCount, portsPerIP, totalNodes int, pools []SNATNodePoolInfo, autoMsg string) SNATProfile {
	totalAvailable := ipCount * portsPerIP
	totalRequired := totalNodes * allocatedPorts
	deficit := totalRequired - totalAvailable

	usagePct := 0.0
	if totalAvailable > 0 && allocatedPorts > 0 {
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
		ipsNeeded = (totalNodes*allocatedPorts + portsPerIP - 1) / portsPerIP
	}

	status := "ok"
	errMsg := ""
	if allocatedPorts == 0 {
		errMsg = autoMsg
	} else if usagePct >= 95 {
		status = "critical"
	} else if usagePct >= 80 {
		status = "warning"
	}

	return SNATProfile{
		Cluster:                  clusterCtx,
		CloudProvider:            provider,
		AllocatedOutboundPorts:   allocatedPorts,
		OutboundIPCount:          ipCount,
		MaxPortsPerIP:            portsPerIP,
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
		Error:                    errMsg,
	}
}

// ─── Handler principal ────────────────────────────────────────────────────────

// GetSNATProfile calcula o orçamento de portas SNAT/NAT do cluster (AKS, GKE ou EKS).
// GET /api/v1/nodepools/snat?cluster=<context>
func (h *NodePoolHandler) GetSNATProfile(c *gin.Context) {
	clusterCtx := c.Query("cluster")
	if clusterCtx == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro cluster é obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Coletar nós via K8s API (comum a todos os providers)
	totalNodes, pools := h.collectNodePoolsFromK8s(ctx, clusterCtx)

	provider := detectSNATProvider(clusterCtx)

	var profile SNATProfile
	var err error
	switch provider {
	case "gke":
		profile, err = h.buildSNATProfileGKE(ctx, clusterCtx, totalNodes, pools)
	case "eks":
		profile, err = h.buildSNATProfileEKS(ctx, clusterCtx, totalNodes, pools)
	default:
		profile, err = h.buildSNATProfileAKS(ctx, clusterCtx, totalNodes, pools)
	}

	if err != nil {
		log.Warn().Err(err).Str("cluster", clusterCtx).Str("provider", provider).Msg("snat: falha ao obter perfil")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Salvar snapshot no histórico (assíncrono)
	if h.snatStore != nil && profile.AllocatedOutboundPorts > 0 {
		snap := profile
		go func() {
			if err := h.snatStore.Save(storage.SNATHistoryRecord{
				Cluster:                snap.Cluster,
				TotalNodeCount:         snap.TotalNodeCount,
				UsagePercent:           snap.UsagePercent,
				NodesUntilLimit:        snap.NodesUntilLimit,
				AllocatedOutboundPorts: snap.AllocatedOutboundPorts,
				OutboundIPCount:        snap.OutboundIPCount,
				RecordedAt:             snap.FetchedAt,
			}); err != nil {
				log.Warn().Err(err).Str("cluster", snap.Cluster).Msg("snat_history: falha ao salvar snapshot")
			}
		}()
	}

	c.JSON(http.StatusOK, profile)
}

// collectNodePoolsFromK8s lista nós do cluster via K8s API e agrupa por node pool.
// Funciona para todos os cloud providers. Retorna totalNodes e []SNATNodePoolInfo.
func (h *NodePoolHandler) collectNodePoolsFromK8s(ctx context.Context, clusterCtx string) (int, []SNATNodePoolInfo) {
	clientset, err := h.kubeManager.GetClient(clusterCtx)
	if err != nil {
		return 0, nil
	}
	nodeList, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, nil
	}

	poolCounts := map[string]int{}
	for _, n := range nodeList.Items {
		pool := nodePoolLabel(n.Labels)
		poolCounts[pool]++
	}

	var pools []SNATNodePoolInfo
	total := 0
	for name, count := range poolCounts {
		pools = append(pools, SNATNodePoolInfo{Name: name, NodeCount: count})
		total += count
	}
	return total, pools
}

// nodePoolLabel retorna o nome do node pool a partir dos labels do nó (multi-cloud).
func nodePoolLabel(labels map[string]string) string {
	// AKS
	if v := labels["kubernetes.azure.com/agentpool"]; v != "" {
		return v
	}
	if v := labels["agentpool"]; v != "" {
		return v
	}
	// EKS
	if v := labels["eks.amazonaws.com/nodegroup"]; v != "" {
		return v
	}
	// GKE
	if v := labels["cloud.google.com/gke-nodepool"]; v != "" {
		return v
	}
	return "default"
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
	SNATUsagePct      float64 `json:"snat_usage_pct"`      // conntrack/allocated (estimativa)
	ConntrackUsagePct float64 `json:"conntrack_usage_pct"` // conntrack/max
	Status            string  `json:"status"`              // ok/warning/critical/unknown
}

// SNATNodesResponse resposta do endpoint de breakdown por nó
type SNATNodesResponse struct {
	Cluster             string         `json:"cluster"`
	CloudProvider       string         `json:"cloud_provider"`
	AllocatedPorts      int            `json:"allocated_outbound_ports"`
	Nodes               []SNATNodeStat `json:"nodes"`
	PrometheusAvailable bool           `json:"prometheus_available"`
	FetchedAt           time.Time      `json:"fetched_at"`
}

// GetSNATNodes retorna breakdown de uso SNAT estimado por nó via Prometheus (conntrack como proxy).
// Funciona para AKS, EKS e GKE — conntrack é métrica kernel independente do cloud provider.
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
		Cluster:       clusterCtx,
		CloudProvider: detectSNATProvider(clusterCtx),
		FetchedAt:     time.Now(),
	}

	// Obter allocatedOutboundPorts do histórico SQLite (evita chamada cloud)
	if h.snatStore != nil {
		if latest, err := h.snatStore.GetLatest(clusterCtx); err == nil && latest != nil {
			resp.AllocatedPorts = latest.AllocatedOutboundPorts
		}
	}

	// Listar nós K8s
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

	// Consultar Prometheus (instant query — todos os nós de uma vez)
	prom, promErr := h.getPromClient(clusterCtx)
	if promErr != nil {
		// Sem Prometheus: retornar nós sem dados de conntrack
		for _, n := range nodeList.Items {
			resp.Nodes = append(resp.Nodes, SNATNodeStat{
				Name:   n.Name,
				Pool:   nodePoolLabel(n.Labels),
				Status: "unknown",
			})
		}
		resp.PrometheusAvailable = false
		c.JSON(http.StatusOK, resp)
		return
	}
	resp.PrometheusAvailable = true

	entriesResult, _ := prom.Query(ctx, "node_nf_conntrack_entries")
	limitResult, _ := prom.Query(ctx, "node_nf_conntrack_entries_limit")

	entriesByIP := map[string]float64{}
	limitByIP := map[string]float64{}
	if entriesResult != nil {
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

	for _, n := range nodeList.Items {
		var ip string
		for _, addr := range n.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				ip = addr.Address
				break
			}
		}

		stat := SNATNodeStat{
			Name:           n.Name,
			Pool:           nodePoolLabel(n.Labels),
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
			stat.Status = "unknown"
		}

		// Status por conntrack: para EKS (AllocatedPorts=0) usa ConntrackUsagePct
		if stat.ConntrackEntries > 0 {
			usagePct := stat.SNATUsagePct
			if resp.AllocatedPorts == 0 {
				usagePct = stat.ConntrackUsagePct
			}
			switch {
			case usagePct >= 90:
				stat.Status = "critical"
			case usagePct >= 70:
				stat.Status = "warning"
			default:
				stat.Status = "ok"
			}
		}

		resp.Nodes = append(resp.Nodes, stat)
	}

	sort.Slice(resp.Nodes, func(i, j int) bool {
		pi := resp.Nodes[i].SNATUsagePct
		pj := resp.Nodes[j].SNATUsagePct
		if resp.AllocatedPorts == 0 {
			pi = resp.Nodes[i].ConntrackUsagePct
			pj = resp.Nodes[j].ConntrackUsagePct
		}
		return pi > pj
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
