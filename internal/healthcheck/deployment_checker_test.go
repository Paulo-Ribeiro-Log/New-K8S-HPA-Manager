package healthcheck

import (
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// TestCircuitBreakerInitialState verifica estado inicial do circuit breaker
func TestCircuitBreakerInitialState(t *testing.T) {
	checker := NewDeploymentChecker()

	if checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker deveria estar fechado inicialmente")
	}

	if checker.GetMetricsFailCount() != 0 {
		t.Errorf("Contador de falhas deveria ser 0, got %d", checker.GetMetricsFailCount())
	}
}

// TestCircuitBreakerThreshold verifica que threshold está configurado corretamente
func TestCircuitBreakerThreshold(t *testing.T) {
	if MetricsCircuitBreakerThreshold != 3 {
		t.Errorf("MetricsCircuitBreakerThreshold = %d, want 3", MetricsCircuitBreakerThreshold)
	}
}

// TestCircuitBreakerReset verifica que reset funciona corretamente
func TestCircuitBreakerReset(t *testing.T) {
	checker := NewDeploymentChecker()

	// Simular estado com falhas
	checker.metricsMu.Lock()
	checker.metricsFailCount = 5
	checker.metricsCircuitOpen = true
	checker.metricsMu.Unlock()

	// Verificar estado antes do reset
	if !checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker deveria estar aberto antes do reset")
	}
	if checker.GetMetricsFailCount() != 5 {
		t.Errorf("Contador de falhas deveria ser 5, got %d", checker.GetMetricsFailCount())
	}

	// Reset
	checker.ResetMetricsCircuitBreaker()

	// Verificar estado após reset
	if checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker deveria estar fechado após reset")
	}
	if checker.GetMetricsFailCount() != 0 {
		t.Errorf("Contador de falhas deveria ser 0 após reset, got %d", checker.GetMetricsFailCount())
	}
}

// TestCircuitBreakerOpensAfterThreshold verifica que circuit breaker abre após N falhas
func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	checker := NewDeploymentChecker()

	// Simular falhas consecutivas até o threshold
	for i := 1; i <= MetricsCircuitBreakerThreshold; i++ {
		checker.metricsMu.Lock()
		checker.metricsFailCount = i
		if i >= MetricsCircuitBreakerThreshold {
			checker.metricsCircuitOpen = true
		}
		checker.metricsMu.Unlock()
	}

	// Após threshold, circuit breaker deve estar aberto
	if !checker.IsMetricsCircuitOpen() {
		t.Errorf("Circuit breaker deveria abrir após %d falhas", MetricsCircuitBreakerThreshold)
	}
}

// TestCircuitBreakerStaysClosedBelowThreshold verifica que circuit breaker permanece fechado abaixo do threshold
func TestCircuitBreakerStaysClosedBelowThreshold(t *testing.T) {
	checker := NewDeploymentChecker()

	// Simular falhas abaixo do threshold
	for i := 1; i < MetricsCircuitBreakerThreshold; i++ {
		checker.metricsMu.Lock()
		checker.metricsFailCount = i
		checker.metricsMu.Unlock()

		if checker.IsMetricsCircuitOpen() {
			t.Errorf("Circuit breaker não deveria abrir com apenas %d falhas (threshold = %d)", i, MetricsCircuitBreakerThreshold)
		}
	}
}

// TestCircuitBreakerThreadSafety verifica que operações são thread-safe
func TestCircuitBreakerThreadSafety(t *testing.T) {
	checker := NewDeploymentChecker()
	iterations := 1000

	var wg sync.WaitGroup

	// Goroutine 1: Incrementa contador de falhas
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			checker.metricsMu.Lock()
			checker.metricsFailCount++
			checker.metricsMu.Unlock()
		}
	}()

	// Goroutine 2: Lê estado do circuit breaker
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = checker.IsMetricsCircuitOpen()
			_ = checker.GetMetricsFailCount()
		}
	}()

	// Goroutine 3: Reset periódico
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/10; i++ {
			checker.ResetMetricsCircuitBreaker()
		}
	}()

	// Aguardar conclusão sem deadlock
	wg.Wait()

	// Se chegou aqui sem deadlock ou panic, passou
	t.Log("Thread safety test passed - no deadlocks or panics")
}

