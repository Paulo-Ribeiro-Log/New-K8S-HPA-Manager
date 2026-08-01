package finops

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"
)

const (
	// gcpComputeEngineServiceID é o ID do serviço "Compute Engine" na Cloud Billing Catalog API —
	// mesmo ID já usado em internal/web/handlers/nodepools_snat_costs.go (SNAT).
	gcpComputeEngineServiceID = "6F81-5844-456A"
	defaultGCPPricingRegion   = "southamerica-east1" // São Paulo — mesma região default do SNAT
	gcpPricingMaxPages        = 20                   // ~5000 SKUs/página — cobre o catálogo inteiro do Compute Engine com folga
)

// gcpMachineFamilies mapeia o prefixo do machine type GCE (minúsculo, como aparece em
// node.spec.instanceType/labels) para o nome exato da família usado nas descrições de SKU da
// Cloud Billing Catalog API ("<família> Instance Core running in <local>" /
// "<família> Instance Ram running in <local>") — confirmado empiricamente contra a API real
// (southamerica-east1, 2026-08-01), não documentação: a API não filtra por família/região no
// servidor, só lista o catálogo inteiro paginado, então o mapeamento local abaixo é obrigatório.
var gcpMachineFamilies = map[string]string{
	"e2":  "E2",
	"n1":  "N1 Predefined",
	"n2":  "N2",
	"n2d": "N2D AMD",
	"n4":  "N4",
	"n4d": "N4D",
	"c2d": "C2D AMD",
	"c3":  "C3",
	"c3d": "C3D",
	"c4":  "C4",
	"c4a": "C4A Arm",
	"c4d": "C4D",
	"t2d": "T2D AMD",
	"a2":  "A2",
	"a3":  "A3",
	"g2":  "G2",
}

// gcpFallbackFamilyPrices são preços USD/hora (core) e USD/GB/hora (ram) de referência pra
// southamerica-east1, capturados ao vivo contra a Cloud Billing Catalog API em 2026-08-01 —
// usados só quando a API está indisponível (mesmo espírito de fallbackPrices em azure_pricing.go).
var gcpFallbackFamilyPrices = map[string][2]float64{
	// family: {core_usd_hour, ram_usd_gb_hour}
	"e2":  {0.0346242, 0.0046403},
	"n1":  {0.0501800, 0.0067250},
	"n2":  {0.0501800, 0.0067250},
	"n2d": {0.0436570, 0.0058510},
	"c2d": {0.0469420, 0.0062860},
	"c3":  {0.0550207, 0.0062532},
	"c3d": {0.0469431, 0.0062865},
	"c4":  {0.0550207, 0.0062532},
	"c4a": {0.0490026, 0.0055735},
	"c4d": {0.0519299, 0.0055506},
	"t2d": {0.0436570, 0.0058510},
	"a2":  {0.0501800, 0.0067250},
	"g2":  {0.0396788, 0.0046485},
}

// gcpSkuEntry é um item da resposta da Cloud Billing Catalog API — subconjunto mínimo dos campos
// usados (mesma API já consultada em nodepools_snat_costs.go, aqui com Description/ServiceRegions
// adicionais para filtrar por família de máquina).
type gcpSkuEntry struct {
	Description    string   `json:"description"`
	ServiceRegions []string `json:"serviceRegions"`
	PricingInfo    []struct {
		PricingExpression struct {
			UsageUnit   string `json:"usageUnit"`
			TieredRates []struct {
				StartUsageAmount float64 `json:"startUsageAmount"`
				UnitPrice        struct {
					Nanos int64 `json:"nanos"`
				} `json:"unitPrice"`
			} `json:"tieredRates"`
		} `json:"pricingExpression"`
	} `json:"pricingInfo"`
}

type gcpSkuListResponse struct {
	Skus          []gcpSkuEntry `json:"skus"`
	NextPageToken string        `json:"nextPageToken"`
}

