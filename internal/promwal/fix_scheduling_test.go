package promwal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Cenário real: nodeSelector app=monitoring e nenhum node com esse label; o pool cshubsystem tem o taint
// CriticalAddonsOnly. As opções são verificadas: cada uma deixa ao menos um node servindo ao pod.
func monitoringPod() PodInfo {
	return PodInfo{Name: "prometheus-prometheus-prometheus-0", Phase: "Pending", NodeSelector: map[string]string{"app": "monitoring"}}
}

func monitoringNodes() []corev1.Node {
	return []corev1.Node{
		node("app-s", "cshubapp2spt", map[string]string{"app": "cshubapp"}),
		node("app-v", "cshubapp2spt", map[string]string{"app": "cshubapp"}),
		node("sys-ak", "cshubsystem", map[string]string{"app": "system"}, taintCritical),
		node("run-t", "runnerspot2", map[string]string{"app": "runners"}, taintSpot),
	}
}

func TestFixOptions_PorPoolComTolerationEValidacao(t *testing.T) {
	opts := fixOptions(monitoringPod(), monitoringNodes(), PlacementFacts{})
	byID := map[string]FixOption{}
	for _, o := range opts {
		byID[o.ID] = o
	}

	app := byID["pool:cshubapp2spt"]
	if app.NodeSelector["agentpool"] != "cshubapp2spt" || len(app.Tolerations) != 0 || len(app.Nodes) != 2 {
		t.Fatalf("pool sem taint: %+v", app)
	}
	sys := byID["pool:cshubsystem"]
	if len(sys.Tolerations) != 1 || sys.Tolerations[0].Key != "CriticalAddonsOnly" || sys.Tolerations[0].Operator != corev1.TolerationOpEqual {
		t.Fatalf("pool com taint deve trazer a toleration: %+v", sys)
	}
	if byID["no-selector"].ID == "" {
		t.Fatal("remover o nodeSelector deve ser oferecido (há nodes sem taint)")
	}
	if len(byID["no-selector"].Nodes) != 2 {
		t.Fatalf("sem seletor só os 2 nodes sem taint servem: %+v", byID["no-selector"].Nodes)
	}
}

func TestFixOptions_DescartaPoolQueNaoResolve(t *testing.T) {
	p := monitoringPod()
	p.NodeAffinity = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{
		{Key: "app", Operator: corev1.NodeSelectorOpIn, Values: []string{"monitoring"}},
	}}}}
	// a nodeAffinity obrigatória continua exigindo app=monitoring: trocar o nodeSelector não resolve
	if opts := fixOptions(p, monitoringNodes(), PlacementFacts{}); len(opts) != 0 {
		t.Fatalf("nenhuma opção deveria passar na validação: %+v", opts)
	}
}

func TestDiagnosePlacement_OpcoesSoSemNodeServindo(t *testing.T) {
	pl := diagnosePlacement(monitoringPod(), monitoringNodes(), "")
	if len(pl.Options) == 0 {
		t.Fatal("sem node servindo, deveria oferecer opções")
	}
	ok := monitoringNodes()
	ok[0].Labels["app"] = "monitoring"
	if pl := diagnosePlacement(monitoringPod(), ok, ""); len(pl.Options) != 0 {
		t.Fatalf("com node servindo não há correção de agendamento: %+v", pl.Options)
	}
}

