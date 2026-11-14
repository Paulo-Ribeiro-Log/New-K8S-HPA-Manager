package finops

import (
	"fmt"
	"strings"
	"testing"
)

func jBase() DeepSimulation {
	return DeepSimulation{VMSize: "atual", IsCurrent: true, Feasible: true, PriceUSDHour: 1, SMT: true,
		NodesRecommended: 8, CostRecommendedBRL: 1000, NodeLossImpactPct: 12.5, NodesEquiv: 8, CostEquivBRL: 1000, AllocCPUMillis: 3800, AllocMemMi: 14000}
}

func jCand(savings float64, nodes int) DeepSimulation {
	return DeepSimulation{VMSize: "nova", Feasible: true, Available: true, CatalogKnown: true, PriceUSDHour: 1, SMT: true,
		VsCurrentOK: true, NodesRecommended: nodes, NodesDeltaRecommended: nodes - 8, SavingsVsCurrentRecBRL: savings,
		CostRecommendedBRL: 1000 - savings, NodeLossImpactPct: 100 / float64(nodes), NodesEquiv: nodes, NodesDeltaEquiv: nodes - 8, SavingsVsCurrentEquivBRL: savings, CostEquivBRL: 1000 - savings, AllocCPUMillis: 3800, AllocMemMi: 14000}
}

func TestJustificativaPorCustoDeTabela(t *testing.T) {
	j := buildJustification(jCand(200, 8), jBase(), DeepDaemonSetOverhead{}, nil, false)
	if j.Verdict != VerdictJustified || len(j.Pros) == 0 {
		t.Fatalf("economia de tabela deveria justificar: %+v", j)
	}
}

func TestJustificativaComReservaPiorEMelhorCaso(t *testing.T) {
	seguro := jCand(100, 8)
	seguro.Coverage = &DeepCoverageScenario{WorstSavingsBRL: 50, BestSavingsBRL: 300}
	if j := buildJustification(seguro, jBase(), DeepDaemonSetOverhead{}, nil, true); j.Verdict != VerdictJustified {
		t.Errorf("economia no pior caso deveria justificar: %+v", j)
	}
	cond := jCand(100, 8)
	cond.Coverage = &DeepCoverageScenario{WorstSavingsBRL: -400, BestSavingsBRL: 300}
	j := buildJustification(cond, jBase(), DeepDaemonSetOverhead{}, nil, true)
	if j.Verdict != VerdictConditional || !strings.Contains(j.Headline, "reserva") {
		t.Errorf("economia só no melhor caso é condicional: %+v", j)
	}
	pior := jCand(100, 8)
	pior.Coverage = &DeepCoverageScenario{WorstSavingsBRL: -400, BestSavingsBRL: -10}
	if j := buildJustification(pior, jBase(), DeepDaemonSetOverhead{}, nil, true); j.Verdict != VerdictNotJustified {
		t.Errorf("mais caro até no melhor caso não se justifica: %+v", j)
	}
}

func TestJustificativaTecnicaSoComGanhoMensuravelECustoLimitado(t *testing.T) {
	ds := DeepDaemonSetOverhead{PerNodeCPUReqMillis: 500, PerNodeMemReqMi: 1024}
	// 5% mais caro, mas 1 node a menos e menor impacto de falha
	ok := jCand(-50, 5)
	ok.NodeLossImpactPct = 10
	j := buildJustification(ok, jBase(), ds, nil, false)
	if j.Verdict != VerdictTechnical || !strings.Contains(strings.Join(j.Pros, " "), "DaemonSets") {
		t.Errorf("ganho técnico com custo +5%% deveria ser technical: %+v", j)
	}
	// 30% mais caro: o ganho não paga
	caro := jCand(-300, 5)
	caro.NodeLossImpactPct = 10
	if j := buildJustification(caro, jBase(), ds, nil, false); j.Verdict != VerdictNotJustified {
		t.Errorf("+30%% de custo não se justifica por ganho técnico: %+v", j)
	}
	// sem ganho nenhum e mais caro
	if j := buildJustification(jCand(-50, 8), jBase(), ds, nil, false); j.Verdict != VerdictNotJustified {
		t.Errorf("mais caro e sem ganho: %+v", j)
	}
}

