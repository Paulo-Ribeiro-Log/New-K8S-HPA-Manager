# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**IMPORTANTE**: Responda sempre em português brasileiro (pt-br).
**IMPORTANTE**: Mensagens de commit (git commit) devem ser sempre em português brasileiro.
**IMPORTANTE**: Mantenha o foco na filosofia KISS.
**IMPORTANTE**: Sempre compile o build em ./build/ - usar `./build/new-k8s-hpa` para executar a aplicação.
**IMPORTANTE**: Estado atual do `main` (resumo): JSON Inspector inline nos visualizadores de log, WIF SSO + OAuth2 app-callback para Gemini Vertex AI, RBAC K8s via `SelfSubjectRulesReview`, Editor de Código Web (Fases 1-10), Diagnóstico SNAT multi-cloud (AKS/GKE/EKS), HPAEditor standalone, RBAC simplificado (`OptionalSRECheck` sempre `isSRE=true`). Branch atual em trabalho: `ajuste-aba-dynatrace` (ver [Plano: Dynatrace — Diagnóstico Acionável](DYNATRACE-DIAGNOSTICS-PLAN.md)). Histórico técnico detalhado de cada branch de feature (o que foi implementado, decisões, bugs corrigidos) → **[docs/history/BRANCH-HISTORY.md](docs/history/BRANCH-HISTORY.md)**.

Ver `ACCESS-CHECK-PLAN.md` para o histórico completo de decisões e comandos `az`/`kubectl` de validação (Revisão 7 do scan de frota).

**IMPORTANTE**: Após `make build`, sempre reiniciar o servidor (`kill <PID> && ./build/new-k8s-hpa web -f`) — o processo não recarrega o binário automaticamente.
**IMPORTANTE**: Ao fazer alterações no frontend (React/TypeScript), sempre rebuild com `./rebuild-web.sh -b` E fazer hard refresh no navegador (Ctrl+Shift+R).

---

## Documentação Modular

- [Quick Start & Features](docs/guides/QUICK_START.md)
- [Development Commands](docs/guides/DEVELOPMENT_COMMANDS.md)
- [Architecture Overview](docs/architecture/OVERVIEW.md)
- [Web Interface Guide](docs/guides/WEB_INTERFACE.md)
- [Common Pitfalls](docs/guides/COMMON_PITFALLS.md)
- [Troubleshooting Completo](docs/guides/TROUBLESHOOTING.md)
- [RBAC Azure AD](docs/guides/RBAC_AZURE_AD_IMPLEMENTATION.md)
- [Changelog](docs/history/CHANGELOG.md)
- [Histórico de Branches (detalhado por feature)](docs/history/BRANCH-HISTORY.md)
- [**Plano: Dynatrace × Health Check**](docs/planning/DYNATRACE_HEALTHCHECK_INTEGRATION.md) ← work in progress
- [**Plano: FinOps Storage**](docs/planning/FINOPS_STORAGE_PLAN.md) ← ✅ CONCLUÍDA — PVCs, discos OS dos nodes, Azure Files/Blob, Relatório Executivo integrado
- [**Plano: FinOps DT Metrics**](FINOPS-DT-METRICS.md) ← ✅ Fases 1-4 concluídas — DT como fonte primária, Prometheus parcial
- [**Plano: FinOps NR Metrics**](FINOPS-NR-METRICS.md) ← work in progress — New Relic para clusters EKS (nenhuma fase iniciada)
- [**Plano: FinOps Isenções**](FINOPS-EXEMPTIONS-PLAN.md) ← work in progress — whitelist por workload com threshold de réplicas (nenhuma fase iniciada)
- [**Plano: Cluster Discovery AKS+EKS**](CLUSTER-DISCOVERY-PLAN.md) ← ✅ Fases 1-5 concluídas — discovery paralelo, config EKS separada, semáforos ampliados, frontend com badges AKS/EKS
- [**Plano: Verificar Acesso (Access Checker)**](ACCESS-CHECK-PLAN.md) ← ⚠️ Revisão 7 pendente de validação real — checa acesso de analista via impersonation K8s + grupos AAD `VV_CLOUD*` resolvidos por `az ad user get-member-groups` (sem Graph API); detecta também acesso admin via IAM do Azure (bypass de RBAC, invisível à impersonation); scan de frota usa `SelfSubjectAccessReview` varrendo todos os namespaces
- [**Plano: Dynatrace — Diagnóstico Acionável**](DYNATRACE-DIAGNOSTICS-PLAN.md) ← ✅ Fase 1 concluída — pacote `internal/actionrules/` unifica regras threshold→ação (fonte única de verdade); CPU throttle usa `builtin:kubernetes.workload.cpu_throttled` (métrica antiga estava quebrada/404). Fase 2 (navegação das sugestões) e Fase 3 (distributed tracing) aguardando
- [**Plano: Teste de Latência sob Demanda**](LATENCY-METRICS-PLAN.md) ← ✅ Fases 1-7 concluídas (Fases 1-6 validadas ponta a ponta em cluster AKS real; Fase 7 só por teste unitário) — teste ativo via pod efêmero + guardrails + contexto histórico DT/Prometheus (Istio) + topologia Cytoscape.js + correlação de breach de latência no Health Check

---

## Comandos Essenciais

```bash
# Build
make build                    # Compilar backend Go (BUILD_PARALLEL=2 por padrão — WSL2 RAM)
make build BUILD_PARALLEL=4   # Override parallelismo se RAM disponível (>8GB livres)
./rebuild-web.sh -b           # Build frontend + backend + reinicia servidor em background (RECOMENDADO após mudanças React)
./rebuild-web.sh -n -b        # Reinicia servidor em background SEM rebuild (apenas restart)
./rebuild-web.sh -k           # Apenas mata o processo na porta 8080
./rebuild-web.sh -s           # Verifica se o servidor está rodando
./rebuild-web.sh -b --ai-provider ollama --ollama-model llama3.2:3b  # Com AI provider
make build-web                # Build completo (frontend + backend)

# Discovery
./build/new-k8s-hpa autodiscover   # Descobre clusters AKS+EKS+GKE em paralelo (salva configs separadas)

# Run
./build/new-k8s-hpa web       # Servidor web (porta 8080)
./build/new-k8s-hpa web -f    # Foreground mode (logs no terminal)
./build/new-k8s-hpa web --ad  # EMERGÊNCIA: Bypass RBAC (flag oculta)

# Dev
make web-dev                  # Frontend dev server (Vite HMR - porta 5173)
make run-dev                  # TUI com debug

# Tests
go test -v ./internal/... -race              # Todos os testes com race detector
go test -v ./internal/healthcheck/... -race  # Pacote específico
go test -run TestGetClient ./internal/...    # Função específica em todos os pacotes
./testes/test-rbac.sh                        # Suite completa RBAC (40+ cenários)

# Debug
tail -f /tmp/k8s-hpa-manager-web-*.log  # Logs do servidor

# Release
make release                  # Build multi-plataforma → build/release/ (linux, darwin Intel, darwin ARM64)
make build-all                # Build multi-plataforma → build/ (sem subpasta release)
# Publicar release no GitHub (ver seção Release no Fluxo de Desenvolvimento)

# Outros
make test-coverage            # Testes com cobertura HTML
make web-install              # npm install no frontend
make web-clean                # Limpa arquivos de build frontend

# Lint frontend
cd internal/web/frontend && npm run lint   # eslint .
```

