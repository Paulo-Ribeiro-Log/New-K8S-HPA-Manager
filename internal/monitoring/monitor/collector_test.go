package monitor

import (
	"context"
	"testing"
	"time"

	"k8s-hpa-manager/internal/monitoring/models"
)

func TestNewCollector(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := &models.ClusterInfo{
		Name:    "test-cluster",
		Context: "kind-kind", // Ajustar para seu cluster
		Server:  "https://127.0.0.1:6443",
	}

	config := DefaultCollectorConfig()
	config.ScanInterval = 10 * time.Second

	collector, err := NewCollector(cluster, "", config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if collector == nil {
		t.Fatal("expected collector to be created")
	}

	if collector.cluster.Name != "test-cluster" {
		t.Errorf("expected cluster name 'test-cluster', got %s", collector.cluster.Name)
	}

	if collector.cache == nil {
		t.Error("expected cache to be initialized")
	}

	if collector.detector == nil {
		t.Error("expected detector to be initialized")
	}
}

func TestCollectorScan(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := &models.ClusterInfo{
		Name:    "test-cluster",
		Context: "kind-kind", // Ajustar para seu cluster
		Server:  "https://127.0.0.1:6443",
	}

	config := DefaultCollectorConfig()
	config.ExcludeNamespaces = []string{"monitoring", "logging"}

	collector, err := NewCollector(cluster, "", config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	result, err := collector.Scan(ctx)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected scan result")
	}

	t.Logf("Scan completed:")
	t.Logf("  Cluster: %s", result.Cluster)
	t.Logf("  Snapshots: %d", result.SnapshotsCount)
	t.Logf("  Anomalies: %d", len(result.Anomalies))
	t.Logf("  Errors: %d", len(result.Errors))
	t.Logf("  Duration: %v", result.Duration)

	// Verifica stats do cache
	stats := collector.GetStats()
	t.Logf("Collector stats:")
	t.Logf("  Total HPAs: %d", stats.TotalHPAs)
	t.Logf("  Total Snapshots: %d", stats.TotalSnapshots)
	t.Logf("  Memory Usage: %d bytes", stats.MemoryUsage)
	t.Logf("  Prometheus Connected: %v", stats.PrometheusConnected)

	if stats.TotalHPAs == 0 {
		t.Log("Warning: No HPAs found in cluster (this might be expected)")
	}
}

func TestCollectorMonitoringLoop(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := &models.ClusterInfo{
		Name:    "test-cluster",
		Context: "kind-kind", // Ajustar para seu cluster
		Server:  "https://127.0.0.1:6443",
	}

	config := DefaultCollectorConfig()
	config.ScanInterval = 5 * time.Second // Scan rápido para teste

	collector, err := NewCollector(cluster, "", config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	// Channel para receber resultados
	resultChan := make(chan *ScanResult, 10)

	// Context com timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Inicia monitoring em goroutine
	go collector.StartMonitoring(ctx, resultChan)

	// Coleta resultados
	scanCount := 0
	for {
		select {
		case result := <-resultChan:
			scanCount++
			t.Logf("Scan #%d:", scanCount)
			t.Logf("  Timestamp: %v", result.Timestamp)
			t.Logf("  Snapshots: %d", result.SnapshotsCount)
			t.Logf("  Anomalies: %d", len(result.Anomalies))

			// Lista anomalias detectadas
			for _, anomaly := range result.Anomalies {
				t.Logf("  Anomaly: %s - %s", anomaly.Type, anomaly.Message)
			}

			// Para após 2 scans
			if scanCount >= 2 {
				cancel()
				return
			}

		case <-ctx.Done():
			t.Logf("Monitoring loop stopped after %d scans", scanCount)

			if scanCount == 0 {
				t.Error("expected at least one scan")
			}

			return
		}
	}
}

func TestCollectorWithPrometheus(t *testing.T) {
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := &models.ClusterInfo{
		Name:    "test-cluster",
		Context: "kind-kind",
		Server:  "https://127.0.0.1:6443",
	}

	config := DefaultCollectorConfig()
	config.EnablePrometheus = true

	// Prometheus endpoint (ajustar para seu ambiente)
	prometheusEndpoint := "http://localhost:9090"

	collector, err := NewCollector(cluster, prometheusEndpoint, config)
	if err != nil {
		t.Fatalf("failed to create collector: %v", err)
	}

	if !collector.IsPrometheusConnected() {
		t.Log("Warning: Prometheus not connected (this might be expected)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	result, err := collector.Scan(ctx)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	t.Logf("Scan with Prometheus:")
	t.Logf("  Snapshots: %d", result.SnapshotsCount)
	t.Logf("  Prometheus Connected: %v", collector.IsPrometheusConnected())

	// Verifica se algum snapshot foi enriched com Prometheus
	cache := collector.GetCache()
	allData := cache.GetAll()

	hasPrometheusData := false
	for _, ts := range allData {
		if len(ts.Snapshots) > 0 {
			latest := ts.GetLatest()
			if latest != nil && latest.DataSource == models.DataSourcePrometheus {
				hasPrometheusData = true
				t.Logf("HPA %s enriched with Prometheus:", latest.Name)
				t.Logf("  CPU Current: %.2f%%", latest.CPUCurrent)
				t.Logf("  Memory Current: %.2f%%", latest.MemoryCurrent)
				t.Logf("  Error Rate: %.2f%%", latest.ErrorRate)
				break
			}
		}
	}

	if collector.IsPrometheusConnected() && !hasPrometheusData {
		t.Log("Warning: Prometheus connected but no data enriched")
	}
}

func TestDefaultCollectorConfig(t *testing.T) {
	config := DefaultCollectorConfig()

	if config.ScanInterval != 30*time.Second {
		t.Errorf("expected scan interval 30s, got %v", config.ScanInterval)
	}

	if !config.EnablePrometheus {
		t.Error("expected Prometheus to be enabled by default")
	}

	if config.DetectorConfig == nil {
		t.Error("expected detector config to be initialized")
	}
}
