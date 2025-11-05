package portforward

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// PortForward gerencia port-forward para Prometheus
type PortForward struct {
	cluster   string
	namespace string
	service   string
	localPort int
	cmd       *exec.Cmd
	cancel    context.CancelFunc
}

// Config configuração do port-forward
type Config struct {
	Cluster   string
	Namespace string // Default: "monitoring"
	Service   string // Default: "prometheus-k8s" ou "prometheus-server"
	LocalPort int    // Default: 9090
}

// New cria novo port-forward
func New(cfg Config) *PortForward {
	// Defaults
	if cfg.Namespace == "" {
		cfg.Namespace = "monitoring"
	}
	if cfg.Service == "" {
		cfg.Service = "prometheus-k8s"
	}
	if cfg.LocalPort == 0 {
		cfg.LocalPort = 9090
	}

	return &PortForward{
		cluster:   cfg.Cluster,
		namespace: cfg.Namespace,
		service:   cfg.Service,
		localPort: cfg.LocalPort,
	}
}

// Start inicia port-forward
func (pf *PortForward) Start() error {
	log.Info().
		Str("cluster", pf.cluster).
		Str("namespace", pf.namespace).
		Str("service", pf.service).
		Int("port", pf.localPort).
		Msg("Iniciando port-forward para Prometheus")

	ctx, cancel := context.WithCancel(context.Background())
	pf.cancel = cancel

	// Comando kubectl port-forward
	pf.cmd = exec.CommandContext(ctx,
		"kubectl",
		"port-forward",
		fmt.Sprintf("svc/%s", pf.service),
		fmt.Sprintf("%d:9090", pf.localPort),
		"-n", pf.namespace,
		"--context", pf.cluster,
	)

	// Inicia em background
	if err := pf.cmd.Start(); err != nil {
		return fmt.Errorf("falha ao iniciar port-forward: %w", err)
	}

	// Aguarda port-forward estar pronto
	if err := pf.waitForReady(); err != nil {
		pf.Stop()
		return err
	}

	log.Info().
		Str("cluster", pf.cluster).
		Int("port", pf.localPort).
		Msg("Port-forward ativo")

	return nil
}

// Stop para port-forward
func (pf *PortForward) Stop() error {
	if pf.cancel != nil {
		pf.cancel()
	}

	if pf.cmd != nil && pf.cmd.Process != nil {
		log.Info().
			Str("cluster", pf.cluster).
			Msg("Parando port-forward")

		if err := pf.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("falha ao parar port-forward: %w", err)
		}
	}

	return nil
}

