package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// HPAChecker valida HPAs (HorizontalPodAutoscalers)
type HPAChecker struct{}

// NewHPAChecker cria um novo HPA checker
func NewHPAChecker() *HPAChecker {
	return &HPAChecker{}
}

// ProgressCallback tipo de callback para progresso
type HPAProgressCallback func(namespace, name, message string, status HealthStatus, current, total int)

// CheckAll valida todos os HPAs nos namespaces especificados
func (c *HPAChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, applyFilters bool, progressCallback HPAProgressCallback) []HPAHealth {
	results := []HPAHealth{}

	// Criar contexto com timeout se especificado
	workCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		workCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	// Primeiro, contar total de HPAs para calcular progresso
	totalHPAs := 0
	for _, ns := range namespaces {
		hpas, err := client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list HPAs")
			continue
		}
		totalHPAs += len(hpas.Items)
	}

	currentHPA := 0

	for _, ns := range namespaces {
		// Listar HPAs no namespace
		hpas, err := client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(workCtx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list HPAs")
			continue
		}

		for _, hpa := range hpas.Items {
			currentHPA++

			// Publicar evento: validando HPA
			if progressCallback != nil {
				progressCallback(ns, hpa.Name,
					fmt.Sprintf("Validando HPA %s/%s...", ns, hpa.Name),
					StatusHealthy, currentHPA, totalHPAs)
			}

			health := c.validateHPA(workCtx, client, ns, &hpa, applyFilters)
			results = append(results, health)

			// Publicar resultado
			if progressCallback != nil {
				summary := c.getHealthSummary(health)
				progressCallback(ns, hpa.Name,
					fmt.Sprintf("HPA %s/%s: %s", ns, hpa.Name, summary), health.Status, currentHPA, totalHPAs)
			}
		}
	}

	return results
}

// getHealthSummary gera resumo compacto do health check
func (c *HPAChecker) getHealthSummary(health HPAHealth) string {
	if health.Status == StatusHealthy {
		return "OK"
	}
	// Para warning/critical, usar mensagem completa
	return health.Message
}

