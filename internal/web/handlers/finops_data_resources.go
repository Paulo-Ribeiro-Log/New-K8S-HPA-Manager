package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/finops"
)

const (
	dataUsageCacheTTL     = 30 * time.Minute
	dataUsageScanTimeout  = 3 * time.Minute
	dataUsageDefaultDays  = 14
	dataResourcesTimeout  = 90 * time.Second
	dataUsageMaxWindowDay = 30
)

const (
	dataRGCacheTTL         = time.Hour
	dataRGNegativeCacheTTL = 5 * time.Minute
)

// cachedDataRG guarda o resultado da resolução do RG de dados de um cluster.
type cachedDataRG struct {
	res *finops.DataRGResolution
	err error
	at  time.Time
}

// cachedDataUsage guarda o uso real por resource ID (não por índice: a ordem da listagem pode
// mudar entre chamadas).
type cachedDataUsage struct {
	usage     map[string]finops.DataUtilization
	scannedAt time.Time
}

// GetDataResources godoc
// GET /api/v1/finops/data-resources?cluster=X[&analyze=true&days=14]
//
// Lista e precifica os recursos do Resource Group de DADOS associado ao cluster (VMs de banco,
// discos managed, PostgreSQL/MySQL Flexible Server, Storage/Redis/Cosmos/Service Bus...). Só
// funciona pra clusters AKS cujo RG segue "rg-<nome>-app-<env>" (ver finops.DeriveDataResourceGroup)
// — fora disso responde available=false com o motivo, nunca um erro HTTP.
//
// A coleta é via ARM REST (a mesma da descoberta de discos desatachados): traz de cada disco o
// estado (Attached/Unattached/Reserved), a VM dona, o tier cobrado e a performance provisionada, o
// que permite (a) não cobrar compute de VM desalocada e (b) ofertas de resizing/limpeza que o
// inventário sozinho já sustenta. Com analyze=true, soma uso real do Azure Monitor (CPU/memória de
// VM, CPU/memória/storage de Flexible Server) e as ofertas de resizing que dependem disso.
func (h *FinOpsHandler) GetDataResources(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	analyze := c.Query("analyze") == "true"
	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = dataUsageDefaultDays
	}
	if days > dataUsageMaxWindowDay {
		days = dataUsageMaxWindowDay
	}

	// Só AKS tem essa convenção de RG. Cluster EKS/GKE não tem "RG de dados".
	switch config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster) {
	case config.CloudProviderEKS, config.CloudProviderGKE:
		c.JSON(http.StatusOK, gin.H{"available": false,
			"reason": "Recursos de dados em Resource Group separado só existem para clusters AKS (convenção rg-<nome>-data-<env>)."})
		return
	}

	// O cluster pode existir só no kubeconfig (sem entrada em clusters-config.json): o nome do
	// cluster já basta pra derivar o RG. A config, quando existe, dá o RG de app (fallback de nome)
	// e a subscription do cluster.
	cfg := h.kubeManager.GetClusterConfig(cluster)
	appRG, clusterSub := "", ""
	if cfg != nil {
		appRG = cfg.ResourceGroup
		clusterSub = cfg.SubscriptionID
		if clusterSub == "" {
			clusterSub = cfg.Subscription
		}
	}
	candidates := finops.DataRGCandidates(cluster, appRG)
	if len(candidates) == 0 {
		c.JSON(http.StatusOK, gin.H{"available": false,
			"reason": fmt.Sprintf("O nome do cluster ('%s') não segue a convenção aks*-<nome>-<env> (hlg/prd/...) e não há Resource Group de app registrado — não foi possível derivar o Resource Group de dados (rg-<nome>-data-<env>).", cluster)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), dataResourcesTimeout)
	defer cancel()

	// Token sem subscription fixa: o RG de dados pode estar em outra subscription que a do cluster.
	armAny, err := azure.NewARMClient(ctx, "")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "data_resource_group": candidates[0],
			"tried_resource_groups": candidates,
			"reason":                "Falha ao autenticar no Azure para procurar o Resource Group de dados: " + err.Error()})
		return
	}
	res, err := h.resolveDataRG(ctx, cluster, armAny, candidates, clusterSub)
	if err != nil {
		reason := err.Error()
		if _, notFound := err.(*finops.ErrDataRGNotFound); notFound {
			reason = "Este cluster não tem Resource Group de dados: " + err.Error() +
				". A convenção é rg-<nome do cluster sem prefixo/sufixo>-data-<env>; se o RG de dados deste cluster tem outro nome, ele não é encontrado."
		}
		c.JSON(http.StatusOK, gin.H{"available": false, "data_resource_group": candidates[0],
			"tried_resource_groups": candidates, "reason": reason})
		return
	}
	dataRG := res.ResourceGroup
	arm := armAny.ForSubscription(res.SubscriptionID)

	resources, err := finops.ListDataResourceGroupWith(ctx, arm, dataRG)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "data_resource_group": dataRG,
			"reason": "Falha ao consultar o Resource Group de dados '" + dataRG + "': " + err.Error()})
		return
	}

	rate, _ := h.exchange.Get()
	priced := finops.PriceDataResources(ctx, resources, h.pricer, h.diskPricer, res.SubscriptionID, rate)

	var diskPrice finops.DiskPriceFunc
	if h.diskPricer != nil {
		diskPrice = h.diskPricer.GetDiskPrice
	}
	finops.ApplyInventoryRecommendations(priced, dataRG, diskPrice, rate)

	// Guarda contra *AzurePricer nil embrulhado numa interface (typed-nil) — chamar método nele
	// entraria em pânico dentro das ofertas de resizing.
	var vmSpecs finops.CloudPricer
	if h.pricer != nil {
		vmSpecs = h.pricer
	}

	analyzedAt := time.Time{}
	if analyze {
		usage, scannedAt := h.dataResourceUsage(cluster, days, arm, priced, vmSpecs)
		analyzedAt = scannedAt
		for i := range priced {
			if u, ok := usage[priced[i].ResourceID]; ok {
				finops.ApplyUsageRecommendations(&priced[i], u, dataRG, vmSpecs, finops.DefaultFlexTargetPricer(rate), rate)
			}
		}
	}

	var totalUSD, totalBRL, savingsBRL float64
	var pricedCount, recCount int
	for _, r := range priced {
		totalUSD += r.MonthlyCostUSD
		totalBRL += r.MonthlyCostBRL
		if r.PriceSource == "api" || r.PriceSource == "table" {
			pricedCount++
		}
		for _, rec := range r.Recommendations {
			if rec.Verdict != "info" {
				recCount++
				savingsBRL += rec.MonthlySavingsBRL
			}
		}
	}

	resp := gin.H{
		"available":              true,
		"data_resource_group":    dataRG,
		"resources":              priced,
		"resource_count":         len(priced),
		"priced_count":           pricedCount, // quantos dos resource_count têm MonthlyCostUSD estimado (os demais têm pricing_note explicando por quê)
		"total_monthly_cost_usd": rightsizingRound2(totalUSD),
		"total_monthly_cost_brl": rightsizingRound2(totalBRL),
		"recommendation_count":   recCount,
		// Soma das economias das ofertas (recommended + consider). Ofertas do MESMO recurso podem
		// ser alternativas entre si (ex: dois tamanhos de VM) — trate como teto, não como soma exata.
		"potential_savings_brl": rightsizingRound2(savingsBRL),
		"analyzed":              analyze,
		"analysis_days":         days,
		// Onde o RG foi achado — pode ser uma subscription diferente da do cluster.
		"subscription":            res.SubscriptionName,
		"subscription_id":         res.SubscriptionID,
		"in_cluster_subscription": res.InClusterSubscription,
		"other_subscriptions":     res.OtherSubscriptions,
		"tried_resource_groups":   res.Tried,
	}
	if analyze && !analyzedAt.IsZero() {
		resp["analyzed_at"] = analyzedAt
	}
	c.JSON(http.StatusOK, resp)
}

