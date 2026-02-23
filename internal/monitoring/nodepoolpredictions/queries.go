package nodepoolpredictions

import (
	"fmt"
	"strings"
	"time"
)

// NodePoolQueries contém todas as queries Prometheus para análise de node pool.
// As queries filtram nodes por instância (IP:porta do node_exporter) ou por nome
// de node (para métricas kube_*), permitindo isolar o pool analisado.
type NodePoolQueries struct{}

// NewNodePoolQueries cria nova instância
func NewNodePoolQueries() *NodePoolQueries {
	return &NodePoolQueries{}
}

// ---------------------------------------------------------------------------
// Helpers de filtro
// ---------------------------------------------------------------------------

// BuildInstanceRegex monta uma regex OR para filtrar node_exporter por IPs do pool.
// Entrada: []string{"10.0.0.1:9100", "10.0.0.2:9100"}
// Saída: "10\\.0\\.0\\.1:9100|10\\.0\\.0\\.2:9100"
//
// IMPORTANTE: PromQL string literals aceitam apenas \\ como backslash literal.
// "\." é sequência inválida em PromQL → usar "\\." para que o regex veja "\.".
func BuildInstanceRegex(instances []string) string {
	escaped := make([]string, len(instances))
	for i, inst := range instances {
		// Substituir "." por "\\." para que PromQL interprete como dot literal no regex.
		// Go string `"\\\\."` = valor `\\.`; PromQL processa `\\` → `\`, então regex vê `\.`.
		escaped[i] = strings.ReplaceAll(inst, ".", `\\.`)
	}
	return strings.Join(escaped, "|")
}

// BuildNodeNameRegex monta regex OR para filtrar métricas kube_* por nome de node.
// Entrada: []string{"aks-compute-12345-0", "aks-compute-12345-1"}
// Saída: "aks-compute-12345-0|aks-compute-12345-1"
func BuildNodeNameRegex(nodeNames []string) string {
	return strings.Join(nodeNames, "|")
}

// formatDuration formata duration para sintaxe Prometheus (ex: "3d", "7d", "14d")
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// ---------------------------------------------------------------------------
// CPU por node
// ---------------------------------------------------------------------------

// GetNodeCPUUsageQuery retorna query para uso de CPU (%) por node do pool.
// Resultado: vetor com um elemento por node (instance label = "<IP>:9100")
// Valor: 0.0 a 1.0 (multiplique por 100 para %)
func (q *NodePoolQueries) GetNodeCPUUsageQuery(instanceRegex string, offset time.Duration) string {
	filter := fmt.Sprintf(`instance=~"%s"`, instanceRegex)
	if offset == 0 {
		return fmt.Sprintf(
			`1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle",%s}[5m]))`,
			filter,
		)
	}
	return fmt.Sprintf(
		`1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle",%s}[5m] offset %s))`,
		filter, formatDuration(offset),
	)
}

// GetNodeCPUCoresUsedQuery retorna query para cores usados por node (valor absoluto)
func (q *NodePoolQueries) GetNodeCPUCoresUsedQuery(instanceRegex string, offset time.Duration) string {
	filter := fmt.Sprintf(`instance=~"%s"`, instanceRegex)
	if offset == 0 {
		return fmt.Sprintf(
			`sum by (instance) (rate(node_cpu_seconds_total{mode!="idle",%s}[5m]))`,
			filter,
		)
	}
	return fmt.Sprintf(
		`sum by (instance) (rate(node_cpu_seconds_total{mode!="idle",%s}[5m] offset %s))`,
		filter, formatDuration(offset),
	)
}

// GetNodeCPUCapacityQuery retorna query para capacidade total de CPU por node
func (q *NodePoolQueries) GetNodeCPUCapacityQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`kube_node_status_capacity{resource="cpu", node=~"%s"}`,
		nodeNameRegex,
	)
}

// ---------------------------------------------------------------------------
// Memória por node
// ---------------------------------------------------------------------------

// GetNodeMemUsagePercentQuery retorna query para uso de memória (%) por node
func (q *NodePoolQueries) GetNodeMemUsagePercentQuery(instanceRegex string, offset time.Duration) string {
	filter := fmt.Sprintf(`instance=~"%s"`, instanceRegex)
	if offset == 0 {
		return fmt.Sprintf(
			`1 - (node_memory_MemAvailable_bytes{%s} / node_memory_MemTotal_bytes{%s})`,
			filter, filter,
		)
	}
	return fmt.Sprintf(
		`1 - (node_memory_MemAvailable_bytes{%s} offset %s / node_memory_MemTotal_bytes{%s} offset %s)`,
		filter, formatDuration(offset), filter, formatDuration(offset),
	)
}

