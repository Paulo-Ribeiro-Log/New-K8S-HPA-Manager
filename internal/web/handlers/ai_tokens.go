package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// AITokensHandler gerencia tokens AI dos usuários
type AITokensHandler struct {
	tokensStore        *storage.UserTokensStore
	localSettingsStore *storage.LocalSettingsStore
}

// NewAITokensHandler cria nova instância
func NewAITokensHandler(tokensStore *storage.UserTokensStore, localSettingsStore *storage.LocalSettingsStore) *AITokensHandler {
	return &AITokensHandler{
		tokensStore:        tokensStore,
		localSettingsStore: localSettingsStore,
	}
}

// SaveTokensRequest request para salvar tokens
type SaveTokensRequest struct {
	AIEmail                  string `json:"ai_email"` // Email para identificar configurações (independente do Azure AD)
	GeminiAPIKey             string `json:"gemini_api_key,omitempty"`
	GeminiModel              string `json:"gemini_model,omitempty"`
	GeminiAuthMode           string `json:"gemini_auth_mode,omitempty"`            // "apikey" ou "vertex"
	GeminiVertexProject      string `json:"gemini_vertex_project,omitempty"`       // projeto GCP para Vertex AI
	GeminiVertexLocation     string `json:"gemini_vertex_location,omitempty"`      // região GCP (ex: us-central1)
	GeminiServiceAccountJSON string `json:"gemini_service_account_json,omitempty"` // JSON do service account GCP
	GeminiWifLoginURL        string `json:"gemini_wif_login_url,omitempty"`         // URL de login SSO corporativo (WIF)
	OpenAIAPIKey             string `json:"openai_api_key,omitempty"`
	OpenAIModel              string `json:"openai_model,omitempty"`
	ClaudeAPIKey             string `json:"claude_api_key,omitempty"`
	ClaudeModel              string `json:"claude_model,omitempty"`
	CopilotAPIKey            string `json:"copilot_api_key,omitempty"`
	CopilotEndpoint          string `json:"copilot_endpoint,omitempty"`
	CopilotDeployment        string `json:"copilot_deployment,omitempty"`
	OllamaModel              string `json:"ollama_model,omitempty"`
	PreferredProvider        string `json:"preferred_provider"`
	DynatraceURL             string `json:"dynatrace_url,omitempty"`
	DynatraceToken           string `json:"dynatrace_token,omitempty"`
	DynatraceTagFilter       string `json:"dynatrace_tag_filter,omitempty"`
}

// TokensResponse response com tokens (sem expor valores completos)
type TokensResponse struct {
	AIEmail                 string `json:"ai_email,omitempty"` // Email usado para identificar configurações
	HasGemini               bool   `json:"has_gemini"`
	GeminiModel             string `json:"gemini_model,omitempty"`
	GeminiAuthMode          string `json:"gemini_auth_mode,omitempty"`       // "apikey" ou "vertex"
	GeminiVertexProject     string `json:"gemini_vertex_project,omitempty"`  // não sensível - é o ID do projeto
	GeminiVertexLocation    string `json:"gemini_vertex_location,omitempty"` // região GCP
	HasGeminiServiceAccount bool   `json:"has_gemini_service_account"`       // true se service account JSON configurado
	HasGeminiRefreshToken   bool   `json:"has_gemini_refresh_token"`         // true se autenticado via Device Auth Google
	GeminiWifLoginURL       string `json:"gemini_wif_login_url,omitempty"`  // URL de login SSO corporativo (WIF)
	HasOpenAI               bool   `json:"has_openai"`
	OpenAIModel             string `json:"openai_model,omitempty"`
	HasClaude               bool   `json:"has_claude"`
	ClaudeModel             string `json:"claude_model,omitempty"`
	HasCopilot              bool   `json:"has_copilot"`
	CopilotEndpoint         string `json:"copilot_endpoint,omitempty"`
	CopilotDeployment       string `json:"copilot_deployment,omitempty"`
	OllamaModel             string `json:"ollama_model,omitempty"`
	PreferredProvider       string `json:"preferred_provider"`
	DynatraceURL            string `json:"dynatrace_url,omitempty"`
	DynatraceTagFilter      string `json:"dynatrace_tag_filter,omitempty"`
	HasDynatrace            bool   `json:"has_dynatrace"`
	UpdatedAt               string `json:"updated_at,omitempty"`
}

