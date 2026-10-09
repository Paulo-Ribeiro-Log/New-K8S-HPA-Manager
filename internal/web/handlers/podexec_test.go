package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/remotecommand"
)

func TestIsWatchdogDebugContainer(t *testing.T) {
	if !isWatchdogDebugContainer(corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Command: debugWatchdogCommand}}) {
		t.Error("container com o laço de vigia deveria ser reaproveitável")
	}
	if isWatchdogDebugContainer(corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Command: []string{"/bin/sh"}}}) {
		t.Error("container antigo (/bin/sh) não deveria ser reaproveitado")
	}
}

func TestNextDebugContainerName(t *testing.T) {
	pod := &corev1.Pod{}
	if got := nextDebugContainerName(pod); got != "k8s-hpa-test-debug" {
		t.Errorf("pod sem debug: got %q", got)
	}
	pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
		{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "k8s-hpa-test-debug"}},
		{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "k8s-hpa-test-debug-2"}},
		{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug-1700000000"}},
	}
	if got := nextDebugContainerName(pod); got != "k8s-hpa-test-debug-3" {
		t.Errorf("com -1 e -2 usados: got %q", got)
	}
}

func TestMarkActivityThrottlesHeartbeat(t *testing.T) {
	calls := 0
	s := &TerminalSession{onHeartbeat: func() { calls++ }}
	for i := 0; i < 50; i++ {
		s.markActivity()
	}
	if calls != 1 {
		t.Fatalf("heartbeat deveria disparar 1x dentro do throttle, disparou %d", calls)
	}
	s.lastHeartbeat = time.Now().Add(-debugHeartbeatThrottle)
	s.markActivity()
	if calls != 2 {
		t.Fatalf("heartbeat deveria disparar de novo após o throttle, total %d", calls)
	}
	if s.idleFor() > time.Second {
		t.Error("idleFor deveria ser ~0 logo após atividade")
	}
}

// Roda os scripts de verdade na imagem usada pelo frontend (nicolaka/netshoot). Pulado sem
// Docker ou sem a imagem local.
func TestDebugWatchdogScriptsNetshoot(t *testing.T) {
	const image = "nicolaka/netshoot:v0.12"
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("imagem %s indisponível: %v", image, err)
	}
	start := func(t *testing.T) string {
		args := append([]string{"run", "-d", "--rm", "--entrypoint", debugWatchdogCommand[0], image}, debugWatchdogCommand[1:]...)
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker run: %v: %s", err, out)
		}
		id := strings.TrimSpace(string(out))
		t.Cleanup(func() { exec.Command("docker", "rm", "-f", id).Run() })
		return id
	}
	sh := func(t *testing.T, id, script string) {
		if out, err := exec.Command("docker", "exec", id, "sh", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("docker exec %q: %v: %s", script, err, out)
		}
	}
	running := func(id string) bool {
		out, _ := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", id).Output()
		return strings.TrimSpace(string(out)) == "true"
	}
	waitExit := func(t *testing.T, id string) {
		for i := 0; i < 25; i++ {
			if !running(id) {
				return
			}
			time.Sleep(time.Second)
		}
		t.Fatal("container deveria ter saído em até ~15s")
	}

	t.Run("para com /tmp/.stop", func(t *testing.T) {
		id := start(t)
		sh(t, id, debugTouchHeartbeat)
		time.Sleep(2 * time.Second)
		if !running(id) {
			t.Fatal("container deveria estar rodando com heartbeat fresco")
		}
		sh(t, id, debugStopUnconditional)
		waitExit(t, id)
	})

	t.Run("ociosidade não para com outra sessão ativa", func(t *testing.T) {
		id := start(t)
		sh(t, id, debugTouchHeartbeat)
		sh(t, id, debugStopIfNoOtherSession)
		time.Sleep(17 * time.Second)
		if !running(id) {
			t.Fatal("heartbeat fresco (outra sessão) deveria impedir o encerramento por ociosidade")
		}
		sh(t, id, fmt.Sprintf("touch -d @$(( $(date +%%s) - 300 )) %s", debugHeartbeatFile))
		sh(t, id, debugStopIfNoOtherSession)
		waitExit(t, id)
	})

	t.Run("backstop: heartbeat velho encerra sozinho", func(t *testing.T) {
		id := start(t)
		sh(t, id, fmt.Sprintf("touch -d @$(( $(date +%%s) - %d )) %s", debugHeartbeatMaxAgeSec+5, debugHeartbeatFile))
		waitExit(t, id)
	})
}

// fakeTermConn entrega mensagens JSON pré-definidas e guarda o que foi escrito.
type fakeTermConn struct {
	in  []string // JSON das mensagens recebidas do browser, em ordem
	out []map[string]interface{}
}

func (f *fakeTermConn) ReadJSON(v interface{}) error {
	if len(f.in) == 0 {
		return io.EOF
	}
	msg := f.in[0]
	f.in = f.in[1:]
	return json.Unmarshal([]byte(msg), v)
}
func (f *fakeTermConn) WriteJSON(v interface{}) error {
	f.out = append(f.out, v.(map[string]interface{}))
	return nil
}
func (f *fakeTermConn) Close() error { return nil }

