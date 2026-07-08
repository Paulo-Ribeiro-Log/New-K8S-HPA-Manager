package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"

	"k8s-hpa-manager/internal/config"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

// ExplorerHandler gerencia as rotas do Resource Explorer —
// navegador universal de recursos Kubernetes (built-in e CRDs).
type ExplorerHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewExplorerHandler cria um handler do Resource Explorer
func NewExplorerHandler(km *config.KubeConfigManager) *ExplorerHandler {
	return &ExplorerHandler{kubeManager: km}
}

// ListResources retorna todos os tipos de recursos disponíveis no cluster via Discovery API.
// Inclui recursos built-in (Pods, Deployments) e CRDs instalados (ExternalSecret, etc.).
// GET /api/v1/explorer/api-resources?cluster=X
func (h *ExplorerHandler) ListResources(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "Parameter 'cluster' is required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get cluster client: %v", err)))
		return
	}

	resources, err := kubeclient.ListAPIResources(clientset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DISCOVERY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resources,
		"count":   len(resources),
	})
}

// ListByKind lista recursos de um tipo específico no cluster.
// GET /api/v1/explorer/items?cluster=X&resource=pods&group=&namespace=Y
// resource = nome plural (e.g. "pods", "externalsecrets")
// group    = API group (e.g. "external-secrets.io"; vazio para recursos core)
// namespace = namespace a filtrar; vazio = todos os namespaces
func (h *ExplorerHandler) ListByKind(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	resource := strings.TrimSpace(c.Query("resource"))
	group := strings.TrimSpace(c.Query("group"))
	namespace := strings.TrimSpace(c.Query("namespace"))

	if cluster == "" || resource == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "Parameters 'cluster' and 'resource' are required"))
		return
	}

	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(cluster)
	if authErr != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_ERROR", authErr.Error()))
		return
	}
	defer cleanup()

	items, err := kubeclient.ListGenericResources(cluster, namespace, resource, group, authArgs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("LIST_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    items,
		"count":   len(items),
	})
}

// GetYAML retorna o manifesto YAML de um recurso específico.
// GET /api/v1/explorer/:cluster/:namespace/:resource/:name?group=external-secrets.io
func (h *ExplorerHandler) GetYAML(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")
	group := strings.TrimSpace(c.Query("group"))

	if cluster == "" || resource == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, resource and name are required"))
		return
	}

	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(cluster)
	if authErr != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("GET_ERROR", authErr.Error()))
		return
	}
	defer cleanup()

	manifest, err := kubeclient.GetGenericResourceYAML(cluster, namespace, resource, group, name, authArgs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("GET_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    manifest,
	})
}

// Describe retorna a saída do kubectl describe para qualquer recurso.
// GET /api/v1/explorer/:cluster/:namespace/:resource/:name/describe?group=external-secrets.io
func (h *ExplorerHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	if cluster == "" || resource == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, resource and name are required"))
		return
	}

	output, err := kubeclient.ExecuteKubectlDescribe(h.kubeManager.ConfigPath(), h.kubeManager.ResolveContext(cluster), resource, name, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DESCRIBE_ERROR", fmt.Sprintf("kubectl describe failed: %v", err)))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster":   cluster,
		"namespace": namespace,
		"name":      name,
		"describe":  output,
	})
}

// Diff gera um unified diff entre dois YAMLs.
// POST /api/v1/explorer/diff
func (h *ExplorerHandler) Diff(c *gin.Context) {
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

// Validate executa dry-run de um manifesto (qualquer tipo de recurso).
// POST /api/v1/explorer/validate
func (h *ExplorerHandler) Validate(c *gin.Context) {
	var req struct {
		Cluster   string `json:"cluster"`
		Namespace string `json:"namespace"`
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

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(req.Cluster)
	if authErr != nil {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("VALIDATION_ERROR", authErr.Error()))
		return
	}
	defer cleanup()

	if err := kubeclient.ApplyGenericResource(req.Cluster, ns, req.YAML, true, false, authArgs); err != nil {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("VALIDATION_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"valid": true},
	})
}

// Apply aplica um manifesto YAML para qualquer tipo de recurso.
// PUT /api/v1/explorer/:cluster/:namespace/:resource/:name?group=external-secrets.io
func (h *ExplorerHandler) Apply(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")

	if cluster == "" || resource == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, resource and name are required"))
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

	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(cluster)
	if authErr != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", authErr.Error()))
		return
	}
	defer cleanup()

	if err := kubeclient.ApplyGenericResource(cluster, namespace, req.YAML, req.DryRun, req.Force, authArgs); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"name":      name,
			"namespace": namespace,
			"cluster":   cluster,
			"resource":  resource,
			"dryRun":    req.DryRun,
		},
	})
}

// Delete deleta qualquer recurso Kubernetes.
// DELETE /api/v1/explorer/:cluster/:namespace/:resource/:name?group=external-secrets.io
func (h *ExplorerHandler) Delete(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	resource := c.Param("resource")
	name := c.Param("name")
	group := strings.TrimSpace(c.Query("group"))

	if cluster == "" || resource == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, resource and name are required"))
		return
	}

	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(cluster)
	if authErr != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DELETE_ERROR", authErr.Error()))
		return
	}
	defer cleanup()

	if err := kubeclient.DeleteGenericResource(cluster, namespace, resource, group, name, authArgs); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DELETE_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("%s/%s deleted successfully", namespace, name),
	})
}
