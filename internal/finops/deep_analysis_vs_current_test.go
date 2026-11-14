package finops

import "testing"

func TestFillVsCurrentComparaComAVMAtual(t *testing.T) {
	sims := []DeepSimulation{
		{VMSize: "atual", IsCurrent: true, Feasible: true, PriceUSDHour: 1, NodesCurrentReq: 10, NodesRecommended: 6, CostCurrentReqBRL: 1000, CostRecommendedBRL: 600},
		{VMSize: "menor", Feasible: true, PriceUSDHour: 1, NodesCurrentReq: 8, NodesRecommended: 5, CostCurrentReqBRL: 800, CostRecommendedBRL: 500},
		{VMSize: "maior", Feasible: true, PriceUSDHour: 1, NodesCurrentReq: 12, NodesRecommended: 7, CostCurrentReqBRL: 1200, CostRecommendedBRL: 700},
		{VMSize: "semPreco", Feasible: true, NodesCurrentReq: 3},
		{VMSize: "naoCabe", Feasible: false, PriceUSDHour: 1},
	}
	fillVsCurrent(sims)

	if sims[0].VsCurrentOK {
		t.Errorf("a VM atual é a base, não se compara com ela mesma")
	}
	m := sims[1]
	if !m.VsCurrentOK || m.NodesDeltaCurrentReq != -2 || m.NodesDeltaRecommended != -1 || m.SavingsVsCurrentReqBRL != 200 || m.SavingsVsCurrentRecBRL != 100 {
		t.Errorf("menor: %+v", m)
	}
	g := sims[2]
	if g.SavingsVsCurrentRecBRL != -100 || g.NodesDeltaRecommended != 1 {
		t.Errorf("maior deveria custar mais que a atual: %+v", g)
	}
	if sims[3].VsCurrentOK || sims[4].VsCurrentOK {
		t.Errorf("sem preço ou que não comporta não tem comparação")
	}
}

func TestFillVsCurrentSemBaseNaoFazNada(t *testing.T) {
	sims := []DeepSimulation{{VMSize: "x", Feasible: true, PriceUSDHour: 1, CostRecommendedBRL: 10}}
	fillVsCurrent(sims)
	if sims[0].VsCurrentOK {
		t.Errorf("sem VM atual viável não há base")
	}
	cur := []DeepSimulation{{IsCurrent: true, Feasible: false}, {Feasible: true, PriceUSDHour: 1}}
	fillVsCurrent(cur)
	if cur[1].VsCurrentOK {
		t.Errorf("atual que não comporta a demanda não serve de base")
	}
}

func TestOcupacaoPadraoDaSimulacaoE100(t *testing.T) {
	if DeepDefaultHeadroom != 1.0 {
		t.Errorf("a simulação de troca de VM usa 100%% de ocupação, veio %v", DeepDefaultHeadroom)
	}
}
