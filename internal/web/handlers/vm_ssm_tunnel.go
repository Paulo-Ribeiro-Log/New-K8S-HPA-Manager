package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// syncBuffer — bytes.Buffer não é seguro pra acesso concorrente; aqui o subprocesso `aws ssm`
// escreve em Stdout/Stderr (via goroutine interna do pacote exec) enquanto waitTunnelReady lê o
// conteúdo acumulado num loop de polling, ao mesmo tempo — sem essa proteção, `go test -race`
// pegaria uma race real entre a escrita (child->pipe->goroutine do exec) e a leitura (polling).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// vm_ssm_tunnel.go — SFTP sobre SSM Session Manager (pedido explícito do usuário, depois de
// perguntar "como vou fazer sftp usando a conexão ssm?"). SSM Session Manager não fala SFTP
// nativamente — a técnica real da AWS pra isso é abrir um túnel de PORT FORWARDING via
// `aws ssm start-session --document-name AWS-StartPortForwardingSession` (encaminha uma porta
// LOCAL pra uma porta REMOTA da instância, ex: 22, através do canal criptografado do SSM) e então
// falar SSH/SFTP normalmente ATRAVÉS desse túnel — a autenticação SSH continua sendo a mesma de
// sempre (perfil de credencial), o SSM só resolve o alcance de rede (útil quando a instância está
// numa subnet privada sem VPN/rota direta, que é justamente por que SSM é escolhido em vez de SSH
// direto).
//
// Diferente do resto do SFTP desta app (vm_sftp.go, deliberadamente SEM ESTADO — cada operação
// abre SSH+SFTP, faz uma coisa, fecha), o túnel SSM PRECISA ficar vivo entre operações — abrir um
// `aws ssm start-session` novo a cada list/download custaria alguns segundos por chamada. Por
// isso este é o único mecanismo desta app que introduz uma sessão com estado pro fluxo de SFTP —
// exceção deliberada, não um desvio acidental do padrão.

// vmSSMTunnelIdleTimeout/MaxDuration — mesmo espírito de internal/portforward (túneis genéricos
// pra pods): nunca deixar um túnel esquecido vivo pra sempre. Mais curto que lá (SFTP é uma tarefa
// pontual, não uma ferramenta de longa duração como port-forward genérico).
const (
	vmSSMTunnelIdleTimeout   = 15 * time.Minute
	vmSSMTunnelMaxDuration   = 2 * time.Hour
	vmSSMTunnelReapInterval  = 2 * time.Minute
	vmSSMTunnelReadyTimeout  = 20 * time.Second
	vmSSMTunnelReadyPollTick = 300 * time.Millisecond
)

type ssmTunnelSession struct {
	cmd        *exec.Cmd
	localPort  int
	instanceID string
	output     *syncBuffer

	mu        sync.Mutex
	stopped   bool
	createdAt time.Time
	lastUsed  time.Time
}

func (s *ssmTunnelSession) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *ssmTunnelSession) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.cmd.Wait()
}

func (s *ssmTunnelSession) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *ssmTunnelSession) expired(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return true
	}
	return now.Sub(s.lastUsed) > vmSSMTunnelIdleTimeout || now.Sub(s.createdAt) > vmSSMTunnelMaxDuration
}

// VMSSMTunnelManager gerencia túneis de port-forwarding via SSM — um subprocesso `aws ssm
// start-session --document-name AWS-StartPortForwardingSession` por sessão, mapeado por
// sessionID (UUID). Reaper em background varre e mata túneis ociosos/velhos periodicamente,
// mesmo princípio de internal/portforward.Manager.
type VMSSMTunnelManager struct {
	sessions sync.Map // sessionID string -> *ssmTunnelSession
	once     sync.Once
}

func NewVMSSMTunnelManager() *VMSSMTunnelManager {
	return &VMSSMTunnelManager{}
}

func (m *VMSSMTunnelManager) ensureReaper() {
	m.once.Do(func() {
		go func() {
			ticker := time.NewTicker(vmSSMTunnelReapInterval)
			defer ticker.Stop()
			for range ticker.C {
				now := time.Now()
				m.sessions.Range(func(key, value interface{}) bool {
					sess := value.(*ssmTunnelSession)
					if sess.expired(now) {
						sess.stop()
						m.sessions.Delete(key)
					}
					return true
				})
			}
		}()
	})
}

func findFreeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("erro ao alocar porta local livre: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitTunnelReady — bug real corrigido, relatado pelo usuário: "erro no handshake SSH com
// ssm-tunnel-...: ssh: handshake failed: EOF". Causa: a versão anterior só checava se a PORTA
// LOCAL aceitava conexão TCP (waitLocalPortOpen, removida) — mas o plugin `session-manager-plugin`
// abre esse listener local quase imediatamente, ANTES do túnel de ponta a ponta (canal SSM real
// até a instância) terminar de se estabelecer. Conectar exatamente nessa janela faz o plugin
// aceitar a conexão TCP local e depois fechá-la (EOF) assim que percebe que o canal remoto ainda
// não está pronto pra relayar dados — do lado do cliente SSH, isso aparece como "handshake failed:
// EOF" (a conexão caiu no meio da negociação, sem nem completar a troca de versão SSH).
//
// Corrigido usando o sinal real de prontidão que o próprio `aws ssm start-session
// --document-name AWS-StartPortForwardingSession` imprime no stdout/stderr quando o túnel está
// genuinamente pronto ponta a ponta — formato documentado e estável da própria AWS:
//
//	Starting session with SessionId: ...
//	Port <porta> opened for sessionId ....
//	Waiting for connections...
//
// só a linha "Waiting for connections..." confirma que o relay está de fato operacional; "Port ...
// opened" (a mensagem anterior) é exatamente o momento que a versão antiga confundia com "pronto".
func waitTunnelReady(ctx context.Context, port int, output *syncBuffer, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if strings.Contains(strings.ToLower(output.String()), "waiting for connections") {
			return nil
		}
		time.Sleep(vmSSMTunnelReadyPollTick)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return fmt.Errorf("timeout esperando o túnel sinalizar prontidão em %s", addr)
}

// StartTunnel abre um novo túnel de port-forwarding SSM — devolve o sessionID (usado depois pra
// resolver o endereço local via ResolveLocalAddr, e pra StopTunnel). remotePort é a porta NA
// INSTÂNCIA (normalmente 22, o sshd) — nunca a porta local, que é sempre escolhida automaticamente
// (findFreeLocalPort) pra evitar colisão entre sessões concorrentes de usuários diferentes.
func (m *VMSSMTunnelManager) StartTunnel(ctx context.Context, instanceID, profile, region string, remotePort int) (string, error) {
	m.ensureReaper()

	localPort, err := findFreeLocalPort()
	if err != nil {
		return "", err
	}

	args := []string{
		"ssm", "start-session",
		"--target", instanceID,
		"--region", region,
		"--profile", profile,
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", fmt.Sprintf("portNumber=%d,localPortNumber=%d", remotePort, localPort),
	}
	cmd := exec.Command("aws", args...)
	outBuf := &syncBuffer{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("erro ao iniciar túnel SSM: %w", err)
	}

	sess := &ssmTunnelSession{
		cmd:        cmd,
		localPort:  localPort,
		instanceID: instanceID,
		output:     outBuf,
		createdAt:  time.Now(),
		lastUsed:   time.Now(),
	}

	readyCtx, cancel := context.WithTimeout(ctx, vmSSMTunnelReadyTimeout)
	defer cancel()
	if err := waitTunnelReady(readyCtx, localPort, outBuf, vmSSMTunnelReadyTimeout); err != nil {
		sess.stop()
		return "", fmt.Errorf("túnel SSM não ficou pronto a tempo: %w (saída do aws cli: %s)", err, outBuf.String())
	}

	sessionID := uuid.New().String()
	m.sessions.Store(sessionID, sess)

	// Se o subprocesso morrer sozinho depois (sessão SSM revogada, instância parada, etc.), o
	// próximo ResolveLocalAddr detecta via isStopped() — não precisamos de outra goroutine de
	// vigia além do reaper de ociosidade já agendado.
	go func() {
		_ = cmd.Wait()
		sess.mu.Lock()
		sess.stopped = true
		sess.mu.Unlock()
	}()

	return sessionID, nil
}

// ResolveLocalAddr devolve "127.0.0.1:<porta>" pra uma sessão de túnel ativa — usado por
// vm_sftp.go no lugar do host:porta cru quando a requisição vem com tunnelSessionId.
func (m *VMSSMTunnelManager) ResolveLocalAddr(sessionID string) (string, error) {
	v, ok := m.sessions.Load(sessionID)
	if !ok {
		return "", fmt.Errorf("sessão de túnel SSM não encontrada (pode ter expirado por ociosidade — reconecte)")
	}
	sess := v.(*ssmTunnelSession)
	if sess.isStopped() {
		m.sessions.Delete(sessionID)
		return "", fmt.Errorf("túnel SSM já foi encerrado")
	}
	sess.touch()
	return fmt.Sprintf("127.0.0.1:%d", sess.localPort), nil
}

// RecentOutput devolve a saída acumulada (stdout+stderr) do subprocesso `aws ssm` de uma sessão —
// usado por vm_sftp.go pra diagnosticar POR QUE um handshake SSH falhou logo após o túnel abrir.
// Achado real, validado ao vivo (não hipótese): quando o túnel está genuinamente pronto (já viu
// "Waiting for connections...") mas a INSTÂNCIA não tem nada escutando na porta de destino (sshd
// não roda ali, ou tá bloqueado por firewall interno), o `session-manager-plugin` aceita a conexão
// TCP local normalmente e só DEPOIS imprime "Connection to destination port failed..." — do lado
// do cliente SSH isso é indistinguível de um túnel-ainda-não-pronto (os dois se manifestam como
// "handshake failed: EOF", a conexão simplesmente cai sem completar a negociação). Reaproveitar
// esse texto já capturado permite diferenciar os dois casos e dar um erro muito mais acionável.
func (m *VMSSMTunnelManager) RecentOutput(sessionID string) string {
	v, ok := m.sessions.Load(sessionID)
	if !ok {
		return ""
	}
	return v.(*ssmTunnelSession).output.String()
}

// StopTunnel encerra um túnel explicitamente — chamado quando o usuário fecha o modal de SFTP ou
// clica em "Desconectar". Idempotente: sessão já removida não é erro (evita corrida entre o
// reaper e um clique manual quase simultâneo).
func (m *VMSSMTunnelManager) StopTunnel(sessionID string) {
	v, ok := m.sessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	v.(*ssmTunnelSession).stop()
}

// StopAll — chamado no shutdown do servidor (mesmo princípio de internal/portforward.Manager e
// teams.CloseBrowser()) pra nunca deixar subprocesso `aws ssm` órfão quando o processo do
// servidor termina.
func (m *VMSSMTunnelManager) StopAll() {
	m.sessions.Range(func(key, value interface{}) bool {
		value.(*ssmTunnelSession).stop()
		m.sessions.Delete(key)
		return true
	})
}

// --- Handlers HTTP ---

type startSSMTunnelRequest struct {
	Profile    string `json:"profile" binding:"required"`
	Region     string `json:"region" binding:"required"`
	RemotePort int    `json:"remotePort"` // 0 = usa 22 (sshd padrão)
}

// StartSSMTunnel — POST /api/v1/vms/:instanceId/ssm-tunnel/start
func (h *VMSFTPHandler) StartSSMTunnel(c *gin.Context) {
	instanceID := c.Param("instanceId")
	var req startSSMTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("requisição inválida: %v", err)},
		})
		return
	}
	if req.RemotePort <= 0 {
		req.RemotePort = 22
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), vmSSMTunnelReadyTimeout+5*time.Second)
	defer cancel()
	sessionID, err := h.ssmTunnels.StartTunnel(ctx, instanceID, req.Profile, req.Region, req.RemotePort)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSM_TUNNEL_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"sessionId": sessionID}})
}

type stopSSMTunnelRequest struct {
	SessionID string `json:"sessionId" binding:"required"`
}

// StopSSMTunnel — POST /api/v1/vms/:instanceId/ssm-tunnel/stop
func (h *VMSFTPHandler) StopSSMTunnel(c *gin.Context) {
	var req stopSSMTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("requisição inválida: %v", err)},
		})
		return
	}
	h.ssmTunnels.StopTunnel(req.SessionID)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
