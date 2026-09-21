package storage

import (
	"fmt"
	"time"
)

// NodePerfBenchmark é a medição de desempenho de CPU single-thread de UM node (o último resultado
// por node — uma nova medição sobrescreve a anterior). Existe pra responder, com dado real do
// ambiente, "trocar o SKU X pelo SKU Y muda o tempo de resposta das APIs?": o nome do SKU não fixa
// o processador (a Microsoft documenta que a mesma série roda em gerações diferentes de Xeon
// conforme região/host), então só medir diz o que cada pool de fato entrega.
type NodePerfBenchmark struct {
	Cluster  string `json:"cluster"`
	NodeName string `json:"node_name"`
	NodePool string `json:"node_pool"`
	// SKU é o tipo de instância lido do label do próprio node (node.kubernetes.io/instance-type) no
	// momento da medição — mais confiável que o registro de node pools (que pode estar defasado ou
	// com caixa diferente, ex: "Standard_f4s_v2" x "Standard_F4s_v2").
	SKU string `json:"sku"`
	// CPUModel é o "model name" de /proc/cpuinfo visto de dentro do pod — revela o processador real
	// do host (ex: "Intel(R) Xeon(R) Platinum 8272CL CPU @ 2.60GHz"), que o SKU sozinho não diz.
	CPUModel string `json:"cpu_model"`
	VCPUs    int    `json:"vcpus"`
	// PyScore: iterações/s do loop de inteiros em Python — melhor amostra (a menos contaminada por
	// vizinhos ruidosos). Proxy de "código de aplicação genérico, single-thread".
	PyScore float64 `json:"py_score"`
	// PySpread: (max-min)/max entre as amostras — mede o quão instável foi a medição. Alto = ruidosa.
	PySpread float64 `json:"py_spread"`
	// RSASignPerSec: assinaturas RSA-2048 por segundo (openssl speed) — proxy de carga criptográfica
	// (terminação TLS).
	RSASignPerSec float64 `json:"rsa_sign_per_sec"`
	// CPUFeatures: extensões de CPU vistas pela VM (separadas por espaço) — explica diferenças de
	// criptografia entre SKUs que compartilham o mesmo "model name".
	CPUFeatures string    `json:"cpu_features,omitempty"`
	Samples     int       `json:"samples"`
	MeasuredAt  time.Time `json:"measured_at"`
}

// Bancos criados antes de cpu_features existir: ADD COLUMN idempotente ("duplicate column" é ignorado
// pelo chamador, mesmo padrão de finopsRightsizingMigrations).
const finopsPerfBenchmarksMigration = `ALTER TABLE node_perf_benchmarks ADD COLUMN cpu_features TEXT`

const finopsPerfBenchmarksSchema = `
CREATE TABLE IF NOT EXISTS node_perf_benchmarks (
    cluster          TEXT NOT NULL,
    node_name        TEXT NOT NULL,
    node_pool        TEXT,
    sku              TEXT,
    cpu_model        TEXT,
    vcpus            INTEGER,
    py_score         REAL,
    py_spread        REAL,
    rsa_sign_per_sec REAL,
    cpu_features     TEXT,
    samples          INTEGER,
    measured_at      DATETIME NOT NULL,
    PRIMARY KEY (cluster, node_name)
);
`

// perfBenchmarkRetention: medição de node que não existe mais (pool recriado/escalado pra baixo)
// não deve pesar pra sempre na mediana por SKU.
const perfBenchmarkRetention = 90 * 24 * time.Hour

// SavePerfBenchmarks grava (upsert por cluster+node) as medições e poda as muito antigas.
func (s *FinOpsRightsizingStore) SavePerfBenchmarks(recs []NodePerfBenchmark) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`
INSERT INTO node_perf_benchmarks (
    cluster, node_name, node_pool, sku, cpu_model, vcpus,
    py_score, py_spread, rsa_sign_per_sec, cpu_features, samples, measured_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(cluster, node_name) DO UPDATE SET
    node_pool = excluded.node_pool, sku = excluded.sku, cpu_model = excluded.cpu_model,
    vcpus = excluded.vcpus, py_score = excluded.py_score, py_spread = excluded.py_spread,
    rsa_sign_per_sec = excluded.rsa_sign_per_sec, cpu_features = excluded.cpu_features, samples = excluded.samples,
    measured_at = excluded.measured_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range recs {
		if _, err := stmt.Exec(r.Cluster, r.NodeName, r.NodePool, r.SKU, r.CPUModel, r.VCPUs,
			r.PyScore, r.PySpread, r.RSASignPerSec, r.CPUFeatures, r.Samples, r.MeasuredAt); err != nil {
			return fmt.Errorf("gravar medição %s/%s: %w", r.Cluster, r.NodeName, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM node_perf_benchmarks WHERE measured_at < ?`, time.Now().Add(-perfBenchmarkRetention)); err != nil {
		return err
	}
	return tx.Commit()
}

// ListPerfBenchmarks devolve as medições mais recentes por node. cluster vazio = frota inteira
// (usado pra comparar SKUs medidos em clusters diferentes).
func (s *FinOpsRightsizingStore) ListPerfBenchmarks(cluster string) ([]NodePerfBenchmark, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := `SELECT cluster, node_name, COALESCE(node_pool,''), COALESCE(sku,''), COALESCE(cpu_model,''),
       COALESCE(vcpus,0), COALESCE(py_score,0), COALESCE(py_spread,0), COALESCE(rsa_sign_per_sec,0),
       COALESCE(cpu_features,''), COALESCE(samples,0), measured_at
FROM node_perf_benchmarks`
	args := []any{}
	if cluster != "" {
		q += ` WHERE cluster = ?`
		args = append(args, cluster)
	}
	q += ` ORDER BY cluster, node_pool, node_name`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []NodePerfBenchmark{}
	for rows.Next() {
		var r NodePerfBenchmark
		if err := rows.Scan(&r.Cluster, &r.NodeName, &r.NodePool, &r.SKU, &r.CPUModel, &r.VCPUs,
			&r.PyScore, &r.PySpread, &r.RSASignPerSec, &r.CPUFeatures, &r.Samples, &r.MeasuredAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
