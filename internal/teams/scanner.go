package teams

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// ScannedChat representa uma conversa encontrada no Teams.
type ScannedChat struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"`
}

// ScanResult é o arquivo salvo pelo scanner.
type ScanResult struct {
	Query      string        `json:"query"`
	SearchedAt string        `json:"searched_at"`
	Count      int           `json:"count"`
	Chats      []ScannedChat `json:"chats"`
}

// ScanConversations abre o Chrome com a sessão do Teams, aguarda o carregamento e
// retorna TODAS as conversas: grupos via IndexedDB e DMs via scroll+click na sidebar.
func ScanConversations(sessionDir, outputPath string, logger *zerolog.Logger) ([]ScannedChat, error) {
	// Serializa contra RunDiscovery/SendBatch (ver operationMu em browser_manager.go).
	operationMu.Lock()
	defer operationMu.Unlock()

	browser, err := getBrowser(sessionDir, logger)
	if err != nil {
		return nil, err
	}

	page, err := browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %w", err)
	}
	// createdPage: `page` é reatribuída no loop de espera abaixo — fechar sempre a aba criada aqui.
	createdPage := page
	defer createdPage.Close() //nolint:errcheck

	restoreWindow(page, logger)

	// Aguardar Teams carregar — apenas pela URL, sem verificar DOM.
	logger.Info().Msg("[Scanner] Aguardando Teams carregar (máx 3min)...")
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
				logger.Info().Str("url", u).Msg("[Scanner] Teams detectado")
				goto loaded
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("timeout aguardando Teams carregar")

