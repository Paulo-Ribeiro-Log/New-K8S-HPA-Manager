package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/finops"
)

// Showback (FINOPS-REVISAO-PLAN.md, Fase 3): custo por namespace e dono, por mês. Cada mês guarda a
// foto do último scan dele (finops_showback_month, no banco do cache de relatórios).

var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// saveShowback grava o showback do relatório no mês em que ele foi gerado. Best-effort.
func (h *FinOpsHandler) saveShowback(cluster string, report *finops.FinOpsReport) {
	if h.reportCacheStore == nil || report == nil {
		return
	}
	rows := finops.BuildShowback(report)
	for i := range rows {
		rows[i].Cluster = cluster
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return
	}
	month := report.GeneratedAt.Format("2006-01")
	if err := h.reportCacheStore.SaveShowback(cluster, month, raw, report.GeneratedAt); err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Showback: falha ao gravar o mês (best-effort)")
	}
}

// GetShowback godoc
// GET /api/v1/finops/showback?month=YYYY-MM[&cluster=X] — sem cluster, todos os clusters.
func (h *FinOpsHandler) GetShowback(c *gin.Context) {
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))
	if !monthRe.MatchString(month) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "'month' deve ser AAAA-MM"})
		return
	}
	cluster := c.Query("cluster")
	if h.reportCacheStore == nil {
		c.JSON(http.StatusOK, gin.H{"month": month, "months": []string{}, "rows": []finops.ShowbackRow{}})
		return
	}
	// O mês corrente sai dos relatórios gravados (inclui clusters analisados antes desta função
	// existir); os anteriores, da foto gravada.
	if month == time.Now().Format("2006-01") {
		if cached, err := h.reportCacheStore.ListAll(); err == nil {
			for _, row := range cached {
				if row.GeneratedAt.Format("2006-01") != month || (cluster != "" && row.Cluster != cluster) {
					continue
				}
				if r, err := h.lastReport(row.Cluster); err == nil && r != nil {
					h.saveShowback(row.Cluster, r)
				}
			}
		}
	}
	stored, err := h.reportCacheStore.ListShowback(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler o showback: " + err.Error()})
		return
	}
	rows := []finops.ShowbackRow{}
	reportAt := map[string]time.Time{}
	for _, m := range stored {
		if cluster != "" && m.Cluster != cluster {
			continue
		}
		var part []finops.ShowbackRow
		if json.Unmarshal(m.RowsJSON, &part) == nil {
			rows = append(rows, part...)
			reportAt[m.Cluster] = m.ReportAt
		}
	}
	months, _ := h.reportCacheStore.ShowbackMonths()
	if len(months) == 0 || months[0] != time.Now().Format("2006-01") {
		months = append([]string{time.Now().Format("2006-01")}, months...)
	}
	c.JSON(http.StatusOK, gin.H{"month": month, "months": months, "rows": rows, "report_at": reportAt})
}
