# HPA Watchdog - Especificação Técnica

## 📋 Visão Geral

**HPA Watchdog** é um vigilante autônomo para monitoramento de Horizontal Pod Autoscalers (HPAs) em clusters Kubernetes, desenvolvido em Go com interface TUI interativa (Bubble Tea + Lipgloss).

**Status**: 🟡 Planejamento
**Versão Planejada**: v1.0.0
**Última Atualização**: 23 de outubro de 2025

---

## 🎯 Objetivos

### Objetivo Principal
Monitorar continuamente múltiplos clusters Kubernetes, detectando anomalias em HPAs, deployments e recursos (CPU/Memory) com sistema de alertas configurável.

### Objetivos Específicos
1. **Monitoramento Multi-Cluster**: Varrer todos os clusters configurados em paralelo
2. **Comparação Temporal**: Armazenar histórico dos últimos 5 minutos para cada métrica
3. **Detecção de Anomalias**: Identificar mudanças abruptas baseadas em thresholds configuráveis
4. **Sistema de Alertas**: Notificações visuais (TUI) e logs estruturados
5. **Configuração Interativa**: Interface TUI para ajustar thresholds e parâmetros
6. **Autonomia Total**: Rodar independentemente do k8s-hpa-manager principal

---

## 🏗️ Arquitetura

### Visão Geral (Híbrida: K8s API + Prometheus)

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
│  │  • HPA config (K8s)                            │           │
│  │  • Current/Desired replicas (K8s)              │           │
│  │  • CPU/Memory metrics (Prometheus)             │           │
│  │  • Request rate, errors, latency (Prometheus)  │           │
│  └─────────────────┬──────────────────────────────┘           │
│                    ▼                                            │
│         ┌──────────────────────┐                               │
│         │  Enhanced Analyzer   │                               │
│         │  • Temporal analysis │                               │
│         │  • Correlation       │                               │
│         │  • Prediction        │                               │
│         └──────────┬───────────┘                               │
│                    ▼                                            │
│              ┌──────────┐                                       │
│              │  Alerts  │                                       │
│              └──────────┘                                       │
└─────────────────────────────────────────────────────────────────┘
```

**Fontes de Dados:**
- **K8s API** (kubernetes/client-go): HPA config, replicas, deployment info, events
- **Prometheus** (prometheus/client_golang): CPU/Memory metrics, request rate, errors, latency, histórico

### Estrutura do Projeto

```
hpa-watchdog/
├── cmd/
│   └── main.go                    # Entry point
├── internal/
│   ├── monitor/
│   │   ├── collector.go           # Unified collector (K8s + Prometheus)
│   │   ├── analyzer.go            # Análise de anomalias avançada
│   │   └── alerter.go             # Sistema de alertas
│   ├── prometheus/                # ⭐ NOVO
│   │   ├── client.go              # Prometheus API client wrapper
│   │   ├── queries.go             # PromQL queries predefinidas
│   │   └── discovery.go           # Auto-discovery de endpoints
│   ├── storage/
│   │   ├── timeseries.go          # Time-series cache (reduzido - Prometheus tem histórico)
│   │   └── persistence.go         # Persistência opcional (SQLite)
│   ├── config/
│   │   ├── loader.go              # Carrega configuração
│   │   ├── thresholds.go          # Gerenciamento de thresholds
│   │   └── clusters.go            # Descoberta de clusters
│   ├── tui/
│   │   ├── app.go                 # Main Bubble Tea app
│   │   ├── views.go               # Renderização de views
│   │   ├── handlers.go            # Event handlers
│   │   ├── components/
│   │   │   ├── dashboard.go      # Dashboard principal
│   │   │   ├── alerts_panel.go   # Painel de alertas
│   │   │   ├── metrics_chart.go  # Gráficos ASCII (dados Prometheus)
│   │   │   ├── config_modal.go   # Modal de configuração
│   │   │   └── cluster_list.go   # Lista de clusters
│   │   └── styles.go              # Estilos Lipgloss
│   └── models/
│       └── types.go               # Estruturas de dados
├── configs/
│   └── watchdog.yaml              # Configuração padrão
├── go.mod
├── go.sum
├── README.md
└── Makefile
```

---

## 📊 Modelo de Dados

### Core Data Structures

```go
// HPASnapshot representa o estado de um HPA em um momento específico
// ⭐ ESTENDIDO COM MÉTRICAS PROMETHEUS
type HPASnapshot struct {
    Timestamp       time.Time
    Cluster         string
    Namespace       string
    Name            string

    // === K8s API Data (Config & State) ===
    // HPA Config
    MinReplicas     int32
    MaxReplicas     int32
    CurrentReplicas int32
    DesiredReplicas int32

    // Targets
    CPUTarget       int32  // % (ex: 70)
    MemoryTarget    int32  // % (ex: 80)

    // Deployment Resources (K8s API)
    CPURequest      string // Ex: "500m"
    CPULimit        string // Ex: "1000m"
    MemoryRequest   string // Ex: "512Mi"
    MemoryLimit     string // Ex: "1Gi"

    // Status
    Ready           bool
    ScalingActive   bool
    LastScaleTime   *time.Time

    // === Prometheus Metrics (Real-time & Historical) ===
    // Current Metrics (Prometheus)
    CPUCurrent      float64 // % atual (mais preciso que K8s API)
    MemoryCurrent   float64 // % atual

    // Historical Metrics (5 min history from Prometheus)
    CPUHistory      []float64 // CPU últimos 5 min (1 ponto/30s = 10 pontos)
    MemoryHistory   []float64 // Memory últimos 5 min
    ReplicaHistory  []int32   // Réplicas últimos 5 min (de kube_hpa_status_current_replicas)

    // Extended Metrics (Prometheus - Optional)
    RequestRate     float64   // Requests/sec (de http_requests_total)
    ErrorRate       float64   // % errors (5xx de http_requests_total)
    P95Latency      float64   // P95 latency (ms) de http_request_duration_seconds
    NetworkRxBytes  float64   // Network RX (bytes/s)
    NetworkTxBytes  float64   // Network TX (bytes/s)

    // Metadata
    DataSource      DataSource // Indica se veio de Prometheus ou Metrics-Server
}

