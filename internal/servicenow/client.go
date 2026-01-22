package servicenow

import (
	"context"

	"github.com/rs/zerolog"
)

// Client é o cliente para o ServiceNow
type Client struct {
	scraper *Scraper
	logger  *zerolog.Logger
}

// NewClient cria um novo cliente ServiceNow
func NewClient(baseURL string, logger *zerolog.Logger) *Client {
	return &Client{
		scraper: NewScraper(logger),
		logger:  logger,
	}
}

// ImportFromURL tenta extrair dados da CHG via HTTP scraping
// Se não conseguir (requer auth), retorna erro indicando usar aba manual
func (c *Client) ImportFromURL(ctx context.Context, chgURL string) (*ImportResponse, error) {
	c.logger.Info().
		Str("url", chgURL).
		Msg("Tentando importar CHG via HTTP")

	// Tentar fazer scraping da página
	result := c.scraper.ExtractFromURL(ctx, chgURL)

	if !result.Success {
		return &ImportResponse{
			Success: false,
			Error:   result.Error,
		}, nil
	}

	// Extrair dados do description
	extractedData := ExtractFromDescription(result.Description)

	c.logger.Info().
		Str("chg_number", result.ChgNumber).
		Str("application", extractedData.Application).
		Str("version", extractedData.Version).
		Str("github_repo", extractedData.GitHubRepo).
		Float64("confidence", extractedData.Confidence).
		Msg("Dados extraídos da CHG")

	return &ImportResponse{
		Success: true,
		ChangeRequest: &ChangeRequest{
			Number:      result.ChgNumber,
			Description: result.Description,
		},
		ExtractedData: extractedData,
	}, nil
}

// ImportFromDescription importa dados diretamente do texto do description
func (c *Client) ImportFromDescription(description string) *ExtractedData {
	return ExtractFromDescription(description)
}
