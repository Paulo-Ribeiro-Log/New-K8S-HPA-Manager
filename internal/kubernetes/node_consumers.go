package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"k8s-hpa-manager/internal/models"
)

// maxNamespacesForScopedMetrics: acima disso, uma única listagem de métricas do cluster inteiro é mais barata do que
// uma chamada por namespace.
const maxNamespacesForScopedMetrics = 10

type podUsageKey struct{ namespace, name string }

// GetNodeConsumers lista os pods do node com o consumo atual (Metrics Server) e o que reservaram. O Metrics Server é
// opcional: sem ele, MetricsAvailable fica false e os pods vêm só com requests/limits.
func (c *Client) GetNodeConsumers(ctx context.Context, nodeName string) (*models.NodeConsumers, error) {
	node, err := c.clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}
	podList, err := c.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	var usage map[podUsageKey]metricsv1beta1.PodMetrics
	var nodeCPU, nodeMem int64
	metricsOK := false
	if c.metricsClient != nil {
		if nm, err := c.metricsClient.MetricsV1beta1().NodeMetricses().Get(ctx, nodeName, metav1.GetOptions{}); err == nil {
			nodeCPU = nm.Usage.Cpu().MilliValue()
			nodeMem = nm.Usage.Memory().Value()
		}
		usage, metricsOK = c.podMetricsFor(ctx, podList.Items)
	}

	out := buildNodeConsumers(node, podList.Items, usage, metricsOK)
	out.CPUUsageM, out.MemUsageBytes = nodeCPU, nodeMem
	return out, nil
}

// podMetricsFor busca as métricas dos namespaces dos pods do node (ou do cluster todo, se forem muitos).
func (c *Client) podMetricsFor(ctx context.Context, pods []corev1.Pod) (map[podUsageKey]metricsv1beta1.PodMetrics, bool) {
	namespaces := map[string]struct{}{}
	for _, p := range pods {
		namespaces[p.Namespace] = struct{}{}
	}
	scopes := []string{""}
	if len(namespaces) <= maxNamespacesForScopedMetrics {
		scopes = scopes[:0]
		for ns := range namespaces {
			scopes = append(scopes, ns)
		}
	}
	usage := map[podUsageKey]metricsv1beta1.PodMetrics{}
	ok := false
	for _, ns := range scopes {
		list, err := c.metricsClient.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		ok = true
		for _, pm := range list.Items {
			usage[podUsageKey{pm.Namespace, pm.Name}] = pm
		}
	}
	return usage, ok
}

// buildNodeConsumers monta a resposta a partir do que foi coletado (função pura, testável). Pods terminados
// (Succeeded/Failed) não ocupam o node e ficam de fora. A ordem é a do maior consumo de memória; o frontend reordena.
func buildNodeConsumers(node *corev1.Node, pods []corev1.Pod, usage map[podUsageKey]metricsv1beta1.PodMetrics, metricsOK bool) *models.NodeConsumers {
	out := &models.NodeConsumers{
		Node:                node.Name,
		CPUAllocatableM:     node.Status.Allocatable.Cpu().MilliValue(),
		MemAllocatableBytes: node.Status.Allocatable.Memory().Value(),
		MetricsAvailable:    metricsOK,
		Pods:                []models.NodeConsumer{},
	}
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		wl, kind := podWorkload(&pod)
		c := models.NodeConsumer{
			Name: pod.Name, Namespace: pod.Namespace, Workload: wl, WorkloadKind: kind, Phase: string(pod.Status.Phase),
		}
		for _, ct := range pod.Spec.Containers {
			c.CPURequestM += ct.Resources.Requests.Cpu().MilliValue()
			c.MemRequestBytes += ct.Resources.Requests.Memory().Value()
			c.CPULimitM += ct.Resources.Limits.Cpu().MilliValue()
			c.MemLimitBytes += ct.Resources.Limits.Memory().Value()
		}
		for _, st := range pod.Status.ContainerStatuses {
			c.RestartCount += int(st.RestartCount)
		}
		if pm, ok := usage[podUsageKey{pod.Namespace, pod.Name}]; ok {
			c.HasMetrics = true
			for _, ct := range pm.Containers {
				cc := models.NodeConsumerContainer{Name: ct.Name, CPUUsageM: ct.Usage.Cpu().MilliValue(), MemUsageBytes: ct.Usage.Memory().Value()}
				c.CPUUsageM += cc.CPUUsageM
				c.MemUsageBytes += cc.MemUsageBytes
				c.Containers = append(c.Containers, cc)
			}
			sort.Slice(c.Containers, func(i, j int) bool { return c.Containers[i].MemUsageBytes > c.Containers[j].MemUsageBytes })
		}
		out.CPURequestedM += c.CPURequestM
		out.MemRequestedBytes += c.MemRequestBytes
		out.Pods = append(out.Pods, c)
	}
	sort.SliceStable(out.Pods, func(i, j int) bool {
		if out.Pods[i].MemUsageBytes != out.Pods[j].MemUsageBytes {
			return out.Pods[i].MemUsageBytes > out.Pods[j].MemUsageBytes
		}
		return out.Pods[i].MemRequestBytes > out.Pods[j].MemRequestBytes
	})
	return out
}

// podWorkload devolve o nome e o tipo do dono do pod. ReplicaSet vira o Deployment (tira o hash do nome); pod sem dono
// devolve o próprio nome com o tipo "Pod".
func podWorkload(pod *corev1.Pod) (string, string) {
	for _, o := range pod.OwnerReferences {
		if o.Controller == nil || !*o.Controller {
			continue
		}
		if o.Kind == "ReplicaSet" {
			if i := strings.LastIndex(o.Name, "-"); i > 0 {
				return o.Name[:i], "Deployment"
			}
		}
		return o.Name, o.Kind
	}
	return pod.Name, "Pod"
}
