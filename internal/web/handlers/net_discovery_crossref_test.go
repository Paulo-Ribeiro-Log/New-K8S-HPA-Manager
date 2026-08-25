package handlers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"k8s-hpa-manager/internal/storage"
)

// newTestNetDiscoveryHandler cria um NetDiscoveryHandler só com o registry populado (SQLite real
// num diretório temporário) — o mínimo necessário pra exercitar crossReferenceIP/crossReferenceHops
// sem subir o resto da infraestrutura do handler (kubeManager/tracker/historyTracker não são
// tocados pelo cross-reference).
func newTestNetDiscoveryHandler(t *testing.T) *NetDiscoveryHandler {
	t.Helper()
	store, err := storage.NewNetDiscoveryRegistryStore(filepath.Join(t.TempDir(), "net-discovery-registry.db"))
	if err != nil {
		t.Fatalf("falha ao criar registry de teste: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &NetDiscoveryHandler{registry: store}
}

func TestCrossReferenceIP_PublicIPNeverConsultsRegistry(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	// Mesmo com uma entrada em cache pra este IP (hipotética — nunca inserida aqui de propósito),
	// o gate de IP privado deve descartar ANTES de qualquer leitura no registry.
	ref := h.crossReferenceIP(context.Background(), "8.8.8.8", nil, "")
	if ref != nil {
		t.Fatalf("IP público não deveria nunca virar InternalRef, got %+v", ref)
	}
}

func TestCrossReferenceIP_LocalModeNeverDoesLiveLookup(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	// clientset nil simula o modo local (ver runDiscovery: podClientset só é atribuído no modo
	// pod) — mesmo com um IP privado nunca visto, não deve haver busca ao vivo (não há como uma
	// busca ao vivo acontecer sem clientset de qualquer forma, mas o contrato explícito da função
	// é retornar nil limpo nesse caso, não panicar tentando usar um clientset nulo).
	ref := h.crossReferenceIP(context.Background(), "10.1.2.3", nil, "algum-cluster")
	if ref != nil {
		t.Fatalf("modo local sem cache não deveria produzir InternalRef, got %+v", ref)
	}
}

func TestCrossReferenceIP_CacheHitSkipsLiveLookup(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	if err := h.registry.Upsert(storage.NetDiscoveryIPCacheEntry{
		IP: "10.0.0.5", Kind: "node", Name: "aks-nodepool1-12345678-vmss000000", Cluster: "meu-cluster",
		CachedAt: time.Now(),
	}); err != nil {
		t.Fatalf("upsert falhou: %v", err)
	}

	// clientset nil de propósito: se a função tentasse mesmo assim uma busca ao vivo (bug), um
	// nil pointer dereference apareceria aqui — o teste passar confirma que o cache-hit satisfez
	// a consulta sem precisar do clientset.
	ref := h.crossReferenceIP(context.Background(), "10.0.0.5", nil, "meu-cluster")
	if ref == nil {
		t.Fatal("esperava cache-hit, got nil")
	}
	if ref.Kind != "node" || ref.Name != "aks-nodepool1-12345678-vmss000000" {
		t.Errorf("dado do cache não bateu: %+v", ref)
	}
}

func TestCrossReferenceIP_LiveLookupNodeMatch(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	clientset := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "aks-nodepool1-12345678-vmss000001"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.1.10"}},
		},
	})

	ref := h.crossReferenceIP(context.Background(), "10.0.1.10", clientset, "meu-cluster")
	if ref == nil {
		t.Fatal("esperava match de node, got nil")
	}
	if ref.Kind != "node" || ref.Name != "aks-nodepool1-12345678-vmss000001" || ref.Cluster != "meu-cluster" {
		t.Errorf("resultado inesperado: %+v", ref)
	}

	// Confirma que a busca ao vivo bem-sucedida grava no cache (cache-on-read) — segunda chamada
	// com clientset nil (simulando modo local depois) ainda encontra o dado.
	ref2 := h.crossReferenceIP(context.Background(), "10.0.1.10", nil, "meu-cluster")
	if ref2 == nil || ref2.Kind != "node" {
		t.Fatalf("esperava que a busca ao vivo tivesse persistido no cache, got %+v", ref2)
	}
}

