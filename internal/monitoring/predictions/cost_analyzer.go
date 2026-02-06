package predictions

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Preços Azure pay-as-you-go (referência Brasil Sul)
const (
	PriceCPUCoreHour = 0.05  // USD por vCPU/hora
	PriceMemGBHour   = 0.005 // USD por GB/hora
	HoursPerMonth    = 730   // 365 dias × 24h / 12 meses

	// Thresholds de over-provisioning
	CPUOverProvisionThreshold = 0.30 // P95 < 30% do request = over-provisioned
	MemOverProvisionThreshold = 0.40 // P95 < 40% do request = over-provisioned

	// Margem de segurança para right-sizing
	RightSizingMargin = 1.20 // P95 + 20%

	// Cotação fallback quando API falha
	DefaultExchangeRate = 5.50

	// Cache da cotação
	exchangeRateCacheTTL = 1 * time.Hour
)

// CostAnalyzer calcula custos de deployments
type CostAnalyzer struct {
	mu                sync.RWMutex
	cachedRate        float64
	cachedRateDate    string
	cachedRateExpires time.Time
}

// NewCostAnalyzer cria novo analisador de custos
func NewCostAnalyzer() *CostAnalyzer {
	return &CostAnalyzer{}
}

// Calculate calcula análise de custo completa para o deployment
func (ca *CostAnalyzer) Calculate(metrics *DeploymentMetrics) *CostAnalysis {
	// 1. Buscar cotação USD/BRL
	rate, rateDate := ca.getExchangeRate()

	// 2. Parsear resources do deployment
	cpuRequestCores := ca.parseResourceToCores(metrics.Resources.CPURequest)
	memRequestGB := ca.parseResourceToGB(metrics.Resources.MemoryRequest)

	replicas := float64(metrics.CurrentReplicas)
	if replicas == 0 {
		replicas = 1
	}

	// 3. Calcular custo atual: (cpu * preço + mem * preço) * replicas * 730h
	cpuCostUSD := cpuRequestCores * PriceCPUCoreHour * HoursPerMonth * replicas
	memCostUSD := memRequestGB * PriceMemGBHour * HoursPerMonth * replicas
	totalCostUSD := cpuCostUSD + memCostUSD
	costPerReplicaUSD := totalCostUSD / replicas

	// 4. Calcular custo otimizado (right-sizing baseado em P95 + 20% margem)
	cpuP95PerReplica := metrics.Current.CPUUsageP95 / replicas
	memP95PerReplica := metrics.Current.MemoryUsageP95 / replicas / (1024 * 1024 * 1024) // bytes → GB

	// Right-size: usar P95 + margem, mas nunca mais que o request atual
	optimizedCPU := math.Min(cpuP95PerReplica*RightSizingMargin, cpuRequestCores)
	optimizedMem := math.Min(memP95PerReplica*RightSizingMargin, memRequestGB)

	// Garantir mínimos razoáveis
	if optimizedCPU < 0.05 {
		optimizedCPU = 0.05 // mínimo 50m
	}
	if optimizedMem < 0.064 {
		optimizedMem = 0.064 // mínimo 64Mi
	}

	optimizedCPUCost := optimizedCPU * PriceCPUCoreHour * HoursPerMonth * replicas
	optimizedMemCost := optimizedMem * PriceMemGBHour * HoursPerMonth * replicas
	recommendedCostUSD := optimizedCPUCost + optimizedMemCost

	savingsUSD := totalCostUSD - recommendedCostUSD
	if savingsUSD < 0 {
		savingsUSD = 0
		recommendedCostUSD = totalCostUSD
	}

	savingsPercent := 0.0
	if totalCostUSD > 0 {
		savingsPercent = (savingsUSD / totalCostUSD) * 100
	}

	// 5. Detectar over-provisioning e gerar recomendações
	recommendations := ca.detectOverProvisioning(metrics, rate, cpuRequestCores, memRequestGB, replicas)

	analysis := &CostAnalysis{
		ExchangeRate:     rate,
		ExchangeRateDate: rateDate,

		CurrentMonthlyCostUSD: math.Round(totalCostUSD*100) / 100,
		CurrentMonthlyCostBRL: math.Round(totalCostUSD*rate*100) / 100,
		CostPerReplicaUSD:     math.Round(costPerReplicaUSD*100) / 100,
		CostPerReplicaBRL:     math.Round(costPerReplicaUSD*rate*100) / 100,
		CostBreakdown: CostBreakdown{
			CPUCostUSD:    math.Round(cpuCostUSD*100) / 100,
			CPUCostBRL:    math.Round(cpuCostUSD*rate*100) / 100,
			MemoryCostUSD: math.Round(memCostUSD*100) / 100,
			MemoryCostBRL: math.Round(memCostUSD*rate*100) / 100,
		},

		RecommendedCostUSD: math.Round(recommendedCostUSD*100) / 100,
		RecommendedCostBRL: math.Round(recommendedCostUSD*rate*100) / 100,
		MonthlySavingsUSD:  math.Round(savingsUSD*100) / 100,
		MonthlySavingsBRL:  math.Round(savingsUSD*rate*100) / 100,
		AnnualSavingsUSD:   math.Round(savingsUSD*12*100) / 100,
		AnnualSavingsBRL:   math.Round(savingsUSD*12*rate*100) / 100,
		SavingsPercent:     math.Round(savingsPercent*10) / 10,

		Recommendations: recommendations,
	}

	log.Info().
		Float64("cost_usd", analysis.CurrentMonthlyCostUSD).
		Float64("cost_brl", analysis.CurrentMonthlyCostBRL).
		Float64("savings_usd", analysis.MonthlySavingsUSD).
		Float64("savings_percent", analysis.SavingsPercent).
		Float64("exchange_rate", rate).
		Str("deployment", metrics.Deployment).
		Msg("Cost analysis completed")

	return analysis
}

