# ANÁLISE PREDITIVA - DOCUMENTAÇÃO COMPLETA

**Data da Implementação**: Janeiro 2026  
**Versão**: 1.0  
**Status**: Produção

---

## 📋 SUMÁRIO

1. [Visão Geral](#visão-geral)
2. [Funcionalidades Implementadas](#funcionalidades-implementadas)
3. [Correções de Bugs Críticos](#correções-de-bugs-críticos)
4. [Análise de Capacidade para Crescimento](#análise-de-capacidade-para-crescimento)
5. [Queries Prometheus Corrigidas](#queries-prometheus-corrigidas)
6. [Estruturas de Dados](#estruturas-de-dados)
7. [Relatórios e Exportação](#relatórios-e-exportação)
8. [Interface do Usuário](#interface-do-usuário)
9. [Banco de Dados e Histórico](#banco-de-dados-e-histórico)
10. [Melhorias de Qualidade](#melhorias-de-qualidade)

---

## 🎯 VISÃO GERAL

A Análise Preditiva é um sistema completo de monitoramento e previsão de comportamento de deployments Kubernetes, utilizando métricas históricas do Prometheus, análise de tendências e inteligência artificial para gerar recomendações acionáveis.

### Objetivos Principais

- **Prevenir Problemas**: Identificar problemas antes que impactem os usuários
- **Otimizar Recursos**: Detectar over-provisioning e oportunidades de economia
- **Planejar Capacidade**: Calcular crescimento horizontal realista baseado em recursos disponíveis
- **Gerar Insights**: Análise profunda com IA para recomendações estratégicas

---

## ⚙️ FUNCIONALIDADES IMPLEMENTADAS

### 1. Coleta de Métricas Temporais

**Arquivo**: `internal/monitoring/predictions/collector.go`

#### Snapshots Temporais
Coleta métricas em 5 pontos no tempo:
- **Atual** (current)
- **3 dias atrás** (day_3_ago)
- **7 dias atrás** (day_7_ago)
- **10 dias atrás** (day_10_ago)
- **14 dias atrás** (day_14_ago)

#### Métricas Coletadas por Snapshot
```go
type MetricSnapshot struct {
    Timestamp      time.Time
    CPUUsageAvg    float64        // CPU média em cores
    CPUUsageP95    float64        // CPU percentil 95
    MemoryUsageAvg float64        // Memory em bytes
    MemoryUsageP95 float64        // Memory percentil 95
    NetworkRxAvg   float64        // Rede recebida (bytes/s)
    NetworkTxAvg   float64        // Rede transmitida (bytes/s)
    RestartCount   int            // Contagem de restarts
    ErrorRate      float64        // Taxa de erro (%)
    Latency        LatencyMetrics // P50, P95, P99 em ms
}
```

### 2. Análise de Tendências

**Cálculo Automático**:
- Mudança percentual entre períodos
- Direção da tendência (increasing, decreasing, stable, volatile)
- Projeção de crescimento

```go
type TrendAnalysis struct {
    CPUTrend            TrendDirection
    MemoryTrend         TrendDirection
    ErrorRateTrend      TrendDirection
    LatencyTrend        TrendDirection
    CPUChange7d         float64
    MemoryChange7d      float64
    ErrorRateChange7d   float64
    LatencyChange7d     float64
}
```

### 3. Health Score

**Sistema de Pontuação 0-100**:

#### Componentes do Score
- **Availability** (30%): Réplicas disponíveis vs desejadas
- **Performance** (30%): CPU/Memory vs limites, latência
- **Stability** (25%): Taxa de restarts, error rate
- **Efficiency** (15%): Utilização de recursos vs requests

#### Categorias
- **90-100**: Healthy (Verde)
- **70-89**: Warning (Amarelo)
- **0-69**: Critical (Vermelho)

### 4. Previsões com IA

**Integração com Modelos de IA**:
- Análise de padrões históricos
- Previsões de curto, médio e longo prazo
- Severidade e probabilidade de eventos

```go
type PredictionsAnalysis struct {
    ShortTerm  []PredictedEvent // 4 horas
    MediumTerm []PredictedEvent // 48 horas
    LongTerm   []PredictedEvent // 7 dias
}

type PredictedEvent struct {
    Event       string
    Probability float64  // 0.0 a 1.0
    Impact      string
    Severity    string   // info, low, medium, high, critical
    TimeWindow  string
}
```

### 5. Root Cause Analysis

**Análise de Causa Raiz**:
```go
type RootCauseAnalysis struct {
    PrimaryIssue   RootCause
    SecondaryIssues []RootCause
    Certainty       float64
}

type RootCause struct {
    Title       string
    Description string
    Evidence    []string
    Certainty   float64
    Category    string   // resource, config, external, code
    Remediation string
}
```

### 6. Análise de Impacto

**Se Nenhuma Ação For Tomada**:
- Impacto nos usuários
- Impacto na infraestrutura
- Timeline de deterioração
- Riscos identificados

**Se Otimizações Forem Aplicadas**:
- Benefícios esperados
- Melhorias de performance
- Economia de recursos

### 7. Recomendações Priorizadas

**Sistema de Priorização 1-5**:
```go
type Recommendation struct {
    Priority               int      // 1 (mais alta) a 5
    Title                  string
    Description            string
    Category               string   // scaling, resources, config, monitoring
    Actions                []string // Passos específicos
    ExpectedImpact         string
    ImplementationEstimate ImplementationEstimate
}

type ImplementationEstimate struct {
    TimeRequired           string  // "5 minutes", "1 hour", "1 day"
    Complexity             string  // low, medium, high
    RiskLevel              string  // low, medium, high
    RequiresDowntime       bool
    ResourceEfficiencyGain float64 // % de economia
}
```

### 8. VM Sizing e Contexto de Infraestrutura

**Coleta Automática**:
- Tipo de instância predominante (Azure, AWS, GCP)
- CPU e Memory por VM
- Máximo de pods por node
- Min/Max/Current nodes do node pool

```go
type VMSizingInfo struct {
    PredominantInstanceType string
    CPUPerVM                int
    MemoryPerVM             int
    MaxPodsPerNode          int
    MinNodes                int
    MaxNodes                int
    CurrentNodes            int
    RecommendedInstanceType string
    RecommendationReason    string
}
```

**Detecção de Instance Type**:
- Leitura de labels Kubernetes:
  - `node.kubernetes.io/instance-type`
  - `beta.kubernetes.io/instance-type`
  - `agentpool` (Azure)
- Fallback para inferência por CPU/Memory

### 9. Aplicações Concorrentes

**Análise de Competição por Recursos**:
```go
type CompetingApp struct {
    Name        string
    Namespace   string
    Replicas    int     // Número de réplicas
    CPUUsage    float64 // CPU total em cores
    MemoryUsage float64 // Memory total em GB
    ImpactLevel string  // low, medium, high
}
```

**Classificação por Impacto**:
- **High**: CPU > 2.0 cores
- **Medium**: CPU > 1.0 cores
- **Low**: CPU ≤ 1.0 cores

---

## 🐛 CORREÇÕES DE BUGS CRÍTICOS

### Bug 1: CPU e Memória = 0.00 cores/GB

**Problema Identificado**:
```go
// ANTES (INCORRETO)
`avg(rate(container_cpu_usage_seconds_total{...}[5m])) by (pod)`
`avg(container_memory_working_set_bytes{...}) by (pod)`
```

**Causa**: 
- Queries retornavam **vetor** (múltiplos valores por pod)
- Função `queryScalar()` pegava apenas `v[0]`
- Se vetor vazio ou primeiro pod zerado → resultado 0.00

**Solução Implementada**:
```go
// DEPOIS (CORRETO)
`sum(rate(container_cpu_usage_seconds_total{...,container!="",container!="POD"}[5m]))`
`sum(container_memory_working_set_bytes{...,container!="",container!="POD"})`
```

**Mudanças**:
- ✅ `avg() by (pod)` → `sum()` (soma todos os pods, retorna escalar único)
- ✅ Adicionados filtros `container!=""` e `container!="POD"`
- ✅ Agora retorna CPU/Memory **total** do deployment

### Bug 2: Cluster Total CPU/Memory = 0.00

**Problema Identificado**:
```go
// ANTES (INCORRETO)
`sum(kube_node_status_capacity{resource="cpu"})`
`sum(kube_pod_container_resource_requests{resource="cpu"})`
```

**Causa**:
- Métricas no formato kube-state-metrics **v1.x**
- Versões v2.x+ usam métricas separadas

**Solução Implementada**:
```go
// DEPOIS (CORRETO - Compatibilidade v1.x e v2.x)
`sum(kube_node_status_capacity_cpu_cores) or sum(kube_node_status_capacity{resource="cpu"})`
`sum(kube_node_status_capacity_memory_bytes) or sum(kube_node_status_capacity{resource="memory"})`
`sum(kube_pod_container_resource_requests_cpu_cores) or sum(kube_pod_container_resource_requests{resource="cpu"})`
`sum(kube_pod_container_resource_requests_memory_bytes) or sum(kube_pod_container_resource_requests{resource="memory"})`
```

**Mudanças**:
- ✅ Tenta formato **v2.x** primeiro (`_cpu_cores` / `_memory_bytes`)
- ✅ Fallback para formato **v1.x** (`{resource="cpu"}`)
- ✅ Compatibilidade com todas versões do kube-state-metrics

### Bug 3: Instance Type Incorreto (t3.2xlarge ao invés de Standard_F4s_v2)

**Problema Identificado**:
- Sistema calculava tipo de instância por CPU/Memory
- Não lia labels reais do Kubernetes

**Solução Implementada**:
```go
// Leitura de múltiplos labels
instanceType := ""
labels := []string{
    "label_node_kubernetes_io_instance_type",
    "label_beta_kubernetes_io_instance_type",
    "label_agentpool",
}

for _, label := range labels {
    query := fmt.Sprintf(`kube_node_labels{node="%s",%s!=""}`, nodeName, label)
    result := queryPrometheus(query)
    if result != "" {
        instanceType = result
        break
    }
}

// Fallback para inferência apenas se não encontrar label
if instanceType == "" {
    instanceType = determineInstanceType(cpuCap, memCap)
}
```

### Bug 4: Informação Insuficiente sobre Aplicações Concorrentes

**Problema**:
- Mostrava apenas contagem: "4 aplicação(s)"
- Não mostrava nomes, namespaces, réplicas

**Solução**:
- Lista completa com todos os detalhes
- Organizada por nível de impacto
- Inclui número de réplicas de cada aplicação
- CPU e Memory por réplica calculados

---

## 📊 ANÁLISE DE CAPACIDADE PARA CRESCIMENTO

### Implementação Completa

**Nova Estrutura de Dados**:
```go
type GrowthCapacityAnalysis struct {
    // Aplicação em Análise
    TargetApp ApplicationCapacity
    
    // Aplicações Concorrentes
    CompetingApps       []ApplicationCapacity
    TotalCompetingUsage ResourceUsage
    
    // Capacidade do Cluster
    CurrentCapacity CapacityInfo
    MaxCapacity     CapacityInfo
    
    // Análise de Crescimento
    AvailableForGrowth        ResourceUsage
    MaxReplicasCurrentNodes   int
    MaxReplicasWithMaxNodes   int
    ReplicasIfRemoveCompeting int
    
    // Recomendações
    RecommendedMaxReplicas int
    GrowthRecommendation   string
    BottleneckResource     string // cpu, memory, nodes
}
```

### Cálculo Realista de Capacidade

**Algoritmo**:

1. **Coleta de Contexto**:
   - Min/Max/Current nodes do node pool
   - Réplicas atuais da aplicação
   - CPU/Memory por réplica
   - Todas aplicações concorrentes com suas réplicas

2. **Cálculo de Capacidade Atual**:
   ```go
   cpuAvailable = clusterTotalCPU - cpuAllocated
   memAvailable = clusterTotalMemory - memAllocated
   
   cpuPerReplica = targetApp.CPUUsage / targetApp.Replicas
   memPerReplica = targetApp.MemoryUsage / targetApp.Replicas
   
   maxReplicasByCPU = cpuAvailable / cpuPerReplica
   maxReplicasByMem = memAvailable / memPerReplica
   
   maxReplicasCurrentNodes = min(maxReplicasByCPU, maxReplicasByMem)
   ```

3. **Cálculo com Escalamento de Nodes**:
   ```go
   maxCapacityCPU = maxNodes * cpuPerVM
   maxCapacityMem = maxNodes * memPerVM
   
   maxReplicasWithMaxNodes = min(
       maxCapacityCPU / cpuPerReplica,
       maxCapacityMem / memPerReplica
   )
   ```

4. **Análise What-If (sem concorrentes)**:
   ```go
   cpuIfRemoveCompeting = cpuAvailable + totalCompetingCPU
   memIfRemoveCompeting = memAvailable + totalCompetingMem
   
   replicasIfRemoveCompeting = min(
       cpuIfRemoveCompeting / cpuPerReplica,
       memIfRemoveCompeting / memPerReplica
   )
   ```

5. **Identificação de Gargalo**:
   ```go
   if memPerReplica/memAvailable > cpuPerReplica/cpuAvailable {
       bottleneck = "memory"
   } else {
       bottleneck = "cpu"
   }
   ```

### Relatório de Crescimento

**Seções do Relatório**:

1. **Configuração do Node Pool**:
   - Nodes Mínimos: X
   - Nodes Máximos: Y
   - Nodes Atuais: Z

2. **Aplicação em Análise**:
   - Réplicas Atuais
   - CPU Total e por réplica
   - Memory Total e por réplica

3. **Aplicações Concorrentes**:
   - Tabela completa com todas as aplicações
   - Nome, Namespace, Réplicas
   - CPU/Memory Total e por réplica
   - Total consumido por concorrentes

4. **Capacidade do Cluster**:
   - Cenário Atual (nodes atuais)
   - Cenário Máximo (se escalar até max nodes)

5. **Capacidade Disponível**:
   - CPU Disponível
   - Memory Disponível
   - Recurso Gargalo identificado

6. **Cenários de Escalabilidade**:
   - Max réplicas com nodes atuais
   - Max réplicas escalando para max nodes
   - Max réplicas se remover concorrentes

7. **Recomendação Final**:
   - Orientação clara sobre crescimento
   - Máximo recomendado de réplicas

---

## 🔍 QUERIES PROMETHEUS CORRIGIDAS

### Queries de CPU e Memória do Deployment

**CPU Usage (média)**:
```promql
sum(rate(container_cpu_usage_seconds_total{
    namespace="NAMESPACE",
    pod=~"DEPLOYMENT-.*",
    container!="",
    container!="POD"
}[5m]))
```

**CPU Usage P95**:
```promql
quantile(0.95, rate(container_cpu_usage_seconds_total{
    namespace="NAMESPACE",
    pod=~"DEPLOYMENT-.*",
    container!="",
    container!="POD"
}[5m]))
```

**Memory Usage (média)**:
```promql
sum(container_memory_working_set_bytes{
    namespace="NAMESPACE",
    pod=~"DEPLOYMENT-.*",
    container!="",
    container!="POD"
})
```

**Memory Usage P95**:
```promql
quantile(0.95, container_memory_working_set_bytes{
    namespace="NAMESPACE",
    pod=~"DEPLOYMENT-.*",
    container!="",
    container!="POD"
})
```

### Queries de Cluster Total (Compatibilidade v1.x e v2.x)

**CPU Total**:
```promql
sum(kube_node_status_capacity_cpu_cores) 
or 
sum(kube_node_status_capacity{resource="cpu"})
```

**Memory Total**:
```promql
sum(kube_node_status_capacity_memory_bytes) 
or 
sum(kube_node_status_capacity{resource="memory"})
```

**CPU Alocada**:
```promql
sum(kube_pod_container_resource_requests_cpu_cores) 
or 
sum(kube_pod_container_resource_requests{resource="cpu"})
```

**Memory Alocada**:
```promql
sum(kube_pod_container_resource_requests_memory_bytes) 
or 
sum(kube_pod_container_resource_requests{resource="memory"})
```

### Query de Instance Type

**Multi-label detection**:
```promql
# Tentar label padrão
kube_node_labels{
    node="NODE_NAME",
    label_node_kubernetes_io_instance_type!=""
}

# Fallback beta label
kube_node_labels{
    node="NODE_NAME",
    label_beta_kubernetes_io_instance_type!=""
}

# Fallback Azure agentpool
kube_node_labels{
    node="NODE_NAME",
    label_agentpool!=""
}
```

### Query de Réplicas por Aplicação

**Número de Réplicas**:
```promql
kube_deployment_status_replicas{
    namespace="NAMESPACE",
    deployment="DEPLOYMENT_NAME"
}
```

**Fallback (contagem de pods)**:
```promql
count(kube_pod_info{
    namespace="NAMESPACE",
    pod=~"DEPLOYMENT_NAME-.*"
})
```

---

## 📦 ESTRUTURAS DE DADOS

### Principais Models

**Arquivo**: `internal/monitoring/predictions/models.go`

```go
// Request
type PredictionRequest struct {
    Cluster    string
    Namespace  string
    Deployment string
    UserEmail  string
}

// Result Completo
type PredictionResult struct {
    RequestID  string
    Cluster    string
    Namespace  string
    Deployment string
    AnalyzedAt time.Time
    DurationMs int64
    
    RawMetrics        DeploymentMetrics
    HealthScore       HealthScore
    Predictions       PredictionsAnalysis
    RootCauseAnalysis RootCauseAnalysis
    ImpactAnalysis    ImpactAnalysis
    ExecutiveSummary  ExecutiveSummary
    Recommendations   []Recommendation
}

// Métricas do Deployment
type DeploymentMetrics struct {
    Deployment string
    Namespace  string
    Cluster    string
    
    DesiredReplicas   int32
    CurrentReplicas   int32
    AvailableReplicas int32
    ReadyReplicas     int32
    Resources         ResourceRequests
    
    Current  MetricSnapshot
    Day3Ago  MetricSnapshot
    Day7Ago  MetricSnapshot
    Day10Ago MetricSnapshot
    Day14Ago MetricSnapshot
    
    Trends           TrendAnalysis
    NodeMetrics      NodeMetrics
    CompetingApps    []CompetingApp
    CapacityForecast CapacityForecast
}

// Node Metrics
type NodeMetrics struct {
    NodeDistribution     map[string]NodeInfo
    TotalCapacity        ClusterCapacity
    VMSizing             VMSizingInfo
    BinPackingAnalysis   BinPackingAnalysis
    NodesUsed            int
    TotalNodesInCluster  int
}

// Capacity Forecast
type CapacityForecast struct {
    CanScale              bool
    MaxAdditionalReplicas int
    LimitingFactor        string
    NodeAnalysis          NodeAnalysisDetail
    ScalingTimeline       ScalingTimeline
    NewNodesNeeded        int
    NewNodesReason        string
    GrowthAnalysis        GrowthCapacityAnalysis // NOVO
}
```

### Database Schema

**Tabela**: `predictions_history`

```sql
CREATE TABLE predictions_history (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id          TEXT UNIQUE NOT NULL,
    cluster             TEXT NOT NULL,
    namespace           TEXT NOT NULL,
    deployment          TEXT NOT NULL,
    analyzed_at         DATETIME NOT NULL,
    duration_ms         INTEGER,
    health_score        INTEGER,
    health_category     TEXT,
    risk_level          TEXT,
    action_required     BOOLEAN,
    
    -- JSON fields
    raw_metrics         TEXT,
    predictions         TEXT,
    recommendations     TEXT,
    executive_summary   TEXT,
    
    created_at          DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Índices para performance
CREATE INDEX idx_predictions_cluster ON predictions_history(cluster);
CREATE INDEX idx_predictions_namespace ON predictions_history(namespace);
CREATE INDEX idx_predictions_deployment ON predictions_history(deployment);
CREATE INDEX idx_predictions_analyzed_at ON predictions_history(analyzed_at DESC);
CREATE INDEX idx_predictions_health_score ON predictions_history(health_score);
```

---

## 📄 RELATÓRIOS E EXPORTAÇÃO

### Formatos Disponíveis

1. **Markdown (.md)**
2. **PDF (.pdf)**

### Estrutura do Relatório

#### Cabeçalho
- Título: ANALISE PREDITIVA: [deployment]
- Cluster, Namespace
- Data de geração
- Health Score com categoria

#### Seções

1. **SUMÁRIO EXECUTIVO**
   - Estado atual
   - Nível de risco
   - Ação necessária (SIM/NÃO)
   - Principais descobertas
   - Impacto no negócio

2. **DADOS ANALISADOS**
   - Métricas de Réplicas
   - Consumo de Recursos
   - Capacidade do Cluster
   - Tipo de VM/Instance
   - Aplicações Concorrentes

3. **ANÁLISE DE CAPACIDADE PARA CRESCIMENTO HORIZONTAL**
   - Configuração do Node Pool
   - Aplicação em Análise
   - Aplicações Concorrentes (tabela completa)
   - Capacidade do Cluster (atual vs máximo)
   - Capacidade Disponível
   - Cenários de Escalabilidade (tabela)
   - Recomendação Final

4. **HEALTH SCORE**
   - Score geral
   - Breakdown por componente
   - Interpretação

5. **PREVISÕES**
   - Curto Prazo (4h)
   - Médio Prazo (48h)
   - Longo Prazo (7d)

6. **ANÁLISE DE CAUSA RAIZ**
   - Problema principal
   - Problemas secundários
   - Evidências
   - Remediação

7. **ANÁLISE DE IMPACTO**
   - Se nenhuma ação for tomada
   - Se otimizações forem aplicadas
   - Timeline de ação

8. **RECOMENDAÇÕES**
   - Lista priorizada (1-5)
   - Categoria
   - Descrição
   - Ações específicas
   - Estimativa de implementação
   - Ganho de eficiência

### Geração de PDF

**Tecnologia**: jsPDF com autoTable

**Características**:
- Layout profissional A4
- Cores e formatação consistentes
- Tabelas responsivas
- Page breaks automáticos
- Cabeçalhos e rodapés
- Logo e branding
- Compressão otimizada

**Código**:
```typescript
const generatePredictionPDF = () => {
  const doc = new jsPDF();
  let yPos = 20;
  
  // Cabeçalho
  doc.setFontSize(20);
  doc.setTextColor(59, 130, 246);
  doc.text('ANÁLISE PREDITIVA', 15, yPos);
  
  // Seções com formatação
  addSection('Sumário Executivo', executiveSummary);
  addSection('Dados Analisados', dataAnalyzed);
  addSection('Análise de Crescimento', growthAnalysis);
  // ... mais seções
  
  // Tabelas
  autoTable(doc, {
    head: [['Cenário', 'Máximo de Réplicas']],
    body: scenariosData,
    theme: 'grid',
    headStyles: { fillColor: [59, 130, 246] }
  });
  
  doc.save(`predicao_${deployment}_${timestamp}.pdf`);
};
```

---

## 🎨 INTERFACE DO USUÁRIO

### Modal de Análise Preditiva

**Características**:
- Modal fullscreen (90vh)
- Scrollable content
- Organização em cards
- Color coding por severidade
- Gráficos interativos (Recharts)
- Botões de ação contextuais

**Seções do Modal**:

1. **Health Score Card**
   - Score visual grande
   - Breakdown de componentes
   - Categoria com cor

2. **Resumo Executivo**
   - Estado atual
   - Nível de risco
   - Principais descobertas
   - Impacto no negócio

3. **Dados Analisados** ⭐ NOVO
   - Grid de métricas de réplicas
   - Consumo de recursos detalhado
   - Capacidade do cluster

4. **Contexto de Infraestrutura**
   - VM Sizing
   - Aplicações Concorrentes
   - Lista completa organizada por impacto

5. **Análise de Capacidade para Crescimento** ⭐ NOVO
   - Node Pool configuration
   - Aplicação em análise (réplicas e recursos)
   - Tabela de concorrentes com réplicas
   - Tabela de capacidade (atual vs máximo)
   - Disponível para crescimento
   - Tabela de cenários de escalabilidade
   - Recomendação final destacada

6. **Gráficos de Tendências**
   - CPU ao longo do tempo (linha)
   - Memory ao longo do tempo (linha)
   - Restarts (barra)
   - Tooltips informativos

7. **Previsões**
   - Cards por time window
   - Severidade colorida
   - Probabilidade em %

8. **Recomendações**
   - Cards priorizados
   - Destaque para economia de custos
   - Estimativas de implementação
   - Ganho de eficiência em %

### Botões e Ações

- **Ver Histórico**: Abre modal de histórico
- **Exportar Relatório**: Escolhe formato (MD/PDF)
- **Aplicar Recomendação**: Ações quick (futuro)

### Histórico de Análises

**Modal Separado**: `PredictionHistoryModal.tsx`

**Funcionalidades**:
- Filtros avançados
  - Cluster
  - Namespace
  - Deployment
  - Health Score (range slider)
  - Risk Level
  - Data (range picker)
- Paginação (10/25/50/100 por página)
- Busca por texto
- Ordenação por colunas
- Ver detalhes de análise passada
- Comparação entre análises (futuro)

**Tabela**:
| Data | Deployment | Namespace | Health | Risco | Ação Req. | Ações |
|------|------------|-----------|--------|-------|-----------|-------|
| ... | ... | ... | 85 | medium | Não | Ver |

---

## 💾 BANCO DE DADOS E HISTÓRICO

### Storage Layer

**Arquivo**: `internal/storage/predictions_store.go`

**Interface**:
```go
type PredictionsStore struct {
    db *sql.DB
}

// Métodos principais
func (s *PredictionsStore) SavePrediction(record PredictionRecord) error
func (s *PredictionsStore) GetByID(id int64) (*PredictionRecord, error)
func (s *PredictionsStore) GetByRequestID(requestID string) (*PredictionRecord, error)
func (s *PredictionsStore) Query(filters PredictionQueryFilters) ([]PredictionRecord, error)
func (s *PredictionsStore) GetStatistics(cluster, namespace, deployment string) (*PredictionStatistics, error)
func (s *PredictionsStore) DeleteOlderThan(days int) (int64, error)
```

### Filtros de Query

```go
type PredictionQueryFilters struct {
    Cluster           string
    Namespace         string
    Deployment        string
    MinHealthScore    int
    MaxHealthScore    int
    RiskLevel         string
    ActionRequired    *bool
    StartDate         *time.Time
    EndDate           *time.Time
    Limit             int
    Offset            int
    OrderBy           string
    OrderDesc         bool
}
```

### Estatísticas

```go
type PredictionStatistics struct {
    TotalAnalyses    int
    AvgHealthScore   float64
    LastAnalyzedAt   time.Time
    CriticalCount    int
    WarningCount     int
    HealthyCount     int
}
```

### Migrations

**Auto-migrate no startup**:
```go
func (s *PredictionsStore) AutoMigrate() error {
    _, err := s.db.Exec(`
        CREATE TABLE IF NOT EXISTS predictions_history (
            -- schema completo
        )
    `)
    
    // Criar índices
    s.createIndexes()
    
    return err
}
```

---

## ✨ MELHORIAS DE QUALIDADE

### 1. Remoção de Emojis

**Problema**: Emojis quebravam renderização no jsPDF

**Arquivos Corrigidos**:
- `DeploymentsTab.tsx`: Removidos ✅ ⚠️ ⏱️ 📊 💰
- Comentários de código
- Mensagens de UI

**Substituições**:
- ✅ → (removido ou texto "OK")
- ⚠️ → "!"
- ⏱️ → "Tempo:"
- 📊 → "Complexidade:"
- 💰 → "Economia:"

### 2. Profissionalismo nos Relatórios

**Markdown**:
- Sem emojis
- Formatação consistente
- Tabelas bem estruturadas
- Hierarquia clara de títulos

**PDF**:
- Layout corporativo
- Cores profissionais (azul, cinza)
- Tipografia clara (sans-serif)
- Espaçamento adequado
- Paginação inteligente

### 3. Precisão nas Métricas

**Formatação Numérica**:
- CPU: 3 casas decimais (0.123 cores)
- Memory: 2 casas decimais (1.23 GB)
- Percentagens: 1 casa decimal (85.5%)
- Contadores: Inteiros (5 réplicas)

**Conversões Consistentes**:
```typescript
// CPU
const cpuCores = value.toFixed(3); // 0.500 cores
const cpuMillicores = (value * 1000).toFixed(0); // 500m

// Memory
const memGB = (bytes / (1024*1024*1024)).toFixed(2); // 1.50 GB
const memMB = (bytes / (1024*1024)).toFixed(0); // 1536 MB

// Percentage
const percent = value.toFixed(1); // 85.5%
```

### 4. Tratamento de Erros

**Backend**:
```go
if err != nil {
    log.Error().Err(err).Msg("Falha na coleta de métricas")
    return nil, fmt.Errorf("falha ao coletar métricas: %w", err)
}
```

**Frontend**:
```typescript
try {
    const result = await analyzePrediction(deployment);
    setPredictionResult(result);
} catch (error) {
    console.error('Erro na análise:', error);
    setPredictionResult({ 
        error: error.message || 'Erro desconhecido' 
    });
}
```

### 5. Performance

**Queries Otimizadas**:
- Índices em colunas frequentemente consultadas
- Limit/Offset para paginação
- Filtros no banco ao invés de pós-processamento
- Cache de resultados (futuro)

**Frontend**:
- Lazy loading de componentes pesados
- Virtualization em listas grandes
- Debounce em buscas
- Memoization de cálculos complexos

---

## 📋 CHECKLIST DE IMPLEMENTAÇÃO

### Backend ✅

- [x] Collector de métricas temporais
- [x] Análise de tendências
- [x] Health Score calculation
- [x] Integração com IA
- [x] Root Cause Analysis
- [x] Impact Analysis
- [x] Sistema de recomendações
- [x] VM Sizing detection
- [x] Aplicações concorrentes
- [x] Análise de capacidade para crescimento
- [x] Queries Prometheus corrigidas
- [x] Database layer completo
- [x] Geração de relatórios markdown
- [x] API endpoints (7 endpoints)

### Frontend ✅

- [x] Modal de análise completo
- [x] Gráficos de tendências
- [x] Cards de métricas
- [x] Health Score visual
- [x] Seção de dados analisados
- [x] Seção de análise de crescimento
- [x] Tabela de aplicações concorrentes
- [x] Geração de PDF profissional
- [x] Modal de histórico
- [x] Filtros avançados
- [x] Paginação
- [x] Busca e ordenação
- [x] Exportação (MD/PDF)
- [x] Remoção de emojis

### Qualidade ✅

- [x] Bugs críticos corrigidos
- [x] Queries testadas e validadas
- [x] Documentação completa
- [x] Formatação consistente
- [x] Tratamento de erros
- [x] Performance otimizada
- [x] Código limpo e comentado
- [x] TypeScript types completos

---

## 🚀 COMO USAR

### 1. Executar Análise

**Via Interface Web**:
1. Selecionar deployment na lista
2. Clicar em "Análise Preditiva"
3. Aguardar análise (5-10 segundos)
4. Ver resultado no modal

**Via API**:
```bash
curl -X POST http://localhost:8080/api/predictions/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "cluster": "production",
    "namespace": "default",
    "deployment": "my-app"
  }'
```

### 2. Exportar Relatório

**Markdown**:
```bash
curl http://localhost:8080/api/predictions/report/REQUEST_ID/markdown \
  -o relatorio.md
```

**PDF**:
- Via interface: Botão "Exportar Relatório" → Selecionar PDF
- Download automático do arquivo

### 3. Ver Histórico

**Via Interface**:
1. No modal de análise, clicar "Ver Histórico"
2. Aplicar filtros desejados
3. Clicar em "Ver" para análise específica

**Via API**:
```bash
curl "http://localhost:8080/api/predictions/history?cluster=prod&namespace=default&limit=10"
```

### 4. Limpar Histórico Antigo

**API Endpoint**:
```bash
curl -X DELETE "http://localhost:8080/api/predictions/cleanup?days=30"
```

---

## 📊 EXEMPLOS DE SAÍDA

### Relatório Markdown (Trecho)

```markdown
## ANÁLISE DE CAPACIDADE PARA CRESCIMENTO HORIZONTAL

**Configuração do Node Pool:**
- Nodes Mínimos: 2
- Nodes Máximos: 10
- Nodes Atuais: 5

**Aplicação em Análise:**
- Réplicas Atuais: 8
- CPU Total: 4.50 cores (0.563 cores/réplica)
- Memória Total: 12.00 GB (1.50 GB/réplica)

**Aplicações Concorrentes (com Réplicas):**

| Aplicação | Namespace | Réplicas | CPU Total | Memory Total | CPU/Réplica | Mem/Réplica |
|-----------|-----------|----------|-----------|--------------|-------------|-------------|
| api-backend | production | 12 | 6.75 cores | 18.00 GB | 0.563 cores | 1.50 GB |
| worker-queue | production | 4 | 2.00 cores | 8.00 GB | 0.500 cores | 2.00 GB |

**Total Concorrentes**: 8.75 cores CPU | 26.00 GB Memória

**Capacidade do Cluster:**

| Cenário | Nodes | CPU Total | Memória Total |
|---------|-------|-----------|---------------|
| Atual | 5 | 20.00 cores | 35.00 GB |
| Máximo (se escalar) | 10 | 40.00 cores | 70.00 GB |

**Cenários de Escalabilidade:**

| Cenário | Máximo de Réplicas |
|---------|-------------------:|
| Nodes Atuais (5) | **15 réplicas** |
| Escalando para Max Nodes (10) | **45 réplicas** |

**RECOMENDAÇÃO:**
- Pode escalar até 15 réplicas nos nodes atuais
- Máximo Recomendado: **15 réplicas**
```

### JSON Response (API)

```json
{
  "request_id": "pred_20260104_123456",
  "cluster": "production",
  "namespace": "default",
  "deployment": "my-app",
  "analyzed_at": "2026-01-04T12:34:56Z",
  "duration_ms": 8543,
  
  "health_score": {
    "overall": 82,
    "category": "healthy",
    "breakdown": {
      "availability": 95,
      "performance": 78,
      "stability": 85,
      "efficiency": 70
    }
  },
  
  "raw_metrics": {
    "desired_replicas": 8,
    "available_replicas": 8,
    "current": {
      "cpu_usage_avg": 4.5,
      "memory_usage_avg": 12884901888
    },
    "node_metrics": {
      "vm_sizing": {
        "predominant_instance_type": "Standard_F4s_v2",
        "cpu_per_vm_cores": 4,
        "memory_per_vm_gb": 7,
        "min_nodes": 2,
        "max_nodes": 10,
        "current_nodes": 5
      }
    },
    "capacity_forecast": {
      "growth_analysis": {
        "target_app": {
          "replicas": 8,
          "usage": {
            "cpu_cores": 4.5,
            "memory_gb": 12.0
          }
        },
        "max_replicas_current_nodes": 15,
        "max_replicas_with_max_nodes": 45,
        "recommended_max_replicas": 15,
        "growth_recommendation": "Pode escalar até 15 réplicas nos nodes atuais",
        "bottleneck_resource": "memory"
      }
    }
  },
  
  "recommendations": [
    {
      "priority": 1,
      "title": "Reduzir memory requests em 30%",
      "category": "cost-optimization",
      "implementation_estimate": {
        "time_required": "10 minutes",
        "complexity": "low",
        "resource_efficiency_gain_percent": 30.0
      }
    }
  ]
}
```

---

## 🔮 ROADMAP FUTURO

### Planejado para Próximas Versões

1. **Machine Learning Avançado**
   - Modelos específicos por tipo de workload
   - Auto-tuning de thresholds
   - Anomaly detection

2. **Ações Automatizadas**
   - Apply recommendations com 1 clique
   - Auto-scaling baseado em previsões
   - Rollback automático se falhar

3. **Comparação de Análises**
   - Diff entre análises
   - Evolução temporal
   - Effectiveness tracking de recomendações

4. **Alertas Proativos**
   - Integração com Alertmanager
   - Notificações por email/slack
   - Webhooks customizáveis

5. **Multi-cluster Insights**
   - Análise comparativa entre clusters
   - Recomendações de migração
   - Cost optimization cross-cluster

6. **Dashboard de Analytics**
   - Métricas agregadas
   - Trends organizacionais
   - ROI de otimizações

---

## 📝 CONCLUSÃO

A funcionalidade de Análise Preditiva está **100% completa** e **pronta para produção**, oferecendo:

- ✅ Análise profunda baseada em dados reais do Prometheus
- ✅ Previsões inteligentes com IA
- ✅ Cálculo realista de capacidade para crescimento
- ✅ Recomendações acionáveis priorizadas
- ✅ Relatórios profissionais (MD/PDF)
- ✅ Interface intuitiva e completa
- ✅ Histórico persistente com busca avançada
- ✅ Queries otimizadas e bugs corrigidos
- ✅ Documentação completa

**Status**: ✅ **PRODUÇÃO READY**

---

**Desenvolvido por**: Paulo Ribeiro  
**Data**: Janeiro 2026  
**Versão do Documento**: 1.0
