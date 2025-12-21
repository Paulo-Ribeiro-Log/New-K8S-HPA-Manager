package collectors

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// DeploymentCollector coleta contexto de diagnóstico de Deployments
type DeploymentCollector struct {
	clientset kubernetes.Interface
}

// NewDeploymentCollector cria um novo DeploymentCollector
func NewDeploymentCollector(clientset kubernetes.Interface) *DeploymentCollector {
	return &DeploymentCollector{
		clientset: clientset,
	}
}

// Collect coleta contexto completo de um Deployment
func (c *DeploymentCollector) Collect(ctx context.Context, namespace, deploymentName string, req *ContextRequest) (*DeploymentContext, error) {
	depContext := &DeploymentContext{
		ReplicaSets: []string{},
		Pods:        []PodSummary{},
	}

	// 1. Obter manifesto do Deployment
	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// Converter para YAML
	yamlBytes, err := yaml.Marshal(deployment)
	if err == nil {
		depContext.ManifestYAML = string(yamlBytes)
	}

	// 2. Coletar ReplicaSets associados
	replicaSets, err := c.clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	})
	if err == nil {
		for _, rs := range replicaSets.Items {
			depContext.ReplicaSets = append(depContext.ReplicaSets, rs.Name)
		}
	}

	// 3. Coletar Pods gerenciados
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	})
	if err == nil {
		for _, pod := range pods.Items {
			depContext.Pods = append(depContext.Pods, c.podToSummary(&pod))
		}
	}

	// 4. Status do rollout
	depContext.RolloutStatus = fmt.Sprintf("Available: %d/%d, Updated: %d, Ready: %d",
		deployment.Status.AvailableReplicas,
		deployment.Status.Replicas,
		deployment.Status.UpdatedReplicas,
		deployment.Status.ReadyReplicas,
	)

	return depContext, nil
}

// podToSummary converte um Pod para PodSummary
func (c *DeploymentCollector) podToSummary(pod *corev1.Pod) PodSummary {
	summary := PodSummary{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Phase:     string(pod.Status.Phase),
	}

	// Contagem de restarts e status de containers
	for _, containerStatus := range pod.Status.ContainerStatuses {
		summary.RestartCount += containerStatus.RestartCount

		cs := ContainerStatus{
			Name:         containerStatus.Name,
			Ready:        containerStatus.Ready,
			RestartCount: containerStatus.RestartCount,
		}

		if containerStatus.State.Running != nil {
			cs.State = "Running"
		} else if containerStatus.State.Waiting != nil {
			cs.State = "Waiting"
			cs.Reason = containerStatus.State.Waiting.Reason
			cs.Message = containerStatus.State.Waiting.Message
		} else if containerStatus.State.Terminated != nil {
			cs.State = "Terminated"
			cs.Reason = containerStatus.State.Terminated.Reason
			cs.Message = containerStatus.State.Terminated.Message
		}

		summary.ContainerStatuses = append(summary.ContainerStatuses, cs)
	}

	return summary
}
