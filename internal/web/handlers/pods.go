package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/config"
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
	kubeManager *config.KubeConfigManager
}

// NewPodHandler cria um handler de pods
func NewPodHandler(km *config.KubeConfigManager) *PodHandler {
	return &PodHandler{
		kubeManager: km,
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

	err = clientset.CoreV1().Pods(namespace).Delete(c.Request.Context(), name, metav1.DeleteOptions{})
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success": true,
			"message": fmt.Sprintf("Pod %s deleted successfully", name),
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

	// Deletar o pod com GracePeriodSeconds=0 para restart rápido
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

	// Calcular recursos totais
	var cpuRequest, memoryRequest, cpuLimit, memoryLimit string
	totalCPUReq := pod.Spec.Containers[0].Resources.Requests.Cpu()
	totalMemReq := pod.Spec.Containers[0].Resources.Requests.Memory()
	totalCPULim := pod.Spec.Containers[0].Resources.Limits.Cpu()
	totalMemLim := pod.Spec.Containers[0].Resources.Limits.Memory()

	if totalCPUReq != nil && !totalCPUReq.IsZero() {
		cpuRequest = totalCPUReq.String()
	}
	if totalMemReq != nil && !totalMemReq.IsZero() {
		memoryRequest = totalMemReq.String()
	}
	if totalCPULim != nil && !totalCPULim.IsZero() {
		cpuLimit = totalCPULim.String()
	}
	if totalMemLim != nil && !totalMemLim.IsZero() {
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
