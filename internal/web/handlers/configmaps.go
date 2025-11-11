package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kylelemons/godebug/diff"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// ConfigMapHandler gerencia as rotas de ConfigMaps (placeholder KISS)
type ConfigMapHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewConfigMapHandler cria um handler com dependências já existentes
func NewConfigMapHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *ConfigMapHandler {
	return &ConfigMapHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// List retorna ConfigMaps com filtros básicos (cluster obrigatório)
func (h *ConfigMapHandler) List(c *gin.Context) {
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
	configMaps, err := kubeClient.ListConfigMaps(c.Request.Context(), namespaces, search, showSystem)
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
		"data":    configMaps,
		"count":   len(configMaps),
	})
}

// Get retorna o manifesto completo de um ConfigMap específico
func (h *ConfigMapHandler) Get(c *gin.Context) {
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
	manifest, err := kubeClient.GetConfigMap(c.Request.Context(), namespace, name)
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

// Diff retornará o diff textual antes do apply
// Diff gera diff texto simples entre YAMLs
func (h *ConfigMapHandler) Diff(c *gin.Context) {
	var req configMapDiffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("Invalid body: %v", err)))
		return
	}
	if strings.TrimSpace(req.Updated) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", "updatedYaml is required"))
		return
	}

	diffText := diff.Diff(req.Original, req.Updated)
	response := gin.H{
		"success": true,
		"data": gin.H{
			"unifiedDiff": diffText,
			"hasChanges":  strings.TrimSpace(diffText) != "",
		},
	}
	c.JSON(http.StatusOK, response)
}

// Validate executa server-side apply com dry-run
func (h *ConfigMapHandler) Validate(c *gin.Context) {
	var req configMapValidateRequest
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
	sanitizedYAML, err := sanitizeConfigMapYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ValidateConfigMap(c.Request.Context(), sanitizedYAML, req.FieldManager, req.Namespace)
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
func (h *ConfigMapHandler) Apply(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req configMapApplyRequest
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
		if manifest, err := kubeClient.GetConfigMap(ctx, namespace, name); err == nil {
			before = manifestToHistoryMap(manifest)
		}
	}

	start := time.Now()
	sanitizedYAML, err := sanitizeConfigMapYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ApplyConfigMap(ctx, sanitizedYAML, req.FieldManager, namespace, name, req.DryRun)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "APPLY_ERROR"
		if apierrors.IsConflict(err) {
			status = http.StatusConflict
		}
		c.JSON(status, errorResponse(errorCode, err.Error()))
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		after := configMapToHistoryMap(result)
		entry := history.HistoryEntry{
			Action:   "apply_configmap",
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

type configMapDiffRequest struct {
	Original string `json:"originalYaml"`
	Updated  string `json:"updatedYaml"`
}

type configMapValidateRequest struct {
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
}

type configMapApplyRequest struct {
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
	DryRun       bool   `json:"dryRun"`
}

func sanitizeConfigMapYAML(yamlContent string) (string, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj); err != nil {
		return "", fmt.Errorf("invalid configmap yaml: %w", err)
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
		return "", fmt.Errorf("failed to marshal sanitized configmap: %w", err)
	}

	return string(cleaned), nil
}

func errorResponse(code, message string) gin.H {
	return gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	}
}

func manifestToHistoryMap(manifest *models.ConfigMapManifest) map[string]interface{} {
	if manifest == nil {
		return nil
	}
	return map[string]interface{}{
		"yaml":            manifest.YAML,
		"resourceVersion": manifest.Metadata.ResourceVersion,
	}
}

func configMapToHistoryMap(cm *corev1.ConfigMap) map[string]interface{} {
	if cm == nil {
		return nil
	}
	return map[string]interface{}{
		"name":            cm.Name,
		"namespace":       cm.Namespace,
		"resourceVersion": cm.ResourceVersion,
		"labels":          cm.Labels,
		"annotations":     cm.Annotations,
	}
}

func parseNamespaces(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var namespaces []string
	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" {
			continue
		}
		namespaces = append(namespaces, clean)
	}
	return namespaces
}
