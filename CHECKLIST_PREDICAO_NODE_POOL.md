# Checklist: Análise Preditiva de Node Pool

**Estudo base:** [ESTUDO_PREDICAO_NODE_POOL.md](ESTUDO_PREDICAO_NODE_POOL.md)
**Iniciado:** 21/02/2026
**Status geral:** ✅ Concluída — Fases 1-15 concluídas (24/02/2026)

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

---

## Fase 10 — Timeline de Saturação com Data Concreta 🚧 Em andamento
**Objetivo**: Transformar a análise de "descritiva" para "preditiva de verdade" — responder "quando exatamente?"
**Impacto**: ⭐⭐⭐⭐⭐ | **Esforço**: Médio

### Design
- Para cada métrica com tendência crescente, calcular: `diasAtéSaturação = (limiar - valorAtual) / taxaDeVariaçãoPorDia`
- Métricas analisadas: CPU, memória, conntrack (por node), pods/node, disco
- Resultado: `SaturationForecast` com data absoluta (ex: "03/03/2026 14:00") + dias restantes + confiança
- Limiares configuráveis: CPU 85%, Mem 85%, conntrack 85% (critical), Pods 90%, Disk 80%
- Considerar: variação de tendência (D-7 vs D-14) para calcular aceleração/desaceleração

### Tarefas Backend
- [ ] 10.1 Criar struct `SaturationForecast` em `models.go`:
  - `Metric` string — "cpu", "memory", "conntrack", "pods", "disk"
  - `CurrentValue` float64 — valor atual (%)
  - `DailyGrowthRate` float64 — taxa de crescimento por dia (p.p./dia)
  - `Threshold` float64 — limiar de saturação (ex: 85.0)
  - `DaysUntilSaturation` *float64 — nil se tendência decrescente
  - `EstimatedDate` *time.Time — data absoluta calculada
  - `Confidence` string — "high" (D-3+D-7+D-14 consistentes), "medium" (2 pontos), "low" (apenas D-3)
  - `AffectedNode` string — node mais crítico (para conntrack)
  - `TrendAcceleration` float64 — se positivo, piora está acelerando
- [ ] 10.2 Criar struct `PoolSaturationTimeline` em `models.go`:
  - `Forecasts` []SaturationForecast — uma por métrica
  - `MostCritical` *SaturationForecast — a que satura primeiro
  - `Summary` string — "pool saturará em X dias (conntrack, node Y)"
- [ ] 10.3 Adicionar `SaturationTimeline PoolSaturationTimeline` ao `NodePoolPredictionResult`
- [ ] 10.4 Implementar `calculateSaturationTimeline()` em `analyzer.go`:
  - Calcular `dailyGrowthRate` a partir dos snapshots D-0/D-3/D-7/D-14
  - Usar regressão linear simples: `slope = (D0 - D14) / 14` com fallback para `(D0 - D7) / 7` e `(D0 - D3) / 3`
  - Calcular `daysUntil = (threshold - current) / dailyGrowthRate`
  - Retornar nil se `dailyGrowthRate <= 0` (tendência estável ou decrescente)
  - Para conntrack: analisar por node, retornar o mais crítico
  - Calcular aceleração: comparar `(D0-D7)/7` vs `(D7-D14)/7`
  - Confidence: "high" se D-3, D-7 e D-14 têm dados, "medium" se 2, "low" se só D-3
- [ ] 10.5 Chamar `calculateSaturationTimeline()` dentro de `Analyze()` após calcular health score
- [ ] 10.6 Incluir timeline no relatório Markdown (nova seção "PREVISAO DE SATURACAO")
- [ ] 10.7 Incluir timeline no relatório PDF (nova seção após Tendências)

### Tarefas Frontend
- [ ] 10.8 Criar componente visual `SaturationTimeline` em `NodePoolPredictionModal.tsx`:
  - Card de destaque para a métrica mais crítica (cor vermelha/laranja conforme proximidade)
  - Tabela com todas as métricas: Métrica | Atual | Crescimento/dia | Satura em | Data | Confiança
  - Badge de urgência: "CRÍTICO (<7 dias)", "ATENÇÃO (7-30 dias)", "ESTÁVEL (>30 dias ou N/A)"
  - Node afetado no caso do conntrack
