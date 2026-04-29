// Espia o protocolo de envio de mensagens do Teams.
//
// Como usar:
//  1. go run ./scripts/teams-spy-send/
//  2. O Chrome abrirá com a sessão existente do Teams.
//  3. Abra manualmente um chat qualquer e envie UMA mensagem de teste.
//  4. O script imprime a requisição HTTP exata (URL, headers, body).
//  5. Ctrl+C para encerrar depois da captura.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// spyJS instala um interceptor de fetch/XHR na página e armazena
// cada POST em window.__spyResults (acessível via page.Eval).
// Formato: async () => valor  — obrigatório para page.Eval do go-rod.
const spyJS = `async () => {
  if (window.__spyInstalled) return 'already';
  window.__spyInstalled = true;
  window.__spyResults   = [];

  // ── fetch ────────────────────────────────────────────────────────────────
  const _fetch = window.fetch;
  window.fetch = async function(input, init) {
    const url    = (typeof input === 'string' ? input : (input && input.url)) || '';
    const method = (init && init.method) ? init.method.toUpperCase() : 'GET';
    if (method === 'POST') {
      try {
        const h = {};
        if (init && init.headers) {
          if (typeof init.headers.forEach === 'function') {
            init.headers.forEach((v, k) => { h[k] = v; });
          } else {
            Object.assign(h, init.headers);
          }
        }
        let body = '';
        if (init && init.body != null) {
          body = typeof init.body === 'string' ? init.body : String(init.body);
        }
        window.__spyResults.push({ source: 'fetch', url, method, headers: h, body });
      } catch(e) {}
    }
    return _fetch.apply(this, arguments);
  };

  // ── XHR ──────────────────────────────────────────────────────────────────
  const _open      = XMLHttpRequest.prototype.open;
  const _send      = XMLHttpRequest.prototype.send;
  const _setHeader = XMLHttpRequest.prototype.setRequestHeader;

  XMLHttpRequest.prototype.open = function(method, url) {
    this.__spyMethod  = (method || '').toUpperCase();
    this.__spyURL     = url || '';
    this.__spyHeaders = {};
    return _open.apply(this, arguments);
  };
  XMLHttpRequest.prototype.setRequestHeader = function(k, v) {
    if (this.__spyHeaders) this.__spyHeaders[k] = v;
    return _setHeader.apply(this, arguments);
  };
  XMLHttpRequest.prototype.send = function(body) {
    if (this.__spyMethod === 'POST') {
      try {
        window.__spyResults.push({
          source:  'xhr',
          url:     this.__spyURL,
          method:  this.__spyMethod,
          headers: this.__spyHeaders || {},
          body:    (body != null ? String(body) : ''),
        });
      } catch(e) {}
    }
    return _send.apply(this, arguments);
  };

  return 'installed';
}`

type spyEntry struct {
	Source  string            `json:"source"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func main() {
	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")

	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println(" Teams — Espião de envio de mensagem (fetch/XHR spy)")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf(" Sessão: %s\n\n", sessionDir)

	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "ERRO: sessão não encontrada em", sessionDir)
		os.Exit(1)
	}

	killChrome(sessionDir)
	time.Sleep(800 * time.Millisecond)

	chromeBin := findChrome()
	l := launcher.New().
		UserDataDir(sessionDir).
		Headless(false).
		Delete("enable-automation").
		Set("disable-blink-features", "AutomationControlled").
		Set("no-first-run").
		Set("no-default-browser-check").
		Set("disk-cache-size", "33554432").
		Set("aggressive-cache-discard")
	if chromeBin != "" {
		l = l.Bin(chromeBin)
		fmt.Printf("[chrome] %s\n", chromeBin)
	}

	ctrlURL, err := l.Launch()
	must(err, "iniciar Chrome")

	browser := rod.New().ControlURL(ctrlURL)
	must(browser.Connect(), "conectar ao browser")
	defer browser.Close()

	_, err = browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	must(err, "criar página")

	fmt.Println("\n[1/3] Aguardando Teams carregar (máx 3 min)...")
	page := waitTeams(browser, 3*time.Minute)
	if page == nil {
		fmt.Fprintln(os.Stderr, "ERRO: timeout aguardando Teams")
		os.Exit(1)
	}
	fmt.Println("      Teams detectado — aguardando estabilizar (15s)...")
	time.Sleep(15 * time.Second)

	fmt.Println("\n[2/3] Injetando spy de fetch/XHR...")
	res, err := page.Eval(spyJS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "AVISO: falha ao injetar spy: %v\n", err)
	} else {
		fmt.Printf("      Resultado: %s\n", res.Value.String())
	}

	fmt.Println("\n[3/3] Monitorando (polling a cada 500ms)...")
	fmt.Println("      >> Envie uma mensagem de teste no Teams agora <<\n")

	seen := 0
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			fmt.Println("\nEncerrado.")
			return
		case <-ticker.C:
			// Re-injetar spy se a página navegou (Teams faz soft-navigations).
			checkAndReinject(page)

			res, err := page.Eval(`JSON.stringify(window.__spyResults || [])`)
			if err != nil {
				continue
			}
			raw := res.Value.String()
			// CDP envolve strings em aspas — remover.
			raw = strings.Trim(raw, `"`)
			raw = strings.ReplaceAll(raw, `\"`, `"`)
			raw = strings.ReplaceAll(raw, `\\n`, "\n")

			var entries []spyEntry
			if err := json.Unmarshal([]byte(raw), &entries); err != nil {
				continue
			}
			if len(entries) <= seen {
				continue
			}
			for i := seen; i < len(entries); i++ {
				printCapture(entries[i])
			}
			seen = len(entries)
		}
	}
}

