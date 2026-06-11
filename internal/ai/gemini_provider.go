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

	"github.com/rs/zerolog/log"
)

// vertexModelMap converte IDs de modelo do AI Studio para o formato versionado do Vertex AI.
// O Vertex AI exige nomes com sufixo de versão (ex: gemini-2.0-flash-001) enquanto o
// AI Studio aceita aliases sem versão (ex: gemini-2.0-flash).
var vertexModelMap = map[string]string{
	// Nomes AI Studio → IDs versionados do Vertex AI
	"gemini-3.5-flash":      "gemini-3.5-flash-preview-0514",
	"gemini-3.1-pro":        "gemini-3.1-pro-preview-0514",
	"gemini-2.5-pro":        "gemini-2.5-pro-preview-06-05",
	"gemini-2.5-flash":      "gemini-2.5-flash-preview-05-20",
	"gemini-2.0-flash":      "gemini-2.0-flash-001",
	"gemini-2.0-flash-lite": "gemini-2.0-flash-lite-001",
	"gemini-1.5-pro":        "gemini-1.5-pro-002",
	"gemini-1.5-flash":      "gemini-1.5-flash-002",
}

// ToVertexModelID converte ID de modelo do AI Studio para formato do Vertex AI.
// Retorna o modelo sem alteração se já tiver sufixo de versão.
func ToVertexModelID(model string) string {
	if mapped, ok := vertexModelMap[model]; ok {
		return mapped
	}
	return model
}

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
	refreshToken       string // OAuth refresh token (Device Auth / WIF App Callback)
	wifPoolProvider    string // "poolID/providerID" para WIF (ex: "entraid-agentspace/entraid-federation-agentspace")
}

