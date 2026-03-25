package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v57/github"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"

	"k8s-hpa-manager/internal/storage"
)

// GitHubTokensHandler gerencia tokens GitHub individuais dos usuários
type GitHubTokensHandler struct {
	tokenStore *storage.GitHubTokenStore
	logger     *zerolog.Logger
}

// NewGitHubTokensHandler cria novo handler de tokens
func NewGitHubTokensHandler(store *storage.GitHubTokenStore, logger *zerolog.Logger) *GitHubTokensHandler {
	return &GitHubTokensHandler{
		tokenStore: store,
		logger:     logger,
	}
}

// TokenStatusResponse representa resposta de status do token
type TokenStatusResponse struct {
	Valid      bool   `json:"valid"`
	Username   string `json:"username,omitempty"`
	Email      string `json:"email,omitempty"`
	Remaining  int    `json:"remaining,omitempty"` // Rate limit remaining
	Limit      int    `json:"limit,omitempty"`     // Rate limit total
	ResetAt    string `json:"reset_at,omitempty"`  // Timestamp do reset
	Configured bool   `json:"configured"`          // Token está configurado?
	Error      string `json:"error,omitempty"`     // Mensagem de erro
}

// GetTokenStatus valida token atual do usuário
// GET /api/v1/github/token/status
func (h *GitHubTokensHandler) GetTokenStatus(c *gin.Context) {
	// Prioridade: 1) contexto RBAC (Azure AD), 2) header X-GitHub-Email (fallback)
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		userEmail = strings.TrimSpace(c.GetHeader("X-GitHub-Email"))
	}
	if userEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Identidade do usuário não disponível. Faça login novamente.",
		})
		return
	}

	// Verificar se token está configurado
	if !h.tokenStore.HasToken(userEmail) {
		c.JSON(http.StatusOK, TokenStatusResponse{
			Valid:      false,
			Configured: false,
			Error:      "Token não configurado para este usuário",
		})
		return
	}

	// Obter token
	token, err := h.tokenStore.GetToken(userEmail)
	if err != nil {
		h.logger.Error().Err(err).Str("user", userEmail).Msg("Failed to get token")
		c.JSON(http.StatusInternalServerError, TokenStatusResponse{
			Valid:      false,
			Configured: true,
			Error:      "Falha ao obter token do armazenamento",
		})
		return
	}

	// Validar token com GitHub API
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	client := github.NewClient(tc)

	// Buscar informações do usuário
	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil {
		h.logger.Warn().Err(err).Str("user", userEmail).Msg("Token validation failed")
		c.JSON(http.StatusOK, TokenStatusResponse{
			Valid:      false,
			Configured: true,
			Error:      fmt.Sprintf("Token inválido: %v", err),
		})
		return
	}

	// Buscar rate limit
	rateLimits, _, err := client.RateLimit.Get(context.Background())
	if err != nil {
		h.logger.Warn().Err(err).Str("user", userEmail).Msg("Failed to get rate limit")
		// Continua sem rate limit (não é crítico)
	}

	response := TokenStatusResponse{
		Valid:      true,
		Username:   user.GetLogin(),
		Email:      user.GetEmail(),
		Configured: true,
	}

	// Adicionar rate limit se disponível
	if rateLimits != nil && rateLimits.Core != nil {
		response.Remaining = rateLimits.Core.Remaining
		response.Limit = rateLimits.Core.Limit
		response.ResetAt = rateLimits.Core.Reset.String()
	}

	c.JSON(http.StatusOK, response)
}

// SaveTokenRequest representa request para salvar token
type SaveTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

// SaveToken salva token do usuário (com validação)
// POST /api/v1/github/token
func (h *GitHubTokensHandler) SaveToken(c *gin.Context) {
	// Parse request
	var req SaveTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Token é obrigatório",
		})
		return
	}

	// Prioridade: 1) contexto RBAC (Azure AD), 2) header X-GitHub-Email (fallback)
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		userEmail = strings.TrimSpace(c.GetHeader("X-GitHub-Email"))
	}
	if userEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Identidade do usuário não disponível. Faça login novamente.",
		})
		return
	}

	// Limpar token (remover espaços)
	token := strings.TrimSpace(req.Token)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Token não pode estar vazio",
		})
		return
	}

	// Validar formato: Classic PATs (ghp_/gho_/ghu_/ghs_/ghr_) e Fine-grained PATs (github_pat_)
	if !strings.HasPrefix(token, "ghp_") &&
		!strings.HasPrefix(token, "gho_") &&
		!strings.HasPrefix(token, "ghu_") &&
		!strings.HasPrefix(token, "ghs_") &&
		!strings.HasPrefix(token, "ghr_") &&
		!strings.HasPrefix(token, "github_pat_") {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Formato inválido. Use Classic PAT (ghp_...) ou Fine-grained PAT (github_pat_...)",
		})
		return
	}

	// Validar token com GitHub API ANTES de salvar
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	client := github.NewClient(tc)

	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil {
		h.logger.Warn().Err(err).Str("user", userEmail).Msg("Token validation failed during save")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Token inválido: %v", err),
		})
		return
	}

	// Salvar token criptografado
	if err := h.tokenStore.SaveToken(userEmail, token); err != nil {
		h.logger.Error().Err(err).Str("user", userEmail).Msg("Failed to save token")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Falha ao salvar token",
		})
		return
	}

	h.logger.Info().
		Str("user", userEmail).
		Str("github_user", user.GetLogin()).
		Msg("GitHub token saved successfully")

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"message":      "Token salvo com sucesso",
		"github_user":  user.GetLogin(),
		"github_email": user.GetEmail(),
	})
}

// DeleteToken remove token do usuário
// DELETE /api/v1/github/token
func (h *GitHubTokensHandler) DeleteToken(c *gin.Context) {
	// Prioridade: 1) contexto RBAC (Azure AD), 2) header X-GitHub-Email (fallback)
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		userEmail = strings.TrimSpace(c.GetHeader("X-GitHub-Email"))
	}
	if userEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Identidade do usuário não disponível. Faça login novamente.",
		})
		return
	}

	// Deletar token
	if err := h.tokenStore.DeleteToken(userEmail); err != nil {
		if strings.Contains(err.Error(), "não encontrado") {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Token não encontrado para este usuário",
			})
			return
		}

		h.logger.Error().Err(err).Str("user", userEmail).Msg("Failed to delete token")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Falha ao deletar token",
		})
		return
	}

	h.logger.Info().Str("user", userEmail).Msg("GitHub token deleted successfully")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Token removido com sucesso",
	})
}
