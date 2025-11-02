package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/history"
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

// GetHistory retorna histórico com filtros opcionais
// GET /api/v1/history?action=update_hpa&cluster=akspriv-prod&start_date=2025-01-01
func (h *HistoryHandler) GetHistory(c *gin.Context) {
	// Parse filtros da query string
	filter := history.HistoryFilter{
		Action:      c.Query("action"),
		Cluster:     c.Query("cluster"),
		Resource:    c.Query("resource"),
		Status:      c.Query("status"),
		SessionName: c.Query("session_name"),
	}

	// Parse datas
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if t, err := time.Parse("2006-01-02", startDateStr); err == nil {
			filter.StartDate = t
		}
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if t, err := time.Parse("2006-01-02", endDateStr); err == nil {
			filter.EndDate = t.Add(24 * time.Hour) // Incluir dia completo
		}
	}

	// Buscar histórico filtrado
	var entries []history.HistoryEntry
	if filter.Action == "" && filter.Cluster == "" && filter.Resource == "" &&
		filter.Status == "" && filter.SessionName == "" &&
		filter.StartDate.IsZero() && filter.EndDate.IsZero() {
		// Sem filtros, retornar todos
		entries = h.tracker.GetAll()
	} else {
		// Com filtros
		entries = h.tracker.GetFiltered(filter)
	}

	c.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"count":   len(entries),
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

// ClearHistory limpa todo o histórico
// DELETE /api/v1/history
func (h *HistoryHandler) ClearHistory(c *gin.Context) {
	if err := h.tracker.Clear(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "History cleared successfully"})
}

// GetHistoryStats retorna estatísticas do histórico
// GET /api/v1/history/stats
func (h *HistoryHandler) GetHistoryStats(c *gin.Context) {
	entries := h.tracker.GetAll()

	stats := map[string]interface{}{
		"total":    len(entries),
		"success":  0,
		"failed":   0,
		"by_action": make(map[string]int),
		"by_cluster": make(map[string]int),
	}

	for _, entry := range entries {
		// Count by status
		if entry.Status == history.StatusSuccess {
			stats["success"] = stats["success"].(int) + 1
		} else if entry.Status == history.StatusFailed {
			stats["failed"] = stats["failed"].(int) + 1
		}

		// Count by action
		byAction := stats["by_action"].(map[string]int)
		byAction[entry.Action]++

		// Count by cluster
		byCluster := stats["by_cluster"].(map[string]int)
		byCluster[entry.Cluster]++
	}

	c.JSON(http.StatusOK, stats)
}
