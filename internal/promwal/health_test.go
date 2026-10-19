package promwal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func cr(ann map[string]string, replicas int32) prometheusCR {
	var c prometheusCR
	c.Metadata.Name, c.Metadata.Namespace, c.Metadata.Annotations = "prom", "monitoring", ann
	c.Spec.Replicas = &replicas
	return c
}

// hpod monta um pod Prometheus; age = há quanto tempo foi criado.
func hpod(name string, ready bool, restarts int32, waiting, term string, age time.Duration) corev1.Pod {
	cs := corev1.ContainerStatus{Name: promContainer, Ready: ready, RestartCount: restarts}
	if waiting != "" {
		cs.State.Waiting = &corev1.ContainerStateWaiting{Reason: waiting}
	}
	if term != "" {
		cs.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: term, ExitCode: 137}
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(testNow.Add(-age))},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{cs}},
	}
}

func TestClassifyHealth(t *testing.T) {
	day := 24 * time.Hour
	recentRecurrence := map[string]string{
		AnnotationCleanupCount:   "2",
		AnnotationCleanupHistory: testNow.Add(-2*day).UTC().Format(time.RFC3339) + "," + testNow.Add(-9*day).UTC().Format(time.RFC3339),
	}
	tests := []struct {
		name     string
		cr       prometheusCR
		pods     []corev1.Pod
		hist     []time.Time
		want     HealthState
		severity string
	}{
		{"saudável", cr(nil, 1), []corev1.Pod{hpod("p-0", true, 0, "", "", day)}, nil, HealthHealthy, ""},
		{"saudável com restarts antigos", cr(nil, 1), []corev1.Pod{hpod("p-0", true, 12, "", "OOMKilled", day)}, nil, HealthHealthy, ""},
		{"OOM em CrashLoop", cr(nil, 1), []corev1.Pod{hpod("p-0", false, 9, "CrashLoopBackOff", "OOMKilled", day)}, nil, HealthDown, SeverityCritical},
		{"sem pods", cr(nil, 2), nil, nil, HealthDown, SeverityCritical},
		{"subindo (novo, sem restarts)", cr(nil, 1), []corev1.Pod{hpod("p-0", false, 0, "", "", time.Minute)}, nil, HealthStarting, ""},
		{"não Ready sem restarts mas velho", cr(nil, 1), []corev1.Pod{hpod("p-0", false, 0, "", "", time.Hour)}, nil, HealthDown, SeverityCritical},
		{"imagem não baixa mesmo sendo novo", cr(nil, 1), []corev1.Pod{hpod("p-0", false, 0, "ImagePullBackOff", "", time.Minute)}, nil, HealthDown, SeverityCritical},
		{"uma réplica caída de duas", cr(nil, 2), []corev1.Pod{hpod("p-0", true, 0, "", "", day), hpod("p-1", false, 4, "CrashLoopBackOff", "OOMKilled", day)}, nil, HealthDown, SeverityCritical},
		{"limpeza interrompida", cr(map[string]string{AnnotationOriginalReplicas: "2"}, 0), nil, nil, HealthRestorePending, SeverityCritical},
		{"reincidente por anotação", cr(recentRecurrence, 1), []corev1.Pod{hpod("p-0", true, 0, "", "", day)}, nil, HealthRecurrent, SeverityWarning},
		{"reincidente por anotação + histórico", cr(map[string]string{AnnotationCleanupHistory: testNow.Add(-3 * day).UTC().Format(time.RFC3339)}, 1),
			[]corev1.Pod{hpod("p-0", true, 0, "", "", day)}, []time.Time{testNow.Add(-day)}, HealthRecurrent, SeverityWarning},
		{"fora do ar prevalece sobre reincidente", cr(recentRecurrence, 1), []corev1.Pod{hpod("p-0", false, 3, "CrashLoopBackOff", "OOMKilled", day)}, nil, HealthDown, SeverityCritical},
		{"desligado de propósito", cr(nil, 0), nil, nil, HealthStopped, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyHealth(tc.cr, tc.pods, tc.hist, testNow)
			if got.State != tc.want || got.Severity != tc.severity {
				t.Fatalf("estado=%s severidade=%q, esperado %s/%q (%s)", got.State, got.Severity, tc.want, tc.severity, got.Summary)
			}
		})
	}
}

func TestClassifyHealthResumoENomes(t *testing.T) {
	got := classifyHealth(cr(nil, 1), []corev1.Pod{hpod("prometheus-prom-0", false, 9, "CrashLoopBackOff", "OOMKilled", 24*time.Hour)}, nil, testNow)
	for _, want := range []string{"prometheus-prom-0", "CrashLoopBackOff", "9 restart"} {
		if !strings.Contains(got.Summary, want) {
			t.Errorf("resumo sem %q: %s", want, got.Summary)
		}
	}
	p := classifyHealth(cr(map[string]string{AnnotationOriginalReplicas: "2"}, 0), nil, nil, testNow)
	if p.PendingRestoreReplicas == nil || *p.PendingRestoreReplicas != 2 {
		t.Errorf("deveria informar as réplicas a restaurar: %+v", p)
	}
}