// NewGeminiProvider cria um novo GeminiProvider
func NewGeminiProvider(config *Config) *GeminiProvider {
	model := config.GeminiModel
	if config.GeminiAuthMode == "vertex" {
		model = ToVertexModelID(model)
	}
	return &GeminiProvider{
		apiKey:             config.GeminiAPIKey,
		model:              model,
		timeout:            time.Duration(config.Timeout) * time.Second,
		client:             &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		authMode:           config.GeminiAuthMode,
		project:            config.GeminiVertexProject,
		location:           config.GeminiVertexLocation,
		serviceAccountJSON: config.GeminiServiceAccountJSON,
		refreshToken:       config.GeminiRefreshToken,
		wifPoolProvider:    config.GeminiWifPoolProvider,
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
//
// Quando `project` está configurado: Vertex AI direto (aiplatform.googleapis.com).
// Sem `project` e com refresh token: AI Studio (generativelanguage.googleapis.com),
// útil para usuários Gemini for Google Workspace sem projeto GCP dedicado.
func (p *GeminiProvider) analyzeVertex(ctx context.Context, prompt string) (string, error) {
	// Com projeto GCP configurado → Vertex AI endpoint diretamente.
	// Evita tentativas no AI Studio que causam 503 visíveis quando o modelo está
	// sobrecarregado lá mas disponível no Vertex AI.
	if p.project != "" {
		token, err := GetVertexAccessToken(ctx, p.serviceAccountJSON, p.refreshToken, p.wifPoolProvider)
		if err != nil {
			return "", err
		}
		vertexURL := fmt.Sprintf(
			"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
			p.location, p.project, p.location, p.model)
		return p.doRequest(ctx, vertexURL, token, "", prompt)
	}

	// Sem projeto: AI Studio com OAuth token (quota individual do Workspace).
	if p.refreshToken != "" && p.serviceAccountJSON == "" {
		token, err := GetAccessTokenFromRefreshToken(ctx, p.refreshToken)
		if err != nil {
			log.Warn().Err(err).Msg("Vertex AI: falha ao obter access token do refresh token")
		} else {
			aiStudioURL := fmt.Sprintf(
				"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
				p.model)
			if result, err := p.doRequest(ctx, aiStudioURL, token, "", prompt); err == nil {
				return result, nil
			} else {
				log.Warn().Err(err).Str("attempt", "ai-studio-no-project").Msg("Vertex AI: AI Studio sem projeto falhou")
			}
		}
	}

	// Fallback: ADC ou service account sem projeto configurado (raro)
	token, err := GetVertexAccessToken(ctx, p.serviceAccountJSON, p.refreshToken, p.wifPoolProvider)
	if err != nil {
		return "", fmt.Errorf("projeto GCP não configurado e nenhuma credencial Vertex AI disponível: %w", err)
	}
	aiStudioURL := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
		p.model)
	return p.doRequest(ctx, aiStudioURL, token, "", prompt)
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

	const maxRetries = 3
	var (
		resp *http.Response
		body []byte
	)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(1<<uint(attempt)) * time.Second // 2s, 4s, 8s
			log.Warn().Int("attempt", attempt).Dur("wait", wait).Int("status", resp.StatusCode).
				Msg("Gemini: retry após erro temporário")
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
			// Recria o request (body foi consumido)
			req, err = http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
			if err != nil {
				return "", fmt.Errorf("failed to recreate request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if bearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+bearerToken)
			}
			if userProject != "" {
				req.Header.Set("X-Goog-User-Project", userProject)
			}
		}

		var doErr error
		resp, doErr = p.client.Do(req)
		if doErr != nil {
			return "", fmt.Errorf("failed to send request: %w", doErr)
		}

		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			break
		}

		// Erros retryáveis: 503 (sobrecarga) e 429 (rate limit)
		if (resp.StatusCode == 503 || resp.StatusCode == 429) && attempt < maxRetries {
			continue
		}

		// Erros definitivos
		switch resp.StatusCode {
		case 403:
			return "", fmt.Errorf("permissão negada (403) — URL: %s — body: %s", url, string(body))
		case 429:
			return "", fmt.Errorf("cota da API Gemini esgotada (429) após %d tentativas. Detalhes: %s", attempt+1, string(body))
		case 503:
			return "", fmt.Errorf("Gemini indisponível (503) após %d tentativas — modelo sobrecarregado. Tente novamente em alguns minutos. Detalhes: %s", attempt+1, string(body))
		default:
			return "", fmt.Errorf("gemini API error (status %d) — URL: %s — body: %s", resp.StatusCode, url, string(body))
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

// adcCredentials representa o arquivo application_default_credentials.json do gcloud.
// Suporta os tipos "authorized_user" (conta Google pessoal/workspace) e
// "external_account_authorized_user" (Workforce Identity Federation — WIF).
type adcCredentials struct {
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	// Campos específicos de WIF (external_account_authorized_user)
	TokenURL string `json:"token_url"`
	Audience string `json:"audience"`
}

// GetADCAccessToken obtém access token via Application Default Credentials (ADC).
// Suporta authorized_user (conta pessoal) e external_account_authorized_user (WIF/Workforce).
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

	if creds.RefreshToken == "" {
		return "", fmt.Errorf("refresh_token ausente no arquivo ADC")
	}

	switch creds.Type {
	case "authorized_user":
		// Conta Google pessoal ou Workspace: oauth2.googleapis.com/token
		return exchangeRefreshToken(ctx, creds.ClientID, creds.ClientSecret, creds.RefreshToken)

	case "external_account_authorized_user":
		// Workforce Identity Federation (WIF): auth.cloud.google/token
		// Gerado por: gcloud auth application-default login --audiences=//iam.googleapis.com/...
		tokenURL := creds.TokenURL
		if tokenURL == "" {
			tokenURL = "https://auth.cloud.google/token"
		}
		return refreshExternalAccountToken(ctx, tokenURL, creds.ClientID, creds.ClientSecret, creds.RefreshToken)

	default:
		return "", fmt.Errorf("tipo de credencial ADC não suportado: %q (suportados: authorized_user, external_account_authorized_user)", creds.Type)
	}
}

// refreshExternalAccountToken renova um token WIF (external_account_authorized_user).
// O endpoint auth.cloud.google/token aceita tokens de Workforce Identity Federation.
func refreshExternalAccountToken(ctx context.Context, tokenURL, clientID, clientSecret, refreshToken string) (string, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição WIF token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao contatar %s: %w", tokenURL, err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida do WIF token endpoint: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("WIF token refresh falhou: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("access_token vazio na resposta WIF")
	}
	return result.AccessToken, nil
}

