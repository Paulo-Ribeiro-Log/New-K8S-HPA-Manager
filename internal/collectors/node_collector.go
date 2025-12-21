package collectors

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// NodeCollector coleta contexto de diagnóstico de Nodes
type NodeCollector struct {
	clientset kubernetes.Interface
}

// NewNodeCollector cria um novo NodeCollector
func NewNodeCollector(clientset kubernetes.Interface) *NodeCollector {
	return &NodeCollector{
		clientset: clientset,
	}
}

// Collect coleta contexto completo de um Node
func (c *NodeCollector) Collect(ctx context.Context, nodeName string, req *ContextRequest) (*NodeContext, error) {
	nodeContext := &NodeContext{
		PodsRunning: []PodSummary{},
	}

	// 1. Obter manifesto do Node
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Converter para YAML
	yamlBytes, err := yaml.Marshal(node)
	if err == nil {
		nodeContext.ManifestYAML = string(yamlBytes)
	}

	// 2. Coletar conditions, taints, resources
	nodeContext.Conditions = node.Status.Conditions
	nodeContext.Taints = node.Spec.Taints
	nodeContext.Allocatable = node.Status.Allocatable
	nodeContext.Capacity = node.Status.Capacity

	// 3. Listar pods rodando no node
	pods, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err == nil {
		for _, pod := range pods.Items {
			nodeContext.PodsRunning = append(nodeContext.PodsRunning, c.podToSummary(&pod))
		}
	}

	return nodeContext, nil
}

// podToSummary converte um Pod para PodSummary
func (c *NodeCollector) podToSummary(pod *corev1.Pod) PodSummary {
	summary := PodSummary{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Phase:     string(pod.Status.Phase),
	}

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
		} else if containerStatus.State.Terminated != nil {
			cs.State = "Terminated"
			cs.Reason = containerStatus.State.Terminated.Reason
		}

		summary.ContainerStatuses = append(summary.ContainerStatuses, cs)
	}

	return summary
}
