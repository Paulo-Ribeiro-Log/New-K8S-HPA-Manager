package models

import (
	"sync"
	"time"
)

// HPASnapshot representa o estado de um HPA em um momento específico
type HPASnapshot struct {
	Timestamp time.Time
	Cluster   string
	Namespace string
	Name      string

	// === K8s API Data (Config & State) ===
	// HPA Config
	MinReplicas     int32
	MaxReplicas     int32
	CurrentReplicas int32
	DesiredReplicas int32

	// Targets
	CPUTarget    int32 // % (ex: 70)
	MemoryTarget int32 // % (ex: 80)

	// Deployment Resources (K8s API)
	CPURequest    string // Ex: "500m"
	CPULimit      string // Ex: "1000m"
	MemoryRequest string // Ex: "512Mi"
	MemoryLimit   string // Ex: "1Gi"

	// Status
	Ready         bool
	ScalingActive bool
	LastScaleTime *time.Time

	// === Prometheus Metrics (Real-time & Historical) ===
	// Current Metrics (Prometheus)
	CPUCurrent    float64 // % atual (mais preciso que K8s API)
	MemoryCurrent float64 // % atual

	// Historical Metrics (5 min history from Prometheus)
	CPUHistory     []float64 // CPU últimos 5 min (1 ponto/30s = 10 pontos)
	MemoryHistory  []float64 // Memory últimos 5 min
	ReplicaHistory []int32   // Réplicas últimos 5 min (de kube_hpa_status_current_replicas)

	// Extended Metrics (Prometheus - Optional)
	RequestRate    float64 // Requests/sec (de http_requests_total)
	ErrorRate      float64 // % errors (5xx de http_requests_total)
	P95Latency     float64 // P95 latency (ms) de http_request_duration_seconds
	NetworkRxBytes float64 // Network RX (bytes/s)
	NetworkTxBytes float64 // Network TX (bytes/s)

	// Additional Metrics (JSON blob) - Fase 2: Historical Baseline
	// Armazena métricas adicionais coletadas do Prometheus que não tem campo dedicado
	// Exemplo: cpu_throttling, memory_oom, pod_restart_count, etc.
	AdditionalMetrics map[string]interface{} `json:"additional_metrics,omitempty"`

	// Baseline Control - Phase 2
	BaselineReady    bool      // Indica se baseline histórico foi coletado e HPA está pronto para monitoramento
	BaselineStart    time.Time // Quando iniciou coleta de baseline
	BaselineComplete time.Time // Quando completou baseline (zero se não completou)

	// Metadata
	DataSource DataSource // Indica se veio de Prometheus ou Metrics-Server
}

// DataSource indica a origem das métricas
type DataSource int

const (
	DataSourcePrometheus    DataSource = iota // Métricas do Prometheus (preferencial)
	DataSourceMetricsServer                   // Fallback: Kubernetes Metrics-Server
	DataSourceHybrid                          // Híbrido: alguns Prometheus, alguns K8s
)

func (d DataSource) String() string {
	switch d {
	case DataSourcePrometheus:
		return "Prometheus"
	case DataSourceMetricsServer:
		return "MetricsServer"
	case DataSourceHybrid:
		return "Hybrid"
	default:
		return "Unknown"
	}
}

// TimeSeriesData armazena histórico de 5 minutos
type TimeSeriesData struct {
	HPAKey      string // "cluster/namespace/name"
	Snapshots   []HPASnapshot
	MaxDuration time.Duration // 5 minutos
	Stats       HPAStats      // Estatísticas calculadas
	sync.RWMutex
}

// HPAStats estatísticas calculadas do histórico
type HPAStats struct {
	// CPU Statistics
	CPUAverage float64
	CPUMin     float64
	CPUMax     float64
	CPUStdDev  float64
	CPUTrend   string // "increasing", "decreasing", "stable"

	// Memory Statistics
	MemoryAverage float64
	MemoryMin     float64
	MemoryMax     float64
	MemoryStdDev  float64
	MemoryTrend   string // "increasing", "decreasing", "stable"

	// Replica Changes
	ReplicaChanges int       // Quantas mudanças em 5min
	LastChange     time.Time // Último scale event
	ReplicaTrend   string    // "increasing", "decreasing", "stable"
}

// Add adiciona um snapshot ao histórico
func (ts *TimeSeriesData) Add(snapshot HPASnapshot) {
	ts.Lock()
	defer ts.Unlock()

	ts.Snapshots = append(ts.Snapshots, snapshot)

	// Remove snapshots antigos (> MaxDuration)
	cutoff := time.Now().Add(-ts.MaxDuration)
	validSnapshots := []HPASnapshot{}
	for _, s := range ts.Snapshots {
		if s.Timestamp.After(cutoff) {
			validSnapshots = append(validSnapshots, s)
		}
	}
	ts.Snapshots = validSnapshots
}

