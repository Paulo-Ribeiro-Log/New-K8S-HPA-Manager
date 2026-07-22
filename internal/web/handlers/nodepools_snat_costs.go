package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"

	"github.com/gin-gonic/gin"
)

// SNATCostInfo preços de referência para o diagnóstico SNAT por cloud provider
type SNATCostInfo struct {
	Cluster        string    `json:"cluster"`
	CloudProvider  string    `json:"cloud_provider"`
	IPPriceMonthly float64   `json:"ip_price_monthly"`  // custo mensal por IP (na moeda informada)
	GWHourlyPrice  float64   `json:"gw_hourly_price"`   // custo por hora do NAT gateway (GKE/EKS)
	DataPricePerGB float64   `json:"data_price_per_gb"` // custo por GB processado (GKE/EKS)
	Currency       string    `json:"currency"`          // "BRL" (AKS) ou "USD" (GKE/EKS)
	PricingRegion  string    `json:"pricing_region"`
	Source         string    `json:"source"` // "azure-retail-api", "gcp-billing-api", "aws-pricing-api", "reference"
	FetchedAt      time.Time `json:"fetched_at"`
	Error          string    `json:"error,omitempty"`
}

// GetSNATCosts retorna preços de referência para o diagnóstico SNAT, usando a API de
// pricing nativa de cada cloud provider.
// GET /api/v1/nodepools/snat/costs?cluster=<cluster>
func (h *NodePoolHandler) GetSNATCosts(c *gin.Context) {
	cluster := c.Query("cluster")
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster é obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	provider := detectSNATProvider(h.kubeManager.GetServerURL(cluster), cluster)
	var info SNATCostInfo

	switch provider {
	case "aks":
		info = fetchAKSIPCosts(ctx)
	case "gke":
		info = fetchGKECosts(ctx, cluster)
	case "eks":
		profile, regionHint := "", ""
		if eksCfg := h.kubeManager.GetEKSClusterConfig(cluster); eksCfg != nil {
			profile = eksCfg.AwsProfile
			regionHint = eksCfg.AwsRegion
		}
		info = fetchEKSCosts(ctx, cluster, profile, regionHint)
	default:
		info = SNATCostInfo{Source: "reference", Error: "provider desconhecido"}
	}

	info.Cluster = cluster
	info.CloudProvider = provider
	info.FetchedAt = time.Now()

	c.JSON(http.StatusOK, info)
}

// ─── AKS — Azure Retail Prices API ───────────────────────────────────────────
// API pública, sem autenticação. Retorna preços em BRL para brazilsouth.

type azureRetailItem struct {
	RetailPrice   float64 `json:"retailPrice"`
	UnitOfMeasure string  `json:"unitOfMeasure"`
	SkuName       string  `json:"skuName"`
	ProductName   string  `json:"productName"`
	ArmRegionName string  `json:"armRegionName"`
	CurrencyCode  string  `json:"currencyCode"`
}

type azureRetailResponse struct {
	Items []azureRetailItem `json:"Items"`
}

func fetchAKSIPCosts(ctx context.Context) SNATCostInfo {
	// Filter: IP público Standard em brazilsouth, preço em BRL
	filter := "serviceFamily eq 'Networking' and armRegionName eq 'brazilsouth' and productName eq 'IP Addresses'"
	reqURL := "https://prices.azure.com/api/retail/prices?api-version=2023-01-01-preview&currencyCode=BRL&$filter=" + url.QueryEscape(filter)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return azureFallbackCosts(fmt.Sprintf("criar request: %v", err))
	}

	resp, err := client.Do(req)
	if err != nil {
		return azureFallbackCosts(fmt.Sprintf("Azure Retail Prices API: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return azureFallbackCosts(fmt.Sprintf("Azure Retail Prices API status %d", resp.StatusCode))
	}

	var result azureRetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return azureFallbackCosts(fmt.Sprintf("decodificar resposta: %v", err))
	}

	// Pegar o preço por hora do IP Standard; multiplicar por 730h para mensal
	var hourlyPrice float64
	for _, item := range result.Items {
		name := strings.ToLower(item.SkuName)
		unit := strings.ToLower(item.UnitOfMeasure)
		if !strings.Contains(unit, "hour") && !strings.Contains(unit, "1 hour") {
			continue
		}
		if (strings.Contains(name, "standard") || strings.Contains(name, "static")) && item.RetailPrice > 0 {
			if hourlyPrice == 0 || item.RetailPrice < hourlyPrice {
				hourlyPrice = item.RetailPrice
			}
		}
	}

	if hourlyPrice == 0 {
		return azureFallbackCosts("nenhum preço encontrado na Azure Retail Prices API")
	}

	return SNATCostInfo{
		IPPriceMonthly: hourlyPrice * 730,
		Currency:       "BRL",
		PricingRegion:  "brazilsouth",
		Source:         "azure-retail-api",
	}
}

