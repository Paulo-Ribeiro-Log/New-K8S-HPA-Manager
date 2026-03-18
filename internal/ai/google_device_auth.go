package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Credenciais públicas do Google Cloud SDK para apps instaladas (open source).
// São "public clients" — o client_secret não é confidencial para apps nativas.
// Fonte: github.com/googleapis/google-cloud-go e google-cloud-sdk
const (
	googleDeviceAuthURL = "https://oauth2.googleapis.com/device/code"
	googleTokenURL      = "https://oauth2.googleapis.com/token"
	googleCloudScope    = "https://www.googleapis.com/auth/cloud-platform"

	// gcloud SDK public client (installed app / native app)
	defaultGoogleClientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	defaultGoogleClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"
)

// DeviceAuthCode resposta do endpoint de device authorization
type DeviceAuthCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"` // segundos entre polls
	// Campos de erro (Google retorna quando falha)
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// DeviceAuthToken resposta do endpoint de token
type DeviceAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// StartDeviceAuth inicia o fluxo Device Authorization Grant.
// Retorna o código que o usuário deve inserir em accounts.google.com/device.
func StartDeviceAuth(ctx context.Context) (*DeviceAuthCode, error) {
	body := url.Values{
		"client_id": {defaultGoogleClientID},
		"scope":     {googleCloudScope},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", googleDeviceAuthURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro ao contactar Google OAuth: %w", err)
	}
	defer resp.Body.Close()

	var result DeviceAuthCode
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("resposta inválida do Google (HTTP %d): %w", resp.StatusCode, err)
	}

	if result.Error != "" {
		msg := result.Error
		if result.ErrorDescription != "" {
			msg += ": " + result.ErrorDescription
		}
		return nil, fmt.Errorf("Google recusou device auth — %s", msg)
	}

	if result.DeviceCode == "" {
		return nil, fmt.Errorf("Google não retornou device_code (HTTP %d)", resp.StatusCode)
	}

	// Garantir URL de verificação padrão
	if result.VerificationURL == "" {
		result.VerificationURL = "https://accounts.google.com/device"
	}
	if result.Interval == 0 {
		result.Interval = 5
	}

	return &result, nil
}

// PollDeviceToken faz polling até o usuário completar a autenticação.
// Retorna o token quando autenticado, ou erro se expirou/cancelou.
func PollDeviceToken(ctx context.Context, deviceCode string, intervalSecs int) (*DeviceAuthToken, error) {
	if intervalSecs <= 0 {
		intervalSecs = 5
	}

	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("autenticação cancelada ou tempo esgotado")
		case <-ticker.C:
			token, pending, err := TryExchangeDeviceToken(ctx, deviceCode)
			if err != nil {
				return nil, err
			}
			if pending {
				continue // usuário ainda não autenticou
			}
			return token, nil
		}
	}
}

// TryExchangeDeviceToken tenta trocar o device_code por um access_token.
// Retorna (token, false, nil) quando autenticado,
// (nil, true, nil) quando ainda aguardando,
// (nil, false, err) em caso de erro fatal.
func TryExchangeDeviceToken(ctx context.Context, deviceCode string) (*DeviceAuthToken, bool, error) {
	body := url.Values{
		"client_id":     {defaultGoogleClientID},
		"client_secret": {defaultGoogleClientSecret},
		"device_code":   {deviceCode},
		"grant_type":    {"urn:ietf:wg:oauth:2.0:device_code"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", googleTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, false, fmt.Errorf("erro ao criar requisição de token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("erro ao contactar Google OAuth: %w", err)
	}
	defer resp.Body.Close()

	var result DeviceAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, false, fmt.Errorf("resposta inválida do Google: %w", err)
	}

	switch result.Error {
	case "":
		// Sucesso
		if result.AccessToken == "" {
			return nil, false, fmt.Errorf("access_token vazio na resposta")
		}
		return &result, false, nil

	case "authorization_pending":
		// Usuário ainda não autenticou — continuar polling
		return nil, true, nil

	case "slow_down":
		// Google pediu para diminuir o ritmo — aguardar mais
		time.Sleep(5 * time.Second)
		return nil, true, nil

	case "expired_token":
		return nil, false, fmt.Errorf("código expirado — inicie o processo novamente")

	case "access_denied":
		return nil, false, fmt.Errorf("acesso negado pelo usuário")

	default:
		return nil, false, fmt.Errorf("erro Google OAuth: %s — %s", result.Error, result.ErrorDesc)
	}
}

// GetAccessTokenFromRefreshToken troca um refresh_token por um novo access_token.
func GetAccessTokenFromRefreshToken(ctx context.Context, refreshToken string) (string, error) {
	body := url.Values{
		"client_id":     {defaultGoogleClientID},
		"client_secret": {defaultGoogleClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", googleTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao contactar Google OAuth: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("erro ao renovar token: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("access_token vazio na resposta")
	}

	return result.AccessToken, nil
}
