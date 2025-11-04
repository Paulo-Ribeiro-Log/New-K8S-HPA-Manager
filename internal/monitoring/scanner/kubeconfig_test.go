package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		s       string
		pattern string
		want    bool
	}{
		// Wildcard *
		{"anything", "*", true},

		// Prefixo*
		{"akspriv-api-prd-admin", "akspriv-*", true},
		{"akspriv-api-prd-admin", "other-*", false},

		// *Sufixo
		{"akspriv-api-prd-admin", "*-prd-admin", true},
		{"akspriv-api-hlg-admin", "*-prd-admin", false},
		{"akspriv-payment-prd-admin", "*-prd-admin", true},

		// *texto*
		{"akspriv-api-prd-admin", "*-api-*", true},
		{"akspriv-payment-prd-admin", "*-api-*", false},

		// Exato (sem *)
		{"exact-match", "exact-match", true},
		{"exact-match", "different", false},
	}

	for _, tt := range tests {
		got := matchPattern(tt.s, tt.pattern)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.s, tt.pattern, got, tt.want)
		}
	}
}

func TestFilterClustersByPattern(t *testing.T) {
	clusters := []string{
		"akspriv-api-prd-admin",
		"akspriv-payment-prd-admin",
		"akspriv-faturamento-prd-admin",
		"akspriv-api-hlg-admin",
		"akspriv-payment-hlg-admin",
		"other-cluster",
	}

	tests := []struct {
		pattern  string
		expected int
	}{
		{"*-prd-admin", 3},
		{"*-hlg-admin", 2},
		{"akspriv-*", 5},
		{"*-api-*", 2},
		{"*", 6},
		{"", 6}, // Sem padrão retorna todos
	}

	for _, tt := range tests {
		result := FilterClustersByPattern(clusters, tt.pattern)
		if len(result) != tt.expected {
			t.Errorf("FilterClustersByPattern(clusters, %q) retornou %d clusters, esperado %d",
				tt.pattern, len(result), tt.expected)
		}
	}
}

func TestLoadClustersFromKubeconfig(t *testing.T) {
	// Cria kubeconfig temporário para teste
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "config")

	// Conteúdo de kubeconfig de teste
	kubeconfigContent := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://cluster1.example.com
  name: cluster1
- cluster:
    server: https://cluster2.example.com
  name: cluster2
contexts:
- context:
    cluster: cluster1
    user: user1
  name: context1
- context:
    cluster: cluster2
    user: user2
  name: context2
current-context: context1
users:
- name: user1
  user:
    token: token1
- name: user2
  user:
    token: token2
`

	// Escreve kubeconfig temporário
	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test kubeconfig: %v", err)
	}

	// Define KUBECONFIG para apontar para arquivo temporário
	oldKubeconfig := os.Getenv("KUBECONFIG")
	os.Setenv("KUBECONFIG", kubeconfigPath)
	defer os.Setenv("KUBECONFIG", oldKubeconfig)

	// Testa carregamento
	clusters, err := LoadClustersFromKubeconfig()
	if err != nil {
		t.Fatalf("LoadClustersFromKubeconfig() error = %v", err)
	}

	// Deve ter 2 contextos
	if len(clusters) != 2 {
		t.Errorf("Expected 2 clusters, got %d", len(clusters))
	}

	// Verifica nomes dos contextos
	expectedContexts := map[string]bool{
		"context1": false,
		"context2": false,
	}

	for _, cluster := range clusters {
		if _, ok := expectedContexts[cluster]; ok {
			expectedContexts[cluster] = true
		}
	}

	for context, found := range expectedContexts {
		if !found {
			t.Errorf("Expected context %q not found in results", context)
		}
	}
}

func TestLoadConfigForEnvironment(t *testing.T) {
	// Cria kubeconfig temporário
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "config")

	kubeconfigContent := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://cluster1.example.com
  name: cluster1
contexts:
- context:
    cluster: cluster1
    user: user1
  name: akspriv-api-prd-admin
- context:
    cluster: cluster1
    user: user1
  name: akspriv-payment-prd-admin
- context:
    cluster: cluster1
    user: user1
  name: akspriv-api-hlg-admin
current-context: akspriv-api-prd-admin
users:
- name: user1
  user:
    token: token1
`

	err := os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test kubeconfig: %v", err)
	}

	oldKubeconfig := os.Getenv("KUBECONFIG")
	os.Setenv("KUBECONFIG", kubeconfigPath)
	defer os.Setenv("KUBECONFIG", oldKubeconfig)

	// Testa PRD
	config, err := LoadConfigForEnvironment(EnvironmentPRD)
	if err != nil {
		t.Fatalf("LoadConfigForEnvironment(PRD) error = %v", err)
	}

	if config.Mode != ScanModeFull {
		t.Errorf("Expected mode ScanModeFull, got %v", config.Mode)
	}

	if config.Environment != EnvironmentPRD {
		t.Errorf("Expected environment PRD, got %v", config.Environment)
	}

	// Deve ter 2 targets PRD
	if len(config.Targets) != 2 {
		t.Errorf("Expected 2 PRD targets, got %d", len(config.Targets))
	}

	// Testa HLG
	config, err = LoadConfigForEnvironment(EnvironmentHLG)
	if err != nil {
		t.Fatalf("LoadConfigForEnvironment(HLG) error = %v", err)
	}

	if config.Environment != EnvironmentHLG {
		t.Errorf("Expected environment HLG, got %v", config.Environment)
	}

	// Deve ter 1 target HLG
	if len(config.Targets) != 1 {
		t.Errorf("Expected 1 HLG target, got %d", len(config.Targets))
	}
}
