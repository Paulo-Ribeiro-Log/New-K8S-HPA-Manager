package nodepoolpredictions

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/monitoring/predictions"

	"github.com/rs/zerolog/log"
)

// Preços Azure pay-as-you-go (referência Brasil Sul)
const (
	npPriceCPUCoreHour = 0.05  // USD por vCPU/hora
	npPriceMemGBHour   = 0.005 // USD por GB/hora
	npHoursPerMonth    = 730   // 365 dias × 24h / 12 meses

	// Thresholds de idle: nodes com CPU < 30% E mem < 30% são considerados ociosos
	npIdleCPUThreshold = 30.0 // %
	npIdleMemThreshold = 30.0 // %

	// Margem de segurança para right-sizing (P95 + 20%)
	npRightSizingMargin = 1.20

	// Cotação fallback quando API falha
	npDefaultExchangeRate = 5.50

	// Cache da cotação
	npExchangeRateCacheTTL = 1 * time.Hour

	// Mínimo de economia para gerar recomendação ($10/mês)
	npMinSavingsToRecommend = 10.0
)

// NodePoolCostAnalyzer calcula custos reais de um node pool baseado no VM SKU
type NodePoolCostAnalyzer struct {
	mu                sync.RWMutex
	cachedRate        float64
	cachedRateDate    string
	cachedRateExpires time.Time
}

// NewNodePoolCostAnalyzer cria um novo analisador de custos para node pools
func NewNodePoolCostAnalyzer() *NodePoolCostAnalyzer {
	return &NodePoolCostAnalyzer{}
}

