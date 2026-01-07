package healthcheck

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Helper para criar ConfigMap de teste
func createTestConfigMap(name, namespace string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}

// Helper para criar Secret de teste
func createTestSecret(name, namespace string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}

func TestConfigChecker_ValidConfigMap(t *testing.T) {
	// Arrange
	cm := createTestConfigMap("app-config", "default", map[string]string{
		"app_name":    "my-app",
		"environment": "production",
		"port":        "8080",
	})
	client := fake.NewSimpleClientset(cm)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s, got %s", StatusHealthy, health.Status)
	}
	if health.Name != "app-config" {
		t.Errorf("Expected name app-config, got %s", health.Name)
	}
	if health.ResourceType != ResourceConfigMap {
		t.Errorf("Expected resource type ConfigMap, got %s", health.ResourceType)
	}
	if !health.Exists {
		t.Error("Expected Exists to be true")
	}
}

func TestConfigChecker_EmptyConfigMap(t *testing.T) {
	// Arrange
	cm := createTestConfigMap("empty-config", "default", map[string]string{})
	client := fake.NewSimpleClientset(cm)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if health.Message != "ConfigMap vazio (sem chaves)" {
		t.Errorf("Unexpected message: %s", health.Message)
	}
	if len(health.Suggestions) == 0 {
		t.Error("Expected suggestions for empty ConfigMap")
	}
}

func TestConfigChecker_ConfigMapWithEmptyValues(t *testing.T) {
	// Arrange
	cm := createTestConfigMap("config-with-empty", "default", map[string]string{
		"valid_key": "valid_value",
		"empty_key": "",
		"space_key": "   ",
	})
	client := fake.NewSimpleClientset(cm)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if len(health.InvalidValues) != 2 {
		t.Errorf("Expected 2 invalid values, got %d: %v", len(health.InvalidValues), health.InvalidValues)
	}
}

func TestConfigChecker_ConfigMapWithInvalidConnectionString(t *testing.T) {
	// Arrange
	cm := createTestConfigMap("db-config", "default", map[string]string{
		"database_url": "mongodb://",       // Inválida - sem host
		"redis_url":    "redis://localhost", // Válida
	})
	client := fake.NewSimpleClientset(cm)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if len(health.InvalidValues) == 0 {
		t.Error("Expected invalid connection string to be detected")
	}
}

func TestConfigChecker_ValidSecret(t *testing.T) {
	// Arrange
	secret := createTestSecret("db-secret", "default", map[string][]byte{
		"username": []byte("admin"),
		"password": []byte("supersecret123"),
	})
	client := fake.NewSimpleClientset(secret)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s, got %s", StatusHealthy, health.Status)
	}
	if health.ResourceType != ResourceSecret {
		t.Errorf("Expected resource type Secret, got %s", health.ResourceType)
	}
}

func TestConfigChecker_EmptySecret(t *testing.T) {
	// Arrange
	secret := createTestSecret("empty-secret", "default", map[string][]byte{})
	client := fake.NewSimpleClientset(secret)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if health.Message != "Secret vazio (sem chaves)" {
		t.Errorf("Unexpected message: %s", health.Message)
	}
}

func TestConfigChecker_SecretWithShortCredentials(t *testing.T) {
	// Arrange
	secret := createTestSecret("weak-secret", "default", map[string][]byte{
		"password": []byte("123"),      // Muito curta
		"api_key":  []byte("validkey123"), // Válida
	})
	client := fake.NewSimpleClientset(secret)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"default"})

	// Assert
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	health := results[0]
	if health.Status != StatusWarning {
		t.Errorf("Expected status %s, got %s", StatusWarning, health.Status)
	}
	if len(health.InvalidValues) == 0 {
		t.Error("Expected short credential to be flagged")
	}
}

func TestConfigChecker_MultipleNamespaces(t *testing.T) {
	// Arrange
	cm1 := createTestConfigMap("config-ns1", "namespace1", map[string]string{"key": "value"})
	cm2 := createTestConfigMap("config-ns2", "namespace2", map[string]string{"key": "value"})
	secret1 := createTestSecret("secret-ns1", "namespace1", map[string][]byte{"password": []byte("secret123")})

	client := fake.NewSimpleClientset(cm1, cm2, secret1)
	checker := NewConfigChecker()

	// Act
	results := checker.CheckAll(context.Background(), client, []string{"namespace1", "namespace2"})

	// Assert
	if len(results) != 3 {
		t.Fatalf("Expected 3 results (2 ConfigMaps + 1 Secret), got %d", len(results))
	}

	// Verificar que temos recursos de ambos namespaces
	namespaces := make(map[string]bool)
	for _, r := range results {
		namespaces[r.Namespace] = true
	}
	if len(namespaces) != 2 {
		t.Errorf("Expected resources from 2 namespaces, got %d", len(namespaces))
	}
}

func TestConfigChecker_LooksLikeConnectionString(t *testing.T) {
	checker := NewConfigChecker()

	tests := []struct {
		key      string
		value    string
		expected bool
	}{
		{"database_url", "mongodb://localhost:27017", true},
		{"redis_url", "redis://redis-svc:6379", true},
		{"postgres_conn", "postgresql://user:pass@host:5432/db", true},
		{"mongo_uri", "mongodb+srv://cluster.mongodb.net", true},
		{"eventhub", "Endpoint=sb://myhub.servicebus.windows.net/", true},
		{"http_endpoint", "https://api.example.com", true},
		{"regular_config", "some_value", false},
		{"port", "8080", false},
		{"app_name", "my-app", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := checker.looksLikeConnectionString(tt.key, tt.value)
			if result != tt.expected {
				t.Errorf("looksLikeConnectionString(%s, %s) = %v, expected %v", tt.key, tt.value, result, tt.expected)
			}
		})
	}
}

func TestConfigChecker_IsValidConnectionString(t *testing.T) {
	checker := NewConfigChecker()

	tests := []struct {
		value    string
		expected bool
	}{
		{"mongodb://localhost:27017/mydb", true},
		{"mongodb+srv://cluster.mongodb.net/db", true},
		{"redis://localhost:6379", true},
		{"postgresql://user:pass@host:5432/db", true},
		{"https://api.example.com/endpoint", true},
		{"Endpoint=sb://myhub.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey", true},
		{"mongodb://", false},                    // Sem host
		{"redis://   ", false},                   // Host vazio
		{"invalid", false},                       // Sem protocolo
		{"http", false},                          // Muito curta
		{"ftp://unsupported.com", false},         // Protocolo não suportado
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			result := checker.isValidConnectionString(tt.value)
			if result != tt.expected {
				t.Errorf("isValidConnectionString(%s) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestConfigChecker_IsAuthenticationKey(t *testing.T) {
	checker := NewConfigChecker()

	tests := []struct {
		key      string
		expected bool
	}{
		{"password", true},
		{"db_password", true},
		{"api_key", true},
		{"apikey", true},
		{"secret_token", true},
		{"auth_token", true},
		{"credential", true},
		{"username", false},
		{"database_url", false},
		{"port", false},
		{"app_name", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := checker.isAuthenticationKey(tt.key)
			if result != tt.expected {
				t.Errorf("isAuthenticationKey(%s) = %v, expected %v", tt.key, result, tt.expected)
			}
		})
	}
}
