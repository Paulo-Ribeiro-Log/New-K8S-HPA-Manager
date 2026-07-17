package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EventHealth representa um evento Kubernetes relevante para health checking
type EventHealth struct {
	Name           string    `json:"name"`
	Namespace      string    `json:"namespace"`
	Reason         string    `json:"reason"`          // FailedScheduling, BackOff, etc.
	Message        string    `json:"message"`         // Mensagem completa do evento
	Type           string    `json:"type"`            // Warning, Normal
	Severity       Severity  `json:"severity"`        // critical, high, medium, low, info
	Count          int32     `json:"count"`           // Número de ocorrências
	FirstTimestamp time.Time `json:"first_timestamp"` // Primeira ocorrência
	LastTimestamp  time.Time `json:"last_timestamp"`  // Última ocorrência
	// Recurso relacionado
	InvolvedKind string `json:"involved_kind"` // Pod, Node, Deployment, etc.
	InvolvedName string `json:"involved_name"` // Nome do recurso
	// Sugestões de correção
	Suggestions []string     `json:"suggestions"`
	Status      HealthStatus `json:"status"` // Para compatibilidade com outros checkers
}

// EventChronicity classifica um evento K8s como crônico ou agudo, a partir do histórico acumulado
// entre execuções de Health Check persistido em health_check_event_history (ver
// HealthCheckStorage.GetEventChronicity em storage.go) — o Count/FirstTimestamp de um EventHealth
// sozinho não é suficiente porque a K8s Events API só retém eventos por poucas horas por padrão.
type EventChronicity struct {
	CumulativeCount int64     `json:"cumulative_count"`
	FirstSeenEver   time.Time `json:"first_seen_ever"`
	IsChronic       bool      `json:"is_chronic"`
}

// CriticalEventReasons lista de reasons que são considerados críticos
var CriticalEventReasons = map[string]bool{
	// Scheduling
	"FailedScheduling": true,
	// Container/Pod
	"BackOff":          true,
	"CrashLoopBackOff": true,
	"Failed":           true,
	"FailedCreate":     true,
	"FailedKillPod":    true,
	// Image
	"ErrImagePull":     true,
	"ImagePullBackOff": true,
	"InvalidImageName": true,
	// Volume/Mount
	"FailedMount":        true,
	"FailedAttachVolume": true,
	"FailedMapVolume":    true,
	// Node
	"NodeNotReady":       true,
	"NodeNotSchedulable": true,
	"Rebooted":           true,
	"HostPortConflict":   true,
	// Resource
	"FailedCreatePodSandBox": true,
	"InsufficientCPU":        true,
	"InsufficientMemory":     true,
	"OutOfDisk":              true,
	"OutOfMemory":            true,
	"OOMKilling":             true,
	// Network
	"NetworkNotReady":               true,
	"FailedCreatePodNetworkSandbox": true,
}

// WarningEventReasons lista de reasons que são considerados warnings
var WarningEventReasons = map[string]bool{
	// Probes
	"Unhealthy":    true,
	"ProbeWarning": true,
	// Scaling
	"FailedGetScale": true,
	"FailedRescale":  true,
	// Updates
	"FailedUpdate": true,
	// Eviction
	"Evicted":   true,
	"Preempted": true,
	// Storage
	"VolumeResizeFailed": true,
	"ProvisioningFailed": true,
	// Node
	"NodeHasSufficientMemory": false, // Ignorar (é positivo)
	"NodeHasSufficientDisk":   false, // Ignorar (é positivo)
	"NodeHasNoDiskPressure":   false, // Ignorar (é positivo)
}

