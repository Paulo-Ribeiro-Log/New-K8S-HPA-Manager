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

// OllamaProvider implementa Provider para Ollama (local)
type OllamaProvider struct {
	baseURL string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewOllamaProvider cria um novo OllamaProvider
func NewOllamaProvider(config *Config) *OllamaProvider {
	return &OllamaProvider{
		baseURL: config.OllamaBaseURL,
		model:   config.OllamaModel,
		timeout: time.Duration(config.Timeout) * time.Second,
		client:  &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
	}
}

// ollamaRequest estrutura de requisição Ollama API
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaResponse estrutura de resposta Ollama API
type ollamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

// Analyze envia prompt para Ollama e retorna análise
func (p *OllamaProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	// URL da API Ollama
	url := fmt.Sprintf("%s/api/generate", p.baseURL)

	// Preparar requisição
	reqBody := ollamaRequest{
		Model:  p.model,
		Prompt: prompt,
		Stream: false, // Não usar streaming para simplificar
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
		return "", fmt.Errorf("failed to send request (is Ollama running?): %w", err)
	}
	defer resp.Body.Close()

	// Ler resposta
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Verificar status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse resposta
	var ollamaResp ollamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Verificar se resposta está completa
	if !ollamaResp.Done {
		return "", fmt.Errorf("incomplete response from ollama")
	}

	return ollamaResp.Response, nil
}

// GetModel retorna nome do modelo
func (p *OllamaProvider) GetModel() string {
	return p.model
}

// IsAvailable verifica se Ollama está acessível
func (p *OllamaProvider) IsAvailable(ctx context.Context) bool {
	// Tenta ping na API
	url := fmt.Sprintf("%s/api/tags", p.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// GetName retorna nome do provider
func (p *OllamaProvider) GetName() string {
	return "ollama"
}
