# 🔒 RBAC Azure AD - Checklist de Integração

**Data**: 10 de dezembro de 2025
**Status**: ✅ Implementação completa - Pronto para integração

---

## ✅ O que já foi feito

- [x] Módulo RBAC backend (`internal/rbac/azure_ad.go`)
- [x] Middleware HTTP (`internal/web/middleware/rbac.go`)
- [x] Testes Go (`internal/rbac/azure_ad_test.go`)
- [x] Hook React (`hooks/useUserPermissions.ts`)
- [x] Componentes React (`components/rbac/ProtectedAction.tsx`, `SREBadge.tsx`)
- [x] Script de teste automatizado (`testes/test-rbac.sh`)
- [x] Documentação completa (RBAC_AZURE_AD_IMPLEMENTATION.md, RBAC_SUMMARY.md)
- [x] Atualização do CLAUDE.md com referência à feature
- [x] Verificação: Você (paulo.gribeiro@viavarejo.com.br) é membro do grupo VV_CLOUD_SRE ✅

---

## 📋 Próximos Passos (Integração)

### 1. Backend - Adicionar RBAC ao Server (5min)

**Arquivo**: `internal/web/server.go`

```go
// Adicionar imports
import (
    "k8s-hpa-manager/internal/rbac"
    "k8s-hpa-manager/internal/web/middleware"
)

// No método setupRoutes(), adicionar após a criação do router:
func (s *Server) setupRoutes() {
    // ... código existente ...

    // ✨ NOVO: Inicializar RBAC
    rbacManager := rbac.NewRBACManager()
    rbacMiddleware := middleware.NewRBACMiddleware(rbacManager)

    // ✨ NOVO: Endpoints de permissões (públicos)
    api := s.router.Group("/api/v1")
    api.GET("/permissions", rbacMiddleware.GetUserPermissions())
    api.POST("/permissions/refresh", rbacMiddleware.RefreshPermissions())

    // ✨ NOVO: Rotas protegidas (apenas SRE)
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

        // Deletes
        protected.DELETE("/hpas/:cluster/:namespace/:name", h.HPAHandler.Delete)
        protected.DELETE("/nodepools/:cluster/:nodepool", h.NodePoolHandler.Delete)
        protected.DELETE("/configmaps/:cluster/:namespace/:name", h.ConfigMapHandler.Delete)
        protected.DELETE("/namespaces/:cluster/:name", h.NamespaceHandler.Delete)
        protected.DELETE("/pods/:cluster/:namespace/:name", h.PodHandler.Delete)
    }

    // ... resto do código existente ...
}
```

**Testar:**
```bash
go build -o build/new-k8s-hpa
./build/new-k8s-hpa web -f

# Em outro terminal:
curl http://localhost:8080/api/v1/permissions | jq
```

---

### 2. Frontend - Adicionar SREBadge ao Header (2min)

**Arquivo**: `internal/web/frontend/src/components/layout/Header.tsx` (ou equivalente)

```typescript
import { SREBadge } from '@/components/rbac';

export function Header() {
  return (
    <header className="...">
      {/* ... elementos existentes ... */}

      {/* ✨ NOVO: Badge de status SRE */}
      <div className="flex items-center gap-4">
        <SREBadge />
      </div>
    </header>
  );
}
```

---

### 3. Frontend - Proteger Botões (15-30min)

#### 3.1. HPAEditor / StagingPanel

**Arquivos**:
- `internal/web/frontend/src/components/staging/StagingPanel.tsx`
- `internal/web/frontend/src/components/hpas/HPAEditor.tsx`

```typescript
import { ProtectedAction } from '@/components/rbac';

// Exemplo: Botão "Aplicar Tudo"
<ProtectedAction>
  <Button onClick={handleApplyAll}>
    <Save className="mr-2 h-4 w-4" />
    Aplicar Tudo ({stagedItems.length})
  </Button>
</ProtectedAction>

// Exemplo: Botão "Aplicar Agora"
<ProtectedAction>
  <Button onClick={handleApplyNow}>
    Aplicar Agora
  </Button>
</ProtectedAction>
```

