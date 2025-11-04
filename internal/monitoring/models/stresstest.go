package models

import (
	"time"
)

// StressTestMetrics captura métricas de pico durante stress test
type StressTestMetrics struct {
	// Metadados do teste
	TestName      string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	Status        StressTestStatus
	TotalScans    int
	ScanInterval  time.Duration

	// Métricas gerais
	TotalClusters       int
	TotalHPAs           int
	TotalHPAsMonitored  int
	TotalHPAsWithIssues int

	// Métricas de pico (valores máximos atingidos)
	PeakMetrics PeakMetrics

	// Problemas detectados
	CriticalIssues []StressTestIssue
	WarningIssues  []StressTestIssue
	InfoIssues     []StressTestIssue

	// Estatísticas por HPA
	HPAMetrics map[string]*HPAStressMetrics // key: cluster/namespace/name

	// Timeline de eventos
	Timeline []TimelineEvent

	// Recomendações
	Recommendations []Recommendation
}

// StressTestStatus status do teste
type StressTestStatus string

const (
	StressTestStatusRunning   StressTestStatus = "running"
	StressTestStatusCompleted StressTestStatus = "completed"
	StressTestStatusStopped   StressTestStatus = "stopped"
	StressTestStatusFailed    StressTestStatus = "failed"
)

// PeakMetrics métricas de pico alcançadas
type PeakMetrics struct {
	// CPU
	MaxCPUPercent     float64
	MaxCPUHPA         string // HPA que atingiu max CPU
	MaxCPUTime        time.Time

	// Memory
	MaxMemoryPercent  float64
	MaxMemoryHPA      string
	MaxMemoryTime     time.Time

	// Réplicas
	TotalReplicasPre  int // Réplicas totais antes do teste
	TotalReplicasPeak int // Réplicas totais no pico
	TotalReplicasPost int // Réplicas totais após o teste
	ReplicaIncrease   int // Aumento absoluto
	ReplicaIncreaseP  float64 // Aumento percentual

	// Erros
	MaxErrorRate      float64
	MaxErrorRateHPA   string
	MaxErrorRateTime  time.Time

	// Latência
	MaxLatencyP95     float64
	MaxLatencyP95HPA  string
	MaxLatencyP95Time time.Time
}

// HPAStressMetrics métricas de um HPA específico durante o teste
type HPAStressMetrics struct {
	// Identificação
	Cluster   string
	Namespace string
	Name      string

	// Configuração
	MinReplicas int32
	MaxReplicas int32
	TargetCPU   int32

	// Comportamento durante o teste
	ReplicasPre   int32 // Antes do teste
	ReplicasPeak  int32 // Pico durante teste
	ReplicasPost  int32 // Após o teste
	ReplicaChanges int  // Número de mudanças

	// Métricas de pico
	PeakCPU      float64
	PeakMemory   float64
	PeakErrorRate float64
	PeakLatency  float64

	// Problemas detectados
	HasIssues     bool
	Issues        []StressTestIssue
	MaxedOut      bool // Chegou ao maxReplicas?
	Oscillated    bool // Oscilou durante teste?
	HighErrorRate bool // Taxa de erro alta?

	// Status final
	Status HPAStressStatus
}

// HPAStressStatus status do HPA no stress test
type HPAStressStatus string

const (
	HPAStatusHealthy   HPAStressStatus = "healthy"   // Sem problemas
	HPAStatusWarning   HPAStressStatus = "warning"   // Problemas menores
	HPAStatusCritical  HPAStressStatus = "critical"  // Problemas críticos
	HPAStatusMaxedOut  HPAStressStatus = "maxed_out" // No limite
	HPAStatusFailed    HPAStressStatus = "failed"    // Falhou completamente
)

