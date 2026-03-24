# Plano de Implementação: Visualização Detalhada de Nodes (VMSS Instances)

## Status: Em Progresso

**Data de Início**: 11/02/2026
**Última Atualização**: 11/02/2026

---

## Visão Geral

Implementação de visualização e monitoramento completo de nodes (VMSS instances) individuais dentro de cada node pool na aba Node Pools do K8s HPA Manager.

---

## Fases de Implementação

### Fase 1: Estrutura de Dados (Backend + Frontend) - [ ]

**Backend**: `internal/models/types.go`

- [ ] Adicionar struct `NodeInfo`
- [ ] Adicionar struct `NodeCondition`
- [ ] Adicionar struct `NodeEvent`
- [ ] Adicionar struct `PodOnNode`
- [ ] Adicionar struct `NodeDetailsResponse`

**Frontend**: `internal/web/frontend/src/lib/api/types.ts`

- [ ] Adicionar interface `NodeInfo`
- [ ] Adicionar interface `NodeCondition`
- [ ] Adicionar interface `NodeTaint`
- [ ] Adicionar interface `NodeEvent`
- [ ] Adicionar interface `PodOnNode`
- [ ] Adicionar interface `NodeDetailsResponse`
- [ ] Adicionar interface `NodesListResponse`

**Campos Principais (NodeInfo)**:
```
- Name, Status, NodePoolName, ClusterName
- KubernetesVersion, ProviderID, InternalIP, ExternalIP
- Age, CreatedAt
- CPU/Memory/Pods Capacity e Allocatable
- CPU/Memory/Disk Usage (com percentuais)
- PodsRunning, PodsTotal
- Conditions, Taints, Labels, Annotations
- Unschedulable flag (cordon status)
```

---

### Fase 2: Backend - Client Methods - [ ]

**Arquivo**: `internal/kubernetes/client.go`

#### 2.1 GetNodesWithMetrics - [ ]
- [ ] Implementar método principal
- [ ] Reutilizar `GetNodesInNodePool()` para obter lista de nodes
- [ ] Buscar detalhes de cada node em paralelo (goroutines + sync.WaitGroup)
- [ ] Ordenar nodes por nome
- [ ] Retornar `[]models.NodeInfo`

#### 2.2 getNodeInfo (helper privado) - [ ]
- [ ] Buscar node via Kubernetes API
- [ ] Extrair informações básicas (nome, IPs, version, age)
- [ ] Extrair Capacity e Allocatable (CPU, Memory, Pods)
- [ ] Processar Conditions (Ready, MemoryPressure, DiskPressure, etc)
- [ ] Determinar status geral do node
- [ ] Chamar `enrichNodeWithMetrics()`
- [ ] Chamar `enrichNodeWithPodCount()`

#### 2.3 enrichNodeWithMetrics - [ ]
- [ ] Tentar obter métricas do Metrics Server (primário)
- [ ] Calcular percentuais de uso (usado/allocatable * 100)
- [ ] Fallback para Prometheus se Metrics Server falhar
- [ ] Tratar erros gracefully (não falhar se métricas indisponíveis)

#### 2.4 enrichNodeWithPodCount - [ ]
- [ ] Listar pods do node (fieldSelector: spec.nodeName)
- [ ] Contar total de pods
- [ ] Contar pods em fase Running
- [ ] Atualizar NodeInfo

#### 2.5 GetNodeDetails - [ ]
- [ ] Implementar método para detalhes completos
- [ ] Chamar `getNodeInfo()` para dados do node
- [ ] Chamar `getPodsOnNode()` para lista de pods
- [ ] Chamar `getNodeEvents()` para eventos recentes
- [ ] Executar `kubectl describe node` (opcional)
- [ ] Retornar `NodeDetailsResponse`

#### 2.6 getPodsOnNode - [ ]
- [ ] Listar pods do node
- [ ] Agregar recursos de todos os containers (CPU/Memory requests/limits)
- [ ] Contar restarts totais
- [ ] Retornar `[]models.PodOnNode`

