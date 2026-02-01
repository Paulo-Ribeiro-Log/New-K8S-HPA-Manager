package healthcheck

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ServiceChecker verifica conectividade de serviços via Pod Worker único
// OTIMIZADO: Usa 1 Pod por namespace ao invés de 1 Job por serviço
type ServiceChecker struct {
	// Configurações
	testTimeoutSeconds int   // Timeout do teste nc (default: 5)
	workerTTLSeconds   int   // TTL do Pod worker (default: 300 = 5min)
	batchSize          int   // Máximo de testes por batch (default: 20)
	maxConcurrent      int   // Máximo de namespaces paralelos (default: 3)

	// REST config para exec
	restConfig *rest.Config
}

// NewServiceChecker cria um novo service checker otimizado
func NewServiceChecker() *ServiceChecker {
	return &ServiceChecker{
		testTimeoutSeconds: 5,
		workerTTLSeconds:   300, // 5 minutos
		batchSize:          20,  // 20 testes por batch
		maxConcurrent:      3,   // 3 namespaces em paralelo
	}
}

// SetRESTConfig configura o REST config para exec (necessário para otimização)
func (sc *ServiceChecker) SetRESTConfig(config *rest.Config) {
	sc.restConfig = config
}

// serviceTarget representa um serviço a ser testado
type serviceTarget struct {
	name      string
	namespace string
	host      string
	port      int32
}

// batchResult agrupa resultados de um batch de testes
type batchResult struct {
	target  serviceTarget
	success bool
	output  string
	latency int64
}

// CheckAll verifica conectividade de todos os serviços nos namespaces especificados
// OTIMIZADO: Usa Pod Worker único por namespace
func (sc *ServiceChecker) CheckAll(ctx context.Context, client kubernetes.Interface, namespaces []string, timeout int, progressCallback ProgressCallback) []ServiceHealth {
	results := []ServiceHealth{}

	// 1. Descobrir serviços agrupados por namespace
	targetsByNS := sc.discoverServicesByNamespace(ctx, client, namespaces)

	totalServices := 0
	for _, targets := range targetsByNS {
		totalServices += len(targets)
	}

	if totalServices == 0 {
		log.Info().Msg("[ServiceChecker] Nenhum serviço descoberto para testar")
		return results
	}

	log.Info().
		Int("total_services", totalServices).
		Int("namespaces", len(targetsByNS)).
		Msg("[ServiceChecker] Serviços descobertos - usando Pod Worker otimizado")

	// 2. Verificar se temos REST config para exec (modo otimizado)
	if sc.restConfig != nil {
		return sc.checkAllOptimized(ctx, client, targetsByNS, timeout, progressCallback)
	}

	// 3. Fallback: modo legado (1 teste por vez sem Pod Worker)
	log.Warn().Msg("[ServiceChecker] REST config não configurado - usando modo legado (mais lento)")
	return sc.checkAllLegacy(ctx, client, targetsByNS, timeout, progressCallback)
}

