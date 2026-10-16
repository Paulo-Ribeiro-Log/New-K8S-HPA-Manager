package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/storage"
)

// Aplicar e acompanhar (FINOPS-REVISAO-PLAN.md, Fase 3): cada envio de recomendação (Staging, patch
// de values, kubectl) fica gravado com a foto do workload no último relatório; a listagem compara a
// foto com o relatório mais recente (finops.EvaluateAction).

// SetActionsStore injeta o store das ações (pode ser nil).
func (h *FinOpsHandler) SetActionsStore(store *storage.FinOpsActionsStore) {
	h.actionsStore = store
}

// lastReport lê o último relatório gravado do cluster com cobertura e isenções (como GetLastReport).
// nil quando o cluster nunca foi analisado ou o cache não está disponível.
func (h *FinOpsHandler) lastReport(cluster string) (*finops.FinOpsReport, error) {
	if h.reportCacheStore == nil {
		return nil, nil
	}
	raw, _, found, err := h.reportCacheStore.Get(cluster)
	if err != nil || !found {
		return nil, err
	}
	var r finops.FinOpsReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	finops.AttachPoolCoverage(r.NodePools, h.poolCoverage(cluster))
	h.applyExemptions(cluster, &r)
	return &r, nil
}

type actionRequest struct {
	Cluster           string  `json:"cluster"`
	Namespace         string  `json:"namespace"`
	Workload          string  `json:"workload"`
	Channel           string  `json:"channel"`
	ExpectedSavingBRL float64 `json:"expected_saving_brl"`
	TargetHPAMin      int     `json:"target_hpa_min"`
	TargetHPAMax      int     `json:"target_hpa_max"`
	TargetCPUMillis   float64 `json:"target_cpu_millis"`
	TargetMemMi       float64 `json:"target_mem_mi"`
}

// RecordAction godoc
// POST /api/v1/finops/actions — registra o envio de uma recomendação para acompanhamento. A foto
// (baseline) vem do último relatório gravado no servidor, nunca do navegador.
func (h *FinOpsHandler) RecordAction(c *gin.Context) {
	start := time.Now()
	var req actionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	switch {
	case req.Cluster == "" || req.Namespace == "" || req.Workload == "":
		c.JSON(http.StatusBadRequest, gin.H{"error": "'cluster', 'namespace' e 'workload' são obrigatórios"})
		return
	case req.Channel != "staging" && req.Channel != "values" && req.Channel != "kubectl":
		c.JSON(http.StatusBadRequest, gin.H{"error": "'channel' deve ser staging, values ou kubectl"})
		return
	case req.TargetHPAMin <= 0 && req.TargetHPAMax <= 0 && req.TargetCPUMillis <= 0 && req.TargetMemMi <= 0:
		c.JSON(http.StatusBadRequest, gin.H{"error": "a recomendação não tem nenhum alvo (réplicas ou requests)"})
		return
	}
	if h.actionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de ações indisponível"})
		return
	}
	report, err := h.lastReport(req.Cluster)
	if err != nil || report == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "o cluster não tem relatório gravado — rode Analisar antes"})
		return
	}
	var w *finops.FinOpsWorkload
	for i := range report.Workloads {
		if report.Workloads[i].Namespace == req.Namespace && report.Workloads[i].Workload == req.Workload {
			w = &report.Workloads[i]
			break
		}
	}
	if w == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workload não está no último relatório do cluster"})
		return
	}
	a := storage.FinOpsAction{
		Cluster: req.Cluster, Namespace: req.Namespace, Workload: req.Workload, Channel: req.Channel,
		CreatedBy: c.GetString("user_email"), CreatedAt: time.Now(), BaselineReportAt: report.GeneratedAt,
		BaselinePods: w.Pods, BaselineHPAMin: w.HPAMin, BaselineHPAMax: w.HPAMax,
		BaselineCPUMillis: w.CPURequestMillis, BaselineMemMi: w.MemRequestMi,
		BaselineCostBRL: w.CostShareBRL, BaselineWasteBRL: w.WasteBRL, ExpectedSavingBRL: req.ExpectedSavingBRL,
		TargetHPAMin: req.TargetHPAMin, TargetHPAMax: req.TargetHPAMax,
		TargetCPUMillis: req.TargetCPUMillis, TargetMemMi: req.TargetMemMi,
	}
	id, err := h.actionsStore.Insert(a)
	logOperation(c, h.historyTracker, "finops_action_"+req.Channel, req.Namespace+"/"+req.Workload, req.Cluster, nil,
		map[string]interface{}{"target_hpa_min": req.TargetHPAMin, "target_hpa_max": req.TargetHPAMax,
			"target_cpu_millis": req.TargetCPUMillis, "target_mem_mi": req.TargetMemMi}, start, err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao gravar a ação: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

type actionWithEval struct {
	storage.FinOpsAction
	Evaluation finops.ActionEvaluation `json:"evaluation"`
}

// ListActions godoc
// GET /api/v1/finops/actions?cluster=X — ações do cluster com o estado no relatório mais recente.
func (h *FinOpsHandler) ListActions(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	out := []actionWithEval{}
	if h.actionsStore == nil {
		c.JSON(http.StatusOK, gin.H{"actions": out})
		return
	}
	list, err := h.actionsStore.ListByCluster(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao ler as ações: " + err.Error()})
		return
	}
	report, _ := h.lastReport(cluster)
	for _, a := range list {
		out = append(out, actionWithEval{FinOpsAction: a, Evaluation: finops.EvaluateAction(a, report)})
	}
	c.JSON(http.StatusOK, gin.H{"actions": out})
}

// DeleteAction godoc
// DELETE /api/v1/finops/actions?cluster=X&id=N — tira a ação do acompanhamento.
func (h *FinOpsHandler) DeleteAction(c *gin.Context) {
	start := time.Now()
	cluster := strings.TrimSpace(c.Query("cluster"))
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if cluster == "" || err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "'cluster' e 'id' são obrigatórios"})
		return
	}
	if h.actionsStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "store de ações indisponível"})
		return
	}
	found, err := h.actionsStore.Delete(cluster, id)
	logOperation(c, h.historyTracker, "finops_action_delete", strconv.FormatInt(id, 10), cluster, nil, nil, start, err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao remover: " + err.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "ação não encontrada"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
