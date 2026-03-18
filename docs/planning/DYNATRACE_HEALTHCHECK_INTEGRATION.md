# Plano de Integração Dynatrace × Health Check

**Data:** 17/03/2026
**Branch:** `integração-dyna`
**Status:** 🟡 Fase 1 concluída (commit `8666044`) — Fase 2 pendente

---

## Diagnóstico: Por Que Não Funciona Hoje

O health check tem um `DynatraceChecker` implementado, mas 3 bugs fazem com que os resultados venham sempre vazios:

### Bug 1 — Cluster name matching falha (BLOQUEADOR PRINCIPAL)
**Arquivo:** `internal/healthcheck/dynatrace_checker.go:52-68`
O checker compara o nome do cluster do kubeconfig (ex: `akspriv-logistica-prd-admin`) com `DTLabels.HostGroup` do Dynatrace (ex: `akspriv-logistica-prd`). O sufixo `-admin` nunca coincide → todos os problems são filtrados silenciosamente.

**Fix:** Normalizar antes do match:
```go
func normalizeClusterName(name string) string {
    name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), "-admin")
    return name
}
// usar normalizeClusterName(stub.K8sCluster) == normalizeClusterName(cluster)
```

### Bug 2 — pageSize hardcoded em 10
**Arquivo:** `internal/dynatrace/client.go:129` (`GetOpenProblems`)
Busca no máximo 10 problems sem paginação. Clusters com mais de 10 problems ativos perdem dados.

**Fix:** Loop com `nextPageKey` até acabar (limite de segurança: 200 problems):
```go
for {
    // fetch page
    if result.NextPageKey == "" { break }
    // set nextPageKey param e continuar
}
```

### Bug 3 — Tag filter bloqueia quando vazio/incorreto
**Arquivo:** `internal/healthcheck/dynatrace_checker.go`
Se `DynatraceTagFilter` está vazio, o seletor muda e pode retornar 0 resultados dependendo da API. Se está preenchido com um valor inexistente, retorna 0 resultados. Nenhum dos dois casos produz erro visível.

**Fix:** Quando tagFilter vazio, buscar sem filtro de tag e depender apenas do cluster match.

---

## Recursos Disponíveis (Mapeados)

### Client Dynatrace (`internal/dynatrace/`)

| Método | Arquivo | Descrição | Usado no HC? |
|--------|---------|-----------|-------------|
| `GetOpenProblems(filter)` | `client.go:129` | Problems OPEN, pageSize **10 fixo** | ✅ Sim (bugado) |
| `GetProblem(problemID)` | `client.go:163` | Detalhe de um problem | ❌ Não |
| `GetEntity(entityID)` | `client.go:177` | Entidade com tags K8s + relações | ❌ Não |
| `EnrichEntitiesWithK8s(stubs)` | `client.go:269` | Extrai cluster/ns/workload dos tags OneAgent | ✅ Sim |
| `GetEntityMetricsForProblem(p)` | `metrics.go:236` | Latência P50/P90/P99, error rate, throughput | ❌ Não |
| `GetEntityEvents(id, from, to)` | `client.go:222` | Eventos recentes da entidade | ❌ Não |
| `GetProblemContext(problem)` | `context.go:214` | **Davis AI evidence + eventos + topologia + traces** em paralelo | ❌ Não |
| `TestConnection()` | `client.go` | Testa conectividade, retorna latência | ❌ No HC |

### Model `DynatraceHealth` atual (`internal/healthcheck/models.go:182`)
```go
type DynatraceHealth struct {
    ProblemID        string
    DisplayID        string       // P-XXXXX
    Title            string
    DTSeverity       string       // AVAILABILITY | ERROR | PERFORMANCE | RESOURCE_CONTENTION
    ImpactLevel      string       // APPLICATION | ENVIRONMENT | INFRASTRUCTURE | SERVICE
    Status           HealthStatus
    Severity         Severity
    StartTime        time.Time
    K8sNamespaces    []string
    K8sWorkloads     []string     // "namespace/workload"
    AffectedEntities []string     // display names
    Message          string
    Suggestions      []string
    CheckedAt        time.Time
    AppVersions      map[string]string
    GitHubRepos      []string
    Squads           []string
    Journeys         []string
    Environments     []string
}
```

