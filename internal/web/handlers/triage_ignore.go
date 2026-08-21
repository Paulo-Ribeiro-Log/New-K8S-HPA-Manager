package handlers

import (
	"net/http"
	"time"

	"k8s-hpa-manager/internal/healthcheck"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// TriageIgnoreHandler gerencia a lista de supressão de sinal externo do Modo Triagem (Fase 4,
// HEALTHCHECK-TRIAGE-MODE-PLAN.md seção 2.5) — mesmo padrão de handler de FiltersHandler
// (filters.go), que cobre postura de recursos K8s, não nome de sinal externo.
type TriageIgnoreHandler struct {
	orchestrator *healthcheck.Orchestrator
}

// NewTriageIgnoreHandler cria um novo handler de ignore-list do Modo Triagem.
func NewTriageIgnoreHandler(orchestrator *healthcheck.Orchestrator) *TriageIgnoreHandler {
	return &TriageIgnoreHandler{orchestrator: orchestrator}
}

// ListEntries lista todas as entradas de supressão
// GET /api/v1/triage-ignore
func (h *TriageIgnoreHandler) ListEntries(c *gin.Context) {
	mgr := h.orchestrator.GetTriageIgnoreManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Triage ignore manager não inicializado",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"entries": mgr.GetEntries(),
		},
	})
}

// AddEntry adiciona uma nova entrada de supressão
// POST /api/v1/triage-ignore
func (h *TriageIgnoreHandler) AddEntry(c *gin.Context) {
	mgr := h.orchestrator.GetTriageIgnoreManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Triage ignore manager não inicializado",
			},
		})
		return
	}

	var entry healthcheck.TriageIgnoreEntry
	if err := c.ShouldBindJSON(&entry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Dados inválidos",
				"details": err.Error(),
			},
		})
		return
	}

	entry.ID = uuid.New().String()
	entry.CreatedAt = time.Now().Format(time.RFC3339)
	if userEmail := c.GetString("user_email"); userEmail != "" {
		entry.CreatedBy = userEmail
	} else {
		entry.CreatedBy = "unknown"
	}

	if err := mgr.AddEntry(entry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": err.Error(),
			},
		})
		return
	}

	if err := mgr.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save triage ignore config")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao salvar lista de supressão",
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Entrada adicionada com sucesso",
		"data":    entry,
	})
}

// RemoveEntry remove uma entrada de supressão
// DELETE /api/v1/triage-ignore/:id
func (h *TriageIgnoreHandler) RemoveEntry(c *gin.Context) {
	mgr := h.orchestrator.GetTriageIgnoreManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Triage ignore manager não inicializado",
			},
		})
		return
	}

	id := c.Param("id")

	if err := mgr.RemoveEntry(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"message": err.Error(),
			},
		})
		return
	}

	if err := mgr.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save triage ignore config")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao salvar lista de supressão",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Entrada removida com sucesso",
	})
}

// ListSources retorna as fontes de sinal externo suportadas hoje (usado pelo frontend pra montar
// o seletor) — Dynatrace/Prometheus (Fase 1) e Elasticsearch (Fase 3) têm um TargetSource
// implementado; Zabbix aparece desabilitado até a Fase 5 ser implementada.
// GET /api/v1/triage-ignore/sources
func (h *TriageIgnoreHandler) ListSources(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": []gin.H{
			{"value": "prometheus_alert", "label": "Alerta Prometheus", "field_label": "Nome do alerta (alertname)", "enabled": true},
			{"value": "dynatrace_problem", "label": "Problem Dynatrace", "field_label": "Título ou Display ID do problem", "enabled": true},
			{"value": "zabbix_trigger", "label": "Trigger Zabbix", "field_label": "Nome do trigger", "enabled": false},
			{"value": "elasticsearch_pattern", "label": "Namespace Elasticsearch", "field_label": "Nome do namespace a ignorar", "enabled": true},
		},
	})
}