const crListJSON = `{"items":[{"metadata":{"name":"prom","namespace":"monitoring"},"spec":{"replicas":1}}]}`

func healthRunner(out string, err error, calls *[]string) Runner {
	return func(_ context.Context, _, ns string, args ...string) ([]byte, error) {
		*calls = append(*calls, ns+"|"+strings.Join(args, " "))
		return []byte(out), err
	}
}

func TestHealthFluxo(t *testing.T) {
	now = func() time.Time { return testNow }
	t.Cleanup(func() { now = time.Now })

	pod := crashPod("prometheus-prom-0", "node-a")
	pod.CreationTimestamp = metav1.NewTime(testNow.Add(-time.Hour))
	cs := fake.NewSimpleClientset(pod)
	var calls []string
	c := &Collector{Client: cs, Run: healthRunner(crListJSON, nil, &calls)}

	var histAsked []string
	rep := c.Health(context.Background(), "ctx", func(ns, name string) []time.Time {
		histAsked = append(histAsked, ns+"/"+name)
		return nil
	})
	if !rep.Verifiable || !rep.OperatorPresent || len(rep.Items) != 1 {
		t.Fatalf("relatório inesperado: %+v", rep)
	}
	if it := rep.Items[0]; it.State != HealthDown || it.Severity != SeverityCritical || it.Namespace != "monitoring" {
		t.Errorf("item = %+v", it)
	}
	if len(histAsked) != 1 || histAsked[0] != "monitoring/prom" {
		t.Errorf("deveria consultar o histórico do recurso: %v", histAsked)
	}
	// Uma única chamada ao kubectl, em todos os namespaces (sem -n), só leitura.
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "|get prometheus --all-namespaces") {
		t.Errorf("chamadas ao kubectl: %v", calls)
	}
}

func TestHealthSemAlcanceNaoVirAlerta(t *testing.T) {
	for name, errMsg := range map[string]string{
		"sem rede":      "kubectl get: exit status 1: Unable to connect to the server: dial tcp: i/o timeout",
		"sem permissão": `kubectl get: exit status 1: Error from server (Forbidden): prometheuses is forbidden`,
	} {
		t.Run(name, func(t *testing.T) {
			var calls []string
			c := &Collector{Client: fake.NewSimpleClientset(), Run: healthRunner("", errors.New(errMsg), &calls)}
			rep := c.Health(context.Background(), "ctx", nil)
			if rep.Verifiable || rep.Reason == "" || len(rep.Items) != 0 {
				t.Fatalf("deveria ser não verificável, sem itens e com motivo: %+v", rep)
			}
		})
	}
}

func TestHealthClusterSemOperator(t *testing.T) {
	var calls []string
	c := &Collector{Client: fake.NewSimpleClientset(), Run: healthRunner("", errors.New(`error: the server doesn't have a resource type "prometheus"`), &calls)}
	rep := c.Health(context.Background(), "ctx", nil)
	if !rep.Verifiable || rep.OperatorPresent || len(rep.Items) != 0 {
		t.Fatalf("cluster sem operator é verificável e sem itens: %+v", rep)
	}
}

func TestHealthFalhaAoListarPods(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pods is forbidden")
	})
	var calls []string
	c := &Collector{Client: cs, Run: healthRunner(crListJSON, nil, &calls)}
	rep := c.Health(context.Background(), "ctx", nil)
	if len(rep.Items) != 1 || rep.Items[0].State != HealthUnverifiable || rep.Items[0].Severity != "" {
		t.Fatalf("falha ao listar pods deveria ser não verificável e sem alerta: %+v", rep.Items)
	}
}

func TestKubectlArgs(t *testing.T) {
	if got := strings.Join(kubectlArgs("c1", "monitoring", []string{"get", "pods"}), " "); got != "--context c1 -n monitoring get pods" {
		t.Errorf("com namespace: %s", got)
	}
	if got := strings.Join(kubectlArgs("c1", "", []string{"get", "prometheus", "--all-namespaces"}), " "); got != "--context c1 get prometheus --all-namespaces" {
		t.Errorf("sem namespace não pode levar -n: %s", got)
	}
}

func TestHealthResumoDePodNaoAgendado(t *testing.T) {
	pod := hpod("prometheus-prom-0", false, 0, "Pending", "", time.Hour)
	pod.Status.Phase = corev1.PodPending
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: realSchedMsg}}
	got := classifyHealth(cr(nil, 1), []corev1.Pod{pod}, nil, testNow)
	if got.State != HealthDown || got.Severity != SeverityCritical {
		t.Fatalf("pod Pending há 1 h é fora do ar crítico: %+v", got)
	}
	if !strings.Contains(got.Summary, "1 node(s) com taint") || !strings.Contains(got.Summary, "nodeSelector/nodeAffinity") {
		t.Errorf("o resumo deveria trazer o motivo do agendamento: %s", got.Summary)
	}
}
