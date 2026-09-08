package teams

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"

	// Alias ssobrowser: mesmo motivo do alias em discover.go — evita colisão com o identificador
	// "browser" já usado dentro de fetchMessageAttempt pra nomear o *rod.Browser.
	ssobrowser "k8s-hpa-manager/internal/browser"
)

// teamsMessageFetchTimeout — teto do loop de scroll/busca pela mensagem alvo. Mensagens antigas
// exigem várias rodadas de scroll pra carregar o histórico (mesmo mecanismo lazy-load do
// RunDiscovery) — 90s cobre a maioria dos casos sem travar indefinidamente se a mensagem não
// existir mais ou o usuário não tiver acesso à conversa.
const teamsMessageFetchTimeout = 90 * time.Second

// teamsMessageMatchToleranceMs — o ID de mensagem no link "Copiar link" do Teams é o epoch-ms de
// composição/chegada (mesma convenção usada por SendBatch ao gerar clientmessageid) — não há API
// pra buscar por ID direto (MCAS bloqueia o chatsvcagg, mesma limitação documentada em
// RunDiscovery), então a mensagem é localizada por proximidade entre esse timestamp e o
// atributo <time datetime> renderizado no DOM. 5s de tolerância absorve o clock skew típico entre
// o "composetime" do cliente que gerou o link e o "arrival time" exibido pelo Teams de quem lê.
const teamsMessageMatchToleranceMs = 5000

var teamsMessageLinkRe = regexp.MustCompile(`teams\.microsoft\.com/l/message/([^/?]+)/(\d+)`)

// FetchedMessage é o resultado de FetchMessageByLink.
type FetchedMessage struct {
	ThreadID    string     `json:"thread_id"`
	MessageID   string     `json:"message_id"`
	Text        string     `json:"text"`
	PostedAt    *time.Time `json:"posted_at,omitempty"`
	Approximate bool       `json:"approximate"` // true quando nenhuma mensagem bateu dentro da tolerância — retornado o candidato mais próximo encontrado
	// ImagesSaved — quantas imagens (TODAS as <img> da mensagem, exceto avatar do remetente —
	// ver isAvatarImg no extractJS) foram baixadas com sucesso pra pasta temporária (ver
	// image_temp_store.go) e referenciadas em Text como "![imagem](teams-temp:<filename>)". Zero
	// não significa necessariamente que a mensagem não tinha imagem — pode ter falhado o download
	// (best-effort, ver resolveMessageImages).
	ImagesSaved int `json:"images_saved,omitempty"`
	// ImagesDetected — quantas <img> foram ENCONTRADAS no DOM (excluindo avatar) antes de sequer
	// tentar baixar. ImagesDetected > 0 && ImagesSaved == 0 aponta pra falha no DOWNLOAD
	// (provável CORS/autenticação no domínio de mídia do Teams); ImagesDetected == 0 aponta pra
	// falha na DETECÇÃO (ver ImgTagsSeen abaixo).
	ImagesDetected int `json:"images_detected,omitempty"`
	// ImgTagsSeen — diagnóstico de detecção: quantas tags <img> (incluindo avatar excluído)
	// existiam no elemento da mensagem escolhida. ImgTagsSeen == 0 indica que a imagem não é uma
	// <img> — layout do Teams diferente do esperado, ou a mensagem escolhida não é a certa.
	ImgTagsSeen int `json:"img_tags_seen,omitempty"`
	// DebugDumpPath — caminho do arquivo com o HTML real do elemento de mensagem, salvo só quando
	// a requisição pediu debug=true (ver saveMessageImageDebugDump). Vazio no caminho normal.
	DebugDumpPath string `json:"debug_dump_path,omitempty"`
	// ImgCandidates — só populado quando debug=true: um registro por <img> encontrada na mensagem
	// (avatar incluso, com ExcludedAsAvatar=true), com os atributos reais que decidiram se ela
	// virou uma imagem baixada ou foi excluída. Existe pra permitir diagnosticar precisamente
	// qual <img> foi capturada sem precisar abrir o dump de HTML completo (DebugDumpPath).
	ImgCandidates []ImgCandidateDebug `json:"img_candidates,omitempty"`
}

// ImgCandidateDebug — um <img> candidato encontrado durante a extração, com os atributos reais
// que decidiram inclusão/exclusão (ver isAvatarImg no extractJS). Só populado em FetchedMessage
// quando a requisição pediu debug=true.
type ImgCandidateDebug struct {
	// Src — a URL efetivamente ESCOLHIDA pra download (ver pickImageSrc no extractJS): prioriza
	// data-src/data-original/srcset (lazy-load JS-based) sobre o "src" bruto.
	Src              string `json:"src"`
	RawSrcAttr       string `json:"raw_src_attr"` // valor cru do atributo "src" — comparar com Src ajuda a confirmar se havia lazy-load
	DataSrc          string `json:"data_src"`
	Srcset           string `json:"srcset"`
	Width            string `json:"width"`
	Height           string `json:"height"`
	Alt              string `json:"alt"`
	ClassName        string `json:"class_name"`
	DataTid          string `json:"data_tid"`
	ExcludedAsAvatar bool   `json:"excluded_as_avatar"`
}

// toTeamsHashRoute converte um link "Copiar link" no formato path
// (https://teams.microsoft.com/l/message/...) pra sua rota interna via hash
// (https://teams.microsoft.com/_#/l/message/...).
//
// Bug real corrigido (relatado ao vivo: "está sendo feita uma pergunta na página do Teams, se
// quero usar uma solução de aplicativo baixado ou página web — isso não existia antes"): o
// formato path é a URL "universal" pensada pra decisão de protocolo do SO (abrir no app
// instalado ou no navegador) — ela sempre passa primeiro por uma página intermediária
// (teams.microsoft.com/dl/launcher/launcher.html) que pergunta exatamente isso, exigindo clique
// manual do usuário; nossa navegação automatizada ficava parada esperando essa pergunta, sem
// ninguém pra responder. Confirmado inspecionando a URL real capturada do launcher num log
// anterior desta mesma investigação: o parâmetro `url` da própria launcher.html contém, urlencoded,
// exatamente `/_#/l/message/<threadId>/<messageId>?context=...` — ou seja, essa é a rota INTERNA
// que o client web usa pro mesmo conteúdo, sem passar pela decisão de app/navegador. Navegando
// direto pra essa forma (com o `_#` já na URL inicial) pula a página intermediária por completo
// na maioria dos casos — mais simples e direto do que tentar detectar e clicar no botão "usar a
// versão web" da página intermediária (mantido como rede de segurança abaixo mesmo assim, ver
// dismissAppLauncherPrompt, caso a página apareça por algum outro motivo).
func toTeamsHashRoute(link string) string {
	const marker = "teams.microsoft.com/l/"
	idx := strings.Index(link, marker)
	if idx == -1 {
		return link
	}
	prefixEnd := idx + len("teams.microsoft.com/")
	return link[:prefixEnd] + "_#/" + link[prefixEnd:]
}

// ParseTeamsMessageLink extrai threadID e messageID de um link "Copiar link" de mensagem do
// Teams, ex: https://teams.microsoft.com/l/message/19:xxx@thread.v2/1786982367908?context=...
func ParseTeamsMessageLink(link string) (threadID, messageID string, err error) {
	m := teamsMessageLinkRe.FindStringSubmatch(link)
	if m == nil {
		return "", "", fmt.Errorf("link não reconhecido — formato esperado: https://teams.microsoft.com/l/message/<threadId>/<messageId>")
	}
	threadID, err = url.PathUnescape(m[1])
	if err != nil {
		return "", "", fmt.Errorf("threadId inválido no link: %w", err)
	}
	return threadID, m[2], nil
}

