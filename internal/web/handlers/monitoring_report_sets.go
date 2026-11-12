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
	store *storage.LocalSettingsStore
}

func NewMonitoringSetsHandler(store *storage.LocalSettingsStore) *MonitoringSetsHandler {
	return &MonitoringSetsHandler{store: store}
}

func (h *MonitoringSetsHandler) key(c *gin.Context) string {
	return "metrics_report_sets:" + c.GetString("user_email")
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
	c.JSON(http.StatusOK, gin.H{"sets": sets})
}

// Put — PUT /api/v1/monitoring/report-sets  {"sets": [...]} substitui a lista do usuário.
func (h *MonitoringSetsHandler) Put(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "armazenamento local indisponível"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxReportSetsBytes)
	var body struct {
		Sets []json.RawMessage `json:"sets"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Sets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: esperado {\"sets\": [...]}"})
		return
	}
	data, err := json.Marshal(body.Sets)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.Set(h.key(c), string(data)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"saved": len(body.Sets)})
}
