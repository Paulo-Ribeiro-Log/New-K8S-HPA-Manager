# Plano de Integração Dynatrace

## Objetivo

Botão "Analisar com Dynatrace" que busca problems abertos, correlaciona com recursos K8s via OneAgent e envia contexto completo para AI analisar origem, dependências e comportamento das aplicações.

**Não é automação de correção — é diagnóstico assistido por AI.**

---

## Contexto do Ambiente

- **Environment ID**: `nyr48864`
- **API Base URL**: `https://nyr48864.live.dynatrace.com/api/v2`
- **UI URL**: `https://nyr48864.apps.dynatrace.com`
- **Token**: individual por enquanto → migrar para service account (`DT_API_TOKEN` env var)
- **OneAgent**: instalado em todos os clusters → correlação K8s automática via tags

### Por que o OneAgent simplifica tudo

O OneAgent injeta automaticamente nas entidades Dynatrace:
```
kubernetes.cluster.name:    prod-cluster
kubernetes.namespace.name:  commerce
kubernetes.workload.name:   payment-service
kubernetes.pod.name:        payment-service-abc123
```
Quando um problem chega, já sabemos exatamente qual pod/deployment/namespace está envolvido — **sem correlator manual**.

---

## Fluxo de Análise

```
Usuário clica "Analisar com Dynatrace"
        ↓
GET /api/v2/problems?problemSelector=status("OPEN")
        ↓
Lista de problems OPEN exibida ao usuário
        ↓
Usuário seleciona um problem
        ↓
App extrai das entidades afetadas:
  - kubernetes.cluster.name
  - kubernetes.namespace.name
  - kubernetes.workload.name
        ↓
Busca estado K8s dos recursos envolvidos:
  - kubectl describe deployment/payment-service -n commerce
  - eventos K8s recentes do namespace
  - logs dos pods afetados (via AI Diagnostics existente)
        ↓
AI recebe contexto completo:
  - Problem Dynatrace (título, causa raiz, entidades, métricas, eventos)
  - Estado K8s dos recursos correlacionados
  - Topologia de dependências (quem chama quem)
        ↓
AI responde:
  - Origem do problema
  - Dependências no caminho crítico
  - Como o comportamento se propagou
  - O que investigar em seguida
```

---

## Arquitetura

### Novos arquivos

```
internal/dynatrace/
  client.go      — cliente HTTP Dynatrace API v2
  models.go      — structs (Problem, Entity, Event, Metric, Topology)

internal/web/handlers/dynatrace.go   — handler REST
internal/web/frontend/src/components/DynatraceTab.tsx
```

### Arquivos a modificar

```
internal/storage/user_tokens_store.go    — adicionar campos DT token/URL
internal/web/handlers/ai_tokens.go       — save/get DT token
internal/web/server.go                   — registrar rotas
internal/web/frontend/src/lib/api/client.ts       — métodos API
internal/web/frontend/src/hooks/useAPI.ts         — hooks
internal/web/frontend/src/components/ToolsMenu.tsx — entrada no menu
internal/web/frontend/src/pages/Index.tsx          — case "dynatrace"
```

---

## Endpoints de API

| Método | Rota | Descrição |
|--------|------|-----------|
| GET | `/api/v1/dynatrace/config` | Config atual (sem token) + status conectividade |
| POST | `/api/v1/dynatrace/test` | Testar conectividade com o token |
| GET | `/api/v1/dynatrace/problems` | Listar problems OPEN + entidades K8s |
| GET | `/api/v1/dynatrace/problems/:id` | Detalhe de um problem específico |
| POST | `/api/v1/dynatrace/problems/:id/analyze` | Análise AI: problema + K8s + topologia |

---

## Dynatrace API v2 — Endpoints usados

```
GET /api/v2/problems
    ?problemSelector=status("OPEN")
    &fields=+affectedEntities,+recentComments,+impactAnalysis,+rootCauseEntity
    &Authorization: Api-Token <token>

GET /api/v2/problems/{problemId}
    — detalhe completo com eventos, entidades, causa raiz

GET /api/v2/entities/{entityId}
    ?fields=+properties,+tags,+toRelationships.calls,+fromRelationships.calledBy
    — entidade com tags K8s e dependências

GET /api/v2/metrics/query
    ?metricSelector=builtin:service.errors.total.rate
    &entitySelector=entityId("<ID>")
    &from=now-1h
    — métricas no período do problema

GET /api/v2/events
    ?entitySelector=entityId("<ID>")
    &from=<startTime>&to=<endTime>
    — eventos (deploys, anomalias) no período
```

