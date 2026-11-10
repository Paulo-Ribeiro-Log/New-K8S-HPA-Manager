package displayenv

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fake monta uma Probe a partir de um "sistema" descrito em memória.
type fake struct {
	goos    string
	env     map[string]string
	files   map[string]string // conteúdo legível
	exists  map[string]bool   // existe (diretório, socket)
	paths   map[string]string // programas no PATH
	dialOK  map[string]bool   // "network:addr" que atendem
	runOut  map[string]string // saída por "nome arg1 arg2..."
	started [][]string
}

func (f *fake) probe() Probe {
	return Probe{
		GOOS:   f.goos,
		Getenv: func(k string) string { return f.env[k] },
		ReadFile: func(p string) ([]byte, error) {
			if c, ok := f.files[p]; ok {
				return []byte(c), nil
			}
			return nil, fmt.Errorf("não existe")
		},
		Exists: func(p string) bool { _, f1 := f.files[p]; return f.exists[p] || f1 },
		LookPath: func(n string) (string, error) {
			if p, ok := f.paths[n]; ok {
				return p, nil
			}
			return "", fmt.Errorf("não achado")
		},
		Dial: func(network, addr string, _ time.Duration) error {
			if f.dialOK[network+":"+addr] {
				return nil
			}
			return fmt.Errorf("recusado")
		},
		Run: func(_ context.Context, name string, args ...string) (string, error) {
			if out, ok := f.runOut[name+" "+strings.Join(args, " ")]; ok {
				return out, nil
			}
			return "", fmt.Errorf("falhou")
		},
	}
}

const wsl2Kernel = "Linux version 6.18.33.2-microsoft-standard-WSL2 (root@x) (gcc) #1 SMP"

const regChrome = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\Shell\\Associations\\UrlAssociations\\https\\UserChoice\r\n    ProgId    REG_SZ    ChromeHTML\r\n\r\n"

// wsl2ComWSLg é o ambiente desta máquina de desenvolvimento: WSL2, WSLg ativo, interop, Chrome do Linux.
func wsl2ComWSLg() *fake {
	return &fake{
		goos:   "linux",
		env:    map[string]string{"WSL_DISTRO_NAME": "Ubuntu-22.04", "DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0", "XDG_RUNTIME_DIR": "/run/user/1000"},
		files:  map[string]string{"/proc/version": wsl2Kernel},
		exists: map[string]bool{"/mnt/wslg": true, "/tmp/.X11-unix/X0": true, "/proc/sys/fs/binfmt_misc/WSLInterop": true, "/run/user/1000/wayland-0": true},
		paths: map[string]string{"google-chrome-stable": "/usr/bin/google-chrome-stable", "xdg-open": "/usr/bin/xdg-open", "Xvfb": "/usr/bin/Xvfb", "docker": "/usr/bin/docker",
			"rundll32.exe": "/mnt/c/WINDOWS/system32/rundll32.exe", "explorer.exe": "/mnt/c/WINDOWS/explorer.exe", "reg.exe": "/mnt/c/WINDOWS/system32/reg.exe"},
		dialOK: map[string]bool{"unix:/tmp/.X11-unix/X0": true, "unix:/run/user/1000/wayland-0": true},
		runOut: map[string]string{`/mnt/c/WINDOWS/system32/reg.exe query HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice /v ProgId`: regChrome},
	}
}

// wsl2SemWSLg é o WSL2 sem modo gráfico: sem /mnt/wslg, sem X, mas com o Windows chamável.
func wsl2SemWSLg() *fake {
	f := wsl2ComWSLg()
	f.env = map[string]string{"WSL_DISTRO_NAME": "Ubuntu-22.04"}
	f.exists = map[string]bool{"/proc/sys/fs/binfmt_misc/WSLInterop": true}
	f.dialOK = map[string]bool{}
	delete(f.paths, "google-chrome-stable")
	return f
}

