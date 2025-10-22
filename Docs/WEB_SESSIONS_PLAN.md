# Sistema de Sessões - Interface Web
**Data:** 18 de Outubro de 2025
**Status:** Planejamento Completo - Pronto para Implementação
**Compatibilidade:** 100% com TUI existente

---

## 📋 Objetivo

Implementar sistema de sessões na interface web com **100% de compatibilidade** com o TUI existente, permitindo salvar/carregar configurações de HPAs e Node Pools no mesmo diretório `~/.k8s-hpa-manager/sessions/`.

---

## 📐 Arquitetura

```
┌─────────────────────────────────────────────────────────┐
│                    FRONTEND (React)                      │
│  ┌─────────────────┐  ┌──────────────────┐             │
│  │ SessionContext  │  │ Session Components│             │
│  │ (State Mgmt)    │  │ - SaveSessionModal│             │
│  │                 │  │ - LoadSessionModal│             │
│  └────────┬────────┘  └────────┬──────────┘             │
│           │                    │                         │
│           └────────┬───────────┘                         │
│                    │ HTTP Requests                       │
└────────────────────┼─────────────────────────────────────┘
                     │
┌────────────────────┼─────────────────────────────────────┐
│                    │     BACKEND (Go)                     │
│           ┌────────▼────────┐                            │
│           │  REST API        │                            │
│           │  /api/v1/sessions│                            │
│           └────────┬─────────┘                            │
│                    │                                      │
│           ┌────────▼────────┐                            │
│           │ SessionManager  │  (REUSA internal/session/) │
│           │ - Save()        │                            │
│           │ - Load()        │                            │
│           │ - List()        │                            │
│           │ - Delete()      │                            │
│           └────────┬─────────┘                            │
│                    │                                      │
│           ┌────────▼────────┐                            │
│           │  File System    │                            │
│           │ ~/.k8s-hpa-...  │                            │
│           └─────────────────┘                            │
└──────────────────────────────────────────────────────────┘
```

---

## 🔧 Componentes a Implementar

### **1. Backend - REST API Endpoints**

**Arquivo:** `internal/web/handlers/sessions.go` (NOVO)

```go
// Endpoints:
GET    /api/v1/sessions                    // Listar todas as sessões
GET    /api/v1/sessions/folders            // Listar pastas (HPA-Upscale, etc)
GET    /api/v1/sessions/folders/:folder    // Listar sessões de uma pasta
GET    /api/v1/sessions/:name              // Carregar sessão específica
POST   /api/v1/sessions                    // Salvar nova sessão
DELETE /api/v1/sessions/:name              // Deletar sessão
GET    /api/v1/sessions/templates          // Listar templates de nomenclatura
```

**Responsabilidades:**
- Reaproveitar `session.Manager` existente (`internal/session/manager.go`)
- Converter entre formato JSON da API e estruturas Go
- Validar permissões e input
- Tratar erros HTTP adequadamente

**Código base do Manager existente:**
```go
// internal/session/manager.go já implementa:
type Manager struct {
    sessionDir string
    templates  []models.SessionTemplate
}

// Métodos disponíveis:
- SaveSession(session *models.Session) error
- SaveSessionToFolder(session *models.Session, folder SessionFolder) error
- LoadSession(name string) (*models.Session, error)
- LoadSessionFromFolder(name string, folder SessionFolder) (*models.Session, error)
- ListSessions() ([]models.Session, error)
- ListSessionsInFolder(folder SessionFolder) ([]models.Session, error)
- DeleteSession(name string) error
- DeleteSessionFromFolder(name string, folder SessionFolder) error
- GenerateSessionName(baseName, templatePattern string, changes []models.HPAChange) string
- GetTemplates() []models.SessionTemplate
```

---

### **2. Frontend - Session Context**

**Arquivo:** `internal/web/frontend/src/contexts/SessionContext.tsx` (NOVO)

