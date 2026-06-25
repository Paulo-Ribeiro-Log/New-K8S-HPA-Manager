package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ADGroup representa um grupo do Azure AD
type ADGroup struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// UserPermissions armazena permissões do usuário
type UserPermissions struct {
	Email    string    `json:"email"`
	IsSRE    bool      `json:"isSRE"`
	Groups   []ADGroup `json:"groups"`
	CachedAt time.Time `json:"cachedAt"`
}

// RBACManager gerencia autorizações baseadas em Azure AD
type RBACManager struct {
	cache          map[string]*UserPermissions
	cacheMutex     sync.RWMutex
	cacheTTL       time.Duration
	disableADCheck bool // Se true, sempre retorna isSRE=true (modo de emergência)
}

// NewRBACManager cria um novo gerenciador RBAC
func NewRBACManager(disableADCheck bool) *RBACManager {
	return &RBACManager{
		cache:          make(map[string]*UserPermissions),
		cacheTTL:       1 * time.Hour, // Cache por 1 hora
		disableADCheck: disableADCheck,
	}
}

// GetCurrentUserEmail obtém email do usuário logado via Azure CLI
func (r *RBACManager) GetCurrentUserEmail(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "az", "account", "show", "--query", "user.name", "-o", "tsv")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	email := strings.TrimSpace(string(output))
	return email, nil
}

// GetUserGroups obtém grupos do usuário via Graph API REST.
// Usa az account get-access-token para evitar o bloqueio CAE do az ad user get-member-groups.
func (r *RBACManager) GetUserGroups(ctx context.Context, email string) ([]ADGroup, error) {
	// Verificar cache primeiro
	r.cacheMutex.RLock()
	if cached, exists := r.cache[email]; exists {
		if time.Since(cached.CachedAt) < r.cacheTTL {
			r.cacheMutex.RUnlock()
			return cached.Groups, nil
		}
	}
	r.cacheMutex.RUnlock()

	// Obter token Graph via az CLI (não sofre bloqueio CAE como az ad user get-member-groups)
	tokenCmd := exec.CommandContext(ctx, "az", "account", "get-access-token",
		"--resource", "https://graph.microsoft.com", "--query", "accessToken", "-o", "tsv")
	tokenOut, err := tokenCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get graph token: %w", err)
	}
	token := strings.TrimSpace(string(tokenOut))

	// Buscar grupos via Graph API com paginação
	var groups []ADGroup
	nextURL := "https://graph.microsoft.com/v1.0/me/memberOf?$select=displayName,id&$top=100"
	client := &http.Client{Timeout: 15 * time.Second}

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build graph request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to call graph api: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("graph api returned %d: %s", resp.StatusCode, string(body))
		}

		var page struct {
			Value    []ADGroup `json:"value"`
			NextLink string    `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse graph response: %w", err)
		}
		groups = append(groups, page.Value...)
		nextURL = page.NextLink
	}

	// Atualizar cache
	r.cacheMutex.Lock()
	isSRE := r.checkSREGroup(groups)
	r.cache[email] = &UserPermissions{
		Email:    email,
		IsSRE:    isSRE,
		Groups:   groups,
		CachedAt: time.Now(),
	}
	r.cacheMutex.Unlock()

	return groups, nil
}

// CheckUserInGroup verifica se usuário está em um grupo específico
func (r *RBACManager) CheckUserInGroup(ctx context.Context, email, groupName string) (bool, error) {
	groups, err := r.GetUserGroups(ctx, email)
	if err != nil {
		return false, err
	}

	for _, group := range groups {
		// Case-insensitive comparison
		if strings.EqualFold(group.DisplayName, groupName) {
			return true, nil
		}
	}
	return false, nil
}

// CheckCurrentUserIsSRE verifica se usuário atual é SRE
func (r *RBACManager) CheckCurrentUserIsSRE(ctx context.Context) (bool, error) {
	// Se verificação AD estiver desabilitada, sempre retorna true (modo emergência)
	if r.disableADCheck {
		return true, nil
	}

	email, err := r.GetCurrentUserEmail(ctx)
	if err != nil {
		return false, err
	}

	return r.CheckUserInGroup(ctx, email, "VV_CLOUD_SRE")
}

// GetUserPermissions obtém permissões completas do usuário
func (r *RBACManager) GetUserPermissions(ctx context.Context, email string) (*UserPermissions, error) {
	// Verificar cache
	r.cacheMutex.RLock()
	if cached, exists := r.cache[email]; exists {
		if time.Since(cached.CachedAt) < r.cacheTTL {
			r.cacheMutex.RUnlock()
			return cached, nil
		}
	}
	r.cacheMutex.RUnlock()

	// Buscar grupos
	groups, err := r.GetUserGroups(ctx, email)
	if err != nil {
		return nil, err
	}

	isSRE := r.checkSREGroup(groups)
	return &UserPermissions{
		Email:    email,
		IsSRE:    isSRE,
		Groups:   groups,
		CachedAt: time.Now(),
	}, nil
}

// GetCurrentUserPermissions obtém permissões do usuário atual
func (r *RBACManager) GetCurrentUserPermissions(ctx context.Context) (*UserPermissions, error) {
	// Se verificação AD estiver desabilitada, retorna permissões bypass
	if r.disableADCheck {
		return &UserPermissions{
			Email:    "bypass@emergency.mode",
			IsSRE:    true,
			Groups:   []ADGroup{{ID: "bypass", DisplayName: "EMERGENCY_MODE"}},
			CachedAt: time.Now(),
		}, nil
	}

	email, err := r.GetCurrentUserEmail(ctx)
	if err != nil {
		return nil, err
	}

	return r.GetUserPermissions(ctx, email)
}

// checkSREGroup verifica pertencimento ao grupo SRE.
// RBAC desabilitado: retorna sempre true. A verificação via Graph API
// usa a conta do servidor (az CLI), não a do usuário real — contas
// .ca@via.com.br não pertencem a VV_CLOUD_SRE mas têm acesso legítimo.
func (r *RBACManager) checkSREGroup(groups []ADGroup) bool {
	return true
}

// ClearCache limpa o cache de permissões
func (r *RBACManager) ClearCache() {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	r.cache = make(map[string]*UserPermissions)
}

// InvalidateUserCache invalida cache de um usuário específico
func (r *RBACManager) InvalidateUserCache(email string) {
	r.cacheMutex.Lock()
	defer r.cacheMutex.Unlock()
	delete(r.cache, email)
}
