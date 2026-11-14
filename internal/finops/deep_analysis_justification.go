package finops

import (
	"fmt"
	"math"
)

// Veredito da troca de VM, contra a VM atual do pool.
const (
	VerdictBase         = "base"          // a própria VM atual
	VerdictJustified    = "justified"     // economia que se sustenta (inclusive no pior caso, com reserva)
	VerdictConditional  = "conditional"   // economia só se a reserva atual for reaproveitada/trocada, ou com ressalva séria
	VerdictTechnical    = "technical"     // sem economia relevante, mas com ganho mensurável para os workloads
	VerdictNotJustified = "not_justified" // nada que pague a troca
	VerdictUnknown      = "unknown"       // faltam preço/dados para dizer
)

// deepTechnicalMaxCostIncreasePct: acima disso, um ganho técnico não paga o custo a mais.
const deepTechnicalMaxCostIncreasePct = 10.0

// DeepJustification explica POR QUE trocar (ou não) a VM atual por esta SKU: economia (tabela, reserva ou
// Savings Plan) e ganhos para os workloads, sempre contra a VM atual, no cenário de requests recomendados.
type DeepJustification struct {
	Verdict  string   `json:"verdict"`
	Headline string   `json:"headline"`
	Pros     []string `json:"pros"`
	Cons     []string `json:"cons"`
}

func justBRL(v float64) string { return "R$ " + mdBrl(math.Abs(v)) }