```typescript
interface SessionContextType {
  // Lista de sessões
  sessions: Session[];
  folders: SessionFolder[];

  // Actions
  saveSession: (name: string, folder: SessionFolder) => Promise<void>;
  loadSession: (name: string) => Promise<Session>;
  deleteSession: (name: string) => Promise<void>;
  listSessions: (folder?: SessionFolder) => Promise<Session[]>;

  // Templates
  templates: SessionTemplate[];
  generateSessionName: (action: string, template: string) => string;

  // Estado
  loading: boolean;
  error: string | null;
}
```

**Integração com StagingContext:**
- SessionContext usa StagingContext para obter dados atuais ao salvar
- Ao carregar sessão, popula StagingContext com dados da sessão
- Ambos os contexts devem estar disponíveis via hooks

---

### **3. Frontend - Save Session Modal**

**Arquivo:** `internal/web/frontend/src/components/SaveSessionModal.tsx` (NOVO)

**Features:**
- **Seleção de pasta** (HPA-Upscale, HPA-Downscale, Node-Upscale, Node-Downscale)
  - Radio buttons ou Select dropdown
  - Descrição de cada pasta
- **Seleção de template de nomenclatura** (4 opções do TUI)
  - Template 1: `{action}_{cluster}_{timestamp}`
  - Template 2: `{action}_{env}_{date}`
  - Template 3: `{timestamp}_{action}_{user}`
  - Template 4: `Quick-save_{timestamp}`
- **Preview do nome gerado** com variáveis substituídas
  - Input de "action" (ex: "upscale-black-friday")
  - Nome final: `upscale-black-friday_prod_18-10-25_14:30`
- **Input de descrição opcional**
  - Textarea para comentários
- **Validação de nome**
  - Somente letras, números, `_`, `-`
  - Máximo 50 caracteres
  - Feedback visual de validação
- **Preview das alterações** que serão salvas
  - Lista de HPAs modificados
  - Lista de Node Pools modificados
  - Sequenciamento (*1, *2) se aplicável
- **Contador de mudanças**
  - "7 mudanças serão salvas (5 HPAs + 2 Node Pools)"
- **Botões de ação**
  - Cancel
  - Save (disabled até nome válido)

**UI Flow:**
```
┌─────────────────────────────────────────────┐
│          Save Session                       │
├─────────────────────────────────────────────┤
│ Folder: ⦿ HPA-Upscale                       │
│         ○ HPA-Downscale                     │
│         ○ Node-Upscale                      │
│         ○ Node-Downscale                    │
├─────────────────────────────────────────────┤
│ Template: [Action + Cluster + Timestamp ▼] │
│                                             │
│ Action Name: [upscale-black-friday____]     │
│                                             │
│ Preview: upscale-black-friday_prod_18-10-25 │
├─────────────────────────────────────────────┤
│ Description (optional):                     │
│ ┌─────────────────────────────────────────┐ │
│ │ Black Friday preparation - scale up     │ │
│ │ ingress and payment services           │ │
│ └─────────────────────────────────────────┘ │
├─────────────────────────────────────────────┤
│ 📊 Changes to save:                         │
│   • 5 HPAs                                  │
│   • 2 Node Pools (*1, *2)                   │
│   Total: 7 changes                          │
├─────────────────────────────────────────────┤
│            [Cancel]  [Save Session]         │
└─────────────────────────────────────────────┘
```

---

### **4. Frontend - Load Session Modal**

**Arquivo:** `internal/web/frontend/src/components/LoadSessionModal.tsx` (NOVO)

**Features:**
- **Tabs por pasta** (HPA-Upscale, HPA-Downscale, Node-Upscale, Node-Downscale)
  - Badge com contador de sessões em cada tab
- **Lista de sessões** com metadados:
  - Nome da sessão (bold)
  - Data de criação (formato relativo: "2 hours ago", "3 days ago")
  - Criado por (usuário)
  - Badges: quantidade de mudanças
    - "5 HPAs"
    - "2 Node Pools"
  - Clusters afetados (chips pequenos)
