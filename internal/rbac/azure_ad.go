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

// GetUserGroups obtém grupos do usuário via Azure CLI
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

	// Executar comando Azure CLI
	cmd := exec.CommandContext(ctx, "az", "ad", "user", "get-member-groups",
		"--id", email, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}

	var groups []ADGroup
	if err := json.Unmarshal(output, &groups); err != nil {
		return nil, fmt.Errorf("failed to parse groups: %w", err)
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

// checkSREGroup verifica se lista de grupos contém VV_CLOUD_SRE
func (r *RBACManager) checkSREGroup(groups []ADGroup) bool {
	for _, group := range groups {
		if strings.EqualFold(group.DisplayName, "VV_CLOUD_SRE") {
			return true
		}
	}
	return false
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