// Calculate retorna análise de custo completa para o node pool.
// Usa o VM SKU real (via azure_vm_specs.go) para custo mais preciso que a análise por deployment.
func (ca *NodePoolCostAnalyzer) Calculate(metrics *NodePoolMetrics) *NodePoolCostAnalysis {
	// 1. Cotação USD→BRL (cache 1h, fallback R$5,50)
	rate, rateDate := ca.getExchangeRate()

	// 2. Specs do VM SKU via mapeamento azure_vm_specs
	vmSpec := predictions.GetVMSpecs(metrics.VMSize)
	vmCPUs := 4   // fallback quando SKU não está no mapeamento
	vmMemGB := 16 // fallback
	vmFamily := "Unknown"
	if vmSpec != nil {
		vmCPUs = vmSpec.VCPUs
		vmMemGB = vmSpec.MemoryGiB
		vmFamily = vmSpec.Family
	} else {
		log.Warn().Str("vm_size", metrics.VMSize).Msg("VM SKU nao encontrado no mapeamento — usando fallback 4 vCPUs / 16 GB")
	}

	// 3. Custo por node: (vCPUs × $0.05 + memGB × $0.005) × 730h/mês
	costPerNodePerHour := float64(vmCPUs)*npPriceCPUCoreHour + float64(vmMemGB)*npPriceMemGBHour
	costPerNodeMonthly := costPerNodePerHour * npHoursPerMonth

	// 4. Custo atual: currentNodes × custo/node
	currentNodes := metrics.CurrentNodes
	if currentNodes == 0 {
		currentNodes = len(metrics.NodesSnapshot)
	}
	currentMonthlyCostUSD := float64(currentNodes) * costPerNodeMonthly

	// Breakdown proporcional (estimativa — VMs são cobradas como unidade)
	cpuFraction := (float64(vmCPUs) * npPriceCPUCoreHour) / costPerNodePerHour
	memFraction := (float64(vmMemGB) * npPriceMemGBHour) / costPerNodePerHour
	breakdown := NodePoolCostBreakdown{
		ComputeCostUSD: round2(currentMonthlyCostUSD * cpuFraction),
		MemoryCostUSD:  round2(currentMonthlyCostUSD * memFraction),
	}

	// 5. Custo máximo possível: maxNodes × custo/node
	maxMonthlyCostUSD := float64(metrics.MaxNodes) * costPerNodeMonthly

	// 6. Idle waste: nodes com CPU < 30% E mem < 30%
	idleNodes := 0
	for _, snap := range metrics.NodesSnapshot {
		if snap.CPUUsagePercent < npIdleCPUThreshold && snap.MemUsagePercent < npIdleMemThreshold {
			idleNodes++
		}
	}
	idleWastePercent := 0.0
	wasteMonthlyCostUSD := 0.0
	if currentNodes > 0 {
		idleWastePercent = float64(idleNodes) / float64(currentNodes) * 100.0
		wasteMonthlyCostUSD = float64(idleNodes) * costPerNodeMonthly
	}

	// 7. Recommended nodes: baseado em P95 de uso + 20% margem de segurança
	recommendedNodes := ca.calculateRecommendedNodes(metrics)
	// Nunca recomendar abaixo do minNodes configurado
	if recommendedNodes < metrics.MinNodes {
		recommendedNodes = metrics.MinNodes
	}

	optimizedMonthlyCostUSD := float64(recommendedNodes) * costPerNodeMonthly
	monthlySavingsUSD := currentMonthlyCostUSD - optimizedMonthlyCostUSD
	if monthlySavingsUSD < 0 {
		monthlySavingsUSD = 0
		optimizedMonthlyCostUSD = currentMonthlyCostUSD
	}

	savingsPercent := 0.0
	if currentMonthlyCostUSD > 0 {
		savingsPercent = monthlySavingsUSD / currentMonthlyCostUSD * 100.0
	}

	// 8. Recomendações de economia
	recs := ca.generateRecommendations(metrics, rate, idleNodes, recommendedNodes, costPerNodeMonthly)

	// 9. SKUs alternativos (Fase 15)
	skuAlts := ca.suggestAlternativeSKUs(metrics, vmCPUs, vmMemGB, costPerNodePerHour, rate, currentNodes)

	analysis := &NodePoolCostAnalysis{
		ExchangeRate:     rate,
		ExchangeRateDate: rateDate,

		VMSize:     metrics.VMSize,
		VMFamily:   vmFamily,
		VMCPUCores: vmCPUs,
		VMMemoryGB: vmMemGB,

		CostPerNodePerHour: round2(costPerNodePerHour),
		CostPerNodeMonthly: round2(costPerNodeMonthly),

		CurrentMonthlyCostUSD: round2(currentMonthlyCostUSD),
		CurrentMonthlyCostBRL: round2(currentMonthlyCostUSD * rate),
		CostBreakdown:         breakdown,

		MaxMonthlyCostUSD: round2(maxMonthlyCostUSD),
		MaxMonthlyCostBRL: round2(maxMonthlyCostUSD * rate),

		IdleWastePercent:    math.Round(idleWastePercent*10) / 10,
		WasteMonthlyCostUSD: round2(wasteMonthlyCostUSD),
		WasteMonthlyCostBRL: round2(wasteMonthlyCostUSD * rate),

		RecommendedNodes:        recommendedNodes,
		OptimizedMonthlyCostUSD: round2(optimizedMonthlyCostUSD),
		OptimizedMonthlyCostBRL: round2(optimizedMonthlyCostUSD * rate),
		MonthlySavingsUSD:       round2(monthlySavingsUSD),
		MonthlySavingsBRL:       round2(monthlySavingsUSD * rate),
		AnnualSavingsUSD:        round2(monthlySavingsUSD * 12),
		AnnualSavingsBRL:        round2(monthlySavingsUSD * 12 * rate),
		SavingsPercent:          math.Round(savingsPercent*10) / 10,

		Recommendations: recs,
		SKUAlternatives: skuAlts,
	}

	log.Info().
		Str("nodepool", metrics.NodePoolName).
		Str("vm_size", metrics.VMSize).
		Int("current_nodes", currentNodes).
		Int("recommended_nodes", recommendedNodes).
		Int("idle_nodes", idleNodes).
		Float64("cost_usd", analysis.CurrentMonthlyCostUSD).
		Float64("cost_brl", analysis.CurrentMonthlyCostBRL).
		Float64("savings_usd", analysis.MonthlySavingsUSD).
		Float64("savings_pct", analysis.SavingsPercent).
		Float64("exchange_rate", rate).
		Msg("Node pool cost analysis concluida")

	return analysis
}

