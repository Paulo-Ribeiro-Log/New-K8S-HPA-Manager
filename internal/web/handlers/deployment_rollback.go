package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/web/sse"
)

// ─── Rollback de Deployment — "Modo K8s nativo" (Deployments não gerenciados pelo Helm) ───────
//
// Pedido explícito do usuário, com o passo a passo `kubectl rollout history/undo/status` como
// referência. Reaproveita ListDeploymentRevisions/RollbackDeploymentToRevision/
// WaitDeploymentRolloutComplete (internal/kubernetes/deployment_rollback.go) — este arquivo é só a
// camada HTTP + streaming SSE (mesmo padrão de NetDiscoveryHandler/Helm operations: POST aplica e
// retorna na hora, sessionId separado só pra acompanhar o "aguardando rollout" via SSE).
//
// Para Deployments Helm-gerenciados, o caminho correto é `POST /helm/releases/:release/rollback`
// (já existente) — nunca este endpoint, que causaria drift do Helm. A decisão de qual caminho usar
// é do FRONTEND (detecta via annotation meta.helm.sh/release-name do manifest já carregado); este
// handler não valida isso — é o cliente da aplicação (React) quem nunca oferece este botão pra um
// Deployment Helm-gerenciado.

// DeploymentRollbackHandler expõe o rollback via ReplicaSet history (equivalente a `kubectl
// rollout undo`).
type DeploymentRollbackHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	inProgress     sync.Map // "cluster/namespace/name" -> struct{} — bloqueia 2 rollbacks concorrentes no mesmo Deployment
}

// NewDeploymentRollbackHandler cria o handler.
func NewDeploymentRollbackHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker) *DeploymentRollbackHandler {
	return &DeploymentRollbackHandler{kubeManager: km, tracker: tracker, historyTracker: ht}
}

func rollbackLockKey(cluster, namespace, name string) string {
	return cluster + "/" + namespace + "/" + name
}

// ListRevisions — GET /api/v1/deployments/:cluster/:namespace/:name/revisions
// Equivalente a `kubectl rollout history deployment/X`.
func (h *DeploymentRollbackHandler) ListRevisions(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)

	revisions, err := kubeClient.ListDeploymentRevisions(c.Request.Context(), namespace, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_REVISIONS_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"revisions": revisions}})
}

// PreviewRevision — GET /api/v1/deployments/:cluster/:namespace/:name/revisions/:revision/preview
// Devolve o YAML de "como ficaria" o Deployment se revertido pra essa revisão — usado pro diff
// visual obrigatório antes de liberar a confirmação no modal (nunca aplica nada).
func (h *DeploymentRollbackHandler) PreviewRevision(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	revisionStr := c.Param("revision")
	revision, err := strconv.ParseInt(revisionStr, 10, 64)
	if err != nil || revision <= 0 {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REVISION", "revision must be a positive integer"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)

	yamlStr, err := kubeClient.GetDeploymentRevisionPreviewYAML(c.Request.Context(), namespace, name, revision)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("PREVIEW_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"yaml": yamlStr}})
}

type deploymentRollbackRequest struct {
	TargetRevision int64  `json:"targetRevision" binding:"required"`
	Reason         string `json:"reason" binding:"required"`
}

// Rollback — POST /api/v1/deployments/:cluster/:namespace/:name/rollback
// Aplica o rollback SINCRONAMENTE (o patch em si é rápido, milissegundos — o usuário vê
// sucesso/falha na hora, sem depender de streaming pra saber se a etapa crítica funcionou) e só
// então inicia o acompanhamento assíncrono do rollout (SSE) — reflete o princípio de "confirmar
// cada passo importante": a confirmação do PATCH em si nunca fica pendente/ambígua.
func (h *DeploymentRollbackHandler) Rollback(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req deploymentRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("REASON_REQUIRED", "motivo do rollback é obrigatório"))
		return
	}

	lockKey := rollbackLockKey(cluster, namespace, name)
	if _, alreadyRunning := h.inProgress.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("ROLLBACK_IN_PROGRESS", "já existe um rollback em andamento para este Deployment"))
		return
	}
	// Só libera o lock quando o rollout TERMINAR de acompanhar (streamRolloutStatus), não aqui —
	// o patch já foi aplicado nesse ponto, mas o Deployment ainda está "em rollback" até o
	// controller convergir; ver defer dentro da goroutine abaixo.

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		h.inProgress.Delete(lockKey)
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)
	ctx := c.Request.Context()

	userEmail := c.GetString("user_email")
	changeCause := fmt.Sprintf("Rollback via K8s HPA Manager para revisão %d (usuário: %s, motivo: %s)",
		req.TargetRevision, userEmail, strings.TrimSpace(req.Reason))

	start := time.Now()
	before, _ := kubeClient.GetDeployment(ctx, namespace, name) // best-effort — só pra auditoria, nunca bloqueia o rollback

	updated, err := kubeClient.RollbackDeploymentToRevision(ctx, namespace, name, req.TargetRevision, changeCause)
	if err != nil {
		h.inProgress.Delete(lockKey)
		status := http.StatusInternalServerError
		code := "ROLLBACK_ERROR"
		if _, ok := err.(*kubeclient.ErrRollbackSameRevision); ok {
			status = http.StatusConflict
			code = "SAME_REVISION"
		}
		if h.historyTracker != nil {
			entry := CreateHistoryEntry(c, "rollback_deployment", fmt.Sprintf("%s/%s", namespace, name), cluster, "failed", nil, nil, time.Since(start).Milliseconds(), err.Error())
			_ = h.historyTracker.Log(entry)
		}
		c.JSON(status, errorResponse(code, err.Error()))
		return
	}

	if h.historyTracker != nil {
		var beforeMap map[string]interface{}
		if before != nil {
			beforeMap = deploymentManifestToHistoryMap(before)
		}
		afterMap := map[string]interface{}{
			"targetRevision": req.TargetRevision,
			"reason":         req.Reason,
			"images":         deploymentImages(updated),
		}
		entry := CreateHistoryEntry(c, "rollback_deployment", fmt.Sprintf("%s/%s", namespace, name), cluster, "success", beforeMap, afterMap, time.Since(start).Milliseconds(), "")
		_ = h.historyTracker.Log(entry)
	}

	sessionID := fmt.Sprintf("deploy-rollback-%s", uuid.New().String())
	// context.Background() — deliberado: a requisição HTTP retorna já a seguir, mas o
	// acompanhamento do rollout precisa sobreviver além da conexão original (mesmo princípio do
	// SSE de outras ferramentas desta app, ex: Helm operations). Limitado internamente pelo
	// timeout de 5min de WaitDeploymentRolloutComplete — nunca roda pra sempre.
	go h.streamRolloutStatus(context.Background(), kubeClient, lockKey, cluster, namespace, name, sessionID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"sessionId": sessionID,
			"images":    deploymentImages(updated),
		},
	})
}