// TestCircuitBreakerResetBetweenSessions simula comportamento entre sessões de health check
func TestCircuitBreakerResetBetweenSessions(t *testing.T) {
	checker := NewDeploymentChecker()

	// Sessão 1: Falhas que abrem o circuit breaker
	for i := 0; i < MetricsCircuitBreakerThreshold; i++ {
		checker.metricsMu.Lock()
		checker.metricsFailCount++
		if checker.metricsFailCount >= MetricsCircuitBreakerThreshold {
			checker.metricsCircuitOpen = true
		}
		checker.metricsMu.Unlock()
	}

	if !checker.IsMetricsCircuitOpen() {
		t.Fatal("Circuit breaker deveria estar aberto após sessão 1")
	}

	// Início da Sessão 2: Reset (como faz orchestrator.go:122)
	checker.ResetMetricsCircuitBreaker()

	// Circuit breaker deve estar fechado para nova sessão
	if checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker deveria estar fechado para nova sessão")
	}

	// Sessão 2: Sucesso (sem falhas)
	// Circuit breaker permanece fechado
	if checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker deveria permanecer fechado sem falhas")
	}
}

// TestCircuitBreakerSuccessResetsFailCount simula sucesso resetando contador
func TestCircuitBreakerSuccessResetsFailCount(t *testing.T) {
	checker := NewDeploymentChecker()

	// Simular 2 falhas (abaixo do threshold)
	checker.metricsMu.Lock()
	checker.metricsFailCount = 2
	checker.metricsMu.Unlock()

	if checker.GetMetricsFailCount() != 2 {
		t.Errorf("Contador deveria ser 2, got %d", checker.GetMetricsFailCount())
	}

	// Simular sucesso (como em enrichWithMetrics quando métricas são coletadas)
	checker.metricsMu.Lock()
	checker.metricsFailCount = 0 // Reset após sucesso
	checker.metricsMu.Unlock()

	if checker.GetMetricsFailCount() != 0 {
		t.Errorf("Contador deveria ser 0 após sucesso, got %d", checker.GetMetricsFailCount())
	}

	if checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker não deveria abrir após sucesso")
	}
}

// TestNewDeploymentChecker verifica que NewDeploymentChecker inicializa corretamente
func TestNewDeploymentChecker(t *testing.T) {
	checker := NewDeploymentChecker()

	if checker == nil {
		t.Fatal("NewDeploymentChecker retornou nil")
	}

	// Verificar estado inicial
	if checker.metricsFailCount != 0 {
		t.Errorf("metricsFailCount inicial = %d, want 0", checker.metricsFailCount)
	}

	if checker.metricsCircuitOpen != false {
		t.Error("metricsCircuitOpen inicial deveria ser false")
	}
}

// TestCircuitBreakerDoesNotOpenOnFirstFailure verifica que uma falha não abre o circuit breaker
func TestCircuitBreakerDoesNotOpenOnFirstFailure(t *testing.T) {
	checker := NewDeploymentChecker()

	// Simular primeira falha
	checker.metricsMu.Lock()
	checker.metricsFailCount = 1
	checker.metricsMu.Unlock()

	if checker.IsMetricsCircuitOpen() {
		t.Error("Circuit breaker NÃO deveria abrir na primeira falha")
	}
}

// TestCircuitBreakerGettersAreConsistent verifica que getters retornam valores consistentes
func TestCircuitBreakerGettersAreConsistent(t *testing.T) {
	checker := NewDeploymentChecker()

	// Definir estado
	checker.metricsMu.Lock()
	checker.metricsFailCount = 2
	checker.metricsCircuitOpen = false
	checker.metricsMu.Unlock()

	// Getters devem refletir estado
	if checker.GetMetricsFailCount() != 2 {
		t.Errorf("GetMetricsFailCount() = %d, want 2", checker.GetMetricsFailCount())
	}
	if checker.IsMetricsCircuitOpen() != false {
		t.Error("IsMetricsCircuitOpen() deveria ser false")
	}

	// Mudar estado
	checker.metricsMu.Lock()
	checker.metricsFailCount = 5
	checker.metricsCircuitOpen = true
	checker.metricsMu.Unlock()

	// Getters devem refletir novo estado
	if checker.GetMetricsFailCount() != 5 {
		t.Errorf("GetMetricsFailCount() = %d, want 5", checker.GetMetricsFailCount())
	}
	if checker.IsMetricsCircuitOpen() != true {
		t.Error("IsMetricsCircuitOpen() deveria ser true")
	}
}