// DataSource indica a origem das métricas
type DataSource int

const (
    DataSourcePrometheus    DataSource = iota // Métricas do Prometheus (preferencial)
    DataSourceMetricsServer                   // Fallback: Kubernetes Metrics-Server
    DataSourceHybrid                          // Híbrido: alguns Prometheus, alguns K8s
)

// TimeSeriesData armazena histórico de 5 minutos
type TimeSeriesData struct {
    HPAKey      string // "cluster/namespace/name"
    Snapshots   []HPASnapshot
    MaxDuration time.Duration // 5 minutos

    sync.RWMutex
}

// Anomaly representa uma anomalia detectada
type Anomaly struct {
    ID          string
    Timestamp   time.Time
    Severity    AlertSeverity // Critical, Warning, Info
    Type        AnomalyType
    Cluster     string
    Namespace   string
    HPAName     string

    // Detalhes
    Message     string
    OldValue    interface{}
    NewValue    interface{}
    Delta       float64 // Variação percentual

    // Contexto
    Snapshot    HPASnapshot
    History     []HPASnapshot // Últimos N snapshots

    Acknowledged bool
    AckedAt      *time.Time
}

// AnomalyType define tipos de anomalias
type AnomalyType int

const (
    AnomalyReplicaSpike      AnomalyType = iota // Aumento abrupto de réplicas
    AnomalyReplicaDrop                          // Queda abrupta de réplicas
    AnomalyCPUSpike                             // CPU current > threshold
    AnomalyMemorySpike                          // Memory current > threshold
    AnomalyResourceChange                       // Mudança em requests/limits
    AnomalyHPAConfigChange                      // Mudança em min/max replicas
    AnomalyScalingStuck                         // HPA não consegue escalar
    AnomalyTargetMiss                           // Current muito acima/abaixo do target
)

// AlertSeverity define níveis de severidade
type AlertSeverity int

const (
    SeverityInfo     AlertSeverity = iota
    SeverityWarning
    SeverityCritical
)

// Thresholds define limites configuráveis
type Thresholds struct {
    // Replica changes
    ReplicaDeltaPercent     float64 // Ex: 50% = alerta se réplicas mudam >50%
    ReplicaDeltaAbsolute    int32   // Ex: 5 = alerta se réplicas mudam ±5

    // CPU/Memory
    CPUWarningPercent       int32   // Ex: 85% = warning
    CPUCriticalPercent      int32   // Ex: 95% = critical
    MemoryWarningPercent    int32   // Ex: 85%
    MemoryCriticalPercent   int32   // Ex: 90%

    // Target deviation
    TargetDeviationPercent  float64 // Ex: 30% = alerta se current está 30% acima/abaixo do target

    // Scaling behavior
    ScalingStuckMinutes     int     // Ex: 10 min sem escalar quando deveria

    // Config changes
    AlertOnConfigChange     bool    // Alertar mudanças em HPA config
    AlertOnResourceChange   bool    // Alertar mudanças em deployment resources

    sync.RWMutex
}

