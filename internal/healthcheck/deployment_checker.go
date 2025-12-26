package healthcheck

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DeploymentChecker valida saúde de Deployments
type DeploymentChecker struct{}

// NewDeploymentChecker cria um novo deployment checker
func NewDeploymentChecker() *DeploymentChecker {
	return &DeploymentChecker{}
}

// CheckAll verifica todos os deployments nos namespaces especificados
func (c *DeploymentChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int) []DeploymentHealth {
	results := []DeploymentHealth{}

	for _, ns := range namespaces {
		deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Error().Err(err).Str("namespace", ns).Msg("Failed to list deployments")
			continue
		}

		for _, d := range deployments.Items {
			health := c.Check(ctx, client, ns, d.Name, timeout)
			results = append(results, health)
		}
	}

	return results
}

// Check verifica a saúde de um deployment específico
func (c *DeploymentChecker) Check(ctx context.Context, client kubernetes.Interface, namespace, name string, timeout int) DeploymentHealth {
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentHealth{
			Name:      name,
			Namespace: namespace,
			Status:    StatusCritical,
			Message:   fmt.Sprintf("Failed to get deployment: %v", err),
			CheckedAt: time.Now(),
		}
	}

	health := DeploymentHealth{
		Name:            name,
		Namespace:       namespace,
		ReplicasReady:   deployment.Status.ReadyReplicas,
		ReplicasDesired: *deployment.Spec.Replicas,
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
	var selector string
	if deployment.Spec.Selector != nil && deployment.Spec.Selector.MatchLabels != nil && len(deployment.Spec.Selector.MatchLabels) > 0 {
		// Construir selector a partir de todas as labels
		labels := []string{}
		for k, v := range deployment.Spec.Selector.MatchLabels {
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		}
		// Juntar todas as labels com vírgula (AND lógico no Kubernetes)
		selector = labels[0]
	} else {
		// Fallback: usar nome do deployment
		selector = fmt.Sprintf("app=%s", name)
	}

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})

	if err != nil {
		log.Error().Err(err).Str("namespace", namespace).Str("deployment", name).Msg("Failed to list pods")
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
