# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
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

### Estado Atual (Novembro 2025)

**Versão Atual:** v1.3.9 (Release: 03 de novembro de 2025)
**GitHub Release:** https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.3.9

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
- ✅ Dashboard com **gauge de dois anéis** mostrando Capacity vs Allocatable - v1.3.3
- ✅ **Métricas precisas** idênticas ao K9s (uso de Allocatable ao invés de Capacity) - v1.3.3
- ✅ Sistema de sessões completo (save/load/rename/delete/edit)
- ✅ Staging area com preview de alterações
- ✅ Snapshot de cluster para rollback
- ✅ Sistema de heartbeat e auto-shutdown (20min inatividade)
- ✅ ApplyAllModal com progress tracking e rollout simulation
- ✅ **Rollout individual para Prometheus Stack** (Deployment/StatefulSet/DaemonSet) - Outubro 2025
- ✅ **Aplicar Agora para Node Pools** - Aplicação individual sem staging - Outubro 2025
- ✅ **Campo de busca inteligente** - HPAs (nome/namespace) e Node Pools (nome/cluster) - v1.2.1
- ✅ **Modal de edição inline** - Edição completa de HPAs no ApplyAllModal - v1.2.1
- ✅ **Sistema de eventos** - Refetch sem reload para estabilidade - v1.2.1
- ✅ **Sistema de Log Viewer** - Modal com captura em tempo real, auto-refresh, exportar CSV - v1.2.1
- ✅ **Toggle de Namespaces de Sistema** - Exibe/oculta namespaces de sistema (kube-system, monitoring, etc.) - Outubro 2025
- ✅ **Combobox de Cluster no Header** - Busca integrada com filtro em tempo real, keyboard navigation - v1.3.2
- ✅ **Redesign CronJobs e Prometheus Pages** - SplitView layout, auto-refresh, controles compactos - v1.3.4
- ✅ **Redesign Staging Page** - SplitView layout (2/5 + 3/5), busca integrada, editor inline - v1.3.7
- ✅ **Load Session Modal Simplificado** - Removido "Apply Directly", scroll independente por painel - v1.3.8
- ✅ **Edição Inline de Node Pools no ApplyAllModal** - Menu ⋮ com opções "Editar Conteúdo" e "Remover da Lista" - v1.3.9
- ✅ **Editor não fecha após salvar** - Correção em StagingPanel para HPAs e Node Pools - v1.3.9
- ✅ **Página de Monitoring HPA-Watchdog** - Sidebar retrátil, integração com engine de monitoramento, métricas em tempo real - Novembro 2025
- ✅ **Refatoração RotatingCollector** - Sistema de monitoramento simplificado, redução de 850 → 450 linhas, baseline automático de 3 dias - 07 nov 2025
- ✅ **Aba ConfigMaps (Monaco Editor)** - Listagem completa com filtro por namespace, edição YAML com monaco-yaml, diff, dry-run e apply direto via backend Go; cards de estatísticas são ocultados apenas nesta aba para maximizar o espaço útil - Nov 2025
- ✅ **Diff visual com Diff2HTML** - Modal dedicado (side-by-side) usando tema VS Code dark, nomes reais de arquivos e mesma paleta do Monaco; backend gera unified diff via `difflib` - Nov 2025
- ✅ **Melhorias de UX na aba ConfigMaps** - Toggle de Labels, botão para recolher o painel de ConfigMaps e botões “X” de limpeza em todos os campos de busca (HPAs, Node Pools, etc.) para liberar espaço no editor - Nov 2025

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

