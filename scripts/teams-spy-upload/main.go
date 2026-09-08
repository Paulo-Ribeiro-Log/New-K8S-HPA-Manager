// Espia o protocolo de UPLOAD DE MÍDIA do Teams via HijackRouter — irmão de
// scripts/teams-spy-send/main.go (que já descobriu o protocolo de envio de TEXTO). Este script
// existe porque o envio de mensagem via chatsvcagg (já implementado em internal/teams/sender.go)
// aceita só HTML de texto — embutir uma imagem como "<img src="data:...">" é REJEITADO pelo
// backend do Teams (confirmado ao vivo: a mensagem chega com um ícone de imagem quebrada). O
// protocolo real de imagem exige primeiro fazer upload dela pro serviço de mídia (o mesmo AMS —
// "http://schema.skype.com/AMSImage" — visto no HTML de uma mensagem recebida, ver
// internal/teams/message_fetch.go) e só DEPOIS referenciar o objeto resultante na mensagem.
//
// Como usar:
//  1. go run ./scripts/teams-spy-upload/
//  2. O Chrome abrirá com a sessão existente do Teams.
//  3. Abra manualmente um chat de TESTE e cole/envie UMA imagem (Ctrl+V de um print, por
//     exemplo) — não precisa ser uma conversa real, qualquer chat de teste serve.
//  4. O script captura e imprime CADA requisição de upload (URL, método, headers relevantes) E a
//     RESPOSTA do servidor (status + corpo, geralmente JSON com o objectId a referenciar depois).
//  5. Ctrl+C para encerrar depois da captura — pode ser necessário capturar mais de uma
//     requisição (o protocolo AMS costuma ter 2 etapas: criar o objeto vazio, depois enviar os
//     bytes) — o script continua capturando até você encerrar manualmente.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	fmt.Println(" Teams — Espião de UPLOAD DE MÍDIA (HijackRouter)")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf(" Sessão: %s\n\n", sessionDir)
	fmt.Println(" INSTRUÇÕES:")
	fmt.Println("  1. Aguarde o Teams carregar completamente")
	fmt.Println("  2. Abra um chat de TESTE e cole/envie UMA imagem (Ctrl+V de um print, ex.)")
	fmt.Println("  3. O script vai capturar e imprimir a(s) requisição(ões) de upload + resposta")
	fmt.Println("  4. Ctrl+C para encerrar (pode levar mais de 1 requisição — deixe rodando)")
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

	page = page.Timeout(10 * time.Minute)

	fmt.Println("\n[2/2] HijackRouter ativo — interceptando upload de mídia...")
	fmt.Println("      >> Cole/envie uma imagem de teste no Teams agora <<")
	fmt.Println()

	router := page.HijackRequests()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	err = router.Add("*", "", func(h *rod.Hijack) {
		req := h.Request
		method := strings.ToUpper(req.Method())
		url := req.URL().String()
		ul := strings.ToLower(url)
		ct := strings.ToLower(req.Header("content-type"))

		// Filtro amplo — qualquer sinal de upload de mídia/objeto AMS: URL contendo o gateway já
		// confirmado (asyncgw), "objects" (o path do AMSImage, ex: .../v1/<id>/objects/<objId>/...),
		// "assets"/"amsgw"/"skypeobjects" (nomes alternativos vistos em implementações de
		// terceiro do mesmo protocolo), OU um Content-Type de imagem/binário/multipart cru
		// (comum no PUT que de fato manda os bytes, independente da URL) — OU o envio da mensagem
		// em si (chatsvcagg, mesmo endpoint já usado por internal/teams/sender.go): precisamos ver
		// o "content" (HTML) exato que referencia a imagem depois do upload, não só os uploads.
		looksLikeUpload := strings.Contains(ul, "asyncgw") ||
			strings.Contains(ul, "/objects") ||
			strings.Contains(ul, "assets") ||
			strings.Contains(ul, "amsgw") ||
			strings.Contains(ul, "skypeobjects") ||
			strings.HasPrefix(ct, "image/") ||
			strings.Contains(ct, "octet-stream") ||
			strings.Contains(ct, "multipart/") ||
			strings.Contains(ul, "chatsvcagg") ||
			strings.Contains(ul, "/messages")

		if !looksLikeUpload || (method != "POST" && method != "PUT") {
			h.ContinueRequest(&proto.FetchContinueRequest{})
			return
		}

		// LoadResponse: envia a requisição pro destino real e carrega a resposta pra inspeção —
		// o Teams recebe a resposta normalmente (não travamos nada), só ganhamos visibilidade
		// do que ele devolve (ver comentário de LoadResponse no vendor do go-rod).
		if err := h.LoadResponse(httpClient, true); err != nil {
			fmt.Printf("[erro ao carregar resposta real] %s %s: %v\n", method, url, err)
			h.ContinueRequest(&proto.FetchContinueRequest{})
			return
		}

		printCapture(method, url, reqHeadersToMap(req.Headers()), req.Body(), h.Response.Payload().ResponseCode, resHeadersToMap(h.Response.Headers()), string(h.Response.Payload().Body))
	})
	must(err, "HijackRouter.Add")

	go router.Run()
	defer router.Stop() //nolint:errcheck

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("\nEncerrado pelo usuário.")
}

// reqHeadersToMap/resHeadersToMap normalizam os dois tipos de headers que o go-rod expõe
// (proto.NetworkHeaders pro request via CDP, http.Header pra resposta via LoadResponse) pro mesmo
// formato simples — só pra printHeadersPrioritized não precisar de 2 implementações.
func reqHeadersToMap(h proto.NetworkHeaders) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		m[k] = v.String()
	}
	return m
}

func resHeadersToMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		m[k] = strings.Join(v, ", ")
	}
	return m
}

func printCapture(method, url string, reqHeaders map[string]string, reqBody string, status int, resHeaders map[string]string, resBody string) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  UPLOAD DE MÍDIA CAPTURADO                                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("Método : %s\n", method)
	fmt.Printf("URL    : %s\n\n", url)

	fmt.Println("── Request Headers ──────────────────────────────────────────")
	printHeadersPrioritized(reqHeaders)

	fmt.Println("\n── Request Body ─────────────────────────────────────────────")
	printBodySafely(reqBody)

	fmt.Printf("\n── Response Status: %d ─────────────────────────────────────\n", status)
	fmt.Println("── Response Headers ─────────────────────────────────────────")
	printHeadersPrioritized(resHeaders)

	fmt.Println("\n── Response Body ────────────────────────────────────────────")
	printBodySafely(resBody)
	fmt.Println("───────────────────────────────────────────────────────────────")
}

func printHeadersPrioritized(headers map[string]string) {
	priority := []string{
		"authorization", "x-skypetoken", "authentication",
		"clientinfo", "clientrequestid", "content-type", "content-length",
		"behavioroverride", "x-ms-client-request-id", "location",
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
}

// printBodySafely nunca despeja bytes binários crus no terminal — imagens/bytes de mídia real
// costumam ser grandes e ilegíveis; mostramos só o tamanho e uma prévia curta em hex nesse caso.
func printBodySafely(body string) {
	if body == "" {
		fmt.Println("  (vazio)")
		return
	}
	var pretty interface{}
	if err := json.Unmarshal([]byte(body), &pretty); err == nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(pretty) //nolint:errcheck
		return
	}
	if len(body) > 500 {
		fmt.Printf("  (binário/texto grande, %d bytes — prévia hex dos primeiros 32 bytes: %x)\n", len(body), []byte(body)[:32])
		return
	}
	fmt.Println(" ", body)
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
