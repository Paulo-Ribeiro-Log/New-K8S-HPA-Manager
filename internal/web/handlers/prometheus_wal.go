package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/promwal"
	"k8s-hpa-manager/internal/web/sse"
)

const (
	walAnalyzeTimeout     = 45 * time.Second
	walAnalyzeDeepTimeout = 5 * time.Minute
	walHealthTimeout      = 20 * time.Second
	walHealthTTL          = 45 * time.Second
	walSchedulingTimeout  = 90 * time.Second
)

// PrometheusWALHandler analisa um Prometheus que não sobe e diz se limpar o WAL é indicado
// (docs: PROMETHEUS-WAL-CLEANUP.md). A análise é leitura; só a análise profunda cria um pod temporário
// (PVC read-only), removido ao final.
type PrometheusWALHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
	store          *promwal.Store
	tracker        *sse.ProgressTracker
	opsMu          sync.Mutex
	ops            map[string]*walOp
	healthMu       sync.Mutex
	healthCache    map[string]walHealthEntry
}

type walHealthEntry struct {
	report promwal.HealthReport
	at     time.Time
}

func NewPrometheusWALHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *PrometheusWALHandler {
	return &PrometheusWALHandler{kubeManager: km, historyTracker: ht, store: promwal.NewStore(), tracker: GetProgressTracker(), ops: map[string]*walOp{}, healthCache: map[string]walHealthEntry{}}
}

// Analyze godoc
// GET /api/v1/prometheus/wal/analyze?cluster=X&namespace=Y[&name=Z][&deep=true]
// name vazio = o único recurso Prometheus do namespace. deep=true monta o PVC read-only em pod temporário.
func (h *PrometheusWALHandler) Analyze(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	namespace := strings.TrimSpace(c.Query("namespace"))
	name := strings.TrimSpace(c.Query("name"))
	deep := c.Query("deep") == "true"
	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetros 'cluster' e 'namespace' são obrigatórios"})
		return
	}

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao obter client do cluster: %v", err)})
		return
	}

	timeout := walAnalyzeTimeout
	if deep {
		timeout = walAnalyzeDeepTimeout
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	col := &promwal.Collector{Client: client, Run: promwal.KubectlRunner}
	name, err = col.ResolveName(ctx, cluster, namespace, name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in, err := col.Collect(ctx, cluster, namespace, name)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	in.HistoryCleanups = h.cleanupHistoryDates(cluster, namespace, name, time.Now())

	var deepErrs []error
	if deep {
		start := time.Now()
		deepErrs = col.CollectDeep(ctx, &in)
		// Criou (e removeu) pod no cluster: entra no histórico, só identificação.
		var opErr error
		if len(deepErrs) > 0 {
			opErr = deepErrs[0]
		}
		logOperation(c, h.historyTracker, "prometheus_wal_analyze_deep", namespace+"/"+name, cluster,
			nil, map[string]interface{}{"pods": len(in.Pods), "pvcs_lidos": len(in.Deep)}, start, opErr)
	}

	report := promwal.Analyze(in, time.Now())
	for _, e := range deepErrs {
		report.Warnings = append(report.Warnings, "Análise profunda falhou — "+e.Error())
	}
	c.JSON(http.StatusOK, h.store.Put(report))
}

// Health godoc
// GET /api/v1/prometheus/wal/health?cluster=X[&refresh=true]
// Verificação leve de integridade dos recursos Prometheus do cluster (só API do Kubernetes: sem pod temporário,
// sem logs, sem CLI cloud). Chamada pelo frontend quando o cluster fica ativo. Cache curto por cluster. Falha de
// alcance/permissão NÃO é erro HTTP: volta 200 com verifiable=false e o motivo (nunca vira alerta).
func (h *PrometheusWALHandler) Health(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro 'cluster' é obrigatório"})
		return
	}
	if c.Query("refresh") != "true" {
		h.healthMu.Lock()
		cached, ok := h.healthCache[cluster]
		h.healthMu.Unlock()
		if ok && time.Since(cached.at) < walHealthTTL {
			c.JSON(http.StatusOK, cached.report)
			return
		}
	}

	var rep promwal.HealthReport
	if client, err := h.kubeManager.GetClient(cluster); err != nil {
		rep = promwal.HealthReport{Cluster: cluster, CheckedAt: time.Now(), Items: []promwal.HealthItem{},
			Reason: fmt.Sprintf("sem alcance ao cluster: %v", err)}
	} else {
		ctx, cancel := context.WithTimeout(c.Request.Context(), walHealthTimeout)
		defer cancel()
		col := &promwal.Collector{Client: client, Run: promwal.KubectlRunner}
		rep = col.Health(ctx, cluster, func(ns, name string) []time.Time {
			return h.cleanupHistoryDates(cluster, ns, name, time.Now())
		})
	}

	h.healthMu.Lock()
	h.healthCache[cluster] = walHealthEntry{report: rep, at: time.Now()}
	h.healthMu.Unlock()
	c.JSON(http.StatusOK, rep)
}

