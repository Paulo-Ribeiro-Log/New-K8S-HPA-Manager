package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// StatefulSetHandler gerencia as rotas de StatefulSets
type StatefulSetHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewStatefulSetHandler cria um handler com dependências já existentes
func NewStatefulSetHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *StatefulSetHandler {
	return &StatefulSetHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// List retorna StatefulSets com filtros básicos (cluster obrigatório)
func (h *StatefulSetHandler) List(c *gin.Context) {
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
	statefulsets, err := kubeClient.ListStatefulSets(c.Request.Context(), namespaces, search, showSystem)
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
		"data":    statefulsets,
		"count":   len(statefulsets),
	})
}

// Get retorna o manifesto completo de um StatefulSet específico
func (h *StatefulSetHandler) Get(c *gin.Context) {
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
	manifest, err := kubeClient.GetStatefulSet(c.Request.Context(), namespace, name)
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
func (h *StatefulSetHandler) Diff(c *gin.Context) {
	var req statefulSetDiffRequest
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
func (h *StatefulSetHandler) Validate(c *gin.Context) {
	var req statefulSetValidateRequest
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
	sanitizedYAML, err := sanitizeStatefulSetYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ValidateStatefulSet(c.Request.Context(), sanitizedYAML, req.FieldManager, req.Namespace)
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
func (h *StatefulSetHandler) Apply(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))
	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	var req statefulSetApplyRequest
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
		if manifest, err := kubeClient.GetStatefulSet(ctx, namespace, name); err == nil {
			before = statefulSetManifestToHistoryMap(manifest)
		}
	}

	start := time.Now()
	sanitizedYAML, err := sanitizeStatefulSetYAML(req.YAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	fmt.Printf("[DEBUG] Handler ApplyStatefulSet: cluster=%s, namespace=%s, name=%s, dryRun=%v, fieldManager=%s\n",
		cluster, namespace, name, req.DryRun, req.FieldManager)

	result, err := kubeClient.ApplyStatefulSet(ctx, sanitizedYAML, req.FieldManager, namespace, name, req.DryRun, req.Force)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "APPLY_ERROR"
		if apierrors.IsConflict(err) {
			status = http.StatusConflict
		}
		fmt.Printf("[ERROR] Failed to apply statefulset: %v\n", err)
		c.JSON(status, errorResponse(errorCode, err.Error()))
		return
	}

	fmt.Printf("[DEBUG] Handler ApplyStatefulSet SUCCESS: resourceVersion=%s\n", result.ResourceVersion)

	if !req.DryRun && h.historyTracker != nil {
		after := statefulSetToHistoryMap(result)
		entry := history.HistoryEntry{
			Action:   "apply_statefulset",
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

type statefulSetDiffRequest struct {
	Original string `json:"originalYaml"`
	Updated  string `json:"updatedYaml"`
	FileName string `json:"fileName"`
}

type statefulSetValidateRequest struct {
	Cluster      string `json:"cluster"`
	Namespace    string `json:"namespace"`
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
}

type statefulSetApplyRequest struct {
	YAML         string `json:"yaml"`
	FieldManager string `json:"fieldManager"`
	DryRun       bool   `json:"dryRun"`
	Force        bool   `json:"force"`
}

func sanitizeStatefulSetYAML(yamlContent string) (string, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj); err != nil {
		return "", fmt.Errorf("invalid statefulset yaml: %w", err)
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
		return "", fmt.Errorf("failed to marshal sanitized statefulset: %w", err)
	}

	return string(cleaned), nil
}

func statefulSetManifestToHistoryMap(manifest *models.StatefulSetManifest) map[string]interface{} {
	if manifest == nil {
		return nil
	}
	return map[string]interface{}{
		"yaml":            manifest.YAML,
		"resourceVersion": manifest.Metadata.ResourceVersion,
	}
}

func statefulSetToHistoryMap(sts *appsv1.StatefulSet) map[string]interface{} {
	if sts == nil {
		return nil
	}
	replicas := int32(0)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	return map[string]interface{}{
		"name":            sts.Name,
		"namespace":       sts.Namespace,
		"resourceVersion": sts.ResourceVersion,
		"labels":          sts.Labels,
		"annotations":     sts.Annotations,
		"replicas":        replicas,
		"readyReplicas":   sts.Status.ReadyReplicas,
	}
}

// Describe retorna a saída do kubectl describe para um StatefulSet
func (h *StatefulSetHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, namespace e name são obrigatórios"})
		return
	}

	// Executar kubectl describe
	output, err := kubeclient.ExecuteKubectlDescribe(cluster, "statefulset", name, namespace)
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

// Delete deleta um StatefulSet específico
func (h *StatefulSetHandler) Delete(c *gin.Context) {
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

	// Deletar statefulset
	kubeClient := kubeclient.NewClient(clientset, cluster)
	err = kubeClient.DeleteStatefulSet(c.Request.Context(), namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "NOT_FOUND",
					"message": fmt.Sprintf("StatefulSet %s/%s not found", namespace, name),
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": fmt.Sprintf("Failed to delete statefulset: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("StatefulSet %s/%s deleted successfully", namespace, name),
	})
}

// RolloutRestart reinicia um StatefulSet (kubectl rollout restart)
func (h *StatefulSetHandler) RolloutRestart(c *gin.Context) {
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
	err = kubeClient.RolloutRestartStatefulSet(c.Request.Context(), namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "NOT_FOUND",
					"message": fmt.Sprintf("StatefulSet %s/%s not found", namespace, name),
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "ROLLOUT_ERROR",
				"message": fmt.Sprintf("Failed to restart statefulset: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("StatefulSet %s/%s restarted successfully", namespace, name),
	})
}

// Scale escala um StatefulSet para o número especificado de réplicas
func (h *StatefulSetHandler) Scale(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	// Log raw request body para debug
	bodyBytes, _ := c.GetRawData()
	log.Debug().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("name", name).
		Str("body", string(bodyBytes)).
		Msg("Scale StatefulSet request received")
	
	// Restaurar body para o ShouldBindJSON
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Parse request body (usar pointer para aceitar 0 como valor válido)
	var req struct {
		Replicas *int32 `json:"replicas" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error().
			Err(err).
			Str("body", string(bodyBytes)).
			Msg("Failed to parse scale request")
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request: %v", err),
			},
		})
		return
	}

	// Validar que replicas não é negativo
	if *req.Replicas < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "Replicas must be >= 0",
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

	// Executar scale
	kubeClient := kubeclient.NewClient(clientset, cluster)
	err = kubeClient.ScaleStatefulSet(c.Request.Context(), namespace, name, *req.Replicas)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "NOT_FOUND",
					"message": fmt.Sprintf("StatefulSet %s/%s not found", namespace, name),
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "SCALE_ERROR",
				"message": fmt.Sprintf("Failed to scale statefulset: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  fmt.Sprintf("StatefulSet %s/%s scaled to %d replicas", namespace, name, *req.Replicas),
		"replicas": *req.Replicas,
	})
}
