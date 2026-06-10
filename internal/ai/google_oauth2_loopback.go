package ai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	googleAuthURL    = "https://accounts.google.com/o/oauth2/auth"
	googleOAuth2TokenURL = "https://oauth2.googleapis.com/token"
	googleOAuth2Scope    = "https://www.googleapis.com/auth/cloud-platform"

	// gcloud SDK public client (installed app / native app)
	// São credenciais públicas — o client_secret não é confidencial para apps nativas.
	loopbackClientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	loopbackClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"
)

// OAuth2Result resultado do fluxo OAuth2
type OAuth2Result struct {
	AccessToken  string
	RefreshToken string
	Error        error
}

// OAuth2LoopbackSession sessão do fluxo OAuth2 com servidor loopback
type OAuth2LoopbackSession struct {
	AuthURL     string
	ResultChan  chan OAuth2Result
	cancel      context.CancelFunc
}

// StartOAuth2LoopbackFlow inicia o fluxo OAuth2 Authorization Code com servidor loopback.
// Retorna a URL de autenticação e um canal que receberá o resultado quando o usuário autenticar.
func StartOAuth2LoopbackFlow(ctx context.Context) (*OAuth2LoopbackSession, error) {
	// Encontrar porta livre
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir porta local: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d", port)

	// PKCE: code_verifier e code_challenge
	verifier, challenge, err := generatePKCE()
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("erro ao gerar PKCE: %w", err)
	}

	// State anti-CSRF
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Construir auth URL
	params := url.Values{
		"client_id":             {loopbackClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {googleOAuth2Scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	authURL := googleAuthURL + "?" + params.Encode()

	resultChan := make(chan OAuth2Result, 1)
	flowCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)

	session := &OAuth2LoopbackSession{
		AuthURL:    authURL,
		ResultChan: resultChan,
		cancel:     cancel,
	}

	// Servidor local para receber o callback
	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Ignorar requisições que não têm code (ex: favicon)
		code := r.URL.Query().Get("code")
		if code == "" {
			http.NotFound(w, r)
			return
		}

		// Exibir página de sucesso/loading no browser do usuário
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Trocar code por tokens
		token, err := exchangeAuthCode(flowCtx, code, redirectURI, verifier)
		if err != nil {
			fmt.Fprint(w, successHTML("❌ Falha na autenticação", "Erro: "+err.Error(), false))
			resultChan <- OAuth2Result{Error: err}
		} else {
			fmt.Fprint(w, successHTML("✅ Autenticado com sucesso!", "Você pode fechar esta aba e voltar à aplicação.", true))
			resultChan <- OAuth2Result{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}
		}

		// Encerrar servidor após um pequeno delay (dar tempo ao browser de renderizar)
		go func() {
			time.Sleep(500 * time.Millisecond)
			srv.Shutdown(context.Background())
		}()
	})

	// Iniciar servidor em goroutine
	go func() {
		defer listener.Close()
		defer cancel()

		// Aguardar cancelamento do contexto para encerrar o servidor
		go func() {
			<-flowCtx.Done()
			srv.Shutdown(context.Background())
			if flowCtx.Err() == context.DeadlineExceeded {
				select {
				case resultChan <- OAuth2Result{Error: fmt.Errorf("tempo esgotado aguardando autenticação (10 minutos)")}:
				default:
				}
			}
		}()

		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case resultChan <- OAuth2Result{Error: fmt.Errorf("erro no servidor local: %w", err)}:
			default:
			}
		}
	}()

	return session, nil
}

