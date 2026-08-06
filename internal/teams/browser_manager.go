package teams

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// browserMu protege o Chrome persistente do Teams. operationMu serializa RunDiscovery,
// ScanConversations e SendBatch entre si — abas paralelas no mesmo perfil podem invalidar o
// SkypeToken (mesmo risco documentado no extractMu do ServiceNow, internal/servicenow/rod_extractor.go).
var (
	browserMu        sync.Mutex
	operationMu      sync.Mutex
	sharedBrowser    *rod.Browser
	sharedSessionDir string
)

// getBrowser retorna o Chrome persistente da sessão do Teams, lançando um processo novo só na
// primeira chamada ou se o anterior morreu. Antes, RunDiscovery/ScanConversations/SendBatch
// matavam e relançavam o Chrome do zero (kill+launch+connect) a cada chamada — reaproveitar o
// mesmo processo entre elas elimina esse cold-start, mesmo padrão já usado pelo ServiceNow
// (RodExtractor.getBrowser, "N CHGs = 1 browser").
func getBrowser(sessionDir string, logger *zerolog.Logger) (*rod.Browser, error) {
	browserMu.Lock()
	defer browserMu.Unlock()

	if sharedBrowser != nil && sharedSessionDir == sessionDir {
		if _, err := sharedBrowser.Pages(); err == nil {
			return sharedBrowser, nil
		}
		logger.Warn().Msg("[Teams] Browser persistente morreu — relançando")
		sharedBrowser = nil
	}

	chromeBin := findSystemChrome()
	if chromeBin != "" {
		logger.Info().Str("bin", chromeBin).Msg("[Teams] Usando Chrome do sistema")
	} else {
		logger.Warn().Msg("[Teams] Chrome do sistema não encontrado — usando Chromium do Rod")
	}

	// Matar instâncias Chrome existentes que usam o mesmo perfil (recuperação de crash/restart
	// anterior) — o Rod não consegue lançar uma nova instância de debug se o perfil já está travado.
	killExistingChrome(sessionDir, logger)
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

	l := launcher.New().
		UserDataDir(sessionDir).
		Headless(false).
		Delete("enable-automation").
		Set("disable-blink-features", "AutomationControlled").
		// Sem segundo argumento: Rod gera --flag (sem =), correto para flags booleanas
		Set("no-first-run").
		Set("no-default-browser-check").
		// Limitar cache em disco a 32 MB para evitar esgotamento de espaço
		Set("disk-cache-size", "33554432").
		Set("aggressive-cache-discard").
		Set("disable-application-cache")

	if chromeBin != "" {
		l = l.Bin(chromeBin)
	}

	ctrlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar Chrome: %w", err)
	}

	b := rod.New().ControlURL(ctrlURL)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar ao browser: %w", err)
	}

	sharedBrowser = b
	sharedSessionDir = sessionDir
	logger.Info().Msg("[Teams] Browser persistente iniciado")
	return b, nil
}

// restoreWindow traz a janela do browser de volta ao estado normal (visível). Chamada no início
// de RunDiscovery/ScanConversations/SendBatch — como o browser é persistente entre chamadas (ver
// getBrowser acima), uma chamada anterior pode ter deixado a janela minimizada; se a sessão salva
// expirou nesse meio-tempo, o Teams volta a pedir login, e o usuário precisa enxergar essa tela
// desde o início da chamada, não só depois de já ter "perdido" a oportunidade de interagir.
func restoreWindow(page *rod.Page, logger *zerolog.Logger) {
	if err := page.SetWindow(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateNormal}); err != nil {
		// Não crítico: em WSL2 sem display gráfico (Xvfb) não existe uma janela real de verdade
		// pra restaurar — a extração continua funcionando normalmente mesmo se isso falhar.
		logger.Debug().Err(err).Msg("[Teams] Não foi possível restaurar a janela do browser (não crítico)")
	}
}

// minimizeWindow minimiza a janela do browser. Chamado depois que a página autenticada do Teams
// já está confirmada carregada — daí em diante toda a extração é via CDP/JS (DOM, IndexedDB),
// sem nenhuma necessidade de interação do usuário, então não há motivo pra manter uma janela do
// Chrome ocupando a tela. Se uma chamada futura precisar de login de novo (sessão expirada),
// restoreWindow (acima) traz a janela de volta no início dessa próxima chamada.
func minimizeWindow(page *rod.Page, logger *zerolog.Logger) {
	if err := page.SetWindow(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateMinimized}); err != nil {
		logger.Debug().Err(err).Msg("[Teams] Não foi possível minimizar a janela do browser (não crítico)")
	}
}

// CloseBrowser encerra o Chrome persistente do Teams, se houver algum aberto. Chamado no
// shutdown do servidor para não deixar o processo órfão rodando em segundo plano — a próxima
// chamada relança do zero via getBrowser.
func CloseBrowser() {
	browserMu.Lock()
	defer browserMu.Unlock()
	if sharedBrowser != nil {
		sharedBrowser.Close() //nolint:errcheck
		sharedBrowser = nil
	}
}
