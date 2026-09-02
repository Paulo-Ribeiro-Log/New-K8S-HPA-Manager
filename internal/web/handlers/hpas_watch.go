package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"k8s-hpa-manager/internal/config"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/web/sse"
)

// HPAWatchHandler — mesmo mecanismo de PodWatchHandler/DeploymentWatchHandler. Achado real que
// mudou o desenho (mesma decisão tomada com o usuário pra Deployments): ListHPAs enriquece cada
// item via EnrichHPAWithDeploymentResources — um Get() no Deployment associado, pra extrair
// DeploymentName/ImageVersion/recursos configurados. HPAs mudam status com frequência (a cada
// ~15s-1min mesmo sem nada relevante mudar), então reagir a CADA evento de Watch pagando esse
// Get() extra poderia gerar MAIS chamadas que o polling de 30s atual. O Watch aqui só entrega os
// campos "quentes" (réplicas/targets, direto de kubeclient.ConvertHPAToModel — a MESMA conversão
// barata, sem chamada de rede) — DeploymentName/ImageVersion/recursos continuam vindo só do
// polling já existente; o merge campo-a-campo é feito no frontend (useHPAsWatch.ts).
type HPAWatchHandler struct {
	kubeManager *config.KubeConfigManager
	session     *watchSession
}

func NewHPAWatchHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker) *HPAWatchHandler {
	return &HPAWatchHandler{kubeManager: km, session: newWatchSession(tracker)}
}

type startHPAWatchRequest struct {
	Cluster   string `json:"cluster" binding:"required"`
	Namespace string `json:"namespace"` // vazio = cluster inteiro
}

// POST /api/v1/hpa-watch
func (h *HPAWatchHandler) Start(c *gin.Context) {
	var req startHPAWatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

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
		h.runWatch(ctx, clientset, sessionID, req.Cluster, req.Namespace)
	})

	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

func (h *HPAWatchHandler) runWatch(ctx context.Context, clientset kubernetes.Interface, sessionID, cluster, namespace string) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Watch(ctx, options)
		},
	}

	_, informer := cache.NewInformer(lw, &autoscalingv2.HorizontalPodAutoscaler{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { h.publish(sessionID, "added", cluster, obj) },
		UpdateFunc: func(_, newObj interface{}) { h.publish(sessionID, "modified", cluster, newObj) },
		DeleteFunc: func(obj interface{}) { h.publish(sessionID, "deleted", cluster, obj) },
	})

	informer.Run(ctx.Done())
}

func (h *HPAWatchHandler) publish(sessionID, eventType, cluster string, obj interface{}) {
	hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		hpa, ok = tombstone.Obj.(*autoscalingv2.HorizontalPodAutoscaler)
		if !ok {
			return
		}
	}

	model := kubeclient.ConvertHPAToModel(cluster, hpa)
	h.session.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID: sessionID, Type: "hpa_" + eventType, Phase: "in_progress", Timestamp: time.Now(),
		Result: model,
	})
}

// GET /api/v1/hpa-watch/:sessionId
func (h *HPAWatchHandler) Stream(c *gin.Context) { h.session.Stream(c) }

// POST /api/v1/hpa-watch/:sessionId/cancel
func (h *HPAWatchHandler) Cancel(c *gin.Context) { h.session.Cancel(c) }
