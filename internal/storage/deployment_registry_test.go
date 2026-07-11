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