loaded:
	// Daqui em diante é tudo via CDP/JS — sem necessidade de interação do usuário.
	minimizeWindow(page, logger)

	logger.Info().Msg("[Scanner] Aguardando Teams inicializar completamente (25s)...")
	time.Sleep(25 * time.Second)

	// Timeout generoso para o JS — a fase DOM+click pode levar ~3 min para 300+ DMs.
	page = page.Timeout(6 * time.Minute)

	logger.Info().Msg("[Scanner] Iniciando varredura completa (IndexedDB + sidebar)...")

	js := `async () => {
		const sleep = ms => new Promise(r => setTimeout(r, ms));

		// ── helpers ────────────────────────────────────────────────────────────
		const openDB = name => new Promise((res, rej) => {
			const r = indexedDB.open(name);
			r.onsuccess = e => res(e.target.result);
			r.onerror   = () => rej(r.error);
		});
		const getAll = (db, store) => new Promise(res => {
			try {
				const tx = db.transaction(store, 'readonly');
				const req = tx.objectStore(store).getAll();
				req.onsuccess = () => res(req.result || []);
				req.onerror   = () => res([]);
			} catch { res([]); }
		});
		// Thread IDs do Teams: 19:, 28: ou 48:
		const isConvId = id =>
			typeof id === 'string' && id.length > 10 &&
			(id.startsWith('19:') || id.startsWith('28:') || id.startsWith('48:'));

		const seen  = new Set();
		const chats = [];
		const addChat = (id, name, src) => {
			if (id && name && !seen.has(id)) { seen.add(id); chats.push({ id, display_name: name, source: src }); }
		};

		// ── 1. IndexedDB: conversation-manager ────────────────────────────────
		// Grupos: extraídos via threadProperties.topic.
		// DMs: extraídos via lastMessage.imdisplayname (remetente != eu).
		// "Eu" = MRI com maior frequência entre os membros.
		try {
			const allDbs = await indexedDB.databases().catch(() => []);
			for (const {name} of allDbs) {
				if (!name || !name.includes('conversation-manager')) continue;
				try {
					const db = await openDB(name);
					if (!db.objectStoreNames.contains('conversations')) { db.close(); continue; }
					const rows = await getAll(db, 'conversations');

					for (const row of rows) {
						const id = row.id || row.threadId || '';
						if (!id || !isConvId(id)) continue;
						const tp    = row.threadProperties || {};
						const topic = tp.topic || tp.name || tp.displayName ||
						              row.topic || row.name || row.displayName || '';
						// IndexedDB: apenas grupos com nome definido.
						// DMs e contatos são extraídos pela DOM tree (nome correto do contato).
						if (topic) addChat(id, topic, 'group');
					}
					db.close();
					break; // apenas o primeiro banco conversation-manager
				} catch {}
			}
		} catch {}

		// ── 2. DOM sidebar: simple-collab-dnd-rail ───────────────────────────
		// Teams v2 codifica o tipo e ID de cada item em data-fui-tree-item-value.
		// Conversas têm OneGQL_*Conversation| no valor; o ID vem após o último |.
		// Self-chat: OneGQL_SelfChatConversation|48:notes
		// 1:1:      OneGQL_OneOnOneChatConversation|19:UUID_UUID@unq.gbl.spaces
		// Grupo:    OneGQL_GroupChatConversation|19:UUID@thread.v2
		// Reunião:  OneGQL_MeetingChatConversation|19:meeting_...
		// NÃO usar aria-expanded para distinguir — todos os itens têm true/false.
		const CONV_TYPES = [
			'OneOnOneChatConversation',
			'GroupChatConversation',
			'MeetingChatConversation',
			'SelfChatConversation',
		];
		const isConvValue = val => CONV_TYPES.some(t => val.includes(t));
		const extractIdFromValue = val => {
			const bar = val.lastIndexOf('|');
			if (bar === -1) return '';
			const after = val.slice(bar + 1).trim();
			return /^(19|28|48):/.test(after) ? after : '';
		};
		const SKIP_TEXTS = new Set([
			'see more','see all your teams','mentions','followed threads',
			'drafts','quick views','chats','favorites','copilot',
		]);
		const srcFromValue = val => {
			if (val.includes('OneOnOneChat'))  return 'dm';
			if (val.includes('GroupChat'))     return 'group';
			if (val.includes('MeetingChat'))   return 'meeting';
			if (val.includes('SelfChat'))      return 'self';
			return 'dom-tree';
		};

		const tree = document.querySelector('[data-tid="simple-collab-dnd-rail"]');
		if (tree) {
			// Expandir Favorites e RecentChats se ainda não expandidos.
			for (const folder of ['RecentChats', 'Favorites']) {
				const el = tree.querySelector('[data-fui-tree-item-value*="' + folder + '"]');
				if (el && el.getAttribute('aria-expanded') !== 'true') {
					el.click();
					await sleep(1000);
				}
			}

			const processTreeItems = () => {
				for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
					const val = el.getAttribute('data-fui-tree-item-value') || '';
					if (!isConvValue(val)) continue;

					const id = extractIdFromValue(val);
					if (!id) continue;

					const name = (el.textContent || '').trim().split(/\n|\r|\s{2,}/)[0].trim();
					if (!name || name.length < 2 || SKIP_TEXTS.has(name.toLowerCase())) continue;
					addChat(id, name, srcFromValue(val));
				}
			};

			const clickSeeMore = () => {
				for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
					if ((el.textContent || '').trim().toLowerCase() === 'see more') {
						el.click(); return true;
					}
				}
				return false;
			};

			tree.scrollTop = 0;
			await sleep(400);
			let prevTotal = -1;
			let seeMoreTries = 0;
			for (let round = 0; round < 100; round++) {
				processTreeItems();
				if (seen.size === prevTotal) {
					if (seeMoreTries < 5 && clickSeeMore()) { seeMoreTries++; await sleep(1000); continue; }
					break;
				}
				prevTotal = seen.size;
				tree.scrollTop += 350;
				await sleep(300);
			}
		}

		return JSON.stringify({ count: chats.length, conversations: chats });
	}`

	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("erro ao varrer Teams: %w", err)
	}

	var raw struct {
		Count         int           `json:"count"`
		Conversations []ScannedChat `json:"conversations"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &raw); err != nil {
		return nil, fmt.Errorf("erro ao parsear resultado: %w", err)
	}

	logger.Info().
		Int("total", raw.Count).
		Msg("[Scanner] Varredura concluída")

	out := ScanResult{
		SearchedAt: time.Now().Format(time.RFC3339),
		Count:      len(raw.Conversations),
		Chats:      raw.Conversations,
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return nil, fmt.Errorf("erro ao salvar: %w", err)
	}
	logger.Info().Str("path", outputPath).Int("total", out.Count).Msg("[Scanner] Resultado salvo")
	return raw.Conversations, nil
}
