package prometheus

import (
	"fmt"
	"strings"
)

// QueryTemplate representa um template de query PromQL
type QueryTemplate struct {
	Name        string
	Description string
	Query       string
	Variables   []string
}

// Predefined PromQL queries para HPA monitoring
var (
	// CPU Queries

	CPUUsageQuery = QueryTemplate{
		Name:        "cpu_usage",
		Description: "CPU usage percentage relative to requests",
		Query: `
sum(rate(container_cpu_usage_seconds_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[1m])) /
sum(kube_pod_container_resource_requests{namespace="{{.namespace}}",pod=~"{{.pod_selector}}",resource="cpu"}) * 100
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	CPUUsageRawQuery = QueryTemplate{
		Name:        "cpu_usage_raw",
		Description: "Raw CPU usage in cores",
		Query: `
sum(rate(container_cpu_usage_seconds_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[1m]))
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	CPUThrottlingQuery = QueryTemplate{
		Name:        "cpu_throttling",
		Description: "CPU throttling percentage",
		Query: `
sum(rate(container_cpu_cfs_throttled_seconds_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[1m])) /
sum(rate(container_cpu_cfs_periods_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[1m])) * 100
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	// Memory Queries

	MemoryUsageQuery = QueryTemplate{
		Name:        "memory_usage",
		Description: "Memory usage percentage relative to requests",
		Query: `
sum(container_memory_working_set_bytes{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}) /
sum(kube_pod_container_resource_requests{namespace="{{.namespace}}",pod=~"{{.pod_selector}}",resource="memory"}) * 100
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	MemoryUsageRawQuery = QueryTemplate{
		Name:        "memory_usage_raw",
		Description: "Raw memory usage in bytes",
		Query: `
sum(container_memory_working_set_bytes{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"})
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	MemoryOOMQuery = QueryTemplate{
		Name:        "memory_oom",
		Description: "OOM kills count",
		Query: `
sum(increase(container_memory_failures_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}",type="oom"}[5m]))
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	// HPA Queries

	HPACurrentReplicasQuery = QueryTemplate{
		Name:        "hpa_current_replicas",
		Description: "Current number of replicas",
		Query: `
kube_horizontalpodautoscaler_status_current_replicas{namespace="{{.namespace}}",horizontalpodautoscaler="{{.hpa_name}}"}
`,
		Variables: []string{"namespace", "hpa_name"},
	}

	HPADesiredReplicasQuery = QueryTemplate{
		Name:        "hpa_desired_replicas",
		Description: "Desired number of replicas",
		Query: `
kube_horizontalpodautoscaler_status_desired_replicas{namespace="{{.namespace}}",horizontalpodautoscaler="{{.hpa_name}}"}
`,
		Variables: []string{"namespace", "hpa_name"},
	}

	HPAReplicaDeltaQuery = QueryTemplate{
		Name:        "hpa_replica_delta",
		Description: "Delta between desired and current replicas",
		Query: `
kube_horizontalpodautoscaler_status_desired_replicas{namespace="{{.namespace}}",horizontalpodautoscaler="{{.hpa_name}}"} -
kube_horizontalpodautoscaler_status_current_replicas{namespace="{{.namespace}}",horizontalpodautoscaler="{{.hpa_name}}"}
`,
		Variables: []string{"namespace", "hpa_name"},
	}

	// Network Queries

	NetworkRxBytesQuery = QueryTemplate{
		Name:        "network_rx_bytes",
		Description: "Network received bytes per second",
		Query: `
sum(rate(container_network_receive_bytes_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[1m]))
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	NetworkTxBytesQuery = QueryTemplate{
		Name:        "network_tx_bytes",
		Description: "Network transmitted bytes per second",
		Query: `
sum(rate(container_network_transmit_bytes_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[1m]))
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	// Application Metrics

	RequestRateQuery = QueryTemplate{
		Name:        "request_rate",
		Description: "HTTP requests per second",
		Query: `
sum(rate(http_requests_total{namespace="{{.namespace}}",service="{{.service}}"}[1m]))
`,
		Variables: []string{"namespace", "service"},
	}

	ErrorRateQuery = QueryTemplate{
		Name:        "error_rate",
		Description: "HTTP error rate percentage (5xx)",
		Query: `
sum(rate(http_requests_total{namespace="{{.namespace}}",service="{{.service}}",status=~"5.."}[1m])) /
sum(rate(http_requests_total{namespace="{{.namespace}}",service="{{.service}}"}[1m])) * 100
`,
		Variables: []string{"namespace", "service"},
	}

	P95LatencyQuery = QueryTemplate{
		Name:        "p95_latency",
		Description: "P95 latency in milliseconds",
		Query: `
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="{{.namespace}}",service="{{.service}}"}[5m])) by (le)) * 1000
`,
		Variables: []string{"namespace", "service"},
	}

	P99LatencyQuery = QueryTemplate{
		Name:        "p99_latency",
		Description: "P99 latency in milliseconds",
		Query: `
histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="{{.namespace}}",service="{{.service}}"}[5m])) by (le)) * 1000
`,
		Variables: []string{"namespace", "service"},
	}

	// Pod Queries

	PodRestartCountQuery = QueryTemplate{
		Name:        "pod_restart_count",
		Description: "Number of pod restarts in last 5 minutes",
		Query: `
sum(increase(kube_pod_container_status_restarts_total{namespace="{{.namespace}}",pod=~"{{.pod_selector}}"}[5m]))
`,
		Variables: []string{"namespace", "pod_selector"},
	}

	PodReadyCountQuery = QueryTemplate{
		Name:        "pod_ready_count",
		Description: "Number of ready pods",
		Query: `
sum(kube_pod_status_ready{namespace="{{.namespace}}",pod=~"{{.pod_selector}}",condition="true"})
`,
		Variables: []string{"namespace", "pod_selector"},
	}
)

// QueryBuilder constrói queries substituindo variáveis
type QueryBuilder struct {
	template QueryTemplate
	vars     map[string]string
}

// NewQueryBuilder cria um novo builder
func NewQueryBuilder(template QueryTemplate) *QueryBuilder {
	return &QueryBuilder{
		template: template,
		vars:     make(map[string]string),
	}
}

// WithNamespace define o namespace
func (qb *QueryBuilder) WithNamespace(namespace string) *QueryBuilder {
	qb.vars["namespace"] = namespace
	return qb
}

// WithHPAName define o nome do HPA
func (qb *QueryBuilder) WithHPAName(name string) *QueryBuilder {
	qb.vars["hpa_name"] = name
	qb.vars["pod_selector"] = name + ".*"
	return qb
}

// WithService define o service
func (qb *QueryBuilder) WithService(service string) *QueryBuilder {
	qb.vars["service"] = service
	return qb
}

// WithPodSelector define um pod selector customizado
func (qb *QueryBuilder) WithPodSelector(selector string) *QueryBuilder {
	qb.vars["pod_selector"] = selector
	return qb
}

// Build constrói a query final
func (qb *QueryBuilder) Build() (string, error) {
	query := strings.TrimSpace(qb.template.Query)

	// Substitui variáveis
	for key, value := range qb.vars {
		placeholder := fmt.Sprintf("{{.%s}}", key)
		query = strings.ReplaceAll(query, placeholder, value)
	}

	// Verifica se ainda há placeholders não substituídos
	if strings.Contains(query, "{{.") {
		return "", fmt.Errorf("query contains unsubstituted variables")
	}

	// Remove quebras de linha extras e espaços
	query = strings.Join(strings.Fields(query), " ")

	return query, nil
}

// Common helper functions

// BuildCPUQuery constrói query de CPU
func BuildCPUQuery(namespace, hpaName string) string {
	query, _ := NewQueryBuilder(CPUUsageQuery).
		WithNamespace(namespace).
		WithHPAName(hpaName).
		Build()
	return query
}

// BuildMemoryQuery constrói query de memória
func BuildMemoryQuery(namespace, hpaName string) string {
	query, _ := NewQueryBuilder(MemoryUsageQuery).
		WithNamespace(namespace).
		WithHPAName(hpaName).
		Build()
	return query
}

// BuildRequestRateQuery constrói query de request rate
func BuildRequestRateQuery(namespace, service string) string {
	query, _ := NewQueryBuilder(RequestRateQuery).
		WithNamespace(namespace).
		WithService(service).
		Build()
	return query
}

// BuildErrorRateQuery constrói query de error rate
func BuildErrorRateQuery(namespace, service string) string {
	query, _ := NewQueryBuilder(ErrorRateQuery).
		WithNamespace(namespace).
		WithService(service).
		Build()
	return query
}

// BuildP95LatencyQuery constrói query de P95 latency
func BuildP95LatencyQuery(namespace, service string) string {
	query, _ := NewQueryBuilder(P95LatencyQuery).
		WithNamespace(namespace).
		WithService(service).
		Build()
	return query
}

// GetAllTemplates retorna todos os templates disponíveis
func GetAllTemplates() []QueryTemplate {
	return []QueryTemplate{
		CPUUsageQuery,
		CPUUsageRawQuery,
		CPUThrottlingQuery,
		MemoryUsageQuery,
		MemoryUsageRawQuery,
		MemoryOOMQuery,
		HPACurrentReplicasQuery,
		HPADesiredReplicasQuery,
		HPAReplicaDeltaQuery,
		NetworkRxBytesQuery,
		NetworkTxBytesQuery,
		RequestRateQuery,
		ErrorRateQuery,
		P95LatencyQuery,
		P99LatencyQuery,
		PodRestartCountQuery,
		PodReadyCountQuery,
	}
}