- **Busca/filtro por nome**
  - Input de busca no topo
  - Filtra em tempo real
- **Preview detalhado** ao selecionar sessão:
  - **Header**:
    - Nome completo
    - Descrição (se houver)
    - Metadados (data, usuário, clusters)
  - **Lista de HPAs** que serão modificados:
    - `namespace/hpa-name`
    - `Min: 1→2, Max: 8→12`
    - `CPU: 60%→70%`
  - **Lista de Node Pools** que serão modificados:
    - `cluster/pool-name`
    - `Nodes: 3→5`
    - `Autoscaling: OFF→ON (min:2, max:10)`
    - Badge *1 ou *2 se sequencial
  - **Valores antes/depois** destacados:
    - Cor verde para aumentos
    - Cor azul para reduções
    - Ícones: ⬆️ ⬇️ ↔️
- **Ações**:
  - **Botão "Load"** - Popula StagingContext e fecha modal
  - **Botão "Delete"** - Remove sessão (com confirmação)
  - **Botão "Cancel"** - Fecha modal
- **Empty state**:
  - Mensagem quando pasta não tem sessões
  - Ícone e texto explicativo

**UI Flow:**
```
┌─────────────────────────────────────────────────────────────┐
│          Load Session                                       │
├─────────────────────────────────────────────────────────────┤
│ [HPA-Upscale(3)] [HPA-Downscale(5)] [Node-Upscale(2)] ...  │
├─────────────────────────────────────────────────────────────┤
│ Search: [_____________________] 🔍                          │
├──────────────────────┬──────────────────────────────────────┤
│ Sessions (5)         │ Preview                              │
│ ┌──────────────────┐ │ ┌──────────────────────────────────┐ │
│ │ upscale_prod...  │ │ │ upscale-black-friday_prod_18-10  │ │
│ │ 2 hours ago      │ │ │ Created: 18/10/25 14:30          │ │
│ │ by: admin        │ │ │ By: admin                        │ │
│ │ [5 HPAs][2 Nodes]│ │ │ Description: Black Friday prep   │ │
│ └──────────────────┘ │ ├──────────────────────────────────┤ │
│                      │ │ HPAs (5):                        │ │
│ ┌──────────────────┐ │ │ • ingress/nginx ⬆️               │ │
│ │ downscale_dev... │ │ │   Min: 1→2, Max: 8→12           │ │
│ │ 1 day ago        │ │ │ • payment/api ⬆️                 │ │
│ │ by: ops-team     │ │ │   CPU: 60%→70%                  │ │
│ │ [3 HPAs]         │ │ │                                  │ │
│ └──────────────────┘ │ │ Node Pools (2):                  │ │
│                      │ │ • monitoring-1 *1 ⬇️             │ │
│ ...                  │ │   Nodes: 5→0                     │ │
│                      │ │ • monitoring-2 *2 ⬆️             │ │
│                      │ │   Autoscaling: ON (2-10)         │ │
├──────────────────────┴──────────────────────────────────────┤
│            [Delete]  [Cancel]  [Load Session]               │
└─────────────────────────────────────────────────────────────┘
```

---

### **5. Integração com Header**

**Arquivo:** `internal/web/frontend/src/components/Header.tsx` (MODIFICAR)

**Adicionar botões** (ao lado de "Apply All"):

```tsx
// Posição: Entre "Apply All" e info do usuário

{/* Save Session Button */}
{modifiedCount > 0 && (
  <Button
    variant="secondary"
    className="bg-blue-500 hover:bg-blue-600 text-white"
    onClick={() => setShowSaveSessionModal(true)}
  >
    <Save className="w-4 h-4 mr-2" />
    Save Session
  </Button>
)}

{/* Load Session Button */}
<Button
  variant="outline"
  onClick={() => setShowLoadSessionModal(true)}
>
  <FolderOpen className="w-4 h-4 mr-2" />
  Load Session
</Button>
```

