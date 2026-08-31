package kubernetes

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// int32Ptr — helper local, mesmo padrão usado noutros testes deste pacote pra literais *int32.
func int32Ptr(v int32) *int32 { return &v }

// newTestDeploymentWithRevisions monta um Deployment + N ReplicaSets (owner via UID, mesma relação
// real que o controller de Deployment usa) simulando um histórico de rollout real: cada
// ReplicaSet tem a annotation `deployment.kubernetes.io/revision` e uma imagem diferente, a mais
// recente é a "atual" (revision == a que o Deployment também carrega em suas próprias
// annotations). Fixture pensada pra bater exatamente com o cenário do usuário: revisão 131 atual,
// 130 e 125 disponíveis pra rollback.
func newTestDeploymentWithRevisions(t *testing.T) (*fake.Clientset, *appsv1.Deployment) {
	t.Helper()
	depUID := types.UID("dep-uid-1")
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mp-promo-admin",
			Namespace: "plataforma-cb-prd",
			UID:       depUID,
			Annotations: map[string]string{
				deploymentRevisionAnnotation: "131",
			},
			Generation: 3,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "registry/mp-promo-admin:v131"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3,
			UpdatedReplicas:    2,
			AvailableReplicas:  2,
			Replicas:           2,
		},
	}

	owner := []metav1.OwnerReference{{Kind: "Deployment", UID: depUID, Name: dep.Name}}
	rsFor := func(revision, image string, createdOffset time.Duration) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              dep.Name + "-" + revision,
				Namespace:         dep.Namespace,
				OwnerReferences:   owner,
				Annotations:       map[string]string{deploymentRevisionAnnotation: revision},
				CreationTimestamp: metav1.NewTime(time.Now().Add(createdOffset)),
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: int32Ptr(2),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{deploymentChangeCauseAnnotation: "deploy automático CI/CD rev " + revision},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: image}},
					},
				},
			},
		}
	}

	rs131 := rsFor("131", "registry/mp-promo-admin:v131", 0)
	rs130 := rsFor("130", "registry/mp-promo-admin:v130", -1*time.Hour)
	rs125 := rsFor("125", "registry/mp-promo-admin:v125", -24*time.Hour)
	// ReplicaSet de OUTRO deployment, mesmo namespace — nunca deveria aparecer na lista (guard
	// contra usar label/nome em vez de UID pra achar o owner).
	unrelated := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "outro-app-abc123",
			Namespace:       dep.Namespace,
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", UID: types.UID("outro-uid"), Name: "outro-app"}},
			Annotations:     map[string]string{deploymentRevisionAnnotation: "5"},
		},
	}

	clientset := fake.NewSimpleClientset(dep, rs131, rs130, rs125, unrelated)
	return clientset, dep
}

func TestListDeploymentRevisions_OrderedAndScopedByOwnerUID(t *testing.T) {
	clientset, dep := newTestDeploymentWithRevisions(t)
	c := NewClient(clientset, "test-cluster")

	revisions, err := c.ListDeploymentRevisions(context.Background(), dep.Namespace, dep.Name)
	if err != nil {
		t.Fatalf("ListDeploymentRevisions falhou: %v", err)
	}
	if len(revisions) != 3 {
		t.Fatalf("len(revisions) = %d, esperava 3 (ReplicaSet de outro deployment não deveria entrar)", len(revisions))
	}

	// Ordem decrescente: 131, 130, 125
	wantOrder := []int64{131, 130, 125}
	for i, want := range wantOrder {
		if revisions[i].Revision != want {
			t.Errorf("revisions[%d].Revision = %d, esperava %d (ordem decrescente)", i, revisions[i].Revision, want)
		}
	}

	if !revisions[0].IsCurrent {
		t.Error("revisão 131 deveria ser IsCurrent=true (bate com a annotation do Deployment)")
	}
	if revisions[1].IsCurrent || revisions[2].IsCurrent {
		t.Error("revisões 130/125 não deveriam ser IsCurrent")
	}

	if revisions[1].ChangeCause != "deploy automático CI/CD rev 130" {
		t.Errorf("ChangeCause da revisão 130 = %q, inesperado", revisions[1].ChangeCause)
	}
	if len(revisions[2].Images) != 1 || revisions[2].Images[0] != "registry/mp-promo-admin:v125" {
		t.Errorf("Images da revisão 125 = %v, esperava [registry/mp-promo-admin:v125]", revisions[2].Images)
	}
}

func TestRollbackDeploymentToRevision_RejectsSameRevision(t *testing.T) {
	clientset, dep := newTestDeploymentWithRevisions(t)
	c := NewClient(clientset, "test-cluster")

	_, err := c.RollbackDeploymentToRevision(context.Background(), dep.Namespace, dep.Name, 131, "motivo qualquer")
	if err == nil {
		t.Fatal("esperava erro ao tentar reverter pra revisão já atual (131), got nil")
	}
	if _, ok := err.(*ErrRollbackSameRevision); !ok {
		t.Errorf("erro = %v (%T), esperava *ErrRollbackSameRevision", err, err)
	}
}

