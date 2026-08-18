package spinnaker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config é a configuração do usuário pra esta integração (seção 10 do plano) — tipo de login,
// URL do Deck por ambiente, e o projeto Spinnaker selecionado (agrupa as applications a
// consultar). Persistida em disco, mesmo padrão simples de internal/browser/sso_config.go
// (~/.k8s-hpa-manager/servicenow-browser.json) — sem SQLite, é config de poucos campos.
type Config struct {
	// LoginIdentifier: "email" ou "matricula" — qual campo do Perfil SSO já existente
	// (~/.k8s-hpa-manager/sso_profile.json) vira o username no POST /login. Campo próprio
	// desta integração, independente de BrowserConfig.SSOLoginIdentifier (usado só pro
	// auto-preenchimento do Azure AD via browser em ServiceNow/Teams).
	LoginIdentifier string `json:"login_identifier,omitempty"`

	// HLGBaseURL/PRDBaseURL são as URLs do Deck (UI), não do Gate (API) — o Gate é resolvido
	// em runtime via ResolveGateURL. Ex: "https://spinnaker-hlg.viavarejo.com.br/".
	HLGBaseURL string `json:"hlg_base_url,omitempty"`
	PRDBaseURL string `json:"prd_base_url,omitempty"`

	// SelectedProject é o nome do projeto Spinnaker escolhido no seletor (seção 10.2) — usado
	// pra resolver quais applications consultar em cada requisição.
	SelectedProject string `json:"selected_project,omitempty"`
}

func configPath(baseDir string) string {
	return filepath.Join(baseDir, "spinnaker_config.json")
}

// LoadConfig lê a configuração salva. Retorna Config{} (zero value) sem erro se o arquivo não
// existir ainda — mesmo padrão de LoadBrowserConfig, nunca é erro "não configurado ainda".
func LoadConfig(baseDir string) (Config, error) {
	data, err := os.ReadFile(configPath(baseDir))
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("ler spinnaker_config.json: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("spinnaker_config.json inválido: %w", err)
	}
	return cfg, nil
}

// SaveConfig persiste a configuração.
func SaveConfig(baseDir string, cfg Config) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("criar diretório de config: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar config: %w", err)
	}
	if err := os.WriteFile(configPath(baseDir), data, 0644); err != nil {
		return fmt.Errorf("gravar spinnaker_config.json: %w", err)
	}
	return nil
}

// envHLG/envPRD — únicos dois ambientes com Gate próprio confirmados até agora (seção 1 do
// plano). "sit" roda dentro do Gate de HLG, não é um terceiro host.
const (
	EnvHLG = "hlg"
	EnvPRD = "prd"
)

// DeckURLForEnv devolve a URL do Deck configurada pro ambiente, ou erro se não configurada.
func (c Config) DeckURLForEnv(env string) (string, error) {
	var url string
	switch env {
	case EnvHLG:
		url = c.HLGBaseURL
	case EnvPRD:
		url = c.PRDBaseURL
	default:
		return "", fmt.Errorf("ambiente %q desconhecido — esperado %q ou %q", env, EnvHLG, EnvPRD)
	}
	if url == "" {
		return "", fmt.Errorf("URL do Spinnaker (Deck) pra %q não configurada no perfil do usuário", env)
	}
	return url, nil
}

// EffectiveLoginIdentifier devolve LoginIdentifier ou "email" como default — mesmo default de
// BrowserConfig.SSOLoginIdentifier (internal/browser/sso_config.go) e de LoadSSOCredentials
// (internal/browser/sso_autologin.go, ramo "default: email").
func (c Config) EffectiveLoginIdentifier() string {
	if c.LoginIdentifier == "" {
		return "email"
	}
	return c.LoginIdentifier
}
