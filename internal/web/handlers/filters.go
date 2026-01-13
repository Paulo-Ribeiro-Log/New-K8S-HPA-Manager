package handlers

import (
	"net/http"
	"time"

	"k8s-hpa-manager/internal/healthcheck"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// FiltersHandler gerencia requisições de filtros
type FiltersHandler struct {
	orchestrator *healthcheck.Orchestrator
}

// NewFiltersHandler cria um novo handler de filtros
func NewFiltersHandler(orchestrator *healthcheck.Orchestrator) *FiltersHandler {
	return &FiltersHandler{
		orchestrator: orchestrator,
	}
}

// ListRules lista todas as regras de filtro
// GET /api/v1/filters
func (h *FiltersHandler) ListRules(c *gin.Context) {
	filterManager := h.orchestrator.GetFilterManager()
	if filterManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Filter manager não inicializado",
			},
		})
		return
	}

	rules := filterManager.GetRules()
	stats := filterManager.GetStats()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rules": rules,
			"stats": stats,
		},
	})
}

// AddRule adiciona nova regra de filtro
// POST /api/v1/filters
func (h *FiltersHandler) AddRule(c *gin.Context) {
	filterManager := h.orchestrator.GetFilterManager()
	if filterManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Filter manager não inicializado",
			},
		})
		return
	}

	var rule healthcheck.FilterRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Dados inválidos",
				"details": err.Error(),
			},
		})
		return
	}

	// Gerar ID e timestamp
	rule.ID = uuid.New().String()
	rule.CreatedAt = time.Now().Format(time.RFC3339)

	// TODO: Pegar email do usuário do contexto RBAC
	rule.CreatedBy = "user@example.com"

	// Adicionar regra
	if err := filterManager.AddRule(rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"message": err.Error(),
			},
		})
		return
	}

	// Salvar no arquivo
	if err := filterManager.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save filters config")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao salvar filtros",
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Regra adicionada com sucesso",
		"data":    rule,
	})
}

// RemoveRule remove regra de filtro
// DELETE /api/v1/filters/:id
func (h *FiltersHandler) RemoveRule(c *gin.Context) {
	filterManager := h.orchestrator.GetFilterManager()
	if filterManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Filter manager não inicializado",
			},
		})
		return
	}

	id := c.Param("id")

	if err := filterManager.RemoveRule(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"message": err.Error(),
			},
		})
		return
	}

	// Salvar no arquivo
	if err := filterManager.Save(); err != nil {
		log.Error().Err(err).Msg("Failed to save filters config")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"message": "Falha ao salvar filtros",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Regra removida com sucesso",
	})
}

// GetCategories retorna lista de categorias disponíveis
// GET /api/v1/filters/categories
func (h *FiltersHandler) GetCategories(c *gin.Context) {
	categories := []gin.H{
		{
			"value":       "deployment_no_probes",
			"label":       "Deployment sem probes",
			"description": "Ignorar deployments que não têm liveness e readiness probes configurados",
		},
		{
			"value":       "deployment_no_liveness_probe",
			"label":       "Deployment sem liveness probe",
			"description": "Ignorar deployments que não têm liveness probe configurado",
		},
		{
			"value":       "deployment_no_readiness_probe",
			"label":       "Deployment sem readiness probe",
			"description": "Ignorar deployments que não têm readiness probe configurado",
		},
		{
			"value":       "configmap_empty",
			"label":       "ConfigMap vazio",
			"description": "Ignorar ConfigMaps vazios (comum em ingress-nginx, cert-manager, etc)",
		},
		{
			"value":       "secret_empty",
			"label":       "Secret vazio",
			"description": "Ignorar Secrets vazios",
		},
		{
			"value":       "secret_service_account",
			"label":       "Service Account Token",
			"description": "Ignorar tokens de service account gerados automaticamente",
		},
		{
			"value":       "secret_invalid_values",
			"label":       "Secret com valores inválidos",
			"description": "Ignorar Secrets com valores inválidos ou vazios",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    categories,
	})
}
