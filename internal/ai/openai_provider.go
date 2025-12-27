package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIProvider implementa Provider para OpenAI API
type OpenAIProvider struct {
	apiKey  string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewOpenAIProvider cria um novo OpenAIProvider
func NewOpenAIProvider(config *Config) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:  config.OpenAIAPIKey,
		model:   config.OpenAIModel,
		timeout: time.Duration(config.Timeout) * time.Second,
		client:  &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
	}
}

// openAIRequest estrutura de requisição OpenAI API
type openAIRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse estrutura de resposta OpenAI API
type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// openAIErrorResponse estrutura de erro da OpenAI API
type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Param   string `json:"param"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Analyze envia prompt para OpenAI API e retorna análise
func (p *OpenAIProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	// URL da API OpenAI
	url := "https://api.openai.com/v1/chat/completions"

	// Preparar requisição
	reqBody := openAIRequest{
		Model: p.model,
		Messages: []openAIMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens: 4096, // GPT-4 suporta até 4096 tokens de output
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Criar requisição HTTP
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Headers necessários para OpenAI API
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Executar requisição
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Ler resposta
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Verificar status code
	if resp.StatusCode != http.StatusOK {
		// Tentar parsear erro estruturado
		var errResp openAIErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("openai API error (status %d): %s - %s",
				resp.StatusCode, errResp.Error.Type, errResp.Error.Message)
		}
		return "", fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse resposta
	var openaiResp openAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extrair texto da resposta
	if len(openaiResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from openai")
	}

	return openaiResp.Choices[0].Message.Content, nil
}

// GetModel retorna nome do modelo
func (p *OpenAIProvider) GetModel() string {
	return p.model
}

// IsAvailable verifica se OpenAI API está configurado
// NOTA: Não faz chamadas à API para evitar consumo de créditos automático
// A validação real da chave acontece apenas quando o usuário clica em "Validar" explicitamente
func (p *OpenAIProvider) IsAvailable(ctx context.Context) bool {
	// Apenas verificar se API key está presente (não faz requisição)
	return p.apiKey != ""
}

// GetName retorna nome do provider
func (p *OpenAIProvider) GetName() string {
	return "openai"
}
