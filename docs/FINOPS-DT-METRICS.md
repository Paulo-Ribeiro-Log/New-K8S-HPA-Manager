# FinOps — Métricas Históricas: Dynatrace (primário) → Prometheus (fallback)

Checklist de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md`.

**Contexto:** O FinOps usa métricas históricas de CPU/memória para calcular desperdício
(`waste_brl`) por workload. A proposta é usar Dynatrace como fonte primária (retenção maior,
percentis nativos, token já configurado) e manter Prometheus como fallback para ambientes
sem DT (homologação).

**Fluxo implementado:**
```
Dynatrace configurado? → sim → busca métricas DT (batch, todos os workloads)
                       → não → Prometheus disponível? → sim → busca métricas Prometheus
                                                      → não → análise histórica omitida
DT parcial? → workloads sem dados DT → Prometheus preenche o restante
```

---

## Fase 1 — Client Dynatrace para métricas de workload ✅ CONCLUÍDA

**Arquivo:** `internal/dynatrace/finops_metrics.go` ← CRIADO

- [x] `GetAllWorkloadMetrics(ctx, windowDays) (map[string]WorkloadMetrics, error)`
  - Executa 4 queries batch: CPU avg, CPU P95, Mem avg, Mem P95
  - `splitBy("k8s.namespace.name","k8s.workload.name")` — retorna todos os workloads de uma vez
  - Metrics: `builtin:container.cpu.usageMilliCores`, `builtin:container.memory.workingSet`
  - Aggregation: `:avg` e `:percentile(95)` com `resolution=inf`
  - `from`: `now-<windowDays>d`, `to`: `now`
  - Chave de retorno: `"namespace/workload"` — compatível com FinOps

- [x] Retorna `map[string]WorkloadMetrics` com CPUAvgMillicores, CPUP95Millicores, MemAvgBytes, MemP95Bytes

---

## Fase 2 — DTEnricher + PrometheusEnricher parcial ✅ CONCLUÍDA

**Arquivo:** `internal/finops/dynatrace_enricher.go` ← CRIADO
**Arquivo:** `internal/finops/prometheus_enricher.go` ← MODIFICADO
**Arquivo:** `internal/finops/models.go` ← MODIFICADO
**Arquivo:** `internal/finops/calculator.go` ← MODIFICADO

- [x] `DTEnricher.EnrichWorkloads(ctx, workloads)` — preenche CPU/Mem P95+avg via DT; retorna
  `map[string]bool` com as chaves "namespace/workload" enriquecidas pelo DT
- [x] `PrometheusEnricher.EnrichWorkloadsPartial(ctx, workloads, dtEnriched)` — executa o enrichment
  Prometheus apenas para workloads não cobertos pelo DT; marca `MetricsSource = "prometheus"`
- [x] `FinOpsWorkload.MetricsSource string` — `"dynatrace"` | `"prometheus"` | `""` (sem dados)
- [x] `Calculator.BuildReport` aceita `dtEnricher *DTEnricher` e `enricher *PrometheusEnricher`:
  DT roda primeiro, Prometheus preenche lacunas

---

## Fase 3 — Wiring no Handler / Server ✅ CONCLUÍDA

**Arquivo:** `internal/web/handlers/finops.go` ← MODIFICADO
**Arquivo:** `internal/web/server.go` ← MODIFICADO
**Arquivo:** `internal/storage/user_tokens_store.go` ← MODIFICADO

- [x] Interface `dtTokenReader` com `GetDynatraceConfig() (url, token string, ok bool)` — evita
  import circular entre `handlers` e `dynatrace`
- [x] `UserTokensStore.GetDynatraceConfig()` — busca token do primeiro usuário configurado
- [x] `FinOpsHandler.GetReport` cria `DTEnricher` automaticamente se DT token disponível
- [x] `server.go` passa `aiTokensStore` para `NewFinOpsHandler`

---

## Fase 4 — Frontend: indicar fonte dos dados ✅ CONCLUÍDA

**Arquivo:** `internal/web/frontend/src/components/FinOpsTab.tsx` ← MODIFICADO

- [x] Ler `workload.metrics_source` ao exibir a coluna de desperdício na aba Oportunidades
  - Badge pequeno ao lado do valor: `DT` (azul) ou `Prom` (laranja) ou nenhum badge se sem dados

- [x] Badge DT/Prom também exibido em CPU P95, Mem P95 e avg_replicas_cost_brl na aba HPA Histórico

- [x] Tooltip no checkbox "Análise histórica" indicar: `"Fonte: Dynatrace (com fallback Prometheus)"`

- [x] Nenhuma mudança na lógica de cálculo — `waste_brl` já vem pronto do backend

---

## Variáveis / dependências

- DT token e URL já lidos de `UserTokensStore` (configurado na aba AI Settings → Dynatrace)
- Métricas DT usadas:

| Métrica DT | Equivalente Prometheus | Unidade |
|---|---|---|
| `builtin:container.cpu.usageMilliCores` | `container_cpu_usage_seconds_total` | millicores |
| `builtin:container.memory.workingSet` | `container_memory_working_set_bytes` | bytes |

---

## Arquivos principais

```
internal/dynatrace/finops_metrics.go              ← CRIADO  (Fase 1)
internal/finops/dynatrace_enricher.go             ← CRIADO  (Fase 2)
internal/finops/prometheus_enricher.go            ← MODIFICADO (Fase 2)
internal/finops/models.go                         ← MODIFICADO (Fase 2)
internal/finops/calculator.go                     ← MODIFICADO (Fase 2)
internal/web/handlers/finops.go                   ← MODIFICADO (Fase 3)
internal/web/server.go                            ← MODIFICADO (Fase 3)
internal/storage/user_tokens_store.go             ← MODIFICADO (Fase 3)
internal/web/frontend/src/components/FinOpsTab.tsx ← MODIFICADO (Fase 4)
```
