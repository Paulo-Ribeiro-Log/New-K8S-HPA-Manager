package finops

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Números reais do calculofrete (Cost Management, 30 dias): R$ 14.325,52 sob reserva e R$ 209,12 sob
// demanda, 51 nodes F4s_v2 a US$ 0,262/h e câmbio 4,99.
func calculofreteCoverage() *PoolCoverage {
	return BuildPoolCoverage(map[string]float64{"reservation": 14325.52, "ondemand": 209.12}, "BRL", 30, time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
}

const calculofreteTableNodeMonth = 0.262 * HoursPerMonth * 4.99 // ~R$ 954/node

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestBuildDeepCoverageCalculofrete(t *testing.T) {
	c := buildDeepCoverage(calculofreteCoverage(), calculofreteTableNodeMonth, 51)
	if c == nil || !c.Computable {
		t.Fatalf("cobertura = %+v", c)
	}
	if !near(c.EffectiveMonthlyBRL, 14736, 5) || !near(c.ReservedMonthlyBRL, 14524, 5) || !near(c.OnDemandMonthlyBRL, 212, 2) {
		t.Errorf("custos mensais: efetivo %.0f, reserva %.0f, sob demanda %.0f", c.EffectiveMonthlyBRL, c.ReservedMonthlyBRL, c.OnDemandMonthlyBRL)
	}
	if !near(c.ReservedNodes, 50.78, 0.05) {
		t.Errorf("nodes reservados = %.2f; quer ~50,78 (51 − 212/954)", c.ReservedNodes)
	}
	if !near(c.TableDiscountPct, 70, 1) || !near(c.ReservedNodeMonthlyBRL, 286, 2) {
		t.Errorf("desconto %.1f%%, node reservado R$ %.0f; quer ~70%% e ~R$ 286", c.TableDiscountPct, c.ReservedNodeMonthlyBRL)
	}
}

func TestBuildDeepCoverageSemNumeros(t *testing.T) {
	sp := BuildPoolCoverage(map[string]float64{"reservation": 600, "savingsplan": 400}, "BRL", 30, time.Now())
	if c := buildDeepCoverage(sp, 900, 10); c.Computable || !strings.Contains(c.Note, "Savings Plan") {
		t.Errorf("Savings Plan relevante não deveria ter cenários: %+v", c)
	}
	usd := BuildPoolCoverage(map[string]float64{"reservation": 1000}, "USD", 30, time.Now())
	if c := buildDeepCoverage(usd, 900, 10); c.Computable || !strings.Contains(c.Note, "USD") {
		t.Errorf("moeda USD: %+v", c)
	}
	spot := BuildPoolCoverage(map[string]float64{"spot": 1000}, "BRL", 30, time.Now())
	if c := buildDeepCoverage(spot, 900, 10); c.Computable || !strings.Contains(c.Note, "Spot") {
		t.Errorf("Spot: %+v", c)
	}
	if buildDeepCoverage(nil, 900, 10) != nil {
		t.Error("sem cobertura deveria ser nil")
	}
}

func TestDeepCoverageScenarios(t *testing.T) {
	c := buildDeepCoverage(calculofreteCoverage(), calculofreteTableNodeMonth, 51)
	f4, _ := DeepSpecFor("Standard_F4s_v2")
	f8, _ := DeepSpecFor("Standard_F8s_v2")
	d4, _ := DeepSpecFor("Standard_D4s_v5")

	// Mesmo SKU, menos nodes: sobra reserva. Pior = reserva inteira paga; melhor = só o usado.
	s := deepCoverageScenario(c, f4, f4, 30, calculofreteTableNodeMonth)
	if s.Basis != "same_sku" || !near(s.IdleReservedNodes, 20.78, 0.05) || !near(s.WorstMonthlyBRL, c.ReservedMonthlyBRL, 1) || !near(s.BestMonthlyBRL, 30*c.ReservedNodeMonthlyBRL, 2) {
		t.Errorf("mesmo SKU, 30 nodes: %+v", s)
	}
	if s.WorstSavingsBRL > 250 || s.BestSavingsBRL < 6000 {
		t.Errorf("economia pior/melhor = %.0f / %.0f", s.WorstSavingsBRL, s.BestSavingsBRL)
	}

	// Mesmo SKU, mais nodes que a reserva: o excedente sai a preço de tabela, nada ocioso.
	s = deepCoverageScenario(c, f4, f4, 61, calculofreteTableNodeMonth)
	if s.IdleReservedNodes != 0 || !near(s.WorstMonthlyBRL, s.BestMonthlyBRL, 1) || !near(s.WorstMonthlyBRL, c.ReservedMonthlyBRL+(61-c.ReservedNodes)*calculofreteTableNodeMonth, 2) {
		t.Errorf("mesmo SKU, 61 nodes: %+v", s)
	}

	// Mesma série, o dobro de vCPU: 26 nodes F8 = 52 unidades F4 — a reserva cobre ~50,8.
	f8Table := 2 * calculofreteTableNodeMonth
	s = deepCoverageScenario(c, f4, f8, 26, f8Table)
	if s.Basis != "same_series" || s.IdleReservedNodes != 0 || !strings.Contains(s.Note, "flexibilidade de tamanho") {
		t.Errorf("mesma série: %+v", s)
	}
	wantExcess := (52 - c.ReservedNodes) / 2 * f8Table
	if !near(s.WorstMonthlyBRL, c.ReservedMonthlyBRL+wantExcess, 2) {
		t.Errorf("mesma série, pior = %.0f; quer %.0f", s.WorstMonthlyBRL, c.ReservedMonthlyBRL+wantExcess)
	}

	// Outra série: pior = reserva ociosa + tudo a preço de tabela; melhor = só a tabela da nova VM.
	d4Table := 0.192 * HoursPerMonth * 4.99
	s = deepCoverageScenario(c, f4, d4, 21, d4Table)
	if s.Basis != "other_series" || !near(s.BestMonthlyBRL, 21*d4Table, 1) || !near(s.WorstMonthlyBRL, c.ReservedMonthlyBRL+21*d4Table, 1) {
		t.Errorf("outra série: %+v", s)
	}
	if s.WorstSavingsBRL > -14000 || s.BestSavingsBRL > 100 {
		t.Errorf("D4s_v5 com a reserva: economia pior %.0f / melhor %.0f — quer prejuízo no pior e ~zero no melhor", s.WorstSavingsBRL, s.BestSavingsBRL)
	}
}

// Fixture com a cobertura real: a "economia" de tabela some e os achados mudam.
func TestBuildPoolDeepAnalysisComReserva(t *testing.T) {
	in := fixtureCalculofrete(true, true)
	// Cobertura proporcional à fixture (10 nodes): ~99% reserva com o mesmo desconto real.
	in.Coverage = BuildPoolCoverage(map[string]float64{"reservation": 14325.52 * 10 / 51, "ondemand": 209.12 * 10 / 51}, "BRL", 30, in.Now)
	in.Prices["standard_f4s_v2"] = DeepPrice{USDHour: 0.262, Source: "api"}
	in.ExchangeRate = 4.99
	in.SKUHints = map[string]*SKUCoverageHint{"standard_d4s_v5": {Kind: HintKindSavingsPlan, Scope: HintScopeSKU, Note: "x"}}

	a := BuildPoolDeepAnalysis(in)
	cov := a.Overview.Coverage
	if cov == nil || !cov.Computable || !near(cov.TableDiscountPct, 70, 1.5) {
		t.Fatalf("cobertura na visão geral: %+v", cov)
	}
	codes := findingCodes(a.Findings)
	if f, ok := codes["pool_reserved"]; !ok || !strings.Contains(f.Title, "custo real") {
		t.Errorf("faltou o achado pool_reserved: %+v", f)
	}
	if f, ok := codes["vm_change_savings"]; ok && !strings.Contains(f.Title, "reserva") {
		t.Errorf("a melhor alternativa deveria considerar a reserva: %+v", f)
	}
	if _, ok := codes["fleet_reserved_skus"]; !ok {
		t.Error("faltou o achado de SKUs cobertos na frota")
	}
	var withCov, prevWorst = 0, -1.0
	for _, s := range a.Simulation {
		if s.Coverage == nil {
			continue
		}
		withCov++
		if s.Feasible && s.Available && s.Coverage.WorstMonthlyBRL < prevWorst {
			t.Errorf("simulação deveria estar ordenada pelo custo efetivo no pior caso (%s)", s.VMSize)
		}
		prevWorst = s.Coverage.WorstMonthlyBRL
		if s.VMSize == "Standard_D4s_v5" && (s.ReservedHint == nil || s.Coverage.Basis != "other_series") {
			t.Errorf("D4s_v5: %+v", s)
		}
	}
	if withCov < 3 {
		t.Errorf("SKUs com preço deveriam ter cenário de reserva: %d", withCov)
	}
}
