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
	corev1 "k8s.io/api/core/v1"
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
	// Atenção: successful_jobs/failed_jobs são os LIMITES de histórico do spec
	// (successfulJobsHistoryLimit/failedJobsHistoryLimit), não contagens — mantidos com esse nome
	// por compatibilidade. As contagens reais dos Jobs retidos estão em history_succeeded/failed.
	SuccessfulJobs int32 `json:"successful_jobs"`
	FailedJobs     int32 `json:"failed_jobs"`

	// Agendamento (calculado aqui; o K8s não expõe a próxima execução)
	TimeZone         string  `json:"time_zone"`                    // spec.timeZone, CRON_TZ= da expressão ou "UTC" (padrão do controller)
	NextScheduleTime *string `json:"next_schedule_time,omitempty"` // RFC3339
	ScheduleError    string  `json:"schedule_error,omitempty"`     // expressão que o parser não entendeu
	LastScheduleAt   *string `json:"last_schedule_at,omitempty"`   // RFC3339 (last_schedule_time sem fuso fica por compatibilidade)
	LastSuccessfulAt *string `json:"last_successful_time,omitempty"`
	// Missed: a execução esperada depois da última (ou da criação) já passou além da tolerância
	// (startingDeadlineSeconds ou 5min), sem job ativo e sem estar suspenso.
	Missed      bool    `json:"missed"`
	MissedSince *string `json:"missed_since,omitempty"`

	// Configuração
	ConcurrencyPolicy       string `json:"concurrency_policy"`
	StartingDeadlineSeconds *int64 `json:"starting_deadline_seconds,omitempty"`
	Image                   string `json:"image,omitempty"` // 1º container do template
	Containers              int    `json:"containers"`
	CreatedAt               string `json:"created_at"`

	// Jobs retidos pelo histórico (preenchido no List, com uma listagem de Jobs por requisição)
	HistorySucceeded int             `json:"history_succeeded"`
	HistoryFailed    int             `json:"history_failed"`
	LastJob          *CronJobLastJob `json:"last_job,omitempty"`
}

// CronJobLastJob é o Job mais recente criado pelo CronJob.
type CronJobLastJob struct {
	Name            string  `json:"name"`
	Status          string  `json:"status"` // Running | Succeeded | Failed
	StartTime       *string `json:"start_time,omitempty"`
	CompletionTime  *string `json:"completion_time,omitempty"`
	DurationSeconds int64   `json:"duration_seconds"` // até agora, se ainda rodando
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

	// Jobs dos CronJobs: uma listagem só (mesmo escopo de namespace), agrupada pelo CronJob dono.
	// Falhar aqui não derruba a lista — só faltam último job e contagens.
	jobsByCronJob := map[string][]batchv1.Job{}
	if jobList, err := client.BatchV1().Jobs(namespaceFilter).List(c.Request.Context(), metav1.ListOptions{}); err == nil {
		for _, j := range jobList.Items {
			for _, ref := range j.OwnerReferences {
				if ref.Kind == "CronJob" {
					key := j.Namespace + "/" + ref.Name
					jobsByCronJob[key] = append(jobsByCronJob[key], j)
				}
			}
		}
	}

	now := time.Now()
	cronJobs := make([]CronJobResponse, 0, len(cronJobList.Items))
	for _, cj := range cronJobList.Items {
		resp := convertCronJobToResponse(&cj)
		applyCronJobJobs(&resp, jobsByCronJob[cj.Namespace+"/"+cj.Name], now)
		cronJobs = append(cronJobs, resp)
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

	authArgs, cleanup, authErr := h.kubeManager.KubectlAuthArgs(cluster)
	if authErr != nil {
		c.JSON(http.StatusInternalServerError, errorResponse("DESCRIBE_ERROR", authErr.Error()))
		return
	}
	defer cleanup()

	output, err := kubeclient.ExecuteKubectlDescribe(authArgs, "cronjob", name, namespace)
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
		resp.LastScheduleAt = rfc3339(cj.Status.LastScheduleTime.Time)
	}
	if cj.Status.LastSuccessfulTime != nil {
		resp.LastSuccessfulAt = rfc3339(cj.Status.LastSuccessfulTime.Time)
	}
	resp.ConcurrencyPolicy = string(cj.Spec.ConcurrencyPolicy)
	if resp.ConcurrencyPolicy == "" {
		resp.ConcurrencyPolicy = string(batchv1.AllowConcurrent)
	}
	resp.StartingDeadlineSeconds = cj.Spec.StartingDeadlineSeconds
	resp.CreatedAt = cj.CreationTimestamp.UTC().Format(time.RFC3339)
	if cs := cj.Spec.JobTemplate.Spec.Template.Spec.Containers; len(cs) > 0 {
		resp.Image = cs[0].Image
		resp.Containers = len(cs)
	}
	applyCronJobSchedule(&resp, cj, time.Now())
	return resp
}

