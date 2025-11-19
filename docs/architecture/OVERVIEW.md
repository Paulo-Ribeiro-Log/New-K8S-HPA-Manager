# 🏗️ Architecture Overview

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Estrutura de Diretórios

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
│   │   │   │   ├── hooks/         # useHeartbeat, useSSE, custom hooks
│   │   │   │   ├── lib/           # API client, utilities
│   │   │   │   └── pages/         # Index, CronJobs, Prometheus
│   │   │   ├── package.json
│   │   │   └── vite.config.ts
│   │   ├── handlers/              # Go REST API handlers
│   │   │   ├── hpas.go           # HPA CRUD
│   │   │   ├── nodepools.go      # Node Pool management
│   │   │   ├── sessions.go       # Session save/load/rename/delete/edit
│   │   │   ├── cronjobs.go       # CronJob management
│   │   │   ├── prometheus.go     # Prometheus Stack
│   │   │   └── sse.go            # SSE handlers
│   │   ├── middleware/
│   │   │   └── auth.go           # Bearer token auth
│   │   ├── sse/                   # Server-Sent Events
│   │   │   └── progress.go       # SSE progress tracking
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
├── docs/                          # Documentação modular
│   ├── guides/                    # Guias práticos
│   ├── architecture/              # Documentação de arquitetura
│   └── history/                   # Histórico de correções
├── Docs/                          # Documentation (web POC, plans, fixes)
├── go.mod & go.sum
├── makefile
├── rebuild-web.sh                 # Web rebuild script (recomendado)
└── *.sh scripts                   # Install, uninstall, backup, restore
```

## Core Components

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
- `handlers/` - REST API endpoints (HPAs, Node Pools, Sessions, CronJobs, Prometheus, SSE)
- `middleware/auth.go` - Bearer token authentication
- `sse/progress.go` - SSE progress tracking system
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

## Data Flow

1. **State-Driven Architecture**: `AppModel` in `models/types.go` maintains complete app state
2. **State Transitions**: `AppState` enum manages flow:
   - Cluster Selection → Session Selection → Namespace Selection → HPA/Node Pool Management → Editing → Help
3. **Multi-Selection Flow**: One Cluster → Multiple Namespaces → Multiple HPAs/Node Pools → Individual Editing
4. **Bubble Tea Messages**: Coordinate between UI interactions and business logic (TUI)
5. **React Query + Context**: State management na web interface
6. **Client Management**: Per-cluster Kubernetes client instances (thread-safe via RWLock)
7. **Session System**: Preserves state for review/editing before application (TUI e Web compartilham formato)

## Dependencies

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
