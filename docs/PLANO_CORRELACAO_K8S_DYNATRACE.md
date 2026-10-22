# Plano: Correlação Automática K8s ↔ Dynatrace no Health Check

**Data:** 18/03/2026
**Branch:** integracao-dyna
**Status:** 🟡 Fases 2 e 4 implementadas (19/03/2026) — Fase 1 e 3 pendentes

---

## Problema Atual

Os dois checks do Health Check rodam **isolados**:

- **K8s**: encontra CrashLoop, HPA no limite, OOMKilled, eventos de falha
- **DT**: encontra problems AVAILABILITY/ERROR mas sem saber se o K8s já detectou algo

Resultado: o operador vê duas listas separadas e precisa cruzar manualmente.

---

## Proposta: Motor de Correlação Bidirecional

```
Health Check Run
├── [paralelo] K8s Checks → lista de workloads problemáticos
│     CrashLoopBackOff, OOMKilled, HPA maxed, Events: Failed/BackOff
│
├── [paralelo] DT Checks → lista de problems por entidade
│     AVAILABILITY, ERROR, PERFORMANCE com entidades enriquecidas
│
└── Correlator (novo)
      ├── Match por nome: "payment-service" ↔ DT entity "payment-service:8080"
      ├── Match por namespace/cluster (K8sNamespace já disponível no DT)
      ├── Para cada match: CorrelatedHealthItem (K8s + DT juntos)
      ├── Escalada de severidade: se ambos indicam problema → CRÍTICO automático
      └── Auto-análise AI: K8s symptoms + DT evidence + Davis AI → 1 chamada unificada
```

---

## Novo Tipo: CorrelatedHealthItem

```go
// CorrelatedHealthItem une sintomas K8s com problemas DT para o mesmo workload
type CorrelatedHealthItem struct {
    WorkloadName  string
    Namespace     string
    Cluster       string

    // K8s side (pode existir sem DT match)
    K8sIssues     []HealthCheckResult
    K8sSeverity   Severity

    // DT side (pode existir sem K8s match)
    DTProblems    []DynatraceHealth
    DTSeverity    Severity

    // Combined
    FinalSeverity Severity   // pior dos dois lados
    Correlated    bool       // true = match encontrado dos dois lados

    // AI unificada (gerada automaticamente para Correlated + críticos)
    AIAnalysis    *string
    AITriggeredAt *time.Time
}
```

---

## Fases de Implementação

### Fase 1 — Busca Reversa K8s → DT *(novo)*

Quando K8s encontra workloads problemáticos, buscar no Dynatrace por entidades com esse nome.

**Novo método:**
```go
// SearchProblemsForWorkloads busca problems DT para uma lista de workloads K8s
func (c *DynatraceChecker) SearchProblemsForWorkloads(
    ctx context.Context,
    dtURL, dtToken string,
    workloads []string, // ex: ["payment-service", "order-api"]
    cluster string,
) []DynatraceHealth
```

**API Dynatrace usada:**
```
GET /api/v2/entities?entitySelector=type("SERVICE"),entityName.startsWith("payment-service")
GET /api/v2/problems?problemSelector=status("OPEN"),entityId("SERVICE-xxx")
```

**Arquivo:** `internal/healthcheck/dynatrace_checker.go` (novo método)

---

### Fase 2 — Motor de Correlação *(novo arquivo)*

**Arquivo:** `internal/healthcheck/correlator.go`

Responsabilidades:
- Receber `[]HealthCheckResult` (K8s) + `[]DynatraceHealth` (DT)
- Normalizar nomes para matching (lowercase, remover sufixos de versão, remover `-admin`)
- Produzir `[]CorrelatedHealthItem` com matches e itens sem match de cada lado
- Escalar severidade quando ambos os lados confirmam o problema

**Lógica de matching (por ordem de confiança):**
1. `K8sWorkload` do DT == nome do deployment K8s (match exato)
2. Nome do deployment K8s contido no `DisplayName` da entidade DT (match parcial)
3. `K8sNamespace` do DT == namespace do deployment K8s (match por namespace)

---

### Fase 3 — Auto AI para Correlated Críticos *(sem clique do usuário)*

Para itens `Correlated=true` com `FinalSeverity >= High`:

- Montar prompt combinado com todo o contexto disponível:
  - Eventos K8s (pod restarts, OOMKilled, BackOff)
  - Estado do HPA (réplicas atual/min/max, métricas)
  - DT Evidence (Davis AI context, root cause)
  - DT Métricas (error_rate, response_p90, throughput)
- Chamar AI de forma **assíncrona** (não bloqueia retorno do HC)
- Resultado cacheado no `CorrelatedHealthItem.AIAnalysis`
- Expiração: TTL de 10 minutos (não re-analisar durante um HC em andamento)

**Arquivo:** `internal/healthcheck/auto_analyzer.go` (novo)

---

### Fase 4 — Frontend: Visão Unificada no Health Check *(React)*

**Cards com badges visuais:**
- `K8s + DT` (roxo) — item correlacionado (ambos os lados encontraram problema)
- `K8s` (azul) — apenas sintomas K8s, sem match no DT
- `DT` (laranja) — apenas problem DT, sem workload K8s identificado com problema

**Dentro do card correlacionado:**
- Seção K8s: eventos, estado HPA, restarts
- Seção DT: severity, evidence, link para o problem no Dynatrace
- Seção AI: diagnóstico unificado (gerado automaticamente para críticos)

**Escalada visual:**
- `FinalSeverity = CRÍTICO` quando ambos confirmam → card com borda vermelha pulsante

---

## Comparativo: Proposta vs. Ideia Original

| Aspecto | Ideia Original | Esta Proposta |
|---|---|---|
| Direção da busca | K8s → DT (unidirecional) | Bidirecional (K8s ↔ DT) |
| Análise AI | Manual após montar lista | Automática para críticos correlacionados |
| Visibilidade | Duas listas separadas | Uma lista unificada com contexto completo |
| Latência | Sequencial (K8s, depois DT) | K8s + DT em paralelo, correlação ao final |
| Escalada de severidade | Separada por sistema | Automática quando ambos confirmam |
| Esforço do operador | Alto (correlação manual) | Baixo (sistema já correlaciona) |

---

## Dependências Técnicas

- `internal/dynatrace/client.go` — adicionar `SearchEntitiesByName()`
- `internal/healthcheck/dynatrace_checker.go` — adicionar `SearchProblemsForWorkloads()`
- `internal/healthcheck/correlator.go` — **novo arquivo**
- `internal/healthcheck/auto_analyzer.go` — **novo arquivo**
- `internal/healthcheck/types.go` — adicionar `CorrelatedHealthItem`
- `internal/web/handlers/healthcheck.go` — retornar `CorrelatedHealthItem[]` além dos resultados atuais
- `internal/web/frontend/src/components/HealthCheckingTab.tsx` — cards unificados

---

## Ordem de Implementação Recomendada

1. Fase 1 (backend — busca reversa) → testa via API isolada
2. Fase 2 (correlator) → unit tests com mocks de K8s e DT results
3. Fase 4 (frontend básico) → mostrar correlação sem AI ainda
4. Fase 3 (auto AI) → adicionar por último (depende de tudo estar estável)