// SaveTokens salva tokens do usuário
func (h *AITokensHandler) SaveTokens(c *gin.Context) {
	var req SaveTokensRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	// Validar que ai_email foi fornecido
	if req.AIEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ai_email is required",
		})
		return
	}

	// Usar ai_email do request (independente do Azure AD)
	userEmailStr := req.AIEmail

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

	// Validar Gemini API key apenas quando NÃO estiver usando Vertex AI (OAuth2).
	// No modo Vertex, a autenticação é via refresh token — não existe API key "AIza...".
	if req.GeminiAPIKey != "" && req.GeminiAuthMode != "vertex" {
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
		// Log detalhado de erros de validação
		log.Warn().
			Str("user_email", userEmailStr).
			Interface("validation_errors", validationErrors).
			Msg("❌ Token validation failed")

		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "token validation failed",
			"validation_errors": validationErrors,
		})
		return
	}

	// Log: tokens validados com sucesso
	log.Info().
		Str("user_email", userEmailStr).
		Str("preferred_provider", req.PreferredProvider).
		Bool("has_gemini", req.GeminiAPIKey != "").
		Bool("has_claude", req.ClaudeAPIKey != "").
		Bool("has_openai", req.OpenAIAPIKey != "").
		Bool("has_copilot", req.CopilotAPIKey != "").
		Msg("✅ Tokens validated successfully - saving to database")

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
	// Gemini Vertex AI (SSO)
	if req.GeminiAuthMode != "" {
		tokens.GeminiAuthMode = req.GeminiAuthMode
	} else {
		tokens.GeminiAuthMode = existingTokens.GeminiAuthMode
	}
	if req.GeminiVertexProject != "" {
		tokens.GeminiVertexProject = req.GeminiVertexProject
	} else {
		tokens.GeminiVertexProject = existingTokens.GeminiVertexProject
	}
	if req.GeminiVertexLocation != "" {
		tokens.GeminiVertexLocation = req.GeminiVertexLocation
	} else {
		tokens.GeminiVertexLocation = existingTokens.GeminiVertexLocation
	}
	if req.GeminiServiceAccountJSON != "" {
		tokens.GeminiServiceAccountJSON = req.GeminiServiceAccountJSON
	} else {
		tokens.GeminiServiceAccountJSON = existingTokens.GeminiServiceAccountJSON
	}
	// WIF Login URL — campo informativo, pode ser limpo enviando string vazia
	tokens.GeminiWifLoginURL = req.GeminiWifLoginURL
	if req.GeminiWifLoginURL == "" {
		tokens.GeminiWifLoginURL = existingTokens.GeminiWifLoginURL
	}
	// Refresh token OAuth2 nunca vem no request — sempre preservar o existente
	tokens.GeminiRefreshToken = existingTokens.GeminiRefreshToken

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

	// Dynatrace
	if req.DynatraceURL != "" {
		tokens.DynatraceURL = req.DynatraceURL
	} else {
		tokens.DynatraceURL = existingTokens.DynatraceURL
	}
	if req.DynatraceToken != "" {
		tokens.DynatraceToken = req.DynatraceToken
	} else {
		tokens.DynatraceToken = existingTokens.DynatraceToken
	}
	// Tag filter: sempre salva o valor enviado (campo não sensível, pode ser limpo)
	tokens.DynatraceTagFilter = req.DynatraceTagFilter

	if err := h.tokensStore.SaveTokens(userEmailStr, tokens); err != nil {
		log.Error().
			Err(err).
			Str("user_email", userEmailStr).
			Msg("❌ Failed to save tokens to database")

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to save tokens",
		})
		return
	}

	// Salvar último email usado no local_settings para persistência
	if h.localSettingsStore != nil {
		if err := h.localSettingsStore.SetLastAIEmail(userEmailStr); err != nil {
			log.Warn().
				Err(err).
				Str("user_email", userEmailStr).
				Msg("⚠️ Failed to save last_ai_email to local_settings (non-critical)")
		} else {
			log.Info().
				Str("user_email", userEmailStr).
				Msg("✅ last_ai_email saved to local_settings")
		}
	}

	log.Info().
		Str("user_email", userEmailStr).
		Str("preferred_provider", tokens.PreferredProvider).
		Bool("has_gemini", tokens.GeminiAPIKey != "").
		Bool("has_claude", tokens.ClaudeAPIKey != "").
		Bool("has_openai", tokens.OpenAIAPIKey != "").
		Bool("has_copilot", tokens.CopilotAPIKey != "").
		Msg("✅ Tokens saved successfully to database")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "tokens saved successfully",
	})
}

