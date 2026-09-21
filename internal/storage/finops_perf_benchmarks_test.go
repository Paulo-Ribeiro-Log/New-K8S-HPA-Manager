package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestPerfBenchmarks_SaveUpsertList(t *testing.T) {
	st, err := NewFinOpsRightsizingStore(filepath.Join(t.TempDir(), "rs.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first := NodePerfBenchmark{Cluster: "c1", NodeName: "n1", NodePool: "p", SKU: "Standard_F4s_v2", CPUModel: "Xeon", VCPUs: 4,
		PyScore: 18, PySpread: 0.1, RSASignPerSec: 1300, CPUFeatures: "adx avx2", Samples: 5, MeasuredAt: now}
	other := NodePerfBenchmark{Cluster: "c2", NodeName: "n1", SKU: "Standard_D4s_v4", PyScore: 19, MeasuredAt: now}
	if err := st.SavePerfBenchmarks([]NodePerfBenchmark{first, other}); err != nil {
		t.Fatal(err)
	}

	// Mesma chave (cluster+node) sobrescreve; não duplica.
	first.PyScore, first.CPUFeatures = 20, "adx avx2 sha_ni"
	if err := st.SavePerfBenchmarks([]NodePerfBenchmark{first}); err != nil {
		t.Fatal(err)
	}
	c1, err := st.ListPerfBenchmarks("c1")
	if err != nil || len(c1) != 1 || c1[0].PyScore != 20 || c1[0].CPUFeatures != "adx avx2 sha_ni" || c1[0].RSASignPerSec != 1300 {
		t.Fatalf("upsert falhou: %+v err=%v", c1, err)
	}
	all, _ := st.ListPerfBenchmarks("")
	if len(all) != 2 {
		t.Errorf("cluster vazio = frota inteira (2 nodes), got %d", len(all))
	}
	if none, err := st.ListPerfBenchmarks("nao-existe"); err != nil || none == nil || len(none) != 0 {
		t.Errorf("cluster sem medição deve dar slice vazio NÃO-nil (vira [] no JSON): %v %v", none, err)
	}
}

func TestPerfBenchmarks_PodaMedicaoAntiga(t *testing.T) {
	st, _ := NewFinOpsRightsizingStore(filepath.Join(t.TempDir(), "rs.db"))
	old := NodePerfBenchmark{Cluster: "c", NodeName: "velho", SKU: "Standard_F4s_v2", PyScore: 1, MeasuredAt: time.Now().Add(-100 * 24 * time.Hour)}
	fresh := NodePerfBenchmark{Cluster: "c", NodeName: "novo", SKU: "Standard_F4s_v2", PyScore: 1, MeasuredAt: time.Now()}
	if err := st.SavePerfBenchmarks([]NodePerfBenchmark{old, fresh}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ListPerfBenchmarks("c")
	if len(got) != 1 || got[0].NodeName != "novo" {
		t.Errorf("medição de 100 dias (node que pode nem existir mais) deveria ser podada: %+v", got)
	}
}

// Banco criado ANTES da coluna cpu_features: abrir o store precisa migrar sem perder as linhas.
func TestPerfBenchmarks_MigraBancoSemCpuFeatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rs.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE node_perf_benchmarks (
		cluster TEXT NOT NULL, node_name TEXT NOT NULL, node_pool TEXT, sku TEXT, cpu_model TEXT, vcpus INTEGER,
		py_score REAL, py_spread REAL, rsa_sign_per_sec REAL, samples INTEGER, measured_at DATETIME NOT NULL,
		PRIMARY KEY (cluster, node_name));
		INSERT INTO node_perf_benchmarks VALUES ('c','n','p','Standard_F4s_v2','Xeon',4,18,0.1,1300,5,datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := NewFinOpsRightsizingStore(path)
	if err != nil {
		t.Fatalf("abrir banco antigo deveria migrar: %v", err)
	}
	got, err := st.ListPerfBenchmarks("c")
	if err != nil || len(got) != 1 || got[0].PyScore != 18 || got[0].CPUFeatures != "" {
		t.Fatalf("linha antiga deveria sobreviver com cpu_features vazio: %+v err=%v", got, err)
	}
	// Reabrir de novo (coluna já existe) não pode falhar.
	if _, err := NewFinOpsRightsizingStore(path); err != nil {
		t.Errorf("migração precisa ser idempotente: %v", err)
	}
}
