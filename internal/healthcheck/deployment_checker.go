package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"k8s-hpa-manager/internal/storage"

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

// Circuit Breaker constants
const (
	// MetricsCircuitBreakerThreshold define quantas falhas consecutivas abrem o circuit breaker
	MetricsCircuitBreakerThreshold = 3
)

// DeploymentChecker valida saúde de Deployments
type DeploymentChecker struct {
	// Circuit Breaker para métricas (thread-safe)
	metricsMu          sync.RWMutex // Protege acesso ao circuit breaker
	metricsFailCount   int          // Contador de falhas consecutivas
	metricsCircuitOpen bool         // Se true, pula tentativas de métricas
}

// NewDeploymentChecker cria um novo deployment checker
func NewDeploymentChecker() *DeploymentChecker {
	return &DeploymentChecker{
		metricsFailCount:   0,
		metricsCircuitOpen: false,
	}
}

// CheckAll verifica todos os deployments nos namespaces especificados
// clusterName é usado para popular a base de conhecimento de deployments
func (c *DeploymentChecker) CheckAll(ctx context.Context, client kubernetes.Interface, metricsClient metricsclientset.Interface, namespaces []string, timeout int, clusterName string, registry *storage.DeploymentRegistry, progressCallback ProgressCallback) []DeploymentHealth {
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
			health := c.Check(deploymentCtx, client, metricsClient, batch.namespace, deployment.Name, timeout, clusterName, registry)
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
// clusterName e registry são usados para popular a base de conhecimento
func (c *DeploymentChecker) Check(ctx context.Context, client kubernetes.Interface, metricsClient metricsclientset.Interface, namespace, name string, timeout int, clusterName string, registry *storage.DeploymentRegistry) DeploymentHealth {
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

	// ✅ NOVO: Popular base de conhecimento de deployments
	if registry != nil && clusterName != "" {
		c.populateDeploymentRegistry(deployment, clusterName, registry)
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

		// 6. Analisar Resources (requests/limits)
		c.analyzeResources(deployment, &health)

		c.enrichWithMetrics(ctx, metricsClient, deployment, selector, timeout, &health)
	}

	// 7. Verificar condições do deployment
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

	// 8. Se nenhum problema detectado
	if health.Status == "" {
		health.Status = StatusHealthy
		health.Message = "Deployment saudável"
	}

	return health
}

// Constantes para validação de probes
const (
	// Timeouts mínimos recomendados (segundos)
	MinLivenessTimeout  = 3
	MinReadinessTimeout = 2
	MinStartupTimeout   = 3

	// InitialDelaySeconds recomendado para apps lentas
	MinInitialDelay = 5
	MaxInitialDelay = 120

	// FailureThreshold mínimo
	MinFailureThreshold = 3
)

// analyzeProbes verifica se deployment tem probes configurados, valida configurações e detecta falhas
func (c *DeploymentChecker) analyzeProbes(deployment *appsv1.Deployment, pods []corev1.Pod, health *DeploymentHealth) {
	// Verificar se deployment tem probes configurados
	hasLiveness := false
	hasReadiness := false
	hasStartup := false

	probeIssues := []ProbeIssue{}

	for _, container := range deployment.Spec.Template.Spec.Containers {
		// Liveness Probe
		if container.LivenessProbe != nil {
			hasLiveness = true
			issues := c.validateProbeConfig(container.Name, "liveness", container.LivenessProbe)
			probeIssues = append(probeIssues, issues...)
		}

		// Readiness Probe
		if container.ReadinessProbe != nil {
			hasReadiness = true
			issues := c.validateProbeConfig(container.Name, "readiness", container.ReadinessProbe)
			probeIssues = append(probeIssues, issues...)
		}

		// Startup Probe
		if container.StartupProbe != nil {
			hasStartup = true
			issues := c.validateProbeConfig(container.Name, "startup", container.StartupProbe)
			probeIssues = append(probeIssues, issues...)
		}
	}

	health.HasLivenessProbe = hasLiveness
	health.HasReadinessProbe = hasReadiness
	health.HasStartupProbe = hasStartup
	health.ProbeIssues = probeIssues

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

	// Adicionar avisos para problemas de configuração de probes
	if len(probeIssues) > 0 && health.Status != StatusCritical {
		if health.Status == "" {
			health.Status = StatusWarning
		}
		// Contar problemas críticos
		criticalCount := 0
		for _, issue := range probeIssues {
			if issue.Severity == "critical" {
				criticalCount++
			}
		}
		if criticalCount > 0 {
			health.Message = fmt.Sprintf("%d problemas críticos na configuração de probes", criticalCount)
		} else if health.Message == "" {
			health.Message = fmt.Sprintf("%d avisos na configuração de probes", len(probeIssues))
		}
	}
}

// validateProbeConfig valida a configuração de um probe e retorna lista de problemas
func (c *DeploymentChecker) validateProbeConfig(containerName, probeType string, probe *corev1.Probe) []ProbeIssue {
	issues := []ProbeIssue{}

	// Validar TimeoutSeconds
	minTimeout := MinReadinessTimeout
	if probeType == "liveness" {
		minTimeout = MinLivenessTimeout
	} else if probeType == "startup" {
		minTimeout = MinStartupTimeout
	}

	if probe.TimeoutSeconds > 0 && probe.TimeoutSeconds < int32(minTimeout) {
		issues = append(issues, ProbeIssue{
			Container: containerName,
			ProbeType: probeType,
			Issue:     fmt.Sprintf("timeoutSeconds=%d muito curto (mínimo recomendado: %d)", probe.TimeoutSeconds, minTimeout),
			Severity:  "warning",
		})
	}

	// Validar InitialDelaySeconds para liveness (pode causar restarts prematuros)
	if probeType == "liveness" {
		if probe.InitialDelaySeconds == 0 {
			issues = append(issues, ProbeIssue{
				Container: containerName,
				ProbeType: probeType,
				Issue:     "initialDelaySeconds=0 pode causar restarts durante inicialização lenta",
				Severity:  "warning",
			})
		} else if probe.InitialDelaySeconds < int32(MinInitialDelay) {
			issues = append(issues, ProbeIssue{
				Container: containerName,
				ProbeType: probeType,
				Issue:     fmt.Sprintf("initialDelaySeconds=%d pode ser insuficiente para apps lentas", probe.InitialDelaySeconds),
				Severity:  "warning",
			})
		}
	}

	// Validar FailureThreshold (muito baixo causa restarts desnecessários)
	if probe.FailureThreshold > 0 && probe.FailureThreshold < int32(MinFailureThreshold) {
		issues = append(issues, ProbeIssue{
			Container: containerName,
			ProbeType: probeType,
			Issue:     fmt.Sprintf("failureThreshold=%d muito baixo (mínimo recomendado: %d)", probe.FailureThreshold, MinFailureThreshold),
			Severity:  "warning",
		})
	}

	// Validar se liveness probe é igual ao readiness (anti-pattern)
	// Isso é validado a nível de deployment, não aqui

	// Validar PeriodSeconds muito baixo (pode sobrecarregar endpoints)
	if probe.PeriodSeconds > 0 && probe.PeriodSeconds < 5 {
		issues = append(issues, ProbeIssue{
			Container: containerName,
			ProbeType: probeType,
			Issue:     fmt.Sprintf("periodSeconds=%d muito frequente (pode sobrecarregar endpoints)", probe.PeriodSeconds),
			Severity:  "warning",
		})
	}

	return issues
}

// analyzeResources verifica configuração de requests/limits e determina QoS class
func (c *DeploymentChecker) analyzeResources(deployment *appsv1.Deployment, health *DeploymentHealth) {
	containerResources := []ContainerResources{}
	resourceIssues := []ResourceIssue{}

	// Contadores para determinar QoS class
	totalContainers := len(deployment.Spec.Template.Spec.Containers)
	containersWithAllRequestsAndLimits := 0
	containersWithAnyRequestOrLimit := 0

	for _, container := range deployment.Spec.Template.Spec.Containers {
		cr := ContainerResources{
			Name: container.Name,
		}

		// Verificar CPU Request
		if container.Resources.Requests != nil {
			if cpuReq := container.Resources.Requests.Cpu(); cpuReq != nil && !cpuReq.IsZero() {
				cr.HasCPURequest = true
				cr.CPURequest = cpuReq.String()
			}
			if memReq := container.Resources.Requests.Memory(); memReq != nil && !memReq.IsZero() {
				cr.HasMemoryRequest = true
				cr.MemoryRequest = memReq.String()
			}
		}

		// Verificar CPU/Memory Limits
		if container.Resources.Limits != nil {
			if cpuLim := container.Resources.Limits.Cpu(); cpuLim != nil && !cpuLim.IsZero() {
				cr.HasCPULimit = true
				cr.CPULimit = cpuLim.String()
			}
			if memLim := container.Resources.Limits.Memory(); memLim != nil && !memLim.IsZero() {
				cr.HasMemoryLimit = true
				cr.MemoryLimit = memLim.String()
			}
		}

		// Verificar se tem todos os requests e limits
		hasAllRequestsAndLimits := cr.HasCPURequest && cr.HasMemoryRequest && cr.HasCPULimit && cr.HasMemoryLimit
		hasAnyRequestOrLimit := cr.HasCPURequest || cr.HasMemoryRequest || cr.HasCPULimit || cr.HasMemoryLimit

		if hasAllRequestsAndLimits {
			containersWithAllRequestsAndLimits++
		}
		if hasAnyRequestOrLimit {
			containersWithAnyRequestOrLimit++
		}

		// Gerar issues para recursos faltando
		if !cr.HasCPURequest {
			resourceIssues = append(resourceIssues, ResourceIssue{
				Container:    container.Name,
				ResourceType: "cpu",
				Issue:        "CPU request não definido",
				Severity:     "warning",
			})
		}
		if !cr.HasMemoryRequest {
			resourceIssues = append(resourceIssues, ResourceIssue{
				Container:    container.Name,
				ResourceType: "memory",
				Issue:        "Memory request não definido",
				Severity:     "warning",
			})
		}
		if !cr.HasCPULimit {
			resourceIssues = append(resourceIssues, ResourceIssue{
				Container:    container.Name,
				ResourceType: "cpu",
				Issue:        "CPU limit não definido (pode consumir recursos ilimitados)",
				Severity:     "warning",
			})
		}
		if !cr.HasMemoryLimit {
			resourceIssues = append(resourceIssues, ResourceIssue{
				Container:    container.Name,
				ResourceType: "memory",
				Issue:        "Memory limit não definido (risco de OOMKill do node)",
				Severity:     "critical",
			})
		}

		// Verificar se requests == limits (Guaranteed QoS para este container)
		if cr.HasCPURequest && cr.HasCPULimit && cr.CPURequest != cr.CPULimit {
			// Não é um problema, mas pode ser útil saber
		}

		containerResources = append(containerResources, cr)
	}

	// Determinar QoS Class
	var qosClass QoSClass
	if containersWithAllRequestsAndLimits == totalContainers && c.allRequestsEqualLimits(deployment) {
		qosClass = QoSGuaranteed
	} else if containersWithAnyRequestOrLimit > 0 {
		qosClass = QoSBurstable
	} else {
		qosClass = QoSBestEffort
	}

	health.QoSClass = qosClass
	health.ContainerResources = containerResources
	health.ResourceIssues = resourceIssues

	// Adicionar avisos baseados no QoS class
	if qosClass == QoSBestEffort && health.Status != StatusCritical {
		if health.Status == "" || health.Status == StatusHealthy {
			health.Status = StatusWarning
		}
		health.Message = "QoS BestEffort: sem requests/limits definidos"
		health.Suggestions = append(health.Suggestions, "Definir resources.requests e resources.limits para todos os containers")
		health.Suggestions = append(health.Suggestions, "QoS BestEffort tem menor prioridade e será evicted primeiro sob pressão")
	}

	// Contar problemas críticos de recursos
	criticalResourceIssues := 0
	for _, issue := range resourceIssues {
		if issue.Severity == "critical" {
			criticalResourceIssues++
		}
	}

	if criticalResourceIssues > 0 && health.Status != StatusCritical {
		health.Status = StatusWarning
		if health.Message == "" || health.Message == "Deployment saudável" {
			health.Message = fmt.Sprintf("%d problemas críticos de recursos", criticalResourceIssues)
		}
	}
}

// allRequestsEqualLimits verifica se todos os containers têm requests == limits (para QoS Guaranteed)
func (c *DeploymentChecker) allRequestsEqualLimits(deployment *appsv1.Deployment) bool {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Resources.Requests == nil || container.Resources.Limits == nil {
			return false
		}

		cpuReq := container.Resources.Requests.Cpu()
		cpuLim := container.Resources.Limits.Cpu()
		memReq := container.Resources.Requests.Memory()
		memLim := container.Resources.Limits.Memory()

		if cpuReq == nil || cpuLim == nil || memReq == nil || memLim == nil {
			return false
		}

		if !cpuReq.Equal(*cpuLim) || !memReq.Equal(*memLim) {
			return false
		}
	}
	return true
}

