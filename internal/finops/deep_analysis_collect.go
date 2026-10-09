package finops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ErrDeepPoolNotFound indica que nenhum node do cluster pertence ao pool pedido.
var ErrDeepPoolNotFound = errors.New("nenhum node encontrado para o node pool")

// DeepCollected é o que a coleta AO VIVO do pool devolve (só leitura no cluster).
type DeepCollected struct {
	Nodes []DeepNode
	Pods  []DeepPod
	// HPAs dos namespaces com pods no pool, chave "namespace/alvo".
	HPAs       map[string]DeepHPA
	Namespaces []string
	VMSize     string
	Region     string
	Priority   string
	// OSDisk: managed | ephemeral (label kubernetes.azure.com/storageprofile da AKS; "" se ausente).
	OSDisk      string
	MetricsLive bool
	Warnings    []string
}

// nodePriority classifica o node em regular/spot pelos labels de cada cloud.
func nodePriority(labels map[string]string) string {
	switch {
	case labels["kubernetes.azure.com/scalesetpriority"] == "spot",
		labels["eks.amazonaws.com/capacityType"] == "SPOT",
		labels["cloud.google.com/gke-spot"] == "true",
		labels["cloud.google.com/gke-preemptible"] == "true":
		return "spot"
	}
	return "regular"
}

// podOwnerKind devolve o tipo do dono do pod (ReplicaSet de Deployment vira "Deployment").
func podOwnerKind(pod *corev1.Pod, rsOwner map[string]string) string {
	for _, ref := range pod.OwnerReferences {
		switch ref.Kind {
		case "ReplicaSet":
			if _, ok := rsOwner[pod.Namespace+"/"+ref.Name]; ok {
				return "Deployment"
			}
			return "ReplicaSet"
		case "StatefulSet", "DaemonSet", "Job":
			return ref.Kind
		}
	}
	return "Pod"
}

func podMissingMemLimit(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if _, ok := c.Resources.Limits[corev1.ResourceMemory]; !ok {
			return true
		}
	}
	return false
}

