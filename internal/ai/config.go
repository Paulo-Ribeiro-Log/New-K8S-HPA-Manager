package ai

import (
	"fmt"
	"os"
)

// Config configuração de providers AI
type Config struct {
	// Provider provider selecionado (gemini, ollama)
	Provider string

	// GeminiAPIKey chave de API do Gemini
	GeminiAPIKey string

	// GeminiModel modelo do Gemini (padrão: gemini-pro)
	GeminiModel string

	// OllamaBaseURL URL base do Ollama (padrão: http://localhost:11434)
	OllamaBaseURL string

	// OllamaModel modelo do Ollama (padrão: llama3.2)
	OllamaModel string

	// Timeout timeout para requisições AI em segundos (padrão: 120)
	Timeout int
}

// DefaultConfig retorna configuração padrão
func DefaultConfig() *Config {
	return &Config{
		Provider:      "gemini",
		GeminiModel:   "gemini-pro",
		OllamaBaseURL: "http://localhost:11434",
		OllamaModel:   "llama3.2",
		Timeout:       120,
	}
}

// Validate valida a configuração
func (c *Config) Validate() error {
	if c.Provider != "gemini" && c.Provider != "ollama" {
		return fmt.Errorf("invalid provider: %s (must be 'gemini' or 'ollama')", c.Provider)
	}

	// Validações específicas do provider
	if c.Provider == "gemini" {
		// Tentar obter API key de variável de ambiente se não foi fornecida
		if c.GeminiAPIKey == "" {
			c.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
		}

		if c.GeminiAPIKey == "" {
			return fmt.Errorf("gemini API key not provided (use --gemini-api-key or GEMINI_API_KEY env var)")
		}

		if c.GeminiModel == "" {
			c.GeminiModel = "gemini-pro"
		}
	}

	if c.Provider == "ollama" {
		if c.OllamaBaseURL == "" {
			c.OllamaBaseURL = "http://localhost:11434"
		}

		if c.OllamaModel == "" {
			c.OllamaModel = "llama3.2"
		}
	}

	if c.Timeout <= 0 {
		c.Timeout = 120
	}

	return nil
}

// GetProviderConfig retorna configuração específica do provider selecionado
func (c *Config) GetProviderConfig() map[string]string {
	config := make(map[string]string)

	if c.Provider == "gemini" {
		config["api_key"] = c.GeminiAPIKey
		config["model"] = c.GeminiModel
	} else if c.Provider == "ollama" {
		config["base_url"] = c.OllamaBaseURL
		config["model"] = c.OllamaModel
	}

	return config
}
