package incidentkb

import "time"

// Incident é um registro confirmado por um analista sobre um problema real
// encontrado em cluster — sintoma, causa raiz e (o mais importante) a
// resolução que de fato funcionou. Alimenta buscas futuras de "já vimos isso
// antes" durante novos diagnósticos.
type Incident struct {
	ID           string    `yaml:"id" json:"id"`
	CreatedAt    time.Time `yaml:"created_at" json:"created_at"`
	Author       string    `yaml:"author" json:"author"`
	Cluster      string    `yaml:"cluster" json:"cluster"`
	Namespace    string    `yaml:"namespace" json:"namespace"`
	ResourceType string    `yaml:"resource_type" json:"resource_type"`
	ResourceName string    `yaml:"resource_name" json:"resource_name"`
	Severity     string    `yaml:"severity" json:"severity"` // critical, high, medium, low
	Tags         []string  `yaml:"tags" json:"tags"`

	// SourceAnalysisID referencia a análise de IA que originou este registro,
	// se houver (AnalysisResult.ID) — vazio quando criado manualmente.
	SourceAnalysisID string `yaml:"source_analysis_id,omitempty" json:"source_analysis_id,omitempty"`

	// Symptom curto (1-2 frases) — o que foi observado.
	Symptom string `yaml:"-" json:"symptom"`

	// RootCause hipótese de causa raiz (geralmente vinda da IA, revisável).
	RootCause string `yaml:"-" json:"root_cause"`

	// Resolution o que de fato foi feito para resolver — o campo mais valioso,
	// só o analista consegue preencher com confiança.
	Resolution string `yaml:"-" json:"resolution"`
}

// SearchFilters restringe uma busca por incidentes.
type SearchFilters struct {
	Cluster      string
	Namespace    string
	ResourceType string
	Limit        int
}

// SearchResult é um Incident com sua pontuação de relevância pra query.
type SearchResult struct {
	Incident *Incident `json:"incident"`
	Score    int       `json:"score"`
}
