package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/models"
)

// Usage retorna, para os ConfigMaps do namespace informado (ou de todos, se omitido),
// se são órfãos (não referenciados por nenhum Pod do cluster) e por quais workloads
// são usados — cross-reference via volumes/envFrom/env, resolvendo o Pod até seu
// workload dono (Deployment/DaemonSet/StatefulSet/Job) via OwnerReferences.
func (h *ConfigMapHandler) Usage(c *gin.Context) {
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
	namespace := strings.TrimSpace(c.Query("namespace"))

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

	ctx := c.Request.Context()

	configMaps, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
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

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		// Degradação graciosa: sem Pods não dá pra cross-referenciar, mas ainda
		// retornamos a lista de ConfigMaps (todos marcados como órfãos por ora).
		pods = &corev1.PodList{}
	}

	usageMap := buildConfigMapUsageMap(pods.Items)

	result := make([]models.ConfigMapUsage, 0, len(configMaps.Items))
	for _, cm := range configMaps.Items {
		owners := usageMap[cm.Namespace+"/"+cm.Name]
		if owners == nil {
			owners = []string{}
		}
		result = append(result, models.ConfigMapUsage{
			Namespace: cm.Namespace,
			Name:      cm.Name,
			IsOrphan:  len(owners) == 0,
			UsedBy:    owners,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// buildConfigMapUsageMap mapeia "namespace/configmap" -> lista ordenada de workloads
// que o referenciam (via volumes, volumes projetados, envFrom e env.valueFrom).
func buildConfigMapUsageMap(pods []corev1.Pod) map[string][]string {
	refs := make(map[string]map[string]bool)
	add := func(namespace, cmName, owner string) {
		key := namespace + "/" + cmName
		if refs[key] == nil {
			refs[key] = make(map[string]bool)
		}
		refs[key][owner] = true
	}

	for i := range pods {
		pod := &pods[i]
		owner := resolveOwnerDisplayName(pod)

		for _, vol := range pod.Spec.Volumes {
			if vol.ConfigMap != nil {
				add(pod.Namespace, vol.ConfigMap.Name, owner)
			}
			if vol.Projected != nil {
				for _, src := range vol.Projected.Sources {
					if src.ConfigMap != nil {
						add(pod.Namespace, src.ConfigMap.Name, owner)
					}
				}
			}
		}

		scanContainers := func(containers []corev1.Container) {
			for _, ct := range containers {
				for _, ef := range ct.EnvFrom {
					if ef.ConfigMapRef != nil {
						add(pod.Namespace, ef.ConfigMapRef.Name, owner)
					}
				}
				for _, env := range ct.Env {
					if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
						add(pod.Namespace, env.ValueFrom.ConfigMapKeyRef.Name, owner)
					}
				}
			}
		}
		scanContainers(pod.Spec.InitContainers)
		scanContainers(pod.Spec.Containers)
	}

	result := make(map[string][]string, len(refs))
	for key, set := range refs {
		list := make([]string, 0, len(set))
		for name := range set {
			list = append(list, name)
		}
		sort.Strings(list)
		result[key] = list
	}
	return result
}

// resolveOwnerDisplayName resolve o Pod até o nome de exibição do workload dono
// (Deployment/DaemonSet/StatefulSet/Job), imitando o "jump to owner" do k9s.
func resolveOwnerDisplayName(pod *corev1.Pod) string {
	for _, ref := range pod.OwnerReferences {
		switch ref.Kind {
		case "ReplicaSet":
			if name := stripReplicaSetHash(ref.Name); name != "" {
				return "Deployment/" + name
			}
			return "ReplicaSet/" + ref.Name
		case "DaemonSet":
			return "DaemonSet/" + ref.Name
		case "StatefulSet":
			return "StatefulSet/" + ref.Name
		case "Job":
			return "Job/" + ref.Name
		}
	}
	return "Pod/" + pod.Name
}

// stripReplicaSetHash remove o sufixo de hash do nome do ReplicaSet (convenção padrão
// do controller de Deployment) para obter o nome do Deployment. Retorna "" se o sufixo
// não parecer um hash (evita falso positivo em nomes com hífen legítimo).
func stripReplicaSetHash(rsName string) string {
	idx := strings.LastIndex(rsName, "-")
	if idx <= 0 {
		return ""
	}
	suffix := rsName[idx+1:]
	if len(suffix) < 6 || len(suffix) > 10 {
		return ""
	}
	for _, r := range suffix {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return rsName[:idx]
}
