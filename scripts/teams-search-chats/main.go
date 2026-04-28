// Script standalone para buscar chats com determinado termo na sidebar do Teams
// e salvar os IDs em ~/.k8s-hpa-manager/teams-chats-<query>.json
//
// Uso:
//   go run ./scripts/teams-search-chats/ "SRE +"
//   go run ./scripts/teams-search-chats/          # usa "SRE +" como padrão
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
	query := "SRE +"
	if len(os.Args) > 1 {
		query = strings.Join(os.Args[1:], " ")
	}

	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")
	slug := strings.NewReplacer(" ", "-", "+", "plus", "/", "-").Replace(strings.ToLower(query))
	outFile := filepath.Join(homeDir, ".k8s-hpa-manager", fmt.Sprintf("teams-chats-%s.json", slug))

	fmt.Printf("[Teams] Buscando chats com %q\n", query)
	fmt.Printf("[Teams] Sessão : %s\n", sessionDir)
	fmt.Printf("[Teams] Saída  : %s\n\n", outFile)

	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "ERRO: sessão não existe — abra o Teams pelo app primeiro")
		os.Exit(1)
	}

	killExistingChrome(sessionDir)
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
		fmt.Printf("[Teams] Chrome : %s\n", chromeBin)
	}

	ctrlURL, err := l.Launch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRO ao iniciar Chrome: %v\n", err)
		os.Exit(1)
	}

	browser := rod.New().ControlURL(ctrlURL)
	if err := browser.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "ERRO ao conectar: %v\n", err)
		os.Exit(1)
	}
	defer browser.Close()

	page, err := browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRO ao criar página: %v\n", err)
		os.Exit(1)
	}

	// Aguardar Teams carregar — polling simples na URL atual + novas abas (MCAS)
	fmt.Println("[Teams] Aguardando Teams carregar (máx 3 min)...")
	activePage := waitForTeams(browser, page, 3*time.Minute)
	if activePage == nil {
		fmt.Fprintln(os.Stderr, "ERRO: timeout aguardando Teams")
		os.Exit(1)
	}
	page = activePage
	fmt.Println("[Teams] Teams carregado — aguardando IndexedDB popular (30s)...")
	time.Sleep(30 * time.Second)

	found := map[string]ChatEntry{}

	// --- Estratégia 1: IndexedDB (conversation-manager) ---
	fmt.Println("[Teams] [1/2] Consultando IndexedDB...")
	idbChats, total, err := searchIndexedDB(page, query)
	if err != nil {
		fmt.Printf("[Teams] IndexedDB erro: %v\n", err)
	} else {
		fmt.Printf("[Teams] IndexedDB: %d conversa(s) no banco, %d match(es) para %q\n", total, len(idbChats), query)
		for _, c := range idbChats {
			key := c.ID
			if key == "" {
				key = "noID:" + c.DisplayName
			}
			found[key] = c
		}
	}

	// --- Estratégia 2: Filtro da sidebar ---
	fmt.Println("[Teams] [2/2] Tentando filtro da sidebar...")
	sidebarChats, err := searchSidebar(page, query)
	if err != nil {
		fmt.Printf("[Teams] Sidebar erro: %v\n", err)
	} else {
		fmt.Printf("[Teams] Sidebar: %d resultado(s)\n", len(sidebarChats))
		for _, c := range sidebarChats {
			key := c.ID
			if key == "" {
				key = "noID:" + c.DisplayName
			}
			if _, exists := found[key]; !exists {
				found[key] = c
			}
		}
	}

	results := make([]ChatEntry, 0, len(found))
	for _, c := range found {
		results = append(results, c)
	}

	fmt.Printf("\n[Teams] ══ Resultado: %d chat(s) ══\n", len(results))
	for _, c := range results {
		fmt.Printf("  [%s]  %s  (id: %s)\n", c.Source, c.DisplayName, c.ID)
	}

	out, _ := json.MarshalIndent(map[string]interface{}{
		"query":       query,
		"searched_at": time.Now().Format(time.RFC3339),
		"count":       len(results),
		"chats":       results,
	}, "", "  ")
	if err := os.WriteFile(outFile, out, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "ERRO ao salvar: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[Teams] Salvo em: %s\n", outFile)
}

