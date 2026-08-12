package servicenow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"

	sharedbrowser "k8s-hpa-manager/internal/browser"
)

// RodExtractor usa a biblioteca Rod (Go nativo) para extrair dados do ServiceNow
// Não precisa de Node.js, npm ou dependências externas
type RodExtractor struct {
	logger     *zerolog.Logger
	sessionDir string     // Diretório de sessão Chromium (~/.k8s-hpa-manager/rod-session)
	extractMu  sync.Mutex // serializa extrações — ServiceNow invalida token com abas paralelas
	// browserMgr é o browser headless persistente — reutilizado entre extrações (N CHGs = 1
	// browser). Implementação compartilhada com internal/teams (Fase 1 de
	// BROWSER-CONSOLIDATION-STUDY.md); protege a si mesma, sem precisar de mutex próprio aqui.
	browserMgr sharedbrowser.Manager

	// loginMu/loginCancel suportam cancelamento explícito de um login visível em andamento
	// (TestSession). Não usamos o context da requisição HTTP aqui de propósito — TestSession
	// roda sobre context.Background() (ver handler) para não ser derrubado por uma reconexão
	// instável do browser durante os vários minutos que um login real pode levar. O cancelamento
	// real vem do botão "Cancelar" do frontend, via endpoint dedicado que aciona este cancelFunc.
	loginMu     sync.Mutex
	loginCancel context.CancelFunc
}

// NewRodExtractor cria um novo extrator Rod
func NewRodExtractor(logger *zerolog.Logger) *RodExtractor {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE") // Windows
	}

	sessionDir := filepath.Join(homeDir, ".k8s-hpa-manager", "rod-session")

	// Criar diretório de sessão se não existir
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		logger.Warn().Err(err).Msg("[Rod] Erro ao criar diretório de sessão")
	}

	logger.Info().
		Str("session_dir", sessionDir).
		Msg("[Rod] Extrator inicializado (Go nativo - sem dependências externas)")

	return &RodExtractor{
		logger:     logger,
		sessionDir: sessionDir,
	}
}

// maxLoginWait é um teto de segurança para fluxos de login visível (TestSession,
// extractWithVisibleLogin) — NÃO é o mecanismo primário de encerramento. O login só deve parar
// por sucesso ou por cancelamento explícito do usuário (botão "Cancelar" no frontend); este teto
// existe apenas para não deixar um browser/processo Chromium órfão rodando pra sempre caso o
// usuário abandone a aba sem cancelar (fechar o notebook, perder rede, etc.).
const maxLoginWait = 20 * time.Minute

// waitOrCancel dorme por d ou retorna mais cedo se ctx for cancelado. Retorna true quando a
// espera foi interrompida por cancelamento (ctx.Done()), false quando o tempo total transcorreu
// normalmente. Usado nos loops de espera de login para que "Cancelar" pare a espera quase
// imediatamente, em vez de esperar o próximo tick do polling.
func waitOrCancel(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-ctx.Done():
		return true
	}
}

// beginLoginSession cria um context cancelável a partir de parent e registra seu cancelFunc para
// que CancelActiveLogin() consiga interrompê-lo a partir de outra requisição HTTP (o botão
// "Cancelar" do frontend). Retorna o context derivado e uma função release a ser chamada em defer
// pelo chamador — release limpa o cancelFunc registrado (sem isso, uma chamada de cancelamento
// tardia afetaria por engano um login seguinte) e libera os recursos do context.
func (r *RodExtractor) beginLoginSession(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	r.loginMu.Lock()
	r.loginCancel = cancel
	r.loginMu.Unlock()
	return ctx, func() {
		r.loginMu.Lock()
		r.loginCancel = nil
		r.loginMu.Unlock()
		cancel()
	}
}

// CancelActiveLogin cancela um fluxo de login visível em andamento (aberto via TestSession),
// fazendo o browser fechar e a requisição HTTP original retornar um status "cancelled" em vez de
// ficar presa até o teto de segurança. Retorna false quando não havia nenhum login em andamento.
func (r *RodExtractor) CancelActiveLogin() bool {
	r.loginMu.Lock()
	cancel := r.loginCancel
	r.loginMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// IsConfigured sempre retorna true pois Rod baixa o browser automaticamente
func (r *RodExtractor) IsConfigured() bool {
	return true
}

// GetStatus retorna o status da configuração do Rod
// Retorna campos compatíveis com o frontend (playwright_configured, script_exists)
func (r *RodExtractor) GetStatus() map[string]interface{} {
	status := map[string]interface{}{
		// Campos para compatibilidade com frontend
		"playwright_configured": true,
		"script_exists":         true,
		"configured":            true,
		"session_dir":           r.sessionDir,
		"type":                  "rod-go-native",
		"dependencies":          "none (Go native)",
		"npx_available":         true,
		"ts_node_available":     true,
		"npm_installed":         true,
		"is_wsl":                IsWSL(),
		"has_display":           HasGraphicalDisplay(),
		"xvfb_installed":        IsXvfbInstalled(),
	}

	if _, err := os.Stat(r.sessionDir); err == nil {
		status["session_exists"] = true
	} else {
		status["session_exists"] = false
	}

	return status
}

func (r *RodExtractor) activeSessionDir() string {
	return r.sessionDir
}

// GetSessionStatus retorna o status da sessão Chromium local.
func (r *RodExtractor) GetSessionStatus() *SessionStatus {
	sessionDir := r.sessionDir
	status := &SessionStatus{SessionDir: sessionDir}

	// Verificar diretório de sessão
	info, err := os.Stat(sessionDir)
	if os.IsNotExist(err) {
		status.Exists = false
		status.Valid = false
		status.Status = "not_found"
		status.Message = "Sessão não encontrada. Será necessário fazer login no Azure AD na próxima extração."
		return status
	}
	if err != nil {
		status.Exists = false
		status.Valid = false
		status.Status = "error"
		status.Message = fmt.Sprintf("Erro ao verificar sessão: %v", err)
		return status
	}

	status.Exists = true
	lastMod := info.ModTime()
	status.LastModified = &lastMod
	hoursSince := time.Since(lastMod).Hours()
	status.HoursSinceUpdate = hoursSince

	entries, _ := os.ReadDir(sessionDir)
	hasSession := len(entries) > 0

	const maxSessionHours = 8.0
	if !hasSession {
		status.Valid = false
		status.Status = "empty"
		status.Message = "Sessão existe mas está vazia. Login será necessário."
	} else if hoursSince > maxSessionHours {
		status.Valid = false
		status.Status = "expired"
		status.Message = fmt.Sprintf("Sessão expirada (%.1f horas). Login será necessário na próxima extração.", hoursSince)
	} else {
		status.Valid = true
		status.Status = "valid"
		status.Message = fmt.Sprintf("Sessão válida (última atualização: %.1f horas atrás).", hoursSince)
	}
	return status
}

// ClearSession remove a sessão do Rod
func (r *RodExtractor) ClearSession() error {
	// Invalidar browser persistente antes de limpar o diretório de sessão
	r.browserMgr.Close()

	dir := r.activeSessionDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		r.logger.Info().Str("dir", dir).Msg("[Rod] Sessão já não existe")
		return nil
	}

	err := os.RemoveAll(dir)
	if err != nil {
		r.logger.Error().Err(err).Str("dir", dir).Msg("[Rod] Erro ao limpar sessão")
		return fmt.Errorf("erro ao limpar sessão: %v", err)
	}

	// Recriar diretório vazio
	os.MkdirAll(dir, 0755) //nolint:errcheck

	r.logger.Info().Str("dir", dir).Msg("[Rod] Sessão limpa com sucesso")
	return nil
}

