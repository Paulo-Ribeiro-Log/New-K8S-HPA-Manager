# FinOps Dashboard — Plano de Implementação

**Objetivo:** Dashboard web para análise de custo real dos clusters AKS, com precificação baseada na Azure Pricing API, alocação de custo por workload/namespace, cenários de custo para HPAs e conversão automática USD→BRL.

**Branch:** `integracao-dyna` (continuar nesta branch)

---

## Contexto e Base Existente

O que já existe e será reutilizado:

| Componente | Arquivo | Como será usado |
|---|---|---|
| NodePool Registry (cluster, nodepool, vm_size, node_count) | `internal/storage/nodepool_registry_store.go` | Fonte primária de nós e VM SKUs |
| Cotação USD/BRL dinâmica | `internal/monitoring/predictions/cost_analyzer.go` | Reutilizar `fetchExchangeRate()` e constante `DefaultExchangeRate = 5.50` |
| Modelos de custo de node pool (structs) | `internal/monitoring/nodepoolpredictions/models.go` | Referência para novos tipos; NÃO reutilizar diretamente |
| Cliente Prometheus | `internal/monitoring/client/prometheus_client.go` | Fase 6 — P95 de uso real |
| Auto-discovery Prometheus | `internal/monitoring/discovery/prometheus.go` | Fase 6 |
| K8s client wrapper | `internal/kubernetes/client.go` | Listar pods, HPAs, requests/limits |
| Padrão de handler (Gin + DI) | `internal/web/handlers/*.go` | Seguir exatamente |
| ToolsMenu | `internal/web/frontend/src/components/ToolsMenu.tsx` | Registrar novo item |

---

## Arquitetura

### Fluxo de dados

```
Azure Pricing API (prices.azure.com) — público, sem auth
        │  vm_size → USD/hora  (cache SQLite 24h)
        ▼
NodePool Registry (SQLite já populado via "Escanear Clusters")
  cluster / nodepool / vm_size / node_count
        │
        ├──────────────────────────────────────────┐
        ▼                                          ▼
  Custo do cluster                         K8s API
  Σ(pool × nodes × preço/h × 730h)         pods Running + requests/limits
        │                                  HPAs (min/max/current replicas)
        └──────────────┬───────────────────────────┘
                       ▼
           Alocação de custo por workload
           proporcional a CPU+RAM requests
                       │
           [Fase 6] Prometheus P95 30d
           desperdício = request - P95×1.2
                       │
           Cotação USD/BRL (já existe)
                       ▼
              FinOpsReport (BRL + USD)
```

### Novo pacote: `internal/finops/`

```
internal/finops/
├── models.go          — tipos: FinOpsReport, FinOpsPool, FinOpsWorkload, FinOpsSummary
├── azure_pricing.go   — cliente Azure Pricing API + cache SQLite
├── exchange_rate.go   — wrapper para cotação USD/BRL (reutiliza lógica existente)
└── calculator.go      — cálculo de custo cluster → workload → HPA scenarios
```

### Handler e rotas

**Arquivo:** `internal/web/handlers/finops.go`

```
GET  /api/v1/finops/report?cluster=X[&namespaces=a,b][&with_prometheus=true]
GET  /api/v1/finops/pricing?sku=Standard_D4s_v3
POST /api/v1/finops/pricing/refresh
GET  /api/v1/finops/exchange-rate
```

### Frontend: `FinOpsTab.tsx`

Acessível via `ToolsMenu.tsx` (novo item no dropdown).

4 abas internas:

| Aba | Conteúdo |
|---|---|
| **Visão Geral** | Custo total/mês por cluster (bar chart), top-5 namespaces por custo, taxa de câmbio atual |
| **Node Pools** | Tabela: VM SKU · preço/hora · nodes · custo/mês em R$ |
| **Workloads** | Tabela ordenável: alocação proporcional, cenários HPA min↔max, verdict badge |
| **Oportunidades** | Workloads `superprovisioned` por saving potencial + botão "Analisar com AI" |

---

## Modelos de Dados

