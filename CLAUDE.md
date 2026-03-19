# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Versão atual: verificar com `git describe --tags --always`. Branch `integracao-dyna` está à frente do `main` com Node Pool Registry, Device Auth Grant para Gemini e correlação bidirecional K8s↔Dynatrace no Health Check (Fases 1-3 concluídas — Fase 4 pendente: análise AI em batch de CorrelatedItems).
**IMPORTANTE**: Ao fazer alterações no frontend (React/TypeScript), sempre rebuild com `./rebuild-web.sh -b` E fazer hard refresh no navegador (Ctrl+Shift+R).

---

## Documentação Modular

- [Quick Start & Features](docs/guides/QUICK_START.md)
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md)
- [Architecture Overview](docs/architecture/OVERVIEW.md)
- [Web Interface Guide](docs/guides/WEB_INTERFACE.md)
- [Common Pitfalls](docs/guides/COMMON_PITFALLS.md)
- [RBAC Azure AD](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md)
- [Changelog](docs/history/CHANGELOG.md)
- [**Plano: Dynatrace × Health Check**](docs/planning/DYNATRACE_HEALTHCHECK_INTEGRATION.md) ← work in progress

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
│   ├── dynatrace/            # Integração Dynatrace API v2 (problems, entities, metrics)
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

**K8s client cache TTL**: `clientTTL = 30min`, `clientCleanupInterval = 15min`. Valores intencionalmente baixos para liberar clients inativos — não aumentar sem motivo (cada client K8s ocupa ~5-10MB).

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

**Performance SSE**: limpeza de replay buffer pós-conclusão usa `time.AfterFunc` (nunca `go func()+time.Sleep` — goroutine leak). Cleanup de zumbis a cada **5 minutos** (`sseCleanupInterval`). Replay buffers inativos expiram após **1 hora** (`maxReplayBufferAge`).

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

### Azure CLI — Timeout Obrigatório

Todas as chamadas `exec.Command("az", ...)` devem usar `exec.CommandContext` com `context.WithTimeout`. Nunca usar `exec.Command` sem contexto — o Azure CLI pode travar indefinidamente em caso de VPN instável ou token expirado. Timeouts padrão: **30s** para operações de leitura, **60s** para `nodepool list/show`, **10min** para operações de escala.

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

`internal/ai/` suporta 5 providers: **Ollama**, **Claude** (Anthropic), **Gemini** (API Key ou Vertex AI via ADC/SSO), **OpenAI** e **Copilot** (Azure OpenAI). Configurável via `AISettingsTab.tsx`. Tokens de usuário persistidos em `internal/storage/user_tokens_store.go`. **Nunca hardcodear API keys** — sempre via storage de tokens.

**Gemini Vertex AI (SSO corporativo)**: `GeminiAuthMode = "vertex"` usa Application Default Credentials (`gcloud auth application-default login`). Requer `GeminiVertexProject` (ou env `GOOGLE_CLOUD_PROJECT`). O ADC do servidor tem prioridade sobre credenciais locais — não requer role IAM explícita se o servidor já tiver acesso.

**ADC file path**: `WriteADCFile()` em `internal/ai/google_device_auth.go` grava em `~/.k8s-hpa-manager/google_adc.json` (caminho próprio da app) — **nunca sobrescreve** `~/.config/gcloud/application_default_credentials.json`. Após gravar, define `GOOGLE_APPLICATION_CREDENTIALS` para apontar para esse arquivo.

**FallbackProvider**: quando o servidor não tem provider padrão configurado, tenta usar ADC da máquina host via `GOOGLE_APPLICATION_CREDENTIALS` ou `~/.config/gcloud/application_default_credentials.json`. Útil em ambientes de dev onde o ADC pessoal já está ativo.

**Vertex AI SSO — 3 tentativas com diagnóstico**: a lógica de inicialização do Gemini Vertex tenta até 3 vezes com logs de diagnóstico detalhados (endpoint, projeto, scopes) antes de falhar. Ver `internal/ai/gemini_provider.go`.

**Autenticação Vertex AI via Device Auth Grant (RFC 8628)**: Fluxo sem servidor de callback — obrigatório em WSL2 (loopback Linux isolado do Windows). Frontend chama `POST /ai/tokens/google-auth/start`, backend obtém `user_code` e `device_code` do Google, frontend exibe o código e `accounts.google.com/device`. Backend faz polling em `POST /ai/tokens/google-auth/poll` até receber o token. Implementado em `internal/ai/google_device_auth.go` + `internal/web/handlers/ai_tokens.go (StartGoogleDeviceAuth/PollGoogleDeviceAuth)`.

