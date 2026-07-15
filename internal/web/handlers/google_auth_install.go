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
	mu              sync.Mutex
	status          string // "waiting_browser" | "authenticated" | "error"
	authURL         string
	errMsg          string
	expiresAt       time.Time
	pkceVerifier    string
	redirectURI     string
	aiEmail         string
	isWIF           bool   // true quando usa auth.cloud.google (WIF)
	wifPoolProvider string // "poolID/providerID" para salvar junto ao refresh token
}

var gAuthSessions sync.Map // sessionID -> *gAuthSession

func newSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

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

// StartGoogleInstallAuth inicia o fluxo OAuth2 usando o callback do próprio app.
// Quando wif_pool_provider está preenchido (ex: "entraid-agentspace/entraid-federation-agentspace"),
// usa o endpoint WIF auth.cloud.google/authorize. Caso contrário, usa accounts.google.com.
func (h *AITokensHandler) StartGoogleInstallAuth(c *gin.Context) {
	var req struct {
		BaseURL         string `json:"base_url"`
		AIEmail         string `json:"ai_email"`
		WIFPoolProvider string `json:"wif_pool_provider"` // "poolID/providerID" para WIF
	}
	c.ShouldBindJSON(&req)

	if req.AIEmail == "" {
		req.AIEmail = c.Query("ai_email")
	}
	if req.BaseURL == "" {
		req.BaseURL = "http://localhost:8080"
	}

	sessionID := newSessionID()
	redirectURI := req.BaseURL + "/oauth/google/callback"

	var authURL, pkceVerifier string
	var err error
	isWIF := false

	// WIF flow: usa auth.cloud.google quando pool/provider estão configurados
	if req.WIFPoolProvider != "" {
		poolID, providerID, ok := ai.ParseWIFPoolProvider(req.WIFPoolProvider)
		if !ok {
			// Formato inválido (ex: URL colada em vez de "poolID/providerID") — bloquear aqui
			// em vez de cair silenciosamente no fallback de conta pessoal abaixo, que geraria
			// um refresh token que "funciona" mas nunca autoriza o Vertex AI corporativo.
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Formato inválido em 'WIF Pool / Provider': esperado 'poolID/providerID' " +
					"(ex: entraid-agentspace/entraid-federation-agentspace), não uma URL completa. " +
					"Recebido: " + req.WIFPoolProvider,
			})
			return
		}
		authURL, pkceVerifier, err = ai.StartWIFAppCallback(redirectURI, sessionID, poolID, providerID)
		isWIF = true
		log.Info().
			Str("pool", poolID).Str("provider", providerID).
			Str("redirect_uri", redirectURI).
			Msg("🔗 WIF auth.cloud.google iniciado")
	}

	// Fallback: OAuth2 padrão (accounts.google.com)
	if !isWIF {
		authURL, pkceVerifier, err = ai.StartOAuth2AppCallback(redirectURI, sessionID, req.AIEmail)
		log.Info().Str("session", sessionID).Str("redirect_uri", redirectURI).Msg("🔗 OAuth2 app-callback iniciado")
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Falha ao gerar URL de autenticação: " + err.Error(),
		})
		return
	}

	session := &gAuthSession{
		status:          "waiting_browser",
		authURL:         authURL,
		expiresAt:       time.Now().Add(15 * time.Minute),
		pkceVerifier:    pkceVerifier,
		redirectURI:     redirectURI,
		aiEmail:         req.AIEmail,
		isWIF:           isWIF,
		wifPoolProvider: req.WIFPoolProvider,
	}
	gAuthSessions.Store(sessionID, session)

	c.JSON(http.StatusOK, gin.H{
		"session_id": sessionID,
		"auth_url":   authURL,
		"status":     "waiting_browser",
		"is_wif":     isWIF,
	})
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
		c.JSON(http.StatusNotFound, gin.H{"error": "sessão não encontrada ou expirada"})
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

// SubmitGoogleAuthCode — mantido por compatibilidade, não é necessário no fluxo app-callback.
func (h *AITokensHandler) SubmitGoogleAuthCode(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "not_applicable", "message": "fluxo app-callback não requer código manual"})
}

