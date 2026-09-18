package handlers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
)

// newTestGinContextWithQuery — mesmo padrão já usado em clusters_test.go/sreapproval_test.go
// (gin.CreateTestContext + httptest.NewRequest), só que populando query params arbitrários.
func newTestGinContextWithQuery(t *testing.T, query map[string]string) *gin.Context {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	v := url.Values{}
	for k, val := range query {
		v.Set(k, val)
	}
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+v.Encode(), nil)
	return c
}

func TestExtractUnknownHostKeyFingerprint(t *testing.T) {
	fp := "SHA256:AbCdEf1234567890+/=="

	cases := []struct {
		name      string
		err       error
		wantFound bool
		wantFP    string
	}{
		{
			name:      "sentinel puro",
			err:       fmt.Errorf("%s%s", hostKeyUnknownSentinel, fp),
			wantFound: true,
			wantFP:    fp,
		},
		{
			name:      "envolto por wrapping externo (handshake SSH)",
			err:       fmt.Errorf("erro no handshake SSH com ssm-tunnel-i-0123:0: %s%s", hostKeyUnknownSentinel, fp),
			wantFound: true,
			wantFP:    fp,
		},
		{
			name:      "fingerprint nunca deve ser truncada no primeiro ':' (formato SHA256:base64)",
			err:       fmt.Errorf("%sSHA256:xyz==", hostKeyUnknownSentinel),
			wantFound: true,
			wantFP:    "SHA256:xyz==",
		},
		{
			name:      "erro comum sem sentinel",
			err:       fmt.Errorf("dial tcp 10.0.0.1:22: connection refused"),
			wantFound: false,
		},
		{
			name:      "nil",
			err:       nil,
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotFP, gotFound := extractUnknownHostKeyFingerprint(tc.err)
			if gotFound != tc.wantFound {
				t.Fatalf("found = %v, esperado %v (err=%v)", gotFound, tc.wantFound, tc.err)
			}
			if tc.wantFound && gotFP != tc.wantFP {
				t.Errorf("fingerprint = %q, esperado %q", gotFP, tc.wantFP)
			}
		})
	}
}

func TestSftpTargetLabel_NuncaRecursivo(t *testing.T) {
	// Regressão real: um sed mal aplicado durante o desenvolvimento desta feature substituiu a
	// própria implementação de sftpTargetLabel por uma chamada a si mesma (recursão infinita,
	// stack overflow em runtime — só descoberto por inspeção manual do diff, não por este teste
	// na hora, mas travado aqui pra nunca mais regredir despercebido). Sem um gin.Context real
	// mockado, o teste mais simples e robusto é só confirmar que a função COMPILA e RETORNA algo
	// determinístico pro caso trivial (sem precisar de um *gin.Context de verdade) — feito
	// indiretamente via um teste de smoke no pacote inteiro (go vet/go build já pegam recursão
	// óbvia, mas um teste que de fato CHAMA a função garante que ela retorna, não trava).
	//
	// gin.Context exige um httptest.ResponseRecorder + *http.Request mínimos pra construir; como
	// sftpTargetLabel só lê query params, isso é barato de montar aqui.
	c := newTestGinContextWithQuery(t, map[string]string{"host": "10.0.0.1", "port": "22"})
	if got := sftpTargetLabel(c); got != "10.0.0.1:22" {
		t.Fatalf("sftpTargetLabel (modo host) = %q, esperado 10.0.0.1:22", got)
	}

	c2 := newTestGinContextWithQuery(t, map[string]string{"tunnelSessionId": "abc-123"})
	if got := sftpTargetLabel(c2); got != "ssm-tunnel:abc-123" {
		t.Fatalf("sftpTargetLabel (modo túnel) = %q, esperado ssm-tunnel:abc-123", got)
	}
}

// startTestSSHExecServer sobe um servidor SSH real (não mock) em 127.0.0.1, só pra validar
// VMFileSession.RunCommand contra o protocolo de verdade — mesmo espírito das demais suítes desta
// ferramenta (fixture real capturada, servidor httptest real, CLI fake no PATH). Só lida com
// requisições "exec" (não abre um shell interativo nem SFTP) — o suficiente pro que RunCommand usa.
// O comando recebido decide o comportamento simulado: prefixo "exit1:" escreve em stderr e sai com
// status 1 (simula `systemctl restart` falhando), prefixo "sleep:" nunca responde (simula travamento
// — usado pra testar o timeout via ctx), qualquer outra coisa escreve o próprio comando em stdout e
// sai com status 0 (confirma que a string chegou intacta no servidor).
func startTestSSHExecServer(t *testing.T) (addr string, closeFn func()) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gerar chave do host: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("criar signer: %v", err)
	}

	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleTestSSHConn(conn, config)
		}
	}()

	closeFn = func() {
		_ = listener.Close()
		close(done)
	}
	return listener.Addr().String(), closeFn
}

func handleTestSSHConn(conn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go handleTestSSHSession(channel, requests)
	}
}

func handleTestSSHSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for req := range requests {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		// Payload de um "exec" request: uint32 length + string do comando (formato SSH_MSG_CHANNEL_REQUEST).
		var payload struct{ Command string }
		_ = ssh.Unmarshal(req.Payload, &payload)
		if req.WantReply {
			_ = req.Reply(true, nil)
		}

		cmd := payload.Command
		switch {
		case len(cmd) >= 6 && cmd[:6] == "exit1:":
			_, _ = channel.Stderr().Write([]byte(cmd[6:]))
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{1}))
		case len(cmd) >= 6 && cmd[:6] == "sleep:":
			// Nunca responde nem fecha — só o cancelamento do ctx do cliente deve destravar o teste.
			select {}
		default:
			_, _ = channel.Write([]byte(cmd))
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		}
		return
	}
}