### Notas de Build

- **`makefile` usa nome em minúsculas** — não `Makefile`. Ferramentas que procuram `Makefile` com M maiúsculo não encontrarão.
- **GOCACHE** redirecionado para `~/.cache/go-build-wsl` (não `/dev/shm`) — evita OOM em WSL2. Auto-trimado ao passar 1500MB. Para limpar: `go clean -cache`.
- **GOTMPDIR** usa `/tmp/go-tmp` — mesmo motivo (evita consumir RAM pura via `/dev/shm`).

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
| Backend | Go 1.25.0, client-go v0.34.1, Gin v1.11.0 |
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

**Padrão: cache de chamadas de CLI externa (az/gcloud/aws)**: qualquer wrapper de CLI cloud chamado no hot path de uma requisição deve ser cacheado em memória com mutex + TTL curto — subprocessos custam 1-3s e são invocados por requisição sem isso. Exemplos existentes: `restConfigEntry` (`kubeconfig.go`, 40min GKE/30min outros), `IsGcloudAuthActive` (`internal/cloudprovider/gcp/auth.go`, 5min), cache de `ListNodeGroups` (2min), `checkReachability` — probe TCP de 3s cacheado por 15s para detectar VPN/rede fora do ar sem pagar o timeout completo de 30s do client K8s. Seguir esse padrão (não chamar CLI direto a cada request) ao adicionar novas integrações cloud.

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

### Frontend — Roteamento SPA (App.tsx)

A SPA usa `react-router-dom`. Rotas definidas em `internal/web/frontend/src/App.tsx`:

| Rota | Componente | Uso |
|------|------------|-----|
| `/login` | `Login.tsx` | Autenticação (JWT ou token estático) |
| `/` | `Index.tsx` | App principal — toda a navegação por tabs |
| `/alerts/:cluster` | `AlertsPage.tsx` | Alertas do cluster |
| `/alerts/:cluster/:namespace/:hpaName` | `AlertsPage.tsx` | Alertas de HPA específico |
| `/ai-analysis/:id` | `AIAnalysisPage.tsx` | Relatório de análise AI salvo |

Todo o estado da aplicação vive em `Index.tsx` (`activeTab` string). Não há rotas para as tabs individuais — a navegação entre tabs é puro estado React.

### Frontend — Sistema de Tabs (Index.tsx)

`activeTab` é uma string que determina o conteúdo renderizado. Dois menus alimentam mudanças de tab:

**`WorkloadMenu`** (Workloads dropdown): `configmaps`, `ingresses`, `gateways`, `secrets`, `deployments`, `daemonsets`, `statefulsets`, `vpas`, `services`, `containers`, `pods`, `events`, `cronjobs`, `namespaces`, `helm`, `prometheus`

**`ToolsMenu`** (Tools dropdown): `monitoring`, `servicemesh`, `healthcheck`, `nexus-values`, `ai-diagnostics`, `github-releases`, `dependencies`, `certificates`, `resource-compare`, `command-runner`, `dynatrace`, `finops`, `teams-broadcast`, `access-check`, `latency-test`

**Tabs principais** (TabNavigation): `dashboard`, `hpa`, `nodepools`, `explorer`, `code-editor`

**Dois padrões de renderização** em `Index.tsx`:
```tsx
// Padrão 1 — display:none (tabs pesadas que ficam montadas em background):
// pods, configmaps, deployments, secrets, containers, ingresses, gateways, healthcheck, code-editor
<div style={{ display: activeTab === "pods" ? "block" : "none" }}>
  {(activeTab === "pods" || hasBeenMounted.current.pods) && <PodsPanel />}
</div>

// Padrão 2 — renderização condicional via renderTabContent() switch/case:
// Todas as outras tabs — são desmontadas quando inativas
```

O `hasBeenMounted` ref garante que tabs pesadas só sejam montadas na primeira visita, mas permanecem no DOM depois (evita perda de estado local e re-fetches).

### Frontend — Contexts

**`StagingContext`** (`src/contexts/StagingContext.tsx`): gerencia o "staging" de mudanças pendentes em HPAs e Node Pools antes do apply em lote. Expõe `addToStaging()`, `removeFromStaging()`, `applyAll()`. O contador de mudanças pendentes é exibido no header. Acessível via `useStagingContext()`.

**`TabContext`** (`src/contexts/TabContext.tsx`): gerencia o sistema multi-cluster (abas de browser `ClusterTabs`). Cada aba tem seu próprio `pageState` com `selectedCluster`, `selectedNamespace`, `activeTab`, `pendingChanges`, etc. Permite abrir o mesmo cluster em múltiplas abas com estados independentes. Acessível via `useTabContext()`.

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
- **GCP** (`cloudprovider/gcp/`): usa `gcloud container node-pools` CLI. `GCPAuthManager` gerencia Device Auth Grant (RFC 8628) para autenticação sem gcloud local. `GetFreshGKEToken()` obtém access token via ADC salvo ou `gcloud auth print-access-token` (cache 45min).
- `GetNodeGroupProvider()` em `internal/config/kubeconfig.go` seleciona o provider pelo prefixo do context name: `arn:aws:eks:...` → AWS; `gke_...` → GCP; demais → Azure.

### Configs de Cluster Separadas por Provider

A config de clusters é dividida em arquivos separados por provider:

| Arquivo | Provider | Struct |
|---------|----------|--------|
| `~/.k8s-hpa-manager/clusters-config.json` | AKS | `ClusterConfig` (Name, ResourceGroup, Subscription) |
| `~/.k8s-hpa-manager/eks-clusters-config.json` | EKS | `EKSClusterConfig` (Name, AwsRegion, AwsProfile, AccountID) |
| `~/.k8s-hpa-manager/gke-clusters-config.json` | GKE | `GKEClusterConfig` (Name, ProjectID, Region) |
| `~/.k8s-hpa-manager/gcp-adc.json` | GKE auth | ADC JSON (client_id, client_secret, refresh_token) |

