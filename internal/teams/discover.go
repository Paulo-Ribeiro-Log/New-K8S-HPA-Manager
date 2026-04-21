package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// findSystemChrome localiza o Chrome/Chromium instalado no sistema operacional.
// Prefere versões mais novas para compatibilidade com Teams.
func findSystemChrome() string {
	candidates := []string{
		"/usr/bin/google-chrome-stable",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/snap/bin/chromium",
		// macOS
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fallback: buscar no PATH
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium-browser", "chromium"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// CapturedRequest representa uma requisição/resposta capturada do Teams.
type CapturedRequest struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	ReqHeaders map[string]string `json:"req_headers,omitempty"`
	Status     int               `json:"status"`
	Body       string            `json:"body,omitempty"` // JSON truncado a 8KB
	CapturedAt time.Time         `json:"captured_at"`
}

// DiscoveryResult é o resultado completo da sessão de descoberta.
type DiscoveryResult struct {
	CapturedAt   time.Time          `json:"captured_at"`
	AuthRequests []CapturedRequest  `json:"auth_requests"`   // authsvc — contém X-Skypetoken
	ChatRequests []CapturedRequest  `json:"chat_requests"`   // chatsvcagg — conversas e mensagens
	OtherAPIs    []CapturedRequest  `json:"other_apis"`      // outras APIs internas do Teams
	SkypeToken   string             `json:"skype_token"`     // extraído automaticamente se encontrado
	Conversations []ConversationHint `json:"conversations"`  // lista de chats encontrados
}

// ConversationHint é uma conversa identificada na descoberta.
type ConversationHint struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	ThreadType  string `json:"thread_type"`
}

// domínios de interesse para interceptação
var teamsInterestDomains = []string{
	"authsvc.teams.microsoft.com",
	"chatsvcagg.teams.microsoft.com",
	"teams.microsoft.com/api",
	"teams.microsoft.com/v1",
	"ng.msg.teams.microsoft.com",
	"api.spaces.skype.com",
}

func isInteresting(url string) bool {
	for _, d := range teamsInterestDomains {
		if strings.Contains(url, d) {
			return true
		}
	}
	return false
}

func isAuthURL(url string) bool {
	return strings.Contains(url, "authsvc") || strings.Contains(url, "/authz")
}

func isChatURL(url string) bool {
	return strings.Contains(url, "chatsvcagg") ||
		strings.Contains(url, "/conversations") ||
		strings.Contains(url, "/messages") ||
		strings.Contains(url, "api.spaces.skype.com")
}