func cap(r Report, id string) Capability {
	for _, c := range r.Capabilities {
		if c.ID == id {
			return c
		}
	}
	return Capability{ID: "AUSENTE"}
}

func TestWSL2ComWSLg(t *testing.T) {
	r := Detect(context.Background(), wsl2ComWSLg().probe())
	if !r.IsWSL || r.WSLVersion != 2 || r.Distro != "Ubuntu-22.04" || !r.Interop {
		t.Errorf("WSL: %+v", r)
	}
	if !r.WSLg.Present || !r.WSLg.X11Reachable || !r.WSLg.WaylandReach || !r.GraphicalDisplay || r.Mode != ModeGraphical {
		t.Errorf("WSLg: %+v mode=%s", r.WSLg, r.Mode)
	}
	if r.ChromePath != "/usr/bin/google-chrome-stable" || !r.Xvfb || !r.Docker || !r.XdgOpen {
		t.Errorf("programas: %+v", r)
	}
	if r.WindowsDefaultBrowser != "Google Chrome" || len(r.WindowsOpeners) != 2 {
		t.Errorf("navegador do Windows: %q %v", r.WindowsDefaultBrowser, r.WindowsOpeners)
	}
	for _, id := range []string{"open_url", "az_login", "teams_automation", "servicenow_login"} {
		if !cap(r, id).OK {
			t.Errorf("%s deveria funcionar com WSLg: %+v", id, cap(r, id))
		}
	}
	if !strings.Contains(cap(r, "open_url").Via, "xdg-open") {
		t.Errorf("com display, abrir link usa o Linux (igual ao OpenURL em auto): %+v", cap(r, "open_url"))
	}
	if len(r.Notes) != 0 {
		t.Errorf("sem notas num ambiente saudável: %v", r.Notes)
	}
}

func TestWSL2SemWSLgUsaONavegadorDoWindows(t *testing.T) {
	r := Detect(context.Background(), wsl2SemWSLg().probe())
	if r.GraphicalDisplay || r.WSLg.Present || r.Mode != ModeWindowsBrowser {
		t.Fatalf("modo: %+v mode=%s", r.WSLg, r.Mode)
	}
	if !cap(r, "open_url").OK || !strings.Contains(cap(r, "open_url").Via, "Windows") || !strings.Contains(cap(r, "open_url").Via, "Google Chrome") {
		t.Errorf("abrir link: %+v", cap(r, "open_url"))
	}
	if !cap(r, "az_login").OK {
		t.Errorf("az login pela interface: %+v", cap(r, "az_login"))
	}
	// o que precisa CONTROLAR um navegador não funciona, e a mensagem explica por quê
	for _, id := range []string{"teams_automation", "servicenow_login"} {
		c := cap(r, id)
		if c.OK || c.Detail == "" {
			t.Errorf("%s não deveria funcionar sem display: %+v", id, c)
		}
	}
	if !strings.Contains(cap(r, "teams_automation").Detail, "WSLg") || !strings.Contains(cap(r, "teams_automation").Detail, "não consegue controlá-lo") {
		t.Errorf("explicação do limite: %q", cap(r, "teams_automation").Detail)
	}
	joined := strings.Join(r.Notes, "|")
	if !strings.Contains(joined, "/mnt/wslg não existe") || !strings.Contains(joined, "não pode ser controlado") {
		t.Errorf("notas: %v", r.Notes)
	}
}

func TestDisplayDefinidoMasMortoNaoContaComoGrafico(t *testing.T) {
	f := wsl2ComWSLg()
	f.dialOK = map[string]bool{} // o socket existe, ninguém atende (WSLg parado)
	f.exists["/mnt/wslg"] = false
	r := Detect(context.Background(), f.probe())
	if r.GraphicalDisplay || r.Mode != ModeWindowsBrowser {
		t.Fatalf("socket morto não é display: %+v mode=%s", r.WSLg, r.Mode)
	}
	if !r.WSLg.X11Socket || r.WSLg.X11Reachable {
		t.Errorf("deveria ver o socket mas não alcançá-lo: %+v", r.WSLg)
	}
	if !strings.Contains(strings.Join(r.Notes, "|"), "ninguém atende") {
		t.Errorf("deveria explicar o DISPLAY morto: %v", r.Notes)
	}
}

