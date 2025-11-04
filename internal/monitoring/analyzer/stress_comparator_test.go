package analyzer

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
)

// Helper para criar baseline de teste
func createTestBaseline() *models.BaselineSnapshot {
	baseline := &models.BaselineSnapshot{
		Timestamp:     time.Now().Add(-30 * time.Minute),
		Duration:      30 * time.Minute,
		TotalHPAs:     2,
		TotalReplicas: 10,
		HPABaselines:  make(map[string]*models.HPABaseline),
	}

	// HPA 1: Estado normal
	baseline.HPABaselines["cluster1/default/test-hpa-1"] = &models.HPABaseline{
		Cluster:         "cluster1",
		Namespace:       "default",
		Name:            "test-hpa-1",
		MinReplicas:     2,
		MaxReplicas:     10,
		CurrentReplicas: 3,
		CPUAvg:          50.0,
		CPUMax:          60.0,
		CPUMin:          40.0,
		MemoryAvg:       45.0,
		MemoryMax:       55.0,
		MemoryMin:       35.0,
		ReplicasAvg:     3.0,
		ReplicasMax:     4,
		ReplicasMin:     2,
		ErrorRateAvg:    0.5,
		LatencyP95Avg:   100.0,
		Healthy:         true,
	}

	// HPA 2: Estado com CPU alta
	baseline.HPABaselines["cluster1/default/test-hpa-2"] = &models.HPABaseline{
		Cluster:         "cluster1",
		Namespace:       "default",
		Name:            "test-hpa-2",
		MinReplicas:     2,
		MaxReplicas:     10,
		CurrentReplicas: 5,
		CPUAvg:          70.0,
		CPUMax:          80.0,
		CPUMin:          60.0,
		MemoryAvg:       60.0,
		MemoryMax:       70.0,
		MemoryMin:       50.0,
		ReplicasAvg:     5.0,
		ReplicasMax:     6,
		ReplicasMin:     4,
		ErrorRateAvg:    1.0,
		LatencyP95Avg:   150.0,
		Healthy:         false,
		Notes:           "CPU alta durante baseline",
	}

	return baseline
}

// Helper para criar snapshot de teste
func createTestSnapshot(cluster, namespace, name string, cpu, memory float64, replicas int32, errorRate, latency float64) *models.HPASnapshot {
	return &models.HPASnapshot{
		Cluster:        cluster,
		Namespace:      namespace,
		Name:           name,
		MinReplicas:    2,
		MaxReplicas:    10,
		CurrentReplicas: replicas,
		CPUCurrent:     cpu,
		MemoryCurrent:  memory,
		ErrorRate:      errorRate,
		P95Latency:     latency,
		Timestamp:      time.Now(),
	}
}

func TestStressComparator_CompareWithBaseline_Normal(t *testing.T) {
	baseline := createTestBaseline()
	comparator := NewStressComparator(baseline, nil)

	// Snapshot atual similar ao baseline (sem mudanças significativas)
	snapshot := createTestSnapshot("cluster1", "default", "test-hpa-1", 52.0, 46.0, 3, 0.6, 105.0)

	result := comparator.CompareWithBaseline(snapshot)

	// Verificações
	if result.Status != StatusNormal {
		t.Errorf("Expected status NORMAL, got %s", result.Status)
	}

	if result.Severity != "info" {
		t.Errorf("Expected severity 'info', got %s", result.Severity)
	}

	if len(result.Issues) > 0 {
		t.Errorf("Expected no issues for normal state, got %d issues", len(result.Issues))
	}

	// Verifica cálculos de delta
	expectedCPUDelta := 2.0 // 52 - 50
	if result.CPUDelta != expectedCPUDelta {
		t.Errorf("Expected CPU delta %.1f, got %.1f", expectedCPUDelta, result.CPUDelta)
	}

	expectedCPUDeltaPercent := 4.0 // (2 / 50) * 100
	if result.CPUDeltaPercent != expectedCPUDeltaPercent {
		t.Errorf("Expected CPU delta percent %.1f%%, got %.1f%%", expectedCPUDeltaPercent, result.CPUDeltaPercent)
	}
}

