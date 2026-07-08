package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// ServiceHandler gerencia as rotas de Services
type ServiceHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewServiceHandler cria um handler de Services
func NewServiceHandler(km *config.KubeConfigManager) *ServiceHandler {
	return &ServiceHandler{kubeManager: km}
}

// NewServiceHandlerWithHistory cria um handler com history tracker
func NewServiceHandlerWithHistory(km *config.KubeConfigManager, ht *history.HistoryTracker) *ServiceHandler {
	return &ServiceHandler{kubeManager: km, historyTracker: ht}
}

// ServiceSummary descreve informações resumidas de um Service
type ServiceSummary struct {
	Cluster    string            `json:"cluster"`
	Namespace  string            `json:"namespace"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	ClusterIP  string            `json:"clusterIP"`
	ExternalIP string            `json:"externalIP,omitempty"`
	Ports      []string          `json:"ports"`
	Selector   map[string]string `json:"selector,omitempty"`
	Age        string            `json:"age"`
}

// List retorna Services filtrados por cluster e namespaces
// GET /api/v1/services?cluster=X&namespaces=Y
func (h *ServiceHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespaces := parseNamespaces(c.Query("namespaces"))
	showSystem := c.Query("showSystem") == "true"

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

	var services []ServiceSummary

	listNS := namespaces
	if len(listNS) == 0 {
		// Listar de todos os namespaces via API
		nsList, err := clientset.CoreV1().Namespaces().List(c.Request.Context(), metav1.ListOptions{})
		if err == nil {
			for _, ns := range nsList.Items {
				listNS = append(listNS, ns.Name)
			}
		}
	}

	for _, ns := range listNS {
		if !showSystem && isSystemNamespaceFn(ns) {
			continue
		}
		svcList, err := clientset.CoreV1().Services(ns).List(c.Request.Context(), metav1.ListOptions{})
		if err != nil {
			continue
		}
		for _, svc := range svcList.Items {
			services = append(services, buildServiceSummary(cluster, ns, svc))
		}
	}

	if services == nil {
		services = []ServiceSummary{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"services": services,
			"count":    len(services),
		},
	})
}

// Get retorna o manifesto YAML completo de um Service
// GET /api/v1/services/:cluster/:namespace/:name
func (h *ServiceHandler) Get(c *gin.Context) {
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

	kc := kubeclient.NewClient(clientset, cluster)
	manifest, err := kc.GetServiceYAML(c.Request.Context(), namespace, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "GET_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    manifest,
	})
}

// Diff gera diff texto entre dois YAMLs de Service
// POST /api/v1/services/diff
func (h *ServiceHandler) Diff(c *gin.Context) {
	var req struct {
		Original string `json:"originalYaml"`
		Updated  string `json:"updatedYaml"`
		FileName string `json:"fileName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("Invalid body: %v", err)))
		return
	}
	if strings.TrimSpace(req.Updated) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", "updatedYaml is required"))
		return
	}

	fromFile := "original"
	toFile := "edited"
	if strings.TrimSpace(req.FileName) != "" {
		fromFile = fmt.Sprintf("%s (original)", req.FileName)
		toFile = fmt.Sprintf("%s (edit)", req.FileName)
	}

	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(req.Original),
		B:        difflib.SplitLines(req.Updated),
		FromFile: fromFile,
		ToFile:   toFile,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DIFF_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"unifiedDiff": text,
			"hasChanges":  strings.TrimSpace(text) != "",
		},
	})
}

// Validate executa dry-run de um manifesto Service
// POST /api/v1/services/validate
func (h *ServiceHandler) Validate(c *gin.Context) {
	var req struct {
		Cluster   string `json:"cluster"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		YAML      string `json:"yaml"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("Invalid body: %v", err)))
		return
	}
	if strings.TrimSpace(req.Cluster) == "" || strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster and yaml are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}

	kc := kubeclient.NewClient(clientset, req.Cluster)
	result, err := kc.ApplyService(c.Request.Context(), req.YAML, req.Namespace, req.Name, true, false)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("VALIDATION_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"name":            result.Name,
			"namespace":       result.Namespace,
			"resourceVersion": result.ResourceVersion,
		},
	})
}

