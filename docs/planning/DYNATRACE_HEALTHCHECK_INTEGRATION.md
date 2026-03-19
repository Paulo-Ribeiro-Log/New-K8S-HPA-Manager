# Plano de Integração Dynatrace × Health Check

**Criado:** 17/03/2026
**Última atualização:** 19/03/2026
**Branch:** `integracao-dyna`

---

## Status das Fases

| Fase | Descrição | Status | Commit |
|------|-----------|--------|--------|
| 1 | Fix básico — cluster matching, paginação, tag filter | ✅ Concluída | `8666044` |
| 2 | Enriquecimento — Davis AI, métricas, eventos | ✅ Concluída | `4dbe402` |
| 3 | Frontend — DynatraceProblemCard expandido | ✅ Concluída | `ca95b17` |
| 4 | Avançado — dedup cross-cluster, cache, batch AI | ✅ Concluída | `bfab22c` |
| 5 | **DT Sinais — aba OneAgent sem problems ativos** | 🔲 Pendente | — |

---

## Fases 1–4 (Concluídas)

### Bugs corrigidos (Fase 1)

| Bug | Correção |
|-----|---------|
| Cluster name matching falha (`-admin` suffix) | `normalizeClusterName()` + `matchesCluster()` — TrimSuffix + lowercase |
| `pageSize` hardcoded em 10 | Loop `nextPageKey`, pageSize=50, limite 200 |
| Tag filter bloqueia quando vazio | Tag filter opcional — cluster match é o filtro primário |

### O que foi entregue (Fases 1–4)

- **DynatraceChecker**: busca problems OPEN paginados, filtra por cluster, enriquece com tags OneAgent
- **Busca reversa** (`SearchProblemsForWorkloads`): workloads K8s sem match DT → busca entidade DT por nome → traz problems ativos
- **Enriquecimento**: Davis AI evidence + métricas (error rate, P90) + eventos recentes (Top 5 problems)
- **Correlator** (`correlator.go`): cruza K8sIssues × DTProblems por `namespace/workload` → `CorrelatedHealthItem[]`
- **Escalada**: K8s >= High **E** DT >= High → `FinalSeverity = Critical`; `prd` + `ENVIRONMENT` impact → `StatusCritical`
- **Cache**: `entityCache sync.Map` TTL 5min para `EnrichEntitiesWithK8s`
- **Frontend**: aba "K8s↔DT" com `CorrelatedItemCard`, badges tricolores, botão "Analisar com AI" por item
- **Batch AI**: `POST /api/v1/healthcheck/correlated/analyze-batch` — diagnóstico consolidado de até 20 itens

---

## Fase 5 — DT Sinais (Nova Aba OneAgent)

### Motivação

O fluxo atual só traz workloads que já geraram um **problem** no Dynatrace. Mas as policies e regras de alerta são muito refinadas — muitos workloads com métricas degradadas nunca acionam um problem formal.

Como todos os clusters de produção têm o OneAgent instalado, é possível consultar **todas as entidades instrumentadas** e identificar riscos por threshold de métricas, independente de existir um problem ativo.

### Diferença do fluxo atual

| | Fluxo atual (Fases 1–4) | Fase 5 (DT Sinais) |
|---|---|---|
| **Ponto de entrada** | `GetOpenProblems` → filtrar por cluster | `ListEntitiesByCluster` → todas as entidades |
| **Trigger** | Problem DT existe | Threshold de métrica ultrapassado |
| **Cobertura** | Só o que o DT alarmou | Tudo que o OneAgent monitora |
| **Correlação node pool** | Não | Nível de cluster (todos os pools do cluster) |
| **Correlação dependências** | Não | Blast radius via `dependency_registry` |

---

## Contexto Técnico — Fase 5

### APIs DT necessárias (novas)

#### `ListEntitiesByCluster` (a criar em `internal/dynatrace/client.go`)

```go
// ListEntitiesByCluster lista entidades de um tipo instrumentadas pelo OneAgent em um cluster.
// Usa DTLabels.HostGroup (dt.host_group.id) como seletor principal.
// entityType: "CLOUD_APPLICATION" (workloads K8s) ou "SERVICE" (serviços instrumentados)
// Retorna até 500 entidades, sem paginação (limite prático por cluster).
func (c *Client) ListEntitiesByCluster(ctx context.Context, clusterName, entityType string) ([]EntityStub, error)
```

