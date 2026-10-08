package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/cloudprovider"
)

// Reconcile — POST /api/v1/nodepools/:cluster/:resource_group/:name/reconcile
// Reaplica a configuração atual do node pool para tirá-lo de Failed/Canceled (AKS:
// `az aks nodepool update` sem mudanças). O provider valida o estado na hora e não espera a
// operação terminar: a tela acompanha pelo status da listagem (Updating → Succeeded/Failed).
func (h *NodePoolHandler) Reconcile(c *gin.Context) {
	cluster, pool := c.Param("cluster"), c.Param("name")
	start := time.Now()
	status, body := reconcileNodePool(c.Request.Context(), h.kubeManager.GetNodeGroupProvider(cluster), cluster, pool)

	// Reconcile aceito ou não, o estado pode ter mudado: a próxima listagem busca de novo.
	h.invalidateNodePoolCache(cluster)

	// Recusa por estado/provider não chegou a mexer no pool: não vai para o histórico.
	if h.historyTracker != nil && (status == http.StatusAccepted || status == http.StatusInternalServerError) {
		result, errMsg := "success", ""
		if status != http.StatusAccepted {
			result, errMsg = "failed", body["message"].(string)
		}
		entry := CreateHistoryEntry(c, "reconcile_nodepool", c.Param("resource_group")+"/"+pool, cluster, result,
			nil, map[string]interface{}{"operation": "az aks nodepool update (sem mudanças)"}, time.Since(start).Milliseconds(), errMsg)
		if err := h.historyTracker.Log(entry); err != nil {
			log.Warn().Err(err).Msg("falha ao registrar reconcile no histórico")
		}
	}
	c.JSON(status, body)
}

// reconcileNodePool traduz o resultado do provider em status HTTP + corpo (separado para teste).
func reconcileNodePool(ctx context.Context, provider cloudprovider.NodeGroupProvider, cluster, pool string) (int, gin.H) {
	err := provider.ReconcileNodeGroup(ctx, cluster, pool)
	var stateErr *cloudprovider.ReconcileStateError
	switch {
	case err == nil:
		log.Info().Str("cluster", cluster).Str("node_pool", pool).Msg("Reconcile de node pool iniciado")
		return http.StatusAccepted, gin.H{"success": true, "message": "Reconcile iniciado: o node pool passa para Updating e volta a Succeeded quando terminar (pode levar vários minutos)."}
	case errors.Is(err, cloudprovider.ErrNotSupported):
		return http.StatusNotImplemented, gin.H{"success": false, "message": "Reconcile não é suportado por este cloud provider (só AKS)"}
	case errors.As(err, &stateErr):
		return http.StatusConflict, gin.H{"success": false, "message": stateErr.Reason, "state": stateErr.State}
	default:
		log.Error().Err(err).Str("cluster", cluster).Str("pool", pool).Msg("Falha no reconcile do node pool")
		return http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()}
	}
}