// calculateRecommendedNodes estima o número ideal de nodes com base em P95 de uso real.
// Garante headroom de 20% acima do P95 e cap de 80% de utilização alvo por node.
func (ca *NodePoolCostAnalyzer) calculateRecommendedNodes(metrics *NodePoolMetrics) int {
	snapshots := metrics.NodesSnapshot
	if len(snapshots) == 0 {
		return metrics.CurrentNodes
	}

	// Coletar percentuais de utilização de cada node
	var cpuPcts, memPcts []float64
	for _, snap := range snapshots {
		cpuPcts = append(cpuPcts, snap.CPUUsagePercent)
		memPcts = append(memPcts, snap.MemUsagePercent)
	}

	// P95 de uso por node
	cpuP95 := percentile95(cpuPcts)
	memP95 := percentile95(memPcts)

	// Target de utilização: P95 + 20% margem, com cap em 80%
	cpuTarget := math.Min(cpuP95*npRightSizingMargin, 80.0)
	memTarget := math.Min(memP95*npRightSizingMargin, 80.0)

	// Carga total atual (soma de todos os nodes)
	var totalCPU, totalMem float64
	for _, snap := range snapshots {
		totalCPU += snap.CPUUsagePercent
		totalMem += snap.MemUsagePercent
	}

	// nodes necessários = carga total / target por node
	nodesForCPU := 1.0
	nodesForMem := 1.0
	if cpuTarget > 0 {
		nodesForCPU = math.Ceil(totalCPU / cpuTarget)
	}
	if memTarget > 0 {
		nodesForMem = math.Ceil(totalMem / memTarget)
	}

	// Bottleneck: usa o maior dos dois recursos
	recommended := math.Max(nodesForCPU, nodesForMem)

	// Nunca recomendar mais que os nodes atuais (não é para escalar)
	if recommended > float64(len(snapshots)) {
		recommended = float64(len(snapshots))
	}
	if recommended < 1 {
		recommended = 1
	}

	log.Debug().
		Float64("cpu_p95", cpuP95).
		Float64("mem_p95", memP95).
		Float64("cpu_target", cpuTarget).
		Float64("mem_target", memTarget).
		Float64("nodes_for_cpu", nodesForCPU).
		Float64("nodes_for_mem", nodesForMem).
		Int("recommended", int(recommended)).
		Msg("calculateRecommendedNodes")

	return int(recommended)
}