// humanizeEventMessage transforma mensagens técnicas do Kubernetes em mensagens legíveis
// Mantém dados técnicos importantes mas formata de forma mais clara
func humanizeEventMessage(reason, originalMessage, involvedKind, involvedName, namespace string) string {
	// === PRESSURE (Node-Problem-Detector) - Casos mais complexos primeiro ===
	if strings.Contains(reason, "PressureIGDiagnostics") {
		return formatPressureMessage(reason, originalMessage, involvedName)
	}

	// Mapeamento de reasons para mensagens humanizadas
	switch reason {
	// === SCHEDULING ===
	case "FailedScheduling":
		return formatSchedulingMessage(originalMessage, involvedName, namespace)

	// === CONTAINER/POD ===
	case "BackOff", "CrashLoopBackOff":
		restarts := extractValue(originalMessage, "restart count", "")
		if restarts != "" {
			return fmt.Sprintf("CRASHLOOP: Pod %s/%s reiniciando em loop (restarts: %s). Verificar logs: kubectl logs %s -n %s --previous", namespace, involvedName, restarts, involvedName, namespace)
		}
		return fmt.Sprintf("CRASHLOOP: Pod %s/%s reiniciando em loop. Verificar logs: kubectl logs %s -n %s --previous", namespace, involvedName, involvedName, namespace)

	case "Failed":
		return fmt.Sprintf("FALHA: %s %s/%s - %s", involvedKind, namespace, involvedName, cleanMessage(originalMessage, 200))

	case "FailedCreate":
		return fmt.Sprintf("FALHA AO CRIAR: Pod %s/%s - %s", namespace, involvedName, cleanMessage(originalMessage, 150))

	case "FailedKillPod":
		return fmt.Sprintf("FALHA AO ENCERRAR: Pod %s/%s nao responde a SIGTERM. %s", namespace, involvedName, cleanMessage(originalMessage, 100))

	// === IMAGE ===
	case "ErrImagePull", "ImagePullBackOff":
		return formatImagePullMessage(originalMessage, involvedName, namespace)

	case "InvalidImageName":
		return fmt.Sprintf("IMAGEM INVALIDA: %s/%s - nome de imagem mal formatado. %s", namespace, involvedName, cleanMessage(originalMessage, 100))

	// === VOLUME/MOUNT ===
	case "FailedMount":
		return fmt.Sprintf("FALHA MOUNT: %s/%s - %s", namespace, involvedName, cleanMessage(originalMessage, 200))

	case "FailedAttachVolume":
		return fmt.Sprintf("FALHA ATTACH VOLUME: %s/%s - volume pode estar em uso. %s", namespace, involvedName, cleanMessage(originalMessage, 150))

	// === NODE ===
	case "NodeNotReady":
		return fmt.Sprintf("NODE NAO PRONTO: %s - kubelet pode estar com problemas. %s", involvedName, cleanMessage(originalMessage, 150))

	case "NodeNotSchedulable":
		return fmt.Sprintf("NODE CORDONED: %s - nao recebe novos pods. %s", involvedName, cleanMessage(originalMessage, 100))

	case "Rebooted":
		return fmt.Sprintf("NODE REINICIADO: %s - pods foram reagendados. %s", involvedName, cleanMessage(originalMessage, 100))

	// === RESOURCES ===
	case "InsufficientCPU":
		return fmt.Sprintf("CPU INSUFICIENTE: Pod %s/%s - cluster sem capacidade. %s", namespace, involvedName, cleanMessage(originalMessage, 150))

	case "InsufficientMemory":
		return fmt.Sprintf("MEMORIA INSUFICIENTE: Pod %s/%s - cluster sem capacidade. %s", namespace, involvedName, cleanMessage(originalMessage, 150))

	case "OutOfMemory", "OOMKilling", "OOMKilled":
		return fmt.Sprintf("OOM KILL: Container em %s/%s encerrado por falta de memoria. Aumentar memory limits.", namespace, involvedName)

	case "OutOfDisk":
		return fmt.Sprintf("DISCO CHEIO: Node %s sem espaco. Limpar ou expandir disco.", involvedName)

	// === PROBES ===
	case "Unhealthy":
		return formatProbeMessage(originalMessage, involvedName, namespace)

	// === EVICTION ===
	case "Evicted":
		evictReason := extractValue(originalMessage, "reason", "pressao de recursos")
		return fmt.Sprintf("EVICTED: Pod %s/%s removido do node. Motivo: %s", namespace, involvedName, evictReason)

	case "Preempted":
		return fmt.Sprintf("PREEMPTED: Pod %s/%s removido para dar lugar a pod de maior prioridade.", namespace, involvedName)

	// === NETWORK ===
	case "NetworkNotReady":
		return fmt.Sprintf("REDE NAO PRONTA: %s/%s - CNI pode estar com problemas. %s", namespace, involvedName, cleanMessage(originalMessage, 150))

	// === DEFAULT ===
	default:
		return fmt.Sprintf("%s: %s %s/%s - %s", reason, involvedKind, namespace, involvedName, cleanMessage(originalMessage, 250))
	}
}

