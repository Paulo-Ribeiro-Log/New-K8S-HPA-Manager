package finops

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

const (
	azurePricingAPIURL  = "https://prices.azure.com/api/retail/prices"
	pricingCacheTTL     = 24 * time.Hour
	defaultPricingRegion = "brazilsouth"
)

// vmSpecs contém (vCPU, RAM GB) por SKU — necessário para calcular capacidade do cluster.
// Complementado com dados da Azure Pricing API quando disponível.
var vmSpecs = map[string][2]int{
	"Standard_B2s":     {2, 4},
	"Standard_B4ms":    {4, 16},
	"Standard_B8ms":    {8, 32},
	"Standard_D2s_v3":  {2, 8},
	"Standard_D4s_v3":  {4, 16},
	"Standard_D8s_v3":  {8, 32},
	"Standard_D16s_v3": {16, 64},
	"Standard_D32s_v3": {32, 128},
	"Standard_D2s_v4":  {2, 8},
	"Standard_D4s_v4":  {4, 16},
	"Standard_D8s_v4":  {8, 32},
	"Standard_D16s_v4": {16, 64},
	"Standard_D2s_v5":  {2, 8},
	"Standard_D4s_v5":  {4, 16},
	"Standard_D8s_v5":  {8, 32},
	"Standard_D16s_v5": {16, 64},
	"Standard_E4s_v3":  {4, 32},
	"Standard_E8s_v3":  {8, 64},
	"Standard_E16s_v3": {16, 128},
	"Standard_E4s_v4":  {4, 32},
	"Standard_E8s_v4":  {8, 64},
	"Standard_E16s_v4": {16, 128},
	"Standard_E4s_v5":  {4, 32},
	"Standard_E8s_v5":  {8, 64},
	"Standard_F4s_v2":  {4, 8},
	"Standard_F8s_v2":  {8, 16},
	"Standard_F16s_v2": {16, 32},
}

// fallbackPrices são preços pay-as-you-go Brasil Sul (USD/hora) usados quando a API falha.
// Referência: https://azure.microsoft.com/pt-br/pricing/details/virtual-machines/linux/
var fallbackPrices = map[string]float64{
	"Standard_B2s":     0.050,
	"Standard_B4ms":    0.166,
	"Standard_B8ms":    0.333,
	"Standard_D2s_v3":  0.096,
	"Standard_D4s_v3":  0.192,
	"Standard_D8s_v3":  0.384,
	"Standard_D16s_v3": 0.768,
	"Standard_D32s_v3": 1.536,
	"Standard_D2s_v4":  0.096,
	"Standard_D4s_v4":  0.192,
	"Standard_D8s_v4":  0.384,
	"Standard_D16s_v4": 0.768,
	"Standard_D2s_v5":  0.096,
	"Standard_D4s_v5":  0.192,
	"Standard_D8s_v5":  0.384,
	"Standard_D16s_v5": 0.768,
	"Standard_E4s_v3":  0.252,
	"Standard_E8s_v3":  0.504,
	"Standard_E16s_v3": 1.008,
	"Standard_E4s_v4":  0.252,
	"Standard_E8s_v4":  0.504,
	"Standard_E16s_v4": 1.008,
	"Standard_E4s_v5":  0.252,
	"Standard_E8s_v5":  0.504,
	"Standard_F4s_v2":  0.169,
	"Standard_F8s_v2":  0.338,
	"Standard_F16s_v2": 0.676,
}

// azurePriceItem representa um item da resposta da Azure Pricing API
type azurePriceItem struct {
	SKUName      string  `json:"skuName"`
	RetailPrice  float64 `json:"retailPrice"`
	UnitOfMeasure string `json:"unitOfMeasure"`
	ArmRegionName string `json:"armRegionName"`
	ProductName  string  `json:"productName"`
	MeterName    string  `json:"meterName"`
}

type azurePricingResponse struct {
	Items    []azurePriceItem `json:"Items"`
	NextPage string           `json:"NextPageLink"`
}

// AzurePricer busca e armazena em cache os preços de VMs Azure
type AzurePricer struct {
	db     *sql.DB
	mu     sync.RWMutex
	region string
}

