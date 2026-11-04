package finops

import (
	"context"
	"errors"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var hpaGR = schema.GroupResource{Group: "autoscaling", Resource: "horizontalpodautoscalers"}

func runningPod(ns, name, owner string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name,
			OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: owner}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func hpaFor(ns, target string, min, max int32) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: target + "-hpa"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Name: target},
			MinReplicas:    &min, MaxReplicas: max,
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: min},
	}
}

// denyHPAs faz a listagem de HPAs falhar: no escopo de cluster (namespace vazio) e nos namespaces
// em `deniedNS`; qualquer outro namespace segue normal.
func denyHPAs(client *fake.Clientset, clusterScope bool, deniedNS ...string) {
	denied := map[string]bool{}
	for _, ns := range deniedNS {
		denied[ns] = true
	}
	client.PrependReactor("list", "horizontalpodautoscalers", func(a k8stesting.Action) (bool, runtime.Object, error) {
		ns := a.GetNamespace()
		if (ns == "" && clusterScope) || denied[ns] {
			return true, nil, apierrors.NewForbidden(hpaGR, "", errors.New("denied"))
		}
		return false, nil, nil
	})
}

func byWorkload(raw []rawWorkload) map[string]rawWorkload {
	m := map[string]rawWorkload{}
	for _, w := range raw {
		m[w.Namespace+"/"+w.Workload] = w
	}
	return m
}

func twoNamespaces() *fake.Clientset {
	return fake.NewSimpleClientset(
		runningPod("a", "kafka-0", "kafka"), runningPod("b", "redis-0", "redis"),
		hpaFor("a", "kafka", 2, 6), hpaFor("b", "redis", 1, 3),
	)
}

// Cluster-scope negado, mas list por namespace liberado: o relatório sai com os dados de HPA e sem aviso.
func TestCollectWorkloads_HPAForbiddenNoCluster_FazFallbackPorNamespace(t *testing.T) {
	client := twoNamespaces()
	denyHPAs(client, true)
	raw, _, _, _, warnings, err := collectWorkloads(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("não deveria falhar: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("fallback completo não deveria avisar: %v", warnings)
	}
	w := byWorkload(raw)
	if got := w["a/kafka"]; got.HPAName != "kafka-hpa" || got.HPAMin != 2 || got.HPAMax != 6 {
		t.Errorf("a/kafka sem dados de HPA: %+v", got)
	}
	if got := w["b/redis"]; got.HPAName != "redis-hpa" || got.HPAMax != 3 {
		t.Errorf("b/redis sem dados de HPA: %+v", got)
	}
}

// Um namespace também negado: o relatório sai, o workload dele fica sem HPA e o aviso o nomeia.
func TestCollectWorkloads_HPAForbiddenParcial_AvisaONamespaceNegado(t *testing.T) {
	client := twoNamespaces()
	denyHPAs(client, true, "b")
	raw, _, _, _, warnings, err := collectWorkloads(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("não deveria falhar: %v", err)
	}
	w := byWorkload(raw)
	if w["a/kafka"].HPAName != "kafka-hpa" {
		t.Errorf("a/kafka deveria ter HPA: %+v", w["a/kafka"])
	}
	if w["b/redis"].HPAName != "" {
		t.Errorf("b/redis foi negado, não deveria ter HPA: %+v", w["b/redis"])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "1 de 2 namespaces (b)") {
		t.Errorf("aviso incorreto: %v", warnings)
	}
}

// Tudo negado: ainda gera o relatório (era aqui que ele falhava inteiro), sem dados de HPA e com aviso.
func TestCollectWorkloads_HPAForbiddenTotal_NaoDerrubaORelatorio(t *testing.T) {
	client := twoNamespaces()
	denyHPAs(client, true, "a", "b")
	raw, _, _, _, warnings, err := collectWorkloads(context.Background(), client, nil)
	if err != nil {
		t.Fatalf("o relatório não pode falhar por falta de permissão em HPA: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("workloads: %+v", raw)
	}
	for _, w := range raw {
		if w.HPAName != "" {
			t.Errorf("sem permissão, %s/%s não deveria ter HPA", w.Namespace, w.Workload)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "nem por namespace") ||
		!strings.Contains(warnings[0], "container.horizontalPodAutoscalers.list") {
		t.Errorf("aviso incorreto: %v", warnings)
	}
}

// Só Forbidden é tolerado: outro erro de API continua abortando o relatório.
func TestCollectWorkloads_HPAErroQueNaoEForbiddenContinuaSendoErro(t *testing.T) {
	client := twoNamespaces()
	client.PrependReactor("list", "horizontalpodautoscalers", func(a k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("boom"))
	})
	if _, _, _, _, _, err := collectWorkloads(context.Background(), client, nil); err == nil {
		t.Fatal("erro interno do apiserver deveria abortar")
	}
}

func TestCollectWorkloads_HPASemNegacaoNaoAvisa(t *testing.T) {
	_, _, _, _, warnings, err := collectWorkloads(context.Background(), twoNamespaces(), nil)
	if err != nil || len(warnings) != 0 {
		t.Errorf("err=%v warnings=%v", err, warnings)
	}
}

// O fallback respeita o filtro de namespaces da análise: não consulta (nem avisa sobre) os de fora.
func TestCollectWorkloads_HPAFallbackRespeitaFiltroDeNamespace(t *testing.T) {
	client := twoNamespaces()
	denyHPAs(client, true, "b")
	_, _, _, _, warnings, err := collectWorkloads(context.Background(), client, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("b está fora do filtro, não deveria gerar aviso: %v", warnings)
	}
}

func TestHPADeniedWarnings_CortaALista(t *testing.T) {
	denied := []string{"n1", "n2", "n3", "n4", "n5", "n6", "n7"}
	w := hpaDeniedWarnings(denied, 10)
	if len(w) != 1 || !strings.Contains(w[0], "7 de 10 namespaces (n1, n2, n3, n4, n5 e mais 2)") {
		t.Errorf("aviso: %v", w)
	}
	if hpaDeniedWarnings(nil, 3) != nil {
		t.Error("sem negados não há aviso")
	}
}