// formatPressureMessage formata mensagens de pressão de recursos (CPU/Memory/Disk)
func formatPressureMessage(reason, originalMessage, nodeName string) string {
	var sb strings.Builder

	// Identificar tipo de pressão
	pressureType := "RECURSOS"
	if strings.Contains(reason, "CPU") {
		pressureType = "CPU"
	} else if strings.Contains(reason, "Memory") {
		pressureType = "MEMORIA"
	} else if strings.Contains(reason, "Disk") {
		pressureType = "DISCO"
	}

	sb.WriteString(fmt.Sprintf("PRESSAO DE %s no Node %s", pressureType, nodeName))

	// Extrair métricas relevantes
	metrics := []string{}

	// CPU cores
	if cores := extractValue(originalMessage, "Number of CPU cores:", ""); cores != "" {
		metrics = append(metrics, fmt.Sprintf("Cores: %s", cores))
	}

	// Load
	if load := extractValue(originalMessage, "/proc/loadavg:", ""); load != "" {
		threshold := extractValue(originalMessage, "Load threshold:", "")
		if threshold != "" {
			metrics = append(metrics, fmt.Sprintf("Load: %s (threshold: %s)", load, threshold))
		} else {
			metrics = append(metrics, fmt.Sprintf("Load: %s", load))
		}
	}

	// PSI CPU
	if psiCPU := extractValue(originalMessage, "PSI CPU some avg300:", ""); psiCPU != "" {
		metrics = append(metrics, fmt.Sprintf("PSI CPU: %s%%", psiCPU))
	}

	// PSI Memory
	if psiMem := extractValue(originalMessage, "PSI Memory some avg300:", ""); psiMem != "" {
		metrics = append(metrics, fmt.Sprintf("PSI Memory: %s%%", psiMem))
	}

	// CPU stats
	if userCPU := extractValue(originalMessage, "user:", ""); userCPU != "" {
		systemCPU := extractValue(originalMessage, "ystem:", "") // "system:" pode vir como "ystem:"
		idleCPU := extractValue(originalMessage, "dle:", "")
		if systemCPU != "" && idleCPU != "" {
			metrics = append(metrics, fmt.Sprintf("CPU: user %s, sys %s, idle %s", userCPU, systemCPU, idleCPU))
		} else {
			metrics = append(metrics, fmt.Sprintf("CPU user: %s", userCPU))
		}
	}

	// Memory stats
	if memUsed := extractValue(originalMessage, "MemUsed:", ""); memUsed != "" {
		memTotal := extractValue(originalMessage, "MemTotal:", "")
		if memTotal != "" {
			metrics = append(metrics, fmt.Sprintf("Mem: %s / %s", memUsed, memTotal))
		}
	}

	// Adicionar métricas à mensagem
	if len(metrics) > 0 {
		sb.WriteString(" | ")
		sb.WriteString(strings.Join(metrics, " | "))
	}

	// Adicionar recomendação
	sb.WriteString(" | Acao: escalar nodes ou reduzir workloads")

	return sb.String()
}

