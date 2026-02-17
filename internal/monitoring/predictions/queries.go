package predictions

import (
	"fmt"
	"time"
)

// PrometheusQueries contém todas as queries necessárias para análise preditiva
type PrometheusQueries struct{}

// NewPrometheusQueries cria nova instância
func NewPrometheusQueries() *PrometheusQueries {
	return &PrometheusQueries{}
}

// GetCPUUsageQuery retorna query para uso de CPU
func (q *PrometheusQueries) GetCPUUsageQuery(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[5m]))`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[5m] offset %s))`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetCPUUsageP95Query retorna query para percentil 95 de CPU
func (q *PrometheusQueries) GetCPUUsageP95Query(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`quantile(0.95, rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[5m]))`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`quantile(0.95, rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[5m] offset %s))`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetMemoryUsageQuery retorna query para uso de memória
func (q *PrometheusQueries) GetMemoryUsageQuery(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!="",container!="POD"})`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!="",container!="POD"} offset %s)`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetMemoryUsageP95Query retorna query para percentil 95 de memória
func (q *PrometheusQueries) GetMemoryUsageP95Query(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`quantile(0.95, container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!="",container!="POD"})`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`quantile(0.95, container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!="",container!="POD"} offset %s)`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetNetworkRxQuery retorna query para tráfego de rede recebido
func (q *PrometheusQueries) GetNetworkRxQuery(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`sum(rate(container_network_receive_bytes_total{namespace="%s",pod=~"%s-.*"}[5m])) by (pod)`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`sum(rate(container_network_receive_bytes_total{namespace="%s",pod=~"%s-.*"}[5m] offset %s)) by (pod)`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetNetworkTxQuery retorna query para tráfego de rede enviado
func (q *PrometheusQueries) GetNetworkTxQuery(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`sum(rate(container_network_transmit_bytes_total{namespace="%s",pod=~"%s-.*"}[5m])) by (pod)`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`sum(rate(container_network_transmit_bytes_total{namespace="%s",pod=~"%s-.*"}[5m] offset %s)) by (pod)`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetRestartCountQuery retorna query para contagem de restarts
func (q *PrometheusQueries) GetRestartCountQuery(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`sum(kube_pod_container_status_restarts_total{namespace="%s",pod=~"%s-.*"})`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`sum(kube_pod_container_status_restarts_total{namespace="%s",pod=~"%s-.*"} offset %s)`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetErrorRateQuery retorna query para taxa de erro
func (q *PrometheusQueries) GetErrorRateQuery(namespace, deployment string, offset time.Duration) string {
	// Assume que há métricas de erro HTTP 5xx
	if offset == 0 {
		return fmt.Sprintf(
			`sum(rate(http_requests_total{namespace="%s",pod=~"%s-.*",status=~"5.."}[5m])) / sum(rate(http_requests_total{namespace="%s",pod=~"%s-.*"}[5m])) * 100`,
			namespace, deployment, namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`sum(rate(http_requests_total{namespace="%s",pod=~"%s-.*",status=~"5.."}[5m] offset %s)) / sum(rate(http_requests_total{namespace="%s",pod=~"%s-.*"}[5m] offset %s)) * 100`,
		namespace, deployment, formatDuration(offset), namespace, deployment, formatDuration(offset),
	)
}

// GetLatencyP50Query retorna query para latência P50
func (q *PrometheusQueries) GetLatencyP50Query(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`histogram_quantile(0.50, sum(rate(http_request_duration_seconds_bucket{namespace="%s",pod=~"%s-.*"}[5m])) by (le)) * 1000`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`histogram_quantile(0.50, sum(rate(http_request_duration_seconds_bucket{namespace="%s",pod=~"%s-.*"}[5m] offset %s)) by (le)) * 1000`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetLatencyP95Query retorna query para latência P95
func (q *PrometheusQueries) GetLatencyP95Query(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="%s",pod=~"%s-.*"}[5m])) by (le)) * 1000`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace="%s",pod=~"%s-.*"}[5m] offset %s)) by (le)) * 1000`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetLatencyP99Query retorna query para latência P99
func (q *PrometheusQueries) GetLatencyP99Query(namespace, deployment string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="%s",pod=~"%s-.*"}[5m])) by (le)) * 1000`,
			namespace, deployment,
		)
	}
	return fmt.Sprintf(
		`histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="%s",pod=~"%s-.*"}[5m] offset %s)) by (le)) * 1000`,
		namespace, deployment, formatDuration(offset),
	)
}

// GetRPSQuery retorna query para requests por segundo (http_requests_total)
func (q *PrometheusQueries) GetRPSQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`sum(rate(http_requests_total{namespace="%s",pod=~"%s-.*"}[5m]))`,
		namespace, deployment,
	)
}

// GetUptimeQuery retorna query para uptime % dos últimos 30 dias
// Calcula a fração de tempo em que replicas_available > 0
func (q *PrometheusQueries) GetUptimeQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`avg_over_time(clamp_max(kube_deployment_status_replicas_available{namespace="%s",deployment="%s"}, 1)[30d:5m]) * 100`,
		namespace, deployment,
	)
}

// GetCurrentAvailabilityQuery retorna query para disponibilidade atual (fallback de uptime)
func (q *PrometheusQueries) GetCurrentAvailabilityQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`kube_deployment_status_replicas_available{namespace="%s",deployment="%s"} / kube_deployment_spec_replicas{namespace="%s",deployment="%s"} * 100`,
		namespace, deployment, namespace, deployment,
	)
}

// GetNodeCPUCapacityQuery retorna query para capacidade de CPU dos nodes
func (q *PrometheusQueries) GetNodeCPUCapacityQuery() string {
	return `sum(kube_node_status_capacity{resource="cpu"}) by (node)`
}

// GetNodeCPUAllocatableQuery retorna query para CPU alocável dos nodes
func (q *PrometheusQueries) GetNodeCPUAllocatableQuery() string {
	return `sum(kube_node_status_allocatable{resource="cpu"}) by (node)`
}

// GetNodeMemoryCapacityQuery retorna query para capacidade de memória dos nodes
func (q *PrometheusQueries) GetNodeMemoryCapacityQuery() string {
	return `sum(kube_node_status_capacity{resource="memory"}) by (node)`
}

// GetNodeMemoryAllocatableQuery retorna query para memória alocável dos nodes
func (q *PrometheusQueries) GetNodeMemoryAllocatableQuery() string {
	return `sum(kube_node_status_allocatable{resource="memory"}) by (node)`
}

// GetNodeCPUUsageQuery retorna query para uso de CPU dos nodes
func (q *PrometheusQueries) GetNodeCPUUsageQuery() string {
	return `sum(rate(node_cpu_seconds_total{mode!="idle"}[5m])) by (instance)`
}

// GetNodeMemoryUsageQuery retorna query para uso de memória dos nodes
func (q *PrometheusQueries) GetNodeMemoryUsageQuery() string {
	return `sum(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) by (instance)`
}

// GetPodsPerNodeQuery retorna query para contagem de pods por node
func (q *PrometheusQueries) GetPodsPerNodeQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`count(kube_pod_info{namespace="%s",pod=~"%s-.*"}) by (node)`,
		namespace, deployment,
	)
}

// GetAllPodsPerNodeQuery retorna query para total de pods por node
func (q *PrometheusQueries) GetAllPodsPerNodeQuery() string {
	return `count(kube_pod_info) by (node)`
}

// GetCompetingAppsQuery retorna query para aplicações concorrentes
func (q *PrometheusQueries) GetCompetingAppsQuery(namespace string, limit int) string {
	return fmt.Sprintf(
		`topk(%d, sum(rate(container_cpu_usage_seconds_total{namespace="%s"}[5m])) by (pod))`,
		limit, namespace,
	)
}

// GetClusterTotalCPUQuery retorna query para CPU total do cluster
func (q *PrometheusQueries) GetClusterTotalCPUQuery() string {
	// Tentar formato v2.x primeiro, depois v1.x como fallback
	return `sum(kube_node_status_capacity_cpu_cores) or sum(kube_node_status_capacity{resource="cpu"})`
}

// GetClusterTotalMemoryQuery retorna query para memória total do cluster
func (q *PrometheusQueries) GetClusterTotalMemoryQuery() string {
	// Tentar formato v2.x primeiro, depois v1.x como fallback
	return `sum(kube_node_status_capacity_memory_bytes) or sum(kube_node_status_capacity{resource="memory"})`
}

// GetClusterAllocatedCPUQuery retorna query para CPU alocada no cluster
func (q *PrometheusQueries) GetClusterAllocatedCPUQuery() string {
	// Tentar formato v2.x primeiro, depois v1.x como fallback
	return `sum(kube_pod_container_resource_requests_cpu_cores) or sum(kube_pod_container_resource_requests{resource="cpu"})`
}

// GetClusterAllocatedMemoryQuery retorna query para memória alocada no cluster
func (q *PrometheusQueries) GetClusterAllocatedMemoryQuery() string {
	// Tentar formato v2.x primeiro, depois v1.x como fallback
	return `sum(kube_pod_container_resource_requests_memory_bytes) or sum(kube_pod_container_resource_requests{resource="memory"})`
}

// GetReplicaCountQuery retorna query para contagem de réplicas
func (q *PrometheusQueries) GetReplicaCountQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`kube_deployment_status_replicas{namespace="%s",deployment="%s"}`,
		namespace, deployment,
	)
}

// GetAvailableReplicasQuery retorna query para réplicas disponíveis
func (q *PrometheusQueries) GetAvailableReplicasQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`kube_deployment_status_replicas_available{namespace="%s",deployment="%s"}`,
		namespace, deployment,
	)
}

// GetReadyReplicasQuery retorna query para réplicas prontas
func (q *PrometheusQueries) GetReadyReplicasQuery(namespace, deployment string) string {
	return fmt.Sprintf(
		`kube_deployment_status_replicas_ready{namespace="%s",deployment="%s"}`,
		namespace, deployment,
	)
}

// GetConntrackEntriesQuery retorna query para entradas atuais de conntrack por node
func (q *PrometheusQueries) GetConntrackEntriesQuery() string {
	return `node_nf_conntrack_entries`
}

// GetConntrackLimitQuery retorna query para limite máximo de conntrack por node
func (q *PrometheusQueries) GetConntrackLimitQuery() string {
	return `node_nf_conntrack_entries_limit`
}

// GetConntrackUsageRatioQuery retorna query de uso % de conntrack por node
func (q *PrometheusQueries) GetConntrackUsageRatioQuery() string {
	return `node_nf_conntrack_entries / node_nf_conntrack_entries_limit * 100`
}

// formatDuration formata duration para formato Prometheus (7d, 30d, etc)
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	minutes := int(d.Minutes())
	return fmt.Sprintf("%dm", minutes)
}