// GetNodeMemTotalQuery retorna query para memória total por node (bytes)
func (q *NodePoolQueries) GetNodeMemTotalQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_memory_MemTotal_bytes{instance=~"%s"}`,
		instanceRegex,
	)
}

// GetNodeMemAvailableQuery retorna query para memória disponível por node (bytes)
func (q *NodePoolQueries) GetNodeMemAvailableQuery(instanceRegex string, offset time.Duration) string {
	filter := fmt.Sprintf(`instance=~"%s"`, instanceRegex)
	if offset == 0 {
		return fmt.Sprintf(`node_memory_MemAvailable_bytes{%s}`, filter)
	}
	return fmt.Sprintf(
		`node_memory_MemAvailable_bytes{%s} offset %s`,
		filter, formatDuration(offset),
	)
}

// GetNodeMemCapacityQuery retorna query para capacidade de memória via kube_node (bytes)
func (q *NodePoolQueries) GetNodeMemCapacityQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`kube_node_status_capacity{resource="memory", node=~"%s"}`,
		nodeNameRegex,
	)
}

// ---------------------------------------------------------------------------
// conntrack por node — queries primárias (filtradas ao pool)
// ---------------------------------------------------------------------------

// GetConntrackEntriesQuery retorna entradas atuais de conntrack por node do pool
func (q *NodePoolQueries) GetConntrackEntriesQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_nf_conntrack_entries{instance=~"%s"}`,
		instanceRegex,
	)
}

// GetConntrackLimitQuery retorna limite máximo de conntrack por node do pool
func (q *NodePoolQueries) GetConntrackLimitQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_nf_conntrack_entries_limit{instance=~"%s"}`,
		instanceRegex,
	)
}

// GetConntrackUsagePercentQuery retorna % de uso de conntrack por node do pool
// Valor: 0.0 a 100.0
func (q *NodePoolQueries) GetConntrackUsagePercentQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_nf_conntrack_entries{instance=~"%s"} / node_nf_conntrack_entries_limit{instance=~"%s"} * 100`,
		instanceRegex, instanceRegex,
	)
}

// GetConntrackGrowthRateQuery retorna taxa de crescimento de conntrack (entradas/minuto).
// Usa deriv() ao invés de rate() porque node_nf_conntrack_entries é um GAUGE (sobe e desce
// quando conexões são abertas/fechadas). rate() pressupõe contadores monotônicos e interpreta
// quedas como "counter reset", gerando taxas absurdamente altas mesmo com uso baixo.
// deriv() usa regressão linear sobre a janela de 1h, dando resultado estável e correto.
func (q *NodePoolQueries) GetConntrackGrowthRateQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`deriv(node_nf_conntrack_entries{instance=~"%s"}[1h]) * 60`,
		instanceRegex,
	)
}

// GetConntrackEntriesWithOffsetQuery retorna entradas de conntrack em ponto no tempo (trend)
func (q *NodePoolQueries) GetConntrackEntriesWithOffsetQuery(instanceRegex string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(`node_nf_conntrack_entries{instance=~"%s"}`, instanceRegex)
	}
	return fmt.Sprintf(
		`node_nf_conntrack_entries{instance=~"%s"} offset %s`,
		instanceRegex, formatDuration(offset),
	)
}

// ---------------------------------------------------------------------------
// conntrack cluster-wide — queries de contexto (sem filtro de pool)
// ---------------------------------------------------------------------------

// GetConntrackEntriesClusterQuery retorna entradas de conntrack de todos os nodes do cluster
func (q *NodePoolQueries) GetConntrackEntriesClusterQuery() string {
	return `node_nf_conntrack_entries`
}

// GetConntrackLimitClusterQuery retorna limite de conntrack de todos os nodes do cluster
func (q *NodePoolQueries) GetConntrackLimitClusterQuery() string {
	return `node_nf_conntrack_entries_limit`
}

// ---------------------------------------------------------------------------
// Disk por node
// ---------------------------------------------------------------------------

