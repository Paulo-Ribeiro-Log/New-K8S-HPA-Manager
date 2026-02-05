// Package metrics testa as métricas Prometheus do Health Checking
package metrics

import (
	"testing"
)

func TestGetMetrics_Singleton(t *testing.T) {
	// Primeira chamada
	m1 := GetMetrics()
	if m1 == nil {
		t.Fatal("GetMetrics() retornou nil")
	}

	// Segunda chamada deve retornar a mesma instância
	m2 := GetMetrics()
	if m1 != m2 {
		t.Error("GetMetrics() não retornou singleton - instâncias diferentes")
	}
}

func TestStatusValue(t *testing.T) {
	tests := []struct {
		status   string
		expected float64
	}{
		{"healthy", 0},
		{"warning", 1},
		{"critical", 2},
		{"unknown", -1},
		{"", -1},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusValue(tt.status)
			if got != tt.expected {
				t.Errorf("StatusValue(%q) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestMetricsInitialization(t *testing.T) {
	m := GetMetrics()

	// Verificar que todas as métricas estão inicializadas
	if m.Duration == nil {
		t.Error("Duration histogram não está inicializado")
	}
	if m.Status == nil {
		t.Error("Status gauge não está inicializado")
	}
	if m.IssuesTotal == nil {
		t.Error("IssuesTotal counter não está inicializado")
	}
	if m.ChecksTotal == nil {
		t.Error("ChecksTotal counter não está inicializado")
	}
	if m.LastRunTimestamp == nil {
		t.Error("LastRunTimestamp gauge não está inicializado")
	}
	if m.DeploymentsChecked == nil {
		t.Error("DeploymentsChecked gauge não está inicializado")
	}
	if m.ServicesChecked == nil {
		t.Error("ServicesChecked gauge não está inicializado")
	}
	if m.HPAsChecked == nil {
		t.Error("HPAsChecked gauge não está inicializado")
	}
	if m.PVCsChecked == nil {
		t.Error("PVCsChecked gauge não está inicializado")
	}
	if m.NodesChecked == nil {
		t.Error("NodesChecked gauge não está inicializado")
	}
	if m.EventsFound == nil {
		t.Error("EventsFound gauge não está inicializado")
	}
}

func TestRecordHealthCheck(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordHealthCheck("test-cluster", "healthy", 2.5)
	m.RecordHealthCheck("test-cluster", "warning", 1.5)
	m.RecordHealthCheck("test-cluster", "critical", 3.0)
}

func TestRecordDeployments(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordDeployments("test-cluster", 10, 3, 1)
}

func TestRecordServices(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordServices("test-cluster", 5, 2, 0)
}

func TestRecordHPAs(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordHPAs("test-cluster", 8, 1, 1)
}

func TestRecordPVCs(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordPVCs("test-cluster", 15, 0, 0)
}

func TestRecordNodes(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordNodes("test-cluster", 3, 1, 0)
}

func TestRecordEvents(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordEvents("test-cluster", 5, 2)
}

func TestRecordIssue(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordIssue("test-cluster", "critical", "deployment")
	m.RecordIssue("test-cluster", "warning", "service")
	m.RecordIssue("test-cluster", "info", "config")
}

func TestRecordIssues(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	severityCounts := map[string]int{
		"critical": 2,
		"warning":  3,
		"info":     5,
	}
	m.RecordIssues("test-cluster", "deployment", severityCounts)
}

func TestReset(t *testing.T) {
	m := GetMetrics()
	cluster := "reset-test-cluster"

	// Registrar valores
	m.RecordDeployments(cluster, 10, 5, 2)

	// Resetar - deve executar sem panic
	m.Reset(cluster)
}

func TestRecordCheckDuration(t *testing.T) {
	m := GetMetrics()

	// Deve executar sem panic
	m.RecordCheckDuration("test-cluster", "deployments", 1.5)
	m.RecordCheckDuration("test-cluster", "services", 0.8)
	m.RecordCheckDuration("test-cluster", "hpas", 0.3)
	m.RecordCheckDuration("test-cluster", "pvcs", 0.2)
	m.RecordCheckDuration("test-cluster", "nodes", 0.5)
	m.RecordCheckDuration("test-cluster", "events", 0.1)
}

func TestMultipleClusters(t *testing.T) {
	m := GetMetrics()

	// Registrar métricas para múltiplos clusters
	clusters := []string{"cluster-a", "cluster-b", "cluster-c"}

	for _, cluster := range clusters {
		m.RecordHealthCheck(cluster, "healthy", 1.0)
		m.RecordDeployments(cluster, 5, 1, 0)
		m.RecordServices(cluster, 3, 0, 0)
		m.RecordHPAs(cluster, 2, 1, 0)
		m.RecordPVCs(cluster, 10, 0, 0)
		m.RecordNodes(cluster, 3, 0, 0)
		m.RecordEvents(cluster, 1, 0)
	}
}

// Benchmark para verificar overhead das métricas
func BenchmarkRecordHealthCheck(b *testing.B) {
	m := GetMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordHealthCheck("benchmark-cluster", "healthy", 1.5)
	}
}

func BenchmarkRecordDeployments(b *testing.B) {
	m := GetMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordDeployments("benchmark-cluster", 100, 10, 5)
	}
}

func BenchmarkRecordIssues(b *testing.B) {
	m := GetMetrics()
	severityCounts := map[string]int{
		"critical": 2,
		"warning":  5,
		"info":     10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordIssues("benchmark-cluster", "deployment", severityCounts)
	}
}
