# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Versão atual oficial: **v1.3.26** (GitHub release).
**IMPORTANTE**: Ao fazer alterações no frontend (React/TypeScript), sempre rebuild com `./rebuild-web.sh -b` E fazer hard refresh no navegador (Ctrl+Shift+R).
**IMPORTANTE**: Data de hoje: **14 de março de 2026** - usar esta data ao documentar mudanças.

---

## Documentação Modular

- [Quick Start & Features](docs/guides/QUICK_START.md)
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md)
- [Architecture Overview](docs/architecture/OVERVIEW.md)
- [Web Interface Guide](docs/guides/WEB_INTERFACE.md)
- [Common Pitfalls](docs/guides/COMMON_PITFALLS.md)
- [RBAC Azure AD](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md)
- [Changelog](docs/history/CHANGELOG.md)

---

## Comandos Essenciais

```bash
# Build
make build                    # Compilar backend Go
./rebuild-web.sh -b           # Build frontend + backend (RECOMENDADO após mudanças React)
make build-web                # Build completo (frontend + backend)

# Run
./build/new-k8s-hpa web       # Servidor web (porta 8080)
./build/new-k8s-hpa web -f    # Foreground mode (logs no terminal)
./build/new-k8s-hpa web --ad  # EMERGÊNCIA: Bypass RBAC (flag oculta)

# Dev
make web-dev                  # Frontend dev server (Vite HMR - porta 5173)
make run-dev                  # TUI com debug

# Tests
go test -v ./internal/... -race   # Todos os testes com race detector
go test -run TestGetClient        # Teste específico
./testes/test-rbac.sh             # Suite completa RBAC (40+ cenários)

# Debug
tail -f /tmp/k8s-hpa-manager-web-*.log  # Logs do servidor

# Release
make release                  # Build multi-plataforma (linux, darwin Intel, darwin ARM64)
make build-all                # Alias para release
./create-v1-release.sh        # Criar release no GitHub

# Outros
make test-coverage            # Testes com cobertura HTML
make web-install              # npm install no frontend
make web-clean                # Limpa arquivos de build frontend
```

### Antes de Commitar

```bash
go test -v ./internal/... -race  # 1. Testes com race detector
make build                        # 2. Verificar build
./rebuild-web.sh -b               # 3. Se alterou frontend
go fmt ./...                      # 4. Formatting Go
go mod vendor                     # 5. Vendored modules atualizados
```

### Após Mudanças no Frontend

```bash
./rebuild-web.sh -b
# Ctrl+Shift+R no navegador (hard refresh obrigatório)
ls -lh internal/web/static/assets/ | grep -E "\.(js|css)$"  # Verificar assets
```

---

## Estrutura do Projeto

```
k8s-hpa-manager/
├── cmd/                      # CLI commands (Cobra): web.go, autodiscover.go, diagnose.go
├── internal/
│   ├── tui/                  # Terminal UI (Bubble Tea)
│   ├── web/
│   │   ├── frontend/         # React SPA (src/components/, src/hooks/, src/lib/api/)
│   │   ├── handlers/         # Go REST API handlers (um arquivo por recurso)
│   │   ├── sse/              # Server-Sent Events broker
│   │   └── middleware/       # RBAC, CORS
│   ├── kubernetes/           # K8s client wrapper (client.go - métodos centrais)
│   ├── azure/                # Azure SDK auth
│   ├── models/               # types.go - fonte de verdade de todos os tipos
│   ├── config/               # Kubeconfig, cache de clients K8s
│   ├── session/              # Sessions TUI ↔ Web (formato JSON compatível)
│   ├── monitoring/           # Prometheus, predictions/, nodepoolpredictions/
│   │   └── engine/           # monitoring_v2.go — discovery automático sem port-forwards
│   ├── rbac/                 # Azure AD RBAC (azure_ad.go)
│   ├── ai/                   # AI Diagnostics (Ollama/Claude/Gemini), reports/
│   ├── sanitizer/            # Sanitização de logs antes de enviar para IA
│   ├── storage/              # SQLite: predictions.db, health_check.db, ai_diagnostics.db
│   │                         # + ai_history_store.go, dependency_registry.go, user_tokens_store.go
│   ├── certificates/         # Gerenciamento de certificados TLS
│   ├── servicenow/           # Integração ServiceNow
│   ├── healthcheck/          # Health checking: orchestrator, deployment/hpa/event/pv checkers
│   └── history/              # History tracker
├── build/                    # Binários compilados
├── vendor/                   # Go modules vendored (go build -mod=vendor)
├── scripts/                  # Scripts de diagnóstico e utilitários
└── docs/                     # Documentação modular
```