func TestApplyScheduling_SubstituiSeletorSomaTolerationERecriaPending(t *testing.T) {
	crJSON := `{"metadata":{"name":"prom","namespace":"monitoring","labels":{"app.kubernetes.io/managed-by":"Helm"}},
	 "spec":{"nodeSelector":{"app":"monitoring","extra":"x"},
	 "tolerations":[{"key":"prometheus-only","operator":"Equal","value":"true","effect":"NoSchedule"}]}}`
	var patches []string
	run := func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "get":
			return []byte(crJSON), nil
		case "patch":
			patches = append(patches, args[len(args)-1])
			return nil, nil
		}
		return nil, nil
	}
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "prometheus-prom", Namespace: "monitoring"}}
	sts.Spec.Template.Spec.NodeSelector = map[string]string{"agentpool": "cshubsystem"} // operator já regerou
	pending := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "prometheus-prom-0", Namespace: "monitoring", Labels: map[string]string{defaultCRLabel: "prom"}},
		Status: corev1.PodStatus{Phase: corev1.PodPending}}
	running := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "prometheus-prom-1", Namespace: "monitoring", Labels: map[string]string{defaultCRLabel: "prom"}},
		Spec: corev1.PodSpec{NodeName: "n1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	cs := fake.NewSimpleClientset(sts, pending, running)

	crit := corev1.Toleration{Key: "CriticalAddonsOnly", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule}
	res, err := (&Collector{Client: cs, Run: run}).ApplyScheduling(context.Background(), SchedulingRequest{
		Cluster: "ctx", Namespace: "monitoring", Name: "prom",
		NodeSelector: map[string]string{"agentpool": "cshubsystem"},
		Tolerations:  []corev1.Toleration{crit, {Key: "prometheus-only", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoSchedule}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("um patch esperado: %v", patches)
	}
	var got struct {
		Spec struct {
			NodeSelector map[string]*string  `json:"nodeSelector"`
			Tolerations  []corev1.Toleration `json:"tolerations"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(patches[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.NodeSelector["app"] != nil || got.Spec.NodeSelector["extra"] != nil {
		t.Fatalf("chaves antigas precisam sair (null): %s", patches[0])
	}
	if v := got.Spec.NodeSelector["agentpool"]; v == nil || *v != "cshubsystem" {
		t.Fatalf("seletor novo ausente: %s", patches[0])
	}
	if len(got.Spec.Tolerations) != 2 {
		t.Fatalf("toleration existente deve ser mantida e a nova somada, sem duplicar: %s", patches[0])
	}
	if len(res.AddedTolerations) != 1 || !strings.Contains(res.AddedTolerations[0], "CriticalAddonsOnly") {
		t.Fatalf("added: %v", res.AddedTolerations)
	}
	if res.Managed != "Helm" || len(res.Warnings) == 0 {
		t.Fatalf("deve avisar que o Helm reverte: %+v", res)
	}
	if len(res.RecreatedPods) != 1 || res.RecreatedPods[0] != "prometheus-prom-0" {
		t.Fatalf("só o pod Pending sem nó é recriado: %v", res.RecreatedPods)
	}
	if _, err := cs.CoreV1().Pods("monitoring").Get(context.Background(), "prometheus-prom-1", metav1.GetOptions{}); err != nil {
		t.Fatal("pod em execução não pode ser apagado")
	}
}

func TestApplyScheduling_RejeitaSeletorInvalido(t *testing.T) {
	c := &Collector{Client: fake.NewSimpleClientset(), Run: fakeRunner(new([]string))}
	if _, err := c.ApplyScheduling(context.Background(), SchedulingRequest{Cluster: "c", Namespace: "n", Name: "p", NodeSelector: map[string]string{"app": ""}}); err == nil {
		t.Fatal("valor vazio deveria ser rejeitado")
	}
}

func withAlloc(n corev1.Node, cpu, mem string) corev1.Node {
	n.Status.Allocatable = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)}
	return n
}

func TestFixOptions_AvisaPoolDeSistema(t *testing.T) {
	var sys FixOption
	for _, o := range fixOptions(monitoringPod(), monitoringNodes(), PlacementFacts{}) {
		if o.ID == "pool:cshubsystem" {
			sys = o
		}
	}
	if !strings.Contains(strings.Join(sys.Notes, " "), "pool de SISTEMA") {
		t.Fatalf("pool com CriticalAddonsOnly deve avisar que é de sistema: %v", sys.Notes)
	}
}

func TestFixOptions_DescartaNodeSemRecursoLivre(t *testing.T) {
	p := monitoringPod()
	p.CPURequestMilli, p.MemRequestBytes = 1000, 4<<30
	nodes := []corev1.Node{
		withAlloc(node("cheio", "poolA", map[string]string{"app": "x"}), "4", "16Gi"),
		withAlloc(node("livre", "poolA", map[string]string{"app": "x"}), "4", "16Gi"),
		withAlloc(node("so-cheio", "poolB", map[string]string{"app": "y"}), "2", "8Gi"),
	}
	facts := PlacementFacts{NodeRequests: map[string]Requests{
		"cheio":    {CPUMilli: 3500, MemBytes: 4 << 30},
		"livre":    {CPUMilli: 500, MemBytes: 2 << 30},
		"so-cheio": {CPUMilli: 1900, MemBytes: 7 << 30},
	}}
	by := map[string]FixOption{}
	for _, o := range fixOptions(p, nodes, facts) {
		by[o.ID] = o
	}
	a := by["pool:poolA"]
	if len(a.Nodes) != 1 || a.Nodes[0] != "livre" || len(a.Excluded) != 1 || !strings.Contains(a.Excluded[0], "CPU livre") {
		t.Fatalf("só o node com folga deve passar: %+v", a)
	}
	b := by["pool:poolB"]
	if b.Blocked == "" || len(b.Nodes) != 0 {
		t.Fatalf("pool sem nenhum node com folga deve vir bloqueado: %+v", b)
	}
}

func TestFixOptions_SemPermissaoParaSomarRequestsAvisaENaoBloqueia(t *testing.T) {
	p := monitoringPod()
	p.CPURequestMilli = 1000
	o := fixOptions(p, monitoringNodes(), PlacementFacts{RequestsErr: "forbidden"})[0]
	if o.Blocked != "" || !strings.Contains(strings.Join(o.Notes, " "), "NÃO verificados") {
		t.Fatalf("sem dados deve avisar, não bloquear: %+v", o)
	}
}

func TestFixOptions_DescartaNodeEmOutraZonaDoVolume(t *testing.T) {
	nodes := []corev1.Node{
		node("z1", "pool", map[string]string{"app": "x", "topology.kubernetes.io/zone": "brazilsouth-1"}),
		node("z2", "pool", map[string]string{"app": "x", "topology.kubernetes.io/zone": "brazilsouth-2"}),
	}
	o := fixOptions(monitoringPod(), nodes, PlacementFacts{PVCZone: "brazilsouth-2"})[0]
	if len(o.Nodes) != 1 || o.Nodes[0] != "z2" || !strings.Contains(o.Excluded[0], "zona") {
		t.Fatalf("só o node da zona do volume serve: %+v", o)
	}
}

func TestLabelAdvice_ComandoPorNuvemEAusenciaQuandoHaNode(t *testing.T) {
	adv := labelAdvice(monitoringPod(), monitoringNodes())
	if adv == nil || !strings.HasPrefix(adv.Command, "az aks nodepool update") || !strings.Contains(adv.Command, "--labels app=monitoring") || adv.Warning == "" {
		t.Fatalf("AKS: %+v", adv)
	}
	if len(adv.Pools) != 3 {
		t.Fatalf("pools: %+v", adv.Pools)
	}
	eks := []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "e1", Labels: map[string]string{"eks.amazonaws.com/nodegroup": "ng1"}}}}
	if a := labelAdvice(monitoringPod(), eks); a == nil || !strings.HasPrefix(a.Command, "aws eks update-nodegroup-config") || a.Warning != "" {
		t.Fatalf("EKS (addOrUpdateLabels não substitui): %+v", a)
	}
	ok := monitoringNodes()
	ok[0].Labels["app"] = "monitoring"
	if labelAdvice(monitoringPod(), ok) != nil {
		t.Fatal("com node já rotulado não há o que restaurar")
	}
}

func TestPlacementFacts_ZonaDoVolumeESomaDeRequests(t *testing.T) {
	ns := "monitoring"
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: ns}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1"}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-1"}, Spec: corev1.PersistentVolumeSpec{NodeAffinity: &corev1.VolumeNodeAffinity{
		Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{
			{Key: "topology.disk.csi.azure.com/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"brazilsouth-3"}}}}}}}}}
	running := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "x"}, Spec: corev1.PodSpec{NodeName: "n1", Containers: []corev1.Container{
		{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("1Gi")}}},
		{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}}},
	}}}
	f := (&Collector{Client: fake.NewSimpleClientset(pvc, pv, running)}).placementFacts(context.Background(), ns, PodInfo{PVC: "data"})
	if f.PVCZone != "brazilsouth-3" {
		t.Fatalf("zona do PV: %q (%s)", f.PVCZone, f.PVCNote)
	}
	if r := f.NodeRequests["n1"]; r.CPUMilli != 500 || r.MemBytes != 1<<30 {
		t.Fatalf("requests do node: %+v", r)
	}
	f = (&Collector{Client: fake.NewSimpleClientset()}).placementFacts(context.Background(), ns, PodInfo{PVC: "data"})
	if f.PVCZone != "" || f.PVCNote == "" {
		t.Fatalf("PVC inexistente vira nota, não zona: %+v", f)
	}
}
