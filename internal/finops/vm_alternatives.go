package finops

import "fmt"

// VMAlternative representa um SKU de VM alternativo sugerido como troca para o pool atual.
type VMAlternative struct {
	VMSize            string  `json:"vm_size"`
	CPUCores          int     `json:"cpu_cores"`
	MemoryGB          int     `json:"memory_gb"`
	MemPerCPUGB       float64 `json:"mem_per_cpu_gb"` // GB de RAM por vCPU
	PriceUSDHour      float64 `json:"price_usd_hour"`
	PriceSource       string  `json:"price_source"`
	CostDeltaPct      float64 `json:"cost_delta_pct"`      // negativo = mais barato
	MonthlySavingsBRL float64 `json:"monthly_savings_brl"` // positivo = economia na frota
	Reason            string  `json:"reason"`
	Verdict           string  `json:"verdict"` // "recommended" | "consider" | "cheaper"
}

// A lógica de sugestão em si (antes SuggestAlternatives, Azure-only) vive agora em
// vm_tiers.go::SuggestVMTier — generalizada pra Azure/GCP/AWS via a interface CloudPricer.

// buildAltReason gera um texto explicativo sobre por que a alternativa é sugerida.
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
