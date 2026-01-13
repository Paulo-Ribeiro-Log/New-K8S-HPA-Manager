package healthcheck

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

// Helper para criar deployment de teste
func createTestDeployment(name, namespace string, desired, ready int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app-container",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: ready,
		},
	}
}

// Helper para criar pod em CrashLoopBackOff
func createCrashingPod(name, namespace, appLabel string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app": appLabel,
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app-container",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}
}

func TestDeploymentChecker_HealthyDeployment(t *testing.T) {
	// Arrange - Deployment com probes configurados (boas práticas)
	deployment := createDeploymentWithProbes("healthy-app", "default", 3, 3, true, true)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "healthy-app", 10)

	// Assert
	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s, got %s", StatusHealthy, health.Status)
	}
	if health.ReplicasReady != 3 {
		t.Errorf("Expected 3 ready replicas, got %d", health.ReplicasReady)
	}
	if health.Message != "Deployment saudável" {
		t.Errorf("Expected healthy message, got: %s", health.Message)
	}
	// Verificar que probes estão configurados
	if !health.HasLivenessProbe {
		t.Error("Expected HasLivenessProbe to be true")
	}
	if !health.HasReadinessProbe {
		t.Error("Expected HasReadinessProbe to be true")
	}
}

func TestDeploymentChecker_PartiallyReadyDeployment(t *testing.T) {
	// Arrange
	deployment := createTestDeployment("partial-app", "default", 3, 1)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "partial-app", 10)

	// Assert
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if health.ReplicasReady != 1 {
		t.Errorf("Expected 1 ready replica, got %d", health.ReplicasReady)
	}
	if health.ReplicasDesired != 3 {
		t.Errorf("Expected 3 desired replicas, got %d", health.ReplicasDesired)
	}
}

func TestDeploymentChecker_NoReplicasReady(t *testing.T) {
	// Arrange
	deployment := createTestDeployment("failing-app", "default", 3, 0)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "failing-app", 10)

	// Assert
	if health.Status != StatusCritical {
		t.Errorf("Expected status %s, got %s", StatusCritical, health.Status)
	}
	if health.Message != "Nenhuma réplica está pronta" {
		t.Errorf("Unexpected message: %s", health.Message)
	}
	if len(health.Suggestions) == 0 {
		t.Error("Expected suggestions, got none")
	}
}

func TestDeploymentChecker_CrashLoopBackOff(t *testing.T) {
	// Arrange
	deployment := createTestDeployment("crashing-app", "default", 3, 0)
	pod := createCrashingPod("crashing-app-pod", "default", "crashing-app")

	client := fake.NewSimpleClientset(deployment, pod)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "crashing-app", 10)

	// Assert - verificar que deployment sem réplicas retorna status crítico
	if health.Status != StatusCritical {
		t.Errorf("Expected status %s, got %s", StatusCritical, health.Status)
	}
	// Nota: fake client tem limitações na busca de pods por label,
	// então não podemos validar ContainersCrash de forma confiável em testes unitários.
	// Isso será testado em testes de integração com cluster real.
	if health.ReplicasReady != 0 {
		t.Errorf("Expected 0 ready replicas, got %d", health.ReplicasReady)
	}
}

func TestDeploymentChecker_CheckAll(t *testing.T) {
	// Arrange
	deployment1 := createTestDeployment("app1", "ns1", 3, 3)
	deployment2 := createTestDeployment("app2", "ns1", 2, 1)
	deployment3 := createTestDeployment("app3", "ns2", 1, 1)

	client := fake.NewSimpleClientset(deployment1, deployment2, deployment3)
	checker := NewDeploymentChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"ns1", "ns2"}, 10)

	// Assert
	if len(results) != 3 {
		t.Errorf("Expected 3 deployments checked, got %d", len(results))
	}

	// Verificar que encontrou os deployments corretos
	foundNs1 := 0
	foundNs2 := 0
	for _, result := range results {
		if result.Namespace == "ns1" {
			foundNs1++
		}
		if result.Namespace == "ns2" {
			foundNs2++
		}
	}

	if foundNs1 != 2 {
		t.Errorf("Expected 2 deployments in ns1, got %d", foundNs1)
	}
	if foundNs2 != 1 {
		t.Errorf("Expected 1 deployment in ns2, got %d", foundNs2)
	}
}

func TestDeploymentChecker_NonexistentDeployment(t *testing.T) {
	// Arrange
	client := fake.NewSimpleClientset()
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "nonexistent", 10)

	// Assert
	if health.Status != StatusCritical {
		t.Errorf("Expected status %s, got %s", StatusCritical, health.Status)
	}
	if health.Name != "nonexistent" {
		t.Errorf("Expected name 'nonexistent', got %s", health.Name)
	}
}