`GetNodeGroupProvider()` lê do arquivo correto. Retrocompatibilidade: `clusters-config.json` com campos `awsRegion`/`awsProfile` é aceito como fallback até o usuário rodar o novo `autodiscover`.

### GKE — Autenticação e Leitura de Workloads (branch `ajustes-gcp`)

**Problema**: clusters GKE autorizados não retornavam workloads (deployments, ingress, HPAs) porque `GetRestConfig()` não tinha tratamento GKE equivalente ao EKS. Com `USE_GKE_GCLOUD_AUTH_PLUGIN=True` setado pelo `EnsureGKEAuthPlugin()`, o kubeconfig exige o plugin, que pode não estar instalado.

**Solução**: `GetRestConfig()` detecta clusters GKE (`gke_` prefix no context name) e injeta um `BearerToken` obtido via `GetFreshGKEToken()`:
1. Tenta `~/.k8s-hpa-manager/gcp-adc.json` → troca `refresh_token` por access token via `https://oauth2.googleapis.com/token`
2. Fallback: `gcloud auth print-access-token` se gcloud estiver no PATH e autenticado
3. Cache em memória de 45min (tokens GCP duram 1h)
4. Se nenhum método funcionar, deixa o kubeconfig como está (funciona se `gke-gcloud-auth-plugin` estiver instalado)

**Device Auth Grant para autodiscovery GKE** (`internal/cloudprovider/gcp/auth.go`):
- `GCPAuthManager.StartLogin()` → chama `ai.StartDeviceAuth()` → obtém `user_code` + `verify_url`
- Frontend (`AutoDiscoverDialog.tsx`) exibe código e link para `accounts.google.com/device`
- `GCPAuthManager.PollStatus()` verifica se o token chegou (non-blocking channel)
- Após auth: salva `~/.k8s-hpa-manager/gcp-adc.json` e define `GOOGLE_APPLICATION_CREDENTIALS`
- Rotas: `GET /api/v1/gcp/auth/status`, `POST /api/v1/gcp/auth/login`, `GET /api/v1/gcp/auth/poll?session_id=...`

**Nota**: `gcpNeedsAuth` no `AutoDiscoverDialog` só exibe aviso quando `has_gcloud=true` AND `authenticated=false` — evita falso positivo quando gcloud não está instalado.

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

### Auto-Shutdown por Inatividade

O servidor desliga automaticamente quando nenhuma página web está conectada por **40 minutos** (proteção contra instâncias esquecidas em WSL2).

**Mecanismo**:
- `startInactivityMonitor()` em `server.go` inicia timer de 45min na startup
- Frontend (`useHeartbeat.ts`) envia `POST /heartbeat` a cada **5 minutos**
- Cada heartbeat reseta o timer para **45 minutos**
- Se 40 minutos passarem sem heartbeat → `os.Exit(0)`

**Causa mais comum de desconexão**: browsers throttleiam `setInterval` em abas em segundo plano — o intervalo de 5min pode escalar para 60min+. O threshold de 40min dá margem para isso. Se ainda ocorrer, reabrir a aba (o frontend envia heartbeat imediatamente ao montar).

### Monitoring V2

`internal/monitoring/engine/monitoring_v2.go` — sem port-forwards. Discovery automático via HTTPS: `https://prometheus-{cluster}-{env}.viavarejo.com.br/`. Cache em memória (TTL 1h). Endpoints em `/api/v1/monitoring/v2/`.

### Monaco Editor — Regras Críticas

**`configureMonacoYaml` é global e deve ser chamado UMA única vez por sessão.** Chamar múltiplas vezes (ex: um por instância de `MonacoYamlEditor`) recria o worker YAML global e pode invalidar `addAction`/`addCommand` registrados em instâncias anteriores — os atalhos Ctrl+Shift+D (decode base64) e Ctrl+Shift+E (encode base64) desaparecem do menu de contexto. Implementado via flag `_yamlConfigured` em `MonacoYamlEditor.tsx`. **Nunca remover esse guard.**

**Atalhos registrados** (em todos os editores não-readOnly via `addAction`):
- `Ctrl+Shift+E` → Encode seleção para Base64
- `Ctrl+Shift+D` → Decode seleção de Base64
- `Ctrl+Shift+Z` (context menu) → Cron → Texto legível
- `Ctrl+Shift+X` (context menu) → Texto → Expressão Cron

**Erros em aba anônima**: "Tracking Prevention blocked access to storage" e "Could not create web worker(s)" são **inofensivos** — Monaco usa fallback síncrono. YAML funciona normalmente.

### CronJobs — Criação de Jobs e CronJobs (branch `criar-jobs-cronjobs`, PR #155)

`CronJobsTab.tsx` + `internal/web/handlers/cronjobs.go` + `internal/kubernetes/client.go`:

**Criação via modal unificado** (`Novo` dropdown → "Job (execução única)" ou "CronJob (agendado)"):
- Jobs criados via `BatchV1().Jobs(ns).Create()` direto — sem `kubectl apply`, sem conflito com Helm field manager
- CronJobs criados via `BatchV1().CronJobs(ns).Create()` — exigem `name` explícito; YAML gerenciado pelo Helm pode ser removido no próximo `helm upgrade`
- Namespace sempre herdado do seletor — nunca do YAML. `generateName: "job-"` automático se YAML não tem `name`
- Dry-run disponível antes de criar

**Template de CronJob selecionado**: botão "Template de CronJob selecionado" no modal de Job carrega o `spec.jobTemplate.spec` do CronJob via `GET /api/v1/cronjobs/:cluster/:namespace/:name/job-template`

**Monaco context menu** (menu de contexto ao selecionar texto no editor YAML):
- Cron → texto: selecionar expressão cron → "Converter cron para texto" → substitui pela descrição em pt-BR
- Texto → cron: selecionar texto natural (ex: "todos os dias às 08:00") → "Converter texto para cron" → substitui pela expressão
- Implementado em `MonacoYamlEditor.tsx` usando `editor.addAction()` + `explainCronExpression()`/`textToCron()` de `cronParser.ts`

**Versionamento no GitHub**: seção colapsável no modal para commitar o YAML. O usuário cola a URL completa da pasta no GitHub (ex: `https://github.com/org/repo/tree/main/jobs/pasta`) — o frontend extrai `owner/repo/branch/path` via regex. Backend `CommitFile` (`POST /api/v1/github/commit-file`) faz GET para obter SHA atual antes de criar/atualizar — evita duplicar arquivos. Suporta qualquer organização.