// formatSchedulingMessage formata mensagens de falha de agendamento
func formatSchedulingMessage(originalMessage, podName, namespace string) string {
	var reason string

	if strings.Contains(originalMessage, "insufficient cpu") {
		cpuNeeded := extractValue(originalMessage, "cpu", "")
		reason = fmt.Sprintf("CPU insuficiente no cluster (necessario: %s)", cpuNeeded)
	} else if strings.Contains(originalMessage, "insufficient memory") {
		memNeeded := extractValue(originalMessage, "memory", "")
		reason = fmt.Sprintf("Memoria insuficiente no cluster (necessario: %s)", memNeeded)
	} else if strings.Contains(originalMessage, "node(s) had taint") {
		reason = "Nodes tem taints que o pod nao tolera"
	} else if strings.Contains(originalMessage, "node(s) didn't match") {
		reason = "Nenhum node atende nodeSelector/affinity"
	} else if strings.Contains(originalMessage, "PersistentVolumeClaim") {
		reason = "PVC nao encontrado ou nao bound"
	} else {
		reason = cleanMessage(originalMessage, 150)
	}

	return fmt.Sprintf("SCHEDULING FALHOU: Pod %s/%s - %s", namespace, podName, reason)
}

// formatImagePullMessage formata mensagens de erro ao baixar imagem
func formatImagePullMessage(originalMessage, podName, namespace string) string {
	// Extrair nome da imagem se possível
	image := extractValue(originalMessage, "image", "")

	if strings.Contains(originalMessage, "not found") {
		if image != "" {
			return fmt.Sprintf("IMAGEM NAO ENCONTRADA: %s/%s - imagem '%s' nao existe no registry", namespace, podName, image)
		}
		return fmt.Sprintf("IMAGEM NAO ENCONTRADA: %s/%s - verificar nome e tag da imagem", namespace, podName)
	}

	if strings.Contains(originalMessage, "unauthorized") || strings.Contains(originalMessage, "denied") {
		return fmt.Sprintf("SEM PERMISSAO: %s/%s - verificar ImagePullSecrets e credenciais do registry", namespace, podName)
	}

	if strings.Contains(originalMessage, "timeout") {
		return fmt.Sprintf("TIMEOUT: %s/%s - timeout ao baixar imagem. Verificar conectividade com registry", namespace, podName)
	}

	return fmt.Sprintf("ERRO PULL IMAGEM: %s/%s - %s", namespace, podName, cleanMessage(originalMessage, 150))
}

// formatProbeMessage formata mensagens de falha de probes
func formatProbeMessage(originalMessage, podName, namespace string) string {
	probeType := "PROBE"
	if strings.Contains(originalMessage, "Liveness") || strings.Contains(originalMessage, "liveness") {
		probeType = "LIVENESS"
	} else if strings.Contains(originalMessage, "Readiness") || strings.Contains(originalMessage, "readiness") {
		probeType = "READINESS"
	} else if strings.Contains(originalMessage, "Startup") || strings.Contains(originalMessage, "startup") {
		probeType = "STARTUP"
	}

	// Extrair código HTTP se existir
	httpCode := extractValue(originalMessage, "HTTP probe failed with statuscode:", "")
	if httpCode != "" {
		return fmt.Sprintf("%s FALHOU: Pod %s/%s retornou HTTP %s. Verificar endpoint de health", probeType, namespace, podName, httpCode)
	}

	// Extrair erro de conexão
	if strings.Contains(originalMessage, "connection refused") {
		return fmt.Sprintf("%s FALHOU: Pod %s/%s - conexao recusada. App pode nao estar escutando na porta correta", probeType, namespace, podName)
	}

	if strings.Contains(originalMessage, "timeout") {
		return fmt.Sprintf("%s FALHOU: Pod %s/%s - timeout. Aumentar timeoutSeconds ou verificar performance da app", probeType, namespace, podName)
	}

	return fmt.Sprintf("%s FALHOU: Pod %s/%s - %s", probeType, namespace, podName, cleanMessage(originalMessage, 150))
}