func TestCrossReferenceIP_LiveLookupServiceMatch(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	clientset := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "checkout-api", Namespace: "prod"},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.20"},
	})

	ref := h.crossReferenceIP(context.Background(), "10.96.0.20", clientset, "meu-cluster")
	if ref == nil {
		t.Fatal("esperava match de service, got nil")
	}
	if ref.Kind != "service" || ref.Name != "checkout-api" || ref.Namespace != "prod" {
		t.Errorf("resultado inesperado: %+v", ref)
	}
}

func TestCrossReferenceIP_LiveLookupPodResolvesToOwnerDeployment(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	rsController := true
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "checkout-api-7d9f8c6b5d", Namespace: "prod",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "checkout-api", Controller: &rsController},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "checkout-api-7d9f8c6b5d-xk2p9", Namespace: "prod",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: "checkout-api-7d9f8c6b5d", Controller: &rsController, UID: types.UID("rs-uid")},
			},
		},
		Status: corev1.PodStatus{PodIP: "10.244.3.7"},
	}
	rs.UID = types.UID("rs-uid")

	clientset := fake.NewSimpleClientset(rs, pod)

	ref := h.crossReferenceIP(context.Background(), "10.244.3.7", clientset, "meu-cluster")
	if ref == nil {
		t.Fatal("esperava match de pod, got nil")
	}
	if ref.Kind != "pod" {
		t.Errorf("Kind = %q, want pod", ref.Kind)
	}
	if ref.PodName != "checkout-api-7d9f8c6b5d-xk2p9" {
		t.Errorf("PodName = %q, want o nome literal do Pod object", ref.PodName)
	}
	// resolveOwnerDisplayName deve subir até o Deployment, não parar no ReplicaSet intermediário.
	if ref.Name != "Deployment/checkout-api" {
		t.Errorf("Name = %q, want Deployment/checkout-api (owner resolvido via resolveOwnerDisplayName)", ref.Name)
	}
}

func TestCrossReferenceIP_NoMatchReturnsNil(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	clientset := fake.NewSimpleClientset() // frota vazia

	ref := h.crossReferenceIP(context.Background(), "10.5.5.5", clientset, "meu-cluster")
	if ref != nil {
		t.Fatalf("frota vazia não deveria produzir match, got %+v", ref)
	}
}

func TestCrossReferenceIP_NilRegistryDisablesLayerEntirely(t *testing.T) {
	// h.registry == nil (registry falhou ao inicializar no server.go, ex: disco cheio) — a camada
	// inteira deve se desligar sem erro, nunca um nil pointer dereference.
	h := &NetDiscoveryHandler{}
	clientset := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "algum-node"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.1.10"}},
		},
	})
	ref := h.crossReferenceIP(context.Background(), "10.0.1.10", clientset, "meu-cluster")
	if ref != nil {
		t.Fatalf("registry nil deveria desligar a camada inteira, got %+v", ref)
	}
}

func TestCrossReferenceHops_SkipsEmptyIPAndOnlyTouchesInternalRef(t *testing.T) {
	h := newTestNetDiscoveryHandler(t)
	clientset := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "aks-nodepool1-vmss0"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.1.10"}},
		},
	})

	hops := []NetDiscoveryHop{
		{Index: 1, TimedOut: true},  // sem IP — deve ser pulado, sem panic
		{Index: 2, IP: "10.0.1.10"}, // bate com o node acima
		{Index: 3, IP: "8.8.8.8"},   // público — nunca vira InternalRef
	}

	h.crossReferenceHops(context.Background(), hops, clientset, "meu-cluster")

	if hops[0].InternalRef != nil {
		t.Errorf("hop sem IP não deveria ganhar InternalRef")
	}
	if hops[1].InternalRef == nil || hops[1].InternalRef.Kind != "node" {
		t.Errorf("hop 2 deveria ter virado node, got %+v", hops[1].InternalRef)
	}
	if hops[2].InternalRef != nil {
		t.Errorf("hop público não deveria ganhar InternalRef, got %+v", hops[2].InternalRef)
	}
}
