package servicenow

import (
	"os"
	"runtime"
	"strings"
)

// BrowserConfig/LoadBrowserConfig/SaveBrowserConfig foram movidos pra internal/browser/
// sso_config.go — agora compartilhados com o Teams (modo Docker/embed), ver
// BROWSER-CONSOLIDATION-STUDY.md. Usar browser.LoadBrowserConfig()/browser.SaveBrowserConfig().

// IsWSL retorna true se o processo está rodando dentro do WSL (Windows Subsystem for Linux)
func IsWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

// HasGraphicalDisplay retorna true se há um display gráfico disponível.
// macOS (darwin) sempre tem display nativo (Quartz/Aqua — sem DISPLAY/WAYLAND).
// Linux/WSL: verifica variáveis X11/Wayland.
func HasGraphicalDisplay() bool {
	if runtime.GOOS == "darwin" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// getWSL2WindowsHostIP retorna o IP do host Windows acessível a partir do WSL2.
// Em WSL2, o host Windows é acessível via o nameserver configurado em /etc/resolv.conf.
// Retorna "" se não estiver em WSL2 ou se o IP não puder ser determinado.
func getWSL2WindowsHostIP() string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			ip := strings.TrimSpace(strings.TrimPrefix(line, "nameserver "))
			if ip != "" && ip != "127.0.0.1" && ip != "::1" {
				return ip
			}
		}
	}
	return ""
}

// cdpHosts retorna a lista de hosts a tentar para conectar ao CDP (usado só pelo fast-path
// passivo de leitura de cookies em cdp_cookies.go — ver WindowsCDPPort abaixo; a aplicação nunca
// lança um Chrome nesse endpoint, só tenta ler de um que o usuário já tenha aberto manualmente).
// Em WSL2: tenta 127.0.0.1 (localhost forwarding) e o IP do host Windows.
// Fora do WSL2: apenas 127.0.0.1.
func cdpHosts() []string {
	hosts := []string{"127.0.0.1"}
	if winIP := getWSL2WindowsHostIP(); winIP != "" {
		hosts = append(hosts, winIP)
	}
	return hosts
}

// WindowsCDPPort é a porta CDP usada pelo fast-path opcional de leitura de cookies
// (ExtractCookiesViaCDP em cdp_cookies.go) — só funciona se o usuário já tiver, por conta
// própria, um Chrome do Windows aberto com --remote-debugging-port=9223. A aplicação nunca lança
// esse Chrome (ver BROWSER-CONSOLIDATION-STUDY.md — o antigo modo "Windows CDP" que lançava o
// browser via PowerShell foi removido por não ser mais chamado por nada).
const WindowsCDPPort = 9223
