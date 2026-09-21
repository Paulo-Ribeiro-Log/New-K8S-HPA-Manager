package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/storage"
)

// Cobertura de reserva / Savings Plan dos node pools (aba Rightsizing). Lê o Cost Management (só
// leitura) e grava o resultado; o aviso por alternativa de SKU é montado na LEITURA
// (nodePoolTierResponses), então atualizar a cobertura não exige reanalisar o cluster.

const (
	coverageDefaultDays = 30
	coverageMinDays     = 7
	coverageMaxDays     = 90
	// A API de custos limita chamadas e PostJSON repete em 429 (até 30 s de espera cada): o teto
	// precisa comportar isso (agora são 2 consultas: por pool e índice de SKUs).
	coverageTimeout = 240 * time.Second
	// Teto do refresh automático dentro do scan — o scan não pode ficar esperando a API de custos.
	coverageAutoTimeout = 60 * time.Second
)

type coverageRequest struct {
	Cluster string `json:"cluster"`
	Days    int    `json:"days"`
}

// coveragePoolSummary é uma linha do resultado da atualização (pool → participações).
type coveragePoolSummary struct {
	NodePool string `json:"node_pool"`
	*finops.PoolCoverage
}

// coverageErr carrega o status HTTP junto com a mensagem, pra o handler responder certo e o refresh
// automático do scan só registrar e seguir.
type coverageErr struct {
	status int
	msg    string
}

func (e *coverageErr) Error() string { return e.msg }

// coverageWindow devolve a janela [from, to] de N dias terminando ONTEM — o dia corrente ainda está
// incompleto no Cost Management.
func coverageWindow(days int) (from, to time.Time) {
	now := time.Now().UTC()
	to = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-time.Second)
	from = to.AddDate(0, 0, -days).Add(time.Second)
	return from, to
}

// coverageClient valida que o cluster é AKS e devolve o cliente ARM já apontando para a subscription
// dele, com a config do cluster.
func (h *FinOpsHandler) coverageClient(ctx context.Context, cluster string) (*azure.ARMClient, *config.ClusterConfig, error) {
	if provider := config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster); provider != config.CloudProviderAKS {
		return nil, nil, &coverageErr{http.StatusBadRequest, "a cobertura de reserva/Savings Plan só está disponível para clusters AKS (Azure)"}
	}
	cfg := h.kubeManager.GetClusterConfig(cluster)
	if cfg == nil {
		return nil, nil, &coverageErr{http.StatusNotFound, fmt.Sprintf("cluster '%s' não encontrado em clusters-config.json — rode o autodiscover", cluster)}
	}
	sub := cfg.SubscriptionID
	if sub == "" {
		sub = cfg.Subscription
	}
	arm, err := azure.NewARMClient(ctx, sub)
	if err != nil {
		return nil, nil, &coverageErr{http.StatusBadGateway, "Falha ao autenticar no Azure: " + err.Error()}
	}
	return arm, cfg, nil
}

func costQueryErr(what string, err error) error {
	msg := "Falha ao consultar o Cost Management (" + what + "): " + err.Error()
	if ae, ok := err.(*azure.ARMError); ok && ae.Status == http.StatusForbidden {
		msg = "Sem permissão de leitura de custos (Cost Management Reader) na subscription deste cluster: " + err.Error()
	}
	return &coverageErr{http.StatusBadGateway, msg}
}

// refreshPoolCoverage consulta e grava a cobertura POR NODE POOL do cluster.
func (h *FinOpsHandler) refreshPoolCoverage(ctx context.Context, arm *azure.ARMClient, cfg *config.ClusterConfig, cluster string, days int) (nodeRG string, from, to time.Time, summary []coveragePoolSummary, err error) {
	nodeRG, gerr := arm.NodeResourceGroup(ctx, cfg.ResourceGroup, cfg.Name)
	if gerr != nil {
		return "", from, to, nil, &coverageErr{http.StatusBadGateway, "Falha ao descobrir o resource group de nodes do AKS: " + gerr.Error()}
	}
	from, to = coverageWindow(days)
	cost, qerr := arm.PoolCostByPricingModel(ctx, nodeRG, from, to)
	if qerr != nil {
		return "", from, to, nil, costQueryErr("cobertura por pool", qerr)
	}

	fetchedAt := time.Now()
	var rows []storage.PoolPricingCoverage
	summary = []coveragePoolSummary{}
	pools := make([]string, 0, len(cost.ByPool))
	for p := range cost.ByPool {
		pools = append(pools, p)
	}
	sort.Strings(pools)
	for _, pool := range pools {
		for model, v := range cost.ByPool[pool] {
			rows = append(rows, storage.PoolPricingCoverage{Cluster: cluster, NodePool: pool, Model: model, Cost: v,
				Currency: cost.Currency, WindowDays: days, FetchedAt: fetchedAt})
		}
		if cov := finops.BuildPoolCoverage(cost.ByPool[pool], cost.Currency, days, fetchedAt); cov != nil {
			summary = append(summary, coveragePoolSummary{NodePool: pool, PoolCoverage: cov})
		}
	}
	if serr := h.rightsizingStore.ReplacePoolPricingCoverage(cluster, rows); serr != nil {
		return "", from, to, nil, &coverageErr{http.StatusInternalServerError, "cobertura consultada, mas falhou ao gravar: " + serr.Error()}
	}
	return nodeRG, from, to, summary, nil
}