// searchIndexedDB consulta o conversation-manager e retorna chats cujo topic contém query.
// Retorna também o total de conversas encontradas para debug.
func searchIndexedDB(page *rod.Page, query string) ([]ChatEntry, int, error) {
	queryLower := strings.ToLower(query)

	js := fmt.Sprintf(`async () => {
		const queryLower = %q;

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
		const dbNames = dbs.map(d => d.name || '');
		const matches = [];
		let total = 0;

		// Tentar todos os DBs do conversation-manager (qualquer sufixo de locale)
		for (const {name} of dbs) {
			if (!name || !name.includes('conversation-manager')) continue;

			try {
				const db = await openDB(name);
				if (!db.objectStoreNames.contains('conversations')) { db.close(); continue; }
				const rows = await getAll(db, 'conversations');
				total += rows.length;

				for (const row of rows) {
					const tp = row.threadProperties || {};
					// Tentar vários campos possíveis para o nome da conversa
					const topic = tp.topic || tp.name || tp.displayName ||
					              row.topic || row.name || row.displayName || '';
					const id = row.id || row.threadId || '';
					const topicLower = topic.toLowerCase();

					if (topicLower.includes(queryLower)) {
						matches.push({
							id,
							display_name: topic,
							source: 'indexeddb',
						});
					}
				}
				db.close();
			} catch(e) {}
		}

		return JSON.stringify({ total, matches, db_names: dbNames });
	}`, queryLower)

	res, err := page.Eval(js)
	if err != nil {
		return nil, 0, err
	}

	var out struct {
		Total   int         `json:"total"`
		Matches []ChatEntry `json:"matches"`
		DBNames []string    `json:"db_names"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return nil, 0, err
	}

	// Debug: listar nomes dos DBs encontrados
	fmt.Printf("[Teams] IndexedDB — bancos encontrados: %s\n",
		strings.Join(filterConvDBs(out.DBNames), ", "))

	return out.Matches, out.Total, nil
}

func filterConvDBs(names []string) []string {
	var r []string
	for _, n := range names {
		if strings.Contains(n, "conversation") || strings.Contains(n, "skype") {
			r = append(r, n)
		}
	}
	if len(r) == 0 {
		return names // mostrar todos se nenhum bater
	}
	return r
}

// searchSidebar usa o campo de filtro da lista de chats na sidebar do Teams.
func searchSidebar(page *rod.Page, query string) ([]ChatEntry, error) {
	queryLower := strings.ToLower(query)

	// Tentar focar e preencher o input de filtro
	fillJS := fmt.Sprintf(`async () => {
		const sels = [
			'[data-tid="chat-filter-input"]',
			'[placeholder*="Filter"]',
			'[placeholder*="Search"]',
			'[placeholder*="Pesquisar"]',
			'[placeholder*="Filtrar"]',
			'input[type="search"]',
			'[role="searchbox"]',
		];
		let input = null;
		for (const s of sels) {
			const el = document.querySelector(s);
			if (el) { input = el; break; }
		}
		if (!input) {
			// Listar todos os inputs visíveis para debug
			const inputs = Array.from(document.querySelectorAll('input')).map(i => ({
				type: i.type, placeholder: i.placeholder,
				dataTid: i.getAttribute('data-tid'), role: i.getAttribute('role'),
			}));
			return JSON.stringify({ok: false, inputs});
		}

		input.focus();
		input.click();
		input.value = '';
		input.dispatchEvent(new Event('input', {bubbles: true}));
		await new Promise(r => setTimeout(r, 300));

		const q = %q;
		for (const ch of q) {
			input.value += ch;
			input.dispatchEvent(new InputEvent('input', {data: ch, bubbles: true, inputType: 'insertText'}));
			await new Promise(r => setTimeout(r, 60));
		}
		return JSON.stringify({ok: true, value: input.value,
			dataTid: input.getAttribute('data-tid'), placeholder: input.placeholder});
	}`, query)

	fillRes, err := page.Eval(fillJS)
	if err != nil {
		return nil, fmt.Errorf("fill: %w", err)
	}
	fmt.Printf("[Teams] Sidebar fill: %s\n", fillRes.Value.String())

	time.Sleep(3 * time.Second)

	// Extrair itens da lista filtrada
	extractJS := fmt.Sprintf(`() => {
		const queryLower = %q;
		const results = [];
		const seen = new Set();

		const getThreadId = (el) => {
			// Atributos diretos
			for (const attr of ['data-thread-id','data-convid','data-conversation-id']) {
				const v = el.getAttribute(attr);
				if (v && v.includes('@thread')) return v;
			}
			// Elementos filhos
			for (const attr of ['data-thread-id','data-convid','data-conversation-id']) {
				const child = el.querySelector('[' + attr + ']');
				if (child) {
					const v = child.getAttribute(attr);
					if (v && v.includes('@thread')) return v;
				}
			}
			// href de link filho
			const link = el.querySelector('a[href*="conversations"]');
			if (link) {
				const m = (link.href || '').match(/conversations\/([^?&#\/]+)/);
				if (m) return decodeURIComponent(m[1]);
			}
			return '';
		};

		const getName = (el) => {
			const nameEl = el.querySelector(
				'[class*="title"],[class*="displayName"],[class*="chatTitle"],[data-tid*="title"]'
			);
			if (nameEl) return nameEl.textContent.trim().split('\n')[0];
			const aria = el.getAttribute('aria-label');
			if (aria) return aria.trim().split('\n')[0];
			return el.textContent.trim().split('\n')[0].substring(0, 100);
		};

		const listSelectors = [
			'[data-tid="chat-list-item"]',
			'[class*="chatListItem"]',
			'[class*="listItemContent"]',
			'[role="listitem"]',
		];

		for (const sel of listSelectors) {
			const items = document.querySelectorAll(sel);
			if (!items.length) continue;
			for (const item of items) {
				const name = getName(item);
				if (!name.toLowerCase().includes(queryLower)) continue;
				const id = getThreadId(item);
				const key = id || name;
				if (seen.has(key)) continue;
				seen.add(key);
				results.push({id, display_name: name, source: 'sidebar'});
			}
			if (results.length) break;
		}
		return JSON.stringify(results);
	}`, queryLower)

	extractRes, err := page.Eval(extractJS)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	var entries []ChatEntry
	if err := json.Unmarshal([]byte(extractRes.Value.String()), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// waitForTeams aguarda o Teams v2 carregar, retornando a aba ativa.
// Verifica apenas a URL — igual ao discover.go original — sem checar seletores DOM
// que variam a cada versão do Teams.
func waitForTeams(browser *rod.Browser, _ *rod.Page, timeout time.Duration) *rod.Page {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pages, _ := browser.Pages()
		for _, p := range pages {
			info, err := p.Info()
			if err != nil {
				continue
			}
			url := info.URL
			if url == "" || url == "about:blank" {
				continue
			}
			if !strings.Contains(url, "teams.microsoft.com") {
				continue
			}
			if strings.Contains(url, "/error") || strings.Contains(url, "/eoa") ||
				strings.Contains(url, "login.microsoftonline") {
				continue
			}
			fmt.Printf("[Teams] URL detectada: %s\n", url)
			return p
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func killExistingChrome(sessionDir string) {
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

func findSystemChrome() string {
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
