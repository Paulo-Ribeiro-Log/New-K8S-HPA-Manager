package servicenow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
)

// RodExtractor usa a biblioteca Rod (Go nativo) para extrair dados do ServiceNow
// Não precisa de Node.js, npm ou dependências externas
type RodExtractor struct {
	logger            *zerolog.Logger
	sessionDir        string // Diretório de sessão local (WSL/Linux/Mac)
	windowsSessionDir string // Diretório de sessão Windows via CDP (apenas no WSL sem display)
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

	// Inicializar diretório de sessão Windows quando rodando no WSL
	var windowsSessionDir string
	if IsWSL() {
		windowsSessionDir = WindowsSessionWSLDir
		if err := os.MkdirAll(windowsSessionDir, 0755); err != nil {
			logger.Warn().Err(err).Str("dir", windowsSessionDir).Msg("[Rod] Erro ao criar diretório de sessão Windows")
		}
	}

	return &RodExtractor{
		logger:            logger,
		sessionDir:        sessionDir,
		windowsSessionDir: windowsSessionDir,
	}
}

// IsConfigured sempre retorna true pois Rod baixa o browser automaticamente
func (r *RodExtractor) IsConfigured() bool {
	return true
}

// GetStatus retorna o status da configuração do Rod
// Retorna campos compatíveis com o frontend (playwright_configured, script_exists)
func (r *RodExtractor) GetStatus() map[string]interface{} {
	wslMode := NeedsWindowsBrowser()
	activeDir := r.activeSessionDir()

	status := map[string]interface{}{
		// Campos para compatibilidade com frontend (espera playwright_configured e script_exists)
		"playwright_configured": true, // Rod está sempre configurado (Go nativo)
		"script_exists":         true, // Não precisa de script externo
		"configured":            true,
		"session_dir":           activeDir,
		"type":                  "rod-go-native",
		"dependencies":          "none (Go native)",
		"npx_available":         true, // Não precisa de npx
		"ts_node_available":     true, // Não precisa de ts-node
		"npm_installed":         true, // Não precisa de npm
		// Informações sobre o modo de execução
		"wsl_mode":    wslMode,                  // true = WSL sem display, usa Chrome Windows via CDP
		"is_wsl":      IsWSL(),                  // true = rodando no WSL (com ou sem display)
		"has_display": HasGraphicalDisplay(),    // true = display gráfico disponível
	}

	// Verificar se sessão existe
	if _, err := os.Stat(activeDir); err == nil {
		status["session_exists"] = true
	} else {
		status["session_exists"] = false
	}

	return status
}

// activeSessionDir retorna o diretório de sessão ativo conforme o ambiente.
// No WSL sem display gráfico usa o diretório Windows; caso contrário usa o local.
func (r *RodExtractor) activeSessionDir() string {
	if NeedsWindowsBrowser() && r.windowsSessionDir != "" {
		return r.windowsSessionDir
	}
	return r.sessionDir
}

// GetSessionStatus retorna o status da sessão
func (r *RodExtractor) GetSessionStatus() *SessionStatus {
	sessionDir := r.activeSessionDir()
	status := &SessionStatus{
		SessionDir: sessionDir,
	}

	// Verificar se o diretório de sessão existe
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

	// Calcular tempo desde última modificação
	hoursSince := time.Since(lastMod).Hours()
	status.HoursSinceUpdate = hoursSince

	// Verificar se existem arquivos de sessão
	entries, _ := os.ReadDir(sessionDir)
	hasSession := len(entries) > 0

	// Sessões do Azure AD geralmente expiram em 8-12 horas
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
	os.MkdirAll(dir, 0755)

	r.logger.Info().Str("dir", dir).Msg("[Rod] Sessão limpa com sucesso")
	return nil
}