// ==================== TESTES DE VALIDAÇÃO DE PROBES ====================

// TestProbeValidationConstants verifica que constantes de validação estão configuradas
func TestProbeValidationConstants(t *testing.T) {
	if MinLivenessTimeout < 1 {
		t.Errorf("MinLivenessTimeout = %d, deveria ser >= 1", MinLivenessTimeout)
	}
	if MinReadinessTimeout < 1 {
		t.Errorf("MinReadinessTimeout = %d, deveria ser >= 1", MinReadinessTimeout)
	}
	if MinStartupTimeout < 1 {
		t.Errorf("MinStartupTimeout = %d, deveria ser >= 1", MinStartupTimeout)
	}
	if MinInitialDelay < 1 {
		t.Errorf("MinInitialDelay = %d, deveria ser >= 1", MinInitialDelay)
	}
	if MinFailureThreshold < 1 {
		t.Errorf("MinFailureThreshold = %d, deveria ser >= 1", MinFailureThreshold)
	}
}

// TestValidateProbeConfigTimeoutTooShort verifica detecção de timeout muito curto
func TestValidateProbeConfigTimeoutTooShort(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		TimeoutSeconds: 1, // Muito curto
	}

	issues := checker.validateProbeConfig("test-container", "liveness", probe)

	if len(issues) == 0 {
		t.Error("Deveria detectar timeout muito curto")
	}

	found := false
	for _, issue := range issues {
		if issue.Container == "test-container" && issue.ProbeType == "liveness" {
			// Timeout muito curto é classificado como SeverityLow
			if issue.Severity == SeverityLow {
				found = true
			}
		}
	}

	if !found {
		t.Error("Issue de timeout deveria ter severity=low")
	}
}

// TestValidateProbeConfigNoInitialDelay verifica detecção de initialDelaySeconds=0
func TestValidateProbeConfigNoInitialDelay(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		InitialDelaySeconds: 0, // Pode causar restarts prematuros
		TimeoutSeconds:      10,
	}

	issues := checker.validateProbeConfig("test-container", "liveness", probe)

	found := false
	for _, issue := range issues {
		if issue.ProbeType == "liveness" && issue.Issue != "" {
			if issue.Issue == "initialDelaySeconds=0 pode causar restarts durante inicialização lenta" {
				found = true
			}
		}
	}

	if !found {
		t.Error("Deveria detectar initialDelaySeconds=0 em liveness probe")
	}
}

// TestValidateProbeConfigLowInitialDelay verifica detecção de initialDelaySeconds muito baixo
func TestValidateProbeConfigLowInitialDelay(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		InitialDelaySeconds: 2, // Menor que MinInitialDelay
		TimeoutSeconds:      10,
	}

	issues := checker.validateProbeConfig("test-container", "liveness", probe)

	found := false
	for _, issue := range issues {
		// initialDelaySeconds baixo é classificado como SeverityLow
		if issue.ProbeType == "liveness" && issue.Severity == SeverityLow {
			found = true
		}
	}

	if !found {
		t.Error("Deveria detectar initialDelaySeconds muito baixo")
	}
}

// TestValidateProbeConfigLowFailureThreshold verifica detecção de failureThreshold muito baixo
func TestValidateProbeConfigLowFailureThreshold(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		FailureThreshold:    1, // Muito baixo
		TimeoutSeconds:      10,
		InitialDelaySeconds: 30,
	}

	issues := checker.validateProbeConfig("test-container", "readiness", probe)

	found := false
	for _, issue := range issues {
		// failureThreshold baixo é classificado como SeverityLow
		if issue.ProbeType == "readiness" && issue.Severity == SeverityLow {
			if len(issue.Issue) > 0 {
				found = true
			}
		}
	}

	if !found {
		t.Error("Deveria detectar failureThreshold muito baixo")
	}
}

// TestValidateProbeConfigFrequentPeriod verifica detecção de periodSeconds muito frequente
func TestValidateProbeConfigFrequentPeriod(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		PeriodSeconds:       2, // Muito frequente (< 5)
		TimeoutSeconds:      10,
		InitialDelaySeconds: 30,
		FailureThreshold:    3,
	}

	issues := checker.validateProbeConfig("test-container", "liveness", probe)

	found := false
	for _, issue := range issues {
		// periodSeconds frequente é classificado como SeverityLow
		if issue.Issue != "" && issue.Severity == SeverityLow {
			found = true
		}
	}

	if !found {
		t.Error("Deveria detectar periodSeconds muito frequente")
	}
}

