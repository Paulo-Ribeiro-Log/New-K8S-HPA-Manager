# Plano: Coleta de Métricas Prometheus para Baseline Histórico

**Data:** 06 de novembro de 2025
**Status:** 📋 Planejamento

---

## 🎯 Objetivo

Usar o sistema de queries Prometheus existente (`internal/monitoring/prometheus/queries.go`) para coletar **TODAS** as métricas necessárias para baseline histórico completo.

---

## ✅ Sistema Existente (Descoberto)

O projeto **JÁ TEM** um sistema completo de queries em `prometheus/queries.go`:

### Queries Disponíveis (16 templates):

| Categoria | Query | Descrição |
|-----------|-------|-----------|
| **CPU** | `CPUUsageQuery` | CPU usage % relative to requests |
| | `CPUUsageRawQuery` | Raw CPU usage in cores |
| | `CPUThrottlingQuery` | CPU throttling % |
| **Memory** | `MemoryUsageQuery` | Memory usage % relative to requests |
| | `MemoryUsageRawQuery` | Raw memory usage in bytes |
| | `MemoryOOMQuery` | OOM kills count |
| **HPA** | `HPACurrentReplicasQuery` | Current replicas |
| | `HPADesiredReplicasQuery` | Desired replicas |
| | `HPAReplicaDeltaQuery` | Delta between desired and current |
| **Network** | `NetworkRxBytesQuery` | Network received bytes/s |
| | `NetworkTxBytesQuery` | Network transmitted bytes/s |
| **Application** | `RequestRateQuery` | HTTP requests per second |
| | `ErrorRateQuery` | HTTP error rate % (5xx) |
| | **`P95LatencyQuery`** | **P95 latency in ms** ⭐ |
| | **`P99LatencyQuery`** | **P99 latency in ms** ⭐ |
| **Pod** | `PodRestartCountQuery` | Pod restarts in last 5min |
| | `PodReadyCountQuery` | Number of ready pods |

### QueryBuilder System:

```go
// Exemplo de uso
query, err := prometheus.NewQueryBuilder(prometheus.P95LatencyQuery).
    WithNamespace("production").
    WithService("api-gateway").
    Build()

// Resultado:
// "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{namespace=\"production\",service=\"api-gateway\"}[5m])) by (le)) * 1000"
```

---

## 🔧 Refatoração do HistoricalCollector

### Problema Atual:

O `historical.go` criado tem apenas **3 queries hardcoded**:
- ❌ CPU básico
- ❌ Memory básico
- ❌ Replicas

### Solução: Usar Sistema Existente

**Vantagens:**
1. ✅ **16 queries prontas** (não precisamos criar)
2. ✅ **P95/P99 latency** já implementado
3. ✅ **QueryBuilder** reutilizável
4. ✅ **Consistência** com resto do projeto
5. ✅ **Manutenibilidade** - queries em um lugar só

---

## 📝 Implementação Proposta

### 1. Refatorar `CollectBaseline()` para usar `GetAllTemplates()`

```go
func (hc *HistoricalCollector) CollectBaseline(
    ctx context.Context,
    cluster, namespace, hpaName, serviceName string,
) error {

    // Busca TODOS os templates de queries disponíveis
    templates := prometheus.GetAllTemplates()

    // Coleta histórico para CADA métrica
    allMetrics := make(map[string]map[time.Time]float64)

    for _, template := range templates {
        // Ignora queries que não se aplicam ao HPA
        if !hc.isApplicable(template, serviceName) {
            continue
        }

        // Build query usando QueryBuilder
        query, err := hc.buildQuery(template, namespace, hpaName, serviceName)
        if err != nil {
            log.Warn().Err(err).Str("query", template.Name).Msg("Skipping query")
            continue
        }

        // Coleta range data
        data, err := hc.queryRange(ctx, query, template.Name, start, end, step)
        if err != nil {
            log.Error().Err(err).Str("query", template.Name).Msg("Query failed")
            continue
        }

        allMetrics[template.Name] = data
    }

    // Mescla TODAS as métricas em snapshots
    snapshots := hc.mergeAllMetrics(cluster, namespace, hpaName, allMetrics)

    // Salva no SQLite
    return hc.storage.SaveHistoricalBaseline(snapshots)
}
```

### 2. Adicionar `isApplicable()` para filtrar queries

