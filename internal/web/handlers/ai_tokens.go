package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
)

// AITokensHandler gerencia tokens AI dos usuários
type AITokensHandler struct {
	tokensStore *storage.UserTokensStore
}

// NewAITokensHandler cria nova instância
func NewAITokensHandler(tokensStore *storage.UserTokensStore) *AITokensHandler {
	return &AITokensHandler{
		tokensStore: tokensStore,
	}
}

// SaveTokensRequest request para salvar tokens
type SaveTokensRequest struct {
	GeminiAPIKey        string `json:"gemini_api_key,omitempty"`
	GeminiModel         string `json:"gemini_model,omitempty"`
	OpenAIAPIKey        string `json:"openai_api_key,omitempty"`
	OpenAIModel         string `json:"openai_model,omitempty"`
	ClaudeAPIKey        string `json:"claude_api_key,omitempty"`
	ClaudeModel         string `json:"claude_model,omitempty"`
	CopilotAPIKey       string `json:"copilot_api_key,omitempty"`
	CopilotEndpoint     string `json:"copilot_endpoint,omitempty"`
	CopilotDeployment   string `json:"copilot_deployment,omitempty"`
	OllamaModel         string `json:"ollama_model,omitempty"`
	PreferredProvider   string `json:"preferred_provider"`
}

