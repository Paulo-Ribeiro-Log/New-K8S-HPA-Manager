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

// VPAHandler gerencia as rotas de VPAs (Vertical Pod Autoscalers)
type VPAHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewVPAHandler cria um handler de VPA
func NewVPAHandler(km *config.KubeConfigManager) *VPAHandler {
	return &VPAHandler{kubeManager: km}
}

// List retorna VPAs com filtros (cluster obrigatório)
// GET /api/v1/vpas?cluster=X&namespaces=Y&showSystem=true
func (h *VPAHandler) List(c *gin.Context) {
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

	// Verificar se CRD do VPA está instalado
	if !kubeclient.VPACRDExists(cluster) {
		c.JSON(http.StatusOK, gin.H{
			"success":         true,
			"data":            []interface{}{},
			"count":           0,
			"crdNotInstalled": true,
		})
		return
	}

	namespaces := parseNamespaces(c.Query("namespaces"))
	showSystem := c.Query("showSystem") == "true"

	var allVPAs []interface{}

	if len(namespaces) == 0 {
		// Listar de todos os namespaces
		vpas, err := kubeclient.GetVPAs(cluster, "")
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
		for _, v := range vpas {
			if !showSystem && isSystemNamespaceFn(v.Namespace) {
				continue
			}
			allVPAs = append(allVPAs, v)
		}
	} else {
		for _, ns := range namespaces {
			if !showSystem && isSystemNamespaceFn(ns) {
				continue
			}
			vpas, err := kubeclient.GetVPAs(cluster, ns)
			if err != nil {
				continue
			}
			for _, v := range vpas {
				allVPAs = append(allVPAs, v)
			}
		}
	}

	if allVPAs == nil {
		allVPAs = []interface{}{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    allVPAs,
		"count":   len(allVPAs),
	})
}

// Get retorna o manifesto YAML completo de um VPA
// GET /api/v1/vpas/:cluster/:namespace/:name
func (h *VPAHandler) Get(c *gin.Context) {
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

	manifest, err := kubeclient.GetVPAYAML(cluster, namespace, name)
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

// Diff gera diff texto entre dois YAMLs de VPA
// POST /api/v1/vpas/diff
func (h *VPAHandler) Diff(c *gin.Context) {
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

// Validate executa dry-run de um manifesto VPA
// POST /api/v1/vpas/validate
func (h *VPAHandler) Validate(c *gin.Context) {
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

	err := kubeclient.ApplyVPA(req.Cluster, ns, req.YAML, true, false)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("VALIDATION_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"valid": true},
	})
}

// Apply aplica um manifesto VPA no cluster
// PUT /api/v1/vpas/:cluster/:namespace/:name
func (h *VPAHandler) Apply(c *gin.Context) {
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

	if err := kubeclient.ApplyVPA(cluster, namespace, req.YAML, req.DryRun, req.Force); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"name":      name,
			"namespace": namespace,
			"cluster":   cluster,
			"dryRun":    req.DryRun,
		},
	})
}

// Delete deleta um VPA específico
// DELETE /api/v1/vpas/:cluster/:namespace/:name
func (h *VPAHandler) Delete(c *gin.Context) {
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

	if err := kubeclient.DeleteVPA(cluster, namespace, name); err != nil {
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
		"message": fmt.Sprintf("VPA %s/%s deleted successfully", namespace, name),
	})
}

// Describe retorna a saída do kubectl describe para um VPA
// GET /api/v1/vpas/:cluster/:namespace/:name/describe
func (h *VPAHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, namespace e name são obrigatórios"})
		return
	}

	output, err := kubeclient.ExecuteKubectlDescribe(h.kubeManager.ConfigPath(), h.kubeManager.ResolveContext(cluster), "verticalpodautoscaler", name, namespace)
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

// isSystemNamespaceFn verifica se um namespace é de sistema (reutiliza lógica existente)
func isSystemNamespaceFn(ns string) bool {
	systemNS := map[string]bool{
		"kube-system": true, "kube-public": true, "kube-node-lease": true,
		"istio-system": true, "cert-manager": true, "gatekeeper-system": true,
		"flux-system": true, "argocd": true, "elastic-system": true,
		"logging": true, "dynatrace": true, "monitoring": true,
	}
	return systemNS[ns]
}
