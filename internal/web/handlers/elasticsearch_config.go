package handlers

import (
	"context"
	"net/http"
	"time"

	esclient "k8s-hpa-manager/internal/elasticsearch"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// ElasticsearchConfigHandler gerencia credenciais Elasticsearch por usuário (HEALTHCHECK-TRIAGE-MODE-PLAN.md
// Fase 3) — mesmo padrão de internal/web/handlers/dynatrace.go (GetConfig/SaveConfig/TestConnection),
// mesma tabela user_ai_tokens, identidade via InjectUserEmail() (JWT/RBAC).
type ElasticsearchConfigHandler struct {
	tokensStore *storage.UserTokensStore
}

// NewElasticsearchConfigHandler cria o handler.
func NewElasticsearchConfigHandler(tokensStore *storage.UserTokensStore) *ElasticsearchConfigHandler {
	return &ElasticsearchConfigHandler{tokensStore: tokensStore}
}

// ─── GET /api/v1/elasticsearch/config ─────────────────────────────────────────

// GetConfig retorna a configuração atual sem expor a senha.
func (h *ElasticsearchConfigHandler) GetConfig(c *gin.Context) {
	userEmail := c.GetString("user_email")

	var url, username, indexPattern string
	hasPassword := false

	if userEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(userEmail)
		if err == nil && tokens != nil {
			url = tokens.ElasticsearchURL
			username = tokens.ElasticsearchUsername
			hasPassword = tokens.ElasticsearchPassword != ""
			indexPattern = tokens.ElasticsearchIndexPattern
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url":      url,
		"username":      username,
		"has_password":  hasPassword,
		"index_pattern": indexPattern,
		"enabled":       url != "" && username != "" && hasPassword,
	})
}

// ─── POST /api/v1/elasticsearch/config ────────────────────────────────────────

// SaveConfig salva URL/usuário/senha/index pattern do Elasticsearch para o usuário logado.
// Merge com os tokens já existentes — mesma lógica de dynatrace.go:SaveConfig.
func (h *ElasticsearchConfigHandler) SaveConfig(c *gin.Context) {
	userEmail := c.GetString("user_email")
	if userEmail == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não identificado"})
		return
	}

	var req struct {
		ElasticsearchURL          string `json:"elasticsearch_url"`
		ElasticsearchUsername     string `json:"elasticsearch_username"`
		ElasticsearchPassword     string `json:"elasticsearch_password"`
		ElasticsearchIndexPattern string `json:"elasticsearch_index_pattern"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if h.tokensStore == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tokens store não configurado"})
		return
	}

	existingTokens, err := h.tokensStore.GetTokens(userEmail)
	if err != nil {
		log.Error().Err(err).Str("user_email", userEmail).Msg("Elasticsearch SaveConfig: falha ao buscar tokens existentes")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get existing tokens"})
		return
	}
	if existingTokens == nil {
		existingTokens = &storage.UserTokens{UserEmail: userEmail}
	}

	// URL/usuário/senha só sobrescrevem se vierem não-vazios (permite salvar só o index pattern
	// sem precisar reenviar a senha a cada save); index pattern sempre sobrescreve, mesmo vazio,
	// pra permitir voltar ao default ("*") — mesma semântica de dynatrace.go:SaveConfig.
	if req.ElasticsearchURL != "" {
		existingTokens.ElasticsearchURL = req.ElasticsearchURL
	}
	if req.ElasticsearchUsername != "" {
		existingTokens.ElasticsearchUsername = req.ElasticsearchUsername
	}
	if req.ElasticsearchPassword != "" {
		existingTokens.ElasticsearchPassword = req.ElasticsearchPassword
	}
	existingTokens.ElasticsearchIndexPattern = req.ElasticsearchIndexPattern

	if existingTokens.PreferredProvider == "" {
		existingTokens.PreferredProvider = "ollama"
	}

	if err := h.tokensStore.SaveTokens(userEmail, existingTokens); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save elasticsearch config"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url":      existingTokens.ElasticsearchURL,
		"username":      existingTokens.ElasticsearchUsername,
		"has_password":  existingTokens.ElasticsearchPassword != "",
		"index_pattern": existingTokens.ElasticsearchIndexPattern,
		"enabled":       existingTokens.ElasticsearchURL != "" && existingTokens.ElasticsearchUsername != "" && existingTokens.ElasticsearchPassword != "",
	})
}

// ─── POST /api/v1/elasticsearch/test ──────────────────────────────────────────

// TestConnection testa conectividade/autenticação com o Elasticsearch do usuário logado — GET /
// (endpoint raiz, barato, não depende de nenhuma convenção de campo/índice).
func (h *ElasticsearchConfigHandler) TestConnection(c *gin.Context) {
	userEmail := c.GetString("user_email")

	var url, username, password string
	if userEmail != "" && h.tokensStore != nil {
		tokens, err := h.tokensStore.GetTokens(userEmail)
		if err == nil && tokens != nil {
			url = tokens.ElasticsearchURL
			username = tokens.ElasticsearchUsername
			password = tokens.ElasticsearchPassword
		}
	}

	if url == "" || username == "" || password == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "URL, usuário e senha precisam estar configurados"})
		return
	}

	client := esclient.NewClient(url, username, password, "")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	latency, err := client.TestConnection(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"latency_ms": latency,
		"base_url":   client.BaseURL(),
	})
}
