# FinOps — Deep Analysis de node pool

**Status**: 🟢 implementado na branch `feat/finops-deep-analysis` (aguardando validação na UI e merge)

## Objetivo

Levar para a ferramenta a análise que hoje é feita à mão, pool a pool, com `kubectl describe/top`,
spec dos pods, HPAs e `az vm list-skus` (caso real: `calculofrete` em `akspriv-oferta-prd` — 51 nodes
F4s_v2 reservando 166 cores e usando 22,8, com a memória a 90–98%). O resultado esperado não é "mais um
número de desperdício", e sim o **diagnóstico do pool**:

- qual recurso **trava o agendamento** (request) e qual é o **gargalo real** (uso), e se eles divergem;
- quanto do node é **custo fixo de DaemonSets**;
- quais workloads estão **sub-requisitados em memória** (uso > request) e o **overcommit** de limits;
- como os **HPAs** reagem a uma mudança de request (HPA por memória, preso no mínimo/máximo);
- **throttling de CPU** por workload (latência);
- **quantos nodes** o pool precisa depois do right-sizing, por SKU candidata (várias famílias e
  gerações), com custo, disco efêmero, zonas e geração de CPU;
- **relatório Markdown** exportável.

## O que já existe e é reaproveitado

| Já existe | Onde | Uso na Deep Analysis |
|---|---|---|
| P95/avg/pico (com data) por workload, request/limit recomendado, histórico de HPA | `FinOpsReport` (cache do último `GET /finops/report`, `FinOpsReportCacheStore`) | Histórico por workload (sem re-scan de 2 min) |
| Preço e specs por SKU | `CloudPricer` (`pricerForCluster`) | Custo das alternativas |
| Benchmark de CPU por SKU | `vm_perf.go` (`PerfSet`) | Comparação de desempenho (quando medido) |
| node → pool | `nodePoolLabelFromNode` | Escopo da coleta |

O que **não** existe (é o escopo deste plano): diagnóstico de alocação, custo de DaemonSet por node,
overcommit, interação request×HPA, throttling, simulação de nº de nodes, capacidades de SKU e o relatório.

## Arquitetura

```
GET /api/v1/finops/deep-analysis?cluster=X&pool=Y[&headroom=0.8][&format=markdown]
        │
        ├─ coleta AO VIVO, restrita ao pool (rápida, segundos):
        │    Nodes do pool (allocatable, capacity, zona, região, SKU, spot, criação)
        │    Pods Running nesses nodes (requests/limits, owner → Deployment/StatefulSet/DaemonSet)
        │    HPAs dos workloads do pool (spec.metrics, min/max, current)
        │    metrics-server: uso por pod e por node (best-effort)
        │
        ├─ histórico: último FinOpsReport do cluster em cache (P95, pico, HPA history) — opcional
        │    (sem cache: segue só com o ao vivo, com aviso "rode Analisar para ter histórico")
        │
        ├─ Prometheus (best-effort, F2): throttling por pod do pool
        ├─ catálogo de SKUs (F3): `az vm list-skus -l <região>` com cache em disco (24h)
        │
        └─ finops.BuildPoolDeepAnalysis(input) → PoolDeepAnalysis   (lógica PURA, testável)
```

Princípios:
- **Toda a lógica de cálculo é pura** (`internal/finops/deep_analysis*.go`), recebendo um
  `DeepAnalysisInput` já coletado. O handler só coleta e monta o input. Testes cobrem a lógica com
  fixtures (inclusive os números reais do `calculofrete`).
- **Leitura apenas**: nenhum endpoint altera o cluster. Sem `RequireSREGroup` (mesmo critério de
  `GET /finops/rightsizing`).
- **Best-effort por fonte**: falha de metrics-server, Prometheus, cache ou catálogo vira `warnings[]`
  na resposta, nunca erro 500. Sem uso real, a análise informa "sem dados" em vez de supor 0.

## Cálculos

### Alocável estimado de uma SKU (para SKUs em que o pool nunca rodou)

Fórmula de reserva da AKS (Kubernetes ≥ 1.29), validada contra o `calculofrete` real
(F4s_v2: 3860m e ~5,8 GiB alocáveis observados):

- CPU reservada: 1 core → 60m, 2 → 100m, 4 → 140m, 8 → 180m, 16 → 260m, 32 → 420m, 64 → 740m.
- Memória reservada: `min(20 MB × maxPods + 50 MB, 25% da memória)` + eviction `100 Mi`.

Para a SKU **atual**, usa-se o alocável **medido** nos nodes (fonte da verdade); a fórmula só entra
para as candidatas.

### Diagnóstico de alocação (por pool)

- `request_pct` = Σ requests / Σ allocatable (CPU e memória); `usage_pct` = Σ uso / Σ allocatable,
  onde uso = P95 histórico × pods quando há cache, senão o uso ao vivo.
- **Recurso que trava o agendamento** = o de maior `request_pct`. **Gargalo real** = o de maior `usage_pct`.
- **Divergência** quando os dois são diferentes, ou quando `request_pct(CPU) ≥ 2 × usage_pct(CPU)`
  (pool dimensionado por request inflado) → texto explicativo.
- `limits_pct` (overcommit) = Σ limits / Σ allocatable.

### DaemonSets

Por node: Σ requests e Σ uso (média e máximo entre os nodes) de pods com owner `DaemonSet`;
`overhead_pct` = uso médio / allocatable; custo mensal = `overhead_pct(mem)` × custo do pool.
Sinaliza DaemonSet sem request de memória e com uso > request.

### Workloads