- [ ] 10.9 Integrar `SaturationForecast` mais crítico no `ActionSummary` (topo do modal)
  - Se `DaysUntilSaturation < 7`: status "critical" + mensagem específica
  - Se `DaysUntilSaturation < 30`: status "attention"
- [ ] 10.10 Adicionar testes unitários para `calculateSaturationTimeline()`

---

## Fase 11 — Correlação com HPAs do Pool ✅ Concluída em 24/02/2026
**Objetivo**: Identificar quando HPAs em maxReplicas indicam gargalo confirmado no pool
**Impacto**: ⭐⭐⭐⭐⭐ | **Esforço**: Médio

- [x] 11.1 Coletar HPAs que rodam em nodes do pool (via K8s API, cross-reference pods → HPAs)
- [x] 11.2 Detectar HPAs com `currentReplicas == maxReplicas` (atMax bool)
- [x] 11.3 Struct `HPAPoolCorrelation`: hpaName, namespace, targetName, targetKind, currentReplicas, maxReplicas, desiredReplicas, targetCPUPct, atMax, podsOnPool, totalPods
- [x] 11.4 Adicionar `HPACorrelation []HPAPoolCorrelation` ao `NodePoolMetrics`
- [x] 11.5 Penalidade no health score: HPAs em limite + CPU >= 70% → -5pts/HPA (máx -15)
- [x] 11.5 KeyFinding automático + predição ShortTerm + recomendação de revisar maxReplicas
- [x] 11.6 Frontend: accordion "HPAs neste Pool" com badge "em limite" e linha por HPA (réplicas, pods, target CPU)
- [x] 11.7 Relatório markdown: seção "HPAs COM PODS NESTE POOL" com tabela

**Implementado em**:
- `models.go`: struct `HPAPoolCorrelation` + campo `HPACorrelation` em `NodePoolMetrics`
- `collector.go`: `collectHPACorrelation()` + helpers `podOwnerName()`, `trimReplicaSetSuffix()`, `countAtMax()`
- `analyzer.go`: penalidade de health score + findings + predição + recomendação
- `nodepool_predictions.go`: seção markdown com tabela
- `NodePoolPredictionModal.tsx`: accordion com ícone Scale, badge "em limite", grid de HPAs

---

## Fase 12 — Gráficos de Tendência no Modal ✅ Concluída em 23/02/2026
**Objetivo**: Visualização intuitiva de CPU/Memória/Pods ao longo de 14 dias
**Impacto**: ⭐⭐⭐⭐ | **Esforço**: Baixo

- [x] 12.1 Usar dados D-0/D-3/D-7/D-14 já coletados (sem nova query Prometheus)
- [x] 12.2 Mini LineChart (Recharts) para CPU, Memória e Pods no modal
- [x] 12.3 Linha de limiar horizontal (85%) para CPU e Memória
- [ ] 12.4 Projeção de tendência futura no gráfico (linha tracejada) — adiado para Fase 14

**Implementado em**: `NodePoolPredictionModal.tsx`
- Helpers: `buildTrendChartData()`, `trendLineColor()`, `TrendMiniChart` (componente)
- Importados: `LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, ReferenceLine` de recharts
- CPU e Memória: Y axis 0-100%, threshold em 85% com linha vermelha tracejada
- Pods: Y axis auto-scale em count/node (sem threshold de percentual)
- conntrack: mantido como card de texto (sem histórico D-14 disponível)
- Cor da linha: laranja (crescendo), verde (decrescendo), índigo (estável)
- Deltas D-3/D-7/D-14 mantidos abaixo de cada gráfico como informação secundária

---

