# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Versão atual estável: `v1.3.33`. Branch `integracao-dyna` está à frente do `main` com Node Pool Registry, Device Auth Grant para Gemini, correlação bidirecional K8s↔Dynatrace no Health Check, aba "DT Sinais" com varredura OneAgent por threshold (Fases 1-5 concluídas), aba Diagnóstico unificada na tab Dynatrace com investigação profunda (HC K8s direcionado + métricas DT + AI), GitHub Releases com SSO/SAML (org configurável via `localStorage["github_org"]`, padrão `casas-bahia`) e aba GitHub na tab Dynatrace com fallback em 3 níveis para correlação sem OneAgent. Branch `migracao-jwt` introduz autenticação JWT (Fases 1-4 concluídas): backend JWT core, middleware dual-mode, login automático Azure AD no frontend, refresh proativo (<1h para expirar) e grace period 24h no backend. Branch `finops-dynatrace` (baseado em `migracao-jwt`): Dynatrace como fonte primária de métricas históricas FinOps com Prometheus como fallback — DTEnricher batch (4 queries splitBy), PrometheusEnricher parcial, campo MetricsSource, badge DT/Prom na UI (Fases 1-4 concluídas, cheklist em FINOPS-DT-METRICS.md). New Relic planejado como camada intermediária para clusters EKS (cadeia: DT → NR → Prometheus), cheklist em FINOPS-NR-METRICS.md — `internal/newrelic/` ainda não criado. Branch `fix-auto-discovery` (baseado em `finops-dynatrace`): auto-discovery paralelo AKS+EKS concluído (Fases 1-5 de CLUSTER-DISCOVERY-PLAN.md) — struct `ClusterConfig` AKS-only, `EKSClusterConfig` em arquivo separado (`eks-clusters-config.json`), semáforos ampliados (10 clusters × 15 subscriptions), `NodeGroupProvider` interface com impl. Azure e AWS. Branch `integracao-teams`: automação de browser para extração de CHGs do Mr.ViaBot no Teams via go-rod (DOM + IndexedDB, sem HTTP direto — MCAS bloqueia) e aprovação inline via SRE Approval system (devstartcd.via.com.br) com `SreApprovalButton` inline e `ServiceNowImportModal` com aba "Teams" como padrão; busca em lote de CHGs via ServiceNow após seleção.
**IMPORTANTE**: Após `make build`, sempre reiniciar o servidor (`kill <PID> && ./build/new-k8s-hpa web -f`) — o processo não recarrega o binário automaticamente.
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
- [**Plano: FinOps Storage**](docs/planning/FINOPS_STORAGE_PLAN.md) ← ✅ CONCLUÍDA — PVCs, discos OS dos nodes, Azure Files/Blob, Relatório Executivo integrado
- [**Plano: FinOps DT Metrics**](FINOPS-DT-METRICS.md) ← ✅ Fases 1-4 concluídas — DT como fonte primária, Prometheus parcial
- [**Plano: FinOps NR Metrics**](FINOPS-NR-METRICS.md) ← work in progress — New Relic para clusters EKS (nenhuma fase iniciada)
- [**Plano: FinOps Isenções**](FINOPS-EXEMPTIONS-PLAN.md) ← work in progress — whitelist por workload com threshold de réplicas (nenhuma fase iniciada)
- [**Plano: Cluster Discovery AKS+EKS**](CLUSTER-DISCOVERY-PLAN.md) ← ✅ Fases 1-5 concluídas — discovery paralelo, config EKS separada, semáforos ampliados, frontend com badges AKS/EKS

---

## Comandos Essenciais

