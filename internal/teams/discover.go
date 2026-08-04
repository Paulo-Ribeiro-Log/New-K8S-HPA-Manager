package teams

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
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
	OnChatRequest func(url string)   `json:"-"` // callback quando Chat API é capturada
}

// ConversationHint é uma conversa identificada na descoberta.
type ConversationHint struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	ThreadType  string `json:"thread_type"`
}

// isTeamsURL verifica se a URL pertence ao Teams — direto ou via proxy MCAS.
func isTeamsURL(url string) bool {
	return strings.Contains(url, "teams.microsoft.com")
}

// isTeamsV2URL verifica especificamente a interface v2 do Teams (incluindo MCAS).
func isTeamsV2URL(url string) bool {
	return strings.Contains(url, "teams.microsoft.com") && strings.Contains(url, "/v2")
}

func isAuthURL(url string) bool {
	return strings.Contains(url, "authsvc") ||
		strings.Contains(url, "/authz") ||
		strings.Contains(url, "microsoftonline.com") ||
		strings.Contains(url, "aad_login") ||
		strings.Contains(url, "login.microsoftonline")
}

func isChatURL(url string) bool {
	return strings.Contains(url, "chatsvcagg") ||
		strings.Contains(url, "/conversations") ||
		strings.Contains(url, "/messages") ||
		strings.Contains(url, "api.spaces.skype") ||
		strings.Contains(url, "ng.msg.teams") ||
		// MCAS proxy: chat via API interna do Teams (teams.microsoft.com.mcas.ms/api/mt/...)
		strings.Contains(url, "/api/mt/") ||
		strings.Contains(url, "/v1/users/ME/conversations") ||
		strings.Contains(url, "/v1/threads") ||
		// async gateway usado para buscar objetos de mensagens
		strings.Contains(url, "asyncgw.teams.microsoft.com")
}