func TestJustificativaSemSMTComThrottling(t *testing.T) {
	c := jCand(-20, 8)
	c.SMT = false
	ws := []DeepWorkloadRow{{HasThrottle: true, ThrottleP95Pct: deepThrottleWarnPct + 5}}
	j := buildJustification(c, jBase(), DeepDaemonSetOverhead{}, ws, false)
	if j.Verdict != VerdictTechnical || !strings.Contains(strings.Join(j.Pros, " "), "throttling") {
		t.Errorf("sem SMT com throttling é ganho para os workloads: %+v", j)
	}
	if j2 := buildJustification(jCand(-20, 8), jBase(), DeepDaemonSetOverhead{}, ws, false); j2.Verdict == VerdictTechnical {
		t.Errorf("sem trocar de SMT não há ganho de throttling: %+v", j2)
	}
}

func TestJustificativaBloqueios(t *testing.T) {
	ind := jCand(500, 8)
	ind.Available = false
	if j := buildJustification(ind, jBase(), DeepDaemonSetOverhead{}, nil, false); j.Verdict != VerdictNotJustified {
		t.Errorf("indisponível: %+v", j)
	}
	sem := jCand(500, 8)
	sem.VsCurrentOK = false
	if j := buildJustification(sem, jBase(), DeepDaemonSetOverhead{}, nil, false); j.Verdict != VerdictUnknown {
		t.Errorf("sem base de comparação: %+v", j)
	}
	if j := buildJustification(jBase(), jBase(), DeepDaemonSetOverhead{}, nil, false); j.Verdict != VerdictBase {
		t.Errorf("a atual é a base: %+v", j)
	}
	// poucos nodes grandes: economia com ressalva
	few := jCand(500, 4)
	few.NodeLossImpactPct = 25
	if j := buildJustification(few, jBase(), DeepDaemonSetOverhead{}, nil, false); j.Verdict != VerdictConditional {
		t.Errorf("economia concentrando risco deveria ter ressalva: %+v", j)
	}
}

func TestMarkdownListaSoAsSKUsJustificadas(t *testing.T) {
	ok := jCand(200, 8)
	ruim := jCand(-300, 8)
	sims := []DeepSimulation{jBase(), ok, ruim}
	fillVsCurrent(sims)
	fillJustifications(sims, DeepDaemonSetOverhead{}, nil, false)
	var b strings.Builder
	writeJustifications(func(f string, a ...interface{}) { b.WriteString(fmt.Sprintf(f, a...)) }, sims)
	out := b.String()
	if !strings.Contains(out, "justificada") || !strings.Contains(out, "1 SKU(s) sem justificativa") {
		t.Errorf("markdown inesperado:\n%s", out)
	}
	if strings.Count(out, "**`nova`**") != 1 {
		t.Errorf("só a justificada deveria ser listada:\n%s", out)
	}
}

func TestNodesCalcMostraCadaRecursoEOLimitePorNode(t *testing.T) {
	// 110 pods por node, 2 de DaemonSet → 108 vagas. 500 pods exigem 5 nodes por pods; CPU e memória exigem menos.
	ds := DeepDaemonSetOverhead{PerNodeCPUReqMillis: 200, PerNodeMemReqMi: 500, PerNodePods: 2}
	d := deepDemand{cpuMillis: 10000, memMi: 20000, pods: 500}
	n, lim, c := nodesCalc(d, 4000, 16000, 110, ds, 1.0, 3)
	if n != 5 || lim != "pods" || c.ByPods != 5 || c.ByCPU != 3 || c.ByMem != 2 {
		t.Errorf("n=%d lim=%s calc=%+v", n, lim, c)
	}
	// a conta antiga continua igual
	if n2, lim2 := nodesFor(d, 4000, 16000, 110, ds, 1.0, 3); n2 != n || lim2 != lim {
		t.Errorf("nodesFor divergiu de nodesCalc: %d %s", n2, lim2)
	}
	// o mínimo de nodes domina quando a demanda é pequena
	if n3, lim3, _ := nodesCalc(deepDemand{cpuMillis: 100, memMi: 100, pods: 1}, 4000, 16000, 110, ds, 1.0, 3); n3 != 3 || lim3 != "min_nodes" {
		t.Errorf("mínimo: %d %s", n3, lim3)
	}
}