---

## Storage — Campos novos em UserTokens

```go
// internal/storage/user_tokens_store.go
DynatraceURL   string  // ex: https://nyr48864.live.dynatrace.com
DynatraceToken string  // Api-Token individual (ou via env DT_API_TOKEN)
```

Fallback: se `DynatraceToken == ""`, usa `os.Getenv("DT_API_TOKEN")`.
Fallback: se `DynatraceURL == ""`, usa `os.Getenv("DT_API_URL")`.

---

## Frontend — DynatraceTab.tsx

### Layout
```
┌─────────────────────────────────────────────────────┐
│  [Cluster: prod ▼]  [🔍 Buscar Problems]            │
├─────────────────────────────────────────────────────┤
│  OPEN PROBLEMS (3)                                  │
│                                                     │
│  🔴 Response time degradation — checkout-api        │
│     Afetados: checkout-api → payment-service        │
│     K8s: commerce / deployment/payment-service      │
│     Início: há 23min          [Analisar com AI →]   │
│                                                     │
│  🟠 Failure rate increase — auth-service            │
│     Afetados: auth-service → db-postgres            │
│     K8s: identity / deployment/auth-service         │
│     Início: há 1h2min         [Analisar com AI →]   │
└─────────────────────────────────────────────────────┘
```

### Ao clicar "Analisar com AI"
- Abre painel lateral (SplitView)
- Lado esquerdo: detalhes do problem Dynatrace (entidades, métricas, eventos, causa raiz DT)
- Lado direito: análise AI em streaming (igual ao AI Diagnostics atual)

### Sub-aba Config
- Campo URL Dynatrace
- Campo API Token (mascarado)
- Botão "Testar Conexão" → mostra latência e versão do ambiente
- Aviso: "Em produção, configure via variáveis de ambiente DT_API_URL e DT_API_TOKEN"

---

## Contexto enviado para a AI

```
=== PROBLEMA DYNATRACE ===
Título: Response time degradation on checkout-api
Severidade: PERFORMANCE
Início: 2026-03-15 14:32 UTC
Causa raiz detectada (Dynatrace): Slow dependency call to payment-service

=== ENTIDADES AFETADAS ===
SERVICE: checkout-api (K8s: cluster=prod, ns=commerce, workload=checkout-api)
SERVICE: payment-service (K8s: cluster=prod, ns=commerce, workload=payment-service)

=== DEPENDÊNCIAS (topologia) ===
checkout-api → payment-service → kafka-broker-01 (externo)
checkout-api → db-postgres (externo)

=== MÉTRICAS NO PERÍODO ===
checkout-api: error rate 4.2%, P95 latency 8.3s (baseline: 0.1%, 320ms)
payment-service: error rate 12.1%, P95 latency 11.2s

=== EVENTOS K8S (últimos 30min) ===
[deployment/payment-service] 2 pod restarts (OOMKilled)
[namespace/commerce] nenhum deploy recente

=== ESTADO K8S ATUAL ===
kubectl describe deployment/payment-service -n commerce:
  Replicas: 2/2 ready
  OOM limit: 512Mi (requests: 256Mi)
  Last restart: 8min ago

=== PERGUNTA ===
Com base no problema Dynatrace e estado K8s, identifique:
1. Origem provável do problema
2. Caminho de propagação entre serviços
3. Quais componentes K8s investigar em seguida
4. O que está fora do K8s e precisa ser investigado em outro sistema
```

---

## Checklist de Implementação

### Fase 1 — Backend ✅ CONCLUÍDA (15/03/2026)

