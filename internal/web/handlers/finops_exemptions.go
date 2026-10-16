package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/storage"
)

// Isenções por workload no FinOps (FINOPS-EXEMPTIONS-PLAN.md). Ex.: HPA 1/1 em HLG é decisão de
// projeto; com a isenção, o workload sai do desperdício e das oportunidades enquanto as réplicas
// ficarem dentro do limite. Aplicadas na leitura do relatório (applyExemptions), sem novo scan.

const exemptionReasonMax = 200

// SetExemptionsStore injeta o store de isenções e o Change History (ambos podem ser nil).
func (h *FinOpsHandler) SetExemptionsStore(store *storage.FinOpsExemptionsStore, tracker *history.HistoryTracker) {
	h.exemptionsStore = store
	h.historyTracker = tracker
}

// applyExemptions aplica as isenções gravadas do cluster no relatório. nil-safe e best-effort.
func (h *FinOpsHandler) applyExemptions(cluster string, report *finops.FinOpsReport) {
	if h.exemptionsStore == nil || report == nil {
		return
	}
	ex, err := h.exemptionsStore.GetAllAsMap(cluster)
	if err != nil {
		log.Warn().Err(err).Str("cluster", cluster).Msg("FinOps/Exemptions: falha ao ler isenções — relatório sem elas")
		return
	}
	finops.ApplyExemptions(report, ex)
}

// ListExemptions godoc
// GET /api/v1/finops/exemptions?cluster=X
func (h *FinOpsHandler) ListExemptions(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if h.exemptionsStore == nil {
		c.JSON(http.StatusOK, []storage.FinOpsExemption{})
		return
	}
	list, err := h.exemptionsStore.ListByCluster(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler isenções: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

type exemptionRequest struct {
	Cluster              string `json:"cluster"`
	Namespace            string `json:"namespace"`
	Workload             string `json:"workload"`
	MaxReplicasThreshold int    `json:"max_replicas_threshold"`
	Reason               string `json:"reason"`
}

// UpsertExemption godoc
// POST /api/v1/finops/exemptions — cria ou atualiza a isenção do workload.
func (h *FinOpsHandler) UpsertExemption(c *gin.Context) {
	start := time.Now()
	var req exemptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	req.Cluster, req.Namespace, req.Workload = strings.TrimSpace(req.Cluster), strings.TrimSpace(req.Namespace), strings.TrimSpace(req.Workload)
	req.Reason = strings.TrimSpace(req.Reason)
	switch {
	case req.Cluster == "" || req.Namespace == "" || req.Workload == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "'cluster', 'namespace' e 'workload' são obrigatórios"})
		return
	case req.MaxReplicasThreshold < 1:
		c.JSON(http.StatusBadRequest, gin.H{"error": "'max_replicas_threshold' deve ser pelo menos 1"})
		return
	case utf8.RuneCountInString(req.Reason) > exemptionReasonMax:
		c.JSON(http.StatusBadRequest, gin.H{"error": "'reason' aceita no máximo 200 caracteres"})
		return
	}
	if h.exemptionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de isenções indisponível"})
		return
	}
	err := h.exemptionsStore.Upsert(storage.FinOpsExemption{
		Cluster: req.Cluster, Namespace: req.Namespace, Workload: req.Workload,
		MaxReplicasThreshold: req.MaxReplicasThreshold, Reason: req.Reason, CreatedBy: c.GetString("user_email"),
	})
	logOperation(c, h.historyTracker, "finops_exemption_upsert", req.Namespace+"/"+req.Workload, req.Cluster, nil,
		map[string]interface{}{"max_replicas_threshold": req.MaxReplicasThreshold, "reason": req.Reason}, start, err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gravar isenção: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteExemption godoc
// DELETE /api/v1/finops/exemptions?cluster=X&namespace=Y&workload=Z
func (h *FinOpsHandler) DeleteExemption(c *gin.Context) {
	start := time.Now()
	cluster, ns, wl := c.Query("cluster"), c.Query("namespace"), c.Query("workload")
	if cluster == "" || ns == "" || wl == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "'cluster', 'namespace' e 'workload' são obrigatórios"})
		return
	}
	if h.exemptionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de isenções indisponível"})
		return
	}
	found, err := h.exemptionsStore.Delete(cluster, ns, wl)
	logOperation(c, h.historyTracker, "finops_exemption_delete", ns+"/"+wl, cluster, nil, nil, start, err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao remover isenção: " + err.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "isenção não encontrada"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
