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

// EventSeverity representa a severidade de um evento Kubernetes
type EventSeverity string

const (
	SeverityCritical EventSeverity = "critical"
	SeverityWarning  EventSeverity = "warning"
	SeverityInfo     EventSeverity = "info"
)

// EventHealth representa um evento Kubernetes relevante para health checking
type EventHealth struct {
	Name           string        `json:"name"`
	Namespace      string        `json:"namespace"`
	Reason         string        `json:"reason"`          // FailedScheduling, BackOff, etc.
	Message        string        `json:"message"`         // Mensagem completa do evento
	Type           string        `json:"type"`            // Warning, Normal
	Severity       EventSeverity `json:"severity"`        // critical, warning, info
	Count          int32         `json:"count"`           // Número de ocorrências
	FirstTimestamp time.Time     `json:"first_timestamp"` // Primeira ocorrência
	LastTimestamp  time.Time     `json:"last_timestamp"`  // Última ocorrência
	// Recurso relacionado
	InvolvedKind string `json:"involved_kind"` // Pod, Node, Deployment, etc.
	InvolvedName string `json:"involved_name"` // Nome do recurso
	// Sugestões de correção
	Suggestions []string `json:"suggestions"`
	Status      HealthStatus `json:"status"` // Para compatibilidade com outros checkers
}

// CriticalEventReasons lista de reasons que são considerados críticos
var CriticalEventReasons = map[string]bool{
	// Scheduling
	"FailedScheduling": true,
	// Container/Pod
	"BackOff":           true,
	"CrashLoopBackOff":  true,
	"Failed":            true,
	"FailedCreate":      true,
	"FailedKillPod":     true,
	// Image
	"ErrImagePull":      true,
	"ImagePullBackOff":  true,
	"InvalidImageName":  true,
	// Volume/Mount
	"FailedMount":       true,
	"FailedAttachVolume": true,
	"FailedMapVolume":   true,
	// Node
	"NodeNotReady":      true,
	"NodeNotSchedulable": true,
	"Rebooted":          true,
	"HostPortConflict":  true,
	// Resource
	"FailedCreatePodSandBox": true,
	"InsufficientCPU":    true,
	"InsufficientMemory": true,
	"OutOfDisk":          true,
	"OutOfMemory":        true,
	"OOMKilling":         true,
	// Network
	"NetworkNotReady":    true,
	"FailedCreatePodNetworkSandbox": true,
}

// WarningEventReasons lista de reasons que são considerados warnings
var WarningEventReasons = map[string]bool{
	// Probes
	"Unhealthy":         true,
	"ProbeWarning":      true,
	// Scaling
	"FailedGetScale":    true,
	"FailedRescale":     true,
	// Updates
	"FailedUpdate":      true,
	// Eviction
	"Evicted":           true,
	"Preempted":         true,
	// Storage
	"VolumeResizeFailed": true,
	"ProvisioningFailed": true,
	// Node
	"NodeHasSufficientMemory": false, // Ignorar (é positivo)
	"NodeHasSufficientDisk":   false, // Ignorar (é positivo)
	"NodeHasNoDiskPressure":   false, // Ignorar (é positivo)
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
					// Formatar mensagem com detalhes do evento
					var statusIcon string
					var eventStatus HealthStatus
					if eventHealth.Severity == SeverityCritical {
						statusIcon = "Critico"
						eventStatus = StatusCritical
					} else {
						statusIcon = "Aviso"
						eventStatus = StatusWarning
					}

					detailMsg := fmt.Sprintf("%s %s/%s: %s - %s",
						statusIcon,
						eventHealth.InvolvedKind,
						eventHealth.InvolvedName,
						eventHealth.Reason,
						eventHealth.Message)

					// Truncar mensagem se muito longa
					if len(detailMsg) > 200 {
						detailMsg = detailMsg[:197] + "..."
					}

					progressCallback(ns, eventHealth.Name, detailMsg, eventStatus, i+1, totalNamespaces)
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

	// Criar EventHealth
	health := &EventHealth{
		Name:           event.Name,
		Namespace:      event.Namespace,
		Reason:         event.Reason,
		Message:        event.Message,
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
func (c *EventChecker) determineSeverity(reason string) EventSeverity {
	if CriticalEventReasons[reason] {
		return SeverityCritical
	}
	if WarningEventReasons[reason] {
		return SeverityWarning
	}
	// Eventos Warning não mapeados são tratados como warning por padrão
	return SeverityWarning
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
		if e.Severity == SeverityWarning {
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
