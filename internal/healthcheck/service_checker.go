package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ServiceChecker verifica conectividade de serviços via Jobs temporários no cluster
type ServiceChecker struct {
	// Configurações de segurança
	jobTTLSeconds       int32 // TTL para auto-delete do Job (default: 60)
	activeDeadlineSecs  int64 // Timeout máximo do Job (default: 30)
	backoffLimit        int32 // Tentativas antes de falhar (default: 0)
	testTimeoutSeconds  int   // Timeout do teste nc (default: 5)
}

// NewServiceChecker cria um novo service checker com configurações seguras
func NewServiceChecker() *ServiceChecker {
	return &ServiceChecker{
		jobTTLSeconds:      60,  // Job deletado 60s após completar
		activeDeadlineSecs: 30,  // Job cancelado após 30s
		backoffLimit:       0,   // Sem retries (falha rápida)
		testTimeoutSeconds: 5,   // nc timeout de 5s
	}
}

// serviceTarget representa um serviço a ser testado
type serviceTarget struct {
	name      string
	namespace string
	host      string
	port      int32
}

// CheckAll verifica conectividade de todos os serviços nos namespaces especificados
func (sc *ServiceChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, progressCallback ProgressCallback) []ServiceHealth {
	results := []ServiceHealth{}

	// 1. Descobrir serviços (apenas Services do K8s - KISS)
	targets := sc.discoverServices(ctx, client, namespaces)

	if len(targets) == 0 {
		log.Info().Msg("[ServiceChecker] Nenhum serviço descoberto para testar")
		return results
	}

	log.Info().Int("total", len(targets)).Msg("[ServiceChecker] Serviços descobertos")

	// 2. Testar cada serviço via Job
	for i, target := range targets {
		if progressCallback != nil {
			progressCallback(target.namespace, target.name,
				fmt.Sprintf("Testando conectividade %s:%d...", target.host, target.port),
				StatusHealthy, i+1, len(targets))
		}

		health := sc.testServiceViaJob(ctx, client, target, timeout)
		results = append(results, health)

		if progressCallback != nil {
			msg := health.Message
			if len(msg) > 80 {
				msg = msg[:80] + "..."
			}
			progressCallback(target.namespace, target.name, msg, health.Status, i+1, len(targets))
		}
	}

	return results
}

// discoverServices descobre serviços K8s nos namespaces (exceto kube-system, etc)
func (sc *ServiceChecker) discoverServices(ctx context.Context, client kubernetes.Interface, namespaces []string) []serviceTarget {
	targets := []serviceTarget{}

	// Namespaces de sistema a ignorar
	systemNS := map[string]bool{
		"kube-system":     true,
		"kube-public":     true,
		"kube-node-lease": true,
		"istio-system":    true,
		"cert-manager":    true,
		"ingress-nginx":   true,
	}

	for _, ns := range namespaces {
		if systemNS[ns] {
			continue
		}

		services, err := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Warn().Err(err).Str("namespace", ns).Msg("[ServiceChecker] Erro ao listar services")
			continue
		}

		for _, svc := range services.Items {
			// Ignorar services sem ClusterIP (headless, ExternalName, etc)
			if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == "None" {
				continue
			}

			// Ignorar kubernetes default service
			if svc.Name == "kubernetes" {
				continue
			}

			// Pegar primeira porta disponível
			for _, port := range svc.Spec.Ports {
				targets = append(targets, serviceTarget{
					name:      svc.Name,
					namespace: ns,
					host:      fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, ns),
					port:      port.Port,
				})
				break // Apenas primeira porta (KISS)
			}
		}
	}

	return targets
}

// testServiceViaJob cria um Job temporário para testar conectividade
func (sc *ServiceChecker) testServiceViaJob(ctx context.Context, client kubernetes.Interface, target serviceTarget, timeout int) ServiceHealth {
	health := ServiceHealth{
		Name:        target.name,
		Namespace:   target.namespace,
		ServiceType: sc.detectServiceType(target.port),
		Status:      StatusUnknown,
		CheckedAt:   time.Now(),
		Suggestions: []string{},
	}

	// Gerar nome único para o Job
	jobName := fmt.Sprintf("hc-%s-%d", target.name, time.Now().Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63] // K8s limit
	}

	// Comando simples: nc -zv -w5 host port
	cmd := fmt.Sprintf("nc -zv -w%d %s %d 2>&1 && echo SUCCESS || echo FAILED",
		sc.testTimeoutSeconds, target.host, target.port)

	// Criar Job com máxima segurança
	job := sc.createSecureJob(jobName, target.namespace, cmd)

	// Criar o Job
	startTime := time.Now()
	_, err := client.BatchV1().Jobs(target.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		health.Status = StatusCritical
		health.Message = fmt.Sprintf("Falha ao criar Job de teste: %v", err)
		health.Suggestions = append(health.Suggestions, "Verificar permissões RBAC para criar Jobs")
		return health
	}

	// Garantir cleanup do Job (dupla garantia: TTL + manual)
	defer sc.cleanupJob(ctx, client, target.namespace, jobName)

	// Aguardar conclusão do Job
	result, err := sc.waitForJob(ctx, client, target.namespace, jobName, timeout)
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		health.Status = StatusCritical
		health.Message = fmt.Sprintf("Timeout ou erro no teste: %v", err)
		health.LatencyMs = latency
		health.Suggestions = append(health.Suggestions,
			fmt.Sprintf("Testar manualmente: kubectl exec -n %s <pod> -- nc -zv %s %d",
				target.namespace, target.host, target.port))
		return health
	}

	// Analisar resultado
	health.LatencyMs = latency
	if strings.Contains(result, "SUCCESS") || strings.Contains(result, "open") {
		health.Status = StatusHealthy
		health.Reachable = true
		health.Message = fmt.Sprintf("Conectado com sucesso (%dms)", latency)
	} else {
		health.Status = StatusCritical
		health.Reachable = false
		health.ConnectionError = result
		health.Message = fmt.Sprintf("Falha na conexão: %s", strings.TrimSpace(result))
		health.Suggestions = append(health.Suggestions,
			"Verificar se o serviço está rodando",
			"Validar NetworkPolicies do namespace",
			fmt.Sprintf("kubectl get endpoints %s -n %s", target.name, target.namespace))
	}

	return health
}

