package storage

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestIsProdCluster(t *testing.T) {
	cases := []struct {
		cluster string
		want    bool
	}{
		{"asaplog-production-admin", true},
		{"akspriv-tms-prd-admin", true},
		{"asaplog-prod", true},
		// Caso real que motivou a correção: "preprod" contém a substring "prod", mas é
		// homologação — não pode ser classificado como produção.
		{"asaplog-preprod-admin", false},
		{"arn:aws:eks:us-east-1:123456789:cluster/asaplog-preprod", false},
		{"akspriv-tms-hlg-admin", false},
		{"akspriv-tms-sit-admin", false},
		{"asaplog-staging-admin", false},
		{"gke_project_us-central1_cluster-dev", false},
	}
	for _, c := range cases {
		if got := isProdCluster(c.cluster); got != c.want {
			t.Errorf("isProdCluster(%q) = %v, want %v", c.cluster, got, c.want)
		}
	}
}

// newTestRegistry cria um DeploymentRegistry sobre SQLite em memória — nunca toca no arquivo
// real do usuário (~/.k8s-hpa-manager/deployment-registry.db).
func newTestRegistry(t *testing.T) *DeploymentRegistry {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	r := &DeploymentRegistry{db: db}
	if err := r.createSchema(); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return r
}

func TestGetProductionVersion_PrefersRealProdOverPreprod(t *testing.T) {
	r := newTestRegistry(t)

	// UpsertDeployment carimba last_seen com time.Now() no momento da chamada (ignora
	// record.LastSeen) — inserir prod PRIMEIRO e preprod DEPOIS reproduz o cenário real do bug:
	// preprod escaneado/visto mais recentemente que produção (ex: app já implantada em
	// homologação antes de ir pra produção). Sob o filtro antigo (SQL LIKE + ORDER BY
	// last_seen DESC), preprod venceria só por ser mais recente.
	if err := r.UpsertDeployment(DeploymentRecord{
		DeploymentName: "asaplog-api", Namespace: "default", Cluster: "asaplog-production-admin",
		AppName: "asaplog-api", Version: "1.9.0",
	}); err != nil {
		t.Fatalf("upsert prod: %v", err)
	}
	if err := r.UpsertDeployment(DeploymentRecord{
		DeploymentName: "asaplog-api", Namespace: "default", Cluster: "asaplog-preprod-admin",
		AppName: "asaplog-api", Version: "2.0.0",
	}); err != nil {
		t.Fatalf("upsert preprod: %v", err)
	}

	rec, err := r.GetProductionVersion("asaplog-api")
	if err != nil {
		t.Fatalf("GetProductionVersion: %v", err)
	}
	if rec.Cluster != "asaplog-production-admin" {
		t.Errorf("Cluster = %q, want asaplog-production-admin", rec.Cluster)
	}
	if rec.Version != "1.9.0" {
		t.Errorf("Version = %q, want 1.9.0 (versão de produção, não preprod)", rec.Version)
	}
}

func TestGetProductionVersion_OnlyNonProdReturnsClearError(t *testing.T) {
	r := newTestRegistry(t)
	if err := r.UpsertDeployment(DeploymentRecord{
		DeploymentName: "asaplog-api", Namespace: "default", Cluster: "asaplog-preprod-admin",
		AppName: "asaplog-api", Version: "2.0.0", LastSeen: time.Now(),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	_, err := r.GetProductionVersion("asaplog-api")
	if err == nil {
		t.Fatal("esperava erro, GetProductionVersion não deveria retornar preprod como produção")
	}
	if !strings.Contains(err.Error(), "não-produtivo") {
		t.Errorf("mensagem de erro não menciona ambiente não-produtivo: %v", err)
	}
}

func TestGetProductionVersion_NotFoundAtAll(t *testing.T) {
	r := newTestRegistry(t)
	_, err := r.GetProductionVersion("app-que-nao-existe")
	if err == nil {
		t.Fatal("esperava erro para app inexistente no registry")
	}
}

// TestGetAll_OnlyValidVersions_AcceptsDashSanitizedFormat reproduz o achado real (ver
// internal/web/handlers/spinnaker.go): o chart convair-helm (squad Reversa/Dat) sanitiza
// app.kubernetes.io/version trocando "." por "-" (ex: "0.0.2-6" vira "0-0-2-6"). Sem aceitar
// essa variante, onlyValidVersions=true excluía esses registros inteiros — não só do Spinnaker,
// de qualquer consumidor de GetAll(..., true) (ex: GitHub Releases).
func TestGetAll_OnlyValidVersions_AcceptsDashSanitizedFormat(t *testing.T) {
	r := newTestRegistry(t)
	records := []DeploymentRecord{
		{DeploymentName: "dot-format", Namespace: "ns", Cluster: "c", AppName: "dot-format", Version: "0.0.2-6", LastSeen: time.Now()},
		{DeploymentName: "dash-format", Namespace: "ns", Cluster: "c", AppName: "dash-format", Version: "0-0-2-6", LastSeen: time.Now()},
		{DeploymentName: "garbage-format", Namespace: "ns", Cluster: "c", AppName: "garbage-format", Version: "latest", LastSeen: time.Now()},
	}
	for _, rec := range records {
		if err := r.UpsertDeployment(rec); err != nil {
			t.Fatalf("upsert %s: %v", rec.DeploymentName, err)
		}
	}

	got, err := r.GetAll("c", "ns", true)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	names := map[string]bool{}
	for _, rec := range got {
		names[rec.DeploymentName] = true
	}
	if !names["dot-format"] {
		t.Error("esperava 'dot-format' (versão com ponto) no resultado")
	}
	if !names["dash-format"] {
		t.Error("esperava 'dash-format' (versão sanitizada com hífen, achado real do chart convair-helm) no resultado")
	}
	if names["garbage-format"] {
		t.Error("'garbage-format' (versão não-semver, ex: 'latest') não deveria passar no filtro")
	}
}
