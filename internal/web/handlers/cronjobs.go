package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"

	"github.com/gin-gonic/gin"
	"github.com/pmezard/go-difflib/difflib"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CronJobHandler gerencia requisições relacionadas a CronJobs
type CronJobHandler struct {
	kubeManager    *config.KubeConfigManager
	historyTracker *history.HistoryTracker
}

// NewCronJobHandler cria um novo handler de CronJobs
func NewCronJobHandler(km *config.KubeConfigManager, ht *history.HistoryTracker) *CronJobHandler {
	return &CronJobHandler{
		kubeManager:    km,
		historyTracker: ht,
	}
}

// CronJobResponse representa um CronJob na resposta
type CronJobResponse struct {
	Name             string  `json:"name"`
	Namespace        string  `json:"namespace"`
	Schedule         string  `json:"schedule"`
	ScheduleDesc     string  `json:"schedule_description"`
	Suspend          *bool   `json:"suspend"`
	LastScheduleTime *string `json:"last_schedule_time,omitempty"`
	ActiveJobs       int     `json:"active_jobs"`
	SuccessfulJobs   int32   `json:"successful_jobs"`
	FailedJobs       int32   `json:"failed_jobs"`
}

// List retorna todos os CronJobs do cluster (todos os namespaces ou filtrado)
// GET /api/v1/cronjobs?cluster=X&namespace=Y&namespaces=A,B&show_system=true
func (h *CronJobHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")
	namespacesParam := c.QueryArray("namespaces")

	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "MISSING_PARAMETERS", "message": "Parameter 'cluster' is required"},
		})
		return
	}

	namespaceFilter := metav1.NamespaceAll
	if namespace != "" {
		namespaceFilter = namespace
	} else if len(namespacesParam) == 1 && namespacesParam[0] != "" {
		namespaceFilter = namespacesParam[0]
	}

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "KUBERNETES_CLIENT_ERROR", "message": fmt.Sprintf("Failed to get Kubernetes client: %v", err)},
		})
		return
	}

	cronJobList, err := client.BatchV1().CronJobs(namespaceFilter).List(c.Request.Context(), metav1.ListOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "KUBERNETES_API_ERROR", "message": fmt.Sprintf("Failed to list CronJobs: %v", err)},
		})
		return
	}

	cronJobs := make([]CronJobResponse, 0, len(cronJobList.Items))
	for _, cj := range cronJobList.Items {
		cronJobs = append(cronJobs, convertCronJobToResponse(&cj))
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": cronJobs, "count": len(cronJobs)})
}

// Get retorna o manifesto YAML de um CronJob específico
// GET /api/v1/cronjobs/:cluster/:namespace/:name
func (h *CronJobHandler) Get(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}

	kc := kubeclient.NewClient(clientset, cluster)
	yamlStr, err := kc.GetCronJobYAML(c.Request.Context(), namespace, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("GET_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"cluster":   cluster,
			"namespace": namespace,
			"name":      name,
			"yaml":      yamlStr,
		},
	})
}

// Apply aplica um manifesto CronJob no cluster (YAML completo)
// PUT /api/v1/cronjobs/:cluster/:namespace/:name/yaml
func (h *CronJobHandler) Apply(c *gin.Context) {
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
	result, err := kc.ApplyCronJob(c.Request.Context(), req.YAML, namespace, name, req.DryRun)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse("APPLY_ERROR", err.Error()))
		return
	}

	if !req.DryRun && h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "apply_cronjob_yaml",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history: %v\n", err)
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
		},
	})
}

// Describe executa kubectl describe em um CronJob
// GET /api/v1/cronjobs/:cluster/:namespace/:name/describe
func (h *CronJobHandler) Describe(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	output, err := kubeclient.ExecuteKubectlDescribe(h.kubeManager.ConfigPath(), h.kubeManager.ResolveContext(cluster), "cronjob", name, namespace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DESCRIBE_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"describe":  output,
		"cluster":   cluster,
		"namespace": namespace,
		"name":      name,
	})
}

