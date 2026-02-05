// Package metrics fornece métricas Prometheus para o Health Checking
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "k8s_hpa_manager"
	subsystem = "healthcheck"
)

// HealthCheckMetrics contém todas as métricas do health checking
type HealthCheckMetrics struct {
	// Duração dos health checks (histograma)
	Duration *prometheus.HistogramVec

	// Status do último health check (gauge: 0=healthy, 1=warning, 2=critical)
	Status *prometheus.GaugeVec

	// Total de issues por severidade (counter)
	IssuesTotal *prometheus.CounterVec

	// Total de checks executados (counter)
	ChecksTotal *prometheus.CounterVec

	// Timestamp do último health check (gauge)
	LastRunTimestamp *prometheus.GaugeVec

	// Total de deployments verificados (gauge)
	DeploymentsChecked *prometheus.GaugeVec

	// Total de services verificados (gauge)
	ServicesChecked *prometheus.GaugeVec

	// Total de HPAs verificados (gauge)
	HPAsChecked *prometheus.GaugeVec

	// Total de PVCs verificados (gauge)
	PVCsChecked *prometheus.GaugeVec

	// Total de nodes verificados (gauge)
	NodesChecked *prometheus.GaugeVec

	// Total de eventos K8s encontrados (gauge)
	EventsFound *prometheus.GaugeVec
}

var (
	// Singleton das métricas
	instance *HealthCheckMetrics
	once     sync.Once
)

// GetMetrics retorna a instância singleton das métricas
func GetMetrics() *HealthCheckMetrics {
	once.Do(func() {
		instance = newHealthCheckMetrics()
	})
	return instance
}

// newHealthCheckMetrics cria e registra todas as métricas
func newHealthCheckMetrics() *HealthCheckMetrics {
	return &HealthCheckMetrics{
		Duration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "duration_seconds",
				Help:      "Duração do health check em segundos",
				Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300},
			},
			[]string{"cluster", "check_type"},
		),

		Status: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "status",
				Help:      "Status do último health check (0=healthy, 1=warning, 2=critical)",
			},
			[]string{"cluster"},
		),

		IssuesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "issues_total",
				Help:      "Total de issues encontradas por severidade",
			},
			[]string{"cluster", "severity", "check_type"},
		),

		ChecksTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "checks_total",
				Help:      "Total de health checks executados",
			},
			[]string{"cluster", "status"},
		),

		LastRunTimestamp: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "last_run_timestamp_seconds",
				Help:      "Timestamp Unix do último health check",
			},
			[]string{"cluster"},
		),

		DeploymentsChecked: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "deployments_checked",
				Help:      "Total de deployments verificados no último check",
			},
			[]string{"cluster", "status"},
		),

		ServicesChecked: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "services_checked",
				Help:      "Total de services verificados no último check",
			},
			[]string{"cluster", "status"},
		),

		HPAsChecked: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "hpas_checked",
				Help:      "Total de HPAs verificados no último check",
			},
			[]string{"cluster", "status"},
		),

		PVCsChecked: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "pvcs_checked",
				Help:      "Total de PVCs verificados no último check",
			},
			[]string{"cluster", "status"},
		),

		NodesChecked: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "nodes_checked",
				Help:      "Total de nodes verificados no último check",
			},
			[]string{"cluster", "status"},
		),

		EventsFound: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "events_found",
				Help:      "Total de eventos K8s encontrados no último check",
			},
			[]string{"cluster", "severity"},
		),
	}
}

// StatusValue converte string de status para valor numérico
func StatusValue(status string) float64 {
	switch status {
	case "healthy":
		return 0
	case "warning":
		return 1
	case "critical":
		return 2
	default:
		return -1
	}
}

// RecordHealthCheck registra métricas de um health check completo
func (m *HealthCheckMetrics) RecordHealthCheck(cluster, status string, durationSeconds float64) {
	m.Duration.WithLabelValues(cluster, "full").Observe(durationSeconds)
	m.Status.WithLabelValues(cluster).Set(StatusValue(status))
	m.ChecksTotal.WithLabelValues(cluster, status).Inc()
	m.LastRunTimestamp.WithLabelValues(cluster).SetToCurrentTime()
}

// RecordCheckDuration registra a duração de um tipo específico de check
func (m *HealthCheckMetrics) RecordCheckDuration(cluster, checkType string, durationSeconds float64) {
	m.Duration.WithLabelValues(cluster, checkType).Observe(durationSeconds)
}

// RecordIssue registra uma issue encontrada
func (m *HealthCheckMetrics) RecordIssue(cluster, severity, checkType string) {
	m.IssuesTotal.WithLabelValues(cluster, severity, checkType).Inc()
}

