package healthcheck

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestNewConfigCrossRefChecker verifica criação do checker
func TestNewConfigCrossRefChecker(t *testing.T) {
	checker := NewConfigCrossRefChecker()
	if checker == nil {
		t.Fatal("NewConfigCrossRefChecker retornou nil")
	}
	if checker.systemNamespaces == nil {
		t.Error("systemNamespaces deveria estar inicializado")
	}
	if !checker.systemNamespaces["kube-system"] {
		t.Error("kube-system deveria estar nos namespaces de sistema")
	}
}

// TestBuildReferenceMapConfigMapVolume verifica detecção de ConfigMap em volume
func TestBuildReferenceMapConfigMapVolume(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "config-vol",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
							},
						},
					},
				},
			},
		},
	}

	cmRefs, _ := checker.buildReferenceMap(pods)

	if len(cmRefs["my-config"]) != 1 {
		t.Errorf("my-config deveria ter 1 referência, got %d", len(cmRefs["my-config"]))
	}
}

// TestBuildReferenceMapSecretVolume verifica detecção de Secret em volume
func TestBuildReferenceMapSecretVolume(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "secret-vol",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "my-secret",
							},
						},
					},
				},
			},
		},
	}

	_, secretRefs := checker.buildReferenceMap(pods)

	if len(secretRefs["my-secret"]) != 1 {
		t.Errorf("my-secret deveria ter 1 referência, got %d", len(secretRefs["my-secret"]))
	}
}

// TestBuildReferenceMapEnvFrom verifica detecção de envFrom
func TestBuildReferenceMapEnvFrom(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						EnvFrom: []corev1.EnvFromSource{
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "env-config"},
								},
							},
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "env-secret"},
								},
							},
						},
					},
				},
			},
		},
	}

	cmRefs, secretRefs := checker.buildReferenceMap(pods)

	if len(cmRefs["env-config"]) != 1 {
		t.Errorf("env-config deveria ter 1 referência, got %d", len(cmRefs["env-config"]))
	}
	if len(secretRefs["env-secret"]) != 1 {
		t.Errorf("env-secret deveria ter 1 referência, got %d", len(secretRefs["env-secret"]))
	}
}

// TestBuildReferenceMapEnvValueFrom verifica detecção de env.valueFrom
func TestBuildReferenceMapEnvValueFrom(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						Env: []corev1.EnvVar{
							{
								Name: "DB_HOST",
								ValueFrom: &corev1.EnvVarSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "db-config"},
										Key:                  "host",
									},
								},
							},
							{
								Name: "DB_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "db-secret"},
										Key:                  "password",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	cmRefs, secretRefs := checker.buildReferenceMap(pods)

	if len(cmRefs["db-config"]) != 1 {
		t.Errorf("db-config deveria ter 1 referência")
	}
	if len(secretRefs["db-secret"]) != 1 {
		t.Errorf("db-secret deveria ter 1 referência")
	}
}

// TestCheckConfigMapOrphan verifica detecção de ConfigMap órfão
func TestCheckConfigMapOrphan(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orphan-config",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Data: map[string]string{"key": "value"},
	}

	health := checker.checkConfigMap(cm, []string{}) // Sem referências

	if !health.IsOrphan {
		t.Error("Deveria ser detectado como órfão")
	}
	if health.Status != StatusWarning {
		t.Errorf("Status = %s, want warning", health.Status)
	}
}

// TestCheckConfigMapReferenced verifica ConfigMap referenciado
func TestCheckConfigMapReferenced(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "used-config",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Data: map[string]string{"key": "value"},
	}

	health := checker.checkConfigMap(cm, []string{"default/pod-1", "default/pod-2"})

	if health.IsOrphan {
		t.Error("Não deveria ser órfão")
	}
	if health.Status != StatusHealthy {
		t.Errorf("Status = %s, want healthy", health.Status)
	}
	if len(health.ReferencedBy) != 2 {
		t.Errorf("ReferencedBy = %d, want 2", len(health.ReferencedBy))
	}
}

// TestCheckSecretOrphan verifica detecção de Secret órfão
func TestCheckSecretOrphan(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "orphan-secret",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Data: map[string][]byte{"key": []byte("value")},
	}

	health := checker.checkSecret(secret, []string{})

	if !health.IsOrphan {
		t.Error("Deveria ser detectado como órfão")
	}
	if health.Status != StatusWarning {
		t.Errorf("Status = %s, want warning", health.Status)
	}
}

