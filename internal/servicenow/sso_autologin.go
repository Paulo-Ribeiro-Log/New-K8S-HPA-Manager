package servicenow

import (
	"github.com/go-rod/rod"
	"github.com/rs/zerolog"

	"k8s-hpa-manager/internal/browser"
)

// ssoAutoLoginAttempt tenta auto-login com perfil SSO a partir da URL atual da página.
// Lógica movida pra internal/browser/sso_autologin.go (compartilhada com o Teams, modo
// Docker/embed) — ver BROWSER-CONSOLIDATION-STUDY.md. Mantido como wrapper local pra não exigir
// mudança nos call sites existentes em rod_extractor.go.
func ssoAutoLoginAttempt(page *rod.Page, sessionDir string, logger *zerolog.Logger) bool {
	return browser.AttemptSSOAutoLogin(page, sessionDir, logger)
}
