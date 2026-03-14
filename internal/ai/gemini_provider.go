package ai

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GeminiProvider implementa Provider para Google Gemini API (AI Studio ou Vertex AI)
type GeminiProvider struct {
	apiKey             string
	model              string
	timeout            time.Duration
	client             *http.Client
	authMode           string // "apikey" ou "vertex"
	project            string // projeto GCP (modo vertex)
	location           string // região GCP (modo vertex, ex: us-central1)
	serviceAccountJSON string // conteúdo JSON do service account (modo vertex sem gcloud)
	refreshToken       string // OAuth refresh token do Device Auth flow
}

// NewGeminiProvider cria um novo GeminiProvider
func NewGeminiProvider(config *Config) *GeminiProvider {
	return &GeminiProvider{
		apiKey:             config.GeminiAPIKey,
		model:              config.GeminiModel,
		timeout:            time.Duration(config.Timeout) * time.Second,
		client:             &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		authMode:           config.GeminiAuthMode,
		project:            config.GeminiVertexProject,
		location:           config.GeminiVertexLocation,
		serviceAccountJSON: config.GeminiServiceAccountJSON,
		refreshToken:       config.GeminiRefreshToken,
	}
}

// geminiRequest estrutura de requisição Gemini API
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// geminiResponse estrutura de resposta Gemini API
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Analyze envia prompt para Gemini API e retorna análise
func (p *GeminiProvider) Analyze(ctx context.Context, prompt string) (string, error) {
	if p.authMode == "vertex" {
		return p.analyzeVertex(ctx, prompt)
	}
	return p.analyzeAPIKey(ctx, prompt)
}

// analyzeAPIKey usa a API do AI Studio com chave de API
func (p *GeminiProvider) analyzeAPIKey(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		p.model, p.apiKey)

	return p.doRequest(ctx, url, "", "", prompt)
}

// analyzeVertex usa o Vertex AI.
// Estratégia para SSO corporativo (sem service account):
//  1. AI Studio endpoint (generativelanguage.googleapis.com) + OAuth token + X-Goog-User-Project
//     → requer apenas serviceusage.services.use (qualquer role básica de projeto)
//  2. Vertex AI endpoint (aiplatform.googleapis.com) + OAuth/ADC/service account
//     → requer roles/aiplatform.user
func (p *GeminiProvider) analyzeVertex(ctx context.Context, prompt string) (string, error) {
	// Tentativa 1: AI Studio com OAuth SSO corporativo + billing do projeto.
	// Muitas contas SSO têm serviceusage.services.use mas não aiplatform.endpoints.predict.
	if p.refreshToken != "" && p.project != "" && p.serviceAccountJSON == "" {
		if token, err := GetAccessTokenFromRefreshToken(ctx, p.refreshToken); err == nil {
			aiStudioURL := fmt.Sprintf(
				"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
				p.model)
			if result, err := p.doRequest(ctx, aiStudioURL, token, p.project, prompt); err == nil {
				return result, nil
			}
			// AI Studio com SSO falhou — continua para Vertex AI endpoint abaixo
		}
	}

	// Tentativa 2: Vertex AI endpoint (service account / ADC / refresh token)
	token, err := GetVertexAccessToken(ctx, p.serviceAccountJSON, p.refreshToken)
	if err != nil {
		return "", err
	}

	vertexURL := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		p.location, p.project, p.location, p.model)

	return p.doRequest(ctx, vertexURL, token, "", prompt)
}

// doRequest executa a requisição HTTP para a API Gemini.
// userProject, quando não vazio, define o header X-Goog-User-Project (billing/quota).
func (p *GeminiProvider) doRequest(ctx context.Context, url, bearerToken, userProject, prompt string) (string, error) {
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if userProject != "" {
		req.Header.Set("X-Goog-User-Project", userProject)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 403:
			return "", fmt.Errorf("Vertex AI: permissão negada (403). Sua conta não tem a role 'aiplatform.endpoints.predict' no projeto GCP. Solicite ao administrador a role 'Vertex AI User' ou use uma API key do AI Studio em vez do Vertex")
		case 429:
			return "", fmt.Errorf("Cota da API Gemini esgotada (429). Aguarde alguns minutos ou considere usar o plano pago. Detalhes: %s", string(body))
		default:
			return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(body))
		}
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// ---------------------------------------------------------------------------
// Service Account JSON auth (sem gcloud, sem browser)
// ---------------------------------------------------------------------------

// serviceAccountCreds representa o arquivo de service account do Google Cloud
type serviceAccountCreds struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

