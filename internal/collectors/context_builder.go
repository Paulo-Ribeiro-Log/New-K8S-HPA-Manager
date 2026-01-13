package collectors

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/kubernetes"
	promclient "k8s-hpa-manager/internal/monitoring/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
)

// ContextBuilder orquestra a coleta de contexto de diagnóstico
type ContextBuilder struct {
	kubeManager *kubernetes.KubeManager
}

// NewContextBuilder cria um novo ContextBuilder
func NewContextBuilder(kubeManager *kubernetes.KubeManager) *ContextBuilder {
	return &ContextBuilder{
		kubeManager: kubeManager,
	}
}

// BuildContext constrói contexto completo de diagnóstico
func (cb *ContextBuilder) BuildContext(ctx context.Context, req *ContextRequest) (*DiagnosticContext, error) {
	// Obter clientset do cluster
	clientset, err := cb.kubeManager.GetClient(req.Cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	// Criar contexto de diagnóstico
	diagCtx := &DiagnosticContext{
		ResourceType: req.ResourceType,
		Cluster:      req.Cluster,
		Namespace:    req.Namespace,
		ResourceName: req.ResourceName,
		CollectedAt:  time.Now(),
	}

	// Coletar contexto específico do tipo de recurso
	switch req.ResourceType {
	case "Pod":
		podCtx, err := cb.collectPodContext(ctx, clientset, req)
		if err != nil {
			return nil, err
		}
		diagCtx.Pod = podCtx

		// 🔍 INVESTIGAÇÃO AUTOMÁTICA DE POD: Validar recursos referenciados
		fmt.Println("🔍 [SMART_INVESTIGATOR] Iniciando investigação de recursos do Pod...")
		investigation := cb.investigatePodResources(ctx, clientset, podCtx)
		if investigation != nil && (len(investigation.MissingResources) > 0 || len(investigation.FoundAlternatives) > 0 || len(investigation.Validations) > 0) {
			diagCtx.Investigation = investigation
			fmt.Printf("✅ [SMART_INVESTIGATOR] Investigação concluída:\n")
			fmt.Printf("   - Recursos faltantes: %d\n", len(investigation.MissingResources))
			fmt.Printf("   - Recursos alternativos: %d\n", len(investigation.FoundAlternatives))
			fmt.Printf("   - Validações: %d\n", len(investigation.Validations))
		} else {
			fmt.Println("✅ [SMART_INVESTIGATOR] Todos os recursos validados com sucesso")
		}

	case "Deployment":
		deployCtx, err := cb.collectDeploymentContext(ctx, clientset, req)
		if err != nil {
			return nil, err
		}
		diagCtx.Deployment = deployCtx

	case "HPA":
		hpaCtx, err := cb.collectHPAContext(ctx, clientset, req)
		if err != nil {
			return nil, err
		}
		diagCtx.HPA = hpaCtx

	case "Node":
		nodeCtx, err := cb.collectNodeContext(ctx, clientset, req)
		if err != nil {
			return nil, err
		}
		diagCtx.Node = nodeCtx

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", req.ResourceType)
	}

	// Coletar eventos Kubernetes
	events, err := cb.collectEvents(ctx, clientset, req)
	if err == nil {
		diagCtx.Events = events
	}

	// kubectl describe (se habilitado)
	if req.IncludeDescribe {
		describeOutput, err := cb.kubeManager.ExecuteKubectlDescribe(req.Cluster, req.Namespace, req.ResourceType, req.ResourceName)
		if err == nil {
			diagCtx.DescribeOutput = describeOutput
		}
	}

	// Coletar métricas do Prometheus (se habilitado e for Pod)
	if req.IncludeMetrics && req.ResourceType == "Pod" {
		prometheusMetrics, err := cb.collectPrometheusMetrics(ctx, req.Cluster, req.Namespace, req.ResourceName)
		if err != nil {
			// Log warning mas não falha a análise se Prometheus não disponível
			fmt.Printf("⚠️  [PROMETHEUS] Falha ao coletar métricas: %v\n", err)
		} else {
			diagCtx.PrometheusMetrics = prometheusMetrics
			fmt.Printf("✅ [PROMETHEUS] Métricas coletadas: CPU=%.2fm, Memory=%.2fMB\n",
				prometheusMetrics.CPUUsageCurrent, prometheusMetrics.MemoryUsageCurrent)
		}
	}

	return diagCtx, nil
}

// collectPodContext coleta contexto de um Pod
func (cb *ContextBuilder) collectPodContext(ctx context.Context, clientset k8sclient.Interface, req *ContextRequest) (*PodContext, error) {
	collector := NewPodCollector(clientset)
	return collector.Collect(ctx, req.Namespace, req.ResourceName, req)
}

// collectDeploymentContext coleta contexto de um Deployment
func (cb *ContextBuilder) collectDeploymentContext(ctx context.Context, clientset k8sclient.Interface, req *ContextRequest) (*DeploymentContext, error) {
	collector := NewDeploymentCollector(clientset)
	return collector.Collect(ctx, req.Namespace, req.ResourceName, req)
}

// collectHPAContext coleta contexto de um HPA
func (cb *ContextBuilder) collectHPAContext(ctx context.Context, clientset k8sclient.Interface, req *ContextRequest) (*HPAContext, error) {
	collector := NewHPACollector(clientset)
	return collector.Collect(ctx, req.Namespace, req.ResourceName, req)
}

// collectNodeContext coleta contexto de um Node
func (cb *ContextBuilder) collectNodeContext(ctx context.Context, clientset k8sclient.Interface, req *ContextRequest) (*NodeContext, error) {
	collector := NewNodeCollector(clientset)
	// Nodes não têm namespace
	return collector.Collect(ctx, req.ResourceName, req)
}

// collectEvents coleta eventos Kubernetes relacionados ao recurso
func (cb *ContextBuilder) collectEvents(ctx context.Context, clientset k8sclient.Interface, req *ContextRequest) ([]corev1.Event, error) {
	// Listar eventos do namespace (últimos 50)
	eventList, err := clientset.CoreV1().Events(req.Namespace).List(ctx, metav1.ListOptions{
		Limit: 50,
	})
	if err != nil {
		return nil, err
	}

	// Filtrar eventos relacionados ao recurso
	var relatedEvents []corev1.Event
	for _, event := range eventList.Items {
		if event.InvolvedObject.Name == req.ResourceName {
			relatedEvents = append(relatedEvents, event)
		}
	}

	return relatedEvents, nil
}

// investigatePodResources executa investigação inteligente de recursos do Pod
func (cb *ContextBuilder) investigatePodResources(ctx context.Context, clientset k8sclient.Interface, podCtx *PodContext) *InvestigationResult {
	if podCtx == nil || podCtx.Manifest == nil {
		return nil
	}

	collector := NewPodCollector(clientset)
	return collector.InvestigateResources(ctx, podCtx.Manifest, podCtx)
}

// collectPrometheusMetrics coleta métricas do Prometheus para um Pod
func (cb *ContextBuilder) collectPrometheusMetrics(ctx context.Context, cluster, namespace, podName string) (*PrometheusMetrics, error) {
	// Criar prometheus client
	promClient, err := promclient.NewPrometheusClient(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}
	defer promClient.Close()

	metrics := &PrometheusMetrics{
		CollectedAt: time.Now(),
	}

	// CPU atual (média 5 min) em millicores
	cpuQuery := fmt.Sprintf(
		`avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod="%s",container!="POD",container!=""}[5m])) * 1000`,
		namespace, podName,
	)
	if cpuUsage, err := promClient.QueryScalar(ctx, cpuQuery); err == nil {
		metrics.CPUUsageCurrent = cpuUsage
	}

	// Memory atual em MB
	memQuery := fmt.Sprintf(
		`avg(container_memory_working_set_bytes{namespace="%s",pod="%s",container!="POD",container!=""}) / (1024 * 1024)`,
		namespace, podName,
	)
	if memUsage, err := promClient.QueryScalar(ctx, memQuery); err == nil {
		metrics.MemoryUsageCurrent = memUsage
	}

	// CPU Request em millicores
	cpuReqQuery := fmt.Sprintf(
		`sum(kube_pod_container_resource_requests{namespace="%s",pod="%s",resource="cpu"}) * 1000`,
		namespace, podName,
	)
	if cpuReq, err := promClient.QueryScalar(ctx, cpuReqQuery); err == nil {
		metrics.CPURequest = cpuReq
	}

	// CPU Limit em millicores
	cpuLimitQuery := fmt.Sprintf(
		`sum(kube_pod_container_resource_limits{namespace="%s",pod="%s",resource="cpu"}) * 1000`,
		namespace, podName,
	)
	if cpuLimit, err := promClient.QueryScalar(ctx, cpuLimitQuery); err == nil {
		metrics.CPULimit = cpuLimit
	}

	// Memory Request em MB
	memReqQuery := fmt.Sprintf(
		`sum(kube_pod_container_resource_requests{namespace="%s",pod="%s",resource="memory"}) / (1024 * 1024)`,
		namespace, podName,
	)
	if memReq, err := promClient.QueryScalar(ctx, memReqQuery); err == nil {
		metrics.MemoryRequest = memReq
	}

	// Memory Limit em MB
	memLimitQuery := fmt.Sprintf(
		`sum(kube_pod_container_resource_limits{namespace="%s",pod="%s",resource="memory"}) / (1024 * 1024)`,
		namespace, podName,
	)
	if memLimit, err := promClient.QueryScalar(ctx, memLimitQuery); err == nil {
		metrics.MemoryLimit = memLimit
	}

	// Restart count
	restartQuery := fmt.Sprintf(
		`sum(kube_pod_container_status_restarts_total{namespace="%s",pod="%s"})`,
		namespace, podName,
	)
	if restarts, err := promClient.QueryScalar(ctx, restartQuery); err == nil {
		metrics.RestartCount = int32(restarts)
	}

	return metrics, nil
}
