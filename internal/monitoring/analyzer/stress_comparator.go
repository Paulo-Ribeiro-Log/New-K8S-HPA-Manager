package analyzer

import (
	"fmt"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
)

// ComparisonStatus status da comparação baseline vs atual
type ComparisonStatus string

const (
	StatusNormal   ComparisonStatus = "NORMAL"   // Dentro dos limites esperados
	StatusDegraded ComparisonStatus = "DEGRADED" // Degradação detectada mas não crítica
	StatusCritical ComparisonStatus = "CRITICAL" // Degradação crítica detectada
)

// ComparisonResult resultado da comparação baseline vs atual
type ComparisonResult struct {
	// Identificação
	Cluster   string
	Namespace string
	HPA       string
	Timestamp time.Time

	// Deltas de CPU
	CPUBaseline      float64
	CPUCurrent       float64
	CPUDelta         float64 // Diferença absoluta
	CPUDeltaPercent  float64 // Diferença percentual
	CPUExceededLimit bool    // Se CPU atual excedeu threshold

	// Deltas de Memória
	MemoryBaseline      float64
	MemoryCurrent       float64
	MemoryDelta         float64
	MemoryDeltaPercent  float64
	MemoryExceededLimit bool

	// Deltas de Réplicas
	ReplicasBaseline      float64 // Média do baseline
	ReplicasCurrent       int32
	ReplicaDelta          int32   // Diferença absoluta
	ReplicaDeltaPercent   float64 // Diferença percentual
	ReplicasExceededLimit bool    // Se réplicas excederam threshold

	// Métricas de aplicação
	ErrorRateBaseline float64
	ErrorRateCurrent  float64
	ErrorRateDelta    float64
	ErrorRateIncreased bool

	LatencyBaseline float64
	LatencyCurrent  float64
	LatencyDelta    float64
	LatencyIncreased bool

	// Status geral
	Status      ComparisonStatus
	Issues      []string // Lista de problemas detectados
	Severity    string   // low, medium, high, critical
	Description string   // Descrição geral do estado
}

// StressComparator compara snapshots atuais com baseline
type StressComparator struct {
	baseline *models.BaselineSnapshot
	config   *ComparatorConfig
}

// ComparatorConfig configuração do comparador
type ComparatorConfig struct {
	// Thresholds de degradação
	CPUDegradedThreshold   float64 // % de aumento considerado degraded (default: 30%)
	CPUCriticalThreshold   float64 // % de aumento considerado critical (default: 50%)

	MemoryDegradedThreshold float64 // % de aumento considerado degraded (default: 30%)
	MemoryCriticalThreshold float64 // % de aumento considerado critical (default: 50%)

	ReplicaDegradedDelta   int32   // Delta absoluto de réplicas considerado degraded (default: 3)
	ReplicaCriticalDelta   int32   // Delta absoluto de réplicas considerado critical (default: 5)

	ErrorRateThreshold     float64 // Aumento de taxa de erros considerado crítico (default: 5%)
	LatencyThreshold       float64 // % de aumento de latência considerado crítico (default: 100%)
}

// DefaultComparatorConfig retorna configuração padrão
func DefaultComparatorConfig() *ComparatorConfig {
	return &ComparatorConfig{
		CPUDegradedThreshold:    30.0,
		CPUCriticalThreshold:    50.0,
		MemoryDegradedThreshold: 30.0,
		MemoryCriticalThreshold: 50.0,
		ReplicaDegradedDelta:    3,
		ReplicaCriticalDelta:    5,
		ErrorRateThreshold:      5.0,
		LatencyThreshold:        100.0,
	}
}

// NewStressComparator cria novo comparador
func NewStressComparator(baseline *models.BaselineSnapshot, config *ComparatorConfig) *StressComparator {
	if config == nil {
		config = DefaultComparatorConfig()
	}

	return &StressComparator{
		baseline: baseline,
		config:   config,
	}
}

