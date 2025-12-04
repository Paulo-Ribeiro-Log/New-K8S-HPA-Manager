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

### 📚 Histórico e Referências
11. [📜 Histórico de Correções](docs/history/CHANGELOG.md) - Correções e refatorações principais
12. [🔍 Análise Cordon/Drain](ANALISE_NODEPOOL_CORDON_DRAIN.md) - Análise detalhada do sistema

### 🚀 Otimizações
13. [⚡ Autodiscover Optimization](docs/optimization/AUTODISCOVER_OPTIMIZATION.md) - Paralelização da busca de subscriptions

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

---

## 🎯 Para Novos Chats do Claude

**Context Template Rápido:**
```
Projeto: Kubernetes HPA + Azure AKS Node Pool Manager

Repositório: git@github.com:Paulo-Ribeiro-Log/New-K8S-HPA-Manager.git
Versão Atual: v1.3.1 (oficial - 2025-12-03)
Tech: Go 1.24+ + React 18.3 (Web)
Build: make build && make web-build
Binary: ./build/new-k8s-hpa

Recent Updates (v1.3.1):
- UI/UX: Gráfico de memória corrigido (azul vs roxo), labels recolhidos por padrão em ConfigMaps/Secrets/Deployments
- ConfigMaps/Secrets/Deployments: Campo "Versão" exibe app.kubernetes.io/version quando disponível
- Node Pools: Botão de refresh no painel "Available Node Pools" busca dados atualizados do Azure AKS
  - Fix: Correção de erros TypeScript em Index.tsx (sequencedNodePools.find com sequence_order)
- VM Disk Specs: Exibe performance de disco (Temp Disk, Max Disks, IOPS, Throughput) no Node Pool Editor
  - Interface VMSpec estendida com tempDiskGiB, maxDataDisks, maxIOPS, maxThroughputMBps
  - Função formatDiskSpecs() formata informações com emojis e unidades legíveis
  - Specs adicionadas para séries Dsv3, Dsv5, Esv3 (VMs mais comuns)
- Card de Cluster Contextual: Dashboard mostra total de clusters, outras abas mostram contexto + versão K8s
  - Componente ClusterContextCard mantém estrutura visual dos StatsCards
  - Hook useClusterInfo busca informações via API /clusters/info
  - Truncate com tooltip para nomes longos, refetch automático a cada 60s
- Comparação Histórica D-1/D-2/D-3: Linha lilás nos gráficos permite comparar métricas atuais com 1, 2 ou 3 dias atrás
  - Backend: Parâmetro days_offset (1-3) na API /monitoring/v2/metrics com validação
  - Frontend: Select com labels descritivas, refetch automático ao trocar período
  - UX: Nome da linha dinâmico (ex: "CPU D-2 (48h atrás)"), opacidade sutil (0.2-0.3)
- Gráfico de Réplicas Corrigido: Backend retorna `replicas_current` corretamente (monitoring_v2.go:452)
- Sistema de Alertas Completo: Exibe TODOS os alertas ativos (removido filtro restritivo de nomes)
- Alertas Filtrados por Tempo: Botão de alertas respeita seletor de tempo no painel "Análise de Métricas"
- Navegação Bidirecional: HPAs ↔ Monitoramento com 1 clique (⚙️ monitora HPA, "Edit HPA" edita)
- Prometheus URL Fix: Corrigido erro HTTP 500 em endpoints de alertas (remoção de sufixo -admin)
- Métricas Prometheus CORRIGIDAS: CPU/Memória histórica agora usam avg() ao invés de sum() (0-100%)
- AlertsDialog Aprimorado: Card de contexto destacado com extração inteligente de HPA/Pod/Container
- VM Specs: Exibe vCPUs e memória no Node Pool Editor (150+ Azure VMs catalogadas)
- Notificações In-App Clicáveis: Sistema web com navegação contextual para AlertsDialog
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

**Happy coding!** 🚀