**⚠️ CORREÇÃO CRÍTICA (Outubro 2025):** Dois bugs críticos foram corrigidos no sistema de heartbeat que causavam shutdown prematuro. Ver detalhes completos na seção [Histórico de Correções](#correção-crítica-sistema-de-heartbeatauto-shutdown-outubro-2025-).

**Solução:**
- **Frontend**: Hook `useHeartbeat` envia POST `/heartbeat` a cada 5 minutos
- **Backend**: Reseta timer de 20 minutos (ou 30min inicial) ao receber heartbeat
- **Auto-shutdown**: Servidor desliga automaticamente se nenhuma página conectada por 20min
- **Thread-safe**: `sync.RWMutex` protege timestamp de heartbeat + `sync.Mutex` protege timer (corrigido em Oct/2025)

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
| **Log Viewer** | ✅ 100% | Modal com logs em tempo real (app + servidor), auto-refresh, copiar, exportar CSV, limpar |
| **System Namespaces Toggle** | ✅ 100% | Filtro de namespaces de sistema (kube-*, monitoring, etc.) com botão toggle |

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

### Toggle de Namespaces de Sistema

**Feature NOVA (Outubro 2025):**
- Filtro inteligente de namespaces de sistema (kube-system, kube-public, monitoring, etc.)
- Botão toggle na mesma linha do título "Available HPAs"
- Estados visuais distintos: ON (azul/primary) e OFF (cinza/muted)
- Default: desabilitado (namespaces de sistema ocultos)

**Implementação:**
- **Backend**: Query parameter `showSystem=true` em `/api/v1/hpas`
- **Frontend**: Estado React com ícones Eye/EyeOff
- **Filtro**: Lista de 53+ namespaces de sistema em `internal/kubernetes/client.go`
- **Posicionamento**: Propriedade `titleAction` no componente `SplitView`

**Workflow:**
```
1. Usuário acessa página de HPAs
2. Por padrão, namespaces de sistema estão ocultos (botão OFF - cinza)
3. Clicar no botão toggle:
   - ON (Eye + azul): Mostra namespaces de sistema
   - OFF (EyeOff + cinza): Oculta namespaces de sistema
4. Backend filtra usando isSystemNamespace()
5. Lista de HPAs atualizada automaticamente via useEffect
```

**Namespaces de sistema filtrados:**
- Kubernetes core: `kube-system`, `kube-public`, `kube-node-lease`, `default`
- Monitoring: `monitoring`, `prometheus`, `grafana`, `kube-prometheus-stack`
- Networking: `calico-system`, `tigera-operator`, `istio-system`, `linkerd`
- Storage: `rook-ceph`, `longhorn-system`, `openebs`
- CI/CD: `argocd`, `flux-system`, `tekton-pipelines`
- Logging: `logging`, `elastic-system`, `loki`
- Security: `cert-manager`, `vault`, `gatekeeper-system`
- E mais 30+ namespaces...

**Arquivos modificados:**
- `internal/web/handlers/hpas.go` - Parse query parameter `showSystem`
- `internal/web/frontend/src/lib/api/client.ts` - Parâmetro `showSystem` em `getHPAs()`
- `internal/web/frontend/src/hooks/useAPI.ts` - Hook `useHPAs` com `showSystem`
- `internal/web/frontend/src/components/SplitView.tsx` - Suporte a `titleAction`
- `internal/web/frontend/src/pages/Index.tsx` - Estado e botão toggle

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
| `/logs` | GET | Retorna logs da aplicação e servidor (buffer + arquivos) |
| `/logs` | DELETE | Limpa buffer de logs da aplicação |
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
- [ ] **Recovery Mode**: Seleção granular de itens, validação de cluster, progress tracking, resumo final

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

**High Priority - TODAS IMPLEMENTADAS! ✅**
1. ✅ Field validation (CPU/memory formats, replica ranges)
2. ✅ Undo/Redo functionality (via staging + menu de edição)
3. ✅ Search/Filter within HPA/namespace lists (campos de busca implementados)
4. ✅ Export sessions to YAML/JSON (save/load session)

**Medium Priority:**
5. ⏳ User-configurable templates (parcial - folders existem)
6. ⏳ **Metrics integration (current usage alongside targets)** - [Ver plano detalhado](./Docs/METRICS_INTEGRATION_PLAN.md)
7. ✅ History tracking with timestamps
8. ⏳ Plugin system for custom validation

**Advanced:**
9. ⏳ Git integration for config tracking
10. ⏳ **Alertmanager integration (proactive alerts + recommendations)** - [Ver plano detalhado](./ALERTMANAGER_INTEGRATION_PLAN.md)
11. ✅ RESTful API for external tools
12. ⏳ Prometheus/Grafana integration (parcial - apenas Prometheus Stack management)

---

## 📜 Histórico de Correções (Principais)

### Nova Arquitetura: SimpleCollector (Novembro 2025) ✅

**Data:** 08 de novembro de 2025

**Motivação:** Sistema de rotação de portas (RotatingCollector) era complexo e não estava funcionando corretamente. Port-forwards não eram criados para todos os clusters e baseline não era carregado do SQLite.

**Problema anterior:**
- Rotação de portas (55551-55556) entre múltiplos clusters não escalava
- Port-forward temporário durante scans (criado e destruído rapidamente)
- Baseline era recriado toda vez ao invés de carregar do SQLite
- Sistema complexo com slots e duração calculada dinamicamente

**Solução: SimpleCollector - Arquitetura Simplificada**

**Novo modelo:**
1. **Scans normais**: 1 porta por cluster, port-forward criado durante scan e destruído após
2. **Baseline**: Porta dedicada (55557) separada dos scans
3. **Lógica inteligente de baseline**:
   - Verifica primeiro se baseline existe no SQLite via `IsBaselineReady()`
   - Só coleta baseline se não existir ou estiver desatualizado
   - Porta 55557 criada sob demanda, destruída após coleta

**Componentes implementados:**

**1️⃣ SimpleCollector** (`internal/monitoring/collector/simple_collector.go`):
```go
type SimpleCollector struct {
    targets       map[string]*SimpleTarget // Cluster → Target mapping
    scanPorts     []int                    // [55551-55556] para scans normais
    baselinePort  int                      // 55557 para baseline
    baselineQueue chan BaselineRequest     // Fila de baselines pendentes
}
```

**2️⃣ Fluxos principais:**

**Scan normal (30s interval):**
```
1. executeScan() → scanCluster(cluster)
2. Criar port-forward temporário
3. Aguardar 2s para port-forward estar pronto
4. Coletar métricas via Prometheus (CPU, Memory, Replicas)
5. Enriquecer snapshot com K8s API (se disponível)
6. Salvar snapshots no SQLite (batch)
7. Destruir port-forward
```

**Baseline (sob demanda):**
```
1. AddTarget() → requestBaselineIfNeeded()
2. Verificar se baseline existe: persistence.IsBaselineReady()
3. Se não existe ou desatualizado → addToBaselineQueue()
4. baselineWorker() processa fila
5. Criar port-forward na porta 55557
6. collectHistoricalData() busca 3 dias via QueryRange
7. Salvar ~4320 snapshots no SQLite
8. Marcar baseline como pronto: MarkBaselineReady()
9. Destruir port-forward (libera porta)
```

**3️⃣ Verificação de baseline:**
```go
func (c *SimpleCollector) requestBaselineIfNeeded(cluster, namespace, hpaName string) {
    // Verifica se baseline já existe e está atualizado
    ready, err := c.persistence.IsBaselineReady(cluster, namespace, hpaName)

    if ready {
        log.Debug().Msg("Baseline já existe e está atualizado")
        return
    }

    // Baseline não existe ou está desatualizado, adiciona à fila
    c.addToBaselineQueue(cluster, namespace, hpaName)
}
```

**Benefícios:**
- ✅ **Simplicidade**: 1 arquivo ao invés de sistema complexo de rotação
- ✅ **Escalabilidade**: Suporta N clusters (scan sequencial)
- ✅ **Separação de responsabilidades**: Scans e baseline não interferem entre si
- ✅ **Baseline inteligente**: Carrega do SQLite primeiro, só recria se necessário
- ✅ **Port-forward eficiente**: Criado sob demanda, destruído após uso
- ✅ **Fila de baseline**: Processa HPAs sequencialmente sem sobrecarga

**Arquivos criados:**
- `internal/monitoring/collector/simple_collector.go` (NOVO - ~665 linhas)

**Próximo passo:**
- Integrar SimpleCollector no `internal/monitoring/engine/engine.go` (substituir RotatingCollector)

---

### Correção: Linhas de Referência nos Gráficos de Métricas (Novembro 2025) ✅

**Data:** 08 de novembro de 2025

**Problema:** Linhas tracejadas de CPU Request e CPU Limit não apareciam no gráfico de CPU da página de Monitoring, apesar de funcionarem corretamente no gráfico de Memory.

**Root Cause:** O eixo Y do gráfico de CPU estava com escala automática baseada apenas nos valores de uso (0.8% a 3.6%), mas as ReferenceLine estavam posicionadas em 75% (Request) e 100% (Limit), ficando **fora da escala visível do gráfico**.

**Solução implementada:**
1. **Domain fixo no YAxis**: Forçado `domain={[0, 150]}` para garantir que linhas até 100% sejam sempre visíveis
2. **Label completo no Target**: Adicionado valor percentual no label da linha verde (`Target: 60%`)
3. **Aplicado em ambos os gráficos**: CPU e Memory agora têm comportamento consistente

**Arquivos modificados:**
- `internal/web/frontend/src/components/MetricsPanel.tsx`:
  - Linha 522: `domain={[0, 150]}` no YAxis de CPU
  - Linha 530: Label `Target: ${cpuTarget}%` com cor verde
  - Linha 686: `domain={[0, 150]}` no YAxis de Memory
  - Linha 694: Label `Target: ${memoryTarget}%` com cor verde

**Resultado:**
- ✅ Linhas tracejadas de Request (laranja) e Limit (vermelha) agora aparecem corretamente
- ✅ Linha Target (verde) com label descritivo
- ✅ Escala do gráfico vai até 150% para acomodar picos acima do limit
- ✅ Consistência visual entre gráficos de CPU e Memory

---

### Refatoração Completa: Sistema de Monitoramento RotatingCollector (Novembro 2025) ✅

**Data:** 07 de novembro de 2025

**Motivação:** Sistema de monitoramento anterior (TimeSlotManager + BaselineWorkers + Queue + Scheduler) tinha 800+ linhas de código complexo, violando princípio KISS e causando over-engineering.

**Solução:** Refatoração completa em 3 fases, reduzindo para ~450 linhas com arquitetura simplificada.

---

#### **FASE 1: Limpeza de Código Legado** ✅

**Arquivos deletados:**
- ❌ `internal/monitoring/timeslot/timeslot.go` (~300 linhas)
- ❌ `internal/monitoring/baseline/worker.go` (~200 linhas)
- ❌ `internal/monitoring/baseline/queue.go` (~150 linhas)
- ❌ `internal/monitoring/baseline/scheduler.go` (~200 linhas)
- ❌ `monitoring-targets.json` (persistência duplicada)

**Arquivos limpos:**
- `internal/monitoring/engine/engine.go` - Removidos imports e referências aos componentes deletados

**Resultado:** -850 linhas de código complexo removidas

---

#### **FASE 2: RotatingCollector - Sistema Simplificado** ✅

**Arquivo criado:** `internal/monitoring/collector/rotating.go` (~450 linhas)

**Arquitetura:**

```go
type RotatingCollector struct {
    clusters     []string                    // Lista de clusters ativos
    targets      map[string]*ClusterTarget   // Cluster → Target mapping
    ports        []int                       // [55551, 55552, 55553, 55554, 55555, 55556]
    slotDuration time.Duration               // Calculado: 60s / totalSlots
    currentSlot  int
    totalSlots   int                         // ceil(len(clusters) / 6)

    persistence  *storage.Persistence
    pfManager    *portforward.PortForwardManager
    kubeManager  *config.KubeConfigManager

    running      bool
    stopCh       chan struct{}
    mu           sync.RWMutex
    wg           sync.WaitGroup
    ctx          context.Context
    cancel       context.CancelFunc
}
```

**Funcionalidades:**

**1️⃣ Rotação Dinâmica de Portas:**
- 6 portas fixas (55551-55556)
- Rotação inteligente: `totalSlots = ceil(numClusters / 6)`
- Duração de slot adaptativa: `slotDuration = 60s / totalSlots`
- Exemplo: 11 clusters → 2 slots de 30s cada

**2️⃣ Métodos Principais:**
```go
func NewRotatingCollector(...) *RotatingCollector
func (c *RotatingCollector) Start() error
func (c *RotatingCollector) Stop()
func (c *RotatingCollector) AddTarget(target scanner.ScanTarget)
func (c *RotatingCollector) RemoveTarget(cluster string)
func (c *RotatingCollector) rotationLoop()              // Loop principal
func (c *RotatingCollector) collectSlot(slotIndex int)  // Coleta 1 slot (6 clusters paralelos)
func (c *RotatingCollector) collectCluster(cluster, port) error
```

**3️⃣ Coleta de Métricas:**
```go
// Dentro de collectCluster():
promEndpoint := fmt.Sprintf("http://localhost:%d", port)
promClient := prometheus.NewClient(cluster, promEndpoint)

for _, ns := range target.Namespaces {
    for _, hpaName := range target.HPAs {
        snapshot := &models.HPASnapshot{
            Cluster: cluster, Namespace: ns, Name: hpaName, Timestamp: now,
        }
        promClient.EnrichSnapshot(ctx, snapshot) // Coleta CPU, Memory, Replicas
        snapshots = append(snapshots, snapshot)
    }
}

persistence.SaveSnapshots(snapshots) // Batch insert no SQLite
```

**4️⃣ Recálculo Dinâmico:**
```go
func (c *RotatingCollector) recalculateSlots() {
    numClusters := len(c.clusters)
    numPorts := len(c.ports)

    c.totalSlots = (numClusters + numPorts - 1) / numPorts  // Ceiling division
    c.slotDuration = 60 * time.Second / time.Duration(c.totalSlots)
}
```

**Integração no Engine:**
```go
// engine.go: Inicialização
kubeManager, _ := config.NewKubeConfigManager(kubeconfigPath)
rotatingCollector := collector.NewRotatingCollector(persistence, pfManager, kubeManager)

// Start()
if err := rotatingCollector.Start(); err != nil {
    return err
}

// AddTarget()
if e.running && e.rotatingCollector != nil {
    e.rotatingCollector.AddTarget(target)
}

// Stop()
if e.rotatingCollector != nil {
    e.rotatingCollector.Stop()
}
```

**Testes:**
- ✅ Compilação sem erros
- ✅ 11 clusters carregados
- ✅ Slots recalculados dinamicamente (1 slot → 2 slots)
- ✅ Graceful shutdown funcionando

---

#### **FASE 3: Baseline Inteligente** ✅

**Feature:** Coleta histórica de 3 dias (72h) de métricas do Prometheus para novos HPAs.

**Implementação:**

```go
func (c *RotatingCollector) CollectBaseline(cluster, namespace, hpaName string) {
    c.wg.Add(1)
    go func() {
        defer c.wg.Done()

        // 1. Port-forward temporário
        c.pfManager.Start(cluster)
        defer c.pfManager.Stop(cluster)

        // 2. Cliente Prometheus
        promClient, _ := prometheus.NewClient(cluster, "http://localhost:55551")

        // 3. Range de 3 dias
        end := time.Now()
        start := end.Add(-72 * time.Hour)
        step := 1 * time.Minute

        // 4. Query range para histórico
        replicasResult, _ := promClient.QueryRange(ctx, replicasQuery, start, end, step)
        cpuResult, _ := promClient.QueryRange(ctx, cpuQuery, start, end, step)
        memoryResult, _ := promClient.QueryRange(ctx, memoryQuery, start, end, step)

        // 5. Converter para snapshots (~4320 pontos)
        snapshots := parseResults(replicasResult, cpuResult, memoryResult)

        // 6. Batch insert no SQLite
        c.persistence.SaveSnapshots(snapshots)
        c.persistence.MarkBaselineReady(cluster, namespace, hpaName)
    }()
}
```

**Trigger Automático:**
```go
// engine.go: AddTarget()
if e.running && e.rotatingCollector != nil {
    for _, ns := range target.Namespaces {
        for _, hpaName := range target.HPAs {
            e.rotatingCollector.CollectBaseline(target.Cluster, ns, hpaName)
        }
    }
}
```

**Queries Prometheus:**
```go
// Réplicas
kube_horizontalpodautoscaler_status_current_replicas{namespace="X",horizontalpodautoscaler="Y"}

// CPU
sum(rate(container_cpu_usage_seconds_total{namespace="X",pod=~"Y.*"}[1m])) /
sum(kube_pod_container_resource_requests{namespace="X",pod=~"Y.*",resource="cpu"}) * 100

// Memória
sum(container_memory_working_set_bytes{namespace="X",pod=~"Y.*"}) /
sum(kube_pod_container_resource_requests{namespace="X",pod=~"Y.*",resource="memory"}) * 100
```

**Correlação de Timestamps:**
```go
// Usa réplicas como base, busca CPU/Memory com ±30s de tolerância
for _, sample := range replicasMatrix {
    for _, value := range sample.Values {
        timestamp := time.Unix(int64(value.Timestamp)/1000, 0)
        snapshot := &models.HPASnapshot{Timestamp: timestamp, ...}

        // Busca CPU correspondente
        for _, cpuSample := range cpuMatrix[0].Values {
            cpuTimestamp := time.Unix(int64(cpuSample.Timestamp)/1000, 0)
            if cpuTimestamp.Equal(timestamp) || cpuTimestamp.Sub(timestamp).Abs() < 30*time.Second {
                snapshot.CPUCurrent = float64(cpuSample.Value)
                break
            }
        }
        // ... mesmo para memória
    }
}
```

**Testes:**
- ✅ CollectBaseline() chamado ao adicionar HPA
- ✅ Port-forward criado (porta 55551)
- ✅ Query range executado (3 dias)
- ✅ Batch insert no SQLite
- ✅ Flag `baseline_ready` marcada
- ✅ Testes unitários atualizados (4 PASS, 3 SKIP)

---

**Arquivos modificados:**
- `internal/monitoring/collector/rotating.go` (NOVO - 602 linhas)
- `internal/monitoring/engine/engine.go` (+40 linhas)
- `internal/monitoring/engine/engine_baseline_test.go` (2 testes desabilitados com documentação)

**Benefícios:**
- ✅ **Redução de código**: 850 linhas → 450 linhas (~53% menor)
- ✅ **Simplicidade**: 1 arquivo ao invés de 4+ componentes
- ✅ **KISS**: Rotação simples com slots dinâmicos
- ✅ **Escalabilidade**: Suporta N clusters com apenas 6 portas
- ✅ **Baseline automático**: Coleta histórica de 3 dias para novos HPAs
- ✅ **Manutenibilidade**: Código fácil de entender e debugar

**Problemas conhecidos resolvidos:**
- ✅ Over-engineering eliminado
- ✅ Port-forwards gerenciados corretamente (temporários por scan)
- ✅ Graceful shutdown implementado
- ✅ Thread-safe (RWMutex)
- ✅ Testes atualizados para nova arquitetura

---

### Página de Monitoring + Integração HPA-Watchdog (Novembro 2025) ✅

**Data:** 05 de novembro de 2025

**Feature implementada:** Página de monitoramento em tempo real integrada com o HPA-Watchdog engine, com sidebar retrátil e coleta automática de métricas via Prometheus.

**Componentes implementados:**

**1️⃣ MonitoringPage com Sidebar Retrátil**
- Sidebar 320px com lista de HPAs monitorados (agrupados por cluster)
- Botão toggle para esconder/mostrar sidebar (maximiza área de gráficos)
- Animação suave de transição (300ms)
- Badge de status do engine (🟢 Ativo / ⚫ Parado) com atualização a cada 10s

**2️⃣ Integração Backend - Monitoring Engine**
- Handler `AddHPA` com normalização automática de cluster name (remove `-admin`)
- Sistema de persistência automática de targets em `~/.k8s-hpa-manager/monitoring-targets.json`
- Port-forward automático por scan (start → scan → stop) para cada cluster
- Compatibilidade com múltiplos clusters simultâneos

**3️⃣ Correção Crítica: Normalização de Cluster Name**
- **Problema**: Frontend enviava `akspriv-prod-admin`, mas port-forward precisava de `akspriv-prod`
- **Solução**: Handler `AddHPA` remove sufixo `-admin` automaticamente (linha 485)
```go
clusterName := strings.TrimSuffix(req.Cluster, "-admin")
```

**4️⃣ API Client - Novos Métodos**
```typescript
addHPAToMonitoring(cluster, namespace, hpa)  // POST /monitoring/hpa
getMonitoringStatus()                         // GET /monitoring/status
startMonitoring()                             // POST /monitoring/start
```

**5️⃣ Workflow Completo**
1. Usuário seleciona HPA e clica "Monitorar"
2. Frontend chama `addHPAToMonitoring()` com cluster normalizado
3. Backend adiciona target ao engine (sem `-admin`)
4. Engine inicia automaticamente se parado
5. Port-forward é criado por scan: `kubectl port-forward svc/prometheus-k8s -n monitoring --context akspriv-prod-admin`
6. Métricas coletadas via Prometheus e salvas no cache
7. Frontend exibe métricas em tempo real na sidebar

**Arquivos modificados:**
- `internal/web/frontend/src/pages/MonitoringPage.tsx` - Sidebar retrátil + badge status
- `internal/web/frontend/src/lib/api/client.ts` - Métodos de monitoring (removida duplicata)
- `internal/web/handlers/monitoring.go` - Normalização de cluster + logs detalhados
- `internal/monitoring/engine/engine.go` - Port-forward por scan (já existia)
- `internal/web/frontend/src/pages/Index.tsx` - Handler onMonitor com auto-start

**Problemas Identificados e Soluções:**
- ❌ **Targets antigos com `-admin`**: Salvos antes da correção, quebravam port-forward
  - ✅ Solução: Remover via API ou limpar arquivo `monitoring-targets.json`
- ❌ **localStorage com HPAs antigos**: Dados obsoletos no browser
  - ✅ Solução: `localStorage.removeItem("monitored_hpas")` + reload

**Benefícios:**
- ✅ Monitoramento em tempo real de múltiplos clusters
- ✅ Sidebar retrátil maximiza área de gráficos
- ✅ Auto-start do engine quando HPA é adicionado
- ✅ Persistência de targets entre reinicializações
- ✅ Port-forward automático e isolado por scan

**⚠️ PROBLEMA IDENTIFICADO (Novembro 2025):**

Após análise detalhada do fluxo de monitoramento, foi identificado que a **implementação atual está ERRADA**:

**Problemas críticos:**
1. **Port-forward efêmero**: Porta é criada e destruída a cada scan (engine.go:373-389)
2. **Sem baseline histórica**: Monitoring inicia sem dados de comparação
3. **Sem fila de portas**: Não há gerenciamento de duas portas simultâneas
4. **Cleanup inadequado**: Portas podem ficar órfãs se servidor crashar

**Fluxo CORRETO (conforme explicado pelo usuário):**

> "o fluxo deve iniciar com o portfoward do prometheus no namespace 'monitoring' na porta 9090, e seguir com a coleta historica dos dados do prometheus dos ultimos 3 dias do hpa selecionado, isso feito os dados serão salvos no sqlite e a partir dai o hpa começa a ser monitorado de fato, pois já temos a base para iniciar a comparação e analise. isso é extremamente importante pois sem essa parte nada temos como comparativo."

**Arquitetura correta:**
1. **Port-forward persistente**: Vive durante toda execução do servidor (não por scan)
2. **Coleta histórica PRIMEIRO**: 3 dias de dados via Prometheus range queries → SQLite
3. **Baseline obrigatória**: Só inicia monitoring após coletar histórico
4. **Duas portas simultâneas**: 55553 e 55554 abertas ao mesmo tempo
5. **Fila alternada**: Leitura alternada entre portas (load balancing)
6. **Cleanup garantido**: Destruição apenas no shutdown do servidor

**Documento de refatoração criado:**
- `/home/paulo/Scripts/Scripts GO/Scale_HPA/Scale_HPA/MONITORING_IMPLEMENTATION_TODO.md`
- Contém 4 fases de implementação detalhadas
- Inclui código de exemplo e planos de teste

**✅ IMPLEMENTAÇÃO CONCLUÍDA (06 nov 2025) - Fases 1-4 REFATORADAS:**

### Refatoração Completa: Time-Slot Based Scanning ✅

**Problema original:** Port-forwards persistentes (1 por cluster) não escalavam para >2 clusters (só 2 portas disponíveis: 55553, 55554).

**Solução final:** Sistema de rotação temporal com time slots para scanning paralelo.

### Fase 1: Port-Forward Manager (Dual Port) ✅
- ✅ PortForwardManager com 2 portas simultâneas (55553, 55554)
- ✅ Sistema de ocupação (oddBusy/evenBusy flags)
- ✅ Auto-descoberta de Prometheus service (5 nomes comuns)
- ✅ Release de porta ao parar port-forward

### Fase 2: Baseline Collection System ✅
- ✅ 3 dias (72h) de coleta histórica via Prometheus
- ✅ 16 métricas coletadas (CPU, Memory, P95/P99, Throttling, OOM, etc.)
- ✅ Validação de cobertura (mínimo 70% de dados)
- ✅ SQLite persistence com `metrics_json` field
- ✅ Flag `baseline_ready` controla início do monitoring
- ✅ Coleta durante scan (usa port-forward ativo do TimeSlotManager)

### Fase 3: TimeSlotManager + Port Queue ✅

**Arquitetura de Time Slots:**
```go
// internal/monitoring/engine/timeslot.go (NOVO)
type TimeSlotManager struct {
    clusters []string
    totalSlots int // (len(clusters) + 1) / 2
    slotDuration time.Duration // 30s (2 clusters), 20s (4), 15s (6+)
    currentSlot int
    slotStart time.Time
}

// Exemplo: 4 clusters → 2 slots de 20s cada
// Slot 0 (0-20s):  cluster[0] (55553) + cluster[1] (55554)
// Slot 1 (20-40s): cluster[2] (55553) + cluster[3] (55554)
// Slot 0 (40-60s): repete...
```

**Correção aplicada em `engine.go`:**
- ❌ **Removido**: Port-forwards persistentes no `Start()` (1 por cluster)
- ❌ **Removido**: scanLoop() que gerenciava scans sequenciais
- ❌ **Removido**: runScan() com código duplicado
- ✅ **Novo**: TimeSlotManager com rotação circular
- ✅ **Novo**: timeSlotScanLoop() - Verifica slot atual a cada 2s
- ✅ **Novo**: executeSlotScan() - Executa 2 clusters em paralelo
- ✅ **Novo**: scanClusterInSlot() - Scan individual com port-forward temporário
- ✅ **Novo**: runScanForTarget() - Lógica de scan extraída para reuso

**Código key (engine.go):**
```go
// Start() - Inicializa TimeSlotManager
clusterNames := extractClusterNames(e.config.Targets)
e.timeSlotManager = NewTimeSlotManager(clusterNames)
log.Info().
    Int("clusters", len(clusterNames)).
    Int("slots", e.timeSlotManager.totalSlots).
    Dur("slot_duration", e.timeSlotManager.slotDuration).
    Msg("TimeSlotManager configurado")

go e.timeSlotScanLoop() // Loop de slots

// timeSlotScanLoop() - Verifica slot a cada 2s
ticker := time.NewTicker(2 * time.Second)
for {
    select {
    case <-ticker.C:
        assignment := e.timeSlotManager.GetCurrentAssignment()
        if assignment.SlotIndex != lastSlot {
            e.executeSlotScan(assignment)
            lastSlot = assignment.SlotIndex
        }
    }
}

// executeSlotScan() - 2 clusters em paralelo
var wg sync.WaitGroup
wg.Add(2)
go e.scanClusterInSlot(assignment.Port55553Cluster, 55553, &wg)
go e.scanClusterInSlot(assignment.Port55554Cluster, 55554, &wg)
wg.Wait()
```

### Fase 4: Dynamic Cluster Management ✅

**AddTarget/RemoveTarget integrados:**
```go
// AddTarget() - Atualiza TimeSlotManager ao adicionar cluster
if e.running && e.timeSlotManager != nil {
    clusterNames := extractClusterNames(e.config.Targets)
    e.timeSlotManager.UpdateClusters(clusterNames)
    log.Info().
        Int("clusters", len(clusterNames)).
        Int("slots", e.timeSlotManager.totalSlots).
        Msg("TimeSlotManager atualizado após adicionar cluster")
    
    // Baseline async (não bloqueia)
    e.wg.Add(1)
    go e.collectHistoricalBaseline(target)
}

// RemoveTarget() - Recalcula slots após remoção
if e.running && e.timeSlotManager != nil {
    clusterNames := extractClusterNames(e.config.Targets)
    e.timeSlotManager.UpdateClusters(clusterNames)
    log.Info().
        Int("clusters", len(clusterNames)).
        Int("slots", e.timeSlotManager.totalSlots).
        Msg("TimeSlotManager atualizado após remover cluster")
}
```

**Benefícios da arquitetura final:**
- ✅ **Escalabilidade ilimitada**: Suporta 2, 4, 10, 100+ clusters
- ✅ **Uso eficiente de recursos**: Apenas 2 portas para N clusters
- ✅ **Scanning paralelo**: 2 clusters simultâneos por slot
- ✅ **Rotação justa**: Todos clusters escaneados em ciclos regulares
- ✅ **Port-forward temporário**: Criado/destruído por scan (não persistente)
- ✅ **Baseline obrigatória**: Só monitora após 3 dias de coleta
- ✅ **Dinâmico**: Adicionar/remover clusters recalcula slots automaticamente
- ✅ **Performance**: Duração de slot adapta-se ao número de clusters

**Arquivos criados:**
- `internal/monitoring/engine/timeslot.go` (NOVO - 220+ linhas)

**Arquivos refatorados:**
- `internal/monitoring/engine/engine.go` (1267 → 1126 linhas após cleanup)

**TODO (Fase 5 - Signal Handling):**
- ⏳ SIGINT/SIGTERM handlers para cleanup garantido
- ⏳ Graceful shutdown de port-forwards ativos
- ⏳ Flush de SQLite antes de terminar

---

### 🔄 TODO: Fase 6 - BaselineQueue com Port-Forwards Dedicados (Novembro 2025) ⏳

**Data proposta:** 06 de novembro de 2025

**Problema atual:** Coleta de baseline de 3 dias (72h) entra em conflito com scans normais porque usa as mesmas portas (55553/55554) e port-forwards temporários são destruídos antes da coleta terminar.

**Solução proposta pelo usuário:**

> "crie mais 2 novos port-forwards para o baseline com a mesma logica dos scans dos clusters normais, e que serão criados no momento da demanda e destruidos depois que a fila ficar vazia. e cada scan da baseline deve acontecer uma vez a cada dia. se o intervalo de um scan for igual ou maior que 2 dias, então um novo scan deve ser executado."

### **📋 Arquitetura:**

```
SCANS NORMAIS (métricas em tempo real):
├─ Porta 55553/55554
├─ TimeSlotManager (rotação 15-30s)
├─ Scan rápido (segundos)
└─ Port-forward temporário por slot

BASELINE (coleta histórica 3 dias):
├─ Porta 55555/55556 (NOVAS)
├─ BaselineQueue (fila de HPAs pendentes)
├─ Scan demorado (minutos - 72h de dados)
├─ Port-forward criado sob demanda
├─ Rescan 1x por dia (se último scan > 24h)
└─ Destruído quando fila vazia
```

### **✅ Vantagens:**

1. **Escalabilidade mantida**: Continua suportando 10+ clusters
2. **Separação de responsabilidades**: Scans normais não bloqueiam baseline
3. **Sem conflito de portas**: 4 portas totais (2 para scans + 2 para baseline)
4. **Eficiência de recursos**: Port-forwards de baseline criados sob demanda
5. **Dados sempre atualizados**: Rescan automático a cada 24h
6. **Baseline de 3 dias preservado**: Tempo suficiente para análise honesta

### **🔄 Fluxo completo:**

1. ✅ Usuário clica "Monitorar HPA"
2. ✅ HPA adicionado à **BaselineQueue** (prioridade 0 - primeira coleta)
3. ✅ **BaselineWorker** detecta item na fila
4. ✅ Cria port-forward em 55555 ou 55556
5. ✅ Coleta baseline de 3 dias via Prometheus (range queries)
6. ✅ Salva métricas no SQLite com timestamp
7. ✅ Marca HPA como `baseline_ready = true`
8. ✅ Remove HPA da fila
9. ✅ Se fila vazia → destrói port-forward (libera recursos)
10. ✅ **Verificação diária**: Se `last_baseline_scan > 24h` → adiciona à fila (prioridade 1)

### **⚙️ Componentes a implementar:**

**1️⃣ BaselineQueue** (`internal/monitoring/baseline/queue.go` - NOVO)
```go
type BaselineQueue struct {
    items []BaselineTask
    mu    sync.RWMutex
}

type BaselineTask struct {
    Cluster      string
    Namespace    string
    HPAName      string
    LastScan     time.Time
    Priority     int  // 0=primeira coleta, 1=rescan diário
    AddedAt      time.Time
}

// Métodos:
// - Add(task) - Adiciona à fila (evita duplicatas)
// - Pop() - Remove e retorna próximo item (maior prioridade)
// - IsEmpty() - Verifica se fila está vazia
// - List() - Lista todos os itens (para debug/UI)
// - Remove(hpaKey) - Remove HPA específico da fila
```

**2️⃣ BaselineWorker** (`internal/monitoring/baseline/worker.go` - NOVO)
```go
type BaselineWorker struct {
    id           int        // 1 ou 2
    port         int        // 55555 ou 55556
    queue        *BaselineQueue
    pfManager    *PortForwardManager
    persistence  *storage.Persistence
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}

// Métodos:
// - Start() - Inicia worker em goroutine
// - Stop() - Para worker gracefully
// - processQueue() - Loop principal (busca itens da fila)
// - collectBaseline(task) - Coleta baseline de 3 dias
// - createPortForward() - Cria port-forward na porta dedicada
// - destroyPortForward() - Destrói port-forward
```

**3️⃣ BaselineScheduler** (`internal/monitoring/baseline/scheduler.go` - NOVO)
```go
type BaselineScheduler struct {
    queue       *BaselineQueue
    persistence *storage.Persistence
    ticker      *time.Ticker
    ctx         context.Context
    cancel      context.CancelFunc
}

// Métodos:
// - Start() - Inicia verificação periódica (a cada 1 hora)
// - Stop() - Para scheduler
// - checkRescans() - Verifica HPAs com last_scan > 24h
// - addToQueue(hpaKey) - Adiciona HPA para rescan
```

**4️⃣ Integração com PortForwardManager** (`internal/monitoring/portforward/portforward.go`)
```go
// Adicionar suporte para portas 55555 e 55556
const (
    PortScanOdd       = 55553  // Scans normais (cluster ímpar)
    PortScanEven      = 55554  // Scans normais (cluster par)
    PortBaselineOdd   = 55555  // Baseline (worker 1)
    PortBaselineEven  = 55556  // Baseline (worker 2)
)

// Método novo:
// - StartBaseline(cluster, port) - Cria port-forward para baseline
```

**5️⃣ Atualização do ScanEngine** (`internal/monitoring/engine/engine.go`)
```go
type ScanEngine struct {
    // ... campos existentes ...

    // NOVO: Sistema de baseline
    baselineQueue     *baseline.BaselineQueue
    baselineWorker1   *baseline.BaselineWorker
    baselineWorker2   *baseline.BaselineWorker
    baselineScheduler *baseline.BaselineScheduler
}

// Alterações:
// - Start() - Inicia workers de baseline e scheduler
// - Stop() - Para workers e scheduler gracefully
// - AddTarget() - Adiciona HPA à BaselineQueue ao invés de coletar inline
```

**6️⃣ Schema SQLite** (`internal/monitoring/storage/persistence.go`)
```sql
-- Adicionar campo last_baseline_scan
ALTER TABLE hpa_snapshots ADD COLUMN last_baseline_scan INTEGER; -- Unix timestamp

-- Index para busca rápida de HPAs pendentes de rescan
CREATE INDEX idx_last_baseline_scan ON hpa_snapshots(last_baseline_scan);
```

### **📊 Exemplo de execução:**

```
T=0s:    Usuário adiciona 5 HPAs
         BaselineQueue = [HPA1(p0), HPA2(p0), HPA3(p0), HPA4(p0), HPA5(p0)]

T=1s:    Worker 1 (55555) → port-forward cluster A → coleta HPA1
         Worker 2 (55556) → port-forward cluster B → coleta HPA2

T=180s:  Worker 1 termina HPA1 (baseline_ready=true, last_scan=now)
         Worker 1 pega HPA3 → port-forward cluster C

T=200s:  Worker 2 termina HPA2 (baseline_ready=true, last_scan=now)
         Worker 2 pega HPA4 → port-forward cluster D

T=380s:  Worker 1 termina HPA3, pega HPA5 → port-forward cluster E
T=400s:  Worker 2 termina HPA4, fila vazia → destrói port-forward 55556

T=560s:  Worker 1 termina HPA5, fila vazia → destrói port-forward 55555
         BaselineQueue = [] (vazia)

T=24h:   Scheduler detecta HPA1.last_scan > 24h
         BaselineQueue = [HPA1(p1)] (prioridade 1 = rescan)
         Worker 1 cria port-forward 55555 → rescaneia HPA1

T=24h+3m: Worker 1 termina rescan, fila vazia → destrói port-forward
```

### **🔍 Detecção de HPAs para rescan:**

```go
// BaselineScheduler.checkRescans() - roda a cada 1 hora
func (s *BaselineScheduler) checkRescans() {
    // Busca todos os HPAs do cache
    allSnapshots := s.persistence.GetAllHPAs()

    cutoff := time.Now().Add(-24 * time.Hour)

    for _, hpa := range allSnapshots {
        if hpa.BaselineReady && hpa.LastBaselineScan.Before(cutoff) {
            task := BaselineTask{
                Cluster:   hpa.Cluster,
                Namespace: hpa.Namespace,
                HPAName:   hpa.Name,
                LastScan:  hpa.LastBaselineScan,
                Priority:  1, // Rescan (menor prioridade que primeira coleta)
                AddedAt:   time.Now(),
            }
            s.queue.Add(task)

            log.Info().
                Str("hpa", hpa.Name).
                Time("last_scan", hpa.LastBaselineScan).
                Msg("HPA adicionado para rescan diário")
        }
    }
}
```

### **📝 Checklist de implementação:**

- [ ] 1. Criar `internal/monitoring/baseline/queue.go` com BaselineQueue
- [ ] 2. Criar `internal/monitoring/baseline/worker.go` com BaselineWorker
- [ ] 3. Criar `internal/monitoring/baseline/scheduler.go` com BaselineScheduler
- [ ] 4. Atualizar PortForwardManager para suportar portas 55555/55556
- [ ] 5. Adicionar campo `last_baseline_scan` no schema SQLite
- [ ] 6. Integrar BaselineQueue/Workers/Scheduler no ScanEngine
- [ ] 7. Atualizar `AddTarget()` para adicionar à fila ao invés de coletar inline
- [ ] 8. Remover lógica antiga de coleta de baseline síncrona
- [ ] 9. Adicionar logs detalhados para debug (início/fim de coleta)
- [ ] 10. Testar com 10 HPAs de clusters diferentes
- [ ] 11. Testar rescan automático após 24h
- [ ] 12. Testar destruição de port-forwards quando fila vazia
- [ ] 13. Atualizar CLAUDE.md com documentação final

### **🎯 Resultado esperado:**

- ✅ Scans normais continuam funcionando (15-30s por ciclo)
- ✅ Baseline de 3 dias coletado corretamente sem conflitos
- ✅ Port-forwards de baseline criados/destruídos sob demanda
- ✅ Rescan automático a cada 24h mantém dados atualizados
- ✅ Sistema escalável para 100+ clusters sem problemas
- ✅ Métricas aparecem na UI imediatamente após baseline completar
- ✅ Nenhum "Sem dados disponíveis" para HPAs em coleta

**Estimativa de implementação:** 2-3 horas

---

### Correção: AddTarget e Coleta de Baseline (Novembro 2025) ✅

**Data:** 06 de novembro de 2025

**Problema identificado:** Ao adicionar novo HPA ao monitoramento, mensagem "Sem dados disponíveis" aparecia mesmo com engine rodando e outros clusters coletando métricas.

**Root Cause:**
1. `collectHistoricalBaselineAsync()` tentava criar port-forward próprio ao adicionar HPA
2. As 2 portas (55553/55554) já estavam ocupadas pelo TimeSlotManager
3. Criação de port-forward falhava silenciosamente
4. Baseline nunca era coletado
5. HPA ficava sem dados indefinidamente

**Correções aplicadas:**

**1️⃣ Removida chamada de `collectHistoricalBaselineAsync()`** (`engine.go:273-281`)
```go
// ANTES (ERRADO - tentava criar port-forward próprio)
e.wg.Add(1)
go e.collectHistoricalBaselineAsync(target)

// DEPOIS (CORRETO - aguarda próximo scan)
log.Info().Msg("Cluster adicionado - baseline será coletado no próximo scan")
```

**2️⃣ Melhorada função `AddTarget()`** (`engine.go:234-308`)
```go
// ANTES: Substituía lista de HPAs (perdia HPAs anteriores)
t.HPAs = target.HPAs

// DEPOIS: Mescla HPAs e namespaces (evita duplicatas)
hpaMap := make(map[string]bool)
for _, hpa := range t.HPAs { hpaMap[hpa] = true }
for _, hpa := range target.HPAs { hpaMap[hpa] = true }
t.HPAs = make([]string, 0, len(hpaMap))
for hpa := range hpaMap { t.HPAs = append(t.HPAs, hpa) }
```

**Fluxo corrigido:**
1. ✅ Usuário clica "Monitorar HPA" (qualquer cluster)
2. ✅ Frontend → Backend → `AddTarget()` mescla HPA à lista
3. ✅ Se cluster novo: TimeSlotManager recalcula slots
4. ✅ TimeSlotManager escaneia cluster em seu slot (15-30s)
5. ✅ Durante scan: Port-forward temporário criado
6. ✅ `runScanForTarget()` detecta HPA sem baseline (linha 1072)
7. ✅ `collectBaselineForHPA()` coleta baseline usando port-forward ativo
8. ✅ HPA marcado como `baseline_ready`
9. ✅ Dados aparecem na interface web!

**Tempo até dados aparecerem:**
- Cluster existente: 15-30 segundos (próximo slot)
- Cluster novo: 15-30 segundos (slot recalculado)

**Arquivos modificados:**
- `internal/monitoring/engine/engine.go`:
  - `AddTarget()` - Mescla de HPAs/namespaces + log claro
  - Removida chamada de `collectHistoricalBaselineAsync()`

**Benefícios:**
- ✅ Coleta de baseline funciona para qualquer cluster
- ✅ Sem conflito de portas (usa port-forward ativo do scan)
- ✅ Escalável para 100+ clusters
- ✅ HPAs anteriores não são perdidos ao adicionar novos

---

### Edição Inline de Node Pools + Correção Editor Staging (Novembro 2025) ✅

**Data:** 03 de novembro de 2025

**Feature implementada:** Menu de edição inline para Node Pools no modal "Confirmar Alterações" (NodePoolApplyModal), idêntico ao já existente para HPAs.

**Problema anterior:**
- HPAs tinham menu ⋮ com opções "Editar Conteúdo" e "Remover da Lista"
- Node Pools só tinham botão "Aplicar" sem possibilidade de edição inline
- Editor no StagingPanel fechava automaticamente após salvar (tanto HPAs quanto Node Pools)

**Solução implementada:**

**1️⃣ Menu Dropdown com 3 pontos (⋮)**
- Adicionado ao lado do botão "Aplicar" em cada Node Pool
- Opções disponíveis:
  - **Editar Conteúdo**: Abre modal inline para edição
  - **Remover da Lista**: Remove Node Pool da lista de alterações

**2️⃣ Modal de Edição Inline**
- Checkbox "Autoscaling Habilitado"
- **Modo Manual**: Campo "Node Count"
- **Modo Autoscaling**: Campos "Min Nodes" e "Max Nodes"
- Validações:
  - Node Count ≥ 0
  - Min Nodes ≥ 0
  - Max Nodes ≥ Min Nodes
- Botões "Cancelar" e "Salvar Alterações"

**3️⃣ Funções Implementadas**
```typescript
handleOpenEdit()        // Abre modal com valores atuais
handleSaveEdit()        // Valida e salva no staging
handleRemoveIndividual() // Remove do staging e adiciona ao removedKeys
```

**4️⃣ Correção: Editor não fecha após salvar**
- **Problema**: `onApplied` callback em `StagingPanel.tsx` executava `setSelectedItem(null)`
- **Solução**: Removido callback `onApplied` de HPAEditor e NodePoolEditor (linhas 251 e 255)
- **Resultado**: Editor permanece aberto após salvar, permitindo múltiplas edições sequenciais

**Arquivos modificados:**
- `internal/web/frontend/src/components/NodePoolApplyModal.tsx` (+93 linhas)
  - Imports: `DropdownMenu`, `MoreVertical`, `Edit`, `Input`, `Label`, `Checkbox`
  - Estados: `editingKey`, `editNodeCount`, `editMinNodes`, `editMaxNodes`, `editAutoscaling`, `removedKeys`, `refreshCounter`
  - Handlers: `handleOpenEdit()`, `handleSaveEdit()`, `handleRemoveIndividual()`
  - UI: DropdownMenu após botão "Aplicar" + Modal de edição inline
- `internal/web/frontend/src/components/StagingPanel.tsx` (-2 linhas)
  - Removido `onApplied={() => setSelectedItem(null)}` (HPAEditor e NodePoolEditor)

**Benefícios:**
- ✅ Paridade completa entre HPAs e Node Pools no ApplyAllModal
- ✅ Edição inline sem sair do modal de confirmação
- ✅ Validação de campos antes de salvar
- ✅ Editor permanece aberto para múltiplas edições
- ✅ UX consistente em toda aplicação

---

### Simplificação Load Session Modal + Correção Scroll Staging (Novembro 2025) ✅

**Data:** 02 de novembro de 2025

**Problemas identificados:**
1. Botão "Apply Directly (Recovery)" podia levar a erros de operação
2. Scroll no painel de itens do Staging movia o painel do editor junto
3. Página ficava em branco ao clicar em "Carregar no Staging" após remoção do Apply Directly

**Soluções implementadas:**

**1️⃣ Remoção do "Apply Directly"**
- Removida função `handleApplyDirectly()` completa (~260 linhas)
- Removidos estados: `selectedHPAs`, `selectedNodePools`, `applyingDirectly`, `currentProcessing`, `recoveryProgress`
- Removidos checkboxes de seleção granular de itens
- Removido botão "Apply Directly (Recovery)" do footer
- Removido progress indicator overlay
- Interface simplificada: Apenas visualização + "Carregar no Staging"

**2️⃣ Correção Scroll Independente**
- Removido `overflow-auto` e `p-4` do container da aba Staging em Index.tsx
- SplitView agora gerencia scroll independente para cada painel
- Scroll no painel esquerdo não afeta painel direito

**3️⃣ Bug Fix: Página em Branco**
- Root cause: Estados removidos ainda eram referenciados em `useEffect()`
- Removidos 2 `useEffect()` que tentavam usar estados inexistentes
- Limpeza completa de referências a `setSelectedHPAs`, `setSelectedNodePools`, etc.

**Arquivos modificados:**
- `internal/web/frontend/src/components/LoadSessionModal.tsx` (-290 linhas)
- `internal/web/frontend/src/pages/Index.tsx` (linha 355-356)

**Benefícios:**
- ✅ Interface mais simples e segura (sem Apply Directly)
- ✅ Scroll independente por painel (UX melhorada)
- ✅ Código limpo sem estados órfãos
- ✅ Bundle reduzido (~8KB menor)

---

### Redesign Completo: Staging Page (Novembro 2025) ✅

**Data:** 02 de novembro de 2025

**Feature implementada:** Redesign completo da página Staging para alinhar com o padrão visual das páginas CronJobs e Prometheus.

**Problema anterior:**
- Layout diferente das outras páginas (não usava SplitView)
- Sem busca integrada
- Edição em modais ao invés de painel inline
- Inconsistência visual com resto da aplicação

**Solução implementada:**

**1️⃣ SplitView Layout (2/5 + 3/5)**
- Painel esquerdo: Lista unificada de HPAs + Node Pools com busca
- Painel direito: Editor inline (HPAEditor/NodePoolEditor)
- Padrão consistente com CronJobs e Prometheus

**2️⃣ Lista unificada com badges:**
```typescript
// Combinar HPAs e Node Pools em uma lista única
const allItems = [
  ...staging.stagedHPAs.map(hpa => ({ type: 'hpa' as const, item: hpa })),
  ...staging.stagedNodePools.map(np => ({ type: 'nodepool' as const, item: np }))
];
```

**3️⃣ Busca integrada:**
- Filtra por nome, namespace (HPA) ou cluster
- Case-insensitive
- Feedback visual quando nenhum item encontrado

**4️⃣ UI compacta e consistente:**
- Cards clicáveis para seleção (border-primary quando selecionado)
- Badges visuais: HPA (azul) e Node Pool (verde)
- Badge "Modified" quando há alterações
- Preview inline das mudanças (ex: "Min: 2 → 5 | Max: 10 → 12")
- Botão trash inline para remover item

**5️⃣ Editor inline no painel direito:**
- Sem modais (edição direta no painel)
- Título dinâmico mostra item selecionado
- Empty state quando nenhum item selecionado

**Arquivos modificados:**
- `internal/web/frontend/src/components/StagingPanel.tsx` - Refatoração completa

**Benefícios:**
- ✅ UI 100% consistente com CronJobs e Prometheus
- ✅ Busca rápida em listas longas (HPAs + Node Pools misturados)
- ✅ Edição mais fluida (inline ao invés de modais)
- ✅ Workflow KISS (filosofia mantida)
- ✅ Padrão SplitView facilita futuras manutenções

---

### Sistema de Temp Staging para "Aplicar Agora" (Novembro 2025) ✅

**Data:** 02 de novembro de 2025

**Problema identificado:** No fluxo "Aplicar Agora", quando o usuário editava valores no modal de confirmação, as alterações não apareciam porque o sistema buscava valores do staging normal (que estava vazio para esse fluxo).

**Root Cause:**
- Fluxo "Aplicar Agora" passava valores diretamente via props para ApplyAllModal
- Quando usuário editava no modal, `handleSaveEdit()` salvava no staging normal via `updateHPAInStaging()`
- MAS o HPA não existia no staging normal (apenas foi passado via props)
- `freshModifiedHPAs` não encontrava o HPA no staging → usava valores stale das props
- Resultado: Edições no modal não apareciam

**Solução Implementada: "Temp Staging"**

Criado sistema de staging temporário exclusivo para fluxo "Aplicar Agora":

**1️⃣ StagingContext** (`internal/web/frontend/src/contexts/StagingContext.tsx`):
- **Estado**: `tempHPA: { current: HPA; original: HPA } | null`
- **Métodos**:
  - `setTempHPA(current, original)` - Salva HPA no temp staging
  - `updateTempHPA(updates)` - Atualiza valores (usado pela edição no modal)
  - `clearTempHPA()` - Limpa temp staging (ao fechar modal)
  - `getTempHPA()` - Retorna HPA temporário

**2️⃣ Index.tsx** (`internal/web/frontend/src/pages/Index.tsx`):
```typescript
const handleApplySingle = (current: HPA, original: HPA) => {
  // Salvar no temp staging para permitir edição no modal
  staging?.setTempHPA(current, original);

  const key = `${current.cluster}/${current.namespace}/${current.name}`;
  setHpasToApply([{ key, current, original }]);
  setShowApplyModal(true);
};
```

**3️⃣ ApplyAllModal** (`internal/web/frontend/src/components/ApplyAllModal.tsx`):

**a) freshModifiedHPAs - Busca do temp staging primeiro:**
```typescript
const freshModifiedHPAs = useMemo(() => {
  return modifiedHPAs.map(({ key, current, original }) => {
    // 1. Tentar buscar do temp staging (para "Aplicar Agora")
    const tempHPA = staging?.tempHPA;
    if (tempHPA && /* match cluster/namespace/name */) {
      return { key, current: tempHPA.current, original: tempHPA.original };
    }

    // 2. Tentar buscar do staging normal (para "Aplicar Todas")
    const freshHPA = staging?.stagedHPAs.find(/* ... */);
    return { key, current: freshHPA || current || original, original };
  });
}, [modifiedHPAs, staging?.stagedHPAs, staging?.tempHPA, refreshCounter]);
```