// validateHPA valida um HPA específico
func (c *HPAChecker) validateHPA(ctx context.Context, client kubernetes.Interface, namespace string, hpa *autoscalingv2.HorizontalPodAutoscaler, applyFilters bool) HPAHealth {
	health := HPAHealth{
		Name:            hpa.Name,
		Namespace:       namespace,
		TargetKind:      hpa.Spec.ScaleTargetRef.Kind,
		TargetName:      hpa.Spec.ScaleTargetRef.Name,
		MinReplicas:     getMinReplicas(hpa),
		MaxReplicas:     hpa.Spec.MaxReplicas,
		CurrentReplicas: hpa.Status.CurrentReplicas,
		DesiredReplicas: hpa.Status.DesiredReplicas,
		CheckedAt:       time.Now(),
		Issues:          []HPAScalingIssue{},
		Suggestions:     []string{},
	}

	// Verificar se target existe
	health.TargetExists = c.checkTargetExists(ctx, client, namespace, hpa.Spec.ScaleTargetRef)

	// Verificar min == max (não escala)
	health.IsMinEqualsMax = health.MinReplicas == health.MaxReplicas
	if health.IsMinEqualsMax {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "config",
			Description: fmt.Sprintf("HPA tem minReplicas (%d) igual a maxReplicas (%d) - nao ha scaling automatico", health.MinReplicas, health.MaxReplicas),
			Severity:    SeverityMedium,
		})
		health.Suggestions = append(health.Suggestions, "Aumentar maxReplicas para permitir scaling automatico ou remover o HPA se scaling nao for necessario")
	}

	// Verificar max muito baixo
	health.IsMaxTooLow = health.MaxReplicas < 3 && !health.IsMinEqualsMax
	if health.IsMaxTooLow {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "config",
			Description: fmt.Sprintf("HPA tem maxReplicas muito baixo (%d) - pouca flexibilidade para escalar", health.MaxReplicas),
			Severity:    SeverityLow,
		})
		health.Suggestions = append(health.Suggestions, "Considerar aumentar maxReplicas para pelo menos 3 para melhor resiliencia")
	}

	// Verificar se está no limite máximo
	health.IsAtMaxReplicas = health.CurrentReplicas == health.MaxReplicas && health.CurrentReplicas > 0
	if health.IsAtMaxReplicas {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "scaling",
			Description: fmt.Sprintf("HPA esta no limite maximo de replicas (%d/%d) - pode precisar de mais capacidade", health.CurrentReplicas, health.MaxReplicas),
			Severity:    SeverityHigh,
		})
		health.Suggestions = append(health.Suggestions, "Verificar metricas e considerar aumentar maxReplicas se carga persistir alta")
	}

	// Verificar se está no mínimo
	health.IsAtMinReplicas = health.CurrentReplicas == health.MinReplicas

	// Verificar annotations que desabilitam scaling
	health.HasScalingDisabled = c.checkScalingDisabled(hpa)
	if health.HasScalingDisabled {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "config",
			Description: "HPA tem annotations que podem desabilitar ou limitar scaling automatico",
			Severity:    SeverityMedium,
		})
		health.Suggestions = append(health.Suggestions, "Verificar annotations do HPA: cluster-autoscaler.kubernetes.io/safe-to-evict, autoscaling.alpha.kubernetes.io/paused")
	}

	// Verificar se target não existe
	if !health.TargetExists {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "target",
			Description: fmt.Sprintf("Target %s/%s nao encontrado no namespace %s - HPA orfao", health.TargetKind, health.TargetName, namespace),
			Severity:    SeverityCritical,
		})
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("Verificar se %s '%s' existe no namespace ou deletar HPA orfao", health.TargetKind, health.TargetName))
	}

	// Validar métricas configuradas
	health.Metrics = c.extractMetrics(hpa)
	health.MetricsCount = len(health.Metrics)

	// Verificar erros em métricas
	for _, metric := range health.Metrics {
		if !metric.IsHealthy {
			health.MetricsErrors++
		}
	}

	if health.MetricsCount == 0 {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "metric",
			Description: "HPA nao tem metricas configuradas - scaling nao funcionara corretamente",
			Severity:    SeverityCritical,
		})
		health.Suggestions = append(health.Suggestions, "Adicionar pelo menos uma metrica (CPU ou Memory) para o HPA funcionar")
	}

	if health.MetricsErrors > 0 {
		health.Issues = append(health.Issues, HPAScalingIssue{
			Type:        "metric",
			Description: fmt.Sprintf("HPA tem %d metrica(s) com erro - verificar se metrics-server esta funcionando", health.MetricsErrors),
			Severity:    SeverityHigh,
		})
		health.Suggestions = append(health.Suggestions, "kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/"+namespace+"/pods")
	}

	// Extrair comportamento de scaling (se definido)
	if hpa.Spec.Behavior != nil {
		if hpa.Spec.Behavior.ScaleUp != nil && hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds != nil {
			health.ScaleUpStabilization = *hpa.Spec.Behavior.ScaleUp.StabilizationWindowSeconds
		}
		if hpa.Spec.Behavior.ScaleDown != nil && hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil {
			health.ScaleDownStabilization = *hpa.Spec.Behavior.ScaleDown.StabilizationWindowSeconds
		}
	}

	// Extrair último tempo de scaling
	if hpa.Status.LastScaleTime != nil {
		health.LastScaleTime = &hpa.Status.LastScaleTime.Time
	}

	// Buscar eventos recentes de scaling
	health.RecentScalingEvents = c.getScalingEvents(ctx, client, namespace, hpa.Name)

	// Verificar eventos de falha de scaling
	for _, event := range health.RecentScalingEvents {
		if event.Type == "FailedScaling" || strings.Contains(event.Reason, "Failed") {
			health.Issues = append(health.Issues, HPAScalingIssue{
				Type:        "scaling",
				Description: fmt.Sprintf("Evento de falha de scaling recente: %s - %s", event.Reason, event.Message),
				Severity:    SeverityHigh,
			})
		}
	}

	// Determinar status geral
	health.Status = c.determineStatus(health)
	health.Message = c.generateMessage(health)

	return health
}

