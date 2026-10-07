package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"
)

// Exclusão de CronJob e de Job (aba CronJobs), com prévia e proteções:
//   - prévia: Jobs do CronJob (quantos rodando) e se ele é gerenciado por Helm/Argo CD/Flux
//     (seria recriado no próximo sync — o certo é remover pela origem);
//   - escolha explícita do que fazer com os Jobs (apagar junto ou manter órfãos);
//   - precondition de UID: só apaga o MESMO objeto mostrado na prévia (se foi recriado no meio
//     tempo, o delete é recusado);
//   - histórico com o manifesto completo, para dar para recriar.
// Job usa propagation Background explícito: na API batch/v1 o padrão de um Job é Orphan (os
// pods ficariam para trás) — o kubectl é que usa Background por padrão.

// CronJobJobInfo é um Job criado pelo CronJob.
type CronJobJobInfo struct {
	Name            string  `json:"name"`
	UID             string  `json:"uid"`
	Status          string  `json:"status"` // Running | Succeeded | Failed
	StartTime       *string `json:"start_time,omitempty"`
	CompletionTime  *string `json:"completion_time,omitempty"`
	DurationSeconds int64   `json:"duration_seconds"`
	ActivePods      int32   `json:"active_pods"`
	ManagedBy       string  `json:"managed_by,omitempty"`
}

// CronJobDeletePreview é o que a tela mostra antes de apagar um CronJob.
type CronJobDeletePreview struct {
	Name       string           `json:"name"`
	Namespace  string           `json:"namespace"`
	UID        string           `json:"uid"`
	Schedule   string           `json:"schedule"`
	Suspended  bool             `json:"suspended"`
	ManagedBy  string           `json:"managed_by,omitempty"` // Helm/Argo CD/Flux: será recriado pela origem
	Jobs       []CronJobJobInfo `json:"jobs"`
	ActiveJobs int              `json:"active_jobs"`
}

// batchManagedBy identifica ferramenta que recria o objeto se ele for apagado à mão.
func batchManagedBy(labels, annotations map[string]string) string {
	switch {
	case annotations["meta.helm.sh/release-name"] != "":
		return "Helm (release " + annotations["meta.helm.sh/release-name"] + ")"
	case labels["app.kubernetes.io/managed-by"] == "Helm":
		return "Helm"
	case labels["argocd.argoproj.io/instance"] != "" || annotations["argocd.argoproj.io/tracking-id"] != "":
		return "Argo CD"
	case labels["kustomize.toolkit.fluxcd.io/name"] != "" || labels["helm.toolkit.fluxcd.io/name"] != "":
		return "Flux"
	}
	return ""
}

func jobInfo(j *batchv1.Job, now time.Time) CronJobJobInfo {
	status, finished := cronJobJobStatus(j)
	info := CronJobJobInfo{Name: j.Name, UID: string(j.UID), Status: status, ActivePods: j.Status.Active, ManagedBy: batchManagedBy(j.Labels, j.Annotations)}
	start := j.CreationTimestamp.Time
	if j.Status.StartTime != nil {
		start = j.Status.StartTime.Time
	}
	info.StartTime = rfc3339(start)
	end := now
	if !finished.IsZero() {
		end = finished
		info.CompletionTime = rfc3339(finished)
	}
	if d := end.Sub(start); d > 0 {
		info.DurationSeconds = int64(d.Seconds())
	}
	return info
}

func buildCronJobDeletePreview(ctx context.Context, client kubernetes.Interface, ns, name string, now time.Time) (*CronJobDeletePreview, error) {
	cj, err := client.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	p := &CronJobDeletePreview{
		Name: cj.Name, Namespace: cj.Namespace, UID: string(cj.UID), Schedule: cj.Spec.Schedule,
		Suspended: cj.Spec.Suspend != nil && *cj.Spec.Suspend,
		ManagedBy: batchManagedBy(cj.Labels, cj.Annotations), Jobs: []CronJobJobInfo{},
	}
	jobs, err := client.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("falha ao listar os Jobs do CronJob: %w", err)
	}
	for i := range jobs.Items {
		j := &jobs.Items[i]
		for _, ref := range j.OwnerReferences {
			if ref.Kind == "CronJob" && ref.UID == cj.UID {
				info := jobInfo(j, now)
				if info.Status == "Running" {
					p.ActiveJobs++
				}
				p.Jobs = append(p.Jobs, info)
			}
		}
	}
	sort.Slice(p.Jobs, func(a, b int) bool { return *p.Jobs[a].StartTime > *p.Jobs[b].StartTime }) // mais recente primeiro
	return p, nil
}

// cronJobManifestYAML é o snapshot para o histórico (sem status/managedFields), pronto para reaplicar.
func cronJobManifestYAML(cj *batchv1.CronJob) string {
	c := cj.DeepCopy()
	c.TypeMeta = metav1.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"}
	c.ManagedFields, c.ResourceVersion, c.UID, c.Generation = nil, "", "", 0
	c.CreationTimestamp = metav1.Time{}
	// via mapa para tirar o "status: {}" (struct vazia não é omitida pela serialização)
	js, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(js, &m); err != nil {
		return ""
	}
	delete(m, "status")
	b, err := sigsyaml.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// deleteCronJobWithUID apaga o CronJob só se o UID ainda for o da prévia. keepJobs=true mantém os
// Jobs (Orphan); senão Jobs e pods vão junto (Background). Devolve o manifesto apagado.
func deleteCronJobWithUID(ctx context.Context, client kubernetes.Interface, ns, name, uid string, keepJobs bool) (string, error) {
	cj, err := client.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if string(cj.UID) != uid {
		return "", errObjectReplaced
	}
	policy := metav1.DeletePropagationBackground
	if keepJobs {
		policy = metav1.DeletePropagationOrphan
	}
	u := types.UID(uid)
	err = client.BatchV1().CronJobs(ns).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &u}})
	return cronJobManifestYAML(cj), err
}

