# RBAC com Azure AD - Implementação Completa

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## 🎯 Objetivo

Implementar controle de acesso baseado em grupos do Azure AD (Entra ID) para restringir operações críticas apenas a membros do grupo **`VV_CLOUD_SRE`**.

---

## 📊 Requisitos Funcionais

### Recursos Restritos (Apenas SREs)

**Backend (API):**
- ✅ `POST /api/v1/hpas/:cluster/:namespace/:name` - Aplicar mudanças em HPAs
- ✅ `POST /api/v1/nodepools/:cluster/:nodepool/apply` - Aplicar mudanças em Node Pools
- ✅ `POST /api/v1/nodepools/:cluster/:nodepool/cordon-drain` - Cordon/Drain de nodes
- ✅ `POST /api/v1/configmaps/:cluster/:namespace/:name` - Aplicar ConfigMaps
- ✅ `POST /api/v1/namespaces/:cluster/:name` - Criar/Aplicar Namespaces
- ✅ `DELETE /api/v1/*` - Qualquer operação de DELETE

**Frontend (UI):**
- ✅ Botões "Aplicar", "Apply", "Aplicar Agora"
- ✅ Botões de "Delete" em todas as abas
- ✅ Aba "Staging" - Painel "Apply All"
- ✅ Editor de ConfigMaps/Namespaces - Botões "Dry-run" e "Apply"

### Recursos Públicos (Todos os Usuários)

- ✅ Visualização de recursos (GET)
- ✅ Edição local (staging area)
- ✅ Save/Load de sessões
- ✅ Monitoramento e métricas
- ✅ Visualização de logs

---

## 🏗️ Arquitetura da Solução

```
┌─────────────────────────────────────────────────────────────┐
│                    Frontend (React/TS)                      │
│  - useUserPermissions() hook                                │
│  - Conditional rendering de botões                          │
│  - Toast de erro se unauthorized                            │
└─────────────────────┬───────────────────────────────────────┘
                      │ HTTP Requests
                      ▼
┌─────────────────────────────────────────────────────────────┐
│               Backend (Go/Gin) - Middleware                 │
│  - RequireSREGroup() middleware                             │
│  - Valida grupo em cada request protegido                   │
│  - Retorna 403 Forbidden se não autorizado                  │
└─────────────────────┬───────────────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│           internal/rbac/azure_ad.go (Novo)                  │
│  - CheckUserInGroup(email, groupName)                       │
│  - Cache de resultados (TTL 1h)                             │
│  - Azure CLI: az ad user get-member-groups                  │
└─────────────────────────────────────────────────────────────┘
```

---

## 🔧 Implementação Backend (Go)

### 1. Módulo RBAC (`internal/rbac/azure_ad.go`)

```go
package rbac

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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
	Email      string
	IsSRE      bool
	Groups     []ADGroup
	CachedAt   time.Time
}

// RBACManager gerencia autorizações baseadas em Azure AD
type RBACManager struct {
	cache      map[string]*UserPermissions
	cacheMutex sync.RWMutex
	cacheTTL   time.Duration
}

// NewRBACManager cria um novo gerenciador RBAC
func NewRBACManager() *RBACManager {
	return &RBACManager{
		cache:    make(map[string]*UserPermissions),
		cacheTTL: 1 * time.Hour, // Cache por 1 hora
	}
}

// GetCurrentUserEmail obtém email do usuário logado via Azure CLI
func (r *RBACManager) GetCurrentUserEmail(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "az", "account", "show", "--query", "user.name", "-o", "tsv")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	email := string(output)
	email = email[:len(email)-1] // Remove newline
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
		if group.DisplayName == groupName {
			return true, nil
		}
	}
	return false, nil
}

// CheckCurrentUserIsSRE verifica se usuário atual é SRE
func (r *RBACManager) CheckCurrentUserIsSRE(ctx context.Context) (bool, error) {
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

// checkSREGroup verifica se lista de grupos contém VV_CLOUD_SRE
func (r *RBACManager) checkSREGroup(groups []ADGroup) bool {
	for _, group := range groups {
		if group.DisplayName == "VV_CLOUD_SRE" {
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
```

---

### 2. Middleware HTTP (`internal/web/middleware/rbac.go`)

```go
package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/rbac"
)

// RBACMiddleware armazena o gerenciador RBAC
type RBACMiddleware struct {
	rbacManager *rbac.RBACManager
}

// NewRBACMiddleware cria um novo middleware RBAC
func NewRBACMiddleware(rbacManager *rbac.RBACManager) *RBACMiddleware {
	return &RBACMiddleware{
		rbacManager: rbacManager,
	}
}

// RequireSREGroup middleware que exige que usuário seja do grupo VV_CLOUD_SRE
func (m *RBACMiddleware) RequireSREGroup() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Verificar se usuário é SRE
		isSRE, err := m.rbacManager.CheckCurrentUserIsSRE(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to verify user permissions",
				"details": err.Error(),
			})
			c.Abort()
			return
		}

		if !isSRE {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Access denied",
				"message": "This operation requires SRE group membership (VV_CLOUD_SRE)",
			})
			c.Abort()
			return
		}

		// Armazenar permissões no contexto para uso posterior
		c.Set("isSRE", true)
		c.Next()
	}
}

// GetUserPermissions endpoint público para frontend verificar permissões
func (m *RBACMiddleware) GetUserPermissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		email, err := m.rbacManager.GetCurrentUserEmail(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to get current user",
			})
			return
		}

		perms, err := m.rbacManager.GetUserPermissions(ctx, email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to get user permissions",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"email": perms.Email,
			"isSRE": perms.IsSRE,
			"groups": perms.Groups,
		})
	}
}
```

