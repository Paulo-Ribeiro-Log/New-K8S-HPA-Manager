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

// ClaudeProvider implementa Provider para Anthropic Claude API
type ClaudeProvider struct {
	apiKey  string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewClaudeProvider cria um novo ClaudeProvider
func NewClaudeProvider(config *Config) *ClaudeProvider {
	return &ClaudeProvider{
		apiKey:  config.ClaudeAPIKey,
		model:   config.ClaudeModel,
		timeout: time.Duration(config.Timeout) * time.Second,
		client:  &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
	}
}

// claudeRequest estrutura de requisição Claude API
type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// claudeResponse estrutura de resposta Claude API
type claudeResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// claudeErrorResponse estrutura de erro da Claude API
type claudeErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Analyze envia prompt para Claude API e retorna análise
func (p *ClaudeProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	// URL da API Claude
	url := "https://api.anthropic.com/v1/messages"

	// Preparar requisição
	reqBody := claudeRequest{
		Model:     p.model,
		MaxTokens: 4096, // Claude suporta até 4096 tokens de output
		Messages: []claudeMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
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

	// Headers necessários para Claude API
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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
		var errResp claudeErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("claude API error (status %d): %s - %s",
				resp.StatusCode, errResp.Error.Type, errResp.Error.Message)
		}
		return "", fmt.Errorf("claude API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse resposta
	var claudeResp claudeResponse
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extrair texto da resposta
	if len(claudeResp.Content) == 0 {
		return "", fmt.Errorf("empty response from claude")
	}

	return claudeResp.Content[0].Text, nil
}

// GetModel retorna nome do modelo
func (p *ClaudeProvider) GetModel() string {
	return p.model
}

// IsAvailable verifica se Claude API está configurado
// NOTA: Não faz chamadas à API para evitar consumo de créditos automático
// A validação real da chave acontece apenas quando o usuário clica em "Validar" explicitamente
func (p *ClaudeProvider) IsAvailable(ctx context.Context) bool {
	// Apenas verificar se API key está presente (não faz requisição)
	return p.apiKey != ""
}

// GetName retorna nome do provider
func (p *ClaudeProvider) GetName() string {
	return "claude"
}
