package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/appremoval"
	helmservice "k8s-hpa-manager/internal/helm"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/pkg/helm"
	"k8s-hpa-manager/internal/web/sse"
)

const appRemovalMaxDuration = 30 * time.Minute

// AppRemovalHandler implementa a ferramenta "Remoção de Aplicação" (Tools menu): lista alvos,
// escaneia as dependências e remove a seleção do usuário (dry-run opcional), com progresso por SSE.
type AppRemovalHandler struct {
	helmService    *helmservice.Service
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	store          *appremoval.Store
	active         sync.Map // "cluster/namespace/app" -> struct{}: uma execução por aplicação
}

func NewAppRemovalHandler(hs *helmservice.Service, tracker *sse.ProgressTracker, ht *history.HistoryTracker) *AppRemovalHandler {
	return &AppRemovalHandler{helmService: hs, tracker: tracker, historyTracker: ht, store: appremoval.NewStore()}
}

// Candidates lista workloads e releases Helm do namespace.
func (h *AppRemovalHandler) Candidates(c *gin.Context) {
	cluster, namespace := c.Param("cluster"), c.Param("namespace")
	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster e namespace são obrigatórios"))
		return
	}
	items, err := appremoval.Candidates(c.Request.Context(), cluster, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"candidates": items})
}

// Scan descobre tudo o que pertence à aplicação (nome de workload ou de release Helm).
func (h *AppRemovalHandler) Scan(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace e nome são obrigatórios"))
		return
	}
	result, err := scanApp(c.Request.Context(), cluster, namespace, name)
	if errors.Is(err, appremoval.ErrNotFound) {
		c.JSON(http.StatusNotFound, errorResponse("NOT_FOUND", err.Error()))
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("SCAN_ERROR", err.Error()))
		return
	}
	h.store.Put(cluster, result)
	c.JSON(http.StatusOK, result)
}

func scanApp(ctx context.Context, cluster, namespace, name string) (*appremoval.Result, error) {
	resources, err := appremoval.Collect(ctx, cluster, namespace)
	if err != nil {
		return nil, err
	}
	return appremoval.Scan(namespace, name, resources)
}

type executeAppRemovalRequest struct {
	Cluster     string              `json:"cluster" binding:"required"`
	Namespace   string              `json:"namespace" binding:"required"`
	Name        string              `json:"name" binding:"required"`
	Items       []appremoval.Target `json:"items"`
	DryRun      bool                `json:"dry_run"`
	ConfirmName string              `json:"confirm_name"`
	// ScanID: scan (ou verificação) guardado no servidor que serve de base à seleção. Sem ele, a
	// execução faz um scan novo.
	ScanID string `json:"scan_id"`
}

// Execute valida a seleção contra um scan novo e inicia a remoção em background. O progresso
// sai por SSE em /app-removal-stream/:operationId.
// POST /api/v1/app-removal/execute
func (h *AppRemovalHandler) Execute(c *gin.Context) {
	var req executeAppRemovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if !req.DryRun && req.ConfirmName != req.Name {
		c.JSON(http.StatusBadRequest, errorResponse("CONFIRMATION_MISMATCH",
			"digite o nome da aplicação para confirmar a remoção"))
		return
	}

	scan, ok := h.executionBasis(c, req)
	if !ok {
		return
	}
	plan, err := appremoval.Plan(scan, req.Items)
	switch {
	case errors.Is(err, appremoval.ErrStale):
		c.JSON(http.StatusConflict, errorResponse("STALE_SCAN", err.Error()+": escaneie novamente"))
		return
	case err != nil:
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_SELECTION", err.Error()))
		return
	}

	lock := fmt.Sprintf("%s/%s/%s", req.Cluster, req.Namespace, req.Name)
	if _, busy := h.active.LoadOrStore(lock, struct{}{}); busy {
		c.JSON(http.StatusConflict, errorResponse("ALREADY_RUNNING", "já existe uma remoção em andamento para esta aplicação"))
		return
	}

	operationID := newAppRemovalOperationID()
	// Autor capturado agora: o *gin.Context é reaproveitado depois que a requisição termina.
	userCtx := history.DetachedUserContext(c)
	go h.run(userCtx, lock, operationID, req, scan, plan)

	c.JSON(http.StatusAccepted, gin.H{"operation_id": operationID, "total": len(plan), "dry_run": req.DryRun})
}

