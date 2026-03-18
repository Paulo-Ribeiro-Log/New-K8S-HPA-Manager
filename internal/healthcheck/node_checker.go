package healthcheck

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeHealth representa a saúde de um Node Kubernetes
type NodeHealth struct {
	Name   string       `json:"name"`
	Status HealthStatus `json:"status"`

	// Condições do Node
	Ready          bool `json:"ready"`
	MemoryPressure bool `json:"memory_pressure"`
	DiskPressure   bool `json:"disk_pressure"`
	PIDPressure    bool `json:"pid_pressure"`
	NetworkUnavail bool `json:"network_unavailable"`

	// Capacidade e Alocação
	Capacity    NodeResources `json:"capacity"`
	Allocatable NodeResources `json:"allocatable"`
	Allocated   NodeResources `json:"allocated"` // Recursos já alocados por pods

	// Utilização (percentual)
	CPUUtilization    float64 `json:"cpu_utilization_percent"`
	MemoryUtilization float64 `json:"memory_utilization_percent"`
	PodUtilization    float64 `json:"pod_utilization_percent"`

	// Informações do Node
	KubeletVersion string `json:"kubelet_version"`
	OSImage        string `json:"os_image"`
	Architecture   string `json:"architecture"`

	// Pods afetados (se node com problemas)
	AffectedPods []AffectedPod `json:"affected_pods,omitempty"`

	// Mensagem e Sugestões
	Message     string    `json:"message"`
	Suggestions []string  `json:"suggestions"`
	CheckedAt   time.Time `json:"checked_at"`
}

// NodeResources representa recursos de um node
type NodeResources struct {
	CPUMillis   int64 `json:"cpu_millis"`   // CPU em millicores
	MemoryBytes int64 `json:"memory_bytes"` // Memória em bytes
	Pods        int64 `json:"pods"`         // Número de pods
	EphemeralGB int64 `json:"ephemeral_gb"` // Storage efêmero em GB
}

// AffectedPod representa um pod afetado por problemas no node
type AffectedPod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
}

// NodeChecker verifica a saúde dos nodes do cluster
type NodeChecker struct{}

// NewNodeChecker cria um novo NodeChecker
func NewNodeChecker() *NodeChecker {
	return &NodeChecker{}
}

// CheckAll verifica todos os nodes do cluster
func (nc *NodeChecker) CheckAll(ctx context.Context, client kubernetes.Interface, timeout int) []NodeHealth {
	results := []NodeHealth{}

	// Listar todos os nodes
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Falha ao listar nodes")
		return results
	}

	// Listar todos os pods para calcular alocação
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Warn().Err(err).Msg("Falha ao listar pods para cálculo de alocação")
		pods = &corev1.PodList{} // Continua sem pods
	}

	// Criar mapa de pods por node
	podsByNode := make(map[string][]corev1.Pod)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" {
			podsByNode[pod.Spec.NodeName] = append(podsByNode[pod.Spec.NodeName], pod)
		}
	}

	for _, node := range nodes.Items {
		health := nc.checkNode(&node, podsByNode[node.Name])
		results = append(results, health)
	}

	return results
}

// checkNode verifica a saúde de um node específico
func (nc *NodeChecker) checkNode(node *corev1.Node, nodePods []corev1.Pod) NodeHealth {
	health := NodeHealth{
		Name:        node.Name,
		Suggestions: []string{},
		CheckedAt:   time.Now(),
	}

	// Extrair informações do node
	health.KubeletVersion = node.Status.NodeInfo.KubeletVersion
	health.OSImage = node.Status.NodeInfo.OSImage
	health.Architecture = node.Status.NodeInfo.Architecture

	// Verificar condições do node
	for _, condition := range node.Status.Conditions {
		switch condition.Type {
		case corev1.NodeReady:
			health.Ready = condition.Status == corev1.ConditionTrue
		case corev1.NodeMemoryPressure:
			health.MemoryPressure = condition.Status == corev1.ConditionTrue
		case corev1.NodeDiskPressure:
			health.DiskPressure = condition.Status == corev1.ConditionTrue
		case corev1.NodePIDPressure:
			health.PIDPressure = condition.Status == corev1.ConditionTrue
		case corev1.NodeNetworkUnavailable:
			health.NetworkUnavail = condition.Status == corev1.ConditionTrue
		}
	}

	// Extrair capacidade e allocatable
	health.Capacity = nc.extractResources(node.Status.Capacity)
	health.Allocatable = nc.extractResources(node.Status.Allocatable)

	// Calcular recursos alocados pelos pods
	health.Allocated = nc.calculateAllocated(nodePods)

	// Calcular utilização percentual
	if health.Allocatable.CPUMillis > 0 {
		health.CPUUtilization = float64(health.Allocated.CPUMillis) / float64(health.Allocatable.CPUMillis) * 100
	}
	if health.Allocatable.MemoryBytes > 0 {
		health.MemoryUtilization = float64(health.Allocated.MemoryBytes) / float64(health.Allocatable.MemoryBytes) * 100
	}
	if health.Allocatable.Pods > 0 {
		health.PodUtilization = float64(len(nodePods)) / float64(health.Allocatable.Pods) * 100
	}

	// Determinar status e mensagem
	nc.determineStatus(&health, nodePods)

	return health
}

// extractResources extrai recursos de um ResourceList
func (nc *NodeChecker) extractResources(resources corev1.ResourceList) NodeResources {
	nr := NodeResources{}

	if cpu, ok := resources[corev1.ResourceCPU]; ok {
		nr.CPUMillis = cpu.MilliValue()
	}
	if mem, ok := resources[corev1.ResourceMemory]; ok {
		nr.MemoryBytes = mem.Value()
	}
	if pods, ok := resources[corev1.ResourcePods]; ok {
		nr.Pods = pods.Value()
	}
	if ephemeral, ok := resources[corev1.ResourceEphemeralStorage]; ok {
		nr.EphemeralGB = ephemeral.Value() / (1024 * 1024 * 1024) // Converter para GB
	}

	return nr
}