## Fase 13 — Ephemeral Storage Growth Rate ✅ Concluída em 24/02/2026
**Objetivo**: Detectar nodes com disco efêmero crescendo rapidamente (silent killer)
**Impacto**: ⭐⭐⭐⭐ | **Esforço**: Baixo

- [x] 13.1 Query Prometheus: `deriv(node_filesystem_avail_bytes[1h])` negado — positivo = disco enchendo (gauge, não counter)
- [x] 13.2 Calcular `MaxGrowthPctDay` + `MinDaysUntilFull` por node (struct `DiskGrowthAnalysis`)
- [x] 13.3 `saturationForecastDisk()` em `analyzer.go` → integrada como item 5 em `calculateSaturationTimeline()`
- [x] 13.4 Filtro ramp-up: uso < 15% E projeção > 90d → retorna ESTAVEL (evita falsos alarmes em nodes novos)
- [x] 13.5 Accordion "Disco Efêmero" em `NodePoolPredictionModal.tsx` (ícone HardDrive)
  - 3 cards: uso atual (colorido por threshold), taxa %/dia, dias até cheio
  - Alerta inline quando disco preenche em ≤ 30 dias

**Implementado em**:
- `queries.go` — query `deriv()` + funções de growth rate por node
- `models.go` — struct `DiskGrowthAnalysis` (MaxGrowthPctDay, MinDaysUntilFull, FastestNode, MaxUsagePct)
- `collector.go` — `calculateDiskGrowth()` com graceful degradation quando node_exporter indisponível
- `analyzer.go` — `saturationForecastDisk()` + integração no item 5 de `calculateSaturationTimeline()`
- `nodepool_predictions.go` — seção markdown "DISCO EFEMERO" no relatório
- `NodePoolPredictionModal.tsx` — accordion completo com cards e badge de urgência
- `saturation_test.go` — 7 novos casos; 28 testes PASS com -race ✅

---

## Fase 14 — Delta Entre Análises (Comparação Histórica) ✅ Concluída em 23/02/2026
**Objetivo**: "Desde a última análise (há 5 dias): conntrack +15%, CPU +8%"
**Impacto**: ⭐⭐⭐ | **Esforço**: Baixo

- [x] 14.1 Ao salvar nova análise, buscar a análise anterior do mesmo pool no SQLite
- [x] 14.2 Calcular delta para: health score, CPU%, mem%, pods, conntrack%, bin packing efficiency
- [x] 14.3 Struct `NodePoolAnalysisDelta` com deltas, listas improving/degrading e summary legível
- [x] 14.4 Exibir delta no modal: badges verde (melhorou) / vermelho (piorou) por métrica

**Implementado em**:
- `models.go` — struct `NodePoolAnalysisDelta` adicionada a `NodePoolPredictionResult`
- `handlers/nodepool_predictions.go` — função `calculateNodePoolDelta()` chamada após Save()
  - Busca 2 análises mais recentes via `store.List(limit=2)`, compara records[0] vs records[1]
  - Delta de: HealthScore, CPU pp, Mem pp, Pods/node, conntrack pp, BinPacking pp
  - Listas `Improving` / `Degrading` com threshold mínimo por métrica
  - Summary: "Desde análise anterior (há Xd): CPU +8.2pp, conntrack +15.3pp"
- `NodePoolPredictionModal.tsx` — seção "Comparação com análise anterior"
  - Exibida entre ActionSummary e HealthScore (seção sempre visível)
  - Badges coloridos: verde (menos pressão = melhora), vermelho (mais pressão = piora)
  - Listas "Melhorando" / "Degradando" resumidas em texto

---

## Fase 15 — Recomendação de VM SKU Alternativo ✅ Concluída em 24/02/2026
**Objetivo**: Sugerir SKU concreto baseado no perfil de uso real (consumo histórico P95)
**Impacto**: ⭐⭐⭐ | **Esforço**: Baixo

