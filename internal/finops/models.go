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
	WindowDays   int               `json:"window_days"` // janela usada para análise Prometheus
	NodePools    []FinOpsPool      `json:"node_pools"`
	Namespaces   []FinOpsNamespace `json:"namespaces"`
	Workloads    []FinOpsWorkload  `json:"workloads"`
	PVCs         []PVCCostItem     `json:"pvcs"`
	Storage      StorageSummary    `json:"storage"`
	Summary      FinOpsSummary     `json:"summary"`
	// NodeUsage é o uso current/top por node único onde os workloads rodam (best-effort, ver
	// ComputeNodeUsage em live_metrics.go) — só preenchido quando BuildReport recebe um
	// metricsClient não-nil (ver Calculator.BuildReport).
	NodeUsage []NodeUsage `json:"node_usage,omitempty"`
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
	// Custo de disco OS por pool (preenchido quando DiskPricer disponível)
	OSDiskSKU     string  `json:"os_disk_sku,omitempty"`
	OSDiskTier    string  `json:"os_disk_tier,omitempty"`
	OSDiskGB      int     `json:"os_disk_gb,omitempty"`
	OSDiskCostUSD float64 `json:"os_disk_cost_usd,omitempty"`
	OSDiskCostBRL float64 `json:"os_disk_cost_brl,omitempty"`
	TotalCostUSD  float64 `json:"total_cost_usd,omitempty"` // MonthlyCostUSD + OSDiskCostUSD
	TotalCostBRL  float64 `json:"total_cost_brl,omitempty"`
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
	Namespace        string  `json:"namespace"`
	Workload         string  `json:"workload"`
	Pods             int     `json:"pods"`
	CPURequestMillis float64 `json:"cpu_request_millis"` // request configurado POR POD (média entre os pods; total = × Pods)
	MemRequestMi     float64 `json:"mem_request_mi"`
	CPULimitMillis   float64 `json:"cpu_limit_millis,omitempty"`
	MemLimitMi       float64 `json:"mem_limit_mi,omitempty"`
	// NodePool é o pool com mais pods deste workload (best-effort — resolvido via label do node,
	// ver nodePoolLabelFromNode em calculator.go; vazio se o node não tiver label reconhecida).
	// Usado pra agregar uso real por pool na sugestão de tier de VM (ver SuggestVMTier).
	NodePool string `json:"node_pool,omitempty"`
	// NodeName é o node com mais pods deste workload (mesmo critério de NodePool, mas o nome
	// literal do node — usado pra correlacionar com NodeUsage, ver live_metrics.go).
	NodeName          string  `json:"node_name,omitempty"`
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
	CPUMaxMillis         float64 `json:"cpu_max_millis,omitempty"`         // pico observado (max_over_time), espelha MemMaxMi
	MemAvgMi             float64 `json:"mem_avg_mi,omitempty"`
	MemP95Mi             float64 `json:"mem_p95_mi,omitempty"`
	MemRecommendedMi     float64 `json:"mem_recommended_mi,omitempty"` // P95 × 1.20
	MemMaxMi             float64 `json:"mem_max_mi,omitempty"`         // pico observado (max_over_time) — só usado pra MemLimitRecommendedMi
	// Data/hora em que o pico (CPUMaxMillis/MemMaxMi) foi observado — sem isso, um "top" sozinho
	// não diz se é de ontem ou de 29 dias atrás. nil quando a fonte é Dynatrace (a API de métricas
	// usada aqui só devolve um valor agregado por janela, sem timestamp do ponto exato — só
	// Prometheus, via QueryRange, tem esse dado) ou quando não há amostra no período.
	CPUMaxAt *time.Time `json:"cpu_max_at,omitempty"`
	MemMaxAt *time.Time `json:"mem_max_at,omitempty"`
	// OldestPodStartedAt é o CreationTimestamp do pod mais antigo ATUALMENTE rodando deste
	// workload — contextualiza o "tempo de vida" pra interpretar o pico: um pico observado há 20
	// dias não significa muito se todos os pods de hoje têm só 2h de vida (rollout recente, pod
	// que gerou o pico já não existe mais). nil se não houver nenhum pod Running no momento do scan.
	OldestPodStartedAt *time.Time `json:"oldest_pod_started_at,omitempty"`

	// ── Live (metrics-server): uso instantâneo agregado dos pods do workload ──
	// Diferente de CPUAvgMillis/CPUP95Millis (histórico via Prometheus/Dynatrace), estes vêm de
	// uma chamada direta à metrics.k8s.io no instante do scan — "current" de verdade, não P95.
	// Best-effort: fica zerado se o metrics-server não estiver disponível no cluster (ver
	// live_metrics.go), nunca bloqueia o resto do relatório.
	CPUCurrentMillis float64 `json:"cpu_current_millis,omitempty"`
	MemCurrentMi     float64 `json:"mem_current_mi,omitempty"`

	// ── Limit recomendado (novo) ───────────────────────────────────────────────
	// Mem: max(P95, pico observado) × 1.3 — protege contra OOMKill mesmo em spike acima do P95
	// (estourar Mem limit mata o pod). CPU: novo request recomendado × proporção limit/request
	// já configurada hoje (preserva a folga de burst que o time já tolerava); sem limit
	// configurado, usa proporção default 2× (estourar CPU limit só throttla, nunca derruba).
	CPULimitRecommendedMillis float64 `json:"cpu_limit_recommended_millis,omitempty"`
	MemLimitRecommendedMi     float64 `json:"mem_limit_recommended_mi,omitempty"`

	// ── Prometheus: histórico HPA (últimos N dias) ────────────────────────────
	HPAAvgReplicas float64 `json:"hpa_avg_replicas,omitempty"` // média de replicas no período
	HPAMaxObserved int     `json:"hpa_max_observed,omitempty"` // máximo visto (vs HPAMax config)
	HPAMinObserved int     `json:"hpa_min_observed,omitempty"` // mínimo visto
	HPAScaleEvents int     `json:"hpa_scale_events,omitempty"` // qtd de mudanças no período
	HPANeverScaled bool    `json:"hpa_never_scaled,omitempty"` // nunca saiu do mínimo → remover HPA

	// Custo estimado baseado em réplicas médias reais (mais preciso que snapshot)
	AvgReplicasCostBRL float64 `json:"avg_replicas_cost_brl,omitempty"`

	// Desperdício financeiro mensal: recursos provisionados além do P95×1.20
	WasteBRL      float64 `json:"waste_brl,omitempty"`
	MetricsSource string  `json:"metrics_source,omitempty"` // "dynatrace" | "prometheus" | ""

	// ── Storage: PVCs correlacionados a este workload ─────────────────────────
	StorageCostUSD float64 `json:"storage_cost_usd,omitempty"`
	StorageCostBRL float64 `json:"storage_cost_brl,omitempty"`
	PVCCount       int     `json:"pvc_count,omitempty"`
	PVCCapacityGB  float64 `json:"pvc_capacity_gb,omitempty"`
}

