package healthcheck

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPVChecker_BoundPVC(t *testing.T) {
	// Arrange: PVC bound com PV existente
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "pv-data",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-data",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
		Provisioner: "kubernetes.io/no-provisioner",
	}

	client := fake.NewSimpleClientset(pvc, pv, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusHealthy {
		t.Errorf("Expected StatusHealthy, got %s", result.Status)
	}
	if !result.IsBound {
		t.Error("Expected IsBound to be true")
	}
	if !result.VolumeExists {
		t.Error("Expected VolumeExists to be true")
	}
	if !result.StorageClassExists {
		t.Error("Expected StorageClassExists to be true")
	}
}

func TestPVChecker_PendingPVC(t *testing.T) {
	// Arrange: PVC pending há mais de 5 minutos
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pending-pvc",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-10 * time.Minute)}, // Criado há 10 min
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusCritical {
		t.Errorf("Expected StatusCritical (pending >5min), got %s", result.Status)
	}
	if !result.IsPending {
		t.Error("Expected IsPending to be true")
	}

	// Deve ter 2 issues: uma de warning (pending) e uma critical (pending >5min)
	if len(result.Issues) < 2 {
		t.Errorf("Expected at least 2 issues, got %d", len(result.Issues))
	}
}

func TestPVChecker_LostPVC(t *testing.T) {
	// Arrange: PVC em estado Lost
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lost-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "deleted-pv",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimLost,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusCritical {
		t.Errorf("Expected StatusCritical for Lost PVC, got %s", result.Status)
	}

	// Verificar se tem issue de Lost
	hasLostIssue := false
	for _, issue := range result.Issues {
		if issue.Type == "status" && issue.Severity == "critical" {
			hasLostIssue = true
			break
		}
	}
	if !hasLostIssue {
		t.Error("Expected a critical status issue for Lost PVC")
	}
}

func TestPVChecker_MissingStorageClass(t *testing.T) {
	// Arrange: PVC referenciando StorageClass que não existe
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-sc-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("nonexistent-class"),
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	client := fake.NewSimpleClientset(pvc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.StorageClassExists {
		t.Error("Expected StorageClassExists to be false")
	}

	// Deve ter issue crítico sobre StorageClass
	hasSCIssue := false
	for _, issue := range result.Issues {
		if issue.Type == "config" && issue.Severity == "critical" {
			hasSCIssue = true
			break
		}
	}
	if !hasSCIssue {
		t.Error("Expected a critical config issue for missing StorageClass")
	}
}

func TestPVChecker_MissingPV(t *testing.T) {
	// Arrange: PVC bound mas PV não existe (cenário inconsistente)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "deleted-pv",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.VolumeExists {
		t.Error("Expected VolumeExists to be false")
	}
	if result.Status != StatusCritical {
		t.Errorf("Expected StatusCritical for missing PV, got %s", result.Status)
	}
}

func TestPVChecker_ReadWriteMany(t *testing.T) {
	// Arrange: PVC com ReadWriteMany (warning sobre compatibilidade)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rwx-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "rwx-pv",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rwx-pv",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, pv, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning for ReadWriteMany, got %s", result.Status)
	}

	// Deve ter warning sobre ReadWriteMany
	hasRWXIssue := false
	for _, issue := range result.Issues {
		if issue.Type == "config" && issue.Severity == "warning" {
			hasRWXIssue = true
			break
		}
	}
	if !hasRWXIssue {
		t.Error("Expected a warning issue for ReadWriteMany access mode")
	}
}

func TestPVChecker_SmallCapacity(t *testing.T) {
	// Arrange: PVC com capacidade muito pequena (<1Gi)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "small-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "small-pv",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("500Mi"), // < 1Gi
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "small-pv",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("500Mi"),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, pv, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning for small capacity, got %s", result.Status)
	}

	// Deve ter warning sobre capacidade pequena
	hasSmallIssue := false
	for _, issue := range result.Issues {
		if issue.Type == "config" && issue.Severity == "warning" {
			hasSmallIssue = true
			break
		}
	}
	if !hasSmallIssue {
		t.Error("Expected a warning issue for small capacity")
	}
}