**Rotas**: `POST /api/v1/jobs`, `POST /api/v1/cronjobs/new`, `GET /api/v1/cronjobs/:cluster/:namespace/:name/job-template`, `POST /api/v1/github/commit-file`

**Nota investigação**: "listar e selecionar CronJob parece disparar um job" — investigado e confirmado que **não há bug**. O `active_jobs > 0` visível é do agendamento natural do K8s, não da UI. O trigger real exige 3 ações explícitas.

### ResizeDivider (SplitView)

`SplitView.tsx` é o componente reutilizável para painéis side-by-side com resize. Usado em `CommandRunnerTab` e `ResourceCompareModal`. Implementação via `useRef` + mouse event listeners. Importar `SplitView` ao criar novas interfaces de edição lado a lado — **não reimplementar o drag logic**.

### Command Runner

`CommandRunnerTab.tsx` + `internal/web/handlers/command_runner.go`: executa comandos (kubectl/shell/python/go) em múltiplos clusters simultaneamente com SSE. Suporta **AI-powered command generation** via `POST /api/v1/command-runner/generate` (gera comandos a partir de prompt em linguagem natural).

### ToolsMenu

`ToolsMenu.tsx` — dropdown com 14 ferramentas avançadas acessíveis no header. Ao adicionar nova ferramenta, registrar aqui como novo item do dropdown.

### Editor de Código (Code Editor)

`CodeEditorTab.tsx` + `internal/web/handlers/code_editor.go` + `code_editor_terminal.go` + `code_editor_lsp.go`: editor de código completo com integração Git/GitHub e LSP, acessível via Tools → "Editor de Código" (tela cheia). Fases 1-7 concluídas — ver `CODE-EDITOR-PLAN.md` para detalhes.

**Repositórios**: clonados em `~/.k8s-hpa-manager/repos/<owner>-<repo>/`. ID local = `owner-repo`. Limite: 10 repos por instância.

**Operações Git via SSE** (progresso em tempo real):
- Clone: `POST /api/v1/code-editor/clone` — injeta token na URL (`https://TOKEN@github.com/...`); token removido da URL remota após push
- Pull: `POST /api/v1/code-editor/repos/:id/pull`
- Push: `POST /api/v1/code-editor/repos/:id/push`

**Operações síncronas — arquivo/árvore**:
- Árvore: `GET /api/v1/code-editor/repos/:id/tree` — profundidade máx 6, ignora `.git`, `node_modules`, `vendor`, `build`
- Arquivo: `GET /api/v1/code-editor/repos/:id/file?path=...` — limite 5MB; `POST` para salvar
- Original (HEAD): `GET /api/v1/code-editor/repos/:id/original?path=...` — conteúdo HEAD para DiffModal
- Criar arquivo: `POST /api/v1/code-editor/repos/:id/file/create`
- Criar pasta: `POST /api/v1/code-editor/repos/:id/mkdir`
- Renomear: `POST /api/v1/code-editor/repos/:id/rename`
- Excluir: `DELETE /api/v1/code-editor/repos/:id/file`
- Busca por nome: `GET /api/v1/code-editor/repos/:id/search?q=...`
- Busca em conteúdo: `GET /api/v1/code-editor/repos/:id/grep?q=` (via `git grep -n --ignore-case`)
- Formatar: `POST /api/v1/code-editor/repos/:id/fmt` — executa formatter da linguagem (`gofmt`, `prettier`, etc.)

**Operações síncronas — git**:
- Status: `GET /api/v1/code-editor/repos/:id/status` — porcelain + ahead/behind
- Branches: `GET /api/v1/code-editor/repos/:id/branches` — faz `fetch --prune` antes
- Commit: `POST /api/v1/code-editor/repos/:id/commit` — `git add .` + `git commit -m`; suporta `--amend`; retorna `{ message }` com output real do git
- Branch: `POST /api/v1/code-editor/repos/:id/branch` (criar), `POST .../checkout` (trocar); ambos retornam `{ branch, message }`
- Merge: `POST /api/v1/code-editor/repos/:id/merge` — suporta `no_ff`
- Stash: `POST /api/v1/code-editor/repos/:id/stash` (`--include-untracked`), `POST .../stash/pop`
- Reset de arquivo: `POST /api/v1/code-editor/repos/:id/reset-file` — `git checkout HEAD` ou `git clean`
- Cherry-pick: `POST /api/v1/code-editor/repos/:id/cherry-pick`
- Tags: `GET /api/v1/code-editor/repos/:id/tags`, `POST` (criar anotada ou leve), `DELETE .../tags/:tag`
- Log: `GET /api/v1/code-editor/repos/:id/log?limit=20`
- Diff: `GET /api/v1/code-editor/repos/:id/diff?path=...`

**Terminal integrado**: WebSocket `GET /api/v1/code-editor/repos/:id/terminal` abre PTY real via `creack/pty`; xterm.js no frontend. Suporta cores ANSI, resize, programas interativos. Painel de 240px na base da área do editor; barra de abas para múltiplos terminais simultâneos (estado em `terminalTabs[]` + `activeTerminalId`).

**Barra de status** (linha azul `#007acc`, altura 20px): `Ln X, Col Y` | linguagem | `UTF-8` | font size `−/NNpx/+` (10–24, `localStorage["ce_font_size"]`) | word wrap toggle (`localStorage["ce_word_wrap"]`) | auto-save toggle (debounce 1,5s, `localStorage["ce_autosave"]`) | format on save toggle (Go/TS/JS/Python/JSON, formata antes de gravar, `localStorage["ce_format_on_save"]`). Font size e word wrap sincronizados via `editorRef.current?.updateOptions()` sem recriar o editor.

**Barra do arquivo ativo**: breadcrumb clicável (cada segmento de dir faz switch para aba Arquivos + `setRevealPath`); botão `Copy` copia path; botão `Locate` revela na tree. Arquivo revelado: `data-reveal-path` + ring amarelo + `scrollIntoView` por 1,5s.

**Context menu da tree** (botão direito): estado `{ x, y, node }` posicionado com `position: fixed`; fecha via `document.addEventListener("mousedown")`; `onMouseDown={e.stopPropagation()}` impede fechamento ao clicar dentro. Arquivo: Abrir / Renomear / Deletar / Copiar caminho / Revelar na tree / Histórico. Pasta: Novo arquivo aqui / Nova pasta aqui / Renomear / Deletar / Copiar caminho.

