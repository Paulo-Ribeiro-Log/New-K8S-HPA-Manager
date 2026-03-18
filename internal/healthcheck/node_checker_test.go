package healthcheck

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestNewNodeChecker verifica criação do NodeChecker
func TestNewNodeChecker(t *testing.T) {
	checker := NewNodeChecker()
	if checker == nil {
		t.Fatal("NewNodeChecker retornou nil")
	}
}

// TestExtractResources verifica extração de recursos
func TestExtractResources(t *testing.T) {
	checker := NewNodeChecker()

	resources := corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse("4"),
		corev1.ResourceMemory:           resource.MustParse("8Gi"),
		corev1.ResourcePods:             resource.MustParse("110"),
		corev1.ResourceEphemeralStorage: resource.MustParse("100Gi"),
	}

	nr := checker.extractResources(resources)

	if nr.CPUMillis != 4000 {
		t.Errorf("CPUMillis = %d, want 4000", nr.CPUMillis)
	}
	if nr.Pods != 110 {
		t.Errorf("Pods = %d, want 110", nr.Pods)
	}
	if nr.EphemeralGB != 100 {
		t.Errorf("EphemeralGB = %d, want 100", nr.EphemeralGB)
	}
}

// TestCalculateAllocated verifica cálculo de recursos alocados
func TestCalculateAllocated(t *testing.T) {
	checker := NewNodeChecker()

	pods := []corev1.Pod{
		{
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			},
		},
		{
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
		},
	}

	allocated := checker.calculateAllocated(pods)

	if allocated.Pods != 2 {
		t.Errorf("Pods = %d, want 2", allocated.Pods)
	}
	if allocated.CPUMillis != 300 {
		t.Errorf("CPUMillis = %d, want 300", allocated.CPUMillis)
	}
}

// TestCalculateAllocatedIgnoresTerminalPods verifica que pods terminais são ignorados
func TestCalculateAllocatedIgnoresTerminalPods(t *testing.T) {
	checker := NewNodeChecker()

	pods := []corev1.Pod{
		{
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("100m"),
							},
						},
					},
				},
			},
		},
		{
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded}, // Pod terminal
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("500m"),
							},
						},
					},
				},
			},
		},
	}

	allocated := checker.calculateAllocated(pods)

	// Deveria ignorar o pod Succeeded
	if allocated.CPUMillis != 100 {
		t.Errorf("CPUMillis = %d, want 100 (pod Succeeded deveria ser ignorado)", allocated.CPUMillis)
	}
}

// TestCheckNodeHealthy verifica node saudável
func TestCheckNodeHealthy(t *testing.T) {
	checker := NewNodeChecker()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
				{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion: "v1.28.0",
				OSImage:        "Ubuntu 22.04",
				Architecture:   "amd64",
			},
		},
	}

	health := checker.checkNode(node, []corev1.Pod{})

	if health.Status != StatusHealthy {
		t.Errorf("Status = %s, want healthy", health.Status)
	}
	if !health.Ready {
		t.Error("Ready deveria ser true")
	}
	if health.MemoryPressure {
		t.Error("MemoryPressure deveria ser false")
	}
}

// TestCheckNodeNotReady verifica node não ready
func TestCheckNodeNotReady(t *testing.T) {
	checker := NewNodeChecker()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:  resource.MustParse("4"),
				corev1.ResourcePods: resource.MustParse("110"),
			},
		},
	}

	health := checker.checkNode(node, []corev1.Pod{})

	if health.Status != StatusCritical {
		t.Errorf("Status = %s, want critical", health.Status)
	}
	if health.Ready {
		t.Error("Ready deveria ser false")
	}
}

// TestCheckNodeMemoryPressure verifica node com pressão de memória
func TestCheckNodeMemoryPressure(t *testing.T) {
	checker := NewNodeChecker()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:  resource.MustParse("4"),
				corev1.ResourcePods: resource.MustParse("110"),
			},
		},
	}

	health := checker.checkNode(node, []corev1.Pod{})

	if health.Status != StatusCritical {
		t.Errorf("Status = %s, want critical", health.Status)
	}
	if !health.MemoryPressure {
		t.Error("MemoryPressure deveria ser true")
	}
}

