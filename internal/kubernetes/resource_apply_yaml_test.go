package kubernetes

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResourceApplyYAMLTemApiVersionKindEEscopoMinimo(t *testing.T) {
	replicas := int32(2)
	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
	}
	out, err := resourceApplyYAML("StatefulSet", "monitoring", "prometheus-x", "prometheus", &replicas, res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"apiVersion: apps/v1", "kind: StatefulSet", "name: prometheus-x", "namespace: monitoring",
		"replicas: 2", "name: prometheus", "memory: 4Gi", "memory: 8Gi"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
	for _, no := range []string{"managedFields", "status", "resourceVersion", "creationTimestamp: null"} {
		if strings.Contains(out, no) {
			t.Errorf("não devia conter %q em:\n%s", no, out)
		}
	}
	// o payload tem que passar pela mesma validação que o kubectl faz antes do apply
	if _, ns, name, err := prepareStatefulSetApplyPayload(out, "monitoring", "prometheus-x"); err != nil || ns != "monitoring" || name != "prometheus-x" {
		t.Errorf("prepareStatefulSetApplyPayload: ns=%q name=%q err=%v", ns, name, err)
	}
}

func TestResourceApplyYAMLSemReplicas(t *testing.T) {
	out, err := resourceApplyYAML("DaemonSet", "kube-system", "ds", "c", nil, corev1.ResourceRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "replicas") {
		t.Errorf("DaemonSet não tem replicas:\n%s", out)
	}
}

func TestPrometheusOwnerName(t *testing.T) {
	yes := true
	sts := &appsv1.StatefulSet{}
	sts.OwnerReferences = []metav1.OwnerReference{{APIVersion: "monitoring.coreos.com/v1", Kind: "Prometheus", Name: "prom", Controller: &yes}}
	if got := prometheusOwnerName(sts); got != "prom" {
		t.Errorf("got %q", got)
	}
	sts.OwnerReferences[0].Controller = nil
	if got := prometheusOwnerName(sts); got != "" {
		t.Errorf("sem controller:true deveria ser vazio, got %q", got)
	}
	sts.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Prometheus", Name: "x", Controller: &yes}}
	if got := prometheusOwnerName(sts); got != "" {
		t.Errorf("outro grupo deveria ser vazio, got %q", got)
	}
}

func TestBuildPrometheusResourcesPatch(t *testing.T) {
	r := int32(1)
	got, err := buildPrometheusResourcesPatch(ResourcePatchRequest{MemoryRequest: "4Gi", MemoryLimit: "8Gi", Replicas: &r})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"spec":{"replicas":1,"resources":{"limits":{"memory":"8Gi"},"requests":{"memory":"4Gi"}}}}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if _, err := buildPrometheusResourcesPatch(ResourcePatchRequest{}); err == nil {
		t.Error("pedido vazio deveria dar erro")
	}
	if _, err := buildPrometheusResourcesPatch(ResourcePatchRequest{MemoryRequest: "abc"}); err == nil {
		t.Error("quantidade inválida deveria dar erro")
	}
}
