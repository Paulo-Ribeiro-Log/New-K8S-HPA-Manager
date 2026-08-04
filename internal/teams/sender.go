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
}

// ProgressFunc é chamada a cada destinatário concluído (sucesso ou falha).
type ProgressFunc func(result SendResult)

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

	logger.Info().Msg("[Sender] Aguardando Teams carregar (máx 3min)...")
	var page *rod.Page
	deadline := time.Now().Add(3 * time.Minute)
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
				logger.Info().Str("url", u).Msg("[Sender] Teams detectado")
				goto loaded
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("timeout aguardando Teams carregar")

loaded:
	logger.Info().Msg("[Sender] Aguardando Teams inicializar (20s)...")
	time.Sleep(20 * time.Second)

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

	js := fmt.Sprintf(`async () => {
		const sleep = ms => new Promise(r => setTimeout(r, ms));

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
					if (!resp.ok) {
						try { errMsg = (await resp.text()).slice(0, 200); } catch {}
					}
					result = { thread_id: threadId, ok: resp.ok, status: resp.status, error: errMsg, index: globalIdx, total };
				} catch(e) {
					result = { thread_id: threadId, ok: false, status: 0, error: String(e), index: globalIdx, total };
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

// escapeJSString escapa uma string para ser embutida com segurança em template JS
// delimitado por aspas simples.
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return s
}
