package finops

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"k8s-hpa-manager/internal/monitoring/discovery"
)

// Nodes por hora da semana, por pool (FINOPS-REVISAO-PLAN.md, Fase 3 — fora do horário): mostra se
// o ambiente já desliga à noite e base a economia nas horas-node que de fato rodam.

// nodeHoursQuery conta os nodes por pool pelo nome AKS (aks-<pool>-<id>-vmss<instância>).
const nodeHoursQuery = `count by (pool) (label_replace(kube_node_info, "pool", "$1", "node", "aks-(.+?)-[0-9]+-vmss.*"))`

// NodeHours: por pool, a média de nodes em cada hora da semana (índice 0 = segunda 00h, horário
// local; 167 = domingo 23h); -1 = hora sem amostra (não é "0 node"). Hours = horas com amostra.
type NodeHours struct {
	Pools map[string][]float64 `json:"pools"`
	Hours int                  `json:"hours"`
	Days  int                  `json:"days"`
}

// hourOfWeek: 0 = segunda 00h.
func hourOfWeek(t time.Time) int {
	wd := (int(t.Weekday()) + 6) % 7
	return wd*24 + t.Hour()
}

// bucketNodeHours agrega a série (ponto por hora) em médias por hora da semana.
func bucketNodeHours(m model.Matrix, loc *time.Location) NodeHours {
	out := NodeHours{Pools: map[string][]float64{}}
	seen := map[int]bool{}
	for _, s := range m {
		pool := string(s.Metric["pool"])
		if pool == "" {
			continue
		}
		sum, n := make([]float64, 168), make([]int, 168)
		for _, v := range s.Values {
			h := hourOfWeek(v.Timestamp.Time().In(loc))
			sum[h] += float64(v.Value)
			n[h]++
			seen[h] = true
		}
		avg := make([]float64, 168)
		for h := range avg {
			avg[h] = -1
			if n[h] > 0 {
				avg[h] = sum[h] / float64(n[h])
			}
		}
		out.Pools[pool] = avg
	}
	out.Hours = len(seen)
	return out
}

// QueryNodeHours consulta os últimos `days` dias (passo de 1 h) no Prometheus do cluster.
func QueryNodeHours(ctx context.Context, prometheusURL string, days int, requiresGCPAuth bool) (NodeHours, error) {
	transport := &http.Transport{}
	var rt http.RoundTripper = transport
	if requiresGCPAuth {
		rt = discovery.GCPAuthTransport(transport)
	} else {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	client, err := api.NewClient(api.Config{Address: prometheusURL, RoundTripper: rt})
	if err != nil {
		return NodeHours{}, fmt.Errorf("falha ao criar cliente Prometheus: %w", err)
	}
	end := time.Now()
	res, _, err := v1.NewAPI(client).QueryRange(ctx, nodeHoursQuery, v1.Range{Start: end.AddDate(0, 0, -days), End: end, Step: time.Hour})
	if err != nil {
		return NodeHours{}, fmt.Errorf("Prometheus: %w", err)
	}
	m, ok := res.(model.Matrix)
	if !ok {
		return NodeHours{}, fmt.Errorf("resposta inesperada do Prometheus (%T)", res)
	}
	out := bucketNodeHours(m, time.Local)
	out.Days = days
	return out, nil
}
