package dynatrace

import "time"

// ManagementZone management zone associada ao problem (corresponde ao alerting profile do squad)
type ManagementZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Problem representa um problema detectado pelo Dynatrace (API v2)
type Problem struct {
	ProblemID        string           `json:"problemId"`
	DisplayID        string           `json:"displayId"`
	Title            string           `json:"title"`
	Status           string           `json:"status"`        // OPEN | CLOSED
	SeverityLevel    string           `json:"severityLevel"` // AVAILABILITY | ERROR | PERFORMANCE | RESOURCE_CONTENTION | CUSTOM_ALERT
	ImpactLevel      string           `json:"impactLevel"`   // APPLICATION | ENVIRONMENT | INFRASTRUCTURE | SERVICE
	StartTime        time.Time        `json:"startTime"`
	EndTime          *time.Time       `json:"endTime,omitempty"`
	AffectedEntities []EntityStub     `json:"affectedEntities"`
	ImpactedEntities []EntityStub     `json:"impactedEntities"`
	RootCauseEntity  *EntityStub      `json:"rootCauseEntity,omitempty"`
	ManagementZones  []ManagementZone `json:"managementZones,omitempty"`
}

// EntityStub referência simplificada a uma entidade Dynatrace
type EntityStub struct {
	EntityID EntityID `json:"entityId"`
	// DisplayName vem da Entity API (GetEntity). Name vem da Problems API (campo "name" nos stubs).
	// A Problems API usa "name", a Entity API usa "displayName" — mapeamos os dois.
	DisplayName string `json:"displayName,omitempty"`
	Name        string `json:"name,omitempty"`
	// Tags K8s básicas (preenchidas após GetEntity via ExtractK8sCorrelation)
	K8sCluster   string `json:"k8sCluster,omitempty"`
	K8sNamespace string `json:"k8sNamespace,omitempty"`
	K8sWorkload  string `json:"k8sWorkload,omitempty"`
	// Tags ricas do OneAgent — DevOps, versão, squad, GitHub, etc.
	Labels *DTLabels `json:"labels,omitempty"`
	// Relações de topologia (preenchidas após GetEntity)
	// CallsTo: entidades que esta entidade chama (downstream)
	// CalledBy: entidades que chamam esta entidade (upstream)
	CallsTo  []EntityID `json:"callsTo,omitempty"`
	CalledBy []EntityID `json:"calledBy,omitempty"`
}

// BestName retorna o melhor nome disponível para a entidade, na ordem de preferência.
func (e *EntityStub) BestName() string {
	if e.DisplayName != "" {
		return e.DisplayName
	}
	if e.Name != "" {
		return e.Name
	}
	return e.EntityID.ID
}