```bash
# Build
make build                    # Compilar backend Go
./rebuild-web.sh -b           # Build frontend + backend + reinicia servidor em background (RECOMENDADO após mudanças React)
make build-web                # Build completo (frontend + backend)

# Discovery
./build/new-k8s-hpa autodiscover   # Descobre clusters AKS+EKS em paralelo (salva configs separadas)

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
# Publicar release no GitHub (ver seção Release no Fluxo de Desenvolvimento)

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
│   │                         # + eks_config.go (EKSClusterConfig, load/save)
│   │                         # + eks_discovery.go (AutoDiscoverEKSClusters via AWS CLI)
│   ├── cloudprovider/        # Interface NodeGroupProvider + impls por cloud
│   │   ├── interface.go      # NodeGroupProvider: List/Scale/SetAutoscaling/AbortOperation
│   │   ├── azure/            # AzureNodeGroupProvider (az CLI)
│   │   └── aws/              # AWSNodeGroupProvider (aws CLI, normaliza ARN → nome curto)
│   ├── collectors/           # Coletores K8s: deployment, HPA, pod, node, investigator
│   ├── metrics/              # Cliente Prometheus (prometheus.go)
│   ├── session/              # Sessions TUI ↔ Web (formato JSON compatível)
│   ├── monitoring/           # Prometheus, predictions/, nodepoolpredictions/
│   │   └── engine/           # monitoring_v2.go — discovery automático sem port-forwards
│   ├── auth/                 # JWT: JWTManager (Generate/Validate/IsConfigured/TTL), claims email/name/is_sre
│   ├── rbac/                 # Azure AD RBAC (azure_ad.go)
│   ├── ai/                   # AI Diagnostics (Ollama/Claude/Gemini), reports/
│   ├── aierrors/             # Tipos de erro normalizados para AI providers
│   ├── sanitizer/            # Sanitização de logs antes de enviar para IA
│   ├── storage/              # SQLite: predictions.db, health_check.db, ai_diagnostics.db
│   │                         # + ai_history_store.go, dependency_registry.go, user_tokens_store.go
│   ├── certificates/         # Gerenciamento de certificados TLS
│   ├── dynatrace/            # Integração Dynatrace API v2 (problems, entities, metrics)
│   ├── servicenow/           # Integração ServiceNow
│   ├── healthcheck/          # Health checking: orchestrator, deployment/hpa/event/pv checkers
│   ├── history/              # History tracker
│   ├── logs/                 # Gerenciamento de logs da aplicação
│   ├── notifications/        # Notificações in-app e Windows (WSL2)
│   ├── sreapproval/          # Integração com sistema SRE Approval (devstartcd.via.com.br)
│   ├── teams/                # Extração de CHGs do Mr.ViaBot via browser automation (go-rod)
│   ├── updater/              # Auto-update: verificação de versão no GitHub
│   ├── validation/           # Validação de recursos K8s
│   └── pkg/
│       ├── helm/             # Cliente Helm via CLI
│       └── nexus/            # Cliente Nexus (artefatos)
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

### CloudProvider Abstraction (Node Groups)

`internal/cloudprovider/interface.go` define `NodeGroupProvider` para abstrair operações de node groups por cloud:

```go
type NodeGroupProvider interface {
    ListNodeGroups(ctx, cluster) ([]models.NodePool, error)
    ScaleNodeGroup(ctx, cluster, group string, count int) error
    SetAutoscaling(ctx, cluster, group string, enable bool, min, max int) error
    AbortOperation(ctx, cluster, group string) error  // retorna ErrNotSupported se N/A
    ValidateAuth(ctx) error
}
```

- **Azure** (`cloudprovider/azure/`): usa `az aks nodepool` CLI — mesma lógica de `buildNodePoolCommands()`, mas encapsulada.
- **AWS** (`cloudprovider/aws/`): usa `aws eks` CLI. Normaliza ARN completo → nome curto via `parseEKSClusterName()`. Região pode ser extraída do ARN se não fornecida.
- `GetNodeGroupProvider()` em `internal/config/kubeconfig.go` seleciona o provider pelo prefixo do context name: se ARN (`arn:aws:eks:...`), usa AWS; caso contrário, usa Azure.

### Config EKS Separada

Após `fix-auto-discovery`, a config de clusters foi dividida em dois arquivos:

| Arquivo | Provider | Struct |
|---------|----------|--------|
| `~/.k8s-hpa-manager/clusters-config.json` | AKS | `ClusterConfig` (Name, ResourceGroup, Subscription) |
| `~/.k8s-hpa-manager/eks-clusters-config.json` | EKS | `EKSClusterConfig` (Name, AwsRegion, AwsProfile, AccountID) |

`GetNodeGroupProvider()` lê do arquivo correto. Retrocompatibilidade: `clusters-config.json` com campos `awsRegion`/`awsProfile` é aceito como fallback até o usuário rodar o novo `autodiscover`.

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

### Variáveis de Ambiente (Auth)

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `K8S_HPA_WEB_TOKEN` | Token estático legado (backward compat) | `poc-token-123` |
| `K8S_HPA_JWT_SECRET` | Secret para assinar JWTs (mín. 32 bytes — ativa modo JWT) | não definido |
| `K8S_HPA_JWT_TTL` | TTL dos tokens JWT (ex: `8h`, `24h`) | `8h` |

### Autenticação JWT (branch `migracao-jwt`)

**Dual-mode**: quando `K8S_HPA_JWT_SECRET` está definido, o sistema opera em modo JWT; caso contrário, cai para token estático (`K8S_HPA_WEB_TOKEN`). O middleware `JWTAuthMiddleware` em `internal/web/middleware/auth.go` decide automaticamente.

**Pacote `internal/auth/`**:
- `jwt.go` — `JWTManager`: `Generate()`, `Validate()`, `IsConfigured()`, `TTL()`. Claims: `email`, `name`, `is_sre`.

**Endpoints de auth** (`internal/web/handlers/auth.go`) — sem middleware de autenticação prévia:
- `POST /auth/login` — obtém email via `az account show`, verifica grupo AD, emite JWT. Retorna `TOKEN_EXPIRED` (código `401`) quando expirado ou `JWT_NOT_CONFIGURED` (`501`) quando secret ausente.
- `POST /auth/logout` — stateless, apenas instrui o frontend a descartar o token.
- `POST /auth/refresh` — valida JWT atual, emite novo com mesmo email/isSRE sem re-consultar Azure AD.

**Frontend**:
- `Login.tsx`: tenta `/auth/login` automaticamente (modo JWT). Se receber `501 JWT_NOT_CONFIGURED`, cai para campo de token estático.
- `apiClient.isTokenExpired()`: decodifica JWT do localStorage e verifica `exp` localmente sem requisição.
- Auto-refresh: antes de cada requisição, se o token expirou, `apiClient` tenta `POST /auth/refresh`. Se falhar, dispara evento `jwt-expired` (capturado em `App.tsx` → logout).
- `App.tsx`: ouve evento `jwt-expired` via `window.addEventListener` para forçar re-login.

**WebSocket**: `WebSocketJWTAuthMiddleware` aceita JWT via query param `?token=<JWT>` (mesmo dual-mode).

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

**Investigação Profunda (`InvestigateProblem`)** em `internal/web/handlers/dynatrace.go`:
- Fluxo de identificação de cluster/namespace em 3 etapas:
  1. HOST entity → regex `aks-<pool>-XXXXXXXX-vmssXXXXX` → `LookupByNodePool` no registry
  2. Fallback keyword: `extractKeywords(mgmtZones...)` → `LookupByKeyword` (LIKE) → `pickEntryByEnv` escolhe cluster compatível com `extractEnvHint(problem)`
  3. Fallback namespace: `FindNamespaceByKeywords` lista namespaces K8s, filtra por env token exato, pontua por keyword match + bônus de env
- **`extractEnvHint`**: retorna `"prd"` por padrão se nenhum token não-prd (hlg/sit/stg/hml/uat/dev) aparecer no problema DT — nunca analisa cluster hlg para problem sem marcador de ambiente
- **`extractKeywords`**: normaliza acentos PT/ES para ASCII (`"Cálculo"` → `"calculo"`) antes da busca — K8s só aceita nomes ASCII
- **`pickEntryByEnv`**: dentre resultados do registry, prefere cluster com env token igual ao envHint (`"prd"` → escolhe cluster prd, nunca hlg)
- Janela padrão para problems fechados: `now-4h` (API DT usa `now-2h` por padrão — insuficiente)

**Node Pool Registry**: `internal/storage/nodepool_registry_store.go` — catálogo SQLite de node pools AKS por cluster (`nodepool_registry.db`). Handler em `internal/web/handlers/nodepool_registry.go`. Rotas: `GET /api/v1/nodepools/registry`, `GET /api/v1/nodepools/registry/lookup?name=<entity>`, `POST /api/v1/nodepools/registry/scan`. Usado pelo `DynatraceTab` para correlacionar entity names do padrão `aks-<nodepool>-XXXXXXXX-vmssXXXXX` com cluster/vm-size/mode. Botão "Escanear Clusters" no tab Dynatrace dispara scan em todos os clusters.

**GitHub Releases (SSO/SAML)**:
- Autenticação via RBAC Azure AD: email injetado automaticamente pelo middleware `InjectUserEmail` — sem campo de email manual
- Org configurável via `localStorage["github_org"]` (padrão `casas-bahia`). Editar no modal de credenciais GitHub
- PATs precisam ter SSO autorizado: Classic (`ghp_*`) → "Configure SSO" no GitHub; Fine-grained (`github_pat_*`) → criar com org autorizada
- `apiClient.getGitHubOrg()` / `setGitHubOrg()` em `internal/web/frontend/src/lib/api/client.ts`
- ServiceNow: regex de repositório usa `[^/]+` (qualquer org) — não hardcodado. Fallback: se não extrai `github_repo`, usa `deploymentName`

**DynatraceGitHubSection** (`DynatraceGitHubSection.tsx`) — fallback em 3 níveis para correlação K8s↔GitHub sem OneAgent:
1. `k8sWorkloads[].AppName` (OneAgent DTLabels) — mais preciso
2. `k8sWorkloads[].Workload` sem AppName — busca no registry por nome do deployment
3. `affectedEntities[].k8sWorkload` — entidades impactadas com info K8s
- Versão: usa `DeploymentConfig.version` do registry quando DT não tem `AppVersion`
- Requer scan prévio na aba GitHub Releases para popular o registry

**EntityMetricsSection** (`DynatraceMetricsPanel.tsx`) — prop `columns?: 1 | 2`:
- `columns=1` (padrão): layout vertical, tab Métricas
- `columns=2`: grid 2 colunas, tab Diagnóstico — P50/P90/P95/P99 agrupados num único chart
- Fallback: métricas fora dos grupos predefinidos são exibidas como charts individuais genéricos

**Export PDF** (`exportPDF` em `DynatraceTab.tsx`):
- `sanitizePDF()` substitui todos os caracteres Unicode fora do WinAnsi antes de passar ao jsPDF
- Sem isso, caracteres como `═══`, `→`, `—`, `•` geram `%P%P%P` no documento
- `removeEmojis()` mantido apenas para uso fora do PDF

### FinOps — Storage & Relatório Executivo

**Tab FinOps possui 7 abas** (ordem): Dashboard → Node Pools → Workloads → HPA Histórico → Armazenamento → Oportunidades → Relatório

**`StorageTab`** (`FinOpsTab.tsx`):
- 4 KPI cards: Custo Total Storage, Nº PVCs, Disco OS R$/mês, Custo Orfãos
- BarChart de custo por tipo de storage (Premium SSD, Standard SSD, Azure Files, etc.)
- Tabela de PVCs com filtro por namespace/tipo, ordenação, badge "orfão" vermelho
- Seção "PVCs Orfãos" colapsável com sugestão `kubectl delete pvc` e botão de cópia
- Aba só aparece quando `report.storage` presente (feature flag implícita)

**`RelatorioTab`** (`FinOpsTab.tsx`):
- Pie chart com composição de custo total: Compute Produtivo + Disco OS + PVCs Ativos + Desperdício (superprovisioning + orfãos)
- Findings priorizados (`critical`/`high`/`medium`) com evidência e ponteiro para a aba correta de ação
- Top 10 workloads por custo total (compute + storage)
- Botão "Exportar PDF": usa `html2canvas` + `jsPDF` (imports dinâmicos); divide canvas em fatias de página A4; arquivo gerado: `finops-relatorio-<cluster>-<date>.pdf`

**Recharts — `renderPieLabel`** (padrão para labels externas em `PieChart`):
- Função retorna `<g>` com dois `<text>` (nome curto + valor/percentual)
- Segmentos < 4% usam `outerRadius + 50` (evita label cair dentro do donut hole)
- `labelLine` deve ser objeto SVG `{ stroke, strokeWidth, opacity }` — **não função** (causa falha de renderização)
- `cursor={{ fill: 'transparent' }}` no `<Tooltip>` para suprimir fundo cinza no hover

**FinOps — cadeia de métricas históricas**: `MetricsSource` em `FinOpsWorkload` indica a fonte usada: `"dynatrace"` (AKS primário), `"newrelic"` (EKS — planejado, `internal/newrelic/` ainda não existe), `""` (sem dados). Cadeia final planejada: DT → NR → Prometheus. Badge cores na UI: DT=azul, NR=âmbar, Prom=laranja.

**FinOps Prometheus**: checkbox "Análise histórica Prometheus" é **`true` por padrão** (era `false`)

**Backend storage** (`internal/finops/`):
- `azure_disk_pricing.go`: `DiskPricer` com cache SQLite + fallback hardcoded por tier (P/E/S series, Azure Files, Blob)
- `storage_calculator.go`: `StorageCalculator.Calculate()` lista PVCs, correlaciona com workloads via ownerRef chain, calcula custo por tier ou por GB
- `storage_calculator_test.go`: 7 funções de teste — `MapStorageClassToAzureType`, `ResolveManagedDiskTier`, custo PVC, Files/Blob, `buildStorageSummary`, detecção de orfãos

**`buildFinOpsPrompt`** (`internal/web/handlers/finops.go`): seção `=== ARMAZENAMENTO ===` com total storage, breakdown por tipo, top 5 workloads por storage, PVCs orfãos com Retain policy destacados

### Conntrack Viewer (Node Pools)

`internal/web/handlers/nodepools_conntrack.go` — dois endpoints:
- `GET /api/v1/nodepools/conntrack` — snapshot atual via `exec` em pod efêmero com `hostNetwork:true`, lê `/proc/net/nf_conntrack` (conta linhas) e `/proc/sys/net/netfilter/nf_conntrack_max` + `nf_conntrack_buckets`
- `GET /api/v1/nodepools/conntrack/history/:node` — histórico 24h via Prometheus (`node_nf_conntrack_entries`, `node_nf_conntrack_max`), retorna array de pontos time-series

**Cache**: snapshot fica em memória por 5 minutos por nó (`conntrackCache` + `conntrackCacheTTL`). Evitar exec repetitivo; pods efêmeros são caros em custo de scheduling.

**Fallback gracioso**: se Prometheus indisponível, histórico retorna array vazio — frontend exibe apenas snapshot atual sem mensagem de erro ao usuário.

**Frontend**: `ConntrackViewerTab.tsx` — BarChart comparando snapshot atual vs histórico 24h. Recomendação automática por nó: OK / Monitorar tendência / Spike ativo / Aumentar limite.

### shadcn Tabs em Modais com Altura Fixa

`TabsContent` do Radix UI usa `[data-state=active]:block` — o `display: block` quebra qualquer cadeia de `flex-1 min-h-0`. **Nunca usar shadcn `<Tabs>` em contextos onde a aba precisa preencher altura restante** (ex: modais, painéis com `flex-1`).

**Solução**: implementação manual com `div` + estado local + renderização condicional:

```tsx
// ✅ CORRETO — controle total do flex chain
const [activeTab, setActiveTab] = useState<"details" | "logs">("details");