// calculateAllocated calcula recursos alocados pelos pods no node
func (nc *NodeChecker) calculateAllocated(pods []corev1.Pod) NodeResources {
	allocated := NodeResources{
		Pods: int64(len(pods)),
	}

	for _, pod := range pods {
		// Ignorar pods em fase terminal
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		for _, container := range pod.Spec.Containers {
			if container.Resources.Requests != nil {
				if cpu := container.Resources.Requests.Cpu(); cpu != nil {
					allocated.CPUMillis += cpu.MilliValue()
				}
				if mem := container.Resources.Requests.Memory(); mem != nil {
					allocated.MemoryBytes += mem.Value()
				}
			}
		}
	}

	return allocated
}

// determineStatus determina o status do node baseado nas condições
func (nc *NodeChecker) determineStatus(health *NodeHealth, pods []corev1.Pod) {
	// Node não está Ready - Critical
	if !health.Ready {
		health.Status = StatusCritical
		health.Message = "Node não está Ready"
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe node %s", health.Name))
		health.Suggestions = append(health.Suggestions, "Verificar logs do kubelet no node")

		// Listar pods afetados
		health.AffectedPods = nc.getAffectedPods(pods)
		return
	}

	// Network Unavailable - Critical
	if health.NetworkUnavail {
		health.Status = StatusCritical
		health.Message = "Rede do node indisponível"
		health.Suggestions = append(health.Suggestions, "Verificar CNI plugin (Calico, Flannel, etc)")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe node %s", health.Name))
		health.AffectedPods = nc.getAffectedPods(pods)
		return
	}

	// Memory Pressure - Critical
	if health.MemoryPressure {
		health.Status = StatusCritical
		health.Message = "Node com pressão de memória"
		health.Suggestions = append(health.Suggestions, "Pods podem ser evicted a qualquer momento")
		health.Suggestions = append(health.Suggestions, "Verificar pods com alto consumo de memória")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl top pods --all-namespaces --sort-by=memory | grep %s", health.Name))
		health.AffectedPods = nc.getAffectedPods(pods)
		return
	}

	// Disk Pressure - Critical
	if health.DiskPressure {
		health.Status = StatusCritical
		health.Message = "Node com pressão de disco"
		health.Suggestions = append(health.Suggestions, "Limpar imagens não utilizadas: docker system prune")
		health.Suggestions = append(health.Suggestions, "Verificar logs e arquivos temporários no node")
		health.AffectedPods = nc.getAffectedPods(pods)
		return
	}

	// PID Pressure - Warning
	if health.PIDPressure {
		health.Status = StatusWarning
		health.Message = "Node com pressão de PIDs"
		health.Suggestions = append(health.Suggestions, "Verificar processos no node")
		health.Suggestions = append(health.Suggestions, "Pode indicar vazamento de processos em containers")
		return
	}

	// Alta utilização de CPU (>90%) - Warning
	if health.CPUUtilization > 90 {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Alta alocação de CPU: %.1f%%", health.CPUUtilization)
		health.Suggestions = append(health.Suggestions, "Considerar adicionar mais nodes ao cluster")
		health.Suggestions = append(health.Suggestions, "Verificar se HPAs estão configurados corretamente")
		return
	}

	// Alta utilização de memória (>90%) - Warning
	if health.MemoryUtilization > 90 {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Alta alocação de memória: %.1f%%", health.MemoryUtilization)
		health.Suggestions = append(health.Suggestions, "Considerar adicionar mais nodes ao cluster")
		health.Suggestions = append(health.Suggestions, "Revisar requests de memória dos deployments")
		return
	}

	// Alta utilização de pods (>90%) - Warning
	if health.PodUtilization > 90 {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Alta utilização de pods: %.1f%% (%d/%d)", health.PodUtilization, len(pods), health.Allocatable.Pods)
		health.Suggestions = append(health.Suggestions, "Node próximo do limite de pods")
		health.Suggestions = append(health.Suggestions, "Considerar aumentar --max-pods no kubelet ou adicionar nodes")
		return
	}

	// Node saudável
	health.Status = StatusHealthy
	health.Message = fmt.Sprintf("Node saudável (CPU: %.1f%%, Mem: %.1f%%, Pods: %d/%d)",
		health.CPUUtilization,
		health.MemoryUtilization,
		len(pods),
		health.Allocatable.Pods,
	)
}

// getAffectedPods retorna lista de pods afetados por problemas no node
func (nc *NodeChecker) getAffectedPods(pods []corev1.Pod) []AffectedPod {
	affected := []AffectedPod{}

	for _, pod := range pods {
		// Ignorar pods em fase terminal
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		affected = append(affected, AffectedPod{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Status:    string(pod.Status.Phase),
		})

		// Limitar a 10 pods para não sobrecarregar a UI
		if len(affected) >= 10 {
			break
		}
	}

	return affected
}

// GetCriticalCount retorna o número de nodes com problemas críticos
func GetNodeCriticalCount(nodes []NodeHealth) int {
	count := 0
	for _, node := range nodes {
		if node.Status == StatusCritical {
			count++
		}
	}
	return count
}

// GetWarningCount retorna o número de nodes com warnings
func GetNodeWarningCount(nodes []NodeHealth) int {
	count := 0
	for _, node := range nodes {
		if node.Status == StatusWarning {
			count++
		}
	}
	return count
}
