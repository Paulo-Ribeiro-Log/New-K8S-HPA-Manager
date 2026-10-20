package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func rl(cpu, mem string) corev1.ResourceList {
	l := corev1.ResourceList{}
	if cpu != "" {
		l[corev1.ResourceCPU] = resource.MustParse(cpu)
	}
	if mem != "" {
		l[corev1.ResourceMemory] = resource.MustParse(mem)
	}
	return l
}

func testPod(ns, name, phase string, owner *metav1.OwnerReference, req, lim corev1.ResourceList) corev1.Pod {
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	if owner != nil {
		p.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	p.Spec.Containers = []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: req, Limits: lim}}}
	p.Status.Phase = corev1.PodPhase(phase)
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{RestartCount: 3}}
	return p
}

func podMetrics(ns, name string, containers map[string][2]string) metricsv1beta1.PodMetrics {
	pm := metricsv1beta1.PodMetrics{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	for n, u := range containers {
		pm.Containers = append(pm.Containers, metricsv1beta1.ContainerMetrics{Name: n, Usage: rl(u[0], u[1])})
	}
	return pm
}

func TestBuildNodeConsumers(t *testing.T) {
	yes := true
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}}
	node.Status.Allocatable = rl("4", "16Gi")
	rs := &metav1.OwnerReference{Kind: "ReplicaSet", Name: "api-7d9f8c6b5", Controller: &yes}
	sts := &metav1.OwnerReference{Kind: "StatefulSet", Name: "prom", Controller: &yes}
	pods := []corev1.Pod{
		testPod("a", "api-1", "Running", rs, rl("500m", "1Gi"), rl("1", "2Gi")),
		testPod("m", "prom-0", "Running", sts, rl("500m", "1Gi"), rl("800m", "3Gi")),
		testPod("a", "sem-metrica", "Running", nil, rl("100m", "128Mi"), nil),
		testPod("a", "job-feito", "Succeeded", nil, rl("1", "1Gi"), nil),
	}
	usage := map[podUsageKey]metricsv1beta1.PodMetrics{
		{"a", "api-1"}:  podMetrics("a", "api-1", map[string][2]string{"app": {"300m", "800Mi"}}),
		{"m", "prom-0"}: podMetrics("m", "prom-0", map[string][2]string{"prometheus": {"200m", "2Gi"}, "config-reloader": {"10m", "20Mi"}}),
	}
	out := buildNodeConsumers(node, pods, usage, true)

	if out.CPUAllocatableM != 4000 || out.MemAllocatableBytes != 16<<30 {
		t.Errorf("allocatable: %d / %d", out.CPUAllocatableM, out.MemAllocatableBytes)
	}
	if len(out.Pods) != 3 {
		t.Fatalf("pod terminado devia ficar de fora; got %d pods", len(out.Pods))
	}
	// ordem: maior memória em uso primeiro; sem métrica por último
	if out.Pods[0].Name != "prom-0" || out.Pods[1].Name != "api-1" || out.Pods[2].Name != "sem-metrica" {
		t.Errorf("ordem: %s, %s, %s", out.Pods[0].Name, out.Pods[1].Name, out.Pods[2].Name)
	}
	prom := out.Pods[0]
	if prom.CPUUsageM != 210 || prom.MemUsageBytes != 2<<30+20<<20 || !prom.HasMetrics {
		t.Errorf("prom soma dos containers: cpu=%d mem=%d", prom.CPUUsageM, prom.MemUsageBytes)
	}
	if len(prom.Containers) != 2 || prom.Containers[0].Name != "prometheus" {
		t.Errorf("containers devem vir do maior para o menor: %+v", prom.Containers)
	}
	if prom.CPULimitM != 800 || prom.MemLimitBytes != 3<<30 || prom.RestartCount != 3 {
		t.Errorf("limits/restarts: %+v", prom)
	}
	if out.Pods[1].Workload != "api" || out.Pods[1].WorkloadKind != "Deployment" {
		t.Errorf("ReplicaSet deveria virar Deployment: %s/%s", out.Pods[1].WorkloadKind, out.Pods[1].Workload)
	}
	if out.Pods[2].Workload != "sem-metrica" || out.Pods[2].WorkloadKind != "Pod" || out.Pods[2].HasMetrics {
		t.Errorf("pod sem dono/métrica: %+v", out.Pods[2])
	}
	if out.CPURequestedM != 1100 { // 500+500+100 (o Succeeded não conta)
		t.Errorf("cpu requested = %d", out.CPURequestedM)
	}
}

func TestBuildNodeConsumersSemMetricsOrdenaPorRequest(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}}
	pods := []corev1.Pod{
		testPod("a", "pequeno", "Running", nil, rl("100m", "128Mi"), nil),
		testPod("a", "grande", "Running", nil, rl("1", "4Gi"), nil),
	}
	out := buildNodeConsumers(node, pods, nil, false)
	if out.MetricsAvailable || out.Pods[0].Name != "grande" {
		t.Errorf("sem métricas, ordena pelo request: %+v", out.Pods)
	}
}
