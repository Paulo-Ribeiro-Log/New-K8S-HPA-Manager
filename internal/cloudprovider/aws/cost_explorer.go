package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Custo faturado de um cluster EKS (FinOps → "Fatura × estimativa"): Cost Explorer, custo
// AMORTIZADO por serviço, filtrado pela tag aws:eks:cluster-name. A AWS aplica essa tag aos
// recursos dos node groups gerenciados, mas ela só aparece no Cost Explorer depois de ativada
// como "cost allocation tag" no Billing (uma vez por conta pagadora).

const ceClusterTag = "aws:eks:cluster-name"

// ServiceCost é o custo por serviço numa janela, na moeda devolvida (USD).
type ServiceCost struct {
	ByService map[string]float64
	Currency  string
}

// parseCostAndUsage soma ResultsByTime[].Groups[] (Keys[0] = serviço) do get-cost-and-usage.
func parseCostAndUsage(body []byte) (ServiceCost, error) {
	var resp struct {
		ResultsByTime []struct {
			Groups []struct {
				Keys    []string `json:"Keys"`
				Metrics map[string]struct {
					Amount string `json:"Amount"`
					Unit   string `json:"Unit"`
				} `json:"Metrics"`
			} `json:"Groups"`
		} `json:"ResultsByTime"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ServiceCost{}, fmt.Errorf("parse Cost Explorer: %w", err)
	}
	out := ServiceCost{ByService: map[string]float64{}}
	for _, r := range resp.ResultsByTime {
		for _, g := range r.Groups {
			m, ok := g.Metrics["AmortizedCost"]
			if !ok || len(g.Keys) == 0 {
				continue
			}
			v, err := strconv.ParseFloat(m.Amount, 64)
			if err != nil {
				continue
			}
			out.ByService[g.Keys[0]] += v
			if out.Currency == "" {
				out.Currency = m.Unit
			}
		}
	}
	return out, nil
}

// EKSCostByService consulta o Cost Explorer (endpoint global, us-east-1) do perfil do cluster.
// `to` é exclusivo (formato do Cost Explorer).
func EKSCostByService(ctx context.Context, profile, clusterName string, from, to time.Time) (ServiceCost, error) {
	filter := fmt.Sprintf(`{"Tags":{"Key":%q,"Values":[%q],"MatchOptions":["EQUALS"]}}`, ceClusterTag, clusterName)
	args := buildAWSArgs("us-east-1", profile, "ce", "get-cost-and-usage",
		"--time-period", fmt.Sprintf("Start=%s,End=%s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		"--granularity", "MONTHLY", "--metrics", "AmortizedCost",
		"--group-by", "Type=DIMENSION,Key=SERVICE", "--filter", filter)
	body, err := runAWSCLI(ctx, profile, args)
	if err != nil {
		return ServiceCost{}, fmt.Errorf("aws ce get-cost-and-usage: %w", err)
	}
	return parseCostAndUsage(body)
}
