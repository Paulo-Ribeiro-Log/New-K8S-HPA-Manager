# Checklist: Análise Preditiva de Node Pool

**Estudo base:** [ESTUDO_PREDICAO_NODE_POOL.md](ESTUDO_PREDICAO_NODE_POOL.md)
**Iniciado:** 21/02/2026
**Status geral:** 🟡 Em andamento — Fase 1 concluída

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

## Fase 2 — Backend: Collector
**Arquivo alvo:** `internal/monitoring/nodepoolpredictions/collector.go` (novo)

- [ ] 2.1 Implementar `NewNodePoolCollector()` (recebe K8s client + Prometheus client + Azure client)
- [ ] 2.2 Resolver nodes do pool via K8s API:
  - [ ] Label selector: `kubernetes.azure.com/agentpool=<name>` ou `agentpool=<name>`
  - [ ] Coletar IPs dos nodes (para filtro Prometheus)
  - [ ] Coletar nomes dos nodes (para filtro kube_* metrics)
  - [ ] Detectar se node está cordoned (unschedulable)
- [ ] 2.3 Coletar dados via Azure AKS API:
  - [ ] min/max/current node count
  - [ ] VM SKU do pool
  - [ ] Autoscaler enabled + config
  - [ ] (Opcional) Histórico de scaling events via Azure Activity Log
- [ ] 2.4 Coletar snapshot atual de cada node:
  - [ ] CPU usage % (via Prometheus ou Metrics Server)
  - [ ] Memory usage %
  - [ ] Pod count + pod capacity (max-pods-per-node)
  - [ ] conntrack entries + limit + %
  - [ ] Disk usage %
  - [ ] PID count
  - [ ] Conditions ativas (MemoryPressure, DiskPressure, PIDPressure)
- [ ] 2.5 Coletar histórico (D-3, D-7, D-14) via Prometheus com offset:
  - [ ] CPU médio por node (normalizado por node count do período)
  - [ ] Memória média por node
  - [ ] Pod density média por node
  - [ ] **Detectar mudança de node count entre períodos → usar média, não soma**
- [ ] 2.6 Coletar conntrack cluster-wide (contexto secundário)
- [ ] 2.7 Coletar eventos do Cluster Autoscaler via K8s Events API
- [ ] 2.8 Calcular BinPacking analysis (fragmentação do pool)
- [ ] 2.9 Graceful degradation quando Prometheus indisponível (fallback para Metrics Server + API K8s apenas)

---

## Fase 3 — Backend: Analyzer (IA)
**Arquivo alvo:** `internal/monitoring/nodepoolpredictions/analyzer.go` (novo)

- [ ] 3.1 Implementar `NodePoolAnalyzer` (reutiliza providers Ollama/Claude do pacote `ai/`)
- [ ] 3.2 Calcular Health Score:
  - [ ] Node Availability (25%): nodes ready vs total
  - [ ] Resource Headroom (30%): CPU/mem disponível até próximo scale
  - [ ] Pod Density (20%): % capacidade de pods usada no node mais denso
  - [ ] conntrack Safety (15%): % conntrack no node mais saturado
  - [ ] Autoscaler Health (10%): frequência + sucesso de scale events
- [ ] 3.3 Calcular ActionSummary (Resumo de Ação — mesmo padrão do deployment)
- [ ] 3.4 Calcular tendências do pool:
  - [ ] CPU trend (D-3, D-7, D-14 normalizados por node)
  - [ ] Memory trend
  - [ ] Pod density trend
  - [ ] conntrack trend (crítico — crescimento mais rápido que outros recursos?)
- [ ] 3.5 Montar prompt específico para IA de node pool:
  - [ ] Contexto: pool name, VM SKU, current/min/max nodes, autoscaler status
  - [ ] Métricas por node (top 3 mais saturados)
  - [ ] conntrack: node mais saturado e tendência
  - [ ] Autoscaler events recentes
  - [ ] Perguntas-chave para IA:
    - "Quando o pool vai saturar dado crescimento atual?"
    - "conntrack vai esgotar antes da memória/CPU?"
    - "O autoscaler consegue reagir antes da saturação?"
    - "Há fragmentação que permite scale-in seguro?"
    - "O VM SKU está adequado para o perfil de workload?"
- [ ] 3.6 Enriquecer previsões com timestamps (mesma função do deployment analyzer)
- [ ] 3.7 Implementar `enrichPredictionsWithTimestamps()` adaptado

---

## Fase 4 — Backend: Cost Analyzer
**Arquivo alvo:** `internal/monitoring/nodepoolpredictions/cost_analyzer.go` (novo)

- [ ] 4.1 Reutilizar `azure_vm_specs.go` para obter preço do VM SKU
- [ ] 4.2 Calcular custo atual: `currentNodes * 730h * costPerHour`
- [ ] 4.3 Calcular custo máximo: `maxNodes * 730h * costPerHour`
- [ ] 4.4 Calcular idle waste: se nodes com < 30% uso de CPU/mem → desperdício
- [ ] 4.5 Calcular recommended nodes baseado em P95 de uso real
- [ ] 4.6 Calcular projected savings (economia mensal com right-sizing)
- [ ] 4.7 Converter USD → BRL via API de câmbio (mesma do deployment — cache 1h)
- [ ] 4.8 Gerar CostRecommendations (ex: "reduzir max nodes", "mudar VM SKU")

