package azure

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Índice de SKUs cobertos por reserva / Savings Plan numa subscription, lido do Cost Management.
//
// O que enxerga: SKUs que RODAM sob reserva/plano no período (uso observado). Reserva ociosa — a
// capacidade que ninguém está usando — não aparece: isso exigiria ler as reservas (papel
// "Reservations Reader"), que a identidade atual não tem. Também é da subscription inteira, sem
// filtro de região (o Cost Management não deixa agrupar por mais de 2 dimensões).

// SKUCost é o custo amortizado por SKU e modelo de preço (reservation | savingsplan).
type SKUCost struct {
	BySKU    map[string]map[string]float64 // SKU canônico (Standard_F4s_v2) → modelo → custo
	Currency string
}

// meterSKURe reconhece o nome do medidor de VM do Cost Management: "F4s v2", "D8ls v5", "DC4s v3".
var meterSKURe = regexp.MustCompile(`^([A-Za-z]{1,3}\d+(?:-\d+)?[A-Za-z]*)\s+(v\d+)$`)

// SKUsFromMeter converte o nome do medidor em SKUs. Um medidor pode agrupar vários tamanhos
// ("D8a v4/D8as v4", "D8 v3/D8s v3") — devolve um SKU por parte. Medidores fora do padrão
// (ex: com "Windows", "Low Priority") não viram SKU.
func SKUsFromMeter(meter string) []string {
	var out []string
	for _, part := range strings.Split(meter, "/") {
		m := meterSKURe.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil {
			continue
		}
		out = append(out, "Standard_"+m[1]+"_"+m[2])
	}
	return out
}

// CoveredSKUs consulta o custo amortizado das VMs cobertas por reserva ou Savings Plan na
// subscription do cliente, agrupado por modelo de preço e medidor (SKU), na janela [from, to].
func (c *ARMClient) CoveredSKUs(ctx context.Context, from, to time.Time) (SKUCost, error) {
	endpoint := fmt.Sprintf("%s/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=%s",
		armBaseURL, c.SubscriptionID, costMgmtAPIVersion)
	payload := map[string]any{
		"type":      "AmortizedCost",
		"timeframe": "Custom",
		"timePeriod": map[string]string{
			"from": from.UTC().Format("2006-01-02T15:04:05Z"),
			"to":   to.UTC().Format("2006-01-02T15:04:05Z"),
		},
		"dataset": map[string]any{
			"granularity": "None",
			"aggregation": map[string]any{"cost": map[string]string{"name": "Cost", "function": "Sum"}},
			"grouping": []map[string]string{
				{"type": "Dimension", "name": "PricingModel"},
				{"type": "Dimension", "name": "Meter"},
			},
			"filter": map[string]any{"and": []map[string]any{
				{"dimensions": map[string]any{"name": "ServiceName", "operator": "In", "values": []string{"Virtual Machines"}}},
				{"dimensions": map[string]any{"name": "PricingModel", "operator": "In", "values": []string{"Reservation", "SavingsPlan"}}},
			}},
		},
	}

	out := SKUCost{BySKU: map[string]map[string]float64{}}
	next := endpoint
	for page := 0; next != "" && page < costMaxPages; page++ {
		body, err := c.PostJSON(ctx, next, payload)
		if err != nil {
			return SKUCost{}, err
		}
		nl, err := accumulateSKUCost(body, &out)
		if err != nil {
			return SKUCost{}, err
		}
		next = nl
	}
	return out, nil
}

func accumulateSKUCost(body []byte, out *SKUCost) (nextLink string, err error) {
	cols, rows, nl, err := parseCostRows(body)
	if err != nil {
		return "", err
	}
	for _, need := range []string{"Cost", "PricingModel", "Meter"} {
		if _, ok := cols[need]; !ok {
			return "", fmt.Errorf("resposta do Cost Management sem a coluna %q", need)
		}
	}
	for _, row := range rows {
		cost, ok := row[cols["Cost"]].(float64)
		if !ok {
			continue
		}
		meter, _ := row[cols["Meter"]].(string)
		model, _ := row[cols["PricingModel"]].(string)
		m := normalizePricingModel(model)
		if m != ModelReservation && m != ModelSavingsPlan {
			continue
		}
		for _, sku := range SKUsFromMeter(meter) {
			if out.BySKU[sku] == nil {
				out.BySKU[sku] = map[string]float64{}
			}
			out.BySKU[sku][m] += cost
		}
		if ci, ok := cols["Currency"]; ok && out.Currency == "" {
			out.Currency, _ = row[ci].(string)
		}
	}
	return nl, nil
}