// launchBrowser inicia um browser e retorna o browser, uma função de cleanup e erro.
// Detecta automaticamente o ambiente:
//   - WSL sem display gráfico → Chrome/Edge Windows via CDP (porta 9223)
//     Chrome Windows é obrigatório neste modo pois gerencia os cookies DPAPI do Windows.
//     O Chromium Linux não consegue descriptografar o perfil do Chrome Windows.
//   - Demais casos → Chromium local via launcher padrão do Rod
//
// initialURL: se não vazio, o browser abre diretamente nessa URL (apenas no modo Windows;
// no modo local a URL é passada via browser.Page pelo caller).
func (r *RodExtractor) launchBrowser(headless bool, initialURL string) (*rod.Browser, func(), error) {
	if NeedsWindowsBrowser() {
		return r.launchWindowsBrowser(initialURL)
	}
	return r.launchLocalBrowser(headless)
}

// launchLocalBrowser usa o launcher padrão do Rod (baixa Chromium automaticamente)
func (r *RodExtractor) launchLocalBrowser(headless bool) (*rod.Browser, func(), error) {
	return r.launchLocalBrowserWithDir(headless, r.activeSessionDir())
}

// launchLocalBrowserWithDir inicia o Chromium local com um diretório de sessão específico.
// Usado para reutilizar sessão salva (ex: sessão Windows acessível via /mnt/c/).
func (r *RodExtractor) launchLocalBrowserWithDir(headless bool, sessionDir string) (*rod.Browser, func(), error) {
	l := launcher.New().
		UserDataDir(sessionDir).
		Headless(headless).
		Set("disable-blink-features", "AutomationControlled")

	r.logger.Info().
		Bool("headless", headless).
		Str("session_dir", sessionDir).
		Msg("[Rod] Iniciando Chromium local...")

	ctrlURL, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}

	b := rod.New().ControlURL(ctrlURL)
	if err := b.Connect(); err != nil {
		return nil, nil, fmt.Errorf("erro ao conectar ao browser: %v", err)
	}

	r.logger.Info().Msg("[Rod] Chromium local iniciado com sucesso")
	return b, func() { b.Close() }, nil
}

// launchWindowsBrowser conecta ao Chrome/Edge do Windows via CDP.
// Lança o browser Windows se ainda não estiver rodando com a porta de debug.
// initialURL: se não vazio, Chrome abre diretamente nessa URL (sem aba em branco).
// O browser Windows NÃO é fechado no cleanup — apenas desconectamos.
func (r *RodExtractor) launchWindowsBrowser(initialURL string) (*rod.Browser, func(), error) {
	port := WindowsCDPPort

	// Verificar se CDP já está disponível (browser já aberto com a porta de debug).
	// Tenta 127.0.0.1 E o IP do host Windows (WSL2 pode não ter localhost forwarding ativo).
	cdpHost, checkErr := WaitCDPReadyHost(port, 3*time.Second)
	if checkErr != nil {
		// CDP não disponível — precisamos lançar o browser Windows
		browserPath, findErr := FindWindowsBrowser()
		if findErr != nil {
			return nil, nil, fmt.Errorf("[Rod WSL] %v", findErr)
		}

		r.logger.Info().
			Str("browser", browserPath).
			Int("port", port).
			Str("session_dir", r.windowsSessionDir).
			Str("initial_url", initialURL).
			Msg("[Rod WSL] Lançando Chrome/Edge Windows com remote debugging...")

		if _, launchErr := LaunchWindowsBrowserForCDP(browserPath, port, r.windowsSessionDir, initialURL); launchErr != nil {
			return nil, nil, launchErr
		}

		// Aguardar o browser Windows iniciar e responder ao CDP (até 40s).
		// Chrome no Windows pode demorar mais em máquinas lentas ou com muitos processos.
		r.logger.Info().Msg("[Rod WSL] Aguardando browser Windows iniciar (até 40s)...")
		var waitErr error
		cdpHost, waitErr = WaitCDPReadyHost(port, 40*time.Second)
		if waitErr != nil {
			return nil, nil, fmt.Errorf("[Rod WSL] browser Windows não respondeu ao CDP na porta %d: %v\n"+
				"Dica: abra o Chrome/Edge manualmente com: chrome.exe --remote-debugging-port=%d", port, waitErr, port)
		}
	}

	r.logger.Info().Str("host", cdpHost).Int("port", port).Msg("[Rod WSL] Conectando ao browser Windows via CDP...")
	wsURL := fmt.Sprintf("ws://%s:%d", cdpHost, port)
	b := rod.New().ControlURL(wsURL)
	if err := b.Connect(); err != nil {
		return nil, nil, fmt.Errorf("[Rod WSL] erro ao conectar ao browser Windows via CDP: %v", err)
	}

	r.logger.Info().Str("host", cdpHost).Msg("[Rod WSL] Conectado ao browser Windows com sucesso!")

	// Cleanup apenas desconecta — NÃO fecha o Chrome do usuário
	cleanup := func() {
		r.logger.Debug().Msg("[Rod WSL] Desconectando do browser Windows (Chrome permanece aberto)")
	}
	return b, cleanup, nil
}

