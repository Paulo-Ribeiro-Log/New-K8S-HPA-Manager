// Espia o protocolo de envio de mensagens do Teams via HijackRouter.
//
// Como usar:
//  1. go run ./scripts/teams-spy-send/
//  2. O Chrome abrirá com a sessão existente do Teams.
//  3. Abra manualmente um chat qualquer e envie UMA mensagem de teste.
//  4. O script captura e imprime a requisição HTTP exata (URL, headers, body).
//  5. Ctrl+C para encerrar depois da captura.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func main() {
	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")

	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println(" Teams — Espião de envio de mensagem (HijackRouter)")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf(" Sessão: %s\n\n", sessionDir)
	fmt.Println(" INSTRUÇÕES:")
	fmt.Println("  1. Aguarde o Teams carregar completamente")
	fmt.Println("  2. Abra qualquer chat e envie UMA mensagem de teste")
	fmt.Println("  3. O script vai capturar e imprimir a requisição")
	fmt.Println("  4. Ctrl+C para encerrar")
	fmt.Println()

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

	fmt.Println("\n[1/2] Aguardando Teams carregar (máx 3 min)...")
	page := waitTeams(browser, 3*time.Minute)
	if page == nil {
		fmt.Fprintln(os.Stderr, "ERRO: timeout aguardando Teams")
		os.Exit(1)
	}
	fmt.Println("      Teams detectado — aguardando estabilizar (15s)...")
	time.Sleep(15 * time.Second)

	// Usar timeout generoso para o hijack — a sessão fica aberta até Ctrl+C.
	page = page.Timeout(10 * time.Minute)

	fmt.Println("\n[2/2] HijackRouter ativo — interceptando POSTs...")
	fmt.Println("      >> Envie uma mensagem de teste no Teams agora <<")
	fmt.Println()

	router := page.HijackRequests()

	captured := make(chan struct{}, 1)

	// Interceptar TODOS os requests — filtramos por POST dentro do handler.
	err = router.Add("*", "", func(h *rod.Hijack) {
		req := h.Request
		method := strings.ToUpper(req.Method())
		url := req.URL().String()

		// Continuar a requisição imediatamente (não bloquear o Teams).
		h.ContinueRequest(&proto.FetchContinueRequest{})

		if method != "POST" {
			return
		}

		// Mostrar qualquer POST para microsoft.com (independente de padrão).
		ul := strings.ToLower(url)
		if !strings.Contains(ul, "microsoft.com") && !strings.Contains(ul, "teams.") {
			return
		}

		body := req.Body()
		headers := req.Headers()

		// Tentar detectar se é envio de mensagem.
		isSend := strings.Contains(ul, "messages") ||
			strings.Contains(ul, "sendmessage") ||
			strings.Contains(ul, "chatsvcagg") ||
			strings.Contains(ul, "/api/mt/") ||
			strings.Contains(ul, "/api/v2/") ||
			strings.Contains(body, "messagetype") ||
			strings.Contains(body, "content")

		if isSend {
			printCapture(method, url, headers, body)
			select {
			case captured <- struct{}{}:
			default:
			}
		} else {
			// Imprimir URL de qualquer outro POST microsoft para descoberta.
			fmt.Printf("[post] %s\n", url)
			if len(body) > 0 && len(body) < 300 {
				fmt.Printf("       %s\n", body)
			}
		}
	})
	must(err, "HijackRouter.Add")

	go router.Run()
	defer router.Stop() //nolint:errcheck

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		fmt.Println("\nEncerrado pelo usuário.")
	case <-captured:
		fmt.Println("\nCaptura concluída. Ctrl+C para encerrar...")
		<-sig
	}
}

func printCapture(method, url string, headers proto.NetworkHeaders, body string) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  REQUISIÇÃO CAPTURADA                                        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("Método : %s\n", method)
	fmt.Printf("URL    : %s\n\n", url)

	fmt.Println("── Headers ───────────────────────────────────────────────────")
	priority := []string{
		"authorization", "x-skypetoken", "authentication",
		"clientinfo", "clientrequestid", "content-type",
		"behavioroverride", "x-ms-client-request-id",
	}
	printed := map[string]bool{}
	for _, pk := range priority {
		for k, v := range headers {
			if strings.EqualFold(k, pk) && !printed[k] {
				fmt.Printf("  %-40s %s\n", k+":", v)
				printed[k] = true
			}
		}
	}
	// Resto em ordem alfabética.
	keys := make([]string, 0, len(headers))
	for k := range headers {
		if !printed[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-40s %s\n", k+":", headers[k])
	}

	fmt.Println("\n── Body ──────────────────────────────────────────────────────")
	if body == "" {
		fmt.Println("  (vazio)")
	} else {
		var pretty interface{}
		if err := json.Unmarshal([]byte(body), &pretty); err == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(pretty) //nolint:errcheck
		} else {
			fmt.Println(body)
		}
	}
	fmt.Println("──────────────────────────────────────────────────────────────")
}

func waitTeams(browser *rod.Browser, timeout time.Duration) *rod.Page {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pages, _ := browser.Pages()
		for _, p := range pages {
			info, _ := p.Info()
			if info == nil {
				continue
			}
			u := info.URL
			if strings.Contains(u, "teams.microsoft.com") &&
				!strings.Contains(u, "/error") &&
				!strings.Contains(u, "login.microsoftonline") &&
				u != "about:blank" {
				fmt.Printf("      URL: %s\n", u)
				return p
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func killChrome(sessionDir string) {
	out, _ := exec.Command("pgrep", "-f", sessionDir).Output()
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
		"/usr/bin/chromium-browser", "/usr/bin/chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium"} {
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
