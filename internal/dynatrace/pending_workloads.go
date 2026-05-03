package dynatrace

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
)

// PendingWorkloadDT representa um workload K8s com pods não prontos conforme reportado pelo Dynatrace.
type PendingWorkloadDT struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Kind      string `json:"kind"`
	Running   int    `json:"running"`
	NotReady  int    `json:"not_ready"`
}

// GetPendingWorkloads retorna workloads do cluster onde nem todos os pods estão prontos,
// consultando builtin:kubernetes.workload.pods.running e pods.readyFraction.
// Filtra por nome de cluster (case-insensitive, ignora sufixo -admin).
// Retorna lista vazia (não erro) quando DT não monitora o cluster.
func (c *Client) GetPendingWorkloads(ctx context.Context, clusterName string) ([]PendingWorkloadDT, error) {
	running, err := c.queryWorkloadByCluster(ctx, "builtin:kubernetes.workload.pods.running:last")
	if err != nil {
		return nil, fmt.Errorf("DT pods.running: %w", err)
	}
	if len(running) == 0 {
		return nil, nil
	}

	readyFrac, err := c.queryWorkloadByCluster(ctx, "builtin:kubernetes.workload.pods.readyFraction:last")
	if err != nil {
		return nil, fmt.Errorf("DT pods.readyFraction: %w", err)
	}

	// Normaliza cluster para matching: remove sufixo -admin, lowercase
	clusterKey := strings.ToLower(strings.TrimSuffix(clusterName, "-admin"))

	var results []PendingWorkloadDT
	for key, runCount := range running {
		// key: "cluster/namespace/workload/kind"
		parts := strings.SplitN(key, "\x00", 4)
		if len(parts) != 4 {
			continue
		}
		clus, ns, wl, kind := parts[0], parts[1], parts[2], parts[3]

		// Filtrar por cluster (contains bidirecional para cobrir prefixos)
		clusNorm := strings.ToLower(strings.TrimSuffix(clus, "-admin"))
		if !strings.Contains(clusNorm, clusterKey) && !strings.Contains(clusterKey, clusNorm) {
			continue
		}

		if runCount <= 0 {
			continue // scaled down intencionalmente
		}

		frac := readyFrac[key] // 0.0–1.0
		notReady := int(math.Round(runCount * (1 - frac)))
		if notReady <= 0 {
			continue
		}

		results = append(results, PendingWorkloadDT{
			Namespace: ns,
			Workload:  wl,
			Kind:      kind,
			Running:   int(math.Round(runCount)),
			NotReady:  notReady,
		})
	}
	return results, nil
}

// queryWorkloadByCluster executa a query com splitBy de cluster/namespace/workload/kind.
// Usa \x00 como separador interno para evitar colisão com "/" em nomes.
func (c *Client) queryWorkloadByCluster(ctx context.Context, metricExpr string) (map[string]float64, error) {
	metricSelector := fmt.Sprintf(
		`%s:splitBy("k8s.cluster.name","k8s.namespace.name","k8s.workload.name","k8s.workload.kind")`,
		metricExpr,
	)

	params := url.Values{
		"metricSelector": {metricSelector},
		"from":           {"now-5m"},
		"to":             {"now"},
		"resolution":     {"inf"},
	}

	var response struct {
		Result []struct {
			Data []struct {
				DimensionMap map[string]string `json:"dimensionMap"`
				Values       []float64         `json:"values"`
			} `json:"data"`
		} `json:"result"`
	}

	if err := c.get(ctx, "metrics/query", params, &response); err != nil {
		return nil, err
	}

	out := make(map[string]float64)
	if len(response.Result) == 0 {
		return out, nil
	}

	for _, series := range response.Result[0].Data {
		clus := series.DimensionMap["k8s.cluster.name"]
		ns := series.DimensionMap["k8s.namespace.name"]
		wl := series.DimensionMap["k8s.workload.name"]
		kind := series.DimensionMap["k8s.workload.kind"]
		if clus == "" || ns == "" || wl == "" || len(series.Values) == 0 {
			continue
		}
		for _, v := range series.Values {
			if !isNaN(v) {
				out[clus+"\x00"+ns+"\x00"+wl+"\x00"+kind] = v
				break
			}
		}
	}
	return out, nil
}

func isNaN(f float64) bool {
	return f != f
}