**entitySelector a usar:**
```
// Primário — via HostGroup tag (dt.host_group.id):
type("CLOUD_APPLICATION"),tag("dt.host_group.id:akspriv-logistica-prd")

// Fallback — via label k8s.cluster.name (se OneAgent K8s nativo):
type("CLOUD_APPLICATION"),kubernetesCluster.name("akspriv-logistica-prd")
```

**Nota crítica**: o `HostGroup` em `DTLabels` mapeia para a tag `dt.host_group.id` na API DT. O valor é o nome do cluster **sem** sufixo `-admin` (ex: `akspriv-logistica-prd`). Usar `normalizeClusterName()` já existente antes de montar o entitySelector.

#### `BatchQueryMetrics` (a criar em `internal/dynatrace/metrics.go`)

```go
// BatchQueryMetrics consulta métricas da última `windowMinutes` para N entidades em paralelo.
// Retorna mapa entityID → métricas. Entidades sem dados são omitidas.
// Limite de 10 goroutines simultâneas para não sobrecarregar a API.
func (c *Client) BatchQueryMetrics(ctx context.Context, entityIDs []string, entityType string, windowMinutes int) map[string]map[string]float64
```

Métricas por tipo já mapeadas em `metricsForEntityType()` — reutilizar diretamente.

---

### Novo modelo `OneAgentSignal`

A criar em `internal/healthcheck/models.go`:

```go
// OneAgentSignal representa um workload instrumentado pelo OneAgent com métricas elevadas,
// mesmo sem um problem DT ativo. É a visão "pré-alarme" do ambiente.
type OneAgentSignal struct {
    // Identificação
    WorkloadName string `json:"workload_name"`
    Namespace    string `json:"namespace"`
    Cluster      string `json:"cluster"`
    EntityID     string `json:"entity_id"`   // ID DT (ex: CLOUD_APPLICATION-XXXX)
    EntityType   string `json:"entity_type"` // CLOUD_APPLICATION | SERVICE

    // Métricas (janela: últimos 60min)
    ErrorRate      float64 `json:"error_rate,omitempty"`      // %
    ResponseP90Ms  float64 `json:"response_p90_ms,omitempty"` // ms
    PodRestarts    int     `json:"pod_restarts,omitempty"`    // contagem 1h
    CPUThrottlePct float64 `json:"cpu_throttle_pct,omitempty"` // %
    PodsReadyPct   float64 `json:"pods_ready_pct,omitempty"`  // %

    // Avaliação de risco (sem problem DT)
    RiskLevel   Severity `json:"risk_level"`            // derivado dos thresholds
    RiskReasons []string `json:"risk_reasons,omitempty"` // ex: "Error rate 8.5% > 5%"
    HasDTProblem bool    `json:"has_dt_problem"`         // true = já coberto pelas Fases 1-4

    // Correlações
    ClusterPools []NodePoolSummary `json:"cluster_pools,omitempty"` // pools do cluster (do registry)
    DependedBy   []string          `json:"depended_by,omitempty"`   // serviços que chamam este
    DependsOn    []string          `json:"depends_on,omitempty"`    // serviços que este chama

    // Metadata
    AppVersion string    `json:"app_version,omitempty"`
    Squad      string    `json:"squad,omitempty"`
    CheckedAt  time.Time `json:"checked_at"`
}

// NodePoolSummary resumo de um node pool para contextualização
type NodePoolSummary struct {
    NodePool  string `json:"nodepool"`
    VMSize    string `json:"vm_size"`
    Mode      string `json:"mode"`
    NodeCount int    `json:"node_count"`
}
```

Adicionar em `HealthCheckResult`:
```go
OneAgentSignals []OneAgentSignal `json:"oneagent_signals,omitempty"`
```

---

### Thresholds padrão

Configuráveis via body do request (`OneAgentThresholds`). Valores default:

| Métrica | Warning | Critical |
|---------|---------|----------|
| `error_rate` | > 5% | > 10% |
| `response_p90_ms` | > 2000ms | > 5000ms |
| `pod_restarts` (1h) | > 3 | > 10 |
| `cpu_throttle_pct` | > 20% | > 50% |
| `pods_ready_pct` | < 90% | < 70% |

---

