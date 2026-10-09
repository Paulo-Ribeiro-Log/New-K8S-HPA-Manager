package finops

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func deepTestNode(name, pool, priority string) *corev1.Node {
	labels := map[string]string{
		"kubernetes.azure.com/agentpool":   pool,
		"node.kubernetes.io/instance-type": "Standard_F4s_v2",
		"topology.kubernetes.io/region":    "brazilsouth",
		"topology.kubernetes.io/zone":      "brazilsouth-1",
	}
	if priority != "" {
		labels["kubernetes.azure.com/scalesetpriority"] = priority
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8063Mi"), corev1.ResourcePods: resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("3860m"), corev1.ResourceMemory: resource.MustParse("5817Mi"), corev1.ResourcePods: resource.MustParse("110"),
			},
		},
	}
}

func deepTestPod(ns, name, node, ownerKind, ownerName, cpuReq, memReq, memLim string) *corev1.Pod {
	res := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	if cpuReq != "" {
		res.Requests[corev1.ResourceCPU] = resource.MustParse(cpuReq)
	}
	if memReq != "" {
		res.Requests[corev1.ResourceMemory] = resource.MustParse(memReq)
	}
	if memLim != "" {
		res.Limits[corev1.ResourceMemory] = resource.MustParse(memLim)
	}
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "c", Resources: res}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	if ownerKind != "" {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: ownerKind, Name: ownerName}}
	}
	return p
}

func TestCollectPoolDeepInput(t *testing.T) {
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Namespace: "fretes", Name: "frete-hub-7d9f8b6c5",
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "frete-hub"}},
	}}
	client := fake.NewSimpleClientset(
		deepTestNode("n1", "calculofrete", ""),
		deepTestNode("n2", "calculofrete", "spot"),
		deepTestNode("outro", "system", ""),
		rs,
		&autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Namespace: "fretes", Name: "frete-hub-hpa"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "frete-hub"}, MaxReplicas: 120,
			},
		},
		&autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Namespace: "sem-pods-no-pool", Name: "x"},
			Spec:       autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Name: "x"}, MaxReplicas: 2},
		},
		deepTestPod("fretes", "frete-hub-7d9f8b6c5-abcde", "n1", "ReplicaSet", "frete-hub-7d9f8b6c5", "800m", "950Mi", "1500Mi"),
		deepTestPod("logging", "fluentd-x1", "n2", "DaemonSet", "fluentd", "100m", "300Mi", ""),
		deepTestPod("fretes", "job-123", "n1", "Job", "job-123", "", "", ""),
		deepTestPod("fretes", "fora-do-pool", "outro", "ReplicaSet", "frete-hub-7d9f8b6c5", "800m", "950Mi", "1500Mi"),
	)

	col, err := CollectPoolDeepInput(context.Background(), client, nil, "calculofrete")
	if err != nil {
		t.Fatal(err)
	}
	if len(col.Nodes) != 2 || col.VMSize != "Standard_F4s_v2" || col.Region != "brazilsouth" || col.Priority != "spot" {
		t.Errorf("coleta de nodes: %d nodes, vm=%s, região=%s, prioridade=%s", len(col.Nodes), col.VMSize, col.Region, col.Priority)
	}
	if col.Nodes[0].CPUAllocMillis != 3860 || col.Nodes[0].MemAllocMi != 5817 || col.Nodes[0].PodsAlloc != 110 {
		t.Errorf("alocável: %+v", col.Nodes[0])
	}
	if col.MetricsLive || col.Nodes[0].HasUsage {
		t.Error("sem metrics-server, não pode haver uso ao vivo")
	}
	if len(col.Pods) != 3 {
		t.Fatalf("pods do pool = %d; quer 3 (o pod em outro pool fica de fora)", len(col.Pods))
	}
	byName := map[string]DeepPod{}
	for _, p := range col.Pods {
		byName[p.Name] = p
	}
	hub := byName["frete-hub-7d9f8b6c5-abcde"]
	if hub.OwnerKind != "Deployment" || hub.Workload != "frete-hub" || hub.CPUReqMillis != 800 || hub.MemLimMi != 1500 || hub.MissingMemLimit {
		t.Errorf("pod do Deployment: %+v", hub)
	}
	if fl := byName["fluentd-x1"]; fl.OwnerKind != "DaemonSet" || fl.Workload != "fluentd" || !fl.MissingMemLimit {
		t.Errorf("pod do DaemonSet: %+v", fl)
	}
	if j := byName["job-123"]; j.OwnerKind != "Job" {
		t.Errorf("pod do Job: %+v", j)
	}

	if len(col.HPAs) != 1 || col.HPAs["fretes/frete-hub"].Name != "frete-hub-hpa" || col.HPAs["fretes/frete-hub"].Min != 1 {
		t.Errorf("HPAs coletados = %+v; quer só o do namespace com pods no pool", col.HPAs)
	}
	if len(col.Namespaces) != 2 || col.Namespaces[0] != "fretes" || col.Namespaces[1] != "logging" {
		t.Errorf("namespaces = %v", col.Namespaces)
	}

	if _, err := CollectPoolDeepInput(context.Background(), client, nil, "inexistente"); !errors.Is(err, ErrDeepPoolNotFound) {
		t.Errorf("pool inexistente deveria dar ErrDeepPoolNotFound: %v", err)
	}
}