// launchBrowser inicia o Chromium local via Rod (baixa automaticamente se necessário).
// Para sessões visíveis (headless=false) sem display gráfico, usa Xvfb como fallback.
func (r *RodExtractor) launchBrowser(headless bool, _ string) (*rod.Browser, func(), error) {
	return r.launchLocalBrowser(headless)
}

// launchLocalBrowser usa o launcher padrão do Rod (baixa Chromium automaticamente)
func (r *RodExtractor) launchLocalBrowser(headless bool) (*rod.Browser, func(), error) {
	return r.launchLocalBrowserWithDir(headless, r.activeSessionDir())
}

// rodLaunchFlags são as flags do launcher usadas por todo Chromium local lançado pelo
// ServiceNow (persistente headless ou visível pra login) — mesmas em ambos os casos.
func rodLaunchFlags() map[string]string {
	return map[string]string{
		"disable-blink-features": "AutomationControlled",
		// Flags de estabilidade WSL2: sem valor → launcher gera --flag (não --flag=)
		"disable-dev-shm-usage":  "",
		"no-sandbox":             "",
		"disable-gpu":            "",
		"disable-setuid-sandbox": "",
	}
}

// launchLocalBrowserWithDir inicia o Chromium local com um diretório de sessão específico.
// Para sessões visíveis (headless=false) sem display gráfico, tenta Xvfb automaticamente.
func (r *RodExtractor) launchLocalBrowserWithDir(headless bool, sessionDir string) (*rod.Browser, func(), error) {
	sharedbrowser.RemoveStaleLockFiles(sessionDir, r.logger)

	var xvfbCleanup func()

	// Browser visível sem display → tentar Xvfb (X Virtual Framebuffer)
	if !headless && !HasGraphicalDisplay() {
		display, cleanup, err := EnsureVirtualDisplay()
		if err != nil {
			r.logger.Warn().Err(err).Msg("[Rod] Sem display gráfico e Xvfb indisponível — tentando mesmo assim")
		} else {
			r.logger.Info().Str("display", display).Msg("[Rod] Display virtual configurado (Xvfb)")
			os.Setenv("DISPLAY", display) //nolint:errcheck
			xvfbCleanup = cleanup
		}
	}

	r.logger.Info().
		Bool("headless", headless).
		Str("session_dir", sessionDir).
		Bool("has_display", HasGraphicalDisplay()).
		Msg("[Rod] Iniciando Chromium local...")

	b, stop, err := sharedbrowser.Launch(sharedbrowser.LaunchOptions{
		SessionDir: sessionDir,
		Headless:   headless,
		Flags:      rodLaunchFlags(),
	})
	if err != nil {
		if xvfbCleanup != nil {
			xvfbCleanup()
		}
		return nil, nil, err
	}

	r.logger.Info().Msg("[Rod] Chromium local iniciado com sucesso")
	return b, func() {
		stop()
		if xvfbCleanup != nil {
			xvfbCleanup()
		}
	}, nil
}

// getBrowser retorna o browser headless persistente, criando um novo se necessário.
// Reutilizar o mesmo browser entre extrações evita abrir N janelas para N CHGs.
func (r *RodExtractor) getBrowser() (*rod.Browser, error) {
	sessionDir := r.activeSessionDir()
	opts := sharedbrowser.LaunchOptions{
		SessionDir: sessionDir,
		Headless:   true,
		Flags:      rodLaunchFlags(),
	}
	return r.browserMgr.Get(opts, func() {
		sharedbrowser.RemoveStaleLockFiles(sessionDir, r.logger)
	}, func() {
		r.logger.Info().Msg("[Rod] Browser persistente iniciado")
	}, r.logger)
}

