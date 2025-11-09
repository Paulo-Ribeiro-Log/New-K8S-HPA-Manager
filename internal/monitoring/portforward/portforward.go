package portforward

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
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
	mu           sync.RWMutex   // CORREÇÃO: Protege acesso concorrente aos maps
	forwards     map[string]*PortForward
	portOdd      int            // Porta para clusters ímpares (default: 55553)
	portEven     int            // Porta para clusters pares (default: 55554)
	oddBusy      bool           // Porta ímpar está em uso
	evenBusy     bool           // Porta par está em uso
	clusterIndex map[string]int // Mapeia cluster -> índice

	// NOVAS portas para baseline (55555 e 55556)
	portBaseline1      int  // Porta baseline 1 (default: 55555)
	portBaseline2      int  // Porta baseline 2 (default: 55556)
	baseline1Busy      bool // Porta baseline 1 está em uso
	baseline2Busy      bool // Porta baseline 2 está em uso
	baselineCluster1   string // Cluster usando porta baseline 1
	baselineCluster2   string // Cluster usando porta baseline 2
}

// NewManager cria novo gerenciador
func NewManager() *PortForwardManager {
	return &PortForwardManager{
		forwards:     make(map[string]*PortForward),
		clusterIndex: make(map[string]int),
		portOdd:      55553, // Porta fixa para clusters ímpares
		portEven:     55554, // Porta fixa para clusters pares

		// Portas dedicadas para baseline (coleta histórica)
		portBaseline1: 55555,
		portBaseline2: 55556,
	}
}

// discoverPrometheusService tenta descobrir o serviço do Prometheus
func (m *PortForwardManager) discoverPrometheusService(cluster string) string {
	// Lista de nomes comuns do Prometheus
	commonNames := []string{
		"prometheus-prometheus", // Prometheus Operator
		"prometheus-k8s",        // Kube-Prometheus
		"prometheus-server",     // Helm Chart comum
		"prometheus",            // Nome simples
		"prometheus-operated",   // Prometheus Operator (statefulset)
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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.RLock()
	defer m.mu.RUnlock()

	pf, exists := m.forwards[cluster]
	if !exists {
		return ""
	}
	return pf.GetURL()
}

// StartPortForward inicia port-forward em porta específica (para baseline)
// Permite especificar porta manualmente (55555 ou 55556)
func (m *PortForwardManager) StartPortForward(cluster string, port int) error {
	// Valida porta (deve ser 55555 ou 55556 para baseline)
	if port != m.portBaseline1 && port != m.portBaseline2 {
		return fmt.Errorf("porta inválida para baseline: %d (esperado 55555 ou 55556)", port)
	}

	// Verifica se porta já está em uso
	key := fmt.Sprintf("%s:%d", cluster, port)
	if pf, exists := m.forwards[key]; exists && pf.IsRunning() {
		log.Info().
			Str("cluster", cluster).
			Int("port", port).
			Msg("Port-forward de baseline já ativo, reutilizando")
		return nil
	}

	// Marca porta como ocupada
	if port == m.portBaseline1 {
		if m.baseline1Busy {
			return fmt.Errorf("porta baseline 1 (%d) já está ocupada por cluster: %s", port, m.baselineCluster1)
		}
		m.baseline1Busy = true
		m.baselineCluster1 = cluster
	} else {
		if m.baseline2Busy {
			return fmt.Errorf("porta baseline 2 (%d) já está ocupada por cluster: %s", port, m.baselineCluster2)
		}
		m.baseline2Busy = true
		m.baselineCluster2 = cluster
	}

	// Descobre o nome do serviço Prometheus
	serviceName := m.discoverPrometheusService(cluster)

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Str("service", serviceName).
		Msg("🔄 Iniciando port-forward dedicado para baseline")

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
		if port == m.portBaseline1 {
			m.baseline1Busy = false
			m.baselineCluster1 = ""
		} else {
			m.baseline2Busy = false
			m.baselineCluster2 = ""
		}
		return err
	}

	m.forwards[key] = pf

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("✅ Port-forward de baseline ativo")

	return nil
}

// StopPortForward para port-forward em porta específica (para baseline)
func (m *PortForwardManager) StopPortForward(cluster string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%d", cluster, port)
	pf, exists := m.forwards[key]
	if !exists {
		return nil
	}

	if err := pf.Stop(); err != nil {
		return err
	}

	// Libera flag da porta
	if port == m.portBaseline1 {
		m.baseline1Busy = false
		m.baselineCluster1 = ""
	} else if port == m.portBaseline2 {
		m.baseline2Busy = false
		m.baselineCluster2 = ""
	}

	delete(m.forwards, key)

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("🛑 Port-forward de baseline parado")

	return nil
}

