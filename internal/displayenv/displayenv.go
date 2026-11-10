// Package displayenv responde "esta máquina consegue mostrar uma janela?" e, quando não consegue
// (WSL sem o modo gráfico, o WSLg), "dá para usar o navegador padrão do Windows?".
//
// Duas coisas diferentes dependem de navegador na ferramenta, e o Windows resolve só uma delas:
//   - ABRIR UM LINK (a própria interface, o login do `az`, OAuth): qualquer navegador serve, inclusive
//     o padrão do Windows, chamado de dentro do WSL (interop). É o que OpenURL e AzLoginEnv fazem.
//   - CONTROLAR UM NAVEGADOR (o robô do Teams e o login do ServiceNow, via CDP): precisa de um Chrome
//     que a ferramenta lance e comande, com uma janela visível. O navegador do Windows, aberto por
//     link, não é controlável. Isso exige o WSLg (ou outro display) — Report.Capabilities diz quando.
//
// Toda sonda ao sistema (arquivos, variáveis, sockets, comandos) entra por Probe, para os cenários
// (WSL2 com e sem WSLg, WSL1, Linux nativo, macOS, Windows) serem testados sem depender da máquina.
package displayenv

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Modos de operação, do melhor para o pior.
const (
	ModeGraphical      = "graphical"       // há display alcançável: dá para abrir e controlar janelas
	ModeWindowsBrowser = "windows-browser" // sem display, mas o navegador padrão do Windows abre links
	ModeHeadlessOnly   = "headless-only"   // nem display nem navegador do Windows: só automação sem janela
)

// Probe é tudo o que a detecção pergunta ao sistema.
type Probe struct {
	GOOS     string
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	Exists   func(string) bool
	LookPath func(string) (string, error)
	// Dial tenta conectar (unix ou tcp) e devolve nil se alguém atende.
	Dial func(network, addr string, timeout time.Duration) error
	// Run executa um comando e devolve a saída (com limite de tempo pelo ctx).
	Run func(ctx context.Context, name string, args ...string) (string, error)
}

// DefaultProbe é a sonda real, sobre o sistema em que o processo roda.
func DefaultProbe() Probe {
	return Probe{
		GOOS:     runtime.GOOS,
		Getenv:   os.Getenv,
		ReadFile: os.ReadFile,
		Exists:   func(p string) bool { _, err := os.Stat(p); return err == nil },
		LookPath: exec.LookPath,
		Dial: func(network, addr string, timeout time.Duration) error {
			c, err := net.DialTimeout(network, addr, timeout)
			if err != nil {
				return err
			}
			return c.Close()
		},
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			out, err := exec.CommandContext(ctx, name, args...).Output()
			return string(out), err
		},
	}
}

// WSLg descreve o modo gráfico do WSL (servidor X11/Wayland que o Windows 11 e o WSL 2 recente oferecem).
type WSLg struct {
	Present        bool   `json:"present"`        // /mnt/wslg existe (o WSLg está instalado e ativo)
	X11Socket      bool   `json:"x11_socket"`     // há um socket X11 em /tmp/.X11-unix
	X11Reachable   bool   `json:"x11_reachable"`  // alguém atende nesse socket (o display de fato responde)
	WaylandSocket  bool   `json:"wayland_socket"` // há o socket do Wayland
	WaylandReach   bool   `json:"wayland_reachable"`
	DisplayEnv     string `json:"display_env"`     // valor de DISPLAY neste processo
	WaylandEnv     string `json:"wayland_env"`     // valor de WAYLAND_DISPLAY neste processo
	SuggestDisplay string `json:"suggest_display"` // DISPLAY a usar quando o ambiente não o define mas o socket responde
}

// Capability é uma ação da ferramenta que depende de navegador e se ela funciona neste ambiente.
type Capability struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Via    string `json:"via,omitempty"`    // como funciona (ex.: "navegador padrão do Windows")
	Detail string `json:"detail,omitempty"` // o que fazer quando não funciona
}