// CollectPoolDeepInput coleta do cluster os nodes do pool, os pods Running neles e o uso ao vivo
// (metrics-server, best-effort). Só leitura.
func CollectPoolDeepInput(ctx context.Context, client kubernetes.Interface, metricsClient metricsclientset.Interface, pool string) (*DeepCollected, error) {
	nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listar nodes: %w", err)
	}

	out := &DeepCollected{}
	nodeIdx := map[string]int{}
	for _, n := range nodeList.Items {
		if nodePoolLabelFromNode(n.Labels) != pool {
			continue
		}
		dn := DeepNode{
			Name:           n.Name,
			Zone:           n.Labels["topology.kubernetes.io/zone"],
			CPUCapMillis:   float64(n.Status.Capacity.Cpu().MilliValue()),
			MemCapMi:       float64(n.Status.Capacity.Memory().Value()) / (1024 * 1024),
			CPUAllocMillis: float64(n.Status.Allocatable.Cpu().MilliValue()),
			MemAllocMi:     float64(n.Status.Allocatable.Memory().Value()) / (1024 * 1024),
			PodsAlloc:      int(n.Status.Allocatable.Pods().Value()),
			CreatedAt:      n.CreationTimestamp.Time,
		}
		nodeIdx[n.Name] = len(out.Nodes)
		out.Nodes = append(out.Nodes, dn)
		if out.VMSize == "" {
			out.VMSize = n.Labels["node.kubernetes.io/instance-type"]
			if out.VMSize == "" {
				out.VMSize = n.Labels["beta.kubernetes.io/instance-type"]
			}
		}
		if out.Region == "" {
			out.Region = n.Labels["topology.kubernetes.io/region"]
		}
		if out.OSDisk == "" {
			out.OSDisk = strings.ToLower(n.Labels["kubernetes.azure.com/storageprofile"])
		}
		if nodePriority(n.Labels) == "spot" {
			out.Priority = "spot"
		}
	}
	if len(out.Nodes) == 0 {
		return nil, fmt.Errorf("%w '%s'", ErrDeepPoolNotFound, pool)
	}
	if out.Priority == "" {
		out.Priority = "regular"
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	for i, n := range out.Nodes {
		nodeIdx[n.Name] = i
	}

	podItems, err := listAllPods(ctx, client, metav1.ListOptions{FieldSelector: "status.phase=Running"})
	if err != nil {
		return nil, fmt.Errorf("listar pods: %w", err)
	}
	var poolPods []*corev1.Pod
	namespaces := map[string]bool{}
	for i := range podItems {
		p := &podItems[i]
		if _, ok := nodeIdx[p.Spec.NodeName]; !ok {
			continue
		}
		poolPods = append(poolPods, p)
		namespaces[p.Namespace] = true
	}

	// ReplicaSet → Deployment só dos namespaces com pods no pool (muito mais leve que listar o
	// cluster inteiro). Falha num namespace não aborta: ResolveWorkload cai no nome sem hash.
	rsOwner := map[string]string{}
	for ns := range namespaces {
		rsList, rsErr := client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
		if rsErr != nil {
			log.Debug().Err(rsErr).Str("namespace", ns).Msg("Deep Analysis: falha ao listar ReplicaSets")
			continue
		}
		for _, rs := range rsList.Items {
			for _, ref := range rs.OwnerReferences {
				if ref.Kind == "Deployment" {
					rsOwner[rs.Namespace+"/"+rs.Name] = ref.Name
				}
			}
		}
	}

	out.HPAs = map[string]DeepHPA{}
	for ns := range namespaces {
		out.Namespaces = append(out.Namespaces, ns)
		hpaList, hErr := client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
		if hErr != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("HPAs do namespace %s: %v", ns, hErr))
			continue
		}
		for i := range hpaList.Items {
			h := &hpaList.Items[i]
			out.HPAs[h.Namespace+"/"+h.Spec.ScaleTargetRef.Name] = deepHPAFrom(h)
		}
	}
	sort.Strings(out.Namespaces)

	podIdx := map[string]int{}
	for _, p := range poolPods {
		podIdx[p.Namespace+"/"+p.Name] = len(out.Pods)
		out.Pods = append(out.Pods, DeepPod{
			Namespace:       p.Namespace,
			Name:            p.Name,
			Node:            p.Spec.NodeName,
			OwnerKind:       podOwnerKind(p, rsOwner),
			Workload:        ResolveWorkload(p, rsOwner),
			CPUReqMillis:    sumCPURequestsMillis(p),
			MemReqMi:        sumMemRequestsMi(p),
			CPULimMillis:    sumCPULimitsMillis(p),
			MemLimMi:        sumMemLimitsMi(p),
			MissingMemLimit: podMissingMemLimit(p),
		})
	}

	if metricsClient == nil {
		return out, nil
	}
	nodeMetrics, nmErr := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	podMetrics, pmErr := metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	if nmErr != nil || pmErr != nil {
		err := nmErr
		if err == nil {
			err = pmErr
		}
		out.Warnings = append(out.Warnings, "metrics-server: "+err.Error())
		return out, nil
	}
	out.MetricsLive = true
	for _, nm := range nodeMetrics.Items {
		if i, ok := nodeIdx[nm.Name]; ok {
			out.Nodes[i].CPUUsageMillis = float64(nm.Usage.Cpu().MilliValue())
			out.Nodes[i].MemUsageMi = float64(nm.Usage.Memory().Value()) / (1024 * 1024)
			out.Nodes[i].HasUsage = true
		}
	}
	for _, pm := range podMetrics.Items {
		i, ok := podIdx[pm.Namespace+"/"+pm.Name]
		if !ok {
			continue
		}
		for _, c := range pm.Containers {
			out.Pods[i].CPUUsageMillis += float64(c.Usage.Cpu().MilliValue())
			out.Pods[i].MemUsageMi += float64(c.Usage.Memory().Value()) / (1024 * 1024)
		}
		out.Pods[i].HasUsage = true
	}
	return out, nil
}