// NewAzurePricer cria um novo pricer com cache SQLite
func NewAzurePricer(region string) (*AzurePricer, error) {
	if region == "" {
		region = defaultPricingRegion
	}

	dbDir := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("criar diretório de cache: %w", err)
	}

	dbPath := filepath.Join(dbDir, "finops_pricing_cache.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("abrir banco de preços: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS finops_pricing_cache (
			sku        TEXT NOT NULL,
			region     TEXT NOT NULL,
			price_usd  REAL NOT NULL,
			fetched_at DATETIME NOT NULL,
			PRIMARY KEY (sku, region)
		)`)
	if err != nil {
		return nil, fmt.Errorf("criar schema de preços: %w", err)
	}

	return &AzurePricer{db: db, region: region}, nil
}

// GetPrice retorna o preço USD/hora de um VM SKU, buscando na API se não estiver em cache.
func (p *AzurePricer) GetPrice(vmSize string) (price float64, source string, err error) {
	// 1. Verificar cache SQLite
	cached, err := p.getFromCache(vmSize)
	if err == nil {
		return cached, "api", nil
	}

	// 2. Buscar na Azure Pricing API
	apiPrice, apiErr := p.fetchFromAPI(vmSize)
	if apiErr == nil {
		p.saveToCache(vmSize, apiPrice)
		return apiPrice, "api", nil
	}

	log.Warn().Str("vm_size", vmSize).Err(apiErr).Msg("Falha na Azure Pricing API, usando fallback")

	// 3. Fallback local
	if fb, ok := fallbackPrices[vmSize]; ok {
		return fb, "fallback", nil
	}

	// 4. Fallback genérico por família de VM
	if fb := estimatePriceByFamily(vmSize); fb > 0 {
		return fb, "fallback", nil
	}

	return 0, "unknown", fmt.Errorf("preço não encontrado para SKU: %s", vmSize)
}

// GetVMSpecs retorna (vCPU, RAM GB) para um SKU. Retorna (0,0) se desconhecido.
func GetVMSpecs(vmSize string) (cpuCores, memGB int) {
	if specs, ok := vmSpecs[vmSize]; ok {
		return specs[0], specs[1]
	}
	// Tenta inferir pela nomenclatura Azure (ex: Standard_D4s_v3 → 4 vCPUs)
	return inferSpecsFromName(vmSize)
}

// InvalidateCache remove todos os preços do cache para forçar re-busca
func (p *AzurePricer) InvalidateCache() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.db.Exec(`DELETE FROM finops_pricing_cache WHERE region = ?`, p.region)
	return err
}

// Close fecha a conexão com o banco de cache
func (p *AzurePricer) Close() error {
	return p.db.Close()
}

// getFromCache busca o preço no cache SQLite. Retorna erro se não encontrado ou expirado.
func (p *AzurePricer) getFromCache(vmSize string) (float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var price float64
	var fetchedAt time.Time

	err := p.db.QueryRow(
		`SELECT price_usd, fetched_at FROM finops_pricing_cache WHERE sku = ? AND region = ?`,
		vmSize, p.region,
	).Scan(&price, &fetchedAt)

	if err != nil {
		return 0, err
	}
	if time.Since(fetchedAt) > pricingCacheTTL {
		return 0, fmt.Errorf("cache expirado para %s", vmSize)
	}

	return price, nil
}

// saveToCache persiste o preço no cache SQLite
func (p *AzurePricer) saveToCache(vmSize string, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, err := p.db.Exec(
		`INSERT INTO finops_pricing_cache (sku, region, price_usd, fetched_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(sku, region) DO UPDATE SET price_usd = excluded.price_usd, fetched_at = excluded.fetched_at`,
		vmSize, p.region, price, time.Now(),
	)
	if err != nil {
		log.Warn().Err(err).Str("sku", vmSize).Msg("Falha ao salvar preço no cache")
	}
}

// fetchFromAPI consulta a Azure Retail Prices API para obter o preço de um SKU
// Documentação: https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices
func (p *AzurePricer) fetchFromAPI(vmSize string) (float64, error) {
	skuName := vmSizeToSKUName(vmSize)

	filter := fmt.Sprintf(
		"serviceName eq 'Virtual Machines' and armRegionName eq '%s' and skuName eq '%s' and priceType eq 'Consumption' and contains(meterName, 'Spot') eq false",
		p.region, skuName,
	)

	reqURL := azurePricingAPIURL + "?api-version=2023-01-01-preview&$filter=" + url.QueryEscape(filter)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return 0, fmt.Errorf("requisição Azure Pricing API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Azure Pricing API retornou status %d", resp.StatusCode)
	}

	var result azurePricingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decodificar resposta Azure Pricing API: %w", err)
	}

	// Filtrar apenas VMs Linux (sem Windows) e pegar o menor preço (pay-as-you-go)
	var bestPrice float64
	for _, item := range result.Items {
		if item.UnitOfMeasure != "1 Hour" {
			continue
		}
		// Ignorar preços Windows (contêm "Windows" no ProductName)
		if strings.Contains(strings.ToLower(item.ProductName), "windows") {
			continue
		}
		// Ignorar Low Priority e Spot
		if strings.Contains(strings.ToLower(item.MeterName), "low priority") ||
			strings.Contains(strings.ToLower(item.MeterName), "spot") {
			continue
		}
		if item.RetailPrice > 0 && (bestPrice == 0 || item.RetailPrice < bestPrice) {
			bestPrice = item.RetailPrice
		}
	}

	if bestPrice == 0 {
		return 0, fmt.Errorf("nenhum preço encontrado para SKU '%s' na região '%s'", skuName, p.region)
	}

	log.Info().
		Str("vm_size", vmSize).
		Str("sku_name", skuName).
		Str("region", p.region).
		Float64("price_usd_hour", bestPrice).
		Msg("Preço obtido da Azure Pricing API")

	return bestPrice, nil
}

// vmSizeToSKUName converte o nome interno do Azure para o skuName da Pricing API
// Ex: "Standard_D4s_v3" → "D4s v3"
func vmSizeToSKUName(vmSize string) string {
	// Remove prefixo "Standard_", "Basic_", "Premium_"
	name := vmSize
	for _, prefix := range []string{"Standard_", "Basic_", "Premium_"} {
		name = strings.TrimPrefix(name, prefix)
	}
	// Substitui underscores por espaços
	return strings.ReplaceAll(name, "_", " ")
}

// estimatePriceByFamily tenta estimar o preço com base na família de VM
// quando o SKU exato não está na tabela fallback. Retorna 0 se não conseguir inferir.
func estimatePriceByFamily(vmSize string) float64 {
	size := strings.ToLower(vmSize)

	// Tenta inferir família e número de vCPUs para estimar custo
	// Preço base aproximado por família (USD/vCPU/hora)
	familyBasePricePerCPU := map[string]float64{
		"standard_d": 0.048,
		"standard_e": 0.063,
		"standard_f": 0.042,
		"standard_b": 0.025,
	}

	for prefix, pricePerCPU := range familyBasePricePerCPU {
		if strings.HasPrefix(size, prefix) {
			cpus, _ := GetVMSpecs(vmSize)
			if cpus > 0 {
				return float64(cpus) * pricePerCPU
			}
		}
	}
	return 0
}

// inferSpecsFromName tenta inferir vCPUs e RAM do nome do SKU quando não está na tabela.
// Funciona para nomenclaturas padrão Azure: D4s → 4 vCPUs.
func inferSpecsFromName(vmSize string) (cpuCores, memGB int) {
	// Remove prefixo "Standard_"
	name := strings.TrimPrefix(vmSize, "Standard_")

	// Extrai número após letra(s) de família (ex: D4s_v3 → 4)
	var cpus int
	for i, c := range name {
		if c >= '0' && c <= '9' {
			fmt.Sscanf(name[i:], "%d", &cpus)
			break
		}
	}
	if cpus == 0 {
		return 0, 0
	}

	// Estimativa de RAM por família
	familyMemRatio := map[byte]int{
		'D': 4,  // D-series: 4 GB por vCPU
		'E': 8,  // E-series: 8 GB por vCPU (memory-optimized)
		'F': 2,  // F-series: 2 GB por vCPU (compute-optimized)
		'B': 2,  // B-series: varia, usar 2 GB como base
	}

	family := byte(strings.ToUpper(name)[0])
	ratio, ok := familyMemRatio[family]
	if !ok {
		ratio = 4 // padrão
	}

	return cpus, cpus * ratio
}
