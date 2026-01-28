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

		currentURL := page.MustInfo().URL

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
func (r *RodExtractor) Extract(ctx context.Context, chgURL string) (*PlaywrightResult, error) {
	// Validar URL
	if !strings.Contains(chgURL, "service-now.com") {
		return nil, fmt.Errorf("URL inválida: deve ser uma URL do ServiceNow")
	}

	sysIDRegex := regexp.MustCompile(`sys_id=([a-f0-9]{32})`)
	if !sysIDRegex.MatchString(chgURL) {
		return nil, fmt.Errorf("URL inválida: sys_id não encontrado")
	}

	// Verificar se tem sessão válida
	sessionStatus := r.GetSessionStatus()
	headless := sessionStatus.Valid // Se tem sessão válida, pode rodar headless

	r.logger.Info().
		Str("url", chgURL).
		Bool("headless", headless).
		Bool("session_valid", sessionStatus.Valid).
		Msg("[Rod] Iniciando extração de CHG")

	// Criar launcher
	l := launcher.New().
		UserDataDir(r.sessionDir).
		Headless(headless).
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
	for {
		if time.Since(startTime) > loginTimeout {
			return &PlaywrightResult{
				Success: false,
				Error:   "Timeout aguardando login no Azure AD (5 minutos)",
			}, nil
		}

		currentURL := page.MustInfo().URL

		isLoginPage := false
		for _, pattern := range loginPatterns {
			if strings.Contains(strings.ToLower(currentURL), strings.ToLower(pattern)) {
				isLoginPage = true
				break
			}
		}

		if isLoginPage && headless {
			// Se estamos em headless e precisa de login, avisar o usuário
			return &PlaywrightResult{
				Success: false,
				Error:   "Sessão expirada. Faça login pelo Menu de Perfil > ServiceNow Session antes de extrair dados.",
			}, nil
		}

		if strings.Contains(currentURL, "service-now.com") && !isLoginPage && !strings.Contains(currentURL, "login") {
			r.logger.Info().Msg("[Rod] Acesso ao ServiceNow confirmado!")
			break
		}

		time.Sleep(2 * time.Second)
	}

	// Aguardar carregamento completo
	time.Sleep(3 * time.Second)
	page.MustWaitLoad()

	// Tentar encontrar o iframe gsft_main
	var targetPage *rod.Page
	frames := page.MustElements("iframe")
	for _, frame := range frames {
		name, _ := frame.Attribute("name")
		if name != nil && *name == "gsft_main" {
			framePage, err := frame.Frame()
			if err == nil {
				targetPage = framePage
				r.logger.Info().Msg("[Rod] Usando iframe gsft_main para extração")
				break
			}
		}
	}

	if targetPage == nil {
		targetPage = page
		r.logger.Info().Msg("[Rod] Usando página principal para extração")
	}

	// Aguardar mais um pouco para formulário carregar
	time.Sleep(2 * time.Second)

	// Extrair dados usando JavaScript
	result, err := targetPage.Eval(`() => {
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
		return nil, fmt.Errorf("erro ao extrair dados: %v", err)
	}

	// Parse do resultado
	data := result.Value.Map()

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

	state := ""
	if v, ok := data["state"]; ok {
		state = v.String()
	}

	// Parse do description para extrair dados estruturados
	extracted := r.parseDescription(description)

	r.logger.Info().
		Str("changeNumber", changeNumber).
		Str("application", extracted.Application).
		Str("version", extracted.Version).
		Msg("[Rod] Extração concluída com sucesso")

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