**Copilot (Azure OpenAI)**: requer `CopilotAPIKey`, `CopilotEndpoint` (ex: `https://my-resource.openai.azure.com`) e `CopilotDeployment`. Env vars: `COPILOT_API_KEY`, `COPILOT_ENDPOINT`, `COPILOT_DEPLOYMENT`.

### Dynatrace (Integração de Problems + Correlação K8s)

`internal/dynatrace/` — cliente HTTP para Dynatrace Environment API v2. `DynatraceHandler` em `internal/web/handlers/dynatrace.go` expõe:
- `GET /api/v1/dynatrace/config` — configuração atual (sem expor token)
- `POST /api/v1/dynatrace/test` — testa conectividade
- `GET /api/v1/dynatrace/problems` — lista problems OPEN (com filtro por management zone ou tag)
- `GET /api/v1/dynatrace/problems/:problemId` — detalhes de um problem
- `POST /api/v1/dynatrace/problems/:problemId/analyze` — análise AI do problem
- `GET /api/v1/dynatrace/history` — histórico de análises

Credenciais salvas via `UserTokensStore` (`DynatraceURL` + `DynatraceToken`). Fallback para env vars `DT_API_URL` e `DT_API_TOKEN`. **Atenção**: URL deve usar `*.live.dynatrace.com` (API), não `*.apps.dynatrace.com` (UI) — o client corrige automaticamente.

**Correlação K8s↔DT no Health Check** (`internal/healthcheck/correlator.go`):
- `Correlate(result)` cruza `DeploymentResults`/`HPAResults`/`EventResults` com `DynatraceResults` pelo mesmo workload (`namespace/nome`)
- `newWorkloadKey()` normaliza: lowercase + remove sufixo `:port` (DT usa `namespace/workload:8080`)
- Escalada automática: se K8s severity >= High **E** DT severity >= High → `FinalSeverity = Critical`
- Busca reversa: workloads K8s sem match DT → `SearchProblemsForWorkloads()` pesquisa por `entityName.startsWith()`
- `POST /api/v1/healthcheck/correlated/analyze` — análise AI de um `CorrelatedHealthItem`
- Frontend: 8ª aba "K8s↔DT" em `HealthCheckResultsPanel.tsx` com badges tricolores e botão "Analisar com AI"

**Node Pool Registry**: `internal/storage/nodepool_registry_store.go` — catálogo SQLite de node pools AKS por cluster (`nodepool_registry.db`). Handler em `internal/web/handlers/nodepool_registry.go`. Rotas: `GET /api/v1/nodepools/registry`, `GET /api/v1/nodepools/registry/lookup?name=<entity>`, `POST /api/v1/nodepools/registry/scan`. Usado pelo `DynatraceTab` para correlacionar entity names do padrão `aks-<nodepool>-XXXXXXXX-vmssXXXXX` com cluster/vm-size/mode. Botão "Escanear Clusters" no tab Dynatrace dispara scan em todos os clusters.

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
| Dynatrace 401/403 | Token inválido ou sem permissão `Read problems` no Dynatrace |
| Dynatrace URL errada | Usar `*.live.dynatrace.com`, não `*.apps.dynatrace.com` |
| Vertex AI sem permissão | Verificar ADC ativo: `gcloud auth application-default print-access-token`. App usa `~/.k8s-hpa-manager/google_adc.json` — checar se existe e não expirou |
| Gemini não autentica no WSL2 | OAuth loopback quebrado no WSL2 — usar Device Auth Grant (botão "Autenticar com Google" → código em `accounts.google.com/device`) |
| Node Pool Registry vazio | Clicar "Escanear Clusters" no tab Dynatrace (requer VPN + clusters acessíveis) |
| Health Check Dynatrace retorna vazio | Verificar token DT e URL. Correlação K8s↔DT requer `check_dynatrace: true` no request + token configurado em AI Settings |
| Aba K8s↔DT vazia após HC | Normal se não há workloads problemáticos — a aba só aparece com dados quando há sintomas K8s ou problems DT ativos |

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
