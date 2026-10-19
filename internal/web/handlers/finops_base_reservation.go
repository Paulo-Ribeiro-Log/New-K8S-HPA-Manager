package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/finops"
)

// Reserva para a carga base (FINOPS-REVISAO-PLAN.md, Fase 3) — só leitura, sem novo scan.

// GetBaseReservations godoc
// GET /api/v1/finops/base-reservations?cluster=X — por pool: carga base (min do autoscaler ou
// tamanho do pool fixo), parte já coberta e economia de reserva/Savings Plan de 1 e 3 anos.
func (h *FinOpsHandler) GetBaseReservations(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if config.DetectCloudProvider(h.kubeManager.GetServerURL(cluster), cluster) != config.CloudProviderAKS {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "A sugestão usa os preços de reserva da Azure — disponível só para AKS."})
		return
	}
	report, err := h.lastReport(cluster)
	if err != nil || report == nil {
		c.JSON(http.StatusOK, gin.H{"available": false, "reason": "O cluster não tem relatório gravado — rode Analisar."})
		return
	}
	scale := map[string]finops.PoolScale{}
	if h.rightsizingStore != nil {
		if tiers, err := h.rightsizingStore.GetNodePoolTierSuggestions(cluster); err == nil {
			for _, t := range tiers {
				scale[strings.ToLower(t.NodePool)] = finops.PoolScale{Min: t.MinNodeCount, Autoscaling: t.AutoscalingEnabled}
			}
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	prices := map[string]finops.CommitmentPrices{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[string]bool{}
	for _, p := range report.NodePools {
		if seen[p.VMSize] || p.VMSize == "" {
			continue
		}
		seen[p.VMSize] = true
		wg.Add(1)
		go func(sku string) {
			defer wg.Done()
			pr, err := finops.FetchCommitmentPrices(ctx, sku, "")
			if err != nil {
				log.Warn().Err(err).Str("sku", sku).Msg("FinOps/BaseReservation: sem preço de compromisso")
				return
			}
			mu.Lock()
			prices[pr.Key(sku)] = pr
			mu.Unlock()
		}(p.VMSize)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{
		"available":     true,
		"exchange_rate": report.ExchangeRate,
		"report_at":     report.GeneratedAt,
		"pools":         finops.SuggestBaseReservations(report.NodePools, report.Workloads, scale, prices, report.ExchangeRate),
	})
}
