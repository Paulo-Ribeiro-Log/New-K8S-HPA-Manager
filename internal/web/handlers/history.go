package handlers

import (
	"fmt"
	"net/http"
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

// GetHistory retorna histórico com filtros opcionais
// GET /api/v1/history?action=update_hpa&cluster=akspriv-prod&start_date=2025-01-01
func (h *HistoryHandler) GetHistory(c *gin.Context) {
	// Parse filtros da query string
	filter := history.HistoryFilter{
		Action:      c.Query("action"),
		Cluster:     c.Query("cluster"),
		Resource:    c.Query("resource"),
		Status:      c.Query("status"),
		UserEmail:   c.Query("user_email"),
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
			filter.EndDate = t // Não adiciona 24h - a comparação será feita apenas por data
		}
	}

	// Buscar histórico filtrado
	var entries []history.HistoryEntry
	if filter.Action == "" && filter.Cluster == "" && filter.Resource == "" &&
		filter.Status == "" && filter.UserEmail == "" && filter.SessionName == "" &&
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
		"total":      len(entries),
		"success":    0,
		"failed":     0,
		"by_action":  make(map[string]int),
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

// GetCordonDrainHistory retorna histórico de operações Cordon/Drain com estatísticas
// GET /api/v1/history/cordon-drain?cluster=prod&start_date=2025-01-01
func (h *HistoryHandler) GetCordonDrainHistory(c *gin.Context) {
	// Parse filtros da query string
	cluster := c.Query("cluster")

	var startDate, endDate time.Time
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if t, err := time.Parse("2006-01-02", startDateStr); err == nil {
			startDate = t
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if t, err := time.Parse("2006-01-02", endDateStr); err == nil {
			endDate = t.Add(24 * time.Hour)
		}
	}

	// Buscar todas as entradas do histórico
	allEntries := h.tracker.GetAll()

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
		// Aplicar filtros
		if cluster != "" && entry.Cluster != cluster {
			continue
		}
		if !startDate.IsZero() && entry.Timestamp.Before(startDate) {
			continue
		}
		if !endDate.IsZero() && entry.Timestamp.After(endDate) {
			continue
		}

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