// GetTokens retorna status dos tokens (sem expor valores)
func (h *AITokensHandler) GetTokens(c *gin.Context) {
	// Aceitar ai_email como query parameter
	aiEmail := c.Query("ai_email")

	// Se não foi fornecido via query, buscar do local_settings (persistência local)
	if aiEmail == "" && h.localSettingsStore != nil {
		lastEmail, err := h.localSettingsStore.GetLastAIEmail()
		if err != nil {
			log.Warn().
				Err(err).
				Msg("⚠️ Failed to get last_ai_email from local_settings")
		} else if lastEmail != "" {
			aiEmail = lastEmail
			log.Info().
				Str("ai_email", aiEmail).
				Msg("📧 Using last_ai_email from local_settings")
		}
	}

	// Se ainda não temos ai_email, retornar resposta vazia (primeiro uso)
	if aiEmail == "" {
		log.Info().Msg("ℹ️ No ai_email found - returning default response (first time setup)")
		c.JSON(http.StatusOK, TokensResponse{
			HasGemini:         false,
			HasOpenAI:         false,
			HasClaude:         false,
			HasCopilot:        false,
			HasDynatrace:      false,
			PreferredProvider: "ollama",
		})
		return
	}

	tokens, err := h.tokensStore.GetTokens(aiEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to get tokens",
		})
		return
	}

	// Se não tem tokens configurados, retornar vazio
	if tokens == nil {
		c.JSON(http.StatusOK, TokensResponse{
			AIEmail:           aiEmail,
			HasGemini:         false,
			HasOpenAI:         false,
			HasClaude:         false,
			HasCopilot:        false,
			HasDynatrace:      false,
			PreferredProvider: "ollama",
		})
		return
	}

	hasGeminiServiceAccount := tokens.GeminiServiceAccountJSON != ""
	hasGeminiRefreshToken := tokens.GeminiRefreshToken != ""
	hasGemini := tokens.GeminiAPIKey != "" || (tokens.GeminiAuthMode == "vertex" && tokens.GeminiVertexProject != "") || hasGeminiRefreshToken
	hasDynatrace := tokens.DynatraceURL != "" || tokens.DynatraceToken != ""

	// Retornar apenas status (não expor tokens completos)
	c.JSON(http.StatusOK, TokensResponse{
		AIEmail:                 tokens.UserEmail,
		HasGemini:               hasGemini,
		GeminiModel:             tokens.GeminiModel,
		GeminiAuthMode:          tokens.GeminiAuthMode,
		GeminiVertexProject:     tokens.GeminiVertexProject,
		GeminiVertexLocation:    tokens.GeminiVertexLocation,
		HasGeminiServiceAccount: hasGeminiServiceAccount,
		HasGeminiRefreshToken:   hasGeminiRefreshToken,
		GeminiWifLoginURL:       tokens.GeminiWifLoginURL,
		HasOpenAI:               tokens.OpenAIAPIKey != "",
		OpenAIModel:             tokens.OpenAIModel,
		HasClaude:               tokens.ClaudeAPIKey != "",
		ClaudeModel:             tokens.ClaudeModel,
		HasCopilot:              tokens.CopilotAPIKey != "",
		CopilotEndpoint:         tokens.CopilotEndpoint,
		CopilotDeployment:       tokens.CopilotDeployment,
		OllamaModel:             tokens.OllamaModel,
		PreferredProvider:       tokens.PreferredProvider,
		DynatraceURL:            tokens.DynatraceURL,
		DynatraceTagFilter:      tokens.DynatraceTagFilter,
		HasDynatrace:            hasDynatrace,
		UpdatedAt:               tokens.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// DeleteTokens remove tokens do usuário
func (h *AITokensHandler) DeleteTokens(c *gin.Context) {
	// Obter ai_email via query parameter
	aiEmail := c.Query("ai_email")
	if aiEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ai_email query parameter is required",
		})
		return
	}

	if err := h.tokensStore.DeleteTokens(aiEmail); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to delete tokens",
		})
		return
	}

	// Remover também do local_settings se for o último email usado
	if h.localSettingsStore != nil {
		lastEmail, _ := h.localSettingsStore.GetLastAIEmail()
		if lastEmail == aiEmail {
			if err := h.localSettingsStore.Delete(storage.SettingLastAIEmail); err != nil {
				log.Warn().
					Err(err).
					Msg("⚠️ Failed to delete last_ai_email from local_settings")
			} else {
				log.Info().Msg("✅ last_ai_email removed from local_settings")
			}
		}
	}

	log.Info().
		Str("ai_email", aiEmail).
		Msg("✅ AI tokens deleted successfully")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "tokens deleted successfully",
	})
}

