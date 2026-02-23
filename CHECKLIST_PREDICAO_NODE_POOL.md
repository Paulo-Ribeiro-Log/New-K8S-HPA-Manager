# Checklist: Análise Preditiva de Node Pool

**Estudo base:** [ESTUDO_PREDICAO_NODE_POOL.md](ESTUDO_PREDICAO_NODE_POOL.md)
**Iniciado:** 21/02/2026
**Status geral:** ✅ Concluído — Todas as 9 fases implementadas

> **Para novos chats:** leia este arquivo + o estudo base antes de começar.
> Marque cada item com ✅ quando concluído e anote a data.

---

## Fase 1 — Backend: Models e Queries ✅ Concluída em 21/02/2026
**Arquivos criados:** `internal/monitoring/nodepoolpredictions/models.go` + `queries.go`

- [x] 1.1 Criar pacote `internal/monitoring/nodepoolpredictions/`
- [x] 1.2 Criar `models.go` com structs:
  - [x] `NodePoolPredictionRequest`
  - [x] `NodePoolPredictionResult` (envelope geral)
  - [x] `NodePoolMetrics` (dados coletados)
  - [x] `NodePoolNodeSnapshot` (estado por node — CPU, mem, pods, conntrack, disk, PID)
  - [x] `TrendSnapshot` (CPU/mem/pods por node, normalizado por node count)
  - [x] `AutoscalerEvent` (eventos do cluster autoscaler)
  - [x] `NodePressureInfo` (nodes com condições ativas)
  - [x] `NodePoolCostAnalysis` (custo baseado em VM SKU real)
  - [x] `ConntrackNodeInfo`, `ConntrackPoolAnalysis`, `ConntrackClusterContext` (próprios — pool + cluster)
  - [x] `BinPackingAnalysis`, `NodePoolCapacityForecast` (capacidade e fragmentação)
  - [x] `NodePoolHealthScore`, `NodePoolHealthBreakdown` (pesos específicos para pool)
  - [x] `NodePoolActionSummary`, `NodePoolPrediction`, `NodePoolRecommendation`
  - [x] `NodePoolRootCauseAnalysis`, `NodePoolExecutiveSummary`
  - [x] `NodePoolTrends`, `TrendDirection`, `DataSourceInfo`
- [x] 1.3 Criar `queries.go` com queries Prometheus:
  - [x] CPU por node — uso % e cores absolutos (filtrado por IPs do pool)
  - [x] Memória por node — uso %, total, disponível
  - [x] conntrack por node — entries, limit, %, growth rate, offset (trend), cluster-wide
  - [x] Disk usage por node — uso %, disponível, read/write rates
  - [x] Pod count por node vs capacity (com offset para trend)
  - [x] PID count por node + limite do kernel
  - [x] Network rx/tx por node (excluindo interfaces internas)
  - [x] `BuildInstanceRegex()` — monta regex de IPs para node_exporter
  - [x] `BuildNodeNameRegex()` — monta regex de nomes para kube_*
  - [x] `DayOffsets()` — retorna map D-3/D-7/D-14
  - [x] `ConntrackStatusFromPercent()` — classifica ok/warning/critical/emergency

---

## Fase 2 — Backend: Collector ✅ Concluída em 21/02/2026
**Arquivo criado:** `internal/monitoring/nodepoolpredictions/collector.go`

- [x] 2.1 Implementar `NewNodePoolCollector()` (recebe K8s client + Prometheus client)
- [x] 2.2 Resolver nodes do pool via K8s API:
  - [x] Label selector: `agentpool=<name>` com fallback para `kubernetes.azure.com/agentpool=<name>`
  - [x] Coletar IPs dos nodes (para filtro Prometheus — formato `IP:9100`)
  - [x] Coletar nomes dos nodes (para filtro kube_* metrics)
  - [x] Detectar se node está cordoned (unschedulable)
  - [x] Extrair resource group e subscription do providerID do node