func rfc3339(t time.Time) *string {
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// applyCronJobSchedule calcula fuso, próxima execução e execução perdida.
func applyCronJobSchedule(resp *CronJobResponse, cj *batchv1.CronJob, now time.Time) {
	sched, err := parseCron(cj.Spec.Schedule)
	if err != nil {
		resp.ScheduleError = err.Error()
		resp.TimeZone = "UTC"
		return
	}
	tz := "UTC" // sem spec.timeZone o controller usa o fuso do kube-controller-manager — UTC nos managed (AKS/EKS/GKE)
	if cj.Spec.TimeZone != nil && *cj.Spec.TimeZone != "" {
		tz = *cj.Spec.TimeZone
	} else if sched.tz != "" {
		tz = sched.tz
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc, tz = time.UTC, "UTC"
	}
	resp.TimeZone = tz

	if next := sched.next(now.In(loc)); !next.IsZero() {
		resp.NextScheduleTime = rfc3339(next)
	}

	if (cj.Spec.Suspend != nil && *cj.Spec.Suspend) || len(cj.Status.Active) > 0 {
		return
	}
	base := cj.CreationTimestamp.Time
	if cj.Status.LastScheduleTime != nil {
		base = cj.Status.LastScheduleTime.Time
	}
	grace := 5 * time.Minute
	if cj.Spec.StartingDeadlineSeconds != nil && time.Duration(*cj.Spec.StartingDeadlineSeconds)*time.Second > grace {
		grace = time.Duration(*cj.Spec.StartingDeadlineSeconds) * time.Second
	}
	if expected := sched.next(base.In(loc)); !expected.IsZero() && now.After(expected.Add(grace)) {
		resp.Missed = true
		resp.MissedSince = rfc3339(expected)
	}
}

// applyCronJobJobs preenche as contagens reais e o último Job a partir dos Jobs retidos.
func applyCronJobJobs(resp *CronJobResponse, jobs []batchv1.Job, now time.Time) {
	var last *batchv1.Job
	for i := range jobs {
		j := &jobs[i]
		switch status, _ := cronJobJobStatus(j); status {
		case "Succeeded":
			resp.HistorySucceeded++
		case "Failed":
			resp.HistoryFailed++
		}
		if last == nil || j.CreationTimestamp.After(last.CreationTimestamp.Time) {
			last = j
		}
	}
	if last == nil {
		return
	}
	status, finished := cronJobJobStatus(last)
	lj := &CronJobLastJob{Name: last.Name, Status: status}
	start := last.CreationTimestamp.Time
	if last.Status.StartTime != nil {
		start = last.Status.StartTime.Time
	}
	lj.StartTime = rfc3339(start)
	end := now
	if !finished.IsZero() {
		end = finished
		lj.CompletionTime = rfc3339(finished)
	}
	if d := end.Sub(start); d > 0 {
		lj.DurationSeconds = int64(d.Seconds())
	}
	resp.LastJob = lj
}

// cronJobJobStatus classifica o Job pelas condições (Complete/Failed); sem condição final, está rodando.
func cronJobJobStatus(j *batchv1.Job) (string, time.Time) {
	for _, cond := range j.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}
		switch cond.Type {
		case batchv1.JobComplete:
			if j.Status.CompletionTime != nil {
				return "Succeeded", j.Status.CompletionTime.Time
			}
			return "Succeeded", cond.LastTransitionTime.Time
		case batchv1.JobFailed:
			return "Failed", cond.LastTransitionTime.Time
		}
	}
	return "Running", time.Time{}
}

func getHistoryCount(limit *int32) int32 {
	if limit == nil {
		return 0
	}
	return *limit
}

// createJobRequest request para criação de Job standalone
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