- [x] Criar `internal/dynatrace/models.go`
  - [x] Struct `Problem` (id, title, status, severity, startTime, affectedEntities, rootCauseEntity, impactLevel)
  - [x] Struct `Entity` (entityId, displayName, type, tags map, relationships)
  - [x] Struct `MetricSeries` (metricId, data points)
  - [x] Struct `Event` (eventId, type, title, entityId, startTime)
  - [x] Struct `K8sCorrelation` + `ProblemSummary`
  - [x] `Entity.ExtractK8sCorrelation()` — extrai tags OneAgent automaticamente

- [x] Criar `internal/dynatrace/client.go`
  - [x] `NewClient(baseURL, apiToken string) (*Client, error)`
  - [x] `GetOpenProblems(ctx) ([]Problem, error)`
  - [x] `GetProblem(ctx, problemId) (*Problem, error)`
  - [x] `GetEntity(ctx, entityId) (*Entity, error)`
  - [x] `GetEntityMetrics(ctx, entityId, selector, from, to) (*MetricData, error)`
  - [x] `GetEntityEvents(ctx, entityId, from, to) ([]Event, error)`
  - [x] `EnrichEntitiesWithK8s(ctx, stubs) []EntityStub`
  - [x] Header `Authorization: Api-Token <token>` em todas as chamadas
  - [x] Fallback: `os.Getenv("DT_API_TOKEN")` / `os.Getenv("DT_API_URL")`

- [x] Adicionar campos ao `internal/storage/user_tokens_store.go`
  - [x] `DynatraceURL string`
  - [x] `DynatraceToken string`
  - [x] Migration SQLite (`ALTER TABLE ADD COLUMN`)

- [x] Atualizar `internal/web/handlers/ai_tokens.go`
  - [x] Save/Get dos campos Dynatrace
  - [x] Token mascarado na resposta GET (`has_dynatrace: bool`)

- [x] Criar `internal/web/handlers/dynatrace.go`
  - [x] `GetConfig` — retorna URL + `enabled` (sem expor token)
  - [x] `TestConnection` — testa conectividade + retorna latência ms
  - [x] `ListProblems` — problems OPEN + correlação K8s via OneAgent
  - [x] `GetProblem` — detalhe + entidades enriquecidas
  - [x] `AnalyzeProblem` — métricas + eventos + prompt estruturado + AI
  - [x] `buildDynatracePrompt` — 5 seções: origem, propagação, K8s, externos, próximos passos
  - [x] Sanitizer aplicado antes de enviar para AI

- [x] Registrar rotas em `internal/web/server.go`
  - [x] GET `/api/v1/dynatrace/config`
  - [x] POST `/api/v1/dynatrace/test`
  - [x] GET `/api/v1/dynatrace/problems`
  - [x] GET `/api/v1/dynatrace/problems/:problemId`
  - [x] POST `/api/v1/dynatrace/problems/:problemId/analyze`

### Fase 2 — Frontend ✅ CONCLUÍDA (15/03/2026)

- [x] Adicionar tipos e métodos em `src/lib/api/client.ts`
  - [x] `DynatraceProblem`, `DynatraceEntity`, `DynatraceConfig`
  - [x] Métodos: `getDynatraceProblems()`, `getDynatraceProblem(id)`, `analyzeDynatraceProblem(id, aiEmail)`
  - [x] `testDynatraceConnection()`, `getDynatraceConfig()`
  - [x] Campos Dynatrace no tipo de retorno de `getAITokens()` (`has_dynatrace`, `dynatrace_url`, `dynatrace_tag_filter`)

- [x] Hook inline `useQuery` em `DynatraceTab.tsx` (sem hook separado em useAPI.ts — inline é suficiente)

- [x] Criar `src/components/DynatraceTab.tsx`
  - [x] Lista de problems OPEN com badge de severidade colorido
  - [x] Badge: 🔴 AVAILABILITY, 🟠 ERROR, 🟡 PERFORMANCE, 🔵 RESOURCE_CONTENTION
  - [x] Coluna K8s: cluster / namespace / workload correlacionado
  - [x] Botão "Analisar com AI" por problema
  - [x] SplitView: detalhes Dynatrace (esquerda) + análise AI (direita)
  - [x] Graceful degradation: card "Configure o Dynatrace em AI Settings" quando não configurado

