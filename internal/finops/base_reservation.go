package finops

import (
	"math"
	"strings"
)

// Reserva para a carga base (FINOPS-REVISAO-PLAN.md, Fase 3): por pool, quantos nodes ficam ligados
// o tempo todo (mínimo do autoscaler, ou o tamanho do pool fixo), quantos já estão cobertos por
// reserva/Savings Plan (pela cobertura do Cost Management) e quanto economizaria comprometer o resto.

// PoolScale é o min/autoscaling ao vivo do pool (gravado pelo scan no rightsizing).
type PoolScale struct {
	Min         int
	Autoscaling bool
}

// CommitmentOption é um modelo de compromisso para os nodes sugeridos.
type CommitmentOption struct {
	Kind          string  `json:"kind"` // reservation | savings_plan
	Term          string  `json:"term"` // 1y | 3y
	MonthlyNode   float64 `json:"monthly_node_brl"`
	SavingMonth   float64 `json:"saving_month_brl"` // para os nodes sugeridos
	SavingPct     float64 `json:"saving_pct"`
	CommitmentBRL float64 `json:"commitment_brl"` // compromisso total no prazo (nodes × preço × meses)
}

// BaseReservationSuggestion é a linha de um pool.
type BaseReservationSuggestion struct {
	Pool           string             `json:"pool"`
	VMSize         string             `json:"vm_size"`
	Nodes          int                `json:"nodes"`
	Autoscaling    bool               `json:"autoscaling"`
	MinNodes       int                `json:"min_nodes,omitempty"`
	BaseNodes      int                `json:"base_nodes"`
	BaseSource     string             `json:"base_source"`      // hpa_min | autoscaler_min | fixed
	NodesAtHPAMin  int                `json:"nodes_at_hpa_min"` // nodes que os requests pedem com todo HPA no mínimo
	CoveredNodes   int                `json:"covered_nodes"`    // estimado pela fatia de reserva+SP do custo
	SuggestedNodes int                `json:"suggested_nodes"`  // base − coberto
	OnDemandNode   float64            `json:"on_demand_node_brl"`
	Options        []CommitmentOption `json:"options,omitempty"`
	Skip           string             `json:"skip,omitempty"` // por que não há sugestão
}

// basePackingEfficiency: fração da capacidade alocável que os pods conseguem ocupar na prática
// (fragmentação entre nodes, anti-afinidade, folga do scheduler).
const basePackingEfficiency = 0.85

// nodesAtHPAMin: quantos nodes do pool os requests pedem com todo HPA no mínimo (pods sem HPA
// contam como estão). DaemonSets entram como custo fixo por node, abatido da capacidade.
func nodesAtHPAMin(p FinOpsPool, workloads []FinOpsWorkload) int {
	nodes := p.AllocatableNodes
	cpuCap, memCap := float64(p.AllocatableCPUMillicores), float64(p.AllocatableMemoryMi)
	if nodes <= 0 || cpuCap <= 0 || memCap <= 0 {
		nodes, cpuCap, memCap = p.NodeCount, float64(p.TotalCPUMillicores), float64(p.TotalMemoryMi)
	}
	if nodes <= 0 || cpuCap <= 0 || memCap <= 0 {
		return 0
	}
	perCPU, perMem := cpuCap/float64(nodes), memCap/float64(nodes)
	var needCPU, needMem float64
	for _, w := range workloads {
		pods := w.Pods
		if pp, ok := w.PoolPods[p.Name]; ok {
			pods = pp
		} else if w.NodePool != p.Name {
			continue
		}
		if pods <= 0 {
			continue
		}
		if strings.EqualFold(w.Kind, "DaemonSet") {
			perCPU -= w.CPURequestMillis
			perMem -= w.MemRequestMi
			continue
		}
		minPods := float64(pods)
		if w.HPAMax > 0 && w.HPAMin > 0 && w.HPAMin < w.HPACurrent && w.Pods > 0 {
			// Parte deste pool no mínimo do HPA (o HPA conta réplicas do workload inteiro).
			minPods = float64(w.HPAMin) * float64(pods) / float64(w.Pods)
		}
		needCPU += w.CPURequestMillis * minPods
		needMem += w.MemRequestMi * minPods
	}
	perCPU, perMem = perCPU*basePackingEfficiency, perMem*basePackingEfficiency
	if perCPU <= 0 || perMem <= 0 {
		return 0
	}
	return int(math.Ceil(math.Max(needCPU/perCPU, needMem/perMem)))
}

