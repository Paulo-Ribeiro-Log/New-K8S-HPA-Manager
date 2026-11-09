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
)

// EditTarget identifica uma mensagem já enviada a editar: a thread e o MessageID real
// (SendResult.MessageID), o mesmo identificador usado para apagar.
type EditTarget struct {
	ThreadID  string `json:"thread_id"`
	MessageID string `json:"message_id"`
}

// EditResult é o resultado de editar uma mensagem.
type EditResult struct {
	ThreadID  string `json:"thread_id"`
	MessageID string `json:"message_id"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`
	Error     string `json:"error,omitempty"`
}

// EditMessages substitui o conteúdo de mensagens já enviadas (só as da própria conta do Teams do
// servidor). Mesmo transporte do envio/exclusão (same-origin na página do Teams, Bearer do cache),
// com PUT em .../conversations/{thread}/messages/{messageId} e o corpo no formato do envio.
//
// O formato exato do corpo da edição NÃO foi capturado de um cliente real: por isso a PRIMEIRA
// mensagem é editada sozinha e, se o Teams recusar, as demais nem são tentadas (marcadas como "não
// tentado") — evita falhar em massa, e o erro devolvido (até 400 caracteres) mostra o que o Teams
// espera. Se algum dia precisar reconfirmar, capture uma edição manual com scripts/teams-spy-send
// e compare com o corpo montado abaixo.
func EditMessages(ctx context.Context, sessionDir string, targets []EditTarget, htmlContent string, onNotice NoticeFunc, logger *zerolog.Logger) ([]EditResult, error) {
	// Serializa contra os demais fluxos do mesmo perfil de Chrome (ver operationMu).
	operationMu.Lock()
	defer operationMu.Unlock()

	page, closePage, err := openAuthenticatedTeamsPage(ctx, sessionDir, logger, onNotice)
	if err != nil {
		return nil, err
	}
	defer closePage()
	page = page.Timeout(10 * time.Minute)

	// Imagens embutidas: mesmo upload do envio (AMS), compartilhado entre os destinatários.
	threadIDs := make([]string, len(targets))
	for i, t := range targets {
		threadIDs[i] = t.ThreadID
	}
	htmlContent = uploadImagesInHTML(page, htmlContent, threadIDs, logger)

	targetsJSON, _ := json.Marshal(targets)
	js := "async () => {\n\t\tconst sleep = ms => new Promise(r => setTimeout(r, ms));\n" +
		teamsAuthExtractJS +
		fmt.Sprintf(`
		// ── 4. Editar: o primeiro sozinho (valida o formato), depois o restante em lotes ──
		const targets     = %s;
		const htmlContent = '%s';
		const batchSize   = 10;

		const editOne = async (t) => {
			const now = new Date().toISOString();
			const encodedThread = encodeURIComponent(t.thread_id);
			const encodedMsg    = encodeURIComponent(t.message_id);
			const convURL = baseURL + '/api/chatsvc/amer/v1/users/ME/conversations/' + encodedThread;
			const body = JSON.stringify({
				amsreferences: [], content: htmlContent, contenttype: 'Text',
				conversationLink: convURL, conversationid: t.thread_id, crossPostChannels: [],
				from: userMRI, fromUserId: userMRI, id: t.message_id,
				imdisplayname: displayName, messagetype: 'RichText/Html',
				composetime: now, originalarrivaltime: now,
				properties: { cards:'[]', files:'[]', formatVariant:'TEAMS',
					importance:'', links:'[]', mentions:'[]',
					onbehalfof:null, policyViolation:null, subject:'', title:'' },
				type: 'Message', version: '0',
			});
			try {
				const resp = await fetchWithAuth(convURL + '/messages/' + encodedMsg, {
					method: 'PUT',
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
				if (!resp.ok) { try { errMsg = (await resp.text()).slice(0, 400); } catch {} }
				return { thread_id: t.thread_id, message_id: t.message_id, ok: resp.ok, status: resp.status, error: errMsg };
			} catch (e) {
				return { thread_id: t.thread_id, message_id: t.message_id, ok: false, status: 0, error: String(e) };
			}
		};

		const results = [];
		if (targets.length > 0) {
			const first = await editOne(targets[0]);
			results.push(first);
			const rest = targets.slice(1);
			if (!first.ok) {
				for (const t of rest) {
					results.push({ thread_id: t.thread_id, message_id: t.message_id, ok: false, status: 0,
						error: 'não tentado: a primeira edição falhou (HTTP ' + first.status + ')' });
				}
			} else {
				for (let i = 0; i < rest.length; i += batchSize) {
					if (window.__bcCancel) {
						for (const t of rest.slice(i)) {
							results.push({ thread_id: t.thread_id, message_id: t.message_id, ok: false, status: 0, error: 'cancelado' });
						}
						break;
					}
					const batchResults = await Promise.all(rest.slice(i, i + batchSize).map(editOne));
					results.push(...batchResults);
					if (i + batchSize < rest.length) await sleep(600);
				}
			}
		}
		return JSON.stringify({ results });
	}`, string(targetsJSON), escapeJSString(htmlContent))

	if ctx.Err() != nil {
		return nil, ErrCancelled
	}
	logger.Info().Int("targets", len(targets)).Msg("[Sender] Editando mensagens...")
	stopCancel := armCancel(ctx, page, logger)
	defer stopCancel()
	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("erro ao executar editar JS: %w", err)
	}

	var out struct {
		Error   string       `json:"error"`
		Results []EditResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return nil, fmt.Errorf("erro ao parsear resultado: %w (raw: %.200s)", err, res.Value.String())
	}
	if out.Error != "" {
		return nil, fmt.Errorf("erro no JS de editar: %s", out.Error)
	}

	ok := 0
	for i := range out.Results {
		r := &out.Results[i]
		if r.OK {
			ok++
		} else if r.Status == 401 {
			r.Error = "Sessão do Teams expirada ou sem permissão (401): faça login na janela do Chrome do Teams e tente de novo. Detalhe: " + r.Error
		}
	}
	logger.Info().Int("edited", ok).Int("failed", len(out.Results)-ok).Msg("[Sender] Edição concluída")
	for _, r := range out.Results {
		if !r.OK {
			msg := r.Error
			if len(msg) > 300 {
				msg = msg[:300]
			}
			logger.Warn().Int("status", r.Status).Str("erro", msg).Msg("[Sender] Falha ao editar")
			break // um exemplo basta; o formato do erro costuma ser o mesmo para todos
		}
	}
	return out.Results, nil
}

