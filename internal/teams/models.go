package teams

import "time"

// ApprovalItem representa um item de aprovação SRE extraído das mensagens do Mr.ViaBot.
type ApprovalItem struct {
	CHG           string    `json:"chg"`
	ServiceNowURL string    `json:"servicenow_url,omitempty"` // ex: https://viavarejo.service-now.com/nav_to.do?uri=change_request.do%3Fnumber%3DCHG0454511
	ApprovalURL   string    `json:"approval_url"`
	Description   string    `json:"description,omitempty"` // ex: "[Logística Abastecimento] supply-neogrid-integration-job - 0.0.4-27"
	ExtractedAt   time.Time `json:"extracted_at"`
	// PostedAt é quando a mensagem foi de fato postada no Teams (não quando nós rodamos a
	// extração) — vem do atributo `datetime` do <time> do DOM ou de composetime/
	// originalarrivaltime do IndexedDB (skypexspaces). Nil quando não foi possível capturar
	// (ex: mensagem só existe no fallback de leaf-node por regex, sem <time> próximo).
	PostedAt *time.Time `json:"posted_at,omitempty"`
}

// ExtractionResult é o resultado da extração de mensagens do Mr.ViaBot.
type ExtractionResult struct {
	Items       []ApprovalItem `json:"items"`
	ExtractedAt time.Time      `json:"extracted_at"`
	Source      string         `json:"source"` // "dom" | "indexeddb"
}