// GetLatest retorna o snapshot mais recente
func (ts *TimeSeriesData) GetLatest() *HPASnapshot {
	ts.RLock()
	defer ts.RUnlock()

	if len(ts.Snapshots) == 0 {
		return nil
	}
	return &ts.Snapshots[len(ts.Snapshots)-1]
}

// GetPrevious retorna o penúltimo snapshot (anterior ao latest)
// Usado para detectar variações bruscas entre scans
func (ts *TimeSeriesData) GetPrevious() *HPASnapshot {
	ts.RLock()
	defer ts.RUnlock()

	if len(ts.Snapshots) < 2 {
		return nil
	}
	return &ts.Snapshots[len(ts.Snapshots)-2]
}

// GetHistory retorna todos os snapshots
func (ts *TimeSeriesData) GetHistory() []HPASnapshot {
	ts.RLock()
	defer ts.RUnlock()

	// Retorna cópia para evitar race conditions
	history := make([]HPASnapshot, len(ts.Snapshots))
	copy(history, ts.Snapshots)
	return history
}

// UnifiedAlert combina alertas de Alertmanager + Watchdog
type UnifiedAlert struct {
	ID       string
	Source   AlertSource   // Alertmanager ou Watchdog
	Severity AlertSeverity // Critical, Warning, Info
	Type     AnomalyType

	// Core Info
	Cluster   string
	Namespace string
	HPAName   string
	Timestamp time.Time

	// Message
	Summary     string
	Description string

	// Alertmanager specific
	Fingerprint  string
	GeneratorURL string
	Status       string // "active", "suppressed"
	SilencedBy   []string

	// Enrichment (from Prometheus + Watchdog)
	Snapshot    *HPASnapshot  // Estado atual do HPA
	Context     *AlertContext // Contexto adicional
	Correlation []string      // IDs de alertas correlacionados

	// Actions
	Acknowledged bool
	AckedAt      *time.Time
	AckedBy      string
}

// AlertSource indica a origem do alerta
type AlertSource int

const (
	AlertSourceAlertmanager AlertSource = iota
	AlertSourceWatchdog
)

func (a AlertSource) String() string {
	switch a {
	case AlertSourceAlertmanager:
		return "Alertmanager"
	case AlertSourceWatchdog:
		return "Watchdog"
	default:
		return "Unknown"
	}
}

// AnomalyType define tipos de anomalias
type AnomalyType int

const (
	AnomalyReplicaSpike     AnomalyType = iota // Aumento abrupto de réplicas
	AnomalyReplicaDrop                         // Queda abrupta de réplicas
	AnomalyCPUSpike                            // CPU current > threshold
	AnomalyMemorySpike                         // Memory current > threshold
	AnomalyResourceChange                      // Mudança em requests/limits
	AnomalyHPAConfigChange                     // Mudança em min/max replicas
	AnomalyScalingStuck                        // HPA não consegue escalar
	AnomalyTargetMiss                          // Current muito acima/abaixo do target
	AnomalyReplicaOscillation                  // Réplicas mudando rapidamente
)

func (a AnomalyType) String() string {
	switch a {
	case AnomalyReplicaSpike:
		return "ReplicaSpike"
	case AnomalyReplicaDrop:
		return "ReplicaDrop"
	case AnomalyCPUSpike:
		return "CPUSpike"
	case AnomalyMemorySpike:
		return "MemorySpike"
	case AnomalyResourceChange:
		return "ResourceChange"
	case AnomalyHPAConfigChange:
		return "HPAConfigChange"
	case AnomalyScalingStuck:
		return "ScalingStuck"
	case AnomalyTargetMiss:
		return "TargetMiss"
	case AnomalyReplicaOscillation:
		return "ReplicaOscillation"
	default:
		return "Unknown"
	}
}

// AlertSeverity define níveis de severidade
type AlertSeverity int

const (
	SeverityInfo     AlertSeverity = iota
	SeverityWarning
	SeverityCritical
)

func (s AlertSeverity) String() string {
	switch s {
	case SeverityInfo:
		return "Info"
	case SeverityWarning:
		return "Warning"
	case SeverityCritical:
		return "Critical"
	default:
		return "Unknown"
	}
}

