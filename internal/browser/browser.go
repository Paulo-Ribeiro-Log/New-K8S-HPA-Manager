// Package browser centraliza a lógica de lançamento/gerenciamento de processos Chrome/Chromium
// via go-rod, compartilhada entre internal/servicenow e internal/teams — as duas únicas features
// da aplicação que dirigem um browser real (extração de CHG e aprovações do Mr.ViaBot). Antes
// desta extração cada pacote reimplementava sua própria versão de "lançar/reconectar num Chrome
// persistente", "matar processos travados no mesmo perfil" e "minimizar/restaurar a janela" — ver
// BROWSER-CONSOLIDATION-STUDY.md (Fase 1) para o levantamento completo da duplicação.
//
// Fase 1 é refatoração pura: cada chamador continua configurando exatamente o que já configurava
// (headless ou não, Chromium embed ou Chrome do sistema, flags do launcher) — nenhum
// comportamento observável muda, só a implementação passa a ser compartilhada.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// LaunchOptions parametriza o lançamento de um processo Chrome/Chromium via go-rod — cobre tanto
// o "embed" (SystemBin vazio, Chromium baixado/gerenciado pelo próprio Rod, revisão fixa) quanto
// um Chrome/Chromium já instalado no host (SystemBin apontando pro binário).
type LaunchOptions struct {
	SessionDir  string // UserDataDir — perfil isolado da sessão (nunca o perfil pessoal do usuário)
	Headless    bool
	SystemBin   string            // vazio = Chromium embutido do Rod; caminho = Chrome/Chromium do sistema
	Flags       map[string]string // launcher.Set(k, v); valor "" gera uma flag booleana (--k, sem "=")
	DeleteFlags []string          // launcher.Delete(k) — aplicado antes de Flags, mesma ordem que os call sites originais usavam
}

// Launch inicia um processo Chrome/Chromium (embed ou do sistema, conforme opts.SystemBin) e
// conecta via CDP. Retorna o *rod.Browser já conectado e uma função stop() que fecha a conexão —
// rod.Browser.Close() já envia Browser.close via CDP, encerrando o processo do SO.
func Launch(opts LaunchOptions) (*rod.Browser, func(), error) {
	l := launcher.New().UserDataDir(opts.SessionDir).Headless(opts.Headless)

	for _, name := range opts.DeleteFlags {
		l = l.Delete(flags.Flag(name))
	}

	keys := make([]string, 0, len(opts.Flags))
	for k := range opts.Flags {
		keys = append(keys, k)
	}
	sort.Strings(keys) // ordem determinística — mapa em Go não garante ordem de iteração

	for _, k := range keys {
		if v := opts.Flags[k]; v == "" {
			l = l.Set(flags.Flag(k))
		} else {
			l = l.Set(flags.Flag(k), v)
		}
	}

	if opts.SystemBin != "" {
		l = l.Bin(opts.SystemBin)
	}

	ctrlURL, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao iniciar browser: %w", err)
	}

	b := rod.New().ControlURL(ctrlURL)
	if err := b.Connect(); err != nil {
		return nil, nil, fmt.Errorf("erro ao conectar ao browser: %w", err)
	}

	return b, func() { b.Close() /*nolint:errcheck*/ }, nil
}

// RemoveStaleLockFiles remove lock files residuais de um crash anterior do Chrome
// (SingletonLock/SingletonSocket/SingletonCookie) — sem isso, o Rod não consegue lançar uma nova
// instância de debug se o perfil já parece "em uso" por um processo morto.
func RemoveStaleLockFiles(sessionDir string, logger *zerolog.Logger) {
	for _, lock := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		lockPath := filepath.Join(sessionDir, lock)
		if _, err := os.Stat(lockPath); err == nil {
			if logger != nil {
				logger.Warn().Str("file", lockPath).Msg("[browser] Removendo lock residual de crash anterior")
			}
			os.Remove(lockPath) //nolint:errcheck
		}
	}
}