**Botão PR**: header, visível quando `branches?.current !== "main" && !== "master"`; abre `CreatePRModal` para criação de PR direto na aplicação. `owner`/`repo` extraídos via `ownerRepo(dir)` em `ListRepos` (lê `git remote get-url origin`) — **não** do ID local por split em `-`, que quebrava com owners com hífen (ex: `casas-bahia`). `CreatePRModal`: título auto-preenchido a partir do branch (ex: `feat/foo` → `Feat Foo`), dropdown de branch destino (exclui o branch atual), descrição opcional; chama `POST /api/v1/code-editor/repos/:id/pr/create` → GitHub REST API com o PAT do `tokenStore`; exibe `PR #N criado!` + botão "Abrir no GitHub" ao concluir.

**Ctrl+P Quick Open**: overlay `absolute inset-0 z-50` (requer `relative` no container pai); `quickOpenFiles = useMemo(() => flattenTree(tree).filter(...))` filtra em tempo real; registrado no Monaco via `addCommand(2048|46)` e via `document.addEventListener("keydown")` global.

**GitHub PAT**: via `GitHubTokenStore` (mesmo store do GitHub Releases). Fallback para `GITHUB_TOKEN` env var. Token injetado via `InjectUserEmail` middleware.

**Monaco no CodeEditorTab**: usa `@monaco-editor/react` direto (sem `MonacoYamlEditor`), detecta linguagem pela extensão do arquivo. **Não chama `configureMonacoYaml`** — evita conflito com o singleton em `MonacoYamlEditor.tsx`. Sidebar arrastável via `ResizeDivider` (mín 160px, máx 520px); largura e último repo persistidos em `localStorage`.

**LSP (Language Server Protocol)**: `code_editor_lsp.go` gerencia processos `gopls`/`pyright` por repositório via `sync.Map` (key: `repoId/lang`). Sessões inativas > 10min são encerradas; cleanup a cada 5min. JSON-RPC via stdin/stdout com header `Content-Length`. Frontend registra providers nativos do Monaco **uma única vez** por sessão (flags `__monacoTSConfigured`, `__monacoGoLSPRegistered`, `__monacoPyLSPRegistered`) usando variáveis globais `window.__lspActiveRepoId` e `window.__lspActiveFilePath` para comunicar o arquivo ativo. Polling de diagnósticos a cada 2,5s via `setModelMarkers`. `gopls` esperado em `~/go/bin/gopls` ou PATH; `pyright`/`pylsp` no PATH (instalar com `npm i -g pyright` ou `pipx install pyright`); `lspVersionRef` incrementado a cada `updateTabContent` e troca de aba. **Nunca registrar `registerCompletionItemProvider("go"|"python", ...)` mais de uma vez** — flag global previne duplicação. `__lspApplyDiagnostics(model, diags, owner)` aceita owner genérico (`"gopls"` ou `"pyright"`) para não sobrescrever markers entre linguagens.

**Por que não usar `monaco-languageclient`**: a v8+ exige `@codingame/monaco-vscode-editor-api` como peer dependency — uma fork do Monaco que substituiria o `monaco-editor` padrão, quebrando `monaco-yaml`, os workers de YAML e toda a configuração atual. A solução adotada usa providers nativos do Monaco (`registerCompletionItemProvider`, `registerHoverProvider`, `registerDefinitionProvider`, `setModelMarkers`) com chamadas HTTP ao backend Go que faz proxy JSON-RPC para o processo do language server.

**Path traversal**: `ReadFile`/`WriteFile` verificam `strings.HasPrefix(fullPath, repoDir)` antes de operar.

### AI Providers (Multi-provider)

`internal/ai/` suporta 5 providers: **Ollama**, **Claude** (Anthropic), **Gemini** (API Key ou Vertex AI via ADC/SSO), **OpenAI** e **Copilot** (Azure OpenAI). Configurável via `AISettingsTab.tsx`. Tokens de usuário persistidos em `internal/storage/user_tokens_store.go`. **Nunca hardcodear API keys** — sempre via storage de tokens.

**Gemini Vertex AI (SSO corporativo)**: `GeminiAuthMode = "vertex"` usa Application Default Credentials (`gcloud auth application-default login`). Requer `GeminiVertexProject` (ou env `GOOGLE_CLOUD_PROJECT`). O ADC do servidor tem prioridade sobre credenciais locais — não requer role IAM explícita se o servidor já tiver acesso.

**ADC file path**: `WriteADCFile()` em `internal/ai/google_device_auth.go` grava em `~/.k8s-hpa-manager/google_adc.json` (caminho próprio da app) — **nunca sobrescreve** `~/.config/gcloud/application_default_credentials.json`. Após gravar, define `GOOGLE_APPLICATION_CREDENTIALS` para apontar para esse arquivo.

**FallbackProvider**: quando o servidor não tem provider padrão configurado, tenta usar ADC da máquina host via `GOOGLE_APPLICATION_CREDENTIALS` ou `~/.config/gcloud/application_default_credentials.json`. Útil em ambientes de dev onde o ADC pessoal já está ativo.

**Vertex AI SSO — 3 tentativas com diagnóstico**: a lógica de inicialização do Gemini Vertex tenta até 3 vezes com logs de diagnóstico detalhados (endpoint, projeto, scopes) antes de falhar. Ver `internal/ai/gemini_provider.go`.

**Autenticação Vertex AI via Device Auth Grant (RFC 8628)**: Fluxo sem servidor de callback — obrigatório em WSL2 (loopback Linux isolado do Windows). Frontend chama `POST /ai/tokens/google-auth/start`, backend obtém `user_code` e `device_code` do Google, frontend exibe o código e `accounts.google.com/device`. Backend faz polling em `POST /ai/tokens/google-auth/poll` até receber o token. Implementado em `internal/ai/google_device_auth.go` + `internal/web/handlers/ai_tokens.go (StartGoogleDeviceAuth/PollGoogleDeviceAuth)`.

**Vertex AI via WIF SSO (Workforce Identity Federation)**: Campo `GeminiWifLoginURL` em `UserTokensStore` armazena o `poolID/providerID` (ex: `entraid-agentspace/entraid-federation-agentspace`). Backend em `google_auth_install.go:StartGoogleInstallAuth` usa `ai.ParseWIFPoolProvider()` para separar por `/` e chama `ai.StartWIFAppCallback(redirectURI, sessionID, poolID, providerID)` — endpoint `auth.cloud.google/authorize`. Callback retorna para `GET /oauth/google/callback` (porta 8080, forwarded no WSL2). Polling de status em `GET /ai/tokens/google-auth/install/status?session_id=...`. **UI (AISettingsTab.tsx)**: seção Vertex AI tem 3 passos — (1) Projeto GCP, (2) Autenticação com WIF Pool/Provider + botão OAuth + estado "aguardando" com link `<a>` clicável em vez de `window.open`, (3) Service Account JSON (alternativa). Tipo de retorno de `getAITokens()` inclui `gemini_wif_login_url?: string` — necessário para popular o campo ao carregar.

