package servicenow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// PlaywrightExtractor executa extração de CHG via Playwright (browser automation)
type PlaywrightExtractor struct {
	frontendDir string
	logger      *zerolog.Logger
}

// PlaywrightResult representa o resultado da extração via Playwright
type PlaywrightResult struct {
	Success          bool            `json:"success"`
	ChangeNumber     string          `json:"changeNumber,omitempty"`
	ShortDescription string          `json:"shortDescription,omitempty"`
	Description      string          `json:"description,omitempty"`
	State            string          `json:"state,omitempty"`
	Extracted        *ExtractedData  `json:"extracted,omitempty"`
	Error            string          `json:"error,omitempty"`
}

// NewPlaywrightExtractor cria um novo extrator Playwright
func NewPlaywrightExtractor(logger *zerolog.Logger) *PlaywrightExtractor {
	// Usar script embarcado no diretório do usuário (~/.k8s-hpa-manager/playwright/)
	playwrightDir := GetPlaywrightDir()

	// Garantir que o script exista
	scriptPath, err := EnsurePlaywrightScript()
	if err != nil {
		logger.Error().
			Err(err).
			Msg("[Playwright] Erro ao configurar script embarcado")
		return &PlaywrightExtractor{
			frontendDir: "",
			logger:      logger,
		}
	}

	logger.Info().
		Str("path", playwrightDir).
		Str("script", scriptPath).
		Msg("[Playwright] Script configurado com sucesso")

	return &PlaywrightExtractor{
		frontendDir: playwrightDir,
		logger:      logger,
	}
}

// IsConfigured verifica se o Playwright está configurado
func (p *PlaywrightExtractor) IsConfigured() bool {
	return p.frontendDir != "" && IsPlaywrightInstalled()
}

// GetStatus retorna o status da configuração do Playwright
func (p *PlaywrightExtractor) GetStatus() map[string]interface{} {
	cwd, _ := os.Getwd()
	execPath, _ := os.Executable()

	status := map[string]interface{}{
		"playwright_configured": false,
		"frontend_dir":          p.frontendDir,
		"script_exists":         false,
		"npx_available":         false,
		"ts_node_available":     false,
		"npm_installed":         false,
		"cwd":                   cwd,
		"exec_path":             execPath,
	}

	if p.frontendDir != "" {
		scriptPath := filepath.Join(p.frontendDir, "servicenow-extractor.ts")
		if _, err := os.Stat(scriptPath); err == nil {
			status["script_exists"] = true
		}

		// Verificar se npm dependencies estão instaladas
		if IsPlaywrightInstalled() {
			status["npm_installed"] = true
			status["playwright_configured"] = true
		}
	}

	// Verificar npx
	if _, err := exec.LookPath("npx"); err == nil {
		status["npx_available"] = true
	}

	// Verificar tsx
	if p.frontendDir != "" {
		cmd := exec.Command("npx", "tsx", "--version")
		cmd.Dir = p.frontendDir
		if err := cmd.Run(); err == nil {
			status["ts_node_available"] = true
		}
	}

	return status
}

// installDependencies instala as dependências npm necessárias
func (p *PlaywrightExtractor) installDependencies() error {
	if IsPlaywrightInstalled() {
		return nil
	}

	p.logger.Info().Msg("[Playwright] Instalando dependências npm (primeira execução)...")

	// Executar npm install
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", "install")
	cmd.Dir = p.frontendDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("erro ao instalar dependências: %v - %s", err, stderr.String())
	}

	// Instalar browsers do Playwright
	p.logger.Info().Msg("[Playwright] Instalando browsers do Playwright...")
	cmd2 := exec.CommandContext(ctx, "npx", "playwright", "install", "chromium")
	cmd2.Dir = p.frontendDir
	cmd2.Stderr = &stderr

	if err := cmd2.Run(); err != nil {
		p.logger.Warn().
			Err(err).
			Str("stderr", stderr.String()).
			Msg("[Playwright] Aviso: erro ao instalar browsers (pode já estar instalado)")
	}

	p.logger.Info().Msg("[Playwright] Dependências instaladas com sucesso")
	return nil
}