func dialTestSSH(t *testing.T, addr string) *ssh.Client {
	t.Helper()
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial SSH de teste: %v", err)
	}
	return client
}

func TestVMFileSession_RunCommand_SucessoDevolveStdoutEExitCodeZero(t *testing.T) {
	addr, closeFn := startTestSSHExecServer(t)
	defer closeFn()
	client := dialTestSSH(t, addr)
	defer client.Close()

	sess := &VMFileSession{sess: &vmSFTPSession{ssh: client}}
	result, err := sess.RunCommand(context.Background(), "sudo -n systemctl restart nginx")
	if err != nil {
		t.Fatalf("RunCommand retornou erro: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, esperado 0", result.ExitCode)
	}
	if result.Stdout != "sudo -n systemctl restart nginx" {
		t.Fatalf("Stdout = %q, esperava o comando ecoado de volta (confirma que chegou intacto no servidor)", result.Stdout)
	}
}

func TestVMFileSession_RunCommand_ExitCodeNaoZeroNaoEErroGo(t *testing.T) {
	addr, closeFn := startTestSSHExecServer(t)
	defer closeFn()
	client := dialTestSSH(t, addr)
	defer client.Close()

	sess := &VMFileSession{sess: &vmSFTPSession{ssh: client}}
	result, err := sess.RunCommand(context.Background(), "exit1:Job for nginx.service failed")
	if err != nil {
		// Confirma o contrato documentado: um exit code != 0 é resultado normal, não um erro Go —
		// o chamador (RestartService) decide a mensagem a partir do conteúdo, não de um err != nil.
		t.Fatalf("RunCommand retornou erro Go pra um exit code != 0: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, esperado 1", result.ExitCode)
	}
	if result.Stderr != "Job for nginx.service failed" {
		t.Fatalf("Stderr = %q, esperava a mensagem real do comando", result.Stderr)
	}
}

func TestVMFileSession_RunCommand_ContextoCanceladoInterrompe(t *testing.T) {
	addr, closeFn := startTestSSHExecServer(t)
	defer closeFn()
	client := dialTestSSH(t, addr)
	defer client.Close()

	sess := &VMFileSession{sess: &vmSFTPSession{ssh: client}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := sess.RunCommand(ctx, "sleep:never responds")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperava erro de contexto cancelado, RunCommand retornou sucesso")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("RunCommand demorou %v pra respeitar o timeout de 200ms — não deveria travar esperando o comando", elapsed)
	}
}

func TestRestartServiceCommand_SystemdEQuotaONomeDoServico(t *testing.T) {
	got := restartServiceCommand("nginx", "")
	want := "sudo -n systemctl restart 'nginx'"
	if got != want {
		t.Errorf("restartServiceCommand(nginx, \"\") = %q, esperado %q", got, want)
	}
}

func TestRestartServiceCommand_SysvUsaServiceRestart(t *testing.T) {
	got := restartServiceCommand("apache2", "sysv")
	want := "sudo -n service 'apache2' restart"
	if got != want {
		t.Errorf("restartServiceCommand(apache2, sysv) = %q, esperado %q", got, want)
	}
}

func TestRestartServiceCommand_NomeComCaractereEspecialNuncaVazaComoComandoSeparado(t *testing.T) {
	// Mesma proteção já validada em TestShellQuote (ssm_command_test.go) — aqui confirma que o
	// campo livre de nome de serviço passa por ela antes de virar parte da linha de comando.
	got := restartServiceCommand("nginx; rm -rf /", "")
	want := `sudo -n systemctl restart 'nginx; rm -rf /'`
	if got != want {
		t.Errorf("restartServiceCommand com caractere especial = %q, esperado %q (tudo dentro de UM argumento)", got, want)
	}
}

func TestServiceStatusCommand_SystemdUsaIsActive(t *testing.T) {
	got := serviceStatusCommand("haproxy", "")
	want := "sudo -n systemctl is-active 'haproxy'"
	if got != want {
		t.Errorf("serviceStatusCommand(haproxy, \"\") = %q, esperado %q", got, want)
	}
}

func TestServiceStatusCommand_SysvUsaServiceStatus(t *testing.T) {
	got := serviceStatusCommand("httpd", "sysv")
	want := "sudo -n service 'httpd' status"
	if got != want {
		t.Errorf("serviceStatusCommand(httpd, sysv) = %q, esperado %q", got, want)
	}
}

func TestRestartFailureMessage_PrefereStderr(t *testing.T) {
	got := restartFailureMessage(&RemoteCommandResult{Stdout: "log normal", Stderr: "erro real", ExitCode: 1})
	if got != "erro real" {
		t.Errorf("restartFailureMessage = %q, esperava priorizar Stderr", got)
	}
}

func TestRestartFailureMessage_CaiParaStdoutQuandoStderrVazio(t *testing.T) {
	got := restartFailureMessage(&RemoteCommandResult{Stdout: "algo saiu no stdout mesmo", ExitCode: 3})
	if got != "algo saiu no stdout mesmo" {
		t.Errorf("restartFailureMessage = %q, esperava cair pro Stdout", got)
	}
}

func TestRestartFailureMessage_GenericoQuandoAmbosVazios(t *testing.T) {
	got := restartFailureMessage(&RemoteCommandResult{ExitCode: 127})
	want := "comando terminou com código 127"
	if got != want {
		t.Errorf("restartFailureMessage = %q, esperado %q", got, want)
	}
}