// Report é o resultado do teste.
type Report struct {
	OS         string `json:"os"`
	IsWSL      bool   `json:"is_wsl"`
	WSLVersion int    `json:"wsl_version,omitempty"` // 1 ou 2; 0 = desconhecido
	Distro     string `json:"distro,omitempty"`
	Interop    bool   `json:"interop"` // dá para executar programas do Windows (.exe) de dentro do WSL

	WSLg             WSLg `json:"wslg"`
	GraphicalDisplay bool `json:"graphical_display"` // há um display que atende AGORA (X11 ou Wayland)

	ChromePath string `json:"chrome_path,omitempty"` // Chrome/Chromium instalado no Linux (o robô usa este)
	Xvfb       bool   `json:"xvfb"`                  // display virtual (invisível) disponível
	Docker     bool   `json:"docker"`
	XdgOpen    bool   `json:"xdg_open"`

	WindowsOpeners        []string `json:"windows_openers,omitempty"` // wslview, rundll32.exe, explorer.exe
	WindowsDefaultBrowser string   `json:"windows_default_browser,omitempty"`

	Mode         string       `json:"mode"`
	Summary      string       `json:"summary"`
	Capabilities []Capability `json:"capabilities"`
	Notes        []string     `json:"notes,omitempty"`
}

// Detect faz o teste completo. Nunca falha: o que não der para saber fica como "não" com uma nota.
func Detect(ctx context.Context, p Probe) Report {
	r := Report{OS: p.GOOS, Capabilities: []Capability{}}

	if p.GOOS == "linux" {
		r.IsWSL, r.WSLVersion = detectWSL(p)
		r.Distro = p.Getenv("WSL_DISTRO_NAME")
	}
	if r.IsWSL {
		r.Interop = detectInterop(p)
	}

	r.WSLg = detectWSLg(p, r.IsWSL)
	switch p.GOOS {
	case "darwin", "windows":
		r.GraphicalDisplay = true // Quartz / janela nativa: não há DISPLAY para testar
	default:
		r.GraphicalDisplay = r.WSLg.X11Reachable || r.WSLg.WaylandReach
	}

	r.ChromePath = firstInPath(p, "google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "microsoft-edge")
	r.Xvfb = has(p, "Xvfb")
	r.Docker = has(p, "docker")
	r.XdgOpen = has(p, "xdg-open")

	if r.IsWSL && r.Interop {
		r.WindowsOpeners = windowsOpeners(p)
		r.WindowsDefaultBrowser = windowsDefaultBrowser(ctx, p, r.WindowsOpeners)
	}

	r.Mode = decideMode(r)
	r.Capabilities = capabilities(r)
	r.Notes = notes(r)
	r.Summary = summary(r)
	return r
}

// ── detecção ────────────────────────────────────────────────────────────────

func detectWSL(p Probe) (isWSL bool, version int) {
	if p.Getenv("WSL_DISTRO_NAME") != "" || p.Getenv("WSL_INTEROP") != "" {
		isWSL = true
	}
	data, err := p.ReadFile("/proc/version")
	if err == nil {
		low := strings.ToLower(string(data))
		if strings.Contains(low, "microsoft") || strings.Contains(low, "wsl") {
			isWSL = true
			// WSL2 roda um kernel próprio ("microsoft-standard-WSL2"); o WSL1 traduz chamadas ("...-Microsoft").
			if strings.Contains(low, "wsl2") || strings.Contains(low, "microsoft-standard") {
				version = 2
			} else {
				version = 1
			}
		}
	}
	return isWSL, version
}

func detectInterop(p Probe) bool {
	if p.Getenv("WSL_INTEROP") != "" {
		return true
	}
	for _, f := range []string{"/proc/sys/fs/binfmt_misc/WSLInterop", "/proc/sys/fs/binfmt_misc/WSLInterop-late"} {
		if p.Exists(f) {
			return true
		}
	}
	return false
}

var displayRe = regexp.MustCompile(`^([^:]*):(\d+)(?:\.\d+)?$`)

func detectWSLg(p Probe, isWSL bool) WSLg {
	g := WSLg{DisplayEnv: p.Getenv("DISPLAY"), WaylandEnv: p.Getenv("WAYLAND_DISPLAY")}
	g.Present = p.Exists("/mnt/wslg")

	// X11: o display do ambiente (":0", ":0.0", "host:0") ou, na falta dele, o socket do WSLg (X0).
	num := ""
	if m := displayRe.FindStringSubmatch(g.DisplayEnv); m != nil {
		num = m[2]
		if host := m[1]; host != "" && host != "unix" {
			// X11 por TCP (porta 6000+n): só tenta conectar
			if n, err := strconv.Atoi(num); err == nil && p.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(6000+n)), 700*time.Millisecond) == nil {
				g.X11Socket, g.X11Reachable = true, true
			}
			num = ""
		}
	}
	if num != "" || g.DisplayEnv == "" {
		cands := []string{}
		if num != "" {
			cands = append(cands, num)
		}
		cands = append(cands, "0") // WSLg usa :0
		for _, n := range cands {
			sock := "/tmp/.X11-unix/X" + n
			if !p.Exists(sock) {
				continue
			}
			g.X11Socket = true
			if p.Dial("unix", sock, 700*time.Millisecond) == nil {
				g.X11Reachable = true
				if g.DisplayEnv == "" {
					g.SuggestDisplay = ":" + n
				}
				break
			}
		}
	}

	// Wayland: WAYLAND_DISPLAY relativo a XDG_RUNTIME_DIR; o WSLg expõe o socket em /mnt/wslg/runtime-dir.
	name := g.WaylandEnv
	if name == "" {
		name = "wayland-0"
	}
	for _, dir := range uniq([]string{p.Getenv("XDG_RUNTIME_DIR"), "/mnt/wslg/runtime-dir"}) {
		if dir == "" {
			continue
		}
		sock := name
		if !filepath.IsAbs(sock) {
			sock = filepath.Join(dir, name)
		}
		if !p.Exists(sock) {
			continue
		}
		g.WaylandSocket = true
		if p.Dial("unix", sock, 700*time.Millisecond) == nil {
			g.WaylandReach = true
			break
		}
	}
	return g
}