// GCPPricer busca e armazena em cache os preços de Compute Engine (GKE) via Cloud Billing
// Catalog API. Implementa a interface CloudPricer, mesmo padrão de AzurePricer.
//
// Diferença estrutural importante da Azure: a GCP precifica vCPU e RAM separadamente por família
// de máquina (ex: "E2 Instance Core"/"E2 Instance Ram"), não um preço único por SKU de VM inteiro
// — o preço final de um machine type é core_usd_hour*vCPUs + ram_usd_gb_hour*RAM_GB.
type GCPPricer struct {
	db        *sql.DB
	mu        sync.RWMutex
	region    string
	refreshSF singleflight.Group
}

// NewGCPPricer cria um novo pricer com cache SQLite (mesmo arquivo/banco do AzurePricer, tabela
// própria — nomes de machine type GCE ("e2-standard-4") nunca colidem com SKUs Azure
// ("Standard_D4s_v3"), mas mantemos tabelas separadas por clareza).
func NewGCPPricer(region string) (*GCPPricer, error) {
	if region == "" {
		region = defaultGCPPricingRegion
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
		CREATE TABLE IF NOT EXISTS finops_gcp_pricing_cache (
			family     TEXT NOT NULL,
			region     TEXT NOT NULL,
			core_usd   REAL NOT NULL,
			ram_usd    REAL NOT NULL,
			fetched_at DATETIME NOT NULL,
			PRIMARY KEY (family, region)
		)`)
	if err != nil {
		return nil, fmt.Errorf("criar schema de preços GCP: %w", err)
	}

	return &GCPPricer{db: db, region: region}, nil
}

// parseGCEMachineType interpreta um machine type GCE padrão (ex: "e2-standard-4",
// "n2d-highmem-8") em família + specs. Não cobre machine types "custom"
// (ex: "n2-custom-4-16384") nem famílias de GPU/memory-optimized com nomenclatura irregular
// (A2/A3/M-series) — essas caem no erro de "não interpretado" e o chamador decide o que fazer
// (GKE node pools na prática usam quase sempre standard/highmem/highcpu de e2/n2/n2d/c3/c4).
func parseGCEMachineType(vmSize string) (familyKey string, vcpus int, ramGB float64, ok bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(vmSize)), "-")
	if len(parts) < 3 {
		return "", 0, 0, false
	}
	familyKey = parts[0]
	if _, known := gcpMachineFamilies[familyKey]; !known {
		return "", 0, 0, false
	}

	n, err := strconv.Atoi(parts[2])
	if err != nil || n <= 0 {
		return "", 0, 0, false
	}
	vcpus = n

	switch parts[1] {
	case "standard":
		ramGB = float64(n) * 4
	case "highmem":
		ramGB = float64(n) * 8
	case "highcpu":
		ramGB = float64(n) * 1
	default:
		return "", 0, 0, false
	}

	return familyKey, vcpus, ramGB, true
}

// GetVMSpecs implementa CloudPricer.GetVMSpecs.
func (p *GCPPricer) GetVMSpecs(vmSize string) (cpuCores, memGB int) {
	_, vcpus, ramGB, ok := parseGCEMachineType(vmSize)
	if !ok {
		return 0, 0
	}
	return vcpus, int(ramGB)
}

// GetPrice implementa CloudPricer.GetPrice — preço USD/hora do machine type completo
// (core_usd_hour*vCPUs + ram_usd_gb_hour*RAM_GB).
func (p *GCPPricer) GetPrice(vmSize string) (price float64, source string, err error) {
	familyKey, vcpus, ramGB, ok := parseGCEMachineType(vmSize)
	if !ok {
		return 0, "unknown", fmt.Errorf("machine type GCE não reconhecido (formato esperado: <família>-<standard|highmem|highcpu>-<vCPUs>): %s", vmSize)
	}

	corePrice, ramPrice, cerr := p.getFamilyPrices(familyKey)
	if cerr == nil {
		return float64(vcpus)*corePrice + ramGB*ramPrice, "api", nil
	}

	log.Warn().Str("machine_type", vmSize).Str("region", p.region).Err(cerr).Msg("Falha ao obter preço GCP Compute Engine, usando fallback")

	if fb, ok := gcpFallbackFamilyPrices[familyKey]; ok {
		return float64(vcpus)*fb[0] + ramGB*fb[1], "fallback", nil
	}

	return 0, "unknown", fmt.Errorf("preço não encontrado para machine type: %s", vmSize)
}

// InvalidateCache remove os preços em cache para a região deste pricer.
func (p *GCPPricer) InvalidateCache() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.db.Exec(`DELETE FROM finops_gcp_pricing_cache WHERE region = ?`, p.region)
	return err
}

// Close fecha a conexão com o banco de cache.
func (p *GCPPricer) Close() error {
	return p.db.Close()
}

// getFamilyPrices retorna (core_usd_hour, ram_usd_gb_hour) para a família na região deste pricer,
// buscando do cache SQLite (TTL de 24h, mesma constante pricingCacheTTL do AzurePricer) e
// disparando um refresh do catálogo inteiro quando expirado/ausente.
func (p *GCPPricer) getFamilyPrices(familyKey string) (corePrice, ramPrice float64, err error) {
	if corePrice, ramPrice, ok := p.readCache(familyKey); ok {
		return corePrice, ramPrice, nil
	}

	if refreshErr := p.refreshCatalog(context.Background()); refreshErr != nil {
		// Falha no refresh — tenta usar o que já tiver em cache, mesmo expirado, antes de desistir.
		if corePrice, ramPrice, ok := p.readCacheIgnoringTTL(familyKey); ok {
			return corePrice, ramPrice, nil
		}
		return 0, 0, refreshErr
	}

	if corePrice, ramPrice, ok := p.readCacheIgnoringTTL(familyKey); ok {
		return corePrice, ramPrice, nil
	}
	return 0, 0, fmt.Errorf("família '%s' não encontrada no catálogo GCP Compute Engine pra região %s", gcpMachineFamilies[familyKey], p.region)
}

func (p *GCPPricer) readCache(familyKey string) (core, ram float64, ok bool) {
	core, ram, fetchedAt, found := p.queryCache(familyKey)
	if !found || time.Since(fetchedAt) > pricingCacheTTL {
		return 0, 0, false
	}
	return core, ram, true
}

func (p *GCPPricer) readCacheIgnoringTTL(familyKey string) (core, ram float64, ok bool) {
	core, ram, _, found := p.queryCache(familyKey)
	return core, ram, found
}

func (p *GCPPricer) queryCache(familyKey string) (core, ram float64, fetchedAt time.Time, found bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	err := p.db.QueryRow(
		`SELECT core_usd, ram_usd, fetched_at FROM finops_gcp_pricing_cache WHERE family = ? AND region = ?`,
		familyKey, p.region,
	).Scan(&core, &ram, &fetchedAt)
	return core, ram, fetchedAt, err == nil
}

// refreshCatalog busca o catálogo inteiro de SKUs de Compute Engine (paginado) e recacheia os
// preços core/ram de todas as famílias conhecidas de uma vez — caro (várias chamadas HTTP,
// milhares de SKUs), por isso feito no máximo 1x por TTL (24h) e protegido por singleflight
// contra chamadas concorrentes.
func (p *GCPPricer) refreshCatalog(ctx context.Context) error {
	_, err, _ := p.refreshSF.Do("refresh", func() (interface{}, error) {
		return nil, p.doRefreshCatalog(ctx)
	})
	return err
}

func (p *GCPPricer) doRefreshCatalog(ctx context.Context) error {
	token := gcpprovider.GetFreshGKEToken(ctx)
	if token == "" {
		return fmt.Errorf("token GCP não disponível (autentique via gcloud ou Device Auth Grant)")
	}

	prices := make(map[string][2]float64) // familyKey -> [core_usd_hour, ram_usd_gb_hour]
	client := &http.Client{Timeout: 20 * time.Second}
	pageToken := ""

	for page := 0; page < gcpPricingMaxPages; page++ {
		reqURL := fmt.Sprintf("https://cloudbilling.googleapis.com/v1/services/%s/skus?pageSize=5000&currencyCode=USD", gcpComputeEngineServiceID)
		if pageToken != "" {
			reqURL += "&pageToken=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return fmt.Errorf("criar request GCP Billing Catalog API: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("requisição GCP Billing Catalog API: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("ler resposta GCP Billing Catalog API: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GCP Billing Catalog API retornou status %d", resp.StatusCode)
		}

		var skuResp gcpSkuListResponse
		if err := json.Unmarshal(body, &skuResp); err != nil {
			return fmt.Errorf("decodificar resposta GCP Billing Catalog API: %w", err)
		}

		for _, sku := range skuResp.Skus {
			extractGCPFamilyPrice(sku, p.region, prices)
		}

		if skuResp.NextPageToken == "" {
			break
		}
		pageToken = skuResp.NextPageToken
	}

	if len(prices) == 0 {
		return fmt.Errorf("nenhum SKU de Compute Engine encontrado para a região %s", p.region)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for family, pr := range prices {
		_, dbErr := p.db.Exec(
			`INSERT INTO finops_gcp_pricing_cache (family, region, core_usd, ram_usd, fetched_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(family, region) DO UPDATE SET core_usd = excluded.core_usd, ram_usd = excluded.ram_usd, fetched_at = excluded.fetched_at`,
			family, p.region, pr[0], pr[1], now,
		)
		if dbErr != nil {
			log.Warn().Err(dbErr).Str("family", family).Msg("Falha ao salvar preço GCP no cache")
		}
	}

	log.Info().Int("families", len(prices)).Str("region", p.region).Msg("Catálogo de preços GCP Compute Engine atualizado")
	return nil
}

// extractGCPFamilyPrice popula `out[familyKey]` com o preço de Core (out[0]) ou Ram (out[1]) de
// `sku`, se ela pertencer a uma família conhecida (gcpMachineFamilies), à região informada, e não
// for uma variante Spot/Preemptible/Sole Tenancy/Committed Use/Custom (queremos o preço
// on-demand padrão, mesmo filtro de intenção que o AzurePricer aplica pra VMs Linux
// não-Windows/não-Spot).
func extractGCPFamilyPrice(sku gcpSkuEntry, region string, out map[string][2]float64) {
	regionMatch := false
	for _, r := range sku.ServiceRegions {
		if r == region {
			regionMatch = true
			break
		}
	}
	if !regionMatch {
		return
	}

	desc := sku.Description
	isCore := strings.Contains(desc, "Instance Core")
	isRam := strings.Contains(desc, "Instance Ram")
	if !isCore && !isRam {
		return
	}
	if strings.Contains(desc, "Spot") || strings.Contains(desc, "Preemptible") ||
		strings.Contains(desc, "Sole Tenancy") || strings.Contains(desc, "Committed") ||
		strings.Contains(desc, "Premium") || strings.Contains(desc, "Custom") {
		return
	}

	for familyKey, familyLabel := range gcpMachineFamilies {
		if !strings.HasPrefix(desc, familyLabel+" Instance") {
			continue
		}
		if len(sku.PricingInfo) == 0 || len(sku.PricingInfo[0].PricingExpression.TieredRates) == 0 {
			return
		}
		price := float64(sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos) / 1e9
		if price <= 0 {
			return
		}

		entry := out[familyKey]
		if isCore {
			entry[0] = price
		} else {
			entry[1] = price
		}
		out[familyKey] = entry
		return
	}
}
