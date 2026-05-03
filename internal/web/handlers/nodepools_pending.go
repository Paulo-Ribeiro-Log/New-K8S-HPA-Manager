package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	dtclient "k8s-hpa-manager/internal/dynatrace"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PendingWorkload descreve um workload K8s com pods não prontos.
type PendingWorkload struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Kind      string `json:"kind"`
	Running   int    `json:"running"`
	NotReady  int    `json:"not_ready"`  // pods not ready (DT) ou pending (K8s)
	OldestAge string `json:"oldest_age"` // ex: "14m", "2h3m"
	Reason    string `json:"reason"`     // motivo FailedScheduling (somente K8s)
	Source    string `json:"source"`     // "dynatrace" | "k8s"
}

// GetPendingWorkloads retorna workloads com pods não prontos/pendentes para o cluster.
// Tenta Dynatrace primeiro; faz fallback para K8s API se DT não retornar dados.
func (h *NodePoolHandler) GetPendingWorkloads(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	aiEmail := strings.TrimSpace(c.Query("ai_email"))
	if cluster == "" {
		c.JSON(400, gin.H{"error": "cluster obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ── Tentativa 1: Dynatrace ─────────────────────────────────────────────
	if dtResult, err := h.queryDTPending(ctx, cluster, aiEmail); err == nil && len(dtResult) > 0 {
		c.JSON(200, gin.H{
			"workloads": dtResult,
			"source":    "dynatrace",
			"total":     len(dtResult),
		})
		return
	}

	// ── Tentativa 2: K8s API (fallback) ───────────────────────────────────
	result, err := h.queryK8sPending(ctx, cluster)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("erro ao listar pods pendentes: %v", err)})
		return
	}

	c.JSON(200, gin.H{
		"workloads": result,
		"source":    "k8s",
		"total":     len(result),
	})
}

// queryDTPending consulta workloads com pods não prontos via Dynatrace.
func (h *NodePoolHandler) queryDTPending(ctx context.Context, cluster, aiEmail string) ([]PendingWorkload, error) {
	var dtURL, dtToken string
	if aiEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(aiEmail)
		if err == nil && tokens != nil {
			dtURL = tokens.DynatraceURL
			dtToken = tokens.DynatraceToken
		}
	}

	dt, err := dtclient.NewClient(dtURL, dtToken)
	if err != nil {
		return nil, err
	}

	dtWorkloads, err := dt.GetPendingWorkloads(ctx, cluster)
	if err != nil {
		return nil, err
	}

	result := make([]PendingWorkload, 0, len(dtWorkloads))
	for _, w := range dtWorkloads {
		result = append(result, PendingWorkload{
			Namespace: w.Namespace,
			Workload:  w.Workload,
			Kind:      w.Kind,
			Running:   w.Running,
			NotReady:  w.NotReady,
			Source:    "dynatrace",
		})
	}
	return result, nil
}

// queryK8sPending lista pods em fase Pending e agrupa por owner.
func (h *NodePoolHandler) queryK8sPending(ctx context.Context, cluster string) ([]PendingWorkload, error) {
	client, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("cluster inacessível: %w", err)
	}

	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Pending",
	})
	if err != nil {
		return nil, fmt.Errorf("erro ao listar pods: %w", err)
	}

	type workloadKey struct{ ns, name, kind string }
	type workloadInfo struct {
		pendingCount int
		oldestTime   time.Time
	}
	byWorkload := map[workloadKey]*workloadInfo{}

	for _, pod := range pods.Items {
		ownerName, ownerKind := pod.Name, "Pod"
		for _, ref := range pod.OwnerReferences {
			if ref.Kind == "ReplicaSet" {
				rs, rsErr := client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
				if rsErr == nil {
					for _, rsOwner := range rs.OwnerReferences {
						if rsOwner.Kind == "Deployment" {
							ownerName = rsOwner.Name
							ownerKind = "Deployment"
							break
						}
					}
				}
				if ownerKind != "Deployment" {
					ownerName = ref.Name
					ownerKind = "ReplicaSet"
				}
			} else if ref.Kind != "" {
				ownerName = ref.Name
				ownerKind = ref.Kind
			}
		}

		key := workloadKey{ns: pod.Namespace, name: ownerName, kind: ownerKind}
		info := byWorkload[key]
		if info == nil {
			info = &workloadInfo{}
			byWorkload[key] = info
		}
		info.pendingCount++
		podAge := pod.CreationTimestamp.Time
		if info.oldestTime.IsZero() || podAge.Before(info.oldestTime) {
			info.oldestTime = podAge
		}
	}

	// Eventos FailedScheduling para motivo do scheduling failure
	schedulingMsgs := map[workloadKey]string{}
	events, evtErr := client.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: "reason=FailedScheduling",
	})
	if evtErr == nil && events != nil {
		for _, evt := range events.Items {
			if evt.InvolvedObject.Kind != "Pod" {
				continue
			}
			pod, podErr := client.CoreV1().Pods(evt.InvolvedObject.Namespace).Get(
				ctx, evt.InvolvedObject.Name, metav1.GetOptions{},
			)
			if podErr != nil {
				continue
			}
			ownerName, ownerKind := pod.Name, "Pod"
			for _, ref := range pod.OwnerReferences {
				if ref.Kind == "ReplicaSet" {
					rs, rsErr := client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
					if rsErr == nil {
						for _, rsOwner := range rs.OwnerReferences {
							if rsOwner.Kind == "Deployment" {
								ownerName = rsOwner.Name
								ownerKind = "Deployment"
								break
							}
						}
					}
					if ownerKind != "Deployment" {
						ownerName = ref.Name
						ownerKind = "ReplicaSet"
					}
				} else if ref.Kind != "" {
					ownerName = ref.Name
					ownerKind = ref.Kind
				}
			}
			key := workloadKey{ns: evt.InvolvedObject.Namespace, name: ownerName, kind: ownerKind}
			if _, exists := schedulingMsgs[key]; !exists {
				msg := evt.Message
				if len(msg) > 200 {
					msg = msg[:200] + "..."
				}
				schedulingMsgs[key] = msg
			}
		}
	}

	now := time.Now()
	result := make([]PendingWorkload, 0, len(byWorkload))
	for key, info := range byWorkload {
		age := ""
		if !info.oldestTime.IsZero() {
			age = formatAge(now.Sub(info.oldestTime))
		}
		result = append(result, PendingWorkload{
			Namespace: key.ns,
			Workload:  key.name,
			Kind:      key.kind,
			NotReady:  info.pendingCount,
			OldestAge: age,
			Reason:    schedulingMsgs[key],
			Source:    "k8s",
		})
	}
	return result, nil
}
