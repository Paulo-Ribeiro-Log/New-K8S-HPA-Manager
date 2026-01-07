package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ProgressCallback é chamado para publicar progresso de cada deployment
type ProgressCallback func(namespace, name, message string, status HealthStatus, current, total int)

// DeploymentChecker valida saúde de Deployments
type DeploymentChecker struct{}

// NewDeploymentChecker cria um novo deployment checker
func NewDeploymentChecker() *DeploymentChecker {
	return &DeploymentChecker{}
}

// CheckAll verifica todos os deployments nos namespaces especificados
func (c *DeploymentChecker) CheckAll(ctx context.Context, client kubernetes.Interface, metricsClient metricsclientset.Interface, namespaces []string, timeout int, progressCallback ProgressCallback) []DeploymentHealth {
	results := []DeploymentHealth{}

	type nsDeployments struct {
		namespace   string
		deployments []appsv1.Deployment
	}

	batches := make([]nsDeployments, 0, len(namespaces))
	totalDeployments := 0

	for _, ns := range namespaces {
		listCtx, cancel := c.withTimeout(ctx, timeout)
		deployments, err := client.AppsV1().Deployments(ns).List(listCtx, metav1.ListOptions{})

		if err != nil {
			if isTimeoutError(err, listCtx) {
				log.Warn().Err(err).Str("namespace", ns).Msg("Timeout listing deployments")
			} else {
				log.Error().Err(err).Str("namespace", ns).Msg("Failed to list deployments")
			}
			if cancel != nil {
				cancel()
			}
			continue
		}

		if cancel != nil {
			cancel()
		}

		batches = append(batches, nsDeployments{
			namespace:   ns,
			deployments: deployments.Items,
		})
		totalDeployments += len(deployments.Items)
	}

	if totalDeployments == 0 {
		return results
	}

	currentDeployment := 0

	for _, batch := range batches {
		for _, deployment := range batch.deployments {
			currentDeployment++

			if progressCallback != nil {
				progressCallback(batch.namespace, deployment.Name, fmt.Sprintf("Verificando deployment %s/%s...", batch.namespace, deployment.Name), StatusHealthy, currentDeployment, totalDeployments)
			}

			deploymentCtx, cancel := c.withTimeout(ctx, timeout)
			health := c.Check(deploymentCtx, client, metricsClient, batch.namespace, deployment.Name, timeout)
			if cancel != nil {
				cancel()
			}

			results = append(results, health)

			if progressCallback != nil {
				summary := c.getHealthSummary(health)
				progressCallback(batch.namespace, deployment.Name, fmt.Sprintf("%s/%s: %s", batch.namespace, deployment.Name, summary), health.Status, currentDeployment, totalDeployments)
			}
		}
	}

	return results
}

// getHealthSummary gera resumo compacto do health check
func (c *DeploymentChecker) getHealthSummary(health DeploymentHealth) string {
	if health.Status == StatusHealthy {
		return fmt.Sprintf("%d/%d réplicas prontas", health.ReplicasReady, health.ReplicasDesired)
	}
	// Para warning/critical, usar mensagem completa
	return health.Message
}

