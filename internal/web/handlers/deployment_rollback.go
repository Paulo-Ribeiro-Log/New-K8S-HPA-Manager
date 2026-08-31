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
	"github.com/rs/zerolog"
	appsv1 "k8s.io/api/apps/v1"

	"k8s-hpa-manager/internal/config"
	helmservice "k8s-hpa-manager/internal/helm"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	helm "k8s-hpa-manager/internal/pkg/helm"
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
// Para Deployments Helm-gerenciados, o caminho equivalente pro Modo K8s nativo/Imagem/Nexus/
// Arquivos (Rollback/SetImage/ApplyManifest abaixo) NUNCA é usado — causaria drift do Helm. A
// decisão de qual caminho usar é do FRONTEND (detecta via annotation meta.helm.sh/release-name do
// manifest já carregado). O Modo Helm em si (HelmRollbackWithBypass, mais abaixo) vive neste mesmo
// arquivo/handler — wrapper dedicado em volta do `helmService.Execute` já usado pela aba Helm
// genérica (POST /helm/releases/:release/rollback), só que com o bypass Kyverno (ver
// kyverno_bypass.go) automatizado em volta, sem afetar o fluxo da aba Helm.

// DeploymentRollbackHandler expõe o rollback via ReplicaSet history (equivalente a `kubectl
// rollout undo`), o patch de imagem (Modo Imagem/Spinnaker), o apply de manifesto histórico (Modo
// Nexus/Arquivos) e o wrapper de `helm rollback` com bypass Kyverno (Modo Helm).
type DeploymentRollbackHandler struct {
	kubeManager    *config.KubeConfigManager
	tracker        *sse.ProgressTracker
	historyTracker *history.HistoryTracker
	logger         *zerolog.Logger
	inProgress     sync.Map // "cluster/namespace/name" -> struct{} — bloqueia 2 rollbacks concorrentes no mesmo Deployment
	// kyvernoBypassRefs — contador de referência por "cluster/namespace" (ver withKyvernoBypass,
	// kyverno_bypass.go): evita remover a label de bypass no meio de outra mutação concorrente
	// (Deployment DIFERENTE, mesmo namespace) que ainda depende dela.
	kyvernoBypassRefs sync.Map
	// helmService — opcional, injetado via SetHelmService depois da construção (ordem de init em
	// server.go: helmService só existe depois deste handler já ter sido criado, evita reordenar um
	// bloco grande de código só por essa dependência). nil-safe: HelmRollbackWithBypass responde
	// 503 se ainda não foi injetado.
	helmService *helmservice.Service
}

// NewDeploymentRollbackHandler cria o handler.
func NewDeploymentRollbackHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, ht *history.HistoryTracker, logger *zerolog.Logger) *DeploymentRollbackHandler {
	return &DeploymentRollbackHandler{kubeManager: km, tracker: tracker, historyTracker: ht, logger: logger}
}

