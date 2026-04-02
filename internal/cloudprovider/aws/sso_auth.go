package aws

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

func newCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// AWSSSOConfig representa a configuração SSO de um perfil AWS.
// Formato YAML (aws-config.yaml):
//
//	profiles:
//	  asaplog:
//	    sso:
//	      start-url: https://asaplog.awsapps.com/start
//	      region: us-east-1
//	      account-id: "400894646268"
//	      role-name: Administrators
//	    region: us-east-1
type AWSSSOConfig struct {
	StartURL  string `json:"start_url"  yaml:"start-url"`
	Region    string `json:"region"     yaml:"region"`
	AccountID string `json:"account_id" yaml:"account-id"`
	RoleName  string `json:"role_name"  yaml:"role-name"`
}

// AWSProfileConfig agrupa SSO e região padrão de um perfil.
type AWSProfileConfig struct {
	SSO    AWSSSOConfig `json:"sso"    yaml:"sso"`
	Region string       `json:"region" yaml:"region"`
}

// AWSConfigFile é o arquivo completo ~/.k8s-hpa-manager/aws-config.yaml.
type AWSConfigFile struct {
	Profiles map[string]*AWSProfileConfig `json:"profiles" yaml:"profiles"`
}

// LoginSession rastreia uma sessão de Device Auth em andamento.
type LoginSession struct {
	Profile   string
	URL       string    // verificationUriComplete (com user_code embutido)
	UserCode  string    // ex: ABCD-EFGH (exibido separado por clareza)
	Done      chan error // fechado quando o token for obtido ou expirar
	StartedAt time.Time
}

// AWSSSOManager gerencia verificação e renovação de tokens AWS SSO
// usando o fluxo Device Authorization Grant do AWS SSO OIDC (RFC 8628).
// Não depende de browser loopback — funciona em WSL2 e ambientes headless.
type AWSSSOManager struct {
	configPath string
	mu         sync.Mutex
	sessions   map[string]*LoginSession
}

// NewAWSSSOManager cria o manager.
func NewAWSSSOManager() *AWSSSOManager {
	homeDir, _ := os.UserHomeDir()
	return &AWSSSOManager{
		configPath: filepath.Join(homeDir, ".k8s-hpa-manager", "aws-config.yaml"),
		sessions:   make(map[string]*LoginSession),
	}
}

// ─── Tipos internos do AWS SSO OIDC ──────────────────────────────────────────

type oidcClientInfo struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

type deviceAuthInfo struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

type oidcTokenInfo struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
}

// ─── IsTokenValid ─────────────────────────────────────────────────────────────

// IsTokenValid retorna true se as credenciais do perfil estão válidas.
func (m *AWSSSOManager) IsTokenValid(ctx context.Context, profile string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"sts", "get-caller-identity", "--output", "json"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	cmd := newCommandContext(checkCtx, "aws", args...)
	return cmd.Run() == nil
}

// ─── StartLogin ───────────────────────────────────────────────────────────────

