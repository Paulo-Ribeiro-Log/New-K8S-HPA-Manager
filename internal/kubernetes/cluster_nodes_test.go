package kubernetes

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"k8s-hpa-manager/internal/models"
)

func drainTestPod(name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "app", UID: k8stypes.UID("uid-" + name),
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "rs"}},
		},
		Spec:   corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, m := range mutate {
		m(p)
	}
	return p
}

func fastDrain(t *testing.T) {
	t.Helper()
	oldRetry, oldPoll := drainRetryInterval, drainPollInterval
	drainRetryInterval, drainPollInterval = 5*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { drainRetryInterval, drainPollInterval = oldRetry, oldPoll })
}

// evictionReactor simula a Eviction API: devolve 429 (PDB) nas primeiras `blocked` chamadas e
// depois remove o pod do tracker.
func evictionReactor(cs *fake.Clientset, blocked int32, calls *int32) {
	cs.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "eviction" {
			return false, nil, nil
		}
		n := atomic.AddInt32(calls, 1)
		if n <= blocked {
			return true, nil, apierrors.NewTooManyRequests("Cannot evict pod as it would violate the pod's disruption budget.", 0)
		}
		ev := action.(k8stesting.CreateAction).GetObject().(interface{ GetName() string })
		gvr := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
		_ = cs.Tracker().Delete(gvr, action.GetNamespace(), ev.GetName())
		return true, nil, nil
	})
}

func TestCheckDrainable_RecusaSemRemoverNada(t *testing.T) {
	fastDrain(t)
	cs := fake.NewSimpleClientset(
		drainTestPod("sem-controller", func(p *corev1.Pod) { p.OwnerReferences = nil }),
		drainTestPod("com-emptydir", func(p *corev1.Pod) {
			p.Spec.Volumes = []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
		}),
		drainTestPod("normal"),
	)
	var calls int32
	evictionReactor(cs, 0, &calls)

	opts := models.DefaultDrainOptions()
	opts.DeleteEmptyDirData = false
	err := NewClient(cs, "c").CheckDrainable(context.Background(), "node-1", opts)
	if err == nil {
		t.Fatal("drain deveria ser recusado")
	}
	for _, want := range []string{"app/sem-controller", "app/com-emptydir", "force", "emptyDir"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("erro deveria citar %q: %v", want, err)
		}
	}
	if calls != 0 {
		t.Errorf("nenhum pod deveria ter sido removido antes da recusa, evictions=%d", calls)
	}
}

func TestDrain_RetentaPDBEReportaProgresso(t *testing.T) {
	fastDrain(t)
	cs := fake.NewSimpleClientset(
		drainTestPod("web-1"),
		drainTestPod("static", func(p *corev1.Pod) { // mirror pod: sem controller, mas é ignorado
			p.OwnerReferences = nil
			p.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: "x"}
		}),
		drainTestPod("ds", func(p *corev1.Pod) { p.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "ds"}} }),
	)
	var calls int32
	evictionReactor(cs, 2, &calls) // PDB bloqueia 2 vezes

	var events []DrainEvent
	err := NewClient(cs, "c").DrainNodeWithProgress(context.Background(), "node-1", models.DefaultDrainOptions(),
		func(e DrainEvent) { events = append(events, e) })
	if err != nil {
		t.Fatalf("drain: %v", err)
	}

	var types []string
	for _, e := range events {
		types = append(types, e.Type)
	}
	got := strings.Join(types, ",")
	if got != "start,evicting,blocked,blocked,evicted,done" {
		t.Errorf("eventos = %s", got)
	}
	if last := events[len(events)-1]; last.Evicted != 1 || last.Total != 1 {
		t.Errorf("done = %+v, esperava 1/1 (mirror e DaemonSet fora)", last)
	}
}

func TestDrain_PDBAteOTimeoutFalha(t *testing.T) {
	fastDrain(t)
	cs := fake.NewSimpleClientset(drainTestPod("web-1"))
	var calls int32
	evictionReactor(cs, 1<<30, &calls)

	opts := models.DefaultDrainOptions()
	opts.Timeout = "50ms"
	err := NewClient(cs, "c").DrainNode(context.Background(), "node-1", opts)
	if err == nil || !strings.Contains(err.Error(), "PodDisruptionBudget") {
		t.Fatalf("esperava falha por PDB no timeout, got %v", err)
	}
}