// WatchdogConfig configuração geral
type WatchdogConfig struct {
    // Monitoring
    ScanIntervalSeconds     int      // Ex: 30s entre scans
    HistoryRetentionMinutes int      // Ex: 5 min de histórico

    // Clusters
    ClustersConfigPath      string   // Path para clusters-config.json
    AutoDiscoverClusters    bool     // Auto-descobre clusters do kubeconfig
    ExcludeClusters         []string // Clusters para ignorar

    // Storage
    EnablePersistence       bool     // Salvar histórico em SQLite
    PersistencePath         string   // Ex: ~/.hpa-watchdog/history.db

    // Alerts
    MaxActiveAlerts         int      // Máximo de alertas ativos (ex: 100)
    AutoAckResolvedAlerts   bool     // Auto-acknowledge alertas resolvidos

    // Thresholds
    Thresholds              Thresholds

    // UI
    RefreshIntervalMs       int      // Ex: 500ms para refresh da TUI
    EnableSounds            bool     // Sons de alerta (beep)

    sync.RWMutex
}
```

---

## 🔄 Fluxo de Operação

### 1. Inicialização

```
1. Load config (watchdog.yaml)
2. Discover clusters (kubeconfig ou clusters-config.json)
3. Initialize time-series storage (in-memory)
4. Start Bubble Tea TUI
5. Start monitoring goroutines (1 por cluster)
```

### 2. Ciclo de Monitoramento (por cluster)

```
Loop (a cada ScanIntervalSeconds):
  1. List all namespaces (skip system namespaces)
  2. For each namespace:
     a. List HPAs
     b. For each HPA:
        - Get HPA config (min/max replicas, targets)
        - Get current metrics (replicas, CPU%, Memory%)
        - Get deployment resources (requests/limits)
        - Create HPASnapshot
        - Store in time-series (keep last 5 min)
  3. Analyze snapshots for anomalies
  4. Generate alerts if thresholds exceeded
  5. Send alerts to TUI via channel
  6. Sleep until next scan