// CompareWithBaseline compara snapshot atual com baseline
func (sc *StressComparator) CompareWithBaseline(current *models.HPASnapshot) *ComparisonResult {
	// Busca baseline do HPA específico
	key := fmt.Sprintf("%s/%s/%s", current.Cluster, current.Namespace, current.Name)
	hpaBaseline, exists := sc.baseline.HPABaselines[key]

	if !exists {
		// HPA não estava no baseline (pode ser novo)
		return &ComparisonResult{
			Cluster:     current.Cluster,
			Namespace:   current.Namespace,
			HPA:         current.Name,
			Timestamp:   time.Now(),
			Status:      StatusNormal,
			Description: "HPA não estava presente no baseline (pode ser novo)",
			Issues:      []string{"HPA não encontrado no baseline"},
			Severity:    "info",
		}
	}

	result := &ComparisonResult{
		Cluster:   current.Cluster,
		Namespace: current.Namespace,
		HPA:       current.Name,
		Timestamp: time.Now(),
		Issues:    []string{},
	}

	// Compara CPU
	result.CPUBaseline = hpaBaseline.CPUAvg
	result.CPUCurrent = current.CPUCurrent
	result.CPUDelta = current.CPUCurrent - hpaBaseline.CPUAvg

	if hpaBaseline.CPUAvg > 0 {
		result.CPUDeltaPercent = (result.CPUDelta / hpaBaseline.CPUAvg) * 100
	}

	// Compara Memória
	result.MemoryBaseline = hpaBaseline.MemoryAvg
	result.MemoryCurrent = current.MemoryCurrent
	result.MemoryDelta = current.MemoryCurrent - hpaBaseline.MemoryAvg

	if hpaBaseline.MemoryAvg > 0 {
		result.MemoryDeltaPercent = (result.MemoryDelta / hpaBaseline.MemoryAvg) * 100
	}

	// Compara Réplicas
	result.ReplicasBaseline = hpaBaseline.ReplicasAvg
	result.ReplicasCurrent = current.CurrentReplicas
	result.ReplicaDelta = current.CurrentReplicas - int32(hpaBaseline.ReplicasAvg)

	if hpaBaseline.ReplicasAvg > 0 {
		result.ReplicaDeltaPercent = (float64(result.ReplicaDelta) / hpaBaseline.ReplicasAvg) * 100
	}

	// Compara métricas de aplicação
	result.ErrorRateBaseline = hpaBaseline.ErrorRateAvg
	result.ErrorRateCurrent = current.ErrorRate
	result.ErrorRateDelta = current.ErrorRate - hpaBaseline.ErrorRateAvg

	result.LatencyBaseline = hpaBaseline.LatencyP95Avg
	result.LatencyCurrent = current.P95Latency
	result.LatencyDelta = current.P95Latency - hpaBaseline.LatencyP95Avg

	// Avalia status
	sc.evaluateStatus(result)

	return result
}

// CompareMultiple compara múltiplos snapshots com baseline
func (sc *StressComparator) CompareMultiple(snapshots []*models.HPASnapshot) []*ComparisonResult {
	results := make([]*ComparisonResult, 0, len(snapshots))

	for _, snapshot := range snapshots {
		result := sc.CompareWithBaseline(snapshot)
		results = append(results, result)
	}

	return results
}