// AlertContext fornece contexto adicional para alertas
type AlertContext struct {
	// Métricas adicionais
	RequestRate float64
	ErrorRate   float64
	P95Latency  float64

	// Análise
	Trend          string // "increasing", "decreasing", "stable"
	PredictedState string // "will_max_out", "will_stabilize"

	// Links
	PrometheusURL  string
	GrafanaURL     string
	KubectlCommand string
}

// Thresholds define limites configuráveis
type Thresholds struct {
	// Replica changes
	ReplicaDeltaPercent  float64 // Ex: 50% = alerta se réplicas mudam >50%
	ReplicaDeltaAbsolute int32   // Ex: 5 = alerta se réplicas mudam ±5

	// CPU/Memory
	CPUWarningPercent    int32 // Ex: 85% = warning
	CPUCriticalPercent   int32 // Ex: 95% = critical
	MemoryWarningPercent int32 // Ex: 85%
	MemoryCriticalPercent int32 // Ex: 90%

	// Target deviation
	TargetDeviationPercent float64 // Ex: 30% = alerta se current está 30% acima/abaixo do target

	// Scaling behavior
	ScalingStuckMinutes int // Ex: 10 min sem escalar quando deveria

	// Config changes
	AlertOnConfigChange   bool // Alertar mudanças em HPA config
	AlertOnResourceChange bool // Alertar mudanças em deployment resources

	// Extended Metrics
	RequestRateSpikePercent  float64 // Ex: 100% = alerta se request rate dobrar
	ErrorRateCriticalPercent float64 // Ex: 5% = alerta se >5% errors
	P95LatencyCriticalMs     float64 // Ex: 1000ms = alerta se P95 >1s

	sync.RWMutex
}

// WatchdogConfig configuração geral
type WatchdogConfig struct {
	// Monitoring
	ScanIntervalSeconds     int // Ex: 30s entre scans
	HistoryRetentionMinutes int // Ex: 5 min de histórico

	// Prometheus
	PrometheusEnabled         bool
	PrometheusAutoDiscover    bool
	PrometheusEndpoints       map[string]string // cluster -> endpoint
	PrometheusFallback        bool
	PrometheusDiscoveryPatterns []string

	// Alertmanager
	AlertmanagerEnabled         bool
	AlertmanagerAutoDiscover    bool
	AlertmanagerEndpoints       map[string]string // cluster -> endpoint
	AlertmanagerSyncInterval    int
	AlertmanagerDiscoveryPatterns []string

	// Clusters
	ClustersConfigPath   string   // Path para clusters-config.json
	AutoDiscoverClusters bool     // Auto-descobre clusters do kubeconfig
	ExcludeClusters      []string // Clusters para ignorar

	// Storage
	EnablePersistence bool   // Salvar histórico em SQLite
	PersistencePath   string // Ex: ~/.hpa-watchdog/history.db

	// Alerts
	MaxActiveAlerts       int      // Máximo de alertas ativos (ex: 100)
	AutoAckResolvedAlerts bool     // Auto-acknowledge alertas resolvidos
	SourcePriority        []string // ["alertmanager", "watchdog"]
	Deduplicate           bool
	DedupeWindowMinutes   int
	AutoCorrelate         bool
	CorrelationWindowMinutes int

	// Thresholds
	Thresholds Thresholds

	// UI
	RefreshIntervalMs int    // Ex: 500ms para refresh da TUI
	Theme             string // dark, light, monokai
	EnableSounds      bool   // Sons de alerta (beep)

	// Logging
	LogLevel      string // debug, info, warn, error
	LogOutput     string
	LogMaxSizeMB  int
	LogMaxBackups int
	LogCompress   bool

	sync.RWMutex
}

// ClusterInfo informações sobre um cluster
type ClusterInfo struct {
	Name       string
	Context    string
	Server     string
	Namespace  string
	IsDefault  bool
	HPACount   int
	AlertCount int
	LastScan   time.Time
	Status     ClusterStatus
}

// ClusterStatus status de um cluster
type ClusterStatus int

const (
	ClusterStatusOnline ClusterStatus = iota
	ClusterStatusOffline
	ClusterStatusError
)

func (c ClusterStatus) String() string {
	switch c {
	case ClusterStatusOnline:
		return "Online"
	case ClusterStatusOffline:
		return "Offline"
	case ClusterStatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// PrometheusHealth representa o status de saúde do Prometheus
type PrometheusHealth struct {
	Endpoint       string
	Timestamp      time.Time
	Healthy        bool
	Connected      bool
	Version        string
	ActiveTargets  int
	DroppedTargets int
	Error          string
}