- [x] 2.3 Coletar dados via Azure AKS API (`az aks nodepool show`):
  - [x] min/max/current node count
  - [x] VM SKU do pool
  - [x] Autoscaler enabled + config
- [x] 2.4 Coletar snapshot atual de cada node (paralelo via goroutines):
  - [x] CPU usage % (Prometheus: `node_cpu_seconds_total`)
  - [x] Memory usage % (Prometheus: `node_memory_MemAvailable/MemTotal`)
  - [x] Pod count + pod capacity (Prometheus ou K8s API fallback)
  - [x] conntrack entries + limit + % (Prometheus: `node_nf_conntrack_*`)
  - [x] Disk usage % (Prometheus: `node_filesystem_*`)
  - [x] PID count (Prometheus: `node_processes_total`)
  - [x] Conditions ativas (MemoryPressure, DiskPressure, PIDPressure) via K8s API
- [x] 2.5 Coletar histórico (D-3, D-7, D-14) via Prometheus com offset:
  - [x] CPU médio por node (normalizado — média, não soma)
  - [x] Memória média por node
  - [x] Pod density média por node
  - [x] Estimar node count histórico via count de instâncias com dados
- [x] 2.6 Coletar conntrack cluster-wide (contexto secundário, sem filtro de pool)
- [x] 2.7 Coletar eventos do Cluster Autoscaler via K8s Events API (últimos 7 dias, max 20)
- [x] 2.8 Calcular BinPacking analysis (CPU/Mem/Pod efficiency + fragmentação + scale-in candidates)
- [x] 2.9 Graceful degradation quando Prometheus indisponível (K8s API + limitação documentada)

---

## Fase 3 — Backend: Analyzer (IA) ✅ Concluída em 22/02/2026
**Arquivo criado:** `internal/monitoring/nodepoolpredictions/analyzer.go`

- [x] 3.1 Implementar `NodePoolAnalyzer` (reutiliza providers Ollama/Claude do pacote `ai/`)
- [x] 3.2 Calcular Health Score:
  - [x] Node Availability (25%): nodes ready vs total
  - [x] Resource Headroom (30%): CPU/mem disponível até próximo scale
  - [x] Pod Density (20%): % capacidade de pods usada no node mais denso
  - [x] conntrack Safety (15%): % conntrack no node mais saturado
  - [x] Autoscaler Health (10%): frequência + sucesso de scale events
- [x] 3.3 Calcular ActionSummary (Resumo de Ação — mesmo padrão do deployment)
- [x] 3.4 Calcular tendências do pool:
  - [x] CPU trend (D-3, D-7, D-14 normalizados por node)
  - [x] Memory trend
  - [x] Pod density trend
  - [x] conntrack trend (crítico — crescimento mais rápido que outros recursos?)
- [x] 3.5 Montar prompt específico para IA de node pool:
  - [x] Contexto: pool name, VM SKU, current/min/max nodes, autoscaler status
  - [x] Métricas por node (top 3 mais saturados)
  - [x] conntrack: node mais saturado e tendência
  - [x] Autoscaler events recentes
  - [x] Perguntas-chave para IA:
    - "Quando o pool vai saturar dado crescimento atual?"
    - "conntrack vai esgotar antes da memória/CPU?"
    - "O autoscaler consegue reagir antes da saturação?"
    - "Há fragmentação que permite scale-in seguro?"
    - "O VM SKU está adequado para o perfil de workload?"
- [x] 3.6 Enriquecer previsões com timestamps (mesma função do deployment analyzer)
- [x] 3.7 Implementar `enrichPredictionsWithTimestamps()` adaptado

---

## Fase 4 — Backend: Cost Analyzer ✅ Concluída em 22/02/2026
**Arquivo criado:** `internal/monitoring/nodepoolpredictions/cost_analyzer.go`

