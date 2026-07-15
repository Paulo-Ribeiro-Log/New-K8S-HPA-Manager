package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// defaultOpenAIBaseURL é o endpoint oficial da OpenAI, usado quando OpenAIBaseURL não é
// configurado.
const defaultOpenAIBaseURL = "https://api.openai.com/v1/chat/completions"

// OpenAIProvider implementa Provider para OpenAI API (ou qualquer endpoint compatível com o
// formato de chat completions da OpenAI, ex: GitHub Models).
type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	timeout time.Duration
	client  *http.Client
}

// NewOpenAIProvider cria um novo OpenAIProvider
func NewOpenAIProvider(config *Config) *OpenAIProvider {
	baseURL := config.OpenAIBaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAIProvider{
		apiKey:  config.OpenAIAPIKey,
		model:   config.OpenAIModel,
		baseURL: baseURL,
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

// maxSizeTokensRe extrai o limite real de tokens de mensagens de erro 413 — endpoints com tier
// restrito (ex: GitHub Models) costumam informar o limite exato, ex: "Max size: 8000 tokens".
var maxSizeTokensRe = regexp.MustCompile(`(?i)max size:\s*(\d+)\s*tokens?`)

// charsPerTokenEstimate é uma aproximação grosseira (não há tokenizer real disponível aqui) —
// texto com muitos timestamps/IDs/pontuação tokeniza pior que texto corrido, então o orçamento
// real de caracteres costuma ser bem menor que essa razão "otimista" sugere — daí o
// safetyFactor em truncatePromptToTokenBudget e o retry com múltiplos fatores em Analyze.
const charsPerTokenEstimate = 4

// truncatePromptToTokenBudget corta o MEIO do prompt pra caber num orçamento aproximado de
// tokens, preservando início (instruções, identificação do recurso) e fim (formato de resposta
// esperado, geralmente o schema JSON) — normalmente as partes mais críticas dos templates em
// internal/ai/prompts.go. safetyFactor (0-1) reduz o orçamento calculado — necessário porque a
// estimativa de caracteres-por-token é imprecisa e sem margem o retry ainda estoura o limite
// real do provider. Retorna o prompt original se já couber no orçamento.
func truncatePromptToTokenBudget(prompt string, maxTokens int, safetyFactor float64) string {
	maxChars := int(float64(maxTokens*charsPerTokenEstimate) * safetyFactor)
	if maxChars <= 0 || len(prompt) <= maxChars {
		return prompt
	}
	marker := "\n\n[... conteúdo truncado automaticamente para caber no limite de tokens do provider de IA ...]\n\n"
	budget := maxChars - len(marker)
	if budget <= 0 {
		return prompt[:maxChars]
	}
	headChars := budget * 60 / 100
	tailChars := budget - headChars
	return prompt[:headChars] + marker + prompt[len(prompt)-tailChars:]
}

// openAICallResult é o resultado bruto de uma chamada HTTP à API — devolvido separado do erro
// formatado final pra permitir inspecionar status/body antes de decidir se vale tentar de novo
// (ex: 413 com prompt truncado).
type openAICallResult struct {
	statusCode int
	content    string
	errMessage string // mensagem de erro já formatada, vazia se statusCode == 200
}

// doAnalyze executa uma única chamada HTTP à API — sem retry, sem truncamento. Analyze() decide
// o que fazer com o resultado.
func (p *OpenAIProvider) doAnalyze(ctx context.Context, prompt string) (openAICallResult, error) {
	reqBody := openAIRequest{
		Model: p.model,
		Messages: []openAIMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 4096, // GPT-4 suporta até 4096 tokens de output
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return openAICallResult{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return openAICallResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return openAICallResult{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return openAICallResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp openAIErrorResponse
		msg := fmt.Sprintf("openai API error (status %d): %s", resp.StatusCode, string(body))
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			msg = fmt.Sprintf("openai API error (status %d): %s - %s",
				resp.StatusCode, errResp.Error.Type, errResp.Error.Message)
		}
		return openAICallResult{statusCode: resp.StatusCode, errMessage: msg}, nil
	}

	var openaiResp openAIResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return openAICallResult{}, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(openaiResp.Choices) == 0 {
		return openAICallResult{}, fmt.Errorf("empty response from openai")
	}

	return openAICallResult{statusCode: http.StatusOK, content: openaiResp.Choices[0].Message.Content}, nil
}

// truncateSafetyFactors: fatores de segurança tentados em ordem, do mais generoso ao mais
// agressivo. Sem tokenizer real, a estimativa de caracteres-por-token erra bastante conforme o
// conteúdo (validado ao vivo contra o GitHub Models: um prompt cheio de timestamps/IDs ainda
// estourou o 413 mesmo truncado pro orçamento "teórico" de 4 chars/token) — por isso o retry
// tenta orçamentos cada vez menores em vez de confiar numa única estimativa.
var truncateSafetyFactors = []float64{0.6, 0.3, 0.12}

// Analyze envia prompt para OpenAI API (ou endpoint compatível, ex: GitHub Models) e retorna a
// análise. Em caso de 413 (prompt maior que o permitido pelo tier do provider), extrai o limite
// real informado na mensagem de erro e tenta de novo com o prompt truncado em orçamentos
// progressivamente mais conservadores (truncateSafetyFactors) — tiers restritos como o do
// GitHub Models cortam bem abaixo da capacidade real do modelo (ex: gpt-4.1 suporta ~1M tokens
// no catálogo, mas o acesso gratuito via Copilot limita a 8000).
func (p *OpenAIProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	result, err := p.doAnalyze(ctx, prompt)
	if err != nil {
		return "", err
	}
	if result.statusCode == http.StatusOK {
		return result.content, nil
	}
	if result.statusCode != http.StatusRequestEntityTooLarge {
		return "", fmt.Errorf("%s", result.errMessage)
	}

	m := maxSizeTokensRe.FindStringSubmatch(result.errMessage)
	if len(m) != 2 {
		return "", fmt.Errorf("%s", result.errMessage)
	}
	maxTokens, convErr := strconv.Atoi(m[1])
	if convErr != nil || maxTokens <= 0 {
		return "", fmt.Errorf("%s", result.errMessage)
	}

	lastErrMsg := result.errMessage
	attempts := 0
	for _, factor := range truncateSafetyFactors {
		truncated := truncatePromptToTokenBudget(prompt, maxTokens, factor)
		if truncated == prompt {
			continue // orçamento desse fator não corta nada — não adianta repetir a chamada
		}
		attempts++
		retryResult, retryErr := p.doAnalyze(ctx, truncated)
		if retryErr != nil {
			return "", retryErr
		}
		if retryResult.statusCode == http.StatusOK {
			return retryResult.content, nil
		}
		lastErrMsg = retryResult.errMessage
	}

	return "", fmt.Errorf("%s (mesmo após %d tentativa(s) de truncar o prompt pra caber em ~%d tokens)", lastErrMsg, attempts, maxTokens)
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