// GetNodeDiskUsagePercentQuery retorna % de uso do disco raiz (/) por node
func (q *NodePoolQueries) GetNodeDiskUsagePercentQuery(instanceRegex string) string {
	filter := fmt.Sprintf(`instance=~"%s", mountpoint="/", fstype!="tmpfs"`, instanceRegex)
	return fmt.Sprintf(
		`1 - (node_filesystem_avail_bytes{%s} / node_filesystem_size_bytes{%s})`,
		filter, filter,
	)
}

// GetNodeDiskAvailableQuery retorna espaço disponível no disco raiz por node (bytes)
func (q *NodePoolQueries) GetNodeDiskAvailableQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_filesystem_avail_bytes{instance=~"%s", mountpoint="/", fstype!="tmpfs"}`,
		instanceRegex,
	)
}

// GetNodeDiskReadRateQuery retorna taxa de leitura do disco por node (bytes/s)
func (q *NodePoolQueries) GetNodeDiskReadRateQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`sum by (instance) (rate(node_disk_read_bytes_total{instance=~"%s", device!~"loop.*"}[5m]))`,
		instanceRegex,
	)
}

// GetNodeDiskWriteRateQuery retorna taxa de escrita do disco por node (bytes/s)
func (q *NodePoolQueries) GetNodeDiskWriteRateQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`sum by (instance) (rate(node_disk_written_bytes_total{instance=~"%s", device!~"loop.*"}[5m]))`,
		instanceRegex,
	)
}

// ---------------------------------------------------------------------------
// Pod density por node
// ---------------------------------------------------------------------------

// GetPodCountPerNodeQuery retorna contagem atual de pods por node do pool
// (via kube_pod_info — filtra por nome do node)
func (q *NodePoolQueries) GetPodCountPerNodeQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`count by (node) (kube_pod_info{node=~"%s"})`,
		nodeNameRegex,
	)
}

// GetPodCountPerNodeWithOffsetQuery retorna contagem de pods por node com offset (trend)
func (q *NodePoolQueries) GetPodCountPerNodeWithOffsetQuery(nodeNameRegex string, offset time.Duration) string {
	if offset == 0 {
		return fmt.Sprintf(
			`count by (node) (kube_pod_info{node=~"%s"})`,
			nodeNameRegex,
		)
	}
	return fmt.Sprintf(
		`count by (node) (kube_pod_info{node=~"%s"} offset %s)`,
		nodeNameRegex, formatDuration(offset),
	)
}

// GetPodCapacityPerNodeQuery retorna capacidade máxima de pods por node (max-pods-per-node)
func (q *NodePoolQueries) GetPodCapacityPerNodeQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`kube_node_status_capacity{resource="pods", node=~"%s"}`,
		nodeNameRegex,
	)
}

// GetPodAllocatablePerNodeQuery retorna capacidade alocável de pods por node
func (q *NodePoolQueries) GetPodAllocatablePerNodeQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`kube_node_status_allocatable{resource="pods", node=~"%s"}`,
		nodeNameRegex,
	)
}

// ---------------------------------------------------------------------------
// PID por node
// ---------------------------------------------------------------------------

// GetNodePIDCountQuery retorna contagem de processos (PIDs) por node
func (q *NodePoolQueries) GetNodePIDCountQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_processes_total{instance=~"%s"}`,
		instanceRegex,
	)
}

// GetNodePIDLimitQuery retorna limite de PIDs por node (do kernel)
func (q *NodePoolQueries) GetNodePIDLimitQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`node_processes_max{instance=~"%s"}`,
		instanceRegex,
	)
}

// ---------------------------------------------------------------------------
// Rede por node
// ---------------------------------------------------------------------------

// GetNodeNetworkRxRateQuery retorna taxa de recebimento de rede por node (bytes/s)
func (q *NodePoolQueries) GetNodeNetworkRxRateQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`sum by (instance) (rate(node_network_receive_bytes_total{instance=~"%s", device!~"lo|veth.*|docker.*|flannel.*|cni.*"}[5m]))`,
		instanceRegex,
	)
}

// GetNodeNetworkTxRateQuery retorna taxa de envio de rede por node (bytes/s)
func (q *NodePoolQueries) GetNodeNetworkTxRateQuery(instanceRegex string) string {
	return fmt.Sprintf(
		`sum by (instance) (rate(node_network_transmit_bytes_total{instance=~"%s", device!~"lo|veth.*|docker.*|flannel.*|cni.*"}[5m]))`,
		instanceRegex,
	)
}