// NodeUsage é o uso de CPU/Mem de um node específico, combinando "current" (live, via
// metrics-server) e "top" (pico histórico, via Prometheus — best-effort, omitido quando
// Prometheus não está disponível). Computado 1x por node único (não por workload) em
// live_metrics.go, correlacionado ao FinOpsWorkload.NodeName de cada workload no frontend.
type NodeUsage struct {
	NodeName         string  `json:"node_name"`
	NodePool         string  `json:"node_pool,omitempty"`
	CPUCapMillis     float64 `json:"cpu_cap_millis,omitempty"`
	MemCapMi         float64 `json:"mem_cap_mi,omitempty"`
	CPUCurrentPct    float64 `json:"cpu_current_pct,omitempty"` // uso live agora, % da capacidade
	MemCurrentPct    float64 `json:"mem_current_pct,omitempty"`
	CPUTopPct        float64 `json:"cpu_top_pct,omitempty"` // pico histórico (Prometheus), % da capacidade
	MemTopPct        float64 `json:"mem_top_pct,omitempty"`
	MetricsAvailable bool    `json:"metrics_available"`
	MetricsError     string  `json:"metrics_error,omitempty"`
	// Data/hora em que CPUTopPct/MemTopPct foram observados — crítico pra nodes efêmeros (ex:
	// spot, que podem ser evictados e recriados com outro nome a qualquer momento): sem isso,
	// não dá pra saber se o "top" é de agora ou de um momento qualquer nos últimos N dias.
	CPUTopAt *time.Time `json:"cpu_top_at,omitempty"`
	MemTopAt *time.Time `json:"mem_top_at,omitempty"`
	// NodeCreatedAt é o CreationTimestamp do node (K8s Node object) — "desde quando ele existe".
	// Nodes spot/preemptible costumam ser recriados com frequência (eviction); um node muito
	// jovem explica por que o "top" pode estar ausente/limitado — não há histórico suficiente no
	// Prometheus pra aquele nome de node específico ainda.
	NodeCreatedAt *time.Time `json:"node_created_at,omitempty"`
}

