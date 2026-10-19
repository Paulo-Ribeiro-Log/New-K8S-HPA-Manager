package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Preço de reserva e Savings Plan por SKU na Azure Retail Prices API (pública, sem login) — base
// da sugestão de reserva para a carga base (FINOPS-REVISAO-PLAN.md, Fase 3).
//
// Na API, um item priceType=Reservation traz em retailPrice o preço do PRAZO INTEIRO (1 ou 3 anos),
// não por hora; o item Consumption (sob demanda) traz, na api-version 2023-01-01-preview, o array
// savingsPlan com o preço por HORA de cada prazo.

// CommitmentPrices são os preços em USD por VM por mês de um SKU numa região.
type CommitmentPrices struct {
	OnDemand      float64 `json:"on_demand"`
	Reservation1Y float64 `json:"reservation_1y,omitempty"`
	Reservation3Y float64 `json:"reservation_3y,omitempty"`
	SavingsPlan1Y float64 `json:"savings_plan_1y,omitempty"`
	SavingsPlan3Y float64 `json:"savings_plan_3y,omitempty"`
}

// Key é a chave do mapa de preços que SuggestBaseReservations espera (SKU normalizado).
func (CommitmentPrices) Key(vmSize string) string { return normalizeVMSize(vmSize) }

type retailCommitmentItem struct {
	RetailPrice     float64 `json:"retailPrice"`
	UnitOfMeasure   string  `json:"unitOfMeasure"`
	Type            string  `json:"type"` // Consumption | Reservation | DevTestConsumption
	ReservationTerm string  `json:"reservationTerm"`
	ProductName     string  `json:"productName"`
	MeterName       string  `json:"meterName"`
	SavingsPlan     []struct {
		RetailPrice float64 `json:"retailPrice"`
		Term        string  `json:"term"`
	} `json:"savingsPlan"`
}

// parseCommitmentItems extrai os preços mensais por VM dos itens Linux (sem Spot/Low Priority).
func parseCommitmentItems(items []retailCommitmentItem) CommitmentPrices {
	var out CommitmentPrices
	minPos := func(cur *float64, v float64) {
		if v > 0 && (*cur == 0 || v < *cur) {
			*cur = v
		}
	}
	for _, it := range items {
		prod, meter := strings.ToLower(it.ProductName), strings.ToLower(it.MeterName)
		if strings.Contains(prod, "windows") || strings.Contains(meter, "spot") || strings.Contains(meter, "low priority") {
			continue
		}
		switch it.Type {
		case "Consumption":
			if it.UnitOfMeasure != "1 Hour" {
				continue
			}
			minPos(&out.OnDemand, it.RetailPrice*HoursPerMonth)
			for _, sp := range it.SavingsPlan {
				switch sp.Term {
				case "1 Year":
					minPos(&out.SavingsPlan1Y, sp.RetailPrice*HoursPerMonth)
				case "3 Years":
					minPos(&out.SavingsPlan3Y, sp.RetailPrice*HoursPerMonth)
				}
			}
		case "Reservation":
			switch it.ReservationTerm {
			case "1 Year":
				minPos(&out.Reservation1Y, it.RetailPrice/12)
			case "3 Years":
				minPos(&out.Reservation3Y, it.RetailPrice/36)
			}
		}
	}
	return out
}

type cachedCommitment struct {
	prices CommitmentPrices
	at     time.Time
}

var (
	commitmentMu    sync.Mutex
	commitmentCache = map[string]cachedCommitment{}
)

const commitmentTTL = 24 * time.Hour

// FetchCommitmentPrices consulta (com cache de 24 h) os preços do SKU (ex.: Standard_F4s_v2) na
// região. Usa armSkuName — o skuName ("F4s v2") falha para alguns SKUs v5/v6.
func FetchCommitmentPrices(ctx context.Context, vmSize, region string) (CommitmentPrices, error) {
	vmSize = normalizeVMSize(vmSize)
	if region == "" {
		region = defaultPricingRegion
	}
	key := region + "/" + vmSize
	commitmentMu.Lock()
	if c, ok := commitmentCache[key]; ok && time.Since(c.at) < commitmentTTL {
		commitmentMu.Unlock()
		return c.prices, nil
	}
	commitmentMu.Unlock()

	// Duas consultas: sem priceType a API só devolve o sob demanda (com o savingsPlan embutido); os
	// itens de reserva só vêm com priceType eq 'Reservation' explícito.
	base := fmt.Sprintf("serviceName eq 'Virtual Machines' and armRegionName eq '%s' and armSkuName eq '%s'", region, vmSize)
	var items []retailCommitmentItem
	for _, filter := range []string{base, base + " and priceType eq 'Reservation'"} {
		part, err := fetchRetailItems(ctx, filter)
		if err != nil {
			return CommitmentPrices{}, err
		}
		items = append(items, part...)
	}
	prices := parseCommitmentItems(items)
	if prices.OnDemand == 0 {
		return CommitmentPrices{}, fmt.Errorf("sem preço sob demanda para %s em %s", vmSize, region)
	}
	commitmentMu.Lock()
	commitmentCache[key] = cachedCommitment{prices: prices, at: time.Now()}
	commitmentMu.Unlock()
	return prices, nil
}

// fetchRetailItems pagina uma consulta da Retail Prices API.
func fetchRetailItems(ctx context.Context, filter string) ([]retailCommitmentItem, error) {
	next := azurePricingAPIURL + "?api-version=2023-01-01-preview&$filter=" + url.QueryEscape(filter)
	client := &http.Client{Timeout: 20 * time.Second}
	var items []retailCommitmentItem
	for page := 0; next != "" && page < 5; page++ {
		if !strings.HasPrefix(next, azurePricingAPIURL) {
			return nil, fmt.Errorf("NextPageLink inesperado: %s", next)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Azure Retail Prices: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Azure Retail Prices retornou status %d", resp.StatusCode)
		}
		var pg struct {
			Items    []retailCommitmentItem `json:"Items"`
			NextPage string                 `json:"NextPageLink"`
		}
		if err := json.Unmarshal(body, &pg); err != nil {
			return nil, fmt.Errorf("decodificar Azure Retail Prices: %w", err)
		}
		items = append(items, pg.Items...)
		next = pg.NextPage
	}
	return items, nil
}
