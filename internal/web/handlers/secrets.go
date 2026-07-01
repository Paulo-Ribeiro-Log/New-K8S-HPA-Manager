package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
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
	sanitizedYAML, err := sanitizeSecretYAML(req.YAML, "", req.Namespace)
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
	sanitizedYAML, err := sanitizeSecretYAML(req.YAML, name, namespace)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_YAML", err.Error()))
		return
	}

	result, err := kubeClient.ApplySecret(ctx, sanitizedYAML, req.FieldManager, namespace, name, req.DryRun, req.Force)
	if err != nil {
		if checkForbidden(c, err) { return }
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
	Force        bool   `json:"force"`
}

func sanitizeSecretYAML(yamlContent, enforceName, enforceNamespace string) (string, error) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlContent), &obj); err != nil {
		return "", fmt.Errorf("invalid secret yaml: %w", err)
	}

	metadata, _ := obj["metadata"].(map[string]interface{})
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	// Forçar nome e namespace corretos (previne erro de mismatch)
	if enforceName != "" {
		metadata["name"] = enforceName
	}
	if enforceNamespace != "" {
		metadata["namespace"] = enforceNamespace
	}

	delete(metadata, "managedFields")
	delete(metadata, "resourceVersion")
	delete(metadata, "uid")
	delete(metadata, "generation")
	delete(metadata, "creationTimestamp")
	delete(metadata, "selfLink")

	// Limpar anotações do kubectl
	removedAnnotations := []string{}
	if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
		// Remover todas as anotações kubectl.*
		for key := range annotations {
			if strings.HasPrefix(key, "kubectl.kubernetes.io/") {
				removedAnnotations = append(removedAnnotations, key)
				delete(annotations, key)
			}
		}
		// Se não sobrou nenhuma anotação, remover o map completo
		if len(annotations) == 0 {
			delete(metadata, "annotations")
		} else {
			metadata["annotations"] = annotations
		}
	}


	obj["metadata"] = metadata

	// ESTRATÉGIA ROBUSTA: Extrair "data" e "stringData" ANTES do yaml.Marshal.
	//
	// O sigs.k8s.io/yaml faz internamente YAML→JSON→YAML via go-yaml/v3.
	// Para strings longas (>80 chars), go-yaml/v3 pode emitir plain scalars com
	// line-folding implícito, inserindo espaços/newlines nos valores base64.
	// Esses caracteres tornam o base64 inválido quando a API do Kubernetes tenta
	// decodificá-lo → "illegal base64 data at input byte N".
	//
	// Solução: remover data/stringData do objeto antes do Marshal e reinjetar
	// manualmente como scalars literais simples (base64 é seguro como plain scalar).
	dataVals := secretExtractDataValues(obj, "data")
	stringDataVals := secretExtractDataValues(obj, "stringData")
	delete(obj, "data")
	delete(obj, "stringData")

	cleaned, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal sanitized secret: %w", err)
	}

	result := string(cleaned)

	// Reintroduzir "data" como scalars literais: base64 usa apenas [A-Za-z0-9+/=]
	// que são sempre válidos como plain YAML scalars, sem risco de line-folding.
	if len(dataVals) > 0 {
		result += "data:\n"
		for _, k := range secretSortedKeys(dataVals) {
			result += fmt.Sprintf("  %s: %s\n", k, dataVals[k])
		}
	}

	// Reintroduzir "stringData" via yaml.Marshal (valores arbitrários, não base64)
	if len(stringDataVals) > 0 {
		sdInterface := make(map[string]interface{}, len(stringDataVals))
		for k, v := range stringDataVals {
			sdInterface[k] = v
		}
		sdYAML, err := yaml.Marshal(map[string]interface{}{"stringData": sdInterface})
		if err == nil {
			result += string(sdYAML)
		}
	}

	return result, nil
}

// secretExtractDataValues extrai valores de um campo map como strings base64.
// Converte []byte (decodificado por go-yaml como !!binary) de volta para base64 string.
func secretExtractDataValues(obj map[string]interface{}, field string) map[string]string {
	result := make(map[string]string)
	rawMap, ok := obj[field].(map[string]interface{})
	if !ok {
		return result
	}
	for k, v := range rawMap {
		switch val := v.(type) {
		case []byte:
			result[k] = base64.StdEncoding.EncodeToString(val)
		case string:
			result[k] = val
		}
	}
	return result
}

// secretSortedKeys retorna as chaves de um map ordenadas para output determinístico.
func secretSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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

