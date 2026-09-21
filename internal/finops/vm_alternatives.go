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
	// InsufficientForLargestWorkload — F1.2 do plano de melhorias (FINOPS-IMPROVEMENTS-PLAN.md):
	// true quando a capacidade desta SKU por node fica abaixo do maior request individual
	// (CPU/Mem) entre os workloads do pool + margem de segurança — sinal de que o maior pod ali
	// rodando pode não conseguir ser agendado nesta SKU menor. Preenchido pelo CHAMADOR via
	// MarkInsufficientForLargestWorkload (vm_tiers.go), nunca por SuggestVMTier em si (que não
	// conhece requests individuais, só o agregado do pool). Nunca remove a alternativa da
	// lista — só sinaliza, a decisão final continua humana.
	InsufficientForLargestWorkload bool `json:"insufficient_for_largest_workload,omitempty"`
	// Perf compara o desempenho de CPU por thread medido desta alternativa com o SKU atual do pool
	// (ver vm_perf.go). Preenchido só na LEITURA (handler de rightsizing), nunca persistido com a
	// análise — depende de medições que podem ser feitas depois do scan. Informativo: não altera o
	// Verdict nem o Reason.
	Perf *PerfComparison `json:"perf,omitempty"`
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