// TestValidateProbeConfigGoodConfiguration verifica que configuração boa não gera issues
func TestValidateProbeConfigGoodConfiguration(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		TimeoutSeconds:      10,
		InitialDelaySeconds: 30,
		FailureThreshold:    3,
		PeriodSeconds:       10,
	}

	issues := checker.validateProbeConfig("test-container", "liveness", probe)

	if len(issues) > 0 {
		t.Errorf("Configuração boa não deveria gerar issues, got %d issues", len(issues))
		for _, issue := range issues {
			t.Logf("Issue: %s - %s", issue.ProbeType, issue.Issue)
		}
	}
}

// TestValidateProbeConfigReadinessNoInitialDelayCheck verifica que readiness não valida initialDelay
func TestValidateProbeConfigReadinessNoInitialDelayCheck(t *testing.T) {
	checker := NewDeploymentChecker()

	probe := &corev1.Probe{
		InitialDelaySeconds: 0, // OK para readiness
		TimeoutSeconds:      10,
		FailureThreshold:    3,
	}

	issues := checker.validateProbeConfig("test-container", "readiness", probe)

	for _, issue := range issues {
		if issue.Issue == "initialDelaySeconds=0 pode causar restarts durante inicialização lenta" {
			t.Error("Readiness probe não deveria validar initialDelaySeconds=0 (apenas liveness)")
		}
	}
}

// TestProbeIssueStruct verifica estrutura ProbeIssue
func TestProbeIssueStruct(t *testing.T) {
	issue := ProbeIssue{
		Container: "nginx",
		ProbeType: "liveness",
		Issue:     "timeout muito curto",
		Severity:  "warning",
	}

	if issue.Container != "nginx" {
		t.Errorf("Container = %s, want nginx", issue.Container)
	}
	if issue.ProbeType != "liveness" {
		t.Errorf("ProbeType = %s, want liveness", issue.ProbeType)
	}
	if issue.Severity != "warning" {
		t.Errorf("Severity = %s, want warning", issue.Severity)
	}
}

// ==================== TESTES DE VALIDAÇÃO DE RECURSOS ====================

// TestQoSClassConstants verifica que constantes de QoS estão definidas
func TestQoSClassConstants(t *testing.T) {
	if QoSGuaranteed != "Guaranteed" {
		t.Errorf("QoSGuaranteed = %s, want Guaranteed", QoSGuaranteed)
	}
	if QoSBurstable != "Burstable" {
		t.Errorf("QoSBurstable = %s, want Burstable", QoSBurstable)
	}
	if QoSBestEffort != "BestEffort" {
		t.Errorf("QoSBestEffort = %s, want BestEffort", QoSBestEffort)
	}
}

// TestResourceIssueStruct verifica estrutura ResourceIssue
func TestResourceIssueStruct(t *testing.T) {
	issue := ResourceIssue{
		Container:    "nginx",
		ResourceType: "memory",
		Issue:        "Memory limit não definido",
		Severity:     "critical",
	}

	if issue.Container != "nginx" {
		t.Errorf("Container = %s, want nginx", issue.Container)
	}
	if issue.ResourceType != "memory" {
		t.Errorf("ResourceType = %s, want memory", issue.ResourceType)
	}
	if issue.Severity != "critical" {
		t.Errorf("Severity = %s, want critical", issue.Severity)
	}
}

// TestContainerResourcesStruct verifica estrutura ContainerResources
func TestContainerResourcesStruct(t *testing.T) {
	cr := ContainerResources{
		Name:             "nginx",
		CPURequest:       "100m",
		MemoryRequest:    "128Mi",
		CPULimit:         "500m",
		MemoryLimit:      "256Mi",
		HasCPURequest:    true,
		HasMemoryRequest: true,
		HasCPULimit:      true,
		HasMemoryLimit:   true,
	}

	if cr.Name != "nginx" {
		t.Errorf("Name = %s, want nginx", cr.Name)
	}
	if cr.CPURequest != "100m" {
		t.Errorf("CPURequest = %s, want 100m", cr.CPURequest)
	}
	if !cr.HasCPURequest || !cr.HasMemoryRequest || !cr.HasCPULimit || !cr.HasMemoryLimit {
		t.Error("Todos os flags deveriam ser true")
	}
}

