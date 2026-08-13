package servicenow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BrowserConfig armazena a preferência de browser para autenticação ServiceNow.
// Salva em ~/.k8s-hpa-manager/servicenow-browser.json (configuração da máquina, não por usuário).
type BrowserConfig struct {
	SSOLoginIdentifier string `json:"sso_login_identifier,omitempty"` // "email" ou "matricula" — qual campo do Perfil SSO usar no login Azure AD (padrão: "email")
}

func browserConfigPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".k8s-hpa-manager", "servicenow-browser.json")
}

// LoadBrowserConfig lê a configuração de browser. Retorna config vazia se não existe.
func LoadBrowserConfig() BrowserConfig {
	data, err := os.ReadFile(browserConfigPath())
	if err != nil {
		return BrowserConfig{}
	}
	var cfg BrowserConfig
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// SaveBrowserConfig persiste a configuração de browser.
func SaveBrowserConfig(cfg BrowserConfig) error {
	path := browserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

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
