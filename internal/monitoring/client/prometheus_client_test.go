package client

import (
	"context"
	"testing"
	"time"
)

// Teste de criação de cliente (requer endpoint disponível)
func TestNewPrometheusClient_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := "akspriv-logreversa-prd-admin"

	client, err := NewPrometheusClient(cluster)

	// Esperado falhar se não houver VPN
	if err != nil {
		t.Logf("Endpoint não disponível (esperado sem VPN): %v", err)
		return
	}

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	defer client.Close()

	if client.endpoint.Cluster != cluster {
		t.Errorf("Expected cluster %s, got %s", cluster, client.endpoint.Cluster)
	}

	if client.httpClient == nil {
		t.Error("Expected non-nil httpClient")
	}
}

// Teste de Query (requer endpoint disponível)
func TestQuery_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := "akspriv-logreversa-prd-admin"
	client, err := NewPrometheusClient(cluster)
	if err != nil {
		t.Skip("Endpoint não disponível (VPN requerida)")
	}
	defer client.Close()

	ctx := context.Background()
	query := `kube_horizontalpodautoscaler_status_current_replicas{namespace="ingress-nginx",horizontalpodautoscaler="nginx-ingress-controller"}`

	result, err := client.Query(ctx, query)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("Expected status success, got %s", result.Status)
	}

	if len(result.Data.Result) == 0 {
		t.Error("Expected at least one result")
	}

	t.Logf("Query returned %d results", len(result.Data.Result))
	if len(result.Data.Result) > 0 {
		t.Logf("First result: %+v", result.Data.Result[0])
	}
}

// Teste de QueryRange (requer endpoint disponível)
func TestQueryRange_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := "akspriv-logreversa-prd-admin"
	client, err := NewPrometheusClient(cluster)
	if err != nil {
		t.Skip("Endpoint não disponível (VPN requerida)")
	}
	defer client.Close()

	ctx := context.Background()
	query := `kube_horizontalpodautoscaler_status_current_replicas{namespace="ingress-nginx",horizontalpodautoscaler="nginx-ingress-controller"}`

	end := time.Now()
	start := end.Add(-1 * time.Hour)
	step := 1 * time.Minute

	result, err := client.QueryRange(ctx, query, start, end, step)
	if err != nil {
		t.Fatalf("QueryRange failed: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("Expected status success, got %s", result.Status)
	}

	if len(result.Data.Result) == 0 {
		t.Error("Expected at least one result")
	}

	if len(result.Data.Result) > 0 && len(result.Data.Result[0].Values) == 0 {
		t.Error("Expected at least one data point")
	}

	t.Logf("QueryRange returned %d series", len(result.Data.Result))
	if len(result.Data.Result) > 0 {
		t.Logf("First series has %d data points", len(result.Data.Result[0].Values))
	}
}

// Teste de GetHPAMetrics (requer endpoint disponível)
func TestGetHPAMetrics_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := "akspriv-logreversa-prd-admin"
	client, err := NewPrometheusClient(cluster)
	if err != nil {
		t.Skip("Endpoint não disponível (VPN requerida)")
	}
	defer client.Close()

	ctx := context.Background()
	namespace := "ingress-nginx"
	hpaName := "nginx-ingress-controller"

	metrics, err := client.GetHPAMetrics(ctx, namespace, hpaName)
	if err != nil {
		t.Fatalf("GetHPAMetrics failed: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected at least one metric")
	}

	expectedMetrics := []string{"current_replicas", "min_replicas", "max_replicas", "desired_replicas"}
	for _, metric := range expectedMetrics {
		if _, exists := metrics[metric]; exists {
			t.Logf("%s: %v", metric, metrics[metric])
		}
	}

	// CPU e Memory podem não existir se pods não estão rodando
	if cpuUsage, exists := metrics["cpu_usage_percent"]; exists {
		t.Logf("cpu_usage_percent: %v", cpuUsage)
	}

	if memUsage, exists := metrics["memory_usage_percent"]; exists {
		t.Logf("memory_usage_percent: %v", memUsage)
	}
}

// Teste de GetHPAHistoricalMetrics (requer endpoint disponível)
func TestGetHPAHistoricalMetrics_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	cluster := "akspriv-logreversa-prd-admin"
	client, err := NewPrometheusClient(cluster)
	if err != nil {
		t.Skip("Endpoint não disponível (VPN requerida)")
	}
	defer client.Close()

	ctx := context.Background()
	namespace := "ingress-nginx"
	hpaName := "nginx-ingress-controller"
	duration := 6 * time.Hour
	step := 1 * time.Minute

	historical, err := client.GetHPAHistoricalMetrics(ctx, namespace, hpaName, duration, step)
	if err != nil {
		t.Fatalf("GetHPAHistoricalMetrics failed: %v", err)
	}

	if len(historical) == 0 {
		t.Error("Expected at least one historical metric")
	}

	for metricName, result := range historical {
		if len(result.Data.Result) > 0 {
			dataPoints := len(result.Data.Result[0].Values)
			t.Logf("%s: %d data points", metricName, dataPoints)
		}
	}
}
