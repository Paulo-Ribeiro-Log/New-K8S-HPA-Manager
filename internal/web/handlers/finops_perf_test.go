package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func benchNode(name, pool string, labels map[string]string, ready, cordoned bool) corev1.Node {
	l := map[string]string{"kubernetes.azure.com/agentpool": pool, "kubernetes.io/hostname": name}
	for k, v := range labels {
		l[k] = v
	}
	st := corev1.ConditionFalse
	if ready {
		st = corev1.ConditionTrue
	}
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: l},
		Spec:       corev1.NodeSpec{Unschedulable: cordoned},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: st}}},
	}
}

func withCondition(n corev1.Node, t corev1.NodeConditionType) corev1.Node {
	n.Status.Conditions = append(n.Status.Conditions, corev1.NodeCondition{Type: t, Status: corev1.ConditionTrue})
	return n
}

func withTaint(n corev1.Node, key string) corev1.Node {
	n.Spec.Taints = append(n.Spec.Taints, corev1.Taint{Key: key, Effect: corev1.TaintEffectNoSchedule})
	return n
}

func names(ns []corev1.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Name
	}
	return out
}

func TestPickBenchNodes_EspalhaPelaLista(t *testing.T) {
	var nodes []corev1.Node
	for _, n := range []string{"n-a", "n-b", "n-c", "n-d", "n-e", "n-f"} {
		nodes = append(nodes, benchNode(n, "calc", nil, true, false))
	}
	got := names(pickBenchNodes(nodes, nil, 2))
	// 6 nodes, 2 amostras: pontas opostas da lista ordenada (índices 0 e 3), não os 2 primeiros.
	if len(got) != 2 || got[0] != "n-a" || got[1] != "n-d" {
		t.Errorf("amostragem deveria espalhar (n-a, n-d), got %v", got)
	}
	if got := pickBenchNodes(nodes[:1], nil, 3); len(got) != 1 {
		t.Errorf("pool com 1 node só rende 1 amostra, got %d", len(got))
	}
}

func TestPickBenchNodes_Filtros(t *testing.T) {
	nodes := []corev1.Node{
		benchNode("ok-1", "apps", nil, true, false),
		benchNode("notready", "apps", nil, false, false),
		benchNode("cordoned", "apps", nil, true, true),
		benchNode("win", "apps", map[string]string{"kubernetes.io/os": "windows"}, true, false),
		withTaint(benchNode("repair", "apps", nil, true, false), "remediator.kubernetes.azure.com/unschedulable"),
		withTaint(benchNode("spot", "apps", nil, true, false), "kubernetes.azure.com/scalesetpriority"), // spot: taint normal, continua elegível
		withCondition(benchNode("disk", "apps", nil, true, false), corev1.NodeDiskPressure),             // imagem de 500 MB pioraria
		benchNode("sys-1", "system", map[string]string{"kubernetes.azure.com/mode": "system"}, true, false),
		benchNode("ing-1", "ingress", nil, true, false),
	}
	got := names(pickBenchNodes(nodes, nil, 5))
	want := map[string]bool{"ok-1": true, "spot": true, "ing-1": true}
	if len(got) != 3 || !want[got[0]] || !want[got[1]] || !want[got[2]] {
		t.Errorf("sem filtro: só Linux/Ready/não-cordonado/fora de reparo e sem pool de sistema (spot continua elegível), got %v", got)
	}
	// Pedindo o pool de sistema explicitamente, ele entra.
	if got := names(pickBenchNodes(nodes, []string{"system"}, 5)); len(got) != 1 || got[0] != "sys-1" {
		t.Errorf("pool pedido explicitamente deveria entrar, got %v", got)
	}
	if got := pickBenchNodes(nodes, []string{"nao-existe"}, 5); len(got) != 0 {
		t.Errorf("pool inexistente → nada, got %v", names(got))
	}
}

func TestBuildPerfBenchPod_Contrato(t *testing.T) {
	n := benchNode("aks-pool-123-vmss0001", "ingress", nil, true, false)
	p := buildPerfBenchPod("default", n)

	if p.Spec.NodeSelector["kubernetes.io/hostname"] != "aks-pool-123-vmss0001" {
		t.Errorf("pod precisa ser fixado no node medido: %v", p.Spec.NodeSelector)
	}
	if len(p.Spec.Tolerations) != 1 || p.Spec.Tolerations[0].Operator != corev1.TolerationOpExists {
		t.Errorf("precisa tolerar qualquer taint (pools ingress/infra/spot): %v", p.Spec.Tolerations)
	}
	if p.Spec.RestartPolicy != corev1.RestartPolicyNever || p.Spec.ActiveDeadlineSeconds == nil {
		t.Error("pod deve ser de execução única, com deadline (limpa sozinho se o handler morrer)")
	}
	if p.Annotations["sidecar.istio.io/inject"] != "false" || p.Labels["sidecar.istio.io/inject"] != "false" {
		t.Error("sem sidecar do Istio (senão o pod nunca completa)")
	}
	sc := p.Spec.Containers[0].SecurityContext
	if sc == nil || sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation ||
		sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
		t.Errorf("container precisa rodar sem root, sem escalada e sem capabilities: %+v", sc)
	}
	if p.Spec.AutomountServiceAccountToken == nil || *p.Spec.AutomountServiceAccountToken {
		t.Error("pod descartável não deve montar token de service account")
	}
	r := p.Spec.Containers[0].Resources
	if _, ok := r.Limits[corev1.ResourceCPU]; ok {
		t.Error("NÃO pode haver limit de CPU — estrangularia a própria medição")
	}
	if r.Requests.Cpu().MilliValue() != 250 {
		t.Errorf("request de CPU deve ser 250m (share justo no node), got %dm", r.Requests.Cpu().MilliValue())
	}
}