<div className="flex-1 flex flex-col min-h-0">
  <div className="flex border-b border-border px-4 pt-3 gap-1 flex-shrink-0">
    {(["details", "logs"] as const).map(tab => (
      <button key={tab} onClick={() => setActiveTab(tab)}
        className={`px-3 py-1.5 text-xs font-medium transition-colors border-b-2 -mb-px ${
          activeTab === tab ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
        }`}>
        {tab === "details" ? "Detalhes" : "Logs"}
      </button>
    ))}
  </div>
  {activeTab === "details" && (
    <div className="flex-1 min-h-0 overflow-y-auto">...</div>
  )}
  {activeTab === "logs" && (
    <div className="flex-1 flex flex-col min-h-0">...</div>
  )}
</div>
```

Ver implementação em `PodQuickViewModal.tsx`.

### MonitorUtils — Conversão de Recursos K8s

`internal/web/frontend/src/lib/monitorUtils.ts` centraliza funções de formatação e parsing de recursos K8s:

- `parseCpuToMillicores(s)` — converte `"300m"` → 300, `"1"` → 1000, `"0.5"` → 500
- `parseMemoryToBytes(s)` — converte `"500Mi"`, `"4Gi"`, `"1024Ki"`, `"1G"` para bytes
- `formatMillicores(m)` — formata millicores para exibição (`"250m"`, `"1.5"`)
- `formatBytes(b)` — formata bytes para exibição (`"128Mi"`, `"2.50Gi"`)

Usar essas funções ao calcular percentuais de uso vs. limit/request. **Nunca calcular percentuais inline em componentes** — usar os parsers do `monitorUtils`.

### ServiceNow — Rod (Go nativo) + WSL2 CDP

`internal/servicenow/` — extração de CHGs via browser automation com **go-rod v0.116.2** (Go nativo, sem Node.js/npm). Suporta autenticação SAML/SSO do Azure AD com persistência de sessão.

**Dois modos de execução** (selecionados automaticamente por `NeedsWindowsBrowser()`):
- **Modo local**: Chromium baixado automaticamente pelo Rod (`launcher.New()`). Sessão em `~/.k8s-hpa-manager/rod-session/`.
- **Modo Windows/WSL2**: Chrome/Edge do Windows via CDP na porta **`9223`** (não 9222 — evita conflito com instâncias existentes). Rod conecta em `ws://<windows-host>:9223`. Sessão no caminho Windows configurado em `BrowserConfig.WindowsSessionDir`.