// extractValue extrai um valor de uma mensagem baseado em um prefixo
func extractValue(message, prefix, defaultVal string) string {
	// Normalizar quebras de linha
	msg := strings.ReplaceAll(message, "\\n", "\n")

	idx := strings.Index(strings.ToLower(msg), strings.ToLower(prefix))
	if idx == -1 {
		return defaultVal
	}

	// Pegar substring após o prefixo
	start := idx + len(prefix)
	if start >= len(msg) {
		return defaultVal
	}

	// Encontrar o fim do valor (próximo espaço, quebra de linha, ou fim da string)
	substr := msg[start:]
	substr = strings.TrimLeft(substr, " :")

	// Encontrar fim do valor
	end := len(substr)
	for i, ch := range substr {
		if ch == '\n' || ch == '\\' || ch == '|' {
			end = i
			break
		}
	}

	value := strings.TrimSpace(substr[:end])
	return value
}

// cleanMessage limpa uma mensagem removendo caracteres especiais (SEM truncar)
func cleanMessage(msg string, _ int) string {
	// Remover quebras de linha literais e reais
	msg = strings.ReplaceAll(msg, "\\n", " | ")
	msg = strings.ReplaceAll(msg, "\n", " | ")
	msg = strings.ReplaceAll(msg, "\t", " ")
	msg = strings.ReplaceAll(msg, "\r", "")

	// Remover caracteres de formatação problemáticos do Go fmt
	msg = strings.ReplaceAll(msg, "%!(MISSING)", "")
	msg = strings.ReplaceAll(msg, "((MISSING)", "")
	msg = strings.ReplaceAll(msg, "(MISSING)", "")
	msg = strings.ReplaceAll(msg, "%!s", "")
	msg = strings.ReplaceAll(msg, "%!i", "")
	msg = strings.ReplaceAll(msg, "%!", "")
	msg = strings.ReplaceAll(msg, "!(", "(")

	// Remover espaços múltiplos
	for strings.Contains(msg, "  ") {
		msg = strings.ReplaceAll(msg, "  ", " ")
	}
	// Remover pipes múltiplos
	for strings.Contains(msg, "| |") {
		msg = strings.ReplaceAll(msg, "| |", "|")
	}

	return strings.TrimSpace(msg)
}

// EventChecker verifica eventos do Kubernetes
type EventChecker struct {
	// Tempo máximo para considerar eventos (padrão: 1 hora)
	MaxEventAge time.Duration
}

// NewEventChecker cria um novo event checker
func NewEventChecker() *EventChecker {
	return &EventChecker{
		MaxEventAge: 1 * time.Hour, // Considerar apenas eventos da última hora
	}
}

// CheckAll verifica eventos em todos os namespaces especificados
func (c *EventChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, progressCallback ProgressCallback) []EventHealth {
	results := []EventHealth{}
	totalNamespaces := len(namespaces)

	for i, ns := range namespaces {
		if progressCallback != nil {
			progressCallback(ns, "", fmt.Sprintf("Verificando eventos em %s...", ns), StatusHealthy, i+1, totalNamespaces)
		}

		// Contexto com timeout
		listCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)

		events, err := client.CoreV1().Events(ns).List(listCtx, metav1.ListOptions{})
		if cancel != nil {
			cancel()
		}

		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list events")
			continue
		}

		// Filtrar e processar eventos
		for _, event := range events.Items {
			eventHealth := c.processEvent(&event)
			if eventHealth != nil {
				results = append(results, *eventHealth)

				// ✅ Publicar detalhes do evento encontrado via callback
				if progressCallback != nil {
					var eventStatus HealthStatus
					if eventHealth.Severity == SeverityCritical {
						eventStatus = StatusCritical
					} else {
						eventStatus = StatusWarning
					}

					// Usar a mensagem já humanizada do eventHealth
					progressCallback(ns, eventHealth.Name, eventHealth.Message, eventStatus, i+1, totalNamespaces)
				}
			}
		}
	}

	// Ordenar por severidade (críticos primeiro) e depois por timestamp
	c.sortBySeverity(results)

	log.Info().
		Int("total_events", len(results)).
		Int("namespaces_checked", totalNamespaces).
		Msg("Event check completed")

	return results
}