func TestDisplayAusenteMasWSLgResponde(t *testing.T) {
	// serviço/sessão sem perfil de login: o processo não herdou DISPLAY, mas o servidor X do WSLg atende
	f := wsl2ComWSLg()
	f.env = map[string]string{"WSL_DISTRO_NAME": "Ubuntu-22.04"}
	delete(f.exists, "/run/user/1000/wayland-0")
	f.dialOK = map[string]bool{"unix:/tmp/.X11-unix/X0": true}
	r := Detect(context.Background(), f.probe())
	if !r.GraphicalDisplay || r.WSLg.SuggestDisplay != ":0" {
		t.Fatalf("deveria achar o display pelo socket: %+v", r.WSLg)
	}
	if !strings.Contains(strings.Join(r.Notes, "|"), "DISPLAY=:0") {
		t.Errorf("deveria sugerir DISPLAY=:0: %v", r.Notes)
	}
	set := map[string]string{}
	got := EnsureDisplayEnv(context.Background(), f.probe(), func(k, v string) error { set[k] = v; return nil })
	if got != ":0" || set["DISPLAY"] != ":0" {
		t.Errorf("EnsureDisplayEnv: got=%q set=%v", got, set)
	}
	// com DISPLAY já definido não mexe em nada
	f.env["DISPLAY"] = ":9"
	if got := EnsureDisplayEnv(context.Background(), f.probe(), func(k, v string) error { t.Error("não deveria definir"); return nil }); got != "" {
		t.Errorf("com DISPLAY definido: %q", got)
	}
	// sem nenhum display alcançável também não inventa
	g := wsl2SemWSLg()
	if got := EnsureDisplayEnv(context.Background(), g.probe(), func(k, v string) error { t.Error("não deveria definir"); return nil }); got != "" {
		t.Errorf("sem display: %q", got)
	}
}

func TestWSL1NaoTemWSLg(t *testing.T) {
	f := wsl2SemWSLg()
	f.files["/proc/version"] = "Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com) (gcc version 5.4.0)"
	r := Detect(context.Background(), f.probe())
	if !r.IsWSL || r.WSLVersion != 1 {
		t.Errorf("WSL 1: %+v", r)
	}
	if !strings.Contains(strings.Join(r.Notes, "|"), "WSL 1 não tem WSLg") {
		t.Errorf("notas: %v", r.Notes)
	}
}

func TestSemInteropNemDisplayFicaSoComAutomacaoSemJanela(t *testing.T) {
	f := wsl2SemWSLg()
	f.exists = map[string]bool{} // sem WSLInterop
	f.paths = map[string]string{"xdg-open": "/usr/bin/xdg-open"}
	r := Detect(context.Background(), f.probe())
	if r.Interop || r.Mode != ModeHeadlessOnly || cap(r, "open_url").OK {
		t.Fatalf("sem interop: %+v mode=%s open=%+v", r, r.Mode, cap(r, "open_url"))
	}
	if !strings.Contains(strings.Join(r.Notes, "|"), "interoperabilidade") {
		t.Errorf("notas: %v", r.Notes)
	}
	if !strings.Contains(cap(r, "az_login").Detail, "--use-device-code") {
		t.Errorf("az login deveria sugerir device code: %+v", cap(r, "az_login"))
	}
}

