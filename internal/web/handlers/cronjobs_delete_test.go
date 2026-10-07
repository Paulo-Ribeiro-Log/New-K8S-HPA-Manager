package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// fakeClientWith monta um cluster falso com: o CronJob (gerenciado por Helm), 2 Jobs dele (um
// rodando, um concluído), um Job de outro CronJob com o mesmo nome (UID diferente) e um Job avulso.
func fakeClientWith(now time.Time) *fake.Clientset {
	return fake.NewSimpleClientset(delTestObjects(now)...)
}

func delTestObjects(now time.Time) []runtime.Object {
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "restart", Namespace: "ns", UID: "cj-uid",
			Annotations: map[string]string{"meta.helm.sh/release-name": "minha-release"}},
		Spec: batchv1.CronJobSpec{Schedule: "15,45 * * * 1-5"},
	}
	owner := []metav1.OwnerReference{{Kind: "CronJob", Name: "restart", UID: "cj-uid"}}
	running := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "restart-2", Namespace: "ns", UID: "j2", OwnerReferences: owner,
		CreationTimestamp: metav1.NewTime(now.Add(-time.Minute))}, Status: batchv1.JobStatus{Active: 1}}
	done := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "restart-1", Namespace: "ns", UID: "j1", OwnerReferences: owner,
		CreationTimestamp: metav1.NewTime(now.Add(-time.Hour))},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour + 30*time.Second))}}}}
	// mesmo nome de dono, mas outro UID (CronJob antigo apagado/recriado): não pode entrar na prévia
	stale := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "restart-old", Namespace: "ns", UID: "j0",
		OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", Name: "restart", UID: "outro-uid"}}}}
	other := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "avulso", Namespace: "ns", UID: "j9"}}
	return []runtime.Object{cj, running, done, stale, other}
}

func TestCronJobDeletePreview(t *testing.T) {
	now := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	cs := fakeClientWith(now)
	p, err := buildCronJobDeletePreview(context.Background(), cs, "ns", "restart", now)
	if err != nil {
		t.Fatal(err)
	}
	if p.UID != "cj-uid" || !strings.HasPrefix(p.ManagedBy, "Helm") || len(p.Jobs) != 2 || p.ActiveJobs != 1 {
		t.Fatalf("prévia: %+v", p)
	}
	if p.Jobs[0].Name != "restart-2" || p.Jobs[0].Status != "Running" || p.Jobs[1].Status != "Succeeded" || p.Jobs[1].DurationSeconds != 30 {
		t.Errorf("jobs (mais recente primeiro): %+v", p.Jobs)
	}
}

func lastDeleteOptions(t *testing.T, cs *fake.Clientset) metav1.DeleteOptions {
	t.Helper()
	acts := cs.Actions()
	for i := len(acts) - 1; i >= 0; i-- {
		if d, ok := acts[i].(k8stesting.DeleteActionImpl); ok {
			return d.DeleteOptions
		}
	}
	t.Fatal("nenhum delete foi chamado")
	return metav1.DeleteOptions{}
}

func TestDeleteCronJobWithUID(t *testing.T) {
	now := time.Now()
	ctx := context.Background()

	// UID diferente da prévia: recusa sem apagar
	cs := fakeClientWith(now)
	if _, err := deleteCronJobWithUID(ctx, cs, "ns", "restart", "uid-velho", false); err != errObjectReplaced {
		t.Fatalf("esperava errObjectReplaced, veio %v", err)
	}
	if _, err := cs.BatchV1().CronJobs("ns").Get(ctx, "restart", metav1.GetOptions{}); err != nil {
		t.Fatal("o CronJob não podia ter sido apagado")
	}

	// apagar junto os Jobs → Background + precondition de UID; manifesto limpo para o histórico
	manifest, err := deleteCronJobWithUID(ctx, cs, "ns", "restart", "cj-uid", false)
	if err != nil {
		t.Fatal(err)
	}
	opts := lastDeleteOptions(t, cs)
	if *opts.PropagationPolicy != metav1.DeletePropagationBackground || *opts.Preconditions.UID != types.UID("cj-uid") {
		t.Errorf("opções: %+v", opts)
	}
	if !strings.Contains(manifest, "kind: CronJob") || !strings.Contains(manifest, "schedule: 15,45 * * * 1-5") || strings.Contains(manifest, "uid:") || strings.Contains(manifest, "status:") {
		t.Errorf("manifesto do histórico:\n%s", manifest)
	}

	// manter os Jobs → Orphan
	cs = fakeClientWith(now)
	if _, err := deleteCronJobWithUID(ctx, cs, "ns", "restart", "cj-uid", true); err != nil {
		t.Fatal(err)
	}
	if opts := lastDeleteOptions(t, cs); *opts.PropagationPolicy != metav1.DeletePropagationOrphan {
		t.Errorf("keepJobs deveria usar Orphan: %v", *opts.PropagationPolicy)
	}
}

func TestDeleteJobWithUID(t *testing.T) {
	ctx := context.Background()
	cs := fakeClientWith(time.Now())
	if _, err := deleteJobWithUID(ctx, cs, "ns", "restart-2", "j-errado"); err != errObjectReplaced {
		t.Fatalf("esperava errObjectReplaced, veio %v", err)
	}
	info, err := deleteJobWithUID(ctx, cs, "ns", "restart-2", "j2")
	if err != nil || info.Status != "Running" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	// padrão da API para Job é Orphan (pods ficariam para trás): tem que ser Background explícito
	if opts := lastDeleteOptions(t, cs); *opts.PropagationPolicy != metav1.DeletePropagationBackground || *opts.Preconditions.UID != types.UID("j2") {
		t.Errorf("opções: %+v", opts)
	}
}

func TestBatchManagedBy(t *testing.T) {
	cases := []struct {
		labels, annotations map[string]string
		want                string
	}{
		{nil, map[string]string{"meta.helm.sh/release-name": "x"}, "Helm (release x)"},
		{map[string]string{"app.kubernetes.io/managed-by": "Helm"}, nil, "Helm"},
		{map[string]string{"argocd.argoproj.io/instance": "app"}, nil, "Argo CD"},
		{nil, map[string]string{"argocd.argoproj.io/tracking-id": "app:batch/CronJob:ns/x"}, "Argo CD"},
		{map[string]string{"kustomize.toolkit.fluxcd.io/name": "k"}, nil, "Flux"},
		{map[string]string{"app": "x"}, nil, ""},
	}
	for _, c := range cases {
		if got := batchManagedBy(c.labels, c.annotations); got != c.want {
			t.Errorf("%v %v: %q, want %q", c.labels, c.annotations, got, c.want)
		}
	}
}
