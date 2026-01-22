package servicenow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Scraper faz scraping de páginas do ServiceNow via HTTP
type Scraper struct {
	httpClient *http.Client
	logger     *zerolog.Logger
}

// NewScraper cria um novo scraper
func NewScraper(logger *zerolog.Logger) *Scraper {
	return &Scraper{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			// Não seguir redirects automaticamente para detectar login
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger: logger,
	}
}

// ScrapeResult resultado do scraping
type ScrapeResult struct {
	Success     bool   `json:"success"`
	Description string `json:"description,omitempty"`
	ChgNumber   string `json:"chg_number,omitempty"`
	Error       string `json:"error,omitempty"`
	NeedsAuth   bool   `json:"needs_auth,omitempty"`
}

// ExtractFromURL faz GET na URL e tenta extrair o campo "Motivo da mudança"
func (s *Scraper) ExtractFromURL(ctx context.Context, chgURL string) *ScrapeResult {
	s.logger.Info().Str("url", chgURL).Msg("Tentando extrair dados da página via HTTP")

	req, err := http.NewRequestWithContext(ctx, "GET", chgURL, nil)
	if err != nil {
		return &ScrapeResult{
			Success: false,
			Error:   fmt.Sprintf("Erro ao criar requisição: %v", err),
		}
	}

	// Headers para simular navegador
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &ScrapeResult{
			Success: false,
			Error:   fmt.Sprintf("Erro na requisição: %v", err),
		}
	}
	defer resp.Body.Close()

	// Verificar se foi redirecionado para login
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently ||
		resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
		location := resp.Header.Get("Location")
		s.logger.Warn().
			Int("status", resp.StatusCode).
			Str("location", location).
			Msg("Redirecionado - provavelmente requer autenticação")

		return &ScrapeResult{
			Success:   false,
			NeedsAuth: true,
			Error:     "A página requer autenticação. Use a aba 'Texto (Manual)' e cole o conteúdo do campo 'Motivo da mudança' diretamente.",
		}
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &ScrapeResult{
			Success:   false,
			NeedsAuth: true,
			Error:     "Acesso negado. Use a aba 'Texto (Manual)' e cole o conteúdo do campo 'Motivo da mudança' diretamente.",
		}
	}

	if resp.StatusCode != http.StatusOK {
		return &ScrapeResult{
			Success: false,
			Error:   fmt.Sprintf("Servidor retornou status %d", resp.StatusCode),
		}
	}

	// Ler o HTML
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ScrapeResult{
			Success: false,
			Error:   fmt.Sprintf("Erro ao ler resposta: %v", err),
		}
	}

	html := string(body)

	// Verificar se é página de login
	if strings.Contains(html, "login") && (strings.Contains(html, "password") || strings.Contains(html, "azure") || strings.Contains(html, "saml")) {
		return &ScrapeResult{
			Success:   false,
			NeedsAuth: true,
			Error:     "Página de login detectada. Use a aba 'Texto (Manual)' e cole o conteúdo do campo 'Motivo da mudança' diretamente.",
		}
	}

	// Tentar extrair o número da CHG
	chgNumber := s.extractChgNumber(html)

	// Tentar extrair o campo "Motivo da mudança" / description
	description := s.extractDescription(html)

	if description == "" {
		s.logger.Warn().Msg("Não foi possível extrair o campo 'Motivo da mudança' do HTML")
		return &ScrapeResult{
			Success:   false,
			NeedsAuth: true,
			Error:     "Não foi possível extrair os dados da página. Isso pode ocorrer se a página requer autenticação ou tem estrutura diferente. Use a aba 'Texto (Manual)' e cole o conteúdo do campo 'Motivo da mudança' diretamente.",
		}
	}

	s.logger.Info().
		Str("chg_number", chgNumber).
		Int("description_length", len(description)).
		Msg("Dados extraídos com sucesso via HTTP")

	return &ScrapeResult{
		Success:     true,
		Description: description,
		ChgNumber:   chgNumber,
	}
}

// extractChgNumber tenta extrair o número da CHG do HTML
func (s *Scraper) extractChgNumber(html string) string {
	// Padrões comuns para número de CHG
	patterns := []string{
		`CHG\d{7,}`,                                    // CHG0123456
		`value="(CHG\d{7,})"`,                          // input value
		`>(CHG\d{7,})<`,                                // dentro de tags
		`"number"\s*:\s*"(CHG\d{7,})"`,                 // JSON
		`data-value="(CHG\d{7,})"`,                     // data attribute
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 0 {
			// Retorna o grupo capturado se existir, senão o match completo
			if len(matches) > 1 && matches[1] != "" {
				return matches[1]
			}
			return matches[0]
		}
	}

	return ""
}

// extractDescription tenta extrair o campo "Motivo da mudança" do HTML
func (s *Scraper) extractDescription(html string) string {
	// Tentar vários padrões para encontrar o campo description/justification

	// Padrão 1: textarea com id/name contendo "description" ou "justification"
	textareaPatterns := []string{
		`<textarea[^>]*(?:id|name)="[^"]*(?:description|justification|reason)[^"]*"[^>]*>([^<]*)</textarea>`,
		`<textarea[^>]*(?:id|name)="[^"]*(?:description|justification|reason)[^"]*"[^>]*>([\s\S]*?)</textarea>`,
	}

	for _, pattern := range textareaPatterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 && len(strings.TrimSpace(matches[1])) > 10 {
			return s.cleanHTML(matches[1])
		}
	}

	// Padrão 2: div ou span com data-field="description"
	divPatterns := []string{
		`data-field="(?:description|justification)"[^>]*>([^<]+)`,
		`<div[^>]*class="[^"]*description[^"]*"[^>]*>([\s\S]*?)</div>`,
	}

	for _, pattern := range divPatterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 && len(strings.TrimSpace(matches[1])) > 10 {
			return s.cleanHTML(matches[1])
		}
	}

	// Padrão 3: JSON com campo description
	jsonPattern := `"description"\s*:\s*"([^"]+)"`
	re := regexp.MustCompile(jsonPattern)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 && len(matches[1]) > 10 {
		// Decodificar escapes JSON
		desc := matches[1]
		desc = strings.ReplaceAll(desc, `\n`, "\n")
		desc = strings.ReplaceAll(desc, `\r`, "")
		desc = strings.ReplaceAll(desc, `\"`, `"`)
		return desc
	}

	return ""
}

// cleanHTML remove tags HTML e limpa o texto
func (s *Scraper) cleanHTML(html string) string {
	// Remover tags HTML
	re := regexp.MustCompile(`<[^>]*>`)
	text := re.ReplaceAllString(html, "")

	// Decodificar entidades HTML comuns
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Limpar espaços extras
	text = strings.TrimSpace(text)

	return text
}
