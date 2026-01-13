package collectors

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// HPACollector coleta contexto de diagnóstico de HPAs
type HPACollector struct {
	clientset kubernetes.Interface
}

// NewHPACollector cria um novo HPACollector
func NewHPACollector(clientset kubernetes.Interface) *HPACollector {
	return &HPACollector{
		clientset: clientset,
	}
}

// Collect coleta contexto completo de um HPA
func (c *HPACollector) Collect(ctx context.Context, namespace, hpaName string, req *ContextRequest) (*HPAContext, error) {
	hpaContext := &HPAContext{
		CurrentMetrics: make(map[string]interface{}),
		ScalingHistory: []ScalingEvent{},
	}

	// 1. Obter manifesto do HPA
	hpa, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, hpaName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get hpa: %w", err)
	}
	hpaContext.Manifest = hpa

	// 2. Verificar se está maxed out
	if hpa.Status.CurrentReplicas == hpa.Spec.MaxReplicas {
		hpaContext.IsMaxedOut = true
	}

	// 3. Coletar métricas atuais
	for _, metric := range hpa.Status.CurrentMetrics {
		switch metric.Type {
		case "Resource":
			if metric.Resource != nil {
				hpaContext.CurrentMetrics[string(metric.Resource.Name)] = map[string]interface{}{
					"current": metric.Resource.Current.AverageUtilization,
					"target":  hpa.Spec.Metrics[0].Resource.Target.AverageUtilization,
				}
			}
		case "Pods":
			if metric.Pods != nil {
				hpaContext.CurrentMetrics["pods"] = map[string]interface{}{
					"current": metric.Pods.Current.AverageValue.String(),
				}
			}
		}
	}

	// 4. Target deployment
	hpaContext.TargetDeployment = hpa.Spec.ScaleTargetRef.Name

	return hpaContext, nil
}