// FetchMessageByLink abre o Teams (Chrome persistente, ver getBrowser em browser_manager.go) e
// localiza a mensagem do link por proximidade de timestamp. Serializado contra
// RunDiscovery/ScanConversations/SendBatch via operationMu — abas paralelas no mesmo perfil podem
// invalidar o SkypeToken.
//
// Duas tentativas, nessa ordem:
//  1. Navegação DIRETA pro link original (URL completa, com o parâmetro `context` intacto) — é o
//     mecanismo oficial do Teams pra esse tipo de link (o mesmo que roda quando alguém clica em
//     "Copiar link" colado em qualquer lugar): o próprio app deveria abrir direto na conversa E na
//     mensagem certas, sem precisar simular navegação nenhuma.
//  2. Fallback: abre `/v2/` normalmente e localiza a conversa clicando na barra lateral (mecanismo
//     mais antigo desta função, mantido como rede de segurança pro caso da Tentativa 1 não
//     funcionar — ex: algum tipo de conversa sem suporte ao deep-link direto).
//
// Motivo de ter as duas: testado ao vivo que a Tentativa 2 sozinha navega corretamente até a
// conversa certa (clique confirmado na barra lateral), mas contra uma thread de reunião
// (`19:meeting_...@thread.v2`) a extração de mensagens deu zero resultados em 59 rodadas de
// scroll — sinal de que aquela navegação (hash + clique na conversa) pode abrir uma aba/visão
// diferente da aba "Chat" da reunião (ex: "Detalhes"), enquanto a URL original já carrega o
// parâmetro `context: {"contextType":"chat"}` que sinaliza pro próprio Teams abrir direto na aba
// de chat — daí a Tentativa 1 ser a primária agora.
// FetchMessageByLink — debug, quando true, faz a extração incluir um dump do HTML real do
// elemento de mensagem localizado (ver saveMessageImageDebugDump), pra investigar por que uma
// imagem esperada não foi detectada. Nunca ligado por padrão (custo de serializar HTML potencial-
// mente grande em toda extração comum) — ver checkbox "Modo diagnóstico" no LoadFromLinkModal.
func FetchMessageByLink(sessionDir, link string, debug bool, logger *zerolog.Logger) (*FetchedMessage, error) {
	threadID, messageID, err := ParseTeamsMessageLink(link)
	if err != nil {
		return nil, err
	}

	msgIDMs, err := strconv.ParseInt(messageID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("id de mensagem inválido no link: %w", err)
	}
	targetTime := time.UnixMilli(msgIDMs)

	// Destrói qualquer imagem salva por uma coleta ANTERIOR antes de começar esta — pedido
	// explícito do usuário ("destruída na próxima coleta"). Roda no início da chamada pública
	// (não em fetchMessageAttempt, chamada até 2x por aqui — tentativa direta + fallback — o que
	// resetaria a pasta no meio de uma mesma coleta e perderia o resultado da 1ª tentativa se a
	// 2ª também encontrar imagem). Best-effort: uma falha aqui não deveria impedir a extração de
	// texto em si, só o reaproveitamento das imagens desta coleta.
	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
		if _, resetErr := ResetTeamsBroadcastImagesDir(homeDir); resetErr != nil {
			logger.Warn().Err(resetErr).Msg("[MessageFetch] Falha ao limpar pasta temporária de imagens da coleta anterior")
		}
	} else {
		logger.Warn().Err(homeErr).Msg("[MessageFetch] Não foi possível resolver $HOME — pasta temporária de imagens não será limpa")
	}

	operationMu.Lock()
	defer operationMu.Unlock()

	browser, err := getBrowser(sessionDir, logger)
	if err != nil {
		return nil, err
	}

	result, directErr := fetchMessageAttempt(browser, sessionDir, threadID, messageID, targetTime, link, debug, logger)
	if directErr == nil {
		return result, nil
	}
	logger.Warn().Err(directErr).Str("thread", threadID).
		Msg("[MessageFetch] Tentativa via link direto falhou — tentando navegar por dentro do app (barra lateral)")

	result, fallbackErr := fetchMessageAttempt(browser, sessionDir, threadID, messageID, targetTime, "", debug, logger)
	if fallbackErr == nil {
		return result, nil
	}
	return nil, fmt.Errorf("%w (link direto também falhou: %v)", fallbackErr, directErr)
}

