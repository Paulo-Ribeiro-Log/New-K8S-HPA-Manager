package teams

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// SendResult representa o resultado do envio para um destinatário.
type SendResult struct {
	ThreadID string `json:"thread_id"`
	OK       bool   `json:"ok"`
	Status   int    `json:"status"`
	Error    string `json:"error,omitempty"`
}

// SendBatch abre o Teams via go-rod e envia htmlContent para cada threadID em lote.
// Todo o tráfego HTTP passa pela página do Teams (same-origin) para contornar o MCAS.
func SendBatch(sessionDir string, threadIDs []string, htmlContent string, logger *zerolog.Logger) ([]SendResult, error) {
	killExistingChrome(sessionDir, logger)
	time.Sleep(800 * time.Millisecond)

	chromeBin := findSystemChrome()
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
		logger.Info().Str("bin", chromeBin).Msg("[Sender] Chrome encontrado")
	}

	ctrlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar Chrome: %w", err)
	}

	browser := rod.New().ControlURL(ctrlURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar ao browser: %w", err)
	}
	defer browser.Close()

	_, err = browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %w", err)
	}

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

	// Escapar o htmlContent para uso seguro dentro de um template JS.
	escapedHTML := escapeJSString(htmlContent)

	// Serializar thread IDs como array JSON.
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

			// Chave MSAL contém "accesstoken" e "teams.officeclient" ou "ic3.teams"
			if (key.toLowerCase().includes('accesstoken') &&
				(key.toLowerCase().includes('ic3.teams') ||
				 key.toLowerCase().includes('teams.officeclient') ||
				 key.toLowerCase().includes('teams.communication'))) {
				try {
					const obj = JSON.parse(val);
					const token = obj.secret || obj.access_token || obj.token || '';
					if (token && token.length > 50) {
						bearerToken = token;
					}
				} catch {}
			}
			// MRI + displayName: chave MSAL "idtoken" ou "account"
			if (!userMRI && key.toLowerCase().includes('account')) {
				try {
					const obj = JSON.parse(val);
					const mri = obj.localAccountId || obj.homeAccountId || '';
					// Formato MRI esperado: 8:orgid:UUID
					if (mri && !mri.includes('login.windows') && !mri.includes('.')) {
						userMRI = '8:orgid:' + (obj.localAccountId || '');
					}
					if (obj.name && !displayName) displayName = obj.name;
				} catch {}
			}
		}

		// Fallback: varrer todas as chaves do localStorage procurando tokens Bearer
		if (!bearerToken) {
			for (let i = 0; i < localStorage.length; i++) {
				const key = localStorage.key(i);
				if (!key) continue;
				const val = localStorage.getItem(key);
				if (!val) continue;
				try {
					const obj = JSON.parse(val);
					const candidates = [obj.secret, obj.access_token, obj.token, obj.skypeToken, obj.skypetoken];
					for (const c of candidates) {
						if (typeof c === 'string' && c.length > 100 && (c.startsWith('eyJ') || c.startsWith('Bearer'))) {
							bearerToken = c.replace(/^Bearer\s+/i, '');
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

		// ── 2. Extrair MRI e displayName do IndexedDB se ainda não temos ──────
		if (!userMRI || !displayName) {
			try {
				const allDbs = await indexedDB.databases().catch(() => []);
				for (const {name} of allDbs) {
					if (!name || !name.includes('conversation-manager')) continue;
					const db = await new Promise((res, rej) => {
						const r = indexedDB.open(name);
						r.onsuccess = e => res(e.target.result);
						r.onerror   = () => rej(r.error);
					});
					// Tentar store "userInfo" ou "profile"
					for (const store of ['userInfo', 'profile', 'profiles', 'users']) {
						if (!db.objectStoreNames.contains(store)) continue;
						const rows = await new Promise(res => {
							const tx  = db.transaction(store, 'readonly');
							const req = tx.objectStore(store).getAll();
							req.onsuccess = () => res(req.result || []);
							req.onerror   = () => res([]);
						});
						for (const row of rows) {
							const mri = row.mri || row.MRI || row.id || '';
							if (mri && (mri.startsWith('8:orgid:') || mri.startsWith('8:live:'))) {
								if (!userMRI) userMRI = mri;
							}
							const dn = row.displayName || row.name || row.imdisplayname || '';
							if (dn && !displayName) displayName = dn;
						}
						if (userMRI && displayName) break;
					}
					db.close();
					if (userMRI && displayName) break;
				}
			} catch {}
		}

		// Fallback MRI: extrair do token JWT (sub ou upn)
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

		// ── 3. Detectar host base (MCAS proxy ou direto) ──────────────────────
		const currentHost = location.host; // ex: teams.microsoft.com.mcas.ms
		const baseURL = location.protocol + '//' + currentHost;

		// ── 4. Enviar em lote (grupos de 10 com 600ms entre grupos) ──────────
		const threadIDs   = %s;
		const htmlContent = '%s';
		const batchSize   = 10;

		const results = [];
		for (let i = 0; i < threadIDs.length; i += batchSize) {
			const batch = threadIDs.slice(i, i + batchSize);
			const batchResults = await Promise.all(batch.map(async (threadId) => {
				const now   = new Date().toISOString();
				// Teams valida que clientmessageid seja um número em formato string (max int64 = 19 dígitos).
				// Date.now() tem 13 dígitos; concatenamos 6 dígitos aleatórios para chegar em 19.
				const msgId = String(Date.now()) + String(Math.floor(Math.random() * 1000000)).padStart(6, '0');

				// URL: encode o threadId (contém : e @ que precisam ser encoded)
				const encodedThread = encodeURIComponent(threadId);
				const url = baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread + '/messages';

				const body = JSON.stringify({
					amsreferences:      [],
					callId:             '',
					clientmessageid:    msgId,
					composetime:        now,
					content:            htmlContent,
					contenttype:        'Text',
					conversationLink:   baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread,
					conversationid:     threadId,
					crossPostChannels:  [],
					from:               userMRI,
					fromUserId:         userMRI,
					id:                 '-1',
					imdisplayname:      displayName,
					messagetype:        'RichText/Html',
					originalarrivaltime: now,
					properties: {
						cards:          '[]',
						files:          '[]',
						formatVariant:  'TEAMS',
						importance:     '',
						links:          '[]',
						mentions:       '[]',
						onbehalfof:     null,
						policyViolation: null,
						subject:        '',
						title:          '',
					},
					state:   0,
					type:    'Message',
					version: '0',
				});

				try {
					const resp = await fetch(url, {
						method: 'POST',
						headers: {
							'authorization':        'Bearer ' + bearerToken,
							'content-type':         'application/json',
							'behavioroverride':     'redirectAs404',
							'x-ms-migration':       'True',
							'x-ms-request-priority': '0',
							'x-ms-test-user':       'False',
						},
						body,
					});
					const status = resp.status;
					let errMsg = '';
					if (!resp.ok) {
						try { errMsg = await resp.text(); } catch {}
						errMsg = errMsg.slice(0, 200);
					}
					return { thread_id: threadId, ok: resp.ok, status, error: errMsg };
				} catch(e) {
					return { thread_id: threadId, ok: false, status: 0, error: String(e) };
				}
			}));
			results.push(...batchResults);
			if (i + batchSize < threadIDs.length) {
				await sleep(600);
			}
		}

		return JSON.stringify({ results, mri: userMRI, display_name: displayName });
	}`, string(threadIDsJSON), escapedHTML)

	logger.Info().Int("recipients", len(threadIDs)).Msg("[Sender] Iniciando envio em lote...")

	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar envio JS: %w", err)
	}

	raw := res.Value.String()

	var out struct {
		Error       string       `json:"error"`
		Results     []SendResult `json:"results"`
		MRI         string       `json:"mri"`
		DisplayName string       `json:"display_name"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("erro ao parsear resultado do envio: %w (raw: %.200s)", err, raw)
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
	logger.Info().
		Str("mri", out.MRI).
		Str("display_name", out.DisplayName).
		Int("sent", ok).
		Int("failed", len(out.Results)-ok).
		Msg("[Sender] Envio concluído")

	return out.Results, nil
}

// escapeJSString escapa uma string para ser embutida com segurança em um template JS
// delimitado por aspas simples.
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return s
}
