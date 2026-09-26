package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// ClusterNodeHandler atende a aba Nodes (Workloads): listagem de todos os nodes, YAML
// editável, describe, cordon/uncordon, drain com progresso e delete.
// Rotas em /api/v1/cluster-nodes — /nodes/:cluster/:nodepool/... já é das telas de Node Pools.
type ClusterNodeHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

func NewClusterNodeHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *ClusterNodeHandler {
	return &ClusterNodeHandler{kubeManager: km, historyTracker: ht}
}

func (h *ClusterNodeHandler) client(c *gin.Context) (*kubeclient.Client, bool) {
	cluster := c.Param("cluster")
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("falha ao conectar no cluster %s: %v", cluster, err)))
		return nil, false
	}
	return kubeclient.NewClient(clientset, cluster), true
}

func (h *ClusterNodeHandler) record(c *gin.Context, action, status string, before, after map[string]interface{}, start time.Time, errMsg string) {
	if h.historyTracker == nil {
		return
	}
	entry := CreateHistoryEntry(c, action, "node/"+c.Param("name"), c.Param("cluster"), status, before, after, time.Since(start).Milliseconds(), errMsg)
	if err := h.historyTracker.Log(entry); err != nil {
		log.Warn().Err(err).Str("action", action).Msg("falha ao registrar histórico")
	}
}

// writeError responde erro de operação no node, com 403 legível quando o RBAC do cluster nega.
func writeNodeError(c *gin.Context, code string, err error) {
	if checkForbidden(c, err) {
		return
	}
	c.JSON(http.StatusInternalServerError, errorResponse(code, err.Error()))
}

