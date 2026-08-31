package handlers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	kubeclient "k8s-hpa-manager/internal/kubernetes"
)

func newBypassTestClient(t *testing.T, namespace string) (*DeploymentRollbackHandler, *kubeclient.Client, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
	kubeClient := kubeclient.NewClient(cs, "test-cluster")
	return &DeploymentRollbackHandler{}, kubeClient, cs
}

func namespaceHasBypassLabel(t *testing.T, cs *fake.Clientset, namespace string) bool {
	t.Helper()
	ns, err := cs.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get namespace: %v", err)
	}
	return ns.Labels[kubeclient.KyvernoBypassLabelKey] == "true"
}

// TestWithKyvernoBypass_EnablesDuringFnDisablesAfter cobre o caso simples: a label existe DENTRO
// de fn() e some depois que fn() retorna — mesmo procedimento manual (label antes, remove depois
// "por segurança") que o usuário descreveu, agora automatizado.
func TestWithKyvernoBypass_EnablesDuringFnDisablesAfter(t *testing.T) {
	h, kubeClient, cs := newBypassTestClient(t, "ns-a")

	var sawLabelDuring bool
	err := h.withKyvernoBypass(context.Background(), kubeClient, "cluster-1", "ns-a", func() error {
		sawLabelDuring = namespaceHasBypassLabel(t, cs, "ns-a")
		return nil
	})
	if err != nil {
		t.Fatalf("withKyvernoBypass falhou: %v", err)
	}
	if !sawLabelDuring {
		t.Error("esperava a label presente DENTRO de fn()")
	}
	if namespaceHasBypassLabel(t, cs, "ns-a") {
		t.Error("esperava a label removida depois de fn() retornar")
	}
}

// TestWithKyvernoBypass_ConcurrentCallsSameNamespace_LabelStaysUntilLast cobre o cenário de
// concorrência que motivou o ref-counting: 2 rollbacks de Deployments DIFERENTES no MESMO
// namespace não podem fazer um remover a label enquanto o outro ainda depende dela.
func TestWithKyvernoBypass_ConcurrentCallsSameNamespace_LabelStaysUntilLast(t *testing.T) {
	h, kubeClient, cs := newBypassTestClient(t, "ns-b")

	release1 := make(chan struct{})
	started2 := make(chan struct{})
	release2 := make(chan struct{})
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	go func() {
		_ = h.withKyvernoBypass(context.Background(), kubeClient, "cluster-1", "ns-b", func() error {
			<-release1
			return nil
		})
		close(done1)
	}()

	go func() {
		_ = h.withKyvernoBypass(context.Background(), kubeClient, "cluster-1", "ns-b", func() error {
			close(started2)
			<-release2
			return nil
		})
		close(done2)
	}()

	<-started2 // as duas fn() estão em andamento agora
	if !namespaceHasBypassLabel(t, cs, "ns-b") {
		t.Fatal("esperava label presente com as 2 chamadas em andamento")
	}

	close(release1)
	<-done1 // 1ª chamada (incluindo seu defer de decrement) já terminou

	if !namespaceHasBypassLabel(t, cs, "ns-b") {
		t.Error("esperava label AINDA presente — a 2ª chamada continua em andamento")
	}

	close(release2)
	<-done2

	if namespaceHasBypassLabel(t, cs, "ns-b") {
		t.Error("esperava label removida só depois que a ÚLTIMA chamada terminou")
	}
}

// TestWithKyvernoBypass_FnErrorStillRemovesLabel garante que a label é removida mesmo quando fn()
// retorna erro — nunca fica presa ligada só porque a mutação em si falhou.
func TestWithKyvernoBypass_FnErrorStillRemovesLabel(t *testing.T) {
	h, kubeClient, cs := newBypassTestClient(t, "ns-c")

	err := h.withKyvernoBypass(context.Background(), kubeClient, "cluster-1", "ns-c", func() error {
		return context.DeadlineExceeded // erro qualquer, simula falha da mutação
	})
	if err == nil {
		t.Fatal("esperava withKyvernoBypass propagar o erro de fn()")
	}
	if namespaceHasBypassLabel(t, cs, "ns-c") {
		t.Error("esperava label removida mesmo com fn() retornando erro")
	}
}

// TestWithKyvernoBypass_DifferentNamespacesIndependentCounters garante que o contador de
// referência é por "cluster/namespace" — uma chamada em outro namespace não interfere.
func TestWithKyvernoBypass_DifferentNamespacesIndependentCounters(t *testing.T) {
	h := &DeploymentRollbackHandler{}
	cs := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-d1"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns-d2"}},
	)
	kubeClient := kubeclient.NewClient(cs, "test-cluster")

	if err := h.withKyvernoBypass(context.Background(), kubeClient, "cluster-1", "ns-d1", func() error { return nil }); err != nil {
		t.Fatalf("withKyvernoBypass(ns-d1) falhou: %v", err)
	}
	if err := h.withKyvernoBypass(context.Background(), kubeClient, "cluster-1", "ns-d2", func() error { return nil }); err != nil {
		t.Fatalf("withKyvernoBypass(ns-d2) falhou: %v", err)
	}

	if namespaceHasBypassLabel(t, cs, "ns-d1") || namespaceHasBypassLabel(t, cs, "ns-d2") {
		t.Error("esperava as duas labels removidas após cada chamada independente terminar")
	}
}
