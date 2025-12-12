# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Versão atual oficial: **v1.3.1** (GitHub release). Tags locais v1.3.2+ são do projeto antigo e devem ser ignoradas.

---

## 📑 Índice / Table of Contents

### 📘 Guias Essenciais
1. [🚀 Quick Start](docs/guides/QUICK_START.md) - Estado atual do projeto, features e tech stack
2. [🔧 Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md) - Comandos de desenvolvimento, build e deploy
3. [🏗️ Architecture Overview](docs/architecture/OVERVIEW.md) - Estrutura de diretórios e componentes
4. [🌐 Interface Web](docs/guides/WEB_INTERFACE.md) - React/TypeScript, features e workflows
5. [⚠️ Common Pitfalls](docs/guides/COMMON_PITFALLS.md) - Erros comuns e soluções
6. [🧪 Testing Strategy](docs/guides/TESTING.md) - Estratégias de teste e validação
7. [🔧 Troubleshooting](docs/guides/TROUBLESHOOTING.md) - Resolução de problemas comuns
8. [🚀 Continuing Development](docs/guides/CONTINUING_DEV.md) - Context templates e best practices
9. [⚡ Async Optimization Plan](docs/guides/ASYNC_OPTIMIZATION_PLAN.md) - Plano de otimização assíncrona da auto descoberta
10. [📦 Installation Scripts](docs/guides/INSTALLATION_SCRIPTS.md) - Scripts de instalação (release vs main)
11. [🔔 Windows Notifications](docs/guides/WINDOWS_NOTIFICATIONS.md) - Sistema de notificações via PowerShell/WSL2
12. [🔒 RBAC Azure AD](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md) - Controle de acesso baseado em grupos do Azure AD

### 📚 Histórico e Referências
11. [📜 Histórico de Correções](docs/history/CHANGELOG.md) - Correções e refatorações principais
12. [🔍 Análise Cordon/Drain](ANALISE_NODEPOOL_CORDON_DRAIN.md) - Análise detalhada do sistema

### 🚀 Otimizações
13. [⚡ Autodiscover Optimization](docs/optimization/AUTODISCOVER_OPTIMIZATION.md) - Paralelização da busca de subscriptions

### 📋 Roadmaps e Estudos
14. [🔐 Estudo SSO Azure AD](ESTUDO_SSO_AZURE_AD.md) - Análise técnica de SSO OAuth 2.0/OIDC
15. [🛣️ Roteiro Implementação SSO](ROTEIRO_IMPLEMENTACAO_SSO.md) - **Roadmap completo para servidor centralizado** (6 dias)
16. [📧 Implementação user_email History](IMPLEMENTACAO_USER_EMAIL_HISTORY.md) - User tracking no History Tracker (v1.3.6)

---

## 📌 Quick Reference

### Comandos Mais Usados

```bash
# Build e Run
make build                    # Compilar backend Go (detecta versão via git tags)
make web-build                # Build frontend (npm run build + copy para static/)
./build/new-k8s-hpa web       # Iniciar servidor web (porta 8080)

# Development
make run-dev                  # TUI com debug (go run . --debug)
make web-dev                  # Frontend dev server (Vite HMR - porta 5173)
make build-web                # Build completo (frontend + backend)
./rebuild-web.sh -b           # ⭐ RECOMENDADO - Evita cache issues

# Testing
make test                     # Rodar todos os testes
make test-coverage            # Testes com coverage (gera coverage.html)
go test -v ./internal/config -race  # Testar race conditions específicas
go test -run TestGetClient    # Rodar teste específico

# Debug Web
tail -f /tmp/k8s-hpa-manager-web-*.log  # Logs do servidor (background mode)
./build/new-k8s-hpa web -f    # Foreground mode (logs no terminal)
./build/new-k8s-hpa web --ad  # ⚠️  EMERGÊNCIA: Bypass RBAC (flag oculta, sem documentação)

# Installation (Release - Estável)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash

# Installation (Main - Desenvolvimento)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash

# Updates
new-k8s-hpa version           # Verificar versão e updates disponíveis
~/.k8s-hpa-manager/scripts/auto-update.sh  # Auto-update interativo

# Release
make release                  # Build multi-plataforma (Linux, macOS Intel/ARM)
./create-v1-release.sh        # Criar release no GitHub com binários
```