// deleteJobWithUID apaga o Job (e os pods dele) só se o UID ainda for o informado.
func deleteJobWithUID(ctx context.Context, client kubernetes.Interface, ns, name, uid string) (*CronJobJobInfo, error) {
	j, err := client.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(j.UID) != uid {
		return nil, errObjectReplaced
	}
	info := jobInfo(j, time.Now())
	policy := metav1.DeletePropagationBackground // padrão da API para Job é Orphan: pods ficariam para trás
	u := types.UID(uid)
	return &info, client.BatchV1().Jobs(ns).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &u}})
}

var errObjectReplaced = fmt.Errorf("o objeto foi recriado ou substituído depois da prévia (UID diferente) — revise e confirme de novo")

func writeBatchDeleteError(c *gin.Context, what string, err error) {
	if checkForbidden(c, err) {
		return
	}
	switch {
	case err == errObjectReplaced:
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case apierrors.IsNotFound(err):
		c.JSON(http.StatusNotFound, gin.H{"error": what + " não existe mais (já foi apagado?)"})
	case apierrors.IsConflict(err):
		c.JSON(http.StatusConflict, gin.H{"error": "o objeto mudou depois da prévia — revise e confirme de novo: " + err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao apagar %s: %v", what, err)})
	}
}

// CronJobDeletePreview — GET /api/v1/cronjobs/:cluster/:namespace/:name/delete-preview
// (também usado para listar os Jobs do CronJob na tela).
func (h *CronJobHandler) CronJobDeletePreview(c *gin.Context) {
	client, err := h.kubeManager.GetClient(c.Param("cluster"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}
	p, err := buildCronJobDeletePreview(c.Request.Context(), client, c.Param("namespace"), c.Param("name"), time.Now())
	if err != nil {
		writeBatchDeleteError(c, "CronJob "+c.Param("name"), err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// DeleteCronJob — DELETE /api/v1/cronjobs/:cluster/:namespace/:name?uid=<uid da prévia>&jobs=delete|keep
func (h *CronJobHandler) DeleteCronJob(c *gin.Context) {
	cluster, ns, name, uid := c.Param("cluster"), c.Param("namespace"), c.Param("name"), c.Query("uid")
	jobs := c.Query("jobs")
	if uid == "" || (jobs != "delete" && jobs != "keep") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe uid (da prévia) e jobs=delete|keep"})
		return
	}
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}
	start := time.Now()
	manifest, err := deleteCronJobWithUID(c.Request.Context(), client, ns, name, uid, jobs == "keep")
	h.recordBatchDelete(c, "delete_cronjob", ns+"/"+name, cluster, manifest, map[string]interface{}{"jobs": jobs}, start, err)
	if err != nil {
		writeBatchDeleteError(c, "CronJob "+name, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("CronJob %s/%s apagado", ns, name), "jobs": jobs})
}

// DeleteJob — DELETE /api/v1/jobs/:cluster/:namespace/:name?uid=<uid>
func (h *CronJobHandler) DeleteJob(c *gin.Context) {
	cluster, ns, name, uid := c.Param("cluster"), c.Param("namespace"), c.Param("name"), c.Query("uid")
	if uid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe o uid do Job (da listagem)"})
		return
	}
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("falha ao conectar no cluster: %v", err)})
		return
	}
	start := time.Now()
	info, err := deleteJobWithUID(c.Request.Context(), client, ns, name, uid)
	var before map[string]interface{}
	if info != nil {
		before = map[string]interface{}{"status": info.Status, "active_pods": info.ActivePods, "start_time": info.StartTime}
	}
	h.recordBatchDeleteMap(c, "delete_job", ns+"/"+name, cluster, before, start, err)
	if err != nil {
		writeBatchDeleteError(c, "Job "+name, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("Job %s/%s apagado (pods junto)", ns, name)})
}

func (h *CronJobHandler) recordBatchDelete(c *gin.Context, action, resource, cluster, manifest string, after map[string]interface{}, start time.Time, err error) {
	var before map[string]interface{}
	if manifest != "" {
		before = map[string]interface{}{"yaml": manifest}
	}
	h.recordBatchDeleteMap(c, action, resource, cluster, before, start, err, after)
}

func (h *CronJobHandler) recordBatchDeleteMap(c *gin.Context, action, resource, cluster string, before map[string]interface{}, start time.Time, err error, after ...map[string]interface{}) {
	if h.historyTracker == nil || err == errObjectReplaced || apierrors.IsNotFound(err) {
		return // nada foi tentado de fato
	}
	status, errMsg := "success", ""
	if err != nil {
		status, errMsg = "failed", err.Error()
	}
	var a map[string]interface{}
	if len(after) > 0 {
		a = after[0]
	}
	entry := CreateHistoryEntry(c, action, resource, cluster, status, before, a, time.Since(start).Milliseconds(), errMsg)
	if logErr := h.historyTracker.Log(entry); logErr != nil {
		fmt.Printf("warning: failed to record history entry: %v\n", logErr)
	}
}