func inputMsg(s string) string {
	return `{"type":"input","data":"` + base64.StdEncoding.EncodeToString([]byte(s)) + `"}`
}

func TestTerminalSessionReadBase64AndPending(t *testing.T) {
	big := strings.Repeat("x", 100) + "ação" // maior que o buffer de 16 bytes abaixo
	conn := &fakeTermConn{in: []string{
		inputMsg("ls -la\r"),
		`{"type":"resize","size":{"cols":120,"rows":40}}`,
		inputMsg(big),
	}}
	s := &TerminalSession{conn: conn, sizeCh: make(chan remotecommand.TerminalSize, 5)}

	buf := make([]byte, 16)
	n, err := s.Read(buf)
	if err != nil || string(buf[:n]) != "ls -la\r" {
		t.Fatalf("1ª leitura: %q %v", buf[:n], err)
	}
	if n, err = s.Read(buf); err != nil || n != 0 {
		t.Fatalf("resize não deveria entregar bytes: %d %v", n, err)
	}
	if sz := s.Next(); sz.Width != 120 || sz.Height != 40 {
		t.Errorf("resize = %+v", sz)
	}
	// colar grande: chega inteiro, em pedaços, sem perder o final (antes era truncado)
	var got []byte
	for len(got) < len(big) {
		n, err := s.Read(buf)
		if err != nil {
			t.Fatalf("leitura do colar: %v (recebido %d de %d)", err, len(got), len(big))
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != big {
		t.Errorf("colar chegou diferente: %q", got)
	}
	if _, err := s.Read(buf); err != io.EOF {
		t.Errorf("sem mais mensagens deveria dar EOF, veio %v", err)
	}
}

func TestTerminalSessionReadRejectsInvalidBase64(t *testing.T) {
	s := &TerminalSession{conn: &fakeTermConn{in: []string{`{"type":"input","data":"%%%"}`}}}
	if _, err := s.Read(make([]byte, 8)); err == nil {
		t.Error("entrada fora de base64 deveria dar erro")
	}
}

func TestTerminalSessionWriteBase64KeepsRawBytes(t *testing.T) {
	conn := &fakeTermConn{}
	s := &TerminalSession{conn: conn}
	// "ção" partido no meio de um caractere multibyte entre dois Write: antes cada pedaço virava "�"
	full := []byte("ação\x1b[1;32mok\x1b[0m")
	if _, err := s.Write(full[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(full[2:]); err != nil {
		t.Fatal(err)
	}
	var joined []byte
	for _, m := range conn.out {
		if m["type"] != "output" {
			t.Fatalf("tipo = %v", m["type"])
		}
		b, err := base64.StdEncoding.DecodeString(m["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		joined = append(joined, b...)
	}
	if string(joined) != string(full) {
		t.Errorf("bytes alterados: %q", joined)
	}
	s.writeNotice("aviso")
	if b, _ := base64.StdEncoding.DecodeString(conn.out[len(conn.out)-1]["data"].(string)); string(b) != "aviso" {
		t.Errorf("writeNotice também deveria sair em base64: %q", b)
	}
}

func TestTerminalSessionResizeBothShapes(t *testing.T) {
	conn := &fakeTermConn{in: []string{
		`{"type":"resize","size":{"cols":120,"rows":40}}`,
		`{"type":"resize","cols":100,"rows":30}`, // formato achatado (antes ignorado: PTY sem tamanho)
		`{"type":"resize","cols":0,"rows":0}`,    // tamanho vazio (container escondido) não vai para o PTY
	}}
	s := &TerminalSession{conn: conn, sizeCh: make(chan remotecommand.TerminalSize, 5)}
	buf := make([]byte, 8)
	for i := 0; i < 3; i++ {
		if n, err := s.Read(buf); err != nil || n != 0 {
			t.Fatalf("resize %d: %d %v", i, n, err)
		}
	}
	if len(s.sizeCh) != 2 {
		t.Fatalf("esperava 2 tamanhos na fila, veio %d", len(s.sizeCh))
	}
	if a, b := s.Next(), s.Next(); a.Width != 120 || a.Height != 40 || b.Width != 100 || b.Height != 30 {
		t.Errorf("tamanhos: %+v %+v", a, b)
	}
}

func TestDebugSessionCount(t *testing.T) {
	key := "https://api/ns/pod/k8s-hpa-test-debug"
	acquireDebugSession(key)
	acquireDebugSession(key) // segunda aba no mesmo container
	if n := releaseDebugSession(key); n != 1 {
		t.Fatalf("com outra sessão aberta, o --rm não pode encerrar: restam %d", n)
	}
	if n := releaseDebugSession(key); n != 0 {
		t.Fatalf("última sessão: restam %d", n)
	}
	if n := releaseDebugSession(key); n != 0 { // release a mais não fica negativo
		t.Fatalf("release extra: %d", n)
	}
}