// fetchMessageAttempt executa uma tentativa completa (aba própria, fechada ao final) de localizar
// a mensagem. Se `directLink` não for vazio, a aba já nasce navegada pra URL completa do link
// (deep-link oficial do Teams); se vazio, nasce em `/v2/` e localiza a conversa clicando na barra
// lateral (mecanismo de fallback, ver comentário de FetchMessageByLink).
func fetchMessageAttempt(browser *rod.Browser, sessionDir, threadID, messageID string, targetTime time.Time, directLink string, debug bool, logger *zerolog.Logger) (*FetchedMessage, error) {
	mode := "direct-link"
	initialURL := directLink
	if directLink == "" {
		mode = "sidebar-click"
		initialURL = "https://teams.microsoft.com/v2/"
	} else {
		initialURL = toTeamsHashRoute(directLink)
	}

	page, err := browser.Page(proto.TargetCreateTarget{URL: initialURL})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %w", err)
	}
	createdPage := page
	defer createdPage.Close() //nolint:errcheck

	restoreWindow(page, logger)

	// Campo "message_id" (não "message" — colidiria com a chave que o zerolog já usa pro texto do
	// próprio log via .Msg(), duplicando a chave no JSON de saída).
	logger.Info().Str("thread", threadID).Str("message_id", messageID).Str("mode", mode).
		Msg("[MessageFetch] Aguardando Teams carregar (máx 3min)...")
	loaded := false
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		pages, _ := browser.Pages()
		for _, p := range pages {
			info, err := p.Info()
			if err != nil || info == nil {
				continue
			}
			u := info.URL
			if strings.Contains(u, "teams.microsoft.com") &&
				!strings.Contains(u, "/error") &&
				!strings.Contains(u, "login.microsoftonline") &&
				u != "about:blank" {
				page = p
				loaded = true
				logger.Info().Str("url", u).Msg("[MessageFetch] Teams detectado")
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

	// Bug real corrigido (relatado pelo usuário ao vivo: a navegação por hash nunca saía do chat
	// do Mr.ViaBot, e o login parecia "não persistir" como nas outras ferramentas de Teams desta
	// app). Duas causas, mesma raiz: a versão original minimizava a janela e disparava o hash-nav
	// assim que a URL batia com teams.microsoft.com — cedo demais. (1) O app React do Teams ainda
	// não tinha terminado de inicializar/anexar seu próprio router nesse instante, então o
	// `window.location.hash = ...` era ignorado e o app, ao terminar de montar, restaurava a
	// última conversa ativa da sessão (o Mr.ViaBot, de longe o chat mais usado por esta app) — não
	// há nada de especial no Mr.ViaBot em si, é só o efeito colateral de navegar cedo demais.
	// (2) Se a sessão salva tivesse expirado, a tela de login/MFA aparecia dentro do mesmo domínio
	// teams.microsoft.com (o mesmo caso documentado em RunDiscovery) e minimizeWindow() escondia
	// essa tela da cara do usuário antes de qualquer chance de completar o login — o usuário nunca
	// via o prompt, só via "não persiste o login". RunDiscovery já resolve isso esperando o
	// SkypeToken antes de minimizar; aqui não precisamos do SkypeToken (não fazemos fetch
	// autenticado), mas precisamos do mesmo tipo de sinal — esperar a UI do Teams (barra lateral
	// de conversas) realmente renderizar, o que só acontece depois do app terminar de montar E do
	// usuário estar de fato autenticado. Vale pros dois modos (link direto e /v2/) — a barra
	// lateral faz parte do shell principal do Teams em ambos os casos.
	logger.Info().Msg("[MessageFetch] Aguardando Teams inicializar e autenticar (máx 4min)...")
	appReady := false
	readyDeadline := time.Now().Add(teamsSkypeTokenWaitTimeout)
	for time.Now().Before(readyDeadline) {
		// Mesmo auto-login/bypass MCAS/detecção de MFA usados por RunDiscovery — cobre o caso de
		// uma sessão expirada exigindo login manual/SSO enquanto esperamos a UI ficar pronta.
		ssobrowser.AttemptSSOAutoLogin(page, sessionDir, logger)
		ssobrowser.SubmitMCASHiddenForm(page)
		detectMFANumber(page, logger)
		// Rede de segurança pra página intermediária "abrir no app ou continuar no navegador"
		// (ver comentário de toTeamsHashRoute acima) — a navegação já pula essa página na maioria
		// dos casos, mas se aparecer mesmo assim (ex: Teams mudar o comportamento, ou algum outro
		// ponto de entrada futuro que não passe por toTeamsHashRoute), clica automaticamente na
		// opção de continuar na versão web em vez de travar esperando alguém responder.
		if dismissAppLauncherPrompt(page, logger) {
			logger.Info().Msg("[MessageFetch] Prompt \"abrir no app ou navegador\" dispensado automaticamente")
		}

		readyRes, readyErr := page.Eval(`() => !!document.querySelector('[data-tid="simple-collab-dnd-rail"]')`)
		if readyErr == nil && !readyRes.Value.Nil() && readyRes.Value.Bool() {
			appReady = true
			logger.Info().Msg("[MessageFetch] UI do Teams pronta (barra lateral renderizada)")
			break
		}
		time.Sleep(2 * time.Second)
	}
	SetTeamsDockerMFANumber("")
	if !appReady {
		// Não minimiza (janela permanece visível — se um login/MFA estava pendente, o usuário
		// ainda consegue vê-lo) e desiste aqui em vez de seguir pra busca: sem a UI confirmada
		// pronta, a navegação abaixo tende a falhar e o erro resultante ("mensagem não
		// encontrada") seria enganoso — esconderia a causa real (login pendente).
		return nil, fmt.Errorf("teams não terminou de carregar/autenticar a tempo — se uma tela de login apareceu, complete-a e tente novamente")
	}

	// Daqui em diante é tudo via CDP/JS (scroll, DOM) — sem necessidade de interação do usuário.
	// Ver comentário de minimizeWindow em browser_manager.go.
	minimizeWindow(page, logger)

	page = page.Timeout(3 * time.Minute)

	if directLink != "" {
		// Modo link direto: a própria navegação inicial (URL completa, com o parâmetro `context`
		// intacto) já é o mecanismo oficial do Teams pra abrir na conversa/mensagem certas — só
		// aguarda um instante pra o app terminar de rolar/destacar a mensagem alvo sozinho.
		time.Sleep(3 * time.Second)
	} else {
		// Bug real corrigido — segunda rodada (relatado ao vivo: mesmo depois de esperar a UI
		// ficar pronta acima, a navegação por hash continuava sem sair do Mr.ViaBot; log
		// confirmou "UI pronta" e, ainda assim, "hash não confirmado na URL" logo em seguida). O
		// próprio RunDiscovery — de onde esse hash-nav foi copiado — nunca dependeu só dele:
		// comentário lá mesmo já é "Se o hash não abriu a conversa, tentar clicar no item da
		// lista de chats", e o clique (não o hash) é o mecanismo confirmado ao vivo contra a Tree
		// do Fluent UI v9 real. O hash-nav sozinho é best-effort/inconsistente — generalizado
		// aqui como PRIMÁRIO deste modo de fallback: procura na barra lateral (mesma
		// Tree/seletores de ScanConversations, incluindo expandir Favoritos/Recentes e paginar
		// "See more") o item cujo `data-fui-tree-item-value` codifica o threadID exato do link, e
		// clica nele — não depende de saber o nome de exibição da conversa.
		threadIDJS, _ := json.Marshal(threadID)
		navJS := fmt.Sprintf(`() => {
			window.location.hash = '#/conversations/%s?ctx=chat';
			return window.location.href;
		}`, threadID)
		navConfirmJS := fmt.Sprintf(`() => window.location.hash.includes(%s)`, threadIDJS)
		clickJS := fmt.Sprintf(`async () => {
			const sleep = ms => new Promise(r => setTimeout(r, ms));
			const targetId = %s;
			const CONV_TYPES = ['OneOnOneChatConversation', 'GroupChatConversation', 'MeetingChatConversation', 'SelfChatConversation'];
			const isConvValue = val => CONV_TYPES.some(t => val.includes(t));
			const extractIdFromValue = val => {
				const bar = val.lastIndexOf('|');
				if (bar === -1) return '';
				const after = val.slice(bar + 1).trim();
				return /^(19|28|48):/.test(after) ? after : '';
			};
			const tree = document.querySelector('[data-tid="simple-collab-dnd-rail"]');
			if (!tree) return JSON.stringify({ clicked: false, reason: 'no-tree' });

			for (const folder of ['RecentChats', 'Favorites']) {
				const el = tree.querySelector('[data-fui-tree-item-value*="' + folder + '"]');
				if (el && el.getAttribute('aria-expanded') !== 'true') { el.click(); await sleep(1000); }
			}

			const tryFind = () => {
				for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
					const val = el.getAttribute('data-fui-tree-item-value') || '';
					if (!isConvValue(val)) continue;
					if (extractIdFromValue(val) === targetId) return el;
				}
				return null;
			};
			const clickSeeMore = () => {
				for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
					if ((el.textContent || '').trim().toLowerCase() === 'see more') { el.click(); return true; }
				}
				return false;
			};

			tree.scrollTop = 0;
			await sleep(300);
			let found = tryFind();
			if (found) { found.click(); return JSON.stringify({ clicked: true, method: 'sidebar-top' }); }

			let prevScroll = -1;
			let seeMoreTries = 0;
			for (let round = 0; round < 150; round++) {
				found = tryFind();
				if (found) { found.click(); return JSON.stringify({ clicked: true, method: 'sidebar-scroll', round }); }
				if (tree.scrollTop === prevScroll) {
					if (seeMoreTries < 5 && clickSeeMore()) { seeMoreTries++; await sleep(1000); continue; }
					break;
				}
				prevScroll = tree.scrollTop;
				tree.scrollTop += 350;
				await sleep(250);
			}
			return JSON.stringify({ clicked: false, reason: 'not-in-sidebar' });
		}`, threadIDJS)

		if _, navErr := page.Eval(navJS); navErr != nil {
			logger.Warn().Err(navErr).Msg("[MessageFetch] Falha ao navegar via hash")
		}
		time.Sleep(2 * time.Second)

		logger.Info().Str("thread", threadID).Msg("[MessageFetch] Procurando a conversa na barra lateral (expandindo/rolando)...")
		clickRes, clickErr := page.Eval(clickJS)
		if clickErr != nil {
			logger.Warn().Err(clickErr).Msg("[MessageFetch] Falha ao executar busca/clique na barra lateral")
		} else if !clickRes.Value.Nil() {
			logger.Info().Str("result", clickRes.Value.String()).Msg("[MessageFetch] Resultado da busca/clique na barra lateral")
		}
		time.Sleep(2 * time.Second)

		// Reforço: reenvia o hash algumas vezes (o clique acima, se funcionou, já deve ter
		// atualizado a URL sozinho via o router do Teams — isso é só uma rede de segurança).
		hashConfirmed := false
		for attempt := 0; attempt < 3; attempt++ {
			if confirmRes, confirmErr := page.Eval(navConfirmJS); confirmErr == nil && !confirmRes.Value.Nil() && confirmRes.Value.Bool() {
				hashConfirmed = true
				break
			}
			if _, navErr := page.Eval(navJS); navErr != nil {
				logger.Warn().Err(navErr).Msg("[MessageFetch] Falha ao navegar via hash (reforço)")
			}
			time.Sleep(2 * time.Second)
		}
		if !hashConfirmed {
			logger.Warn().Str("thread", threadID).Msg("[MessageFetch] Hash da conversa alvo não confirmado na URL mesmo após busca/clique na barra lateral — a conversa pode não estar acessível na barra lateral desta conta, ou a busca pode estar rodando na conversa errada")
		}
		time.Sleep(1 * time.Second)
	}

	extractJS := fmt.Sprintf(`() => {
		// DEBUG_MODE — ligado via FetchMessageRequest.Debug (frontend, checkbox "Modo
		// diagnóstico"). Quando true, cada mensagem candidata carrega o outerHTML do próprio
		// elemento — usado só pra investigar por que uma imagem não foi detectada (ver
		// saveMessageImageDebugDump abaixo); NUNCA ligado por padrão, pra não pagar o custo de
		// serializar/transportar HTML potencialmente grande em toda extração comum sem imagem.
		const DEBUG_MODE = %t;
		// Bug real corrigido (relatado ao vivo, confirmado via dump de data-tid salvo por
		// saveDebugDiagnostics): o layout atual do Teams v2 (pelo menos pra chat de reunião,
		// possivelmente mais amplo) usa data-tid="chat-pane-message" como container de cada
		// mensagem — NUNCA "messageBody" (herdado sem validação do domMsgJS do Mr.ViaBot em
		// RunDiscovery, que é uma conversa comum, não de reunião). Com nenhum dos seletores
		// batendo, a extração rodava dezenas de rodadas sem nunca ver sequer 1 mensagem, mesmo
		// com a mensagem certa visível na tela (0 elementos = nunca chegava nem a tentar achar o
		// <time>). "chat-pane-message" agora é o primeiro/prioritário; os antigos permanecem como
		// fallback pra não regredir contra outros tipos de conversa (1:1/grupo) não testados
		// nesta rodada.
		const selectors = ['[data-tid="chat-pane-message"]', '[data-tid="messageBody"]', '[class*="messageBodyContent"]', '[class*="message-body"]', '[class*="messageBody"]'];
		// htmlToMarkdown converte o HTML rico da mensagem do Teams pra Markdown — antes a extração
		// usava innerText puro, perdendo negrito/itálico/links/listas (pedido explícito do
		// usuário: "não está carregando o texto com a mesma formatação markdown do chat"). Espelha
		// o sentido inverso de markdownToTeamsHTML (internal/web/handlers/teams_broadcast.go, usado
		// no envio) — mesmo vocabulário de sintaxe (negrito/itálico/tachado/código/link/lista/
		// citação) pra manter round-trip consistente entre carregar uma mensagem existente e
		// reenviar via a mesma ferramenta. BT via String.fromCharCode (não um literal backtick no
		// código) porque este bloco inteiro está dentro de um raw string Go delimitado por
		// backtick — um backtick literal aqui fecharia a string Go no lugar errado.
		const BT = String.fromCharCode(96);
		// currentImages acumula as URLs de TODA imagem (emoji incluso — pedido explícito do
		// usuário: "todas as imagens contidas na mensagem devem ser carregadas independente de ser
		// emoji ou não") encontrada na mensagem sendo processada no momento — resetada a cada
		// elemento de mensagem no loop "els.forEach" abaixo. O índice de cada URL nesta lista casa
		// com o marcador "@@TEAMS_IMG_i@@" deixado no texto (substituído depois, já com os bytes
		// reais baixados, por resolveMessageImages em FetchMessageByLink).
		let currentImages = [];
		// currentImgTagsTotal — contador de diagnóstico (quantas <img> existiam no elemento),
		// resetado junto com currentImages a cada elemento de mensagem. Exposto até o Go
		// (domMsg.imgTagsTotal) pra distinguir "não havia nenhuma <img>" de "havia <img> mas o
		// download/decodificação falhou depois".
		let currentImgTagsTotal = 0;
		// isAvatarImg — ÚNICA exclusão que sobrevive nesta versão: a foto de perfil do remetente
		// (renderizada como <img> dentro do mesmo elemento de mensagem em vários layouts de chat)
		// não é conteúdo da mensagem, é identidade visual de quem enviou — incluí-la produziria
		// literalmente o sintoma relatado ao vivo ("a única imagem gerada foi um quadro preto sem
		// nenhum elemento que nada tem a ver com a imagem original": o placeholder de avatar sem
		// foto customizada do Teams é uma silhueta escura/preta sólida). Só exclui com um sinal
		// FORTE e específico (a palavra inteira "avatar" em itemtype/classe/data-tid/data-testid),
		// nunca por heurística de tamanho — diferente da extinta isEmojiImg, aqui um falso-negativo
		// (avatar não reconhecido) é aceitável, um falso-positivo (foto real excluída) não.
		const isAvatarImg = (img) => {
			const marker = ((img.getAttribute('itemtype') || '') + ' ' + (img.className || '') + ' ' + (img.getAttribute('data-testid') || '') + ' ' + (img.getAttribute('data-tid') || '')).toLowerCase();
			// "persona" cobre o componente de avatar do Fluent UI (usado pelo Teams v2) —
			// nomenclatura própria da Microsoft pra esse componente, além do termo genérico
			// "avatar" que outros frameworks de chat costumam usar.
			return /\bavatar\b|\bpersona\b/.test(marker);
		};
		// isReactionImg — CONFIRMADO via dump de HTML real (Modo diagnóstico): reações (👍/❤️/etc.)
		// de OUTRAS pessoas à mensagem são renderizadas como <img itemtype="http://schema.skype.
		// com/Emoji"> dentro de um bloco [data-tid="diverse-reaction-summary"] — que é FILHO
		// DIRETO do mesmo elemento "chat-pane-message" que os seletores capturam (irmão do bloco
		// de texto/conteúdo, não um ancestral separado). Sem essa exclusão, o texto/imagens da
		// mensagem reenviada ganhariam as reações de terceiros como se fossem conteúdo original —
		// nunca é isso que "imagens contidas na mensagem" deveria significar. closest() sobe a
		// árvore de ancestrais procurando esse data-tid específico.
		const isReactionImg = (img) => !!img.closest('[data-tid="diverse-reaction-summary"], [class*="ChatMessage__reactions"]');
		// currentImgCandidates — diagnóstico detalhado, só populado quando DEBUG_MODE (custo de
		// serializar atributos de CADA <img> visto, não só as incluídas): um registro por <img>
		// encontrada no elemento, com os atributos que decidiram inclusão/exclusão. Existe porque
		// duas rodadas de heurística "às cegas" (sem acesso a uma sessão real do Teams) já erraram
		// — a 1ª descartou uma foto real classificando como emoji; a 2ª (fallback de
		// background-image, já removido) capturou uma imagem completamente errada ("quadro preto
		// que nada tem a ver com a original", quase certamente o placeholder de avatar sem foto).
		// Com isso, o usuário pode compartilhar exatamente o que cada <img> real da mensagem
		// carregava (src/classe/data-tid) sem precisar abrir um dump de HTML inteiro.
		let currentImgCandidates = [];
		const htmlToMarkdown = (node) => {
			let out = '';
			for (const child of node.childNodes) {
				if (child.nodeType === Node.TEXT_NODE) { out += child.textContent; continue; }
				if (child.nodeType !== Node.ELEMENT_NODE) continue;
				const tag = child.tagName.toLowerCase();
				switch (tag) {
					case 'b': case 'strong':
						out += '**' + htmlToMarkdown(child).trim() + '**'; break;
					case 'i': case 'em':
						out += '*' + htmlToMarkdown(child).trim() + '*'; break;
					case 's': case 'strike': case 'del':
						out += '~~' + htmlToMarkdown(child).trim() + '~~'; break;
					case 'code':
						out += BT + (child.textContent || '') + BT; break;
					case 'pre':
						out += '\n' + BT + BT + BT + '\n' + (child.textContent || '') + '\n' + BT + BT + BT + '\n'; break;
					case 'a': {
						const href = child.getAttribute('href') || '';
						const label = htmlToMarkdown(child).trim();
						out += href ? '[' + (label || href) + '](' + href + ')' : label;
						break;
					}
					case 'br':
						// Quebra de linha "dura" do Markdown (2 espaços + \n) — um \n sozinho é
						// tratado como espaço em branco (soft break) pelo CommonMark/remark-gfm
						// (o motor usado no preview desta aba), colapsando linha de cima e linha
						// de baixo numa só. Ver mesmo raciocínio no case 'p'/'div' abaixo.
						out += '  \n'; break;
					case 'ul': {
						for (const li of child.querySelectorAll(':scope > li')) {
							out += '- ' + htmlToMarkdown(li).trim() + '\n';
						}
						break;
					}
					case 'ol': {
						let i = 1;
						for (const li of child.querySelectorAll(':scope > li')) {
							out += (i++) + '. ' + htmlToMarkdown(li).trim() + '\n';
						}
						break;
					}
					case 'blockquote':
						out += '> ' + htmlToMarkdown(child).trim().replace(/\n/g, '\n> ') + '\n'; break;
					case 'img': {
						currentImgTagsTotal++;
						const isAvatar = isAvatarImg(child);
						const isReaction = isReactionImg(child);
						// pickImageSrc — bug real CONFIRMADO via dump de HTML real capturado ao
						// vivo (Modo diagnóstico): o Teams v2 (componente "AMSImage" do Fluent UI,
						// itemtype="http://schema.skype.com/AMSImage") usa lazy-loading real —
						// "src" sempre carrega um GIF 1x1 PRETO/transparente
						// ("data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=", confirmado
						// byte-a-byte batendo com o arquivo baixado nas tentativas anteriores) como
						// placeholder — a URL real está em "data-orig-src" (visualização "imgo",
						// provavelmente resolução original) ou "data-gallery-src" (visualização
						// "imgpsh_fullsize", a galeria em tela cheia) — nomes de atributo
						// específicos do Teams, nunca confirmáveis sem essa captura real (as
						// tentativas anteriores tentaram "data-src"/"data-original", nomes comuns
						// em outras libs de lazy-load, mas não os usados aqui).
						const pickImageSrc = (img) => {
							for (const attr of ['data-orig-src', 'data-gallery-src', 'data-src', 'data-original', 'data-lazy-src', 'data-fallback-src']) {
								const v = img.getAttribute(attr);
								if (v) return v;
							}
							const srcset = img.getAttribute('srcset') || img.getAttribute('data-srcset');
							if (srcset) {
								const parts = srcset.split(',').map(s => s.trim()).filter(Boolean);
								if (parts.length > 0) {
									const last = parts[parts.length - 1].split(/\s+/)[0];
									if (last) return last;
								}
							}
							return img.currentSrc || img.getAttribute('src') || '';
						};
						const pickedSrc = pickImageSrc(child);
						if (DEBUG_MODE) {
							currentImgCandidates.push({
								src: pickedSrc.slice(0, 300),
								rawSrcAttr: (child.getAttribute('src') || '').slice(0, 150),
								dataSrc: (child.getAttribute('data-orig-src') || child.getAttribute('data-gallery-src') || child.getAttribute('data-src') || '').slice(0, 150),
								srcset: (child.getAttribute('srcset') || '').slice(0, 150),
								width: child.getAttribute('width') || String(child.naturalWidth || ''),
								height: child.getAttribute('height') || String(child.naturalHeight || ''),
								alt: (child.getAttribute('alt') || '').slice(0, 100),
								className: String(child.className || '').slice(0, 200),
								dataTid: child.getAttribute('data-tid') || '',
								excludedAsAvatar: isAvatar || isReaction,
							});
						}
						if (isAvatar || isReaction) {
							// Avatar (foto de perfil do remetente) ou reação (👍 de OUTRAS pessoas
							// à mensagem, confirmado via dump real: vive dentro de
							// [data-tid="diverse-reaction-summary"], irmão do bloco de texto,
							// dentro do MESMO elemento "chat-pane-message" que os seletores
							// capturam) — nenhum dos dois é "imagem contida na mensagem" no
							// sentido do que a pessoa escreveu/anexou (ver isAvatarImg/
							// isReactionImg acima).
							out += child.getAttribute('alt') || '';
						} else {
							// TODA imagem (emoji incluso, pedido explícito do usuário) vira um
							// marcador de posição único; o Go substitui pelo Markdown de imagem
							// real ("![imagem](teams-temp:<filename>)") depois de baixar os bytes
							// de verdade (ver resolveMessageImages em FetchMessageByLink) —
							// precisa ser feito no Go porque só ele tem acesso ao disco.
							if (pickedSrc) {
								out += '@@TEAMS_IMG_' + currentImages.length + '@@';
								currentImages.push(pickedSrc);
							} else {
								out += child.getAttribute('alt') || '';
							}
						}
						break;
					}
					case 'p': case 'div':
						// Bug real corrigido (relatado ao vivo: "não respeita os espaços de
						// separação... sobretudo os espaços da linha de cima para a linha de
						// baixo"): cada <p>/<div> representa uma linha própria da mensagem
						// original no Teams, mas um único '\n' entre elas é soft break em
						// Markdown — o preview via react-markdown/remark-gfm renderiza como uma
						// linha só (junta com espaço). Precisa do mesmo hard break de <br> acima
						// pra cada linha aparecer separada de verdade; um <p>/<div> VAZIO (linha
						// em branco de propósito na mensagem original) contribui só com a quebra
						// em si, o que empilha certo como uma linha vazia extra no resultado.
						out += htmlToMarkdown(child) + '  \n'; break;
					default:
						out += htmlToMarkdown(child);
				}
			}
			return out;
		};
		const findTimestamp = (el) => {
			let node = el;
			for (let d = 0; d < 10 && node; d++) {
				if (node.querySelector) {
					const t = node.querySelector('time[datetime]');
					if (t) { const dt = t.getAttribute('datetime'); if (dt) return dt; }
					// Fallback: elemento com atributo title/aria-label parseável como data (alguns
					// layouts do Teams põem o horário só como texto de tooltip, não <time datetime>).
					// time.Parse (Go, RFC3339) descarta qualquer coisa que não seja ISO 8601 de
					// verdade — seguro mesmo testando vários candidatos "title"/"aria-label" soltos.
					for (const attr of ['title', 'aria-label']) {
						const el2 = node.querySelector('[' + attr + ']');
						const v = el2 && el2.getAttribute(attr);
						if (v && /^\d{4}-\d{2}-\d{2}T/.test(v)) return v;
					}
				}
				node = node.parentElement;
			}
			return '';
		};
		const messages = [];
		for (const sel of selectors) {
			const els = document.querySelectorAll(sel);
			if (els.length > 0) {
				els.forEach(el => {
					currentImages = [];
					currentImgTagsTotal = 0;
					currentImgCandidates = [];
					// Preferir a conversão pra Markdown; cair pro innerText puro só se ela vier
					// vazia (ex: elemento sem nenhum childNode reconhecido pelo conversor) — nesse
					// fallback os marcadores "@@TEAMS_IMG_i@@" (que só existem no resultado de
					// htmlToMarkdown) não sobrevivem, então as imagens coletadas também são
					// descartadas — sem posição no texto pra reinserir, guardá-las seria
					// inconsistente.
					let text = htmlToMarkdown(el).trim();
					if (!text) {
						text = (el.innerText || el.textContent || '').trim();
						currentImages = [];
					}
					if (text.length > 0) {
						messages.push({
							text, postedAt: findTimestamp(el), imageUrls: currentImages.slice(),
							imgTagsTotal: currentImgTagsTotal,
							imgCandidates: DEBUG_MODE ? currentImgCandidates.slice() : [],
							debugHtml: DEBUG_MODE ? (el.outerHTML || '').slice(0, 30000) : '',
						});
					}
				});
				if (messages.length > 0) break;
			}
		}
		return JSON.stringify(messages);
	}`, debug)

	// Sobe até o topo da conversa repetidamente até achar uma mensagem dentro da tolerância do
	// timestamp alvo, ou até o histórico acabar (scrollHeight parar de crescer por 3 rodadas
	// seguidas) — mesmo padrão adaptativo de RunDiscovery, mas orientado por "achou o alvo?" em
	// vez de "apareceu alguma mensagem?".
	scrollJS := `() => {
		// Mesmo achado do extractJS acima (ver comentário lá) — o container real de scroll da
		// lista de mensagens usa data-tid="message-pane-list-viewport" (ou "message-pane-body"
		// como ancestral), não nenhum dos nomes antigos. Sem eles, o loop de scroll caía nos
		// seletores genéricos (ex: [role="list"]) e rolava alguma OUTRA lista da página (ex: a
		// barra lateral), nunca a lista de mensagens de verdade — 57-60 rodadas de "scrolled:true"
		// sem jamais esgotar o histórico real, porque nunca era o histórico real sendo rolado.
		const selectors = ['[data-tid="message-pane-list-viewport"]', '[data-tid="message-pane-body"]', '[data-tid="messageList"]', '[class*="messageListContainer"]', '[class*="scrollContainer"]', '[class*="chatContent"]', '[class*="message-list"]', '[role="log"]', '[role="list"]'];
		for (const sel of selectors) {
			const el = document.querySelector(sel);
			if (el && el.scrollHeight > el.clientHeight) {
				const height = el.scrollHeight;
				el.scrollTop = 0;
				return { scrolled: true, scrollHeight: height };
			}
		}
		return { scrolled: false, scrollHeight: 0 };
	}`

	// domImgCandidate — mirror de ImgCandidateDebug (tipo exportado, usado na resposta HTTP) com
	// tags JSON em camelCase, pra bater com o que o próprio extractJS produz (JSON.stringify de
	// objeto JS usa as chaves literais, sempre camelCase neste arquivo) — a resposta HTTP em si
	// segue snake_case (convenção do resto deste pacote), daí os dois tipos separados em vez de
	// um só reaproveitado direto.
	type domImgCandidate struct {
		Src              string `json:"src"`
		RawSrcAttr       string `json:"rawSrcAttr"`
		DataSrc          string `json:"dataSrc"`
		Srcset           string `json:"srcset"`
		Width            string `json:"width"`
		Height           string `json:"height"`
		Alt              string `json:"alt"`
		ClassName        string `json:"className"`
		DataTid          string `json:"dataTid"`
		ExcludedAsAvatar bool   `json:"excludedAsAvatar"`
	}
	type domMsg struct {
		Text      string   `json:"text"`
		PostedAt  string   `json:"postedAt"`
		ImageURLs []string `json:"imageUrls"`
		// ImgTagsTotal — diagnóstico (nunca usado pra decidir nada, só logado/exposto): quantas
		// <img> existiam no elemento (incluindo avatar excluído) — distingue "não havia nenhuma
		// <img>" de "havia <img>, mas algo falhou depois (download/decodificação)".
		ImgTagsTotal int    `json:"imgTagsTotal"`
		DebugHTML    string `json:"debugHtml"`
		// ImgCandidates — só populado quando debug=true (ver DEBUG_MODE no extractJS): um registro
		// por <img> encontrada no elemento, com os atributos reais que decidiram inclusão/
		// exclusão. Permite compartilhar exatamente o que cada <img> carregava sem precisar abrir
		// o dump de HTML inteiro.
		ImgCandidates []domImgCandidate `json:"imgCandidates"`
	}

	var best *domMsg
	bestDiff := int64(-1)
	lastScrollHeight := float64(-1)
	stableRounds := 0
	rounds := 0
	totalMsgsSeen := 0
	withTimestampSeen := 0
	// firstSnapshot — mensagens vistas na PRIMEIRA rodada (antes de qualquer scroll), usado como
	// rede de segurança abaixo: se nenhuma mensagem em NENHUMA rodada tiver um timestamp
	// reconhecível (ver bug corrigido abaixo), a conversa ainda foi navegada corretamente — a
	// última mensagem visível na primeira rodada é o melhor palpite disponível sem depender de
	// timestamp nenhum.
	var firstSnapshot []domMsg
	fetchDeadline := time.Now().Add(teamsMessageFetchTimeout)

	for time.Now().Before(fetchDeadline) {
		rounds++
		if res, evalErr := page.Eval(extractJS); evalErr == nil && !res.Value.Nil() {
			var msgs []domMsg
			if json.Unmarshal([]byte(res.Value.String()), &msgs) == nil {
				totalMsgsSeen += len(msgs)
				if rounds == 1 {
					firstSnapshot = msgs
				}
				for i := range msgs {
					m := msgs[i]
					if m.PostedAt == "" {
						continue
					}
					t, perr := time.Parse(time.RFC3339, m.PostedAt)
					if perr != nil {
						continue
					}
					withTimestampSeen++
					diff := t.Sub(targetTime).Milliseconds()
					if diff < 0 {
						diff = -diff
					}
					if bestDiff == -1 || diff < bestDiff {
						bestDiff = diff
						mm := m
						best = &mm
					}
				}
			}
		}

		if bestDiff != -1 && bestDiff <= teamsMessageMatchToleranceMs {
			logger.Info().Int64("diff_ms", bestDiff).Msg("[MessageFetch] Mensagem encontrada dentro da tolerância")
			break
		}

		scrollRes, scrollErr := page.Eval(scrollJS)
		if scrollErr != nil {
			break
		}
		var sres struct {
			Scrolled     bool    `json:"scrolled"`
			ScrollHeight float64 `json:"scrollHeight"`
		}
		if json.Unmarshal([]byte(scrollRes.Value.String()), &sres) == nil {
			if !sres.Scrolled {
				break
			}
			if sres.ScrollHeight == lastScrollHeight {
				stableRounds++
				if stableRounds >= 3 {
					logger.Info().Msg("[MessageFetch] Histórico da conversa esgotado — parando busca")
					break
				}
			} else {
				stableRounds = 0
			}
			lastScrollHeight = sres.ScrollHeight
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// Bug real corrigido (relatado ao vivo: navegação chegou na conversa/mensagem certa, mas o
	// carregamento retornava "mensagem não encontrada" mesmo assim) — `best` só é preenchido para
	// mensagens com um `<time datetime>` (ou title/aria-label ISO) reconhecível a até 10
	// ancestrais de distância; em algumas conversas NENHUMA mensagem carrega esse atributo de
	// forma reconhecível — o mesmo tipo de limitação já documentado em `filterByAge`/
	// `RunDiscovery` ("itens sem PostedAt são mantidos" porque a extração de timestamp via DOM é
	// sabidamente não-confiável). Fallback: se nenhuma mensagem com timestamp foi achada em
	// nenhuma rodada, mas a primeira rodada (antes de qualquer scroll — a visão inicial da
	// conversa) tinha ao menos uma mensagem de texto, usa a ÚLTIMA dela (mais recente na ordem
	// natural do DOM) como melhor palpite. Sempre marcado `Approximate: true`.
	if best == nil && len(firstSnapshot) > 0 {
		fallback := firstSnapshot[len(firstSnapshot)-1]
		best = &fallback
		logger.Warn().Int("first_snapshot_count", len(firstSnapshot)).
			Msg("[MessageFetch] Nenhuma mensagem com timestamp reconhecível — usando a última mensagem visível na conversa como palpite (Approximate=true)")
	}

	if best == nil {
		// Diagnóstico rico (screenshot + inventário de data-tid) só neste caminho de falha total
		// — confirmado ao vivo que `total_msgs_seen: 0` pode acontecer mesmo depois de dezenas de
		// rodadas de scroll (log real: rounds:59, total_msgs_seen:0 no modo sidebar-click contra
		// uma thread de reunião), ou seja, NENHUM dos seletores de `extractJS` bate em nada nessa
		// visão da conversa. Sem acesso a uma sessão real pra inspecionar o DOM ao vivo, capturar
		// isso é o único jeito de descobrir os seletores certos sem continuar adivinhando às cegas.
		diagPath := saveDebugDiagnostics(page, logger)
		logger.Warn().Str("mode", mode).Int("rounds", rounds).Int("total_msgs_seen", totalMsgsSeen).Int("with_timestamp", withTimestampSeen).
			Str("screenshot", diagPath).
			Msg("[MessageFetch] Nenhuma mensagem encontrada na conversa (nem como palpite) — dump de diagnóstico salvo")
		return nil, fmt.Errorf("mensagem não encontrada na conversa (modo %s) — verifique se você tem acesso a ela ou se o histórico ainda está disponível", mode)
	}

	text := normalizeExtractedText(best.Text)
	imagesSaved := 0
	if len(best.ImageURLs) > 0 {
		text, imagesSaved = resolveMessageImages(page, text, best.ImageURLs, logger)
	}

	result := &FetchedMessage{
		ThreadID:  threadID,
		MessageID: messageID,
		Text:      text,
		// bestDiff == -1 cobre o fallback acima (nenhum timestamp reconhecível — best veio do
		// firstSnapshot, sem diff calculado nenhum).
		Approximate:    bestDiff == -1 || bestDiff > teamsMessageMatchToleranceMs,
		ImagesSaved:    imagesSaved,
		ImagesDetected: len(best.ImageURLs),
		ImgTagsSeen:    best.ImgTagsTotal,
	}
	if postedAt, perr := time.Parse(time.RFC3339, best.PostedAt); perr == nil {
		result.PostedAt = &postedAt
	}
	if debug {
		if best.DebugHTML != "" {
			result.DebugDumpPath = saveMessageImageDebugDump(best.DebugHTML, logger)
		}
		for _, c := range best.ImgCandidates {
			result.ImgCandidates = append(result.ImgCandidates, ImgCandidateDebug{
				Src: c.Src, RawSrcAttr: c.RawSrcAttr, DataSrc: c.DataSrc, Srcset: c.Srcset,
				Width: c.Width, Height: c.Height, Alt: c.Alt,
				ClassName: c.ClassName, DataTid: c.DataTid, ExcludedAsAvatar: c.ExcludedAsAvatar,
			})
		}
	}
	logger.Info().Str("thread", threadID).Str("mode", mode).Int64("diff_ms", bestDiff).Bool("approx", result.Approximate).
		Int("images_detected", result.ImagesDetected).Int("images_saved", result.ImagesSaved).
		Int("img_tags_seen", result.ImgTagsSeen).
		Msg("[MessageFetch] Mensagem carregada")
	return result, nil
}

// saveMessageImageDebugDump grava o outerHTML (já capturado pelo extractJS, ver DEBUG_MODE) do
// elemento de mensagem localizado num arquivo em ~/.k8s-hpa-manager/teams-debug/ — só chamada
// quando FetchMessageByLink recebeu debug=true (checkbox "Modo diagnóstico" no frontend). Permite
// inspecionar a estrutura DOM real por trás de uma imagem que não foi detectada, sem precisar de
// acesso direto a uma sessão do Teams. Best-effort: falha aqui nunca derruba a extração em si.
func saveMessageImageDebugDump(html string, logger *zerolog.Logger) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-debug")
	if err := os.MkdirAll(dir, 0700); err != nil {
		logger.Debug().Err(err).Msg("[MessageFetch] Falha ao criar diretório de diagnóstico de imagem")
		return ""
	}
	path := filepath.Join(dir, "message-image-debug-"+time.Now().Format("20060102-150405")+".html")
	if err := os.WriteFile(path, []byte(html), 0600); err != nil {
		logger.Debug().Err(err).Msg("[MessageFetch] Falha ao salvar dump de diagnóstico de imagem")
		return ""
	}
	logger.Info().Str("path", path).Msg("[MessageFetch] Dump de diagnóstico da mensagem salvo (Modo diagnóstico)")
	return path
}

// saveDebugDiagnostics captura um screenshot da página + um inventário dos atributos `data-tid`
// presentes no DOM no momento da falha, salvos em `~/.k8s-hpa-manager/teams-debug/`. Chamada só
// quando a extração falha por completo (nenhuma mensagem em nenhuma rodada, ver comentário no
// call site) — permite inspecionar visualmente o layout real da conversa/tipo de thread sem
// precisar reproduzir a falha ao vivo numa sessão de debug interativa. Best-effort: qualquer erro
// aqui é só logado em Debug, nunca propagado (não é o objetivo principal da chamada).
// dismissAppLauncherPrompt procura, na página atual, um link/botão de "continuar na versão web"
// da página intermediária de abertura de app do Teams (ver comentário de toTeamsHashRoute) e
// clica nele — best-effort, chamado a cada iteração do loop de espera "app pronto" junto com o
// auto-login/bypass MCAS já existentes. Retorna false silenciosamente (não é erro) quando a
// página não tem esse prompt, que é o caso normal na maioria das chamadas.
func dismissAppLauncherPrompt(page *rod.Page, logger *zerolog.Logger) bool {
	js := `() => {
		const keywords = [
			'use the web app instead', 'continue on this browser', 'continue in browser',
			'use web app instead', "don't have the app? use the web app", 'view in browser',
			'usar o aplicativo da web', 'continuar neste navegador', 'usar a versão da web',
			'continuar no navegador',
		];
		const candidates = Array.from(document.querySelectorAll('a, button'));
		for (const el of candidates) {
			const text = (el.textContent || '').trim().toLowerCase();
			if (keywords.some(k => text.includes(k))) {
				el.click();
				return true;
			}
		}
		return false;
	}`
	res, err := page.Eval(js)
	if err != nil || res.Value.Nil() {
		return false
	}
	clicked := res.Value.Bool()
	if clicked {
		logger.Debug().Msg("[MessageFetch] Botão de continuar na versão web encontrado e clicado")
	}
	return clicked
}

func saveDebugDiagnostics(page *rod.Page, logger *zerolog.Logger) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-debug")
	if err := os.MkdirAll(dir, 0700); err != nil {
		logger.Debug().Err(err).Msg("[MessageFetch] Falha ao criar diretório de diagnóstico")
		return ""
	}

	if href, err := page.Eval(`() => window.location.href`); err == nil && !href.Value.Nil() {
		logger.Warn().Str("url", href.Value.String()).Msg("[MessageFetch] URL no momento da falha")
	}

	tidJS := `() => {
		const tids = Array.from(document.querySelectorAll('[data-tid]'))
			.map(el => el.getAttribute('data-tid'))
			.filter((v, i, a) => v && a.indexOf(v) === i);
		return JSON.stringify(tids.slice(0, 80));
	}`
	if res, err := page.Eval(tidJS); err == nil && !res.Value.Nil() {
		logger.Warn().Str("data_tids", res.Value.String()).Msg("[MessageFetch] Inventário de data-tid na página")
	}

	pngBytes, err := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
	if err != nil {
		logger.Debug().Err(err).Msg("[MessageFetch] Falha ao capturar screenshot de diagnóstico")
		return ""
	}
	pngPath := filepath.Join(dir, "message-fetch-"+time.Now().Format("20060102-150405")+".png")
	if err := os.WriteFile(pngPath, pngBytes, 0600); err != nil {
		logger.Debug().Err(err).Msg("[MessageFetch] Falha ao salvar screenshot de diagnóstico")
		return ""
	}
	return pngPath
}

// normalizeExtractedText corrige dois artefatos comuns de texto extraído via DOM (relatados ao
// vivo: "respeite também o UTF-8"):
//  1. NBSP (U+00A0) — o Teams usa esse caractere (não espaço comum) em vários pontos do editor
//     rico pra espaçamento visual que o HTML normal colapsaria; sobrevive ao innerText/textContent
//     e ao round-trip JSON (CDP → Go são UTF-8 nativo dos dois lados, nunca corrompe o byte em
//     si), mas mistura espaços "normais" com NBSP no mesmo texto — inconsistente visualmente e
//     estranho ao copiar/colar o resultado em outro lugar. Normalizado pra espaço comum.
//  2. Qualquer sequência de bytes UTF-8 inválida — não deveria acontecer nesse pipeline (CDP e
//     Go's encoding/json são ambos UTF-8 nativo), mas `strings.ToValidUTF8` é uma rede de
//     segurança barata: sem ela, `encoding/json.Marshal` substitui bytes inválidos por caracteres
//     de substituição (�) espalhados pelo meio do texto sem nenhum aviso — melhor remover de vez.
func normalizeExtractedText(s string) string {
	s = strings.ReplaceAll(s, "\u00A0", " ")
	return strings.ToValidUTF8(s, "")
}

// teamsImagePlaceholderRe casa o marcador "@@TEAMS_IMG_i@@" deixado no texto pelo extractJS de
// fetchMessageAttempt (ver case 'img' dentro de htmlToMarkdown) — usado por resolveMessageImages
// pra saber qual índice de currentImages/best.ImageURLs cada marcador representa.
var teamsImagePlaceholderRe = regexp.MustCompile(`@@TEAMS_IMG_(\d+)@@`)

// resolveMessageImages baixa cada URL de imagem encontrada na mensagem escolhida, salva os bytes
// reais na pasta temporária (ver image_temp_store.go) e substitui os marcadores "@@TEAMS_IMG_i@@"
// deixados no texto pela sintaxe Markdown de imagem apontando pro arquivo salvo
// ("![imagem](teams-temp:<filename>)") — esse esquema "teams-temp:" nunca sai desta aplicação: o
// frontend resolve pra uma URL real do endpoint que serve a pasta temporária ao exibir o preview,
// e o backend resolve pros bytes reais (embutidos como data URI) na hora do envio (ver
// resolveLocalImagePlaceholders em internal/web/handlers/teams_broadcast.go).
//
// Best-effort em cada etapa — uma imagem que falhar ao baixar/decodificar/salvar vira um aviso
// textual em vez de derrubar a extração inteira (a mensagem já foi localizada com sucesso nesse
// ponto; perder só a imagem é preferível a perder tudo).
func resolveMessageImages(page *rod.Page, text string, urls []string, logger *zerolog.Logger) (string, int) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Warn().Err(err).Msg("[MessageFetch] Não foi possível resolver $HOME — imagens da mensagem não serão salvas")
		return stripImagePlaceholders(text, len(urls)), 0
	}
	dir := TeamsBroadcastImagesDir(homeDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		logger.Warn().Err(err).Msg("[MessageFetch] Falha ao preparar pasta temporária de imagens")
		return stripImagePlaceholders(text, len(urls)), 0
	}

	saved := 0
	resolved := teamsImagePlaceholderRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := teamsImagePlaceholderRe.FindStringSubmatch(match)
		idx, convErr := strconv.Atoi(sub[1])
		if convErr != nil || idx < 0 || idx >= len(urls) {
			return "[imagem indisponível]"
		}
		data, mimeType, fetchErr := fetchImageBytes(page, urls[idx], logger)
		if fetchErr != nil || len(data) == 0 {
			logger.Warn().Err(fetchErr).Int("index", idx).Msg("[MessageFetch] Falha ao obter bytes da imagem")
			return "[imagem indisponível]"
		}
		filename, saveErr := SaveTeamsBroadcastImage(dir, idx, mimeType, data)
		if saveErr != nil {
			logger.Warn().Err(saveErr).Int("index", idx).Msg("[MessageFetch] Falha ao salvar imagem no disco")
			return "[imagem indisponível]"
		}
		saved++
		return "![imagem](teams-temp:" + filename + ")"
	})
	logger.Info().Int("found", len(urls)).Int("saved", saved).Msg("[MessageFetch] Imagens da mensagem processadas")
	return resolved, saved
}

