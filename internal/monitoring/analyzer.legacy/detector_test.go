package analyzer

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
	"k8s-hpa-manager/internal/monitoring/storage"
)

func TestNewDetector(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	if detector == nil {
		t.Fatal("expected detector to be created")
	}

	if detector.config.OscillationMaxChanges != 5 {
		t.Errorf("expected OscillationMaxChanges 5, got %d", detector.config.OscillationMaxChanges)
	}
}

func TestDetectOscillation(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Adiciona snapshots com muitas mudanças de réplicas
	replicas := []int32{3, 5, 3, 7, 5, 8, 6}
	for i, r := range replicas {
		snapshot := &models.HPASnapshot{
			Timestamp:       time.Now().Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: r,
			MaxReplicas:     10,
			CPUTarget:       70,
			CPUCurrent:      65.0,
		}
		cache.Add(snapshot)
	}

	// Detecta anomalias
	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect oscillation anomaly")
	}

	// Verifica que é oscillation
	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeOscillation {
			found = true
			if anomaly.Severity != models.SeverityCritical {
				t.Errorf("expected Critical severity, got %v", anomaly.Severity)
			}
		}
	}

	if !found {
		t.Error("expected to find OSCILLATION anomaly")
	}
}

func TestDetectMaxedOut(t *testing.T) {
	// Cache com janela de 10min para não limpar durante teste
	cache := storage.NewTimeSeriesCache(&storage.CacheConfig{
		MaxDuration:  10 * time.Minute,
		ScanInterval: 30 * time.Second,
	})
	detector := NewDetector(cache, nil)

	// Adiciona snapshots no limite máximo com CPU alta
	// Precisa cobrir pelo menos 2min de duração
	now := time.Now().Add(-3 * time.Minute) // Começa 3min atrás
	for i := 0; i < 7; i++ { // 7 snapshots * 30s = 3.5min
		snapshot := &models.HPASnapshot{
			Timestamp:       now.Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: 20,
			MaxReplicas:     20, // No limite
			CPUTarget:       70,
			CPUCurrent:      95.0, // 25% acima do target
		}
		cache.Add(snapshot)
	}

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect maxed out anomaly")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeMaxedOut {
			found = true
			if anomaly.Severity != models.SeverityCritical {
				t.Errorf("expected Critical severity, got %v", anomaly.Severity)
			}
			if len(anomaly.Actions) == 0 {
				t.Error("expected actions to be suggested")
			}
		}
	}

	if !found {
		t.Error("expected to find MAXED_OUT anomaly")
	}
}

func TestDetectMaxedOut_NotMaxed(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// HPA não está no limite
	snapshot := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 10,
		MaxReplicas:     20, // Não no limite
		CPUTarget:       70,
		CPUCurrent:      95.0,
	}
	cache.Add(snapshot)

	result := detector.Detect()

	// Não deve detectar maxed out
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeMaxedOut {
			t.Error("should not detect maxed out when not at max replicas")
		}
	}
}

func TestDetectHighErrorRate(t *testing.T) {
	cache := storage.NewTimeSeriesCache(&storage.CacheConfig{
		MaxDuration:  10 * time.Minute,
		ScanInterval: 30 * time.Second,
	})
	detector := NewDetector(cache, nil)

	// Adiciona snapshots com alta taxa de erros
	// Precisa cobrir pelo menos 2min
	now := time.Now().Add(-3 * time.Minute)
	for i := 0; i < 7; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp:       now.Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: 5,
			MaxReplicas:     10,
			CPUTarget:       70,
			CPUCurrent:      75.0,
			ErrorRate:       8.5, // 8.5% > 5% threshold
			RequestRate:     1000.0,
			P95Latency:      500.0,
			DataSource:      models.DataSourcePrometheus,
		}
		cache.Add(snapshot)
	}

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect high error rate anomaly")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeHighErrorRate {
			found = true
			if anomaly.Severity != models.SeverityCritical {
				t.Errorf("expected Critical severity, got %v", anomaly.Severity)
			}
		}
	}

	if !found {
		t.Error("expected to find HIGH_ERROR_RATE anomaly")
	}
}

func TestDetectHighErrorRate_NoPrometheus(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Snapshot sem Prometheus (DataSource != Prometheus)
	snapshot := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 5,
		ErrorRate:       10.0, // Alto mas sem Prometheus
		DataSource:      models.DataSourceMetricsServer,
	}
	cache.Add(snapshot)

	result := detector.Detect()

	// Não deve detectar sem Prometheus
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeHighErrorRate {
			t.Error("should not detect high error rate without Prometheus data")
		}
	}
}

func TestDetectPodsNotReady(t *testing.T) {
	cache := storage.NewTimeSeriesCache(&storage.CacheConfig{
		MaxDuration:  10 * time.Minute,
		ScanInterval: 30 * time.Second,
	})
	detector := NewDetector(cache, nil)

	// Adiciona snapshots com pods not ready
	// Precisa cobrir pelo menos 3min
	now := time.Now().Add(-4 * time.Minute)
	for i := 0; i < 9; i++ { // 9 snapshots * 30s = 4.5min
		snapshot := &models.HPASnapshot{
			Timestamp:       now.Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: 5,
			MaxReplicas:     10,
			Ready:           false, // Not ready
		}
		cache.Add(snapshot)
	}

	result := detector.Detect()

	if len(result.Anomalies) == 0 {
		t.Fatal("expected to detect pods not ready anomaly")
	}

	found := false
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypePodsNotReady {
			found = true
			if anomaly.Severity != models.SeverityCritical {
				t.Errorf("expected Critical severity, got %v", anomaly.Severity)
			}
		}
	}

	if !found {
		t.Error("expected to find PODS_NOT_READY anomaly")
	}
}

