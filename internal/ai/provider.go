package ai

import (
	"context"
	"fmt"
)

// Provider interface base para providers AI
type Provider interface {
	// Analyze envia prompt para AI e retorna análise
	Analyze(ctx context.Context, prompt string) (string, error)

	// GetModel retorna nome do modelo sendo usado
	GetModel() string

	// IsAvailable verifica se o provider está disponível
	IsAvailable(ctx context.Context) bool

	// GetName retorna nome do provider
	GetName() string
}

// NewProvider cria um novo provider baseado na configuração
func NewProvider(config *Config) (Provider, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	switch config.Provider {
	case "gemini":
		return NewGeminiProvider(config), nil

	case "ollama":
		return NewOllamaProvider(config), nil

	case "claude":
		return NewClaudeProvider(config), nil

	case "openai":
		return NewOpenAIProvider(config), nil

	case "copilot":
		return NewCopilotProvider(config), nil

	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}
