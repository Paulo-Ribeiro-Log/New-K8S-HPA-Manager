package teams

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/browser"
)

// teamsEmbedBrowserEnvVar, quando setada como "true"/"1", força o Teams a usar o Chromium
// embutido do Rod em vez do Chrome do sistema — Fase 2 de BROWSER-CONSOLIDATION-STUDY.md.
// Existe pra validar empiricamente se o SPA pesado do Teams v2 se comporta igual num Chromium
// genérico sem branding "Google Chrome" (hipóteses da seção 3 do estudo — fingerprinting/
// detecção de automação, robustez de renderização). Reversível: sem a env var, comportamento
// idêntico a antes (prefere o Chrome do sistema, cai pro embed só se não achar nenhum).
const teamsEmbedBrowserEnvVar = "K8S_HPA_TEAMS_EMBED_BROWSER"

func teamsForceEmbedBrowser() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(teamsEmbedBrowserEnvVar)))
	return v == "true" || v == "1"
}

// operationMu serializa RunDiscovery, ScanConversations e SendBatch entre si — abas paralelas no
// mesmo perfil podem invalidar o SkypeToken (mesmo risco documentado no extractMu do ServiceNow,
// internal/servicenow/rod_extractor.go). browserMgr é o Chrome persistente reaproveitado pelas
// três operações (Fase 1 de BROWSER-CONSOLIDATION-STUDY.md — implementação movida pra
// internal/browser, que também é usada pelo ServiceNow).
var (
	operationMu sync.Mutex
	browserMgr  browser.Manager
)

// getBrowser retorna o Chrome persistente da sessão do Teams, lançando um processo novo só na
// primeira chamada ou se o anterior morreu. Antes, RunDiscovery/ScanConversations/SendBatch
// matavam e relançavam o Chrome do zero (kill+launch+connect) a cada chamada — reaproveitar o
// mesmo processo entre elas elimina esse cold-start, mesmo padrão já usado pelo ServiceNow
// (RodExtractor.getBrowser, "N CHGs = 1 browser").
func getBrowser(sessionDir string, logger *zerolog.Logger) (*rod.Browser, error) {
	var chromeBin string
	if teamsForceEmbedBrowser() {
		logger.Info().Str("env", teamsEmbedBrowserEnvVar).
			Msg("[Teams] Forçando Chromium embutido do Rod (Fase 2 de BROWSER-CONSOLIDATION-STUDY.md) — ignorando Chrome do sistema mesmo se disponível")
	} else if bin := browser.FindSystemChrome(); bin != "" {
		chromeBin = bin
		logger.Info().Str("bin", chromeBin).Msg("[Teams] Usando Chrome do sistema")
	} else {
		logger.Warn().Msg("[Teams] Chrome do sistema não encontrado — usando Chromium do Rod")
	}

	opts := browser.LaunchOptions{
		SessionDir:  sessionDir,
		Headless:    false,
		SystemBin:   chromeBin,
		DeleteFlags: []string{"enable-automation"},
		Flags: map[string]string{
			// Sem valor: launcher gera --flag (sem "="), correto para flags booleanas
			"disable-blink-features":   "AutomationControlled",
			"no-first-run":             "",
			"no-default-browser-check": "",
			// Limitar cache em disco a 32 MB para evitar esgotamento de espaço
			"disk-cache-size":           "33554432",
			"aggressive-cache-discard":  "",
			"disable-application-cache": "",
		},
	}

	return browserMgr.Get(opts, func() {
		// Matar instâncias Chrome existentes que usam o mesmo perfil (recuperação de
		// crash/restart anterior) — o Rod não consegue lançar uma nova instância de debug se o
		// perfil já está travado.
		browser.KillExistingChrome(sessionDir, logger)
		time.Sleep(1 * time.Second)

		// Limpar cache do Chrome antes de lançar — cresce sem limite e pode esgotar o disco.
		// Apenas cache e Code Cache são removidos; cookies/LocalStorage/IndexedDB são preservados.
		for _, cacheSubDir := range []string{"Default/Cache", "Default/Code Cache", "Default/GPUCache"} {
			cacheDir := filepath.Join(sessionDir, cacheSubDir)
			if _, err := os.Stat(cacheDir); err == nil {
				os.RemoveAll(cacheDir) //nolint:errcheck
				logger.Info().Str("dir", cacheDir).Msg("[Teams] Cache Chrome removido antes do launch")
			}
		}
	}, func() {
		logger.Info().Msg("[Teams] Browser persistente iniciado")
	}, logger)
}

// restoreWindow traz a janela do browser de volta ao estado normal (visível). Chamada no início
// de RunDiscovery/ScanConversations/SendBatch — como o browser é persistente entre chamadas (ver
// getBrowser acima), uma chamada anterior pode ter deixado a janela minimizada; se a sessão salva
// expirou nesse meio-tempo, o Teams volta a pedir login, e o usuário precisa enxergar essa tela
// desde o início da chamada, não só depois de já ter "perdido" a oportunidade de interagir.
func restoreWindow(page *rod.Page, logger *zerolog.Logger) {
	browser.RestoreWindow(page, logger)
}

// minimizeWindow minimiza a janela do browser. Chamado depois que a página autenticada do Teams
// já está confirmada carregada — daí em diante toda a extração é via CDP/JS (DOM, IndexedDB),
// sem nenhuma necessidade de interação do usuário, então não há motivo pra manter uma janela do
// Chrome ocupando a tela. Se uma chamada futura precisar de login de novo (sessão expirada),
// restoreWindow (acima) traz a janela de volta no início dessa próxima chamada.
func minimizeWindow(page *rod.Page, logger *zerolog.Logger) {
	browser.MinimizeWindow(page, logger)
}

// CloseBrowser encerra o Chrome persistente do Teams, se houver algum aberto. Chamado no
// shutdown do servidor para não deixar o processo órfão rodando em segundo plano — a próxima
// chamada relança do zero via getBrowser.
func CloseBrowser() {
	browserMgr.Close()
}