**b) handleSaveEdit - Detecta origem e atualiza corretamente:**
```typescript
const handleSaveEdit = () => {
  // ... validações ...

  const isFromTempStaging = /* verifica se HPA está no tempHPA */;

  if (isFromTempStaging) {
    staging?.updateTempHPA(updates);  // Atualiza temp staging
    toast.success(`HPA ${name} atualizado (Aplicar Agora)`);
  } else {
    staging?.updateHPAInStaging(/* ... */, updates);  // Atualiza staging normal
    toast.success(`HPA ${name} atualizado no staging`);
  }

  setRefreshCounter(prev => prev + 1);  // Force refresh do useMemo
};
```

**c) useEffect - Limpa temp staging ao fechar modal:**
```typescript
useEffect(() => {
  if (!open) {
    staging?.clearTempHPA();
  }
}, [open, staging]);
```

**Fluxos após correção:**

**Fluxo "Aplicar Agora":**
1. Usuário edita HPA → Clica "Aplicar Agora"
2. `handleApplySingle()` salva no **temp staging**
3. ApplyAllModal abre → `freshModifiedHPAs` busca do temp staging
4. ✅ Modal mostra alterações (cluster → editado)
5. Usuário edita no modal → `updateTempHPA()` atualiza temp staging
6. `refreshCounter++` → `useMemo` re-executa → busca valores atualizados
7. ✅ Modal reflete edições (cluster → editado → editado no modal)
8. Modal fecha → `clearTempHPA()` limpa

