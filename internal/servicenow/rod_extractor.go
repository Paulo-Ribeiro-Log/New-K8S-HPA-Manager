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
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// RodExtractor usa a biblioteca Rod (Go nativo) para extrair dados do ServiceNow
// Não precisa de Node.js, npm ou dependências externas
type RodExtractor struct {
	logger      *zerolog.Logger
	sessionDir  string // Diretório de sessão Chromium (~/.k8s-hpa-manager/rod-session)
	mu          sync.Mutex // protege browser/browserStop
	extractMu   sync.Mutex // serializa extrações — ServiceNow invalida token com abas paralelas
	browser     *rod.Browser // browser persistente — reutilizado entre extrações (N CHGs = 1 browser)
	browserStop func()
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
	r.mu.Lock()
	if r.browser != nil {
		if r.browserStop != nil {
			r.browserStop()
		}
		r.browser = nil
		r.browserStop = nil
	}
	r.mu.Unlock()

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

// launchLocalBrowserWithDir inicia o Chromium local com um diretório de sessão específico.
// Para sessões visíveis (headless=false) sem display gráfico, tenta Xvfb automaticamente.
func (r *RodExtractor) launchLocalBrowserWithDir(headless bool, sessionDir string) (*rod.Browser, func(), error) {
	// Remover lock files residuais de crashes anteriores
	for _, lock := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		lockPath := filepath.Join(sessionDir, lock)
		if _, err := os.Stat(lockPath); err == nil {
			r.logger.Warn().Str("file", lockPath).Msg("[Rod] Removendo lock residual de crash anterior")
			os.Remove(lockPath) //nolint:errcheck
		}
	}

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

	l := launcher.New().
		UserDataDir(sessionDir).
		Headless(headless).
		Set("disable-blink-features", "AutomationControlled").
		// Flags de estabilidade WSL2: sem segundo argumento → Rod gera --flag (não --flag=)
		Set("disable-dev-shm-usage").
		Set("no-sandbox").
		Set("disable-gpu").
		Set("disable-setuid-sandbox")

	r.logger.Info().
		Bool("headless", headless).
		Str("session_dir", sessionDir).
		Bool("has_display", HasGraphicalDisplay()).
		Msg("[Rod] Iniciando Chromium local...")

	ctrlURL, err := l.Launch()
	if err != nil {
		if xvfbCleanup != nil {
			xvfbCleanup()
		}
		return nil, nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}

	b := rod.New().ControlURL(ctrlURL)
	if err := b.Connect(); err != nil {
		if xvfbCleanup != nil {
			xvfbCleanup()
		}
		return nil, nil, fmt.Errorf("erro ao conectar ao browser: %v", err)
	}

	r.logger.Info().Msg("[Rod] Chromium local iniciado com sucesso")
	return b, func() {
		b.Close()
		if xvfbCleanup != nil {
			xvfbCleanup()
		}
	}, nil
}

// getBrowser retorna o browser headless persistente, criando um novo se necessário.
// Reutilizar o mesmo browser entre extrações evita abrir N janelas para N CHGs.
func (r *RodExtractor) getBrowser() (*rod.Browser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.browser != nil {
		if _, err := r.browser.Pages(); err == nil {
			return r.browser, nil
		}
		// Browser morreu — cleanup e recria
		if r.browserStop != nil {
			r.browserStop()
		}
		r.browser = nil
		r.browserStop = nil
	}

	b, stop, err := r.launchLocalBrowserWithDir(true, r.activeSessionDir())
	if err != nil {
		return nil, err
	}
	r.browser = b
	r.browserStop = stop
	r.logger.Info().Msg("[Rod] Browser persistente iniciado")
	return b, nil
}

