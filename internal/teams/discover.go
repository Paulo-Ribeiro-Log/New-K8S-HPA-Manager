package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// CapturedRequest representa uma requisição/resposta capturada do Teams.
type CapturedRequest struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	ReqHeaders map[string]string `json:"req_headers,omitempty"`
	Status     int               `json:"status,omitempty"`
	Body       string            `json:"body,omitempty"`
	CapturedAt time.Time         `json:"captured_at"`
}

// CapturedWS representa um frame WebSocket capturado.
type CapturedWS struct {
	URL        string    `json:"url"`
	Direction  string    `json:"direction"` // "recv" | "send"
	Payload    string    `json:"payload"`
	CapturedAt time.Time `json:"captured_at"`
}

// DiscoveryResult é o resultado completo da sessão de descoberta.
type DiscoveryResult struct {
	CapturedAt    time.Time          `json:"captured_at"`
	AuthRequests  []CapturedRequest  `json:"auth_requests"`
	ChatRequests  []CapturedRequest  `json:"chat_requests"`
	OtherAPIs     []CapturedRequest  `json:"other_apis"`
	WebSockets    []CapturedWS       `json:"websockets"`
	SkypeToken    string             `json:"skype_token"`
	Conversations []ConversationHint `json:"conversations"`
}

// ConversationHint é uma conversa identificada na descoberta.
type ConversationHint struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	ThreadType  string `json:"thread_type"`
}

func isAuthURL(url string) bool {
	return strings.Contains(url, "authsvc") ||
		strings.Contains(url, "/authz") ||
		strings.Contains(url, "microsoftonline.com")
}

func isChatURL(url string) bool {
	return strings.Contains(url, "chatsvcagg") ||
		strings.Contains(url, "/conversations") ||
		strings.Contains(url, "/messages") ||
		strings.Contains(url, "api.spaces.skype") ||
		strings.Contains(url, "ng.msg.teams")
}

func isTeamsRelevant(url string) bool {
	keywords := []string{
		"teams.microsoft.com",
		"api.spaces.skype.com",
		"chatsvcagg",
		"authsvc",
		"ng.msg.teams",
		"microsoftonline.com",
	}
	for _, kw := range keywords {
		if strings.Contains(url, kw) {
			return true
		}
	}
	return false
}

