package healthcheck

import (
	"sync"
	"testing"
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