// FindSystemChrome localiza o Chrome/Chromium instalado no sistema (distinto do embed do Rod).
func FindSystemChrome() string {
	candidates := []string{
		"/usr/bin/google-chrome-stable",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/snap/bin/chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium-browser", "chromium"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// KillExistingChrome encerra processos Chrome/Chromium que já estejam usando sessionDir como
// perfil (recuperação de crash/restart anterior) — o Rod não consegue lançar uma nova instância
// de debug se o perfil já está travado por outro processo vivo. Tenta SIGTERM primeiro, SIGKILL
// só nos que sobrevivem 500ms depois.
func KillExistingChrome(sessionDir string, logger *zerolog.Logger) {
	out, err := exec.Command("pgrep", "-f", sessionDir).Output()
	if err != nil || len(out) == 0 {
		return
	}
	pids := strings.Fields(string(out))
	if logger != nil {
		logger.Info().Strs("pids", pids).Str("session_dir", sessionDir).
			Msg("[browser] Encerrando instâncias Chrome existentes com o mesmo perfil")
	}
	for _, pid := range pids {
		exec.Command("kill", "-TERM", pid).Run() //nolint:errcheck
	}
	time.Sleep(500 * time.Millisecond)
	out2, _ := exec.Command("pgrep", "-f", sessionDir).Output()
	for _, pid := range strings.Fields(string(out2)) {
		exec.Command("kill", "-9", pid).Run() //nolint:errcheck
	}
}

// RestoreWindow traz a janela do browser de volta ao estado normal (visível).
func RestoreWindow(page *rod.Page, logger *zerolog.Logger) {
	if err := page.SetWindow(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateNormal}); err != nil {
		// Não crítico: em WSL2 sem display gráfico (Xvfb) não existe uma janela real de verdade
		// pra restaurar — a extração continua funcionando normalmente mesmo se isso falhar.
		if logger != nil {
			logger.Debug().Err(err).Msg("[browser] Não foi possível restaurar a janela (não crítico)")
		}
	}
}

// MinimizeWindow minimiza a janela do browser.
func MinimizeWindow(page *rod.Page, logger *zerolog.Logger) {
	if err := page.SetWindow(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateMinimized}); err != nil {
		if logger != nil {
			logger.Debug().Err(err).Msg("[browser] Não foi possível minimizar a janela (não crítico)")
		}
	}
}

// Manager mantém um *rod.Browser persistente e reutilizável entre chamadas — lança um processo
// novo só na primeira chamada, se o processo anterior morreu, ou se o SessionDir pedido mudou
// (mesma checagem que ServiceNow e Teams já faziam cada um com sua própria cópia da lógica).
type Manager struct {
	mu         sync.Mutex
	sessionDir string
	browser    *rod.Browser
}

// Get retorna o browser persistente atual se ainda estiver vivo e opts.SessionDir bater com o da
// última chamada; caso contrário lança um novo processo via Launch(opts) — chamando beforeLaunch
// (se não-nil) logo antes, para o chamador fazer sua própria limpeza pré-launch (matar processos
// travados, esvaziar cache em disco, remover locks etc.) do jeito que cada feature já fazia, e
// afterLaunch (se não-nil) só quando um browser novo de fato foi lançado — nunca no caminho de
// reuso — para preservar o log "iniciado" que cada chamador já emitia só nesse caso.
func (m *Manager) Get(opts LaunchOptions, beforeLaunch, afterLaunch func(), logger *zerolog.Logger) (*rod.Browser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser != nil && m.sessionDir == opts.SessionDir {
		if _, err := m.browser.Pages(); err == nil {
			return m.browser, nil
		}
		if logger != nil {
			logger.Warn().Msg("[browser] Browser persistente morreu — relançando")
		}
		m.browser = nil
	}

	if beforeLaunch != nil {
		beforeLaunch()
	}

	b, _, err := Launch(opts)
	if err != nil {
		return nil, err
	}
	m.browser = b
	m.sessionDir = opts.SessionDir
	if afterLaunch != nil {
		afterLaunch()
	}
	return b, nil
}

// Close encerra o browser persistente, se houver algum aberto. A próxima chamada a Get relança
// do zero.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.browser != nil {
		m.browser.Close() //nolint:errcheck
		m.browser = nil
	}
}