### Correlação com Node Pool Registry

**Decisão de design:** correlação em nível de **cluster**, não por workload individual.

A chain `SERVICE → PROCESS_GROUP_INSTANCE → HOST → extractNodePoolFromName()` exige 3 saltos de API por entidade, inviável durante o HC.

**Abordagem adotada:** para cada sinal, usar `K8sCluster` já disponível na entidade para chamar `nodepool_registry_store.GetAll(cluster)` (uma query SQLite) → preencher `ClusterPools[]` com todos os pools do cluster.

**Código relevante já existente:**
- `internal/storage/nodepool_registry_store.go`: `GetAll(cluster)` → `[]NodePoolRegistryEntry`
- `internal/web/handlers/nodepool_registry.go`: `extractNodePoolFromName()` + regex `^aks-(.+?)-\d{5,8}-vmss[0-9a-f]+`
- `LookupByNodePool(poolName)` — disponível se no futuro quisermos a chain completa

---

### Correlação com Dependency Registry

Para cada workload at-risk:

```go
// Blast radius: quem depende deste workload
callers, _ := depRegistry.SearchByServiceName(workloadName)
// Filtrar: deps onde workloadName aparece como target/provider
// → preencher DependedBy[]

// Upstream: de quem este workload depende
deps, _ := depRegistry.GetAll(cluster, namespace, "")
// Filtrar: deps onde workloadName aparece como source
// → preencher DependsOn[]
```

**Código relevante já existente:**
- `internal/storage/dependency_registry.go`: `SearchByServiceName(query)`, `GetAll(cluster, ns, type)`
- Schema: `dependencies(source_service, target_service, service_type, cluster, namespace)`

---

### Fluxo de integração no Orchestrator

```
POST /api/v1/healthcheck/run { check_oneagent_signals: true, ... }
    │
    ├── [paralelo] K8s checkers (deployments, hpa, events, pvc)
    ├── [paralelo] DynatraceChecker.CheckAll() — problems ativos
    │
    ▼ (após wg.Wait())
    OneAgentSignalsChecker.CheckAll(
        ctx, dtURL, dtToken, cluster,
        existingDTProblemWorkloads,  // para marcar HasDTProblem=true
        nodePoolStore, depRegistry,
        thresholds,
    )
    │
    ▼
    result.OneAgentSignals = signals
    result.CorrelatedItems = Correlate(result)  // já existente
```

**Timeout budget sugerido:** 30s adicionais (`TimeoutOneAgent = 30s`)
- `ListEntitiesByCluster` (2 chamadas: CLOUD_APPLICATION + SERVICE): ~5s
- `BatchQueryMetrics` (N entidades, 10 goroutines): ~20s
- Correlações locais (SQLite): ~1s

---

### Novo Checker `OneAgentSignalsChecker`

A criar em `internal/healthcheck/oneagent_signals_checker.go`:

```go
type OneAgentSignalsChecker struct{}

func (c *OneAgentSignalsChecker) CheckAll(
    ctx context.Context,
    dtURL, dtToken string,
    cluster string,
    existingWorkloads map[string]struct{}, // workloads já cobertos por DT problems (namespace/name)
    nodePoolStore *storage.NodePoolRegistryStore,
    depRegistry  *storage.DependencyRegistry,
    thresholds   OneAgentThresholds,
    timeoutSec   int,
) []OneAgentSignal
```

Lógica interna:
1. `ListEntitiesByCluster(ctx, cluster, "CLOUD_APPLICATION")` + `ListEntitiesByCluster(ctx, cluster, "SERVICE")` em paralelo
2. Deduplicar por `EntityID`
3. `BatchQueryMetrics(ctx, entityIDs, windowMinutes=60)`
4. Para cada entidade com dados: aplicar thresholds → calcular `RiskLevel` + `RiskReasons`
5. Descartar entidades sem risco E sem dados de métrica (`RiskLevel == SeverityInfo && len(RiskReasons) == 0`)
6. Marcar `HasDTProblem = true` se o workload já está em `existingWorkloads`
7. Enriquecer com `nodePoolStore.GetAll(cluster)` → `ClusterPools[]`
8. Enriquecer com `depRegistry.SearchByServiceName()` → `DependedBy[]`, `DependsOn[]`

---

### Nova rota HTTP

```
GET /api/v1/healthcheck/oneagent-signals/:sessionId
```

