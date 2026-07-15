package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTruncatePromptToTokenBudget(t *testing.T) {
	t.Run("prompt já cabe no orçamento — retorna sem alteração", func(t *testing.T) {
		prompt := "prompt curto"
		got := truncatePromptToTokenBudget(prompt, 1000, 0.6)
		if got != prompt {
			t.Errorf("esperava prompt inalterado, got %q", got)
		}
	})

	t.Run("prompt maior que o orçamento é truncado e preserva início e fim", func(t *testing.T) {
		head := strings.Repeat("A", 500)
		middle := strings.Repeat("B", 5000)
		tail := "FIM-DO-PROMPT-SCHEMA-JSON"
		prompt := head + middle + tail

		got := truncatePromptToTokenBudget(prompt, 200, 1.0) // ~800 chars de orçamento

		if !strings.HasPrefix(got, "AAAA") {
			t.Error("esperava que o início do prompt fosse preservado")
		}
		if !strings.HasSuffix(got, tail) {
			t.Errorf("esperava que o fim do prompt (%q) fosse preservado, got suffix: %q", tail, got[len(got)-40:])
		}
		if !strings.Contains(got, "truncado") {
			t.Error("esperava marcador de truncamento no meio do prompt")
		}
		if len(got) >= len(prompt) {
			t.Errorf("prompt truncado (%d chars) não é menor que o original (%d chars)", len(got), len(prompt))
		}
	})

	t.Run("safetyFactor menor produz um corte mais agressivo", func(t *testing.T) {
		prompt := strings.Repeat("X", 10000)
		generous := truncatePromptToTokenBudget(prompt, 1000, 0.6)
		aggressive := truncatePromptToTokenBudget(prompt, 1000, 0.12)
		if len(aggressive) >= len(generous) {
			t.Errorf("safetyFactor menor deveria gerar prompt menor: aggressive=%d, generous=%d", len(aggressive), len(generous))
		}
	})

	t.Run("maxTokens zero ou negativo não trunca", func(t *testing.T) {
		prompt := strings.Repeat("X", 100)
		if got := truncatePromptToTokenBudget(prompt, 0, 0.6); got != prompt {
			t.Error("maxTokens=0 deveria retornar o prompt original")
		}
	})
}

func TestOpenAIProvider_Analyze_RetriesOn413WithTruncatedPrompt(t *testing.T) {
	var callCount int
	var receivedPrompts []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var body openAIRequest
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		receivedPrompts = append(receivedPrompts, body.Messages[0].Content)

		if callCount == 1 {
			// Primeira chamada: simula o erro real do GitHub Models
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte(`{"error":{"message":"Request body too large for gpt-4.1 model. Max size: 8000 tokens.","type":"invalid_request_error"}}`)) //nolint:errcheck
			return
		}
		// Segunda chamada (com prompt truncado): sucesso
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"análise concluída"}}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIModel = "openai/gpt-4.1"
	cfg.OpenAIBaseURL = server.URL
	cfg.Timeout = 10
	provider := NewOpenAIProvider(cfg)

	// Prompt bem maior que o orçamento de 8000 tokens (~32000 chars) pra forçar truncamento real.
	bigPrompt := strings.Repeat("linha de log muito longa\n", 3000)

	result, err := provider.Analyze(context.Background(), bigPrompt)
	if err != nil {
		t.Fatalf("Analyze retornou erro inesperado: %v", err)
	}
	if result != "análise concluída" {
		t.Errorf("result = %q, want %q", result, "análise concluída")
	}
	if callCount != 2 {
		t.Fatalf("esperava 2 chamadas (original + retry truncado), got %d", callCount)
	}
	if len(receivedPrompts[1]) >= len(receivedPrompts[0]) {
		t.Errorf("segundo prompt (retry) deveria ser menor que o primeiro: %d >= %d",
			len(receivedPrompts[1]), len(receivedPrompts[0]))
	}
}

func TestOpenAIProvider_Analyze_NoRetryWhenPromptAlreadyFits(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte(`{"error":{"message":"Request body too large for gpt-4.1 model. Max size: 8000 tokens.","type":"invalid_request_error"}}`)) //nolint:errcheck
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIBaseURL = server.URL
	cfg.Timeout = 10
	provider := NewOpenAIProvider(cfg)

	// Prompt pequeno — já cabe no limite de 8000 tokens informado, então truncar não mudaria
	// nada (truncated == prompt) e Analyze não deve tentar de novo.
	_, err := provider.Analyze(context.Background(), "prompt curto")
	if err == nil {
		t.Fatal("esperava erro (413 persistente), got nil")
	}
	if callCount != 1 {
		t.Errorf("esperava 1 chamada só (sem retry inútil), got %d", callCount)
	}
}

func TestOpenAIProvider_Analyze_OtherErrorsDoNotRetry(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`)) //nolint:errcheck
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	cfg.OpenAIBaseURL = server.URL
	cfg.Timeout = 10
	provider := NewOpenAIProvider(cfg)

	_, err := provider.Analyze(context.Background(), strings.Repeat("x", 100000))
	if err == nil {
		t.Fatal("esperava erro 401, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("erro não menciona a causa real: %v", err)
	}
	if callCount != 1 {
		t.Errorf("401 não deveria disparar retry, got %d chamadas", callCount)
	}
}