**Ícones:**
- **Save Session**: 💾 `Save` (lucide-react)
- **Load Session**: 📂 `FolderOpen` (lucide-react)

**Comportamento:**
- "Save Session" só aparece quando `modifiedCount > 0`
- "Load Session" sempre visível
- Ambos abrem modals respectivos

---

## 📝 Estrutura de Dados

### **Session (JSON)**
```json
{
  "name": "upscale_prod_18-10-25_14:30",
  "created_at": "2025-10-18T14:30:00Z",
  "created_by": "admin",
  "description": "Black Friday preparation",
  "template_used": "{action}_{env}_{timestamp}",
  "metadata": {
    "clusters_affected": ["akspriv-faturamento-prd"],
    "namespaces_count": 3,
    "hpa_count": 5,
    "node_pool_count": 2,
    "total_changes": 7
  },
  "changes": [
    {
      "cluster": "akspriv-faturamento-prd",
      "namespace": "ingress-nginx",
      "name": "nginx-ingress-controller",
      "min_replicas": 2,
      "max_replicas": 12,
      "target_cpu": 70,
      "original_min_replicas": 1,
      "original_max_replicas": 8,
      "original_target_cpu": 60
    }
  ],
  "node_pool_changes": [
    {
      "cluster_name": "akspriv-faturamento-prd",
      "resource_group": "rg-faturamento-prd",
      "name": "monitoring-1",
      "node_count": 0,
      "min_node_count": 0,
      "max_node_count": 0,
      "autoscaling_enabled": false,
      "original_node_count": 5,
      "original_autoscaling_enabled": true,
      "order": 1
    },
    {
      "cluster_name": "akspriv-faturamento-prd",
      "resource_group": "rg-faturamento-prd",
      "name": "monitoring-2",
      "node_count": 0,
      "min_node_count": 2,
      "max_node_count": 10,
      "autoscaling_enabled": true,
      "original_node_count": 3,
      "original_autoscaling_enabled": false,
      "order": 2
    }
  ],
  "rollback_data": {
    "original_state_captured": true,
    "can_rollback": true,
    "rollback_script_generated": false
  }
}
```

### **TypeScript Types**

**Arquivo:** `internal/web/frontend/src/lib/api/types.ts` (ADICIONAR)

```typescript
export interface Session {
  name: string;
  created_at: string;
  created_by: string;
  description?: string;
  template_used: string;
  metadata: SessionMetadata;
  changes: HPAChange[];
  node_pool_changes: NodePoolChange[];
  rollback_data: RollbackData;
}

export interface SessionMetadata {
  clusters_affected: string[];
  namespaces_count: number;
  hpa_count: number;
  node_pool_count: number;
  total_changes: number;
}

export interface HPAChange {
  cluster: string;
  namespace: string;
  name: string;
  min_replicas: number;
  max_replicas: number;
  target_cpu?: number;
  target_memory?: number;
  original_min_replicas: number;
  original_max_replicas: number;
  original_target_cpu?: number;
  original_target_memory?: number;
}

export interface NodePoolChange {
  cluster_name: string;
  resource_group: string;
  name: string;
  node_count: number;
  min_node_count: number;
  max_node_count: number;
  autoscaling_enabled: boolean;
  original_node_count: number;
  original_min_node_count: number;
  original_max_node_count: number;
  original_autoscaling_enabled: boolean;
  order?: number; // *1 ou *2
}

export interface RollbackData {
  original_state_captured: boolean;
  can_rollback: boolean;
  rollback_script_generated: boolean;
}

export interface SessionTemplate {
  name: string;
  pattern: string;
  description: string;
  variables: Record<string, string>;
}

export type SessionFolder =
  | "HPA-Upscale"
  | "HPA-Downscale"
  | "Node-Upscale"
  | "Node-Downscale";
```

---

### **Conversão StagingContext → Session**

**Arquivo:** `internal/web/frontend/src/lib/sessionConverter.ts` (NOVO)

