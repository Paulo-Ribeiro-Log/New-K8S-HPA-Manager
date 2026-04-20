package servicenow

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// IsXvfbInstalled verifica se o Xvfb está instalado no sistema.
func IsXvfbInstalled() bool {
	_, err := exec.LookPath("Xvfb")
	return err == nil
}

// XvfbInstallHint retorna o comando de instalação do Xvfb para a distro atual.
func XvfbInstallHint() string {
	// Detectar distro via /etc/os-release
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		content := string(data)
		if contains(content, "ubuntu") || contains(content, "debian") {
			return "sudo apt-get install -y xvfb"
		}
		if contains(content, "fedora") || contains(content, "rhel") || contains(content, "centos") {
			return "sudo dnf install -y xorg-x11-server-Xvfb"
		}
	}
	return "sudo apt-get install -y xvfb"
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// StartXvfb inicia um display virtual Xvfb no número informado (ex: ":99").
// Retorna o processo iniciado e um cleanup que o encerra.
func StartXvfb(displayNum string) (*exec.Cmd, error) {
	cmd := exec.Command("Xvfb", displayNum, "-screen", "0", "1280x900x24", "-ac", "+extension", "GLX")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Xvfb não pôde iniciar no display %s: %v", displayNum, err)
	}
	// Aguardar Xvfb estar pronto
	time.Sleep(1200 * time.Millisecond)
	return cmd, nil
}

// EnsureVirtualDisplay retorna um DISPLAY utilizável:
//   - Se já há um display configurado (WSLg, X11 forwarding), retorna ele.
//   - Senão inicia Xvfb no :99 e retorna ":99".
//
// Retorna o display string e um cleanup (nil se usou display existente).
func EnsureVirtualDisplay() (display string, cleanup func(), err error) {
	// Usar display existente se disponível (WSLg, X11 forwarding, etc.)
	if d := os.Getenv("DISPLAY"); d != "" {
		return d, nil, nil
	}
	if d := os.Getenv("WAYLAND_DISPLAY"); d != "" {
		return d, nil, nil
	}

	if !IsXvfbInstalled() {
		return "", nil, fmt.Errorf(
			"Xvfb não está instalado. Instale com:\n  %s\n\n"+
				"O Xvfb permite rodar o browser em modo virtual (sem display físico), "+
				"necessário para autenticação no ServiceNow em ambientes sem interface gráfica.",
			XvfbInstallHint(),
		)
	}

	xvfbDisplay := ":99"
	cmd, startErr := StartXvfb(xvfbDisplay)
	if startErr != nil {
		return "", nil, startErr
	}

	cleanup = func() {
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
	}
	return xvfbDisplay, cleanup, nil
}