func windowsOpeners(p Probe) []string {
	var out []string
	for _, name := range []string{"wslview", "rundll32.exe", "explorer.exe"} {
		if _, err := p.LookPath(name); err == nil {
			out = append(out, name)
			continue
		}
		// o PATH do WSL pode não incluir o Windows (appendWindowsPath=false): tenta o caminho conhecido
		if strings.HasSuffix(name, ".exe") {
			for _, base := range windowsSystemDirs {
				if p.Exists(base + "/" + name) {
					out = append(out, name)
					break
				}
			}
		}
	}
	return out
}

// Pastas do Windows montadas no WSL onde ficam rundll32.exe e explorer.exe.
var windowsSystemDirs = []string{"/mnt/c/Windows/System32", "/mnt/c/WINDOWS/system32", "/mnt/c/Windows"}

var progIDRe = regexp.MustCompile(`ProgId\s+REG_SZ\s+(\S+)`)

// windowsDefaultBrowser descobre (melhor esforço, só leitura) o navegador padrão do Windows pelo registro.
func windowsDefaultBrowser(ctx context.Context, p Probe, openers []string) string {
	if len(openers) == 0 {
		return ""
	}
	regExe := "reg.exe"
	if path, err := p.LookPath(regExe); err == nil {
		regExe = path
	} else {
		found := false
		for _, base := range windowsSystemDirs {
			if p.Exists(base + "/reg.exe") {
				regExe, found = base+"/reg.exe", true
				break
			}
		}
		if !found {
			return ""
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := p.Run(cctx, regExe, "query", `HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`, "/v", "ProgId")
	if err != nil {
		return ""
	}
	m := progIDRe.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return friendlyBrowser(m[1])
}

func friendlyBrowser(progID string) string {
	switch {
	case strings.HasPrefix(progID, "ChromeHTML"):
		return "Google Chrome"
	case strings.HasPrefix(progID, "MSEdgeHTM"):
		return "Microsoft Edge"
	case strings.HasPrefix(progID, "FirefoxURL"):
		return "Mozilla Firefox"
	case strings.HasPrefix(progID, "BraveHTML"):
		return "Brave"
	case strings.HasPrefix(progID, "OperaStable"), strings.HasPrefix(progID, "Opera"):
		return "Opera"
	case strings.HasPrefix(progID, "VivaldiHTM"):
		return "Vivaldi"
	}
	return progID
}

func decideMode(r Report) string {
	if r.GraphicalDisplay {
		return ModeGraphical
	}
	if r.IsWSL && r.Interop && len(r.WindowsOpeners) > 0 {
		return ModeWindowsBrowser
	}
	return ModeHeadlessOnly
}

func capabilities(r Report) []Capability {
	// Abrir um link — o mesmo caminho que OpenURL escolhe em "auto": com display, o navegador do Linux;
	// no WSL sem display, o navegador padrão do Windows.
	open := Capability{ID: "open_url", Label: "Abrir links (a interface, OAuth, login)"}
	windowsOK := r.IsWSL && r.Interop && len(r.WindowsOpeners) > 0
	windowsVia := "navegador padrão do Windows"
	if r.WindowsDefaultBrowser != "" {
		windowsVia += " (" + r.WindowsDefaultBrowser + ")"
	}
	switch {
	case r.OS == "darwin" || r.OS == "windows":
		open.OK, open.Via = true, "navegador padrão do sistema"
	case r.XdgOpen && r.GraphicalDisplay:
		open.OK, open.Via = true, "xdg-open (navegador do Linux)"
	case windowsOK:
		open.OK, open.Via = true, windowsVia
	default:
		open.Detail = "nenhum navegador acessível: abra o endereço manualmente no navegador do Windows"
	}

	azl := Capability{ID: "az_login", Label: "Login do Azure CLI pela interface", OK: open.OK, Via: open.Via}
	if !open.OK {
		azl.Detail = "rode `az login --use-device-code` no terminal"
	}

	teams := Capability{ID: "teams_automation", Label: "Robô do Teams (Chrome controlado pela ferramenta)"}
	switch {
	case r.GraphicalDisplay && r.ChromePath != "":
		teams.OK, teams.Via = true, "Chrome do Linux no display gráfico"
	case !r.GraphicalDisplay:
		teams.Detail = "precisa de uma janela visível (login e MFA): ative o WSLg (`wsl --update` no Windows 11/10 atualizado) — o navegador do Windows só abre links, a ferramenta não consegue controlá-lo"
	default:
		teams.Detail = "instale o Google Chrome ou o Chromium no Linux (ex.: google-chrome-stable)"
	}

	snow := Capability{ID: "servicenow_login", Label: "Login do ServiceNow (janela visível)"}
	if r.GraphicalDisplay {
		snow.OK, snow.Via = true, "Chromium no display gráfico"
	} else {
		snow.Detail = "o login precisa de uma janela visível; sem display só o Xvfb (invisível) existe. A extração de CHG com sessão já válida roda sem janela"
	}
	return []Capability{open, azl, teams, snow}
}

func notes(r Report) []string {
	var n []string
	if r.IsWSL && r.WSLVersion == 1 {
		n = append(n, "WSL 1 não tem WSLg: o modo gráfico exige o WSL 2.")
	}
	if r.IsWSL && !r.Interop {
		n = append(n, "A interoperabilidade com o Windows está desligada (/etc/wsl.conf: [interop] enabled=false): o navegador do Windows não pode ser chamado.")
	}
	if r.IsWSL && r.WSLg.SuggestDisplay != "" && r.WSLg.DisplayEnv == "" {
		n = append(n, "O WSLg responde em "+r.WSLg.SuggestDisplay+", mas DISPLAY não está definido neste processo (sessão sem o perfil de login): defina DISPLAY="+r.WSLg.SuggestDisplay+" ou deixe a ferramenta definir sozinha.")
	}
	if r.WSLg.DisplayEnv != "" && !r.WSLg.X11Reachable && !r.WSLg.WaylandReach {
		n = append(n, "DISPLAY="+r.WSLg.DisplayEnv+" está definido, mas ninguém atende nele (servidor X parado ou WSLg inativo). Reinicie o WSL (`wsl --shutdown` no Windows).")
	}
	if r.IsWSL && !r.WSLg.Present && !r.GraphicalDisplay {
		n = append(n, "/mnt/wslg não existe: o WSLg não está ativo nesta distro (Windows sem suporte ou desabilitado em .wslconfig: guiApplications=false).")
	}
	if r.IsWSL && r.Mode == ModeWindowsBrowser {
		n = append(n, "Sem display, o robô do Teams e o login do ServiceNow não funcionam: o navegador do Windows abre links, mas não pode ser controlado pela ferramenta.")
	}
	return n
}

func summary(r Report) string {
	switch r.Mode {
	case ModeGraphical:
		if r.IsWSL {
			return "Modo gráfico disponível (WSLg): a ferramenta abre e controla janelas."
		}
		return "Display gráfico disponível."
	case ModeWindowsBrowser:
		s := "Sem modo gráfico no WSL: links abrem no navegador padrão do Windows"
		if r.WindowsDefaultBrowser != "" {
			s += " (" + r.WindowsDefaultBrowser + ")"
		}
		return s + ". O robô do Teams e o login do ServiceNow precisam do WSLg."
	}
	return "Sem display gráfico e sem acesso ao navegador do Windows: só a automação sem janela funciona."
}

// ── utilitários ─────────────────────────────────────────────────────────────

func has(p Probe, name string) bool {
	_, err := p.LookPath(name)
	return err == nil
}

func firstInPath(p Probe, names ...string) string {
	for _, n := range names {
		if path, err := p.LookPath(n); err == nil {
			return path
		}
	}
	return ""
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
