package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// Custo faturado de um cluster GKE (FinOps → "Fatura × estimativa"): export de billing do Cloud
// Billing no BigQuery, custo + créditos por serviço, filtrado pelo label que o GKE põe nos
// recursos do cluster (goog-k8s-cluster-name). A tabela do export é da organização e não dá para
// descobrir sozinha: vem de K8S_HPA_GKE_BILLING_TABLE (projeto.dataset.gcp_billing_export_v1_XXX).

// ServiceCost é o custo por serviço numa janela.
type ServiceCost struct {
	ByService map[string]float64
	Currency  string
}

var bqTableRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_]+\.[A-Za-z0-9_]+$`)

// parseBQRows lê a saída de `bq query --format=json` ([{service, cost, currency}]).
func parseBQRows(body []byte) (ServiceCost, error) {
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return ServiceCost{}, fmt.Errorf("parse BigQuery: %w", err)
	}
	out := ServiceCost{ByService: map[string]float64{}}
	for _, r := range rows {
		svc, _ := r["service"].(string)
		var v float64
		switch c := r["cost"].(type) {
		case string:
			v, _ = strconv.ParseFloat(c, 64)
		case float64:
			v = c
		}
		if svc == "" {
			svc = "Outros"
		}
		out.ByService[svc] += v
		if cur, _ := r["currency"].(string); out.Currency == "" && cur != "" {
			out.Currency = cur
		}
	}
	return out, nil
}

// GKECostByService consulta o export de billing (bq CLI, conta do gcloud) para o cluster.
func GKECostByService(ctx context.Context, table, clusterName string, from, to time.Time) (ServiceCost, error) {
	if !bqTableRe.MatchString(table) {
		return ServiceCost{}, fmt.Errorf("K8S_HPA_GKE_BILLING_TABLE inválida (esperado projeto.dataset.tabela): %q", table)
	}
	query := fmt.Sprintf("SELECT service.description AS service, "+
		"SUM(cost) + SUM(IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0)) AS cost, ANY_VALUE(currency) AS currency "+
		"FROM `%s` WHERE usage_start_time >= TIMESTAMP('%s') AND usage_start_time < TIMESTAMP('%s') "+
		"AND EXISTS (SELECT 1 FROM UNNEST(labels) l WHERE l.key = 'goog-k8s-cluster-name' AND l.value = @cluster) "+
		"GROUP BY service", table, from.Format("2006-01-02"), to.Format("2006-01-02"))
	cmd := exec.CommandContext(ctx, "bq", "query", "--use_legacy_sql=false", "--format=json", "--max_rows=1000",
		"--parameter=cluster:STRING:"+clusterName, query)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ServiceCost{}, fmt.Errorf("bq query: %w: %s", err, string(ee.Stderr))
		}
		return ServiceCost{}, fmt.Errorf("bq query: %w", err)
	}
	return parseBQRows(out)
}