// SetHelmService injeta a dependência do serviço Helm — chamado uma vez em server.go, depois que
// helmService é construído (ver comentário do campo helmService acima).
func (h *DeploymentRollbackHandler) SetHelmService(hs *helmservice.Service) {
	h.helmService = hs
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

	var updated *appsv1.Deployment
	err = h.withKyvernoBypass(ctx, kubeClient, cluster, namespace, func() error {
		var innerErr error
		updated, innerErr = kubeClient.RollbackDeploymentToRevision(ctx, namespace, name, req.TargetRevision, changeCause)
		return innerErr
	})
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

type deploymentSetImageRequest struct {
	Images map[string]string `json:"images" binding:"required"` // containerName -> image:tag
	Reason string            `json:"reason" binding:"required"`
}

// SetImage — POST /api/v1/deployments/:cluster/:namespace/:name/set-image
// "Modo Imagem" do Rollback — troca só a imagem de um ou mais containers (equivalente a `kubectl
// set image`), sem tocar em mais nada do manifesto. Item 1 do procedimento interno de rollback
// manual desta empresa: o método mais simples/rápido, só válido quando NENHUM manifesto
// (ConfigMap/Ingress/Deployment/Service) foi alterado desde a versão-alvo. Mesmo padrão de
// lock+auditoria+streaming de rollout de Rollback acima — reaproveita streamRolloutStatus, já que
// trocar a imagem dispara um rollout de verdade (novo ReplicaSet), igual a um rollback nativo.
func (h *DeploymentRollbackHandler) SetImage(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req deploymentSetImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if len(req.Images) == 0 {
		c.JSON(http.StatusBadRequest, errorResponse("IMAGES_REQUIRED", "informe ao menos uma imagem"))
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

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		h.inProgress.Delete(lockKey)
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)
	ctx := c.Request.Context()

	start := time.Now()
	before, _ := kubeClient.GetDeployment(ctx, namespace, name) // best-effort — só pra auditoria

	var updated *appsv1.Deployment
	err = h.withKyvernoBypass(ctx, kubeClient, cluster, namespace, func() error {
		var innerErr error
		updated, innerErr = kubeClient.SetDeploymentContainerImages(ctx, namespace, name, req.Images)
		return innerErr
	})
	if err != nil {
		h.inProgress.Delete(lockKey)
		if h.historyTracker != nil {
			entry := CreateHistoryEntry(c, "rollback_deployment_image", fmt.Sprintf("%s/%s", namespace, name), cluster, "failed", nil, nil, time.Since(start).Milliseconds(), err.Error())
			_ = h.historyTracker.Log(entry)
		}
		c.JSON(http.StatusInternalServerError, errorResponse("SET_IMAGE_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		var beforeMap map[string]interface{}
		if before != nil {
			beforeMap = deploymentManifestToHistoryMap(before)
		}
		afterMap := map[string]interface{}{
			"images": req.Images,
			"reason": req.Reason,
		}
		entry := CreateHistoryEntry(c, "rollback_deployment_image", fmt.Sprintf("%s/%s", namespace, name), cluster, "success", beforeMap, afterMap, time.Since(start).Milliseconds(), "")
		_ = h.historyTracker.Log(entry)
	}

	sessionID := fmt.Sprintf("deploy-rollback-%s", uuid.New().String())
	go h.streamRolloutStatus(context.Background(), kubeClient, lockKey, cluster, namespace, name, sessionID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"sessionId": sessionID,
			"images":    deploymentImages(updated),
		},
	})
}

type deploymentApplyManifestRequest struct {
	YAML   string `json:"yaml" binding:"required"`
	Reason string `json:"reason" binding:"required"`
	Force  bool   `json:"force"`
}

// ApplyManifest — POST /api/v1/deployments/:cluster/:namespace/:name/apply-manifest
// Usado pelos Modos Nexus e Arquivos — aplica um manifesto Deployment já extraído (kubectl apply
// via server-side apply, mesmo ApplyDeployment genérico já usado pelo editor YAML normal da aba
// Deployments), mas com o mesmo lock+auditoria+streaming de rollout dos outros modos.
//
// Achado real, relatado pelo usuário: "essas opções [--wait/--recreate-pods] não existem para
// nexus ou para o modo manual?" — antes desta correção, NexusRollbackSection nem chamava
// useRollbackProgress() (aplicava e simplesmente esperava dar certo, sem visibilidade nenhuma se
// os pods ficaram saudáveis), e FileRollbackSection já tinha o guard progress.active/progress.done
// mas nunca chamava progress.start() de fato (código morto). --wait/--recreate-pods são flags
// específicas do `helm rollback` sem equivalente direto num kubectl apply cru — --recreate-pods
// não faz sentido aqui (um apply que muda o PodTemplateSpec já dispara rollout sozinho, sem
// precisar forçar) — mas o VALOR real por trás de --wait (saber se os pods realmente ficaram
// prontos antes de considerar sucesso) é igualmente válido aqui. Corrigido reaproveitando
// streamRolloutStatus (a mesma função já usada por Rollback/SetImage) — reaplicar um manifesto
// histórico É um rollback de verdade, merece a mesma visibilidade de rollout que os outros modos.
func (h *DeploymentRollbackHandler) ApplyManifest(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req deploymentApplyManifestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("YAML_REQUIRED", "yaml é obrigatório"))
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

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		h.inProgress.Delete(lockKey)
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)
	ctx := c.Request.Context()

	start := time.Now()
	before, _ := kubeClient.GetDeployment(ctx, namespace, name) // best-effort — só pra auditoria

	var updated *appsv1.Deployment
	err = h.withKyvernoBypass(ctx, kubeClient, cluster, namespace, func() error {
		var innerErr error
		updated, innerErr = kubeClient.ApplyDeployment(ctx, req.YAML, "k8s-hpa-manager-rollback-manifest", namespace, name, false, req.Force)
		return innerErr
	})
	if err != nil {
		h.inProgress.Delete(lockKey)
		if h.historyTracker != nil {
			entry := CreateHistoryEntry(c, "rollback_deployment_manifest", fmt.Sprintf("%s/%s", namespace, name), cluster, "failed", nil, nil, time.Since(start).Milliseconds(), err.Error())
			_ = h.historyTracker.Log(entry)
		}
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		var beforeMap map[string]interface{}
		if before != nil {
			beforeMap = deploymentManifestToHistoryMap(before)
		}
		afterMap := map[string]interface{}{
			"images": deploymentImages(updated),
			"reason": req.Reason,
		}
		entry := CreateHistoryEntry(c, "rollback_deployment_manifest", fmt.Sprintf("%s/%s", namespace, name), cluster, "success", beforeMap, afterMap, time.Since(start).Milliseconds(), "")
		_ = h.historyTracker.Log(entry)
	}

	sessionID := fmt.Sprintf("deploy-rollback-%s", uuid.New().String())
	go h.streamRolloutStatus(context.Background(), kubeClient, lockKey, cluster, namespace, name, sessionID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"sessionId": sessionID,
			"images":    deploymentImages(updated),
		},
	})
}