**Fluxo "Aplicar Todas"** (inalterado):
1. Usuário adiciona HPAs ao staging normal
2. `freshModifiedHPAs` busca do staging normal
3. Edições no modal atualizam staging normal
4. ✅ Funciona como antes

**Arquivos modificados:**
- `internal/web/frontend/src/contexts/StagingContext.tsx` (+40 linhas)
  - Interface `StagingContextType` com métodos temp staging
  - Estado `tempHPA` e funções (`setTempHPA`, `updateTempHPA`, etc)
  - Adicionado ao `value` do Provider

- `internal/web/frontend/src/pages/Index.tsx` (+3 linhas)
  - `handleApplySingle()` chama `staging.setTempHPA()`

- `internal/web/frontend/src/components/ApplyAllModal.tsx` (+50 linhas, -10 linhas)
  - `freshModifiedHPAs`: Busca temp staging primeiro
  - `handleSaveEdit()`: Detecta origem e usa método correto
  - `useEffect`: Limpa temp staging ao fechar modal
  - Import `useEffect`

**Benefícios:**
- ✅ Edições no modal "Aplicar Agora" agora funcionam corretamente
- ✅ Separação clara entre fluxos "Aplicar Agora" e "Aplicar Todas"
- ✅ Staging normal preservado para aplicações em lote
- ✅ Limpeza automática de temp staging ao fechar modal
- ✅ Toasts informativos indicam qual staging foi atualizado