// executionBasis devolve o scan que valida a seleção: o guardado (scan_id) ou, sem ele, um scan novo.
// Responde ao cliente e devolve ok=false em qualquer falha.
func (h *AppRemovalHandler) executionBasis(c *gin.Context, req executeAppRemovalRequest) (*appremoval.Result, bool) {
	if req.ScanID != "" {
		cluster, scan, found := h.store.Get(req.ScanID)
		if !found {
			c.JSON(http.StatusNotFound, errorResponse("SCAN_EXPIRED", "scan expirado ou desconhecido: escaneie novamente"))
			return nil, false
		}
		if cluster != req.Cluster || scan.Namespace != req.Namespace || scan.Name != req.Name {
			c.JSON(http.StatusBadRequest, errorResponse("SCAN_MISMATCH", "o scan não pertence a esta aplicação"))
			return nil, false
		}
		return scan, true
	}
	scan, err := scanApp(c.Request.Context(), req.Cluster, req.Namespace, req.Name)
	if errors.Is(err, appremoval.ErrNotFound) {
		c.JSON(http.StatusNotFound, errorResponse("NOT_FOUND", err.Error()))
		return nil, false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("SCAN_ERROR", err.Error()))
		return nil, false
	}
	return scan, true
}

type verifyAppRemovalRequest struct {
	ScanID string `json:"scan_id" binding:"required"`
}

const (
	verifyAttempts = 3
	verifyInterval = 3 * time.Second
)

// Verify confere o namespace contra o scan guardado: o que foi removido, o que sobrou, o que está
// em Terminating, o que foi recriado e o que apareceu de novo. As sobras viram um novo scan_id,
// base de uma nova rodada de remoção. Só lê o cluster.
// POST /api/v1/app-removal/verify
func (h *AppRemovalHandler) Verify(c *gin.Context) {
	var req verifyAppRemovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	cluster, original, found := h.store.Get(req.ScanID)
	if !found {
		c.JSON(http.StatusNotFound, errorResponse("SCAN_EXPIRED", "scan expirado ou desconhecido: escaneie novamente"))
		return
	}

	// Itens em Terminating costumam sumir em segundos: espera um pouco antes de reportar.
	var result *appremoval.VerifyResult
	for attempt := 1; attempt <= verifyAttempts; attempt++ {
		resources, err := appremoval.Collect(c.Request.Context(), cluster, original.Namespace)
		if err != nil {
			c.JSON(http.StatusInternalServerError, errorResponse("VERIFY_ERROR", err.Error()))
			return
		}
		result = appremoval.Verify(original, resources)
		if !result.Settling() || attempt == verifyAttempts {
			break
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-time.After(verifyInterval):
		}
	}
	if result.Leftovers != nil {
		h.store.Put(cluster, result.Leftovers)
	}
	c.JSON(http.StatusOK, result)
}