#### 2.7 getNodeEvents - [ ]
- [ ] Buscar eventos do node (involvedObject.kind=Node)
- [ ] Ordenar por timestamp (mais recente primeiro)
- [ ] Limitar a 20 eventos mais recentes
- [ ] Retornar `[]models.NodeEvent`

#### 2.8 Funções auxiliares - [ ]
- [ ] `determineNodeStatus()` - Status geral baseado em conditions
- [ ] `formatAge()` - Duração em formato compacto (2d5h, 30m, 45s)
- [ ] `formatMemory()` - Bytes em Gi/Mi/Ki
- [ ] `parseQuantity()` - Converter resource.Quantity para float64
- [ ] `executeKubectlDescribeNode()` - Executar kubectl describe

---

### Fase 3: Backend - REST API Handlers - [ ]

**Arquivo**: `internal/web/handlers/nodepools.go`

#### 3.1 Handler ListNodesInNodePool - [ ]
- [ ] Validar parâmetros (cluster, nodepool)
- [ ] Obter Kubernetes client via kubeManager
- [ ] Chamar `k8sClient.GetNodesWithMetrics()`
- [ ] Retornar JSON com lista de nodes e metadados
- [ ] Tratamento de erros (400, 500)

#### 3.2 Handler GetNodeDetails - [ ]
- [ ] Validar parâmetros (cluster, nodepool, node)
- [ ] Obter Kubernetes client
- [ ] Chamar `k8sClient.GetNodeDetails()`
- [ ] Retornar JSON com detalhes completos
- [ ] Tratamento de erros

**Arquivo**: `internal/web/server.go`

#### 3.3 Registrar rotas - [ ]
- [ ] Adicionar rota `GET /api/v1/nodes/:cluster/:nodepool`
- [ ] Adicionar rota `GET /api/v1/nodes/:cluster/:nodepool/:node`

---

### Fase 4: Frontend - React Hooks - [ ]

**Arquivo**: `internal/web/frontend/src/hooks/useNodes.ts` (novo)

#### 4.1 Hook useNodes - [ ]
- [ ] Implementar com React Query
- [ ] Query key: `['nodes', cluster, nodePoolName]`
- [ ] Enabled quando cluster e nodePoolName existem
- [ ] staleTime: 30000ms (30 segundos)
- [ ] refetchInterval: 60000ms (auto-refresh a cada 1 minuto)

#### 4.2 Hook useNodeDetails - [ ]
- [ ] Implementar com React Query
- [ ] Query key: `['node-details', cluster, nodePoolName, nodeName]`
- [ ] Enabled quando todos os parâmetros existem
- [ ] staleTime: 20000ms (20 segundos)

**Arquivo**: `internal/web/frontend/src/lib/api/client.ts`

#### 4.3 Métodos API Client - [ ]
- [ ] `getNodesInNodePool(cluster, nodePoolName): Promise<NodesListResponse>`
- [ ] `getNodeDetails(cluster, nodePoolName, nodeName): Promise<NodeDetailsResponse>`

---

### Fase 5: Frontend - Componentes React - [ ]

**Arquivo**: `internal/web/frontend/src/components/NodeDetailsTab.tsx` (novo)

#### 5.1 Componente NodeDetailsTab - [ ]
- [ ] Estrutura básica com Card
- [ ] Header com título, badge de count, botão Refresh
- [ ] Usar hook `useNodes()`
- [ ] Estados: loading, empty, error
- [ ] Tabela com colunas:
  - Node Name
  - Status (Badge colorido)
  - CPU Usage (Progress + percentual + texto usado/allocatable)
  - Memory Usage (Progress + percentual + texto usado/allocatable)
  - Disk Usage (Progress + percentual)
  - Pods (count rodando/capacidade)
  - Age
  - K8s Version
  - Botão Details
- [ ] Click no botão Details abre modal
- [ ] Estado `selectedNode` para controlar modal

