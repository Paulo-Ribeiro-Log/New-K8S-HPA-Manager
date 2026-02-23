# Estudo: Análise Preditiva para Node Pools

**Data:** 21/02/2026
**Autor:** Paulo + Claude Sonnet 4.6
**Status:** Aprovado para implementação

---

## 1. Motivação

A análise preditiva de deployments (v1.3.8+) se mostrou valiosa, mas node pools têm impacto operacional **ainda maior**:

- Escalar um deployment → segundos
- Escalar um node pool → **5–15 minutos** (provisionamento de VM no Azure)
- Um node saturado afeta **todos os workloads** do pool, não só um deployment

Portanto, prever saturação de node pool **antes** que aconteça é mais estratégico do que prever saturação de um deployment individual.

---

## 2. O que já existe no codebase

O terreno está parcialmente preparado. Os seguintes elementos já existem em `internal/monitoring/predictions/`:

### models.go
- `ConntrackAnalysis` — struct completo com nodes warning/critical, highest node, usage %
- `ConntrackNodeInfo` — por node: current entries, max entries, usage %, status
- `VMSizingInfo` — min/max/current nodes, VM SKU, max pods per node
- `BinPackingAnalysis` — eficiência atual, fragmentação, necessidade de rebalancing
- `NodeMetrics`, `NodeInfo`, `ClusterCapacity` — já modelados para contexto de deployment

### queries.go
- `GetConntrackEntriesQuery()` → `node_nf_conntrack_entries`
- `GetConntrackLimitQuery()` → `node_nf_conntrack_entries_limit`
- `GetConntrackUsageRatioQuery()` → `node_nf_conntrack_entries / node_nf_conntrack_entries_limit * 100`
- `GetNodeCPUUsageQuery()`, `GetNodeMemoryUsageQuery()` — por instance (node_exporter)

---

## 3. Diferenças críticas vs Análise de Deployments

| Dimensão | Deployment | Node Pool |
|---|---|---|
| Scaling entity | Réplicas (segundos) | VMs (5–15 min) |
| Custo | CPU/mem proporcional | VM SKU inteiro (muito mais caro) |
| Limites duros | HPA max_replicas | max-pods-per-node, IP limits |
| conntrack | N/A (nível de container) | Crítico — por node (kernel Linux) |
| Disk I/O | Container ephemeral | IOPS do disco VM (Azure Premium SSD) |
| PID pressure | Raramente relevante | Risco real em pools com pods densos |
| Autoscaler | HPA (horizontal pod) | Cluster Autoscaler (scale VM) |
| Network | Container bandwidth | VM NIC limits (dependente do SKU) |
| Fragmentação | N/A | Bin-packing: nodes com recursos ociosos |
| Histórico de scaling | kube_deployment_* | Kubernetes Events + Azure Activity Log |

---

## 4. Análise conntrack: Por Pool ou Cluster Inteiro?

### Conceito técnico
conntrack (Connection Tracking) é uma feature do kernel Linux que rastreia conexões de rede. Cada node tem **sua própria tabela conntrack independente**. Quando a tabela esgota, novas conexões são **silenciosamente descartadas** — um dos problemas mais difíceis de diagnosticar em Kubernetes.

### Decisão de escopo
**Recomendação: ambos os níveis**

| Nível | Prós | Contras |
|---|---|---|
| **Por node do pool** (primário) | Acionável, correlacionado aos workloads do pool, permite identificar node problemático | Não mostra pressão em outros pools |
| **Cluster inteiro** (secundário) | Panorama completo, mostra se problema é concentrado ou distribuído | Menos específico para decisão sobre este pool |

### Como filtrar nodes do pool no Prometheus
Nodes AKS têm labels: `kubernetes.azure.com/agentpool=<nome>` ou `agentpool=<nome>`

```promql
# conntrack filtrado ao pool "compute"
node_nf_conntrack_entries{instance=~"<ips-dos-nodes-do-pool>"}

# Abordagem: resolver IPs dos nodes via K8s API (label selector) → montar regex
```

### Thresholds sugeridos
- `< 70%` → OK (verde)
- `70–85%` → Warning (amarelo) — iniciar investigação
- `> 85%` → Critical (vermelho) — risco iminente de descarte de conexões
- `> 95%` → Emergência — conexões sendo descartadas agora

---

## 5. Sobre "Apenas nodes existentes são suficientes?"