```typescript
export function convertStagingToSession(
  staging: StagingContextType,
  name: string,
  description: string,
  template: string
): Session {
  // Converter HPAs
  const hpaChanges: HPAChange[] = staging.getAll().map(({ key, data }) => ({
    cluster: data.current.cluster,
    namespace: data.current.namespace,
    name: data.current.name,
    min_replicas: data.current.min_replicas,
    max_replicas: data.current.max_replicas,
    target_cpu: data.current.target_cpu,
    target_memory: data.current.target_memory,
    original_min_replicas: data.original.min_replicas,
    original_max_replicas: data.original.max_replicas,
    original_target_cpu: data.original.target_cpu,
    original_target_memory: data.original.target_memory,
  }));

  // Converter Node Pools
  const nodePoolChanges: NodePoolChange[] = staging.getAllNodePools().map(({ key, data }) => ({
    cluster_name: data.current.cluster_name,
    resource_group: data.current.resource_group,
    name: data.current.name,
    node_count: data.current.node_count,
    min_node_count: data.current.min_node_count,
    max_node_count: data.current.max_node_count,
    autoscaling_enabled: data.current.autoscaling_enabled,
    original_node_count: data.original.node_count,
    original_min_node_count: data.original.min_node_count,
    original_max_node_count: data.original.max_node_count,
    original_autoscaling_enabled: data.original.autoscaling_enabled,
    order: data.order,
  }));

  // Calcular metadados
  const clustersSet = new Set<string>();
  hpaChanges.forEach(c => clustersSet.add(c.cluster));
  nodePoolChanges.forEach(c => clustersSet.add(c.cluster_name));

  const namespacesSet = new Set<string>();
  hpaChanges.forEach(c => namespacesSet.add(`${c.cluster}/${c.namespace}`));

  return {
    name,
    created_at: new Date().toISOString(),
    created_by: "web-user",
    description,
    template_used: template,
    metadata: {
      clusters_affected: Array.from(clustersSet),
      namespaces_count: namespacesSet.size,
      hpa_count: hpaChanges.length,
      node_pool_count: nodePoolChanges.length,
      total_changes: hpaChanges.length + nodePoolChanges.length,
    },
    changes: hpaChanges,
    node_pool_changes: nodePoolChanges,
    rollback_data: {
      original_state_captured: true,
      can_rollback: true,
      rollback_script_generated: false,
    },
  };
}
```

### **Conversão Session → StagingContext**

```typescript
export function loadSessionIntoStaging(
  session: Session,
  staging: StagingContextType
): void {
  // Limpar staging atual
  staging.clear();
  staging.clearNodePools();

  // Carregar HPAs
  session.changes.forEach(change => {
    const current: HPA = {
      cluster: change.cluster,
      namespace: change.namespace,
      name: change.name,
      min_replicas: change.min_replicas,
      max_replicas: change.max_replicas,
      target_cpu: change.target_cpu,
      target_memory: change.target_memory,
      // ... outros campos
    };

    const original: HPA = {
      ...current,
      min_replicas: change.original_min_replicas,
      max_replicas: change.original_max_replicas,
      target_cpu: change.original_target_cpu,
      target_memory: change.original_target_memory,
    };

    staging.add(current, original);
  });

  // Carregar Node Pools
  session.node_pool_changes.forEach(change => {
    const current: NodePool = {
      cluster_name: change.cluster_name,
      resource_group: change.resource_group,
      name: change.name,
      node_count: change.node_count,
      min_node_count: change.min_node_count,
      max_node_count: change.max_node_count,
      autoscaling_enabled: change.autoscaling_enabled,
      // ... outros campos
    };

    const original: NodePool = {
      ...current,
      node_count: change.original_node_count,
      min_node_count: change.original_min_node_count,
      max_node_count: change.original_max_node_count,
      autoscaling_enabled: change.original_autoscaling_enabled,
    };

    staging.addNodePool(current, original, change.order);
  });
}
```

---

