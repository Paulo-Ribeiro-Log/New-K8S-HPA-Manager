package storage

import (
	"database/sql"
	"testing"
)

// newTestDependencyRegistryWithLegacySchema recria o schema EXATO de antes da generalização pra
// ConfigMaps (secret_name TEXT NOT NULL, sem resource_kind/resource_name/resource_subtype) e então
// chama initSchema() por cima — reproduz o cenário real que causou o bug "NOT NULL constraint
// failed: secret_data_entries.secret_name" em bancos já existentes (confirmado ao vivo contra
// ~/.k8s-hpa-manager/dependency-registry.db de um ambiente com 23 clusters escaneados antes da
// mudança: 100% dos scans passaram a falhar silenciosamente, ReplaceSecretDataForCluster fazendo
// rollback do DELETE+INSERT inteiro a cada tentativa, deixando a tabela congelada com dados
// obsoletos). Os outros helpers deste arquivo (newTestDependencyRegistry) sempre criam a tabela do
// zero já com o schema novo, então nunca exercitam esse caminho de migração.
func newTestDependencyRegistryWithLegacySchema(t *testing.T) *DependencyRegistry {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	legacySchema := `
	CREATE TABLE secret_data_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cluster TEXT NOT NULL,
		namespace TEXT NOT NULL,
		secret_name TEXT NOT NULL,
		secret_type TEXT DEFAULT '',
		data_key TEXT NOT NULL,
		value_base64 TEXT NOT NULL,
		value_decoded TEXT DEFAULT '',
		is_binary INTEGER DEFAULT 0,
		truncated INTEGER DEFAULT 0,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}
	// Simula dados reais já persistidos por um scan anterior (schema antigo).
	if _, err := db.Exec(
		`INSERT INTO secret_data_entries (cluster, namespace, secret_name, secret_type, data_key, value_base64, value_decoded) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"cluster-legado", "ns-a", "secret-antigo", "Opaque", "k1", "eA==", "x",
	); err != nil {
		t.Fatalf("failed to seed legacy row: %v", err)
	}

	r := &DependencyRegistry{db: db}
	if err := r.initSchema(); err != nil {
		t.Fatalf("initSchema (migration from legacy) failed: %v", err)
	}
	return r
}

// TestInitSchema_MigratesLegacySecretDataSchema reproduz o bug real: um banco com o schema antigo
// (secret_name NOT NULL) passando por initSchema() precisa terminar num estado onde
// ReplaceSecretDataForCluster funciona — sem "NOT NULL constraint failed" — inclusive pra
// ConfigMaps (resource_kind="configmap"), que é justamente o caso que ficava invisível: a falha
// batia igual pra secret e configmap, mas só configmap não tinha nenhum dado antigo pra mascarar o
// sintoma.
func TestInitSchema_MigratesLegacySecretDataSchema(t *testing.T) {
	r := newTestDependencyRegistryWithLegacySchema(t)

	if r.hasLegacySecretDataSchema() {
		t.Fatalf("hasLegacySecretDataSchema still true after initSchema() migration")
	}

	err := r.ReplaceSecretDataForCluster("cluster-a", []SecretDataRecord{
		{
			ResourceKind: "secret", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-secret",
			DataKey: "k1", ValueBase64: "eA==", ValueDecoded: "x",
		},
		{
			ResourceKind: "configmap", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-config",
			DataKey: "app.properties", ValueBase64: "dGVzdGU=", ValueDecoded: "teste",
		},
	})
	if err != nil {
		t.Fatalf("ReplaceSecretDataForCluster failed after migration (this is the exact bug reported: %q): %v",
			"NOT NULL constraint failed: secret_data_entries.secret_name", err)
	}

	results, err := r.SearchSecretData("teste", "value")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 1 || results[0].ResourceKind != "configmap" {
		t.Fatalf("expected 1 configmap result after migration, got %+v", results)
	}
}

// newTestDependencyRegistry cria um DependencyRegistry sobre SQLite em memória — nunca toca no
// arquivo real do usuário (~/.k8s-hpa-manager/dependency-registry.db). Mesmo padrão de
// newTestRegistry em deployment_registry_test.go.
func newTestDependencyRegistry(t *testing.T) *DependencyRegistry {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	r := &DependencyRegistry{db: db}
	if err := r.initSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return r
}

func seedSecretData(t *testing.T, r *DependencyRegistry, cluster string, entries []SecretDataRecord) {
	t.Helper()
	if err := r.ReplaceSecretDataForCluster(cluster, entries); err != nil {
		t.Fatalf("ReplaceSecretDataForCluster failed: %v", err)
	}
}

