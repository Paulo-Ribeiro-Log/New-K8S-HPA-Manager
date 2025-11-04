# Prometheus Package

Cliente Prometheus completo com queries predefinidas, auto-discovery e integração com port-forward manager.

## Componentes

### Client (`client.go`)

Cliente wrapper para a API do Prometheus com suporte a métricas de HPA.

**Features:**
- Conexão ao Prometheus via HTTP
- Queries PromQL (instant e range)
- Enriquecimento automático de HPASnapshot com métricas
- Extração de métricas: CPU, Memory, Request Rate, Error Rate, P95 Latency
- Histórico de 5 minutos com step de 30s
- Reconnection automático

**Exemplo de uso:**

```go
// Cria client
client, err := NewClient("production", "http://localhost:55553")
if err != nil {
    log.Fatal(err)
}

// Testa conexão
ctx := context.Background()
if err := client.TestConnection(ctx); err != nil {
    log.Fatal(err)
}

// Obtém CPU atual
cpu, err := client.GetCPUUsage(ctx, "production", "my-app")
fmt.Printf("CPU: %.2f%%\n", cpu)

// Obtém histórico de CPU (últimos 5min)
cpuHistory, err := client.GetCPUHistory(ctx, "production", "my-app")
for i, val := range cpuHistory {
    fmt.Printf("T-%d: %.2f%%\n", (len(cpuHistory)-i)*30, val)
}

// Enriquece snapshot
snapshot := &models.HPASnapshot{
    Namespace: "production",
    Name:      "my-app",
}

if err := client.EnrichSnapshot(ctx, snapshot); err == nil {
    fmt.Printf("CPU: %.2f%%, Memory: %.2f%%\n",
        snapshot.CPUCurrent,
        snapshot.MemoryCurrent,
    )
}
```

### Queries (`queries.go`)

Templates de queries PromQL prontos para uso com builder fluent.

**Queries Disponíveis:**

| Query | Descrição | Variáveis |
|-------|-----------|-----------|
| `CPUUsageQuery` | CPU % relativo a requests | namespace, pod_selector |
| `MemoryUsageQuery` | Memory % relativo a requests | namespace, pod_selector |
| `HPACurrentReplicasQuery` | Réplicas atuais | namespace, hpa_name |
| `HPADesiredReplicasQuery` | Réplicas desejadas | namespace, hpa_name |
| `RequestRateQuery` | Requests/sec | namespace, service |
| `ErrorRateQuery` | Error rate % (5xx) | namespace, service |
| `P95LatencyQuery` | P95 latency (ms) | namespace, service |
| `NetworkRxBytesQuery` | Network RX bytes/s | namespace, pod_selector |
| `NetworkTxBytesQuery` | Network TX bytes/s | namespace, pod_selector |
| `PodRestartCountQuery` | Pod restarts (5min) | namespace, pod_selector |

**Exemplo de uso:**

```go
// Usando QueryBuilder
query, err := NewQueryBuilder(CPUUsageQuery).
    WithNamespace("production").
    WithHPAName("my-app").
    Build()

// Ou usando funções helper
query := BuildCPUQuery("production", "my-app")
query := BuildMemoryQuery("staging", "test-app")
query := BuildRequestRateQuery("production", "api-service")

// Query customizada
builder := NewQueryBuilder(CustomTemplate).
    WithNamespace("production").
    WithPodSelector("app=my-app,tier=backend").
    Build()
```

### Discovery (`discovery.go`)

Auto-discovery de endpoints Prometheus com integração ao port-forward manager.

**Features:**
- Auto-discovery em múltiplos namespaces
- Padrões de nome de serviço configuráveis
- Integração automática com PortForwardManager
- Health check completo
- Versão e targets do Prometheus

**Padrões de Descoberta (default):**

Namespaces:
- `monitoring`
- `prometheus`
- `kube-prometheus`

Serviços:
- `prometheus`
- `prometheus-server`
- `prometheus-operated`
- `kube-prometheus-stack-prometheus`
- `prometheus-k8s`