// ClusterCapacity guarda a capacidade total do cluster (exportada para uso no enricher)
type ClusterCapacity struct {
	CPUMillicores int64
	MemoryMi      int64
}

// FinOpsSummary consolida os números mais importantes do relatório
type FinOpsSummary struct {
	TotalMonthlyCostBRL   float64 `json:"total_monthly_cost_brl"`
	TotalMonthlyCostUSD   float64 `json:"total_monthly_cost_usd"`
	TopNamespace          string  `json:"top_namespace"`
	PotentialSavingsBRL   float64 `json:"potential_savings_brl"`  // soma de WasteBRL (Prometheus)
	HPASavingsIfMinBRL    float64 `json:"hpa_savings_if_min_brl"` // economia se todos HPA no mínimo
	WorkloadsAnalyzed     int     `json:"workloads_analyzed"`
	SuperprovisionedCount int     `json:"superprovisioned_count"`
	OOMRiskCount          int     `json:"oom_risk_count"`
	NoRequestCount        int     `json:"no_request_count"`
	HPARemovableCount     int     `json:"hpa_removable_count"`   // HPAs que nunca escalaram
	FixedHighCostCount    int     `json:"fixed_high_cost_count"` // workloads caros sem HPA
	// NoDataCount — verdict "sem_dados" (ver BuildReport): workloads que nunca receberam
	// enriquecimento de uso real (MetricsSource==""), distintos de "ok" (verificado e saudável).
	// Não contam como desperdício/risco nem como "eficiente" — são simplesmente desconhecidos.
	NoDataCount int `json:"no_data_count"`
	// Storage totals (preenchidos quando DiskPricer disponível)
	StorageMonthlyCostBRL  float64 `json:"storage_monthly_cost_brl,omitempty"`
	StorageMonthlyCostUSD  float64 `json:"storage_monthly_cost_usd,omitempty"`
	OSDiskCostBRL          float64 `json:"os_disk_cost_brl,omitempty"`
	OrphanedStorageCostBRL float64 `json:"orphaned_storage_cost_brl,omitempty"`
	TotalWithStorageBRL    float64 `json:"total_with_storage_brl,omitempty"`

	// MetricsAttempted/MetricsWorkloadsEnriched — bug real corrigido, relatado pelo usuário via
	// um scan onde TODOS os workloads e node pools vieram com desperdício R$0, CPU/Mem 0%, "Com
	// Oportunidade 0" — indistinguível, na UI, de "cluster genuinamente sem nenhum desperdício",
	// quando na real era falha SILENCIOSA de coleta (Prometheus/Dynatrace indisponível ou erro de
	// rede no momento do scan — antes só logada como Warn no servidor, nunca chegava na resposta
	// da API). MetricsAttempted=true (DT ou Prometheus configurado/tentado) combinado com
	// MetricsWorkloadsEnriched==0 e WorkloadsAnalyzed>0 é o sinal de falha — extremamente
	// improvável que TODOS os workloads de um cluster real tenham uso zero ao mesmo tempo; o
	// frontend usa essa combinação pra mostrar um aviso em vez de fingir que os números (0%,
	// R$0, "Com Oportunidade 0") são confiáveis. MetricsAttempted=false é o caso normal/
	// deliberado de "sem Prometheus" (with_prometheus=false e sem Dynatrace configurado) — não
	// deve gerar aviso nenhum.
	MetricsAttempted         bool `json:"metrics_attempted"`
	MetricsWorkloadsEnriched int  `json:"metrics_workloads_enriched"`

	// MetricsCollectionError — bug real corrigido: quando MetricsAttempted && MetricsWorkloadsEnriched
	// == 0, a UI sempre assumia "provável falha de coleta TRANSITÓRIA (VPN/rede/API indisponível
	// no momento do scan)" — mas DTEnricher/PrometheusEnricher podiam retornar zero workloads
	// enriquecidos por dois motivos bem diferentes, indistinguíveis até agora: (a) a consulta de
	// fato falhou (timeout, conexão recusada, erro HTTP) — aí sim é transitório, "reanalisar em
	// alguns minutos" pode resolver; ou (b) a consulta teve sucesso (HTTP 200) mas o cluster
	// genuinamente não tem nenhuma métrica pra devolver (sem OneAgent Dynatrace instalado, sem
	// Prometheus com essas métricas) — um problema ESTRUTURAL de cobertura, que "reanalisar" não
	// resolve sozinho, precisa configurar DT/Prometheus pra este cluster. Preenchido (não-vazio)
	// só no caso (a) — DTEnricher.CollectionError()/PrometheusEnricher.CollectionErrors()
	// reportaram pelo menos um erro real; fica "" no caso (b) e quando MetricsWorkloadsEnriched >
	// 0 (nenhum sinal de falha).
	//
	// Deliberadamente SEM omitempty — precisa sempre aparecer no JSON (mesmo como "") pra o
	// frontend distinguir "consultei e não achei erro" (chave presente, vazia) de "relatório
	// antigo, cacheado em SQLite (ver GetLastReport) de antes deste campo existir" (chave
	// ausente) — com omitempty as duas situações ficariam indistinguíveis (`undefined` nos dois
	// casos), e o frontend cairia sempre na mensagem genérica de antes mesmo pra relatórios
	// frescos sem nenhum erro real.
	MetricsCollectionError string `json:"metrics_collection_error"`

	// MetricsCollectionTimeout — true quando a ÚNICA causa das falhas de coleta foram timeouts das
	// queries PESADAS de container do Prometheus (as leves de HPA responderam): o Prometheus está no
	// ar, o custo da consulta pra esta janela é que estoura o tempo. Diferente de rede/VPN fora —
	// a orientação certa é reanalisar com uma janela menor, não "esperar alguns minutos". Ver
	// PrometheusEnricher.HeavyQueryTimeoutsOnly.
	MetricsCollectionTimeout bool `json:"metrics_collection_timeout,omitempty"`
}

