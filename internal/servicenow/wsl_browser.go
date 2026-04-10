package servicenow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BrowserConfig armazena a preferência de browser para autenticação ServiceNow.
// Salva em ~/.k8s-hpa-manager/servicenow-browser.json (configuração da máquina, não por usuário).
type BrowserConfig struct {
	ForceWindowsBrowser bool `json:"force_windows_browser"`
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

// HasGraphicalDisplay retorna true se há um servidor gráfico (X11/Wayland) disponível no ambiente atual
func HasGraphicalDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// NeedsWindowsBrowser retorna true quando o browser Windows deve ser usado para autenticação.
//
// Ordem de precedência:
//  1. Env var K8S_HPA_WINDOWS_BROWSER=true → força modo Windows (útil para testes/CI)
//  2. Configuração persistida em ~/.k8s-hpa-manager/servicenow-browser.json
//  3. Detecção automática: WSL sem display gráfico
func NeedsWindowsBrowser() bool {
	// 1. Env var tem prioridade máxima
	if os.Getenv("K8S_HPA_WINDOWS_BROWSER") == "true" {
		return true
	}
	// 2. Configuração persistida pelo usuário
	cfg := LoadBrowserConfig()
	if cfg.ForceWindowsBrowser {
		return true
	}
	// 3. Detecção automática: WSL sem display
	return IsWSL() && !HasGraphicalDisplay()
}

// windowsBrowserCandidates lista paths conhecidos de Chrome/Edge no Windows acessíveis via WSL
var windowsBrowserCandidates = []string{
	"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe",
	"/mnt/c/Program Files (x86)/Google/Chrome/Application/chrome.exe",
	"/mnt/c/Program Files/Microsoft/Edge/Application/msedge.exe",
	"/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
}

// FindWindowsBrowser localiza o primeiro browser Windows disponível nos paths conhecidos
func FindWindowsBrowser() (string, error) {
	for _, path := range windowsBrowserCandidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf(
		"nenhum browser Windows encontrado (Chrome/Edge). Instale o Google Chrome ou Microsoft Edge no Windows",
	)
}

// wslToWindowsPath converte um path WSL (/mnt/c/foo/bar) para formato Windows (C:\foo\bar)
func wslToWindowsPath(wslPath string) string {
	if !strings.HasPrefix(wslPath, "/mnt/") {
		return wslPath
	}
	parts := strings.SplitN(wslPath[5:], "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return wslPath
	}
	drive := strings.ToUpper(parts[0])
	rest := strings.ReplaceAll(parts[1], "/", `\`)
	return drive + `:\` + rest
}

// LaunchWindowsBrowserForCDP lança o browser Windows com remote debugging na porta especificada.
// browserWSLPath: path do browser no formato WSL (/mnt/c/...).
// userDataWSLDir: diretório de dados do usuário no formato WSL — será convertido para Windows.
// initialURL: se não vazio, Chrome abre diretamente nessa URL (sem aba em branco inicial).
//
// Usa PowerShell Start-Process em vez de cmd.exe /c start para garantir que os flags chegam ao
// Chrome sem problemas de quoting — cmd.exe via exec.Command do Go pode descartar os argumentos.
func LaunchWindowsBrowserForCDP(browserWSLPath string, port int, userDataWSLDir string, initialURL string) (*exec.Cmd, error) {
	winBrowserPath := wslToWindowsPath(browserWSLPath)
	winUserDataDir := wslToWindowsPath(userDataWSLDir)

	chromeArgs := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		// Necessário para o WSL2 conseguir conectar: por padrão Chrome só escuta em
		// 127.0.0.1 (loopback Windows), que não é alcançável a partir do WSL2.
		// Com 0.0.0.0, escuta em todas as interfaces incluindo a 172.22.x.x do WSL2.
		"--remote-debugging-address=0.0.0.0",
		"--remote-allow-origins=*",
		fmt.Sprintf(`--user-data-dir=%s`, winUserDataDir),
		"--profile-directory=HPA-Manager",
		"--no-first-run",
		"--disable-default-apps",
		"--no-default-browser-check",
	}
	if initialURL != "" {
		chromeArgs = append(chromeArgs, initialURL)
	}

	// Monta -ArgumentList para PowerShell: cada arg entre aspas duplas, aspas internas escapadas
	quotedArgs := make([]string, len(chromeArgs))
	for i, a := range chromeArgs {
		// Escapa aspas duplas existentes e envolve em aspas duplas
		quotedArgs[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	argList := strings.Join(quotedArgs, ", ")

	// Escapa o path do browser (aspas duplas internas → `"`)
	escapedBrowserPath := strings.ReplaceAll(winBrowserPath, `"`, `\"`)

	// Start-Process lança o processo desanexado e passa os args sem reinterpretar quoting do shell
	psCmd := fmt.Sprintf(`Start-Process -FilePath "%s" -ArgumentList %s`, escapedBrowserPath, argList)

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("erro ao lançar browser Windows via PowerShell: %v", err)
	}
	return cmd, nil
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

// cdpHosts retorna a lista de hosts a tentar para conectar ao CDP.
// Em WSL2: tenta 127.0.0.1 (localhost forwarding) e o IP do host Windows.
// Fora do WSL2: apenas 127.0.0.1.
func cdpHosts() []string {
	hosts := []string{"127.0.0.1"}
	if winIP := getWSL2WindowsHostIP(); winIP != "" {
		hosts = append(hosts, winIP)
	}
	return hosts
}

// WaitCDPReady aguarda o endpoint CDP ficar disponível na porta especificada.
// Em WSL2, tenta tanto 127.0.0.1 quanto o IP do host Windows (fallback para quando
// o localhost forwarding não está ativo). Retorna o host que respondeu.
func WaitCDPReady(port int, timeout time.Duration) error {
	_, err := WaitCDPReadyHost(port, timeout)
	return err
}

// WaitCDPReadyHost igual ao WaitCDPReady mas retorna o host que respondeu.
// Útil para usar o mesmo host na URL de conexão WebSocket do Rod.
func WaitCDPReadyHost(port int, timeout time.Duration) (string, error) {
	hosts := cdpHosts()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		for _, host := range hosts {
			url := fmt.Sprintf("http://%s:%d/json/version", host, port)
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return host, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("CDP não ficou disponível na porta %d após %v (hosts tentados: %v)", port, timeout, hosts)
}

// WindowsCDPPort é a porta padrão usada para conectar ao browser Windows via CDP.
// Usa 9223 (e não 9222) para evitar conflito com instâncias de Chrome já abertas com debug.
const WindowsCDPPort = 9223

// WindowsSessionWSLDir retorna o diretório de sessão para o browser Windows, no formato WSL.
// Usa o perfil do usuário Windows (C:\Users\{username}\k8s-hpa-manager-session) para evitar
// bloqueios de política de segurança que impedem escrita na raiz de C:\.
// Fallback para /mnt/c/k8s-hpa-manager-session se não conseguir obter o perfil Windows.
func WindowsSessionWSLDir() string {
	// Tenta obter o USERPROFILE Windows via cmd.exe
	// Em WSL, cmd.exe está disponível e %USERPROFILE% aponta para C:\Users\{username}
	out, err := exec.Command("cmd.exe", "/c", "echo", "%USERPROFILE%").Output()
	if err == nil {
		winProfile := strings.TrimSpace(string(out))
		// Resultado esperado: "C:\Users\username"
		if strings.HasPrefix(winProfile, `C:\`) || strings.HasPrefix(winProfile, `c:\`) {
			// Converte C:\Users\username → /mnt/c/Users/username
			wslPath := windowsToWSLPath(winProfile)
			if wslPath != "" {
				return filepath.Join(wslPath, "k8s-hpa-manager-session")
			}
		}
	}
	return "/mnt/c/k8s-hpa-manager-session"
}

// windowsToWSLPath converte um path Windows (C:\foo\bar) para formato WSL (/mnt/c/foo/bar)
func windowsToWSLPath(winPath string) string {
	if len(winPath) < 3 || winPath[1] != ':' {
		return ""
	}
	drive := strings.ToLower(string(winPath[0]))
	rest := strings.ReplaceAll(winPath[3:], `\`, "/")
	return "/mnt/" + drive + "/" + rest
}