// getMinReplicas extrai minReplicas com fallback para 1
func getMinReplicas(hpa *autoscalingv2.HorizontalPodAutoscaler) int32 {
	if hpa.Spec.MinReplicas != nil {
		return *hpa.Spec.MinReplicas
	}
	return 1
}

// checkTargetExists verifica se o target do HPA existe
func (c *HPAChecker) checkTargetExists(ctx context.Context, client kubernetes.Interface, namespace string, ref autoscalingv2.CrossVersionObjectReference) bool {
	switch ref.Kind {
	case "Deployment":
		_, err := client.AppsV1().Deployments(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		return err == nil
	case "StatefulSet":
		_, err := client.AppsV1().StatefulSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		return err == nil
	case "ReplicaSet":
		_, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		return err == nil
	default:
		// Para outros tipos, assumir que existe (pode ser CRD)
		return true
	}
}

// checkScalingDisabled verifica se há annotations que desabilitam scaling
func (c *HPAChecker) checkScalingDisabled(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	if hpa.Annotations == nil {
		return false
	}

	disablingAnnotations := []string{
		"autoscaling.alpha.kubernetes.io/paused",
		"cluster-autoscaler.kubernetes.io/safe-to-evict",
	}

	for _, ann := range disablingAnnotations {
		if val, ok := hpa.Annotations[ann]; ok {
			if val == "true" || val == "false" { // safe-to-evict=false pode indicar problema
				return true
			}
		}
	}

	return false
}

// extractMetrics extrai informações das métricas configuradas
func (c *HPAChecker) extractMetrics(hpa *autoscalingv2.HorizontalPodAutoscaler) []HPAMetricConfig {
	var metrics []HPAMetricConfig

	for _, metric := range hpa.Spec.Metrics {
		config := HPAMetricConfig{
			Type:      string(metric.Type),
			IsHealthy: true, // Assumir saudável inicialmente
		}

		switch metric.Type {
		case autoscalingv2.ResourceMetricSourceType:
			if metric.Resource != nil {
				config.Name = string(metric.Resource.Name)
				config.TargetType = string(metric.Resource.Target.Type)
				if metric.Resource.Target.AverageUtilization != nil {
					config.TargetValue = fmt.Sprintf("%d%%", *metric.Resource.Target.AverageUtilization)
				} else if metric.Resource.Target.AverageValue != nil {
					config.TargetValue = metric.Resource.Target.AverageValue.String()
				}
			}
		case autoscalingv2.PodsMetricSourceType:
			if metric.Pods != nil {
				config.Name = metric.Pods.Metric.Name
				config.TargetType = string(metric.Pods.Target.Type)
				if metric.Pods.Target.AverageValue != nil {
					config.TargetValue = metric.Pods.Target.AverageValue.String()
				}
			}
		case autoscalingv2.ObjectMetricSourceType:
			if metric.Object != nil {
				config.Name = metric.Object.Metric.Name
				config.TargetType = string(metric.Object.Target.Type)
				if metric.Object.Target.Value != nil {
					config.TargetValue = metric.Object.Target.Value.String()
				} else if metric.Object.Target.AverageValue != nil {
					config.TargetValue = metric.Object.Target.AverageValue.String()
				}
			}
		case autoscalingv2.ExternalMetricSourceType:
			if metric.External != nil {
				config.Name = metric.External.Metric.Name
				config.TargetType = string(metric.External.Target.Type)
				if metric.External.Target.Value != nil {
					config.TargetValue = metric.External.Target.Value.String()
				} else if metric.External.Target.AverageValue != nil {
					config.TargetValue = metric.External.Target.AverageValue.String()
				}
			}
		}

		metrics = append(metrics, config)
	}

	// Verificar status das métricas a partir das condições
	for i := range metrics {
		for _, condition := range hpa.Status.Conditions {
			if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionFalse {
				metrics[i].IsHealthy = false
				metrics[i].ErrorMessage = condition.Message
			}
			if condition.Type == autoscalingv2.AbleToScale && condition.Status == corev1.ConditionFalse {
				metrics[i].IsHealthy = false
				metrics[i].ErrorMessage = condition.Message
			}
		}
	}

	// Extrair valores atuais das métricas
	for i, metricSpec := range metrics {
		for _, currentMetric := range hpa.Status.CurrentMetrics {
			if string(currentMetric.Type) == metricSpec.Type {
				switch currentMetric.Type {
				case autoscalingv2.ResourceMetricSourceType:
					if currentMetric.Resource != nil && currentMetric.Resource.Current.AverageUtilization != nil {
						metrics[i].CurrentValue = fmt.Sprintf("%d%%", *currentMetric.Resource.Current.AverageUtilization)
					}
				case autoscalingv2.PodsMetricSourceType:
					if currentMetric.Pods != nil {
						metrics[i].CurrentValue = currentMetric.Pods.Current.AverageValue.String()
					}
				}
			}
		}
	}

	return metrics
}

// getScalingEvents busca eventos recentes de scaling do HPA
func (c *HPAChecker) getScalingEvents(ctx context.Context, client kubernetes.Interface, namespace, hpaName string) []HPAScalingEvent {
	var events []HPAScalingEvent

	// Buscar eventos do HPA
	eventList, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=HorizontalPodAutoscaler", hpaName),
	})
	if err != nil {
		log.Debug().Err(err).Str("hpa", hpaName).Msg("Failed to get HPA events")
		return events
	}

	// Filtrar eventos recentes (últimas 24 horas)
	cutoff := time.Now().Add(-24 * time.Hour)

	for _, event := range eventList.Items {
		eventTime := event.LastTimestamp.Time
		if eventTime.IsZero() {
			eventTime = event.FirstTimestamp.Time
		}

		if eventTime.After(cutoff) {
			scalingEvent := HPAScalingEvent{
				Timestamp: eventTime,
				Reason:    event.Reason,
				Message:   event.Message,
			}

			// Classificar tipo de evento
			switch event.Reason {
			case "SuccessfulRescale":
				if strings.Contains(event.Message, "up") || strings.Contains(event.Message, "increased") {
					scalingEvent.Type = "ScaledUp"
				} else {
					scalingEvent.Type = "ScaledDown"
				}
			case "FailedGetResourceMetric", "FailedComputeMetricsReplicas", "FailedRescale":
				scalingEvent.Type = "FailedScaling"
			default:
				scalingEvent.Type = event.Reason
			}

			events = append(events, scalingEvent)
		}
	}

	return events
}

