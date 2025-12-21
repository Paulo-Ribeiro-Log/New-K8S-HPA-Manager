package collectors

import (
	"context"
	"fmt"
	"time"

	"k8s-hpa-manager/internal/kubernetes"

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

	// TODO: Coletar alertas do Prometheus (req.IncludeMetrics)
	// Isso será implementado quando integrarmos com o módulo de monitoramento

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