func TestStressComparator_CompareWithBaseline_Degraded(t *testing.T) {
	baseline := createTestBaseline()
	config := DefaultComparatorConfig()
	comparator := NewStressComparator(baseline, config)

	// Snapshot com CPU degradada (+35% = degraded mas não critical)
	snapshot := createTestSnapshot("cluster1", "default", "test-hpa-1", 67.5, 46.0, 5, 0.6, 105.0)

	result := comparator.CompareWithBaseline(snapshot)

	// Verificações
	if result.Status != StatusDegraded {
		t.Errorf("Expected status DEGRADED, got %s", result.Status)
	}

	if result.Severity != "warning" {
		t.Errorf("Expected severity 'warning', got %s", result.Severity)
	}

	if !result.CPUExceededLimit {
		t.Error("Expected CPUExceededLimit to be true")
	}

	if len(result.Issues) == 0 {
		t.Error("Expected at least one issue for degraded state")
	}

	// Delta deve ser +35%
	expectedDeltaPercent := 35.0
	if result.CPUDeltaPercent != expectedDeltaPercent {
		t.Errorf("Expected CPU delta percent %.1f%%, got %.1f%%", expectedDeltaPercent, result.CPUDeltaPercent)
	}
}

func TestStressComparator_CompareWithBaseline_Critical(t *testing.T) {
	baseline := createTestBaseline()
	config := DefaultComparatorConfig()
	comparator := NewStressComparator(baseline, config)

	// Snapshot com múltiplas métricas críticas
	snapshot := createTestSnapshot(
		"cluster1", "default", "test-hpa-1",
		80.0,  // CPU +60% (critical)
		70.0,  // Memory +55% (critical)
		9,     // Replicas +6 (critical)
		6.0,   // ErrorRate +5.5% (critical)
		250.0, // Latency +150% (critical)
	)

	result := comparator.CompareWithBaseline(snapshot)

	// Verificações
	if result.Status != StatusCritical {
		t.Errorf("Expected status CRITICAL, got %s", result.Status)
	}

	if result.Severity != "critical" {
		t.Errorf("Expected severity 'critical', got %s", result.Severity)
	}

	if !result.CPUExceededLimit {
		t.Error("Expected CPUExceededLimit to be true")
	}

	if !result.MemoryExceededLimit {
		t.Error("Expected MemoryExceededLimit to be true")
	}

	if !result.ReplicasExceededLimit {
		t.Error("Expected ReplicasExceededLimit to be true")
	}

	if !result.ErrorRateIncreased {
		t.Error("Expected ErrorRateIncreased to be true")
	}

	if !result.LatencyIncreased {
		t.Error("Expected LatencyIncreased to be true")
	}

	// Deve ter múltiplos issues
	if len(result.Issues) < 5 {
		t.Errorf("Expected at least 5 issues for critical state, got %d", len(result.Issues))
	}
}

func TestStressComparator_CompareWithBaseline_HPANotInBaseline(t *testing.T) {
	baseline := createTestBaseline()
	comparator := NewStressComparator(baseline, nil)

	// HPA que não estava no baseline
	snapshot := createTestSnapshot("cluster1", "default", "new-hpa", 50.0, 45.0, 3, 0.5, 100.0)

	result := comparator.CompareWithBaseline(snapshot)

	// Verificações
	if result.Status != StatusNormal {
		t.Errorf("Expected status NORMAL for new HPA, got %s", result.Status)
	}

	if result.Severity != "info" {
		t.Errorf("Expected severity 'info', got %s", result.Severity)
	}

	// Deve ter issue indicando que HPA não estava no baseline
	if len(result.Issues) == 0 {
		t.Error("Expected at least one issue indicating HPA not in baseline")
	}
}

func TestStressComparator_CompareMultiple(t *testing.T) {
	baseline := createTestBaseline()
	comparator := NewStressComparator(baseline, nil)

	snapshots := []*models.HPASnapshot{
		createTestSnapshot("cluster1", "default", "test-hpa-1", 52.0, 46.0, 3, 0.6, 105.0),   // Normal
		createTestSnapshot("cluster1", "default", "test-hpa-2", 100.0, 90.0, 9, 8.0, 300.0), // Critical
	}

	results := comparator.CompareMultiple(snapshots)

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Primeiro deve ser normal
	if results[0].Status != StatusNormal {
		t.Errorf("Expected first result to be NORMAL, got %s", results[0].Status)
	}

	// Segundo deve ser crítico
	if results[1].Status != StatusCritical {
		t.Errorf("Expected second result to be CRITICAL, got %s", results[1].Status)
	}
}