// refreshSKUIndex consulta e grava o índice de SKUs cobertos por reserva/Savings Plan da subscription
// (uso observado). Devolve quantos SKUs ficaram no índice.
func (h *FinOpsHandler) refreshSKUIndex(ctx context.Context, arm *azure.ARMClient, days int) (int, error) {
	from, to := coverageWindow(days)
	cost, err := arm.CoveredSKUs(ctx, from, to)
	if err != nil {
		return 0, costQueryErr("SKUs cobertos", err)
	}
	fetchedAt := time.Now()
	var rows []storage.SKUPricingCoverage
	for sku, byModel := range cost.BySKU {
		for model, v := range byModel {
			rows = append(rows, storage.SKUPricingCoverage{SubscriptionID: arm.SubscriptionID, SKU: sku, Model: model,
				Cost: v, Currency: cost.Currency, WindowDays: days, FetchedAt: fetchedAt})
		}
	}
	if err := h.rightsizingStore.ReplaceSKUPricingCoverage(arm.SubscriptionID, rows, fetchedAt); err != nil {
		return 0, &coverageErr{http.StatusInternalServerError, "índice consultado, mas falhou ao gravar: " + err.Error()}
	}
	return len(cost.BySKU), nil
}

// RefreshPricingCoverage godoc
// POST /api/v1/finops/pricing-coverage/refresh
//
// Consulta o custo AMORTIZADO no Cost Management (últimos N dias, até ontem) e grava: a cobertura de
// reserva/Savings Plan por node pool do cluster e o índice de SKUs cobertos da subscription. Só AKS.
func (h *FinOpsHandler) RefreshPricingCoverage(c *gin.Context) {
	var req coverageRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Cluster) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: 'cluster' é obrigatório"})
		return
	}
	if h.rightsizingStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de rightsizing indisponível (a cobertura não pode ser gravada)"})
		return
	}
	days := req.Days
	if days <= 0 {
		days = coverageDefaultDays
	}
	if days < coverageMinDays {
		days = coverageMinDays
	}
	if days > coverageMaxDays {
		days = coverageMaxDays
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), coverageTimeout)
	defer cancel()

	respondErr := func(err error) {
		status := http.StatusBadGateway
		if ce, ok := err.(*coverageErr); ok {
			status = ce.status
		}
		c.JSON(status, gin.H{"error": err.Error()})
	}
	arm, cfg, err := h.coverageClient(ctx, req.Cluster)
	if err != nil {
		respondErr(err)
		return
	}

	log.Info().Str("cluster", req.Cluster).Str("user", c.GetString("user_email")).Int("days", days).
		Msg("FinOps/Coverage: consultando Cost Management")
	nodeRG, from, to, summary, err := h.refreshPoolCoverage(ctx, arm, cfg, req.Cluster, days)
	if err != nil {
		respondErr(err)
		return
	}
	// O índice de SKUs é complementar: se falhar, a cobertura por pool já gravada continua valendo.
	skus, ierr := h.refreshSKUIndex(ctx, arm, days)
	resp := gin.H{
		"cluster": req.Cluster, "node_resource_group": nodeRG, "window_days": days,
		"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "pools": summary,
		"sku_index": gin.H{"skus": skus},
	}
	if ierr != nil {
		resp["sku_index"] = gin.H{"skus": 0, "error": ierr.Error()}
	}
	c.JSON(http.StatusOK, resp)
}

// coverageRefreshTTL: o refresh automático do scan não repete a consulta se a anterior é mais nova
// que isso (o Cost Management limita chamadas e o dado só muda ~1x por dia).
const coverageRefreshTTL = 6 * time.Hour