// checkAllOptimized usa Pod Worker único por namespace (RECOMENDADO)
func (sc *ServiceChecker) checkAllOptimized(ctx context.Context, client kubernetes.Interface, targetsByNS map[string][]serviceTarget, timeout int, progressCallback ProgressCallback) []ServiceHealth {
	var allResults []ServiceHealth
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Semáforo para limitar namespaces paralelos
	sem := make(chan struct{}, sc.maxConcurrent)

	progress := 0
	totalServices := 0
	for _, targets := range targetsByNS {
		totalServices += len(targets)
	}

	for ns, targets := range targetsByNS {
		if len(targets) == 0 {
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // Adquirir slot

		go func(namespace string, nsTargets []serviceTarget) {
			defer wg.Done()
			defer func() { <-sem }() // Liberar slot

			log.Info().
				Str("namespace", namespace).
				Int("services", len(nsTargets)).
				Msg("[ServiceChecker] Criando Pod Worker para namespace")

			// Criar Pod Worker para este namespace
			workerPod, err := sc.createWorkerPod(ctx, client, namespace)
			if err != nil {
				log.Error().Err(err).Str("namespace", namespace).Msg("[ServiceChecker] Falha ao criar Pod Worker")

				// Marcar todos serviços deste namespace como falha
				mu.Lock()
				for _, target := range nsTargets {
					allResults = append(allResults, ServiceHealth{
						Name:        target.name,
						Namespace:   target.namespace,
						ServiceType: sc.detectServiceType(target.port),
						Status:      StatusCritical,
						Message:     fmt.Sprintf("Falha ao criar Pod Worker: %v", err),
						CheckedAt:   time.Now(),
						Suggestions: []string{"Verificar permissões RBAC para criar Pods"},
					})
					progress++
					if progressCallback != nil {
						progressCallback(target.namespace, target.name,
							fmt.Sprintf("Erro: %v", err), StatusCritical, progress, totalServices)
					}
				}
				mu.Unlock()
				return
			}

			// Garantir cleanup do Pod Worker
			defer sc.cleanupWorkerPod(ctx, client, namespace, workerPod.Name)

			// Aguardar Pod ficar Ready
			if err := sc.waitForPodReady(ctx, client, namespace, workerPod.Name, 60); err != nil {
				log.Error().Err(err).Str("pod", workerPod.Name).Msg("[ServiceChecker] Pod Worker não ficou Ready")
				mu.Lock()
				for _, target := range nsTargets {
					allResults = append(allResults, ServiceHealth{
						Name:        target.name,
						Namespace:   target.namespace,
						ServiceType: sc.detectServiceType(target.port),
						Status:      StatusCritical,
						Message:     fmt.Sprintf("Pod Worker não ficou Ready: %v", err),
						CheckedAt:   time.Now(),
					})
					progress++
				}
				mu.Unlock()
				return
			}

			// Testar serviços em batches
			for i := 0; i < len(nsTargets); i += sc.batchSize {
				end := i + sc.batchSize
				if end > len(nsTargets) {
					end = len(nsTargets)
				}
				batch := nsTargets[i:end]

				// Executar batch de testes
				batchResults := sc.executeBatch(ctx, client, namespace, workerPod.Name, batch, timeout)

				// Processar resultados
				mu.Lock()
				for _, br := range batchResults {
					health := sc.processResult(br)
					allResults = append(allResults, health)
					progress++

					if progressCallback != nil {
						msg := health.Message
						if len(msg) > 80 {
							msg = msg[:80] + "..."
						}
						progressCallback(health.Namespace, health.Name, msg, health.Status, progress, totalServices)
					}
				}
				mu.Unlock()
			}

			log.Info().
				Str("namespace", namespace).
				Int("tested", len(nsTargets)).
				Msg("[ServiceChecker] Namespace concluído")

		}(ns, targets)
	}

	wg.Wait()
	return allResults
}

// createWorkerPod cria um Pod Worker temporário para executar testes
func (sc *ServiceChecker) createWorkerPod(ctx context.Context, client kubernetes.Interface, namespace string) (*corev1.Pod, error) {
	podName := fmt.Sprintf("hc-worker-%d", time.Now().UnixNano()%100000)

	// Valores para SecurityContext
	runAsNonRoot := true
	runAsUser := int64(65534)    // nobody
	runAsGroup := int64(65534)   // nogroup
	readOnlyFS := true
	allowPrivEsc := false

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "hpa-manager-healthcheck",
				"app.kubernetes.io/component":  "service-checker-worker",
				"app.kubernetes.io/managed-by": "hpa-manager",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: boolPtr(false),
			// Manter o Pod rodando para executar múltiplos testes
			Containers: []corev1.Container{
				{
					Name:  "worker",
					Image: "busybox:1.36",
					// Sleep infinito - vamos usar exec para rodar comandos
					Command: []string{"/bin/sh", "-c", "trap 'exit 0' TERM; while true; do sleep 1; done"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("16Mi"),
						},
					},
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
	}

	return client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
}

// waitForPodReady aguarda o Pod ficar em estado Running
func (sc *ServiceChecker) waitForPodReady(ctx context.Context, client kubernetes.Interface, namespace, podName string, timeoutSeconds int) error {
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)

	for time.Now().Before(deadline) {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("erro ao obter Pod: %w", err)
		}

		if pod.Status.Phase == corev1.PodRunning {
			// Verificar se container está ready
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == "worker" && cs.Ready {
					return nil
				}
			}
		}

		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return fmt.Errorf("Pod terminou com status: %s", pod.Status.Phase)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("timeout aguardando Pod ficar Ready")
}

