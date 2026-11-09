package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
	"github.com/ysmood/gson"

	"k8s-hpa-manager/internal/browser"
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
		// O cache MSAL guarda VÁRIOS access tokens (escopos/audiências diferentes, alguns já
		// vencidos). Pegar "o último que casa" mandava, às vezes, um token vencido/errado e o
		// Teams respondia 401 "Authentication failed" (errorCode 911) para todos os destinatários.
		// Por isso coletamos todos, ordenamos (válido > vencido, escopo ic3.teams primeiro, mais
		// novo primeiro) e fetchWithAuth tenta os demais quando o escolhido leva 401.
		const tokenCandidates = [];
		const nowSec = Date.now() / 1000;
		const jwtExp = (t) => {
			try {
				const pl = JSON.parse(atob(t.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
				return Number(pl.exp) || 0;
			} catch { return 0; }
		};
		const jwtAud = (t) => {
			try {
				const pl = JSON.parse(atob(t.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
				return String(pl.aud || '');
			} catch { return ''; }
		};
		let currentKey = '';
		const addCandidate = (token, pref) => {
			if (!token || token.length <= 50 || tokenCandidates.some(c => c.token === token)) return;
			tokenCandidates.push({ token, pref, exp: jwtExp(token), aud: jwtAud(token), key: currentKey.slice(0, 140) });
		};
		// Diagnóstico SEM segredo: só nomes de chave, formato do valor (nomes dos campos), audiência e
		// validade. Serve para ver se o cache do Teams mudou (ex.: valores criptografados).
		const authDiag = () => ({
			candidates: tokenCandidates.map(c => ({ aud: c.aud, expMin: c.exp ? Math.round((c.exp - nowSec) / 60) : null, pref: c.pref, key: c.key })),
			// Formato do cache "tmp.auth.v1" (caminho -> tipo/tamanho/4 primeiros caracteres; nunca o valor).
			tmpShapes: (() => {
				const out = [];
				for (let i = 0; i < localStorage.length && out.length < 6; i++) {
					const k = localStorage.key(i) || '';
					if (!/^tmp\.auth\.v1\..*\.token\.https:\/\/(chatsvcagg|ic3|api\.spaces)/i.test(k)) continue;
					const leaves = [];
					const walk = (v, path, d) => {
						if (leaves.length >= 14 || d > 5) return;
						if (v === null || v === undefined) { leaves.push(path + ':null'); return; }
						if (typeof v === 'string') {
							if (v[0] === '{' || v[0] === '[') { try { walk(JSON.parse(v), path + '(json)', d + 1); return; } catch {} }
							leaves.push(path + ':str[' + v.length + ']' + v.slice(0, 4));
						} else if (typeof v === 'number' || typeof v === 'boolean') {
							leaves.push(path + ':' + v);
						} else if (typeof v === 'object') {
							for (const kk in v) walk(v[kk], path + '.' + kk, d + 1);
						}
					};
					try { walk(JSON.parse(localStorage.getItem(k) || ''), '', 0); } catch { leaves.push('não-JSON'); }
					out.push({ key: k.slice(-48), leaves });
				}
				return out;
			})(),
			msalKeys: (() => {
				const out = [];
				for (let i = 0; i < localStorage.length && out.length < 40; i++) {
					const k = localStorage.key(i) || '';
					if (!/msal|accesstoken|idtoken|token/i.test(k)) continue;
					let shape = '?';
					try {
						const o = JSON.parse(localStorage.getItem(k) || '');
						shape = (o && typeof o === 'object') ? Object.keys(o).slice(0, 8).join(',') : typeof o;
					} catch { shape = 'não-JSON'; }
					out.push({ key: k.slice(0, 140), shape });
				}
				return out;
			})(),
		});

		for (let i = 0; i < localStorage.length; i++) {
			const key = localStorage.key(i);
			if (!key) continue;
			const val = localStorage.getItem(key);
			if (!val) continue;

			currentKey = key;
			// Cache próprio do Teams (substituiu o MSAL para os tokens vigentes): chaves
			// "tmp.auth.v1.<id>.Token.<RECURSO>" com { item, shouldRefresh, hitCount }. O token do
			// chatsvcagg é o do endpoint de chat; o do MSAL ficou só como resíduo (vencido há semanas).
			if (/^tmp\.auth\.v1\..*\.token\./i.test(key)) {
				try {
					const found = [];
					const walk = (v, d) => {
						if (v == null || d > 4) return;
						if (typeof v === 'string') {
							if (v.startsWith('eyJ') && v.length > 100) found.push(v);
							else if (v[0] === '{') { try { walk(JSON.parse(v), d + 1); } catch {} }
						} else if (typeof v === 'object') {
							for (const k in v) walk(v[k], d + 1);
						}
					};
					walk(JSON.parse(val), 0);
					const up = key.toUpperCase();
					for (const t of found) addCandidate(t, up.includes('CHATSVCAGG') ? 3 : (up.includes('IC3') ? 2 : 0));
				} catch {}
			}
			if (key.toLowerCase().includes('accesstoken') &&
				(key.toLowerCase().includes('ic3.teams') ||
				 key.toLowerCase().includes('teams.officeclient') ||
				 key.toLowerCase().includes('teams.communication'))) {
				try {
					const obj = JSON.parse(val);
					const token = obj.secret || obj.access_token || obj.token || '';
					addCandidate(token, key.toLowerCase().includes('ic3.teams') ? 1 : 0);
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
		if (tokenCandidates.length === 0) {
			for (let i = 0; i < localStorage.length; i++) {
				const key = localStorage.key(i);
				if (!key) continue;
				const val = localStorage.getItem(key);
				if (!val) continue;
				try {
					const obj = JSON.parse(val);
					for (const c of [obj.secret, obj.access_token, obj.token]) {
						if (typeof c === 'string' && c.length > 100 && c.startsWith('eyJ')) {
							addCandidate(c, 0);
						}
					}
				} catch {}
			}
		}

		tokenCandidates.sort((a, b) => {
			const va = a.exp === 0 || a.exp > nowSec + 60 ? 1 : 0;
			const vb = b.exp === 0 || b.exp > nowSec + 60 ? 1 : 0;
			return (vb - va) || (b.pref - a.pref) || (b.exp - a.exp);
		});
		if (tokenCandidates.length) bearerToken = tokenCandidates[0].token;

		if (!bearerToken) {
			return JSON.stringify({ error: 'Bearer token não encontrado no localStorage', auth_diag: authDiag() });
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

		// fetch com o Bearer escolhido; se o Teams responde 401, tenta os outros tokens do cache e
		// passa a usar o que funcionar (um 401 não processa a requisição, então repetir é seguro).
		const fetchWithAuth = async (url, init) => {
			const call = (tok) => fetch(url, { ...init, headers: { ...init.headers, 'authorization': 'Bearer ' + tok } });
			const resp = await call(bearerToken);
			if (resp.status !== 401) return resp;
			for (const c of tokenCandidates) {
				if (c.token === bearerToken || (c.exp && c.exp < nowSec)) continue; // pula vencidos
				const r = await call(c.token);
				if (r.status !== 401) { bearerToken = c.token; return r; }
			}
			return resp;
		};
`

// SendBatch abre o Teams via go-rod e envia htmlContent para cada threadID em lote.
// onProgress é chamada após cada envio individual — pode ser nil.
// Todo o tráfego HTTP passa pela página do Teams (same-origin) para contornar o MCAS.
func SendBatch(sessionDir string, threadIDs []string, htmlContent string, onProgress ProgressFunc, logger *zerolog.Logger) ([]SendResult, error) {
	return SendBatchNotify(context.Background(), sessionDir, threadIDs, htmlContent, onProgress, nil, logger)
}

// NoticeFunc recebe avisos para o usuário durante a operação (ex.: "faça login no Teams").
type NoticeFunc func(msg string)

// ErrCancelled indica que a operação foi cancelada pelo usuário antes de começar a enviar/alterar.
var ErrCancelled = fmt.Errorf("operação cancelada")

// SendBatchNotify é o SendBatch com um canal de avisos (onNotice pode ser nil) e cancelamento: ao
// cancelar ctx, os lotes que ainda não começaram não são enviados (as mensagens já enviadas
// continuam, com o message_id) e os destinatários restantes voltam com Error "cancelado".
func SendBatchNotify(ctx context.Context, sessionDir string, threadIDs []string, htmlContent string, onProgress ProgressFunc, onNotice NoticeFunc, logger *zerolog.Logger) ([]SendResult, error) {
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
			if isTeamsURL(u) &&
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
		logger.Warn().Str("abas", pageURLsSummary(browser)).Msg("[Sender] Nenhuma aba do Teams reconhecida antes do timeout")
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

	// A barra lateral aparece mesmo com a sessão expirada (o Teams mostra o pedido de login por
	// cima), e o token do cache vencido gera 401 em todo destinatário. Confirma a autenticação de
	// verdade e, se preciso, mantém a janela visível esperando o login.
	if err := ensureTeamsAuth(ctx, page, sessionDir, logger, onNotice); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ErrCancelled
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
		// O JS manda JSON.stringify(result): o gson já entrega isso como string (Unmarshal falha
		// com "value has been parsed"), então o progresso ao vivo nunca chegava à tela.
		var r SendResult
		if err := json.Unmarshal([]byte(j.Str()), &r); err != nil {
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

	// Bug real corrigido (relatado ao vivo: "a mensagem foi enviada sem a imagem" — confirmado
	// que o Teams aceita a requisição de texto com sucesso mas descarta/sanitiza qualquer
	// "<img src="data:...">" embutido): antes de montar o payload de texto, faz upload real de
	// cada imagem embutida (ver uploadImagesInHTML/uploadImageToAMS) pro serviço de mídia AMS do
	// Teams — mesmo protocolo usado pelo cliente nativo, descoberto capturando o tráfego de um
	// upload manual real (scripts/teams-spy-upload). Upload único, compartilhado entre todos os
	// destinatários deste envio (permissions inclui todos os threadIDs) — best-effort, se falhar
	// mantém o data URI original (nunca pior que o comportamento anterior).
	htmlContent = uploadImagesInHTML(page, htmlContent, threadIDs, logger)

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
			if (window.__bcCancel) {
				for (let j = i; j < threadIDs.length; j++) {
					results.push({ thread_id: threadIDs[j], ok: false, status: 0, error: 'cancelado', message_id: '', index: j, total });
				}
				break;
			}
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
					const resp = await fetchWithAuth(url, {
						method: 'POST',
						headers: {
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

		return JSON.stringify({ results, mri: userMRI, display_name: displayName, auth_diag: { ...authDiag(), usedAud: (tokenCandidates.find(c => c.token === bearerToken) || {}).aud || '' } });
	}`, string(threadIDsJSON), escapedHTML)

	logger.Info().Int("recipients", len(threadIDs)).Msg("[Sender] Iniciando envio em lote...")

	stopCancel := armCancel(ctx, page, logger)
	defer stopCancel()
	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar envio JS: %w", err)
	}

	var out struct {
		Error       string       `json:"error"`
		Results     []SendResult `json:"results"`
		MRI         string       `json:"mri"`
		DisplayName string       `json:"display_name"`
		AuthDiag    interface{}  `json:"auth_diag"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return nil, fmt.Errorf("erro ao parsear resultado: %w (raw: %.200s)", err, res.Value.String())
	}
	if out.Error != "" {
		logAuthDiag(logger, out.AuthDiag)
		return nil, fmt.Errorf("erro no JS de envio: %s", out.Error)
	}

	ok := 0
	for _, r := range out.Results {
		if r.OK {
			ok++
		}
	}
	logger.Info().Str("mri", out.MRI).Int("sent", ok).Int("failed", len(out.Results)-ok).Msg("[Sender] Envio concluído")
	for i := range out.Results {
		if r := &out.Results[i]; !r.OK && r.Status == 401 {
			r.Error = "Sessão do Teams expirada ou sem permissão (401): abra a janela do Chrome do Teams no servidor, faça login e reenvie. Detalhe: " + r.Error
		}
	}
	logSendFailures(logger, out.Results)
	if ok < len(out.Results) {
		logAuthDiag(logger, out.AuthDiag)
	}
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
func DeleteMessages(ctx context.Context, sessionDir string, targets []DeleteTarget, logger *zerolog.Logger) ([]DeleteResult, error) {
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
			if isTeamsURL(u) &&
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
		logger.Warn().Str("abas", pageURLsSummary(browser)).Msg("[Sender] Nenhuma aba do Teams reconhecida antes do timeout")
		return nil, fmt.Errorf("timeout aguardando Teams carregar")
	}

	logger.Info().Msg("[Sender] Aguardando Teams inicializar e autenticar (máx 4min)...")
	if !waitForTeamsUIReady(page, sessionDir, logger, "[Sender]") {
		return nil, fmt.Errorf("teams não terminou de carregar/autenticar a tempo — se uma tela de login apareceu, complete-a e tente novamente")
	}

	if err := ensureTeamsAuth(ctx, page, sessionDir, logger, nil); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ErrCancelled
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
			if (window.__bcCancel) {
				for (const t of targets.slice(i)) {
					results.push({ thread_id: t.thread_id, message_id: t.message_id, ok: false, status: 0, error: 'cancelado' });
				}
				break;
			}
			const batch = targets.slice(i, i + batchSize);
			const batchResults = await Promise.all(batch.map(async (t) => {
				const encodedThread = encodeURIComponent(t.thread_id);
				const encodedMsg    = encodeURIComponent(t.message_id);
				const url = baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread + '/messages/' + encodedMsg;

				let result;
				try {
					const resp = await fetchWithAuth(url, {
						method: 'DELETE',
						headers: {
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

	stopCancel := armCancel(ctx, page, logger)
	defer stopCancel()
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

// logSendFailures registra, agrupado por status HTTP, o motivo das falhas (antes o log só trazia a
// contagem e o motivo real — ex.: 401 Authentication failed — ficava só na tela).
func logSendFailures(logger *zerolog.Logger, results []SendResult) {
	type key struct {
		status int
		msg    string
	}
	counts := map[key]int{}
	for _, r := range results {
		if r.OK {
			continue
		}
		msg := r.Error
		if len(msg) > 200 {
			msg = msg[:200]
		}
		counts[key{r.Status, msg}]++
	}
	for k, n := range counts {
		logger.Warn().Int("status", k.status).Str("erro", k.msg).Int("destinatarios", n).Msg("[Sender] Falha no envio")
	}
}

// logAuthDiag registra o diagnóstico de autenticação do JS (sem nenhum token: nomes de chave,
// formato do valor, audiência e validade) quando um envio falha.
func logAuthDiag(logger *zerolog.Logger, diag interface{}) {
	if diag == nil {
		return
	}
	b, _ := json.Marshal(diag)
	logger.Warn().RawJSON("auth_diag", b).Msg("[Sender] Diagnóstico de autenticação do Teams (sem tokens)")
}

// teamsAuthWaitTimeout é o teto de espera pelo login do usuário quando a sessão do Teams expirou.
const teamsAuthWaitTimeout = 5 * time.Minute

// probeTeamsAuth faz uma chamada autenticada barata ao chatsvc (com todos os tokens do cache, ver
// fetchWithAuth) e devolve o status HTTP. 401 = sem sessão válida; 0 = nenhum token no cache; -1 =
// falha de rede/JS (desconhecido: não trava o envio). Qualquer outro status significa que a
// autenticação foi aceita.
func probeTeamsAuth(page *rod.Page) int {
	js := "async () => {\n\t\tconst sleep = ms => new Promise(r => setTimeout(r, ms));\n" +
		teamsAuthExtractJS + `
		try {
			const resp = await fetchWithAuth(baseURL + '/api/chatsvc/amer/v1/users/ME/properties', {
				method: 'GET',
				headers: { 'behavioroverride': 'redirectAs404', 'x-ms-migration': 'True' },
			});
			return JSON.stringify({ status: resp.status });
		} catch (e) {
			return JSON.stringify({ status: -1, error: String(e) });
		}
	}`
	res, err := page.Timeout(30 * time.Second).Eval(js)
	if err != nil || res == nil {
		return -1
	}
	var out struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal([]byte(res.Value.String()), &out) != nil {
		return -1
	}
	if out.Error != "" && out.Status == 0 {
		return 0 // "Bearer token não encontrado"
	}
	return out.Status
}

// ensureTeamsAuth garante que o Teams aceita a autenticação antes de enviar/apagar. Se a sessão
// expirou, traz a janela do Chrome para a frente, avisa o usuário (onNotice) e espera o login por
// até teamsAuthWaitTimeout, tentando também o login SSO automático. Antes, a ferramenta
// minimizava a janela e seguia em frente: o Teams pedia novo login por cima da barra lateral, e o
// usuário só via "401 Authentication failed" em todos os destinatários, sem saber o motivo.
func ensureTeamsAuth(ctx context.Context, page *rod.Page, sessionDir string, logger *zerolog.Logger, onNotice NoticeFunc) error {
	st := probeTeamsAuth(page)
	if st != 401 && st != 0 {
		return nil
	}
	msg := "A sessão do Teams expirou: faça login na janela do Chrome do Teams (aberta no servidor). O envio continua sozinho assim que o login for concluído."
	logger.Warn().Int("status", st).Msg("[Sender] Sessão do Teams sem autenticação válida — aguardando login do usuário")
	restoreWindow(page, logger)
	if onNotice != nil {
		onNotice(msg)
	}
	deadline := time.Now().Add(teamsAuthWaitTimeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			SetTeamsDockerMFANumber("")
			return ErrCancelled
		}
		browser.AttemptSSOAutoLogin(page, sessionDir, logger)
		browser.SubmitMCASHiddenForm(page)
		detectMFANumber(page, logger)
		time.Sleep(3 * time.Second)
		if st = probeTeamsAuth(page); st != 401 && st != 0 {
			logger.Info().Msg("[Sender] Login do Teams concluído — retomando")
			SetTeamsDockerMFANumber("")
			return nil
		}
	}
	SetTeamsDockerMFANumber("")
	return fmt.Errorf("sessão do Teams expirada: o login não foi concluído em %d min — faça login na janela do Chrome do Teams e tente de novo", int(teamsAuthWaitTimeout.Minutes()))
}

// armCancel zera a bandeira de cancelamento da página e passa a observá-la: quando ctx é cancelado,
// liga window.__bcCancel, que os JS de envio/edição/exclusão checam antes de cada lote. Devolve a
// função que encerra o observador (chamar com defer).
func armCancel(ctx context.Context, page *rod.Page, logger *zerolog.Logger) func() {
	_, _ = page.Timeout(10 * time.Second).Eval(`() => { window.__bcCancel = false; }`)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if _, err := page.Timeout(10 * time.Second).Eval(`() => { window.__bcCancel = true; }`); err != nil {
				logger.Warn().Err(err).Msg("[Sender] Falha ao sinalizar o cancelamento na página do Teams")
			} else {
				logger.Info().Msg("[Sender] Cancelamento sinalizado: os lotes restantes não serão executados")
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}