// cleanupHistoryDates devolve as datas das limpezas deste Prometheus no Change History (últimos 30 dias) em que
// algo foi de fato apagado, inclusive as que falharam depois de apagar. O histórico é local desta instalação;
// a anotação persistente no recurso cobre as limpezas feitas por outras pessoas.
func (h *PrometheusWALHandler) cleanupHistoryDates(cluster, namespace, name string, now time.Time) []time.Time {
	resource := namespace + "/" + name
	entries, _, err := h.historyTracker.Query(history.HistoryFilter{
		Action: "prometheus_wal_cleanup", Cluster: cluster, Resource: resource,
		StartDate: now.Add(-promwal.RecurrenceWindow),
	})
	if err != nil {
		return nil
	}
	var dates []time.Time
	for _, e := range entries {
		if e.Resource != resource { // o filtro do histórico é por trecho: "monitoring/prom" também casa "monitoring/prom-x"
			continue
		}
		if cleaned, _ := e.After["cleaned"].([]interface{}); len(cleaned) > 0 {
			dates = append(dates, e.Timestamp)
		}
	}
	return dates
}

// ---- Limpeza do WAL -------------------------------------------------------------------------------

const (
	walConfirmWord  = "APAGAR"
	walCleanTimeout = 45 * time.Minute
)

type walCleanupRequest struct {
	AnalysisID  string   `json:"analysis_id"`
	Replicas    []string `json:"replicas"`     // pods a limpar; vazio = todas as afetadas da análise
	Confirm     string   `json:"confirm"`      // deve ser "APAGAR"
	ConfirmName string   `json:"confirm_name"` // nome do recurso Prometheus
	Force       bool     `json:"force"`        // ignora veredito e análise profunda (exige decisão explícita)
}

type walHTTPError struct {
	Status int
	Msg    string
}

// planCleanup valida o pedido contra a análise e devolve os PVCs alvo. Pura: o handler só traduz o erro em HTTP.
func planCleanup(r promwal.Report, req walCleanupRequest) ([]promwal.Target, *walHTTPError) {
	if req.Confirm != walConfirmWord || req.ConfirmName != r.Name {
		return nil, &walHTTPError{http.StatusBadRequest, fmt.Sprintf("confirmação inválida: digite %s e o nome do recurso Prometheus (%s)", walConfirmWord, r.Name)}
	}
	if r.PendingRestoreReplicas != nil {
		return nil, &walHTTPError{http.StatusConflict, "há uma limpeza anterior interrompida: restaure o Prometheus antes"}
	}
	if r.Verdict != promwal.VerdictCleanWAL && !req.Force {
		return nil, &walHTTPError{http.StatusConflict, fmt.Sprintf("a análise indica %q, não a limpeza do WAL (use force só se for decisão explícita)", r.Verdict)}
	}

	pods := map[string]promwal.PodReport{}
	for _, p := range r.Pods {
		pods[p.Pod] = p
	}
	names := req.Replicas
	if len(names) == 0 {
		names = r.AffectedReplicas
	}
	if len(names) == 0 {
		return nil, &walHTTPError{http.StatusBadRequest, "nenhuma réplica a limpar: informe 'replicas'"}
	}
	affected := map[string]bool{}
	for _, n := range r.AffectedReplicas {
		affected[n] = true
	}
	deep := map[string]bool{}
	for _, d := range r.Deep {
		deep[d.Pod] = true
	}

	var targets []promwal.Target
	seen := map[string]bool{}
	for _, n := range names {
		pr, ok := pods[n]
		switch {
		case seen[n]:
			continue
		case !ok:
			return nil, &walHTTPError{http.StatusBadRequest, "réplica fora da análise: " + n}
		case !req.Force && !affected[n]:
			return nil, &walHTTPError{http.StatusConflict, n + " não é uma réplica com indicação de limpeza na análise"}
		case !req.Force && !deep[n]:
			return nil, &walHTTPError{http.StatusConflict, "rode a análise profunda de " + n + " antes: é ela que mostra os dados que serão perdidos"}
		case pr.PVC == "":
			return nil, &walHTTPError{http.StatusConflict, "PVC de " + n + " desconhecido na análise"}
		}
		seen[n] = true
		targets = append(targets, promwal.Target{Pod: n, PVC: pr.PVC})
	}
	return targets, nil
}

