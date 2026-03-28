package finops

import (
	"fmt"
	"math"
	"strings"
)

// VMAlternative representa um SKU de VM alternativo sugerido como troca para o pool atual
type VMAlternative struct {
	VMSize            string  `json:"vm_size"`
	CPUCores          int     `json:"cpu_cores"`
	MemoryGB          int     `json:"memory_gb"`
	MemPerCPUGB       float64 `json:"mem_per_cpu_gb"`    // GB de RAM por vCPU
	PriceUSDHour      float64 `json:"price_usd_hour"`
	PriceSource       string  `json:"price_source"`
	CostDeltaPct      float64 `json:"cost_delta_pct"`      // negativo = mais barato
	MonthlySavingsBRL float64 `json:"monthly_savings_brl"` // positivo = economia na frota
	Reason            string  `json:"reason"`
	Verdict           string  `json:"verdict"` // "recommended" | "consider" | "cheaper"
}

// SuggestAlternatives retorna até 3 SKUs alternativos para um node pool dado seu padrão
// de utilização de CPU e Memória. Retorna nil se o SKU é desconhecido ou não há alternativas.
//
// cpuUtilPct e memUtilPct são percentuais de uso real (0-100) referentes à capacidade
// do pool. Se não houver dados Prometheus, passar 0 — nenhuma sugestão de família será
// emitida mas sugestões de versão mais recente ainda podem aparecer.
//
// nodeCount é o número atual de nodes no pool — usado para calcular economia mensal total.
func SuggestAlternatives(
	currentSKU string,
	cpuUtilPct, memUtilPct int,
	pricer *AzurePricer,
	exchangeRate float64,
	nodeCount int,
) []VMAlternative {
	currentCPU, currentMem := GetVMSpecs(currentSKU)
	if currentCPU == 0 || currentMem == 0 {
		return nil
	}
	if pricer == nil {
		return nil
	}
	currentPrice, _, priceErr := pricer.GetPrice(currentSKU)
	if priceErr != nil || currentPrice <= 0 {
		return nil
	}

	currentMemPerCPU := currentMem / currentCPU // GB por vCPU
	family := extractVMFamily(currentSKU)       // "D", "E", "F", "B"

	// ── Determinar gargalo ────────────────────────────────────────────────────
	// Só considera gargalo se temos dados de uso (> 0)
	hasUsage := cpuUtilPct > 0 || memUtilPct > 0
	isMemBottleneck := hasUsage && memUtilPct > 65 && float64(memUtilPct) > float64(cpuUtilPct)*1.3
	isCPUHeavy := hasUsage && cpuUtilPct > 65 && float64(cpuUtilPct) > float64(memUtilPct)*1.3
	isOversized := hasUsage && cpuUtilPct < 30 && memUtilPct < 30

	type candid struct {
		sku     string
		verdict string
	}
	var candidates []candid

	switch {
	case isMemBottleneck && currentMemPerCPU <= 2:
		// F-series (2 GB/vCPU) → D-series (4 GB/vCPU) — mesmos vCPUs, 2× RAM
		for _, ver := range []string{"v5", "v4", "v3"} {
			candidates = append(candidates, candid{
				fmt.Sprintf("Standard_D%ds_%s", currentCPU, ver), "recommended",
			})
		}

	case isMemBottleneck && currentMemPerCPU <= 4:
		// D-series (4 GB/vCPU) → E-series (8 GB/vCPU) — mesmos vCPUs, 2× RAM
		for _, ver := range []string{"v5", "v4", "v3"} {
			candidates = append(candidates, candid{
				fmt.Sprintf("Standard_E%ds_%s", currentCPU, ver), "recommended",
			})
		}

	case isCPUHeavy && currentMemPerCPU >= 8:
		// E-series com alto CPU → D-series (menos RAM, mais barato)
		for _, ver := range []string{"v5", "v4"} {
			candidates = append(candidates, candid{
				fmt.Sprintf("Standard_D%ds_%s", currentCPU, ver), "consider",
			})
		}
		// Ou F-series (ainda menos RAM, mais barato ainda)
		candidates = append(candidates, candid{
			fmt.Sprintf("Standard_F%ds_v2", currentCPU), "consider",
		})

	case isCPUHeavy && currentMemPerCPU >= 4:
		// D-series com alto CPU → F-series (compute-optimized, mais barato)
		candidates = append(candidates, candid{
			fmt.Sprintf("Standard_F%ds_v2", currentCPU), "consider",
		})

	case isOversized && currentCPU >= 4:
		// Cluster sub-utilizado → sugerir metade dos vCPUs (mesma família ou mais recente)
		halfCPU := currentCPU / 2
		switch family {
		case "D":
			for _, ver := range []string{"v5", "v4"} {
				candidates = append(candidates, candid{
					fmt.Sprintf("Standard_D%ds_%s", halfCPU, ver), "cheaper",
				})
			}
		case "E":
			for _, ver := range []string{"v5", "v4"} {
				candidates = append(candidates, candid{
					fmt.Sprintf("Standard_E%ds_%s", halfCPU, ver), "cheaper",
				})
			}
		case "F":
			candidates = append(candidates, candid{
				fmt.Sprintf("Standard_F%ds_v2", halfCPU), "cheaper",
			})
			for _, ver := range []string{"v5"} {
				candidates = append(candidates, candid{
					fmt.Sprintf("Standard_D%ds_%s", halfCPU, ver), "cheaper",
				})
			}
		default:
			for _, ver := range []string{"v5", "v4"} {
				candidates = append(candidates, candid{
					fmt.Sprintf("Standard_D%ds_%s", halfCPU, ver), "cheaper",
				})
			}
		}
	}

	// ── Processar candidatos ──────────────────────────────────────────────────
	seen := map[string]bool{strings.ToLower(currentSKU): true}
	var result []VMAlternative

	for _, c := range candidates {
		if len(result) >= 3 {
			break
		}
		key := strings.ToLower(c.sku)
		if seen[key] {
			continue
		}
		seen[key] = true

		cpu, mem := GetVMSpecs(c.sku)
		if cpu == 0 {
			continue
		}

		price, src, err := pricer.GetPrice(c.sku)
		if err != nil || price <= 0 {
			continue
		}

		costDeltaPct := (price - currentPrice) / currentPrice * 100
		// Economia mensal para toda a frota do pool (positivo = mais barato)
		monthlySavings := (currentPrice - price) * HoursPerMonth * float64(nodeCount) * exchangeRate

		result = append(result, VMAlternative{
			VMSize:            c.sku,
			CPUCores:          cpu,
			MemoryGB:          mem,
			MemPerCPUGB:       math.Round(float64(mem)/float64(cpu)*10) / 10,
			PriceUSDHour:      price,
			PriceSource:       src,
			CostDeltaPct:      math.Round(costDeltaPct*10) / 10,
			MonthlySavingsBRL: math.Round(monthlySavings*100) / 100,
			Reason:            buildAltReason(currentCPU, currentMem, cpu, mem, cpuUtilPct, memUtilPct),
			Verdict:           c.verdict,
		})
	}

	return result
}

