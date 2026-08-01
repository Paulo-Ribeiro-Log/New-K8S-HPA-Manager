package finops

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

const (
	awsPricingAPIRegion = "us-east-1" // AWS Price List API só existe nessa região, independente da região sendo consultada
	defaultAWSRegion    = "us-east-1"
	awsCLITimeout       = 20 * time.Second
)

// awsEBSVolumeTypes são os volumeApiName válidos do driver EBS CSI (ebs.csi.aws.com) e do
// in-tree "kubernetes.io/aws-ebs" — mesma chave de parâmetro "type" usada pelo PD CSI do GKE
// (coincidência de nomenclatura entre os dois clouds, não relacionado).
var awsEBSVolumeTypes = map[string]bool{
	"gp2": true, "gp3": true, "io1": true, "io2": true, "st1": true, "sc1": true,
}

// awsFallbackInstancePrices são preços USD/hora on-demand Linux de referência pra us-east-1,
// capturados ao vivo contra a AWS Price List API em 2026-08-01 — usados só quando a API está
// indisponível (mesmo espírito de fallbackPrices em azure_pricing.go / gcpFallbackFamilyPrices).
var awsFallbackInstancePrices = map[string]float64{
	"m5.large":   0.096,
	"m5.xlarge":  0.192,
	"m5.2xlarge": 0.384,
	"c5.large":   0.085,
	"c5.xlarge":  0.17,
	"t3.medium":  0.0416,
	"t3.large":   0.0832,
}

// awsFallbackDiskPrices são preços USD/GB/mês de referência pra us-east-1, capturados ao vivo em
// 2026-08-01.
var awsFallbackDiskPrices = map[string]float64{
	"gp2": 0.10,
	"gp3": 0.08,
	"io1": 0.125,
	"io2": 0.125,
	"st1": 0.045,
	"sc1": 0.015,
}

// Defaults usados quando não há forma de detectar o disco real do node/PVC — mesmo espírito de
// defaultGKEDiskSizeGB/defaultGKEDiskType (calculator.go). gp3 é o tipo default de volume raiz dos
// EKS-optimized AMIs desde ~2023 (antes era gp2); 20GB é o tamanho default de node group gerenciado
// quando não customizado.
const (
	defaultEKSDiskSizeGB = 20
	defaultEKSDiskType   = "gp3"
)

// mapStorageClassToAWSVolumeType determina o tipo de volume EBS (gp2/gp3/io1/io2/st1/sc1) de uma
// StorageClass EKS — mesmo espírito de mapStorageClassToGCPDiskType (gcp_pricing.go), mesma chave
// de parâmetro "type" (coincidência entre os dois clouds, não relacionado). typeParam vem de
// sc.Parameters["type"], usado tanto pelo driver moderno "ebs.csi.aws.com" quanto pelo legado
// in-tree "kubernetes.io/aws-ebs".
//
// Prioridade: parâmetro "type" explícito > nome da SC (hints) > default (gp3, mesmo default real
// dos EKS-optimized AMIs já usado em osDiskCostForPool/calculator.go).
func mapStorageClassToAWSVolumeType(scName, typeParam string) (string, string) {
	if typeParam != "" {
		lower := strings.ToLower(typeParam)
		if awsEBSVolumeTypes[lower] {
			return lower, "type_param"
		}
	}

	scLower := strings.ToLower(scName)
	switch {
	case strings.Contains(scLower, "io2"):
		return "io2", "name_hint"
	case strings.Contains(scLower, "io1"):
		return "io1", "name_hint"
	case strings.Contains(scLower, "gp2"):
		return "gp2", "name_hint"
	case strings.Contains(scLower, "gp3"):
		return "gp3", "name_hint"
	case strings.Contains(scLower, "st1") || strings.Contains(scLower, "throughput"):
		return "st1", "name_hint"
	case strings.Contains(scLower, "sc1") || strings.Contains(scLower, "cold"):
		return "sc1", "name_hint"
	}

	return defaultEKSDiskType, "default"
}

// AWSPricer busca e armazena em cache os preços de instância EC2 (compute) e volume EBS (disco)
// via `aws pricing get-products` (AWS Price List API — só existe na região us-east-1,
// independente da região sendo precificada; mesmo padrão já usado em
// internal/web/handlers/nodepools_snat_costs.go pro NAT Gateway). Implementa CloudPricer, mesmo
// padrão de AzurePricer/GCPPricer.
//
// Diferente do Azure/GCP: não há endpoint REST simples com Bearer token pra essa API — o caminho
// suportado é via `aws` CLI (exec.Command), que resolve as credenciais/assume-role do profile
// configurado. profile determina QUAL conta/credencial autentica a chamada; não faz parte da
// chave de cache (preço é um fato público por região, não por conta/profile).
type AWSPricer struct {
	db            *sql.DB
	mu            sync.RWMutex
	region        string
	profile       string
	refreshSF     singleflight.Group
	diskRefreshSF singleflight.Group
}