func TestSearchSecretData_KeyMode(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{
			Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-secret",
			DataKey: "testeteste", ValueBase64: "dGVzdGU=", ValueDecoded: "teste",
		},
		{
			Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "outra-secret",
			DataKey: "DB_PASSWORD", ValueBase64: "c2VuaGE=", ValueDecoded: "senha",
		},
	})

	results, err := r.SearchSecretData("testeteste", "key")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 1 || results[0].DataKey != "testeteste" {
		t.Fatalf("expected 1 result matching key 'testeteste', got %+v", results)
	}

	// Busca por chave não deve casar com o VALOR — "teste" não é substring de nenhuma chave aqui.
	results, err = r.SearchSecretData("teste", "key")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 1 {
		// "testeteste" contém "teste" como substring do nome da CHAVE — isso é esperado (case 1
		// só, não "senha"/"DB_PASSWORD").
		t.Fatalf("expected 1 result (key substring match), got %d: %+v", len(results), results)
	}
}

func TestSearchSecretData_ValueMode_DecodedSubstring(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{
			Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "conn-string",
			DataKey: "DATABASE_URL", ValueBase64: "cG9zdGdyZXM6Ly90ZXN0ZUBob3N0",
			ValueDecoded: "postgres://teste@host",
		},
	})

	// "teste" aparece NO MEIO do valor decodificado — só um match confiável de substring
	// (não-alinhado a 3 bytes) encontra isso; é exatamente o caso que a comparação ingênua
	// "codificar o termo e procurar no base64 inteiro" perderia.
	results, err := r.SearchSecretData("teste", "value")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 1 || results[0].DataKey != "DATABASE_URL" {
		t.Fatalf("expected 1 result via decoded substring match, got %+v", results)
	}
}

func TestSearchSecretData_ValueMode_Base64FullMatch(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{
			Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-secret",
			DataKey: "testeteste", ValueBase64: "dGVzdGU=", ValueDecoded: "teste",
		},
	})

	// Buscar diretamente pela forma já codificada em base64 também deve encontrar (bate contra
	// value_base64 mesmo sem decodificar).
	results, err := r.SearchSecretData("dGVzdGU=", "value")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 1 || results[0].DataKey != "testeteste" {
		t.Fatalf("expected 1 result via base64 match, got %+v", results)
	}
}

func TestSearchSecretData_ValueMode_DoesNotMatchKeyOnly(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{
			Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-secret",
			DataKey: "senha-do-banco", ValueBase64: "eHl6", ValueDecoded: "xyz",
		},
	})

	// "senha" só aparece na CHAVE, não no valor — modo "value" não deve casar.
	results, err := r.SearchSecretData("senha", "value")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results (term only present in key, not value), got %+v", results)
	}
}

func TestSearchSecretData_Wildcard(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s1", DataKey: "rds-password", ValueBase64: "eA==", ValueDecoded: "x"},
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s2", DataKey: "redis-password", ValueBase64: "eQ==", ValueDecoded: "y"},
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s3", DataKey: "other", ValueBase64: "eg==", ValueDecoded: "z"},
	})

	results, err := r.SearchSecretData("*password", "key")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for wildcard '*password', got %d: %+v", len(results), results)
	}
}

func TestReplaceSecretDataForCluster_SnapshotSemantics(t *testing.T) {
	r := newTestDependencyRegistry(t)

	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s1", DataKey: "k1", ValueBase64: "eA==", ValueDecoded: "x"},
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s2", DataKey: "k2", ValueBase64: "eQ==", ValueDecoded: "y"},
	})
	seedSecretData(t, r, "cluster-b", []SecretDataRecord{
		{Cluster: "cluster-b", Namespace: "ns-b", ResourceName: "s3", DataKey: "k3", ValueBase64: "eg==", ValueDecoded: "z"},
	})

	// Novo scan de cluster-a com só 1 entrada (a outra secret/chave foi removida do cluster) —
	// deve SUBSTITUIR, não acumular.
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s1", DataKey: "k1", ValueBase64: "eA==", ValueDecoded: "x"},
	})

	resultsA, err := r.SearchSecretData("k*", "key")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	// k1 (cluster-a, ainda existe) + k3 (cluster-b, nunca tocado) = 2. k2 (cluster-a, removido no
	// segundo scan) não deve aparecer mais.
	if len(resultsA) != 2 {
		t.Fatalf("expected 2 results after replace (k1 + k3, k2 gone), got %d: %+v", len(resultsA), resultsA)
	}
	for _, rec := range resultsA {
		if rec.DataKey == "k2" {
			t.Fatalf("k2 should have been removed by ReplaceSecretDataForCluster snapshot semantics, got %+v", resultsA)
		}
	}
}

