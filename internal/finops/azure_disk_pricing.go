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

const diskCacheTTL = 24 * time.Hour

// diskTierDef descreve um tier de disco managed com sua capacidade mínima em GB
type diskTierDef struct {
	Tier string
	GB   float64
}

// managedDiskTiers mapeia tipo de disco → tiers em ordem crescente de capacidade
var managedDiskTiers = map[string][]diskTierDef{
	"Premium SSD": {
		{"P1", 4}, {"P2", 8}, {"P3", 16}, {"P4", 32}, {"P6", 64}, {"P10", 128},
		{"P15", 256}, {"P20", 512}, {"P30", 1024}, {"P40", 2048}, {"P50", 4096},
	},
	"Standard SSD": {
		{"E1", 4}, {"E2", 8}, {"E3", 16}, {"E4", 32}, {"E6", 64}, {"E10", 128},
		{"E15", 256}, {"E20", 512}, {"E30", 1024}, {"E40", 2048}, {"E50", 4096},
	},
	"Standard HDD": {
		{"S4", 32}, {"S6", 64}, {"S10", 128}, {"S15", 256}, {"S20", 512},
		{"S30", 1024}, {"S40", 2048}, {"S50", 4096},
	},
}

// diskFallbackPrices contém preços USD/mês por disco por tier (brazilsouth, LRS, jan/2026)
// Referência: https://azure.microsoft.com/pricing/details/managed-disks/
var diskFallbackPrices = map[string]float64{
	// Premium SSD (P-series)
	"Premium SSD/P1": 1.54, "Premium SSD/P2": 3.08, "Premium SSD/P3": 5.28,
	"Premium SSD/P4": 5.28, "Premium SSD/P6": 10.22, "Premium SSD/P10": 19.71,
	"Premium SSD/P15": 38.09, "Premium SSD/P20": 73.22, "Premium SSD/P30": 135.17,
	"Premium SSD/P40": 254.56, "Premium SSD/P50": 461.03,
	// Standard SSD (E-series)
	"Standard SSD/E1": 0.60, "Standard SSD/E2": 1.20, "Standard SSD/E3": 2.16,
	"Standard SSD/E4": 1.92, "Standard SSD/E6": 3.84, "Standard SSD/E10": 7.68,
	"Standard SSD/E15": 14.84, "Standard SSD/E20": 28.56, "Standard SSD/E30": 52.49,
	"Standard SSD/E40": 96.26, "Standard SSD/E50": 183.78,
	// Standard HDD (S-series)
	"Standard HDD/S4": 1.32, "Standard HDD/S6": 2.64, "Standard HDD/S10": 5.28,
	"Standard HDD/S15": 10.21, "Standard HDD/S20": 19.71,
	"Standard HDD/S30": 36.62, "Standard HDD/S40": 70.31, "Standard HDD/S50": 135.17,
}

// filesAndBlobFallbackPerGB contém preços USD/GB/mês para Azure Files e Blob (brazilsouth, LRS, jan/2026)
var filesAndBlobFallbackPerGB = map[string]float64{
	"Azure Files Standard": 0.060,
	"Azure Files Premium":  0.130,
	"Azure Blob Hot":       0.023,
}

// storageClassProvisioners mapeia provisioner → categoria ("disk", "files", "blob")
var storageClassProvisioners = map[string]string{
	"disk.csi.azure.com":        "disk",
	"file.csi.azure.com":        "files",
	"blob.csi.azure.com":        "blob",
	"kubernetes.io/azure-disk":  "disk",
	"kubernetes.io/azure-file":  "files",
}

// skuNameToAzureType mapeia o parâmetro skuName da StorageClass → tipo de disco Azure
var skuNameToAzureType = map[string]string{
	"premium_lrs":      "Premium SSD",
	"premium_zrs":      "Premium SSD",
	"standardssd_lrs":  "Standard SSD",
	"standardssd_zrs":  "Standard SSD",
	"standard_lrs":     "Standard HDD",
}

// storageClassNameHints é a tabela de fallback para mapear nome de SC → tipo Azure
var storageClassNameHints = []struct {
	contains  string
	azureType string
}{
	{"premium", "Premium SSD"},
	{"standard-ssd", "Standard SSD"},
	{"standardssd", "Standard SSD"},
	{"managed-csi", "Standard SSD"}, // AKS default
	{"managed", "Standard HDD"},
	{"standard-hdd", "Standard HDD"},
	{"hdd", "Standard HDD"},
	{"azurefile-premium", "Azure Files Premium"},
	{"azurefile", "Azure Files Standard"},
	{"blob", "Azure Blob Hot"},
	{"blobfuse", "Azure Blob Hot"},
}

// DiskPricer busca e armazena em cache preços de storage Azure (discos managed e Azure Files/Blob)
type DiskPricer struct {
	db     *sql.DB
	mu     sync.RWMutex
	region string
}