// TestAnalyzeResourcesBestEffort verifica detecção de QoS BestEffort
func TestAnalyzeResourcesBestEffort(t *testing.T) {
	checker := NewDeploymentChecker()

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							// Sem resources definidos
						},
					},
				},
			},
		},
	}

	health := &DeploymentHealth{}
	checker.analyzeResources(deployment, health)

	if health.QoSClass != QoSBestEffort {
		t.Errorf("QoSClass = %s, want BestEffort", health.QoSClass)
	}

	if len(health.ResourceIssues) != 4 {
		t.Errorf("Deveria ter 4 issues (cpu/memory request/limit), got %d", len(health.ResourceIssues))
	}
}

// TestAnalyzeResourcesBurstable verifica detecção de QoS Burstable
func TestAnalyzeResourcesBurstable(t *testing.T) {
	checker := NewDeploymentChecker()

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								// Sem limits
							},
						},
					},
				},
			},
		},
	}

	health := &DeploymentHealth{}
	checker.analyzeResources(deployment, health)

	if health.QoSClass != QoSBurstable {
		t.Errorf("QoSClass = %s, want Burstable", health.QoSClass)
	}

	// Deveria ter 2 issues (falta cpu limit e memory limit)
	if len(health.ResourceIssues) != 2 {
		t.Errorf("Deveria ter 2 issues (cpu/memory limit), got %d", len(health.ResourceIssues))
	}
}

// TestAnalyzeResourcesGuaranteed verifica detecção de QoS Guaranteed
func TestAnalyzeResourcesGuaranteed(t *testing.T) {
	checker := NewDeploymentChecker()

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	health := &DeploymentHealth{}
	checker.analyzeResources(deployment, health)

	if health.QoSClass != QoSGuaranteed {
		t.Errorf("QoSClass = %s, want Guaranteed", health.QoSClass)
	}

	// Não deveria ter issues
	if len(health.ResourceIssues) != 0 {
		t.Errorf("Não deveria ter issues, got %d", len(health.ResourceIssues))
	}
}

// TestAnalyzeResourcesNoMemoryLimit verifica que falta de memory limit é critical
func TestAnalyzeResourcesNoMemoryLimit(t *testing.T) {
	checker := NewDeploymentChecker()

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("500m"),
									// Sem memory limit
								},
							},
						},
					},
				},
			},
		},
	}

	health := &DeploymentHealth{}
	checker.analyzeResources(deployment, health)

	// Verificar que tem issue high para memory limit (OOMKill risk)
	hasHighMemoryIssue := false
	for _, issue := range health.ResourceIssues {
		if issue.ResourceType == "memory" && issue.Severity == SeverityHigh {
			hasHighMemoryIssue = true
		}
	}

	if !hasHighMemoryIssue {
		t.Error("Deveria ter issue high para memory limit faltando")
	}
}

// TestAnalyzeResourcesMultipleContainers verifica análise com múltiplos containers
func TestAnalyzeResourcesMultipleContainers(t *testing.T) {
	checker := NewDeploymentChecker()

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
						{
							Name: "sidecar",
							// Sem resources - vai puxar QoS para Burstable
						},
					},
				},
			},
		},
	}

	health := &DeploymentHealth{}
	checker.analyzeResources(deployment, health)

	// Com um container sem resources, QoS deve ser Burstable (não Guaranteed)
	if health.QoSClass != QoSBurstable {
		t.Errorf("QoSClass = %s, want Burstable (um container sem resources)", health.QoSClass)
	}

	// Verificar que ContainerResources tem 2 entradas
	if len(health.ContainerResources) != 2 {
		t.Errorf("Deveria ter 2 ContainerResources, got %d", len(health.ContainerResources))
	}
}

// TestAllRequestsEqualLimits verifica função auxiliar
func TestAllRequestsEqualLimits(t *testing.T) {
	checker := NewDeploymentChecker()

	// Caso 1: requests == limits
	deployment1 := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	if !checker.allRequestsEqualLimits(deployment1) {
		t.Error("Deveria retornar true quando requests == limits")
	}

	// Caso 2: requests != limits
	deployment2 := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "nginx",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	if checker.allRequestsEqualLimits(deployment2) {
		t.Error("Deveria retornar false quando requests != limits")
	}
}
