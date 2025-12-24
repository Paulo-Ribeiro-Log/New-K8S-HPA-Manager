package handlers

import (
	"context"
	"fmt"
	"net/http"

	"k8s-hpa-manager/internal/ai"
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
	OpenAIAPIKey        string `json:"openai_api_key,omitempty"`
	ClaudeAPIKey        string `json:"claude_api_key,omitempty"`
	CopilotAPIKey       string `json:"copilot_api_key,omitempty"`
	CopilotEndpoint     string `json:"copilot_endpoint,omitempty"`
	CopilotDeployment   string `json:"copilot_deployment,omitempty"`
	PreferredProvider   string `json:"preferred_provider"`
}

// TokensResponse response com tokens (sem expor valores completos)
type TokensResponse struct {
	HasGemini         bool   `json:"has_gemini"`
	HasOpenAI         bool   `json:"has_openai"`
	HasClaude         bool   `json:"has_claude"`
	HasCopilot        bool   `json:"has_copilot"`
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

	// Validar que pelo menos um token foi fornecido
	if req.GeminiAPIKey == "" && req.OpenAIAPIKey == "" && req.ClaudeAPIKey == "" && req.CopilotAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "at least one API key must be provided",
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

	// Validar tokens (testar se são válidos)
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

	// Salvar tokens
	tokens := &storage.UserTokens{
		UserEmail:         userEmailStr,
		GeminiAPIKey:      req.GeminiAPIKey,
		OpenAIAPIKey:      req.OpenAIAPIKey,
		ClaudeAPIKey:      req.ClaudeAPIKey,
		CopilotAPIKey:     req.CopilotAPIKey,
		CopilotEndpoint:   req.CopilotEndpoint,
		CopilotDeployment: req.CopilotDeployment,
		PreferredProvider: req.PreferredProvider,
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
		HasOpenAI:         tokens.OpenAIAPIKey != "",
		HasClaude:         tokens.ClaudeAPIKey != "",
		HasCopilot:        tokens.CopilotAPIKey != "",
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

	// Criar config temporário
	config := &ai.Config{
		Provider:     "gemini",
		GeminiAPIKey: apiKey,
		GeminiModel:  "gemini-2.0-flash-exp",
		Timeout:      10,
	}

	// Criar provider
	provider, err := ai.NewProvider(config)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Testar disponibilidade (não consome quota significativa)
	ctx := context.Background()
	if !provider.IsAvailable(ctx) {
		return fmt.Errorf("API key is invalid or Gemini service is unavailable")
	}

	return nil
}

// validateClaudeToken valida token do Claude (Anthropic)
func validateClaudeToken(apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("API key is empty")
	}

	// Validar formato básico do token Claude
	// Claude API keys começam com "sk-ant-api03-"
	if len(apiKey) < 20 {
		return fmt.Errorf("API key is too short (minimum 20 characters)")
	}

	// Criar config temporário
	config := &ai.Config{
		Provider:     "claude",
		ClaudeAPIKey: apiKey,
		ClaudeModel:  "claude-3-5-sonnet-20241022",
		Timeout:      10,
	}

	// Criar provider
	provider, err := ai.NewProvider(config)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Testar disponibilidade (faz requisição mínima para validar token)
	ctx := context.Background()
	if !provider.IsAvailable(ctx) {
		return fmt.Errorf("API key is invalid or Claude service is unavailable")
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

	// Criar config temporário
	config := &ai.Config{
		Provider:     "openai",
		OpenAIAPIKey: apiKey,
		OpenAIModel:  "gpt-4o-mini",
		Timeout:      10,
	}

	// Criar provider
	provider, err := ai.NewProvider(config)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Testar disponibilidade (faz requisição mínima para validar token)
	ctx := context.Background()
	if !provider.IsAvailable(ctx) {
		return fmt.Errorf("API key is invalid or OpenAI service is unavailable")
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

	// Criar config temporário
	config := &ai.Config{
		Provider:          "copilot",
		CopilotAPIKey:     apiKey,
		CopilotEndpoint:   endpoint,
		CopilotDeployment: deployment,
		CopilotAPIVersion: "2024-02-15-preview",
		Timeout:           10,
	}

	// Criar provider
	provider, err := ai.NewProvider(config)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Testar disponibilidade (faz requisição mínima para validar token)
	ctx := context.Background()
	if !provider.IsAvailable(ctx) {
		return fmt.Errorf("API key is invalid or Copilot service is unavailable")
	}

	return nil
}

// RegisterRoutes registra rotas do handler
func (h *AITokensHandler) RegisterRoutes(router *gin.RouterGroup) {
	tokens := router.Group("/ai/tokens")
	{
		tokens.GET("", h.GetTokens)               // GET /api/v1/ai/tokens
		tokens.POST("", h.SaveTokens)             // POST /api/v1/ai/tokens
		tokens.DELETE("", h.DeleteTokens)         // DELETE /api/v1/ai/tokens
		tokens.POST("/validate", h.ValidateToken) // POST /api/v1/ai/tokens/validate
	}
}
