# HPA Watchdog - Análise de Integração com Prometheus

## 📊 Comparação: K8s API vs Prometheus

### Abordagem Original (K8s API apenas)

```
Watchdog → K8s API → Metrics Server → Pods
         ↓
    HPASnapshot (cada 30s)
```

**Vantagens:**
- ✅ Acesso direto aos dados do cluster
- ✅ Sem dependência externa (metrics-server já existe)
- ✅ Dados em tempo real

**Desvantagens:**
- ❌ Métricas limitadas (CPU/Memory básicas)
- ❌ Sem histórico nativo (precisa armazenar tudo)
- ❌ Performance impacto ao varrer muitos clusters
- ❌ Sem métricas customizadas
- ❌ Difícil fazer análise temporal complexa

---

### Abordagem com Prometheus (RECOMENDADA) ✅

```
Watchdog → Prometheus API → TSDB (histórico rico)
         ↓
    HPASnapshot + Extended Metrics
```

**Vantagens:**
- ✅ **Histórico nativo** - Prometheus já armazena métricas (retention configurável)
- ✅ **Métricas ricas** - CPU, Memory, Network, Latency, Custom Metrics
- ✅ **Performance** - Queries otimizadas (PromQL)
- ✅ **Análise temporal** - Range queries fáceis (`[5m]`, `rate()`, `increase()`)
- ✅ **Alerting nativo** - Pode usar regras do Prometheus como base
- ✅ **Escalável** - Prometheus já lida com muitos targets
- ✅ **Correlação** - Cruzar dados de HPA com outras métricas (requests, errors)

**Desvantagens:**
- ⚠️ Dependência do Prometheus instalado no cluster
- ⚠️ Configuração adicional (endpoints Prometheus)
- ⚠️ Possível lag de scraping (default: 15s)

---

## 🏆 Decisão: Arquitetura Híbrida

### Melhor dos Dois Mundos

```
┌─────────────────────────────────────────────────────────────────┐
│                        HPA Watchdog                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────┐              ┌──────────────────┐         │
│  │  K8s API        │              │  Prometheus API  │         │
│  │  (Config Data)  │              │  (Metrics Data)  │         │
│  └────────┬────────┘              └────────┬─────────┘         │
│           │                                 │                   │
│           ├─────────────┐  ┌───────────────┤                   │
│           ▼             ▼  ▼               ▼                   │
│  ┌────────────────────────────────────────────────┐           │
│  │          Unified Collector                      │           │
│  │  - HPA config (K8s)                            │           │
│  │  - Current/Desired replicas (K8s)              │           │
│  │  - Min/Max replicas (K8s)                      │           │
│  │  - CPU/Memory metrics (Prometheus)             │           │
│  │  - Request rate (Prometheus)                   │           │
│  │  - Error rate (Prometheus)                     │           │
│  │  - P95 latency (Prometheus)                    │           │
│  └─────────────────┬──────────────────────────────┘           │
│                    ▼                                            │
│         ┌──────────────────────┐                               │
│         │  Enhanced Analyzer   │                               │
│         │  - Temporal analysis │                               │
│         │  - Correlation       │                               │
│         │  - Prediction        │                               │
│         └──────────┬───────────┘                               │
│                    ▼                                            │
│              ┌──────────┐                                       │
│              │  Alerts  │                                       │
│              └──────────┘                                       │
└─────────────────────────────────────────────────────────────────┘
```

### O Que Vem de Onde

#### K8s API (kubernetes client-go)
**Usado para:** Dados de configuração e estado
```
✅ HPA config (min/max replicas, targets)
✅ Current/Desired replicas
✅ Deployment info
✅ Pod status (Ready/NotReady)
✅ Events (HPA scaling events)
✅ Resource requests/limits
```