**Precedência de `NeedsWindowsBrowser()`:**
1. Env var `K8S_HPA_WINDOWS_BROWSER=true` — força modo Windows
2. Config persistida em `~/.k8s-hpa-manager/servicenow-browser.json`
3. Auto-detect: WSL sem display gráfico (`DISPLAY`/`WAYLAND_DISPLAY` vazios)

**Sessão Azure AD**: expira em ~8h. `RodExtractor.GetSessionStatus()` valida pelo timestamp de modificação do diretório. `ClearSession()` remove e recria o diretório vazio.

**Endpoints de gerenciamento de sessão:**
- `GET /api/v1/servicenow/session-status` — status da sessão atual
- `DELETE /api/v1/servicenow/session` — limpar sessão
- `POST /api/v1/servicenow/session/test` — testar autenticação
- `GET/PUT /api/v1/servicenow/browser-config` — ler/gravar `BrowserConfig`

**Compatibilidade de frontend**: `RodExtractor.GetStatus()` retorna campos `playwright_configured`/`script_exists` como `true` para não quebrar o frontend legado (que esperava Playwright).

### Teams Mr.ViaBot + SRE Approval (branch `integracao-teams`)

**`internal/teams/`** — extrai CHGs de aprovação SRE das mensagens do Mr.ViaBot no Microsoft Teams via automação de browser (go-rod). O acesso HTTP direto ao `chatsvcagg` é bloqueado pelo MCAS (Microsoft Cloud App Security) — a extração ocorre inteiramente via DOM JS e IndexedDB do browser.