// generateRecommendations gera recomendações de economia específicas para node pools.
// Analisa: max nodes superdimensionado, idle nodes, right-sizing e candidatos a Spot.
func (ca *NodePoolCostAnalyzer) generateRecommendations(
	metrics *NodePoolMetrics,
	rate float64,
	idleNodes, recommendedNodes int,
	costPerNodeMonthly float64,
) []NodePoolCostRecommendation {
	var recs []NodePoolCostRecommendation
	currentNodes := metrics.CurrentNodes

	// Rec 1: Max nodes superdimensionado
	// Se maxNodes > currentNodes + 2 e autoscaler habilitado, pode reduzir teto
	if metrics.AutoscalerEnabled && metrics.MaxNodes > currentNodes+2 {
		suggestedMax := currentNodes + 2
		savings := float64(metrics.MaxNodes-suggestedMax) * costPerNodeMonthly
		if savings > npMinSavingsToRecommend {
			recs = append(recs, NodePoolCostRecommendation{
				Title: "Reduzir max nodes do autoscaler",
				Description: fmt.Sprintf(
					"Max nodes configurado em %d, mas pool nunca ultrapassou %d nodes. "+
						"Reduzir max para %d evita gastos inesperados em picos pontuais.",
					metrics.MaxNodes, currentNodes, suggestedMax,
				),
				Action:        "reduce_max_nodes",
				CostBeforeUSD: round2(float64(metrics.MaxNodes) * costPerNodeMonthly),
				CostAfterUSD:  round2(float64(suggestedMax) * costPerNodeMonthly),
				SavingsUSD:    round2(savings),
				SavingsBRL:    round2(savings * rate),
				Impact:        "low",
			})
		}
	}

	// Rec 2: Nodes ociosos — consolidar carga
	if idleNodes > 0 && idleNodes < currentNodes {
		savings := float64(idleNodes) * costPerNodeMonthly
		if savings > npMinSavingsToRecommend {
			recs = append(recs, NodePoolCostRecommendation{
				Title: fmt.Sprintf("Consolidar %d node(s) ocioso(s)", idleNodes),
				Description: fmt.Sprintf(
					"%d node(s) com CPU e memoria abaixo de %.0f%%. "+
						"Considerar reduzir minNodes ou rebalancear workloads para melhor densidade.",
					idleNodes, npIdleCPUThreshold,
				),
				Action:        "consolidate_idle_nodes",
				CostBeforeUSD: round2(float64(currentNodes) * costPerNodeMonthly),
				CostAfterUSD:  round2(float64(currentNodes-idleNodes) * costPerNodeMonthly),
				SavingsUSD:    round2(savings),
				SavingsBRL:    round2(savings * rate),
				Impact:        "medium",
			})
		}
	}

	// Rec 3: Right-sizing — pool atual tem mais nodes que o necessário (P95 + 20%)
	if recommendedNodes < currentNodes && recommendedNodes >= metrics.MinNodes {
		savings := float64(currentNodes-recommendedNodes) * costPerNodeMonthly
		if savings > npMinSavingsToRecommend {
			recs = append(recs, NodePoolCostRecommendation{
				Title: fmt.Sprintf("Right-sizing: reduzir para %d nodes", recommendedNodes),
				Description: fmt.Sprintf(
					"Analise de P95 indica que %d nodes sao suficientes para a carga atual (P95 + 20%% de margem). "+
						"Reduzir de %d para %d nodes libera %.2f USD/mes.",
					recommendedNodes, currentNodes, recommendedNodes, savings,
				),
				Action:        "right_size_nodepool",
				CostBeforeUSD: round2(float64(currentNodes) * costPerNodeMonthly),
				CostAfterUSD:  round2(float64(recommendedNodes) * costPerNodeMonthly),
				SavingsUSD:    round2(savings),
				SavingsBRL:    round2(savings * rate),
				Impact:        "high",
			})
		}
	}

	// Rec 4: Spot nodes — se uso do pool é consistentemente baixo (< 70% CPU e mem)
	allLowUsage := len(metrics.NodesSnapshot) > 0
	for _, snap := range metrics.NodesSnapshot {
		if snap.CPUUsagePercent > 70 || snap.MemUsagePercent > 70 {
			allLowUsage = false
			break
		}
	}
	if allLowUsage {
		// Spot nodes costam ~60% menos que on-demand
		spotSavings := float64(currentNodes) * costPerNodeMonthly * 0.60
		if spotSavings > npMinSavingsToRecommend {
			recs = append(recs, NodePoolCostRecommendation{
				Title: "Considerar Spot nodes para workloads tolerantes a interrupcao",
				Description: fmt.Sprintf(
					"Pool com utilizacao consistentemente abaixo de 70%%. "+
						"Spot nodes Azure podem reduzir custo em ~60%% para workloads que tolerem interrupcoes pontuais.",
				),
				Action:        "enable_spot_nodes",
				CostBeforeUSD: round2(float64(currentNodes) * costPerNodeMonthly),
				CostAfterUSD:  round2(float64(currentNodes) * costPerNodeMonthly * 0.40),
				SavingsUSD:    round2(spotSavings),
				SavingsBRL:    round2(spotSavings * rate),
				Impact:        "medium",
			})
		}
	}

	return recs
}

// ---------------------------------------------------------------------------
// Fase 15 — Sugestão de VM SKU alternativo baseada em consumo histórico
// ---------------------------------------------------------------------------

// historicalP95 calcula P95 de CPU% e Mem% combinando snapshot atual (D-0) com
// os dados históricos de tendência (D-3, D-7, D-14).
// Usar histórico evita sugerir alternativas baseadas em um momento atípico.
func (ca *NodePoolCostAnalyzer) historicalP95(metrics *NodePoolMetrics) (cpuP95, memP95 float64) {
	var cpuValues, memValues []float64

	// D-0: snapshot atual de cada node
	for _, snap := range metrics.NodesSnapshot {
		cpuValues = append(cpuValues, snap.CPUUsagePercent)
		memValues = append(memValues, snap.MemUsagePercent)
	}

	// D-3, D-7, D-14: ValuePerNode é a média por node naquele ponto histórico
	for _, trend := range metrics.CPUTrendPerNode {
		if trend.ValuePerNode > 0 {
			cpuValues = append(cpuValues, trend.ValuePerNode)
		}
	}
	for _, trend := range metrics.MemTrendPerNode {
		if trend.ValuePerNode > 0 {
			memValues = append(memValues, trend.ValuePerNode)
		}
	}

	return percentile95(cpuValues), percentile95(memValues)
}