---

### Correção: ApplyAllModal Não Atualiza Após Edição (Novembro 2025) ✅

**Data:** 02 de novembro de 2025

**Problema identificado:** Valores editados no modal "Confirmar Alterações" não refrescavam para mostrar as alterações mais recentes.

**Root Cause:**
- ApplyAllModal usava `modifiedHPAs` (dados stale do prop) ao invés de `freshModifiedHPAs` (dados frescos do staging)
- `freshModifiedHPAs` é derivado do staging via `useMemo` e sincroniza com mudanças em tempo real
- Três locais críticos estavam usando dados stale:
  1. Linha 148: `hpaToEdit` busca HPA para edição inline
  2. Linha 228: `handleApplyAll` itera sobre HPAs para aplicar
  3. Linha 542: Nome do HPA no modal de edição

**Solução implementada:**

**Arquivo**: `internal/web/frontend/src/components/ApplyAllModal.tsx`

```typescript
// Linha 148 - Modal de edição inline
// ❌ ANTES:
const hpaToEdit = modifiedHPAs.find(({ key }) => key === editingKey);
// ✅ DEPOIS:
const hpaToEdit = freshModifiedHPAs.find(({ key }) => key === editingKey);

// Linha 228 - Aplicação em lote
// ❌ ANTES:
for (const { key, current } of modifiedHPAs) {
// ✅ DEPOIS:
for (const { key, current } of freshModifiedHPAs) {

// Linha 542 - Nome no modal de edição
// ❌ ANTES:
{modifiedHPAs.find(({ key }) => key === editingKey)?.current.name}
// ✅ DEPOIS:
{freshModifiedHPAs.find(({ key }) => key === editingKey)?.current.name}
```

