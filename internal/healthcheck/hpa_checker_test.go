package healthcheck

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHPAChecker_MinEqualsMax(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPA com min == max
	minReplicas := int32(3)
	targetCPU := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 3, // min == max
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 3,
		},
	}

	// Criar deployment target
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(hpa, deployment)
	ctx := context.Background()

	health := checker.validateHPA(ctx, client, "default", hpa, false)

	// Verificar que detectou min == max
	if !health.IsMinEqualsMax {
		t.Error("Expected IsMinEqualsMax to be true")
	}

	// Com min == max E CurrentReplicas == MaxReplicas, temos dois issues:
	// - min == max: SeverityMedium
	// - at max replicas: SeverityHigh
	// SeverityHigh resulta em StatusCritical
	if health.Status != StatusCritical {
		t.Errorf("Expected status Critical (due to at max replicas), got %s", health.Status)
	}

	// Verificar que há issue sobre min == max com SeverityMedium
	found := false
	for _, issue := range health.Issues {
		if issue.Type == "config" && issue.Severity == SeverityMedium {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find config medium severity issue for min == max")
	}
}

func TestHPAChecker_MaxTooLow(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPA com max muito baixo
	minReplicas := int32(1)
	targetCPU := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 2, // Muito baixo
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 1,
			DesiredReplicas: 1,
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(hpa, deployment)
	ctx := context.Background()

	health := checker.validateHPA(ctx, client, "default", hpa, false)

	if !health.IsMaxTooLow {
		t.Error("Expected IsMaxTooLow to be true")
	}
}

func TestHPAChecker_AtMaxReplicas(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPA no limite máximo
	minReplicas := int32(1)
	targetCPU := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 10, // No limite máximo
			DesiredReplicas: 10,
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(hpa, deployment)
	ctx := context.Background()

	health := checker.validateHPA(ctx, client, "default", hpa, false)

	if !health.IsAtMaxReplicas {
		t.Error("Expected IsAtMaxReplicas to be true")
	}

	// IsAtMaxReplicas é SeverityHigh, que resulta em StatusCritical
	if health.Status != StatusCritical {
		t.Errorf("Expected status Critical (high severity), got %s", health.Status)
	}
}

func TestHPAChecker_TargetNotFound(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPA apontando para deployment inexistente
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "non-existent-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 5,
		},
	}

	// Não criar o deployment target
	client := fake.NewSimpleClientset(hpa)
	ctx := context.Background()

	health := checker.validateHPA(ctx, client, "default", hpa, false)

	if health.TargetExists {
		t.Error("Expected TargetExists to be false")
	}

	if health.Status != StatusCritical {
		t.Errorf("Expected status Critical, got %s", health.Status)
	}

	// Verificar que há issue sobre target não encontrado
	found := false
	for _, issue := range health.Issues {
		if issue.Type == "target" && issue.Severity == "critical" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find target critical issue")
	}
}

func TestHPAChecker_NoMetrics(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPA sem métricas
	minReplicas := int32(1)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-metrics-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 5,
			Metrics:     []autoscalingv2.MetricSpec{}, // Sem métricas
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(hpa, deployment)
	ctx := context.Background()

	health := checker.validateHPA(ctx, client, "default", hpa, false)

	if health.MetricsCount != 0 {
		t.Errorf("Expected MetricsCount to be 0, got %d", health.MetricsCount)
	}

	if health.Status != StatusCritical {
		t.Errorf("Expected status Critical, got %s", health.Status)
	}
}

func TestHPAChecker_HealthyHPA(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPA saudável
	minReplicas := int32(2)
	targetCPU := int32(80)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-hpa",
			Namespace: "default",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "test-deployment",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 5,
			DesiredReplicas: 5,
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

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(hpa, deployment)
	ctx := context.Background()

	health := checker.validateHPA(ctx, client, "default", hpa, false)

	if health.Status != StatusHealthy {
		t.Errorf("Expected status Healthy, got %s", health.Status)
	}

	if health.MetricsCount != 1 {
		t.Errorf("Expected MetricsCount to be 1, got %d", health.MetricsCount)
	}

	if len(health.Issues) != 0 {
		t.Errorf("Expected no issues, got %d", len(health.Issues))
	}
}

