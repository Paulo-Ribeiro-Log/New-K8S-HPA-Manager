package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// AADGroupLookup resolve, via `az cli` (sem Graph API), a quais grupos do Azure AD um
// e-mail arbitrário pertence.
//
// Usa `az ad user get-member-groups --id <email>` — uma única chamada que já devolve
// TODOS os grupos do usuário — em vez de listar todos os grupos com um prefixo e checar
// membership um a um (`az ad group member check`), que não escala: um prefixo amplo como
// "VV_CLOUD_" retorna 800+ grupos no tenant, inviabilizando a checagem grupo-a-grupo em
// tempo de request.
type AADGroupLookup struct {
	groupPrefix string

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

type cacheEntry struct {
	groups    []ADGroup
	expiresAt time.Time
}

const memberGroupsCacheTTL = 10 * time.Minute

// NewAADGroupLookup cria um resolver para grupos com o prefixo informado (ex: "VV_CLOUD_").
func NewAADGroupLookup(groupPrefix string) *AADGroupLookup {
	return &AADGroupLookup{
		groupPrefix: groupPrefix,
		cache:       make(map[string]cacheEntry),
	}
}

// fetchMemberGroups busca TODOS os grupos do e-mail via `az ad user get-member-groups`.
// Aceita e-mail/UPN diretamente em --id (não precisa resolver Object ID antes).
func fetchMemberGroups(ctx context.Context, email string) ([]ADGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "az", "ad", "user", "get-member-groups",
		"--id", email,
		"--query", "[].{id:id,displayName:displayName}",
		"-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("usuário %s não encontrado no Azure AD ou sem grupos: %w", email, err)
	}

	var groups []ADGroup
	if err := json.Unmarshal(out, &groups); err != nil {
		return nil, fmt.Errorf("falha ao parsear grupos AAD: %w", err)
	}
	return groups, nil
}

// GetAllGroups devolve TODOS os grupos AAD do e-mail (sem filtro de prefixo) — usado
// para exibir a lista completa ao SRE, além do subconjunto usado para impersonation.
// Resultado cacheado por memberGroupsCacheTTL.
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

// ResolveVVCloudGroups devolve o subconjunto dos grupos do e-mail cujo nome começa com o
// prefixo configurado — são os GUIDs usados para impersonation no K8s.
func (l *AADGroupLookup) ResolveVVCloudGroups(ctx context.Context, email string) ([]ADGroup, error) {
	allGroups, err := l.GetAllGroups(ctx, email)
	if err != nil {
		return nil, err
	}

	var matched []ADGroup
	for _, g := range allGroups {
		if strings.HasPrefix(g.DisplayName, l.groupPrefix) {
			matched = append(matched, g)
		}
	}
	return matched, nil
}
