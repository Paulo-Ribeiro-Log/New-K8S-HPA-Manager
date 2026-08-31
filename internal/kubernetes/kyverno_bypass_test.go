package kubernetes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSetNamespaceKyvernoBypass_EnableAddsLabel(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ns-x"},
	})
	client := NewClient(cs, "test-cluster")

	if err := client.SetNamespaceKyvernoBypass(context.Background(), "ns-x", true); err != nil {
		t.Fatalf("SetNamespaceKyvernoBypass(enable) falhou: %v", err)
	}

	ns, err := cs.CoreV1().Namespaces().Get(context.Background(), "ns-x", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get namespace falhou: %v", err)
	}
	if got := ns.Labels[KyvernoBypassLabelKey]; got != "true" {
		t.Errorf("label %s = %q, esperava \"true\"", KyvernoBypassLabelKey, got)
	}
}

func TestSetNamespaceKyvernoBypass_DisableRemovesLabel(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "ns-x",
			Labels: map[string]string{KyvernoBypassLabelKey: "true", "outra-label": "preservada"},
		},
	})
	client := NewClient(cs, "test-cluster")

	if err := client.SetNamespaceKyvernoBypass(context.Background(), "ns-x", false); err != nil {
		t.Fatalf("SetNamespaceKyvernoBypass(disable) falhou: %v", err)
	}

	ns, err := cs.CoreV1().Namespaces().Get(context.Background(), "ns-x", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get namespace falhou: %v", err)
	}
	if _, ok := ns.Labels[KyvernoBypassLabelKey]; ok {
		t.Errorf("label %s ainda presente após disable: %+v", KyvernoBypassLabelKey, ns.Labels)
	}
	// Nunca mexe em outras labels do namespace — merge patch só toca a chave do bypass.
	if got := ns.Labels["outra-label"]; got != "preservada" {
		t.Errorf("outra-label = %q, esperava \"preservada\" (não devia ter sido tocada)", got)
	}
}

func TestSetNamespaceKyvernoBypass_NamespaceInexistente(t *testing.T) {
	cs := fake.NewSimpleClientset()
	client := NewClient(cs, "test-cluster")

	if err := client.SetNamespaceKyvernoBypass(context.Background(), "nao-existe", true); err == nil {
		t.Error("esperava erro ao aplicar patch num namespace inexistente, veio nil")
	}
}
