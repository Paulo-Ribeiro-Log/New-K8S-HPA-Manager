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
4. [Interface Web](#-interface-web-reacttypescript)
5. [Common Pitfalls](#%EF%B8%8F-common-pitfalls--gotchas)
6. [Testing Strategy](#-testing-strategy)
7. [Troubleshooting](#-troubleshooting)
8. [Continuing Development](#-continuing-development)
9. [Histórico de Correções](#-histórico-de-correções-principais)

---

## 🚀 Quick Start Para Novos Chats

### Project Summary
**Terminal-based Kubernetes HPA and Azure AKS Node Pool management tool** built with Go and Bubble Tea TUI framework. Features async rollout progress tracking with Rich Python-style progress bars, integrated status panel, session management, and unified HPA/node pool operations.

**NOVO (Outubro 2025)**: Interface web completa (React/TypeScript) com compatibilidade 100% TUI para sessões.

### Estado Atual (Outubro 2025)

**Versão Atual:** v1.2.0 (Release: 23 de outubro de 2025)
**GitHub Release:** https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0

**TUI (Terminal Interface):**
- ✅ Interface responsiva (adapta-se ao tamanho real do terminal - mínimo 80x24)
- ✅ Execução sequencial de node pools para stress tests (F12)
- ✅ Rollouts detalhados de HPA (Deployment/DaemonSet/StatefulSet)
- ✅ CronJob management (F9) e Prometheus Stack (F8)
- ✅ Status container compacto (80x10) com progress bars Rich Python
- ✅ Auto-descoberta de clusters via `k8s-hpa-manager autodiscover`
- ✅ Validação VPN on-demand (verifica conectividade K8s antes de operações críticas)
- ✅ Modais de confirmação (Ctrl+D/Ctrl+U exigem confirmação)
- ✅ Log detalhado de alterações (antes → depois) no StatusContainer
- ✅ Sistema de Logs completo (F3) - visualizador com scroll, copiar, limpar
- ✅ Race condition corrigida (Mutex RWLock para testes paralelos de cluster)
- ✅ **Sistema de updates automático** - Detecção 1x por dia com notificação

**Web Interface:**
- ✅ Interface web completa (99% funcional)
- ✅ HPAs, Node Pools, CronJobs e Prometheus Stack implementados
- ✅ Dashboard redesignado com layout moderno grid 2x2 e métricas reais
- ✅ Sistema de sessões completo (save/load/rename/delete/edit)
- ✅ Staging area com preview de alterações
- ✅ Snapshot de cluster para rollback
- ✅ Sistema de heartbeat e auto-shutdown (20min inatividade)
- ✅ ApplyAllModal com progress tracking e rollout simulation
- ✅ **Rollout individual para Prometheus Stack** (Deployment/StatefulSet/DaemonSet) - Outubro 2025
- ✅ **Aplicar Agora para Node Pools** - Aplicação individual sem staging - Outubro 2025

### Tech Stack
- **Language**: Go 1.23+ (toolchain 1.24.7)
- **TUI Framework**: Bubble Tea v0.24.2 + Lipgloss v1.1.0
- **K8s Client**: client-go v0.31.4 (official)
- **Azure SDK**: azcore v1.19.1, azidentity v1.12.0
- **Web Frontend**: React 18.3 + TypeScript 5.8 + Vite 5.4
- **Web UI**: shadcn/ui (Radix UI) + Tailwind CSS 3.4
- **Architecture**: MVC pattern com state-driven UI

---

## 🔧 Development Commands

### Terminal Requirements (TUI)

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

### Installation and Updates

```bash
# Instalação completa em 1 comando (clone + build + install)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# O que faz:
# - Clona repositório
# - Compila com injeção de versão
# - Instala em /usr/local/bin/
# - Copia scripts utilitários para ~/.k8s-hpa-manager/scripts/
# - Cria atalho k8s-hpa-web

# Sistema de updates automático
k8s-hpa-manager version       # Verificar versão e updates disponíveis
~/.k8s-hpa-manager/scripts/auto-update.sh             # Auto-update interativo
~/.k8s-hpa-manager/scripts/auto-update.sh --yes       # Auto-update sem confirmação
~/.k8s-hpa-manager/scripts/auto-update.sh --check     # Apenas verificar
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run   # Simular

# Scripts utilitários instalados
k8s-hpa-web start/stop/status/logs/restart            # Gerenciar servidor web
~/.k8s-hpa-manager/scripts/uninstall.sh              # Desinstalar
~/.k8s-hpa-manager/scripts/backup.sh                 # Backup (dev)
~/.k8s-hpa-manager/scripts/restore.sh                # Restore (dev)
```

📚 **Documentação:**
- `INSTALL_GUIDE.md` - Guia completo de instalação
- `UPDATE_BEHAVIOR.md` - Como funciona o sistema de updates
- `AUTO_UPDATE_EXAMPLES.md` - Exemplos de uso do auto-update

### Building and Running (TUI)

```bash
# Build TUI
make build                    # Build to ./build/k8s-hpa-manager (version auto-detected)
make build-all                # Build for multiple platforms (Linux, macOS, Windows)
make run                      # Build and run
make run-dev                  # Run with debug logging (go run . --debug)
make version                  # Show detected version from git tags
make release                  # Build for all platforms (Linux, macOS amd64/arm64, Windows)
```

### Building and Running (Web Interface)

```bash
# Frontend development
make web-install              # Install frontend dependencies (npm install)
make web-dev                  # Start Vite dev server (port 5173)
                              # Backend: ./build/k8s-hpa-manager web --port 8080 (terminal 2)

# Production build
make web-build                # Build frontend → internal/web/static/
make build-web                # Build completo (frontend + Go binary com embed)

# Run web server
./build/k8s-hpa-manager web              # Background mode (default)
./build/k8s-hpa-manager web -f           # Foreground mode
./build/k8s-hpa-manager web --port 8080  # Custom port

# IMPORTANTE: Rebuild obrigatório
./rebuild-web.sh -b           # Script recomendado (evita cache issues)
```

### Testing

```bash
make test                     # Run all tests with verbose output
make test-coverage            # Run tests with coverage (generates coverage.html)
```

### Safe Deploy (Deploy Seguro)

**Script automatizado para deploy seguro de dev2 → main com validações completas:**

```bash
./safe-deploy.sh              # Deploy completo (interativo com confirmações)
./safe-deploy.sh --dry-run    # Simular deploy sem executar (teste)
./safe-deploy.sh --yes        # Deploy automático sem confirmações
./safe-deploy.sh --skip-tests # Pular execução de testes (não recomendado)
./safe-deploy.sh --skip-build # Pular build (não recomendado)
./safe-deploy.sh --help       # Ver todas as opções
```

**O que o script faz:**
1. ✅ **Validações iniciais**: Working tree limpo, branches existem
2. ✅ **Testes**: Executa `make test` (pode pular com --skip-tests)
3. ✅ **Build**: Compila TUI e Web (pode pular com --skip-build)
4. ✅ **Backup**: Cria branch de backup automático (backup-TIMESTAMP-pre-deploy)
5. ✅ **Merge**: dev2 → main com detecção de conflitos
6. ✅ **Sync**: Rebase com origin/main
7. ✅ **Tags**: Opção de atualizar tags (ex: v1.2.0)
8. ✅ **Push**: Branch main e tags para GitHub
9. ✅ **Sync dev2**: Opção de sincronizar dev2 com main após deploy

**Workflow recomendado:**
```bash
# 1. Testar primeiro (dry-run)
./safe-deploy.sh --dry-run

# 2. Deploy real após validar
./safe-deploy.sh

# 3. Ou deploy automático (CI/CD)
./safe-deploy.sh --yes
```

**Vantagens:**
- 🛡️ Previne quebra da branch main
- 🔄 Backup automático antes de qualquer alteração
- ✅ Validações completas (testes, build, working tree)
- 📊 Resumo claro do que será feito
- 🎯 Modo dry-run para testes seguros

**Nota:** O script `safe-deploy.sh` está no `.gitignore` e não é versionado (uso local apenas).

### Installation

```bash
./install.sh                  # Automated installer → /usr/local/bin/
./uninstall.sh                # Uninstaller (optionally removes session data)

# After installation
k8s-hpa-manager               # Run TUI from anywhere
k8s-hpa-manager web           # Run web interface
k8s-hpa-manager --debug       # Debug mode
k8s-hpa-manager --help        # Show help
```

### Cluster Auto-Discovery

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
3. Node Pools prontos para uso (TUI e Web)

### Backup and Restore

```bash
./backup.sh "descrição"       # Criar backup antes de modificações
./restore.sh                  # Listar backups disponíveis
./restore.sh backup_name      # Restaurar backup específico
```
- Mantém os 10 backups mais recentes automaticamente
- Metadados inclusos (git commit, data, usuário)

---

## 🏗️ Architecture Overview

### Estrutura de Diretórios

```
k8s-hpa-manager/
├── cmd/
│   ├── root.go                    # CLI entry point & commands (Cobra)
│   ├── web.go                     # Web server command
│   ├── version.go                 # Version command
│   ├── autodiscover.go            # Cluster auto-discovery
│   └── k8s-teste/                 # Layout test command
├── internal/
│   ├── tui/                       # Terminal UI (Bubble Tea)
│   │   ├── app.go                 # Main orchestrator + centralized text methods
│   │   ├── handlers.go            # Event handlers
│   │   ├── views.go               # UI rendering & layout
│   │   ├── message.go             # Bubble Tea messages
│   │   ├── text_input.go          # Centralized text input with intelligent cursor
│   │   ├── resource_*.go          # HPA/Node Pool resource management
│   │   ├── cronjob_*.go           # CronJob management (F9)
│   │   ├── components/            # UI components
│   │   │   ├── status_container.go
│   │   │   └── unified_container.go
│   │   └── layout/                # Layout managers
│   │       ├── manager.go
│   │       ├── screen.go
│   │       ├── panels.go
│   │       └── constants.go
│   ├── web/                       # Web Interface (React/TypeScript)
│   │   ├── frontend/              # React SPA
│   │   │   ├── src/
│   │   │   │   ├── components/    # UI components (shadcn/ui)
│   │   │   │   ├── contexts/      # StagingContext, TabContext
│   │   │   │   ├── hooks/         # useHeartbeat, custom hooks
│   │   │   │   ├── lib/           # API client, utilities
│   │   │   │   └── pages/         # Index, CronJobs, Prometheus
│   │   │   ├── package.json
│   │   │   └── vite.config.ts
│   │   ├── handlers/              # Go REST API handlers
│   │   │   ├── hpas.go           # HPA CRUD
│   │   │   ├── nodepools.go      # Node Pool management
│   │   │   ├── sessions.go       # Session save/load/rename/delete/edit
│   │   │   ├── cronjobs.go       # CronJob management
│   │   │   └── prometheus.go     # Prometheus Stack
│   │   ├── middleware/
│   │   │   └── auth.go           # Bearer token auth
│   │   ├── static/               # Build output (embedado no Go binary)
│   │   └── server.go             # Gin HTTP server com heartbeat/auto-shutdown
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
│   ├── updater/                   # Versioning system
│   │   ├── version.go
│   │   ├── github.go
│   │   └── checker.go
│   └── ui/                        # UI utilities
│       ├── progress.go
│       ├── logs.go
│       └── status_panel.go
├── build/                         # Build artifacts
├── backups/                       # Code backups (via backup.sh)
├── Docs/                          # Documentation (web POC, plans, fixes)
├── go.mod & go.sum
├── makefile
├── rebuild-web.sh                 # Web rebuild script (recomendado)
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

**Web Layer** (`internal/web/`):
- `server.go` - Gin HTTP server com heartbeat e auto-shutdown
- `handlers/` - REST API endpoints (HPAs, Node Pools, Sessions, CronJobs, Prometheus)
- `middleware/auth.go` - Bearer token authentication
- `frontend/` - React/TypeScript SPA com shadcn/ui

**Business Logic** (`internal/`):
- `kubernetes/client.go` - K8s client wrapper with per-cluster management
- `config/kubeconfig.go` - Kubeconfig discovery (akspriv-* pattern) **+ Mutex RWLock**
- `session/manager.go` - Session persistence with template naming (compatível TUI ↔ Web)
- `models/types.go` - Complete domain model and app state (AppModel)
- `azure/auth.go` - Azure SDK auth with browser/device code fallback

**Entry Points**:
- `main.go` - Application bootstrap
- `cmd/root.go` - Cobra CLI commands and flags (TUI)
- `cmd/web.go` - Web server command (background/foreground modes)

### Data Flow

1. **State-Driven Architecture**: `AppModel` in `models/types.go` maintains complete app state
2. **State Transitions**: `AppState` enum manages flow:
   - Cluster Selection → Session Selection → Namespace Selection → HPA/Node Pool Management → Editing → Help
3. **Multi-Selection Flow**: One Cluster → Multiple Namespaces → Multiple HPAs/Node Pools → Individual Editing
4. **Bubble Tea Messages**: Coordinate between UI interactions and business logic (TUI)
5. **React Query + Context**: State management na web interface
6. **Client Management**: Per-cluster Kubernetes client instances (thread-safe via RWLock)
7. **Session System**: Preserves state for review/editing before application (TUI e Web compartilham formato)

### Dependencies

**Core Framework**:
- Bubble Tea v0.24.2 - TUI framework
- Lipgloss v1.1.0 - Styling and layout
- Cobra v1.10.1 - CLI commands
- Gin v1.11.0 - HTTP server (web)

**Kubernetes**:
- client-go v0.31.4 - Official K8s Go client

**Azure**:
- azcore v1.19.1 - Core SDK
- azidentity v1.12.0 - Authentication
- Azure CLI - External dependency for node pool operations

**Web Frontend**:
- React 18.3 + TypeScript 5.8
- Vite 5.4 - Build tool com HMR
- shadcn/ui - UI components (Radix UI primitives)
- Tailwind CSS 3.4 - Styling
- React Query (TanStack) - Server state management
- React Router DOM - Client-side routing

---

## 🌐 Interface Web (React/TypeScript)

### Quick Start Web

```bash
# Development (2 terminais)
make web-install                              # Terminal 1: Install dependencies
make web-dev                                  # Terminal 1: Vite dev server (port 5173)
./build/k8s-hpa-manager web --port 8080       # Terminal 2: Backend API

# Production Build
make build-web                                # Build frontend + Go binary (embeds static/)
./build/k8s-hpa-manager web                   # Run integrated server (background mode)

# Background vs Foreground
./build/k8s-hpa-manager web                   # Background (default) - daemon mode
./build/k8s-hpa-manager web -f                # Foreground - logs no terminal
# Auto-shutdown: 20 min após última página fechar (sistema de heartbeat)
```

### Tech Stack Frontend

| Tecnologia | Versão | Uso |
|------------|--------|-----|
| **React** | 18.3 | UI framework |
| **TypeScript** | 5.8 | Type safety |
| **Vite** | 5.4 | Build tool (HMR rápido) |
| **shadcn/ui** | Latest | UI components (Radix UI) |
| **Tailwind CSS** | 3.4 | Styling |
| **React Query** | TanStack | Server state management |
| **React Router** | DOM | Client-side routing |
| **Lucide React** | Latest | Icons |
| **Recharts** | Latest | Charts (Dashboard) |

### Sistema de Heartbeat e Auto-Shutdown

**Problema resolvido:** Servidor web rodando em background consome recursos indefinidamente mesmo sem uso.

**Solução:**
- **Frontend**: Hook `useHeartbeat` envia POST `/heartbeat` a cada 5 minutos
- **Backend**: Reseta timer de 20 minutos ao receber heartbeat
- **Auto-shutdown**: Servidor desliga automaticamente se nenhuma página conectada por 20min
- **Thread-safe**: `sync.RWMutex` protege timestamp de heartbeat

**Implementação:**

```typescript
// Frontend: hooks/useHeartbeat.ts
useEffect(() => {
  const sendHeartbeat = async () => {
    await fetch('/heartbeat', { method: 'POST' });
  };

  sendHeartbeat(); // Imediato ao montar
  const interval = setInterval(sendHeartbeat, 5 * 60 * 1000); // 5 min

  return () => clearInterval(interval);
}, []);
```

```go
// Backend: internal/web/server.go
func (s *Server) startInactivityMonitor() {
    s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)
}

func (s *Server) handleHeartbeat(c *gin.Context) {
    s.heartbeatMutex.Lock()
    s.lastHeartbeat = time.Now()
    s.heartbeatMutex.Unlock()

    if s.shutdownTimer != nil {
        s.shutdownTimer.Stop()
    }
    s.shutdownTimer = time.AfterFunc(20*time.Minute, s.autoShutdown)
}
```

### Features Implementadas Web

| Feature | Status | Descrição |
|---------|--------|-----------|
| **HPAs** | ✅ 100% | CRUD completo com edição de recursos (CPU/Memory Request/Limit) + Aplicar Agora |
| **Node Pools** | ✅ 100% | Editor funcional (autoscaling, node count, min/max) + **Botão "Aplicar Agora"** |
| **CronJobs** | ✅ 100% | Suspend/Resume |
| **Prometheus Stack** | ✅ 100% | Resource management + **Rollout individual (Deployment/StatefulSet/DaemonSet)** |
| **Sessions** | ✅ 100% | Save/Load/Rename/Delete/Edit (compatível TUI) |
| **Staging Area** | ✅ 100% | Preview de alterações antes de aplicar |
| **ApplyAllModal** | ✅ 100% | Progress tracking com rollout simulation |
| **Dashboard** | ✅ 100% | Grid 2x2 com métricas reais (CPU/Memory allocation) |
| **Snapshot Cluster** | ✅ 100% | Captura estado atual para rollback |
| **Heartbeat System** | ✅ 100% | Auto-shutdown em 20min inatividade |

### Workflow Session Management (Web)

```
1. Editar HPAs/Node Pools → Staging Area (mudanças pendentes em memória)
2. "Save Session" → Modal com folders (HPA-Upscale/Downscale/Node-Upscale/Downscale)
3. Templates de nomenclatura: {action}_{cluster}_{timestamp}_{env}
4. "Load Session" → Grid de sessões com dropdown menu (⋮)
5. Dropdown actions:
   - Load: Carrega para Staging Area
   - Rename: Altera nome da sessão
   - Edit Content: EditSessionModal (edita HPAs/Node Pools salvos)
   - Delete: Remove sessão (com confirmação)
6. "Apply Changes" → ApplyAllModal com preview before/after
7. Progress tracking: Rollout simulation com progress bars
```

### Snapshot de Cluster para Rollback

**Feature NOVA (Outubro 2025):**
- Captura estado atual do cluster (TODOS os HPAs + Node Pools)
- Salva como sessão sem modificações (original_values = new_values)
- Permite rollback completo em caso de incident

**Workflow:**
```
1. Selecionar cluster
2. "Save Session" → Detecta staging vazio
3. Modal oferece "Capturar Snapshot do Cluster"
4. Backend busca dados FRESCOS via API K8s/Azure (não usa cache)
5. Salva em folder "Rollback" ou custom
6. Para restaurar: Load session → Apply
```

### Rebuild Web Obrigatório

**IMPORTANTE**: Sempre use o script recomendado para rebuilds web:

```bash
./rebuild-web.sh -b           # Build completo (frontend + backend)
```

**Por que não usar `make build` direto:**
- Cache do Vite pode causar stale files
- Static files podem não embedar corretamente
- Frontend e backend precisam sincronizar versões

**Após rebuild:**
1. Hard refresh no browser: `Ctrl+Shift+R`
2. Verificar logs: `/tmp/k8s-hpa-manager-web-*.log` (modo background)

### API Endpoints

**Base URL**: `http://localhost:8080/api/v1`

**Autenticação**: Bearer token no header `Authorization: Bearer poc-token-123`

| Endpoint | Method | Descrição |
|----------|--------|-----------|
| `/clusters` | GET | Lista clusters disponíveis |
| `/namespaces?cluster=X` | GET | Lista namespaces do cluster |
| `/hpas?cluster=X&namespace=Y` | GET | Lista HPAs |
| `/hpas/:cluster/:namespace/:name` | PUT | Atualiza HPA |
| `/nodepools?cluster=X` | GET | Lista node pools |
| `/nodepools/:cluster/:rg/:name` | PUT | Atualiza node pool |
| `/sessions` | GET | Lista sessões salvas |
| `/sessions` | POST | Salva nova sessão |
| `/sessions/:name` | DELETE | Remove sessão |
| `/sessions/:name/rename` | PUT | Renomeia sessão |
| `/sessions/:name` | PUT | Atualiza conteúdo da sessão |
| `/cronjobs?cluster=X&namespace=Y` | GET | Lista CronJobs |
| `/prometheus?cluster=X` | GET | Lista recursos Prometheus |
| `/prometheus/:cluster/:namespace/:type/:name/rollout` | POST | **Rollout de recurso Prometheus (deployment/statefulset/daemonset)** |
| `/heartbeat` | POST | Heartbeat (mantém servidor vivo) |

---

## ⚠️ Common Pitfalls / Gotchas

### Web Development

1. **SEMPRE usar `./rebuild-web.sh -b`** para builds web
   - ❌ NÃO: `npm run build && make build` (pode causar cache issues)
   - ✅ SIM: `./rebuild-web.sh -b`

2. **Hard refresh obrigatório** após rebuild
   - `Ctrl+Shift+R` no browser para limpar cache JavaScript

3. **TabProvider obrigatório** no App.tsx
   - Deve envolver `StagingProvider` e outros contexts
   - Erro sem TabProvider: "useTabManager must be used within a TabProvider"

4. **Cluster name suffix mismatch**
   - Sessions salvam sem `-admin` (ex: `akspriv-prod`)
   - Kubeconfig contexts têm `-admin` (ex: `akspriv-prod-admin`)
   - **Fix**: `StagingContext.loadFromSession()` adiciona `-admin` automaticamente
   - **Fix**: `findClusterInConfig()` remove `-admin` para matching

5. **Staging context patterns**
   - ❌ NÃO existe: `staging.add()`, `staging.getNodePool()`
   - ✅ Usar: `staging.addHPAToStaging()`, `staging.stagedNodePools.find()`

6. **Background mode logs**
   - Logs salvos em `/tmp/k8s-hpa-manager-web-*.log`
   - Use `tail -f /tmp/k8s-hpa-manager-web-*.log` para debug

### TUI Development

1. **Sempre usar `[]rune` para texto** (Unicode-safe)
   ```go
   // ❌ ERRADO
   text := "Hello"
   text[0] = 'h' // Não funciona com emojis

   // ✅ CORRETO
   runes := []rune("Hello 👋")
   runes[0] = 'h'
   text = string(runes)
   ```

2. **ESC deve preservar contexto**
   - Usar `handleEscape()` centralizado em `handlers.go`
   - NUNCA fazer `return tea.Quit` direto no ESC
   - Exemplo: F9 (CronJobs) → ESC → volta para Namespaces (preserva seleções)

3. **Estado sempre em AppModel**
   - `internal/models/types.go` é a ÚNICA fonte de verdade
   - NUNCA criar estado local em handlers ou views
   - Bubble Tea messages para comunicação assíncrona

4. **Bubble Tea messages para async**
   - NUNCA usar goroutines diretas para operações K8s/Azure
   - Sempre retornar `tea.Cmd` que envia mensagem quando completo
   ```go
   // ❌ ERRADO
   go func() {
       applyHPA() // Race condition!
   }()

   // ✅ CORRETO
   return func() tea.Msg {
       err := applyHPA()
       return HPAAppliedMsg{err: err}
   }
   ```

5. **Mutex para concorrência**
   - `clientMutex` em `getClient()` - protege criação de K8s clients
   - `heartbeatMutex` em web server - protege timestamp
   - Double-check locking pattern para performance

### Azure CLI

1. **Warnings não são erros**
   - `pkg_resources deprecated` → ignorar
   - `isOnlyWarnings()` em `executeAzureCommand()` separa stderr real de warnings

2. **Scale com autoscaling habilitado**
   - Azure CLI rejeita `scale` se autoscaling enabled
   - **Ordem correta**: Disable autoscaling → Scale → Enable autoscaling
   - Ver `buildNodePoolCommands()` em `app.go` para lógica de 4 cenários

3. **Timeout de 5 segundos**
   - Validação Azure com timeout evita travamento em problemas de rede/DNS
   - Ver `configurateSubscription()` em `cmd/root.go`

### Session System

1. **Folders obrigatórios**
   - Save/Load/Delete/Rename requerem `folder` parameter (query string na API)
   - Folders: `HPA-Upscale`, `HPA-Downscale`, `Node-Upscale`, `Node-Downscale`, `Mixed`

2. **Metadata auto-calculada**
   - NÃO editar manualmente campos `clusters_affected`, `namespaces_affected`
   - Backend recalcula automaticamente ao salvar/atualizar sessão

3. **Compatibilidade TUI ↔ Web**
   - Mesmo formato JSON
   - Mesma estrutura de diretórios (`~/.k8s-hpa-manager/sessions/`)
   - `SessionManager` Go compartilhado por ambos

### Race Conditions Conhecidas (RESOLVIDAS)

1. **getClient() race condition** ✅ RESOLVIDO
   - Múltiplos goroutines criavam clients simultaneamente
   - **Fix**: `sync.RWMutex` com double-check locking
   - Ver `internal/config/kubeconfig.go`

2. **testClusterConnections() race** ✅ RESOLVIDO
   - `tea.Batch()` iniciava todos testes em paralelo
   - **Fix**: Mutex protege criação de clients (read lock para leituras, write lock para criação)

---

## 🧪 Testing Strategy

### Unit Tests

```bash
make test                     # Run all tests
make test-coverage            # Coverage report → coverage.html
```

### Manual Testing Web

**Pre-requisitos:**
1. Build obrigatório: `./rebuild-web.sh -b`
2. Hard refresh no browser: `Ctrl+Shift+R`

**Checklist:**
- [ ] HPAs: Load, Edit (min/max replicas, targets, resources), Apply
- [ ] Node Pools: Load, Edit (count, autoscaling, min/max), Apply
- [ ] Sessions: Save, Load, Rename, Delete, Edit Content
- [ ] Staging Area: Add items, Clear, Apply, Cancel
- [ ] ApplyAllModal: Preview changes, Apply, Progress tracking
- [ ] Heartbeat: Abrir tab → fechar → servidor desliga em 20min
- [ ] Snapshot: Capturar estado do cluster para rollback
- [ ] Dashboard: Métricas reais (CPU/Memory allocation)

**Logs:**
```bash
# Modo background
tail -f /tmp/k8s-hpa-manager-web-*.log

# Modo foreground
./build/k8s-hpa-manager web -f --debug
```

### Manual Testing TUI

```bash
make run-dev                              # Debug mode
./build/k8s-hpa-manager --demo            # Demo mode (sem executar)
./build/k8s-hpa-manager --debug           # Debug logging
```

**Checklist:**
- [ ] Cluster discovery e conexão (F5 para reload)
- [ ] Multi-namespace selection (Space para selecionar múltiplos)
- [ ] HPA batch operations (Ctrl+U para aplicar todos)
- [ ] Node Pool sequential execution (F12 para marcar *1 e *2)
- [ ] Session save/load (Ctrl+S/Ctrl+L)
- [ ] VPN validation (mensagens em operações críticas)
- [ ] CronJob management (F9)
- [ ] Prometheus Stack (F8)
- [ ] Log viewer (F3)
- [ ] Modais de confirmação (Ctrl+D/Ctrl+U)

### Testing VPN Validation

**Simular VPN desconectada:**
```bash
# Desconectar VPN
sudo ifconfig <vpn-interface> down

# Iniciar aplicação
./build/k8s-hpa-manager

# Esperado:
# 🔍 Validando conectividade VPN...
# ❌ VPN desconectada - Kubernetes inacessível
# 💡 SOLUÇÃO: Conecte-se à VPN e tente novamente (F5)
```

### Testing Auto-Shutdown (Web)

```bash
# Iniciar servidor em foreground para ver logs
./build/k8s-hpa-manager web -f --debug

# Abrir browser em http://localhost:8080
# Fechar todas as abas
# Aguardar 20 minutos

# Esperado no terminal:
# ╔════════════════════════════════════════════════════════════╗
# ║             AUTO-SHUTDOWN POR INATIVIDADE                 ║
# ╚════════════════════════════════════════════════════════════╝
# ⏰ Último heartbeat: 14:35:22 (há 20 minutos)
# 🛑 Nenhuma página web conectada por mais de 20 minutos
# ✅ Servidor sendo encerrado...
```

### Testing Update System

**Teste 1: Detecção de Updates**
```bash
./build/k8s-hpa-manager version

# Esperado (se houver update disponível):
# k8s-hpa-manager versão 1.1.0
# 🔍 Verificando updates...
# 🆕 Nova versão disponível: 1.1.0 → 1.2.0
# 📦 Download: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0
# 📝 Release Notes (preview): ...
```

**Teste 2: Auto-Update Check**
```bash
~/.k8s-hpa-manager/scripts/auto-update.sh --check

# Esperado:
# Status da Instalação
# ℹ️  Versão atual: 1.1.0
# ℹ️  Localização: /usr/local/bin/k8s-hpa-manager
# ⚠️  Nova versão disponível: 1.1.0 → 1.2.0
```

**Teste 3: Auto-Update Dry-Run**
```bash
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run --yes

# Esperado:
# ⚠️  MODO DRY RUN - Nenhuma alteração será feita
# ℹ️  Auto-confirmação ativada (--yes)
# [DRY RUN] Simulando download e instalação...
# ✅ Simulação concluída! (modo dry-run)
```

**Teste 4: Cache de Verificação**
```bash
# Verificar cache
ls -lh ~/.k8s-hpa-manager/.update-check
cat ~/.k8s-hpa-manager/.update-check

# Forçar nova verificação
rm ~/.k8s-hpa-manager/.update-check
./build/k8s-hpa-manager version
```

**Teste 5: Instalação do Zero**
```bash
# Em máquina limpa ou container
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# Esperado:
# ✅ Instalação concluída com sucesso!
# Versão instalada: 1.2.0
# Binário: /usr/local/bin/k8s-hpa-manager
```

---

## 🔧 Troubleshooting

### Problemas Comuns Web

| Problema | Solução |
|----------|---------|
| **Tela branca após rebuild** | Hard refresh: `Ctrl+Shift+R` |
| **"TabProvider not found"** | Adicionar `<TabProvider>` em App.tsx |
| **Sessions não carregam** | Verificar `~/.k8s-hpa-manager/sessions/` existe |
| **Cluster not found** | Executar `k8s-hpa-manager autodiscover` |
| **401 Unauthorized** | Token incorreto - usar `poc-token-123` (default) |
| **Servidor não desliga** | Verificar heartbeat no console do browser (POST /heartbeat a cada 5min) |

### Problemas Comuns TUI

| Problema | Solução |
|----------|---------|
| **Cluster offline** | `kubectl cluster-info --context=<cluster>` |
| **VPN desconectada** | Conectar VPN e pressionar F5 para reload |
| **HPAs não carregam** | Verificar RBAC e toggle namespaces sistema (tecla `S`) |
| **Azure timeout** | Validar `az login` e subscription ativa |
| **Race condition** | Atualizar para versão com mutex fix (v1.6.0+) |
| **Node pools não carregam** | Executar `k8s-hpa-manager autodiscover` |

### Problemas Comuns - Sistema de Updates

| Problema | Solução |
|----------|---------|
| **Updates não detectados** | Remover cache: `rm ~/.k8s-hpa-manager/.update-check` e executar `k8s-hpa-manager version` |
| **GitHub API rate limit** | Configurar token: `export GITHUB_TOKEN=ghp_...` antes de executar |
| **Versão mostra "dev"** | Recompilar com `make build` (injeta versão via git tags) |
| **Cache não expira** | TTL de 24h - forçar com `rm ~/.k8s-hpa-manager/.update-check` |
| **Auto-update falha** | Verificar conexão, permissões sudo e requisitos (Go, Git, kubectl) |
| **Scripts não instalados** | Executar `curl ... install-from-github.sh | bash` novamente |

### Debug Mode

```bash
# TUI
k8s-hpa-manager --debug

# Web
./build/k8s-hpa-manager web -f --debug

# Logs exibidos:
#   - Estado da aplicação (AppState transitions)
#   - Mensagens Bubble Tea
#   - Operações Kubernetes (API calls)
#   - Azure authentication flow
#   - HTTP requests/responses (web)
```

### Backup e Restore

```bash
# Criar backup antes de modificações
./backup.sh "descrição do backup"

# Listar backups disponíveis
./restore.sh

# Restaurar backup específico
./restore.sh backup_20251001_122526
```

- Mantém 10 backups mais recentes
- Metadados inclusos (git commit, data, usuário)

---

## 🚀 Continuing Development

### Context for Next Claude Sessions

**Quick Context Template:**
```
Projeto: Terminal-based Kubernetes HPA + Azure AKS Node Pool management tool

Versão Atual: v1.2.0 (Outubro 2025)
Release: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0

Tech Stack:
- Go 1.23+ (toolchain 1.24.7)
- TUI: Bubble Tea + Lipgloss
- Web: React 18.3 + TypeScript 5.8 + Vite 5.4 + shadcn/ui
- K8s: client-go v0.31.4
- Azure: azcore v1.19.1, azidentity v1.12.0

Estado Atual (Outubro 2025):
✅ TUI completo com execução sequencial, validação VPN, modais
✅ Web interface 99% funcional (HPAs, Node Pools, Sessions, Dashboard)
✅ Sistema de heartbeat e auto-shutdown (20min inatividade)
✅ Snapshot de cluster para rollback
✅ Race condition corrigida (mutex RWLock)
✅ Compatibilidade TUI ↔ Web para sessões
✅ Sistema completo de instalação e updates (v1.2.0)

Build TUI: make build
Build Web: ./rebuild-web.sh -b
Binary: ./build/k8s-hpa-manager
Instalação: curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

### File Structure Quick Reference

```
internal/
├── tui/                       # Terminal UI (Bubble Tea)
│   ├── app.go                 # Main orchestrator + text methods
│   ├── handlers.go            # Event handling
│   ├── views.go               # UI rendering
│   ├── resource_*.go          # Resource management
│   └── components/            # UI components
├── web/                       # Web Interface
│   ├── frontend/src/          # React/TypeScript SPA
│   │   ├── components/        # shadcn/ui components
│   │   ├── contexts/          # StagingContext, TabContext
│   │   └── pages/             # Index, CronJobs, Prometheus
│   ├── handlers/              # Go REST API
│   │   ├── hpas.go
│   │   ├── nodepools.go
│   │   └── sessions.go
│   └── server.go              # Gin HTTP server
├── models/types.go            # App state (AppModel)
├── session/manager.go         # Session persistence
├── kubernetes/client.go       # K8s wrapper (com mutex)
├── config/kubeconfig.go       # Cluster discovery (com mutex)
└── azure/auth.go              # Azure auth
```

### Development Commands Quick Reference

```bash
# TUI
make build                    # → ./build/k8s-hpa-manager
make run-dev                  # Debug mode

# Web
./rebuild-web.sh -b           # Build completo (recomendado)
make web-dev                  # Vite dev server
./build/k8s-hpa-manager web   # Run server

# Testing
make test                     # Unit tests
make test-coverage            # Coverage report

# Installation & Updates
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
k8s-hpa-manager version       # Check version and updates
~/.k8s-hpa-manager/scripts/auto-update.sh              # Interactive update
~/.k8s-hpa-manager/scripts/auto-update.sh --yes        # Auto-confirm (for cron)
~/.k8s-hpa-manager/scripts/auto-update.sh --check      # Check status
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run    # Simulate

# Cluster setup
k8s-hpa-manager autodiscover  # Auto-descobre clusters

# Backup
./backup.sh "desc"            # Create backup
./restore.sh                  # List/restore backups
```

### Best Practices

**When Adding Features:**
1. Follow MVC pattern: Views in `views.go`, logic in `handlers.go`, state in `models/types.go`
2. Use Bubble Tea commands for async operations (NUNCA goroutines diretas)
3. Update help in `renderHelp()` function (TUI)
4. Run `make build` (TUI) ou `./rebuild-web.sh -b` (Web) after changes
5. Update this CLAUDE.md

**Code Style:**
- **Error handling**: Proper propagation, no panics
- **State management**: All UI state in `AppModel` (TUI) ou Context API (Web)
- **Async operations**: Bubble Tea commands (TUI) ou React Query (Web)
- **Unicode safety**: Always use `[]rune` para texto
- **Logging**: Use `a.debugLog()` method (TUI) ou console (Web)
- **Concurrency**: Mutex quando necessário (ex: `clientMutex` em `getClient()`)

**Common Gotchas:**
- Function closures: Check for missing `}`
- Bubble Tea returns: Always return `tea.Model` and `tea.Cmd`
- Text editing: Initialize `CursorPosition` when starting
- Session persistence: Use folder-aware functions
- Azure auth: Handle token expiration gracefully
- Web rebuild: SEMPRE usar `./rebuild-web.sh -b`
- Hard refresh: `Ctrl+Shift+R` após rebuild web

### Known Technical Debt

**Code Quality:**
- Some async operations need better error propagation
- Unit test coverage could be expanded (especialmente web handlers)
- Inline documentation for complex functions
- Large cluster lists could benefit from virtualization

**UI/UX:**
- Better handling of very small terminals (TUI)
- Support for color themes/accessibility (both)
- More intuitive keyboard shortcuts (TUI)
- More detailed progress indicators (both)

### Potential Next Features

**High Priority:**
1. Field validation (CPU/memory formats, replica ranges)
2. Undo/Redo functionality (Web)
3. Search/Filter within HPA/namespace lists (both)
4. Export sessions to YAML/JSON (both)

**Medium Priority:**
5. User-configurable templates
6. Metrics integration (current usage alongside targets)
7. History tracking with timestamps
8. Plugin system for custom validation

**Advanced:**
9. Git integration for config tracking
10. Notification system for failures
11. RESTful API for external tools (já existe para Web)
12. Prometheus/Grafana integration

---

## 📜 Histórico de Correções (Principais)

### Sistema Completo de Instalação e Updates (Outubro 2025) ✅

**Release:** v1.2.0 (publicada em 23 de outubro de 2025)
**GitHub:** https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0

**Feature:** Scripts automatizados de instalação, atualização e gerenciamento.

**Implementação:**
- **install-from-github.sh** - Instalador completo:
  - Clona repositório automaticamente
  - Verifica requisitos (Go, Git, kubectl, Azure CLI)
  - Compila com injeção de versão via git tags
  - Instala em `/usr/local/bin/k8s-hpa-manager`
  - Copia scripts utilitários para `~/.k8s-hpa-manager/scripts/`
  - Cria atalho `k8s-hpa-web` para servidor web
  - Testa instalação automaticamente

- **auto-update.sh** - Sistema de atualização automática:
  - `--yes` / `-y` - Auto-confirmação (para scripts/cron)
  - `--dry-run` / `-d` - Modo simulação (testes)
  - `--check` / `-c` - Apenas verificar status
  - `--force` / `-f` - Forçar reinstalação
  - Verificação automática 1x por dia (TUI startup)
  - Notificação no StatusContainer (TUI) ou comando `version`
  - Cache em `~/.k8s-hpa-manager/.update-check` (24h TTL)

- **Sistema de versionamento**:
  - Versão injetada via `-ldflags` durante build
  - Detecção automática via `git describe --tags`
  - Comparação semântica (MAJOR.MINOR.PATCH)
  - Verificação via GitHub API (`/repos/.../releases/latest`)
  - Suporte a GitHub token (rate limiting)

**Testes realizados (v1.2.0):**
- ✅ Detecção de updates (1.1.0 → 1.2.0)
- ✅ Comando `version` com preview de release notes
- ✅ Auto-update `--dry-run` (simulação sem alterações)
- ✅ Auto-update `--check` (status e versão disponível)
- ✅ Auto-update `--yes` (auto-confirmação)
- ✅ Cache de verificação (24h TTL)
- ✅ Link de download correto
- ✅ Binário instalado em `/usr/local/bin/`

**Arquivos criados:**
- `install-from-github.sh` - Instalador completo
- `auto-update.sh` - Script de auto-update com flags
- `INSTALL_GUIDE.md` - Guia completo de instalação
- `QUICK_INSTALL.md` - Instalação rápida
- `UPDATE_BEHAVIOR.md` - Documentação do sistema de updates
- `AUTO_UPDATE_EXAMPLES.md` - Exemplos de uso (cron, scripts, CI/CD)
- `INSTRUCTIONS_RELEASE.md` - Como publicar releases
- `create_release.sh` - Script de criação de releases

**Workflow de uso:**
```bash
# Instalação
curl -fsSL https://raw.githubusercontent.com/.../install-from-github.sh | bash

# Verificar updates
k8s-hpa-manager version

# Auto-update interativo
~/.k8s-hpa-manager/scripts/auto-update.sh

# Auto-update automático (cron)
~/.k8s-hpa-manager/scripts/auto-update.sh --yes

# Simular antes de aplicar
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run
```

**Scripts utilitários copiados:**
- `web-server.sh` - Gerenciar servidor web (com atalho `k8s-hpa-web`)
- `uninstall.sh` - Desinstalar aplicação
- `auto-update.sh` - Auto-update com flags `--yes` e `--dry-run`
- `backup.sh` / `restore.sh` - Backup/restore para desenvolvimento
- `rebuild-web.sh` - Rebuild interface web

**Benefícios:**
- ✅ Instalação em 1 comando (clone + build + install)
- ✅ Updates automáticos com notificação
- ✅ Versionamento semântico via Git tags
- ✅ Scripts utilitários sempre disponíveis
- ✅ Fácil gerenciamento do servidor web
- ✅ Auto-update seguro com confirmação (ou `--yes` para automação)
- ✅ Dry-run para testes antes de aplicar
- ✅ Desinstalação limpa e simples

**Arquivos modificados:**
- `cmd/root.go` - Flags `--check-updates`, função `checkForUpdatesAsync()`
- `cmd/version.go` - Comando `version` com verificação de updates
- `internal/updater/` (NOVO) - Sistema completo de versionamento
  - `version.go` - Versão injetada via ldflags, comparação semântica
  - `github.go` - Cliente GitHub API para releases
  - `checker.go` - Lógica de verificação (cache 24h)
- `internal/tui/app.go` - Notificação no StatusContainer (após 3s)
- `makefile` - LDFLAGS com injeção de versão, targets `version` e `release`
- `README.md` - Seção de instalação e updates atualizada
- `CLAUDE.md` - Documentação atualizada com instalação e updates

### Rollout Individual para Prometheus Stack (Outubro 2025) ✅

**Feature:** Botões individuais de rollout para cada recurso do Prometheus Stack (Deployment/StatefulSet/DaemonSet).

**Implementação:**
- **Backend**:
  - Funções genéricas de rollout em `internal/kubernetes/client.go`:
    - `RolloutDeployment()` (já existia)
    - `RolloutStatefulSet()` (NOVO - linhas 1368-1389)
    - `RolloutDaemonSet()` (NOVO - linhas 1391-1412)
  - Handler `Rollout()` em `internal/web/handlers/prometheus.go` (linhas 506-562)
  - Rota API: `POST /api/v1/prometheus/:cluster/:namespace/:type/:name/rollout`

- **Frontend**:
  - Botão "Rollout" individual para cada recurso no card
  - Estado de loading com spinner durante execução
  - Auto-refresh da lista após 2 segundos
  - Toast notifications de sucesso/erro

**Workflow:**
1. Usuário acessa página "Prometheus"
2. Cada card tem botões "Rollout" e "Editar"
3. Click em "Rollout" adiciona annotation `kubectl.kubernetes.io/restartedAt` com timestamp
4. Pods do recurso são reiniciados (rolling restart)

**Arquivos modificados:**
- `internal/kubernetes/client.go` - Funções de rollout genéricas
- `internal/web/handlers/prometheus.go` - Handler Rollout()
- `internal/web/server.go` - Rota POST rollout
- `internal/web/frontend/src/pages/PrometheusPage.tsx` - UI com botões

### Aplicar Agora para Node Pools (Outubro 2025) ✅

**Feature:** Botão "Aplicar Agora" no Node Pool Editor que aplica alterações diretamente no cluster sem passar pelo staging.

**Implementação:**
- Botão verde "✅ Aplicar Agora" ao lado de "💾 Salvar (Staging)"
- Layout idêntico ao HPA Editor (3 botões na mesma linha)
- Estado de loading com spinner ("Aplicando...")
- Logs detalhados no console (before → after)
- Toast notifications de sucesso/erro
- Chama diretamente `apiClient.updateNodePool()` para aplicação imediata

**Diferença entre botões:**
- **💾 Salvar (Staging)**: Adiciona ao staging para aplicar em lote depois
- **✅ Aplicar Agora**: Aplica imediatamente no cluster (Azure API)
- **Cancelar**: Volta aos valores originais

**Workflow:**
1. Usuário seleciona Node Pool → Editor abre
2. Modifica valores (Node Count, Autoscaling, Min/Max)
3. Clica "Aplicar Agora"
4. API chama Azure CLI para update
5. Toast de sucesso/erro
6. Editor reseta para novo estado

**Arquivos modificados:**
- `internal/web/frontend/src/components/NodePoolEditor.tsx`:
  - Import: `Loader2`, `Zap`, `apiClient`, `toast`
  - Estado: `isApplying`
  - Função: `handleApplyNow()` (linhas 110-162)
  - UI: Layout de botões reorganizado (linhas 368-406)

**Correção de Layout:**
- Removido `sticky bottom-0` que causava efeito flutuante
- Removido `p-4 overflow-y-auto h-full` do container
- Container simples `space-y-4` como no HPAEditor
- Botões fixados no flow normal do documento

### Race Condition em Testes de Cluster (Outubro 2025) ✅

**Problema:** Goroutines concorrentes causavam race condition ao testar conexões com múltiplos clusters simultaneamente.

**Solução:**
- Adicionado `sync.RWMutex` em `KubeConfigManager`
- Double-check locking pattern para performance
- Read lock para leituras, write lock para criação

**Arquivos modificados:**
- `internal/config/kubeconfig.go`

### Azure CLI Warnings como Erros (Outubro 2025) ✅

**Problema:** Warnings do Azure CLI (`pkg_resources deprecated`) eram tratados como erros fatais.

**Solução:**
- Separação stdout/stderr em `executeAzureCommand()`
- Lista de warnings conhecidos (ignorados)
- Validação inteligente via `isOnlyWarnings()`

**Arquivos modificados:**
- `internal/tui/app.go:3535-3683`

### Node Pool Sequence Logic (Outubro 2025) ✅

**Problema:** Azure CLI não permite `scale` com autoscaling habilitado - aplicação tentava scale ANTES de desabilitar.

**Solução:**
- 4 cenários detectados automaticamente:
  1. AUTO → MANUAL: Disable autoscaling → Scale
  2. MANUAL → AUTO: Scale → Enable autoscaling
  3. AUTO → AUTO: Update min/max
  4. MANUAL → MANUAL: Scale direto

**Arquivos modificados:**
- `internal/tui/app.go:3433-3545`

### Cluster Name Mismatch (Outubro 2025) ✅

**Problema:** Node pools não carregavam porque `findClusterInConfig()` não fazia match correto entre nomes com/sem `-admin` suffix.

**Solução:**
- Remove `-admin` suffix para comparação
- Fallback para match exato (backward compatibility)

**Arquivos modificados:**
- `internal/web/handlers/nodepools.go:256-282`

### Web Interface Tela Branca (Outubro 2025) ✅

**Problema:** NodePoolEditor e HPAEditor causavam tela branca porque métodos do StagingContext não existiam.

**Solução:**
- Corrigir chamadas para métodos existentes:
  - `staging.addHPAToStaging()` ao invés de `staging.add()`
  - `staging.stagedNodePools.find()` ao invés de `staging.getNodePool()`

**Arquivos modificados:**
- `internal/web/frontend/src/components/NodePoolEditor.tsx`
- `internal/web/frontend/src/components/HPAEditor.tsx`

### Sistema de Heartbeat e Auto-Shutdown (Outubro 2025) ✅

**Funcionalidade NOVA:** Servidor web desliga automaticamente após 20 minutos de inatividade.

**Implementação:**
- Frontend: `useHeartbeat` hook envia POST `/heartbeat` a cada 5 minutos
- Backend: Timer de 20 minutos resetado a cada heartbeat
- Thread-safe: `sync.RWMutex` protege timestamp

**Arquivos modificados:**
- `internal/web/server.go` - Monitor de inatividade
- `internal/web/frontend/src/hooks/useHeartbeat.ts` - Hook React

### Snapshot de Cluster para Rollback (Outubro 2025) ✅

**Funcionalidade NOVA:** Captura estado atual do cluster (TODOS os HPAs + Node Pools) para rollback.

**Implementação:**
- `fetchClusterDataForSnapshot()` busca dados FRESCOS via API (não usa cache)
- Salva como sessão com original_values = new_values
- Integração com TabManager para cluster selection

**Arquivos modificados:**
- `internal/web/frontend/src/components/SaveSessionModal.tsx`
- `internal/web/frontend/src/pages/Index.tsx` - Sincronização TabManager

### Session Management (Rename/Edit/Delete) (Outubro 2025) ✅

**Funcionalidade NOVA:** UI completa para gerenciamento de sessões salvas.

**Implementação:**
- Dropdown menu (⋮) em cada sessão
- Modais de confirmação (delete) e edição (rename)
- EditSessionModal para editar conteúdo (HPAs/Node Pools)

**Arquivos modificados:**
- `internal/web/frontend/src/components/LoadSessionModal.tsx`
- `internal/web/frontend/src/components/EditSessionModal.tsx` (NOVO)
- `internal/web/handlers/sessions.go` - Endpoint rename e update

---

**Happy coding!** 🚀