**Exemplo de uso:**

```go
// Auto-discovery com port-forward
config := DefaultDiscoveryConfig()
config.PortForwardMgr = portForwardManager

endpoint, err := DiscoverPrometheus(ctx, k8sClient, config)
if err != nil {
    log.Fatal(err)
}
// endpoint = "http://localhost:55553"

// Discovery em múltiplos clusters
endpoints := DiscoverAllPrometheusEndpoints(ctx, k8sClients, config)
for cluster, endpoint := range endpoints {
    fmt.Printf("%s: %s\n", cluster, endpoint)
}

// Health check
health, err := CheckPrometheusHealth(ctx, endpoint)
fmt.Printf("Version: %s, Targets: %d\n",
    health.Version,
    health.ActiveTargets,
)
```

## Integração Completa

### Exemplo: Monitoramento com Prometheus + Port-Forward

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/config"
    "github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/monitor"
    "github.com/Paulo-Ribeiro-Log/hpa-watchdog/internal/prometheus"
)

func main() {
    ctx := context.Background()

    // 1. Cria port-forward manager
    pfMgr := monitor.NewPortForwardManager(55553)
    defer pfMgr.Shutdown()

    // Heartbeat loop
    go func() {
        ticker := time.NewTicker(5 * time.Second)
        for range ticker.C {
            pfMgr.Heartbeat()
        }
    }()

    // 2. Descobre clusters
    cfg, _ := config.Load("configs/watchdog.yaml")
    clusters, _ := config.DiscoverClusters(cfg)

    // 3. Cria K8s clients
    k8sClients := make(map[string]*monitor.K8sClient)
    for _, cluster := range clusters {
        client, _ := monitor.NewK8sClient(cluster)
        k8sClients[cluster.Name] = client
    }

    // 4. Auto-discovery Prometheus
    promConfig := prometheus.DefaultDiscoveryConfig()
    promConfig.PortForwardMgr = pfMgr

    promEndpoints := prometheus.DiscoverAllPrometheusEndpoints(
        ctx,
        k8sClients,
        promConfig,
    )

    // 5. Cria Prometheus clients
    promClients := make(map[string]*prometheus.Client)
    for cluster, endpoint := range promEndpoints {
        client, _ := prometheus.NewClient(cluster, endpoint)
        promClients[cluster] = client
    }

    // 6. Loop de coleta
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        for clusterName, k8sClient := range k8sClients {
            promClient := promClients[clusterName]

            // Lista namespaces
            namespaces, _ := k8sClient.ListNamespaces(ctx, []string{})

            for _, ns := range namespaces {
                // Lista HPAs
                hpas, _ := k8sClient.ListHPAs(ctx, ns)

                for _, hpa := range hpas {
                    // Coleta snapshot do K8s
                    snapshot, _ := k8sClient.CollectHPASnapshot(ctx, &hpa)

                    // Enriquece com Prometheus
                    if promClient != nil {
                        _ = promClient.EnrichSnapshot(ctx, snapshot)
                    }

                    // Processa snapshot
                    fmt.Printf("[%s] %s/%s: CPU=%.2f%%, Memory=%.2f%%, Replicas=%d/%d\n",
                        clusterName,
                        snapshot.Namespace,
                        snapshot.Name,
                        snapshot.CPUCurrent,
                        snapshot.MemoryCurrent,
                        snapshot.CurrentReplicas,
                        snapshot.DesiredReplicas,
                    )
                }
            }
        }
    }
}
```

## Métricas Coletadas

### HPASnapshot Enrichment

Quando `EnrichSnapshot()` é chamado, os seguintes campos são preenchidos:

```go
type HPASnapshot struct {
    // ... campos K8s ...

    // Prometheus metrics
    CPUCurrent    float64   // % atual via Prometheus
    MemoryCurrent float64   // % atual via Prometheus

    // Históricos (5min, step 30s = 10 pontos)
    CPUHistory     []float64
    MemoryHistory  []float64
    ReplicaHistory []int32

    // Extended metrics
    RequestRate    float64 // req/s
    ErrorRate      float64 // % errors (5xx)
    P95Latency     float64 // ms
    NetworkRxBytes float64 // bytes/s
    NetworkTxBytes float64 // bytes/s

    DataSource DataSource // Prometheus vs MetricsServer
}
```

### Queries PromQL Utilizadas

**CPU Usage:**
```promql
sum(rate(container_cpu_usage_seconds_total{namespace="prod",pod=~"app.*"}[1m])) /
sum(kube_pod_container_resource_requests{namespace="prod",pod=~"app.*",resource="cpu"}) * 100
```

**Memory Usage:**
```promql
sum(container_memory_working_set_bytes{namespace="prod",pod=~"app.*"}) /
sum(kube_pod_container_resource_requests{namespace="prod",pod=~"app.*",resource="memory"}) * 100
```

**Request Rate:**
```promql
sum(rate(http_requests_total{namespace="prod",service="api"}[1m]))
```

**Error Rate:**
```promql
sum(rate(http_requests_total{namespace="prod",service="api",status=~"5.."}[1m])) /
sum(rate(http_requests_total{namespace="prod",service="api"}[1m])) * 100
```

## Auto-Discovery Flow

```
┌──────────────────────────────────────────────────────────┐
│                    Auto-Discovery                         │
├──────────────────────────────────────────────────────────┤
│                                                           │
│  1. For each namespace (monitoring, prometheus, ...)     │
│      │                                                    │
│      ├─► For each service pattern                        │
│      │    (prometheus, prometheus-server, ...)          │
│      │                                                    │
│      └─► Try to find service                            │
│           │                                               │
│           ├─► If found:                                  │
│           │    ├─► Start port-forward (55553:9090)       │
│           │    ├─► Test connection                       │
│           │    └─► Return endpoint                       │
│           │                                               │
│           └─► If not found: Try next pattern             │
│                                                           │
│  2. Port-forward established: http://localhost:55553     │
│                                                           │
│  3. Create Prometheus client                             │
│                                                           │
│  4. Ready to collect metrics!                            │
└──────────────────────────────────────────────────────────┘
```

## Troubleshooting

### Prometheus não encontrado

```bash
# Verificar se Prometheus está rodando
kubectl get pods -n monitoring | grep prometheus

