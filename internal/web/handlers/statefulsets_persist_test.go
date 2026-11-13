package handlers

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var persistT0 = time.Date(2026, 10, 19, 16, 28, 12, 0, time.UTC)

func stsWith(gen int64, writers ...metav1.ManagedFieldsEntry) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "s", Generation: gen, ManagedFields: writers}}
}

func writer(manager string, at time.Time) metav1.ManagedFieldsEntry {
	t := metav1.NewTime(at)
	return metav1.ManagedFieldsEntry{Manager: manager, Operation: metav1.ManagedFieldsOperationUpdate, Time: &t}
}

func TestControllerOwner(t *testing.T) {
	yes, no := true, false
	owned := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{Kind: "Namespace", Name: "x", Controller: &no},
		{Kind: "Prometheus", Name: "prometheus-prometheus", Controller: &yes},
	}}}
	if o := controllerOwner(owned); o == nil || o.Kind != "Prometheus" || o.Name != "prometheus-prometheus" {
		t.Fatalf("dono controlador = %+v", o)
	}
	if controllerOwner(&appsv1.StatefulSet{}) != nil || controllerOwner(nil) != nil {
		t.Error("StatefulSet sem dono controlador não tem owner")
	}
	notController := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{Kind: "X", Name: "y"}}}}
	if controllerOwner(notController) != nil {
		t.Error("ownerReference sem controller=true não é dono controlador")
	}
}

func TestDetectRevert(t *testing.T) {
	since := persistT0.Add(-2 * time.Second)
	operator := writer("PrometheusOperator", persistT0.Add(3*time.Second))

	cases := []struct {
		name            string
		before, applied int64
		after           *appsv1.StatefulSet
		wantReverted    bool
		wantWriter      string
	}{
		{"apply mudou o spec e ficou", 4, 5, stsWith(5, writer("kubectl", persistT0)), false, ""},
		{"apply sem mudança (no-op)", 4, 4, stsWith(4), false, ""},
		{"operator reescreveu depois do apply", 4, 5, stsWith(6, writer("kubectl", persistT0), operator), true, "PrometheusOperator"},
		{"operator reescreveu antes de lermos o resultado", 4, 6, stsWith(6, operator), true, "PrometheusOperator"},
		{"geração andou mas só o nosso gerenciador escreveu", 4, 5, stsWith(6, writer("kubectl", persistT0)), false, ""},
		{"escrita de outro gerenciador anterior ao apply não conta", 4, 5, stsWith(6, writer("helm", persistT0.Add(-time.Hour))), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, by := detectRevert(tc.before, tc.applied, tc.after, since)
			if got != tc.wantReverted || by != tc.wantWriter {
				t.Fatalf("reverted=%v por %q, esperado %v por %q", got, by, tc.wantReverted, tc.wantWriter)
			}
		})
	}
	if got, _ := detectRevert(1, 2, nil, since); got {
		t.Error("sem objeto relido não dá para afirmar reversão")
	}
}

func TestLastForeignWriterEscolheOMaisRecente(t *testing.T) {
	sts := stsWith(3,
		writer("helm", persistT0.Add(time.Second)),
		writer("PrometheusOperator", persistT0.Add(4*time.Second)),
		writer("kubectl", persistT0.Add(10*time.Second)), // o nosso, ignorado mesmo sendo o mais recente
	)
	if got := lastForeignWriter(sts, persistT0); got != "PrometheusOperator" {
		t.Errorf("gerenciador = %q, esperado PrometheusOperator", got)
	}
}

func TestSleepCtx(t *testing.T) {
	if !sleepCtx(context.Background(), time.Millisecond) {
		t.Error("deveria esperar até o fim")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, time.Minute) {
		t.Error("ctx cancelado deve interromper a espera")
	}
}