// autoRefreshCoverage é o refresh do scan: atualiza a cobertura do cluster e o índice de SKUs da
// subscription quando estão velhos. Best-effort e limitado no tempo — nunca falha nem atrasa o scan
// além do teto: qualquer erro só é registrado (ex: sem permissão de custos, VPN, 429).
func (h *FinOpsHandler) autoRefreshCoverage(parent context.Context, cluster string) {
	if h.rightsizingStore == nil || h.kubeManager == nil {
		return
	}
	if config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster) != config.CloudProviderAKS {
		return
	}
	poolStale := true
	if rows, err := h.rightsizingStore.ListPoolPricingCoverage(cluster); err == nil && len(rows) > 0 {
		poolStale = time.Since(rows[0].FetchedAt) > coverageRefreshTTL
	}
	cfg := h.kubeManager.GetClusterConfig(cluster)
	if cfg == nil {
		return
	}
	sub := cfg.SubscriptionID
	if sub == "" {
		sub = cfg.Subscription
	}
	// Só descarta a consulta do índice quando a subscription já tem ID resolvido e o retrato é fresco.
	skuStale := true
	if cfg.SubscriptionID != "" {
		skuStale = time.Since(h.rightsizingStore.SKUPricingCoverageFetchedAt(cfg.SubscriptionID)) > coverageRefreshTTL
	}
	if !poolStale && !skuStale {
		return
	}

	ctx, cancel := context.WithTimeout(parent, coverageAutoTimeout)
	defer cancel()
	arm, cfg, err := h.coverageClient(ctx, cluster)
	if err != nil {
		log.Debug().Err(err).Str("cluster", cluster).Str("sub", sub).Msg("FinOps/Coverage: refresh automático ignorado")
		return
	}
	if poolStale {
		if _, _, _, _, err := h.refreshPoolCoverage(ctx, arm, cfg, cluster, coverageDefaultDays); err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Coverage: refresh automático da cobertura por pool falhou (o scan segue)")
		}
	}
	if skuStale || time.Since(h.rightsizingStore.SKUPricingCoverageFetchedAt(arm.SubscriptionID)) > coverageRefreshTTL {
		if _, err := h.refreshSKUIndex(ctx, arm, coverageDefaultDays); err != nil {
			log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Coverage: refresh automático do índice de SKUs falhou (o scan segue)")
		}
	}
}

// skuCoverageIndex carrega o índice de SKUs cobertos gravado (todas as subscriptions). nil-safe: sem
// store ou sem dados devolve um índice vazio, e nada muda nas alternativas.
func (h *FinOpsHandler) skuCoverageIndex() finops.SKUCoverageIndex {
	if h.rightsizingStore == nil {
		return finops.SKUCoverageIndex{}
	}
	rows, err := h.rightsizingStore.ListSKUPricingCoverage()
	if err != nil {
		log.Warn().Err(err).Msg("FinOps/Coverage: falha ao ler o índice de SKUs — omitido")
		return finops.SKUCoverageIndex{}
	}
	return finops.BuildSKUCoverageIndex(rows)
}

// poolCoverage carrega a cobertura gravada do cluster, por node pool (chave em minúsculas — o
// Cost Management devolve os IDs em minúsculas e o AKS exige nome de pool em minúsculas).
// nil-safe: sem store, sem consulta ou erro de leitura devolve mapa vazio (a resposta de rightsizing
// simplesmente não traz cobertura).
func (h *FinOpsHandler) poolCoverage(cluster string) map[string]*finops.PoolCoverage {
	out := map[string]*finops.PoolCoverage{}
	if h.rightsizingStore == nil {
		return out
	}
	rows, err := h.rightsizingStore.ListPoolPricingCoverage(cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Coverage: falha ao ler cobertura — omitida")
		return out
	}
	type acc struct {
		cost       map[string]float64
		currency   string
		windowDays int
		fetchedAt  time.Time
	}
	by := map[string]*acc{}
	for _, r := range rows {
		k := strings.ToLower(r.NodePool)
		a := by[k]
		if a == nil {
			a = &acc{cost: map[string]float64{}, currency: r.Currency, windowDays: r.WindowDays, fetchedAt: r.FetchedAt}
			by[k] = a
		}
		a.cost[r.Model] += r.Cost
	}
	for pool, a := range by {
		if cov := finops.BuildPoolCoverage(a.cost, a.currency, a.windowDays, a.fetchedAt); cov != nil {
			out[pool] = cov
		}
	}
	return out
}
