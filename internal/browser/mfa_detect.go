package browser

import (
	"time"

	"github.com/go-rod/rod"
)

// fieldDetectTimeout — teto curto pra checagem de presença do elemento, mesmo valor de
// fieldTimeout em TryAutoFillAzureAD (sso_autologin.go): evita bloquear ~3s (timeout default do
// go-rod) a cada iteração de loop quando a página não está na tela de MFA.
const fieldDetectTimeout = 800 * time.Millisecond

// DetectMFANumber procura o número do "number matching" do Microsoft Authenticator (a tela
// "Approve sign in request" do Azure AD, que pede pra digitar um número de 1-3 dígitos no
// celular). Selector confirmado empiricamente inspecionando a página real (login.microsoftonline
// .com/common/DeviceAuthTls/reprocess) durante um teste ao vivo do modo Docker do Teams:
// <div id="idRichContext_DisplaySign" class="displaySign display-sign-height">50</div>.
// Retorna ("", false) quando a página não está nessa tela — seguro chamar em toda iteração de um
// loop de espera, mesmo padrão de TryAutoFillAzureAD/AttemptSSOAutoLogin.
func DetectMFANumber(page *rod.Page) (string, bool) {
	el, err := page.Timeout(fieldDetectTimeout).Element("#idRichContext_DisplaySign")
	if err != nil {
		return "", false
	}
	text, err := el.Text()
	if err != nil || text == "" {
		return "", false
	}
	return text, true
}