#### 3.2. ConfigMapsTab

**Arquivo**: `internal/web/frontend/src/components/configmaps/ConfigMapsTab.tsx`

```typescript
import { ProtectedAction } from '@/components/rbac';

// Botão Apply
<ProtectedAction>
  <Button onClick={handleApplyYAML}>
    <CheckCircle2 className="mr-2 h-4 w-4" />
    Apply
  </Button>
</ProtectedAction>

// Botão Delete
<ProtectedAction>
  <Button variant="destructive" onClick={handleDelete}>
    <Trash2 className="mr-2 h-4 w-4" />
    Delete
  </Button>
</ProtectedAction>

// Botão Dry-run (pode ficar público ou protegido, você decide)
<ProtectedAction>
  <Button onClick={handleDryRun}>
    <TriangleAlert className="mr-2 h-4 w-4" />
    Dry-run
  </Button>
</ProtectedAction>
```

#### 3.3. NamespacesTab

**Arquivo**: `internal/web/frontend/src/components/namespaces/NamespacesTab.tsx`

```typescript
// Botão Criar Namespace
<ProtectedAction>
  <Button onClick={handleCreateNamespace}>
    <Plus className="mr-2 h-4 w-4" />
    Criar Namespace
  </Button>
</ProtectedAction>

// Botão Apply (editor YAML)
<ProtectedAction>
  <Button onClick={handleApply}>
    Aplicar
  </Button>
</ProtectedAction>

// Botão Delete (dropdown menu)
<ProtectedAction showWarning={false}>
  <DropdownMenuItem onClick={handleDelete} className="text-destructive">
    <Trash2 className="mr-2 h-4 w-4" />
    Delete
  </DropdownMenuItem>
</ProtectedAction>
```

#### 3.4. NodePoolsTab

**Arquivo**: `internal/web/frontend/src/components/nodepools/NodePoolsTab.tsx`

```typescript
// Botão Aplicar Agora
<ProtectedAction>
  <Button onClick={handleApplyNow}>
    Aplicar Agora
  </Button>
</ProtectedAction>

// Botão Cordon/Drain
<ProtectedAction>
  <Button onClick={handleCordonDrain}>
    Cordon/Drain
  </Button>
</ProtectedAction>
```

#### 3.5. PodsTab

**Arquivo**: `internal/web/frontend/src/components/pods/PodsTab.tsx`

```typescript
// Botão Delete Pod
<ProtectedAction>
  <Button variant="destructive" onClick={handleDeletePod}>
    <Trash2 className="mr-2 h-4 w-4" />
    Delete Pod
  </Button>
</ProtectedAction>
```

---

### 4. Build e Deploy (5min)

```bash
# Backend
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA
go build -o build/new-k8s-hpa

# Frontend
cd internal/web/frontend
npm install  # Se houver novos packages
npm run build

# Ou usar script all-in-one
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA
./rebuild-web.sh -b

# Iniciar servidor
./build/new-k8s-hpa web -f
```

---

### 5. Testes de Validação (10min)

#### 5.1. Teste Backend
```bash
# Terminal 1: Servidor rodando
./build/new-k8s-hpa web -f

# Terminal 2: Testes
# Verificar permissões
curl http://localhost:8080/api/v1/permissions | jq

# Tentar operação protegida (deve funcionar se você é SRE)
curl -X POST http://localhost:8080/api/v1/hpas/cluster/namespace/hpa-name \
  -H "Content-Type: application/json" \
  -d '{"minReplicas": 3}'

# Verificar resposta: 200 OK (SRE) ou 403 Forbidden (não-SRE)
```

#### 5.2. Teste Frontend
1. Acessar `http://localhost:8080`
2. Verificar badge SRE no header:
   - ✅ Se SRE: Badge verde "SRE"
   - ⚠️ Se não-SRE: Badge cinza "Read-Only"