// resolveDataRG resolve (com cache) em qual subscription está o RG de dados do cluster. A busca
// entre subscriptions custa dezenas de chamadas ao ARM: resultado positivo vale 1h; "não achei" só
// 5 min (pode ter sido criado logo depois).
func (h *FinOpsHandler) resolveDataRG(ctx context.Context, cluster string, arm *azure.ARMClient, candidates []string, clusterSub string) (*finops.DataRGResolution, error) {
	h.dataRGMu.Lock()
	e, ok := h.dataRGCache[cluster]
	h.dataRGMu.Unlock()
	if ok {
		ttl := dataRGCacheTTL
		if e.err != nil {
			ttl = dataRGNegativeCacheTTL
		}
		if time.Since(e.at) < ttl {
			return e.res, e.err
		}
	}

	res, err := finops.ResolveDataResourceGroup(ctx, arm, candidates, clusterSub)
	// Só guarda "não existe" e sucesso — falha de rede/auth não pode virar um "não existe" cacheado.
	if _, notFound := err.(*finops.ErrDataRGNotFound); err == nil || notFound {
		h.dataRGMu.Lock()
		h.dataRGCache[cluster] = cachedDataRG{res: res, err: err, at: time.Now()}
		h.dataRGMu.Unlock()
	}
	if err == nil && !res.InClusterSubscription {
		log.Info().Str("cluster", cluster).Str("data_rg", res.ResourceGroup).Str("subscription", res.SubscriptionName).
			Msg("FinOps/DataResources: RG de dados fica em subscription diferente da do cluster")
	}
	return res, err
}

// dataResourceUsage devolve o uso real por resource ID, do cache (30 min) ou de uma coleta nova.
// singleflight evita duas coletas idênticas em paralelo (duplo clique, duas abas).
func (h *FinOpsHandler) dataResourceUsage(cluster string, days int, arm *azure.ARMClient, resources []finops.AzureDataResource, specs finops.CloudPricer) (map[string]finops.DataUtilization, time.Time) {
	key := fmt.Sprintf("%s|%d", cluster, days)

	h.dataUsageMu.Lock()
	entry, ok := h.dataUsageCache[key]
	h.dataUsageMu.Unlock()
	if ok && time.Since(entry.scannedAt) < dataUsageCacheTTL {
		return entry.usage, entry.scannedAt
	}

	v, _, _ := h.dataUsageSF.Do(key, func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), dataUsageScanTimeout)
		defer cancel()
		byIndex := finops.AnalyzeDataResourceUsage(ctx, arm, resources, days, specs)
		usage := make(map[string]finops.DataUtilization, len(byIndex))
		for i, u := range byIndex {
			usage[resources[i].ResourceID] = u
		}
		e := cachedDataUsage{usage: usage, scannedAt: time.Now()}
		h.dataUsageMu.Lock()
		h.dataUsageCache[key] = e
		h.dataUsageMu.Unlock()
		log.Info().Str("cluster", cluster).Int("days", days).Int("resources_with_usage", len(usage)).Msg("FinOps/DataResources: uso real coletado (Azure Monitor)")
		return e, nil
	})
	e := v.(cachedDataUsage)
	return e.usage, e.scannedAt
}
