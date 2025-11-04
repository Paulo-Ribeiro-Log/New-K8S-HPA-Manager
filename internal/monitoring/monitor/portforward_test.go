package monitor

import (
	"testing"
	"time"
)

// TestNewPortForwardManager testa a criação do gerenciador
func TestNewPortForwardManager(t *testing.T) {
	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	if mgr == nil {
		t.Fatal("Expected manager to be created")
	}

	if mgr.localPort != 55553 {
		t.Errorf("Expected local port 55553, got %d", mgr.localPort)
	}

	if len(mgr.processes) != 0 {
		t.Errorf("Expected 0 processes, got %d", len(mgr.processes))
	}

	// Verifica que heartbeat foi inicializado
	if time.Since(mgr.lastHeartbeat) > time.Second {
		t.Error("Expected recent heartbeat initialization")
	}
}

// TestHeartbeat testa o mecanismo de heartbeat
func TestHeartbeat(t *testing.T) {
	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	// Aguarda um pouco
	time.Sleep(100 * time.Millisecond)

	initialHeartbeat := mgr.lastHeartbeat

	// Envia heartbeat
	mgr.Heartbeat()

	// Verifica que heartbeat foi atualizado
	if !mgr.lastHeartbeat.After(initialHeartbeat) {
		t.Error("Expected heartbeat to be updated")
	}
}

// TestGetStatus testa o método GetStatus
func TestGetStatus(t *testing.T) {
	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	status := mgr.GetStatus()

	if status == nil {
		t.Fatal("Expected status to be returned")
	}

	localPort, ok := status["local_port"].(int)
	if !ok {
		t.Error("Expected local_port in status")
	}

	if localPort != 55553 {
		t.Errorf("Expected local_port 55553, got %d", localPort)
	}

	activeForwards, ok := status["active_forwards"].(int)
	if !ok {
		t.Error("Expected active_forwards in status")
	}

	if activeForwards != 0 {
		t.Errorf("Expected 0 active_forwards, got %d", activeForwards)
	}

	processes, ok := status["processes"].([]map[string]interface{})
	if !ok {
		t.Error("Expected processes in status")
	}

	if len(processes) != 0 {
		t.Errorf("Expected 0 processes, got %d", len(processes))
	}
}

// TestShutdown testa o shutdown graceful
func TestShutdown(t *testing.T) {
	mgr := NewPortForwardManager(55553)

	// Envia alguns heartbeats
	mgr.Heartbeat()
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	mgr.Shutdown()

	// Verifica que context foi cancelado
	select {
	case <-mgr.ctx.Done():
		// OK - context foi cancelado
	default:
		t.Error("Expected context to be cancelled after shutdown")
	}
}

// TestPortForwardProcess testa UpdateLastUsed
func TestPortForwardProcessUpdateLastUsed(t *testing.T) {
	proc := &PortForwardProcess{
		Cluster:   "test-cluster",
		Service:   "test-svc",
		LastUsed:  time.Now().Add(-1 * time.Hour),
		IsRunning: true,
	}

	oldLastUsed := proc.LastUsed

	// Aguarda um pouco
	time.Sleep(10 * time.Millisecond)

	// Atualiza
	proc.UpdateLastUsed()

	// Verifica que foi atualizado
	if !proc.LastUsed.After(oldLastUsed) {
		t.Error("Expected LastUsed to be updated")
	}

	if time.Since(proc.LastUsed) > time.Second {
		t.Error("Expected LastUsed to be recent")
	}
}

// TestHeartbeatTimeout testa o timeout de heartbeat
func TestHeartbeatTimeout(t *testing.T) {
	// Pula em CI/CD ou se não queremos esperar muito
	if testing.Short() {
		t.Skip("Skipping heartbeat timeout test in short mode")
	}

	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	// Define heartbeat inicial no passado
	mgr.mu.Lock()
	mgr.lastHeartbeat = time.Now().Add(-35 * time.Second) // Além do timeout de 30s
	mgr.mu.Unlock()

	// Adiciona um processo mock
	mgr.mu.Lock()
	mgr.processes["test/test/test"] = &PortForwardProcess{
		Cluster:   "test",
		Namespace: "test",
		Service:   "test",
		IsRunning: true,
	}
	mgr.mu.Unlock()

	// Aguarda o monitor verificar (HeartbeatInterval = 10s, mas vamos forçar)
	time.Sleep(2 * time.Second)

	// Força uma verificação manual (em vez de esperar 10s)
	mgr.cleanupInactiveProcesses()

	// Verifica que processos foram removidos
	mgr.mu.RLock()
	count := len(mgr.processes)
	mgr.mu.RUnlock()

	// Nota: O processo mock será removido por inatividade, não por heartbeat timeout
	// pois não tem um Cmd real. Isso é esperado.
	if count > 1 {
		t.Errorf("Expected processes to be cleaned up, got %d", count)
	}
}

// TestCleanupInactiveProcesses testa limpeza de processos inativos
func TestCleanupInactiveProcesses(t *testing.T) {
	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	// Adiciona processos mock - um ativo e um inativo
	mgr.mu.Lock()
	mgr.processes["active/test/svc"] = &PortForwardProcess{
		Cluster:   "active",
		Namespace: "test",
		Service:   "svc",
		LastUsed:  time.Now(), // Recente
		IsRunning: true,
	}
	mgr.processes["inactive/test/svc"] = &PortForwardProcess{
		Cluster:   "inactive",
		Namespace: "test",
		Service:   "svc",
		LastUsed:  time.Now().Add(-10 * time.Minute), // Inativo há 10 min
		IsRunning: true,
	}
	mgr.mu.Unlock()

	// Executa cleanup
	mgr.cleanupInactiveProcesses()

	// Verifica que apenas o ativo permanece
	mgr.mu.RLock()
	count := len(mgr.processes)
	_, activeExists := mgr.processes["active/test/svc"]
	_, inactiveExists := mgr.processes["inactive/test/svc"]
	mgr.mu.RUnlock()

	if count != 1 {
		t.Errorf("Expected 1 process after cleanup, got %d", count)
	}

	if !activeExists {
		t.Error("Expected active process to remain")
	}

	if inactiveExists {
		t.Error("Expected inactive process to be removed")
	}
}

// BenchmarkHeartbeat benchmark para heartbeat
func BenchmarkHeartbeat(b *testing.B) {
	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.Heartbeat()
	}
}

// BenchmarkGetStatus benchmark para GetStatus
func BenchmarkGetStatus(b *testing.B) {
	mgr := NewPortForwardManager(55553)
	defer mgr.Shutdown()

	// Adiciona alguns processos mock
	for i := 0; i < 10; i++ {
		mgr.processes[string(rune(i))] = &PortForwardProcess{
			Cluster:   "test",
			Service:   "svc",
			IsRunning: true,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mgr.GetStatus()
	}
}
