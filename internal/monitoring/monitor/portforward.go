package monitor

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultLocalPort porta local padrão para port-forward
	DefaultLocalPort = 55553

	// HeartbeatInterval intervalo de heartbeat
	HeartbeatInterval = 10 * time.Second

	// HeartbeatTimeout timeout sem heartbeat antes de encerrar port-forward
	HeartbeatTimeout = 30 * time.Second
)

// PortForwardManager gerencia port-forwards com lifecycle e heartbeat
type PortForwardManager struct {
	localPort     int
	processes     map[string]*PortForwardProcess // cluster -> process
	lastHeartbeat time.Time
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// PortForwardProcess representa um port-forward ativo
type PortForwardProcess struct {
	Cluster      string
	Namespace    string
	Service      string
	RemotePort   int
	LocalPort    int
	Cmd          *exec.Cmd
	StartedAt    time.Time
	LastUsed     time.Time
	IsRunning    bool
	mu           sync.RWMutex
}

// NewPortForwardManager cria um novo gerenciador de port-forwards
func NewPortForwardManager(localPort int) *PortForwardManager {
	ctx, cancel := context.WithCancel(context.Background())

	mgr := &PortForwardManager{
		localPort:     localPort,
		processes:     make(map[string]*PortForwardProcess),
		lastHeartbeat: time.Now(),
		ctx:           ctx,
		cancel:        cancel,
	}

	// Inicia goroutine de heartbeat monitor
	mgr.wg.Add(1)
	go mgr.heartbeatMonitor()

	log.Info().
		Int("local_port", localPort).
		Msg("PortForward manager initialized")

	return mgr
}

// StartPortForward inicia um port-forward para um serviço
func (m *PortForwardManager) StartPortForward(cluster, namespace, service string, remotePort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s/%s/%s", cluster, namespace, service)

	// Verifica se já existe
	if proc, exists := m.processes[key]; exists {
		if proc.IsRunning {
			log.Debug().
				Str("cluster", cluster).
				Str("service", service).
				Msg("Port-forward already running")
			proc.UpdateLastUsed()
			return nil
		}
	}

	// Cria comando kubectl port-forward
	cmd := exec.CommandContext(
		m.ctx,
		"kubectl",
		"port-forward",
		"-n", namespace,
		fmt.Sprintf("svc/%s", service),
		fmt.Sprintf("%d:%d", m.localPort, remotePort),
		"--context", cluster,
	)

	// Redireciona output para logs
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Inicia o processo
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start port-forward: %w", err)
	}

	proc := &PortForwardProcess{
		Cluster:    cluster,
		Namespace:  namespace,
		Service:    service,
		RemotePort: remotePort,
		LocalPort:  m.localPort,
		Cmd:        cmd,
		StartedAt:  time.Now(),
		LastUsed:   time.Now(),
		IsRunning:  true,
	}

	m.processes[key] = proc

	// Goroutine para monitorar o processo
	m.wg.Add(1)
	go m.monitorProcess(key, proc)

	log.Info().
		Str("cluster", cluster).
		Str("namespace", namespace).
		Str("service", service).
		Int("local_port", m.localPort).
		Int("remote_port", remotePort).
		Msg("Port-forward started")

	// Aguarda um pouco para o port-forward estar pronto
	time.Sleep(2 * time.Second)

	return nil
}

// monitorProcess monitora o processo de port-forward
func (m *PortForwardManager) monitorProcess(key string, proc *PortForwardProcess) {
	defer m.wg.Done()

	// Aguarda o processo terminar
	err := proc.Cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	proc.mu.Lock()
	proc.IsRunning = false
	proc.mu.Unlock()

	if err != nil {
		// Process terminou com erro (pode ser normal se foi cancelado)
		if m.ctx.Err() == nil {
			log.Warn().
				Err(err).
				Str("cluster", proc.Cluster).
				Str("service", proc.Service).
				Msg("Port-forward process terminated")
		}
	}

	delete(m.processes, key)

	log.Debug().
		Str("cluster", proc.Cluster).
		Str("service", proc.Service).
		Msg("Port-forward process cleaned up")
}

// StopPortForward para um port-forward específico
func (m *PortForwardManager) StopPortForward(cluster, namespace, service string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s/%s/%s", cluster, namespace, service)

	proc, exists := m.processes[key]
	if !exists {
		return fmt.Errorf("port-forward not found: %s", key)
	}

	if proc.Cmd != nil && proc.Cmd.Process != nil {
		if err := proc.Cmd.Process.Kill(); err != nil {
			log.Warn().
				Err(err).
				Str("cluster", cluster).
				Str("service", service).
				Msg("Failed to kill port-forward process")
		}
	}

	log.Info().
		Str("cluster", cluster).
		Str("service", service).
		Msg("Port-forward stopped")

	return nil
}

