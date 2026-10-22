# Análise Profissional — Integração Dynatrace

> Elaborada em 21/03/2026 — Branch `integracao-dyna`

---

## Inventário Atual (~5.560 linhas de código)

| Componente | Arquivo | Linhas | Estado |
|---|---|---|---|
| Cliente HTTP DT API v2 | `internal/dynatrace/client.go` | 605 | ✅ Funcional |
| Modelos e extração de correlação | `internal/dynatrace/models.go` | 342 | ⚠️ Lacunas |
| Coleta de métricas por tipo de entidade | `internal/dynatrace/metrics.go` | 385 | ✅ Funcional |
| Contexto rico (evidências, eventos, topologia, traces) | `internal/dynatrace/context.go` | 338 | ⚠️ Lacunas |
| 8 endpoints REST | `internal/web/handlers/dynatrace.go` | 701 | ✅ Funcional |
| Correlação bidirecional K8s↔DT | `internal/healthcheck/correlator.go` | 221 | ✅ Funcional |
| Checker para Health Check | `internal/healthcheck/dynatrace_checker.go` | 641 | ⚠️ Lacunas |
| Interface React completa | `internal/web/frontend/src/components/DynatraceTab.tsx` | 2327 | ⚠️ Lacunas |

---

## O que está correto e funcionando bem

- ✅ Coleta de métricas P50/P90/P95/P99 com séries temporais por tipo de entidade (SERVICE, CLOUD_APPLICATION, HOST, PROCESS_GROUP, APPLICATION)
- ✅ Correlação bidirecional: DT→K8s (CheckAll) e K8s→DT (SearchProblemsForWorkloads com busca reversa por nome)
- ✅ Deduplicação de entidades com cache em memória (TTL 5min via `sync.Map`) — evita sobrecarga na API
- ✅ Escalada automática de severidade quando K8s + DT confirmam o mesmo problema (ambos >= High → Critical)
- ✅ Filtro web-only: descarta problems cujas entidades são todas APPLICATION/BROWSER/SYNTHETIC (sem relevância K8s)
- ✅ Sanitização automática (IPv4, JWT, Bearer, passwords) antes de enviar contexto para AI
- ✅ VRP (Visual Resolution Path) com algoritmo DAG, nós arrastáveis e zoom
- ✅ Múltiplas tentativas de fallback para identificar cluster (`kubernetes.cluster.name` > `dt.host_group.id` > Management Zone)
- ✅ Histórico de análises AI com metadata completo (DisplayID, severidade, squads, timestamps)
- ✅ Node Pool Registry para correlacionar entity names padrão `aks-<pool>-XXXXXXXX-vmssXXXX` com cluster/vm-size
- ✅ Análise AI com prompt rico: min/avg/max por métrica, severity labels (🔴/🟡/🟢), instrução de não usar kubectl
- ✅ GitHub releases correlacionadas via `devops.k8s.io/github-repository-id` + app version
- ✅ Timeout em todas as chamadas à API DT com context propagation

---

## Problemas Identificados

### 1. Correlação Falha para Entidades `APPLICATION` (causa do "TMS Embarcador")

`ExtractK8sCorrelation` faz **apenas 1 nível de busca**: vai na entidade e lê suas tags. Funciona para `CLOUD_APPLICATION` e `SERVICE` com OneAgent, mas **entidades do tipo `APPLICATION`** (que representam a visão APM/RUM da aplicação) não recebem tags `kubernetes.*` diretamente — o DT não as injeta nesse nível.

A correlação existe, mas está **1 nível abaixo**:
```
APPLICATION "TMS Embarcador"
  → GetEntity() → tags → ExtractK8sCorrelation() → nil  ← FALHA AQUI
```

O correto seria:
```
APPLICATION "TMS Embarcador"
  → GetEntity()
    → toRelationships["isServiceOf"] / fromRelationships["hasApplication"]
      → SERVICE filho → GetEntity() → tags → ExtractK8sCorrelation() → ✅ namespace/cluster
```

O fallback de display name (`"TMS Embarcador"` → `"tms-embarcador"`) adicionado como paliativo é uma heurística que pode dar falsos positivos. **A correção definitiva é fazer o traversal de relações 1 nível abaixo.**

