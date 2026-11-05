package handlers

import (
	"regexp"
	"strings"
)

var ssoLinkRe = regexp.MustCompile(`https://github\.com/enterprises/\S+/sso\?\S+`)

// cloneFailureMessage transforma a saída do `git clone` que falhou numa mensagem que o usuário
// consegue agir: token sem SSO autorizado, credencial recusada ou, no mínimo, a linha "fatal:" do
// git (em vez de só "exit status 128").
func cloneFailureMessage(stderrLines []string, waitErr error) string {
	all := strings.Join(stderrLines, "\n")
	if strings.Contains(all, "SAML SSO") {
		msg := "O token do perfil não está autorizado para a organização (SAML SSO). " +
			"No GitHub, abra Settings > Developer settings > Personal access tokens, use Configure SSO no token e autorize a organização; depois tente de novo."
		if link := ssoLinkRe.FindString(all); link != "" {
			msg += " Link de autorização: " + link
		}
		return msg
	}
	if strings.Contains(all, "Authentication failed") || strings.Contains(all, "could not read Username") ||
		strings.Contains(all, "Invalid username or token") {
		return "O GitHub recusou a credencial. Confira o perfil escolhido e se o token ainda é válido."
	}
	for i := len(stderrLines) - 1; i >= 0; i-- {
		if rest, ok := strings.CutPrefix(stderrLines[i], "fatal: "); ok {
			return rest
		}
	}
	return waitErr.Error()
}
