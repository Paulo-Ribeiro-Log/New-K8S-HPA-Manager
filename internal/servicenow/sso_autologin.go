package servicenow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/rs/zerolog"
	"k8s-hpa-manager/internal/pkg/nexus"
)

// ssoProfileOnDisk é a estrutura do sso_profile.json compartilhado com o handlers package.
type ssoProfileOnDisk struct {
	Email             string `json:"email"`
	Matricula         string `json:"matricula"`
	EncryptedPassword string `json:"encrypted_password"`
}

// loadSSOForAutoLogin carrega as credenciais SSO para auto-preenchimento do Azure AD.
// loginIdentifier define qual campo usar como username: "email" ou "matricula".
// Retorna ("", "", false) se o perfil não estiver configurado — sem erro, só silencia.
func loadSSOForAutoLogin(baseDir, loginIdentifier string) (username, password string, ok bool) {
	data, err := os.ReadFile(filepath.Join(baseDir, "sso_profile.json"))
	if err != nil {
		return "", "", false
	}
	var p ssoProfileOnDisk
	if err := json.Unmarshal(data, &p); err != nil {
		return "", "", false
	}
	if p.EncryptedPassword == "" {
		return "", "", false
	}
	pw, err := nexus.DecryptPassword(p.EncryptedPassword)
	if err != nil {
		return "", "", false
	}

	if loginIdentifier == "matricula" {
		if p.Matricula == "" {
			return "", "", false
		}
		return p.Matricula, pw, true
	}
	// default: email
	if p.Email == "" {
		return "", "", false
	}
	return p.Email, pw, true
}

// tryAutoFillAzureAD tenta preencher automaticamente o formulário do Azure AD.
// Detecta a fase atual (email ou senha) e preenche o campo correspondente.
// Retorna true se alguma ação foi tomada, false se a página não é Azure AD ou se falhou silenciosamente.
func tryAutoFillAzureAD(page *rod.Page, username, password string, logger *zerolog.Logger) bool {
	if username == "" || password == "" {
		return false
	}

	info, err := page.Info()
	if err != nil {
		return false
	}
	if !strings.Contains(info.URL, "login.microsoftonline.com") &&
		!strings.Contains(info.URL, "login.windows.net") &&
		!strings.Contains(info.URL, "login.live.com") {
		return false
	}

	// Fase 1 — campo de email
	if emailEl, err := page.Timeout(3 * time.Second).Element("input[name='loginfmt'], input[type='email']#i0116, input[type='email']"); err == nil {
		val, _ := emailEl.Text()
		if val == "" {
			if err := emailEl.SelectAllText(); err == nil {
				emailEl.Input(username) //nolint:errcheck
			} else {
				emailEl.Input(username) //nolint:errcheck
			}
			time.Sleep(500 * time.Millisecond)
			// Clicar em Next
			if nextBtn, err := page.Timeout(2 * time.Second).Element("#idSIButton9, input[type='submit']"); err == nil {
				nextBtn.Click(proto.InputMouseButtonLeft, 1) //nolint:errcheck
				logger.Info().Str("username", username).Msg("[Rod/SSO] Email preenchido e Next clicado")
				time.Sleep(1 * time.Second)
				return true
			}
		}
	}

	// Fase 2 — campo de senha
	if pwEl, err := page.Timeout(3 * time.Second).Element("input[name='passwd'], input[type='password']#i0118, input[type='password']"); err == nil {
		pwEl.Input(password) //nolint:errcheck
		time.Sleep(500 * time.Millisecond)
		if signInBtn, err := page.Timeout(2 * time.Second).Element("#idSIButton9, input[type='submit']"); err == nil {
			signInBtn.Click(proto.InputMouseButtonLeft, 1) //nolint:errcheck
			logger.Info().Msg("[Rod/SSO] Senha preenchida e Sign In clicado")
			time.Sleep(1500 * time.Millisecond)
			return true
		}
	}

	// "Stay signed in?" — clicar Yes para persistir sessão
	if yesBtn, err := page.Timeout(2 * time.Second).Element("#idSIButton9"); err == nil {
		text, _ := yesBtn.Text()
		if strings.Contains(strings.ToLower(text), "yes") || strings.Contains(strings.ToLower(text), "sim") {
			yesBtn.Click(proto.InputMouseButtonLeft, 1) //nolint:errcheck
			logger.Info().Msg("[Rod/SSO] 'Stay signed in' confirmado (Yes)")
			time.Sleep(1 * time.Second)
			return true
		}
	}

	return false
}

// ssoLoginIdentifier retorna o identificador configurado para o ServiceNow (email ou matricula).
// Lê do BrowserConfig.SSOLoginIdentifier; padrão é "email".
func ssoLoginIdentifier() string {
	cfg := LoadBrowserConfig()
	if cfg.SSOLoginIdentifier != "" {
		return cfg.SSOLoginIdentifier
	}
	return "email"
}

// baseDir retorna ~/.k8s-hpa-manager/ a partir do diretório de sessão do Rod.
func baseDirFromSessionDir(sessionDir string) string {
	return filepath.Dir(sessionDir)
}

// ssoAutoLoginAttempt tenta auto-login com perfil SSO a partir da URL atual da página.
// Chama tryAutoFillAzureAD apenas se a URL for uma página de login Azure AD.
// Retorna o identificador usado para log.
func ssoAutoLoginAttempt(page *rod.Page, sessionDir string, logger *zerolog.Logger) bool {
	baseDir := baseDirFromSessionDir(sessionDir)
	identifier := ssoLoginIdentifier()
	username, password, ok := loadSSOForAutoLogin(baseDir, identifier)
	if !ok {
		return false
	}
	logger.Info().
		Str("login_identifier", identifier).
		Str("username", fmt.Sprintf("%.3s***", username)).
		Msg("[Rod/SSO] Tentando auto-login com Perfil SSO corporativo")
	return tryAutoFillAzureAD(page, username, password, logger)
}