// Diff retorna o diff unificado entre o YAML original e o editado
// POST /api/v1/cronjobs/diff
func (h *CronJobHandler) Diff(c *gin.Context) {
	var req struct {
		OriginalYAML string `json:"originalYaml"`
		UpdatedYAML  string `json:"updatedYaml"`
		FileName     string `json:"fileName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("INVALID_REQUEST", fmt.Sprintf("Invalid body: %v", err)))
		return
	}

	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(req.OriginalYAML),
		B:        difflib.SplitLines(req.UpdatedYAML),
		FromFile: fmt.Sprintf("a/%s", req.FileName),
		ToFile:   fmt.Sprintf("b/%s", req.FileName),
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
			"hasChanges":  text != "",
		},
	})
}

// Validate executa dry-run do YAML do CronJob
// POST /api/v1/cronjobs/validate
func (h *CronJobHandler) Validate(c *gin.Context) {
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
	if req.Cluster == "" || strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster and yaml are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}

	kc := kubeclient.NewClient(clientset, req.Cluster)
	_, err = kc.ApplyCronJob(c.Request.Context(), req.YAML, req.Namespace, req.Name, true)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, errorResponse("VALIDATION_ERROR", err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"valid": true}})
}

// Trigger cria um Job manualmente a partir do CronJob
// POST /api/v1/cronjobs/:cluster/:namespace/:name/trigger
func (h *CronJobHandler) Trigger(c *gin.Context) {
	cluster := strings.TrimSpace(c.Param("cluster"))
	namespace := strings.TrimSpace(c.Param("namespace"))
	name := strings.TrimSpace(c.Param("name"))

	if cluster == "" || namespace == "" || name == "" {
		c.JSON(http.StatusBadRequest, errorResponse("MISSING_PARAMETER", "cluster, namespace and name are required"))
		return
	}

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("CLIENT_ERROR", fmt.Sprintf("Failed to get client: %v", err)))
		return
	}

	start := time.Now()
	kc := kubeclient.NewClient(clientset, cluster)
	jobName, err := kc.TriggerCronJob(c.Request.Context(), namespace, name)
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse("TRIGGER_ERROR", err.Error()))
		return
	}

	if h.historyTracker != nil {
		entry := history.HistoryEntry{
			Action:   "trigger_cronjob",
			Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster:  cluster,
			After:    map[string]interface{}{"jobName": jobName},
			Status:   "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Job '%s' criado com sucesso a partir do CronJob '%s'", jobName, name),
		"data":    gin.H{"jobName": jobName, "namespace": namespace, "cluster": cluster},
	})
}

// Update atualiza suspend ou schedule de um CronJob (toggle rápido)
// PUT /api/v1/cronjobs/:cluster/:namespace/:name
func (h *CronJobHandler) Update(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	var req struct {
		Suspend  *bool   `json:"suspend,omitempty"`
		Schedule *string `json:"schedule,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("Invalid request body: %v", err)},
		})
		return
	}
	if req.Suspend == nil && req.Schedule == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": "At least one of 'suspend' or 'schedule' must be provided"},
		})
		return
	}

	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "KUBERNETES_CLIENT_ERROR", "message": fmt.Sprintf("Failed to get Kubernetes client: %v", err)},
		})
		return
	}

	cronJob, err := client.BatchV1().CronJobs(namespace).Get(c.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   gin.H{"code": "CRONJOB_NOT_FOUND", "message": fmt.Sprintf("CronJob not found: %v", err)},
		})
		return
	}

	before := map[string]interface{}{"schedule": cronJob.Spec.Schedule, "suspend": cronJob.Spec.Suspend}

	if req.Suspend != nil {
		cronJob.Spec.Suspend = req.Suspend
	}
	if req.Schedule != nil {
		cronJob.Spec.Schedule = *req.Schedule
	}

	start := time.Now()
	updatedCronJob, err := client.BatchV1().CronJobs(namespace).Update(c.Request.Context(), cronJob, metav1.UpdateOptions{})
	if err != nil {
		if checkForbidden(c, err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "KUBERNETES_UPDATE_ERROR", "message": fmt.Sprintf("Failed to update CronJob: %v", err)},
		})
		return
	}

	if h.historyTracker != nil {
		after := map[string]interface{}{"schedule": updatedCronJob.Spec.Schedule, "suspend": updatedCronJob.Spec.Suspend}
		entry := history.HistoryEntry{
			Action: "update_cronjob", Resource: fmt.Sprintf("%s/%s", namespace, name),
			Cluster: cluster, Before: before, After: after, Status: "success",
			Duration: time.Since(start).Milliseconds(),
		}
		if err := h.historyTracker.Log(entry); err != nil {
			fmt.Printf("warning: failed to record history entry: %v\n", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("CronJob '%s' updated successfully", name),
		"data":    convertCronJobToResponse(updatedCronJob),
	})
}

