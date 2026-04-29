// Coleta TODOS os chats do Teams (grupos + DMs) e salva em
// ~/.k8s-hpa-manager/teams-chats-all.json
//
// Uso:
//   go run ./scripts/teams-collect-all-chats/
package main

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
)

type ChatEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"`
}

func main() {
	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")
	outFile := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-chats-all.json")

	fmt.Println("══════════════════════════════════════════════════════")
	fmt.Println(" Teams — Coletor completo de chats (grupos + DMs)")
	fmt.Println("══════════════════════════════════════════════════════")
	fmt.Printf(" Sessão : %s\n", sessionDir)
	fmt.Printf(" Saída  : %s\n\n", outFile)

	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "ERRO: sessão não existe")
		os.Exit(1)
	}

	killChrome(sessionDir)
	time.Sleep(800 * time.Millisecond)

	chromeBin := findChrome()
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
		fmt.Printf(" Chrome : %s\n", chromeBin)
	}

	ctrlURL, err := l.Launch()
	must(err, "iniciar Chrome")

	browser := rod.New().ControlURL(ctrlURL)
	must(browser.Connect(), "conectar ao browser")
	defer browser.Close()

	page, err := browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	must(err, "criar página")

	fmt.Println("\n[1/4] Aguardando Teams carregar (máx 3 min)...")
	page = waitTeams(browser, page, 3*time.Minute)
	if page == nil {
		fmt.Fprintln(os.Stderr, "ERRO: timeout aguardando Teams")
		os.Exit(1)
	}
	fmt.Println("      Teams carregado — aguardando estabilizar (20s)...")
	time.Sleep(20 * time.Second)

	seen := map[string]ChatEntry{}

	// ── Fase 1: IndexedDB completo (conversation-manager + contacts + folders) ──
	fmt.Println("\n[2/4] IndexedDB — varredura em todos os bancos relevantes...")
	idbChats := scanAllIndexedDB(page)
	fmt.Printf("      Chats capturados: %d\n", len(idbChats))
	for _, c := range idbChats {
		seen[c.ID] = c
	}

	// ── Fase 2: Navegar para aba Chat ─────────────────────────────────────────
	fmt.Println("\n[3/5] Navegando para aba Chat...")
	if clicked, sel := clickChatTab(page); clicked {
		fmt.Printf("      Aba Chat clicada via: %s\n", sel)
		time.Sleep(5 * time.Second) // esperar lista carregar
	}

	// ── Fase 3: DOM — tree scroll + data-fui-tree-item-value ─────────────────
	fmt.Println("\n[4/4] DOM — sidebar scroll + data-fui-tree-item-value...")
	fmt.Println("      Aguarde 1-3 minutos.")

	domChats, listInfo := scrollAndCapture(page)
	fmt.Printf("      Container: %s\n", listInfo)

	newDMs := 0
	for _, c := range domChats {
		if _, exists := seen[c.ID]; !exists {
			seen[c.ID] = c
			newDMs++
		}
	}
	fmt.Printf("      Itens via DOM: %d (novos: %d)\n", len(domChats), newDMs)

	// ── Resultado ─────────────────────────────────────────────────────────────
	results := make([]ChatEntry, 0, len(seen))
	for _, c := range seen {
		results = append(results, c)
	}

	bySource := map[string]int{}
	for _, c := range results {
		bySource[c.Source]++
	}
	fmt.Printf("\n══ TOTAL: %d chats — %v ══\n", len(results), bySource)

	out, _ := json.MarshalIndent(map[string]interface{}{
		"searched_at": time.Now().Format(time.RFC3339),
		"count":       len(results),
		"chats":       results,
	}, "", "  ")
	must(os.WriteFile(outFile, out, 0600), "salvar arquivo")
	fmt.Printf("\n Salvo em: %s\n", outFile)
}