// EntityID identificador de uma entidade Dynatrace
type EntityID struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Entity entidade completa com propriedades e tags
type Entity struct {
	EntityID    string                 `json:"entityId"`
	DisplayName string                 `json:"displayName"`
	Type        string                 `json:"type"`
	Tags        []Tag                  `json:"tags"`
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
	EvidenceType   string      `json:"evidenceType"`
	DisplayName    string      `json:"displayName"`
	Entity         EntityStub  `json:"entity"`
	GroupingEntity *EntityStub `json:"groupingEntity,omitempty"`
	RootCause      bool        `json:"rootCause"`
	StartTime      time.Time   `json:"startTime"`
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
	EventID    string            `json:"eventId"`
	EventType  string            `json:"eventType"`
	Title      string            `json:"title"`
	StartTime  time.Time         `json:"startTime"`
	EndTime    *time.Time        `json:"endTime,omitempty"`
	EntityID   string            `json:"entityId"`
	EntityName string            `json:"entityName,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// DTLabels tags Kubernetes e DevOps extraídas do OneAgent.
// Permitem correlacionar automaticamente entidades Dynatrace com deployments K8s e releases GitHub.
type DTLabels struct {
	// Identificação da aplicação (app.kubernetes.io/*)
	AppName        string `json:"appName,omitempty"`        // app.kubernetes.io/name
	AppVersion     string `json:"appVersion,omitempty"`     // app.kubernetes.io/version
	AppEnvironment string `json:"appEnvironment,omitempty"` // app.kubernetes.io/environment (prd|hlg|dev)
	AppInstance    string `json:"appInstance,omitempty"`    // app.kubernetes.io/instance
	ReleaseName    string `json:"releaseName,omitempty"`    // app.kubernetes.io/release-name
	Stage          string `json:"stage,omitempty"`          // app.kubernetes.io/stage (stable|canary)
	DeployedBy     string `json:"deployedBy,omitempty"`     // app.kubernetes.io/deployed-by (spinnaker-helm)
	IsCanary       string `json:"isCanary,omitempty"`       // app.kubernetes.io/deployed-by-canary

	// Infra K8s
	Namespace string `json:"namespace,omitempty"` // k8s.namespace.name
	HostGroup string `json:"hostGroup,omitempty"` // dt.host_group.id → identifica cluster AKS (ex: akspriv-busca-prd)

	// DevOps / rastreabilidade (devops.k8s.io/*)
	GitHubRepoID     string `json:"githubRepoId,omitempty"`     // devops.k8s.io/github-repository-id
	ComponentName    string `json:"componentName,omitempty"`    // devops.k8s.io/component-name
	ComponentSquad   string `json:"componentSquad,omitempty"`   // devops.k8s.io/component-squad
	ComponentJourney string `json:"componentJourney,omitempty"` // devops.k8s.io/component-journey
	ComponentTribe   string `json:"componentTribe,omitempty"`   // devops.k8s.io/component-tribe

	// Helm
	HelmChart string `json:"helmChart,omitempty"` // helm.sh/chart
}

// K8sCorrelation correlação entre entidade Dynatrace e recurso K8s
// extraída automaticamente das tags do OneAgent
type K8sCorrelation struct {
	Cluster   string `json:"Cluster"`
	Namespace string `json:"Namespace"`
	Workload  string `json:"Workload"`
	PodName   string `json:"PodName,omitempty"`
	// Campos extras extraídos das DTLabels — úteis no Health Check e correlação com GitHub releases
	AppName      string `json:"AppName,omitempty"`
	AppVersion   string `json:"AppVersion,omitempty"`
	GitHubRepoID string `json:"GitHubRepoID,omitempty"`
	Environment  string `json:"Environment,omitempty"`
}

// ExtractK8sCorrelation extrai correlação K8s das tags de uma entidade.
// O OneAgent injeta essas tags automaticamente em todos os processos K8s.
// Usa múltiplas fontes: kubernetes.*, k8s.namespace.name, dt.host_group.id, app.kubernetes.io/*
func (e *Entity) ExtractK8sCorrelation() *K8sCorrelation {
	corr := &K8sCorrelation{}
	for _, tag := range e.Tags {
		switch tag.Key {
		// Fontes primárias (Kubernetes context)
		case "kubernetes.cluster.name":
			corr.Cluster = tag.Value
		case "kubernetes.namespace.name", "k8s.namespace.name":
			if corr.Namespace == "" {
				corr.Namespace = tag.Value
			}
		case "kubernetes.workload.name":
			corr.Workload = tag.Value
		case "kubernetes.pod.name":
			corr.PodName = tag.Value
		// Cluster via host_group (AKS: "akspriv-busca-prd")
		case "dt.host_group.id":
			if corr.Cluster == "" {
				corr.Cluster = tag.Value
			}
		// Workload via app.kubernetes.io/name (fallback)
		case "app.kubernetes.io/name":
			corr.AppName = tag.Value
			if corr.Workload == "" {
				corr.Workload = tag.Value
			}
		// Namespace via etiqueta Kubernetes
		case "Namespace":
			if corr.Namespace == "" {
				corr.Namespace = tag.Value
			}
		// Versão e GitHub
		case "app.kubernetes.io/version":
			corr.AppVersion = tag.Value
		case "devops.k8s.io/github-repository-id":
			corr.GitHubRepoID = tag.Value
		case "app.kubernetes.io/environment":
			corr.Environment = tag.Value
		}
	}
	if corr.Cluster == "" && corr.Namespace == "" && corr.Workload == "" {
		return nil
	}
	return corr
}

// ExtractDTLabels extrai todas as tags ricas (DevOps + K8s) de uma entidade.
func (e *Entity) ExtractDTLabels() *DTLabels {
	l := &DTLabels{}
	hasAny := false
	for _, tag := range e.Tags {
		switch tag.Key {
		case "app.kubernetes.io/name":
			l.AppName = tag.Value
			hasAny = true
		case "app.kubernetes.io/version":
			l.AppVersion = tag.Value
			hasAny = true
		case "app.kubernetes.io/environment":
			l.AppEnvironment = tag.Value
			hasAny = true
		case "app.kubernetes.io/instance":
			l.AppInstance = tag.Value
			hasAny = true
		case "app.kubernetes.io/release-name":
			l.ReleaseName = tag.Value
			hasAny = true
		case "app.kubernetes.io/stage":
			l.Stage = tag.Value
			hasAny = true
		case "app.kubernetes.io/deployed-by":
			l.DeployedBy = tag.Value
			hasAny = true
		case "app.kubernetes.io/deployed-by-canary":
			l.IsCanary = tag.Value
			hasAny = true
		case "k8s.namespace.name", "kubernetes.namespace.name", "Namespace":
			if l.Namespace == "" {
				l.Namespace = tag.Value
				hasAny = true
			}
		case "dt.host_group.id":
			l.HostGroup = tag.Value
			hasAny = true
		case "devops.k8s.io/github-repository-id":
			l.GitHubRepoID = tag.Value
			hasAny = true
		case "devops.k8s.io/component-name":
			l.ComponentName = tag.Value
			hasAny = true
		case "devops.k8s.io/component-squad":
			l.ComponentSquad = tag.Value
			hasAny = true
		case "devops.k8s.io/component-journey":
			l.ComponentJourney = tag.Value
			hasAny = true
		case "devops.k8s.io/component-tribe":
			l.ComponentTribe = tag.Value
			hasAny = true
		case "helm.sh/chart":
			l.HelmChart = tag.Value
			hasAny = true
		}
	}
	if !hasAny {
		return nil
	}
	return l
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
