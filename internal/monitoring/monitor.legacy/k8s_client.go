package monitor

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient wrapper para client-go com contexto do cluster
type K8sClient struct {
	Clientset *kubernetes.Clientset // Exportado para uso em outros packages
	config    *rest.Config
	cluster   *models.ClusterInfo
}

// NewK8sClient cria um novo client para um cluster específico
func NewK8sClient(cluster *models.ClusterInfo) (*K8sClient, error) {
	// Carrega kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: cluster.Context,
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	// Cria rest.Config
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create client config for cluster %s: %w", cluster.Name, err)
	}

	// Timeout padrão para requests
	config.Timeout = 30 * time.Second

	// Cria clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset for cluster %s: %w", cluster.Name, err)
	}

	log.Info().
		Str("cluster", cluster.Name).
		Str("context", cluster.Context).
		Str("server", cluster.Server).
		Msg("K8s client created successfully")

	return &K8sClient{
		Clientset: clientset,
		config:    config,
		cluster:   cluster,
	}, nil
}

// ListNamespaces lista todos os namespaces (exceto system namespaces)
func (k *K8sClient) ListNamespaces(ctx context.Context, excludePatterns []string) ([]string, error) {
	namespaces, err := k.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Default patterns to exclude
	defaultExclude := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
	}

	exclude := append(defaultExclude, excludePatterns...)
	result := []string{}

	for _, ns := range namespaces.Items {
		if !contains(exclude, ns.Name) {
			result = append(result, ns.Name)
		}
	}

	log.Debug().
		Str("cluster", k.cluster.Name).
		Int("total", len(namespaces.Items)).
		Int("filtered", len(result)).
		Msg("Namespaces listed")

	return result, nil
}

// ListHPAs lista todos os HPAs em um namespace
func (k *K8sClient) ListHPAs(ctx context.Context, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
	hpaList, err := k.Clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list HPAs in namespace %s: %w", namespace, err)
	}

	log.Debug().
		Str("cluster", k.cluster.Name).
		Str("namespace", namespace).
		Int("count", len(hpaList.Items)).
		Msg("HPAs listed")

	return hpaList.Items, nil
}

// GetHPA obtém um HPA específico
func (k *K8sClient) GetHPA(ctx context.Context, namespace, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	hpa, err := k.Clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get HPA %s/%s: %w", namespace, name, err)
	}

	return hpa, nil
}

// GetDeployment obtém deployment associado ao HPA
func (k *K8sClient) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	deployment, err := k.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	return deployment, nil
}

// CollectHPASnapshot coleta um snapshot completo de um HPA
func (k *K8sClient) CollectHPASnapshot(ctx context.Context, hpa *autoscalingv2.HorizontalPodAutoscaler) (*models.HPASnapshot, error) {
	snapshot := &models.HPASnapshot{
		Timestamp: time.Now(),
		Cluster:   k.cluster.Name,
		Namespace: hpa.Namespace,
		Name:      hpa.Name,
	}

	// HPA Config
	snapshot.MinReplicas = *hpa.Spec.MinReplicas
	snapshot.MaxReplicas = hpa.Spec.MaxReplicas
	snapshot.CurrentReplicas = hpa.Status.CurrentReplicas
	snapshot.DesiredReplicas = hpa.Status.DesiredReplicas

	// Targets (CPU/Memory)
	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType {
			if metric.Resource.Name == corev1.ResourceCPU && metric.Resource.Target.AverageUtilization != nil {
				snapshot.CPUTarget = *metric.Resource.Target.AverageUtilization
			}
			if metric.Resource.Name == corev1.ResourceMemory && metric.Resource.Target.AverageUtilization != nil {
				snapshot.MemoryTarget = *metric.Resource.Target.AverageUtilization
			}
		}
	}

	// Status
	for _, condition := range hpa.Status.Conditions {
		if condition.Type == autoscalingv2.ScalingActive {
			snapshot.ScalingActive = condition.Status == corev1.ConditionTrue
		}
		if condition.Type == autoscalingv2.AbleToScale {
			snapshot.Ready = condition.Status == corev1.ConditionTrue
		}
	}

	if hpa.Status.LastScaleTime != nil {
		scaleTime := hpa.Status.LastScaleTime.Time
		snapshot.LastScaleTime = &scaleTime
	}

	// Tenta obter resources do deployment/target
	if k.Clientset != nil && hpa.Spec.ScaleTargetRef.Kind == "Deployment" {
		deployment, err := k.GetDeployment(ctx, hpa.Namespace, hpa.Spec.ScaleTargetRef.Name)
		if err != nil {
			log.Warn().
				Err(err).
				Str("cluster", k.cluster.Name).
				Str("namespace", hpa.Namespace).
				Str("hpa", hpa.Name).
				Str("deployment", hpa.Spec.ScaleTargetRef.Name).
				Msg("Failed to get deployment for HPA")
		} else {
			// Extrai resources do primeiro container
			if len(deployment.Spec.Template.Spec.Containers) > 0 {
				container := deployment.Spec.Template.Spec.Containers[0]
				if container.Resources.Requests != nil {
					if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
						snapshot.CPURequest = cpu.String()
					}
					if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
						snapshot.MemoryRequest = mem.String()
					}
				}
				if container.Resources.Limits != nil {
					if cpu, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
						snapshot.CPULimit = cpu.String()
					}
					if mem, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
						snapshot.MemoryLimit = mem.String()
					}
				}
			}
		}
	}

	// Por enquanto, DataSource é MetricsServer (Prometheus virá depois)
	snapshot.DataSource = models.DataSourceMetricsServer

	log.Debug().
		Str("cluster", k.cluster.Name).
		Str("namespace", hpa.Namespace).
		Str("hpa", hpa.Name).
		Int32("current_replicas", snapshot.CurrentReplicas).
		Int32("desired_replicas", snapshot.DesiredReplicas).
		Msg("HPA snapshot collected")

	return snapshot, nil
}

// TestConnection testa a conexão com o cluster
func (k *K8sClient) TestConnection(ctx context.Context) error {
	_, err := k.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("connection test failed for cluster %s: %w", k.cluster.Name, err)
	}

	log.Info().
		Str("cluster", k.cluster.Name).
		Msg("Connection test successful")

	return nil
}

// GetClusterInfo retorna informações do cluster
func (k *K8sClient) GetClusterInfo() *models.ClusterInfo {
	return k.cluster
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