// RunDiscovery lança o Chrome do sistema, navega para o Teams,
// intercepta TODA atividade de rede via CDP Network events e salva em outputDir.
func RunDiscovery(sessionDir, outputDir string, logger *zerolog.Logger, timeout time.Duration) (*DiscoveryResult, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de saída: %v", err)
	}

	chromeBin := findSystemChrome()
	if chromeBin != "" {
		logger.Info().Str("bin", chromeBin).Msg("[Teams] Usando Chrome do sistema")
	} else {
		logger.Warn().Msg("[Teams] Chrome do sistema não encontrado — usando Chromium do Rod")
	}

	l := launcher.New().
		UserDataDir(sessionDir).
		Headless(false).
		Delete("enable-automation").
		Set("disable-blink-features", "AutomationControlled").
		Set("no-first-run", "").
		Set("no-default-browser-check", "")

	if chromeBin != "" {
		l = l.Bin(chromeBin)
	}

	ctrlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar Chrome: %v", err)
	}

	browser := rod.New().ControlURL(ctrlURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar ao browser: %v", err)
	}
	defer browser.Close()

	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}

	result := &DiscoveryResult{CapturedAt: time.Now()}
	var mu sync.Mutex

	// Mapa requestId → URL para correlacionar request com response
	pendingReqs := map[proto.NetworkRequestID]CapturedRequest{}

	// ── Habilitar Network domain via CDP ──────────────────────────────────
	netEnable := proto.NetworkEnable{}
	if err := netEnable.Call(page); err != nil {
		return nil, fmt.Errorf("erro ao habilitar CDP Network: %v", err)
	}

	// ── Capturar requests ─────────────────────────────────────────────────
	go page.EachEvent(func(e *proto.NetworkRequestWillBeSent) {
		if !isTeamsRelevant(e.Request.URL) {
			return
		}
		reqHeaders := map[string]string{}
		for k, v := range e.Request.Headers {
			lk := strings.ToLower(k)
			if lk == "authorization" || lk == "x-skypetoken" || lk == "client-request-id" || lk == "x-ms-client-request-id" {
				reqHeaders[k] = fmt.Sprintf("%v", v)
			}
		}
		mu.Lock()
		pendingReqs[e.RequestID] = CapturedRequest{
			URL:        e.Request.URL,
			Method:     e.Request.Method,
			ReqHeaders: reqHeaders,
			CapturedAt: time.Now(),
		}
		mu.Unlock()
	})()

	// ── Capturar responses e body ─────────────────────────────────────────
	go page.EachEvent(func(e *proto.NetworkResponseReceived) {
		mu.Lock()
		req, ok := pendingReqs[e.RequestID]
		if !ok {
			mu.Unlock()
			return
		}
		req.Status = e.Response.Status
		delete(pendingReqs, e.RequestID)
		mu.Unlock()

		// Buscar body da resposta
		getBody := proto.NetworkGetResponseBody{RequestID: e.RequestID}
		bodyResp, err := getBody.Call(page)
		body := ""
		if err == nil && bodyResp != nil {
			body = bodyResp.Body
			if len(body) > 8192 {
				body = body[:8192] + "...[truncado]"
			}
		}
		req.Body = body

		// Extrair X-Skypetoken do body de authsvc
		mu.Lock()
		if result.SkypeToken == "" && isAuthURL(req.URL) && body != "" {
			var authResp map[string]interface{}
			if json.Unmarshal([]byte(body), &authResp) == nil {
				if tokens, ok := authResp["tokens"].(map[string]interface{}); ok {
					if st, ok := tokens["skypeToken"].(string); ok && st != "" {
						result.SkypeToken = st
						logger.Info().Str("prefix", st[:min(20, len(st))]+"...").Msg("[Teams] ✅ X-Skypetoken capturado!")
					}
				}
				if st, ok := authResp["skypeToken"].(string); ok && st != "" {
					result.SkypeToken = st
					logger.Info().Msg("[Teams] ✅ X-Skypetoken capturado (formato alt)!")
				}
			}
		}
		mu.Unlock()

		mu.Lock()
		switch {
		case isAuthURL(req.URL):
			result.AuthRequests = append(result.AuthRequests, req)
		case isChatURL(req.URL):
			result.ChatRequests = append(result.ChatRequests, req)
			logger.Info().Str("url", req.URL).Int("status", req.Status).Msg("[Teams] 💬 Chat API capturada!")
		default:
			result.OtherAPIs = append(result.OtherAPIs, req)
		}
		total := len(result.AuthRequests) + len(result.ChatRequests) + len(result.OtherAPIs)
		mu.Unlock()

		logger.Debug().
			Str("url", req.URL).
			Int("status", req.Status).
			Int("total", total).
			Msg("[Teams] req capturada")
	})()

	// ── Capturar WebSockets ───────────────────────────────────────────────
	go page.EachEvent(func(e *proto.NetworkWebSocketCreated) {
		if isTeamsRelevant(e.URL) {
			logger.Info().Str("url", e.URL).Msg("[Teams] 🔌 WebSocket criado")
		}
	})()

	go page.EachEvent(func(e *proto.NetworkWebSocketFrameReceived) {
		payload := e.Response.PayloadData
		if len(payload) > 2048 {
			payload = payload[:2048] + "...[truncado]"
		}
		if strings.Contains(payload, "message") || strings.Contains(payload, "conversation") {
			mu.Lock()
			result.WebSockets = append(result.WebSockets, CapturedWS{
				Direction:  "recv",
				Payload:    payload,
				CapturedAt: time.Now(),
			})
			mu.Unlock()
			logger.Info().Int("len", len(payload)).Msg("[Teams] 🔌 WS frame com mensagem capturado")
		}
	})()

	// ── Navegar para Teams ────────────────────────────────────────────────
	logger.Info().Msg("[Teams] Navegando para teams.microsoft.com...")
	if err := page.Navigate("https://teams.microsoft.com/_#/"); err != nil {
		return nil, fmt.Errorf("erro ao navegar: %v", err)
	}
	page.MustEval(`() => {
		Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
	}`)

	logger.Info().Msgf("[Teams] Aguardando (até %v) — navegue até o chat do MR.ViaBot...", timeout)
	logger.Info().Msg("[Teams] ═══════════════════════════════════════════════")
	logger.Info().Msg("[Teams]  Abra o chat do MR.ViaBot e role as mensagens")
	logger.Info().Msg("[Teams] ═══════════════════════════════════════════════")

	// Aguardar com log de progresso a cada 15s
	deadline := time.Now().Add(timeout)
	lastTotal := 0
	for time.Now().Before(deadline) {
		time.Sleep(15 * time.Second)
		mu.Lock()
		total := len(result.AuthRequests) + len(result.ChatRequests) + len(result.OtherAPIs)
		ws := len(result.WebSockets)
		token := result.SkypeToken != ""
		mu.Unlock()

		if total != lastTotal || ws > 0 {
			logger.Info().
				Int("auth", len(result.AuthRequests)).
				Int("chat", len(result.ChatRequests)).
				Int("ws_frames", ws).
				Bool("skype_token", token).
				Str("restante", time.Until(deadline).Round(time.Second).String()).
				Msg("[Teams] Progresso")
			lastTotal = total
		}
	}

	// Tentar extrair token do localStorage se não encontrado via CDP
	if result.SkypeToken == "" {
		logger.Info().Msg("[Teams] Tentando localStorage para X-Skypetoken...")
		val, err := page.Eval(`() => {
			const stores = [localStorage, sessionStorage];
			for (const store of stores) {
				for (let key of Object.keys(store)) {
					try {
						const val = JSON.parse(store[key]);
						if (val && val.secret) return val.secret;
						if (val && val.skypeToken) return val.skypeToken;
						if (val && val.tokens && val.tokens.skypeToken) return val.tokens.skypeToken;
					} catch {}
				}
			}
			return null;
		}`)
		if err == nil && !val.Value.Nil() && val.Value.String() != "" {
			mu.Lock()
			result.SkypeToken = val.Value.String()
			mu.Unlock()
			logger.Info().Msg("[Teams] ✅ X-Skypetoken extraído do localStorage!")
		}
	}

	// Salvar resultados
	timestamp := time.Now().Format("2006-01-02-150405")
	saveJSON(filepath.Join(outputDir, "auth-requests.json"), result.AuthRequests, logger)
	saveJSON(filepath.Join(outputDir, "chat-requests.json"), result.ChatRequests, logger)
	saveJSON(filepath.Join(outputDir, "websocket-frames.json"), result.WebSockets, logger)
	saveJSON(filepath.Join(outputDir, "other-apis.json"), result.OtherAPIs, logger)

	fullPath := filepath.Join(outputDir, fmt.Sprintf("discovery-%s.json", timestamp))
	data, _ := json.MarshalIndent(result, "", "  ")
	os.WriteFile(fullPath, data, 0600) //nolint:errcheck

	printSummary(result, outputDir, logger)
	return result, nil
}

