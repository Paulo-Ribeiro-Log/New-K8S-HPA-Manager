package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PrometheusMetricsTest representa métricas Prometheus para teste
type PrometheusMetricsTest struct {
	CollectedAt        time.Time `json:"collected_at"`
	CPUUsageCurrent    float64   `json:"cpu_usage_current"`
	CPURequest         float64   `json:"cpu_request"`
	CPULimit           float64   `json:"cpu_limit"`
	MemoryUsageCurrent float64   `json:"memory_usage_current"`
	MemoryRequest      float64   `json:"memory_request"`
	MemoryLimit        float64   `json:"memory_limit"`
	RestartCount       int32     `json:"restart_count"`
	ReadyReplicas      int32     `json:"ready_replicas"`
	DesiredReplicas    int32     `json:"desired_replicas"`
}

func TestAIHistoryStore_SaveAndRetrieveWithPrometheusMetrics(t *testing.T) {
	// Criar diretório temporário para o banco de teste
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_ai_history.db")

	// Criar cliente SQLite
	client, err := NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite client: %v", err)
	}
	defer client.Close()

	// Criar store
	store := NewAIHistoryStore(client)

	// Criar métricas Prometheus de teste
	prometheusMetrics := &PrometheusMetricsTest{
		CollectedAt:        time.Now(),
		CPUUsageCurrent:    250.5,  // 250.5m
		CPURequest:         500.0,  // 500m
		CPULimit:           1000.0, // 1000m = 1 CPU
		MemoryUsageCurrent: 512.0,  // 512 MB
		MemoryRequest:      768.0,  // 768 MB
		MemoryLimit:        1024.0, // 1024 MB = 1 GB
		RestartCount:       3,
		ReadyReplicas:      2,
		DesiredReplicas:    3,
	}

	// Serializar métricas para JSON
	metricsJSON, err := json.Marshal(prometheusMetrics)
	if err != nil {
		t.Fatalf("Failed to marshal Prometheus metrics: %v", err)
	}

	// Criar registro de histórico com métricas
	record := &HistoryRecord{
		ID:                "test-analysis-001",
		ResourceType:      "Pod",
		Cluster:           "test-cluster",
		Namespace:         "default",
		ResourceName:      "nginx-pod-123",
		Provider:          "ollama",
		Model:             "llama3.2:3b",
		Analysis:          "Pod está em CrashLoopBackOff devido a falta de memória",
		Suggestions:       `[{"type":"fix","description":"Aumentar memory limit","priority":"high"}]`,
		PrometheusMetrics: string(metricsJSON), // NOVO - FASE 2.1
		TokensUsed:        1500,
		ResponseTime:      3.5,
		AnalyzedAt:        time.Now(),
		UserEmail:         "test@example.com",
	}

	// Salvar no banco de dados
	err = store.Save(record)
	if err != nil {
		t.Fatalf("Failed to save record with Prometheus metrics: %v", err)
	}

	// Recuperar do banco de dados
	retrieved, err := store.GetByID(record.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve record: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Retrieved record is nil")
	}

	// Verificar se métricas foram salvas corretamente
	if record.ID != retrieved.ID {
		t.Errorf("ID mismatch: expected %s, got %s", record.ID, retrieved.ID)
	}
	if record.ResourceType != retrieved.ResourceType {
		t.Errorf("ResourceType mismatch: expected %s, got %s", record.ResourceType, retrieved.ResourceType)
	}
	if record.Cluster != retrieved.Cluster {
		t.Errorf("Cluster mismatch: expected %s, got %s", record.Cluster, retrieved.Cluster)
	}
	if retrieved.PrometheusMetrics == "" {
		t.Error("Prometheus metrics should not be empty")
	}

	// Deserializar métricas recuperadas
	var retrievedMetrics PrometheusMetricsTest
	err = json.Unmarshal([]byte(retrieved.PrometheusMetrics), &retrievedMetrics)
	if err != nil {
		t.Fatalf("Failed to unmarshal retrieved Prometheus metrics: %v", err)
	}

	// Verificar valores das métricas
	if prometheusMetrics.CPUUsageCurrent != retrievedMetrics.CPUUsageCurrent {
		t.Errorf("CPUUsageCurrent mismatch: expected %.2f, got %.2f",
			prometheusMetrics.CPUUsageCurrent, retrievedMetrics.CPUUsageCurrent)
	}
	if prometheusMetrics.MemoryUsageCurrent != retrievedMetrics.MemoryUsageCurrent {
		t.Errorf("MemoryUsageCurrent mismatch: expected %.2f, got %.2f",
			prometheusMetrics.MemoryUsageCurrent, retrievedMetrics.MemoryUsageCurrent)
	}
	if prometheusMetrics.RestartCount != retrievedMetrics.RestartCount {
		t.Errorf("RestartCount mismatch: expected %d, got %d",
			prometheusMetrics.RestartCount, retrievedMetrics.RestartCount)
	}

	t.Logf("✅ Successfully saved and retrieved Prometheus metrics:")
	t.Logf("   CPU Usage: %.2fm (Request: %.2fm, Limit: %.2fm)",
		retrievedMetrics.CPUUsageCurrent, retrievedMetrics.CPURequest, retrievedMetrics.CPULimit)
	t.Logf("   Memory Usage: %.2fMB (Request: %.2fMB, Limit: %.2fMB)",
		retrievedMetrics.MemoryUsageCurrent, retrievedMetrics.MemoryRequest, retrievedMetrics.MemoryLimit)
	t.Logf("   Replicas: %d/%d (ready/desired)",
		retrievedMetrics.ReadyReplicas, retrievedMetrics.DesiredReplicas)
	t.Logf("   Restart Count: %d", retrievedMetrics.RestartCount)
}

