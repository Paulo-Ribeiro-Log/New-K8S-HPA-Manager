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
	// Detectar diretório do frontend usando caminhos absolutos
	frontendDir := ""

	// Obter diretório atual do executável
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)

	// Obter diretório de trabalho atual
	cwd, _ := os.Getwd()

	possiblePaths := []string{
		// Caminho absoluto a partir do diretório do executável (build/)
		filepath.Join(execDir, "..", "internal", "web", "frontend"),
		// Caminho absoluto a partir do CWD
		filepath.Join(cwd, "internal", "web", "frontend"),
		// Caminhos relativos
		"internal/web/frontend",
		"./internal/web/frontend",
		// Caminho hardcoded para desenvolvimento
		filepath.Join(os.Getenv("HOME"), "Scripts", "Scripts GO", "New-K8s-HPA-Manager", "Scale_HPA", "internal", "web", "frontend"),
	}

	for _, p := range possiblePaths {
		// Converter para caminho absoluto
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}

		scriptPath := filepath.Join(absPath, "playwright", "servicenow-extractor.ts")
		if _, err := os.Stat(scriptPath); err == nil {
			frontendDir = absPath
			logger.Info().
				Str("path", absPath).
				Str("script", scriptPath).
				Msg("[Playwright] Encontrado diretório do frontend")
			break
		}
	}

	if frontendDir == "" {
		logger.Warn().
			Str("cwd", cwd).
			Str("execDir", execDir).
			Msg("[Playwright] Diretório do frontend não encontrado")
	}

	return &PlaywrightExtractor{
		frontendDir: frontendDir,
		logger:      logger,
	}
}

// IsConfigured verifica se o Playwright está configurado
func (p *PlaywrightExtractor) IsConfigured() bool {
	return p.frontendDir != ""
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
		"cwd":                   cwd,
		"exec_path":             execPath,
	}

	if p.frontendDir != "" {
		status["playwright_configured"] = true

		scriptPath := filepath.Join(p.frontendDir, "playwright", "servicenow-extractor.ts")
		if _, err := os.Stat(scriptPath); err == nil {
			status["script_exists"] = true
		}
	}

	// Verificar npx
	if _, err := exec.LookPath("npx"); err == nil {
		status["npx_available"] = true
	}

	// Verificar tsx (substitui ts-node)
	cmd := exec.Command("npx", "tsx", "--version")
	cmd.Dir = p.frontendDir
	if err := cmd.Run(); err == nil {
		status["ts_node_available"] = true
	}

	return status
}

// Extract executa o script Playwright para extrair dados da CHG
func (p *PlaywrightExtractor) Extract(ctx context.Context, chgURL string) (*PlaywrightResult, error) {
	if !p.IsConfigured() {
		return nil, fmt.Errorf("Playwright não está configurado - diretório do frontend não encontrado")
	}

	// Validar URL
	if !strings.Contains(chgURL, "service-now.com") {
		return nil, fmt.Errorf("URL inválida: deve ser uma URL do ServiceNow")
	}

	sysIDRegex := regexp.MustCompile(`sys_id=([a-f0-9]{32})`)
	if !sysIDRegex.MatchString(chgURL) {
		return nil, fmt.Errorf("URL inválida: sys_id não encontrado")
	}

	scriptPath := filepath.Join(p.frontendDir, "playwright", "servicenow-extractor.ts")

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
