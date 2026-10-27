package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nodeWithPool(name, pool, vm string) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
		"kubernetes.azure.com/agentpool":   pool,
		"node.kubernetes.io/instance-type": vm,
	}}}
}

func TestAggregateNodePools_RefleteNomeEFamiliaNovos(t *testing.T) {
	// node pool renomeado (calculofrete → calculofret2) e trocado de família (D → F)
	nodes := []corev1.Node{
		nodeWithPool("aks-calculofret2-1-vmss000000", "calculofret2", "Standard_F4s_v2"),
		nodeWithPool("aks-calculofret2-1-vmss000001", "calculofret2", "Standard_F4s_v2"),
		nodeWithPool("aks-system-1-vmss000000", "system", "Standard_D2s_v3"),
	}
	got := aggregateNodePools(nodes)

	if _, ok := got["calculofrete"]; ok {
		t.Fatal("o nome antigo não deveria aparecer")
	}
	p := got["calculofret2"]
	if p == nil || p.count != 2 || p.vmSize != "Standard_F4s_v2" {
		t.Fatalf("calculofret2 = %+v", p)
	}
	if len(got) != 2 {
		t.Fatalf("esperava 2 pools, veio %d", len(got))
	}
}
