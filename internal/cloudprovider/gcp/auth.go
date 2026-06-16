package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const oauth2TokenURL = "https://oauth2.googleapis.com/token"

// adcFile representa o arquivo application_default_credentials.json.
// Suporta "authorized_user" (OAuth2 pessoal/workspace) e
// "external_account_authorized_user" (WIF corporativo).
type adcFile struct {
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	TokenURL     string `json:"token_url"` // usado por WIF
}

// GetGCPAccessToken obtém um access_token GCP via Application Default Credentials.
// Exportado para uso pelo pacote config (discovery GKE via REST API).
func GetGCPAccessToken(ctx context.Context) (string, error) {
	return getGCPAccessToken(ctx)
}

// getGCPAccessToken obtém um access_token GCP via Application Default Credentials.
// Ordem de busca:
//  1. GOOGLE_APPLICATION_CREDENTIALS (env var)
//  2. ~/.config/k8s-hpa-manager/google_credentials.json (ADC da própria app — Device Auth)
//  3. ~/.config/gcloud/application_default_credentials.json (gcloud auth application-default login)
func getGCPAccessToken(ctx context.Context) (string, error) {
	adcPath, err := findGCPADCFile()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(adcPath)
	if err != nil {
		return "", fmt.Errorf("erro ao ler ADC (%s): %w", adcPath, err)
	}

	var creds adcFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("arquivo ADC inválido: %w", err)
	}

	if creds.RefreshToken == "" {
		return "", fmt.Errorf("refresh_token ausente no ADC — autentique em AI Settings → Gemini → Autenticar via Código de Dispositivo")
	}

	tokenURL := oauth2TokenURL
	if creds.Type == "external_account_authorized_user" && creds.TokenURL != "" {
		tokenURL = creds.TokenURL
	}

	return refreshGCPToken(ctx, tokenURL, creds.ClientID, creds.ClientSecret, creds.RefreshToken)
}

// findGCPADCFile localiza o arquivo ADC na ordem de prioridade da app.
func findGCPADCFile() (string, error) {
	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("não foi possível determinar o home dir: %w", err)
	}

	candidates := []string{
		filepath.Join(home, ".config", "k8s-hpa-manager", "google_credentials.json"),
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("credenciais GCP não encontradas — autentique em AI Settings → Gemini → \"Autenticar via Código de Dispositivo\"")
}

// refreshGCPToken troca um refresh_token por um access_token via OAuth2.
func refreshGCPToken(ctx context.Context, tokenURL, clientID, clientSecret, refreshToken string) (string, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if clientID != "" {
		body.Set("client_id", clientID)
	}
	if clientSecret != "" {
		body.Set("client_secret", clientSecret)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição de token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao renovar token GCP: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida do token endpoint: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("token refresh falhou: %s — %s", result.Error, result.Description)
	}
	return result.AccessToken, nil
}
