package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// AADGroupLookup resolve, via Microsoft Graph REST API (token obtido pelo `az cli`), a
// quais grupos do Azure AD um e-mail arbitrário pertence.
//
// Não usa mais o subcomando `az ad user get-member-groups --id <email>` (versão anterior
// desta struct) — esse subcomando internamente chama a Graph action `getMemberGroups`
// (`POST /users/{id}/getMemberGroups`), que sofre bloqueio de CAE (Continuous Access
// Evaluation) no tenant desta empresa — o mesmo problema já documentado e corrigido em
// `azure_ad.go`'s `GetUserGroups` (que usa `GET /me/memberOf` em vez do subcomando `az ad`
// por esse exato motivo, mas só resolve o usuário ATUAL — "/me" não aceita e-mail
// arbitrário). Aqui replicamos a mesma técnica (chamada Graph direta via token, não a
// action `getMemberGroups`) generalizada para qualquer e-mail, usando
// `GET /users/{id}/transitiveMemberOf/microsoft.graph.group` — mesmo tipo de endpoint
// (leitura simples, não action), mesma imunidade ao bloqueio de CAE.
//
// Fallback de resolução de identidade (`resolveUserObjectID`): o segmento `{id}` do Graph
// só aceita Object ID ou userPrincipalName exato — falha com 404 se o e-mail informado for
// um endereço secundário (`mail`/`otherMails`) diferente do UPN da conta. Isso é o caso real
// das contas de nuvem `*.ca@via.com.br` (GCP/AWS, ver CloudAccountHintField no CLAUDE.md):
// a mesma pessoa tem uma identidade separada nessa conta secundária cujo UPN no Entra ID nem
// sempre é idêntico ao endereço `*.ca@via.com.br` digitado pelo analista — só o atributo
// `mail`/`otherMails` bate. Quando a tentativa direta por UPN falha, resolve o Object ID via
// `$filter` (mail/otherMails/userPrincipalName) antes de tentar de novo.
type AADGroupLookup struct {
	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

type cacheEntry struct {
	groups    []ADGroup
	expiresAt time.Time
}

const (
	memberGroupsCacheTTL = 10 * time.Minute
	graphAPIBaseURL      = "https://graph.microsoft.com/v1.0"
	graphRequestTimeout  = 20 * time.Second
)

// NewAADGroupLookup cria um resolver de grupos AAD por e-mail.
func NewAADGroupLookup() *AADGroupLookup {
	return &AADGroupLookup{
		cache: make(map[string]cacheEntry),
	}
}

// getGraphAccessToken obtém um token Graph via `az account get-access-token` — mesma
// técnica já validada em `azure_ad.go`'s `GetUserGroups`, evita o bloqueio de CAE do
// subcomando `az ad`.
func getGraphAccessToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, graphRequestTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "az", "account", "get-access-token",
		"--resource", "https://graph.microsoft.com",
		"--query", "accessToken", "-o", "tsv").Output()
	if err != nil {
		return "", fmt.Errorf("falha ao obter token Graph via az cli: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("token Graph vazio retornado pelo az cli")
	}
	return token, nil
}

// graphGet executa um GET autenticado contra a Graph API e decodifica a resposta em `out`.
// Retorna o status HTTP mesmo em caso de erro, para o chamador decidir se vale fallback
// (404 = não encontrado, tentar outra estratégia) ou erro definitivo (401/403/5xx).
func graphGet(ctx context.Context, token, requestURL string, extraHeaders map[string]string, out interface{}) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, graphRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, fmt.Errorf("falha ao montar requisição Graph: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: graphRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("falha ao chamar Graph API: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("graph API retornou %d: %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, fmt.Errorf("falha ao parsear resposta Graph: %w", err)
	}
	return resp.StatusCode, nil
}