// ── Storage types ─────────────────────────────────────────────────────────────

// PVCCostItem representa um PVC com seu custo mensal calculado
type PVCCostItem struct {
	Namespace      string  `json:"namespace"`
	Name           string  `json:"name"`
	BoundPV        string  `json:"bound_pv"` // nome do PV vinculado
	StorageClass   string  `json:"storage_class"`
	CapacityGB     float64 `json:"capacity_gb"`
	AzureDiskType  string  `json:"azure_disk_type"` // "Premium SSD", "Azure Files Standard", etc.
	AzureDiskTier  string  `json:"azure_disk_tier"` // "P10", "E6" — vazio para Files/Blob
	PricePerMonth  float64 `json:"price_usd_month"` // preço base (por disco ou por GB)
	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl"`
	Phase          string  `json:"phase"`          // Bound | Pending | Lost
	ReclaimPolicy  string  `json:"reclaim_policy"` // Delete | Retain
	WorkloadRef    string  `json:"workload_ref"`   // "namespace/deployment" se detectável
	IsOrphaned     bool    `json:"is_orphaned"`    // true se nenhum pod montando
	PriceSource    string  `json:"price_source"`   // "api" | "fallback"
}

// StorageClassBreakdown agrega custo de PVCs por StorageClass
type StorageClassBreakdown struct {
	StorageClass   string  `json:"storage_class"`
	AzureType      string  `json:"azure_type"`
	PVCCount       int     `json:"pvc_count"`
	TotalGB        float64 `json:"total_gb"`
	MonthlyCostBRL float64 `json:"monthly_cost_brl"`
}

// StorageSummary consolida os números de armazenamento do relatório
type StorageSummary struct {
	TotalMonthlyCostUSD float64                 `json:"total_monthly_cost_usd"`
	TotalMonthlyCostBRL float64                 `json:"total_monthly_cost_brl"`
	OSDiskCostUSD       float64                 `json:"os_disk_cost_usd"`
	OSDiskCostBRL       float64                 `json:"os_disk_cost_brl"`
	PVCCount            int                     `json:"pvc_count"`
	BoundPVCCount       int                     `json:"bound_pvc_count"`
	OrphanedPVCCount    int                     `json:"orphaned_pvc_count"`
	OrphanedCostBRL     float64                 `json:"orphaned_cost_brl"`
	TotalCapacityGB     float64                 `json:"total_capacity_gb"`
	ByStorageClass      []StorageClassBreakdown `json:"by_storage_class"`
	ByNamespace         map[string]float64      `json:"by_namespace"`
}