func TestLinuxNativoEOutrosSistemas(t *testing.T) {
	lin := &fake{goos: "linux", env: map[string]string{"DISPLAY": ":1"}, files: map[string]string{"/proc/version": "Linux version 6.5.0-generic"},
		exists: map[string]bool{"/tmp/.X11-unix/X1": true}, dialOK: map[string]bool{"unix:/tmp/.X11-unix/X1": true},
		paths: map[string]string{"xdg-open": "/usr/bin/xdg-open", "chromium": "/usr/bin/chromium"}}
	r := Detect(context.Background(), lin.probe())
	if r.IsWSL || !r.GraphicalDisplay || r.Mode != ModeGraphical || !cap(r, "teams_automation").OK || r.ChromePath != "/usr/bin/chromium" {
		t.Errorf("Linux nativo com X: %+v mode=%s", r, r.Mode)
	}

	for _, goos := range []string{"darwin", "windows"} {
		r := Detect(context.Background(), (&fake{goos: goos}).probe())
		if !r.GraphicalDisplay || r.Mode != ModeGraphical || !cap(r, "open_url").OK {
			t.Errorf("%s: %+v mode=%s", goos, r, r.Mode)
		}
	}
}

func TestWaylandSoETCP(t *testing.T) {
	way := &fake{goos: "linux", env: map[string]string{"WAYLAND_DISPLAY": "wayland-1", "XDG_RUNTIME_DIR": "/run/user/1000"}, files: map[string]string{"/proc/version": "Linux x"},
		exists: map[string]bool{"/run/user/1000/wayland-1": true}, dialOK: map[string]bool{"unix:/run/user/1000/wayland-1": true}}
	if r := Detect(context.Background(), way.probe()); !r.WSLg.WaylandReach || !r.GraphicalDisplay {
		t.Errorf("Wayland: %+v", r.WSLg)
	}
	tcp := &fake{goos: "linux", env: map[string]string{"DISPLAY": "192.168.0.5:0"}, files: map[string]string{"/proc/version": "Linux x"}, dialOK: map[string]bool{"tcp:192.168.0.5:6000": true}}
	if r := Detect(context.Background(), tcp.probe()); !r.WSLg.X11Reachable || !r.GraphicalDisplay {
		t.Errorf("X11 por TCP: %+v", r.WSLg)
	}
}

func TestNavegadorPadraoDoWindows(t *testing.T) {
	for progID, want := range map[string]string{
		"ChromeHTML": "Google Chrome", "ChromeHTML.ABC123": "Google Chrome", "MSEdgeHTM": "Microsoft Edge", "FirefoxURL-308046B0AF4A39CB": "Mozilla Firefox",
		"BraveHTML": "Brave", "OperaStable": "Opera", "VivaldiHTM.XYZ": "Vivaldi", "OutroHTML": "OutroHTML",
	} {
		if got := friendlyBrowser(progID); got != want {
			t.Errorf("%s -> %q, quero %q", progID, got, want)
		}
	}
	// sem reg.exe no PATH mas montado em /mnt/c: ainda acha
	f := wsl2SemWSLg()
	delete(f.paths, "reg.exe")
	f.exists["/mnt/c/Windows/System32/reg.exe"] = true
	f.runOut = map[string]string{`/mnt/c/Windows/System32/reg.exe query HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice /v ProgId`: strings.Replace(regChrome, "ChromeHTML", "MSEdgeHTM", 1)}
	if r := Detect(context.Background(), f.probe()); r.WindowsDefaultBrowser != "Microsoft Edge" {
		t.Errorf("reg.exe por caminho: %q", r.WindowsDefaultBrowser)
	}
	// o registro não respondeu: o navegador fica em branco, e o modo continua funcionando
	g := wsl2SemWSLg()
	g.runOut = map[string]string{}
	if r := Detect(context.Background(), g.probe()); r.WindowsDefaultBrowser != "" || r.Mode != ModeWindowsBrowser {
		t.Errorf("sem leitura do registro: %q %s", r.WindowsDefaultBrowser, r.Mode)
	}
}
