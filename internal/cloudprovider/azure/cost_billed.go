package azure

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Custo FATURADO (amortizado) dos resource groups de um AKS, por serviço do Azure. É o que a fatura
// mostra para o cluster — inclui o que o relatório do FinOps não estima (load balancer, IP público,
// rede, Log Analytics no RG, o próprio plano do AKS). Amortizado pelo mesmo motivo da cobertura
// (cost_coverage.go): no ActualCost a hora coberta por reserva pré-paga custa zero.

// RGServiceCost é o custo por resource group (em minúsculas) e serviço (ServiceName) numa janela.
type RGServiceCost struct {
	ByRG     map[string]map[string]float64
	Currency string
}

// CostByResourceGroupAndService consulta o custo AMORTIZADO dos resource groups na janela [from, to],
// agrupado por resource group e serviço.
func (c *ARMClient) CostByResourceGroupAndService(ctx context.Context, rgs []string, from, to time.Time) (RGServiceCost, error) {
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
				{"type": "Dimension", "name": "ResourceGroupName"},
				{"type": "Dimension", "name": "ServiceName"},
			},
			"filter": map[string]any{
				"dimensions": map[string]any{"name": "ResourceGroupName", "operator": "In", "values": rgs},
			},
		},
	}

	out := RGServiceCost{ByRG: map[string]map[string]float64{}}
	next := endpoint
	for page := 0; next != "" && page < costMaxPages; page++ {
		body, err := c.PostJSON(ctx, next, payload)
		if err != nil {
			return RGServiceCost{}, err
		}
		nl, err := accumulateRGServiceCost(body, &out)
		if err != nil {
			return RGServiceCost{}, err
		}
		next = nl
	}
	return out, nil
}

// accumulateRGServiceCost soma as linhas de uma página no acumulador e devolve o nextLink ("" = fim).
func accumulateRGServiceCost(body []byte, out *RGServiceCost) (nextLink string, err error) {
	idx, rows, nl, err := parseCostRows(body)
	if err != nil {
		return "", err
	}
	for _, need := range []string{"Cost", "ResourceGroupName", "ServiceName"} {
		if _, ok := idx[need]; !ok {
			return "", fmt.Errorf("resposta do Cost Management sem a coluna %q", need)
		}
	}
	for _, row := range rows {
		cost, ok := row[idx["Cost"]].(float64)
		if !ok {
			continue
		}
		rg, _ := row[idx["ResourceGroupName"]].(string)
		rg = strings.ToLower(strings.TrimSpace(rg))
		if rg == "" {
			continue
		}
		svc, _ := row[idx["ServiceName"]].(string)
		if strings.TrimSpace(svc) == "" {
			svc = "Outros"
		}
		if out.ByRG[rg] == nil {
			out.ByRG[rg] = map[string]float64{}
		}
		out.ByRG[rg][svc] += cost
		if ci, ok := idx["Currency"]; ok && out.Currency == "" {
			out.Currency, _ = row[ci].(string)
		}
	}
	return nl, nil
}

// DiskCost é o custo amortizado de discos do RG de nodes: OS por pool (no VMSS) e por disco.
type DiskCost struct {
	OSByPool map[string]float64 // pool → custo do disco do SO dos nodes (serviço Storage no VMSS)
	ByDisk   map[string]float64 // nome do disco (minúsculas) → custo
	Currency string
}

// DiskCostByResource consulta o custo AMORTIZADO do serviço Storage no RG de nodes, por recurso.
func (c *ARMClient) DiskCostByResource(ctx context.Context, nodeRG string, from, to time.Time) (DiskCost, error) {
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
			"grouping":    []map[string]string{{"type": "Dimension", "name": "ResourceId"}},
			"filter": map[string]any{"and": []map[string]any{
				{"dimensions": map[string]any{"name": "ResourceGroupName", "operator": "In", "values": []string{nodeRG}}},
				{"dimensions": map[string]any{"name": "ServiceName", "operator": "In", "values": []string{"Storage"}}},
			}},
		},
	}
	out := DiskCost{OSByPool: map[string]float64{}, ByDisk: map[string]float64{}}
	next := endpoint
	for page := 0; next != "" && page < costMaxPages; page++ {
		body, err := c.PostJSON(ctx, next, payload)
		if err != nil {
			return DiskCost{}, err
		}
		nl, err := accumulateDiskCost(body, &out)
		if err != nil {
			return DiskCost{}, err
		}
		next = nl
	}
	return out, nil
}

// accumulateDiskCost soma uma página: VMSS do AKS → disco do SO do pool; /disks/<nome> → disco.
func accumulateDiskCost(body []byte, out *DiskCost) (string, error) {
	idx, rows, nl, err := parseCostRows(body)
	if err != nil {
		return "", err
	}
	for _, need := range []string{"Cost", "ResourceId"} {
		if _, ok := idx[need]; !ok {
			return "", fmt.Errorf("resposta do Cost Management sem a coluna %q", need)
		}
	}
	for _, row := range rows {
		cost, ok := row[idx["Cost"]].(float64)
		if !ok {
			continue
		}
		rid, _ := row[idx["ResourceId"]].(string)
		rid = strings.ToLower(rid)
		if pool := PoolFromVMSSResourceID(rid); pool != "" {
			out.OSByPool[pool] += cost
		} else if i := strings.Index(rid, "/microsoft.compute/disks/"); i >= 0 {
			out.ByDisk[rid[i+len("/microsoft.compute/disks/"):]] += cost
		} else {
			continue
		}
		if ci, ok := idx["Currency"]; ok && out.Currency == "" {
			out.Currency, _ = row[ci].(string)
		}
	}
	return nl, nil
}
