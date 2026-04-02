package handlers

import (
	"fmt"
	"net/http"
	"time"

	"k8s-hpa-manager/internal/config"

	"github.com/gin-gonic/gin"
)

// VPNStatusResponse representa o status da conexão VPN
type VPNStatusResponse struct {
	Connected    bool   `json:"connected"`
	Message      string `json:"message"`
	Timestamp    int64  `json:"timestamp"`
	CloudProvider string `json:"cloud_provider,omitempty"`
}

// VPNHandler verifica conectividade por cluster específico.
type VPNHandler struct {
	kubeManager *config.KubeConfigManager
}

func NewVPNHandler(km *config.KubeConfigManager) *VPNHandler {
	return &VPNHandler{kubeManager: km}
}

// CheckStatus testa conectividade TCP com o cluster informado via ?cluster=.
// Se não informado, retorna connected=true (sem bloquear o usuário).
// GET /api/v1/vpn/status?cluster=<context-name>
func (h *VPNHandler) CheckStatus(c *gin.Context) {
	clusterName := c.Query("cluster")

	resp := VPNStatusResponse{Timestamp: time.Now().Unix()}

	if clusterName == "" {
		// Sem cluster específico — não mostrar banner
		resp.Connected = true
		resp.Message = "Nenhum cluster selecionado"
		c.JSON(http.StatusOK, resp)
		return
	}

	const tcpTimeout = 5 * time.Second
	reachable := h.kubeManager.TestClusterTCPConnection(clusterName, tcpTimeout)

	serverURL := h.kubeManager.GetServerURL(clusterName)
	provider := config.DetectCloudProvider(serverURL)
	resp.CloudProvider = string(provider)

	if reachable {
		resp.Connected = true
		resp.Message = fmt.Sprintf("Cluster %s acessível", clusterName)
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.Connected = false
	switch provider {
	case config.CloudProviderEKS:
		resp.Message = "Cluster AWS EKS inacessível — verifique a VPN AWS"
	case config.CloudProviderAKS:
		resp.Message = "Cluster Azure AKS inacessível — verifique a VPN Azure"
	default:
		resp.Message = fmt.Sprintf("Cluster %s inacessível — verifique a VPN", clusterName)
	}
	c.JSON(http.StatusServiceUnavailable, resp)
}