func saveJSON(path string, v interface{}, logger *zerolog.Logger) {
	data, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(path, data, 0600); err != nil {
		logger.Error().Err(err).Str("file", path).Msg("[Teams] Erro ao salvar")
		return
	}
	logger.Info().Str("file", path).Msg("[Teams] Salvo")
}

func printSummary(result *DiscoveryResult, outputDir string, logger *zerolog.Logger) {
	logger.Info().Msg("[Teams] ════════════════ RESUMO ════════════════")
	logger.Info().Msgf("[Teams] Auth requests : %d", len(result.AuthRequests))
	logger.Info().Msgf("[Teams] Chat requests : %d", len(result.ChatRequests))
	logger.Info().Msgf("[Teams] WS frames     : %d", len(result.WebSockets))
	if result.SkypeToken != "" {
		logger.Info().Msgf("[Teams] SkypeToken    : %s...", result.SkypeToken[:min(30, len(result.SkypeToken))])
	} else {
		logger.Warn().Msg("[Teams] SkypeToken    : NÃO ENCONTRADO — inspecione auth-requests.json")
	}
	logger.Info().Msg("[Teams] ════════════════ PRÓXIMOS PASSOS ══════")
	logger.Info().Msgf("[Teams] 1. Abrir %s/auth-requests.json", outputDir)
	logger.Info().Msg("[Teams]    → procurar campo skypeToken ou tokens.skypeToken")
	logger.Info().Msgf("[Teams] 2. Abrir %s/chat-requests.json", outputDir)
	logger.Info().Msg("[Teams]    → identificar URL de listagem de conversas")
	logger.Info().Msg("[Teams]    → identificar threadId do MR.ViaBot")
	logger.Info().Msgf("[Teams] 3. Abrir %s/websocket-frames.json", outputDir)
	logger.Info().Msg("[Teams]    → inspecionar formato das mensagens em tempo real")
	logger.Info().Msg("[Teams] ═════════════════════════════════════════")
}

// findSystemChrome localiza o Chrome/Chromium instalado no sistema.
func findSystemChrome() string {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