```

### 3. Análise de Anomalias

```go
// Pseudo-código
func AnalyzeSnapshot(current HPASnapshot, history []HPASnapshot) []Anomaly {
    anomalies := []Anomaly{}

    if len(history) < 2 {
        return anomalies // Precisa de histórico
    }

    previous := history[len(history)-1]

    // 1. Replica spike/drop
    replicaDelta := float64(current.CurrentReplicas - previous.CurrentReplicas)
    replicaDeltaPercent := (replicaDelta / float64(previous.CurrentReplicas)) * 100

    if abs(replicaDeltaPercent) > Thresholds.ReplicaDeltaPercent {
        anomalies = append(anomalies, Anomaly{
            Type: AnomalyReplicaSpike,
            Severity: SeverityWarning,
            Message: fmt.Sprintf("Réplicas mudaram %d → %d (%.1f%%)",
                previous.CurrentReplicas, current.CurrentReplicas, replicaDeltaPercent),
            Delta: replicaDeltaPercent,
        })
    }

    // 2. CPU spike
    if current.CPUCurrent >= Thresholds.CPUCriticalPercent {
        anomalies = append(anomalies, Anomaly{
            Type: AnomalyCPUSpike,
            Severity: SeverityCritical,
            Message: fmt.Sprintf("CPU crítico: %d%% (limite: %d%%)",
                current.CPUCurrent, Thresholds.CPUCriticalPercent),
        })
    } else if current.CPUCurrent >= Thresholds.CPUWarningPercent {
        anomalies = append(anomalies, Anomaly{
            Type: AnomalyCPUSpike,
            Severity: SeverityWarning,
            Message: fmt.Sprintf("CPU alto: %d%% (aviso: %d%%)",
                current.CPUCurrent, Thresholds.CPUWarningPercent),
        })
    }

    // 3. Target deviation
    cpuDeviation := float64(current.CPUCurrent - current.CPUTarget)
    cpuDeviationPercent := (cpuDeviation / float64(current.CPUTarget)) * 100

    if abs(cpuDeviationPercent) > Thresholds.TargetDeviationPercent {
        anomalies = append(anomalies, Anomaly{
            Type: AnomalyTargetMiss,
            Severity: SeverityWarning,
            Message: fmt.Sprintf("CPU desviou do target: %d%% (target: %d%%, desvio: %.1f%%)",
                current.CPUCurrent, current.CPUTarget, cpuDeviationPercent),
        })
    }

    // 4. Config changes
    if Thresholds.AlertOnConfigChange {
        if current.MinReplicas != previous.MinReplicas ||
           current.MaxReplicas != previous.MaxReplicas {
            anomalies = append(anomalies, Anomaly{
                Type: AnomalyHPAConfigChange,
                Severity: SeverityInfo,
                Message: fmt.Sprintf("HPA config alterado: min %d→%d, max %d→%d",
                    previous.MinReplicas, current.MinReplicas,
                    previous.MaxReplicas, current.MaxReplicas),
            })
        }
    }

    // ... outras análises

    return anomalies
}
```

---

## 🎨 Interface TUI

### Layout Principal

```
╔════════════════════════════════════════════════════════════════════════════╗
║  HPA Watchdog v1.0.0              Clusters: 5  HPAs: 142  Alerts: 3 🔴    ║
╠════════════════════════════════════════════════════════════════════════════╣
║                                                                            ║
║  📊 DASHBOARD                                          ⚙️  Config  ?  Help║
║                                                                            ║
║  ┌─ Clusters ──────────────┐  ┌─ Recent Alerts ─────────────────────────┐║
║  │ ✅ akspriv-prod-east (34)│  │ 🔴 CRITICAL | 14:35:22                  │║
║  │ ✅ akspriv-prod-west (28)│  │    akspriv-prod-east/api-gateway/nginx  │║
║  │ ✅ akspriv-qa (18)       │  │    CPU spike: 95% (limit: 90%)          │║
║  │ ⚠️  akspriv-dev (12)     │  │                                          │║
║  │ ❌ akspriv-staging (OFF) │  │ ⚠️  WARNING | 14:34:10                  │║
║  │                           │  │    akspriv-qa/payments/api              │║
║  │ Total: 142 HPAs          │  │    Replica spike: 5 → 15 (200%)         │║
║  │ Scanning: 30s interval   │  │                                          │║
║  └───────────────────────────┘  │ ℹ️  INFO | 14:32:45                     │║
║                                  │    akspriv-dev/frontend/web             │║
║  ┌─ Top Anomalies (5 min) ──┐  │    HPA config changed: max 10→20        │║
║  │ 🔥 CPU Spikes:        8   │  │                                          │║
║  │ 📈 Replica Changes:   5   │  │ [↑↓] Scroll  [A] Ack  [C] Clear All    │║
║  │ ⚙️  Config Changes:   2   │  └──────────────────────────────────────────┘║
║  │ 🎯 Target Misses:     3   │                                             ║
║  └───────────────────────────┘  ┌─ Metrics Chart (api-gateway/nginx) ────┐║
║                                  │                                          │║
║  ┌─ Quick Stats ────────────┐  │  CPU %  100│         ╭──╮               │║
║  │ Active Alerts:        3   │  │         90│      ╭──╯  ╰╮              │║
║  │ Acked Alerts:         12  │  │         80│   ╭──╯      ╰─             │║
║  │ Last Scan:      14:35:45  │  │         70│╭──╯                        │║
║  │ Next Scan:            15s │  │         60├─────────────────────────    │║
║  │ Uptime:          2h 34m   │  │            14:30   14:32   14:34  14:35│║
║  └───────────────────────────┘  │                                          │║
║                                  │  Replicas  20│              ███         │║
║  [Tab] Views  [C] Config       │         15│           ███╱             │║
║  [F5] Refresh [Q] Quit         │         10│        ███╱                │║
║                                  │          5│     ███                    │║
║                                  │          0├─────────────────────────    │║
║                                  │            14:30   14:32   14:34  14:35│║
║                                  └──────────────────────────────────────────┘║
╚════════════════════════════════════════════════════════════════════════════╝
```

### Views (Tabs)

#### 1. Dashboard (Principal)
- Overview de todos os clusters
- Alertas recentes (painel scrollable)
- Gráficos ASCII de métricas selecionadas
- Quick stats

#### 2. Alerts View
```
╔═ Active Alerts (3) ═══════════════════════════════════════════════════════╗
║ 🔴 CRITICAL | 14:35:22 | akspriv-prod-east/api-gateway/nginx               ║
║    Type: CPU Spike                                                         ║
║    CPU: 95% (Warning: 85%, Critical: 90%)                                  ║
║    Current Replicas: 8  Min: 2  Max: 10                                    ║
║    [A] Acknowledge  [D] Details  [→] View History                          ║
╠════════════════════════════════════════════════════════════════════════════╣
║ ⚠️  WARNING | 14:34:10 | akspriv-qa/payments/api                           ║
║    Type: Replica Spike                                                     ║
║    Replicas: 5 → 15 (200% increase)                                        ║
║    Reason: CPU 85% → trigger scaling                                       ║
║    [A] Acknowledge  [D] Details                                            ║
╠════════════════════════════════════════════════════════════════════════════╣
║ ℹ️  INFO | 14:32:45 | akspriv-dev/frontend/web                             ║
║    Type: HPA Config Change                                                 ║
║    Min Replicas: 2 → 2  Max Replicas: 10 → 20                             ║
║    [A] Acknowledge                                                         ║
╚════════════════════════════════════════════════════════════════════════════╝
[↑↓] Navigate  [A] Ack Selected  [Shift+A] Ack All  [ESC] Back
```

#### 3. Cluster View
```
╔═ Cluster: akspriv-prod-east ═══════════════════════════════════════════════╗
║ Status: ✅ Online  |  HPAs: 34  |  Namespaces: 12  |  Uptime: 47d 3h       ║
╠════════════════════════════════════════════════════════════════════════════╣
║ Namespace          HPAs   Alerts   CPU Avg   Mem Avg   Last Scan          ║
║ ─────────────────────────────────────────────────────────────────────────  ║
║ api-gateway          3       1      78%       65%      14:35:22           ║
║ payments             5       1      62%       58%      14:35:20           ║
║ frontend             2       0      45%       42%      14:35:18           ║
║ backend              8       0      58%       61%      14:35:16           ║
║ ...                                                                        ║
╚════════════════════════════════════════════════════════════════════════════╝
[Enter] View Namespace  [H] HPA Details  [ESC] Back
```

#### 4. Config Modal
```
╔═ Configuration ═══════════════════════════════════════════════════════════╗
║                                                                            ║
║  Monitoring Settings                                                       ║
║  ┌────────────────────────────────────────────────────────────────────┐  ║
║  │ Scan Interval:            [30] seconds                             │  ║
║  │ History Retention:        [5] minutes                              │  ║
║  │ Auto-discover Clusters:   [✓] Enabled                              │  ║
║  └────────────────────────────────────────────────────────────────────┘  ║
║                                                                            ║
║  Alert Thresholds                                                          ║
║  ┌────────────────────────────────────────────────────────────────────┐  ║
║  │ Replica Delta (percent):  [50] %                                   │  ║
║  │ Replica Delta (absolute): [5] replicas                             │  ║
║  │                                                                     │  ║
║  │ CPU Warning:              [85] %                                   │  ║
║  │ CPU Critical:             [90] %                                   │  ║
║  │ Memory Warning:           [85] %                                   │  ║
║  │ Memory Critical:          [90] %                                   │  ║
║  │                                                                     │  ║
║  │ Target Deviation:         [30] %                                   │  ║
║  │ Scaling Stuck:            [10] minutes                             │  ║
║  │                                                                     │  ║
║  │ Alert on Config Change:   [✓] Enabled                              │  ║
║  │ Alert on Resource Change: [✓] Enabled                              │  ║
║  └────────────────────────────────────────────────────────────────────┘  ║
║                                                                            ║
║  [↑↓] Navigate  [Enter] Edit  [S] Save  [R] Reset Defaults  [ESC] Cancel ║
╚════════════════════════════════════════════════════════════════════════════╝
```

### Componentes Interativos

#### Gráficos ASCII
```go
// Exemplo de ASCII chart para CPU/Memory/Replicas
// Usa biblioteca github.com/guptarohit/asciigraph

