package promwal

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func node(name, pool string, labels map[string]string, taints ...corev1.Taint) corev1.Node {
	l := map[string]string{"agentpool": pool}
	for k, v := range labels {
		l[k] = v
	}
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: l}, Spec: corev1.NodeSpec{Taints: taints}}
}

var (
	taintCritical = corev1.Taint{Key: "CriticalAddonsOnly", Value: "true", Effect: corev1.TaintEffectNoSchedule}
	taintSpot     = corev1.Taint{Key: "kubernetes.azure.com/scalesetpriority", Value: "spot", Effect: corev1.TaintEffectNoSchedule}
	taintProm     = corev1.Taint{Key: "prometheus-only", Value: "true", Effect: corev1.TaintEffectNoSchedule}
)

// O pod e os nodes do cluster de homologação analisado (saída do kubectl colada pelo usuário).
func realPod() PodInfo {
	return PodInfo{
		Name: "prometheus-prometheus-prometheus-0", Phase: "Pending", WaitingReason: "Pending", SchedulingMessage: realSchedMsg,
		NodeSelector: map[string]string{"app": "abastecimento"},
		Tolerations: []corev1.Toleration{
			{Key: "kubernetes.azure.com/scalesetpriority", Operator: corev1.TolerationOpEqual, Value: "spot", Effect: corev1.TaintEffectNoSchedule},
			{Key: "prometheus-only", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule},
			{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
		},
	}
}

func realNodes() []corev1.Node {
	return []corev1.Node{
		node("aks-abastecimsys-39084123-vmss00009u", "abastecimsys", nil, taintCritical),
		node("aks-adanalytspot-32007304-vmss0000zg", "adanalytspot", nil, taintSpot),
		node("aks-adanalytspot-32007304-vmss0000zh", "adanalytspot", nil, taintSpot),
	}
}

func fitOf(pl *Placement, name string) NodeFit {
	for _, n := range pl.Nodes {
		if n.Node == name {
			return n
		}
	}
	return NodeFit{}
}

func TestPlacementCenarioReal(t *testing.T) {
	pl := diagnosePlacement(realPod(), realNodes(), "")
	if len(pl.Nodes) != 3 {
		t.Fatalf("esperava 3 nodes: %+v", pl.Nodes)
	}
	for _, n := range pl.Nodes {
		if n.Fits {
			t.Errorf("nenhum node deveria servir: %+v", n)
		}
	}
	sys := fitOf(pl, "aks-abastecimsys-39084123-vmss00009u")
	if sys.Pool != "abastecimsys" || !strings.Contains(strings.Join(sys.Reasons, "|"), "falta o label app=abastecimento") ||
		!strings.Contains(strings.Join(sys.Reasons, "|"), "taint não tolerado: CriticalAddonsOnly=true:NoSchedule") {
		t.Errorf("node de sistema: %+v", sys)
	}
	spot := fitOf(pl, "aks-adanalytspot-32007304-vmss0000zg")
	if len(spot.Reasons) != 1 || spot.Reasons[0] != "falta o label app=abastecimento" {
		t.Errorf("node spot só falha no label (o taint spot é tolerado): %+v", spot)
	}
	for _, want := range []string{"Nenhum dos 3 node(s) atende a nodeSelector (app=abastecimento)", "0 de 3 têm app=abastecimento", "defina-o no node pool"} {
		if !strings.Contains(pl.Summary, want) {
			t.Errorf("resumo sem %q:\n%s", want, pl.Summary)
		}
	}
	if pl.Note == "" {
		t.Error("deveria trazer a nota dos limites do diagnóstico")
	}
}

func TestPlacementVariantes(t *testing.T) {
	t.Run("label com valor diferente", func(t *testing.T) {
		nodes := append(realNodes()[:2:2], node("n-x", "outro", map[string]string{"app": "financeiro"}))
		pl := diagnosePlacement(realPod(), nodes, "")
		if !strings.Contains(pl.Summary, "com outro valor: n-x (app=financeiro)") {
			t.Errorf("deveria apontar o node com outro valor: %s", pl.Summary)
		}
		if r := fitOf(pl, "n-x").Reasons; len(r) != 1 || !strings.Contains(r[0], "label app=financeiro (o pod exige app=abastecimento)") {
			t.Errorf("motivo do node com outro valor: %v", r)
		}
	})

	t.Run("node com o label e tolerável serve: o bloqueio é outro", func(t *testing.T) {
		nodes := append(realNodes(), node("n-ok", "adanalytspot", map[string]string{"app": "abastecimento"}, taintSpot, taintProm))
		pl := diagnosePlacement(realPod(), nodes, "")
		if !fitOf(pl, "n-ok").Fits || !strings.Contains(pl.Summary, "1 node(s) passam") || !strings.Contains(pl.Summary, "n-ok") {
			t.Errorf("n-ok deveria servir e o resumo apontar outro bloqueio: %s", pl.Summary)
		}
	})

	t.Run("o único node do seletor tem taint não tolerado", func(t *testing.T) {
		nodes := []corev1.Node{
			node("n-label", "sys", map[string]string{"app": "abastecimento"}, taintCritical),
			node("n-sem", "spot", nil, taintSpot),
		}
		pl := diagnosePlacement(realPod(), nodes, "")
		if !strings.Contains(pl.Summary, "Só 1 node(s) atendem ao seletor, mas estão bloqueados") || !strings.Contains(pl.Summary, "n-label (taint não tolerado: CriticalAddonsOnly=true:NoSchedule)") {
			t.Errorf("resumo: %s", pl.Summary)
		}
	})

	t.Run("node cordonado", func(t *testing.T) {
		n := node("n-cord", "p", map[string]string{"app": "abastecimento"})
		n.Spec.Unschedulable = true
		pl := diagnosePlacement(realPod(), []corev1.Node{n}, "")
		if r := fitOf(pl, "n-cord").Reasons; len(r) != 1 || !strings.Contains(r[0], "cordonado") {
			t.Errorf("motivo: %v", r)
		}
	})

	t.Run("sem permissão para listar nodes", func(t *testing.T) {
		pl := diagnosePlacement(realPod(), nil, "sem permissão ao listar os nodes")
		if pl.Unavailable == "" || len(pl.Nodes) != 0 || !strings.Contains(pl.Summary, "Não foi possível diagnosticar por node") {
			t.Errorf("%+v", pl)
		}
	})
}

func TestPlacementNodeAffinity(t *testing.T) {
	term := func(exprs ...corev1.NodeSelectorRequirement) corev1.NodeSelectorTerm {
		return corev1.NodeSelectorTerm{MatchExpressions: exprs}
	}
	pod := PodInfo{Name: "p", NodeAffinity: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
		term(corev1.NodeSelectorRequirement{Key: "pool", Operator: corev1.NodeSelectorOpIn, Values: []string{"a", "b"}}),
		term(corev1.NodeSelectorRequirement{Key: "gpu", Operator: corev1.NodeSelectorOpExists}),
	}}}
	nodes := []corev1.Node{
		node("n-a", "x", map[string]string{"pool": "a"}),     // casa o 1º termo
		node("n-gpu", "x", map[string]string{"gpu": "true"}), // casa o 2º termo (OU)
		node("n-no", "x", map[string]string{"pool": "c"}),    // não casa nenhum
	}
	pl := diagnosePlacement(pod, nodes, "")
	if !fitOf(pl, "n-a").Fits || !fitOf(pl, "n-gpu").Fits || fitOf(pl, "n-no").Fits {
		t.Fatalf("termos de nodeAffinity são OU: %+v", pl.Nodes)
	}
	if r := fitOf(pl, "n-no").Reasons; len(r) != 1 || !strings.Contains(r[0], "nodeAffinity: pool In [a, b]") {
		t.Errorf("motivo do node que não casa (termo mais próximo): %v", r)
	}

	gt := PodInfo{NodeAffinity: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
		term(corev1.NodeSelectorRequirement{Key: "cpu", Operator: corev1.NodeSelectorOpGt, Values: []string{"4"}})}}}
	pl = diagnosePlacement(gt, []corev1.Node{node("big", "x", map[string]string{"cpu": "8"}), node("small", "x", map[string]string{"cpu": "2"})}, "")
	if !fitOf(pl, "big").Fits || fitOf(pl, "small").Fits {
		t.Errorf("Gt: %+v", pl.Nodes)
	}
	notIn := PodInfo{NodeAffinity: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
		term(corev1.NodeSelectorRequirement{Key: "tier", Operator: corev1.NodeSelectorOpNotIn, Values: []string{"spot"}})}}}
	pl = diagnosePlacement(notIn, []corev1.Node{node("sem", "x", nil), node("spot", "x", map[string]string{"tier": "spot"})}, "")
	if !fitOf(pl, "sem").Fits || fitOf(pl, "spot").Fits {
		t.Errorf("NotIn deve aceitar node sem o label: %+v", pl.Nodes)
	}
}