// identifyBottleneckFromP95 determina o bottleneck a partir dos P95 históricos.
// cpu → CPU domina (P95 ≥ 60% e 40% maior que mem) | memory → Mem domina | balanced → nenhum domina
func identifyBottleneckFromP95(cpuP95, memP95 float64) string {
	const pressureThreshold = 60.0
	const dominanceRatio = 1.4

	cpuHigh := cpuP95 >= pressureThreshold
	memHigh := memP95 >= pressureThreshold

	if cpuHigh && memP95 > 0 && cpuP95 > memP95*dominanceRatio {
		return "cpu"
	}
	if memHigh && cpuP95 > 0 && memP95 > cpuP95*dominanceRatio {
		return "memory"
	}
	return "balanced"
}

// suggestAlternativeSKUs sugere até 3 VMs alternativas baseado no consumo histórico real.
//
// Algoritmo:
//  1. Calcular P95 histórico de CPU% e Mem% (D-0 + D-3/D-7/D-14)
//  2. Derivar uso real: cpuUsed = currentVCPUs × cpuP95/100; memUsed = currentMemGB × memP95/100
//  3. FILTRO PRIMÁRIO: candidato deve acomodar cpuUsed e memUsed com 20% de headroom
//     (vCPUs ≥ ceil(cpuUsed/0.80) e memGB ≥ ceil(memUsed/0.80))
//  4. SCORE: bottleneck relief + cost savings + generation bonus
func (ca *NodePoolCostAnalyzer) suggestAlternativeSKUs(
	metrics *NodePoolMetrics,
	currentVCPUs, currentMemGB int,
	currentCostPerHour, rate float64,
	currentNodes int,
) []NodePoolSKUAlternative {
	if currentVCPUs == 0 || currentMemGB == 0 || currentCostPerHour == 0 {
		return nil
	}

	// 1. P95 histórico (snapshot atual + D-3/D-7/D-14)
	cpuP95, memP95 := ca.historicalP95(metrics)
	if cpuP95 == 0 && memP95 == 0 {
		// Sem dados históricos → evitar recomendação sem base
		return nil
	}

	// 2. Consumo real a P95
	cpuUsedAtP95 := float64(currentVCPUs) * cpuP95 / 100.0 // vCPUs efetivamente usados
	memUsedAtP95 := float64(currentMemGB) * memP95 / 100.0 // GB efetivamente usados

	// 3. Requisito mínimo: SKU deve acomodar P95 com 20% de headroom (max 80% util)
	const headroom = 0.80
	minVCPUs := int(math.Ceil(cpuUsedAtP95 / headroom))
	minMemGB := int(math.Ceil(memUsedAtP95 / headroom))
	if minVCPUs < 1 {
		minVCPUs = 1
	}
	if minMemGB < 1 {
		minMemGB = 1
	}

	// 4. Bottleneck e pool cost
	bottleneck := identifyBottleneckFromP95(cpuP95, memP95)
	currentPoolMonthly := float64(currentNodes) * currentCostPerHour * npHoursPerMonth

	isExoticFamily := func(family string) bool {
		return strings.HasPrefix(family, "NC") ||
			strings.HasPrefix(family, "NV") ||
			family == "M-Series"
	}

	type candidate struct {
		spec  predictions.VMSpec
		score float64
	}
	var candidates []candidate

	for _, spec := range predictions.GetAllVMSpecs() {
		if spec.Size == metrics.VMSize {
			continue
		}
		if isExoticFamily(spec.Family) {
			continue
		}

		// FILTRO PRIMÁRIO: capacidade mínima para lidar com carga histórica P95
		if spec.VCPUs < minVCPUs || spec.MemoryGiB < minMemGB {
			continue
		}

		altCostPerHour := float64(spec.VCPUs)*npPriceCPUCoreHour + float64(spec.MemoryGiB)*npPriceMemGBHour

		// Rejeitar SKUs muito mais caros sem ganho justificável
		if altCostPerHour > currentCostPerHour*1.50 {
			continue
		}

		var score float64

		// Custo: cada 10% mais barato vale 5 pts
		costFactor := (currentCostPerHour - altCostPerHour) / currentCostPerHour
		score += costFactor * 5.0

		// Utilização esperada após migração (baseia o score no alívio real do bottleneck)
		cpuUtilAfter := cpuUsedAtP95 / float64(spec.VCPUs) * 100.0
		memUtilAfter := memUsedAtP95 / float64(spec.MemoryGiB) * 100.0

		switch bottleneck {
		case "cpu":
			// Priorizar SKUs que reduzem pressão de CPU historicamente alta
			cpuRelief := (cpuP95 - cpuUtilAfter) / cpuP95
			score += cpuRelief * 8.0
		case "memory":
			// Priorizar SKUs que reduzem pressão de memória historicamente alta
			memRelief := (memP95 - memUtilAfter) / memP95
			score += memRelief * 8.0
		case "balanced":
			// Sem bottleneck definido: priorizar economia, penalizar over-provision
			if spec.VCPUs > currentVCPUs {
				overCPU := float64(spec.VCPUs-currentVCPUs) / float64(currentVCPUs)
				score -= overCPU * 2.0
			}
		}

		// Bônus de geração: VMs mais novas tendem a ter melhor perf/custo
		if strings.Contains(spec.Size, "_v5") {
			score += 0.3
		} else if strings.Contains(spec.Size, "_v4") {
			score += 0.2
		} else if strings.Contains(spec.Size, "_v3") {
			score += 0.1
		}

		candidates = append(candidates, candidate{spec: spec, score: score})
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	maxResults := 3
	if len(candidates) < maxResults {
		maxResults = len(candidates)
	}

	result := make([]NodePoolSKUAlternative, 0, maxResults)
	for i := 0; i < maxResults; i++ {
		spec := candidates[i].spec
		altCostPerHour := float64(spec.VCPUs)*npPriceCPUCoreHour + float64(spec.MemoryGiB)*npPriceMemGBHour
		altPoolMonthly := float64(currentNodes) * altCostPerHour * npHoursPerMonth
		savingsUSD := currentPoolMonthly - altPoolMonthly

		savingsPct := 0.0
		if currentPoolMonthly > 0 {
			savingsPct = savingsUSD / currentPoolMonthly * 100.0
		}

		cpuDelta := float64(spec.VCPUs-currentVCPUs) / float64(currentVCPUs) * 100.0
		memDelta := float64(spec.MemoryGiB-currentMemGB) / float64(currentMemGB) * 100.0

		// Utilização esperada após migração (para mostrar no rationale)
		cpuUtilAfter := cpuUsedAtP95 / float64(spec.VCPUs) * 100.0
		memUtilAfter := memUsedAtP95 / float64(spec.MemoryGiB) * 100.0

		result = append(result, NodePoolSKUAlternative{
			VMSize:             spec.Size,
			VMFamily:           spec.Family,
			VMCPUCores:         spec.VCPUs,
			VMMemoryGB:         spec.MemoryGiB,
			CostPerNodeHourUSD: round2(altCostPerHour),
			CostPerNodeMonthly: round2(altCostPerHour * npHoursPerMonth),
			PoolMonthlyCostUSD: round2(altPoolMonthly),
			PoolMonthlyCostBRL: round2(altPoolMonthly * rate),
			SavingsUSD:         round2(savingsUSD),
			SavingsBRL:         round2(savingsUSD * rate),
			SavingsPercent:     math.Round(savingsPct*10) / 10,
			Bottleneck:         bottleneck,
			Rationale:          buildSKURationale(bottleneck, spec, cpuP95, memP95, cpuUtilAfter, memUtilAfter, savingsPct),
			CPUDeltaPercent:    math.Round(cpuDelta*10) / 10,
			MemDeltaPercent:    math.Round(memDelta*10) / 10,
		})
	}

	log.Info().
		Str("nodepool", metrics.NodePoolName).
		Str("bottleneck", bottleneck).
		Float64("cpu_p95_hist", math.Round(cpuP95*10)/10).
		Float64("mem_p95_hist", math.Round(memP95*10)/10).
		Int("min_vcpus_needed", minVCPUs).
		Int("min_mem_gb_needed", minMemGB).
		Int("alternatives_found", len(result)).
		Msg("SKU alternatives gerados baseado em consumo historico P95")

	return result
}

// buildSKURationale gera explicação legível mencionando os P95 históricos que motivaram a sugestão.
func buildSKURationale(
	bottleneck string,
	spec predictions.VMSpec,
	cpuP95, memP95 float64,
	cpuUtilAfter, memUtilAfter float64,
	savingsPct float64,
) string {
	switch bottleneck {
	case "cpu":
		return fmt.Sprintf(
			"CPU P95 historico %.0f%% — %s (%d vCPUs) reduziria para ~%.0f%% de utilizacao com headroom adequado.",
			cpuP95, spec.Size, spec.VCPUs, cpuUtilAfter,
		)
	case "memory":
		return fmt.Sprintf(
			"Mem P95 historico %.0f%% — %s (%dGB) reduziria para ~%.0f%% de utilizacao com headroom adequado.",
			memP95, spec.Size, spec.MemoryGiB, memUtilAfter,
		)
	case "balanced":
		if savingsPct > 0 {
			return fmt.Sprintf(
				"Consumo historico balanceado (CPU P95 %.0f%%, Mem P95 %.0f%%). %s acomoda esta carga com %.1f%% de reducao de custo.",
				cpuP95, memP95, spec.Size, savingsPct,
			)
		}
		return fmt.Sprintf(
			"Consumo historico balanceado (CPU P95 %.0f%%, Mem P95 %.0f%%). %s oferece geracao mais nova com melhor relacao custo/desempenho.",
			cpuP95, memP95, spec.Size,
		)
	}
	return fmt.Sprintf("Compativel com consumo historico: CPU P95 %.0f%%, Mem P95 %.0f%%.", cpuP95, memP95)
}

// ---------------------------------------------------------------------------
// Câmbio USD → BRL (cache 1h, mesma lógica do deployment cost_analyzer)
// ---------------------------------------------------------------------------

func (ca *NodePoolCostAnalyzer) getExchangeRate() (float64, string) {
	ca.mu.RLock()
	if ca.cachedRate > 0 && time.Now().Before(ca.cachedRateExpires) {
		rate, date := ca.cachedRate, ca.cachedRateDate
		ca.mu.RUnlock()
		return rate, date
	}
	ca.mu.RUnlock()

	rate, date, err := ca.fetchExchangeRate()
	if err != nil {
		log.Warn().Err(err).Float64("fallback", npDefaultExchangeRate).
			Msg("Falha ao buscar cotacao USD/BRL para node pool — usando fallback")
		return npDefaultExchangeRate, time.Now().Format("2006-01-02")
	}

	ca.mu.Lock()
	ca.cachedRate = rate
	ca.cachedRateDate = date
	ca.cachedRateExpires = time.Now().Add(npExchangeRateCacheTTL)
	ca.mu.Unlock()

	return rate, date
}

func (ca *NodePoolCostAnalyzer) fetchExchangeRate() (float64, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get("https://economia.awesomeapi.com.br/json/last/USD-BRL")
	if err != nil {
		return 0, "", fmt.Errorf("falha na requisicao: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("API retornou status %d", resp.StatusCode)
	}

	var result map[string]struct {
		Bid        string `json:"bid"`
		CreateDate string `json:"create_date"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", fmt.Errorf("falha ao decodificar resposta: %w", err)
	}

	usdBRL, ok := result["USDBRL"]
	if !ok {
		return 0, "", fmt.Errorf("campo USDBRL nao encontrado na resposta")
	}

	var rate float64
	if _, err := fmt.Sscanf(usdBRL.Bid, "%f", &rate); err != nil {
		return 0, "", fmt.Errorf("falha ao parsear cotacao '%s': %w", usdBRL.Bid, err)
	}

	date := usdBRL.CreateDate
	if len(date) >= 10 {
		date = date[:10]
	}

	log.Info().Float64("rate", rate).Str("date", date).
		Msg("Cotacao USD/BRL obtida para node pool cost analysis")

	return rate, date, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// percentile95 retorna o valor no percentil 95 de uma slice de float64.
// Usa ordenação simples (pools têm no máximo ~100 nodes).
func percentile95(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// round2 arredonda para 2 casas decimais
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