// processEvent processa um evento e retorna EventHealth se for relevante
func (c *EventChecker) processEvent(event *corev1.Event) *EventHealth {
	// Ignorar eventos muito antigos
	eventTime := event.LastTimestamp.Time
	if eventTime.IsZero() {
		eventTime = event.EventTime.Time
	}
	if eventTime.IsZero() {
		eventTime = event.CreationTimestamp.Time
	}

	if time.Since(eventTime) > c.MaxEventAge {
		return nil
	}

	// Ignorar eventos do tipo Normal (apenas Warning e Error)
	if event.Type == corev1.EventTypeNormal {
		return nil
	}

	// Determinar severidade
	severity := c.determineSeverity(event.Reason)
	if severity == SeverityInfo {
		return nil // Ignorar eventos informativos
	}

	// Criar mensagem humanizada
	humanizedMessage := humanizeEventMessage(
		event.Reason,
		event.Message,
		event.InvolvedObject.Kind,
		event.InvolvedObject.Name,
		event.Namespace,
	)

	// Criar EventHealth
	health := &EventHealth{
		Name:           event.Name,
		Namespace:      event.Namespace,
		Reason:         event.Reason,
		Message:        humanizedMessage, // Usar mensagem humanizada
		Type:           event.Type,
		Severity:       severity,
		Count:          event.Count,
		FirstTimestamp: event.FirstTimestamp.Time,
		LastTimestamp:  eventTime,
		InvolvedKind:   event.InvolvedObject.Kind,
		InvolvedName:   event.InvolvedObject.Name,
		Suggestions:    c.getSuggestions(event.Reason, event.InvolvedObject.Kind, event.Namespace, event.InvolvedObject.Name),
	}

	// Mapear para HealthStatus
	if severity == SeverityCritical {
		health.Status = StatusCritical
	} else {
		health.Status = StatusWarning
	}

	return health
}

// determineSeverity determina a severidade baseada no reason do evento
func (c *EventChecker) determineSeverity(reason string) Severity {
	if CriticalEventReasons[reason] {
		return SeverityCritical
	}
	if WarningEventReasons[reason] {
		return SeverityMedium
	}
	// Eventos Warning não mapeados são tratados como medium por padrão
	return SeverityMedium
}