// executeBatch executa um batch de testes de conectividade via exec
func (sc *ServiceChecker) executeBatch(ctx context.Context, client kubernetes.Interface, namespace, podName string, targets []serviceTarget, timeout int) []batchResult {
	results := make([]batchResult, len(targets))

	// Construir comando batch que testa todos os serviços
	// Formato: host:port|host:port|...
	// Retorno: host:port:OK:latency ou host:port:FAIL:error
	var testCommands []string
	for _, t := range targets {
		// Comando que retorna resultado estruturado
		cmd := fmt.Sprintf(`
START=$(date +%%s%%N)
if nc -zv -w%d %s %d 2>&1; then
  END=$(date +%%s%%N)
  LATENCY=$(( (END - START) / 1000000 ))
  echo "%s:%d:OK:${LATENCY}ms"
else
  echo "%s:%d:FAIL:connection_refused"
fi`, sc.testTimeoutSeconds, t.host, t.port, t.host, t.port, t.host, t.port)
		testCommands = append(testCommands, cmd)
	}

	// Juntar todos os comandos
	fullCmd := strings.Join(testCommands, "\n")

	// Executar via exec
	startTime := time.Now()
	output, err := sc.execInPod(ctx, client, namespace, podName, []string{"/bin/sh", "-c", fullCmd})

	if err != nil {
		// Se exec falhou, marcar todos como falha
		for i, t := range targets {
			results[i] = batchResult{
				target:  t,
				success: false,
				output:  fmt.Sprintf("exec error: %v", err),
				latency: time.Since(startTime).Milliseconds(),
			}
		}
		return results
	}

	// Parsear output
	lines := strings.Split(strings.TrimSpace(output), "\n")
	resultMap := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Formato: host:port:STATUS:info
		// Encontrar o resultado pelo host:port
		for _, t := range targets {
			prefix := fmt.Sprintf("%s:%d:", t.host, t.port)
			if strings.HasPrefix(line, prefix) {
				resultMap[prefix] = line
				break
			}
		}
	}

	// Mapear resultados para cada target
	for i, t := range targets {
		key := fmt.Sprintf("%s:%d:", t.host, t.port)
		line := resultMap[key]

		if strings.Contains(line, ":OK:") {
			results[i] = batchResult{
				target:  t,
				success: true,
				output:  line,
				latency: sc.parseLatency(line),
			}
		} else {
			results[i] = batchResult{
				target:  t,
				success: false,
				output:  line,
				latency: time.Since(startTime).Milliseconds() / int64(len(targets)),
			}
		}
	}

	return results
}

// parseLatency extrai latência do output
func (sc *ServiceChecker) parseLatency(output string) int64 {
	// Formato: host:port:OK:123ms
	parts := strings.Split(output, ":")
	if len(parts) >= 4 {
		latStr := strings.TrimSuffix(parts[3], "ms")
		var lat int64
		fmt.Sscanf(latStr, "%d", &lat)
		return lat
	}
	return 0
}

