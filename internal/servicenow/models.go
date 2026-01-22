package servicenow

import "time"

// ChangeRequest representa uma Change Request do ServiceNow
type ChangeRequest struct {
	SysID            string    `json:"sys_id"`
	Number           string    `json:"number"`
	ShortDescription string    `json:"short_description"`
	Description      string    `json:"description"`
	State            string    `json:"state"`
	OpenedAt         time.Time `json:"opened_at"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
}

// ChangeRequestResponse é a resposta da API ServiceNow
type ChangeRequestResponse struct {
	Result ChangeRequest `json:"result"`
}

// ExtractedData contém os dados extraídos do campo description
type ExtractedData struct {
	// Campos principais para GitHub Releases
	Application   string  `json:"application"`
	Version       string  `json:"version"`
	GitHubRepo    string  `json:"github_repo"`
	XLReleaseURL  string  `json:"xlrelease_url"`

	// Campos adicionais úteis
	Squad         string   `json:"squad"`
	Branch        string   `json:"branch"`
	JiraIssues    []string `json:"jira_issues"`
	Product       string   `json:"product"`
	Project       string   `json:"project"`
	XLReleaseTitle string  `json:"xlrelease_title"`
	Severity      string   `json:"severity"`

	// Confiança da extração (0.0 a 1.0)
	Confidence    float64 `json:"confidence"`
}

// ImportRequest é o request para importar dados de uma CHG
type ImportRequest struct {
	URL string `json:"url" binding:"required"`
}

// ImportResponse é a resposta da importação
type ImportResponse struct {
	Success       bool           `json:"success"`
	ChangeRequest *ChangeRequest `json:"change_request,omitempty"`
	ExtractedData *ExtractedData `json:"extracted_data,omitempty"`
	Error         string         `json:"error,omitempty"`
}
