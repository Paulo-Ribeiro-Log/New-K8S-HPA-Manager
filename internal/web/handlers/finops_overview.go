package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/finops"
)

// Visão multi-cluster (FINOPS-REVISAO-PLAN.md, Fase 3): ranking de custo e desperdício a partir do
// último relatório gravado de cada cluster (finops-report-cache.db), sem novo scan. Cobertura e
// isenções são reaplicadas como em GetLastReport.

// clusterJourneyRe: "akspriv-oferta-prd-admin" → "oferta" (fallback quando o cluster não tem a tag
// de jornada em clusters-config.json).
var clusterJourneyRe = regexp.MustCompile(`(?i)^aks[a-z]*-(.+?)-(hlg|prd|hml|dev|sit|stg|uat|qa)(?:-admin)?$`)

func (h *FinOpsHandler) journeyOf(cluster string) string {
	if h.kubeManager != nil {
		if cfg := h.kubeManager.GetClusterConfig(cluster); cfg != nil {
			if j := strings.TrimSpace(cfg.Journey()); j != "" {
				return j
			}
		}
	}
	if m := clusterJourneyRe.FindStringSubmatch(cluster); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// GetOverview godoc
// GET /api/v1/finops/overview — um resumo por cluster já analisado, do maior custo para o menor.
func (h *FinOpsHandler) GetOverview(c *gin.Context) {
	if h.reportCacheStore == nil {
		c.JSON(http.StatusOK, gin.H{"clusters": []finops.ClusterOverview{}})
		return
	}
	rows, err := h.reportCacheStore.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler os relatórios gravados: " + err.Error()})
		return
	}
	out := make([]finops.ClusterOverview, 0, len(rows))
	for _, row := range rows {
		var report finops.FinOpsReport
		if err := json.Unmarshal(row.ReportJSON, &report); err != nil {
			log.Warn().Err(err).Str("cluster", row.Cluster).Msg("FinOps/Overview: relatório gravado ilegível — ignorado")
			continue
		}
		if report.Cluster == "" {
			report.Cluster = row.Cluster
		}
		finops.AttachPoolCoverage(report.NodePools, h.poolCoverage(row.Cluster))
		h.applyExemptions(row.Cluster, &report)
		o := finops.SummarizeCluster(&report)
		o.Cluster = row.Cluster
		o.Journey = h.journeyOf(row.Cluster)
		o.Environment = finops.EnvironmentOf(row.Cluster)
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalBRL > out[j].TotalBRL })
	c.JSON(http.StatusOK, gin.H{"clusters": out})
}