// StressTestIssue problema detectado durante stress test
type StressTestIssue struct {
	// Identificação
	Cluster   string
	Namespace string
	HPAName   string

	// Detalhes do problema
	Type        string // Tipo da anomalia (ex: "Oscillation", "MaxedOut")
	Severity    AlertSeverity
	DetectedAt  time.Time
	Duration    time.Duration
	Description string

	// Evidências
	Evidence map[string]interface{}

	// Recomendação
	Recommendation string

	// Status
	Resolved   bool
	ResolvedAt time.Time
}

// TimelineEvent evento na timeline do teste
type TimelineEvent struct {
	Timestamp   time.Time
	Type        TimelineEventType
	Cluster     string
	Namespace   string
	HPAName     string
	Description string
	Severity    AlertSeverity
	Details     map[string]interface{}
}

// TimelineEventType tipo de evento na timeline
type TimelineEventType string

const (
	EventTestStarted      TimelineEventType = "test_started"
	EventTestCompleted    TimelineEventType = "test_completed"
	EventTestStopped      TimelineEventType = "test_stopped"
	EventAnomalyDetected  TimelineEventType = "anomaly_detected"
	EventAnomalyResolved  TimelineEventType = "anomaly_resolved"
	EventHPAScaled        TimelineEventType = "hpa_scaled"
	EventHPAMaxedOut      TimelineEventType = "hpa_maxed_out"
	EventHighCPU          TimelineEventType = "high_cpu"
	EventHighErrorRate    TimelineEventType = "high_error_rate"
	EventScanCompleted    TimelineEventType = "scan_completed"
)

// Recommendation recomendação de ação
type Recommendation struct {
	Priority    RecommendationPriority
	Category    RecommendationCategory
	Target      string // cluster/namespace/hpa
	Title       string
	Description string
	Action      string
	Rationale   string
	Impact      string // Impacto esperado
}

// RecommendationPriority prioridade da recomendação
type RecommendationPriority string

const (
	PriorityImmediate RecommendationPriority = "immediate" // Ação imediata necessária
	PriorityHigh      RecommendationPriority = "high"      // Importante, resolver logo
	PriorityMedium    RecommendationPriority = "medium"    // Resolver eventualmente
	PriorityLow       RecommendationPriority = "low"       // Nice to have
)

// RecommendationCategory categoria da recomendação
type RecommendationCategory string

const (
	CategoryScaling      RecommendationCategory = "scaling"       // Ajuste de scaling
	CategoryResources    RecommendationCategory = "resources"     // Ajuste de resources
	CategoryConfiguration RecommendationCategory = "configuration" // Ajuste de config
	CategoryCode         RecommendationCategory = "code"          // Problema de código
	CategoryInfra        RecommendationCategory = "infrastructure" // Problema de infra
)

// NewStressTestMetrics cria nova instância de métricas de stress test
func NewStressTestMetrics(testName string, startTime time.Time, scanInterval time.Duration) *StressTestMetrics {
	return &StressTestMetrics{
		TestName:       testName,
		StartTime:      startTime,
		Status:         StressTestStatusRunning,
		ScanInterval:   scanInterval,
		HPAMetrics:     make(map[string]*HPAStressMetrics),
		Timeline:       make([]TimelineEvent, 0),
		CriticalIssues: make([]StressTestIssue, 0),
		WarningIssues:  make([]StressTestIssue, 0),
		InfoIssues:     make([]StressTestIssue, 0),
		Recommendations: make([]Recommendation, 0),
	}
}

// RecordEvent registra evento na timeline
func (m *StressTestMetrics) RecordEvent(eventType TimelineEventType, cluster, namespace, hpa, description string, severity AlertSeverity, details map[string]interface{}) {
	event := TimelineEvent{
		Timestamp:   time.Now(),
		Type:        eventType,
		Cluster:     cluster,
		Namespace:   namespace,
		HPAName:     hpa,
		Description: description,
		Severity:    severity,
		Details:     details,
	}
	m.Timeline = append(m.Timeline, event)
}