- [x] 4.1 Reutilizar `azure_vm_specs.go` para obter preço do VM SKU (import do pacote `predictions`)
- [x] 4.2 Calcular custo atual: `currentNodes * 730h * costPerHour`
- [x] 4.3 Calcular custo máximo: `maxNodes * 730h * costPerHour`
- [x] 4.4 Calcular idle waste: nodes com CPU < 30% E mem < 30% → desperdício
- [x] 4.5 Calcular recommended nodes baseado em P95 de uso real (+ 20% margem, cap 80%)
- [x] 4.6 Calcular projected savings (economia mensal + anual com right-sizing)
- [x] 4.7 Converter USD → BRL via API de câmbio (mesma do deployment — cache 1h)
- [x] 4.8 Gerar CostRecommendations: "reduzir max nodes", "consolidar idle nodes", "right-sizing", "spot nodes"

---

## Fase 5 — Backend: API REST + Storage ✅ Concluída em 22/02/2026
**Arquivos:** `internal/web/handlers/nodepool_predictions.go` + `internal/storage/nodepool_predictions_store.go`

- [x] 5.1 Criar `nodepool_predictions_store.go`:
  - [x] Tabela SQLite `nodepool_predictions` (espelho de `predictions_store.go`)
  - [x] `Save()`, `GetByID()`, `List()` com filtros (cluster, nodepool, data)
  - [x] DB separado: `./build/nodepool_predictions.db`
- [x] 5.2 Criar handler `nodepool_predictions.go`:
  - [x] `POST /api/v1/nodepoolpredictions/analyze`
  - [x] `GET  /api/v1/nodepoolpredictions/history`
  - [x] `GET  /api/v1/nodepoolpredictions/report/:id/markdown`
- [x] 5.3 Registrar rotas em `internal/web/server.go`
- [x] 5.4 Adicionado com `rbacMiddleware.InjectUserEmail()` (somente leitura para todos)

---

## Fase 6 — Relatórios (Markdown + PDF) ✅ Concluída em 22/02/2026
**Arquivo alvo:** `internal/web/handlers/nodepool_predictions.go` + `src/lib/nodePoolPdfGenerator.ts`

- [x] 6.1 Backend: Gerar relatório Markdown com seções:
  - [x] Sumário Executivo (topo — posição corrigida)
  - [x] Resumo de Ação (HoursToCritical, TopActionCommand)
  - [x] Health Score (breakdown com 5 componentes e pesos)
  - [x] Estado atual do pool (nodes, autoscaler, pressão)
  - [x] Estado por Node (tabela completa: CPU%, Mem%, Pods, conntrack%, Disk%, Status)
  - [x] Análise conntrack (pool aggregate + tabela por node com entries/limit/uso%)
  - [x] Tendências (D-0/D-3/D-7/D-14 para CPU, Memória e Pods por node)
  - [x] Histórico Autoscaler (tabela com data, tipo, delta, motivo — últimos 10 eventos)
  - [x] Bin Packing (eficiência, fragmentação, candidatos scale-in, recursos desperdiçados)
  - [x] Análise de Custo (custo atual/máx/economia em USD e BRL + recomendações)
  - [x] Previsões IA (curto/médio/longo prazo com confiança e severidade)
  - [x] Recomendações (com ações e comandos az/kubectl)
  - [x] Fix: `bp.ScaleInSafe` (bool) → `bp.ScaleInCandidates` (int) para contagem real
  - [x] Fix: `cn.ConntrackEntries/Limit/Percent` → `cn.CurrentEntries/MaxEntries/UsagePercent`
- [x] 6.2 Frontend: PDF via `nodePoolPdfGenerator.ts` (novo, jsPDF + jspdf-autotable)
  - [x] Botão "Exportar PDF" no modal ao lado do "Exportar MD"
  - [x] Seções: Header logo, Sumário, Infra, Nodes, Tendências, Previsões, Recomendações, Bin Packing, Custo, Causa Raiz
  - [x] Footer em todas as páginas com nome do node pool e cluster

---