// TestSession abre o Chromium para o usuário fazer login no ServiceNow.
// Em WSL sem display gráfico, usa Xvfb (X Virtual Framebuffer) como display virtual.
// O browser é invisível no Xvfb — use WSLg ou x11vnc para visualizá-lo,
// ou confie no SSO silencioso se a máquina for domain-joined.
func (r *RodExtractor) TestSession(ctx context.Context) (*SessionStatus, error) {
	// Registra um cancelFunc para este login — permite que CancelActiveLogin() (acionado pelo
	// botão "Cancelar" do frontend, numa requisição HTTP separada) interrompa a espera a
	// qualquer momento, em vez de depender só do teto de segurança maxLoginWait.
	ctx, release := r.beginLoginSession(ctx)
	defer release()

	sessionDir := r.activeSessionDir()
	if IsWSL() && !HasGraphicalDisplay() {
		if IsXvfbInstalled() {
			r.logger.Info().Msg("[Rod] WSL sem display gráfico — usando Xvfb (:99) como display virtual")
			r.logger.Info().Msg("[Rod] Para ver o browser: instale WSLg (Windows 11) ou use 'x11vnc -display :99 -forever'")
		} else {
			r.logger.Warn().Msgf("[Rod] WSL sem display gráfico e Xvfb não instalado. Instale com: %s", XvfbInstallHint())
		}
	}

	r.logger.Info().
		Str("session_dir", sessionDir).
		Bool("is_wsl", IsWSL()).
		Bool("has_display", HasGraphicalDisplay()).
		Msg("[Rod] Iniciando login - abrindo browser visível")

	// Invalidar browser persistente: sessão será limpa a seguir
	r.browserMgr.Close()

	// Limpar sessão anterior para garantir login fresco
	r.logger.Info().Msg("[Rod] Limpando sessão anterior para garantir login fresco...")
	os.RemoveAll(sessionDir)      //nolint:errcheck
	os.MkdirAll(sessionDir, 0755) //nolint:errcheck

	// Iniciar browser abrindo diretamente no ServiceNow (sem aba em branco)
	const serviceNowHome = "https://viavarejo.service-now.com"
	browser, closeFunc, err := r.launchBrowser(false, serviceNowHome)
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}

	// Navegar para ServiceNow
	page, err := browser.Page(proto.TargetCreateTarget{URL: serviceNowHome})
	if err != nil {
		closeFunc()
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	if _, activateErr := page.Activate(); activateErr != nil {
		r.logger.Warn().Err(activateErr).Msg("[Rod] Aviso: não foi possível ativar aba do ServiceNow")
	}

	r.logger.Info().Msg("[Rod] Navegando para ServiceNow...")
	r.logger.Info().Msg("[Rod] ============================================")
	r.logger.Info().Msg("[Rod] AGUARDANDO VOCÊ FAZER LOGIN NO AZURE AD...")
	r.logger.Info().Msg("[Rod] A página só fecha quando o login terminar ou você clicar em Cancelar")
	r.logger.Info().Msg("[Rod] ============================================")

	// loginTimeout é só um teto de segurança (ver comentário de maxLoginWait) — o encerramento
	// normal é por sucesso ou pelo cancelamento explícito via CancelActiveLogin().
	loginTimeout := maxLoginWait
	startTime := time.Now()

	// cancelledResult fecha a página/browser e monta o SessionStatus retornado quando o usuário
	// cancela o login pelo botão "Cancelar" (ctx.Done() disparado por CancelActiveLogin()).
	cancelledResult := func() (*SessionStatus, error) {
		r.logger.Info().Msg("[Rod] Login cancelado pelo usuário")
		page.Close() //nolint:errcheck
		closeFunc()
		return &SessionStatus{
			Valid:   false,
			Status:  "cancelled",
			Message: "Login cancelado.",
		}, nil
	}

	loginPatterns := []string{
		"login.microsoftonline.com",
		"login.windows.net",
		"login.live.com",
	}

	// FASE 1: Aguardar redirect para página de login (até 30s)
	r.logger.Info().Msg("[Rod] Fase 1: Aguardando redirect para Azure AD (até 30s)...")

	loginPageDetected := false
	serviceNowLoadedCount := 0

	for i := 0; i < 15; i++ {
		if ctx.Err() != nil {
			return cancelledResult()
		}

		pageInfo, err := page.Info()
		if err != nil {
			if waitOrCancel(ctx, 2*time.Second) {
				return cancelledResult()
			}
			continue
		}
		currentURL := pageInfo.URL
		r.logger.Debug().Str("url", currentURL).Int("check", i+1).Msg("[Rod] Verificando URL...")

		for _, pattern := range loginPatterns {
			if strings.Contains(currentURL, pattern) {
				loginPageDetected = true
				r.logger.Info().Str("url", currentURL).Msg("[Rod] Página de login do Azure AD detectada!")
				break
			}
		}

		if loginPageDetected {
			break
		}

		if strings.Contains(currentURL, "service-now.com") &&
			!strings.Contains(currentURL, "saml") &&
			!strings.Contains(currentURL, "login") {
			serviceNowLoadedCount++
			r.logger.Debug().Int("count", serviceNowLoadedCount).Msg("[Rod] ServiceNow carregado, aguardando confirmação...")

			if serviceNowLoadedCount >= 5 {
				r.logger.Info().Msg("[Rod] Confirmado: já está autenticado no ServiceNow (sessão anterior válida)")
				waitOrCancel(ctx, 2*time.Second)
				page.Close() //nolint:errcheck
				closeFunc()
				status := r.GetSessionStatus()
				status.Valid = true
				status.Message = "Sessão já está válida."
				return status, nil
			}
		} else {
			serviceNowLoadedCount = 0
		}

		if waitOrCancel(ctx, 2*time.Second) {
			return cancelledResult()
		}
	}

	if !loginPageDetected {
		r.logger.Warn().Msg("[Rod] Página de login não detectada após 30s, aguardando mais...")
	}

	// FASE 2: Aguardar o usuário completar o login.
	// Se Perfil SSO corporativo estiver configurado, auto-preenche o formulário Azure AD.
	r.logger.Info().Msg("[Rod] Aguardando login no Azure AD (auto-login via Perfil SSO se configurado)...")

	autoLoginAttempted := false

	for {
		if ctx.Err() != nil {
			return cancelledResult()
		}

		elapsed := time.Since(startTime)
		if elapsed > loginTimeout {
			r.logger.Warn().Dur("elapsed", elapsed).Msg("[Rod] Teto de segurança atingido aguardando login")
			page.Close() //nolint:errcheck
			closeFunc()
			return &SessionStatus{
				Valid:   false,
				Status:  "timeout",
				Message: "Tempo máximo de espera atingido. Tente novamente.",
			}, nil
		}

		pageInfo, err := page.Info()
		if err != nil {
			if waitOrCancel(ctx, 2*time.Second) {
				return cancelledResult()
			}
			continue
		}
		currentURL := pageInfo.URL

		isOnLogin := false
		for _, pattern := range loginPatterns {
			if strings.Contains(currentURL, pattern) {
				isOnLogin = true
				break
			}
		}

		// Tentar auto-login via Perfil SSO quando na página do Azure AD
		if isOnLogin {
			if ssoAutoLoginAttempt(page, r.sessionDir, r.logger) {
				if !autoLoginAttempted {
					autoLoginAttempted = true
					r.logger.Info().Msg("[Rod] Auto-login SSO iniciado — aguardando redirect...")
				}
				if waitOrCancel(ctx, 2*time.Second) {
					return cancelledResult()
				}
				continue
			}
		}

		if strings.Contains(currentURL, "service-now.com") &&
			!isOnLogin &&
			!strings.Contains(currentURL, "saml") &&
			!strings.Contains(currentURL, "login") {

			r.logger.Info().Str("url", currentURL).Dur("elapsed", elapsed).Msg("[Rod] LOGIN COMPLETADO COM SUCESSO!")

			r.logger.Info().Msg("[Rod] Fase 4: Aguardando página carregar (5s)...")
			if waitOrCancel(ctx, 5*time.Second) {
				return cancelledResult()
			}

			r.logger.Info().Msg("[Rod] Navegando para home do ServiceNow...")
			page.Navigate("https://viavarejo.service-now.com/now/nav/ui/home") //nolint:errcheck
			if waitOrCancel(ctx, 5*time.Second) {
				return cancelledResult()
			}

			r.logger.Info().Msg("[Rod] Aguardando cookies serem salvos (5s)...")
			if waitOrCancel(ctx, 5*time.Second) {
				return cancelledResult()
			}

			r.logger.Info().Msg("[Rod] Encerrando sessão de login...")
			page.Close() //nolint:errcheck
			closeFunc()

			activeDir := r.activeSessionDir()
			entries, _ := os.ReadDir(activeDir)
			r.logger.Info().Int("files_count", len(entries)).Str("session_dir", activeDir).Msg("[Rod] Sessão salva!")

			return &SessionStatus{
				Valid:      true,
				Exists:     true,
				Status:     "valid",
				SessionDir: activeDir,
				Message:    "Login realizado com sucesso! Agora você pode extrair CHGs.",
			}, nil
		}

		if int(elapsed.Seconds())%10 == 0 {
			r.logger.Info().Str("url", currentURL).Dur("elapsed", elapsed).Msg("[Rod] Ainda aguardando login...")
		}

		if waitOrCancel(ctx, 2*time.Second) {
			return cancelledResult()
		}
	}
}