## 🎨 UI/UX Flow

### **Fluxo de Salvar Sessão:**
1. Usuário modifica HPAs e/ou Node Pools (staging area populated)
2. Header mostra "Apply All (7)" e "Save Session"
3. Clica em **"Save Session"** no Header
4. **SaveSessionModal** abre com:
   - **Step 1**: Escolher pasta de destino (radio buttons)
   - **Step 2**: Escolher template de nomenclatura (dropdown)
   - **Step 3**: Input de "action name" (ex: "upscale-black-friday")
   - **Preview em tempo real**: `upscale-black-friday_prod_18-10-25_14:30`
   - **Description** (textarea opcional)
   - **Preview de mudanças**: "7 mudanças serão salvas (5 HPAs + 2 Node Pools)"
5. Clica **"Save Session"**
6. Backend salva em `~/.k8s-hpa-manager/sessions/{Folder}/{Name}.json`
7. Toast de confirmação: "✅ Sessão salva com sucesso!"
8. Modal fecha
9. **Staging area mantém os dados** (usuário ainda pode aplicar ou editar)

### **Fluxo de Carregar Sessão:**
1. Usuário clica em **"Load Session"** no Header
2. **LoadSessionModal** abre com:
   - Tabs por pasta (HPA-Upscale, HPA-Downscale, etc)
   - Badge mostra contador: "HPA-Upscale (3)"
3. Tab ativa mostra **lista de sessões** (ordenadas por data, mais recente primeiro)
4. Cada item mostra:
   - Nome da sessão
   - Data relativa ("2 hours ago")
   - Criado por
   - Badges de quantidade (5 HPAs, 2 Nodes)
   - Clusters afetados