import "github.com/guptarohit/asciigraph"

func renderMetricChart(snapshots []HPASnapshot, metric string) string {
    data := extractMetricData(snapshots, metric)

    graph := asciigraph.Plot(data,
        asciigraph.Height(10),
        asciigraph.Width(50),
        asciigraph.Caption(metric),
    )

    return graph
}
```

### Controles de Teclado

| Tecla | Ação |
|-------|------|
| `Tab` | Alternar entre views (Dashboard, Alerts, Clusters, Config) |
| `↑↓` ou `j k` | Navegar listas |
| `Space` | Selecionar item |
| `Enter` | Ver detalhes / Editar |
| `A` | Acknowledge alerta selecionado |
| `Shift+A` | Acknowledge todos os alertas |
| `C` | Clear alertas acknowledged |
| `D` | Ver detalhes do alerta |
| `H` | Ver histórico de snapshots |
| `G` | Ir para gráfico de métricas |
| `S` | Salvar configuração |
| `R` | Reload configuração |
| `F5` | Force refresh |
| `Ctrl+C` ou `Q` | Quit |
| `?` | Help |

---

## ⚙️ Configuração

### Arquivo `configs/watchdog.yaml`

```yaml
# HPA Watchdog Configuration

monitoring:
  scan_interval_seconds: 30        # Intervalo entre scans (segundos)
  history_retention_minutes: 5     # Quanto histórico manter em memória

clusters:
  config_path: "~/.k8s-hpa-manager/clusters-config.json"
  auto_discover: true              # Auto-descobre clusters do kubeconfig
  exclude:                         # Clusters para ignorar
    - akspriv-test
    - akspriv-sandbox

storage:
  enable_persistence: false        # Salvar histórico em SQLite
  persistence_path: "~/.hpa-watchdog/history.db"

alerts:
  max_active_alerts: 100           # Máximo de alertas ativos
  auto_ack_resolved: true          # Auto-acknowledge alertas resolvidos

thresholds:
  # Replica changes
  replica_delta_percent: 50.0      # Alerta se réplicas mudam >50%
  replica_delta_absolute: 5        # Alerta se réplicas mudam ±5

  # CPU/Memory
  cpu_warning_percent: 85
  cpu_critical_percent: 90
  memory_warning_percent: 85
  memory_critical_percent: 90

  # Target deviation
  target_deviation_percent: 30.0   # Alerta se current está 30% acima/abaixo do target

  # Scaling behavior
  scaling_stuck_minutes: 10        # Alerta se não escala quando deveria

  # Config changes
  alert_on_config_change: true     # Alertar mudanças em HPA config
  alert_on_resource_change: true   # Alertar mudanças em deployment resources