### Estrutura do Projeto

```
k8s-hpa-manager/
├── cmd/                      # CLI commands (Cobra)
├── internal/
│   ├── tui/                  # Terminal UI (Bubble Tea)
│   ├── web/                  # Web Interface (React/TS)
│   │   ├── frontend/         # React SPA
│   │   ├── handlers/         # Go REST API
│   │   └── sse/              # Server-Sent Events
│   ├── kubernetes/           # K8s client wrapper
│   ├── azure/                # Azure SDK auth
│   └── models/               # Data structures
├── docs/                     # Documentação modular
└── build/                    # Build artifacts
```

### Tech Stack (Quick Reference)

| Categoria | Tecnologia |
|-----------|------------|
| **Backend** | Go 1.24+, client-go v0.34, Azure SDK |
| **Frontend** | React 18.3, TypeScript 5.8, Vite 5.4 |
| **UI** | shadcn/ui, Tailwind CSS 3.4 |
| **Arquitetura** | MVC, SSE (Server-Sent Events) |

### Features Principais

✅ **TUI Completo** - Interface terminal responsiva com progress bars
✅ **Web Interface** - React/TypeScript com 99% das features
✅ **HPAs & Node Pools** - CRUD completo com staging area
✅ **Cordon/Drain** - Sistema de evacuação com progress em tempo real (SSE)
✅ **Audit Log** - Rastreabilidade completa de operações Cordon/Drain e Rollouts com histórico persistente
✅ **CronJob Editor Aprimorado** - Interface visual com parser de cron, descrições legíveis e validação em tempo real
✅ **Sessions** - Save/load/edit com compatibilidade TUI ↔ Web
✅ **Monitoring V2** - Sistema sem port-forwards, acesso direto Prometheus
✅ **Auto-Updates** - Sistema automático de detecção e instalação
✅ **Autodiscover Otimizado** - Busca paralela de subscriptions (10x mais rápido)
✅ **Navegação Inteligente** - Navegação bidirecional: HPAs ↔ Monitoramento com 1 clique
✅ **Métricas Corrigidas** - CPU/Memória agora calculam média correta (0-100%) com avg() ao invés de sum()
✅ **VM Specs Display** - Exibe vCPUs e memória de cada VM Size no Node Pool Editor
✅ **Notificações In-App Clicáveis** - Sistema de notificações web com navegação contextual para AlertsDialog
✅ **AlertsDialog Aprimorado** - Card de contexto destacado com extração inteligente de HPA/Pod/Container/Deployment
✅ **Sistema de Alertas Completo** - Exibe TODOS os alertas ativos (sem filtros restritivos) com filtro por período de tempo
✅ **Alertas Filtrados por Tempo** - Botão de alertas no painel "Análise de Métricas" respeita seletor de tempo (5min-24h)
✅ **Gráfico de Réplicas Corrigido** - Backend agora retorna `replicas_current` corretamente (era `replicas`)
✅ **Comparação Histórica D-1/D-2/D-3** - Linha lilás nos gráficos permite comparar métricas atuais com 1, 2 ou 3 dias atrás
  - Select dropdown nos modais expandidos para escolher período de comparação
  - Backend: parâmetro `days_offset` (1-3) com validação e cálculo dinâmico de offset
  - Frontend: refetch automático ao trocar período, labels descritivas nos selects
  - Linha com opacidade 0.2-0.3 e nome dinâmico (ex: "CPU D-2 (48h atrás)")
✅ **Card de Cluster Contextual** - Card de estatísticas adapta-se ao contexto da aba
  - Dashboard: exibe total de clusters disponíveis no kubeconfig
  - Outras abas: mostra contexto selecionado + versão do Kubernetes
  - Usa truncate com tooltip para nomes longos, mantém consistência visual