// scanAllIndexedDB varre conversation-manager, capiv3-contacts-manager,
// conversation-folder-manager e faz uma busca genérica por thread IDs em todos os bancos.
func scanAllIndexedDB(page *rod.Page) []ChatEntry {
	js := `async () => {
		const openDB = n => new Promise((res, rej) => {
			const r = indexedDB.open(n);
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

		const isId = id =>
			typeof id === 'string' && id.length > 10 &&
			(id.startsWith('19:') || id.startsWith('28:') || id.startsWith('48:'));
		const membersName = arr =>
			Array.isArray(arr)
				? arr.map(m => m.displayName || m.name || '').filter(Boolean).join(', ')
				: '';
		// Extrai o primeiro thread ID de uma string JSON
		const extractId = s => {
			const m = s.match(/"((?:19|28|48):[^"]{15,})"/);
			return m ? m[1] : '';
		};

		const allDbs = await indexedDB.databases().catch(() => []);
		const seen  = new Set();
		const chats = [];
		const addChat = (id, name, src) => {
			if (!id || !name || seen.has(id)) return;
			seen.add(id); chats.push({id, display_name: name, source: src});
		};

		// ── 1. conversation-manager: grupos (topic) e DMs (lastMessage.imdisplayname) ──
		for (const {name} of allDbs) {
			if (!name || !name.includes('conversation-manager')) continue;
			try {
				const db = await openDB(name);
				if (!db.objectStoreNames.contains('conversations')) { db.close(); continue; }
				const rows = await getAll(db, 'conversations');

				for (const row of rows) {
					const id = row.id || row.threadId || '';
					if (!id || !isId(id)) continue;
					const tp    = row.threadProperties || {};
					const topic = tp.topic || tp.name || tp.displayName ||
					              row.topic || row.name || row.displayName || '';
					// IndexedDB: apenas grupos com nome definido.
					// DMs e contatos são extraídos pela DOM tree (nome correto do contato).
					if (topic) addChat(id, topic, 'group');
				}
				db.close();
				break;
			} catch {}
		}

		// ── 2. capiv3-contacts-manager: contatos com threadId de DM ──
		for (const {name} of allDbs) {
			if (!name || !name.includes('capiv3-contacts-manager')) continue;
			try {
				const db = await openDB(name);
				for (const storeName of db.objectStoreNames) {
					const rows = await getAll(db, storeName);
					for (const row of rows) {
						const displayName = row.displayName || row.displayname || row.name || '';
						if (!displayName) continue;
						// thread ID pode estar em vários campos
						const tid = row.threadId || row.thread_id || row.conversationId ||
						            row.chatId   || row.mri || '';
						if (tid && isId(tid)) {
							addChat(tid, displayName, 'contacts');
						}
					}
				}
				db.close();
				break;
			} catch {}
		}

		// ── 3. conversation-folder-manager: conversas organizadas em pastas ──
		for (const {name} of allDbs) {
			if (!name || !name.includes('conversation-folder-manager')) continue;
			try {
				const db = await openDB(name);
				for (const storeName of db.objectStoreNames) {
					const rows = await getAll(db, storeName);
					for (const row of rows) {
						const id   = row.id || row.threadId || row.conversationId || '';
						const dname = row.displayName || row.name || row.topic || '';
						if (id && isId(id) && dname) {
							addChat(id, dname, 'folder');
						} else if (id && isId(id)) {
							// Sem nome: guardar como referência — servirá de fallback
							addChat(id, id.substring(3, 20) + '...', 'folder-noname');
						}
					}
				}
				db.close();
				break;
			} catch {}
		}

		// ── 4. buddy-manager: lista de contatos ──
		for (const {name} of allDbs) {
			if (!name || !name.includes('buddy-manager')) continue;
			try {
				const db = await openDB(name);
				for (const storeName of db.objectStoreNames) {
					const rows = await getAll(db, storeName);
					for (const row of rows) {
						const displayName = row.displayName || row.displayname || row.name || '';
						const tid = row.threadId || row.thread_id || row.conversationId || '';
						if (tid && isId(tid) && displayName) {
							addChat(tid, displayName, 'buddy');
						}
					}
				}
				db.close();
				break;
			} catch {}
		}

		// ── 5. p2p-shared-manager: DMs peer-to-peer ──
		for (const {name} of allDbs) {
			if (!name || !name.includes('p2p-shared-manager')) continue;
			try {
				const db = await openDB(name);
				for (const storeName of db.objectStoreNames) {
					const rows = await getAll(db, storeName);
					for (const row of rows) {
						const id = row.threadId || row.thread_id || row.conversationId || row.id || '';
						const dname = row.displayName || row.displayname || row.name || row.title || '';
						if (id && isId(id) && dname) addChat(id, dname, 'p2p');
					}
				}
				db.close();
				break;
			} catch {}
		}

		// ── 6. Varredura genérica: qualquer banco com thread IDs + displayName ──
		// Cobre casos não previstos acima. Procura em TODOS os bancos restantes.
		const scannedNames = new Set([
			'conversation-manager', 'capiv3-contacts-manager',
			'conversation-folder-manager', 'buddy-manager', 'p2p-shared-manager',
		]);
		for (const {name} of allDbs) {
			if (!name) continue;
			const prefix = name.split(':')[1] || '';  // parte após "Teams:"
			if (scannedNames.has(prefix)) continue;
			// Só escanear bancos que têm "manager" no nome — evitar telemetria, cache, etc.
			if (!name.includes('-manager:') && !name.includes('profiles')) continue;
			try {
				const db = await openDB(name);
				for (const storeName of db.objectStoreNames) {
					const rows = await getAll(db, storeName);
					for (const row of rows) {
						// Procurar thread ID em qualquer campo
						const id = row.threadId || row.thread_id || row.conversationId ||
						           row.chatId   || row.id        || '';
						const displayName = row.displayName || row.displayname || row.name ||
						                    row.title       || '';
						if (id && isId(id) && displayName && displayName.length > 1) {
							addChat(id, displayName, prefix.replace(/-manager$/, ''));
						}
					}
				}
				db.close();
			} catch {}
		}

		return JSON.stringify(chats);
	}`

	res, err := page.Eval(js)
	if err != nil {
		fmt.Printf("      IndexedDB eval erro: %v\n", err)
		return nil
	}
	var chats []ChatEntry
	if err := json.Unmarshal([]byte(res.Value.String()), &chats); err != nil {
		fmt.Printf("      IndexedDB parse erro: %v\n", err)
		return nil
	}
	return chats
}