#### Prometheus API (prometheus/client_golang)
**Usado para:** Métricas e análise temporal
```
✅ CPU usage (real-time + histórico)
✅ Memory usage (real-time + histórico)
✅ Request rate (QPS)
✅ Error rate (5xx, 4xx)
✅ Response latency (P50, P95, P99)
✅ Network I/O
✅ Custom application metrics
✅ Histórico de scaling (kube_hpa_status_current_replicas[5m])
```

---

## 🔧 Queries Prometheus Úteis

### 1. CPU Usage (HPA Target)
```promql
# CPU atual dos pods do HPA
sum(rate(container_cpu_usage_seconds_total{
    namespace="api-gateway",
    pod=~"nginx-.*"
}[1m])) /
sum(kube_pod_container_resource_requests{
    namespace="api-gateway",
    pod=~"nginx-.*",
    resource="cpu"
}) * 100

# Histórico de 5 minutos
sum(rate(container_cpu_usage_seconds_total{...}[5m]))
```

### 2. Memory Usage (HPA Target)
```promql
# Memory atual
sum(container_memory_working_set_bytes{
    namespace="api-gateway",
    pod=~"nginx-.*"
}) /
sum(kube_pod_container_resource_requests{
    namespace="api-gateway",
    pod=~"nginx-.*",
    resource="memory"
}) * 100
```

### 3. Current Replicas (Historical)
```promql
# Réplicas atuais (últimos 5 min)
kube_horizontalpodautoscaler_status_current_replicas{
    namespace="api-gateway",
    horizontalpodautoscaler="nginx"
}[5m]

# Réplicas desejadas
kube_horizontalpodautoscaler_status_desired_replicas{...}
```

### 4. Request Rate (Contexto)
```promql
# Requests por segundo
sum(rate(http_requests_total{
    namespace="api-gateway",
    service="nginx"
}[1m]))
```

### 5. Error Rate (Correlação)
```promql
# Taxa de erro (%)
sum(rate(http_requests_total{
    namespace="api-gateway",
    status=~"5.."
}[1m])) /
sum(rate(http_requests_total{
    namespace="api-gateway"
}[1m])) * 100
```

### 6. P95 Latency (Correlação)
```promql
histogram_quantile(0.95,
    sum(rate(http_request_duration_seconds_bucket{
        namespace="api-gateway"
    }[1m])) by (le)
)
```

---

## 📊 Enhanced Data Model com Prometheus

### HPASnapshot Estendido

```go
// HPASnapshot com métricas Prometheus
type HPASnapshot struct {
    Timestamp       time.Time
    Cluster         string
    Namespace       string
    Name            string

    // === K8s API Data ===
    // HPA Config
    MinReplicas     int32
    MaxReplicas     int32
    CurrentReplicas int32
    DesiredReplicas int32

    // Targets
    CPUTarget       int32  // % (ex: 70)
    MemoryTarget    int32  // % (ex: 80)

    // === Prometheus Metrics ===
    // Current Metrics (real-time)
    CPUCurrent      float64 // % atual (Prometheus)
    MemoryCurrent   float64 // % atual (Prometheus)

    // Historical Metrics (5 min)
    CPUHistory      []float64 // CPU últimos 5 min (1 ponto/30s)
    MemoryHistory   []float64 // Memory últimos 5 min

    // Replica History (Prometheus)
    ReplicaHistory  []int32   // Réplicas últimos 5 min

    // Extended Metrics (Prometheus)
    RequestRate     float64   // Requests/sec
    ErrorRate       float64   // % errors (5xx)
    P95Latency      float64   // P95 latency (ms)
    NetworkRxBytes  float64   // Network RX (bytes/s)
    NetworkTxBytes  float64   // Network TX (bytes/s)

    // Deployment Resources (K8s API)
    CPURequest      string // Ex: "500m"
    CPULimit        string // Ex: "1000m"
    MemoryRequest   string // Ex: "512Mi"
    MemoryLimit     string // Ex: "1Gi"

    // Status (K8s API)
    Ready           bool
    ScalingActive   bool
    LastScaleTime   *time.Time
}
```

---

## 🔌 Implementação: Prometheus Client

### Dependências