func TestClearSecretData(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "s1", DataKey: "k1", ValueBase64: "eA==", ValueDecoded: "x"},
	})

	if err := r.ClearSecretData(); err != nil {
		t.Fatalf("ClearSecretData failed: %v", err)
	}

	results, err := r.SearchSecretData("k1", "key")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after ClearSecretData, got %+v", results)
	}
}

// TestSearchSecretData_MixedResourceKinds confirma que a busca ampliada pra ConfigMaps retorna
// resultados de Secret E ConfigMap juntos na mesma chamada, com resource_kind distinguindo a
// origem — sem filtro por tipo de recurso (pedido do usuário foi "amplie a busca incluindo
// configmaps", não "adicione outro seletor").
func TestSearchSecretData_MixedResourceKinds(t *testing.T) {
	r := newTestDependencyRegistry(t)
	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{
			ResourceKind: "secret", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-secret",
			ResourceSubtype: "Opaque", DataKey: "testeteste", ValueBase64: "dGVzdGU=", ValueDecoded: "teste",
		},
		{
			ResourceKind: "configmap", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "minha-config",
			DataKey: "app.properties", ValueBase64: "dGVzdGU=", ValueDecoded: "teste",
		},
	})

	results, err := r.SearchSecretData("teste", "value")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 secret + 1 configmap), got %d: %+v", len(results), results)
	}

	kinds := map[string]bool{}
	for _, rec := range results {
		kinds[rec.ResourceKind] = true
		if rec.ResourceKind == "configmap" && rec.ResourceSubtype != "" {
			t.Errorf("expected empty ResourceSubtype for configmap entry, got %q", rec.ResourceSubtype)
		}
	}
	if !kinds["secret"] || !kinds["configmap"] {
		t.Fatalf("expected both 'secret' and 'configmap' kinds present, got %+v", results)
	}
}

// TestReplaceSecretDataForResource_ScopedReplace confirma o comportamento usado pelo refresh
// pontual pós-Resync AKV: substitui só as linhas do recurso indicado (cluster+namespace+
// resource_kind+resource_name), sem tocar em outras chaves do MESMO cluster nem em outros
// recursos — ao contrário de ReplaceSecretDataForCluster, que apaga o cluster inteiro.
func TestReplaceSecretDataForResource_ScopedReplace(t *testing.T) {
	r := newTestDependencyRegistry(t)

	seedSecretData(t, r, "cluster-a", []SecretDataRecord{
		{ResourceKind: "secret", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "akv-ns-a", DataKey: "DB_PASSWORD", ValueBase64: "eA==", ValueDecoded: "old-value"},
		{ResourceKind: "secret", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "outra-secret", DataKey: "k1", ValueBase64: "eQ==", ValueDecoded: "intocado"},
		{ResourceKind: "configmap", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "akv-ns-a", DataKey: "k2", ValueBase64: "eg==", ValueDecoded: "outro-kind"},
	})

	// Simula o resultado de uma releitura ao vivo com o valor já atualizado pelo external-secrets.
	err := r.ReplaceSecretDataForResource("cluster-a", "ns-a", "secret", "akv-ns-a", []SecretDataRecord{
		{ResourceKind: "secret", Cluster: "cluster-a", Namespace: "ns-a", ResourceName: "akv-ns-a", DataKey: "DB_PASSWORD", ValueBase64: "bmV3", ValueDecoded: "new-value"},
	})
	if err != nil {
		t.Fatalf("ReplaceSecretDataForResource failed: %v", err)
	}

	results, err := r.SearchSecretData("*", "key")
	if err != nil {
		t.Fatalf("SearchSecretData failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 rows total (1 atualizada + 2 intocadas), got %d: %+v", len(results), results)
	}

	byKey := map[string]SecretDataRecord{}
	for _, rec := range results {
		byKey[rec.ResourceKind+":"+rec.ResourceName+":"+rec.DataKey] = rec
	}

	updated, ok := byKey["secret:akv-ns-a:DB_PASSWORD"]
	if !ok || updated.ValueDecoded != "new-value" {
		t.Fatalf("expected DB_PASSWORD updated to 'new-value', got %+v", updated)
	}
	if _, ok := byKey["secret:outra-secret:k1"]; !ok {
		t.Fatalf("outra-secret:k1 should be untouched by a resource-scoped replace, got %+v", results)
	}
	if _, ok := byKey["configmap:akv-ns-a:k2"]; !ok {
		t.Fatalf("configmap:akv-ns-a:k2 should be untouched (different resource_kind, same name), got %+v", results)
	}
}