func deploymentImages(dep *appsv1.Deployment) []string {
	images := make([]string, 0, len(dep.Spec.Template.Spec.Containers))
	for _, ctr := range dep.Spec.Template.Spec.Containers {
		images = append(images, ctr.Image)
	}
	return images
}

// streamRolloutStatus acompanha o rollout via polling (WaitDeploymentRolloutComplete) e publica
// cada tick como evento SSE — mesmo padrão de progresso nunca-silencioso já usado por
// Cordon/Drain/Health Check/Net Discovery nesta app.
func (h *DeploymentRollbackHandler) streamRolloutStatus(ctx context.Context, kubeClient *kubeclient.Client, lockKey, cluster, namespace, name, sessionID string) {
	defer h.inProgress.Delete(lockKey)

	send := func(evtType, phase, message string, progress float64, result interface{}) {
		h.tracker.SendToClient(sessionID, sse.ProgressEvent{
			ID:        sessionID,
			Type:      evtType,
			Phase:     phase,
			Message:   message,
			Progress:  progress,
			Timestamp: time.Now(),
			Cluster:   cluster,
			Result:    result,
		})
	}

	send("init", "started", "Aguardando rollout...", 0.05, nil)

	err := kubeClient.WaitDeploymentRolloutComplete(ctx, namespace, name, func(s kubeclient.DeploymentRolloutStatus) {
		progress := 0.1
		if s.DesiredReplicas > 0 {
			progress = 0.1 + 0.85*float64(s.AvailableReplicas)/float64(s.DesiredReplicas)
		}
		if progress > 0.95 {
			progress = 0.95
		}
		send("rollout", "in_progress",
			fmt.Sprintf("%d/%d réplicas atualizadas, %d/%d disponíveis", s.UpdatedReplicas, s.DesiredReplicas, s.AvailableReplicas, s.DesiredReplicas),
			progress, s)
	})
	if err != nil {
		send("error", "failed", err.Error(), 1.0, nil)
		return
	}
	send("complete", "completed", fmt.Sprintf("Deployment %s/%s revertido e disponível com sucesso", namespace, name), 1.0, nil)
}

// Stream — GET /api/v1/deployments/rollback/stream/:sessionId
// Mesmo padrão exato de NetDiscoveryHandler.Stream (replay de eventos perdidos + canal ao vivo).
func (h *DeploymentRollbackHandler) Stream(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	for _, evt := range h.tracker.GetReplayEvents(sessionID) {
		if _, err := c.Writer.WriteString(sse.FormatSSE(evt)); err != nil {
			return
		}
		c.Writer.Flush()
	}

	client := sse.NewClient(sessionID)
	h.tracker.AddClient(client)
	defer h.tracker.RemoveClient(sessionID)

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
