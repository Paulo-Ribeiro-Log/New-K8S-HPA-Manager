package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/portforward"
)

// PortForwardHandler expõe o gerenciador de sessões de port-forward de pods (internal/portforward)
// via API REST + histórico de auditoria — ferramenta genérica no menu "Port Forward" (modal),
// pra encaminhar QUALQUER porta de QUALQUER pod. Não confundir com o mecanismo em
// internal/web/handlers/portforward.go (nome de arquivo já ocupado por infraestrutura antiga e
// não relacionada, específica do pod do Kiali via `kubectl port-forward` como subprocesso — ver
// PortForwardManager/GetKialiLocalURL naquele arquivo, mantido intocado). Ver
// PORT-FORWARD-PLAN.md e a seção "Port Forward" do CLAUDE.md pro desenho completo.
type PortForwardHandler struct {
	manager        *portforward.Manager
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

func NewPortForwardHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *PortForwardHandler {
	return &PortForwardHandler{
		manager:        portforward.NewManager(km),
		kubeManager:    km,
		historyTracker: ht,
	}
}

// Manager expõe o *portforward.Manager subjacente — usado por server.go pra plugar StopAll() nos
// 3 caminhos de shutdown (mesmo padrão de teams.CloseBrowser()).
func (h *PortForwardHandler) Manager() *portforward.Manager {
	return h.manager
}

// PodContainerPort é uma porta declarada por um container do pod — usada pro seletor de "portas
// conhecidas" no frontend (sugestão, não obrigatório — o usuário sempre pode digitar qualquer
// porta manualmente).
type PodContainerPort struct {
	Container string `json:"container"`
	Port      int32  `json:"port"`
	Name      string `json:"name,omitempty"`
	Protocol  string `json:"protocol"`
}

// GetPodPorts — GET /api/v1/portforward/pod-ports?cluster=&namespace=&pod=
// Lista as portas declaradas nos containers do pod (containerPort), pra popular o seletor de
// "porta conhecida" no modal — puramente uma sugestão de UX, o backend aceita qualquer porta
// válida em Start independente de estar nesta lista (algumas apps escutam em portas não
// declaradas no manifest).
func (h *PortForwardHandler) GetPodPorts(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	namespace := strings.TrimSpace(c.Query("namespace"))
	podName := strings.TrimSpace(c.Query("pod"))
	if cluster == "" || namespace == "" || podName == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMS", "cluster, namespace e pod são obrigatórios"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("CLUSTER_ERROR", err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("POD_GET_ERROR", err.Error()))
		return
	}

	ports := make([]PodContainerPort, 0)
	for _, ct := range pod.Spec.Containers {
		for _, p := range ct.Ports {
			ports = append(ports, PodContainerPort{
				Container: ct.Name,
				Port:      p.ContainerPort,
				Name:      p.Name,
				Protocol:  string(p.Protocol),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "ports": ports, "phase": string(pod.Status.Phase)})
}

// StartPodPortForwardRequest — corpo de POST /api/v1/portforward/start.
type StartPodPortForwardRequest struct {
	Cluster     string `json:"cluster" binding:"required"`
	Namespace   string `json:"namespace" binding:"required"`
	Pod         string `json:"pod" binding:"required"`
	Container   string `json:"container"`
	Workload    string `json:"workload"`
	RemotePort  int    `json:"remote_port" binding:"required"`
	LocalPort   int    `json:"local_port"`
	BindAddress string `json:"bind_address"`
	Label       string `json:"label"`
}

// Start — POST /api/v1/portforward/start (RequireSREGroup — abre acesso de rede real ao pod).
func (h *PortForwardHandler) Start(c *gin.Context) {
	var req StartPodPortForwardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	userInfo := GetUserInfoForHistory(c)
	startedAt := time.Now()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	info, err := h.manager.Start(ctx, portforward.StartOptions{
		Cluster:     req.Cluster,
		Namespace:   req.Namespace,
		Pod:         req.Pod,
		Container:   req.Container,
		Workload:    req.Workload,
		RemotePort:  req.RemotePort,
		LocalPort:   req.LocalPort,
		BindAddress: req.BindAddress,
		Label:       req.Label,
		CreatedBy:   userInfo.Email,
	})

	if h.historyTracker != nil {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = err.Error()
		}
		h.historyTracker.Log(history.HistoryEntry{
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "portforward_start",
			Resource:  req.Namespace + "/" + req.Pod,
			Cluster:   req.Cluster,
			Status:    status,
			ErrorMsg:  errMsg,
			Duration:  time.Since(startedAt).Milliseconds(),
			After: map[string]interface{}{
				"remote_port": req.RemotePort,
				"local_port":  info.LocalPort,
				"bind":        info.BindAddress,
			},
		})
	}

	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse("PORTFORWARD_START_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "session": info})
}

// Stop — POST /api/v1/portforward/stop/:id (RequireSREGroup).
func (h *PortForwardHandler) Stop(c *gin.Context) {
	id := c.Param("id")
	before, existed := h.manager.Get(id)

	err := h.manager.Stop(id, "")

	if h.historyTracker != nil && existed {
		userInfo := GetUserInfoForHistory(c)
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failed"
			errMsg = err.Error()
		}
		h.historyTracker.Log(history.HistoryEntry{
			UserEmail: userInfo.Email,
			UserName:  userInfo.Name,
			Action:    "portforward_stop",
			Resource:  before.Namespace + "/" + before.Pod,
			Cluster:   before.Cluster,
			Status:    status,
			ErrorMsg:  errMsg,
		})
	}

	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse("PORTFORWARD_NOT_FOUND", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// List — GET /api/v1/portforward/list. Sem RBAC extra (leitura) — sessões são globais/visíveis
// pra qualquer usuário autenticado, mesma transparência de outras ferramentas server-side desta
// app (evita duas pessoas abrirem port-forwards duplicados pro mesmo pod sem saber).
func (h *PortForwardHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "sessions": h.manager.List()})
}

// Get — GET /api/v1/portforward/:id.
func (h *PortForwardHandler) Get(c *gin.Context) {
	info, ok := h.manager.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, errorResponse("PORTFORWARD_NOT_FOUND", "sessão não encontrada"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "session": info})
}