// RecordIssues registra múltiplas issues de uma vez
func (m *HealthCheckMetrics) RecordIssues(cluster, checkType string, severityCounts map[string]int) {
	for severity, count := range severityCounts {
		for i := 0; i < count; i++ {
			m.IssuesTotal.WithLabelValues(cluster, severity, checkType).Inc()
		}
	}
}

// RecordDeployments registra contagem de deployments por status
func (m *HealthCheckMetrics) RecordDeployments(cluster string, healthy, warning, critical int) {
	m.DeploymentsChecked.WithLabelValues(cluster, "healthy").Set(float64(healthy))
	m.DeploymentsChecked.WithLabelValues(cluster, "warning").Set(float64(warning))
	m.DeploymentsChecked.WithLabelValues(cluster, "critical").Set(float64(critical))
}

// RecordServices registra contagem de services por status
func (m *HealthCheckMetrics) RecordServices(cluster string, healthy, warning, critical int) {
	m.ServicesChecked.WithLabelValues(cluster, "healthy").Set(float64(healthy))
	m.ServicesChecked.WithLabelValues(cluster, "warning").Set(float64(warning))
	m.ServicesChecked.WithLabelValues(cluster, "critical").Set(float64(critical))
}

// RecordHPAs registra contagem de HPAs por status
func (m *HealthCheckMetrics) RecordHPAs(cluster string, healthy, warning, critical int) {
	m.HPAsChecked.WithLabelValues(cluster, "healthy").Set(float64(healthy))
	m.HPAsChecked.WithLabelValues(cluster, "warning").Set(float64(warning))
	m.HPAsChecked.WithLabelValues(cluster, "critical").Set(float64(critical))
}

// RecordPVCs registra contagem de PVCs por status
func (m *HealthCheckMetrics) RecordPVCs(cluster string, healthy, warning, critical int) {
	m.PVCsChecked.WithLabelValues(cluster, "healthy").Set(float64(healthy))
	m.PVCsChecked.WithLabelValues(cluster, "warning").Set(float64(warning))
	m.PVCsChecked.WithLabelValues(cluster, "critical").Set(float64(critical))
}

// RecordNodes registra contagem de nodes por status
func (m *HealthCheckMetrics) RecordNodes(cluster string, healthy, warning, critical int) {
	m.NodesChecked.WithLabelValues(cluster, "healthy").Set(float64(healthy))
	m.NodesChecked.WithLabelValues(cluster, "warning").Set(float64(warning))
	m.NodesChecked.WithLabelValues(cluster, "critical").Set(float64(critical))
}

// RecordEvents registra contagem de eventos por severidade
func (m *HealthCheckMetrics) RecordEvents(cluster string, warning, critical int) {
	m.EventsFound.WithLabelValues(cluster, "warning").Set(float64(warning))
	m.EventsFound.WithLabelValues(cluster, "critical").Set(float64(critical))
}

// Reset limpa métricas de um cluster específico (útil antes de novo check)
func (m *HealthCheckMetrics) Reset(cluster string) {
	// Gauges podem ser resetados para 0
	m.Status.WithLabelValues(cluster).Set(-1)
	m.DeploymentsChecked.WithLabelValues(cluster, "healthy").Set(0)
	m.DeploymentsChecked.WithLabelValues(cluster, "warning").Set(0)
	m.DeploymentsChecked.WithLabelValues(cluster, "critical").Set(0)
	m.ServicesChecked.WithLabelValues(cluster, "healthy").Set(0)
	m.ServicesChecked.WithLabelValues(cluster, "warning").Set(0)
	m.ServicesChecked.WithLabelValues(cluster, "critical").Set(0)
	m.HPAsChecked.WithLabelValues(cluster, "healthy").Set(0)
	m.HPAsChecked.WithLabelValues(cluster, "warning").Set(0)
	m.HPAsChecked.WithLabelValues(cluster, "critical").Set(0)
	m.PVCsChecked.WithLabelValues(cluster, "healthy").Set(0)
	m.PVCsChecked.WithLabelValues(cluster, "warning").Set(0)
	m.PVCsChecked.WithLabelValues(cluster, "critical").Set(0)
	m.NodesChecked.WithLabelValues(cluster, "healthy").Set(0)
	m.NodesChecked.WithLabelValues(cluster, "warning").Set(0)
	m.NodesChecked.WithLabelValues(cluster, "critical").Set(0)
	m.EventsFound.WithLabelValues(cluster, "warning").Set(0)
	m.EventsFound.WithLabelValues(cluster, "critical").Set(0)
}
