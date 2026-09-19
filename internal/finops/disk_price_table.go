package finops

import (
	"math"
	"strings"

	"k8s-hpa-manager/internal/models"
)

// Tabela de preços de referência (USD) pros tipos de disco que a estimativa por API/tier não cobre
// — mesma ideia das listas diskFallbackPrices/gcpDiskFallbackPrices/awsFallbackDiskPrices: valores
// de tabela on-demand, sem desconto, numa região de referência. Diferente daquelas, aqui o preço
// NÃO é só capacidade: Ultra Disk, Premium SSD v2 e Hyperdisk cobram também por IOPS e throughput
// PROVISIONADOS (que é onde está a maior parte do custo), então cada linha tem 3 componentes.
//
// Capturados ao vivo em 2026-09-19 nas APIs públicas de preço de cada cloud:
//   - Azure: Retail Prices API, brazilsouth, LRS (preço/hora × 730 h/mês).
//   - GCP:   Cloud Billing Catalog, São Paulo (southamerica-east1), USD/mês.
//   - AWS:   Price List API, us-east-1.
//
// Regiões diferentes da de referência custam diferente — a UI avisa quando há disco precificado
// por esta tabela (price_source == "table").

// (hoursPerMonth — 730 h/mês — já é declarada em data_resources_pricing.go)

// diskPerfPrice descreve o preço de um tipo de disco: por GiB/mês + por IOPS/mês + por MBps/mês,
// com uma cota gratuita de IOPS/throughput (baseline incluído no preço da capacidade).
type diskPerfPrice struct {
	PerGBMonth   float64
	PerIOPSMonth float64
	PerMBpsMonth float64
	FreeIOPS     float64 // IOPS incluídos sem custo (só o excedente é cobrado)
	FreeMBps     float64 // throughput incluído sem custo
}

// Azure — chave: SKU do disco em minúsculas (sku.name da ARM API). Ultra e Premium SSD v2 não têm
// tier fixo de tamanho como o Premium SSD "P10": capacidade, IOPS e throughput são provisionados
// separadamente. Para disco desatachado NÃO há a cobrança "vCPU reservation" do Ultra (só existe
// enquanto atachado a uma VM).
var azureTableDiskPrices = map[string]diskPerfPrice{
	// Ultra Disk: capacidade $0.000328/GiB-h · IOPS $0.000136/h · throughput $0.000913/MBps-h.
	"ultrassd_lrs": {
		PerGBMonth:   0.000328 * hoursPerMonth,
		PerIOPSMonth: 0.000136 * hoursPerMonth,
		PerMBpsMonth: 0.000913 * hoursPerMonth,
	},
	// Premium SSD v2: capacidade $0.000208/GiB-h; os 3.000 primeiros IOPS e 125 MBps são gratuitos,
	// o excedente custa $0.000013/IOPS-h e $0.000104/MBps-h.
	"premiumv2_lrs": {
		PerGBMonth:   0.000208 * hoursPerMonth,
		PerIOPSMonth: 0.000013 * hoursPerMonth,
		PerMBpsMonth: 0.000104 * hoursPerMonth,
		FreeIOPS:     3000,
		FreeMBps:     125,
	},
}

// GCP — chave: tipo do disco (diskTypes/<tipo>). Preços USD/mês em São Paulo. O baseline de
// 3.000 IOPS e 140 MiB/s do Hyperdisk Balanced é o documentado pelo Google (só o excedente é
// cobrado); os demais Hyperdisk cobram a performance provisionada inteira.
var gcpTableDiskPrices = map[string]diskPerfPrice{
	"hyperdisk-balanced":                   {PerGBMonth: 0.127, PerIOPSMonth: 0.008, PerMBpsMonth: 0.064, FreeIOPS: 3000, FreeMBps: 140},
	"hyperdisk-balanced-high-availability": {PerGBMonth: 0.2544, PerIOPSMonth: 0.0159, PerMBpsMonth: 0.1272, FreeIOPS: 3000, FreeMBps: 140},
	"hyperdisk-extreme":                    {PerGBMonth: 0.199, PerIOPSMonth: 0.051},
	"hyperdisk-ml":                         {PerGBMonth: 0.127, PerMBpsMonth: 0.191},
	"hyperdisk-throughput":                 {PerGBMonth: 0.008, PerMBpsMonth: 0.398},
}

// AWS — chave: volumeApiName. Só o EBS magnético (standard, geração anterior): $0.05/GB-mês. A
// cobrança por milhão de requisições de I/O do magnético não se aplica a volume sem atividade.
var awsTableDiskPrices = map[string]diskPerfPrice{
	"standard": {PerGBMonth: 0.05},
}

// tablePriceFor devolve a linha da tabela pro disco (por cloud + tipo), se existir.
func tablePriceFor(d models.UnattachedDisk) (diskPerfPrice, bool) {
	key := strings.ToLower(strings.TrimSpace(d.DiskType))
	var row diskPerfPrice
	var ok bool
	switch d.Provider {
	case "azure":
		row, ok = azureTableDiskPrices[key]
	case "gcp":
		row, ok = gcpTableDiskPrices[key]
	case "aws":
		row, ok = awsTableDiskPrices[key]
	}
	return row, ok
}

// tableMonthlyCostUSD calcula capacidade + IOPS/throughput provisionados acima da cota gratuita.
func tableMonthlyCostUSD(d models.UnattachedDisk, p diskPerfPrice) float64 {
	cost := d.SizeGB * p.PerGBMonth
	cost += math.Max(0, d.ProvisionedIOPS-p.FreeIOPS) * p.PerIOPSMonth
	cost += math.Max(0, d.ProvisionedMBps-p.FreeMBps) * p.PerMBpsMonth
	return round2(cost)
}