Retorna `result.OneAgentSignals` do resultado já persistido. Não precisa de novo endpoint de run — os sinais são incluídos no resultado normal quando `check_oneagent_signals: true`.

---

### Nova aba frontend: "DT Sinais"

**Arquivo:** `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx`
**Posição:** 9ª aba (após "K8s↔DT")

#### Layout do card `OneAgentSignalCard`

```
┌─────────────────────────────────────────────────────────┐
│ ▶ checkout-api               [WARNING]  [DT Sinais]     │
│   payments / akspriv-prd                                │
├─────────────────────────────────────────────────────────┤
│ Métricas (1h):                                          │
│   ERR: 8.5%  P90: 1850ms  Restarts: 5  CPU Throttle: 35%│
│                                                         │
│ ⚠ Razões:                                               │
│   • Error rate 8.5% > threshold 5%                     │
│   • Pod restarts 5 > threshold 3                       │
│                                                         │
│ Node Pools do cluster:                                  │
│   systempool (Standard_D2s_v5, 3 nós) · System         │
│   userpool   (Standard_D4s_v5, 8 nós) · User           │
│                                                         │
│ Dependentes afetados (blast radius): order-service,     │
│ payment-gateway  (+2 outros)                            │
│                                                         │
│ [Já tem problem DT ↗] ou [Sem problem DT — só métricas] │
│                                           [Analisar AI] │
└─────────────────────────────────────────────────────────┘
```

#### Filtros na aba
- Toggle: "Apenas at-risk" (oculta `SeverityInfo`) / "Todos instrumentados"
- Toggle: "Ocultar com problem DT ativo" (HasDTProblem=true → já cobertos na aba K8s↔DT)

---

## Checklist de Implementação — Fase 5

### Fase 5A — Backend: novos métodos no client DT

- [ ] **`internal/dynatrace/client.go`**: `ListEntitiesByCluster(ctx, clusterName, entityType)` — entitySelector por `tag("dt.host_group.id:cluster")`, paginação até 500 entidades
- [ ] **`internal/dynatrace/client.go`**: fallback entitySelector por `kubernetesCluster.name(cluster)` se primário retornar vazio
- [ ] **`internal/dynatrace/metrics.go`**: `BatchQueryMetrics(ctx, entityIDs, entityType, windowMinutes)` — goroutines paralelas (semáforo de 10), reutiliza `metricsForEntityType()` existente
- [ ] **Testar:** `ListEntitiesByCluster` retorna entidades reais com o HostGroup tag configurado nos clusters de prod

### Fase 5B — Backend: modelo e checker

- [ ] **`internal/healthcheck/models.go`**: structs `OneAgentSignal`, `NodePoolSummary`, `OneAgentThresholds` (com defaults)
- [ ] **`internal/healthcheck/models.go`**: campo `OneAgentSignals []OneAgentSignal` em `HealthCheckResult`
- [ ] **`internal/healthcheck/oneagent_signals_checker.go`**: `OneAgentSignalsChecker.CheckAll()` completo (lista entidades → métricas → thresholds → correlações)
- [ ] **`internal/healthcheck/oneagent_signals_checker.go`**: enriquecimento com `nodePoolStore.GetAll(cluster)` → `NodePoolSummary[]`
- [ ] **`internal/healthcheck/oneagent_signals_checker.go`**: enriquecimento com `depRegistry.SearchByServiceName()` → `DependedBy[]` + `DependsOn[]`
- [ ] **`internal/healthcheck/orchestrator.go`**: chamar `OneAgentSignalsChecker.CheckAll()` após `wg.Wait()`, quando `req.CheckOneAgentSignals == true`
- [ ] **`internal/healthcheck/orchestrator.go`**: passar `existingWorkloads` (mapa dos workloads já cobertos por DT problems) para o checker

### Fase 5C — Backend: handler e rota

- [ ] **`internal/web/handlers/healthcheck.go`**: injetar `nodePoolStore` e `depRegistry` no `HealthCheckHandler` (via construtor)
- [ ] **`internal/web/handlers/healthcheck.go`**: incluir `OneAgentSignals` na resposta do `Get` e `Run`
- [ ] **`internal/web/server.go`**: passar stores ao construtor do `HealthCheckHandler`
- [ ] **`internal/web/handlers/healthcheck.go`**: `AnalyzeOneAgentSignal` — análise AI individual com contexto (métricas + pools + blast radius)
- [ ] **`internal/web/server.go`**: rota `POST /api/v1/healthcheck/oneagent/analyze` (SRE only)

