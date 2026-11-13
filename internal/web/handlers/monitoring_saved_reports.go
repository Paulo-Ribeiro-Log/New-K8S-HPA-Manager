package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/storage"
)

// MonitoringSavedReportsHandler guarda relatórios do Relatório de Métricas que o usuário salvou DE PROPÓSITO.
// O relatório vive só 1 hora na memória do servidor (reportJobTTL); salvar o torna reabrível depois, inclusive
// depois de reiniciar o servidor. Nada é salvo automaticamente, e cada usuário só enxerga os seus.
type MonitoringSavedReportsHandler struct {
	store   *storage.SavedReportsStore
	reports *MonitoringReportHandler
}

func NewMonitoringSavedReportsHandler(store *storage.SavedReportsStore, reports *MonitoringReportHandler) *MonitoringSavedReportsHandler {
	return &MonitoringSavedReportsHandler{store: store, reports: reports}
}

func (h *MonitoringSavedReportsHandler) unavailable(c *gin.Context) bool {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "armazenamento de relatórios salvos indisponível"})
		return true
	}
	return false
}

func savedReportStatus(err error) int {
	switch {
	case errors.Is(err, storage.ErrSavedReportName):
		return http.StatusBadRequest
	case errors.Is(err, storage.ErrSavedReportsLimit):
		return http.StatusConflict
	case errors.Is(err, storage.ErrSavedReportTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, storage.ErrSavedReportNotFound):
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// Save — POST /api/v1/monitoring/reports/:id/save  {"name": "..."}
// Salva o resultado do relatório (job) do próprio usuário, já concluído.
func (h *MonitoringSavedReportsHandler) Save(c *gin.Context) {
	if h.unavailable(c) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	job, ok := h.reports.find(c)
	if !ok {
		return
	}
	h.reports.mu.Lock()
	state, result := job.State, job.Result
	h.reports.mu.Unlock()
	if state != "done" || result == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "o relatório ainda não terminou: salve quando a coleta acabar"})
		return
	}
	data, err := json.Marshal(result)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "não consegui montar o relatório para salvar: " + err.Error()})
		return
	}
	clusters := map[string]bool{}
	for _, r := range result.Rows {
		clusters[r.Cluster] = true
	}
	meta, err := h.store.Save(c.GetString("user_email"), storage.SavedReportMeta{
		Name: body.Name, WindowStart: result.Window.Start, WindowEnd: result.Window.End, Apps: len(result.Rows), Clusters: len(clusters),
	}, data)
	if err != nil {
		c.JSON(savedReportStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, meta)
}

// List — GET /api/v1/monitoring/saved-reports → {"reports": [...]} (sem o resultado)
func (h *MonitoringSavedReportsHandler) List(c *gin.Context) {
	if h.unavailable(c) {
		return
	}
	list, err := h.store.List(c.GetString("user_email"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reports": list})
}

func (h *MonitoringSavedReportsHandler) id(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return 0, false
	}
	return id, true
}

// Get — GET /api/v1/monitoring/saved-reports/:id → {"meta": {...}, "result": {...}}
func (h *MonitoringSavedReportsHandler) Get(c *gin.Context) {
	if h.unavailable(c) {
		return
	}
	id, ok := h.id(c)
	if !ok {
		return
	}
	meta, raw, err := h.store.Get(c.GetString("user_email"), id)
	if err != nil {
		c.JSON(savedReportStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"meta": meta, "result": json.RawMessage(raw)})
}

// Delete — DELETE /api/v1/monitoring/saved-reports/:id
func (h *MonitoringSavedReportsHandler) Delete(c *gin.Context) {
	if h.unavailable(c) {
		return
	}
	id, ok := h.id(c)
	if !ok {
		return
	}
	if err := h.store.Delete(c.GetString("user_email"), id); err != nil {
		c.JSON(savedReportStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}