**Contexto técnico:**
```typescript
// freshModifiedHPAs sincroniza com staging em tempo real
const freshModifiedHPAs = useMemo(() => {
  return modifiedHPAs.map(({ key, original }) => {
    const freshHPA = staging?.stagedHPAs.find(
      h => h.cluster === original.cluster &&
           h.namespace === original.namespace &&
           h.name === original.name
    );
    return {
      key,
      current: freshHPA || original, // Sempre pega valor ATUAL do staging
      original
    };
  });
}, [modifiedHPAs, staging?.stagedHPAs, refreshCounter]);
```

**Benefícios:**
- ✅ Edições inline refletem imediatamente na lista
- ✅ Valores aplicados são sempre os mais recentes
- ✅ Preview de alterações 100% preciso
- ✅ Consistência entre modal de edição e visualização

---

### Correção: "Nenhuma mudança visível" Após Editar Valores (Novembro 2025) ✅

**Data:** 02 de novembro de 2025

**Problema identificado:** Ao editar um HPA no modal inline (ex: Min Replicas 2 → 5) e salvar, a mensagem "Nenhuma mudança visível (valores idênticos)" ainda aparecia.

**Root Cause:**

**Arquivo**: `internal/web/frontend/src/pages/Index.tsx` (linha 405)

O objeto `original` estava sendo criado incorretamente, misturando valores atuais com valores originais:

```typescript
// ❌ ANTES (ERRADO):
original: { ...hpa, ...hpa.originalValues } as HPA,
```

**O que causava o bug:**

1. `{ ...hpa, ...hpa.originalValues }` cria um objeto:
   - Primeiro: Copia TODOS os campos de `hpa` (valores ATUAIS modificados)
   - Depois: Sobrescreve apenas com campos que existem em `hpa.originalValues`

2. `originalValues` é um objeto **parcial**, não contém todos os campos

3. Resultado: `original` ficava com mix de valores atuais + alguns valores originais

4. Exemplo prático:
   ```typescript
   // Estado quando você edita Min Replicas: 2 → 5
   hpa.originalValues = { min_replicas: 2, max_replicas: 10, target_cpu: 80 }
   hpa (atual) = { min_replicas: 5, max_replicas: 10, target_cpu: 80, target_memory: 90 }

   // Com { ...hpa, ...hpa.originalValues }:
   original = {
     min_replicas: 2,        // De originalValues ✅
     max_replicas: 10,       // De originalValues ✅
     target_cpu: 80,         // De originalValues ✅
     target_memory: 90,      // De hpa (ATUAL) ❌ BUG!
     // ... outros campos de hpa (atual)
   }

   // Comparação current vs original:
   // - min_replicas: 5 vs 2 → Mostra diferença ✅
   // - target_memory: 90 vs 90 → NÃO mostra diferença ❌ (ambos iguais!)
   ```

5. `renderChange()` retorna `null` para campos iguais, array `changes` ficava vazio → mensagem "Nenhuma mudança visível"

**Solução implementada:**

```typescript
// ✅ DEPOIS (CORRETO):
original: hpa.originalValues as HPA,
```

Agora `original` contém **APENAS** os valores originais puros salvos no staging, sem contaminação de valores atuais.

**Benefícios:**
- ✅ Comparação precisa entre valores originais e modificados
- ✅ Todas as edições aparecem corretamente no preview de mudanças
- ✅ Mensagem "Nenhuma mudança visível" só aparece quando realmente não há mudanças
- ✅ Diff completo e preciso para todas as alterações

---

### Correção: History Tracker com Campos Vazios (Novembro 2025) ✅

**Data:** 02 de novembro de 2025

**Problema identificado:** History Tracker salvava campos de recursos vazios (`cpu_request`, `memory_request`, `cpu_limit`, `memory_limit`) impossibilitando comparação completa "Antes vs Depois".

**Root Cause:**
- Handler `hpas.go` usava campos **errados** para capturar recursos do deployment
- ❌ **Antes**: Usava `Current*` fields (métricas de uso real - ainda não implementadas)
- ✅ **Correção**: Usar `Target*` fields (configuração do deployment - implementados em `EnrichHPAWithDeploymentResources`)

**Explicação técnica:**
```go
// internal/kubernetes/client.go (linha 1168-1223)
func EnrichHPAWithDeploymentResources(ctx context.Context, hpa *models.HPA) error {
    // Preenche Target* fields com configuração do deployment
    hpa.TargetCPURequest = cpuReq.String()      // ✅ Configuração real
    hpa.TargetMemoryRequest = memReq.String()   // ✅ Configuração real
    // ...

    // Current* fields são para métricas de USO REAL (TODO via Metrics Server)
    // hpa.CurrentCPURequest = ...  // ❌ Ainda não implementado
}
```

**Solução implementada:**

**Arquivo**: `internal/web/handlers/hpas.go`

**1️⃣ Estado ANTES da alteração (linha 232-246):**
```go
// ANTES (ERRADO)
beforeState = map[string]interface{}{
    "cpu_request":     beforeHPA.CurrentCPURequest,    // ❌ Vazio
    "memory_request":  beforeHPA.CurrentMemoryRequest, // ❌ Vazio
    "cpu_limit":       beforeHPA.CurrentCPULimit,      // ❌ Vazio
    "memory_limit":    beforeHPA.CurrentMemoryLimit,   // ❌ Vazio
}

// DEPOIS (CORRETO)
beforeState = map[string]interface{}{
    "cpu_request":     beforeHPA.TargetCPURequest,     // ✅ Configurado
    "memory_request":  beforeHPA.TargetMemoryRequest,  // ✅ Configurado
    "cpu_limit":       beforeHPA.TargetCPULimit,       // ✅ Configurado
    "memory_limit":    beforeHPA.TargetMemoryLimit,    // ✅ Configurado
}
```

**2️⃣ Estado DEPOIS da alteração (linha 289-299):**
```go
// ANTES (ERRADO)
afterState = map[string]interface{}{
    "cpu_request":    updatedHPA.CurrentCPURequest,    // ❌ Vazio
    "memory_request": updatedHPA.CurrentMemoryRequest, // ❌ Vazio
    "cpu_limit":      updatedHPA.CurrentCPULimit,      // ❌ Vazio
    "memory_limit":   updatedHPA.CurrentMemoryLimit,   // ❌ Vazio
}

// DEPOIS (CORRETO)
afterState = map[string]interface{}{
    "cpu_request":    updatedHPA.TargetCPURequest,     // ✅ Configurado
    "memory_request": updatedHPA.TargetMemoryRequest,  // ✅ Configurado
    "cpu_limit":      updatedHPA.TargetCPULimit,       // ✅ Configurado
    "memory_limit":   updatedHPA.TargetMemoryLimit,    // ✅ Configurado
}
```

**Fluxo de dados corrigido:**
1. `GetHPA()` busca HPA do Kubernetes (linha 233)
2. `EnrichHPAWithDeploymentResources()` preenche `Target*` com recursos do deployment (linha 284)
3. Captura BEFORE state com `Target*` fields (linha 236-245)
4. `UpdateHPA()` aplica mudanças no HPA e deployment (linha 253)
5. `GetHPA()` busca HPA atualizado (linha 279)
6. Captura AFTER state com `Target*` fields (linha 290-299)
7. `historyTracker.Log()` salva comparação completa (linha 302-313)

**Resultado:**
```json
// ANTES (campos vazios)
{
  "cpu_limit": "",
  "cpu_request": "",
  "memory_limit": "",
  "memory_request": ""
}

// DEPOIS (campos preenchidos)
{
  "cpu_limit": "2",
  "cpu_request": "500m",
  "memory_limit": "4Gi",
  "memory_request": "2Gi"
}
```

**Arquivos modificados:**
- `internal/web/handlers/hpas.go` (linhas 241-244, 295-298)

**Benefícios:**
- ✅ History Viewer mostra comparação completa "Antes vs Depois"
- ✅ Rastreabilidade completa de mudanças de recursos
- ✅ Compliance e auditoria melhorados
- ✅ Troubleshooting facilitado com histórico detalhado

---

### Redesign Completo: CronJobs e Prometheus Pages (Novembro 2025) ✅

**Data:** 01 de novembro de 2025

**Feature implementada:** Redesign completo das páginas de CronJobs e Prometheus Stack para alinhar com o padrão visual das páginas de HPAs e Node Pools.

**Problema anterior:**
- Layout desalinhado com resto da aplicação
- Controles dispersos e pouco intuitivos
- Sem busca integrada
- Estado não atualizava em tempo real após alterações

**Solução implementada:**

**1️⃣ SplitView Layout (2/5 + 3/5)**
- Painel esquerdo: Lista de recursos com busca
- Painel direito: Editor com formulários de edição
- Padrão consistente com HPAs e Node Pools

**2️⃣ Componentes criados:**
```typescript
// Lista compacta com badges de status
CronJobListItem.tsx
PrometheusListItem.tsx

// Editores com aplicação direta (sem staging)
CronJobEditor.tsx    → Suspend/Resume compacto (grid 2 botões)
PrometheusEditor.tsx → Edição de recursos + Rollout
```

**3️⃣ Auto-refresh após alterações:**
```typescript
// Pattern implementado em ambas as páginas
React.useEffect(() => {
  if (selectedItem && items.length > 0) {
    const updated = items.find(item => item.name === selectedItem.name);
    if (updated) setSelectedItem(updated);
  }
}, [items]);
```

**4️⃣ UI compacta e intuitiva:**
- **CronJobEditor**: 2 botões lado a lado (Ativar/Suspender)
  - Variant styling mostra estado ativo
  - Botão disabled quando já no estado desejado
- **PrometheusEditor**: Rollout movido para topo direito (seguro)
  - Botão "Editar Recursos" expande formulário inline
  - Salvamento direto no cluster (sem staging)
  - Botão Cancelar apenas no modo de edição

**5️⃣ Busca integrada:**
- CronJobs: Busca por nome e namespace
- Prometheus: Busca por nome, namespace e componente

**Arquivos criados:**
- `internal/web/frontend/src/components/CronJobListItem.tsx`
- `internal/web/frontend/src/components/PrometheusListItem.tsx`
- `internal/web/frontend/src/components/CronJobEditor.tsx`
- `internal/web/frontend/src/components/PrometheusEditor.tsx`

**Arquivos refatorados:**
- `internal/web/frontend/src/pages/CronJobsPage.tsx`
- `internal/web/frontend/src/pages/PrometheusPage.tsx`

**Build artifacts:**
- Frontend: `index-Ds3wDSKs.js` (628.21 kB)

