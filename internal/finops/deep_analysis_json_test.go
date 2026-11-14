package finops

import (
	"encoding/json"
	"testing"
)

// A resposta da Deep Analysis precisa virar JSON em qualquer entrada: NaN/Inf quebram o json.Marshal e o handler
// devolve erro (o modal fica sem a análise). Cobre dados completos e os casos vazios/degenerados.
func TestDeepAnalysisSempreViraJSON(t *testing.T) {
	cases := map[string]func() DeepAnalysisInput{
		"completo":                   func() DeepAnalysisInput { return fixtureCalculofrete(true, true) },
		"sem métricas nem histórico": func() DeepAnalysisInput { return fixtureCalculofrete(false, false) },
		"sem workloads": func() DeepAnalysisInput {
			in := fixtureCalculofrete(true, true)
			in.Pods = nil
			return in
		},
		"sem preços": func() DeepAnalysisInput {
			in := fixtureCalculofrete(true, true)
			in.Prices = map[string]DeepPrice{}
			return in
		},
		"alocável zerado": func() DeepAnalysisInput {
			in := fixtureCalculofrete(true, true)
			for i := range in.Nodes {
				in.Nodes[i].CPUAllocMillis, in.Nodes[i].MemAllocMi = 0, 0
			}
			return in
		},
		"sem nodes": func() DeepAnalysisInput {
			in := fixtureCalculofrete(true, true)
			in.Nodes = nil
			return in
		},
		"com reserva": func() DeepAnalysisInput {
			in := fixtureCalculofrete(true, true)
			in.Coverage = BuildPoolCoverage(map[string]float64{"reservation": 2809, "ondemand": 41}, "BRL", 30, in.Now)
			return in
		},
		"ocupação 0,3": func() DeepAnalysisInput {
			in := fixtureCalculofrete(true, true)
			in.Headroom = 0.3
			return in
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("pânico: %v", r)
				}
			}()
			a := BuildPoolDeepAnalysis(mk())
			if _, err := json.Marshal(a); err != nil {
				t.Fatalf("a análise não vira JSON: %v", err)
			}
			_ = RenderDeepAnalysisMarkdown(a)
		})
	}
}

// A coluna "pool hoje × com esta VM" depende de simulation_inputs.pool_today: sem ele a tela mostra "—" em toda VM.
func TestAnaliseCompletaTrazOPoolDeHojeEAsComparacoes(t *testing.T) {
	a := BuildPoolDeepAnalysis(fixtureCalculofrete(true, true))
	pt := a.SimulationInputs.PoolToday
	if pt.Nodes <= 0 || pt.MonthlyBRL <= 0 || pt.UnitBRL <= 0 || pt.VMSize == "" {
		t.Fatalf("pool de hoje vazio: %+v", pt)
	}
	if pt.MonthlyBRL != a.Overview.MonthlyCostBRL || pt.Nodes != a.Overview.Nodes {
		t.Errorf("pool de hoje diverge do overview: %+v vs %v/%d", pt, a.Overview.MonthlyCostBRL, a.Overview.Nodes)
	}
	comValores := 0
	for _, s := range a.Simulation {
		if s.PriceUSDHour > 0 && s.Feasible {
			if !s.VsTodayOK || s.Justification == nil || s.Fit == nil {
				t.Errorf("%s tem preço e comporta, mas falta comparação/justificativa/encaixe: %+v", s.VMSize, s)
			}
			comValores++
		}
	}
	if comValores == 0 {
		t.Fatal("nenhuma SKU com valores na fixture")
	}
	inv := a.SimulationInputs.Inventory
	if inv.Nodes <= 0 || len(inv.Kinds) == 0 {
		t.Errorf("inventário vazio: %+v", inv)
	}
}