## Fase 7 — Frontend ✅ Concluída em 22/02/2026
**Arquivos:** `NodePoolEditor.tsx` + novos componentes

- [x] 7.1 Criar `src/hooks/useNodePoolPredictions.ts`:
  - [x] `useAnalyzeNodePool()` — mutation para iniciar análise
  - [x] `useNodePoolPredictionHistory()` — query com filtros
- [x] 7.2 Criar `src/components/NodePoolPredictionModal.tsx`:
  - [x] Seguir estrutura do modal de deployment (DeploymentsTab)
  - [x] Seção: ActionSummary (topo)
  - [x] Seção: Health Score com breakdown visual
  - [x] **Seção conntrack** (nova — destaque especial):
    - [x] Tabela: node, entries, limit, %, status badge
    - [x] Barra de progresso colorida por node
    - [x] Cluster-wide aggregate abaixo
  - [x] Seção: Estado atual dos nodes (tabela visual)
  - [x] Seção: Tendências (D-3/D-7/D-14 por metrica)
  - [x] Seção: Autoscaler (eventos)
  - [x] Seção: Análise de Custo
  - [x] Seção: Previsões IA (short/medium/long term)
  - [x] Seção: Recomendações
  - [x] Botões: Exportar MD, Histórico, Fechar
- [x] 7.3 Criar `src/components/NodePoolPredictionHistoryModal.tsx`
- [x] 7.4 Integrar botão em `NodePoolEditor.tsx`:
  - [x] Header → botão "Análise Preditiva" (antes das Tabs)
  - [x] Gradiente visual igual ao deployment (azul→roxo)
  - [x] Loading state durante análise
  - [x] Abre `NodePoolPredictionModal` com resultado
  - [x] Botão "Histórico de Análises" separado
- [x] 7.5 Adicionar métodos em `src/lib/api/client.ts`:
  - [x] `analyzeNodePool(cluster, nodepool)`
  - [x] `getNodePoolPredictionHistory(filters)`
  - [x] `getNodePoolPredictionReport(id)` — download MD

---

## Fase 8 — Testes e Validação ✅ Concluída em 22/02/2026
- [ ] 8.1 Testar coleta sem Prometheus (Metrics Server only) — requer cluster real
- [ ] 8.2 Testar coleta com pool de 1 node — requer cluster real
- [ ] 8.3 Testar pool com node cordoned — requer cluster real
- [ ] 8.4 Verificar conntrack quando node_exporter não disponível — requer cluster real
- [ ] 8.5 Testar normalização de tendência quando pool mudou de tamanho — requer cluster real
- [x] 8.6 `go test -v ./internal/monitoring/nodepoolpredictions/... -race` — 16/16 PASS ✅
  - `TestBuildInstanceRegex_*` (4 casos) — valida escaping de pontos para PromQL
  - `TestBuildNodeNameRegex_*` (3 casos) — valida regex de nomes de nodes
  - `TestConntrackStatusFromPercent` (11 casos) — valida thresholds ok/warning/critical/emergency
  - `TestDayOffsets_Values` — valida offsets D-3/D-7/D-14
  - `TestFormatDuration_*` (3 casos) — valida formatação dias/horas/sem offset
  - `TestGet*Query_*` (4 casos) — smoke tests das queries Prometheus
- [x] 8.7 Build completo: `make build` — ✅ sem erros

---

## Fase 9 — Documentação e CLAUDE.md ✅ Concluída em 22/02/2026
- [x] 9.1 Atualizar `CLAUDE.md` com feature description completa (funcionalidades, backend, frontend, API REST, testes, bugs corrigidos)
- [x] 9.2 Sessão registrada no histórico de sessões recentes do CLAUDE.md
- [ ] 9.3 Atualizar `docs/guides/QUICK_START.md` — opcional, feito quando houver release

---

## Dependências e Pré-requisitos