// Check verifica a saúde de um deployment específico
func (c *DeploymentChecker) Check(ctx context.Context, client kubernetes.Interface, metricsClient metricsclientset.Interface, namespace, name string, timeout int) DeploymentHealth {
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		status := StatusCritical
		message := fmt.Sprintf("Falha ao buscar deployment: %v", err)

		if isTimeoutError(err, ctx) {
			status = StatusWarning
			message = "Timeout ao obter manifesto do deployment"
		}

		suggestions := []string{"Verificar conectividade com a API Kubernetes"}
		if timeout > 0 {
			suggestions = append(suggestions, fmt.Sprintf("Aumentar timeout acima de %ds se necessário", timeout))
		}

		return DeploymentHealth{
			Name:        name,
			Namespace:   namespace,
			Status:      status,
			Message:     message,
			Suggestions: suggestions,
			CheckedAt:   time.Now(),
		}
	}

	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	} else {
		log.Warn().Str("namespace", namespace).Str("deployment", name).Msg("Deployment sem replicas explícitas; assumindo padrão 1")
	}

	health := DeploymentHealth{
		Name:            name,
		Namespace:       namespace,
		ReplicasReady:   deployment.Status.ReadyReplicas,
		ReplicasDesired: desiredReplicas,
		CheckedAt:       time.Now(),
		Suggestions:     []string{},
	}

	// 1. Verificar réplicas
	if health.ReplicasReady == 0 && health.ReplicasDesired > 0 {
		health.Status = StatusCritical
		health.Message = "Nenhuma réplica está pronta"
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe deployment %s -n %s", name, namespace))
		health.Suggestions = append(health.Suggestions, "Verificar eventos do deployment")
		return health
	}

	if health.ReplicasReady < health.ReplicasDesired {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Apenas %d/%d réplicas prontas", health.ReplicasReady, health.ReplicasDesired)
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl get pods -n %s -l app=%s", namespace, name))
		health.Suggestions = append(health.Suggestions, "Verificar logs dos pods")
	}

	// 2. Buscar pods do deployment usando label selector
	selector := fmt.Sprintf("app=%s", name)

	if deployment.Spec.Selector != nil {
		compiledSelector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			log.Error().Err(err).Str("namespace", namespace).Str("deployment", name).Msg("Failed to compile label selector for deployment")
		} else if !compiledSelector.Empty() {
			selector = compiledSelector.String()
		}
	}

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})

	if err != nil {
		if isTimeoutError(err, ctx) {
			log.Warn().Err(err).Str("namespace", namespace).Str("deployment", name).Msg("Timeout listing pods for deployment")
			if health.Status == "" || health.Status == StatusHealthy {
				health.Status = StatusWarning
				health.Message = "Timeout ao listar pods do deployment"
			}
			health.Suggestions = appendSuggestionOnce(health.Suggestions, "Reexecutar análise ou aumentar o timeout configurado")
		} else {
			log.Error().Err(err).Str("namespace", namespace).Str("deployment", name).Msg("Failed to list pods")
		}
	} else {
		// 3. Contar containers em crash e image pull errors
		crashCount := 0
		imagePullErrors := 0

		for _, pod := range pods.Items {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					reason := cs.State.Waiting.Reason

					if reason == "CrashLoopBackOff" || reason == "Error" {
						crashCount++
					}

					if reason == "ImagePullBackOff" || reason == "ErrImagePull" {
						imagePullErrors++
					}
				}
			}
		}

		health.ContainersCrash = int32(crashCount)
		health.ImagePullErrors = int32(imagePullErrors)

		// 4. Atualizar status baseado em problemas encontrados
		if crashCount > 0 {
			health.Status = StatusCritical
			health.Message = fmt.Sprintf("%d containers em CrashLoopBackOff", crashCount)
			health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl logs <pod-name> -n %s --previous", namespace))
			health.Suggestions = append(health.Suggestions, "Analisar logs anteriores ao crash")
			return health
		}

		if imagePullErrors > 0 {
			health.Status = StatusCritical
			health.Message = fmt.Sprintf("%d erros ao puxar imagens", imagePullErrors)
			health.Suggestions = append(health.Suggestions, "Verificar se a imagem existe no registry")
			health.Suggestions = append(health.Suggestions, "Validar ImagePullSecrets")
			return health
		}

		// 5. Analisar Liveness e Readiness Probes
		c.analyzeProbes(deployment, pods.Items, &health)
		c.enrichWithMetrics(ctx, metricsClient, deployment, selector, timeout, &health)
	}

	// 6. Verificar condições do deployment
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == "Progressing" && condition.Status == "False" {
			health.Status = StatusWarning
			health.Message = fmt.Sprintf("Deployment não está progredindo: %s", condition.Reason)
			health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe deployment %s -n %s", name, namespace))
			return health
		}

		if condition.Type == "Available" && condition.Status == "False" {
			health.Status = StatusCritical
			health.Message = fmt.Sprintf("Deployment não está disponível: %s", condition.Reason)
			health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe deployment %s -n %s", name, namespace))
			return health
		}
	}

	// 7. Se nenhum problema detectado
	if health.Status == "" {
		health.Status = StatusHealthy
		health.Message = "Deployment saudável"
	}

	return health
}