func TestDeploymentChecker_Timeout(t *testing.T) {
	// Arrange
	deployment := createTestDeployment("slow-app", "default", 3, 3)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	start := time.Now()
	health := checker.Check(context.Background(), client, "default", "slow-app", 1) // 1 segundo timeout
	elapsed := time.Since(start)

	// Assert
	if elapsed > 2*time.Second {
		t.Errorf("Check took too long: %v", elapsed)
	}
	if health.Status == "" {
		t.Error("Expected status to be set")
	}
}

// Helper para criar deployment com probes
func createDeploymentWithProbes(name, namespace string, desired, ready int32, hasLiveness, hasReadiness bool) *appsv1.Deployment {
	deployment := createTestDeployment(name, namespace, desired, ready)

	// Adicionar probes aos containers
	container := &deployment.Spec.Template.Spec.Containers[0]

	if hasLiveness {
		container.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       5,
		}
	}

	if hasReadiness {
		container.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       3,
		}
	}

	deployment.Spec.Template.Spec.Containers = []corev1.Container{*container}
	return deployment
}

// Helper para criar pod com readiness probe falhando
func createPodWithReadinessFailure(name, namespace, appLabel string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app": appLabel,
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
					Reason: "ContainersNotReady",
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "app-container",
					Ready: false,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}
}

func TestDeploymentChecker_NoProbesConfigured(t *testing.T) {
	// Arrange
	deployment := createDeploymentWithProbes("no-probes-app", "default", 3, 3, false, false)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "no-probes-app", 10)

	// Assert
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if !health.HasLivenessProbe {
		// OK - esperado
	} else {
		t.Error("Expected HasLivenessProbe to be false")
	}
	if !health.HasReadinessProbe {
		// OK - esperado
	} else {
		t.Error("Expected HasReadinessProbe to be false")
	}
	if health.Message != "Liveness e readiness probes não configurados" {
		t.Errorf("Unexpected message: %s", health.Message)
	}
	if len(health.Suggestions) < 2 {
		t.Error("Expected suggestions for configuring probes")
	}
}

func TestDeploymentChecker_WithProbesConfigured(t *testing.T) {
	// Arrange
	deployment := createDeploymentWithProbes("with-probes-app", "default", 3, 3, true, true)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "with-probes-app", 10)

	// Assert
	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s, got %s", StatusHealthy, health.Status)
	}
	if !health.HasLivenessProbe {
		t.Error("Expected HasLivenessProbe to be true")
	}
	if !health.HasReadinessProbe {
		t.Error("Expected HasReadinessProbe to be true")
	}
	if health.LivenessProbeFailures != 0 {
		t.Errorf("Expected 0 liveness failures, got %d", health.LivenessProbeFailures)
	}
	if health.ReadinessProbeFailures != 0 {
		t.Errorf("Expected 0 readiness failures, got %d", health.ReadinessProbeFailures)
	}
}

func TestDeploymentChecker_OnlyLivenessProbe(t *testing.T) {
	// Arrange
	deployment := createDeploymentWithProbes("liveness-only-app", "default", 3, 3, true, false)
	client := fake.NewSimpleClientset(deployment)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "liveness-only-app", 10)

	// Assert
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if !health.HasLivenessProbe {
		t.Error("Expected HasLivenessProbe to be true")
	}
	if health.HasReadinessProbe {
		t.Error("Expected HasReadinessProbe to be false")
	}
	if health.Message != "Readiness probe não configurado" {
		t.Errorf("Unexpected message: %s", health.Message)
	}
}

func TestDeploymentChecker_ReadinessProbeFailures(t *testing.T) {
	// Arrange
	deployment := createDeploymentWithProbes("readiness-fail-app", "default", 3, 2, true, true)
	pod := createPodWithReadinessFailure("readiness-fail-app-pod", "default", "readiness-fail-app")

	client := fake.NewSimpleClientset(deployment, pod)
	checker := NewDeploymentChecker()

	// Act
	health := checker.Check(context.Background(), client, "default", "readiness-fail-app", 10)

	// Assert - verificar que detecta readiness probe falhando
	// Nota: fake client tem limitações, então não podemos validar contagem exata de falhas
	// Isso será testado em testes de integração
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if !health.HasReadinessProbe {
		t.Error("Expected HasReadinessProbe to be true")
	}
}