// checkAndReinject reinstala o spy se window.__spyInstalled for falsy.
func checkAndReinject(page *rod.Page) {
	res, err := page.Eval(`!!window.__spyInstalled`)
	if err != nil {
		return
	}
	if res.Value.String() == "false" {
		page.Eval(spyJS) //nolint:errcheck
	}
}

func printCapture(e spyEntry) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Printf("║  CAPTURADO via %-46s║\n", e.Source+"                                              ")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("URL    : %s\n", e.URL)
	fmt.Printf("Método : %s\n\n", e.Method)

	fmt.Println("── Headers ───────────────────────────────────────────────────")
	priority := []string{
		"authorization", "x-skypetoken", "authentication",
		"clientinfo", "clientrequestid", "content-type",
		"user-agent", "x-ms-client-request-id",
	}
	printed := map[string]bool{}
	for _, pk := range priority {
		for k, v := range e.Headers {
			if strings.EqualFold(k, pk) && !printed[k] {
				fmt.Printf("  %-40s %s\n", k+":", v)
				printed[k] = true
			}
		}
	}
	for k, v := range e.Headers {
		if !printed[k] {
			fmt.Printf("  %-40s %s\n", k+":", v)
		}
	}

	fmt.Println("\n── Body ──────────────────────────────────────────────────────")
	if e.Body == "" {
		fmt.Println("  (vazio)")
	} else {
		var pretty interface{}
		if err := json.Unmarshal([]byte(e.Body), &pretty); err == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(pretty) //nolint:errcheck
		} else {
			fmt.Println(e.Body)
		}
	}
	fmt.Println("──────────────────────────────────────────────────────────────")
}

func waitTeams(browser *rod.Browser, timeout time.Duration) *rod.Page {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pages, _ := browser.Pages()
		for _, p := range pages {
			info, err := p.Info()
			if err != nil {
				continue
			}
			u := info.URL
			if u == "" || u == "about:blank" ||
				!strings.Contains(u, "teams.microsoft.com") ||
				strings.Contains(u, "/error") ||
				strings.Contains(u, "login.microsoftonline") {
				continue
			}
			fmt.Printf("      URL: %s\n", u)
			return p
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func killChrome(sessionDir string) {
	out, err := exec.Command("pgrep", "-f", sessionDir).Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, pid := range strings.Fields(string(out)) {
		exec.Command("kill", "-TERM", pid).Run() //nolint:errcheck
	}
	time.Sleep(500 * time.Millisecond)
	out2, _ := exec.Command("pgrep", "-f", sessionDir).Output()
	for _, pid := range strings.Fields(string(out2)) {
		exec.Command("kill", "-9", pid).Run() //nolint:errcheck
	}
}

func findChrome() string {
	for _, p := range []string{
		"/usr/bin/google-chrome-stable", "/usr/bin/google-chrome",
		"/usr/bin/chromium-browser", "/usr/bin/chromium", "/snap/bin/chromium",
	} {
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

func must(err error, ctx string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRO (%s): %v\n", ctx, err)
		os.Exit(1)
	}
}