// determineStatus determina o status geral do HPA
func (c *HPAChecker) determineStatus(health HPAHealth) HealthStatus {
	hasCritical := false
	hasWarning := false

	for _, issue := range health.Issues {
		if issue.Severity == "critical" {
			hasCritical = true
		} else if issue.Severity == "warning" {
			hasWarning = true
		}
	}

	if hasCritical {
		return StatusCritical
	}
	if hasWarning {
		return StatusWarning
	}
	return StatusHealthy
}

// generateMessage gera a mensagem principal do HPA
func (c *HPAChecker) generateMessage(health HPAHealth) string {
	if health.Status == StatusHealthy {
		return fmt.Sprintf("OK: HPA %s/%s funcionando corretamente (%d/%d replicas, %d metricas)",
			health.Namespace, health.Name, health.CurrentReplicas, health.MaxReplicas, health.MetricsCount)
	}

	// Construir mensagem com problemas
	var problems []string
	for _, issue := range health.Issues {
		if issue.Severity == "critical" {
			problems = append(problems, fmt.Sprintf("CRITICO: %s", issue.Description))
		} else {
			problems = append(problems, fmt.Sprintf("AVISO: %s", issue.Description))
		}
	}

	if len(problems) == 0 {
		return fmt.Sprintf("HPA %s/%s com status desconhecido", health.Namespace, health.Name)
	}

	return strings.Join(problems, " | ")
}