// DeepHistoryFromReport extrai, do último FinOpsReport em cache, o histórico por pod de cada workload
// (chave "namespace/workload").
func DeepHistoryFromReport(r *FinOpsReport) map[string]DeepHistory {
	if r == nil {
		return nil
	}
	h := make(map[string]DeepHistory, len(r.Workloads))
	for _, w := range r.Workloads {
		if w.MetricsSource == "" && w.CPUP95Millis == 0 && w.MemP95Mi == 0 {
			continue
		}
		h[w.Namespace+"/"+w.Workload] = DeepHistory{
			CPUAvgMillis:  w.CPUAvgMillis,
			CPUP95Millis:  w.CPUP95Millis,
			CPUPeakMillis: w.CPUMaxMillis,
			MemAvgMi:      w.MemAvgMi,
			MemP95Mi:      w.MemP95Mi,
			MemPeakMi:     w.MemMaxMi,
			Source:        w.MetricsSource,

			HPAAvgReplicas: w.HPAAvgReplicas,
			HPAMaxObserved: w.HPAMaxObserved,
			HPAMinObserved: w.HPAMinObserved,
			HPAScaleEvents: w.HPAScaleEvents,
			HPANeverScaled: w.HPANeverScaled,
		}
	}
	return h
}

// deepHPAFrom converte um HPA (autoscaling/v2) para DeepHPA, juntando o alvo do spec com a
// utilização atual do status.
func deepHPAFrom(h *autoscalingv2.HorizontalPodAutoscaler) DeepHPA {
	d := DeepHPA{Name: h.Name, Min: 1, Max: int(h.Spec.MaxReplicas), Current: int(h.Status.CurrentReplicas)}
	if h.Spec.MinReplicas != nil {
		d.Min = int(*h.Spec.MinReplicas)
	}
	current := map[string]*int{}
	for _, cm := range h.Status.CurrentMetrics {
		var name string
		var util *int32
		switch {
		case cm.Resource != nil:
			name, util = string(cm.Resource.Name), cm.Resource.Current.AverageUtilization
		case cm.ContainerResource != nil:
			name, util = string(cm.ContainerResource.Name), cm.ContainerResource.Current.AverageUtilization
		}
		if name != "" && util != nil {
			v := int(*util)
			current[string(cm.Type)+"/"+name] = &v
		}
	}
	for _, m := range h.Spec.Metrics {
		dm := DeepHPAMetric{Type: string(m.Type)}
		var target *autoscalingv2.MetricTarget
		switch {
		case m.Resource != nil:
			dm.Name, target = string(m.Resource.Name), &m.Resource.Target
		case m.ContainerResource != nil:
			dm.Name, target = string(m.ContainerResource.Name), &m.ContainerResource.Target
		case m.Pods != nil:
			dm.Name, target = m.Pods.Metric.Name, &m.Pods.Target
		case m.Object != nil:
			dm.Name, target = m.Object.Metric.Name, &m.Object.Target
		case m.External != nil:
			dm.Name, target = m.External.Metric.Name, &m.External.Target
		}
		if target != nil {
			dm.TargetType = string(target.Type)
			if target.AverageUtilization != nil {
				dm.TargetUtilization = int(*target.AverageUtilization)
			}
			switch {
			case target.AverageValue != nil:
				dm.TargetValue = target.AverageValue.String()
			case target.Value != nil:
				dm.TargetValue = target.Value.String()
			}
		}
		dm.CurrentUtilization = current[dm.Type+"/"+dm.Name]
		d.Metrics = append(d.Metrics, dm)
	}
	return d
}