// evaluateStatus avalia o status geral da comparação
func (sc *StressComparator) evaluateStatus(result *ComparisonResult) {
	criticalCount := 0
	degradedCount := 0

	// Avalia CPU
	if result.CPUDeltaPercent >= sc.config.CPUCriticalThreshold {
		criticalCount++
		result.CPUExceededLimit = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"CPU aumentou %.1f%% (de %.1f%% para %.1f%%)",
			result.CPUDeltaPercent, result.CPUBaseline, result.CPUCurrent,
		))
	} else if result.CPUDeltaPercent >= sc.config.CPUDegradedThreshold {
		degradedCount++
		result.CPUExceededLimit = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"CPU aumentou %.1f%% (de %.1f%% para %.1f%%)",
			result.CPUDeltaPercent, result.CPUBaseline, result.CPUCurrent,
		))
	}

	// Avalia Memória
	if result.MemoryDeltaPercent >= sc.config.MemoryCriticalThreshold {
		criticalCount++
		result.MemoryExceededLimit = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"Memória aumentou %.1f%% (de %.1f%% para %.1f%%)",
			result.MemoryDeltaPercent, result.MemoryBaseline, result.MemoryCurrent,
		))
	} else if result.MemoryDeltaPercent >= sc.config.MemoryDegradedThreshold {
		degradedCount++
		result.MemoryExceededLimit = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"Memória aumentou %.1f%% (de %.1f%% para %.1f%%)",
			result.MemoryDeltaPercent, result.MemoryBaseline, result.MemoryCurrent,
		))
	}

	// Avalia Réplicas
	if result.ReplicaDelta >= sc.config.ReplicaCriticalDelta {
		criticalCount++
		result.ReplicasExceededLimit = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"Réplicas aumentaram em %d (de %.0f para %d)",
			result.ReplicaDelta, result.ReplicasBaseline, result.ReplicasCurrent,
		))
	} else if result.ReplicaDelta >= sc.config.ReplicaDegradedDelta {
		degradedCount++
		result.ReplicasExceededLimit = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"Réplicas aumentaram em %d (de %.0f para %d)",
			result.ReplicaDelta, result.ReplicasBaseline, result.ReplicasCurrent,
		))
	}

	// Avalia Taxa de Erros
	if result.ErrorRateDelta >= sc.config.ErrorRateThreshold {
		criticalCount++
		result.ErrorRateIncreased = true
		result.Issues = append(result.Issues, fmt.Sprintf(
			"Taxa de erros aumentou %.2f%% (de %.2f%% para %.2f%%)",
			result.ErrorRateDelta, result.ErrorRateBaseline, result.ErrorRateCurrent,
		))
	}

	// Avalia Latência
	if result.LatencyBaseline > 0 {
		latencyDeltaPercent := (result.LatencyDelta / result.LatencyBaseline) * 100
		if latencyDeltaPercent >= sc.config.LatencyThreshold {
			criticalCount++
			result.LatencyIncreased = true
			result.Issues = append(result.Issues, fmt.Sprintf(
				"Latência P95 aumentou %.1f%% (de %.1fms para %.1fms)",
				latencyDeltaPercent, result.LatencyBaseline, result.LatencyCurrent,
			))
		}
	}

	// Define status geral
	if criticalCount > 0 {
		result.Status = StatusCritical
		result.Severity = "critical"
		result.Description = fmt.Sprintf("%d problemas críticos detectados durante stress test", criticalCount)
	} else if degradedCount > 0 {
		result.Status = StatusDegraded
		result.Severity = "warning"
		result.Description = fmt.Sprintf("%d degradações detectadas durante stress test", degradedCount)
	} else {
		result.Status = StatusNormal
		result.Severity = "info"
		result.Description = "HPA está operando dentro dos limites esperados"
	}
}

// GetSummary retorna resumo das comparações
func (sc *StressComparator) GetSummary(results []*ComparisonResult) *ComparisonSummary {
	summary := &ComparisonSummary{
		TotalHPAs: len(results),
		Timestamp: time.Now(),
	}

	for _, result := range results {
		switch result.Status {
		case StatusCritical:
			summary.CriticalCount++
			summary.CriticalHPAs = append(summary.CriticalHPAs, fmt.Sprintf("%s/%s/%s", result.Cluster, result.Namespace, result.HPA))
		case StatusDegraded:
			summary.DegradedCount++
			summary.DegradedHPAs = append(summary.DegradedHPAs, fmt.Sprintf("%s/%s/%s", result.Cluster, result.Namespace, result.HPA))
		case StatusNormal:
			summary.NormalCount++
		}

		// Acumula métricas
		summary.TotalCPUDelta += result.CPUDelta
		summary.TotalMemoryDelta += result.MemoryDelta
		summary.TotalReplicaDelta += int(result.ReplicaDelta)
	}

	// Calcula percentual de saúde
	if summary.TotalHPAs > 0 {
		summary.HealthPercentage = (float64(summary.NormalCount) / float64(summary.TotalHPAs)) * 100
	}

	return summary
}

// ComparisonSummary resumo geral das comparações
type ComparisonSummary struct {
	Timestamp time.Time

	// Contadores
	TotalHPAs      int
	NormalCount    int
	DegradedCount  int
	CriticalCount  int
	HealthPercentage float64

	// Listas de HPAs problemáticos
	CriticalHPAs []string
	DegradedHPAs []string

	// Métricas agregadas
	TotalCPUDelta     float64
	TotalMemoryDelta  float64
	TotalReplicaDelta int
}

// String retorna representação em string do resumo
func (cs *ComparisonSummary) String() string {
	return fmt.Sprintf(
		"Total: %d HPAs | Normal: %d | Degraded: %d | Critical: %d | Saúde: %.1f%%",
		cs.TotalHPAs, cs.NormalCount, cs.DegradedCount, cs.CriticalCount, cs.HealthPercentage,
	)
}
