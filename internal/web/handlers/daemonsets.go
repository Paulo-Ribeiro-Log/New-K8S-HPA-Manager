package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// DaemonSetHandler gerencia as rotas de DaemonSets
type DaemonSetHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewDaemonSetHandler cria um handler com dependências já existentes
func NewDaemonSetHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *DaemonSetHandler {
	return &DaemonSetHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// List retorna DaemonSets com filtros básicos (cluster obrigatório)
func (h *DaemonSetHandler) List(c *gin.Context) {
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
	search := c.Query("search")

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

	kubeClient := kubeclient.NewClient(clientset, cluster)
	daemonsets, err := kubeClient.ListDaemonSets(c.Request.Context(), namespaces, search, showSystem)
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    daemonsets,
		"count":   len(daemonsets),
	})
}

// Get retorna o manifesto completo de um DaemonSet específico
func (h *DaemonSetHandler) Get(c *gin.Context) {
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

	kubeClient := kubeclient.NewClient(clientset, cluster)
	manifest, err := kubeClient.GetDaemonSet(c.Request.Context(), namespace, name)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "GET_ERROR"
		if apierrors.IsNotFound(err) {
			status = http.StatusNotFound
			errorCode = "NOT_FOUND"
		}
		c.JSON(status, gin.H{
			"success": false,
			"error": gin.H{
				"code":    errorCode,
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

// Diff gera diff texto simples entre YAMLs
func (h *DaemonSetHandler) Diff(c *gin.Context) {
	var req daemonSetDiffRequest
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
	response := gin.H{
		"success": true,
		"data": gin.H{
			"unifiedDiff": text,
			"hasChanges":  strings.TrimSpace(text) != "",
		},
	}
	c.JSON(http.StatusOK, response)
}

// Validate executa server-side apply com dry-run
func (h *DaemonSetHandler) Validate(c *gin.Context) {
	var req daemonSetValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("Invalid body: %v", err)))
		return
	}

	if strings.TrimSpace(req.Cluster) == "" || strings.TrimSpace(req.Namespace) == "" || strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and yaml are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}

	kubeClient := kubeclient.NewClient(clientset, req.Cluster)
	sanitizedYAML, err := sanitizeDaemonSetYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ValidateDaemonSet(c.Request.Context(), sanitizedYAML, req.FieldManager, req.Namespace)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "VALIDATION_ERROR"
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			status = http.StatusUnprocessableEntity
		}
		c.JSON(status, errorResponse(errorCode, err.Error()))
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

// Apply executa server-side apply opcionalmente com dry-run e registra histórico
func (h *DaemonSetHandler) Apply(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req daemonSetApplyRequest
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
		if manifest, err := kubeClient.GetDaemonSet(ctx, namespace, name); err == nil {
			before = daemonSetManifestToHistoryMap(manifest)
		}
	}

	start := time.Now()
	sanitizedYAML, err := sanitizeDaemonSetYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	fmt.Printf("[DEBUG] Handler ApplyDaemonSet: cluster=%s, namespace=%s, name=%s, dryRun=%v, fieldManager=%s\n",
		cluster, namespace, name, req.DryRun, req.FieldManager)

	result, err := kubeClient.ApplyDaemonSet(ctx, sanitizedYAML, req.FieldManager, namespace, name, req.DryRun, req.Force)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "APPLY_ERROR"
		if apierrors.IsConflict(err) {
			status = http.StatusConflict
		}
		fmt.Printf("[ERROR] Failed to apply daemonset: %v\n", err)
		c.JSON(status, errorResponse(errorCode, err.Error()))
		return
	}

	fmt.Printf("[DEBUG] Handler ApplyDaemonSet SUCCESS: resourceVersion=%s\n", result.ResourceVersion)

	if !req.DryRun && h.historyTracker != nil {
		after := daemonSetToHistoryMap(result)
		entry := history.HistoryEntry{
			Action:   "apply_daemonset",
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

type daemonSetDiffRequest struct {
	Original string `json:"originalYaml"`
	Updated  string `json:"updatedYaml"`
	FileName string `json:"fileName"`
}

type daemonSetValidateRequest struct {
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
}

type daemonSetApplyRequest struct {
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
	DryRun       bool   `json:"dryRun"`
	Force        bool   `json:"force"`
}

func sanitizeDaemonSetYAML(yamlContent string) (string, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj); err != nil {
		return "", fmt.Errorf("invalid daemonset yaml: %w", err)
	}

	metadata, _ := obj["metadata"].(map[string]interface{})
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	delete(metadata, "managedFields")
	delete(metadata, "resourceVersion")
	delete(metadata, "uid")
	delete(metadata, "generation")
	delete(metadata, "creationTimestamp")
	delete(metadata, "selfLink")
	delete(metadata, "annotations.kubectl.kubernetes.io/last-applied-configuration")

	obj["metadata"] = metadata

	cleaned, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal sanitized daemonset: %w", err)
	}

	return string(cleaned), nil
}

func daemonSetManifestToHistoryMap(manifest *models.DaemonSetManifest) map[string]interface{} {
	if manifest == nil {
		return nil
	}
	return map[string]interface{}{
		"yaml":            manifest.YAML,
		"resourceVersion": manifest.Metadata.ResourceVersion,
	}
}

func daemonSetToHistoryMap(ds *appsv1.DaemonSet) map[string]interface{} {
	if ds == nil {
		return nil
	}
	return map[string]interface{}{
		"name":                   ds.Name,
		"namespace":              ds.Namespace,
		"resourceVersion":        ds.ResourceVersion,
		"labels":                 ds.Labels,
		"annotations":            ds.Annotations,
		"desiredNumberScheduled": ds.Status.DesiredNumberScheduled,
		"numberReady":            ds.Status.NumberReady,
	}
}

// Describe retorna a saída do kubectl describe para um DaemonSet
func (h *DaemonSetHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, namespace e name são obrigatórios"})
		return
	}

	// Executar kubectl describe
	output, err := kubeclient.ExecuteKubectlDescribe(cluster, "daemonset", name, namespace)
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

// Delete deleta um DaemonSet específico
func (h *DaemonSetHandler) Delete(c *gin.Context) {
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

	// Obter clientset
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

	// Deletar daemonset
	kubeClient := kubeclient.NewClient(clientset, cluster)
	err = kubeClient.DeleteDaemonSet(c.Request.Context(), namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "NOT_FOUND",
					"message": fmt.Sprintf("DaemonSet %s/%s not found", namespace, name),
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": fmt.Sprintf("Failed to delete daemonset: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("DaemonSet %s/%s deleted successfully", namespace, name),
	})
}

// RolloutRestart reinicia um DaemonSet (kubectl rollout restart)
func (h *DaemonSetHandler) RolloutRestart(c *gin.Context) {
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

	// Obter clientset
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

	// Executar rollout restart
	kubeClient := kubeclient.NewClient(clientset, cluster)
	err = kubeClient.RolloutRestartDaemonSet(c.Request.Context(), namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "NOT_FOUND",
					"message": fmt.Sprintf("DaemonSet %s/%s not found", namespace, name),
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "ROLLOUT_ERROR",
				"message": fmt.Sprintf("Failed to restart daemonset: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("DaemonSet %s/%s restarted successfully", namespace, name),
	})
}