### Resposta: Sim, com ressalvas

**O que o Prometheus resolve:**
- Dados históricos de nodes removidos permanecem no TSDB até expirar o retention
- Snapshots D-3, D-7, D-14 funcionam mesmo se o pool escalou/descalou

**O problema real:**
- Se o pool tinha 5 nodes há 7 dias e hoje tem 10, a soma agregada não é comparável
- **Solução**: normalizar por node (médias per-node, não soma total) ao comparar períodos com node counts diferentes

**Limitação honesta:**
- Sem Prometheus: análise restrita ao estado atual + eventos K8s (~1h de histórico via API)
- Nodes removidos há mais tempo que o retention do Prometheus: dados perdidos

**Implementação prática:**
```go
// Ao calcular tendência, verificar se node count mudou entre snapshots
// Se mudou: usar média por node, não soma
avgCPUPerNode := totalCPU / float64(nodeCount)
```

---

## 6. Arquitetura da Solução

### Novo pacote: `internal/monitoring/nodepoolpredictions/`

```
nodepoolpredictions/
├── models.go          // structs NodePoolMetrics, NodePoolPredictionResult, etc.
├── queries.go         // queries Prometheus específicas para nodes
├── collector.go       // coleta de K8s API + Prometheus + Azure API
├── analyzer.go        // análise via IA (Ollama/Claude) com prompt específico
└── cost_analyzer.go   // custo real baseado no VM SKU (mais preciso que deployment)
```

### Novos arquivos de backend
```
internal/web/handlers/nodepool_predictions.go  // REST API handler
internal/storage/nodepool_predictions_store.go // SQLite persistence
```

### Frontend
```
src/components/NodePoolPredictionModal.tsx      // modal de resultados
src/hooks/useNodePoolPredictions.ts             // hook React Query
```

---

## 7. Fontes de Dados

| Fonte | O que coleta | Disponibilidade |
|---|---|---|
| Kubernetes API | Nodes do pool (labels), conditions, events, pod density | Sempre disponível |
| Azure AKS API | Min/max/current nodes, VM SKU, autoscaler config | Requer autenticação Azure |
| Prometheus | CPU/mem/disk/network/conntrack por node (histórico) | Quando configurado |
| Metrics Server | CPU/mem atual por node | Geralmente disponível |

---

## 8. Queries Prometheus para Node Pool

```promql
# CPU por node (trend) — filtrado por nodes do pool
rate(node_cpu_seconds_total{mode!="idle", instance=~"<POOL_NODES_REGEX>"}[5m])

# Memória disponível por node
node_memory_MemAvailable_bytes{instance=~"<POOL_NODES_REGEX>"}
node_memory_MemTotal_bytes{instance=~"<POOL_NODES_REGEX>"}

# conntrack por node (NOVO — não existe em deployment analysis)
node_nf_conntrack_entries{instance=~"<POOL_NODES_REGEX>"}
node_nf_conntrack_entries_limit{instance=~"<POOL_NODES_REGEX>"}

# Disk por node
rate(node_disk_read_bytes_total{instance=~"<POOL_NODES_REGEX>"}[5m])
rate(node_disk_written_bytes_total{instance=~"<POOL_NODES_REGEX>"}[5m])
node_filesystem_avail_bytes{instance=~"<POOL_NODES_REGEX>", mountpoint="/"}

# Pod density por node
count(kube_pod_info{node=~"<POOL_NODES_NAME_REGEX>"}) by (node)
kube_node_status_capacity{resource="pods", node=~"<POOL_NODES_NAME_REGEX>"}

# PID count por node
node_processes_total{instance=~"<POOL_NODES_REGEX>"}

# Network por node
rate(node_network_receive_bytes_total{instance=~"<POOL_NODES_REGEX>", device!="lo"}[5m])
rate(node_network_transmit_bytes_total{instance=~"<POOL_NODES_REGEX>", device!="lo"}[5m])

# Autoscaler events (cluster-wide, filtrar por pool)
kube_event_count_total{reason="TriggeredScaleUp", involvedObject_kind="Node"}
kube_event_count_total{reason="ScaleDown", involvedObject_kind="Node"}
```

---

## 9. Modelos de Dados Principais

### NodePoolPredictionRequest
```go
type NodePoolPredictionRequest struct {
    Cluster      string `json:"cluster" binding:"required"`
    NodePoolName string `json:"nodepool_name" binding:"required"`
    UserEmail    string `json:"user_email,omitempty"`
}
```

