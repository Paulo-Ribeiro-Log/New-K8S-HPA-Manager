package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"k8s-hpa-manager/internal/ai"
	"k8s-hpa-manager/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// ─────────────────────────────────────────────────────────────────────────────
// Sessão de autenticação OAuth2 (em memória, TTL de 15 min)
// ─────────────────────────────────────────────────────────────────────────────

type gAuthSession struct {
	mu        sync.Mutex
	status    string // "waiting_browser" | "authenticated" | "error"
	authURL   string
	errMsg    string
	expiresAt time.Time
}

var gAuthSessions sync.Map // sessionID -> *gAuthSession

func newSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// cleanExpiredSessions remove sessões antigas (chamada periodicamente)
func init() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			gAuthSessions.Range(func(k, v any) bool {
				s := v.(*gAuthSession)
				s.mu.Lock()
				expired := time.Now().After(s.expiresAt)
				s.mu.Unlock()
				if expired {
					gAuthSessions.Delete(k)
				}
				return true
			})
		}
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// Handlers
// ─────────────────────────────────────────────────────────────────────────────

// StartGoogleInstallAuth inicia o fluxo OAuth2 Authorization Code com servidor loopback.
// Retorna session_id e auth_url para o frontend exibir ao usuário.
func (h *AITokensHandler) StartGoogleInstallAuth(c *gin.Context) {
	sessionID := newSessionID()
	session := &gAuthSession{
		status:    "waiting_browser",
		expiresAt: time.Now().Add(15 * time.Minute),
	}
	gAuthSessions.Store(sessionID, session)

	// Iniciar fluxo OAuth2 em goroutine
	go h.runOAuth2Flow(sessionID, session, c.Query("ai_email"))

	// Aguardar até a auth_url estar disponível (max 3s)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		url := session.authURL
		session.mu.Unlock()
		if url != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	session.mu.Lock()
	resp := gin.H{
		"session_id": sessionID,
		"auth_url":   session.authURL,
		"status":     session.status,
	}
	session.mu.Unlock()

	c.JSON(http.StatusOK, resp)
}

// GetGoogleAuthStatus retorna o status atual da sessão de autenticação.
func (h *AITokensHandler) GetGoogleAuthStatus(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id obrigatório"})
		return
	}

	val, ok := gAuthSessions.Load(sessionID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "sessão não encontrada"})
		return
	}
	s := val.(*gAuthSession)
	s.mu.Lock()
	defer s.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"status":   s.status,
		"auth_url": s.authURL,
		"error":    s.errMsg,
	})
}

// SubmitGoogleAuthCode não é mais necessário com o fluxo loopback (mantido por compatibilidade).
func (h *AITokensHandler) SubmitGoogleAuthCode(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "not_applicable", "message": "fluxo loopback não requer código manual"})
}

// ─────────────────────────────────────────────────────────────────────────────
// Fluxo OAuth2 (goroutine)
// ─────────────────────────────────────────────────────────────────────────────

func (h *AITokensHandler) runOAuth2Flow(sessionID string, s *gAuthSession, aiEmail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// Iniciar servidor loopback + gerar auth URL
	oauthSession, err := ai.StartOAuth2LoopbackFlow(ctx)
	if err != nil {
		s.mu.Lock()
		s.status = "error"
		s.errMsg = "Erro ao iniciar fluxo OAuth2: " + err.Error()
		s.mu.Unlock()
		log.Error().Err(err).Msg("❌ Falha ao iniciar OAuth2 loopback")
		return
	}

	s.mu.Lock()
	s.authURL = oauthSession.AuthURL
	s.mu.Unlock()

	log.Info().Str("session", sessionID).Msg("🔗 OAuth2 loopback iniciado, aguardando autenticação do usuário")

	// Aguardar resultado (bloqueante até autenticar, erro ou timeout)
	result := <-oauthSession.ResultChan

	if result.Error != nil {
		s.mu.Lock()
		s.status = "error"
		s.errMsg = result.Error.Error()
		s.mu.Unlock()
		log.Error().Err(result.Error).Msg("❌ OAuth2 autenticação falhou")
		return
	}

	// Autenticado! Salvar refresh_token se email fornecido
	s.mu.Lock()
	s.status = "authenticated"
	s.mu.Unlock()

	if aiEmail != "" && result.RefreshToken != "" {
		if err := h.saveGoogleRefreshToken(aiEmail, result.RefreshToken); err != nil {
			log.Warn().Err(err).Str("ai_email", aiEmail).Msg("⚠️ Falha ao salvar refresh_token")
		} else {
			log.Info().Str("ai_email", aiEmail).Msg("✅ Google refresh_token salvo no banco")
		}
	}

	log.Info().Str("session", sessionID).Msg("✅ OAuth2 autenticação Google concluída")
}

func (h *AITokensHandler) saveGoogleRefreshToken(aiEmail, refreshToken string) error {
	existing, _ := h.tokensStore.GetTokens(aiEmail)
	if existing == nil {
		existing = &storage.UserTokens{UserEmail: aiEmail}
	}
	existing.GeminiRefreshToken = refreshToken
	existing.GeminiAuthMode = "vertex"
	return h.tokensStore.SaveTokens(aiEmail, existing)
}
