package handlers

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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
