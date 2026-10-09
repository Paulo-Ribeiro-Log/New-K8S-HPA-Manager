package finops

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Deep Analysis de node pool — ver FINOPS-DEEP-ANALYSIS-PLAN.md.
//
// BuildPoolDeepAnalysis é PURA: recebe tudo já coletado (DeepAnalysisInput — nodes e pods do pool ao
// vivo, histórico do último relatório em cache, preços das SKUs) e devolve o diagnóstico. A coleta fica
// em deep_analysis_collect.go e a orquestração no handler, para a lógica ser testável com fixtures.

const (
	// DeepDefaultHeadroom é a ocupação-alvo dos nodes na simulação (80% do espaço livre).
	DeepDefaultHeadroom = 0.80
	// deepMinNodes é o mínimo de nodes sugerido para um pool de produção (perder 1 node = 1/3).
	deepMinNodes = 3
	// deepMinCPURecMillis é o piso do request de CPU recomendado.
	deepMinCPURecMillis = 50
	// deepMemLimitFactor espelha recommendedLimits: limit de memória = max(P95, pico) × 1,3.
	deepMemLimitFactor = 1.3
	// deepNodeLossWarnPct: acima disso, perder um node derruba uma fatia grande demais do pool.
	deepNodeLossWarnPct = 15.0
	// deepThrottleWarnPct: P95 de períodos de CPU com throttling a partir do qual o workload é
	// sinalizado (latência).
	deepThrottleWarnPct = 10.0
)

// ── Entrada ──────────────────────────────────────────────────────────────────────────────────────

// DeepNode é um node do pool como coletado do cluster.
type DeepNode struct {
	Name           string
	Zone           string
	CPUCapMillis   float64
	MemCapMi       float64
	CPUAllocMillis float64
	MemAllocMi     float64
	PodsAlloc      int
	// Uso ao vivo (metrics-server); HasUsage=false quando indisponível.
	CPUUsageMillis float64
	MemUsageMi     float64
	HasUsage       bool
	CreatedAt      time.Time
}

// DeepPod é um pod Running em um node do pool.
type DeepPod struct {
	Namespace string
	Name      string
	Node      string
	// OwnerKind: Deployment, StatefulSet, DaemonSet, Job, ReplicaSet ou Pod (avulso).
	OwnerKind string
	// Workload: nome resolvido com ResolveWorkload (mesma chave do FinOpsReport).
	Workload     string
	CPUReqMillis float64
	MemReqMi     float64
	CPULimMillis float64
	MemLimMi     float64
	// MissingMemLimit: algum container sem limit de memória.
	MissingMemLimit bool
	// Uso ao vivo (metrics-server).
	CPUUsageMillis float64
	MemUsageMi     float64
	HasUsage       bool
}

// DeepHistory é o histórico POR POD de um workload, vindo do último FinOpsReport em cache.
type DeepHistory struct {
	CPUAvgMillis  float64
	CPUP95Millis  float64
	CPUPeakMillis float64
	MemAvgMi      float64
	MemP95Mi      float64
	MemPeakMi     float64
	Source        string // dynatrace | prometheus
	// Histórico do HPA no período do relatório.
	HPAAvgReplicas float64
	HPAMaxObserved int
	HPAMinObserved int
	HPAScaleEvents int
	HPANeverScaled bool
}

// DeepThrottle é o throttling de CPU de um pod: % dos períodos do CFS em que o container foi
// congelado por atingir o CPU limit.
type DeepThrottle struct {
	P95Pct     float64
	CurrentPct float64
}

// DeepPrice é o preço de uma SKU (USD/hora).
type DeepPrice struct {
	USDHour float64
	Source  string
}

// DeepAnalysisInput reúne tudo o que BuildPoolDeepAnalysis precisa.
type DeepAnalysisInput struct {
	Cluster  string
	Pool     string
	VMSize   string
	Region   string
	Priority string // regular | spot
	OSDisk   string // managed | ephemeral | ""
	// CurrentSpec: spec da SKU atual (DeepSpecFor, ou specs do pricer quando fora da tabela).
	CurrentSpec DeepSKUSpec
	// Candidates: SKUs alternativas para a simulação (sem a atual).
	Candidates   []DeepSKUSpec
	Prices       map[string]DeepPrice // chave: VMSize em minúsculas
	ExchangeRate float64
	Nodes        []DeepNode
	Pods         []DeepPod
	MetricsLive  bool
	// History: chave "namespace/workload".
	History            map[string]DeepHistory
	HistoryGeneratedAt *time.Time
	HistoryWindowDays  int
	// HPAs: chave "namespace/alvo" (nome do Deployment/StatefulSet).
	HPAs map[string]DeepHPA
	// Throttling: chave "namespace/pod". ThrottleWindowDays = janela do P95 (0 = não coletado).
	Throttling         map[string]DeepThrottle
	ThrottleWindowDays int
	// SKUCaps: catálogo de capacidades da região (chave: SKU em minúsculas); vazio = não carregado.
	SKUCaps map[string]SKUCapabilities
	// SKUCatalogStatus: fresh | stale | loading | unavailable | "" (não se aplica, ex.: fora da AKS).
	SKUCatalogStatus    string
	SKUCatalogFetchedAt *time.Time
	Headroom            float64
	Warnings            []string
	Now                 time.Time
}

// ── Saída ────────────────────────────────────────────────────────────────────────────────────────

// PoolDeepAnalysis é o resultado da Deep Analysis de um node pool.
type PoolDeepAnalysis struct {
	Cluster     string                `json:"cluster"`
	Pool        string                `json:"pool"`
	GeneratedAt time.Time             `json:"generated_at"`
	Overview    DeepPoolOverview      `json:"overview"`
	Allocation  DeepAllocation        `json:"allocation"`
	Nodes       []DeepNodeRow         `json:"nodes"`
	DaemonSets  DeepDaemonSetOverhead `json:"daemonsets"`
	Workloads   []DeepWorkloadRow     `json:"workloads"`
	Simulation  []DeepSimulation      `json:"simulation"`
	Headroom    float64               `json:"headroom"`
	Findings    []DeepFinding         `json:"findings"`
	History     DeepHistoryMeta       `json:"history"`
	MetricsLive bool                  `json:"metrics_live"`
	// ThrottleWindowDays: janela do P95 de throttling (0 = não coletado).
	ThrottleWindowDays int `json:"throttle_window_days"`
	// Catálogo de capacidades de SKU usado na simulação.
	SKUCatalogStatus    string     `json:"sku_catalog_status,omitempty"`
	SKUCatalogFetchedAt *time.Time `json:"sku_catalog_fetched_at,omitempty"`
	Warnings            []string   `json:"warnings"`
}

// DeepPoolOverview resume o pool.
type DeepPoolOverview struct {
	VMSize         string   `json:"vm_size"`
	VCPU           int      `json:"vcpu"`
	MemGB          int      `json:"mem_gb"`
	CPU            string   `json:"cpu,omitempty"`
	SMT            bool     `json:"smt"`
	Nodes          int      `json:"nodes"`
	Zones          []string `json:"zones"`
	Region         string   `json:"region,omitempty"`
	Priority       string   `json:"priority"`
	OSDisk         string   `json:"os_disk,omitempty"`
	MaxPods        int      `json:"max_pods"`
	PodsRunning    int      `json:"pods_running"`
	PriceUSDHour   float64  `json:"price_usd_hour"`
	PriceSource    string   `json:"price_source,omitempty"`
	MonthlyCostBRL float64  `json:"monthly_cost_brl"`
	ExchangeRate   float64  `json:"exchange_rate"`
}

