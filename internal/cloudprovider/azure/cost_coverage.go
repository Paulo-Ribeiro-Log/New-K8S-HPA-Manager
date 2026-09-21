package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Cobertura de preço (reserva / Savings Plan / sob demanda / spot) dos node pools de um AKS, lida do
// Cost Management. Só leitura, e funciona com o papel de leitura de custos da subscription — não exige
// "Reservations Reader"/"Savings plan Reader" (que só o tenant/billing concede).
//
// Por que CUSTO AMORTIZADO e não horas: o Cost Management informa o modelo de preço de cada linha
// (PricingModel), mas UsageQuantity tem unidade que varia por medidor (não dá pra somar como horas de
// VM), e no custo efetivo (ActualCost) uma hora coberta por reserva pré-paga custa ZERO — some da
// conta. O amortizado distribui o preço da reserva/plano pelas horas que ele cobriu.

const (
	costMgmtAPIVersion = "2023-03-01"
	aksAPIVersion      = "2024-02-01"
	costMaxPages       = 5
)

// PricingModels devolvidos pelo Cost Management (normalizados em minúsculas).
const (
	ModelReservation = "reservation"
	ModelSavingsPlan = "savingsplan"
	ModelOnDemand    = "ondemand"
	ModelSpot        = "spot"
	ModelOther       = "other"
)

// PoolCost é o custo amortizado por node pool e modelo de preço numa janela.
type PoolCost struct {
	ByPool   map[string]map[string]float64 // node pool → modelo (constantes acima) → custo
	Currency string
}

// NodeResourceGroup devolve o resource group dos nodes ("MC_...") do AKS — não dá pra derivar do nome
// (pode ter nome customizado), então lê do próprio managed cluster.
func (c *ARMClient) NodeResourceGroup(ctx context.Context, resourceGroup, clusterName string) (string, error) {
	u := fmt.Sprintf("%s/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s?api-version=%s",
		armBaseURL, c.SubscriptionID, url.PathEscape(resourceGroup), url.PathEscape(clusterName), aksAPIVersion)
	body, err := c.Get(ctx, u)
	if err != nil {
		return "", err
	}
	var resp struct {
		Properties struct {
			NodeResourceGroup string `json:"nodeResourceGroup"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse managed cluster: %w", err)
	}
	if resp.Properties.NodeResourceGroup == "" {
		return "", fmt.Errorf("o managed cluster %s não informou nodeResourceGroup", clusterName)
	}
	return resp.Properties.NodeResourceGroup, nil
}

// PoolCostByPricingModel consulta o custo AMORTIZADO das VMs do resource group de nodes na janela
// [from, to], agrupado por modelo de preço e por recurso, e o agrega por node pool (nome do VMSS).
func (c *ARMClient) PoolCostByPricingModel(ctx context.Context, nodeRG string, from, to time.Time) (PoolCost, error) {
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
				{"type": "Dimension", "name": "ResourceId"},
			},
			"filter": map[string]any{"and": []map[string]any{
				{"dimensions": map[string]any{"name": "ResourceGroupName", "operator": "In", "values": []string{nodeRG}}},
				{"dimensions": map[string]any{"name": "ServiceName", "operator": "In", "values": []string{"Virtual Machines"}}},
			}},
		},
	}

	out := PoolCost{ByPool: map[string]map[string]float64{}}
	next := endpoint
	for page := 0; next != "" && page < costMaxPages; page++ {
		body, err := c.PostJSON(ctx, next, payload)
		if err != nil {
			return PoolCost{}, err
		}
		nl, err := accumulatePoolCost(body, &out)
		if err != nil {
			return PoolCost{}, err
		}
		next = nl
	}
	return out, nil
}

// vmssPoolRe extrai o node pool do nome do VMSS que o AKS cria: aks-<pool>-<8 dígitos>-vmss.
var vmssPoolRe = regexp.MustCompile(`(?i)/virtualmachinescalesets/aks-(.+?)-\d+-vmss`)

// PoolFromVMSSResourceID devolve o node pool a partir do resource ID de um VMSS do AKS ("" se não for).
func PoolFromVMSSResourceID(resourceID string) string {
	m := vmssPoolRe.FindStringSubmatch(resourceID)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

func normalizePricingModel(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "reservation":
		return ModelReservation
	case "savingsplan":
		return ModelSavingsPlan
	case "ondemand":
		return ModelOnDemand
	case "spot":
		return ModelSpot
	default:
		return ModelOther
	}
}

// parseCostRows decodifica a resposta do Cost Management em índice de colunas, linhas e nextLink.
func parseCostRows(body []byte) (cols map[string]int, rows [][]any, nextLink string, err error) {
	var resp struct {
		Properties struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows     [][]any `json:"rows"`
			NextLink string  `json:"nextLink"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, "", fmt.Errorf("parse cost management: %w", err)
	}
	cols = map[string]int{}
	for i, c := range resp.Properties.Columns {
		cols[c.Name] = i
	}
	return cols, resp.Properties.Rows, resp.Properties.NextLink, nil
}

// accumulatePoolCost soma as linhas de uma página no acumulador e devolve o nextLink ("" = fim).
// Linhas de recursos que não são VMSS de node pool (ex: uma VM avulsa no grupo) são ignoradas.
func accumulatePoolCost(body []byte, out *PoolCost) (nextLink string, err error) {
	idx, rows, nl, err := parseCostRows(body)
	if err != nil {
		return "", err
	}
	for _, need := range []string{"Cost", "PricingModel", "ResourceId"} {
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
		pool := PoolFromVMSSResourceID(rid)
		if pool == "" {
			continue
		}
		model, _ := row[idx["PricingModel"]].(string)
		if out.ByPool[pool] == nil {
			out.ByPool[pool] = map[string]float64{}
		}
		out.ByPool[pool][normalizePricingModel(model)] += cost
		if ci, ok := idx["Currency"]; ok && out.Currency == "" {
			out.Currency, _ = row[ci].(string)
		}
	}
	return nl, nil
}