// waitForReady aguarda port-forward estar pronto
func (pf *PortForward) waitForReady() error {
	url := fmt.Sprintf("http://localhost:%d/-/ready", pf.localPort)
	timeout := time.After(30 * time.Second) // Aumentado para 30s (clusters Azure AKS remotos)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout aguardando port-forward em %s", url)

		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

// GetURL retorna URL do Prometheus
func (pf *PortForward) GetURL() string {
	return fmt.Sprintf("http://localhost:%d", pf.localPort)
}

// IsRunning verifica se port-forward está ativo
func (pf *PortForward) IsRunning() bool {
	if pf.cmd == nil || pf.cmd.Process == nil {
		return false
	}

	// Tenta conectar
	url := fmt.Sprintf("http://localhost:%d/-/ready", pf.localPort)
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}

// PortForwardManager gerencia múltiplos port-forwards
type PortForwardManager struct {
	forwards    map[string]*PortForward
	portOdd     int  // Porta para clusters ímpares (default: 55553)
	portEven    int  // Porta para clusters pares (default: 55554)
	oddBusy     bool // Porta ímpar está em uso
	evenBusy    bool // Porta par está em uso
	clusterIndex map[string]int // Mapeia cluster -> índice
}

// NewManager cria novo gerenciador
func NewManager() *PortForwardManager {
	return &PortForwardManager{
		forwards:     make(map[string]*PortForward),
		clusterIndex: make(map[string]int),
		portOdd:      55553, // Porta fixa para clusters ímpares
		portEven:     55554, // Porta fixa para clusters pares
	}
}

// discoverPrometheusService tenta descobrir o serviço do Prometheus
func (m *PortForwardManager) discoverPrometheusService(cluster string) string {
	// Lista de nomes comuns do Prometheus
	commonNames := []string{
		"prometheus-prometheus",  // Prometheus Operator
		"prometheus-k8s",         // Kube-Prometheus
		"prometheus-server",      // Helm Chart comum
		"prometheus",             // Nome simples
		"prometheus-operated",    // Prometheus Operator (statefulset)
	}

	// Tenta cada nome
	for _, serviceName := range commonNames {
		log.Debug().
			Str("cluster", cluster).
			Str("service", serviceName).
			Msg("Tentando descobrir serviço Prometheus")

		// Context precisa do sufixo -admin
		context := cluster
		if !strings.HasSuffix(cluster, "-admin") {
			context = cluster + "-admin"
		}

		// Testa se serviço existe
		cmd := exec.Command("kubectl",
			"get", "svc", serviceName,
			"-n", "monitoring",
			"--context", context,
			"--no-headers",
		)

		if err := cmd.Run(); err == nil {
			log.Info().
				Str("cluster", cluster).
				Str("service", serviceName).
				Msg("Serviço Prometheus descoberto")
			return serviceName
		}
	}

	// Fallback: retorna default
	log.Warn().
		Str("cluster", cluster).
		Msg("Não foi possível descobrir serviço Prometheus, usando default: prometheus-k8s")
	return "prometheus-k8s"
}

// Start inicia port-forward para um cluster
func (m *PortForwardManager) Start(cluster string) error {
	// Se já existe, retorna
	if pf, exists := m.forwards[cluster]; exists && pf.IsRunning() {
		log.Info().Str("cluster", cluster).Msg("Port-forward já ativo, reutilizando")
		return nil
	}

	// Atribui índice ao cluster se ainda não tem
	if _, exists := m.clusterIndex[cluster]; !exists {
		m.clusterIndex[cluster] = len(m.clusterIndex)
	}
	index := m.clusterIndex[cluster]

	// Determina porta baseado no índice (ímpar/par)
	var port int
	var waitForRelease bool
	if index%2 == 0 {
		// Cluster par -> porta 55554
		port = m.portEven
		if m.evenBusy {
			waitForRelease = true
			log.Info().
				Str("cluster", cluster).
				Int("index", index).
				Int("port", port).
				Msg("Porta par ocupada, aguardando release...")
		}
		m.evenBusy = true
	} else {
		// Cluster ímpar -> porta 55553
		port = m.portOdd
		if m.oddBusy {
			waitForRelease = true
			log.Info().
				Str("cluster", cluster).
				Int("index", index).
				Int("port", port).
				Msg("Porta ímpar ocupada, aguardando release...")
		}
		m.oddBusy = true
	}

	// Se porta está ocupada, para o port-forward anterior
	if waitForRelease {
		m.releasePortForCluster(port)
		time.Sleep(2 * time.Second) // Aguarda porta ser liberada
	}

	// Descobre o nome do serviço Prometheus
	serviceName := m.discoverPrometheusService(cluster)

	log.Info().
		Str("cluster", cluster).
		Int("index", index).
		Int("port", port).
		Msg("Iniciando port-forward com porta compartilhada")

	// Context precisa do sufixo -admin
	context := cluster
	if !strings.HasSuffix(cluster, "-admin") {
		context = cluster + "-admin"
	}

	pf := New(Config{
		Cluster:   context,
		Service:   serviceName,
		LocalPort: port,
	})

	if err := pf.Start(); err != nil {
		// Libera flag da porta em caso de erro
		if index%2 == 0 {
			m.evenBusy = false
		} else {
			m.oddBusy = false
		}
		return err
	}

	m.forwards[cluster] = pf
	return nil
}

// releasePortForCluster para port-forwards usando uma porta específica
func (m *PortForwardManager) releasePortForCluster(port int) {
	for cluster, pf := range m.forwards {
		if pf.localPort == port {
			log.Info().
				Str("cluster", cluster).
				Int("port", port).
				Msg("Liberando porta para reutilização")
			pf.Stop()
			delete(m.forwards, cluster)
			return
		}
	}
}

// Stop para port-forward de um cluster
func (m *PortForwardManager) Stop(cluster string) error {
	pf, exists := m.forwards[cluster]
	if !exists {
		return nil
	}

	if err := pf.Stop(); err != nil {
		return err
	}

	// Libera flag da porta
	if index, exists := m.clusterIndex[cluster]; exists {
		if index%2 == 0 {
			m.evenBusy = false
		} else {
			m.oddBusy = false
		}
	}

	delete(m.forwards, cluster)
	return nil
}

// StopAll para todos os port-forwards
func (m *PortForwardManager) StopAll() error {
	for cluster, pf := range m.forwards {
		if err := pf.Stop(); err != nil {
			log.Error().
				Err(err).
				Str("cluster", cluster).
				Msg("Erro ao parar port-forward")
		}
	}

	m.forwards = make(map[string]*PortForward)
	m.oddBusy = false
	m.evenBusy = false
	return nil
}

// GetURL retorna URL do Prometheus para um cluster
func (m *PortForwardManager) GetURL(cluster string) string {
	pf, exists := m.forwards[cluster]
	if !exists {
		return ""
	}
	return pf.GetURL()
}
