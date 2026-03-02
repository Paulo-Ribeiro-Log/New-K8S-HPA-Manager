# Resource Explorer — Navegador Universal de Recursos Kubernetes

**Objetivo**: Implementar uma aba "Explorer" que funcione como o K9s — o usuário busca qualquer tipo de recurso (built-in ou CRD como ExternalSecret, Certificate, NetworkPolicy) e a aplicação lista todos os recursos desse tipo no cluster, com YAML editor completo.

**Data de criação**: 02 de março de 2026
**Branch**: new-k8s-hpa-dev

---

## Decisões de Arquitetura

- `dynamic` client **NÃO está no vendor** → usar kubectl shell (padrão VPAHandler)
- `discovery` client **ESTÁ no vendor** → usar `clientset.Discovery().ServerPreferredResources()` para listar todos os tipos disponíveis (incluindo CRDs)
- kubectl para CRUD: `kubectl get {kind.group} -o json/yaml`, `kubectl apply -f -`, `kubectl delete`
- Nova aba "Explorer" no WorkloadMenu com Combobox de autocomplete para tipo de recurso

---

## Checklist de Implementação

### Fase 1 — Backend: Tipos Go
- [x] `internal/models/types.go` — Adicionar `APIResourceInfo`
  ```go
  type APIResourceInfo struct {
      Kind       string   `json:"kind"`
      Name       string   `json:"name"`       // plural (e.g. "externalsecrets")
      Group      string   `json:"group"`      // e.g. "external-secrets.io"
      Version    string   `json:"version"`    // e.g. "v1beta1"
      Namespaced bool     `json:"namespaced"`
      Verbs      []string `json:"verbs"`
  }
  ```
- [x] `internal/models/types.go` — Adicionar `GenericResourceSummary`
  ```go
  type GenericResourceSummary struct {
      Name              string            `json:"name"`
      Namespace         string            `json:"namespace"`
      Kind              string            `json:"kind"`
      APIVersion        string            `json:"apiVersion"`
      Age               string            `json:"age"`
      Labels            map[string]string `json:"labels"`
      AdditionalColumns map[string]string `json:"additionalColumns"`
  }
  ```
- [x] `internal/models/types.go` — Adicionar `GenericResourceManifest`
  ```go
  type GenericResourceManifest struct {
      Cluster   string `json:"cluster"`
      Namespace string `json:"namespace"`
      Kind      string `json:"kind"`
      Name      string `json:"name"`
      YAML      string `json:"yaml"`
  }
  ```

### Fase 2 — Backend: Métodos Kubernetes
- [x] `internal/kubernetes/client.go` — `ListAPIResources(clientset kubernetes.Interface)` — Discovery API
- [x] `internal/kubernetes/client.go` — `ListGenericResources(cluster, namespace, name, group string)`
- [x] `internal/kubernetes/client.go` — `GetGenericResourceYAML(cluster, namespace, resourceName, group, name string)`
- [x] `internal/kubernetes/client.go` — `ApplyGenericResource(cluster, namespace, yamlContent string, dryRun, force bool)`
- [x] `internal/kubernetes/client.go` — `DeleteGenericResource(cluster, namespace, resourceName, group, name string)`
- [x] `make build` — Compilação sem erros ✅

### Fase 3 — Backend: Handler e Rotas
- [x] `internal/web/handlers/explorer.go` — Handler criado com: `ListResources`, `ListByKind`, `GetYAML`, `Describe`, `Diff`, `Validate`, `Apply`, `Delete`
- [x] `internal/web/server.go` — Rotas registradas em `/api/v1/explorer/...` com RBAC em PUT e DELETE
- [x] `make build` — Compilação sem erros ✅

**Rotas registradas:**
- `GET  /api/v1/explorer/api-resources?cluster=X`
- `GET  /api/v1/explorer/items?cluster=X&resource=Y&group=Z&namespace=W`
- `GET  /api/v1/explorer/:cluster/:namespace/:resource/:name?group=Z`
- `GET  /api/v1/explorer/:cluster/:namespace/:resource/:name/describe`
- `POST /api/v1/explorer/diff`
- `POST /api/v1/explorer/validate`
- `PUT  /api/v1/explorer/:cluster/:namespace/:resource/:name` (RBAC)
- `DELETE /api/v1/explorer/:cluster/:namespace/:resource/:name` (RBAC)

### Fase 4 — Frontend: Tipos TypeScript
- [x] `internal/web/frontend/src/lib/api/types.ts` — Adicionados: `APIResourceInfo`, `GenericResourceSummary`, `GenericResourceManifest`, `ExplorerDiffResult`, `ExplorerApplyResult`

### Fase 5 — Frontend: API Client
- [x] `internal/web/frontend/src/lib/api/client.ts` — Adicionados 8 métodos: `getAPIResources`, `listGenericResources`, `getGenericResourceYAML`, `applyGenericResource`, `deleteGenericResource`, `diffGenericResource`, `validateGenericResource`, `describeGenericResource`