// Extract executa o script Playwright para extrair dados da CHG
func (p *PlaywrightExtractor) Extract(ctx context.Context, chgURL string) (*PlaywrightResult, error) {
	if p.frontendDir == "" {
		return nil, fmt.Errorf("Playwright não está configurado - diretório não encontrado")
	}

	// Instalar dependências se necessário
	if err := p.installDependencies(); err != nil {
		return nil, fmt.Errorf("erro ao configurar Playwright: %v", err)
	}

	// Validar URL
	if !strings.Contains(chgURL, "service-now.com") {
		return nil, fmt.Errorf("URL inválida: deve ser uma URL do ServiceNow")
	}

	sysIDRegex := regexp.MustCompile(`sys_id=([a-f0-9]{32})`)
	if !sysIDRegex.MatchString(chgURL) {
		return nil, fmt.Errorf("URL inválida: sys_id não encontrado")
	}

	scriptPath := filepath.Join(p.frontendDir, "servicenow-extractor.ts")

	p.logger.Info().
		Str("url", chgURL).
		Str("script", scriptPath).
		Msg("[Playwright] Iniciando extração de CHG - aguardando até 5 minutos para login Azure AD")

	// Criar contexto com timeout de 5 minutos (independente do request HTTP)
	// Isso permite tempo suficiente para o usuário fazer login no Azure AD
	execCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Preparar comando (tsx é mais simples que ts-node para ESM)
	cmd := exec.CommandContext(execCtx, "npx", "tsx", scriptPath, chgURL)
	cmd.Dir = p.frontendDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Executar e aguardar
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			p.logger.Error().
				Err(err).
				Str("stderr", stderr.String()).
				Msg("[Playwright] Erro na execução")
			return nil, fmt.Errorf("erro na execução: %v - %s", err, stderr.String())
		}
	case <-execCtx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return nil, fmt.Errorf("timeout: extração demorou mais de 5 minutos - faça login no Azure AD mais rapidamente")
	}

	// Parse do resultado JSON
	output := stdout.String()

	// Encontrar o JSON no output (pode ter logs antes)
	jsonStart := strings.Index(output, "=== RESULTADO ===")
	if jsonStart != -1 {
		output = output[jsonStart+len("=== RESULTADO ==="):]
	}
	output = strings.TrimSpace(output)

	var result PlaywrightResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// Tentar extrair JSON de qualquer lugar do output
		jsonRegex := regexp.MustCompile(`\{[\s\S]*"success"[\s\S]*\}`)
		match := jsonRegex.FindString(stdout.String())
		if match != "" {
			if err := json.Unmarshal([]byte(match), &result); err != nil {
				return nil, fmt.Errorf("erro ao parsear resultado: %v", err)
			}
		} else {
			return nil, fmt.Errorf("resultado não é JSON válido: %s", output)
		}
	}

	p.logger.Info().
		Bool("success", result.Success).
		Str("changeNumber", result.ChangeNumber).
		Msg("[Playwright] Extração concluída")

	return &result, nil
}

// ExtractWithSSE executa extração com eventos SSE para progresso
func (p *PlaywrightExtractor) ExtractWithSSE(ctx context.Context, chgURL string, progressChan chan<- string) (*PlaywrightResult, error) {
	if progressChan != nil {
		progressChan <- "Iniciando browser Chromium..."
	}

	result, err := p.Extract(ctx, chgURL)

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

// SessionStatus representa o status da sessão do Playwright
type SessionStatus struct {
	Exists           bool       `json:"exists"`
	Valid            bool       `json:"valid"`
	Status           string     `json:"status"` // "valid", "expired", "not_found"
	SessionDir       string     `json:"session_dir"`
	LastModified     *time.Time `json:"last_modified,omitempty"`
	HoursSinceUpdate float64    `json:"hours_since_update,omitempty"`
	Message          string     `json:"message"`
}

// getSessionDir retorna o diretório de sessão do Playwright
func (p *PlaywrightExtractor) getSessionDir() string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE") // Windows
	}
	return filepath.Join(homeDir, ".k8s-hpa-manager", "playwright-session")
}