func TestAIHistoryStore_QueryWithPrometheusMetrics(t *testing.T) {
	// Criar diretório temporário para o banco de teste
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_ai_history_query.db")

	// Criar cliente SQLite
	client, err := NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite client: %v", err)
	}
	defer client.Close()

	// Criar store
	store := NewAIHistoryStore(client)

	// Criar múltiplos registros com métricas diferentes
	for i := 0; i < 3; i++ {
		metrics := &PrometheusMetricsTest{
			CollectedAt:        time.Now(),
			CPUUsageCurrent:    float64(100 * (i + 1)), // 100m, 200m, 300m
			MemoryUsageCurrent: float64(256 * (i + 1)), // 256MB, 512MB, 768MB
			RestartCount:       int32(i),
		}

		metricsJSON, _ := json.Marshal(metrics)

		record := &HistoryRecord{
			ID:                string(rune('A' + i)) + "-test-record",
			ResourceType:      "Pod",
			Cluster:           "test-cluster",
			Namespace:         "default",
			ResourceName:      "pod-" + string(rune('1'+i)),
			Provider:          "ollama",
			Analysis:          "Test analysis",
			PrometheusMetrics: string(metricsJSON),
			AnalyzedAt:        time.Now(),
		}

		err := store.Save(record)
		if err != nil {
			t.Fatalf("Failed to save record %d: %v", i, err)
		}
	}

	// Buscar todos os registros
	filters := &QueryFilters{
		Cluster:   "test-cluster",
		Namespace: "default",
		Limit:     10,
	}

	records, err := store.Query(filters)
	if err != nil {
		t.Fatalf("Failed to query records: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("Expected 3 records, got %d", len(records))
	}

	// Verificar se todos têm métricas
	for i, record := range records {
		if record.PrometheusMetrics == "" {
			t.Errorf("Record %d should have Prometheus metrics", i)
		}

		var metrics PrometheusMetricsTest
		err := json.Unmarshal([]byte(record.PrometheusMetrics), &metrics)
		if err != nil {
			t.Fatalf("Failed to unmarshal metrics for record %d: %v", i, err)
		}

		t.Logf("Record %d - CPU: %.2fm, Memory: %.2fMB, Restarts: %d",
			i, metrics.CPUUsageCurrent, metrics.MemoryUsageCurrent, metrics.RestartCount)
	}
}

func TestAIHistoryStore_SaveWithoutPrometheusMetrics(t *testing.T) {
	// Criar diretório temporário para o banco de teste
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_ai_history_no_metrics.db")

	// Criar cliente SQLite
	client, err := NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite client: %v", err)
	}
	defer client.Close()

	// Criar store
	store := NewAIHistoryStore(client)

	// Criar registro SEM métricas Prometheus (backward compatibility)
	record := &HistoryRecord{
		ID:                "test-no-metrics",
		ResourceType:      "Deployment",
		Cluster:           "test-cluster",
		Namespace:         "default",
		ResourceName:      "nginx-deployment",
		Provider:          "ollama",
		Analysis:          "Deployment está healthy",
		PrometheusMetrics: "", // Vazio - sem métricas
		AnalyzedAt:        time.Now(),
	}

	// Salvar no banco de dados
	err = store.Save(record)
	if err != nil {
		t.Fatalf("Failed to save record without metrics: %v", err)
	}

	// Recuperar do banco de dados
	retrieved, err := store.GetByID(record.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve record: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Retrieved record is nil")
	}

	// Verificar que métricas estão vazias (não null)
	if retrieved.PrometheusMetrics != "" {
		t.Errorf("Prometheus metrics should be empty string, got: %s", retrieved.PrometheusMetrics)
	}

	t.Logf("✅ Backward compatibility: Record without metrics saved successfully")
}

func TestMain(m *testing.M) {
	// Setup
	code := m.Run()

	// Teardown
	os.Exit(code)
}