ui:
  refresh_interval_ms: 500         # Refresh da TUI
  enable_sounds: false             # Sons de alerta (beep)
  theme: "dark"                    # dark, light, monokai

logging:
  level: "info"                    # debug, info, warn, error
  output: "~/.hpa-watchdog/watchdog.log"
  max_size_mb: 100
  max_backups: 3
```

---

## 🔌 Integração com k8s-hpa-manager

### Reutilização de Código

O watchdog pode reutilizar componentes existentes do k8s-hpa-manager:

```go
// Importar módulos compartilhados
import (
    "k8s-hpa-manager/internal/kubernetes"  // K8s client wrapper
    "k8s-hpa-manager/internal/config"      // Cluster discovery
    "k8s-hpa-manager/internal/models"      // HPAInfo, Namespace
)

// Exemplo: usar KubeConfigManager para descobrir clusters
func discoverClusters() ([]string, error) {
    manager := config.NewKubeConfigManager()
    return manager.DiscoverClusters()
}
```

### Independência

Mesmo compartilhando código, o watchdog é **completamente autônomo**:
- Binário separado: `hpa-watchdog`
- Configuração separada: `~/.hpa-watchdog/`
- Não depende do k8s-hpa-manager estar rodando
- Pode rodar em background como daemon

---

## 🚀 Roadmap de Desenvolvimento

### Fase 1: MVP (v1.0.0) - 2 semanas
- [ ] **Setup Projeto**
  - [ ] Estrutura de diretórios
  - [ ] Go modules
  - [ ] Makefile
- [ ] **Core Monitoring**
  - [ ] Collector de métricas K8s
  - [ ] Time-series storage (in-memory)
  - [ ] Análise básica de anomalias
- [ ] **TUI Básico**
  - [ ] Dashboard view
  - [ ] Alerts panel
  - [ ] Navegação básica
- [ ] **Config System**
  - [ ] YAML parser
  - [ ] Thresholds management

### Fase 2: Features Avançadas (v1.1.0) - 1 semana
- [ ] **Enhanced UI**
  - [ ] Cluster view
  - [ ] Config modal interativo
  - [ ] ASCII charts (asciigraph)
- [ ] **Advanced Analysis**
  - [ ] Scaling stuck detection
  - [ ] Target deviation tracking
  - [ ] Trend prediction (opcional)
- [ ] **Persistence**
  - [ ] SQLite storage
  - [ ] Alert history
  - [ ] Export to JSON/CSV

### Fase 3: Polish & Production (v1.2.0) - 1 semana
- [ ] **Production Ready**
  - [ ] Systemd service file
  - [ ] Docker image
  - [ ] Log rotation
- [ ] **Notifications**
  - [ ] Webhook support (Slack, Discord, Teams)
  - [ ] Email alerts (opcional)
- [ ] **Performance**
  - [ ] Optimize memory usage
  - [ ] Parallel cluster scanning
  - [ ] Benchmark large clusters (100+ HPAs)

---

## 📦 Dependências

### Core
- **Go 1.23+**: Linguagem principal
- **client-go v0.31.4**: Cliente Kubernetes oficial
- **Bubble Tea v0.24.2**: TUI framework
- **Lipgloss v1.1.0**: Styling

### Adicionais
- **viper**: Config management (YAML)
- **asciigraph**: Gráficos ASCII
- **go-sqlite3** (opcional): Persistência
- **zerolog**: Structured logging

```bash
go get github.com/charmbracelet/bubbletea@v0.24.2
go get github.com/charmbracelet/lipgloss@v1.1.0
go get k8s.io/client-go@v0.31.4
go get github.com/spf13/viper
go get github.com/guptarohit/asciigraph
go get github.com/rs/zerolog
go get github.com/mattn/go-sqlite3  # opcional
```

---

## 🧪 Testing Strategy

### Unit Tests
```bash
# Testar componentes isolados
go test ./internal/monitor/...
go test ./internal/storage/...
go test ./internal/config/...
```

### Integration Tests
```bash
# Testar integração com K8s (requer cluster)
go test ./tests/integration/...
```

### Manual Testing
```bash
# Build e run
make build
./build/hpa-watchdog

# Com config customizada
./build/hpa-watchdog --config /path/to/config.yaml

# Debug mode
./build/hpa-watchdog --debug
```

---

## 📝 Comandos CLI

### Usage

```bash
# Iniciar watchdog com config padrão
hpa-watchdog

# Especificar config customizada
hpa-watchdog --config /path/to/watchdog.yaml

# Debug mode (logs verbose)
hpa-watchdog --debug

# Mostrar versão
hpa-watchdog version

# Validar configuração
hpa-watchdog validate --config watchdog.yaml

# Export histórico
hpa-watchdog export --output history.json --format json

