package handlers

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// GatewayHandler gerencia as rotas da Gateway API (gateway.networking.k8s.io)
type GatewayHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewGatewayHandler cria um handler com dependências já existentes
func NewGatewayHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *GatewayHandler {
	return &GatewayHandler{kubeManager: km, historyTracker: ht}
}

const gatewayGroup = "gateway.networking.k8s.io"

// gatewayKindInfo mapeia kind (minúsculo singular) → (resourceName plural, cluster-scoped)
var gatewayKindInfo = map[string]struct {
	Resource    string
	ClusterWide bool
}{
	"gateway":      {"gateways", false},
	"httproute":    {"httproutes", false},
	"grpcroute":    {"grpcroutes", false},
	"tcproute":     {"tcproutes", false},
	"gatewayclass": {"gatewayclasses", true},
}

func kindToResourceName(kind string) (resource string, clusterWide bool) {
	k := strings.ToLower(kind)
	if info, ok := gatewayKindInfo[k]; ok {
		return info.Resource, info.ClusterWide
	}
	return k + "s", false
}

// List retorna resources da Gateway API com filtros básicos.
// GET /api/v1/gateways?cluster=&kind=gateway&namespace=
func (h *GatewayHandler) List(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "Parameter 'cluster' is required"))
		return
	}
	kind := strings.ToLower(strings.TrimSpace(c.Query("kind")))
	if kind == "" {
		kind = "gateway"
	}
	resourceName, clusterWide := kindToResourceName(kind)
	namespace := strings.TrimSpace(c.Query("namespace"))
	if clusterWide {
		namespace = ""
	}

	items, err := kubeclient.ListGenericResources(cluster, namespace, resourceName, gatewayGroup)
	if err != nil {
		// Gateway API é opcional — qualquer erro de kubectl (CRD não instalado, sem suporte, etc.)
		// resulta em lista vazia ao invés de HTTP 500
		c.JSON(http.StatusOK, gin.H{
			"success":        true,
			"data":           []models.GatewaySummary{},
			"count":          0,
			"not_installed":  true,
			"install_reason": err.Error(),
		})
		return
	}

	summaries := make([]models.GatewaySummary, 0, len(items))
	for _, item := range items {
		s := models.GatewaySummary{
			Cluster:   cluster,
			Namespace: item.Namespace,
			Name:      item.Name,
			Kind:      item.Kind,
			Labels:    item.Labels,
			UpdatedAt: time.Now(),
		}
		if addr, ok := item.AdditionalColumns["address"]; ok && addr != "" {
			s.Addresses = []string{addr}
		}
		summaries = append(summaries, s)
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": summaries, "count": len(summaries)})
}

// Get retorna o manifesto YAML de um recurso Gateway API para edição.
// GET /api/v1/gateways/:cluster/:namespace/:kind/:name
func (h *GatewayHandler) Get(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	kind := strings.ToLower(c.Param("kind"))
	name := c.Param("name")

	resourceName, clusterWide := kindToResourceName(kind)
	ns := namespace
	if clusterWide {
		ns = ""
	}

	manifest, err := kubeclient.GetGenericResourceYAML(cluster, ns, resourceName, gatewayGroup, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("GET_ERROR", err.Error()))
		return
	}

	result := models.GatewayManifest{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      name,
		Kind:      manifest.Kind,
		YAML:      manifest.YAML,
		Metadata:  models.GatewayMetadata{},
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// Describe retorna a saída do kubectl describe.
// GET /api/v1/gateways/:cluster/:namespace/:kind/:name/describe
func (h *GatewayHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	kind := strings.ToLower(c.Param("kind"))
	name := c.Param("name")

	resourceName, clusterWide := kindToResourceName(kind)
	selector := fmt.Sprintf("%s.%s", resourceName, gatewayGroup)
	args := []string{"describe", selector, name, "--context", cluster}
	if !clusterWide && namespace != "" && namespace != "_" {
		args = append(args, "-n", namespace)
	}

	cmd := exec.Command("kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DESCRIBE_ERROR", fmt.Sprintf("%v: %s", err, string(out))))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster":   cluster,
		"namespace": namespace,
		"kind":      kind,
		"name":      name,
		"describe":  string(out),
	})
}

