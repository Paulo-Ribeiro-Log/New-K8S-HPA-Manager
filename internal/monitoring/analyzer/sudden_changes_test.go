package analyzer

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/storage"
)

func TestDetectCPUSpike(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Adiciona snapshot com CPU normal (30%)
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      30.0, // 30%
	}
	cache.Add(snapshot1)

	// Adiciona snapshot com CPU spike (95% = +216% de variação)
	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      95.0, // 95% (spike de 30% → 95%)
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect CPU spike")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeCPUSpike {
			found = true
			t.Logf("CPU Spike detected: %s", anomaly.Message)
			if anomaly.Severity != models.SeverityWarning {
				t.Errorf("expected Warning severity, got %v", anomaly.Severity)
			}
		}
	}

	if !found {
		t.Error("expected to find CPU_SPIKE anomaly")
	}
}

func TestDetectCPUSpikeNoChange(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Adiciona dois snapshots com CPU estável
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      50.0,
	}
	cache.Add(snapshot1)

	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      52.0, // Variação pequena (4%)
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	// Não deve detectar CPU spike
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeCPUSpike {
			t.Error("should not detect CPU spike for small change")
		}
	}
}

func TestDetectReplicaSpike(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Snapshot com poucas réplicas
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
	}
	cache.Add(snapshot1)

	// Snapshot com spike de réplicas (3 → 10 = +7)
	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 10, // +7 replicas
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect replica spike")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeReplicaSpike {
			found = true
			t.Logf("Replica Spike detected: %s", anomaly.Message)
		}
	}

	if !found {
		t.Error("expected to find REPLICA_SPIKE anomaly")
	}
}

func TestDetectErrorSpike(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Snapshot com error rate baixo
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		ErrorRate:       1.0, // 1% errors
		DataSource:      models.DataSourcePrometheus,
	}
	cache.Add(snapshot1)

	// Snapshot com error spike (1% → 10% = +9%)
	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		ErrorRate:       10.0, // 10% errors (spike de +9%)
		DataSource:      models.DataSourcePrometheus,
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect error spike")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeErrorSpike {
			found = true
			t.Logf("Error Spike detected: %s", anomaly.Message)
			if anomaly.Severity != models.SeverityCritical {
				t.Errorf("expected Critical severity, got %v", anomaly.Severity)
			}
		}
	}

	if !found {
		t.Error("expected to find ERROR_SPIKE anomaly")
	}
}

func TestDetectLatencySpike(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Snapshot com latency normal
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		P95Latency:      100.0, // 100ms
		DataSource:      models.DataSourcePrometheus,
	}
	cache.Add(snapshot1)

	// Snapshot com latency spike (100ms → 500ms = +400%)
	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		P95Latency:      500.0, // 500ms (spike de +400%)
		DataSource:      models.DataSourcePrometheus,
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect latency spike")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeLatencySpike {
			found = true
			t.Logf("Latency Spike detected: %s", anomaly.Message)
		}
	}

	if !found {
		t.Error("expected to find LATENCY_SPIKE anomaly")
	}
}

func TestDetectCPUDrop(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Snapshot com CPU alto
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 10,
		CPUCurrent:      90.0, // 90%
	}
	cache.Add(snapshot1)

	// Snapshot com CPU drop (90% → 20% = -77% de queda)
	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 10,
		CPUCurrent:      20.0, // 20% (drop de 90% → 20%)
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect CPU drop")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeCPUDrop {
			found = true
			t.Logf("CPU Drop detected: %s", anomaly.Message)
		}
	}

	if !found {
		t.Error("expected to find CPU_DROP anomaly")
	}
}

func TestMultipleSuddenChanges(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Snapshot inicial
	snapshot1 := &models.HPASnapshot{
		Timestamp:       time.Now().Add(-30 * time.Second),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      30.0,
		ErrorRate:       1.0,
		P95Latency:      100.0,
		DataSource:      models.DataSourcePrometheus,
	}
	cache.Add(snapshot1)

	// Snapshot com múltiplas variações
	snapshot2 := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 10,    // +7 (replica spike)
		CPUCurrent:      95.0,  // +216% (CPU spike)
		ErrorRate:       12.0,  // +11% (error spike)
		P95Latency:      500.0, // +400% (latency spike)
		DataSource:      models.DataSourcePrometheus,
	}
	cache.Add(snapshot2)

	result := detector.Detect()

	t.Logf("Detected %d anomalies", len(result.Anomalies))

	// Deve detectar múltiplas anomalias
	types := make(map[AnomalyType]bool)
	for _, anomaly := range result.Anomalies {
		types[anomaly.Type] = true
		t.Logf("Detected: %s - %s", anomaly.Type, anomaly.Message)
	}

	// Verifica que foram detectadas
	expectedTypes := []AnomalyType{
		AnomalyTypeCPUSpike,
		AnomalyTypeReplicaSpike,
		AnomalyTypeErrorSpike,
		AnomalyTypeLatencySpike,
	}

	for _, expectedType := range expectedTypes {
		if !types[expectedType] {
			t.Errorf("expected to detect %s", expectedType)
		}
	}
}

func TestSuddenChangeRequiresTwoSnapshots(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Adiciona apenas 1 snapshot (não deve detectar sudden changes)
	snapshot := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 10,
		CPUCurrent:      95.0,
	}
	cache.Add(snapshot)

	result := detector.Detect()

	// Não deve detectar sudden changes (precisa de 2 snapshots para comparar)
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeCPUSpike ||
			anomaly.Type == AnomalyTypeReplicaSpike ||
			anomaly.Type == AnomalyTypeErrorSpike ||
			anomaly.Type == AnomalyTypeLatencySpike ||
			anomaly.Type == AnomalyTypeCPUDrop {
			t.Errorf("should not detect sudden changes with only 1 snapshot: %s", anomaly.Type)
		}
	}
}
