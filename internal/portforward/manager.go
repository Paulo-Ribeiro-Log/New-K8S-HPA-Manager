// Package portforward implementa um gerenciador de sessões de port-forward pra pods K8s,
// expostas ao usuário via API + modal no frontend (diferente de internal/web/handlers/portforward.go,
// que é infraestrutura antiga e não relacionada — específica do pod do Kiali via `kubectl
// port-forward` como subprocesso — e de internal/config/portforward.go, que é um cache INTERNO de
// túneis usado só pra proxy de requisições do próprio servidor, sempre contra um Service, sem
// controle de porta local/bind address, sem estatísticas, sem start/stop explícito pelo usuário).
//
// Mecanismo: reaproveita o mesmo túnel SPDY do client-go (k8s.io/client-go/tools/portforward,
// idêntico ao usado por `kubectl port-forward`) internamente, sempre vinculado em
// 127.0.0.1:<porta efêmera> — mas em vez de expor essa porta efêmera diretamente pro usuário (que
// ficaria presa a 127.0.0.1, inacessível de outra máquina — problema real em cenários WSL2
// servidor + browser Windows, ou o servidor acessado remotamente por outros usuários), este
// pacote abre um listener TCP PRÓPRIO (endereço/porta escolhidos pelo usuário, padrão 0.0.0.0
// pra ficar acessível de qualquer host que já alcance o servidor) e faz proxy bidirecional
// (io.Copy) entre esse listener e o túnel SPDY interno — ganha também visibilidade total por
// conexão (bytes trafegados, contagem de conexões, última atividade), que a biblioteca do
// client-go não expõe.
package portforward

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// Status representa o estado de uma sessão de port-forward.
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusError    Status = "error"
	StatusStopped  Status = "stopped"
)

const (
	// IdleTimeout — sessão sem nenhuma conexão nova aceita nesse período é encerrada
	// automaticamente (rede de segurança contra túnel esquecido aberto — mesmo espírito do
	// clientTTL de internal/config/kubeconfig.go). Reiniciado a cada nova conexão aceita, não
	// por tráfego dentro de uma conexão já aberta (uma conexão longa/streaming legítima não deve
	// ser derrubada só por isso).
	IdleTimeout = 60 * time.Minute
	// MaxDuration — teto absoluto de vida de uma sessão, mesmo com uso ativo constante.
	MaxDuration = 8 * time.Hour
	// retentionAfterStop — quanto tempo uma sessão encerrada (Stopped/Error) continua visível na
	// lista antes de ser removida — dá tempo do usuário ver o motivo do encerramento sem acumular
	// entradas mortas indefinidamente.
	retentionAfterStop = 15 * time.Minute
	cleanupEvery       = 1 * time.Minute
	readyTimeout       = 15 * time.Second
)

// BindAddress — únicos dois valores aceitos, deliberadamente restrito (não aceita qualquer IP
// arbitrário) pra evitar exposição acidental numa interface de rede inesperada num servidor
// compartilhado.
const (
	BindAll   = "0.0.0.0"   // acessível de qualquer host que já alcance o servidor
	BindLocal = "127.0.0.1" // só a partir da própria máquina do servidor
)

// KubeConfigGetter é o subconjunto de *config.KubeConfigManager que este pacote precisa.
type KubeConfigGetter interface {
	GetClient(cluster string) (kubernetes.Interface, error)
	GetRestConfig(cluster string) (*rest.Config, error)
}

// StartOptions descreve uma sessão de port-forward a ser aberta.
type StartOptions struct {
	Cluster     string
	Namespace   string
	Pod         string
	Container   string // opcional, só pra exibição — port-forward do K8s não é por container
	Workload    string // opcional, ex: "Deployment/checkout-api" — só pra exibição
	RemotePort  int
	LocalPort   int    // 0 = escolhido automaticamente pelo SO
	BindAddress string // "" = BindAll
	Label       string
	CreatedBy   string
}