// Regressão: a VM atual usa o alocável MEDIDO e as candidatas o ESTIMADO. Uma candidata da mesma forma e do mesmo preço
// que cai de 14 para 13 nodes só por essa diferença não é economia: a comparação usa a atual no alocável estimado.
func TestComparacaoUsaAVMAtualNoMesmoAlocavelDasCandidatas(t *testing.T) {
	cur := jBase()
	cur.NodesCurrentReq, cur.CostCurrentReqBRL = 41, 41*794
	cur.NodesRecommended, cur.CostRecommendedBRL = 14, 14*794 // medido
	cur.BaselineNodesCurrentReq, cur.BaselineCostCurrentReqBRL = 41, 41*794
	cur.BaselineNodesRecommended, cur.BaselineCostRecommendedBRL = 13, 13*794 // estimado, como a candidata
	same := jCand(0, 13)
	same.NodesCurrentReq, same.CostCurrentReqBRL = 41, 41*794
	same.CostRecommendedBRL = 13 * 794
	sims := []DeepSimulation{cur, same}
	fillVsCurrent(sims)
	s := sims[1]
	if s.NodesDeltaRecommended != 0 || s.SavingsVsCurrentRecBRL != 0 || s.NodesDeltaCurrentReq != 0 {
		t.Fatalf("mesma forma e preço não pode render economia: %+v", s)
	}
	j := buildJustification(s, sims[0], DeepDaemonSetOverhead{}, nil, false)
	if j.Verdict != VerdictNotJustified {
		t.Errorf("sem economia nem ganho a troca não se justifica: %+v", j)
	}
	// sem a base (ex.: sem preço), cai no valor medido, como antes
	cur.BaselineNodesRecommended, cur.BaselineCostRecommendedBRL = 0, 0
	if n, c := baselineRecommended(&cur); n != 14 || c != 14*794 {
		t.Errorf("sem base deveria usar o medido: %d %v", n, c)
	}
}

func TestPluralDeNodesNaJustificativa(t *testing.T) {
	if nodesWord(1) != "1 node" || nodesWord(3) != "3 nodes" {
		t.Errorf("plural: %q %q", nodesWord(1), nodesWord(3))
	}
}

func TestComparacaoComOCustoRealDoPoolHoje(t *testing.T) {
	sims := []DeepSimulation{
		{VMSize: "atual", IsCurrent: true, Feasible: true, PriceUSDHour: 1, CostCurrentReqBRL: 30000, CostRecommendedBRL: 20000},
		{VMSize: "nova", Feasible: true, PriceUSDHour: 1, CostCurrentReqBRL: 24000, CostRecommendedBRL: 15000},
		{VMSize: "semPreco", Feasible: true, CostRecommendedBRL: 1},
	}
	fillVsToday(sims, 40000) // o pool de hoje custa R$ 40.000/mês de tabela
	n := sims[1]
	if !n.VsTodayOK || n.DeltaTodayRecBRL != -25000 || n.DeltaTodayRecPct != -62.5 || n.DeltaTodayCurrentReqBRL != -16000 || n.DeltaTodayCurrentReqPct != -40 {
		t.Errorf("nova: %+v", n)
	}
	if !sims[0].VsTodayOK || sims[0].DeltaTodayRecBRL != -20000 {
		t.Errorf("a atual também se compara com hoje (efeito de ocupação/requests): %+v", sims[0])
	}
	if sims[2].VsTodayOK {
		t.Errorf("sem preço não compara")
	}
	none := []DeepSimulation{{Feasible: true, PriceUSDHour: 1, CostRecommendedBRL: 5}}
	fillVsToday(none, 0)
	if none[0].VsTodayOK {
		t.Errorf("sem custo de hoje não há base")
	}
}