```go
// internal/finops/models.go

type FinOpsReport struct {
    Cluster      string
    GeneratedAt  time.Time
    ExchangeRate float64        // USD → BRL no momento da análise
    NodePools    []FinOpsPool
    Namespaces   []FinOpsNamespace
    Workloads    []FinOpsWorkload
    Summary      FinOpsSummary
}

type FinOpsPool struct {
    Name           string
    VMSize         string
    VMPriceUSDHour float64   // da Azure Pricing API
    NodeCount      int
    Mode           string    // System | User
    MonthlyCostUSD float64   // VMPriceUSDHour × NodeCount × 730
    MonthlyCostBRL float64
}

type FinOpsNamespace struct {
    Namespace      string
    MonthlyCostBRL float64
    MonthlyCostUSD float64
    WorkloadCount  int
}

type FinOpsWorkload struct {
    Namespace        string
    Workload         string
    Pods             int
    CPURequestMillis float64
    MemRequestMi     float64
    CostShareUSD     float64   // alocação proporcional ao cluster
    CostShareBRL     float64
    HPAMin           int
    HPAMax           int
    HPACurrent       int
    HPACostMinBRL    float64   // custo se ficasse sempre no mínimo
    HPACostMaxBRL    float64   // custo se ficasse sempre no máximo
    HPACostCurrentBRL float64
    // Campos opcionais (Fase 6 — Prometheus)
    CPUP95Millis     float64
    MemP95Mi         float64
    WasteBRL         float64
    Verdict          string    // "superprovisioned" | "ok" | "oom_risk" | "no_request"
}

type FinOpsSummary struct {
    TotalMonthlyCostBRL  float64
    TotalMonthlyCostUSD  float64
    TopNamespace         string
    PotentialSavingsBRL  float64   // soma dos WasteBRL (Fase 6)
    HPASavingsIfMinBRL   float64   // economia se todos HPAs no mínimo
    WorkloadsAnalyzed    int
    SuperprovisionedCount int
    OOMRiskCount         int
}
```

### Fórmulas de cálculo

```
// Custo mensal do cluster
ClusterMonthlyCostUSD = Σ_pools (node_count × vm_price_usd_hour × 730)

// Capacidade total do cluster (soma de todos os nodes)
ClusterTotalCPU = Σ_pools (node_count × vm_cpu_cores × 1000)   // millicores
ClusterTotalMem = Σ_pools (node_count × vm_mem_gb × 1024)      // MiB

// Alocação proporcional por workload (50% CPU + 50% MEM)
CPUShareFraction  = workload_cpu_req / ClusterTotalCPU
MemShareFraction  = workload_mem_req / ClusterTotalMem
WorkloadCostShare = ((CPUShareFraction + MemShareFraction) / 2) × ClusterMonthlyCostUSD

// Cenários HPA
PodCostUSD    = WorkloadCostShare / HPACurrent (ou 1 se sem HPA)
HPACostMinUSD = PodCostUSD × HPAMin
HPACostMaxUSD = PodCostUSD × HPAMax

// Desperdício (Fase 6 — requer Prometheus)
SuggestedReqCPU = P95_CPU × 1.20   // margem 20%
SuggestedReqMem = P95_Mem × 1.20
WasteUSD = (ActualReq - SuggestedReq) × PodCostPerUnit × Pods

// Veredicto (sem Prometheus — baseado em HPA e requests zerados)
"no_request"      → CPURequestMillis == 0
"superprovisioned" → HPA sempre no mínimo E HPACostMinBRL < HPACostCurrentBRL × 0.7
"ok"              → caso padrão
```

---

## Azure Pricing API

**Endpoint público (sem auth):**
```
GET https://prices.azure.com/api/retail/prices?
  api-version=2023-01-01-preview
  &$filter=serviceName eq 'Virtual Machines'
    and armRegionName eq 'brazilsouth'
    and priceType eq 'Consumption'
    and contains(skuName, 'Spot') eq false
```

**Mapeamento de vm_size para skuName:**
```
"Standard_D4s_v3" → remover "Standard_" → "D4s_v3" → substituir "_" → "D4s v3"
```

**Cache SQLite** — tabela `finops_pricing_cache` em `~/.k8s-hpa-manager/finops_pricing_cache.db`:
```sql
CREATE TABLE finops_pricing_cache (
    sku         TEXT PRIMARY KEY,
    region      TEXT NOT NULL,
    price_usd   REAL NOT NULL,
    fetched_at  DATETIME NOT NULL
);
```
TTL: 24 horas. Fallback: tabela local com SKUs mais comuns (ver seção abaixo).

**Fallback de preços (SKUs comuns Brasil Sul — pay-as-you-go):**
```go
var fallbackPrices = map[string]float64{
    "Standard_D2s_v3":  0.096,
    "Standard_D4s_v3":  0.192,
    "Standard_D8s_v3":  0.384,
    "Standard_D16s_v3": 0.768,
    "Standard_D2s_v4":  0.096,
    "Standard_D4s_v4":  0.192,
    "Standard_D8s_v4":  0.384,
    "Standard_E4s_v3":  0.252,
    "Standard_E8s_v3":  0.504,
    "Standard_F4s_v2":  0.169,
    "Standard_F8s_v2":  0.338,
    "Standard_B2s":     0.050,
    "Standard_B4ms":    0.166,
}
```

