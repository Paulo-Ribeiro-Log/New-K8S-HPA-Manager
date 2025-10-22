# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/k8s-hpa-manager` para executar a aplicação.
**IMPORTANTE**: Interface **totalmente responsiva** - adapta-se a qualquer tamanho de terminal (recomendado: 80x24 ou maior).

---

## 📑 Índice / Table of Contents

1. [Quick Start](#-quick-start-para-novos-chats)
2. [Development Commands](#-development-commands)
3. [Architecture Overview](#-architecture-overview)
4. [Recent Features (2025)](#-melhorias-recentes-2025)
5. [User Interface & Controls](#-user-interface--controls)
6. [Troubleshooting](#-troubleshooting)
7. [Future Development](#-continuing-development)

---

## 🚀 Quick Start Para Novos Chats

### Project Summary
**Terminal-based Kubernetes HPA and Azure AKS Node Pool management tool** built with Go and Bubble Tea TUI framework. Features async rollout progress tracking with Rich Python-style progress bars, integrated status panel, session management, and unified HPA/node pool operations.

### Estado Atual (Outubro 2025)
- ✅ **Interface Responsiva** - adapta-se ao tamanho real do terminal (sem forçar 188x45)
- ✅ **Otimizada para Produção** - texto legível, painéis compactos (60x12), operação segura
- ✅ **Layout completo** com execução sequencial de node pools para stress tests
- ✅ **Rollouts detalhados** de HPA (Deployment/DaemonSet/StatefulSet)
- ✅ **CronJob management** completo (F9)
- ✅ **Prometheus Stack Management** (F8) com métricas reais
- ✅ **Status container** compacto (80x10) com progress bars Rich Python
- ✅ **Auto-descoberta de clusters** via `k8s-hpa-manager autodiscover`
- ✅ **Validação VPN on-demand** - verifica conectividade K8s antes de operações críticas
- ✅ **Validação Azure com timeout** - não trava em problemas de DNS/rede
- ✅ **Modais de confirmação** - Ctrl+D/Ctrl+U exigem confirmação antes de aplicar
- ✅ **Modais como overlay** - aparecem sobre o conteúdo sem esconder a aplicação
- ✅ **Log detalhado de alterações** - todas as mudanças exibidas no StatusContainer (antes → depois)
- ✅ **Navegação sequencial de abas** - Ctrl+←/→ para navegar entre abas com wrap-around
- ✅ **Versionamento automático** - via git tags com verificação de updates 1x/dia
- ✅ **Sistema de Logs Completo** (F3) - visualizador com scroll, copiar, limpar logs
- ✅ **Navegação ESC corrigida** - Node Pools voltam para Namespaces (origem do Ctrl+N)
- ✅ **Race condition corrigida** - Mutex RWLock para testes paralelos de cluster (thread-safe)
- ✅ **Interface Web POC (99% completa)** - HPAs, Node Pools, CronJobs e Prometheus Stack implementados com edição funcional + Dashboard redesignado com layout moderno grid 2x2 e métricas reais (ver Docs/README_WEB.md)

### Tech Stack
- **Language**: Go 1.23+ (toolchain 1.24.7)
- **TUI Framework**: Bubble Tea + Lipgloss
- **K8s Client**: client-go (official)
- **Azure SDK**: azcore, azidentity, armcontainerservice
- **Architecture**: MVC pattern com state-driven UI

---

## 🔧 Development Commands

### Terminal Requirements

**✅ Interface Totalmente Responsiva**

A aplicação usa **EXATAMENTE o tamanho do seu terminal** - sem forçar dimensões artificiais:

- **Adapta-se ao terminal**: Usa suas dimensões reais (ex: 80x24, 120x30, etc)
- **Texto legível**: Não precisa zoom out - mantenha Ctrl+0 (tamanho normal)
- **Otimizada para produção**: Layout compacto, operação segura sem erros visuais
- **Sem limites artificiais**: Removido forçamento de 188x45 que causava texto minúsculo

**Como funciona:**
1. Aplicação detecta tamanho real do terminal
2. Ajusta painéis automaticamente (60x12 base)
3. Status panel compacto (80x10)
4. Context box inline (cluster | sessão)
5. Scroll quando necessário

**Validação VPN e Azure:**
- **VPN Check**: Usa `kubectl cluster-info` para validar conectividade K8s real
- **Validação on-demand**: Testa VPN em início, namespaces, HPAs e timeouts
- **Azure timeout**: 5 segundos para evitar travamentos DNS
- **Mensagens claras**: Exibidas no StatusContainer com soluções (F5 para retry)

### Building and Running
```bash
make build                    # Build to ./build/k8s-hpa-manager (version auto-detected)
make build-all                # Build for multiple platforms (Linux, macOS, Windows)
make run                      # Build and run
make run-dev                  # Run with debug logging (go run . --debug)
make version                  # Show detected version from git tags
make release                  # Build for all platforms (Linux, macOS amd64/arm64, Windows)
```

### Testing
```bash
make test                     # Run all tests with verbose output
make test-coverage            # Run tests with coverage (generates coverage.html)
```

### Installation
```bash
./install.sh                  # Automated installer → /usr/local/bin/
./uninstall.sh                # Uninstaller (optionally removes session data)

# After installation
k8s-hpa-manager               # Run from anywhere
k8s-hpa-manager --debug       # Debug mode
k8s-hpa-manager --help        # Show help
```

### Cluster Auto-Discovery (NOVO)
```bash
k8s-hpa-manager autodiscover  # Auto-descobre clusters do kubeconfig
```
- Extrai resource groups do campo `user` (formato: `clusterAdmin_{RG}_{CLUSTER}`)
- Descobre subscriptions via Azure CLI
- Gera/atualiza `~/.k8s-hpa-manager/clusters-config.json`
- Escalável para 26, 70+ clusters

**Workflow:**
1. `az aks get-credentials --name CLUSTER --resource-group RG`
2. `k8s-hpa-manager autodiscover`
3. Node Pools prontos para uso

### Backup and Restore
```bash
./backup.sh "descrição"       # Criar backup antes de modificações
./restore.sh                  # Listar backups disponíveis
./restore.sh backup_name      # Restaurar backup específico
```
- Mantém os 10 backups mais recentes automaticamente
- Metadados inclusos (git commit, data, usuário)

### Local Development
```bash
./build/k8s-hpa-manager --debug              # Run local build
./build/k8s-hpa-manager --kubeconfig /path   # Custom kubeconfig
```

---

## 🏗️ Architecture Overview

### Estrutura de Diretórios

```
k8s-hpa-manager/
├── cmd/
│   ├── root.go                    # CLI entry point & commands (Cobra)
│   └── k8s-teste/                 # Layout test command
│       ├── main.go
│       └── simple_demo.go
├── internal/
│   ├── tui/                       # Terminal UI (Bubble Tea)
│   │   ├── app.go                 # Main orchestrator + centralized text methods
│   │   ├── handlers.go            # Event handlers
│   │   ├── views.go               # UI rendering & layout
│   │   ├── message.go             # Bubble Tea messages
│   │   ├── text_input.go          # Centralized text input with intelligent cursor
│   │   ├── resource_handlers.go   # HPA/Node Pool resource handlers
│   │   ├── resource_views.go      # Resource-specific views
│   │   ├── resource_operations.go # Resource operations
│   │   ├── cronjob_handlers.go    # CronJob handlers
│   │   ├── cronjob_views.go       # CronJob views
│   │   ├── add_cluster_*.go       # Cluster addition
│   │   ├── components/            # UI components
│   │   │   ├── status_container.go
│   │   │   └── unified_container.go
│   │   └── layout/                # Layout managers
│   │       ├── manager.go
│   │       ├── screen.go
│   │       ├── panels.go
│   │       └── constants.go
│   ├── models/
│   │   └── types.go               # All data structures & app state
│   ├── session/
│   │   └── manager.go             # Session persistence (template naming)
│   ├── kubernetes/
│   │   └── client.go              # K8s API wrapper (client-go)
│   ├── config/
│   │   └── kubeconfig.go          # Cluster discovery
│   ├── azure/
│   │   └── auth.go                # Azure SDK authentication
│   └── ui/                        # UI utilities
│       ├── progress.go
│       ├── logs.go
│       └── status_panel.go
├── build/                         # Build artifacts
├── backups/                       # Code backups (via backup.sh)
├── go.mod & go.sum
├── makefile
└── *.sh scripts                   # Install, uninstall, backup, restore
```

### Core Components

**TUI Layer** (`internal/tui/`):
- `app.go` - Main Bubble Tea app with centralized text editing
- `handlers.go` - User input and event handling
- `views.go` - UI rendering with intelligent cursor display
- `text_input.go` - Centralized text input module with cursor overlay
- `resource_*.go` - HPA and node pool resource management
- `cronjob_*.go` - CronJob management (F9)
- `components/` - Reusable UI components (status, containers)
- `layout/` - Layout management system

**Business Logic** (`internal/`):
- `kubernetes/client.go` - K8s client wrapper with per-cluster management
- `config/kubeconfig.go` - Kubeconfig discovery (akspriv-* pattern)
- `session/manager.go` - Session persistence with template naming
- `models/types.go` - Complete domain model and app state (AppModel)
- `azure/auth.go` - Azure SDK auth with browser/device code fallback

**Entry Points**:
- `main.go` - Application bootstrap
- `cmd/root.go` - Cobra CLI commands and flags

### Data Flow

1. **State-Driven Architecture**: `AppModel` in `models/types.go` maintains complete app state
2. **State Transitions**: `AppState` enum manages flow:
   - Cluster Selection → Session Selection → Namespace Selection → HPA/Node Pool Management → Editing → Help
3. **Multi-Selection Flow**: One Cluster → Multiple Namespaces → Multiple HPAs/Node Pools → Individual Editing
4. **Bubble Tea Messages**: Coordinate between UI interactions and business logic
5. **Client Management**: Per-cluster Kubernetes client instances
6. **Session System**: Preserves state for review/editing before application

### Dependencies

**Core Framework**:
- Bubble Tea v0.24.2 - TUI framework
- Lipgloss v1.1.0 - Styling and layout
- Cobra v1.10.1 - CLI commands

**Kubernetes**:
- client-go v0.31.4 - Official K8s Go client

**Azure**:
- azcore v1.19.1 - Core SDK
- azidentity v1.12.0 - Authentication
- armcontainerservice - AKS node pool management
- Azure CLI - External dependency for node pool operations

---

## 🆕 Melhorias Recentes (2025)

### 📱 Interface Responsiva e Otimizada para Produção (Outubro 2025)
**Problema resolvido:** Interface exigia terminal 188x45, causando texto minúsculo e risco de erros operacionais

**Solução implementada:**
- ✅ **Removido forçamento de dimensões** - `applyTerminalSizeLimit()` agora retorna tamanho REAL do terminal
- ✅ **Painéis compactos** - Reduzidos de 70x18 para 60x12 (altura mínima 5 linhas)
- ✅ **Status panel menor** - 140x15 → 80x10 (economiza espaço vertical)
- ✅ **Context box inline** - De 3-4 linhas com bordas para 1 linha: `cluster | sessão`
- ✅ **Validação Azure com timeout** - 5 segundos para evitar travamentos DNS
- ✅ **Texto legível** - Terminal em tamanho normal (Ctrl+0), sem zoom out necessário

**Arquivos modificados:**
- `internal/tui/app.go` - Removido forçamento, StatusContainer 80x10, timeout Azure
- `internal/tui/layout/constants.go` - MinTerminal 188x45 → 80x24
- `internal/tui/views.go` - Painéis 70x18 → 60x12, context box simplificado, altura dinâmica
- `cmd/root.go` - Timeout 5s em validação Azure

### 📊 Prometheus Stack Management (Outubro 2025)
- **Painel F8**: "Prometheus Stack Management" responsivo com scroll
- **Métricas Assíncronas**: Coleta em background via Metrics Server (não bloqueia UI)
- **Exibição Dual**:
  - Lista: `CPU: 1 (uso: 264m)/2 | MEM: 8Gi (uso: 3918Mi)/12Gi`
  - Edição: `CPU Request: 1`, `Memory Request: 8Gi`
- **Campos Display Separados**: `DisplayCPURequest` vs `CurrentCPURequest` (edit)
- **Auto-scroll**: Item selecionado sempre visível
- **Refresh a cada 300ms** durante coleta

### 🔄 Memória de Estado para CronJobs (Outubro 2025)
- **ESC preserva estado**: Volta para namespaces mantendo seleções e scroll
- **Fluxo**: Clusters → Namespaces → F9 (CronJobs) → ESC (volta preservando)
- **Delegação**: Handler ESC delegado para `handleEscape()` com lógica unificada
- **Consistência**: Comportamento idêntico ao F8 (Prometheus)

### 🔒 Correção de Race Condition em Testes de Cluster (Outubro 2025)
**Problema resolvido:** Goroutines concorrentes causavam race condition ao testar conexões com múltiplos clusters simultaneamente

**Sintomas:**
- Stack trace mostrando múltiplos goroutines (104, 105, 106, 107) acessando kubeconfig simultaneamente
- Erro em `message.go:216` durante `testSingleClusterConnection`
- Race condition em `json.Unmarshal` durante parsing de kubeconfig
- Concurrent map access em HTTP header creation

**Causa raiz:**
- `testClusterConnections()` iniciava testes paralelos para TODOS os clusters via `tea.Batch()`
- Cada goroutine chamava `getClient()` que carregava e parseava kubeconfig
- Operações de criação de cliente NÃO eram thread-safe
- Múltiplos goroutines tentavam criar clientes simultaneamente sem sincronização

**Solução implementada:**
- ✅ **Mutex RWLock** - Adicionado `sync.RWMutex` em `KubeConfigManager`
- ✅ **Double-check locking** - Padrão otimizado para minimizar contenção
- ✅ **Read lock para leituras** - Permite múltiplas leituras concorrentes de clientes existentes
- ✅ **Write lock para criação** - Serializa criação de novos clientes
- ✅ **Thread-safe client cache** - Map de clientes protegido por mutex

**Arquivos modificados:**
- `internal/config/kubeconfig.go` - Adicionado import `sync`, field `clientMutex sync.RWMutex`, lógica de double-check locking

**Código antes:**
```go
func (k *KubeConfigManager) getClient(clusterName string) (kubernetes.Interface, error) {
    if client, exists := k.clients[clusterName]; exists {  // ❌ Race condition
        return client, nil
    }
    // ... criar cliente sem proteção ...
    k.clients[clusterName] = client  // ❌ Concurrent map write
    return client, nil
}
```

**Código depois:**
```go
func (k *KubeConfigManager) getClient(clusterName string) (kubernetes.Interface, error) {
    // 1. Read lock para checagem rápida (permite leituras concorrentes)
    k.clientMutex.RLock()
    if client, exists := k.clients[clusterName]; exists {
        k.clientMutex.RUnlock()
        return client, nil
    }
    k.clientMutex.RUnlock()

    // 2. Write lock para criação (serializa criação)
    k.clientMutex.Lock()
    defer k.clientMutex.Unlock()

    // 3. Double-check: outro goroutine pode ter criado enquanto esperávamos lock
    if client, exists := k.clients[clusterName]; exists {
        return client, nil
    }

    // 4. Criar cliente de forma thread-safe
    // ... código de criação ...
    k.clients[clusterName] = client  // ✅ Protegido por write lock
    return client, nil
}
```

**Benefícios:**
- **Performance**: Read lock permite múltiplas leituras simultâneas (baixa contenção)
- **Segurança**: Write lock serializa criação de clientes (sem race conditions)
- **Eficiência**: Double-check locking evita lock desnecessário se cliente já existe
- **Produção-ready**: Solução padrão para lazy initialization concorrente em Go

### 🐛 Correção de Warnings Azure CLI como Erros (Outubro 2025)
**Problema resolvido:** Azure CLI warnings (como `pkg_resources deprecated`) eram tratados como erros fatais, abortando operações de node pool

**Solução implementada:**
- ✅ **Separação stdout/stderr** - `cmd.Stdout` e `cmd.Stderr` em buffers separados
- ✅ **Lista de warnings conhecidos** - Ignora `pkg_resources`, `extension altered`, etc
- ✅ **Validação inteligente** - Verifica se stderr contém APENAS warnings
- ✅ **Exit code real** - Usa `cmd.Run()` para verificar sucesso, não presença de stderr
- ✅ **Debug mode** - Warnings aparecem em `--debug` mas não falham operação

**Warnings ignorados:**
```
UserWarning: pkg_resources is deprecated
The behavior of this command has been altered by the following extension
__import__('pkg_resources').declare_namespace(__name__)
WARNING: (qualquer linha com prefixo WARNING:)
```

**Arquivos modificados:**
- `internal/tui/app.go:3535-3683` - Função `executeAzureCommand()` refatorada
- Import `bytes` adicionado para buffers separados

**Antes:**
```go
output, err := cmd.CombinedOutput()  // ❌ Mistura stdout + stderr
if err != nil { return error }       // ❌ Warnings tratados como erro
```

**Depois:**
```go
var stdout, stderr bytes.Buffer
cmd.Stdout, cmd.Stderr = &stdout, &stderr
err := cmd.Run()
if err != nil && !isOnlyWarnings(stderr) { return error }  // ✅ Ignora warnings
```

### 🔄 Lógica Sequencial Inteligente de Node Pools (Outubro 2025)
**Problema resolvido:** Azure CLI não permite `scale` com autoscaling habilitado - aplicação tentava scale ANTES de desabilitar autoscaling

**Cenário problemático:**
- Usuário muda node pool de **AUTO → MANUAL** e define `NodeCount = 0`
- Aplicação tentava: `az aks nodepool scale` → ❌ **ERROR: Cannot scale cluster autoscaler enabled node pool**
- Ordem errada dos comandos causava falha

**Solução implementada:**
- ✅ **4 cenários detectados automaticamente**:
  1. **AUTO → MANUAL**: Desabilita autoscaling → Faz scale
  2. **MANUAL → AUTO**: Faz scale → Habilita autoscaling com min/max
  3. **AUTO → AUTO**: Atualiza min/max count
  4. **MANUAL → MANUAL**: Faz scale direto

**Arquivos modificados:**
- `internal/tui/app.go:3433-3545` - Lógica de construção de comandos refatorada

**Workflow esperado pelo usuário (agora funciona!):**
```bash
# Cenário: Stress test com scale down completo
# 1. Node pool "fatura" está com autoscaling AUTO (min: 2, max: 5)
# 2. Usuário muda para MANUAL e define NodeCount = 0
# 3. Aplicação INTELIGENTEMENTE executa:
#    → PASSO 1: az aks nodepool update --disable-cluster-autoscaler
#    → PASSO 2: az aks nodepool scale --node-count 0
# ✅ Operação bem-sucedida!
```

**Código antes (ordem errada):**
```go
// ❌ Tentava scale ANTES de desabilitar
if pool.NodeCount != pool.OriginalValues.NodeCount {
    cmds.append(scaleCommand)  // ERRO se autoscaling ativo!
}
if pool.AutoscalingEnabled != pool.OriginalValues.AutoscalingEnabled {
    cmds.append(disableAutoscaling)
}
```

**Código depois (ordem inteligente):**
```go
// ✅ Detecta cenário e ordena comandos corretamente
changingToManual := pool.OriginalValues.AutoscalingEnabled && !pool.AutoscalingEnabled
if changingToManual {
    cmds.append(disableAutoscaling)  // PRIMEIRO desabilita
    if nodeCountChanged {
        cmds.append(scaleCommand)     // DEPOIS faz scale
    }
}
```

### 🚀 Execução Sequencial Assíncrona de Node Pools (Outubro 2025)
**Problema resolvido:** Execução sequencial bloqueava a interface durante aplicação de node pools

**Solução implementada:**
- ✅ **Execução totalmente assíncrona** - Non-blocking via Bubble Tea messages
- ✅ **Interface sempre responsiva** - Edite HPAs, navegue, gerencie outros recursos
- ✅ **Feedback em tempo real** - StatusContainer mostra progresso ao vivo
- ✅ **Auto-execução do segundo pool** - Sistema monitora *1 e inicia *2 automaticamente
- ✅ **Multi-tasking completo** - Aplique HPAs enquanto node pools executam
- ✅ **Validação VPN integrada** - Verifica conectividade antes de aplicar (timeout 5s)
- ✅ **Error handling robusto** - Falhas não travam a UI, feedback claro se VPN desconectada

**Workflow:**
1. F12 para marcar monitoring-1 (*1) e monitoring-2 (*2)
2. Editar valores (manual/auto, node counts)
3. Ctrl+D/U → Inicia execução assíncrona
4. StatusContainer: `🔍 Verificando conectividade VPN com Azure...`
5. StatusContainer: `✅ VPN conectada - Azure acessível` (ou `❌ VPN desconectada`)
6. **Interface livre** - Edite HPAs, CronJobs, etc.
7. StatusContainer: `🔄 *1: Aplicando...` → `✅ *1: Completado`
8. Sistema inicia *2 automaticamente
9. StatusContainer: `🚀 Iniciando automaticamente *2` → `✅ *2: Completado`

**Arquivos modificados:**
- `internal/tui/message.go` - Novas mensagens assíncronas
- `internal/tui/app.go` - Handlers para mensagens sequenciais
- `internal/tui/handlers.go` - Detecção automática de execução sequencial

### 🔐 Validação VPN On-Demand (Outubro 2025)
**Problema resolvido:** Aplicação não validava VPN antes de operações Kubernetes, causando timeouts e erros confusos

**Solução implementada:**
- ✅ **Validação com kubectl** - Usa `kubectl cluster-info --request-timeout=5s` (testa conectividade K8s real)
- ✅ **On-demand em pontos críticos**:
  - `discoverClusters()` - Valida VPN antes de descobrir clusters
  - `loadNamespaces()` - Valida VPN antes de carregar namespaces
  - `loadHPAs()` - Valida VPN antes de carregar HPAs
  - `testSingleClusterConnection()` - Diagnostica em timeout/erro de conexão
  - `configurateSubscription()` - Diagnostica em timeout Azure (5s)
- ✅ **Mensagens no StatusContainer** - Não quebra TUI, exibe dentro do container
- ✅ **Soluções claras**:
  - `❌ VPN desconectada - Kubernetes inacessível`
  - `💡 SOLUÇÃO: Conecte-se à VPN e tente novamente (F5)`

**Workflow:**
1. Aplicação inicia → `🔍 Validando conectividade VPN...`
2. Se VPN OFF → `❌ VPN desconectada - kubectl não funcionará` + mensagem de solução
3. Se VPN ON → `✅ VPN conectada - Kubernetes acessível` + continua operação
4. Em qualquer timeout posterior → Diagnostica novamente VPN/Azure AD

**Arquivos modificados:**
- `internal/tui/message.go` - Função `checkVPNConnectivity()` com kubectl, integrado em pontos críticos
- `internal/tui/app.go` - Validação VPN em `loadNamespaces()` e `loadHPAs()`
- `internal/models/types.go` - Campos VPN status (VPNConnected, VPNLastCheck, VPNStatusMessage)

### 🔍 Auto-Descoberta de Clusters (Outubro 2025)
- **Comando**: `k8s-hpa-manager autodiscover`
- **Extração**: Resource groups do campo `user` (formato: `clusterAdmin_{RG}_{CLUSTER}`)
- **Integração Azure CLI**: Descobre subscriptions automaticamente
- **Escalável**: 26, 70+ clusters sem configuração manual
- **Casos de Uso**: Onboarding, mudanças de subscriptions, rotação de credenciais

### ✅ Modais de Confirmação e Segurança (Outubro 2025)
**Problema resolvido:** Operações críticas (Ctrl+D/Ctrl+U) aplicavam alterações imediatamente sem confirmação, risco de erros operacionais

**Solução implementada:**
- ✅ **Confirmação obrigatória** - Todos Ctrl+D/Ctrl+U exigem confirmação explícita (ENTER/ESC)
- ✅ **Modais como overlay** - Aparecem sobre o conteúdo sem esconder a aplicação (mantém contexto visual)
- ✅ **Mensagens personalizadas por tipo**:
  - HPA individual: `"Aplicar alterações do HPA:\nnamespace/nome"`
  - HPAs em lote: `"Aplicar alterações em TODOS os HPAs selecionados"`
  - Node pools: `"Aplicar alterações nos Node Pools modificados"`
  - Node pools sequencial: `"Executar sequencialmente:\n*1 pool1 → *2 pool2"`
  - Sessão mista: `"Aplicar alterações da sessão mista:\nX HPAs + Y Node Pools"`
- ✅ **Indicador de quantidade** - `⚡ X itens serão modificados no cluster!`
- ✅ **Modal de erro VPN** - Feedback visual quando VPN desconectada com soluções (F5: reload, ESC: fechar)
- ✅ **Modal de restart** - Após auto-descoberta informa necessidade de restart (F4: sair, ESC: continuar)

**Arquivos modificados:**
- `internal/models/types.go` - Campos ShowConfirmModal, ConfirmModalCallback, etc
- `internal/tui/views.go` - Funções renderConfirmModal(), renderVPNErrorModal(), renderRestartModal()
- `internal/tui/app.go` - renderModalOverlay() para exibir modais sobre conteúdo, executeConfirmedAction()
- `internal/tui/handlers.go` - Modificados Ctrl+D/Ctrl+U para mostrar modal antes de aplicar

### 🔄 Navegação Sequencial entre Abas (Outubro 2025)
- **Ctrl+→**: Próxima aba com wrap-around (última → primeira)
- **Ctrl+←**: Aba anterior com wrap-around (primeira → última)
- **Ícones direcionais**: ⬅️/➡️ no status durante navegação
- **Foco sempre visível**: Aba em foco exibida após navegação
- **Complementa Alt+1-9/0**: Navegação numérica direta + navegação sequencial

### 🔧 Correção de Carregamento de Node Pools (Outubro 2025)
**Problema resolvido:** Node pools não carregavam porque a aplicação procurava `clusters-config.json` em locais incorretos

**Solução implementada:**
- ✅ **Prioridade de busca corrigida** - Agora busca primeiro em `~/.k8s-hpa-manager/clusters-config.json` (onde `autodiscover` salva)
- ✅ **Fallback inteligente** - Se não encontrar no diretório padrão, tenta:
  1. `~/.k8s-hpa-manager/clusters-config.json` (padrão - onde autodiscover salva)
  2. Diretório do executável (fallback 1)
  3. Diretório de trabalho atual (fallback 2)
- ✅ **Mensagem de erro clara** - Sugere executar `k8s-hpa-manager autodiscover` se arquivo não for encontrado
- ✅ **Consistência com autodiscover** - Ambos usam o mesmo diretório padrão

**Causa raiz:**
- `loadClusterConfig()` em `internal/tui/message.go` buscava primeiro no diretório do executável
- Comando `autodiscover` salva em `~/.k8s-hpa-manager/` (diretório padrão da aplicação)
- Incompatibilidade de caminhos causava falha no carregamento dos node pools

**Arquivos modificados:**
- `internal/tui/message.go` (linhas 467-501) - Função `loadClusterConfig()` com prioridade corrigida
- `internal/tui/views.go` (linhas 3092-3093) - Help atualizado com informação da correção

**Workflow correto:**
1. `k8s-hpa-manager autodiscover` → gera `~/.k8s-hpa-manager/clusters-config.json`
2. Aplicação inicia → busca primeiro em `~/.k8s-hpa-manager/`
3. Node pools carregam corretamente ✅

### 📝 Log Detalhado de Alterações (Outubro 2025)
**Problema resolvido:** Usuário não via quais alterações estavam sendo aplicadas, apenas mensagens genéricas de sucesso/erro

**Solução implementada:**
- ✅ **Log antes → depois** - Todas alterações exibidas no StatusContainer com formato `valor_antigo → valor_novo`
- ✅ **Alterações de HPA** - Min/Max Replicas, CPU/Memory Target: `Min Replicas: 1 → 2`, `CPU Target: 50% → 70%`
- ✅ **Alterações de recursos** - CPU/Memory Request/Limit: `CPU Request: 50m → 100m`, `Memory Limit: 512Mi → 1Gi`
- ✅ **Alterações de node pools** - Count, Min/Max, Autoscaling: `Node Count: 3 → 5`, `Autoscaling: Desativado → Ativado`
- ✅ **Logs por operação**:
  - `⚙️ Aplicando HPA: namespace/nome`
  - `📝 Min Replicas: 1 → 2`
  - `🔧 CPU Request: 50m → 100m`
  - `✅ HPA aplicado: namespace/nome`
- ✅ **Logs de erro** - `❌ Erro ao aplicar HPA namespace/nome: erro_detalhado`

**Exemplo de output:**
```
⚙️ Aplicando HPA: ingress-nginx/nginx-ingress-controller
  📝 Min Replicas: 1 → 2
  📝 Max Replicas: 8 → 12
  📝 CPU Target: 60% → 70%
  🔧 CPU Request: 50m → 100m
  🔧 Memory Request: 90Mi → 180Mi
✅ HPA aplicado: ingress-nginx/nginx-ingress-controller
```

**Arquivos modificados:**
- `internal/tui/app.go` - Funções logHPAChanges(), logResourceChanges(), logNodePoolChanges()
- `internal/tui/app.go` - applyHPAChanges() e applyHPAChangesAsync() com logs detalhados
- `internal/tui/app.go` - applyNodePoolChanges() com logs detalhados

### 🔄 Sistema de Versionamento Automático e Updates (Outubro 2025)
**Funcionalidade:** Sistema completo de versionamento semântico e verificação automática de updates

**Características implementadas:**
- ✅ **Versionamento automático via Git Tags** - Versão injetada no build usando `git describe --tags`
- ✅ **Comando version** - `k8s-hpa-manager version` mostra versão e verifica updates
- ✅ **Verificação em background** - Checa GitHub Releases 1x por dia (não-bloqueante)
- ✅ **Notificação no TUI** - Mensagens aparecem no StatusContainer após 3 segundos
- ✅ **Flag configurável** - `--check-updates=false` para desabilitar verificação
- ✅ **Versão dev** - Builds sem tag mostram "dev-<commit>" e não verificam updates
- ✅ **Cache inteligente** - Arquivo `~/.k8s-hpa-manager/.update-check` controla frequência
- ✅ **Timeout 5s** - Não trava se GitHub estiver offline

**Estrutura:**
```
internal/updater/
├── version.go    # Versionamento semântico (var Version injetada)
├── github.go     # Cliente GitHub API (releases/latest)
└── checker.go    # Lógica de verificação (1x/dia, cache)
```

**Workflow de Release:**
```bash
# 1. Criar tag de versão
git tag v1.6.0
git push origin v1.6.0

# 2. Build automático com versão injetada
make build
# Output: Building k8s-hpa-manager v1.6.0...

# 3. Verificar versão no binário
./build/k8s-hpa-manager version
# Output: k8s-hpa-manager versão 1.6.0

# 4. Criar release multiplataforma
make release
# Gera binários para Linux, macOS (amd64/arm64), Windows
```

**Comandos:**
```bash
# Verificar versão e updates
k8s-hpa-manager version

# Ver versão detectada durante build
make version

# Build com versão injetada
make build                    # Versão da tag atual
make release                  # Multi-platform builds

# Desabilitar verificação automática
k8s-hpa-manager --check-updates=false
```

**Notificação no TUI:**
Quando houver update disponível (após 3s do startup):
```
┌─ Status e Informações ────────────────────┐
│ 🆕 Nova versão disponível: 1.5.0 → 1.6.0  │
│ 📦 Download: https://github.com/.../v1.6.0│
│ 💡 Execute 'k8s-hpa-manager version'       │
└────────────────────────────────────────────┘
```

**Arquivos modificados:**
- `internal/updater/` (NOVO) - Sistema completo de versionamento
- `cmd/version.go` (NOVO) - Comando version
- `cmd/root.go` - Flag --check-updates e verificação em background
- `internal/tui/app.go` - Notificação no StatusContainer (checkForUpdatesInBackground)
- `makefile` - LDFLAGS com injeção de versão, targets version e release

### 📝 Sistema de Logs Completo (Outubro 2025)
**Funcionalidade:** Sistema completo de logging com visualizador TUI integrado

**Características implementadas:**
- ✅ **Salvamento automático** - Todos os logs do StatusContainer salvos em `Logs/k8s-hpa-manager_YYYY-MM-DD.log`
- ✅ **Rotação de arquivos** - 10MB por arquivo, mantém 5 backups
- ✅ **Thread-safe** - Mutex para operações concorrentes
- ✅ **Buffer em memória** - 1000 linhas para acesso rápido
- ✅ **Visualizador TUI (F3)** - Interface completa de visualização
- ✅ **Colorização** - Logs coloridos por nível (ERROR vermelho, WARNING laranja, SUCCESS verde)
- ✅ **Navegação completa** - ↑↓/k j, PgUp/PgDn, Home/End
- ✅ **Copiar logs** - Tecla C copia para `/tmp/k8s-hpa-manager-logs.txt`
- ✅ **Limpar logs** - Tecla L limpa arquivo de logs
- ✅ **Reload** - R/F5 recarrega logs em tempo real
- ✅ **ESC para voltar** - Retorna ao estado anterior

**Estrutura:**
```
Logs/
└── k8s-hpa-manager_2025-10-15.log    # Logs do dia
internal/logs/
└── manager.go                         # Singleton LogManager
internal/tui/
├── logviewer_handlers.go              # Handlers de navegação
└── logviewer_views.go                 # Renderização colorida
```

**Arquivos modificados:**
- `internal/logs/manager.go` (NOVO) - Sistema completo de logging
- `internal/tui/logviewer_handlers.go` (NOVO) - Handlers do visualizador
- `internal/tui/logviewer_views.go` (NOVO) - Renderização TUI
- `internal/tui/components/status_container.go` - Integração automática
- `internal/tui/app.go` - F3 global, handleEscape para StateLogViewer
- `internal/models/types.go` - StateLogViewer e campos relacionados
- `.gitignore` - Ignora `Logs/` e `*.log`

### 🐛 Correção de Navegação ESC em Node Pools (Outubro 2025)
**Problema resolvido:** ESC na tela de node pools voltava para seleção de clusters em vez de namespaces

**Solução implementada:**
- ✅ **Fluxo corrigido**: Namespaces → Ctrl+N → Node Pools → ESC → Namespaces
- ✅ **Consistência**: Volta para onde veio (origem do Ctrl+N)

**Arquivo modificado:**
- `internal/tui/app.go:1603` - `StateNodeSelection` agora vai para `StateNamespaceSelection`

**Antes:** `Clusters ← ESC ← Node Pools` ❌
**Depois:** `Namespaces ← ESC ← Node Pools` ✅

### 🔧 Correções de Linter para CI/CD (Outubro 2025)
**Problema resolvido:** GitHub Actions falhavam com erros de linter

**Correções aplicadas:**
- ✅ **strings.TrimSuffix** - Simplificado em 3 locais (app.go, message.go)
- ✅ **fmt.Sprintf desnecessário** - Removido em 4 locais (handlers.go, app.go)
- ✅ **fmt.Println com \n redundante** - Corrigido em cmd/root.go e cmd/k8s-teste/main.go
- ✅ **Nil check redundante** - Removido em app.go:4362

**Resultado:**
- ✅ `make test` passa sem erros
- ✅ CI do GitHub passa
- ℹ️ 77 sugestões de linter restantes (não críticas, código funcional)

### 💾 Salvamento Manual para Rollback (Janeiro 2025)
- **Ctrl+S sem modificações**: Cria snapshots para rollback
- **Workflow**:
  1. Carregar sessão (Ctrl+L)
  2. Ctrl+S imediatamente (sem modificar)
  3. Nomear como "rollback-producao-2025-01-10"
  4. Sessão de backup pronta
- **Funciona em**: HPAs, Node Pools, Sessões Mistas

### 🔄 Execução Sequencial de Node Pools (Assíncrona)
- **F12**: Marca até 2 node pools para execução sequencial
- **Indicadores**: `*1` (manual), `*2` (auto após conclusão do *1)
- **Execução Assíncrona**: Non-blocking - interface permanece responsiva
- **Multi-tasking**: Edite HPAs, gerencie CronJobs enquanto node pools executam
- **Feedback em Tempo Real**: StatusContainer mostra progresso
- **Stress Tests**: monitoring-1 → 0 nodes, monitoring-2 → scale up
- **Persistência**: Salvos em sessões

### 🎯 CronJob Management (F9)
- **Acesso**: F9 na seleção de namespaces
- **Status**: 🟢 Ativo, 🔴 Suspenso, 🟡 Falhou, 🔵 Executando
- **Schedule**: Conversão cron → texto ("0 2 * * * - executa todo dia às 2:00 AM")
- **Operações**: Ctrl+D individual, Ctrl+U batch
- **Memória**: ESC volta para namespaces preservando estado

### 🎨 Progress Bars Rich Python
- **Caracteres**: ━ (preenchido), ╌ (vazio)
- **Cores Dinâmicas**:
  - 0-24%: 🔴 Vermelho
  - 25-49%: 🟠 Laranja
  - 50-74%: 🟡 Amarelo
  - 75-99%: 🟢 Verde claro
  - 100%: ✅ Verde completo
- **Lifecycle**: Início → Progresso → Sucesso/Falha
- **Auto-cleanup**: 3 segundos após conclusão
- **Bottom-Up**: Novos itens na última linha

### 🔧 Correções Críticas
- **MinReplicas**: Corrigido parsing de ponteiros *int32
- **Rollouts em Sessões**: DaemonSet/StatefulSet salvos corretamente
- **Variation Selectors**: Removidos caracteres invisíveis (U+FE0F) de emojis
- **Terminal Size**: Limitação 188x45 para garantir visibilidade
- **Mouse Selection**: Removido `tea.WithMouseCellMotion()` para permitir seleção de texto

### 📐 Layout Responsivo (Janeiro 2025)
- **Terminal Limit**: 42 linhas x 185 colunas (via `applyTerminalSizeLimit()`)
- **Espaçamento Universal**: Status Panel em posição fixa em todas as telas
- **Node Pool Responsivo**: Scroll Shift+Up/Down, indicadores `[5-15/45]`

---

## ⌨️ User Interface & Controls

### Navigation Controls
- **Arrow Keys / vi-keys (hjkl)**: Navigate lists
- **Tab**: Switch panels
- **Space**: Select/deselect items
- **Enter**: Confirm selection or edit
- **ESC**: Go back/cancel (preserva contexto!)
- **F3**: Log viewer (scroll, copiar, limpar)
- **F4**: Exit application
- **?**: Help screen (scrollable)

### Cluster & Session Management
- **Ctrl+L**: Load session
- **F5/R**: Reload cluster list
- **Ctrl+S**: Save session (funciona SEM modificações para rollback)
- **Ctrl+M**: Create mixed session (HPAs + Node Pools)

### HPA Operations
- **Ctrl+D**: Apply individual HPA (shows ● counter)
- **Ctrl+U**: Apply all HPAs in batch
- **Space** (edit mode): Toggle rollout (Deployment/DaemonSet/StatefulSet)
- **↑↓**: Navigate rollout fields

### Node Pool Operations
- **Ctrl+N**: Access node pool management
- **Ctrl+D/Ctrl+U**: Apply node pool changes
- **Space** (edit mode): Toggle autoscaling
- **F12**: Mark for sequential execution (max 2)

### CronJob Management (F9)
- **F9**: Access CronJob management
- **Space**: Select/deselect
- **Enter**: Edit (enable/disable suspend)
- **Ctrl+D/U**: Apply changes
- **ESC**: Return to namespaces (preserves state)

### Prometheus Stack (F8)
- **F8**: Access Prometheus Stack management
- **Enter**: Edit resources (CPU/Memory requests/limits)
- **Shift+Up/Down**: Scroll
- **ESC**: Return to namespaces (preserves state)

### Tab Navigation
- **Ctrl+T**: New tab (max 10 tabs)
- **Ctrl+W**: Close current tab (doesn't close last)
- **Alt+1-9**: Switch to tab 1-9 (quick shortcut)
- **Alt+0**: Switch to tab 10
- **Ctrl+→**: Next tab (with wrap-around)
- **Ctrl+←**: Previous tab (with wrap-around)

### Scroll Controls
- **Shift+Up/Down**: Scroll painéis responsivos
- **Mouse Wheel**: Alternative scroll
- **Indicadores**: `[5-15/45]` mostram posição

### Log Viewer (F3)
- **F3**: Open log viewer
- **↑↓ / k j**: Scroll line by line
- **PgUp/PgDn**: Scroll by page
- **Home**: Jump to beginning
- **End**: Jump to end
- **C**: Copy logs to `/tmp/k8s-hpa-manager-logs.txt`
- **L**: Clear all logs
- **R / F5**: Reload logs
- **ESC**: Return to previous screen

### Special Features
- **S** (namespace selection): Toggle system namespaces
- **F9**: CronJob management
- **F8**: Prometheus Stack management

---

## 🔑 Key Features

### Kubernetes HPA Management
1. **Cluster Discovery**: Auto-discover `akspriv-*` clusters
2. **Single Cluster Selection**: One cluster at a time
3. **Multiple Namespace Selection**: Visual indicators + toggle
4. **System Namespace Filtering**: Toggle with `S` key
5. **Async HPA Counting**: Background counting for performance
6. **Multi-HPA Selection**: Batch operations
7. **Live Editing**: Min/max replicas, CPU/memory targets
8. **Rollout Integration**: Toggle per HPA (Deployment/DaemonSet/StatefulSet)

### Azure AKS Node Pool Management
9. **Node Pool Discovery**: From `clusters-config.json` or autodiscover
10. **Azure Authentication**: Browser + device code fallback
11. **Subscription Management**: Auto-configuration
12. **Node Pool Editing**: Count, min/max, autoscaler
13. **Real-time Application**: Via Azure CLI with progress
14. **Filtered Output**: Clean JSON from Azure CLI

### Session & Workflow Management
15. **Unified Sessions**: Save/restore HPA and node pools
16. **Mixed Sessions**: Combine HPAs + Node Pools (Ctrl+M)
17. **Session Type Detection**: Auto-detect HPA/Node Pool/Mixed
18. **State-Preserving Loading**: Review before applying
19. **Template Naming**: Variables: `{action}_{cluster}_{timestamp}_{env}_{user}_{hpa_count}`
20. **Session Name Display**: Persistent across screens
21. **Rollout Persistence**: Saved with sessions
22. **Manual Rollback**: Save without modifications (Ctrl+S)

### Session Storage
- **Location**: `~/.k8s-hpa-manager/sessions/`
- **Folders**: `HPA-Upscale/`, `HPA-Downscale/`, `Node-Upscale/`, `Node-Downscale/`
- **Cleanup**: Keeps 5 most recent autosaves

### Progress Tracking & Status
23. **Async Rollout Progress**: Rich Python-style bars (━/╌)
24. **Integrated Status Panel**: "📊 Status e Informações" (140x15)
25. **Multiple Rollout Support**: Deployment/DaemonSet/StatefulSet
26. **Thread-Safe Updates**: Mutex-protected progress
27. **Visual Indicators**: ● counters for application tracking

### User Interface & Experience
28. **Comprehensive Help**: `?` key with scroll navigation
29. **Cluster Connectivity**: Real-time status indicators
30. **Individual & Batch**: Ctrl+D (individual), Ctrl+U (batch)
31. **Error Recovery**: ESC returns from errors
32. **Multi-panel Interface**: TAB navigation, namespace-grouped HPAs
33. **Non-Blocking Status**: Dedicated panel, no workflow interruption
34. **Auto Subscription Switching**: Azure subscription per cluster
35. **Token Expiration Handling**: Auto re-authentication

---

## 🔧 Troubleshooting

### Common Issues

**Installation:**
- **"Go not found"**: Install Go 1.23+ from https://golang.org/dl/
- **Permission denied**: Script needs sudo for `/usr/local/bin/`
- **Binary not found**: Restart terminal or check PATH

**Cluster:**
- **Offline status**: `kubectl cluster-info --context=<cluster>`
- **Client not found**: Fixed in recent versions, restart if persists
- **HPAs not loading**: Check RBAC, toggle system namespaces with `S`

**Sessions:**
- **Loading doesn't apply**: Intentional - use Ctrl+D/U to apply
- **Help too large**: Use ↑↓ or PgUp/PgDn to scroll

### Error Recovery
- **ESC**: Returns from errors while preserving context
- **F4**: Force exit
- **?**: Access help from any screen

### Performance Tips
- System namespace filtering improves loading speed
- Background HPA counting reduces wait times
- Session system preserves work between runs

---

## 🚀 Continuing Development

### Context for Next Claude Sessions

**Quick Context Template:**
```
Projeto: Terminal-based Kubernetes HPA + Azure AKS Node Pool management tool

Tech Stack:
- Go 1.23+ (toolchain 1.24.7)
- Bubble Tea TUI + Lipgloss
- client-go (K8s) + Azure SDK

Estado Atual (Outubro 2025):
✅ Auto-descoberta de clusters (autodiscover command)
✅ Progress bars Rich Python com cores dinâmicas
✅ CronJob management (F9) com memória de estado
✅ Prometheus Stack management (F8) com métricas reais
✅ Execução sequencial de node pools
✅ Sessões de rollback (Ctrl+S sem modificações)
✅ Layout responsivo 188x45 com scroll inteligente
✅ Modais de confirmação (Ctrl+D/U) com overlay
✅ Log detalhado (antes → depois) no StatusContainer
✅ Navegação sequencial de abas (Ctrl+←/→)

Build: make build
Binary: ./build/k8s-hpa-manager
```

### File Structure Quick Reference
```
internal/tui/
├── app.go - Main orchestrator + text methods
├── text_input.go - Centralized text input (intelligent cursor)
├── handlers.go - Event handling
├── views.go - UI rendering
├── resource_*.go - Resource management
├── cronjob_*.go - CronJob management
├── components/ - UI components
└── layout/ - Layout managers

internal/
├── models/types.go - App state (AppModel)
├── session/manager.go - Session persistence
├── kubernetes/client.go - K8s wrapper
├── azure/auth.go - Azure auth
└── ui/ - Progress, logs, status
```

### Development Commands Quick Reference
```bash
# Build & Run
make build                    # → ./build/k8s-hpa-manager
make run-dev                  # Debug mode

# Install
./install.sh                  # Global install
k8s-hpa-manager --debug       # Run with debug

# Backup
./backup.sh "desc"            # Create backup
./restore.sh                  # List/restore backups

# Auto-Discovery
k8s-hpa-manager autodiscover  # Discover clusters
```

### Best Practices

**When Adding Features:**
1. Follow MVC pattern: Views in `views.go`, logic in `handlers.go`, state in `models/types.go`
2. Use text editing helpers from `text_input.go`
3. Update help in `renderHelp()` function
4. Run `make build` after changes
5. Update this CLAUDE.md

**Code Style:**
- Error handling: Proper propagation, no panics
- State management: All UI state in `AppModel`
- Async operations: Use Bubble Tea commands
- Unicode safety: Always use `[]rune`
- Logging: Use `a.debugLog()` method

**Common Gotchas:**
- Function closures: Check for missing `}`
- Bubble Tea returns: Always return `tea.Model` and `tea.Cmd`
- Text editing: Initialize `CursorPosition` when starting
- Session persistence: Use folder-aware functions
- Azure auth: Handle token expiration gracefully

### Known Technical Debt

**Code Quality:**
- Some async operations need better error propagation
- Unit test coverage could be expanded
- Inline documentation for complex functions
- Large cluster lists could benefit from virtualization

**UI/UX:**
- Better handling of very small terminals
- Support for color themes/accessibility
- More intuitive keyboard shortcuts
- More detailed progress indicators

### Potential Next Features

**High Priority:**
1. Field validation (CPU/memory formats, replica ranges)
2. Undo/Redo functionality
3. Search/Filter within HPA/namespace lists
4. Export sessions to YAML/JSON

**Medium Priority:**
5. User-configurable templates
6. Metrics integration (current usage alongside targets)
7. History tracking with timestamps
8. Plugin system for custom validation

**Advanced:**
9. Git integration for config tracking
10. Notification system for failures
11. Optional web dashboard
12. RESTful API for external tools
13. Prometheus/Grafana integration

---

## 📝 Important Implementation Notes

### System Namespaces Filter
Auto-filters: `kube-system`, `istio-system`, `cert-manager`, `gatekeeper-system`, `monitoring`, `prometheus`, `grafana`, `flux-system`, `argocd`, and more.
Full list in `internal/kubernetes/client.go:systemNamespaces`.

### Cluster Pattern Matching
Only discovers clusters with `akspriv-*` pattern from kubeconfig contexts.

### Template Variables
- `{action}` - Custom action name
- `{cluster}` - Primary cluster name
- `{env}` - Environment (dev/prod/staging/test)
- `{timestamp}` - dd-mm-yy_hh:mm:ss
- `{date}` - dd-mm-yy
- `{time}` - hh:mm:ss
- `{user}` - Current system user
- `{hpa_count}` - Number of HPAs

### Custom Border Implementation
Since Lipgloss 1.1.0 doesn't include native BorderTitle support, the app implements custom `renderPanelWithTitle()`:
- Auto-calculates panel width
- Draws Unicode borders (╭─╮│╰╯) with integrated titles
- Handles Unicode safely using rune conversion
- Centers titles dynamically

---

## 🌐 Interface Web

### Status: ✅ 95% Completa - Node Pools Editor Funcional

Interface web moderna construída com **React + TypeScript + shadcn/ui**, totalmente integrada ao backend Go existente.

**Estrutura:**
```
internal/web/
├── frontend/          # React/TypeScript app (NOVO)
│   ├── src/
│   ├── package.json
│   ├── vite.config.ts
│   └── README.md
├── static/            # Build output (embedado no Go binary)
├── handlers/          # Go REST API handlers
├── middleware/        # Auth, CORS, Logging
└── server.go         # Gin HTTP server
```

**Desenvolvimento:**
```bash
# 1. Instalar dependências do frontend
make web-install

# 2. Iniciar backend Go (terminal 1)
./build/k8s-hpa-manager web --port 8080

# 3. Iniciar frontend dev server (terminal 2)
make web-dev
# Frontend: http://localhost:5173
# API proxy: /api/* → http://localhost:8080
```

**Build Produção:**
```bash
# Build completo (frontend + backend)
make build-web

# Ou separado
make web-build    # Build frontend → internal/web/static/
make build        # Build Go binary (embeda static/)
```

**Executar:**
```bash
./build/k8s-hpa-manager web --port 8080
# Acesse: http://localhost:8080
# Token: poc-token-123 (padrão POC)
```

**Tech Stack Frontend:**
- **Framework**: React 18.3 + TypeScript 5.8
- **Build**: Vite 5.4 (HMR, fast builds)
- **Styling**: Tailwind CSS 3.4
- **UI**: shadcn/ui (Radix UI primitives)
- **State**: React Query (TanStack)
- **Routing**: React Router DOM
- **Icons**: Lucide React
- **Charts**: Recharts

**Features Implementadas:**
- ✅ **Backend REST API** (Gin Framework)
- ✅ **Autenticação** Bearer Token
- ✅ **Endpoints**: Clusters, Namespaces, HPAs, Node Pools, CronJobs, Prometheus
- ✅ **Validação** Azure/VPN (cache 5min, timeout 5s)
- ✅ **Frontend React** moderno com shadcn/ui
- ✅ **Dashboard** com estatísticas e gráficos
- ✅ **HPA Management** - CRUD completo com edição de recursos
- ✅ **Node Pools** - Grid responsivo com editor funcional (autoscaling, node count, min/max)
- ✅ **Node Pool Cluster Matching** - Correção de `-admin` suffix para matching correto
- ✅ **CronJobs** - Suspend/Resume
- ✅ **Prometheus Stack** - Resource management
- ✅ **Modal de Confirmação** - Preview de alterações e progress bars de rollout
- ✅ **Deployment Resource Updates** - CPU/Memory Request/Limit aplicados ao deployment
- ✅ **Dev Server** com proxy API
- ✅ **Embed no Go** binary (produção)
- 🚧 **Sessões** (Planejado - ver `Docs/WEB_SESSIONS_PLAN.md`)
- 🚧 Rollouts (pendente)

**Arquitetura:**
- **Zero impacto** no TUI existente
- **Modo exclusivo**: TUI **ou** Web (não simultâneo)
- **Reutilização**: Toda lógica K8s/Azure compartilhada
- **Build único**: Frontend embedado no binário Go

### 📋 Sistema de Sessões (Planejado)

**Status**: Plano completo documentado em `Docs/WEB_SESSIONS_PLAN.md`

**Objetivo**: Sistema de save/load de sessões compatível 100% com TUI, permitindo:
- Salvar staging area (HPAs + Node Pools) em sessões nomeadas
- Carregar sessões salvas de volta para staging
- Sessões criadas no TUI funcionam na Web e vice-versa
- Templates de nomenclatura com variáveis: `{action}`, `{cluster}`, `{timestamp}`, etc.

**Estrutura de Diretórios**:
```
~/.k8s-hpa-manager/sessions/
├── HPA-Upscale/           # Sessões de upscale de HPAs
├── HPA-Downscale/         # Sessões de downscale de HPAs
├── Node-Upscale/          # Sessões de upscale de Node Pools
└── Node-Downscale/        # Sessões de downscale de Node Pools
```

**Componentes Planejados**:

**Backend**:
- `internal/web/handlers/sessions.go` - Handlers REST API
- Endpoints: GET/POST/DELETE `/api/v1/sessions`
- Reutiliza `internal/session/manager.go` (código TUI existente)

**Frontend**:
- `SessionContext.tsx` - Gerenciamento de estado de sessões
- `SaveSessionModal.tsx` - UI para salvar sessão atual
- `LoadSessionModal.tsx` - UI para carregar sessões existentes
- `sessionConverter.ts` - Conversão Staging ↔ Session JSON
- Integração com `StagingContext` existente

**Fluxo de Uso**:
1. Usuário edita HPAs/Node Pools → Staging area
2. Clica "Save Session" → SaveSessionModal abre
3. Escolhe pasta (HPA-Upscale/Downscale/Node-Upscale/Downscale)
4. Define nome usando template ou custom
5. Backend salva JSON em `~/.k8s-hpa-manager/sessions/{folder}/{name}.json`
6. Para carregar: LoadSessionModal lista sessões → Preview → Load → Staging area

**Compatibilidade TUI ↔ Web**:
- Mesmo formato JSON de sessão
- Mesma estrutura de diretórios
- SessionManager Go compartilhado
- Templates idênticos

**Ver documentação completa**: `Docs/WEB_SESSIONS_PLAN.md`

### 🐛 Correções Críticas da Interface Web (Outubro 2025)

#### 1. **Fix: Modal Enviando Objeto HPA Parcial (RESOLVIDO)**

**Problema:** Modal de confirmação enviava apenas as alterações (delta) ao backend, mas o handler esperava objeto HPA completo via `c.ShouldBindJSON(&hpa)`. Isso causava:
- Campos não editados ficavam vazios/null no backend
- `MaxReplicas:0` falhava na validação (`maxReplicas must be >= 1`)
- Alterações de Memory Limit falhavam mesmo sendo válidas

**Sintoma:**
```go
📝 Received HPA update: {Name: Namespace: Cluster: MinReplicas:<nil> MaxReplicas:0 ... TargetMemoryLimit:385Mi ...}
❌ Error: maxReplicas must be >= 1
```

**Causa Raiz:**
```typescript
// ❌ ANTES - Enviava apenas alterações
const updates: any = {};
if (current.min_replicas !== original.min_replicas) {
  updates.min_replicas = current.min_replicas;
}
// ... apenas campos modificados ...

await apiClient.updateHPA(cluster, namespace, name, updates);
// Backend recebia: {target_memory_limit: "385Mi"} ❌
```

**Solução Implementada:**
```typescript
// ✅ DEPOIS - Envia HPA completo
await apiClient.updateHPA(
  current.cluster,
  current.namespace,
  current.name,
  current  // Objeto HPA completo com todos os campos
);
// Backend recebia: {name: "nginx", namespace: "ingress-nginx", min_replicas: 2, max_replicas: 10, target_memory_limit: "385Mi", ...} ✅
```

**Arquivo Modificado:**
- `internal/web/frontend/src/components/ApplyAllModal.tsx:173-180`

**Resultado:** Todas as alterações de HPA (replicas, targets, resources) agora aplicam com sucesso! ✅

#### 2. **Fix: Page Reload Perdendo Estado da Aplicação (RESOLVIDO)**

**Problema:** Após aplicar alterações, `window.location.reload()` era executado, causando:
- Perda do cluster selecionado
- Retorno à tela de login
- Perda de contexto de navegação

**Solução:**
```typescript
// ❌ ANTES
const { hpas, loading, updateHPA } = useHPAs(selectedCluster);
window.location.reload(); // Perdia todo o estado

// ✅ DEPOIS
const { hpas, loading, refetch: refetchHPAs } = useHPAs(selectedCluster);
refetchHPAs(); // Atualiza apenas HPAs, preserva estado
```

**Arquivos Modificados:**
- `internal/web/frontend/src/pages/Index.tsx:42,269`
- `internal/web/frontend/src/components/ApplyAllModal.tsx:209,270`

#### 3. **Fix: Modal Mostrando Campos Não Alterados (RESOLVIDO)**

**Problema:** Modal exibia `"Target Memory (%): — → —"` para campos que não foram editados (null → null).

**Solução:**
```typescript
const renderChange = (label: string, before: any, after: any) => {
  // Normalizar null/undefined
  const normalizedBefore = before ?? null;
  const normalizedAfter = after ?? null;

  // Não exibir se ambos são null (sem alteração real)
  if (normalizedBefore === normalizedAfter) return null;

  // Não exibir se ambos são vazios (— → —)
  if ((normalizedBefore === null || normalizedBefore === "") &&
      (normalizedAfter === null || normalizedAfter === "")) {
    return null;
  }

  return (/* ... renderiza apenas mudanças reais ... */);
};
```

**Arquivo Modificado:**
- `internal/web/frontend/src/components/ApplyAllModal.tsx:221-243`

#### 4. **Feature: Backend Deployment Resource Updates (IMPLEMENTADO)**

**Funcionalidade:** Backend agora atualiza CPU/Memory Request/Limit no deployment associado ao HPA.

**Implementação:**
```go
// Atualizar resources do deployment se fornecidos
if hpa.TargetCPURequest != "" || hpa.TargetCPULimit != "" ||
   hpa.TargetMemoryRequest != "" || hpa.TargetMemoryLimit != "" {

    deployment, err := c.clientset.AppsV1().Deployments(hpa.Namespace).Get(...)
    if err != nil {
        return fmt.Errorf("failed to get deployment: %w", err)
    }

    container := &deployment.Spec.Template.Spec.Containers[0]

    // Parse e aplicar quantities (100m, 256Mi, 1Gi)
    if hpa.TargetCPURequest != "" {
        cpuRequest, err := resource.ParseQuantity(hpa.TargetCPURequest)
        container.Resources.Requests["cpu"] = cpuRequest
    }
    // ... CPU Limit, Memory Request, Memory Limit ...

    // Aplicar ao cluster
    _, err = c.clientset.AppsV1().Deployments(hpa.Namespace).Update(ctx, deployment, ...)
}
```

**Arquivo Modificado:**
- `internal/kubernetes/client.go:188-253`

#### 5. **Fix: Node Pool Editor e Cluster Name Matching (RESOLVIDO - Outubro 2025)**

**Problema:** Editor de Node Pools não aparecia ao clicar nos itens da lista. A API retornava erro "CLUSTER_NOT_FOUND" mesmo com clusters válidos.

**Causa Raiz:**
1. **Mismatch de nomes**: Frontend enviava `akspriv-lab-001-admin`, mas `clusters-config.json` não tinha esse cluster
2. **Função `findClusterInConfig()`**: Não fazia match correto entre contextos do kubeconfig (com `-admin`) e nomes no config file (sem `-admin`)

**Sintoma:**
```json
// API Request
GET /api/v1/nodepools?cluster=akspriv-lab-001-admin

// API Response
{
  "success": false,
  "error": {
    "code": "CLUSTER_NOT_FOUND",
    "message": "Cluster not found in clusters-config.json: cluster 'akspriv-lab-001-admin' not found"
  }
}
```

**Solução Implementada:**

**1. Corrigida lógica de matching em `findClusterInConfig()`:**
```go
// ✅ ANTES (incorreto)
for _, cluster := range clusters {
    if cluster.ClusterName == clusterContext {  // Não remove -admin
        return &cluster, nil
    }
}

// ✅ DEPOIS (correto)
func findClusterInConfig(clusterContext string) (*models.ClusterConfig, error) {
    // Remover -admin do contexto (kubeconfig contexts têm -admin, config file não)
    clusterNameWithoutAdmin := strings.TrimSuffix(clusterContext, "-admin")

    for _, cluster := range clusters {
        // Remover -admin do cluster name também para comparação
        configClusterName := strings.TrimSuffix(cluster.ClusterName, "-admin")

        // Comparar sem o sufixo -admin
        if configClusterName == clusterNameWithoutAdmin {
            return &cluster, nil
        }

        // Também comparar exatamente como está (fallback)
        if cluster.ClusterName == clusterContext {
            return &cluster, nil
        }
    }

    return nil, fmt.Errorf("cluster '%s' not found in clusters-config.json", clusterContext)
}
```

**2. Estrutura JSON correta:**
```json
// clusters-config.json (gerado por autodiscover)
[
  {
    "clusterName": "akspriv-faturamento-prd",  // ✅ sem -admin
    "resourceGroup": "rg-faturamento-app-prd",
    "subscription": "PRD - ONLINE 2"
  }
]

// models.ClusterConfig (Go struct)
type ClusterConfig struct {
    ClusterName   string `json:"clusterName"`   // ✅ camelCase matching JSON
    ResourceGroup string `json:"resourceGroup"`
    Subscription  string `json:"subscription"`
}
```

**Teste Bem-Sucedido:**
```bash
# Teste com cluster válido
curl -s -H 'Authorization: Bearer poc-token-123' \
  'http://localhost:8080/api/v1/nodepools?cluster=akspriv-faturamento-prd-admin' | jq '.'

# Resposta (sucesso)
{
  "success": true,
  "data": [
    {
      "name": "fatura",
      "cluster_name": "akspriv-faturamento-prd",
      "vm_size": "Standard_F4s_v2",
      "node_count": 1,
      "min_node_count": 1,
      "max_node_count": 3,
      "autoscaling_enabled": true,
      "status": "Succeeded",
      "is_system_pool": false
    }
  ],
  "count": 4
}
```

**Para o Editor Aparecer no Frontend:**
1. **Hard refresh** no browser: `Ctrl+Shift+R` para limpar cache JavaScript
2. **Selecionar cluster válido**: Use um cluster que existe em `~/.k8s-hpa-manager/clusters-config.json`
3. **Verificar clusters disponíveis**:
   ```bash
   cat ~/.k8s-hpa-manager/clusters-config.json | jq '.[].clusterName'
   ```
4. **Clicar em um node pool** da lista - o editor deve aparecer no painel direito

**Arquivos Modificados:**
- `internal/web/handlers/nodepools.go:256-282` - Função `findClusterInConfig()` corrigida
- `internal/models/types.go` - Struct `ClusterConfig` com tags JSON corretas (camelCase)

**Nota Importante:**
- O cluster `akspriv-lab-001-admin` da imagem do usuário **NÃO EXISTE** no `clusters-config.json` real
- Clusters disponíveis incluem: `akspriv-faturamento-prd`, `akspriv-abastecimento-prd`, `akspriv-tms-prd`, etc.
- Execute `k8s-hpa-manager autodiscover` se clusters estiverem faltando no config file

**Validação:**
- MinReplicas relaxada: `>= 0` (permite scale-to-zero)
- Debug logging adicionado em `hpas.go:164,175`

#### 5. **Feature: ApplyAllModal com Progress Tracking (IMPLEMENTADO)**

**Funcionalidades:**
- ✅ **Preview Mode** - Exibe before → after de todas alterações
- ✅ **Progress Mode** - Mostra aplicação sequencial com progress bars
- ✅ **Rollout Simulation** - Progress bars animadas (0-100%) para Deployment/DaemonSet/StatefulSet
- ✅ **Error Handling** - Erro individual por HPA sem bloquear outros
- ✅ **Auto-close** - Fecha modal em 2s após sucesso total

**Arquivos Criados/Modificados:**
- `internal/web/frontend/src/components/ApplyAllModal.tsx` (NOVO - 460 linhas)
- `internal/web/frontend/src/components/HPAEditor.tsx` (callback pattern)
- `internal/web/frontend/src/pages/Index.tsx` (integração modal)
- `internal/web/frontend/src/lib/api/types.ts` (tipos expandidos)

**Documentação:**
- `internal/web/frontend/README.md` - Frontend docs
- `Docs/README_WEB.md` - Web interface overview
- `Docs/WEB_INTERFACE_DESIGN.md` - Arquitetura completa

---

#### 6. **Feature: Dashboard com Métricas de Cluster (IMPLEMENTADO - Outubro 2025)**

**Objetivo:** Dashboard mostrando informações essenciais do cluster com gráficos gauge-style para CPU e memória.

**Problema Inicial:**
- Dashboard exibia erro "Failed to get cluster info"
- Frontend não conseguia acessar dados do backend
- Estrutura de resposta JSON incorreta no cliente API

**Solução Implementada:**

**1. Correção do Cliente API:**
```typescript
// ❌ ANTES - Estrutura incorreta
async getClusterInfo(): Promise<ClusterInfo> {
  const response = await this.request('/clusters/info', { method: 'GET' });
  return response.data.data; // ❌ Tentava acessar data.data
}

// ✅ DEPOIS - Estrutura correta
async getClusterInfo(): Promise<ClusterInfo> {
  const response = await this.request('/clusters/info', { method: 'GET' }) as { success: boolean; data: ClusterInfo };
  return response.data; // ✅ Acessa apenas data
}
```

**2. Melhorias na Interface:**
```typescript
// Labels corrigidos para refletir dados reais
<CircularMetric
  percentage={clusterInfo?.cpuUsagePercent || 0}
  label="CPU Requests"     // ✅ ANTES: "CPU Usage"
  icon={Cpu}
  color="text-blue-500"
/>
<CircularMetric
  percentage={clusterInfo?.memoryUsagePercent || 0}
  label="Memory Requests"  // ✅ ANTES: "Memory Usage"
  icon={HardDrive}
  color="text-green-500"
/>
```

**3. Limpeza do Dashboard:**
- ❌ **Removido:** Cards "HPAs por Namespace" e "Distribuição de Réplicas" (não faziam sentido)
- ✅ **Mantido:** "Informações do Cluster" e "Alocação de Recursos"

**Features do Dashboard:**
- ✅ **Informações do Cluster:** Nome, contexto, versão K8s, namespace, contadores (nodes/pods)
- ✅ **Gráficos Gauge:** CPU e memória com percentuais circulares animados
- ✅ **Layout Responsivo:** Grid 2 colunas, design limpo
- ✅ **Auto-refresh:** Atualização a cada 30 segundos
- ✅ **Error Handling:** Botão "Tentar novamente" em caso de erro

**Esclarecimento sobre Métricas:**
- **CPU/Memory %** = Alocação de recursos via `requests` dos containers
- **NÃO é uso real** - para métricas reais seria necessário Metrics Server ou Prometheus
- Títulos alterados para "Alocação de Recursos" para evitar confusão

**Arquivos Modificados:**
- `internal/web/frontend/src/lib/api/client.ts:85-87` - Fix estrutura response
- `internal/web/frontend/src/components/DashboardCharts.tsx:194-210` - Labels e layout
- `internal/config/kubeconfig.go:601` - Comentário sobre fonte dos dados

**Resultado:** Dashboard funcional exibindo informações reais do cluster com gráficos gauge profissionais! ✅

#### 7. **Feature: Dashboard Redesign com MetricsGauge (IMPLEMENTADO - Outubro 2025)**

**Objetivo:** Redesign completo do dashboard para um estilo mais moderno e profissional com layout em grid 2x2.

**Problema:** O dashboard anterior tinha um layout básico que não aproveitava bem o espaço e não tinha uma aparência profissional.

**Solução Implementada:**

**1. Novo Componente MetricsGauge:**
```typescript
// Componente reutilizável para métricas com gauge circular + barra de progresso
interface MetricsGaugeProps {
  icon: LucideIcon;
  label: string;
  value: number;
  unit?: string;
  maxValue?: number;
  warningThreshold?: number;
  dangerThreshold?: number;
}

// Features:
- Gráfico circular SVG customizado
- Barra de progresso inferior (shadcn/ui Progress)
- Cores dinâmicas baseadas em thresholds
- Animações suaves (stroke-dashoffset)
- Status visual (success/warning/destructive)
```

**2. Layout Grid 2x2 Moderno:**
```typescript
// Dashboard com 4 cards principais em grid responsivo
<div className="grid grid-cols-1 md:grid-cols-2 gap-6 p-6">
  <MetricsGauge 
    icon={Cpu} 
    label="CPU Usage" 
    value={cpuUsagePercent} 
    warningThreshold={70} 
    dangerThreshold={90} 
  />
  <MetricsGauge 
    icon={HardDrive} 
    label="Memory Usage" 
    value={memoryUsagePercent} 
    warningThreshold={70} 
    dangerThreshold={90} 
  />
  <MetricsGauge 
    icon={Activity} 
    label="CPU Usage Over Time" 
    value={0} 
    // Placeholder para funcionalidade futura
  />
  <MetricsGauge 
    icon={Database} 
    label="Memory Usage Over Time" 
    value={0} 
    // Placeholder para funcionalidade futura
  />
</div>
```

**3. Melhorias Visuais:**
- **Gauge Circular Responsivo:** SVG que se adapta ao container
- **Barra de Progresso:** Indicador visual adicional na base do card
- **Cores Inteligentes:** 
  - 🟢 Verde (0-69%): Normal
  - 🟡 Amarelo (70-89%): Warning  
  - 🔴 Vermelho (90%+): Danger
- **Animações Suaves:** Transições de 0.8s para mudanças de valores
- **Cards Uniformes:** Altura e layout consistentes

**4. Sistema de Threshold Configurável:**
```typescript
// Thresholds customizáveis por métrica
const cpuThresholds = { warning: 70, danger: 90 };
const memoryThresholds = { warning: 80, danger: 95 };
```

**5. Placeholder Cards para Expansão Futura:**
- **CPU Usage Over Time:** Gráfico de linha temporal
- **Memory Usage Over Time:** Gráfico de linha temporal  
- **HPAs by Namespace:** Distribuição por namespace
- **Replica Distribution:** Distribuição de réplicas

**Features Implementadas:**
- ✅ **Layout Grid 2x2** responsivo (1 col mobile, 2 cols desktop)
- ✅ **Componente MetricsGauge** reutilizável
- ✅ **Gauge Circular** com animação de progresso
- ✅ **Barra de Progresso** inferior para reforço visual
- ✅ **Sistema de Cores** baseado em thresholds configuráveis
- ✅ **Integração com shadcn/ui** (Progress, Card, etc.)
- ✅ **Métricas Reais** do cluster selecionado
- ✅ **Placeholder Cards** para funcionalidades futuras

**Arquivos Criados/Modificados:**
- `internal/web/frontend/src/components/MetricsGauge.tsx` (NOVO - 89 linhas)
- `internal/web/frontend/src/components/DashboardCharts.tsx` (redesign completo)

**Resultado:** Dashboard moderno estilo enterprise com layout profissional em grid 2x2! ✅

---

**Happy coding!** 🚀

---

## 📌 Lembrete Final

**Sempre compile o build em ./build/** - `make build` → `./build/k8s-hpa-manager`

**Para continuar POC web:** Leia `Docs/README_WEB.md` ou execute `./QUICK_START_WEB.sh`

# CLAUDE.md - Sessão de Desenvolvimento Web Interface

## Data: 18 de Outubro de 2025
## Objetivo: Implementar sistema de sessões completo na interface web

---

## 🚨 PROBLEMA INICIAL IDENTIFICADO

### Sintomas Reportados pelo Usuário:
1. **Tela branca** ao selecionar node pools no editor
2. **Alterações não sendo salvas** no staging quando editava HPAs
3. **Sistema de sessões não funcional** - sem interface visual para Save/Load
4. **Sessões não sendo salvas** no local correto (`~/.k8s-hpa-manager/sessions/<pasta>`)
5. **Load não reconhecia** arquivos existentes nas pastas de sessão

---

## 🔧 DIAGNÓSTICO E CORREÇÕES REALIZADAS

### 1. **PROBLEMA: Tela Branca no NodePoolEditor**

**Causa Raiz:**
```typescript
// ❌ ERRO: Métodos inexistentes no StagingContext
const stagedPool = staging.getNodePool(key);  // Não existe
staging.addNodePool(modifiedNodePool, nodePool, order);  // Assinatura errada
```

**Solução Aplicada:**
```typescript
// ✅ CORRETO: Usar métodos reais do StagingContext
const stagedPool = staging.stagedNodePools.find(
  np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
);

staging.addNodePoolToStaging(nodePool);
staging.updateNodePoolInStaging(nodePool.cluster_name, nodePool.name, updates);
```

**Arquivos Modificados:**
- `internal/web/frontend/src/components/NodePoolEditor.tsx`

---

### 2. **PROBLEMA: HPAEditor Não Salvava no Staging**

**Causa Raiz:**
```typescript
// ❌ ERRO: Método inexistente
staging.add(modifiedHPA, hpa);  // Método não existe no contexto
```

**Solução Aplicada:**
```typescript
// ✅ CORRETO: Fluxo correto do staging
staging.addHPAToStaging(hpa);  // Adicionar primeiro
staging.updateHPAInStaging(hpa.cluster, hpa.namespace, hpa.name, updates);  // Depois atualizar
```

**Arquivos Modificados:**
- `internal/web/frontend/src/components/HPAEditor.tsx`

---

### 3. **PROBLEMA: Index.tsx com Métodos Inexistentes**

**Causa Raiz:**
```typescript
// ❌ ERRO: Propriedades que não existiam
modifiedCount={staging.count + staging.nodePoolCount}  // Não existe
staging.getAll()  // Não existe
staging.getAllNodePools()  // Não existe
```

**Solução Aplicada:**
```typescript
// ✅ CORRETO: Usar API correta do StagingContext
modifiedCount={staging.getChangesCount().total}

const modifiedHPAs = staging.stagedHPAs
  .filter(hpa => hpa.isModified)
  .map(hpa => ({
    key: `${hpa.cluster}/${hpa.namespace}/${hpa.name}`,
    current: hpa,
    original: { ...hpa, ...hpa.originalValues } as HPA,
  }));
```

**Arquivos Modificados:**
- `internal/web/frontend/src/pages/Index.tsx`