func azureFallbackCosts(errMsg string) SNATCostInfo {
	return SNATCostInfo{
		IPPriceMonthly: 20.0, // IP público Standard Azure Brasil Sul — referência documental
		Currency:       "BRL",
		PricingRegion:  "brazilsouth",
		Source:         "reference",
		Error:          errMsg,
	}
}

// ─── GKE — Google Cloud Billing Catalog API ──────────────────────────────────
// Requer ADC token. Busca SKUs de Cloud NAT e External IP em southamerica-east1.

// gcpSkuResponse é a resposta da Cloud Billing Catalog API
type gcpSkuResponse struct {
	Skus []struct {
		Description    string   `json:"description"`
		ServiceRegions []string `json:"serviceRegions"`
		PricingInfo    []struct {
			PricingExpression struct {
				UsageUnit   string `json:"usageUnit"`
				TieredRates []struct {
					StartUsageAmount float64 `json:"startUsageAmount"`
					UnitPrice        struct {
						CurrencyCode string `json:"currencyCode"`
						Units        string `json:"units"`
						Nanos        int64  `json:"nanos"`
					} `json:"unitPrice"`
				} `json:"tieredRates"`
			} `json:"pricingExpression"`
		} `json:"pricingInfo"`
	} `json:"skus"`
}

// gcpSkuUSD extrai o preço unitário em USD da primeira tiered rate com startUsageAmount == 0
func gcpSkuUSD(skuPricingInfo interface{}) float64 {
	type pricingShape struct {
		PricingExpression struct {
			TieredRates []struct {
				StartUsageAmount float64 `json:"startUsageAmount"`
				UnitPrice        struct {
					Nanos int64 `json:"nanos"`
				} `json:"unitPrice"`
			} `json:"tieredRates"`
		} `json:"pricingExpression"`
	}
	b, err := json.Marshal(skuPricingInfo)
	if err != nil {
		return 0
	}
	var p pricingShape
	if err := json.Unmarshal(b, &p); err != nil {
		return 0
	}
	for _, rate := range p.PricingExpression.TieredRates {
		if rate.StartUsageAmount == 0 {
			return float64(rate.UnitPrice.Nanos) / 1e9
		}
	}
	return 0
}