// GoogleOAuthCallback é chamado pelo Google após o usuário autenticar.
// Rota: GET /oauth/google/callback (fora do prefixo /api/v1)
func (h *AITokensHandler) GoogleOAuthCallback(c *gin.Context) {
	code := c.Query("code")
	sessionID := c.Query("state")
	errParam := c.Query("error")

	if errParam != "" {
		renderCallbackPage(c, "❌ Autenticação cancelada", "O usuário cancelou ou negou o acesso.", false)
		if sessionID != "" {
			if val, ok := gAuthSessions.Load(sessionID); ok {
				s := val.(*gAuthSession)
				s.mu.Lock()
				s.status = "error"
				s.errMsg = "Autenticação cancelada: " + errParam
				s.mu.Unlock()
			}
		}
		return
	}

	if code == "" || sessionID == "" {
		renderCallbackPage(c, "❌ Parâmetros inválidos", "code ou state ausentes na resposta do Google.", false)
		return
	}

	val, ok := gAuthSessions.Load(sessionID)
	if !ok {
		renderCallbackPage(c, "❌ Sessão expirada", "A sessão de autenticação expirou. Inicie o processo novamente.", false)
		return
	}
	s := val.(*gAuthSession)

	s.mu.Lock()
	pkceVerifier := s.pkceVerifier
	redirectURI := s.redirectURI
	aiEmail := s.aiEmail
	isWIF := s.isWIF
	wifPoolProvider := s.wifPoolProvider
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var accessToken, refreshToken string
	var exchErr error

	if isWIF {
		// Troca de código WIF via auth.cloud.google/token
		accessToken, refreshToken, exchErr = ai.ExchangeWIFCode(ctx, code, redirectURI, pkceVerifier)
	} else {
		accessToken, refreshToken, exchErr = ai.ExchangeAuthCode(ctx, code, redirectURI, pkceVerifier)
	}

	if exchErr != nil {
		s.mu.Lock()
		s.status = "error"
		s.errMsg = "Falha ao trocar código por token: " + exchErr.Error()
		s.mu.Unlock()
		renderCallbackPage(c, "❌ Falha na autenticação", exchErr.Error(), false)
		return
	}

	// Salvar refresh_token (e wifPoolProvider se WIF)
	if aiEmail != "" {
		tokenToSave := refreshToken
		// WIF às vezes retorna apenas access_token sem refresh_token — guardar o access como fallback
		if tokenToSave == "" && accessToken != "" {
			tokenToSave = accessToken
		}
		if tokenToSave != "" {
			if err := h.saveGoogleRefreshToken(aiEmail, tokenToSave, wifPoolProvider); err != nil {
				log.Warn().Err(err).Str("ai_email", aiEmail).Msg("⚠️ Falha ao salvar refresh_token")
			} else {
				log.Info().Str("ai_email", aiEmail).Bool("is_wif", isWIF).Msg("✅ Google token salvo")
			}
		}
	}
	_ = accessToken

	s.mu.Lock()
	s.status = "authenticated"
	s.mu.Unlock()

	log.Info().Str("session", sessionID).Msg("✅ OAuth2 Google concluído via app-callback")
	renderCallbackPage(c, "✅ Autenticado com sucesso!", "Pode fechar esta aba e voltar à aplicação.", true)
}

func (h *AITokensHandler) saveGoogleRefreshToken(aiEmail, refreshToken string, wifPoolProvider ...string) error {
	existing, _ := h.tokensStore.GetTokens(aiEmail)
	if existing == nil {
		existing = &storage.UserTokens{UserEmail: aiEmail}
	}
	existing.GeminiRefreshToken = refreshToken
	existing.GeminiAuthMode = "vertex"
	// Salvar pool/provider WIF para que o refresh funcione depois
	if len(wifPoolProvider) > 0 && wifPoolProvider[0] != "" {
		existing.GeminiWifLoginURL = wifPoolProvider[0]
	}
	return h.tokensStore.SaveTokens(aiEmail, existing)
}

func renderCallbackPage(c *gin.Context, title, msg string, success bool) {
	color := "#dc2626"
	if success {
		color = "#16a34a"
	}
	html := `<!DOCTYPE html><html><head><meta charset="utf-8">
<title>` + title + `</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f9fafb;}
.card{background:#fff;border-radius:12px;padding:2rem 3rem;box-shadow:0 4px 24px rgba(0,0,0,.1);text-align:center;max-width:420px;}
h1{color:` + color + `;font-size:1.5rem;margin-bottom:.5rem;}p{color:#6b7280;}
</style></head><body><div class="card"><h1>` + title + `</h1><p>` + msg + `</p></div></body></html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}