3. Clicar na badge → Ver popover com grupos
4. Tentar clicar em "Aplicar Tudo" na aba Staging:
   - ✅ Se SRE: Botão habilitado e funcional
   - ❌ Se não-SRE: Botão desabilitado com toast "Acesso negado"
5. Verificar outros botões (Delete, Apply, etc.)

#### 5.3. Teste de Não-SRE (Simulação)
Para testar comportamento de usuário não-SRE, você pode:

**Opção 1: Mock no código (temporário)**
```go
// internal/rbac/azure_ad.go (APENAS PARA TESTE)
func (r *RBACManager) CheckCurrentUserIsSRE(ctx context.Context) (bool, error) {
    return false, nil  // Simular não-SRE
}
```

**Opção 2: Criar conta de teste no Azure AD**
- Criar usuário teste que NÃO esteja no grupo VV_CLOUD_SRE
- Fazer login com essa conta
- Testar aplicação

---

## 📊 Checklist de Verificação Final

- [ ] Backend compila sem erros (`go build -o build/new-k8s-hpa`)
- [ ] Testes Go passam (`go test ./internal/rbac`)
- [ ] Frontend compila sem erros (`npm run build`)
- [ ] Endpoint `/permissions` retorna dados corretos
- [ ] Badge SRE aparece no header
- [ ] Botões protegidos ficam desabilitados para não-SRE
- [ ] Operações POST/PUT/DELETE retornam 403 para não-SRE
- [ ] Cache de permissões funciona (2ª chamada mais rápida)
- [ ] Refresh de permissões funciona via popover

---

## 🔧 Troubleshooting

### Erro: "failed to get current user"
**Causa**: Azure CLI não está logado ou falha de autenticação
**Solução**:
```bash
az login
az account show  # Verificar login
```

### Erro: "failed to get user groups"
**Causa**: Usuário não tem permissões no Azure AD ou timeout
**Solução**:
```bash
# Testar manualmente
az ad user get-member-groups --id $(az account show --query user.name -o tsv) -o json

# Aumentar timeout (internal/web/middleware/rbac.go)
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
```

### Badge SRE não aparece
**Causa**: Endpoint `/permissions` não está retornando dados
**Solução**:
```bash
# Verificar logs do servidor
tail -f /tmp/k8s-hpa-manager-web-*.log

# Testar endpoint diretamente
curl http://localhost:8080/api/v1/permissions

# Verificar console do navegador (F12) para erros
```

### Botões continuam habilitados para não-SRE
**Causa**: Componente `<ProtectedAction>` não foi aplicado
**Solução**: Revisar código do componente e garantir que está envolvendo o botão corretamente

---

## 🎯 Features Opcionais (Futuras)

Se você quiser estender a funcionalidade no futuro:

- [ ] Auditoria de tentativas de acesso (salvar em DB)
- [ ] Logs estruturados JSON para SIEM
- [ ] Suporte a múltiplos grupos (VV_CLOUD_SRE_LOGISTICA, etc.)
- [ ] Níveis de permissão granulares (READ, WRITE, DELETE)
- [ ] Interface de administração de permissões
- [ ] Métricas Prometheus (rbac_authorization_attempts, rbac_cache_hits)
- [ ] Notificações de tentativas de acesso negado

---

## 📚 Documentação de Referência

- [RBAC_AZURE_AD_IMPLEMENTATION.md](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md) - Guia técnico completo
- [RBAC_SUMMARY.md](docs/guides/RBAC_SUMMARY.md) - Resumo da implementação
- [CLAUDE.md](CLAUDE.md) - Documentação principal do projeto

---

## ✅ Resumo

**Implementação Completa:**
- ✅ Backend Go com verificação Azure AD
- ✅ Middleware HTTP para proteção de rotas
- ✅ Frontend React com hooks e componentes
- ✅ Testes automatizados
- ✅ Documentação completa

**Tempo Estimado de Integração:** 30-45 minutos

**Próximo Passo:** Seguir checklist acima e integrar ao `server.go` + componentes do frontend.

---

**Gostaria de ajuda com algum passo específico da integração?** 🚀
