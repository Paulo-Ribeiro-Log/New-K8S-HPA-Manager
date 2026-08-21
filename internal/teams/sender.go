package teams

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
	"github.com/ysmood/gson"
)

// SendResult representa o resultado do envio para um destinatário.
type SendResult struct {
	ThreadID string `json:"thread_id"`
	OK       bool   `json:"ok"`
	Status   int    `json:"status"`
	Error    string `json:"error,omitempty"`
	Index    int    `json:"index"`
	Total    int    `json:"total"`
	// MessageID é o ID real atribuído pelo servidor do Teams a esta mensagem — capturado do
	// header Location da resposta HTTP 201 (padrão REST de "recurso criado"; confirmado contra
	// uma implementação de terceiro do mesmo protocolo, ocilo/skype-http, que extrai o ID exatamente
	// dessa forma), com fallback pro corpo da resposta se o header não vier. Necessário pra poder
	// apagar a mensagem depois (DeleteMessages abaixo) — o `clientmessageid` gerado localmente na
	// hora de montar o envio NUNCA serve pra isso, o servidor não o usa como identificador.
	// Vazio quando o envio falhou ou quando o servidor não expôs o ID por nenhum dos dois meios.
	MessageID string `json:"message_id,omitempty"`
}

// ProgressFunc é chamada a cada destinatário concluído (sucesso ou falha).
type ProgressFunc func(result SendResult)

// DeleteTarget identifica uma mensagem já enviada que deve ser apagada: a thread e o MessageID
// real (SendResult.MessageID) — nunca um clientmessageid.
type DeleteTarget struct {
	ThreadID  string `json:"thread_id"`
	MessageID string `json:"message_id"`
}

// DeleteResult é o resultado de apagar uma mensagem.
type DeleteResult struct {
	ThreadID  string `json:"thread_id"`
	MessageID string `json:"message_id"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`
	Error     string `json:"error,omitempty"`
}

// teamsAuthExtractJS é o trecho de JS compartilhado entre SendBatch e DeleteMessages: extrai
// bearerToken/userMRI/displayName do cache MSAL no localStorage da página do Teams e monta
// baseURL — mesmo mecanismo de autenticação usado pelos dois fluxos de escrita (enviar/apagar),
// que dependem do mesmo Bearer token pra chamar o chatsvcagg/amer. Extraído aqui (era duplicado
// inline dentro de SendBatch) pra DeleteMessages não reimplementar a mesma extração de token.
const teamsAuthExtractJS = `
		// ── 1. Extrair Bearer token do cache MSAL no localStorage ─────────────
		let bearerToken = '';
		let userMRI     = '';
		let displayName = '';

		for (let i = 0; i < localStorage.length; i++) {
			const key = localStorage.key(i);
			if (!key) continue;
			const val = localStorage.getItem(key);
			if (!val) continue;

			if (key.toLowerCase().includes('accesstoken') &&
				(key.toLowerCase().includes('ic3.teams') ||
				 key.toLowerCase().includes('teams.officeclient') ||
				 key.toLowerCase().includes('teams.communication'))) {
				try {
					const obj = JSON.parse(val);
					const token = obj.secret || obj.access_token || obj.token || '';
					if (token && token.length > 50) bearerToken = token;
				} catch {}
			}
			if (!userMRI && key.toLowerCase().includes('account')) {
				try {
					const obj = JSON.parse(val);
					const mri = obj.localAccountId || obj.homeAccountId || '';
					if (mri && !mri.includes('login.windows') && !mri.includes('.')) {
						userMRI = '8:orgid:' + (obj.localAccountId || '');
					}
					if (obj.name && !displayName) displayName = obj.name;
				} catch {}
			}
		}

		// Fallback: qualquer chave com token Bearer longo
		if (!bearerToken) {
			for (let i = 0; i < localStorage.length; i++) {
				const key = localStorage.key(i);
				if (!key) continue;
				const val = localStorage.getItem(key);
				if (!val) continue;
				try {
					const obj = JSON.parse(val);
					for (const c of [obj.secret, obj.access_token, obj.token]) {
						if (typeof c === 'string' && c.length > 100 && c.startsWith('eyJ')) {
							bearerToken = c;
							break;
						}
					}
				} catch {}
				if (bearerToken) break;
			}
		}

		if (!bearerToken) {
			return JSON.stringify({ error: 'Bearer token não encontrado no localStorage' });
		}

		// ── 2. MRI via JWT se não encontrado ──────────────────────────────────
		if (!userMRI) {
			try {
				const parts = bearerToken.split('.');
				if (parts.length >= 2) {
					const payload = JSON.parse(atob(parts[1].replace(/-/g,'+').replace(/_/g,'/')));
					const oid = payload.oid || payload.sub || '';
					if (oid) userMRI = '8:orgid:' + oid;
					if (!displayName) displayName = payload.name || payload.upn || '';
				}
			} catch {}
		}
		if (!userMRI)     userMRI     = '8:orgid:unknown';
		if (!displayName) displayName = 'Unknown User';

		// ── 3. Host base (MCAS ou direto) ────────────────────────────────────
		const baseURL = location.protocol + '//' + location.host;
`