// createSecureJob cria um Job com SecurityContext máximo
func (sc *ServiceChecker) createSecureJob(name, namespace, cmd string) *batchv1.Job {
	// Valores para SecurityContext
	runAsNonRoot := true
	runAsUser := int64(65534)    // nobody
	runAsGroup := int64(65534)   // nogroup
	readOnlyFS := true
	allowPrivEsc := false

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "hpa-manager-healthcheck",
				"app.kubernetes.io/component":  "service-checker",
				"app.kubernetes.io/managed-by": "hpa-manager",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &sc.jobTTLSeconds,
			ActiveDeadlineSeconds:   &sc.activeDeadlineSecs,
			BackoffLimit:            &sc.backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					// Sem ServiceAccount especial - usa default (mínimo privilégio)
					AutomountServiceAccountToken: boolPtr(false),
					Containers: []corev1.Container{
						{
							Name:  "checker",
							Image: "busybox:1.36", // Imagem minimal e estável
							Command: []string{"/bin/sh", "-c", cmd},
							// Recursos mínimos
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
							// SecurityContext máximo
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             &runAsNonRoot,
								RunAsUser:                &runAsUser,
								RunAsGroup:               &runAsGroup,
								ReadOnlyRootFilesystem:   &readOnlyFS,
								AllowPrivilegeEscalation: &allowPrivEsc,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
					// SecurityContext do Pod
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
						RunAsGroup:   &runAsGroup,
						FSGroup:      &runAsGroup,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
				},
			},
		},
	}
}

// waitForJob aguarda a conclusão do Job e retorna os logs
func (sc *ServiceChecker) waitForJob(ctx context.Context, client kubernetes.Interface, namespace, jobName string, timeout int) (string, error) {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)

	for time.Now().Before(deadline) {
		job, err := client.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("erro ao obter Job: %w", err)
		}

		// Job completou com sucesso
		if job.Status.Succeeded > 0 {
			return sc.getJobLogs(ctx, client, namespace, jobName)
		}

		// Job falhou
		if job.Status.Failed > 0 {
			logs, _ := sc.getJobLogs(ctx, client, namespace, jobName)
			return logs, nil // Retorna logs mesmo em falha
		}

		// Aguardar um pouco antes de verificar novamente
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return "", fmt.Errorf("timeout aguardando Job")
}

// getJobLogs obtém os logs do Pod criado pelo Job
func (sc *ServiceChecker) getJobLogs(ctx context.Context, client kubernetes.Interface, namespace, jobName string) (string, error) {
	// Listar pods do Job
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil || len(pods.Items) == 0 {
		return "", fmt.Errorf("nenhum pod encontrado para Job %s", jobName)
	}

	// Obter logs do primeiro pod
	podName := pods.Items[0].Name
	req := client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	logs, err := req.Do(ctx).Raw()
	if err != nil {
		return "", fmt.Errorf("erro ao obter logs: %w", err)
	}

	return string(logs), nil
}

// cleanupJob remove o Job e seus pods (backup caso TTL falhe)
func (sc *ServiceChecker) cleanupJob(ctx context.Context, client kubernetes.Interface, namespace, jobName string) {
	propagation := metav1.DeletePropagationBackground
	err := client.BatchV1().Jobs(namespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil {
		log.Warn().Err(err).Str("job", jobName).Msg("[ServiceChecker] Erro ao limpar Job (TTL irá remover)")
	}
}

// detectServiceType detecta tipo de serviço pela porta
func (sc *ServiceChecker) detectServiceType(port int32) ServiceType {
	switch port {
	case 27017:
		return ServiceMongoDB
	case 6379:
		return ServiceRedis
	case 5432:
		return ServicePostgres
	case 9092:
		return ServiceKafka
	case 5672, 15672:
		return ServiceRabbitMQ
	case 80, 443, 8080, 8443:
		return ServiceHTTP
	default:
		return ServiceHTTP // Default genérico
	}
}

// Helpers
func boolPtr(b bool) *bool {
	return &b
}