---

### 2. Enriquecimento de Contexto Limitado ao TOP 5

Em `dynatrace_checker.go`, `enrichWithContext()` busca evidências Davis, eventos e métricas **apenas para os 5 problems mais severos**. Um cluster com 10 problems AVAILABILITY vai ignorar os demais completamente — a aba K8s↔DT mostra informação incompleta.

---

### 3. Busca Reversa Limitada a 10 Workloads

`SearchProblemsForWorkloads` limita a 10 nomes (`names[:10]`). Em namespaces com muitos deployments problemáticos simultaneamente, os demais são silenciosamente ignorados.

---

### 4. Sem Deployment Events do Dynatrace

O DT rastreia via OneAgent **todos os eventos de deploy, config change e escala** ocorridos no cluster. Endpoint: `GET /api/v2/events?eventType=DEPLOYMENT`. Esse dado é **fundamental para correlacionar "o problema começou logo após o deploy da versão X"** e não está sendo usado em nenhum lugar da integração.

---

### 5. Traces Distribuídos Inacessíveis na Prática

`GetProblemContext` tenta buscar traces via `/distributed-tracing/traces`, mas esse endpoint exige o escopo **`DataExport`** que praticamente nenhum token de API padrão tem habilitado. Quando falha, registra o erro mas o usuário fica sem informação de trace.

O que o DT fornece **sem esse escopo** e que não estamos extraindo separadamente são as **`transactional evidences`** já presentes nas evidências Davis — que incluem o trace ID e a request problemática identificada automaticamente pelo Davis AI.

---

### 6. Métricas que Faltam e Fariam Diferença Real

Das métricas coletadas, várias são úteis. Mas faltam as que mais impactam decisões de escala e ajuste de recursos:

| Métrica | Seletor DT | Por que importa |
|---|---|---|
| CPU requests vs limits ratio | `builtin:kubernetes.workload.requestsLimits.cpu` | Mostra se o container está no limite antes de throttle começar |
| Memory requests vs limits | `builtin:kubernetes.workload.requestsLimits.memory` | Mostra risco de OOMKill iminente |
| Pods em estado Pending | `builtin:kubernetes.workload.pods.pendingFraction` | Indica falta de recursos no cluster — o HPA quer escalar mas não consegue |
| OOMKilled count | `builtin:kubernetes.workload.pods.oomKilled` | Indica memory limit inadequado — escalar não resolve, precisa aumentar limit |
| HPA replicas atuais | `builtin:kubernetes.hpa.currentReplicas` | Mostra se o HPA já está no máximo |
| HPA desired vs current | `builtin:kubernetes.hpa.desiredReplicas` | Se desired > current → escalando mas sem capacity |
| Latência por endpoint | `builtin:service.keyRequest.response.time` | P95 por rota específica, não só o serviço inteiro — identifica qual endpoint está lento |
| GC pause (JVM) | `builtin:tech.jvm.gc.suspensionTime` | Para Java: identifica GC como causa de latência sem precisar de profiler |

---

### 7. Sem Timeline Visual do Incidente

O DT sabe exatamente o que aconteceu antes do problem abrir: CPU subiu às 14:32, primeiro erro às 14:35, problem aberto às 14:36. Esses dados existem nos eventos e evidências, mas o frontend não os consolida em nenhuma **linha do tempo cronológica**. O usuário precisa inferir a sequência olhando timestamps separados em abas diferentes.

Uma timeline consolidando: deploys → eventos K8s → métricas → abertura do problem → eventos durante o incident transformaria completamente a capacidade de investigação.

---

### 8. Sem Integração com SLOs do Dynatrace

Se o ambiente tem SLOs configurados (taxa de erro < 0.1%, latência P95 < 2s), o DT calcula o **error budget burn rate** em tempo real. Um problem que queimou 30% do budget mensal em 1 hora é muito mais grave que um que queimou 0.01%. Essa informação é crítica para priorização, mas não estamos acessando o endpoint `/api/v2/slo`.

---

### 9. AI sem Substância quando Métricas Estão Vazias