---

### 3. Integração com Server (`internal/web/server.go`)

```go
// Adicionar ao setupRoutes()

// Inicializar RBAC Manager
rbacManager := rbac.NewRBACManager()
rbacMiddleware := middleware.NewRBACMiddleware(rbacManager)

// Endpoint público para verificar permissões
api.GET("/permissions", rbacMiddleware.GetUserPermissions())

// Aplicar middleware a rotas protegidas
protected := api.Group("")
protected.Use(rbacMiddleware.RequireSREGroup())
{
	// HPAs
	protected.POST("/hpas/:cluster/:namespace/:name", h.HPAHandler.Apply)

	// Node Pools
	protected.POST("/nodepools/:cluster/:nodepool/apply", h.NodePoolHandler.Apply)
	protected.POST("/nodepools/:cluster/:nodepool/cordon-drain", h.NodePoolHandler.CordonDrain)

	// ConfigMaps
	protected.POST("/configmaps/:cluster/:namespace/:name", h.ConfigMapHandler.Apply)
	protected.PUT("/configmaps/:cluster/:namespace/:name", h.ConfigMapHandler.Apply)

	// Namespaces
	protected.POST("/namespaces/:cluster", h.NamespaceHandler.Create)
	protected.PUT("/namespaces/:cluster/:name", h.NamespaceHandler.Apply)

	// Deletes (todas as resources)
	protected.DELETE("/hpas/:cluster/:namespace/:name", h.HPAHandler.Delete)
	protected.DELETE("/nodepools/:cluster/:nodepool", h.NodePoolHandler.Delete)
	protected.DELETE("/configmaps/:cluster/:namespace/:name", h.ConfigMapHandler.Delete)
	protected.DELETE("/namespaces/:cluster/:name", h.NamespaceHandler.Delete)
	protected.DELETE("/pods/:cluster/:namespace/:name", h.PodHandler.Delete)
}
```

---

## 🎨 Implementação Frontend (React/TypeScript)

### 1. Hook de Permissões (`hooks/useUserPermissions.ts`)

```typescript
import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/lib/api/client';

interface UserPermissions {
  email: string;
  isSRE: boolean;
  groups: Array<{
    id: string;
    displayName: string;
  }>;
}

export function useUserPermissions() {
  return useQuery<UserPermissions>({
    queryKey: ['user-permissions'],
    queryFn: async () => {
      const response = await apiClient.get('/api/v1/permissions');
      return response.data;
    },
    staleTime: 1000 * 60 * 60, // Cache por 1 hora
    retry: 1,
  });
}
```

---

### 2. Componente de Proteção (`components/ProtectedAction.tsx`)

```typescript
import { ReactNode } from 'react';
import { useUserPermissions } from '@/hooks/useUserPermissions';
import { Button } from '@/components/ui/button';
import { toast } from 'sonner';
import { ShieldAlert } from 'lucide-react';

interface ProtectedActionProps {
  children: ReactNode;
  fallback?: ReactNode;
  showWarning?: boolean;
}

export function ProtectedAction({
  children,
  fallback = null,
  showWarning = true
}: ProtectedActionProps) {
  const { data: permissions, isLoading } = useUserPermissions();

  if (isLoading) {
    return null; // Ou skeleton
  }

  if (!permissions?.isSRE) {
    if (showWarning) {
      return (
        <Button
          variant="outline"
          disabled
          onClick={() => {
            toast.error('Acesso negado', {
              description: 'Esta operação requer permissão de SRE (grupo VV_CLOUD_SRE)',
              icon: <ShieldAlert className="h-4 w-4" />,
            });
          }}
        >
          {fallback || children}
        </Button>
      );
    }
    return null;
  }

  return <>{children}</>;
}
```

---

### 3. Exemplo de Uso no Frontend

