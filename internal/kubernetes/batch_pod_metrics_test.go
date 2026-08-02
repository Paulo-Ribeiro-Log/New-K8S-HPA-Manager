package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mustQuantity(t *testing.T, s string) resource.Quantity {
	t.Helper()
	q, err := resource.ParseQuantity(s)
	if err != nil {
		t.Fatalf("resource.ParseQuantity(%q) falhou: %v", s, err)
	}
	return q
}

// TestBuildBatchPodMetrics_ComputesPercentages cobre a lógica extraída na refatoração que
// eliminou o List() redundante de GetBatchPodMetrics (bug real de N+1 corrigido em
// PodHandler.List): buildBatchPodMetrics precisa continuar calculando os mesmos percentuais de
// antes, agora recebendo os pods já prontos em vez de listá-los sozinha.
func TestBuildBatchPodMetrics_ComputesPercentages(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "with-metrics"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    mustQuantity(t, "200m"),
							corev1.ResourceMemory: mustQuantity(t, "256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    mustQuantity(t, "400m"),
							corev1.ResourceMemory: mustQuantity(t, "512Mi"),
						},
					},
				}},
			},
		},
		{
			// Pod sem entrada em usageMap — deve ser omitido do resultado, não gerar entrada zerada.
			ObjectMeta: metav1.ObjectMeta{Name: "no-metrics"},
		},
	}

	usageMap := map[string]podMetricsUsage{
		"with-metrics": {cpu: 100, mem: 128 * 1024 * 1024}, // 100m, 128Mi
	}

	got := buildBatchPodMetrics(pods, usageMap)

	if len(got) != 1 {
		t.Fatalf("esperava 1 entrada (pod sem metrics omitido), got %d: %+v", len(got), got)
	}

	m, ok := got["with-metrics"]
	if !ok {
		t.Fatalf("esperava entrada para 'with-metrics', got %+v", got)
	}
	if m.CPUMillicores != 100 {
		t.Errorf("CPUMillicores = %d, want 100", m.CPUMillicores)
	}
	if m.CPUPercentRequest != 50 {
		t.Errorf("CPUPercentRequest = %.2f, want 50 (100m/200m)", m.CPUPercentRequest)
	}
	if m.CPUPercentLimit != 25 {
		t.Errorf("CPUPercentLimit = %.2f, want 25 (100m/400m)", m.CPUPercentLimit)
	}
	if m.MemPercentRequest != 50 {
		t.Errorf("MemPercentRequest = %.2f, want 50 (128Mi/256Mi)", m.MemPercentRequest)
	}
	if m.MemPercentLimit != 25 {
		t.Errorf("MemPercentLimit = %.2f, want 25 (128Mi/512Mi)", m.MemPercentLimit)
	}

	if _, ok := got["no-metrics"]; ok {
		t.Errorf("pod sem metrics não deveria aparecer no resultado")
	}
}

// TestBuildBatchPodMetrics_NoRequestsOrLimits garante o sentinel -1 (não 0) quando o pod não
// declara request/limit — 0 seria ambíguo com "0% de uso".
func TestBuildBatchPodMetrics_NoRequestsOrLimits(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "sem-request-limit"}},
	}
	usageMap := map[string]podMetricsUsage{
		"sem-request-limit": {cpu: 50, mem: 1024},
	}

	got := buildBatchPodMetrics(pods, usageMap)
	m, ok := got["sem-request-limit"]
	if !ok {
		t.Fatalf("esperava entrada para o pod")
	}
	if m.CPUPercentRequest != -1 || m.CPUPercentLimit != -1 || m.MemPercentRequest != -1 || m.MemPercentLimit != -1 {
		t.Errorf("esperava -1 em todos os percentuais sem request/limit, got %+v", m)
	}
}