func TestTolerates(t *testing.T) {
	taint := corev1.Taint{Key: "k", Value: "v", Effect: corev1.TaintEffectNoSchedule}
	cases := map[string]struct {
		tol  corev1.Toleration
		want bool
	}{
		"Equal casa":                   {corev1.Toleration{Key: "k", Operator: corev1.TolerationOpEqual, Value: "v", Effect: corev1.TaintEffectNoSchedule}, true},
		"Equal com valor diferente":    {corev1.Toleration{Key: "k", Operator: corev1.TolerationOpEqual, Value: "x"}, false},
		"operador vazio é Equal":       {corev1.Toleration{Key: "k", Value: "v"}, true},
		"Exists sem valor":             {corev1.Toleration{Key: "k", Operator: corev1.TolerationOpExists}, true},
		"Exists sem chave tolera tudo": {corev1.Toleration{Operator: corev1.TolerationOpExists}, true},
		"efeito diferente":             {corev1.Toleration{Key: "k", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute}, false},
		"chave diferente":              {corev1.Toleration{Key: "z", Operator: corev1.TolerationOpExists}, false},
	}
	for name, c := range cases {
		if got := tolerates([]corev1.Toleration{c.tol}, taint); got != c.want {
			t.Errorf("%s: tolerates = %v, esperado %v", name, got, c.want)
		}
	}
	// PreferNoSchedule nunca bloqueia.
	n := node("n", "p", nil, corev1.Taint{Key: "soft", Effect: corev1.TaintEffectPreferNoSchedule})
	if !evalNode(PodInfo{}, &n).fit.Fits {
		t.Error("PreferNoSchedule não impede o agendamento")
	}
}