// NewAWSPricer cria um novo pricer com cache SQLite (mesmo arquivo dos outros pricers, tabelas
// próprias).
func NewAWSPricer(region, profile string) (*AWSPricer, error) {
	if region == "" {
		region = defaultAWSRegion
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

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS finops_aws_pricing_cache (
			instance_type TEXT NOT NULL,
			region        TEXT NOT NULL,
			price_usd     REAL NOT NULL,
			vcpu          INTEGER NOT NULL,
			mem_gb        REAL NOT NULL,
			fetched_at    DATETIME NOT NULL,
			PRIMARY KEY (instance_type, region)
		)`); err != nil {
		return nil, fmt.Errorf("criar schema de preços AWS (EC2): %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS finops_aws_disk_pricing_cache (
			volume_type   TEXT NOT NULL,
			region        TEXT NOT NULL,
			usd_per_gb_mo REAL NOT NULL,
			fetched_at    DATETIME NOT NULL,
			PRIMARY KEY (volume_type, region)
		)`); err != nil {
		return nil, fmt.Errorf("criar schema de preços de disco AWS (EBS): %w", err)
	}

	return &AWSPricer{db: db, region: region, profile: profile}, nil
}

// GetPrice implementa CloudPricer.GetPrice — preço USD/hora on-demand Linux do instance type.
func (p *AWSPricer) GetPrice(instanceType string) (price float64, source string, err error) {
	if price, _, _, ok := p.readInstanceCache(instanceType); ok {
		return price, "api", nil
	}

	if refreshErr := p.refreshInstance(context.Background(), instanceType); refreshErr != nil {
		if price, _, _, ok := p.readInstanceCacheIgnoringTTL(instanceType); ok {
			return price, "api", nil
		}
		log.Warn().Str("instance_type", instanceType).Str("region", p.region).Err(refreshErr).Msg("Falha ao obter preço EC2, usando fallback")
		if fb, ok := awsFallbackInstancePrices[instanceType]; ok {
			return fb, "fallback", nil
		}
		return 0, "unknown", refreshErr
	}

	if price, _, _, ok := p.readInstanceCacheIgnoringTTL(instanceType); ok {
		return price, "api", nil
	}
	if fb, ok := awsFallbackInstancePrices[instanceType]; ok {
		return fb, "fallback", nil
	}
	return 0, "unknown", fmt.Errorf("preço não encontrado para instance type: %s", instanceType)
}

// GetVMSpecs implementa CloudPricer.GetVMSpecs — (vCPU, RAM GB), vindos da mesma consulta que já
// popula o preço (a AWS Price List API retorna specs e preço no mesmo item).
func (p *AWSPricer) GetVMSpecs(instanceType string) (cpuCores, memGB int) {
	if _, vcpu, mem, ok := p.readInstanceCache(instanceType); ok {
		return vcpu, int(mem)
	}
	if err := p.refreshInstance(context.Background(), instanceType); err != nil {
		return 0, 0
	}
	if _, vcpu, mem, ok := p.readInstanceCacheIgnoringTTL(instanceType); ok {
		return vcpu, int(mem)
	}
	return 0, 0
}

// InvalidateCache remove os preços em cache pra região deste pricer.
func (p *AWSPricer) InvalidateCache() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.db.Exec(`DELETE FROM finops_aws_pricing_cache WHERE region = ?`, p.region); err != nil {
		return err
	}
	_, err := p.db.Exec(`DELETE FROM finops_aws_disk_pricing_cache WHERE region = ?`, p.region)
	return err
}

// Close fecha a conexão com o banco de cache.
func (p *AWSPricer) Close() error {
	return p.db.Close()
}

func (p *AWSPricer) readInstanceCache(instanceType string) (price float64, vcpu int, memGB float64, ok bool) {
	price, vcpu, memGB, fetchedAt, found := p.queryInstanceCache(instanceType)
	if !found || time.Since(fetchedAt) > pricingCacheTTL {
		return 0, 0, 0, false
	}
	return price, vcpu, memGB, true
}

func (p *AWSPricer) readInstanceCacheIgnoringTTL(instanceType string) (price float64, vcpu int, memGB float64, ok bool) {
	price, vcpu, memGB, _, found := p.queryInstanceCache(instanceType)
	return price, vcpu, memGB, found
}

