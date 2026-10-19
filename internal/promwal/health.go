package promwal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HealthState é o estado de integridade de um recurso Prometheus (verificação leve, só API do Kubernetes).
type HealthState string

const (
	HealthRestorePending HealthState = "desligado_por_limpeza" // limpeza interrompida: o Prometheus foi desligado e não religou
	HealthDown           HealthState = "fora_do_ar"            // pods sem Ready com OOM/CrashLoop/Pending, ou sem pods
	HealthRecurrent      HealthState = "reincidente"           // 2 ou mais limpezas do WAL em 30 dias
	HealthStarting       HealthState = "iniciando"             // ainda subindo (sem restarts e recém-criado)
	HealthStopped        HealthState = "desligado"             // replicas 0 sem anotação de limpeza (alguém desligou)
	HealthHealthy        HealthState = "saudavel"
	HealthUnverifiable   HealthState = "nao_verificavel" // sem alcance ou sem permissão
)

const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"

	// startupGrace: pod novo sem restarts ainda pode estar subindo (replay do WAL leva tempo).
	startupGrace = 5 * time.Minute
)

type HealthPod struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
	Reason   string `json:"reason,omitempty"` // OOMKilled, CrashLoopBackOff, Pending…
}

type HealthItem struct {
	Namespace              string      `json:"namespace"`
	Name                   string      `json:"name"`
	State                  HealthState `json:"state"`
	Severity               string      `json:"severity,omitempty"` // critical | warning | vazio (sem alerta)
	Summary                string      `json:"summary"`
	Pods                   []HealthPod `json:"pods,omitempty"`
	Recurrence             Recurrence  `json:"recurrence"`
	PendingRestoreReplicas *int32      `json:"pending_restore_replicas,omitempty"`
}

type HealthReport struct {
	Cluster         string       `json:"cluster"`
	CheckedAt       time.Time    `json:"checked_at"`
	Verifiable      bool         `json:"verifiable"`       // false = não deu para consultar o cluster (sem alerta)
	Reason          string       `json:"reason,omitempty"` // por que não é verificável
	OperatorPresent bool         `json:"operator_present"` // false = o cluster não tem o CRD do prometheus-operator
	Items           []HealthItem `json:"items"`
}

type prometheusCRList struct {
	Items []prometheusCR `json:"items"`
}

// Health verifica a integridade dos recursos Prometheus do cluster. Leitura pura da API do Kubernetes (um
// `kubectl get prometheus -A` e uma listagem de pods por namespace): sem pod temporário, sem logs, sem CLI cloud.
// hist devolve as datas de limpeza do Change History de um recurso (pode ser nil).
func (c *Collector) Health(ctx context.Context, cluster string, hist func(namespace, name string) []time.Time) HealthReport {
	rep := HealthReport{Cluster: cluster, CheckedAt: now(), Items: []HealthItem{}}

	out, err := c.Run(ctx, cluster, "", "get", "prometheus", "--all-namespaces", "-o", "json")
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "doesn't have a resource type") || strings.Contains(msg, "no matches for kind") {
			rep.Verifiable = true // o cluster respondeu: simplesmente não usa prometheus-operator
			return rep
		}
		rep.Reason = healthReason(msg)
		return rep
	}
	var list prometheusCRList
	if err := json.Unmarshal(out, &list); err != nil {
		rep.Reason = "resposta inválida ao listar os recursos Prometheus"
		return rep
	}
	rep.Verifiable, rep.OperatorPresent = true, true

	for _, cr := range list.Items {
		ns, name := cr.Metadata.Namespace, cr.Metadata.Name
		pods, err := c.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: defaultCRLabel + "=" + name})
		if err != nil {
			rep.Items = append(rep.Items, HealthItem{
				Namespace: ns, Name: name, State: HealthUnverifiable,
				Summary: "não foi possível listar os pods: " + healthReason(err.Error()),
			})
			continue
		}
		var dates []time.Time
		if hist != nil {
			dates = hist(ns, name)
		}
		rep.Items = append(rep.Items, classifyHealth(cr, pods.Items, dates, rep.CheckedAt))
	}
	return rep
}

