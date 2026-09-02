package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/web/sse"
)

// PodWatchHandler transmite ao vivo (via Watch do K8s, mesmo mecanismo do k9s) as mudanças de
// estado dos Pods de um cluster/namespace, empurradas pro navegador via SSE — piloto restrito à
// aba Pods (PodsPanel.tsx) e ao drill-down de pods da aba Deployments, com fallback automático
// pro polling já existente se o Watch não conectar ou cair. Ver plano em docs internos da sessão
// que motivou esta feature: substitui o "List() a cada 5s" por eventos empurrados pelo
// kube-apiserver assim que algo muda de verdade.
//
// Mesmo padrão de pods_logs_stream.go (session_id + context.CancelFunc + goroutine publicando no
// ProgressTracker compartilhado) — a parte de sessão/Stream/Cancel foi extraída pro esqueleto
// compartilhado watchSession (watch_common.go, reaproveitado também por Deployments e HPAs). Usa
// cache.NewInformer (Reflector do client-go) em vez de um Watch() cru feito à mão — o Reflector já
// resolve sozinho reconexão e o caso "resourceVersion expirado (410 Gone), precisa re-listar do
// zero", relevante dado o histórico real de instabilidade de VPN já documentado nesta app.
type PodWatchHandler struct {
	kubeManager *config.KubeConfigManager
	session     *watchSession
	podHandler  *PodHandler
}

func NewPodWatchHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, podHandler *PodHandler) *PodWatchHandler {
	return &PodWatchHandler{kubeManager: km, session: newWatchSession(tracker), podHandler: podHandler}
}

type startPodWatchRequest struct {
	Cluster string `json:"cluster" binding:"required"`
	// Namespace vazio = cluster inteiro (todos os namespaces) — mesma convenção de
	// PodsPanel.tsx's namespaceFilter (undefined = todos).
	Namespace  string `json:"namespace"`
	ShowSystem bool   `json:"show_system"`
}

// Start inicia o Watch e devolve um session_id pra o frontend conectar via GET /:sessionId
// (EventSource/SSE). Não bloqueia — o Reflector roda em background, publicando eventos conforme
// chegam.
//
// POST /api/v1/pod-watch
func (h *PodWatchHandler) Start(c *gin.Context) {
	var req startPodWatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	// NÃO usar h.kubeManager.GetClient() — o restConfig cacheado tem Timeout=30s (mesmo motivo já
	// documentado em pods_logs_stream.go), que mataria a conexão de Watch depois de ~28s mesmo com
	// eventos chegando. Clientset próprio, Timeout=0, a partir do mesmo restConfig.
	restConfig, err := h.kubeManager.GetRestConfig(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}
	watchConfig := rest.CopyConfig(restConfig)
	watchConfig.Timeout = 0
	clientset, err := kubernetes.NewForConfig(watchConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", err.Error()))
		return
	}

	sessionID := h.session.start(func(ctx context.Context, sessionID string) {
		h.runWatch(ctx, clientset, sessionID, req.Cluster, req.Namespace, req.ShowSystem)
	})

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// runWatch monta o Reflector (cache.NewInformer) e roda até o contexto ser cancelado. O
// Informer já resolve sozinho reconexão/relist em caso de queda de conexão ou resourceVersion
// expirado — não precisamos reimplementar esse loop à mão.
func (h *PodWatchHandler) runWatch(ctx context.Context, clientset kubernetes.Interface, sessionID, cluster, namespace string, showSystem bool) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return clientset.CoreV1().Pods(namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return clientset.CoreV1().Pods(namespace).Watch(ctx, options)
		},
	}

	_, informer := cache.NewInformer(lw, &corev1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			h.publish(sessionID, "added", cluster, obj, showSystem)
		},
		UpdateFunc: func(_, newObj interface{}) {
			h.publish(sessionID, "modified", cluster, newObj, showSystem)
		},
		DeleteFunc: func(obj interface{}) {
			h.publish(sessionID, "deleted", cluster, obj, showSystem)
		},
	})

	informer.Run(ctx.Done()) // bloqueia até cancelamento
}

// publish converte o objeto do evento (sempre *corev1.Pod, exceto em DeletedFinalStateUnknown —
// caso raro de delete perdido durante uma desconexão, onde o Informer só sabe a chave, não o
// objeto completo) e publica no broker SSE compartilhado, usando a MESMA conversão que
// GET /pods já usa (PodHandler.convertToPodSummary) — nenhum tipo novo no frontend.
func (h *PodWatchHandler) publish(sessionID, eventType, cluster string, obj interface{}, showSystem bool) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			return
		}
	}

	if !showSystem && isSystemNamespace(pod.Namespace) {
		return
	}

	summary := h.podHandler.convertToPodSummary(cluster, pod, nil)
	h.session.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID: sessionID, Type: "pod_" + eventType, Phase: "in_progress", Timestamp: time.Now(),
		Result: summary,
	})
}

// GET /api/v1/pod-watch/:sessionId
func (h *PodWatchHandler) Stream(c *gin.Context) { h.session.Stream(c) }

// POST /api/v1/pod-watch/:sessionId/cancel
func (h *PodWatchHandler) Cancel(c *gin.Context) { h.session.Cancel(c) }