type gatewayDiffRequest struct {
	Original string `json:"originalYaml"`
	Updated  string `json:"updatedYaml"`
	FileName string `json:"fileName"`
}

// Diff computa um unified diff entre dois YAMLs de Gateway API.
// POST /api/v1/gateways/diff
func (h *GatewayHandler) Diff(c *gin.Context) {
	var req gatewayDiffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	fileName := req.FileName
	if fileName == "" {
		fileName = "resource.yaml"
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(req.Original),
		B:        difflib.SplitLines(req.Updated),
		FromFile: "original/" + fileName,
		ToFile:   "updated/" + fileName,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DIFF_ERROR", err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"unifiedDiff": text, "hasChanges": text != ""},
	})
}

type gatewayValidateRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	YAML      string `json:"yaml"`
	Kind      string `json:"kind"`
}

// Validate faz dry-run server-side do manifesto.
// POST /api/v1/gateways/validate
func (h *GatewayHandler) Validate(c *gin.Context) {
	var req gatewayValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	if err := kubeclient.ApplyGenericResource(req.Cluster, req.Namespace, req.YAML, true, false); err != nil {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("VALIDATION_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "Manifesto válido (dry-run OK)"}})
}

type gatewayApplyRequest struct {
	YAML   string `json:"yaml"`
	DryRun bool   `json:"dryRun"`
	Force  bool   `json:"force"`
}

// Create cria um novo recurso Gateway API.
// POST /api/v1/gateways/:cluster/:namespace/:kind
func (h *GatewayHandler) Create(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	kind := strings.ToLower(c.Param("kind"))

	var req gatewayApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	_, clusterWide := kindToResourceName(kind)
	ns := namespace
	if clusterWide {
		ns = ""
	}

	if err := kubeclient.ApplyGenericResource(cluster, ns, req.YAML, req.DryRun, req.Force); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CREATE_ERROR", err.Error()))
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "create_gateway",
			Resource: fmt.Sprintf("%s/%s", namespace, kind),
			Cluster:  cluster,
			Status:   "success",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"cluster": cluster, "namespace": namespace, "kind": kind,
			"dryRun": req.DryRun, "appliedAt": time.Now().Format(time.RFC3339),
		},
	})
}

// Apply atualiza um recurso Gateway API existente.
// PUT /api/v1/gateways/:cluster/:namespace/:kind/:name
func (h *GatewayHandler) Apply(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	kind := strings.ToLower(c.Param("kind"))
	name := c.Param("name")

	var req gatewayApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", err.Error()))
		return
	}

	_, clusterWide := kindToResourceName(kind)
	ns := namespace
	if clusterWide {
		ns = ""
	}

	if err := kubeclient.ApplyGenericResource(cluster, ns, req.YAML, req.DryRun, req.Force); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "apply_gateway",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   "success",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"name": name, "namespace": namespace, "cluster": cluster, "kind": kind,
			"dryRun": req.DryRun, "appliedAt": time.Now().Format(time.RFC3339),
		},
	})
}

// Delete remove um recurso Gateway API.
// DELETE /api/v1/gateways/:cluster/:namespace/:kind/:name
func (h *GatewayHandler) Delete(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	kind := strings.ToLower(c.Param("kind"))
	name := c.Param("name")

	resourceName, clusterWide := kindToResourceName(kind)
	ns := namespace
	if clusterWide {
		ns = ""
	}

	if err := kubeclient.DeleteGenericResource(cluster, ns, resourceName, gatewayGroup, name); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DELETE_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		_ = h.historyTracker.Log(history.HistoryEntry{
			Action:   "delete_gateway",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   "success",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("%s/%s deletado com sucesso", kind, name),
	})
}