func TestPVChecker_DeleteReclaimPolicy(t *testing.T) {
	// Arrange: PVC bound com PV com ReclaimPolicy=Delete
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-policy-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "delete-pv",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "delete-pv",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, // WARNING: dados perdidos se PVC deletado
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, pv, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning for Delete reclaim policy, got %s", result.Status)
	}

	if result.ReclaimPolicy != "Delete" {
		t.Errorf("Expected ReclaimPolicy 'Delete', got '%s'", result.ReclaimPolicy)
	}
}

func TestPVChecker_SystemPVCFiltered(t *testing.T) {
	// Arrange: PVC em namespace de sistema
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd-data",
			Namespace: "kube-system", // Namespace de sistema
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	client := fake.NewSimpleClientset(pvc)
	checker := NewPVChecker()

	// Act: Com filtros habilitados, deve ignorar PVCs de sistema
	results := checker.CheckAll(context.Background(), client, []string{"kube-system"}, 30, true, nil)

	// Assert
	if len(results) != 0 {
		t.Errorf("Expected 0 results (system PVC filtered), got %d", len(results))
	}
}

func TestPVChecker_CheckAll(t *testing.T) {
	// Arrange: Múltiplos PVCs com diferentes status
	pvcHealthy := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-pvc",
			Namespace: "app-1",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "pv-healthy",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pvcWarning := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "warning-pvc",
			Namespace: "app-2",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "pv-warning",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, // RWX = warning
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pvHealthy := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-healthy",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	pvWarning := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-warning",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvcHealthy, pvcWarning, pvHealthy, pvWarning, sc)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"app-1", "app-2"}, 30, false, nil)

	// Assert
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Verificar que temos pelo menos um healthy e um warning
	healthyCount := 0
	warningCount := 0
	for _, r := range results {
		if r.Status == StatusHealthy {
			healthyCount++
		}
		if r.Status == StatusWarning {
			warningCount++
		}
	}

	if healthyCount != 1 {
		t.Errorf("Expected 1 healthy PVC, got %d", healthyCount)
	}
	if warningCount != 1 {
		t.Errorf("Expected 1 warning PVC, got %d", warningCount)
	}
}

func TestPVChecker_ProgressCallback(t *testing.T) {
	// Arrange
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("standard"),
			VolumeName:       "test-pv",
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
		},
	}

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "standard",
		},
	}

	client := fake.NewSimpleClientset(pvc, pv, sc)
	checker := NewPVChecker()

	// Track callback invocations
	callbackCount := 0
	callback := func(namespace, name, message string, status HealthStatus, current, total int) {
		callbackCount++
		if namespace != "default" {
			t.Errorf("Expected namespace 'default', got '%s'", namespace)
		}
		if name != "test-pvc" {
			t.Errorf("Expected name 'test-pvc', got '%s'", name)
		}
	}

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, callback)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if callbackCount < 2 { // At least 2 calls: validating + result
		t.Errorf("Expected at least 2 callback invocations, got %d", callbackCount)
	}
}

func TestPVChecker_NoStorageClass(t *testing.T) {
	// Arrange: PVC sem StorageClass definida (usa default)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-sc-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			// StorageClassName não definido
			VolumeName:  "no-sc-pv",
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "no-sc-pv",
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
		},
	}

	client := fake.NewSimpleClientset(pvc, pv)
	checker := NewPVChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"}, 30, false, nil)

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Status != StatusWarning {
		t.Errorf("Expected StatusWarning for missing StorageClass, got %s", result.Status)
	}

	// Deve ter issue sobre StorageClass não definida
	hasIssue := false
	for _, issue := range result.Issues {
		if issue.Type == "config" && issue.Severity == "warning" {
			hasIssue = true
			break
		}
	}
	if !hasIssue {
		t.Error("Expected a warning issue for undefined StorageClass")
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