func TestRollbackDeploymentToRevision_PatchesTemplateAndRecordsChangeCause(t *testing.T) {
	clientset, dep := newTestDeploymentWithRevisions(t)
	c := NewClient(clientset, "test-cluster")

	updated, err := c.RollbackDeploymentToRevision(context.Background(), dep.Namespace, dep.Name, 130, "Rollback via K8s HPA Manager: revisão 131 -> 130 (motivo: instabilidade)")
	if err != nil {
		t.Fatalf("RollbackDeploymentToRevision falhou: %v", err)
	}

	if len(updated.Spec.Template.Spec.Containers) != 1 || updated.Spec.Template.Spec.Containers[0].Image != "registry/mp-promo-admin:v130" {
		t.Fatalf("imagem do template pós-rollback = %+v, esperava v130", updated.Spec.Template.Spec.Containers)
	}
	gotCause := updated.Spec.Template.Annotations[deploymentChangeCauseAnnotation]
	if gotCause != "Rollback via K8s HPA Manager: revisão 131 -> 130 (motivo: instabilidade)" {
		t.Errorf("change-cause annotation = %q, inesperado", gotCause)
	}

	// Confirma que o Update() realmente persistiu no fake clientset (não só no objeto em memória
	// devolvido) — relê direto do fake API server.
	persisted, err := clientset.AppsV1().Deployments(dep.Namespace).Get(context.Background(), dep.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("falha ao reler deployment após rollback: %v", err)
	}
	if persisted.Spec.Template.Spec.Containers[0].Image != "registry/mp-promo-admin:v130" {
		t.Errorf("imagem persistida = %q, esperava v130", persisted.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestRollbackDeploymentToRevision_UnknownRevisionFails(t *testing.T) {
	clientset, dep := newTestDeploymentWithRevisions(t)
	c := NewClient(clientset, "test-cluster")

	_, err := c.RollbackDeploymentToRevision(context.Background(), dep.Namespace, dep.Name, 999, "")
	if err == nil {
		t.Fatal("esperava erro pra revisão inexistente (999), got nil")
	}
}

func TestGetDeploymentRevisionPreviewYAML_NeverMutatesLiveState(t *testing.T) {
	clientset, dep := newTestDeploymentWithRevisions(t)
	c := NewClient(clientset, "test-cluster")

	yamlStr, err := c.GetDeploymentRevisionPreviewYAML(context.Background(), dep.Namespace, dep.Name, 125)
	if err != nil {
		t.Fatalf("GetDeploymentRevisionPreviewYAML falhou: %v", err)
	}
	if yamlStr == "" {
		t.Fatal("YAML de preview vazio")
	}

	// O preview precisa refletir a imagem da revisão 125...
	if !strings.Contains(yamlStr, "v125") {
		t.Errorf("preview YAML não contém a imagem da revisão 125: %s", yamlStr)
	}

	// ...mas o Deployment real no cluster (fake) NUNCA deve ter sido tocado — GetDeploymentRevisionPreviewYAML
	// é só leitura, nunca chama Update().
	persisted, err := clientset.AppsV1().Deployments(dep.Namespace).Get(context.Background(), dep.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("falha ao reler deployment: %v", err)
	}
	if persisted.Spec.Template.Spec.Containers[0].Image != "registry/mp-promo-admin:v131" {
		t.Errorf("preview vazou pro estado real do cluster — imagem persistida = %q, esperava v131 (inalterada)", persisted.Spec.Template.Spec.Containers[0].Image)
	}
}

func TestWaitDeploymentRolloutComplete_ReturnsWhenReplicasMatch(t *testing.T) {
	clientset, dep := newTestDeploymentWithRevisions(t)
	// Simula rollout em andamento: status ainda não bateu com o desejado nem com a geração.
	dep.Status.ObservedGeneration = 2 // Generation real é 3
	dep.Status.UpdatedReplicas = 1
	dep.Status.AvailableReplicas = 1
	dep.Status.Replicas = 2
	clientset = fake.NewSimpleClientset(dep)
	c := NewClient(clientset, "test-cluster")

	// Dispara uma goroutine que "completa" o rollout pouco depois de iniciado o polling — simula
	// o controller convergindo o Status ao longo do tempo, mesmo cenário real.
	go func() {
		time.Sleep(50 * time.Millisecond)
		fresh, _ := clientset.AppsV1().Deployments(dep.Namespace).Get(context.Background(), dep.Name, metav1.GetOptions{})
		fresh.Status.ObservedGeneration = 3
		fresh.Status.UpdatedReplicas = 2
		fresh.Status.AvailableReplicas = 2
		fresh.Status.Replicas = 2
		_, _ = clientset.AppsV1().Deployments(dep.Namespace).UpdateStatus(context.Background(), fresh, metav1.UpdateOptions{})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var ticks int
	err := c.WaitDeploymentRolloutComplete(ctx, dep.Namespace, dep.Name, func(s DeploymentRolloutStatus) { ticks++ })
	if err != nil {
		t.Fatalf("WaitDeploymentRolloutComplete falhou: %v", err)
	}
	if ticks < 2 {
		t.Errorf("ticks = %d, esperava pelo menos 2 (estado incompleto + estado completo)", ticks)
	}
}
