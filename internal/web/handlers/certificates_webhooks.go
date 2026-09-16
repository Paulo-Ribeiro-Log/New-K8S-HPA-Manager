package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/certificates"
)

// ListMutatingWebhooks lista os MutatingWebhookConfiguration de um cluster — objeto CLUSTER-SCOPED
// (sem namespace), por isso o cluster vai via query param (?cluster=X) em vez de segmento de path
// — evitaria colidir com a rota /:cluster/:namespace/:name já existente neste grupo.
func (h *CertificatesHandler) ListMutatingWebhooks(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PARAMETER", "message": "cluster e obrigatorio"},
		})
		return
	}

	result, err := h.scanner.ListMutatingWebhookConfigurations(c.Request.Context(), cluster)
	if err != nil {
		log.Error().Err(err).Str("cluster", cluster).Msg("Erro ao listar MutatingWebhookConfigurations")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_WEBHOOKS_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// UpdateMutatingWebhookCABundle sobrescreve o caBundle de uma ou mais entradas webhooks[] de um
// MutatingWebhookConfiguration — ver certificates.Scanner.UpdateMutatingWebhookCABundle pro
// racional completo (caso de uso: renovar a confiança do apiserver no webhook do Delinea DSV
// injector ou do Istio sidecar injector depois que o certificado de serviço deles é rotacionado).
func (h *CertificatesHandler) UpdateMutatingWebhookCABundle(c *gin.Context) {
	var req certificates.UpdateMutatingWebhookCABundleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("Requisicao invalida: %v", err)},
		})
		return
	}

	if req.Cluster == "" || req.ConfigName == "" || req.CABundlePEM == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PARAMETER", "message": "cluster, configName e caBundlePEM sao obrigatorios"},
		})
		return
	}

	result, err := h.scanner.UpdateMutatingWebhookCABundle(c.Request.Context(), req)
	if err != nil {
		log.Error().
			Err(err).
			Str("cluster", req.Cluster).
			Str("configName", req.ConfigName).
			Msg("Erro ao atualizar caBundle do MutatingWebhookConfiguration")
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "UPDATE_CABUNDLE_ERROR", "message": err.Error()},
		})
		return
	}

	before := map[string]interface{}{"entries": result.Before}
	after := map[string]interface{}{
		"updatedNames": result.UpdatedNames,
		"newSubject":   result.NewSubject,
		"newIssuer":    result.NewIssuer,
		"newNotAfter":  result.NewNotAfter,
	}
	entry := CreateHistoryEntry(c, "webhook-cabundle-update", req.ConfigName, req.Cluster, "success", before, after, 0, "")
	if err := h.historyTracker.Log(entry); err != nil {
		log.Warn().Err(err).Msg("erro ao registrar atualização de caBundle no history tracker")
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