#### 5.2 Função getStatusBadge - [ ]
- [ ] Ready: variant="success"
- [ ] NotReady: variant="destructive"
- [ ] SchedulingDisabled: variant="warning"
- [ ] Default: variant="secondary"

#### 5.3 Função getUsageColor - [ ]
- [ ] >= 90%: bg-red-500
- [ ] >= 75%: bg-yellow-500
- [ ] < 75%: bg-green-500

**Arquivo**: `internal/web/frontend/src/components/NodeDetailsModal.tsx` (novo)

#### 5.4 Componente NodeDetailsModal - [ ]
- [ ] Dialog com max-w-6xl e max-h-[90vh]
- [ ] Usar hook `useNodeDetails()`
- [ ] Header com título dinâmico (Node Details: {nodeName})
- [ ] Description com nodePoolName e cluster
- [ ] Tabs com 5 abas:
  - Overview
  - Metrics
  - Pods (count dinâmico)
  - Events (count dinâmico)
  - Describe

#### 5.5 Sub-componente OverviewTab - [ ]
- [ ] ScrollArea com altura fixa
- [ ] Card: Basic Information (grid 2 colunas)
  - Name, Status, Internal IP, External IP
  - Kubernetes Version, Age
- [ ] Card: Node Conditions
  - Lista de conditions com Badge de status
  - Exibir mensagem quando disponível
- [ ] Card: Labels (opcional se existir)
  - Badges com key:value

#### 5.6 Sub-componente MetricsTab - [ ]
- [ ] ScrollArea com altura fixa
- [ ] Grid 2 colunas com Cards:
  - CPU Card (Capacity, Allocatable, Used, Usage %)
  - Memory Card (Capacity, Allocatable, Used, Usage %)
  - Pods Card (Capacity, Allocatable, Running, Total)
  - Disk Card (Usage % apenas)

#### 5.7 Sub-componente PodsTab - [ ]
- [ ] ScrollArea com altura fixa
- [ ] Tabela com colunas:
  - Name (font-medium)
  - Namespace
  - Phase (Badge colorido)
  - CPU Request
  - Memory Request
  - Restarts (Badge se > 0)

#### 5.8 Sub-componente EventsTab - [ ]
- [ ] ScrollArea com altura fixa
- [ ] Cards individuais para cada evento
- [ ] Badge de tipo (Warning: destructive, Normal: secondary)
- [ ] Exibir: reason, message, source, count, timestamps

#### 5.9 Sub-componente DescribeTab - [ ]
- [ ] ScrollArea com altura fixa
- [ ] Pre tag com fonte monospace
- [ ] Classe: text-xs bg-muted p-4 rounded font-mono whitespace-pre-wrap

---

### Fase 6: Integração no NodePoolEditor - [ ]

**Arquivo**: `internal/web/frontend/src/components/NodePoolEditor.tsx`

#### 6.1 Imports - [ ]
- [ ] Importar `NodeDetailsTab`
- [ ] Importar `Tabs, TabsContent, TabsList, TabsTrigger` do shadcn/ui
- [ ] Importar ícone `Server` do lucide-react

#### 6.2 Estrutura com Tabs - [ ]
- [ ] Envolver conteúdo atual em Tabs
- [ ] TabsList com 2 triggers:
  - Configuration (aba atual)
  - View Nodes (nova aba, disabled se !nodePool)
- [ ] TabsContent "editor": manter conteúdo atual
- [ ] TabsContent "nodes": adicionar NodeDetailsTab

#### 6.3 Props do NodeDetailsTab - [ ]
- [ ] Passar `cluster={nodePool.cluster_name}`
- [ ] Passar `nodePoolName={nodePool.name}`
- [ ] Condicional: só renderizar se nodePool existe

---

### Fase 7: Queries Prometheus (Opcional - Fallback) - [ ]

**Arquivo**: `internal/monitoring/prometheus/queries.go`

#### 7.1 Node CPU Usage Query - [ ]
- [ ] Nome: "node_cpu_usage"
- [ ] Query: rate(node_cpu_seconds_total[5m]) com mode="idle"
- [ ] Variável: node_name