// TokensResponse response com tokens (sem expor valores completos)
type TokensResponse struct {
	HasGemini         bool   `json:"has_gemini"`
	GeminiModel       string `json:"gemini_model,omitempty"`
	HasOpenAI         bool   `json:"has_openai"`
	OpenAIModel       string `json:"openai_model,omitempty"`
	HasClaude         bool   `json:"has_claude"`
	ClaudeModel       string `json:"claude_model,omitempty"`
	HasCopilot        bool   `json:"has_copilot"`
	CopilotEndpoint   string `json:"copilot_endpoint,omitempty"`
	CopilotDeployment string `json:"copilot_deployment,omitempty"`
	OllamaModel       string `json:"ollama_model,omitempty"`
	PreferredProvider string `json:"preferred_provider"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

// SaveTokens salva tokens do usuário
func (h *AITokensHandler) SaveTokens(c *gin.Context) {
	// Obter user email do contexto RBAC
	userEmail, exists := c.Get("user_email")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userEmailStr, ok := userEmail.(string)
	if !ok || userEmailStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user email"})
		return
	}

	var req SaveTokensRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	// Validar preferred provider
	if req.PreferredProvider == "" {
		req.PreferredProvider = "ollama" // Padrão
	}

	validProviders := map[string]bool{"gemini": true, "openai": true, "claude": true, "copilot": true, "ollama": true}
	if !validProviders[req.PreferredProvider] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid preferred_provider (must be: gemini, openai, claude, copilot, or ollama)",
		})
		return
	}

	// Buscar tokens existentes para fazer merge (manter chaves existentes se novas não forem fornecidas)
	existingTokens, err := h.tokensStore.GetTokens(userEmailStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to get existing tokens",
		})
		return
	}

	// Se não existir, criar novo
	if existingTokens == nil {
		existingTokens = &storage.UserTokens{
			UserEmail: userEmailStr,
		}
	}

	// Validar tokens (testar se são válidos) - apenas se novos tokens forem fornecidos
	validationErrors := make(map[string]string)

	if req.GeminiAPIKey != "" {
		if err := validateGeminiToken(req.GeminiAPIKey); err != nil {
			validationErrors["gemini"] = err.Error()
		}
	}

	if req.ClaudeAPIKey != "" {
		if err := validateClaudeToken(req.ClaudeAPIKey); err != nil {
			validationErrors["claude"] = err.Error()
		}
	}

	if req.OpenAIAPIKey != "" {
		if err := validateOpenAIToken(req.OpenAIAPIKey); err != nil {
			validationErrors["openai"] = err.Error()
		}
	}

	if req.CopilotAPIKey != "" {
		if err := validateCopilotToken(req.CopilotAPIKey, req.CopilotEndpoint, req.CopilotDeployment); err != nil {
			validationErrors["copilot"] = err.Error()
		}
	}

	if len(validationErrors) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "token validation failed",
			"validation_errors": validationErrors,
		})
		return
	}

	// Fazer merge: manter valores existentes se novos não forem fornecidos
	tokens := &storage.UserTokens{
		UserEmail:         userEmailStr,
		PreferredProvider: req.PreferredProvider,
	}

	// Gemini
	if req.GeminiAPIKey != "" {
		tokens.GeminiAPIKey = req.GeminiAPIKey
	} else {
		tokens.GeminiAPIKey = existingTokens.GeminiAPIKey
	}
	if req.GeminiModel != "" {
		tokens.GeminiModel = req.GeminiModel
	} else {
		tokens.GeminiModel = existingTokens.GeminiModel
	}

	// OpenAI
	if req.OpenAIAPIKey != "" {
		tokens.OpenAIAPIKey = req.OpenAIAPIKey
	} else {
		tokens.OpenAIAPIKey = existingTokens.OpenAIAPIKey
	}
	if req.OpenAIModel != "" {
		tokens.OpenAIModel = req.OpenAIModel
	} else {
		tokens.OpenAIModel = existingTokens.OpenAIModel
	}

	// Claude
	if req.ClaudeAPIKey != "" {
		tokens.ClaudeAPIKey = req.ClaudeAPIKey
	} else {
		tokens.ClaudeAPIKey = existingTokens.ClaudeAPIKey
	}
	if req.ClaudeModel != "" {
		tokens.ClaudeModel = req.ClaudeModel
	} else {
		tokens.ClaudeModel = existingTokens.ClaudeModel
	}

	// Copilot
	if req.CopilotAPIKey != "" {
		tokens.CopilotAPIKey = req.CopilotAPIKey
	} else {
		tokens.CopilotAPIKey = existingTokens.CopilotAPIKey
	}
	if req.CopilotEndpoint != "" {
		tokens.CopilotEndpoint = req.CopilotEndpoint
	} else {
		tokens.CopilotEndpoint = existingTokens.CopilotEndpoint
	}
	if req.CopilotDeployment != "" {
		tokens.CopilotDeployment = req.CopilotDeployment
	} else {
		tokens.CopilotDeployment = existingTokens.CopilotDeployment
	}

	// Ollama
	if req.OllamaModel != "" {
		tokens.OllamaModel = req.OllamaModel
	} else {
		tokens.OllamaModel = existingTokens.OllamaModel
	}

	if err := h.tokensStore.SaveTokens(userEmailStr, tokens); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to save tokens",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "tokens saved successfully",
	})
}

// GetTokens retorna status dos tokens (sem expor valores)
func (h *AITokensHandler) GetTokens(c *gin.Context) {
	// Obter user email do contexto RBAC
	userEmail, exists := c.Get("user_email")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userEmailStr, ok := userEmail.(string)
	if !ok || userEmailStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user email"})
		return
	}

	tokens, err := h.tokensStore.GetTokens(userEmailStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to get tokens",
		})
		return
	}

	// Se não tem tokens configurados, retornar vazio
	if tokens == nil {
		c.JSON(http.StatusOK, TokensResponse{
			HasGemini:         false,
			HasOpenAI:         false,
			HasClaude:         false,
			HasCopilot:        false,
			PreferredProvider: "ollama",
		})
		return
	}

	// Retornar apenas status (não expor tokens completos)
	c.JSON(http.StatusOK, TokensResponse{
		HasGemini:         tokens.GeminiAPIKey != "",
		GeminiModel:       tokens.GeminiModel,
		HasOpenAI:         tokens.OpenAIAPIKey != "",
		OpenAIModel:       tokens.OpenAIModel,
		HasClaude:         tokens.ClaudeAPIKey != "",
		ClaudeModel:       tokens.ClaudeModel,
		HasCopilot:        tokens.CopilotAPIKey != "",
		CopilotEndpoint:   tokens.CopilotEndpoint,
		CopilotDeployment: tokens.CopilotDeployment,
		OllamaModel:       tokens.OllamaModel,
		PreferredProvider: tokens.PreferredProvider,
		UpdatedAt:         tokens.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// DeleteTokens remove tokens do usuário
func (h *AITokensHandler) DeleteTokens(c *gin.Context) {
	// Obter user email do contexto RBAC
	userEmail, exists := c.Get("user_email")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userEmailStr, ok := userEmail.(string)
	if !ok || userEmailStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user email"})
		return
	}

	if err := h.tokensStore.DeleteTokens(userEmailStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to delete tokens",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "tokens deleted successfully",
	})
}

// ValidateToken valida um token específico sem salvar
func (h *AITokensHandler) ValidateToken(c *gin.Context) {
	var req struct {
		Provider   string `json:"provider"`
		APIKey     string `json:"api_key"`
		Endpoint   string `json:"endpoint,omitempty"`   // Para Copilot
		Deployment string `json:"deployment,omitempty"` // Para Copilot
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	var validationErr error
	switch req.Provider {
	case "gemini":
		validationErr = validateGeminiToken(req.APIKey)
	case "claude":
		validationErr = validateClaudeToken(req.APIKey)
	case "openai":
		validationErr = validateOpenAIToken(req.APIKey)
	case "copilot":
		validationErr = validateCopilotToken(req.APIKey, req.Endpoint, req.Deployment)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid provider",
		})
		return
	}

	if validationErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"valid": false,
			"error": validationErr.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"message": fmt.Sprintf("%s token is valid", req.Provider),
	})
}

// validateGeminiToken valida token do Gemini
func validateGeminiToken(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key is empty")
	}

	// ⚠️ IMPORTANTE: NÃO fazer chamadas à API para validar!
	// Cada chamada consome quota (mesmo IsAvailable)
	// Validar apenas formato básico
	
	// Gemini API keys geralmente têm ~39 caracteres e formato "AIzaSy..."
	if len(apiKey) < 20 {
		return fmt.Errorf("API key is too short (minimum 20 characters)")
	}
	
	if len(apiKey) > 100 {
		return fmt.Errorf("API key is too long (maximum 100 characters)")
	}

	// Validação de formato básica (Gemini keys começam com "AIza")
	if !strings.HasPrefix(apiKey, "AIza") {
		return fmt.Errorf("Gemini API key deve começar com 'AIza'")
	}

	return nil
}

// validateClaudeToken valida token do Claude (Anthropic)
func validateClaudeToken(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key is empty")
	}

	// ⚠️ IMPORTANTE: NÃO fazer chamadas à API para validar!
	// Cada chamada pode consumir quota dependendo do plano
	// Validar apenas formato básico
	
	// Claude API keys começam com "sk-ant-api03-"
	if len(apiKey) < 20 {
		return fmt.Errorf("API key is too short (minimum 20 characters)")
	}
	
	if !strings.HasPrefix(apiKey, "sk-ant-") {
		return fmt.Errorf("Claude API key deve começar com 'sk-ant-'")
	}

	return nil
}

// validateOpenAIToken valida token do OpenAI
func validateOpenAIToken(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key is empty")
	}

	// Validar formato básico do token OpenAI
	// OpenAI API keys começam com "sk-"
	if len(apiKey) < 20 {
		return fmt.Errorf("API key is too short (minimum 20 characters)")
	}
	
	// ⚠️ IMPORTANTE: NÃO fazer chamadas à API para validar!
	// Cada chamada pode consumir quota dependendo do plano
	// Validar apenas formato básico
	
	if !strings.HasPrefix(apiKey, "sk-") {
		return fmt.Errorf("OpenAI API key deve começar com 'sk-'")
	}

	return nil
}

// validateCopilotToken valida token do Microsoft Copilot (Azure OpenAI)
func validateCopilotToken(apiKey, endpoint, deployment string) error {
	if apiKey == "" {
		return fmt.Errorf("API key is empty")
	}

	if endpoint == "" {
		return fmt.Errorf("endpoint is required for Copilot (Azure OpenAI)")
	}

	if deployment == "" {
		deployment = "gpt-4o" // Padrão
	}

	// Validar formato básico
	if len(apiKey) < 20 {
		return fmt.Errorf("API key is too short (minimum 20 characters)")
	}
	
	// ⚠️ IMPORTANTE: NÃO fazer chamadas à API para validar!
	// Cada chamada pode consumir quota dependendo do plano
	// Validar apenas formato básico
	
	// Validar formato do endpoint (deve ser URL HTTPS)
	if !strings.HasPrefix(endpoint, "https://") {
		return fmt.Errorf("Endpoint must be an HTTPS URL")
	}
	
	// Validar que deployment não está vazio
	if len(deployment) < 3 {
		return fmt.Errorf("Deployment name is too short")
	}

	return nil
}

// ModelInfo informações sobre um modelo disponível
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"is_default"`
}