- [x] 15.1 `historicalP95()`: combina snapshots D-0 + `CPUTrendPerNode` + `MemTrendPerNode` (14 dias históricos)
- [x] 15.2 Filtro primário obrigatório: SKU deve suportar `cpuUsedAtP95` e `memUsedAtP95` com 20% headroom
  - `minVCPUs = ⌈cpuUsedAtP95 / 0.80⌉`, `minMemGB = ⌈memUsedAtP95 / 0.80⌉`
  - Cap de custo: máximo 1.50× custo atual (sem SKUs muito mais caros)
- [x] 15.3 `identifyBottleneckFromP95()`: "cpu" (P95 ≥ 60% e 1.4× dominância), "memory" (inverso), "balanced"
- [x] 15.4 Score composto: alívio do bottleneck (×8) + economia de custo (×5) + bônus de geração (v5 +0.3, v4 +0.2, v3 +0.1)
- [x] 15.5 `GetAllVMSpecs()` exportado em `predictions/azure_vm_specs.go` para iteração do catálogo
- [x] 15.6 Struct `NodePoolSKUAlternative` + campo `SKUAlternatives` em `NodePoolCostAnalysis`
- [x] 15.7 Cards de alternativas no modal (dentro do accordion de Custo) com deltas CPU/RAM e justificativa P95
- [x] 15.8 Seção markdown "VMs Alternativas (baseado em consumo historico P95)" no relatório

**Implementado em**:
- `predictions/azure_vm_specs.go` — `GetAllVMSpecs()` exportado para acesso externo ao catálogo
- `models.go` — struct `NodePoolSKUAlternative` (vmSize, vCPUs, memGB, custos USD/BRL, savings, bottleneck, rationale, deltas)
- `cost_analyzer.go` — `suggestAlternativeSKUs()`, `historicalP95()`, `identifyBottleneckFromP95()`, `buildSKURationale()`
- `nodepool_predictions.go` — tabela markdown de alternativas dentro da seção de Custo
- `NodePoolPredictionModal.tsx` — cards de SKUs (até 3) com badge de economia, deltas coloridos e rationale em itálico
- `saturation_test.go` — 8 novos casos (bottleneck detection, historicalP95, suggestSKU); 57 testes PASS com -race ✅

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
| 23/02/2026 | Fase 10 | Timeline de Saturação: structs `SaturationForecast`/`PoolSaturationTimeline`, `calculateSaturationTimeline()` com 4 métricas (CPU/mem/conntrack/pods), filtro ramp-up, accordion no modal, seção markdown/PDF | Paulo + Claude |
| 23/02/2026 | Fase 12 | Gráficos de tendência: `TrendMiniChart` (Recharts LineChart) para CPU/Mem/Pods com threshold 85% (ReferenceLine), cor dinâmica (laranja/verde/índigo), D-0/D-3/D-7/D-14 | Paulo + Claude |
| 23/02/2026 | Fase 14 | Delta entre análises: `calculateNodePoolDelta()` no handler, struct `NodePoolAnalysisDelta`, badges verde/vermelho no modal, summary "há Xd: CPU +8.2pp" | Paulo + Claude |
| 24/02/2026 | Fase 11 | Correlação com HPAs: `collectHPACorrelation()`, struct `HPAPoolCorrelation`, penalidade -5pts/HPA no health score, finding automático, accordion "HPAs neste Pool", seção markdown | Paulo + Claude |
| 24/02/2026 | Fase 13 | Disco efêmero: query `deriv()` negada, `DiskGrowthAnalysis`, `saturationForecastDisk()` como item 5 da timeline, filtro ramp-up (<15% + >90d), accordion modal, seção markdown; 28 testes PASS -race ✅ | Paulo + Claude |
| 24/02/2026 | Fase 15 | SKU alternativo histórico: `historicalP95()` (D-0+D-3/D-7/D-14), `identifyBottleneckFromP95()`, `suggestAlternativeSKUs()` (filtro P95 + cap 1.5×), `GetAllVMSpecs()`, cards no modal, tabela markdown; 57 testes PASS -race ✅ | Paulo + Claude |

---

*Atualizar esta tabela a cada sessão de desenvolvimento.*
