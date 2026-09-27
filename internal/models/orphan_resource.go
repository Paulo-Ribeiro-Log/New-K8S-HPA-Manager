package models

// ScopedResourceGroup é um Resource Group do escopo de uma jornada (FinOps → Recursos órfãos):
// RG com a tag "jornada", RG de app de um cluster da jornada, node resource group (MC_*) desse
// cluster ou o RG de dados rg-<nome>-data-<env>.
type ScopedResourceGroup struct {
	SubscriptionID string `json:"subscription_id"`
	ResourceGroup  string `json:"resource_group"`
	Journey        string `json:"journey,omitempty"`     // jornada atribuída ao RG (tag, ou a do cluster)
	JourneyTag     string `json:"journey_tag,omitempty"` // valor da tag "jornada" do próprio RG
	Source         string `json:"source"`                // "tag" | "cluster" | "node" | "data"
	Cluster        string `json:"cluster,omitempty"`     // cluster que trouxe o RG (sources cluster/node/data)
	// Ambiente do RG (prd/hlg/...): da tag de ambiente (EnvTag) ou do nome. O escopo só mantém RGs
	// do mesmo ambiente do cluster analisado — HLG e PRD de uma jornada não se misturam.
	Environment string `json:"environment,omitempty"`
	EnvTag      string `json:"env_tag,omitempty"`
	// ExcludedReason: preenchido nos RGs deixados de fora (lista "ignorados" da resposta).
	ExcludedReason string `json:"excluded_reason,omitempty"`
}

// OrphanResource é um recurso Azure sem uso — desatachado ou sem conexões — nos RGs de uma
// jornada. Discos ficam de fora (relatório de discos, com cruzamento de PVs); aqui entram os
// demais tipos (NIC, IP público, Private Endpoint, VM desalocada, NSG, LB...).
type OrphanResource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"` // tipo ARM, ex: "microsoft.network/publicipaddresses"
	Kind           string `json:"kind,omitempty"`
	Location       string `json:"location,omitempty"`
	ResourceGroup  string `json:"resource_group"`
	SubscriptionID string `json:"subscription_id"`
	Journey        string `json:"journey,omitempty"`
	// JourneySource: de onde veio a jornada — "resource_tag" (tag do recurso), "rg_tag" (tag do RG),
	// "node_rg" (node RG MC_* do cluster, exclusivo dele) ou "none".
	JourneySource string            `json:"journey_source,omitempty"`
	Reason        string            `json:"reason"` // por que é considerado órfão
	SKU           string            `json:"sku,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`

	// Há quanto tempo está assim. O Azure não registra "desatachado desde" para esses tipos: a
	// referência é a última alteração do recurso na tabela resourcechanges do Resource Graph
	// (retenção de ~14 dias). SinceBasis: "last_change" (AgeDays = dias desde LastChange) ou
	// "no_change_14d" (nenhuma alteração na janela — AgeDays é o piso de 14).
	LastChange string `json:"last_change,omitempty"`
	SinceBasis string `json:"since_basis"`
	AgeDays    int    `json:"age_days"`
	// Recent: alterado nos últimos 7 dias — pode ter ficado órfão há pouco (ex: troca de VM).
	Recent bool `json:"recent"`

	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl"`
	PriceNote      string  `json:"price_note,omitempty"`
	DeleteCommand  string  `json:"delete_command,omitempty"`
}