// ModelsResponse resposta com modelos disponíveis por provider
type ModelsResponse struct {
	Provider string      `json:"provider"`
	Models   []ModelInfo `json:"models"`
}

// GetAvailableModels retorna modelos disponíveis por provider
func (h *AITokensHandler) GetAvailableModels(c *gin.Context) {
	provider := c.Query("provider")
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "provider query parameter is required",
		})
		return
	}

	var models []ModelInfo

	switch provider {
	case "gemini":
		models = []ModelInfo{
			{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Description: "Mais recente e rápido (recomendado)", IsDefault: true},
			{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Description: "Mais avançado com maior capacidade"},
			{ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", Description: "Versão estável anterior"},
			{ID: "gemini-2.0-flash-exp", Name: "Gemini 2.0 Flash (Experimental)", Description: "Experimental - quotas limitadas no Free Tier"},
		}
	case "claude":
		models = []ModelInfo{
			{ID: "claude-3-5-sonnet-20241022", Name: "Claude 3.5 Sonnet", Description: "Modelo equilibrado (recomendado)", IsDefault: true},
			{ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku", Description: "Modelo rápido e econômico"},
			{ID: "claude-3-opus-20240229", Name: "Claude 3 Opus", Description: "Modelo mais avançado"},
		}
	case "openai":
		models = []ModelInfo{
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Description: "Modelo rápido e econômico", IsDefault: true},
			{ID: "gpt-4o", Name: "GPT-4o", Description: "Modelo mais avançado"},
			{ID: "gpt-4-turbo", Name: "GPT-4 Turbo", Description: "Modelo anterior mais rápido"},
			{ID: "gpt-3.5-turbo", Name: "GPT-3.5 Turbo", Description: "Modelo anterior econômico"},
		}
	case "ollama":
		models = []ModelInfo{
			{ID: "llama3.2:3b", Name: "Llama 3.2 3B", Description: "Modelo rápido (3B parâmetros)", IsDefault: true},
			{ID: "qwen2.5:7b", Name: "Qwen 2.5 7B", Description: "Modelo médio (7B parâmetros)"},
			{ID: "deepseek-r1:7b", Name: "DeepSeek R1 7B", Description: "Modelo de raciocínio (7B)"},
			{ID: "qwen2.5:14b", Name: "Qwen 2.5 14B", Description: "Modelo avançado (14B parâmetros)"},
		}
	case "copilot":
		models = []ModelInfo{
			{ID: "gpt-4o", Name: "GPT-4o (Azure)", Description: "Deployment padrão", IsDefault: true},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini (Azure)", Description: "Deployment econômico"},
			{ID: "gpt-4-turbo", Name: "GPT-4 Turbo (Azure)", Description: "Deployment turbo"},
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid provider (must be: gemini, claude, openai, ollama, or copilot)",
		})
		return
	}

	c.JSON(http.StatusOK, ModelsResponse{
		Provider: provider,
		Models:   models,
	})
}

// RegisterRoutes registra rotas do handler
func (h *AITokensHandler) RegisterRoutes(router *gin.RouterGroup, rbacMiddleware interface{ InjectUserEmail() gin.HandlerFunc }) {
	// Aplicar middleware de injeção de user_email para todas as rotas de tokens
	tokens := router.Group("/ai/tokens")
	tokens.Use(rbacMiddleware.InjectUserEmail())
	{
		tokens.GET("", h.GetTokens)               // GET /api/v1/ai/tokens
		tokens.POST("", h.SaveTokens)             // POST /api/v1/ai/tokens
		tokens.DELETE("", h.DeleteTokens)         // DELETE /api/v1/ai/tokens
		tokens.POST("/validate", h.ValidateToken) // POST /api/v1/ai/tokens/validate
	}

	// Endpoint de modelos disponíveis (não precisa de user_email)
	router.GET("/ai/models", h.GetAvailableModels) // GET /api/v1/ai/models?provider=gemini
}
