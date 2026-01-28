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
	r.logger.Info().Msg("[Rod] Iniciando login - abrindo browser visível")

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
	defer browser.Close()

	// Navegar para ServiceNow
	page, err := browser.Page(proto.TargetCreateTarget{URL: "https://viavarejo.service-now.com"})
	if err != nil {
		return nil, fmt.Errorf("erro ao criar página: %v", err)
	}

	r.logger.Info().Msg("[Rod] Navegando para ServiceNow - aguardando login do usuário...")

	// Aguardar login do usuário (máximo 3 minutos)
	loginTimeout := 3 * time.Minute
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

	for {
		if time.Since(startTime) > loginTimeout {
			r.logger.Warn().Msg("[Rod] Timeout aguardando login")
			return r.GetSessionStatus(), nil
		}

		// Obter URL atual com tratamento de erro
		pageInfo, err := page.Info()
		if err != nil {
			r.logger.Warn().Err(err).Msg("[Rod] Erro ao obter info da página durante login")
			time.Sleep(2 * time.Second)
			continue
		}
		currentURL := pageInfo.URL

		// Verificar se está no ServiceNow (não em página de login)
		isLoginPage := false
		for _, pattern := range loginPatterns {
			if strings.Contains(strings.ToLower(currentURL), strings.ToLower(pattern)) {
				isLoginPage = true
				break
			}
		}

		if strings.Contains(currentURL, "service-now.com") && !isLoginPage {
			r.logger.Info().Msg("[Rod] Login realizado com sucesso!")
			time.Sleep(2 * time.Second) // Dar tempo para salvar cookies
			return r.GetSessionStatus(), nil
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

	// Aguardar carregamento completo
	r.logger.Info().Msg("[Rod] Aguardando carregamento completo da página (3s)...")
	time.Sleep(3 * time.Second)
	if err := page.WaitLoad(); err != nil {
		r.logger.Warn().Err(err).Msg("[Rod] Aviso: erro ao aguardar WaitLoad (continuando mesmo assim)")
	}
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
	r.logger.Info().Msg("[Rod] Aguardando formulário carregar (2s)...")
	time.Sleep(2 * time.Second)

	r.logger.Info().Msg("[Rod] ========== EXECUTANDO JAVASCRIPT PARA EXTRAIR DADOS ==========")

	// Extrair dados usando JavaScript
	jsResult, err := targetPage.Eval(`() => {
		function getFieldValue(fieldName) {
			const selectors = [
				'input[name="' + fieldName + '"]',
				'textarea[name="' + fieldName + '"]',
				'#' + fieldName,
				'[id$="' + fieldName + '"]',
				'[data-field="' + fieldName + '"]'
			];

			for (const selector of selectors) {
				const el = document.querySelector(selector);
				if (el && el.value) {
					return el.value;
				}
			}
			return '';
		}

		// Número da CHG
		let changeNumber = '';
		let numberEl = document.querySelector('#sys_readonly\\.change_request\\.number') ||
					  document.querySelector('[name="change_request.number"]') ||
					  document.querySelector('[aria-label="Número"]');
		if (numberEl) {
			changeNumber = numberEl.value || numberEl.textContent || '';
		}
		if (!changeNumber) {
			changeNumber = getFieldValue('number') ||
						  getFieldValue('sys_readonly.change_request.number') || '';
		}
		if (!changeNumber) {
			const match = document.title.match(/(CHG[0-9]+)/i);
			if (match) changeNumber = match[1];
		}

		// Short description
		let shortDescription = '';
		let shortDescEl = document.querySelector('#sys_readonly\\.change_request\\.short_description') ||
						 document.querySelector('[name="change_request.short_description"]') ||
						 document.querySelector('[aria-label="Descrição resumida"]');
		if (shortDescEl) {
			shortDescription = shortDescEl.value || shortDescEl.textContent || '';
		}
		if (!shortDescription) {
			shortDescription = getFieldValue('short_description') ||
							  getFieldValue('sys_readonly.change_request.short_description') || '';
		}

		// Motivo da mudança / Justification
		let description = '';
		let justificationEl = document.querySelector('#sys_readonly\\.change_request\\.justification') ||
							 document.querySelector('[name="change_request.justification"]') ||
							 document.querySelector('[aria-label="Motivo da mudança"]');
		if (justificationEl) {
			description = justificationEl.value || justificationEl.textContent || '';
		}
		if (!description) {
			description = getFieldValue('description') ||
						 getFieldValue('sys_readonly.change_request.description') ||
						 getFieldValue('u_motivo_mudanca') || '';
		}

		// Se ainda não achou, procurar textareas com conteúdo relevante
		if (!description) {
			const textareas = document.querySelectorAll('textarea');
			for (const ta of textareas) {
				const val = ta.value || '';
				if (val.length > 100 && (val.includes('Aplicação') || val.includes('Versão') || val.includes('Squad'))) {
					description = val;
					break;
				}
			}
		}

		const stateEl = document.querySelector('[name="state"]');
		const state = stateEl && stateEl.options ? (stateEl.options[stateEl.selectedIndex] ? stateEl.options[stateEl.selectedIndex].text : '') : '';

		return {
			changeNumber: changeNumber,
			shortDescription: shortDescription,
			description: description,
			state: state
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
