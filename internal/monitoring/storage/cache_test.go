package storage

import (
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
)

func TestNewTimeSeriesCache(t *testing.T) {
	cache := NewTimeSeriesCache(nil)
	if cache == nil {
		t.Fatal("expected cache to be created")
	}

	if cache.maxDuration != 5*time.Minute {
		t.Errorf("expected maxDuration 5m, got %v", cache.maxDuration)
	}

	if cache.scanInterval != 30*time.Second {
		t.Errorf("expected scanInterval 30s, got %v", cache.scanInterval)
	}

	if cache.maxSnapshots != 10 {
		t.Errorf("expected maxSnapshots 10, got %d", cache.maxSnapshots)
	}
}

func TestCacheAdd(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	snapshot := &models.HPASnapshot{
		Timestamp:       time.Now(),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      70.5,
	}

	err := cache.Add(snapshot)
	if err != nil {
		t.Fatalf("failed to add snapshot: %v", err)
	}

	// Verify snapshot was added
	ts := cache.Get("test-cluster", "default", "test-hpa")
	if ts == nil {
		t.Fatal("expected to find TimeSeriesData")
	}

	if len(ts.Snapshots) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(ts.Snapshots))
	}
}

func TestCacheAddMultiple(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add 5 snapshots
	for i := 0; i < 5; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp:       time.Now().Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: int32(3 + i),
			CPUCurrent:      70.0 + float64(i),
		}

		err := cache.Add(snapshot)
		if err != nil {
			t.Fatalf("failed to add snapshot %d: %v", i, err)
		}
	}

	ts := cache.Get("test-cluster", "default", "test-hpa")
	if ts == nil {
		t.Fatal("expected to find TimeSeriesData")
	}

	if len(ts.Snapshots) != 5 {
		t.Errorf("expected 5 snapshots, got %d", len(ts.Snapshots))
	}
}

func TestCacheGetLatestSnapshot(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	now := time.Now()

	// Add snapshots
	for i := 0; i < 3; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp:       now.Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: int32(3 + i),
			CPUCurrent:      float64(70 + i),
		}
		cache.Add(snapshot)
	}

	latest := cache.GetLatestSnapshot("test-cluster", "default", "test-hpa")
	if latest == nil {
		t.Fatal("expected latest snapshot")
	}

	if latest.CurrentReplicas != 5 {
		t.Errorf("expected replicas 5, got %d", latest.CurrentReplicas)
	}

	if latest.CPUCurrent != 72.0 {
		t.Errorf("expected CPU 72.0, got %.2f", latest.CPUCurrent)
	}
}

func TestCacheCleanup(t *testing.T) {
	config := &CacheConfig{
		MaxDuration:  1 * time.Minute,
		ScanInterval: 10 * time.Second,
	}
	cache := NewTimeSeriesCache(config)

	now := time.Now()

	// Add old snapshot (2 minutes ago)
	oldSnapshot := &models.HPASnapshot{
		Timestamp:       now.Add(-2 * time.Minute),
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 3,
		CPUCurrent:      70.0,
	}
	cache.Add(oldSnapshot)

	// Add recent snapshot
	recentSnapshot := &models.HPASnapshot{
		Timestamp:       now,
		Cluster:         "test-cluster",
		Namespace:       "default",
		Name:            "test-hpa",
		CurrentReplicas: 5,
		CPUCurrent:      75.0,
	}
	cache.Add(recentSnapshot)

	// Cleanup should remove old snapshot
	ts := cache.Get("test-cluster", "default", "test-hpa")
	if ts == nil {
		t.Fatal("expected TimeSeriesData")
	}

	// Should only have 1 snapshot (recent one)
	if len(ts.Snapshots) != 1 {
		t.Errorf("expected 1 snapshot after cleanup, got %d", len(ts.Snapshots))
	}

	if ts.Snapshots[0].CurrentReplicas != 5 {
		t.Errorf("expected recent snapshot to be kept")
	}
}

func TestCalculateStats(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add snapshots with varying CPU
	cpuValues := []float64{70, 75, 80, 85, 90}
	for i, cpu := range cpuValues {
		snapshot := &models.HPASnapshot{
			Timestamp:       time.Now().Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: 5,
			CPUCurrent:      cpu,
			MemoryCurrent:   60.0,
		}
		cache.Add(snapshot)
	}

	ts := cache.Get("test-cluster", "default", "test-hpa")
	if ts == nil {
		t.Fatal("expected TimeSeriesData")
	}

	// Check stats
	stats := ts.Stats

	// Average should be 80
	expectedAvg := 80.0
	if stats.CPUAverage != expectedAvg {
		t.Errorf("expected CPU average %.2f, got %.2f", expectedAvg, stats.CPUAverage)
	}

	// Min should be 70
	if stats.CPUMin != 70.0 {
		t.Errorf("expected CPU min 70.0, got %.2f", stats.CPUMin)
	}

	// Max should be 90
	if stats.CPUMax != 90.0 {
		t.Errorf("expected CPU max 90.0, got %.2f", stats.CPUMax)
	}

	// Trend should be increasing
	if stats.CPUTrend != "increasing" {
		t.Errorf("expected CPU trend 'increasing', got '%s'", stats.CPUTrend)
	}
}