// AddIssue adiciona problema detectado
func (m *StressTestMetrics) AddIssue(issue StressTestIssue) {
	switch issue.Severity {
	case SeverityCritical:
		m.CriticalIssues = append(m.CriticalIssues, issue)
	case SeverityWarning:
		m.WarningIssues = append(m.WarningIssues, issue)
	case SeverityInfo:
		m.InfoIssues = append(m.InfoIssues, issue)
	}

	// Registra na timeline
	m.RecordEvent(
		EventAnomalyDetected,
		issue.Cluster,
		issue.Namespace,
		issue.HPAName,
		issue.Description,
		issue.Severity,
		issue.Evidence,
	)
}

// AddRecommendation adiciona recomendação
func (m *StressTestMetrics) AddRecommendation(rec Recommendation) {
	m.Recommendations = append(m.Recommendations, rec)
}

// UpdatePeakMetrics atualiza métricas de pico
func (m *StressTestMetrics) UpdatePeakMetrics(hpaKey string, cpu, memory, errorRate, latency float64) {
	now := time.Now()

	// CPU
	if cpu > m.PeakMetrics.MaxCPUPercent {
		m.PeakMetrics.MaxCPUPercent = cpu
		m.PeakMetrics.MaxCPUHPA = hpaKey
		m.PeakMetrics.MaxCPUTime = now
	}

	// Memory
	if memory > m.PeakMetrics.MaxMemoryPercent {
		m.PeakMetrics.MaxMemoryPercent = memory
		m.PeakMetrics.MaxMemoryHPA = hpaKey
		m.PeakMetrics.MaxMemoryTime = now
	}

	// Error Rate
	if errorRate > m.PeakMetrics.MaxErrorRate {
		m.PeakMetrics.MaxErrorRate = errorRate
		m.PeakMetrics.MaxErrorRateHPA = hpaKey
		m.PeakMetrics.MaxErrorRateTime = now
	}

	// Latency
	if latency > m.PeakMetrics.MaxLatencyP95 {
		m.PeakMetrics.MaxLatencyP95 = latency
		m.PeakMetrics.MaxLatencyP95HPA = hpaKey
		m.PeakMetrics.MaxLatencyP95Time = now
	}
}

// Complete marca teste como completo
func (m *StressTestMetrics) Complete() {
	m.Status = StressTestStatusCompleted
	m.EndTime = time.Now()
	m.Duration = m.EndTime.Sub(m.StartTime)

	// Calcula métricas finais
	m.TotalHPAsWithIssues = len(m.CriticalIssues) + len(m.WarningIssues)

	// Registra evento de conclusão
	m.RecordEvent(
		EventTestCompleted,
		"", "", "",
		"Stress test concluído",
		SeverityInfo,
		map[string]interface{}{
			"duration":     m.Duration.String(),
			"total_scans":  m.TotalScans,
			"total_hpas":   m.TotalHPAs,
			"hpas_with_issues": m.TotalHPAsWithIssues,
		},
	)
}

// GetHealthPercentage retorna percentual de HPAs saudáveis
func (m *StressTestMetrics) GetHealthPercentage() float64 {
	if m.TotalHPAsMonitored == 0 {
		return 0
	}
	healthyHPAs := m.TotalHPAsMonitored - m.TotalHPAsWithIssues
	return float64(healthyHPAs) / float64(m.TotalHPAsMonitored) * 100
}

// GetTestResult retorna resultado do teste (PASS/FAIL)
func (m *StressTestMetrics) GetTestResult() string {
	// Critérios de sucesso:
	// - Menos de 10% de HPAs com problemas críticos
	// - Nenhum HPA completamente failed

	criticalPercentage := float64(len(m.CriticalIssues)) / float64(m.TotalHPAsMonitored) * 100

	if criticalPercentage > 10 {
		return "FAIL"
	}

	// Verifica se há HPAs failed
	for _, hpaMetric := range m.HPAMetrics {
		if hpaMetric.Status == HPAStatusFailed {
			return "FAIL"
		}
	}

	return "PASS"
}