func fetchGKECosts(ctx context.Context, clusterCtx string) SNATCostInfo {
	token := gcpprovider.GetFreshGKEToken(ctx)
	if token == "" {
		return gkeFallbackCosts("token ADC não disponível — autentique com gcloud")
	}

	// Cloud NAT e External IP estão no serviço de Networking (95FF-2EF5-5EA1)
	// e Compute Engine (6F81-5844-456A). Tentamos Networking primeiro.
	serviceIDs := []string{"95FF-2EF5-5EA1", "6F81-5844-456A"}

	var gwHourly, dataPerGB, externalIPHourly float64

	for _, svcID := range serviceIDs {
		skuURL := fmt.Sprintf("https://cloudbilling.googleapis.com/v1/services/%s/skus?pageSize=500&currencyCode=USD", svcID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, skuURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}

		var skuResp gcpSkuResponse
		if err := json.Unmarshal(body, &skuResp); err != nil {
			continue
		}

		for _, sku := range skuResp.Skus {
			desc := strings.ToLower(sku.Description)
			if len(sku.PricingInfo) == 0 {
				continue
			}
			price := gcpSkuPriceUSD(sku.PricingInfo[0].PricingExpression.TieredRates)
			if price == 0 {
				continue
			}

			switch {
			case strings.Contains(desc, "cloud nat gateway") && gwHourly == 0:
				gwHourly = price
			case strings.Contains(desc, "cloud nat") && strings.Contains(desc, "data") && dataPerGB == 0:
				dataPerGB = price
			case (strings.Contains(desc, "external ip") || strings.Contains(desc, "static ip")) &&
				strings.Contains(desc, "in use") && externalIPHourly == 0:
				externalIPHourly = price
			}
		}

		if gwHourly > 0 {
			break // encontrou o serviço correto
		}
	}

	if gwHourly == 0 && externalIPHourly == 0 {
		return gkeFallbackCosts("nenhum SKU Cloud NAT encontrado na GCP Billing Catalog API")
	}

	ipMonthly := externalIPHourly * 730
	if ipMonthly == 0 {
		ipMonthly = 0.004 * 730 // fallback documental: $0.004/hr
	}
	if gwHourly == 0 {
		gwHourly = 0.044 // fallback documental
	}
	if dataPerGB == 0 {
		dataPerGB = 0.045 // fallback documental
	}

	return SNATCostInfo{
		IPPriceMonthly: ipMonthly,
		GWHourlyPrice:  gwHourly,
		DataPricePerGB: dataPerGB,
		Currency:       "USD",
		PricingRegion:  "southamerica-east1",
		Source:         "gcp-billing-api",
	}
}

// gcpSkuPriceUSD extrai o preço USD da tiered rate com startUsageAmount == 0
func gcpSkuPriceUSD(tieredRates []struct {
	StartUsageAmount float64 `json:"startUsageAmount"`
	UnitPrice        struct {
		CurrencyCode string `json:"currencyCode"`
		Units        string `json:"units"`
		Nanos        int64  `json:"nanos"`
	} `json:"unitPrice"`
}) float64 {
	for _, rate := range tieredRates {
		if rate.StartUsageAmount == 0 {
			return float64(rate.UnitPrice.Nanos) / 1e9
		}
	}
	return 0
}

func gkeFallbackCosts(errMsg string) SNATCostInfo {
	return SNATCostInfo{
		IPPriceMonthly: 0.004 * 730, // $0.004/hr × 730h — External IP in use (southamerica-east1)
		GWHourlyPrice:  0.044,       // Cloud NAT Gateway por hora
		DataPricePerGB: 0.045,       // Cloud NAT Data Processing por GB
		Currency:       "USD",
		PricingRegion:  "southamerica-east1",
		Source:         "reference",
		Error:          errMsg,
	}
}

// ─── EKS — AWS Pricing API via CLI ──────────────────────────────────────────
// Usa `aws pricing get-products --service-code AmazonVPC` (sempre us-east-1 para pricing).
// Extrai preços de NAT Gateway horário e por GB processado.