func TestMarkdownColunaPoolHojeXComEstaVM(t *testing.T) {
	today := DeepPoolToday{VMSize: "atual", Nodes: 40, MonthlyBRL: 40000, UnitBRL: 1000}
	s := DeepSimulation{Feasible: true, PriceUSDHour: 1, VsTodayOK: true, NodesRecommended: 12, CostRecommendedBRL: 12000,
		NodesEquiv: 15, CostEquivBRL: 15000, DeltaTodayEquivBRL: -25000, DeltaTodayEquivPct: -62.5,
		DeltaTodayRecBRL: -28000, DeltaTodayRecPct: -70, DeltaTodayCurrentReqBRL: -16000, DeltaTodayCurrentReqPct: -40}
	out := mdToday(s, today, nil)
	for _, want := range []string{"hoje R$", "(40 × R$", "com esta VM R$", "(15 × R$", "−R$", "-62.5%", "reduzindo requests"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em %q", want, out)
		}
	}
	if mdToday(DeepSimulation{Feasible: true}, today, nil) != "—" {
		t.Errorf("sem comparação deveria ser traço")
	}
	if got := mdDeltaCell(0.2, 0); got != "igual" {
		t.Errorf("diferença desprezível: %q", got)
	}
	if got := mdDeltaCell(500, 1.25); !strings.HasPrefix(got, "+R$ ") {
		t.Errorf("mais caro leva +: %q", got)
	}
}

func TestMdTodayMostraTabelaEAmortizadoJuntos(t *testing.T) {
	today := DeepPoolToday{Nodes: 10, MonthlyBRL: 40000, UnitBRL: 4000}
	cov := &DeepCoverage{EffectiveMonthlyBRL: 15000, ReservedMonthlyBRL: 14000, OnDemandMonthlyBRL: 1000}
	s := DeepSimulation{Feasible: true, PriceUSDHour: 1, VsTodayOK: true, NodesRecommended: 6, CostRecommendedBRL: 24000,
		DeltaTodayRecBRL: -16000, DeltaTodayRecPct: -40,
		Coverage: &DeepCoverageScenario{WorstMonthlyBRL: 16000, BestMonthlyBRL: 9000, WorstSavingsBRL: -1000, BestSavingsBRL: 6000, WorstDeltaPct: 6.7, BestDeltaPct: -40}}
	out := mdToday(s, today, cov)
	for _, want := range []string{"tabela (sob demanda)", "amortizado:", "hoje R$ ", "pior R$ ", "melhor R$ "} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em %q", want, out)
		}
	}
	// sem cenário amortizado (ex.: Savings Plan): ainda mostra o amortizado de hoje e avisa
	s.Coverage = nil
	if out := mdToday(s, today, cov); !strings.Contains(out, "sem cenário amortizado") || !strings.Contains(out, "amortizado:") {
		t.Errorf("sem cenário: %q", out)
	}
	// sem cobertura: nada de amortizado
	if out := mdToday(s, today, nil); strings.Contains(out, "amortizado") {
		t.Errorf("sem cobertura não há amortizado: %q", out)
	}
	var b strings.Builder
	writeAmortized(func(f string, a ...interface{}) { b.WriteString(fmt.Sprintf(f, a...)) },
		&DeepCoverage{EffectiveMonthlyBRL: 15000, ReservedMonthlyBRL: 14000, OnDemandMonthlyBRL: 1000, WindowDays: 30, TableMonthlyBRL: 40000, AmortizedVsTablePct: -62.5, AmortizedNodeBRL: 1500, TableNodeBRL: 4000}, 10)
	if got := b.String(); !strings.Contains(got, "R$ 15.000,00/mês") && !strings.Contains(got, "/mês**") {
		t.Errorf("painel amortizado: %q", got)
	}
	if got := b.String(); !strings.Contains(got, "reserva") || !strings.Contains(got, "baixa o custo em 62%") {
		t.Errorf("composição e desconto: %q", got)
	}
}