// SuggestBaseReservations monta as sugestões. prices: por vm_size (USD/VM/mês); rate: USD→BRL.
// A carga base é o maior entre o mínimo do autoscaler e os nodes que o pool pede com todo HPA no
// mínimo — limitada ao tamanho atual; pool fixo (sem autoscaler) = o pool inteiro.
func SuggestBaseReservations(pools []FinOpsPool, workloads []FinOpsWorkload, scale map[string]PoolScale, prices map[string]CommitmentPrices, rate float64) []BaseReservationSuggestion {
	out := make([]BaseReservationSuggestion, 0, len(pools))
	for _, p := range pools {
		s := BaseReservationSuggestion{Pool: p.Name, VMSize: p.VMSize, Nodes: p.NodeCount}
		sc, known := scale[strings.ToLower(p.Name)]
		s.Autoscaling = known && sc.Autoscaling
		s.NodesAtHPAMin = nodesAtHPAMin(p, workloads)
		s.BaseNodes, s.BaseSource = p.NodeCount, "fixed"
		if s.Autoscaling {
			s.MinNodes = sc.Min
			s.BaseNodes, s.BaseSource = sc.Min, "autoscaler_min"
			if s.NodesAtHPAMin > sc.Min {
				s.BaseNodes, s.BaseSource = s.NodesAtHPAMin, "hpa_min"
			}
			s.BaseNodes = min(s.BaseNodes, p.NodeCount)
		}
		if c := p.Coverage; c != nil {
			s.CoveredNodes = int(math.Round((c.Reservation + c.SavingsPlan) * float64(p.NodeCount)))
		}
		s.SuggestedNodes = max(0, s.BaseNodes-s.CoveredNodes)

		pr, ok := prices[normalizeVMSize(p.VMSize)]
		switch {
		case p.Coverage != nil && p.Coverage.Spot >= 0.5, strings.Contains(strings.ToLower(p.Name), "spot"):
			s.Skip = "pool Spot: já é mais barato que reserva e pode perder os nodes a qualquer momento"
		case !known:
			s.Skip = "min do autoscaler desconhecido — rode Analisar com a análise histórica para gravar"
		case s.BaseNodes == 0:
			s.Skip = "o pool pode ir a 0 nodes: não há carga base"
		case s.SuggestedNodes == 0:
			s.Skip = "a carga base já está coberta por reserva/Savings Plan"
		case !ok || pr.OnDemand == 0:
			s.Skip = "sem preço de reserva para este SKU na Retail Prices API"
		}
		if ok && pr.OnDemand > 0 {
			s.OnDemandNode = round2(pr.OnDemand * rate)
		}
		if s.Skip == "" {
			add := func(kind, term string, usd float64, months int) {
				if usd <= 0 || usd >= pr.OnDemand {
					return
				}
				n := float64(s.SuggestedNodes)
				s.Options = append(s.Options, CommitmentOption{
					Kind: kind, Term: term,
					MonthlyNode:   round2(usd * rate),
					SavingMonth:   round2((pr.OnDemand - usd) * rate * n),
					SavingPct:     round2((1 - usd/pr.OnDemand) * 100),
					CommitmentBRL: round2(usd * rate * n * float64(months)),
				})
			}
			add("reservation", "1y", pr.Reservation1Y, 12)
			add("reservation", "3y", pr.Reservation3Y, 36)
			add("savings_plan", "1y", pr.SavingsPlan1Y, 12)
			add("savings_plan", "3y", pr.SavingsPlan3Y, 36)
			if len(s.Options) == 0 {
				s.Skip = "sem preço de reserva para este SKU na Retail Prices API"
			}
		}
		out = append(out, s)
	}
	return out
}