// Apply aplica um manifesto Service no cluster
// PUT /api/v1/services/:cluster/:namespace/:name
func (h *ServiceHandler) Apply(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req struct {
		YAML   string `json:"yaml"`
		DryRun bool   `json:"dryRun"`
		Force  bool   `json:"force"`
	}
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

	start := time.Now()
	kc := kubeclient.NewClient(clientset, cluster)
	result, err := kc.ApplyService(c.Request.Context(), req.YAML, namespace, name, req.DryRun, req.Force)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "apply_service",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		_ = h.historyTracker.Log(entry)
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

// Create cria um novo Service a partir de YAML
// POST /api/v1/services/:cluster/:namespace
func (h *ServiceHandler) Create(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))

	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster and namespace are required"))
		return
	}

	var req struct {
		YAML string `json:"yaml"`
	}
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

	kc := kubeclient.NewClient(clientset, cluster)
	result, err := kc.ApplyService(c.Request.Context(), req.YAML, namespace, "", false, false)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse("CREATE_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": gin.H{
			"name":      result.Name,
			"namespace": result.Namespace,
			"cluster":   cluster,
		},
	})
}

// Delete deleta um Service específico
// DELETE /api/v1/services/:cluster/:namespace/:name
func (h *ServiceHandler) Delete(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

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

	kc := kubeclient.NewClient(clientset, cluster)
	if err := kc.DeleteService(c.Request.Context(), namespace, name); err != nil {
		if checkForbidden(c, err) {
			return
		}
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
		"message": fmt.Sprintf("Service %s/%s deleted successfully", namespace, name),
	})
}

// Describe retorna a saída do kubectl describe para um Service
// GET /api/v1/services/:cluster/:namespace/:name/describe
func (h *ServiceHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, namespace e name são obrigatórios"})
		return
	}

	output, err := kubeclient.ExecuteKubectlDescribe(h.kubeManager.ConfigPath(), h.kubeManager.ResolveContext(cluster), "service", name, namespace)
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

func buildServiceSummary(cluster, namespace string, svc corev1.Service) ServiceSummary {
	var ports []string
	for _, port := range svc.Spec.Ports {
		portStr := fmt.Sprintf("%d", port.Port)
		if port.TargetPort.IntVal > 0 {
			portStr += fmt.Sprintf(":%d", port.TargetPort.IntVal)
		} else if port.TargetPort.StrVal != "" {
			portStr += fmt.Sprintf(":%s", port.TargetPort.StrVal)
		}
		if port.NodePort > 0 {
			portStr += fmt.Sprintf(":%d", port.NodePort)
		}
		portStr += fmt.Sprintf("/%s", port.Protocol)
		ports = append(ports, portStr)
	}

	externalIP := ""
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		for _, ing := range svc.Status.LoadBalancer.Ingress {
			if ing.IP != "" {
				externalIP = ing.IP
				break
			} else if ing.Hostname != "" {
				externalIP = ing.Hostname
				break
			}
		}
	} else if len(svc.Spec.ExternalIPs) > 0 {
		externalIP = svc.Spec.ExternalIPs[0]
	}

	return ServiceSummary{
		Cluster:    cluster,
		Namespace:  namespace,
		Name:       svc.Name,
		Type:       string(svc.Spec.Type),
		ClusterIP:  svc.Spec.ClusterIP,
		ExternalIP: externalIP,
		Ports:      ports,
		Selector:   svc.Spec.Selector,
		Age:        formatServiceAge(svc.CreationTimestamp.Time),
	}
}

func formatServiceAge(t time.Time) string {
	d := time.Since(t)
	if d.Hours() >= 24 {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d.Hours() >= 1 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
