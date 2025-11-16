package engine

import (
	"testing"
	"time"
)

func TestNewMonitoringEngineV2(t *testing.T) {
	engine := NewMonitoringEngineV2()

	if engine == nil {
		t.Fatal("Expected non-nil engine")
	}

	if engine.cache == nil {
		t.Error("Expected cache to be initialized")
	}

	if engine.clients == nil {
		t.Error("Expected clients map to be initialized")
	}

	if engine.running {
		t.Error("Expected engine not to be running initially")
	}
}

func TestStartStop(t *testing.T) {
	engine := NewMonitoringEngineV2()

	// Test Start
	err := engine.Start()
	if err != nil {
		t.Errorf("Unexpected error on Start: %v", err)
	}

	if !engine.IsRunning() {
		t.Error("Expected engine to be running after Start")
	}

	// Test Start again (should fail)
	err = engine.Start()
	if err == nil {
		t.Error("Expected error when starting already running engine")
	}

	// Test Stop
	err = engine.Stop()
	if err != nil {
		t.Errorf("Unexpected error on Stop: %v", err)
	}

	if engine.IsRunning() {
		t.Error("Expected engine not to be running after Stop")
	}

	// Test Stop again (should fail)
	err = engine.Stop()
	if err == nil {
		t.Error("Expected error when stopping already stopped engine")
	}
}

func TestIsRunning(t *testing.T) {
	engine := NewMonitoringEngineV2()

	if engine.IsRunning() {
		t.Error("Expected engine not to be running initially")
	}

	engine.Start()

	if !engine.IsRunning() {
		t.Error("Expected engine to be running after Start")
	}

	engine.Stop()

	if engine.IsRunning() {
		t.Error("Expected engine not to be running after Stop")
	}
}