// TestCheckNodeDiskPressure verifica node com pressão de disco
func TestCheckNodeDiskPressure(t *testing.T) {
	checker := NewNodeChecker()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:  resource.MustParse("4"),
				corev1.ResourcePods: resource.MustParse("110"),
			},
		},
	}

	health := checker.checkNode(node, []corev1.Pod{})

	if health.Status != StatusCritical {
		t.Errorf("Status = %s, want critical", health.Status)
	}
	if !health.DiskPressure {
		t.Error("DiskPressure deveria ser true")
	}
}

// TestCheckNodeHighCPUUtilization verifica warning para alta utilização de CPU
func TestCheckNodeHighCPUUtilization(t *testing.T) {
	checker := NewNodeChecker()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"), // 4000m
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
		},
	}

	// Pods que alocam >90% da CPU
	pods := []corev1.Pod{
		{
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("3700m"), // 92.5%
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
	}

	health := checker.checkNode(node, pods)

	if health.Status != StatusWarning {
		t.Errorf("Status = %s, want warning (CPU >90%%)", health.Status)
	}
	if health.CPUUtilization <= 90 {
		t.Errorf("CPUUtilization = %.1f%%, deveria ser >90%%", health.CPUUtilization)
	}
}

// TestGetAffectedPods verifica lista de pods afetados
func TestGetAffectedPods(t *testing.T) {
	checker := NewNodeChecker()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "kube-system"},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodSucceeded}, // Terminal - ignorado
		},
	}

	affected := checker.getAffectedPods(pods)

	if len(affected) != 2 {
		t.Errorf("Deveria ter 2 pods afetados (ignorando Succeeded), got %d", len(affected))
	}
}

// TestGetNodeCriticalCount verifica contagem de nodes críticos
func TestGetNodeCriticalCount(t *testing.T) {
	nodes := []NodeHealth{
		{Name: "node-1", Status: StatusHealthy},
		{Name: "node-2", Status: StatusCritical},
		{Name: "node-3", Status: StatusWarning},
		{Name: "node-4", Status: StatusCritical},
	}

	count := GetNodeCriticalCount(nodes)
	if count != 2 {
		t.Errorf("Critical count = %d, want 2", count)
	}
}

// TestGetNodeWarningCount verifica contagem de nodes com warning
func TestGetNodeWarningCount(t *testing.T) {
	nodes := []NodeHealth{
		{Name: "node-1", Status: StatusHealthy},
		{Name: "node-2", Status: StatusCritical},
		{Name: "node-3", Status: StatusWarning},
		{Name: "node-4", Status: StatusWarning},
	}

	count := GetNodeWarningCount(nodes)
	if count != 2 {
		t.Errorf("Warning count = %d, want 2", count)
	}
}

// TestNodeHealthStruct verifica estrutura NodeHealth
func TestNodeHealthStruct(t *testing.T) {
	health := NodeHealth{
		Name:              "node-1",
		Status:            StatusHealthy,
		Ready:             true,
		KubeletVersion:    "v1.28.0",
		CPUUtilization:    50.5,
		MemoryUtilization: 60.0,
	}

	if health.Name != "node-1" {
		t.Errorf("Name = %s, want node-1", health.Name)
	}
	if health.CPUUtilization != 50.5 {
		t.Errorf("CPUUtilization = %.1f, want 50.5", health.CPUUtilization)
	}
}

// TestNodeResourcesStruct verifica estrutura NodeResources
func TestNodeResourcesStruct(t *testing.T) {
	nr := NodeResources{
		CPUMillis:   4000,
		MemoryBytes: 8 * 1024 * 1024 * 1024,
		Pods:        110,
		EphemeralGB: 100,
	}

	if nr.CPUMillis != 4000 {
		t.Errorf("CPUMillis = %d, want 4000", nr.CPUMillis)
	}
	if nr.Pods != 110 {
		t.Errorf("Pods = %d, want 110", nr.Pods)
	}
}
