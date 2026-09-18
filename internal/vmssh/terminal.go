package vmssh

import (
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// TerminalSession abstrai um pty remoto aberto via SSH (RequestPty + Shell) atrás de um
// io.ReadWriter simples + Resize — mesmo papel que github.com/creack/pty cumpre pro terminal
// local do Code Editor (internal/web/handlers/code_editor_terminal.go), permitindo ao handler WS
// (vm_terminal.go) usar exatamente o mesmo protocolo JSON sem saber a diferença.
type TerminalSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

// OpenTerminal abre uma sessão de shell interativo com pty remoto. cols/rows são o tamanho
// inicial (o frontend manda um "resize" logo após conectar, mesmo padrão do terminal do Code
// Editor — o valor aqui só evita um pty com tamanho indefinido nos primeiros bytes).
func OpenTerminal(client *ssh.Client, cols, rows uint16) (*TerminalSession, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir sessão SSH: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", int(rows), int(cols), modes); err != nil {
		sess.Close()
		return nil, fmt.Errorf("erro ao solicitar pty remoto: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("erro ao abrir stdin da sessão: %w", err)
	}
	// StdoutPipe basta — com pty alocado, o shell remoto tem stdout/stderr conectados ao MESMO
	// descritor de pty (comportamento POSIX padrão de um pty real), então não existe um stream de
	// stderr separado a capturar aqui (diferente de exec sem pty, onde SSH usa um canal de
	// "extended data" próprio pra stderr).
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("erro ao abrir stdout da sessão: %w", err)
	}

	if err := sess.Shell(); err != nil {
		sess.Close()
		return nil, fmt.Errorf("erro ao iniciar shell remoto: %w", err)
	}

	return &TerminalSession{client: client, session: sess, stdin: stdin, stdout: stdout}, nil
}

func (s *TerminalSession) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *TerminalSession) Write(p []byte) (int, error) { return s.stdin.Write(p) }

// Resize redimensiona o pty remoto — equivalente a pty.Setsize (github.com/creack/pty) usado pelo
// terminal local do Code Editor.
func (s *TerminalSession) Resize(cols, rows uint16) error {
	return s.session.WindowChange(int(rows), int(cols))
}

// Close encerra a sessão e a conexão SSH. Idempotente o bastante pra ser chamado via defer mesmo
// que a sessão já tenha caído sozinha (erros de Close() de uma conexão já fechada são ignorados
// pelo chamador, mesmo padrão do defer ptmx.Close() em code_editor_terminal.go).
func (s *TerminalSession) Close() error {
	s.session.Close()
	return s.client.Close()
}