### DTLabels disponíveis (extraídos de tags OneAgent)
- `AppName`, `AppVersion`, `AppEnvironment` (prd/hlg/dev)
- `HostGroup` → nome do cluster AKS (sem `-admin`)
- `GitHubRepoID`, `ComponentName`, `ComponentSquad`, `ComponentJourney`
- `Namespace`, `HelmChart`, `Stage` (stable/canary), `IsCanary`

### ProblemContext (retornado por `GetProblemContext`)
```go
type ProblemContext struct {
    Problem   *Problem
    Evidence  []Evidence     // Davis AI root cause evidences
    Events    []Event        // Eventos de todas as entidades afetadas (deduped)
    Topology  []TopologyNode // Relações calls/calledBy
    Traces    []Trace        // Distributed traces (requer DataExport scope)
}
```

### ProblemMetricsResponse (retornado por `GetEntityMetricsForProblem`)
- Por entidade: latência P50/P90/P95/P99 (ms), error rate (%), throughput (req/min)
- Janela: `StartTime - 30min` até `now` (ou `EndTime + 15min`)
- Resolução automática: 1m/5m/10m/30m conforme janela

---

## Plano de Implementação

### Fase 1 — Fix Básico (health check passa a retornar dados)

**Estimativa:** 1 sessão de código

#### 1.1 Fix cluster matching
- **Arquivo:** `internal/healthcheck/dynatrace_checker.go`
- Adicionar `normalizeClusterName()` que faz `TrimSuffix("-admin")` + lowercase + trim
- Aplicar em ambos os lados do match (cluster do kubeconfig e `stub.K8sCluster`, `DTLabels.HostGroup`)

#### 1.2 Paginação em GetOpenProblems
- **Arquivo:** `internal/dynatrace/client.go`
- Loop `nextPageKey` com limite de 200 problems
- Timeout por página (5s) + timeout total (timeoutSec do request)

#### 1.3 Tag filter resiliente
- **Arquivo:** `internal/healthcheck/dynatrace_checker.go`
- Quando `tagFilter == ""`: buscar sem filtro, depender apenas do cluster match
- Logar quantidade de problems encontrados antes e depois do filtro de cluster (debug)

---

### Fase 2 — Enriquecimento com Dados Disponíveis

**Estimativa:** 1-2 sessões de código

#### 2.1 Adicionar campos ao modelo DynatraceHealth
- **Arquivo:** `internal/healthcheck/models.go`
```go
// Novos campos a adicionar:
Evidence      []string           // Evidências Davis AI (root cause)
RecentEvents  []string           // Últimos 3 eventos ("TIPO: título")
MetricsSummary map[string]float64 // {"error_rate": 12.5, "response_p90_ms": 2300}
ContextFetched bool              // Se GetProblemContext foi chamado com sucesso
```

#### 2.2 Chamar GetProblemContext nos Top 5 problems
- **Arquivo:** `internal/healthcheck/dynatrace_checker.go`
- Ordenar problems por severidade (AVAILABILITY > ERROR > PERFORMANCE > RESOURCE_CONTENTION)
- Para os Top 5: chamar `client.GetProblemContext()` em goroutines paralelas
- Extrair evidências Davis: `context.Evidence[].Details` → `DynatraceHealth.Evidence`
- Extrair eventos recentes: top 3 por timestamp → `DynatraceHealth.RecentEvents`
- Timeout próprio: `min(timeoutSec-5, 15)` segundos

#### 2.3 Métricas para problems críticos (AVAILABILITY/ERROR)
- **Arquivo:** `internal/healthcheck/dynatrace_checker.go`
- Para problems com `DTSeverity == "AVAILABILITY"` ou `"ERROR"`: chamar `GetEntityMetricsForProblem()`
- Preencher `MetricsSummary`: extrair max error_rate e max P90 das entidades
- Timeout: 10s (não bloquear o health check por métricas)

