package finops

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Reserva / Savings Plan na simulação da Deep Analysis.
//
// A simulação calcula custo com preço de TABELA. Num pool coberto por reserva isso engana muito: no
// calculofrete, a tabela dava R$ 48,7 mil/mês e o custo efetivo (Cost Management) era ~R$ 14,7 mil, com
// 99% sob reserva. Aqui o custo efetivo do pool vira a base, e cada SKU da simulação ganha o intervalo
// real (pior/melhor caso) do custo depois da troca — com as mesmas cautelas de coverage.go: sem número
// quando não dá para calcular sem chute (Savings Plan relevante, moeda ≠ BRL, pool Spot).

// DeepCoverage é a cobertura de reserva/Savings Plan do pool, convertida para a simulação.
type DeepCoverage struct {
	Reservation float64 `json:"reservation"` // participação no custo efetivo (0..1)
	SavingsPlan float64 `json:"savings_plan"`
	OnDemand    float64 `json:"on_demand"`
	Spot        float64 `json:"spot"`
	// Custos mensais (R$) derivados da janela do Cost Management.
	EffectiveMonthlyBRL   float64 `json:"effective_monthly_brl"`
	ReservedMonthlyBRL    float64 `json:"reserved_monthly_brl"`
	SavingsPlanMonthlyBRL float64 `json:"savings_plan_monthly_brl"`
	OnDemandMonthlyBRL    float64 `json:"on_demand_monthly_brl"`
	// ReservedNodes: nodes do SKU atual cobertos pela reserva (estimativa: a parte sob demanda é
	// cobrada a preço de tabela, então nodes sob demanda = custo sob demanda ÷ tabela por node).
	ReservedNodes float64 `json:"reserved_nodes"`
	// ReservedNodeMonthlyBRL: custo efetivo de um node reservado (custo da reserva ÷ nodes reservados).
	ReservedNodeMonthlyBRL float64 `json:"reserved_node_monthly_brl"`
	// TableDiscountPct: desconto da reserva sobre a tabela, por node.
	TableDiscountPct float64   `json:"table_discount_pct"`
	Currency         string    `json:"currency,omitempty"`
	WindowDays       int       `json:"window_days"`
	FetchedAt        time.Time `json:"fetched_at"`
	// Computable: dá para calcular os cenários sem chute. Quando false, Note diz por quê.
	Computable bool   `json:"computable"`
	Note       string `json:"note,omitempty"`
}

// DeepCoverageScenario é o custo efetivo de uma SKU da simulação (cenário de requests recomendados).
type DeepCoverageScenario struct {
	// Basis: same_sku | same_series | other_series.
	Basis           string  `json:"basis"`
	WorstMonthlyBRL float64 `json:"worst_monthly_brl"`
	BestMonthlyBRL  float64 `json:"best_monthly_brl"`
	// Economia em relação ao custo EFETIVO atual (positivo = economiza).
	WorstSavingsBRL float64 `json:"worst_savings_brl"`
	BestSavingsBRL  float64 `json:"best_savings_brl"`
	// IdleReservedNodes: capacidade reservada que sobra (em nodes do SKU atual).
	IdleReservedNodes float64 `json:"idle_reserved_nodes"`
	Note              string  `json:"note"`
}

// buildDeepCoverage converte a cobertura gravada (PoolCoverage) para a simulação. tableNodeMonthBRL é o
// custo de tabela de um node do SKU atual por mês; nodes, o nº atual de nodes do pool.
func buildDeepCoverage(cov *PoolCoverage, tableNodeMonthBRL float64, nodes int) *DeepCoverage {
	if cov == nil || cov.WindowDays <= 0 || cov.EffectiveCost <= 0 {
		return nil
	}
	monthFactor := (float64(HoursPerMonth) / 24) / float64(cov.WindowDays)
	eff := cov.EffectiveCost * monthFactor
	d := &DeepCoverage{
		Reservation: round2(cov.Reservation), SavingsPlan: round2(cov.SavingsPlan),
		OnDemand: round2(cov.OnDemand), Spot: round2(cov.Spot),
		EffectiveMonthlyBRL:   round2(eff),
		ReservedMonthlyBRL:    round2(cov.Reservation * eff),
		SavingsPlanMonthlyBRL: round2(cov.SavingsPlan * eff),
		OnDemandMonthlyBRL:    round2(cov.OnDemand * eff),
		Currency:              cov.Currency, WindowDays: cov.WindowDays, FetchedAt: cov.FetchedAt,
	}
	switch {
	case cov.Currency != "" && !strings.EqualFold(cov.Currency, "BRL"):
		d.Note = "Custo do Cost Management em " + cov.Currency + ": os cenários em R$ não foram calculados."
		return d
	case cov.Spot >= coverageWarnShare:
		d.Note = "Pool Spot: reserva não se aplica."
		return d
	case cov.SavingsPlan >= coverageWarnShare:
		d.Note = fmt.Sprintf("Savings Plan cobre ~%s do custo efetivo. Ele vale para várias famílias e o compromisso por hora segue sendo cobrado: o efeito de uma troca depende do resto do consumo coberto pelo plano, então não há número sem chute.", pct(cov.SavingsPlan))
		return d
	case tableNodeMonthBRL <= 0 || nodes <= 0:
		d.Note = "Sem preço de tabela do SKU atual: cenários não calculados."
		return d
	}
	// Nodes fora da reserva: sob demanda (tabela) + o pouco de Savings Plan (aproximado pela tabela,
	// < 20% do custo).
	nonReserved := (d.OnDemandMonthlyBRL + d.SavingsPlanMonthlyBRL) / tableNodeMonthBRL
	d.ReservedNodes = round2(math.Max(0, math.Min(float64(nodes), float64(nodes)-nonReserved)))
	if d.ReservedNodes > 0 {
		d.ReservedNodeMonthlyBRL = round2(d.ReservedMonthlyBRL / d.ReservedNodes)
		d.TableDiscountPct = round2(math.Max(0, (1-d.ReservedNodeMonthlyBRL/tableNodeMonthBRL)*100))
	}
	d.Computable = true
	return d
}