func isTeamsRelevant(url string) bool {
	keywords := []string{
		"teams.microsoft.com",
		"api.spaces.skype.com",
		"chatsvcagg",
		"authsvc",
		"ng.msg.teams",
		"microsoftonline.com",
		// MCAS proxy — empresa redireciona Teams via Microsoft Cloud App Security
		"mcas.ms",
		"access.mcas.ms",
		"trouter",
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
// killExistingChrome encerra processos Chrome/Chromium que usam o sessionDir como perfil.
// Necessário porque o Rod não consegue lançar uma instância de debug se o perfil já está bloqueado.
func killExistingChrome(sessionDir string, logger *zerolog.Logger) {
	// Listar PIDs de Chrome/Chromium usando o perfil
	out, err := exec.Command("pgrep", "-f", sessionDir).Output()
	if err != nil || len(out) == 0 {
		return
	}
	pids := strings.Fields(string(out))
	logger.Info().Strs("pids", pids).Msg("[Teams] Encerrando instâncias Chrome existentes com o perfil rod-session")
	for _, pid := range pids {
		exec.Command("kill", "-TERM", pid).Run() //nolint:errcheck
	}
	time.Sleep(500 * time.Millisecond)
	// SIGKILL se ainda houver processos
	out2, _ := exec.Command("pgrep", "-f", sessionDir).Output()
	for _, pid := range strings.Fields(string(out2)) {
		exec.Command("kill", "-9", pid).Run() //nolint:errcheck
	}
}

func RunDiscovery(sessionDir, outputDir string, logger *zerolog.Logger, timeout time.Duration) (*DiscoveryResult, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de saída: %v", err)
	}

	// Serializa contra ScanConversations/SendBatch — abas paralelas no mesmo perfil podem
	// invalidar o SkypeToken (ver comentário de operationMu em browser_manager.go).
	operationMu.Lock()
	defer operationMu.Unlock()

	browser, err := getBrowser(sessionDir, logger)
	if err != nil {
		return nil, err
	}

	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	// createdPage: `page` é reatribuída mais abaixo (MCAS pode abrir o Teams v2 em outra aba),
	// mas a aba criada aqui precisa ser fechada de qualquer forma ao final.
	createdPage := page
	defer createdPage.Close() //nolint:errcheck

	result := &DiscoveryResult{CapturedAt: time.Now()}
	var mu sync.Mutex

	// attachListeners habilita CDP Network e registra handlers em uma page.
	// Chamado para cada aba que abre o Teams (v2 pode abrir em nova aba).
	attachListeners := func(p *rod.Page, label string) {
		pendingReqs := map[proto.NetworkRequestID]CapturedRequest{}
		var pendingMu sync.Mutex

		netEnable := proto.NetworkEnable{}
		if err := netEnable.Call(p); err != nil {
			logger.Warn().Err(err).Str("page", label).Msg("[Teams] Falha ao habilitar Network CDP")
			return
		}
		logger.Info().Str("page", label).Msg("[Teams] CDP Network habilitado")

		go p.EachEvent(func(e *proto.NetworkRequestWillBeSent) {
			if !isTeamsRelevant(e.Request.URL) {
				return
			}
			reqHeaders := map[string]string{}
			for k, v := range e.Request.Headers {
				lk := strings.ToLower(k)
				if lk == "authorization" || lk == "x-skypetoken" || lk == "client-request-id" {
					reqHeaders[k] = fmt.Sprintf("%v", v)
				}
			}
			// Extrair SkypeToken diretamente do header da requisição.
			// No ambiente MCAS o token não aparece no body da resposta — é
			// enviado como "X-Skypetoken" ou "authorization: skype_token ..." nos requests.
			mu.Lock()
			if result.SkypeToken == "" {
				for k, v := range reqHeaders {
					lk := strings.ToLower(k)
					val := fmt.Sprintf("%v", v)
					if lk == "x-skypetoken" && val != "" {
						result.SkypeToken = val
						logger.Info().Str("prefix", val[:min(20, len(val))]).Msg("[Teams] SkypeToken capturado (req header X-Skypetoken)!")
					} else if lk == "authorization" && strings.HasPrefix(val, "skype_token ") {
						result.SkypeToken = strings.TrimPrefix(val, "skype_token ")
						logger.Info().Msg("[Teams] SkypeToken capturado (req header authorization: skype_token)!")
					}
				}
			}
			mu.Unlock()
			pendingMu.Lock()
			pendingReqs[e.RequestID] = CapturedRequest{
				URL:        e.Request.URL,
				Method:     e.Request.Method,
				ReqHeaders: reqHeaders,
				CapturedAt: time.Now(),
			}
			pendingMu.Unlock()
		})()

		go p.EachEvent(func(e *proto.NetworkResponseReceived) {
			pendingMu.Lock()
			req, ok := pendingReqs[e.RequestID]
			if !ok {
				pendingMu.Unlock()
				return
			}
			req.Status = e.Response.Status
			delete(pendingReqs, e.RequestID)
			pendingMu.Unlock()

			// Body só é capturado para requisições de auth e chat — OtherAPIs não precisam
			// de body e representariam centenas de assets (JS/CSS/imagens) acumulando em RAM/disco.
			isAuth := isAuthURL(req.URL)
			isChat := isChatURL(req.URL)
			if isAuth || isChat {
				getBody := proto.NetworkGetResponseBody{RequestID: e.RequestID}
				bodyResp, err := getBody.Call(p)
				if err == nil && bodyResp != nil {
					body := bodyResp.Body
					if len(body) > 8192 {
						body = body[:8192] + "...[truncado]"
					}
					req.Body = body
				}
			}

			mu.Lock()
			// Fallback: tentar extrair SkypeToken do body da resposta de auth
			if result.SkypeToken == "" && isAuth && req.Body != "" {
				var authResp map[string]interface{}
				if json.Unmarshal([]byte(req.Body), &authResp) == nil {
					if tokens, ok := authResp["tokens"].(map[string]interface{}); ok {
						if st, ok2 := tokens["skypeToken"].(string); ok2 && st != "" {
							result.SkypeToken = st
							logger.Info().Str("prefix", st[:min(20, len(st))]).Msg("[Teams] SkypeToken capturado (body)!")
						}
					}
					if st, ok2 := authResp["skypeToken"].(string); ok2 && st != "" {
						result.SkypeToken = st
						logger.Info().Msg("[Teams] SkypeToken capturado (body alt)!")
					}
				}
			}
			switch {
			case isAuth:
				result.AuthRequests = append(result.AuthRequests, req)
			case isChat:
				result.ChatRequests = append(result.ChatRequests, req)
				logger.Info().Str("url", req.URL).Int("status", req.Status).Msg("[Teams] Chat API capturada!")
				if cb := result.OnChatRequest; cb != nil {
					go cb(req.URL)
				}
			default:
				// OtherAPIs: guardar apenas URL+status (sem body) e limitar a 100 entradas
				if len(result.OtherAPIs) < 100 {
					result.OtherAPIs = append(result.OtherAPIs, req)
				}
			}
			mu.Unlock()
		})()

		go p.EachEvent(func(e *proto.NetworkWebSocketCreated) {
			if isTeamsRelevant(e.URL) {
				logger.Info().Str("url", e.URL).Msg("[Teams] WebSocket criado")
			}
		})()

		go p.EachEvent(func(e *proto.NetworkWebSocketFrameReceived) {
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
				logger.Info().Int("len", len(payload)).Msg("[Teams] WS frame capturado")
			}
		})()
	}

	// ── Navegar para Teams v2 — suporta MCAS proxy e acesso direto ──────
	// MCAS (Microsoft Cloud App Security) redireciona teams.microsoft.com →
	// teams.microsoft.com.mcas.ms. Navegar para a URL direta e deixar o
	// redirect acontecer garante compatibilidade com ambos os cenários.
	logger.Info().Msg("[Teams] Navegando para teams.microsoft.com/v2/ ...")
	if err := page.Navigate("https://teams.microsoft.com/v2/"); err != nil {
		return nil, fmt.Errorf("erro ao navegar: %v", err)
	}
	attachListeners(page, "main")

	// Monitorar novas abas — Teams v2 pode abrir em aba separada (MCAS inclusive)
	// Marcar a aba principal como já processada para evitar listeners duplicados.
	seenPages := map[string]bool{}
	if mainInfo, err := page.Info(); err == nil && mainInfo != nil {
		seenPages[string(mainInfo.TargetID)] = true
	}
	// tabMonitorDone limita esse goroutine à duração desta chamada. Antes o browser.Close() no
	// final da função interrompia o loop indiretamente (browser.Pages() passava a retornar erro);
	// com o browser persistente entre chamadas, sem isso o goroutine rodaria para sempre a cada
	// nova invocação de RunDiscovery, acumulando N loops zumbis.
	tabMonitorDone := make(chan struct{})
	defer close(tabMonitorDone)
	go func() {
		for {
			select {
			case <-tabMonitorDone:
				return
			case <-time.After(3 * time.Second):
			}
			pages, err := browser.Pages()
			if err != nil {
				return
			}
			for _, p := range pages {
				info, err := p.Info()
				if err != nil || seenPages[string(info.TargetID)] {
					continue
				}
				if isTeamsURL(info.URL) {
					seenPages[string(info.TargetID)] = true
					logger.Info().Str("url", info.URL).Msg("[Teams] Nova aba detectada — anexando listeners")
					attachListeners(p, info.URL)
				}
			}
		}
	}()

	// Aguardar Teams v2 carregar (pode levar até 5 minutos em WSL/máquinas lentas)
	logger.Info().Msg("[Teams] Aguardando Teams v2 carregar (pode levar ~5min em WSL)...")
	loadDeadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(loadDeadline) {
		info, _ := page.Info()
		if info != nil && isTeamsURL(info.URL) &&
			!strings.Contains(info.URL, "error") && !strings.Contains(info.URL, "eoa") {
			logger.Info().Str("url", info.URL).Msg("[Teams] Teams v2 carregado!")
			break
		}
		// Verificar se Teams abriu em outra aba (inclui URL MCAS)
		pages, _ := browser.Pages()
		for _, p := range pages {
			pInfo, err := p.Info()
			if err == nil && isTeamsV2URL(pInfo.URL) {
				page = p // usar esta aba como referência
				logger.Info().Str("url", pInfo.URL).Msg("[Teams] Teams v2 encontrado em aba separada!")
				goto teamsLoaded
			}
		}
		time.Sleep(5 * time.Second)
	}
teamsLoaded:

	// ThreadId do Mr.ViaBot (descoberto na Fase 0)
	const mrViaBotThreadID = "19:eab1be93-5589-4a3f-9f47-d6cfcbc50a0c_61740f97-9be2-4459-b054-5230364585a7@unq.gbl.spaces"

	// Aguardar SkypeToken ser capturado. Máximo 90s.
	//
	// Testado ao vivo removendo essa espera (achando que era só usada no fetch() diagnóstico
	// pro chatsvcagg, que o MCAS sempre bloqueia): quebrou a extração de verdade (0 mensagens
	// no DOM). Na prática o SkypeToken funciona como sinal indireto de "o Teams terminou de
	// sincronizar dados o suficiente pra aceitar interação" — sem esperar por ele, o
	// hash-nav/click/scroll rodam cedo demais, antes da conversa estar pronta. Mantido.
	for i := 0; i < 18; i++ {
		time.Sleep(5 * time.Second)
		mu.Lock()
		captured := result.SkypeToken != ""
		mu.Unlock()
		if captured {
			logger.Info().Msg("[Teams] SkypeToken capturado — navegando ao chat do Mr.ViaBot")
			break
		}
	}

	// Navegar ao chat do Mr.ViaBot via JS (altera hash SPA sem criar nova aba).
	// page.Navigate() para outro path causa nova aba e cancela o contexto atual.
	navJS := fmt.Sprintf(`() => {
		window.location.hash = '#/conversations/%s?ctx=chat';
		return window.location.href;
	}`, mrViaBotThreadID)
	navRes, navErr := page.Eval(navJS)
	if navErr == nil {
		logger.Info().Str("href", navRes.Value.String()).Msg("[Teams] Navegação SPA via hash disparada")
	} else {
		logger.Warn().Err(navErr).Msg("[Teams] Falha ao navegar via hash")
	}
	// Testado ao vivo: esperar por [data-tid="messageBody"] aqui não adianta — o Teams só
	// renderiza esses elementos DEPOIS do scroll (lista virtualizada, ver mais abaixo), então
	// um polling por esse seletor sempre bate no teto do timeout, sem ganhar nada sobre um
	// sleep fixo. 3s é suficiente pra rota SPA via hash processar.
	time.Sleep(3 * time.Second)

	// Se o hash não abriu a conversa, tentar clicar no item da lista de chats
	clickJS := `() => {
		const keywords = ['viabot', 'mr.viabot', 'mr viabot'];
		const selectors = [
			'[data-tid="chat-list-item"]',
			'[class*="listItem"]',
			'[class*="chatListItem"]',
			'[role="listitem"]',
			'[class*="conversationItem"]',
			'[class*="chat-item"]'
		];
		for (const sel of selectors) {
			for (const item of document.querySelectorAll(sel)) {
				const text = (item.textContent || '').toLowerCase();
				if (keywords.some(k => text.includes(k))) {
					item.click();
					return { clicked: true, selector: sel, text: item.textContent.substring(0, 80) };
				}
			}
		}
		for (const el of document.querySelectorAll('[aria-label]')) {
			const label = (el.getAttribute('aria-label') || '').toLowerCase();
			if (keywords.some(k => label.includes(k))) {
				el.click();
				return { clicked: true, label: el.getAttribute('aria-label') };
			}
		}
		return { clicked: false };
	}`
	clickRes, clickErr := page.Eval(clickJS)
	clicked := false
	if clickErr == nil && !clickRes.Value.Nil() {
		logger.Info().Str("result", clickRes.Value.String()).Msg("[Teams] Tentativa de click na conversa Mr.ViaBot")
		clicked = clickRes.Value.Get("clicked").Bool()
	}
	// Só vale esperar aqui se o click realmente aconteceu — se nada foi clicado, a navegação
	// via hash acima provavelmente já resolveu (ou nada vai mudar esperando às cegas). Mesmo
	// raciocínio do sleep acima: o conteúdo só aparece depois do scroll, então 3s (só pra
	// deixar o click processar) é tão eficaz quanto os 10s fixos anteriores.
	if clicked {
		time.Sleep(3 * time.Second)
	}

	// Rolar para o topo da conversa para forçar carregamento lazy de mensagens antigas.
	// O Teams só renderiza mensagens próximas ao viewport — sem scroll, CHGs de horas
	// atrás ficam fora do DOM e não são capturadas. Três rodadas com pausa de 5s cada.
	scrollJS := `() => {
		const selectors = [
			'[data-tid="messageList"]',
			'[class*="messageListContainer"]',
			'[class*="scrollContainer"]',
			'[class*="chatContent"]',
			'[class*="message-list"]',
			'[role="log"]',
			'[role="list"]',
		];
		for (const sel of selectors) {
			const el = document.querySelector(sel);
			if (el && el.scrollHeight > el.clientHeight) {
				el.scrollTop = 0;
				return { scrolled: true, selector: sel, scrollHeight: el.scrollHeight };
			}
		}
		window.scrollTo(0, 0);
		return { scrolled: false };
	}`
	for i := 0; i < 3; i++ {
		scrollRes, scrollErr := page.Eval(scrollJS)
		if scrollErr == nil && !scrollRes.Value.Nil() {
			logger.Info().Str("result", scrollRes.Value.String()).Msgf("[Teams] Scroll %d/3 para carregar mensagens antigas", i+1)
		} else if scrollErr != nil {
			logger.Warn().Err(scrollErr).Msgf("[Teams] Erro no scroll %d/3", i+1)
		}
		time.Sleep(5 * time.Second)
	}

	// Extrair mensagens diretamente do DOM (não depende de HTTP — MCAS bloqueia fetch() externo)
	domMsgJS := `() => {
		const selectors = [
			'[data-tid="messageBody"]',
			'[class*="messageBodyContent"]',
			'[class*="message-body"]',
			'[class*="messageBody"]',
			'[class*="bubble-wrapper"] [class*="content"]',
			'[class*="itemContent"]'
		];
		// Inclui os href de <a> dentro do container — necessário quando o Teams
		// renderiza o link devstartcd como hyperlink e não como texto visível.
		// Em ambientes corporativos os links são embalados em Safe Links (Defender) ou
		// MCAS proxy — decodificar aqui para que o regex do parser encontre a URL real.
		const collectHrefs = (el) => {
			const hrefs = [];
			el.querySelectorAll('a[href]').forEach(a => {
				let h = (a.href || a.getAttribute('href') || '').trim();
				if (!h || h.startsWith('javascript') || h.startsWith('#')) return;
				// Safe Links: https://*.safelinks.protection.outlook.com/?url=<encoded>
				if (h.includes('safelinks.protection.outlook.com')) {
					try { const orig = new URL(h).searchParams.get('url'); if (orig) h = decodeURIComponent(orig); } catch {}
				}
				// Teams link proxy: https://teams.microsoft.com/l/link?url=<encoded>
				if (h.includes('/l/link') && h.includes('url=')) {
					try { const orig = new URL(h).searchParams.get('url'); if (orig) h = decodeURIComponent(orig); } catch {}
				}
				// MCAS proxy: devstartcd.via.com.br.mcas.ms → devstartcd.via.com.br
				if (h.includes('.mcas.ms')) {
					h = h.replace(/(devstartcd\.via\.com\.br)\.mcas\.ms/g, '$1');
				}
				hrefs.push(h);
			});
			return hrefs.join('\n');
		};
		// Teams v2 renderiza a hora de cada mensagem num <time datetime="ISO8601">, geralmente
		// como irmão/tio do container de texto (cabeçalho da mensagem). Sobe até 10 ancestrais
		// procurando esse elemento — mesma estratégia de robustez do fallback de CHG abaixo,
		// já que a profundidade exata varia com o layout (mensagem própria vs. de terceiros).
		const findTimestamp = (el) => {
			let node = el;
			for (let d = 0; d < 10 && node; d++) {
				const t = node.querySelector ? node.querySelector('time[datetime]') : null;
				if (t) { const dt = t.getAttribute('datetime'); if (dt) return dt; }
				node = node.parentElement;
			}
			return '';
		};
		const messages = [];
		for (const sel of selectors) {
			const els = document.querySelectorAll(sel);
			if (els.length > 0) {
				els.forEach(el => {
					const text = (el.innerText || el.textContent || '').trim();
					const hrefs = collectHrefs(el);
					const combined = hrefs ? text + '\n' + hrefs : text;
					if (combined.length > 5) messages.push({ text: combined, postedAt: findTimestamp(el) });
				});
				if (messages.length > 0) break;
			}
		}
		// Fallback: encontrar <a> com CHGxxxxx e subir na DOM até o container
		// que também contenha a URL sre-approval (captura "Nome e versão" junto)
		if (messages.length === 0) {
			const chgRe = /CHG\d{5,}/i;
			const added = new Set();
			for (const el of document.querySelectorAll('*')) {
				if (el.children.length > 0) continue; // só leaf nodes
				const t = (el.innerText || el.textContent || '').trim();
				if (!chgRe.test(t) || t.length > 40) continue; // leaf com número CHG
				// Subir até achar container com sre-approval (texto ou href)
				let ancestor = el.parentElement;
				for (let d = 0; d < 15 && ancestor; d++) {
					const at = (ancestor.innerText || '').trim();
					const ahrefs = collectHrefs(ancestor);
					const combined = ahrefs ? at + '\n' + ahrefs : at;
					if ((combined.includes('sre-approval') || combined.includes('devstartcd')) && combined.length < 3000) {
						if (!added.has(combined)) { added.add(combined); messages.push({ text: combined, postedAt: findTimestamp(ancestor) }); }
						break;
					}
					ancestor = ancestor.parentElement;
				}
			}
			// Remover substrings: manter apenas o maior container por mensagem
			const deduped = [];
			for (const m of messages) {
				const supIdx = deduped.findIndex(d => d.text.includes(m.text));
				const subIdx = deduped.findIndex(d => m.text.includes(d.text));
				if (supIdx >= 0) { /* já coberto pelo maior */ }
				else if (subIdx >= 0) { deduped[subIdx] = m; }
				else { deduped.push(m); }
			}
			messages.splice(0, messages.length, ...deduped);
		}
		return JSON.stringify({ url: window.location.href, count: messages.length, messages: messages });
	}`
	domResult, domErr := page.Eval(domMsgJS)
	if domErr == nil && !domResult.Value.Nil() {
		domStr := domResult.Value.String()
		msgPath := filepath.Join(outputDir, "viabot-dom-messages.json")
		os.WriteFile(msgPath, []byte(domStr), 0600) //nolint:errcheck
		logger.Info().Str("file", msgPath).Int("bytes", len(domStr)).Msg("[Teams] Mensagens DOM salvas")
		// Logar preview
		var domData map[string]interface{}
		if json.Unmarshal([]byte(domStr), &domData) == nil {
			count, _ := domData["count"].(float64)
			currentURL, _ := domData["url"].(string)
			logger.Info().Float64("count", count).Str("url", currentURL).Msg("[Teams] Resultado extração DOM")
			if msgs, ok := domData["messages"].([]interface{}); ok {
				for i, m := range msgs {
					if i >= 5 {
						break
					}
					obj, _ := m.(map[string]interface{})
					text, _ := obj["text"].(string)
					if len(text) > 300 {
						text = text[:300]
					}
					postedAt, _ := obj["postedAt"].(string)
					logger.Info().Int("msg_n", i+1).Str("text", text).Str("posted_at", postedAt).Msg("[Teams] Mensagem DOM")
				}
			}
		}
	} else if domErr != nil {
		logger.Warn().Err(domErr).Msg("[Teams] Falha na extração DOM")
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

	// ── Coletar URLs de conversas via performance API ─────────────────────
	// O Teams carrega conversas via cache/IndexedDB — não aparecem no CDP.
	// Usamos performance.getEntriesByType para ver todas as URLs que foram
	// chamadas pela página, incluindo as de conversas/mensagens.
	logger.Info().Msg("[Teams] Coletando URLs de conversas via performance API...")
	perfResult, perfErr := page.Eval(`() => {
		const entries = performance.getEntriesByType('resource');
		const convURLs = entries
			.map(e => e.name)
			.filter(u => u.includes('conversation') || u.includes('/messages') || u.includes('/threads') || u.includes('chatsvcagg'));
		return JSON.stringify(convURLs);
	}`)
	if perfErr == nil && !perfResult.Value.Nil() {
		perfPath := filepath.Join(outputDir, "performance-conv-urls.json")
		os.WriteFile(perfPath, []byte(perfResult.Value.String()), 0600) //nolint:errcheck
		logger.Info().Str("file", perfPath).Msg("[Teams] URLs de performance salvas")
	}

	// ── Tentar múltiplos endpoints de conversas via JS fetch ──────────────
	// O Teams usa o protocolo Skype (chatsvcagg) para conversas — não o /api/mt/.
	// No ambiente MCAS o host é prefixado com .mcas.ms.
	// startTime = sexta-feira passada (18/abr/2026) em ms para cobrir ausência de msgs hoje.
	// Injetar o SkypeToken no JS para autenticar o fetch no chatsvcagg.
	// O chatsvcagg requer "Authorization: skype_token <jwt>" — não usa cookies.
	mu.Lock()
	skypeToken := result.SkypeToken
	mu.Unlock()

	// MCAS bloqueia fetch() direto ao chatsvcagg mesmo com SkypeToken.
	// O Teams armazena todas as conversas no IndexedDB — lemos de lá sem HTTP.
	logger.Info().Msg("[Teams] Buscando Mr.ViaBot no IndexedDB do Teams...")
	// O store 'conversations' só tem metadata (sem conteúdo de mensagens).
	// Buscar topics nos metadados + varrer skypexspaces que tem mensagens reais.
	fetchJS := `async () => {
		const openDB = (name) => new Promise((resolve, reject) => {
			const r = indexedDB.open(name);
			r.onsuccess = e => resolve(e.target.result);
			r.onerror = () => reject(r.error);
		});
		const getAll = (db, storeName) => new Promise((resolve) => {
			try {
				const tx = db.transaction(storeName, 'readonly');
				const req = tx.objectStore(storeName).getAll();
				req.onsuccess = () => resolve(req.result || []);
				req.onerror = () => resolve([]);
			} catch(e) { resolve([]); }
		});

		const dbs = await indexedDB.databases().catch(() => []);
		const keywords = ['mr.viabot', 'viavarejo.service-now.com', 'devstartcd.via', 'chg0', 'sre-approval', 'sre approval'];
		const hasKw = (s) => keywords.some(k => s.includes(k)) || /chg\d{5,}/i.test(s);
		const results = { total_dbs: dbs.length, viabot: null, all_matches: [], conv_topics: [], skypexspaces_stores: [], error: null };

		// O schema exato de timestamp no IndexedDB do Teams varia entre versões/stores — tenta
		// os nomes de campo conhecidos do Skype/Teams primeiro (mais confiável), com fallback
		// genérico por qualquer chave própria de "row" cujo nome contenha "time" e o valor pareça
		// timestamp (string ISO ou epoch ms plausível). Normaliza epoch ms pra ISO string, já que
		// o parser Go só entende formatos RFC3339.
		const findRowTimestamp = (row) => {
			if (!row || typeof row !== 'object') return '';
			const candidates = [
				'composetime', 'composeTime', 'originalarrivaltime', 'originalArrivalTime',
				'clientArrivalTime', 'clientarrivaltime', 'arrivalTime', 'arrivaltime',
				'serverArrivalTime', 'createdTime', 'createdtime', 'timestamp', 'time',
			];
			for (const key of candidates) {
				const v = row[key];
				if (typeof v === 'string' && v.length >= 8) return v;
				if (typeof v === 'number' && v > 1000000000000) return new Date(v).toISOString();
			}
			for (const key of Object.keys(row)) {
				if (!/time/i.test(key)) continue;
				const v = row[key];
				if (typeof v === 'string' && v.length >= 8) return v;
				if (typeof v === 'number' && v > 1000000000000) return new Date(v).toISOString();
			}
			return '';
		};

		// 1. conversation-manager: buscar nos campos botMembers, threadProperties, lastMessage
		for (const {name} of dbs) {
			if (!name || !name.includes('conversation-manager:react-web-client')) continue;
			if (name.endsWith(':pt-br')) continue;
			try {
				const db = await openDB(name);
				if (!db.objectStoreNames.contains('conversations')) { db.close(); continue; }
				const rows = await getAll(db, 'conversations');
				results.total_convs = rows.length;
				const botSamples = [];
				for (const row of rows) {
					const bots = row.botMembers || [];
					const tp = row.threadProperties || {};
					const topic = tp.topic || tp.name || '';
					const lastMsg = typeof row.lastMessage === 'string' ? row.lastMessage : JSON.stringify(row.lastMessage || '');
					const raw = (JSON.stringify(bots) + lastMsg + topic).toLowerCase();
					if (bots.length > 0 && botSamples.length < 5) {
						botSamples.push({ id: row.id, bots, topic });
					}
					if (hasKw(raw)) {
						// Para o thread do Mr.ViaBot, retornar lastMessage completo e messages[]
						const isViaBot = row.id && row.id.includes('unq.gbl.spaces');
						const fullLastMsg = isViaBot ? (row.lastMessage || null) : undefined;
						const msgs = isViaBot ? (Array.isArray(row.messages) ? row.messages : []) : undefined;
						const match = { id: row.id, topic, bots, source: 'conv-manager', snippet: raw.substring(0, 600),
							...(isViaBot ? { lastMessage: fullLastMsg, messages: msgs } : {}) };
						results.all_matches.push(match);
						if (!results.viabot) results.viabot = match;
					}
				}
				results.bot_samples = botSamples;
				db.close();
				break;
			} catch(e) { results.error = String(e); }
		}

		// 2. chat-info-pane-manager: stores com mensagens pinadas e histórico de chats
		for (const {name} of dbs) {
			if (!name || !name.includes('chat-info-pane-manager')) continue;
			if (name.endsWith(':pt-br')) continue;
			try {
				const db = await openDB(name);
				const storeNames = Array.from(db.objectStoreNames);
				for (const sn of storeNames) {
					const rows = await getAll(db, sn);
					if (rows.length === 0) continue;
					if (!results.chat_info_sample) {
						const r0 = rows[0];
						results.chat_info_sample = { store: sn, keys: Object.keys(r0), snippet: JSON.stringify(r0).substring(0, 300) };
					}
					for (const row of rows) {
						const raw = JSON.stringify(row).toLowerCase();
						if (hasKw(raw)) {
							// conversationId (camelCase) é o thread real; id é o ID da mensagem pinada
							const threadId = row.conversationId || row.conversationid || row.threadId || row.id;
							const match = { id: threadId, msg_id: row.id, source: 'chat-info/' + sn, raw_keys: Object.keys(row), snippet: JSON.stringify(row).substring(0, 600) };
							results.all_matches.push(match);
							if (!results.viabot) results.viabot = match;
						}
					}
				}
				db.close();
				break;
			} catch(e) {}
		}

		// 3. skypexspaces: banco de mensagens reais — extrair conteúdo completo para o parser
		// O skypexspaces armazena o histórico offline sem lazy loading (independente do DOM).
		// Empurrar o JSON bruto de cada mensagem relevante: o parser Go usa regex e
		// encontra CHG + devstartcd URLs independente da estrutura exata do objeto.
		const idbMessages = [];
		for (const {name} of dbs) {
			if (!name || !name.includes('skypexspaces')) continue;
			try {
				const db = await openDB(name);
				const storeNames = Array.from(db.objectStoreNames);
				results.skypexspaces_stores = storeNames;
				for (const sn of storeNames) {
					const rows = await getAll(db, sn);
					if (rows.length > 0 && !results.skypex_sample) {
						results.skypex_sample = { store: sn, count: rows.length, snippet: JSON.stringify(rows[0]).substring(0, 300) };
					}
					for (const row of rows) {
						const raw = JSON.stringify(row);
						const rawLow = raw.toLowerCase();
						if (!hasKw(rawLow)) continue;
						results.viabot = { id: row.id || row.threadId, source: 'skypexspaces/' + sn, snippet: rawLow.substring(0, 400) };
						const postedAt = findRowTimestamp(row);
						// Extrair conteúdo de todos os campos textuais conhecidos
						const content = row.content || row.body || row.text || row.message || '';
						if (typeof content === 'string' && content.length > 0) {
							idbMessages.push({ text: content, postedAt }); // HTML ou texto — parser e regex vão extrair
						}
						// Também empurrar o JSON bruto (truncado): URLs aparecem verbatim no JSON
						idbMessages.push({ text: raw.substring(0, 8000), postedAt });
					}
				}
				db.close();
			} catch(e) {}
		}

		// Também extrair messages[] do conversation-manager (se disponível para o ViaBot)
		if (results.viabot && Array.isArray(results.viabot.messages)) {
			for (const msg of results.viabot.messages) {
				const postedAt = findRowTimestamp(msg);
				const s = typeof msg === 'string' ? msg : JSON.stringify(msg);
				if (s.length > 5) idbMessages.push({ text: s.substring(0, 8000), postedAt });
			}
		}

		results.idb_messages = idbMessages;
		return JSON.stringify(results);
	}`
	fetchResult, err := page.Eval(fetchJS)
	if err == nil && !fetchResult.Value.Nil() {
		rawConvs := fetchResult.Value.String()
		convPath := filepath.Join(outputDir, "conversations-raw.json")
		os.WriteFile(convPath, []byte(rawConvs), 0600) //nolint:errcheck
		logger.Info().Str("file", convPath).Int("bytes", len(rawConvs)).Msg("[Teams] Resultados de conversas salvos")

		// Salvar mensagens extraídas do IndexedDB em arquivo separado para o extractor
		var convResults map[string]interface{}
		if json.Unmarshal([]byte(rawConvs), &convResults) == nil {
			totalConvs, _ := convResults["total_convs"].(float64)
			totalDBs, _ := convResults["total_dbs"].(float64)
			logger.Info().
				Int("total_dbs", int(totalDBs)).
				Int("total_convs", int(totalConvs)).
				Msg("[Teams] IndexedDB escaneado")

			// Salvar mensagens do IndexedDB para processamento pelo extractor. Cada entrada vem
			// como {text, postedAt} do fetchJS — postedAt vazio quando findRowTimestamp não achou
			// nenhum campo de horário reconhecível no row.
			if idbMsgs, ok := convResults["idb_messages"].([]interface{}); ok && len(idbMsgs) > 0 {
				var rawMsgs []RawMessage
				for _, m := range idbMsgs {
					obj, ok := m.(map[string]interface{})
					if !ok {
						continue
					}
					text, _ := obj["text"].(string)
					if text == "" {
						continue
					}
					postedAt, _ := obj["postedAt"].(string)
					rawMsgs = append(rawMsgs, RawMessage{Text: text, PostedAt: postedAt})
				}
				if len(rawMsgs) > 0 {
					idbData, _ := json.Marshal(map[string]interface{}{"messages": rawMsgs})
					idbPath := filepath.Join(outputDir, "viabot-indexeddb-messages.json")
					os.WriteFile(idbPath, idbData, 0600) //nolint:errcheck
					logger.Info().Int("count", len(rawMsgs)).Str("file", idbPath).Msg("[Teams] Mensagens IndexedDB salvas para processamento")
				}
			}

			// Logar todos os matches encontrados
			if allMatchesRaw, ok := convResults["all_matches"].([]interface{}); ok && len(allMatchesRaw) > 0 {
				logger.Info().Int("count", len(allMatchesRaw)).Msg("[Teams] Matches com keywords SRE Approval encontrados")
				for i, mRaw := range allMatchesRaw {
					m, _ := mRaw.(map[string]interface{})
					if m == nil {
						continue
					}
					id, _ := m["id"].(string)
					source, _ := m["source"].(string)
					topic, _ := m["topic"].(string)
					snippet, _ := m["snippet"].(string)
					if len(snippet) > 200 {
						snippet = snippet[:200]
					}
					logger.Info().
						Int("match_n", i+1).
						Str("id", id).
						Str("source", source).
						Str("topic", topic).
						Str("snippet", snippet).
						Msg("[Teams] Match encontrado")
					// Preferir IDs no formato Teams thread (19:...@thread.v2)
					if strings.HasPrefix(id, "19:") && strings.Contains(id, "@thread") {
						result.Conversations = append(result.Conversations, ConversationHint{ID: id, DisplayName: topic})
					}
				}
			}

			// Verificar se encontrou MR.ViaBot diretamente
			if viabotRaw := convResults["viabot"]; viabotRaw != nil {
				viabot, _ := viabotRaw.(map[string]interface{})
				if viabot != nil {
					id, _ := viabot["id"].(string)
					topic, _ := viabot["topic"].(string)
					threadType, _ := viabot["threadType"].(string)
					source, _ := viabot["source"].(string)
					logger.Info().
						Str("thread_id", id).
						Str("topic", topic).
						Str("thread_type", threadType).
						Str("source", source).
						Msg("[Teams] ✅ MR.ViaBot encontrado no IndexedDB!")
					// Adicionar se ainda não foi adicionado via all_matches
					found := false
					for _, c := range result.Conversations {
						if c.ID == id {
							found = true
							break
						}
					}
					if !found {
						result.Conversations = append(result.Conversations, ConversationHint{ID: id, DisplayName: topic, ThreadType: threadType})
					}
				}
			} else {
				logger.Warn().Msg("[Teams] Mr.ViaBot não encontrado no IndexedDB — listando topics disponíveis")
				if topics, ok := convResults["conv_topics"].([]interface{}); ok {
					logger.Info().Int("count", len(topics)).Msg("[Teams] Topics coletados (buscar 'viabot' manualmente)")
					for _, c := range topics {
						conv, _ := c.(map[string]interface{})
						if conv == nil {
							continue
						}
						topic, _ := conv["topic"].(string)
						id, _ := conv["id"].(string)
						tl := strings.ToLower(topic)
						if strings.Contains(tl, "via") || strings.Contains(tl, "bot") || strings.Contains(tl, "mr") {
							logger.Info().Str("topic", topic).Str("id", id).Msg("[Teams] Candidato")
						}
					}
				}
				if stores, ok := convResults["skypexspaces_stores"].([]interface{}); ok {
					logger.Info().Int("stores", len(stores)).Msg("[Teams] Stores do skypexspaces escaneados")
				}
			}
		}

		// Tentativa HTTP direta (esperada falhar com MCAS — mantida para diagnóstico)
		if skypeToken != "" {
			msgPath := filepath.Join(outputDir, "viabot-messages.json")
			if err2 := fetchViaBotMessages(skypeToken, mrViaBotThreadID, msgPath, logger); err2 != nil {
				logger.Debug().Err(err2).Msg("[Teams] HTTP chatsvcagg bloqueado pelo MCAS (esperado)")
			}
		}
	} else {
		logger.Warn().Err(err).Msg("[Teams] Falha ao buscar conversas via JS")
	}

	// Apenas viabot-dom-messages.json e conversations-raw.json são lidos de volta pelo extractor.
	// Os demais arquivos de debug (auth-requests, chat-requests, discovery-*.json, etc.) foram
	// removidos — eram escritos e nunca lidos, consumindo disco desnecessariamente.
	printSummary(result, outputDir, logger)
	return result, nil
}

// fetchViaBotMessages faz chamada HTTP direta ao chatsvcagg (bypassa MCAS do browser).
// startTime: hoje às 00:00 UTC em ms. Tenta 3 endpoints (prod/gov/mcas).
func fetchViaBotMessages(skypeToken, threadID, outputPath string, logger *zerolog.Logger) error {
	// startTime = ontem 00:00 UTC (últimos 2 dias)
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour)
	startTimeMs := startOfDay.UnixMilli()

	encodedThread := url.PathEscape(threadID)
	endpoints := []string{
		fmt.Sprintf("https://chatsvcagg.teams.microsoft.com/v1/users/ME/conversations/%s/messages?startTime=%d&pageSize=200", encodedThread, startTimeMs),
		fmt.Sprintf("https://api.flightproxy.teams.microsoft.com/api/v2/ep/conv-svc-aggregator/conv/v1/users/ME/conversations/%s/messages?startTime=%d&pageSize=200", encodedThread, startTimeMs),
	}

	client := &http.Client{Timeout: 15 * time.Second}
	for _, endpoint := range endpoints {
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "skype_token "+skypeToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("X-Ms-Client-Version", "49/24112107003")
		req.Header.Set("X-Ms-Skypetoken", skypeToken)

		resp, err := client.Do(req)
		if err != nil {
			logger.Debug().Err(err).Str("url", endpoint).Msg("[Teams] HTTP endpoint falhou")
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		logger.Info().Int("status", resp.StatusCode).Str("url", endpoint).Int("bytes", len(body)).Msg("[Teams] Resposta chatsvcagg")

		if resp.StatusCode == http.StatusOK {
			os.WriteFile(outputPath, body, 0600) //nolint:errcheck
			logger.Info().Str("file", outputPath).Msg("[Teams] ✅ Mensagens do MR.ViaBot salvas!")
			return nil
		}
		// Salvar resposta de erro para diagnóstico
		errPath := outputPath + ".error.json"
		os.WriteFile(errPath, body, 0600) //nolint:errcheck
		logger.Warn().Int("status", resp.StatusCode).Str("error_file", errPath).Msg("[Teams] Endpoint retornou erro — salvo para diagnóstico")
	}
	return fmt.Errorf("todos os endpoints falharam para threadId=%s", threadID)
}

func extractConversations(convResp map[string]interface{}, result *DiscoveryResult, logger *zerolog.Logger) {
	// Suporta campo "value" (OData), "conversations" ou array direto
	var list []interface{}
	switch {
	case convResp["value"] != nil:
		list, _ = convResp["value"].([]interface{})
	case convResp["conversations"] != nil:
		list, _ = convResp["conversations"].([]interface{})
	}
	logger.Info().Int("total", len(list)).Msg("[Teams] Total de conversas encontradas")
	for _, c := range list {
		conv, _ := c.(map[string]interface{})
		if conv == nil {
			continue
		}
		topic, _ := conv["topic"].(string)
		id, _ := conv["id"].(string)
		threadType, _ := conv["threadType"].(string)
		tl := strings.ToLower(topic)
		isBot := strings.Contains(tl, "mrviabot") || strings.Contains(tl, "mr.viabot") || strings.Contains(tl, "viabot")
		if isBot {
			logger.Info().Str("thread_id", id).Str("topic", topic).Msg("[Teams] ✅ MR.ViaBot threadId encontrado!")
		}
		result.Conversations = append(result.Conversations, ConversationHint{
			ID: id, DisplayName: topic, ThreadType: threadType,
		})
	}
}

func printSummary(result *DiscoveryResult, _ string, logger *zerolog.Logger) {
	logger.Info().Msg("[Teams] ════════════════ RESUMO ════════════════")
	logger.Info().Msgf("[Teams] Auth requests : %d", len(result.AuthRequests))
	logger.Info().Msgf("[Teams] Chat requests : %d", len(result.ChatRequests))
	logger.Info().Msgf("[Teams] WS frames     : %d", len(result.WebSockets))
	logger.Info().Msgf("[Teams] Conversas     : %d", len(result.Conversations))
	if result.SkypeToken != "" {
		logger.Info().Msgf("[Teams] SkypeToken    : %s...", result.SkypeToken[:min(30, len(result.SkypeToken))])
	} else {
		logger.Warn().Msg("[Teams] SkypeToken    : NÃO ENCONTRADO")
	}
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