### NodePoolMetrics (dados coletados)
```go
type NodePoolMetrics struct {
    NodePoolName   string
    VMSize         string
    CurrentNodes   int
    MinNodes       int
    MaxNodes       int

    // Estado atual de cada node no pool
    NodesSnapshot  []NodePoolNodeSnapshot

    // Trends por node (normalizadas — média por node)
    // Snapshots: atual, D-3, D-7, D-14
    CPUTrendPerNode    []TrendSnapshot
    MemTrendPerNode    []TrendSnapshot
    PodsTrendPerNode   []TrendSnapshot

    // conntrack (primário: filtrado ao pool; secundário: cluster-wide)
    ConntrackPerNode   []ConntrackNodeInfo
    ConntrackCluster   ConntrackAnalysis

    // Eventos do Cluster Autoscaler
    AutoscalerEvents   []AutoscalerEvent

    // Nodes com pressões ativas
    NodesWithPressure  []NodePressureInfo

    // Fragmentação / bin-packing
    BinPacking         BinPackingAnalysis

    // Custo real baseado no VM SKU
    CostAnalysis       NodePoolCostAnalysis
}
```

### NodePoolNodeSnapshot (estado de um node)
```go
type NodePoolNodeSnapshot struct {
    NodeName       string
    CPUUsagePercent   float64
    MemUsagePercent   float64
    PodCount          int
    PodCapacity       int
    PodDensityPercent float64
    ConntrackPercent  float64
    DiskUsagePercent  float64
    PIDCount          int
    Conditions        []string  // pressures ativas
    IsUnschedulable   bool      // cordoned
}
```

---

## 10. Health Score específico para Node Pool

| Componente | Peso | Métrica |
|---|---|---|
| Node Availability | 25% | nodes ready vs total |
| Resource Headroom | 30% | CPU/mem disponível até próximo scale |
| Pod Density | 20% | % de capacidade de pods usada (máx por node) |
| conntrack Safety | 15% | % conntrack no node mais saturado do pool |
| Autoscaler Health | 10% | frequência e sucesso de scale events |

---

## 11. Custo: Muito mais preciso que Deployment

Para node pool o custo é baseado no **VM SKU real** (já temos `azure_vm_specs.go`):

```go
type NodePoolCostAnalysis struct {
    VMSize              string
    CostPerNodePerHour  float64  // ex: Standard_D4s_v3 = $0.192/h
    CurrentMonthlyCost  float64  // currentNodes * 730h * preço
    MaxMonthlyCost      float64  // maxNodes * 730h * preço
    IdleWastePercent    float64  // % custo desperdiçado (nodes subutilizados)
    RecommendedNodes    int      // otimizado pelo uso real P95
    ProjectedSavings    float64  // economia mensal com right-sizing
    CostPerNodeBRL      float64  // convertido via API de câmbio
}
```

---

## 12. API REST planejada

```
POST /api/v1/nodepoolpredictions/analyze        — inicia análise
GET  /api/v1/nodepoolpredictions/history         — histórico com filtros
GET  /api/v1/nodepoolpredictions/report/:id/markdown — exportar relatório MD
GET  /api/v1/nodepoolpredictions/report/:id/pdf     — exportar relatório PDF
```

---

## 13. Ponto de entrada no Frontend

**Localização do botão**: `NodePoolEditor.tsx` → Tab "Configuration" → header do painel, junto aos demais botões de ação.

**Componentes novos:**
- `NodePoolPredictionModal.tsx` — modal de resultados (segue padrão do deployment)
- `NodePoolPredictionHistoryModal.tsx` — histórico de análises
- `hooks/useNodePoolPredictions.ts` — React Query hook

---

## 14. Decisões de Design Confirmadas

1. **conntrack**: por node do pool (primário) + cluster-wide (contexto)
2. **Histórico**: Prometheus resolve trends mesmo com nodes removidos; normalizar por node count quando pool escala/descala
3. **Custo**: baseado em VM SKU real (muito mais preciso que deployment)
4. **Storage**: SQLite separado (`nodepool_predictions.db`)
5. **IA**: mesmo provider (Ollama/Claude) com prompt específico para infraestrutura
6. **Prompt IA**: focado em: quando pool satura, conntrack vai esgotar antes da memória, autoscaler consegue reagir a tempo, VM SKU correto para o workload