func healthReason(msg string) string {
	switch {
	case strings.Contains(msg, "Forbidden") || strings.Contains(msg, "forbidden"):
		return "sem permissão no cluster para listar os recursos Prometheus"
	case strings.Contains(msg, "Unable to connect") || strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "no such host") || strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "signal: killed"):
		return "sem alcance ao cluster (VPN desconectada ou API indisponível)"
	default:
		return msg
	}
}

// classifyHealth decide o estado de um recurso Prometheus. Prioridade: limpeza interrompida > fora do ar >
// reincidente > iniciando > desligado > saudável.
func classifyHealth(cr prometheusCR, pods []corev1.Pod, histDates []time.Time, now time.Time) HealthItem {
	item := HealthItem{Namespace: cr.Metadata.Namespace, Name: cr.Metadata.Name}
	ann := cr.Metadata.Annotations
	item.Recurrence = BuildRecurrence(parseDates(ann[AnnotationCleanupHistory]), histDates, parseCount(ann[AnnotationCleanupCount]), now)

	replicas := int32(1)
	if cr.Spec.Replicas != nil {
		replicas = *cr.Spec.Replicas
	}

	var notReady, starting []string
	for i := range pods {
		p := podInfo(&pods[i])
		reason := p.LastTermReason
		if p.WaitingReason != "" {
			reason = p.WaitingReason
		}
		item.Pods = append(item.Pods, HealthPod{Name: p.Name, Ready: p.Ready, Restarts: p.Restarts, Reason: reason})
		if p.Ready {
			continue
		}
		young := now.Sub(pods[i].CreationTimestamp.Time) < startupGrace
		if p.Restarts == 0 && !isBadWaiting(p.WaitingReason) && p.LastTermReason == "" && young {
			starting = append(starting, p.Name)
			continue
		}
		desc := p.Name
		if reason != "" {
			desc += ": " + reason
		}
		if cause, _ := schedulingCause(p.SchedulingMessage); cause != "" && p.Node == "" {
			desc += " — " + cause
		}
		if p.Restarts > 0 {
			desc += fmt.Sprintf(" (%d restart(s))", p.Restarts)
		}
		notReady = append(notReady, desc)
	}

	switch {
	case ann[AnnotationOriginalReplicas] != "":
		n := parseReplicas(ann[AnnotationOriginalReplicas])
		item.PendingRestoreReplicas = &n
		item.State, item.Severity = HealthRestorePending, SeverityCritical
		item.Summary = fmt.Sprintf("Uma limpeza do WAL desligou o Prometheus e não chegou a religar (réplicas originais: %d). Restaure pelo diagnóstico do WAL.", n)
	case replicas > 0 && len(pods) == 0:
		item.State, item.Severity = HealthDown, SeverityCritical
		item.Summary = fmt.Sprintf("Nenhum pod do Prometheus (esperado: %d réplica(s)).", replicas)
	case len(notReady) > 0:
		item.State, item.Severity = HealthDown, SeverityCritical
		item.Summary = "Prometheus fora do ar: " + strings.Join(notReady, "; ")
	case item.Recurrence.Recurrent:
		item.State, item.Severity = HealthRecurrent, SeverityWarning
		item.Summary = RecurrenceWarning(item.Recurrence, now)
	case len(starting) > 0:
		item.State = HealthStarting
		item.Summary = "Subindo: " + strings.Join(starting, ", ")
	case replicas == 0:
		item.State = HealthStopped
		item.Summary = "Desligado (replicas 0), sem limpeza do WAL em andamento."
	default:
		item.State = HealthHealthy
		item.Summary = "Todos os pods Ready."
	}
	return item
}

func isBadWaiting(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "RunContainerError":
		return true
	}
	return false
}