// GetSessionStatus retorna o status detalhado da sessão do Playwright
func (p *PlaywrightExtractor) GetSessionStatus() *SessionStatus {
	sessionDir := p.getSessionDir()

	status := &SessionStatus{
		SessionDir: sessionDir,
	}

	// Verificar se o diretório existe
	info, err := os.Stat(sessionDir)
	if os.IsNotExist(err) {
		status.Exists = false
		status.Valid = false
		status.Status = "not_found"
		status.Message = "Sessão não encontrada. Será necessário fazer login no Azure AD na próxima extração."
		p.logger.Info().Str("dir", sessionDir).Msg("[Playwright] Diretório de sessão não existe")
		return status
	}

	if err != nil {
		status.Exists = false
		status.Valid = false
		status.Status = "error"
		status.Message = fmt.Sprintf("Erro ao verificar sessão: %v", err)
		p.logger.Error().Err(err).Msg("[Playwright] Erro ao verificar diretório de sessão")
		return status
	}

	status.Exists = true
	lastMod := info.ModTime()
	status.LastModified = &lastMod

	// Calcular tempo desde última modificação
	hoursSince := time.Since(lastMod).Hours()
	status.HoursSinceUpdate = hoursSince

	// Verificar se existem arquivos de sessão importantes
	cookiesDir := filepath.Join(sessionDir, "Default")
	hasCookies := false
	if _, err := os.Stat(cookiesDir); err == nil {
		// Verificar se há arquivos dentro
		entries, _ := os.ReadDir(cookiesDir)
		hasCookies = len(entries) > 0
	}

	// Sessões do Azure AD geralmente expiram em 8-12 horas
	const maxSessionHours = 8.0

	if !hasCookies {
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

	p.logger.Info().
		Bool("valid", status.Valid).
		Str("status", status.Status).
		Float64("hours", hoursSince).
		Msg("[Playwright] Status da sessão verificado")

	return status
}

// ClearSession remove a sessão do Playwright, forçando novo login
func (p *PlaywrightExtractor) ClearSession() error {
	sessionDir := p.getSessionDir()

	// Verificar se existe
	if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
		p.logger.Info().Str("dir", sessionDir).Msg("[Playwright] Sessão já não existe")
		return nil
	}

	// Remover diretório e todo conteúdo
	err := os.RemoveAll(sessionDir)
	if err != nil {
		p.logger.Error().Err(err).Str("dir", sessionDir).Msg("[Playwright] Erro ao limpar sessão")
		return fmt.Errorf("erro ao limpar sessão: %v", err)
	}

	p.logger.Info().Str("dir", sessionDir).Msg("[Playwright] Sessão limpa com sucesso")
	return nil
}

// TestSession abre o browser para o usuário fazer login preventivamente
// Usa o mesmo script TypeScript com o comando --login
func (p *PlaywrightExtractor) TestSession(ctx context.Context) (*SessionStatus, error) {
	if p.frontendDir == "" {
		return nil, fmt.Errorf("Playwright não está configurado")
	}

	// Instalar dependências se necessário
	if err := p.installDependencies(); err != nil {
		return nil, fmt.Errorf("erro ao configurar Playwright: %v", err)
	}

	scriptPath := filepath.Join(p.frontendDir, "servicenow-extractor.ts")

	p.logger.Info().
		Str("script", scriptPath).
		Msg("[Playwright] Iniciando login - abrindo browser visível")

	// Contexto com timeout de 3 minutos para login
	execCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Executar script TypeScript com comando --login
	cmd := exec.CommandContext(execCtx, "npx", "tsx", scriptPath, "--login")
	cmd.Dir = p.frontendDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Executar e aguardar
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		if err != nil {
			p.logger.Warn().
				Err(err).
				Str("stderr", stderr.String()).
				Msg("[Playwright] Login finalizado (pode ser normal se usuário fechou browser)")
		}
	case <-execCtx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		p.logger.Warn().Msg("[Playwright] Timeout no login - usuário demorou mais de 3 minutos")
	}

	// Verificar status após o login
	status := p.GetSessionStatus()

	p.logger.Info().
		Bool("valid", status.Valid).
		Str("status", status.Status).
		Msg("[Playwright] Status da sessão após login")

	return status, nil
}