// Extract extrai dados de uma CHG do ServiceNow
func (r *RodExtractor) Extract(ctx context.Context, chgURL string) (result *PlaywrightResult, err error) {
	// Recover de panic para evitar crash
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error().
				Interface("panic", rec).
				Str("url", chgURL).
				Msg("[Rod] PANIC CAPTURADO na extração")
			err = fmt.Errorf("erro interno durante extração: %v", rec)
			result = nil
		}
	}()

	// Serializar extrações: ServiceNow invalida o token SAML quando múltiplas abas
	// tentam autenticar simultaneamente — processar uma de cada vez.
	r.extractMu.Lock()
	defer r.extractMu.Unlock()

	r.logger.Info().Str("url", chgURL).Msg("[Rod] ========== INICIANDO EXTRAÇÃO ==========")

	// Validar URL
	if !strings.Contains(chgURL, "service-now.com") {
		r.logger.Error().Str("url", chgURL).Msg("[Rod] URL inválida - não contém service-now.com")
		return nil, fmt.Errorf("URL inválida: deve ser uma URL do ServiceNow")
	}

	// Reescrever URLs legadas nav_to.do → formato direto (nav_to.do não carrega o formulário corretamente no headless)
	if strings.Contains(chgURL, "nav_to.do") {
		chgNumRe := regexp.MustCompile(`(?i)CHG\d{5,}`)
		if m := chgNumRe.FindString(chgURL); m != "" {
			chgURL = "https://viavarejo.service-now.com/change_request.do?sysparm_query=number=" + strings.ToUpper(m)
			r.logger.Info().Str("rewritten_url", chgURL).Msg("[Rod] URL nav_to.do reescrita para formato direto")
		}
	}

	sysIDRegex := regexp.MustCompile(`sys_id=([a-f0-9]{32})`)
	chgNumberRegex := regexp.MustCompile(`(?i)(number=CHG\d+|CHG\d{5,})`)
	if !sysIDRegex.MatchString(chgURL) && !chgNumberRegex.MatchString(chgURL) {
		r.logger.Error().Str("url", chgURL).Msg("[Rod] URL inválida - sys_id nem número de CHG encontrado")
		return nil, fmt.Errorf("URL inválida: informe uma URL com sys_id ou número da CHG (ex: ?number=CHG0454511)")
	}

	r.logger.Info().Msg("[Rod] URL validada com sucesso")

	// Caminho rápido: REST API com cookies do Chrome (sem abrir browser).
	// Usa CDP (porta 9223) para obter cookies em Go puro — sem PowerShell nem DPAPI.
	// Só aceita o resultado se a descrição/justificativa veio preenchida —
	// o ServiceNow pode retornar o registro com campos restritos vazios via ACL.
	if cookies, cerr := ExtractCookiesViaCDP(WindowsCDPPort, snCookieDomain); cerr == nil {
		snd := NewSNDirectClient(cookies)
		if apiResult, apiErr := snd.FetchFromURL(chgURL); apiErr == nil && apiResult.Success && snHasTemplate(apiResult.Description) && apiResult.Extracted != nil && apiResult.Extracted.Application != "" {
			r.logger.Info().Str("chg", apiResult.ChangeNumber).Str("app", apiResult.Extracted.Application).Msg("[Rod] Extração via REST API concluída (CDP, sem browser)")
			return apiResult, nil
		}
		r.logger.Debug().Msg("[Rod] REST API sem Application parseada — usando browser para obter conteúdo completo")
	} else {
		r.logger.Debug().Err(cerr).Msg("[Rod] CDP indisponível — usando browser")
	}

	// Verificação de sessão (Chromium headless — funciona sem display em qualquer ambiente)
	headless := true

	sessionStatus := r.GetSessionStatus()
	if !sessionStatus.Valid {
		r.logger.Warn().
			Str("session_status", sessionStatus.Status).
			Str("session_dir", sessionStatus.SessionDir).
			Msg("[Rod] Sessão local inválida ou ausente — extração bloqueada")
		return &PlaywrightResult{
			Success: false,
			Error:   "Sessão não autenticada. Acesse Menu de Perfil → ServiceNow Session e faça login antes de extrair dados.",
		}, nil
	}

	r.logger.Info().
		Str("url", chgURL).
		Bool("headless", headless).
		Str("session_dir", r.activeSessionDir()).
		Msg("[Rod] Configuração de sessão verificada")

	browser, err := r.getBrowser()
	if err != nil {
		r.logger.Error().Err(err).Msg("[Rod] ERRO ao obter browser")
		return nil, err
	}
	r.logger.Info().Msg("[Rod] Browser obtido com sucesso")

	// Navegar para a CHG em nova aba
	r.logger.Info().Str("chg_url", chgURL).Msg("[Rod] Criando nova página e navegando para a CHG...")
	page, err := browser.Page(proto.TargetCreateTarget{URL: chgURL})
	if err != nil {
		r.logger.Error().Err(err).Str("chg_url", chgURL).Msg("[Rod] ERRO ao criar página")
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	defer page.Close() //nolint:errcheck
	if _, activateErr := page.Activate(); activateErr != nil {
		r.logger.Warn().Err(activateErr).Msg("[Rod] Aviso: não foi possível ativar aba da CHG")
	}
	r.logger.Info().Msg("[Rod] Página criada com sucesso, aguardando carregamento...")

	loginTimeout := 5 * time.Minute
	startTime := time.Now()

	loginPatterns := []string{
		"login.microsoftonline.com",
		"login.windows.net",
		"login.live.com",
		"adfs",
		"saml",
		"oauth",
		"signin",
		"sso",
	}

	// Aguardar até chegar no ServiceNow
	r.logger.Info().Msg("[Rod] Iniciando loop de verificação de login/acesso...")
	loopCount := 0
	for {
		if ctx.Err() != nil {
			r.logger.Info().Msg("[Rod] Extração cancelada aguardando login/acesso")
			return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
		}

		loopCount++
		elapsed := time.Since(startTime)

		if elapsed > loginTimeout {
			r.logger.Error().Dur("elapsed", elapsed).Int("loop_count", loopCount).Msg("[Rod] TIMEOUT aguardando login no Azure AD")
			return &PlaywrightResult{
				Success: false,
				Error:   "Timeout aguardando login no Azure AD (5 minutos)",
			}, nil
		}

		pageInfo, err := page.Info()
		if err != nil {
			r.logger.Warn().Err(err).Int("loop_count", loopCount).Msg("[Rod] Erro ao obter info da página (tentando novamente...)")
			if waitOrCancel(ctx, 2*time.Second) {
				return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
			}
			continue
		}
		currentURL := pageInfo.URL

		if loopCount%5 == 1 {
			r.logger.Debug().Str("current_url", currentURL).Int("loop_count", loopCount).Dur("elapsed", elapsed).Msg("[Rod] Verificando estado da página...")
		}

		isLoginPage := false
		for _, pattern := range loginPatterns {
			if strings.Contains(strings.ToLower(currentURL), strings.ToLower(pattern)) {
				isLoginPage = true
				r.logger.Debug().Str("pattern", pattern).Str("url", currentURL).Msg("[Rod] Detectada página de login")
				break
			}
		}

		if isLoginPage && headless {
			// Azure AD redirect detectado em modo headless.
			// Tenta auto-login via Perfil SSO corporativo; fallback: aguarda SSO silencioso (até 45s).
			r.logger.Info().Str("url", currentURL).Msg("[Rod] Azure AD redirect detectado — tentando auto-login via Perfil SSO...")
			ssoAutoLoginAttempt(page, r.sessionDir, r.logger)

			ssoDeadline := time.Now().Add(45 * time.Second)
			ssoOK := false
			for time.Now().Before(ssoDeadline) {
				if waitOrCancel(ctx, 1*time.Second) {
					return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
				}
				// Continuar tentando preencher formulário (múltiplas fases: email → senha → "stay signed in")
				ssoAutoLoginAttempt(page, r.sessionDir, r.logger)

				info, err := page.Info()
				if err != nil {
					continue
				}
				u := info.URL
				isStillLogin := false
				for _, p := range loginPatterns {
					if strings.Contains(strings.ToLower(u), strings.ToLower(p)) {
						isStillLogin = true
						break
					}
				}
				if !isStillLogin && strings.Contains(u, "service-now.com") && !strings.Contains(u, "login") {
					r.logger.Info().Str("url", u).Msg("[Rod] Login concluído (SSO auto ou corporativo) — continuando extração")
					ssoOK = true
					break
				}
			}
			if !ssoOK {
				r.logger.Warn().Msg("[Rod] SSO automático não concluído em 45s — sessão expirada")
				return &PlaywrightResult{
					Success: false,
					Error:   "Sessão expirada. Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair dados.",
				}, nil
			}
			break
		}

		if strings.Contains(currentURL, "service-now.com") && !isLoginPage && !strings.Contains(currentURL, "login") {
			r.logger.Info().Str("url", currentURL).Dur("elapsed", elapsed).Int("loops", loopCount).Msg("[Rod] Acesso ao ServiceNow confirmado!")
			break
		}

		if waitOrCancel(ctx, 300*time.Millisecond) {
			return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
		}
	}

	// Aguardar carregamento completo.
	// ServiceNow é SPA: o evento "load" dispara antes do router interno terminar de
	// renderizar o formulário, então 1s de buffer evita document.body == null no eval JS.
	r.logger.Info().Msg("[Rod] Aguardando carregamento da página...")
	if err := page.WaitLoad(); err != nil {
		// "Execution context was destroyed" é esperado quando o ServiceNow redireciona
		// de change_request.do para nav_to.do durante o carregamento. O gsft_main ainda
		// não está no DOM neste momento — a busca de iframe ocorre no loop abaixo.
		r.logger.Warn().Err(err).Msg("[Rod] Aviso: WaitLoad interrompido por navegação (ServiceNow redirect) — continuando")
		time.Sleep(1 * time.Second)
	}

	// Loop integrado: detecta o iframe gsft_main E aguarda conteúdo do template.
	// O gsft_main pode não estar no DOM imediatamente após o redirect do ServiceNow;
	// por isso a busca é feita a cada iteração (não apenas uma vez após WaitLoad).
	// Timeout total de 45s cobre o browser frio (primeira extração) + carregamento AJAX.
	hasTemplateJS := `() => {
		function hasTemplate(text) {
			if (!text || text.length < 20) return false;
			return text.includes('github.com/') ||
			       text.includes('* Aplicação') ||
			       text.includes('* Versão:') ||
			       text.includes('* Repositório:');
		}
		for (const ta of document.querySelectorAll('textarea')) {
			if (hasTemplate(ta.value || ta.textContent || '')) return true;
		}
		for (const el of document.querySelectorAll('div, span, pre, td')) {
			if (el.children.length > 3) continue;
			if (hasTemplate(el.innerText || el.textContent || '')) return true;
		}
		return false;
	}`

	var targetPage *rod.Page
	formReady := false
	deadline := time.Now().Add(45 * time.Second)

	r.logger.Info().Msg("[Rod] Aguardando iframe gsft_main e conteúdo do template CHG...")
	for time.Now().Before(deadline) && ctx.Err() == nil {
		// Tentar encontrar gsft_main se ainda não encontrado
		if targetPage == nil {
			if frames, ferr := page.Elements("iframe"); ferr == nil {
				for _, frame := range frames {
					name, _ := frame.Attribute("name")
					if name != nil && *name == "gsft_main" {
						if framePage, ferr2 := frame.Frame(); ferr2 == nil {
							targetPage = framePage
							r.logger.Info().Msg("[Rod] iframe gsft_main encontrado")
						}
						break
					}
				}
			}
		}

		checkPage := targetPage
		if checkPage == nil {
			checkPage = page
		}
		if res, err := checkPage.Eval(hasTemplateJS); err == nil && res != nil && res.Value.Bool() {
			r.logger.Info().Bool("in_iframe", targetPage != nil).Msg("[Rod] Conteúdo do template CHG detectado — pronto para extrair")
			formReady = true
			break
		}
		// Fallback: checar na página externa quando targetPage é o iframe
		if targetPage != nil && targetPage != page {
			if res, err := page.Eval(hasTemplateJS); err == nil && res != nil && res.Value.Bool() {
				r.logger.Info().Msg("[Rod] Conteúdo do template CHG detectado na página externa")
				formReady = true
				break
			}
		}
		waitOrCancel(ctx, 500*time.Millisecond)
	}

	if ctx.Err() != nil {
		r.logger.Info().Msg("[Rod] Extração cancelada aguardando template CHG")
		return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
	}

	if targetPage == nil {
		targetPage = page
		r.logger.Info().Msg("[Rod] Usando página principal para extração (gsft_main não encontrado em 45s)")
	}
	if !formReady {
		r.logger.Warn().Msg("[Rod] Template não detectado em 45s — prosseguindo com extração")
	}

	r.logger.Info().Msg("[Rod] ========== EXECUTANDO JAVASCRIPT PARA EXTRAIR DADOS ==========")

	// Capturar informações sobre a página para debug
	pageDebug, _ := targetPage.Eval(`() => {
		return {
			url: window.location.href,
			title: document.title,
			bodyLength: document.body ? document.body.innerHTML.length : 0,
			iframeCount: document.querySelectorAll('iframe').length,
			inputCount: document.querySelectorAll('input').length,
			textareaCount: document.querySelectorAll('textarea').length,
			hasGForm: !!document.querySelector('.g_form'),
			hasNowForm: !!document.querySelector('now-record-form'),
			hasSncForm: !!document.querySelector('[data-type="now-record-form"]'),
			hasFormSection: !!document.querySelector('.form-group, .form_section, .section_header'),
		};
	}`)
	if pageDebug != nil {
		debugData := pageDebug.Value.Map()
		currentURL := debugData["url"].String()
		currentTitle := debugData["title"].String()

		r.logger.Info().
			Str("url", currentURL).
			Str("title", currentTitle).
			Int("body_length", int(debugData["bodyLength"].Int())).
			Int("iframe_count", int(debugData["iframeCount"].Int())).
			Int("input_count", int(debugData["inputCount"].Int())).
			Int("textarea_count", int(debugData["textareaCount"].Int())).
			Bool("has_g_form", debugData["hasGForm"].Bool()).
			Bool("has_now_form", debugData["hasNowForm"].Bool()).
			Bool("has_snc_form", debugData["hasSncForm"].Bool()).
			Bool("has_form_section", debugData["hasFormSection"].Bool()).
			Msg("[Rod] Debug da página")

		loginIndicators := []string{
			"login.microsoftonline.com",
			"login.windows.net",
			"login.live.com",
			"Sign in to your account",
		}

		needsLogin := false
		for _, indicator := range loginIndicators {
			if strings.Contains(currentURL, indicator) || strings.Contains(currentTitle, indicator) {
				needsLogin = true
				break
			}
		}

		if needsLogin {
			r.logger.Warn().Str("url", currentURL).Str("title", currentTitle).Msg("[Rod] Sessão expirada - página de login detectada")

			page.Close() //nolint:errcheck

			activeDir := r.activeSessionDir()
			r.logger.Info().Str("session_dir", activeDir).Msg("[Rod] Limpando sessão inválida e reabrindo browser para login...")
			os.RemoveAll(activeDir)      //nolint:errcheck
			os.MkdirAll(activeDir, 0755) //nolint:errcheck

			r.logger.Info().Msg("[Rod] ============================================")
			r.logger.Info().Msg("[Rod] ABRINDO BROWSER PARA LOGIN NO AZURE AD...")
			r.logger.Info().Msg("[Rod] Faça login e aguarde a extração continuar")
			r.logger.Info().Msg("[Rod] ============================================")

			return r.extractWithVisibleLogin(ctx, chgURL)
		}

		if !strings.Contains(currentURL, "service-now.com") {
			r.logger.Error().Str("url", currentURL).Msg("[Rod] ERRO: Não estamos no ServiceNow - possível redirect inesperado")
			return &PlaywrightResult{
				Success: false,
				Error:   "Página inesperada. Não foi possível acessar o ServiceNow. Verifique sua conexão e tente novamente.",
			}, nil
		}
	}

	// Extrair dados usando JavaScript com múltiplas estratégias
	jsResult, err := targetPage.Eval(`() => {
		function getFieldValue(fieldName) {
			const selectors = [
				'input[name="' + fieldName + '"]',
				'textarea[name="' + fieldName + '"]',
				'#' + fieldName,
				'[id$="' + fieldName + '"]',
				'[data-field="' + fieldName + '"]',
				'[ng-model*="' + fieldName + '"]',
				'[data-name="' + fieldName + '"]',
			];

			for (const selector of selectors) {
				try {
					const el = document.querySelector(selector);
					if (el && (el.value || el.textContent)) {
						return el.value || el.textContent.trim();
					}
				} catch(e) {}
			}
			return '';
		}

		function getDisplayValue(fieldName) {
			const selectors = [
				'[data-field-name="' + fieldName + '"] .display-value',
				'[data-field-name="' + fieldName + '"]',
				'.variable-label:contains("' + fieldName + '") + .variable-value',
				'[aria-label*="' + fieldName + '"]',
				'label:contains("' + fieldName + '") + *',
			];

			for (const selector of selectors) {
				try {
					const el = document.querySelector(selector);
					if (el) {
						return el.value || el.textContent.trim() || '';
					}
				} catch(e) {}
			}
			return '';
		}

		let changeNumber = '';
		const titleMatch = document.title.match(/(CHG[0-9]+)/i);
		if (titleMatch) {
			changeNumber = titleMatch[1];
		}

		if (!changeNumber) {
			const numberSelectors = [
				'#sys_readonly\\.change_request\\.number',
				'[name="change_request.number"]',
				'[aria-label="Número"]',
				'[data-field-name="number"]',
				'.form-control[name="number"]',
				'input[id*="number"]',
				'span.display-value[data-field="number"]',
			];
			for (const sel of numberSelectors) {
				try {
					const el = document.querySelector(sel);
					if (el && (el.value || el.textContent)) {
						const val = (el.value || el.textContent).trim();
						if (val.match(/CHG[0-9]+/i)) {
							changeNumber = val;
							break;
						}
					}
				} catch(e) {}
			}
		}

		if (!changeNumber) {
			const allText = (document.body && document.body.innerText) || '';
			const chgMatch = allText.match(/CHG[0-9]{7,}/i);
			if (chgMatch) {
				changeNumber = chgMatch[0];
			}
		}

		let shortDescription = '';
		const shortDescSelectors = [
			'#sys_readonly\\.change_request\\.short_description',
			'[name="change_request.short_description"]',
			'[aria-label="Descrição resumida"]',
			'[aria-label="Short description"]',
			'[data-field-name="short_description"]',
			'textarea[id*="short_description"]',
			'input[id*="short_description"]',
		];
		for (const sel of shortDescSelectors) {
			try {
				const el = document.querySelector(sel);
				if (el && (el.value || el.textContent)) {
					shortDescription = (el.value || el.textContent).trim();
					if (shortDescription) break;
				}
			} catch(e) {}
		}

		// Extrair o campo "Motivo da mudança" (u_motivo_mudanca / justification).
		// O conteúdo tem o template da esteira com github.com, versão, aplicação, etc.
		function hasTemplate(text) {
			if (!text || text.length < 20) return false;
			return text.includes('github.com/') ||
			       text.includes('* Aplicação') ||
			       text.includes('* Versão:') ||
			       text.includes('* Repositório:') ||
			       (text.includes('Aplicação') && text.includes('Versão'));
		}
		function getText(el) {
			return (el.value || el.innerText || el.textContent || '').trim();
		}

		let description = '';

		// 1. Seletores diretos — u_motivo_mudanca tem prioridade (campo PT-BR da via)
		const directSels = [
			'[name="change_request.u_motivo_mudanca"]',
			'#change_request\\.u_motivo_mudanca',
			'#sys_readonly\\.change_request\\.u_motivo_mudanca',
			'textarea[id*="u_motivo"]',
			'textarea[id*="motivo"]',
			'[name="change_request.justification"]',
			'#sys_readonly\\.change_request\\.justification',
			'textarea[id*="justification"]',
			'[data-field-name="u_motivo_mudanca"]',
			'[data-field-name="justification"]',
			'[aria-label*="motivo"]',
			'[aria-label*="Motivo"]',
			'[aria-label="Justification"]',
		];
		for (const sel of directSels) {
			try {
				const el = document.querySelector(sel);
				if (!el) continue;
				const text = getText(el);
				if (hasTemplate(text)) { description = text; break; }
			} catch(e) {}
		}

		// 2. Varrer todas as textareas (campo editável)
		if (!description) {
			for (const ta of document.querySelectorAll('textarea')) {
				const text = getText(ta);
				if (hasTemplate(text)) { description = text; break; }
			}
		}

		// 3. Varrer divs/spans/pre (campo read-only no ServiceNow clássico usa innerText)
		if (!description) {
			for (const el of document.querySelectorAll('div, span, pre, td')) {
				if (el.children.length > 3) continue;
				const text = getText(el);
				if (hasTemplate(text) && text.length > 30) { description = text; break; }
			}
		}

		let state = '';
		const stateEl = document.querySelector('[name="state"], [data-field-name="state"]');
		if (stateEl) {
			if (stateEl.options && stateEl.selectedIndex >= 0) {
				state = stateEl.options[stateEl.selectedIndex].text;
			} else {
				state = stateEl.value || stateEl.textContent || '';
			}
		}

		const debugInputs = [];
		document.querySelectorAll('input[type="text"], textarea').forEach((el, i) => {
			if (i < 10 && (el.name || el.id)) {
				debugInputs.push({
					tag: el.tagName,
					name: el.name || '',
					id: el.id || '',
					valueLength: (el.value || '').length
				});
			}
		});

		return {
			changeNumber: changeNumber,
			shortDescription: shortDescription,
			description: description,
			state: state,
			debugInputs: JSON.stringify(debugInputs)
		};
	}`)

	if err != nil {
		r.logger.Error().Err(err).Msg("[Rod] ERRO ao executar JavaScript de extração")
		return nil, fmt.Errorf("erro ao extrair dados: %v", err)
	}

	r.logger.Info().Msg("[Rod] JavaScript executado com sucesso, processando resultado...")

	if jsResult == nil || jsResult.Value.Nil() {
		r.logger.Error().Msg("[Rod] Resultado do JavaScript é nil ou vazio")
		return nil, fmt.Errorf("resultado da extração está vazio")
	}

	data := jsResult.Value.Map()
	r.logger.Debug().Int("fields_count", len(data)).Msg("[Rod] Mapa de dados obtido do JavaScript")

	changeNumber := ""
	if v, ok := data["changeNumber"]; ok {
		changeNumber = v.String()
		r.logger.Debug().Str("value", changeNumber).Msg("[Rod] Campo changeNumber extraído")
	}

	shortDescription := ""
	if v, ok := data["shortDescription"]; ok {
		shortDescription = v.String()
		r.logger.Debug().Str("value", shortDescription).Msg("[Rod] Campo shortDescription extraído")
	}

	description := ""
	if v, ok := data["description"]; ok {
		description = v.String()
		r.logger.Debug().Int("length", len(description)).Msg("[Rod] Campo description extraído")
	}

	state := ""
	if v, ok := data["state"]; ok {
		state = v.String()
		r.logger.Debug().Str("value", state).Msg("[Rod] Campo state extraído")
	}

	if v, ok := data["debugInputs"]; ok {
		r.logger.Debug().Str("inputs", v.String()).Msg("[Rod] Debug: inputs/textareas encontrados na página")
	}

	r.logger.Info().Msg("[Rod] Parseando description para extrair dados estruturados...")
	extracted := r.parseDescription(description)

	r.logger.Info().
		Str("changeNumber", changeNumber).
		Str("application", extracted.Application).
		Str("version", extracted.Version).
		Str("squad", extracted.Squad).
		Str("github_repo", extracted.GitHubRepo).
		Int("jira_issues", len(extracted.JiraIssues)).
		Msg("[Rod] ========== EXTRAÇÃO CONCLUÍDA COM SUCESSO ==========")

	return &PlaywrightResult{
		Success:          true,
		ChangeNumber:     changeNumber,
		ShortDescription: shortDescription,
		Description:      description,
		State:            state,
		Extracted:        extracted,
	}, nil
}

