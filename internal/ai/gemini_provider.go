package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// GeminiProvider implementa Provider para Google Gemini API (AI Studio ou Vertex AI)
type GeminiProvider struct {
	apiKey   string
	model    string
	timeout  time.Duration
	client   *http.Client
	authMode string // "apikey" ou "vertex"
	project  string // projeto GCP (modo vertex)
	location string // região GCP (modo vertex, ex: us-central1)
}

// NewGeminiProvider cria um novo GeminiProvider
func NewGeminiProvider(config *Config) *GeminiProvider {
	return &GeminiProvider{
		apiKey:   config.GeminiAPIKey,
		model:    config.GeminiModel,
		timeout:  time.Duration(config.Timeout) * time.Second,
		client:   &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		authMode: config.GeminiAuthMode,
		project:  config.GeminiVertexProject,
		location: config.GeminiVertexLocation,
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
	if p.authMode == "vertex" {
		return p.analyzeVertex(ctx, prompt)
	}
	return p.analyzeAPIKey(ctx, prompt)
}

// analyzeAPIKey usa a API do AI Studio com chave de API
func (p *GeminiProvider) analyzeAPIKey(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		p.model, p.apiKey)

	return p.doRequest(ctx, url, "", prompt)
}

// analyzeVertex usa o Vertex AI com Application Default Credentials (SSO via gcloud)
func (p *GeminiProvider) analyzeVertex(ctx context.Context, prompt string) (string, error) {
	token, err := getGcloudAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("falha ao obter token gcloud (execute 'gcloud auth application-default login'): %w", err)
	}

	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		p.location, p.project, p.location, p.model)

	return p.doRequest(ctx, url, token, prompt)
}

// doRequest executa a requisição HTTP para a API Gemini (AI Studio ou Vertex AI)
func (p *GeminiProvider) doRequest(ctx context.Context, url, bearerToken, prompt string) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// getGcloudAccessToken obtém o access token via Application Default Credentials (gcloud)
func getGcloudAccessToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gcloud auth print-access-token: %w", err)
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("gcloud retornou token vazio")
	}
	return token, nil
}

// GetModel retorna nome do modelo
func (p *GeminiProvider) GetModel() string {
	return p.model
}

// IsAvailable verifica se Gemini API está configurado
// NOTA: Não faz chamadas à API para evitar consumo de quota automático
func (p *GeminiProvider) IsAvailable(ctx context.Context) bool {
	if p.authMode == "vertex" {
		// Verificar se gcloud está disponível e autenticado
		cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
		return cmd.Run() == nil
	}
	return p.apiKey != ""
}

// GetName retorna nome do provider
func (p *GeminiProvider) GetName() string {
	return "gemini"
}
