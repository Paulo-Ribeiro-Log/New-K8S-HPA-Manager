package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/monitoring/discovery"
)

// deepAnalysisTimeout limita a coleta ao vivo + preços da Deep Analysis.
const deepAnalysisTimeout = 90 * time.Second

// deepThrottleTimeout limita a consulta de throttling ao Prometheus (best-effort).
const deepThrottleTimeout = 45 * time.Second

// deepSKUCatalogWait é quanto a primeira análise de uma região espera a carga do catálogo de SKUs.
const deepSKUCatalogWait = 20 * time.Second

// GetDeepAnalysis godoc
// GET /api/v1/finops/deep-analysis?cluster=X&pool=Y[&headroom=0.8][&format=markdown]
//
// Deep Analysis de um node pool (ver FINOPS-DEEP-ANALYSIS-PLAN.md): coleta AO VIVO só os nodes e pods
// do pool, combina com o histórico do último relatório FinOps em cache (sem re-scan) e devolve o
// diagnóstico de alocação, DaemonSets, workloads e a simulação de nodes por SKU. Só leitura.
func (h *FinOpsHandler) GetDeepAnalysis(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	pool := strings.TrimSpace(c.Query("pool"))
	if cluster == "" || pool == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros 'cluster' e 'pool' são obrigatórios"})
		return
	}
	headroom := finops.DeepDefaultHeadroom
	if v := c.Query("headroom"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0.3 || f > 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "headroom deve estar entre 0.3 e 1.0"})
			return
		}
		headroom = f
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), deepAnalysisTimeout)
	defer cancel()
	status, body := h.doDeepAnalysis(ctx, cluster, pool, headroom)
	if a, ok := body.(finops.PoolDeepAnalysis); ok && c.Query("format") == "markdown" {
		name := fmt.Sprintf("deep-analysis-%s-%s-%s.md", deepFileSafe(cluster), deepFileSafe(pool), a.GeneratedAt.Format("20060102-1504"))
		c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(finops.RenderDeepAnalysisMarkdown(a)))
		return
	}
	c.JSON(status, body)
}

// deepFileSafe deixa só caracteres seguros para nome de arquivo (contexts EKS são ARNs com / e :).
func deepFileSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, s)
}

func (h *FinOpsHandler) doDeepAnalysis(ctx context.Context, cluster, pool string, headroom float64) (int, interface{}) {
	start := time.Now()
	k8sClient, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return http.StatusInternalServerError, gin.H{"error": "Falha ao conectar ao cluster: " + err.Error()}
	}
	metricsClient, mErr := h.kubeManager.GetMetricsClient(cluster)
	if mErr != nil {
		metricsClient = nil
	}

	col, err := finops.CollectPoolDeepInput(ctx, k8sClient, metricsClient, pool)
	if err != nil {
		if errors.Is(err, finops.ErrDeepPoolNotFound) {
			return http.StatusNotFound, gin.H{"error": err.Error()}
		}
		return http.StatusInternalServerError, gin.H{"error": "Falha na coleta do pool: " + err.Error()}
	}

	warnings := append([]string{}, col.Warnings...)
	pricer := h.pricerForCluster(cluster)
	spec := deepCurrentSpec(col, pricer)

	var candidates []finops.DeepSKUSpec
	if strings.HasPrefix(strings.ToLower(spec.VMSize), "standard_") {
		candidates = finops.DeepCandidateSKUs(spec.VMSize, spec.VCPU)
	} else {
		warnings = append(warnings, "Simulação com outras SKUs disponível só para AKS por enquanto: apenas a SKU atual foi simulada.")
	}
	if col.Priority == "spot" {
		warnings = append(warnings, "Pool Spot: custos calculados com preço de tabela (sob demanda); o custo real do Spot é menor.")
	}

	prices := deepFetchPrices(pricer, append([]finops.DeepSKUSpec{spec}, candidates...))

	var rate float64
	if h.exchange != nil {
		rate, _ = h.exchange.Get()
	}
	if rate <= 0 {
		warnings = append(warnings, "Câmbio USD→BRL indisponível: custos em R$ ficam zerados.")
	}

	in := finops.DeepAnalysisInput{
		Cluster:      cluster,
		Pool:         pool,
		VMSize:       spec.VMSize,
		Region:       col.Region,
		Priority:     col.Priority,
		OSDisk:       col.OSDisk,
		CurrentSpec:  spec,
		Candidates:   candidates,
		Prices:       prices,
		ExchangeRate: rate,
		Nodes:        col.Nodes,
		Pods:         col.Pods,
		HPAs:         col.HPAs,
		MetricsLive:  col.MetricsLive,
		Headroom:     headroom,
		Warnings:     warnings,
	}
	h.attachDeepHistory(cluster, &in)
	h.attachDeepCoverage(cluster, pool, spec, candidates, &in)
	if len(candidates) > 0 {
		h.attachSKUCatalog(col.Region, &in)
	}
	attachDeepThrottling(ctx, cluster, col.Namespaces, &in)

	result := finops.BuildPoolDeepAnalysis(in)
	log.Info().Str("cluster", cluster).Str("pool", pool).Int("nodes", len(col.Nodes)).Int("pods", len(col.Pods)).
		Bool("history", result.History.Available).Dur("elapsed", time.Since(start)).Msg("FinOps: Deep Analysis gerada")
	return http.StatusOK, result
}