// buildJustification monta a justificativa de s contra cur (a VM atual). covOK: o pool tem reserva/Savings Plan
// com cenário numérico. Só afirma o que os números sustentam; sem preço ou sem base, o veredito é "unknown".
func buildJustification(s, cur DeepSimulation, ds DeepDaemonSetOverhead, workloads []DeepWorkloadRow, covOK bool) DeepJustification {
	j := DeepJustification{Pros: []string{}, Cons: []string{}}
	if s.IsCurrent {
		j.Verdict, j.Headline = VerdictBase, "VM atual: base da comparação"
		return j
	}
	if s.CatalogKnown && !s.Available {
		j.Verdict, j.Headline = VerdictNotJustified, "Indisponível na região do cluster"
		j.Cons = append(j.Cons, "A SKU não está disponível na região (catálogo da Azure).")
		return j
	}
	if !s.Feasible {
		j.Verdict, j.Headline = VerdictNotJustified, "Não comporta os DaemonSets por node"
		return j
	}
	if !s.VsCurrentOK {
		j.Verdict, j.Headline = VerdictUnknown, "Sem preço ou sem base para comparar com a VM atual"
		return j
	}

	// ── Custo ──────────────────────────────────────────────────────────────────────────────────
	costSafe, costConditional, costWorse := false, false, false
	var costIncreasePct float64
	table := s.SavingsVsCurrentRecBRL
	if cur.CostRecommendedBRL > 0 && table < 0 {
		costIncreasePct = -table / cur.CostRecommendedBRL * 100
	}
	if covOK && s.Coverage != nil {
		w, b := s.Coverage.WorstSavingsBRL, s.Coverage.BestSavingsBRL
		switch {
		case w > 0:
			costSafe = true
			j.Pros = append(j.Pros, fmt.Sprintf("Economia efetiva de %s/mês mesmo no pior caso (a reserva atual segue paga e ociosa); até %s/mês se ela for reaproveitada.", justBRL(w), justBRL(b)))
		case b > 0:
			costConditional = true
			j.Pros = append(j.Pros, fmt.Sprintf("Economia efetiva de até %s/mês, só se a reserva atual for reaproveitada por outro pool/cluster ou trocada.", justBRL(b)))
			j.Cons = append(j.Cons, fmt.Sprintf("No pior caso (a reserva atual fica ociosa) o custo efetivo sobe %s/mês.", justBRL(w)))
		default:
			costWorse = true
			j.Cons = append(j.Cons, fmt.Sprintf("O custo efetivo é maior que o de hoje mesmo no melhor caso (%s/mês a mais).", justBRL(b)))
		}
		if table > 0 {
			j.Pros = append(j.Pros, fmt.Sprintf("Preço de tabela: %s/mês a menos que a VM atual.", justBRL(table)))
		} else if table < 0 {
			j.Cons = append(j.Cons, fmt.Sprintf("Preço de tabela: %s/mês a mais que a VM atual.", justBRL(table)))
		}
	} else {
		switch {
		case table > 0:
			costSafe = true
			pct := 0.0
			if cur.CostRecommendedBRL > 0 {
				pct = table / cur.CostRecommendedBRL * 100
			}
			j.Pros = append(j.Pros, fmt.Sprintf("Economiza %s/mês (%.0f%%) contra a VM atual, preço de tabela sob demanda.", justBRL(table), pct))
		case table < 0:
			costWorse = true
			j.Cons = append(j.Cons, fmt.Sprintf("Custa %s/mês a mais (%.0f%%) que a VM atual.", justBRL(table), costIncreasePct))
		default:
			j.Cons = append(j.Cons, "Mesmo custo da VM atual.")
		}
	}
	if h := s.ReservedHint; h != nil {
		kind := "reserva"
		if h.Kind == "savings_plan" {
			kind = "Savings Plan"
		}
		j.Pros = append(j.Pros, fmt.Sprintf("Esta SKU já roda sob %s na frota (escopo %s): a troca aproveita um desconto que já é pago.", kind, map[bool]string{true: "série", false: "SKU"}[h.Scope == "series"]))
	}

	// ── Ganhos para os workloads ───────────────────────────────────────────────────────────────
	tech := 0
	if d := s.NodesDeltaRecommended; d < 0 {
		tech++
		j.Pros = append(j.Pros, fmt.Sprintf("%d nodes a menos (%d → %d): deixa de duplicar DaemonSets em %d nodes (≈ %.1f vCPU e %.1f GB de requests fixos).",
			-d, cur.NodesRecommended, s.NodesRecommended, -d, float64(-d)*ds.PerNodeCPUReqMillis/1000, float64(-d)*ds.PerNodeMemReqMi/1024))
	} else if d > 0 {
		j.Cons = append(j.Cons, fmt.Sprintf("%d nodes a mais (%d → %d): mais DaemonSets duplicados (≈ %.1f vCPU e %.1f GB de requests fixos).",
			d, cur.NodesRecommended, s.NodesRecommended, float64(d)*ds.PerNodeCPUReqMillis/1000, float64(d)*ds.PerNodeMemReqMi/1024))
	}
	if cur.NodeLossImpactPct-s.NodeLossImpactPct >= 5 {
		tech++
		j.Pros = append(j.Pros, fmt.Sprintf("Perder 1 node derruba %.0f%% do pool (hoje %.0f%%): menor impacto de uma falha.", s.NodeLossImpactPct, cur.NodeLossImpactPct))
	}
	if s.NodeLossImpactPct > deepNodeLossWarnPct && s.NodeLossImpactPct > cur.NodeLossImpactPct {
		j.Cons = append(j.Cons, fmt.Sprintf("Perder 1 node derruba %.0f%% do pool (hoje %.0f%%): poucos nodes grandes concentram risco.", s.NodeLossImpactPct, cur.NodeLossImpactPct))
	}
	if thr := throttledWorkloads(workloads); thr > 0 && cur.SMT && !s.SMT {
		tech++
		j.Pros = append(j.Pros, fmt.Sprintf("%d workload(s) com throttling de CPU: sem SMT, 1 vCPU = 1 core físico, o que ajuda a latência.", thr))
	}
	if big := biggestPodSharePct(workloads, s.AllocCPUMillis, s.AllocMemMi); big > 0 {
		curBig := biggestPodSharePct(workloads, cur.AllocCPUMillis, cur.AllocMemMi)
		if curBig-big >= 5 {
			tech++
			j.Pros = append(j.Pros, fmt.Sprintf("O maior pod ocupa %.0f%% de um node (hoje %.0f%%): mais folga para encaixar pods e menos fragmentação.", big, curBig))
		} else if big > 50 && big > curBig {
			j.Cons = append(j.Cons, fmt.Sprintf("O maior pod ocupa %.0f%% de um node (hoje %.0f%%): pouco espaço para encaixar outros pods.", big, curBig))
		}
	}
	if s.EphemeralOSDisk != nil && !*s.EphemeralOSDisk {
		j.Cons = append(j.Cons, "Sem suporte a disco de SO efêmero: o disco do SO é cobrado e mais lento.")
	}
	if s.AllocEstimated {
		j.Cons = append(j.Cons, "Alocável por node estimado pela fórmula da AKS (não medido): confirme antes de migrar.")
	}

	// ── Veredito ───────────────────────────────────────────────────────────────────────────────
	switch {
	case costSafe && s.NodeLossImpactPct <= deepNodeLossWarnPct:
		j.Verdict, j.Headline = VerdictJustified, "Justificada pelo custo"
		if tech > 0 {
			j.Headline += " e com ganho para os workloads"
		}
	case costSafe || costConditional:
		j.Verdict = VerdictConditional
		j.Headline = "Economia com ressalva"
		if costConditional {
			j.Headline = "Só se a reserva atual for reaproveitada"
		}
	case tech > 0 && (!costWorse || costIncreasePct <= deepTechnicalMaxCostIncreasePct):
		j.Verdict = VerdictTechnical
		j.Headline = "Ganho para os workloads, sem economia"
		if costIncreasePct > 0 {
			j.Headline = fmt.Sprintf("Ganho para os workloads por +%.0f%% de custo", costIncreasePct)
		}
	default:
		j.Verdict, j.Headline = VerdictNotJustified, "Nada que pague a troca"
	}
	return j
}

func throttledWorkloads(ws []DeepWorkloadRow) int {
	n := 0
	for _, w := range ws {
		if w.HasThrottle && w.ThrottleP95Pct >= deepThrottleWarnPct {
			n++
		}
	}
	return n
}

// biggestPodSharePct é o maior percentual de um node (alocável) que um único pod do pool pede, por CPU ou memória.
func biggestPodSharePct(ws []DeepWorkloadRow, allocCPU, allocMem float64) float64 {
	var best float64
	for _, w := range ws {
		if allocCPU > 0 {
			best = math.Max(best, w.CPUReqMillis/allocCPU*100)
		}
		if allocMem > 0 {
			best = math.Max(best, w.MemReqMi/allocMem*100)
		}
	}
	return round2(best)
}

// fillJustifications preenche a justificativa de cada SKU contra a VM atual.
func fillJustifications(sims []DeepSimulation, ds DeepDaemonSetOverhead, workloads []DeepWorkloadRow, covOK bool) {
	var cur DeepSimulation
	for _, s := range sims {
		if s.IsCurrent {
			cur = s
			break
		}
	}
	for i := range sims {
		j := buildJustification(sims[i], cur, ds, workloads, covOK)
		sims[i].Justification = &j
	}
}
