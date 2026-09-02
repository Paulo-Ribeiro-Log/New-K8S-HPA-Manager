package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	appsv1 "k8s.io/api/apps/v1"
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

// DeploymentWatchHandler — mesmo mecanismo de PodWatchHandler (Reflector + SSE via watchSession
// compartilhado), aplicado a Deployments. Achado real que mudou o desenho: diferente de Pods,
// ListDeployments enriquece cada item com UnhealthyPodCount/PodIssueReason cruzando com a lista
// de Pods do namespace (ver CLAUDE.md, "Deployments — Status de pod individual refletido na
// listagem") — reagir a CADA evento de Watch refazendo esse cruzamento pagaria um custo real a
// cada mudança de status. Decisão tomada explicitamente com o usuário: o Watch aqui só entrega os
// campos "quentes" (spec/status/imagem, direto de kubeclient.BuildDeploymentSummary — a MESMA
// conversão barata, sem chamada de rede, que ListDeployments já usa por baixo, ANTES do
// enriquecimento) — UnhealthyPodCount/PodIssueReason continuam vindo só do polling de 60s já
// existente; o merge campo-a-campo é feito no frontend (useDeploymentsWatch.ts).
type DeploymentWatchHandler struct {
	kubeManager *config.KubeConfigManager
	session     *watchSession
}

func NewDeploymentWatchHandler(km *config.KubeConfigManager, tracker *sse.ProgressTracker) *DeploymentWatchHandler {
	return &DeploymentWatchHandler{kubeManager: km, session: newWatchSession(tracker)}
}

type startDeploymentWatchRequest struct {
	Cluster   string `json:"cluster" binding:"required"`
	Namespace string `json:"namespace"` // vazio = cluster inteiro
}

// POST /api/v1/deployment-watch
func (h *DeploymentWatchHandler) Start(c *gin.Context) {
	var req startDeploymentWatchRequest
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

func (h *DeploymentWatchHandler) runWatch(ctx context.Context, clientset kubernetes.Interface, sessionID, cluster, namespace string) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return clientset.AppsV1().Deployments(namespace).List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return clientset.AppsV1().Deployments(namespace).Watch(ctx, options)
		},
	}

	_, informer := cache.NewInformer(lw, &appsv1.Deployment{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { h.publish(sessionID, "added", cluster, obj) },
		UpdateFunc: func(_, newObj interface{}) { h.publish(sessionID, "modified", cluster, newObj) },
		DeleteFunc: func(obj interface{}) { h.publish(sessionID, "deleted", cluster, obj) },
	})

	informer.Run(ctx.Done())
}

func (h *DeploymentWatchHandler) publish(sessionID, eventType, cluster string, obj interface{}) {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		dep, ok = tombstone.Obj.(*appsv1.Deployment)
		if !ok {
			return
		}
	}

	summary := kubeclient.BuildDeploymentSummary(cluster, dep)
	h.session.tracker.SendToClient(sessionID, sse.ProgressEvent{
		ID: sessionID, Type: "deployment_" + eventType, Phase: "in_progress", Timestamp: time.Now(),
		Result: summary,
	})
}

// GET /api/v1/deployment-watch/:sessionId
func (h *DeploymentWatchHandler) Stream(c *gin.Context) { h.session.Stream(c) }

// POST /api/v1/deployment-watch/:sessionId/cancel
func (h *DeploymentWatchHandler) Cancel(c *gin.Context) { h.session.Cancel(c) }