// SendBatch abre o Teams via go-rod e envia htmlContent para cada threadID em lote.
// onProgress é chamada após cada envio individual — pode ser nil.
// Todo o tráfego HTTP passa pela página do Teams (same-origin) para contornar o MCAS.
func SendBatch(sessionDir string, threadIDs []string, htmlContent string, onProgress ProgressFunc, logger *zerolog.Logger) ([]SendResult, error) {
	// Serializa contra RunDiscovery/ScanConversations (ver operationMu em browser_manager.go).
	operationMu.Lock()
	defer operationMu.Unlock()

	browser, err := getBrowser(sessionDir, logger)
	if err != nil {
		return nil, err
	}

	createdPage, err := browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %w", err)
	}
	defer createdPage.Close() //nolint:errcheck

	restoreWindow(createdPage, logger)

	logger.Info().Msg("[Sender] Aguardando Teams carregar (máx 3min)...")
	var page *rod.Page
	deadline := time.Now().Add(3 * time.Minute)
	loaded := false
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
				page = p
				loaded = true
				logger.Info().Str("url", u).Msg("[Sender] Teams detectado")
				break
			}
		}
		if loaded {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !loaded {
		return nil, fmt.Errorf("timeout aguardando Teams carregar")
	}

	// Espera de verdade a UI autenticar (barra lateral renderizada) antes de minimizar — ver
	// comentário de waitForTeamsUIReady em browser_manager.go. Antes disso minimizava a janela
	// assim que a URL batia (sinal fraco, verdadeiro mesmo em tela de login) e só esperava um
	// sleep fixo de 20s — um login pendente nunca tinha chance real de ser completado, e o envio
	// seguia adiante mesmo sem sessão válida (só falhava depois, lá no JS, ao não achar Bearer
	// token no localStorage — erro tardio e confuso pra quem só via "não conecta no Teams").
	logger.Info().Msg("[Sender] Aguardando Teams inicializar e autenticar (máx 4min)...")
	if !waitForTeamsUIReady(page, sessionDir, logger, "[Sender]") {
		return nil, fmt.Errorf("teams não terminou de carregar/autenticar a tempo — se uma tela de login apareceu, complete-a e tente novamente")
	}

	// Daqui em diante os envios são via CDP/JS (preenche e clica o compose box) — sem
	// necessidade de interação do usuário.
	minimizeWindow(page, logger)

	// Timeout generoso: 10min para envios em lote grandes.
	page = page.Timeout(10 * time.Minute)

	// Expor callback Go → JS: chamado após cada send individual.
	// O CDP binding é awaitable do lado JS.
	stopExpose, err := page.Expose("onSendProgress", func(j gson.JSON) (interface{}, error) {
		if onProgress == nil {
			return nil, nil
		}
		var r SendResult
		if err := j.Unmarshal(&r); err != nil {
			logger.Warn().Err(err).Msg("[Sender] Falha ao parsear progresso")
			return nil, nil
		}
		onProgress(r)
		return nil, nil
	})
	if err != nil {
		logger.Warn().Err(err).Msg("[Sender] Falha ao expor onSendProgress — continuando sem callbacks")
	} else {
		defer stopExpose() //nolint:errcheck
	}

	escapedHTML := escapeJSString(htmlContent)
	threadIDsJSON, _ := json.Marshal(threadIDs)

	js := "async () => {\n\t\tconst sleep = ms => new Promise(r => setTimeout(r, ms));\n" +
		teamsAuthExtractJS +
		fmt.Sprintf(`
		// ── 4. Envio em lote com callback de progresso após cada item ─────────
		const threadIDs   = %s;
		const htmlContent = '%s';
		const batchSize   = 10;
		const total       = threadIDs.length;
		const results     = [];

		for (let i = 0; i < threadIDs.length; i += batchSize) {
			const batch = threadIDs.slice(i, i + batchSize);
			const batchResults = await Promise.all(batch.map(async (threadId, batchIdx) => {
				const globalIdx = i + batchIdx;
				const now   = new Date().toISOString();
				const msgId = String(Date.now()) + String(Math.floor(Math.random() * 1000000)).padStart(6, '0');

				const encodedThread = encodeURIComponent(threadId);
				const url = baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread + '/messages';

				// contenttype 'Text' (capitalizado) e messagetype 'RichText/Html' são os valores
				// REAIS capturados via HijackRouter contra o endpoint chatsvcagg/amer do Teams
				// moderno (scripts/teams-spy-send/main.go, Fase 1) — confirmados em produção há
				// meses. NÃO trocar por 'text'/'RichText' (valores do protocolo Skype S2S clássico,
				// documentados em libs de terceiro tipo skype-http/SkPy): esse é um protocolo
				// diferente/mais novo, e 'RichText' sem sufixo faz o Teams tratar o conteúdo como
				// texto literal, exibindo as tags HTML cruas em vez de renderizá-las — bug real
				// reproduzido ao vivo ao tentar essa troca (revertido no mesmo dia). Se algum dia
				// precisar reconfirmar, use scripts/teams-spy-send pra capturar um envio manual real
				// e comparar contra estes valores antes de mudar qualquer coisa aqui.
				const body = JSON.stringify({
					amsreferences: [], callId: '', clientmessageid: msgId,
					composetime: now, content: htmlContent, contenttype: 'Text',
					conversationLink: baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread,
					conversationid: threadId, crossPostChannels: [],
					from: userMRI, fromUserId: userMRI, id: '-1',
					imdisplayname: displayName, messagetype: 'RichText/Html',
					originalarrivaltime: now,
					properties: { cards:'[]', files:'[]', formatVariant:'TEAMS',
						importance:'', links:'[]', mentions:'[]',
						onbehalfof:null, policyViolation:null, subject:'', title:'' },
					state: 0, type: 'Message', version: '0',
				});

				let result;
				try {
					const resp = await fetch(url, {
						method: 'POST',
						headers: {
							'authorization': 'Bearer ' + bearerToken,
							'content-type': 'application/json',
							'behavioroverride': 'redirectAs404',
							'x-ms-migration': 'True',
							'x-ms-request-priority': '0',
							'x-ms-test-user': 'False',
						},
						body,
					});
					let errMsg = '';
					let messageId = '';
					if (!resp.ok) {
						try { errMsg = (await resp.text()).slice(0, 200); } catch {}
					} else {
						// O ID real da mensagem (necessário pra poder apagá-la depois) vem no
						// header Location da resposta 201 — padrão REST de "recurso criado",
						// confirmado contra uma implementação de terceiro do mesmo protocolo
						// (ocilo/skype-http). Fallback pro corpo da resposta (campos
						// Id/id/MessageId/messageId) caso o endpoint específico do Teams não
						// exponha o header — nunca visto acontecer, mas mais barato checar do
						// que assumir.
						try {
							const loc = resp.headers.get('location') || '';
							if (loc) {
								const segs = loc.split('/').filter(Boolean);
								messageId = segs[segs.length - 1] || '';
							}
						} catch {}
						if (!messageId) {
							try {
								const bodyText = await resp.text();
								if (bodyText) {
									const bodyJson = JSON.parse(bodyText);
									messageId = String(bodyJson.Id || bodyJson.id || bodyJson.MessageId || bodyJson.messageId || '');
								}
							} catch {}
						}
					}
					result = { thread_id: threadId, ok: resp.ok, status: resp.status, error: errMsg, message_id: messageId, index: globalIdx, total };
				} catch(e) {
					result = { thread_id: threadId, ok: false, status: 0, error: String(e), message_id: '', index: globalIdx, total };
				}

				// Notificar Go sobre este envio individual
				try { await window.onSendProgress(JSON.stringify(result)); } catch {}
				return result;
			}));
			results.push(...batchResults);
			if (i + batchSize < threadIDs.length) await sleep(600);
		}

		return JSON.stringify({ results, mri: userMRI, display_name: displayName });
	}`, string(threadIDsJSON), escapedHTML)

	logger.Info().Int("recipients", len(threadIDs)).Msg("[Sender] Iniciando envio em lote...")

	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar envio JS: %w", err)
	}

	var out struct {
		Error       string       `json:"error"`
		Results     []SendResult `json:"results"`
		MRI         string       `json:"mri"`
		DisplayName string       `json:"display_name"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return nil, fmt.Errorf("erro ao parsear resultado: %w (raw: %.200s)", err, res.Value.String())
	}
	if out.Error != "" {
		return nil, fmt.Errorf("erro no JS de envio: %s", out.Error)
	}

	ok := 0
	for _, r := range out.Results {
		if r.OK {
			ok++
		}
	}
	logger.Info().Str("mri", out.MRI).Int("sent", ok).Int("failed", len(out.Results)-ok).Msg("[Sender] Envio concluído")
	return out.Results, nil
}

// DeleteMessages abre o Teams via go-rod e apaga cada mensagem em targets — mesmo protocolo do
// SendBatch (mesmo endpoint chatsvcagg/amer, mesma extração de Bearer token via
// teamsAuthExtractJS), com DELETE em vez de POST. Confirmado contra uma implementação de
// terceiro do protocolo Skype/Teams (Terrance/SkPy): DELETE
// {msgsHost}/users/ME/conversations/{threadId}/messages/{messageId}, sem corpo. MessageID
// precisa ser o ID REAL atribuído pelo servidor (SendResult.MessageID, capturado do header
// Location no momento do envio) — o clientmessageid usado só na hora de compor a mensagem nunca
// serve como identificador pro servidor.
func DeleteMessages(sessionDir string, targets []DeleteTarget, logger *zerolog.Logger) ([]DeleteResult, error) {
	// Serializa contra RunDiscovery/ScanConversations/SendBatch (ver operationMu em
	// browser_manager.go) — mesmo perfil de Chrome, abas paralelas podem invalidar o SkypeToken.
	operationMu.Lock()
	defer operationMu.Unlock()

	browser, err := getBrowser(sessionDir, logger)
	if err != nil {
		return nil, err
	}

	createdPage, err := browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %w", err)
	}
	defer createdPage.Close() //nolint:errcheck

	restoreWindow(createdPage, logger)

	logger.Info().Msg("[Sender] Aguardando Teams carregar (máx 3min)...")
	var page *rod.Page
	deadline := time.Now().Add(3 * time.Minute)
	loaded := false
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
				page = p
				loaded = true
				logger.Info().Str("url", u).Msg("[Sender] Teams detectado (apagar mensagem)")
				break
			}
		}
		if loaded {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !loaded {
		return nil, fmt.Errorf("timeout aguardando Teams carregar")
	}

	logger.Info().Msg("[Sender] Aguardando Teams inicializar e autenticar (máx 4min)...")
	if !waitForTeamsUIReady(page, sessionDir, logger, "[Sender]") {
		return nil, fmt.Errorf("teams não terminou de carregar/autenticar a tempo — se uma tela de login apareceu, complete-a e tente novamente")
	}

	minimizeWindow(page, logger)
	page = page.Timeout(3 * time.Minute)

	targetsJSON, _ := json.Marshal(targets)

	js := "async () => {\n\t\tconst sleep = ms => new Promise(r => setTimeout(r, ms));\n" +
		teamsAuthExtractJS +
		fmt.Sprintf(`
		// ── 4. Apagar em lote ──────────────────────────────────────────────────
		const targets   = %s;
		const batchSize = 10;
		const results   = [];

		for (let i = 0; i < targets.length; i += batchSize) {
			const batch = targets.slice(i, i + batchSize);
			const batchResults = await Promise.all(batch.map(async (t) => {
				const encodedThread = encodeURIComponent(t.thread_id);
				const encodedMsg    = encodeURIComponent(t.message_id);
				const url = baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread + '/messages/' + encodedMsg;

				let result;
				try {
					const resp = await fetch(url, {
						method: 'DELETE',
						headers: {
							'authorization': 'Bearer ' + bearerToken,
							'behavioroverride': 'redirectAs404',
							'x-ms-migration': 'True',
						},
					});
					let errMsg = '';
					if (!resp.ok) {
						try { errMsg = (await resp.text()).slice(0, 200); } catch {}
					}
					result = { thread_id: t.thread_id, message_id: t.message_id, ok: resp.ok, status: resp.status, error: errMsg };
				} catch(e) {
					result = { thread_id: t.thread_id, message_id: t.message_id, ok: false, status: 0, error: String(e) };
				}
				return result;
			}));
			results.push(...batchResults);
			if (i + batchSize < targets.length) await sleep(600);
		}

		return JSON.stringify({ results });
	}`, string(targetsJSON))

	logger.Info().Int("targets", len(targets)).Msg("[Sender] Apagando mensagens em lote...")

	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar apagar JS: %w", err)
	}

	var out struct {
		Error   string         `json:"error"`
		Results []DeleteResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return nil, fmt.Errorf("erro ao parsear resultado: %w (raw: %.200s)", err, res.Value.String())
	}
	if out.Error != "" {
		return nil, fmt.Errorf("erro no JS de apagar: %s", out.Error)
	}

	ok := 0
	for _, r := range out.Results {
		if r.OK {
			ok++
		}
	}
	logger.Info().Int("deleted", ok).Int("failed", len(out.Results)-ok).Msg("[Sender] Apagar concluído")
	return out.Results, nil
}

// escapeJSString escapa uma string para ser embutida com segurança em template JS
// delimitado por aspas simples.
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return s
}
