package handlers

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/history"

	"github.com/gin-gonic/gin"
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

// List retorna todos os CronJobs (de todos os namespaces se namespace não especificado)
func (h *CronJobHandler) List(c *gin.Context) {
	cluster := c.Query("cluster")
	namespace := c.Query("namespace")

	if cluster == "" {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETERS",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	// Definir namespace para busca (vazio significa todos os namespaces)
	namespaceFilter := namespace
	if namespace == "" {
		namespaceFilter = metav1.NamespaceAll
	}

	fmt.Printf("[DEBUG] CronJobs - Listing for cluster: %s, namespace filter: %s\n", cluster, namespaceFilter)

	// Obter client do cluster
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		fmt.Printf("[DEBUG] CronJobs - Failed to get client for cluster %s: %v\n", cluster, err)
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KUBERNETES_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	// Listar CronJobs
	cronJobList, err := client.BatchV1().CronJobs(namespaceFilter).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[DEBUG] CronJobs - Error listing cronjobs with filter %s: %v\n", namespaceFilter, err)
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KUBERNETES_API_ERROR",
				"message": fmt.Sprintf("Failed to list CronJobs: %v", err),
			},
		})
		return
	}

	fmt.Printf("[DEBUG] CronJobs - Found %d cronjobs with namespace filter %s\n", len(cronJobList.Items), namespaceFilter)

	// Converter para resposta
	cronJobs := make([]CronJobResponse, 0)
	for _, cj := range cronJobList.Items {
		fmt.Printf("[DEBUG] CronJobs - Processing cronjob: %s\n", cj.Name)
		cronJob := convertCronJobToResponse(&cj)
		cronJobs = append(cronJobs, cronJob)
	}

	fmt.Printf("[DEBUG] CronJobs - Total cronjobs processed: %d\n", len(cronJobs))

	c.JSON(200, gin.H{
		"success": true,
		"data":    cronJobs,
		"count":   len(cronJobs),
	})
}

// Update atualiza o estado de suspend ou schedule de um CronJob
func (h *CronJobHandler) Update(c *gin.Context) {
	cluster := c.Param("cluster")
	namespace := c.Param("namespace")
	name := c.Param("name")

	var req struct {
		Suspend  *bool   `json:"suspend,omitempty"`
		Schedule *string `json:"schedule,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": fmt.Sprintf("Invalid request body: %v", err),
			},
		})
		return
	}

	// Validar que pelo menos um campo foi fornecido
	if req.Suspend == nil && req.Schedule == nil {
		c.JSON(400, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "At least one of 'suspend' or 'schedule' must be provided",
			},
		})
		return
	}

	// Obter client do cluster
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KUBERNETES_CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get Kubernetes client: %v", err),
			},
		})
		return
	}

	// Buscar CronJob atual
	cronJob, err := client.BatchV1().CronJobs(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		c.JSON(404, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CRONJOB_NOT_FOUND",
				"message": fmt.Sprintf("CronJob not found: %v", err),
			},
		})
		return
	}

	// Capturar estado antes da atualização
	before := map[string]interface{}{
		"schedule": cronJob.Spec.Schedule,
		"suspend":  cronJob.Spec.Suspend,
	}

	// Atualizar campos conforme fornecido
	if req.Suspend != nil {
		cronJob.Spec.Suspend = req.Suspend
	}
	if req.Schedule != nil {
		cronJob.Spec.Schedule = *req.Schedule
	}

	// Aplicar atualização
	start := time.Now()
	updatedCronJob, err := client.BatchV1().CronJobs(namespace).Update(context.Background(), cronJob, metav1.UpdateOptions{})
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "KUBERNETES_UPDATE_ERROR",
				"message": fmt.Sprintf("Failed to update CronJob: %v", err),
			},
		})
		return
	}

	if h.historyTracker != nil {
		after := map[string]interface{}{
			"schedule": updatedCronJob.Spec.Schedule,
			"suspend":  updatedCronJob.Spec.Suspend,
		}
		entry := history.HistoryEntry{
			Action:   "update_cronjob",
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

	c.JSON(200, gin.H{
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

	// LastScheduleTime
	if cj.Status.LastScheduleTime != nil {
		timeStr := cj.Status.LastScheduleTime.Format("2006-01-02 15:04:05")
		resp.LastScheduleTime = &timeStr
	}

	return resp
}

// describeCronSchedule converte cron expression para texto legível
func describeCronSchedule(schedule string) string {
	// Formato cron: "minute hour day month weekday"
	// Exemplos básicos (pode ser expandido)
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
		return schedule // Retornar expressão original se não reconhecida
	}
}

// getHistoryCount retorna o valor de um pointer int32 ou 0
func getHistoryCount(limit *int32) int32 {
	if limit == nil {
		return 0
	}
	return *limit
}