// diagnoseDOMStart_REMOVED — diagnóstico concluído, função removida.
// Ver histórico git se necessário reativar.
func diagnoseDOMStart_REMOVED(page *rod.Page) {
	// ── Parte A: expandir "Recent Chats" e capturar valores da tree ──────────
	jsDOM := `async () => {
		const sleep = ms => new Promise(r => setTimeout(r, ms));
		const tree = document.querySelector('[data-tid="simple-collab-dnd-rail"]');
		if (!tree) return JSON.stringify({error: 'tree not found'});

		// Encontrar e expandir "Recent Chats"
		let expandedRC = false;
		const rcEl = tree.querySelector('[data-fui-tree-item-value*="RecentChats"]');
		if (rcEl) {
			const exp = rcEl.getAttribute('aria-expanded');
			if (exp !== 'true') { rcEl.click(); await sleep(2000); expandedRC = true; }
		}

		// Coletar TODOS os [data-fui-tree-item-value] na tree
		const allItems = [...tree.querySelectorAll('[data-fui-tree-item-value]')];
		const items = allItems.map(el => ({
			val:      (el.getAttribute('data-fui-tree-item-value') || '').substring(0, 120),
			expanded: el.getAttribute('aria-expanded') || '',
			text:     (el.textContent || '').trim().substring(0, 60).replace(/\s+/g, ' '),
		}));

		return JSON.stringify({
			expanded_rc: expandedRC,
			tree_h: tree.clientHeight,
			tree_scrollH: tree.scrollHeight,
			item_count: items.length,
			items,
		});
	}`

	// ── Parte B: contacts + memberProperties + lastMessage dos DMs ──────────
	jsIDB := `async () => {
		const openDB = n => new Promise((res, rej) => {
			const r = indexedDB.open(n);
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
		const allDbs = await indexedDB.databases().catch(() => []);

		// ── contacts: primeiras 3 linhas para ver estrutura ──
		let contactCount = 0;
		let contactSample = [];
		for (const {name} of allDbs) {
			if (!name || !name.includes('capiv3-contacts-manager')) continue;
			try {
				const db = await openDB(name);
				for (const storeName of db.objectStoreNames) {
					const rows = await getAll(db, storeName);
					contactCount += rows.length;
					for (const row of rows.slice(0, 3)) {
						contactSample.push({
							keys: Object.keys(row),
							mri:  row.mri  || '',
							id:   row.id   || '',
							dn:   row.displayName || row.displayname || row.name || '',
						});
					}
					if (contactSample.length >= 3) break;
				}
				db.close();
				break;
			} catch {}
		}

		// ── DMs: memberProperties + member.id + lastMessage ──
		const isId = id => typeof id === 'string' && id.length > 10 &&
			(id.startsWith('19:') || id.startsWith('28:') || id.startsWith('48:'));
		let dmCount = 0;
		let dmSamples = [];
		for (const {name} of allDbs) {
			if (!name || !name.includes('conversation-manager')) continue;
			try {
				const db = await openDB(name);
				if (!db.objectStoreNames.contains('conversations')) { db.close(); continue; }
				const rows = await getAll(db, 'conversations');
				for (const row of rows) {
					const id = row.id || '';
					if (!id || !isId(id)) continue;
					const tp    = row.threadProperties || {};
					const topic = tp.topic || tp.name || '';
					if (topic) continue;
					dmCount++;
					if (dmSamples.length < 3) {
						const members = row.members || [];
						const mp = row.memberProperties;
						const lm = row.lastMessage;
						dmSamples.push({
							id:          id.substring(0, 50),
							member_ids:  members.slice(0, 4).map(m => (m.id || '').substring(0, 50)),
							mp_type:     typeof mp,
							mp_sample:   JSON.stringify(mp).substring(0, 300),
							lm_keys:     lm ? Object.keys(lm) : [],
							lm_imdisplay: lm ? (lm.imdisplayname || lm.displayname || '') : '',
							lm_from:      lm ? JSON.stringify(lm.from || '').substring(0, 100) : '',
						});
					}
				}
				db.close();
				break;
			} catch {}
		}

		return JSON.stringify({contact_count: contactCount, contact_sample: contactSample, dm_count: dmCount, dm_samples: dmSamples});
	}`

	resDOM, errDOM := page.Eval(jsDOM)
	resIDB, errIDB := page.Eval(jsIDB)

	// ── Parte A: output ──────────────────────────────────────────────────────
	if errDOM != nil {
		fmt.Printf("      DOM erro: %v\n", errDOM)
	} else {
		var d struct {
			Error      string `json:"error"`
			ExpandedRC bool   `json:"expanded_rc"`
			TreeH      int    `json:"tree_h"`
			TreeScrollH int   `json:"tree_scrollH"`
			ItemCount  int    `json:"item_count"`
			Items      []struct {
				Val      string `json:"val"`
				Expanded string `json:"expanded"`
				Text     string `json:"text"`
			} `json:"items"`
		}
		raw := resDOM.Value.String()
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			fmt.Printf("      DOM parse erro: %s\n", raw[:min(300, len(raw))])
		} else if d.Error != "" {
			fmt.Printf("      DOM: %s\n", d.Error)
		} else {
			fmt.Printf("      Tree h=%d scrollH=%d | RecentChats expandido=%v | itens=%d\n",
				d.TreeH, d.TreeScrollH, d.ExpandedRC, d.ItemCount)
			fmt.Println("      data-fui-tree-item-value de cada item:")
			for i, it := range d.Items {
				fmt.Printf("        [%d] expanded=%q text=%q\n            val=%s\n",
					i+1, it.Expanded, it.Text, it.Val)
			}
		}
	}

	// ── Parte B: output ──────────────────────────────────────────────────────
	if errIDB != nil {
		fmt.Printf("      IDB erro: %v\n", errIDB)
	} else {
		var idb struct {
			ContactCount  int `json:"contact_count"`
			ContactSample []struct {
				Keys []string `json:"keys"`
				Mri  string   `json:"mri"`
				ID   string   `json:"id"`
				Dn   string   `json:"dn"`
			} `json:"contact_sample"`
			DmCount   int `json:"dm_count"`
			DmSamples []struct {
				ID          string   `json:"id"`
				MemberIDs   []string `json:"member_ids"`
				MpType      string   `json:"mp_type"`
				MpSample    string   `json:"mp_sample"`
				LmKeys      []string `json:"lm_keys"`
				LmImdisplay string   `json:"lm_imdisplay"`
				LmFrom      string   `json:"lm_from"`
			} `json:"dm_samples"`
		}
		raw := resIDB.Value.String()
		if err := json.Unmarshal([]byte(raw), &idb); err != nil {
			fmt.Printf("      IDB parse erro: %s\n", raw[:min(400, len(raw))])
		} else {
			fmt.Printf("\n      Contacts: %d total\n", idb.ContactCount)
			for i, c := range idb.ContactSample {
				fmt.Printf("        [%d] keys=%v  mri=%q  id=%q  dn=%q\n", i+1, c.Keys, c.Mri, c.ID, c.Dn)
			}
			fmt.Printf("\n      DMs no IndexedDB: %d (sem topic)\n", idb.DmCount)
			for i, s := range idb.DmSamples {
				fmt.Printf("      DM[%d] %s\n", i+1, s.ID)
				fmt.Printf("        member_ids: %v\n", s.MemberIDs)
				fmt.Printf("        memberProperties (%s): %s\n", s.MpType, s.MpSample)
				fmt.Printf("        lastMessage.keys=%v  imdisplayname=%q  from=%s\n",
					s.LmKeys, s.LmImdisplay, s.LmFrom)
			}
		}
	}
}

