package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// BrowserConfig armazena a preferência de login SSO compartilhada entre todas as features que
// dirigem um browser real (ServiceNow e Teams) — hoje só o campo abaixo é lido/escrito de fato.
// Salva em ~/.k8s-hpa-manager/servicenow-browser.json (nome herdado de quando só o ServiceNow
// usava; mantido por compatibilidade com configs já existentes em disco — não é mais exclusivo
// do ServiceNow, ver AttemptSSOAutoLogin em sso_autologin.go).
type BrowserConfig struct {
	SSOLoginIdentifier string `json:"sso_login_identifier,omitempty"` // "email" ou "matricula" — qual campo do Perfil SSO usar no login Azure AD (padrão: "email")
}

func browserConfigPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".k8s-hpa-manager", "servicenow-browser.json")
}

// LoadBrowserConfig lê a configuração de browser. Retorna config vazia se não existe.
func LoadBrowserConfig() BrowserConfig {
	data, err := os.ReadFile(browserConfigPath())
	if err != nil {
		return BrowserConfig{}
	}
	var cfg BrowserConfig
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// SaveBrowserConfig persiste a configuração de browser.
func SaveBrowserConfig(cfg BrowserConfig) error {
	path := browserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