func fetchEKSCosts(ctx context.Context, clusterCtx, profile, regionHint string) SNATCostInfo {
	// extractAWSRegionFromARN só funciona quando clusterCtx é o ARN completo — contexts EKS
	// com nome amigável (ex: "asaplog-production-admin", sem prefixo arn:aws:eks:) caem no
	// regionHint (região salva em EKSClusterConfig) antes do fallback fixo us-east-1. Isso é
	// só o valor exibido em PricingRegion — a chamada à Pricing API em si é sempre em
	// us-east-1 (única região onde essa API está disponível).
	region := extractAWSRegionFromARN(clusterCtx)
	if region == "" {
		region = regionHint
	}
	if region == "" {
		region = "us-east-1"
	}

	// AWS Pricing API só está disponível na região us-east-1
	args := []string{
		"pricing", "get-products",
		"--service-code", "AmazonVPC",
		"--region", "us-east-1",
		"--filters",
		"Type=TERM_MATCH,Field=servicecode,Value=AmazonVPC",
		"--format-version", "aws_v1",
		"--output", "json",
		"--page-size", "100",
	}
	// Sem --profile, a chamada usa o profile default do ambiente, que pode não ter a
	// permissão IAM pricing:GetProducts na conta específica desse cluster (ambientes com
	// múltiplas contas/profiles AWS por cluster) — mesmo profile já resolvido para as
	// demais chamadas EKS (buildSNATProfileEKS).
	if profile != "" {
		args = append(args, "--profile", profile)
	}

	out, err := exec.CommandContext(ctx, "aws", args...).Output()
	if err != nil {
		return eksFallbackCosts(fmt.Sprintf("aws pricing get-products: %v", err))
	}

	var priceResp struct {
		PriceList []string `json:"PriceList"`
	}
	if err := json.Unmarshal(out, &priceResp); err != nil {
		return eksFallbackCosts(fmt.Sprintf("decodificar resposta AWS pricing: %v", err))
	}

	var gwHourly, dataPerGB float64

	for _, pl := range priceResp.PriceList {
		var item map[string]interface{}
		if err := json.Unmarshal([]byte(pl), &item); err != nil {
			continue
		}

		product, _ := item["product"].(map[string]interface{})
		if product == nil {
			continue
		}
		attrs, _ := product["attributes"].(map[string]interface{})
		if attrs == nil {
			continue
		}
		group, _ := attrs["group"].(string)
		loc, _ := attrs["location"].(string)

		// Filtrar pela localização desejada (aproximação: us-east-1 como referência)
		if !strings.Contains(strings.ToLower(loc), "virginia") && !strings.Contains(strings.ToLower(loc), "east") {
			// Aceitar qualquer localização quando não encontrar a desejada
			if gwHourly > 0 && dataPerGB > 0 {
				continue
			}
		}

		price := extractAWSPrice(item)
		if price == 0 {
			continue
		}

		switch {
		case group == "NGW:NatGateway" && gwHourly == 0:
			gwHourly = price
		case group == "NGW:Data" && dataPerGB == 0:
			dataPerGB = price
		}

		if gwHourly > 0 && dataPerGB > 0 {
			break
		}
	}

	if gwHourly == 0 && dataPerGB == 0 {
		return eksFallbackCosts("nenhum preço NAT Gateway encontrado na AWS Pricing API")
	}
	if gwHourly == 0 {
		gwHourly = 0.044
	}
	if dataPerGB == 0 {
		dataPerGB = 0.044
	}

	return SNATCostInfo{
		GWHourlyPrice:  gwHourly,
		DataPricePerGB: dataPerGB,
		Currency:       "USD",
		PricingRegion:  region,
		Source:         "aws-pricing-api",
	}
}

// extractAWSPrice navega na estrutura de termos AWS OnDemand para extrair o preço unitário
func extractAWSPrice(item map[string]interface{}) float64 {
	terms, _ := item["terms"].(map[string]interface{})
	if terms == nil {
		return 0
	}
	onDemand, _ := terms["OnDemand"].(map[string]interface{})
	if onDemand == nil {
		return 0
	}
	for _, offerRaw := range onDemand {
		offer, _ := offerRaw.(map[string]interface{})
		if offer == nil {
			continue
		}
		priceDims, _ := offer["priceDimensions"].(map[string]interface{})
		if priceDims == nil {
			continue
		}
		for _, dimRaw := range priceDims {
			dim, _ := dimRaw.(map[string]interface{})
			if dim == nil {
				continue
			}
			pricePerUnit, _ := dim["pricePerUnit"].(map[string]interface{})
			usdStr, _ := pricePerUnit["USD"].(string)
			if usdStr == "" {
				continue
			}
			var price float64
			fmt.Sscanf(usdStr, "%f", &price)
			return price
		}
	}
	return 0
}

// extractAWSRegionFromARN extrai a região de um ARN EKS
// arn:aws:eks:us-east-1:123456:cluster/name → "us-east-1"
func extractAWSRegionFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func eksFallbackCosts(errMsg string) SNATCostInfo {
	return SNATCostInfo{
		GWHourlyPrice:  0.044, // NAT Gateway por hora (us-east-1 referência)
		DataPricePerGB: 0.044, // por GB processado
		Currency:       "USD",
		PricingRegion:  "us-east-1",
		Source:         "reference",
		Error:          errMsg,
	}
}