// RunDiscovery lança o Chromium com a sessão existente, navega para o Teams,
// intercepta todas as chamadas de rede relevantes e salva em outputDir.
func RunDiscovery(sessionDir, outputDir string, logger *zerolog.Logger, timeout time.Duration) (*DiscoveryResult, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de saída: %v", err)
	}

	logger.Info().
		Str("session_dir", sessionDir).
		Str("output_dir", outputDir).
		Dur("timeout", timeout).
		Msg("[Teams] Iniciando descoberta de APIs do Teams...")

	// Preferir Chrome do sistema (mais atualizado) em vez do Chromium do Rod.
	// Teams exige versões recentes e rejeita browsers muito antigos.
	chromeBin := findSystemChrome()
	if chromeBin != "" {
		logger.Info().Str("bin", chromeBin).Msg("[Teams] Usando Chrome do sistema")
	} else {
		logger.Warn().Msg("[Teams] Chrome do sistema não encontrado — usando Chromium do Rod (pode falhar no Teams)")
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
		return nil, fmt.Errorf("erro ao iniciar Chromium: %v", err)
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
	var captured []CapturedRequest

	// Interceptar todas as requisições relevantes
	router := browser.HijackRequests()
	router.MustAdd("*teams.microsoft.com*", func(ctx *rod.Hijack) {
		url := ctx.Request.URL().String()
		if !isInteresting(url) {
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
			return
		}

		// Copiar headers da requisição
		reqHeaders := map[string]string{}
		for k, v := range ctx.Request.Headers() {
			lk := strings.ToLower(k)
			if lk == "authorization" || lk == "x-skypetoken" || lk == "client-request-id" {
				reqHeaders[k] = fmt.Sprintf("%v", v)
			}
		}

		ctx.MustLoadResponse()

		body := ctx.Response.Body()
		if len(body) > 8192 {
			body = body[:8192] + "...[truncado]"
		}

		cap := CapturedRequest{
			URL:        url,
			Method:     ctx.Request.Method(),
			ReqHeaders: reqHeaders,
			Status:     ctx.Response.Payload().ResponseCode,
			Body:       body,
			CapturedAt: time.Now(),
		}
		captured = append(captured, cap)

		// Extrair X-Skypetoken do body de authsvc
		if isAuthURL(url) && result.SkypeToken == "" {
			var authResp map[string]interface{}
			if json.Unmarshal([]byte(body), &authResp) == nil {
				if tokens, ok := authResp["tokens"].(map[string]interface{}); ok {
					if st, ok := tokens["skypeToken"].(string); ok && st != "" {
						result.SkypeToken = st
						logger.Info().Str("token_prefix", st[:min(20, len(st))]+"...").Msg("[Teams] X-Skypetoken capturado!")
					}
				}
				// Formato alternativo: campo direto "skypeToken"
				if st, ok := authResp["skypeToken"].(string); ok && st != "" {
					result.SkypeToken = st
					logger.Info().Msg("[Teams] X-Skypetoken capturado (formato alternativo)!")
				}
			}
		}

		logger.Debug().
			Str("url", url).
			Int("status", cap.Status).
			Int("body_len", len(body)).
			Msg("[Teams] Requisição capturada")
	})
	go router.Run()

	// Também interceptar api.spaces.skype.com (outro domínio do Teams)
	router2 := browser.HijackRequests()
	router2.MustAdd("*api.spaces.skype.com*", func(ctx *rod.Hijack) {
		ctx.MustLoadResponse()
		body := ctx.Response.Body()
		if len(body) > 8192 {
			body = body[:8192] + "...[truncado]"
		}
		cap := CapturedRequest{
			URL:        ctx.Request.URL().String(),
			Method:     ctx.Request.Method(),
			Status:     ctx.Response.Payload().ResponseCode,
			Body:       body,
			CapturedAt: time.Now(),
		}
		captured = append(captured, cap)
	})
	go router2.Run()

	// Navegar para Teams — usar URL com parâmetro para evitar detecção
	logger.Info().Msg("[Teams] Navegando para teams.microsoft.com...")
	if err := page.Navigate("https://teams.microsoft.com/_#/"); err != nil {
		return nil, fmt.Errorf("erro ao navegar para Teams: %v", err)
	}
	// Remover flag de automação via JavaScript antes do Teams carregar
	page.MustEval(`() => {
		Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
	}`)

	// Aguardar Teams carregar completamente
	logger.Info().Msgf("[Teams] Aguardando Teams carregar (até %v)...", timeout)
	logger.Info().Msg("[Teams] =============================================")
	logger.Info().Msg("[Teams] Navegue até o chat do MR.ViaBot no Teams")
	logger.Info().Msg("[Teams] As requisições serão capturadas automaticamente")
	logger.Info().Msg("[Teams] =============================================")

	deadline := time.Now().Add(timeout)
	lastCount := 0
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		if len(captured) != lastCount {
			logger.Info().
				Int("total_capturadas", len(captured)).
				Str("tempo_restante", time.Until(deadline).Round(time.Second).String()).
				Msg("[Teams] Progresso...")
			lastCount = len(captured)
		}
	}

	// Tentar extrair X-Skypetoken do localStorage se não capturado via intercept
	if result.SkypeToken == "" {
		logger.Info().Msg("[Teams] Tentando extrair token do localStorage...")
		val, err := page.Eval(`() => {
			for (let key of Object.keys(localStorage)) {
				try {
					const val = JSON.parse(localStorage[key]);
					if (val && val.secret) return val.secret;
					if (val && val.skypeToken) return val.skypeToken;
				} catch {}
			}
			// Tentar sessionStorage também
			for (let key of Object.keys(sessionStorage)) {
				try {
					const val = JSON.parse(sessionStorage[key]);
					if (val && val.secret) return val.secret;
					if (val && val.skypeToken) return val.skypeToken;
				} catch {}
			}
			return null;
		}`)
		if err == nil && !val.Value.Nil() && val.Value.String() != "" {
			result.SkypeToken = val.Value.String()
			logger.Info().Msg("[Teams] X-Skypetoken extraído do localStorage!")
		}
	}

	// Tentar listar conversas via JavaScript (Teams pode expor via window.__teamsAppState ou similar)
	logger.Info().Msg("[Teams] Tentando extrair lista de conversas via JavaScript...")
	convResult, err := page.Eval(`() => {
		try {
			// Tentar acessar React fiber (Teams é React)
			const el = document.querySelector('[data-tid="chat-list"]') ||
			           document.querySelector('[data-app-section-id="chatList"]');
			if (!el) return null;
			const items = el.querySelectorAll('[data-tid="chat-list-item"], [role="listitem"]');
			const convs = [];
			items.forEach(item => {
				const nameEl = item.querySelector('[data-tid="chat-item-title"], .css-175oi2r span');
				if (nameEl) convs.push({ name: nameEl.textContent.trim() });
			});
			return JSON.stringify(convs);
		} catch(e) { return null; }
	}`)
	if err == nil && !convResult.Value.Nil() && convResult.Value.String() != "" {
		var convs []map[string]string
		if json.Unmarshal([]byte(convResult.Value.String()), &convs) == nil {
			for _, c := range convs {
				result.Conversations = append(result.Conversations, ConversationHint{
					DisplayName: c["name"],
				})
			}
			logger.Info().Int("count", len(result.Conversations)).Msg("[Teams] Conversas encontradas via DOM")
		}
	}

	// Classificar as requisições capturadas
	for _, cap := range captured {
		if isAuthURL(cap.URL) {
			result.AuthRequests = append(result.AuthRequests, cap)
		} else if isChatURL(cap.URL) {
			result.ChatRequests = append(result.ChatRequests, cap)
		} else {
			result.OtherAPIs = append(result.OtherAPIs, cap)
		}
	}

	// Salvar resultado completo
	resultPath := filepath.Join(outputDir, fmt.Sprintf("discovery-%s.json", time.Now().Format("2006-01-02-150405")))
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(resultPath, data, 0600); err != nil {
		logger.Error().Err(err).Msg("[Teams] Erro ao salvar resultado")
	} else {
		logger.Info().Str("file", resultPath).Msg("[Teams] Resultado salvo")
	}

	// Salvar arquivos separados por categoria para facilitar análise
	saveCategory(filepath.Join(outputDir, "auth-requests.json"), result.AuthRequests, logger)
	saveCategory(filepath.Join(outputDir, "chat-requests.json"), result.ChatRequests, logger)
	saveCategory(filepath.Join(outputDir, "other-apis.json"), result.OtherAPIs, logger)

	// Relatório no terminal
	printSummary(result, resultPath, logger)

	return result, nil
}

