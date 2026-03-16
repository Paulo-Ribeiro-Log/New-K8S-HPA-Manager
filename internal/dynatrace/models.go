package dynatrace

import "time"

// ManagementZone management zone associada ao problem (corresponde ao alerting profile do squad)
type ManagementZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Problem representa um problema detectado pelo Dynatrace (API v2)
type Problem struct {
	ProblemID        string    `json:"problemId"`
	DisplayID        string    `json:"displayId"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`        // OPEN | CLOSED
	SeverityLevel    string    `json:"severityLevel"` // AVAILABILITY | ERROR | PERFORMANCE | RESOURCE_CONTENTION | CUSTOM_ALERT
	ImpactLevel      string    `json:"impactLevel"`   // APPLICATION | ENVIRONMENT | INFRASTRUCTURE | SERVICE
	StartTime        time.Time `json:"startTime"`
	EndTime          *time.Time `json:"endTime,omitempty"`
	AffectedEntities []EntityStub     `json:"affectedEntities"`
	ImpactedEntities []EntityStub     `json:"impactedEntities"`
	RootCauseEntity  *EntityStub      `json:"rootCauseEntity,omitempty"`
	ManagementZones  []ManagementZone `json:"managementZones,omitempty"`
}

// EntityStub referência simplificada a uma entidade Dynatrace
type EntityStub struct {
	EntityID    EntityID `json:"entityId"`
	DisplayName string   `json:"displayName,omitempty"`
	// Tags K8s extraídas do OneAgent (preenchidas após GetEntity)
	K8sCluster   string `json:"k8sCluster,omitempty"`
	K8sNamespace string `json:"k8sNamespace,omitempty"`
	K8sWorkload  string `json:"k8sWorkload,omitempty"`
}

// EntityID identificador de uma entidade Dynatrace
type EntityID struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Entity entidade completa com propriedades e tags
type Entity struct {
	EntityID    string            `json:"entityId"`
	DisplayName string            `json:"displayName"`
	Type        string            `json:"type"`
	Tags        []Tag             `json:"tags"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	// Relações de topologia
	FromRelationships map[string][]EntityStub `json:"fromRelationships,omitempty"`
	ToRelationships   map[string][]EntityStub `json:"toRelationships,omitempty"`
}

// Tag tag de uma entidade Dynatrace
type Tag struct {
	Context string `json:"context"`
	Key     string `json:"key"`
	Value   string `json:"value,omitempty"`
}

// EvidenceDetails detalhes de evidências do problema
type EvidenceDetails struct {
	TotalCount int        `json:"totalCount"`
	Details    []Evidence `json:"details"`
}

// Evidence uma evidência específica do problema
type Evidence struct {
	EvidenceType  string      `json:"evidenceType"`
	DisplayName   string      `json:"displayName"`
	Entity        EntityStub  `json:"entity"`
	GroupingEntity *EntityStub `json:"groupingEntity,omitempty"`
	RootCause     bool        `json:"rootCause"`
	StartTime     time.Time   `json:"startTime"`
}

// MetricData série temporal de uma métrica
type MetricData struct {
	MetricID   string         `json:"metricId"`
	Resolution string         `json:"resolution"`
	Data       []MetricSeries `json:"data"`
}

// MetricSeries pontos de uma série temporal
type MetricSeries struct {
	DimensionMap map[string]string `json:"dimensionMap"`
	Timestamps   []int64           `json:"timestamps"`
	Values       []float64         `json:"values"`
}

// Event evento registrado no Dynatrace
type Event struct {
	EventID         string    `json:"eventId"`
	EventType       string    `json:"eventType"`
	Title           string    `json:"title"`
	StartTime       time.Time `json:"startTime"`
	EndTime         *time.Time `json:"endTime,omitempty"`
	EntityID        string    `json:"entityId"`
	EntityName      string    `json:"entityName,omitempty"`
	Properties      map[string]string `json:"properties,omitempty"`
}

// K8sCorrelation correlação entre entidade Dynatrace e recurso K8s
// extraída automaticamente das tags do OneAgent
type K8sCorrelation struct {
	Cluster   string // kubernetes.cluster.name
	Namespace string // kubernetes.namespace.name
	Workload  string // kubernetes.workload.name
	PodName   string // kubernetes.pod.name (opcional)
}

// ExtractK8sCorrelation extrai correlação K8s das tags de uma entidade.
// O OneAgent injeta essas tags automaticamente em todos os processos K8s.
func (e *Entity) ExtractK8sCorrelation() *K8sCorrelation {
	corr := &K8sCorrelation{}
	for _, tag := range e.Tags {
		switch tag.Key {
		case "kubernetes.cluster.name":
			corr.Cluster = tag.Value
		case "kubernetes.namespace.name":
			corr.Namespace = tag.Value
		case "kubernetes.workload.name":
			corr.Workload = tag.Value
		case "kubernetes.pod.name":
			corr.PodName = tag.Value
		}
	}
	if corr.Cluster == "" && corr.Namespace == "" && corr.Workload == "" {
		return nil
	}
	return corr
}

// ProblemSummary resumo de um problem com correlação K8s já resolvida
type ProblemSummary struct {
	ProblemID     string     `json:"problemId"`
	DisplayID     string     `json:"displayId"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	SeverityLevel string     `json:"severityLevel"`
	ImpactLevel   string     `json:"impactLevel"`
	StartTime     time.Time  `json:"startTime"`
	EndTime       *time.Time `json:"endTime,omitempty"`
	// Entidades afetadas com correlação K8s
	AffectedEntities []EntityStub     `json:"affectedEntities"`
	ImpactedEntities []EntityStub     `json:"impactedEntities,omitempty"`
	RootCauseEntity  *EntityStub      `json:"rootCauseEntity,omitempty"`
	ManagementZones  []ManagementZone `json:"managementZones,omitempty"`
	// Correlações K8s únicas encontradas neste problem
	K8sWorkloads []K8sCorrelation `json:"k8sWorkloads,omitempty"`
}