// StopAll para todos os port-forwards
func (m *PortForwardManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Info().
		Int("count", len(m.processes)).
		Msg("Stopping all port-forwards")

	for key, proc := range m.processes {
		if proc.Cmd != nil && proc.Cmd.Process != nil {
			if err := proc.Cmd.Process.Kill(); err != nil {
				log.Warn().
					Err(err).
					Str("key", key).
					Msg("Failed to kill port-forward process")
			}
		}
	}

	m.processes = make(map[string]*PortForwardProcess)
}

// Heartbeat atualiza o timestamp do último heartbeat
func (m *PortForwardManager) Heartbeat() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastHeartbeat = time.Now()

	log.Debug().
		Time("last_heartbeat", m.lastHeartbeat).
		Msg("Heartbeat received")
}

// heartbeatMonitor monitora heartbeats e encerra port-forwards órfãos
func (m *PortForwardManager) heartbeatMonitor() {
	defer m.wg.Done()

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			log.Info().Msg("Heartbeat monitor stopping")
			return

		case <-ticker.C:
			m.mu.RLock()
			timeSinceLastHeartbeat := time.Since(m.lastHeartbeat)
			processCount := len(m.processes)
			m.mu.RUnlock()

			if timeSinceLastHeartbeat > HeartbeatTimeout && processCount > 0 {
				log.Warn().
					Dur("time_since_heartbeat", timeSinceLastHeartbeat).
					Int("active_processes", processCount).
					Msg("Heartbeat timeout detected - stopping all port-forwards")

				m.StopAll()
			}

			// Cleanup de processos inativos (não usados há mais de 5 minutos)
			m.cleanupInactiveProcesses()
		}
	}
}

// cleanupInactiveProcesses remove port-forwards não usados
func (m *PortForwardManager) cleanupInactiveProcesses() {
	m.mu.Lock()
	defer m.mu.Unlock()

	inactiveThreshold := 5 * time.Minute
	now := time.Now()

	for key, proc := range m.processes {
		proc.mu.RLock()
		timeSinceUsed := now.Sub(proc.LastUsed)
		proc.mu.RUnlock()

		if timeSinceUsed > inactiveThreshold {
			log.Info().
				Str("cluster", proc.Cluster).
				Str("service", proc.Service).
				Dur("inactive_time", timeSinceUsed).
				Msg("Cleaning up inactive port-forward")

			if proc.Cmd != nil && proc.Cmd.Process != nil {
				_ = proc.Cmd.Process.Kill()
			}

			delete(m.processes, key)
		}
	}
}

// Shutdown encerra o gerenciador e todos os port-forwards
func (m *PortForwardManager) Shutdown() {
	log.Info().Msg("Shutting down PortForward manager")

	m.StopAll()
	m.cancel()
	m.wg.Wait()

	log.Info().Msg("PortForward manager shutdown complete")
}

// GetLocalEndpoint retorna o endpoint local para um serviço
func (m *PortForwardManager) GetLocalEndpoint(cluster, namespace, service string, remotePort int) (string, error) {
	// Garante que o port-forward está rodando
	if err := m.StartPortForward(cluster, namespace, service, remotePort); err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("http://localhost:%d", m.localPort)

	// Testa se o endpoint está acessível
	if err := m.testEndpoint(endpoint); err != nil {
		return "", fmt.Errorf("endpoint not accessible: %w", err)
	}

	return endpoint, nil
}

// testEndpoint testa se um endpoint está acessível
func (m *PortForwardManager) testEndpoint(endpoint string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// GetStatus retorna o status do gerenciador
func (m *PortForwardManager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	processes := []map[string]interface{}{}
	for _, proc := range m.processes {
		proc.mu.RLock()
		processes = append(processes, map[string]interface{}{
			"cluster":     proc.Cluster,
			"namespace":   proc.Namespace,
			"service":     proc.Service,
			"local_port":  proc.LocalPort,
			"remote_port": proc.RemotePort,
			"started_at":  proc.StartedAt,
			"last_used":   proc.LastUsed,
			"is_running":  proc.IsRunning,
			"uptime":      time.Since(proc.StartedAt).String(),
		})
		proc.mu.RUnlock()
	}

	return map[string]interface{}{
		"local_port":      m.localPort,
		"last_heartbeat":  m.lastHeartbeat,
		"active_forwards": len(m.processes),
		"processes":       processes,
	}
}

// UpdateLastUsed atualiza o timestamp de último uso
func (p *PortForwardProcess) UpdateLastUsed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastUsed = time.Now()
}
