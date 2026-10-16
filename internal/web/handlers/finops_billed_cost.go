package handlers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/config"
)

// Custo faturado do cluster (FINOPS-REVISAO-PLAN.md, Fase 3): custo AMORTIZADO dos últimos 30 dias
// (até ontem) do RG do AKS e do RG de nodes (MC_...), por serviço do Azure. Fica ao lado do custo
// estimado pelo relatório — a diferença mostra o que a estimativa não vê (rede, load balancer,
// Log Analytics, plano do AKS) e o desconto de reserva/Savings Plan. Só leitura, só AKS.

const (
	billedCostDays    = 30
	billedCostTimeout = 90 * time.Second
	billedCostTTL     = 6 * time.Hour
)

type billedService struct {
	Service string  `json:"service"`
	Cost    float64 `json:"cost"`
}

type billedCostRG struct {
	Name     string          `json:"name"`
	Role     string          `json:"role"` // "cluster" (RG do AKS) | "nodes" (MC_...)
	Total    float64         `json:"total"`
	Services []billedService `json:"services"`
}

type billedCostResponse struct {
	Available      bool            `json:"available"`
	Reason         string          `json:"reason,omitempty"`
	Cluster        string          `json:"cluster"`
	Currency       string          `json:"currency,omitempty"`
	WindowDays     int             `json:"window_days,omitempty"`
	From           string          `json:"from,omitempty"`
	To             string          `json:"to,omitempty"`
	FetchedAt      time.Time       `json:"fetched_at,omitempty"`
	Total          float64         `json:"total"`
	Services       []billedService `json:"services,omitempty"` // somados entre os RGs, maior primeiro
	ResourceGroups []billedCostRG  `json:"resource_groups,omitempty"`
}

// sortedServices transforma serviço → custo em lista do maior para o menor (ignora centavos).
func sortedServices(m map[string]float64) ([]billedService, float64) {
	out := make([]billedService, 0, len(m))
	total := 0.0
	for s, v := range m {
		total += v
		if v >= 0.01 || v <= -0.01 {
			out = append(out, billedService{Service: s, Cost: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Service < out[j].Service
	})
	return out, total
}

// buildBilledCost monta a resposta a partir do custo por RG e serviço. Pura, para teste.
func buildBilledCost(cluster string, cost azure.RGServiceCost, clusterRG, nodeRG string) billedCostResponse {
	resp := billedCostResponse{Available: true, Cluster: cluster, Currency: cost.Currency}
	merged := map[string]float64{}
	for _, rg := range []struct{ name, role string }{{clusterRG, "cluster"}, {nodeRG, "nodes"}} {
		if rg.name == "" {
			continue
		}
		byService := cost.ByRG[strings.ToLower(rg.name)]
		services, total := sortedServices(byService)
		for s, v := range byService {
			merged[s] += v
		}
		resp.ResourceGroups = append(resp.ResourceGroups, billedCostRG{Name: rg.name, Role: rg.role, Total: total, Services: services})
	}
	resp.Services, resp.Total = sortedServices(merged)
	return resp
}

// GetBilledCost godoc
// GET /api/v1/finops/billed-cost?cluster=X[&refresh=true]
//
// Custo faturado (amortizado, 30 dias) dos RGs do cluster AKS, por serviço. Em cache por 6 h.
// Falha de autenticação/permissão volta 200 com available=false e o motivo (o card some com aviso).
func (h *FinOpsHandler) GetBilledCost(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster) != config.CloudProviderAKS {
		c.JSON(http.StatusOK, billedCostResponse{Cluster: cluster,
			Reason: "O custo faturado vem do Cost Management do Azure — disponível só para clusters AKS."})
		return
	}

	h.billedMu.Lock()
	if h.billedCache == nil {
		h.billedCache = map[string]billedCostResponse{}
	}
	cached, ok := h.billedCache[cluster]
	h.billedMu.Unlock()
	if ok && c.Query("refresh") != "true" && time.Since(cached.FetchedAt) < billedCostTTL {
		c.JSON(http.StatusOK, cached)
		return
	}

	v, _, _ := h.billedSF.Do(cluster, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), billedCostTimeout)
		defer cancel()
		return h.fetchBilledCost(ctx, cluster), nil
	})
	resp := v.(billedCostResponse)
	if resp.Available {
		h.billedMu.Lock()
		h.billedCache[cluster] = resp
		h.billedMu.Unlock()
	}
	c.JSON(http.StatusOK, resp)
}

func (h *FinOpsHandler) fetchBilledCost(ctx context.Context, cluster string) billedCostResponse {
	fail := func(reason string) billedCostResponse {
		log.Warn().Str("cluster", cluster).Str("motivo", reason).Msg("FinOps/BilledCost: custo faturado indisponível")
		return billedCostResponse{Cluster: cluster, Reason: reason}
	}
	arm, cfg, err := h.coverageClient(ctx, cluster)
	if err != nil {
		return fail(err.Error())
	}
	nodeRG, err := arm.NodeResourceGroup(ctx, cfg.ResourceGroup, cfg.Name)
	if err != nil {
		return fail("Falha ao descobrir o resource group de nodes do AKS: " + err.Error())
	}
	from, to := coverageWindow(billedCostDays)
	cost, err := arm.CostByResourceGroupAndService(ctx, []string{cfg.ResourceGroup, nodeRG}, from, to)
	if err != nil {
		return fail(costQueryErr("custo faturado", err).Error())
	}
	resp := buildBilledCost(cluster, cost, cfg.ResourceGroup, nodeRG)
	resp.WindowDays = billedCostDays
	resp.From, resp.To = from.Format("2006-01-02"), to.Format("2006-01-02")
	resp.FetchedAt = time.Now()
	return resp
}