```typescript
// Exemplo 1: Botão de Apply em HPAEditor
<ProtectedAction>
  <Button onClick={handleApply}>
    <Save className="mr-2 h-4 w-4" />
    Aplicar Mudanças
  </Button>
</ProtectedAction>

// Exemplo 2: Botão de Delete (com fallback customizado)
<ProtectedAction fallback="Delete (SRE Only)">
  <Button variant="destructive" onClick={handleDelete}>
    <Trash2 className="mr-2 h-4 w-4" />
    Deletar
  </Button>
</ProtectedAction>

// Exemplo 3: Ocultar completamente se não for SRE
<ProtectedAction showWarning={false}>
  <DropdownMenuItem onClick={handleDangerousAction}>
    <AlertTriangle className="mr-2 h-4 w-4" />
    Ação Crítica
  </DropdownMenuItem>
</ProtectedAction>

// Exemplo 4: Condicional no código
const { data: permissions } = useUserPermissions();

if (permissions?.isSRE) {
  // Renderizar features de SRE
  return <SREDashboard />;
}

return <ReadOnlyDashboard />;
```

---

## 🧪 Testes

### Teste Manual (Azure CLI)

```bash
# 1. Verificar usuário atual
az account show --query user.name -o tsv

# 2. Verificar grupos do usuário
USER_EMAIL=$(az account show --query user.name -o tsv)
az ad user get-member-groups --id "$USER_EMAIL" -o json

# 3. Verificar se está no grupo VV_CLOUD_SRE
az ad user get-member-groups --id "$USER_EMAIL" -o json | jq '.[] | select(.displayName == "VV_CLOUD_SRE")'

# Resultado esperado:
# {
#   "displayName": "VV_CLOUD_SRE",
#   "id": "eb865ea5-2672-49be-abc8-74c248c556b0"
# }
```

### Teste Backend (Go)

```go
// internal/rbac/azure_ad_test.go
package rbac

import (
	"context"
	"testing"
)

func TestCheckCurrentUserIsSRE(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	isSRE, err := manager.CheckCurrentUserIsSRE(ctx)
	if err != nil {
		t.Fatalf("Failed to check SRE status: %v", err)
	}

	t.Logf("User is SRE: %v", isSRE)
}

func TestGetUserGroups(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	email, err := manager.GetCurrentUserEmail(ctx)
	if err != nil {
		t.Fatalf("Failed to get current user: %v", err)
	}

	groups, err := manager.GetUserGroups(ctx, email)
	if err != nil {
		t.Fatalf("Failed to get user groups: %v", err)
	}

	t.Logf("User %s has %d groups", email, len(groups))
	for _, group := range groups {
		t.Logf("  - %s (ID: %s)", group.DisplayName, group.ID)
	}
}
```

---

## 🔒 Segurança

### Considerações Importantes

1. **Cache com TTL**: Permissões são cacheadas por 1 hora para performance
2. **Timeout**: Comandos Azure CLI têm timeout de 10 segundos
3. **Validação Server-Side**: Frontend apenas oculta UI, backend sempre valida
4. **Auditoria**: Logar tentativas de acesso não autorizado
5. **Graceful Degradation**: Se Azure CLI falhar, negar acesso por segurança

### Logs de Auditoria (Opcional)

```go
// internal/rbac/audit.go
package rbac

import (
	"log"
	"time"
)

type AuditLog struct {
	Timestamp time.Time
	Email     string
	Action    string
	Resource  string
	Allowed   bool
	Reason    string
}

func (r *RBACManager) LogAccessAttempt(email, action, resource string, allowed bool, reason string) {
	log := AuditLog{
		Timestamp: time.Now(),
		Email:     email,
		Action:    action,
		Resource:  resource,
		Allowed:   allowed,
		Reason:    reason,
	}

	// Salvar em arquivo ou database
	// ...
}
```

---

## 📋 Checklist de Implementação

- [ ] Criar `internal/rbac/azure_ad.go`
- [ ] Criar `internal/web/middleware/rbac.go`
- [ ] Adicionar endpoint `GET /api/v1/permissions`
- [ ] Proteger rotas sensíveis com middleware
- [ ] Criar hook `useUserPermissions()` no frontend
- [ ] Criar componente `<ProtectedAction>`
- [ ] Atualizar todos os botões de "Apply" e "Delete"
- [ ] Testar com usuário SRE
- [ ] Testar com usuário não-SRE (criar conta de teste)
- [ ] Adicionar logs de auditoria (opcional)
- [ ] Documentar no README

---

## 🚀 Deploy

```bash
# 1. Backend
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA
go mod tidy
go build -o build/new-k8s-hpa

# 2. Frontend
cd internal/web/frontend
npm install
npm run build

# 3. Restart servidor
./rebuild-web.sh -b

# 4. Testar permissões
curl http://localhost:8080/api/v1/permissions
```

---

## 📚 Referências

- [Azure CLI - az ad user get-member-groups](https://learn.microsoft.com/en-us/cli/azure/ad/user?view=azure-cli-latest#az-ad-user-get-member-groups)
- [Azure CLI - az ad signed-in-user](https://learn.microsoft.com/en-us/cli/azure/ad/signed-in-user?view=azure-cli-latest)
- [Gin Middleware](https://gin-gonic.com/docs/examples/custom-middleware/)
- [React Query - Authentication](https://tanstack.com/query/latest/docs/framework/react/guides/authentication)

---

**Happy coding!** 🚀