// List — GET /api/v1/cluster-nodes/:cluster
func (h *ClusterNodeHandler) List(c *gin.Context) {
	k, ok := h.client(c)
	if !ok {
		return
	}
	if mc, err := h.kubeManager.GetMetricsClient(c.Param("cluster")); err == nil {
		if typed, ok := mc.(*metricsclientset.Clientset); ok {
			k.SetMetricsClient(typed)
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	nodes, err := k.ListClusterNodes(ctx)
	if err != nil {
		writeNodeError(c, "LIST_ERROR", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": nodes})
}

// Permissions — GET /api/v1/cluster-nodes/:cluster/permissions
// RBAC real do cluster para as ações da aba (Node é recurso sem namespace — as checagens por
// namespace das outras abas não o cobrem).
func (h *ClusterNodeHandler) Permissions(c *gin.Context) {
	clientset, err := h.kubeManager.GetClient(c.Param("cluster"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	can := func(verb, resource, subresource string) bool {
		sar := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: verb, Resource: resource, Subresource: subresource},
		}}
		res, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		return err == nil && res.Status.Allowed
	}
	c.JSON(http.StatusOK, gin.H{
		"canPatch":  can("patch", "nodes", ""),
		"canDelete": can("delete", "nodes", ""),
		"canEvict":  can("create", "pods", "eviction"),
	})
}

// Get — GET /api/v1/cluster-nodes/:cluster/:name
func (h *ClusterNodeHandler) Get(c *gin.Context) {
	k, ok := h.client(c)
	if !ok {
		return
	}
	manifest, err := k.GetNodeManifest(c.Request.Context(), c.Param("name"))
	if err != nil {
		writeNodeError(c, "GET_ERROR", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": manifest})
}

// Workloads — GET /api/v1/cluster-nodes/:cluster/:name/workloads
// Namespaces com pods no node e os deployments de cada um (navegação node → namespaces →
// deployments → pods do painel direito da aba Nodes).
func (h *ClusterNodeHandler) Workloads(c *gin.Context) {
	k, ok := h.client(c)
	if !ok {
		return
	}
	data, err := k.NodeWorkloads(c.Request.Context(), c.Param("name"))
	if err != nil {
		writeNodeError(c, "WORKLOADS_ERROR", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// Describe — GET /api/v1/cluster-nodes/:cluster/:name/describe
func (h *ClusterNodeHandler) Describe(c *gin.Context) {
	cluster, name := c.Param("cluster"), c.Param("name")
	authArgs, cleanup, err := h.kubeManager.KubectlAuthArgs(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DESCRIBE_ERROR", err.Error()))
		return
	}
	defer cleanup()

	output, err := kubeclient.ExecuteKubectlDescribe(authArgs, "node", name, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DESCRIBE_ERROR", fmt.Sprintf("kubectl describe falhou: %v", err)))
		return
	}
	c.JSON(http.StatusOK, gin.H{"cluster": cluster, "name": name, "describe": output})
}

// Apply — PUT /api/v1/cluster-nodes/:cluster/:name  Body: { yaml, dryRun, force }
func (h *ClusterNodeHandler) Apply(c *gin.Context) {
	var req struct {
		YAML   string `json:"yaml" binding:"required"`
		DryRun bool   `json:"dryRun"`
		Force  bool   `json:"force"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", "yaml é obrigatório"))
		return
	}
	k, ok := h.client(c)
	if !ok {
		return
	}
	ctx, name := c.Request.Context(), c.Param("name")

	var before map[string]interface{}
	if !req.DryRun {
		if m, err := k.GetNodeManifest(ctx, name); err == nil {
			before = map[string]interface{}{"yaml": m.YAML}
		}
	}
	start := time.Now()
	result, err := k.ApplyNode(ctx, req.YAML, "", name, req.DryRun, req.Force)
	if err != nil {
		if !req.DryRun {
			h.record(c, history.ActionApplyNode, "failed", before, nil, start, err.Error())
		}
		writeNodeError(c, "APPLY_ERROR", err)
		return
	}
	if !req.DryRun {
		h.record(c, history.ActionApplyNode, "success", before, map[string]interface{}{
			"labels": result.Labels, "annotations": result.Annotations,
			"taints": result.Spec.Taints, "unschedulable": result.Spec.Unschedulable,
		}, start, "")
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"name": name, "dryRun": req.DryRun}})
}

// Delete — DELETE /api/v1/cluster-nodes/:cluster/:name
// Remove só o objeto Node da API; a VM continua existindo na cloud (igual kubectl/k9s).
func (h *ClusterNodeHandler) Delete(c *gin.Context) {
	k, ok := h.client(c)
	if !ok {
		return
	}
	start := time.Now()
	if err := k.DeleteNode(c.Request.Context(), c.Param("name")); err != nil {
		h.record(c, history.ActionDeleteNode, "failed", nil, nil, start, err.Error())
		writeNodeError(c, "DELETE_ERROR", err)
		return
	}
	h.record(c, history.ActionDeleteNode, "success", nil, nil, start, "")
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Cordon — POST /api/v1/cluster-nodes/:cluster/:name/cordon
func (h *ClusterNodeHandler) Cordon(c *gin.Context) { h.setSchedulable(c, false) }

// Uncordon — POST /api/v1/cluster-nodes/:cluster/:name/uncordon
func (h *ClusterNodeHandler) Uncordon(c *gin.Context) { h.setSchedulable(c, true) }

func (h *ClusterNodeHandler) setSchedulable(c *gin.Context, schedulable bool) {
	k, ok := h.client(c)
	if !ok {
		return
	}
	action, op := history.ActionCordonNode, k.CordonNode
	if schedulable {
		action, op = history.ActionUncordonNode, k.UncordonNode
	}
	start := time.Now()
	before := map[string]interface{}{"unschedulable": schedulable}
	after := map[string]interface{}{"unschedulable": !schedulable}
	if err := op(c.Request.Context(), c.Param("name")); err != nil {
		h.record(c, action, "failed", before, nil, start, err.Error())
		writeNodeError(c, "CORDON_ERROR", err)
		return
	}
	h.record(c, action, "success", before, after, start, "")
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Drain — POST /api/v1/cluster-nodes/:cluster/:name/drain  Body: models.DrainOptions
// Faz cordon antes (como o kubectl drain) e responde em SSE (text/event-stream) com um
// kubeclient.DrainEvent por passo e um evento final "done" ou "error". Fechar a conexão cancela o
// drain (pods já removidos continuam removidos; o node continua cordoned).
func (h *ClusterNodeHandler) Drain(c *gin.Context) {
	opts := models.DefaultDrainOptions()
	if err := c.ShouldBindJSON(opts); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("opções inválidas: %v", err)))
		return
	}
	opts.ChunkSize = 1 // um node; ChunkSize aqui seria pods por lote — mantém o comportamento seguro
	if err := kubeclient.ValidateDrainOptions(opts); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	k, ok := h.client(c)
	if !ok {
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	send := func(e kubeclient.DrainEvent) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		c.Writer.Flush()
	}

	start := time.Now()
	// Checagem estrita do kubectl drain antes de mexer no node (nada é alterado se recusar).
	if err := k.CheckDrainable(c.Request.Context(), c.Param("name"), opts); err != nil {
		h.record(c, history.ActionDrainNode, "failed", map[string]interface{}{"options": opts}, nil, start, err.Error())
		send(kubeclient.DrainEvent{Type: "error", Message: err.Error()})
		return
	}
	// Como o kubectl drain: cordon primeiro, para nada novo ser agendado no node durante o drain.
	if err := k.CordonNode(c.Request.Context(), c.Param("name")); err != nil {
		h.record(c, history.ActionDrainNode, "failed", map[string]interface{}{"options": opts}, nil, start, err.Error())
		send(kubeclient.DrainEvent{Type: "error", Message: "falha no cordon antes do drain: " + err.Error()})
		return
	}
	send(kubeclient.DrainEvent{Type: "cordon", Message: "node marcado como unschedulable (cordon)"})

	var last kubeclient.DrainEvent
	err := k.DrainNodeWithProgress(c.Request.Context(), c.Param("name"), opts, func(e kubeclient.DrainEvent) {
		last = e
		send(e)
	})
	before := map[string]interface{}{"options": opts}
	after := map[string]interface{}{"evicted": last.Evicted, "total": last.Total}
	if err != nil {
		h.record(c, history.ActionDrainNode, "failed", before, after, start, err.Error())
		send(kubeclient.DrainEvent{Type: "error", Message: err.Error(), Evicted: last.Evicted, Total: last.Total})
		return
	}
	h.record(c, history.ActionDrainNode, "success", before, after, start, "")
}