// escapeODataLiteral escapa aspas simples num literal de string usado dentro de um
// `$filter` OData (`'` vira `''`) — defesa contra e-mails/entradas com aspas.
func escapeODataLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// resolveUserObjectID busca o Object ID de um usuário cujo e-mail (mail, otherMails ou
// userPrincipalName) bate com o informado — usado como fallback quando a busca direta por
// `/users/{email}` falha com 404 (e-mail é um endereço secundário, não o UPN da conta).
// `$filter` sobre `otherMails/any(...)` exige o header `ConsistencyLevel: eventual` (query
// avançada da Graph API sobre propriedade multi-valor).
func resolveUserObjectID(ctx context.Context, token, email string) (string, error) {
	escaped := escapeODataLiteral(email)
	filter := fmt.Sprintf(
		"mail eq '%s' or userPrincipalName eq '%s' or otherMails/any(x:x eq '%s')",
		escaped, escaped, escaped,
	)
	requestURL := fmt.Sprintf("%s/users?$filter=%s&$select=id,userPrincipalName,mail&$count=true",
		graphAPIBaseURL, url.QueryEscape(filter))

	var page struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if _, err := graphGet(ctx, token, requestURL, map[string]string{"ConsistencyLevel": "eventual"}, &page); err != nil {
		return "", err
	}
	if len(page.Value) == 0 {
		return "", fmt.Errorf("nenhum usuário encontrado no Entra ID com mail/UPN/otherMails = %s", email)
	}
	return page.Value[0].ID, nil
}

// fetchTransitiveGroups busca todos os grupos (transitivos, incluindo herdados via
// aninhamento) do usuário identificado por `identifier` (UPN, e-mail ou Object ID) —
// filtrado a `microsoft.graph.group` para não trazer directoryRoles/outros tipos de membro.
func fetchTransitiveGroups(ctx context.Context, token, identifier string) ([]ADGroup, int, error) {
	var groups []ADGroup
	nextURL := fmt.Sprintf("%s/users/%s/transitiveMemberOf/microsoft.graph.group?$select=id,displayName&$top=999",
		graphAPIBaseURL, url.PathEscape(identifier))

	for nextURL != "" {
		var page struct {
			Value    []ADGroup `json:"value"`
			NextLink string    `json:"@odata.nextLink"`
		}
		status, err := graphGet(ctx, token, nextURL, nil, &page)
		if err != nil {
			return nil, status, err
		}
		groups = append(groups, page.Value...)
		nextURL = page.NextLink
	}
	return groups, http.StatusOK, nil
}

// fetchMemberGroups busca TODOS os grupos (transitivos) do e-mail via Microsoft Graph.
// Tenta primeiro o e-mail como UPN direto (caminho rápido, cobre a maioria dos casos);
// se a Graph API não reconhecer esse identificador (404), resolve o Object ID via
// mail/otherMails e tenta de novo — cobre contas de nuvem secundárias (`*.ca@via.com.br`)
// cujo UPN não é o mesmo endereço usado no dia a dia.
func fetchMemberGroups(ctx context.Context, email string) ([]ADGroup, error) {
	token, err := getGraphAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	groups, status, err := fetchTransitiveGroups(ctx, token, email)
	if err == nil {
		return groups, nil
	}
	if status != http.StatusNotFound {
		// Erro definitivo (auth/permissão/rede) — fallback não ajudaria.
		return nil, fmt.Errorf("falha ao consultar grupos de %s no Entra ID: %w", email, err)
	}

	// 404 direto por UPN — tenta resolver por mail/otherMails antes de desistir.
	objectID, resolveErr := resolveUserObjectID(ctx, token, email)
	if resolveErr != nil {
		return nil, fmt.Errorf("usuário %s não encontrado no Entra ID (nem como UPN, nem via mail/otherMails): %w", email, resolveErr)
	}

	groups, _, err = fetchTransitiveGroups(ctx, token, objectID)
	if err != nil {
		return nil, fmt.Errorf("usuário %s resolvido (Object ID %s), mas falha ao buscar grupos: %w", email, objectID, err)
	}
	return groups, nil
}

// GetAllGroups devolve TODOS os grupos AAD do e-mail — resultado cacheado por
// memberGroupsCacheTTL.
func (l *AADGroupLookup) GetAllGroups(ctx context.Context, email string) ([]ADGroup, error) {
	emailKey := strings.ToLower(strings.TrimSpace(email))

	l.cacheMu.RLock()
	if entry, ok := l.cache[emailKey]; ok && time.Now().Before(entry.expiresAt) {
		l.cacheMu.RUnlock()
		return entry.groups, nil
	}
	l.cacheMu.RUnlock()

	groups, err := fetchMemberGroups(ctx, emailKey)
	if err != nil {
		return nil, err
	}

	l.cacheMu.Lock()
	l.cache[emailKey] = cacheEntry{groups: groups, expiresAt: time.Now().Add(memberGroupsCacheTTL)}
	l.cacheMu.Unlock()

	return groups, nil
}