type walOp struct {
	cancel context.CancelFunc
	key    string
}

func (h *PrometheusWALHandler) registerOp(id, key string, cancel context.CancelFunc) bool {
	h.opsMu.Lock()
	defer h.opsMu.Unlock()
	for _, o := range h.ops {
		if o.key == key {
			return false
		}
	}
	h.ops[id] = &walOp{cancel: cancel, key: key}
	return true
}

func (h *PrometheusWALHandler) finishOp(id string) {
	h.opsMu.Lock()
	delete(h.ops, id)
	h.opsMu.Unlock()
}

// Cleanup godoc
// POST /api/v1/prometheus/wal/cleanup (SRE) — valida e dispara a limpeza em segundo plano; progresso por SSE.
func (h *PrometheusWALHandler) Cleanup(c *gin.Context) {
	var req walCleanupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	report, ok := h.store.Get(req.AnalysisID)
	if !ok {
		c.JSON(http.StatusGone, gin.H{"error": "análise inexistente ou expirada (vale 10 min): rode a análise de novo"})
		return
	}
	targets, herr := planCleanup(report, req)
	if herr != nil {
		c.JSON(herr.Status, gin.H{"error": herr.Msg})
		return
	}
	client, err := h.kubeManager.GetClient(report.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao obter client do cluster: %v", err)})
		return
	}
	if _, ok := h.store.Take(req.AnalysisID); !ok {
		c.JSON(http.StatusGone, gin.H{"error": "a análise já foi usada em outra limpeza"})
		return
	}

	opID := newWALOpID()
	ctx, cancel := context.WithTimeout(context.Background(), walCleanTimeout)
	key := report.Cluster + "/" + report.Namespace + "/" + report.Name
	if !h.registerOp(opID, key, cancel) {
		cancel()
		c.JSON(http.StatusConflict, gin.H{"error": "já há uma limpeza em andamento neste Prometheus"})
		return
	}

	creq := promwal.CleanupRequest{Cluster: report.Cluster, Namespace: report.Namespace, Name: report.Name, Targets: targets}
	go h.runCleanup(ctx, cancel, opID, creq, client, history.DetachedUserContext(c))
	c.JSON(http.StatusAccepted, gin.H{"operation_id": opID, "targets": targets})
}

func (h *PrometheusWALHandler) runCleanup(ctx context.Context, cancel context.CancelFunc, opID string, req promwal.CleanupRequest, client kubernetes.Interface, userCtx context.Context) {
	defer cancel()
	defer h.finishOp(opID)
	start := time.Now()

	send := func(e sse.ProgressEvent) {
		e.ID, e.Timestamp, e.Cluster = opID, time.Now(), req.Cluster
		h.tracker.SendToClient(opID, e)
	}
	send(sse.ProgressEvent{Type: "wal-cleanup", Phase: "started", Message: "Limpeza iniciada"})

	cl := &promwal.Cleaner{Collector: &promwal.Collector{Client: client, Run: promwal.KubectlRunner}}
	res, err := cl.Execute(ctx, req, func(s promwal.Step) {
		send(sse.ProgressEvent{Type: "wal-cleanup", Phase: "in_progress", Message: s.Message, Progress: s.Progress, Details: s.Key})
	})

	status, errMsg := history.StatusSuccess, ""
	if err != nil {
		status, errMsg = history.StatusFailed, err.Error()
	}
	if h.historyTracker != nil {
		cleaned := make([]map[string]interface{}, 0, len(res.Cleaned))
		for _, t := range res.Cleaned {
			cleaned = append(cleaned, map[string]interface{}{"pvc": t.PVC, "wal_bytes": t.WALBytes, "chunks_head_bytes": t.ChunksHeadBytes, "last_block_ends_at": t.LastBlockEndsAt})
		}
		_ = h.historyTracker.Log(userCtx, history.HistoryEntry{
			Action: "prometheus_wal_cleanup", Resource: req.Namespace + "/" + req.Name, Cluster: req.Cluster,
			After:  map[string]interface{}{"cleaned": cleaned, "restored": res.Restored, "ready": res.Ready},
			Status: status, ErrorMsg: errMsg, Duration: time.Since(start).Milliseconds(),
		})
	}

	if err != nil {
		send(sse.ProgressEvent{Type: "error", Phase: "failed", Message: "Limpeza falhou", Error: err.Error(), Result: res})
		return
	}
	send(sse.ProgressEvent{Type: "complete", Phase: "completed", Message: "Limpeza concluída", Progress: 1, Result: res})
}