```go
import (
    "github.com/prometheus/client_golang/api"
    promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
    "github.com/prometheus/common/model"
)
```

### Prometheus Client Wrapper

```go
package prometheus

import (
    "context"
    "fmt"
    "time"

    "github.com/prometheus/client_golang/api"
    promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
    "github.com/prometheus/common/model"
)

type Client struct {
    api    promv1.API
    cluster string
}

func NewClient(prometheusURL, clusterName string) (*Client, error) {
    client, err := api.NewClient(api.Config{
        Address: prometheusURL,
    })
    if err != nil {
        return nil, err
    }

    return &Client{
        api:    promv1.NewAPI(client),
        cluster: clusterName,
    }, nil
}

// GetCPUUsage retorna CPU usage atual e histórico (5 min)
func (c *Client) GetCPUUsage(ctx context.Context, namespace, hpaName string, podSelector string) (*MetricData, error) {
    // Query CPU atual (1 min rate)
    query := fmt.Sprintf(`
        sum(rate(container_cpu_usage_seconds_total{
            namespace="%s",
            pod=~"%s"
        }[1m])) /
        sum(kube_pod_container_resource_requests{
            namespace="%s",
            pod=~"%s",
            resource="cpu"
        }) * 100
    `, namespace, podSelector, namespace, podSelector)

    result, warnings, err := c.api.Query(ctx, query, time.Now())
    if err != nil {
        return nil, err
    }
    if len(warnings) > 0 {
        // Log warnings
    }

    // Parse result
    cpuCurrent := parseScalarResult(result)

    // Query histórico (5 min)
    rangeQuery := fmt.Sprintf(`
        sum(rate(container_cpu_usage_seconds_total{
            namespace="%s",
            pod=~"%s"
        }[1m]))
    `, namespace, podSelector)

    rangeResult, _, err := c.api.QueryRange(ctx, rangeQuery, promv1.Range{
        Start: time.Now().Add(-5 * time.Minute),
        End:   time.Now(),
        Step:  30 * time.Second, // 1 ponto a cada 30s = 10 pontos
    })
    if err != nil {
        return nil, err
    }

    cpuHistory := parseRangeResult(rangeResult)

    return &MetricData{
        Current: cpuCurrent,
        History: cpuHistory,
    }, nil
}

// GetReplicaHistory retorna histórico de réplicas (5 min)
func (c *Client) GetReplicaHistory(ctx context.Context, namespace, hpaName string) ([]int32, error) {
    query := fmt.Sprintf(`
        kube_horizontalpodautoscaler_status_current_replicas{
            namespace="%s",
            horizontalpodautoscaler="%s"
        }
    `, namespace, hpaName)

    result, _, err := c.api.QueryRange(ctx, query, promv1.Range{
        Start: time.Now().Add(-5 * time.Minute),
        End:   time.Now(),
        Step:  30 * time.Second,
    })
    if err != nil {
        return nil, err
    }

    return parseReplicaHistory(result), nil
}

// GetRequestRate retorna taxa de requests/sec
func (c *Client) GetRequestRate(ctx context.Context, namespace, service string) (float64, error) {
    query := fmt.Sprintf(`
        sum(rate(http_requests_total{
            namespace="%s",
            service="%s"
        }[1m]))
    `, namespace, service)

    result, _, err := c.api.Query(ctx, query, time.Now())
    if err != nil {
        return 0, err
    }

    return parseScalarResult(result), nil
}

// GetErrorRate retorna taxa de erros (%)
func (c *Client) GetErrorRate(ctx context.Context, namespace, service string) (float64, error) {
    query := fmt.Sprintf(`
        sum(rate(http_requests_total{
            namespace="%s",
            service="%s",
            status=~"5.."
        }[1m])) /
        sum(rate(http_requests_total{
            namespace="%s",
            service="%s"
        }[1m])) * 100
    `, namespace, service, namespace, service)

    result, _, err := c.api.Query(ctx, query, time.Now())
    if err != nil {
        return 0, err
    }

    return parseScalarResult(result), nil
}

// Helper functions
func parseScalarResult(result model.Value) float64 {
    if result.Type() == model.ValVector {
        vector := result.(model.Vector)
        if len(vector) > 0 {
            return float64(vector[0].Value)
        }
    }
    return 0
}

func parseRangeResult(result model.Value) []float64 {
    if result.Type() == model.ValMatrix {
        matrix := result.(model.Matrix)
        if len(matrix) > 0 {
            values := make([]float64, len(matrix[0].Values))
            for i, pair := range matrix[0].Values {
                values[i] = float64(pair.Value)
            }
            return values
        }
    }
    return []float64{}
}

func parseReplicaHistory(result model.Value) []int32 {
    floats := parseRangeResult(result)
    replicas := make([]int32, len(floats))
    for i, f := range floats {
        replicas[i] = int32(f)
    }
    return replicas
}
```