// detectOverProvisioning detecta recursos super-provisionados e gera recomendações
func (ca *CostAnalyzer) detectOverProvisioning(
	metrics *DeploymentMetrics,
	rate float64,
	cpuRequestCores float64,
	memRequestGB float64,
	replicas float64,
) []CostRecommendation {
	var recs []CostRecommendation

	if replicas == 0 {
		replicas = 1
	}

	// CPU over-provisioning: P95 < 30% do request
	cpuP95PerReplica := metrics.Current.CPUUsageP95 / replicas
	if cpuRequestCores > 0 && cpuP95PerReplica/cpuRequestCores < CPUOverProvisionThreshold {
		// Sugerir right-size: P95 + 20% margem
		suggestedCPU := cpuP95PerReplica * RightSizingMargin
		if suggestedCPU < 0.05 {
			suggestedCPU = 0.05
		}

		costBefore := cpuRequestCores * PriceCPUCoreHour * HoursPerMonth * replicas
		costAfter := suggestedCPU * PriceCPUCoreHour * HoursPerMonth * replicas
		savings := costBefore - costAfter

		if savings > 0.50 { // só recomendar se economia > $0.50/mês
			recs = append(recs, CostRecommendation{
				Title: "Reduzir CPU request (over-provisioned)",
				Description: fmt.Sprintf(
					"CPU P95 usa apenas %.0f%% do request. Reduzir de %s para %sm libera recursos sem impacto.",
					(cpuP95PerReplica/cpuRequestCores)*100,
					metrics.Resources.CPURequest,
					formatCPU(suggestedCPU),
				),
				Action:        "downsize_cpu",
				CostBeforeUSD: math.Round(costBefore*100) / 100,
				CostAfterUSD:  math.Round(costAfter*100) / 100,
				SavingsUSD:    math.Round(savings*100) / 100,
				SavingsBRL:    math.Round(savings*rate*100) / 100,
				Impact:        "low",
			})
		}
	}

	// Memory over-provisioning: P95 < 40% do request
	memP95PerReplica := metrics.Current.MemoryUsageP95 / replicas / (1024 * 1024 * 1024) // bytes → GB
	if memRequestGB > 0 && memP95PerReplica/memRequestGB < MemOverProvisionThreshold {
		suggestedMem := memP95PerReplica * RightSizingMargin
		if suggestedMem < 0.064 {
			suggestedMem = 0.064
		}

		costBefore := memRequestGB * PriceMemGBHour * HoursPerMonth * replicas
		costAfter := suggestedMem * PriceMemGBHour * HoursPerMonth * replicas
		savings := costBefore - costAfter

		if savings > 0.50 {
			recs = append(recs, CostRecommendation{
				Title: "Reduzir Memory request (over-provisioned)",
				Description: fmt.Sprintf(
					"Memoria P95 usa apenas %.0f%% do request. Reduzir de %s para %s libera recursos.",
					(memP95PerReplica/memRequestGB)*100,
					metrics.Resources.MemoryRequest,
					formatMemory(suggestedMem),
				),
				Action:        "right_size_memory",
				CostBeforeUSD: math.Round(costBefore*100) / 100,
				CostAfterUSD:  math.Round(costAfter*100) / 100,
				SavingsUSD:    math.Round(savings*100) / 100,
				SavingsBRL:    math.Round(savings*rate*100) / 100,
				Impact:        "low",
			})
		}
	}

	// Réplicas ociosas: se CPU e Memory totais estão muito baixas
	totalCPUUsage := metrics.Current.CPUUsageAvg
	totalCPUCapacity := cpuRequestCores * replicas
	if replicas > 1 && totalCPUCapacity > 0 && totalCPUUsage/totalCPUCapacity < 0.20 {
		// Uso total < 20% da capacidade alocada - pode ter réplicas demais
		minReplicas := math.Ceil(totalCPUUsage / (cpuRequestCores * 0.60)) // target 60% usage
		if minReplicas < 1 {
			minReplicas = 1
		}

		if minReplicas < replicas {
			costPerReplica := (cpuRequestCores*PriceCPUCoreHour + memRequestGB*PriceMemGBHour) * HoursPerMonth
			savings := (replicas - minReplicas) * costPerReplica

			if savings > 1.00 {
				recs = append(recs, CostRecommendation{
					Title: "Reduzir numero de replicas",
					Description: fmt.Sprintf(
						"Uso total de CPU esta em %.0f%% da capacidade alocada. Reduzir de %.0f para %.0f replicas.",
						(totalCPUUsage/totalCPUCapacity)*100,
						replicas, minReplicas,
					),
					Action:        "reduce_replicas",
					CostBeforeUSD: math.Round(costPerReplica*replicas*100) / 100,
					CostAfterUSD:  math.Round(costPerReplica*minReplicas*100) / 100,
					SavingsUSD:    math.Round(savings*100) / 100,
					SavingsBRL:    math.Round(savings*rate*100) / 100,
					Impact:        "medium",
				})
			}
		}
	}

	return recs
}