**Dois mecanismos de extração** (aplicados em ordem):
1. **DOM**: seletores CSS em `[data-tid="messageBody"]` e similares. Fallback: percorre leaf nodes com regex `CHG\d{5,}`, sobe a árvore DOM até achar container com `sre-approval` (max 15 ancestors), deduplica por substring.
2. **IndexedDB**: varre `conversation-manager:react-web-client`, `chat-info-pane-manager` e `skypexspaces` — busca keywords (`chg0`, `sre-approval`, `viabot`) e thread IDs do formato `19:...@thread.v2`.

**SkypeToken**: capturado do CDP Network (`X-Skypetoken` ou `authorization: skype_token`) antes do body da resposta. Fallback: `localStorage`/`sessionStorage` após carga do Teams. Necessário apenas para o endpoint HTTP de fallback (que falha com MCAS mesmo com token).

**Sessões separadas**:
- `~/.k8s-hpa-manager/teams-session/` — perfil Chrome para Teams (go-rod). **Nunca misturar com `rod-session`** do ServiceNow — perfis Chrome incompatíveis corrompem um ao outro.
- `~/.k8s-hpa-manager/teams-cache/approvals-cache.json` — cache de CHGs em disco. Persiste 48h por merge; `needs_refresh` na resposta JSON é apenas indicativo (não oculta dados).

