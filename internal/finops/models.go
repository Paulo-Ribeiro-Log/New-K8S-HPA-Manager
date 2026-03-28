package finops

import "time"

// HoursPerMonth é a média de horas em um mês (365 × 24 / 12)
const HoursPerMonth = 730

// SafetyMargin é a margem SRE aplicada sobre o P95 para o request recomendado (20%)
const SafetyMargin = 1.20

// FinOpsReport é o resultado completo da análise de custo de um cluster
type FinOpsReport struct {
	Cluster      string            `json:"cluster"`
	GeneratedAt  time.Time         `json:"generated_at"`
	ExchangeRate float64           `json:"exchange_rate"`
	ExchangeDate string            `json:"exchange_date"`
	WindowDays   int               `json:"window_days"`   // janela usada para análise Prometheus
	NodePools    []FinOpsPool      `json:"node_pools"`
	Namespaces   []FinOpsNamespace `json:"namespaces"`
	Workloads    []FinOpsWorkload  `json:"workloads"`
	Summary      FinOpsSummary     `json:"summary"`
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
	TotalCPUMillicores int64   `json:"total_cpu_millicores"`
	TotalMemoryMi      int64   `json:"total_memory_mi"`
}

// FinOpsNamespace agrega o custo alocado de todos os workloads de um namespace
type FinOpsNamespace struct {
	Namespace      string  `json:"namespace"`
	WorkloadCount  int     `json:"workload_count"`
	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl"`
}

// FinOpsWorkload representa um workload K8s com custo proporcional alocado.
// Os campos Prometheus (*) são preenchidos somente quando with_prometheus=true.
type FinOpsWorkload struct {
	Namespace         string  `json:"namespace"`
	Workload          string  `json:"workload"`
	Pods              int     `json:"pods"`
	CPURequestMillis  float64 `json:"cpu_request_millis"`  // request configurado (por pod)
	MemRequestMi      float64 `json:"mem_request_mi"`
	CPULimitMillis    float64 `json:"cpu_limit_millis,omitempty"`
	MemLimitMi        float64 `json:"mem_limit_mi,omitempty"`
	CostShareUSD      float64 `json:"cost_share_usd"`
	CostShareBRL      float64 `json:"cost_share_brl"`
	HPAMin            int     `json:"hpa_min"`
	HPAMax            int     `json:"hpa_max"`
	HPACurrent        int     `json:"hpa_current"`
	HPACostMinBRL     float64 `json:"hpa_cost_min_brl"`
	HPACostMaxBRL     float64 `json:"hpa_cost_max_brl"`
	HPACostCurrentBRL float64 `json:"hpa_cost_current_brl"`
	Verdict           string  `json:"verdict"` // no_request|ok|superprovisioned|oom_risk|hpa_removable

	// ── Prometheus: uso real CPU/Mem (últimos N dias) ─────────────────────────
	// Agregação: max P95 entre pods do workload, avg das médias entre pods.
	CPUAvgMillis         float64 `json:"cpu_avg_millis,omitempty"`
	CPUP95Millis         float64 `json:"cpu_p95_millis,omitempty"`
	CPURecommendedMillis float64 `json:"cpu_recommended_millis,omitempty"` // P95 × 1.20
	MemAvgMi             float64 `json:"mem_avg_mi,omitempty"`
	MemP95Mi             float64 `json:"mem_p95_mi,omitempty"`
	MemRecommendedMi     float64 `json:"mem_recommended_mi,omitempty"` // P95 × 1.20

	// ── Prometheus: histórico HPA (últimos N dias) ────────────────────────────
	HPAAvgReplicas     float64 `json:"hpa_avg_replicas,omitempty"`   // média de replicas no período
	HPAMaxObserved     int     `json:"hpa_max_observed,omitempty"`   // máximo visto (vs HPAMax config)
	HPAMinObserved     int     `json:"hpa_min_observed,omitempty"`   // mínimo visto
	HPAScaleEvents     int     `json:"hpa_scale_events,omitempty"`   // qtd de mudanças no período
	HPANeverScaled     bool    `json:"hpa_never_scaled,omitempty"`   // nunca saiu do mínimo → remover HPA

	// Custo estimado baseado em réplicas médias reais (mais preciso que snapshot)
	AvgReplicasCostBRL float64 `json:"avg_replicas_cost_brl,omitempty"`

	// Desperdício financeiro mensal: recursos provisionados além do P95×1.20
	WasteBRL float64 `json:"waste_brl,omitempty"`
}

// ClusterCapacity guarda a capacidade total do cluster (exportada para uso no enricher)
type ClusterCapacity struct {
	CPUMillicores int64
	MemoryMi      int64
}

// FinOpsSummary consolida os números mais importantes do relatório
type FinOpsSummary struct {
	TotalMonthlyCostBRL    float64 `json:"total_monthly_cost_brl"`
	TotalMonthlyCostUSD    float64 `json:"total_monthly_cost_usd"`
	TopNamespace           string  `json:"top_namespace"`
	PotentialSavingsBRL    float64 `json:"potential_savings_brl"`   // soma de WasteBRL (Prometheus)
	HPASavingsIfMinBRL     float64 `json:"hpa_savings_if_min_brl"`  // economia se todos HPA no mínimo
	WorkloadsAnalyzed      int     `json:"workloads_analyzed"`
	SuperprovisionedCount  int     `json:"superprovisioned_count"`
	OOMRiskCount           int     `json:"oom_risk_count"`
	NoRequestCount         int     `json:"no_request_count"`
	HPARemovableCount      int     `json:"hpa_removable_count"`     // HPAs que nunca escalaram
	FixedHighCostCount     int     `json:"fixed_high_cost_count"`   // workloads caros sem HPA (candidatos a adicionar HPA)
}