// DeepResource é a visão de alocação de um recurso no pool (CPU em millicores, memória em Mi).
type DeepResource struct {
	Allocatable float64 `json:"allocatable"`
	Requests    float64 `json:"requests"`
	Limits      float64 `json:"limits"`
	// UsageLive: soma do uso ao vivo dos nodes (metrics-server).
	UsageLive float64 `json:"usage_live"`
	HasLive   bool    `json:"has_live"`
	// UsageP95: Σ P95 por pod × pods dos workloads com histórico + uso ao vivo dos DaemonSets (soma de
	// P95s — conservador, os picos não são simultâneos).
	UsageP95     float64 `json:"usage_p95"`
	HasP95       bool    `json:"has_p95"`
	RequestPct   float64 `json:"request_pct"`
	LimitPct     float64 `json:"limit_pct"`
	UsageLivePct float64 `json:"usage_live_pct"`
	UsageP95Pct  float64 `json:"usage_p95_pct"`
}

// usagePct é o uso usado nas decisões: ao vivo quando disponível, senão o P95 histórico.
func (r DeepResource) usagePct() (float64, bool) {
	if r.HasLive {
		return r.UsageLivePct, true
	}
	if r.HasP95 {
		return r.UsageP95Pct, true
	}
	return 0, false
}

// DeepAllocation é o diagnóstico de alocação do pool.
type DeepAllocation struct {
	CPU DeepResource `json:"cpu"`
	Mem DeepResource `json:"mem"`
	// SchedulingBound: recurso com maior % reservado — o que "enche" os nodes para o scheduler.
	SchedulingBound string `json:"scheduling_bound"` // cpu | memory
	// RealBottleneck: recurso com maior % de uso real ("" sem dado de uso).
	RealBottleneck  string `json:"real_bottleneck"`
	Mismatch        bool   `json:"mismatch"`
	Explanation     string `json:"explanation"`
	NodesMemAbove90 int    `json:"nodes_mem_above_90"`
	NodesMemAbove95 int    `json:"nodes_mem_above_95"`
	NodesCPUAbove90 int    `json:"nodes_cpu_above_90"`
}