---

## Fase 5 — Backend: API REST + Storage
**Arquivos:** `internal/web/handlers/nodepool_predictions.go` + `internal/storage/nodepool_predictions_store.go`

- [ ] 5.1 Criar `nodepool_predictions_store.go`:
  - [ ] Tabela SQLite `nodepool_predictions` (espelho de `predictions_store.go`)
  - [ ] `Save()`, `GetByID()`, `List()` com filtros (cluster, nodepool, data)
  - [ ] DB separado: `./build/nodepool_predictions.db`
- [ ] 5.2 Criar handler `nodepool_predictions.go`:
  - [ ] `POST /api/v1/nodepoolpredictions/analyze`
  - [ ] `GET  /api/v1/nodepoolpredictions/history`
  - [ ] `GET  /api/v1/nodepoolpredictions/report/:id/markdown`
- [ ] 5.3 Registrar rotas em `internal/web/server.go`
- [ ] 5.4 Adicionar ao grupo RBAC (somente leitura para todos, histórico para todos)

---

## Fase 6 — Relatórios (Markdown + PDF)
**Arquivo alvo:** `internal/web/handlers/nodepool_predictions.go` + `src/lib/reportGenerator.ts`

- [ ] 6.1 Backend: Gerar relatório Markdown com seções:
  - [ ] Sumário Executivo
  - [ ] Health Score (breakdown)
  - [ ] Estado atual dos nodes (tabela: node, CPU%, mem%, pods%, conntrack%, disk%)
  - [ ] Análise conntrack (por node + cluster-wide)
  - [ ] Tendências (D-3, D-7, D-14)
  - [ ] Histórico Autoscaler
  - [ ] Análise de Custo
  - [ ] Recomendações
- [ ] 6.2 Frontend: Suporte a PDF via `reportGenerator.ts` (adaptar o existente)

---

## Fase 7 — Frontend
**Arquivos:** `NodePoolEditor.tsx` + novos componentes

- [ ] 7.1 Criar `src/hooks/useNodePoolPredictions.ts`:
  - [ ] `useAnalyzeNodePool()` — mutation para iniciar análise
  - [ ] `useNodePoolPredictionHistory()` — query com filtros
- [ ] 7.2 Criar `src/components/NodePoolPredictionModal.tsx`:
  - [ ] Seguir estrutura do modal de deployment (DeploymentsTab)
  - [ ] Seção: ActionSummary (topo)
  - [ ] Seção: Health Score com breakdown visual
  - [ ] **Seção conntrack** (nova — destaque especial):
    - [ ] Tabela: node, entries, limit, %, status badge
    - [ ] Barra de progresso colorida por node
    - [ ] Cluster-wide aggregate abaixo
  - [ ] Seção: Estado atual dos nodes (tabela visual)
  - [ ] Seção: Tendências (gráfico ou tabela D-0/D-3/D-7/D-14)
  - [ ] Seção: Autoscaler (eventos + previsão)
  - [ ] Seção: Análise de Custo
  - [ ] Seção: Previsões IA (short/medium/long term)
  - [ ] Seção: Recomendações
  - [ ] Botões: Exportar MD / PDF, Fechar
- [ ] 7.3 Criar `src/components/NodePoolPredictionHistoryModal.tsx`
- [ ] 7.4 Integrar botão em `NodePoolEditor.tsx`:
  - [ ] Tab "Configuration" → header → botão "Análise Preditiva"
  - [ ] Gradiente visual igual ao deployment (azul→roxo)
  - [ ] Loading state durante análise
  - [ ] Abrir `NodePoolPredictionModal` com resultado
- [ ] 7.5 Adicionar métodos em `src/lib/api/client.ts`:
  - [ ] `analyzeNodePool(cluster, nodepool)`
  - [ ] `getNodePoolPredictionHistory(filters)`
  - [ ] `getNodePoolPredictionReport(id, format)`

---

## Fase 8 — Testes e Validação
- [ ] 8.1 Testar coleta sem Prometheus (Metrics Server only)
- [ ] 8.2 Testar coleta com pool de 1 node
- [ ] 8.3 Testar pool com node cordoned
- [ ] 8.4 Verificar conntrack quando node_exporter não disponível (graceful degradation)
- [ ] 8.5 Testar normalização de tendência quando pool mudou de tamanho
- [ ] 8.6 `go test -v ./internal/monitoring/nodepoolpredictions/... -race`
- [ ] 8.7 Build completo: `make build && ./rebuild-web.sh -b`

---

## Fase 9 — Documentação e CLAUDE.md
- [ ] 9.1 Atualizar `CLAUDE.md` com feature description
- [ ] 9.2 Atualizar `docs/guides/QUICK_START.md`
- [ ] 9.3 Marcar versão como `v1.3.20+` nas notas de release

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

---

*Atualizar esta tabela a cada sessão de desenvolvimento.*