// analyzeProbes verifica se deployment tem probes configurados e se estão falhando
func (c *DeploymentChecker) analyzeProbes(deployment *appsv1.Deployment, pods []corev1.Pod, health *DeploymentHealth) {
	// Verificar se deployment tem probes configurados
	hasLiveness := false
	hasReadiness := false

	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.LivenessProbe != nil {
			hasLiveness = true
		}
		if container.ReadinessProbe != nil {
			hasReadiness = true
		}
	}

	health.HasLivenessProbe = hasLiveness
	health.HasReadinessProbe = hasReadiness

	// Contar falhas de probes nos pods
	livenessFailures := 0
	readinessFailures := 0

	for _, pod := range pods {
		// Verificar condições do pod para detectar falhas de probe
		for _, condition := range pod.Status.Conditions {
			// Ready=False pode indicar readiness probe falhando
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionFalse {
				if condition.Reason == "ContainersNotReady" {
					readinessFailures++
				}
			}
		}

		// Verificar container statuses para restarts causados por liveness probe
		for _, cs := range pod.Status.ContainerStatuses {
			// RestartCount alto pode indicar liveness probe matando container
			if cs.RestartCount > 0 {
				// Se container está em CrashLoopBackOff, já foi detectado antes
				// Aqui detectamos restarts por liveness probe (container saudável mas restart > 0)
				if cs.State.Running != nil || cs.State.Waiting != nil {
					// Verificar se último término foi por unhealthy (liveness probe)
					if cs.LastTerminationState.Terminated != nil {
						if cs.LastTerminationState.Terminated.Reason == "Error" && cs.LastTerminationState.Terminated.ExitCode == 137 {
							// Exit code 137 = SIGKILL (típico de liveness probe matando container)
							livenessFailures++
						}
					}
				}
			}
		}
	}

	health.LivenessProbeFailures = int32(livenessFailures)
	health.ReadinessProbeFailures = int32(readinessFailures)

	// Adicionar avisos se probes não estão configurados
	if !hasLiveness && health.Status != StatusCritical {
		if health.Status == "" {
			health.Status = StatusWarning
		}
		health.Message = "Liveness probe não configurado"
		health.Suggestions = append(health.Suggestions, "Configurar liveness probe para detectar containers travados")
		health.Suggestions = append(health.Suggestions, "Exemplo: livenessProbe com httpGet, tcpSocket ou exec")
	}

	if !hasReadiness && health.Status != StatusCritical {
		if health.Status == "" {
			health.Status = StatusWarning
		}
		if health.Message == "Liveness probe não configurado" {
			health.Message = "Liveness e readiness probes não configurados"
		} else {
			health.Message = "Readiness probe não configurado"
		}
		health.Suggestions = append(health.Suggestions, "Configurar readiness probe para controlar tráfego")
		health.Suggestions = append(health.Suggestions, "Exemplo: readinessProbe com httpGet para validar app inicializada")
	}

	// Adicionar avisos se há falhas de probes
	if readinessFailures > 0 && health.Status != StatusCritical {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Readiness probe falhando em %d pods", readinessFailures)
		health.Suggestions = append(health.Suggestions, "Verificar logs dos pods para entender falhas de readiness")
		health.Suggestions = append(health.Suggestions, fmt.Sprintf("kubectl describe pod -n %s -l app=%s", health.Namespace, health.Name))
	}

	if livenessFailures > 0 && health.Status != StatusCritical {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Liveness probe causando restarts em %d containers", livenessFailures)
		health.Suggestions = append(health.Suggestions, "Liveness probe pode estar muito agressivo (timeout/threshold)")
		health.Suggestions = append(health.Suggestions, "Verificar se aplicação responde dentro do timeout configurado")
	}
}

