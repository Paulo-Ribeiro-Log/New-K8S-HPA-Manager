package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/storage"
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
	at := report.GeneratedAt.Local()
	if err := h.reportCacheStore.SaveShowback(cluster, at.Format("2006-01"), raw, report.GeneratedAt); err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Showback: falha ao gravar o mês (best-effort)")
	}
	if err := h.reportCacheStore.SaveShowbackDay(cluster, at.Format("2006-01-02"), raw, report.GeneratedAt); err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Showback: falha ao gravar o dia (best-effort)")
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
	days, _ := h.reportCacheStore.ListShowbackDays(month)

	// Ritmo mensal = foto do último scan do mês; acumulado = soma dia a dia (AccrueShowback).
	rateRows := []finops.ShowbackRow{}
	reportAt := map[string]time.Time{}
	snaps := map[string][]finops.ShowbackSnapshot{}
	for _, m := range stored {
		if cluster != "" && m.Cluster != cluster {
			continue
		}
		var part []finops.ShowbackRow
		if json.Unmarshal(m.RowsJSON, &part) == nil {
			rateRows = append(rateRows, part...)
			reportAt[m.Cluster] = m.ReportAt
			if !hasDays(days, m.Cluster) { // mês gravado antes da foto diária existir
				d := m.ReportAt.Local()
				snaps[m.Cluster] = []finops.ShowbackSnapshot{{Day: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local), Rows: part}}
			}
		}
	}
	for _, d := range days {
		if cluster != "" && d.Cluster != cluster {
			continue
		}
		var part []finops.ShowbackRow
		day, err := time.ParseInLocation("2006-01-02", d.Day, time.Local)
		if err != nil || json.Unmarshal(d.RowsJSON, &part) != nil {
			continue
		}
		snaps[d.Cluster] = append(snaps[d.Cluster], finops.ShowbackSnapshot{Day: day, Rows: part})
	}
	monthStart, _ := time.ParseInLocation("2006-01", month, time.Local)
	until := monthStart.AddDate(0, 1, -1)
	if now := time.Now(); until.After(now) {
		until = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	}
	accrued := []finops.ShowbackRow{}
	coverage := map[string]gin.H{}
	for cl, list := range snaps {
		rows, covered := finops.AccrueShowback(list, monthStart, until)
		accrued = append(accrued, rows...)
		coverage[cl] = gin.H{"days": covered, "scans": len(list), "first_day": list[0].Day.Format("2006-01-02")}
	}
	periodDays := int(until.Sub(monthStart).Hours()/24) + 1
	months, _ := h.reportCacheStore.ShowbackMonths()
	if len(months) == 0 || months[0] != time.Now().Format("2006-01") {
		months = append([]string{time.Now().Format("2006-01")}, months...)
	}
	c.JSON(http.StatusOK, gin.H{
		"month": month, "months": months, "rows": rateRows, "accrued_rows": accrued,
		"report_at": reportAt, "coverage": coverage, "period_days": periodDays,
	})
}

func hasDays(days []storage.ShowbackDay, cluster string) bool {
	for _, d := range days {
		if d.Cluster == cluster {
			return true
		}
	}
	return false
}
