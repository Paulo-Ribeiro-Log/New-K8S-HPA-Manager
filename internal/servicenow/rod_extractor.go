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
	logger     *zerolog.Logger
	sessionDir string
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
		// Campos para compatibilidade com frontend (espera playwright_configured e script_exists)
		"playwright_configured": true, // Rod está sempre configurado (Go nativo)
		"script_exists":         true, // Não precisa de script externo
		"configured":            true,
		"session_dir":           r.sessionDir,
		"type":                  "rod-go-native",
		"dependencies":          "none (Go native)",
		"npx_available":         true, // Não precisa de npx
		"ts_node_available":     true, // Não precisa de ts-node
		"npm_installed":         true, // Não precisa de npm
	}

	// Verificar se sessão existe
	if _, err := os.Stat(r.sessionDir); err == nil {
		status["session_exists"] = true
	} else {
		status["session_exists"] = false
	}

	return status
}

// GetSessionStatus retorna o status da sessão
func (r *RodExtractor) GetSessionStatus() *SessionStatus {
	status := &SessionStatus{
		SessionDir: r.sessionDir,
	}

	// Verificar se o diretório de sessão existe
	info, err := os.Stat(r.sessionDir)
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
	entries, _ := os.ReadDir(r.sessionDir)
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
	if _, err := os.Stat(r.sessionDir); os.IsNotExist(err) {
		r.logger.Info().Str("dir", r.sessionDir).Msg("[Rod] Sessão já não existe")
		return nil
	}

	err := os.RemoveAll(r.sessionDir)
	if err != nil {
		r.logger.Error().Err(err).Str("dir", r.sessionDir).Msg("[Rod] Erro ao limpar sessão")
		return fmt.Errorf("erro ao limpar sessão: %v", err)
	}

	// Recriar diretório vazio
	os.MkdirAll(r.sessionDir, 0755)

	r.logger.Info().Str("dir", r.sessionDir).Msg("[Rod] Sessão limpa com sucesso")
	return nil
}

// TestSession abre o browser para o usuário fazer login
func (r *RodExtractor) TestSession(ctx context.Context) (*SessionStatus, error) {
	r.logger.Info().
		Str("session_dir", r.sessionDir).
		Msg("[Rod] Iniciando login - abrindo browser visível")

	// Limpar sessão anterior para garantir login fresco
	r.logger.Info().Msg("[Rod] Limpando sessão anterior para garantir login fresco...")
	os.RemoveAll(r.sessionDir)
	os.MkdirAll(r.sessionDir, 0755)

	// Criar launcher com diretório de sessão persistente
	l := launcher.New().
		UserDataDir(r.sessionDir).
		Headless(false). // Sempre visível para login
		Set("disable-blink-features", "AutomationControlled")

	url, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar ao browser: %v", err)
	}

	// Navegar para ServiceNow
	page, err := browser.Page(proto.TargetCreateTarget{URL: "https://viavarejo.service-now.com"})
	if err != nil {
		browser.Close()
		return nil, fmt.Errorf("erro ao criar página: %v", err)
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
				browser.Close()
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
			browser.Close()
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

			// Fechar browser
			r.logger.Info().Msg("[Rod] Fechando browser...")
			browser.Close()

			// Verificar arquivos salvos
			entries, _ := os.ReadDir(r.sessionDir)
			r.logger.Info().
				Int("files_count", len(entries)).
				Str("session_dir", r.sessionDir).
				Msg("[Rod] Sessão salva!")

			return &SessionStatus{
				Valid:      true,
				Exists:     true,
				Status:     "valid",
				SessionDir: r.sessionDir,
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

	// Verificar se tem sessão válida
	sessionStatus := r.GetSessionStatus()
	headless := sessionStatus.Valid // Se tem sessão válida, pode rodar headless

	r.logger.Info().
		Str("url", chgURL).
		Bool("headless", headless).
		Bool("session_valid", sessionStatus.Valid).
		Str("session_status", sessionStatus.Status).
		Str("session_dir", r.sessionDir).
		Msg("[Rod] Configuração de sessão verificada")

	// Criar launcher
	r.logger.Info().
		Bool("headless", headless).
		Str("user_data_dir", r.sessionDir).
		Msg("[Rod] Criando launcher do browser...")

	l := launcher.New().
		UserDataDir(r.sessionDir).
		Headless(headless).
		Set("disable-blink-features", "AutomationControlled")

	r.logger.Info().Msg("[Rod] Launcher criado, iniciando browser...")
	r.logger.Info().Msg("[Rod] (Pode baixar Chromium automaticamente na primeira execução - aguarde...)")

	url, err := l.Launch()
	if err != nil {
		r.logger.Error().
			Err(err).
			Bool("headless", headless).
			Str("session_dir", r.sessionDir).
			Msg("[Rod] ERRO ao iniciar browser - verifique se há espaço em disco e permissões")
		return nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}
	r.logger.Info().Str("control_url", url).Msg("[Rod] Browser iniciado com sucesso")

	r.logger.Info().Str("control_url", url).Msg("[Rod] Conectando ao browser via DevTools Protocol...")
	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		r.logger.Error().
			Err(err).
			Str("control_url", url).
			Msg("[Rod] ERRO ao conectar ao browser - verifique se o Chromium está instalado corretamente")
		return nil, fmt.Errorf("erro ao conectar ao browser: %v", err)
	}
	defer func() {
		r.logger.Info().Msg("[Rod] Fechando browser...")
		browser.Close()
		r.logger.Info().Msg("[Rod] Browser fechado")
	}()
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

			// Fechar browser headless
			browser.Close()

			// Limpar sessão inválida
			r.logger.Info().Msg("[Rod] Limpando sessão inválida e reabrindo browser para login...")
			os.RemoveAll(r.sessionDir)
			os.MkdirAll(r.sessionDir, 0755)

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
	repoRegex := regexp.MustCompile(`\* Repositório:\s*github\.com/viavarejo-internal/([^.]+)\.git`)
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

	// Criar launcher em modo VISÍVEL
	l := launcher.New().
		UserDataDir(r.sessionDir).
		Headless(false). // VISÍVEL para login
		Set("disable-blink-features", "AutomationControlled")

	url, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("erro ao iniciar browser: %v", err)
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar ao browser: %v", err)
	}
	defer browser.Close()

	// Navegar para a CHG
	page, err := browser.Page(proto.TargetCreateTarget{URL: chgURL})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %v", err)
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
