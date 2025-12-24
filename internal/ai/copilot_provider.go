package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CopilotProvider implementa Provider para Microsoft Copilot (Azure OpenAI)
type CopilotProvider struct {
	apiKey     string
	endpoint   string // Azure OpenAI endpoint (ex: https://my-resource.openai.azure.com)
	deployment string // Deployment name
	apiVersion string // API version (ex: 2024-02-15-preview)
	timeout    time.Duration
	client     *http.Client
}

// NewCopilotProvider cria um novo CopilotProvider
func NewCopilotProvider(config *Config) *CopilotProvider {
	// Garantir que endpoint não tenha trailing slash
	endpoint := strings.TrimSuffix(config.CopilotEndpoint, "/")

	// API version padrão se não especificada
	apiVersion := config.CopilotAPIVersion
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	return &CopilotProvider{
		apiKey:     config.CopilotAPIKey,
		endpoint:   endpoint,
		deployment: config.CopilotDeployment,
		apiVersion: apiVersion,
		timeout:    time.Duration(config.Timeout) * time.Second,
		client:     &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
	}
}

// copilotRequest estrutura de requisição Azure OpenAI API
type copilotRequest struct {
	Messages  []copilotMessage `json:"messages"`
	MaxTokens int              `json:"max_tokens,omitempty"`
}

type copilotMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// copilotResponse estrutura de resposta Azure OpenAI API
type copilotResponse struct {
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

// copilotErrorResponse estrutura de erro da Azure OpenAI API
type copilotErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Param   string `json:"param"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Analyze envia prompt para Azure OpenAI API e retorna análise
func (p *CopilotProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	// Construir URL da API Azure OpenAI
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		p.endpoint, p.deployment, p.apiVersion)

	// Preparar requisição
	reqBody := copilotRequest{
		Messages: []copilotMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens: 4096,
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

	// Headers necessários para Azure OpenAI API
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", p.apiKey) // Azure usa "api-key" ao invés de "Authorization: Bearer"

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
		var errResp copilotErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("copilot API error (status %d): %s - %s",
				resp.StatusCode, errResp.Error.Type, errResp.Error.Message)
		}
		return "", fmt.Errorf("copilot API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse resposta
	var copilotResp copilotResponse
	if err := json.Unmarshal(body, &copilotResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extrair texto da resposta
	if len(copilotResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from copilot")
	}

	return copilotResp.Choices[0].Message.Content, nil
}

// GetModel retorna nome do deployment
func (p *CopilotProvider) GetModel() string {
	return p.deployment
}

// IsAvailable verifica se Azure OpenAI API está acessível
func (p *CopilotProvider) IsAvailable(ctx context.Context) bool {
	// Verificar se API key e endpoint estão presentes
	if p.apiKey == "" || p.endpoint == "" || p.deployment == "" {
		return false
	}

	// Fazer check leve com timeout curto
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Construir URL de teste
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		p.endpoint, p.deployment, p.apiVersion)

	// Fazer uma requisição mínima de teste
	reqBody := copilotRequest{
		Messages: []copilotMessage{
			{
				Role:    "user",
				Content: "test",
			},
		},
		MaxTokens: 10, // Requisição mínima
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return false
	}

	req, err := http.NewRequestWithContext(checkCtx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", p.apiKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Verificar se API retornou 200 OK (chave válida)
	return resp.StatusCode == http.StatusOK
}

// GetName retorna nome do provider
func (p *CopilotProvider) GetName() string {
	return "copilot"
}