// DeepNodeRow é a visão de um node.
type DeepNodeRow struct {
	Name           string     `json:"name"`
	Zone           string     `json:"zone,omitempty"`
	CPUAllocMillis float64    `json:"cpu_alloc_millis"`
	MemAllocMi     float64    `json:"mem_alloc_mi"`
	Pods           int        `json:"pods"`
	CPUReqPct      float64    `json:"cpu_req_pct"`
	MemReqPct      float64    `json:"mem_req_pct"`
	CPULimPct      float64    `json:"cpu_lim_pct"`
	MemLimPct      float64    `json:"mem_lim_pct"`
	CPUUsagePct    float64    `json:"cpu_usage_pct"`
	MemUsagePct    float64    `json:"mem_usage_pct"`
	HasUsage       bool       `json:"has_usage"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}

// DeepDaemonSetRow é um DaemonSet presente no pool (valores por pod = por node).
type DeepDaemonSetRow struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	Pods          int      `json:"pods"`
	CPUReqMillis  float64  `json:"cpu_req_millis"`
	MemReqMi      float64  `json:"mem_req_mi"`
	MemLimMi      float64  `json:"mem_lim_mi"`
	CPUUsageAvg   float64  `json:"cpu_usage_avg_millis"`
	CPUUsageMax   float64  `json:"cpu_usage_max_millis"`
	MemUsageAvgMi float64  `json:"mem_usage_avg_mi"`
	MemUsageMaxMi float64  `json:"mem_usage_max_mi"`
	HasUsage      bool     `json:"has_usage"`
	Flags         []string `json:"flags"`
}

// DeepDaemonSetOverhead é o custo fixo dos DaemonSets em cada node do pool.
type DeepDaemonSetOverhead struct {
	PerNodeCPUReqMillis   float64            `json:"per_node_cpu_req_millis"`
	PerNodeMemReqMi       float64            `json:"per_node_mem_req_mi"`
	PerNodeCPUUsageMillis float64            `json:"per_node_cpu_usage_millis"`
	PerNodeMemUsageMi     float64            `json:"per_node_mem_usage_mi"`
	PerNodePods           float64            `json:"per_node_pods"`
	HasUsage              bool               `json:"has_usage"`
	CPUOverheadPct        float64            `json:"cpu_overhead_pct"`
	MemOverheadPct        float64            `json:"mem_overhead_pct"`
	MonthlyCostBRL        float64            `json:"monthly_cost_brl"`
	Items                 []DeepDaemonSetRow `json:"items"`
}

// DeepWorkloadRow é um workload (não DaemonSet) com pods no pool. Valores por pod.
type DeepWorkloadRow struct {
	Namespace    string  `json:"namespace"`
	Workload     string  `json:"workload"`
	Kind         string  `json:"kind"`
	PodsOnPool   int     `json:"pods_on_pool"`
	CPUReqMillis float64 `json:"cpu_req_millis"`
	MemReqMi     float64 `json:"mem_req_mi"`
	CPULimMillis float64 `json:"cpu_lim_millis"`
	MemLimMi     float64 `json:"mem_lim_mi"`
	// Uso ao vivo por pod (média e máximo entre os pods do pool).
	CPUUsageAvgMillis float64 `json:"cpu_usage_avg_millis"`
	CPUUsageMaxMillis float64 `json:"cpu_usage_max_millis"`
	MemUsageAvgMi     float64 `json:"mem_usage_avg_mi"`
	MemUsageMaxMi     float64 `json:"mem_usage_max_mi"`
	HasLiveUsage      bool    `json:"has_live_usage"`
	// Histórico por pod (último relatório).
	CPUP95Millis  float64 `json:"cpu_p95_millis,omitempty"`
	CPUPeakMillis float64 `json:"cpu_peak_millis,omitempty"`
	MemP95Mi      float64 `json:"mem_p95_mi,omitempty"`
	MemPeakMi     float64 `json:"mem_peak_mi,omitempty"`
	HasHistory    bool    `json:"has_history"`
	// UsageBasis: "p95" (histórico) | "live" (instantâneo) | "" (sem uso).
	UsageBasis           string  `json:"usage_basis"`
	CPUUsageVsRequestPct float64 `json:"cpu_usage_vs_request_pct"`
	MemUsageVsRequestPct float64 `json:"mem_usage_vs_request_pct"`
	// CPURequestSharePct: participação deste workload na CPU reservada do pool.
	CPURequestSharePct float64 `json:"cpu_request_share_pct"`
	CPURecMillis       float64 `json:"cpu_rec_millis"`
	MemRecMi           float64 `json:"mem_rec_mi"`
	MemLimitRecMi      float64 `json:"mem_limit_rec_mi"`
	// Throttling de CPU (Prometheus): P95 e atual do pior pod; HasThrottle=false sem coleta.
	ThrottleP95Pct     float64 `json:"throttle_p95_pct,omitempty"`
	ThrottleCurrentPct float64 `json:"throttle_current_pct,omitempty"`
	HasThrottle        bool    `json:"has_throttle"`
	// CPULimitRecMillis: só quando há throttling relevante (limit sugerido); 0 = manter.
	CPULimitRecMillis float64      `json:"cpu_limit_rec_millis,omitempty"`
	HPA               *DeepHPAView `json:"hpa,omitempty"`
	Flags             []string     `json:"flags"`
}

// DeepSimulation é o resultado da simulação de uma SKU para o pool.
type DeepSimulation struct {
	VMSize         string  `json:"vm_size"`
	VCPU           int     `json:"vcpu"`
	MemGB          int     `json:"mem_gb"`
	CPU            string  `json:"cpu,omitempty"`
	SMT            bool    `json:"smt"`
	Series         string  `json:"series,omitempty"`
	IsCurrent      bool    `json:"is_current"`
	AllocCPUMillis float64 `json:"alloc_cpu_millis"`
	AllocMemMi     float64 `json:"alloc_mem_mi"`
	// AllocEstimated: alocável pela fórmula da AKS (false = medido nos nodes do pool).
	AllocEstimated bool    `json:"alloc_estimated"`
	PriceUSDHour   float64 `json:"price_usd_hour"`
	PriceSource    string  `json:"price_source,omitempty"`
	// Cenário 1 — requests atuais (só troca a VM).
	NodesCurrentReq    int     `json:"nodes_current_req"`
	CostCurrentReqBRL  float64 `json:"cost_current_req_brl"`
	LimitingCurrentReq string  `json:"limiting_current_req"`
	// Cenário 2 — requests recomendados (depois do right-sizing).
	NodesRecommended      int     `json:"nodes_recommended"`
	CostRecommendedBRL    float64 `json:"cost_recommended_brl"`
	LimitingRecommended   string  `json:"limiting_recommended"`
	SavingsRecommendedBRL float64 `json:"savings_recommended_brl"`
	NodeLossImpactPct     float64 `json:"node_loss_impact_pct"`
	Feasible              bool    `json:"feasible"`
	// Capacidades do catálogo (F3). CatalogKnown=false: catálogo não carregado (Available assume true).
	CatalogKnown    bool     `json:"catalog_known"`
	Available       bool     `json:"available"`
	EphemeralOSDisk *bool    `json:"ephemeral_os_disk,omitempty"`
	Zones           []string `json:"zones,omitempty"`
	VCPUsPerCore    int      `json:"vcpus_per_core,omitempty"`
	Notes           []string `json:"notes"`
}

// DeepFinding é um achado do diagnóstico.
type DeepFinding struct {
	Severity string `json:"severity"` // critical | warning | info
	Code     string `json:"code"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// DeepHistoryMeta descreve o histórico usado.
type DeepHistoryMeta struct {
	Available   bool       `json:"available"`
	GeneratedAt *time.Time `json:"generated_at,omitempty"`
	WindowDays  int        `json:"window_days,omitempty"`
	// Coverage: workloads do pool com histórico / total de workloads do pool.
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

// ── Construção ───────────────────────────────────────────────────────────────────────────────────

// BuildPoolDeepAnalysis monta o diagnóstico completo do pool a partir do input já coletado.
func BuildPoolDeepAnalysis(in DeepAnalysisInput) PoolDeepAnalysis {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	headroom := in.Headroom
	if headroom <= 0 || headroom > 1 {
		headroom = DeepDefaultHeadroom
	}

	out := PoolDeepAnalysis{
		Cluster:     in.Cluster,
		Pool:        in.Pool,
		GeneratedAt: now,
		Headroom:    headroom,
		MetricsLive: in.MetricsLive,

		ThrottleWindowDays:  in.ThrottleWindowDays,
		SKUCatalogStatus:    in.SKUCatalogStatus,
		SKUCatalogFetchedAt: in.SKUCatalogFetchedAt,
		Warnings:            append([]string{}, in.Warnings...),
		Findings:            []DeepFinding{},
		Nodes:               []DeepNodeRow{},
		Workloads:           []DeepWorkloadRow{},
		Simulation:          []DeepSimulation{},
	}

	out.Overview = buildDeepOverview(in)
	out.Nodes = buildDeepNodes(in)
	out.Allocation = buildDeepAllocation(in, out.Nodes)
	out.DaemonSets = buildDeepDaemonSets(in, out.Overview.MonthlyCostBRL)
	out.Workloads, out.History = buildDeepWorkloads(in, out.Allocation.CPU.Requests)
	out.Simulation = buildDeepSimulation(in, out.Overview, out.DaemonSets, out.Workloads, headroom)
	out.Findings = buildDeepFindings(out)

	if !in.MetricsLive {
		out.Warnings = append(out.Warnings, "metrics-server indisponível: uso ao vivo de nodes e pods não foi coletado.")
	}
	if !out.History.Available {
		out.Warnings = append(out.Warnings, "Sem relatório FinOps em cache para o cluster: a análise usa só o uso instantâneo. Rode \"Analisar\" no FinOps para ter P95 e picos históricos.")
	}
	return out
}

func buildDeepOverview(in DeepAnalysisInput) DeepPoolOverview {
	zones := map[string]bool{}
	maxPods := 0
	for _, n := range in.Nodes {
		if n.Zone != "" {
			zones[n.Zone] = true
		}
		if n.PodsAlloc > maxPods {
			maxPods = n.PodsAlloc
		}
	}
	zl := make([]string, 0, len(zones))
	for z := range zones {
		zl = append(zl, z)
	}
	sort.Strings(zl)

	price := in.Prices[strings.ToLower(in.VMSize)]
	priority := in.Priority
	if priority == "" {
		priority = "regular"
	}
	return DeepPoolOverview{
		VMSize:         in.VMSize,
		VCPU:           in.CurrentSpec.VCPU,
		MemGB:          in.CurrentSpec.MemGB,
		CPU:            in.CurrentSpec.CPU,
		SMT:            in.CurrentSpec.SMT,
		Nodes:          len(in.Nodes),
		Zones:          zl,
		Region:         in.Region,
		Priority:       priority,
		OSDisk:         in.OSDisk,
		MaxPods:        maxPods,
		PodsRunning:    len(in.Pods),
		PriceUSDHour:   price.USDHour,
		PriceSource:    price.Source,
		MonthlyCostBRL: round2(price.USDHour * HoursPerMonth * float64(len(in.Nodes)) * in.ExchangeRate),
		ExchangeRate:   in.ExchangeRate,
	}
}

func deepPct(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return round2(part / whole * 100)
}

func buildDeepNodes(in DeepAnalysisInput) []DeepNodeRow {
	type agg struct {
		cpuReq, memReq, cpuLim, memLim float64
		pods                           int
	}
	byNode := map[string]*agg{}
	for _, p := range in.Pods {
		a, ok := byNode[p.Node]
		if !ok {
			a = &agg{}
			byNode[p.Node] = a
		}
		a.cpuReq += p.CPUReqMillis
		a.memReq += p.MemReqMi
		a.cpuLim += p.CPULimMillis
		a.memLim += p.MemLimMi
		a.pods++
	}
	rows := make([]DeepNodeRow, 0, len(in.Nodes))
	for _, n := range in.Nodes {
		a := byNode[n.Name]
		if a == nil {
			a = &agg{}
		}
		row := DeepNodeRow{
			Name:           n.Name,
			Zone:           n.Zone,
			CPUAllocMillis: n.CPUAllocMillis,
			MemAllocMi:     round2(n.MemAllocMi),
			Pods:           a.pods,
			CPUReqPct:      deepPct(a.cpuReq, n.CPUAllocMillis),
			MemReqPct:      deepPct(a.memReq, n.MemAllocMi),
			CPULimPct:      deepPct(a.cpuLim, n.CPUAllocMillis),
			MemLimPct:      deepPct(a.memLim, n.MemAllocMi),
			HasUsage:       n.HasUsage,
		}
		if n.HasUsage {
			row.CPUUsagePct = deepPct(n.CPUUsageMillis, n.CPUAllocMillis)
			row.MemUsagePct = deepPct(n.MemUsageMi, n.MemAllocMi)
		}
		if !n.CreatedAt.IsZero() {
			t := n.CreatedAt
			row.CreatedAt = &t
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].MemUsagePct != rows[j].MemUsagePct {
			return rows[i].MemUsagePct > rows[j].MemUsagePct
		}
		return rows[i].MemReqPct > rows[j].MemReqPct
	})
	return rows
}

func buildDeepAllocation(in DeepAnalysisInput, nodes []DeepNodeRow) DeepAllocation {
	var a DeepAllocation
	liveNodes := 0
	for _, n := range in.Nodes {
		a.CPU.Allocatable += n.CPUAllocMillis
		a.Mem.Allocatable += n.MemAllocMi
		if n.HasUsage {
			a.CPU.UsageLive += n.CPUUsageMillis
			a.Mem.UsageLive += n.MemUsageMi
			liveNodes++
		}
	}
	// Só considera o uso ao vivo quando TODOS os nodes têm medição (senão a soma fica subestimada).
	a.CPU.HasLive = liveNodes > 0 && liveNodes == len(in.Nodes)
	a.Mem.HasLive = a.CPU.HasLive

	for _, p := range in.Pods {
		a.CPU.Requests += p.CPUReqMillis
		a.Mem.Requests += p.MemReqMi
		a.CPU.Limits += p.CPULimMillis
		a.Mem.Limits += p.MemLimMi
		// DaemonSet: o histórico do relatório é o MAIOR P95 entre os pods do cluster inteiro (o pior
		// node de todos os pools) — multiplicado pelos pods do pool, infla a soma (calculofrete:
		// fluentd 500m de P95 "por pod" contra 133m de média no pool). Para eles, o uso ao vivo.
		if p.OwnerKind == "DaemonSet" {
			if p.HasUsage {
				a.CPU.UsageP95 += p.CPUUsageMillis
				a.Mem.UsageP95 += p.MemUsageMi
			}
			continue
		}
		if h, ok := in.History[p.Namespace+"/"+p.Workload]; ok {
			if h.CPUP95Millis > 0 {
				a.CPU.UsageP95 += h.CPUP95Millis
				a.CPU.HasP95 = true
			}
			if h.MemP95Mi > 0 {
				a.Mem.UsageP95 += h.MemP95Mi
				a.Mem.HasP95 = true
			}
		}
	}

	for _, r := range []*DeepResource{&a.CPU, &a.Mem} {
		r.RequestPct = deepPct(r.Requests, r.Allocatable)
		r.LimitPct = deepPct(r.Limits, r.Allocatable)
		if r.HasLive {
			r.UsageLivePct = deepPct(r.UsageLive, r.Allocatable)
		}
		if r.HasP95 {
			r.UsageP95Pct = deepPct(r.UsageP95, r.Allocatable)
		}
		r.Allocatable = round2(r.Allocatable)
		r.Requests = round2(r.Requests)
		r.Limits = round2(r.Limits)
		r.UsageLive = round2(r.UsageLive)
		r.UsageP95 = round2(r.UsageP95)
	}

	a.SchedulingBound = "cpu"
	if a.Mem.RequestPct > a.CPU.RequestPct {
		a.SchedulingBound = "memory"
	}
	cpuUse, okCPU := a.CPU.usagePct()
	memUse, okMem := a.Mem.usagePct()
	if okCPU || okMem {
		a.RealBottleneck = "cpu"
		if memUse > cpuUse {
			a.RealBottleneck = "memory"
		}
	}

	for _, n := range nodes {
		if !n.HasUsage {
			continue
		}
		if n.MemUsagePct >= 90 {
			a.NodesMemAbove90++
		}
		if n.MemUsagePct >= 95 {
			a.NodesMemAbove95++
		}
		if n.CPUUsagePct >= 90 {
			a.NodesCPUAbove90++
		}
	}

	inflatedCPU := okCPU && a.CPU.RequestPct >= 50 && a.CPU.RequestPct >= 2*cpuUse
	a.Mismatch = (a.RealBottleneck != "" && a.RealBottleneck != a.SchedulingBound) || inflatedCPU
	a.Explanation = deepAllocationExplanation(a, cpuUse, memUse, inflatedCPU)
	return a
}

func resourceLabel(r string) string {
	if r == "memory" {
		return "memória"
	}
	return "CPU"
}

func deepAllocationExplanation(a DeepAllocation, cpuUse, memUse float64, inflatedCPU bool) string {
	if a.RealBottleneck == "" {
		return fmt.Sprintf("O pool é preenchido pelo request de %s (CPU %.0f%% e memória %.0f%% reservados). Sem dado de uso real para comparar.",
			resourceLabel(a.SchedulingBound), a.CPU.RequestPct, a.Mem.RequestPct)
	}
	base := fmt.Sprintf("Reservado: CPU %.0f%%, memória %.0f%%. Em uso: CPU %.0f%%, memória %.0f%%.",
		a.CPU.RequestPct, a.Mem.RequestPct, cpuUse, memUse)
	switch {
	case a.SchedulingBound != a.RealBottleneck:
		return fmt.Sprintf("O pool é dimensionado pelo request de %s, mas o gargalo real é %s. %s Ajustar os requests ao uso real faz %s definir o tamanho do pool.",
			resourceLabel(a.SchedulingBound), resourceLabel(a.RealBottleneck), base, resourceLabel(a.RealBottleneck))
	case inflatedCPU:
		return fmt.Sprintf("O request de CPU está bem acima do uso real e é o que enche os nodes. %s", base)
	default:
		return fmt.Sprintf("Request e uso apontam para o mesmo recurso (%s). %s", resourceLabel(a.RealBottleneck), base)
	}
}

func buildDeepDaemonSets(in DeepAnalysisInput, poolCostBRL float64) DeepDaemonSetOverhead {
	type acc struct {
		row             DeepDaemonSetRow
		cpuUse, memUse  []float64
		missingMemLimit bool
	}
	byDS := map[string]*acc{}
	var order []string
	for _, p := range in.Pods {
		if p.OwnerKind != "DaemonSet" {
			continue
		}
		key := p.Namespace + "/" + p.Workload
		a, ok := byDS[key]
		if !ok {
			a = &acc{row: DeepDaemonSetRow{Namespace: p.Namespace, Name: p.Workload, Flags: []string{}}}
			byDS[key] = a
			order = append(order, key)
		}
		a.row.Pods++
		a.row.CPUReqMillis += p.CPUReqMillis
		a.row.MemReqMi += p.MemReqMi
		a.row.MemLimMi += p.MemLimMi
		if p.MissingMemLimit {
			a.missingMemLimit = true
		}
		if p.HasUsage {
			a.cpuUse = append(a.cpuUse, p.CPUUsageMillis)
			a.memUse = append(a.memUse, p.MemUsageMi)
		}
	}

	var o DeepDaemonSetOverhead
	o.Items = []DeepDaemonSetRow{}
	nodes := float64(len(in.Nodes))
	for _, key := range order {
		a := byDS[key]
		n := float64(a.row.Pods)
		a.row.CPUReqMillis = round2(a.row.CPUReqMillis / n)
		a.row.MemReqMi = round2(a.row.MemReqMi / n)
		a.row.MemLimMi = round2(a.row.MemLimMi / n)
		if len(a.cpuUse) > 0 {
			a.row.HasUsage = true
			a.row.CPUUsageAvg, a.row.CPUUsageMax = avgMax(a.cpuUse)
			a.row.MemUsageAvgMi, a.row.MemUsageMaxMi = avgMax(a.memUse)
			o.HasUsage = true
		}
		if a.row.MemReqMi == 0 {
			a.row.Flags = append(a.row.Flags, "no_mem_request")
		}
		if a.missingMemLimit {
			a.row.Flags = append(a.row.Flags, "no_mem_limit")
		}
		if a.row.HasUsage && a.row.MemReqMi > 0 && a.row.MemUsageAvgMi > a.row.MemReqMi {
			a.row.Flags = append(a.row.Flags, "mem_under_requested")
		}
		if a.row.HasUsage && a.row.CPUReqMillis > 0 && a.row.CPUUsageAvg > a.row.CPUReqMillis {
			a.row.Flags = append(a.row.Flags, "cpu_under_requested")
		}
		// Por node: um pod do DaemonSet por node (pods/nodes ponderado para DaemonSets com
		// nodeSelector que não cobrem todos os nodes).
		share := 1.0
		if nodes > 0 {
			share = math.Min(1, n/nodes)
		}
		o.PerNodeCPUReqMillis += a.row.CPUReqMillis * share
		o.PerNodeMemReqMi += a.row.MemReqMi * share
		o.PerNodeCPUUsageMillis += a.row.CPUUsageAvg * share
		o.PerNodeMemUsageMi += a.row.MemUsageAvgMi * share
		o.PerNodePods += share
		o.Items = append(o.Items, a.row)
	}
	sort.SliceStable(o.Items, func(i, j int) bool {
		return math.Max(o.Items[i].MemUsageAvgMi, o.Items[i].MemReqMi) > math.Max(o.Items[j].MemUsageAvgMi, o.Items[j].MemReqMi)
	})

	var allocCPU, allocMem float64
	for _, n := range in.Nodes {
		allocCPU += n.CPUAllocMillis
		allocMem += n.MemAllocMi
	}
	if nodes > 0 {
		allocCPU /= nodes
		allocMem /= nodes
	}
	cpuBase := math.Max(o.PerNodeCPUUsageMillis, o.PerNodeCPUReqMillis)
	memBase := math.Max(o.PerNodeMemUsageMi, o.PerNodeMemReqMi)
	o.CPUOverheadPct = deepPct(cpuBase, allocCPU)
	o.MemOverheadPct = deepPct(memBase, allocMem)
	// Custo: a fatia de memória (o recurso mais escasso nos pools típicos) do custo do pool.
	o.MonthlyCostBRL = round2(poolCostBRL * math.Max(o.CPUOverheadPct, o.MemOverheadPct) / 100)
	o.PerNodeCPUReqMillis = round2(o.PerNodeCPUReqMillis)
	o.PerNodeMemReqMi = round2(o.PerNodeMemReqMi)
	o.PerNodeCPUUsageMillis = round2(o.PerNodeCPUUsageMillis)
	o.PerNodeMemUsageMi = round2(o.PerNodeMemUsageMi)
	o.PerNodePods = round2(o.PerNodePods)
	return o
}

func avgMax(vs []float64) (avg, max float64) {
	for _, v := range vs {
		avg += v
		if v > max {
			max = v
		}
	}
	return round2(avg / float64(len(vs))), round2(max)
}

// ceilTo arredonda v para cima no múltiplo de step.
func ceilTo(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return math.Ceil(v/step) * step
}

func buildDeepWorkloads(in DeepAnalysisInput, poolCPUReq float64) ([]DeepWorkloadRow, DeepHistoryMeta) {
	type acc struct {
		row             DeepWorkloadRow
		cpuUse, memUse  []float64
		missingMemLimit bool
		pods            []string
	}
	byWL := map[string]*acc{}
	var order []string
	for _, p := range in.Pods {
		if p.OwnerKind == "DaemonSet" {
			continue
		}
		key := p.Namespace + "/" + p.Workload
		a, ok := byWL[key]
		if !ok {
			a = &acc{row: DeepWorkloadRow{Namespace: p.Namespace, Workload: p.Workload, Kind: p.OwnerKind, Flags: []string{}}}
			byWL[key] = a
			order = append(order, key)
		}
		a.row.PodsOnPool++
		a.pods = append(a.pods, p.Namespace+"/"+p.Name)
		a.row.CPUReqMillis += p.CPUReqMillis
		a.row.MemReqMi += p.MemReqMi
		a.row.CPULimMillis += p.CPULimMillis
		a.row.MemLimMi += p.MemLimMi
		if p.MissingMemLimit {
			a.missingMemLimit = true
		}
		if p.HasUsage {
			a.cpuUse = append(a.cpuUse, p.CPUUsageMillis)
			a.memUse = append(a.memUse, p.MemUsageMi)
		}
	}

	meta := DeepHistoryMeta{
		Available:   len(in.History) > 0,
		GeneratedAt: in.HistoryGeneratedAt,
		WindowDays:  in.HistoryWindowDays,
		Total:       len(order),
	}

	rows := make([]DeepWorkloadRow, 0, len(order))
	for _, key := range order {
		a := byWL[key]
		r := a.row
		n := float64(r.PodsOnPool)
		totalCPUReq := r.CPUReqMillis
		r.CPUReqMillis = round2(r.CPUReqMillis / n)
		r.MemReqMi = round2(r.MemReqMi / n)
		r.CPULimMillis = round2(r.CPULimMillis / n)
		r.MemLimMi = round2(r.MemLimMi / n)
		r.CPURequestSharePct = deepPct(totalCPUReq, poolCPUReq)
		if len(a.cpuUse) > 0 {
			r.HasLiveUsage = true
			r.CPUUsageAvgMillis, r.CPUUsageMaxMillis = avgMax(a.cpuUse)
			r.MemUsageAvgMi, r.MemUsageMaxMi = avgMax(a.memUse)
		}
		if h, ok := in.History[key]; ok && (h.CPUP95Millis > 0 || h.MemP95Mi > 0) {
			r.HasHistory = true
			r.CPUP95Millis = round2(h.CPUP95Millis)
			r.CPUPeakMillis = round2(h.CPUPeakMillis)
			r.MemP95Mi = round2(h.MemP95Mi)
			r.MemPeakMi = round2(h.MemPeakMi)
			meta.Covered++
		}

		// Base de uso: P95 histórico; sem histórico, o uso ao vivo (máximo entre os pods para CPU
		// e memória — o instantâneo não captura pico, então o máximo é o mais seguro).
		var cpuBase, memBase float64
		switch {
		case r.HasHistory:
			r.UsageBasis = "p95"
			cpuBase, memBase = r.CPUP95Millis, math.Max(r.MemP95Mi, r.MemUsageMaxMi)
		case r.HasLiveUsage:
			r.UsageBasis = "live"
			cpuBase, memBase = r.CPUUsageMaxMillis, r.MemUsageMaxMi
		}
		if r.UsageBasis != "" {
			r.CPUUsageVsRequestPct = deepPct(cpuBase, r.CPUReqMillis)
			r.MemUsageVsRequestPct = deepPct(memBase, r.MemReqMi)
			r.CPURecMillis = math.Max(deepMinCPURecMillis, ceilTo(cpuBase*SafetyMargin, 10))
			r.MemRecMi = ceilTo(memBase*SafetyMargin, 16)
			peak := math.Max(memBase, r.MemPeakMi)
			r.MemLimitRecMi = ceilTo(peak*deepMemLimitFactor, 16)
			if r.MemLimitRecMi < r.MemRecMi {
				r.MemLimitRecMi = r.MemRecMi
			}
		}

		// HPA: projeta a utilização com o request recomendado e ajusta o recomendado ao alvo.
		if h, ok := in.HPAs[key]; ok {
			hist := in.History[key]
			avgCPU, avgMem := hist.CPUAvgMillis, hist.MemAvgMi
			if avgCPU <= 0 {
				avgCPU = r.CPUUsageAvgMillis
			}
			if avgMem <= 0 {
				avgMem = r.MemUsageAvgMi
			}
			applyHPA(&r, h, hist, avgCPU, avgMem)
		}

		// Throttling: o pior pod do workload no pool.
		for _, pk := range a.pods {
			th, ok := in.Throttling[pk]
			if !ok {
				continue
			}
			r.HasThrottle = true
			r.ThrottleP95Pct = math.Max(r.ThrottleP95Pct, round2(th.P95Pct))
			r.ThrottleCurrentPct = math.Max(r.ThrottleCurrentPct, round2(th.CurrentPct))
		}
		if r.HasThrottle && r.ThrottleP95Pct >= deepThrottleWarnPct && r.CPULimMillis > 0 {
			r.Flags = append(r.Flags, "cpu_throttled")
			peak := math.Max(r.CPUPeakMillis, r.CPUUsageMaxMillis)
			r.CPULimitRecMillis = ceilTo(math.Max(r.CPULimMillis*2, peak*2), 100)
		}

		if r.CPUReqMillis == 0 && r.MemReqMi == 0 {
			r.Flags = append(r.Flags, "no_requests")
		}
		if a.missingMemLimit {
			r.Flags = append(r.Flags, "no_mem_limit")
		}
		if r.UsageBasis != "" && r.MemReqMi > 0 && memBase > r.MemReqMi {
			r.Flags = append(r.Flags, "mem_under_requested")
		}
		if r.UsageBasis != "" && r.CPUReqMillis > 0 && cpuBase < r.CPUReqMillis*0.25 {
			r.Flags = append(r.Flags, "cpu_over_requested")
		}
		if r.MemReqMi > 0 && r.MemLimMi > 2*r.MemReqMi {
			r.Flags = append(r.Flags, "high_mem_limit_ratio")
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CPURequestSharePct > rows[j].CPURequestSharePct })
	return rows, meta
}

// deepDemand é a demanda de recursos dos workloads (não DaemonSet) do pool num cenário.
type deepDemand struct {
	cpuMillis, memMi float64
	pods             int
}

func deepDemands(in DeepAnalysisInput, workloads []DeepWorkloadRow) (current, recommended deepDemand) {
	for _, w := range workloads {
		n := float64(w.PodsOnPool)
		var cpuUse, memUse float64
		switch w.UsageBasis {
		case "p95":
			cpuUse, memUse = w.CPUP95Millis, math.Max(w.MemP95Mi, w.MemUsageMaxMi)
		case "live":
			cpuUse, memUse = w.CPUUsageMaxMillis, w.MemUsageMaxMi
		}
		current.cpuMillis += math.Max(w.CPUReqMillis, cpuUse) * n
		current.memMi += math.Max(w.MemReqMi, memUse) * n
		current.pods += w.PodsOnPool

		if w.UsageBasis == "" {
			// Sem uso conhecido, o recomendado é o próprio request atual (nada a ajustar).
			recommended.cpuMillis += w.CPUReqMillis * n
			recommended.memMi += w.MemReqMi * n
		} else {
			recommended.cpuMillis += w.CPURecMillis * n
			recommended.memMi += w.MemRecMi * n
		}
		recommended.pods += w.PodsOnPool
	}
	return current, recommended
}

// nodesFor calcula os nodes necessários para uma demanda numa SKU com o alocável informado, já
// descontado o custo fixo de DaemonSets. Devolve 0 quando a SKU não comporta nem os DaemonSets.
func nodesFor(d deepDemand, allocCPU, allocMem float64, maxPods int, ds DeepDaemonSetOverhead, headroom float64, minNodes int) (int, string) {
	freeCPU := allocCPU - ds.PerNodeCPUReqMillis
	freeMem := allocMem - math.Max(ds.PerNodeMemReqMi, ds.PerNodeMemUsageMi)
	freePods := float64(maxPods) - ds.PerNodePods
	if freeCPU <= 0 || freeMem <= 0 || freePods <= 0 {
		return 0, ""
	}
	byCPU := d.cpuMillis / (freeCPU * headroom)
	byMem := d.memMi / (freeMem * headroom)
	byPods := float64(d.pods) / freePods

	need, limiting := byCPU, "cpu"
	if byMem > need {
		need, limiting = byMem, "memory"
	}
	if byPods > need {
		need, limiting = byPods, "pods"
	}
	n := int(math.Ceil(need - 1e-9))
	if n < minNodes {
		return minNodes, "min_nodes"
	}
	return n, limiting
}

func buildDeepSimulation(in DeepAnalysisInput, ov DeepPoolOverview, ds DeepDaemonSetOverhead, workloads []DeepWorkloadRow, headroom float64) []DeepSimulation {
	if len(in.Nodes) == 0 {
		return []DeepSimulation{}
	}
	maxPods := ov.MaxPods
	if maxPods <= 0 {
		maxPods = 110
	}
	minNodes := deepMinNodes
	if z := len(ov.Zones); z > minNodes {
		minNodes = z
	}

	var capMem, allocCPU, allocMem float64
	for _, n := range in.Nodes {
		capMem += n.MemCapMi
		allocCPU += n.CPUAllocMillis
		allocMem += n.MemAllocMi
	}
	nn := float64(len(in.Nodes))
	capMem /= nn
	allocCPU /= nn
	allocMem /= nn
	memFactor := deepMemFactor(capMem, in.CurrentSpec.MemGB)

	current, recommended := deepDemands(in, workloads)

	mk := func(spec DeepSKUSpec, isCurrent bool) DeepSimulation {
		s := DeepSimulation{
			VMSize: spec.VMSize, VCPU: spec.VCPU, MemGB: spec.MemGB, CPU: spec.CPU, SMT: spec.SMT,
			Series: spec.Series, IsCurrent: isCurrent, Notes: []string{},
		}
		if isCurrent {
			s.AllocCPUMillis, s.AllocMemMi = round2(allocCPU), round2(allocMem)
		} else {
			c, m := EstimateAllocatable(spec, maxPods, memFactor)
			s.AllocCPUMillis, s.AllocMemMi, s.AllocEstimated = round2(c), round2(m), true
		}
		applySKUCaps(&s, in, ov)
		p, hasPrice := in.Prices[strings.ToLower(spec.VMSize)]
		s.PriceUSDHour, s.PriceSource = p.USDHour, p.Source
		if !hasPrice || p.USDHour <= 0 {
			s.Notes = append(s.Notes, "Preço indisponível para esta SKU.")
		}
		monthly := func(nodes int) float64 {
			return round2(float64(nodes) * p.USDHour * HoursPerMonth * in.ExchangeRate)
		}
		s.NodesCurrentReq, s.LimitingCurrentReq = nodesFor(current, s.AllocCPUMillis, s.AllocMemMi, maxPods, ds, headroom, minNodes)
		s.NodesRecommended, s.LimitingRecommended = nodesFor(recommended, s.AllocCPUMillis, s.AllocMemMi, maxPods, ds, headroom, minNodes)
		s.Feasible = s.NodesRecommended > 0
		if !s.Feasible {
			s.Notes = append(s.Notes, "A SKU não comporta nem o custo fixo de DaemonSets por node.")
			return s
		}
		s.CostCurrentReqBRL = monthly(s.NodesCurrentReq)
		s.CostRecommendedBRL = monthly(s.NodesRecommended)
		if hasPrice && p.USDHour > 0 {
			s.SavingsRecommendedBRL = round2(ov.MonthlyCostBRL - s.CostRecommendedBRL)
		}
		s.NodeLossImpactPct = round2(100 / float64(s.NodesRecommended))
		if s.NodeLossImpactPct > deepNodeLossWarnPct {
			s.Notes = append(s.Notes, fmt.Sprintf("Perder 1 node derruba %.0f%% do pool.", s.NodeLossImpactPct))
		}
		if !s.SMT {
			s.Notes = append(s.Notes, "1 vCPU = 1 core físico (sem SMT): melhor para latência.")
		}
		return s
	}

	sims := []DeepSimulation{mk(in.CurrentSpec, true)}
	for _, c := range in.Candidates {
		sims = append(sims, mk(c, false))
	}
	sort.SliceStable(sims, func(i, j int) bool {
		a, b := sims[i], sims[j]
		if a.Feasible != b.Feasible {
			return a.Feasible
		}
		if a.Available != b.Available {
			return a.Available
		}
		ap, bp := a.PriceUSDHour > 0, b.PriceUSDHour > 0
		if ap != bp {
			return ap
		}
		return a.CostRecommendedBRL < b.CostRecommendedBRL
	})
	return sims
}

func buildDeepFindings(a PoolDeepAnalysis) []DeepFinding {
	var f []DeepFinding
	add := func(sev, code, title, detail string) {
		f = append(f, DeepFinding{Severity: sev, Code: code, Title: title, Detail: detail})
	}
	al := a.Allocation

	if al.Mismatch {
		sev := "warning"
		if al.Mem.UsageLivePct >= 85 || al.NodesMemAbove90 > 0 {
			sev = "critical"
		}
		add(sev, "allocation_mismatch", "Pool dimensionado pelo recurso errado", al.Explanation)
	}
	if al.NodesMemAbove90 > 0 {
		sev := "warning"
		if al.NodesMemAbove95 > 0 {
			sev = "critical"
		}
		add(sev, "nodes_memory_pressure", fmt.Sprintf("%d de %d nodes com 90%% ou mais de memória em uso", al.NodesMemAbove90, len(a.Nodes)),
			fmt.Sprintf("%d deles com 95%% ou mais. Risco de eviction, OOMKill e picos de latência (GC, perda de page cache).", al.NodesMemAbove95))
	}
	if al.Mem.LimitPct > 150 {
		add("warning", "memory_overcommit", fmt.Sprintf("Overcommit de memória: limits somam %.0f%% do alocável", al.Mem.LimitPct),
			"Os limits permitem que os pods cresçam muito além do que o node comporta. Reduza o limit para no máximo 1,5–2× o request.")
	}

	if ds := a.DaemonSets; ds.MemOverheadPct >= 20 {
		add("warning", "daemonset_overhead", fmt.Sprintf("DaemonSets ocupam %.0f%% da memória de cada node", ds.MemOverheadPct),
			fmt.Sprintf("Custo fixo por node: %.0f Mi de memória e %.0fm de CPU (≈ R$ %s/mês no pool). Nodes maiores diluem esse custo.",
				math.Max(ds.PerNodeMemUsageMi, ds.PerNodeMemReqMi), math.Max(ds.PerNodeCPUUsageMillis, ds.PerNodeCPUReqMillis), mdBrl(ds.MonthlyCostBRL)))
	}
	var dsNoReq []string
	for _, d := range a.DaemonSets.Items {
		if containsStr(d.Flags, "no_mem_request") && d.MemUsageAvgMi >= 100 {
			dsNoReq = append(dsNoReq, fmt.Sprintf("%s/%s (%.0f Mi)", d.Namespace, d.Name, d.MemUsageAvgMi))
		}
	}
	if len(dsNoReq) > 0 {
		add("warning", "daemonset_no_mem_request", "DaemonSets sem request de memória",
			"O scheduler não enxerga esse consumo: "+strings.Join(dsNoReq, ", ")+".")
	}

	var under, over []string
	for _, w := range a.Workloads {
		if containsStr(w.Flags, "mem_under_requested") {
			under = append(under, fmt.Sprintf("%s (%.0f%%)", w.Workload, w.MemUsageVsRequestPct))
		}
		if containsStr(w.Flags, "cpu_over_requested") && w.CPURequestSharePct >= 1 {
			over = append(over, fmt.Sprintf("%s (%.0f%% do pool)", w.Workload, w.CPURequestSharePct))
		}
	}
	if len(under) > 0 {
		add("warning", "memory_under_requested", fmt.Sprintf("%d %s mais memória do que pede%s", len(under), deepPlural(len(under), "workload usa", "workloads usam"), deepPlural(len(under), "", "m")),
			"Uso acima do request: o scheduler empacota pods demais por node. "+strings.Join(firstN(under, 8), ", ")+".")
	}
	if len(over) > 0 {
		add("info", "cpu_over_requested", fmt.Sprintf("%d %s com request de CPU 4× ou mais acima do uso", len(over), deepPlural(len(over), "workload", "workloads")),
			"Principais: "+strings.Join(firstN(over, 8), ", ")+".")
	}

	var memPinned, adjusted, pinnedMin, throttled []string
	for _, w := range a.Workloads {
		if w.HPA != nil {
			if w.HPA.State == "pinned_max" && containsStr(w.Flags, "hpa_memory_metric") {
				memPinned = append(memPinned, fmt.Sprintf("%s (%d/%d réplicas, %s)", w.Workload, w.HPA.Current, w.HPA.Max, hpaMetricsSummary(w.HPA)))
			}
			if w.HPA.State == "pinned_min" && w.CPURequestSharePct >= 5 {
				pinnedMin = append(pinnedMin, fmt.Sprintf("%s (min %d, %.0f%% da CPU reservada)", w.Workload, w.HPA.Min, w.CPURequestSharePct))
			}
		}
		if containsStr(w.Flags, "rec_adjusted_for_hpa") {
			adjusted = append(adjusted, w.Workload)
		}
		if containsStr(w.Flags, "cpu_throttled") {
			throttled = append(throttled, fmt.Sprintf("%s (P95 %.0f%%, limit %.0fm)", w.Workload, w.ThrottleP95Pct, w.CPULimMillis))
		}
	}
	if len(memPinned) > 0 {
		add("warning", "hpa_memory_pinned_max", "HPA por memória preso no máximo",
			"Em JVM o heap não é devolvido ao sistema, então a utilização de memória não cai depois de um pico e o HPA não reduz réplicas. Prefira escalar por CPU ou por métrica de requisições. "+strings.Join(firstN(memPinned, 8), ", ")+".")
	}
	if len(adjusted) > 0 {
		add("warning", "hpa_request_interaction", "Request recomendado ajustado ao alvo do HPA",
			"O HPA mede utilização como uso ÷ request: com o request recomendado puro, a utilização ficaria acima do alvo e o HPA tenderia a escalar. O recomendado foi elevado para manter a utilização no alvo: "+strings.Join(firstN(adjusted, 8), ", ")+".")
	}
	if len(pinnedMin) > 0 {
		add("info", "hpa_pinned_min", "Réplicas definidas pelo minReplicas, não pela carga",
			"O HPA quer menos réplicas, mas o mínimo segura. Revise o minReplicas com dados de pico: "+strings.Join(firstN(pinnedMin, 8), ", ")+".")
	}
	if len(throttled) > 0 {
		add("warning", "cpu_throttling", "Throttling de CPU (impacto em latência)",
			"Pods congelados pelo CPU limit em parte dos períodos, mesmo com uso médio baixo — causa clássica de p99 alto. Suba ou remova o CPU limit: "+strings.Join(firstN(throttled, 8), ", ")+".")
	}

	// Melhor alternativa factível com preço (excluindo a atual).
	var cur *DeepSimulation
	var best *DeepSimulation
	for i := range a.Simulation {
		s := &a.Simulation[i]
		if s.IsCurrent {
			cur = s
			continue
		}
		if !s.Feasible || !s.Available || s.PriceUSDHour <= 0 || s.NodeLossImpactPct > deepNodeLossWarnPct {
			continue
		}
		if best == nil || s.CostRecommendedBRL < best.CostRecommendedBRL {
			best = s
		}
	}
	if cur != nil && cur.Feasible && cur.SavingsRecommendedBRL > 0 {
		add("info", "rightsizing_savings", "Economia só com o ajuste de requests (mesma VM)",
			fmt.Sprintf("%s: %d → %d nodes, economia estimada de R$ %s/mês.", cur.VMSize, a.Overview.Nodes, cur.NodesRecommended, mdBrl(cur.SavingsRecommendedBRL)))
	}
	if best != nil && best.SavingsRecommendedBRL > 0 {
		add("info", "vm_change_savings", "Melhor alternativa de VM (após ajuste de requests)",
			fmt.Sprintf("%s (%d vCPU / %d GB, %s): %d nodes, R$ %s/mês, economia de R$ %s/mês.",
				best.VMSize, best.VCPU, best.MemGB, best.CPU, best.NodesRecommended, mdBrl(best.CostRecommendedBRL), mdBrl(best.SavingsRecommendedBRL)))
	}

	sevRank := map[string]int{"critical": 0, "warning": 1, "info": 2}
	sort.SliceStable(f, func(i, j int) bool { return sevRank[f[i].Severity] < sevRank[f[j].Severity] })
	if f == nil {
		f = []DeepFinding{}
	}
	return f
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func firstN(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), fmt.Sprintf("e mais %d", len(xs)-n))
}