### Fase 5D — Frontend: tipos e API client

- [ ] **`internal/web/frontend/src/types/healthcheck.ts`**: interfaces `OneAgentSignal`, `NodePoolSummary`, `OneAgentThresholds`
- [ ] **`internal/web/frontend/src/types/healthcheck.ts`**: campo `oneagent_signals?: OneAgentSignal[]` em `HealthCheckResult`
- [ ] **`internal/web/frontend/src/lib/api/client.ts`**: `analyzeOneAgentSignal(signal, aiEmail)` → `POST /healthcheck/oneagent/analyze`

### Fase 5E — Frontend: aba "DT Sinais"

- [ ] **`HealthCheckResultsPanel.tsx`**: componente `OneAgentSignalCard` — métricas como badges, risk reasons, node pools, blast radius, badge HasDTProblem
- [ ] **`HealthCheckResultsPanel.tsx`**: aba "DT Sinais" (9ª aba, após K8s↔DT) com contadores: N at-risk, M sem problem DT
- [ ] **`HealthCheckResultsPanel.tsx`**: filtro "Apenas at-risk" (default on) e toggle "Ocultar com problem DT ativo"
- [ ] **`HealthCheckResultsPanel.tsx`**: botão "Analisar com AI" no `OneAgentSignalCard`
- [ ] **`HealthCheckResultsPanel.tsx`** (opcional): botão "Analisar tudo com AI" (batch) reutilizando padrão do `CorrelatedTab`

### Fase 5F — Configuração HC request

- [ ] **Frontend HC options**: novo toggle "Sinais OneAgent" nas opções do health check (ao lado de "Problems Dynatrace")
- [ ] **Frontend HC options**: seção expansível "Thresholds" com sliders/inputs para error rate, P90, restarts, throttle
- [ ] **Backend**: campo `CheckOneAgentSignals bool` + `OneAgentThresholds` no `HealthCheckRequest`

---

## Checklist — Fases Anteriores (histórico)

### ✅ Fase 1 — Fix Básico (commit `8666044` — 17/03/2026)
- [x] `dynatrace_checker.go`: `normalizeClusterName()` + `matchesCluster()`
- [x] `client.go`: paginação `GetOpenProblems` (pageSize=50, loop nextPageKey, limite 200)
- [x] `dynatrace_checker.go`: tagFilter opcional

### ✅ Fase 2 — Enriquecimento (17/03/2026)
- [x] `models.go`: campos `Evidence`, `RecentEvents`, `MetricsSummary`, `ContextFetched` em `DynatraceHealth`
- [x] `dynatrace_checker.go`: `GetProblemContext` Top 5 (paralelo, timeout 15s)
- [x] `dynatrace_checker.go`: `GetEntityMetricsForProblem` para AVAILABILITY/ERROR (timeout 10s)
- [x] `types/healthcheck.ts`: sincronizar interface `DynatraceHealth`

### ✅ Fase 3 — Frontend (17/03/2026)
- [x] `HealthCheckResultsPanel.tsx`: Evidence Davis AI, MetricsSummary badges, RecentEvents, badge "Sem correlação K8s"
- [x] `HealthCheckResultsPanel.tsx`: botão "Analisar com AI" por problem + resultado colapsável

### ✅ Fase 4 — Avançado (18–19/03/2026)
- [x] `HealthCheckResultsPanel.tsx`: dedup cross-cluster `displayIdClusters` + badge "Afeta N clusters"
- [x] `dynatrace_checker.go`: escalada prd + ENVIRONMENT → Critical
- [x] `client.go`: cache `entityCache sync.Map` TTL 5min
- [x] `healthcheck.go`: `AnalyzeCorrelatedBatch` + `buildBatchCorrelatedPrompt`
- [x] `server.go`: rota `POST /api/v1/healthcheck/correlated/analyze-batch`
- [x] `client.ts`: `analyzeCorrelatedBatch()`
- [x] `HealthCheckResultsPanel.tsx`: `CorrelatedTab` com botão "Analisar tudo com AI"

---