func TestReplicaChanges(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add snapshots with replica changes
	replicas := []int32{3, 3, 5, 5, 7}
	for i, r := range replicas {
		snapshot := &models.HPASnapshot{
			Timestamp:       time.Now().Add(time.Duration(i) * 30 * time.Second),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa",
			CurrentReplicas: r,
			CPUCurrent:      70.0,
		}
		cache.Add(snapshot)
	}

	ts := cache.Get("test-cluster", "default", "test-hpa")
	if ts == nil {
		t.Fatal("expected TimeSeriesData")
	}

	// Should have 2 replica changes (3->5, 5->7)
	if ts.Stats.ReplicaChanges != 2 {
		t.Errorf("expected 2 replica changes, got %d", ts.Stats.ReplicaChanges)
	}

	// Trend should be increasing
	if ts.Stats.ReplicaTrend != "increasing" {
		t.Errorf("expected replica trend 'increasing', got '%s'", ts.Stats.ReplicaTrend)
	}
}

func TestCacheStats(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add snapshots to different HPAs
	for i := 0; i < 3; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp:       time.Now(),
			Cluster:         "test-cluster",
			Namespace:       "default",
			Name:            "test-hpa-" + string(rune('a'+i)),
			CurrentReplicas: 3,
		}
		cache.Add(snapshot)
	}

	stats := cache.Stats()

	if stats.TotalHPAs != 3 {
		t.Errorf("expected 3 HPAs, got %d", stats.TotalHPAs)
	}

	if stats.TotalSnapshots != 3 {
		t.Errorf("expected 3 snapshots, got %d", stats.TotalSnapshots)
	}
}

func TestGetByCluster(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add HPAs from different clusters
	clusters := []string{"cluster-a", "cluster-b", "cluster-a"}
	for i, cluster := range clusters {
		snapshot := &models.HPASnapshot{
			Timestamp: time.Now(),
			Cluster:   cluster,
			Namespace: "default",
			Name:      "hpa-" + string(rune('1'+i)),
		}
		cache.Add(snapshot)
	}

	// Get cluster-a HPAs
	clusterAData := cache.GetByCluster("cluster-a")
	if len(clusterAData) != 2 {
		t.Errorf("expected 2 HPAs from cluster-a, got %d", len(clusterAData))
	}

	// Get cluster-b HPAs
	clusterBData := cache.GetByCluster("cluster-b")
	if len(clusterBData) != 1 {
		t.Errorf("expected 1 HPA from cluster-b, got %d", len(clusterBData))
	}
}

func TestDelete(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	snapshot := &models.HPASnapshot{
		Timestamp: time.Now(),
		Cluster:   "test-cluster",
		Namespace: "default",
		Name:      "test-hpa",
	}
	cache.Add(snapshot)

	// Verify it exists
	if cache.Get("test-cluster", "default", "test-hpa") == nil {
		t.Fatal("expected HPA to exist")
	}

	// Delete it
	cache.Delete("test-cluster", "default", "test-hpa")

	// Verify it's gone
	if cache.Get("test-cluster", "default", "test-hpa") != nil {
		t.Error("expected HPA to be deleted")
	}
}

func TestClear(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add multiple HPAs
	for i := 0; i < 5; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp: time.Now(),
			Cluster:   "test-cluster",
			Namespace: "default",
			Name:      "hpa-" + string(rune('a'+i)),
		}
		cache.Add(snapshot)
	}

	stats := cache.Stats()
	if stats.TotalHPAs != 5 {
		t.Errorf("expected 5 HPAs, got %d", stats.TotalHPAs)
	}

	// Clear cache
	cache.Clear()

	stats = cache.Stats()
	if stats.TotalHPAs != 0 {
		t.Errorf("expected 0 HPAs after clear, got %d", stats.TotalHPAs)
	}
}

func TestMemoryUsage(t *testing.T) {
	cache := NewTimeSeriesCache(nil)

	// Add some snapshots
	for i := 0; i < 10; i++ {
		snapshot := &models.HPASnapshot{
			Timestamp: time.Now(),
			Cluster:   "test-cluster",
			Namespace: "default",
			Name:      "test-hpa",
		}
		cache.Add(snapshot)
	}

	usage := cache.MemoryUsage()
	if usage <= 0 {
		t.Error("expected memory usage > 0")
	}

	// Should be approximately 10 snapshots * 500 bytes + overhead
	expectedMin := int64(10 * 500)
	if usage < expectedMin {
		t.Errorf("expected memory usage >= %d, got %d", expectedMin, usage)
	}
}