func TestGetPrometheusURL(t *testing.T) {
	engine := NewMonitoringEngineV2()

	tests := []struct {
		cluster string
		want    string
	}{
		{
			cluster: "akspriv-faturamento-hlg-admin",
			want:    "https://prometheus-faturamento-hlg.viavarejo.com.br/",
		},
		{
			cluster: "akspriv-checkout-prod-admin",
			want:    "https://prometheus-checkout-prod.viavarejo.com.br/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cluster, func(t *testing.T) {
			got := engine.GetPrometheusURL(tt.cluster)
			if got != tt.want {
				t.Errorf("GetPrometheusURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClearCache(t *testing.T) {
	engine := NewMonitoringEngineV2()

	// Adicionar algo ao cache
	engine.cache.Set("test-key", "test-value")

	if engine.cache.Size() == 0 {
		t.Error("Expected cache to have items")
	}

	// Limpar cache
	engine.ClearCache()

	if engine.cache.Size() != 0 {
		t.Error("Expected cache to be empty after Clear")
	}
}

func TestClearCacheForHPA(t *testing.T) {
	engine := NewMonitoringEngineV2()

	cluster := "test-cluster"
	namespace := "test-namespace"
	hpaName := "test-hpa"

	cacheKey := "hpa:test-cluster:test-namespace:test-hpa"
	engine.cache.Set(cacheKey, "test-data")

	// Verificar que está no cache
	if _, exists := engine.cache.Get(cacheKey); !exists {
		t.Error("Expected key to be in cache")
	}

	// Limpar cache do HPA
	engine.ClearCacheForHPA(cluster, namespace, hpaName)

	// Verificar que foi removido
	if _, exists := engine.cache.Get(cacheKey); exists {
		t.Error("Expected key to be removed from cache")
	}
}

func TestGetCacheStats(t *testing.T) {
	engine := NewMonitoringEngineV2()

	// Adicionar items ao cache
	engine.cache.Set("key1", "value1")
	engine.cache.Set("key2", "value2")

	stats := engine.GetCacheStats()

	if stats == nil {
		t.Fatal("Expected non-nil stats")
	}

	if totalEntries, ok := stats["total_entries"].(int); !ok || totalEntries != 2 {
		t.Errorf("Expected 2 total entries, got %v", stats["total_entries"])
	}

	if activeEntries, ok := stats["active_entries"].(int); !ok || activeEntries != 2 {
		t.Errorf("Expected 2 active entries, got %v", stats["active_entries"])
	}
}

// Teste de integração - requer endpoint Prometheus disponível
func TestGetHPAMetrics_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	engine := NewMonitoringEngineV2()
	engine.Start()
	defer engine.Stop()

	cluster := "akspriv-logreversa-prd-admin"
	namespace := "ingress-nginx"
	hpaName := "nginx-ingress-controller"

	snapshot, err := engine.GetHPAMetrics(cluster, namespace, hpaName)

	if err != nil {
		t.Skipf("Endpoint não disponível (VPN requerida): %v", err)
	}

	if snapshot == nil {
		t.Fatal("Expected non-nil snapshot")
	}

	if snapshot.Cluster != cluster {
		t.Errorf("Expected cluster %s, got %s", cluster, snapshot.Cluster)
	}

	if snapshot.Namespace != namespace {
		t.Errorf("Expected namespace %s, got %s", namespace, snapshot.Namespace)
	}

	if snapshot.Name != hpaName {
		t.Errorf("Expected name %s, got %s", hpaName, snapshot.Name)
	}

	t.Logf("Snapshot: CurrentReplicas=%v, MinReplicas=%d, MaxReplicas=%d",
		snapshot.CurrentReplicas, snapshot.MinReplicas, snapshot.MaxReplicas)
}

// Teste de cache hit - verifica que segunda chamada vem do cache
func TestGetHPAMetrics_CacheHit_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	engine := NewMonitoringEngineV2()
	engine.Start()
	defer engine.Stop()

	cluster := "akspriv-logreversa-prd-admin"
	namespace := "ingress-nginx"
	hpaName := "nginx-ingress-controller"

	// Primeira chamada - cache miss
	start1 := time.Now()
	snapshot1, err := engine.GetHPAMetrics(cluster, namespace, hpaName)
	duration1 := time.Since(start1)

	if err != nil {
		t.Skipf("Endpoint não disponível (VPN requerida): %v", err)
	}

	// Segunda chamada - deve vir do cache (mais rápida)
	start2 := time.Now()
	snapshot2, err := engine.GetHPAMetrics(cluster, namespace, hpaName)
	duration2 := time.Since(start2)

	if err != nil {
		t.Errorf("Unexpected error on second call: %v", err)
	}

	// Cache hit deve ser significativamente mais rápido
	if duration2 >= duration1 {
		t.Logf("Warning: Cache hit not faster (1st: %v, 2nd: %v)", duration1, duration2)
	}

	// Snapshots devem ser idênticos (mesmo objeto do cache)
	if snapshot1.Timestamp != snapshot2.Timestamp {
		t.Error("Expected same snapshot from cache")
	}

	t.Logf("Cache miss: %v, Cache hit: %v", duration1, duration2)
}

// Teste de múltiplos HPAs em paralelo
func TestGetMultipleHPAMetrics_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	engine := NewMonitoringEngineV2()
	engine.Start()
	defer engine.Stop()

	targets := []HPATarget{
		{
			Cluster:   "akspriv-logreversa-prd-admin",
			Namespace: "ingress-nginx",
			HPAName:   "nginx-ingress-controller",
		},
		// Adicionar mais targets se disponíveis
	}

	results, err := engine.GetMultipleHPAMetrics(targets)

	// Esperado falhar se VPN não disponível, mas não deve crashar
	if err != nil {
		t.Logf("Some targets failed (expected without VPN): %v", err)
	}

	if len(results) != len(targets) {
		t.Errorf("Expected %d results, got %d", len(targets), len(results))
	}

	// Pelo menos um resultado não-nil indica sucesso parcial
	hasSuccess := false
	for _, result := range results {
		if result != nil {
			hasSuccess = true
			t.Logf("Success: %s/%s/%s", result.Cluster, result.Namespace, result.Name)
		}
	}

	if !hasSuccess {
		t.Skip("No successful results (VPN required)")
	}
}