// Create cria um novo Secret no cluster/namespace especificado
func (h *SecretHandler) Create(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))

	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Cluster and namespace must be provided",
			},
		})
		return
	}

	var req secretApplyRequest
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

	kubeClient := kubeclient.NewClient(clientset, cluster)

	// Parse YAML para extrair o nome do secret
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(req.YAML), &obj); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_YAML",
				"message": fmt.Sprintf("Invalid YAML: %v", err),
			},
		})
		return
	}

	metadata, _ := obj["metadata"].(map[string]interface{})
	if metadata == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_METADATA",
				"message": "Secret YAML must contain metadata",
			},
		})
		return
	}

	secretName, _ := metadata["name"].(string)
	if secretName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_NAME",
				"message": "Secret YAML must contain metadata.name",
			},
		})
		return
	}

	startTime := time.Now()

	// Aplicar o secret (usando apply para criar ou atualizar)
	result, err := kubeClient.ApplySecret(c.Request.Context(), req.YAML, req.FieldManager, namespace, secretName, false, true)
	if err != nil {
		if checkForbidden(c, err) { return }
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "APPLY_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	duration := time.Since(startTime)

	// Registrar no histórico
	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "create_secret",
			Resource: fmt.Sprintf("%s/%s", namespace, secretName),
			Cluster:  cluster,
			Before:   nil,
			After: map[string]interface{}{
				"name":            secretName,
				"namespace":       namespace,
				"resourceVersion": result.ResourceVersion,
			},
			Status:   "success",
			Duration: duration.Milliseconds(),
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
			"createdAt":       time.Now().UTC(),
		},
	})
}

// Describe retorna a saída do kubectl describe para um Secret
func (h *SecretHandler) Describe(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster, namespace e name são obrigatórios"})
		return
	}

	// Executar kubectl describe
	output, err := kubeclient.ExecuteKubectlDescribe(cluster, "secret", name, namespace)
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

// Delete deleta um Secret específico.
// Secrets de revisão Helm (sh.helm.release.*) podem ser removidos por qualquer usuário autenticado.
// Outros secrets requerem membership no grupo SRE.
func (h *SecretHandler) Delete(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	// Verificar permissão: Helm revision secrets são removíveis por qualquer autenticado;
	// secrets regulares requerem SRE.
	isHelmRevision := strings.HasPrefix(name, "sh.helm.release.")
	if !isHelmRevision {
		isSREVal, exists := c.Get("isSRE")
		isSRE, _ := isSREVal.(bool)
		if !exists || !isSRE {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "FORBIDDEN",
					"message": "Remoção de secrets requer permissão SRE (grupo VV_CLOUD_SRE)",
				},
			})
			return
		}
	}

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
	err = kubeClient.DeleteSecret(c.Request.Context(), namespace, name)
	if err != nil {
		if checkForbidden(c, err) { return }
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error": gin.H{
					"code":    "NOT_FOUND",
					"message": fmt.Sprintf("Secret %s/%s not found", namespace, name),
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "DELETE_ERROR",
				"message": fmt.Sprintf("Failed to delete secret: %v", err),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Secret %s/%s deleted successfully", namespace, name),
	})
}

// akvExternalSecretName monta o nome do ExternalSecret padrão usado pelo SRE Tools
// para sincronização com Azure Key Vault: sempre "sre-tools-external-secrets-<namespace>".
func akvExternalSecretName(namespace string) string {
	return fmt.Sprintf("sre-tools-external-secrets-%s", namespace)
}

// ResyncAKV força o external-secrets operator a ressincronizar imediatamente com o
// Azure Key Vault, anotando o ExternalSecret com force-sync=<unix timestamp> --overwrite.
// Aproveita o cluster/namespace já selecionados na UI — não precisa do nome do Secret,
// apenas do namespace (o nome do ExternalSecret é fixo por convenção do SRE Tools).
func (h *SecretHandler) ResyncAKV(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	if cluster == "" || namespace == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster e namespace são obrigatórios"))
		return
	}

	resourceName := akvExternalSecretName(namespace)
	timestamp := time.Now().Unix()
	annotation := fmt.Sprintf("force-sync=%d", timestamp)

	args := []string{
		"annotate", "externalsecret", resourceName,
		annotation,
		"-n", namespace,
		"--context", cluster,
		"--overwrite",
	}
	command := "kubectl " + strings.Join(args, " ")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()

	status := "success"
	if err != nil {
		status = "error"
	}
	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "resync_akv",
			Resource: fmt.Sprintf("%s/%s", namespace, resourceName),
			Cluster:  cluster,
			After:    map[string]interface{}{"annotation": annotation, "command": command},
			Status:   status,
		}
		if logErr := h.historyTracker.Log(entry); logErr != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", logErr)
		}
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"command": command,
			"output":  strings.TrimSpace(string(output)),
			"error": gin.H{
				"code":    "ANNOTATE_ERROR",
				"message": fmt.Sprintf("%v - %s", err, strings.TrimSpace(string(output))),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"command":      command,
		"output":       strings.TrimSpace(string(output)),
		"resourceName": resourceName,
		"namespace":    namespace,
		"cluster":      cluster,
		"timestamp":    timestamp,
	})
}