func (c *DeploymentChecker) enrichWithMetrics(ctx context.Context, metricsClient metricsclientset.Interface, deployment *appsv1.Deployment, selector string, timeout int, health *DeploymentHealth) {
	if metricsClient == nil {
		return
	}

	metricsCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		duration := 5 * time.Second
		if timeout > 0 {
			duration = time.Duration(timeout) * time.Second
		}
		metricsCtx, cancel = context.WithTimeout(ctx, duration)
	}
	if cancel != nil {
		defer cancel()
	}

	podMetrics, err := metricsClient.MetricsV1beta1().PodMetricses(deployment.Namespace).List(metricsCtx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		if metricsCtx.Err() == context.DeadlineExceeded {
			log.Warn().Str("namespace", deployment.Namespace).Str("deployment", deployment.Name).Msg("Timeout ao buscar métricas de pods para deployment")
		} else {
			log.Warn().Err(err).Str("namespace", deployment.Namespace).Str("deployment", deployment.Name).Msg("Falha ao buscar métricas de pods para deployment")
		}
		return
	}

	if len(podMetrics.Items) == 0 {
		return
	}

	containerResources := make(map[string]corev1.ResourceRequirements, len(deployment.Spec.Template.Spec.Containers))
	for _, container := range deployment.Spec.Template.Spec.Containers {
		containerResources[container.Name] = container.Resources
	}

	var usedCPUMilli, baseCPUMilli int64
	var usedMemoryBytes, baseMemoryBytes int64

	for _, podMetric := range podMetrics.Items {
		for _, containerMetric := range podMetric.Containers {
			usage := containerMetric.Usage
			usedCPUMilli += usage.Cpu().MilliValue()
			usedMemoryBytes += usage.Memory().Value()

			if resources, ok := containerResources[containerMetric.Name]; ok {
				var cpuBase int64
				var memBase int64

				if resources.Requests != nil {
					cpuBase = resources.Requests.Cpu().MilliValue()
					memBase = resources.Requests.Memory().Value()
				}

				if cpuBase == 0 && resources.Limits != nil {
					cpuBase = resources.Limits.Cpu().MilliValue()
				}

				if memBase == 0 && resources.Limits != nil {
					memBase = resources.Limits.Memory().Value()
				}

				baseCPUMilli += cpuBase
				baseMemoryBytes += memBase
			}
		}
	}

	if baseCPUMilli > 0 {
		cpuPercent := (float64(usedCPUMilli) / float64(baseCPUMilli)) * 100
		health.CPUUsagePercent = math.Round(cpuPercent*10) / 10
	} else if usedCPUMilli > 0 {
		health.Suggestions = appendSuggestionOnce(health.Suggestions, "Definir requests/limits de CPU para visibilidade de uso")
	}

	if baseMemoryBytes > 0 {
		memPercent := (float64(usedMemoryBytes) / float64(baseMemoryBytes)) * 100
		health.MemoryUsagePercent = math.Round(memPercent*10) / 10
	} else if usedMemoryBytes > 0 {
		health.Suggestions = appendSuggestionOnce(health.Suggestions, "Definir requests/limits de memória para visibilidade de uso")
	}
}

func appendSuggestionOnce(list []string, suggestion string) []string {
	for _, existing := range list {
		if existing == suggestion {
			return list
		}
	}
	return append(list, suggestion)
}

func (c *DeploymentChecker) withTimeout(ctx context.Context, timeoutSec int) (context.Context, context.CancelFunc) {
	if timeoutSec <= 0 {
		return ctx, nil
	}

	timeoutDuration := time.Duration(timeoutSec) * time.Second

	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining <= timeoutDuration {
			return ctx, nil
		}
	}

	return context.WithTimeout(ctx, timeoutDuration)
}

func isTimeoutError(err error, ctx context.Context) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return true
	}

	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
		return true
	}

	return false
}