func TestDetectMultipleAnomalies(t *testing.T) {
	cache := storage.NewTimeSeriesCache(&storage.CacheConfig{
		MaxDuration:  10 * time.Minute,
		ScanInterval: 30 * time.Second,
	})
	detector := NewDetector(cache, nil)

	// Adiciona snapshots com múltiplas anomalias
	now := time.Now().Add(-4 * time.Minute)
	// Oscillation: muitas mudanças (precisa >5 mudanças)
	// Maxed out: últimos snapshots no limite com CPU alta
	// High error rate: todos com erro alto
	replicas := []int32{10, 15, 10, 20, 15, 20, 15, 20, 20} // 9 snapshots = 4.5min, 6 mudanças
	for i, r := range replicas {
		snapshot := &models.HPASnapshot{
			Timestamp:       now.Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: r,
			MaxReplicas:     20,
			CPUTarget:       70,
			CPUCurrent:      95.0, // Maxed out (25% acima do target)
			ErrorRate:       7.5,  // High error rate (>5%)
			Ready:           true,  // Pods estão ready
			DataSource:      models.DataSourcePrometheus,
		}
		cache.Add(snapshot)
	}

	result := detector.Detect()

	// Deve detectar pelo menos 2 anomalias
	if len(result.Anomalies) < 2 {
		t.Fatalf("expected at least 2 anomalies, got %d", len(result.Anomalies))
	}

	// Verificar quais foram detectadas
	types := make(map[AnomalyType]bool)
	for _, anomaly := range result.Anomalies {
		types[anomaly.Type] = true
		t.Logf("Detected: %s - %s", anomaly.Type, anomaly.Message)
	}

	// Deve ter oscillation (6 mudanças) e high error rate (7.5% > 5%)
	// Maxed out pode ou não ser detectado dependendo dos últimos snapshots
	if !types[AnomalyTypeOscillation] {
		t.Error("expected to detect oscillation")
	}
	if !types[AnomalyTypeHighErrorRate] {
		t.Error("expected to detect high error rate")
	}
}

func TestGetAnomalyCount(t *testing.T) {
	result := &DetectionResult{
		Anomalies: []Anomaly{
			{Type: AnomalyTypeOscillation},
			{Type: AnomalyTypeMaxedOut},
			{Type: AnomalyTypeOscillation},
		},
	}

	counts := result.GetAnomalyCount()

	if counts[AnomalyTypeOscillation] != 2 {
		t.Errorf("expected 2 oscillation anomalies, got %d", counts[AnomalyTypeOscillation])
	}

	if counts[AnomalyTypeMaxedOut] != 1 {
		t.Errorf("expected 1 maxed out anomaly, got %d", counts[AnomalyTypeMaxedOut])
	}
}

func TestGetBySeverity(t *testing.T) {
	result := &DetectionResult{
		Anomalies: []Anomaly{
			{Type: AnomalyTypeOscillation, Severity: models.SeverityCritical},
			{Type: AnomalyTypeMaxedOut, Severity: models.SeverityCritical},
			{Type: AnomalyTypeHighErrorRate, Severity: models.SeverityWarning},
		},
	}

	critical := result.GetBySeverity(models.SeverityCritical)
	if len(critical) != 2 {
		t.Errorf("expected 2 critical anomalies, got %d", len(critical))
	}

	warning := result.GetBySeverity(models.SeverityWarning)
	if len(warning) != 1 {
		t.Errorf("expected 1 warning anomaly, got %d", len(warning))
	}
}

func TestGetByCluster(t *testing.T) {
	result := &DetectionResult{
		Anomalies: []Anomaly{
			{Cluster: "cluster-a"},
			{Cluster: "cluster-b"},
			{Cluster: "cluster-a"},
		},
	}

	clusterA := result.GetByCluster("cluster-a")
	if len(clusterA) != 2 {
		t.Errorf("expected 2 anomalies from cluster-a, got %d", len(clusterA))
	}

	clusterB := result.GetByCluster("cluster-b")
	if len(clusterB) != 1 {
		t.Errorf("expected 1 anomaly from cluster-b, got %d", len(clusterB))
	}
}

func TestMinDuration(t *testing.T) {
	cache := storage.NewTimeSeriesCache(nil)
	detector := NewDetector(cache, nil)

	// Adiciona apenas 1 snapshot no limite (não deve alertar)
	snapshot := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 20,
		MaxReplicas:     20,
		CPUTarget:       70,
		CPUCurrent:      95.0,
	}
	cache.Add(snapshot)

	result := detector.Detect()

	// Não deve alertar pois não atingiu duração mínima
	for _, anomaly := range result.Anomalies {
		if anomaly.Type == AnomalyTypeMaxedOut {
			t.Error("should not alert on first snapshot (min duration not met)")
		}
	}
}
