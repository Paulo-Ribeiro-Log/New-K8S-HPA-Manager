package finops

import (
	"fmt"
	"strings"
	"testing"
)

func jBase() DeepSimulation {
	return DeepSimulation{VMSize: "atual", IsCurrent: true, Feasible: true, PriceUSDHour: 1, SMT: true,
		NodesRecommended: 8, CostRecommendedBRL: 1000, NodeLossImpactPct: 12.5, AllocCPUMillis: 3800, AllocMemMi: 14000}
}

func jCand(savings float64, nodes int) DeepSimulation {
	return DeepSimulation{VMSize: "nova", Feasible: true, Available: true, CatalogKnown: true, PriceUSDHour: 1, SMT: true,
		VsCurrentOK: true, NodesRecommended: nodes, NodesDeltaRecommended: nodes - 8, SavingsVsCurrentRecBRL: savings,
		CostRecommendedBRL: 1000 - savings, NodeLossImpactPct: 100 / float64(nodes), AllocCPUMillis: 3800, AllocMemMi: 14000}
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