// GetNodeNetworkRxWithOffsetQuery retorna rx rate com offset para trends
func (q *NodePoolQueries) GetNodeNetworkRxWithOffsetQuery(instanceRegex string, offset time.Duration) string {
	if offset == 0 {
		return q.GetNodeNetworkRxRateQuery(instanceRegex)
	}
	return fmt.Sprintf(
		`sum by (instance) (rate(node_network_receive_bytes_total{instance=~"%s", device!~"lo|veth.*|docker.*|flannel.*|cni.*"}[5m] offset %s))`,
		instanceRegex, formatDuration(offset),
	)
}

// ---------------------------------------------------------------------------
// Node info / metadata
// ---------------------------------------------------------------------------

// GetNodeInfoQuery retorna informações dos nodes (nome, IP interno, labels)
// Usado para mapear node names → IPs do node_exporter
func (q *NodePoolQueries) GetNodeInfoQuery() string {
	return `kube_node_info`
}

// GetNodeConditionsQuery retorna conditions dos nodes do pool
// Usado para detectar MemoryPressure, DiskPressure, PIDPressure, etc.
func (q *NodePoolQueries) GetNodeConditionsQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`kube_node_status_condition{node=~"%s", status="true"}`,
		nodeNameRegex,
	)
}

// GetNodeLabelsQuery retorna labels dos nodes (útil para obter agentpool label)
func (q *NodePoolQueries) GetNodeLabelsQuery() string {
	return `kube_node_labels`
}

// ---------------------------------------------------------------------------
// Cluster Autoscaler (events)
// ---------------------------------------------------------------------------

// GetAutoscalerScaleUpEventsQuery retorna contagem de eventos de scale-up do autoscaler
func (q *NodePoolQueries) GetAutoscalerScaleUpEventsQuery() string {
	return `kube_event_count_total{reason="TriggeredScaleUp", involvedObject_kind="Node"}`
}

// GetAutoscalerScaleDownEventsQuery retorna contagem de eventos de scale-down
func (q *NodePoolQueries) GetAutoscalerScaleDownEventsQuery() string {
	return `kube_event_count_total{reason="ScaleDown", involvedObject_kind="Node"}`
}

// ---------------------------------------------------------------------------
// Queries de capacidade / allocatable do cluster (contexto geral)
// ---------------------------------------------------------------------------

// GetClusterCPUAllocatableQuery retorna CPU alocável total do cluster
func (q *NodePoolQueries) GetClusterCPUAllocatableQuery() string {
	return `sum(kube_node_status_allocatable{resource="cpu"})`
}

// GetClusterMemAllocatableQuery retorna memória alocável total do cluster (bytes)
func (q *NodePoolQueries) GetClusterMemAllocatableQuery() string {
	return `sum(kube_node_status_allocatable{resource="memory"})`
}

// GetPoolCPUAllocatableQuery retorna CPU alocável dos nodes do pool
func (q *NodePoolQueries) GetPoolCPUAllocatableQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`sum(kube_node_status_allocatable{resource="cpu", node=~"%s"})`,
		nodeNameRegex,
	)
}

// GetPoolMemAllocatableQuery retorna memória alocável dos nodes do pool (bytes)
func (q *NodePoolQueries) GetPoolMemAllocatableQuery(nodeNameRegex string) string {
	return fmt.Sprintf(
		`sum(kube_node_status_allocatable{resource="memory", node=~"%s"})`,
		nodeNameRegex,
	)
}

// ---------------------------------------------------------------------------
// Trends — offsets pré-definidos
// ---------------------------------------------------------------------------

// DayOffsets retorna os offsets para coleta de snapshots históricos (D-3, D-7, D-14)
func DayOffsets() map[string]time.Duration {
	return map[string]time.Duration{
		"D-3":  3 * 24 * time.Hour,
		"D-7":  7 * 24 * time.Hour,
		"D-14": 14 * 24 * time.Hour,
	}
}

// ConntrackStatusFromPercent classifica o status do conntrack baseado no percentual
// de uso, seguindo os thresholds definidos no estudo:
// ok (<70%), warning (70-85%), critical (85-95%), emergency (>95%)
func ConntrackStatusFromPercent(percent float64) string {
	switch {
	case percent >= 95:
		return "emergency"
	case percent >= 85:
		return "critical"
	case percent >= 70:
		return "warning"
	default:
		return "ok"
	}
}