### Fase 6 — Frontend: Hooks
- [x] `internal/web/frontend/src/hooks/useAPI.ts` — `useAPIResources(cluster)` com cache local de 5 minutos
- [x] `internal/web/frontend/src/hooks/useAPI.ts` — `useGenericResources(cluster, resourceName, group, namespace)`

### Fase 7 — Frontend: Componente ResourceExplorerTab
- [x] Criar `internal/web/frontend/src/components/ResourceExplorerTab.tsx`:
  - **Props**: `cluster, namespaces, selectedNamespace, onNamespaceChange, showSystemNamespaces, onToggleSystemNamespaces`
  - **Painel esquerdo (lista)**:
    - [x] Combobox com autocomplete (shadcn/ui `Command` + `Popover`)
    - [x] Agrupa: "Recursos Built-in" / "CRDs"
    - [x] Select de namespace (oculto para recursos cluster-scoped)
    - [x] Toggle "Sistema" (Eye/EyeOff)
    - [x] Lista de items com Name, Namespace, Age + colunas adicionais (status, phase)
    - [x] Estado vazio: "Selecione um tipo de recurso para começar"
    - [x] Estado de erro com mensagem descritiva
  - **Painel direito (detalhes)**:
    - [x] Header: nome, namespace, kind/apiVersion, age
    - [x] MonacoYamlEditor com toolbar completa (Undo/Redo, Diff, Validar, Aplicar, Cancelar, Fullscreen)
    - [x] Menu 3 pontos: Describe, Deletar (com `<ProtectedAction>`)

### Fase 8 — Frontend: Registro no Menu
- [x] `internal/web/frontend/src/components/WorkloadMenu.tsx` — `{ id: "explorer", label: "Explorer", icon: Search }` adicionado
- [x] `internal/web/frontend/src/pages/Index.tsx` — case `"explorer"` adicionado + oculta stats cards

### Fase 9 — Build Final e Testes
- [x] `./rebuild-web.sh -b` — Build completo (13.46s) ✅
- [ ] Hard refresh (Ctrl+Shift+R)
- [ ] Aba Explorer abre sem erros
- [ ] Combobox exibe tipos do cluster (built-in + CRDs instalados)
- [ ] Selecionar "ExternalSecret" → lista recursos corretamente
- [ ] Selecionar um recurso → YAML no Monaco Editor
- [ ] Dry-run com feedback visual
- [ ] Apply com modal de confirmação
- [ ] Delete com RBAC (apenas SRE)
- [ ] Describe abre modal com output completo
- [ ] Recurso cluster-scoped (ex: PersistentVolume) → sem seletor de namespace
- [ ] CRD não instalado → mensagem amigável

### Fase 10 — Documentação
- [ ] `CLAUDE.md` — Adicionar aba Explorer na seção "Features Principais"
- [ ] `README.md` — Adicionar Explorer na tabela de funcionalidades

---

## Arquivos Afetados

| Arquivo | Ação | Status |
|---------|------|--------|
| `internal/models/types.go` | Adicionar 3 tipos Go | ✅ Concluído |
| `internal/kubernetes/client.go` | Adicionar 5 métodos | ✅ Concluído |
| `internal/web/handlers/explorer.go` | Criar (novo arquivo) | ✅ Concluído |
| `internal/web/server.go` | Registrar rotas | ✅ Concluído |
| `internal/web/frontend/src/lib/api/types.ts` | Adicionar tipos TS | ✅ Concluído |
| `internal/web/frontend/src/lib/api/client.ts` | Adicionar métodos API | ✅ Concluído |
| `internal/web/frontend/src/hooks/useAPI.ts` | Adicionar 2 hooks | ✅ Concluído |
| `internal/web/frontend/src/components/ResourceExplorerTab.tsx` | Criar (novo arquivo) | ✅ Concluído |
| `internal/web/frontend/src/components/WorkloadMenu.tsx` | Adicionar aba | ✅ Concluído |
| `internal/web/frontend/src/pages/Index.tsx` | Adicionar case | ✅ Concluído |
| `CLAUDE.md` | Documentar feature | ✅ Concluído |
| `README.md` | Documentar feature | ✅ Concluído |

---

## Contexto para Novos Chats

```
Projeto: Kubernetes HPA + Azure AKS Node Pool Manager
Branch: new-k8s-hpa-dev

Feature em desenvolvimento: Resource Explorer (PLANO_RESOURCE_EXPLORER.md)
- Aba genérica para navegar qualquer recurso K8s (built-in + CRDs)
- Padrão de implementação: kubectl shell (igual VPAHandler em internal/web/handlers/vpas.go)
- Discovery API disponível via clientset.Discovery().ServerPreferredResources()
- dynamic client NÃO está no vendor

Arquivos de referência para padrões:
- internal/web/handlers/vpas.go — padrão kubectl-based para CRDs
- internal/kubernetes/client.go (linhas 4724-4868) — funções kubectl VPA
- internal/web/frontend/src/components/VPAsTab.tsx — padrão de componente
- internal/web/frontend/src/hooks/useAPI.ts (linhas 682-741) — padrão de hook
```