func TestAnaliseAnexaPlacementDoPodNaoAgendado(t *testing.T) {
	in := Input{Pods: []PodInfo{realPod()}, Nodes: realNodes(), DeepSkipped: []string{"prometheus-prometheus-prometheus-0"}}
	r := Analyze(in, testNow)
	pod := r.Pods[0]
	if pod.Verdict != VerdictInvestigate || pod.Placement == nil || !strings.Contains(pod.Placement.Summary, "0 de 3 têm app=abastecimento") {
		t.Fatalf("o pod não agendado deveria trazer o diagnóstico por node: %+v", pod)
	}
	// Com o diagnóstico por node, a dica genérica de placement sai (seria redundante).
	for _, e := range pod.Evidence {
		if strings.Contains(e, "tolerations, nodeSelector e affinity") {
			t.Errorf("dica genérica não deveria aparecer junto do diagnóstico por node: %s", e)
		}
	}
	// Sem a lista de nodes (sem permissão), cai na dica genérica e explica por quê.
	in.Nodes, in.NodesError = nil, "sem permissão ao listar os nodes"
	pod = Analyze(in, testNow).Pods[0]
	if pod.Placement == nil || pod.Placement.Unavailable == "" {
		t.Errorf("deveria informar que não foi possível diagnosticar: %+v", pod.Placement)
	}
	// Pod saudável ou crashando não tem diagnóstico de agendamento.
	if h := Analyze(Input{Pods: []PodInfo{{Name: "p", Ready: true}}}, testNow).Pods[0]; h.Placement != nil {
		t.Error("pod Ready não tem placement")
	}
}

func TestCollectLeNodesSoComPodNaoAgendado(t *testing.T) {
	pending := crashPod("prometheus-prom-0", "")
	pending.Spec.NodeSelector = map[string]string{"app": "abastecimento"}
	pending.Spec.Tolerations = []corev1.Toleration{{Key: "prometheus-only", Operator: corev1.TolerationOpExists}}
	pending.Status = corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: realSchedMsg}}}
	n := realNodes()[0]
	cs := fake.NewSimpleClientset(pending, &n)
	c := &Collector{Client: cs, Run: fakeRunner(new([]string))}

	in, err := c.Collect(context.Background(), "ctx", "monitoring", "prom")
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Nodes) != 1 || in.Pods[0].NodeSelector["app"] != "abastecimento" || len(in.Pods[0].Tolerations) != 1 {
		t.Fatalf("deveria ler nodes e as restrições do pod: nodes=%d pod=%+v", len(in.Nodes), in.Pods[0])
	}

	// Pod agendado (crashando): não lista nodes (leitura desnecessária, e exige permissão a mais).
	listed := false
	cs2 := fake.NewSimpleClientset(crashPod("prometheus-prom-0", "node-a"))
	cs2.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) { listed = true; return false, nil, nil })
	in, _ = (&Collector{Client: cs2, Run: fakeRunner(new([]string))}).Collect(context.Background(), "ctx", "monitoring", "prom")
	if listed || len(in.Nodes) != 0 {
		t.Error("não deveria listar nodes quando todos os pods foram agendados")
	}

	// Sem permissão para listar nodes: não derruba a análise.
	cs3 := fake.NewSimpleClientset(pending)
	cs3.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("nodes is forbidden")
	})
	in, err = (&Collector{Client: cs3, Run: fakeRunner(new([]string))}).Collect(context.Background(), "ctx", "monitoring", "prom")
	if err != nil || in.NodesError == "" || !strings.Contains(in.NodesError, "forbidden") {
		t.Fatalf("falha ao listar nodes deveria virar NodesError sem erro de coleta: err=%v nodesErr=%q", err, in.NodesError)
	}
}