// findADCFile localiza o arquivo Application Default Credentials.
// Ordem de busca: GOOGLE_APPLICATION_CREDENTIALS → ADC próprio da app → gcloud → vscode extension
func findADCFile() (string, error) {
	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("não foi possível determinar o home dir: %w", err)
	}

	candidates := []string{
		// Credencial própria da app (gravada pelo Device Auth Grant — não conflita com gcloud)
		filepath.Join(home, ".config", "k8s-hpa-manager", "google_credentials.json"),
		// gcloud application-default login
		filepath.Join(home, ".config", "gcloud", "application_default_credentials.json"),
		// VSCode extension
		filepath.Join(home, ".cache", "google-vscode-extension", "auth", "application_default_credentials.json"),
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("ADC não encontrado (procurado em %v)", candidates)
}

// appADCPath retorna o caminho do ADC próprio da aplicação (nunca conflita com gcloud).
func appADCPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("não foi possível determinar o home dir: %w", err)
	}
	return filepath.Join(home, ".config", "k8s-hpa-manager", "google_credentials.json"), nil
}

// WriteADCFile grava credenciais Google em caminho próprio da aplicação.
// Usa o mesmo formato do gcloud ADC para que GetADCAccessToken possa lê-lo.
// NÃO toca no arquivo gcloud (~/.config/gcloud/application_default_credentials.json).
func WriteADCFile(refreshToken string) error {
	adcPath, err := appADCPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(adcPath), 0700); err != nil {
		return fmt.Errorf("erro ao criar diretório ADC: %w", err)
	}

	// Mesmas credenciais do gcloud SDK (public client — não é segredo)
	const clientID = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	const clientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"

	creds := adcCredentials{
		Type:         "authorized_user",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("erro ao serializar ADC: %w", err)
	}

	if err := os.WriteFile(adcPath, data, 0600); err != nil {
		return fmt.Errorf("erro ao gravar ADC (%s): %w", adcPath, err)
	}
	return nil
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
// Prioridade: 1) WIF refresh token (poolID/providerID)  2) OAuth refresh token (Device Auth / App Callback)
//             3) Service Account JSON  4) ADC file (gcloud)
// wifPoolProvider: formato "poolID/providerID" (ex: "entraid-agentspace/entraid-federation-agentspace")
func GetVertexAccessToken(ctx context.Context, serviceAccountJSON, refreshToken string, wifPoolProvider ...string) (string, error) {
	// WIF refresh token — prioridade máxima quando pool/provider configurado
	if len(wifPoolProvider) > 0 && wifPoolProvider[0] != "" && refreshToken != "" {
		poolID, providerID, ok := ParseWIFPoolProvider(wifPoolProvider[0])
		if ok {
			token, err := RefreshWIFToken(ctx, refreshToken, poolID, providerID)
			if err == nil {
				return token, nil
			}
			log.Warn().Err(err).Str("pool", poolID).Str("provider", providerID).Msg("WIF: falha ao renovar token, tentando próxima opção")
		}
	}

	// OAuth2 standard refresh token (Device Auth ou App Callback)
	if refreshToken != "" {
		token, err := GetAccessTokenFromRefreshToken(ctx, refreshToken)
		if err == nil {
			return token, nil
		}
		// Se o refresh padrão falhou, tentar via auth.cloud.google/token (WIF sem pool explícito).
		// Tokens WIF armazenados sem gemini_wif_login_url configurado no DB chegam aqui.
		token, err = refreshExternalAccountToken(ctx, "https://auth.cloud.google/token", "", "", refreshToken)
		if err == nil {
			return token, nil
		}
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
