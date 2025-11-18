package monitor

import (
	"context"
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestCollectHPASnapshot testa a coleta de snapshot de HPA
func TestCollectHPASnapshot(t *testing.T) {
	// Mock cluster info
	cluster := &models.ClusterInfo{
		Name:    "test-cluster",
		Context: "test-context",
		Server:  "https://localhost:6443",
	}

	// Mock HPA
	minReplicas := int32(2)
	cpuTarget := int32(70)
	memTarget := int32(80)
	lastScaleTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "test-namespace",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deployment",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &cpuTarget,
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &memTarget,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 4,
			LastScaleTime:   &lastScaleTime,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:   autoscalingv2.ScalingActive,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   autoscalingv2.AbleToScale,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	// Create K8sClient (sem clientset real para teste unitário)
	client := &K8sClient{
		cluster: cluster,
	}

	// Collect snapshot
	ctx := context.Background()
	snapshot, err := client.CollectHPASnapshot(ctx, hpa)

	// Validations
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if snapshot == nil {
		t.Fatal("Expected snapshot, got nil")
	}

	// Verify basic fields
	if snapshot.Cluster != "test-cluster" {
		t.Errorf("Expected cluster 'test-cluster', got '%s'", snapshot.Cluster)
	}

	if snapshot.Namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got '%s'", snapshot.Namespace)
	}

	if snapshot.Name != "test-hpa" {
		t.Errorf("Expected name 'test-hpa', got '%s'", snapshot.Name)
	}

	// Verify replicas
	if snapshot.MinReplicas != 2 {
		t.Errorf("Expected MinReplicas 2, got %d", snapshot.MinReplicas)
	}

	if snapshot.MaxReplicas != 10 {
		t.Errorf("Expected MaxReplicas 10, got %d", snapshot.MaxReplicas)
	}

	if snapshot.CurrentReplicas != 3 {
		t.Errorf("Expected CurrentReplicas 3, got %d", snapshot.CurrentReplicas)
	}

	if snapshot.DesiredReplicas != 4 {
		t.Errorf("Expected DesiredReplicas 4, got %d", snapshot.DesiredReplicas)
	}

	// Verify targets
	if snapshot.CPUTarget != 70 {
		t.Errorf("Expected CPUTarget 70, got %d", snapshot.CPUTarget)
	}

	if snapshot.MemoryTarget != 80 {
		t.Errorf("Expected MemoryTarget 80, got %d", snapshot.MemoryTarget)
	}

	// Verify status
	if !snapshot.ScalingActive {
		t.Error("Expected ScalingActive to be true")
	}

	if !snapshot.Ready {
		t.Error("Expected Ready to be true")
	}

	if snapshot.LastScaleTime == nil {
		t.Error("Expected LastScaleTime to be set")
	}

	// Verify timestamp is recent
	if time.Since(snapshot.Timestamp) > time.Second {
		t.Error("Expected recent timestamp")
	}
}

// TestContainsHelper testa a função helper contains
func TestContainsHelper(t *testing.T) {
	slice := []string{"foo", "bar", "baz"}

	if !contains(slice, "foo") {
		t.Error("Expected contains to find 'foo'")
	}

	if !contains(slice, "bar") {
		t.Error("Expected contains to find 'bar'")
	}

	if contains(slice, "notfound") {
		t.Error("Expected contains to not find 'notfound'")
	}

	if contains([]string{}, "foo") {
		t.Error("Expected contains to not find anything in empty slice")
	}
}

// TestFilterNamespaces testa a filtragem de namespaces
func TestFilterNamespaces(t *testing.T) {
	allNamespaces := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
		"production",
		"staging",
		"development",
	}

	exclude := []string{
		"kube-system",
		"kube-public",
		"kube-node-lease",
		"default",
	}

	var filtered []string
	for _, ns := range allNamespaces {
		if !contains(exclude, ns) {
			filtered = append(filtered, ns)
		}
	}

	expected := 3 // production, staging, development
	if len(filtered) != expected {
		t.Errorf("Expected %d filtered namespaces, got %d", expected, len(filtered))
	}

	if contains(filtered, "kube-system") {
		t.Error("Expected kube-system to be filtered out")
	}

	if !contains(filtered, "production") {
		t.Error("Expected production to be included")
	}
}

// BenchmarkCollectHPASnapshot benchmark para CollectHPASnapshot
func BenchmarkCollectHPASnapshot(b *testing.B) {
	cluster := &models.ClusterInfo{
		Name:    "bench-cluster",
		Context: "bench-context",
		Server:  "https://localhost:6443",
	}

	minReplicas := int32(2)
	cpuTarget := int32(70)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bench-hpa",
			Namespace: "bench-namespace",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "bench-deployment",
			},
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &cpuTarget,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 4,
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:   autoscalingv2.ScalingActive,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	client := &K8sClient{
		cluster: cluster,
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.CollectHPASnapshot(ctx, hpa)
	}
}

// TestResourceParsing testa o parsing de resources
func TestResourceParsing(t *testing.T) {
	tests := []struct {
		name     string
		quantity string
		want     string
	}{
		{"CPU millicores", "500m", "500m"},
		{"CPU cores", "2", "2"},
		{"Memory Mi", "512Mi", "512Mi"},
		{"Memory Gi", "1Gi", "1Gi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := resource.ParseQuantity(tt.quantity)
			if err != nil {
				t.Errorf("Failed to parse quantity %s: %v", tt.quantity, err)
			}

			got := q.String()
			if got != tt.want {
				t.Errorf("Expected %s, got %s", tt.want, got)
			}
		})
	}
}
