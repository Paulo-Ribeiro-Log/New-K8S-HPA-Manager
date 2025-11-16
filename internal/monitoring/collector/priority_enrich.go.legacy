package collector

import (
	"context"

	"github.com/rs/zerolog/log"
	"k8s-hpa-manager/internal/monitoring/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// enrichSnapshotWithK8sData enriquece snapshot com dados do K8s API
// Preenche: MinReplicas, MaxReplicas, CurrentReplicas, DesiredReplicas,
// CPUTarget, MemoryTarget, CPURequest, CPULimit, MemoryRequest, MemoryLimit
func (c *PriorityCollector) enrichSnapshotWithK8sData(
	ctx context.Context,
	clientset kubernetes.Interface,
	snapshot *models.HPASnapshot,
) {
	// 1. Busca HPA do K8s API
	hpa, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(snapshot.Namespace).Get(
		ctx, snapshot.Name, metav1.GetOptions{},
	)
	if err != nil {
		log.Debug().
			Err(err).
			Str("cluster", snapshot.Cluster).
			Str("namespace", snapshot.Namespace).
			Str("hpa", snapshot.Name).
			Msg("Failed to get HPA from K8s API")
		return
	}

	// 2. Extrai config do HPA
	if hpa.Spec.MinReplicas != nil {
		snapshot.MinReplicas = *hpa.Spec.MinReplicas
	}
	snapshot.MaxReplicas = hpa.Spec.MaxReplicas
	snapshot.CurrentReplicas = hpa.Status.CurrentReplicas
	snapshot.DesiredReplicas = hpa.Status.DesiredReplicas

	// 3. Extrai targets (CPU/Memory)
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == "Resource" && metric.Resource != nil {
			if metric.Resource.Name == corev1.ResourceCPU && metric.Resource.Target.AverageUtilization != nil {
				snapshot.CPUTarget = *metric.Resource.Target.AverageUtilization
			}
			if metric.Resource.Name == corev1.ResourceMemory && metric.Resource.Target.AverageUtilization != nil {
				snapshot.MemoryTarget = *metric.Resource.Target.AverageUtilization
			}
		}
	}

	// 4. Busca Deployment/StatefulSet/DaemonSet para obter resources
	if hpa.Spec.ScaleTargetRef.Kind == "" {
		return // Sem target definido
	}

	targetKind := hpa.Spec.ScaleTargetRef.Kind
	targetName := hpa.Spec.ScaleTargetRef.Name

	var containers []corev1.Container

	switch targetKind {
	case "Deployment":
		deployment, err := clientset.AppsV1().Deployments(snapshot.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err != nil {
			log.Debug().
				Err(err).
				Str("cluster", snapshot.Cluster).
				Str("namespace", snapshot.Namespace).
				Str("deployment", targetName).
				Msg("Failed to get Deployment")
			return
		}
		containers = deployment.Spec.Template.Spec.Containers

	case "StatefulSet":
		statefulset, err := clientset.AppsV1().StatefulSets(snapshot.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err != nil {
			log.Debug().
				Err(err).
				Str("cluster", snapshot.Cluster).
				Str("namespace", snapshot.Namespace).
				Str("statefulset", targetName).
				Msg("Failed to get StatefulSet")
			return
		}
		containers = statefulset.Spec.Template.Spec.Containers

	case "DaemonSet":
		daemonset, err := clientset.AppsV1().DaemonSets(snapshot.Namespace).Get(ctx, targetName, metav1.GetOptions{})
		if err != nil {
			log.Debug().
				Err(err).
				Str("cluster", snapshot.Cluster).
				Str("namespace", snapshot.Namespace).
				Str("daemonset", targetName).
				Msg("Failed to get DaemonSet")
			return
		}
		containers = daemonset.Spec.Template.Spec.Containers

	default:
		log.Debug().
			Str("cluster", snapshot.Cluster).
			Str("namespace", snapshot.Namespace).
			Str("hpa", snapshot.Name).
			Str("target_kind", targetKind).
			Msg("Unsupported target kind for resource extraction")
		return
	}

	// 5. Extrai resources do primeiro container (padrão comum)
	if len(containers) > 0 {
		container := containers[0]

		// Requests
		if container.Resources.Requests != nil {
			if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				snapshot.CPURequest = cpu.String()
			}
			if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				snapshot.MemoryRequest = mem.String()
			}
		}

		// Limits
		if container.Resources.Limits != nil {
			if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
				snapshot.CPULimit = cpu.String()
			}
			if mem, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
				snapshot.MemoryLimit = mem.String()
			}
		}
	}

	log.Debug().
		Str("cluster", snapshot.Cluster).
		Str("namespace", snapshot.Namespace).
		Str("hpa", snapshot.Name).
		Str("cpu_request", snapshot.CPURequest).
		Str("cpu_limit", snapshot.CPULimit).
		Str("memory_request", snapshot.MemoryRequest).
		Str("memory_limit", snapshot.MemoryLimit).
		Msg("Snapshot enriched with K8s data")
}