// O caso do usuário: 48 nodes de 3,9 cores e 13,3 GiB. O que roda dentro dos nodes não importa; a pergunta é quantos
// nodes de outra VM entregam a mesma capacidade.
func TestMesmaCapacidadeDeHojeEm48Nodes(t *testing.T) {
	totCPU, totMem := 48*3860.0, 48*13.3*1024
	// E8s_v5: 7,8 cores e 60,3 GiB → CPU 187,2 ÷ 7,8 = 24 nodes; memória 638 ÷ 60,3 ≈ 10,6 → 11
	n, lim, c := equivNodes(totCPU, totMem, 700, 7820, 60.3*1024, 110, 3)
	if n != 24 || lim != "cpu" || c.ByCPU != 24 || c.ByMem != 11 || c.ByPods != 7 {
		t.Errorf("E8s_v5: n=%d lim=%s calc=%+v", n, lim, c)
	}
	// a própria forma da VM atual devolve os mesmos 48 (a tolerância cobre a estimativa do alocável)
	if n, _, _ := equivNodes(totCPU, totMem, 700, 3860, 13.3*1024, 110, 3); n != 48 {
		t.Errorf("mesma forma deveria dar 48, deu %d", n)
	}
	// estimativa 1% menor que o medido não custa um node a mais; 5% menor custa
	if n, _, _ := equivNodes(totCPU, totMem, 700, 3860*0.99, 13.3*1024, 110, 3); n != 48 {
		t.Errorf("1%% de diferença: %d", n)
	}
	if n, _, _ := equivNodes(totCPU, totMem, 700, 3860*0.95, 13.3*1024, 110, 3); n <= 48 {
		t.Errorf("5%% de diferença deveria exigir mais nodes: %d", n)
	}
	// o limite de pods por node pode mandar: 6.000 pods ÷ 110 = 55 nodes
	if n, lim, _ := equivNodes(totCPU, totMem, 6000, 7820, 60.3*1024, 110, 3); n != 55 || lim != "pods" {
		t.Errorf("limite de pods: %d %s", n, lim)
	}
	// o mínimo de nodes vale quando a capacidade pede menos
	if n, lim, _ := equivNodes(1000, 1000, 5, 7820, 60.3*1024, 110, 3); n != 3 || lim != "min_nodes" {
		t.Errorf("mínimo: %d %s", n, lim)
	}
	if n, _, _ := equivNodes(totCPU, totMem, 700, 0, 0, 110, 3); n != 0 {
		t.Errorf("VM sem alocável não comporta: %d", n)
	}
}

func TestSimulacaoUsaACapacidadeDeHojeComoBase(t *testing.T) {
	a := BuildPoolDeepAnalysis(fixtureCalculofrete(true, true))
	c := a.SimulationInputs.Capacity
	if c.Nodes != a.Overview.Nodes || c.TotalCPU <= 0 || c.TotalMemMi <= 0 || c.PodsRunning <= 0 {
		t.Fatalf("capacidade de hoje: %+v", c)
	}
	for _, s := range a.Simulation {
		if !s.Feasible {
			continue
		}
		if s.IsCurrent {
			if s.NodesEquiv != a.Overview.Nodes {
				t.Errorf("a VM atual na mesma capacidade são os nodes de hoje: %d vs %d", s.NodesEquiv, a.Overview.Nodes)
			}
			continue
		}
		// capacidade entregue pela VM nova nunca fica abaixo da de hoje (menos a tolerância)
		if float64(s.NodesEquiv)*s.AllocCPUMillis*equivTolerance < c.TotalCPU*0.999 {
			t.Errorf("%s: %d nodes não cobrem a CPU de hoje", s.VMSize, s.NodesEquiv)
		}
		if float64(s.NodesEquiv)*s.AllocMemMi*equivTolerance < c.TotalMemMi*0.999 {
			t.Errorf("%s: %d nodes não cobrem a memória de hoje", s.VMSize, s.NodesEquiv)
		}
	}
}