**Benefícios:**
- ✅ UI consistente em toda a aplicação
- ✅ Busca rápida em listas longas
- ✅ Feedback visual imediato após alterações
- ✅ Controles compactos e seguros
- ✅ Salvamento direto no cluster (CronJobs e Prometheus não usam staging)

---

### Correção Crítica: Métricas de Dashboard + Gauge de Dois Anéis (Novembro 2025) ✅

**Data:** 01 de novembro de 2025

**Problema identificado:** Métricas de CPU e memória no dashboard mostravam valores **diferentes** do K9s (diferença de ~11% em memória).

**Root Cause:**
- Backend usava `node.Status.Capacity` para cálculo de percentuais
- K9s e `kubectl top` usam `node.Status.Allocatable`
- **Capacity** = Total de hardware (ex: 8 GB RAM)
- **Allocatable** = Capacity - Reservas do sistema (ex: 6.1 GB = 76% do total)
- Reservas: kubelet, OS, eviction threshold (~24% em memória, ~4% em CPU)

**Correção aplicada:**

**1️⃣ Backend - Cálculo correto:**
```go
// ANTES (ERRADO)
if memory := node.Status.Capacity.Memory(); memory != nil {
    totalMemoryCapacity += memory.Value()
}

// DEPOIS (CORRETO)
if memory := node.Status.Allocatable.Memory(); memory != nil {
    totalMemoryAllocatable += memory.Value()
}
```

**2️⃣ Backend - Novos campos de métricas:**
```go
type ClusterMetrics struct {
    CPUUsagePercent       float64 // % de uso vs Allocatable
    MemoryUsagePercent    float64 // % de uso vs Allocatable
    CPUCapacityPercent    float64 // % de Allocatable vs Capacity (novo)
    MemoryCapacityPercent float64 // % de Allocatable vs Capacity (novo)
}
```

**3️⃣ Frontend - Gauge de dois anéis concêntricos:**
- **Anel externo (Capacity):**
  - 🟦 Azul: Allocatable (ex: 76% da memória total)
  - ⚫ Cinza: System Reserved (ex: 24% reservado para OS/kubelet)
- **Anel interno (Usage):**
  - 🟢/🟡/🔴 Verde/Amarelo/Vermelho: Uso real (ex: 48.5% do allocatable)

**4️⃣ Frontend - Legenda educativa:**
```
✓ Allocatable:       76.1%  (disponível para pods)
✓ System Reserved:   23.9%  (kubelet, OS, eviction)
✓ Current Usage:     48.5%  (uso real)
```

**Resultados:**

**Antes:**
```
K9s:       CPU 19%,  Memory 48%
Dashboard: CPU 19.5%, Memory 36.9%  ❌ 11% de diferença!
```

**Depois:**
```
K9s:       CPU 19%,  Memory 48%
Dashboard: CPU 19.7%, Memory 48.5%  ✅ <1% de diferença (timing)
```

**Benefícios:**
- ✅ Métricas agora **100% precisas** (idênticas ao K9s)
- ✅ Visualização **educativa** do overhead do sistema
- ✅ Diagnóstico facilitado de clusters com overhead alto
- ✅ Transparência total sobre uso de recursos

**Arquivos modificados:**
- `internal/config/kubeconfig.go` - Cálculo de Allocatable vs Capacity
- `internal/web/handlers/clusters.go` - Novos campos na API
- `internal/web/frontend/src/lib/api/types.ts` - Tipos TypeScript
- `internal/web/frontend/src/components/MetricsGauge.tsx` - Gauge de dois anéis
- `internal/web/frontend/src/components/DashboardCharts.tsx` - Layout otimizado

---

### Feature: Combobox de Busca de Clusters no Header (Outubro 2025) ✅

**Data:** 31 de outubro de 2025

**Feature implementada:** Combobox com busca integrada para seleção de clusters no header da interface web.

**Problema anterior:**
- Select dropdown simples sem busca
- Usuário tinha que rolar lista completa de clusters (70+ clusters)
- Difícil encontrar cluster específico rapidamente

**Solução implementada:**
- ✅ **Combobox completo** usando componentes shadcn/ui (Command + Popover)
- ✅ **Busca integrada** - Campo de busca dentro do dropdown
- ✅ **Filtragem em tempo real** - CommandInput filtra automaticamente
- ✅ **Keyboard navigation** - Setas, Enter, Esc funcionam nativamente
- ✅ **Check visual** - Ícone ✓ mostra cluster selecionado
- ✅ **Auto-close** - Dropdown fecha automaticamente após seleção
- ✅ **Acessibilidade** - role="combobox" e ARIA attributes corretos

**Componentes utilizados:**
```typescript
<Popover>
  <PopoverTrigger>
    <Button role="combobox">
      {selectedCluster || "Selecione ou busque um cluster..."}
      <ChevronsUpDown />
    </Button>
  </PopoverTrigger>
  <PopoverContent>
    <Command>
      <CommandInput placeholder="Buscar cluster..." />
      <CommandList>
        <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
        <CommandGroup>
          {clusters.map((cluster) => (
            <CommandItem onSelect={handleSelect}>
              <Check /> {cluster}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </Command>
  </PopoverContent>
</Popover>
```

**Arquivos modificados:**
- `Header.tsx` - Substituído Select por Combobox completo
- Removido `ClusterSelectorForTab.tsx` modificações (não é usado no header)

**Benefícios:**
- ✅ **Busca rápida**: Digite parte do nome e encontre instantaneamente
- ✅ **UX melhorada**: Um componente unificado ao invés de dois separados
- ✅ **Escalável**: Funciona perfeitamente com 70+ clusters
- ✅ **Keyboard-friendly**: Navegação completa via teclado
- ✅ **Feedback visual**: Check mark no item selecionado

**Exemplos de uso:**
- Digite "hlg" → Filtra todos os clusters de homologação
- Digite "faturamento" → Mostra `akspriv-faturamento-hlg-admin`
- Setas ↑↓ → Navega entre clusters filtrados
- Enter → Seleciona e fecha dropdown
- Esc → Fecha sem selecionar

---

### Correção Crítica: Input Fields e Modal Auto-Update (Outubro 2025) ✅

**Data:** 31 de outubro de 2025

**Problema 1 identificado:** Campos de input numéricos na interface web não podiam ser limpos completamente, sempre retinham pelo menos um dígito.

**Cenário que causava bug:**
- Usuário tenta deletar valor "4" → Campo deveria ficar vazio → Digita "25" → Deveria mostrar "25"
- **Comportamento errado**: Delete "4" → Campo mostra "1" → Digita "25" → Campo mostra "125"

**Solução aplicada:**
1. **Mudança de tipo de input**: `type="number"` → `type="text"` com validação regex `/^\d+$/`
2. **Estados de string**: Mudado de `number` → `string` para permitir campo vazio
3. **Removido onBlur**: Handler que restaurava valores default foi removido
4. **UX melhorada**: Adicionado `select()` em `onClick` e `onFocus` para selecionar todo texto

**Arquivos modificados:**
- `HPAEditor.tsx` - Campos Min/Max Replicas, Target CPU/Memory, Resources
- `NodePoolEditor.tsx` - Campos Node Count, Min/Max Nodes

---

**Problema 2 identificado:** Modal de confirmação (ApplyAllModal) não refletia alterações feitas no editor inline, exigindo fechar e reabrir o modal para ver mudanças.

**Cenário que causava bug:**
1. Carregar sessão no staging
2. Abrir modal de confirmação
3. Clicar "Editar Conteúdo" (⋮ menu)
4. Alterar valores (ex: Max Replicas 11 → 10)
5. Salvar
6. **Bug**: Modal não atualizava, usuário tinha que fechar e reabrir

**Root Cause:**
- Modal renderizava dados da **prop** `modifiedHPAs` (fixa e imutável)
- Staging era atualizado corretamente, mas React não detectava mudança
- `refreshCounter` existia mas não forçava re-render dos dados

**Solução aplicada:**
1. **Criado `freshModifiedHPAs` com `useMemo`**: Deriva dados frescos do staging a cada render
2. **Substituído `modifiedHPAs` por `freshModifiedHPAs`**: Modal agora renderiza dados dinâmicos
3. **`refreshCounter` nas dependências do useMemo**: Força recálculo quando incrementado

**Código implementado:**
```typescript
// Deriva dados frescos do staging
const freshModifiedHPAs = useMemo(() => {
  return modifiedHPAs.map(({ key, original }) => {
    const freshHPA = staging?.stagedHPAs.find(
      h => h.cluster === original.cluster &&
           h.namespace === original.namespace &&
           h.name === original.name
    );

    return {
      key,
      current: freshHPA || original, // Dados frescos do staging
      original
    };
  });
}, [modifiedHPAs, staging?.stagedHPAs, refreshCounter]);

// Renderiza usando dados frescos
{freshModifiedHPAs.map(...)}
```

**Arquivos modificados:**
- `ApplyAllModal.tsx` - Import useMemo, freshModifiedHPAs, rendering atualizado

**Workflow completo agora:**
1. Usuário edita HPA no modal "Editar Conteúdo"
2. Salva → `staging.updateHPAInStaging()` atualiza dados
3. `setRefreshCounter(prev => prev + 1)` incrementa contador
4. `useMemo` detecta mudança e busca dados frescos do staging
5. React detecta mudança em `freshModifiedHPAs`
6. **Modal atualiza automaticamente** sem fechar/reabrir

**Benefícios:**
- ✅ Input fields podem ser limpos completamente (ex: "4" → "" → "25" = "25")
- ✅ Modal reflete alterações instantaneamente após edição
- ✅ Workflow mais fluido sem passos desnecessários
- ✅ Dados sempre sincronizados com staging

---

### Melhorias no Sistema de Recovery (Snapshot) - Outubro 2025 ✅

**Data:** 29 de outubro de 2025

**Problema identificado:** Sistema de recovery (Apply Directly) não validava cluster, não mostrava progresso individual e não tinha resumo final de estatísticas.

**Melhorias implementadas:**

**1️⃣ Validação de Cluster Automática**
- Detecta clusters dos itens selecionados
- Valida se há apenas 1 cluster (recovery multi-cluster não suportado)
- Troca contexto Kubernetes automaticamente (`cluster-admin`)
- Configura subscription Azure se necessário
- Exibe mensagem de erro clara se VPN desconectada

**2️⃣ Feedback de Progresso Individual**
- Progress bar visual durante execução
- Contador de progresso: `[3/10] Restaurando HPA: namespace/name...`
- Estatísticas em tempo real: `✅ 5 OK | ❌ 2 Erros`
- Estado visual atualizado dinamicamente

**3️⃣ Resumo Final com Estatísticas**
- Toast notification com resumo completo:
  - ✅ **100% sucesso**: `Recovery 100% concluído: 10 itens restaurados`
  - ⚠️ **Parcial**: `Recovery parcial: 8 OK, 2 falhas | Itens falhados: HPA: ns/name1, Node Pool: pool2`
  - ❌ **Falha total**: `Recovery falhou: 10 erros | Verifique conectividade e logs`
- Logs detalhados no console (`[Recovery] ✅ HPA restaurado (3/5): namespace/name`)
- Modal fecha automaticamente após 2s se houver sucesso

**4️⃣ Tratamento de Erros Robusto**
- Continua execução mesmo com erros individuais
- Lista de itens falhados para troubleshooting
- Previne fechamento de modal se todos os itens falharem
- Mensagens de erro específicas (VPN, cluster não encontrado, timeout)

**Arquivos modificados:**
- `internal/web/frontend/src/components/LoadSessionModal.tsx`:
  - Estados de progresso: `currentProcessing`, `recoveryProgress`
  - Função `handleApplyDirectly()` reescrita (linhas 260-519)
  - Progress bar visual (linhas 1104-1140)
- Build: Frontend v1.2.7-dirty (assets atualizados)

**Workflow completo:**
```
1. Usuário seleciona sessão de rollback
2. Marca/desmarca HPAs e Node Pools (checkboxes)
3. Clica "Apply Directly (Recovery)"
4. Sistema valida cluster e troca contexto
5. Progress bar mostra progresso individual
6. Estatísticas em tempo real (OK/Erros)
7. Resumo final com toast notification
8. Modal fecha automaticamente (se sucesso)
```

