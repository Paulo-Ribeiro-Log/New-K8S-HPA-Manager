package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// isSystemNamespace verifica se um namespace é de sistema
func isSystemNamespace(namespace string) bool {
	systemNamespaces := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"gatekeeper-system",
		"calico-system",
	}
	for _, ns := range systemNamespaces {
		if namespace == ns {
			return true
		}
	}
	return strings.HasPrefix(namespace, "kube-") || strings.HasPrefix(namespace, "calico-")
}

// PodHandler gerencia as rotas de Pods
type PodHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewPodHandler cria um handler de pods
func NewPodHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *PodHandler {
	return &PodHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// ContainerStatus representa o status de um container
type ContainerStatus struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state"`
	StateReason  string `json:"stateReason,omitempty"`
	Started      *bool  `json:"started,omitempty"`
}

// PodSummary representa um resumo de Pod
type PodSummary struct {
	Cluster         string            `json:"cluster"`
	Namespace       string            `json:"namespace"`
	Name            string            `json:"name"`
	PodIP           string            `json:"podIP,omitempty"`
	NodeName        string            `json:"nodeName,omitempty"`
	Phase           string            `json:"phase"`
	Labels          map[string]string `json:"labels,omitempty"`
	Containers      []ContainerStatus `json:"containers"`
	ReadyContainers int               `json:"readyContainers"`
	TotalContainers int               `json:"totalContainers"`
	CPURequest      string            `json:"cpuRequest,omitempty"`
	MemoryRequest   string            `json:"memoryRequest,omitempty"`
	CPULimit        string            `json:"cpuLimit,omitempty"`
	MemoryLimit     string            `json:"memoryLimit,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	CreatedAt       string            `json:"createdAt"`
	Restarts        int32             `json:"restarts"`
}

// PodManifest representa o manifest completo de um Pod
type PodManifest struct {
	Cluster   string                 `json:"cluster"`
	Namespace string                 `json:"namespace"`
	Name      string                 `json:"name"`
	Yaml      string                 `json:"yaml"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// List retorna pods com filtros
func (h *PodHandler) List(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	namespaces := parseNamespaces(c.Query("namespaces"))
	showSystem := c.Query("showSystem") == "true"
	search := strings.ToLower(c.Query("search"))

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	var pods []PodSummary
	var allPods *corev1.PodList

	if len(namespaces) == 0 {
		// Lista todos os namespaces
		allPods, err = clientset.CoreV1().Pods("").List(c.Request.Context(), metav1.ListOptions{})
	} else {
		// Lista múltiplos namespaces específicos
		allPods = &corev1.PodList{}
		for _, ns := range namespaces {
			podList, err := clientset.CoreV1().Pods(ns).List(c.Request.Context(), metav1.ListOptions{})
			if err != nil {
				continue
			}
			allPods.Items = append(allPods.Items, podList.Items...)
		}
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	for _, pod := range allPods.Items {
		// Filtrar namespaces do sistema se necessário
		if !showSystem && isSystemNamespace(pod.Namespace) {
			continue
		}

		// Filtrar por busca
		if search != "" {
			podName := strings.ToLower(pod.Name)
			namespace := strings.ToLower(pod.Namespace)
			nodeName := strings.ToLower(pod.Spec.NodeName)

			matchFound := strings.Contains(podName, search) ||
				strings.Contains(namespace, search) ||
				strings.Contains(nodeName, search)

			// Buscar também em nomes de containers
			if !matchFound {
				for _, container := range pod.Spec.Containers {
					if strings.Contains(strings.ToLower(container.Name), search) {
						matchFound = true
						break
					}
				}
			}

			if !matchFound {
				continue
			}
		}

		summary := h.convertToPodSummary(cluster, &pod)
		pods = append(pods, summary)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    pods,
		"count":   len(pods),
	})
}

// Get retorna o manifest completo de um pod
func (h *PodHandler) Get(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster, namespace and name must be provided",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	pod, err := clientset.CoreV1().Pods(namespace).Get(c.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("Pod not found: %v", err),
			},
		})
		return
	}

	yamlBytes, err := yaml.Marshal(pod)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MARSHAL_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	manifest := PodManifest{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      name,
		Yaml:      string(yamlBytes),
		Metadata: map[string]interface{}{
			"uid":             pod.UID,
			"resourceVersion": pod.ResourceVersion,
			"labels":          pod.Labels,
			"annotations":     pod.Annotations,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    manifest,
	})
}