---

## ⚙️ Configuração com Prometheus

### watchdog.yaml (atualizado)

```yaml
# HPA Watchdog Configuration

monitoring:
  scan_interval_seconds: 30
  history_retention_minutes: 5

  # Prometheus Integration (NOVO)
  prometheus:
    enabled: true                     # Habilita integração Prometheus
    auto_discover: true               # Auto-descobre endpoints via K8s
    fallback_to_metrics_server: true # Fallback para metrics-server se Prometheus indisponível

    # Endpoints por cluster (opcional - se auto_discover=false)
    endpoints:
      akspriv-prod-east: "http://prometheus.monitoring.svc:9090"
      akspriv-prod-west: "http://prometheus.monitoring.svc:9090"
      akspriv-qa: "http://prometheus-qa.monitoring.svc:9090"

    # Queries customizadas (opcional - permite override)
    custom_queries:
      cpu_usage: |
        sum(rate(container_cpu_usage_seconds_total{
          namespace="{namespace}",
          pod=~"{pod_selector}"
        }[1m])) / sum(kube_pod_container_resource_requests{...}) * 100

    # Timeout para queries
    query_timeout_seconds: 10

clusters:
  config_path: "~/.k8s-hpa-manager/clusters-config.json"
  auto_discover: true

thresholds:
  # Replica changes
  replica_delta_percent: 50.0
  replica_delta_absolute: 5

  # CPU/Memory
  cpu_warning_percent: 85
  cpu_critical_percent: 90
  memory_warning_percent: 85
  memory_critical_percent: 90

  # Extended Thresholds (Prometheus)
  request_rate_spike_percent: 100.0  # Alerta se request rate dobrar
  error_rate_critical_percent: 5.0   # Alerta se >5% errors
  p95_latency_critical_ms: 1000      # Alerta se P95 >1s
```

---

## 🎨 Interface TUI Atualizada

### Dashboard com Métricas Prometheus

