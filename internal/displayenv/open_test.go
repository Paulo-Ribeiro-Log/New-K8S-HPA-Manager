package displayenv

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestValidURL(t *testing.T) {
	for _, ok := range []string{"http://localhost:8080", "https://login.microsoftonline.com/x?a=1&b=2#frag", "https://devicelogin.microsoft.com/"} {
		if err := ValidURL(ok); err != nil {
			t.Errorf("%q deveria valer: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " http://x", "http://x ", "file:///etc/passwd", "javascript:alert(1)", "ftp://x", "http://", "//x", "https://x/a b", "https://x/\n", "calc.exe", "https://x/\x00"} {
		if err := ValidURL(bad); err == nil {
			t.Errorf("%q não deveria valer", bad)
		}
	}
}

func opener(f *fake, pref string, failFirst int) (*Opener, *[][]string) {
	var started [][]string
	n := 0
	return &Opener{Probe: f.probe(), Pref: pref, Start: func(name string, args ...string) error {
		started = append(started, append([]string{name}, args...))
		n++
		if n <= failFirst {
			return fmt.Errorf("falhou ao iniciar")
		}
		return nil
	}}, &started
}

const target = "http://localhost:8080/?a=1&b=2"

func TestSemDisplayAbreNoNavegadorDoWindows(t *testing.T) {
	o, started := opener(wsl2SemWSLg(), "", 0)
	via, err := o.OpenURL(context.Background(), target)
	if err != nil || !strings.Contains(via, "rundll32.exe") {
		t.Fatalf("via=%q err=%v", via, err)
	}
	want := []string{"/mnt/c/WINDOWS/system32/rundll32.exe", "url.dll,FileProtocolHandler", target}
	if len(*started) != 1 || strings.Join((*started)[0], "|") != strings.Join(want, "|") {
		t.Errorf("comando: %v", *started) // a URL com & vai como UM argumento, sem shell
	}
}

func TestWslviewTemPrioridade(t *testing.T) {
	f := wsl2SemWSLg()
	f.paths["wslview"] = "/usr/bin/wslview"
	o, started := opener(f, "", 0)
	if via, _ := o.OpenURL(context.Background(), target); via != "wslview" || (*started)[0][0] != "/usr/bin/wslview" {
		t.Errorf("via=%q %v", via, *started)
	}
}

func TestComDisplayContinuaNoNavegadorDoLinux(t *testing.T) {
	o, started := opener(wsl2ComWSLg(), "", 0)
	if via, err := o.OpenURL(context.Background(), target); err != nil || via != "xdg-open" || (*started)[0][0] != "/usr/bin/xdg-open" {
		t.Errorf("com WSLg: via=%q err=%v %v", via, err, *started)
	}
	// e, se o Linux falhar ao iniciar, cai no Windows
	o, started = opener(wsl2ComWSLg(), "", 1)
	if via, err := o.OpenURL(context.Background(), target); err != nil || !strings.Contains(via, "rundll32.exe") || len(*started) != 2 {
		t.Errorf("fallback: via=%q err=%v %v", via, err, *started)
	}
}

func TestPreferenciaForcada(t *testing.T) {
	// windows: mesmo com display, abre no Windows
	o, started := opener(wsl2ComWSLg(), "windows", 0)
	if via, _ := o.OpenURL(context.Background(), target); !strings.Contains(via, "rundll32.exe") || strings.Contains((*started)[0][0], "xdg-open") {
		t.Errorf("windows forçado: %q %v", via, *started)
	}
	// linux: mesmo sem display, xdg-open (comportamento antigo)
	o, started = opener(wsl2SemWSLg(), "linux", 0)
	f := wsl2SemWSLg()
	f.paths["xdg-open"] = "/usr/bin/xdg-open"
	o, started = opener(f, "linux", 0)
	if via, _ := o.OpenURL(context.Background(), target); via != "xdg-open" || (*started)[0][0] != "/usr/bin/xdg-open" {
		t.Errorf("linux forçado: %q %v", via, *started)
	}
}

func TestNaoAbreURLInvalidaNemSemNavegador(t *testing.T) {
	o, started := opener(wsl2SemWSLg(), "", 0)
	if _, err := o.OpenURL(context.Background(), "file:///etc/passwd"); err == nil || len(*started) != 0 {
		t.Errorf("URL inválida não pode abrir nada: err=%v %v", err, *started)
	}
	f := wsl2SemWSLg()
	f.exists = map[string]bool{} // sem interop
	delete(f.paths, "xdg-open")  // e sem navegador no Linux
	o, started = opener(f, "", 0)
	_, err := o.OpenURL(context.Background(), target)
	if err == nil || !strings.Contains(err.Error(), target) || len(*started) != 0 {
		t.Errorf("sem navegador, o erro deve trazer a URL para abrir à mão: %v", err)
	}
	// tudo falha ao iniciar: o erro lista as tentativas e traz a URL
	o, _ = opener(wsl2SemWSLg(), "", 99)
	if _, err := o.OpenURL(context.Background(), target); err == nil || !strings.Contains(err.Error(), "rundll32.exe") || !strings.Contains(err.Error(), target) {
		t.Errorf("erro com as tentativas: %v", err)
	}
}

func TestOutrosSistemasUsamOComandoNativo(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "open", "windows": "rundll32"} {
		o, started := opener(&fake{goos: goos}, "", 0)
		if _, err := o.OpenURL(context.Background(), target); err != nil || (*started)[0][0] != want {
			t.Errorf("%s: %v %v", goos, err, *started)
		}
	}
}

func TestAzLoginEnv(t *testing.T) {
	ctx := context.Background()
	// WSL sem display: BROWSER aponta para o Windows (rundll32 com %s; exit 0, como o Python espera)
	env := AzLoginEnv(ctx, wsl2SemWSLg().probe(), "")
	if len(env) != 1 || env[0] != "BROWSER=/mnt/c/WINDOWS/system32/rundll32.exe url.dll,FileProtocolHandler %s" {
		t.Errorf("sem display: %v", env)
	}
	// wslview, quando existe, vence
	f := wsl2SemWSLg()
	f.paths["wslview"] = "/usr/bin/wslview"
	if env := AzLoginEnv(ctx, f.probe(), ""); len(env) != 1 || env[0] != "BROWSER=wslview %s" {
		t.Errorf("wslview: %v", env)
	}
	// com display, não mexe (comportamento de sempre)…
	if env := AzLoginEnv(ctx, wsl2ComWSLg().probe(), ""); env != nil {
		t.Errorf("com display não deve mudar nada: %v", env)
	}
	// …a não ser que se force o Windows, ou se force o Linux
	if env := AzLoginEnv(ctx, wsl2ComWSLg().probe(), "windows"); len(env) != 1 || !strings.Contains(env[0], "rundll32.exe") {
		t.Errorf("windows forçado: %v", env)
	}
	if env := AzLoginEnv(ctx, wsl2SemWSLg().probe(), "linux"); env != nil {
		t.Errorf("linux forçado: %v", env)
	}
	// fora do WSL, nada
	if env := AzLoginEnv(ctx, (&fake{goos: "linux", files: map[string]string{"/proc/version": "Linux x"}}).probe(), ""); env != nil {
		t.Errorf("Linux nativo: %v", env)
	}
	// só explorer.exe: sai com código 1 mesmo funcionando, o Python o trataria como falha → não usa
	g := wsl2SemWSLg()
	delete(g.paths, "rundll32.exe")
	if env := AzLoginEnv(ctx, g.probe(), ""); env != nil {
		t.Errorf("só explorer.exe: %v", env)
	}
}
