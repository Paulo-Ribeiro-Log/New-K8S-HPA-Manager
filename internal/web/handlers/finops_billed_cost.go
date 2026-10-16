package handlers

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/aws"
	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/cloudprovider/gcp"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/finops"
)

// Custo faturado do cluster (FINOPS-REVISAO-PLAN.md, Fase 3): custo AMORTIZADO dos últimos 30 dias
// (até ontem), por serviço, ao lado do custo estimado pelo relatório — a diferença mostra o que a
// estimativa não vê (rede, load balancer, monitoramento, plano do cluster) e o desconto de
// reserva/Savings Plan. Só leitura.
//   - AKS: Cost Management do RG do AKS, do RG de nodes (MC_...) e do RG de dados
//     (rg-<nome>-data-<env>, que pode estar em outra subscription — resolveDataRG);
//   - EKS: Cost Explorer pela tag aws:eks:cluster-name (precisa estar ativada como cost allocation tag);
//   - GKE: export de billing no BigQuery pelo label goog-k8s-cluster-name (tabela em
//     K8S_HPA_GKE_BILLING_TABLE).

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
	Role     string          `json:"role"` // "cluster" (RG do AKS / tag do EKS / label do GKE) | "nodes" (MC_...) | "data" (RG de dados)
	Total    float64         `json:"total"`
	Services []billedService `json:"services"`
}

type billedCostResponse struct {
	Available      bool            `json:"available"`
	Reason         string          `json:"reason,omitempty"`
	Cluster        string          `json:"cluster"`
	Provider       string          `json:"provider,omitempty"` // aks | eks | gke
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

// billedGroup é um escopo de custo (RG, tag ou label) com o custo por serviço.
type billedGroup struct {
	name, role string
	byService  map[string]float64
}

// buildBilledCost monta a resposta a partir dos escopos. Pura, para teste.
func buildBilledCost(cluster, currency string, groups []billedGroup) billedCostResponse {
	resp := billedCostResponse{Available: true, Cluster: cluster, Currency: currency}
	merged := map[string]float64{}
	for _, g := range groups {
		if g.name == "" {
			continue
		}
		services, total := sortedServices(g.byService)
		for s, v := range g.byService {
			merged[s] += v
		}
		resp.ResourceGroups = append(resp.ResourceGroups, billedCostRG{Name: g.name, Role: g.role, Total: total, Services: services})
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
	from, to := coverageWindow(billedCostDays)
	var resp billedCostResponse
	switch config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster) {
	case config.CloudProviderEKS:
		cfg := h.kubeManager.GetEKSClusterConfig(cluster)
		if cfg == nil {
			return fail("cluster EKS sem entrada em eks-clusters-config.json — rode o autodiscover")
		}
		sc, err := aws.EKSCostByService(ctx, cfg.AwsProfile, cfg.Name, from, to.Add(time.Second))
		if err != nil {
			return fail("Falha ao consultar o Cost Explorer: " + err.Error())
		}
		if len(sc.ByService) == 0 {
			return fail("O Cost Explorer não trouxe custo com a tag aws:eks:cluster-name=" + cfg.Name + ". Ative a tag como cost allocation tag no Billing da conta pagadora (leva ~24 h para aparecer).")
		}
		resp = buildBilledCost(cluster, sc.Currency, []billedGroup{{name: "tag aws:eks:cluster-name=" + cfg.Name, role: "cluster", byService: sc.ByService}})
		resp.Provider = "eks"
	case config.CloudProviderGKE:
		table := strings.TrimSpace(os.Getenv("K8S_HPA_GKE_BILLING_TABLE"))
		if table == "" {
			return fail("Para GKE, defina K8S_HPA_GKE_BILLING_TABLE com a tabela do export de billing no BigQuery (projeto.dataset.gcp_billing_export_v1_XXXX) e reinicie o servidor.")
		}
		name := cluster
		if cfg := h.kubeManager.GetGKEClusterConfig(cluster); cfg != nil {
			name = cfg.Name
		}
		sc, err := gcp.GKECostByService(ctx, table, name, from, to.Add(time.Second))
		if err != nil {
			return fail("Falha ao consultar o BigQuery: " + err.Error())
		}
		if len(sc.ByService) == 0 {
			return fail("O export de billing não trouxe custo com o label goog-k8s-cluster-name=" + name + ".")
		}
		resp = buildBilledCost(cluster, sc.Currency, []billedGroup{{name: "label goog-k8s-cluster-name=" + name, role: "cluster", byService: sc.ByService}})
		resp.Provider = "gke"
	default:
		arm, cfg, err := h.coverageClient(ctx, cluster)
		if err != nil {
			return fail(err.Error())
		}
		nodeRG, err := arm.NodeResourceGroup(ctx, cfg.ResourceGroup, cfg.Name)
		if err != nil {
			return fail("Falha ao descobrir o resource group de nodes do AKS: " + err.Error())
		}
		cost, err := arm.CostByResourceGroupAndService(ctx, []string{cfg.ResourceGroup, nodeRG}, from, to)
		if err != nil {
			return fail(costQueryErr("custo faturado", err).Error())
		}
		groups := []billedGroup{
			{name: cfg.ResourceGroup, role: "cluster", byService: cost.ByRG[strings.ToLower(cfg.ResourceGroup)]},
			{name: nodeRG, role: "nodes", byService: cost.ByRG[strings.ToLower(nodeRG)]},
		}
		// RG de dados (pode estar em outra subscription): complementar — falha só registra.
		if g, ok := h.billedDataRG(ctx, cluster, cfg.ResourceGroup, cfg.SubscriptionID, from, to); ok {
			groups = append(groups, g)
		}
		resp = buildBilledCost(cluster, cost.Currency, groups)
		resp.Provider = "aks"
	}
	resp.WindowDays = billedCostDays
	resp.From, resp.To = from.Format("2006-01-02"), to.Format("2006-01-02")
	resp.FetchedAt = time.Now()
	return resp
}

// billedDataRG consulta o custo do RG de dados do cluster (rg-<nome>-data-<env>), achado em
// qualquer subscription do tenant pela mesma resolução da aba Recursos de Dados.
func (h *FinOpsHandler) billedDataRG(ctx context.Context, cluster, appRG, clusterSub string, from, to time.Time) (billedGroup, bool) {
	candidates := finops.DataRGCandidates(cluster, appRG)
	if len(candidates) == 0 {
		return billedGroup{}, false
	}
	armAny, err := azure.NewARMClient(ctx, "")
	if err != nil {
		return billedGroup{}, false
	}
	res, err := h.resolveDataRG(ctx, cluster, armAny, candidates, clusterSub)
	if err != nil || res == nil || res.SubscriptionID == "" {
		return billedGroup{}, false
	}
	cost, err := armAny.ForSubscription(res.SubscriptionID).CostByResourceGroupAndService(ctx, []string{res.ResourceGroup}, from, to)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Str("rg", res.ResourceGroup).Msg("FinOps/BilledCost: custo do RG de dados não consultado")
		return billedGroup{}, false
	}
	return billedGroup{name: res.ResourceGroup, role: "data", byService: cost.ByRG[strings.ToLower(res.ResourceGroup)]}, true
}
