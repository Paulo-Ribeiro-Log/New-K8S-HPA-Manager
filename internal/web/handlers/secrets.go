package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// SecretHandler gerencia as rotas de Secrets (placeholder KISS)
type SecretHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewSecretHandler cria um handler com dependências já existentes
func NewSecretHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *SecretHandler {
	return &SecretHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// List retorna Secrets com filtros básicos (cluster obrigatório)
func (h *SecretHandler) List(c *gin.Context) {
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
	secrets, err := kubeClient.ListSecrets(c.Request.Context(), namespaces, search, showSystem)
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
		"data":    secrets,
		"count":   len(secrets),
	})
}

// Get retorna o manifesto completo de um Secret específico
func (h *SecretHandler) Get(c *gin.Context) {
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
	manifest, err := kubeClient.GetSecret(c.Request.Context(), namespace, name)
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
func (h *SecretHandler) Diff(c *gin.Context) {
	var req secretDiffRequest
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
func (h *SecretHandler) Validate(c *gin.Context) {
	var req secretValidateRequest
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
	sanitizedYAML, err := sanitizeSecretYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ValidateSecret(c.Request.Context(), sanitizedYAML, req.FieldManager, req.Namespace)
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
func (h *SecretHandler) Apply(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req secretApplyRequest
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
		if manifest, err := kubeClient.GetSecret(ctx, namespace, name); err == nil {
			before = secretManifestToHistoryMap(manifest)
		}
	}

	start := time.Now()
	sanitizedYAML, err := sanitizeSecretYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ApplySecret(ctx, sanitizedYAML, req.FieldManager, namespace, name, req.DryRun)
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
		after := secretToHistoryMap(result)
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

type secretDiffRequest struct {
	Original string `json:"originalYaml"`
	Updated  string `json:"updatedYaml"`
	FileName string `json:"fileName"`
}

type secretValidateRequest struct {
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
}

type secretApplyRequest struct {
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
	DryRun       bool   `json:"dryRun"`
}

func sanitizeSecretYAML(yamlContent string) (string, error) {
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

func secretManifestToHistoryMap(manifest *models.SecretManifest) map[string]interface{} {
	if manifest == nil {
		return nil
	}
	return map[string]interface{}{
		"yaml":            manifest.YAML,
		"resourceVersion": manifest.Metadata.ResourceVersion,
	}
}

func secretToHistoryMap(cm *corev1.Secret) map[string]interface{} {
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