// StartLogin inicia o Device Authorization Grant do AWS SSO OIDC para o perfil.
// Retorna a sessão com URL e userCode assim que disponíveis.
// Não usa `aws sso login` — evita o problema de loopback redirect no WSL2.
func (m *AWSSSOManager) StartLogin(ctx context.Context, profile string) (*LoginSession, error) {
	m.mu.Lock()
	if existing, ok := m.sessions[profile]; ok && existing.URL != "" {
		m.mu.Unlock()
		return existing, nil
	}
	m.mu.Unlock()

	cfg, err := m.LoadProfileConfig(profile)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler configuração: %w", err)
	}
	if cfg == nil || cfg.SSO.StartURL == "" {
		// Sinaliza que o perfil precisa ser configurado primeiro.
		return &LoginSession{Profile: profile, Done: make(chan error, 1), StartedAt: time.Now()}, nil
	}

	ssoRegion := cfg.SSO.Region
	if ssoRegion == "" {
		ssoRegion = cfg.Region
	}
	if ssoRegion == "" {
		return nil, fmt.Errorf("região SSO não configurada para o perfil '%s'", profile)
	}

	// 1. Registrar cliente OIDC.
	oidcClient, err := m.registerOIDCClient(ctx, ssoRegion)
	if err != nil {
		return nil, fmt.Errorf("falha ao registrar cliente OIDC: %w", err)
	}

	// 2. Iniciar autorização do dispositivo.
	devAuth, err := m.startDeviceAuthorization(ctx, ssoRegion, oidcClient, cfg.SSO.StartURL)
	if err != nil {
		return nil, fmt.Errorf("falha ao iniciar Device Auth: %w", err)
	}

	url := devAuth.VerificationURIComplete
	if url == "" {
		url = devAuth.VerificationURI
	}

	session := &LoginSession{
		Profile:   profile,
		URL:       url,
		UserCode:  devAuth.UserCode,
		Done:      make(chan error, 1),
		StartedAt: time.Now(),
	}

	m.mu.Lock()
	m.sessions[profile] = session
	m.mu.Unlock()

	// 3. Polling em background até token ou expiração.
	go func() {
		expires := devAuth.ExpiresIn
		if expires <= 0 {
			expires = 600
		}
		pollCtx, cancel := context.WithTimeout(context.Background(), time.Duration(expires)*time.Second)
		defer cancel()

		interval := time.Duration(devAuth.Interval) * time.Second
		if interval < 5*time.Second {
			interval = 5 * time.Second
		}

		for {
			select {
			case <-pollCtx.Done():
				session.Done <- fmt.Errorf("timeout: autenticação não concluída em %ds", expires)
				m.removeSession(profile)
				return
			case <-time.After(interval):
			}

			token, err := m.pollToken(pollCtx, ssoRegion, oidcClient, devAuth.DeviceCode)
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, "authorization_pending") || strings.Contains(msg, "slow_down") {
					continue
				}
				session.Done <- err
				m.removeSession(profile)
				return
			}

			// Token obtido — salvar no cache AWS SSO.
			if saveErr := m.saveTokenCache(cfg.SSO.StartURL, ssoRegion, token); saveErr != nil {
				session.Done <- fmt.Errorf("token obtido mas falha ao salvar cache: %w", saveErr)
			} else {
				session.Done <- nil
			}

			// Garantir que o perfil existe em ~/.aws/config para uso pelo aws CLI.
			_ = m.ensureAWSProfile(profile, cfg)

			time.AfterFunc(60*time.Second, func() { m.removeSession(profile) })
			return
		}
	}()

	return session, nil
}

func (m *AWSSSOManager) removeSession(profile string) {
	m.mu.Lock()
	delete(m.sessions, profile)
	m.mu.Unlock()
}

// GetSession retorna cópia da sessão ativa para o perfil, ou nil.
func (m *AWSSSOManager) GetSession(profile string) *LoginSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[profile]; ok {
		cp := *s
		return &cp
	}
	return nil
}

// ─── AWS SSO OIDC HTTP ────────────────────────────────────────────────────────

func oidcBaseURL(region string) string {
	return fmt.Sprintf("https://oidc.%s.amazonaws.com", region)
}

