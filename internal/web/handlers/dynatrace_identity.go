package handlers

import (
	"strings"
	"sync"

	"k8s-hpa-manager/internal/storage"
)

// Identidade do usuário no Dynatrace (chave das credenciais no user_ai_tokens).
//
// Preferência: o e-mail do Perfil SSO corporativo (Credenciais → Perfil SSO), que o usuário
// controla. O e-mail do login do app (JWT) NÃO é escolhido pelo usuário: vem da conta ativa do
// Azure CLI (`az account show`) — que muitas vezes é uma identidade administrativa (ex:
// 4960023587.ca@...), não o e-mail corporativo. Com a identidade presa ao login, não havia como
// "alterar o e-mail do Dynatrace" e trocar de conta no `az` fazia a configuração sumir.
//
// O app roda localmente por usuário e o Perfil SSO é único por instalação, então substituir o
// e-mail recebido pelo do Perfil SSO vale para todo endpoint Dynatrace sem mudar contrato.
// Leitura com fallback para o e-mail do login (onde as credenciais ficavam antes desta correção):
// ninguém precisa reconfigurar; o próximo "Salvar" grava sob o e-mail do SSO.

var dynatraceSSODir struct {
	sync.RWMutex
	dir string
}

// SetDynatraceSSOProfileDir informa onde está o sso_profile.json (chamado pelo server na montagem).
func SetDynatraceSSOProfileDir(dir string) {
	dynatraceSSODir.Lock()
	dynatraceSSODir.dir = dir
	dynatraceSSODir.Unlock()
}

func dynatraceSSOEmail() string {
	dynatraceSSODir.RLock()
	dir := dynatraceSSODir.dir
	dynatraceSSODir.RUnlock()
	if dir == "" {
		return ""
	}
	return strings.TrimSpace(ssoProfileEmail(dir))
}

// dynatraceIdentity devolve o e-mail sob o qual as credenciais Dynatrace são gravadas e de onde
// ele veio ("sso" = Perfil SSO; "login" = login do app/az).
func dynatraceIdentity(loginEmail string) (email, source string) {
	if sso := dynatraceSSOEmail(); sso != "" {
		return sso, "sso"
	}
	return loginEmail, "login"
}

// dynatraceLookupEmails são as chaves de leitura, em ordem: identidade atual e, se diferente, o
// e-mail do login (credenciais gravadas antes desta correção).
func dynatraceLookupEmails(loginEmail string) []string {
	id, _ := dynatraceIdentity(loginEmail)
	out := []string{}
	for _, e := range []string{id, loginEmail} {
		if e != "" && (len(out) == 0 || !strings.EqualFold(out[0], e)) {
			out = append(out, e)
		}
	}
	return out
}

// dynatraceTokensFor devolve o registro que tem credenciais Dynatrace (identidade primeiro,
// depois o legado) e a chave em que ele foi achado. Sem credencial em nenhum: o registro da
// identidade (pode ter outros campos) ou vazio.
func dynatraceTokensFor(store *storage.UserTokensStore, loginEmail string) (*storage.UserTokens, string) {
	if store == nil {
		return &storage.UserTokens{}, ""
	}
	var first *storage.UserTokens
	firstKey := ""
	for _, e := range dynatraceLookupEmails(loginEmail) {
		t, err := store.GetTokens(e)
		if err != nil || t == nil {
			continue
		}
		if t.DynatraceURL != "" || t.DynatraceToken != "" || t.DynatraceHLGURL != "" || t.DynatraceHLGToken != "" {
			return t, e
		}
		if first == nil {
			first, firstKey = t, e
		}
	}
	if first == nil {
		return &storage.UserTokens{}, ""
	}
	return first, firstKey
}