// ValidateToken valida um token específico sem salvar
func (h *AITokensHandler) ValidateToken(c *gin.Context) {
	var req struct {
		Provider           string `json:"provider"`
		APIKey             string `json:"api_key"`
		Endpoint           string `json:"endpoint,omitempty"`
		Deployment         string `json:"deployment,omitempty"`
		VertexProject      string `json:"vertex_project,omitempty"`
		VertexLocation     string `json:"vertex_location,omitempty"`
		ServiceAccountJSON string `json:"service_account_json,omitempty"`
		AIEmail            string `json:"ai_email,omitempty"` // para buscar refresh token do DB
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
	case "gemini-vertex":
		// Buscar refresh token e wifPoolProvider armazenados
		refreshToken := ""
		wifPoolProvider := ""
		if req.AIEmail != "" && req.ServiceAccountJSON == "" {
			if tokens, err := h.tokensStore.GetTokens(req.AIEmail); err == nil && tokens != nil {
				refreshToken = tokens.GeminiRefreshToken
				wifPoolProvider = tokens.GeminiWifLoginURL
			}
		}
		validationErr = validateGeminiVertexConnection(req.VertexProject, req.ServiceAccountJSON, refreshToken, wifPoolProvider)
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

// validateGeminiVertexConnection testa autenticação para Vertex AI.
// Prioridade: WIF refresh → OAuth2 refresh → serviceAccountJSON → ADC file (gcloud)
func validateGeminiVertexConnection(project, serviceAccountJSON, refreshToken, wifPoolProvider string) error {
	if project == "" {
		return fmt.Errorf("projeto GCP é obrigatório para Vertex AI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := ai.GetVertexAccessToken(ctx, serviceAccountJSON, refreshToken, wifPoolProvider)
	return err
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

	// ⚠️ IMPORTANTE: NÃO fazer chamadas à API para validar!
	// Chaves OpenAI/Azure OpenAI têm formatos variados (sk-, sk-proj-, UUID, etc.)
	// Validar apenas comprimento mínimo — não restringir por prefixo
	if len(apiKey) < 20 {
		return fmt.Errorf("API key is too short (minimum 20 characters)")
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

// GetAvailableModels retorna modelos disponíveis por provider.
// Para Gemini, aceita query param `mode=vertex` para retornar modelos do Vertex AI
// (diferentes dos modelos do AI Studio / API Key).
func (h *AITokensHandler) GetAvailableModels(c *gin.Context) {
	provider := c.Query("provider")
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "provider query parameter is required",
		})
		return
	}

	mode := c.Query("mode") // "vertex" ou "" (apikey)

	var models []ModelInfo

	switch provider {
	case "gemini":
		if mode == "vertex" {
			// Modelos disponíveis no Vertex AI (aiplatform.googleapis.com)
			// Nomes devem ser os IDs exatos aceitos pelo endpoint Vertex AI
			models = []ModelInfo{
				{ID: "gemini-2.0-flash-001", Name: "Gemini 2.0 Flash", Description: "Estável e rápido — disponível em todas as regiões Vertex AI (recomendado)", IsDefault: true},
				{ID: "gemini-2.0-flash-lite-001", Name: "Gemini 2.0 Flash Lite", Description: "Mais econômico e leve"},
				{ID: "gemini-1.5-flash-002", Name: "Gemini 1.5 Flash", Description: "Geração anterior — estável e amplamente disponível"},
				{ID: "gemini-1.5-pro-002", Name: "Gemini 1.5 Pro", Description: "Geração anterior — mais robusto, contexto longo"},
				{ID: "gemini-2.5-flash-preview-05-20", Name: "Gemini 2.5 Flash (Preview)", Description: "Preview — pode não estar disponível em todos os projetos"},
			}
		} else {
			// Modelos do AI Studio (generativelanguage.googleapis.com) — modo API Key
			models = []ModelInfo{
				{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Description: "Mais recente — Free Tier: 15 RPM / 1M tokens/dia (recomendado)", IsDefault: true},
				{ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", Description: "Versão anterior estável"},
				{ID: "gemini-1.5-flash", Name: "Gemini 1.5 Flash", Description: "Geração anterior — muito estável"},
				{ID: "gemini-1.5-pro", Name: "Gemini 1.5 Pro", Description: "Geração anterior — robusto, contexto 1M tokens"},
			}
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

// StartGoogleDeviceAuth inicia o Device Authorization Grant do Google.
// Retorna user_code e verification_url para o usuário autenticar no browser.
func (h *AITokensHandler) StartGoogleDeviceAuth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	deviceAuth, err := ai.StartDeviceAuth(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Falha ao iniciar autenticação Google: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device_code":      deviceAuth.DeviceCode,
		"user_code":        deviceAuth.UserCode,
		"verification_url": deviceAuth.VerificationURL,
		"expires_in":       deviceAuth.ExpiresIn,
		"interval":         deviceAuth.Interval,
	})
}

// PollGoogleDeviceAuth faz uma tentativa de trocar o device_code por token.
// O frontend chama este endpoint periodicamente até receber sucesso.
func (h *AITokensHandler) PollGoogleDeviceAuth(c *gin.Context) {
	var req struct {
		DeviceCode string `json:"device_code"`
		AIEmail    string `json:"ai_email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_code é obrigatório"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Uma tentativa apenas — o frontend faz o loop de polling
	token, pending, err := ai.TryExchangeDeviceToken(ctx, req.DeviceCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if pending {
		c.JSON(http.StatusOK, gin.H{"status": "pending"})
		return
	}

	// Autenticado! Salvar refresh_token no banco se ai_email fornecido
	if req.AIEmail != "" && token.RefreshToken != "" {
		existing, _ := h.tokensStore.GetTokens(req.AIEmail)
		if existing == nil {
			existing = &storage.UserTokens{UserEmail: req.AIEmail}
		}
		existing.GeminiRefreshToken = token.RefreshToken
		existing.GeminiAuthMode = "vertex"
		if err := h.tokensStore.SaveTokens(req.AIEmail, existing); err != nil {
			log.Warn().Err(err).Str("ai_email", req.AIEmail).Msg("⚠️ Falha ao salvar refresh_token")
		} else {
			log.Info().Str("ai_email", req.AIEmail).Msg("✅ Google refresh_token salvo")
		}

		// Gravar ADC para que o servidor use as mesmas credenciais sem precisar do gcloud
		if err := ai.WriteADCFile(token.RefreshToken); err != nil {
			log.Warn().Err(err).Msg("⚠️ Falha ao gravar ADC — servidor usará apenas token do usuário")
		} else {
			log.Info().Msg("✅ ADC gravado — servidor pode usar credenciais Google sem gcloud")
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "authenticated",
		"access_token": token.AccessToken,
	})
}

// RegisterRoutes registra rotas do handler
func (h *AITokensHandler) RegisterRoutes(router *gin.RouterGroup, rbacMiddleware interface{ InjectUserEmail() gin.HandlerFunc }) {
	// Rotas de tokens AI - NÃO usam middleware InjectUserEmail
	// O email é fornecido diretamente no request (ai_email), independente do Azure AD
	tokens := router.Group("/ai/tokens")
	{
		tokens.GET("", h.GetTokens)                                         // GET /api/v1/ai/tokens?ai_email=...
		tokens.POST("", h.SaveTokens)                                       // POST /api/v1/ai/tokens (body: {ai_email, ...})
		tokens.DELETE("", h.DeleteTokens)                                   // DELETE /api/v1/ai/tokens?ai_email=...
		tokens.POST("/validate", h.ValidateToken)                           // POST /api/v1/ai/tokens/validate
		tokens.POST("/google-auth/start", h.StartGoogleDeviceAuth)          // Device Authorization Grant (legado)
		tokens.POST("/google-auth/poll", h.PollGoogleDeviceAuth)            // Polling de token (legado)
		tokens.POST("/google-auth/install/start", h.StartGoogleInstallAuth) // Instala gcloud + inicia auth
		tokens.GET("/google-auth/install/status", h.GetGoogleAuthStatus)    // Polling de status da sessão
		tokens.POST("/google-auth/install/code", h.SubmitGoogleAuthCode)    // Submete código de verificação
	}

	// Endpoint de modelos disponíveis (não precisa de ai_email)
	router.GET("/ai/models", h.GetAvailableModels) // GET /api/v1/ai/models?provider=gemini
}
