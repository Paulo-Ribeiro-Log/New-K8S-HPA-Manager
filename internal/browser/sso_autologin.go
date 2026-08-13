package browser

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

// ssoProfileOnDisk é a estrutura do sso_profile.json ("Perfil SSO corporativo centralizado",
// ~/.k8s-hpa-manager/sso_profile.json) — compartilhado com internal/web/handlers/sso_profile.go.
type ssoProfileOnDisk struct {
	Email             string `json:"email"`
	Matricula         string `json:"matricula"`
	EncryptedPassword string `json:"encrypted_password"`
}

// LoadSSOCredentials carrega as credenciais SSO para auto-preenchimento do Azure AD.
// loginIdentifier define qual campo usar como username: "email" ou "matricula".
// Retorna ("", "", false) se o perfil não estiver configurado — sem erro, só silencia.
func LoadSSOCredentials(baseDir, loginIdentifier string) (username, password string, ok bool) {
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

// TryAutoFillAzureAD tenta preencher automaticamente o formulário do Azure AD.
// Detecta a fase atual (email ou senha) e preenche o campo correspondente.
// Retorna true se alguma ação foi tomada, false se a página não é Azure AD ou se falhou silenciosamente.
func TryAutoFillAzureAD(page *rod.Page, username, password string, logger *zerolog.Logger) bool {
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

	// Timeout curto para cada seletor: evita bloquear 3s quando o campo não existe na fase atual
	const fieldTimeout = 800 * time.Millisecond

	// Fase 1 — campo de email
	//
	// Bug real corrigido (achado ao vivo testando o modo Docker do Teams): o clique em Next só
	// era tentado DENTRO do `if val == ""` — se o campo já preenchido (por nós numa iteração
	// anterior, ou por autofill nativo do próprio Chrome/Microsoft) mas o clique em si falhasse
	// silenciosamente (ex: page.Timeout(2s).Element(...) não achou o botão a tempo por lentidão
	// de render), a próxima chamada via AttemptSSOAutoLogin encontrava `val != ""` e nunca mais
	// tentava clicar — ficava preso pra sempre com o campo preenchido mas o formulário nunca
	// submetido, até estourar o teto de segurança do loop chamador. Corrigido: preencher (só se
	// vazio) e tentar o clique são duas ações independentes agora — o clique é tentado sempre que
	// o campo tiver algum valor, preenchido por nós ou não.
	if emailEl, err := page.Timeout(fieldTimeout).Element("input[name='loginfmt'], input[type='email']#i0116, input[type='email']"); err == nil {
		// Bug real corrigido (2ª rodada, achado ao vivo logo depois do fix acima): o campo de
		// email PERMANECE no DOM depois de avançar pra fase de senha — só some visualmente,
		// virando um chip minúsculo (10x13px) marcado aria-hidden="true" + tabIndex=-1.
		// checkVisibility()/offsetWidth continuam reportando "visível" nesse estado (não é
		// display:none nem visibility:hidden de verdade), então o fix acima (clicar sempre que
		// val!="") reintroduziu o mesmo travamento por outro caminho: a cada iteração, achava
		// esse campo fantasma com valor preenchido e clicava "Next" de novo, indefinidamente,
		// sem NUNCA chegar na Fase 2 (senha) — sintoma observado: "Email preenchido e Next
		// clicado" repetindo a cada ~7s, tela mostrando "Please enter your password" (Next
		// clicado com o campo de senha vazio). aria-hidden/tabIndex=-1 é o sinal real e confiável
		// de "esta fase já passou" — ausente/false enquanto o campo ainda é o atual.
		emailPhaseActive := true
		if hidden, evalErr := emailEl.Eval(`() => this.getAttribute('aria-hidden') === 'true' || this.tabIndex === -1`); evalErr == nil && hidden != nil && hidden.Value.Bool() {
			emailPhaseActive = false
		}
		// IMPORTANTE: usar JS (.value) em vez de Text() (.innerText) — inputs sempre têm innerText=""
		val := ""
		if emailPhaseActive {
			if res, evalErr := emailEl.Eval(`() => this.value`); evalErr == nil && res != nil {
				val = res.Value.String()
			}
			if val == "" {
				emailEl.SelectAllText() //nolint:errcheck
				emailEl.Input(username) //nolint:errcheck
				time.Sleep(500 * time.Millisecond)
				val = username
			}
		}
		if emailPhaseActive && val != "" {
			if nextBtn, err := page.Timeout(2 * time.Second).Element("#idSIButton9, input[type='submit']"); err == nil {
				nextBtn.Click(proto.InputMouseButtonLeft, 1) //nolint:errcheck
				if logger != nil {
					logger.Info().Str("username", username).Msg("[browser/SSO] Email preenchido e Next clicado")
				}
				time.Sleep(1 * time.Second)
				return true
			}
		}
	}

	// Fase 2 — campo de senha (mesmo padrão corrigido da Fase 1: preencher e clicar são ações
	// independentes, clique sempre tentado quando o campo tem valor).
	if pwEl, err := page.Timeout(fieldTimeout).Element("input[name='passwd'], input[type='password']#i0118, input[type='password']"); err == nil {
		pwVal := ""
		if res, evalErr := pwEl.Eval(`() => this.value`); evalErr == nil && res != nil {
			pwVal = res.Value.String()
		}
		if pwVal == "" {
			pwEl.SelectAllText() //nolint:errcheck
			pwEl.Input(password) //nolint:errcheck
			time.Sleep(500 * time.Millisecond)
			pwVal = password
		}
		if pwVal != "" {
			if signInBtn, err := page.Timeout(2 * time.Second).Element("#idSIButton9, input[type='submit']"); err == nil {
				signInBtn.Click(proto.InputMouseButtonLeft, 1) //nolint:errcheck
				if logger != nil {
					logger.Info().Msg("[browser/SSO] Senha preenchida e Sign In clicado")
				}
				time.Sleep(1500 * time.Millisecond)
				return true
			}
		}
	}

	// "Stay signed in?" — clicar Yes para persistir sessão
	if yesBtn, err := page.Timeout(2 * time.Second).Element("#idSIButton9"); err == nil {
		text, _ := yesBtn.Text()
		if strings.Contains(strings.ToLower(text), "yes") || strings.Contains(strings.ToLower(text), "sim") {
			yesBtn.Click(proto.InputMouseButtonLeft, 1) //nolint:errcheck
			if logger != nil {
				logger.Info().Msg("[browser/SSO] 'Stay signed in' confirmado (Yes)")
			}
			time.Sleep(1 * time.Second)
			return true
		}
	}

	return false
}

// SSOLoginIdentifier retorna o identificador configurado (email ou matricula) pro Perfil SSO
// compartilhado. Lê do BrowserConfig.SSOLoginIdentifier; padrão é "email".
func SSOLoginIdentifier() string {
	cfg := LoadBrowserConfig()
	if cfg.SSOLoginIdentifier != "" {
		return cfg.SSOLoginIdentifier
	}
	return "email"
}

// AttemptSSOAutoLogin tenta auto-login com o Perfil SSO corporativo a partir da URL atual da
// página — usado tanto pelo ServiceNow (rod_extractor.go) quanto pelo Teams (discover.go, modo
// Docker/embed) pra evitar que o usuário precise digitar email/senha manualmente numa tela de
// login Azure AD, já que a app já conhece essa credencial via Perfil SSO. sessionDir é o
// UserDataDir do browser (~/.k8s-hpa-manager/<algo>-session/) — sso_profile.json vive um nível
// acima, em ~/.k8s-hpa-manager/ diretamente. Retorna o identificador usado para log.
func AttemptSSOAutoLogin(page *rod.Page, sessionDir string, logger *zerolog.Logger) bool {
	baseDir := filepath.Dir(sessionDir)
	identifier := SSOLoginIdentifier()
	username, password, ok := LoadSSOCredentials(baseDir, identifier)
	if !ok {
		return false
	}
	if logger != nil {
		logger.Info().
			Str("login_identifier", identifier).
			Str("username", fmt.Sprintf("%.3s***", username)).
			Msg("[browser/SSO] Tentando auto-login com Perfil SSO corporativo")
	}
	return TryAutoFillAzureAD(page, username, password, logger)
}