**CPU e RAM por SKU** — necessário para calcular capacidade do cluster:
```go
// VM specs lookup (vCPU, RAM GB)
var vmSpecs = map[string][2]int{
    "Standard_D2s_v3":  {2, 8},
    "Standard_D4s_v3":  {4, 16},
    "Standard_D8s_v3":  {8, 32},
    "Standard_D16s_v3": {16, 64},
    "Standard_D2s_v4":  {2, 8},
    "Standard_D4s_v4":  {4, 16},
    "Standard_D8s_v4":  {8, 32},
    "Standard_E4s_v3":  {4, 32},
    "Standard_E8s_v3":  {8, 64},
    "Standard_F4s_v2":  {4, 8},
    "Standard_F8s_v2":  {8, 16},
    "Standard_B2s":     {2, 4},
    "Standard_B4ms":    {4, 16},
    // Adicionar conforme necessário via Azure Pricing API response
}
```

---

## Checklist de Implementação

### Fase 1 — Azure Pricing API + Cache SQLite ✅ CONCLUÍDA
- [x] Criar `internal/finops/models.go` com todos os tipos
- [x] Criar `internal/finops/azure_pricing.go`
  - [x] Função `GetPrice(vmSize, region string) (price, source, error)`
  - [x] Mapeamento `vmSizeToSKUName()` (Standard_D4s_v3 → D4s v3)
  - [x] Cache SQLite (tabela `finops_pricing_cache`, TTL 24h) em `~/.k8s-hpa-manager/finops_pricing_cache.db`
  - [x] Fallback com `fallbackPrices` map (26 SKUs)
  - [x] Função `GetVMSpecs(vmSize string) (cpuCores, memGB int)` + `inferSpecsFromName()`
- [x] Criar `internal/finops/exchange_rate.go` (cotação USD/BRL dinâmica, cache 1h, fallback 5.50)
- [x] Testes passando: `go test -v ./internal/finops/...`
  - 5x Standard_D4s_v3 = $700 USD = R$ 3.667/mês (cotação real: 5.2334)

### Fase 2 — Exchange Rate + Calculator ✅ CONCLUÍDA
- [x] `internal/finops/exchange_rate.go` criado na Fase 1
- [x] Criar `internal/finops/calculator.go`
  - [x] `Calculator.BuildReport(ctx, cluster, client, pools, namespaces)` → FinOpsReport completo
  - [x] `calculatePoolCosts()` — custo e capacidade por pool (via AzurePricer)
  - [x] `collectWorkloads()` — lista pods Running + HPAs, resolve pod→RS→Deployment
  - [x] `allocateCosts()` — alocação proporcional (50% CPU + 50% RAM), cenários HPA min/max/current
  - [x] `determineVerdict()` — no_request | superprovisioned | ok (Fase 6 enriquece com Prometheus)
  - [x] `aggregateNamespaces()` + `buildSummary()`
- [x] Testes passando (9/9): stripHash, filtros namespace, pool costs, alocação, agregação
  - Resultado real: D4s_v3=$0.318/h (API), D8s_v3=$0.636/h → cluster 5 nodes = R$9.722/mês

### Fase 3 — Handler Go ✅ CONCLUÍDA
- [x] Criar `internal/web/handlers/finops.go`
  - [x] `FinOpsHandler` struct com `kubeManager`, `npRegistryStore`, `pricer`, `exchange`
  - [x] `GetReport` — cluster obrigatório, namespaces opcionais (CSV)
  - [x] `GetPricing` — preço SKU + specs (vCPU, RAM)
  - [x] `RefreshPricing` — invalida cache SQLite
  - [x] `GetExchangeRate` — cotação atual com flag fallback
- [x] Registrar rotas em `internal/web/server.go`
- [x] `make build` + testes com cluster real:
  - `GET /api/v1/finops/exchange-rate` → `{"usd_brl":5.2368,"date":"2026-03-25"}`
  - `GET /api/v1/finops/pricing?sku=Standard_D4s_v3` → `{"price_usd_hour":0.318,"source":"api"}`
  - `GET /api/v1/finops/report?cluster=akspriv-abastecimento-hlg-admin` → relatório completo com node pools, namespaces e workloads
- **Nota**: o registry guarda cluster com sufixo `-admin` — frontend deve usar nome com sufixo