// applySKUCaps aplica à simulação as capacidades do catálogo da região: disponibilidade, disco de SO
// efêmero, zonas e SMT real (vCPUsPerCore). Sem catálogo carregado, assume disponível.
func applySKUCaps(s *DeepSimulation, in DeepAnalysisInput, ov DeepPoolOverview) {
	s.Available = true
	if len(in.SKUCaps) == 0 {
		return
	}
	s.CatalogKnown = true
	caps, ok := in.SKUCaps[strings.ToLower(s.VMSize)]
	if !ok {
		s.Available = false
		s.Notes = append(s.Notes, "SKU não oferecida na região.")
		return
	}
	eph := caps.EphemeralOSDisk
	s.EphemeralOSDisk = &eph
	s.Zones = caps.Zones
	s.VCPUsPerCore = caps.VCPUsPerCore
	if caps.VCPUsPerCore > 0 {
		s.SMT = caps.VCPUsPerCore > 1
	}
	if caps.Restricted {
		s.Available = false
		s.Notes = append(s.Notes, "SKU restrita para a assinatura na região ("+caps.RestrictionReason+").")
		return
	}
	if !eph && ov.OSDisk == "ephemeral" {
		s.Notes = append(s.Notes, "Não suporta disco de SO efêmero (o pool atual usa): exige disco Managed.")
	}
	if missing := missingZones(ov.Zones, caps.Zones); len(missing) > 0 {
		s.Notes = append(s.Notes, "Indisponível na(s) zona(s) "+strings.Join(missing, ", ")+" usada(s) pelo pool.")
	}
}

// missingZones devolve as zonas do pool ("brazilsouth-2" → "2") ausentes das zonas da SKU.
func missingZones(poolZones, skuZones []string) []string {
	have := map[string]bool{}
	for _, z := range skuZones {
		have[z] = true
	}
	var miss []string
	for _, pz := range poolZones {
		z := pz
		if i := strings.LastIndex(pz, "-"); i >= 0 {
			z = pz[i+1:]
		}
		if z != "" && z != "0" && !have[z] {
			miss = append(miss, z)
		}
	}
	return miss
}

// deepPlural escolhe a forma singular ou plural conforme n.
func deepPlural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