// scrollAndCapture percorre simple-collab-dnd-rail (role=tree) usando data-fui-tree-item-value.
// O Teams v2 armazena o thread ID no sufixo desse atributo (após ~RecentChats~).
func scrollAndCapture(page *rod.Page) ([]ChatEntry, string) {
	page = page.Timeout(5 * time.Minute)

	js := `async () => {
		const sleep = ms => new Promise(r => setTimeout(r, ms));
		const isId  = id =>
			typeof id === 'string' && id.length > 10 &&
			(id.startsWith('19:') || id.startsWith('28:') || id.startsWith('48:'));

		const seen  = new Set();
		const chats = [];
		const add   = (id, name, src) => {
			if (id && name && !seen.has(id)) { seen.add(id); chats.push({id, display_name: name, source: src}); }
		};

		// Teams v2: tipo e ID do chat estão no data-fui-tree-item-value.
		// Conversas contêm OneGQL_*Conversation|; o ID vem após o último |.
		// NÃO usar aria-expanded — todos os itens têm true/false, inclusive conversas.
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
		const srcFromValue = val => {
			if (val.includes('OneOnOneChat'))  return 'dm';
			if (val.includes('GroupChat'))     return 'group';
			if (val.includes('MeetingChat'))   return 'meeting';
			if (val.includes('SelfChat'))      return 'self';
			return 'dom-tree';
		};

		// ── 1. Encontrar a tree de chats ─────────────────────────────────────
		const tree = document.querySelector('[data-tid="simple-collab-dnd-rail"]');
		if (!tree) return JSON.stringify({chats: [], desc: 'tree not found', total: 0});

		// ── 2. Expandir Favorites e RecentChats ───────────────────────────────
		for (const folder of ['RecentChats', 'Favorites']) {
			const el = tree.querySelector('[data-fui-tree-item-value*="' + folder + '"]');
			if (el && el.getAttribute('aria-expanded') !== 'true') {
				el.click();
				await sleep(1000);
			}
		}

		const SKIP_TEXTS = new Set([
			'see more','see all your teams','mentions','followed threads',
			'drafts','quick views','chats','favorites','copilot','activity',
			'calendar','calls','new message',
		]);

		// Processa itens de conversa pelo conteúdo do data-fui-tree-item-value
		const processItems = () => {
			for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
				const val = el.getAttribute('data-fui-tree-item-value') || '';
				if (!isConvValue(val)) continue;

				const id = extractIdFromValue(val);
				if (!id) continue;

				// Nome: primeiro bloco de texto do elemento (sem texto dos filhos se houver)
				const rawText = (el.textContent || '').trim();
				// Pegar apenas a primeira linha não-vazia
				const name = rawText.split(/\n|\r|  {2,}/)[0].trim();
				if (!name || name.length < 2 || SKIP_TEXTS.has(name.toLowerCase())) continue;

				add(id, name, 'dom-tree');
			}
		};

		// ── 3. Clicar "See more" para carregar todos os chats recentes ─────────
		const clickSeeMore = () => {
			for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
				const t = (el.textContent || '').trim().toLowerCase();
				if (t === 'see more') { el.click(); return true; }
			}
			return false;
		};

		// ── 4. Loop: processar + scroll + see-more ────────────────────────────
		tree.scrollTop = 0;
		await sleep(400);

		let prevTotal = -1;
		let seeMoreClicks = 0;
		for (let round = 0; round < 100; round++) {
			processItems();

			if (seen.size === prevTotal) {
				// Tentar "See more" para carregar mais itens
				if (seeMoreClicks < 5 && clickSeeMore()) {
					seeMoreClicks++;
					await sleep(1200);
					continue;
				}
				break; // sem novos itens
			}
			prevTotal = seen.size;
			tree.scrollTop += 350;
			await sleep(300);
		}

		const desc = 'tree h=' + tree.clientHeight + ' scrollH=' + tree.scrollHeight + ' seeMore=' + seeMoreClicks;
		return JSON.stringify({chats, desc, total: chats.length});
	}`

	res, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Sprintf("eval erro: %v", err)
	}
	var out struct {
		Chats []ChatEntry `json:"chats"`
		Desc  string      `json:"desc"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return nil, fmt.Sprintf("parse erro: %v", err)
	}
	return out.Chats, out.Desc
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clickChatTab(page *rod.Page) (bool, string) {
	res, err := page.Eval(`() => {
		for (const sel of ['[data-tid="app-bar-chat-item"]','[aria-label="Chat"][role="button"]','[aria-label="Chat"]']) {
			const el = document.querySelector(sel);
			if (el) { el.click(); return sel; }
		}
		for (const el of document.querySelectorAll('[role="button"],[role="tab"],button,a')) {
			const t = (el.textContent || el.getAttribute('aria-label') || '').trim();
			if (t.toLowerCase() === 'chat') { el.click(); return 'text:' + t; }
		}
		return null;
	}`)
	if err != nil || res.Value.Nil() {
		return false, ""
	}
	v := res.Value.String()
	return v != "null" && v != "", v
}

func waitTeams(browser *rod.Browser, _ *rod.Page, timeout time.Duration) *rod.Page {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pages, _ := browser.Pages()
		for _, p := range pages {
			info, err := p.Info()
			if err != nil {
				continue
			}
			u := info.URL
			if u == "" || u == "about:blank" ||
				!strings.Contains(u, "teams.microsoft.com") ||
				strings.Contains(u, "/error") ||
				strings.Contains(u, "login.microsoftonline") {
				continue
			}
			fmt.Printf("      URL: %s\n", u)
			return p
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func killChrome(sessionDir string) {
	out, err := exec.Command("pgrep", "-f", sessionDir).Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, pid := range strings.Fields(string(out)) {
		exec.Command("kill", "-TERM", pid).Run() //nolint:errcheck
	}
	time.Sleep(500 * time.Millisecond)
	out2, _ := exec.Command("pgrep", "-f", sessionDir).Output()
	for _, pid := range strings.Fields(string(out2)) {
		exec.Command("kill", "-9", pid).Run() //nolint:errcheck
	}
}

func findChrome() string {
	for _, p := range []string{
		"/usr/bin/google-chrome-stable", "/usr/bin/google-chrome",
		"/usr/bin/chromium-browser", "/usr/bin/chromium", "/snap/bin/chromium",
	} {
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

func must(err error, ctx string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRO (%s): %v\n", ctx, err)
		os.Exit(1)
	}
}