#### 7.2 Node Memory Usage Query - [ ]
- [ ] Nome: "node_memory_usage"
- [ ] Query: (1 - (MemAvailable / MemTotal)) * 100
- [ ] Variável: node_name

#### 7.3 Node Disk Usage Query - [ ]
- [ ] Nome: "node_disk_usage"
- [ ] Query: (1 - (avail_bytes / size_bytes)) * 100
- [ ] Variável: node_name
- [ ] Mountpoint: "/"

#### 7.4 Integração em enrichNodeWithPrometheusMetrics - [ ]
- [ ] Executar queries do Prometheus
- [ ] Parsear respostas
- [ ] Preencher NodeInfo com métricas
- [ ] Tratamento de erros (não falhar se indisponível)

---

## Dependências

### Backend (Go)
- k8s.io/client-go (já instalado)
- k8s.io/metrics (já instalado)
- k8s.io/apimachinery (já instalado)

### Frontend (React/TypeScript)
- @tanstack/react-query (já instalado)
- shadcn/ui components:
  - Table, TableBody, TableCell, TableHead, TableHeader, TableRow
  - Card, CardContent, CardHeader, CardTitle
  - Badge
  - Button
  - Progress
  - Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription
  - Tabs, TabsContent, TabsList, TabsTrigger
  - ScrollArea
- lucide-react icons:
  - Server, Activity, Box, AlertCircle, FileText, Tag, Info, RefreshCcw

---

## Endpoints API REST

### GET /api/v1/nodes/:cluster/:nodepool
**Descrição**: Lista nodes de um node pool com métricas

**Parâmetros**:
- cluster (path): Nome do cluster
- nodepool (path): Nome do node pool

**Resposta**:
```json
{
  "success": true,
  "data": {
    "nodes": [...],
    "count": 3,
    "node_pool_name": "default",
    "cluster": "cluster-prod"
  }
}
```

### GET /api/v1/nodes/:cluster/:nodepool/:node
**Descrição**: Detalhes completos de um node específico

**Parâmetros**:
- cluster (path): Nome do cluster
- nodepool (path): Nome do node pool
- node (path): Nome do node

**Resposta**:
```json
{
  "success": true,
  "data": {
    "node": {...},
    "pods": [...],
    "events": [...],
    "kubectl_describe": "..."
  }
}
```

---

## Estratégia de Testes

### Backend Tests

**Arquivo**: `internal/kubernetes/client_test.go`

- [ ] TestGetNodesWithMetrics_Success
- [ ] TestGetNodesWithMetrics_EmptyNodePool
- [ ] TestGetNodesWithMetrics_APIError
- [ ] TestGetNodesWithMetrics_MetricsUnavailable
- [ ] TestGetNodeDetails_Success
- [ ] TestGetNodeDetails_NotFound
- [ ] TestGetNodeDetails_WithoutPods
- [ ] TestGetNodeDetails_WithoutEvents

### Frontend Tests

**Arquivo**: `NodeDetailsTab.test.tsx`

- [ ] Renderiza tabela de nodes corretamente
- [ ] Exibe estado de loading
- [ ] Trata node pool vazio
- [ ] Abre modal ao clicar em Details

**Arquivo**: `NodeDetailsModal.test.tsx`

- [ ] Exibe overview corretamente
- [ ] Exibe métricas com gráficos
- [ ] Lista pods do node
- [ ] Exibe eventos recentes
- [ ] Renderiza kubectl describe

---

## Performance e Otimizações

### Caching
- React Query: 30s staleTime para lista, 20s para detalhes
- Auto-refresh: 60s para lista, manual para detalhes
- Cache automático de requests idênticos

### Paralelização
- Busca de múltiplos nodes em paralelo (goroutines)
- Sync.WaitGroup para aguardar todas as goroutines
- Mutex para acesso thread-safe ao slice de resultados

