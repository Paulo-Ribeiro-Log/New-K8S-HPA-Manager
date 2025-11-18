# Monitor Package

Unified collector that integrates K8s API, Prometheus, Storage, and Analyzer for HPA monitoring.

## 📋 Components

### 1. K8sClient (`k8s_client.go`)
Wrapper for Kubernetes client-go with cluster context.

**Features:**
- Multi-cluster support via kubeconfig contexts
- HPA listing and snapshot collection
- Deployment resource extraction
- Namespace filtering
- Connection testing

**Usage:**
```go
cluster := &models.ClusterInfo{
    Name:    "production",
    Context: "prod-cluster",
    Server:  "https://api.prod.k8s.io",
}

k8sClient, err := monitor.NewK8sClient(cluster)
if err != nil {
    log.Fatal(err)
}

// Test connection
ctx := context.Background()
if err := k8sClient.TestConnection(ctx); err != nil {
    log.Fatal(err)
}

// List HPAs
hpas, err := k8sClient.ListHPAs(ctx, "default")
for _, hpa := range hpas {
    snapshot, err := k8sClient.CollectHPASnapshot(ctx, &hpa)
    // ... use snapshot
}
```

### 2. Collector (`collector.go`) ✅

Unified collector that orchestrates K8s + Prometheus + Analyzer.

**Features:**
- Automatic HPA discovery across namespaces
- Prometheus enrichment (optional)
- Time-series cache integration
- Anomaly detection
- Monitoring loop with configurable interval
- Non-blocking result channel

**Architecture:**
```
Collector
├─ K8sClient: Collects HPA state from K8s API
├─ PrometheusClient: Enriches with metrics (optional)
├─ TimeSeriesCache: Stores snapshots in-memory
└─ Detector: Analyzes and detects anomalies
```

**Usage:**
```go
cluster := &models.ClusterInfo{
    Name:    "production",
    Context: "prod-cluster",
    Server:  "https://api.prod.k8s.io",
}

config := monitor.DefaultCollectorConfig()
config.ScanInterval = 30 * time.Second
config.EnablePrometheus = true
config.ExcludeNamespaces = []string{"monitoring", "logging"}

// Create collector
collector, err := monitor.NewCollector(
    cluster,
    "http://prometheus.monitoring.svc:9090", // Prometheus endpoint
    config,
)
if err != nil {
    log.Fatal(err)
}

// Single scan
ctx := context.Background()
result, err := collector.Scan(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Snapshots: %d\n", result.SnapshotsCount)
fmt.Printf("Anomalies: %d\n", len(result.Anomalies))

// Continuous monitoring
resultChan := make(chan *monitor.ScanResult, 10)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go collector.StartMonitoring(ctx, resultChan)

for result := range resultChan {
    fmt.Printf("[%s] Snapshots: %d, Anomalies: %d\n",
        result.Timestamp.Format(time.RFC3339),
        result.SnapshotsCount,
        len(result.Anomalies),
    )

    for _, anomaly := range result.Anomalies {
        fmt.Printf("  - %s: %s\n", anomaly.Type, anomaly.Message)
    }
}
```

## 🔄 Monitoring Flow

```
┌─────────────────────────────────────────────────────────────┐
│               Collector.StartMonitoring()                    │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Every 30s (configurable):                                  │
│                                                              │
│  1. List Namespaces (exclude system namespaces)            │
│     ├─ kube-system, kube-public, etc skipped               │
│     └─ Custom excludes from config                         │
│                                                              │
│  2. For each namespace:                                     │
│     ├─ List HPAs via K8s API                               │
│     │                                                        │
│     └─ For each HPA:                                        │
│        ├─ Collect HPA snapshot from K8s                    │
│        │  ├─ HPA config (min/max replicas)                 │
│        │  ├─ Current state (replicas, ready)               │
│        │  └─ Deployment resources (CPU/Memory)             │
│        │                                                     │
│        ├─ Enrich with Prometheus (if available)            │
│        │  ├─ CPU/Memory current usage                      │
│        │  ├─ Historical data (5min)                        │
│        │  └─ Extended metrics (errors, latency)            │
│        │                                                     │
│        └─ Add snapshot to TimeSeriesCache                  │
│                                                              │
│  3. Detect Anomalies                                        │
│     ├─ Analyzer.Detect() uses cache                        │
│     ├─ Checks 5 critical anomalies (Phase 1 MVP)          │
│     └─ Returns detected anomalies                          │
│                                                              │
│  4. Send ScanResult to channel                             │
│     ├─ Snapshots count                                     │
│     ├─ Anomalies detected                                  │
│     ├─ Errors encountered                                  │
│     └─ Scan duration                                        │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## 🧪 Testing

### Unit Tests (short mode)
```bash
go test ./internal/monitor/... -short -v
```

### Integration Tests (requires cluster)
```bash
go test ./internal/monitor/... -v
```

---

**Status:** ✅ Phase 2 Complete
**Next:** TUI implementation (Phase 3)
