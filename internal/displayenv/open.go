package displayenv

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// EnvBrowser escolhe onde abrir links: "auto" (padrão), "windows" ou "linux".
//   - auto: o navegador do Linux quando há display; no WSL SEM display, o navegador padrão do Windows.
//   - windows: no WSL, sempre o navegador padrão do Windows (o das suas sessões e do SSO).
//   - linux: sempre xdg-open (comportamento antigo).
const EnvBrowser = "K8S_HPA_BROWSER"

// ValidURL aceita só http/https, com host e sem espaços nem caracteres de controle: a URL vira
// argumento de um programa (nunca passa por um shell), mas não há motivo para aceitar mais que isso.
func ValidURL(raw string) error {
	if strings.TrimSpace(raw) != raw || raw == "" {
		return fmt.Errorf("URL vazia ou com espaços nas pontas")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || r == ' ' {
			return fmt.Errorf("URL com espaço ou caractere de controle")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL inválida: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("só URLs http/https com host são abertas")
	}
	return nil
}

// plan é um jeito de abrir a URL: um programa e os argumentos.
type plan struct {
	Via  string
	Name string
	Args []string
}

func (p plan) String() string { return p.Via }

// windowsExe devolve o caminho de um programa do Windows visível no WSL (PATH ou /mnt/c).
func windowsExe(p Probe, name string) (string, bool) {
	if path, err := p.LookPath(name); err == nil {
		return path, true
	}
	for _, base := range windowsSystemDirs {
		if p.Exists(base + "/" + name) {
			return base + "/" + name, true
		}
	}
	return "", false
}

func windowsPlans(p Probe, target string) []plan {
	var out []plan
	if path, err := p.LookPath("wslview"); err == nil { // do pacote wslu: o jeito oficial
		out = append(out, plan{"wslview", path, []string{target}})
	}
	if path, ok := windowsExe(p, "rundll32.exe"); ok { // abre no navegador padrão; sem shell, aceita & na URL
		out = append(out, plan{"rundll32.exe (navegador padrão do Windows)", path, []string{"url.dll,FileProtocolHandler", target}})
	}
	if path, ok := windowsExe(p, "explorer.exe"); ok {
		out = append(out, plan{"explorer.exe (navegador padrão do Windows)", path, []string{target}})
	}
	return out
}

// planOpen monta as tentativas, em ordem, para abrir a URL neste ambiente.
func planOpen(r Report, p Probe, target, pref string) []plan {
	switch r.OS {
	case "darwin":
		return []plan{{"open", "open", []string{target}}}
	case "windows":
		return []plan{{"rundll32 (navegador padrão)", "rundll32", []string{"url.dll,FileProtocolHandler", target}}}
	}

	var xdg []plan
	if path, err := p.LookPath("xdg-open"); err == nil {
		xdg = []plan{{"xdg-open", path, []string{target}}}
	}
	if !r.IsWSL || !r.Interop {
		if pref == "windows" {
			return nil // fora do WSL não há navegador do Windows para chamar
		}
		return xdg
	}

	win := windowsPlans(p, target)
	switch pref {
	case "linux":
		return xdg // o comportamento antigo, com ou sem display
	case "windows":
		return win
	}
	// auto: com display, o Linux; sem display, o navegador do Windows
	if r.GraphicalDisplay {
		return append(xdg, win...)
	}
	return win
}

// Opener abre URLs. Start é injetável para os testes não abrirem navegador de verdade.
type Opener struct {
	Probe Probe
	Pref  string // "", "auto", "windows" ou "linux" (K8S_HPA_BROWSER)
	Start func(name string, args ...string) error
}

// NewOpener é o Opener real deste processo.
func NewOpener() *Opener {
	return &Opener{
		Probe: DefaultProbe(),
		Pref:  strings.ToLower(os.Getenv(EnvBrowser)),
		Start: func(name string, args ...string) error { return exec.Command(name, args...).Start() },
	}
}

// OpenURL abre a URL no navegador adequado ao ambiente e diz por qual caminho abriu. Não espera o
// navegador fechar. Se o 1º jeito falhar ao iniciar, tenta o seguinte.
func (o *Opener) OpenURL(ctx context.Context, target string) (via string, err error) {
	if err := ValidURL(target); err != nil {
		return "", err
	}
	r := Detect(ctx, o.Probe)
	plans := planOpen(r, o.Probe, target, o.Pref)
	if len(plans) == 0 {
		return "", fmt.Errorf("nenhum navegador acessível neste ambiente (%s): abra %s manualmente", r.Summary, target)
	}
	var errs []string
	for _, pl := range plans {
		if err := o.Start(pl.Name, pl.Args...); err != nil {
			errs = append(errs, pl.Via+": "+err.Error())
			continue
		}
		return pl.Via, nil
	}
	return "", fmt.Errorf("não consegui abrir o navegador (%s): abra %s manualmente", strings.Join(errs, "; "), target)
}

// AzLoginEnv devolve as variáveis de ambiente para o `az login`, que abre o navegador pelo módulo
// `webbrowser` do Python — este respeita BROWSER (comando com %s). No WSL sem display (ou com
// K8S_HPA_BROWSER=windows), aponta para o navegador padrão do Windows; nos demais casos não mexe
// em nada. Sem isso o `az login` pela interface falhava em silêncio onde não há navegador no Linux.
func AzLoginEnv(ctx context.Context, p Probe, pref string) []string {
	r := Detect(ctx, p)
	if !r.IsWSL || !r.Interop || pref == "linux" {
		return nil
	}
	if r.GraphicalDisplay && pref != "windows" {
		return nil
	}
	for _, pl := range windowsPlans(p, "%s") {
		switch {
		case pl.Via == "wslview":
			return []string{"BROWSER=wslview %s"}
		case strings.HasPrefix(pl.Via, "rundll32.exe"):
			return []string{"BROWSER=" + pl.Name + " url.dll,FileProtocolHandler %s"}
		}
	}
	return nil // explorer.exe sai com código 1 mesmo funcionando: o Python o trataria como falha
}

// EnsureDisplayEnv define DISPLAY quando o WSLg responde mas o processo não herdou a variável (serviço,
// sessão sem perfil de login). Sem ela, o Chrome do robô não acha o display, mesmo ele existindo.
// Devolve o valor definido ("" se nada mudou).
func EnsureDisplayEnv(ctx context.Context, p Probe, setenv func(k, v string) error) string {
	if p.Getenv("DISPLAY") != "" {
		return ""
	}
	r := Detect(ctx, p)
	if d := r.WSLg.SuggestDisplay; d != "" {
		if err := setenv("DISPLAY", d); err == nil {
			return d
		}
	}
	return ""
}