type deploymentHelmRollbackRequest struct {
	Release          string `json:"release" binding:"required"`
	ReleaseNamespace string `json:"releaseNamespace"`
	TargetRevision   int    `json:"targetRevision" binding:"required"`
	Wait             bool   `json:"wait"`
	RecreatePods     bool   `json:"recreatePods"`
	Reason           string `json:"reason" binding:"required"`
}

// HelmRollbackWithBypass — POST /api/v1/deployments/:cluster/:namespace/:name/helm-rollback
// "Modo Helm" do Rollback de Deployment — wrapper DEDICADO em volta de helmService.Execute (o
// mesmo motor já usado por POST /helm/releases/:release/rollback, a rota genérica da aba Helm) —
// nunca toca essa rota genérica nem seu comportamento, só reaproveita o mesmo serviço.
//
// Achado real, relatado pelo usuário depois de testar contra um cluster real: "o kyverno bloqueia
// qualquer tentativa de deployments fora da esteira, o que daria problema principalmente com o
// modo manual... acredito que ele precisa ser usada em qualquer modo que for executar o rollback".
// Confirmado também pelo usuário: em cluster Helm-gerenciado, `--force` é adicionalmente
// obrigatório pra permitir a alteração manual ("é o próprio --force que permite alterações manuais
// em ambiente helm managed") — por isso este endpoint SEMPRE manda Force:true, nunca aceita false
// (diferente do checkbox opcional "Forçar" da rota genérica da aba Helm, que continua intocada).
//
// Kyverno intercepta a admissão da mutação (o que o `helm rollback` de fato aplica no cluster), não
// o subprocesso em si — por isso basta envolver a chamada síncrona a helmService.Execute com o
// mesmo withKyvernoBypass (ver kyverno_bypass.go) já usado por Rollback/SetImage/ApplyManifest.
// helmService.Execute é SÍNCRONO (bloqueia até o subprocesso `helm rollback` terminar —
// StreamOperation só replaya o resultado já concluído, não é streaming assíncrono de verdade, ver
// internal/pkg/helm/cli_client.go) — não há fase de "aguardando rollout" separada como em
// Rollback/SetImage/ApplyManifest (que watcham o rollout via ReplicaSet depois de retornar); o
// lock (h.inProgress) é liberado no fim desta própria função, nunca passado pra streamRolloutStatus.
func (h *DeploymentRollbackHandler) HelmRollbackWithBypass(c *gin.Context) {
	cluster, namespace, name := c.Param("cluster"), c.Param("namespace"), c.Param("name")
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}
	if h.helmService == nil {
		c.JSON(http.StatusServiceUnavailable, errorResponse("HELM_UNAVAILABLE", "serviço Helm indisponível"))
		return
	}

	var req deploymentHelmRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("REASON_REQUIRED", "motivo do rollback é obrigatório"))
		return
	}

	releaseNamespace := strings.TrimSpace(req.ReleaseNamespace)
	if releaseNamespace == "" {
		releaseNamespace = namespace
	}

	lockKey := rollbackLockKey(cluster, namespace, name)
	if _, alreadyRunning := h.inProgress.LoadOrStore(lockKey, struct{}{}); alreadyRunning {
		c.JSON(http.StatusConflict, errorResponse("ROLLBACK_IN_PROGRESS", "já existe um rollback em andamento para este Deployment"))
		return
	}
	defer h.inProgress.Delete(lockKey) // síncrono — nunca passa a bola pro streamRolloutStatus

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	kubeClient := kubeclient.NewClient(clientset, cluster)
	ctx := c.Request.Context()

	start := time.Now()
	var resp *helm.HelmActionResponse
	err = h.withKyvernoBypass(ctx, kubeClient, cluster, releaseNamespace, func() error {
		var innerErr error
		resp, innerErr = h.helmService.Execute(ctx, cluster, helm.HelmActionRequest{
			Namespace:      releaseNamespace,
			ReleaseName:    req.Release,
			Action:         helm.ActionRollback,
			TargetRevision: req.TargetRevision,
			Force:          true, // sempre — obrigatório neste cenário, confirmado pelo usuário
			Wait:           req.Wait,
			RecreatePods:   req.RecreatePods,
		})
		return innerErr
	})

	if err != nil {
		if h.historyTracker != nil {
			entry := CreateHistoryEntry(c, "rollback_deployment_helm", fmt.Sprintf("%s/%s", namespace, name), cluster, "failed", nil, nil, time.Since(start).Milliseconds(), err.Error())
			_ = h.historyTracker.Log(entry)
		}
		c.JSON(http.StatusInternalServerError, errorResponse("HELM_ROLLBACK_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		afterMap := map[string]interface{}{
			"release":        req.Release,
			"targetRevision": req.TargetRevision,
			"reason":         req.Reason,
		}
		entry := CreateHistoryEntry(c, "rollback_deployment_helm", fmt.Sprintf("%s/%s", namespace, name), cluster, "success", nil, afterMap, time.Since(start).Milliseconds(), "")
		_ = h.historyTracker.Log(entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
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