### Fase 4 — Frontend: Visão Geral + Node Pools ✅ CONCLUÍDA
- [x] Criar `internal/web/frontend/src/components/FinOpsTab.tsx`
  - [x] Seletor de cluster (lista clusters com sufixo `-admin`, exibe sem sufixo)
  - [x] Botão "Analisar" com loading state + auto-fetch por cluster
  - [x] Aba "Visão Geral": 4 summary cards + bar charts (top namespaces + custo por pool)
  - [x] Aba "Node Pools": tabela completa (VM SKU · vCPU · RAM · nodes · modo · USD/h · R$/mês · fonte)
  - [x] Aba "Workloads": tabela ordenável com filtro por veredicto + colunas HPA min/cur/max
  - [x] Aba "Oportunidades": lista de workloads superprovisioned com saving estimado/mês e /ano
  - [x] Badges de veredicto coloridos (vermelho/amarelo/verde/cinza)
  - [x] Alert de economia potencial quando há workloads superprovisioned
- [x] Registrar `FinOpsTab` no `ToolsMenu.tsx` (item "FinOps" com ícone CircleDollarSign)
- [x] Registrar case `"finops"` em `Index.tsx` + exclusão dos stats cards
- [x] `./rebuild-web.sh -b` sem erros

### Fase 5 — Frontend: Workloads + Oportunidades + AI ✅ CONCLUÍDA
- [x] Aba "Workloads": tabela ordenável (já na Fase 4) com filtro por veredicto
- [x] Aba "Oportunidades": lista cards superprovisioned com saving/mês e /ano
- [x] Botão "Exportar CSV" → download `finops-<cluster>-<data>.csv` com todos os workloads
- [x] Botão "Analisar com AI":
  - Frontend: POST `/api/v1/finops/analyze` com `{ai_email, report}`
  - Backend: `buildFinOpsPrompt()` envia summary + node pools + top-10 workloads + superprovisionados
  - Resultado exibido em painel colapsável com ícone Brain acima das abas
- [x] `./rebuild-web.sh -b` sem erros

### Fase 6 — Integração Prometheus (desperdício real)
- [ ] Adicionar parâmetro `with_prometheus=true` na rota `GetReport`
- [ ] Queries PromQL (janela configurável, padrão 30d):
  - [ ] `quantile_over_time(0.95, container_memory_working_set_bytes{...}[Xd])`
  - [ ] `avg_over_time(container_memory_working_set_bytes{...}[Xd])`
  - [ ] `quantile_over_time(0.95, rate(container_cpu_usage_seconds_total{...}[5m])[Xd:5m])`
  - [ ] `avg_over_time(rate(container_cpu_usage_seconds_total{...}[5m])[Xd:5m])`
- [ ] Preencher `CPUP95Millis`, `MemP95Mi`, `WasteBRL` em `FinOpsWorkload`
- [ ] Recalcular verdicts com dados reais (`oom_risk` quando P95 > 95% do request)
- [ ] Adicionar toggle "Incluir dados Prometheus (lento)" no frontend
- [ ] Adicionar coluna "Desperdício R$/mês" na tabela de Workloads
- [ ] Atualizar aba "Oportunidades" com valores reais de waste

### Finalização
- [ ] Atualizar `CLAUDE.md` com seção FinOps
- [ ] Commit final
- [ ] Deletar `FINOPS_PLAN.md` (ou mover para `docs/planning/`)

---

## Decisões de Design

| Decisão | Escolha | Motivo |
|---|---|---|
| Região Azure padrão | `brazilsouth` | Todos os clusters são Brasil |
| Tipo de preço | `Consumption` (pay-as-you-go) | Conservador; reservas dependem de contrato |
| Cache de preços | SQLite, TTL 24h | API pública mas lenta (~2s por SKU) |
| Capacidade do cluster | vm_specs local (vCPU + RAM) | Azure Pricing API não retorna specs de VM |
| Prometheus | Opcional (fase 6) | Queries de 30d são lentas; custo base não depende de Prometheus |
| Alocação de custo | 50% CPU + 50% RAM proporcional | Simples, justo, sem precisar de billing real por pod |
| Fallback de cotação | R$ 5.50 | Já usado no projeto |

---

## Referências

- Azure Pricing API docs: `https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices`
- Cotação USD/BRL: `https://economia.awesomeapi.com.br/json/last/USD-BRL`
- Script Python de referência: `/home/paulo/Scripts/Analisa_Consumo_AKS_v1.py`
- NodePool Registry: `internal/storage/nodepool_registry_store.go`
- Cost Analyzer existente: `internal/monitoring/predictions/cost_analyzer.go`
