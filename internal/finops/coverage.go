package finops

import (
	"fmt"
	"strings"
	"time"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// Cobertura de reserva / Savings Plan de um node pool e o aviso ao trocar de SKU.
//
// As sugestões de tier calculam a economia com o preço de TABELA (pay-as-you-go). Num pool coberto por
// reserva isso engana: a reserva é paga de qualquer jeito, então trocar o SKU não devolve o valor de
// tabela — e, se a nova VM for de outra série, ela sai da cobertura e a reserva fica ociosa. Este
// arquivo mede o quanto do custo efetivo do pool vem de cada modelo de preço (lido do Cost Management)
// e gera o aviso por alternativa. É informativo: não altera o cálculo de economia nem o veredito.

const (
	// coverageWarnShare: a partir de quanto do custo efetivo vir de reserva/Savings Plan o aviso aparece.
	coverageWarnShare = 0.20

	CoverageLevelReservation = "reservation"
	CoverageLevelSavingsPlan = "savings_plan"
)

// PoolCoverage é a participação (0..1) de cada modelo de preço no custo amortizado do pool na janela.
// É fração do CUSTO efetivo, não das horas: uma hora coberta por reserva custa menos que uma sob
// demanda, então a fatia de reserva aparece um pouco menor que a fatia real de horas.
type PoolCoverage struct {
	Reservation float64 `json:"reservation"`
	SavingsPlan float64 `json:"savings_plan"`
	OnDemand    float64 `json:"on_demand"`
	Spot        float64 `json:"spot"`
	Other       float64 `json:"other,omitempty"`
	// EffectiveCost é o custo amortizado total do pool na janela (na moeda da subscription).
	EffectiveCost float64   `json:"effective_cost"`
	Currency      string    `json:"currency,omitempty"`
	WindowDays    int       `json:"window_days"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// BuildPoolCoverage converte o custo por modelo de preço em participações. nil quando não há custo
// (pool sem VM cobrada na janela: não dá pra afirmar cobertura nenhuma).
func BuildPoolCoverage(costByModel map[string]float64, currency string, windowDays int, fetchedAt time.Time) *PoolCoverage {
	var total float64
	for _, v := range costByModel {
		if v > 0 {
			total += v
		}
	}
	if total <= 0 {
		return nil
	}
	share := func(model string) float64 {
		if v := costByModel[model]; v > 0 {
			return v / total
		}
		return 0
	}
	return &PoolCoverage{
		Reservation:   share(azure.ModelReservation),
		SavingsPlan:   share(azure.ModelSavingsPlan),
		OnDemand:      share(azure.ModelOnDemand),
		Spot:          share(azure.ModelSpot),
		Other:         share(azure.ModelOther),
		EffectiveCost: total,
		Currency:      currency,
		WindowDays:    windowDays,
		FetchedAt:     fetchedAt,
	}
}

// CoverageWarning é o aviso, por alternativa de SKU, de que a economia mostrada (preço de tabela)
// pode não se realizar porque o pool está sob reserva/Savings Plan.
type CoverageWarning struct {
	Level string `json:"level"` // reservation | savings_plan
	Note  string `json:"note"`
	// Scenarios: o intervalo real do efeito da troca em pool com reserva (só quando dá para calcular
	// sem chute — ver AssessSwapScenarios). Preenchido pelo chamador, que conhece nodes e câmbio.
	Scenarios *SwapScenarios `json:"scenarios,omitempty"`
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }

// AssessCoverageWarning avalia a troca do SKU atual pela alternativa. nil quando não há o que avisar
// (mesmo SKU, sem cobertura consultada, ou cobertura de reserva/plano abaixo do limite — inclui o pool
// spot, onde reserva não se aplica).
func AssessCoverageWarning(currentSKU, altSKU string, cov *PoolCoverage) *CoverageWarning {
	if cov == nil || strings.EqualFold(strings.TrimSpace(currentSKU), strings.TrimSpace(altSKU)) {
		return nil
	}
	hasRes := cov.Reservation >= coverageWarnShare
	hasSP := cov.SavingsPlan >= coverageWarnShare
	if !hasRes && !hasSP {
		return nil
	}

	var note string
	level := CoverageLevelSavingsPlan
	if hasRes {
		level = CoverageLevelReservation
		curSeries, altSeries := SeriesKey(currentSKU), SeriesKey(altSKU)
		if curSeries != "" && curSeries == altSeries {
			note = fmt.Sprintf("Reserva cobre ~%s do custo efetivo deste pool. %s é da mesma série de %s: se a reserva tiver flexibilidade de tamanho ela continua valendo, mas um tamanho diferente consome a reserva proporcionalmente — a parte que sobrar só não vira desperdício se outras VMs do mesmo SKU dentro do escopo da reserva (subscription/conta) a absorverem (e o que passar do reservado é cobrado sob demanda).",
				pct(cov.Reservation), altSKU, currentSKU)
		} else {
			note = fmt.Sprintf("Reserva cobre ~%s do custo efetivo deste pool. %s não é da mesma série de %s: a reserva atual não cobre a nova VM — os nodes trocados seriam cobrados sob demanda, e a reserva só não vira desperdício se outras VMs do mesmo SKU dentro do escopo dela (subscription/conta) a absorverem, ou se for trocada.",
				pct(cov.Reservation), altSKU, currentSKU)
		}
		if hasSP {
			note += fmt.Sprintf(" Um Savings Plan cobre outros ~%s.", pct(cov.SavingsPlan))
		}
		note += " A economia mostrada usa preço de tabela e pode não se realizar."
	} else {
		note = fmt.Sprintf("Savings Plan cobre ~%s do custo efetivo deste pool. Ele vale para várias famílias, então a troca tende a manter o desconto — mas o valor comprometido por hora continua sendo cobrado: a economia só existe se o plano seguir totalmente consumido por outros recursos. A economia mostrada usa preço de tabela.",
			pct(cov.SavingsPlan))
	}
	return &CoverageWarning{Level: level, Note: note}
}

// SwapInputs são os dados de uma alternativa que o cálculo dos cenários precisa (do resultado do
// SuggestVMTier e do câmbio) — a coverage.go não conhece nodes nem preços.
type SwapInputs struct {
	NodeCount       int     // nodes do pool usados no cálculo da economia de tabela
	AltPriceUSDHour float64 // preço de tabela da alternativa
	ExchangeRate    float64 // USD → BRL
	TableSavingsBRL float64 // economia de tabela mostrada hoje (positivo = economia)
	CostDeltaPct    float64 // variação do preço unitário da alternativa (negativo = mais barata)
}

// SwapScenarios é o intervalo do efeito da troca no gasto TOTAL, em R$/mês, sinal de economia
// (positivo = economiza, negativo = gasta mais). Os dois extremos existem porque a reserva é paga de
// qualquer jeito: o que decide é se ela é reaproveitada por outras VMs do mesmo SKU no escopo dela, e
// isso a aplicação não consegue ler (falta o papel Reservations Reader).
type SwapScenarios struct {
	// BestCaseSavingsBRL: a reserva liberada é aproveitada por outras VMs → a economia de tabela.
	BestCaseSavingsBRL float64 `json:"best_case_savings_brl"`
	// WorstCaseSavingsBRL: a reserva fica ociosa (segue paga) → só o que a parte sob demanda de hoje
	// economiza; numa troca de série o custo sobe (valor negativo).
	WorstCaseSavingsBRL float64 `json:"worst_case_savings_brl"`
	Note                string  `json:"note"`
}

// brlInt formata R$ com separador de milhar ("37.481"), sem casas decimais.
func brlInt(v float64) string {
	n := int64(v + 0.5)
	if v < 0 {
		n = int64(v - 0.5)
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-R$ " + b.String()
	}
	return "R$ " + b.String()
}

// AssessSwapScenarios calcula o melhor e o pior caso da troca num pool com reserva. nil quando não dá
// para calcular sem chute:
//   - sem cobertura, mesmo SKU, ou reserva abaixo do limite;
//   - Savings Plan relevante (o plano acompanha a troca — qualquer número seria palpite);
//   - moeda que não é BRL, ou sem nodes/preço/câmbio;
//   - troca dentro da mesma série para um tamanho MAIOR (o excedente sobre a reserva depende de
//     detalhes da flexibilidade de tamanho).
//
// Premissas (explícitas na nota): o nº de nodes atual e o pool inteiro trocado.
//   - Outra série: pior caso = a reserva segue paga e ociosa, e TODOS os nodes passam a ser da
//     alternativa sob demanda; o que se recupera é só a parte sob demanda de hoje.
//   - Mesma série menor: a parte reservada não encolhe (paga do mesmo jeito); só a fatia sob demanda
//     de hoje economiza a variação de preço da alternativa.
func AssessSwapScenarios(currentSKU, altSKU string, cov *PoolCoverage, in SwapInputs) *SwapScenarios {
	if cov == nil || strings.EqualFold(strings.TrimSpace(currentSKU), strings.TrimSpace(altSKU)) {
		return nil
	}
	if cov.Reservation < coverageWarnShare || cov.SavingsPlan >= coverageWarnShare {
		return nil
	}
	if cov.Currency != "" && !strings.EqualFold(cov.Currency, "BRL") {
		return nil
	}
	if in.NodeCount <= 0 || in.AltPriceUSDHour <= 0 || in.ExchangeRate <= 0 || cov.WindowDays <= 0 {
		return nil
	}

	// Custo sob demanda que o pool paga hoje, por mês (o custo efetivo é da janela inteira).
	monthFactor := (float64(HoursPerMonth) / 24) / float64(cov.WindowDays)
	odToday := cov.OnDemand * cov.EffectiveCost * monthFactor

	curSeries, altSeries := SeriesKey(currentSKU), SeriesKey(altSKU)
	sameSeries := curSeries != "" && curSeries == altSeries

	sc := &SwapScenarios{BestCaseSavingsBRL: in.TableSavingsBRL}
	best := fmt.Sprintf("Melhor caso: a reserva liberada é aproveitada por outras VMs do mesmo SKU dentro do escopo dela = a economia de tabela (%s/mês).", brlInt(in.TableSavingsBRL))
	assumption := fmt.Sprintf(" Considera %d nodes e o pool inteiro trocado.", in.NodeCount)

	if sameSeries {
		if in.CostDeltaPct >= 0 {
			return nil
		}
		sc.WorstCaseSavingsBRL = odToday * (-in.CostDeltaPct / 100)
		sc.Note = fmt.Sprintf("Pior caso: a parte reservada não encolhe (a reserva é paga do mesmo jeito), então só a fatia sob demanda de hoje (%s/mês) economiza %.0f%% = %s/mês. %s%s",
			brlInt(odToday), -in.CostDeltaPct, brlInt(sc.WorstCaseSavingsBRL), best, assumption)
		return sc
	}

	altAll := in.AltPriceUSDHour * float64(HoursPerMonth) * float64(in.NodeCount) * in.ExchangeRate
	sc.WorstCaseSavingsBRL = -(altAll - odToday)
	sc.Note = fmt.Sprintf("Pior caso: a reserva segue paga e ociosa, e os %d nodes %s saem sob demanda (%s/mês) menos a parte sob demanda de hoje (%s/mês) = %s/mês. %s%s",
		in.NodeCount, altSKU, brlInt(altAll), brlInt(odToday), brlInt(sc.WorstCaseSavingsBRL), best, assumption)
	return sc
}