Por workload no pool: pods no pool, request/limit por pod, uso ao vivo (média/máx por pod), P95/pico
históricos (cache), `mem_usage/request`, `cpu_usage/request`, participação na CPU reservada do pool.
Flags: `mem_under_requested` (uso > request), `cpu_over_requested` (P95 < 25% do request),
`no_requests`, `no_mem_limit`, `high_mem_limit_ratio` (limit > 2× request).
Recomendação: request de CPU no P95 × 1,2 (mínimo de 50m); request de memória = max(P95, uso atual) × 1,2;
limit de memória = max(P95, pico) × 1,3 (mesmo critério de `recommendedLimits`).

### HPAs (F2)

Métricas do `spec.metrics` (Resource cpu/memory, Pods, External). Estado: `pinned_min` (réplicas =
min e uso ≪ alvo), `pinned_max` (réplicas = max). Alerta de **HPA por memória preso no máximo** (padrão
JVM: o heap não é devolvido). **Simulação**: utilização projetada = uso médio / request recomendado;
se ultrapassar o alvo do HPA → "baixar o request dispara scale-up" (caso `frete-hub`).

### Throttling (F2)

`sum(rate(container_cpu_cfs_throttled_periods_total[5m])) / sum(rate(container_cpu_cfs_periods_total[5m]))`
por pod do pool, P95 na janela do relatório, agregado por workload. Acima de 5% → alerta de latência
(sugere subir/remover o CPU limit e request no P95 para APIs sensíveis).

### Simulação de nodes por SKU

Para cada candidata (SKU atual + mesma contagem de vCPU nas famílias D/E/F das gerações correntes,
Intel e AMD, + o dobro de vCPU):

```
livre_cpu = allocatable_cpu − requests de DaemonSet por node
livre_mem = allocatable_mem − max(requests, uso) de DaemonSet por node
livre_pods = maxPods − pods de DaemonSet por node
nodes = ceil(max( Σcpu_req_apps / (livre_cpu × headroom),
                  Σmem_apps    / (livre_mem × headroom),
                  pods_apps    / livre_pods ))
nodes = max(nodes, nº de zonas do pool, 3)
```

Dois cenários: **requests atuais** (o que acontece trocando só a VM) e **requests recomendados**
(depois do right-sizing). `Σmem_apps` = max(Σ requests, Σ uso P95). Custo = nodes × preço/h × 730 × câmbio.
Inclui o **impacto da perda de um node** (`1/nodes` do pool) e alerta quando passa de ~15%.

### Capacidades de SKU (F3)

`az rest` em `Microsoft.Compute/skus?$filter=location eq '<região>'` (filtro no servidor, ~5 s; o
`az vm list-skus` não terminou em 8 minutos no brazilsouth), com cache em disco por região (7 dias),
atualização em segundo plano e espera de até 20 s na primeira carga. Campos:
`EphemeralOSDiskSupported`, zonas, `vCPUsPerCore` (SMT), restrições. Geração de CPU por série
(tabela estática: F_v2 → Intel Skylake/Cascade Lake, D_v4 → Cascade Lake, D_v5 → Ice Lake, Das_v5 →
AMD Milan, Das_v6/Fas_v6 → AMD Genoa, …). Alerta quando o pool usa disco efêmero e a candidata não suporta.

## Fases

- [x] **F1 — Backend núcleo**: tipos, alocável estimado, diagnóstico de alocação, DaemonSets,
  workloads/overcommit, simulação de nodes (requests atuais × recomendados), coletor ao vivo do pool,
  merge com o cache do relatório, endpoint `GET /finops/deep-analysis`, testes.
- [x] **F2 — HPA e throttling**: métricas/estado dos HPAs, simulação request×HPA, throttling via Prometheus.
- [x] **F3 — Catálogo de SKUs**: capacidades (efêmero, zonas, SMT) e geração de CPU nas candidatas.
- [x] **F4 — Frontend**: aba **Deep Analysis** no FinOps (lista de pools do último relatório) + modal por
  pool com as seções (Resumo, Alocação, Nodes, DaemonSets, Workloads, HPAs, Simulação de VMs).
- [x] **F5 — Relatório Markdown**: `format=markdown` no endpoint (`RenderDeepAnalysisMarkdown`) + botão "Exportar Markdown" no modal.
- [x] **F6 — Documentação**: entrada em `docs/history/CHANGELOG.md` e este plano.

## Verificação (por fase)

`go test ./internal/finops/... ./internal/web/handlers/... -race`, `go vet`, `go fmt`, `make build`;
frontend: `npx tsc --noEmit -p tsconfig.app.json` comparado ao baseline e `./rebuild-web.sh -b`.
Validação real (pelo usuário, com VPN): Deep Analysis do `calculofrete` deve reproduzir os números da
análise manual (166c reservados / ~23c usados, ~1,8 GiB de DaemonSet por node, ~15–16 nodes de 16 GB).

## Validação com dados reais (calculofrete, 09/10/2026)

Rodada sem servidor, chamando o mesmo código do endpoint contra `akspriv-oferta-prd` (só leitura):
coleta de 51 nodes / 748 pods / 18 HPAs em ~3 s, throttling de 562 pods em ~6 s, catálogo de SKUs em ~5 s.
Bateu com a análise manual: 166 cores reservados, 209 GiB de memória reservada, 33 nodes com memória
≥ 90%, ~1,8 GiB e 510m de DaemonSet por node. Ajustes feitos a partir dela: soma de P95 com DaemonSets
ao vivo (o histórico deles é o pior node do cluster), HPA fixo sem ajuste de request, throttling < 1%
fora da tabela, catálogo via `az rest`.