// Stream godoc
// GET /api/v1/prometheus/wal/progress/:id — SSE (rota em grupo com WebSocketJWTAuthMiddleware: token em ?token=).
func (h *PrometheusWALHandler) Stream(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id obrigatório"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range h.tracker.GetReplayEvents(id) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
		if evt.Type == "complete" || evt.Type == "error" {
			return
		}
	}
	client := sse.NewClient(id)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(id)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-client.Channel:
			if !ok {
				return
			}
			if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
				return
			}
			c.Writer.Flush()
			if evt.Type == "complete" || evt.Type == "error" {
				return
			}
		}
	}
}

// Cancel godoc
// POST /api/v1/prometheus/wal/cancel/:id (SRE) — cancela; a limpeza em curso religa o Prometheus antes de terminar.
func (h *PrometheusWALHandler) Cancel(c *gin.Context) {
	h.opsMu.Lock()
	op, ok := h.ops[c.Param("id")]
	h.opsMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "operação inexistente ou já terminada"})
		return
	}
	op.cancel()
	c.JSON(http.StatusAccepted, gin.H{"status": "cancelando: o Prometheus será religado"})
}

// Restore godoc
// POST /api/v1/prometheus/wal/restore (SRE) {cluster,namespace,name} — religa um Prometheus que uma limpeza
// interrompida deixou desligado (a anotação gravada no recurso diz quantas réplicas).
func (h *PrometheusWALHandler) Restore(c *gin.Context) {
	var req struct{ Cluster, Namespace, Name string }
	if err := c.ShouldBindJSON(&req); err != nil || req.Cluster == "" || req.Namespace == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe cluster, namespace e name"})
		return
	}
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao obter client do cluster: %v", err)})
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), walAnalyzeTimeout)
	defer cancel()
	cl := &promwal.Cleaner{Collector: &promwal.Collector{Client: client, Run: promwal.KubectlRunner}}
	n, err := cl.Restore(ctx, req.Cluster, req.Namespace, req.Name)
	logOperation(c, h.historyTracker, "prometheus_wal_restore", req.Namespace+"/"+req.Name, req.Cluster,
		nil, map[string]interface{}{"replicas": n}, start, err)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"restored_replicas": n})
}

// Scheduling godoc
// POST /api/v1/prometheus/wal/scheduling (SRE) {cluster,namespace,name,node_selector,tolerations} — corrige o
// agendamento de um Prometheus cujo pod não cabe em nenhum node: substitui o spec.nodeSelector e soma tolerations no
// recurso Prometheus e recria os pods presos em Pending. Não toca no WAL nem nos volumes.
func (h *PrometheusWALHandler) Scheduling(c *gin.Context) {
	var req struct {
		Cluster      string              `json:"cluster"`
		Namespace    string              `json:"namespace"`
		Name         string              `json:"name"`
		NodeSelector map[string]string   `json:"node_selector"`
		Tolerations  []corev1.Toleration `json:"tolerations"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Cluster == "" || req.Namespace == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe cluster, namespace e name"})
		return
	}
	client, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao obter client do cluster: %v", err)})
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), walSchedulingTimeout)
	defer cancel()
	col := &promwal.Collector{Client: client, Run: promwal.KubectlRunner}
	res, err := col.ApplyScheduling(ctx, promwal.SchedulingRequest{
		Cluster: req.Cluster, Namespace: req.Namespace, Name: req.Name,
		NodeSelector: req.NodeSelector, Tolerations: req.Tolerations,
	})
	// Só labels e taints (identificação): nada sensível no histórico.
	logOperation(c, h.historyTracker, "prometheus_scheduling_fix", req.Namespace+"/"+req.Name, req.Cluster,
		map[string]interface{}{"node_selector": res.BeforeSelector},
		map[string]interface{}{"node_selector": res.AfterSelector, "tolerations_added": res.AddedTolerations, "pods_recreated": res.RecreatedPods},
		start, err)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	h.healthMu.Lock()
	delete(h.healthCache, req.Cluster)
	h.healthMu.Unlock()
	c.JSON(http.StatusOK, res)
}

func newWALOpID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "walclean-" + hex.EncodeToString(b)
}