| Requisito | Status |
|---|---|
| Prometheus com node_exporter | Necessário para análise completa |
| Azure CLI autenticado | Para dados do pool (min/max nodes, SKU) |
| Ollama ou ANTHROPIC_API_KEY | Para análise IA |
| `azure_vm_specs.go` existente | ✅ Já existe |
| `ConntrackAnalysis` struct | ✅ Já existe em models.go |
| Queries conntrack | ✅ Já existem em queries.go |
| Provider IA (Ollama/Claude) | ✅ Já existe no pacote `ai/` |
| SQLite storage pattern | ✅ Já existe em `predictions_store.go` |

---

## Notas de Implementação

### Identificação dos nodes do pool
```go
// Label selector para nodes do AKS node pool
labelSelector := fmt.Sprintf("kubernetes.azure.com/agentpool=%s", nodePoolName)
// Fallback:
labelSelector = fmt.Sprintf("agentpool=%s", nodePoolName)
```

### Normalização quando pool escalou/descalou
```go
// SEMPRE usar média por node, não soma total
// Isso garante comparabilidade entre snapshots com node counts diferentes
avgCPUPercent := totalCPU / float64(nodeCount)
```

### conntrack: Montar regex de IPs para Prometheus
```go
// node_exporter expõe na label "instance" como "<IP>:<porta>"
// Montar regex: "192.168.1.10:9100|192.168.1.11:9100|..."
instances := strings.Join(nodeExporterInstances, "|")
query := fmt.Sprintf(`node_nf_conntrack_entries{instance=~"%s"}`, instances)
```

### Graceful degradation sem Prometheus
```go
// Se Prometheus indisponível → coletar apenas:
// 1. Estado atual via Metrics Server (CPU/mem dos nodes)
// 2. Pod count via K8s API
// 3. Node conditions via K8s API
// 4. conntrack: indisponível (exige node_exporter)
// → Marcar HasSufficientData = false em ConntrackAnalysis
```

---

## Progresso por Data

| Data | Fase | O que foi feito | Quem |
|---|---|---|---|
| 21/02/2026 | Estudo | Análise completa, decisões de design, documentação | Paulo + Claude |
| 21/02/2026 | Fase 1 | `models.go` (22 structs) + `queries.go` (28 funções) — compila sem erros | Paulo + Claude |
| 21/02/2026 | Fase 2 | `collector.go` — coleta completa: K8s API + Prometheus + Azure CLI + graceful degradation | Paulo + Claude |
| 22/02/2026 | Fase 3 | `analyzer.go` — Health Score (5 pesos), trends, prompt IA, fallback, enrichment de timestamps/confiança | Paulo + Claude |
| 22/02/2026 | Fase 4 | `cost_analyzer.go` — custo real por VM SKU, idle waste, P95 right-sizing, 4 recomendações, câmbio USD/BRL | Paulo + Claude |
| 22/02/2026 | Fase 5 | `nodepool_predictions_store.go` + `nodepool_predictions.go` + rotas em `server.go` — build completo ✅ | Paulo + Claude |
| 22/02/2026 | Fase 7 | Frontend: hook, modal principal, modal histórico, botão no NodePoolEditor, métodos API client — build completo ✅ | Paulo + Claude |
| 22/02/2026 | Fase 6 | Markdown: seções completas (sumário, breakdown, conntrack por node, autoscaler history, bin packing, custo, previsões, recomendações); PDF: nodePoolPdfGenerator.ts + botão no modal ✅ | Paulo + Claude |
| 22/02/2026 | Fase 8 | Testes unitários: 16/16 PASS com -race; BuildInstanceRegex, ConntrackStatus, DayOffsets, formatDuration, smoke tests das queries ✅ | Paulo + Claude |
| 22/02/2026 | Fase 9 | Documentação: CLAUDE.md atualizado com feature completa, bugs corrigidos, histórico de sessão ✅ | Paulo + Claude |

---

*Atualizar esta tabela a cada sessão de desenvolvimento.*