func TestSummarizeNode(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "aks-app-123", Labels: map[string]string{
			"kubernetes.azure.com/agentpool":   "app",
			"node-role.kubernetes.io/worker":   "",
			"topology.kubernetes.io/zone":      "brazilsouth-1",
			"node.kubernetes.io/instance-type": "Standard_D8s_v5",
		}},
		Spec: corev1.NodeSpec{Unschedulable: true, Taints: []corev1.Taint{
			{Key: "dedicated", Value: "batch", Effect: corev1.TaintEffectNoSchedule},
			{Key: "node.kubernetes.io/unschedulable", Effect: corev1.TaintEffectNoSchedule},
		}},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("7820m"), corev1.ResourceMemory: resource.MustParse("28Gi"),
				corev1.ResourcePods: resource.MustParse("110"),
			},
		},
	}
	s := summarizeNode(n)
	if s.Status != "Ready" || !s.Unschedulable || s.NodePool != "app" || s.Zone != "brazilsouth-1" {
		t.Errorf("resumo inesperado: %+v", s)
	}
	if strings.Join(s.Roles, ",") != "worker" || strings.Join(s.Pressures, ",") != "MemoryPressure" {
		t.Errorf("roles=%v pressures=%v", s.Roles, s.Pressures)
	}
	if strings.Join(s.Taints, ",") != "dedicated=batch:NoSchedule,node.kubernetes.io/unschedulable:NoSchedule" {
		t.Errorf("taints=%v", s.Taints)
	}
	if s.CPUAllocatable != 7820 || s.PodsCapacity != 110 {
		t.Errorf("allocatable cpu=%d pods=%d", s.CPUAllocatable, s.PodsCapacity)
	}
}

func TestApplyNode_RecusaNomeOuKindErrado(t *testing.T) {
	c := NewClient(fake.NewSimpleClientset(), "c")
	if _, err := c.ApplyNode(context.Background(), "kind: Node\nmetadata:\n  name: outro\n", "", "node-1", true, false); err == nil {
		t.Error("nome diferente do node editado deveria ser recusado")
	}
	if _, err := c.ApplyNode(context.Background(), "kind: Pod\nmetadata:\n  name: node-1\n", "", "node-1", true, false); err == nil {
		t.Error("kind diferente de Node deveria ser recusado")
	}
}

// O sequenciamento de node pools chama DrainNode direto (sem CheckDrainable): pods sem controller e
// com emptyDir continuam sendo removidos, como antes desta mudança.
func TestDrainNode_MantemComportamentoDoSequenciamento(t *testing.T) {
	fastDrain(t)
	cs := fake.NewSimpleClientset(
		drainTestPod("sem-controller", func(p *corev1.Pod) { p.OwnerReferences = nil }),
		drainTestPod("com-emptydir", func(p *corev1.Pod) {
			p.Spec.Volumes = []corev1.Volume{{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
		}),
	)
	var calls int32
	evictionReactor(cs, 0, &calls)

	opts := models.DefaultDrainOptions()
	opts.DeleteEmptyDirData = false
	if err := NewClient(cs, "c").DrainNode(context.Background(), "node-1", opts); err != nil {
		t.Fatalf("DrainNode: %v", err)
	}
	if calls != 2 {
		t.Errorf("esperava 2 evictions, got %d", calls)
	}
}

func TestNodeWorkloads(t *testing.T) {
	withHash := func(rs, hash string) func(*corev1.Pod) {
		return func(p *corev1.Pod) {
			p.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs}}
			p.Labels = map[string]string{"pod-template-hash": hash}
		}
	}
	cs := fake.NewSimpleClientset(
		drainTestPod("api-1", withHash("checkout-api-7d9f8", "7d9f8")),
		drainTestPod("api-2", withHash("checkout-api-7d9f8", "7d9f8")),
		drainTestPod("ds-1", func(p *corev1.Pod) { p.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}} }),
		drainTestPod("outro-node", withHash("x-1", "1"), func(p *corev1.Pod) { p.Spec.NodeName = "node-2" }),
	)
	// O fake não aplica field selectors: filtra por node aqui para validar só o agrupamento.
	got, err := NewClient(cs, "c").NodeWorkloads(context.Background(), "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Namespace != "app" {
		t.Fatalf("namespaces = %+v", got)
	}
	if got[0].Pods < 3 || strings.Join(got[0].Deployments, ",") == "" || got[0].Deployments[0] != "checkout-api" {
		t.Errorf("agrupamento inesperado: %+v", got[0])
	}
	if podDeploymentName(drainTestPod("sem-hash", func(p *corev1.Pod) {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "rs-avulso"}}
	})) != "" {
		t.Error("ReplicaSet sem pod-template-hash não deveria virar Deployment")
	}
}
