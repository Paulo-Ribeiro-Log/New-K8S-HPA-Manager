package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClassifyNodeInterruption(t *testing.T) {
	cases := []struct{ reason, msg, want string }{
		{"SpotEvictionIncoming", "Preempt Started.", nodeCategorySpot},
		{"PreemptScheduled", "VM Scheduled for Preempt at 2026-10-02T10:00:00Z", nodeCategorySpot},
		{"TerminateScheduled", "VM Scheduled for Terminate", nodeCategoryScheduled},
		{"RebootScheduled", "", nodeCategoryScheduled},
		{"RedeployScheduled", "", nodeCategoryScheduled},
		{"FreezeScheduled", "", nodeCategoryScheduled},
		{"NodeNotReady", "Node is not ready", ""},
		// falso positivo real: node recém-criado e saudável
		{"NoVMEventScheduled", `Node condition VMEventScheduled is now: False, reason: NoVMEventScheduled, message: "VM has no scheduled event"`, ""},
		{"VMEventScheduled", `Node condition VMEventScheduled is now: False, reason: NoVMEventScheduled`, ""},
		{"NoPreemptScheduled", "VM has no scheduled event", ""},
		{"RemovingNode", "Removing node aks-x from the cluster", ""},
	}
	for _, c := range cases {
		if got := classifyNodeInterruption(c.reason, c.msg); got != c.want {
			t.Errorf("classify(%q, %q) = %q, want %q", c.reason, c.msg, got, c.want)
		}
	}
}

// Evento real do AKS: antes era descartado (o filtro só procurava "evict"/"terminat" na mensagem).
func TestFetchNodeEventsV2_SpotEviction(t *testing.T) {
	node := "aks-monitrngspot-33421997-vmss000000"
	now := metav1.NewTime(time.Now())
	cs := fake.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{Kind: "Node", Name: node},
			Reason:         "SpotEvictionIncoming", Message: "Preempt Started.", Type: "Warning",
			LastTimestamp: now,
		},
		// evento genérico mais recente não pode esconder a causa (spot)
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e2", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{Kind: "Node", Name: node},
			Reason:         "NodeNotReady", Message: "Node is not ready", Type: "Normal",
			LastTimestamp: metav1.NewTime(now.Add(time.Minute)),
		},
	)
	nodes, _ := fetchNodeEventsV2(context.Background(), cs, "monitrngspot")
	if len(nodes) != 1 {
		t.Fatalf("esperava 1 node, veio %+v", nodes)
	}
	n := nodes[0]
	if n.Category != nodeCategorySpot || !strings.HasPrefix(n.Reason, "SpotEvictionIncoming") {
		t.Errorf("node = %+v", n)
	}
	if !strings.Contains(n.Details, "NodeNotReady") || !strings.Contains(n.Details, "Preempt Started.") {
		t.Errorf("details deveria ter os dois eventos: %q", n.Details)
	}
}

// Evento "sem evento agendado" do NPD num node saudável não pode entrar na lista de removidos.
func TestFetchNodeEventsV2_NoVMEventScheduledIgnorado(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Node", Name: "aks-novo-1-vmss000000"},
		Reason:         "NoVMEventScheduled", Type: "Normal",
		Message:       `Node condition VMEventScheduled is now: False, reason: NoVMEventScheduled, message: "VM has no scheduled event"`,
		LastTimestamp: metav1.NewTime(time.Now()),
	})
	if nodes, _ := fetchNodeEventsV2(context.Background(), cs, "novo-1"); len(nodes) != 0 {
		t.Fatalf("node saudável não deveria aparecer: %+v", nodes)
	}
}

// Node ainda existente, Ready e não cordoned, mas com Scheduled Event ativo (condition do NPD).
func TestFetchUnhealthyNodes_ScheduledEventCondition(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "aks-spot-1-vmss000001"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: "PreemptScheduled", Status: corev1.ConditionTrue, Reason: "PreemptScheduled", Message: "VM Scheduled for Preempt"},
			}},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "aks-spot-1-vmss000002"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: "PreemptScheduled", Status: corev1.ConditionFalse},
			}},
		},
	)
	nodes, _ := fetchUnhealthyNodes(context.Background(), cs, "spot-1")
	if len(nodes) != 1 || nodes[0].Name != "aks-spot-1-vmss000001" {
		t.Fatalf("esperava só o node com PreemptScheduled=True, veio %+v", nodes)
	}
	if nodes[0].Category != nodeCategorySpot || nodes[0].Source != "k8s-node-scheduled-event" {
		t.Errorf("node = %+v", nodes[0])
	}
}
