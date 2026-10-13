package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"k8s-hpa-manager/internal/history"

	"github.com/gin-gonic/gin"
)

// HistoryHandler gerencia endpoints de histórico
type HistoryHandler struct {
	tracker *history.HistoryTracker
}

// NewHistoryHandler cria um novo handler
func NewHistoryHandler(tracker *history.HistoryTracker) *HistoryHandler {
	return &HistoryHandler{
		tracker: tracker,
	}
}

// parseHistoryDate lê AAAA-MM-DD no fuso local (o dia do usuário, não o UTC).
func parseHistoryDate(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

// GetHistory retorna o histórico filtrado e paginado (mais recentes primeiro).
// GET /api/v1/history?action=&cluster=&resource=&status=&user_email=&session_name=&start_date=&end_date=&limit=&offset=
func (h *HistoryHandler) GetHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	offset, _ := strconv.Atoi(c.Query("offset"))
	filter := history.HistoryFilter{
		Action:      c.Query("action"),
		Cluster:     c.Query("cluster"),
		Resource:    c.Query("resource"),
		Status:      c.Query("status"),
		UserEmail:   c.Query("user_email"),
		SessionName: c.Query("session_name"),
		StartDate:   parseHistoryDate(c.Query("start_date")),
		EndDate:     parseHistoryDate(c.Query("end_date")),
		Limit:       limit,
		Offset:      offset,
	}

	entries, total, err := h.tracker.Query(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"entries":  entries,
		"count":    len(entries),
		"total":    total,
		"offset":   offset,
		"has_more": offset+len(entries) < total,
	})
}

// GetHistoryEntry retorna uma entrada específica por ID
// GET /api/v1/history/:id
func (h *HistoryHandler) GetHistoryEntry(c *gin.Context) {
	id := c.Param("id")

	entry, err := h.tracker.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("History entry not found: %s", id)})
		return
	}

	c.JSON(http.StatusOK, entry)
}

// Não existe endpoint para apagar o histórico: é um registro de auditoria. Entradas saem só pela
// retenção automática (history.RetentionDays).

// GetHistoryStats retorna estatísticas do histórico inteiro (status, ação, cluster, usuário)
// GET /api/v1/history/stats
func (h *HistoryHandler) GetHistoryStats(c *gin.Context) {
	stats, err := h.tracker.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":          stats.Total,
		"success":        stats.Success,
		"failed":         stats.Failed,
		"by_action":      stats.ByAction,
		"by_cluster":     stats.ByCluster,
		"by_user":        stats.ByUser,
		"retention_days": history.RetentionDays,
	})
}

// GetCordonDrainHistory retorna histórico de operações Cordon/Drain com estatísticas
// GET /api/v1/history/cordon-drain?cluster=prod&start_date=2025-01-01
func (h *HistoryHandler) GetCordonDrainHistory(c *gin.Context) {
	// Parse filtros da query string
	allEntries, _, err := h.tracker.Query(history.HistoryFilter{
		Actions:   []string{history.ActionCordonNode, history.ActionDrainNode, history.ActionNodePoolSequence},
		Cluster:   c.Query("cluster"),
		StartDate: parseHistoryDate(c.Query("start_date")),
		EndDate:   parseHistoryDate(c.Query("end_date")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Estrutura de resposta
	response := gin.H{
		"cordon_operations":   []history.HistoryEntry{},
		"drain_operations":    []history.HistoryEntry{},
		"sequence_operations": []history.HistoryEntry{},
		"stats": gin.H{
			"total_cordon":      0,
			"total_drain":       0,
			"total_sequences":   0,
			"cordon_success":    0,
			"cordon_failed":     0,
			"drain_success":     0,
			"drain_failed":      0,
			"sequence_success":  0,
			"sequence_failed":   0,
			"total_duration_ms": int64(0),
			"avg_duration_ms":   int64(0),
		},
	}

	cordonOps := []history.HistoryEntry{}
	drainOps := []history.HistoryEntry{}
	sequenceOps := []history.HistoryEntry{}
	totalDuration := int64(0)
	operationCount := 0

	// Filtrar e processar entradas
	for _, entry := range allEntries {
		// Classificar por tipo de operação
		switch entry.Action {
		case history.ActionCordonNode:
			cordonOps = append(cordonOps, entry)
			response["stats"].(gin.H)["total_cordon"] = response["stats"].(gin.H)["total_cordon"].(int) + 1
			if entry.Status == history.StatusSuccess {
				response["stats"].(gin.H)["cordon_success"] = response["stats"].(gin.H)["cordon_success"].(int) + 1
			} else if entry.Status == history.StatusFailed {
				response["stats"].(gin.H)["cordon_failed"] = response["stats"].(gin.H)["cordon_failed"].(int) + 1
			}
			totalDuration += entry.Duration
			operationCount++

		case history.ActionDrainNode:
			drainOps = append(drainOps, entry)
			response["stats"].(gin.H)["total_drain"] = response["stats"].(gin.H)["total_drain"].(int) + 1
			if entry.Status == history.StatusSuccess {
				response["stats"].(gin.H)["drain_success"] = response["stats"].(gin.H)["drain_success"].(int) + 1
			} else if entry.Status == history.StatusFailed {
				response["stats"].(gin.H)["drain_failed"] = response["stats"].(gin.H)["drain_failed"].(int) + 1
			}
			totalDuration += entry.Duration
			operationCount++

		case history.ActionNodePoolSequence:
			sequenceOps = append(sequenceOps, entry)
			response["stats"].(gin.H)["total_sequences"] = response["stats"].(gin.H)["total_sequences"].(int) + 1
			if entry.Status == history.StatusSuccess {
				response["stats"].(gin.H)["sequence_success"] = response["stats"].(gin.H)["sequence_success"].(int) + 1
			} else if entry.Status == history.StatusFailed {
				response["stats"].(gin.H)["sequence_failed"] = response["stats"].(gin.H)["sequence_failed"].(int) + 1
			}
			totalDuration += entry.Duration
			operationCount++
		}
	}

	// Calcular média de duração
	if operationCount > 0 {
		response["stats"].(gin.H)["avg_duration_ms"] = totalDuration / int64(operationCount)
	}
	response["stats"].(gin.H)["total_duration_ms"] = totalDuration

	// Adicionar operações à resposta
	response["cordon_operations"] = cordonOps
	response["drain_operations"] = drainOps
	response["sequence_operations"] = sequenceOps

	c.JSON(http.StatusOK, response)
}