// GetServiceAccountAccessToken obtém access token a partir do JSON de service account.
// Não requer gcloud CLI nem arquivo ADC — usa apenas stdlib Go.
func GetServiceAccountAccessToken(ctx context.Context, serviceAccountJSON string) (string, error) {
	if serviceAccountJSON == "" {
		return "", fmt.Errorf("service account JSON não fornecido")
	}

	var creds serviceAccountCreds
	if err := json.Unmarshal([]byte(serviceAccountJSON), &creds); err != nil {
		return "", fmt.Errorf("service account JSON inválido: %w", err)
	}

	if creds.Type != "service_account" {
		return "", fmt.Errorf("tipo inválido no JSON: %q (esperado: service_account)", creds.Type)
	}
	if creds.PrivateKey == "" || creds.ClientEmail == "" {
		return "", fmt.Errorf("service account JSON incompleto: private_key e client_email são obrigatórios")
	}

	tokenURI := creds.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	// Parsear private key RSA
	block, _ := pem.Decode([]byte(creds.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("private_key inválida no service account JSON (formato PEM esperado)")
	}

	privateKeyRaw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("erro ao parsear private_key: %w", err)
	}

	rsaKey, ok := privateKeyRaw.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private_key não é RSA")
	}

	// Montar JWT (header.payload.signature)
	now := time.Now().Unix()
	headerJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payloadJSON, _ := json.Marshal(map[string]interface{}{
		"iss":   creds.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   tokenURI,
		"iat":   now,
		"exp":   now + 3600,
	})

	sigInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON)

	hash := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("erro ao assinar JWT: %w", err)
	}

	jwt := sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	// Trocar JWT por access token
	body := url.Values{
		"grant_type": {"urn:ietf:wg:oauth:2.0:jwt-bearer"},
		"assertion":  {jwt},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURI, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição de token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao contatar Google OAuth2: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida do Google OAuth2: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("Google OAuth2 recusou o service account: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("access_token vazio na resposta do Google OAuth2")
	}

	return result.AccessToken, nil
}

// ---------------------------------------------------------------------------
// ADC (Application Default Credentials) — fallback quando não há service account
// ---------------------------------------------------------------------------

// adcCredentials representa o arquivo application_default_credentials.json do gcloud
type adcCredentials struct {
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

// GetADCAccessToken obtém access token via Application Default Credentials (ADC).
// Lê o arquivo ADC do gcloud e troca o refresh_token pela API do Google.
func GetADCAccessToken(ctx context.Context) (string, error) {
	adcPath, err := findADCFile()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(adcPath)
	if err != nil {
		return "", fmt.Errorf("erro ao ler ADC (%s): %w", adcPath, err)
	}

	var creds adcCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("arquivo ADC inválido: %w", err)
	}

	if creds.Type != "authorized_user" {
		return "", fmt.Errorf("tipo de credencial ADC não suportado: %q", creds.Type)
	}
	if creds.RefreshToken == "" {
		return "", fmt.Errorf("refresh_token ausente no arquivo ADC")
	}

	return exchangeRefreshToken(ctx, creds.ClientID, creds.ClientSecret, creds.RefreshToken)
}

// findADCFile localiza o arquivo Application Default Credentials.
func findADCFile() (string, error) {
	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("não foi possível determinar o home dir: %w", err)
	}

	candidates := []string{
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
		filepath.Join(home, ".cache", "google-vscode-extension", "auth", "application_default_credentials.json"),
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("ADC não encontrado (procurado em %v)", candidates)
}

// exchangeRefreshToken troca um refresh_token por um access_token via Google OAuth2.
func exchangeRefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (string, error) {
	body := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://oauth2.googleapis.com/token",
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição OAuth2: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao contatar Google OAuth2: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida do Google OAuth2: %w", err)
	}

	if result.Error != "" {
		return "", fmt.Errorf("Google OAuth2 recusou o token: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("access_token vazio na resposta do Google OAuth2")
	}

	return result.AccessToken, nil
}

// GetVertexAccessToken retorna access token para Vertex AI.
// Prioridade: 1) OAuth refresh token (Device Auth)  2) Service Account JSON  3) ADC file (gcloud)
func GetVertexAccessToken(ctx context.Context, serviceAccountJSON, refreshToken string) (string, error) {
	if refreshToken != "" {
		token, err := GetAccessTokenFromRefreshToken(ctx, refreshToken)
		if err == nil {
			return token, nil
		}
		// Se o refresh token falhou, tentar próxima opção
	}

	if serviceAccountJSON != "" {
		return GetServiceAccountAccessToken(ctx, serviceAccountJSON)
	}

	token, err := GetADCAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("nenhuma credencial Vertex AI configurada — use o botão 'Autenticar com Google' nas configurações de IA")
	}
	return token, nil
}

// GetModel retorna nome do modelo
func (p *GeminiProvider) GetModel() string {
	return p.model
}

// IsAvailable verifica se Gemini API está configurado
func (p *GeminiProvider) IsAvailable(ctx context.Context) bool {
	if p.authMode == "vertex" {
		// No modo Vertex, disponível se o projeto GCP está configurado.
		// A autenticação real (refresh token / service account / ADC) é testada
		// apenas na análise — consistente com Claude, OpenAI e Copilot.
		return p.project != ""
	}
	return p.apiKey != ""
}

// GetName retorna nome do provider
func (p *GeminiProvider) GetName() string {
	return "gemini"
}
