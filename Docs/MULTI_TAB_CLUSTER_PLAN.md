# Plano de Implementação: Sistema de Múltiplas Abas com Clusters Independentes

**Projeto:** k8s-hpa-manager  
**Versão:** v1.2.6+  
**Data:** 31 de outubro de 2025  
**Complexidade Estimada:** ALTA (8-10 semanas de desenvolvimento)

---

## 📋 Índice

1. [Análise da Arquitetura Atual](#1-análise-da-arquitetura-atual)
2. [Arquitetura Proposta](#2-arquitetura-proposta)
3. [Plano de Implementação](#3-plano-de-implementação)
4. [Estimativa de Complexidade e Riscos](#4-estimativa-de-complexidade-e-riscos)
5. [Alternativas Consideradas](#5-alternativas-consideradas)
6. [Recomendações Técnicas](#6-recomendações-técnicas)

---

## 1. Análise da Arquitetura Atual

### 1.1 Estado Atual do Frontend (React/TypeScript)

#### TabContext (✅ Parcialmente Pronto)
**Arquivo:** `internal/web/frontend/src/contexts/TabContext.tsx`

**O que já existe:**
- ✅ Sistema de gerenciamento de múltiplas abas (max: 10 abas)
- ✅ Estado isolado por aba (`TabState`)
- ✅ Atalhos de teclado (Alt+1-9, Alt+T para nova aba, Alt+W para fechar)
- ✅ Reducer pattern para gerenciamento de estado
- ✅ Métodos helper: `addTab()`, `closeTab()`, `switchTab()`, `getActiveTab()`

**Estrutura `TabState`:**
```typescript
interface TabState {
  id: string;                  // Identificador único
  name: string;                // Nome da aba (ex: "Cluster Prod-01")
  clusterContext: string;      // Contexto Kubernetes
  active: boolean;             // Aba ativa
  modified: boolean;           // Tem mudanças pendentes
  
  pageState: {
    activeTab: string;         // dashboard | hpas | nodepools | cronjobs | prometheus
    selectedCluster: string;
    selectedNamespace: string;
    selectedHPA: HPA | null;
    selectedNodePool: NodePool | null;
    // ... outros estados da UI
  };
  
  pendingChanges: {
    total: number;
    hpas: number;
    nodePools: number;
  };
}
```

**Limitações identificadas:**
- ❌ **Staging Area não é isolado por aba** - `StagingContext` é global
- ❌ **Cluster switch não troca contexto Kubernetes de verdade** - apenas muda estado local
- ❌ **Não há sincronização entre TabManager e StagingContext**
- ❌ **Primeira aba criada com cluster "default" hardcoded** (linha 352)

#### StagingContext (⚠️ Precisa Refatoração)
**Arquivo:** `internal/web/frontend/src/contexts/StagingContext.tsx`

**O que existe:**
- ✅ Gerenciamento de HPAs staged (com originalValues)
- ✅ Gerenciamento de Node Pools staged
- ✅ Métodos de add/update/remove/clear

**Problema CRÍTICO:**
```typescript
// ❌ Estado global - compartilhado por TODAS as abas
const [stagedHPAs, setStagedHPAs] = useState<StagingHPA[]>([]);
const [stagedNodePools, setStagedNodePools] = useState<StagingNodePool[]>([]);
```

**Necessidade:**
- Transformar em estado isolado por aba
- Cada aba deve ter sua própria staging area independente

#### Index.tsx (⚠️ Estado Local sem Isolamento)
**Arquivo:** `internal/web/frontend/src/pages/Index.tsx`

**Problema:**
```typescript
// ❌ Estado local - não sincroniza com TabContext
const [selectedCluster, setSelectedCluster] = useState("");
const [selectedNamespace, setSelectedNamespace] = useState("");
const [selectedHPA, setSelectedHPA] = useState<HPA | null>(null);
// ...
```

**Função de troca de cluster:**
```typescript
const handleClusterChange = async (newCluster: string) => {
  // ✅ Chama backend para switch context
  await apiClient.switchContext(newCluster);
  
  // ✅ Atualiza estado local
  setSelectedCluster(newCluster);
  
  // ✅ Sincroniza com TabManager
  updateActiveTabState({ selectedCluster: newCluster });
}
```

**Limitação:** 
- Função atual troca contexto **globalmente** no backend
- Não suporta múltiplos clusters simultaneamente

---

### 1.2 Estado Atual do Backend (Go)

#### KubeConfigManager (✅ Thread-Safe, mas Global)
**Arquivo:** `internal/config/kubeconfig.go`

**O que existe:**
- ✅ Pool de clients Kubernetes (`map[string]kubernetes.Interface`)
- ✅ Thread-safety com `sync.RWMutex` (linhas 38, 151-156)
- ✅ Double-check locking para criação de clients (linhas 159-165)
- ✅ Método `SwitchContext()` - muda contexto via kubectl (linha 524)
- ✅ Método `SwitchAzureContext()` - muda subscription Azure (linha 549)

**Problema CRÍTICO:**
```go
// ❌ Context switch é GLOBAL - afeta TODAS as requisições subsequentes
func (k *KubeConfigManager) SwitchContext(context string) error {
    // Executa: kubectl config use-context <context>
    cmd := exec.Command("kubectl", "config", "use-context", context)
    
    // Atualiza contexto em memória
    k.config.CurrentContext = context
    
    // LIMPA todos os clients - próxima requisição usará novo contexto
    k.clientMutex.Lock()
    k.clients = make(map[string]kubernetes.Interface)
    k.clientMutex.Unlock()
}
```

**Impacto:**
- Se ABA 1 está em cluster A e ABA 2 troca para cluster B
- ABA 1 **também** passa a usar cluster B (silenciosamente!)

#### Kubernetes Client (✅ Por Cluster, mas sem Isolamento de Sessão)
**Arquivo:** `internal/kubernetes/client.go`

**Estrutura:**
```go
type Client struct {
    clientset kubernetes.Interface  // Client oficial K8s
    cluster   string                 // Nome do cluster
}

func NewClient(clientset kubernetes.Interface, clusterName string) *Client {
    return &Client{
        clientset: clientset,
        cluster:   clusterName,
    }
}
```

**Como funciona hoje:**
1. Handler recebe `cluster` via query param (ex: `/api/v1/hpas?cluster=akspriv-prod-admin`)
2. Chama `kubeManager.GetClient(cluster)` para obter client
3. Client é **criado ou reutilizado** do pool global
4. Operação K8s é executada no cluster correto

**Limitação:**
- Pool de clients é **compartilhado globalmente**
- Não há conceito de "sessão" ou "aba" no backend
- Se duas abas usam o mesmo cluster, compartilham o mesmo client (OK para leituras, problema para writes)

#### Handlers HTTP (✅ Stateless, mas sem Isolamento)
**Arquivos:**
- `internal/web/handlers/hpas.go`
- `internal/web/handlers/nodepools.go`

**Como funcionam:**
```go
func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")  // Ex: "akspriv-prod-admin"
    
    // Obter client do pool global
    client, err := h.kubeManager.GetClient(cluster)
    
    // Listar HPAs do cluster
    hpas, err := kubeClient.ListHPAs(ctx, namespace)
    
    c.JSON(200, gin.H{"data": hpas})
}
```

**Vantagens:**
- ✅ Handlers são **stateless** (bom para escalabilidade)
- ✅ Cluster é passado em **cada requisição** (explícito)

**Limitações:**
- ❌ Não há conceito de "sessão HTTP" ou "tab ID"
- ❌ Backend não sabe **qual aba** está fazendo a requisição
- ❌ `SwitchContext` é global - afeta todas as abas

---

### 1.3 Fluxo Atual de Dados

```
┌─────────────────────────────────────────────────────────────┐
│                    FRONTEND (React)                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐        ┌──────────────┐                  │
│  │ TabContext   │        │  StagingCtx  │                  │
│  │ (Multi-tab)  │        │  (GLOBAL!)   │                  │
│  └──────┬───────┘        └──────┬───────┘                  │
│         │                       │                           │
│         ├─ Tab 1 (Prod)        ├─ stagedHPAs (shared)     │
│         ├─ Tab 2 (Dev)         └─ stagedNodePools (shared) │
│         └─ Tab 3 (QA)                                       │
│                                                             │
│  ┌──────────────────────────────────────────────┐          │
│  │          Index.tsx (Page State)              │          │
│  │  - selectedCluster (local, not synced)       │          │
│  │  - selectedNamespace                         │          │
│  │  - handleClusterChange() → switchContext()   │          │
│  └──────────────────┬───────────────────────────┘          │
│                     │                                       │
└─────────────────────┼───────────────────────────────────────┘
                      │ HTTP Request
                      │ GET /api/v1/hpas?cluster=X
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                    BACKEND (Go)                              │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────────────────────────────────────┐          │
│  │         KubeConfigManager (GLOBAL)           │          │
│  │  - clients: map[string]*Client               │          │
│  │  - CurrentContext: string (GLOBAL!)          │          │
│  │  - SwitchContext() → kubectl use-context     │          │
│  └──────────────────┬───────────────────────────┘          │
│                     │                                       │
│                     ├─ Client("akspriv-prod")              │
│                     ├─ Client("akspriv-dev")               │
│                     └─ Client("akspriv-qa")                │
│                                                             │
│  ┌──────────────────────────────────────────────┐          │
│  │            HTTP Handlers (Stateless)         │          │
│  │  - HPAHandler.List(cluster)                  │          │
│  │  - NodePoolHandler.List(cluster)             │          │
│  │  - ClusterHandler.SwitchContext(cluster)     │          │
│  └──────────────────────────────────────────────┘          │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                      │
                      ▼
           ┌─────────────────────┐
           │  Kubernetes API     │
           │  (client-go)        │
           └─────────────────────┘
```

**Problemas identificados:**
1. ❌ **StagingContext é global** - mudanças em uma aba afetam outras
2. ❌ **SwitchContext é global** - troca de cluster afeta todas as abas
3. ❌ **Nenhum identificador de sessão/aba** - backend não sabe qual aba está fazendo requisição
4. ❌ **Estado do Index.tsx não sincroniza com TabContext** - pode ficar dessincronizado

---

## 2. Arquitetura Proposta

### 2.1 Visão Geral

**Objetivo:** Cada aba opera de forma **totalmente independente**:
- ✅ Cluster diferente por aba
- ✅ Staging area isolado por aba
- ✅ Operações paralelas sem conflitos
- ✅ Backend mantém estado isolado por "sessão HTTP" ou "tab ID"

### 2.2 Frontend: Refatoração de Contextos

#### Opção A: StagingContext Isolado por Aba (RECOMENDADO)

**Mudança de paradigma:**
```typescript
// ❌ ANTES: Estado global
const [stagedHPAs, setStagedHPAs] = useState<StagingHPA[]>([]);

// ✅ DEPOIS: Estado por aba
const [tabStagingData, setTabStagingData] = useState<Map<string, {
  stagedHPAs: StagingHPA[];
  stagedNodePools: StagingNodePool[];
}>>(new Map());
```

**Métodos refatorados:**
```typescript
interface StagingContextType {
  // Novo parâmetro: tabId
  addHPAToStaging: (tabId: string, hpa: HPA) => void;
  updateHPAInStaging: (tabId: string, cluster: string, namespace: string, name: string, updates: Partial<HPA>) => void;
  removeHPAFromStaging: (tabId: string, cluster: string, namespace: string, name: string) => void;
  
  // Métodos para obter dados da aba ativa
  getActiveTabStagingData: () => { stagedHPAs: StagingHPA[]; stagedNodePools: StagingNodePool[] };
  clearTabStaging: (tabId: string) => void;
}
```

**Integração com TabContext:**
```typescript
const StagingProvider = ({ children }) => {
  const { getActiveTab } = useTabManager();
  
  const addHPAToStaging = (hpa: HPA) => {
    const activeTab = getActiveTab();
    if (!activeTab) return;
    
    addHPAToStagingInternal(activeTab.id, hpa);
  };
};
```

#### Opção B: Staging Dentro do TabState (Alternativa)

**Mudança estrutural:**
```typescript
interface TabState {
  id: string;
  name: string;
  clusterContext: string;
  
  // ✅ Staging area dentro do estado da aba
  stagingData: {
    stagedHPAs: StagingHPA[];
    stagedNodePools: StagingNodePool[];
  };
  
  pageState: { ... };
  pendingChanges: { ... };
}
```

**Vantagens:**
- Staging naturalmente isolado por aba
- Um único reducer gerencia tudo
- Serialização para sessão mais fácil

**Desvantagens:**
- TabContext fica muito grande
- Mistura lógica de UI com lógica de negócio

**Recomendação:** Opção A é mais limpa (separação de responsabilidades)

---

### 2.3 Backend: Isolamento de Clusters por Sessão HTTP

#### Problema a Resolver

**Cenário:**
- Aba 1 (Tab ID: `tab-001`) usa cluster `akspriv-prod-admin`
- Aba 2 (Tab ID: `tab-002`) usa cluster `akspriv-dev-admin`
- Aba 1 faz requisição `/api/v1/hpas?cluster=akspriv-prod-admin`
- **Simultaneamente**, Aba 2 faz requisição `/api/v1/hpas?cluster=akspriv-dev-admin`

**Atualmente:**
- Backend tem **um único pool de clients** compartilhado
- `SwitchContext()` muda contexto **globalmente**

**Solução Proposta:** **Session-Aware Backend**

#### Opção 1: Header `X-Tab-ID` (RECOMENDADO)

**Frontend envia:**
```http
GET /api/v1/hpas?cluster=akspriv-prod-admin
Authorization: Bearer poc-token-123
X-Tab-ID: tab-20251031-001
```

**Backend:**
```go
// Novo middleware: Extrai Tab ID do header
func TabSessionMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        tabID := c.GetHeader("X-Tab-ID")
        if tabID == "" {
            tabID = "default-tab"
        }
        c.Set("tabID", tabID)
        c.Next()
    }
}

// SessionManager gerencia clients por (tabID + cluster)
type SessionManager struct {
    clients map[string]*kubernetes.Clientset  // Key: "tabID:cluster"
    mutex   sync.RWMutex
}

func (sm *SessionManager) GetClient(tabID, cluster string) (*kubernetes.Clientset, error) {
    key := fmt.Sprintf("%s:%s", tabID, cluster)
    
    sm.mutex.RLock()
    if client, exists := sm.clients[key]; exists {
        sm.mutex.RUnlock()
        return client, nil
    }
    sm.mutex.RUnlock()
    
    // Criar novo client isolado para esta sessão
    sm.mutex.Lock()
    defer sm.mutex.Unlock()
    
    // Double-check
    if client, exists := sm.clients[key]; exists {
        return client, nil
    }
    
    client, err := createClientForCluster(cluster)
    if err != nil {
        return nil, err
    }
    
    sm.clients[key] = client
    return client, nil
}
```

**Handler refatorado:**
```go
func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")
    tabID := c.GetString("tabID")  // Extraído pelo middleware
    
    // Obter client isolado para esta aba
    client, err := h.sessionManager.GetClient(tabID, cluster)
    
    // Resto do código...
}
```

**Vantagens:**
- ✅ Simples de implementar
- ✅ Não requer mudanças estruturais no backend
- ✅ Backend permanece stateless (clients em cache, mas podem ser limpos)

**Desvantagens:**
- ⚠️ Mais memória (N abas × M clusters = N×M clients)
- ⚠️ Necessita garbage collection de clients inativos

#### Opção 2: Context Switching Dinâmico (NÃO Recomendado)

**Ideia:** Fazer `SwitchContext()` antes de **cada requisição**

```go
func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")
    
    // ❌ Troca contexto globalmente antes da operação
    h.kubeManager.SwitchContext(cluster)
    
    // Executa operação
    client, _ := h.kubeManager.GetClient(cluster)
    hpas, _ := client.ListHPAs(ctx, namespace)
}
```

**Problemas:**
- ❌ **Race condition crítica** se requisições concorrentes
- ❌ Aba 1 requisita cluster A, Aba 2 requisita cluster B simultaneamente
- ❌ Cliente de Aba 1 pode receber dados de cluster B (silenciosamente!)

**Conclusão:** NÃO usar esta abordagem

#### Opção 3: Client-Go Direto sem kubectl (Alternativa Robusta)

**Ideia:** Remover dependência do `kubectl config use-context`

**Implementação:**
```go
// Criar client diretamente via REST config
func createClientForCluster(clusterName string, kubeconfigPath string) (*kubernetes.Clientset, error) {
    config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
    if err != nil {
        return nil, err
    }
    
    // Sobrescrever context no config
    config.CurrentContext = clusterName
    
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        return nil, err
    }
    
    return clientset, nil
}
```

**Vantagens:**
- ✅ **Nenhuma dependência de kubectl global**
- ✅ Clients **verdadeiramente independentes**
- ✅ Sem race conditions

**Desvantagens:**
- ⚠️ Código mais verboso
- ⚠️ Precisa parsing manual do kubeconfig

**Recomendação:** Combinar com Opção 1 (Header `X-Tab-ID`)

---

### 2.4 Diagrama da Arquitetura Proposta

```
┌─────────────────────────────────────────────────────────────────┐
│                   FRONTEND (React)                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌────────────────────────┐      ┌──────────────────────────┐  │
│  │   TabContext (Multi)   │      │  StagingContext (Multi)  │  │
│  ├────────────────────────┤      ├──────────────────────────┤  │
│  │ Tab 1 (ID: tab-001)    │      │ tabStagingData:          │  │
│  │  - Cluster: Prod       │◄─────┤   tab-001: {             │  │
│  │  - Namespace: default  │      │     stagedHPAs: [...]    │  │
│  │  - State: {...}        │      │     stagedNodePools: [...│  │
│  ├────────────────────────┤      │   }                       │  │
│  │ Tab 2 (ID: tab-002)    │      │   tab-002: {             │  │
│  │  - Cluster: Dev        │◄─────┤     stagedHPAs: [...]    │  │
│  │  - Namespace: app-ns   │      │   }                       │  │
│  └────────────────────────┘      └──────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────┐          │
│  │         API Client (axios)                       │          │
│  │  - Adiciona header: X-Tab-ID: <activeTabId>     │          │
│  │  - Interceptor automático para todas requisições│          │
│  └──────────────────┬───────────────────────────────┘          │
│                     │                                           │
└─────────────────────┼───────────────────────────────────────────┘
                      │ HTTP Request
                      │ GET /api/v1/hpas?cluster=akspriv-prod-admin
                      │ X-Tab-ID: tab-001
                      ▼
┌─────────────────────────────────────────────────────────────────┐
│                      BACKEND (Go)                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────────────────────────────────────────┐          │
│  │       Middleware: TabSessionMiddleware()         │          │
│  │  - Extrai X-Tab-ID do header                     │          │
│  │  - Injeta no gin.Context                         │          │
│  └──────────────────┬───────────────────────────────┘          │
│                     │                                           │
│                     ▼                                           │
│  ┌──────────────────────────────────────────────────┐          │
│  │       SessionManager (NOVO!)                     │          │
│  │  clients: map[string]*Client                     │          │
│  │    - Key: "tab-001:akspriv-prod-admin"          │          │
│  │    - Key: "tab-001:akspriv-dev-admin"           │          │
│  │    - Key: "tab-002:akspriv-dev-admin"           │          │
│  │                                                  │          │
│  │  GetClient(tabID, cluster) → *Client            │          │
│  │  CleanupInactiveClients(timeout)                │          │
│  └──────────────────┬───────────────────────────────┘          │
│                     │                                           │
│                     ▼                                           │
│  ┌──────────────────────────────────────────────────┐          │
│  │            HTTP Handlers (Session-Aware)         │          │
│  │  - HPAHandler.List(cluster, tabID)               │          │
│  │  - NodePoolHandler.List(cluster, tabID)          │          │
│  └──────────────────────────────────────────────────┘          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                      │
                      ▼
          ┌──────────────────────────┐
          │  Kubernetes Clients      │
          │  (Isolados por Aba)      │
          ├──────────────────────────┤
          │  Client(tab-001, prod)   │
          │  Client(tab-001, dev)    │
          │  Client(tab-002, dev)    │
          └──────────────────────────┘
```

---

## 3. Plano de Implementação

### Fase 1: Refatoração Frontend (2-3 semanas)

#### Tarefa 1.1: Isolamento do StagingContext por Aba
**Complexidade:** MÉDIA  
**Estimativa:** 3-5 dias

**Passos:**
1. Criar tipo `TabStagingData`:
   ```typescript
   type TabStagingData = Map<string, {
     stagedHPAs: StagingHPA[];
     stagedNodePools: StagingNodePool[];
     loadedSessionInfo: LoadedSessionInfo | null;
   }>;
   ```

2. Refatorar `StagingProvider`:
   ```typescript
   const [tabStagingData, setTabStagingData] = useState<TabStagingData>(new Map());
   ```

3. Atualizar métodos para receber `tabId`:
   ```typescript
   const addHPAToStaging = useCallback((tabId: string, hpa: HPA) => {
     setTabStagingData(prev => {
       const current = prev.get(tabId) || { stagedHPAs: [], stagedNodePools: [], loadedSessionInfo: null };
       // Adicionar HPA...
       const updated = new Map(prev);
       updated.set(tabId, { ...current, stagedHPAs: [...current.stagedHPAs, stagingHPA] });
       return updated;
     });
   }, []);
   ```

4. Criar helper `useActiveTabStaging()`:
   ```typescript
   const useActiveTabStaging = () => {
     const { getActiveTab } = useTabManager();
     const staging = useStaging();
     
     const activeTab = getActiveTab();
     if (!activeTab) return null;
     
     return {
       stagedHPAs: staging.getStagedHPAs(activeTab.id),
       stagedNodePools: staging.getStagedNodePools(activeTab.id),
       addHPA: (hpa: HPA) => staging.addHPAToStaging(activeTab.id, hpa),
       // ...
     };
   };
   ```

5. **Testes:**
   - Criar 3 abas com clusters diferentes
   - Adicionar HPAs em staging em cada aba
   - Trocar entre abas - staging deve permanecer isolado
   - Fechar aba - staging deve ser limpo

**Arquivos afetados:**
- `internal/web/frontend/src/contexts/StagingContext.tsx`
- `internal/web/frontend/src/hooks/useActiveTabStaging.ts` (NOVO)
- Todos os componentes que usam `useStaging()` (HPAEditor, NodePoolEditor, StagingPanel, etc.)

---

#### Tarefa 1.2: Sincronização TabContext ↔ Index.tsx
**Complexidade:** BAIXA  
**Estimativa:** 1-2 dias

**Problema atual:**
```typescript
// Index.tsx mantém estado local
const [selectedCluster, setSelectedCluster] = useState("");

// TabContext mantém estado separado
const { updateActiveTabState } = useTabManager();
```

**Solução:**
```typescript
// ✅ Usar APENAS TabContext como fonte de verdade
const Index = () => {
  const { getActiveTab, updateActiveTabState } = useTabManager();
  const activeTab = getActiveTab();
  
  // Derive estado do TabContext
  const selectedCluster = activeTab?.pageState.selectedCluster || "";
  const selectedNamespace = activeTab?.pageState.selectedNamespace || "";
  
  // Atualizar apenas via TabContext
  const setSelectedCluster = (cluster: string) => {
    updateActiveTabState({ selectedCluster: cluster });
  };
};
```

**Testes:**
- Trocar de aba - estado deve atualizar automaticamente
- Modificar estado - deve persistir ao voltar para aba

**Arquivos afetados:**
- `internal/web/frontend/src/pages/Index.tsx`

---

#### Tarefa 1.3: Interceptor Axios com Header `X-Tab-ID`
**Complexidade:** BAIXA  
**Estimativa:** 1 dia

**Implementação:**
```typescript
// internal/web/frontend/src/lib/api/client.ts
import axios from 'axios';
import { useTabManager } from '@/contexts/TabContext';

const apiClient = axios.create({
  baseURL: '/api/v1',
  headers: {
    'Authorization': `Bearer ${import.meta.env.VITE_API_TOKEN || 'poc-token-123'}`,
  },
});

// Interceptor para adicionar X-Tab-ID
apiClient.interceptors.request.use((config) => {
  const { getActiveTab } = useTabManager();
  const activeTab = getActiveTab();
  
  if (activeTab) {
    config.headers['X-Tab-ID'] = activeTab.id;
  }
  
  return config;
});

export { apiClient };
```

**Problema:** `useTabManager()` não funciona fora de componentes React!

**Solução:** Criar store global para TabID ativo
```typescript
// internal/web/frontend/src/lib/tabStore.ts
let currentTabID: string | null = null;

export const setCurrentTabID = (tabID: string) => {
  currentTabID = tabID;
};

export const getCurrentTabID = () => currentTabID;

// TabContext atualiza store quando troca de aba
useEffect(() => {
  if (activeTabIndex >= 0 && tabs[activeTabIndex]) {
    setCurrentTabID(tabs[activeTabIndex].id);
  }
}, [activeTabIndex, tabs]);
```

**Testes:**
- Criar 2 abas
- Fazer requisição em cada aba
- Verificar backend recebe `X-Tab-ID` correto

**Arquivos afetados:**
- `internal/web/frontend/src/lib/api/client.ts`
- `internal/web/frontend/src/lib/tabStore.ts` (NOVO)
- `internal/web/frontend/src/contexts/TabContext.tsx`

---

#### Tarefa 1.4: UI de Tabs (TabBar Component)
**Complexidade:** MÉDIA  
**Estimativa:** 2-3 dias

**Features:**
- Visual similar a browser tabs
- Botão "+" para nova aba
- Botão "×" para fechar aba (com confirmação se houver mudanças)
- Indicador de mudanças pendentes (badge numérico)
- Drag-and-drop para reordenar abas (opcional)

**Wireframe:**
```
┌────────────────────────────────────────────────────────────┐
│ [+] │ Prod (2) [×] │ Dev [×] │ QA (5) [×] │               │
└────────────────────────────────────────────────────────────┘
      ↑          ↑         ↑        ↑
      │          │         │        └─ Badge de mudanças pendentes
      │          │         └────────── Aba inativa
      │          └──────────────────── Aba ativa (highlight)
      └─────────────────────────────── Botão nova aba
```

**Componentes:**
- `TabBar.tsx` - Container de abas
- `TabItem.tsx` - Aba individual
- `NewTabButton.tsx` - Botão "+"
- `TabCloseConfirmModal.tsx` - Modal de confirmação

**Testes:**
- Criar/fechar abas
- Badge atualiza quando adiciona itens ao staging
- Confirmação ao fechar aba com mudanças

**Arquivos afetados:**
- `internal/web/frontend/src/components/tabs/` (NOVO diretório)

---

### Fase 2: Refatoração Backend (3-4 semanas)

#### Tarefa 2.1: SessionManager - Pool de Clients por Tab
**Complexidade:** ALTA  
**Estimativa:** 5-7 dias

**Implementação:**
```go
// internal/web/session/manager.go (NOVO)
package session

import (
    "fmt"
    "sync"
    "time"
    "k8s.io/client-go/kubernetes"
)

type ClientKey struct {
    TabID   string
    Cluster string
}

func (k ClientKey) String() string {
    return fmt.Sprintf("%s:%s", k.TabID, k.Cluster)
}

type SessionManager struct {
    clients    map[string]*kubernetes.Clientset
    lastAccess map[string]time.Time
    mutex      sync.RWMutex
}

func NewSessionManager() *SessionManager {
    sm := &SessionManager{
        clients:    make(map[string]*kubernetes.Clientset),
        lastAccess: make(map[string]time.Time),
    }
    
    // Iniciar garbage collector de clients inativos
    go sm.cleanupLoop()
    
    return sm
}

func (sm *SessionManager) GetClient(tabID, cluster, kubeconfigPath string) (*kubernetes.Clientset, error) {
    key := ClientKey{TabID: tabID, Cluster: cluster}.String()
    
    // Read lock para verificar se existe
    sm.mutex.RLock()
    if client, exists := sm.clients[key]; exists {
        sm.lastAccess[key] = time.Now()
        sm.mutex.RUnlock()
        return client, nil
    }
    sm.mutex.RUnlock()
    
    // Write lock para criar
    sm.mutex.Lock()
    defer sm.mutex.Unlock()
    
    // Double-check
    if client, exists := sm.clients[key]; exists {
        sm.lastAccess[key] = time.Now()
        return client, nil
    }
    
    // Criar novo client
    client, err := createClientForCluster(cluster, kubeconfigPath)
    if err != nil {
        return nil, fmt.Errorf("failed to create client for %s/%s: %w", tabID, cluster, err)
    }
    
    sm.clients[key] = client
    sm.lastAccess[key] = time.Now()
    
    return client, nil
}

func (sm *SessionManager) CloseTab(tabID string) {
    sm.mutex.Lock()
    defer sm.mutex.Unlock()
    
    // Remover todos os clients desta aba
    for key := range sm.clients {
        if keyMatches(key, tabID) {
            delete(sm.clients, key)
            delete(sm.lastAccess, key)
        }
    }
}

func (sm *SessionManager) cleanupLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        sm.cleanupInactiveClients(30 * time.Minute)
    }
}

func (sm *SessionManager) cleanupInactiveClients(timeout time.Duration) {
    sm.mutex.Lock()
    defer sm.mutex.Unlock()
    
    now := time.Now()
    for key, lastAccess := range sm.lastAccess {
        if now.Sub(lastAccess) > timeout {
            delete(sm.clients, key)
            delete(sm.lastAccess, key)
        }
    }
}

// Helper function
func createClientForCluster(cluster, kubeconfigPath string) (*kubernetes.Clientset, error) {
    // Implementação usando client-go direto
    config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
    if err != nil {
        return nil, err
    }
    
    // Override context
    config.CurrentContext = cluster
    
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        return nil, err
    }
    
    return clientset, nil
}
```

**Testes:**
- Criar clients para múltiplas abas/clusters
- Verificar isolamento (requests simultâneos)
- Testar cleanup de clients inativos
- Memory profiling (verificar vazamento de memória)

**Arquivos afetados:**
- `internal/web/session/manager.go` (NOVO)
- `internal/web/session/manager_test.go` (NOVO)

---

#### Tarefa 2.2: Middleware TabSessionMiddleware
**Complexidade:** BAIXA  
**Estimativa:** 1-2 dias

**Implementação:**
```go
// internal/web/middleware/session.go (NOVO)
package middleware

import (
    "github.com/gin-gonic/gin"
)

func TabSessionMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        tabID := c.GetHeader("X-Tab-ID")
        
        // Fallback para default se não fornecido
        if tabID == "" {
            tabID = "default-tab"
        }
        
        // Injetar no contexto Gin
        c.Set("tabID", tabID)
        
        c.Next()
    }
}

// Helper para extrair tabID nos handlers
func GetTabID(c *gin.Context) string {
    if tabID, exists := c.Get("tabID"); exists {
        return tabID.(string)
    }
    return "default-tab"
}
```

**Registro no servidor:**
```go
// internal/web/server.go
func (s *Server) setupRoutes() {
    // ...
    
    // API v1 (com auth + session)
    api := s.router.Group("/api/v1")
    api.Use(middleware.AuthMiddleware(s.token))
    api.Use(middleware.TabSessionMiddleware())  // ✅ NOVO
    
    // ...
}
```

**Testes:**
- Requisição COM header `X-Tab-ID`
- Requisição SEM header (deve usar "default-tab")
- Verificar `c.GetString("tabID")` nos handlers

**Arquivos afetados:**
- `internal/web/middleware/session.go` (NOVO)
- `internal/web/server.go`

---

#### Tarefa 2.3: Refatoração de Handlers para SessionManager
**Complexidade:** MÉDIA  
**Estimativa:** 3-5 dias

**Exemplo - HPAHandler:**
```go
// ❌ ANTES
type HPAHandler struct {
    kubeManager *config.KubeConfigManager
}

func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")
    client, err := h.kubeManager.GetClient(cluster)
    // ...
}

// ✅ DEPOIS
type HPAHandler struct {
    sessionManager *session.SessionManager
    kubeconfigPath string
}

func (h *HPAHandler) List(c *gin.Context) {
    cluster := c.Query("cluster")
    tabID := middleware.GetTabID(c)
    
    // Obter client isolado para esta aba/cluster
    client, err := h.sessionManager.GetClient(tabID, cluster, h.kubeconfigPath)
    if err != nil {
        c.JSON(500, gin.H{"error": "Failed to get client"})
        return
    }
    
    // Resto do código...
}
```

**Handlers a refatorar:**
- `HPAHandler.List()`
- `HPAHandler.Update()`
- `NodePoolHandler.List()`
- `NodePoolHandler.Update()`
- `NamespaceHandler.List()`
- `CronJobHandler.*`
- `PrometheusHandler.*`

**Testes para cada handler:**
- Requisições simultâneas de abas diferentes
- Verificar dados retornados correspondem ao cluster correto
- Sem leakage de dados entre abas

**Arquivos afetados:**
- `internal/web/handlers/hpas.go`
- `internal/web/handlers/nodepools.go`
- `internal/web/handlers/namespaces.go`
- `internal/web/handlers/cronjobs.go`
- `internal/web/handlers/prometheus.go`

---

#### Tarefa 2.4: Endpoint de Fechamento de Aba
**Complexidade:** BAIXA  
**Estimativa:** 1 dia

**Nova rota:**
```go
// internal/web/handlers/tabs.go (NOVO)
package handlers

type TabHandler struct {
    sessionManager *session.SessionManager
}

func NewTabHandler(sm *session.SessionManager) *TabHandler {
    return &TabHandler{sessionManager: sm}
}

// CloseTab limpa clients da aba
func (h *TabHandler) CloseTab(c *gin.Context) {
    tabID := c.Param("tabId")
    
    if tabID == "" {
        c.JSON(400, gin.H{"error": "tabId required"})
        return
    }
    
    // Limpar clients desta aba
    h.sessionManager.CloseTab(tabID)
    
    c.JSON(200, gin.H{
        "message": "Tab closed successfully",
        "tabId":   tabID,
    })
}
```

**Rota:**
```go
// internal/web/server.go
api.DELETE("/tabs/:tabId", tabHandler.CloseTab)
```

**Frontend - chamada ao fechar aba:**
```typescript
const closeTab = useCallback(async (index: number) => {
  const tab = tabs[index];
  
  // Chamar backend para limpar clients
  try {
    await apiClient.delete(`/tabs/${tab.id}`);
  } catch (error) {
    console.error("Failed to close tab on backend:", error);
  }
  
  // Remover do state local
  dispatch({ type: 'CLOSE_TAB', payload: { index } });
}, [tabs]);
```

**Testes:**
- Fechar aba - backend deve remover clients
- Memory check - clients devem ser garbage collected

**Arquivos afetados:**
- `internal/web/handlers/tabs.go` (NOVO)
- `internal/web/server.go`
- `internal/web/frontend/src/contexts/TabContext.tsx`

---

### Fase 3: Features Avançadas (2-3 semanas)

#### Tarefa 3.1: Apply All Tabs (Operação em Lote)
**Complexidade:** ALTA  
**Estimativa:** 5-7 dias

**Requisito:**
- Botão "Apply All Tabs" aplica staging de TODAS as abas sequencialmente
- Progress tracking individual por aba
- Rollback automático se aba falhar (opcional)

**UI:**
```
┌─────────────────────────────────────────────┐
│      Apply Changes - All Tabs               │
├─────────────────────────────────────────────┤
│                                             │
│  Tab 1 (Prod): [████████████░░░░] 75%      │
│    ✅ 3 HPAs applied                        │
│    ⏳ 1 Node Pool in progress...            │
│                                             │
│  Tab 2 (Dev): [████████████████] 100%      │
│    ✅ 5 HPAs applied                        │
│    ✅ 2 Node Pools applied                  │
│                                             │
│  Tab 3 (QA): [░░░░░░░░░░░░░░░░] Waiting... │
│                                             │
│  [Cancel]                     [Continue]    │
└─────────────────────────────────────────────┘
```

**Backend:**
```go
// Novo endpoint: POST /api/v1/apply-all-tabs
type ApplyAllTabsRequest struct {
    Tabs []struct {
        TabID   string          `json:"tabId"`
        Changes []HPAChange     `json:"changes"`
        NodePoolChanges []NodePoolChange `json:"nodePoolChanges"`
    } `json:"tabs"`
}

func (h *ApplyHandler) ApplyAllTabs(c *gin.Context) {
    // Aplicar mudanças de cada aba sequencialmente
    // Retornar stream de eventos (Server-Sent Events)
}
```

**Testes:**
- 3 abas com mudanças diferentes
- Aplicar tudo - verificar ordem de execução
- Simular falha em aba 2 - verificar rollback/tratamento

**Arquivos afetados:**
- `internal/web/handlers/apply.go` (NOVO)
- `internal/web/frontend/src/components/ApplyAllTabsModal.tsx` (NOVO)

---

#### Tarefa 3.2: Persistência de Sessão Multi-Tab
**Complexidade:** MÉDIA  
**Estimativa:** 3-4 dias

**Requisito:**
- Salvar estado de TODAS as abas em sessão
- Carregar sessão restaura todas as abas

**Formato de sessão:**
```json
{
  "sessionName": "Stress Test - 2025-10-31",
  "createdAt": "2025-10-31T14:30:00Z",
  "tabs": [
    {
      "name": "Prod Cluster",
      "clusterContext": "akspriv-prod-admin",
      "changes": [...],
      "nodePoolChanges": [...]
    },
    {
      "name": "Dev Cluster",
      "clusterContext": "akspriv-dev-admin",
      "changes": [...],
      "nodePoolChanges": [...]
    }
  ]
}
```

**Backend:**
```go
// Atualizar SessionManager para salvar múltiplas abas
type Session struct {
    SessionName string          `json:"sessionName"`
    CreatedAt   time.Time       `json:"createdAt"`
    Tabs        []TabSession    `json:"tabs"`
}

type TabSession struct {
    Name            string            `json:"name"`
    ClusterContext  string            `json:"clusterContext"`
    Changes         []models.HPAChange     `json:"changes"`
    NodePoolChanges []models.NodePoolChange `json:"nodePoolChanges"`
}
```

**Frontend:**
```typescript
const loadSession = async (sessionName: string) => {
  const session = await apiClient.get(`/sessions/${sessionName}`);
  
  // Fechar abas atuais
  closeAllTabs();
  
  // Criar aba para cada tab da sessão
  session.tabs.forEach(tab => {
    const newTab = addTab(tab.name, tab.clusterContext);
    
    // Carregar staging da aba
    loadTabStaging(newTab.id, tab.changes, tab.nodePoolChanges);
  });
};
```

**Testes:**
- Salvar sessão com 3 abas
- Fechar aplicação
- Carregar sessão - verificar todas as abas restauradas
- Staging de cada aba deve estar correto

**Arquivos afetados:**
- `internal/session/manager.go` (atualizar)
- `internal/web/handlers/sessions.go` (atualizar)
- `internal/web/frontend/src/components/SaveSessionModal.tsx`
- `internal/web/frontend/src/components/LoadSessionModal.tsx`

---

#### Tarefa 3.3: Dashboard Multi-Cluster
**Complexidade:** MÉDIA  
**Estimativa:** 3-4 dias

**Requisito:**
- Dashboard consolidado mostrando métricas de TODOS os clusters abertos em abas
- Gráficos comparativos side-by-side

**UI:**
```
┌───────────────────────────────────────────────────────┐
│              Multi-Cluster Dashboard                  │
├───────────────────────────────────────────────────────┤
│                                                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │   Prod      │  │    Dev      │  │     QA      │  │
│  ├─────────────┤  ├─────────────┤  ├─────────────┤  │
│  │ CPU: 75%    │  │ CPU: 45%    │  │ CPU: 30%    │  │
│  │ Mem: 60%    │  │ Mem: 50%    │  │ Mem: 40%    │  │
│  │ Nodes: 10   │  │ Nodes: 5    │  │ Nodes: 3    │  │
│  │ Pods: 120   │  │ Pods: 80    │  │ Pods: 50    │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  │
│                                                       │
│  ┌───────────────────────────────────────────────┐  │
│  │     CPU Usage Comparison (Last 1h)            │  │
│  │  [Line chart com 3 linhas: Prod, Dev, QA]    │  │
│  └───────────────────────────────────────────────┘  │
│                                                       │
└───────────────────────────────────────────────────────┘
```

**Backend:**
```go
// Novo endpoint: GET /api/v1/dashboard/multi-cluster
func (h *DashboardHandler) MultiCluster(c *gin.Context) {
    tabID := middleware.GetTabID(c)
    
    // Buscar clusters de todas as abas abertas
    // (necessita novo método no SessionManager para listar abas ativas)
    
    metrics := []ClusterMetrics{}
    for _, cluster := range activeClusters {
        m, _ := h.kubeManager.GetClusterMetrics(cluster)
        metrics = append(metrics, m)
    }
    
    c.JSON(200, metrics)
}
```

**Testes:**
- Abrir 3 abas com clusters diferentes
- Dashboard deve mostrar métricas de todos
- Fechar aba - dashboard deve atualizar

**Arquivos afetados:**
- `internal/web/handlers/dashboard.go` (NOVO)
- `internal/web/frontend/src/components/MultiClusterDashboard.tsx` (NOVO)

---

### Fase 4: Otimização e Testes (1-2 semanas)

#### Tarefa 4.1: Testes de Concorrência
**Complexidade:** ALTA  
**Estimativa:** 3-5 dias

**Cenários de teste:**
1. **Stress Test - 10 abas simultâneas:**
   - Criar 10 abas, cada uma em cluster diferente
   - Fazer 100 requisições GET/POST simultâneas
   - Verificar isolamento de dados
   - Memory profiling (detectar vazamentos)

2. **Race Condition Test:**
   - 2 abas no mesmo cluster
   - Editar MESMO HPA simultaneamente
   - Verificar conflitos de escrita

3. **Client Pool Test:**
   - Criar 50 clients (5 abas × 10 clusters)
   - Verificar garbage collection funciona
   - Memory footprint deve estabilizar

**Tools:**
- Go: `go test -race` para detectar race conditions
- `pprof` para memory profiling
- Artillery para load testing HTTP

**Arquivos afetados:**
- `internal/web/session/manager_test.go`
- `tests/e2e/multi_tab_test.go` (NOVO)

---

#### Tarefa 4.2: Otimização de Memória
**Complexidade:** MÉDIA  
**Estimativa:** 2-3 dias

**Melhorias:**
1. **Lazy Loading de Clients:**
   - Não criar client até primeira requisição
   - Client pool com LRU eviction

2. **Compressão de Staging Data:**
   - Staging area grande pode consumir muita memória frontend
   - Considerar compressão ou pagination

3. **Cleanup Agressivo:**
   - Reduzir timeout de clients inativos (30min → 10min)
   - Limpar staging de aba fechada imediatamente

**Benchmarks:**
- Baseline: Memória com 1 aba
- Target: Memória com 10 abas < 2× baseline

**Arquivos afetados:**
- `internal/web/session/manager.go`
- `internal/web/frontend/src/contexts/StagingContext.tsx`

---

#### Tarefa 4.3: Documentação e Guias
**Complexidade:** BAIXA  
**Estimativa:** 2-3 dias

**Documentos a criar:**
1. `MULTI_TAB_USAGE_GUIDE.md` - Como usar múltiplas abas
2. `MULTI_TAB_ARCHITECTURE.md` - Arquitetura técnica
3. `TROUBLESHOOTING_MULTI_TAB.md` - Problemas comuns
4. Atualizar `CLAUDE.md` com novas features

**Conteúdo:**
- Screenshots da UI
- Exemplos de uso (stress tests)
- Limitações conhecidas
- Best practices

**Arquivos afetados:**
- `Docs/MULTI_TAB_*.md` (NOVO)
- `CLAUDE.md`
- `README.md`

---

## 4. Estimativa de Complexidade e Riscos

### 4.1 Estimativa de Tempo

| Fase | Tarefas | Dias | Semanas |
|------|---------|------|---------|
| **Fase 1: Frontend** | 4 tarefas | 7-13 dias | 1.5-2.5 semanas |
| **Fase 2: Backend** | 4 tarefas | 10-15 dias | 2-3 semanas |
| **Fase 3: Features Avançadas** | 3 tarefas | 11-15 dias | 2-3 semanas |
| **Fase 4: Otimização e Testes** | 3 tarefas | 7-11 dias | 1.5-2 semanas |
| **Buffer (20%)** | - | 7-11 dias | 1.5-2 semanas |
| **TOTAL** | 14 tarefas | **42-65 dias** | **8-13 semanas** |

**Equipe:**
- 1 desenvolvedor full-stack: **10-13 semanas**
- 2 desenvolvedores (1 frontend + 1 backend): **6-8 semanas**

---

### 4.2 Complexidade Técnica

| Componente | Complexidade | Justificativa |
|------------|--------------|---------------|
| **StagingContext Refactoring** | 🔴 ALTA | Mudança estrutural profunda, afeta muitos componentes |
| **SessionManager Backend** | 🔴 ALTA | Concorrência, memory management, garbage collection |
| **Tab UI Components** | 🟡 MÉDIA | UI complexa, mas padrão conhecido |
| **Interceptor Axios** | 🟢 BAIXA | Pattern simples, bem documentado |
| **Apply All Tabs** | 🔴 ALTA | Orquestração complexa, error handling |
| **Multi-Tab Sessions** | 🟡 MÉDIA | Estrutura de dados complexa, mas lógica direta |
| **Testes de Concorrência** | 🔴 ALTA | Difícil reproduzir race conditions, debugging complexo |

---

### 4.3 Riscos Identificados

#### Risco 1: Race Conditions no Backend
**Severidade:** 🔴 ALTA  
**Probabilidade:** 🟡 MÉDIA

**Descrição:**
- Múltiplas abas fazendo requisições simultâneas para mesmo cluster
- Conflitos de escrita (ex: 2 abas editando mesmo HPA)
- Clients compartilhados podem causar estado inconsistente

**Mitigação:**
- ✅ Usar `sync.RWMutex` em **TODAS** as operações do SessionManager
- ✅ Testes com `go test -race`
- ✅ Locks otimistas no frontend (versionamento de recursos)

#### Risco 2: Vazamento de Memória
**Severidade:** 🔴 ALTA  
**Probabilidade:** 🟡 MÉDIA

**Descrição:**
- Cada aba cria clients Kubernetes (heavy objects)
- Staging area pode crescer indefinidamente
- Frontend React pode ter memory leaks (closures)

**Mitigação:**
- ✅ Garbage collection agressivo de clients inativos (10min)
- ✅ Limpar staging ao fechar aba
- ✅ Memory profiling com `pprof` (Go) e Chrome DevTools (React)
- ✅ Limitar número máximo de abas (default: 10)

#### Risco 3: Complexidade de State Management
**Severidade:** 🟡 MÉDIA  
**Probabilidade:** 🔴 ALTA

**Descrição:**
- Estado distribuído entre TabContext, StagingContext e Index.tsx
- Fácil criar bugs de sincronização
- Debugging difícil

**Mitigação:**
- ✅ Usar **single source of truth** (TabContext)
- ✅ Derive estado ao invés de duplicar
- ✅ Testes de integração para fluxos críticos
- ✅ Logging detalhado de mudanças de estado (dev mode)

#### Risco 4: Experiência de Usuário Confusa
**Severidade:** 🟡 MÉDIA  
**Probabilidade:** 🟡 MÉDIA

**Descrição:**
- Usuário pode se perder com muitas abas abertas
- Difícil saber qual aba tem quais mudanças
- Fechar aba acidentalmente perde trabalho

**Mitigação:**
- ✅ Badge numérico mostrando mudanças pendentes
- ✅ Confirmação ao fechar aba com mudanças
- ✅ Auto-save de staging (localStorage)
- ✅ Visual claro de qual aba está ativa
- ✅ Tooltip com informações de cada aba

#### Risco 5: Cluster Switching Lento
**Severidade:** 🟢 BAIXA  
**Probabilidade:** 🟡 MÉDIA

**Descrição:**
- Criar client Kubernetes pode demorar (autenticação, rede)
- Trocar de aba pode parecer lento

**Mitigação:**
- ✅ Pre-warm de clients (criar em background)
- ✅ Loading states visuais
- ✅ Cache agressivo de clients

---

## 5. Alternativas Consideradas

### Alternativa 1: Abas Apenas no Frontend (Sem Isolamento Backend)
**Descrição:**
- Backend permanece como está (pool global de clients)
- Abas são puramente UI (troca de estado local)
- `SwitchContext()` continua global

**Vantagens:**
- ✅ Implementação muito mais simples
- ✅ Sem mudanças no backend
- ✅ Rápido de desenvolver (2-3 semanas)

**Desvantagens:**
- ❌ **NÃO resolve problema de clusters simultâneos**
- ❌ Trocar de aba faz `SwitchContext()` - afeta todas as abas
- ❌ Não é multi-cluster verdadeiro

**Conclusão:** ❌ Não atende requisito principal

---

### Alternativa 2: WebSocket com Session ID
**Descrição:**
- Frontend estabelece WebSocket connection por aba
- Backend mantém session state no WebSocket handler
- Cada aba tem client isolado no backend

**Vantagens:**
- ✅ Isolamento verdadeiro por aba
- ✅ Real-time updates fáceis
- ✅ Session management nativo

**Desvantagens:**
- ❌ Mudança arquitetural muito grande (HTTP → WebSocket)
- ❌ Complexidade adicional (reconnection, state sync)
- ❌ Difícil debugar
- ❌ Não funciona bem com load balancers

**Conclusão:** ⚠️ Over-engineering para o caso de uso

---

### Alternativa 3: Cluster como Query Param (Stateless Total)
**Descrição:**
- Remover conceito de "contexto ativo" completamente
- TODAS as requisições passam `cluster` como param
- Backend cria client on-demand a cada request
- Sem pool de clients (criar e destruir)

**Vantagens:**
- ✅ Absolutamente stateless
- ✅ Sem race conditions
- ✅ Escalabilidade horizontal fácil

**Desvantagens:**
- ❌ Performance ruim (criar client a cada request é caro)
- ❌ Muita pressão na API do Kubernetes
- ❌ Timeout em operações longas

**Conclusão:** ⚠️ Performance inaceitável

---

### Alternativa 4: Backend Separado por Cluster
**Descrição:**
- Rodar uma instância do backend para CADA cluster
- Frontend se conecta a múltiplos backends
- Ex: `http://localhost:8081` (prod), `http://localhost:8082` (dev)

**Vantagens:**
- ✅ Isolamento total (processos separados)
- ✅ Sem race conditions
- ✅ Fácil debugar (logs separados)

**Desvantagens:**
- ❌ Complexidade operacional (gerenciar N processos)
- ❌ Consumo de recursos (N × memória/CPU)
- ❌ Difícil escalar (1 backend por cluster = 70+ processos?)
- ❌ CORS issues com múltiplas origins

**Conclusão:** ❌ Não escalável para 70+ clusters

---

### Comparação de Alternativas

| Critério | Alt 1: Frontend Only | Alt 2: WebSocket | Alt 3: Stateless | Alt 4: Multi Backend | **Solução Proposta** |
|----------|---------------------|------------------|------------------|----------------------|---------------------|
| **Isolamento Real** | ❌ Não | ✅ Sim | ✅ Sim | ✅ Sim | ✅ Sim |
| **Performance** | ✅ Boa | ✅ Boa | ❌ Ruim | ✅ Boa | ✅ Boa |
| **Complexidade** | ✅ Baixa | ❌ Alta | ✅ Média | ❌ Alta | 🟡 Média-Alta |
| **Escalabilidade** | ✅ Boa | 🟡 Média | ✅ Excelente | ❌ Ruim | ✅ Boa |
| **Tempo Implementação** | 2-3 sem | 6-8 sem | 3-4 sem | 4-6 sem | **8-10 sem** |
| **Riscos** | 🟢 Baixo | 🔴 Alto | 🟡 Médio | 🟡 Médio | 🟡 Médio |

---

## 6. Recomendações Técnicas

### 6.1 Arquitetura Recomendada

**Implementar:**
- ✅ **Header `X-Tab-ID`** - Simples e eficaz
- ✅ **SessionManager com Pool de Clients** - Melhor tradeoff performance/complexidade
- ✅ **StagingContext isolado por aba** - State management limpo
- ✅ **Garbage collection de clients inativos** - Previne vazamento de memória

**Não implementar (pelo menos inicialmente):**
- ❌ WebSockets - Over-engineering
- ❌ Multi-backend - Complexidade operacional
- ❌ Stateless total - Performance ruim

---

### 6.2 Ordem de Implementação Recomendada

**Prioridade 1 (MVP):**
1. Refatoração StagingContext (Tarefa 1.1)
2. SessionManager Backend (Tarefa 2.1)
3. Middleware TabSession (Tarefa 2.2)
4. Refatoração Handlers (Tarefa 2.3)
5. Tab UI básico (Tarefa 1.4)

**Após MVP funcionar:**
6. Sincronização TabContext ↔ Index (Tarefa 1.2)
7. Interceptor Axios (Tarefa 1.3)
8. Endpoint Close Tab (Tarefa 2.4)

**Features avançadas:**
9. Apply All Tabs (Tarefa 3.1)
10. Persistência Multi-Tab (Tarefa 3.2)

**Polimento:**
11. Multi-Cluster Dashboard (Tarefa 3.3)
12. Testes de Concorrência (Tarefa 4.1)
13. Otimização (Tarefa 4.2)
14. Documentação (Tarefa 4.3)

---

### 6.3 Métricas de Sucesso

**Funcionalidade:**
- ✅ 10 abas abertas simultaneamente sem crashes
- ✅ Cada aba opera em cluster independente
- ✅ Staging area 100% isolado entre abas
- ✅ Apply All Tabs funciona para 3+ abas

**Performance:**
- ✅ Memory footprint < 2× baseline com 10 abas
- ✅ Troca de aba < 100ms (percepção de instantâneo)
- ✅ Client creation < 2s (aceitável com loading state)

**Qualidade:**
- ✅ Zero race conditions detectadas (`go test -race`)
- ✅ Memory leaks < 1MB/hora (Go) e < 5MB/hora (React)
- ✅ Testes E2E cobrem cenários principais

---

### 6.4 Considerações de Produção

**Limitações Recomendadas:**
- Max 10 abas por usuário (evitar abuso de recursos)
- Timeout de 10min para clients inativos (garbage collection)
- Max 100 itens em staging por aba (prevenir UI lenta)

**Monitoring:**
- Dashboard com métricas: `active_tabs`, `active_clients`, `memory_usage`
- Alertas para memory leaks ou client pool crescendo indefinidamente

**Segurança:**
- Validar `X-Tab-ID` não contém caracteres perigosos
- Rate limiting por Tab ID (prevenir abuse)
- Sanitizar cluster names (evitar command injection)

---

## 7. Conclusão

### Viabilidade

**Técnica:** ✅ VIÁVEL  
A implementação é tecnicamente possível com as tecnologias atuais (Go, React, client-go).

**Complexidade:** 🟡 MÉDIA-ALTA  
Não é trivial, mas é factível com planejamento adequado e testes rigorosos.

**Prazo:** 8-13 semanas  
Com 1 desenvolvedor full-stack ou 6-8 semanas com 2 desenvolvedores.

### Recomendação Final

✅ **RECOMENDO A IMPLEMENTAÇÃO** seguindo o plano proposto:

1. **MVP primeiro** (Tarefas prioritárias 1-5) - 4-5 semanas
2. **Validar com usuários** - Testes de usabilidade
3. **Iterar** com features avançadas (Tarefas 6-14) - 4-5 semanas

**Alternativa conservadora:**
Se orçamento for limitado, implementar apenas **Fase 1 + Fase 2** (6-7 semanas) e avaliar ROI antes de investir em features avançadas.

---

**Documento criado por:** Claude Code (Anthropic)  
**Data:** 31 de outubro de 2025  
**Versão:** 1.0
