package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/incidentkb"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// IncidentKBHandler expõe a base de conhecimento de incidentes de cluster —
// registros curados e confirmados por analista, separados do histórico bruto
// de execuções de IA (AIHistoryStore, que tem expiração automática).
type IncidentKBHandler struct {
	store *incidentkb.Store
}

// NewIncidentKBHandler cria um novo IncidentKBHandler.
func NewIncidentKBHandler(store *incidentkb.Store) *IncidentKBHandler {
	return &IncidentKBHandler{store: store}
}

// createIncidentRequest é o corpo aceito por Create — o analista confirma
// (e pode editar) sintoma/causa raiz vindos de uma análise de IA, e
// obrigatoriamente preenche a resolução real aplicada.
type createIncidentRequest struct {
	Cluster          string   `json:"cluster" binding:"required"`
	Namespace        string   `json:"namespace" binding:"required"`
	ResourceType     string   `json:"resource_type" binding:"required"`
	ResourceName     string   `json:"resource_name" binding:"required"`
	Severity         string   `json:"severity"`
	Tags             []string `json:"tags"`
	Symptom          string   `json:"symptom" binding:"required"`
	RootCause        string   `json:"root_cause"`
	Resolution       string   `json:"resolution" binding:"required"`
	SourceAnalysisID string   `json:"source_analysis_id"`
}

// Create grava um novo incidente confirmado pelo analista autenticado.
func (h *IncidentKBHandler) Create(c *gin.Context) {
	var req createIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Severity == "" {
		req.Severity = "medium"
	}

	author := history.GetCurrentUserInfo(c).Email

	inc := &incidentkb.Incident{
		Author:           author,
		Cluster:          req.Cluster,
		Namespace:        req.Namespace,
		ResourceType:     req.ResourceType,
		ResourceName:     req.ResourceName,
		Severity:         req.Severity,
		Tags:             req.Tags,
		Symptom:          req.Symptom,
		RootCause:        req.RootCause,
		Resolution:       req.Resolution,
		SourceAnalysisID: req.SourceAnalysisID,
	}

	saved, err := h.store.Save(inc)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save incident to knowledge base")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save incident"})
		return
	}

	c.JSON(http.StatusCreated, saved)
}

// List busca incidentes com filtros e query textual opcionais.
// GET /api/v1/incident-kb?q=&cluster=&namespace=&resource_type=&limit=
func (h *IncidentKBHandler) List(c *gin.Context) {
	filters := incidentkb.SearchFilters{
		Cluster:      c.Query("cluster"),
		Namespace:    c.Query("namespace"),
		ResourceType: c.Query("resource_type"),
		Limit:        50,
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filters.Limit = limit
		}
	}

	results, err := h.store.Search(c.Query("q"), filters)
	if err != nil {
		log.Error().Err(err).Msg("Failed to search incident knowledge base")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search incidents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"total":   len(results),
	})
}

// GetByID retorna um incidente específico.
func (h *IncidentKBHandler) GetByID(c *gin.Context) {
	inc, err := h.store.GetByID(c.Param("id"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to load incident")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load incident"})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found"})
		return
	}
	c.JSON(http.StatusOK, inc)
}

// Export retorna um único incidente em Markdown pronto pra colar no
// Confluence (sem o front-matter YAML do formato de armazenamento interno).
// GET /api/v1/incident-kb/:id/export
func (h *IncidentKBHandler) Export(c *gin.Context) {
	inc, err := h.store.GetByID(c.Param("id"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to load incident for export")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load incident"})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found"})
		return
	}

	filename := fmt.Sprintf("incidente_%s_%s.md", inc.ResourceName, inc.ID)
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", incidentkb.ExportMarkdown(inc))
}

// ExportAll retorna, em um único Markdown, todos os incidentes que casarem
// com os mesmos filtros de List/Search — pra popular de uma vez uma página
// do Confluence com a base inteira (ou um recorte dela).
// GET /api/v1/incident-kb/export-all?q=&cluster=&namespace=&resource_type=
func (h *IncidentKBHandler) ExportAll(c *gin.Context) {
	filters := incidentkb.SearchFilters{
		Cluster:      c.Query("cluster"),
		Namespace:    c.Query("namespace"),
		ResourceType: c.Query("resource_type"),
	}

	results, err := h.store.Search(c.Query("q"), filters)
	if err != nil {
		log.Error().Err(err).Msg("Failed to search incident knowledge base for export")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search incidents"})
		return
	}

	incidents := make([]*incidentkb.Incident, 0, len(results))
	for _, r := range results {
		incidents = append(incidents, r.Incident)
	}

	c.Header("Content-Disposition", "attachment; filename=\"base-conhecimento-incidentes.md\"")
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", incidentkb.ExportBundleMarkdown(incidents))
}