# Run como daemon (background)
hpa-watchdog daemon --pidfile /var/run/hpa-watchdog.pid
```

---

## 🎯 Casos de Uso

### 1. Monitoramento de Produção
```
Operador SRE quer monitorar 5 clusters de produção 24/7.

Workflow:
1. Configurar thresholds conservadores (CPU: 85%, Replicas: 30%)
2. Habilitar persistência (SQLite)
3. Rodar como systemd service
4. Configurar webhook para Slack em alertas CRITICAL
5. Dashboard em terminal separado para visualização
```

### 2. Detecção de Anomalias Pós-Deploy
```
Dev faz deploy novo e quer monitorar comportamento do HPA.

Workflow:
1. Iniciar watchdog com scan_interval=15s
2. Foco no cluster/namespace específico
3. Observar gráficos de CPU/Replicas em tempo real
4. Alertas instantâneos se CPU spike > 90%
5. Histórico de 5 min para análise
```

### 3. Auditoria de Mudanças
```
Compliance quer rastrear todas mudanças em HPAs.

Workflow:
1. Habilitar alert_on_config_change: true
2. Habilitar alert_on_resource_change: true
3. Persistência habilitada
4. Export periódico para CSV
5. Alertas INFO para cada mudança detectada
```

---

## 🔐 Segurança

### Autenticação K8s
- Usa kubeconfig padrão (`~/.kube/config`)
- Suporta múltiplos contexts
- Respeita RBAC do cluster
- Não requer permissões de escrita (read-only)

### Permissões Necessárias

```yaml
# ClusterRole necessária para watchdog
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: hpa-watchdog-reader
rules:
- apiGroups: [""]
  resources: ["namespaces", "pods"]
  verbs: ["get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets", "statefulsets", "daemonsets"]
  verbs: ["get", "list"]
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["get", "list"]
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods", "nodes"]
  verbs: ["get", "list"]
```

### Dados Sensíveis
- Não armazena credenciais
- Logs não contêm secrets
- Config YAML pode conter apenas nomes de clusters

---

## 📊 Métricas e Performance

### Benchmarks Esperados

| Métrica | Target | Observações |
|---------|--------|-------------|
| **Scan Time** | < 5s por cluster | 50 HPAs, 10 namespaces |
| **Memory Usage** | < 100 MB | 5 clusters, 250 HPAs, 5 min histórico |
| **CPU Usage** | < 5% idle | Scanning em background |
| **Storage** | < 10 MB/dia | SQLite com 1000 alerts/dia |

### Escalabilidade

| Cenário | HPAs | Clusters | Memory | Scan Time |
|---------|------|----------|--------|-----------|
| Small   | 50   | 2        | ~50 MB | ~3s       |
| Medium  | 250  | 5        | ~100 MB| ~10s      |
| Large   | 500  | 10       | ~200 MB| ~20s      |

---

## 🐛 Troubleshooting

### Problemas Comuns

#### 1. Cluster Connection Failed
```
Erro: failed to list namespaces in cluster akspriv-prod: context deadline exceeded

Solução:
- Verificar conectividade VPN
- Testar com kubectl: kubectl cluster-info --context=akspriv-prod
- Aumentar timeout na config: client_timeout_seconds: 30
```

#### 2. High Memory Usage
```
Sintoma: Watchdog usando >500 MB RAM

Solução:
- Reduzir history_retention_minutes (padrão: 5 min)
- Limitar max_active_alerts (padrão: 100)
- Desabilitar persistence se não necessário
```

#### 3. Missing Metrics
```
Sintoma: Métricas de CPU/Memory aparecem como 0%

Solução:
- Verificar se Prometheus está acessível: curl http://prometheus.monitoring.svc:9090/-/healthy
- Testar query PromQL manualmente
- Fallback para metrics-server se Prometheus indisponível
- Verificar se metrics-server está instalado: kubectl top pods
```

#### 4. Prometheus Connection Failed
```
Sintoma: Failed to query Prometheus: connection refused

Solução:
- Verificar endpoint Prometheus na config
- Testar conectividade: kubectl port-forward -n monitoring svc/prometheus 9090:9090
- Verificar auto-discovery: prometheus.auto_discover: true
- Habilitar fallback: prometheus.fallback_to_metrics_server: true
```

---

## 🔌 Integração Prometheus

### Visão Geral

O HPA Watchdog usa **arquitetura híbrida** para coleta de dados:
- **K8s API** (kubernetes/client-go): Configuração e estado dos HPAs
- **Prometheus** (prometheus/client_golang): Métricas e histórico temporal

### Vantagens do Prometheus

✅ **Histórico Nativo** - Prometheus já armazena métricas (não precisa cache local)
✅ **Métricas Ricas** - CPU, Memory, Network, Request Rate, Errors, Latency
✅ **Análise Temporal** - Queries range fáceis (`[5m]`, `rate()`, `increase()`)
✅ **Performance** - PromQL otimizado para TSDB
✅ **Correlação** - Cruzar HPA com tráfego, erros, latency

### Queries Prometheus Úteis

#### 1. CPU Usage (HPA Target)
```promql
# CPU atual dos pods do HPA (%)
sum(rate(container_cpu_usage_seconds_total{
    namespace="api-gateway",
    pod=~"nginx-.*"
}[1m])) /
sum(kube_pod_container_resource_requests{
    namespace="api-gateway",
    pod=~"nginx-.*",
    resource="cpu"
}) * 100
```

#### 2. Memory Usage
```promql
# Memory atual (%)
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