func (h *AppRemovalHandler) run(userCtx context.Context, lock, operationID string, req executeAppRemovalRequest, scan *appremoval.Result, plan []appremoval.Target) {
	defer h.active.Delete(lock)
	ctx, cancel := context.WithTimeout(context.Background(), appRemovalMaxDuration)
	defer cancel()

	origin := map[string]appremoval.Origin{}
	for _, it := range scan.Items {
		origin[it.Kind+"/"+it.Name] = it.Origin
	}

	h.send(operationID, sse.ProgressEvent{
		Type: "init", Phase: "started", Progress: 0,
		Message: fmt.Sprintf("%d itens na fila", len(plan)),
	})

	runner := appremoval.KubectlRunner{Uninstall: h.helmUninstall}
	start := time.Now()
	last := start
	outcomes := appremoval.Run(ctx, runner, req.Cluster, req.Namespace, plan, req.DryRun, func(o appremoval.Outcome, done, total int) {
		phase := "completed"
		if o.Status == appremoval.StatusFailed {
			phase = "failed"
		}
		h.send(operationID, sse.ProgressEvent{
			Type: "item", Phase: phase, Progress: float64(done) / float64(total),
			Message: fmt.Sprintf("%s/%s: %s", o.Kind, o.Name, o.Status), Error: o.Message, Result: o,
		})
		if !req.DryRun && (o.Status == appremoval.StatusDeleted || o.Status == appremoval.StatusFailed) {
			h.logItem(userCtx, req, o, origin[o.Kind+"/"+o.Name], time.Since(last))
		}
		last = time.Now()
	})

	failed := false
	for _, o := range outcomes {
		failed = failed || o.Status == appremoval.StatusFailed
	}
	h.send(operationID, sse.ProgressEvent{
		Type: "complete", Phase: "completed", Progress: 1,
		Message: "execução finalizada",
		Result:  gin.H{"dry_run": req.DryRun, "failed": failed, "outcomes": outcomes},
	})
}

// logItem grava no Change History só identificação (tipo, nome, UID, origem); nunca conteúdo.
func (h *AppRemovalHandler) logItem(ctx context.Context, req executeAppRemovalRequest, o appremoval.Outcome, origin appremoval.Origin, took time.Duration) {
	if h.historyTracker == nil {
		return
	}
	status, msg := history.StatusSuccess, ""
	if o.Status == appremoval.StatusFailed {
		status, msg = history.StatusFailed, o.Message
	}
	_ = h.historyTracker.Log(ctx, history.HistoryEntry{
		Action:   "app_removal",
		Resource: o.Kind + "/" + o.Name,
		Cluster:  req.Cluster,
		Before:   map[string]interface{}{"namespace": req.Namespace, "application": req.Name, "origin": string(origin)},
		After:    map[string]interface{}{"status": o.Status},
		Status:   status,
		ErrorMsg: msg,
		Duration: took.Milliseconds(),
	})
}

func (h *AppRemovalHandler) helmUninstall(ctx context.Context, cluster, namespace, release string, dryRun bool) error {
	if h.helmService == nil {
		return errors.New("serviço Helm indisponível")
	}
	_, err := h.helmService.Execute(ctx, cluster, helm.HelmActionRequest{
		Namespace:   namespace,
		ReleaseName: release,
		Action:      helm.ActionUninstall,
		DryRun:      dryRun,
	})
	return err
}

func (h *AppRemovalHandler) send(operationID string, e sse.ProgressEvent) {
	if h.tracker == nil {
		return
	}
	e.ID = operationID
	e.Timestamp = time.Now()
	h.tracker.SendToClient(operationID, e)
}

// Stream envia o progresso de uma remoção (SSE). Rota atrás do WebSocketJWTAuthMiddleware (?token=).
// GET /api/v1/app-removal-stream/:operationId
func (h *AppRemovalHandler) Stream(c *gin.Context) {
	operationID := c.Param("operationId")
	if operationID == "" || h.tracker == nil {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_OPERATION", "operationId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range h.tracker.GetReplayEvents(operationID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
		if evt.Type == "complete" || evt.Type == "error" {
			return
		}
	}

	client := sse.NewClient(operationID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(operationID)

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.Channel:
			if !ok {
				return
			}
			if _, err := c.Writer.WriteString(sse.FormatSSE(event)); err != nil {
				return
			}
			c.Writer.Flush()
			if event.Type == "complete" || event.Type == "error" {
				return
			}
		}
	}
}

func newAppRemovalOperationID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return "app-removal-" + hex.EncodeToString(buf)
}