#### 2.4 Sincronizar tipos para o frontend
- **Arquivo:** `internal/web/frontend/src/types/healthcheck.ts` (interface `DynatraceHealth`)
- Adicionar os novos campos opcionais: `evidence?`, `recent_events?`, `metrics_summary?`

---

### Fase 3 — Frontend HealthCheckResultsPanel

**Estimativa:** 1 sessão de código

#### 3.1 DynatraceProblemCard expandido
- **Arquivo:** `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx`
- Mostrar Evidence Davis AI em callout destacado (ícone 🧠, "Root cause identificado:")
- Mostrar MetricsSummary: badges `ERR: 12.5%` e `P90: 2300ms` ao lado da severidade
- Mostrar RecentEvents: lista compacta `• TIPO: título` (max 3)
- Badge "Sem correlação K8s" quando `k8s_workloads` vazio (ajuda debug de tags)

#### 3.2 Botão "Analisar com AI"
- **Arquivo:** `HealthCheckResultsPanel.tsx`
- Chama `POST /api/v1/dynatrace/problems/:problemId/analyze` diretamente
- Mostra loading inline no card
- Exibe resultado da análise colapsável abaixo do card
- (Alternativa: navegar para aba Dynatrace com problem pré-selecionado via state)

#### 3.3 Badge de qualidade da correlação
- Quando `context_fetched: true` → badge "🔍 Contexto completo"
- Quando `context_fetched: false` → tooltip "Contexto parcial — timeout ou sem permissão"

---

### Fase 4 — Melhorias Avançadas (opcional)

#### 4.1 Deduplicação cross-cluster
- Mesmo `problem_id` pode aparecer em múltiplos clusters
- No resultado final: agrupar por `DisplayID`, mostrar "Afeta N clusters"

#### 4.2 Escalada de severidade por impacto
- Se problem afeta ambiente `prd` E `ImpactLevel == "ENVIRONMENT"` → forçar `SeverityCritical`
- Considera `Environments[]` extraído dos DTLabels

#### 4.3 Cache de enrichment
- Problema: `EnrichEntitiesWithK8s` faz N chamadas à API Entity para cada check
- Solução: cache em memória TTL 5min por `entityID` → reduz latência no health check

---

## Checklist de Implementação

### ✅ Fase 1 — Fix Básico (commit `8666044` — 17/03/2026)

- [x] **`internal/healthcheck/dynatrace_checker.go`**: `normalizeClusterName()` + `matchesCluster()` aplicados no match
- [x] **`internal/dynatrace/client.go`**: Paginação em `GetOpenProblems` (pageSize=50, loop nextPageKey, limite 200)
- [x] **`internal/healthcheck/dynatrace_checker.go`**: `clusterNorm` calculado antes do loop; tagFilter opcional sem bloquear
- [ ] **Testar em ambiente real:** Executar health check com Dynatrace habilitado → deve retornar problems do cluster

### ✅ Fase 2 — Enriquecimento (17/03/2026)

- [x] **`internal/healthcheck/models.go`**: Adicionar campos `Evidence`, `RecentEvents`, `MetricsSummary`, `ContextFetched` ao `DynatraceHealth`
- [x] **`internal/healthcheck/dynatrace_checker.go`**: Chamar `GetProblemContext` nos Top 5 problems (goroutines paralelas, timeout 15s)
- [x] **`internal/healthcheck/dynatrace_checker.go`**: Chamar `GetEntityMetricsForProblem` para problems AVAILABILITY/ERROR (timeout 10s)
- [x] **`internal/web/frontend/src/types/healthcheck.ts`**: Sincronizar interface `DynatraceHealth` com novos campos

### ✅ Fase 3 — Frontend (17/03/2026)

- [x] **`HealthCheckResultsPanel.tsx`**: Mostrar Evidence Davis AI em callout destacado
- [x] **`HealthCheckResultsPanel.tsx`**: Mostrar MetricsSummary (error rate %, P90 ms) como badges
- [x] **`HealthCheckResultsPanel.tsx`**: Mostrar RecentEvents (top 3, compacto)
- [x] **`HealthCheckResultsPanel.tsx`**: Badge "Sem correlação K8s" quando workloads vazio
- [x] **`HealthCheckResultsPanel.tsx`**: Botão "Analisar com AI" → chama `/dynatrace/problems/:id/analyze`
- [x] **`HealthCheckResultsPanel.tsx`**: Exibir resultado da análise AI colapsável no card

