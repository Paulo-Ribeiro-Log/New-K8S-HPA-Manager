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
	// Docker (selenium/standalone-chrome) resolve a causa raiz real do crash do Chromium embed
	// (revisão pinada e antiga, ver docker_browser.go) com um Chrome atual rodando isolado — mas
	// fica atrás de opt-in explícito (ver comentário no topo de docker_browser.go: validado com
	// sucesso, porém expôs um crash severo do servidor inteiro num 2º teste, corrigido via patch
	// no vendor do go-rod, ainda não testado o suficiente pra virar padrão). Sem a env var, cai
	// direto pro Chrome do sistema — comportamento idêntico a antes desta feature existir.
	if teamsDockerEnabled() {
		if b, err := getDockerBrowser(logger); err == nil {
			return b, nil
		} else {
			logger.Warn().Err(err).Msg("[Teams] Docker indisponível ou falhou — caindo para Chrome do sistema/embed")
		}
	}

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

	flags := map[string]string{
		// Sem valor: launcher gera --flag (sem "="), correto para flags booleanas
		"disable-blink-features":   "AutomationControlled",
		"no-first-run":             "",
		"no-default-browser-check": "",
		// Limitar cache em disco a 32 MB para evitar esgotamento de espaço
		"disk-cache-size":           "33554432",
		"aggressive-cache-discard":  "",
		"disable-application-cache": "",
	}
	if teamsForceEmbedBrowser() {
		// Chromium embed do Rod (revisão pinada) — testado ao vivo em WSL2 sem essas flags: o
		// processo caiu sozinho (~90s depois de carregar o Teams v2, CDP desconectou) durante a
		// espera do SkypeToken. Mesmas flags de estabilidade que o ServiceNow já usa com sucesso
		// nesta mesma máquina (rod_extractor.go:rodLaunchFlags). Escopado só ao caminho de
		// experimento explícito (env var) — não aplicado ao fallback "Chrome do sistema não
		// encontrado" pra não mudar esse comportamento pré-existente fora do escopo da Fase 2.
		flags["disable-gpu"] = ""
		flags["no-sandbox"] = ""
		flags["disable-dev-shm-usage"] = ""
		flags["disable-setuid-sandbox"] = ""
	}

	opts := browser.LaunchOptions{
		SessionDir:  sessionDir,
		Headless:    false,
		SystemBin:   chromeBin,
		DeleteFlags: []string{"enable-automation"},
		Flags:       flags,
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

// waitForTeamsUIReady espera a UI do Teams (barra lateral de conversas, `simple-collab-dnd-rail`)
// renderizar de fato — sinal de "app React terminou de montar E usuário está autenticado", o mesmo
// usado por FetchMessageByLink (message_fetch.go). Tenta auto-login SSO/bypass do form oculto do
// MCAS/detecção de número MFA a cada iteração (mesmo mecanismo de RunDiscovery), e dispensa o
// prompt "abrir no app ou continuar no navegador" caso apareça.
//
// Bug real corrigido — ScanConversations/SendBatch nunca tinham esse loop: minimizavam a janela
// assim que a URL batia com teams.microsoft.com (sinal fraco — verdadeiro mesmo em telas de
// login/MFA em andamento, ver comentário equivalente em RunDiscovery/FetchMessageByLink) e só
// esperavam um sleep fixo (25s/20s) antes de varrer o DOM/enviar mensagens — sem dar tempo real de
// completar um login pendente e sem nenhum sinal de que a UI de fato carregou. Resultado observado
// ao vivo: scan retornando 0 conversas sem nenhum erro, indistinguível de "conta sem chats". Essas
// duas operações (usadas pela aba Teams Broadcast) tinham ficado pra trás da correção já aplicada
// em RunDiscovery/FetchMessageByLink — extraído aqui pra as três (mais FetchMessageByLink, que
// pode ser migrada depois) pararem de duplicar a mesma lógica.
//
// Retorna false se o teto (teamsSkypeTokenWaitTimeout) estourar sem a UI ficar pronta — o chamador
// deve manter a janela visível e desistir com um erro explícito nesse caso, nunca prosseguir como
// se estivesse tudo certo (mascararia um login pendente atrás de um resultado vazio).
func waitForTeamsUIReady(page *rod.Page, sessionDir string, logger *zerolog.Logger, logPrefix string) bool {
	deadline := time.Now().Add(teamsSkypeTokenWaitTimeout)
	for time.Now().Before(deadline) {
		browser.AttemptSSOAutoLogin(page, sessionDir, logger)
		browser.SubmitMCASHiddenForm(page)
		detectMFANumber(page, logger)
		if dismissAppLauncherPrompt(page, logger) {
			logger.Info().Str("prefix", logPrefix).Msg("Prompt \"abrir no app ou navegador\" dispensado automaticamente")
		}
		readyRes, readyErr := page.Eval(`() => !!document.querySelector('[data-tid="simple-collab-dnd-rail"]')`)
		if readyErr == nil && !readyRes.Value.Nil() && readyRes.Value.Bool() {
			logger.Info().Str("prefix", logPrefix).Msg("UI do Teams pronta (barra lateral renderizada)")
			SetTeamsDockerMFANumber("")
			return true
		}
		time.Sleep(2 * time.Second)
	}
	SetTeamsDockerMFANumber("")
	return false
}

// CloseBrowser encerra o Chrome persistente do Teams, se houver algum aberto — tanto o processo
// local (Chrome do sistema/embed, via browserMgr) quanto o container Docker (se o modo Docker
// tiver sido usado nesta execução, via CloseDockerBrowser). Chamado no shutdown do servidor para
// não deixar nada órfão rodando em segundo plano — a próxima chamada relança do zero via
// getBrowser.
func CloseBrowser() {
	browserMgr.Close()
	CloseDockerBrowser()
}