// TestSession abre o Chromium para o usuário fazer login no ServiceNow.
// Em WSL sem display gráfico, usa Xvfb (X Virtual Framebuffer) como display virtual.
// O browser é invisível no Xvfb — use WSLg ou x11vnc para visualizá-lo,
// ou confie no SSO silencioso se a máquina for domain-joined.
func (r *RodExtractor) TestSession(ctx context.Context) (*SessionStatus, error) {
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
	r.mu.Lock()
	if r.browser != nil {
		if r.browserStop != nil {
			r.browserStop()
		}
		r.browser = nil
		r.browserStop = nil
	}
	r.mu.Unlock()

	// Limpar sessão anterior para garantir login fresco
	r.logger.Info().Msg("[Rod] Limpando sessão anterior para garantir login fresco...")
	os.RemoveAll(sessionDir)  //nolint:errcheck
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
	r.logger.Info().Msg("[Rod] O browser vai ficar aberto por até 3 minutos")
	r.logger.Info().Msg("[Rod] ============================================")

	loginTimeout := 3 * time.Minute
	startTime := time.Now()

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
		pageInfo, err := page.Info()
		if err != nil {
			time.Sleep(2 * time.Second)
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
				time.Sleep(2 * time.Second)
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

		time.Sleep(2 * time.Second)
	}

	if !loginPageDetected {
		r.logger.Warn().Msg("[Rod] Página de login não detectada após 30s, aguardando mais...")
	}

	// FASE 2: Aguardar o usuário completar o login (até 3 minutos)
	r.logger.Info().Msg("[Rod] Aguardando você completar o login no Azure AD...")

	for {
		elapsed := time.Since(startTime)
		if elapsed > loginTimeout {
			r.logger.Warn().Msg("[Rod] Timeout aguardando login (3 minutos)")
			page.Close() //nolint:errcheck
			closeFunc()
			return &SessionStatus{
				Valid:   false,
				Status:  "timeout",
				Message: "Timeout aguardando login. Tente novamente.",
			}, nil
		}

		pageInfo, err := page.Info()
		if err != nil {
			time.Sleep(2 * time.Second)
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

		if strings.Contains(currentURL, "service-now.com") &&
			!isOnLogin &&
			!strings.Contains(currentURL, "saml") &&
			!strings.Contains(currentURL, "login") {

			r.logger.Info().Str("url", currentURL).Dur("elapsed", elapsed).Msg("[Rod] LOGIN COMPLETADO COM SUCESSO!")

			r.logger.Info().Msg("[Rod] Fase 4: Aguardando página carregar (5s)...")
			time.Sleep(5 * time.Second)

			r.logger.Info().Msg("[Rod] Navegando para home do ServiceNow...")
			page.Navigate("https://viavarejo.service-now.com/now/nav/ui/home") //nolint:errcheck
			time.Sleep(5 * time.Second)

			r.logger.Info().Msg("[Rod] Aguardando cookies serem salvos (5s)...")
			time.Sleep(5 * time.Second)

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

		time.Sleep(2 * time.Second)
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
	// Funciona em WSL2 quando o usuário já está logado no Chrome do Windows.
	// Só aceita o resultado se a descrição/justificativa veio preenchida —
	// o ServiceNow pode retornar o registro com campos restritos vazios via ACL.
	if IsWSL() {
		if cookies, cerr := ExtractChromeCookiesWSL(snCookieDomain); cerr == nil {
			snd := NewSNDirectClient(cookies)
			if apiResult, apiErr := snd.FetchFromURL(chgURL); apiErr == nil && apiResult.Success && apiResult.Description != "" {
				r.logger.Info().Str("chg", apiResult.ChangeNumber).Msg("[Rod] Extração via REST API concluída (caminho rápido, sem browser)")
				return apiResult, nil
			}
			r.logger.Debug().Msg("[Rod] REST API sem descrição ou falhou — usando browser como fallback")
		} else {
			r.logger.Debug().Err(cerr).Msg("[Rod] Sem cookies do Chrome no Windows — usando browser")
		}
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

	// Browser persistente: reutilizado entre extrações — N CHGs = 1 Chromium aberto
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
			time.Sleep(2 * time.Second)
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
			r.logger.Warn().Bool("headless", headless).Str("url", currentURL).Msg("[Rod] Sessão expirada - login necessário mas estamos em modo headless")
			return &PlaywrightResult{
				Success: false,
				Error:   "Sessão expirada. Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair dados.",
			}, nil
		}

		if strings.Contains(currentURL, "service-now.com") && !isLoginPage && !strings.Contains(currentURL, "login") {
			r.logger.Info().Str("url", currentURL).Dur("elapsed", elapsed).Int("loops", loopCount).Msg("[Rod] Acesso ao ServiceNow confirmado!")
			break
		}

		time.Sleep(300 * time.Millisecond)
	}

	// Aguardar carregamento completo.
	// ServiceNow é SPA: o evento "load" dispara antes do router interno terminar de
	// renderizar o formulário, então 1s de buffer evita document.body == null no eval JS.
	r.logger.Info().Msg("[Rod] Aguardando carregamento da página...")
	if err := page.WaitLoad(); err != nil {
		r.logger.Warn().Err(err).Msg("[Rod] Aviso: erro ao aguardar WaitLoad (continuando mesmo assim)")
	}
	time.Sleep(1 * time.Second)

	r.logger.Info().Msg("[Rod] Página carregada, buscando iframes...")

	// Tentar encontrar o iframe gsft_main
	var targetPage *rod.Page
	frames, err := page.Elements("iframe")
	if err != nil {
		r.logger.Warn().Err(err).Msg("[Rod] Aviso: erro ao buscar iframes")
		frames = nil
	} else {
		r.logger.Info().Int("count", len(frames)).Msg("[Rod] Iframes encontrados")
	}

	for _, frame := range frames {
		name, _ := frame.Attribute("name")
		if name != nil {
			r.logger.Debug().Str("iframe_name", *name).Msg("[Rod] Verificando iframe...")
			if *name == "gsft_main" {
				framePage, err := frame.Frame()
				if err == nil {
					targetPage = framePage
					r.logger.Info().Msg("[Rod] Usando iframe gsft_main para extração")
					break
				} else {
					r.logger.Warn().Err(err).Msg("[Rod] Erro ao acessar iframe gsft_main")
				}
			}
		}
	}

	if targetPage == nil {
		targetPage = page
		r.logger.Info().Msg("[Rod] Usando página principal para extração (sem iframe)")
	}

	// Aguardar o conteúdo do template CHG aparecer na página.
	// Marcadores do template (github.com/, * Aplicação, * Versão:) só surgem depois
	// que o AJAX do ServiceNow preenche o campo "Motivo da mudança" (u_motivo_mudanca).
	r.logger.Info().Msg("[Rod] Aguardando conteúdo do template CHG carregar...")
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
	formReady := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if res, err := targetPage.Eval(hasTemplateJS); err == nil && res != nil && res.Value.Bool() {
			r.logger.Info().Msg("[Rod] Conteúdo do template CHG detectado — pronto para extrair")
			formReady = true
			break
		}
		if targetPage != page {
			if res, err := page.Eval(hasTemplateJS); err == nil && res != nil && res.Value.Bool() {
				r.logger.Info().Msg("[Rod] Conteúdo do template CHG detectado na página externa")
				formReady = true
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !formReady {
		r.logger.Warn().Msg("[Rod] Template não detectado em 20s — usando 3s de fallback")
		time.Sleep(3 * time.Second)
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
			os.RemoveAll(activeDir)  //nolint:errcheck
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

	versionRegex := regexp.MustCompile(`\* Versão:\s*([\d]+\.[\d]+\.[\d]+-?[\d]*)\.`)
	if match := versionRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Version = strings.TrimSpace(match[1])
	}

	repoRegex := regexp.MustCompile(`\* Repositório:\s*github\.com/[^/]+/([^.]+)\.git`)
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

	loginTimeout := 3 * time.Minute
	startTime := time.Now()

	loginPatterns := []string{
		"login.microsoftonline.com",
		"login.windows.net",
		"login.live.com",
	}

	for {
		if time.Since(startTime) > loginTimeout {
			return &PlaywrightResult{
				Success: false,
				Error:   "Timeout aguardando login no Azure AD (3 minutos)",
			}, nil
		}

		pageInfo, err := page.Info()
		if err != nil {
			time.Sleep(2 * time.Second)
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
			time.Sleep(1 * time.Second)
			break
		}

		time.Sleep(300 * time.Millisecond)
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