// Delete deleta um pod
func (h *PodHandler) Delete(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster, namespace and name must be provided",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	ctx := c.Request.Context()
	var before map[string]interface{}
	if pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		before = map[string]interface{}{
			"name":      pod.Name,
			"namespace": pod.Namespace,
			"phase":     string(pod.Status.Phase),
			"nodeName":  pod.Spec.NodeName,
		}
	}

	start := time.Now()
	err = clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "delete_pod",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Before:   before,
			After:    nil,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success": true,
			"message": fmt.Sprintf("Pod %s deleted successfully", name),
		},
	})
}

// Kill força a terminação imediata de um pod (gracePeriod=0)
func (h *PodHandler) Kill(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster, namespace and name must be provided",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	ctx := c.Request.Context()

	// Capturar estado antes do kill
	var before map[string]interface{}
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("Pod not found: %v", err),
			},
		})
		return
	}

	before = map[string]interface{}{
		"name":      pod.Name,
		"namespace": pod.Namespace,
		"phase":     string(pod.Status.Phase),
		"nodeName":  pod.Spec.NodeName,
	}

	// Kill forçado com GracePeriodSeconds=0
	start := time.Now()
	gracePeriod := int64(0)
	err = clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KILL_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "kill_pod",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Before:   before,
			After:    map[string]interface{}{"killed": true, "gracePeriod": 0},
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success": true,
			"message": fmt.Sprintf("Pod %s killed immediately (forced termination)", name),
		},
	})
}

// Restart reinicia um pod (delete + deixa controller recriar)
func (h *PodHandler) Restart(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster, namespace and name must be provided",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	// Buscar o pod para verificar se é gerenciado por um controller
	pod, err := clientset.CoreV1().Pods(namespace).Get(c.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": fmt.Sprintf("Pod not found: %v", err),
			},
		})
		return
	}

	// Verificar se o pod tem owner (Deployment, StatefulSet, DaemonSet, etc)
	hasOwner := len(pod.OwnerReferences) > 0
	var ownerKind string
	if hasOwner {
		ownerKind = pod.OwnerReferences[0].Kind
	}

	// Capturar estado antes do restart
	before := map[string]interface{}{
		"name":      pod.Name,
		"namespace": pod.Namespace,
		"phase":     string(pod.Status.Phase),
		"nodeName":  pod.Spec.NodeName,
		"hasOwner":  hasOwner,
	}
	if hasOwner {
		before["ownerKind"] = ownerKind
	}

	// Deletar o pod com GracePeriodSeconds=0 para restart rápido
	start := time.Now()
	gracePeriod := int64(0)
	err = clientset.CoreV1().Pods(namespace).Delete(c.Request.Context(), name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "RESTART_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "restart_pod",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Before:   before,
			After:    map[string]interface{}{"restarted": true, "hasOwner": hasOwner, "ownerKind": ownerKind},
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	message := fmt.Sprintf("Pod %s restarted successfully", name)
	if hasOwner {
		message = fmt.Sprintf("Pod %s restarted successfully (managed by %s)", name, ownerKind)
	} else {
		message = fmt.Sprintf("Pod %s deleted successfully (WARNING: pod has no owner and will NOT be recreated)", name)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success":   true,
			"message":   message,
			"hasOwner":  hasOwner,
			"ownerKind": ownerKind,
		},
	})
}