```
╔═ HPA: api-gateway/nginx ════════════════════════════════════════════╗
║                                                                      ║
║  Status: ✅ Healthy  |  Replicas: 8/10  |  CPU: 78%  |  Mem: 65%   ║
║                                                                      ║
║  ┌─ CPU Usage (5 min) ───────────┐  ┌─ Memory Usage (5 min) ─────┐║
║  │  100%│                         │  │  100%│                      │║
║  │   90%│         ╭──╮            │  │   80%│                      │║
║  │   80%│      ╭──╯  ╰╮           │  │   60%│   ╭────╮            │║
║  │   70%│   ╭──╯      ╰─          │  │   40%│╭──╯    ╰──╮         │║
║  │   60%├─────────────────────    │  │   20%├──────────────────   │║
║  │      14:30   14:32   14:35     │  │      14:30   14:32   14:35 │║
║  │  Target: 70%                   │  │  Target: 80%                │║
║  └─────────────────────────────────┘  └─────────────────────────────┘║
║                                                                      ║
║  ┌─ Replicas (5 min) ─────────────┐  ┌─ Request Rate ─────────────┐║
║  │   10│              ███          │  │  500 rps│        ╭╮        │║
║  │    8│           ███╱            │  │  400 rps│      ╭─╯╰─╮      │║
║  │    6│        ███╱               │  │  300 rps│   ╭──╯    ╰──╮   │║
║  │    4│     ███                   │  │  200 rps│╭──╯          ╰─  │║
║  │    2├──────────────────────     │  │  100 rps├─────────────────│║
║  │     14:30   14:32   14:35      │  │         14:30   14:32  14:35│║
║  └─────────────────────────────────┘  └─────────────────────────────┘║
║                                                                      ║
║  📊 Extended Metrics (Prometheus)                                   ║
║  ┌──────────────────────────────────────────────────────────────┐  ║
║  │  Request Rate:    425 req/s    (↑ 12% vs 5min ago)          │  ║
║  │  Error Rate:      0.8%         (✅ below 5% threshold)       │  ║
║  │  P95 Latency:     245ms        (✅ below 1000ms threshold)   │  ║
║  │  Network RX:      12.5 MB/s                                  │  ║
║  │  Network TX:      8.3 MB/s                                   │  ║
║  └──────────────────────────────────────────────────────────────┘  ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## 🚀 Vantagens da Arquitetura Híbrida

### 1. Detecção de Anomalias Melhorada

**Antes (só K8s API):**
```
❌ CPU spike: 95% (limite: 90%)
```

**Depois (com Prometheus):**
```
🔴 ANOMALIA DETECTADA
   CPU spike: 95% (limite: 90%)

   📊 Contexto (Prometheus):
   - Request rate: 850 req/s (↑300% vs baseline 200 req/s)
   - Error rate: 12% (↑ de 0.5%)
   - P95 latency: 2.5s (↑ de 150ms)

   💡 Diagnóstico: Traffic spike + errors = provável incident
   🚨 Ação sugerida: Verificar upstream services
```

### 2. Análise Temporal Rica

```go
// Detectar "scaling oscillation" (réplicas sobem e descem rapidamente)
func detectOscillation(replicaHistory []int32) bool {
    changes := 0
    for i := 1; i < len(replicaHistory); i++ {
        if replicaHistory[i] != replicaHistory[i-1] {
            changes++
        }
    }
    // Se réplicas mudaram >5x em 5 min = oscilação
    return changes > 5
}
```

### 3. Predição de Anomalias

```go
// Usar histórico Prometheus para prever scaling
func predictScaling(cpuHistory []float64, cpuTarget int32) bool {
    // Tendência: CPU crescendo consistentemente
    if len(cpuHistory) < 5 {
        return false
    }

    // Últimos 5 pontos em tendência de alta
    increasing := true
    for i := 1; i < 5; i++ {
        if cpuHistory[len(cpuHistory)-i] <= cpuHistory[len(cpuHistory)-i-1] {
            increasing = false
            break
        }
    }

    currentCPU := cpuHistory[len(cpuHistory)-1]

    // Se CPU subindo e próximo do target = scaling iminente
    if increasing && currentCPU > float64(cpuTarget)*0.9 {
        return true
    }

    return false
}
```

---

## 🎯 Decisão Final

### ✅ RECOMENDAÇÃO: Usar Prometheus como Fonte Principal

**Motivos:**
1. **Histórico nativo** - Não precisa armazenar tudo em memória
2. **Métricas ricas** - Muito além de CPU/Memory
3. **Análise avançada** - PromQL permite queries complexas
4. **Escalabilidade** - Prometheus já é otimizado para TSDB
5. **Contexto completo** - Correlacionar HPA com tráfego, errors, latency

**Fallback:**
- Se Prometheus não disponível → usar metrics-server (K8s API)
- Flag `fallback_to_metrics_server: true` na config

**Conclusão:**
A arquitetura híbrida (K8s API para config + Prometheus para métricas) oferece o melhor resultado para um watchdog robusto e inteligente! 🚀