**Refresh é síncrono e lento** (`POST /api/v1/teams/approvals/refresh`): abre o Chrome, navega para `teams.microsoft.com/v2/`, aguarda carregamento (~2min max), navega para o chat do Mr.ViaBot via hash SPA `#/conversations/<threadID>`, extrai o DOM e fecha. Pode levar **~90s**. O handler bloqueia e retorna `409 Conflict` se já houver extração em andamento (`h.refreshing`).

**Navegação Teams v2 + MCAS**: o redirect `teams.microsoft.com → teams.microsoft.com.mcas.ms` é automático. O `RunDiscovery` monitora novas abas (`browser.Pages()` a cada 3s) e anexa listeners CDP a cada aba com URL do Teams — necessário porque o v2 pode abrir em aba separada.

**Thread ID do Mr.ViaBot** é hardcoded em `discover.go` e `extractor.go`: `19:eab1be93-5589-4a3f-9f47-d6cfcbc50a0c_61740f97-9be2-4459-b054-5230364585a7@unq.gbl.spaces`. Se o bot mudar de conta, atualizar ambos os arquivos.

**`internal/sreapproval/`** — aprovação de deployments em `https://devstartcd.via.com.br`. Fluxo CSRF-aware: GET página → `cookiejar` mantém sessão → extrai campos `<input type="hidden">` e `<form action>` → POST com `email`. Detecta `já foi finalizada` no HTML e retorna `*ErrAlreadyFinalized{ApproverEmail, ApproverSquad}` — o handler retorna `200 OK` com `already_finalized: true` (não erro HTTP).

