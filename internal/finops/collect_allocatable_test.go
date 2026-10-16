package finops

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func nodeWithAlloc(name, pool, cpu, mem string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"agentpool": pool}},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(mem),
		}},
	}
}

// A capacidade usada na aba Node Pools é a alocável (o que sobra para os pods), não a nominal da VM:
// numa F4s_v2 (4 vCPU / 8 GiB) o AKS deixa ~3860m e ~5,6 GiB.
func TestCollectWorkloads_SomaAllocatablePorPool(t *testing.T) {
	client := fake.NewSimpleClientset(
		nodeWithAlloc("n1", "calculofrete", "3860m", "5719Mi"),
		nodeWithAlloc("n2", "calculofrete", "3860m", "5719Mi"),
		nodeWithAlloc("n3", "frete", "3860m", "5719Mi"),
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "kafka-0",
				OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "kafka"}}},
			Spec:   corev1.PodSpec{NodeName: "n1"},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)
	raw, _, _, alloc, err := collectWorkloads(context.Background(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a := alloc["calculofrete"]; a.cpuMillis != 7720 || a.memMi != 11438 || a.nodes != 2 {
		t.Errorf("calculofrete: %+v", a)
	}
	if a := alloc["frete"]; a.nodes != 1 || a.cpuMillis != 3860 {
		t.Errorf("frete: %+v", a)
	}
	if len(raw) != 1 || raw[0].Kind != "StatefulSet" {
		t.Errorf("workload: %+v", raw)
	}
}