func (m *AWSSSOManager) registerOIDCClient(ctx context.Context, region string) (*oidcClientInfo, error) {
	body := map[string]interface{}{
		"clientName": "k8s-hpa-manager",
		"clientType": "public",
		"scopes":     []string{"sso:account:access"},
	}
	var result oidcClientInfo
	if err := m.oidcPost(ctx, oidcBaseURL(region)+"/client/register", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *AWSSSOManager) startDeviceAuthorization(ctx context.Context, region string, client *oidcClientInfo, startURL string) (*deviceAuthInfo, error) {
	body := map[string]interface{}{
		"clientId":     client.ClientID,
		"clientSecret": client.ClientSecret,
		"startUrl":     startURL,
	}
	var result deviceAuthInfo
	if err := m.oidcPost(ctx, oidcBaseURL(region)+"/device_authorization", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *AWSSSOManager) pollToken(ctx context.Context, region string, client *oidcClientInfo, deviceCode string) (*oidcTokenInfo, error) {
	body := map[string]interface{}{
		"clientId":     client.ClientID,
		"clientSecret": client.ClientSecret,
		"grantType":    "urn:ietf:params:oauth:grant-type:device_code",
		"deviceCode":   deviceCode,
	}
	var result oidcTokenInfo
	if err := m.oidcPost(ctx, oidcBaseURL(region)+"/token", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *AWSSSOManager) oidcPost(ctx context.Context, url string, body interface{}, result interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rawBody bytes.Buffer
	if _, err := rawBody.ReadFrom(resp.Body); err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(rawBody.Bytes(), &errResp)
		if errResp.Error != "" {
			return fmt.Errorf("%s: %s", errResp.Error, errResp.ErrorDescription)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, rawBody.String())
	}

	return json.Unmarshal(rawBody.Bytes(), result)
}

// ─── Token cache ──────────────────────────────────────────────────────────────

// saveTokenCache salva o token no formato que o AWS CLI espera em ~/.aws/sso/cache/.
func (m *AWSSSOManager) saveTokenCache(startURL, region string, token *oidcTokenInfo) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cacheDir := filepath.Join(homeDir, ".aws", "sso", "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}

	// Nome do arquivo: SHA1 hex do startURL (sem \n).
	h := sha1.Sum([]byte(startURL))
	filename := fmt.Sprintf("%x.json", h)

	expiresAt := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)

	cache := map[string]interface{}{
		"startUrl":    startURL,
		"region":      region,
		"accessToken": token.AccessToken,
		"expiresAt":   expiresAt.Format("2006-01-02T15:04:05UTC"),
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(cacheDir, filename), data, 0600)
}

// ─── Config ───────────────────────────────────────────────────────────────────

// LoadConfig lê o arquivo aws-config.yaml completo.
func (m *AWSSSOManager) LoadConfig() (*AWSConfigFile, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AWSConfigFile{Profiles: make(map[string]*AWSProfileConfig)}, nil
		}
		return nil, fmt.Errorf("falha ao ler aws-config.yaml: %w", err)
	}
	var cfg AWSConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("falha ao parsear aws-config.yaml: %w", err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*AWSProfileConfig)
	}
	return &cfg, nil
}

// LoadProfileConfig lê a config de um perfil específico.
func (m *AWSSSOManager) LoadProfileConfig(profile string) (*AWSProfileConfig, error) {
	cfg, err := m.LoadConfig()
	if err != nil {
		return nil, err
	}
	if p, ok := cfg.Profiles[profile]; ok {
		return p, nil
	}
	return nil, nil
}

// SaveProfileConfig salva ou atualiza a config de um perfil.
func (m *AWSSSOManager) SaveProfileConfig(profile string, profileCfg *AWSProfileConfig) error {
	cfg, err := m.LoadConfig()
	if err != nil {
		return err
	}
	cfg.Profiles[profile] = profileCfg

	if err := os.MkdirAll(filepath.Dir(m.configPath), 0700); err != nil {
		return fmt.Errorf("falha ao criar diretório de config: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		return err
	}
	return m.ensureAWSProfile(profile, profileCfg)
}

// DeleteProfileConfig remove a config de um perfil.
func (m *AWSSSOManager) DeleteProfileConfig(profile string) error {
	cfg, err := m.LoadConfig()
	if err != nil {
		return err
	}
	delete(cfg.Profiles, profile)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath, data, 0600)
}

// ensureAWSProfile garante que o perfil existe em ~/.aws/config.
// Adiciona a entrada se ausente; não sobrescreve entradas existentes.
func (m *AWSSSOManager) ensureAWSProfile(profile string, cfg *AWSProfileConfig) error {
	if cfg == nil || cfg.SSO.StartURL == "" {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	awsConfigPath := filepath.Join(homeDir, ".aws", "config")
	if err := os.MkdirAll(filepath.Dir(awsConfigPath), 0700); err != nil {
		return err
	}

	existing, _ := os.ReadFile(awsConfigPath)
	header := fmt.Sprintf("[profile %s]", profile)
	if strings.Contains(string(existing), header) {
		return nil
	}

	ssoRegion := cfg.SSO.Region
	if ssoRegion == "" {
		ssoRegion = cfg.Region
	}
	region := cfg.Region
	if region == "" {
		region = ssoRegion
	}

	entry := fmt.Sprintf(`
[profile %s]
sso_start_url = %s
sso_region = %s
sso_account_id = %s
sso_role_name = %s
region = %s
output = json
`, profile, cfg.SSO.StartURL, ssoRegion, cfg.SSO.AccountID, cfg.SSO.RoleName, region)

	f, err := os.OpenFile(awsConfigPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("falha ao abrir ~/.aws/config: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}
