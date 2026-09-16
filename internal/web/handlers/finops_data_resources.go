package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/finops"
)

// GetDataResources godoc
// GET /api/v1/finops/data-resources?cluster=X
//
// Lista e precifica (best-effort) os recursos do Resource Group de DADOS associado ao cluster —
// pedido explícito do usuário: "o resource group de dados... também impacta custos de cloud...
// estamos analisando sempre apenas os resource groups de app". Só funciona pra clusters AKS cujo
// Resource Group segue a convenção confirmada "rg-<nome>-app-<env>" (ver
// finops.DeriveDataResourceGroup) — sem isso (GKE/EKS, ou AKS fora dessa convenção), responde
// available=false com o motivo, nunca um erro HTTP: é uma feature opt-in por convenção de nome,
// não algo que todo cluster deveria ter.
//
// Cobertura de tipos validada ao vivo contra um RG de dados real (ver comentário de
// finops.AzureDataResource) — inclui VMs self-hosted de banco, discos managed, Azure Database for
// PostgreSQL/MySQL Flexible Server, além dos PaaS originalmente cobertos (SQL, Storage, Redis,
// Cosmos DB, Service Bus/Event Hub).
func (h *FinOpsHandler) GetDataResources(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}

	cfg := h.kubeManager.GetClusterConfig(cluster)
	if cfg == nil || cfg.ResourceGroup == "" {
		c.JSON(http.StatusOK, gin.H{
			"available": false,
			"reason":    "Cluster não encontrado na configuração AKS, ou sem Resource Group associado — este recurso só está disponível para clusters AKS.",
		})
		return
	}

	dataRG, ok := finops.DeriveDataResourceGroup(cfg.ResourceGroup)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"available": false,
			"reason":    fmt.Sprintf("O Resource Group deste cluster ('%s') não segue a convenção 'rg-<nome>-app-<env>' — não foi possível derivar o Resource Group de dados correspondente.", cfg.ResourceGroup),
		})
		return
	}

	resources, err := finops.ListDataResourceGroup(c.Request.Context(), dataRG, cfg.Subscription)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"available":           false,
			"data_resource_group": dataRG,
			"reason":              "Falha ao consultar o Resource Group de dados '" + dataRG + "': " + err.Error(),
		})
		return
	}

	rate, _ := h.exchange.Get()
	priced := finops.PriceDataResources(c.Request.Context(), resources, h.pricer, h.diskPricer, cfg.Subscription, rate)

	var totalUSD, totalBRL float64
	var pricedCount int
	for _, r := range priced {
		totalUSD += r.MonthlyCostUSD
		totalBRL += r.MonthlyCostBRL
		if r.PriceSource == "api" {
			pricedCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"available":              true,
		"data_resource_group":    dataRG,
		"resources":              priced,
		"resource_count":         len(priced),
		"priced_count":           pricedCount, // quantos dos resource_count têm MonthlyCostUSD estimado (os demais têm pricing_note explicando por quê)
		"total_monthly_cost_usd": rightsizingRound2(totalUSD),
		"total_monthly_cost_brl": rightsizingRound2(totalBRL),
	})
}
