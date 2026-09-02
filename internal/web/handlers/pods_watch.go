package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
// aba Pods (PodsPanel.tsx), com fallback automático pro polling de 5s já existente se o Watch não
// conectar ou cair. Ver plano em docs internos da sessão que motivou esta feature: substitui o
// "List() a cada 5s" por eventos empurrados pelo kube-apiserver assim que algo muda de verdade.
//
// Mesmo padrão de pods_logs_stream.go (session_id + context.CancelFunc + goroutine publicando no
// ProgressTracker compartilhado) — copiado quase literalmente, só troca "linha de log" por
// "evento de pod". Usa cache.NewInformer (Reflector do client-go) em vez de um Watch() cru feito
// à mão — o Reflector já resolve sozinho reconexão e o caso "resourceVersion expirado (410 Gone),
// precisa re-listar do zero", relevante dado o histórico real de instabilidade de VPN já
// documentado nesta app.
type PodWatchHandler struct {
	kubeManager *config.KubeConfigManager
	tracker     *sse.ProgressTracker
	podHandler  *PodHandler
	cancelFuncs sync.Map // sessionID -> context.CancelFunc
}

func NewPodWatchHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker, podHandler *PodHandler) *PodWatchHandler {
	return &PodWatchHandler{kubeManager: km, tracker: tracker, podHandler: podHandler}
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

	sessionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancelFuncs.Store(sessionID, cancel)

	go h.runWatch(ctx, clientset, sessionID, req.Cluster, req.Namespace, req.ShowSystem)

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

	// Contexto cancelado (usuário fechou a aba, ou Cancel() explícito) — sinaliza fim pro handler
	// de Stream fechar a conexão HTTP em vez de ficar pendurado (mesmo padrão de
	// pods_logs_stream.go).
	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID: sessionID, Type: "complete", Phase: "completed", Timestamp: time.Now(),
	})
	h.cancelFuncs.Delete(sessionID)
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
	h.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID: sessionID, Type: "pod_" + eventType, Phase: "in_progress", Timestamp: time.Now(),
		Result: summary,
	})
}

// Stream conecta o cliente ao fluxo SSE de um Watch em andamento — mesmo formato de
// pods_logs_stream.go's Stream, exceto que aqui o loop NUNCA para sozinho em eventos "pod_*" (só
// em "complete"/"error"), já que o Watch é contínuo por natureza.
//
// GET /api/v1/pod-watch/:sessionId
func (h *PodWatchHandler) Stream(c *gin.Context) {
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

// Cancel para o Watch de uma sessão — chamado quando a aba fecha ou o hook detecta que precisa
// desligar o Watch (ex: troca de cluster/namespace). Cancela o contexto compartilhado pelo
// Informer daquela sessão, que desbloqueia sozinho o Run(ctx.Done()).
//
// POST /api/v1/pod-watch/:sessionId/cancel
func (h *PodWatchHandler) Cancel(c *gin.Context) {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_SESSION", "sessionId obrigatório"))
		return
	}

	if val, ok := h.cancelFuncs.Load(sessionID); ok {
		val.(context.CancelFunc)()
		h.cancelFuncs.Delete(sessionID)
		c.JSON(http.StatusOK, gin.H{"cancelled": true})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"cancelled": false, "message": "sessão não encontrada ou já finalizada"})
	}
}
