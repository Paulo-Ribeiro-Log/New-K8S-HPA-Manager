package handlers

import (
	"strings"
	"testing"

	"k8s-hpa-manager/internal/models"
)

// O histórico é somente-inclusão: o YAML do Secret (com os valores em base64) não pode entrar.
func TestSecretManifestToHistoryMapNaoGravaValores(t *testing.T) {
	yamlSecret := "apiVersion: v1\nkind: Secret\ndata:\n  password: c3VwZXItc2VjcmV0bw==\n"
	m := secretManifestToHistoryMap(&models.SecretManifest{
		YAML:     yamlSecret,
		Metadata: models.SecretMetadata{ResourceVersion: "42"},
	})
	for k, v := range m {
		if s, ok := v.(string); ok && strings.Contains(s, "c3VwZXItc2VjcmV0bw==") {
			t.Fatalf("campo %q guarda o valor do Secret", k)
		}
	}
	if _, ok := m["yaml"]; ok {
		t.Fatal("o campo yaml não deve existir")
	}
	if m["yaml_sha256"] == "" || m["yaml_bytes"] != len(yamlSecret) || m["resourceVersion"] != "42" {
		t.Fatalf("identificação incompleta: %v", m)
	}
	if secretManifestToHistoryMap(nil) != nil {
		t.Fatal("nil deveria continuar nil")
	}
}
