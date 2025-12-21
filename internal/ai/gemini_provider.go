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

// GeminiProvider implementa Provider para Google Gemini API
type GeminiProvider struct {
	apiKey  string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewGeminiProvider cria um novo GeminiProvider
func NewGeminiProvider(config *Config) *GeminiProvider {
	return &GeminiProvider{
		apiKey:  config.GeminiAPIKey,
		model:   config.GeminiModel,
		timeout: time.Duration(config.Timeout) * time.Second,
		client:  &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
	}
}

// geminiRequest estrutura de requisição Gemini API
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// geminiResponse estrutura de resposta Gemini API
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Analyze envia prompt para Gemini API e retorna análise
func (p *GeminiProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	// URL da API Gemini
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		p.model, p.apiKey)

	// Preparar requisição
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
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

	req.Header.Set("Content-Type", "application/json")

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
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse resposta
	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extrair texto da resposta
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// GetModel retorna nome do modelo
func (p *GeminiProvider) GetModel() string {
	return p.model
}

// IsAvailable verifica se Gemini API está acessível
func (p *GeminiProvider) IsAvailable(ctx context.Context) bool {
	// Tenta uma requisição simples para verificar conectividade
	testPrompt := "test"
	_, err := p.Analyze(ctx, testPrompt)
	return err == nil
}

// GetName retorna nome do provider
func (p *GeminiProvider) GetName() string {
	return "gemini"
}