// parseDescription extrai dados estruturados do campo description
func (r *RodExtractor) parseDescription(description string) *ExtractedData {
	extracted := &ExtractedData{}

	appRegex := regexp.MustCompile(`\* Aplicação\(ões\):\s*([^.\n]+)\.`)
	if match := appRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Application = strings.TrimSpace(match[1])
	}

	// Captura a linha inteira (não só dígitos/pontos) — cobre tags alfanuméricas reais
	// (ex: "choic-4437_cnpj_v6-1", usada por outros squads/produtos, sem formato semver).
	// O "." final do template é removido à parte (TrimSuffix), já que a própria tag pode
	// conter pontos (ex: "4.0.4-3"), tornando ambíguo tentar excluir isso via regex.
	versionRegex := regexp.MustCompile(`\* Versão:\s*([^\n]+)`)
	if match := versionRegex.FindStringSubmatch(description); len(match) > 1 {
		v := strings.TrimSpace(match[1])
		v = strings.TrimSuffix(v, ".")
		extracted.Version = v
	}

	// Cobre os dois formatos de template já vistos em CHGs reais:
	//   "* Repositório: github.com/org/repo.git"        (formato antigo)
	//   "* URL do Repositório: github.com/org/repo.git" (formato atual — "Nome do
	//   Repositório:" sozinho não é confiável: já vimos CHG onde o "Projeto"/"Aplicação(ões)"
	//   tem sufixo extra (ex: "-b2c") que não existe no repo real, então preferimos sempre a
	//   URL completa, que também evita ambiguidade de owner/org).
	repoRegex := regexp.MustCompile(`\*\s*(?:URL do )?Repositório:\s*github\.com/[^/]+/([^.]+)\.git`)
	if match := repoRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.GitHubRepo = strings.TrimSpace(match[1])
	}

	squadRegex := regexp.MustCompile(`\* Squad\(s\):\s*([^.\n]+)\.`)
	if match := squadRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Squad = strings.TrimSpace(match[1])
	}

	branchRegex := regexp.MustCompile(`\* Branch no GitHub:\s*([^\n]+)\.`)
	if match := branchRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Branch = strings.TrimSpace(match[1])
	}

	productRegex := regexp.MustCompile(`\* Produto:\s*([^.\n]+)\.`)
	if match := productRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Product = strings.TrimSpace(match[1])
	}

	xlURLRegex := regexp.MustCompile(`\* Link da release no XL-Release:\s*(https?://[^\s\n]+)`)
	if match := xlURLRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.XLReleaseURL = strings.TrimSpace(match[1])
	}

	xlTitleRegex := regexp.MustCompile(`\* Titulo da release no XL-Release:\s*([^\n]+)\.`)
	if match := xlTitleRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.XLReleaseTitle = strings.TrimSpace(match[1])
	}

	jiraRegex := regexp.MustCompile(`([A-Z]+-\d+)`)
	if matches := jiraRegex.FindAllString(description, -1); len(matches) > 0 {
		seen := make(map[string]bool)
		unique := []string{}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				unique = append(unique, m)
			}
		}
		extracted.JiraIssues = unique
	}

	return extracted
}