// GetLogs retorna os logs de um container
func (h *PodHandler) GetLogs(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	podName := strings.TrimSpace(c.Param("name"))
	containerName := c.Query("container")
	tailStr := c.Query("tail")

	if cluster == "" || namespace == "" || podName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster, namespace and pod name must be provided",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	opts := &corev1.PodLogOptions{}

	if containerName != "" {
		opts.Container = containerName
	}

	if tailStr != "" {
		tailLines, err := strconv.ParseInt(tailStr, 10, 64)
		if err == nil && tailLines > 0 {
			opts.TailLines = &tailLines
		}
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	logs, err := req.DoRaw(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LOGS_ERROR",
				"message": fmt.Sprintf("Failed to get logs: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"logs": string(logs),
		},
	})
}

// convertToPodSummary converte um Pod do Kubernetes para PodSummary
func (h *PodHandler) convertToPodSummary(cluster string, pod *corev1.Pod) PodSummary {
	containers := []ContainerStatus{}
	readyCount := 0
	totalRestarts := int32(0)

	// Processar containers
	for _, cs := range pod.Status.ContainerStatuses {
		state := "unknown"
		stateReason := ""

		if cs.State.Running != nil {
			state = "running"
		} else if cs.State.Waiting != nil {
			state = "waiting"
			stateReason = cs.State.Waiting.Reason
		} else if cs.State.Terminated != nil {
			state = "terminated"
			stateReason = cs.State.Terminated.Reason
		}

		if cs.Ready {
			readyCount++
		}

		totalRestarts += cs.RestartCount

		containers = append(containers, ContainerStatus{
			Name:         cs.Name,
			Image:        cs.Image,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
			State:        state,
			StateReason:  stateReason,
			Started:      cs.Started,
		})
	}

	// Calcular recursos totais (soma de todos os containers)
	var cpuRequest, memoryRequest, cpuLimit, memoryLimit string
	var totalCPUReq, totalMemReq, totalCPULim, totalMemLim resource.Quantity

	for _, container := range pod.Spec.Containers {
		if req := container.Resources.Requests.Cpu(); req != nil && !req.IsZero() {
			totalCPUReq.Add(*req)
		}
		if req := container.Resources.Requests.Memory(); req != nil && !req.IsZero() {
			totalMemReq.Add(*req)
		}
		if lim := container.Resources.Limits.Cpu(); lim != nil && !lim.IsZero() {
			totalCPULim.Add(*lim)
		}
		if lim := container.Resources.Limits.Memory(); lim != nil && !lim.IsZero() {
			totalMemLim.Add(*lim)
		}
	}

	if !totalCPUReq.IsZero() {
		cpuRequest = totalCPUReq.String()
	}
	if !totalMemReq.IsZero() {
		memoryRequest = totalMemReq.String()
	}
	if !totalCPULim.IsZero() {
		cpuLimit = totalCPULim.String()
	}
	if !totalMemLim.IsZero() {
		memoryLimit = totalMemLim.String()
	}

	createdAt := pod.CreationTimestamp.Format(time.RFC3339)

	return PodSummary{
		Cluster:         cluster,
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		PodIP:           pod.Status.PodIP,
		NodeName:        pod.Spec.NodeName,
		Phase:           string(pod.Status.Phase),
		Labels:          pod.Labels,
		Containers:      containers,
		ReadyContainers: readyCount,
		TotalContainers: len(pod.Spec.Containers),
		CPURequest:      cpuRequest,
		MemoryRequest:   memoryRequest,
		CPULimit:        cpuLimit,
		MemoryLimit:     memoryLimit,
		ResourceVersion: pod.ResourceVersion,
		CreatedAt:       createdAt,
		Restarts:        totalRestarts,
	}
}

// Describe retorna a saída do kubectl describe para um Pod
func (h *PodHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, namespace e name são obrigatórios"})
		return
	}

	// Executar kubectl describe
	output, err := kubeclient.ExecuteKubectlDescribe(cluster, "pod", name, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Erro ao executar kubectl describe: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster":   cluster,
		"namespace": namespace,
		"name":      name,
		"describe":  output,
	})
}

// PodsSummary representa um resumo agregado de pods de um namespace
type PodsSummary struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
}