func saveCategory(path string, items []CapturedRequest, logger *zerolog.Logger) {
	if len(items) == 0 {
		return
	}
	data, _ := json.MarshalIndent(items, "", "  ")
	if err := os.WriteFile(path, data, 0600); err != nil {
		logger.Error().Err(err).Str("file", path).Msg("[Teams] Erro ao salvar categoria")
	}
}

func printSummary(result *DiscoveryResult, resultPath string, logger *zerolog.Logger) {
	logger.Info().Msg("[Teams] ============ RESUMO DA DESCOBERTA ============")
	logger.Info().Msgf("[Teams] Auth requests capturadas : %d", len(result.AuthRequests))
	logger.Info().Msgf("[Teams] Chat requests capturadas : %d", len(result.ChatRequests))
	logger.Info().Msgf("[Teams] Outras APIs capturadas   : %d", len(result.OtherAPIs))

	if result.SkypeToken != "" {
		logger.Info().Msgf("[Teams] X-Skypetoken             : %s...", result.SkypeToken[:min(30, len(result.SkypeToken))])
	} else {
		logger.Warn().Msg("[Teams] X-Skypetoken             : NÃO ENCONTRADO")
		logger.Warn().Msg("[Teams]   → Tente navegar para o chat do MR.ViaBot durante a sessão")
	}

	if len(result.Conversations) > 0 {
		logger.Info().Msg("[Teams] Conversas encontradas:")
		for _, c := range result.Conversations {
			logger.Info().Msgf("[Teams]   - %s (id: %s)", c.DisplayName, c.ID)
		}
	}

	logger.Info().Msgf("[Teams] Arquivo completo: %s", resultPath)
	logger.Info().Msg("[Teams] =================================================")
	logger.Info().Msg("[Teams] Próximos passos:")
	logger.Info().Msg("[Teams]   1. Inspecionar auth-requests.json → encontrar campo com X-Skypetoken")
	logger.Info().Msg("[Teams]   2. Inspecionar chat-requests.json → identificar URL de listagem de chats")
	logger.Info().Msg("[Teams]   3. Localizar threadId do MR.ViaBot na lista de conversas")
	logger.Info().Msg("[Teams]   4. Atualizar TEAMS-EXTRACTION-PLAN.md com URLs reais encontradas")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