// TestCheckMissingReferences verifica detecção de referências faltando
func TestCheckMissingReferences(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "config-vol",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "missing-config"},
							},
						},
					},
				},
			},
		},
	}

	configMaps := []corev1.ConfigMap{} // ConfigMap não existe
	secrets := []corev1.Secret{}

	missing := checker.checkMissingReferences(pods, configMaps, secrets, "default")

	if len(missing) != 1 {
		t.Errorf("Deveria ter 1 referência faltando, got %d", len(missing))
	}
	if len(missing) > 0 && missing[0].Name != "missing-config" {
		t.Errorf("Missing ref name = %s, want missing-config", missing[0].Name)
	}
}

// TestCheckMissingReferencesOptional verifica que referências opcionais são ignoradas
func TestCheckMissingReferencesOptional(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	optional := true
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "config-vol",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "optional-config"},
								Optional:             &optional,
							},
						},
					},
				},
			},
		},
	}

	configMaps := []corev1.ConfigMap{} // ConfigMap não existe, mas é opcional
	secrets := []corev1.Secret{}

	missing := checker.checkMissingReferences(pods, configMaps, secrets, "default")

	if len(missing) != 0 {
		t.Errorf("Não deveria ter referências faltando (é opcional), got %d", len(missing))
	}
}

// TestIsSystemResource verifica detecção de recursos de sistema
func TestIsSystemResource(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	tests := []struct {
		name     string
		expected bool
	}{
		{"kube-root-ca.crt", true},
		{"istio-ca-secret", true},
		{"default-token-abc123", true},
		{"sh.helm.release.v1", true},
		{"my-app-config", false},
		{"database-secret", false},
	}

	for _, tt := range tests {
		result := checker.isSystemResource(tt.name)
		if result != tt.expected {
			t.Errorf("isSystemResource(%s) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

// TestIsSystemSecret verifica detecção de secrets de sistema
func TestIsSystemSecret(t *testing.T) {
	checker := NewConfigCrossRefChecker()

	// Secret de service account
	saSecret := &corev1.Secret{
		Type: corev1.SecretTypeServiceAccountToken,
	}
	if !checker.isSystemSecret(saSecret) {
		t.Error("ServiceAccountToken deveria ser secret de sistema")
	}

	// Secret normal
	normalSecret := &corev1.Secret{
		Type: corev1.SecretTypeOpaque,
	}
	if checker.isSystemSecret(normalSecret) {
		t.Error("Secret Opaque normal não deveria ser de sistema")
	}

	// Secret TLS do cert-manager
	certManagerSecret := &corev1.Secret{
		Type: corev1.SecretTypeTLS,
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"cert-manager.io/certificate-name": "my-cert",
			},
		},
	}
	if !checker.isSystemSecret(certManagerSecret) {
		t.Error("Secret TLS do cert-manager deveria ser de sistema")
	}
}

// TestGetOrphanCount verifica contagem de órfãos
func TestGetOrphanCount(t *testing.T) {
	configs := []ConfigCrossRefHealth{
		{Name: "config-1", IsOrphan: false},
		{Name: "config-2", IsOrphan: true},
		{Name: "config-3", IsOrphan: true},
		{Name: "config-4", IsOrphan: false},
	}

	count := GetOrphanCount(configs)
	if count != 2 {
		t.Errorf("Orphan count = %d, want 2", count)
	}
}

// TestGetMissingRefCount verifica contagem de referências faltando
func TestGetMissingRefCount(t *testing.T) {
	missing := []MissingReference{
		{Name: "config-1"},
		{Name: "secret-1"},
		{Name: "config-2"},
	}

	count := GetMissingRefCount(missing)
	if count != 3 {
		t.Errorf("Missing ref count = %d, want 3", count)
	}
}

// TestConfigCrossRefHealthStruct verifica estrutura
func TestConfigCrossRefHealthStruct(t *testing.T) {
	health := ConfigCrossRefHealth{
		Name:         "my-config",
		Namespace:    "default",
		ResourceType: "ConfigMap",
		Status:       StatusHealthy,
		IsOrphan:     false,
		KeyCount:     5,
	}

	if health.Name != "my-config" {
		t.Errorf("Name = %s, want my-config", health.Name)
	}
	if health.ResourceType != "ConfigMap" {
		t.Errorf("ResourceType = %s, want ConfigMap", health.ResourceType)
	}
}

// TestMissingReferenceStruct verifica estrutura
func TestMissingReferenceStruct(t *testing.T) {
	missing := MissingReference{
		ResourceType: "Secret",
		Name:         "db-secret",
		Namespace:    "production",
		ReferencedBy: "production/api-pod",
		RefType:      "env",
	}

	if missing.ResourceType != "Secret" {
		t.Errorf("ResourceType = %s, want Secret", missing.ResourceType)
	}
	if missing.RefType != "env" {
		t.Errorf("RefType = %s, want env", missing.RefType)
	}
}