// deepCoverageScenario calcula o custo efetivo de rodar `nodes` nodes da SKU alt depois da troca.
// cur/alt: specs; altTableNodeMonthBRL: tabela por node da alt; sameSeries: mesma série do SKU atual.
//
// Premissas (na nota): os nodes atuais rodaram a janela inteira; a reserva tem flexibilidade de
// tamanho (padrão da Azure) e continua paga até vencer ou ser trocada.
func deepCoverageScenario(c *DeepCoverage, cur, alt DeepSKUSpec, nodes int, altTableNodeMonthBRL float64) *DeepCoverageScenario {
	if c == nil || !c.Computable || nodes <= 0 || altTableNodeMonthBRL <= 0 {
		return nil
	}
	R := c.ReservedNodes
	perRes := c.ReservedNodeMonthlyBRL
	sc := &DeepCoverageScenario{}
	n := float64(nodes)

	switch {
	case strings.EqualFold(cur.VMSize, alt.VMSize) || (SeriesKey(cur.VMSize) != "" && SeriesKey(cur.VMSize) == SeriesKey(alt.VMSize)):
		// Mesma série: a reserva cobre a nova VM na proporção dos vCPUs (flexibilidade de tamanho).
		sc.Basis = "same_series"
		ratio := 1.0
		if cur.VCPU > 0 && alt.VCPU > 0 {
			ratio = float64(alt.VCPU) / float64(cur.VCPU)
		}
		if strings.EqualFold(cur.VMSize, alt.VMSize) {
			sc.Basis = "same_sku"
		}
		units := n * ratio // demanda em nodes do SKU atual
		excess := math.Max(0, units-R)
		excessCost := excess / ratio * altTableNodeMonthBRL
		sc.IdleReservedNodes = round2(math.Max(0, R-units))
		sc.WorstMonthlyBRL = c.ReservedMonthlyBRL + excessCost
		sc.BestMonthlyBRL = math.Min(units, R)*perRes + excessCost
		if sc.IdleReservedNodes >= 0.5 {
			sc.Note = fmt.Sprintf("A reserva cobre ~%.0f nodes de %s e o pool passaria a usar ~%.0f: sobram ~%.0f nodes reservados (%s/mês). Pior caso: seguem pagos e ociosos. Melhor caso: outro pool ou cluster com o mesmo SKU/série no escopo da reserva os aproveita.",
				R, cur.VMSize, units, sc.IdleReservedNodes, brlInt(sc.IdleReservedNodes*perRes))
		} else {
			sc.Note = fmt.Sprintf("A reserva (~%.0f nodes de %s) segue integralmente usada; o que passar dela é cobrado a preço de tabela.", R, cur.VMSize)
		}
		if sc.Basis == "same_series" {
			sc.Note += " Supõe reserva com flexibilidade de tamanho (padrão da Azure)."
		}
	default:
		// Outra série: a reserva não cobre a nova VM.
		sc.Basis = "other_series"
		altAll := n * altTableNodeMonthBRL
		sc.IdleReservedNodes = round2(R)
		sc.WorstMonthlyBRL = c.ReservedMonthlyBRL + altAll
		sc.BestMonthlyBRL = altAll
		sc.Note = fmt.Sprintf("A reserva de %s (~%.0f nodes, %s/mês) não cobre %s. Pior caso: segue paga e ociosa, e os %d nodes novos saem a preço de tabela. Melhor caso: a reserva é aproveitada por outras VMs %s no escopo dela ou trocada.",
			cur.VMSize, R, brlInt(c.ReservedMonthlyBRL), alt.VMSize, nodes, cur.VMSize)
	}
	sc.WorstMonthlyBRL = round2(sc.WorstMonthlyBRL)
	sc.BestMonthlyBRL = round2(sc.BestMonthlyBRL)
	sc.WorstSavingsBRL = round2(c.EffectiveMonthlyBRL - sc.WorstMonthlyBRL)
	sc.BestSavingsBRL = round2(c.EffectiveMonthlyBRL - sc.BestMonthlyBRL)
	return sc
}