func (c *DeploymentChecker) enrichWithMetrics(ctx context.Context, metricsClient metricsclientset.Interface, deployment *appsv1.Deployment, selector string, timeout int, health *DeploymentHealth) {
	if metricsClient == nil {
		return
	}

	// Circuit Breaker: verificar se aberto (thread-safe)
	c.metricsMu.RLock()
	isOpen := c.metricsCircuitOpen
	c.metricsMu.RUnlock()

	if isOpen {
		log.Debug().
			Str("namespace", deployment.Namespace).
			Str("deployment", deployment.Name).
			Msg("Circuit breaker aberto - pulando coleta de métricas")
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
		// Circuit Breaker: incrementa contador de falhas (thread-safe)
		c.metricsMu.Lock()
		c.metricsFailCount++
		failCount := c.metricsFailCount

		if metricsCtx.Err() == context.DeadlineExceeded {
			log.Warn().
				Str("namespace", deployment.Namespace).
				Str("deployment", deployment.Name).
				Int("fail_count", failCount).
				Msg("Timeout ao buscar métricas de pods para deployment")
		} else {
			log.Warn().
				Err(err).
				Str("namespace", deployment.Namespace).
				Str("deployment", deployment.Name).
				Int("fail_count", failCount).
				Msg("Falha ao buscar métricas de pods para deployment")
		}

		// Circuit Breaker: abre após N falhas consecutivas
		if failCount >= MetricsCircuitBreakerThreshold && !c.metricsCircuitOpen {
			c.metricsCircuitOpen = true
			log.Warn().
				Int("threshold", MetricsCircuitBreakerThreshold).
				Msg("Circuit breaker ABERTO - métricas desabilitadas para esta sessão de health check")
		}
		c.metricsMu.Unlock()
		return
	}

	// Circuit Breaker: sucesso - reseta contador de falhas (thread-safe)
	c.metricsMu.Lock()
	if c.metricsFailCount > 0 {
		log.Debug().
			Int("previous_fail_count", c.metricsFailCount).
			Msg("Métricas coletadas com sucesso - resetando contador de falhas")
		c.metricsFailCount = 0
	}
	c.metricsMu.Unlock()

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

// IsMetricsCircuitOpen retorna true se o circuit breaker de métricas está aberto (thread-safe)
func (c *DeploymentChecker) IsMetricsCircuitOpen() bool {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()
	return c.metricsCircuitOpen
}

// GetMetricsFailCount retorna o número de falhas consecutivas de métricas (thread-safe)
func (c *DeploymentChecker) GetMetricsFailCount() int {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()
	return c.metricsFailCount
}

// ResetMetricsCircuitBreaker reseta o circuit breaker de métricas (thread-safe, para nova sessão)
func (c *DeploymentChecker) ResetMetricsCircuitBreaker() {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	c.metricsFailCount = 0
	c.metricsCircuitOpen = false
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

// populateDeploymentRegistry extrai informações do deployment e popula a base de conhecimento
func (c *DeploymentChecker) populateDeploymentRegistry(deployment *appsv1.Deployment, clusterName string, registry *storage.DeploymentRegistry) {
	// Extrair versão de labels
	version := deployment.Labels["app.kubernetes.io/version"]

	// Extrair app name de labels
	appName := deployment.Labels["app.kubernetes.io/name"]
	if appName == "" {
		appName = deployment.Labels["app"]
	}
	if appName == "" {
		appName = deployment.Name // Fallback para nome do deployment
	}

	// Extrair squad do label devops.k8s.io/squad (busca em labels e annotations)
	squad := deployment.Labels["devops.k8s.io/squad"]
	if squad == "" && deployment.Annotations != nil {
		squad = deployment.Annotations["devops.k8s.io/squad"]
	}

	// Extrair ServiceNow task number (busca em labels e annotations)
	// Formato esperado: "CHG0001234" ou apenas "0001234"
	servicenowTask := deployment.Labels["devops.k8s.io/servicenow-task-number"]
	if servicenowTask == "" && deployment.Annotations != nil {
		servicenowTask = deployment.Annotations["devops.k8s.io/servicenow-task-number"]
	}
	// Garantir prefixo CHG se não estiver presente
	if servicenowTask != "" && !strings.HasPrefix(servicenowTask, "CHG") {
		servicenowTask = "CHG" + servicenowTask
	}

	// Extrair imagem e tag do primeiro container
	var fullImage, imageTag string
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		fullImage = deployment.Spec.Template.Spec.Containers[0].Image
		imageTag = storage.ExtractVersionFromImage(fullImage)

		// Se não tem version label, usar tag da imagem
		if version == "" {
			version = imageTag
		}
	}

	// Determinar status de saúde básico
	status := "healthy"
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	readyReplicas := deployment.Status.ReadyReplicas

	if readyReplicas == 0 && desiredReplicas > 0 {
		status = "critical"
	} else if readyReplicas < desiredReplicas {
		status = "warning"
	}

	// Criar registro
	record := storage.DeploymentRecord{
		DeploymentName:  deployment.Name,
		Namespace:       deployment.Namespace,
		Cluster:         clusterName,
		Version:         version,
		ImageTag:        imageTag,
		FullImage:       fullImage,
		ReplicasCurrent: int(readyReplicas),
		ReplicasDesired: int(desiredReplicas),
		AppName:         appName,
		Status:          status,
		Squad:           squad,
		ServiceNowTask:  servicenowTask,
		CreatedAt:       deployment.CreationTimestamp.Time, // ✅ Data real de criação do deployment do Kubernetes
	}

	// Upsert na base (ignora erros para não quebrar health check)
	if err := registry.UpsertDeployment(record); err != nil {
		log.Warn().
			Err(err).
			Str("cluster", clusterName).
			Str("namespace", deployment.Namespace).
			Str("deployment", deployment.Name).
			Msg("Failed to update deployment registry")
	} else {
		log.Debug().
			Str("cluster", clusterName).
			Str("namespace", deployment.Namespace).
			Str("deployment", deployment.Name).
			Str("version", version).
			Msg("Updated deployment registry")
	}
}