// getSuggestions retorna sugestões de correção baseadas no tipo de evento
func (c *EventChecker) getSuggestions(reason, kind, namespace, name string) []string {
	suggestions := []string{}

	switch reason {
	// Scheduling
	case "FailedScheduling":
		suggestions = append(suggestions, "Verificar se há nodes disponíveis com recursos suficientes")
		suggestions = append(suggestions, "kubectl describe pod "+name+" -n "+namespace)
		suggestions = append(suggestions, "kubectl get nodes -o wide")
		suggestions = append(suggestions, "Verificar taints e tolerations dos nodes")

	// Container/Pod
	case "BackOff", "CrashLoopBackOff":
		suggestions = append(suggestions, "Verificar logs do container")
		suggestions = append(suggestions, "kubectl logs "+name+" -n "+namespace+" --previous")
		suggestions = append(suggestions, "kubectl describe pod "+name+" -n "+namespace)
		suggestions = append(suggestions, "Verificar se a aplicação está configurada corretamente")

	case "Failed", "FailedCreate":
		suggestions = append(suggestions, "kubectl describe "+strings.ToLower(kind)+" "+name+" -n "+namespace)
		suggestions = append(suggestions, "Verificar eventos relacionados")

	// Image
	case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
		suggestions = append(suggestions, "Verificar se a imagem existe no registry")
		suggestions = append(suggestions, "Verificar ImagePullSecrets configurados")
		suggestions = append(suggestions, "kubectl describe pod "+name+" -n "+namespace)
		suggestions = append(suggestions, "Validar credenciais do registry")

	// Volume/Mount
	case "FailedMount", "FailedAttachVolume", "FailedMapVolume":
		suggestions = append(suggestions, "Verificar se o PV/PVC existe e está bound")
		suggestions = append(suggestions, "kubectl get pvc -n "+namespace)
		suggestions = append(suggestions, "kubectl describe pvc -n "+namespace)
		suggestions = append(suggestions, "Verificar se o storage class está disponível")

	// Node
	case "NodeNotReady", "NodeNotSchedulable":
		suggestions = append(suggestions, "kubectl describe node "+name)
		suggestions = append(suggestions, "Verificar status do kubelet no node")
		suggestions = append(suggestions, "Verificar conectividade de rede do node")

	case "Rebooted":
		suggestions = append(suggestions, "Verificar motivo do reboot no node")
		suggestions = append(suggestions, "kubectl describe node "+name)
		suggestions = append(suggestions, "Verificar logs do sistema operacional")

	// Resource
	case "InsufficientCPU", "InsufficientMemory":
		suggestions = append(suggestions, "Verificar requests/limits dos pods")
		suggestions = append(suggestions, "Considerar adicionar mais nodes ao cluster")
		suggestions = append(suggestions, "kubectl top nodes")
		suggestions = append(suggestions, "kubectl top pods -n "+namespace)

	case "OutOfMemory", "OOMKilling":
		suggestions = append(suggestions, "Aumentar limits de memória do container")
		suggestions = append(suggestions, "Verificar memory leaks na aplicação")
		suggestions = append(suggestions, "kubectl describe pod "+name+" -n "+namespace)

	// Probes
	case "Unhealthy":
		suggestions = append(suggestions, "Verificar configuração de liveness/readiness probes")
		suggestions = append(suggestions, "kubectl describe pod "+name+" -n "+namespace)
		suggestions = append(suggestions, "Verificar se aplicação responde no endpoint de health")
		suggestions = append(suggestions, "Aumentar timeout/threshold das probes se necessário")

	// Eviction
	case "Evicted", "Preempted":
		suggestions = append(suggestions, "Verificar pressão de recursos no node")
		suggestions = append(suggestions, "kubectl describe node")
		suggestions = append(suggestions, "Verificar PodDisruptionBudget")

	default:
		suggestions = append(suggestions, "kubectl describe "+strings.ToLower(kind)+" "+name+" -n "+namespace)
		suggestions = append(suggestions, "kubectl get events -n "+namespace+" --sort-by='.lastTimestamp'")
	}

	return suggestions
}

// sortBySeverity ordena eventos por severidade (críticos primeiro)
func (c *EventChecker) sortBySeverity(events []EventHealth) {
	// Bubble sort simples (lista geralmente pequena)
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			// Críticos antes de warnings
			if events[j].Severity == SeverityCritical && events[i].Severity != SeverityCritical {
				events[i], events[j] = events[j], events[i]
			}
			// Dentro da mesma severidade, mais recentes primeiro
			if events[i].Severity == events[j].Severity && events[j].LastTimestamp.After(events[i].LastTimestamp) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
}

// GetCriticalCount retorna número de eventos críticos
func (c *EventChecker) GetCriticalCount(events []EventHealth) int {
	count := 0
	for _, e := range events {
		if e.Severity == SeverityCritical {
			count++
		}
	}
	return count
}

// GetWarningCount retorna número de eventos warning
func (c *EventChecker) GetWarningCount(events []EventHealth) int {
	count := 0
	for _, e := range events {
		if e.Severity == SeverityMedium {
			count++
		}
	}
	return count
}

// GroupByResource agrupa eventos por recurso relacionado
func (c *EventChecker) GroupByResource(events []EventHealth) map[string][]EventHealth {
	grouped := make(map[string][]EventHealth)

	for _, e := range events {
		key := fmt.Sprintf("%s/%s/%s", e.Namespace, e.InvolvedKind, e.InvolvedName)
		grouped[key] = append(grouped[key], e)
	}

	return grouped
}

// FilterByKind filtra eventos por tipo de recurso (Pod, Node, Deployment, etc.)
func (c *EventChecker) FilterByKind(events []EventHealth, kind string) []EventHealth {
	filtered := []EventHealth{}
	for _, e := range events {
		if strings.EqualFold(e.InvolvedKind, kind) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// SetMaxEventAge configura o tempo máximo para considerar eventos
func (c *EventChecker) SetMaxEventAge(duration time.Duration) {
	c.MaxEventAge = duration
}