// convertCronJobToResponse converte CronJob do Kubernetes para resposta
func convertCronJobToResponse(cj *batchv1.CronJob) CronJobResponse {
	resp := CronJobResponse{
		Name:           cj.Name,
		Namespace:      cj.Namespace,
		Schedule:       cj.Spec.Schedule,
		ScheduleDesc:   describeCronSchedule(cj.Spec.Schedule),
		Suspend:        cj.Spec.Suspend,
		ActiveJobs:     len(cj.Status.Active),
		SuccessfulJobs: getHistoryCount(cj.Spec.SuccessfulJobsHistoryLimit),
		FailedJobs:     getHistoryCount(cj.Spec.FailedJobsHistoryLimit),
	}
	if cj.Status.LastScheduleTime != nil {
		timeStr := cj.Status.LastScheduleTime.Format("2006-01-02 15:04:05")
		resp.LastScheduleTime = &timeStr
	}
	return resp
}

func describeCronSchedule(schedule string) string {
	switch schedule {
	case "0 * * * *":
		return "A cada hora"
	case "*/5 * * * *":
		return "A cada 5 minutos"
	case "0 0 * * *":
		return "Todo dia à meia-noite"
	case "0 2 * * *":
		return "Todo dia às 2:00 AM"
	case "0 0 * * 0":
		return "Todo domingo à meia-noite"
	default:
		return schedule
	}
}

func getHistoryCount(limit *int32) int32 {
	if limit == nil {
		return 0
	}
	return *limit
}

// createJobRequest request para criação de Job standalone
type createJobRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	YAML      string `json:"yaml"`
	DryRun    bool   `json:"dry_run"`
}

// CreateJob cria um Job a partir de YAML via K8s API (Helm-safe).
// POST /api/v1/jobs
func (h *CronJobHandler) CreateJob(c *gin.Context) {
	var req createJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campo 'cluster' é obrigatório"})
		return
	}
	if strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campo 'yaml' é obrigatório"})
		return
	}

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}

	kc := kubeclient.NewClient(clientset, req.Cluster)
	job, err := kc.CreateJobFromYAML(c.Request.Context(), req.YAML, req.Namespace, req.DryRun)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"name":      job.Name,
		"namespace": job.Namespace,
		"dry_run":   req.DryRun,
	})
}

// GetJobTemplate retorna YAML de template de Job derivado de um CronJob.
// GET /api/v1/cronjobs/:cluster/:namespace/:name/job-template
func (h *CronJobHandler) GetJobTemplate(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}

	kc := kubeclient.NewClient(clientset, cluster)
	yamlContent, err := kc.GetJobTemplateYAML(c.Request.Context(), namespace, name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"yaml": yamlContent})
}

// CreateCronJob cria um CronJob novo a partir de YAML via K8s API.
// POST /api/v1/cronjobs/new
func (h *CronJobHandler) CreateCronJob(c *gin.Context) {
	var req struct {
		Cluster   string `json:"cluster"`
		Namespace string `json:"namespace"`
		YAML      string `json:"yaml"`
		DryRun    bool   `json:"dry_run"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campo 'cluster' é obrigatório"})
		return
	}
	if strings.TrimSpace(req.YAML) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campo 'yaml' é obrigatório"})
		return
	}

	clientset, err := h.kubeManager.GetClient(req.Cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}

	kc := kubeclient.NewClient(clientset, req.Cluster)
	cj, err := kc.CreateCronJobFromYAML(c.Request.Context(), req.YAML, req.Namespace, req.DryRun)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"name":      cj.Name,
		"namespace": cj.Namespace,
		"schedule":  cj.Spec.Schedule,
		"dry_run":   req.DryRun,
	})
}