func TestHPAChecker_CheckAll(t *testing.T) {
	checker := NewHPAChecker()

	// Criar HPAs em múltiplos namespaces
	minReplicas := int32(2)
	targetCPU := int32(80)

	hpa1 := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hpa-1",
			Namespace: "ns1",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "deploy-1",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: 10,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 5,
			DesiredReplicas: 5,
		},
	}

	// hpa-2: minReplicas=1, maxReplicas=2 (max < 3 e min != max = IsMaxTooLow)
	minReplicasLow := int32(1)
	hpa2 := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hpa-2",
			Namespace: "ns2",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "deploy-2",
			},
			MinReplicas: &minReplicasLow, // min=1, max=2 -> IsMaxTooLow=true
			MaxReplicas: 2,               // max < 3 mas min != max
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 1,
			DesiredReplicas: 1,
		},
	}

	deploy1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-1",
			Namespace: "ns1",
		},
	}

	deploy2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-2",
			Namespace: "ns2",
		},
	}

	client := fake.NewSimpleClientset(hpa1, hpa2, deploy1, deploy2)
	ctx := context.Background()

	// Usar callback simples para testes
	callbackCount := 0
	callback := func(namespace, name, message string, status HealthStatus, current, total int) {
		callbackCount++
	}

	results := checker.CheckAll(ctx, client, []string{"ns1", "ns2"}, 30, false, callback)

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Verificar que callback foi chamado
	if callbackCount == 0 {
		t.Error("Expected callback to be called at least once")
	}

	// Verificar que encontrou os problemas esperados
	foundMaxTooLow := false
	for _, r := range results {
		if r.Name == "hpa-2" && r.IsMaxTooLow {
			foundMaxTooLow = true
		}
	}
	if !foundMaxTooLow {
		t.Error("Expected to find HPA with max too low")
	}
}

func TestHPAChecker_ExtractMetrics(t *testing.T) {
	checker := NewHPAChecker()

	targetCPU := int32(80)
	targetMemory := int32(70)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetMemory,
						},
					},
				},
			},
		},
	}

	metrics := checker.extractMetrics(hpa)

	if len(metrics) != 2 {
		t.Errorf("Expected 2 metrics, got %d", len(metrics))
	}

	// Verificar CPU
	foundCPU := false
	for _, m := range metrics {
		if m.Name == "cpu" && m.TargetValue == "80%" {
			foundCPU = true
		}
	}
	if !foundCPU {
		t.Error("Expected to find CPU metric with 80% target")
	}

	// Verificar Memory
	foundMemory := false
	for _, m := range metrics {
		if m.Name == "memory" && m.TargetValue == "70%" {
			foundMemory = true
		}
	}
	if !foundMemory {
		t.Error("Expected to find Memory metric with 70% target")
	}
}

func TestHPAChecker_DetermineStatus(t *testing.T) {
	checker := NewHPAChecker()

	tests := []struct {
		name     string
		issues   []HPAScalingIssue
		expected HealthStatus
	}{
		{
			name:     "No issues",
			issues:   []HPAScalingIssue{},
			expected: StatusHealthy,
		},
		{
			name: "Medium only (results in Warning)",
			issues: []HPAScalingIssue{
				{Severity: SeverityMedium},
			},
			expected: StatusWarning,
		},
		{
			name: "Critical only",
			issues: []HPAScalingIssue{
				{Severity: SeverityCritical},
			},
			expected: StatusCritical,
		},
		{
			name: "High only (results in Critical)",
			issues: []HPAScalingIssue{
				{Severity: SeverityHigh},
			},
			expected: StatusCritical,
		},
		{
			name: "Medium and Critical",
			issues: []HPAScalingIssue{
				{Severity: SeverityMedium},
				{Severity: SeverityCritical},
			},
			expected: StatusCritical, // Critical takes precedence
		},
		{
			name: "Low only (results in Healthy)",
			issues: []HPAScalingIssue{
				{Severity: SeverityLow},
			},
			expected: StatusHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := HPAHealth{Issues: tt.issues}
			status := checker.determineStatus(health)
			if status != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, status)
			}
		})
	}
}

func TestHPAChecker_CheckTargetExists(t *testing.T) {
	checker := NewHPAChecker()
	ctx := context.Background()

	// Criar recursos
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment",
			Namespace: "default",
		},
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-statefulset",
			Namespace: "default",
		},
	}

	client := fake.NewSimpleClientset(deployment, statefulset)

	tests := []struct {
		name     string
		ref      autoscalingv2.CrossVersionObjectReference
		expected bool
	}{
		{
			name: "Deployment exists",
			ref: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "my-deployment",
			},
			expected: true,
		},
		{
			name: "Deployment not exists",
			ref: autoscalingv2.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "non-existent",
			},
			expected: false,
		},
		{
			name: "StatefulSet exists",
			ref: autoscalingv2.CrossVersionObjectReference{
				Kind: "StatefulSet",
				Name: "my-statefulset",
			},
			expected: true,
		},
		{
			name: "Unknown kind (assume exists)",
			ref: autoscalingv2.CrossVersionObjectReference{
				Kind: "CustomResource",
				Name: "something",
			},
			expected: true, // Unknown kinds default to true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.checkTargetExists(ctx, client, "default", tt.ref)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHPAChecker_ScalingEvents(t *testing.T) {
	checker := NewHPAChecker()
	ctx := context.Background()

	// Criar evento de scaling
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-event",
			Namespace: "default",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "HorizontalPodAutoscaler",
			Name: "test-hpa",
		},
		Reason:        "SuccessfulRescale",
		Message:       "New size: 5; reason: cpu resource utilization (percentage of request) above target",
		LastTimestamp: metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
	}

	client := fake.NewSimpleClientset(event)

	events := checker.getScalingEvents(ctx, client, "default", "test-hpa")

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if events[0].Reason != "SuccessfulRescale" {
		t.Errorf("Expected reason SuccessfulRescale, got %s", events[0].Reason)
	}
}
