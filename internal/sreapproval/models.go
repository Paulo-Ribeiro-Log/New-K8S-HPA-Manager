package sreapproval

import "time"

// ApprovalStatus status possíveis da aprovação
type ApprovalStatus string

const (
	StatusPending   ApprovalStatus = "PENDING"   // Aguardando aprovação
	StatusApproved  ApprovalStatus = "APPROVED"  // Aprovado
	StatusRejected  ApprovalStatus = "REJECTED"  // Rejeitado
	StatusFinalized ApprovalStatus = "FINALIZED" // Finalizado
)

// ApprovalInfo informações de uma solicitação de aprovação SRE
type ApprovalInfo struct {
	// Identificadores
	ID            string `json:"id"`             // UUID da solicitação (ex: 842b99dd-a40e-45c9-b8b8-9e45d0c887d0)
	ChangeNumber  string `json:"change_number"`  // Número da CHG (ex: CHG0436696)

	// Informações do deploy
	Title         string `json:"title"`          // Título completo (ex: [Retira Rápido] gcb-order-details-api - 1.1.0-3)
	Application   string `json:"application"`    // Nome da aplicação (ex: gcb-order-details-api)
	Version       string `json:"version"`        // Versão (ex: 1.1.0-3)
	SquadOwner    string `json:"squad_owner"`    // Squad dona do deploy (ex: Retira Rápido)

	// Status da aprovação
	Status        ApprovalStatus `json:"status"`         // Status atual
	IsFinalized   bool           `json:"is_finalized"`   // Se já foi finalizada

	// Dados do aprovador (preenchido quando aprovado)
	ApproverEmail string     `json:"approver_email,omitempty"` // Email de quem aprovou
	ApproverSquad string     `json:"approver_squad,omitempty"` // Squad do aprovador (ex: SRE Logística)
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`    // Data/hora da aprovação
}

// ApprovalRequest request para verificar ou aprovar uma solicitação
type ApprovalRequest struct {
	ApprovalURL string `json:"approval_url" binding:"required"` // URL completa do SRE Approval
}

// ApprovalResponse resposta da API de aprovação
type ApprovalResponse struct {
	Success      bool          `json:"success"`
	ApprovalInfo *ApprovalInfo `json:"approval_info,omitempty"`
	Message      string        `json:"message,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// ApproveActionRequest request para executar a aprovação
type ApproveActionRequest struct {
	ApprovalURL   string `json:"approval_url" binding:"required"`
	ApproverEmail string `json:"approver_email,omitempty"` // Se vazio, usa email do Azure CLI
}

// ApproveActionResponse resposta da ação de aprovar
type ApproveActionResponse struct {
	Success          bool   `json:"success"`
	Message          string `json:"message,omitempty"`
	Error            string `json:"error,omitempty"`
	AlreadyFinalized bool   `json:"already_finalized,omitempty"`
	ApproverEmail    string `json:"approver_email,omitempty"`
}
