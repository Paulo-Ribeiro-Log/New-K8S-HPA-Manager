package healthcheck

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
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
	}

	// 5. Verificar condições do deployment
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

	// 6. Se nenhum problema detectado
	if health.Status == "" {
		health.Status = StatusHealthy
		health.Message = "Deployment saudável"
	}

	return health
}