func (p *AWSPricer) queryInstanceCache(instanceType string) (price float64, vcpu int, memGB float64, fetchedAt time.Time, found bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	err := p.db.QueryRow(
		`SELECT price_usd, vcpu, mem_gb, fetched_at FROM finops_aws_pricing_cache WHERE instance_type = ? AND region = ?`,
		instanceType, p.region,
	).Scan(&price, &vcpu, &memGB, &fetchedAt)
	return price, vcpu, memGB, fetchedAt, err == nil
}

// refreshInstance consulta `aws pricing get-products` filtrado por instanceType (a API suporta
// filtro server-side por esse campo — confirmado empiricamente, diferente do filtro por região,
// que não é confiável via nome legível "location" e por isso é feito client-side abaixo via
// regionCode) e cacheia preço + specs.
func (p *AWSPricer) refreshInstance(ctx context.Context, instanceType string) error {
	_, err, _ := p.refreshSF.Do(instanceType, func() (interface{}, error) {
		return nil, p.doRefreshInstance(ctx, instanceType)
	})
	return err
}

func (p *AWSPricer) doRefreshInstance(ctx context.Context, instanceType string) error {
	args := []string{
		"pricing", "get-products",
		"--service-code", "AmazonEC2",
		"--region", awsPricingAPIRegion,
		"--format-version", "aws_v1",
		"--output", "json",
		"--filters",
		"Type=TERM_MATCH,Field=instanceType,Value=" + instanceType,
		"Type=TERM_MATCH,Field=operatingSystem,Value=Linux",
		"Type=TERM_MATCH,Field=tenancy,Value=Shared",
		"Type=TERM_MATCH,Field=preInstalledSw,Value=NA",
		"Type=TERM_MATCH,Field=capacitystatus,Value=Used",
	}
	if p.profile != "" {
		args = append(args, "--profile", p.profile)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, awsCLITimeout)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "aws", args...).Output()
	if err != nil {
		return fmt.Errorf("aws pricing get-products (EC2 %s): %w", instanceType, err)
	}

	var resp struct {
		PriceList []string `json:"PriceList"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("decodificar resposta AWS pricing (EC2): %w", err)
	}

	for _, raw := range resp.PriceList {
		var item map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
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
		regionCode, _ := attrs["regionCode"].(string)
		if regionCode != p.region {
			continue
		}
		price := extractAWSOnDemandPrice(item)
		if price <= 0 {
			continue
		}
		vcpu, _ := strconv.Atoi(fmt.Sprint(attrs["vcpu"]))
		memGB := parseAWSMemoryGB(fmt.Sprint(attrs["memory"]))

		p.mu.Lock()
		_, dbErr := p.db.Exec(
			`INSERT INTO finops_aws_pricing_cache (instance_type, region, price_usd, vcpu, mem_gb, fetched_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(instance_type, region) DO UPDATE SET price_usd = excluded.price_usd, vcpu = excluded.vcpu, mem_gb = excluded.mem_gb, fetched_at = excluded.fetched_at`,
			instanceType, p.region, price, vcpu, memGB, time.Now(),
		)
		p.mu.Unlock()
		if dbErr != nil {
			return fmt.Errorf("salvar preço EC2 no cache: %w", dbErr)
		}

		log.Info().Str("instance_type", instanceType).Str("region", p.region).Float64("price_usd_hour", price).
			Int("vcpu", vcpu).Float64("mem_gb", memGB).Msg("Preço obtido da AWS Price List API")
		return nil
	}

	return fmt.Errorf("nenhum preço encontrado pra %s na região %s", instanceType, p.region)
}

// GetDiskPricePerGBMonth retorna o preço USD/GB/mês do tipo de volume EBS informado (ex: "gp3").
// Mesmo modelo linear da GCP (sem "tier" de tamanho fixo como a Azure) — o chamador multiplica
// pelo tamanho real do disco em GB.
func (p *AWSPricer) GetDiskPricePerGBMonth(volumeType string) (price float64, source string, err error) {
	volumeType = strings.ToLower(strings.TrimSpace(volumeType))
	if !awsEBSVolumeTypes[volumeType] {
		return 0, "unknown", fmt.Errorf("tipo de volume EBS não reconhecido: %s", volumeType)
	}

	if price, ok := p.readDiskCache(volumeType); ok {
		return price, "api", nil
	}

	if refreshErr := p.refreshDisk(context.Background(), volumeType); refreshErr != nil {
		if price, ok := p.readDiskCacheIgnoringTTL(volumeType); ok {
			return price, "api", nil
		}
		log.Warn().Str("volume_type", volumeType).Str("region", p.region).Err(refreshErr).Msg("Falha ao obter preço EBS, usando fallback")
		if fb, ok := awsFallbackDiskPrices[volumeType]; ok {
			return fb, "fallback", nil
		}
		return 0, "unknown", refreshErr
	}

	if price, ok := p.readDiskCacheIgnoringTTL(volumeType); ok {
		return price, "api", nil
	}
	if fb, ok := awsFallbackDiskPrices[volumeType]; ok {
		return fb, "fallback", nil
	}
	return 0, "unknown", fmt.Errorf("preço de disco '%s' não encontrado na região %s", volumeType, p.region)
}

func (p *AWSPricer) readDiskCache(volumeType string) (price float64, ok bool) {
	price, fetchedAt, found := p.queryDiskCache(volumeType)
	if !found || time.Since(fetchedAt) > pricingCacheTTL {
		return 0, false
	}
	return price, true
}

func (p *AWSPricer) readDiskCacheIgnoringTTL(volumeType string) (price float64, ok bool) {
	price, _, found := p.queryDiskCache(volumeType)
	return price, found
}

func (p *AWSPricer) queryDiskCache(volumeType string) (price float64, fetchedAt time.Time, found bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	err := p.db.QueryRow(
		`SELECT usd_per_gb_mo, fetched_at FROM finops_aws_disk_pricing_cache WHERE volume_type = ? AND region = ?`,
		volumeType, p.region,
	).Scan(&price, &fetchedAt)
	return price, fetchedAt, err == nil
}

func (p *AWSPricer) refreshDisk(ctx context.Context, volumeType string) error {
	_, err, _ := p.diskRefreshSF.Do(volumeType, func() (interface{}, error) {
		return nil, p.doRefreshDisk(ctx, volumeType)
	})
	return err
}

func (p *AWSPricer) doRefreshDisk(ctx context.Context, volumeType string) error {
	args := []string{
		"pricing", "get-products",
		"--service-code", "AmazonEC2",
		"--region", awsPricingAPIRegion,
		"--format-version", "aws_v1",
		"--output", "json",
		"--filters",
		"Type=TERM_MATCH,Field=volumeApiName,Value=" + volumeType,
		"Type=TERM_MATCH,Field=productFamily,Value=Storage",
	}
	if p.profile != "" {
		args = append(args, "--profile", p.profile)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, awsCLITimeout)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "aws", args...).Output()
	if err != nil {
		return fmt.Errorf("aws pricing get-products (EBS %s): %w", volumeType, err)
	}

	var resp struct {
		PriceList []string `json:"PriceList"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("decodificar resposta AWS pricing (EBS): %w", err)
	}

	for _, raw := range resp.PriceList {
		var item map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
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
		regionCode, _ := attrs["regionCode"].(string)
		if regionCode != p.region {
			continue
		}
		price := extractAWSOnDemandPrice(item)
		if price <= 0 {
			continue
		}

		p.mu.Lock()
		_, dbErr := p.db.Exec(
			`INSERT INTO finops_aws_disk_pricing_cache (volume_type, region, usd_per_gb_mo, fetched_at)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(volume_type, region) DO UPDATE SET usd_per_gb_mo = excluded.usd_per_gb_mo, fetched_at = excluded.fetched_at`,
			volumeType, p.region, price, time.Now(),
		)
		p.mu.Unlock()
		if dbErr != nil {
			return fmt.Errorf("salvar preço EBS no cache: %w", dbErr)
		}

		log.Info().Str("volume_type", volumeType).Str("region", p.region).Float64("usd_per_gb_month", price).
			Msg("Preço de EBS obtido da AWS Price List API")
		return nil
	}

	return fmt.Errorf("nenhum preço encontrado pro volume %s na região %s", volumeType, p.region)
}

// extractAWSOnDemandPrice extrai o primeiro pricePerUnit.USD de terms.OnDemand — mesma navegação
// de extractAWSPrice (internal/web/handlers/nodepools_snat_costs.go), duplicada aqui porque
// internal/finops não pode importar internal/web/handlers (esse pacote já importa
// internal/finops — importar na direção contrária fecharia um import cycle).
func extractAWSOnDemandPrice(item map[string]interface{}) float64 {
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
			price, err := strconv.ParseFloat(usdStr, 64)
			if err != nil {
				continue
			}
			return price
		}
	}
	return 0
}

// parseAWSMemoryGB interpreta o atributo "memory" da AWS Price List API (ex: "8 GiB", "0.5 GiB")
// em GB. Retorna 0 se não conseguir interpretar.
func parseAWSMemoryGB(memStr string) float64 {
	memStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(memStr), "GiB"))
	val, err := strconv.ParseFloat(strings.TrimSpace(memStr), 64)
	if err != nil {
		return 0
	}
	return val
}