**Endpoints Teams**:
- `GET /api/v1/teams/approvals/today` — CHGs do dia (filtro por `ExtractedAt.YearDay`)
- `GET /api/v1/teams/approvals/search?chg=CHG0455046` — busca no cache 48h (resposta em ms)
- `POST /api/v1/teams/approvals/refresh` — extração completa (~90s, bloqueante)

**Endpoints SRE Approval**:
- `GET /api/v1/sre-approval/info?url=...` — scraping HTML da página de aprovação
- `POST /api/v1/sre-approval/approve` — submete aprovação (**requer `RequireSREGroup()`**)
- `GET /api/v1/sre-approval/extract-id?url=...` — extrai ID da URL
- `GET /api/v1/sre-approval/current-user` — email via `az account show`

**`SreApprovalButton.tsx`**: botão inline no header do Health Check SRE. Chama `getSreApprovalInfo()` automaticamente no mount (sem click). Exibe email do aprovador quando `finalized`. Obtém email do usuário logado via `/sre-approval/current-user` antes de aprovar.

**`ServiceNowImportModal.tsx`**: modal com 3 abas — **"Teams (Mr.ViaBot)"** (padrão), "Playwright/Rod" e "Manual". A aba Teams carrega `getTeamsApprovalsToday()` na abertura e permite selecionar CHGs para extração em lote via ServiceNow.

**`internal/teams/testdata/`** está no `.gitignore` — contém tokens de sessão capturados durante debug.
### Certificates

`internal/certificates/` + `internal/web/handlers/certificates.go`: discovery de certs TLS em secrets K8s, validação de expiração, import/export. Usar para qualquer operação envolvendo TLS no cluster.

---

## RBAC Azure AD

- **Grupo SRE**: `VV_CLOUD_SRE` (ID: `eb865ea5-2672-49be-abc8-74c248c556b0`)
- Backend: `internal/rbac/azure_ad.go` + middleware em `internal/web/middleware/rbac.go` (RBAC de grupo). Auth de request: `internal/web/middleware/auth.go` (`JWTAuthMiddleware`)
- Frontend: hook `useUserPermissions()` + componente `<ProtectedAction>` para proteger botões
- Cache: TTL de 1 hora para permissões
- Rotas destrutivas (POST/PUT/DELETE) protegidas automaticamente pelo middleware

---

## Troubleshooting Rápido