**Benefícios:**
- ✅ Recovery mais confiável com validação de cluster
- ✅ Visibilidade completa do progresso
- ✅ Troubleshooting facilitado com logs e lista de falhas
- ✅ UX melhorada com feedback em tempo real
- ✅ Prevenção de erros (multi-cluster, VPN desconectada)

---

### Correção de Assets Não Embeddados - go:embed (Outubro 2025) ✅

**Release:** v1.2.6 (28 de outubro de 2025)
**Commit:** 0f05463

**Problema identificado:** Webpage em branco em qualquer computador após instalação da release.

**Root Cause:**
- `go:embed` **APENAS** embeda arquivos versionados no Git
- `internal/web/static/*` estava no `.gitignore`
- GitHub Actions gerava os arquivos, mas `go:embed` não os encontrava
- Resultado: Binário compilado sem assets embeddados → webpage em branco

**Solução:**
1. ✅ Removido `internal/web/static/*` do `.gitignore`
2. ✅ Commitados arquivos de build no repositório:
   - `internal/web/static/assets/index-CW0HINYd.css` (76 KB)
   - `internal/web/static/assets/index-QahD77AR.js` (577 KB)
   - `internal/web/static/index.html`, `favicon.ico`
3. ✅ Release v1.2.6 criada com assets embeddados

**Validação:**
```bash
curl http://localhost:8080/assets/index-QahD77AR.js  # ✅ 200 OK (590.689 bytes)
curl http://localhost:8080/assets/index-CW0HINYd.css # ✅ 200 OK (76 KB)
```

**Lição aprendida:**
- `go:embed` requer arquivos commitados no Git
- Arquivos gerados em build-time devem ser versionados **OU** copiados para local não-ignorado
- Usar `all:` prefix para incluir subdiretórios (`//go:embed all:static`)

---

### Correção web-server.sh - Detecção de Porta Real (Outubro 2025) ✅

**Problema identificado:** Comando `status` sempre mostrava porta 8080, mesmo quando servidor rodava em porta diferente.

**Solução:**
- Script agora extrai porta real do processo em execução via `ps aux`
- Usa regex para encontrar flag `--port` na linha de comando
- Fallback para 8080 se não encontrar porta especificada

**Testes:**
```bash
./web-server.sh 9000 start  # Inicia na porta 9000
./web-server.sh status      # ✅ Mostra "📍 URL: http://localhost:9000"
```

**Arquivo modificado:** `web-server.sh` (linhas 114-140)

---

### Correção de Cross-Compilation para Windows/macOS (Outubro 2025) ✅

**Commit:** b84461c (27 de outubro de 2025)

**Problema identificado:** Build multi-plataforma falhava durante `make release` com erro de compilação.

**Erro:**
```
Error: cmd/root.go:239:59: undefined: unix.TCGETS
```

**Causa:**
- Função `isatty()` não utilizada no código usava `unix.IoctlGetTermios()` e `unix.TCGETS`
- `golang.org/x/sys/unix` é específico do Linux/Unix
- Cross-compilation para Windows e macOS falhava no GitHub Actions

**Solução:**
- ❌ Removido import `golang.org/x/sys/unix`
- ❌ Removida função `isatty()` não utilizada (código morto)
- ✅ Código agora é cross-platform compatível

**Nota técnica:** O projeto já possui `github.com/mattn/go-isatty` como dependência (via Gin framework), que é cross-platform. Se precisar verificar TTY no futuro, usar essa biblioteca ao invés de `unix.IoctlGetTermios()`.

**Testes realizados:**
- ✅ `make release` compila para todas as plataformas:
  - Linux amd64:        82M ✓
  - macOS amd64 (Intel): 82M ✓
  - macOS arm64 (Apple): 80M ✓
  - Windows amd64:       82M ✓

**Arquivos modificados:**
- `cmd/root.go` (-7 linhas)
  - Removido import `golang.org/x/sys/unix`
  - Removida função `isatty()` (linhas 237-241)

**Impacto:**
- ✅ GitHub Actions CI/CD agora compila binários para todas as plataformas
- ✅ Releases automatizadas funcionando corretamente
- ✅ Sem perda de funcionalidade (código removido não era usado)

---

### Sistema de Log Viewer para Interface Web (Outubro 2025) ✅

**Feature:** Sistema completo de visualização de logs com captura em tempo real, auto-refresh, exportação CSV e limpeza.

**Implementação:**
- **Backend** (`internal/web/handlers/logs.go`):
  - `LogBuffer` - Buffer circular thread-safe (RWMutex) com 1000 logs em memória
  - `LogsHandler` - Handler com métodos `GetLogs()` e `ClearLogs()`
  - Múltiplas fontes de logs:
    - Buffer em memória (logs da aplicação)
    - Arquivos de log (`/tmp/k8s-hpa-manager-web-*.log`)
    - Sistema (journalctl - opcional, comentado)

- **Middleware de Logging** (`internal/web/server.go`):
  - `loggingMiddleware()` - Captura TODAS as requisições HTTP
  - Formato: `[timestamp] METHOD path | Status: XXX | Latency: XXXms`
  - Filtro inteligente: Ignora `/health` e `/heartbeat` para não poluir logs
  - Thread-safe com acesso protegido ao buffer

- **Frontend** (`internal/web/frontend/src/components/LogViewer.tsx`):
  - Modal responsivo (max-w-6xl, h-85vh)
  - **Auto-refresh** - Toggle on/off, atualiza a cada 3 segundos
  - **Copiar** - Copia logs para clipboard
  - **Exportar CSV** - Parsing inteligente de logs estruturados
  - **Limpar** - Limpa buffer com confirmação
  - **Estatísticas** - Badges de total/errors/warnings/info

- **Integração no Header** (`internal/web/frontend/src/components/Header.tsx`):
  - Botão discreto com ícone 📄 (FileText)
  - Tooltip "View System Logs"

**API Routes:**
- `GET /api/v1/logs` - Buscar logs (buffer + arquivos)
- `DELETE /api/v1/logs` - Limpar buffer

**Workflow:**
1. Usuário clica no ícone 📄 no header
2. Modal abre com logs divididos por fonte:
   - **Application Logs (In-Memory)** - Requisições HTTP capturadas
   - **Web Server Logs** - Logs do arquivo do servidor
3. Auto-refresh mantém logs atualizados automaticamente
4. Exportar CSV para análise offline
5. Limpar buffer quando necessário

**Testes realizados:**
- ✅ Captura de requisições HTTP em tempo real
- ✅ Auto-refresh funcionando (3s)
- ✅ Copiar para clipboard
- ✅ Exportar CSV com parsing correto
- ✅ Limpar buffer com confirmação
- ✅ Estatísticas de logs (total, errors, warnings)
- ✅ Thread-safe (RWMutex)

**Arquivos criados:**
- `internal/web/handlers/logs.go` (NOVO)
- `internal/web/frontend/src/components/LogViewer.tsx` (NOVO)

**Arquivos modificados:**
- `internal/web/server.go` - Middleware + rotas de logs
- `internal/web/frontend/src/components/Header.tsx` - Botão de logs
- `internal/web/frontend/src/pages/Index.tsx` - Integração do modal

**Benefícios:**
- ✅ Debugging facilitado com logs em tempo real
- ✅ Investigação de erros sem acesso ao servidor
- ✅ Exportação para análise offline (CSV)
- ✅ Auto-refresh elimina necessidade de recarregar manualmente
- ✅ Filtros inteligentes (ignora health/heartbeat)

---

### Correção Crítica: Sistema de Heartbeat/Auto-Shutdown (Outubro 2025) ✅

**Commit:** 7e38820 (24 de outubro de 2025)

**Problema identificado:** Servidor web desligava prematuramente mesmo com heartbeats sendo enviados.

**Bug 1: Race Condition no Timer**
- **Problema:** O `shutdownTimer` não tinha proteção mutex, permitindo race conditions entre múltiplos heartbeats simultâneos ou durante o disparo do timer
- **Solução:** Adicionado `timerMutex sync.Mutex` na struct Server para proteger todas as operações de Stop() e AfterFunc()
- **Impacto:** Previne desligamentos inesperados durante operações concorrentes

**Bug 2: Timer Inicial Prematuro**
- **Problema:** Timer de 20 minutos começava a contar imediatamente quando servidor iniciava, NÃO quando frontend conectava
- **Cenário que causava o bug:**
  1. Servidor inicia às 14:15 (cria timer para 14:35)
  2. Frontend envia primeiro heartbeat às 14:25 (cria novo timer para 14:45)
  3. Heartbeats subsequentes em 14:30, 14:35...
  4. **MAS**: Timer original das 14:35 ainda estava ativo e disparava!
- **Solução:** Timer inicial aumentado para 30 minutos (tempo de graça), primeiro heartbeat do frontend reseta para 20 minutos normais
- **Impacto:** Garante que servidor não desligue antes do frontend conectar

**Melhorias de Logging:**
```
💓 Heartbeat recebido: 15:44:49 | Próximo shutdown em: 16:04:49
```
- Log detalhado em cada heartbeat mostrando timestamp recebido e próximo shutdown
- Mensagem clara sobre timer inicial de 30 minutos
- Facilita debugging e monitoramento do sistema

**Testes realizados:**
- ✅ Múltiplos heartbeats recebidos e processados corretamente
- ✅ Timer resetado a cada heartbeat (verificado via logs)
- ✅ Servidor permanece ativo com página aberta
- ✅ Múltiplas abas abertas simultaneamente (cada uma envia heartbeat)

**Arquivos modificados:**
- `internal/web/server.go` (+18 linhas, -4 linhas)
  - Adicionado `timerMutex sync.Mutex`
  - Protegido todas as operações no timer com mutex
  - Timer inicial aumentado de 20min → 30min
  - Log detalhado em cada heartbeat

**Impacto:** Sistema de auto-shutdown agora funciona corretamente sem desligar prematuramente.

---

### Campo de Busca e Edição Inline na Interface Web (Outubro 2025) ✅

**Release:** v1.2.1 (publicada em 24 de outubro de 2025)
**GitHub:** https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.1

**Features:** Campo de busca inteligente, edição inline de HPAs, e correções críticas de estabilidade.

**Implementação:**
- **Campo de Busca Inteligente**:
  - Campo de busca no painel "Available HPAs" (busca por nome e namespace)
  - Campo de busca no painel "Available Node Pools" (busca por nome e cluster)
  - Interface consistente com ícone de lupa
  - Busca case-insensitive em tempo real
  - Feedback visual quando nenhum item é encontrado

- **Modal de Edição Inline (ApplyAllModal)**:
  - Edição completa de HPAs sem sair do modal de confirmação
  - Dropdown menu (⋮) com opções "Editar Conteúdo" e "Remover da Lista"
  - Validação de campos (Min/Max Replicas, Target CPU/Memory 1-100%)
  - Suporte a edição de recursos (CPU/Memory Request/Limit)
  - Checkboxes de rollout (Deployment, DaemonSet, StatefulSet)
  - Atualização em staging após edição

- **Correções de Bugs Críticos**:
  - Remove `window.location.reload()` que causava restart da página
  - Implementa sistema de eventos customizados (`rescanNodePools`)
  - Adiciona listener no hook `useNodePools` para refetch automático
  - Previne perda de dados durante operações de Node Pools
  - Mantém estado e contexto durante operações longas

**Arquivos modificados:**
- `internal/web/frontend/src/pages/Index.tsx` (+129 linhas)
- `internal/web/frontend/src/hooks/useAPI.ts` (+32 linhas)
- `internal/web/frontend/src/components/ApplyAllModal.tsx` (+355 linhas)
- `internal/web/static/` (rebuild frontend)

**Benefícios:**
- ✅ Produtividade aumentada com busca rápida (70+ HPAs/Node Pools)
- ✅ Correção de erros sem interromper fluxo de trabalho
- ✅ Estabilidade em operações longas (sem restart)
- ✅ Experiência de usuário consistente e previsível

---

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
- "Não faça over-enginnering"
