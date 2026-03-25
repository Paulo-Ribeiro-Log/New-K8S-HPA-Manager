package finops

import "time"

// HoursPerMonth é a média de horas em um mês (365 × 24 / 12)
const HoursPerMonth = 730

// FinOpsReport é o resultado completo da análise de custo de um cluster
type FinOpsReport struct {
	Cluster      string           `json:"cluster"`
	GeneratedAt  time.Time        `json:"generated_at"`
	ExchangeRate float64          `json:"exchange_rate"`      // USD → BRL no momento
	ExchangeDate string           `json:"exchange_date"`       // data da cotação
	NodePools    []FinOpsPool     `json:"node_pools"`
	Namespaces   []FinOpsNamespace `json:"namespaces"`
	Workloads    []FinOpsWorkload  `json:"workloads"`
	Summary      FinOpsSummary    `json:"summary"`
}

// FinOpsPool representa um node pool com seu custo baseado no VM SKU real
type FinOpsPool struct {
	Name               string  `json:"name"`
	VMSize             string  `json:"vm_size"`
	VMCPUCores         int     `json:"vm_cpu_cores"`
	VMMemoryGB         int     `json:"vm_memory_gb"`
	VMPriceUSDHour     float64 `json:"vm_price_usd_hour"`
	PriceSource        string  `json:"price_source"` // "api" | "fallback"
	NodeCount          int     `json:"node_count"`
	Mode               string  `json:"mode"` // System | User
	MonthlyCostUSD     float64 `json:"monthly_cost_usd"`
	MonthlyCostBRL     float64 `json:"monthly_cost_brl"`
	TotalCPUMillicores int64   `json:"total_cpu_millicores"` // capacidade total do pool
	TotalMemoryMi      int64   `json:"total_memory_mi"`
}

// FinOpsNamespace agrega o custo alocado de todos os workloads de um namespace
type FinOpsNamespace struct {
	Namespace      string  `json:"namespace"`
	WorkloadCount  int     `json:"workload_count"`
	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl"`
}

// FinOpsWorkload representa um workload K8s com custo proporcional alocado
type FinOpsWorkload struct {
	Namespace         string  `json:"namespace"`
	Workload          string  `json:"workload"`
	Pods              int     `json:"pods"`
	CPURequestMillis  float64 `json:"cpu_request_millis"`
	MemRequestMi      float64 `json:"mem_request_mi"`
	CostShareUSD      float64 `json:"cost_share_usd"`  // alocação proporcional
	CostShareBRL      float64 `json:"cost_share_brl"`
	HPAMin            int     `json:"hpa_min"`
	HPAMax            int     `json:"hpa_max"`
	HPACurrent        int     `json:"hpa_current"`
	HPACostMinBRL     float64 `json:"hpa_cost_min_brl"`     // custo se ficasse no mínimo
	HPACostMaxBRL     float64 `json:"hpa_cost_max_brl"`     // custo se ficasse no máximo
	HPACostCurrentBRL float64 `json:"hpa_cost_current_brl"` // custo com replicas atuais
	// Campos opcionais — preenchidos na Fase 6 (Prometheus)
	CPUP95Millis float64 `json:"cpu_p95_millis,omitempty"`
	MemP95Mi     float64 `json:"mem_p95_mi,omitempty"`
	WasteBRL     float64 `json:"waste_brl,omitempty"`
	Verdict      string  `json:"verdict"` // "superprovisioned" | "ok" | "oom_risk" | "no_request"
}

// FinOpsSummary consolida os números mais importantes do relatório
type FinOpsSummary struct {
	TotalMonthlyCostBRL   float64 `json:"total_monthly_cost_brl"`
	TotalMonthlyCostUSD   float64 `json:"total_monthly_cost_usd"`
	TopNamespace          string  `json:"top_namespace"`
	PotentialSavingsBRL   float64 `json:"potential_savings_brl"`  // soma de WasteBRL (Fase 6)
	HPASavingsIfMinBRL    float64 `json:"hpa_savings_if_min_brl"` // economia se todos no mínimo
	WorkloadsAnalyzed     int     `json:"workloads_analyzed"`
	SuperprovisionedCount int     `json:"superprovisioned_count"`
	OOMRiskCount          int     `json:"oom_risk_count"`
	NoRequestCount        int     `json:"no_request_count"`
}