O prompt instrui a AI a citar valores numéricos reais. Mas quando a janela de métricas não retornou dados (entidade sem instrumentação completa, problema resolvido há muito tempo, timeout), o prompt manda `"⚠️ Nenhuma métrica retornou dados"` e a AI fica sem base para analisar — mas ainda tenta responder, resultando em análises genéricas e inúteis.

Falta um **fluxo alternativo**: quando métricas são vazias, usar somente evidências Davis + eventos + topologia como contexto (que geralmente sempre existem) com um prompt adaptado para esse cenário.

---

### 10. Sem Registro de Ações no Problem DT

O DT tem endpoint `POST /api/v2/problems/{problemId}/comments`. Quando o HPA Manager executa uma ação (escalou HPA, reiniciou deployment, fez rollback), **nunca registra isso no problem DT**. Os outros SREs que olham o problem no console DT não sabem que já foi tomada alguma ação. A rastreabilidade fica só no audit trail interno da ferramenta.

---

### 11. Matching de Correlação Frágil para Nomes Divergentes

`newWorkloadKey()` faz lowercase + remove `:port`. Mas o DT às vezes usa o nome do `SERVICE` (ex: `"payment-api"`) enquanto o K8s usa o nome do `Deployment` (ex: `"payment-service"`). Sem saber o mapeamento, a correlação falha silenciosamente — o workload aparece como "sem correlação DT" mesmo tendo um problem ativo.

---

### 12. Sem Alertas Proativos

A ferramenta só mostra problems quando o usuário abre a aba DT manualmente ou agenda um Health Check. Não existe nenhum mecanismo de **notificação proativa**: "o problem P-123 de AVAILABILITY abriu no cluster que você gerencia." Ao menos um evento SSE publicado quando problems novos de alta severidade aparecem seria valioso para resposta imediata.

---

## Prioridades de Implementação

| # | O que fazer | Impacto | Esforço | Arquivo(s) |
|---|---|---|---|---|
| **1** | **Traversal de relações para APPLICATION entities** — quando entity não tem tags K8s, buscar entidades filhas via `toRelationships`/`fromRelationships` | Corrige "TMS Embarcador" e todos APPLICATION entities | Médio | `dynatrace/client.go`, `models.go` |
| **2** | **Timeline visual do incidente** — consolida deploy events + K8s events + evidências Davis em linha do tempo cronológica no frontend | Muda completamente a qualidade de investigação | Médio | `context.go`, `DynatraceTab.tsx` |
| **3** | **Deploy events DT** — buscar `GET /api/v2/events?eventType=DEPLOYMENT` no período do problem e incluir no contexto AI e na timeline | "O problema começou após deploy da v2.3.1" — fundamental | Baixo | `context.go`, `handlers/dynatrace.go` |
| **4** | **Métricas faltantes** — adicionar CPU/Memory requests vs limits, pods Pending, OOMKill, HPA replicas ao conjunto de métricas K8s | Diagnóstico de resource pressure fica completo | Baixo | `dynatrace/metrics.go` |
| **5** | **Fallback AI sem métricas** — prompt alternativo baseado em evidências Davis + eventos quando métricas estão vazias | Elimina respostas genéricas inúteis | Baixo | `handlers/dynatrace.go` |
| **6** | **Comentário no problem DT após ação** — `POST /api/v2/problems/{id}/comments` após scale/restart/rollback executado | Rastreabilidade para o time inteiro no console DT | Baixo | `dynatrace/client.go`, handlers de ação |
| **7** | **Enriquecimento de contexto além do TOP 5** — ao menos para todos CRITICAL e HIGH | Completude do Health Check sem problemas ignorados | Baixo | `healthcheck/dynatrace_checker.go` |
| **8** | **SLOs** — buscar burn rate do SLO afetado via `/api/v2/slo` e incluir no contexto | Quantifica impacto de negócio real | Alto esforço |`dynatrace/client.go`, `handlers/dynatrace.go` |
| **9** | **Latência por endpoint** — `builtin:service.keyRequest.response.time` separado por rota | Identifica qual endpoint específico está lento | Baixo | `dynatrace/metrics.go` |
| **10** | **SSE para novos problems** — polling leve e publicação no broker SSE quando AVAILABILITY/ERROR abrem | Resposta proativa sem precisar abrir a aba | Alto esforço | `handlers/dynatrace.go`, `sse/` |