**Modelos Gemini Vertex AI**: `gemini-3.5-flash`, `gemini-3.1-pro-001`, `gemini-2.5-pro-preview-05-06` (Agentspace). Modo Vertex AI não aceita modelos do AI Studio — IDs diferentes. Ver `internal/ai/gemini_provider.go` para lista de modelos por modo.

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

### Diagnóstico SNAT (Node Pools)

`internal/web/handlers/nodepools_snat.go` + `SNATPortWidget.tsx` — diagnóstico de portas SNAT multi-cloud (AKS/GKE/EKS). Renderizado em `Index.tsx` (`renderTabContent` case `"nodepools"`) acima do `SplitView`, visível quando um cluster está selecionado.

**Atenção**: `NodePoolTab.tsx` é um componente órfão — **nunca é importado** pela aplicação. Toda adição de feature na aba Node Pools deve ir em `Index.tsx` (case `"nodepools"`) ou `TabContent.tsx` (sistema multi-tab), não em `NodePoolTab.tsx`.

**Endpoints**:
- `GET /api/v1/nodepools/snat?cluster=<cluster>` — perfil atual; salva snapshot no histórico SQLite de forma assíncrona (exceto EKS, onde `AllocatedOutboundPorts=0`)
- `GET /api/v1/nodepools/snat/projection?cluster=<cluster>` — histórico 30 dias + regressão linear para projeção de crescimento
- `GET /api/v1/nodepools/snat/nodes?cluster=<cluster>` — breakdown por nó via Prometheus (conntrack como proxy)
- `GET /api/v1/nodepools/snat/costs?cluster=<cluster>` — preços de referência via API nativa de cada cloud provider (ver abaixo)

**Detecção de provider** (`detectSNATProvider(clusterCtx)`): prefixo `gke_` → GKE, `arn:aws:eks:` → EKS, demais → AKS.

**Constantes de portas**:
- AKS: `snatPortsPerIPAzure = 64000` — portas SNAT por IP público no LB
- GKE: `snatPortsPerIPGCP = 64512` — faixa 0-64511 do Cloud NAT
- EKS: `snatPortsPerIPAWS = 55000` — conexões simultâneas por EIP/destino (modelo diferente)

**Builder por provider**:
- `buildSNATProfileAKS` — `az aks show` para `allocatedOutboundPorts` + `managedOutboundIPs.count` (timeout 30s)
- `buildSNATProfileGKE` — verifica auth GCP (`GCPAuthManager.CheckStatus`) antes de chamar `gcloud compute routers list --regions <region> --format json(name,nats)`. Zona → região: strip último segmento quando tem 1 char (ex: `us-central1-a` → `us-central1`). Default `minPortsPerVm=64` quando não configurado. `AUTO_ONLY` NAT IPs: conta 1 por NAT (subestimado — aviso no campo `Error`). Retorna `RequiresGCPAuth=true` quando gcloud presente mas não autenticado
- `buildSNATProfileEKS` — `aws ec2 describe-nat-gateways` conta EIPs; `AllocatedOutboundPorts=0` sinaliza modelo diferente (não por nó); `Error` descreve o modelo AWS
- `buildSNATProfileFromValues` — fórmula compartilhada AKS + GKE: `totalAvailable = ipCount × portsPerIP`, `totalRequired = allocatedPorts × nodes`. Status: `ok` (<80%), `warning` (80-95%), `critical` (≥95%)

**`SNATProfile`** contém:
- `CloudProvider string` — `"aks"`, `"gke"`, `"eks"`
- `AllocatedOutboundPorts` — 0 para EKS (modelo N/A)
- `MaxNodesAllowed` — máximo de nós suportados antes de falha SNAT (0 para EKS)
- `NodesUntilLimit` — quantos nós ainda cabem
- `IPsNeededForCurrentNodes` — IPs adicionais necessários para cobrir a carga atual
- `RequiresGCPAuth bool` — true quando gcloud não autenticado (GKE); frontend exibe tela de login

**Detecção de node pool por cloud** (`nodePoolLabel`): tenta label `kubernetes.azure.com/agentpool` (AKS), `eks.amazonaws.com/nodegroup` (EKS), `cloud.google.com/gke-nodepool` (GKE) — na ordem, retorna o primeiro não-vazio.

**Histórico SQLite** (`internal/storage/snat_history_store.go`): store `snat_history.db` em WAL mode. Deduplica: no máximo 1 snapshot/hora/cluster. Retenção 90 dias. Não salva quando `AllocatedOutboundPorts == 0` (EKS). Métodos: `Save`, `GetRecent(cluster, days)`, `GetLatest(cluster)`, `ComputeSNATProjection(records, nodesUntilLimit)`.

**Projeção de crescimento** (`ComputeSNATProjection`): regressão linear sobre `total_node_count` ao longo do tempo. Confiança: `high` (≥14 pontos + ≥7 dias de span), `medium` (≥5 pontos + ≥2 dias), `low`, `none`. Retorna `GrowthPerDay`, `DaysUntilLimit` (-1 = indeterminado), `EstimatedDate`.

**Frontend**: header compacto sempre visível. Quando `requires_gcp_auth`, exibe badge âmbar "Login GCP necessário". Clique abre `Dialog` (`max-w-2xl`, `max-h-[78vh]`).

**Auth GCP no widget** (`SNATPortWidget.tsx`): quando `data.requires_gcp_auth && data.cloud_provider === "gke"` (`gcpNeedsAuth`), o modal exibe tela de autenticação inline com Device Auth Grant — mesmo fluxo de `AutoDiscoverDialog` (`checkGCPAuth` / `startGCPLogin` / `pollGCPLogin` via `/api/v1/gcp/auth/status|login|poll`). Após login bem-sucedido: `refetch()` recarrega o perfil SNAT. Tabs e conteúdo ficam ocultos enquanto auth é necessária.

**Pricing nativo por cloud** (`internal/web/handlers/nodepools_snat_costs.go`): endpoint `GetSNATCosts` busca preços reais da API de cada provider; fallback documental quando a API falha.