## Contexto Técnico Crítico

### Arquivos-chave

| Arquivo | Responsabilidade |
|---------|-----------------|
| `internal/healthcheck/dynatrace_checker.go` | Checker problems DT |
| `internal/healthcheck/correlator.go` | Correlação K8s ↔ DT |
| `internal/healthcheck/models.go` | Todos os tipos do HC |
| `internal/healthcheck/orchestrator.go` | Orquestra checkers, busca reversa, correlator |
| `internal/dynatrace/client.go` | API entities + problems |
| `internal/dynatrace/metrics.go` | Métricas por problem/entidade |
| `internal/dynatrace/context.go` | Davis AI evidence + topologia |
| `internal/dynatrace/models.go` | `EntityStub`, `DTLabels`, `Entity` |
| `internal/storage/nodepool_registry_store.go` | Registry de node pools por cluster |
| `internal/storage/dependency_registry.go` | Registry de dependências de serviços |
| `internal/web/handlers/healthcheck.go` | Handler HTTP + prompts AI |
| `internal/web/handlers/nodepool_registry.go` | `extractNodePoolFromName()`, regex VMSS |
| `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx` | UI completa dos resultados |
| `internal/web/frontend/src/types/healthcheck.ts` | Types frontend |
| `internal/web/frontend/src/lib/api/client.ts` | API client centralizado |

### Padrão de nome de node AKS

Node pool `abastec` → nodes:
```
aks-abastec-12345678-vmss000000
aks-abastec-12345678-vmss000001
```
Regex: `^aks-(.+?)-\d{5,8}-vmss[0-9a-f]+` (já em `nodepool_registry.go:193`)

O scanner usa **label preferida** `kubernetes.azure.com/agentpool` (colocada pelo AKS) com fallback para o regex.

### Cluster name pattern

- Kubeconfig context: `akspriv-logistica-prd-admin` (com `-admin`)
- DT `DTLabels.HostGroup` / tag `dt.host_group.id`: `akspriv-logistica-prd` (sem `-admin`)
- `normalizeClusterName()` em `dynatrace_checker.go` já faz `TrimSuffix("-admin") + ToLower`

### Fluxo de credenciais

```
POST /api/v1/healthcheck/run {
    check_dynatrace: true,
    check_oneagent_signals: true,   ← novo
    ai_email: "x@y.com"
}
  → handler → tokensStore.GetTokens(ai_email)
  → req.DynatraceURL + req.DynatraceToken preenchidos
  → orchestrator → DynatraceChecker.CheckAll()
  → orchestrator → busca reversa (SearchProblemsForWorkloads)
  → orchestrator → OneAgentSignalsChecker.CheckAll()   ← novo
  → orchestrator → Correlate(result)
```

### DTLabels disponíveis (tags OneAgent)

| Campo | Tag Kubernetes | Uso |
|-------|---------------|-----|
| `AppName` | `app.kubernetes.io/name` | Nome do workload |
| `AppVersion` | `app.kubernetes.io/version` | Versão do app |
| `AppEnvironment` | `app.kubernetes.io/environment` | `prd\|hlg\|dev` |
| `HostGroup` | `dt.host_group.id` | Nome do cluster AKS (sem `-admin`) |
| `Namespace` | `k8s.namespace.name` | Namespace K8s |
| `ComponentSquad` | `devops.k8s.io/squad` | Squad responsável |
| `ComponentJourney` | `devops.k8s.io/journey` | Jornada de negócio |
| `GitHubRepoID` | `devops.k8s.io/github-repo-id` | Repo GitHub |
| `HelmChart` | `helm.sh/chart` | Chart Helm |
| `Stage` | `app.kubernetes.io/stage` | `stable\|canary` |

### Timeout budgets

| Operação | Timeout |
|---------|---------|
| `GetOpenProblems` (paginado) | 8s |
| `EnrichEntitiesWithK8s` | 5s |
| `GetProblemContext` (Top 5, paralelo) | 15s |
| `GetEntityMetricsForProblem` | 10s |
| `ListEntitiesByCluster` (Fase 5, 2 calls) | 5s |
| `BatchQueryMetrics` (Fase 5, N entidades) | 20s |
| **Recomendado `TimeoutDynatrace`** | 45s |
| **Recomendado `TimeoutOneAgent` (Fase 5)** | 30s |
