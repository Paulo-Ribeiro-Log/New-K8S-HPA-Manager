# 🚀 Continuing Development

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Context for Next Claude Sessions

**Quick Context Template:**
```
Projeto: Terminal-based Kubernetes HPA + Azure AKS Node Pool management tool

Versão Atual: v1.0.6 (Novembro 2025)
Release: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.6

Tech Stack:
- Go 1.23+ (toolchain 1.24.7)
- TUI: Bubble Tea + Lipgloss
- Web: React 18.3 + TypeScript 5.8 + Vite 5.4 + shadcn/ui
- K8s: client-go v0.31.4
- Azure: azcore v1.19.1, azidentity v1.12.0

Estado Atual (Novembro 2025):
✅ TUI completo com execução sequencial, validação VPN, modais
✅ Web interface 99% funcional (HPAs, Node Pools, Sessions, Dashboard)
✅ Sistema de heartbeat e auto-shutdown (20min inatividade)
✅ Snapshot de cluster para rollback
✅ Race condition corrigida (mutex RWLock)
✅ Compatibilidade TUI ↔ Web para sessões
✅ Sistema completo de instalação e updates (v1.0.6)
✅ SSE Progress Bar para operações Cordon/Drain

Build TUI: make build
Build Web: ./rebuild-web.sh -b
Binary: ./build/k8s-hpa-manager
Instalação: curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

## File Structure Quick Reference

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
│   │   ├── hooks/             # useHeartbeat, useSSE
│   │   └── pages/             # Index, CronJobs, Prometheus
│   ├── handlers/              # Go REST API
│   │   ├── hpas.go
│   │   ├── nodepools.go
│   │   ├── sessions.go
│   │   └── sse.go            # SSE handlers
│   ├── sse/                   # SSE infrastructure
│   │   └── progress.go       # Progress tracking
│   └── server.go              # Gin HTTP server
├── models/types.go            # App state (AppModel)
├── session/manager.go         # Session persistence
├── kubernetes/client.go       # K8s wrapper (com mutex)
├── config/kubeconfig.go       # Cluster discovery (com mutex)
└── azure/auth.go              # Azure auth
```

## Development Commands Quick Reference

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

## Best Practices

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

## Known Technical Debt

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

## Potential Next Features

**High Priority - TODAS IMPLEMENTADAS! ✅**
1. ✅ Field validation (CPU/memory formats, replica ranges)
2. ✅ Undo/Redo functionality (via staging + menu de edição)
3. ✅ Search/Filter within HPA/namespace lists (campos de busca implementados)
4. ✅ Export sessions to YAML/JSON (save/load session)

**Medium Priority:**
5. ⏳ User-configurable templates (parcial - folders existem)
6. ⏳ **Metrics integration (current usage alongside targets)** - Ver plano detalhado
7. ✅ History tracking with timestamps
8. ⏳ Plugin system for custom validation

**Advanced:**
9. ⏳ Git integration for config tracking
10. ⏳ **Alertmanager integration (proactive alerts + recommendations)** - Ver plano detalhado
11. ✅ RESTful API for external tools
12. ⏳ Prometheus/Grafana integration (parcial - apenas Prometheus Stack management)