// exchangeAuthCode troca o authorization code por access_token + refresh_token
func exchangeAuthCode(ctx context.Context, code, redirectURI, codeVerifier string) (*struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}, error) {
	body := url.Values{
		"client_id":     {loopbackClientID},
		"client_secret": {loopbackClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
		"code_verifier": {codeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", googleOAuth2TokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao contatar Google OAuth: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("resposta inválida do Google: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("Google OAuth erro: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("access_token vazio na resposta")
	}
	return &result, nil
}

// StartOAuth2AppCallback gera a URL de autenticação OAuth2 usando o próprio app como callback.
// Não abre servidor local — o callback é tratado pelo endpoint /oauth/google/callback do app.
// loginHint: email corporativo opcional — quando informado, o Google direciona para o provedor WIF
// configurado pela empresa (ex: Azure AD), evitando que o usuário escolha uma conta pessoal.
func StartOAuth2AppCallback(redirectURI, sessionID, loginHint string) (authURL, pkceVerifier string, err error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return "", "", fmt.Errorf("erro ao gerar PKCE: %w", err)
	}

	params := url.Values{
		"client_id":             {loopbackClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {googleOAuth2Scope},
		"state":                 {sessionID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	if loginHint != "" {
		params.Set("login_hint", loginHint)
	}
	return googleAuthURL + "?" + params.Encode(), verifier, nil
}

// ExchangeAuthCode troca o authorization code por tokens. Exportado para uso nos handlers.
func ExchangeAuthCode(ctx context.Context, code, redirectURI, pkceVerifier string) (accessToken, refreshToken string, err error) {
	result, err := exchangeAuthCode(ctx, code, redirectURI, pkceVerifier)
	if err != nil {
		return "", "", err
	}
	return result.AccessToken, result.RefreshToken, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Workforce Identity Federation (WIF) — auth.cloud.google
// ─────────────────────────────────────────────────────────────────────────────

const (
	wifAuthURL  = "https://auth.cloud.google/authorize"
	wifTokenURL = "https://auth.cloud.google/token"
)

// ParseWIFPoolProvider extrai poolID e providerID de strings no formato "pool/provider"
// ou "//iam.googleapis.com/locations/global/workforcePools/pool/providers/provider".
func ParseWIFPoolProvider(s string) (poolID, providerID string, ok bool) {
	// Formato curto: "entraid-agentspace/entraid-federation-agentspace"
	if strings.Contains(s, "/") && !strings.HasPrefix(s, "//") {
		parts := strings.SplitN(s, "/", 2)
		return parts[0], parts[1], true
	}
	// Formato longo: //iam.googleapis.com/.../workforcePools/POOL/providers/PROVIDER
	if strings.Contains(s, "workforcePools/") {
		// Extrair após workforcePools/
		after := s[strings.Index(s, "workforcePools/")+len("workforcePools/"):]
		parts := strings.SplitN(after, "/providers/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

// WIFAudience retorna o resource name completo do WIF pool+provider.
func WIFAudience(poolID, providerID string) string {
	return fmt.Sprintf(
		"//iam.googleapis.com/locations/global/workforcePools/%s/providers/%s",
		poolID, providerID,
	)
}

// StartWIFAppCallback gera a URL de autenticação WIF via auth.cloud.google.
// O callback é o endpoint /oauth/google/callback do próprio app.
func StartWIFAppCallback(redirectURI, sessionID, poolID, providerID string) (authURL, pkceVerifier string, err error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return "", "", fmt.Errorf("erro ao gerar PKCE WIF: %w", err)
	}

	params := url.Values{
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {googleOAuth2Scope},
		"state":                 {sessionID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"audience":              {WIFAudience(poolID, providerID)},
	}
	return wifAuthURL + "?" + params.Encode(), verifier, nil
}

// ExchangeWIFCode troca o authorization code WIF por access_token (e refresh_token se disponível).
func ExchangeWIFCode(ctx context.Context, code, redirectURI, pkceVerifier string) (accessToken, refreshToken string, err error) {
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {pkceVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", wifTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("erro ao criar requisição WIF token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("erro ao contatar auth.cloud.google/token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("resposta inválida do WIF token endpoint: %w", err)
	}
	if result.Error != "" {
		return "", "", fmt.Errorf("WIF auth erro: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", "", fmt.Errorf("access_token vazio na resposta WIF")
	}
	return result.AccessToken, result.RefreshToken, nil
}

// RefreshWIFToken renova um access token WIF usando o refresh token.
// Suporta o tipo "external_account_authorized_user" do ADC do gcloud.
func RefreshWIFToken(ctx context.Context, refreshToken, poolID, providerID string) (string, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"audience":      {WIFAudience(poolID, providerID)},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", wifTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição refresh WIF: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao contatar auth.cloud.google/token (refresh): %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida do WIF refresh: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("WIF refresh erro: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("access_token vazio no refresh WIF")
	}
	return result.AccessToken, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func successHTML(title, msg string, success bool) string {
	color := "#dc2626"
	if success {
		color = "#16a34a"
	}
	return fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8">
<title>%s</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f9fafb;}
.card{background:#fff;border-radius:12px;padding:2rem 3rem;box-shadow:0 4px 24px rgba(0,0,0,.1);text-align:center;max-width:420px;}
h1{color:%s;font-size:1.5rem;margin-bottom:.5rem;}p{color:#6b7280;}</style></head>
<body><div class="card"><h1>%s</h1><p>%s</p></div></body></html>`, title, color, title, msg)
}