// fetchImageBytes obtém os bytes reais de uma URL de imagem já visível na página, em 2 camadas:
//
//  1. page.GetResource (CDP "Page.getResourceContent") — lê o conteúdo que o PRÓPRIO Chrome já
//     baixou pra exibir a tag <img> na tela, sem fazer nenhuma requisição de rede nova.
//
// Bug real corrigido (relatado ao vivo: toda imagem virava "[imagem indisponível]" mesmo com a
// foto claramente visível na mensagem original): a 1ª versão desta função fazia um fetch()
// MANUAL da mesma URL dentro do JS da página — e um fetch() explícito passa pela checagem de
// CORS do navegador, mesmo quando a MESMA URL carrega normalmente como <img src> (exibir uma tag
// <img> nunca é sujeito a CORS, só um fetch()/XHR explícito é). O domínio de mídia do Teams
// provavelmente não libera CORS pra chamadas fetch() de terceiros, então a imagem aparecia
// normalmente na tela mas o fetch() sempre falhava silenciosamente (capturado pelo catch,
// virando `null`). page.GetResource usa o comando de debug do Chrome (CDP) pra ler o conteúdo já
// em memória do browser pra aquele recurso — não é uma requisição JS, não está sujeito a CORS
// nenhum, porque o CDP tem acesso privilegiado ao processo do browser.
//
//  2. Fallback: fetch() autenticado via JS (mecanismo antigo) — mantido só pro caso raro de
//     GetResource não achar o recurso (ex: já saiu do cache do Chrome), sem custo extra quando a
//     camada 1 já funciona (que deve ser a maioria dos casos).
func fetchImageBytes(page *rod.Page, url string, logger *zerolog.Logger) ([]byte, string, error) {
	if data, err := page.GetResource(url); err == nil && len(data) > 0 {
		return data, http.DetectContentType(data), nil
	} else if err != nil {
		logger.Debug().Err(err).Msg("[MessageFetch] GetResource (CDP) não encontrou o recurso — tentando fetch via JS")
	}

	urlJSON, _ := json.Marshal(url)
	fetchJS := fmt.Sprintf(`async () => {
		const bytesToBase64 = (bytes) => {
			let binary = '';
			const chunkSize = 0x8000;
			for (let i = 0; i < bytes.length; i += chunkSize) {
				binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunkSize));
			}
			return btoa(binary);
		};
		try {
			const resp = await fetch(%s, { credentials: 'include' });
			if (!resp.ok) return JSON.stringify(null);
			const buf = await resp.arrayBuffer();
			const mime = (resp.headers.get('content-type') || '').split(';')[0].trim();
			return JSON.stringify({ mime, base64: bytesToBase64(new Uint8Array(buf)) });
		} catch (e) {
			return JSON.stringify(null);
		}
	}`, string(urlJSON))

	res, evalErr := page.Eval(fetchJS)
	if evalErr != nil {
		return nil, "", evalErr
	}
	var out *struct {
		Mime   string `json:"mime"`
		Base64 string `json:"base64"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil || out == nil {
		return nil, "", fmt.Errorf("fetch via JS (fallback) também não retornou dados — provável CORS/autenticação do domínio de mídia")
	}
	data, decErr := base64.StdEncoding.DecodeString(out.Base64)
	if decErr != nil {
		return nil, "", decErr
	}
	mimeType := out.Mime
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return data, mimeType, nil
}

// stripImagePlaceholders substitui todo marcador "@@TEAMS_IMG_i@@" por um aviso textual — usada
// quando resolveMessageImages falha antes mesmo de tentar o download (ex: $HOME não resolvido),
// pra nunca deixar o marcador cru vazando pro texto final da mensagem.
func stripImagePlaceholders(text string, count int) string {
	if count == 0 {
		return text
	}
	return teamsImagePlaceholderRe.ReplaceAllString(text, "[imagem indisponível]")
}