| Provider | API usada | O que busca |
|---|---|---|
| AKS | Azure Retail Prices API (`prices.azure.com`) | IP público Standard em BRL, `armRegionName=brazilsouth`, `productName=IP Addresses` |
| GKE | GCP Cloud Billing Catalog API (`cloudbilling.googleapis.com`) via ADC token (`GetFreshGKEToken`) | SKUs por substring: `"cloud nat gateway"`, `"cloud nat data"`, `"external ip in use"` nos service IDs `95FF-2EF5-5EA1` (Networking) e `6F81-5844-456A` (Compute Engine) |
| EKS | AWS Pricing API via `aws pricing get-products --service-code AmazonVPC --region us-east-1` | Grupos `NGW:NatGateway` (por hora) e `NGW:Data` (por GB); extrai preço via `terms.OnDemand.*.priceDimensions.*.pricePerUnit.USD` |

**`SNATCostInfo`** (resposta de `/snat/costs`): `ip_price_monthly` (AKS/GKE: custo por IP/mês), `gw_hourly_price` (GKE/EKS: custo por hora do NAT GW), `data_price_per_gb` (GKE/EKS: custo por GB processado), `currency` (`"BRL"` para AKS, `"USD"` para GKE/EKS), `pricing_region`, `source` (`"azure-retail-api"` / `"gcp-billing-api"` / `"aws-pricing-api"` / `"reference"`).

**`FALLBACK_COSTS`** (frontend): map de fallbacks documentais por provider, usados quando o endpoint `/snat/costs` falha — AKS: R$20/IP/mês, GKE: $0.004/h IP + $0.044/h NAT GW + $0.045/GB, EKS: $0.044/h NAT GW + $0.044/GB.

**5 abas manuais** (div + estado — nunca shadcn `<Tabs>`):
- **Diagnóstico** — barra de uso (AKS/GKE) ou info NAT GW (EKS), 6 cards métricas, seção Capacidade, breakdown por node pool
- **Financeiro** — usa `SNATCostInfo` do endpoint `/snat/costs` (lazy, cache 1h) com `fmtCost()` que formata BRL ou USD conforme `currency`; badge indica a fonte dos preços; AKS: custo IP/mês em BRL real; GKE: NAT GW/hora + IP externo/mês + GB em USD real; EKS: NAT GW/hora + GB em USD real com estimativa mensal. Fallback para `FALLBACK_COSTS` quando API indisponível
- **Fórmula** — AKS/GKE: equações passo a passo; EKS: modelo 55k conexões simultâneas por EIP
- **Projeção** — taxa de crescimento (nós/semana), badge de confiança, `LineChart` com `ReferenceLine` em `max_nodes_allowed`
- **Nós** — tabela com mini progress bars; usa `conntrack_usage_pct` como proxy quando `allocated_ports === 0` (EKS); histórico conntrack 6h via `AreaChart`

**Integração com análise preditiva de node pools**: `NodePoolPredictionsHandler` lê o snapshot mais recente do SQLite antes de chamar `analyzer.Analyze()` e popula `req.SNATContext` (`SNATContextData`). O prompt gerado inclui seção `# SNAT DO LOAD BALANCER AKS` com status, capacidade de nós, projeção de crescimento e comandos corretivos `az aks update`. Categorias `"snat"` adicionadas ao schema JSON de `root_cause` e `recommendations`.

Cache de 2min via React Query (`staleTime: 2 * 60 * 1000`).

**Por que não usar Conntrack aqui**: SNAT é uma limitação do Load Balancer/NAT Gateway (fora do node), não do kernel conntrack. São complementares — conntrack serve como proxy de estimativa por nó, mas o orçamento SNAT real vem da API de cada cloud.

### HPAEditor (HPA Tab)

`HPAEditor.tsx` — editor standalone de HPA extraído do inline editor do `HPAListItem`. Substitui o painel de edição que ficava acoplado ao item da lista.

**Funcionalidades**:
- Edição de `minReplicas`, `maxReplicas`, `targetCPU`, `targetMemory`
- Edição de recursos (`cpuRequest`, `cpuLimit`, `memoryRequest`, `memoryLimit`)
- Checkbox "Incluir no Staging sem alterar valores" — adiciona o HPA ao staging context sem modificar nenhum campo (útil para incluir HPAs na sessão de staging como referência ou para rollout)
- Checkbox para rollout de Deployment / DaemonSet / StatefulSet após aplicar
- Usa `useK8sPermissions` para habilitar/desabilitar botões conforme RBAC do cluster
- `ProtectedAction allowed={permissions.canUpdateHPA}` — usa RBAC K8s, não grupo AD

**Integração**: `HPATab.tsx` renderiza `HPAEditor` em painel lateral quando um HPA é selecionado na lista.

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

### JSON Inspector (Logs)

Ferramenta de inspeção e formatação de JSON embutida em todos os visualizadores de log. Ativada por **seleção de texto** — o usuário seleciona um trecho do log e clica no botão flutuante que aparece.

**Arquivos:**
- `src/lib/jsonFormatter.ts` — `tryFormatJson(input)`, `tokenizeJson(line)` e `extractJsonBlock(text)`
- `src/hooks/useJsonInspector.ts` — hook de detecção de seleção via `selectionchange` event + posicionamento do botão flutuante via `getRangeAt(0).getBoundingClientRect()`
- `src/components/JsonInspectorModal.tsx` — modal split-view com entrada editável (esquerda) e saída formatada + syntax highlight + numeração de linhas (direita). Também exporta `JsonFloatingButton`

**Componentes que usam o inspetor** (padrão idêntico nos 3 primeiros):
- `PodLogsPanel.tsx` — `onMouseUp={jsonInspector.handleMouseUp}` no container de log
- `PodQuickViewModal.tsx` — idem na área de scroll de logs
- `ContainersTab.tsx` — wrapper `<div onMouseUp={...}>` em volta do `<ScrollArea>` de logs
- `LogViewer.tsx` — abordagem diferente: `<Textarea ref={textareaRef} onSelect={...}>` + botão no toolbar (sem botão flutuante, pois `window.getSelection()` não funciona em textarea)

**Comportamento crítico de `tryFormatJson`:**
1. Tenta `JSON.parse()` no texto completo
2. Se falhar, chama `extractJsonBlock()` que percorre o texto procurando o primeiro `{` ou `[` e extrai o bloco balanceado — necessário para logs com prefixo (`2024-01-01T12:00:00Z INFO {"msg":"..."}` → extrai `{"msg":"..."}`)
3. Se a extração tiver sucesso, retorna `wasExtracted: true` → modal exibe aviso âmbar
4. Se tudo falhar, retorna `errorLine`/`errorCol` extraídos da mensagem V8 (`"(line N column M)"`) → linha com erro é destacada em vermelho no painel direito