// TestSession abre o browser para o usuário fazer login
func (r *RodExtractor) TestSession(ctx context.Context) (*SessionStatus, error) {
	sessionDir := r.activeSessionDir()
	r.logger.Info().
		Str("session_dir", sessionDir).
		Bool("wsl_mode", NeedsWindowsBrowser()).
		Msg("[Rod] Iniciando login - abrindo browser visível")

	// Limpar sessão anterior para garantir login fresco
	r.logger.Info().Msg("[Rod] Limpando sessão anterior para garantir login fresco...")
	os.RemoveAll(sessionDir)
	os.MkdirAll(sessionDir, 0755)

	// Iniciar browser abrindo diretamente no ServiceNow (sem aba em branco)
	const serviceNowHome = "https://viavarejo.service-now.com"
	browser, closeFunc, err := r.launchBrowser(false, serviceNowHome)
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}

	// Navegar para ServiceNow (modo local: abrir nova aba; modo Windows: já abriu na URL)
	page, err := browser.Page(proto.TargetCreateTarget{URL: serviceNowHome})
	if err != nil {
		closeFunc()
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	// Trazer a aba para frente (necessário no modo Windows: Chrome abre com aba em branco, Rod cria segunda aba)
	if _, activateErr := page.Activate(); activateErr != nil {
		r.logger.Warn().Err(activateErr).Msg("[Rod] Aviso: não foi possível ativar aba do ServiceNow")
	}

	r.logger.Info().Msg("[Rod] Navegando para ServiceNow...")
	r.logger.Info().Msg("[Rod] ============================================")
	r.logger.Info().Msg("[Rod] AGUARDANDO VOCÊ FAZER LOGIN NO AZURE AD...")
	r.logger.Info().Msg("[Rod] O browser vai ficar aberto por até 3 minutos")
	r.logger.Info().Msg("[Rod] ============================================")

	// Aguardar login do usuário (máximo 3 minutos)
	loginTimeout := 3 * time.Minute
	startTime := time.Now()

	loginPatterns := []string{
		"login.microsoftonline.com",
		"login.windows.net",
		"login.live.com",
	}

	// FASE 1: Aguardar redirect inicial para página de login
	// ServiceNow pode levar até 15 segundos para detectar falta de sessão e redirecionar
	r.logger.Info().Msg("[Rod] Fase 1: Aguardando redirect para Azure AD (até 30s)...")

	loginPageDetected := false
	serviceNowLoadedCount := 0

	for i := 0; i < 15; i++ { // Máximo 30 segundos para detectar página de login
		pageInfo, err := page.Info()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		currentURL := pageInfo.URL
		r.logger.Debug().Str("url", currentURL).Int("check", i+1).Msg("[Rod] Verificando URL...")

		// Verificar se está na página de login do Azure AD
		for _, pattern := range loginPatterns {
			if strings.Contains(currentURL, pattern) {
				loginPageDetected = true
				r.logger.Info().
					Str("url", currentURL).
					Msg("[Rod] Página de login do Azure AD detectada!")
				break
			}
		}

		if loginPageDetected {
			break
		}

		// Se está no ServiceNow, contar quantas vezes consecutivas
		// Só considera "já logado" se ficar no ServiceNow por 5 verificações seguidas (10s)
		// Isso evita falso positivo durante o carregamento inicial
		if strings.Contains(currentURL, "service-now.com") &&
			!strings.Contains(currentURL, "saml") &&
			!strings.Contains(currentURL, "login") {
			serviceNowLoadedCount++
			r.logger.Debug().Int("count", serviceNowLoadedCount).Msg("[Rod] ServiceNow carregado, aguardando confirmação...")

			// Só considera logado se ficar no ServiceNow por 5 checks seguidos (10 segundos)
			if serviceNowLoadedCount >= 5 {
				r.logger.Info().Msg("[Rod] Confirmado: já está autenticado no ServiceNow (sessão anterior válida)")
				time.Sleep(2 * time.Second)
				page.Close()
				closeFunc()
				status := r.GetSessionStatus()
				status.Valid = true
				status.Message = "Sessão já está válida."
				return status, nil
			}
		} else {
			serviceNowLoadedCount = 0 // Reset se mudou de página
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
			page.Close()
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

		// Verificar se SAIU da página de login e CHEGOU no ServiceNow
		isOnLogin := false
		for _, pattern := range loginPatterns {
			if strings.Contains(currentURL, pattern) {
				isOnLogin = true
				break
			}
		}

		// Sucesso: está no ServiceNow E não está em página de login/saml
		if strings.Contains(currentURL, "service-now.com") &&
			!isOnLogin &&
			!strings.Contains(currentURL, "saml") &&
			!strings.Contains(currentURL, "login") {

			r.logger.Info().
				Str("url", currentURL).
				Dur("elapsed", elapsed).
				Msg("[Rod] LOGIN COMPLETADO COM SUCESSO!")

			// FASE 4: Persistir cookies (aguardar bastante)
			r.logger.Info().Msg("[Rod] Fase 4: Aguardando página carregar (5s)...")
			time.Sleep(5 * time.Second)

			r.logger.Info().Msg("[Rod] Navegando para home do ServiceNow...")
			page.Navigate("https://viavarejo.service-now.com/now/nav/ui/home")
			time.Sleep(5 * time.Second)

			r.logger.Info().Msg("[Rod] Aguardando cookies serem salvos (5s)...")
			time.Sleep(5 * time.Second)

			// Fechar página e desconectar (não fecha o Chrome do Windows no modo WSL)
			r.logger.Info().Msg("[Rod] Encerrando sessão de login...")
			page.Close()
			closeFunc()

			// Verificar arquivos salvos
			activeDir := r.activeSessionDir()
			entries, _ := os.ReadDir(activeDir)
			r.logger.Info().
				Int("files_count", len(entries)).
				Str("session_dir", activeDir).
				Msg("[Rod] Sessão salva!")

			return &SessionStatus{
				Valid:      true,
				Exists:     true,
				Status:     "valid",
				SessionDir: activeDir,
				Message:    "Login realizado com sucesso! Agora você pode extrair CHGs.",
			}, nil
		}

		// Log a cada 10 segundos
		if int(elapsed.Seconds())%10 == 0 {
			r.logger.Info().
				Str("url", currentURL).
				Dur("elapsed", elapsed).
				Msg("[Rod] Ainda aguardando login...")
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

	r.logger.Info().Str("url", chgURL).Msg("[Rod] ========== INICIANDO EXTRAÇÃO ==========")

	// Validar URL
	if !strings.Contains(chgURL, "service-now.com") {
		r.logger.Error().Str("url", chgURL).Msg("[Rod] URL inválida - não contém service-now.com")
		return nil, fmt.Errorf("URL inválida: deve ser uma URL do ServiceNow")
	}

	sysIDRegex := regexp.MustCompile(`sys_id=([a-f0-9]{32})`)
	if !sysIDRegex.MatchString(chgURL) {
		r.logger.Error().Str("url", chgURL).Msg("[Rod] URL inválida - sys_id não encontrado")
		return nil, fmt.Errorf("URL inválida: sys_id não encontrado")
	}

	r.logger.Info().Msg("[Rod] URL validada com sucesso")

	// Verificar sessão antes de qualquer tentativa de browser.
	// Se não há sessão válida, não tenta autenticar — retorna erro claro.
	sessionStatus := r.GetSessionStatus()
	if !sessionStatus.Valid {
		r.logger.Warn().
			Str("session_status", sessionStatus.Status).
			Str("session_dir", sessionStatus.SessionDir).
			Msg("[Rod] Sessão inválida ou ausente — extração bloqueada")
		return &PlaywrightResult{
			Success: false,
			Error:   "Sessão não autenticada. Acesse Menu de Perfil → ServiceNow Session e faça login antes de extrair dados.",
		}, nil
	}

	headless := true // sessão válida confirmada — Chrome pode rodar silenciosamente

	r.logger.Info().
		Str("url", chgURL).
		Bool("headless", headless).
		Bool("session_valid", sessionStatus.Valid).
		Str("session_status", sessionStatus.Status).
		Str("session_dir", r.sessionDir).
		Msg("[Rod] Configuração de sessão verificada")

	// Iniciar browser (local ou Windows via CDP conforme ambiente e configuração)
	r.logger.Info().
		Bool("headless", headless).
		Bool("wsl_mode", NeedsWindowsBrowser()).
		Str("session_dir", r.activeSessionDir()).
		Msg("[Rod] Iniciando browser para extração...")

	// No modo Windows: Chrome já abre na URL da CHG (sem aba em branco)
	// No modo local: Chromium abre em branco e browser.Page() navega para a URL
	browser, closeFunc, err := r.launchBrowser(headless, chgURL)
	if err != nil {
		r.logger.Error().Err(err).Msg("[Rod] ERRO ao iniciar browser")
		return nil, err
	}
	defer closeFunc()
	r.logger.Info().Msg("[Rod] Conectado ao browser com sucesso")

	// Navegar para a CHG
	r.logger.Info().Str("chg_url", chgURL).Msg("[Rod] Criando nova página e navegando para a CHG...")
	page, err := browser.Page(proto.TargetCreateTarget{URL: chgURL})
	if err != nil {
		r.logger.Error().
			Err(err).
			Str("chg_url", chgURL).
			Msg("[Rod] ERRO ao criar página - pode ser problema de conectividade ou timeout")
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	defer page.Close() //nolint:errcheck
	// Trazer a aba para frente (necessário no modo Windows: Chrome abre com aba em branco, Rod cria segunda aba)
	if _, activateErr := page.Activate(); activateErr != nil {
		r.logger.Warn().Err(activateErr).Msg("[Rod] Aviso: não foi possível ativar aba da CHG")
	}
	r.logger.Info().Msg("[Rod] Página criada com sucesso, aguardando carregamento...")

	// Timeout de 5 minutos para login se necessário
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
			r.logger.Error().
				Dur("elapsed", elapsed).
				Int("loop_count", loopCount).
				Msg("[Rod] TIMEOUT aguardando login no Azure AD")
			return &PlaywrightResult{
				Success: false,
				Error:   "Timeout aguardando login no Azure AD (5 minutos)",
			}, nil
		}

		// Obter URL atual com tratamento de erro
		pageInfo, err := page.Info()
		if err != nil {
			r.logger.Warn().
				Err(err).
				Int("loop_count", loopCount).
				Msg("[Rod] Erro ao obter info da página (tentando novamente...)")
			time.Sleep(2 * time.Second)
			continue
		}
		currentURL := pageInfo.URL

		// Log a cada 5 iterações ou quando mudar URL
		if loopCount%5 == 1 {
			r.logger.Debug().
				Str("current_url", currentURL).
				Int("loop_count", loopCount).
				Dur("elapsed", elapsed).
				Msg("[Rod] Verificando estado da página...")
		}

		isLoginPage := false
		for _, pattern := range loginPatterns {
			if strings.Contains(strings.ToLower(currentURL), strings.ToLower(pattern)) {
				isLoginPage = true
				r.logger.Debug().
					Str("pattern", pattern).
					Str("url", currentURL).
					Msg("[Rod] Detectada página de login")
				break
			}
		}

		if isLoginPage && headless {
			// Se estamos em headless e precisa de login, avisar o usuário
			r.logger.Warn().
				Bool("headless", headless).
				Str("url", currentURL).
				Msg("[Rod] Sessão expirada - login necessário mas estamos em modo headless")
			return &PlaywrightResult{
				Success: false,
				Error:   "Sessão expirada. Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair dados.",
			}, nil
		}

		if strings.Contains(currentURL, "service-now.com") && !isLoginPage && !strings.Contains(currentURL, "login") {
			r.logger.Info().
				Str("url", currentURL).
				Dur("elapsed", elapsed).
				Int("loops", loopCount).
				Msg("[Rod] Acesso ao ServiceNow confirmado!")
			break
		}

		time.Sleep(2 * time.Second)
	}

	// Aguardar carregamento completo - ServiceNow é lento para carregar formulários
	r.logger.Info().Msg("[Rod] Aguardando carregamento completo da página (5s)...")
	time.Sleep(5 * time.Second)
	if err := page.WaitLoad(); err != nil {
		r.logger.Warn().Err(err).Msg("[Rod] Aviso: erro ao aguardar WaitLoad (continuando mesmo assim)")
	}

	// Aguardar elementos dinâmicos carregarem
	r.logger.Info().Msg("[Rod] Aguardando elementos dinâmicos (3s adicionais)...")
	time.Sleep(3 * time.Second)

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

	// Aguardar mais um pouco para formulário carregar
	r.logger.Info().Msg("[Rod] Aguardando formulário carregar (3s)...")
	time.Sleep(3 * time.Second)

	r.logger.Info().Msg("[Rod] ========== EXECUTANDO JAVASCRIPT PARA EXTRAIR DADOS ==========")

	// Primeiro, vamos capturar informações sobre a página para debug
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

		// VERIFICAÇÃO CRÍTICA: Se ainda estamos na página de login, a sessão expirou!
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
			r.logger.Warn().
				Str("url", currentURL).
				Str("title", currentTitle).
				Bool("was_headless", headless).
				Msg("[Rod] Sessão expirada - página de login detectada")

			// Fechar browser/desconectar antes de reabrir para login
			page.Close()
			closeFunc()

			// Limpar sessão inválida
			activeDir := r.activeSessionDir()
			r.logger.Info().Str("session_dir", activeDir).Msg("[Rod] Limpando sessão inválida e reabrindo browser para login...")
			os.RemoveAll(activeDir)
			os.MkdirAll(activeDir, 0755)

			// REABRIR BROWSER EM MODO VISÍVEL PARA LOGIN
			r.logger.Info().Msg("[Rod] ============================================")
			r.logger.Info().Msg("[Rod] ABRINDO BROWSER PARA LOGIN NO AZURE AD...")
			r.logger.Info().Msg("[Rod] Faça login e aguarde a extração continuar")
			r.logger.Info().Msg("[Rod] ============================================")

			return r.extractWithVisibleLogin(ctx, chgURL)
		}

		// Verificar se realmente estamos no ServiceNow
		if !strings.Contains(currentURL, "service-now.com") {
			r.logger.Error().
				Str("url", currentURL).
				Msg("[Rod] ERRO: Não estamos no ServiceNow - possível redirect inesperado")
			return &PlaywrightResult{
				Success: false,
				Error:   "Página inesperada. Não foi possível acessar o ServiceNow. Verifique sua conexão e tente novamente.",
			}, nil
		}
	}

	// Extrair dados usando JavaScript com múltiplas estratégias
	jsResult, err := targetPage.Eval(`() => {
		// Helper para buscar valor em múltiplos seletores
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

		// Helper para buscar em spans/divs de display (UI Next)
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

		// Estratégia 1: Número da CHG pelo título da página
		let changeNumber = '';
		const titleMatch = document.title.match(/(CHG[0-9]+)/i);
		if (titleMatch) {
			changeNumber = titleMatch[1];
		}

		// Estratégia 2: Buscar em elementos comuns
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

		// Estratégia 3: Buscar CHG em qualquer lugar visível
		if (!changeNumber) {
			const allText = document.body.innerText || '';
			const chgMatch = allText.match(/CHG[0-9]{7,}/i);
			if (chgMatch) {
				changeNumber = chgMatch[0];
			}
		}

		// Short description - múltiplas estratégias
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

		// Motivo da mudança / Justification - múltiplas estratégias
		let description = '';
		const descSelectors = [
			'#sys_readonly\\.change_request\\.justification',
			'[name="change_request.justification"]',
			'[aria-label="Motivo da mudança"]',
			'[aria-label="Justification"]',
			'[data-field-name="justification"]',
			'textarea[id*="justification"]',
			'#sys_readonly\\.change_request\\.description',
			'[name="change_request.description"]',
			'textarea[id*="description"]',
			'[data-field-name="description"]',
			'[data-field-name="u_motivo_mudanca"]',
		];
		for (const sel of descSelectors) {
			try {
				const el = document.querySelector(sel);
				if (el && (el.value || el.textContent)) {
					const val = (el.value || el.textContent).trim();
					if (val.length > 50) {
						description = val;
						break;
					}
				}
			} catch(e) {}
		}

		// Última tentativa: buscar qualquer textarea grande com conteúdo relevante
		if (!description) {
			const textareas = document.querySelectorAll('textarea');
			for (const ta of textareas) {
				const val = ta.value || ta.textContent || '';
				if (val.length > 100 && (
					val.includes('Aplicação') ||
					val.includes('Versão') ||
					val.includes('Squad') ||
					val.includes('github') ||
					val.includes('Repositório')
				)) {
					description = val;
					break;
				}
			}
		}

		// Se ainda não encontrou, buscar em divs com classe específica
		if (!description) {
			const contentDivs = document.querySelectorAll('.sn-widget-textblock-body, .activity-stream-message, .sn-card-component_content');
			for (const div of contentDivs) {
				const text = div.textContent || '';
				if (text.length > 100 && (text.includes('Aplicação') || text.includes('Squad'))) {
					description = text.trim();
					break;
				}
			}
		}

		// State
		let state = '';
		const stateEl = document.querySelector('[name="state"], [data-field-name="state"]');
		if (stateEl) {
			if (stateEl.options && stateEl.selectedIndex >= 0) {
				state = stateEl.options[stateEl.selectedIndex].text;
			} else {
				state = stateEl.value || stateEl.textContent || '';
			}
		}

		// Debug: listar todos os inputs/textareas encontrados
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
		r.logger.Error().
			Err(err).
			Msg("[Rod] ERRO ao executar JavaScript de extração")
		return nil, fmt.Errorf("erro ao extrair dados: %v", err)
	}

	r.logger.Info().Msg("[Rod] JavaScript executado com sucesso, processando resultado...")

	// Parse do resultado
	if jsResult == nil || jsResult.Value.Nil() {
		r.logger.Error().Msg("[Rod] Resultado do JavaScript é nil ou vazio")
		return nil, fmt.Errorf("resultado da extração está vazio")
	}

	data := jsResult.Value.Map()
	r.logger.Debug().
		Int("fields_count", len(data)).
		Msg("[Rod] Mapa de dados obtido do JavaScript")

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

	// Log dos inputs encontrados para debug
	if v, ok := data["debugInputs"]; ok {
		r.logger.Debug().Str("inputs", v.String()).Msg("[Rod] Debug: inputs/textareas encontrados na página")
	}

	// Parse do description para extrair dados estruturados
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

	// Aplicação
	appRegex := regexp.MustCompile(`\* Aplicação\(ões\):\s*([^.\n]+)\.`)
	if match := appRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Application = strings.TrimSpace(match[1])
	}

	// Versão
	versionRegex := regexp.MustCompile(`\* Versão:\s*([\d]+\.[\d]+\.[\d]+-?[\d]*)\.`)
	if match := versionRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Version = strings.TrimSpace(match[1])
	}

	// Repositório (GitHubRepo)
	repoRegex := regexp.MustCompile(`\* Repositório:\s*github\.com/[^/]+/([^.]+)\.git`)
	if match := repoRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.GitHubRepo = strings.TrimSpace(match[1])
	}

	// Squad
	squadRegex := regexp.MustCompile(`\* Squad\(s\):\s*([^.\n]+)\.`)
	if match := squadRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Squad = strings.TrimSpace(match[1])
	}

	// Branch
	branchRegex := regexp.MustCompile(`\* Branch no GitHub:\s*([^\n]+)\.`)
	if match := branchRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Branch = strings.TrimSpace(match[1])
	}

	// Produto
	productRegex := regexp.MustCompile(`\* Produto:\s*([^.\n]+)\.`)
	if match := productRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.Product = strings.TrimSpace(match[1])
	}

	// XL Release URL (XLReleaseURL)
	xlURLRegex := regexp.MustCompile(`\* Link da release no XL-Release:\s*(https?://[^\s\n]+)`)
	if match := xlURLRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.XLReleaseURL = strings.TrimSpace(match[1])
	}

	// XL Release Title (XLReleaseTitle - sem 'e' extra)
	xlTitleRegex := regexp.MustCompile(`\* Titulo da release no XL-Release:\s*([^\n]+)\.`)
	if match := xlTitleRegex.FindStringSubmatch(description); len(match) > 1 {
		extracted.XLReleaseTitle = strings.TrimSpace(match[1])
	}

	// Jira Issues
	jiraRegex := regexp.MustCompile(`([A-Z]+-\d+)`)
	if matches := jiraRegex.FindAllString(description, -1); len(matches) > 0 {
		// Remover duplicatas
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

	// Iniciar browser visível abrindo diretamente na URL da CHG
	browser, closeFunc, err := r.launchBrowser(false, chgURL)
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar browser para login: %v", err)
	}
	defer closeFunc()

	// Navegar para a CHG
	page, err := browser.Page(proto.TargetCreateTarget{URL: chgURL})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}
	defer page.Close() //nolint:errcheck
	// Trazer a aba para frente (necessário no modo Windows)
	if _, activateErr := page.Activate(); activateErr != nil {
		r.logger.Warn().Err(activateErr).Msg("[Rod] Aviso: não foi possível ativar aba")
	}

	r.logger.Info().Msg("[Rod] Browser aberto - faça login no Azure AD...")

	// Aguardar login (máximo 3 minutos)
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

		// Verificar se saiu da página de login
		isOnLogin := false
		for _, pattern := range loginPatterns {
			if strings.Contains(currentURL, pattern) {
				isOnLogin = true
				break
			}
		}

		// Se está no ServiceNow e não na página de login, sucesso!
		if strings.Contains(currentURL, "service-now.com") && !isOnLogin && !strings.Contains(currentURL, "saml") {
			r.logger.Info().
				Str("url", currentURL).
				Msg("[Rod] Login completado! Aguardando página carregar...")

			// Aguardar página carregar
			time.Sleep(5 * time.Second)
			page.WaitLoad()
			time.Sleep(3 * time.Second)

			break
		}

		time.Sleep(2 * time.Second)
	}

	r.logger.Info().Msg("[Rod] Extraindo dados da CHG...")

	// Agora extrair os dados
	var targetPage *rod.Page

	// Buscar iframe gsft_main
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

	time.Sleep(2 * time.Second)

	// Executar JavaScript de extração (mesmo código do Extract)
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
			const allText = document.body.innerText || '';
			const chgMatch = allText.match(/CHG[0-9]{7,}/i);
			if (chgMatch) changeNumber = chgMatch[0];
		}

		let shortDescription = getFieldValue('short_description') ||
			getFieldValue('sys_readonly.change_request.short_description') || '';

		let description = getFieldValue('justification') ||
			getFieldValue('sys_readonly.change_request.justification') ||
			getFieldValue('description') || '';

		if (!description) {
			const textareas = document.querySelectorAll('textarea');
			for (const ta of textareas) {
				const val = ta.value || '';
				if (val.length > 100 && (val.includes('Aplicação') || val.includes('Squad'))) {
					description = val;
					break;
				}
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

	// Parse resultado
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