// deepCurrentSpec resolve a spec da SKU atual: tabela da Deep Analysis → pricer → capacidade medida.
func deepCurrentSpec(col *finops.DeepCollected, pricer finops.CloudPricer) finops.DeepSKUSpec {
	if s, ok := finops.DeepSpecFor(col.VMSize); ok {
		return s
	}
	spec := finops.DeepSKUSpec{VMSize: col.VMSize, SMT: true}
	if pricer != nil {
		spec.VCPU, spec.MemGB = pricer.GetVMSpecs(col.VMSize)
	}
	if (spec.VCPU == 0 || spec.MemGB == 0) && len(col.Nodes) > 0 {
		spec.VCPU = int(math.Round(col.Nodes[0].CPUCapMillis / 1000))
		spec.MemGB = int(math.Round(col.Nodes[0].MemCapMi / 1024))
	}
	return spec
}

// deepFetchPrices busca os preços das SKUs em paralelo (no máximo 6 ao mesmo tempo). SKUs sem preço
// ficam fora do mapa — a simulação as marca como "preço indisponível".
func deepFetchPrices(pricer finops.CloudPricer, specs []finops.DeepSKUSpec) map[string]finops.DeepPrice {
	prices := make(map[string]finops.DeepPrice, len(specs))
	if pricer == nil {
		return prices
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, s := range specs {
		sku := s.VMSize
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p, src, err := pricer.GetPrice(sku)
			if err != nil || p <= 0 {
				return
			}
			mu.Lock()
			prices[strings.ToLower(sku)] = finops.DeepPrice{USDHour: p, Source: src}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return prices
}

// attachDeepHistory anexa ao input o histórico do último relatório FinOps em cache (best-effort).
func (h *FinOpsHandler) attachDeepHistory(cluster string, in *finops.DeepAnalysisInput) {
	if h.reportCacheStore == nil {
		return
	}
	raw, generatedAt, found, err := h.reportCacheStore.Get(cluster)
	if err != nil || !found {
		return
	}
	var rep finops.FinOpsReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		in.Warnings = append(in.Warnings, "Relatório FinOps em cache ilegível: histórico ignorado.")
		return
	}
	in.History = finops.DeepHistoryFromReport(&rep)
	in.HistoryWindowDays = rep.WindowDays
	gen := generatedAt
	in.HistoryGeneratedAt = &gen
}

// attachDeepThrottling consulta o throttling de CPU dos namespaces do pool no Prometheus do cluster
// (best-effort: sem Prometheus ou com falha, vira aviso e a análise segue sem throttling).
func attachDeepThrottling(ctx context.Context, cluster string, namespaces []string, in *finops.DeepAnalysisInput) {
	promURL := discovery.GetPrometheusURL(cluster)
	if promURL == "" {
		in.Warnings = append(in.Warnings, "Prometheus não encontrado para o cluster: throttling de CPU não foi coletado.")
		return
	}
	window := in.HistoryWindowDays
	enricher, err := finops.NewPrometheusEnricher(promURL, window, discovery.RequiresGCPAuth(cluster))
	if err != nil {
		in.Warnings = append(in.Warnings, "Prometheus: "+err.Error())
		return
	}
	tctx, cancel := context.WithTimeout(ctx, deepThrottleTimeout)
	defer cancel()
	th, days, err := enricher.CPUThrottlingByPod(tctx, namespaces, window)
	if err != nil {
		in.Warnings = append(in.Warnings, "Throttling de CPU indisponível: "+err.Error())
		return
	}
	in.Throttling, in.ThrottleWindowDays = th, days
}

// attachSKUCatalog anexa o catálogo de capacidades de SKU da região (cache; carga em segundo plano).
func (h *FinOpsHandler) attachSKUCatalog(region string, in *finops.DeepAnalysisInput) {
	if h.skuCatalog == nil || region == "" {
		in.SKUCatalogStatus = finops.SKUCatalogUnavailable
		return
	}
	cat, status, lastErr := h.skuCatalog.GetWait(region, deepSKUCatalogWait)
	in.SKUCatalogStatus = status
	if cat != nil {
		in.SKUCaps = cat.SKUs
		at := cat.FetchedAt
		in.SKUCatalogFetchedAt = &at
	}
	switch {
	case status == finops.SKUCatalogLoading && lastErr != "":
		in.Warnings = append(in.Warnings, "Catálogo de SKUs da região indisponível ("+lastErr+"): disponibilidade, disco efêmero e zonas das SKUs não foram verificados.")
	case status == finops.SKUCatalogLoading:
		in.Warnings = append(in.Warnings, "Catálogo de SKUs da região ainda carregando em segundo plano: clique em Atualizar em instantes para ver disponibilidade, disco efêmero e zonas de cada SKU.")
	case status == finops.SKUCatalogUnavailable:
		in.Warnings = append(in.Warnings, "Região do pool desconhecida: disponibilidade das SKUs não foi verificada.")
	}
}

// attachDeepCoverage anexa a cobertura de reserva/Savings Plan do pool (gravada pelo FinOps a partir do
// Cost Management) e os SKUs que já rodam cobertos na frota. Sem cobertura gravada para o cluster
// (AKS), dispara em segundo plano o mesmo refresh automático do scan (no máximo um por cluster).
func (h *FinOpsHandler) attachDeepCoverage(cluster, pool string, spec finops.DeepSKUSpec, candidates []finops.DeepSKUSpec, in *finops.DeepAnalysisInput) {
	if !strings.HasPrefix(strings.ToLower(spec.VMSize), "standard_") {
		return
	}
	covByPool := h.poolCoverage(cluster)
	in.Coverage = covByPool[strings.ToLower(pool)]
	ix := h.skuCoverageIndex()
	if !ix.Empty() {
		in.SKUHints = map[string]*finops.SKUCoverageHint{}
		for _, s := range append([]finops.DeepSKUSpec{spec}, candidates...) {
			if hint := ix.Hint(s.VMSize); hint != nil {
				in.SKUHints[strings.ToLower(s.VMSize)] = hint
			}
		}
	}
	if in.Coverage != nil {
		return
	}
	if len(covByPool) == 0 {
		in.Warnings = append(in.Warnings, "Cobertura de reserva/Savings Plan ainda não consultada para este cluster: os custos usam preço de tabela. A consulta ao Cost Management foi disparada em segundo plano — clique em Atualizar em ~1 minuto.")
		go func() {
			_, _, _ = h.deepCoverageSF.Do(cluster, func() (interface{}, error) {
				h.autoRefreshCoverage(context.Background(), cluster)
				return nil, nil
			})
		}()
		return
	}
	in.Warnings = append(in.Warnings, "Sem cobertura de reserva/Savings Plan para este pool no Cost Management: os custos usam preço de tabela.")
}