// execInPod executa comando no Pod via API exec
func (sc *ServiceChecker) execInPod(ctx context.Context, client kubernetes.Interface, namespace, podName string, cmd []string) (string, error) {
	if sc.restConfig == nil {
		return "", fmt.Errorf("REST config não configurado")
	}

	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "worker",
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(sc.restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("erro ao criar executor: %w", err)
	}

	var stdout, stderr strings.Builder
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		return "", fmt.Errorf("erro no exec: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.String() + stderr.String(), nil
}

// processResult converte batchResult em ServiceHealth
func (sc *ServiceChecker) processResult(br batchResult) ServiceHealth {
	health := ServiceHealth{
		Name:        br.target.name,
		Namespace:   br.target.namespace,
		ServiceType: sc.detectServiceType(br.target.port),
		CheckedAt:   time.Now(),
		LatencyMs:   br.latency,
		Suggestions: []string{},
	}

	if br.success {
		health.Status = StatusHealthy
		health.Reachable = true
		health.Message = fmt.Sprintf("Conectado com sucesso (%dms)", br.latency)
	} else {
		health.Status = StatusCritical
		health.Reachable = false
		health.ConnectionError = br.output
		health.Message = fmt.Sprintf("Falha na conexão: %s", strings.TrimSpace(br.output))
		health.Suggestions = append(health.Suggestions,
			"Verificar se o serviço está rodando",
			"Validar NetworkPolicies do namespace",
			fmt.Sprintf("kubectl get endpoints %s -n %s", br.target.name, br.target.namespace))
	}

	return health
}

// cleanupWorkerPod remove o Pod Worker
func (sc *ServiceChecker) cleanupWorkerPod(ctx context.Context, client kubernetes.Interface, namespace, podName string) {
	gracePeriod := int64(0) // Immediate deletion
	err := client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
	if err != nil {
		log.Warn().Err(err).Str("pod", podName).Msg("[ServiceChecker] Erro ao limpar Pod Worker")
	} else {
		log.Debug().Str("pod", podName).Msg("[ServiceChecker] Pod Worker removido")
	}
}

// checkAllLegacy modo legado sem Pod Worker (fallback)
func (sc *ServiceChecker) checkAllLegacy(ctx context.Context, client kubernetes.Interface, targetsByNS map[string][]serviceTarget, timeout int, progressCallback ProgressCallback) []ServiceHealth {
	var results []ServiceHealth
	progress := 0

	totalServices := 0
	for _, targets := range targetsByNS {
		totalServices += len(targets)
	}

	for _, targets := range targetsByNS {
		for _, target := range targets {
			progress++

			if progressCallback != nil {
				progressCallback(target.namespace, target.name,
					fmt.Sprintf("Testando %s:%d (modo legado)...", target.host, target.port),
					StatusUnknown, progress, totalServices)
			}

			// No modo legado, apenas verificamos se há endpoints
			health := sc.checkServiceEndpoints(ctx, client, target)
			results = append(results, health)

			if progressCallback != nil {
				progressCallback(target.namespace, target.name, health.Message, health.Status, progress, totalServices)
			}
		}
	}

	return results
}

// checkServiceEndpoints verifica se o Service tem endpoints (modo legado, sem criar pods)
func (sc *ServiceChecker) checkServiceEndpoints(ctx context.Context, client kubernetes.Interface, target serviceTarget) ServiceHealth {
	health := ServiceHealth{
		Name:        target.name,
		Namespace:   target.namespace,
		ServiceType: sc.detectServiceType(target.port),
		Status:      StatusUnknown,
		CheckedAt:   time.Now(),
		Suggestions: []string{},
	}

	// Verificar endpoints do service
	endpoints, err := client.CoreV1().Endpoints(target.namespace).Get(ctx, target.name, metav1.GetOptions{})
	if err != nil {
		health.Status = StatusWarning
		health.Message = fmt.Sprintf("Não foi possível verificar endpoints: %v", err)
		return health
	}

	// Contar endpoints ativos
	activeEndpoints := 0
	for _, subset := range endpoints.Subsets {
		activeEndpoints += len(subset.Addresses)
	}

	if activeEndpoints > 0 {
		health.Status = StatusHealthy
		health.Reachable = true
		health.Message = fmt.Sprintf("Service com %d endpoint(s) ativo(s)", activeEndpoints)
	} else {
		health.Status = StatusWarning
		health.Message = "Service sem endpoints ativos"
		health.Suggestions = append(health.Suggestions,
			"Verificar se há pods com os labels corretos",
			fmt.Sprintf("kubectl get endpoints %s -n %s -o yaml", target.name, target.namespace))
	}

	return health
}

// discoverServicesByNamespace descobre serviços agrupados por namespace
func (sc *ServiceChecker) discoverServicesByNamespace(ctx context.Context, client kubernetes.Interface, namespaces []string) map[string][]serviceTarget {
	targetsByNS := make(map[string][]serviceTarget)

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
				targetsByNS[ns] = append(targetsByNS[ns], serviceTarget{
					name:      svc.Name,
					namespace: ns,
					host:      fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, ns),
					port:      port.Port,
				})
				break // Apenas primeira porta (KISS)
			}
		}
	}

	return targetsByNS
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