```go
func (hc *HistoricalCollector) isApplicable(template prometheus.QueryTemplate, serviceName string) bool {
    // Queries que requerem service name
    applicationQueries := []string{
        "request_rate",
        "error_rate",
        "p95_latency",
        "p99_latency",
    }

    // Se query precisa de service mas não foi fornecido, skip
    for _, appQuery := range applicationQueries {
        if template.Name == appQuery && serviceName == "" {
            return false
        }
    }

    return true
}
```

### 3. Schema SQLite Expandido

**Problema:** `hpa_snapshots` table atual não tem campos para todas as métricas.

**Solução:** Usar JSON blob para métricas adicionais:

```sql
ALTER TABLE hpa_snapshots ADD COLUMN metrics_json TEXT;

-- Exemplo de dados JSON:
{
  "cpu_throttling": 5.2,
  "memory_oom": 0,
  "network_rx_bytes": 1024000,
  "network_tx_bytes": 512000,
  "request_rate": 150.5,
  "error_rate": 0.5,
  "p95_latency": 250.0,
  "p99_latency": 500.0,
  "pod_restart_count": 0,
  "pod_ready_count": 3
}
```

**Vantagens:**
- ✅ Não precisa alterar schema para cada nova métrica
- ✅ Flexível para adicionar novas métricas
- ✅ SQLite tem bom suporte a JSON (`json_extract()`)

---

## 🚀 Implementação em 3 Etapas

### Etapa 1: Refatorar `historical.go` ✅ PRONTO PARA IMPLEMENTAR

```go
// Trocar queries hardcoded por sistema de templates
import "k8s-hpa-manager/internal/monitoring/prometheus"

func (hc *HistoricalCollector) CollectBaseline(...) {
    templates := prometheus.GetAllTemplates()
    // ... usar templates ...
}
```

### Etapa 2: Expandir Schema SQLite

```sql
-- Migration
ALTER TABLE hpa_snapshots ADD COLUMN metrics_json TEXT;
```

```go
// Persistence method atualizado
func (p *Persistence) SaveHistoricalBaseline(snapshots) {
    // ...
    metricsJSON, _ := json.Marshal(snapshot.AdditionalMetrics)
    stmt.Exec(..., string(metricsJSON))
}
```

### Etapa 3: API para Queries Customizadas (Futuro)

Permitir usuário adicionar queries customizadas via config:

```yaml
# ~/.k8s-hpa-manager/custom-queries.yaml
queries:
  - name: custom_business_metric
    query: "sum(rate(business_transactions_total{...}[5m]))"
    variables: [namespace, service]
```

---

## 📊 Métricas Essenciais vs Opcionais

### Essenciais (Core):
- ✅ CPU Usage
- ✅ Memory Usage
- ✅ Current/Desired Replicas
- ✅ HPA Delta

### Importantes (Aplicações Web):
- ✅ Request Rate
- ✅ Error Rate
- ⭐ **P95 Latency** (critical para SLOs)
- ⭐ **P99 Latency** (tail latency)

### Opcionais (Debug):
- CPU Throttling
- Memory OOM
- Network TX/RX
- Pod Restarts
- Pod Ready Count

---

## ⚠️ Considerações

### 1. Service Name Required

Queries de aplicação (request_rate, p95/p99 latency) requerem `service` name:

```go
// Precisa mapear HPA → Service
hpaName := "api-gateway-hpa"
serviceName := "api-gateway"  // Como descobrir automaticamente?
```

**Soluções:**
- Inferir via labels do HPA (`hpa.spec.scaleTargetRef.name`)
- Configuração manual em `monitoring-targets.json`
- Descobrir via K8s API (listar services no namespace)

### 2. Histograms Podem Não Existir

P95/P99 dependem de instrumentação da aplicação:

```promql
# Requer que aplicação exporte histogram metrics
http_request_duration_seconds_bucket
```

Se não existir, query retorna vazio → não é erro, apenas skip.

### 3. Performance

16 queries × 3 dias × step 5min = muitas requisições.

**Mitigação:**
- Executar queries em paralelo (goroutines)
- Cache de resultados intermediários
- Timeout individual por query (30s)

---

## 🎯 Próximo Passo Recomendado

**Refatorar `historical.go` para usar sistema existente:**

1. Import `prometheus` package
2. Substituir 3 queries hardcoded por `GetAllTemplates()`
3. Adicionar `metrics_json` field no schema
4. Testar com 1 HPA primeiro

**Benefício imediato:**
- 3 queries → 16 queries (5x mais dados!)
- **P95/P99 latency** disponível para análise
- Baseline muito mais rico para detecção de anomalias

---

**Quer que eu implemente essa refatoração agora?**