// openAuthenticatedTeamsPage abre o Teams no Chrome persistente, espera a UI renderizar e garante
// a autenticação (ensureTeamsAuth), minimizando a janela no fim. closePage fecha a aba criada.
func openAuthenticatedTeamsPage(ctx context.Context, sessionDir string, logger *zerolog.Logger, onNotice NoticeFunc) (*rod.Page, func(), error) {
	br, err := getBrowser(sessionDir, logger)
	if err != nil {
		return nil, nil, err
	}
	created, err := br.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao criar página: %w", err)
	}
	closePage := func() { _ = created.Close() }

	restoreWindow(created, logger)

	logger.Info().Msg("[Sender] Aguardando Teams carregar (máx 3min)...")
	var page *rod.Page
	deadline := time.Now().Add(3 * time.Minute)
	for page == nil && time.Now().Before(deadline) {
		pages, _ := br.Pages()
		for _, p := range pages {
			info, _ := p.Info()
			if info == nil {
				continue
			}
			u := info.URL
			if isTeamsURL(u) && !strings.Contains(u, "/error") &&
				!strings.Contains(u, "login.microsoftonline") && u != "about:blank" {
				page = p
				break
			}
		}
		if page == nil {
			time.Sleep(2 * time.Second)
		}
	}
	if page == nil {
		logger.Warn().Str("abas", pageURLsSummary(br)).Msg("[Sender] Nenhuma aba do Teams reconhecida antes do timeout")
		closePage()
		return nil, nil, fmt.Errorf("timeout aguardando Teams carregar")
	}

	logger.Info().Msg("[Sender] Aguardando Teams inicializar e autenticar (máx 4min)...")
	if !waitForTeamsUIReady(page, sessionDir, logger, "[Sender]") {
		closePage()
		return nil, nil, fmt.Errorf("teams não terminou de carregar/autenticar a tempo — se uma tela de login apareceu, complete-a e tente novamente")
	}
	if err := ensureTeamsAuth(ctx, page, sessionDir, logger, onNotice); err != nil {
		closePage()
		return nil, nil, err
	}
	minimizeWindow(page, logger)
	return page, closePage, nil
}