// SessionInfo é a projeção pública (JSON) de uma Session — nunca expõe os campos internos de
// sincronização (mutex, channels, listener, conexões ativas).
type SessionInfo struct {
	ID                string     `json:"id"`
	Cluster           string     `json:"cluster"`
	Namespace         string     `json:"namespace"`
	Pod               string     `json:"pod"`
	Workload          string     `json:"workload,omitempty"`
	Container         string     `json:"container,omitempty"`
	RemotePort        int        `json:"remote_port"`
	LocalPort         int        `json:"local_port"`
	BindAddress       string     `json:"bind_address"`
	Label             string     `json:"label,omitempty"`
	Status            Status     `json:"status"`
	Error             string     `json:"error,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	CreatedBy         string     `json:"created_by,omitempty"`
	StoppedAt         *time.Time `json:"stopped_at,omitempty"`
	ConnectionsTotal  int64      `json:"connections_total"`
	ConnectionsActive int64      `json:"connections_active"`
	// BytesSent/BytesReceived são do ponto de vista do CLIENTE LOCAL que conecta na porta
	// encaminhada (como um monitor de rede comum: ↑ enviado, ↓ recebido) — Sent = cliente→pod,
	// Received = pod→cliente.
	BytesSent     int64      `json:"bytes_sent"`
	BytesReceived int64      `json:"bytes_received"`
	LastActivity  *time.Time `json:"last_activity,omitempty"`
}

// session é o estado interno completo de uma sessão — nunca serializado diretamente (ver
// SessionInfo/snapshot()).
type session struct {
	info SessionInfo // campos protegidos por mu (Status/Error/StoppedAt) OU só escritos 1x antes de publicar (o resto)
	mu   sync.Mutex

	connectionsTotal  int64 // atomic
	connectionsActive int64 // atomic
	bytesSent         int64 // atomic
	bytesReceived     int64 // atomic
	lastActivityUnix  int64 // atomic, unix nano

	stopCh      chan struct{}
	stopOnce    sync.Once
	listener    net.Listener
	activeConns sync.Map // net.Conn -> struct{}
}

func (s *session) touch() {
	atomic.StoreInt64(&s.lastActivityUnix, time.Now().UnixNano())
}

func (s *session) snapshot() SessionInfo {
	s.mu.Lock()
	info := s.info
	s.mu.Unlock()

	info.ConnectionsTotal = atomic.LoadInt64(&s.connectionsTotal)
	info.ConnectionsActive = atomic.LoadInt64(&s.connectionsActive)
	info.BytesSent = atomic.LoadInt64(&s.bytesSent)
	info.BytesReceived = atomic.LoadInt64(&s.bytesReceived)
	if ts := atomic.LoadInt64(&s.lastActivityUnix); ts > 0 {
		t := time.Unix(0, ts)
		info.LastActivity = &t
	}
	return info
}

// terminate encerra a sessão (idempotente — pode ser chamado tanto por Stop() explícito quanto
// pela goroutine que detecta o túnel SPDY caindo sozinho; o primeiro a chegar decide o
// status/motivo final, o segundo é um no-op silencioso via sync.Once).
func (s *session) terminate(status Status, errMsg string) {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.activeConns.Range(func(k, _ any) bool {
			if c, ok := k.(net.Conn); ok {
				_ = c.Close()
			}
			return true
		})

		s.mu.Lock()
		s.info.Status = status
		s.info.Error = errMsg
		now := time.Now()
		s.info.StoppedAt = &now
		s.mu.Unlock()
	})
}

// Manager gerencia o ciclo de vida de todas as sessões de port-forward ativas no processo.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*session

	kubeManager KubeConfigGetter
	cleanupOnce sync.Once
}

func NewManager(km KubeConfigGetter) *Manager {
	m := &Manager{sessions: make(map[string]*session), kubeManager: km}
	m.ensureCleanup()
	return m
}

func (m *Manager) ensureCleanup() {
	m.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(cleanupEvery)
			defer ticker.Stop()
			for range ticker.C {
				m.runCleanup()
			}
		}()
	})
}

func (m *Manager) runCleanup() {
	now := time.Now()

	m.mu.RLock()
	snapshot := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		snapshot = append(snapshot, s)
	}
	m.mu.RUnlock()

	toDelete := make([]string, 0)
	for _, s := range snapshot {
		info := s.snapshot()
		switch info.Status {
		case StatusRunning:
			idleSince := info.CreatedAt
			if info.LastActivity != nil {
				idleSince = *info.LastActivity
			}
			if now.Sub(idleSince) > IdleTimeout {
				s.terminate(StatusStopped, fmt.Sprintf("encerrado automaticamente: ocioso há mais de %s", IdleTimeout))
			} else if now.Sub(info.CreatedAt) > MaxDuration {
				s.terminate(StatusStopped, fmt.Sprintf("encerrado automaticamente: duração máxima de %s atingida", MaxDuration))
			}
		case StatusStopped, StatusError:
			if info.StoppedAt != nil && now.Sub(*info.StoppedAt) > retentionAfterStop {
				toDelete = append(toDelete, info.ID)
			}
		}
	}

	if len(toDelete) > 0 {
		m.mu.Lock()
		for _, id := range toDelete {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
	}
}

// Start abre uma nova sessão de port-forward. Bloqueia até o túnel estar pronto (ou timeout) —
// chamador deve rodar em goroutine/handler HTTP normal, não é instantâneo (handshake SPDY real).
func (m *Manager) Start(ctx context.Context, opts StartOptions) (SessionInfo, error) {
	if opts.Cluster == "" || opts.Namespace == "" || opts.Pod == "" {
		return SessionInfo{}, fmt.Errorf("cluster, namespace e pod são obrigatórios")
	}
	if opts.RemotePort < 1 || opts.RemotePort > 65535 {
		return SessionInfo{}, fmt.Errorf("porta remota inválida: %d", opts.RemotePort)
	}
	if opts.LocalPort != 0 && (opts.LocalPort < 1 || opts.LocalPort > 65535) {
		return SessionInfo{}, fmt.Errorf("porta local inválida: %d", opts.LocalPort)
	}
	bindAddr := opts.BindAddress
	if bindAddr == "" {
		bindAddr = BindAll
	}
	if bindAddr != BindAll && bindAddr != BindLocal {
		return SessionInfo{}, fmt.Errorf("bind address não suportado: %q (use %q ou %q)", bindAddr, BindAll, BindLocal)
	}

	clientset, err := m.kubeManager.GetClient(opts.Cluster)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("falha ao obter client K8s: %w", err)
	}
	restConfig, err := m.kubeManager.GetRestConfig(opts.Cluster)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("falha ao obter rest config: %w", err)
	}

	pod, err := clientset.CoreV1().Pods(opts.Namespace).Get(ctx, opts.Pod, metav1.GetOptions{})
	if err != nil {
		return SessionInfo{}, fmt.Errorf("falha ao obter pod %s/%s: %w", opts.Namespace, opts.Pod, err)
	}
	if pod.Status.Phase != corev1.PodRunning {
		return SessionInfo{}, fmt.Errorf("pod %s/%s não está Running (fase atual: %s)", opts.Namespace, opts.Pod, pod.Status.Phase)
	}

	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("falha ao criar transporte SPDY: %w", err)
	}
	reqURL := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(opts.Namespace).
		Name(opts.Pod).
		SubResource("portforward").
		URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, reqURL)

	s := &session{
		info: SessionInfo{
			ID:          uuid.NewString(),
			Cluster:     opts.Cluster,
			Namespace:   opts.Namespace,
			Pod:         opts.Pod,
			Workload:    opts.Workload,
			Container:   opts.Container,
			RemotePort:  opts.RemotePort,
			BindAddress: bindAddr,
			Label:       opts.Label,
			Status:      StatusStarting,
			CreatedAt:   time.Now(),
			CreatedBy:   opts.CreatedBy,
		},
		stopCh: make(chan struct{}),
	}

	readyCh := make(chan struct{})
	var errOut strings.Builder
	pf, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", opts.RemotePort)}, s.stopCh, readyCh, io.Discard, &errOut)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("falha ao criar port-forwarder: %w", err)
	}

	go func() {
		fwErr := pf.ForwardPorts()
		msg := ""
		if fwErr != nil {
			msg = fwErr.Error()
		}
		if stderr := strings.TrimSpace(errOut.String()); stderr != "" {
			if msg != "" {
				msg += " — "
			}
			msg += "stderr: " + stderr
		}
		if msg == "" {
			msg = "túnel encerrado"
		}
		// Idempotente via sync.Once: se a sessão já foi parada explicitamente (Stop) ou nunca
		// chegou a ficar Running (falha no listener local, ver abaixo), esta chamada é um no-op.
		s.terminate(StatusError, msg)
	}()

	select {
	case <-readyCh:
	case <-time.After(readyTimeout):
		s.terminate(StatusError, fmt.Sprintf("timeout (%s) esperando túnel pro pod ficar pronto: %s", readyTimeout, errOut.String()))
		return SessionInfo{}, fmt.Errorf("timeout esperando túnel SPDY ficar pronto")
	case <-ctx.Done():
		s.terminate(StatusError, "cancelado pelo chamador")
		return SessionInfo{}, ctx.Err()
	}

	ports, err := pf.GetPorts()
	if err != nil || len(ports) == 0 {
		s.terminate(StatusError, "falha ao obter porta local do túnel interno")
		return SessionInfo{}, fmt.Errorf("falha ao obter porta local do túnel: %w", err)
	}
	tunnelPort := int(ports[0].Local)

	ln, err := net.Listen("tcp", net.JoinHostPort(bindAddr, strconv.Itoa(opts.LocalPort)))
	if err != nil {
		s.terminate(StatusError, "falha ao abrir listener local: "+err.Error())
		return SessionInfo{}, fmt.Errorf("falha ao abrir porta local %d em %s (porta em uso?): %w", opts.LocalPort, bindAddr, err)
	}
	s.listener = ln

	s.mu.Lock()
	s.info.LocalPort = ln.Addr().(*net.TCPAddr).Port
	s.info.Status = StatusRunning
	s.mu.Unlock()

	m.mu.Lock()
	m.sessions[s.info.ID] = s
	m.mu.Unlock()

	go m.acceptLoop(s, tunnelPort)

	log.Info().
		Str("id", s.info.ID).
		Str("cluster", opts.Cluster).
		Str("namespace", opts.Namespace).
		Str("pod", opts.Pod).
		Int("remote_port", opts.RemotePort).
		Int("local_port", s.info.LocalPort).
		Str("bind", bindAddr).
		Msg("Port-forward iniciado")

	return s.snapshot(), nil
}

func (m *Manager) acceptLoop(s *session, tunnelPort int) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			break
		}
		atomic.AddInt64(&s.connectionsTotal, 1)
		atomic.AddInt64(&s.connectionsActive, 1)
		s.touch()
		go m.proxyConn(s, conn, tunnelPort)
	}
	// Se o listener caiu sem passar por terminate() (ex: erro de I/O inesperado no listener em
	// si, não coberto pelo caminho normal de Stop/queda do túnel), garante que a sessão não fique
	// "running" fantasma na lista.
	s.terminate(StatusError, "listener local encerrado inesperadamente")
}

func (m *Manager) proxyConn(s *session, localConn net.Conn, tunnelPort int) {
	s.activeConns.Store(localConn, struct{}{})
	defer func() {
		s.activeConns.Delete(localConn)
		_ = localConn.Close()
		atomic.AddInt64(&s.connectionsActive, -1)
	}()

	remoteConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort), 10*time.Second)
	if err != nil {
		log.Warn().Err(err).Str("session", s.info.ID).Msg("Port-forward: falha ao conectar no túnel SPDY interno")
		return
	}
	defer remoteConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&countingWriter{w: remoteConn, counter: &s.bytesSent, s: s}, localConn)
		if tc, ok := remoteConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&countingWriter{w: localConn, counter: &s.bytesReceived, s: s}, remoteConn)
		if tc, ok := localConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	wg.Wait()
}

// countingWriter contabiliza bytes trafegados e atualiza a última atividade da sessão — usado
// como destino de io.Copy nas duas direções do proxy.
type countingWriter struct {
	w       io.Writer
	counter *int64
	s       *session
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		atomic.AddInt64(c.counter, int64(n))
		c.s.touch()
	}
	return n, err
}

// Stop encerra uma sessão explicitamente (ação do usuário).
func (m *Manager) Stop(id, reason string) error {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("sessão %q não encontrada", id)
	}
	if reason == "" {
		reason = "parado pelo usuário"
	}
	s.terminate(StatusStopped, reason)
	return nil
}

// StopAll encerra todas as sessões — chamado nos caminhos de shutdown do servidor.
func (m *Manager) StopAll() {
	m.mu.RLock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()
	for _, s := range sessions {
		s.terminate(StatusStopped, "servidor encerrando")
	}
}

// List retorna todas as sessões (ativas e recentemente encerradas, dentro de retentionAfterStop),
// mais recentes primeiro.
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s.snapshot())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// Get retorna uma sessão específica.
func (m *Manager) Get(id string) (SessionInfo, bool) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return SessionInfo{}, false
	}
	return s.snapshot(), true
}