// NewDiskPricer cria um DiskPricer que compartilha o mesmo SQLite da VM pricing.
func NewDiskPricer(region string) (*DiskPricer, error) {
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
		return nil, fmt.Errorf("abrir banco de preços de disco: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS disk_pricing_cache (
			disk_type  TEXT NOT NULL,
			tier       TEXT NOT NULL,
			region     TEXT NOT NULL,
			price_usd  REAL NOT NULL,
			fetched_at DATETIME NOT NULL,
			PRIMARY KEY (disk_type, tier, region)
		)`)
	if err != nil {
		return nil, fmt.Errorf("criar schema de preços de disco: %w", err)
	}

	return &DiskPricer{db: db, region: region}, nil
}

// GetDiskPrice retorna o preço USD/mês de um disco managed por tipo e tier.
// Ex: GetDiskPrice("Premium SSD", "P10", "") → (19.71, "api"|"fallback", nil)
func (p *DiskPricer) GetDiskPrice(diskType, tier, region string) (float64, string, error) {
	if region == "" {
		region = p.region
	}

	if cached, err := p.getFromCache(diskType, tier, region); err == nil {
		return cached, "api", nil
	}

	if apiPrice, err := p.fetchDiskFromAPI(diskType, tier, region); err == nil {
		p.saveToCache(diskType, tier, region, apiPrice)
		return apiPrice, "api", nil
	} else {
		log.Warn().Str("disk_type", diskType).Str("tier", tier).Err(err).Msg("FinOps: fallback de preço de disco")
	}

	if fb, ok := diskFallbackPrices[diskType+"/"+tier]; ok {
		return fb, "fallback", nil
	}
	return 0, "unknown", fmt.Errorf("preço não encontrado: %s %s", diskType, tier)
}

// GetFilesPricePerGB retorna o preço USD/GB/mês para Azure Files ou Blob.
// Ex: GetFilesPricePerGB("Azure Files Standard", "") → (0.060, "api"|"fallback", nil)
func (p *DiskPricer) GetFilesPricePerGB(filesType, region string) (float64, string, error) {
	if region == "" {
		region = p.region
	}

	// "per_gb" é o pseudo-tier no cache para serviços não-managed-disk
	if cached, err := p.getFromCache(filesType, "per_gb", region); err == nil {
		return cached, "api", nil
	}

	if apiPrice, err := p.fetchFilesFromAPI(filesType, region); err == nil {
		p.saveToCache(filesType, "per_gb", region, apiPrice)
		return apiPrice, "api", nil
	} else {
		log.Warn().Str("files_type", filesType).Err(err).Msg("FinOps: fallback de preço Files/Blob")
	}

	if fb, ok := filesAndBlobFallbackPerGB[filesType]; ok {
		return fb, "fallback", nil
	}
	return 0, "unknown", fmt.Errorf("preço não encontrado: %s", filesType)
}

// MapStorageClassToAzureType determina o tipo Azure de uma StorageClass.
// Prioridade para files/blob: provisioner primeiro (skuName nesses CSI = tier de replicação LRS/ZRS, não disk type).
// Prioridade para disk: skuName param > provisioner > nome da SC > default.
// Retorna (azureType, method) onde method é "sku_param", "provisioner", "name_hint" ou "default".
func MapStorageClassToAzureType(scName, provisioner, skuName string) (string, string) {
	scLower := strings.ToLower(scName)

	// 1. Provisioner files/blob — tem prioridade máxima porque o skuName nesses CSI drivers
	//    indica replication tier (Standard_LRS, Premium_LRS), não o tipo de armazenamento.
	if provType, ok := storageClassProvisioners[provisioner]; ok {
		switch provType {
		case "files":
			if strings.Contains(scLower, "premium") || strings.Contains(strings.ToLower(skuName), "premium") {
				return "Azure Files Premium", "provisioner"
			}
			return "Azure Files Standard", "provisioner"
		case "blob":
			return "Azure Blob Hot", "provisioner"
		}
		// provType == "disk": continua para sku_param abaixo
	}

	// 2. skuName explícito (apenas para discos managed — disk CSI ou in-tree)
	if skuName != "" {
		lower := strings.ToLower(skuName)
		if azType, ok := skuNameToAzureType[lower]; ok {
			return azType, "sku_param"
		}
		if strings.HasPrefix(lower, "premium") {
			return "Premium SSD", "sku_param"
		}
		if strings.Contains(lower, "standardssd") || (strings.Contains(lower, "standard") && strings.Contains(lower, "ssd")) {
			return "Standard SSD", "sku_param"
		}
	}

	// 3. Nome da StorageClass (hints — ordem importa: mais específico primeiro)
	for _, hint := range storageClassNameHints {
		if strings.Contains(scLower, hint.contains) {
			return hint.azureType, "name_hint"
		}
	}

	// 4. Default AKS (managed-csi → Standard SSD)
	return "Standard SSD", "default"
}

// ResolveManagedDiskTier retorna o tier que cobre a capacidade solicitada.
// Ex: ("Premium SSD", 100) → "P10" (128 GB cobre 100 GB)
// Ex: ("Standard SSD", 32)  → "E4"
func ResolveManagedDiskTier(azureType string, capacityGB float64) string {
	tiers, ok := managedDiskTiers[azureType]
	if !ok {
		tiers = managedDiskTiers["Standard SSD"]
	}
	for _, t := range tiers {
		if t.GB >= capacityGB {
			return t.Tier
		}
	}
	return tiers[len(tiers)-1].Tier
}

// InvalidateDiskCache remove todos os preços de disco do cache para a região configurada.
func (p *DiskPricer) InvalidateDiskCache() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.db.Exec(`DELETE FROM disk_pricing_cache WHERE region = ?`, p.region)
	return err
}

// Close fecha a conexão com o banco de cache.
func (p *DiskPricer) Close() error {
	return p.db.Close()
}

// ── internals ────────────────────────────────────────────────────────────────

func (p *DiskPricer) getFromCache(diskType, tier, region string) (float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var price float64
	var fetchedAt time.Time
	err := p.db.QueryRow(
		`SELECT price_usd, fetched_at FROM disk_pricing_cache WHERE disk_type = ? AND tier = ? AND region = ?`,
		diskType, tier, region,
	).Scan(&price, &fetchedAt)
	if err != nil {
		return 0, err
	}
	if time.Since(fetchedAt) > diskCacheTTL {
		return 0, fmt.Errorf("cache expirado")
	}
	return price, nil
}

func (p *DiskPricer) saveToCache(diskType, tier, region string, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.db.Exec(
		`INSERT INTO disk_pricing_cache (disk_type, tier, region, price_usd, fetched_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(disk_type, tier, region) DO UPDATE SET price_usd = excluded.price_usd, fetched_at = excluded.fetched_at`,
		diskType, tier, region, price, time.Now(),
	)
	if err != nil {
		log.Warn().Err(err).Str("disk_type", diskType).Str("tier", tier).Msg("FinOps: falha ao salvar preço de disco")
	}
}

// fetchDiskFromAPI consulta a Azure Pricing API para managed disks.
// Filter: serviceName eq 'Storage' and productName eq 'Premium SSD Managed Disks' and skuName eq 'P10 LRS'
func (p *DiskPricer) fetchDiskFromAPI(diskType, tier, region string) (float64, error) {
	productName, ok := diskTypeToProductName(diskType)
	if !ok {
		return 0, fmt.Errorf("tipo de disco não suportado: %s", diskType)
	}

	filter := fmt.Sprintf(
		"serviceName eq 'Storage' and armRegionName eq '%s' and productName eq '%s' and skuName eq '%s LRS'",
		region, productName, tier,
	)
	return p.queryPricingAPI(filter, "1/Month")
}

// fetchFilesFromAPI consulta a Azure Pricing API para Azure Files e Blob.
func (p *DiskPricer) fetchFilesFromAPI(filesType, region string) (float64, error) {
	var filter string
	switch filesType {
	case "Azure Files Standard":
		filter = fmt.Sprintf(
			"serviceName eq 'Azure Files' and armRegionName eq '%s' and skuName eq 'LRS' and meterName eq 'LRS Data Stored'",
			region,
		)
	case "Azure Files Premium":
		filter = fmt.Sprintf(
			"serviceName eq 'Azure Files' and armRegionName eq '%s' and skuName eq 'Premium LRS' and meterName eq 'Premium LRS Provisioned'",
			region,
		)
	case "Azure Blob Hot":
		filter = fmt.Sprintf(
			"serviceName eq 'Azure Blob Storage' and armRegionName eq '%s' and skuName eq 'LRS' and meterName eq 'LRS Data Stored'",
			region,
		)
	default:
		return 0, fmt.Errorf("tipo não suportado: %s", filesType)
	}
	return p.queryPricingAPI(filter, "1 GB/Month")
}

// queryPricingAPI executa uma query na Azure Retail Prices API e retorna o menor preço encontrado
func (p *DiskPricer) queryPricingAPI(filter, unitOfMeasure string) (float64, error) {
	reqURL := azurePricingAPIURL + "?api-version=2023-01-01-preview&$filter=" + url.QueryEscape(filter)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return 0, fmt.Errorf("requisição Azure Pricing API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Azure Pricing API status %d", resp.StatusCode)
	}

	var result azurePricingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decodificar resposta: %w", err)
	}

	var bestPrice float64
	for _, item := range result.Items {
		if !strings.EqualFold(item.UnitOfMeasure, unitOfMeasure) {
			continue
		}
		if item.RetailPrice > 0 && (bestPrice == 0 || item.RetailPrice < bestPrice) {
			bestPrice = item.RetailPrice
		}
	}

	if bestPrice == 0 {
		return 0, fmt.Errorf("nenhum preço encontrado (filter=%s)", filter)
	}
	return bestPrice, nil
}

// diskTypeToProductName mapeia tipo interno → nome do produto na Azure Pricing API
func diskTypeToProductName(diskType string) (string, bool) {
	switch diskType {
	case "Premium SSD":
		return "Premium SSD Managed Disks", true
	case "Standard SSD":
		return "Standard SSD Managed Disks", true
	case "Standard HDD":
		return "Standard HDD Managed Disks", true
	}
	return "", false
}
