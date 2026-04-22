package teams

import "time"

// ApprovalItem representa um item de aprovação SRE extraído das mensagens do Mr.ViaBot.
type ApprovalItem struct {
	CHG           string    `json:"chg"`
	ServiceNowURL string    `json:"servicenow_url,omitempty"` // ex: https://viavarejo.service-now.com/nav_to.do?uri=change_request.do%3Fnumber%3DCHG0454511
	ApprovalURL   string    `json:"approval_url"`
	Description   string    `json:"description,omitempty"` // ex: "[Logística Abastecimento] supply-neogrid-integration-job - 0.0.4-27"
	ExtractedAt   time.Time `json:"extracted_at"`
}

// ExtractionResult é o resultado da extração de mensagens do Mr.ViaBot.
type ExtractionResult struct {
	Items       []ApprovalItem `json:"items"`
	ExtractedAt time.Time      `json:"extracted_at"`
	Source      string         `json:"source"` // "dom" | "indexeddb"
}