**Tech Stack:**
| Categoria | Tecnologia |
|-----------|------------|
| Backend | Go 1.24.0+, client-go v0.34.1, Gin v1.11.0 |
| Frontend | React 18.3.1, TypeScript 5.8.3, Vite 5.4.21 |
| UI | shadcn/ui (Radix UI), Tailwind CSS 3.4.17, Recharts |
| Editor | Monaco Editor 0.52.2, xterm.js 5.3.0, diff2html |
| Web Server | Gin 1.11.0, SSE, WebSocket |
| Graphs | Cytoscape.js (dependency graphs) |
| Forms | react-hook-form + Zod validation |

---

## Conceitos de Arquitetura Críticos

### Thread-Safety (Go)

`sync.RWMutex` com double-check locking para o `clientCache` em `internal/config/kubeconfig.go`. Nunca acessar o cache sem o mutex.

**Bubble Tea — NUNCA usar goroutines diretas:**
```go
// ❌ ERRADO - Race condition
go func() { result := applyHPA() }()

// ✅ CORRETO - Retornar tea.Cmd
return func() tea.Msg {
    err := applyHPA()
    return HPAAppliedMsg{err: err}
}
```

### Estado Global

`internal/models/types.go` é a **única** fonte de verdade. `AppModel` contém todo o estado da aplicação. Nunca criar estado local em handlers ou views — sempre modificar `AppModel` e retornar mensagem.

### Handlers HTTP (Padrão Gin + DI)

```go
// internal/web/handlers/example.go
type ExampleHandler struct {
    clientCache *cache.ClientCache  // Shared K8s clients — NUNCA criar direto
    logger      *zerolog.Logger
}
```

Rotas registradas em `internal/web/server.go`. RBAC via middleware em rotas POST/PUT/DELETE.

### Frontend — API Client

Todas as chamadas HTTP centralizadas em `internal/web/frontend/src/lib/api/client.ts`. Nunca fazer `fetch` direto em componentes.

### React Query

```typescript
// SEMPRE usar queryKey único para invalidação
queryKey: ['resource-type', cluster, namespace],
// NUNCA usar window.location.reload() — usar queryClient.invalidateQueries()
```

### SSE (Server-Sent Events)

Broker em `internal/web/sse/progress.go` gerencia múltiplos clients. Usado em Cordon/Drain, Health Check, Helm Apply, Node Pool operations, **Command Runner**. Cada operação longa publica eventos via SSE para feedback em tempo real.

### WebSocket (Terminal)

Protocolo JSON em `internal/web/handlers/websocket_shell.go`:
- Envio: `{type: "input", data: "..."}` ou `{type: "resize", rows: N, cols: N}`
- Resposta: `{type: "output", data: "base64..."}`
- SEMPRE usar `event.preventDefault()` em key handlers para evitar duplicação de caracteres

**Auth WebSocket**: WebSockets não enviam headers customizados. O middleware `WebSocketAuthMiddleware` aceita token via query param como fallback: `ws://host/terminal?token=<TOKEN>`.

### Versionamento

Versão injetada via ldflags em build time (`main.version`). **Nunca hardcodear versão no código.**

```bash
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
go build -ldflags "-X main.version=$(VERSION)" -o build/new-k8s-hpa
```

---

## Peculiaridades Críticas

### Azure CLI — Ordem de Operações para Node Pools

Azure CLI **rejeita** `az aks nodepool scale` se autoscaling estiver habilitado. Implementação em `internal/tui/app.go:buildNodePoolCommands()` lida com 4 cenários:
- Apenas autoscaling (min/max): um `update --enable-cluster-autoscaler`
- Apenas node count: `disable-cluster-autoscaler` → `scale`
- Ambos: enable → disable → scale → enable novamente
- Abort real: `az aks nodepool operation-abort` (cancela no ARM, não apenas o CLI local)

### Suffix `-admin` em Cluster Names

Sessions salvam sem `-admin`, mas kubeconfig contexts têm `-admin`. `StagingContext.tsx` usa `ensureAdminSuffix()` ao carregar sessões. Ao criar prompts IA, usar nome **sem** `-admin`.

### TypeMeta em YAMLs do typed API

A API typed do K8s (`clientset.CoreV1().Secrets().Get()`) **não preenche** `TypeMeta`. Antes de serializar para YAML com `yaml.Marshal`, sempre adicionar:

```go
secret.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}
```

Sem isso, `kubectl apply` falha com "apiVersion not set, kind not set".

### Dynamic Client (CRDs)

O dynamic client **não está no vendor**. Para recursos CRD (VPAs, recursos do Explorer, etc.), usar kubectl shell: `kubectl get/apply/delete -o yaml`. O Discovery API (`clientset.Discovery().ServerPreferredResources()`) está disponível e é usado no Resource Explorer.

### Bubble Tea — Texto Unicode-Safe

Sempre usar `[]rune` ao invés de `string` para manipulação de texto no TUI. Cursor position em runes, não bytes.

### Audit Trail (History Tracking)

Toda operação destrutiva deve registrar no `HistoryTracker` (`internal/history/tracker.go`):

