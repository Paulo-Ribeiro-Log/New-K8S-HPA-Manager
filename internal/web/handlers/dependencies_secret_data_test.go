package handlers

import (
	"encoding/base64"
	"testing"

	"k8s-hpa-manager/internal/healthcheck"
	"k8s-hpa-manager/internal/storage"
)

// newIsolatedDependencyRegistry cria um storage.DependencyRegistry real (via NewDependencyRegistry,
// já que os campos do struct são privados e não há outro construtor), mas apontando $HOME pra um
// diretório temporário — isola completamente do banco real do usuário
// (~/.k8s-hpa-manager/dependency-registry.db). t.Setenv desfaz o override no fim do teste.
func newIsolatedDependencyRegistry(t *testing.T) *storage.DependencyRegistry {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	reg, err := storage.NewDependencyRegistry()
	if err != nil {
		t.Fatalf("failed to create isolated dependency registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg
}

// TestConvertAndPersistSecretDataEntries_ConfigMapEndToEnd reproduz o caminho REAL de produção
// (Scan → convertSecretDataEntries → ReplaceSecretDataForCluster → SearchSecretData) pra um
// ConfigMap — motivado por relato do usuário de que um valor conhecido de ConfigMap.Data não
// aparecia na busca. Testa a fiação entre os pacotes healthcheck/storage, não coberta pelos testes
// unitários de cada pacote isoladamente (que testam extractConfigMapDataEntries e SearchSecretData
// separados, nunca a conversão + persistência real entre os dois).
func TestConvertAndPersistSecretDataEntries_ConfigMapEndToEnd(t *testing.T) {
	reg := newIsolatedDependencyRegistry(t)

	decoded := "database.host=teste-valor-conhecido"
	entries := []healthcheck.SecretDataEntry{
		{
			ResourceKind: "configmap",
			Cluster:      "cluster-a",
			Namespace:    "ns-a",
			ResourceName: "minha-config",
			DataKey:      "app.properties",
			ValueDecoded: decoded,
			ValueBase64:  base64.StdEncoding.EncodeToString([]byte(decoded)),
		},
	}

	records := convertSecretDataEntries(entries)
	if len(records) != 1 {
		t.Fatalf("convertSecretDataEntries: expected 1 record, got %d", len(records))
	}
	if records[0].ResourceKind != "configmap" || records[0].ResourceName != "minha-config" {
		t.Fatalf("convertSecretDataEntries produced unexpected record: %+v", records[0])
	}

	if err := reg.ReplaceSecretDataForCluster("cluster-a", records); err != nil {
		t.Fatalf("ReplaceSecretDataForCluster failed: %v", err)
	}

	// Busca por um termo que só aparece NO MEIO do valor decodificado (caso realista de "grep
	// por um valor conhecido"), modo "value" (default da UI).
	results, err := reg.SearchSecretData("teste-valor-conhecido", "value")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result searching by value, got %d: %+v", len(results), results)
	}
	if results[0].ResourceKind != "configmap" || results[0].ResourceName != "minha-config" || results[0].DataKey != "app.properties" {
		t.Fatalf("unexpected search result: %+v", results[0])
	}

	// Busca por chave também deve funcionar.
	results, err = reg.SearchSecretData("app.properties", "key")
	if err != nil {
		t.Fatalf("SearchSecretData (key mode) failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result searching by key, got %d: %+v", len(results), results)
	}
}