// GetBaselinePortAvailable retorna primeira porta baseline disponível (55555 ou 55556)
// Retorna 0 se nenhuma porta disponível
func (m *PortForwardManager) GetBaselinePortAvailable() int {
	if !m.baseline1Busy {
		return m.portBaseline1
	}
	if !m.baseline2Busy {
		return m.portBaseline2
	}
	return 0 // Nenhuma porta disponível
}

// IsBaselinePortBusy verifica se porta baseline está ocupada
func (m *PortForwardManager) IsBaselinePortBusy(port int) bool {
	if port == m.portBaseline1 {
		return m.baseline1Busy
	}
	if port == m.portBaseline2 {
		return m.baseline2Busy
	}
	return false
}

// StartWithPort inicia port-forward DEDICADO PERSISTENTE em porta específica
// Para HPAs prioritários - pool de 6 portas [55551-55556]
func (m *PortForwardManager) StartWithPort(cluster string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verifica se já existe
	key := fmt.Sprintf("%s:%d", cluster, port)
	if pf, exists := m.forwards[key]; exists && pf.IsRunning() {
		log.Info().
			Str("cluster", cluster).
			Int("port", port).
			Msg("Port-forward dedicado já ativo, reutilizando")
		return nil
	}

	serviceName := m.discoverPrometheusService(cluster)

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Str("service", serviceName).
		Msg("🔄 Criando port-forward DEDICADO PERSISTENTE")

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
		return fmt.Errorf("falha ao criar port-forward dedicado: %w", err)
	}

	m.forwards[key] = pf

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("✅ Port-forward DEDICADO criado e PERSISTENTE")

	return nil
}

// StopWithPort para port-forward dedicado
func (m *PortForwardManager) StopWithPort(cluster string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%d", cluster, port)
	pf, exists := m.forwards[key]
	if !exists {
		return nil
	}

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("🛑 Parando port-forward dedicado")

	if err := pf.Stop(); err != nil {
		return err
	}

	delete(m.forwards, key)

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("✅ Port-forward dedicado destruído e porta liberada")

	return nil
}

// StartBaselinePort inicia port-forward TEMPORÁRIO para baseline
// Pool de 2 portas [55557, 55558] - sob demanda
func (m *PortForwardManager) StartBaselinePort(cluster string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Aloca primeira porta disponível
	var port int
	if !m.baseline1Busy {
		port = 55557
		m.baseline1Busy = true
		m.baselineCluster1 = cluster
	} else if !m.baseline2Busy {
		port = 55558
		m.baseline2Busy = true
		m.baselineCluster2 = cluster
	} else {
		return 0, fmt.Errorf("nenhuma porta de baseline disponível (máximo 2 coletas simultâneas)")
	}

	serviceName := m.discoverPrometheusService(cluster)

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Str("service", serviceName).
		Msg("🔄 Criando port-forward TEMPORÁRIO para baseline")

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
		// Libera porta em caso de erro
		if port == 55557 {
			m.baseline1Busy = false
			m.baselineCluster1 = ""
		} else {
			m.baseline2Busy = false
			m.baselineCluster2 = ""
		}
		return 0, fmt.Errorf("falha ao criar port-forward de baseline: %w", err)
	}

	key := fmt.Sprintf("baseline:%s:%d", cluster, port)
	m.forwards[key] = pf

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("✅ Port-forward de baseline ativo (temporário)")

	return port, nil
}

// StopBaselinePort para port-forward de baseline e libera porta
func (m *PortForwardManager) StopBaselinePort(cluster string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("baseline:%s:%d", cluster, port)
	pf, exists := m.forwards[key]
	if !exists {
		return nil
	}

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("🛑 Parando port-forward de baseline")

	if err := pf.Stop(); err != nil {
		return err
	}

	// Libera porta
	if port == 55557 {
		m.baseline1Busy = false
		m.baselineCluster1 = ""
	} else if port == 55558 {
		m.baseline2Busy = false
		m.baselineCluster2 = ""
	}

	delete(m.forwards, key)

	log.Info().
		Str("cluster", cluster).
		Int("port", port).
		Msg("✅ Port-forward de baseline destruído e porta liberada")

	return nil
}
