// Diagnóstico: despeja TODOS os itens data-fui-tree-item-value da sidebar do Teams.
// Uso: go run ./scripts/teams-diag-sidebar/
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

func main() {
	homeDir, _ := os.UserHomeDir()
	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "teams-session")

	fmt.Println("══ Teams — Diagnóstico de Sidebar ══")
	fmt.Printf("Sessão: %s\n\n", sessionDir)

	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "ERRO: sessão não encontrada")
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
		Set("no-default-browser-check")
	if chromeBin != "" {
		l = l.Bin(chromeBin)
	}

	ctrlURL, err := l.Launch()
	must(err, "Chrome")
	browser := rod.New().ControlURL(ctrlURL)
	must(browser.Connect(), "connect")
	defer browser.Close()

	_, err = browser.Page(proto.TargetCreateTarget{URL: "https://teams.microsoft.com/v2/"})
	must(err, "page")

	fmt.Println("Aguardando Teams (máx 3 min)...")
	page := waitTeams(browser, 3*time.Minute)
	if page == nil {
		fmt.Fprintln(os.Stderr, "timeout")
		os.Exit(1)
	}
	fmt.Println("Teams carregado. Estabilizando 20s...")
	time.Sleep(20 * time.Second)

	page = page.Timeout(2 * time.Minute)

	js := `async () => {
		const sleep = ms => new Promise(r => setTimeout(r, ms));

		// ── Tentar clicar na aba Chat se ainda não estiver ativa ─────────────
		const chatSelectors = [
			'[data-tid="app-bar-nav-chat"]',
			'[data-tid="app-bar-chat"]',
			'[aria-label="Chat"]',
			'nav [href*="/chat"]',
			'[data-tid*="chat"]',
		];
		for (const sel of chatSelectors) {
			const el = document.querySelector(sel);
			if (el) { el.click(); await sleep(2000); break; }
		}

		// ── Aguardar a tree aparecer (até 20s) ────────────────────────────────
		let tree = null;
		for (let i = 0; i < 40; i++) {
			tree = document.querySelector('[data-tid="simple-collab-dnd-rail"]');
			if (tree) break;
			await sleep(500);
		}
		if (!tree) {
			// Fallback: listar todos os data-tid disponíveis para diagnóstico
			const tids = [...document.querySelectorAll('[data-tid]')]
				.map(e => e.getAttribute('data-tid')).filter(Boolean);
			const unique = [...new Set(tids)].slice(0, 60);
			return JSON.stringify({ error: 'tree not found', availableTids: unique });
		}

		// Clicar em todas as pastas colapsadas
		for (let round = 0; round < 5; round++) {
			let expanded = false;
			for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
				if (el.getAttribute('aria-expanded') === 'false') {
					el.click();
					expanded = true;
					await sleep(600);
				}
			}
			if (!expanded) break;
			await sleep(500);
		}

		// Scroll completo para carregar itens lazy
		tree.scrollTop = 0;
		await sleep(300);
		for (let i = 0; i < 80; i++) {
			tree.scrollTop += 300;
			await sleep(150);
		}
		tree.scrollTop = 0;
		await sleep(500);

		// Coletar TODOS os itens sem nenhum filtro
		const items = [];
		for (const el of tree.querySelectorAll('[data-fui-tree-item-value]')) {
			const val      = el.getAttribute('data-fui-tree-item-value') || '';
			const expanded = el.getAttribute('aria-expanded');
			const text     = (el.textContent || '').trim().split(/\n|\r|\s{2,}/)[0].trim();
			items.push({
				type:     expanded === '' ? 'chat' : 'folder',
				expanded: expanded,
				text:     text.slice(0, 80),
				value:    val.slice(0, 200),
			});
		}

		// Também varrer IndexedDB à procura de self-chat (threads com 1 membro ou 28:)
		const selfChats = [];
		try {
			const allDbs = await indexedDB.databases().catch(() => []);
			for (const {name} of allDbs) {
				if (!name || !name.includes('conversation-manager')) continue;
				const db = await new Promise((res, rej) => {
					const r = indexedDB.open(name);
					r.onsuccess = e => res(e.target.result);
					r.onerror   = () => rej(r.error);
				});
				if (!db.objectStoreNames.contains('conversations')) { db.close(); continue; }
				const rows = await new Promise(res => {
					const tx  = db.transaction('conversations', 'readonly');
					const req = tx.objectStore('conversations').getAll();
					req.onsuccess = () => res(req.result || []);
					req.onerror   = () => res([]);
				});
				for (const row of rows) {
					const id = row.id || row.threadId || '';
					if (!id) continue;
					const members = row.members || [];
					// self-chat: 1 membro OU prefixo 28:
					if (members.length <= 1 || id.startsWith('28:')) {
						selfChats.push({
							id: id.slice(0, 100),
							members: members.length,
							topic: (row.threadProperties || {}).topic || '',
						});
					}
				}
				db.close();
				break;
			}
		} catch(e) {}

		return JSON.stringify({ itemCount: items.length, items, selfChats });
	}`

	res, err := page.Eval(js)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERRO eval: %v\n", err)
		os.Exit(1)
	}

	raw := res.Value.String()
	// CDP envolve em aspas — limpar
	raw = strings.Trim(raw, `"`)
	raw = strings.ReplaceAll(raw, `\"`, `"`)
	raw = strings.ReplaceAll(raw, `\\n`, "\n")

	var out struct {
		Error         string   `json:"error"`
		AvailableTids []string `json:"availableTids"`
		ItemCount     int      `json:"itemCount"`
		Items         []struct {
			Type     string `json:"type"`
			Expanded string `json:"expanded"`
			Text     string `json:"text"`
			Value    string `json:"value"`
		} `json:"items"`
		SelfChats []struct {
			ID      string `json:"id"`
			Members int    `json:"members"`
			Topic   string `json:"topic"`
		} `json:"selfChats"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		fmt.Println("raw:", raw[:min(len(raw), 500)])
		os.Exit(1)
	}
	if out.Error != "" {
		fmt.Println("ERRO JS:", out.Error)
		if len(out.AvailableTids) > 0 {
			fmt.Println("\ndata-tid disponíveis na página:")
			for _, tid := range out.AvailableTids {
				fmt.Println(" ", tid)
			}
		}
		os.Exit(1)
	}

	fmt.Printf("\n═══ SIDEBAR: %d itens ═══\n\n", out.ItemCount)
	for i, item := range out.Items {
		marker := "  chat  "
		if item.Type == "folder" {
			marker = "[PASTA] "
		}
		fmt.Printf("%3d %s %-40s  %s\n", i+1, marker, `"`+item.Text+`"`, item.Value)
	}

	fmt.Printf("\n═══ SELF-CHATS no IndexedDB: %d ═══\n", len(out.SelfChats))
	for _, sc := range out.SelfChats {
		fmt.Printf("  id=%s  members=%d  topic=%q\n", sc.ID, sc.Members, sc.Topic)
	}
}

func waitTeams(browser *rod.Browser, timeout time.Duration) *rod.Page {
	deadline := time.Now().Add(timeout)
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
				fmt.Printf("  URL: %s\n", u)
				return p
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func killChrome(sessionDir string) {
	out, _ := exec.Command("pgrep", "-f", sessionDir).Output()
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
		"/usr/bin/chromium-browser", "/usr/bin/chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium"} {
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