✅ **UI/UX Improvements (v1.3.1)** - Melhorias na interface e usabilidade
  - Gráfico de Memória: Linha corrente agora usa cor azul (#3b82f6) ao invés de roxo, evitando confusão com linha D-1
  - ConfigMaps/Secrets/Deployments: Labels iniciam recolhidos, campo "Versão" exibido quando disponível (app.kubernetes.io/version)
  - Node Pools: Botão de refresh adicionado no painel "Available Node Pools" para atualizar dados do Azure AKS
  - VM Disk Specs: Exibe informações de performance de disco (Temp Disk, Max Disks, IOPS, Throughput) no Node Pool Editor
✅ **Dashboard por Namespace (v1.3.2)** - Visibilidade granular de consumo de recursos
  - Card "Top 5 Namespaces por Consumo" no Dashboard principal
  - Grid 3 colunas: CPU Usage | Memory Usage | Top 5 Namespaces (lado a lado)
  - Tabs interativas: CPU (millicores), Memory (GB), Pods (count)
  - Barras de progresso coloridas: verde (0-50%), amarelo (50-75%), vermelho (75-100%)
  - Seção "Outros" com soma de namespaces fora do Top 5
  - Auto-refresh a cada 60 segundos + botão manual de refresh
  - Backend: Queries Prometheus agregadas por namespace (`sum by (namespace)`)
  - Endpoint: `GET /api/v1/namespaces/:cluster/metrics?limit=5`
  - Graceful degradation: estado de erro elegante quando Prometheus inacessível
✅ **Aba Namespaces (v1.3.4)** - Gerenciamento completo de Namespaces Kubernetes
  - **Overview Top 5**: Quando nenhum namespace selecionado, exibe 3 gráficos de pizza lado a lado
    - Top 5 Namespaces por CPU (millicores)
    - Top 5 Namespaces por Memória (GB)
    - Top 5 Namespaces por Pods (count)
    - Seção "Outros" agregando namespaces fora do Top 5
  - **Detalhes do Namespace**: Ao selecionar namespace, exibe painel com:
    - Metadados: Nome, Cluster, Status (Phase do namespace), Age (formato compacto: 2d5h, 30m, 45s)
    - Monaco YAML Editor (tema VS Code) com **edição completa e persistente**
    - **Sistema de edição avançado** (copiado de ConfigMaps):
      - ✅ Undo/Redo com histórico de 50 versões (cache persistente por namespace)
      - ✅ View Mode toggle: Editor | Diff (side-by-side)
      - ✅ Validação Dry-run (testa YAML antes de aplicar)
      - ✅ Visualizar diff com diff2html (modal compacto + fullscreen)
      - ✅ Apply com confirmação (exibe mudanças detectadas)
      - ✅ Cancelar (restaura YAML original)
      - ✅ Botões duplicados em: painel normal + modal fullscreen
    - Botões de ação: Describe, Visualizar diff, Expandir Editor, Dry-run, Cancelar, Aplicar
    - Dropdown menu (⋮) com opção Delete (cor vermelha)
    - Modal fullscreen do editor com toolbar completa de edição
  - **Criar Namespace**: Botão no header do painel "Visualização" (à direita)
    - Modal com campo de texto para nome do namespace
    - Validação: nome obrigatório, cluster selecionado
    - Auto-refresh da lista após criação bem-sucedida
  - **Backend completo**:
    - `GET /namespaces/:cluster/:name` - Obter manifesto YAML
    - `GET /namespaces/:cluster/:name/describe` - kubectl describe
    - `POST /namespaces/:cluster` - Criar namespace
    - `PUT /namespaces/:cluster/:name` - **Aplicar YAML editado** (com suporte a dry-run)
    - `DELETE /namespaces/:cluster/:name` - Deletar namespace
    - `GET /namespaces/:cluster/metrics?limit=5` - Top 5 métricas
  - **Componente**: `NamespacesTab.tsx` (1240+ linhas, dual-mode: overview vs details)
  - **Correções**: Hook `useNamespaces` agora retorna `refetch` corretamente conectado ao botão Atualizar
  - **Imports**: diff (createTwoFilesPatch), diff2html (html), js-yaml (load), ícones Undo2/Redo2/CheckCircle2/TriangleAlert/FileDiff
✅ **Aba Pods (v1.3.3)** - Gerenciamento completo de Pods Kubernetes
  - Listagem de pods com filtros por namespace, search, system pods
  - Status visual com badges coloridos (Running/Pending/Failed/CrashLoopBackOff)
  - Indicadores de restart count com alertas visuais (>3 restarts)
  - Informações de containers: estado, imagem, restarts, ready status
  - Labels colapsáveis por pod
  - Modal de detalhes com 2 tabs: YAML Manifest + Logs
  - Logs de containers individuais (tail 500 linhas) com **syntax highlighting**
  - Botão "Delete Pod" com confirmação
  - Auto-refresh a cada 30 segundos
  - Backend: Endpoints já existiam em `internal/web/handlers/pods.go`
  - API: `GET /pods?cluster=...&namespaces=...`, `GET /pods/:cluster/:namespace/:name`, `DELETE /pods/:cluster/:namespace/:name`, `GET /pods/:cluster/:namespace/:name/logs`
  - Componente: `PodsPanel.tsx` (estilo ConfigMaps/Secrets/Deployments)
✅ **Aba Containers (v1.3.3)** - Visualização dedicada de containers
  - Painel esquerdo: Lista de pods + containers (tree view)
  - Painel direito: Logs + Detalhes do container selecionado
  - Botão "Labels" movido para painel direito (Container Logs & Details)
  - Auto-refresh de logs a cada 3 segundos (opcional)
  - Download de logs como arquivo .txt
  - Componente: `ContainersTab.tsx`
✅ **UX Improvements - Pods/Containers (v1.3.3)**
  - **Métricas inline**: Layout horizontal "RESTARTS: 3  CPU/R: 800m  CPU/L: 1" (ao invés de grid vertical)
  - **Badge de versão**: Extrai versão da imagem do container (ex: nginx:1.21.0 → "1.21.0")
  - **Log highlighting com estilos inline**: Colorização automática de logs
    - ❌ ERROR/FATAL/EXCEPTION: Vermelho (#f87171) com fundo e borda vermelha
    - ⚠️ WARN/WARNING: Amarelo/Laranja (#fbbf24) com fundo e borda
    - ℹ️ INFO: Azul claro (#60a5fa)
    - 🔍 DEBUG/TRACE: Roxo (#c084fc) com fonte menor
    - ✅ SUCCESS/COMPLETED: Verde (#4ade80)
    - 🔴 HTTP 4xx/5xx: Laranja (#fb923c) com fundo
    - HTTP 2xx/3xx: Verde claro (#86efac)
  - **YAML Fullscreen Modal**: Botão "Expandir" no painel YAML (estilo ConfigMaps) com Monaco Editor
  - **kubectl describe**: Botão "Describe" em todas as abas (Pods, ConfigMaps, Secrets, Deployments)
    - Backend: Executa `kubectl describe {resourceType} {name}` via `internal/kubernetes/client.go:ExecuteKubectlDescribe()`
    - API: `GET /api/v1/{resource}/:cluster/:namespace/:name/describe`
    - Handlers: `configmaps.go`, `secrets.go`, `deployments.go`, `pods.go`
    - Frontend: Modal com ScrollArea exibindo output completo do kubectl describe
✅ **RBAC com Azure AD (v1.3.5+)** - Controle de acesso baseado em grupos do Azure AD
  - **Grupo SRE**: `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
  - **Backend**: Módulo `internal/rbac/azure_ad.go` com verificação via Azure CLI (`az ad user get-member-groups`)
  - **Middleware**: `internal/web/middleware/rbac.go` protege rotas sensíveis (POST, PUT, DELETE)
  - **Frontend**: Hook `useUserPermissions()` + componente `<ProtectedAction>` para ocultar/desabilitar botões
  - **Cache**: TTL de 1 hora para permissões, invalidação manual via endpoint `/permissions/refresh`
  - **Recursos Protegidos**: Apply (HPAs, Node Pools, ConfigMaps, Namespaces), Delete (todos os recursos), Cordon/Drain
  - **Recursos Públicos**: Visualização (GET), Staging area, Sessions, Monitoramento, Logs
  - **Badge de Status**: Componente `<SREBadge>` exibe status SRE no header com popover de grupos
  - **Testes**: Suite completa (`./testes/test-rbac.sh`) + testes Go unitários (`go test ./internal/rbac`)
  - **Documentação**: [RBAC_AZURE_AD_IMPLEMENTATION.md](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md) + [RBAC_SUMMARY.md](docs/guides/RBAC_SUMMARY.md)

---

## 🎯 Para Novos Chats do Claude

**Context Template Rápido:**
```
Projeto: Kubernetes HPA + Azure AKS Node Pool Manager

Repositório: git@github.com:Paulo-Ribeiro-Log/New-K8S-HPA-Manager.git
Versão Atual: v1.3.5+ (em desenvolvimento)
Tech: Go 1.24+ + React 18.3 (Web)
Build: make build && make web-build
Binary: ./build/new-k8s-hpa

Recent Updates (v1.3.5):
- **RBAC com Azure AD**: Sistema completo de controle de acesso
  - Frontend: 10 componentes protegidos (44 botões com `<ProtectedAction>`)
  - Backend: Middleware RBAC protegendo 20+ rotas (POST/PUT/DELETE)
  - Hook `useUserPermissions()` com cache de 1 hora
  - Flag oculta `--ad` para bypass em emergências (sem documentação)
  - Badge SRE no header, teste completo em `./testes/test-rbac.sh`
  - Módulo: `internal/rbac/azure_ad.go`, `internal/web/middleware/rbac.go`
  - Grupo: VV_CLOUD_SRE (eb865ea5-2672-49be-abc8-74c248c556b0)

Recent Updates (v1.3.4):
- Aba Namespaces: Gerenciamento completo com overview Top 5 (CPU/Memory/Pods) + **Sistema de edição avançado**
  - Dual-mode: Overview (charts) quando nenhum namespace selecionado, detalhes com Monaco Editor quando selecionado
  - CRUD completo: Criar, Visualizar, Editar YAML (com undo/redo/diff/dry-run/apply), Deletar namespaces
  - **Edição avançada** (copiado de ConfigMaps): Undo/Redo (50 versões), Diff side-by-side, Dry-run, Apply com confirmação
  - Botões duplicados em painel normal e modal fullscreen
  - Botão "Criar Namespace" no header do painel "Visualização"
  - kubectl describe integrado, modal fullscreen para edição com toolbar completa
  - **Lista de Deployments**: Botão colapsável (estilo Labels do ConfigMaps) exibindo deployments do namespace
    - Mostra ícone Package + nome + réplicas (ready/total)
    - Carregamento automático ao selecionar namespace
    - Endpoint: `GET /api/v1/deployments?cluster=...&namespaces=...`
    - Estado inicial: recolhido, click para expandir/colapsar
  - Backend: endpoints POST/GET/**PUT**/DELETE em `/namespaces/:cluster`
  - Handler Apply: `internal/web/handlers/namespaces.go:Apply()` com suporte a dry-run
  - Kubernetes client: `internal/kubernetes/client.go:ApplyNamespace()` reutilizado
  - API client: `internal/web/frontend/src/lib/api/client.ts:applyNamespace()` adicionado
  - Component: `NamespacesTab.tsx` (1250+ linhas com toda lógica de edição + deployments)

Recent Updates (v1.3.3):
- Aba Pods/Containers: Gerenciamento completo de Pods e Containers Kubernetes
  - Listagem com filtros (namespace, search, system pods)
  - Métricas inline, badge de versão extraído da imagem
  - Log highlighting com cores (ERROR vermelho, WARN amarelo, INFO azul, etc)
  - YAML fullscreen modal + kubectl describe integrado
- kubectl describe: Implementado em todas as abas (Pods, ConfigMaps, Secrets, Deployments)
  - Backend: `ExecuteKubectlDescribe()` em `internal/kubernetes/client.go`
  - API: `GET /api/v1/{resource}/:cluster/:namespace/:name/describe`

- Dashboard por Namespace (v1.3.2): Card "Top 5 Namespaces por Consumo"
  - Grid 3 colunas: CPU | Memory | Top 5 NS
  - Tabs: CPU (millicores), Memory (GB), Pods (count)
  - Prometheus agregado: sum by (namespace)
```

**Ver documentação completa:**
- [Quick Start](docs/guides/QUICK_START.md) - Estado atual e features
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md) - Comandos essenciais
- [Architecture](docs/architecture/OVERVIEW.md) - Estrutura técnica

---

## 🏗️ Conceitos de Arquitetura Críticos

### Padrão de Concorrência (Thread-Safety)
**SEMPRE usar sync.RWMutex para acesso a recursos compartilhados:**

```go
// internal/config/kubeconfig.go - Cliente K8s por cluster
var (
    clientCache  = make(map[string]*kubernetes.Clientset)
    clientMutex  sync.RWMutex  // CRÍTICO: protege criação de clients
)

// Padrão: Double-check locking
clientMutex.RLock()
if client, exists := clientCache[cluster]; exists {
    clientMutex.RUnlock()
    return client, nil
}
clientMutex.RUnlock()

// Upgrade para write lock apenas quando necessário
clientMutex.Lock()
defer clientMutex.Unlock()
// ... criação do client
```

**Bubble Tea - NUNCA usar goroutines diretas:**
```go
// ❌ ERRADO - Race condition
go func() {
    result := applyHPA()
}()

// ✅ CORRETO - Retornar tea.Cmd
return func() tea.Msg {
    err := applyHPA()
    return HPAAppliedMsg{err: err}
}
```

### Sistema de Estado (AppModel)
**`internal/models/types.go` é a ÚNICA fonte de verdade:**

```go
type AppModel struct {
    State              AppState           // Máquina de estados
    Clusters           []string           // Clusters disponíveis
    SelectedCluster    int                // Índice do cluster selecionado
    Namespaces         []string           // Namespaces do cluster
    SelectedNamespaces map[string]bool    // Multi-seleção
    HPAs               []HPAInfo          // HPAs carregados
    NodePools          []NodePoolInfo     // Node pools do cluster
    StagingChanges     []interface{}      // Staging area (pré-apply)
    // ... 40+ campos para estado completo da aplicação
}

// Transições de estado válidas
type AppState int
const (
    StateClusterSelection    // Seleção de cluster
    StateSessionSelection    // Load/Save sessão
    StateNamespaceSelection  // Seleção de namespaces
    StateResourceSelection   // HPAs/NodePools
    StateEditing            // Edição de recursos
    StateHelp               // Tela de ajuda
)
```

**NUNCA criar estado local em handlers ou views** - sempre modificar AppModel e retornar mensagem.

### Sistema de Sessões (Compatibilidade TUI ↔ Web)
**Formato JSON compartilhado** (`internal/session/manager.go`):

```json
{
  "id": "uuid-v4",
  "name": "session-name",
  "folder": "HPA-Upscale|HPA-Downscale|Node-Upscale|Node-Downscale|Mixed",
  "created_at": "2025-11-26T10:00:00Z",
  "clusters_affected": ["cluster1", "cluster2"],  // Auto-calculado
  "namespaces_affected": ["ns1", "ns2"],         // Auto-calculado
  "hpas": [...],
  "node_pools": [...]
}
```

**Diretórios organizados por tipo:**
```
~/.k8s-hpa-manager/sessions/
├── HPA-Upscale/
├── HPA-Downscale/
├── Node-Upscale/
├── Node-Downscale/
└── Mixed/
```

### Sistema SSE (Server-Sent Events)
**Progress tracking em tempo real** (`internal/web/sse/progress.go`):

```go
// Broker gerencia múltiplos clients conectados
type Broker struct {
    clients    map[chan string]bool
    newClients chan chan string
    defunctClients chan chan string
    messages   chan string
}

// Uso: Cordon/Drain com updates em tempo real
func (h *Handler) CordonDrain(c *gin.Context) {
    // 1. Criar canal SSE
    messageChan := make(chan string)
    h.sseBroker.NewClients <- messageChan

    // 2. Executar operação em goroutine
    go func() {
        for _, node := range nodes {
            h.sseBroker.Publish(fmt.Sprintf("Cordoning %s...", node))
            // ... operação
            h.sseBroker.Publish(fmt.Sprintf("✅ Done: %s", node))
        }
    }()

    // 3. Stream events para client
    c.Stream(func(w io.Writer) bool {
        if msg, ok := <-messageChan; ok {
            c.SSEvent("progress", msg)
            return true
        }
        return false
    })
}
```

### Staging Area (Pré-visualização de mudanças)
**React Context + Go Backend:**

```typescript
// internal/web/frontend/src/contexts/StagingContext.tsx
interface StagingContextType {
  stagedHPAs: HPAWithChanges[]
  stagedNodePools: NodePoolWithChanges[]
  addHPAToStaging: (hpa: HPAInfo, changes: HPAChanges) => void
  removeHPAFromStaging: (id: string) => void
  applyAll: () => Promise<void>  // Rollout com SSE progress
}

// Fluxo: Edit → Stage → Preview → Apply All
// 1. Usuário edita HPA (modal inline)
// 2. addHPAToStaging() adiciona à staging area
// 3. ApplyAllModal mostra diff antes/depois
// 4. applyAll() executa rollout com progress tracking
```

---

## ⚙️ Peculiaridades Técnicas Importantes

### Azure CLI - Ordem de Operações para Node Pools

**CRÍTICO**: Azure CLI rejeita `az aks nodepool scale` se autoscaling estiver habilitado.

**Ordem correta para 4 cenários** (`internal/tui/app.go:buildNodePoolCommands()`):

```go
// Cenário 1: Apenas MinCount/MaxCount (autoscaling já habilitado)
// 1. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y

// Cenário 2: Apenas Node Count (desabilitar autoscaling)
// 1. az aks nodepool update --disable-cluster-autoscaler
// 2. az aks nodepool scale --node-count N

// Cenário 3: MinCount/MaxCount + Node Count
// 1. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y
// 2. az aks nodepool update --disable-cluster-autoscaler
// 3. az aks nodepool scale --node-count N
// 4. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y

// Cenário 4: Apenas habilitar autoscaling (sem mudanças)
// 1. az aks nodepool update --enable-cluster-autoscaler --min-count X --max-count Y
```

### Validação VPN e Conectividade K8s

**Validação on-demand** - não assume que VPN está conectada:

```go
// internal/config/kubeconfig.go
func ValidateClusterConnection(cluster string) error {
    // Timeout de 5 segundos para evitar travamento
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Usa kubectl cluster-info (mais confiável que ping)
    cmd := exec.CommandContext(ctx, "kubectl", "cluster-info",
                               "--context", cluster)

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("VPN desconectada ou cluster inacessível")
    }
    return nil
}
```

**Pontos de validação:**
1. Início da aplicação (todos os clusters)
2. Ao carregar namespaces (cluster selecionado)
3. Ao carregar HPAs (cluster selecionado)
4. Timeout em operações Azure CLI (5s)

### Suffix `-admin` em Cluster Names

**Problema**: Sessions salvam sem `-admin`, mas kubeconfig contexts têm `-admin`.

**Soluções implementadas:**

```typescript
// Web: internal/web/frontend/src/contexts/StagingContext.tsx
const findClusterInConfig = (sessionCluster: string) => {
  // Remove -admin para matching
  return clusters.find(c =>
    c.name === sessionCluster ||
    c.name === `${sessionCluster}-admin`
  )
}

// Load session adiciona -admin automaticamente
const loadFromSession = (session: Session) => {
  session.hpas.forEach(hpa => {
    hpa.cluster = ensureAdminSuffix(hpa.cluster)  // akspriv-prod → akspriv-prod-admin
  })
}
```

### Web Interface - Hard Refresh Obrigatório

**Após `./rebuild-web.sh -b`, SEMPRE fazer hard refresh:**

```bash
# 1. Build
./rebuild-web.sh -b

# 2. Browser - Ctrl+Shift+R (Linux/Windows) ou Cmd+Shift+R (macOS)
# Isso limpa cache JavaScript e carrega novos assets
```

**Por que?** Vite gera nomes de arquivo com hash (ex: `index-abc123.js`), mas navegadores podem cachear `index.html` antigo.

### Bubble Tea - Texto Unicode-Safe

**SEMPRE usar `[]rune` ao invés de strings:**

```go
// ❌ ERRADO - Quebra com emojis
text := "Hello 👋"
text[0] = 'h'  // Panic ou corrupção

// ✅ CORRETO
runes := []rune("Hello 👋")
runes[0] = 'h'
text = string(runes)
```

**Cursor position também usa runes:**
```go
// internal/tui/text_input.go
type TextInput struct {
    value  []rune  // Texto como runes
    cursor int     // Posição do cursor em runes (não bytes)
}
```

---

## 📖 Navegação

Toda a documentação detalhada foi modularizada em arquivos separados na pasta `docs/`:

- **`docs/guides/`** - Guias práticos de desenvolvimento
- **`docs/architecture/`** - Documentação de arquitetura
- **`docs/history/`** - Histórico de mudanças e correções

Cada arquivo contém um link "Voltar ao CLAUDE.md principal" no topo para fácil navegação.

---

## 🔗 Links Externos

- **GitHub**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager
- **Latest Release**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/latest
- **Análise Cordon/Drain**: [ANALISE_NODEPOOL_CORDON_DRAIN.md](ANALISE_NODEPOOL_CORDON_DRAIN.md)
- **Plano Otimização Async**: [PLANO_OTIMIZACAO_ASYNC.md](PLANO_OTIMIZACAO_ASYNC.md)

---

## 🔄 Últimas Atualizações (12/12/2025)

### Service Mesh - Kiali Integration

#### Seletor Traffic Implementado
- **Traffic Accordion** com 3 protocolos:
  - **gRPC**: Requests (padrão), Received Messages, Sent Messages, Total Messages
  - **HTTP**: Requests (única opção)
  - **TCP**: Sent Bytes (padrão), Received Bytes, Total Bytes
- Accordion múltiplo (Traffic + Display podem abrir simultaneamente)
- Interface idêntica ao Kiali original

#### Sistema de Cores Dinâmico para Erros
**Problema Resolvido**: Chaves duplicadas `line-color` e `target-arrow-color` faziam apenas primeira definição (estática) ser usada.

**Solução**: Removidas definições estáticas, mantidas apenas funções dinâmicas:

```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
'line-color': function(ele: any) {
  const errorRate = parseFloat(ele.data('errorRate') || '0');
  
  // PRIORIDADE: Erros sempre aparecem primeiro
  if (errorRate >= 5) return '#ef4444';    // Vermelho (>=5%)
  if (errorRate >= 1) return '#f97316';    // Laranja (1-5%)
  if (errorRate >= 0.1) return '#eab308';  // Amarelo (0.1-1%)
  if (errorRate > 0) return '#fca5a5';     // Vermelho claro (>0%)
  
  // Sem erros: tráfego e protocolo
  if (rate > 100) return '#10b981';        // Verde (alto tráfego)
  if (protocol === 'http') return '#3b82f6'; // Azul (HTTP)
  return '#9ca3af';                        // Cinza (padrão)
}
```

#### Cálculo de Error Rate pelo Backend
**Implementação**: Backend agora calcula `errorRate` analisando `responses` do Kiali:

```go
// internal/web/handlers/servicemesh.go
// Se Kiali retorna apenas códigos de erro (4xx/5xx) com count 0.00,
// considera 100% erro (todas requisições falhando)
if errorRequests == 0 && totalRequests == 0 {
    hasErrorCodes := false
    for code := range edge.Data.Traffic.Responses {
        if len(code) > 0 && (code[0] == '4' || code[0] == '5') {
            hasErrorCodes = true
            break
        }
    }
    if hasErrorCodes {
        simpleEdge.ErrorRate = 100.0  // 🔴 Linha vermelha
    }
}
```

#### Correção do Refresh
**Problema**: Refresh removia todos elementos e recriava, causando perda de nodes.

**Solução**: Método `updateGraphDataFromResponse()` que:
- Atualiza apenas **dados** de elementos existentes
- Adiciona **novos** nodes/edges
- Remove **apenas** elementos que desapareceram
- Mantém **posições** dos nodes existentes
- Layout apenas para nodes novos

```typescript
// internal/web/frontend/src/components/ServiceMeshGraph.tsx
const loadServiceGraphSilent = async () => {
  const data = await apiClient.getServiceGraph(...)
  
  if (cyInstance.current) {
    updateGraphDataFromResponse(data);  // ✅ Não destrói gráfico
  } else {
    setGraphData(data);  // Cria novo
  }
}
```

**Labels de Erro**: Agora exibem `X.X% err` inline com métricas de tráfego.

---

**Happy coding!** 🚀