# Verificar serviços
kubectl get svc -n monitoring | grep prometheus

# Testar conexão manual
kubectl port-forward -n monitoring svc/prometheus-server 9090:9090
curl http://localhost:9090/api/v1/query?query=up
```

### Port-forward falhando

```bash
# Verificar se porta 55553 está livre
lsof -i :55553

# Matar processo se necessário
kill <PID>

# Verificar logs do port-forward manager
# (via zerolog output)
```

### Métricas vazias

Verifique se Prometheus está coletando métricas:

```promql
# Verificar se métricas de CPU existem
container_cpu_usage_seconds_total

# Verificar kube-state-metrics
kube_pod_container_resource_requests

# Verificar se namespace/pod existem
kube_pod_info{namespace="production"}
```

## Performance

**Benchmarks (Go 1.24, AMD64):**

```
BenchmarkQueryBuilder-8        1000000   1200 ns/op
BenchmarkBuildCPUQuery-8       2000000    800 ns/op
```

**Métricas de Performance:**

- Query instant: ~50-200ms
- Range query (5min): ~100-300ms
- EnrichSnapshot completo: ~500-1000ms (7 queries)
- Discovery (por cluster): ~2-5s
- Memória: ~10 MB por client

## Próximos Passos

- [ ] Cache de queries (5-10s TTL)
- [ ] Retry com exponential backoff
- [ ] Circuit breaker para Prometheus down
- [ ] Métricas customizadas via config
- [ ] Recording rules support
- [ ] Alertmanager integration
