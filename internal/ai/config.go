package ai

import (
	"fmt"
	"os"
)

// Config configuração de providers AI
type Config struct {
	// Provider provider selecionado (gemini, ollama, claude, openai)
	Provider string

	// GeminiAPIKey chave de API do Gemini
	GeminiAPIKey string

	// GeminiModel modelo do Gemini (padrão: gemini-pro)
	GeminiModel string

	// OllamaBaseURL URL base do Ollama (padrão: http://localhost:11434)
	OllamaBaseURL string

	// OllamaModel modelo do Ollama (padrão: llama3.2)
	OllamaModel string

	// ClaudeAPIKey chave de API do Claude (Anthropic)
	ClaudeAPIKey string

	// ClaudeModel modelo do Claude (padrão: claude-3-5-sonnet-20241022)
	ClaudeModel string

	// OpenAIAPIKey chave de API do OpenAI
	OpenAIAPIKey string

	// OpenAIModel modelo do OpenAI (padrão: gpt-4o-mini)
	OpenAIModel string

	// CopilotAPIKey chave de API do Microsoft Copilot (Azure OpenAI)
	CopilotAPIKey string

	// CopilotEndpoint endpoint do Azure OpenAI (ex: https://my-resource.openai.azure.com)
	CopilotEndpoint string

	// CopilotDeployment nome do deployment no Azure OpenAI
	CopilotDeployment string

	// CopilotAPIVersion versão da API Azure OpenAI (padrão: 2024-02-15-preview)
	CopilotAPIVersion string

	// Timeout timeout para requisições AI em segundos (padrão: 300 - 5 minutos)
	Timeout int
}

// DefaultConfig retorna configuração padrão
func DefaultConfig() *Config {
	return &Config{
		Provider:          "ollama", // Ollama llama3.2 3B
		GeminiModel:       "gemini-2.0-flash-exp", // Free tier: 15 RPM / 1.5M RPD
		OllamaBaseURL:     "http://localhost:11434",
		OllamaModel:       "llama3.2:3b", // Llama3.2 3B - melhor qualidade
		ClaudeModel:       "claude-3-5-sonnet-20241022",
		OpenAIModel:       "gpt-4o-mini",            // GPT-4o-mini - barato e eficiente
		CopilotAPIVersion: "2024-02-15-preview",     // Azure OpenAI API version
		CopilotDeployment: "gpt-4o",                 // Deployment padrão
		Timeout:           300,                      // 5 minutos para modelos mais lentos
	}
}

// Validate valida a configuração
func (c *Config) Validate() error {
	if c.Provider != "gemini" && c.Provider != "ollama" && c.Provider != "claude" && c.Provider != "openai" && c.Provider != "copilot" {
		return fmt.Errorf("invalid provider: %s (must be 'gemini', 'ollama', 'claude', 'openai' or 'copilot')", c.Provider)
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
			c.GeminiModel = "gemini-2.0-flash-exp" // Modelo padrão (fallback se não especificado)
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

	if c.Provider == "claude" {
		// Tentar obter API key de variável de ambiente se não foi fornecida
		if c.ClaudeAPIKey == "" {
			c.ClaudeAPIKey = os.Getenv("CLAUDE_API_KEY")
		}

		if c.ClaudeAPIKey == "" {
			return fmt.Errorf("claude API key not provided (use --claude-api-key or CLAUDE_API_KEY env var)")
		}

		if c.ClaudeModel == "" {
			c.ClaudeModel = "claude-3-5-sonnet-20241022"
		}
	}

	if c.Provider == "openai" {
		// Tentar obter API key de variável de ambiente se não foi fornecida
		if c.OpenAIAPIKey == "" {
			c.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
		}

		if c.OpenAIAPIKey == "" {
			return fmt.Errorf("openai API key not provided (use --openai-api-key or OPENAI_API_KEY env var)")
		}

		if c.OpenAIModel == "" {
			c.OpenAIModel = "gpt-4o-mini"
		}
	}

	if c.Provider == "copilot" {
		// Tentar obter API key de variável de ambiente se não foi fornecida
		if c.CopilotAPIKey == "" {
			c.CopilotAPIKey = os.Getenv("COPILOT_API_KEY")
		}

		if c.CopilotAPIKey == "" {
			return fmt.Errorf("copilot API key not provided (use --copilot-api-key or COPILOT_API_KEY env var)")
		}

		// Endpoint é obrigatório para Azure OpenAI
		if c.CopilotEndpoint == "" {
			c.CopilotEndpoint = os.Getenv("COPILOT_ENDPOINT")
		}

		if c.CopilotEndpoint == "" {
			return fmt.Errorf("copilot endpoint not provided (use --copilot-endpoint or COPILOT_ENDPOINT env var)")
		}

		// Deployment é obrigatório para Azure OpenAI
		if c.CopilotDeployment == "" {
			c.CopilotDeployment = os.Getenv("COPILOT_DEPLOYMENT")
		}

		if c.CopilotDeployment == "" {
			c.CopilotDeployment = "gpt-4o" // Padrão
		}

		if c.CopilotAPIVersion == "" {
			c.CopilotAPIVersion = "2024-02-15-preview"
		}
	}

	if c.Timeout <= 0 {
		c.Timeout = 300 // 5 minutos para análises preditivas complexas
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
	} else if c.Provider == "claude" {
		config["api_key"] = c.ClaudeAPIKey
		config["model"] = c.ClaudeModel
	} else if c.Provider == "openai" {
		config["api_key"] = c.OpenAIAPIKey
		config["model"] = c.OpenAIModel
	} else if c.Provider == "copilot" {
		config["api_key"] = c.CopilotAPIKey
		config["endpoint"] = c.CopilotEndpoint
		config["deployment"] = c.CopilotDeployment
		config["api_version"] = c.CopilotAPIVersion
	}

	return config
}