func TestStressComparator_GetSummary(t *testing.T) {
	baseline := createTestBaseline()
	comparator := NewStressComparator(baseline, nil)

	snapshots := []*models.HPASnapshot{
		createTestSnapshot("cluster1", "default", "test-hpa-1", 52.0, 46.0, 3, 0.6, 105.0),   // Normal
		createTestSnapshot("cluster1", "default", "test-hpa-2", 100.0, 90.0, 9, 8.0, 300.0), // Critical
	}

	results := comparator.CompareMultiple(snapshots)
	summary := comparator.GetSummary(results)

	// Verificações
	if summary.TotalHPAs != 2 {
		t.Errorf("Expected 2 total HPAs, got %d", summary.TotalHPAs)
	}

	if summary.NormalCount != 1 {
		t.Errorf("Expected 1 normal HPA, got %d", summary.NormalCount)
	}

	if summary.CriticalCount != 1 {
		t.Errorf("Expected 1 critical HPA, got %d", summary.CriticalCount)
	}

	// Health percentage deve ser 50% (1 de 2 normal)
	expectedHealth := 50.0
	if summary.HealthPercentage != expectedHealth {
		t.Errorf("Expected health percentage %.1f%%, got %.1f%%", expectedHealth, summary.HealthPercentage)
	}

	// Deve ter 1 HPA na lista de críticos
	if len(summary.CriticalHPAs) != 1 {
		t.Errorf("Expected 1 critical HPA in list, got %d", len(summary.CriticalHPAs))
	}
}

func TestStressComparator_CustomConfig(t *testing.T) {
	baseline := createTestBaseline()

	// Config customizado com thresholds mais sensíveis
	config := &ComparatorConfig{
		CPUDegradedThreshold:    10.0, // 10% já é degraded
		CPUCriticalThreshold:    20.0, // 20% é critical
		MemoryDegradedThreshold: 10.0,
		MemoryCriticalThreshold: 20.0,
		ReplicaDegradedDelta:    1,    // +1 réplica já é degraded
		ReplicaCriticalDelta:    2,    // +2 réplicas é critical
		ErrorRateThreshold:      2.0,  // +2% de erro é critical
		LatencyThreshold:        50.0, // +50% de latência é critical
	}

	comparator := NewStressComparator(baseline, config)

	// Snapshot com pequena degradação (seria normal com config padrão)
	snapshot := createTestSnapshot("cluster1", "default", "test-hpa-1", 57.0, 46.0, 4, 0.6, 105.0)

	result := comparator.CompareWithBaseline(snapshot)

	// Com config sensível, deve detectar como degraded
	if result.Status == StatusNormal {
		t.Error("Expected detection with sensitive config, got NORMAL")
	}

	// CPU aumentou 14% (de 50 para 57), deve ser degraded com threshold de 10%
	if !result.CPUExceededLimit {
		t.Error("Expected CPUExceededLimit to be true with sensitive config")
	}
}

func TestComparatorConfig_Default(t *testing.T) {
	config := DefaultComparatorConfig()

	// Verifica valores padrão
	if config.CPUDegradedThreshold != 30.0 {
		t.Errorf("Expected CPU degraded threshold 30.0, got %.1f", config.CPUDegradedThreshold)
	}

	if config.CPUCriticalThreshold != 50.0 {
		t.Errorf("Expected CPU critical threshold 50.0, got %.1f", config.CPUCriticalThreshold)
	}

	if config.ReplicaDegradedDelta != 3 {
		t.Errorf("Expected replica degraded delta 3, got %d", config.ReplicaDegradedDelta)
	}

	if config.ErrorRateThreshold != 5.0 {
		t.Errorf("Expected error rate threshold 5.0, got %.1f", config.ErrorRateThreshold)
	}
}

func TestComparisonSummary_String(t *testing.T) {
	summary := &ComparisonSummary{
		TotalHPAs:        10,
		NormalCount:      7,
		DegradedCount:    2,
		CriticalCount:    1,
		HealthPercentage: 70.0,
	}

	str := summary.String()

	// Deve conter informações chave
	if len(str) == 0 {
		t.Error("Expected non-empty string representation")
	}

	// Verificar se contém os números
	// Nota: teste simples, não faz parse completo da string
	t.Logf("Summary string: %s", str)
}