- [x] Atualizar `src/components/ToolsMenu.tsx`
  - [x] Adicionar item `{ id: "dynatrace", label: "Dynatrace", icon: AlertTriangle }`

- [x] Atualizar `src/pages/Index.tsx`
  - [x] Import `DynatraceTab`
  - [x] Case `"dynatrace"` no switch
  - [x] Adicionar `"dynatrace"` na condição que oculta stats cards

- [x] Configuração de credenciais no perfil do analista (`AISettingsTab.tsx`)
  - [x] Campos: Dynatrace URL + API Token (mascarado)
  - [x] Badge de status "Dynatrace Configurado/Não Configurado"
  - [x] Escopos necessários documentados na UI
  - [x] **Filtro por Tag** — campo opcional para filtrar problems por squad/time (ex: `SRE-LOGISTICA`)
  - [x] Filtro aplicado na API Dynatrace (`tag("...")` no `problemSelector`) — não no cliente
  - [x] Filtro salvo por analista (individual) — cada squad configura o seu
  - [x] Botão "Testar Conexão" diretamente na seção (mostra latência ms ou erro)

### Fase 2b — Health Check com Dynatrace ✅ CONCLUÍDA (15/03/2026)

- [x] `DynatraceChecker` em `internal/healthcheck/dynatrace_checker.go`
  - [x] Busca problems OPEN filtrados por cluster (via tags OneAgent) + tag do analista
  - [x] Mapeamento de severidade DT → HC (`AVAILABILITY→critical`, `ERROR→high`, etc.)
  - [x] Sugestões geradas por tipo de problem
- [x] `DynatraceHealth` struct em `internal/healthcheck/models.go`
- [x] `CheckDynatrace bool` + `AIEmail` + credenciais internos no `HealthCheckRequest`
- [x] `DynatraceResults []DynatraceHealth` no `HealthCheckResult`
- [x] Goroutine Dynatrace no orchestrator (paralela, com SSE progress)
- [x] `HealthCheckHandler` recebe `tokensStore` — popula credenciais DT automaticamente
- [x] `calculateSummary` inclui problems DT nos totais/críticos
- [x] `DynatraceHealth` interface no `healthcheck.ts` frontend
- [x] `check_dynatrace` + `ai_email` no `HealthCheckRequest` frontend

### Fase 3 — Refinamentos

- [x] Sanitizer: dados Dynatrace passam pelo `internal/sanitizer/` antes de enviar à AI
- [x] Filtro por tag de squad (individual por analista, configurável em AI Settings)
- [ ] Cache de entities: TTL 5min para não sobrecarregar API Dynatrace
- [ ] Audit trail: registrar análises no `historyTracker`
- [ ] Suporte a múltiplos environments Dynatrace (quando necessário)

---

## Observações Técnicas

- **Token nunca retorna na API** — `GetConfig` retorna só `{ baseUrl, enabled, hasToken: true/false }`
- **Graceful degradation** — sem token configurado, tab mostra card de configuração (não erro)
- **Rate limit Dynatrace** — API tem ~50 req/min; não fazer polling, apenas on-demand
- **Sanitizer obrigatório** — respostas Dynatrace podem conter IPs internos, credenciais em properties
- **Migração para service account** — suporte a `DT_API_TOKEN` env var já na implementação inicial

---

## Exemplo de Resposta AI Esperada

```
ORIGEM: O problema originou no payment-service, não no checkout-api.
Os 2 restarts por OOMKilled indicam que o payment-service está
processando payloads maiores do que o esperado, possivelmente
por aumento de tamanho de mensagens no kafka-broker-01.

PROPAGAÇÃO: kafka-broker-01 → payment-service (OOM) → timeouts
→ checkout-api acumula requisições pendentes → latência P95 sobe.

COMPONENTES K8S PARA INVESTIGAR:
1. deployment/payment-service — aumentar memory limit de 512Mi para 1Gi
2. Verificar HPA do payment-service — min replicas pode estar muito baixo
3. Checar ConfigMap de configuração do consumer Kafka

FORA DO K8S — investigar em outro sistema:
- kafka-broker-01: verificar tamanho médio das mensagens e throughput
- Se mensagens cresceram: origem no producer (fora deste cluster)
```