### Graceful Degradation
- Métricas indisponíveis: não falhar, apenas não exibir
- Metrics Server down: fallback para Prometheus
- Prometheus down: exibir apenas dados da API Kubernetes
- kubectl describe error: não bloquear outras informações

---

## Segurança

- Read-only operations (nenhuma modificação de nodes)
- Mesma autenticação via kubeconfig context
- Mesma política RBAC do resto da aplicação
- Nenhum dado sensível exposto (apenas informações públicas do K8s)

---

## Critérios de Aceite

### Funcionalidade
- [ ] Lista de nodes exibe todos os nodes do node pool
- [ ] Métricas de uso (CPU, Memory, Disk) exibidas com percentuais
- [ ] Status do node exibido corretamente (Ready, NotReady, Cordoned)
- [ ] Modal de detalhes exibe informações completas
- [ ] Todas as 5 abas do modal funcionam corretamente
- [ ] Auto-refresh funciona (lista atualiza a cada 60s)
- [ ] Botão Refresh manual funciona
- [ ] Integração com NodePoolEditor funcional

### UX/UI
- [ ] Interface responsiva e performática
- [ ] Loading states claros
- [ ] Empty states informativos
- [ ] Error handling apropriado com mensagens claras
- [ ] Progress bars coloridas (verde/amarelo/vermelho)
- [ ] Badges coloridos para status
- [ ] Modal com scrolling apropriado
- [ ] Tabelas com sorting (se aplicável)

### Performance
- [ ] Lista de nodes carrega em < 2 segundos (para até 10 nodes)
- [ ] Detalhes de node carregam em < 1 segundo
- [ ] Sem vazamentos de memória (verificar com profiler)
- [ ] Sem race conditions (go test -race)

### Testes
- [ ] Todos os testes backend passam
- [ ] Todos os testes frontend passam
- [ ] Coverage >= 80% para código novo
- [ ] Testes de integração passam

---

## Notas de Implementação

### Ordem Estrita de Implementação
1. Fase 1 (Types) ANTES de Fase 2 (Backend)
2. Fase 2 e 3 (Backend completo) ANTES de Fase 4 e 5 (Frontend)
3. Fase 6 (Integração) após Fase 5 estar completa
4. Fase 7 (Prometheus) é OPCIONAL e pode ser feita por último

### Pontos de Atenção
- Sempre verificar se Metrics Server está disponível antes de tentar Prometheus
- Não falhar completamente se métricas não disponíveis
- Formatar bytes em unidades legíveis (Gi, Mi, Ki) não bytes raw
- Age em formato compacto (2d5h, 30m, 45s) não timestamps
- kubectl describe pode falhar - tratar gracefully
- Eventos podem estar vazios - não é erro

### Reutilização de Código
- Função `GetNodesInNodePool()` já existe - reutilizar
- Padrões de handlers HTTP já estabelecidos - seguir
- Componentes shadcn/ui já configurados - usar
- React Query patterns já implementados - manter consistência
- Formatação de recursos já existe em outros lugares - reutilizar

---

## Histórico de Mudanças

### 11/02/2026 - Criação do Plano
- Plano inicial completo com 7 fases
- Estrutura de dados definida
- Endpoints API especificados
- Componentes React planejados
- Estratégia de testes definida

---

## Próximos Passos

1. Implementar Fase 1 (Estrutura de Dados)
2. Implementar Fase 2 (Backend Client Methods)
3. Implementar Fase 3 (REST API Handlers)
4. Implementar Fase 4 (React Hooks)
5. Implementar Fase 5 (Componentes React)
6. Implementar Fase 6 (Integração)
7. Implementar Fase 7 (Prometheus Queries) - OPCIONAL

---

## Referências

- Código existente: `internal/kubernetes/client.go:GetNodesInNodePool()`
- Handlers: `internal/web/handlers/nodepools.go`
- Frontend: `internal/web/frontend/src/components/NodePoolTab.tsx`
- API Types: `internal/web/frontend/src/lib/api/types.ts`
- Padrões de componentes: ConfigMapsTab, DeploymentsTab, PodsPanel