// getExchangeRate retorna cotação USD→BRL com cache de 1h
func (ca *CostAnalyzer) getExchangeRate() (float64, string) {
	ca.mu.RLock()
	if ca.cachedRate > 0 && time.Now().Before(ca.cachedRateExpires) {
		rate := ca.cachedRate
		date := ca.cachedRateDate
		ca.mu.RUnlock()
		return rate, date
	}
	ca.mu.RUnlock()

	// Buscar cotação da API
	rate, date, err := ca.fetchExchangeRate()
	if err != nil {
		log.Warn().Err(err).Float64("fallback_rate", DefaultExchangeRate).
			Msg("Falha ao buscar cotacao USD/BRL, usando fallback")
		return DefaultExchangeRate, time.Now().Format("2006-01-02")
	}

	// Cachear resultado
	ca.mu.Lock()
	ca.cachedRate = rate
	ca.cachedRateDate = date
	ca.cachedRateExpires = time.Now().Add(exchangeRateCacheTTL)
	ca.mu.Unlock()

	return rate, date
}

// fetchExchangeRate busca cotação comercial USD→BRL via API pública
func (ca *CostAnalyzer) fetchExchangeRate() (float64, string, error) {
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

	// Extrair apenas a data (sem hora)
	date := usdBRL.CreateDate
	if len(date) >= 10 {
		date = date[:10]
	}

	log.Info().
		Float64("rate", rate).
		Str("date", date).
		Msg("Cotacao USD/BRL obtida com sucesso")

	return rate, date, nil
}

// parseResourceToCores converte resource string para cores
// Ex: "500m" → 0.5, "1" → 1.0, "2000m" → 2.0
func (ca *CostAnalyzer) parseResourceToCores(resource string) float64 {
	if resource == "" || resource == "0" {
		return 0
	}
	if strings.HasSuffix(resource, "m") {
		val := 0.0
		fmt.Sscanf(strings.TrimSuffix(resource, "m"), "%f", &val)
		return val / 1000.0
	}
	val := 0.0
	fmt.Sscanf(resource, "%f", &val)
	return val
}

// parseResourceToGB converte resource string para GB
// Ex: "512Mi" → 0.5, "1Gi" → 1.0, "2048Mi" → 2.0
func (ca *CostAnalyzer) parseResourceToGB(resource string) float64 {
	if resource == "" || resource == "0" {
		return 0
	}
	multipliers := map[string]float64{
		"Ki": 1.0 / (1024 * 1024),
		"Mi": 1.0 / 1024,
		"Gi": 1.0,
		"Ti": 1024.0,
	}
	for suffix, mult := range multipliers {
		if strings.HasSuffix(resource, suffix) {
			val := 0.0
			fmt.Sscanf(strings.TrimSuffix(resource, suffix), "%f", &val)
			return val * mult
		}
	}
	// Bytes puros
	val := 0.0
	fmt.Sscanf(resource, "%f", &val)
	return val / (1024 * 1024 * 1024)
}

// formatCPU formata cores para string legível (millicores se < 1)
func formatCPU(cores float64) string {
	if cores < 1 {
		return fmt.Sprintf("%.0f", cores*1000) // ex: "500" (será "500m" no contexto)
	}
	return fmt.Sprintf("%.1f", cores) // ex: "2.0"
}

// formatMemory formata GB para string legível
func formatMemory(gb float64) string {
	if gb < 1 {
		return fmt.Sprintf("%.0fMi", gb*1024)
	}
	return fmt.Sprintf("%.1fGi", gb)
}
