package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"k8s-hpa-manager/internal/ai"
)

// GCPAuthManager gerencia autenticação GCP via Device Authorization Grant (RFC 8628).
// Após autenticação, salva ADC JSON e configura GOOGLE_APPLICATION_CREDENTIALS para
// que o gke-gcloud-auth-plugin use automaticamente as credenciais.
type GCPAuthManager struct {
	mu       sync.Mutex
	sessions map[string]*gcpLoginSession
}

type gcpLoginSession struct {
	DeviceCode string
	UserCode   string
	VerifyURL  string
	ExpiresAt  time.Time
	Interval   int
	Done       chan error
}

// GCPAuthStatus descreve o estado atual da autenticação GCP.
type GCPAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Account       string `json:"account,omitempty"`
	HasGcloud     bool   `json:"has_gcloud"`
	HasADC        bool   `json:"has_adc"`
}

// GCPLoginResult retorna os dados para o frontend iniciar o fluxo.
type GCPLoginResult struct {
	SessionID   string    `json:"session_id"`
	UserCode    string    `json:"user_code"`
	VerifyURL   string    `json:"verify_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	IntervalSec int       `json:"interval_sec"`
}

func NewGCPAuthManager() *GCPAuthManager {
	return &GCPAuthManager{sessions: make(map[string]*gcpLoginSession)}
}

// CheckStatus verifica o estado atual da autenticação GCP.
func (m *GCPAuthManager) CheckStatus(ctx context.Context) GCPAuthStatus {
	status := GCPAuthStatus{}

	if _, err := exec.LookPath("gcloud"); err == nil {
		status.HasGcloud = true

		cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		out, err := exec.CommandContext(cmdCtx, "gcloud", "auth", "list",
			"--filter=status:ACTIVE", "--format=json").Output()
		if err == nil {
			var accounts []struct {
				Account string `json:"account"`
			}
			if json.Unmarshal(out, &accounts) == nil && len(accounts) > 0 {
				status.Authenticated = true
				status.Account = accounts[0].Account
			}
		}
	}

	adcPath := gcpADCPath()
	if _, err := os.Stat(adcPath); err == nil {
		status.HasADC = true
		if !status.Authenticated {
			status.Authenticated = isADCValid(adcPath)
		}
	}

	return status
}

// StartLogin inicia o Device Authorization Grant e retorna os dados para exibição ao usuário.
func (m *GCPAuthManager) StartLogin(ctx context.Context) (*GCPLoginResult, string, error) {
	code, err := ai.StartDeviceAuth(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("falha ao iniciar Device Auth: %w", err)
	}

	sessionID := fmt.Sprintf("gcp-%d", time.Now().UnixNano())
	session := &gcpLoginSession{
		DeviceCode: code.DeviceCode,
		UserCode:   code.UserCode,
		VerifyURL:  code.VerificationURL,
		ExpiresAt:  time.Now().Add(time.Duration(code.ExpiresIn) * time.Second),
		Interval:   code.Interval,
		Done:       make(chan error, 1),
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	go m.pollAndSave(context.Background(), sessionID, session)

	return &GCPLoginResult{
		SessionID:   sessionID,
		UserCode:    code.UserCode,
		VerifyURL:   code.VerificationURL,
		ExpiresAt:   session.ExpiresAt,
		IntervalSec: code.Interval,
	}, sessionID, nil
}

// PollStatus verifica se a sessão completou. Retorna (done, success).
func (m *GCPAuthManager) PollStatus(sessionID string) (done bool, success bool) {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	m.mu.Unlock()

	if !ok {
		return true, false
	}

	select {
	case err := <-session.Done:
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		return true, err == nil
	default:
		if time.Now().After(session.ExpiresAt) {
			m.mu.Lock()
			delete(m.sessions, sessionID)
			m.mu.Unlock()
			return true, false
		}
		return false, false
	}
}

// pollAndSave faz polling do token e salva as credenciais quando autenticado.
func (m *GCPAuthManager) pollAndSave(ctx context.Context, sessionID string, session *gcpLoginSession) {
	interval := session.Interval
	if interval <= 0 {
		interval = 5
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			session.Done <- fmt.Errorf("cancelado")
			return
		case <-ticker.C:
			token, pending, err := ai.TryExchangeDeviceToken(ctx, session.DeviceCode)
			if err != nil {
				session.Done <- err
				return
			}
			if pending {
				if time.Now().After(session.ExpiresAt) {
					session.Done <- fmt.Errorf("código expirado")
					return
				}
				continue
			}

			if saveErr := WriteGCPADCFile(token.AccessToken, token.RefreshToken); saveErr != nil {
				session.Done <- saveErr
				return
			}

			if token.RefreshToken != "" {
				go activateRefreshToken(context.Background(), token.AccessToken, token.RefreshToken)
			}

			session.Done <- nil
			return
		}
	}
}

// WriteGCPADCFile salva Application Default Credentials em ~/.k8s-hpa-manager/gcp-adc.json
// e configura GOOGLE_APPLICATION_CREDENTIALS no processo atual.
func WriteGCPADCFile(accessToken, refreshToken string) error {
	adcPath := gcpADCPath()
	if err := os.MkdirAll(filepath.Dir(adcPath), 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório ADC: %w", err)
	}

	adc := map[string]interface{}{
		"client_id":     "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com",
		"client_secret": "d-FL95Q19q7MQmFpd7hHD0Ty",
		"refresh_token": refreshToken,
		"type":          "authorized_user",
	}
	if accessToken != "" {
		adc["access_token"] = accessToken
	}

	data, err := json.MarshalIndent(adc, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar ADC: %w", err)
	}

	if err := os.WriteFile(adcPath, data, 0600); err != nil {
		return fmt.Errorf("falha ao salvar ADC: %w", err)
	}

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adcPath) //nolint:errcheck
	return nil
}

// activateRefreshToken tenta importar as credenciais no gcloud CLI para que
// comandos gcloud (ex: gcloud container clusters list) também funcionem.
func activateRefreshToken(ctx context.Context, accessToken, refreshToken string) {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return
	}

	account := resolveAccountEmail(ctx, accessToken)
	if account == "" {
		account = "k8s-hpa-manager@local"
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	exec.CommandContext(cmdCtx, //nolint:errcheck
		"gcloud", "auth", "activate-refresh-token",
		account, refreshToken, "--quiet",
	).Run()
}

// resolveAccountEmail obtém o email da conta via Google userinfo endpoint.
func resolveAccountEmail(ctx context.Context, accessToken string) string {
	if accessToken == "" {
		return ""
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET",
		"https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var info struct {
		Email string `json:"email"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return ""
	}
	return info.Email
}

// LoadSavedGCPADC carrega o ADC salvo anteriormente e define GOOGLE_APPLICATION_CREDENTIALS.
// Chamado na inicialização quando há clusters GKE no kubeconfig.
func LoadSavedGCPADC() {
	path := gcpADCPath()
	if isADCValid(path) {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path) //nolint:errcheck
	}
}

// gcpADCPath retorna o caminho do arquivo ADC da aplicação.
func gcpADCPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".k8s-hpa-manager", "gcp-adc.json")
}

// isADCValid verifica se o arquivo ADC contém um refresh_token.
func isADCValid(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var adc struct {
		RefreshToken string `json:"refresh_token"`
	}
	return json.Unmarshal(data, &adc) == nil && adc.RefreshToken != ""
}