### ✅ Fase 4 — Avançado (18/03/2026)

- [x] **`HealthCheckResultsPanel.tsx`**: Deduplicação cross-cluster por `DisplayID` — badge "Afeta N clusters" com tooltip de clusters
- [x] **`internal/healthcheck/dynatrace_checker.go`**: Escalada de severidade para `prd` + `ENVIRONMENT` impact → StatusCritical
- [x] **`internal/dynatrace/client.go`**: Cache em memória TTL 5min para `EnrichEntitiesWithK8s` (package-level `sync.Map`)

---

## Contexto Técnico Crítico

### Arquivos-chave e line numbers atuais
| Arquivo | Responsabilidade | Linhas críticas |
|---------|-----------------|-----------------|
| `internal/healthcheck/dynatrace_checker.go` | Checker principal | 22-143 (CheckAll), 52-68 (match bugado) |
| `internal/healthcheck/models.go` | DynatraceHealth struct | 182-204 |
| `internal/healthcheck/orchestrator.go` | Chama o checker | 479-482 |
| `internal/dynatrace/client.go` | API problems+entities | 129-160 (GetOpenProblems) |
| `internal/dynatrace/metrics.go` | Métricas por problem | 236-316 (GetEntityMetricsForProblem) |
| `internal/dynatrace/context.go` | Contexto completo | 214-337 (GetProblemContext) |
| `internal/web/handlers/health_check.go` | Handler HTTP | 79-86 (token population) |
| `internal/web/frontend/src/components/HealthCheckResultsPanel.tsx` | UI resultados | DynatraceProblemCard |
| `internal/web/frontend/src/types/healthcheck.ts` | Types frontend | DynatraceHealth interface |

### Fluxo de credenciais
```
POST /api/v1/healthcheck/run { check_dynatrace: true, ai_email: "x@y.com" }
  → handler/health_check.go:79 → tokensStore.GetTokens(ai_email)
  → req.DynatraceURL + req.DynatraceToken preenchidos
  → orchestrator.go:479 → dynatrace_checker.CheckAll(dtURL, dtToken, tagFilter, cluster)
```

### Cluster name pattern (WSL2/AKS)
- Kubeconfig context: `akspriv-logistica-prd-admin` (com `-admin`)
- Dynatrace `DTLabels.HostGroup`: `akspriv-logistica-prd` (sem `-admin`)
- Fix: `strings.TrimSuffix(name, "-admin")` antes de qualquer comparação

### Timeout budget para o checker (timeoutSec padrão = 20s)
- GetOpenProblems (paginado): até 8s
- EnrichEntitiesWithK8s: até 5s
- GetProblemContext (Top 5, paralelo): até 15s → separar em goroutine com select
- GetEntityMetricsForProblem (críticos): até 10s → separar em goroutine com select
- **Total real com Fases 1+2:** recomenda-se `TimeoutDynatrace = 45s` no request

---

## Notas de Implementação

1. **`GetProblemContext` pode retornar 403 em Traces** — `context.go` já trata isso graciosamente, o campo `Traces` fica vazio sem erro fatal.

2. **Evidence Davis AI** — `Evidence[].Details` é texto livre do Davis. Concatenar com `"; "` para o campo `Evidence []string` no modelo.

3. **`GetEntityMetricsForProblem` auto-seleciona métricas por tipo de entidade** (SERVICE → latência/errors; HOST → CPU/mem; DATABASE → query time). O `MetricsSummary` deve pegar as chaves mais relevantes para exibição.

4. **Normalização do cluster deve ser case-insensitive** — alguns contexts têm maiúsculas inconsistentes.

5. **Não usar `DynatraceTagFilter` como filtro exclusivo** — deve ser filtro adicional opcional. O match de cluster é o filtro primário.