#### 3. Replica History
```promql
# Réplicas atuais (últimos 5 min)
kube_horizontalpodautoscaler_status_current_replicas{
    namespace="api-gateway",
    horizontalpodautoscaler="nginx"
}[5m]
```

#### 4. Request Rate
```promql
# Requests por segundo
sum(rate(http_requests_total{
    namespace="api-gateway",
    service="nginx"
}[1m]))
```

#### 5. Error Rate
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

#### 6. P95 Latency
```promql
# P95 latency (ms)
histogram_quantile(0.95,
    sum(rate(http_request_duration_seconds_bucket{
        namespace="api-gateway"
    }[1m])) by (le)
) * 1000
```

### Auto-Discovery de Prometheus

```yaml
# watchdog.yaml
monitoring:
  prometheus:
    enabled: true
    auto_discover: true  # Descobre endpoint via K8s Service

    # Padrões de busca (padrão)
    discovery_patterns:
      - "prometheus.monitoring.svc:9090"
      - "prometheus-server.prometheus.svc:9090"
      - "kube-prometheus-stack-prometheus.monitoring.svc:9090"

    # Ou especificar manualmente
    endpoints:
      akspriv-prod-east: "http://prometheus.monitoring.svc:9090"
```

### Fallback para Metrics-Server

```yaml
monitoring:
  prometheus:
    enabled: true
    fallback_to_metrics_server: true  # Usa K8s metrics-server se Prometheus falhar
```

**Comportamento:**
1. Tenta Prometheus primeiro
2. Se falhar (timeout, connection refused), usa metrics-server
3. Métricas básicas (CPU/Memory) via K8s API
4. Sem histórico rico nem extended metrics

### Exemplo de Alerta Enriquecido

**Antes (só K8s API):**
```
🔴 CPU spike: 95% (limite: 90%)
```

**Depois (com Prometheus):**
```
🔴 ANOMALIA CRÍTICA DETECTADA

📊 HPA: api-gateway/nginx
⏰ Timestamp: 14:35:22

🔥 CPU Spike: 95% (limite: 90%)
   Histórico 5min: [72%, 75%, 78%, 85%, 92%, 95%]
   Tendência: ↗️ Alta consistente

📈 Contexto (Prometheus):
   • Request rate: 850 req/s (↑300% vs baseline 200 req/s)
   • Error rate: 12% (↑ de 0.5% normal)
   • P95 latency: 2.5s (↑ de 150ms normal)
   • Réplicas: 8 → 10 (scaling ativo)

💡 Diagnóstico Automático:
   Traffic spike + high errors + latency degradation
   = Provável incident upstream ou capacidade insuficiente

🚨 Ação Sugerida:
   1. Verificar upstream services (dependencies)
   2. Revisar logs para errors
   3. Considerar aumentar max_replicas se traffic sustentado
```

---

## 📚 Documentação Adicional

### README.md do Projeto
- Quick Start
- Installation
- Screenshots
- Contributing

### docs/ARCHITECTURE.md
- Detalhamento técnico da arquitetura
- Diagramas de sequência
- Decisões de design

### docs/API.md (se houver API REST futura)
- Endpoints
- Request/Response examples
- Authentication

---

## 🤝 Contribuição

### Como Contribuir
1. Fork do repositório
2. Branch: `git checkout -b feature/nova-feature`
3. Commit: `git commit -m 'feat: adiciona nova feature'`
4. Push: `git push origin feature/nova-feature`
5. Pull Request

### Code Style
- Seguir padrões do k8s-hpa-manager
- Usar Bubble Tea commands para async
- Unicode-safe (sempre `[]rune` para texto)
- Error handling adequado

---

## 📄 Licença

MIT License - mesmo do k8s-hpa-manager principal

---

## 🔗 Referências

- [Bubble Tea Documentation](https://github.com/charmbracelet/bubbletea)
- [Kubernetes client-go](https://github.com/kubernetes/client-go)
- [HPA Best Practices](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
- [k8s-hpa-manager](https://github.com/Paulo-Ribeiro-Log/Scale_HPA)

---

**Última Atualização**: 23 de outubro de 2025
**Autor**: Paulo Ribeiro
**Status**: 🟡 Planejamento - Pronto para desenvolvimento