5. Usuário **seleciona uma sessão** da lista
6. **Preview detalhado** aparece no painel direito:
   - Metadados (nome, descrição, data, usuário)
   - Lista de HPAs com valores antes/depois
   - Lista de Node Pools com valores antes/depois
   - Badges *1/*2 se sequencial
7. Usuário revisa e clica **"Load Session"**
8. Frontend chama `loadSessionIntoStaging(session, staging)`
9. **StagingContext é populado** com os dados da sessão
10. Header atualiza contador: "Apply All (7)"
11. Modal fecha
12. Usuário pode:
    - Revisar as mudanças nas abas HPAs e Node Pools
    - Editar antes de aplicar
    - Aplicar diretamente com "Apply All"

---

## 🔄 Compatibilidade TUI ↔ Web

### **100% Compatível:**

✅ **Mesmo formato JSON**
- Estrutura idêntica de `Session`
- Campos `changes`, `node_pool_changes`, `metadata`, etc

✅ **Mesmo diretório**
- `~/.k8s-hpa-manager/sessions/`
- Criado automaticamente se não existir

✅ **Mesmas pastas**
- `HPA-Upscale/`
- `HPA-Downscale/`
- `Node-Upscale/`
- `Node-Downscale/`

✅ **Mesmos templates de nomenclatura**
- Template 1: `{action}_{cluster}_{timestamp}`
- Template 2: `{action}_{env}_{date}`
- Template 3: `{timestamp}_{action}_{user}`
- Template 4: `Quick-save_{timestamp}`

✅ **Sessões salvas pelo TUI podem ser carregadas pela Web**
- Leitura do mesmo JSON
- Parsing dos mesmos campos
- Mesma estrutura de HPAChange e NodePoolChange

✅ **Sessões salvas pela Web podem ser carregadas pelo TUI**
- Escrita do mesmo JSON
- Metadados compatíveis
- RollbackData incluído

✅ **Suporte a sessões mistas**
- HPAs + Node Pools na mesma sessão
- Sequenciamento (*1, *2) preservado
- Rollback data mantido

---

## ✅ Checklist de Implementação

### **Backend:**
- [ ] Criar `internal/web/handlers/sessions.go`
  - [ ] `GET /api/v1/sessions` - Listar todas
  - [ ] `GET /api/v1/sessions/folders` - Listar pastas
  - [ ] `GET /api/v1/sessions/folders/:folder` - Listar por pasta
  - [ ] `GET /api/v1/sessions/:name` - Carregar específica
  - [ ] `POST /api/v1/sessions` - Salvar nova
  - [ ] `DELETE /api/v1/sessions/:name` - Deletar
  - [ ] `GET /api/v1/sessions/templates` - Listar templates
- [ ] Registrar rotas em `internal/web/server.go`
- [ ] Testes unitários para handlers

### **Frontend - Tipos e Utilitários:**
- [ ] Adicionar tipos em `src/lib/api/types.ts`
  - [ ] `Session`
  - [ ] `SessionMetadata`
  - [ ] `HPAChange`
  - [ ] `NodePoolChange`
  - [ ] `RollbackData`
  - [ ] `SessionTemplate`
  - [ ] `SessionFolder`
- [ ] Criar `src/lib/sessionConverter.ts`
  - [ ] `convertStagingToSession()`
  - [ ] `loadSessionIntoStaging()`
- [ ] Criar `src/hooks/useSession.ts`

### **Frontend - Contextos:**
- [ ] Criar `src/contexts/SessionContext.tsx`
  - [ ] Provider
  - [ ] Hook `useSession()`
  - [ ] Métodos de save/load/delete/list
  - [ ] Integração com API client

### **Frontend - Componentes:**
- [ ] Criar `src/components/SaveSessionModal.tsx`
  - [ ] Seleção de pasta (radio buttons)
  - [ ] Seleção de template (dropdown)
  - [ ] Input de action name
  - [ ] Preview de nome gerado
  - [ ] Textarea de descrição
  - [ ] Preview de mudanças
  - [ ] Validação de nome
  - [ ] Botões Cancel/Save
- [ ] Criar `src/components/LoadSessionModal.tsx`
  - [ ] Tabs por pasta
  - [ ] Lista de sessões com metadados
  - [ ] Busca/filtro
  - [ ] Preview detalhado (painel direito)
  - [ ] Botões Delete/Cancel/Load
  - [ ] Empty states
- [ ] Modificar `src/components/Header.tsx`
  - [ ] Adicionar botão "Save Session"
  - [ ] Adicionar botão "Load Session"
  - [ ] Abrir modals respectivos

### **Integração:**
- [ ] Integrar SessionContext com StagingContext
- [ ] Testar conversão StagingContext → Session
- [ ] Testar conversão Session → StagingContext
- [ ] Testes end-to-end (salvar e carregar)
- [ ] Testar compatibilidade com TUI
  - [ ] Salvar no TUI, carregar na Web
  - [ ] Salvar na Web, carregar no TUI

### **Documentação:**
- [ ] Atualizar `Docs/README_WEB.md`
- [ ] Atualizar `CLAUDE.md`
- [ ] Screenshots dos modals
- [ ] Exemplos de uso

---

## 📚 Referências

**Código Existente:**
- `internal/session/manager.go` - Implementação completa de SessionManager
- `internal/models/types.go:390` - Estrutura de `Session`
- `internal/tui/app.go` - Uso de sessões no TUI

**Documentação Web:**
- `Docs/README_WEB.md` - Índice da documentação web
- `Docs/WEB_POC_STATUS.md` - Status atual da POC
- `Docs/WEB_INTERFACE_DESIGN.md` - Design da arquitetura

---

## 🎯 Próximos Passos

1. **Criar handler backend** (`sessions.go`)
2. **Registrar rotas** no servidor
3. **Criar tipos TypeScript**
4. **Implementar SessionContext**
5. **Criar SaveSessionModal**
6. **Criar LoadSessionModal**
7. **Integrar no Header**
8. **Testar compatibilidade TUI**
9. **Documentar e fazer screenshots**

---

**Última Atualização:** 18 de Outubro de 2025
**Próxima Revisão:** Após implementação do backend