// ExtractWithSSE executa extração com eventos SSE para progresso
func (r *RodExtractor) ExtractWithSSE(ctx context.Context, chgURL string, progressChan chan<- string) (*PlaywrightResult, error) {
	if progressChan != nil {
		progressChan <- "Iniciando browser Chromium..."
	}

	result, err := r.Extract(ctx, chgURL)

	if progressChan != nil {
		if err != nil {
			progressChan <- fmt.Sprintf("Erro: %v", err)
		} else if result.Success {
			progressChan <- fmt.Sprintf("CHG %s extraída com sucesso!", result.ChangeNumber)
		} else {
			progressChan <- fmt.Sprintf("Falha: %s", result.Error)
		}
	}

	return result, err
}

// extractWithVisibleLogin abre browser visível para login e depois extrai dados
func (r *RodExtractor) extractWithVisibleLogin(ctx context.Context, chgURL string) (*PlaywrightResult, error) {
	r.logger.Info().Str("url", chgURL).Msg("[Rod] Iniciando extração com login visível...")

	browser, closeFunc, err := r.launchBrowser(false, chgURL)
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar browser para login: %v", err)
	}
	defer closeFunc()

	page, err := browser.Page(proto.TargetCreateTarget{URL: chgURL})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	defer page.Close() //nolint:errcheck
	if _, activateErr := page.Activate(); activateErr != nil {
		r.logger.Warn().Err(activateErr).Msg("[Rod] Aviso: não foi possível ativar aba")
	}

	r.logger.Info().Msg("[Rod] Browser aberto - faça login no Azure AD...")
	r.logger.Info().Msg("[Rod] A página só fecha quando o login terminar ou a extração for cancelada")

	// loginTimeout é só um teto de segurança (ver comentário de maxLoginWait) — o encerramento
	// normal é por sucesso de login ou pelo usuário cancelar a extração no frontend (que aborta
	// esta mesma requisição HTTP e cancela ctx).
	loginTimeout := maxLoginWait
	startTime := time.Now()

	loginPatterns := []string{
		"login.microsoftonline.com",
		"login.windows.net",
		"login.live.com",
	}

	for {
		if ctx.Err() != nil {
			r.logger.Info().Msg("[Rod] Extração cancelada durante o login visível")
			return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
		}

		if time.Since(startTime) > loginTimeout {
			return &PlaywrightResult{
				Success: false,
				Error:   "Tempo máximo de espera pelo login no Azure AD atingido. Tente novamente.",
			}, nil
		}

		pageInfo, err := page.Info()
		if err != nil {
			if waitOrCancel(ctx, 2*time.Second) {
				return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
			}
			continue
		}
		currentURL := pageInfo.URL

		isOnLogin := false
		for _, pattern := range loginPatterns {
			if strings.Contains(currentURL, pattern) {
				isOnLogin = true
				break
			}
		}

		if strings.Contains(currentURL, "service-now.com") && !isOnLogin && !strings.Contains(currentURL, "saml") {
			r.logger.Info().Str("url", currentURL).Msg("[Rod] Login completado! Aguardando página carregar...")
			page.WaitLoad() //nolint:errcheck
			waitOrCancel(ctx, 1*time.Second)
			break
		}

		if waitOrCancel(ctx, 300*time.Millisecond) {
			return &PlaywrightResult{Success: false, Error: "Extração cancelada."}, nil
		}
	}

	r.logger.Info().Msg("[Rod] Extraindo dados da CHG...")

	var targetPage *rod.Page

	frames, _ := page.Elements("iframe")
	for _, frame := range frames {
		name, _ := frame.Attribute("name")
		if name != nil && *name == "gsft_main" {
			framePage, err := frame.Frame()
			if err == nil {
				targetPage = framePage
				r.logger.Info().Msg("[Rod] Usando iframe gsft_main")
				break
			}
		}
	}

	if targetPage == nil {
		targetPage = page
	}

	// Aguardar conteúdo do template CHG (mesma lógica do caminho principal)
	hasTemplateJSVL := `() => {
		function hasTemplate(text) {
			if (!text || text.length < 20) return false;
			return text.includes('github.com/') ||
			       text.includes('* Aplicação') ||
			       text.includes('* Versão:') ||
			       text.includes('* Repositório:');
		}
		for (const ta of document.querySelectorAll('textarea')) {
			if (hasTemplate(ta.value || ta.textContent || '')) return true;
		}
		for (const el of document.querySelectorAll('div, span, pre, td')) {
			if (el.children.length > 3) continue;
			if (hasTemplate(el.innerText || el.textContent || '')) return true;
		}
		return false;
	}`
	deadlineVL := time.Now().Add(20 * time.Second)
	formReadyVL := false
	for time.Now().Before(deadlineVL) {
		if res, err := targetPage.Eval(hasTemplateJSVL); err == nil && res != nil && res.Value.Bool() {
			formReadyVL = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !formReadyVL {
		time.Sleep(3 * time.Second)
	}

	jsResult, err := targetPage.Eval(`() => {
		function getFieldValue(fieldName) {
			const selectors = [
				'input[name="' + fieldName + '"]',
				'textarea[name="' + fieldName + '"]',
				'#' + fieldName,
				'[id$="' + fieldName + '"]',
			];
			for (const selector of selectors) {
				try {
					const el = document.querySelector(selector);
					if (el && (el.value || el.textContent)) {
						return el.value || el.textContent.trim();
					}
				} catch(e) {}
			}
			return '';
		}

		let changeNumber = '';
		const titleMatch = document.title.match(/(CHG[0-9]+)/i);
		if (titleMatch) changeNumber = titleMatch[1];
		if (!changeNumber) {
			const allText = (document.body && document.body.innerText) || '';
			const chgMatch = allText.match(/CHG[0-9]{7,}/i);
			if (chgMatch) changeNumber = chgMatch[0];
		}

		let shortDescription = getFieldValue('short_description') ||
			getFieldValue('sys_readonly.change_request.short_description') || '';

		function hasTemplate(text) {
			if (!text || text.length < 20) return false;
			return text.includes('github.com/') ||
			       text.includes('* Aplicação') ||
			       text.includes('* Versão:') ||
			       text.includes('* Repositório:') ||
			       (text.includes('Aplicação') && text.includes('Versão'));
		}
		function getText(el) {
			return (el.value || el.innerText || el.textContent || '').trim();
		}

		let description = '';
		const directSels = [
			'[name="change_request.u_motivo_mudanca"]',
			'#change_request\\.u_motivo_mudanca',
			'#sys_readonly\\.change_request\\.u_motivo_mudanca',
			'textarea[id*="u_motivo"]',
			'[name="change_request.justification"]',
			'#sys_readonly\\.change_request\\.justification',
			'[data-field-name="u_motivo_mudanca"]',
			'[data-field-name="justification"]',
		];
		for (const sel of directSels) {
			try {
				const el = document.querySelector(sel);
				if (!el) continue;
				const text = getText(el);
				if (hasTemplate(text)) { description = text; break; }
			} catch(e) {}
		}
		if (!description) {
			for (const ta of document.querySelectorAll('textarea')) {
				const text = getText(ta);
				if (hasTemplate(text)) { description = text; break; }
			}
		}
		if (!description) {
			for (const el of document.querySelectorAll('div, span, pre, td')) {
				if (el.children.length > 3) continue;
				const text = getText(el);
				if (hasTemplate(text) && text.length > 30) { description = text; break; }
			}
		}

		return {
			changeNumber: changeNumber,
			shortDescription: shortDescription,
			description: description,
			state: ''
		};
	}`)

	if err != nil {
		return nil, fmt.Errorf("erro ao extrair dados: %v", err)
	}

	data := jsResult.Value.Map()

	changeNumber := ""
	if v, ok := data["changeNumber"]; ok {
		changeNumber = v.String()
	}

	shortDescription := ""
	if v, ok := data["shortDescription"]; ok {
		shortDescription = v.String()
	}

	description := ""
	if v, ok := data["description"]; ok {
		description = v.String()
	}

	extracted := r.parseDescription(description)

	r.logger.Info().
		Str("changeNumber", changeNumber).
		Str("application", extracted.Application).
		Str("version", extracted.Version).
		Msg("[Rod] Extração com login visível concluída!")

	return &PlaywrightResult{
		Success:          true,
		ChangeNumber:     changeNumber,
		ShortDescription: shortDescription,
		Description:      description,
		State:            "",
		Extracted:        extracted,
	}, nil
}
