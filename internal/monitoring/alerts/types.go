package alerts

import "time"

// Alert representa um alerta do Prometheus
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"` // firing, pending, inactive
	ActiveAt    *time.Time        `json:"activeAt,omitempty"`
	Value       string            `json:"value"`
}

// PrometheusAlertsResponse representa a resposta da API /api/v1/alerts
type PrometheusAlertsResponse struct {
	Status string `json:"status"`
	Data   struct {
		Alerts []Alert `json:"alerts"`
	} `json:"data"`
}

// AlertSummary é um resumo de alertas por severidade
type AlertSummary struct {
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
	Info     int `json:"info"`
	Total    int `json:"total"`
}

// HPAAlert representa um alerta correlacionado com um HPA
type HPAAlert struct {
	AlertName   string    `json:"alertName"`
	Severity    string    `json:"severity"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	ActiveAt    time.Time `json:"activeAt"`

	// Contexto do HPA
	Namespace  string `json:"namespace"`
	HPAName    string `json:"hpaName,omitempty"`
	Deployment string `json:"deployment,omitempty"`
	Pod        string `json:"pod,omitempty"`
	Container  string `json:"container,omitempty"`

	// Sugestão de ação
	Mitigation *MitigationSuggestion `json:"mitigation,omitempty"`
}

// NodePoolAlert representa um alerta correlacionado com um Node Pool
type NodePoolAlert struct {
	AlertName   string    `json:"alertName"`
	Severity    string    `json:"severity"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	ActiveAt    time.Time `json:"activeAt"`

	// Contexto do Node
	Node string `json:"node"`

	// Sugestão de ação
	Mitigation *MitigationSuggestion `json:"mitigation,omitempty"`
}

// MitigationSuggestion representa uma sugestão de ação para mitigar o alerta
type MitigationSuggestion struct {
	AutoMitigable bool     `json:"autoMitigable"` // Pode ser mitigado automaticamente?
	Action        string   `json:"action"`        // Descrição da ação sugerida
	Priority      string   `json:"priority"`      // critical, high, medium, low
	Steps         []string `json:"steps"`         // Passos específicos
}

// AlertFilter representa filtros para buscar alertas
type AlertFilter struct {
	Severity   []string // critical, warning, info
	State      []string // firing, pending, inactive
	Namespace  string
	AlertNames []string
}