```go
entry := helpers.CreateHistoryEntry(c, "scale-hpa", before, after)
history.Log(entry)
```

`CreateHistoryEntry()` obtém `UserEmail`/`UserName` automaticamente via contexto Gin (RBAC). Dados persistidos em `~/.k8s-hpa-manager/history/` (JSON, max 1000 entradas em memória).

### Sanitizer (AI Diagnostics)

`internal/sanitizer/` mascara automaticamente IPv4, JWT, Bearer tokens, passwords, API keys antes de enviar contexto para IA. Nunca enviar dados brutos de log diretamente para AI providers — sempre passar pelo sanitizer.

### Variável de Ambiente

`K8S_HPA_WEB_TOKEN` — token de autenticação da API (default: `poc-token-123` se não definido).

### Monitoring V2

`internal/monitoring/engine/monitoring_v2.go` — sem port-forwards. Discovery automático via HTTPS: `https://prometheus-{cluster}-{env}.viavarejo.com.br/`. Cache em memória (TTL 1h). Endpoints em `/api/v1/monitoring/v2/`.

### Monaco Editor em Aba Anônima

Erros como "Tracking Prevention blocked access to storage" e "Could not create web worker(s)" são **inofensivos** — Monaco tem fallback automático para modo síncrono. A funcionalidade de edição YAML não é afetada.

### ResizeDivider (SplitView)

`SplitView.tsx` é o componente reutilizável para painéis side-by-side com resize. Usado em `CommandRunnerTab` e `ResourceCompareModal`. Implementação via `useRef` + mouse event listeners. Importar `SplitView` ao criar novas interfaces de edição lado a lado — **não reimplementar o drag logic**.

### Command Runner

`CommandRunnerTab.tsx` + `internal/web/handlers/command_runner.go`: executa comandos (kubectl/shell/python/go) em múltiplos clusters simultaneamente com SSE. Suporta **AI-powered command generation** via `POST /api/v1/command-runner/generate` (gera comandos a partir de prompt em linguagem natural).

### ToolsMenu

`ToolsMenu.tsx` — dropdown com 10 ferramentas avançadas acessíveis no header. Ao adicionar nova ferramenta, registrar aqui como novo item do dropdown.

### AI Providers (Multi-provider)

`internal/ai/` suporta Ollama, Claude API e Gemini. Configurável via `AISettingsTab.tsx`. Tokens de usuário persistidos em `internal/storage/user_tokens_store.go`. **Nunca hardcodear API keys** — sempre via storage de tokens.

### Certificates

`internal/certificates/` + `internal/web/handlers/certificates.go`: discovery de certs TLS em secrets K8s, validação de expiração, import/export. Usar para qualquer operação envolvendo TLS no cluster.

---

## RBAC Azure AD

- **Grupo SRE**: `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
- Backend: `internal/rbac/azure_ad.go` + middleware em `internal/web/middleware/rbac.go`
- Frontend: hook `useUserPermissions()` + componente `<ProtectedAction>` para proteger botões
- Cache: TTL de 1 hora para permissões
- Rotas destrutivas (POST/PUT/DELETE) protegidas automaticamente pelo middleware

---

## Troubleshooting Rápido

| Problema | Solução |
|----------|---------|
| Frontend não atualiza | `./rebuild-web.sh -b` + Ctrl+Shift+R |
| Build falha sem versão | `git fetch --tags --prune` |
| Race condition em testes | Verificar mutex em `internal/config/kubeconfig.go` |
| Editor YAML "apiVersion not set" | Adicionar TypeMeta antes do yaml.Marshal |
| AI Diagnostics timeout | Usar modelo llama3.2:3b (max viável com 6GB RAM) |
| Cluster inacessível | VPN ou cluster desligado — testar `kubectl cluster-info --context <name>` |
| Terminal duplica "ç" | Verificar `event.preventDefault()` antes de `ws.send()` em PodTerminal.tsx |
| Command Runner sem resposta | Verificar se SSE broker está iniciado e session ID é único |
| Dependency graph não carrega | Cytoscape requer container com dimensões definidas (não `height: 0`) |
| Certificados não listados | Verificar se secrets têm label `type: kubernetes.io/tls` |

---

## Fluxo de Desenvolvimento

### Backend (TUI ou API)
```bash
# Editar → testar → build
go test -v ./internal/... -race
make build
./build/new-k8s-hpa web -f  # testar
```

### Frontend (React)
```bash
# Dev com hot reload
cd internal/web/frontend && npm run dev  # porta 5173
# Em outro terminal:
./build/new-k8s-hpa web -f              # API na porta 8080

# Build para produção
./rebuild-web.sh -b  # + Ctrl+Shift+R no navegador
```

### Release
```bash
git tag v1.3.X && git push origin v1.3.X
make release           # binários multi-plataforma
./create-v1-release.sh # publica no GitHub
```