// GetSummary retorna um resumo agregado de pods de um namespace
func (h *PodHandler) GetSummary(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")

	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "cluster and namespace are required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(c.Request.Context(), metav1.ListOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": fmt.Sprintf("Failed to list pods: %v", err),
			},
		})
		return
	}

	summary := PodsSummary{Total: len(pods.Items)}

	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			summary.Running++
		case corev1.PodPending:
			summary.Pending++
		case corev1.PodFailed:
			summary.Failed++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}

// CreateDebugPod cria um pod de debug standalone no namespace
func (h *PodHandler) CreateDebugPod(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")

	var req struct {
		Name string `json:"name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	if cluster == "" || namespace == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "cluster, namespace and name are required",
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	// Define debug pod spec
	debugPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":  "debug",
				"type": "standalone-debug",
			},
			Annotations: map[string]string{
				"description": "Standalone debug pod created via HPA Manager",
				"created-at":  time.Now().Format(time.RFC3339),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "netshoot",
					Image:           "nicolaka/netshoot:latest",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/bash"},
					Args:            []string{"-c", "sleep infinity"},
					Stdin:           true,
					StdinOnce:       false,
					TTY:             true,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Create the pod
	_, err = clientset.CoreV1().Pods(namespace).Create(c.Request.Context(), debugPod, metav1.CreateOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CREATE_ERROR",
				"message": fmt.Sprintf("Failed to create debug pod: %v", err),
			},
		})
		return
	}

	// Register in history
	entry := history.HistoryEntry{
		Action:   "create_debug_pod",
		Resource: fmt.Sprintf("%s/%s", namespace, req.Name),
		Cluster:  cluster,
		Before:   nil,
		After: map[string]interface{}{
			"pod":  req.Name,
			"type": "standalone-debug",
		},
		Status:   "success",
		Duration: 0,
	}
	if err := h.historyTracker.Log(entry); err != nil {
		fmt.Printf("warning: failed to record history entry: %v\n", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success": true,
			"message": fmt.Sprintf("Debug pod %s created successfully", req.Name),
		},
	})
}

// podApplyRequest representa uma requisição de apply de Pod
type podApplyRequest struct {
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
	DryRun       bool   `json:"dryRun"`
}

// Apply aplica mudanças em um Pod existente
func (h *PodHandler) Apply(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req podApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("Invalid body: %v", err)))
		return
	}
	if strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", "yaml is required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}
	ctx := c.Request.Context()
	kubeClient := kubeclient.NewClient(clientset, cluster)

	var before map[string]interface{}
	if !req.DryRun {
		// Capturar estado anterior para history tracking
		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			yamlBytes, _ := yaml.Marshal(pod)
			before = map[string]interface{}{
				"name":      pod.Name,
				"namespace": pod.Namespace,
				"phase":     string(pod.Status.Phase),
				"yaml":      string(yamlBytes),
			}
		}
	}

	start := time.Now()
	result, err := kubeClient.ApplyPod(ctx, req.YAML, req.FieldManager, namespace, name, req.DryRun)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "APPLY_ERROR"
		c.JSON(status, errorResponse(errorCode, err.Error()))
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		after := map[string]interface{}{
			"name":      result.Name,
			"namespace": result.Namespace,
			"phase":     string(result.Status.Phase),
		}
		entry := history.HistoryEntry{
			Action:   "apply_pod",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Before:   before,
			After:    after,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"name":            result.Name,
			"namespace":       result.Namespace,
			"cluster":         cluster,
			"resourceVersion": result.ResourceVersion,
			"dryRun":          req.DryRun,
			"appliedAt":       time.Now().UTC(),
		},
	})
}

// GetMetrics retorna métricas atuais de CPU e memória de um Pod
func (h *PodHandler) GetMetrics(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}

	ctx := c.Request.Context()
	kubeClient := kubeclient.NewClient(clientset, cluster)

	// Buscar métricas usando metrics-server
	metrics, err := kubeClient.GetPodMetricsFromServer(ctx, namespace, name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"available": false,
				"message":   "Metrics not available",
			},
		})
		return
	}

	// Buscar Pod para obter requests/limits
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("GET_POD_ERROR", err.Error()))
		return
	}

	// Calcular totais de CPU e memória
	var totalCPU, totalMemory int64
	for _, container := range metrics.Containers {
		totalCPU += container.Usage.Cpu().MilliValue()
		totalMemory += container.Usage.Memory().Value()
	}

	// Calcular requests e limits
	var cpuRequest, cpuLimit, memRequest, memLimit int64
	for _, container := range pod.Spec.Containers {
		if req := container.Resources.Requests[corev1.ResourceCPU]; !req.IsZero() {
			cpuRequest += req.MilliValue()
		}
		if lim := container.Resources.Limits[corev1.ResourceCPU]; !lim.IsZero() {
			cpuLimit += lim.MilliValue()
		}
		if req := container.Resources.Requests[corev1.ResourceMemory]; !req.IsZero() {
			memRequest += req.Value()
		}
		if lim := container.Resources.Limits[corev1.ResourceMemory]; !lim.IsZero() {
			memLimit += lim.Value()
		}
	}

	// Calcular percentuais
	cpuPercent := 0.0
	if cpuLimit > 0 {
		cpuPercent = float64(totalCPU) / float64(cpuLimit) * 100
	} else if cpuRequest > 0 {
		cpuPercent = float64(totalCPU) / float64(cpuRequest) * 100
	}

	memPercent := 0.0
	if memLimit > 0 {
		memPercent = float64(totalMemory) / float64(memLimit) * 100
	} else if memRequest > 0 {
		memPercent = float64(totalMemory) / float64(memRequest) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": true,
			"cpu": gin.H{
				"current": totalCPU,
				"request": cpuRequest,
				"limit":   cpuLimit,
				"percent": cpuPercent,
				"unit":    "millicores",
			},
			"memory": gin.H{
				"current": totalMemory,
				"request": memRequest,
				"limit":   memLimit,
				"percent": memPercent,
				"unit":    "bytes",
			},
		},
	})
}

// DownloadFromPod faz download de arquivo/diretório do pod
// GET /api/v1/pods/:cluster/:namespace/:name/download?container=nginx&path=/path/to/file&type=file
func (h *PodHandler) DownloadFromPod(c *gin.Context) {
	startTime := time.Now()
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")
	remotePath := c.Query("path")
	transferType := c.DefaultQuery("type", "file") // file ou directory

	// Obter informações do usuário (se disponível via RBAC/SSO)
	userEmail := c.GetString("user_email")
	userName := c.GetString("user_name")
	if userEmail == "" {
		userEmail = "anonymous"
	}

	// Helper para log de auditoria
	logAudit := func(status string, errorMsg string, fileInfo map[string]interface{}) {
		entry := history.HistoryEntry{
			Timestamp: startTime,
			UserEmail: userEmail,
			UserName:  userName,
			Action:    "download_file_from_pod",
			Resource:  fmt.Sprintf("%s/%s", namespace, podName),
			Cluster:   cluster,
			Before: map[string]interface{}{
				"remote_path":   remotePath,
				"container":     container,
				"transfer_type": transferType,
			},
			After:    fileInfo,
			Status:   status,
			ErrorMsg: errorMsg,
			Duration: time.Since(startTime).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("Warning: failed to log download audit: %v\n", err)
		}
	}

	if remotePath == "" {
		logAudit("failed", "Parâmetro 'path' é obrigatório", nil)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parâmetro 'path' é obrigatório",
		})
		return
	}

	// Obter client Kubernetes
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		logAudit("failed", fmt.Sprintf("Erro ao obter client: %v", err), nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao obter client: %v", err),
		})
		return
	}

	// Criar wrapper com métodos customizados
	kubeClient := kubeclient.NewClient(clientset, cluster)

	// Executar kubectl cp via client wrapper (validação de pod existe é feita pelo kubectl)
	tempFile, err := kubeClient.CopyFromPod(namespace, podName, container, remotePath, transferType == "directory")
	if err != nil {
		logAudit("failed", fmt.Sprintf("kubectl cp failed: %v", err), nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao copiar arquivo: %v", err),
		})
		return
	}

	// Garantir cleanup do arquivo temporário após download
	defer kubeClient.CleanupTempFile(tempFile)

	// Obter informações do arquivo transferido
	fileInfo, err := os.Stat(tempFile)
	var fileSize int64 = 0
	if err == nil {
		fileSize = fileInfo.Size()
	}

	// Determinar nome do arquivo para download
	filename := remotePath
	if strings.Contains(filename, "/") {
		parts := strings.Split(filename, "/")
		filename = parts[len(parts)-1]
	}
	if transferType == "directory" {
		filename = filename + ".tar.gz"
	}

	// Log de auditoria com sucesso
	logAudit("success", "", map[string]interface{}{
		"filename":      filename,
		"file_size":     fileSize,
		"transfer_type": transferType,
		"download_time": time.Since(startTime).String(),
	})

	// Abrir arquivo para leitura binária RAW
	file, err := os.Open(tempFile)
	if err != nil {
		logAudit("failed", fmt.Sprintf("Failed to open temp file: %v", err), nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao abrir arquivo: %v", err),
		})
		return
	}
	defer file.Close()

	// Headers HTTP para download binário RAW (sem encoding)
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Length", fmt.Sprintf("%d", fileSize))

	// Copiar arquivo RAW diretamente para response (sem processamento)
	c.Status(http.StatusOK)
	io.Copy(c.Writer, file)
}

// BrowseFiles lista arquivos e diretórios de um pod
func (h *PodHandler) BrowseFiles(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")
	remotePath := c.DefaultQuery("path", "/")

	if remotePath == "" {
		remotePath = "/"
	}

	// Obter client Kubernetes
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao obter client: %v", err),
		})
		return
	}

	// Criar wrapper com métodos customizados
	kubeClient := kubeclient.NewClient(clientset, cluster)

	// Listar arquivos do diretório
	files, err := kubeClient.ListDirectory(namespace, podName, container, remotePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao listar diretório: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"path":  remotePath,
			"files": files,
		},
	})
}

// batchDownloadRequest representa uma requisição de download em batch
type batchDownloadRequest struct {
	Paths []string `json:"paths" binding:"required"`
}

// DownloadMultipleFromPod faz download de múltiplos arquivos empacotados em tar.gz
func (h *PodHandler) DownloadMultipleFromPod(c *gin.Context) {
	startTime := time.Now()
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	podName := c.Param("name")
	container := c.Query("container")

	// Parse request body
	var req batchDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	if len(req.Paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Nenhum arquivo especificado",
		})
		return
	}

	// Obter informações do usuário (se disponível via RBAC/SSO)
	userEmail := c.GetString("user_email")
	userName := c.GetString("user_name")
	if userEmail == "" {
		userEmail = "anonymous"
	}

	// Helper para log de auditoria
	logAudit := func(status string, errorMsg string, fileInfo map[string]interface{}) {
		entry := history.HistoryEntry{
			Timestamp: startTime,
			UserEmail: userEmail,
			UserName:  userName,
			Action:    "batch_download_from_pod",
			Resource:  fmt.Sprintf("%s/%s", namespace, podName),
			Cluster:   cluster,
			Before: map[string]interface{}{
				"paths":     req.Paths,
				"container": container,
				"count":     len(req.Paths),
			},
			After:    fileInfo,
			Status:   status,
			ErrorMsg: errorMsg,
			Duration: time.Since(startTime).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("Warning: failed to log batch download audit: %v\n", err)
		}
	}

	// Obter client Kubernetes
	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		logAudit("failed", fmt.Sprintf("Erro ao obter client: %v", err), nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao obter client: %v", err),
		})
		return
	}

	// Criar wrapper com métodos customizados
	kubeClient := kubeclient.NewClient(clientset, cluster)

	// Executar batch download
	tempFile, err := kubeClient.CopyMultipleFromPod(namespace, podName, container, req.Paths)
	if err != nil {
		logAudit("failed", fmt.Sprintf("Batch download failed: %v", err), nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao copiar arquivos: %v", err),
		})
		return
	}

	// Garantir cleanup do arquivo temporário após download
	defer kubeClient.CleanupTempFile(tempFile)

	// Obter informações do arquivo transferido
	fileInfo, err := os.Stat(tempFile)
	var fileSize int64 = 0
	if err == nil {
		fileSize = fileInfo.Size()
	}

	// Nome do arquivo de download
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("batch-download-%s.tar.gz", timestamp)

	// Log de auditoria com sucesso
	logAudit("success", "", map[string]interface{}{
		"filename":      filename,
		"file_size":     fileSize,
		"file_count":    len(req.Paths),
		"download_time": time.Since(startTime).String(),
	})

	// Abrir arquivo para leitura binária RAW
	file, err := os.Open(tempFile)
	if err != nil {
		logAudit("failed", fmt.Sprintf("Failed to open temp file: %v", err), nil)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao abrir arquivo: %v", err),
		})
		return
	}
	defer file.Close()

	// Headers HTTP para download binário RAW (sem encoding)
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Length", fmt.Sprintf("%d", fileSize))

	// Copiar arquivo RAW diretamente para response (sem processamento)
	c.Status(http.StatusOK)
	io.Copy(c.Writer, file)
}

// PodReference representa uma referência a um pod para operações em batch
type PodReference struct {
	Namespace string `json:"namespace" binding:"required"`
	Name      string `json:"name" binding:"required"`
}

// BatchOperationRequest representa uma requisição de operação em batch
type BatchOperationRequest struct {
	Pods []PodReference `json:"pods" binding:"required,min=1"`
}

// BatchOperationResult representa o resultado de uma operação em um pod
type BatchOperationResult struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Error     string `json:"error,omitempty"`
}

// BatchDelete deleta múltiplos pods
func (h *PodHandler) BatchDelete(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster must be provided",
			},
		})
		return
	}

	var req BatchOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	ctx := c.Request.Context()
	results := make([]BatchOperationResult, 0, len(req.Pods))
	successCount := 0
	start := time.Now()

	for _, podRef := range req.Pods {
		result := BatchOperationResult{
			Namespace: podRef.Namespace,
			Name:      podRef.Name,
		}

		err := clientset.CoreV1().Pods(podRef.Namespace).Delete(ctx, podRef.Name, metav1.DeleteOptions{})
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			result.Message = fmt.Sprintf("Falha ao deletar pod %s/%s", podRef.Namespace, podRef.Name)
		} else {
			result.Success = true
			result.Message = fmt.Sprintf("Pod %s/%s deletado com sucesso", podRef.Namespace, podRef.Name)
			successCount++
		}

		results = append(results, result)
	}

	// Registrar no histórico
	if h.historyTracker != nil {
		podNames := make([]string, len(req.Pods))
		for i, p := range req.Pods {
			podNames[i] = fmt.Sprintf("%s/%s", p.Namespace, p.Name)
		}
		entry := history.HistoryEntry{
			Action:   "batch_delete_pods",
			Resource: fmt.Sprintf("%d pods", len(req.Pods)),
			Cluster:  cluster,
			Before: map[string]interface{}{
				"pods":  podNames,
				"count": len(req.Pods),
			},
			After: map[string]interface{}{
				"success_count": successCount,
				"failed_count":  len(req.Pods) - successCount,
			},
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"results":       results,
			"total":         len(req.Pods),
			"success_count": successCount,
			"failed_count":  len(req.Pods) - successCount,
		},
	})
}

// BatchKill força a terminação imediata de múltiplos pods
func (h *PodHandler) BatchKill(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster must be provided",
			},
		})
		return
	}

	var req BatchOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	ctx := c.Request.Context()
	results := make([]BatchOperationResult, 0, len(req.Pods))
	successCount := 0
	start := time.Now()
	gracePeriod := int64(0)

	for _, podRef := range req.Pods {
		result := BatchOperationResult{
			Namespace: podRef.Namespace,
			Name:      podRef.Name,
		}

		err := clientset.CoreV1().Pods(podRef.Namespace).Delete(ctx, podRef.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		})
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			result.Message = fmt.Sprintf("Falha ao matar pod %s/%s", podRef.Namespace, podRef.Name)
		} else {
			result.Success = true
			result.Message = fmt.Sprintf("Pod %s/%s terminado forçadamente", podRef.Namespace, podRef.Name)
			successCount++
		}

		results = append(results, result)
	}

	// Registrar no histórico
	if h.historyTracker != nil {
		podNames := make([]string, len(req.Pods))
		for i, p := range req.Pods {
			podNames[i] = fmt.Sprintf("%s/%s", p.Namespace, p.Name)
		}
		entry := history.HistoryEntry{
			Action:   "batch_kill_pods",
			Resource: fmt.Sprintf("%d pods", len(req.Pods)),
			Cluster:  cluster,
			Before: map[string]interface{}{
				"pods":        podNames,
				"count":       len(req.Pods),
				"gracePeriod": 0,
			},
			After: map[string]interface{}{
				"success_count": successCount,
				"failed_count":  len(req.Pods) - successCount,
			},
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"results":       results,
			"total":         len(req.Pods),
			"success_count": successCount,
			"failed_count":  len(req.Pods) - successCount,
		},
	})
}

// BatchRestart reinicia múltiplos pods
func (h *PodHandler) BatchRestart(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster must be provided",
			},
		})
		return
	}

	var req BatchOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	ctx := c.Request.Context()
	results := make([]BatchOperationResult, 0, len(req.Pods))
	successCount := 0
	start := time.Now()
	gracePeriod := int64(0)

	for _, podRef := range req.Pods {
		result := BatchOperationResult{
			Namespace: podRef.Namespace,
			Name:      podRef.Name,
		}

		// Verificar se o pod tem owner
		pod, err := clientset.CoreV1().Pods(podRef.Namespace).Get(ctx, podRef.Name, metav1.GetOptions{})
		hasOwner := false
		ownerKind := ""
		if err == nil && len(pod.OwnerReferences) > 0 {
			hasOwner = true
			ownerKind = pod.OwnerReferences[0].Kind
		}

		err = clientset.CoreV1().Pods(podRef.Namespace).Delete(ctx, podRef.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		})
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			result.Message = fmt.Sprintf("Falha ao reiniciar pod %s/%s", podRef.Namespace, podRef.Name)
		} else {
			result.Success = true
			if hasOwner {
				result.Message = fmt.Sprintf("Pod %s/%s reiniciado (gerenciado por %s)", podRef.Namespace, podRef.Name, ownerKind)
			} else {
				result.Message = fmt.Sprintf("Pod %s/%s deletado (ATENÇÃO: sem owner, não será recriado)", podRef.Namespace, podRef.Name)
			}
			successCount++
		}

		results = append(results, result)
	}

	// Registrar no histórico
	if h.historyTracker != nil {
		podNames := make([]string, len(req.Pods))
		for i, p := range req.Pods {
			podNames[i] = fmt.Sprintf("%s/%s", p.Namespace, p.Name)
		}
		entry := history.HistoryEntry{
			Action:   "batch_restart_pods",
			Resource: fmt.Sprintf("%d pods", len(req.Pods)),
			Cluster:  cluster,
			Before: map[string]interface{}{
				"pods":  podNames,
				"count": len(req.Pods),
			},
			After: map[string]interface{}{
				"success_count": successCount,
				"failed_count":  len(req.Pods) - successCount,
			},
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"results":       results,
			"total":         len(req.Pods),
			"success_count": successCount,
			"failed_count":  len(req.Pods) - successCount,
		},
	})
}