| Problema | Solução |
|----------|---------|
| JWT: login retorna 501 | `K8S_HPA_JWT_SECRET` não definido no servidor — frontend cai para token estático automaticamente |
| JWT: login retorna "AZ_CLI_ERROR" | Azure CLI não autenticado no servidor — executar `az login` no servidor |
| JWT: token expira antes do esperado | Verificar `K8S_HPA_JWT_TTL` (padrão `8h`); refresh automático falhou — verificar logs do servidor |
| JWT: frontend em loop de login | `isTokenExpired()` retornando true mas refresh retornando 401 — limpar `localStorage` manualmente |
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
| Investigação profunda analisa cluster hlg sendo problem de prd | `extractEnvHint` retornou non-prd indevidamente — verificar se management zone ou título tem token "hlg"/"sit" como substring de outra palavra (ex: "transition") |
| Investigação profunda não identifica namespace | Keywords com acentos não batiam em nomes K8s (ASCII). Verificar se management zone tem Unicode; `extractKeywords` normaliza automaticamente desde da9a99b |
| Mudanças no backend não tomam efeito | Servidor não foi reiniciado — `make build` só gera o binário; o processo em execução não recarrega. Matar e reiniciar manualmente |
| Aba GitHub (DynatraceTab) vazia | Sem k8sWorkloads nem affectedEntities com info K8s no problem DT. Executar scan na aba GitHub Releases para popular o registry |
| GitHub Releases: erro "token SAML" | PAT não tem SSO autorizado para a org. Classic PAT: GitHub → Settings → PAT → Configure SSO. Fine-grained: criar novo com org selecionada |
| GitHub Releases: org errada | Editar org no modal de credenciais GitHub (ícone de perfil). Padrão: `casas-bahia` |
| Export PDF com "%P%P%P" no texto | Caracteres Unicode fora do WinAnsi na resposta da IA (═══, →, —). `sanitizePDF()` já converte — verificar se está sendo chamada em todos os `doc.text()` dentro de `exportPDF` |
| Tab detalhes de modal não preenche a altura | shadcn `<Tabs>` usa `display:block` que quebra `flex-1 min-h-0`. Substituir por implementação manual de tabs com `div` + estado local (ver `PodQuickViewModal.tsx`) |
| Colunas CPU/MEM no monitor se movem juntas | Data cells com `text-right` criam ilusão de movimento ao arrastar — manter alinhamento à esquerda nas células de dados |
| Conntrack: snapshot sempre vazio | Pod efêmero precisa de permissão `hostNetwork: true` e acesso ao node. Verificar se o cluster permite pods privilegiados |
| Conntrack: histórico não carrega | Prometheus indisponível — comportamento esperado (fallback gracioso). Verificar URL do Prometheus em `/api/v1/monitoring/v2/` |
| ServiceNow não abre no navegador (WSL2) | CDP não conectou na porta 9223. Iniciar Chrome com `--remote-debugging-port=9223` ou verificar firewall WSL2. Ver `WindowsCDPPort` em `wsl_browser.go` |
| ServiceNow extrai mas não autentica | Sessão expirada (>8h). Limpar sessão via `DELETE /api/v1/servicenow/session` e re-autenticar no Chrome Windows |
| Teams: refresh retorna 409 Conflict | Extração já em andamento — aguardar os ~90s ou reiniciar o servidor |
| Teams: CHGs não aparecem (DOM vazio) | Mr.ViaBot não carregou no prazo. Verificar se a sessão `~/.k8s-hpa-manager/teams-session/` está válida (Azure AD pode ter expirado — deletar a pasta para re-autenticar na próxima abertura do Chrome) |
| Teams: thread ID do Mr.ViaBot mudou | Atualizar constante `mrViaBotThreadID` em ambos `discover.go` e `extractor.go` |
| Teams: Chrome não abre (WSL2 sem display) | Adicionar `--no-sandbox` e verificar se há Chrome instalado em `/usr/bin/google-chrome*`. Rod tenta Chromium como fallback mas pode não estar disponível |
| SRE Approval: aprovação retorna "já finalizada" mas é 200 | Comportamento correto — `ErrAlreadyFinalized` retorna `already_finalized: true` na resposta JSON com o email do aprovador original |
| SreApprovalButton não carrega status | `getSreApprovalInfo` falhou silenciosamente — inspecionar o response de `/api/v1/sre-approval/info?url=...`. A página `devstartcd.via.com.br` pode estar inacessível fora da rede corporativa |

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
# Merge branch → main
git checkout main && git merge --no-ff <branch> && git push origin main

# Tag e push
git tag v1.3.X && git push origin v1.3.X

# Build multi-plataforma
make release   # gera: build/new-k8s-hpa-linux-amd64, darwin-amd64, darwin-arm64

# Criar release no GitHub (com upload de binários)
gh release create v1.3.X \
  build/new-k8s-hpa-linux-amd64 \
  build/new-k8s-hpa-darwin-amd64 \
  build/new-k8s-hpa-darwin-arm64 \
  --title "v1.3.X" \
  --notes "Descrição das mudanças"
```

> `create-v1-release.sh` era específico para v1.0.0 — **não usar** para releases correntes.