**Renderização linha-a-linha** (design crítico): `ValidJsonPanel` e `InvalidJsonPanel` iteram `json.split("\n")` e tokenizam **cada linha individualmente** via `tokenizeJson(line)`. Isso garante que o número da linha N sempre corresponda ao conteúdo da linha N. **Não tokenizar o JSON completo** num único passo — o alinhamento com os números de linha quebra quando tokens `space` contêm `\n`.

**Formato de log correto para FluentD + EventHub**: JSON puro por linha com timestamp embutido:
```json
{"time":"2024-06-08T12:00:00Z","level":"INFO","msg":"pod started","pod":"api-7d9f"}
```
O formato `TIMESTAMP LEVEL {JSON}` (timestamp fora do objeto) falha no FluentD `@type json` e no EventHub consumer. O inspetor detecta esse caso via `extractJsonBlock` e avisa com o badge âmbar.

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
- **`OptionalSRECheck` sempre retorna `isSRE=true`** — verificação de grupo AD desabilitada em `rbac.go` (linha 145-148). Todos os usuários autenticados têm acesso SRE. Não remover esse comportamento sem alinhamento explícito.

---

## RBAC K8s via SelfSubjectRulesReview

Camada adicional ao RBAC Azure AD (branch `correcao-jwt`): permissões reais do cluster por namespace, obtidas via `SelfSubjectRulesReview` da API do K8s. Independente de grupos AD — reflete exatamente o que o kubeconfig do servidor permite.

**Backend** (`internal/kubernetes/permissions.go` + `internal/web/handlers/k8s_permissions.go`):
- `NamespacePermissions` — struct com campos `canListHPA`, `canUpdateHPA`, `canExecPods`, `canWriteSecrets`, etc.
- `K8sPermissionsHandler` — cache em memória com TTL de **5 minutos** por chave `cluster/namespace`
- Endpoint: `GET /api/v1/k8s-permissions?cluster=<c>&namespace=<ns>`
- Campo `Incomplete: true` quando o cluster usa wildcard policies complexas — nesse caso assume acesso total para não bloquear

**Frontend** (`internal/hooks/useK8sPermissions.ts`):
- `useK8sPermissions(cluster, namespace)` — React Query com `staleTime: 5min`, retry 1, sem refetch no foco
- Fallback conservador: leitura permitida, escrita bloqueada (enquanto carrega ou cluster indefinido)
- Retorna `{ permissions, canWrite }` onde `canWrite = permissions.canUpdateHPA`

**`ProtectedAction` atualizado** — nova prop `allowed?: boolean`:
```tsx
// Sem prop: verifica grupo AD (isSRE) — comportamento original
<ProtectedAction><Button>Escalar HPA</Button></ProtectedAction>

// Com prop: usa permissão K8s real — ignora grupo AD
const { permissions } = useK8sPermissions(cluster, namespace);
<ProtectedAction allowed={permissions.canUpdateHPA}>
  <Button>Escalar HPA</Button>
</ProtectedAction>
```

**Quando usar qual verificação:**
- `ProtectedAction` sem `allowed` → operações que requerem pertencer ao grupo SRE (ex: aprovar CHGs, operações de node pool)
- `ProtectedAction allowed={...}` → operações que refletem o RBAC do cluster (ex: editar HPA, secret, deployment — depende do que o kubeconfig permite)

---

## Troubleshooting Rápido

Os problemas mais críticos/surpreendentes:

| Problema | Solução |
|----------|---------|
| Frontend não atualiza após mudança | `./rebuild-web.sh -b` + Ctrl+Shift+R no browser |
| Mudanças no backend não tomam efeito | Servidor não reiniciado — `make build` só gera o binário; matar e reiniciar (`kill <PID> && ./build/new-k8s-hpa web -f`) |
| Servidor desliga sozinho após ~40min | Auto-shutdown por inatividade — reabrir a aba reinicia o heartbeat (browsers throttleiam `setInterval` em abas de fundo) |
| Build falha sem versão | `git fetch --tags --prune` |
| Cluster inacessível | VPN desconectada — `kubectl cluster-info --context <name>` |
| JWT: login retorna 501 | `K8S_HPA_JWT_SECRET` não definido — frontend cai para token estático automaticamente |
| JWT: login retorna "AZ_CLI_ERROR" | Azure CLI não autenticado — executar `az login` no servidor |
| JWT: frontend em loop de login | Limpar `localStorage` manualmente |
| Monaco: Ctrl+Shift+D/E sumiu do contexto | `configureMonacoYaml` chamado múltiplas vezes — verificar flag `_yamlConfigured` em `MonacoYamlEditor.tsx` |
| SNAT widget não aparece na aba Node Pools | Widget fica em `Index.tsx` case `"nodepools"` — `NodePoolTab.tsx` é componente **órfão** (nunca importado) |
| Arquivo em `pages/` não tem efeito | Vários arquivos em `src/pages/` são **mortos**: `Index.backup.tsx`, `Index.broken.tsx`, `Index.tsx.broken`, `SimpleIndex.tsx`, `MinimalIndex.tsx`, `TestIndex.tsx` — nunca importados. Editar apenas `Index.tsx` |
| Tab de modal não preenche a altura | shadcn `<Tabs>` usa `display:block` que quebra `flex-1 min-h-0` — usar implementação manual (ver `PodQuickViewModal.tsx`) |
| GKE: workloads não carregam | `GetFreshGKEToken()` sem credenciais — verificar `~/.k8s-hpa-manager/gcp-adc.json` ou autenticar via AutoDiscover |
| K8s RBAC: botão disabled mesmo sendo SRE | `useK8sPermissions` ainda carregando ou RBAC real do cluster prevalece — verificar se `allowed` prop está sendo passada |
| Teams: refresh retorna 409 Conflict | Extração já em andamento — aguardar ~90s ou reiniciar o servidor |
| Code Editor: push rejeitado com "non-fast-forward" | Pull --rebase automático implementado — se o rebase falhar (conflito), push retorna erro com mensagem de conflito |

> Troubleshooting completo (Code Editor, Dynatrace, ServiceNow, Teams, LSP, FinOps, SNAT, etc.) → [docs/guides/TROUBLESHOOTING.md](docs/guides/TROUBLESHOOTING.md)

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
make release   # gera: build/release/new-k8s-hpa-linux-amd64, darwin-amd64, darwin-arm64

# Criar release no GitHub (com upload de binários)
gh release create v1.3.X \
  build/release/new-k8s-hpa-linux-amd64 \
  build/release/new-k8s-hpa-darwin-amd64 \
  build/release/new-k8s-hpa-darwin-arm64 \
  --title "v1.3.X" \
  --notes "Descrição das mudanças"
```

> `create-v1-release.sh` era específico para v1.0.0 — **não usar** para releases correntes.
