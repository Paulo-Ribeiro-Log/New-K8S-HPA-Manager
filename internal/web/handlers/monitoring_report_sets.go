package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/storage"
)

// maxReportSetsBytes limita o que um usuário guarda: são nomes e alvos, não dados.
const maxReportSetsBytes = 256 * 1024

// MonitoringSetsHandler guarda os "conjuntos salvos" do Relatório de Métricas por usuário, no SQLite
// local (local_settings). Antes ficavam só no localStorage do navegador e sumiam ao trocar de origem
// (porta, host, navegador ou perfil). O conteúdo é opaco para o servidor: a tela valida o formato.
type MonitoringSetsHandler struct {
	store  *storage.LocalSettingsStore
	prefix string // chave em local_settings, antes do e-mail do usuário
	field  string // nome do campo do JSON ("sets" ou "events")
}

func NewMonitoringSetsHandler(store *storage.LocalSettingsStore) *MonitoringSetsHandler {
	return &MonitoringSetsHandler{store: store, prefix: "metrics_report_sets:", field: "sets"}
}

// NewMonitoringEventsHandler guarda os eventos nomeados (janelas de tempo salvas) do mesmo jeito.
func NewMonitoringEventsHandler(store *storage.LocalSettingsStore) *MonitoringSetsHandler {
	return &MonitoringSetsHandler{store: store, prefix: "metrics_report_events:", field: "events"}
}

// NewMonitoringMarkersHandler guarda os marcadores pontuais do Tempo real ("Marcar agora") do mesmo jeito.
func NewMonitoringMarkersHandler(store *storage.LocalSettingsStore) *MonitoringSetsHandler {
	return &MonitoringSetsHandler{store: store, prefix: "metrics_live_markers:", field: "markers"}
}

func (h *MonitoringSetsHandler) key(c *gin.Context) string {
	return h.prefix + c.GetString("user_email")
}

// Get — GET /api/v1/monitoring/report-sets → {"sets": [...]}
func (h *MonitoringSetsHandler) Get(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "armazenamento local indisponível"})
		return
	}
	raw, err := h.store.Get(h.key(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sets := json.RawMessage("[]")
	if raw != "" && json.Valid([]byte(raw)) {
		sets = json.RawMessage(raw)
	}
	c.JSON(http.StatusOK, gin.H{h.field: sets})
}

// Put — PUT /api/v1/monitoring/report-sets  {"sets": [...]} substitui a lista do usuário.
func (h *MonitoringSetsHandler) Put(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "armazenamento local indisponível"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxReportSetsBytes)
	var body map[string][]json.RawMessage
	items, ok := []json.RawMessage(nil), false
	if err := c.ShouldBindJSON(&body); err == nil {
		items, ok = body[h.field]
	}
	if !ok || items == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: esperado {\"" + h.field + "\": [...]}"})
		return
	}
	data, err := json.Marshal(items)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.Set(h.key(c), string(data)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"saved": len(items)})
}