// extractVMFamily retorna a letra da família de VM (D, E, F, B, …)
func extractVMFamily(vmSize string) string {
	name := strings.TrimPrefix(vmSize, "Standard_")
	if len(name) == 0 {
		return ""
	}
	return strings.ToUpper(string(name[0]))
}

// buildAltReason gera um texto explicativo sobre por que a alternativa é sugerida
func buildAltReason(curCPU, curMem, altCPU, altMem, cpuPct, memPct int) string {
	curRatio := curMem / curCPU
	altRatio := altMem / altCPU

	if altRatio > curRatio && altCPU == curCPU {
		factor := altMem / curMem
		return fmt.Sprintf("Mesmos %d vCPUs — %d GB vs %d GB RAM (%d× mais memória por core)",
			altCPU, altMem, curMem, factor)
	}
	if altRatio < curRatio && altCPU == curCPU {
		return fmt.Sprintf("Mesmos %d vCPUs — %d GB RAM (memória reduzida conforme gargalo é CPU)",
			altCPU, altMem)
	}
	if altCPU < curCPU {
		return fmt.Sprintf("Reduzir para %d vCPUs / %d GB RAM (CPU: %d%%, Mem: %d%% — cluster sub-utilizado)",
			altCPU, altMem, cpuPct, memPct)
	}
	return fmt.Sprintf("%d vCPUs / %d GB RAM (%d GB/vCPU)", altCPU, altMem, altRatio)
}
